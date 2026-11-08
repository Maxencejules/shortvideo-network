package devnet_test

// Authorization phases of TestDevnetBroadcasterLifecycle: UA (unauthorized
// callers, roles, signer requests, signer start-ups and mTLS), PI
// (provider impersonation), FW (verbatim forwards) and PB (the
// publication permission).

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/lib/pq"
	"google.golang.org/protobuf/proto"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// bcSignerRequest and bcSignerResponse are the signer's wire frames
// (services/broadcaster/internal/signer/protocol.go): a 4-byte big-endian
// length, then one JSON document.
type bcSignerRequest struct {
	Version       int    `json:"v"`
	Op            string `json:"op"`
	KeyName       string `json:"key_name,omitempty"`
	ChainID       string `json:"chain_id,omitempty"`
	AccountNumber uint64 `json:"account_number,omitempty"`
	Sequence      uint64 `json:"sequence,omitempty"`
	Gas           uint64 `json:"gas,omitempty"`
	TimeoutHeight uint64 `json:"timeout_height,omitempty"`
	TypeURL       string `json:"type_url,omitempty"`
	Value         []byte `json:"value,omitempty"`
}

type bcSignerResponse struct {
	Refusal *struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"refusal"`
	TxBytes []byte `json:"tx_bytes"`
}

// dialSignerAsServe connects to the signer's socket with the serve uid's
// credentials: under a root launcher, on a locked OS thread whose uid and
// gid are switched with raw setresgid/setresuid calls (they change only
// the calling thread; the saved uid stays 0, so the thread can switch
// back), so SO_PEERCRED reports the serve uid. Once the thread is root
// again it is unlocked and returns to the scheduler: a thread that exits
// would deliver Pdeathsig to every child it once started. Only a thread
// whose credentials could not be restored stays locked and ends with its
// goroutine. This is a test-only client: it shows what the signer refuses
// even from its one allowed peer.
func (n *bnet) dialSignerAsServe() net.Conn {
	n.t.Helper()
	if n.serveID.uid == n.me.uid {
		conn, err := net.Dial("unix", n.signerSock)
		if err != nil {
			n.t.Fatal(err)
		}
		return conn
	}
	type dialed struct {
		conn net.Conn
		err  error
	}
	out := make(chan dialed, 1)
	go func() {
		runtime.LockOSThread()
		uid, gid := uintptr(n.serveID.uid), uintptr(n.serveID.uid)
		if _, _, e := syscall.RawSyscall(syscall.SYS_SETRESGID, gid, gid, 0); e != 0 {
			out <- dialed{err: fmt.Errorf("setresgid: %v", e)}
			return
		}
		if _, _, e := syscall.RawSyscall(syscall.SYS_SETRESUID, uid, uid, 0); e != 0 {
			out <- dialed{err: fmt.Errorf("setresuid: %v", e)}
			return
		}
		conn, err := net.Dial("unix", n.signerSock)
		_, _, e1 := syscall.RawSyscall(syscall.SYS_SETRESUID, 0, 0, 0)
		_, _, e2 := syscall.RawSyscall(syscall.SYS_SETRESGID, 0, 0, 0)
		if e1 != 0 || e2 != 0 {
			out <- dialed{conn: conn, err: fmt.Errorf("restore credentials: %v %v (dial error %v)", e1, e2, err)}
			return
		}
		runtime.UnlockOSThread()
		out <- dialed{conn: conn, err: err}
	}()
	d := <-out
	if d.err != nil {
		n.t.Fatalf("dial the signer as the serve uid: %v", d.err)
	}
	return d.conn
}

// askSigner sends one request to the signer as the serve uid and returns
// its answer.
func (n *bnet) askSigner(req bcSignerRequest) bcSignerResponse {
	n.t.Helper()
	conn := n.dialSignerAsServe()
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		n.t.Fatal(err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		n.t.Fatal(err)
	}
	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame, uint32(len(body)))
	copy(frame[4:], body)
	if _, err := conn.Write(frame); err != nil {
		n.t.Fatal(err)
	}
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		n.t.Fatal(err)
	}
	size := binary.BigEndian.Uint32(head[:])
	if size == 0 || size > 256<<10 {
		n.t.Fatalf("signer frame of %d bytes", size)
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(conn, raw); err != nil {
		n.t.Fatal(err)
	}
	var resp bcSignerResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		n.t.Fatal(err)
	}
	return resp
}

// signerRefuses requires the signer to refuse req with code.
func (n *bnet) signerRefuses(phase, what string, req bcSignerRequest, code string) {
	n.t.Helper()
	resp := n.askSigner(req)
	if resp.Refusal == nil || resp.Refusal.Code != code || len(resp.TxBytes) != 0 {
		got := "no refusal"
		if resp.Refusal != nil {
			got = resp.Refusal.Code + ": " + resp.Refusal.Detail
		}
		n.t.Fatalf("phase %s: signer request %s answered %s (%d tx bytes), want refusal %s", phase, what, got, len(resp.TxBytes), code)
	}
	fmt.Printf("phase=%s signer_request=%q refused=%s\n", phase, what, code)
}

// bcTestCA is a CA the test makes in memory to issue client certificates
// the dev-ca cannot (zero or two principal SANs, an expired certificate).
type bcTestCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newBcTestCA(t interface{ Fatal(...any) }, name string) bcTestCA {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return bcTestCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// client issues a client certificate with the given URI SANs, valid from
// notBefore to notAfter, and returns an HTTPS client presenting it.
func (ca bcTestCA) client(t interface{ Fatal(...any) }, roots *x509.CertPool, uris []string, notBefore, notAfter time.Time) *http.Client {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var parsed []*url.URL
	for _, u := range uris {
		p, err := url.Parse(u)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, p)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "devnet test client"},
		NotBefore: notBefore, NotAfter: notAfter, URIs: parsed,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}},
	}}
}

// tlsRefused requires a create through client to fail before any answer
// (the TLS handshake or the first read fails) and to leave no row.
func (n *bnet) tlsRefused(phase, what string, client *http.Client, base string) {
	n.t.Helper()
	req := n.registerReq(creatorOne, "cua-tls", sizeOne)
	body, err := json.Marshal(req)
	if err != nil {
		n.t.Fatal(err)
	}
	resp, err := client.Post(base+"/v1/intents", "application/json", bytes.NewReader(body))
	if err == nil {
		_ = resp.Body.Close()
		n.t.Fatalf("phase %s: %s: the request got an answer %d, want a TLS refusal", phase, what, resp.StatusCode)
	}
	n.noIntentWithKey(phase, req.IdempotencyKey)
	fmt.Printf("phase=%s tls_refused=%q rows=0\n", phase, what)
}

// statusRefused requires a create through client to answer status and
// code and to leave no row.
func (n *bnet) statusRefused(phase, what string, client *http.Client, base string, wantStatus int, wantCode string) {
	n.t.Helper()
	req := n.registerReq(creatorOne, "cua-san", sizeOne)
	body, err := json.Marshal(req)
	if err != nil {
		n.t.Fatal(err)
	}
	status, _, code := n.call(client, http.MethodPost, base+"/v1/intents", body)
	if status != wantStatus || code != wantCode {
		n.t.Fatalf("phase %s: %s: %d %s, want %d %s", phase, what, status, code, wantStatus, wantCode)
	}
	n.noIntentWithKey(phase, req.IdempotencyKey)
	fmt.Printf("phase=%s refused=%q http=%d code=%s rows=0\n", phase, what, status, code)
}

// sqlRefused requires a SQL-path intent of caller (enqueued through db) to
// end FAILED(reason) with no attempt and no forwarded broadcast.
func (n *bnet) sqlRefused(phase, what, caller string, db *sql.DB, req bcIntentReq, reason string) {
	n.t.Helper()
	forwarded := n.proxy.count(broadcastMethod).Forwarded
	id := n.enqueueAs(caller, db, req)
	n.failed(phase, id, reason)
	if rows := n.attempts(id); len(rows) != 0 {
		n.t.Fatalf("phase %s: %s left %d attempts", phase, what, len(rows))
	}
	if n.proxy.count(broadcastMethod).Forwarded != forwarded {
		n.t.Fatalf("phase %s: %s forwarded a broadcast", phase, what)
	}
	fmt.Printf("phase=%s sql_path=%q result=FAILED(%s) attempts=0 forwards=0\n", phase, what, reason)
}

// enqueueAs calls broadcaster.enqueue_intent as the role of db, the SQL
// role of caller (no application row), and returns the intent id.
func (n *bnet) enqueueAs(caller string, db *sql.DB, req bcIntentReq) string {
	n.t.Helper()
	requestJSON, err := json.Marshal(req.Request)
	if err != nil {
		n.t.Fatal(err)
	}
	authJSON, err := json.Marshal(req.AuthContext)
	if err != nil {
		n.t.Fatal(err)
	}
	ctx, cancel := bcCtx()
	defer cancel()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT intent_id::text FROM broadcaster.enqueue_intent($1, $2::jsonb, $3::jsonb, $4, $5::interval)`,
		req.Kind, string(requestJSON), string(authJSON), req.IdempotencyKey, fmt.Sprintf("%d seconds", req.ExpiresInSeconds)).Scan(&id); err != nil {
		n.t.Fatalf("enqueue_intent %s: %v", req.Kind, err)
	}
	obj := req.object
	obj.Caller = caller
	n.objects[id] = obj
	return id
}

