package authority

import (
	"bytes"
	"math"
	"testing"

	errorsmod "cosmossdk.io/errors"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// registryKeeper is readyKeeper with a provider submitter (bytes20(4),
// ADMITTED_PROVIDER), DEC-002 and DEC-003 as devnet test roles, and two rows
// that are not: DEC-004 with approved_policy true and DEC-005 with another
// label.
func registryKeeper(t testing.TB) (*keeperEnv, []byte) {
	t.Helper()
	gen, seats, authority := readyGenesis()
	provider := bytes20(4)
	gen.Authority.Submitters = append(gen.Authority.Submitters, &typesv1.Submitter{
		Address: provider, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
	})
	gen.DevnetTestParameters = append(gen.DevnetTestParameters,
		devnetParam("DEC-002", "DEVNET_TEST_ONLY"),
		devnetParam("DEC-003", "DEVNET_TEST_ONLY"),
		&typesv1.DevnetTestParameter{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-004", RoleName: "DEVNET_TEST_ONLY", ApprovedPolicy: true},
		&typesv1.DevnetTestParameter{Label: "APPROVED", DecisionId: "DEC-005", RoleName: "DEVNET_TEST_ONLY"},
	)
	return newKeeperEnv(t, gen, seats, authority), provider
}

// registryKV is the store the test hooks write to, without gas.
func (e *keeperEnv) registryKV() storetypes.KVStore {
	return e.cms.GetKVStore(e.registryKey)
}

// queueWith proposes id from seats[0] and approves it with seats 0..2
// through the handlers of k (a keeper with hooks or peers).
func (e *keeperEnv) queueWith(t testing.TB, k Keeper, id string, msg *authorityv1.MsgProposeChange) {
	t.Helper()
	if _, err := k.Propose(e.txCtx(), propose(e.seats[0].Signer, id, e.policy(t), msg)); err != nil {
		t.Fatalf("propose %s: %v", id, err)
	}
	for i := 0; i < ApprovedThreshold; i++ {
		if _, err := k.Approve(e.txCtx(), approve(e.seats[i].Signer, "ap-"+id+"-"+string(rune('a'+i)), id)); err != nil {
			t.Fatalf("approve %s: %v", id, err)
		}
	}
	if got := recordOf(t, e.kv(), id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatalf("%s is %v", id, got)
	}
}

func assertCode(t *testing.T, err error, name string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want %s, got success", name)
	}
	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	if codespace != svnerr.ModuleName || code != uint32(typesv1.ErrorCode_value[name]) {
		t.Fatalf("want %s, got %s/%d (%v)", name, codespace, code, err)
	}
}

func eventCount(events sdk.Events, kind string) int {
	n := 0
	for _, ev := range events {
		if ev.Type == kind {
			n++
		}
	}
	return n
}

func eventAttribute(events sdk.Events, kind, key string) (string, bool) {
	for _, ev := range events {
		if ev.Type != kind {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == key {
				return attr.Value, true
			}
		}
	}
	return "", false
}

// storeHooks is a unit-test double for the x/providers and x/content
// hooks. It writes to its own store and emits an event before it returns
// its configured code, the opposite of what a real registry does on a
// failure, so a test can show authority discards a failed hook's writes.
type storeHooks struct {
	key   *storetypes.KVStoreKey
	code  msgkit.Code
	calls []registryCall
}

func (h *storeHooks) touch(ctx sdk.Context, record string) msgkit.Code {
	ctx.KVStore(h.key).Set([]byte(record), []byte{1})
	ctx.EventManager().EmitEvent(sdk.NewEvent("fake_registry", sdk.NewAttribute("record", record)))
	return h.code
}

func (h *storeHooks) AdmitByCouncil(ctx sdk.Context, id string, devnet bool) msgkit.Code {
	h.calls = append(h.calls, registryCall{Method: "admit", ID: id, Devnet: devnet})
	return h.touch(ctx, "admit/"+id)
}

func (h *storeHooks) DisableByCouncil(ctx sdk.Context, id string) msgkit.Code {
	h.calls = append(h.calls, registryCall{Method: "disable", ID: id})
	return h.touch(ctx, "disable/"+id)
}

func (h *storeHooks) ForceWithdrawByCouncil(ctx sdk.Context, id string, version uint64, reason []byte, devnet bool) msgkit.Code {
	h.calls = append(h.calls, registryCall{
		Method: "force_withdraw", ID: id, Version: version, Reason: append([]byte(nil), reason...), Devnet: devnet,
	})
	return h.touch(ctx, "withdraw/"+id)
}

// fakeSource is another module's business ledger.
type fakeSource map[string][]byte

func (f fakeSource) BusinessDigest(_ sdk.Context, id string) ([]byte, bool) {
	value, found := f[id]
	return value, found
}

func TestKeeperRegistryHookCommitsOnlyOnSuccess(t *testing.T) {
	e, _ := registryKeeper(t)
	hooks := &storeHooks{key: e.registryKey}
	k := e.keeper.WithRegistry(hooks, hooks)
	e.queueWith(t, k, "admit-p1", admitProposal("p1"))

	ctx := e.txCtx()
	if _, err := k.Execute(ctx, execute(e.authority, "ex-admit", "admit-p1")); err != nil {
		t.Fatal(err)
	}
	if !sameCalls(hooks.calls, []registryCall{{Method: "admit", ID: "p1", Devnet: true}}) {
		t.Fatalf("calls %+v", hooks.calls)
	}
	if !e.registryKV().Has([]byte("admit/p1")) {
		t.Fatal("the successful hook write was not committed")
	}
	if eventCount(ctx.EventManager().Events(), "fake_registry") != 1 {
		t.Fatal("the hook event was not emitted on success")
	}
	if got := recordOf(t, e.kv(), "admit-p1").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(got)
	}

	// The same business id again is idempotent. The registry is not called.
	authorityBefore, registryBefore := dumpKV(e.kv()), dumpKV(e.registryKV())
	again := e.txCtx()
	if _, err := k.Execute(again, execute(e.authority, "ex-admit", "admit-p1")); err != nil {
		t.Fatal(err)
	}
	if value, _ := eventAttribute(again.EventManager().Events(), "authority_result", "idempotent"); value != "true" || len(hooks.calls) != 1 {
		t.Fatalf("idempotent %q calls %d", value, len(hooks.calls))
	}
	if !bytes.Equal(authorityBefore, dumpKV(e.kv())) || !bytes.Equal(registryBefore, dumpKV(e.registryKV())) {
		t.Fatal("an idempotent execute wrote")
	}
}

