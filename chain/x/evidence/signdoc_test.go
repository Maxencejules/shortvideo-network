package evidence_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec/unknownproto"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

var update = flag.Bool("update", false, "rewrite testdata/sign-vectors.json")

const vectorsFile = "testdata/sign-vectors.json"

// signVector is one golden SVN-EVIDENCE-ATTESTATION-1 vector. Keys are
// derived in the test from key_label (sha256("svn-evidence-test-seed:" +
// label) is the ed25519 seed; TEST-ONLY); the file holds only the public
// key, the signer address, the canonical text, a hash of the sign bytes, and
// the signature.
type signVector struct {
	Name      string `json:"name"`
	KeyLabel  string `json:"key_label"`
	PublicKey string `json:"public_key"`
	Signer    string `json:"signer"`
	Canonical string `json:"canonical"`
	SignBytes string `json:"sign_bytes_sha256"`
	Signature string `json:"signature"`
}

type vectorFile struct {
	Comment string       `json:"comment"`
	Vectors []signVector `json:"vectors"`
}

// vectorItem is a fixed item of the auditor of label: no store is read.
func vectorItem(label, suffix, leaseID, contentID string) *evidencev1.EvidenceItem {
	return &evidencev1.EvidenceItem{
		EvidenceId:        prefix(label) + suffix,
		Kind:              evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		LeaseId:           leaseID,
		ContentId:         contentID,
		ContentVersion:    1,
		ObservationDigest: manifestOf(contentID),
		ExpiryUnixSeconds: now + itemLife,
		Attestation:       true,
	}
}

// maxID is an id of exactly 64 bytes starting with start.
func maxID(start string) string { return start + strings.Repeat("z", msgkit.IDMaxBytes-len(start)) }

// maximalItem has 64-byte ids and the largest canonical integers.
func maximalItem(label string, i int) *evidencev1.EvidenceItem {
	it := vectorItem(label, "", "", "")
	it.EvidenceId = maxID(fmt.Sprintf("%s%03d-", prefix(label), i))
	it.LeaseId = maxID(fmt.Sprintf("l%03d-", i))
	it.ContentId = maxID(fmt.Sprintf("c%03d-", i))
	it.ContentVersion = uint64(genesis.SafeIntegerMax)
	it.ExpiryUnixSeconds = genesis.UnixSecondsMax
	return it
}

// maximalBatch has n maximal items, a 64-byte batch id and key id, and the
// largest canonical epoch.
func maximalBatch(label string, n int) *evidencev1.MsgSubmitAuditBatch {
	items := make([]*evidencev1.EvidenceItem, n)
	for i := range items {
		items[i] = maximalItem(label, i)
	}
	msg := batch(label, "max", items...)
	msg.BatchId = maxID(prefix(label) + "b-")
	msg.Epoch = uint64(genesis.SafeIntegerMax)
	msg.AuditorSignature.KeyId = maxID("key-")
	return msg
}

