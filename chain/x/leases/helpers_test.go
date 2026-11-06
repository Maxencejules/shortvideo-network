package leases_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"math"
	"strings"
	"testing"
	"time"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases/leasestest"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Unit-test fakes. They stand in for x/authority, x/providers, x/content,
// and x/evidence, which this package never imports (TestKeeperImports).
// The provider and content fakes keep their records in real IAVL stores
// mounted next to the leases store, so a handler run on a cached branch
// (ctx.CacheContext) changes them only in that branch, exactly as the real
// keepers' writes behave, and a trial snapshot covers them. The provider
// fake verifies signatures with the real msgkit.VerifyInner. None of this
// is end-to-end evidence: the real keepers run in chain/x/leases/difftest
// and chain/app.

// Synthetic 20-byte accounts. No real keys or addresses.
var (
	providerAcct    = bytes.Repeat([]byte{0x11}, 20) // p1
	provider2Acct   = bytes.Repeat([]byte{0x22}, 20) // p2
	serviceAcct     = bytes.Repeat([]byte{0x33}, 20)
	service2Acct    = bytes.Repeat([]byte{0x34}, 20)
	service3Acct    = bytes.Repeat([]byte{0x35}, 20)
	broadcasterAcct = bytes.Repeat([]byte{0x44}, 20)
	outsiderAcct    = bytes.Repeat([]byte{0x55}, 20)
	authorityAcct   = bytes.Repeat([]byte{0x66}, 20)
	auditorAcct     = bytes.Repeat([]byte{0x77}, 20)
)

const (
	providersStore = "providers"
	contentStore   = "content"
	// testGas is the gas limit of one test message.
	testGas uint64 = 2_000_000
	// term is the DEC-005 devnet test number the fake authority returns.
	term int64 = 3600
	// packageSize is the package size of the default content records.
	packageSize uint64 = 1000
)

// t0 is the block time of a fresh test context.
const t0 = msgkittest.BlockUnixSeconds

// testKey derives a deterministic ed25519 key from a label. No private
// key is written to any file.
func testKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-leases-test-seed:" + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func pub(label string) []byte { return []byte(testKey(label).Public().(ed25519.PublicKey)) }

func digestOf(label string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(label))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

func manifestOf(contentID string) *typesv1.Digest { return digestOf("manifest:" + contentID) }

// fakeAuthority is the x/authority read surface: roles, governed limits,
// the DEC-005 devnet number, and the policy version.
type fakeAuthority struct {
	roles     map[string]typesv1.SubmitterRole
	limits    map[string]uint64
	term      uint64
	termFound bool
	policy    uint64
}

func (f *fakeAuthority) Role(_ sdk.Context, addr []byte) typesv1.SubmitterRole {
	return f.roles[string(addr)]
}

func (f *fakeAuthority) GovernedLimit(_ sdk.Context, key string) (uint64, bool) {
	v, ok := f.limits[key]
	return v, ok
}

func (f *fakeAuthority) DevnetNumeric(_ sdk.Context, decisionID string) (uint64, bool) {
	if decisionID != leases.TermDecisionID || !f.termFound {
		return 0, false
	}
	return f.term, true
}

func (f *fakeAuthority) PolicyVersion(sdk.Context) uint64 { return f.policy }

// fakeProviders keeps svn.providers.v1.Provider records in the "providers"
// store and implements the spec semantics of the provider handoff API.
type fakeProviders struct{ key *storetypes.KVStoreKey }

func providerKey(id string) []byte { return append([]byte{0x01}, id...) }

func (f *fakeProviders) load(ctx sdk.Context, id string) (*providersv1.Provider, bool) {
	raw := ctx.KVStore(f.key).Get(providerKey(id))
	if raw == nil {
		return nil, false
	}
	var p providersv1.Provider
	if err := proto.Unmarshal(raw, &p); err != nil {
		panic(err)
	}
	return &p, true
}

func (f *fakeProviders) save(ctx sdk.Context, p *providersv1.Provider) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	if err != nil {
		panic(err)
	}
	ctx.KVStore(f.key).Set(providerKey(p.GetProviderId()), raw)
}

