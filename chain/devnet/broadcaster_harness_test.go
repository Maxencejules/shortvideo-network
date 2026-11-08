package devnet_test

// The run harness of the broadcaster devnet tests: four svnd validator
// processes (one machine, private network namespace in the test commands),
// a PostgreSQL 18.6 cluster started by `svn-broadcaster dev-db`, a
// development CA from `svn-broadcaster dev-ca`, the outbox migrated by
// `svn-broadcaster migrate --callers`, and the `signer` and `serve`
// processes, serve behind the fault proxy. When the test runs as root, the
// signer and serve run under two distinct unused uids (plan 4.1); otherwise
// both run under the test's own uid, which separates key bytes in memory
// only (the same-uid limit of docs/architecture/broadcaster.md 4.2).

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
)

// bcDevDB is dev-db's description line.
type bcDevDB struct {
	ChainID         string `json:"chain_id"`
	ClusterDir      string `json:"cluster_dir"`
	SocketDir       string `json:"socket_dir"`
	Port            int    `json:"port"`
	Database        string `json:"database"`
	PostgresVersion string `json:"postgres_version"`
	Roles           []struct {
		Role         string `json:"role"`
		Use          string `json:"use"`
		PasswordFile string `json:"password_file"`
	} `json:"roles"`
}

// bcCAFiles is dev-ca's output.
type bcCAFiles struct {
	Dir        string `json:"dir"`
	CACert     string `json:"ca_cert_file"`
	ServerCert string `json:"server_cert_file"`
	ServerKey  string `json:"server_key_file"`
	Clients    []struct {
		Principal string `json:"principal"`
		Cert      string `json:"cert_file"`
		Key       string `json:"key_file"`
	} `json:"clients"`
}

// Caller principals of the run and the PostgreSQL roles dev-db creates.
const (
	callerApp      = "app-fixture"
	callerOther    = "other-caller"
	callerLimited  = "limited-caller"
	callerStranger = "stranger"
	roleAppSQL     = "app_fixture_sql"
	roleLimitedSQL = "limited_sql"
	roleReader     = "harness_reader"
	roleStrangerDB = "stranger_sql"
	roleOwner      = "svn_broadcaster_owner"
	roleRuntime    = "svn_broadcaster"
	// Hosting-intent principals of the operator's registry.
	creatorOne   = "creator-1"
	creatorTwo   = "creator-2"
	creatorNoKey = "creator-9"
)

// bcAllKinds are the seven kinds of docs/architecture/broadcaster.md 10.1.
var bcAllKinds = []string{"REGISTER_CONTENT", "WITHDRAW_CONTENT", "OFFER_LEASE", "RELAY_ACCEPT_LEASE",
	"RELAY_RENEW_LEASE", "RELAY_END_LEASE", "FORWARD_PROVIDER_TX"}

// bnet is one broadcaster devnet run.
type bnet struct {
	t      *testing.T
	start  time.Time
	layout devnet.Layout
	home   string
	stops  []func() error
	svnd   string
	rpcs   []string
	d      *demo

	launch   *bcLauncher
	base     string
	distinct bool
	signID   bcIdentity
	serveID  bcIdentity
	me       bcIdentity

	ca       bcCAFiles
	db       bcDevDB
	passFile map[string]string
	devdb    *bcProc
	signer   *bcProc
	serve    *bcProc
	proxy    *bcProxy

	numbers map[string]any
	callers []any
	listen  string
	// apiAddr is the listener the test's API calls go to: serve's listen
	// address, or the second serve process's while phase CS runs on it.
	apiAddr    string
	registry   string
	signerCfg  string
	serveCfg   string
	migrateCfg string
	faultServe string
	faultSign  string
	signerSock string

	clients    map[string]*http.Client
	owner      *sql.DB
	reader     *sql.DB
	sqlCaller  *sql.DB
	intentKeys map[string]ed25519.PrivateKey

	// service and bcast are the devnet accounts whose keys the signer
	// holds: SERVICE (ADMITTED_SERVICE) and BROADCASTER.
	service loaded
	bcast   loaded

	// providers are the provider ids the run registered (checkAccounting
	// (e) reads their reservations).
	providers []string
	// pi is what phase S prepared for phase PI; stdout is the file that
	// holds a copy of the test's own output (phase L scans it).
	pi     *bcPI
	stdout string

	objects map[string]bcObject
	// leaked is set when phase L found a canary in a log: the failure log
	// tails are then not printed, so no secret reaches the test output.
	leaked         bool
	lastTransition int64
	verifiedSeq    map[string]int64
	serial         int
}