// phaseUnauthorized is phase UA.
func phaseUnauthorized(n *bnet, name string) {
	d := n.d
	// Caller without the kind, HTTP and SQL path.
	deadline, expiry := n.terms()
	n.ownContent(name, callerApp, sizeOne, "cua")
	offer := n.offerOf("oua", "cua", "p2", sizeOne, deadline, expiry)
	n.refused(name, callerLimited, n.offerReq(creatorOne, offer), http.StatusForbidden, "KIND_NOT_ALLOWED")
	limited := bcOpenDB(n.t, n.db, roleLimitedSQL, n.passFile[roleLimitedSQL])
	n.sqlRefused(name, "limited-caller OFFER_LEASE", callerLimited, limited, n.offerReq(creatorOne, offer), "UNAUTHORIZED")
	// Another caller's intent and another caller's content.
	own := n.create(callerApp, n.registerReq(creatorOne, "cua-read", sizeOne))
	if status, _, code := n.get(callerOther, own.IntentID); status != http.StatusNotFound || code != "NOT_FOUND" {
		n.t.Fatalf("phase %s: another caller's read: %d %s", name, status, code)
	}
	n.committed(name, own.IntentID)
	fmt.Printf("phase=%s other_caller_read=404\n", name)
	n.refused(name, callerOther, n.withdrawReq(creatorOne, "cua"), http.StatusNotFound, "NOT_FOUND")
	// SQL path without PUBLISH_CONTENT.
	noPublish := n.registerReq(creatorOne, "cua-nopub", sizeOne)
	noPublish.AuthContext = auth(creatorOne, "SCHEDULE_HOSTING")
	n.sqlRefused(name, "REGISTER_CONTENT without PUBLISH_CONTENT", callerApp, n.sqlCaller, noPublish, "UNAUTHORIZED")
	// A role not in callers.
	stranger := bcOpenDB(n.t, n.db, roleStrangerDB, n.passFile[roleStrangerDB])
	ctx, cancel := bcCtx()
	reqJSON, _ := json.Marshal(n.registerReq(creatorOne, "cua-stranger", sizeOne).Request)
	_, err := stranger.ExecContext(ctx, `SELECT broadcaster.enqueue_intent('REGISTER_CONTENT', $1::jsonb, '{}'::jsonb, 'stranger-1', '60 seconds'::interval)`, string(reqJSON))
	cancel()
	var pqErr *pq.Error
	if err == nil || !errors.As(err, &pqErr) || (pqErr.Code != "42501" && pqErr.Code != "SV003") {
		n.t.Fatalf("phase %s: a role not in callers called enqueue_intent: %v", name, err)
	}
	n.noIntentWithKey(name, "stranger-1")
	fmt.Printf("phase=%s sql_role_not_in_callers=refused sqlstate=%s rows=0\n", name, pqErr.Code)

	// The signer asked directly by its one allowed peer uid.
	n.d.syncSequence(n.bcast)
	height := uint64(d.height())
	value, err := proto.MarshalOptions{Deterministic: true}.Marshal(offerWith(offer, n.bcastAddr(), "ua-signer"))
	if err != nil {
		n.t.Fatal(err)
	}
	base := bcSignerRequest{Version: 1, Op: "sign", ChainID: d.chainID, Gas: 2_000_000}
	r := base
	r.KeyName, r.AccountNumber, r.Sequence, r.TimeoutHeight = "broadcaster", d.numbers[n.bcast.addr], d.seq[n.bcast.addr], height+10
	r.TypeURL, r.Value = "/svn.leases.v1.MsgOfferLease", value
	n.signerRefuses(name, "MsgOfferLease with the broadcaster key", r, "TYPE_NOT_ALLOWED")
	// The other direction: the service key never signs provider consent.
	acceptValue, err := proto.MarshalOptions{Deterministic: true}.Marshal(&leasesv1.MsgAcceptLease{Signer: n.serviceAddr(),
		BusinessId: "ua-signer-accept", OfferId: offer.GetOfferId(), LeaseId: "lua-signer", ProviderAcceptance: n.acceptSig(offer, "lua-signer", d.keys["p2"])})
	if err != nil {
		n.t.Fatal(err)
	}
	r = base
	r.KeyName, r.AccountNumber, r.Sequence, r.TimeoutHeight = "service", d.numbers[n.service.addr], d.seq[n.service.addr], height+10
	r.TypeURL, r.Value = "/svn.leases.v1.MsgAcceptLease", acceptValue
	n.signerRefuses(name, "MsgAcceptLease with the service key", r, "TYPE_NOT_ALLOWED")
	// The demo's intent key is in no registry entry.
	regMsg := d.registerContent(n.service, "ua-signer-reg", "cua-signer", 1, sizeOne)
	regValue, err := proto.MarshalOptions{Deterministic: true}.Marshal(regMsg)
	if err != nil {
		n.t.Fatal(err)
	}
	r = base
	r.KeyName, r.AccountNumber, r.TimeoutHeight = "service", d.numbers[n.service.addr], height+10
	r.TypeURL, r.Value = "/svn.content.v1.MsgRegisterContent", regValue
	n.signerRefuses(name, "register with an unregistered intent key", r, "INTENT_KEY_UNREGISTERED")
	// A message the service key may sign, so the deadline checks decide.
	serviceValue, err := proto.MarshalOptions{Deterministic: true}.Marshal(offerWith(offer, n.serviceAddr(), "ua-timeout"))
	if err != nil {
		n.t.Fatal(err)
	}
	r.Value = serviceValue
	r.KeyName, r.TypeURL, r.TimeoutHeight = "service", "/svn.leases.v1.MsgOfferLease", 0
	n.signerRefuses(name, "timeout_height 0", r, "TIMEOUT_ZERO")
	r.TimeoutHeight = height + uint64(n.numbers["signer_max_timeout_offset_blocks"].(int)) + 50
	n.signerRefuses(name, "timeout_height above the bound", r, "TIMEOUT_TOO_FAR")

	// Signer start-ups with keys the broadcaster may never hold.
	for _, k := range []struct{ account, class string }{
		{"authority", "SERVICE"}, {"auditor-1", "BROADCASTER"}, {"provider-1", "BROADCASTER"}, {"seat-1", "SERVICE"},
	} {
		n.signerStartRefused(name, k.account, k.class)
	}

	// mTLS against the running binary.
	n.tlsRefused(name, "no client certificate", n.mtlsClient("", ""), n.apiURL())
	other := newBcTestCA(n.t, "another CA")
	roots := x509.NewCertPool()
	pemCA, err := os.ReadFile(n.ca.CACert)
	if err != nil || !roots.AppendCertsFromPEM(pemCA) {
		n.t.Fatalf("CA: %v", err)
	}
	now := time.Now()
	good := []string{"svn://principal/" + callerApp}
	n.tlsRefused(name, "certificate from another CA", other.client(n.t, roots, good, now.Add(-time.Hour), now.Add(time.Hour)), n.apiURL())
	n.uaTrustedTestCA(name, other, roots, now)
}