func (f *fakeProviders) Provider(ctx sdk.Context, id string) (*providersv1.Provider, bool) {
	return f.load(ctx, id)
}

func (f *fakeProviders) CheckNewObligation(ctx sdk.Context, id string) (*providersv1.Provider, msgkit.Code) {
	p, found := f.load(ctx, id)
	if !found {
		return nil, msgkit.NotFound
	}
	if p.GetState() != providersv1.ProviderState_PROVIDER_STATE_ADMITTED {
		return nil, msgkit.Forbidden
	}
	return p, msgkit.OK
}

func (f *fakeProviders) VerifyProviderSignature(ctx sdk.Context, id string, domain typesv1.SignatureDomain, canonical string, sig *typesv1.InnerSignature) msgkit.Code {
	p, found := f.load(ctx, id)
	if !found {
		return msgkit.NotFound
	}
	return msgkit.VerifyInner(sig, domain, p.GetSigningKeyId(), p.GetSigningPubkey(), canonical)
}

func (f *fakeProviders) check(ctx sdk.Context, id string, amount uint64) (*providersv1.Provider, msgkit.Code) {
	if amount == 0 {
		return nil, msgkit.Malformed
	}
	p, found := f.load(ctx, id)
	if !found {
		return nil, msgkit.NotFound
	}
	if amount > math.MaxUint64-p.GetReservedBytes() {
		return nil, msgkit.Overflow
	}
	if p.GetReservedBytes()+amount > p.GetAdmittedCapacityBytes() {
		return nil, msgkit.Capacity
	}
	return p, msgkit.OK
}

func (f *fakeProviders) ReserveProbe(ctx sdk.Context, id string, amount uint64) msgkit.Code {
	_, code := f.check(ctx, id, amount)
	return code
}

func (f *fakeProviders) Reserve(ctx sdk.Context, id string, amount uint64) msgkit.Code {
	p, code := f.check(ctx, id, amount)
	if code != msgkit.OK {
		return code
	}
	p.ReservedBytes += amount
	f.save(ctx, p)
	return msgkit.OK
}

func (f *fakeProviders) Release(ctx sdk.Context, id string, amount uint64) msgkit.Code {
	if amount == 0 {
		return msgkit.Malformed
	}
	p, found := f.load(ctx, id)
	if !found {
		return msgkit.NotFound
	}
	if amount > p.GetReservedBytes() {
		return msgkit.Underflow
	}
	p.ReservedBytes -= amount
	f.save(ctx, p)
	return msgkit.OK
}

// fakeContent keeps svn.content.v1.Content records in the "content" store
// and implements CheckOfferable, CheckNotWithdrawn, and CheckPackage as
// x/content does.
type fakeContent struct{ key *storetypes.KVStoreKey }

func contentKey(id string) []byte { return append([]byte{0x01}, id...) }

func (f *fakeContent) load(ctx sdk.Context, id string) (*contentv1.Content, bool) {
	raw := ctx.KVStore(f.key).Get(contentKey(id))
	if raw == nil {
		return nil, false
	}
	var c contentv1.Content
	if err := proto.Unmarshal(raw, &c); err != nil {
		panic(err)
	}
	return &c, true
}

func (f *fakeContent) save(ctx sdk.Context, c *contentv1.Content) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(c)
	if err != nil {
		panic(err)
	}
	ctx.KVStore(f.key).Set(contentKey(c.GetContentId()), raw)
}

func (f *fakeContent) CheckOfferable(ctx sdk.Context, id string, version uint64, manifest []byte, packageBytes uint64) msgkit.Code {
	c, found := f.load(ctx, id)
	if !found {
		return msgkit.NotFound
	}
	if c.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		return msgkit.Withdrawn
	}
	if c.GetState() != contentv1.ContentState_CONTENT_STATE_PENDING || c.GetContentVersion() != version {
		return msgkit.Conflict
	}
	if !bytes.Equal(c.GetManifestDigest().GetValue(), manifest) || c.GetPackageBytes() != packageBytes {
		return msgkit.Integrity
	}
	return msgkit.OK
}

