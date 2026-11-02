package difftest

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases/leasestest"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The real keepers satisfy the read surfaces x/leases declares (the real
// x/authority keeper too, although these tests use a fake of it), the
// lease keeper is the content withdrawal hook the app wires, and the fake
// authority satisfies every surface it stands in for.
var (
	_ leases.Providers          = providers.Keeper{}
	_ leases.Content            = content.Keeper{}
	_ leasestest.ProviderReader = providers.Keeper{}
	_ content.WithdrawalHook    = leases.Keeper{}
	_ msgkit.BusinessSource     = leases.Keeper{}
	_ leases.Roles              = authority.Keeper{}
	_ leases.Limits             = authority.Keeper{}
	_ leases.DevnetParams       = authority.Keeper{}
	_ leases.Policy             = authority.Keeper{}
	_ leases.InitRecorder       = (*authority.Order)(nil)
	_ leases.Roles              = fakeAuthority{}
	_ leases.Limits             = fakeAuthority{}
	_ leases.DevnetParams       = fakeAuthority{}
	_ leases.Policy             = fakeAuthority{}
	_ providers.Roles           = fakeAuthority{}
	_ providers.Limits          = fakeAuthority{}
	_ content.Roles             = fakeAuthority{}
	_ content.DevnetParams      = fakeAuthority{}
	_ content.Limits            = fakeAuthority{}
)

const (
	authorityStore = "authority"
	// txGasLimit is the gas meter limit of one transaction (the protocol's
	// largest allowed limit, msgkit.Enforce).
	txGasLimit uint64 = 20_000_000
	// txBytesLen is the transaction byte count every message sees.
	txBytesLen = 512
	// startTime is the header time of the first block of every sequence.
	startTime = msgkittest.BlockUnixSeconds
	// termSeconds is the DEC-005 devnet test number of the families that
	// have one. It is a test value, not policy (DEC-005 is unset).
	termSeconds int64 = 3600
)

// Synthetic 20-byte accounts. No real keys or addresses.
var (
	serviceAcct     = account(0x31)
	service2Acct    = account(0x32)
	broadcasterAcct = account(0x41)
	authorityAcct   = account(0x51)
	auditorAcct     = account(0x61)
	outsiderAcct    = account(0x71)
)

func account(b byte) []byte { return bytes.Repeat([]byte{b}, msgkit.AccountBytes) }

// providerAccount is the ADMITTED_PROVIDER account with index i.
func providerAccount(i int) []byte { return account(byte(0x81 + i)) }

// providerAccounts is the number of provider accounts with a role.
const providerAccounts = 8

func hexOf(b []byte) string { return hex.EncodeToString(b) }

