package authority

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// numericParams are the devnet test parameter rows of TestDevnetNumeric:
// one row per way a row can match or fail the State.devnet rule.
func numericParams() []*typesv1.DevnetTestParameter {
	return []*typesv1.DevnetTestParameter{
		{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-005", NumericValue: 3600},
		{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-008", NumericValue: 3},
		// A matching row without a numeric value: found, value 0.
		{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-002", RoleName: "DEVNET_TEST_ONLY"},
		// The largest value a row can hold is returned unchanged.
		{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-010", NumericValue: 1<<64 - 1},
		// Another label, or approved policy: never a devnet stand-in.
		{Label: "APPROVED", DecisionId: "DEC-006", NumericValue: 7},
		{Label: "proposed_test_parameter", DecisionId: "DEC-011", NumericValue: 8},
		{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-007", NumericValue: 9, ApprovedPolicy: true},
	}
}

// numericKeeper writes a genesis state holding params into a fresh real
// IAVL store.
func numericKeeper(t *testing.T, params []*typesv1.DevnetTestParameter) (sdk.Context, Keeper, *storetypes.KVStoreKey) {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, ModuleName)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	st, _, _ := readyState()
	k := NewKeeper(keys[ModuleName])
	genesis := &authorityv1.GenesisState{
		Authority:            &authorityv1.AuthorityState{Validators: st.Validators, Submitters: st.Submitters, DevnetTestSeats: st.Seats},
		DevnetTestParameters: params,
	}
	if err := k.Init(ctx, genesis); err != nil {
		t.Fatal(err)
	}
	return ctx, k, keys[ModuleName]
}

// TestDevnetNumeric: DevnetNumeric returns numeric_value under exactly the
// DevnetRole rule (chain id prefix "shortvideo-dev-", label
// PROPOSED_TEST_PARAMETER, approved_policy false). It is not found on any
// other chain id, for a row with another label or approved policy, for a
// missing row, on an empty store, and when the stored state does not
// decode. It agrees with the engine's State.devnet and with DevnetRole on
// every case, and it writes nothing.
func TestDevnetNumeric(t *testing.T) {
	ctx, k, key := numericKeeper(t, numericParams())
	if ctx.ChainID() != msgkittest.ChainID {
		t.Fatalf("test context chain id %q", ctx.ChainID())
	}
	type want struct {
		value uint64
		found bool
	}
	ids := map[string]want{
		"DEC-005":  {3600, true},
		"DEC-008":  {3, true},
		"DEC-002":  {0, true},
		"DEC-010":  {1<<64 - 1, true},
		"DEC-006":  {0, false}, // label APPROVED
		"DEC-011":  {0, false}, // label in lower case
		"DEC-007":  {0, false}, // approved_policy true
		"DEC-099":  {0, false}, // no row
		"dec-005":  {0, false}, // ids compare exactly
		"DEC-005 ": {0, false},
		"":         {0, false},
	}
	chains := map[string]bool{
		msgkittest.ChainID:          true,
		"shortvideo-dev-1":          true,
		"shortvideo-dev-":           true,
		"shortvideo-beta-1":         false,
		"shortvideo-dev":            false,
		"SHORTVIDEO-DEV-1":          false,
		"x-shortvideo-dev-1":        false,
		"":                          false,
		"shortvideo-production-dev": false,
	}
	before := msgkit.StoreSnapshot(ctx.KVStore(key))
	for chain, devnet := range chains {
		c := ctx.WithChainID(chain)
		view := State{ChainID: chain, Params: numericParams()}
		for id, w := range ids {
			if !devnet {
				w = want{}
			}
			value, found := k.DevnetNumeric(c, id)
			if value != w.value || found != w.found {
				t.Errorf("chain %q %q: (%d, %v), want (%d, %v)", chain, id, value, found, w.value, w.found)
			}
			row := view.devnet(id)
			if (row != nil) != found || row.GetNumericValue() != value {
				t.Errorf("chain %q %q: engine rule %v, read (%d, %v)", chain, id, row, value, found)
			}
			if _, roleFound := k.DevnetRole(c, id); roleFound != found {
				t.Errorf("chain %q %q: DevnetRole found %v, DevnetNumeric found %v", chain, id, roleFound, found)
			}
		}
	}
	if !bytes.Equal(before, msgkit.StoreSnapshot(ctx.KVStore(key))) {
		t.Fatal("DevnetNumeric wrote to the store")
	}

	t.Run("empty store", func(t *testing.T) {
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if value, found := NewKeeper(keys[ModuleName]).DevnetNumeric(empty, "DEC-005"); found || value != 0 {
			t.Fatalf("(%d, %v)", value, found)
		}
	})

	t.Run("state does not decode", func(t *testing.T) {
		c, kk, kkey := numericKeeper(t, numericParams())
		c.KVStore(kkey).Set([]byte{stateKey}, []byte{0xff, 0xff, 0xff})
		if value, found := kk.DevnetNumeric(c, "DEC-005"); found || value != 0 {
			t.Fatalf("undecodable state: (%d, %v)", value, found)
		}
		if _, found := kk.DevnetRole(c, "DEC-002"); found {
			t.Fatal("undecodable state: DevnetRole found")
		}
	})

	// DEC-005 arrives from a genesis document the way a node reads it:
	// protojson through AppModule.InitGenesis, numeric_value as a JSON
	// string (protojson's uint64 form). The devnet harness writes the same
	// row (chain/devnet/init.go).
	t.Run("DEC-005 from a genesis document", func(t *testing.T) {
		st, _, _ := readyState()
		base, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&authorityv1.GenesisState{
			Authority: &authorityv1.AuthorityState{Validators: st.Validators, Submitters: st.Submitters, DevnetTestSeats: st.Seats},
		})
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(base, &doc); err != nil {
			t.Fatal(err)
		}
		doc["devnet_test_parameters"] = json.RawMessage(`[
			{"label": "PROPOSED_TEST_PARAMETER", "decision_id": "DEC-005", "numeric_value": "3600"},
			{"label": "PROPOSED_TEST_PARAMETER", "decision_id": "DEC-008", "numeric_value": "3"}
		]`)
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		c, keys := msgkittest.NewContext(t, ModuleName)
		kk := NewKeeper(keys[ModuleName])
		module := NewAppModule(kk, nil)
		if err := module.ValidateGenesis(nil, nil, raw); err != nil {
			t.Fatal(err)
		}
		module.InitGenesis(c.WithGasMeter(storetypes.NewInfiniteGasMeter()), nil, raw)
		if value, found := kk.DevnetNumeric(c, "DEC-005"); !found || value != 3600 {
			t.Fatalf("devnet chain: (%d, %v)", value, found)
		}
		if value, found := kk.DevnetNumeric(c.WithChainID("shortvideo-beta-1"), "DEC-005"); found || value != 0 {
			t.Fatalf("beta chain: (%d, %v)", value, found)
		}
	})
}

// TestPolicyVersionRead: PolicyVersion is the committed policy version the
// engine loads. It is 1 on an empty store and after InitGenesis (no side
// document yet), stays 1 while a proposal is only proposed and approved
// (the side document now exists), becomes 2 after the first executed
// proposal and 3 after the second, and does not move on a rejected
// execution. A side document holding 0 or no policy_version reads 1, as
// load does. A side document that does not decode panics (fail closed),
// exactly where load fails. The read writes nothing.
func TestPolicyVersionRead(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if got := NewKeeper(keys[ModuleName]).PolicyVersion(empty); got != 1 {
			t.Fatalf("empty store: %d", got)
		}
	})

	t.Run("executed proposals", func(t *testing.T) {
		e := newKeeperEnv(t)
		store := e.ctx.KVStore(e.keys[ModuleName])
		if store.Has([]byte{sideKey}) {
			t.Fatal("side document before any message")
		}
		requireVersion := func(want uint64, when string) {
			t.Helper()
			before := e.snapshot(ModuleName)
			if got := e.keeper.PolicyVersion(e.ctx); got != want {
				t.Fatalf("%s: PolicyVersion %d, want %d", when, got, want)
			}
			if loaded := e.policyVersion(t); loaded != want {
				t.Fatalf("%s: load sees %d, want %d", when, loaded, want)
			}
			if !bytes.Equal(before, e.snapshot(ModuleName)) {
				t.Fatalf("%s: PolicyVersion wrote to the store", when)
			}
		}
		requireVersion(1, "after InitGenesis")

		limit := func(value uint64) *authorityv1.MsgProposeChange {
			return payloadProposal(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
				GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: msgkit.LimitMaxPageLimit, Value: value},
			}}, msgkit.LimitMaxPageLimit)
		}
		e.queue(t, e.keeper, "pv-1", limit(50))
		if !store.Has([]byte{sideKey}) {
			t.Fatal("no side document after propose and approve")
		}
		if side := e.side(t); side.PolicyVersion != 1 {
			t.Fatalf("side document policy_version %d", side.PolicyVersion)
		}
		requireVersion(1, "queued, not executed")

		// A rejected execution (unknown proposal) changes nothing.
		before := e.snapshot(ModuleName)
		if _, err := e.keeper.Execute(e.tx(), execute(e.authority, "ex-none", "pv-none")); err == nil {
			t.Fatal("executing an unknown proposal succeeded")
		}
		if !bytes.Equal(before, e.snapshot(ModuleName)) {
			t.Fatal("rejected execution wrote")
		}
		requireVersion(1, "after a rejected execution")

		if _, err := e.keeper.Execute(e.tx(), execute(e.authority, "ex-pv-1", "pv-1")); err != nil {
			t.Fatal(err)
		}
		requireVersion(2, "after the first executed proposal")
		if side := e.side(t); side.PolicyVersion != 2 {
			t.Fatalf("side document policy_version %d", side.PolicyVersion)
		}

		e.queue(t, e.keeper, "pv-2", limit(40))
		requireVersion(2, "second proposal queued")
		if _, err := e.keeper.Execute(e.tx(), execute(e.authority, "ex-pv-2", "pv-2")); err != nil {
			t.Fatal(err)
		}
		requireVersion(3, "after the second executed proposal")
	})

	t.Run("stored documents", func(t *testing.T) {
		cases := []struct {
			name  string
			doc   string
			want  uint64
			panic bool
		}{
			{"no policy_version", `{}`, 1, false},
			{"policy_version 0", `{"policy_version":0}`, 1, false},
			{"policy_version 1", `{"policy_version":1}`, 1, false},
			{"policy_version 7 with other rows", `{"policy_version":7,"governed":[{"id":"max_page_limit","value":5}],"providers":[]}`, 7, false},
			{"largest", `{"policy_version":18446744073709551615}`, 1<<64 - 1, false},
			{"truncated", `{`, 0, true},
			{"not an object", `[]`, 0, true},
			{"string version", `{"policy_version":"2"}`, 0, true},
			{"negative version", `{"policy_version":-1}`, 0, true},
			{"version above uint64", `{"policy_version":18446744073709551616}`, 0, true},
			{"fractional version", `{"policy_version":1.5}`, 0, true},
			{"binary", "\xff\x00", 0, true},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				e := newKeeperEnv(t)
				e.ctx.KVStore(e.keys[ModuleName]).Set([]byte{sideKey}, []byte(c.doc))
				st, loadErr := e.keeper.load(e.ctx)
				if (loadErr != nil) != c.panic {
					t.Fatalf("load error %v, want failure %v", loadErr, c.panic)
				}
				got, panicked := policyVersionOrPanic(e.keeper, e.ctx)
				if panicked != c.panic {
					t.Fatalf("panicked %v, want %v", panicked, c.panic)
				}
				if c.panic {
					return
				}
				if got != c.want || st.PolicyVersion != c.want {
					t.Fatalf("PolicyVersion %d, load %d, want %d", got, st.PolicyVersion, c.want)
				}
			})
		}
	})
}

// policyVersionOrPanic calls PolicyVersion and reports a panic instead of
// propagating it.
func policyVersionOrPanic(k Keeper, ctx sdk.Context) (version uint64, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(string); !ok {
				panic(fmt.Sprintf("PolicyVersion panicked with %T, want a string", r))
			}
			panicked = true
		}
	}()
	return k.PolicyVersion(ctx), false
}