func (f *fakeContent) CheckPackage(ctx sdk.Context, id string, version uint64, manifest []byte, packageBytes uint64) msgkit.Code {
	c, found := f.load(ctx, id)
	if !found || c.GetContentVersion() != version {
		return msgkit.NotFound
	}
	if !bytes.Equal(c.GetManifestDigest().GetValue(), manifest) || c.GetPackageBytes() != packageBytes {
		return msgkit.Integrity
	}
	return msgkit.OK
}

func (f *fakeContent) CheckNotWithdrawn(ctx sdk.Context, id string) msgkit.Code {
	c, found := f.load(ctx, id)
	if !found {
		return msgkit.NotFound
	}
	if c.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		return msgkit.Withdrawn
	}
	return msgkit.OK
}

// fakeEvidence is the goal-09 evidence read surface.
type evidenceRecord struct {
	expiry  int64
	expired bool
}

type fakeEvidence struct{ records map[string]evidenceRecord }

func (f *fakeEvidence) RenewalEvidence(_ sdk.Context, id string) (int64, bool, bool) {
	r, ok := f.records[id]
	return r.expiry, r.expired, ok
}

// fakeLedger is another module's business-id ledger.
type fakeLedger struct{ digests map[string][]byte }

func (f *fakeLedger) BusinessDigest(_ sdk.Context, id string) ([]byte, bool) {
	d, ok := f.digests[id]
	return append([]byte(nil), d...), ok
}

type env struct {
	t         testing.TB
	ctx       sdk.Context
	keys      map[string]*storetypes.KVStoreKey
	k         leases.Keeper
	auth      *fakeAuthority
	providers *fakeProviders
	content   *fakeContent
	evidence  *fakeEvidence
	peer      *fakeLedger
}

