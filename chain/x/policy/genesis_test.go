package policy_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// appDocument is the policy document chain/app writes (state.go
// policyState): proto names, no unpopulated fields, decision rows sorted.
const appDocument = `{"policy":{"policy_version":"1","safety_caps":{"safety_max_package_bytes":"268435456",` +
	`"safety_max_evidence_batch":32,"safety_max_expiry_steps_per_block":128,"safety_max_page_limit":100,` +
	`"safety_max_tx_bytes":"65536","safety_max_gas_per_tx":"20000000","safety_max_open_offers_per_provider":1024},` +
	`"blocked_decisions":[{"decision_id":"DEC-001","schema_version":1},{"decision_id":"DEC-002","schema_version":1},` +
	`{"decision_id":"DEC-003","schema_version":1},{"decision_id":"DEC-004","schema_version":1},` +
	`{"decision_id":"DEC-005","schema_version":1},{"decision_id":"DEC-006","schema_version":1},` +
	`{"decision_id":"DEC-007","schema_version":1},{"decision_id":"DEC-008","schema_version":1}]}}`

func mutated(edit func(*policyv1.PolicyState)) *policyv1.GenesisState {
	state := policy.DefaultGenesisState()
	edit(state.Policy)
	return state
}

// TestGenesisValidateImportExport: one invalid document per rule; the app's
// document validates; InitGenesis needs an empty store and writes nothing
// on an error; a fresh state exports and re-imports to the same store; the
// export is refused, naming the prefix, for every row the document cannot
// carry and for app or policy versions other than 1.
func TestGenesisValidateImportExport(t *testing.T) {
	state, err := policy.ParseGenesis(json.RawMessage(appDocument))
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateGenesis(state); err != nil {
		t.Fatalf("app document: %v", err)
	}
	if !proto.Equal(state, policy.DefaultGenesisState()) {
		t.Fatal("the app document differs from DefaultGenesisState")
	}
	parseErrors := map[string]string{
		"empty":         "",
		"unknown field": `{"policy":{"policy_version":"1"},"extra":1}`,
		"not json":      `{`,
	}
	for name, raw := range parseErrors {
		if _, err := policy.ParseGenesis(json.RawMessage(raw)); err == nil {
			t.Fatalf("parse %s: accepted", name)
		}
	}
	unknownCaps := policy.DefaultGenesisState()
	unknownCaps.Policy.SafetyCaps.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
	unknownRow := policy.DefaultGenesisState()
	unknownRow.Policy.BlockedDecisions[3].ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
	unknownState := policy.DefaultGenesisState()
	unknownState.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
	invalid := map[string]*policyv1.GenesisState{
		"nil":                nil,
		"{} (no policy)":     {},
		"policy_version 0":   mutated(func(p *policyv1.PolicyState) { p.PolicyVersion = 0 }),
		"policy_version 2":   mutated(func(p *policyv1.PolicyState) { p.PolicyVersion = 2 }),
		"no caps":            mutated(func(p *policyv1.PolicyState) { p.SafetyCaps = nil }),
		"tx bytes changed":   mutated(func(p *policyv1.PolicyState) { p.SafetyCaps.SafetyMaxTxBytes = 65535 }),
		"gas changed":        mutated(func(p *policyv1.PolicyState) { p.SafetyCaps.SafetyMaxGasPerTx++ }),
		"page limit changed": mutated(func(p *policyv1.PolicyState) { p.SafetyCaps.SafetyMaxPageLimit = 99 }),
		"caps unknown field": unknownCaps,
		"row unknown field":  unknownRow,
		"state unknown":      unknownState,
		"seven rows":         mutated(func(p *policyv1.PolicyState) { p.BlockedDecisions = p.BlockedDecisions[:7] }),
		"nine rows": mutated(func(p *policyv1.PolicyState) {
			p.BlockedDecisions = append(p.BlockedDecisions, &typesv1.BlockedDecisionParameter{DecisionId: "DEC-009", SchemaVersion: 1})
		}),
		"duplicate row": mutated(func(p *policyv1.PolicyState) {
			p.BlockedDecisions[7] = &typesv1.BlockedDecisionParameter{DecisionId: "DEC-001", SchemaVersion: 1}
		}),
		"unknown decision": mutated(func(p *policyv1.PolicyState) { p.BlockedDecisions[7].DecisionId = "DEC-099" }),
		"nil row":          mutated(func(p *policyv1.PolicyState) { p.BlockedDecisions[2] = nil }),
		"approved":         mutated(func(p *policyv1.PolicyState) { p.BlockedDecisions[0].Approved = true }),
		"value_version 1":  mutated(func(p *policyv1.PolicyState) { p.BlockedDecisions[4].ValueVersion = 1 }),
		"schema_version 2": mutated(func(p *policyv1.PolicyState) { p.BlockedDecisions[5].SchemaVersion = 2 }),
		"schema_version 0": mutated(func(p *policyv1.PolicyState) { p.BlockedDecisions[6].SchemaVersion = 0 }),
	}
	for name, bad := range invalid {
		if err := policy.ValidateGenesis(bad); err == nil {
			t.Fatalf("%s: accepted", name)
		}
		e := newBareEnv(t)
		before := e.snapshot()
		if err := e.k.InitGenesis(e.ctx, bad); err == nil {
			t.Fatalf("%s: imported", name)
		}
		if !bytes.Equal(before, e.snapshot()) {
			t.Fatalf("%s: a refused import wrote", name)
		}
	}
	reordered := mutated(func(p *policyv1.PolicyState) {
		p.BlockedDecisions[0], p.BlockedDecisions[7] = p.BlockedDecisions[7], p.BlockedDecisions[0]
	})
	if err := policy.ValidateGenesis(reordered); err != nil {
		t.Fatalf("rows in another order: %v", err)
	}

	t.Run("requires an empty store", func(t *testing.T) {
		for name, key := range map[string][]byte{
			"scaffold key":   {0x01},
			"layout key":     msgkit.LayoutVersionKey,
			"unrelated row":  {0x09, 'x'},
			"governed value": policy.GovernedKey(msgkit.LimitMaxPageLimit),
		} {
			e := newBareEnv(t)
			e.store().Set(key, []byte{1})
			before := e.snapshot()
			if err := e.k.InitGenesis(e.ctx, policy.DefaultGenesisState()); err == nil || !strings.Contains(err.Error(), "store is not empty") {
				t.Fatalf("%s: %v", name, err)
			}
			if !bytes.Equal(before, e.snapshot()) {
				t.Fatalf("%s: a refused import wrote", name)
			}
		}
	})

	t.Run("fresh round trip", func(t *testing.T) {
		e := newEnv(t)
		exported, err := e.k.ExportGenesis(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(exported, policy.DefaultGenesisState()) {
			t.Fatalf("export %v", exported)
		}
		raw, err := policy.MarshalGenesis(exported)
		if err != nil {
			t.Fatal(err)
		}
		back, err := policy.ParseGenesis(raw)
		if err != nil {
			t.Fatal(err)
		}
		second := newBareEnv(t)
		if err := second.k.InitGenesis(second.ctx, back); err != nil {
			t.Fatal(err)
		}
		if err := second.k.InitVersions(second.ctx, testVersionMap(), 1); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(e.snapshot(), second.snapshot()) {
			t.Fatal("re-imported store differs")
		}
		// The decision rows come from the authority source, copied.
		exported.Policy.BlockedDecisions[0].Approved = true
		if e.auth.decisions[0].GetApproved() {
			t.Fatal("export shares the authority rows")
		}
	})

	t.Run("export refusal", func(t *testing.T) {
		refusals := []struct {
			name, want string
			apply      func(*env)
		}{
			{"governed value", "governed values (prefix 0x02)", func(e *env) {
				e.k.SetLimit(e.fresh(), msgkit.LimitMaxPageLimit, 5, "p1")
			}},
			{"pending change", "pending governed changes (prefix 0x03)", func(e *env) {
				e.auth.version = 2
				e.k.SchedulePending(e.fresh(), msgkit.LimitMaxPageLimit, 5, h+5, "p1", 2)
			}},
			{"plan", "an upgrade plan (prefix 0x06)", func(e *env) {
				e.k.ScheduleUpgrade(e.fresh(), "up-1", h+5, digestOf("r"), "p1")
			}},
			{"applied upgrade", "applied upgrades (prefix 0x07)", func(e *env) {
				e.store().Set(policy.AppliedKey("up-0"), policy.EncodeU64(3))
			}},
			{"app version 2", "app version (prefix 0x04) is 2", func(e *env) {
				e.store().Set(policy.AppVersionKey(), policy.EncodeU64(2))
			}},
			{"app version absent", "app version (prefix 0x04) is 0", func(e *env) {
				e.store().Delete(policy.AppVersionKey())
			}},
			{"policy version 2", "policy version is 2", func(e *env) { e.auth.version = 2 }},
		}
		for _, r := range refusals {
			e := newEnv(t)
			r.apply(e)
			err := e.k.ExportCheck(e.ctx)
			if !errors.Is(err, policy.ErrExportRefused) || !strings.Contains(err.Error(), r.want) {
				t.Fatalf("%s: %v, want %q", r.name, err, r.want)
			}
			if _, err := e.k.ExportGenesis(e.ctx); !errors.Is(err, policy.ErrExportRefused) {
				t.Fatalf("%s: ExportGenesis %v", r.name, err)
			}
			am := policy.NewAppModule(e.k, nil)
			mustPanic(t, "policy export refused", func() { am.ExportGenesis(e.ctx, nil) })
		}
	})

	t.Run("export of damaged state", func(t *testing.T) {
		e := newEnv(t)
		e.store().Set(policy.AppVersionKey(), []byte{1})
		if err := e.k.ExportCheck(e.ctx); err == nil || errors.Is(err, policy.ErrExportRefused) {
			t.Fatalf("damaged app version: %v", err)
		}
		e = newEnv(t)
		e.store().Delete(policy.SafetyCapsKey())
		if _, err := e.k.ExportGenesis(e.ctx); err == nil || !strings.Contains(err.Error(), "safety caps row missing") {
			t.Fatalf("missing caps: %v", err)
		}
		e = newEnv(t)
		e.auth.decisions = e.auth.decisions[:7]
		if _, err := e.k.ExportGenesis(e.ctx); err == nil || !strings.Contains(err.Error(), "7 blocked decision rows") {
			t.Fatalf("authority rows that do not validate: %v", err)
		}
	})

	t.Run("version map is not exported", func(t *testing.T) {
		e := newEnv(t)
		if len(e.k.VersionMap(e.ctx)) == 0 {
			t.Fatal("no version map")
		}
		if err := e.k.ExportCheck(e.ctx); err != nil {
			t.Fatalf("version map rows refused the export: %v", err)
		}
	})
}

// TestDefaultGenesisApprovesNothing: the default document blocks every
// decision and carries the protocol caps.
func TestDefaultGenesisApprovesNothing(t *testing.T) {
	state := policy.DefaultGenesisState()
	if len(state.GetPolicy().GetBlockedDecisions()) != len(genesis.DecisionIDs) {
		t.Fatal("decision rows")
	}
	for i, row := range state.GetPolicy().GetBlockedDecisions() {
		if row.GetDecisionId() != genesis.DecisionIDs[i] || row.GetApproved() || row.GetValueVersion() != 0 || row.GetSchemaVersion() != 1 {
			t.Fatalf("row %d %v", i, row)
		}
	}
	if !proto.Equal(state.GetPolicy().GetSafetyCaps(), policy.DefaultSafetyCaps()) || state.GetPolicy().GetPolicyVersion() != 1 {
		t.Fatal("caps or version")
	}
}

// FuzzPolicyGenesisJSON: ParseGenesis and ValidateGenesis never panic; a
// document they accept equals DefaultGenesisState up to the order of the
// decision rows, imports into an empty store, and its export encoding
// parses and validates again.
func FuzzPolicyGenesisJSON(f *testing.F) {
	f.Add([]byte(appDocument))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"policy":{"policy_version":"1"}}`))
	f.Add([]byte(strings.Replace(appDocument, `"DEC-008","schema_version":1`, `"DEC-008","schema_version":1,"approved":true`, 1)))
	f.Fuzz(func(t *testing.T, raw []byte) {
		state, err := policy.ParseGenesis(raw)
		if err != nil {
			return
		}
		if policy.ValidateGenesis(state) != nil {
			return
		}
		p := state.GetPolicy()
		if p.GetPolicyVersion() != 1 || !proto.Equal(p.GetSafetyCaps(), policy.DefaultSafetyCaps()) || len(p.GetBlockedDecisions()) != len(genesis.DecisionIDs) {
			t.Fatalf("accepted %s", raw)
		}
		for _, row := range p.GetBlockedDecisions() {
			if row.GetApproved() || row.GetValueVersion() != 0 {
				t.Fatalf("accepted an approved row: %s", raw)
			}
		}
		e := newBareEnv(t)
		if err := e.k.InitGenesis(e.ctx, state); err != nil {
			t.Fatalf("accepted document did not import: %v", err)
		}
		out, err := policy.MarshalGenesis(state)
		if err != nil {
			t.Fatal(err)
		}
		again, err := policy.ParseGenesis(out)
		if err != nil || policy.ValidateGenesis(again) != nil {
			t.Fatalf("export encoding of an accepted document: %v", err)
		}
	})
}
