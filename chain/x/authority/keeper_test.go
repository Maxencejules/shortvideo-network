package authority

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"

	store "github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// keeperEnv is a keeper on a real rootmulti store (IAVL over an in-memory
// cosmos-db). Contexts read and write through the SDK gas store. registryKey
// is a second IAVL store that the registry test hooks write to; it stays
// empty unless a test uses it (keeper_registry_test.go).
type keeperEnv struct {
	cms         storetypes.CommitMultiStore
	key         *storetypes.KVStoreKey
	registryKey *storetypes.KVStoreKey
	keeper      Keeper
	seats       []testSeat
	authority   []byte
	height      int64
	unix        int64
}

func newKeeperEnv(t testing.TB, gen *authorityv1.GenesisState, seats []testSeat, authority []byte) *keeperEnv {
	t.Helper()
	cms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger())
	key := storetypes.NewKVStoreKey(ModuleName)
	registryKey := storetypes.NewKVStoreKey("registry")
	cms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
	cms.MountStoreWithDB(registryKey, storetypes.StoreTypeIAVL, nil)
	if err := cms.LoadLatestVersion(); err != nil {
		t.Fatal(err)
	}
	env := &keeperEnv{cms: cms, key: key, registryKey: registryKey, keeper: NewKeeper(key), seats: seats, authority: authority, height: testHeight, unix: testTime}
	if err := env.keeper.Init(env.blockCtx(), gen); err != nil {
		t.Fatal(err)
	}
	return env
}

func readyKeeper(t testing.TB) *keeperEnv {
	t.Helper()
	gen, seats, authority := readyGenesis()
	return newKeeperEnv(t, gen, seats, authority)
}

// blockCtx has an infinite gas meter, as InitChain and EndBlock do.
func (e *keeperEnv) blockCtx() sdk.Context {
	header := cmtproto.Header{ChainID: testChainID, Height: e.height, Time: time.Unix(e.unix, 0).UTC()}
	return sdk.NewContext(e.cms, header, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter()).
		WithEventManager(sdk.NewEventManager()).
		WithExecMode(sdk.ExecModeFinalize)
}

// txCtx is one message: a finite gas meter inside the authority bounds and a
// fresh event manager.
func (e *keeperEnv) txCtx() sdk.Context {
	return e.blockCtx().WithGasMeter(storetypes.NewGasMeter(5_000_000))
}

// kv is the authority store without gas.
func (e *keeperEnv) kv() storetypes.KVStore {
	return e.cms.GetKVStore(e.key)
}

func (e *keeperEnv) policy(t testing.TB) uint64 {
	t.Helper()
	return loadAt(t, e.kv(), e.height, e.unix).PolicyVersion()
}

// queue proposes id from seats[0] and approves it with seats 0..2 through
// the keeper handlers.
func (e *keeperEnv) queue(t testing.TB, id string, msg *authorityv1.MsgProposeChange) {
	t.Helper()
	if _, err := e.keeper.Propose(e.txCtx(), propose(e.seats[0].Signer, id, e.policy(t), msg)); err != nil {
		t.Fatalf("propose %s: %v", id, err)
	}
	for i := 0; i < ApprovedThreshold; i++ {
		if _, err := e.keeper.Approve(e.txCtx(), approve(e.seats[i].Signer, "ap-"+id+"-"+strconv.Itoa(i), id)); err != nil {
			t.Fatalf("approve %s: %v", id, err)
		}
	}
}

func resultCode(t testing.TB, ctx sdk.Context) (code string, idempotent bool) {
	t.Helper()
	for _, event := range ctx.EventManager().Events() {
		if event.Type != "authority_result" {
			continue
		}
		for _, attr := range event.Attributes {
			switch attr.Key {
			case "code":
				code = attr.Value
			case "idempotent":
				idempotent = attr.Value == "true"
			}
		}
	}
	return code, idempotent
}

