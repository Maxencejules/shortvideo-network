package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

const devChain = "shortvideo-dev-cfgtest"

// serveFile is a complete serve configuration as a JSON object.
func serveFile() map[string]any {
	return map[string]any{
		"chain_id": devChain,
		"rpc_url":  "http://127.0.0.1:26657",
		"database": map[string]any{
			"socket_dir": "/tmp/svn-pg-x/socket", "port": 5432, "name": "broadcaster", "role": "svn_broadcaster",
			"password_file": "/run/broadcaster/db-password", "application_name": "svn-broadcaster",
			"connect_timeout_seconds": 10,
		},
		"listen": map[string]any{
			"address": "127.0.0.1:8443", "tls_cert_file": "/run/broadcaster/server.pem",
			"tls_key_file": "/run/broadcaster/server-key.pem", "client_ca_file": "/run/broadcaster/ca.pem",
		},
		"signer":              map[string]any{"socket_path": "/run/broadcaster-signer/sock", "serve_uid": 2001, "signer_uid": 2002},
		"intent_key_registry": "/etc/broadcaster/intent-keys.json",
		"callers": []any{
			map[string]any{"principal": "app-fixture", "kinds": []string{"REGISTER_CONTENT", "OFFER_LEASE"}},
			map[string]any{"principal": "provider-agent", "kinds": []string{"RELAY_END_LEASE"}, "open_intent_cap": 16,
				"bound_provider_id": "provider-1"},
		},
	}
}

func signerFile() map[string]any {
	return map[string]any{
		"chain_id":            devChain,
		"rpc_url":             "http://127.0.0.1:26657",
		"signer":              map[string]any{"socket_path": "/run/broadcaster-signer/sock", "serve_uid": 2001, "signer_uid": 2002},
		"intent_key_registry": "/etc/broadcaster/intent-keys.json",
		"keys": []any{
			map[string]any{"name": "service", "class": "SERVICE", "path": "/var/lib/signer/service.key"},
			map[string]any{"name": "broadcaster", "class": "BROADCASTER", "path": "/var/lib/signer/broadcaster.key"},
		},
	}
}

func parse(t *testing.T, m map[string]any) (*Config, error) {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return Parse(raw)
}

func mustParse(t *testing.T, m map[string]any) *Config {
	t.Helper()
	c, err := parse(t, m)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return c
}

func TestParseServeAndSigner(t *testing.T) {
	c := mustParse(t, serveFile())
	if err := c.RequireServe(); err != nil {
		t.Fatal(err)
	}
	if c.Numbers != ProposedNumbers() {
		t.Fatal("left-out numbers do not take their PROPOSED values")
	}
	if c.Signer.Boundary() != policy.DistinctUIDs {
		t.Fatal("two uids are the distinct-uid boundary")
	}
	s := mustParse(t, signerFile())
	if err := s.RequireSigner(); err != nil {
		t.Fatal(err)
	}
	same := signerFile()
	same["signer"] = map[string]any{"socket_path": "/run/s/sock", "serve_uid": 1001, "signer_uid": 1001}
	if mustParse(t, same).Signer.Boundary() != policy.SameUID {
		t.Fatal("one uid is the same-uid fallback")
	}
}

// TestNoDefaultChainIDOrRPC checks plan section 11: the chain id and the
// node RPC URL are required and have no default.
func TestNoDefaultChainIDOrRPC(t *testing.T) {
	for _, key := range []string{"chain_id", "rpc_url"} {
		m := serveFile()
		delete(m, key)
		if _, err := parse(t, m); err == nil {
			t.Fatalf("configuration without %s accepted", key)
		}
	}
}

