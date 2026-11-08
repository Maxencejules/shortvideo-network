package config

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
)

// configSeeds is the seed corpus of FuzzConfigParse: the example
// configuration and edits refused by the size, UTF-8, strict-walk, decode,
// and field rules.
func configSeeds(t testing.TB) [][]byte {
	t.Helper()
	ex := example(t)
	set := func(value any, path ...any) []byte { return edit(t, path, value, false) }
	return [][]byte{
		ex,
		set("127.0.0.1/32", "allowlist", 0, "ip"),
		set(true, "audits", 0, "skip_fetch"),
		set(nil, "ca_file"),
		set("https://p.test/x", "audits", 0, "endpoints", 0, "url"),
		set(86401, "evidence_lifetime_seconds"),
		set(1.5, "limits", "idle_read_timeout_ms"),
		bytes.Replace(ex, []byte(`"store_root"`), []byte(`"store_root": "x", "store_root"`), 1),
		append(append([]byte(nil), ex...), []byte("{}")...),
		[]byte("{\"chain_rpc\":\"\xff\"}"),
		[]byte(""), []byte("[]"), []byte("{}"), []byte("null"),
		bytes.Repeat([]byte(" "), MaxFileBytes+1),
	}
}

// TestConfigSeeds: the seed corpus reaches the accepted example and at
// least one refusal per rule family; every refusal wraps ErrInvalid.
func TestConfigSeeds(t *testing.T) {
	accepted, refused := 0, 0
	for _, raw := range configSeeds(t) {
		if _, err := Parse(raw); err == nil {
			accepted++
		} else if errors.Is(err, ErrInvalid) {
			refused++
		} else {
			t.Fatalf("%.80q: %v", raw, err)
		}
	}
	if accepted != 1 || refused != len(configSeeds(t))-1 {
		t.Fatalf("seeds: %d accepted, %d refused", accepted, refused)
	}
}

// FuzzConfigParse: Parse never panics and refuses only with ErrInvalid. An
// accepted configuration parses again to the same value and holds 1 to
// MaxAudits audits, each with either a capability manifest path or 1 to
// MaxEndpoints endpoints whose URLs pass the URL rules, and an allowlist the
// policy accepts.
func FuzzConfigParse(f *testing.F) {
	for _, raw := range configSeeds(f) {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		c, err := Parse(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%q: %v", raw, err)
			}
			return
		}
		again, err := Parse(raw)
		if err != nil || !reflect.DeepEqual(again, c) {
			t.Fatalf("%q: parses again to %+v, %v", raw, again, err)
		}
		if len(c.Audits) < 1 || len(c.Audits) > MaxAudits {
			t.Fatalf("%q: %d audits", raw, len(c.Audits))
		}
		for i, a := range c.Audits {
			if len(a.Endpoints) > MaxEndpoints || (len(a.Endpoints) == 0) == (a.CapabilityManifestPath == "") {
				t.Fatalf("%q: audit %d has %d endpoints and capability manifest %q", raw, i, len(a.Endpoints), a.CapabilityManifestPath)
			}
			for _, e := range a.Endpoints {
				if _, err := policy.ParseEndpoint(e.URL); err != nil {
					t.Fatalf("%q: audit %d endpoint %v", raw, i, err)
				}
			}
		}
		if _, err := policy.New(c.Allowlist); err != nil {
			t.Fatalf("%q: allowlist %v", raw, err)
		}
	})
}
