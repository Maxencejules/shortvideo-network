package content

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"testing"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Synthetic 20-byte accounts. None is a real key.
var (
	serviceAddr   = bytes.Repeat([]byte{0x11}, 20)
	service2Addr  = bytes.Repeat([]byte{0x12}, 20)
	providerAddr  = bytes.Repeat([]byte{0x21}, 20)
	authorityAddr = bytes.Repeat([]byte{0x31}, 20)
	outsiderAddr  = bytes.Repeat([]byte{0x99}, 20)
)

// fakeRoles, fakeParams, fakeLimits, and recordingHook stand in for
// x/authority and the goal-08 lease module in unit tests only.
type fakeRoles struct {
	roles map[string]typesv1.SubmitterRole
}

func (f *fakeRoles) Role(_ sdk.Context, addr []byte) typesv1.SubmitterRole {
	return f.roles[string(addr)]
}

type fakeParams struct {
	roles map[string]string
}

func (f *fakeParams) DevnetRole(_ sdk.Context, decisionID string) (string, bool) {
	role, ok := f.roles[decisionID]
	return role, ok
}

type fakeLimits struct {
	values map[string]uint64
}

func (f *fakeLimits) GovernedLimit(_ sdk.Context, key string) (uint64, bool) {
	v, ok := f.values[key]
	return v, ok
}

type hookCall struct {
	id      string
	version uint64
}

type recordingHook struct {
	calls []hookCall
	err   error
	// write, when set, is written to the content store by the hook before
	// it returns err, so tests can prove a failed hook leaves nothing.
	write []byte
	key   *storetypes.KVStoreKey
}

func (h *recordingHook) ContentWithdrawn(ctx sdk.Context, id string, version uint64) error {
	h.calls = append(h.calls, hookCall{id: id, version: version})
	if h.write != nil {
		ctx.KVStore(h.key).Set(h.write, []byte{1})
	}
	return h.err
}

// fakeSource is another module's business-id ledger.
type fakeSource map[string][]byte

func (f fakeSource) BusinessDigest(_ sdk.Context, id string) ([]byte, bool) {
	d, ok := f[id]
	return d, ok
}

type fixture struct {
	t      testing.TB
	ctx    sdk.Context
	keys   map[string]*storetypes.KVStoreKey
	keeper Keeper
	roles  *fakeRoles
	params *fakeParams
	limits *fakeLimits
	hook   *recordingHook
	peer   fakeSource
	intent ed25519.PrivateKey
}

type fixtureOptions struct {
	noHook bool
	extra  []string
}

// seedKey derives a deterministic synthetic ed25519 key from a label. It is
// a test key only.
func seedKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn content unit test key: " + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func newFixture(t testing.TB) *fixture {
	return newFixtureWith(t, fixtureOptions{})
}

// newFixtureWith mounts a real IAVL content store (and any extra stores),
// admits serviceAddr and service2Addr as ADMITTED_SERVICE, providerAddr as
// ADMITTED_PROVIDER and authorityAddr as AUTHORITY, enables the DEC-001
// devnet role, and wires a fake "authority" business-id peer.
func newFixtureWith(t testing.TB, opts fixtureOptions) *fixture {
	t.Helper()
	names := append([]string{ModuleName}, opts.extra...)
	ctx, keys := msgkittest.NewContext(t, names...)
	f := &fixture{
		t: t,
		// Reads in tests (queries, exports, snapshots of records) use an
		// unlimited meter; every message gets its own limited meter from tx.
		ctx:  ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()),
		keys: keys,
		roles: &fakeRoles{roles: map[string]typesv1.SubmitterRole{
			string(serviceAddr):   typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
			string(service2Addr):  typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
			string(providerAddr):  typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
			string(authorityAddr): typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY,
		}},
		params: &fakeParams{roles: map[string]string{HostingDecisionID: DevnetRoleName}},
		limits: &fakeLimits{values: map[string]uint64{}},
		peer:   fakeSource{},
		intent: seedKey("hosting intent"),
	}
	peers := msgkit.NewPeers()
	deps := Deps{Roles: f.roles, Params: f.params, Limits: f.limits, Peers: peers}
	if !opts.noHook {
		f.hook = &recordingHook{key: keys[ModuleName]}
		deps.Hook = f.hook
	}
	f.keeper = NewKeeper(keys[ModuleName], deps)
	peers.Add("authority", f.peer)
	peers.Add(ModuleName, f.keeper)
	return f
}

