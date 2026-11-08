package main

// TestSmokeProcesses is a unit-labelled smoke test, not end-to-end
// evidence: it builds the svn-broadcaster binary and runs dev-ca, dev-db,
// migrate, signer, serve, status, reconcile-once and signer-clear as real
// processes against a real PostgreSQL cluster (started by dev-db) and a
// fake chain RPC on loopback (fakenode_test.go). The real chain is
// exercised by the devnet test of Wave 2 (B9). Subtest same-uid runs the
// signer and serve under one uid (the test's own when it is not root, as
// in CI; one unused uid when it is root). When the test runs as root,
// subtest distinct-uids also runs them under two distinct unused uids, as
// the devnet launcher does (plan 4.1); only that run exercises the
// process boundary.

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"

	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

const smokeChainID = "shortvideo-dev-smoke"

// pgTestLockName is the machine-wide lock file of internal/pgtest
// (os.TempDir()/svn-pgtest.lock): test binaries that start a PostgreSQL
// cluster hold it, so one cluster runs at a time.
const pgTestLockName = "svn-pgtest.lock"

// takePGLock takes the machine-wide cluster lock, waiting at most 15
// minutes, and releases it at the end of the test.
func takePGLock(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(os.TempDir(), pgTestLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Minute)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			t.Fatalf("PostgreSQL test lock: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Cleanup(func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	})
}

// identity is a process identity of the smoke test.
type identity struct{ uid uint32 }

// cred is the credential that runs a process under id; nil when id is the
// test's own uid.
func (id identity) cred() *syscall.Credential {
	if id.uid == uint32(os.Geteuid()) {
		return nil
	}
	return &syscall.Credential{Uid: id.uid, Gid: id.uid, Groups: []uint32{}}
}

// unusedUIDs returns n numeric uids with no passwd entry and no process.
func unusedUIDs(t *testing.T, n int) []uint32 {
	t.Helper()
	used := map[uint32]bool{}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		info, err := os.Stat(filepath.Join("/proc", p.Name()))
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			used[st.Uid] = true
		}
	}
	var out []uint32
	for uid := uint32(3_710_001); len(out) < n && uid < 3_720_000; uid++ {
		if used[uid] {
			continue
		}
		if _, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
			continue
		}
		if _, err := user.LookupGroupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
			continue
		}
		out = append(out, uid)
	}
	if len(out) != n {
		t.Fatalf("found %d unused uids, want %d", len(out), n)
	}
	return out
}

// proc is one started subcommand process.
type proc struct {
	name   string
	cmd    *exec.Cmd
	stdout *syncBuffer
	stderr *syncBuffer
	done   chan struct{}
	err    error
}

// syncBuffer is a bytes.Buffer safe for one writer and concurrent readers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// smoke is the state of one smoke run.
type smoke struct {
	t       *testing.T
	bin     string
	base    string
	logs    []*proc
	env     []string
	serveID identity
	signID  identity
}

// childEnv is the whole environment of every child: no PG* variable and
// nothing else of the test's environment.
var childEnv = []string{"PATH=/usr/bin:/bin", "TZ=UTC", "LC_ALL=C.UTF-8"}

// start starts the binary with args under id.
func (s *smoke) start(name string, id identity, args ...string) *proc {
	s.t.Helper()
	p := &proc{name: name, stdout: &syncBuffer{}, stderr: &syncBuffer{}, done: make(chan struct{})}
	p.cmd = exec.Command(s.bin, args...)
	p.cmd.Env = s.env
	p.cmd.Dir = "/"
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Credential: id.cred(), Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := p.cmd.Start(); err != nil {
		s.t.Fatalf("start %s: %v", name, err)
	}
	go func() {
		p.err = p.cmd.Wait()
		close(p.done)
	}()
	s.logs = append(s.logs, p)
	s.t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		// A test that failed early: SIGTERM first, so dev-db removes its
		// cluster and the signer its socket; SIGKILL after 30 s.
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(30 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
	})
	return p
}

// run runs the binary to completion (bounded) and returns its exit code.
func (s *smoke) run(name string, id identity, args ...string) (int, *proc) {
	s.t.Helper()
	p := s.start(name, id, args...)
	select {
	case <-p.done:
	case <-time.After(2 * time.Minute):
		s.t.Fatalf("%s did not finish: %s", name, p.stderr.String())
	}
	return exitOf(p), p
}

