package milestone_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

func (m *mile) bootProviders(pub ed25519.PublicKey) {
	m.t.Helper()
	_ = pub
	stageURL, err := url.Parse(m.stage)
	if err != nil {
		m.t.Fatal(err)
	}
	stagePort := portOf(m.t, stageURL.Host)
	for _, spec := range []struct {
		id, view, account string
	}{
		{"p1", "a", "provider-1"}, {"p2", "a", "provider-1"}, {"p3", "a", "provider-1"}, {"p4", "b", "provider-2"},
	} {
		dir := filepath.Join(m.work, "prov-"+spec.id)
		view := m.views[spec.view]
		chainURL, err := url.Parse(view.URL)
		if err != nil {
			m.t.Fatal(err)
		}
		cfg := map[string]any{
			"provider_id": spec.id, "key_id": "k" + spec.id, "machine_id": "vm-1",
			"listen": "127.0.0.1:0", "data_dir": dir, "hosting_consent": true,
			"declared_capacity_bytes": 50000000, "quota_bytes": 50000000, "min_free_bytes": 1,
			"bandwidth_bytes_per_second": 10000000, "transfer_budget_seconds": 60,
			"max_package_bytes": 50000000, "max_term_seconds": 7200,
			"clock_uncertainty_seconds": 30, "stall_after_seconds": 12,
			"chain_id": m.chainID, "chain_url": view.URL, "source_ca": m.stageCA, "chain_ca": view.CA,
			"destinations":       []any{dest(stageURL.Hostname(), stagePort)},
			"chain_destinations": []any{dest(chainURL.Hostname(), portOf(m.t, chainURL.Host))},
		}
		path := filepath.Join(m.work, spec.id+".json")
		writeJSON(m.t, path, cfg)
		m.run("init-"+spec.id, m.bins["svn-provider"], "init", path)
		if err := os.WriteFile(filepath.Join(dir, "source.token"), []byte("milestone-source\n"), 0o600); err != nil {
			m.t.Fatal(err)
		}
		copyMode(m.t, filepath.Join(m.layout.Home, "accounts", spec.account+".hex"), filepath.Join(dir, "keys", "outer.hex"))
		c := m.start(spec.id, m.bins["svn-provider"], "run", path)
		listen := m.waitFile(filepath.Join(dir, "listen.json"), 20*time.Second)
		var doc struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(listen, &doc); err != nil {
			m.t.Fatalf("%s listen: %v %s", spec.id, err, c.out.String())
		}
		m.prov[spec.id] = &provider{id: spec.id, dir: dir, ca: filepath.Join(dir, "tls.crt"), url: strings.TrimRight(doc.URL, "/"), child: c}
		token, err := os.ReadFile(filepath.Join(dir, "admin.token"))
		if err != nil {
			m.t.Fatal(err)
		}
		m.postJSON(m.sched+"/v1/hosts", map[string]string{
			"provider_id": spec.id, "endpoint": strings.TrimRight(doc.URL, "/"),
			"ca_file": filepath.Join(dir, "tls.crt"), "admin_token": strings.TrimSpace(string(token)),
		})
		fmt.Printf("phase=provider id=%s url=%s outer_account=%s\n", spec.id, strings.TrimRight(doc.URL, "/"), spec.account)
	}
}

func (m *mile) admitProviders(pub ed25519.PublicKey) {
	m.t.Helper()
	_ = pub
	for _, spec := range []struct{ id, who string }{{"p1", "p1"}, {"p2", "p1"}, {"p3", "p1"}, {"p4", "p2"}} {
		who := m.p1
		if spec.who == "p2" {
			who = m.p2
		}
		raw, err := os.ReadFile(filepath.Join(m.prov[spec.id].dir, "enrollment.json"))
		if err != nil {
			m.t.Fatal(err)
		}
		var en struct {
			Public string `json:"public_key_hex"`
		}
		if err := json.Unmarshal(raw, &en); err != nil {
			m.t.Fatal(err)
		}
		pubKey, err := hex.DecodeString(en.Public)
		if err != nil || len(pubKey) != 32 {
			m.t.Fatalf("enrollment %s", en.Public)
		}
		sum, err := capability.Encode(capability.Manifest{
			ProviderID: spec.id, SigningKeyID: "k" + spec.id, DeclaredCapacityBytes: 50000000,
			Endpoints: []capability.Endpoint{{Name: "https", URL: "https://127.0.0.1/segments"}},
		})
		if err != nil {
			m.t.Fatal(err)
		}
		m.commit("register-"+spec.id, who, &providersv1.MsgRegisterProvider{
			Signer: who.key.PubKey().Address(), BusinessId: "reg-" + spec.id, ProviderId: spec.id,
			SigningPubkey: pubKey, SigningKeyId: "k" + spec.id, EndpointManifestDigest: sum.Digest(),
			DeclaredCapacityBytes: 50000000, OperatorGroupId: providers.DevnetOperatorGroup,
		})
		m.council("admit-"+spec.id, &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{
			AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: spec.id},
		}})
	}
}