func TestKeeperRegistryForbiddenKeepsTheRejection(t *testing.T) {
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		t.Run(kind, func(t *testing.T) {
			e, _ := registryKeeper(t)
			hooks := &storeHooks{key: e.registryKey, code: msgkit.Forbidden}
			k := e.keeper.WithRegistry(hooks, hooks)
			id, msg := registryProposal(kind)
			e.queueWith(t, k, id, msg)
			registryBefore := dumpKV(e.registryKV())
			version := e.policy(t)

			ctx := e.txCtx()
			if _, err := k.Execute(ctx, execute(e.authority, "ex-"+id, id)); err != nil {
				t.Fatalf("a keep result is handler success: %v", err)
			}
			if code, _ := eventAttribute(ctx.EventManager().Events(), "authority_result", "code"); code != CodeForbidden {
				t.Fatalf("result code %q", code)
			}
			if eventCount(ctx.EventManager().Events(), "fake_registry") != 0 {
				t.Fatal("a rejected hook's event was emitted")
			}
			if !bytes.Equal(registryBefore, dumpKV(e.registryKV())) {
				t.Fatal("a rejected hook's write was committed")
			}
			if got := recordOf(t, e.kv(), id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED {
				t.Fatal(got)
			}
			if e.policy(t) != version {
				t.Fatal("a rejected proposal moved the policy version")
			}
		})
	}
}

