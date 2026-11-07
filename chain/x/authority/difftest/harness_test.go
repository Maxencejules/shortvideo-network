package difftest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The real x/authority keeper is the read surface the registries consume
// and a business-id source, and the real registries are its council hooks.
var (
	_ authority.ProviderAdmission = providers.Keeper{}
	_ authority.ContentRemoval    = content.Keeper{}
	_ providers.Roles             = authority.Keeper{}
	_ providers.Limits            = authority.Keeper{}
	_ content.Roles               = authority.Keeper{}
	_ content.DevnetParams        = authority.Keeper{}
	_ content.Limits              = authority.Keeper{}
	_ msgkit.BusinessSource       = authority.Keeper{}
)

const (
	// txGasLimit is the gas meter limit of one transaction (the protocol's
	// largest allowed limit).
	txGasLimit = genesis.MaxGasPerTx
	// txBytesLen is the transaction byte count every message sees.
	txBytesLen = 512
	// startTime and startHeight are the first block of every sequence.
	startTime         = msgkittest.BlockUnixSeconds
	startHeight int64 = 1
	// seatCount and validatorCount are the genesis council and set.
	seatCount      = authority.ApprovedSeats
	validatorCount = 6
	// validatorPower is every genesis validator's power (spec genesis has
	// equal powers).
	validatorPower int64 = 10
	// packageBytes is the size of every registered content.
	packageBytes uint64 = 1000
	// providerAccounts is the number of ADMITTED_PROVIDER accounts.
	providerAccounts = 4
)

// Synthetic 20-byte accounts. No real keys or addresses.
var (
	serviceAcct     = account(0x31)
	broadcasterAcct = account(0x41)
	authorityAcct   = account(0x51)
	auditorAcct     = account(0x61)
	outsiderAcct    = account(0x71)
)

func account(b byte) []byte { return bytes.Repeat([]byte{b}, msgkit.AccountBytes) }

// providerAccount is the ADMITTED_PROVIDER account with index i.
func providerAccount(i int) []byte { return account(byte(0x81 + i)) }

// seatID, seatPub and seatAcct describe council seat i (1-based).
func seatID(i int) string              { return "seat-" + strconv.Itoa(i) }
func seatPub(i int) []byte             { return pubFor("seat-key-" + strconv.Itoa(i)) }
func seatAcct(i int) []byte            { return seatAddress(seatPub(i)) }
func nodeID(i int) string              { return "v" + strconv.Itoa(i) }
func consensusKey(label string) []byte { return pubFor("consensus-" + label) }