func (m *mile) bootBroadcaster(pub ed25519.PublicKey) {
	m.t.Helper()
	base := filepath.Join(m.work, "broadcaster")
	mustMkdir(filepath.Join(base, "secrets"))
	caText := jsonLineContaining(m.run("dev-ca", m.bins["svn-broadcaster"], "dev-ca", "--out", filepath.Join(base, "ca"), "--principal", "app-fixture"), "ca_cert_file")
	var ca struct {
		CACert, ServerCert, ServerKey string
		Clients                       []struct{ Cert, Key string }
	}
	// Field names follow the command's JSON.
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(caText)), &raw); err != nil {
		m.t.Fatalf("dev-ca: %v %s", err, caText)
	}
	ca.CACert = str(raw["ca_cert_file"])
	ca.ServerCert = str(raw["server_cert_file"])
	ca.ServerKey = str(raw["server_key_file"])
	clients, _ := raw["clients"].([]any)
	if len(clients) != 1 {
		m.t.Fatalf("dev-ca clients %s", caText)
	}
	one, _ := clients[0].(map[string]any)
	ca.Clients = []struct{ Cert, Key string }{{Cert: str(one["cert_file"]), Key: str(one["key_file"])}}
	db := m.start("broadcaster-db", m.bins["svn-broadcaster"], "dev-db", "--chain-id", m.chainID, "--secrets-dir", filepath.Join(base, "secrets"))
	dbLine := m.waitLine(db, `"socket_dir"`, 60*time.Second)
	var info struct {
		Socket string `json:"socket_dir"`
		Port   int    `json:"port"`
		Name   string `json:"database"`
		Roles  []struct {
			Use  string `json:"use"`
			Role string `json:"role"`
			File string `json:"password_file"`
		} `json:"roles"`
	}
	if err := json.Unmarshal([]byte(dbLine), &info); err != nil {
		m.t.Fatalf("dev-db: %v %s", err, db.out.String())
	}
	pass := map[string]string{}
	role := map[string]string{}
	for _, r := range info.Roles {
		pass[r.Use] = r.File
		role[r.Use] = r.Role
	}
	keys := filepath.Join(base, "keys")
	mustMkdir(keys)
	copyMode(m.t, filepath.Join(m.layout.Home, "accounts", "service.hex"), filepath.Join(keys, "service.hex"))
	copyMode(m.t, filepath.Join(m.layout.Home, "accounts", "broadcaster.hex"), filepath.Join(keys, "broadcaster.hex"))
	regPath := filepath.Join(base, "intent-keys.json")
	writeJSONMode(m.t, regPath, map[string]any{"entries": []any{map[string]any{
		"principal_id": "creator-1", "public_key_hex": hex.EncodeToString(pub), "label": "milestone",
	}}}, 0o444)
	uid := os.Getuid()
	dbSection := func(use string) map[string]any {
		return map[string]any{"socket_dir": info.Socket, "port": info.Port, "name": info.Name, "role": role[use],
			"password_file": pass[use], "application_name": "milestone-" + use, "connect_timeout_seconds": 10}
	}
	numbers := map[string]any{"poll_interval_ms": 200, "stall_window_seconds": 8}
	signerSection := map[string]any{"socket_path": filepath.Join(base, "signer.sock"), "serve_uid": uid, "signer_uid": uid}
	callers := []any{map[string]any{"principal": "app-fixture", "kinds": []string{"REGISTER_CONTENT", "WITHDRAW_CONTENT", "OFFER_LEASE", "RELAY_ACCEPT_LEASE"}, "open_intent_cap": 32}}
	rpc := m.layout.Topology.Validators[0].RPC
	port := freePort(m.t)
	m.bcURL = "https://127.0.0.1:" + strconv.Itoa(port)
	writeJSON(m.t, filepath.Join(base, "migrate.json"), map[string]any{"chain_id": m.chainID, "rpc_url": rpc, "database": dbSection("owner"), "callers": callers, "numbers": numbers})
	writeJSON(m.t, filepath.Join(base, "serve.json"), map[string]any{
		"chain_id": m.chainID, "rpc_url": rpc, "database": dbSection("runtime"),
		"listen": map[string]any{"address": "127.0.0.1:" + strconv.Itoa(port), "tls_cert_file": ca.ServerCert, "tls_key_file": ca.ServerKey, "client_ca_file": ca.CACert},
		"signer": signerSection, "intent_key_registry": regPath, "callers": callers, "numbers": numbers,
	})
	writeJSON(m.t, filepath.Join(base, "signer.json"), map[string]any{
		"chain_id": m.chainID, "rpc_url": rpc, "signer": signerSection, "intent_key_registry": regPath, "numbers": numbers,
		"keys": []any{
			map[string]any{"name": "service", "class": "SERVICE", "path": filepath.Join(keys, "service.hex")},
			map[string]any{"name": "broadcaster", "class": "BROADCASTER", "path": filepath.Join(keys, "broadcaster.hex")},
		},
	})
	m.run("migrate", m.bins["svn-broadcaster"], "migrate", "--config", filepath.Join(base, "migrate.json"), "--callers")
	signer := m.start("signer", m.bins["svn-broadcaster"], "signer", "--config", filepath.Join(base, "signer.json"))
	m.waitFileOrExit(signer, filepath.Join(base, "signer.sock"), 40*time.Second)
	serve := m.start("broadcaster", m.bins["svn-broadcaster"], "serve", "--config", filepath.Join(base, "serve.json"))
	m.bc = mtls(m.t, ca.CACert, ca.Clients[0].Cert, ca.Clients[0].Key)
	m.waitHTTP(m.bc, m.bcURL+"/v1/health", 200, 40*time.Second, serve)
	fmt.Printf("phase=broadcaster url=%s\n", m.bcURL)
}

