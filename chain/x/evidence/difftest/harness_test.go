package difftest

import (
	"bytes"
	"crypto/sha256"
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
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases/leasestest"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The real keepers satisfy the read surfaces x/evidence and x/leases
// declare, as the app wires them (chain/app/app.go), and the fake authority
// satisfies every surface it stands in for.
var (
	_ evidence.Leases        = leases.Keeper{}
	_ evidence.Content       = content.Keeper{}
	_ leases.Evidence        = evidence.Keeper{}
	_ msgkit.BusinessSource  = evidence.Keeper{}
	_ content.WithdrawalHook = leases.Keeper{}
	_ evidence.Roles         = fakeAuthority{}
	_ evidence.Limits        = fakeAuthority{}
	_ leases.Roles           = fakeAuthority{}
	_ leases.Limits          = fakeAuthority{}
	_ leases.DevnetParams    = fakeAuthority{}
	_ leases.Policy          = fakeAuthority{}
	_ providers.Roles        = fakeAuthority{}
	_ providers.Limits       = fakeAuthority{}
	_ content.Roles          = fakeAuthority{}
	_ content.DevnetParams   = fakeAuthority{}
	_ content.Limits         = fakeAuthority{}
)

const (
	authorityStore = "authority"
	// txGasLimit is the gas meter limit of one transaction (the protocol's
	// largest allowed limit, msgkit.Enforce).
	txGasLimit uint64 = 20_000_000
	// txBytesLen is the transaction byte count every lease and content
	// message sees. An audit batch sees its own encoded message length
	// (deliver), so the 65536-byte rule of msgkit.Enforce applies to it.
	txBytesLen = 512
	// startTime is the header time of the first block of every sequence:
	// 2026-01-01T00:00:00Z, an epoch boundary (EpochSeconds 3600).
	startTime = msgkittest.BlockUnixSeconds
	// termSeconds is the DEC-005 devnet number of every sequence. It is a
	// test value, not policy (DEC-005 is unset).
	termSeconds int64 = 3600
	// epochSeconds is the round length the harness computes epochs with. It
	// equals evidence.EpochSeconds (TestHarnessConstants); the harness keeps
	// its own copy so a changed chain constant shows up as a divergence.
	epochSeconds int64 = 3600
)

// Store indexes in harness.keys.
const (
	keyAuthority = iota
	keyProviders
	keyContent
	keyLeases
	keyEvidence
)

// Synthetic 20-byte accounts. No real keys or addresses. The auditor
// accounts are the addresses of the auditor test keys (Q1: the account key
// signs), computed by addressOf.
var (
	serviceAcct     = account(0x31)
	broadcasterAcct = account(0x41)
	authorityAcct   = account(0x51)
	outsiderAcct    = account(0x71)
	auditorA        = addressOf(pubFor(auditorKeyA))
	auditorB        = addressOf(pubFor(auditorKeyB))
)

func account(b byte) []byte { return bytes.Repeat([]byte{b}, 20) }

// providerAccount is the ADMITTED_PROVIDER account with index i.
func providerAccount(i int) []byte { return account(byte(0x81 + i)) }

// providerAccounts is the number of provider accounts with a role.
const providerAccounts = 4

// addressOf is the account address of an ed25519 public key: the first 20
// bytes of its SHA-256, as the SDK's ed25519 PubKey.Address computes it.
// It is the harness's own computation, not evidence.AuditorAddress.
func addressOf(pub []byte) []byte {
	sum := sha256.Sum256(pub)
	return append([]byte(nil), sum[:20]...)
}

// prefixOf is the id prefix of an account: the lowercase hex of its first
// 8 bytes and "." (deviation D-17), computed by the harness.
func prefixOf(addr []byte) string { return hex.EncodeToString(addr[:8]) + "." }

func hexOf(b []byte) string { return hex.EncodeToString(b) }

// roleTable is the submitter roles of every sequence: the fake authority
// returns them and the oracle's submitters config repeats them.
func roleTable() map[string]typesv1.SubmitterRole {
	roles := map[string]typesv1.SubmitterRole{
		string(serviceAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
		string(broadcasterAcct): typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
		string(authorityAcct):   typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY,
		string(auditorA):        typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
		string(auditorB):        typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
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

func be(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

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
	for addr, role := range roleTable() {
		store.Set(authKey(authRole, addr), []byte{byte(role)})
	}
	for key, value := range cfg.governed {
		store.Set(authKey(authLimit, key), be(value))
	}
	store.Set(authKey(authNumeric, leases.TermDecisionID), be(uint64(termSeconds)))
	for _, decision := range []string{content.HostingDecisionID, "DEC-002", "DEC-003"} {
		store.Set(authKey(authDevnetRole, decision), []byte(content.DevnetRoleName))
	}
	store.Set([]byte{authPolicy}, be(1))
}

// config is one sequence's chain configuration.
type config struct {
	governed map[string]uint64
	// trialCheck compares the trial branch of every rejected or idempotent
	// message with its pre-state, and the other modules' stores before and
	// after every accepted audit batch (I9); invariants runs the lease and
	// evidence invariant checkers after every step. The benchmarks turn
	// both off.
	trialCheck bool
	invariants bool
}

// eventRef is one typed event: its svn.events.v1 type without the package
// prefix and the id it reports (the first of lease_id, offer_id,
// provider_id, content_id, evidence_id it carries).
type eventRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// outcome is the Go side of one step.
type outcome struct {
	code       msgkit.Code
	idempotent bool
	events     []eventRef
	// states is the state attribute of every evidence_state event, in order
	// (deviation D-16).
	states []string
	// released is the number of leases a block ended and offers it closed.
	released int
	gas      uint64
	// eventBytes is the event type, attribute key, and attribute value bytes
	// of every event of an accepted step (the measurements log it).
	eventBytes int
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
	leaseRead   leases.Keeper
	lease       leases.Keeper
	evid        evidence.Keeper
	now         int64
	height      int64
	baseline    map[string]uint64
	// intercept wraps message routing; only the mutant tests set it.
	intercept func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error
}

// newHarness wires the keepers as chain/app does: x/evidence reads leases
// through a copy of the lease keeper without the evidence reader, and the
// lease keeper the router and the content withdrawal hook use reads
// evidence for renewals. Business ids are one namespace across the four
// modules (msgkit.Peers).
func newHarness(tb testing.TB, cfg *config) *harness {
	tb.Helper()
	ctx, keys := msgkittest.NewContext(tb, authorityStore, providers.ModuleName, content.ModuleName,
		leases.ModuleName, evidence.ModuleName)
	h := &harness{
		tb:       tb,
		cfg:      cfg,
		auth:     fakeAuthority{key: keys[authorityStore]},
		now:      startTime,
		height:   1,
		baseline: map[string]uint64{},
		keys: []*storetypes.KVStoreKey{
			keys[authorityStore], keys[providers.ModuleName], keys[content.ModuleName], keys[leases.ModuleName],
			keys[evidence.ModuleName],
		},
	}
	h.ctx = ctx.WithBlockTime(time.Unix(h.now, 0).UTC()).WithBlockHeight(h.height).
		WithGasMeter(storetypes.NewInfiniteGasMeter())
	h.auth.write(h.ctx, cfg)

	peers := msgkit.NewPeers()
	h.prov = providers.NewKeeper(keys[providers.ModuleName], providers.Deps{Roles: h.auth, Limits: h.auth, Peers: peers})
	h.contentRead = content.NewKeeper(keys[content.ModuleName], content.Deps{Roles: h.auth, Params: h.auth, Limits: h.auth, Peers: peers})
	leaseDeps := leases.Deps{
		Roles: h.auth, Limits: h.auth, Params: h.auth, Policy: h.auth,
		Providers: h.prov, Content: h.contentRead, Peers: peers,
	}
	h.leaseRead = leases.NewKeeper(keys[leases.ModuleName], leaseDeps)
	h.evid = evidence.NewKeeper(keys[evidence.ModuleName], evidence.Deps{
		Roles: h.auth, Limits: h.auth, Leases: h.leaseRead, Content: h.contentRead, Peers: peers,
	})
	leaseDeps.Evidence = h.evid
	h.lease = leases.NewKeeper(keys[leases.ModuleName], leaseDeps)
	h.cont = content.NewKeeper(keys[content.ModuleName], content.Deps{
		Roles: h.auth, Params: h.auth, Limits: h.auth, Peers: peers, Hook: h.lease,
	})
	peers.Add(providers.ModuleName, h.prov)
	peers.Add(content.ModuleName, h.cont)
	peers.Add(leases.ModuleName, h.lease)
	peers.Add(evidence.ModuleName, h.evid)

	if err := h.prov.InitGenesis(h.ctx, &providersv1.GenesisState{}); err != nil {
		tb.Fatalf("providers genesis: %v", err)
	}
	if err := h.cont.InitGenesis(h.ctx, &contentv1.GenesisState{}); err != nil {
		tb.Fatalf("content genesis: %v", err)
	}
	if err := h.lease.InitGenesis(h.ctx, &leasesv1.GenesisState{}); err != nil {
		tb.Fatalf("leases genesis: %v", err)
	}
	if err := h.evid.InitGenesis(h.ctx, &evidencev1.GenesisState{}); err != nil {
		tb.Fatalf("evidence genesis: %v", err)
	}
	return h
}

// unmetered is the current block context without a gas limit, for reads
// the harness makes outside a transaction.
func (h *harness) unmetered() sdk.Context {
	return h.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
}

func (h *harness) snapshot(ctx sdk.Context, keys ...*storetypes.KVStoreKey) []byte {
	if len(keys) == 0 {
		keys = h.keys
	}
	return evidencetest.StoreSnapshot(ctx, keys...)
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
	switch s.op {
	case fxBlock:
		out = h.block(s.headerTime)
	case fxAdmit, fxGoverned:
		out = h.council(s)
	default:
		out = h.deliver(s)
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
		h.tb.Fatalf("after %s: leases: %v", what, err)
	}
	if err := leasestest.CheckOfferIntegrity(ctx, h.lease, h.contentRead); err != nil {
		h.tb.Fatalf("after %s: leases: %v", what, err)
	}
	if err := evidencetest.CheckInvariants(ctx, h.evid, h.leaseRead); err != nil {
		h.tb.Fatalf("after %s: evidence: %v", what, err)
	}
}

// deliver runs one transaction message as the SDK's FinalizeBlock does: a
// fresh event manager and gas meter, a cached branch that is written back
// only when the handler succeeds. A rejected or idempotent message must
// leave the trial branch byte-identical to its pre-state. An accepted audit
// batch must leave the authority, providers, content, and leases stores
// byte-identical (I9: x/evidence writes only its own store).
func (h *harness) deliver(s *step) outcome {
	h.tb.Helper()
	txBytes := txBytesLen
	if s.op == opAudit {
		txBytes = proto.Size(s.msg)
	}
	ctx := h.ctx.WithEventManager(sdk.NewEventManager()).
		WithGasMeter(storetypes.NewGasMeter(txGasLimit)).
		WithTxBytes(make([]byte, txBytes))
	cache, write := ctx.CacheContext()
	var pre, preOthers []byte
	if h.cfg.trialCheck {
		pre = h.snapshot(cache)
		preOthers = h.snapshot(cache, h.keys[:keyEvidence]...)
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
	switch {
	case !h.cfg.trialCheck:
	case out.idempotent:
		// The whole trial branch, which covers I9's stores too.
		if !bytes.Equal(pre, h.snapshot(cache)) {
			h.tb.Errorf("idempotent %s changed the trial state", s.op)
		}
	case s.op == opAudit:
		if !bytes.Equal(preOthers, h.snapshot(cache, h.keys[:keyEvidence]...)) {
			h.tb.Errorf("I9: an accepted audit batch changed another module's store")
		}
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
	case *evidencev1.MsgSubmitAuditBatch:
		_, err = h.evid.SubmitAuditBatch(ctx, m)
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
// BeginBlock body (ExpireDue). x/evidence has no BeginBlock (deviation
// D-4). It is the only place the harness moves the clock: every later
// transaction sees t as its committed time.
func (h *harness) block(t int64) outcome {
	h.tb.Helper()
	if t <= h.now {
		h.tb.Fatalf("block time %d is not after %d", t, h.now)
	}
	// The previous block's writes are committed first, as the SDK commits
	// after every block; the stores then keep no unsaved writes across
	// blocks (an IAVL iterator sorts every unsaved write when it opens).
	h.ctx.MultiStore().(storetypes.CommitMultiStore).Commit()
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

// council applies a council fixture: a provider admission the way
// x/authority executes it (the hook runs in a cached branch that is written
// back only on OK), or a governed limit change, which writes the fake
// authority's governed limit row (x/authority writes its own row).
func (h *harness) council(s *step) outcome {
	h.tb.Helper()
	ctx := h.ctx.WithEventManager(sdk.NewEventManager()).WithGasMeter(storetypes.NewGasMeter(txGasLimit))
	cache, write := ctx.CacheContext()
	var pre []byte
	if h.cfg.trialCheck {
		pre = h.snapshot(cache)
	}
	code := msgkit.OK
	switch s.op {
	case fxAdmit:
		code = h.prov.AdmitByCouncil(cache, s.id, true)
	case fxGoverned:
		ceiling, ok := msgkit.Ceiling(s.id)
		if !ok || s.value < 1 || s.value > ceiling {
			h.tb.Fatalf("governed limit fixture %s=%d outside [1, ceiling]", s.id, s.value)
		}
		cache.KVStore(h.keys[keyAuthority]).Set(authKey(authLimit, s.id), be(s.value))
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

// eventIDFields is the order in which an event's reported id is chosen
// (spec_oracle.py EVENT_ID_FIELDS).
var eventIDFields = []protoreflect.Name{"lease_id", "offer_id", "provider_id", "content_id", "evidence_id"}

// parseEvents fills out.events with the typed svn.events.v1 events (parsed
// with msgkit.ParseTyped, not string scraping), out.states with the state of
// every evidence_state event, and out.released with the leases ended and
// offers closed. Every EvidenceAccepted must be followed by the
// evidence_state event of the same evidence id (deviation D-16).
func (h *harness) parseEvents(out *outcome, events sdk.Events) {
	h.tb.Helper()
	out.events = []eventRef{}
	pending := ""
	for _, ev := range events {
		out.eventBytes += len(ev.Type)
		for _, attr := range ev.Attributes {
			out.eventBytes += len(attr.Key) + len(attr.Value)
		}
		switch {
		case strings.HasPrefix(ev.Type, "svn.events.v1."):
			if pending != "" {
				h.tb.Fatalf("EvidenceAccepted %q has no evidence_state event after it", pending)
			}
			msg, err := msgkit.ParseTyped(abci.Event(ev))
			if err != nil {
				h.tb.Fatalf("typed event %s does not parse: %v", ev.Type, err)
			}
			ref := eventRef{Type: strings.TrimPrefix(ev.Type, "svn.events.v1."), ID: eventID(msg)}
			out.events = append(out.events, ref)
			switch ref.Type {
			case "LeaseEnded":
				out.released++
			case "EvidenceAccepted":
				pending = ref.ID
			}
		case ev.Type == leases.EventOfferClosed:
			out.released++
		case ev.Type == evidence.EventState:
			if len(ev.Attributes) != 3 || ev.Attributes[0].Key != evidence.AttrEvidenceID ||
				ev.Attributes[1].Key != evidence.AttrLeaseID || ev.Attributes[2].Key != evidence.AttrState {
				h.tb.Fatalf("evidence_state attributes %v", ev.Attributes)
			}
			if ev.Attributes[0].Value != pending {
				h.tb.Fatalf("evidence_state for %q follows EvidenceAccepted %q", ev.Attributes[0].Value, pending)
			}
			pending = ""
			out.states = append(out.states, ev.Attributes[2].Value)
		}
	}
	if pending != "" {
		h.tb.Fatalf("EvidenceAccepted %q has no evidence_state event after it", pending)
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

// recordView is one evidence record with its reported state.
type recordView struct {
	stored   *evidencev1.EvidenceRecord
	reported evidencev1.EvidenceState
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
	pairs       map[string]string
	pairRows    []pairView
	records     map[string]recordView
	recordIDs   []string
	batches     map[string]bool
	batchIDs    []string
	// windows maps an auditor address to its window row (epoch, count).
	windows     map[string][2]uint64
	businessIDs []string
	maxBatch    uint64
	maxExpiry   uint64
}

func pairKey(providerID, contentID string, version uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", providerID, contentID, version)
}

func (h *harness) view() *view {
	h.tb.Helper()
	ctx := h.unmetered()
	v := &view{
		now:        h.now,
		providers:  map[string]*providersv1.Provider{},
		contents:   map[string]*contentv1.Content{},
		tombstones: map[string]*contentv1.Tombstone{},
		offers:     map[string]*leasesv1.Offer{},
		leases:     map[string]*leasesv1.Lease{},
		pairs:      map[string]string{},
		records:    map[string]recordView{},
		batches:    map[string]bool{},
		windows:    map[string][2]uint64{},
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
	}
	for _, l := range leaseGenesis.GetLeases() {
		v.leases[l.GetLeaseId()] = l
		v.leaseIDs = append(v.leaseIDs, l.GetLeaseId())
	}
	leaseStore := ctx.MultiStore().GetKVStore(h.keys[keyLeases])
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
	h.readEvidence(ctx, v)
	v.businessIDs = h.businessIDs(ctx)
	limits := func(key string) uint64 {
		governed, found := h.auth.GovernedLimit(ctx, key)
		return msgkit.Effective(key, governed, found)
	}
	v.maxBatch = limits(msgkit.LimitMaxEvidenceBatch)
	v.maxExpiry = limits(msgkit.LimitMaxExpirySteps)
	return v
}

// readEvidence reads the evidence store: every record as stored and with
// the state the module's export reports at the view's time, the batch
// markers, and the auditor windows.
func (h *harness) readEvidence(ctx sdk.Context, v *view) {
	h.tb.Helper()
	exported, err := h.evid.ExportGenesis(ctx)
	if err != nil {
		h.tb.Fatalf("evidence export: %v", err)
	}
	store := ctx.MultiStore().GetKVStore(h.keys[keyEvidence])
	for _, report := range exported.GetRecords() {
		id := report.GetEvidenceId()
		var stored evidencev1.EvidenceRecord
		if err := proto.Unmarshal(store.Get(evidence.RecordKey(id)), &stored); err != nil {
			h.tb.Fatalf("record %q does not decode: %v", id, err)
		}
		v.records[id] = recordView{stored: &stored, reported: report.GetState()}
		v.recordIDs = append(v.recordIDs, id)
	}
	eachRow(store, evidence.BatchPrefix, func(key, _ []byte) {
		v.batches[string(key[1:])] = true
		v.batchIDs = append(v.batchIDs, string(key[1:]))
	})
	eachRow(store, evidence.AuditorWindowPrefix, func(key, value []byte) {
		epoch, count, err := evidence.DecodeWindow(value)
		if err != nil {
			h.tb.Fatalf("window row %x: %v", key, err)
		}
		v.windows[string(key[1:])] = [2]uint64{epoch, count}
	})
}

// businessIDs is the union of the providers, content, leases, and evidence
// ledgers. The protocol has one business-id namespace, so an id in two
// ledgers is a failure.
func (h *harness) businessIDs(ctx sdk.Context) []string {
	h.tb.Helper()
	seen := map[string]string{}
	ledgers := []struct {
		name   string
		key    *storetypes.KVStoreKey
		prefix byte
	}{
		{providers.ModuleName, h.keys[keyProviders], providers.BusinessKeyPrefix},
		{content.ModuleName, h.keys[keyContent], content.BusinessPrefix},
		{leases.ModuleName, h.keys[keyLeases], leases.BusinessPrefix},
		{evidence.ModuleName, h.keys[keyEvidence], evidence.BusinessPrefix},
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

// epochOf is the audit round of a committed time (the harness's own
// computation of evidence.EpochAt for protocol times).
func epochOf(t int64) uint64 { return uint64(t / epochSeconds) }

// heldLeases is the held leases of the view, in id order.
func (v *view) heldLeases() []string {
	var out []string
	for _, id := range v.leaseIDs {
		if v.leases[id].GetReservationHeld() {
			out = append(out, id)
		}
	}
	return out
}

// windowCount is the auditor's items in the view's current epoch.
func (v *view) windowCount(addr []byte) uint64 {
	w, ok := v.windows[string(addr)]
	if !ok || w[0] != epochOf(v.now) {
		return 0
	}
	return w[1]
}