// uaTrustedTestCA starts a second serve process that trusts the test's own
// CA (and no other), so certificates the dev-ca cannot issue reach the
// principal check: zero and two principal SANs answer 401, an expired
// certificate fails the handshake, and a well-formed one is served.
func (n *bnet) uaTrustedTestCA(name string, ca bcTestCA, roots *x509.CertPool, now time.Time) {
	n.t.Helper()
	caFile := filepath.Join(n.base, "etc", "ua-client-ca.pem")
	bcWriteFile(n.t, caFile, ca.pem, 0o644, n.me)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		n.t.Fatal(err)
	}
	listen := ln.Addr().String()
	if err := ln.Close(); err != nil {
		n.t.Fatal(err)
	}
	cfg := n.writeServeConfig("serve-ua.json", listen, n.proxy.url(), nil)
	raw, err := os.ReadFile(cfg)
	if err != nil {
		n.t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		n.t.Fatal(err)
	}
	parsed["listen"].(map[string]any)["client_ca_file"] = caFile
	uaCfg := filepath.Join(n.base, "etc", "serve-ua-ca.json")
	bcWriteJSON(n.t, uaCfg, parsed)
	p := n.launch.start("serve-ua", n.serveID, []string{"SVN_FAULTPOINT_DIR=" + n.faultServe}, "serve", "--config", uaCfg)
	base := "https://" + listen
	control := ca.client(n.t, roots, []string{"svn://principal/" + callerApp}, now.Add(-time.Hour), now.Add(time.Hour))
	bcWait(n.t, "serve-ua health", 2*time.Minute, func() bool {
		resp, err := control.Get(base + "/v1/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, p)
	fmt.Printf("phase=%s control=test-CA certificate with one principal SAN served (health 200)\n", name)
	n.statusRefused(name, "zero principal SANs", ca.client(n.t, roots, nil, now.Add(-time.Hour), now.Add(time.Hour)), base,
		http.StatusUnauthorized, "UNAUTHENTICATED")
	n.statusRefused(name, "two principal SANs", ca.client(n.t, roots, []string{"svn://principal/" + callerApp, "svn://principal/" + callerOther},
		now.Add(-time.Hour), now.Add(time.Hour)), base, http.StatusUnauthorized, "UNAUTHENTICATED")
	n.tlsRefused(name, "expired certificate", ca.client(n.t, roots, []string{"svn://principal/" + callerApp}, now.Add(-48*time.Hour), now.Add(-24*time.Hour)), base)
	if code := p.stop(n.t); code != 0 {
		n.t.Fatalf("phase %s: serve-ua exit %d: %s", name, code, p.logTail(5))
	}
}

// signerStartRefused starts a signer whose configuration names account's
// key (copied from the devnet home) as class, and requires it to refuse to
// start: a non-zero exit and no socket.
func (n *bnet) signerStartRefused(phase, account, class string) {
	n.t.Helper()
	dir := filepath.Join(n.base, "keys-ua-"+account)
	bcMkdir(n.t, dir, 0o700, n.signID)
	raw := readHexFile(n.t, filepath.Join(n.home, "accounts", account+".hex"))
	keyPath := filepath.Join(dir, "key.key")
	bcWriteFile(n.t, keyPath, []byte(hex.EncodeToString(raw)+"\n"), 0o600, n.signID)
	sock := filepath.Join(n.base, "sock", "ua-"+account+".sock")
	other := map[string]any{"name": "service", "class": "SERVICE", "path": filepath.Join(n.base, "keys", "service.key")}
	if class == "SERVICE" {
		other = map[string]any{"name": "broadcaster", "class": "BROADCASTER", "path": filepath.Join(n.base, "keys", "broadcaster.key")}
	}
	cfg := filepath.Join(n.base, "etc", "signer-ua-"+account+".json")
	bcWriteJSON(n.t, cfg, map[string]any{"chain_id": n.layout.Topology.ChainID, "rpc_url": n.rpcs[0],
		"signer":              map[string]any{"socket_path": sock, "serve_uid": n.serveID.uid, "signer_uid": n.signID.uid},
		"intent_key_registry": n.registry, "numbers": n.numbers,
		"keys": []any{map[string]any{"name": "forbidden", "class": class, "path": keyPath}, other}})
	code, p := n.launch.run("signer-ua-"+account, n.signID, "signer", "--config", cfg)
	if code == 0 {
		n.t.Fatalf("phase %s: a signer holding the %s key as %s started", phase, account, class)
	}
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		n.t.Fatalf("phase %s: the refused signer left a socket: %v", phase, err)
	}
	fmt.Printf("phase=%s signer_start key=%s class=%s exit=%d refused=true reason=%s\n", phase, account, class, code, lastErrorMessage(p.log()))
}

// lastErrorMessage returns the error field of the last JSON log line.
func lastErrorMessage(log string) string {
	lines := bytes.Split(bytes.TrimSpace([]byte(log)), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var line struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(lines[i], &line) == nil && line.Error != "" {
			return strconv.Quote(line.Error)
		}
	}
	return `""`
}

// edFromSecret derives an ed25519 key from an account key's 32 secret
// bytes: a signature "by the service key" as far as an inner signature
// (which is always ed25519) can be one.
func edFromSecret(id string, secret []byte) ed25519Key {
	return ed25519Key{id: id, priv: ed25519.NewKeyFromSeed(secret[:ed25519.SeedSize])}
}

// phaseImpersonation is phase PI: a relay acceptance signed by keys other
// than the provider's current key, and a renewal and an end signed with
// p1's key before its rotation, are each refused by the pre-check with no
// row; the forged acceptance, sent by the test from p1's own account, is
// refused on chain with UNAUTHORIZED; p1's reservation and held leases do
// not change.
func phaseImpersonation(n *bnet, name string) {
	d, pi := n.d, n.pi
	n.ownContent(name, callerApp, sizeOne, pi.contentID, "cpi2")
	_, expiry := n.terms()
	offer := n.offerOf(pi.offerID, pi.contentID, "p1", sizeOne, pi.deadline, expiry)
	n.offerCommitted(name, callerApp, offer)
	n.leaseCommitted(name, offer, pi.leaseID)
	deadline2, _ := n.terms()
	offer2 := n.offerOf("opi2", "cpi2", "p1", sizeOne, deadline2, expiry)
	n.offerCommitted(name, callerApp, offer2)
	height := d.height()
	reserved, held := d.reservedAt(d.clients[0], "p1", height), d.heldOn(d.clients[0], "p1", height)

	serviceSecret := readHexFile(n.t, filepath.Join(n.home, "accounts", "service.hex"))
	bcastSecret := readHexFile(n.t, filepath.Join(n.home, "accounts", "broadcaster.hex"))
	forgers := []struct {
		label string
		key   ed25519Key
	}{
		{"service key (ed25519 from its secret)", edFromSecret("svc-forged", serviceSecret)},
		{"broadcaster key (ed25519 from its secret)", edFromSecret("bcast-forged", bcastSecret)},
		{"provider-2 key", d.keys["p2"]},
		{"provider-1 retired key", pi.firstKey},
		{"random key", newKey("random-key")},
	}
	for _, f := range forgers {
		n.refused(name+" "+f.label, callerApp, n.acceptReq(creatorOne, offer2, "lpi2", n.acceptSig(offer2, "lpi2", f.key)),
			http.StatusUnprocessableEntity, "PROVIDER_SIGNATURE_INVALID")
	}
	oldRenew := &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, PublicKey: pi.firstKey.pub(),
		Signature: pi.oldRenew, KeyId: pi.firstKey.id, CanonicalProfileVersion: 1}
	oldEnd := &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, PublicKey: pi.firstKey.pub(),
		Signature: pi.oldEnd, KeyId: pi.firstKey.id, CanonicalProfileVersion: 1}
	n.refused(name+" renew-before-rotation", callerApp, n.renewReq(creatorOne, pi.leaseID, "p1", pi.contentID, sizeOne, 2, pi.renewDeadline, oldRenew),
		http.StatusUnprocessableEntity, "PROVIDER_SIGNATURE_INVALID")
	n.refused(name+" end-before-rotation", callerApp, n.endReq(creatorOne, pi.leaseID, "p1", pi.contentID, oldEnd),
		http.StatusUnprocessableEntity, "PROVIDER_SIGNATURE_INVALID")

	// The forced chain check: the forged acceptance from p1's own account.
	forged := d.acceptMsg(d.provider1, "pi-forced", offer2, "lpi2", newKey("random-key-2"))
	d.syncSequence(d.provider1)
	res := d.send(d.provider1, forged)
	unauthorized := uint32(typesv1.ErrorCode_ERROR_CODE_UNAUTHORIZED)
	if res.deliver.Codespace != "svn" || res.deliver.Code != unauthorized {
		n.t.Fatalf("phase %s: the forged acceptance from p1's account: deliver %s/%d %s", name, res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
	}
	after := d.height()
	if got := d.reservedAt(d.clients[0], "p1", after); got != reserved {
		n.t.Fatalf("phase %s: p1 reserved %d, %d before", name, got, reserved)
	}
	if got := d.heldOn(d.clients[0], "p1", after); fmt.Sprint(got) != fmt.Sprint(held) {
		n.t.Fatalf("phase %s: p1 holds %v, %v before", name, got, held)
	}
	fmt.Printf("phase=%s forgers=%d refused_at_precheck=7 rows=0 forced_chain_check=svn/%d tx=%X p1_reserved=%d unchanged held=%v unchanged\n",
		name, len(forgers), unauthorized, res.hash, reserved, held)
}