func buildVectors(t testing.TB) []signVector {
	t.Helper()
	unknownKind := vectorItem(auditor1, "k4", "l1", "c1")
	unknownKind.Kind = 4
	thirtyTwo := make([]*evidencev1.EvidenceItem, 32)
	for i := range thirtyTwo {
		thirtyTwo[i] = vectorItem(auditor1, fmt.Sprintf("e%02d", i), fmt.Sprintf("l%d", i%3+1), fmt.Sprintf("c%d", i%3+1))
	}
	inputs := []struct {
		name  string
		label string
		msg   *evidencev1.MsgSubmitAuditBatch
	}{
		{"one-item", auditor1, batch(auditor1, "1", vectorItem(auditor1, "e1", "l1", "c1"))},
		{"two-items", auditor1, batch(auditor1, "2", vectorItem(auditor1, "e1", "l1", "c1"), vectorItem(auditor1, "e2", "l2", "c2"))},
		{"thirty-two-items", auditor1, batch(auditor1, "32", thirtyTwo...)},
		{"unknown-kind-number", auditor1, batch(auditor1, "k", unknownKind)},
		{"maximal-ids", auditor1, maximalBatch(auditor1, 1)},
		{"second-auditor", auditor2, batch(auditor2, "1", vectorItem(auditor2, "e1", "l1", "c1"))},
	}
	var out []signVector
	for _, in := range inputs {
		canonical, err := evidence.AuditCanonical(in.msg)
		if err != nil {
			t.Fatalf("%s: %v", in.name, err)
		}
		signBytes, err := evidence.AuditSignBytes(in.msg)
		if err != nil {
			t.Fatalf("%s: %v", in.name, err)
		}
		if !bytes.Equal(signBytes, append([]byte("SVN-EVIDENCE-ATTESTATION-1\x00"), canonical...)) {
			t.Fatalf("%s: sign bytes are not domain || 0x00 || canonical", in.name)
		}
		sign(in.msg, in.label)
		if code := evidence.VerifyAuditSignature(in.msg); code != msgkit.OK {
			t.Fatalf("%s: signature does not verify: %s", in.name, code)
		}
		sum := sha256.Sum256(signBytes)
		out = append(out, signVector{
			Name: in.name, KeyLabel: in.label, PublicKey: hex.EncodeToString(pub(in.label)),
			Signer: hex.EncodeToString(in.msg.GetSigner()), Canonical: canonical,
			SignBytes: hex.EncodeToString(sum[:]), Signature: hex.EncodeToString(in.msg.GetAuditorSignature().GetSignature()),
		})
	}
	return out
}

// TestSignBytesVectors pins the SVN-EVIDENCE-ATTESTATION-1 batch payload
// (owner ruling on Q2) with golden vectors. The producer imports the same
// builders. Run with -update to rewrite the file after an intended payload
// change.
func TestSignBytesVectors(t *testing.T) {
	got := vectorFile{
		Comment: "Golden x/evidence SVN-EVIDENCE-ATTESTATION-1 vectors (owner ruling on Q2, DR-09-02). Signed bytes are " +
			"UTF-8(domain) || 0x00 || canonical. TEST-ONLY keys: ed25519 seed = sha256(\"svn-evidence-test-seed:\" + key_label); " +
			"no private key is stored. signer = first 20 bytes of sha256(public_key). Synthetic values only.",
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
	for _, v := range got.Vectors {
		if bytes.Contains(raw, []byte(hex.EncodeToString(testKey(v.KeyLabel)))) ||
			bytes.Contains(raw, []byte(hex.EncodeToString(testKey(v.KeyLabel).Seed()))) {
			t.Fatalf("vector file holds the private key of %s", v.KeyLabel)
		}
	}
	// The one-item payload has exactly the three batch keys and the 13
	// envelope keys.
	var payload struct {
		BatchID   string           `json:"batch_id"`
		Envelopes []map[string]any `json:"envelopes"`
		Signer    string           `json:"signer"`
	}
	dec = json.NewDecoder(strings.NewReader(want.Vectors[0].Canonical))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Envelopes) != 1 || len(payload.Envelopes[0]) != 13 || payload.Signer != want.Vectors[0].Signer {
		t.Fatalf("one-item payload %+v", payload)
	}
	if !strings.Contains(want.Vectors[3].Canonical, `"kind":"4"`) {
		t.Fatalf("unknown kind vector %s", want.Vectors[3].Canonical)
	}
}