// bcNumbers are the PROPOSED numbers this run sets (DR-11-10); the run
// prints them. Short timeouts keep the run inside the CI shard timeout:
// K = 40 blocks keeps a held broadcast valid while the test commits a
// competing transaction (devnet blocks come about every 0.25 s), and L = 2
// is the PROPOSED margin.
func bcNumbers() map[string]any {
	return map[string]any{
		"poll_interval_ms": 200, "rpc_timeout_seconds": 2, "intent_lifetime_min_seconds": 5,
		"stall_window_seconds": 5, "observer_freshness_polls": 10,
		"timeout_offset_blocks": 40, "signer_max_timeout_offset_blocks": 80, "indexing_margin_blocks": 2,
		"backoff_base_ms": 200, "backoff_cap_seconds": 2,
	}
}

// newBnet starts a broadcaster devnet run: binaries, validators, the
// broadcaster's database, CA, configuration, signer and serve.
func newBnet(t *testing.T) *bnet {
	t.Helper()
	n := &bnet{t: t, start: time.Now(), passFile: map[string]string{}, clients: map[string]*http.Client{},
		intentKeys: map[string]ed25519.PrivateKey{}, objects: map[string]bcObject{}, verifiedSeq: map[string]int64{}}
	n.me = bcIdentity{uint32(os.Geteuid())}

	binDir, err := os.MkdirTemp("", "svn-bdev-bin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(binDir) })
	if err := os.Chmod(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin, svnd := bcBuildBinaries(t, binDir)
	n.svnd = svnd
	n.startValidators()
	n.d = newDemo(t, n.layout, n.home)
	n.service, n.bcast = n.d.service, n.d.broadcaster

	if n.me.uid == 0 {
		uids := bcUnusedUIDs(t, 2)
		n.signID, n.serveID, n.distinct = bcIdentity{uids[0]}, bcIdentity{uids[1]}, true
		fmt.Printf("boundary_uids=signer:%d,serve:%d (root launcher: two distinct unused uids)\n", uids[0], uids[1])
	} else {
		n.signID, n.serveID = n.me, n.me
		fmt.Printf("boundary_uids=same:%d (launcher is not root: one uid, key bytes separated in memory only)\n", n.me.uid)
	}
	n.layoutDirs()
	n.launch = &bcLauncher{t: t, bin: bin, logDir: filepath.Join(n.base, "logs"), env: bcChildEnv}
	// On a failure, the last lines of each broadcaster log (redacting JSON)
	// go to the test output before the processes stop; this runs before
	// the launcher's cleanups. After a canary hit nothing is printed.
	t.Cleanup(func() {
		if !t.Failed() || n.leaked {
			return
		}
		for _, p := range n.launch.procs {
			fmt.Printf("--- failure log tail %s ---\n%s\n", filepath.Base(p.logPath), p.logTail(12))
		}
	})
	n.writeKeys()
	n.writeRegistry()
	n.runDevCA()
	n.startDevDB()
	n.proxy = startProxy(t, "serve-1", n.rpcs[0])
	n.writeConfigs()
	n.migrate()
	n.setupRoles()
	n.startSigner()
	n.serve = n.startServe("serve", n.serveCfg)
	fmt.Printf("phase=setup-processes seconds=%.1f chain_id=%s postgres_version=%q\n",
		time.Since(n.start).Seconds(), n.layout.Topology.ChainID, n.db.PostgresVersion)
	return n
}

// startValidators initializes a devnet and starts its four validators with
// the svnd binary; it waits for height 3.
func (n *bnet) startValidators() {
	t := n.t
	n.home = t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: n.home})
	if err != nil {
		t.Fatal(err)
	}
	n.layout = layout
	n.stops = make([]func() error, len(layout.Topology.Validators))
	t.Cleanup(func() {
		for _, stop := range n.stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	for i := range n.stops {
		n.startValidator(i)
	}
	n.rpcs = rpcList(layout)
	if _, _, err := devnet.WaitEqual(n.rpcs, 3, 60*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
}

// startValidator starts validator i with svnd.
func (n *bnet) startValidator(i int) {
	stop, err := launchBinary(n.svnd, n.layout.Topology.Validators[i].Home)
	if err != nil {
		n.t.Fatal(err)
	}
	n.stops[i] = stop
}

// stopValidator stops validator i.
func (n *bnet) stopValidator(i int) {
	if err := n.stops[i](); err != nil {
		n.t.Fatal(tail(n.layout, err))
	}
	n.stops[i] = nil
}

// layoutDirs creates the run's directory tree in a new 0755 directory.
func (n *bnet) layoutDirs() {
	t := n.t
	base, err := os.MkdirTemp("", "svn-bdev-")
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
	n.base = base
	bcMkdir(t, filepath.Join(base, "etc"), 0o755, n.me)
	bcMkdir(t, filepath.Join(base, "logs"), 0o700, n.me)
	bcMkdir(t, filepath.Join(base, "secrets"), 0o711, n.me)
	bcMkdir(t, filepath.Join(base, "keys"), 0o700, n.signID)
	bcMkdir(t, filepath.Join(base, "sock"), 0o755, n.signID)
	n.faultServe = filepath.Join(base, "fault-serve")
	n.faultSign = filepath.Join(base, "fault-signer")
	bcMkdir(t, n.faultServe, 0o700, n.serveID)
	bcMkdir(t, n.faultSign, 0o700, n.signID)
	n.signerSock = filepath.Join(base, "sock", "signer.sock")
}

// writeKeys copies the devnet's service and broadcaster account keys into
// the signer's key directory: the only files that hold signing keys, owned
// by the signer uid with mode 0600.
func (n *bnet) writeKeys() {
	for _, name := range []string{"service", "broadcaster"} {
		raw := readHexFile(n.t, filepath.Join(n.home, "accounts", name+".hex"))
		bcWriteFile(n.t, filepath.Join(n.base, "keys", name+".key"), []byte(hex.EncodeToString(raw)+"\n"), 0o600, n.signID)
	}
}

// writeRegistry writes the operator's intent-key registry: public keys
// only, one per hosting-intent principal, in a file neither broadcaster
// process may write. The private halves stay in the test as the devnet
// account-host fixture (a test stand-in, not DEC-001 custody).
func (n *bnet) writeRegistry() {
	var entries []any
	for _, principal := range []string{creatorOne, creatorTwo} {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			n.t.Fatal(err)
		}
		n.intentKeys[principal] = priv
		entries = append(entries, map[string]any{"principal_id": principal, "public_key_hex": hex.EncodeToString(pub), "label": "devnet fixture " + principal})
	}
	raw, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		n.t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	if !n.distinct {
		mode = 0o444
	}
	n.registry = filepath.Join(n.base, "etc", "intent-keys.json")
	bcWriteFile(n.t, n.registry, raw, mode, n.me)
}

// runDevCA creates the development CA and one client certificate per
// caller principal (plus stranger, a principal no caller entry names).
func (n *bnet) runDevCA() {
	args := []string{"dev-ca", "--out", filepath.Join(n.base, "ca"), "--serve-uid", strconv.FormatUint(uint64(n.serveID.uid), 10)}
	for _, p := range []string{callerApp, callerOther, callerLimited, callerStranger} {
		args = append(args, "--principal", p)
	}
	code, p := n.launch.run("dev-ca", n.me, args...)
	if code != 0 {
		n.t.Fatalf("dev-ca exit %d: %s", code, p.log())
	}
	if err := json.Unmarshal([]byte(p.stdout.String()), &n.ca); err != nil || len(n.ca.Clients) != 4 {
		n.t.Fatalf("dev-ca output %q: %v", p.stdout.String(), err)
	}
	for _, c := range n.ca.Clients {
		n.clients[c.Principal] = n.mtlsClient(c.Cert, c.Key)
	}
}

// mtlsClient is a caller's HTTPS client with a client certificate, trusting
// only the dev-ca CA.
func (n *bnet) mtlsClient(certFile, keyFile string) *http.Client {
	roots := x509.NewCertPool()
	pemCA, err := os.ReadFile(n.ca.CACert)
	if err != nil || !roots.AppendCertsFromPEM(pemCA) {
		n.t.Fatalf("CA file: %v", err)
	}
	var certs []tls.Certificate
	if certFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			n.t.Fatal(err)
		}
		certs = append(certs, cert)
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: certs},
	}}
}

