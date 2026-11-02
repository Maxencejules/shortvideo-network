package leases_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

var update = flag.Bool("update", false, "rewrite testdata/sign-vectors.json")

const vectorsFile = "testdata/sign-vectors.json"

// signVector is one golden provider inner-signature vector. Keys are
// derived in the test from key_label (sha256("svn-leases-test-seed:" +
// label) is the ed25519 seed); the file holds only the public key.
type signVector struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	KeyLabel  string `json:"key_label"`
	PublicKey string `json:"public_key"`
	Canonical string `json:"canonical"`
	SignBytes string `json:"sign_bytes_sha256"`
	Signature string `json:"signature"`
}

type vectorFile struct {
	Comment string       `json:"comment"`
	Vectors []signVector `json:"vectors"`
}

// vectorOffer is a fixed stored offer for the acceptance vectors.
func vectorOffer() *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId: "o1", ContentId: "c1", ContentVersion: 1, ProviderId: "p1",
		ManifestDigest: manifestOf("c1"), PackageBytes: 1000, TermsDigest: digestOf("terms:o1"),
		StartUnixSeconds: 1767225700, DeadlineUnixSeconds: 1767229300, ExpiryUnixSeconds: 1767226200,
		TermVersion: 1, Scheduler: serviceAcct,
	}
}

// extremeOffer has every integer at an extreme and 64-byte ids: the
// payload still exists (integers are decimal strings).
func extremeOffer() *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId: maxID("o", 1), ContentId: maxID("c", 1), ContentVersion: math.MaxUint64, ProviderId: maxID("p", 1),
		ManifestDigest: manifestOf("c1"), PackageBytes: math.MaxUint64, TermsDigest: digestOf("terms"),
		StartUnixSeconds: math.MinInt64, DeadlineUnixSeconds: math.MaxInt64, ExpiryUnixSeconds: -1,
		TermVersion: math.MaxUint64, Scheduler: serviceAcct,
	}
}

func buildVectors(t testing.TB) []signVector {
	t.Helper()
	type input struct {
		name   string
		domain typesv1.SignatureDomain
		label  string
		build  func() ([]byte, string, error)
	}
	accept := func(o *leasesv1.Offer, leaseID string) func() ([]byte, string, error) {
		return func() ([]byte, string, error) {
			b, err := leases.AcceptSignBytes(o, leaseID)
			if err != nil {
				return nil, "", err
			}
			c, err := leases.AcceptCanonicalForTest(o, leaseID)
			return b, c, err
		}
	}
	renew := func(leaseID string, tv uint64, deadline int64, d *typesv1.Digest, n uint64, ev string) func() ([]byte, string, error) {
		return func() ([]byte, string, error) {
			b, err := leases.RenewSignBytes(leaseID, tv, deadline, d, n, ev)
			if err != nil {
				return nil, "", err
			}
			c, err := leases.RenewCanonicalForTest(leaseID, tv, deadline, d, n, ev)
			return b, c, err
		}
	}
	end := func(leaseID, providerID string, tv uint64) func() ([]byte, string, error) {
		return func() ([]byte, string, error) {
			b, err := leases.EndSignBytes(leaseID, providerID, tv)
			if err != nil {
				return nil, "", err
			}
			c, err := leases.EndCanonicalForTest(leaseID, providerID, tv)
			return b, c, err
		}
	}
	inputs := []input{
		{"accept", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, "p1", accept(vectorOffer(), "l1")},
		{"accept-extreme-values", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, "p1", accept(extremeOffer(), maxID("l", 1))},
		{"renew", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, "p1", renew("l1", 2, 1767232900, manifestOf("c1"), 1000, "")},
		{"renew-with-evidence", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, "p1", renew("l1", 3, 1767236500, manifestOf("c1"), 1000, "ev-1")},
		{"renew-out-of-range", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, "p1", renew("l1", math.MaxUint64, -5, manifestOf("c1"), math.MaxUint64, "")},
		{"end", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, "p1", end("l1", "p1", 1)},
		{"end-after-renewal", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, "p1", end("l1", "p1", 2)},
	}
	var out []signVector
	for _, in := range inputs {
		signBytes, canonical, err := in.build()
		if err != nil {
			t.Fatalf("%s: %v", in.name, err)
		}
		domain, _ := msgkit.DomainString(in.domain)
		if !bytes.Equal(signBytes, append(append([]byte(domain), 0), canonical...)) {
			t.Fatalf("%s: sign bytes are not domain || 0x00 || canonical", in.name)
		}
		sig := ed25519.Sign(testKey(in.label), signBytes)
		if !msgkit.VerifyWithKey(pub(in.label), in.domain, canonical, sig) {
			t.Fatalf("%s: signature does not verify", in.name)
		}
		sum := sha256.Sum256(signBytes)
		out = append(out, signVector{
			Name: in.name, Domain: domain, KeyLabel: in.label, PublicKey: hex.EncodeToString(pub(in.label)),
			Canonical: canonical, SignBytes: hex.EncodeToString(sum[:]), Signature: hex.EncodeToString(sig),
		})
	}
	return out
}