// The handler bounds transaction bytes and the gas limit before any store
// read, and a non-20-byte signer is MALFORMED. Coverage for the round-2
// checks that had no test (review NB13); fails if enforceCommitted drops a
// bound.
func TestHandlerChecksTxSizeAndGas(t *testing.T) {
	env := readyKeeper(t)
	msg := func(id string) *authorityv1.MsgProposeChange {
		return propose(env.seats[0].Signer, id, 1, governedMsg("max_page_limit", 10))
	}
	rows := []struct {
		name string
		ctx  sdk.Context
		msg  *authorityv1.MsgProposeChange
		err  error
	}{
		{"tx bytes over the bound", env.txCtx().WithTxBytes(make([]byte, genesis.MaxTxBytes+1)), msg("p-size"), svnerr.ErrBounds},
		{"gas limit 0", env.txCtx().WithGasMeter(storetypes.NewGasMeter(0)), msg("p-gas0"), svnerr.ErrBounds},
		{"gas limit over the bound", env.txCtx().WithGasMeter(storetypes.NewGasMeter(genesis.MaxGasPerTx + 1)), msg("p-gasmax"), svnerr.ErrBounds},
		{"short signer", env.txCtx(), func() *authorityv1.MsgProposeChange { m := msg("p-short"); m.Signer = bytes20(1)[:19]; return m }(), svnerr.ErrMalformed},
	}
	for _, row := range rows {
		before := dumpKV(env.kv())
		if _, err := env.keeper.Propose(row.ctx, row.msg); !errors.Is(err, row.err) {
			t.Fatalf("%s: %v", row.name, err)
		}
		wantUnchanged(t, env.kv(), before, row.name)
	}
	if _, err := env.keeper.Propose(env.txCtx().WithTxBytes(make([]byte, genesis.MaxTxBytes)), msg("p-edge")); err != nil {
		t.Fatalf("tx bytes at the bound: %v", err)
	}
	if _, err := env.keeper.Propose(env.txCtx().WithGasMeter(storetypes.NewGasMeter(genesis.MaxGasPerTx)), msg("p-gasedge")); err != nil {
		t.Fatalf("gas limit at the bound: %v", err)
	}
	low := env.txCtx().WithGasMeter(storetypes.NewGasMeter(authorityGas - 1))
	before := dumpKV(env.kv())
	func() {
		defer func() {
			if _, outOfGas := recover().(storetypes.ErrorOutOfGas); !outOfGas {
				t.Fatal("a gas limit below the fixed charge did not run out of gas")
			}
		}()
		_, _ = env.keeper.Propose(low, msg("p-low"))
	}()
	wantUnchanged(t, env.kv(), before, "out of gas")
}

// transition: success writes, an idempotent retry and a failure write
// nothing, and a keep outcome writes and reports its code on an event (plan
// 2.5). Round-2 behaviour was the same; fails if a keep is not written
// (mutant keep-not-written).
func TestTransitionOutcomes(t *testing.T) {
	env := readyKeeper(t)
	msg := func() *authorityv1.MsgProposeChange {
		m := propose(env.seats[0].Signer, "p-t", 1, governedMsg("max_page_limit", 10))
		m.ExpiryUnixSeconds = env.unix + 50
		return m
	}
	if _, err := env.keeper.Propose(env.txCtx(), msg()); err != nil {
		t.Fatal(err)
	}
	before := dumpKV(env.kv())
	retry := env.txCtx()
	if _, err := env.keeper.Propose(retry, msg()); err != nil {
		t.Fatal(err)
	}
	if code, idem := resultCode(t, retry); code != "" || !idem {
		t.Fatalf("retry event code %q idempotent %v", code, idem)
	}
	wantUnchanged(t, env.kv(), before, "idempotent retry")

	replay := msg()
	replay.ProposalId = "p-t2"
	failed := env.txCtx()
	if _, err := env.keeper.Propose(failed, replay); !errors.Is(err, svnerr.ErrReplay) {
		t.Fatalf("replay %v", err)
	}
	if code, _ := resultCode(t, failed); code != CodeReplay {
		t.Fatalf("replay event %q", code)
	}
	wantUnchanged(t, env.kv(), before, "replay")

	env.unix += 50
	kept := env.txCtx()
	if _, err := env.keeper.Approve(kept, approve(env.seats[1].Signer, "a-late", "p-t")); err != nil {
		t.Fatalf("keep returned %v", err)
	}
	if code, _ := resultCode(t, kept); code != CodeExpired {
		t.Fatalf("keep event %q", code)
	}
	if recordOf(t, env.kv(), "p-t").state() != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED || openOf(t, env.kv(), env.seats[0].ID) != 0 {
		t.Fatal("keep outcome was not written")
	}
}