// TestSignPayloadNeverFails: every value whose integers are in the safe
// range builds a payload (the handler rejects it at its own check later);
// an integer outside the safe range, a nil message, item, or signature, or
// an invalid digest returns ErrNoCanonicalForm and never panics.
func TestSignPayloadNeverFails(t *testing.T) {
	base := func() *evidencev1.MsgSubmitAuditBatch {
		return batch(auditor1, "x", vectorItem(auditor1, "e1", "l1", "c1"))
	}
	builds := map[string]func(*evidencev1.MsgSubmitAuditBatch){
		"kind-0":              func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].Kind = 0 },
		"kind-3":              func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].Kind = 3 },
		"kind-5":              func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].Kind = 5 },
		"kind-negative":       func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].Kind = -7 },
		"expiry-negative":     func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ExpiryUnixSeconds = -5 },
		"expiry-0":            func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ExpiryUnixSeconds = 0 },
		"attestation-false":   func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].Attestation = false },
		"content-version-0":   func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ContentVersion = 0 },
		"content-version-max": func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ContentVersion = uint64(genesis.SafeIntegerMax) },
		"schema-0":            func(m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 0 },
		"no-items":            func(m *evidencev1.MsgSubmitAuditBatch) { m.Items = nil },
		"signer-empty":        func(m *evidencev1.MsgSubmitAuditBatch) { m.Signer = nil },
	}
	for name, edit := range builds {
		msg := base()
		edit(msg)
		if _, err := evidence.AuditSignBytes(msg); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	canonical, err := evidence.AuditCanonical(func() *evidencev1.MsgSubmitAuditBatch {
		m := base()
		m.Items[0].Kind = 4
		m.Items[0].ExpiryUnixSeconds = -5
		return m
	}())
	if err != nil || !strings.Contains(canonical, `"kind":"4"`) || !strings.Contains(canonical, `"expiry_unix_seconds":-5`) {
		t.Fatalf("payload %s %v", canonical, err)
	}
	fails := map[string]func(*evidencev1.MsgSubmitAuditBatch){
		"content-version-2^53":       func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ContentVersion = 1 << 53 },
		"content-version-2^63":       func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ContentVersion = 1 << 63 },
		"content-version-max-uint64": func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ContentVersion = math.MaxUint64 },
		"epoch-2^53":                 func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = 1 << 53 },
		"epoch-max-uint64":           func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = math.MaxUint64 },
		"expiry-min-int64":           func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ExpiryUnixSeconds = math.MinInt64 },
		"expiry-2^53":                func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ExpiryUnixSeconds = 1 << 53 },
		"nil-item":                   func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0] = nil },
		"nil-signature":              func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature = nil },
		"nil-digest":                 func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ObservationDigest = nil },
		"digest-31-bytes":            func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ObservationDigest.Value = make([]byte, 31) },
		"digest-algorithm-0":         func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ObservationDigest.Algorithm = 0 },
	}
	for name, edit := range fails {
		msg := base()
		edit(msg)
		if _, err := evidence.AuditSignBytes(msg); !errors.Is(err, evidence.ErrNoCanonicalForm) {
			t.Fatalf("%s: error %v, want ErrNoCanonicalForm", name, err)
		}
		if _, err := evidence.AuditEnvelope(msg, 0); !errors.Is(err, evidence.ErrNoCanonicalForm) {
			t.Fatalf("%s: envelope error %v", name, err)
		}
	}
	if _, err := evidence.AuditCanonical(nil); !errors.Is(err, evidence.ErrNoCanonicalForm) {
		t.Fatal("nil message has a payload")
	}
	for _, i := range []int{-1, 1} {
		if _, err := evidence.AuditEnvelope(base(), i); !errors.Is(err, evidence.ErrNoCanonicalForm) {
			t.Fatalf("index %d: %v", i, err)
		}
	}
	// VerifyAuditSignature: a non-canonical payload after a valid structure
	// is UNAUTHORIZED; a nil signature is MALFORMED; a nil message never
	// panics.
	msg := sign(base(), auditor1)
	msg.Items[0].ContentVersion = 1 << 63
	if code := evidence.VerifyAuditSignature(msg); code != msgkit.Unauthorized {
		t.Fatalf("non-canonical payload: %s", code)
	}
	msg.AuditorSignature = nil
	if code := evidence.VerifyAuditSignature(msg); code != msgkit.Malformed {
		t.Fatalf("nil signature: %s", code)
	}
	if code := evidence.VerifyAuditSignature(nil); code != msgkit.Malformed {
		t.Fatalf("nil message: %s", code)
	}
}