// tx returns a context for one message: a fresh gas meter and event
// manager on the shared multistore, like a new DeliverTx.
func (f *fixture) tx() sdk.Context {
	return f.ctx.WithGasMeter(storetypes.NewGasMeter(msgkittest.GasLimit)).WithEventManager(sdk.NewEventManager())
}

func manifestDigest(label string) []byte {
	sum := sha256.Sum256([]byte("synthetic manifest: " + label))
	return sum[:]
}

func reasonDigest(label string) []byte {
	sum := sha256.Sum256([]byte("synthetic reason: " + label))
	return sum[:]
}

func digestOf(value []byte) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: append([]byte(nil), value...)}
}

const testPackageBytes uint64 = 4096

// sign returns a hosting-intent signature over message bytes by key.
func sign(key ed25519.PrivateKey, keyID string, message []byte) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  hostingDomain,
		PublicKey:               append([]byte(nil), key.Public().(ed25519.PublicKey)...),
		Signature:               ed25519.Sign(key, message),
		KeyId:                   keyID,
		CanonicalProfileVersion: 1,
	}
}

// registerMsg is a valid RegisterContent from serviceAddr.
func (f *fixture) registerMsg(businessID, contentID string) *contentv1.MsgRegisterContent {
	msg := &contentv1.MsgRegisterContent{
		Signer:         append([]byte(nil), serviceAddr...),
		BusinessId:     businessID,
		ContentId:      contentID,
		ContentVersion: 1,
		ManifestDigest: digestOf(manifestDigest(contentID)),
		PackageBytes:   testPackageBytes,
	}
	f.signRegister(msg, DevnetRoleName, f.intent)
	return msg
}

// signRegister replaces the intent with one over msg's current fields.
func (f *fixture) signRegister(msg *contentv1.MsgRegisterContent, role string, key ed25519.PrivateKey) {
	f.t.Helper()
	bz, err := RegisterIntentSignBytes(msg.GetSigner(), msg.GetContentId(), msg.GetContentVersion(),
		msg.GetManifestDigest().GetValue(), msg.GetPackageBytes(), role)
	if err != nil {
		f.t.Fatalf("register sign bytes: %v", err)
	}
	msg.HostingIntent = sign(key, "intent-key", bz)
}

// withdrawMsg is a valid WithdrawContent from serviceAddr.
func (f *fixture) withdrawMsg(businessID, contentID string) *contentv1.MsgWithdrawContent {
	msg := &contentv1.MsgWithdrawContent{
		Signer:         append([]byte(nil), serviceAddr...),
		BusinessId:     businessID,
		ContentId:      contentID,
		ContentVersion: 1,
		ReasonDigest:   digestOf(reasonDigest(contentID)),
	}
	f.signWithdraw(msg, DevnetRoleName, f.intent)
	return msg
}

func (f *fixture) signWithdraw(msg *contentv1.MsgWithdrawContent, role string, key ed25519.PrivateKey) {
	f.t.Helper()
	bz, err := WithdrawIntentSignBytes(msg.GetSigner(), msg.GetContentId(), msg.GetContentVersion(),
		msg.GetReasonDigest().GetValue(), role)
	if err != nil {
		f.t.Fatalf("withdraw sign bytes: %v", err)
	}
	msg.HostingIntent = sign(key, "intent-key", bz)
}

func (f *fixture) register(msg *contentv1.MsgRegisterContent) (sdk.Context, error) {
	ctx := f.tx()
	_, err := f.keeper.RegisterContent(ctx, msg)
	return ctx, err
}

func (f *fixture) withdraw(msg *contentv1.MsgWithdrawContent) (sdk.Context, error) {
	ctx := f.tx()
	_, err := f.keeper.WithdrawContent(ctx, msg)
	return ctx, err
}

