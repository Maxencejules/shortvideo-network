package devnet_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The audit demo runs the real off-chain producer (agents/auditor, built
// with go build) against four svnd processes on one machine and a loopback
// HTTPS test endpoint. Labels printed in the log:
const (
	labelEndpoint = "loopback test endpoint, synthetic package, not a provider agent"
	labelAuditors = "auditor-1 and auditor-2 are one operator on one machine; not independent auditors"
	evidencePath  = "/svn.evidence.v1.Query/Evidence"
	// auditGuard and auditInclusion are the producer's epoch guard and
	// inclusion timeout in this demo (the configuration's lower bounds are
	// 30 and 5 seconds).
	auditGuard     = int64(30)
	auditInclusion = int64(20)
)

// auditSegments are the segment sizes of every synthetic package.
var auditSegments = []int{3000, 5000, 2000, 4000}

// auditPackage is one synthetic package: deterministic bytes (a SHA-256
// counter stream keyed by the content id; nothing is media) and its
// svn-media-manifest-v1 in SVN-JCS-1. The content record, the offer, and
// the lease carry SHA-256 of the manifest text, which the producer checks
// before it fetches anything.
type auditPackage struct {
	contentID string
	bytes     []byte
	manifest  []byte
	digest    []byte
}

func newAuditPackage(t *testing.T, contentID string) auditPackage {
	t.Helper()
	var pkg []byte
	var segments []any
	counter := 0
	for i, size := range auditSegments {
		data := make([]byte, 0, size)
		for len(data) < size {
			block := sha256.Sum256([]byte(fmt.Sprintf("svn-audit-demo:%s/%d", contentID, counter)))
			counter++
			data = append(data, block[:min(len(block), size-len(data))]...)
		}
		sum := sha256.Sum256(data)
		segments = append(segments, map[string]any{
			"segment_id": fmt.Sprintf("s%04d", i), "sha256": hex.EncodeToString(sum[:]), "byte_count": strconv.Itoa(size),
		})
		pkg = append(pkg, data...)
	}
	text, err := jcs.Canonicalize(map[string]any{
		"schema_version": int64(1), "profile": "SVN-JCS-1", "manifest_id": "m-" + contentID,
		"content_id": contentID, "content_version": int64(1), "package_bytes": strconv.Itoa(len(pkg)),
		"renditions": []any{map[string]any{"rendition_id": "r0", "codec": "avc1", "segments": segments}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(text))
	return auditPackage{contentID: contentID, bytes: pkg, manifest: []byte(text), digest: sum[:]}
}

func (p auditPackage) size() uint64 { return uint64(len(p.bytes)) }

// testCA is a certificate authority generated at run time; its key stays
// in memory.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "svn devnet audit test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (ca testCA) leaf(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 1), Subject: pkix.Name{CommonName: "svn devnet audit test endpoint"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// auditEndpoint is the loopback HTTPS test endpoint. It serves GET
// /packages/{content_id}/1 from served (a missing content id is 404) and
// counts requests.
type auditEndpoint struct {
	port     uint16
	requests atomic.Int64
}

func startAuditEndpoint(t *testing.T, ca testCA, served map[string][]byte) *auditEndpoint {
	t.Helper()
	ep := &auditEndpoint{}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ep.requests.Add(1)
		id, ok := strings.CutPrefix(r.URL.Path, "/packages/")
		id, ok2 := strings.CutSuffix(id, "/1")
		body, found := served[id]
		if r.Method != http.MethodGet || !ok || !ok2 || !found {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{ca.leaf(t)}, MinVersion: tls.VersionTLS12}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	ep.port = uint16(p)
	return ep
}

// closedPort is a loopback port with no listener: a connection to it is
// refused (a network failure, never a provider answer).
func closedPort(t *testing.T) uint16 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(l.Addr().String())
	_ = l.Close()
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	return uint16(p)
}

// buildAuditor builds the producer binary from agents/auditor into a
// temporary directory. A build failure fails the test; it never skips.
func buildAuditor(t *testing.T) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go tool: %v", err)
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "agents", "auditor"))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "auditor")
	start := time.Now()
	cmd := exec.Command(goTool, "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build agents/auditor: %v\n%s", err, out)
	}
	fmt.Printf("phase=build-auditor seconds=%.1f\n", time.Since(start).Seconds())
	return bin
}

// auditorCLI runs the producer binary and returns its exit code and its
// stdout lines (one JSON object each).
func auditorCLI(t *testing.T, bin string, args ...string) (int, []json.RawMessage) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("auditor %v: %v", args, err)
	}
	var lines []json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			t.Fatalf("auditor %s: stdout line is not JSON: %q", args[0], line)
		}
		lines = append(lines, json.RawMessage(line))
	}
	fmt.Printf("phase=auditor-%s exit=%d lines=%d stderr=%q\n", args[0], code, len(lines), strings.TrimSpace(stderr.String()))
	return code, lines
}

func decodeLine(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
}

// The producer's output objects, as far as the test reads them.
type runLine struct {
	RunID      string `json:"run_id"`
	HeaderTime int64  `json:"header_unix_seconds"`
	Epoch      int64  `json:"epoch"`
	Audits     []struct {
		LeaseID      string   `json:"lease_id"`
		Result       string   `json:"result"`
		Reasons      []string `json:"reasons"`
		ReportSHA256 string   `json:"report_sha256"`
	} `json:"audits"`
}