func TestKeeperRegistryFailureWritesNothing(t *testing.T) {
	codes := []msgkit.Code{msgkit.NotFound, msgkit.DecisionBlocked, msgkit.Conflict, msgkit.Withdrawn, msgkit.StaleVersion, msgkit.WrongDomain}
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		for _, code := range codes {
			t.Run(kind+"/"+string(code), func(t *testing.T) {
				e, _ := registryKeeper(t)
				hooks := &storeHooks{key: e.registryKey, code: code}
				k := e.keeper.WithRegistry(hooks, hooks)
				id, msg := registryProposal(kind)
				e.queueWith(t, k, id, msg)
				authorityBefore, registryBefore := dumpKV(e.kv()), dumpKV(e.registryKV())

				ctx := e.txCtx()
				_, err := k.Execute(ctx, execute(e.authority, "ex-"+id, id))
				assertCode(t, err, string(code))
				if code == msgkit.WrongDomain {
					// A code authority never produces itself keeps its svn
					// number through sdkError (msgkit), not MALFORMED.
					if _, abci, _ := errorsmod.ABCIInfo(err, false); abci != 12 {
						t.Fatalf("WRONG_DOMAIN surfaced as svn %d", abci)
					}
				}
				if len(hooks.calls) != 1 {
					t.Fatalf("calls %+v", hooks.calls)
				}
				if !bytes.Equal(authorityBefore, dumpKV(e.kv())) {
					t.Fatal("a registry failure changed the authority store")
				}
				if !bytes.Equal(registryBefore, dumpKV(e.registryKV())) {
					t.Fatal("a failed hook's write was committed")
				}
				if eventCount(ctx.EventManager().Events(), "fake_registry") != 0 {
					t.Fatal("a failed hook's event was emitted")
				}
				if got := recordOf(t, e.kv(), id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
					t.Fatal(got)
				}
			})
		}
	}
}

// A hook that wrote and answered OK must not keep its write when a later
// engine check fails. With the policy version at 2^64-1, ExecuteChange calls
// the hook and then returns OVERFLOW; the keeper drops the registry branch
// with its events. Fails if the hooks are bound to the transaction context
// instead of a branch. Binding them to the message branch instead of the
// nested one is caught by TestKeeperRegistryForbiddenKeepsTheRejection (a
// keep writes the message branch).
func TestKeeperRegistryWriteDroppedOnLateFailure(t *testing.T) {
	e, _ := registryKeeper(t)
	e.kv().Set([]byte{policyKey}, encodeU64(math.MaxUint64))
	hooks := &storeHooks{key: e.registryKey}
	k := e.keeper.WithRegistry(hooks, hooks)
	e.queueWith(t, k, "admit-p1", admitProposal("p1"))
	if got := recordOf(t, e.kv(), "admit-p1").BasePolicyVersion; got != math.MaxUint64 {
		t.Fatalf("base policy version %d", got)
	}
	authorityBefore, registryBefore := dumpKV(e.kv()), dumpKV(e.registryKV())

	ctx := e.txCtx()
	_, err := k.Execute(ctx, execute(e.authority, "ex-admit", "admit-p1"))
	assertCode(t, err, CodeOverflow)
	if !sameCalls(hooks.calls, []registryCall{{Method: "admit", ID: "p1", Devnet: true}}) {
		t.Fatalf("the hook was not reached before the late check: %+v", hooks.calls)
	}
	if !bytes.Equal(registryBefore, dumpKV(e.registryKV())) {
		t.Fatal("a hook write outlived a late OVERFLOW")
	}
	if eventCount(ctx.EventManager().Events(), "fake_registry") != 0 {
		t.Fatal("a hook event outlived a late OVERFLOW")
	}
	if !bytes.Equal(authorityBefore, dumpKV(e.kv())) {
		t.Fatal("a late OVERFLOW changed the authority store")
	}
	if got := recordOf(t, e.kv(), "admit-p1").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal(got)
	}
}

func TestKeeperForceWithdrawArgumentsSurviveTheRecord(t *testing.T) {
	e, _ := registryKeeper(t)
	hooks := &storeHooks{key: e.registryKey}
	k := e.keeper.WithRegistry(hooks, hooks)
	e.queueWith(t, k, "withdraw-c1", withdrawProposal("c1", 1, reasonValue))

	stored := recordOf(t, e.kv(), "withdraw-c1").Decoded
	if stored.Kind != kindForceWithdraw || stored.ContentID != "c1" || stored.ContentVersion != 1 || !bytes.Equal(stored.Reason, reasonValue) {
		t.Fatalf("proposal record payload %+v", stored)
	}

	if _, err := k.Execute(e.txCtx(), execute(e.authority, "ex-withdraw", "withdraw-c1")); err != nil {
		t.Fatal(err)
	}
	want := []registryCall{{Method: "force_withdraw", ID: "c1", Version: 1, Reason: reasonValue, Devnet: true}}
	if !sameCalls(hooks.calls, want) {
		t.Fatalf("calls %+v want %+v", hooks.calls, want)
	}
	if !e.registryKV().Has([]byte("withdraw/c1")) {
		t.Fatal("force-withdraw write missing")
	}
}