// phaseForward is phase FW: p1's account-signed acceptance forwarded
// verbatim commits by its own hash; one without a timeout height and one
// with a forged inner signature are refused with no row.
func phaseForward(n *bnet, name string) {
	d := n.d
	offer := n.relaySetup(name, "cfw", "ofw", "p1")
	d.syncSequence(d.provider1)
	noTimeout := n.signTimeout(d.provider1, 0, d.acceptMsg(d.provider1, "fw-a", offer, "lfw", d.keys["p1"]))
	n.refused(name+" no-timeout-height", callerApp, n.forwardReq(creatorOne, noTimeout, "p1", "cfw", "lfw"),
		http.StatusUnprocessableEntity, "FORWARD_DEADLINE")
	forgedSig := n.signTimeout(d.provider1, uint64(d.height()+30), d.acceptMsg(d.provider1, "fw-b", offer, "lfw", newKey("forged-key")))
	n.refused(name+" forged-inner-signature", callerApp, n.forwardReq(creatorOne, forgedSig, "p1", "cfw", "lfw"),
		http.StatusUnprocessableEntity, "PROVIDER_SIGNATURE_INVALID")
	raw := n.signTimeout(d.provider1, uint64(d.height()+30), d.acceptMsg(d.provider1, "fw-c", offer, "lfw", d.keys["p1"]))
	v := n.committed(name, n.create(callerApp, n.forwardReq(creatorOne, raw, "p1", "cfw", "lfw")).IntentID)
	if !bytes.Equal(n.hashOf(v.Commitment.FirstCommitTxHash), sha256Of(raw)) {
		n.t.Fatalf("phase %s: the forward committed %s, not the provider's bytes %X", name, v.Commitment.FirstCommitTxHash, sha256Of(raw))
	}
	fmt.Printf("phase=%s forwarded_verbatim=%X committed_by_own_hash=true refused=no-timeout-height,forged-inner-signature\n", name, sha256Of(raw))
}