// startDevDB starts dev-db with the caller, reader and stranger login
// roles and reads its description line.
func (n *bnet) startDevDB() {
	n.devdb = n.launch.start("dev-db", n.me, nil, "dev-db", "--chain-id", n.layout.Topology.ChainID,
		"--secrets-dir", filepath.Join(n.base, "secrets"), "--serve-uid", strconv.FormatUint(uint64(n.serveID.uid), 10),
		"--login-role", roleAppSQL, "--login-role", roleLimitedSQL, "--login-role", roleReader, "--login-role", roleStrangerDB)
	bcWait(n.t, "dev-db description", 3*time.Minute, func() bool {
		line, _, ok := strings.Cut(n.devdb.stdout.String(), "\n")
		if !ok {
			return false
		}
		if err := json.Unmarshal([]byte(line), &n.db); err != nil {
			n.t.Fatalf("dev-db line %q: %v", line, err)
		}
		return true
	}, n.devdb)
	if !strings.HasPrefix(n.db.ClusterDir, filepath.Join(os.TempDir(), "svn-pg-")) || n.db.ChainID != n.layout.Topology.ChainID {
		n.t.Fatalf("dev-db description %+v", n.db)
	}
	// Cleanups run in reverse order, so this one runs before the
	// launcher's: it stops dev-db first (SIGTERM; dev-db removes the
	// cluster), then removes a cluster directory dev-db could not remove.
	n.t.Cleanup(func() {
		if !n.devdb.exited() {
			_ = n.devdb.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-n.devdb.done:
			case <-time.After(30 * time.Second):
				_ = syscall.Kill(-n.devdb.cmd.Process.Pid, syscall.SIGKILL)
				<-n.devdb.done
			}
		}
		if _, err := os.Lstat(n.db.ClusterDir); err == nil {
			n.t.Errorf("dev-db left its cluster directory %s; removing it", n.db.ClusterDir)
			_ = os.RemoveAll(n.db.ClusterDir)
		}
	})
	for _, r := range n.db.Roles {
		n.passFile[r.Role] = r.PasswordFile
	}
}

