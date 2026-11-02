package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/verify"
)

const examplePath = "../testdata/config/example.json"

func example(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExampleConfig(t *testing.T) {
	c, err := Load(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		ChainRPC:  "http://127.0.0.1:26657",
		CAFile:    ".local/auditor/test-ca.crt",
		StoreRoot: ".local/auditor/store",
		Limits: fetch.Limits{
			Connect: 5 * time.Second, TLSHandshake: 5 * time.Second, ResponseHeader: 10 * time.Second,
			IdleRead: 10 * time.Second, Object: 300 * time.Second,
		},
		RunBudget: time.Hour,
		Allowlist: []policy.Entry{{ProviderID: "p1", Host: "127.0.0.1", IP: netip.MustParseAddr("127.0.0.1"), Port: 8443}},
		Audits: []Audit{
			{
				LeaseID: "lease-1", ProviderID: "p1", ContentID: "c1", EndpointIndex: 0,
				ManifestPath: ".local/auditor/manifests/c1.json",
				Endpoints:    []Endpoint{{Name: "loopback test endpoint", URL: "https://127.0.0.1:8443/"}},
				Mode:         verify.WholePackage,
			},
			{
				LeaseID: "lease-2", ProviderID: "p1", ContentID: "c2", EndpointIndex: 0,
				ManifestPath: ".local/auditor/manifests/c2.json", CapabilityManifestPath: ".local/auditor/capability/p1.json",
				Mode: verify.SampledSegments, SampleCount: 4,
			},
		},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got %+v", c)
	}
	if _, err := fetch.New(fetch.Options{Policy: mustPolicy(t, c.Allowlist), Limits: c.Limits}); err != nil {
		t.Fatalf("limits rejected by fetch: %v", err)
	}
}

func mustPolicy(t *testing.T, entries []policy.Entry) *policy.Policy {
	t.Helper()
	p, err := policy.New(entries)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The PROPOSED defaults apply when limits are absent.
func TestDefaults(t *testing.T) {
	raw := bytes.Replace(example(t), []byte(`"limits"`), []byte(`"limits_removed"`), 1)
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	delete(generic, "limits_removed")
	delete(generic, "ca_file")
	delete(generic, "allowlist")
	raw, _ = json.Marshal(generic)
	c, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Limits.Connect != 5*time.Second || c.Limits.Object != 5*time.Minute || c.RunBudget != time.Hour || c.CAFile != "" || len(c.Allowlist) != 0 {
		t.Fatalf("%+v", c)
	}
}

// edit applies a JSON edit to the example: path segments name object keys or
// array indexes; a nil value deletes the key.
func edit(t *testing.T, path []any, value any, del bool) []byte {
	t.Helper()
	var doc any
	if err := json.Unmarshal(example(t), &doc); err != nil {
		t.Fatal(err)
	}
	node := doc
	for _, p := range path[:len(path)-1] {
		switch k := p.(type) {
		case string:
			node = node.(map[string]any)[k]
		case int:
			node = node.([]any)[k]
		}
	}
	switch k := path[len(path)-1].(type) {
	case string:
		if del {
			delete(node.(map[string]any), k)
		} else {
			node.(map[string]any)[k] = value
		}
	case int:
		node.([]any)[k] = value
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRejects(t *testing.T) {
	set := func(value any, path ...any) []byte { return edit(t, path, value, false) }
	del := func(path ...any) []byte { return edit(t, path, nil, true) }
	ex := example(t)
	cases := map[string][]byte{
		"empty":                  []byte(""),
		"not an object":          []byte("[]"),
		"trailing data":          append(append([]byte(nil), ex...), []byte("{}")...),
		"null":                   set(nil, "ca_file"),
		"case variant key":       bytes.Replace(ex, []byte(`"chain_rpc"`), []byte(`"Chain_RPC"`), 1),
		"duplicate key":          bytes.Replace(ex, []byte(`"store_root"`), []byte(`"store_root": "x", "store_root"`), 1),
		"unknown key":            set(true, "debug"),
		"unknown nested key":     set(true, "audits", 0, "skip_fetch"),
		"missing chain_rpc":      del("chain_rpc"),
		"chain_rpc scheme":       set("ftp://127.0.0.1:26657", "chain_rpc"),
		"chain_rpc userinfo":     set("http://u:p@127.0.0.1:26657", "chain_rpc"),
		"chain_rpc path":         set("http://127.0.0.1:26657/x", "chain_rpc"),
		"missing store_root":     del("store_root"),
		"empty ca_file":          set("", "ca_file"),
		"NUL in path":            set("a\x00b", "store_root"),
		"limit below bound":      set(99, "limits", "connect_timeout_ms"),
		"limit above bound":      set(60001, "limits", "tls_handshake_timeout_ms"),
		"limit zero":             set(0, "limits", "idle_read_timeout_ms"),
		"limit negative":         set(-1, "limits", "idle_read_timeout_ms"),
		"limit fraction":         set(1.5, "limits", "idle_read_timeout_ms"),
		"limit string":           set("100", "limits", "idle_read_timeout_ms"),
		"object below a phase":   set(4000, "limits", "object_budget_ms"),
		"run below object":       set(200000, "limits", "run_budget_ms"),
		"no audits":              set([]any{}, "audits"),
		"bad lease id":           set("Lease", "audits", 0, "lease_id"),
		"bad provider id":        set("", "audits", 0, "provider_id"),
		"bad content id":         set("c/1", "audits", 0, "content_id"),
		"missing endpoint_index": del("audits", 0, "endpoint_index"),
		"endpoint_index 8":       set(8, "audits", 1, "endpoint_index"),
		"endpoint past list":     set(1, "audits", 0, "endpoint_index"),
		"both endpoint sources":  set("x.json", "audits", 0, "capability_manifest_path"),
		"no endpoint source":     del("audits", 0, "endpoints"),
		"endpoint http":          set([]any{map[string]any{"name": "a", "url": "http://127.0.0.1:8443/"}}, "audits", 0, "endpoints"),
		"endpoint userinfo":      set([]any{map[string]any{"name": "a", "url": "https://u@p.test/"}}, "audits", 0, "endpoints"),
		"endpoint path":          set([]any{map[string]any{"name": "a", "url": "https://p.test/x"}}, "audits", 0, "endpoints"),
		"endpoint empty name":    set([]any{map[string]any{"name": "", "url": "https://p.test/"}}, "audits", 0, "endpoints"),
		"nine endpoints":         set(nineEndpoints(), "audits", 0, "endpoints"),
		"missing manifest_path":  del("audits", 0, "manifest_path"),
		"unknown mode":           set("MERKLE_PROOF", "audits", 0, "mode"),
		"whole with sample":      set(2, "audits", 0, "sample_count"),
		"sampled without count":  del("audits", 1, "sample_count"),
		"sample count 0":         set(0, "audits", 1, "sample_count"),
		"sample count too large": set(32769, "audits", 1, "sample_count"),
		"duplicate lease":        set("lease-1", "audits", 1, "lease_id"),
		"allowlist cidr ip":      set("10.0.0.0/8", "allowlist", 0, "ip"),
		"allowlist wildcard":     set("*", "allowlist", 0, "provider_id"),
		"allowlist link-local":   set(map[string]any{"provider_id": "p1", "host": "metadata.test", "ip": "169.254.169.254", "port": 443}, "allowlist", 0),
		"allowlist port 0":       set(0, "allowlist", 0, "port"),
		"allowlist port 70000":   set(70000, "allowlist", 0, "port"),
		"allowlist extra field":  set(true, "allowlist", 0, "any_port"),
	}
	for name, raw := range cases {
		_, err := Parse(raw)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: %v", name, err)
	}
	many := make([]any, MaxAudits+1)
	for i := range many {
		many[i] = map[string]any{"lease_id": "l" + strings.Repeat("x", i%60) + string(rune('a'+i%26)), "provider_id": "p1", "content_id": "c1",
			"endpoint_index": 0, "manifest_path": "m", "endpoints": []any{map[string]any{"name": "a", "url": "https://p.test/"}}, "mode": "WHOLE_PACKAGE"}
	}
	if _, err := Parse(set(many, "audits")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%d audits: %v", MaxAudits+1, err)
	}
	if _, err := Parse(append(bytes.Repeat([]byte(" "), MaxFileBytes), ex...)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized: %v", err)
	}
	if _, err := Load(t.TempDir()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("directory: %v", err)
	}
}

func nineEndpoints() []any {
	out := make([]any, 9)
	for i := range out {
		out[i] = map[string]any{"name": "e", "url": "https://p.test/"}
	}
	return out
}

// bypassWords are names a check-disabling setting would carry.
var bypassWords = []string{
	"insecure", "skip", "allow_private", "private", "disable", "bypass", "proxy", "no_verify",
	"override", "dial", "resolver", "cidr", "wildcard", "trust", "follow", "redirect",
	"compress", "unsafe", "debug", "test", "any", "all",
}

// walkTypes visits every struct field reachable from t.
func walkTypes(t reflect.Type, visit func(owner reflect.Type, f reflect.StructField)) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			visit(t, t.Field(i))
			walk(t.Field(i).Type)
		}
	}
	walk(t)
}

// TestConfigHasNoBypass shows that configuration cannot switch a check off
// or reach the fetch test seam: no key or field carries a bypass word, every
// value is data (no function, interface, channel, or map), the allowlist
// entry is exactly the four-field tuple, and bypass-style keys are refused
// as unknown.
func TestConfigHasNoBypass(t *testing.T) {
	for _, root := range []reflect.Type{reflect.TypeOf(fileConfig{}), reflect.TypeOf(Config{})} {
		walkTypes(root, func(owner reflect.Type, f reflect.StructField) {
			names := []string{strings.ToLower(f.Name)}
			if tag, _, _ := strings.Cut(f.Tag.Get("json"), ","); tag != "" {
				names = append(names, tag)
			}
			for _, name := range names {
				for _, word := range bypassWords {
					if strings.Contains(name, word) && !(word == "all" && name == "allowlist") {
						t.Errorf("%s.%s carries %q", owner.Name(), f.Name, word)
					}
				}
			}
			switch f.Type.Kind() {
			case reflect.Func, reflect.Interface, reflect.Chan, reflect.Map, reflect.UnsafePointer:
				t.Errorf("%s.%s is a %s", owner.Name(), f.Name, f.Type.Kind())
			}
		})
	}
	entry := reflect.TypeOf(fileEntry{})
	var tags []string
	for i := 0; i < entry.NumField(); i++ {
		tags = append(tags, entry.Field(i).Tag.Get("json"))
	}
	if strings.Join(tags, ",") != "provider_id,host,ip,port" {
		t.Fatalf("allowlist entry fields %v", tags)
	}
	for _, key := range []string{
		"insecure_skip_verify", "allow_private", "allow_private_addresses", "proxy", "https_proxy",
		"follow_redirects", "dial_override", "dial_target", "resolver", "skip_fetch", "disable_policy",
		"accept_encoding", "tls_min_version",
	} {
		raw := edit(t, []any{key}, true, false)
		if _, err := Parse(raw); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s accepted: %v", key, err)
		}
	}
}

func TestLoadBoundsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte(" "), MaxFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}
