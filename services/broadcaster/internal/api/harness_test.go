package api

// Test harness: a development CA, server and client certificates generated
// at run time into a temporary directory, the server built with New and
// served with Serve on a loopback listener (the constructor and serve path
// the binary uses), and HTTP clients that present a client certificate.

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
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

const testChainID = "shortvideo-dev-apitest"

// pki is a run-time development CA.
type pki struct {
	t      *testing.T
	dir    string
	ca     *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPool *x509.CertPool
	serial int64
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "svn dev CA (test)"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &pki{t: t, dir: t.TempDir(), ca: ca, caKey: caKey, caPool: pool, serial: 1}
}

// issue signs a leaf certificate for tmpl's names and returns it with its
// key as a tls.Certificate and as PEM blocks.
func (p *pki) issue(tmpl *x509.Certificate) (tls.Certificate, []byte, []byte) {
	p.t.Helper()
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		p.t.Fatal(err)
	}
	p.serial++
	tmpl.SerialNumber = big.NewInt(p.serial)
	tmpl.NotBefore, tmpl.NotAfter = time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.ca, &leafKey.PublicKey, p.caKey)
	if err != nil {
		p.t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		p.t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		p.t.Fatal(err)
	}
	return pair, certPEM, keyPEM
}

// client returns a certificate whose URI SANs are uris.
func (p *pki) client(uris ...string) tls.Certificate {
	p.t.Helper()
	tmpl := &x509.Certificate{Subject: pkix.Name{CommonName: "caller"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	for _, s := range uris {
		u, err := url.Parse(s)
		if err != nil {
			p.t.Fatal(err)
		}
		tmpl.URIs = append(tmpl.URIs, u)
	}
	pair, _, _ := p.issue(tmpl)
	return pair
}

// serverFiles writes the server certificate, its key (mode 0600) and the
// CA bundle and returns their paths.
func (p *pki) serverFiles() (certFile, keyFile, caFile string) {
	p.t.Helper()
	_, certPEM, keyPEM := p.issue(&x509.Certificate{
		Subject: pkix.Name{CommonName: "svn-broadcaster"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	certFile = filepath.Join(p.dir, "server.pem")
	keyFile = filepath.Join(p.dir, "server-key.pem")
	caFile = filepath.Join(p.dir, "ca.pem")
	for path, data := range map[string][]byte{
		certFile: certPEM, keyFile: keyPEM, caFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.ca.Raw}),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			p.t.Fatal(err)
		}
	}
	return certFile, keyFile, caFile
}

// Fixture identities.
var (
	serviceAddr     = bytes.Repeat([]byte{0x11}, 20)
	broadcasterAddr = bytes.Repeat([]byte{0x22}, 20)
	intentSeed      = sha256.Sum256([]byte("api test intent key"))
	providerSeed    = sha256.Sum256([]byte("api test provider key"))
	otherSeed       = sha256.Sum256([]byte("api test other key"))
)

func intentKey() ed25519.PrivateKey   { return ed25519.NewKeyFromSeed(intentSeed[:]) }
func providerKey() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(providerSeed[:]) }
func otherKey() ed25519.PrivateKey    { return ed25519.NewKeyFromSeed(otherSeed[:]) }

// testCallers: the app fixture may send every key-signed content and lease
// kind; the provider agent is bound to provider-1 and relays or forwards
// only; the relay agent may relay or forward but is bound to no provider.
func testCallers() []policy.Caller {
	return []policy.Caller{
		{Principal: "app-fixture", Kinds: []policy.Kind{policy.KindRegisterContent, policy.KindWithdrawContent,
			policy.KindOfferLease, policy.KindRelayAcceptLease, policy.KindRelayRenewLease, policy.KindRelayEndLease}},
		{Principal: "other-app", Kinds: []policy.Kind{policy.KindRegisterContent, policy.KindWithdrawContent,
			policy.KindOfferLease}},
		{Principal: "provider-agent", Kinds: []policy.Kind{policy.KindRelayEndLease, policy.KindRelayRenewLease,
			policy.KindForwardProviderTx}, BoundProviderID: "provider-1"},
		{Principal: "relay-agent", Kinds: []policy.Kind{policy.KindRelayEndLease, policy.KindForwardProviderTx}},
	}
}

// env is one running test server.
type env struct {
	t      *testing.T
	pki    *pki
	srv    *Server
	st     *fakeStore
	ch     *fakeChain
	base   string
	cfg    Config
	cancel context.CancelFunc
	done   chan error
}

func testConfig(certFile, keyFile, caFile, listen string) Config {
	return Config{
		ChainID: testChainID, ListenAddress: listen, TLSCertFile: certFile, TLSKeyFile: keyFile, ClientCAFile: caFile,
		Callers: testCallers(), MaxBodyBytes: 65536, MaxAuthContextBytes: 2048, PageSize: 100,
		MinLifetime: 30 * time.Second, MaxLifetime: 24 * time.Hour, RelayLocalExpiry: time.Hour, ForwardWindow: 100,
		DeadlinePages: 10, ObserverFreshness: 2 * time.Second,
	}
}

func testDeps(t *testing.T, st Store, ch ChainReader) Deps {
	t.Helper()
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := policy.ParseRegistry([]byte(`{"entries":[{"principal_id":"creator-7","public_key_hex":"` +
		hex.EncodeToString(intentKey().Public().(ed25519.PublicKey)) + `","label":"test creator"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return Deps{
		Store: st, Chain: ch, Codec: codec, Registry: reg,
		Signers: []SignerKey{
			{Class: policy.ClassService, Address: serviceAddr, PublicKey: bytes.Repeat([]byte{2}, 33), Algorithm: "secp256k1"},
			{Class: policy.ClassBroadcaster, Address: broadcasterAddr, PublicKey: bytes.Repeat([]byte{3}, 33), Algorithm: "secp256k1"},
		},
	}
}

// newEnv builds the server with New and serves it with Serve on a
// loopback listener.
func newEnv(t *testing.T) *env {
	t.Helper()
	p := newPKI(t)
	certFile, keyFile, caFile := p.serverFiles()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(certFile, keyFile, caFile, ln.Addr().String())
	st, ch := newFakeStore(), newFakeChain(testChainID)
	srv, err := New(cfg, testDeps(t, st, ch))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &env{t: t, pki: p, srv: srv, st: st, ch: ch, base: "https://" + ln.Addr().String(), cfg: cfg, cancel: cancel,
		done: make(chan error, 1)}
	go func() { e.done <- srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-e.done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("serve did not stop")
		}
	})
	return e
}

// httpClient presents cert (none when cert is nil) and trusts roots.
func httpClient(roots *x509.CertPool, cert *tls.Certificate) *http.Client {
	cfg := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}}
}

// as returns a client for principal.
func (e *env) as(principal string) *http.Client {
	c := e.pki.client("svn://principal/" + principal)
	return httpClient(e.pki.caPool, &c)
}

// answer is one response.
type answer struct {
	status int
	body   []byte
}

func (a answer) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(a.body, &m); err != nil {
		t.Fatalf("answer is not JSON: %v: %s", err, a.body)
	}
	return m
}

func (a answer) code(t *testing.T) string {
	t.Helper()
	e, ok := a.json(t)["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error object: %s", a.body)
	}
	return e["code"].(string)
}

func (e *env) do(c *http.Client, method, path string, body []byte) answer {
	e.t.Helper()
	req, err := http.NewRequest(method, e.base+path, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		e.t.Fatalf("content type %q", ct)
	}
	return answer{status: resp.StatusCode, body: b}
}

// Request builders.

var (
	manifestDigest = sha256.Sum256([]byte("manifest"))
	reasonDigest   = sha256.Sum256([]byte("reason"))
	termsDigest    = sha256.Sum256([]byte("terms"))
)

func authContext(principal string, perms ...string) map[string]any {
	return map[string]any{"principal_kind": "DEV_FIXTURE", "principal_id": principal, "permissions": perms,
		"policy_ref": "policy-1"}
}

func innerSig(domain, keyID string, pub ed25519.PublicKey, sig []byte) map[string]any {
	return map[string]any{"domain": domain, "key_id": keyID, "public_key_hex": hex.EncodeToString(pub),
		"signature_hex": hex.EncodeToString(sig), "canonical_profile_version": 1}
}

// registerRequest is a REGISTER_CONTENT request whose hosting intent the
// key k signs for the service signer as submitting host.
func registerRequest(t *testing.T, contentID string, k ed25519.PrivateKey) map[string]any {
	t.Helper()
	sb, err := content.RegisterIntentSignBytes(serviceAddr, contentID, 1, manifestDigest[:], 4096, content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"content_id": contentID, "content_version": 1, "manifest_digest_hex": hex.EncodeToString(manifestDigest[:]),
		"package_bytes": 4096,
		"hosting_intent": innerSig("SIGNATURE_DOMAIN_HOSTING_INTENT", "intent-key-1", k.Public().(ed25519.PublicKey),
			ed25519.Sign(k, sb)),
	}
}

func withdrawRequest(t *testing.T, contentID string, k ed25519.PrivateKey) map[string]any {
	t.Helper()
	sb, err := content.WithdrawIntentSignBytes(serviceAddr, contentID, 1, reasonDigest[:], content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"content_id": contentID, "content_version": 1, "reason_digest_hex": hex.EncodeToString(reasonDigest[:]),
		"hosting_intent": innerSig("SIGNATURE_DOMAIN_HOSTING_INTENT", "intent-key-1", k.Public().(ed25519.PublicKey),
			ed25519.Sign(k, sb)),
	}
}

func offerRequest(contentID, offerID string) map[string]any {
	return map[string]any{
		"offer_id": offerID, "content_id": contentID, "content_version": 1, "provider_id": "provider-1",
		"manifest_digest_hex": hex.EncodeToString(manifestDigest[:]), "package_bytes": 4096,
		"terms_digest_hex": hex.EncodeToString(termsDigest[:]), "start_unix_seconds": 1800000000,
		"deadline_unix_seconds": 1800003600, "expiry_unix_seconds": 1800000600,
	}
}

func envelope(t *testing.T, kind, key string, expires int64, auth, req map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"kind": kind, "idempotency_key": key, "expires_in_seconds": expires, "auth_context": auth, "request": req,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