func TestKeeperWithoutRegistryIsMalformed(t *testing.T) {
	e, _ := registryKeeper(t)
	e.queueWith(t, e.keeper, "admit-p1", admitProposal("p1"))
	before := dumpKV(e.kv())
	_, err := e.keeper.Execute(e.txCtx(), execute(e.authority, "ex-admit", "admit-p1"))
	assertCode(t, err, CodeMalformed)
	if !bytes.Equal(before, dumpKV(e.kv())) {
		t.Fatal("a missing registry changed the authority store")
	}
	if got := recordOf(t, e.kv(), "admit-p1").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal(got)
	}
}

func TestWithRegistryRejectsNilHooks(t *testing.T) {
	e, _ := registryKeeper(t)
	hooks := &storeHooks{key: e.registryKey}
	for name, call := range map[string]func(){
		"nil provider": func() { e.keeper.WithRegistry(nil, hooks) },
		"nil content":  func() { e.keeper.WithRegistry(hooks, nil) },
		"both nil":     func() { e.keeper.WithRegistry(nil, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			call()
		})
	}
}

func TestKeeperBusinessIDsAreOneNamespace(t *testing.T) {
	e, _ := registryKeeper(t)
	foreign := bytes.Repeat([]byte{0x5a}, 32)
	peers := msgkit.NewPeers()
	k := e.keeper.WithPeers(peers)
	peers.Add(ModuleName, k)
	peers.Add("providers", fakeSource{"taken": foreign})

	before := dumpKV(e.kv())
	taken := propose(e.seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))
	taken.BusinessId = "taken"
	_, err := k.Propose(e.txCtx(), taken)
	assertCode(t, err, CodeReplay)
	if !bytes.Equal(before, dumpKV(e.kv())) {
		t.Fatal("a replayed propose changed the authority store")
	}

	e.queueWith(t, k, "p-add", powerMsg("v5", pub(9), 1, 20))
	before = dumpKV(e.kv())
	_, err = k.Approve(e.txCtx(), approve(e.seats[3].Signer, "taken", "p-add"))
	assertCode(t, err, CodeReplay)
	_, err = k.Execute(e.txCtx(), execute(e.authority, "taken", "p-add"))
	assertCode(t, err, CodeReplay)
	if !bytes.Equal(before, dumpKV(e.kv())) || recordOf(t, e.kv(), "p-add").state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal("a replayed approve or execute changed the authority store")
	}

	// The other direction: providers and content see authority's ids.
	ctx := e.blockCtx()
	own, found := k.BusinessDigest(ctx, "b-p-add")
	if !found || len(own) != 32 {
		t.Fatalf("authority business id %x %v", own, found)
	}
	seen, found := peers.Lookup(ctx, "b-p-add", "providers")
	if !found || !bytes.Equal(seen, own) {
		t.Fatal("a peer module does not see authority's business id")
	}
	if _, found := peers.Lookup(ctx, "b-p-add", ModuleName); found {
		t.Fatal("authority's lookup consulted its own ledger through peers")
	}

	// Without peers, authority alone does not know the providers id.
	plain := propose(e.seats[0].Signer, "p-other", e.policy(t), powerMsg("v6", pub(10), 1, 20))
	plain.BusinessId = "taken"
	if _, err := e.keeper.Propose(e.txCtx(), plain); err != nil {
		t.Fatalf("without peers: %v", err)
	}
}