type signLine struct {
	BatchID     string   `json:"batch_id"`
	ProviderID  string   `json:"provider_id"`
	Epoch       uint64   `json:"epoch"`
	BusinessID  string   `json:"business_id"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type submitLine struct {
	BatchID   string `json:"batch_id"`
	State     string `json:"state"`
	Codespace string `json:"codespace"`
	Code      uint32 `json:"code"`
	TxHash    string `json:"tx_hash"`
	Height    int64  `json:"height"`
	Ack       *struct {
		Code      uint32
		Codespace string
	} `json:"ack"`
	Items []struct {
		EvidenceID string `json:"evidence_id"`
		State      string `json:"state"`
	} `json:"items"`
	Warnings []string `json:"warnings"`
	Label    string   `json:"label"`
}

type verifyLine struct {
	RecordSource         string   `json:"record_source"`
	EvidenceID           string   `json:"evidence_id"`
	Verdict              string   `json:"verdict"`
	State                string   `json:"state"`
	ReportResult         string   `json:"report_result"`
	ReportEndpointSource string   `json:"report_endpoint_source"`
	ClaimLimit           string   `json:"claim_limit"`
	Limitations          []string `json:"limitations"`
}

// auditConfig is the producer's operator configuration file.
type auditConfig struct {
	ChainRPC       string       `json:"chain_rpc"`
	CAFile         string       `json:"ca_file"`
	StoreRoot      string       `json:"store_root"`
	Allowlist      []auditEntry `json:"allowlist"`
	Audits         []auditSpec  `json:"audits"`
	AuditorAddress string       `json:"auditor_address"`
	KeyPath        string       `json:"key_path"`
	KeyID          string       `json:"key_id"`
	Lifetime       int64        `json:"evidence_lifetime_seconds"`
	Guard          int64        `json:"epoch_guard_seconds"`
	Inclusion      int64        `json:"inclusion_timeout_seconds"`
}

type auditEntry struct {
	ProviderID string `json:"provider_id"`
	Host       string `json:"host"`
	IP         string `json:"ip"`
	Port       uint16 `json:"port"`
}

// auditSpec is one configured audit: exactly one of CapabilityManifestPath
// and Endpoints is set.
type auditSpec struct {
	LeaseID                string     `json:"lease_id"`
	ProviderID             string     `json:"provider_id"`
	ContentID              string     `json:"content_id"`
	EndpointIndex          int        `json:"endpoint_index"`
	ManifestPath           string     `json:"manifest_path"`
	CapabilityManifestPath string     `json:"capability_manifest_path,omitempty"`
	Endpoints              []auditURL `json:"endpoints,omitempty"`
	Mode                   string     `json:"mode"`
}

type auditURL struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func writeAuditConfig(t *testing.T, path string, cfg auditConfig) string {
	t.Helper()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// auditCapability writes the capability manifest of providerID that lists
// the given test endpoints in order (svn-provider-capability-v1 in SVN-JCS-1,
// naming the provider's current signing key id) into dir and returns its
// digest, which the provider record commits, and the file's path, which the
// producer configuration names. The producer takes an endpoint from it only
// when it hashes to the committed digest, and only such an endpoint can give
// a FAIL (interim choice N2).
func (d *demo) auditCapability(dir, providerID string, capacity uint64, endpoints ...capability.Endpoint) (*typesv1.Digest, string) {
	d.t.Helper()
	result, err := capability.Encode(capability.Manifest{
		ProviderID: providerID, SigningKeyID: d.keys[providerID].id, DeclaredCapacityBytes: capacity, Endpoints: endpoints,
	})
	if err != nil {
		d.t.Fatal(err)
	}
	path := filepath.Join(dir, providerID+".capability.json")
	if err := os.WriteFile(path, []byte(result.Canonical), 0o600); err != nil {
		d.t.Fatal(err)
	}
	return result.Digest(), path
}

// registerPackage registers content whose manifest digest and package bytes
// are the synthetic package's.
func (d *demo) registerPackage(who loaded, bid string, p auditPackage) *contentv1.MsgRegisterContent {
	d.t.Helper()
	signer := who.key.PubKey().Address()
	signBytes, err := content.RegisterIntentSignBytes(signer, p.contentID, 1, p.digest, p.size(), content.DevnetRoleName)
	if err != nil {
		d.t.Fatal(err)
	}
	return &contentv1.MsgRegisterContent{
		Signer: signer, BusinessId: bid, ContentId: p.contentID, ContentVersion: 1,
		ManifestDigest: sha256Digest(p.digest), PackageBytes: p.size(),
		HostingIntent: signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, d.intent, signBytes),
	}
}

func (d *demo) packageOffer(bid, offerID, providerID string, p auditPackage, deadline, expiry int64) *leasesv1.MsgOfferLease {
	return &leasesv1.MsgOfferLease{
		Signer: d.service.key.PubKey().Address(), BusinessId: bid, OfferId: offerID,
		ContentId: p.contentID, ContentVersion: 1, ProviderId: providerID,
		ManifestDigest: sha256Digest(p.digest), PackageBytes: p.size(), TermsDigest: leaseTerms(offerID),
		StartUnixSeconds: deadline - devnetTerm, DeadlineUnixSeconds: deadline, ExpiryUnixSeconds: expiry, TermVersion: 1,
	}
}

// renewNaming is a provider-signed renewal of leaseID to term 2 that names
// evidenceID (x/leases renew check 13).
func (d *demo) renewNaming(who loaded, bid, leaseID, providerID string, p auditPackage, deadline int64, evidenceID string) *leasesv1.MsgRenewLease {
	d.t.Helper()
	manifest := sha256Digest(p.digest)
	signBytes, err := leases.RenewSignBytes(leaseID, 2, deadline, manifest, p.size(), evidenceID)
	if err != nil {
		d.t.Fatal(err)
	}
	return &leasesv1.MsgRenewLease{
		Signer: who.key.PubKey().Address(), BusinessId: bid, LeaseId: leaseID, TermVersion: 2,
		DeadlineUnixSeconds: deadline, ManifestDigest: manifest, PackageBytes: p.size(), EvidenceId: evidenceID,
		ProviderSignature: signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, d.keys[providerID], signBytes),
	}
}

// edPrivate is an ed25519 account's private key (the devnet key file
// holds the SDK form: seed, then public key).
func edPrivate(t *testing.T, who loaded) ed25519.PrivateKey {
	t.Helper()
	key, ok := who.key.(*sdked.PrivKey)
	if !ok || len(key.Key) != ed25519.PrivateKeySize {
		t.Fatalf("%s has no ed25519 account key", who.name)
	}
	return ed25519.PrivateKey(key.Key)
}

// auditItem is one AUDITOR_ATTESTATION item of content version 1.
func auditItem(evidenceID, leaseID, contentID string, observation []byte, expiry int64) *evidencev1.EvidenceItem {
	return &evidencev1.EvidenceItem{
		EvidenceId: evidenceID, Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		LeaseId: leaseID, ContentId: contentID, ContentVersion: 1, ObservationDigest: sha256Digest(observation),
		ExpiryUnixSeconds: expiry, Attestation: true,
	}
}

// signedBatch is a batch sent by who and signed by who's account key (owner
// ruling on Q1), built by test code, never by a producer option.
func signedBatch(t *testing.T, who loaded, bid, batchID string, epoch uint64, items ...*evidencev1.EvidenceItem) *evidencev1.MsgSubmitAuditBatch {
	t.Helper()
	priv := edPrivate(t, who)
	msg := &evidencev1.MsgSubmitAuditBatch{
		Signer: who.key.PubKey().Address(), BusinessId: bid, BatchId: batchID, Epoch: epoch,
		SchemaVersion: evidence.SchemaVersion, Items: items,
		AuditorSignature: &typesv1.InnerSignature{
			Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION, PublicKey: append([]byte(nil), priv.Public().(ed25519.PublicKey)...),
			KeyId: "key-" + who.name, CanonicalProfileVersion: 1,
		},
	}
	return resign(t, msg, priv)
}

func resign(t *testing.T, msg *evidencev1.MsgSubmitAuditBatch, priv ed25519.PrivateKey) *evidencev1.MsgSubmitAuditBatch {
	t.Helper()
	msg.AuditorSignature.Signature = nil
	signBytes, err := evidence.AuditSignBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	msg.AuditorSignature.Signature = ed25519.Sign(priv, signBytes)
	if code := evidence.VerifyAuditSignature(msg); code != msgkit.OK {
		t.Fatalf("test batch does not verify: %s", code)
	}
	return msg
}

func idPrefix(who loaded) string { return evidence.IDPrefix(who.key.PubKey().Address()) }

// syncSequence reads who's committed account sequence from node v1. The
// producer signs with auditor-1's account too, so the test's own sequence
// count for it is refreshed after the producer has submitted.
func (d *demo) syncSequence(who loaded) {
	d.t.Helper()
	data, err := gogoproto.Marshal(&authtypes.QueryAccountRequest{Address: sdk.AccAddress(who.key.PubKey().Address()).String()})
	if err != nil {
		d.t.Fatal(err)
	}
	res, err := d.clients[0].ABCIQueryWithOptions(context.Background(), "/cosmos.auth.v1beta1.Query/Account", data, rpcclient.ABCIQueryOptions{})
	if err != nil || res.Response.Code != 0 {
		d.t.Fatalf("account %s: %v %v", who.name, err, res)
	}
	var resp authtypes.QueryAccountResponse
	if err := d.enc.Codec.Unmarshal(res.Response.Value, &resp); err != nil {
		d.t.Fatal(err)
	}
	var account sdk.AccountI
	if err := d.enc.InterfaceRegistry.UnpackAny(resp.Account, &account); err != nil || account == nil {
		d.t.Fatalf("account %s: %v", who.name, err)
	}
	d.seq[who.addr] = account.GetSequence()
}

// evidenceAt reads Query/Evidence for one id (or one lease) on client at
// height (0: latest).
func (d *demo) evidenceAt(req *evidencev1.QueryEvidenceRequest, height int64) []*evidencev1.EvidenceRecord {
	d.t.Helper()
	var res evidencev1.QueryEvidenceResponse
	if err := proto.Unmarshal(d.leaseQuery(d.clients[0], evidencePath, req, height), &res); err != nil {
		d.t.Fatal(err)
	}
	return res.GetRecords()
}

// reportedState is the reported state of one evidence id at height, or
// UNSPECIFIED without a record.
func (d *demo) reportedState(evidenceID string, height int64) evidencev1.EvidenceState {
	d.t.Helper()
	records := d.evidenceAt(&evidencev1.QueryEvidenceRequest{EvidenceId: evidenceID}, height)
	switch len(records) {
	case 0:
		return evidencev1.EvidenceState_EVIDENCE_STATE_UNSPECIFIED
	case 1:
		return records[0].GetState()
	}
	d.t.Fatalf("evidence %s: %d records", evidenceID, len(records))
	return 0
}

func (d *demo) wantReported(phase, evidenceID string, height int64, want evidencev1.EvidenceState) {
	d.t.Helper()
	if got := d.reportedState(evidenceID, height); got != want {
		d.t.Fatalf("phase %s: %s at height %d reports %s, want %s", phase, evidenceID, height, got, want)
	}
}

// auditRound waits, polling the header time, until the current audit round
// started at least guard+60 s ago and has at least need seconds left, so a
// batch signed now commits in its round. It logs the wait and returns the
// epoch.
func (d *demo) auditRound(phase string, need int64) uint64 {
	d.t.Helper()
	now := d.chainTime()
	start := int64(evidence.EpochAt(now)) * evidence.EpochSeconds
	end := start + evidence.EpochSeconds
	target := now
	switch {
	case end-now < need:
		target = end + auditGuard + 60
	case now-start < auditGuard+60:
		target = start + auditGuard + 60
	}
	if target > now {
		d.waitChainTime(target, time.Duration(target-now+120)*time.Second)
	}
	after := d.chainTime()
	fmt.Printf("phase=%s-round epoch=%d header_time=%d waited_seconds=%d seconds_left=%d\n", phase, evidence.EpochAt(after), after, after-now,
		(int64(evidence.EpochAt(after))+1)*evidence.EpochSeconds-after)
	return evidence.EpochAt(after)
}

// leaseBytes is the committed ActiveLeases answer for one pair.
func (d *demo) leaseBytes(providerID, contentID string) []byte {
	d.t.Helper()
	return d.leaseQuery(d.clients[0], activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: providerID, ContentId: contentID}, 0)
}

// committedAudit commits one test-built batch and logs its sizes.
func (d *demo) committedAudit(phase string, who loaded, msg *evidencev1.MsgSubmitAuditBatch) *cmtResult {
	d.t.Helper()
	res := d.send(who, msg)
	if res.deliver.Code != 0 {
		d.t.Fatalf("phase %s: deliver %s/%d %s", phase, res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
	}
	raw, err := res.deliver.Marshal()
	if err != nil {
		d.t.Fatal(err)
	}
	fmt.Printf("phase=%s height=%d code=0 items=%d gas_used=%d result_bytes=%d txhash=%X\n", phase, res.height, len(msg.GetItems()), res.deliver.GasUsed, len(raw), res.hash)
	return res
}

// TestDevnetAuditLifecycle is the Goal 09 audit demo on a single-machine
// four-process devnet (not independent operators). The real producer
// (agents/auditor, built here) audits held leases over a loopback HTTPS
// test endpoint permitted by exact allowlist entries, signs, submits, and
// reads committed records back; a reviewer verifies them. It shows:
//
//   - run, sign, submit, status: with endpoints from capability manifests
//     whose digests the providers committed on chain, a PASS lands RECORDED,
//     a corrupted segment and a 404 land REJECTED, and a refused connection
//     stays INCOMPLETE off chain (no item, no record)
//   - the same failing endpoints typed into the operator configuration give
//     no FAIL (interim choice N2): INCOMPLETE ENDPOINT_UNBOUND
//     with the provider's answer kept in the reasons, nothing signed, no
//     record
//   - an acknowledgement (CheckTx) is not a commit: the producer prints both,
//     an expired round is acknowledged with code 0 and commits
//     STALE_VERSION (5), and a broadcast while the network has no quorum
//     stays SUBMITTED until a block holds it
//   - a real rebroadcast of the same bytes is refused by the node's mempool
//     cache ("tx already exists in cache"); the producer records the answer
//     as unknown and keeps the batch SUBMITTED; transaction bytes stored
//     without a record are still found on chain
//   - the same business payload under a new transaction hash is an
//     idempotent success with one set of records
//   - a dishonest but admitted auditor's false PASS and false FAIL are
//     recorded: a valid signature does not make an observation true; the
//     chain-only limits still bind it
//   - evidence availability (a report missing or altered in the producer's
//     store) is a separate question from chain inclusion (reviewer verdicts)
//   - no evidence renews a lease (FORBIDDEN while live, EXPIRED after its
//     expiry) and no audit is recorded for an ended, expired, or withdrawn
//     obligation; RECORDED turns EXPIRED at the first height whose header
//     time reaches the expiry
//   - per-transaction bytes and gas of 1-, 8- and 32-item batches, and an
//     in-process replay of every block that reproduces all four nodes' app
//     hashes
func TestDevnetAuditLifecycle(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	fmt.Println("label=single-machine four-process devnet (not independent operators)")
	fmt.Println("label=" + labelEndpoint)
	fmt.Println("label=" + labelAuditors)
	bin := buildAuditor(t)
	layout, home, stops := startDevnet(t)
	rpcs := rpcList(layout)
	d := newDemo(t, layout, home)
	a1, a2 := loadEd(t, home, "auditor-1"), loadEd(t, home, "auditor-2")
	p1, p2 := idPrefix(a1), idPrefix(a2)
	fmt.Printf("phase=start height=%d chain_id=%s accounts=%d auditor_1_prefix=%s auditor_2_prefix=%s\n",
		d.height(), layout.Topology.ChainID, len(layout.Topology.Accounts), p1, p2)

	// Phase 1: synthetic packages, the loopback test endpoint (la correct,
	// lb one corrupted byte in its last segment, lc absent: 404) and a closed
	// port for ld, providers whose records commit the digest of a capability
	// manifest listing those endpoints, content with synthetic package
	// manifests, and held leases. la, lb (p1) and lc, ld (p2) are audited by
	// the producer; le (p1), lw (p1), and lx (p2, deadline near) are the
	// ended, withdrawn, and expiring obligations of phase 9.
	leaseRows := []struct{ lease, content, provider string }{
		{"la", "ca", "p1"}, {"lb", "cb", "p1"}, {"lc", "cc", "p2"}, {"ld", "cd", "p2"},
		{"le", "ce", "p1"}, {"lw", "cw", "p1"}, {"lx", "cx", "p2"},
	}
	pkgs := map[string]auditPackage{}
	for _, row := range leaseRows {
		pkgs[row.content] = newAuditPackage(t, row.content)
	}
	dir := t.TempDir()
	ca := newTestCA(t)
	caPath := filepath.Join(dir, "test-ca.crt")
	if err := os.WriteFile(caPath, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	corrupted := append([]byte(nil), pkgs["cb"].bytes...)
	corrupted[len(corrupted)-1] ^= 0xff
	ep := startAuditEndpoint(t, ca, map[string][]byte{"ca": pkgs["ca"].bytes, "cb": corrupted})
	down := closedPort(t)
	url := func(port uint16) string { return fmt.Sprintf("https://127.0.0.1:%d", port) }
	// p1's manifest lists the endpoint; p2's the endpoint (index 0) and the
	// closed port (index 1). The records carry only the digests.
	regP1 := d.registerProvider(d.provider1, "reg-p1", "p1", capacityOne)
	regP2 := d.registerProvider(d.provider2, "reg-p2", "p2", capacityOne)
	capPath := map[string]string{}
	regP1.EndpointManifestDigest, capPath["p1"] = d.auditCapability(dir, "p1", capacityOne,
		capability.Endpoint{Name: "loopback test endpoint", URL: url(ep.port)})
	regP2.EndpointManifestDigest, capPath["p2"] = d.auditCapability(dir, "p2", capacityOne,
		capability.Endpoint{Name: "loopback test endpoint", URL: url(ep.port)},
		capability.Endpoint{Name: "closed loopback port", URL: url(down)})
	d.commit("register-p1", d.provider1, regP1)
	d.commit("register-p2", d.provider2, regP2)
	d.council("admit-p1", "prop-admit-p1", "admit-p1", admitChange("p1"))
	d.council("admit-p2", "prop-admit-p2", "admit-p2", admitChange("p2"))
	var registrations []sdk.Msg
	for _, row := range leaseRows {
		registrations = append(registrations, d.registerPackage(d.service, "reg-"+row.content, pkgs[row.content]))
	}
	d.commitMulti("register-content", d.service, registrations...)
	setup := d.chainTime()
	far, near := setup+2400, setup+240
	var offers []sdk.Msg
	offerOf := map[string]*leasesv1.MsgOfferLease{}
	for _, row := range leaseRows {
		deadline := far
		if row.lease == "lx" {
			deadline = near
		}
		offer := d.packageOffer("of-"+row.lease, "o-"+row.lease, row.provider, pkgs[row.content], deadline, setup+900)
		offerOf[row.lease] = offer
		offers = append(offers, offer)
	}
	d.commitMulti("offers", d.service, offers...)
	var accepts []sdk.Msg
	for _, row := range leaseRows {
		accepts = append(accepts, d.acceptMsg(d.broadcaster, "ac-"+row.lease, offerOf[row.lease], row.lease, d.keys[row.provider]))
	}
	d.commitMulti("accepts", d.broadcaster, accepts...)
	fmt.Printf("phase=leases held=%d deadline_far=%d deadline_lx=%d\n", len(leaseRows), far, near)

	// Phase 2: the producer configuration. Exact allowlist entries permit
	// the endpoint and the closed port; each audit names its endpoint by its
	// index in its provider's capability manifest.
	allow := []auditEntry{
		{ProviderID: "p1", Host: "127.0.0.1", IP: "127.0.0.1", Port: ep.port},
		{ProviderID: "p2", Host: "127.0.0.1", IP: "127.0.0.1", Port: ep.port},
		{ProviderID: "p2", Host: "127.0.0.1", IP: "127.0.0.1", Port: down},
	}
	manifestFile := func(contentID string) string {
		path := filepath.Join(dir, contentID+".manifest.json")
		if err := os.WriteFile(path, pkgs[contentID].manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// bound takes the endpoint from the provider's capability manifest, the
	// source that allows FAIL; typed takes it from the operator's endpoints
	// list (phase 4b).
	bound := func(lease, contentID, provider string, index int) auditSpec {
		return auditSpec{LeaseID: lease, ProviderID: provider, ContentID: contentID, ManifestPath: manifestFile(contentID),
			CapabilityManifestPath: capPath[provider], EndpointIndex: index, Mode: "WHOLE_PACKAGE"}
	}
	typed := func(lease, contentID, provider string, port uint16) auditSpec {
		return auditSpec{LeaseID: lease, ProviderID: provider, ContentID: contentID, ManifestPath: manifestFile(contentID),
			Endpoints: []auditURL{{Name: "loopback test endpoint", URL: url(port)}}, Mode: "WHOLE_PACKAGE"}
	}
	storeRoot := filepath.Join(dir, "store")
	base := auditConfig{
		ChainRPC: layout.Topology.Validators[0].RPC, CAFile: caPath, StoreRoot: storeRoot, Allowlist: allow,
		AuditorAddress: a1.addr, KeyPath: filepath.Join(home, "accounts", "auditor-1.hex"), KeyID: "key-auditor-1",
		Lifetime: 180, Guard: auditGuard, Inclusion: auditInclusion,
	}
	cfg1 := base
	cfg1.Audits = []auditSpec{bound("la", "ca", "p1", 0), bound("lb", "cb", "p1", 0), bound("lc", "cc", "p2", 0), bound("ld", "cd", "p2", 1)}
	cfg1Path := writeAuditConfig(t, filepath.Join(dir, "auditor-1.json"), cfg1)
	fmt.Printf("phase=endpoint port=%d closed_port=%d allowlist_entries=%d\n", ep.port, down, len(allow))

	// Phase 3: the audit round. Signing waits until the round has room.
	d.auditRound("audit", 150)

	// Phase 4: auditor-1 runs, signs, and submits with the real binary.
	code, lines := auditorCLI(t, bin, "run", "--config", cfg1Path)
	if code != 0 || len(lines) != 1 {
		t.Fatalf("run: exit %d, %d lines", code, len(lines))
	}
	var run1 runLine
	decodeLine(t, lines[0], &run1)
	wantRun := map[string]string{"la": "PASS", "lb": "FAIL SEGMENT_DIGEST_MISMATCH", "lc": "FAIL HTTP_STATUS", "ld": "INCOMPLETE CONNECT_FAILED"}
	evidenceOf := map[string]string{}
	reportOf := map[string]string{}
	for _, a := range run1.Audits {
		got := strings.TrimSpace(a.Result + " " + strings.Join(a.Reasons, " "))
		if got != wantRun[a.LeaseID] {
			t.Fatalf("run: %s is %q, want %q", a.LeaseID, got, wantRun[a.LeaseID])
		}
		if len(a.ReportSHA256) != 64 {
			t.Fatalf("run: %s has no report", a.LeaseID)
		}
		reportOf[a.LeaseID] = a.ReportSHA256
		evidenceOf[a.LeaseID] = p1 + a.ReportSHA256[:47]
		fmt.Printf("phase=run lease=%s result=%q report_sha256=%s\n", a.LeaseID, got, a.ReportSHA256)
	}
	if len(run1.Audits) != 4 || ep.requests.Load() == 0 {
		t.Fatalf("run: %d audits, %d endpoint requests", len(run1.Audits), ep.requests.Load())
	}
	code, lines = auditorCLI(t, bin, "sign", "--config", cfg1Path, "--run", run1.RunID)
	if code != 0 || len(lines) != 2 {
		t.Fatalf("sign: exit %d, %d lines", code, len(lines))
	}
	var batchIDs []string
	signed := map[string]signLine{}
	for _, line := range lines {
		var s signLine
		decodeLine(t, line, &s)
		batchIDs = append(batchIDs, s.BatchID)
		signed[s.ProviderID] = s
		fmt.Printf("phase=sign batch=%s provider=%s epoch=%d items=%d\n", s.BatchID, s.ProviderID, s.Epoch, len(s.EvidenceIDs))
	}
	p1IDs := signed["p1"].EvidenceIDs
	if len(p1IDs) != 2 || len(signed["p2"].EvidenceIDs) != 1 || signed["p2"].EvidenceIDs[0] != evidenceOf["lc"] ||
		!((p1IDs[0] == evidenceOf["la"] && p1IDs[1] == evidenceOf["lb"]) || (p1IDs[0] == evidenceOf["lb"] && p1IDs[1] == evidenceOf["la"])) {
		t.Fatalf("sign: %+v (ld must have no item)", signed)
	}
	itemState := map[string]string{evidenceOf["la"]: "RECORDED", evidenceOf["lb"]: "REJECTED", evidenceOf["lc"]: "REJECTED"}
	code, lines = auditorCLI(t, bin, append([]string{"submit", "--config", cfg1Path}, "--batch", batchIDs[0], "--batch", batchIDs[1])...)
	if code != 0 || len(lines) != 2 {
		t.Fatalf("submit: exit %d, %d lines", code, len(lines))
	}
	for _, line := range lines {
		var s submitLine
		decodeLine(t, line, &s)
		if s.State != "COMMITTED" || s.Code != 0 || s.Ack == nil || s.Ack.Code != 0 || s.Label != "COMMITTED(0)" {
			t.Fatalf("submit: %s", line)
		}
		for _, item := range s.Items {
			if item.State != itemState[item.EvidenceID] {
				t.Fatalf("submit: %s is %s, want %s", item.EvidenceID, item.State, itemState[item.EvidenceID])
			}
			fmt.Printf("phase=submit batch=%s ack=%d committed_height=%d committed_code=%d evidence=%s item=%s\n", s.BatchID, s.Ack.Code, s.Height, s.Code, item.EvidenceID, item.State)
		}
	}
	wantState := map[string]evidencev1.EvidenceState{
		"la": evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED, "lb": evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED,
		"lc": evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED,
	}
	for lease, want := range wantState {
		d.wantReported("submit", evidenceOf[lease], 0, want)
	}
	if records := d.evidenceAt(&evidencev1.QueryEvidenceRequest{LeaseId: "ld"}, 0); len(records) != 0 {
		t.Fatalf("ld (INCOMPLETE, network failure) has %d records", len(records))
	}
	fmt.Println("phase=incomplete lease=ld reason=CONNECT_FAILED on_chain=false")
	recordA := d.evidenceAt(&evidencev1.QueryEvidenceRequest{EvidenceId: evidenceOf["la"]}, 0)[0]
	expiryA := recordA.GetExpiryUnixSeconds()
	// status broadcasts nothing and reads the same committed result.
	code, lines = auditorCLI(t, bin, "status", "--config", cfg1Path, "--batch", batchIDs[0])
	var status submitLine
	if code != 0 || len(lines) != 1 {
		t.Fatalf("status: exit %d", code)
	}
	decodeLine(t, lines[0], &status)
	if status.State != "COMMITTED" || status.Ack != nil {
		t.Fatalf("status: %s", lines[0])
	}
	verify := func(phase, evidenceID, storeDir, want string) verifyLine {
		t.Helper()
		code, lines := auditorCLI(t, bin, "verify", "--evidence-id", evidenceID, "--store", storeDir, "--node", layout.Topology.Validators[0].RPC)
		if len(lines) != 1 {
			t.Fatalf("phase %s verify: exit %d, %d lines", phase, code, len(lines))
		}
		var v verifyLine
		decodeLine(t, lines[0], &v)
		wantExit := 0
		if want != "CONSISTENT" {
			wantExit = 3
		}
		if v.Verdict != want || code != wantExit || !strings.HasPrefix(v.RecordSource, "NODE ") || v.ClaimLimit != evidence.ClaimLimit {
			t.Fatalf("phase %s verify %s: exit %d %s, want %s", phase, evidenceID, code, lines[0], want)
		}
		fmt.Printf("phase=%s verify=%s verdict=%s state=%s report=%s endpoint_source=%s\n", phase, evidenceID, v.Verdict, v.State, v.ReportResult, v.ReportEndpointSource)
		return v
	}
	// Every phase 4 audit named its provider's capability manifest, so each
	// stored report says CAPABILITY_MANIFEST, as a REJECTED record needs (N2).
	for _, lease := range []string{"la", "lb", "lc"} {
		if v := verify("review", evidenceOf[lease], storeRoot, "CONSISTENT"); v.ReportEndpointSource != "CAPABILITY_MANIFEST" {
			t.Fatalf("phase review: report of %s has endpoint_source %q, want CAPABILITY_MANIFEST", lease, v.ReportEndpointSource)
		}
	}

	// Phase 4b (interim choice N2): the same failing endpoint
	// typed into the operator's endpoints list. Nothing binds it to the
	// provider, so the server's wrong answers give no FAIL: lb (the
	// corrupted byte) and lc (404) are INCOMPLETE ENDPOINT_UNBOUND with the
	// provider's answer kept in the reasons, `sign` finds no PASS or FAIL
	// audit, and no record appears for either lease or for the evidence ids
	// their reports would have.
	d.auditRound("operator-endpoint", 60)
	cfgTyped := base
	cfgTyped.StoreRoot = filepath.Join(dir, "store-operator-endpoints")
	cfgTyped.Audits = []auditSpec{typed("lb", "cb", "p1", ep.port), typed("lc", "cc", "p2", ep.port)}
	cfgTypedPath := writeAuditConfig(t, filepath.Join(dir, "auditor-1-operator-endpoints.json"), cfgTyped)
	recordsBefore := map[string]int{}
	for _, lease := range []string{"lb", "lc"} {
		recordsBefore[lease] = len(d.evidenceAt(&evidencev1.QueryEvidenceRequest{LeaseId: lease}, 0))
	}
	requestsBefore := ep.requests.Load()
	code, lines = auditorCLI(t, bin, "run", "--config", cfgTypedPath)
	if code != 0 || len(lines) != 1 {
		t.Fatalf("operator-endpoint run: exit %d, %d lines", code, len(lines))
	}
	var runTyped runLine
	decodeLine(t, lines[0], &runTyped)
	wantTyped := map[string]string{"lb": "INCOMPLETE ENDPOINT_UNBOUND SEGMENT_DIGEST_MISMATCH", "lc": "INCOMPLETE ENDPOINT_UNBOUND HTTP_STATUS"}
	unboundIDs := map[string]string{}
	for _, a := range runTyped.Audits {
		got := strings.TrimSpace(a.Result + " " + strings.Join(a.Reasons, " "))
		if got != wantTyped[a.LeaseID] || len(a.ReportSHA256) != 64 {
			t.Fatalf("operator-endpoint run: %s is %q (report %q), want %q", a.LeaseID, got, a.ReportSHA256, wantTyped[a.LeaseID])
		}
		unboundIDs[a.LeaseID] = p1 + a.ReportSHA256[:47]
		// The stored report records the source the audit was classified
		// with; read it rather than assume it.
		rawReport, err := os.ReadFile(filepath.Join(cfgTyped.StoreRoot, "reports", a.ReportSHA256+".json"))
		if err != nil {
			t.Fatalf("operator-endpoint run: report of %s: %v", a.LeaseID, err)
		}
		var stored struct {
			EndpointSource string `json:"endpoint_source"`
			Result         string `json:"result"`
		}
		if err := json.Unmarshal(rawReport, &stored); err != nil {
			t.Fatalf("operator-endpoint run: report of %s: %v", a.LeaseID, err)
		}
		if stored.EndpointSource != "OPERATOR_CONFIG" || stored.Result != "INCOMPLETE" {
			t.Fatalf("operator-endpoint run: report of %s has endpoint_source %q result %q, want OPERATOR_CONFIG INCOMPLETE", a.LeaseID, stored.EndpointSource, stored.Result)
		}
		fmt.Printf("phase=operator-endpoint-run lease=%s result=%q report_endpoint_source=%s report_result=%s\n", a.LeaseID, got, stored.EndpointSource, stored.Result)
	}
	if len(runTyped.Audits) != 2 || len(unboundIDs) != 2 || ep.requests.Load() < requestsBefore+2 {
		t.Fatalf("operator-endpoint run: %d audits, %d new endpoint requests", len(runTyped.Audits), ep.requests.Load()-requestsBefore)
	}
	code, lines = auditorCLI(t, bin, "sign", "--config", cfgTypedPath, "--run", runTyped.RunID)
	if code != 0 || len(lines) != 1 {
		t.Fatalf("operator-endpoint sign: exit %d, %d lines", code, len(lines))
	}
	var signedTyped struct {
		RunID   string `json:"run_id"`
		Batches *int   `json:"batches"`
		BatchID string `json:"batch_id"`
	}
	decodeLine(t, lines[0], &signedTyped)
	if signedTyped.RunID != runTyped.RunID || signedTyped.Batches == nil || *signedTyped.Batches != 0 || signedTyped.BatchID != "" {
		t.Fatalf("operator-endpoint sign: %s (nothing may be signed)", lines[0])
	}
	for _, lease := range []string{"lb", "lc"} {
		if n := len(d.evidenceAt(&evidencev1.QueryEvidenceRequest{LeaseId: lease}, 0)); n != recordsBefore[lease] {
			t.Fatalf("operator-endpoint: records of %s %d -> %d", lease, recordsBefore[lease], n)
		}
		d.wantReported("operator-endpoint", unboundIDs[lease], 0, evidencev1.EvidenceState_EVIDENCE_STATE_UNSPECIFIED)
	}
	fmt.Printf("phase=operator-endpoint signed_batches=0 new_records=0 label=%q\n", "an endpoint not bound to the provider gives no FAIL (N2)")

	// Phase 8a: a renewal of la naming its live RECORDED evidence is
	// FORBIDDEN; the lease bytes do not change.
	beforeA := d.leaseBytes("p1", "ca")
	d.reject("renew-names-live-evidence", d.broadcaster, d.renewNaming(d.broadcaster, "rn-ev-live", "la", "p1", pkgs["ca"], far+devnetTerm, evidenceOf["la"]), msgkit.Forbidden)
	if !bytes.Equal(beforeA, d.leaseBytes("p1", "ca")) {
		t.Fatal("a renewal naming evidence changed lease la")
	}

	// Phase 5: the same business payload under a new transaction hash (the
	// next account sequence): idempotent success, one set of records.
	stored, err := os.ReadFile(filepath.Join(storeRoot, "batches", batchIDs[0]+".pb"))
	if err != nil {
		t.Fatal(err)
	}
	var again evidencev1.MsgSubmitAuditBatch
	if err := proto.Unmarshal(stored, &again); err != nil {
		t.Fatal(err)
	}
	evidenceBefore := d.evidenceAt(&evidencev1.QueryEvidenceRequest{LeaseId: "la"}, 0)
	d.syncSequence(a1)
	res := d.send(a1, &again)
	if res.deliver.Code != 0 || !hasEvent(res.deliver.Events, "evidence_result") || hasEvent(res.deliver.Events, "svn.events.v1.EvidenceAccepted") {
		t.Fatalf("resend: %s/%d events %v", res.deliver.Codespace, res.deliver.Code, eventTypesAll(res.deliver.Events))
	}
	if after := d.evidenceAt(&evidencev1.QueryEvidenceRequest{LeaseId: "la"}, 0); len(after) != len(evidenceBefore) || len(after) != 1 {
		t.Fatalf("resend: records of la %d -> %d", len(evidenceBefore), len(after))
	}
	fmt.Printf("phase=resend-new-hash height=%d code=0 idempotent=true records_of_la=1 txhash=%X\n", res.height, res.hash)

	// Phase 6: auditor-2, admitted but dishonest, fetches nothing. Its false
	// PASS for lb (lb's public manifest digest) is RECORDED and its false
	// FAIL for la (a fabricated report digest) is REJECTED, beside
	// auditor-1's opposite records. A valid signature shows which admitted
	// key made a statement; it does not make the statement true.
	epoch := d.auditRound("dishonest", 90)
	now := d.chainTime()
	others := d.leaseBytes("p1", "cb")
	falsePass, falseFail := p2+"false-pass-lb", p2+"false-fail-la"
	fabricated := sha256.Sum256([]byte("svn-audit-demo-fabricated-fail-report"))
	d.committedAudit("dishonest-auditor", a2, signedBatch(t, a2, p2+"bid-dh-1", p2+"dh-1", epoch,
		auditItem(falseFail, "la", "ca", fabricated[:], now+600),
		auditItem(falsePass, "lb", "cb", pkgs["cb"].digest, now+600)))
	d.wantReported("dishonest", falsePass, 0, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED)
	d.wantReported("dishonest", falseFail, 0, evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED)
	if !bytes.Equal(others, d.leaseBytes("p1", "cb")) || !bytes.Equal(beforeA, d.leaseBytes("p1", "ca")) {
		t.Fatal("a false attestation changed a lease")
	}
	emptyStore := filepath.Join(dir, "auditor-2-store")
	if err := os.Mkdir(emptyStore, 0o700); err != nil {
		t.Fatal(err)
	}
	verify("dishonest", falsePass, emptyStore, "PAYLOAD_UNAVAILABLE")
	// The block that includes it has a later header time than now, so the
	// expiry is 120 s past the lifetime bound of the latest header.
	d.reject("dishonest-lifetime-above-86400", a2, signedBatch(t, a2, p2+"bid-dh-2", p2+"dh-2", epoch,
		auditItem(p2+"dh-2-e", "la", "ca", pkgs["ca"].digest, now+evidence.MaxEvidenceLifetimeSeconds+120)), msgkit.Bounds)
	otherVersion := auditItem(p2+"dh-3-e", "la", "ca", pkgs["ca"].digest, now+600)
	otherVersion.ContentVersion = 2
	d.reject("dishonest-other-content-version", a2, signedBatch(t, a2, p2+"bid-dh-3", p2+"dh-3", epoch, otherVersion), msgkit.Conflict)
	d.reject("dishonest-takes-auditor-1-ids", a2, signedBatch(t, a2, p2+"bid-dh-4", p1+"dh-4", epoch,
		auditItem(p1+"dh-4-e", "la", "ca", pkgs["ca"].digest, now+600)), msgkit.Forbidden)
	d.committedAudit("auditor-1-own-ids", a1, signedBatch(t, a1, p1+"bid-dh-4", p1+"dh-4", epoch,
		auditItem(p1+"dh-4-e", "la", "ca", pkgs["ca"].digest, now+600)))
	d.reject("renew-names-false-pass", d.broadcaster, d.renewNaming(d.broadcaster, "rn-ev-false", "lb", "p1", pkgs["cb"], far+devnetTerm, falsePass), msgkit.Forbidden)
	d.reject("broadcaster-relays-auditor-batch", d.broadcaster, func() sdk.Msg {
		m := signedBatch(t, a1, p1+"bid-relay", p1+"relay", epoch, auditItem(p1+"relay-e", "la", "ca", pkgs["ca"].digest, now+600))
		m.Signer = d.broadcaster.key.PubKey().Address()
		return m
	}(), msgkit.Forbidden)

	// Phase 7: availability is not inclusion.
	// (a) A's report missing from the store: PAYLOAD_UNAVAILABLE; the chain
	// record does not change.
	if err := os.Remove(filepath.Join(storeRoot, "reports", reportOf["la"]+".json")); err != nil {
		t.Fatal(err)
	}
	verify("availability", evidenceOf["la"], storeRoot, "PAYLOAD_UNAVAILABLE")
	d.wantReported("availability", evidenceOf["la"], 0, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED)
	// (b) B's report altered: CHECKSUM_MISMATCH.
	reportB := filepath.Join(storeRoot, "reports", reportOf["lb"]+".json")
	rawB, err := os.ReadFile(reportB)
	if err != nil {
		t.Fatal(err)
	}
	rawB[len(rawB)/2] ^= 0x01
	if err := os.WriteFile(reportB, rawB, 0o600); err != nil {
		t.Fatal(err)
	}
	verify("availability", evidenceOf["lb"], storeRoot, "CHECKSUM_MISMATCH")
	// (e) C's record is a provider-availability result (404, REJECTED), a
	// different thing from a report the producer lost (a).
	verify("availability", evidenceOf["lc"], storeRoot, "CONSISTENT")

	// (c) An expired round. A second run audits la and lb with a longer
	// lifetime; test code copies its signed batch into the previous epoch
	// (re-signed by auditor-1's key, new batch and business ids) and
	// `auditor submit` sends it: the warning EPOCH_LIKELY_STALE, CheckTx
	// code 0, committed code 5 (STALE_VERSION), FAILED(5), never recorded.
	d.auditRound("second-run", 300)
	cfg2 := base
	cfg2.Lifetime = 900
	cfg2.Audits = []auditSpec{bound("la", "ca", "p1", 0), bound("lb", "cb", "p1", 0)}
	cfg2Path := writeAuditConfig(t, filepath.Join(dir, "auditor-1-second.json"), cfg2)
	code, lines = auditorCLI(t, bin, "run", "--config", cfg2Path)
	if code != 0 || len(lines) != 1 {
		t.Fatalf("second run: exit %d", code)
	}
	var run2 runLine
	decodeLine(t, lines[0], &run2)
	// Plan section 18 step 5: the honest producer, re-run against the same
	// endpoints, observes la PASS and lb FAIL again. The same operator on the
	// same machine ran it twice: it is not an independent retrieval test.
	if len(run2.Audits) != 2 {
		t.Fatalf("second run: %d audits", len(run2.Audits))
	}
	for _, a := range run2.Audits {
		got := strings.TrimSpace(a.Result + " " + strings.Join(a.Reasons, " "))
		if got != wantRun[a.LeaseID] || (a.LeaseID != "la" && a.LeaseID != "lb") {
			t.Fatalf("second run: %s is %q, want %q", a.LeaseID, got, wantRun[a.LeaseID])
		}
		fmt.Printf("phase=rerun lease=%s result=%q label=%q\n", a.LeaseID, got, "same operator, same machine; not an independent retrieval test")
	}
	code, lines = auditorCLI(t, bin, "sign", "--config", cfg2Path, "--run", run2.RunID)
	if code != 0 || len(lines) != 1 {
		t.Fatalf("second sign: exit %d, %d lines", code, len(lines))
	}
	var batchS signLine
	decodeLine(t, lines[0], &batchS)
	stale := writeStaleBatch(t, storeRoot, batchS.BatchID, edPrivate(t, a1))
	code, lines = auditorCLI(t, bin, "submit", "--config", cfg2Path, "--batch", stale)
	if len(lines) != 1 {
		t.Fatalf("stale submit: exit %d", code)
	}
	var staleRes submitLine
	decodeLine(t, lines[0], &staleRes)
	if code != 3 || staleRes.State != "FAILED" || staleRes.Code != 5 || staleRes.Label != "FAILED(5)" || staleRes.Ack == nil || staleRes.Ack.Code != 0 ||
		len(staleRes.Warnings) != 1 || staleRes.Warnings[0] != "EPOCH_LIKELY_STALE" {
		t.Fatalf("stale submit: exit %d %s", code, lines[0])
	}
	fmt.Printf("phase=stale-round batch=%s warning=%s ack=%d committed_code=%d state=%s\n", stale, staleRes.Warnings[0], staleRes.Ack.Code, staleRes.Code, staleRes.Label)
	for _, id := range batchS.EvidenceIDs {
		verify("stale-round", id, storeRoot, "NOT_ON_CHAIN")
	}

	// (d) No quorum: v3 and v4 stop. `auditor submit` of batch S is
	// acknowledged (CheckTx code 0) and no block holds it: SUBMITTED, never
	// recorded. A second submit rebuilds the same bytes at the same account
	// sequence; the node's mempool refuses them ("tx already exists in
	// cache", an RPC error), so the producer records the answer as unknown
	// and the batch stays SUBMITTED. After v3 and v4 restart, `status`
	// reports COMMITTED(0) and reads the records back, also after the
	// submission records are removed (bytes stored before the broadcast
	// without a record: the crash window between the two writes).
	for _, i := range []int{2, 3} {
		if err := stops[i](); err != nil {
			t.Fatal(err)
		}
		stops[i] = nil
	}
	fmt.Println("phase=no-quorum stopped=v3,v4")
	code, lines = auditorCLI(t, bin, "submit", "--config", cfg2Path, "--batch", batchS.BatchID)
	var waiting submitLine
	decodeLine(t, lines[0], &waiting)
	if code != 3 || waiting.State != "SUBMITTED" || waiting.Ack == nil || waiting.Ack.Code != 0 || len(waiting.Items) != 2 {
		t.Fatalf("no-quorum submit: exit %d %s", code, lines[0])
	}
	for _, item := range waiting.Items {
		if item.State != "NOT_ON_CHAIN" {
			t.Fatalf("no-quorum submit: %s", lines[0])
		}
	}
	fmt.Printf("phase=no-quorum-submit ack=%d state=%s recorded=false tx=%s\n", waiting.Ack.Code, waiting.Label, waiting.TxHash)
	code, lines = auditorCLI(t, bin, "submit", "--config", cfg2Path, "--batch", batchS.BatchID)
	var rebroadcast submitLine
	decodeLine(t, lines[0], &rebroadcast)
	if code != 3 || rebroadcast.State != "SUBMITTED" || rebroadcast.Ack != nil {
		t.Fatalf("rebroadcast: exit %d %s", code, lines[0])
	}
	first := readSubmission(t, storeRoot, batchS.BatchID, 0)
	second := readSubmission(t, storeRoot, batchS.BatchID, 1)
	if first.Ack != "ACCEPTED" || second.Ack != "UNKNOWN" || second.TxHash != first.TxHash {
		t.Fatalf("submissions: %+v %+v", first, second)
	}
	cacheRefusal := checkMempoolCache(t, d, storeRoot, batchS.BatchID)
	fmt.Printf("phase=rebroadcast node_answer=%q recorded_ack=%s same_hash=true state=%s\n", cacheRefusal, second.Ack, rebroadcast.Label)
	for _, i := range []int{2, 3} {
		stop, err := launch(layout.Topology.Validators[i].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[i] = stop
	}
	if _, _, err := devnet.WaitEqual(rpcs, d.height()+2, 90*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	hash, err := hex.DecodeString(first.TxHash)
	if err != nil {
		t.Fatal(err)
	}
	committedS := d.waitTx(hash, 60*time.Second)
	for n := 0; n < 2; n++ {
		if err := os.Remove(filepath.Join(storeRoot, "batches", submissionName(batchS.BatchID, n)+".json")); err != nil {
			t.Fatal(err)
		}
	}
	code, lines = auditorCLI(t, bin, "status", "--config", cfg2Path, "--batch", batchS.BatchID)
	var restored submitLine
	decodeLine(t, lines[0], &restored)
	if code != 0 || restored.State != "COMMITTED" || restored.Height != committedS.Height || restored.TxHash != first.TxHash {
		t.Fatalf("status after restart: exit %d %s", code, lines[0])
	}
	fmt.Printf("phase=quorum-back committed_height=%d committed_code=%d state=%s submission_records_removed=2 found_by_stored_bytes=true\n",
		committedS.Height, committedS.TxResult.Code, restored.Label)

	// Phase 8b: la's first evidence past its expiry. A renewal naming it is
	// EXPIRED; the reported state turns from RECORDED to EXPIRED at the
	// first height whose header time reaches the expiry (effective) and is
	// RECORDED at the height before (effective-1). The stored record is
	// never rewritten.
	reached := d.waitChainTime(expiryA, time.Duration(max(expiryA-d.chainTime(), 0)+120)*time.Second)
	d.reject("renew-names-expired-evidence", d.broadcaster, d.renewNaming(d.broadcaster, "rn-ev-expired", "la", "p1", pkgs["ca"], far+devnetTerm, evidenceOf["la"]), msgkit.Expired)
	if !bytes.Equal(beforeA, d.leaseBytes("p1", "ca")) {
		t.Fatal("a renewal naming expired evidence changed lease la")
	}
	// The first height whose header time reaches the expiry: header times
	// never decrease, so a binary search over 1..reached finds it.
	low, high := int64(1), reached
	for low < high {
		mid := (low + high) / 2
		if d.blockTime(mid) >= expiryA {
			high = mid
		} else {
			low = mid + 1
		}
	}
	effective := low
	if effective <= 1 || d.blockTime(effective) < expiryA || d.blockTime(effective-1) >= expiryA {
		t.Fatalf("expiry %d: effective height %d", expiryA, effective)
	}
	d.wantReported("expiry", evidenceOf["la"], effective-1, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED)
	d.wantReported("expiry", evidenceOf["la"], effective, evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED)
	d.wantReported("expiry", evidenceOf["la"], 0, evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED)
	fmt.Printf("phase=evidence-expiry expiry=%d effective_minus_1=%d (header %d) RECORDED effective=%d (header %d) EXPIRED\n",
		expiryA, effective-1, d.blockTime(effective-1), effective, d.blockTime(effective))

	// Phase 9: ended, withdrawn, and expired obligations. auditor-1 records
	// le and lw while they are held; then le ends, cw is withdrawn, and lx
	// passes its deadline. A new audit of each, passing or failing, is
	// CONFLICT, WITHDRAWN, or CONFLICT, and leaves the lease and content
	// answers unchanged; le's and lw's earlier RECORDED records still read
	// RECORDED until their own expiry: one past observation each.
	epoch = d.auditRound("obligations", 90)
	now = d.chainTime()
	d.syncSequence(a1)
	heldE, heldW := p1+"held-le", p1+"held-lw"
	d.committedAudit("audit-held", a1, signedBatch(t, a1, p1+"bid-held", p1+"held", epoch,
		auditItem(heldE, "le", "ce", pkgs["ce"].digest, now+600), auditItem(heldW, "lw", "cw", pkgs["cw"].digest, now+600)))
	d.commit("end-le", d.broadcaster, d.endMsg(d.broadcaster, "en-le", "le", "p1", 1))
	d.commit("withdraw-cw", d.service, d.withdrawContent(d.service, "wd-cw", "cw", 1, reason("cw")))
	d.waitChainTime(near, time.Duration(max(near-d.chainTime(), 0)+120)*time.Second)
	epoch = d.auditRound("obligations-late", 60)
	now = d.chainTime()
	answers := func() []byte {
		var out []byte
		for _, row := range []struct{ provider, contentID string }{{"p1", "ce"}, {"p1", "cw"}, {"p2", "cx"}} {
			out = append(out, d.leaseBytes(row.provider, row.contentID)...)
			out = append(out, d.leaseQuery(d.clients[0], contentPath, &contentv1.QueryContentRequest{ContentId: row.contentID, ContentVersion: 1}, 0)...)
		}
		return out
	}
	beforeLate := answers()
	for i, row := range []struct {
		lease, contentID string
		want             msgkit.Code
	}{{"le", "ce", msgkit.Conflict}, {"lw", "cw", msgkit.Withdrawn}, {"lx", "cx", msgkit.Conflict}} {
		for j, observation := range [][]byte{pkgs[row.contentID].digest, fabricated[:]} {
			id := fmt.Sprintf("late-%d-%d", i, j)
			d.reject("late-audit-"+row.lease, a1, signedBatch(t, a1, p1+"bid-"+id, p1+id, epoch,
				auditItem(p1+id+"-e", row.lease, row.contentID, observation, now+60)), row.want)
		}
	}
	if !bytes.Equal(beforeLate, answers()) {
		t.Fatal("a rejected audit changed a lease or content answer")
	}
	d.wantReported("obligations", heldE, 0, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED)
	d.wantReported("obligations", heldW, 0, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED)
	fmt.Println("phase=obligations ended=CONFLICT withdrawn=WITHDRAWN expired=CONFLICT earlier_records=RECORDED")

	// Phase 10: per-transaction measurements of test-built batches (F1, F8,
	// F32: 1 item; 8 items over la x3, lb x3, lc x2; 32 items over la x11,
	// lb x11, lc x10), from committed results.
	epoch = d.auditRound("measure", 90)
	now = d.chainTime()
	fixture := func(name string, n int) {
		items := make([]*evidencev1.EvidenceItem, n)
		for i := range items {
			lease, contentID := []string{"la", "lb", "lc"}[i*3/n], []string{"ca", "cb", "cc"}[i*3/n]
			observation := pkgs[contentID].digest
			if i%2 == 1 {
				observation = fabricated[:]
			}
			items[i] = auditItem(fmt.Sprintf("%s%s-%02d", p2, name, i), lease, contentID, observation, now+600)
		}
		msg := signedBatch(t, a2, p2+"bid-"+name, p2+name, epoch, items...)
		res := d.committedAudit("measure-"+name, a2, msg)
		committed := d.waitTx(res.hash, 30*time.Second)
		text, err := evidence.AuditCanonical(msg)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("phase=measure fixture=%s items=%d tx_bytes=%d gas_used=%d canonical_payload_bytes=%d label=%q\n",
			name, n, len(committed.Tx), committed.TxResult.GasUsed, len(text), "per-fixture measurement on one machine; no throughput or network-capacity claim")
	}
	fixture("f1", 1)
	fixture("f8", 8)
	fixture("f32", 32)

	// Phase 11: equal queries on all four nodes, every committed event, and
	// an in-process replay of every block.
	fixed := d.height()
	if _, _, err := devnet.WaitEqual(rpcs, fixed+1, 60*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	var want []byte
	for i, client := range d.clients {
		raw := d.leaseQuery(client, evidencePath, &evidencev1.QueryEvidenceRequest{LeaseId: "la", Page: &typesv1.PageRequest{Limit: 100}}, fixed)
		if i == 0 {
			want = raw
		} else if !bytes.Equal(want, raw) {
			t.Fatalf("evidence query of la at %d differs on node %d", fixed, i+1)
		}
	}
	d.scanEvents(fixed)
	start := time.Now()
	replayed := d.replay(layout.Genesis, fixed)
	fmt.Printf("phase=replay-time height=%d seconds=%.1f label=%q\n", fixed, time.Since(start).Seconds(), "one machine; not a catch-up benchmark")
	if !bytes.Equal(replayQuery(t, replayed, evidencePath, &evidencev1.QueryEvidenceRequest{LeaseId: "la", Page: &typesv1.PageRequest{Limit: 100}}, fixed), want) {
		t.Fatal("replayed evidence query differs from the nodes")
	}
}

func hasEvent(events []abci.Event, kind string) bool {
	for _, ev := range events {
		if ev.Type == kind {
			return true
		}
	}
	return false
}

func eventTypesAll(events []abci.Event) []string {
	var out []string
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return out
}

// submission is the part of a stored submission record the test reads.
type submission struct {
	Ack    string `json:"ack"`
	TxHash string `json:"tx_hash"`
}

// submissionName is the producer's store name of submission n of a batch
// (agents/auditor/chainclient.SubmissionName): the batch id's auditor
// prefix and 47 hex characters of SHA-256 of
// "svn-audit-submission:" + batch id + ":" + n.
func submissionName(batchID string, n int) string {
	sum := sha256.Sum256([]byte("svn-audit-submission:" + batchID + ":" + strconv.Itoa(n)))
	return batchID[:2*evidence.IDPrefixBytes+1] + hex.EncodeToString(sum[:])[:47]
}

func readSubmission(t *testing.T, storeRoot, batchID string, n int) submission {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(storeRoot, "batches", submissionName(batchID, n)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var s submission
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// checkMempoolCache sends the stored bytes of submission 0 once more with
// broadcast_tx_sync and returns the node's answer, which must be the
// mempool cache refusal: the bytes wait in the mempool.
func checkMempoolCache(t *testing.T, d *demo, storeRoot, batchID string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(storeRoot, "batches", submissionName(batchID, 0)+".pb"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.broadcastSync(d.clients[0], raw)
	if err == nil || !strings.Contains(err.Error(), "tx already exists in cache") {
		t.Fatalf("broadcast of waiting bytes: %v", err)
	}
	return "tx already exists in cache"
}

// staleSummary mirrors the producer's batch summary
// (agents/auditor/batch.Summary; SVN-JCS-1 text with exactly these keys).
type staleSummary struct {
	Format      string           `json:"format"`
	BatchID     string           `json:"batch_id"`
	BusinessID  string           `json:"business_id"`
	ProviderID  string           `json:"provider_id"`
	RunID       string           `json:"run_id"`
	Epoch       int64            `json:"epoch"`
	Signer      string           `json:"signer"`
	SignerKeyID string           `json:"signer_key_id"`
	Supersedes  string           `json:"supersedes"`
	Items       []map[string]any `json:"items"`
}

// writeStaleBatch copies the stored batch batchID into the previous epoch:
// the batch id and business id follow the producer's derivations
// (agents/auditor/batch.BatchID and BusinessID), the message is re-signed
// with priv (auditor-1's key), and the message and summary files are
// written into the producer's store as `sign` writes them. It returns the
// new batch id.
func writeStaleBatch(t *testing.T, storeRoot, batchID string, priv ed25519.PrivateKey) string {
	t.Helper()
	dir := filepath.Join(storeRoot, "batches")
	raw, err := os.ReadFile(filepath.Join(dir, batchID+".pb"))
	if err != nil {
		t.Fatal(err)
	}
	var msg evidencev1.MsgSubmitAuditBatch
	if err := proto.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	rawSummary, err := os.ReadFile(filepath.Join(dir, batchID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary staleSummary
	dec := json.NewDecoder(bytes.NewReader(rawSummary))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&summary); err != nil {
		t.Fatal(err)
	}
	prefix := evidence.IDPrefix(msg.GetSigner())
	epoch := msg.GetEpoch() - 1
	ids := make([]any, 0, len(msg.GetItems()))
	for _, it := range msg.GetItems() {
		ids = append(ids, it.GetEvidenceId())
	}
	text, err := jcs.Canonicalize(map[string]any{"auditor": hex.EncodeToString(msg.GetSigner()), "epoch": strconv.FormatUint(epoch, 10), "evidence_ids": ids})
	if err != nil {
		t.Fatal(err)
	}
	idSum := sha256.Sum256([]byte(text))
	staleID := prefix + hex.EncodeToString(idSum[:])[:47]
	bidSum := sha256.Sum256([]byte("svn-audit-business:" + staleID + ":0"))
	msg.Epoch, msg.BatchId, msg.BusinessId = epoch, staleID, prefix+hex.EncodeToString(bidSum[:])[:47]
	resign(t, &msg, priv)
	out, err := proto.MarshalOptions{Deterministic: true}.Marshal(&msg)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]any, 0, len(summary.Items))
	for _, it := range summary.Items {
		item := map[string]any{}
		for k, v := range it {
			if n, ok := v.(json.Number); ok {
				i, err := n.Int64()
				if err != nil {
					t.Fatal(err)
				}
				item[k] = i
				continue
			}
			item[k] = v
		}
		items = append(items, item)
	}
	summaryText, err := jcs.Canonicalize(map[string]any{
		"format": summary.Format, "batch_id": staleID, "business_id": msg.GetBusinessId(), "provider_id": summary.ProviderID,
		"run_id": summary.RunID, "epoch": int64(epoch), "signer": summary.Signer, "signer_key_id": summary.SignerKeyID,
		"supersedes": summary.Supersedes, "items": items,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{staleID + ".pb": out, staleID + ".json": []byte(summaryText)} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return staleID
}
