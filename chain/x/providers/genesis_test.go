package providers

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// rotatedProvider is a valid record at generation 3 with two retired keys.
func rotatedProvider(id string) *providersv1.Provider {
	p := seededProvider(id, providersv1.ProviderState_PROVIDER_STATE_DRAINING, 1000, 250)
	p.KeyGeneration = 3
	p.PreviousSigningKeys = []*providersv1.PreviousSigningKey{
		{PublicKey: testPub("old-1-" + id), Generation: 1, RetiredUnixSeconds: 1_700_000_000},
		{PublicKey: testPub("old-2-" + id), Generation: 2, RetiredUnixSeconds: 1_700_000_000},
	}
	return p
}

// TestValidateGenesisRejects covers every rule of ValidateGenesis.
func TestValidateGenesisRejects(t *testing.T) {
	valid := func() *providersv1.Provider { return rotatedProvider("p1") }
	cases := map[string]func(*providersv1.GenesisState){
		"nil-record":                func(s *providersv1.GenesisState) { s.Providers = append(s.Providers, nil) },
		"duplicate-id":              func(s *providersv1.GenesisState) { s.Providers = append(s.Providers, valid()) },
		"id-empty":                  func(s *providersv1.GenesisState) { s.Providers[0].ProviderId = "" },
		"id-65":                     func(s *providersv1.GenesisState) { s.Providers[0].ProviderId = "a" + strings.Repeat("b", 64) },
		"id-pattern":                func(s *providersv1.GenesisState) { s.Providers[0].ProviderId = "P1" },
		"account-19":                func(s *providersv1.GenesisState) { s.Providers[0].Account = providerAcct[:19] },
		"account-empty":             func(s *providersv1.GenesisState) { s.Providers[0].Account = nil },
		"pubkey-33":                 func(s *providersv1.GenesisState) { s.Providers[0].SigningPubkey = append(testPub("x"), 0) },
		"key-id-pattern":            func(s *providersv1.GenesisState) { s.Providers[0].SigningKeyId = "Key" },
		"key-id-empty":              func(s *providersv1.GenesisState) { s.Providers[0].SigningKeyId = "" },
		"generation-0":              func(s *providersv1.GenesisState) { s.Providers[0].KeyGeneration = 0 },
		"previous-count-short":      func(s *providersv1.GenesisState) { s.Providers[0].KeyGeneration = 4 },
		"previous-count-long":       func(s *providersv1.GenesisState) { s.Providers[0].KeyGeneration = 2 },
		"previous-nil":              func(s *providersv1.GenesisState) { s.Providers[0].PreviousSigningKeys[1] = nil },
		"previous-generation-order": func(s *providersv1.GenesisState) { s.Providers[0].PreviousSigningKeys[1].Generation = 1 },
		"previous-generation-gap":   func(s *providersv1.GenesisState) { s.Providers[0].PreviousSigningKeys[0].Generation = 0 },
		"previous-pubkey-31":        func(s *providersv1.GenesisState) { s.Providers[0].PreviousSigningKeys[0].PublicKey = testPub("y")[:31] },
		"previous-retired-0":        func(s *providersv1.GenesisState) { s.Providers[0].PreviousSigningKeys[0].RetiredUnixSeconds = 0 },
		"previous-retired-negative": func(s *providersv1.GenesisState) { s.Providers[0].PreviousSigningKeys[0].RetiredUnixSeconds = -5 },
		"previous-retired-year-10000": func(s *providersv1.GenesisState) {
			s.Providers[0].PreviousSigningKeys[1].RetiredUnixSeconds = 253402300800
		},
		"previous-retired-decreasing": func(s *providersv1.GenesisState) {
			s.Providers[0].PreviousSigningKeys[1].RetiredUnixSeconds = 1_699_999_999
		},
		"previous-unknown-field": func(s *providersv1.GenesisState) {
			s.Providers[0].PreviousSigningKeys[0].ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
		},
		"digest-missing":     func(s *providersv1.GenesisState) { s.Providers[0].EndpointManifestDigest = nil },
		"digest-algorithm-0": func(s *providersv1.GenesisState) { s.Providers[0].EndpointManifestDigest.Algorithm = 0 },
		"digest-31":          func(s *providersv1.GenesisState) { s.Providers[0].EndpointManifestDigest.Value = make([]byte, 31) },
		"digest-unknown-field": func(s *providersv1.GenesisState) {
			s.Providers[0].EndpointManifestDigest.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
		},
		"state-unspecified": func(s *providersv1.GenesisState) { s.Providers[0].State = 0 },
		"state-unknown":     func(s *providersv1.GenesisState) { s.Providers[0].State = 5 },
		"capacity-0": func(s *providersv1.GenesisState) {
			s.Providers[0].AdmittedCapacityBytes = 0
			s.Providers[0].ReservedBytes = 0
		},
		"reserved-above":        func(s *providersv1.GenesisState) { s.Providers[0].ReservedBytes = 1001 },
		"group-pattern":         func(s *providersv1.GenesisState) { s.Providers[0].OperatorGroupId = "Group" },
		"group-empty":           func(s *providersv1.GenesisState) { s.Providers[0].OperatorGroupId = "" },
		"record-unknown-field":  func(s *providersv1.GenesisState) { s.Providers[0].ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01}) },
		"genesis-unknown-field": func(s *providersv1.GenesisState) { s.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01}) },
	}
	base := &providersv1.GenesisState{Providers: []*providersv1.Provider{valid()}}
	if err := ValidateGenesis(base); err != nil {
		t.Fatalf("base: %v", err)
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			state := &providersv1.GenesisState{Providers: []*providersv1.Provider{valid()}}
			edit(state)
			if err := ValidateGenesis(state); err == nil {
				t.Fatal("accepted")
			}
			e := newEnv(t)
			if err := e.k.InitGenesis(e.unmetered(), state); err == nil {
				t.Fatal("InitGenesis accepted an invalid document")
			}
			if len(msgkit.StoreSnapshot(e.store(ModuleName))) != 0 {
				t.Fatal("rejected InitGenesis wrote")
			}
		})
	}
	if err := ValidateGenesis(nil); err == nil {
		t.Fatal("nil state accepted")
	}
	// Boundaries that are valid.
	edge := valid()
	edge.ProviderId = "a" + strings.Repeat("b", 63)
	edge.ReservedBytes = edge.AdmittedCapacityBytes
	edge.PreviousSigningKeys[0].RetiredUnixSeconds = 1
	edge.PreviousSigningKeys[1].RetiredUnixSeconds = 253402300799
	edge.PreviousSigningKeys[1].PublicKey = edge.SigningPubkey // rotate-back is allowed
	if err := ValidateGenesis(&providersv1.GenesisState{Providers: []*providersv1.Provider{edge}}); err != nil {
		t.Fatalf("edge values: %v", err)
	}
}