// TestKeeperMalformedPeerBusinessDigestIsIntegrity: a peer ledger value that
// is not 32 bytes is INTEGRITY through the keeper, the same rule as
// authority's own 0x11 value; a found id with a nil value is not free.
// Propose, approve and execute are refused, the authority store is
// byte-identical and the proposal stays QUEUED.
func TestKeeperMalformedPeerBusinessDigestIsIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value []byte
	}{
		{"31 bytes", bytes.Repeat([]byte{0x5a}, 31)},
		{"33 bytes", bytes.Repeat([]byte{0x5a}, 33)},
		{"empty", []byte{}},
		{"nil", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := registryKeeper(t)
			peers := msgkit.NewPeers()
			k := e.keeper.WithPeers(peers)
			peers.Add(ModuleName, k)
			peers.Add("providers", fakeSource{"damaged": tc.value})

			before := dumpKV(e.kv())
			msg := propose(e.seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))
			msg.BusinessId = "damaged"
			_, err := k.Propose(e.txCtx(), msg)
			assertCode(t, err, CodeIntegrity)
			if !bytes.Equal(before, dumpKV(e.kv())) {
				t.Fatal("a propose with a damaged peer digest changed the authority store")
			}

			e.queueWith(t, k, "p-add", powerMsg("v5", pub(9), 1, 20))
			before = dumpKV(e.kv())
			_, err = k.Approve(e.txCtx(), approve(e.seats[3].Signer, "damaged", "p-add"))
			assertCode(t, err, CodeIntegrity)
			_, err = k.Execute(e.txCtx(), execute(e.authority, "damaged", "p-add"))
			assertCode(t, err, CodeIntegrity)
			if !bytes.Equal(before, dumpKV(e.kv())) || recordOf(t, e.kv(), "p-add").state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				t.Fatal("an approve or execute with a damaged peer digest changed the authority store")
			}
		})
	}
}

