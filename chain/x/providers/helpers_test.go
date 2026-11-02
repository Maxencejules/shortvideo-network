package providers

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"testing"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Synthetic 20-byte accounts. No real keys or addresses.
var (
	providerAcct    = bytes.Repeat([]byte{0x11}, 20)
	provider2Acct   = bytes.Repeat([]byte{0x22}, 20)
	serviceAcct     = bytes.Repeat([]byte{0x33}, 20)
	broadcasterAcct = bytes.Repeat([]byte{0x44}, 20)
	outsiderAcct    = bytes.Repeat([]byte{0x55}, 20)
)

// Store names mounted by every test context. "authority" and "content"
// stand in for the other modules' stores, so tests can prove a provider
// message touches only the providers store.
var otherStores = []string{"authority", "content"}

// fakeRoles is the authority role read surface (unit-test fake).
type fakeRoles struct {
	byAddr map[string]typesv1.SubmitterRole
}

func (f *fakeRoles) Role(_ sdk.Context, addr []byte) typesv1.SubmitterRole {
	return f.byAddr[string(addr)]
}

// fakeLimits is the authority governed-limit read surface (unit-test fake).
type fakeLimits struct{ values map[string]uint64 }

func (f *fakeLimits) GovernedLimit(_ sdk.Context, key string) (uint64, bool) {
	v, ok := f.values[key]
	return v, ok
}

// fakeLedger is another module's business-id ledger (unit-test fake).
type fakeLedger struct{ digests map[string][]byte }

func (f *fakeLedger) BusinessDigest(_ sdk.Context, id string) ([]byte, bool) {
	d, ok := f.digests[id]
	return append([]byte(nil), d...), ok
}

type env struct {
	t      testing.TB
	ctx    sdk.Context
	keys   map[string]*storetypes.KVStoreKey
	k      Keeper
	roles  *fakeRoles
	limits *fakeLimits
	peer   *fakeLedger
}

func newEnv(t testing.TB) *env {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, append([]string{ModuleName}, otherStores...)...)
	roles := &fakeRoles{byAddr: map[string]typesv1.SubmitterRole{
		string(providerAcct):    typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
		string(provider2Acct):   typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
		string(serviceAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
		string(broadcasterAcct): typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
	}}
	limits := &fakeLimits{values: map[string]uint64{}}
	peers := msgkit.NewPeers()
	peer := &fakeLedger{digests: map[string][]byte{}}
	k := NewKeeper(keys[ModuleName], Deps{Roles: roles, Limits: limits, Peers: peers})
	peers.Add("authority", peer)
	peers.Add(ModuleName, k)
	return &env{t: t, ctx: ctx, keys: keys, k: k, roles: roles, limits: limits, peer: peer}
}

// fresh is the context of one message: a new gas meter at the test limit
// and a new event manager. Writes go straight to the store (no cache
// branch), so a handler that wrote before failing would be visible.
func (e *env) fresh() sdk.Context {
	return e.ctx.WithGasMeter(storetypes.NewGasMeter(msgkittest.GasLimit)).WithEventManager(sdk.NewEventManager())
}

// unmetered is a context for fixture seeding.
func (e *env) unmetered() sdk.Context {
	return e.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
}

func (e *env) store(name string) storetypes.KVStore {
	return e.ctx.MultiStore().GetKVStore(e.keys[name])
}

// snapshot is every mounted store, in name order.
func (e *env) snapshot() map[string][]byte {
	out := map[string][]byte{}
	for name := range e.keys {
		out[name] = msgkit.StoreSnapshot(e.store(name))
	}
	return out
}

func (e *env) requireSameSnapshot(before map[string][]byte, what string) {
	e.t.Helper()
	after := e.snapshot()
	names := make([]string, 0, len(before))
	for name := range before {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !bytes.Equal(before[name], after[name]) {
			e.t.Fatalf("%s: store %q changed", what, name)
		}
	}
}

// result is the observable outcome of one message.
type result struct {
	code       msgkit.Code
	idempotent bool
	events     []abci.Event
}

// typed returns the svn.events.v1 events.
func (r result) typed() []abci.Event {
	var out []abci.Event
	for _, ev := range r.events {
		if len(ev.Type) > len("svn.events.v1.") && ev.Type[:len("svn.events.v1.")] == "svn.events.v1." {
			out = append(out, ev)
		}
	}
	return out
}

// codeOf maps a handler error to its protocol code through the ABCI info,
// the same way a FinalizeBlock result carries it: codespace "svn" and the
// svn.types.v1.ErrorCode number.
func codeOf(t testing.TB, err error) msgkit.Code {
	t.Helper()
	if err == nil {
		return msgkit.OK
	}
	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	if codespace != svnerr.ModuleName {
		t.Fatalf("error %v has codespace %q, want %q", err, codespace, svnerr.ModuleName)
	}
	name, ok := typesv1.ErrorCode_name[int32(code)]
	if !ok || code == 0 {
		t.Fatalf("error %v has code %d outside svn.types.v1.ErrorCode", err, code)
	}
	return msgkit.Code(name)
}

func (e *env) finish(ctx sdk.Context, err error) result {
	e.t.Helper()
	r := result{code: codeOf(e.t, err), events: ctx.EventManager().ABCIEvents()}
	for _, ev := range r.events {
		if ev.Type == ModuleName+"_result" {
			r.idempotent = true
		}
	}
	return r
}

func (e *env) register(ctx sdk.Context, msg *providersv1.MsgRegisterProvider) result {
	_, err := e.k.RegisterProvider(ctx, msg)
	return e.finish(ctx, err)
}

func (e *env) update(ctx sdk.Context, msg *providersv1.MsgUpdateProvider) result {
	_, err := e.k.UpdateProvider(ctx, msg)
	return e.finish(ctx, err)
}