// TestAttestationPayloadRejectsInvalidUTF8: a signed string (batch_id,
// evidence_id, lease_id, content_id, key_id) that is not valid UTF-8 has no
// SVN-JCS-1 form (ErrNoCanonicalForm). Without this rule the encoder writes
// each invalid byte as U+FFFD, so a signature over "c1\uFFFD" would also
// verify for "c1\xff" and "c1\xfe": ids never signed. VerifyAuditSignature
// (the producer's and the reviewer's check) and the handler answer
// UNAUTHORIZED. A transaction cannot carry such a string (protobuf-go
// refuses it); the rule keeps the exported builders injective.
func TestAttestationPayloadRejectsInvalidUTF8(t *testing.T) {
	base := func() *evidencev1.MsgSubmitAuditBatch {
		return batch(auditor1, "u", vectorItem(auditor1, "e1", "l1", "c1"))
	}
	for name, edit := range map[string]func(*evidencev1.MsgSubmitAuditBatch){
		"batch_id":    func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId += "\xff" },
		"evidence_id": func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].EvidenceId += "\xff" },
		"lease_id":    func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].LeaseId += "\xfe" },
		"content_id":  func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0].ContentId += "\xc3" },
		"key_id":      func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.KeyId += "\xff" },
	} {
		msg := base()
		edit(msg)
		if _, err := evidence.AuditSignBytes(msg); !errors.Is(err, evidence.ErrNoCanonicalForm) {
			t.Fatalf("%s: sign bytes error %v", name, err)
		}
		if name != "batch_id" {
			if _, err := evidence.AuditEnvelope(msg, 0); !errors.Is(err, evidence.ErrNoCanonicalForm) {
				t.Fatalf("%s: envelope error %v", name, err)
			}
		}
	}
	// The U+FFFD spelling is valid UTF-8 and is signed; the invalid
	// spellings that the encoder would turn into it do not verify.
	signedMsg := base()
	signedMsg.Items[0].ContentId = "c1\uFFFD"
	sign(signedMsg, auditor1)
	if code := evidence.VerifyAuditSignature(signedMsg); code != msgkit.OK {
		t.Fatalf("U+FFFD spelling: %s", code)
	}
	e := newEnv(t)
	for _, invalid := range []string{"c1\xff", "c1\xfe"} {
		altered := proto.Clone(signedMsg).(*evidencev1.MsgSubmitAuditBatch)
		altered.Items[0].ContentId = invalid
		if code := evidence.VerifyAuditSignature(altered); code != msgkit.Unauthorized {
			t.Fatalf("%q: VerifyAuditSignature %s", invalid, code)
		}
		e.requireReject(e.submit(e.fresh(), altered), msgkit.Unauthorized, fmt.Sprintf("content id %q", invalid))
	}
}

// smallestOversizedBatch returns the smallest item count whose canonical
// payload (maximal items) exceeds 65536 bytes.
func smallestOversizedBatch(t testing.TB, label string) (*evidencev1.MsgSubmitAuditBatch, int) {
	t.Helper()
	for n := 33; n <= 200; n++ {
		msg := maximalBatch(label, n)
		if _, err := evidence.AuditCanonical(msg); errors.Is(err, evidence.ErrPayloadTooLarge) {
			return msg, n
		}
	}
	t.Fatal("no oversized batch up to 200 items")
	return nil, 0
}

// TestSignPayloadSizeBound: 32 maximal items stay below the 65536-byte
// SVN-JCS-1 cap; the smallest item count above it is BOUNDS at the payload
// step while its transaction stays below 65536 bytes. Sizes are logged.
func TestSignPayloadSizeBound(t *testing.T) {
	canonical, err := evidence.AuditCanonical(maximalBatch(auditor1, 32))
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) >= 65536 {
		t.Fatalf("32 maximal items: %d canonical bytes", len(canonical))
	}
	t.Logf("32 maximal items: %d canonical payload bytes (cap 65536)", len(canonical))
	msg, n := smallestOversizedBatch(t, auditor1)
	sign(msg, auditor1)
	size := proto.Size(msg)
	if size >= 65536 {
		t.Fatalf("%d-item message is %d bytes; it does not fit a transaction", n, size)
	}
	if code := evidence.VerifyAuditSignature(msg); code != msgkit.Bounds {
		t.Fatalf("oversized payload: %s, want BOUNDS", code)
	}
	smaller := maximalBatch(auditor1, n-1)
	if _, err := evidence.AuditCanonical(smaller); err != nil {
		t.Fatalf("%d items: %v", n-1, err)
	}
	t.Logf("smallest oversized batch: %d maximal items; encoded message %d bytes", n, size)
}