func (m *mile) bootIndexer() {
	m.t.Helper()
	base := filepath.Join(m.work, "indexer")
	mustMkdir(filepath.Join(base, "secrets"))
	db := m.start("indexer-db", m.bins["svn-indexer"], "dev-db", "--chain-id", m.chainID, "--secrets-dir", filepath.Join(base, "secrets"))
	line := m.waitLine(db, `"socket_dir"`, 60*time.Second)
	var info struct {
		Socket string `json:"socket_dir"`
		Port   int    `json:"port"`
		Name   string `json:"database"`
		Roles  []struct {
			Use, Role, File string `json:"use"`
		} `json:"roles"`
	}
	// The struct tags above are wrong for Role and File. Decode loosely.
	var loose struct {
		Socket string `json:"socket_dir"`
		Port   int    `json:"port"`
		Name   string `json:"database"`
		Roles  []struct {
			Use  string `json:"use"`
			Role string `json:"role"`
			File string `json:"password_file"`
		} `json:"roles"`
	}
	if err := json.Unmarshal([]byte(line), &loose); err != nil {
		m.t.Fatalf("indexer db: %v %s", err, db.out.String())
	}
	info.Socket, info.Port, info.Name = loose.Socket, loose.Port, loose.Name
	pass := map[string]string{}
	roleName := map[string]string{}
	for _, r := range loose.Roles {
		pass[r.Use] = r.File
		roleName[r.Use] = r.Role
	}
	port := freePort(m.t)
	m.indexer = "http://127.0.0.1:" + strconv.Itoa(port)
	writeJSON(m.t, filepath.Join(base, "indexer.json"), map[string]any{
		"chain_id": m.chainID, "rpc_url": m.layout.Topology.Validators[0].RPC, "listen": "127.0.0.1:" + strconv.Itoa(port),
		"database": map[string]any{
			"socket_dir": info.Socket, "port": info.Port, "name": info.Name,
			"owner_role": roleName["owner"], "owner_password_file": pass["owner"],
			"role": roleName["runtime"], "password_file": pass["runtime"],
			"application_name": "milestone-indexer", "connect_timeout_seconds": 10,
		},
		"numbers": map[string]any{"stale_after_seconds": 600, "poll_ms": 200, "reconcile_ms": 5000, "rpc_timeout_seconds": 10},
	})
	m.run("indexer-migrate", m.bins["svn-indexer"], "migrate", "--config", filepath.Join(base, "indexer.json"))
	c := m.start("indexer", m.bins["svn-indexer"], "serve", "--config", filepath.Join(base, "indexer.json"))
	m.waitHTTP(http.DefaultClient, m.indexer+"/v1/status", 200, 40*time.Second, c)
	fmt.Printf("phase=indexer url=%s\n", m.indexer)
	_ = info
}