// roleTable is the submitter roles of every sequence: the fake authority
// returns them and the oracle's submitters config repeats them.
func roleTable() map[string]typesv1.SubmitterRole {
	roles := map[string]typesv1.SubmitterRole{
		string(serviceAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
		string(service2Acct):    typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
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

// fakeAuthority is the unit-test stand-in for the x/authority read surface.
// Its rows live in the "authority" IAVL store, so its reads are metered
// store reads and belong to every trial snapshot:
//
//	0x01 | account   -> role (one byte)
//	0x02 | limit key -> governed value (8 bytes BE)
//	0x03 | decision  -> devnet numeric value (8 bytes BE)
//	0x04 | decision  -> devnet role name
//	0x05             -> policy version (8 bytes BE)
//
// It is not x/authority and is never presented as such.
type fakeAuthority struct{ key *storetypes.KVStoreKey }

const (
	authRole       byte = 0x01
	authLimit      byte = 0x02
	authNumeric    byte = 0x03
	authDevnetRole byte = 0x04
	authPolicy     byte = 0x05
)

func authKey(prefix byte, suffix string) []byte { return append([]byte{prefix}, suffix...) }

func (f fakeAuthority) Role(ctx sdk.Context, addr []byte) typesv1.SubmitterRole {
	raw := ctx.KVStore(f.key).Get(authKey(authRole, string(addr)))
	if len(raw) != 1 {
		return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
	}
	return typesv1.SubmitterRole(raw[0])
}

func (f fakeAuthority) uint(ctx sdk.Context, key []byte) (uint64, bool) {
	raw := ctx.KVStore(f.key).Get(key)
	if len(raw) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(raw), true
}

func (f fakeAuthority) GovernedLimit(ctx sdk.Context, key string) (uint64, bool) {
	return f.uint(ctx, authKey(authLimit, key))
}

func (f fakeAuthority) DevnetNumeric(ctx sdk.Context, decisionID string) (uint64, bool) {
	return f.uint(ctx, authKey(authNumeric, decisionID))
}

func (f fakeAuthority) DevnetRole(ctx sdk.Context, decisionID string) (string, bool) {
	raw := ctx.KVStore(f.key).Get(authKey(authDevnetRole, decisionID))
	if raw == nil {
		return "", false
	}
	return string(raw), true
}

func (f fakeAuthority) PolicyVersion(ctx sdk.Context) uint64 {
	v, found := f.uint(ctx, []byte{authPolicy})
	if !found {
		return 1
	}
	return v
}

func (f fakeAuthority) write(ctx sdk.Context, cfg *config) {
	store := ctx.KVStore(f.key)
	be := func(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
	for addr, role := range roleTable() {
		store.Set(authKey(authRole, addr), []byte{byte(role)})
	}
	for key, value := range cfg.governed {
		store.Set(authKey(authLimit, key), be(value))
	}
	if cfg.term > 0 {
		store.Set(authKey(authNumeric, leases.TermDecisionID), be(cfg.term))
	}
	for _, decision := range []string{content.HostingDecisionID, "DEC-002", "DEC-003"} {
		store.Set(authKey(authDevnetRole, decision), []byte(content.DevnetRoleName))
	}
	store.Set([]byte{authPolicy}, be(1))
}

// config is one sequence's chain configuration.
type config struct {
	// term is the DEC-005 devnet number; 0 means the chain has none (every
	// offer and renewal is DECISION_BLOCKED).
	term     uint64
	governed map[string]uint64
	// seeds are provider records written by providers.InitGenesis before the
	// first step (the SeedProvider fixture).
	seeds []*providersv1.Provider
	// trialCheck compares the trial branch of every rejected or idempotent
	// message with its pre-state; invariants runs CheckInvariants and
	// CheckOfferIntegrity after every step. The scale tests turn both off
	// and check invariants per block instead.
	trialCheck bool
	invariants bool
}

// eventRef is one typed event: its svn.events.v1 type without the package
// prefix and the id it reports (the first of lease_id, offer_id,
// provider_id, content_id it carries).
type eventRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// outcome is the Go side of one step.
type outcome struct {
	code       msgkit.Code
	idempotent bool
	events     []eventRef
	// closed is the offer ids of leases_offer_closed events, in order.
	closed []string
	// released is the order in which leases ended and offers closed:
	// "lease:<id>" for a LeaseEnded event, "offer:<id>" for a
	// leases_offer_closed event.
	released []string
	gas      uint64
}

func (o outcome) codeName() string {
	if o.code == msgkit.OK {
		return "ok"
	}
	return string(o.code)
}

// harness holds the real keepers of one sequence.
type harness struct {
	tb          testing.TB
	cfg         *config
	ctx         sdk.Context
	keys        []*storetypes.KVStoreKey
	auth        fakeAuthority
	prov        providers.Keeper
	contentRead content.Keeper
	cont        content.Keeper
	lease       leases.Keeper
	now         int64
	height      int64
	baseline    map[string]uint64
	ended       map[string]bool
	// intercept wraps message routing; only the trial-snapshot mutant test
	// sets it.
	intercept func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error
}

func newHarness(tb testing.TB, cfg *config) *harness {
	tb.Helper()
	ctx, keys := msgkittest.NewContext(tb, authorityStore, providers.ModuleName, content.ModuleName, leases.ModuleName)
	h := &harness{
		tb:       tb,
		cfg:      cfg,
		auth:     fakeAuthority{key: keys[authorityStore]},
		now:      startTime,
		height:   1,
		baseline: map[string]uint64{},
		ended:    map[string]bool{},
		keys: []*storetypes.KVStoreKey{
			keys[authorityStore], keys[providers.ModuleName], keys[content.ModuleName], keys[leases.ModuleName],
		},
	}
	h.ctx = ctx.WithBlockTime(time.Unix(h.now, 0).UTC()).WithBlockHeight(h.height).
		WithGasMeter(storetypes.NewInfiniteGasMeter())
	h.auth.write(h.ctx, cfg)

	peers := msgkit.NewPeers()
	h.prov = providers.NewKeeper(keys[providers.ModuleName], providers.Deps{Roles: h.auth, Limits: h.auth, Peers: peers})
	h.contentRead = content.NewKeeper(keys[content.ModuleName], content.Deps{Roles: h.auth, Params: h.auth, Limits: h.auth, Peers: peers})
	h.lease = leases.NewKeeper(keys[leases.ModuleName], leases.Deps{
		Roles: h.auth, Limits: h.auth, Params: h.auth, Policy: h.auth,
		Providers: h.prov, Content: h.contentRead, Peers: peers,
	})
	h.cont = content.NewKeeper(keys[content.ModuleName], content.Deps{
		Roles: h.auth, Params: h.auth, Limits: h.auth, Peers: peers, Hook: h.lease,
	})
	peers.Add(providers.ModuleName, h.prov)
	peers.Add(content.ModuleName, h.cont)
	peers.Add(leases.ModuleName, h.lease)

	if err := h.prov.InitGenesis(h.ctx, &providersv1.GenesisState{Providers: cfg.seeds}); err != nil {
		tb.Fatalf("providers genesis: %v", err)
	}
	for _, seed := range cfg.seeds {
		h.baseline[seed.GetProviderId()] = seed.GetReservedBytes()
	}
	if err := h.cont.InitGenesis(h.ctx, &contentv1.GenesisState{}); err != nil {
		tb.Fatalf("content genesis: %v", err)
	}
	if err := h.lease.InitGenesis(h.ctx, &leasesv1.GenesisState{}); err != nil {
		tb.Fatalf("leases genesis: %v", err)
	}
	return h
}

// unmetered is the current block context without a gas limit, for reads
// the harness makes outside a transaction.
func (h *harness) unmetered() sdk.Context {
	return h.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
}

func (h *harness) snapshot(ctx sdk.Context) []byte { return leasestest.StoreSnapshot(ctx, h.keys...) }

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
	switch s.op {
	case fxBlock:
		out = h.block(s.headerTime)
	case fxAdmit, fxDisable, fxForceWithdraw:
		out = h.council(s)
	default:
		out = h.deliver(s)
	}
	for _, ev := range out.events {
		if ev.Type != "LeaseEnded" {
			continue
		}
		if h.ended[ev.ID] {
			h.tb.Errorf("I8: lease %q emitted LeaseEnded twice (step %s)", ev.ID, s.op)
		}
		h.ended[ev.ID] = true
	}
	if h.cfg.invariants {
		h.checkInvariants(s.op)
	}
	return out
}

func (h *harness) checkInvariants(what string) {
	h.tb.Helper()
	ctx := h.unmetered()
	if err := leasestest.CheckInvariants(ctx, h.lease, h.prov, h.baseline); err != nil {
		h.tb.Fatalf("after %s: %v", what, err)
	}
	if err := leasestest.CheckOfferIntegrity(ctx, h.lease, h.contentRead); err != nil {
		h.tb.Fatalf("after %s: %v", what, err)
	}
}

// deliver runs one transaction message as the SDK's FinalizeBlock does: a
// fresh event manager and gas meter, a cached branch that is written back
// only when the handler succeeds. A rejected or idempotent message must
// leave the trial branch byte-identical to its pre-state.
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
	out := outcome{code: codeOf(h.tb, err), gas: ctx.GasMeter().GasConsumed()}
	if out.code != msgkit.OK {
		if h.cfg.trialCheck && !bytes.Equal(pre, h.snapshot(cache)) {
			h.tb.Errorf("rejected %s (%s) changed the trial state", s.op, out.code)
		}
		return out
	}
	out.idempotent = idempotentEvent(cache.EventManager().Events())
	if out.idempotent && h.cfg.trialCheck && !bytes.Equal(pre, h.snapshot(cache)) {
		h.tb.Errorf("idempotent %s changed the trial state", s.op)
	}
	write()
	h.parseEvents(&out, ctx.EventManager().Events())
	return out
}

// route calls the message server that owns msg. A handler panic fails the
// test: it is an invariant violation the SDK would turn into a failed
// transaction, and no step of these tests should reach one.
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
	case *leasesv1.MsgOfferLease:
		_, err = h.lease.OfferLease(ctx, m)
	case *leasesv1.MsgAcceptLease:
		_, err = h.lease.AcceptLease(ctx, m)
	case *leasesv1.MsgRenewLease:
		_, err = h.lease.RenewLease(ctx, m)
	case *leasesv1.MsgEndLease:
		_, err = h.lease.EndLease(ctx, m)
	case *providersv1.MsgRegisterProvider:
		_, err = h.prov.RegisterProvider(ctx, m)
	case *providersv1.MsgUpdateProvider:
		_, err = h.prov.UpdateProvider(ctx, m)
	case *providersv1.MsgRotateProviderKey:
		_, err = h.prov.RotateProviderKey(ctx, m)
	case *providersv1.MsgDrainProvider:
		_, err = h.prov.DrainProvider(ctx, m)
	case *contentv1.MsgRegisterContent:
		_, err = h.cont.RegisterContent(ctx, m)
	case *contentv1.MsgWithdrawContent:
		_, err = h.cont.WithdrawContent(ctx, m)
	default:
		h.tb.Fatalf("no route for %T", msg)
	}
	return err
}