// 128 pending validator changes fill the cap; the 129th execute is BOUNDS
// and stays QUEUED with the store unchanged; one EndBlock applies all 128.
// Fails on the old code, which shared the 128 lifetime quota with proposals,
// so the council could not even open the 129th proposal.
func TestPendingCap(t *testing.T) {
	env := readyKeeper(t)
	key := func(n int) []byte {
		out := pub(byte(n))
		out[0] = 0xaa
		out[1] = byte(n >> 8)
		return out
	}
	for i := 0; i < MaxPendingChanges; i++ {
		id := "add-" + strconv.Itoa(i)
		env.queue(t, id, powerMsg("n"+strconv.Itoa(i), key(i), 1, uint64(100+i)))
		if _, err := env.keeper.Execute(env.txCtx(), execute(env.authority, "ex-"+id, id)); err != nil {
			t.Fatalf("execute %s: %v", id, err)
		}
	}
	if got := len(pendingOf(t, env.kv())); got != MaxPendingChanges {
		t.Fatalf("%d pending", got)
	}
	env.queue(t, "add-over", powerMsg("n-over", key(500), 1, 400))
	before := dumpKV(env.kv())
	if _, err := env.keeper.Execute(env.txCtx(), execute(env.authority, "ex-over", "add-over")); !errors.Is(err, svnerr.ErrBounds) {
		t.Fatalf("129th pending change: %v", err)
	}
	wantUnchanged(t, env.kv(), before, "pending cap")
	if recordOf(t, env.kv(), "add-over").state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal("cap reject moved the proposal")
	}
	env.height = 100 + MaxPendingChanges - 1
	updates, err := env.keeper.EndBlock(env.blockCtx())
	if err != nil || len(updates) != MaxPendingChanges {
		t.Fatalf("one EndBlock applied %d: %v", len(updates), err)
	}
	if len(pendingOf(t, env.kv())) != 0 || len(loadAt(t, env.kv(), env.height, env.unix).Validators()) != 4+MaxPendingChanges {
		t.Fatal("EndBlock left entries or missed validators")
	}
}

// A genesis pending entry is applied by EndBlock at its height, not before.
// Round 2 applied it from the state key; this pins the 0x03 path and fails
// if InitStore drops pending entries (mutant init-skips-pending).
func TestGenesisPendingIsAppliedAtItsHeight(t *testing.T) {
	gen, seats, authority := readyGenesis()
	gen.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("gen-add", "v5", pub(9), 1, 5)}
	if err := ValidateGenesisState(gen, &GenesisPoint{InitialHeight: 1, GenesisUnixSeconds: testTime}); err != nil {
		t.Fatal(err)
	}
	env := newKeeperEnv(t, gen, seats, authority)
	env.height = 4
	if updates, err := env.keeper.EndBlock(env.blockCtx()); err != nil || len(updates) != 0 {
		t.Fatalf("height 4: %v %v", updates, err)
	}
	env.height = 5
	updates, err := env.keeper.EndBlock(env.blockCtx())
	if err != nil || len(updates) != 1 || updates[0].Power != 1 || !bytes.Equal(updates[0].PubKey.GetEd25519(), pub(9)) {
		t.Fatalf("height 5: %v %v", updates, err)
	}
	if len(pendingOf(t, env.kv())) != 0 {
		t.Fatal("applied entry is still pending")
	}
}