func (f *fixture) force(id string, version uint64, reason []byte, devnet bool) (sdk.Context, msgkit.Code) {
	ctx := f.tx()
	return ctx, f.keeper.ForceWithdrawByCouncil(ctx, id, version, reason, devnet)
}

// mustRegister registers contentID from serviceAddr or fails the test.
func (f *fixture) mustRegister(businessID, contentID string) {
	f.t.Helper()
	if _, err := f.register(f.registerMsg(businessID, contentID)); err != nil {
		f.t.Fatalf("register %s: %v", contentID, err)
	}
}

func (f *fixture) mustWithdraw(businessID, contentID string) {
	f.t.Helper()
	if _, err := f.withdraw(f.withdrawMsg(businessID, contentID)); err != nil {
		f.t.Fatalf("withdraw %s: %v", contentID, err)
	}
}

// snapshot is every key/value of the content store, read without gas.
func (f *fixture) snapshot() []byte {
	return msgkit.StoreSnapshot(f.ctx.MultiStore().GetKVStore(f.keys[ModuleName]))
}

// allSnapshot is every mounted store in name order.
func (f *fixture) allSnapshot() []byte {
	names := make([]string, 0, len(f.keys))
	for name := range f.keys {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []byte
	for _, name := range names {
		out = append(out, name...)
		out = append(out, 0)
		out = append(out, msgkit.StoreSnapshot(f.ctx.MultiStore().GetKVStore(f.keys[name]))...)
	}
	return out
}

func (f *fixture) record(id string) *contentv1.Content {
	f.t.Helper()
	c, ok := f.keeper.content(f.ctx, id, 1)
	if !ok {
		f.t.Fatalf("content %s missing", id)
	}
	return c
}

// requireCode asserts an ABCI result: nil for OK, otherwise codespace svn
// and the numeric protocol code of want.
func requireCode(t testing.TB, err error, want msgkit.Code) {
	t.Helper()
	got := codeFromErr(t, err)
	if got != want {
		t.Fatalf("code = %q (err %v), want %q", got, err, want)
	}
}

func codeFromErr(t testing.TB, err error) msgkit.Code {
	t.Helper()
	if err == nil {
		return msgkit.OK
	}
	space, number, _ := errorsmod.ABCIInfo(err, false)
	if space != svnerr.ModuleName {
		t.Fatalf("error %v has codespace %q, want %q", err, space, svnerr.ModuleName)
	}
	name, ok := typesv1.ErrorCode_name[int32(number)]
	if !ok || number == 0 {
		t.Fatalf("error %v has code %d outside the protocol enum", err, number)
	}
	return msgkit.Code(name)
}

func requireSame(t testing.TB, before, after []byte, what string) {
	t.Helper()
	if !bytes.Equal(before, after) {
		t.Fatalf("%s: store changed (%d -> %d bytes)", what, len(before), len(after))
	}
}

// typedEvents parses every svn.events.v1 event of ctx.
func typedEvents(t testing.TB, ctx sdk.Context) []proto.Message {
	t.Helper()
	var out []proto.Message
	for _, ev := range ctx.EventManager().ABCIEvents() {
		if !strings.HasPrefix(ev.Type, "svn.events.v1.") {
			continue
		}
		m, err := msgkit.ParseTyped(ev)
		if err != nil {
			t.Fatalf("parse %s: %v", ev.Type, err)
		}
		out = append(out, m)
	}
	return out
}

func idempotentEvent(ctx sdk.Context) bool {
	for _, ev := range ctx.EventManager().ABCIEvents() {
		if ev.Type == ModuleName+"_result" && len(ev.Attributes) == 1 &&
			ev.Attributes[0].Key == "idempotent" && ev.Attributes[0].Value == "true" {
			return true
		}
	}
	return false
}

func noEvents(t testing.TB, ctx sdk.Context, what string) {
	t.Helper()
	if n := len(ctx.EventManager().ABCIEvents()); n != 0 {
		t.Fatalf("%s: %d events emitted, want none", what, n)
	}
}

var errPlainHook = errors.New("synthetic hook failure")