// block starts a new block at header time t and runs the x/leases
// BeginBlock body (ExpireDue). It is the only place the harness moves the
// clock: every later transaction sees t as its committed time.
func (h *harness) block(t int64) outcome {
	h.tb.Helper()
	if t <= h.now {
		h.tb.Fatalf("block time %d is not after %d", t, h.now)
	}
	h.now = t
	h.height++
	h.ctx = h.ctx.WithBlockTime(time.Unix(t, 0).UTC()).WithBlockHeight(h.height)
	ctx := h.ctx.WithEventManager(sdk.NewEventManager()).WithGasMeter(storetypes.NewInfiniteGasMeter())
	if err := h.lease.ExpireDue(ctx); err != nil {
		h.tb.Fatalf("ExpireDue at %d: %v", t, err)
	}
	out := outcome{gas: ctx.GasMeter().GasConsumed()}
	h.parseEvents(&out, ctx.EventManager().Events())
	return out
}

// council applies an executed council change the way x/authority does:
// the hook runs in a cached branch that is written back only on OK.
func (h *harness) council(s *step) outcome {
	h.tb.Helper()
	ctx := h.ctx.WithEventManager(sdk.NewEventManager()).WithGasMeter(storetypes.NewGasMeter(txGasLimit))
	cache, write := ctx.CacheContext()
	var pre []byte
	if h.cfg.trialCheck {
		pre = h.snapshot(cache)
	}
	var code msgkit.Code
	switch s.op {
	case fxAdmit:
		code = h.prov.AdmitByCouncil(cache, s.id, true)
	case fxDisable:
		code = h.prov.DisableByCouncil(cache, s.id)
	case fxForceWithdraw:
		code = h.cont.ForceWithdrawByCouncil(cache, s.id, s.version, s.reason, true)
	}
	out := outcome{code: code, gas: ctx.GasMeter().GasConsumed()}
	if code != msgkit.OK {
		if h.cfg.trialCheck && !bytes.Equal(pre, h.snapshot(cache)) {
			h.tb.Errorf("rejected %s (%s) changed the trial state", s.op, code)
		}
		return out
	}
	write()
	h.parseEvents(&out, ctx.EventManager().Events())
	return out
}