// A store with the round-2 side document halts EndBlock and is refused by
// export. Fails on the old code, which loaded 0x20 and carried on.
func TestLegacySideDocumentHalts(t *testing.T) {
	env := readyKeeper(t)
	env.kv().Set([]byte{legacySideKey}, []byte(`{"policy_version":1}`))
	if _, err := env.keeper.EndBlock(env.blockCtx()); err == nil || !strings.Contains(err.Error(), "re-initialize") {
		t.Fatalf("legacy store: %v", err)
	}
	if _, err := env.keeper.Export(env.blockCtx()); !errors.Is(err, ErrExportRefused) || !strings.Contains(err.Error(), "0x20") {
		t.Fatalf("legacy export: %v", err)
	}
}

// ExportGenesis returns the genesis for a fresh store and panics with the
// ExportState error once a message has written side state. Fails on the old
// code, which exported the state key and dropped the side document.
func TestExportGenesisPanicsOnSideState(t *testing.T) {
	env := readyKeeper(t)
	module := NewAppModule(env.keeper, nil)
	if raw := module.ExportGenesis(env.blockCtx(), nil); len(raw) == 0 {
		t.Fatal("fresh export is empty")
	}
	if _, err := env.keeper.Propose(env.txCtx(), propose(env.seats[0].Signer, "p-x", 1, governedMsg("max_page_limit", 10))); err != nil {
		t.Fatal(err)
	}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		module.ExportGenesis(env.blockCtx(), nil)
	}()
	err, isErr := recovered.(error)
	if !isErr || !errors.Is(err, ErrExportRefused) || !strings.Contains(fmt.Sprint(recovered), "proposal records") {
		t.Fatalf("export with side state: %v", recovered)
	}
}

// A config key that still holds pending entries, proposals or an approved
// council is the round-2 layout. LoadState refuses it, so each council
// message fails with ERROR_CODE_INTEGRITY through the keeper (svn error and
// authority_result code) and the store is byte-identical. EndBlock never
// applies the pending entries kept in the config: there is no 0x03 key, so
// it returns no update at their height. ExportState refuses the config.
// Pins README "No migration" and validator-lifecycle.md "Old devnet homes";
// the second fix-round review found that deleting the old-layout check
// passed every suite.
func TestOldLayoutConfigIsIntegrity(t *testing.T) {
	for _, row := range []struct {
		name string
		edit func(*authorityv1.GenesisState)
	}{
		{"pending entries", func(cfg *authorityv1.GenesisState) {
			cfg.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v5", pub(9), 1, 50)}
		}},
		{"proposals", func(cfg *authorityv1.GenesisState) {
			cfg.Authority.Proposals = []*authorityv1.Proposal{{ProposalId: "p-old", ParameterKey: "max_page_limit"}}
		}},
		{"approved council", func(cfg *authorityv1.GenesisState) {
			cfg.Authority.ApprovedCouncil = []*authorityv1.DevnetTestSeat{{SeatId: "seat-9", PublicKey: pub(50)}}
		}},
	} {
		env := readyKeeper(t)
		mutateConfig(t, env.kv(), row.edit)
		if _, err := LoadState(env.kv(), testChainID, env.height, env.unix); err == nil || !strings.Contains(err.Error(), "old layout") {
			t.Fatalf("%s: LoadState %v", row.name, err)
		}
		before := dumpKV(env.kv())
		messages := []struct {
			name string
			run  func(sdk.Context) error
		}{
			{"propose", func(ctx sdk.Context) error {
				_, err := env.keeper.Propose(ctx, propose(env.seats[0].Signer, "p-new", 1, governedMsg("max_page_limit", 10)))
				return err
			}},
			{"approve", func(ctx sdk.Context) error {
				_, err := env.keeper.Approve(ctx, approve(env.seats[1].Signer, "a-new", "p-new"))
				return err
			}},
			{"execute", func(ctx sdk.Context) error {
				_, err := env.keeper.Execute(ctx, execute(env.authority, "e-new", "p-new"))
				return err
			}},
		}
		for _, msg := range messages {
			ctx := env.txCtx()
			err := msg.run(ctx)
			if !errors.Is(err, svnerr.ErrIntegrity) || !strings.Contains(err.Error(), CodeIntegrity) {
				t.Fatalf("%s: %s got %v", row.name, msg.name, err)
			}
			wantUnchanged(t, env.kv(), before, row.name+": "+msg.name)
		}
		env.height = 50
		if updates, err := env.keeper.EndBlock(env.blockCtx()); err != nil || len(updates) != 0 {
			t.Fatalf("%s: EndBlock at 50: %v %v", row.name, updates, err)
		}
		wantUnchanged(t, env.kv(), before, row.name+": EndBlock")
		if _, err := ExportState(env.kv()); err == nil || errors.Is(err, ErrExportRefused) || !strings.Contains(err.Error(), "old layout") {
			t.Fatalf("%s: export %v", row.name, err)
		}
	}
}