// The read surface reads one key each. A malformed value fails closed:
// Role and DevnetRole report nothing (callers then refuse), GovernedLimit
// and BusinessDigest panic rather than report a ceiling or a free id.
func TestKeeperReadSurface(t *testing.T) {
	e, provider := registryKeeper(t)
	ctx := e.blockCtx()
	store := e.kv()

	t.Run("role", func(t *testing.T) {
		cases := []struct {
			name string
			addr []byte
			want typesv1.SubmitterRole
		}{
			{"authority", e.authority, typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY},
			{"service", bytes20(2), typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
			{"provider", provider, typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER},
			{"seat is not a submitter", e.seats[0].Signer, typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"unknown", bytes20(77), typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"short", e.authority[:19], typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"long", append(append([]byte(nil), e.authority...), 0), typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"nil", nil, typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
		}
		for _, tc := range cases {
			if got := e.keeper.Role(ctx, tc.addr); got != tc.want {
				t.Fatalf("%s: %v", tc.name, got)
			}
		}
		odd := bytes20(78)
		store.Set(AdmissionKey(odd), []byte{99})
		if got := e.keeper.Role(ctx, odd); got != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			t.Fatalf("value outside the enum: %v", got)
		}
		store.Set(AdmissionKey(odd), []byte{1, 1})
		if got := e.keeper.Role(ctx, odd); got != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			t.Fatalf("two-byte value: %v", got)
		}
		store.Delete(AdmissionKey(odd))
	})

	t.Run("devnet role", func(t *testing.T) {
		if role, found := e.keeper.DevnetRole(ctx, "DEC-002"); !found || role != "DEVNET_TEST_ONLY" {
			t.Fatalf("DEC-002 %q %v", role, found)
		}
		if role, found := e.keeper.DevnetRole(ctx, "DEC-003"); !found || role != "DEVNET_TEST_ONLY" {
			t.Fatalf("DEC-003 %q %v", role, found)
		}
		for _, id := range []string{"DEC-004", "DEC-005", "DEC-001", ""} {
			if role, found := e.keeper.DevnetRole(ctx, id); found {
				t.Fatalf("%s %q", id, role)
			}
		}
		if role, found := e.keeper.DevnetRole(ctx.WithChainID("shortvideo-beta-1"), "DEC-002"); found {
			t.Fatalf("non-devnet chain id %q", role)
		}
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if _, found := NewKeeper(keys[ModuleName]).DevnetRole(empty, "DEC-002"); found {
			t.Fatal("an empty store has a devnet role")
		}
		// An old-layout config (a proposal in the config key) and an
		// undecodable one are not found: callers return DECISION_BLOCKED.
		raw := store.Get([]byte{configKey})
		defer store.Set([]byte{configKey}, raw)
		cfg, err := readConfig(store)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Authority.Proposals = []*authorityv1.Proposal{{ProposalId: "old"}}
		old, err := marshalState(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string][]byte{"old layout": old, "undecodable": {0xff}} {
			store.Set([]byte{configKey}, value)
			if role, found := e.keeper.DevnetRole(ctx, "DEC-002"); found {
				t.Fatalf("%s config: %q", name, role)
			}
		}
	})

	t.Run("governed limit", func(t *testing.T) {
		if value, found := e.keeper.GovernedLimit(ctx, msgkit.LimitMaxPageLimit); found {
			t.Fatalf("before %d", value)
		}
		e.queue(t, "lim-1", governedMsg(msgkit.LimitMaxPageLimit, 50))
		if _, err := e.keeper.Execute(e.txCtx(), execute(e.authority, "ex-lim-1", "lim-1")); err != nil {
			t.Fatal(err)
		}
		if value, found := e.keeper.GovernedLimit(e.blockCtx(), msgkit.LimitMaxPageLimit); !found || value != 50 {
			t.Fatalf("after %d %v", value, found)
		}
		if _, found := e.keeper.GovernedLimit(e.blockCtx(), msgkit.LimitMaxPackageBytes); found {
			t.Fatal("an unset limit was found")
		}
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if _, found := NewKeeper(keys[ModuleName]).GovernedLimit(empty, msgkit.LimitMaxPageLimit); found {
			t.Fatal("an empty store has a governed limit")
		}
		ceiling, _ := governedCeiling(msgkit.LimitMaxPageLimit)
		for name, value := range map[string][]byte{
			"7 bytes":         make([]byte, 7),
			"zero":            encodeU64(0),
			"above ceiling":   encodeU64(ceiling + 1),
			"9 bytes":         make([]byte, 9),
			"unknown key set": nil,
		} {
			key := governedKey(msgkit.LimitMaxPageLimit)
			if value == nil {
				key = governedKey("max_unknown_limit")
				value = encodeU64(1)
			}
			prev := store.Get(key)
			store.Set(key, value)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatalf("%s: GovernedLimit reported a value or the ceiling on damaged state", name)
					}
				}()
				e.keeper.GovernedLimit(e.blockCtx(), string(key[1:]))
			}()
			if prev == nil {
				store.Delete(key)
			} else {
				store.Set(key, prev)
			}
		}
	})

	t.Run("business digest", func(t *testing.T) {
		e.queue(t, "p-bd", powerMsg("v7", pub(11), 1, 20))
		id := "b-p-bd"
		digest, found := e.keeper.BusinessDigest(e.blockCtx(), id)
		stored := store.Get(businessKey(id))
		if !found || len(digest) != 32 || !bytes.Equal(digest, stored) {
			t.Fatalf("digest %x stored %x", digest, stored)
		}
		digest[0] ^= 0xff
		if again, _ := e.keeper.BusinessDigest(e.blockCtx(), id); !bytes.Equal(again, stored) {
			t.Fatal("BusinessDigest returned the stored slice")
		}
		for _, id := range []string{"unknown", "", "Upper", string(make([]byte, 65))} {
			if _, found := e.keeper.BusinessDigest(e.blockCtx(), id); found {
				t.Fatalf("%q found", id)
			}
		}
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if _, found := NewKeeper(keys[ModuleName]).BusinessDigest(empty, id); found {
			t.Fatal("an empty store has a business id")
		}
		// Fails closed: a damaged value never reports the id as free.
		store.Set(businessKey("damaged"), bytes.Repeat([]byte{1}, 31))
		defer store.Delete(businessKey("damaged"))
		defer func() {
			if recover() == nil {
				t.Fatal("BusinessDigest reported a 31-byte value")
			}
		}()
		e.keeper.BusinessDigest(e.blockCtx(), "damaged")
	})
}

func TestAuthoritySourcesAreDeterministic(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}