func idempotentEvent(events sdk.Events) bool {
	for _, ev := range events {
		if !strings.HasSuffix(ev.Type, "_result") {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == "idempotent" && attr.Value == "true" {
				return true
			}
		}
	}
	return false
}

// eventIDFields is the order in which an event's reported id is chosen.
var eventIDFields = []protoreflect.Name{"lease_id", "offer_id", "provider_id", "content_id"}

// parseEvents fills out.events with the typed svn.events.v1 events
// (parsed with msgkit.ParseTyped, not string scraping), out.closed with the
// offer ids of leases_offer_closed events, and out.released with both in
// event order. Other untyped events (*_result) are ignored.
func (h *harness) parseEvents(out *outcome, events sdk.Events) {
	h.tb.Helper()
	out.events = []eventRef{}
	for _, ev := range events {
		switch {
		case strings.HasPrefix(ev.Type, "svn.events.v1."):
			msg, err := msgkit.ParseTyped(abci.Event(ev))
			if err != nil {
				h.tb.Fatalf("typed event %s does not parse: %v", ev.Type, err)
			}
			ref := eventRef{Type: strings.TrimPrefix(ev.Type, "svn.events.v1."), ID: eventID(msg)}
			out.events = append(out.events, ref)
			if ref.Type == "LeaseEnded" {
				out.released = append(out.released, "lease:"+ref.ID)
			}
		case ev.Type == leases.EventOfferClosed:
			if len(ev.Attributes) != 2 || ev.Attributes[0].Key != leases.AttrOfferID || ev.Attributes[1].Key != leases.AttrReason {
				h.tb.Fatalf("leases_offer_closed attributes %v", ev.Attributes)
			}
			out.closed = append(out.closed, ev.Attributes[0].Value)
			out.released = append(out.released, "offer:"+ev.Attributes[0].Value)
		}
	}
}