func TestRPCURLRules(t *testing.T) {
	ok := map[string]bool{
		"http://127.0.0.1:26657": false, "http://[::1]:26657": false, "http://127.0.0.2:1/": false,
		"https://node.example.org:443": true,
	}
	for u, allow := range ok {
		if err := checkRPCURL(u, allow); err != nil {
			t.Fatalf("%s refused: %v", u, err)
		}
	}
	bad := map[string]bool{
		"":                                false,
		"http://10.0.0.1:26657":           true, // non-loopback http, even with allow_remote_rpc
		"http://localhost:26657":          false,
		"https://node.example.org:443":    false, // remote without allow_remote_rpc
		"http://127.0.0.1":                false, // no port
		"http://user@127.0.0.1:1":         false,
		"http://127.0.0.1:1/rpc":          false,
		"http://127.0.0.1:1/?x=1":         false,
		"http://127.0.0.1:1/#f":           false,
		"ftp://127.0.0.1:1":               false,
		"127.0.0.1:26657":                 false,
		"http://127.0.0.1:0":              false,
		"http://[::1%25lo]:26657/extra/x": false,
	}
	for u, allow := range bad {
		if err := checkRPCURL(u, allow); err == nil {
			t.Fatalf("%s accepted", u)
		}
	}
}

// TestNumbersHaveHardBounds checks plan section 11 (DR-11-10): every
// Numbers field has hard bounds and a PROPOSED value inside them, a value
// one outside either bound refuses the configuration, and both bounds
// themselves pass the bounds check.
func TestNumbersHaveHardBounds(t *testing.T) {
	var n Numbers
	fields := n.numberFields()
	if len(fields) != reflect.TypeOf(n).NumField() || len(fields) != len(numberBounds) {
		t.Fatalf("%d fields, %d bounds", len(fields), len(numberBounds))
	}
	p := ProposedNumbers()
	if err := p.Validate(); err != nil {
		t.Fatalf("PROPOSED values: %v", err)
	}
	for _, f := range fields {
		b, ok := numberBounds[f.name]
		if !ok {
			t.Fatalf("%s has no bounds", f.name)
		}
		if b.proposed < b.lo || b.proposed > b.hi || b.lo > b.hi {
			t.Fatalf("%s: PROPOSED %d outside [%d, %d]", f.name, b.proposed, b.lo, b.hi)
		}
		for _, v := range []int64{b.lo - 1, b.hi + 1} {
			m := serveFile()
			m["numbers"] = map[string]any{f.name: v}
			_, err := parse(t, m)
			if err == nil || !strings.Contains(err.Error(), f.name) || !strings.Contains(err.Error(), "hard bounds") {
				t.Fatalf("%s=%d: %v", f.name, v, err)
			}
		}
		for _, v := range []int64{b.lo, b.hi} {
			q := ProposedNumbers()
			for _, g := range q.numberFields() {
				if g.name == f.name {
					*g.v = v
				}
			}
			if err := q.checkBounds(); err != nil {
				t.Fatalf("%s=%d at its bound refused: %v", f.name, v, err)
			}
		}
	}
	m := serveFile()
	m["numbers"] = map[string]any{"no_such_number": 1}
	if _, err := parse(t, m); err == nil {
		t.Fatal("unknown number accepted")
	}
	m["numbers"] = map[string]any{"poll_interval_ms": 1.5}
	if _, err := parse(t, m); err == nil {
		t.Fatal("fractional number accepted")
	}
	got := mustParse(t, map[string]any{"chain_id": devChain, "rpc_url": "http://127.0.0.1:1",
		"numbers": map[string]any{"poll_interval_ms": 100}}).Numbers
	if got.PollIntervalMillis != 100 || got.TimeoutOffsetBlocks != 40 {
		t.Fatal("a given number is not used, or a left-out one lost its PROPOSED value")
	}
	for _, line := range ProposedNumbers().Describe() {
		if !strings.HasSuffix(line, "(PROPOSED)") {
			t.Fatalf("%q is not labelled PROPOSED", line)
		}
	}
}