// dbSection is the database section of a configuration for role.
func (n *bnet) dbSection(role, app string) map[string]any {
	return map[string]any{"socket_dir": n.db.SocketDir, "port": n.db.Port, "name": n.db.Database, "role": role,
		"password_file": n.passFile[role], "application_name": app, "connect_timeout_seconds": 10}
}

// writeConfigs writes the migrate, signer and serve configuration files.
func (n *bnet) writeConfigs() {
	n.numbers = bcNumbers()
	cap16, cap256 := 16, 256
	n.callers = []any{
		map[string]any{"principal": callerApp, "kinds": bcAllKinds, "open_intent_cap": cap256, "sql_role": roleAppSQL},
		map[string]any{"principal": callerOther, "kinds": []string{"REGISTER_CONTENT", "WITHDRAW_CONTENT", "OFFER_LEASE"}, "open_intent_cap": cap16},
		map[string]any{"principal": callerLimited, "kinds": []string{"REGISTER_CONTENT"}, "open_intent_cap": cap16, "sql_role": roleLimitedSQL},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		n.t.Fatal(err)
	}
	n.listen = ln.Addr().String()
	n.apiAddr = n.listen
	if err := ln.Close(); err != nil {
		n.t.Fatal(err)
	}
	chainID := n.layout.Topology.ChainID
	n.migrateCfg = filepath.Join(n.base, "etc", "migrate.json")
	bcWriteJSON(n.t, n.migrateCfg, map[string]any{"chain_id": chainID, "rpc_url": n.rpcs[0],
		"database": n.dbSection(roleOwner, "devnet-migrate"), "callers": n.callers, "numbers": n.numbers})
	signer := map[string]any{"socket_path": n.signerSock, "serve_uid": n.serveID.uid, "signer_uid": n.signID.uid}
	n.signerCfg = filepath.Join(n.base, "etc", "signer.json")
	bcWriteJSON(n.t, n.signerCfg, map[string]any{"chain_id": chainID, "rpc_url": n.rpcs[0], "signer": signer,
		"intent_key_registry": n.registry, "numbers": n.numbers, "keys": []any{
			map[string]any{"name": "service", "class": "SERVICE", "path": filepath.Join(n.base, "keys", "service.key")},
			map[string]any{"name": "broadcaster", "class": "BROADCASTER", "path": filepath.Join(n.base, "keys", "broadcaster.key")},
		}})
	n.serveCfg = n.writeServeConfig("serve.json", n.listen, n.proxy.url(), nil)
	var nums []string
	for k, v := range n.numbers {
		nums = append(nums, fmt.Sprintf("%s=%v", k, v))
	}
	sort.Strings(nums)
	fmt.Printf("numbers=%s (PROPOSED, DR-11-10; chosen for this run, not the lower bounds; every other number at its PROPOSED value; a numbers_override line names a phase's serve configuration that differs)\n", strings.Join(nums, ","))
}