// newEnv mounts the leases, providers, and content stores, imports an
// empty leases genesis (layout key), and registers providers p1 and p2
// (ADMITTED, capacity 1 GiB, current keys "p1"/"key-p1" and "p2"/"key-p2")
// and content c1, c2, c3 (PENDING, version 1, 1000 bytes).
func newEnv(t testing.TB) *env {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, leases.ModuleName, providersStore, contentStore)
	auth := &fakeAuthority{
		roles: map[string]typesv1.SubmitterRole{
			string(providerAcct):    typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
			string(provider2Acct):   typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
			string(serviceAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
			string(service2Acct):    typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
			string(service3Acct):    typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
			string(broadcasterAcct): typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
			string(authorityAcct):   typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY,
			string(auditorAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
		},
		limits:    map[string]uint64{},
		term:      uint64(term),
		termFound: true,
		policy:    1,
	}
	peers := msgkit.NewPeers()
	peer := &fakeLedger{digests: map[string][]byte{}}
	e := &env{
		t:         t,
		ctx:       ctx,
		keys:      keys,
		auth:      auth,
		providers: &fakeProviders{key: keys[providersStore]},
		content:   &fakeContent{key: keys[contentStore]},
		evidence:  &fakeEvidence{records: map[string]evidenceRecord{}},
		peer:      peer,
	}
	e.k = leases.NewKeeper(keys[leases.ModuleName], leases.Deps{
		Roles: auth, Limits: auth, Params: auth, Policy: auth,
		Providers: e.providers, Content: e.content, Evidence: e.evidence, Peers: peers,
	})
	peers.Add("content", peer)
	peers.Add(leases.ModuleName, e.k)
	e.putProvider("p1", providerAcct, providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1<<30, 0)
	e.putProvider("p2", provider2Acct, providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1<<30, 0)
	for _, id := range []string{"c1", "c2", "c3"} {
		e.putContent(id, contentv1.ContentState_CONTENT_STATE_PENDING, packageSize)
	}
	if err := e.k.InitGenesis(e.unmetered(), &leasesv1.GenesisState{}); err != nil {
		t.Fatal(err)
	}
	return e
}

// fresh is the context of one message: a new gas meter and event manager.
func (e *env) fresh() sdk.Context {
	return e.ctx.WithGasMeter(storetypes.NewGasMeter(testGas)).WithEventManager(sdk.NewEventManager())
}

// at is fresh at another block time.
func (e *env) at(unix int64) sdk.Context {
	return e.fresh().WithBlockTime(time.Unix(unix, 0).UTC())
}

// unmetered is a context for fixture seeding.
func (e *env) unmetered() sdk.Context {
	return e.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
}

func (e *env) store(name string) storetypes.KVStore {
	return e.ctx.MultiStore().GetKVStore(e.keys[name])
}

func (e *env) leaseStore() storetypes.KVStore { return e.store(leases.ModuleName) }

// allKeys are the stores a lease handler can touch, in a fixed order.
func (e *env) allKeys() []*storetypes.KVStoreKey {
	return []*storetypes.KVStoreKey{e.keys[leases.ModuleName], e.keys[providersStore], e.keys[contentStore]}
}

// snapshot is every mounted store of ctx (committed or a cached branch).
func (e *env) snapshot(ctx sdk.Context) []byte { return leasestest.StoreSnapshot(ctx, e.allKeys()...) }

func (e *env) putProvider(id string, account []byte, state providersv1.ProviderState, capacity, reserved uint64) {
	e.providers.save(e.unmetered(), &providersv1.Provider{
		ProviderId:             id,
		Account:                append([]byte(nil), account...),
		SigningPubkey:          pub(id),
		SigningKeyId:           "key-" + id,
		KeyGeneration:          1,
		EndpointManifestDigest: digestOf("endpoint:" + id),
		State:                  state,
		AdmittedCapacityBytes:  capacity,
		ReservedBytes:          reserved,
		OperatorGroupId:        "devnet-test-only",
	})
}

func (e *env) provider(id string) *providersv1.Provider {
	e.t.Helper()
	p, found := e.providers.load(e.unmetered(), id)
	if !found {
		e.t.Fatalf("no provider %q", id)
	}
	return p
}

func (e *env) editProvider(id string, edit func(*providersv1.Provider)) {
	p := e.provider(id)
	edit(p)
	e.providers.save(e.unmetered(), p)
}

func (e *env) putContent(id string, state contentv1.ContentState, size uint64) {
	e.content.save(e.unmetered(), &contentv1.Content{
		ContentId:      id,
		ContentVersion: 1,
		ManifestDigest: manifestOf(id),
		PackageBytes:   size,
		SubmittingHost: serviceAcct,
		State:          state,
	})
}

func (e *env) setContentState(id string, state contentv1.ContentState) {
	c, found := e.content.load(e.unmetered(), id)
	if !found {
		e.t.Fatalf("no content %q", id)
	}
	c.State = state
	e.content.save(e.unmetered(), c)
}

// result is the observable outcome of one call on a trial branch.
type result struct {
	code       msgkit.Code
	idempotent bool
	events     []abci.Event
	// trialSame is true when the trial branch's stores equal the stores
	// before the call.
	trialSame bool
	gas       uint64
}

// codeOf maps a handler error to its protocol code through the ABCI info,
// as a FinalizeBlock result carries it: codespace "svn" and the
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

// exec runs fn on a cached branch of ctx (the trial state), records the
// code, the branch's events, and whether the branch's stores still equal
// the stores of ctx, and writes the branch back only on success.
func (e *env) exec(ctx sdk.Context, fn func(sdk.Context) error) result {
	e.t.Helper()
	before := e.snapshot(ctx)
	cached, write := ctx.CacheContext()
	err := fn(cached)
	r := result{
		code:      codeOf(e.t, err),
		events:    cached.EventManager().ABCIEvents(),
		trialSame: bytes.Equal(before, e.snapshot(cached)),
		gas:       cached.GasMeter().GasConsumed(),
	}
	for _, ev := range r.events {
		if ev.Type == leases.ModuleName+"_result" {
			r.idempotent = true
		}
	}
	if err == nil {
		write()
	}
	return r
}

func (e *env) offer(ctx sdk.Context, msg *leasesv1.MsgOfferLease) result {
	return e.exec(ctx, func(c sdk.Context) error { _, err := e.k.OfferLease(c, msg); return err })
}

func (e *env) accept(ctx sdk.Context, msg *leasesv1.MsgAcceptLease) result {
	return e.exec(ctx, func(c sdk.Context) error { _, err := e.k.AcceptLease(c, msg); return err })
}

func (e *env) renew(ctx sdk.Context, msg *leasesv1.MsgRenewLease) result {
	return e.exec(ctx, func(c sdk.Context) error { _, err := e.k.RenewLease(c, msg); return err })
}

func (e *env) end(ctx sdk.Context, msg *leasesv1.MsgEndLease) result {
	return e.exec(ctx, func(c sdk.Context) error { _, err := e.k.EndLease(c, msg); return err })
}

// expire runs ExpireDue on a cached branch of ctx and writes it back, with
// its events, on success.
func (e *env) expire(ctx sdk.Context) error {
	cached, write := ctx.CacheContext()
	if err := e.k.ExpireDue(cached); err != nil {
		return err
	}
	write()
	return nil
}

// requireOK fails unless r is a non-idempotent success.
func (e *env) requireOK(r result, what string) {
	e.t.Helper()
	if r.code != msgkit.OK || r.idempotent {
		e.t.Fatalf("%s: code %s idempotent %t", what, r.code, r.idempotent)
	}
}

// requireReject fails unless r is the given failure that left the trial
// state byte-identical and emitted nothing.
func (e *env) requireReject(r result, want msgkit.Code, what string) {
	e.t.Helper()
	if r.code != want {
		e.t.Fatalf("%s: code %s, want %s", what, r.code, want)
	}
	if !r.trialSame {
		e.t.Fatalf("%s: rejected message changed the trial state", what)
	}
	if len(r.events) != 0 {
		e.t.Fatalf("%s: rejected message emitted %d events", what, len(r.events))
	}
}

// requireIdempotent fails unless r is an idempotent success that wrote
// nothing and emitted only leases_result.
func (e *env) requireIdempotent(r result, what string) {
	e.t.Helper()
	if r.code != msgkit.OK || !r.idempotent || !r.trialSame || len(r.events) != 1 {
		e.t.Fatalf("%s: code %s idempotent %t same %t events %d", what, r.code, r.idempotent, r.trialSame, len(r.events))
	}
}

// Message builders. Default offer terms: start t0+100, deadline start+term,
// expiry t0+600, the content record's digest and bytes, term version 1.

func (e *env) offerMsg(offerID, contentID, providerID string) *leasesv1.MsgOfferLease {
	return e.offerMsgAt(offerID, contentID, providerID, t0)
}

// offerMsgAt is offerMsg with times relative to committed time now.
func (e *env) offerMsgAt(offerID, contentID, providerID string, now int64) *leasesv1.MsgOfferLease {
	start := now + 100
	return &leasesv1.MsgOfferLease{
		Signer:              serviceAcct,
		BusinessId:          "off-" + offerID,
		OfferId:             offerID,
		ContentId:           contentID,
		ContentVersion:      1,
		ProviderId:          providerID,
		ManifestDigest:      manifestOf(contentID),
		PackageBytes:        packageSize,
		TermsDigest:         digestOf("terms:" + offerID),
		StartUnixSeconds:    start,
		DeadlineUnixSeconds: start + term,
		ExpiryUnixSeconds:   now + 600,
		TermVersion:         1,
	}
}

func withTime(ctx sdk.Context, unix int64) sdk.Context {
	return ctx.WithBlockTime(time.Unix(unix, 0).UTC())
}

// sign signs the sign bytes of one inner signature with the key of label
// and names keyID.
func sign(domain typesv1.SignatureDomain, signBytes []byte, label, keyID string) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  domain,
		PublicKey:               pub(label),
		Signature:               ed25519.Sign(testKey(label), signBytes),
		KeyId:                   keyID,
		CanonicalProfileVersion: 1,
	}
}