func eventID(msg proto.Message) string {
	m := msg.ProtoReflect()
	for _, name := range eventIDFields {
		fd := m.Descriptor().Fields().ByName(name)
		if fd != nil && fd.Kind() == protoreflect.StringKind && m.Get(fd).String() != "" {
			return m.Get(fd).String()
		}
	}
	return ""
}

// view is the decoded committed state the generator chooses inputs from and
// the projection is built from. It is read without gas.
type view struct {
	now         int64
	providers   map[string]*providersv1.Provider
	providerIDs []string
	contents    map[string]*contentv1.Content
	tombstones  map[string]*contentv1.Tombstone
	contentIDs  []string
	offers      map[string]*leasesv1.Offer
	offerIDs    []string
	leases      map[string]*leasesv1.Lease
	leaseIDs    []string
	// used is the offer ids with a used-offer marker (deviation D-4).
	used map[string]bool
	// pairs maps pairKey(provider, content, version) to the held lease id.
	pairs         map[string]string
	pairRows      []pairView
	providerOpen  map[string]int
	contentOpen   map[string]int
	businessIDs   []string
	maxOpenOffers uint64
	maxExpiry     uint64
}

func pairKey(providerID, contentID string, version uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", providerID, contentID, version)
}

func (h *harness) view() *view {
	h.tb.Helper()
	ctx := h.unmetered()
	v := &view{
		now:          h.now,
		providers:    map[string]*providersv1.Provider{},
		contents:     map[string]*contentv1.Content{},
		tombstones:   map[string]*contentv1.Tombstone{},
		offers:       map[string]*leasesv1.Offer{},
		leases:       map[string]*leasesv1.Lease{},
		used:         map[string]bool{},
		pairs:        map[string]string{},
		providerOpen: map[string]int{},
		contentOpen:  map[string]int{},
	}
	provGenesis, err := h.prov.ExportGenesis(ctx)
	if err != nil {
		h.tb.Fatalf("providers export: %v", err)
	}
	for _, p := range provGenesis.GetProviders() {
		v.providers[p.GetProviderId()] = p
		v.providerIDs = append(v.providerIDs, p.GetProviderId())
	}
	contentGenesis := h.cont.ExportGenesis(ctx)
	for _, c := range contentGenesis.GetContents() {
		v.contents[c.GetContentId()] = c
		v.contentIDs = append(v.contentIDs, c.GetContentId())
	}
	for _, tomb := range contentGenesis.GetTombstones() {
		v.tombstones[tomb.GetContentId()] = tomb
	}
	leaseGenesis, err := h.lease.ExportGenesis(ctx)
	if err != nil {
		h.tb.Fatalf("leases export: %v", err)
	}
	for _, o := range leaseGenesis.GetOffers() {
		v.offers[o.GetOfferId()] = o
		v.offerIDs = append(v.offerIDs, o.GetOfferId())
		v.providerOpen[o.GetProviderId()]++
		v.contentOpen[o.GetContentId()]++
	}
	for _, l := range leaseGenesis.GetLeases() {
		v.leases[l.GetLeaseId()] = l
		v.leaseIDs = append(v.leaseIDs, l.GetLeaseId())
	}
	leaseStore := ctx.MultiStore().GetKVStore(h.keys[3])
	eachRow(leaseStore, leases.UsedOfferPrefix, func(key, _ []byte) {
		v.used[string(key[1:])] = true
	})
	eachRow(leaseStore, leases.PairPrefix, func(key, value []byte) {
		parts := bytes.SplitN(key[1:], []byte{0x00}, 3)
		if len(parts) != 3 || len(parts[2]) != 8 {
			h.tb.Fatalf("pair row %x is malformed", key)
		}
		version := binary.BigEndian.Uint64(parts[2])
		v.pairs[pairKey(string(parts[0]), string(parts[1]), version)] = string(value)
		v.pairRows = append(v.pairRows, pairView{
			ProviderID: string(parts[0]), ContentID: string(parts[1]),
			ContentVersion: msgkit.Dec(version), LeaseID: string(value),
		})
	})
	v.businessIDs = h.businessIDs(ctx)
	limits := func(key string) uint64 {
		governed, found := h.auth.GovernedLimit(ctx, key)
		return msgkit.Effective(key, governed, found)
	}
	v.maxOpenOffers = limits(msgkit.LimitMaxOpenOffers)
	v.maxExpiry = limits(msgkit.LimitMaxExpirySteps)
	return v
}

