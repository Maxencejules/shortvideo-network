package authority

import (
	"bytes"
	"encoding/json"
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

const registryStore = "registry"

// keeperEnv is a real IAVL multistore with the authority store and a
// second store the test hooks write to.
type keeperEnv struct {
	ctx       sdk.Context
	keeper    Keeper
	keys      map[string]*storetypes.KVStoreKey
	seats     [][]byte
	authority []byte
	service   []byte
	provider  []byte
}

func newKeeperEnv(t *testing.T) *keeperEnv {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, ModuleName, registryStore)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	st, seats, authoritySigner := readyState()
	provider := bytes20(4)
	genesis := &authorityv1.GenesisState{
		Authority: &authorityv1.AuthorityState{
			Validators: st.Validators,
			Submitters: append(st.Submitters, &typesv1.Submitter{
				Address: provider, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
			}),
			DevnetTestSeats: st.Seats,
		},
		DevnetTestParameters: append(st.Params,
			devnetParam("DEC-002", "DEVNET_TEST_ONLY"),
			devnetParam("DEC-003", "DEVNET_TEST_ONLY"),
			&typesv1.DevnetTestParameter{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-004", RoleName: "DEVNET_TEST_ONLY", ApprovedPolicy: true},
			&typesv1.DevnetTestParameter{Label: "APPROVED", DecisionId: "DEC-005", RoleName: "DEVNET_TEST_ONLY"},
		),
	}
	k := NewKeeper(keys[ModuleName])
	if err := k.Init(ctx, genesis); err != nil {
		t.Fatal(err)
	}
	return &keeperEnv{
		ctx: ctx, keeper: k, keys: keys, seats: seats,
		authority: authoritySigner, service: bytes20(2), provider: provider,
	}
}

// tx is one message context: a finite gas meter inside the authority
// bounds and a fresh event manager.
func (e *keeperEnv) tx() sdk.Context {
	return e.ctx.WithGasMeter(storetypes.NewGasMeter(5_000_000)).WithEventManager(sdk.NewEventManager())
}

func (e *keeperEnv) snapshot(name string) []byte {
	return msgkit.StoreSnapshot(e.ctx.MultiStore().GetKVStore(e.keys[name]))
}

func (e *keeperEnv) queue(t *testing.T, k Keeper, id string, msg *authorityv1.MsgProposeChange) {
	t.Helper()
	if _, err := k.Propose(e.tx(), propose(e.seats[0], id, e.policyVersion(t), msg)); err != nil {
		t.Fatalf("propose %s: %v", id, err)
	}
	for i := 0; i < ApprovedThreshold; i++ {
		if _, err := k.Approve(e.tx(), approve(e.seats[i], "ap-"+id+"-"+string(rune('a'+i)), id)); err != nil {
			t.Fatalf("approve %s: %v", id, err)
		}
	}
	if got := e.proposalState(t, id); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatalf("%s is %v", id, got)
	}
}