// TestIndexingMarginOneRefused: indexing_margin_blocks 1 is refused at
// parse time (a node reporting height H+1 may not have indexed block H yet,
// outbox.MinIndexingMargin); 2, the lowest allowed value, is accepted and
// reaches the outbox limits.
func TestIndexingMarginOneRefused(t *testing.T) {
	m := serveFile()
	m["numbers"] = map[string]any{"indexing_margin_blocks": 1}
	if _, err := parse(t, m); err == nil || !strings.Contains(err.Error(), "indexing_margin_blocks") {
		t.Fatalf("indexing_margin_blocks 1: %v, want a refusal", err)
	}
	m["numbers"] = map[string]any{"indexing_margin_blocks": 2}
	c := mustParse(t, m)
	if c.Numbers.IndexingMarginBlocks != 2 || numberBounds["indexing_margin_blocks"].proposed != 2 {
		t.Fatalf("indexing margin %d, PROPOSED %d", c.Numbers.IndexingMarginBlocks, numberBounds["indexing_margin_blocks"].proposed)
	}
}

func TestNumbersRelations(t *testing.T) {
	cases := map[string]map[string]any{
		"K above K_max":          {"timeout_offset_blocks": 100, "signer_max_timeout_offset_blocks": 50},
		"backoff base above cap": {"backoff_base_ms": 5000, "backoff_cap_seconds": 4},
		"lifetime min above max": {"intent_lifetime_min_seconds": 600, "intent_lifetime_max_seconds": 300},
		"relay expiry below min": {"intent_lifetime_min_seconds": 600, "relay_local_expiry_seconds": 300},
		"default cap above sum":  {"open_intents_per_caller": 300, "caller_caps_sum_max": 200},
	}
	for name, nums := range cases {
		m := map[string]any{"chain_id": devChain, "rpc_url": "http://127.0.0.1:1", "numbers": nums}
		if _, err := parse(t, m); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	d := ProposedNumbers()
	if d.PollInterval() != 500*time.Millisecond || d.ObserverFreshness() != 2*time.Second || d.MinLifetime() != 30*time.Second ||
		d.RelayLocalExpiry() != time.Hour || d.BackoffCap() != time.Minute || d.EnqueueLockTimeout() != 2*time.Second {
		t.Fatal("duration helpers")
	}
}

// TestUnknownFieldsRefused checks the strict decoder at every level,
// including any field that could hold a hosting-intent key (plan 7.4):
// the configuration has none.
func TestUnknownFieldsRefused(t *testing.T) {
	edits := map[string]func(m map[string]any){
		"top level intent key": func(m map[string]any) { m["intent_key"] = "00" },
		"hosting intent seed":  func(m map[string]any) { m["hosting_intent_seed"] = "00" },
		"database dsn":         func(m map[string]any) { m["database"].(map[string]any)["dsn"] = "postgres://" },
		"database sslmode":     func(m map[string]any) { m["database"].(map[string]any)["sslmode"] = "disable" },
		"database password":    func(m map[string]any) { m["database"].(map[string]any)["password"] = "x" },
		"listen extra":         func(m map[string]any) { m["listen"].(map[string]any)["client_auth"] = "none" },
		"caller extra":         func(m map[string]any) { m["callers"].([]any)[0].(map[string]any)["admin"] = true },
		"null value":           func(m map[string]any) { m["listen"] = nil },
		"rpc url as number":    func(m map[string]any) { m["rpc_url"] = 1 },
	}
	for name, edit := range edits {
		m := serveFile()
		edit(m)
		if _, err := parse(t, m); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := Parse([]byte(`{"chain_id":"` + devChain + `","chain_id":"other","rpc_url":"http://127.0.0.1:1"}`)); err == nil {
		t.Fatal("duplicate key accepted")
	}
	if _, err := Parse([]byte(strings.Repeat(" ", MaxFileBytes+1))); err == nil {
		t.Fatal("oversized file accepted")
	}
}

// TestDevPrincipalsRefuseNonDevChain checks plan 5.6 and 7.1: callers
// (development principals) and keys (development keys) refuse a chain id
// without the shortvideo-dev- prefix.
func TestDevPrincipalsRefuseNonDevChain(t *testing.T) {
	m := serveFile()
	m["chain_id"] = "shortvideo-beta-1"
	if _, err := parse(t, m); err == nil || !strings.Contains(err.Error(), "development principals") {
		t.Fatalf("callers on a non-dev chain: %v", err)
	}
	s := signerFile()
	s["chain_id"] = "shortvideo-beta-1"
	if _, err := parse(t, s); err == nil || !strings.Contains(err.Error(), "development keys") {
		t.Fatalf("keys on a non-dev chain: %v", err)
	}
	m = serveFile()
	m["chain_id"] = "shortvideo-beta-1"
	delete(m, "callers")
	if _, err := parse(t, m); err != nil {
		t.Fatalf("no principals and no keys on a non-dev chain: %v", err)
	}
}

// TestCapsSumBounded checks plan 5.7: the per-caller caps (explicit or the
// default) sum to at most caller_caps_sum_max, so the global number of
// open intents is bounded without a global lock.
func TestCapsSumBounded(t *testing.T) {
	m := serveFile()
	m["numbers"] = map[string]any{"caller_caps_sum_max": 272}
	c := mustParse(t, m) // 256 (default) + 16
	want := []CallerRegistration{{Principal: "app-fixture", OpenIntentCap: 256}, {Principal: "provider-agent", OpenIntentCap: 16}}
	if regs := c.CallerRegistrations(); !reflect.DeepEqual(regs, want) {
		t.Fatalf("registrations %+v", regs)
	}
	m["numbers"] = map[string]any{"caller_caps_sum_max": 271}
	if _, err := parse(t, m); err == nil || !strings.Contains(err.Error(), "caller_caps_sum_max") {
		t.Fatalf("caps above the sum accepted: %v", err)
	}
	for _, v := range []int64{0, MaxCallerCap + 1} {
		m := serveFile()
		m["callers"].([]any)[1].(map[string]any)["open_intent_cap"] = v
		if _, err := parse(t, m); err == nil {
			t.Fatalf("cap %d accepted", v)
		}
	}
}

func TestCallersAndKeysRules(t *testing.T) {
	callerEdits := map[string]func(c []any){
		"duplicate principal": func(c []any) { c[1].(map[string]any)["principal"] = "app-fixture" },
		"bad principal":       func(c []any) { c[0].(map[string]any)["principal"] = "App Fixture" },
		"no kinds":            func(c []any) { c[0].(map[string]any)["kinds"] = []string{} },
		"unknown kind":        func(c []any) { c[0].(map[string]any)["kinds"] = []string{"AUTHORITY_END"} },
		"repeated kind":       func(c []any) { c[0].(map[string]any)["kinds"] = []string{"OFFER_LEASE", "OFFER_LEASE"} },
		"bad provider":        func(c []any) { c[1].(map[string]any)["bound_provider_id"] = "Provider One" },
	}
	for name, edit := range callerEdits {
		m := serveFile()
		edit(m["callers"].([]any))
		if _, err := parse(t, m); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	keyEdits := map[string]func(m map[string]any){
		"three keys": func(m map[string]any) {
			m["keys"] = append(m["keys"].([]any), map[string]any{"name": "third", "class": "SERVICE", "path": "/k"})
		},
		"two service keys": func(m map[string]any) { m["keys"].([]any)[1].(map[string]any)["class"] = "SERVICE" },
		"auditor class":    func(m map[string]any) { m["keys"].([]any)[0].(map[string]any)["class"] = "AUDITOR" },
		"bad name":         func(m map[string]any) { m["keys"].([]any)[0].(map[string]any)["name"] = "Service" },
		"same name":        func(m map[string]any) { m["keys"].([]any)[1].(map[string]any)["name"] = "service" },
		"relative path":    func(m map[string]any) { m["keys"].([]any)[0].(map[string]any)["path"] = "service.key" },
	}
	for name, edit := range keyEdits {
		m := signerFile()
		edit(m)
		if _, err := parse(t, m); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestSectionRules(t *testing.T) {
	sectionEdits := map[string]func(m map[string]any){
		"listen on all addresses": func(m map[string]any) { m["listen"].(map[string]any)["address"] = "0.0.0.0:8443" },
		"listen on a host name":   func(m map[string]any) { m["listen"].(map[string]any)["address"] = "localhost:8443" },
		"listen port 0":           func(m map[string]any) { m["listen"].(map[string]any)["address"] = "127.0.0.1:0" },
		"relative cert":           func(m map[string]any) { m["listen"].(map[string]any)["tls_cert_file"] = "server.pem" },
		"unclean key path":        func(m map[string]any) { m["listen"].(map[string]any)["tls_key_file"] = "/run/../run/k.pem" },
		"db relative socket":      func(m map[string]any) { m["database"].(map[string]any)["socket_dir"] = "sock" },
		"db port 0":               func(m map[string]any) { m["database"].(map[string]any)["port"] = 0 },
		"db bad role":             func(m map[string]any) { m["database"].(map[string]any)["role"] = "Svn-Role" },
		"db timeout":              func(m map[string]any) { m["database"].(map[string]any)["connect_timeout_seconds"] = 61 },
		"db relative passfile":    func(m map[string]any) { m["database"].(map[string]any)["password_file"] = "pw" },
		"root serve uid":          func(m map[string]any) { m["signer"].(map[string]any)["serve_uid"] = 0 },
		"root signer uid":         func(m map[string]any) { m["signer"].(map[string]any)["signer_uid"] = 0 },
		"long socket path": func(m map[string]any) {
			m["signer"].(map[string]any)["socket_path"] = "/" + strings.Repeat("s", MaxSocketPath)
		},
		"relative registry": func(m map[string]any) { m["intent_key_registry"] = "intent-keys.json" },
		"gas for forward":   func(m map[string]any) { m["declared_gas_by_kind"] = map[string]int64{"FORWARD_PROVIDER_TX": 100} },
		"gas unknown kind":  func(m map[string]any) { m["declared_gas_by_kind"] = map[string]int64{"X": 100} },
		"gas above bound":   func(m map[string]any) { m["declared_gas_by_kind"] = map[string]int64{"OFFER_LEASE": 20_000_001} },
		"chain id too long": func(m map[string]any) { m["chain_id"] = devChain + strings.Repeat("x", 40) },
	}
	for name, edit := range sectionEdits {
		m := serveFile()
		edit(m)
		if _, err := parse(t, m); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := LoopbackAddress("[::1]:8443"); err != nil {
		t.Fatal(err)
	}
}

func TestRequireSections(t *testing.T) {
	for _, drop := range []string{"database", "listen", "signer", "intent_key_registry", "callers"} {
		m := serveFile()
		delete(m, drop)
		if err := mustParse(t, m).RequireServe(); err == nil {
			t.Fatalf("serve without %s accepted", drop)
		}
	}
	m := serveFile()
	m["keys"] = signerFile()["keys"]
	if err := mustParse(t, m).RequireServe(); err == nil {
		t.Fatal("serve configuration naming key files accepted")
	}
	for _, drop := range []string{"signer", "keys", "intent_key_registry"} {
		s := signerFile()
		delete(s, drop)
		if err := mustParse(t, s).RequireSigner(); err == nil {
			t.Fatalf("signer without %s accepted", drop)
		}
	}
	for _, add := range []string{"database", "listen", "callers"} {
		s := signerFile()
		s[add] = serveFile()[add]
		if err := mustParse(t, s).RequireSigner(); err == nil {
			t.Fatalf("signer configuration with %s accepted", add)
		}
	}
	s := signerFile()
	s["rpc_url"], s["allow_remote_rpc"] = "https://node.example.org:443", true
	if err := mustParse(t, s).RequireSigner(); err == nil {
		t.Fatal("signer over a remote node accepted")
	}
	if err := mustParse(t, signerFile()).RequireDatabase(); err == nil {
		t.Fatal("database requirement passed without a database")
	}
	if err := mustParse(t, serveFile()).RequireDatabase(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckProcessUID(t *testing.T) {
	c := mustParse(t, serveFile())
	if err := c.CheckProcessUID(false, 2001); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckProcessUID(true, 2002); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		signer bool
		uid    uint32
	}{{false, 2002}, {true, 2001}, {false, 0}, {true, 0}, {false, 3}} {
		if err := c.CheckProcessUID(tc.signer, tc.uid); err == nil {
			t.Fatalf("signer=%v uid %d accepted", tc.signer, tc.uid)
		}
	}
	m := serveFile()
	delete(m, "signer")
	if err := mustParse(t, m).CheckProcessUID(false, 2001); err == nil {
		t.Fatal("uid check without a signer section")
	}
}

// fakeNode is a unit-test stand-in for the node's /status.
type fakeNode struct {
	chainID string
	err     error
}

func (f fakeNode) Status(context.Context) (chain.Status, error) {
	return chain.Status{ChainID: f.chainID, Height: 10}, f.err
}

// TestRefusesChainIDMismatch checks plan section 11 and 7.6: the start-up
// check refuses a node or a database of another chain id.
func TestRefusesChainIDMismatch(t *testing.T) {
	c := mustParse(t, serveFile())
	ctx := context.Background()
	dbOK := func(_ context.Context, id string) error {
		if id != devChain {
			return store.ErrChainMismatch
		}
		return nil
	}
	if err := c.VerifyChainID(ctx, fakeNode{chainID: devChain}, dbOK); err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyChainID(ctx, fakeNode{chainID: "shortvideo-dev-other"}, dbOK); !errors.Is(err, ErrChainIDMismatch) {
		t.Fatalf("node of another chain: %v", err)
	}
	dbOther := func(context.Context, string) error { return store.ErrChainMismatch }
	if err := c.VerifyChainID(ctx, fakeNode{chainID: devChain}, dbOther); !errors.Is(err, ErrChainIDMismatch) {
		t.Fatalf("database of another chain: %v", err)
	}
	dbUnbound := func(context.Context, string) error { return store.ErrChainUnbound }
	if err := c.VerifyChainID(ctx, fakeNode{chainID: devChain}, dbUnbound); err == nil || errors.Is(err, ErrChainIDMismatch) {
		t.Fatalf("unbound database: %v", err)
	}
	if err := c.VerifyChainID(ctx, fakeNode{err: chain.ErrChainID}, dbOK); err == nil {
		t.Fatal("node error accepted")
	}
	if err := c.VerifyChainID(ctx, nil, dbOK); err == nil {
		t.Fatal("no node accepted")
	}
}

// pqEnvNames are the environment variables lib/pq v1.12.3 reads
// (PQ/connector.go env tags and PQ/conn.go PQGO_DEBUG), plus PG* names
// lib/pq does not read, which the prefix rule refuses as well.
var pqEnvNames = []string{
	"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE", "PGSERVICE",
	"PGSERVICEFILE", "PGOPTIONS", "PGAPPNAME", "PGSSLMODE", "PGSSLCERT", "PGSSLKEY", "PGSSLROOTCERT", "PGSSLSNI",
	"PGSSLINLINE", "PGSSLNEGOTIATION", "PGREQUIRESSL", "PGSSLCRL", "PGCONNECT_TIMEOUT", "PGCLIENTENCODING",
	"PGDATESTYLE", "PGTZ", "PGGEQO", "PGTARGETSESSIONATTRS", "PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION",
	"PGKRBSRVNAME", "PGGSSLIB", "PGLOCALEDIR", "PGSYSCONFDIR", "PGDATA", "PQGO_DEBUG",
}

// TestStartRefusesPGEnv checks plan 7.7: each PG* variable and PQGO_DEBUG
// refuses start, and the refusal names the variable, never its value.
func TestStartRefusesPGEnv(t *testing.T) {
	clean := []string{"PATH=/usr/bin", "HOME=/root", "XPG=1", "LANG=C.UTF-8"}
	if err := CheckEnvironment(clean); err != nil {
		t.Fatalf("clean environment refused: %v", err)
	}
	for _, name := range pqEnvNames {
		err := CheckEnvironment(append(clean, name+"=canary-value-7d1c"))
		if !errors.Is(err, ErrEnvironment) || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(err.Error(), "canary-value-7d1c") {
			t.Fatalf("%s: the error quotes the value", name)
		}
	}
	if err := CheckEnvironment([]string{"PGHOST="}); !errors.Is(err, ErrEnvironment) {
		t.Fatal("an empty PG* variable is still set")
	}
}

func TestConversions(t *testing.T) {
	c := mustParse(t, serveFile())
	if err := c.StoreLimits().Validate(); err != nil {
		t.Fatalf("store limits: %v", err)
	}
	if err := c.OutboxLimits().Validate(); err != nil {
		t.Fatalf("outbox limits: %v", err)
	}
	a, err := c.APIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if a.ChainID != devChain || a.ListenAddress != "127.0.0.1:8443" || a.MaxBodyBytes != 65536 || a.PageSize != 100 ||
		a.RelayLocalExpiry != time.Hour || a.ForwardWindow != 100 || a.DeadlinePages != 10 || len(a.Callers) != 2 ||
		a.Callers[1].BoundProviderID != "provider-1" || a.Callers[0].Kinds[1] != policy.KindOfferLease {
		t.Fatalf("api config %+v", a)
	}
	if _, err := mustParse(t, signerFile()).APIConfig(); err == nil {
		t.Fatal("API configuration from a signer file")
	}
	s := mustParse(t, signerFile())
	sc, err := s.SignerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if sc.ChainID != devChain || sc.Boundary != policy.DistinctUIDs || sc.ServeUID != 2001 || len(sc.Keys) != 2 ||
		sc.Keys[1].Class != policy.ClassBroadcaster || sc.MaxTimeoutOffset != 1000 || sc.FreshnessPolls != 4 ||
		sc.PollInterval != 500*time.Millisecond || sc.RegistryPath != "/etc/broadcaster/intent-keys.json" {
		t.Fatalf("signer config %+v", sc)
	}
	if _, err := c.SignerConfig(); err == nil {
		t.Fatal("signer configuration from a serve file")
	}
	m := serveFile()
	m["declared_gas_by_kind"] = map[string]int64{"OFFER_LEASE": 300000}
	g := mustParse(t, m)
	if gas, err := g.GasFor(policy.KindOfferLease); err != nil || gas != 300000 {
		t.Fatalf("override gas %d %v", gas, err)
	}
	if gas, err := g.GasFor(policy.KindRegisterContent); err != nil || gas != 2_000_000 {
		t.Fatalf("default gas %d %v", gas, err)
	}
	if _, err := g.GasFor(policy.KindForwardProviderTx); err == nil {
		t.Fatal("gas for a forward")
	}
	if _, err := g.GasFor("NOPE"); err == nil {
		t.Fatal("gas for an unrouted kind")
	}
}

func TestLoadFileRules(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(serveFile())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "serve.json")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil {
		t.Fatal("symlinked file loaded")
	}
	for _, bad := range []string{"serve.json", dir, filepath.Join(dir, "missing.json"), dir + "/./serve.json"} {
		if _, err := Load(bad); err == nil {
			t.Fatalf("%s loaded", bad)
		}
	}
}

// TestConnConfigPasswordFile checks the password file rules: one line of 1
// to 256 bytes in a file only the reading uid can read.
func TestConnConfigPasswordFile(t *testing.T) {
	dir := t.TempDir()
	pw := filepath.Join(dir, "pw")
	m := serveFile()
	m["database"].(map[string]any)["password_file"] = pw
	c := mustParse(t, m)
	write := func(data string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(pw, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(pw, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("pass-canary\n", 0o600)
	cc, err := c.ConnConfig()
	if err != nil || cc.Password != "pass-canary" || cc.User != "svn_broadcaster" || cc.Database != "broadcaster" ||
		cc.SocketDir != "/tmp/svn-pg-x/socket" || cc.ConnectTimeout != 10*time.Second || cc.Port != 5432 {
		t.Fatalf("%+v %v", cc, err)
	}
	for name, data := range map[string]string{
		"two lines": "a\nb\n", "empty": "\n", "carriage return": "a\r\n", "nul": "a\x00b", "long": strings.Repeat("p", 257),
	} {
		write(data, 0o600)
		if _, err := c.ConnConfig(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	write("pass", 0o640)
	if _, err := c.ConnConfig(); err == nil {
		t.Fatal("group-readable password file accepted")
	}
	if _, err := mustParse(t, signerFile()).ConnConfig(); err == nil {
		t.Fatal("connection settings without a database section")
	}
}