// writeServeConfig writes a new serve configuration file listening on
// listen whose node is rpcURL, with numbers overriding the run's numbers.
func (n *bnet) writeServeConfig(name, listen, rpcURL string, numbers map[string]any) string {
	nums := map[string]any{}
	for k, v := range n.numbers {
		nums[k] = v
	}
	var overrides []string
	for k, v := range numbers {
		nums[k] = v
		overrides = append(overrides, fmt.Sprintf("%s=%v", k, v))
	}
	if len(overrides) != 0 {
		sort.Strings(overrides)
		fmt.Printf("numbers_override file=%s %s (PROPOSED, DR-11-10; the other numbers as in numbers=)\n", name, strings.Join(overrides, " "))
	}
	path := filepath.Join(n.base, "etc", name)
	cfg := map[string]any{"chain_id": n.layout.Topology.ChainID, "rpc_url": rpcURL,
		"database": n.dbSection(roleRuntime, "devnet-"+strings.TrimSuffix(name, ".json")),
		"listen": map[string]any{"address": listen, "tls_cert_file": n.ca.ServerCert, "tls_key_file": n.ca.ServerKey,
			"client_ca_file": n.ca.CACert},
		"signer":              map[string]any{"socket_path": n.signerSock, "serve_uid": n.serveID.uid, "signer_uid": n.signID.uid},
		"intent_key_registry": n.registry, "callers": n.callers, "numbers": nums}
	raw, err := json.Marshal(cfg)
	if err != nil {
		n.t.Fatal(err)
	}
	bcWriteFile(n.t, path, raw, 0o644, n.me)
	return path
}