func (e *keeperEnv) policyVersion(t *testing.T) uint64 {
	t.Helper()
	st, err := e.keeper.load(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return st.PolicyVersion
}

func (e *keeperEnv) proposalState(t *testing.T, id string) authorityv1.ProposalState {
	t.Helper()
	state, err := e.keeper.Export(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range state.GetAuthority().GetProposals() {
		if row.GetProposalId() == id {
			return row.GetState()
		}
	}
	t.Fatalf("proposal %s missing", id)
	return 0
}

func (e *keeperEnv) side(t *testing.T) sideDocument {
	t.Helper()
	var doc sideDocument
	if err := json.Unmarshal(e.ctx.KVStore(e.keys[ModuleName]).Get([]byte{sideKey}), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
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
	e := newKeeperEnv(t)
	hooks := &storeHooks{key: e.keys[registryStore]}
	k := e.keeper.WithRegistry(hooks, hooks)
	e.queue(t, k, "admit-p1", admitProposal("p1"))

	ctx := e.tx()
	if _, err := k.Execute(ctx, execute(e.authority, "ex-admit", "admit-p1")); err != nil {
		t.Fatal(err)
	}
	if !sameCalls(hooks.calls, []registryCall{{Method: "admit", ID: "p1", Devnet: true}}) {
		t.Fatalf("calls %+v", hooks.calls)
	}
	if !e.ctx.KVStore(e.keys[registryStore]).Has([]byte("admit/p1")) {
		t.Fatal("the successful hook write was not committed")
	}
	if eventCount(ctx.EventManager().Events(), "fake_registry") != 1 {
		t.Fatal("the hook event was not emitted on success")
	}
	if got := e.proposalState(t, "admit-p1"); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(got)
	}

	// The same business id again is idempotent. The registry is not called.
	authorityBefore, registryBefore := e.snapshot(ModuleName), e.snapshot(registryStore)
	again := e.tx()
	if _, err := k.Execute(again, execute(e.authority, "ex-admit", "admit-p1")); err != nil {
		t.Fatal(err)
	}
	if value, _ := eventAttribute(again.EventManager().Events(), "authority_result", "idempotent"); value != "true" || len(hooks.calls) != 1 {
		t.Fatalf("idempotent %q calls %d", value, len(hooks.calls))
	}
	if !bytes.Equal(authorityBefore, e.snapshot(ModuleName)) || !bytes.Equal(registryBefore, e.snapshot(registryStore)) {
		t.Fatal("an idempotent execute wrote")
	}
}

func TestKeeperRegistryForbiddenKeepsTheRejection(t *testing.T) {
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		t.Run(kind, func(t *testing.T) {
			e := newKeeperEnv(t)
			hooks := &storeHooks{key: e.keys[registryStore], code: msgkit.Forbidden}
			k := e.keeper.WithRegistry(hooks, hooks)
			id, msg := registryProposal(kind)
			e.queue(t, k, id, msg)
			registryBefore := e.snapshot(registryStore)
			version := e.policyVersion(t)

			ctx := e.tx()
			if _, err := k.Execute(ctx, execute(e.authority, "ex-"+id, id)); err != nil {
				t.Fatalf("a keep result is handler success: %v", err)
			}
			if code, _ := eventAttribute(ctx.EventManager().Events(), "authority_result", "code"); code != CodeForbidden {
				t.Fatalf("result code %q", code)
			}
			if eventCount(ctx.EventManager().Events(), "fake_registry") != 0 {
				t.Fatal("a rejected hook's event was emitted")
			}
			if !bytes.Equal(registryBefore, e.snapshot(registryStore)) {
				t.Fatal("a rejected hook's write was committed")
			}
			if got := e.proposalState(t, id); got != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED {
				t.Fatal(got)
			}
			if e.policyVersion(t) != version {
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
				e := newKeeperEnv(t)
				hooks := &storeHooks{key: e.keys[registryStore], code: code}
				k := e.keeper.WithRegistry(hooks, hooks)
				id, msg := registryProposal(kind)
				e.queue(t, k, id, msg)
				authorityBefore, registryBefore := e.snapshot(ModuleName), e.snapshot(registryStore)

				ctx := e.tx()
				_, err := k.Execute(ctx, execute(e.authority, "ex-"+id, id))
				assertCode(t, err, string(code))
				if len(hooks.calls) != 1 {
					t.Fatalf("calls %+v", hooks.calls)
				}
				if !bytes.Equal(authorityBefore, e.snapshot(ModuleName)) {
					t.Fatal("a registry failure changed the authority store")
				}
				if !bytes.Equal(registryBefore, e.snapshot(registryStore)) {
					t.Fatal("a failed hook's write was committed")
				}
				if eventCount(ctx.EventManager().Events(), "fake_registry") != 0 {
					t.Fatal("a failed hook's event was emitted")
				}
				if got := e.proposalState(t, id); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestKeeperForceWithdrawArgumentsSurviveTheSideDocument(t *testing.T) {
	e := newKeeperEnv(t)
	hooks := &storeHooks{key: e.keys[registryStore]}
	k := e.keeper.WithRegistry(hooks, hooks)
	e.queue(t, k, "withdraw-c1", withdrawProposal("c1", 1, reasonValue))

	var stored *decodedWire
	for _, row := range e.side(t).Decoded {
		if row.ID == "withdraw-c1" {
			stored = &row
		}
	}
	if stored == nil || stored.Kind != "force_withdraw" || stored.ContentID != "c1" || stored.ContentVersion != 1 || !bytes.Equal(stored.Reason, reasonValue) {
		t.Fatalf("side document row %+v", stored)
	}

	if _, err := k.Execute(e.tx(), execute(e.authority, "ex-withdraw", "withdraw-c1")); err != nil {
		t.Fatal(err)
	}
	want := []registryCall{{Method: "force_withdraw", ID: "c1", Version: 1, Reason: reasonValue, Devnet: true}}
	if !sameCalls(hooks.calls, want) {
		t.Fatalf("calls %+v want %+v", hooks.calls, want)
	}
	if !e.ctx.KVStore(e.keys[registryStore]).Has([]byte("withdraw/c1")) {
		t.Fatal("force-withdraw write missing")
	}
}

func TestKeeperWithoutRegistryIsMalformed(t *testing.T) {
	e := newKeeperEnv(t)
	e.queue(t, e.keeper, "admit-p1", admitProposal("p1"))
	before := e.snapshot(ModuleName)
	_, err := e.keeper.Execute(e.tx(), execute(e.authority, "ex-admit", "admit-p1"))
	assertCode(t, err, CodeMalformed)
	if !bytes.Equal(before, e.snapshot(ModuleName)) {
		t.Fatal("a missing registry changed the authority store")
	}
	if got := e.proposalState(t, "admit-p1"); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal(got)
	}
}

func TestWithRegistryRejectsNilHooks(t *testing.T) {
	e := newKeeperEnv(t)
	hooks := &storeHooks{key: e.keys[registryStore]}
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
	e := newKeeperEnv(t)
	foreign := bytes.Repeat([]byte{0x5a}, 32)
	peers := msgkit.NewPeers()
	k := e.keeper.WithPeers(peers)
	peers.Add(ModuleName, k)
	peers.Add("providers", fakeSource{"taken": foreign})

	before := e.snapshot(ModuleName)
	taken := propose(e.seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))
	taken.BusinessId = "taken"
	_, err := k.Propose(e.tx(), taken)
	assertCode(t, err, CodeReplay)
	if !bytes.Equal(before, e.snapshot(ModuleName)) {
		t.Fatal("a replayed propose changed the authority store")
	}

	e.queue(t, k, "p-add", powerMsg("v5", pub(9), 1, 20))
	before = e.snapshot(ModuleName)
	_, err = k.Approve(e.tx(), approve(e.seats[3], "taken", "p-add"))
	assertCode(t, err, CodeReplay)
	_, err = k.Execute(e.tx(), execute(e.authority, "taken", "p-add"))
	assertCode(t, err, CodeReplay)
	if !bytes.Equal(before, e.snapshot(ModuleName)) || e.proposalState(t, "p-add") != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal("a replayed approve or execute changed the authority store")
	}

	// The other direction: providers and content see authority's ids.
	own, found := k.BusinessDigest(e.ctx, "b-p-add")
	if !found || len(own) != 32 {
		t.Fatalf("authority business id %x %v", own, found)
	}
	seen, found := peers.Lookup(e.ctx, "b-p-add", "providers")
	if !found || !bytes.Equal(seen, own) {
		t.Fatal("a peer module does not see authority's business id")
	}
	if _, found := peers.Lookup(e.ctx, "b-p-add", ModuleName); found {
		t.Fatal("authority's lookup consulted its own ledger through peers")
	}

	// Without peers, authority alone does not know the providers id.
	plain := propose(e.seats[0], "p-other", 1, powerMsg("v6", pub(10), 1, 20))
	plain.BusinessId = "taken"
	plain.BasePolicyVersion = e.policyVersion(t)
	if _, err := e.keeper.Propose(e.tx(), plain); err != nil {
		t.Fatalf("without peers: %v", err)
	}
}

func TestKeeperReadSurface(t *testing.T) {
	e := newKeeperEnv(t)

	t.Run("role", func(t *testing.T) {
		cases := []struct {
			name string
			addr []byte
			want typesv1.SubmitterRole
		}{
			{"authority", e.authority, typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY},
			{"service", e.service, typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
			{"provider", e.provider, typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER},
			{"seat is not a submitter", e.seats[0], typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"unknown", bytes20(77), typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"short", e.authority[:19], typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"long", append(append([]byte(nil), e.authority...), 0), typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
			{"nil", nil, typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED},
		}
		for _, tc := range cases {
			if got := e.keeper.Role(e.ctx, tc.addr); got != tc.want {
				t.Fatalf("%s: %v", tc.name, got)
			}
		}
		odd := bytes20(78)
		store := e.ctx.KVStore(e.keys[ModuleName])
		store.Set(admitKeyBytes(odd), []byte{99})
		if got := e.keeper.Role(e.ctx, odd); got != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			t.Fatalf("value outside the enum: %v", got)
		}
		store.Set(admitKeyBytes(odd), []byte{1, 1})
		if got := e.keeper.Role(e.ctx, odd); got != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			t.Fatalf("two-byte value: %v", got)
		}
		store.Delete(admitKeyBytes(odd))
	})

	t.Run("devnet role", func(t *testing.T) {
		if role, found := e.keeper.DevnetRole(e.ctx, "DEC-002"); !found || role != "DEVNET_TEST_ONLY" {
			t.Fatalf("DEC-002 %q %v", role, found)
		}
		if role, found := e.keeper.DevnetRole(e.ctx, "DEC-003"); !found || role != "DEVNET_TEST_ONLY" {
			t.Fatalf("DEC-003 %q %v", role, found)
		}
		for _, id := range []string{"DEC-004", "DEC-005", "DEC-001", ""} {
			if role, found := e.keeper.DevnetRole(e.ctx, id); found {
				t.Fatalf("%s %q", id, role)
			}
		}
		beta := e.ctx.WithChainID("shortvideo-beta-1")
		if role, found := e.keeper.DevnetRole(beta, "DEC-002"); found {
			t.Fatalf("non-devnet chain id %q", role)
		}
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if _, found := NewKeeper(keys[ModuleName]).DevnetRole(empty, "DEC-002"); found {
			t.Fatal("an empty store has a devnet role")
		}
	})

	t.Run("governed limit", func(t *testing.T) {
		if value, found := e.keeper.GovernedLimit(e.ctx, msgkit.LimitMaxPageLimit); found {
			t.Fatalf("before %d", value)
		}
		limit := payloadProposal(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
			GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: msgkit.LimitMaxPageLimit, Value: 50},
		}}, msgkit.LimitMaxPageLimit)
		e.queue(t, e.keeper, "lim-1", limit)
		if _, err := e.keeper.Execute(e.tx(), execute(e.authority, "ex-lim-1", "lim-1")); err != nil {
			t.Fatal(err)
		}
		if value, found := e.keeper.GovernedLimit(e.ctx, msgkit.LimitMaxPageLimit); !found || value != 50 {
			t.Fatalf("after %d %v", value, found)
		}
		if _, found := e.keeper.GovernedLimit(e.ctx, msgkit.LimitMaxPackageBytes); found {
			t.Fatal("an unset limit was found")
		}
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if _, found := NewKeeper(keys[ModuleName]).GovernedLimit(empty, msgkit.LimitMaxPageLimit); found {
			t.Fatal("an empty store has a governed limit")
		}
	})

	t.Run("business digest", func(t *testing.T) {
		e.queue(t, e.keeper, "p-bd", powerMsg("v7", pub(11), 1, 20))
		msg := propose(e.seats[0], "p-bd", 0, powerMsg("v7", pub(11), 1, 20))
		digest, found := e.keeper.BusinessDigest(e.ctx, msg.GetBusinessId())
		if !found {
			t.Fatal("committed business id not found")
		}
		row := e.side(t)
		var stored []byte
		for _, item := range row.Business {
			if item.ID == msg.GetBusinessId() {
				stored = item.Value
			}
		}
		if !bytes.Equal(digest, stored) || len(digest) != 32 {
			t.Fatalf("digest %x stored %x", digest, stored)
		}
		digest[0] ^= 0xff
		if again, _ := e.keeper.BusinessDigest(e.ctx, msg.GetBusinessId()); !bytes.Equal(again, stored) {
			t.Fatal("BusinessDigest returned the stored slice")
		}
		for _, id := range []string{"unknown", "", "Upper", string(make([]byte, 65))} {
			if _, found := e.keeper.BusinessDigest(e.ctx, id); found {
				t.Fatalf("%q found", id)
			}
		}
		empty, keys := msgkittest.NewContext(t, ModuleName)
		if _, found := NewKeeper(keys[ModuleName]).BusinessDigest(empty, "b-p-bd"); found {
			t.Fatal("an empty store has a business id")
		}
	})

	t.Run("corrupt side document", func(t *testing.T) {
		ctx, keys := msgkittest.NewContext(t, ModuleName)
		k := NewKeeper(keys[ModuleName])
		ctx.KVStore(keys[ModuleName]).Set([]byte{sideKey}, []byte("{"))
		if _, found := k.GovernedLimit(ctx, msgkit.LimitMaxPageLimit); found {
			t.Fatal("a corrupt document has a limit")
		}
		defer func() {
			if recover() == nil {
				t.Fatal("BusinessDigest reported an id free on a corrupt document")
			}
		}()
		k.BusinessDigest(ctx, "b-1")
	})
}