// acceptSig is the acceptance of offer and leaseID by the key of label.
func acceptSig(t testing.TB, offer *leasesv1.Offer, leaseID, label, keyID string) *typesv1.InnerSignature {
	t.Helper()
	signBytes, err := leases.AcceptSignBytes(offer, leaseID)
	if err != nil {
		t.Fatal(err)
	}
	return sign(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, signBytes, label, keyID)
}

// storedOffer reads an open offer from committed state.
func (e *env) storedOffer(id string) *leasesv1.Offer {
	e.t.Helper()
	offer, found, err := e.k.OfferForTest(e.unmetered(), id)
	if err != nil || !found {
		e.t.Fatalf("open offer %q: found %t err %v", id, found, err)
	}
	return offer
}

func (e *env) storedLease(id string) *leasesv1.Lease {
	e.t.Helper()
	lease, found, err := e.k.LeaseForTest(e.unmetered(), id)
	if err != nil || !found {
		e.t.Fatalf("lease %q: found %t err %v", id, found, err)
	}
	return lease
}

// acceptMsg accepts the stored open offer offerID as leaseID, relayed by
// the broadcaster, signed by the provider's current default key.
func (e *env) acceptMsg(offerID, leaseID string) *leasesv1.MsgAcceptLease {
	offer := e.storedOffer(offerID)
	return &leasesv1.MsgAcceptLease{
		Signer:             broadcasterAcct,
		BusinessId:         "acc-" + leaseID,
		OfferId:            offerID,
		LeaseId:            leaseID,
		ProviderAcceptance: acceptSig(e.t, offer, leaseID, offer.GetProviderId(), "key-"+offer.GetProviderId()),
	}
}

