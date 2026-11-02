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

// numericKeeper writes the unit-test genesis (readyGenesis) with params as
// its devnet test parameters into a fresh real IAVL store.
func numericKeeper(t *testing.T, params []*typesv1.DevnetTestParameter) (sdk.Context, Keeper, *storetypes.KVStoreKey) {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, ModuleName)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	gen, _, _ := readyGenesis()
	gen.DevnetTestParameters = params
	k := NewKeeper(keys[ModuleName])
	if err := k.Init(ctx, gen); err != nil {
		t.Fatal(err)
	}
	return ctx, k, keys[ModuleName]
}

// TestDevnetNumeric: DevnetNumeric returns numeric_value under exactly the
// DevnetRole rule (chain id prefix "shortvideo-dev-", label
// PROPOSED_TEST_PARAMETER, approved_policy false). It is not found on any
// other chain id, for a row with another label or approved policy, for a
// missing row, on an empty store, and when the config key (0x01) does not
// decode or has the old layout. It agrees with the engine's State.devnet and
// with DevnetRole on every case, and it writes nothing.
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
		view := State{ChainID: chain, config: &authorityv1.GenesisState{DevnetTestParameters: numericParams()}}
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

	// An undecodable config and an old-layout config (a proposal in the
	// config key) fail LoadState; both read surfaces then report not found.
	t.Run("config does not load", func(t *testing.T) {
		for name, edit := range map[string]func(storetypes.KVStore){
			"undecodable": func(kv storetypes.KVStore) { kv.Set([]byte{configKey}, []byte{0xff, 0xff, 0xff}) },
			"old layout": func(kv storetypes.KVStore) {
				mutateConfig(t, kv, func(cfg *authorityv1.GenesisState) {
					cfg.Authority.Proposals = []*authorityv1.Proposal{{ProposalId: "old"}}
				})
			},
		} {
			c, kk, kkey := numericKeeper(t, numericParams())
			edit(c.KVStore(kkey))
			if _, err := LoadState(c.KVStore(kkey), c.ChainID(), 1, 1); err == nil {
				t.Fatalf("%s: LoadState accepted the config", name)
			}
			if value, found := kk.DevnetNumeric(c, "DEC-005"); found || value != 0 {
				t.Fatalf("%s config: (%d, %v)", name, value, found)
			}
			if _, found := kk.DevnetRole(c, "DEC-002"); found {
				t.Fatalf("%s config: DevnetRole found", name)
			}
		}
	})

	// DEC-005 arrives from a genesis document the way a node reads it:
	// protojson through AppModule.InitGenesis, numeric_value as a JSON
	// string (protojson's uint64 form). The devnet harness writes the same
	// row (chain/devnet/init.go).
	t.Run("DEC-005 from a genesis document", func(t *testing.T) {
		gen, _, _ := readyGenesis()
		base, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&authorityv1.GenesisState{Authority: gen.GetAuthority()})
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
// engine loads (State.policy, key 0x04). It is 1 on an empty store and after
// InitGenesis (no 0x04 key yet), stays 1 while a proposal is only proposed
// and approved, becomes 2 after the first executed proposal and 3 after the
// second, and does not move on a rejected execution. A stored value reads as
// the engine reads it, and panics (fail closed) exactly where the engine's
// read is ERROR_CODE_INTEGRITY: a value that is not 8 bytes or is below 2.
// The read writes nothing.
func TestPolicyVersionRead(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if got := NewKeeper(keys[ModuleName]).PolicyVersion(empty); got != 1 {
			t.Fatalf("empty store: %d", got)
		}
	})

	t.Run("executed proposals", func(t *testing.T) {
		e := readyKeeper(t)
		store := e.kv()
		if store.Has([]byte{policyKey}) {
			t.Fatal("policy version key before any message")
		}
		requireVersion := func(want uint64, when string) {
			t.Helper()
			before := dumpKV(store)
			if got := e.keeper.PolicyVersion(e.blockCtx()); got != want {
				t.Fatalf("%s: PolicyVersion %d, want %d", when, got, want)
			}
			if loaded := e.policy(t); loaded != want {
				t.Fatalf("%s: the engine sees %d, want %d", when, loaded, want)
			}
			wantUnchanged(t, store, before, when+": PolicyVersion")
		}
		requireVersion(1, "after InitGenesis")

		e.queue(t, "pv-1", governedMsg(msgkit.LimitMaxPageLimit, 50))
		if store.Has([]byte{policyKey}) {
			t.Fatal("policy version key after propose and approve")
		}
		requireVersion(1, "queued, not executed")

		// A rejected execution (unknown proposal) changes nothing.
		before := dumpKV(store)
		if _, err := e.keeper.Execute(e.txCtx(), execute(e.authority, "ex-none", "pv-none")); err == nil {
			t.Fatal("executing an unknown proposal succeeded")
		}
		wantUnchanged(t, store, before, "rejected execution")
		requireVersion(1, "after a rejected execution")

		if _, err := e.keeper.Execute(e.txCtx(), execute(e.authority, "ex-pv-1", "pv-1")); err != nil {
			t.Fatal(err)
		}
		requireVersion(2, "after the first executed proposal")
		if raw := store.Get([]byte{policyKey}); !bytes.Equal(raw, encodeU64(2)) {
			t.Fatalf("stored policy version %x", raw)
		}

		e.queue(t, "pv-2", governedMsg(msgkit.LimitMaxPageLimit, 40))
		requireVersion(2, "second proposal queued")
		if _, err := e.keeper.Execute(e.txCtx(), execute(e.authority, "ex-pv-2", "pv-2")); err != nil {
			t.Fatal(err)
		}
		requireVersion(3, "after the second executed proposal")
	})

	t.Run("stored values", func(t *testing.T) {
		cases := []struct {
			name  string
			value []byte
			want  uint64
			panic bool
		}{
			{"2", encodeU64(2), 2, false},
			{"7", encodeU64(7), 7, false},
			{"largest", encodeU64(1<<64 - 1), 1<<64 - 1, false},
			{"0", encodeU64(0), 0, true},
			{"1 stored", encodeU64(1), 0, true},
			{"7 bytes", make([]byte, 7), 0, true},
			{"9 bytes", append(encodeU64(2), 0), 0, true},
			{"1 byte", []byte{2}, 0, true},
			{"text", []byte("2"), 0, true},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				e := readyKeeper(t)
				e.kv().Set([]byte{policyKey}, c.value)
				engine, out := loadAt(t, e.kv(), e.height, e.unix).policy()
				if engineFails := !out.Success(); engineFails != c.panic || (engineFails && out.Code != CodeIntegrity) {
					t.Fatalf("engine read: %d %v, want failure %v", engine, out, c.panic)
				}
				before := dumpKV(e.kv())
				got, panicked := policyVersionOrPanic(e.keeper, e.blockCtx())
				if panicked != c.panic {
					t.Fatalf("panicked %v, want %v", panicked, c.panic)
				}
				wantUnchanged(t, e.kv(), before, "PolicyVersion")
				if c.panic {
					return
				}
				if got != c.want || engine != c.want {
					t.Fatalf("PolicyVersion %d, engine %d, want %d", got, engine, c.want)
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