func TestKeeperLoadsAnOlderSideDocument(t *testing.T) {
	// A goal-06 document carries provider rows. They decode as unknown keys
	// and the next save drops them; other rows survive.
	e := newKeeperEnv(t)
	older := map[string]any{
		"policy_version": 1,
		"business":       []map[string]any{{"id": "old-1", "value": bytes.Repeat([]byte{7}, 32)}},
		"providers": []map[string]any{{
			"id": "p1", "account": bytes20(4), "signing_pub": pub(4), "signing_key_id": "k1",
			"generation": 1, "state": "APPLIED", "operator_group": "devnet-test-only", "endpoint": pub(8), "capacity": 1000,
		}},
		"prev_provider": []map[string]any{{"id": "p1", "value": [][]byte{pub(3)}}},
	}
	raw, err := json.Marshal(older)
	if err != nil {
		t.Fatal(err)
	}
	e.ctx.KVStore(e.keys[ModuleName]).Set([]byte{sideKey}, raw)
	if digest, found := e.keeper.BusinessDigest(e.ctx, "old-1"); !found || !bytes.Equal(digest, bytes.Repeat([]byte{7}, 32)) {
		t.Fatal("older business row unreadable")
	}
	if _, err := e.keeper.Propose(e.tx(), propose(e.seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))); err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(e.ctx.KVStore(e.keys[ModuleName]).Get([]byte{sideKey}), &keys); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"providers", "prev_provider"} {
		if _, present := keys[gone]; present {
			t.Fatalf("%s survived a save", gone)
		}
	}
	if _, found := e.keeper.BusinessDigest(e.ctx, "old-1"); !found {
		t.Fatal("older business row lost on save")
	}
	if _, found := e.keeper.BusinessDigest(e.ctx, "b-p-add"); !found {
		t.Fatal("new business row missing")
	}
}

func TestAuthoritySourcesAreDeterministic(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}