// TestSignBytesVectors pins the provider ACCEPT, RENEW, and END payloads
// (decision request DR-08-04) with golden vectors. Run with -update to
// rewrite the file after an intended payload change.
func TestSignBytesVectors(t *testing.T) {
	got := vectorFile{
		Comment: "Golden x/leases provider inner-signature vectors (DR-08-04). Signed bytes are UTF-8(domain) || 0x00 || canonical. " +
			"Test keys: ed25519 seed = sha256(\"svn-leases-test-seed:\" + key_label); no private key is stored. Synthetic values only.",
		Vectors: buildVectors(t),
	}
	path := filepath.FromSlash(vectorsFile)
	if *update {
		raw, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	var want vectorFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&want); err != nil {
		t.Fatal(err)
	}
	if len(want.Vectors) != len(got.Vectors) {
		t.Fatalf("%d vectors in the file, %d built", len(want.Vectors), len(got.Vectors))
	}
	for i := range got.Vectors {
		if got.Vectors[i] != want.Vectors[i] {
			t.Fatalf("vector %s differs:\n got %+v\nwant %+v", got.Vectors[i].Name, got.Vectors[i], want.Vectors[i])
		}
	}
	// The file carries public keys only: no 64-byte ed25519 private key
	// of any label appears in it.
	for _, v := range got.Vectors {
		if bytes.Contains(raw, []byte(hex.EncodeToString(testKey(v.KeyLabel)))) ||
			bytes.Contains(raw, []byte(hex.EncodeToString(testKey(v.KeyLabel).Seed()))) {
			t.Fatalf("vector file holds the private key of %s", v.KeyLabel)
		}
	}
	accept := want.Vectors[0].Canonical
	for _, key := range []string{"contentId", "contentVersion", "deadlineUnixSeconds", "expiryUnixSeconds", "leaseId", "manifestDigest",
		"offerId", "packageBytes", "providerId", "scheduler", "startUnixSeconds", "termVersion", "termsDigest"} {
		if !strings.Contains(accept, `"`+key+`":`) {
			t.Fatalf("acceptance payload lacks %s: %s", key, accept)
		}
	}
	// The end payload binds the lease's term version (D-12): the vectors at
	// term 1 and term 2 differ.
	ends := map[string]string{}
	for _, v := range want.Vectors {
		if v.Domain == "SVN-PROVIDER-END-1" {
			ends[v.Name] = v.Canonical
		}
	}
	if len(ends) != 2 || ends["end"] == ends["end-after-renewal"] ||
		!strings.Contains(ends["end"], `"termVersion":"1"`) || !strings.Contains(ends["end-after-renewal"], `"termVersion":"2"`) {
		t.Fatalf("end payloads do not bind the term version: %v", ends)
	}
}

// TestSignPayloadNeverFails: integers are decimal strings, so a payload
// exists for every integer value (negative times, 2^64-1 counters); the
// value is then rejected by its own range check, not by the signature. Only
// a nil offer or a payload above the SVN-JCS-1 size cap has no payload.
func TestSignPayloadNeverFails(t *testing.T) {
	canonical, err := leases.RenewCanonicalForTest("l1", math.MaxUint64, -5, manifestOf("c1"), math.MaxUint64, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"deadlineUnixSeconds":"-5"`, `"termVersion":"18446744073709551615"`, `"packageBytes":"18446744073709551615"`, `"evidenceId":""`} {
		if !strings.Contains(canonical, want) {
			t.Fatalf("payload %s lacks %s", canonical, want)
		}
	}
	extreme, err := leases.AcceptCanonicalForTest(extremeOffer(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"startUnixSeconds":"-9223372036854775808"`, `"deadlineUnixSeconds":"9223372036854775807"`, `"expiryUnixSeconds":"-1"`} {
		if !strings.Contains(extreme, want) {
			t.Fatalf("payload %s lacks %s", extreme, want)
		}
	}
	if _, err := leases.RenewSignBytes("l1", 2, 1, nil, 1, ""); err != nil {
		t.Fatalf("nil manifest digest: %v", err)
	}
	if _, err := leases.AcceptSignBytes(nil, "l1"); err == nil {
		t.Fatal("nil offer has a payload")
	}
	if _, err := leases.RenewSignBytes("l1", 2, 1, manifestOf("c1"), 1, strings.Repeat("e", 70_000)); err == nil {
		t.Fatal("a payload above 64 KiB has a canonical form")
	}

	// A renewal whose signed deadline is out of range: the signature
	// verifies, the deadline is BOUNDS (after the signature and the gate).
	e := newEnv(t)
	e.seedLease(e.leaseRecord("l1", "c1", "p1"))
	msg := e.renewMsg("l1", "ren-1")
	msg.DeadlineUnixSeconds = -5
	e.signRenew(msg, "p1", "key-p1")
	e.requireReject(e.renew(e.fresh(), msg), msgkit.Bounds, "signed negative deadline")
	// An evidence id whose payload has no canonical form cannot carry a
	// valid signature: UNAUTHORIZED after the structural checks.
	long := e.renewMsg("l1", "ren-2")
	long.EvidenceId = strings.Repeat("e", 70_000)
	e.requireReject(e.renew(e.fresh(), long), msgkit.Unauthorized, "non-canonical payload")
	// The structural checks still run first on such a payload.
	long.ProviderSignature.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END
	e.requireReject(e.renew(e.fresh(), long), msgkit.WrongDomain, "non-canonical payload, wrong domain")
}