func (m *mile) bootAPI() {
	m.t.Helper()
	m.worker = filepath.Join(m.work, "media")
	mustMkdir(filepath.Join(m.worker, "quarantine"))
	pg := m.startPostgres()
	port := freePort(m.t)
	m.api = "http://127.0.0.1:" + strconv.Itoa(port)
	cfg := filepath.Join(m.work, "api.json")
	raw, err := os.ReadFile(filepath.Join(m.root, "services/api/fixtures/development.json"))
	if err != nil {
		m.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		m.t.Fatal(err)
	}
	doc["listen"] = "127.0.0.1:" + strconv.Itoa(port)
	writeJSON(m.t, cfg, doc)
	cmd := exec.Command(m.bins["svn-api"])
	cmd.Env = append(m.env(),
		"SVN_API_CONFIG="+cfg, "SVN_API_PGHOST=127.0.0.1", "SVN_API_PGPORT="+pg.port,
		"SVN_API_PGDATABASE=svn_api", "SVN_API_PGUSER=svn_api", "SVN_API_PGPASSWORD="+pg.password,
		"SVN_API_QUARANTINE_DIR="+m.worker, "SVN_API_LISTEN="+doc["listen"].(string),
	)
	c := m.startCmd("api", cmd)
	m.waitHTTP(http.DefaultClient, m.api+"/uploads/u00000000000000000000000000000000", 401, 30*time.Second, c)
	m.pg = pg
	fmt.Printf("phase=api url=%s postgres=%s\n", m.api, pg.version)
}

type pgInfo struct {
	port, password, socket, version string
}

func (m *mile) startPostgres() pgInfo {
	m.t.Helper()
	base := filepath.Join(m.work, "api-pg")
	data := filepath.Join(base, "data")
	sock := filepath.Join(base, "sock")
	mustMkdir(sock)
	pass := "milestone-api-pass"
	pw := filepath.Join(base, "pw")
	if err := os.MkdirAll(base, 0o700); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(pw, []byte(pass+"\n"), 0o600); err != nil {
		m.t.Fatal(err)
	}
	bin := "/usr/lib/postgresql/18/bin/"
	m.run("initdb", bin+"initdb", "-D", data, "--username", "svn_api", "--pwfile", pw, "--auth-local=scram-sha-256", "--auth-host=scram-sha-256", "--no-instructions")
	port := strconv.Itoa(freePort(m.t))
	c := m.start("api-postgres", bin+"postgres", "-D", data, "-c", "listen_addresses=127.0.0.1", "-c", "port="+port, "-c", "unix_socket_directories="+sock)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.out.String(), "ready to accept connections") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	ver := m.run("pg-version", bin+"postgres", "--version")
	create := exec.Command(bin+"createdb", "-h", sock, "-p", port, "-U", "svn_api", "svn_api")
	create.Env = append(m.env(), "PGPASSWORD="+pass)
	if out := m.runCmd("createdb", create); out != "" {
		fmt.Print(out)
	}
	fmt.Printf("phase=postgres version=%s port=%s\n", strings.TrimSpace(ver), port)
	return pgInfo{port: port, password: pass, socket: sock, version: strings.TrimSpace(ver)}
}