func (e *env) signRenew(msg *leasesv1.MsgRenewLease, label, keyID string) {
	signBytes, err := leases.RenewSignBytes(msg.GetLeaseId(), msg.GetTermVersion(), msg.GetDeadlineUnixSeconds(),
		msg.GetManifestDigest(), msg.GetPackageBytes(), msg.GetEvidenceId())
	if err != nil {
		e.t.Fatal(err)
	}
	msg.ProviderSignature = sign(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, signBytes, label, keyID)
}

// renewMsg renews the stored lease by one term, relayed by the
// broadcaster, signed by the provider's default key.
func (e *env) renewMsg(leaseID, businessID string) *leasesv1.MsgRenewLease {
	lease := e.storedLease(leaseID)
	msg := &leasesv1.MsgRenewLease{
		Signer:              broadcasterAcct,
		BusinessId:          businessID,
		LeaseId:             leaseID,
		TermVersion:         lease.GetTermVersion() + 1,
		DeadlineUnixSeconds: lease.GetDeadlineUnixSeconds() + term,
		ManifestDigest:      proto.Clone(lease.GetManifestDigest()).(*typesv1.Digest),
		PackageBytes:        lease.GetPackageBytes(),
	}
	e.signRenew(msg, lease.GetProviderId(), "key-"+lease.GetProviderId())
	return msg
}

// signEnd signs msg's end payload for termVersion (the lease's term
// version when the provider signs).
func (e *env) signEnd(msg *leasesv1.MsgEndLease, providerID, label, keyID string, termVersion uint64) {
	signBytes, err := leases.EndSignBytes(msg.GetLeaseId(), providerID, termVersion)
	if err != nil {
		e.t.Fatal(err)
	}
	msg.ProviderSignature = sign(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, signBytes, label, keyID)
}

// endMsg is a provider-signed end relayed by the broadcaster.
func (e *env) endMsg(leaseID, businessID string) *leasesv1.MsgEndLease {
	lease := e.storedLease(leaseID)
	msg := &leasesv1.MsgEndLease{Signer: broadcasterAcct, BusinessId: businessID, LeaseId: leaseID}
	e.signEnd(msg, lease.GetProviderId(), lease.GetProviderId(), "key-"+lease.GetProviderId(), lease.GetTermVersion())
	return msg
}

// authorityEndMsg is an authority end.
func authorityEndMsg(leaseID, businessID string) *leasesv1.MsgEndLease {
	return &leasesv1.MsgEndLease{Signer: authorityAcct, BusinessId: businessID, LeaseId: leaseID, AuthorityEnd: true}
}

// mustOffer commits an offer through the handler.
func (e *env) mustOffer(msg *leasesv1.MsgOfferLease) {
	e.t.Helper()
	e.requireOK(e.offer(e.fresh(), msg), "offer "+msg.GetOfferId())
}