// TestAttestationPreimageIsNotASignDoc: the app enables only
// SIGN_MODE_DIRECT (chain/app/encoding.go), whose signed bytes are a
// deterministic protobuf SignDoc. Its first byte is a field tag of fields
// 1-4 (0x0a, 0x12, 0x1a, 0x20); an attestation preimage starts with "S"
// (0x53) and is never "{" (amino JSON). The preimage does not decode as a
// strict SignDoc. The separation argument is unreviewed (owner ruling on
// Q1: devnet beta only).
func TestAttestationPreimageIsNotASignDoc(t *testing.T) {
	e := newEnv(t)
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"))
	preimage, err := evidence.AuditSignBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(preimage, []byte("SVN-EVIDENCE-ATTESTATION-1\x00{")) || preimage[0] != 0x53 {
		t.Fatalf("preimage starts %q", preimage[:32])
	}
	signDocFirst := map[byte]bool{0x0a: true, 0x12: true, 0x1a: true, 0x20: true}
	if signDocFirst[preimage[0]] || preimage[0] == '{' {
		t.Fatalf("preimage first byte %#x", preimage[0])
	}
	doc := &txtypes.SignDoc{BodyBytes: []byte{1}, AuthInfoBytes: []byte{2}, ChainId: msgkittest.ChainID, AccountNumber: 7}
	raw, err := doc.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if raw[0] != 0x0a {
		t.Fatalf("SignDoc encoding starts %#x", raw[0])
	}
	var decoded txtypes.SignDoc
	unmarshalErr := decoded.Unmarshal(preimage)
	strictErr := unknownproto.RejectUnknownFieldsStrict(preimage, &txtypes.SignDoc{}, nil)
	if unmarshalErr == nil && strictErr == nil {
		t.Fatal("the attestation preimage decodes as a strict SignDoc")
	}
	// The other direction: a SignDoc never verifies as an attestation,
	// whose verifier always prepends the domain.
	sig := ed25519.Sign(testKey(auditor1), raw)
	if msgkit.VerifyWithKey(pub(auditor1), typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION, string(raw), sig) {
		t.Fatal("a SignDoc signature verified as an attestation")
	}
}

// TestAuditorAddressMatchesSDK: AuditorAddress equals the SDK ed25519
// PubKey.Address (the account address of an ed25519 account key); other key
// sizes have no address; IDPrefix is 16 hex characters and a dot.
func TestAuditorAddressMatchesSDK(t *testing.T) {
	keys := [][]byte{pub(auditor1), pub(auditor2), sdked.GenPrivKey().PubKey().Bytes()}
	for _, key := range keys {
		sdkAddr := (&sdked.PubKey{Key: key}).Address()
		if !bytes.Equal(evidence.AuditorAddress(key), sdkAddr) {
			t.Fatalf("address %x, SDK %x", evidence.AuditorAddress(key), sdkAddr)
		}
		p := evidence.IDPrefix(sdkAddr)
		if p != hex.EncodeToString(sdkAddr[:8])+"." || len(p) != 17 || !msgkit.ValidID(p) {
			t.Fatalf("prefix %q", p)
		}
	}
	if evidence.AuditorAddress(make([]byte, 31)) != nil || evidence.AuditorAddress(nil) != nil {
		t.Fatal("a 31-byte key has an address")
	}
	if evidence.IDPrefix(make([]byte, 19)) != "" || evidence.IDPrefix(nil) != "" {
		t.Fatal("a 19-byte address has a prefix")
	}
	if prefix(auditor1) == prefix(auditor2) {
		t.Fatal("the two test auditors share a prefix")
	}
}