// TestGenesisRoundTrip: init -> export is proto.Equal to the sorted input,
// a re-import of the export gives a byte-identical store, and the JSON
// encoding round-trips through the module.
func TestGenesisRoundTrip(t *testing.T) {
	input := &providersv1.GenesisState{Providers: []*providersv1.Provider{
		rotatedProvider("pz"),
		seededProvider("pa", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 3),
		seededProvider("pm", providersv1.ProviderState_PROVIDER_STATE_DISABLED, 99, 99),
	}}
	first := newEnv(t)
	order := &recorder{}
	module := NewAppModule(first.k, order)
	raw, err := MarshalGenesis(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.ValidateGenesis(nil, nil, raw); err != nil {
		t.Fatalf("module ValidateGenesis: %v", err)
	}
	module.InitGenesis(first.unmetered(), nil, raw)
	if len(order.names) != 1 || order.names[0] != ModuleName {
		t.Fatalf("init order %v", order.names)
	}
	exported, err := ParseGenesis(module.ExportGenesis(first.unmetered(), nil))
	if err != nil {
		t.Fatal(err)
	}
	sorted := &providersv1.GenesisState{Providers: []*providersv1.Provider{input.Providers[1], input.Providers[2], input.Providers[0]}}
	if !proto.Equal(exported, sorted) {
		t.Fatalf("export %v, want %v", exported, sorted)
	}
	second := newEnv(t)
	if err := second.k.InitGenesis(second.unmetered(), exported); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msgkit.StoreSnapshot(first.store(ModuleName)), msgkit.StoreSnapshot(second.store(ModuleName))) {
		t.Fatal("re-import is not byte-identical")
	}
	// Order of the input document does not change the stored bytes.
	third := newEnv(t)
	if err := third.k.InitGenesis(third.unmetered(), sorted); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msgkit.StoreSnapshot(first.store(ModuleName)), msgkit.StoreSnapshot(third.store(ModuleName))) {
		t.Fatal("stored bytes depend on document order")
	}
}