// mustLease commits offer+accept for (content, provider) as leaseID.
func (e *env) mustLease(leaseID, contentID, providerID string) *leasesv1.Lease {
	e.t.Helper()
	offerID := "o-" + leaseID
	e.mustOffer(e.offerMsg(offerID, contentID, providerID))
	e.requireOK(e.accept(e.fresh(), e.acceptMsg(offerID, leaseID)), "accept "+leaseID)
	return e.storedLease(leaseID)
}

// seedOffer writes an open offer and its rows directly (fixture only).
func (e *env) seedOffer(offer *leasesv1.Offer) {
	e.t.Helper()
	if err := e.k.SeedOfferForTest(e.unmetered(), offer); err != nil {
		e.t.Fatal(err)
	}
}

// seedLease writes a lease and its rows directly and, when it is held,
// adds its bytes to the provider's reserved bytes (fixture only).
func (e *env) seedLease(lease *leasesv1.Lease) {
	e.t.Helper()
	e.k.SeedLeaseForTest(e.unmetered(), lease)
	if lease.GetReservationHeld() {
		e.editProvider(lease.GetProviderId(), func(p *providersv1.Provider) { p.ReservedBytes += lease.GetPackageBytes() })
	}
}

// offerBy is offerMsg sent by another scheduler.
func (e *env) offerBy(scheduler []byte, offerID, contentID, providerID string) *leasesv1.MsgOfferLease {
	msg := e.offerMsg(offerID, contentID, providerID)
	msg.Signer = scheduler
	return msg
}

// offerRecordBy is offerRecord with another scheduler.
func (e *env) offerRecordBy(scheduler []byte, offerID, contentID, providerID string) *leasesv1.Offer {
	o := e.offerRecord(offerID, contentID, providerID)
	o.Scheduler = append([]byte(nil), scheduler...)
	return o
}

// offerRecord is the stored offer the default offerMsg would write.
func (e *env) offerRecord(offerID, contentID, providerID string) *leasesv1.Offer {
	msg := e.offerMsg(offerID, contentID, providerID)
	return &leasesv1.Offer{
		OfferId: msg.GetOfferId(), ContentId: msg.GetContentId(), ContentVersion: msg.GetContentVersion(),
		ProviderId: msg.GetProviderId(), ManifestDigest: msg.GetManifestDigest(), PackageBytes: msg.GetPackageBytes(),
		TermsDigest: msg.GetTermsDigest(), StartUnixSeconds: msg.GetStartUnixSeconds(),
		DeadlineUnixSeconds: msg.GetDeadlineUnixSeconds(), ExpiryUnixSeconds: msg.GetExpiryUnixSeconds(),
		TermVersion: msg.GetTermVersion(), Scheduler: serviceAcct,
	}
}

// leaseRecord is a held ACTIVE lease with the default terms.
func (e *env) leaseRecord(leaseID, contentID, providerID string) *leasesv1.Lease {
	o := e.offerRecord("o-"+leaseID, contentID, providerID)
	return &leasesv1.Lease{
		LeaseId: leaseID, OfferId: o.GetOfferId(), ContentId: contentID, ContentVersion: 1, ProviderId: providerID,
		ManifestDigest: o.GetManifestDigest(), PackageBytes: o.GetPackageBytes(), TermsDigest: o.GetTermsDigest(),
		StartUnixSeconds: o.GetStartUnixSeconds(), DeadlineUnixSeconds: o.GetDeadlineUnixSeconds(),
		State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, TermVersion: 1, PolicyVersion: 1, ReservationHeld: true,
	}
}

// requireInvariants runs leasestest.CheckInvariants over committed state.
func (e *env) requireInvariants(baseline map[string]uint64) {
	e.t.Helper()
	if err := leasestest.CheckInvariants(e.unmetered(), e.k, e.providers, baseline); err != nil {
		e.t.Fatal(err)
	}
	if err := leasestest.CheckOfferIntegrity(e.unmetered(), e.k, e.content); err != nil {
		e.t.Fatal(err)
	}
}

// typedNames returns the svn.events.v1 event types of r, without the prefix.
func (r result) typedNames() []string {
	var out []string
	for _, ev := range r.events {
		if name, ok := strings.CutPrefix(ev.Type, "svn.events.v1."); ok {
			out = append(out, name)
		}
	}
	return out
}