// submitterRoles is the genesis submitter table. Seat accounts are not
// submitters (DR-06-06).
func submitterRoles() map[string]typesv1.SubmitterRole {
	roles := map[string]typesv1.SubmitterRole{
		string(serviceAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
		string(broadcasterAcct): typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
		string(authorityAcct):   typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY,
		string(auditorAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
	}
	for i := 0; i < providerAccounts; i++ {
		roles[string(providerAccount(i))] = typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER
	}
	return roles
}

// roleName is the spec machine's role name (the enum name without its prefix).
func roleName(role typesv1.SubmitterRole) string {
	return strings.TrimPrefix(role.String(), "SUBMITTER_ROLE_")
}

// config is one sequence's chain configuration.
type config struct {
	// threshold is the DEC-008 devnet numeric value; the approved shape is
	// authority.ApprovedThreshold (G-6).
	threshold uint64
	delay     int64
	seeds     []*providersv1.Provider
	// trialCheck compares the trial branch of every rejected or idempotent
	// message with its pre-state; invariants runs the council invariant
	// checker after every step.
	trialCheck bool
	invariants bool
}

func defaultConfig() *config {
	return &config{
		threshold: authority.ApprovedThreshold, delay: 10, trialCheck: true, invariants: true,
		seeds: []*providersv1.Provider{
			seedRecord("seed-admitted", providerAccount(2), providersv1.ProviderState_PROVIDER_STATE_ADMITTED),
			seedRecord("seed-draining", providerAccount(3), providersv1.ProviderState_PROVIDER_STATE_DRAINING),
		},
	}
}

// genesisState is the authority genesis of cfg.
func genesisState(cfg *config) *authorityv1.GenesisState {
	auth := &authorityv1.AuthorityState{QuorumLoss: authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET}
	for i := 1; i <= validatorCount; i++ {
		auth.Validators = append(auth.Validators, &authorityv1.ValidatorPower{
			ConsensusPubkey: consensusKey(nodeID(i)), Power: validatorPower, NodeId: nodeID(i),
		})
	}
	for i := 1; i <= seatCount; i++ {
		auth.DevnetTestSeats = append(auth.DevnetTestSeats, &authorityv1.DevnetTestSeat{SeatId: seatID(i), PublicKey: seatPub(i)})
	}
	roles := submitterRoles()
	addrs := make([]string, 0, len(roles))
	for addr := range roles {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		auth.Submitters = append(auth.Submitters, &typesv1.Submitter{Address: []byte(addr), Role: roles[addr]})
	}
	state := &authorityv1.GenesisState{Authority: auth}
	for i := 1; i <= 8; i++ {
		state.BlockedDecisions = append(state.BlockedDecisions, &typesv1.BlockedDecisionParameter{
			DecisionId: fmt.Sprintf("DEC-%03d", i), SchemaVersion: 1,
		})
	}
	for _, id := range []string{content.HostingDecisionID, "DEC-002", "DEC-003"} {
		state.DevnetTestParameters = append(state.DevnetTestParameters, &typesv1.DevnetTestParameter{
			Label: genesis.DevnetTestLabel, DecisionId: id, RoleName: content.DevnetRoleName,
		})
	}
	state.DevnetTestParameters = append(state.DevnetTestParameters, &typesv1.DevnetTestParameter{
		Label: genesis.DevnetTestLabel, DecisionId: "DEC-008", NumericValue: cfg.threshold, DelaySeconds: cfg.delay,
	})
	return state
}

// eventRef is one typed event: its svn.events.v1 type without the package
// prefix, the id it reports, and, for the council events, the policy
// version it carries.
type eventRef struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	PolicyVersion string `json:"policy_version,omitempty"`
}

// outcome is the Go side of one step.
type outcome struct {
	code       msgkit.Code
	idempotent bool
	// keep is an authority keep outcome: the handler returned success with
	// a protocol code on authority_result (G-7, DR-06-05).
	keep bool
	// events are the compared typed events: for a council step only
	// PolicyChanged and ProposalExecuted (the spec's apply_proposal_payload
	// models no registry event, so the x/providers and x/content events of
	// a registry hook are not compared), for a registry transaction all of
	// them.
	events []eventRef
}

func (o outcome) codeName() string {
	if o.code == msgkit.OK {
		return "ok"
	}
	return string(o.code)
}

// harness holds the real keepers of one sequence.
type harness struct {
	tb   testing.TB
	cfg  *config
	ctx  sdk.Context
	keys []*storetypes.KVStoreKey // authority, providers, content
	auth authority.Keeper
	prov providers.Keeper
	cont content.Keeper
	now  int64
	// height is the current block height; transactions run at it.
	height int64
	// genesisConfig is the authority config key at genesis (I7).
	genesisConfig *authorityv1.GenesisState
	// executes counts successful executes (I1, exact form).
	executes uint64
	// proposers maps each committed proposal id to its proposing seat id
	// (I10).
	proposers map[string]string
	// last is the committed state after the latest step (or genesis): the
	// one read the invariants, the projection, and the generator share.
	last *committed
	// intercept wraps message routing; only the trial-snapshot mutant test
	// sets it.
	intercept func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error
}

func newHarness(tb testing.TB, cfg *config) *harness {
	tb.Helper()
	ctx, keys := msgkittest.NewContext(tb, authority.ModuleName, providers.ModuleName, content.ModuleName)
	h := &harness{
		tb: tb, cfg: cfg, now: startTime, height: startHeight, proposers: map[string]string{},
		keys: []*storetypes.KVStoreKey{keys[authority.ModuleName], keys[providers.ModuleName], keys[content.ModuleName]},
	}
	h.ctx = ctx.WithBlockTime(time.Unix(h.now, 0).UTC()).WithBlockHeight(h.height).
		WithGasMeter(storetypes.NewInfiniteGasMeter())
	read := authority.NewKeeper(keys[authority.ModuleName])
	peers := msgkit.NewPeers()
	h.prov = providers.NewKeeper(keys[providers.ModuleName], providers.Deps{Roles: read, Limits: read, Peers: peers})
	h.cont = content.NewKeeper(keys[content.ModuleName], content.Deps{Roles: read, Params: read, Limits: read, Peers: peers})
	h.auth = read.WithRegistry(h.prov, h.cont).WithPeers(peers)
	peers.Add(authority.ModuleName, h.auth)
	peers.Add(content.ModuleName, h.cont)
	peers.Add(providers.ModuleName, h.prov)

	gen := genesisState(cfg)
	if err := h.auth.Init(h.ctx, gen); err != nil {
		tb.Fatalf("authority genesis: %v", err)
	}
	h.genesisConfig = h.config(h.ctx)
	if err := h.prov.InitGenesis(h.ctx, &providersv1.GenesisState{Providers: cfg.seeds}); err != nil {
		tb.Fatalf("providers genesis: %v", err)
	}
	if err := h.cont.InitGenesis(h.ctx, &contentv1.GenesisState{}); err != nil {
		tb.Fatalf("content genesis: %v", err)
	}
	h.last = h.read(h.unmetered())
	return h
}

// committed is one read of the committed council state.
type committed struct {
	cfg     *authorityv1.GenesisState
	gov     *authorityv1.AuthorityState
	records []storedRecord
	version uint64
}

// read reads the committed council state at ctx: the config key, the whole
// Query/Governance walk, the stored proposal records, the policy version.
func (h *harness) read(ctx sdk.Context) *committed {
	h.tb.Helper()
	return &committed{cfg: h.config(ctx), gov: h.governance(ctx), records: h.records(ctx), version: h.auth.PolicyVersion(ctx)}
}

// config decodes the authority config key (0x01) at ctx.
func (h *harness) config(ctx sdk.Context) *authorityv1.GenesisState {
	h.tb.Helper()
	raw, err := h.auth.ConsensusBytes(ctx)
	if err != nil {
		h.tb.Fatalf("authority config: %v", err)
	}
	var cfg authorityv1.GenesisState
	if err := proto.Unmarshal(raw, &cfg); err != nil {
		h.tb.Fatalf("authority config does not decode: %v", err)
	}
	return &cfg
}

// unmetered is the current block context without a gas limit, for reads
// the harness makes outside a transaction.
func (h *harness) unmetered() sdk.Context {
	return h.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
}

// snapshot is every key of the three stores at ctx.
func (h *harness) snapshot(ctx sdk.Context) []byte {
	var out []byte
	for _, key := range h.keys {
		out = append(out, []byte(key.Name())...)
		out = append(out, 0)
		out = append(out, msgkit.StoreSnapshot(ctx.KVStore(key))...)
	}
	return out
}

// codeOf maps a handler error to its protocol code. An error outside the
// svn codespace fails the test.
func codeOf(tb testing.TB, err error) msgkit.Code {
	tb.Helper()
	if err == nil {
		return msgkit.OK
	}
	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	if codespace != svnerr.ModuleName {
		tb.Fatalf("error %v has codespace %q, want %q", err, codespace, svnerr.ModuleName)
	}
	name, ok := typesv1.ErrorCode_name[int32(code)]
	if !ok || code == 0 {
		tb.Fatalf("error %v has code %d outside svn.types.v1.ErrorCode", err, code)
	}
	return msgkit.Code(name)
}

// apply runs one step and returns the Go outcome.
func (h *harness) apply(s *step) outcome {
	h.tb.Helper()
	var out outcome
	if s.op == fxBlock {
		out = h.block(s.headerTime)
	} else {
		out = h.deliver(s)
	}
	if msg, ok := s.msg.(*authorityv1.MsgProposeChange); ok && out.code == msgkit.OK && !out.idempotent {
		h.proposers[msg.GetProposalId()] = h.seatOf(msg.GetSigner())
	}
	if s.op == opExecute && out.code == msgkit.OK && !out.idempotent {
		h.executes++
	}
	ctx := h.unmetered()
	h.last = h.read(ctx)
	if h.cfg.invariants {
		if err := h.checkCommitted(ctx, h.last); err != nil {
			h.tb.Fatalf("after %s (%s): %v", s.op, out.codeName(), err)
		}
	}
	return out
}

// seatOf is the seat id of a seat account, or "".
func (h *harness) seatOf(addr []byte) string {
	for i := 1; i <= seatCount; i++ {
		if bytes.Equal(seatAcct(i), addr) {
			return seatID(i)
		}
	}
	return ""
}

// deliver runs one transaction message as the SDK's FinalizeBlock does: a
// fresh event manager and gas meter, a cached branch that is written back
// only when the handler succeeds (an authority keep is a success). A
// rejected or idempotent message must leave the trial branch
// byte-identical to its pre-state.
func (h *harness) deliver(s *step) outcome {
	h.tb.Helper()
	ctx := h.ctx.WithEventManager(sdk.NewEventManager()).
		WithGasMeter(storetypes.NewGasMeter(txGasLimit)).
		WithTxBytes(make([]byte, txBytesLen))
	cache, write := ctx.CacheContext()
	var pre []byte
	if h.cfg.trialCheck {
		pre = h.snapshot(cache)
	}
	err := h.route(cache, s.msg)
	out := outcome{code: codeOf(h.tb, err)}
	if out.code != msgkit.OK {
		if h.cfg.trialCheck && !bytes.Equal(pre, h.snapshot(cache)) {
			h.tb.Errorf("rejected %s (%s) changed the trial state", s.op, out.code)
		}
		return out
	}
	events := cache.EventManager().Events()
	out.idempotent, out.keep, out.code = resultEvent(h.tb, events)
	if out.idempotent && h.cfg.trialCheck && !bytes.Equal(pre, h.snapshot(cache)) {
		h.tb.Errorf("idempotent %s changed the trial state", s.op)
	}
	write()
	h.parseEvents(&out, ctx.EventManager().Events(), s.council())
	return out
}

// resultEvent reads the *_result events of a successful handler: an
// idempotent retry, or an authority keep with its code. A keep and a
// retry together, two codes, or a result with no attribute fail the test.
func resultEvent(tb testing.TB, events sdk.Events) (idempotent, keep bool, code msgkit.Code) {
	tb.Helper()
	code = msgkit.OK
	for _, ev := range events {
		if !strings.HasSuffix(ev.Type, "_result") {
			continue
		}
		if len(ev.Attributes) != 1 {
			tb.Fatalf("%s carries %d attributes", ev.Type, len(ev.Attributes))
		}
		attr := ev.Attributes[0]
		switch {
		case attr.Key == "idempotent" && attr.Value == "true" && !idempotent && !keep:
			idempotent = true
		case attr.Key == "code" && ev.Type == "authority_result" && !idempotent && !keep:
			keep, code = true, msgkit.Code(attr.Value)
		default:
			tb.Fatalf("unexpected result event %s %s=%s", ev.Type, attr.Key, attr.Value)
		}
	}
	return idempotent, keep, code
}

// route calls the message server that owns msg. A handler panic fails the
// test.
func (h *harness) route(ctx sdk.Context, msg proto.Message) (err error) {
	h.tb.Helper()
	defer func() {
		if r := recover(); r != nil {
			h.tb.Fatalf("handler panic on %s: %v", proto.MessageName(msg), r)
		}
	}()
	if h.intercept != nil {
		return h.intercept(ctx, msg, h.dispatch)
	}
	return h.dispatch(ctx, msg)
}

func (h *harness) dispatch(ctx sdk.Context, msg proto.Message) error {
	var err error
	switch m := msg.(type) {
	case *authorityv1.MsgProposeChange:
		_, err = h.auth.Propose(ctx, m)
	case *authorityv1.MsgApproveChange:
		_, err = h.auth.Approve(ctx, m)
	case *authorityv1.MsgExecuteChange:
		_, err = h.auth.Execute(ctx, m)
	case *providersv1.MsgRegisterProvider:
		_, err = h.prov.RegisterProvider(ctx, m)
	case *contentv1.MsgRegisterContent:
		_, err = h.cont.RegisterContent(ctx, m)
	default:
		h.tb.Fatalf("no route for %T", msg)
	}
	return err
}

// block starts a new block at header time t and runs the x/authority
// EndBlock body (the due validator changes), as spec CommitBlock does at
// its new height. It is the only place the harness moves the clock and the
// height. An EndBlock error is a chain halt: nothing of the block commits,
// the clock and the height stay where they were, and the code is the
// outcome (G-8).
func (h *harness) block(t int64) outcome {
	h.tb.Helper()
	if t <= h.now {
		h.tb.Fatalf("block time %d is not after %d", t, h.now)
	}
	next := h.ctx.WithBlockTime(time.Unix(t, 0).UTC()).WithBlockHeight(h.height + 1)
	ctx := next.WithEventManager(sdk.NewEventManager()).WithGasMeter(storetypes.NewInfiniteGasMeter())
	cache, write := ctx.CacheContext()
	if _, err := h.auth.EndBlock(cache); err != nil {
		return outcome{code: codeOf(h.tb, err)}
	}
	write()
	h.ctx, h.now = next, t
	h.height++
	return outcome{events: []eventRef{}}
}

// councilEvents are the typed events a council step compares.
var councilEvents = map[string]bool{"PolicyChanged": true, "ProposalExecuted": true}

// parseEvents fills out.events with the typed svn.events.v1 events (parsed
// with msgkit.ParseTyped, not string scraping).
func (h *harness) parseEvents(out *outcome, events sdk.Events, council bool) {
	h.tb.Helper()
	out.events = []eventRef{}
	for _, ev := range events {
		if !strings.HasPrefix(ev.Type, "svn.events.v1.") {
			continue
		}
		msg, err := msgkit.ParseTyped(abci.Event(ev))
		if err != nil {
			h.tb.Fatalf("typed event %s does not parse: %v", ev.Type, err)
		}
		kind := strings.TrimPrefix(ev.Type, "svn.events.v1.")
		if council && !councilEvents[kind] {
			continue
		}
		ref := eventRef{Type: kind, ID: eventID(msg)}
		if council {
			ref.PolicyVersion = msgkit.Dec(eventVersion(msg))
		}
		out.events = append(out.events, ref)
	}
}

// eventIDFields is the order in which an event's reported id is chosen.
var eventIDFields = []protoreflect.Name{"proposal_id", "parameter_key", "provider_id", "content_id"}

func eventID(msg proto.Message) string {
	m := msg.ProtoReflect()
	if m.Descriptor().Name() == "PolicyChanged" {
		return m.Get(m.Descriptor().Fields().ByName("parameter_key")).String()
	}
	for _, name := range eventIDFields {
		fd := m.Descriptor().Fields().ByName(name)
		if fd != nil && fd.Kind() == protoreflect.StringKind && m.Get(fd).String() != "" {
			return m.Get(fd).String()
		}
	}
	return ""
}

func eventVersion(msg proto.Message) uint64 {
	m := msg.ProtoReflect()
	fd := m.Descriptor().Fields().ByName("policy_version")
	if fd == nil {
		return 0
	}
	return m.Get(fd).Uint()
}

// Authority store layout read by the projection and the invariant checker
// (chain/x/authority/store.go; README "Store layout"). TestStoreLayout ties
// each prefix to the keeper's own read surface.
const (
	prefixPending  byte = 0x03
	prefixPolicy   byte = 0x04
	prefixProposal byte = 0x10
	prefixBusiness byte = 0x11
	prefixExecuted byte = 0x12
	prefixGoverned byte = 0x13
	prefixOpen     byte = 0x14
	prefixRetired  byte = 0x15
)

func eachRow(store storetypes.KVStore, prefix byte, visit func(key, value []byte)) {
	it := storetypes.KVStorePrefixIterator(store, []byte{prefix})
	defer it.Close()
	for ; it.Valid(); it.Next() {
		visit(it.Key(), it.Value())
	}
}

func decodeU64(tb testing.TB, what string, raw []byte) uint64 {
	tb.Helper()
	if len(raw) != 8 {
		tb.Fatalf("%s holds %d bytes, not 8", what, len(raw))
	}
	return binary.BigEndian.Uint64(raw)
}