// exitOf returns a finished process's exit code (-1 for a signal).
func exitOf(p *proc) int {
	if p.err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(p.err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// stop sends SIGTERM and waits (bounded) for the exit code.
func (s *smoke) stop(p *proc) int {
	s.t.Helper()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		s.t.Fatalf("signal %s: %v", p.name, err)
	}
	select {
	case <-p.done:
	case <-time.After(time.Minute):
		s.t.Fatalf("%s did not stop on SIGTERM: %s", p.name, p.stderr.String())
	}
	return exitOf(p)
}

// mkdir creates dir with mode, owned by id.
func mkdir(t *testing.T, dir string, mode os.FileMode, id identity) {
	t.Helper()
	if err := os.Mkdir(dir, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	if id.uid != uint32(os.Geteuid()) {
		if err := os.Chown(dir, int(id.uid), int(id.uid)); err != nil {
			t.Fatal(err)
		}
	}
}

// writeFile writes data to a new file with mode, owned by id.
func writeFile(t *testing.T, path string, data []byte, mode os.FileMode, id identity) {
	t.Helper()
	if err := writeNew(path, data, mode, id.uid, uint32(os.Geteuid())); err != nil {
		t.Fatal(err)
	}
}

// writeJSON writes v as a 0644 configuration file.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, b, 0o644, identity{uint32(os.Geteuid())})
}

// buildBinary builds svn-broadcaster into dir with the test binary's race
// setting.
func buildBinary(t *testing.T, dir string) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go command is needed to build the binary under test: %v", err)
	}
	out := filepath.Join(dir, "svn-broadcaster")
	args := []string{"build", "-o", out}
	if raceEnabled {
		args = append(args, "-race")
	}
	args = append(args, ".")
	cmd := exec.Command(goTool, args...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	if err := os.Chmod(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return out
}

// freePort returns a loopback port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// devKey writes a fresh development secp256k1 account key file and
// returns its address.
func devKey(t *testing.T, path string, id identity) []byte {
	t.Helper()
	sk := secp256k1.GenPrivKey()
	writeFile(t, path, []byte(hex.EncodeToString(sk.Bytes())+"\n"), 0o600, id)
	return sk.PubKey().Address()
}

func TestSmokeProcesses(t *testing.T) {
	takePGLock(t)
	euid := uint32(os.Geteuid())
	binDir, err := os.MkdirTemp("", "svn-smoke-bin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(binDir); err != nil {
			t.Errorf("remove %s: %v", binDir, err)
		}
	})
	if err := os.Chmod(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := buildBinary(t, binDir)
	if euid != 0 {
		// The same-uid fallback of a launcher that is not root (CI).
		t.Logf("euid %d is not root: only the same-uid run executes; the distinct-uid run of plan 4.1 needs a root launcher and did not run", euid)
		t.Run("same-uid", func(t *testing.T) { smokeRun(t, bin, identity{euid}, identity{euid}) })
		return
	}
	// A root launcher: the same-uid layout under one unused uid, then the
	// boundary of plan 4.1 with two distinct unused uids.
	uids := unusedUIDs(t, 3)
	t.Run("same-uid", func(t *testing.T) { smokeRun(t, bin, identity{uids[2]}, identity{uids[2]}) })
	t.Run("distinct-uids", func(t *testing.T) { smokeRun(t, bin, identity{uids[0]}, identity{uids[1]}) })
}