// More than MaxPendingChanges keys under 0x03 is integrity damage; no
// message or genesis stores them. readPending refuses the 129th entry, so
// ApplyDue and a validator execute are INTEGRITY with the store unchanged
// and the proposal still QUEUED, EndBlock halts with ErrIntegrity, and
// ExportState refuses. At exactly 128 entries ApplyDue applies all of them
// and ExportState returns them. The second fix-round review found both
// bound checks unreached (coverage 0).
func TestPendingOverCapIsIntegrity(t *testing.T) {
	key := func(n int) []byte {
		out := pub(byte(n))
		out[0] = 0xbb
		out[1] = byte(n >> 8)
		return out
	}
	fill := func(kv KV, n int) {
		for i := 0; i < n; i++ {
			putPendingRaw(t, kv, heightEntry("over-"+strconv.Itoa(i), "n"+strconv.Itoa(i), key(i), 1, 1000))
		}
	}

	at := readyKeeper(t)
	fill(at.kv(), MaxPendingChanges)
	exported, err := ExportState(at.kv())
	if err != nil || len(exported.GetAuthority().GetPendingValidatorPower()) != MaxPendingChanges {
		t.Fatalf("export of %d entries: %v", MaxPendingChanges, err)
	}
	at.height = 1000
	if updates, err := at.keeper.EndBlock(at.blockCtx()); err != nil || len(updates) != MaxPendingChanges {
		t.Fatalf("EndBlock with %d entries applied %d: %v", MaxPendingChanges, len(updates), err)
	}

	env := readyKeeper(t)
	env.queue(t, "add-x", powerMsg("v5", pub(9), 1, 2000))
	fill(env.kv(), MaxPendingChanges+1)
	before := dumpKV(env.kv())
	if _, err := env.keeper.Execute(env.txCtx(), execute(env.authority, "ex-x", "add-x")); !errors.Is(err, svnerr.ErrIntegrity) {
		t.Fatalf("validator execute over the cap: %v", err)
	}
	wantUnchanged(t, env.kv(), before, "validator execute over the cap")
	if recordOf(t, env.kv(), "add-x").state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal("integrity reject moved the proposal")
	}
	if _, changed, out := loadAt(t, env.kv(), 1000, env.unix).ApplyDue(); changed || out.Code != CodeIntegrity {
		t.Fatalf("ApplyDue over the cap: %+v changed %v", out, changed)
	}
	wantUnchanged(t, env.kv(), before, "ApplyDue over the cap")
	env.height = 1000
	if updates, err := env.keeper.EndBlock(env.blockCtx()); !errors.Is(err, svnerr.ErrIntegrity) || len(updates) != 0 {
		t.Fatalf("EndBlock over the cap: %v %v", updates, err)
	}
	wantUnchanged(t, env.kv(), before, "EndBlock over the cap")
	// Export on a store that holds only config, admission and pending keys,
	// so the 0x10 refusal cannot answer first.
	over, _, _ := readyStore(t)
	fill(over, MaxPendingChanges+1)
	if _, err := ExportState(over); err == nil || errors.Is(err, ErrExportRefused) || !strings.Contains(err.Error(), "more than 128 pending") {
		t.Fatalf("export over the cap: %v", err)
	}
}