// phasePublication is phase PB: a register without PUBLISH_CONTENT, a
// register whose hosting intent is signed with a key not registered for
// the principal, and a withdraw by another principal are refused with no
// row and no transaction.
func phasePublication(n *bnet, name string) {
	n.ownContent(name, callerApp, sizeOne, "cpb")
	noPerm := n.registerReq(creatorOne, "cpb-noperm", sizeOne)
	noPerm.AuthContext = auth(creatorOne, "SCHEDULE_HOSTING")
	n.refused(name, callerApp, noPerm, http.StatusForbidden, "PERMISSION_MISSING")
	wrongKey := n.registerReq(creatorTwo, "cpb-wrongkey", sizeOne)
	wrongKey.AuthContext = auth(creatorOne, "PUBLISH_CONTENT")
	n.refused(name, callerApp, wrongKey, http.StatusForbidden, "INTENT_KEY_UNREGISTERED")
	n.refused(name, callerApp, n.registerReq(creatorNoKey, "cpb-nokey", sizeOne), http.StatusForbidden, "INTENT_KEY_UNREGISTERED")
	n.refused(name, callerApp, n.withdrawReq(creatorTwo, "cpb"), http.StatusNotFound, "NOT_FOUND")
	for _, c := range []string{"cpb-noperm", "cpb-wrongkey", "cpb-nokey"} {
		if e := n.objectEffects(bcObject{Event: evRegistered, ID: c, Version: 1, HasVersion: true}); len(e) != 0 {
			n.t.Fatalf("phase %s: %s was registered on chain", name, c)
		}
	}
	if e := n.objectEffects(bcObject{Event: evWithdrawn, ID: "cpb", Version: 1, HasVersion: true}); len(e) != 0 {
		n.t.Fatalf("phase %s: cpb was withdrawn on chain", name)
	}
	fmt.Printf("phase=%s refused=no-permission,unregistered-intent-key(x2),withdraw-by-other-principal rows=0 transactions=0\n", name)
}