// smokeRun is one smoke run with the signer under signID and serve under
// serveID; equal identities are the same-uid fallback.
func smokeRun(t *testing.T, bin string, signID, serveID identity) {
	euid := uint32(os.Geteuid())
	s := &smoke{t: t, env: childEnv, serveID: serveID, signID: signID, bin: bin}
	distinct := signID != serveID
	if distinct {
		t.Logf("boundary_uids=signer:%d,serve:%d", signID.uid, serveID.uid)
	} else {
		t.Logf("boundary_uids=same:%d", serveID.uid)
	}
	me := identity{euid}
	base, err := os.MkdirTemp("", "svn-smoke-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove %s: %v", base, err)
		}
	})
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	s.base = base
	mkdir(t, filepath.Join(base, "etc"), 0o755, me)
	mkdir(t, filepath.Join(base, "secrets"), 0o711, me)
	mkdir(t, filepath.Join(base, "keys"), 0o700, s.signID)
	mkdir(t, filepath.Join(base, "sock"), 0o755, s.signID)

	// Keys, registry and the fake node.
	serviceAddr := devKey(t, filepath.Join(base, "keys", "service.key"), s.signID)
	broadcasterAddr := devKey(t, filepath.Join(base, "keys", "broadcaster.key"), s.signID)
	codec, err := chain.NewCodec(smokeChainID)
	if err != nil {
		t.Fatal(err)
	}
	node := newFakeNode(t, codec, []fakeAccount{
		{address: serviceAddr, number: 5, role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
		{address: broadcasterAddr, number: 6, role: typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER},
	})
	intentPub, intentSecret, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(base, "etc", "intent-keys.json")
	registryMode := os.FileMode(0o644)
	if !distinct {
		registryMode = 0o444
	}
	reg, err := json.Marshal(map[string]any{"entries": []any{map[string]any{
		"principal_id": "creator-1", "public_key_hex": hex.EncodeToString(intentPub), "label": "smoke creator"}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, registryPath, reg, registryMode, me)

	// dev-ca.
	caDir := filepath.Join(base, "ca")
	code, p := s.run("dev-ca", me, "dev-ca", "--out", caDir, "--principal", "app-fixture",
		"--serve-uid", strconv.FormatUint(uint64(s.serveID.uid), 10))
	if code != exitOK {
		t.Fatalf("dev-ca exit %d: %s", code, p.stderr.String())
	}
	var ca caFiles
	if err := json.Unmarshal([]byte(p.stdout.String()), &ca); err != nil || len(ca.Clients) != 1 {
		t.Fatalf("dev-ca output %q: %v", p.stdout.String(), err)
	}

	// dev-db.
	db := s.start("dev-db", me, "dev-db", "--chain-id", smokeChainID, "--secrets-dir", filepath.Join(base, "secrets"),
		"--serve-uid", strconv.FormatUint(uint64(s.serveID.uid), 10), "--login-role", "app_fixture_sql")
	info := readDevDB(t, db)
	if strings.HasPrefix(info.ClusterDir, filepath.Join(os.TempDir(), "svn-pg-")) {
		// Last resort after dev-db stopped (cleanups run in reverse
		// order): a cluster directory dev-db could not remove.
		t.Cleanup(func() {
			if _, err := os.Lstat(info.ClusterDir); err == nil {
				t.Errorf("removing the cluster directory dev-db left: %s", info.ClusterDir)
				if err := os.RemoveAll(info.ClusterDir); err != nil {
					t.Errorf("remove %s: %v", info.ClusterDir, err)
				}
			}
		})
	}
	if info.ChainID != smokeChainID || info.Database != devDatabase || len(info.Roles) != 3 ||
		!strings.HasPrefix(info.ClusterDir, filepath.Join(os.TempDir(), "svn-pg-")) {
		t.Fatalf("dev-db description %+v", info)
	}
	t.Logf("postgres_version=%q", info.PostgresVersion)
	passFile := map[string]string{}
	for _, r := range info.Roles {
		passFile[r.Use] = r.PasswordFile
	}
	dbSection := func(role, use string) map[string]any {
		return map[string]any{"socket_dir": info.SocketDir, "port": info.Port, "name": info.Database, "role": role,
			"password_file": passFile[use], "application_name": "smoke-" + use, "connect_timeout_seconds": 10}
	}
	numbers := map[string]any{"poll_interval_ms": 200, "rpc_timeout_seconds": 2, "intent_lifetime_min_seconds": 5,
		"stall_window_seconds": 3}
	signerSection := map[string]any{"socket_path": filepath.Join(base, "sock", "signer.sock"),
		"serve_uid": s.serveID.uid, "signer_uid": s.signID.uid}
	callers := []any{map[string]any{"principal": "app-fixture", "kinds": []string{"REGISTER_CONTENT", "OFFER_LEASE"},
		"open_intent_cap": 16, "sql_role": "app_fixture_sql"}}
	listenAddr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	migrateCfg := filepath.Join(base, "etc", "migrate.json")
	writeJSON(t, migrateCfg, map[string]any{"chain_id": smokeChainID, "rpc_url": node.url,
		"database": dbSection(store.OwnerRole, "owner"), "callers": callers, "numbers": numbers})
	serveConfig := func(name, chainID, rpcURL string) string {
		path := filepath.Join(base, "etc", name)
		writeJSON(t, path, map[string]any{"chain_id": chainID, "rpc_url": rpcURL,
			"database": dbSection(store.RuntimeRole, "runtime"),
			"listen": map[string]any{"address": listenAddr, "tls_cert_file": ca.ServerCert, "tls_key_file": ca.ServerKey,
				"client_ca_file": ca.CACert},
			"signer": signerSection, "intent_key_registry": registryPath, "callers": callers, "numbers": numbers})
		return path
	}
	serveCfg := serveConfig("serve.json", smokeChainID, node.url)
	signerCfg := filepath.Join(base, "etc", "signer.json")
	writeJSON(t, signerCfg, map[string]any{"chain_id": smokeChainID, "rpc_url": node.url, "signer": signerSection,
		"intent_key_registry": registryPath, "numbers": numbers, "keys": []any{
			map[string]any{"name": "service", "class": "SERVICE", "path": filepath.Join(base, "keys", "service.key")},
			map[string]any{"name": "broadcaster", "class": "BROADCASTER", "path": filepath.Join(base, "keys", "broadcaster.key")},
		}})

	// serve refuses an unmigrated database before anything else.
	if code, p := s.run("serve-unmigrated", s.serveID, "serve", "--config", serveCfg); code != exitRefused ||
		!strings.Contains(p.stderr.String(), "older than the binary") {
		t.Fatalf("serve on an unmigrated database: exit %d: %s", code, p.stderr.String())
	}

	// migrate, then again (nothing to apply), with the callers.
	code, p = s.run("migrate", me, "migrate", "--config", migrateCfg, "--callers")
	var mres migrateResult
	if code != exitOK || json.Unmarshal([]byte(p.stdout.String()), &mres) != nil || len(mres.Applied) != 2 ||
		mres.CallersRegistered != 1 {
		t.Fatalf("migrate exit %d: %s %s", code, p.stdout.String(), p.stderr.String())
	}
	code, p = s.run("migrate-again", me, "migrate", "--config", migrateCfg)
	if code != exitOK || json.Unmarshal([]byte(p.stdout.String()), &mres) != nil || len(mres.Applied) != 0 {
		t.Fatalf("second migrate exit %d: %s %s", code, p.stdout.String(), p.stderr.String())
	}

	// serve refuses a node of another chain id, and a database bound to
	// another chain id than its configuration's.
	otherCodec, err := chain.NewCodec("shortvideo-dev-other")
	if err != nil {
		t.Fatal(err)
	}
	other := newFakeNode(t, otherCodec, nil)
	if code, p := s.run("serve-other-node", s.serveID, "serve", "--config", serveConfig("serve-other-node.json", smokeChainID, other.url)); code != exitRefused ||
		!strings.Contains(p.stderr.String(), "another chain id") {
		t.Fatalf("serve against a node of another chain: exit %d: %s", code, p.stderr.String())
	}
	if code, p := s.run("serve-other-db", s.serveID, "serve", "--config", serveConfig("serve-other-db.json", "shortvideo-dev-other", other.url)); code != exitRefused ||
		!strings.Contains(p.stderr.String(), "serves another chain id: database") {
		t.Fatalf("serve over a database bound to another chain: exit %d: %s", code, p.stderr.String())
	}

	// signer, then serve.
	signer := s.start("signer", s.signID, "signer", "--config", signerCfg)
	waitFor(t, "signer socket", func() bool {
		_, err := os.Lstat(filepath.Join(base, "sock", "signer.sock"))
		return err == nil
	}, signer)
	serve := s.start("serve", s.serveID, "serve", "--config", serveCfg)
	client := mtlsClient(t, ca)
	baseURL := "https://" + listenAddr
	waitFor(t, "serve health", func() bool {
		resp, err := client.Get(baseURL + "/v1/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, serve)

	// One request through mTLS, its replay, and its status.
	var manifest [32]byte
	if _, err := rand.Read(manifest[:]); err != nil {
		t.Fatal(err)
	}
	sb, err := content.RegisterIntentSignBytes(serviceAddr, "smoke-video", 1, manifest[:], 4096, content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"kind": "REGISTER_CONTENT", "idempotency_key": "smoke-1", "expires_in_seconds": 600,
		"auth_context": map[string]any{"principal_kind": "DEV_FIXTURE", "principal_id": "creator-1",
			"permissions": []string{"PUBLISH_CONTENT"}, "policy_ref": "policy-1"},
		"request": map[string]any{"content_id": "smoke-video", "content_version": 1,
			"manifest_digest_hex": hex.EncodeToString(manifest[:]), "package_bytes": 4096,
			"hosting_intent": map[string]any{"domain": "SIGNATURE_DOMAIN_HOSTING_INTENT", "key_id": "intent-key-1",
				"public_key_hex": hex.EncodeToString(intentPub), "signature_hex": hex.EncodeToString(ed25519.Sign(intentSecret, sb)),
				"canonical_profile_version": 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, created := postJSON(t, client, baseURL+"/v1/intents", body)
	if status != http.StatusCreated || created["intent_id"] == "" || created["kind"] != "REGISTER_CONTENT" {
		t.Fatalf("create: %d %v", status, created)
	}
	id, _ := created["intent_id"].(string)
	if status, again := postJSON(t, client, baseURL+"/v1/intents", body); status != http.StatusOK || again["intent_id"] != id {
		t.Fatalf("replay: %d %v", status, again)
	}
	var signed []byte
	waitFor(t, "a broadcast of the signed register", func() bool {
		if txs := node.broadcastTxs(); len(txs) > 0 {
			signed = txs[0]
			return true
		}
		return false
	}, serve)
	d, err := codec.Decode(signed)
	if err != nil || len(d.Msgs) != 1 || len(d.Signers) != 1 || !bytes.Equal(d.Signers[0].Address, serviceAddr) ||
		d.TimeoutHeight == 0 || d.Gas != 2_000_000 {
		t.Fatalf("broadcast transaction %+v: %v", d, err)
	}
	msg, ok := d.Msgs[0].Msg.(*contentv1.MsgRegisterContent)
	if !ok || msg.GetContentId() != "smoke-video" || msg.GetBusinessId() == "" || !bytes.Equal(msg.GetManifestDigest().GetValue(), manifest[:]) {
		t.Fatalf("broadcast message %T %v", d.Msgs[0].Msg, d.Msgs[0].Msg)
	}
	waitFor(t, "SUBMITTED status", func() bool {
		status, v := getJSON(t, client, baseURL+"/v1/intents/"+id)
		return status == http.StatusOK && v["status"] == "OPERATION_STATUS_SUBMITTED" && v["business_id"] == msg.GetBusinessId()
	}, serve)

	// status reads under the owner role; reconcile-once and signer-clear
	// find the signer locks held by serve.
	code, p = s.run("status", me, "status", "--config", migrateCfg)
	var st statusResult
	if code != exitOK || json.Unmarshal([]byte(p.stdout.String()), &st) != nil || len(st.Signers) != 2 ||
		st.Observer == nil || st.Observer.LatestHeight < 10 || st.ChainID != smokeChainID {
		t.Fatalf("status exit %d: %s %s", code, p.stdout.String(), p.stderr.String())
	}
	if code, p := s.run("reconcile-held", s.serveID, "reconcile-once", "--config", serveCfg); code != exitLockHeld {
		t.Fatalf("reconcile-once while serve runs: exit %d: %s", code, p.stderr.String())
	}
	serviceBech, err := codec.Bech32(serviceAddr)
	if err != nil {
		t.Fatal(err)
	}
	foreign := strings.Repeat("ab", 32)
	if code, p := s.run("clear-held", s.serveID, "signer-clear", "--config", serveCfg, "--signer", serviceBech,
		"--sequence", "0", "--foreign-hash", foreign); code != exitLockHeld {
		t.Fatalf("signer-clear while serve runs: exit %d: %s", code, p.stderr.String())
	}

	// Stop serve cleanly; then reconcile-once takes the locks and
	// signer-clear finds no event.
	if code := s.stop(serve); code != exitOK {
		t.Fatalf("serve exit %d on SIGTERM: %s", code, serve.stderr.String())
	}
	if !strings.Contains(serve.stderr.String(), `"number":"timeout_offset_blocks","value":"40","label":"PROPOSED"`) {
		t.Errorf("serve did not log the PROPOSED numbers in use: %s", lastLines(serve.stderr.String(), 5))
	}
	if code, p := s.run("reconcile", s.serveID, "reconcile-once", "--config", serveCfg); code != exitOK {
		t.Fatalf("reconcile-once after serve stopped: exit %d: %s", code, p.stderr.String())
	}
	if code, p := s.run("clear-none", s.serveID, "signer-clear", "--config", serveCfg, "--signer", serviceBech,
		"--sequence", "0", "--foreign-hash", foreign); code != exitNotFound {
		t.Fatalf("signer-clear without an event: exit %d: %s", code, p.stderr.String())
	}
	if code := s.stop(signer); code != exitOK {
		t.Fatalf("signer exit %d on SIGTERM: %s", code, signer.stderr.String())
	}
	if _, err := os.Lstat(filepath.Join(base, "sock", "signer.sock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("signer socket left behind: %v", err)
	}

	// No database password reached a log or an output.
	var secrets []string
	for _, r := range info.Roles {
		b, err := os.ReadFile(r.PasswordFile)
		if err != nil {
			t.Fatal(err)
		}
		secrets = append(secrets, strings.TrimSpace(string(b)))
	}
	if code := s.stop(db); code != exitOK {
		t.Fatalf("dev-db exit %d on SIGTERM: %s", code, db.stderr.String())
	}
	for _, path := range append([]string{info.ClusterDir}, passFile["owner"], passFile["runtime"], passFile["login"]) {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("dev-db left %s behind: %v", path, err)
		}
	}
	for _, p := range s.logs {
		for _, sec := range secrets {
			if strings.Contains(p.stdout.String(), sec) || strings.Contains(p.stderr.String(), sec) {
				t.Errorf("%s output holds a database password", p.name)
			}
		}
	}
	for _, p := range s.logs {
		t.Logf("%s exit %d; last log lines:\n%s", p.name, exitOf(p), lastLines(p.stderr.String(), 2))
	}
	if paths := node.unservedPaths(); len(paths) != 0 {
		t.Logf("fake node was asked for methods or paths it does not model: %v", paths)
	}
}

// readDevDB reads dev-db's description line, waiting at most two minutes.
func readDevDB(t *testing.T, p *proc) devDBInfo {
	t.Helper()
	var info devDBInfo
	waitFor(t, "dev-db description", func() bool {
		line, err := bufio.NewReader(strings.NewReader(p.stdout.String())).ReadString('\n')
		if err != nil {
			return false
		}
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			t.Fatalf("dev-db line %q: %v", line, err)
		}
		return true
	}, p)
	return info
}

// waitFor polls cond every 100 ms for at most two minutes and fails the
// test if p exits first.
func waitFor(t *testing.T, what string, cond func() bool, p *proc) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for !cond() {
		select {
		case <-p.done:
			t.Fatalf("waiting for %s: %s exited %d: %s", what, p.name, exitOf(p), p.stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; %s stderr: %s", what, p.name, p.stderr.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// mtlsClient is a caller's HTTPS client with the dev-ca client
// certificate, trusting only the dev-ca CA.
func mtlsClient(t *testing.T, ca caFiles) *http.Client {
	t.Helper()
	roots := x509.NewCertPool()
	pemCA, err := os.ReadFile(ca.CACert)
	if err != nil || !roots.AppendCertsFromPEM(pemCA) {
		t.Fatalf("CA file: %v", err)
	}
	cert, err := tls.LoadX509KeyPair(ca.Clients[0].Cert, ca.Clients[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}},
	}}
}

func postJSON(t *testing.T, c *http.Client, url string, body []byte) (int, map[string]any) {
	t.Helper()
	resp, err := c.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return decodeResponse(t, resp)
}

func getJSON(t *testing.T, c *http.Client, url string) (int, map[string]any) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return decodeResponse(t, resp)
}

func decodeResponse(t *testing.T, resp *http.Response) (int, map[string]any) {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%d %q: %v", resp.StatusCode, raw, err)
	}
	return resp.StatusCode, v
}

// lastLines returns the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