// TestKindName: named kinds lose the EVIDENCE_KIND_ prefix; the reserved
// numbers 3-5 and any other number are decimal strings.
func TestKindName(t *testing.T) {
	for kind, name := range map[evidencev1.EvidenceKind]string{
		0: "UNSPECIFIED", 1: "PROVIDER_RECEIPT", 2: "AUDITOR_ATTESTATION", 3: "3", 4: "4", 5: "5", 99: "99", -1: "-1",
	} {
		if got := evidence.KindName(kind); got != name {
			t.Fatalf("kind %d: %q, want %q", kind, got, name)
		}
	}
}

// TestEpochAt: floor(t / 3600) at both sides of a boundary; times below 1
// are epoch 0.
func TestEpochAt(t *testing.T) {
	cases := map[int64]uint64{
		math.MinInt64: 0, -1: 0, 0: 0, 1: 0, 3599: 0, 3600: 1, 7199: 1, 7200: 2,
		t0 - 1: uint64(t0/3600) - 1, t0: uint64(t0 / 3600), now: uint64(t0 / 3600),
		genesis.UnixSecondsMax: uint64(genesis.UnixSecondsMax / 3600),
	}
	for unix, want := range cases {
		if got := evidence.EpochAt(unix); got != want {
			t.Fatalf("EpochAt(%d) = %d, want %d", unix, got, want)
		}
	}
	if t0%evidence.EpochSeconds != 0 {
		t.Fatal("t0 is not an epoch boundary")
	}
}

// envelopeSchemaPath is docs/protocol/schemas/evidence-envelope.schema.json.
func envelopeSchemaPath(t testing.TB) string {
	return filepath.Join(filepath.Dir(msgkittest.BoundsFile(t)), "schemas", "evidence-envelope.schema.json")
}

type schemaProperty struct {
	Type      string   `json:"type"`
	Const     any      `json:"const"`
	Enum      []string `json:"enum"`
	MinLength *int     `json:"minLength"`
	MaxLength *int     `json:"maxLength"`
	Pattern   string   `json:"pattern"`
	Minimum   *int64   `json:"minimum"`
	Maximum   *int64   `json:"maximum"`
}

type envelopeSchema struct {
	Schema               string                    `json:"$schema"`
	ID                   string                    `json:"$id"`
	Title                string                    `json:"title"`
	Description          string                    `json:"description"`
	Type                 string                    `json:"type"`
	AdditionalProperties bool                      `json:"additionalProperties"`
	Required             []string                  `json:"required"`
	Properties           map[string]schemaProperty `json:"properties"`
}

// validateEnvelope checks one envelope text against the schema's keywords
// (the schema decodes strictly, so a keyword this checker does not know
// fails the test). It returns the first violation.
func validateEnvelope(schema envelopeSchema, text string) error {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var envelope map[string]any
	if err := dec.Decode(&envelope); err != nil {
		return err
	}
	for _, key := range schema.Required {
		if _, ok := envelope[key]; !ok {
			return fmt.Errorf("missing %s", key)
		}
	}
	keys := make([]string, 0, len(envelope))
	for key := range envelope {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		prop, ok := schema.Properties[key]
		if !ok {
			return fmt.Errorf("additional property %s", key)
		}
		value := envelope[key]
		if prop.Const != nil {
			want := fmt.Sprint(prop.Const)
			if fmt.Sprint(value) != want {
				return fmt.Errorf("%s = %v, const %v", key, value, want)
			}
		}
		if len(prop.Enum) > 0 {
			found := false
			for _, option := range prop.Enum {
				found = found || value == option
			}
			if !found {
				return fmt.Errorf("%s = %v not in %v", key, value, prop.Enum)
			}
		}
		switch prop.Type {
		case "string":
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s is not a string", key)
			}
			if (prop.MinLength != nil && len(s) < *prop.MinLength) || (prop.MaxLength != nil && len(s) > *prop.MaxLength) {
				return fmt.Errorf("%s length %d", key, len(s))
			}
			if prop.Pattern != "" && !regexp.MustCompile(prop.Pattern).MatchString(s) {
				return fmt.Errorf("%s = %q off pattern", key, s)
			}
		case "integer":
			n, ok := value.(json.Number)
			if !ok {
				return fmt.Errorf("%s is not a number", key)
			}
			v, err := n.Int64()
			if err != nil {
				return fmt.Errorf("%s = %s is not an integer", key, n)
			}
			if (prop.Minimum != nil && v < *prop.Minimum) || (prop.Maximum != nil && v > *prop.Maximum) {
				return fmt.Errorf("%s = %d out of range", key, v)
			}
		case "":
		default:
			return fmt.Errorf("%s has unchecked type %s", key, prop.Type)
		}
	}
	return nil
}