// businessIDs is the union of the providers, content, and leases ledgers.
// The protocol has one business-id namespace, so an id in two ledgers is a
// failure.
func (h *harness) businessIDs(ctx sdk.Context) []string {
	h.tb.Helper()
	seen := map[string]string{}
	ledgers := []struct {
		name   string
		key    *storetypes.KVStoreKey
		prefix byte
	}{
		{providers.ModuleName, h.keys[1], providers.BusinessKeyPrefix},
		{content.ModuleName, h.keys[2], content.BusinessPrefix},
		{leases.ModuleName, h.keys[3], leases.BusinessPrefix},
	}
	ids := []string{}
	for _, ledger := range ledgers {
		eachRow(ctx.MultiStore().GetKVStore(ledger.key), ledger.prefix, func(key, value []byte) {
			id := string(key[1:])
			if len(value) != msgkit.DigestBytes {
				h.tb.Fatalf("%s business row %q holds %d bytes", ledger.name, id, len(value))
			}
			if other, dup := seen[id]; dup {
				h.tb.Fatalf("business id %q committed by %s and %s", id, other, ledger.name)
			}
			seen[id] = ledger.name
			ids = append(ids, id)
		})
	}
	sort.Strings(ids)
	return ids
}

func eachRow(store storetypes.KVStore, prefix byte, visit func(key, value []byte)) {
	it := storetypes.KVStorePrefixIterator(store, []byte{prefix})
	defer it.Close()
	for ; it.Valid(); it.Next() {
		visit(it.Key(), it.Value())
	}
}

// heldDue counts the items ExpireDue still has to process at the view's
// time: held leases whose deadline and open offers whose expiry is not
// after it.
func (v *view) heldDue() int {
	n := 0
	for _, l := range v.leases {
		if l.GetReservationHeld() && l.GetDeadlineUnixSeconds() <= v.now {
			n++
		}
	}
	for _, o := range v.offers {
		if o.GetExpiryUnixSeconds() <= v.now {
			n++
		}
	}
	return n
}