// migrate applies the migrations and registers the callers.
func (n *bnet) migrate() {
	code, p := n.launch.run("migrate", n.me, "migrate", "--config", n.migrateCfg, "--callers")
	var res struct {
		Applied           []int `json:"applied"`
		CallersRegistered int   `json:"callers_registered"`
	}
	if code != 0 || json.Unmarshal([]byte(p.stdout.String()), &res) != nil || len(res.Applied) != 2 || res.CallersRegistered != 3 {
		n.t.Fatalf("migrate exit %d: %s %s", code, p.stdout.String(), p.log())
	}
}

// setupRoles grants the harness role SELECT on the outbox and creates the
// application fixture table of the SQL path, as the owner role.
func (n *bnet) setupRoles() {
	n.owner = bcOpenDB(n.t, n.db, roleOwner, n.passFile[roleOwner])
	ctx, cancel := bcCtx()
	defer cancel()
	for _, stmt := range []string{
		`GRANT USAGE ON SCHEMA broadcaster TO ` + roleReader,
		`GRANT SELECT ON ALL TABLES IN SCHEMA broadcaster TO ` + roleReader,
		`CREATE SCHEMA app_fixture`,
		`CREATE TABLE app_fixture.uploads (id text PRIMARY KEY CHECK (id ~ '^[a-z0-9-]{1,64}$'), intent_id uuid NOT NULL)`,
		`GRANT USAGE ON SCHEMA app_fixture TO ` + roleAppSQL,
		`GRANT SELECT, INSERT ON app_fixture.uploads TO ` + roleAppSQL,
		`GRANT USAGE ON SCHEMA app_fixture TO ` + roleReader,
		`GRANT SELECT ON app_fixture.uploads TO ` + roleReader,
	} {
		if _, err := n.owner.ExecContext(ctx, stmt); err != nil {
			n.t.Fatalf("%s: %v", stmt, err)
		}
	}
	// The owner connection is not used again.
	if err := n.owner.Close(); err != nil {
		n.t.Fatal(err)
	}
	n.reader = bcOpenDB(n.t, n.db, roleReader, n.passFile[roleReader])
	n.sqlCaller = bcOpenDB(n.t, n.db, roleAppSQL, n.passFile[roleAppSQL])
	n.readerCannotWrite()
}

// startSigner starts the signer and waits for its socket: a socket file
// whose status changed after the start (a killed signer leaves its old
// socket, which the new signer replaces and chmods).
func (n *bnet) startSigner() {
	started := time.Now().Add(-time.Second)
	n.signer = n.launch.start("signer", n.signID, []string{"SVN_FAULTPOINT_DIR=" + n.faultSign}, "signer", "--config", n.signerCfg)
	bcWait(n.t, "signer socket", 2*time.Minute, func() bool {
		info, err := os.Lstat(n.signerSock)
		if err != nil {
			return false
		}
		st := info.Sys().(*syscall.Stat_t)
		return time.Unix(st.Ctim.Sec, st.Ctim.Nsec).After(started)
	}, n.signer)
}

// startServe starts a serve process with cfg and waits until its API
// answers.
func (n *bnet) startServe(name, cfg string) *bcProc {
	raw, err := os.ReadFile(cfg)
	if err != nil {
		n.t.Fatal(err)
	}
	var parsed struct {
		Listen struct {
			Address string `json:"address"`
		} `json:"listen"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		n.t.Fatal(err)
	}
	p := n.launch.start(name, n.serveID, []string{"SVN_FAULTPOINT_DIR=" + n.faultServe}, "serve", "--config", cfg)
	client := n.clients[callerApp]
	bcWait(n.t, name+" health", 2*time.Minute, func() bool {
		resp, err := client.Get("https://" + parsed.Listen.Address + "/v1/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, p)
	return p
}

// restartServe starts serve again with the run's configuration after the
// previous process exited.
func (n *bnet) restartServe() {
	if !n.serve.exited() {
		n.t.Fatalf("restartServe: serve still runs")
	}
	n.serve = n.startServe("serve", n.serveCfg)
}