// TestEnvelopeMatchesSchema: every AuditEnvelope of a valid batch has
// exactly the 13 keys and value constraints of the frozen
// svn-evidence-envelope-v1 schema; an envelope of a reserved kind or of
// attestation false does not (the checker rejects them). The Python
// jsonschema validation of the producer's golden envelopes is
// tests/unit/test_evidence_envelopes.py (package P3a).
func TestEnvelopeMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile(envelopeSchemaPath(t))
	if err != nil {
		t.Fatal(err)
	}
	var schema envelopeSchema
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&schema); err != nil {
		t.Fatalf("schema has a keyword this checker does not read: %v", err)
	}
	if schema.AdditionalProperties || len(schema.Required) != 13 || schema.ID != "svn-evidence-envelope-v1" {
		t.Fatalf("schema %+v", schema)
	}
	e := newEnv(t)
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2"))
	msg.Items[1].ObservationDigest = digestOf("other")
	for i := range msg.GetItems() {
		envelope, err := evidence.AuditEnvelope(msg, i)
		if err != nil {
			t.Fatal(err)
		}
		text, err := msgkit.Canonical(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateEnvelope(schema, text); err != nil {
			t.Fatalf("envelope %d %s: %v", i, text, err)
		}
		if len(envelope) != len(schema.Required) {
			t.Fatalf("envelope %d has %d keys", i, len(envelope))
		}
	}
	maximal, err := evidence.AuditEnvelope(maximalBatch(auditor1, 1), 0)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := msgkit.Canonical(maximal)
	if err := validateEnvelope(schema, text); err != nil {
		t.Fatalf("maximal envelope: %v", err)
	}
	for name, edit := range map[string]func(*evidencev1.EvidenceItem){
		"kind-4":            func(it *evidencev1.EvidenceItem) { it.Kind = 4 },
		"attestation-false": func(it *evidencev1.EvidenceItem) { it.Attestation = false },
		"content-version-0": func(it *evidencev1.EvidenceItem) { it.ContentVersion = 0 },
		"expiry-0":          func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = 0 },
	} {
		bad := batch(auditor1, "bad", e.item(auditor1, "e1", "l1"))
		edit(bad.Items[0])
		envelope, err := evidence.AuditEnvelope(bad, 0)
		if err != nil {
			t.Fatal(err)
		}
		text, _ := msgkit.Canonical(envelope)
		if validateEnvelope(schema, text) == nil {
			t.Fatalf("%s: schema accepted %s", name, text)
		}
	}
}

// BenchmarkVerifyAuditSignature measures steps 6-8 (structure, binding,
// canonical payload, ed25519) for 1 and 32 items. One machine; not a
// throughput figure; gas is not CPU time.
func BenchmarkVerifyAuditSignature(b *testing.B) {
	for _, n := range []int{1, 32} {
		items := make([]*evidencev1.EvidenceItem, n)
		for i := range items {
			items[i] = vectorItem(auditor1, fmt.Sprintf("e%02d", i), "l1", "c1")
		}
		msg := signed(auditor1, "bench", items...)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if code := evidence.VerifyAuditSignature(msg); code != msgkit.OK {
					b.Fatal(code)
				}
			}
		})
	}
}