func (e *env) rotate(ctx sdk.Context, msg *providersv1.MsgRotateProviderKey) result {
	_, err := e.k.RotateProviderKey(ctx, msg)
	return e.finish(ctx, err)
}

func (e *env) drain(ctx sdk.Context, msg *providersv1.MsgDrainProvider) result {
	_, err := e.k.DrainProvider(ctx, msg)
	return e.finish(ctx, err)
}

func (e *env) mustOK(r result, what string) {
	e.t.Helper()
	if r.code != msgkit.OK {
		e.t.Fatalf("%s: code %s, want success", what, r.code)
	}
}

func (e *env) record(id string) *providersv1.Provider {
	e.t.Helper()
	record, found := e.k.Provider(e.unmetered(), id)
	if !found {
		e.t.Fatalf("provider %q not found", id)
	}
	return record
}

// seed writes provider records through InitGenesis on an empty store.
func (e *env) seed(records ...*providersv1.Provider) {
	e.t.Helper()
	if err := e.k.InitGenesis(e.unmetered(), &providersv1.GenesisState{Providers: records}); err != nil {
		e.t.Fatalf("seed: %v", err)
	}
}

// Deterministic ed25519 test keys. A key id is also the key name.
func testPriv(name string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-providers-test-key/" + name))
	return ed25519.NewKeyFromSeed(seed[:])
}

func testPub(name string) []byte {
	return append([]byte(nil), testPriv(name).Public().(ed25519.PublicKey)...)
}

func digestOf(label string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(label))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

func regMsg(id string) *providersv1.MsgRegisterProvider {
	return &providersv1.MsgRegisterProvider{
		Signer:                 providerAcct,
		BusinessId:             "reg-" + id,
		ProviderId:             id,
		SigningPubkey:          testPub("key-" + id),
		SigningKeyId:           "key-" + id,
		EndpointManifestDigest: digestOf("endpoint/" + id),
		DeclaredCapacityBytes:  1_000_000,
		OperatorGroupId:        DevnetOperatorGroup,
	}
}

func updMsg(id, business string, capacity uint64) *providersv1.MsgUpdateProvider {
	return &providersv1.MsgUpdateProvider{
		Signer:                 providerAcct,
		BusinessId:             business,
		ProviderId:             id,
		EndpointManifestDigest: digestOf(fmt.Sprintf("endpoint/%s/%d", id, capacity)),
		DeclaredCapacityBytes:  capacity,
	}
}

func innerSig(domain typesv1.SignatureDomain, keyName string, message []byte) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  domain,
		PublicKey:               testPub(keyName),
		Signature:               ed25519.Sign(testPriv(keyName), message),
		KeyId:                   keyName,
		CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}
}

// rotMsg rotates id from the key named current to the key named next,
// signed by current over the canonical rotation payload.
func rotMsg(t testing.TB, id, current, next string, expected uint64) *providersv1.MsgRotateProviderKey {
	t.Helper()
	msg := &providersv1.MsgRotateProviderKey{
		Signer:             providerAcct,
		BusinessId:         fmt.Sprintf("rot-%s-%s", id, next),
		ProviderId:         id,
		NewSigningPubkey:   testPub(next),
		NewSigningKeyId:    next,
		ExpectedGeneration: expected,
	}
	signRotate(t, msg, current)
	return msg
}

func signRotate(t testing.TB, msg *providersv1.MsgRotateProviderKey, keyName string) {
	t.Helper()
	signBytes, err := RotateSignBytes(msg.GetProviderId(), msg.GetNewSigningKeyId(), msg.GetNewSigningPubkey(), msg.GetExpectedGeneration())
	if err != nil {
		t.Fatalf("rotate sign bytes: %v", err)
	}
	msg.CurrentKeySignature = innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE, keyName, signBytes)
}

// drainMsg drains id, signed by the key named keyName at generation gen.
func drainMsg(t testing.TB, id, keyName string, gen uint64) *providersv1.MsgDrainProvider {
	t.Helper()
	signBytes, err := DrainSignBytes(id, gen)
	if err != nil {
		t.Fatalf("drain sign bytes: %v", err)
	}
	return &providersv1.MsgDrainProvider{
		Signer:            providerAcct,
		BusinessId:        "drain-" + id,
		ProviderId:        id,
		ProviderSignature: innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN, keyName, signBytes),
	}
}

// registered registers id and returns its record.
func (e *env) registered(id string) *providersv1.Provider {
	e.t.Helper()
	e.mustOK(e.register(e.fresh(), regMsg(id)), "register "+id)
	return e.record(id)
}

// admitted registers and admits id through the council hook.
func (e *env) admitted(id string) *providersv1.Provider {
	e.t.Helper()
	e.registered(id)
	if code := e.k.AdmitByCouncil(e.fresh(), id, true); code != msgkit.OK {
		e.t.Fatalf("admit %s: %s", id, code)
	}
	return e.record(id)
}

// seededProvider is a valid genesis record.
func seededProvider(id string, state providersv1.ProviderState, capacity, reserved uint64) *providersv1.Provider {
	return &providersv1.Provider{
		ProviderId:             id,
		Account:                append([]byte(nil), providerAcct...),
		SigningPubkey:          testPub("key-" + id),
		SigningKeyId:           "key-" + id,
		KeyGeneration:          1,
		EndpointManifestDigest: digestOf("endpoint/" + id),
		State:                  state,
		AdmittedCapacityBytes:  capacity,
		ReservedBytes:          reserved,
		OperatorGroupId:        DevnetOperatorGroup,
	}
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }

func newGasMeter(limit uint64) storetypes.GasMeter { return storetypes.NewGasMeter(limit) }