// TestGenesisRoundTripFromCommittedTransitions: a store built by messages
// and council hooks exports a valid genesis whose import reproduces the
// provider records byte for byte. Business ids are not in the v0 genesis
// schema (DR-07-03), so only the 0x01 prefix is compared.
func TestGenesisRoundTripFromCommittedTransitions(t *testing.T) {
	e := newEnv(t)
	e.admitted("p1")
	e.admitted("p2")
	e.registered("p3")
	e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate")
	e.mustOK(e.update(e.fresh(), updMsg("p1", "upd-p1", 42)), "update")
	e.mustOK(e.drain(e.fresh(), drainMsg(t, "p2", "key-p2", 1)), "drain")
	if code := e.k.DisableByCouncil(e.fresh(), "p2"); code != msgkit.OK {
		t.Fatal(code)
	}
	state, err := e.k.ExportGenesis(e.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGenesis(state); err != nil {
		t.Fatal(err)
	}
	other := newEnv(t)
	if err := other.k.InitGenesis(other.unmetered(), state); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prefixSnapshot(e, ProviderKeyPrefix), prefixSnapshot(other, ProviderKeyPrefix)) {
		t.Fatal("provider records differ after export and import")
	}
	if len(prefixSnapshot(other, BusinessKeyPrefix)) != 0 {
		t.Fatal("import wrote business ids")
	}
	again, err := other.k.ExportGenesis(other.unmetered())
	if err != nil || !proto.Equal(state, again) {
		t.Fatalf("second export differs: %v", err)
	}
}

func prefixSnapshot(e *env, prefix byte) []byte {
	var out []byte
	it := e.store(ModuleName).Iterator([]byte{prefix}, []byte{prefix + 1})
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out = append(out, it.Key()...)
		out = append(out, 0xff)
		out = append(out, it.Value()...)
		out = append(out, 0xfe)
	}
	return out
}

func TestGenesisDocumentRules(t *testing.T) {
	module := NewAppModule(newEnv(t).k, nil)
	t.Run("default-is-empty-and-valid", func(t *testing.T) {
		raw := module.DefaultGenesis(nil)
		if err := module.ValidateGenesis(nil, nil, raw); err != nil {
			t.Fatal(err)
		}
		state, err := ParseGenesis(raw)
		if err != nil || len(state.GetProviders()) != 0 {
			t.Fatalf("default %s: %v", raw, err)
		}
	})
	t.Run("empty-document", func(t *testing.T) {
		if err := module.ValidateGenesis(nil, nil, nil); err == nil {
			t.Fatal("empty document accepted")
		}
	})
	t.Run("unknown-json-field", func(t *testing.T) {
		raw := json.RawMessage(`{"providers":[],"extra":1}`)
		if err := module.ValidateGenesis(nil, nil, raw); err == nil {
			t.Fatal("unknown top-level field accepted")
		}
		raw = json.RawMessage(`{"providers":[{"provider_id":"p1","endpoint_url":"https://x.invalid"}]}`)
		if err := module.ValidateGenesis(nil, nil, raw); err == nil {
			t.Fatal("unknown provider field accepted")
		}
	})
	t.Run("init-panics-on-invalid", func(t *testing.T) {
		e := newEnv(t)
		m := NewAppModule(e.k, nil)
		defer func() {
			if recover() == nil {
				t.Fatal("InitGenesis did not panic")
			}
		}()
		m.InitGenesis(e.unmetered(), nil, json.RawMessage(`{"providers":[{"provider_id":"P1"}]}`))
	})
	t.Run("init-into-occupied-store", func(t *testing.T) {
		e := newEnv(t)
		e.registered("p1")
		if err := e.k.InitGenesis(e.unmetered(), &providersv1.GenesisState{}); err == nil {
			t.Fatal("InitGenesis into a store with providers succeeded")
		}
	})
	t.Run("export-rejects-corrupt-record", func(t *testing.T) {
		e := newEnv(t)
		e.registered("p1")
		e.store(ModuleName).Set(ProviderKey("p2"), []byte{0xff, 0xff})
		if _, err := e.k.ExportGenesis(e.unmetered()); err == nil {
			t.Fatal("corrupt record exported")
		}
		raw, _ := proto.Marshal(seededProvider("p3", providersv1.ProviderState_PROVIDER_STATE_APPLIED, 1, 0))
		e.store(ModuleName).Set(ProviderKey("p2"), raw)
		if _, err := e.k.ExportGenesis(e.unmetered()); err == nil {
			t.Fatal("record under the wrong key exported")
		}
		if _, err := e.k.QueryProvider(e.fresh(), &providersv1.QueryProviderRequest{ProviderId: "p2"}); err == nil {
			t.Fatal("query returned a record under the wrong key")
		}
	})
}

type recorder struct{ names []string }

func (r *recorder) Record(name string) { r.names = append(r.names, name) }

// genesisJSON is a valid document for fuzz seeds.
func genesisJSON(t testing.TB, providers ...*providersv1.Provider) []byte {
	t.Helper()
	raw, err := MarshalGenesis(&providersv1.GenesisState{Providers: providers})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
