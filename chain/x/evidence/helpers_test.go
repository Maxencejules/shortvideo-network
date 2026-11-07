package evidence_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
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
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Unit-test fakes. They stand in for x/authority, x/leases, and x/content,
// which this package never imports (TestKeeperImports). The lease and
// content fakes keep their records in real IAVL stores mounted next to the
// evidence store, so a handler run on a cached branch (ctx.CacheContext) is
// compared over every store it could reach, and a snapshot shows that an
// accepted batch leaves them untouched. None of this is end-to-end
// evidence: the real keepers run in chain/app (goal 09 integration) and
// chain/x/evidence/difftest.

// TEST-ONLY KEYS. Every ed25519 key in these tests is derived from a fixed
// label: the seed is sha256("svn-evidence-test-seed:" + label). They are
// synthetic development keys for unit tests, published in this file; no
// private key is written to any file, and none is a real auditor key.
func testKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-evidence-test-seed:" + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func pub(label string) []byte { return []byte(testKey(label).Public().(ed25519.PublicKey)) }

// addr is the account address of the key of label (Q1: an auditor's
// account address derives from its ed25519 key).
func addr(label string) []byte { return evidence.AuditorAddress(pub(label)) }

// prefix is the id prefix of the auditor of label.
func prefix(label string) string { return evidence.IDPrefix(addr(label)) }

const (
	auditor1 = "auditor-1"
	auditor2 = "auditor-2"
)

// Synthetic 20-byte accounts with other roles. No real keys or addresses.
var (
	providerAcct    = bytes.Repeat([]byte{0x11}, 20)
	serviceAcct     = bytes.Repeat([]byte{0x33}, 20)
	broadcasterAcct = bytes.Repeat([]byte{0x44}, 20)
	outsiderAcct    = bytes.Repeat([]byte{0x55}, 20)
	authorityAcct   = bytes.Repeat([]byte{0x66}, 20)
)

const (
	leasesStore  = "leases"
	contentStore = "content"
	// testGas is the gas limit of one test message.
	testGas uint64 = 2_000_000
	// t0 is 2026-01-01T00:00:00Z, an epoch boundary (t0 % 3600 == 0).
	t0 = msgkittest.BlockUnixSeconds
	// now is the block time of a fresh test context: 600 s into the epoch
	// that starts at t0.
	now = t0 + 600
	// leaseDeadline is the deadline of the default leases.
	leaseDeadline = t0 + 30*86400
	// itemLife is the default item lifetime (expiry = now + itemLife).
	itemLife int64 = 3600
)

// epochNow is the epoch of now.
var epochNow = evidence.EpochAt(now)

func digestOf(label string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(label))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

func manifestOf(contentID string) *typesv1.Digest { return digestOf("manifest:" + contentID) }

// fakeAuthority is the x/authority read surface: roles and governed limits.
type fakeAuthority struct {
	roles  map[string]typesv1.SubmitterRole
	limits map[string]uint64
}

func (f *fakeAuthority) Role(_ sdk.Context, a []byte) typesv1.SubmitterRole {
	return f.roles[string(a)]
}

func (f *fakeAuthority) GovernedLimit(_ sdk.Context, key string) (uint64, bool) {
	v, ok := f.limits[key]
	return v, ok
}

// fakeLeases keeps svn.leases.v1.Lease records in the "leases" store and
// implements Lease as x/leases does (any state, a copy).
type fakeLeases struct{ key *storetypes.KVStoreKey }

func leaseKey(id string) []byte { return append([]byte{0x02}, id...) }

func (f *fakeLeases) Lease(ctx sdk.Context, id string) (*leasesv1.Lease, bool) {
	raw := ctx.KVStore(f.key).Get(leaseKey(id))
	if raw == nil {
		return nil, false
	}
	var l leasesv1.Lease
	if err := proto.Unmarshal(raw, &l); err != nil {
		panic(err)
	}
	return &l, true
}

func (f *fakeLeases) save(ctx sdk.Context, l *leasesv1.Lease) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(l)
	if err != nil {
		panic(err)
	}
	ctx.KVStore(f.key).Set(leaseKey(l.GetLeaseId()), raw)
}

// fakeContent keeps svn.content.v1.Content records in the "content" store
// and implements CheckNotWithdrawn as x/content does.
type fakeContent struct{ key *storetypes.KVStoreKey }

func contentKey(id string) []byte { return append([]byte{0x01}, id...) }

func (f *fakeContent) CheckNotWithdrawn(ctx sdk.Context, id string) msgkit.Code {
	raw := ctx.KVStore(f.key).Get(contentKey(id))
	if raw == nil {
		return msgkit.NotFound
	}
	var c contentv1.Content
	if err := proto.Unmarshal(raw, &c); err != nil {
		panic(err)
	}
	if c.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		return msgkit.Withdrawn
	}
	return msgkit.OK
}

func (f *fakeContent) save(ctx sdk.Context, c *contentv1.Content) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(c)
	if err != nil {
		panic(err)
	}
	ctx.KVStore(f.key).Set(contentKey(c.GetContentId()), raw)
}

// fakeLedger is another module's business-id ledger.
type fakeLedger struct{ digests map[string][]byte }

func (f *fakeLedger) BusinessDigest(_ sdk.Context, id string) ([]byte, bool) {
	d, ok := f.digests[id]
	return append([]byte(nil), d...), ok
}

type env struct {
	t       testing.TB
	ctx     sdk.Context
	keys    map[string]*storetypes.KVStoreKey
	k       evidence.Keeper
	auth    *fakeAuthority
	leases  *fakeLeases
	content *fakeContent
	peer    *fakeLedger
}

// newEnv mounts the evidence, leases, and content stores, imports an empty
// evidence genesis (layout key) at block time now, admits auditor-1 and
// auditor-2 as AUDITOR and one account of every other role, registers
// content c1, c2, c3 (PENDING, version 1) and holds leases l1 (c1, p1), l2
// (c2, p1), and l3 (c3, p2): ACTIVE, reservation held, deadline
// leaseDeadline, manifest manifestOf(content).
func newEnv(t testing.TB) *env {
	t.Helper()
	e := genesisEnv(t)
	if err := e.k.InitGenesis(e.unmetered(), &evidencev1.GenesisState{}); err != nil {
		t.Fatal(err)
	}
	return e
}

// genesisEnv is newEnv without the evidence InitGenesis: the evidence store
// is empty.
func genesisEnv(t testing.TB) *env {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, evidence.ModuleName, leasesStore, contentStore)
	ctx = ctx.WithBlockTime(time.Unix(now, 0).UTC())
	auth := &fakeAuthority{
		roles: map[string]typesv1.SubmitterRole{
			string(addr(auditor1)):  typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
			string(addr(auditor2)):  typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
			string(providerAcct):    typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
			string(serviceAcct):     typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
			string(broadcasterAcct): typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
			string(authorityAcct):   typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY,
		},
		limits: map[string]uint64{},
	}
	peers := msgkit.NewPeers()
	e := &env{
		t:       t,
		ctx:     ctx,
		keys:    keys,
		auth:    auth,
		leases:  &fakeLeases{key: keys[leasesStore]},
		content: &fakeContent{key: keys[contentStore]},
		peer:    &fakeLedger{digests: map[string][]byte{}},
	}
	e.k = evidence.NewKeeper(keys[evidence.ModuleName], evidence.Deps{
		Roles: auth, Limits: auth, Leases: e.leases, Content: e.content, Peers: peers,
	})
	peers.Add("content", e.peer)
	peers.Add(evidence.ModuleName, e.k)
	for _, id := range []string{"c1", "c2", "c3"} {
		e.putContent(id, contentv1.ContentState_CONTENT_STATE_PENDING)
	}
	e.putLease(e.leaseRecord("l1", "c1", "p1"))
	e.putLease(e.leaseRecord("l2", "c2", "p1"))
	e.putLease(e.leaseRecord("l3", "c3", "p2"))
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

// unmetered is a context for fixture seeding and reads.
func (e *env) unmetered() sdk.Context {
	return e.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
}

func (e *env) store(name string) storetypes.KVStore {
	return e.ctx.MultiStore().GetKVStore(e.keys[name])
}

// allKeys are every mounted store, in a fixed order.
func (e *env) allKeys() []*storetypes.KVStoreKey {
	return []*storetypes.KVStoreKey{e.keys[evidence.ModuleName], e.keys[leasesStore], e.keys[contentStore]}
}

// otherKeys are the stores of the other modules' fakes.
func (e *env) otherKeys() []*storetypes.KVStoreKey {
	return []*storetypes.KVStoreKey{e.keys[leasesStore], e.keys[contentStore]}
}

// snapshot is every mounted store of ctx (committed or a cached branch).
func (e *env) snapshot(ctx sdk.Context) []byte {
	return evidencetest.StoreSnapshot(ctx, e.allKeys()...)
}

func (e *env) putContent(id string, state contentv1.ContentState) {
	e.content.save(e.unmetered(), &contentv1.Content{
		ContentId:      id,
		ContentVersion: 1,
		ManifestDigest: manifestOf(id),
		PackageBytes:   1000,
		SubmittingHost: serviceAcct,
		State:          state,
	})
}

func (e *env) putLease(l *leasesv1.Lease) { e.leases.save(e.unmetered(), l) }

// withdraw marks a content record WITHDRAWN.
func (e *env) withdraw(id string) { e.putContent(id, contentv1.ContentState_CONTENT_STATE_WITHDRAWN) }

func (e *env) lease(id string) *leasesv1.Lease {
	e.t.Helper()
	l, found := e.leases.Lease(e.unmetered(), id)
	if !found {
		e.t.Fatalf("no lease %q", id)
	}
	return l
}

func (e *env) editLease(id string, edit func(*leasesv1.Lease)) {
	l := e.lease(id)
	edit(l)
	e.putLease(l)
}

// leaseRecord is a held ACTIVE lease of (content, provider).
func (e *env) leaseRecord(leaseID, contentID, providerID string) *leasesv1.Lease {
	return &leasesv1.Lease{
		LeaseId: leaseID, OfferId: "o-" + leaseID, ContentId: contentID, ContentVersion: 1, ProviderId: providerID,
		ManifestDigest: manifestOf(contentID), PackageBytes: 1000, TermsDigest: digestOf("terms:" + leaseID),
		StartUnixSeconds: t0 - 100, DeadlineUnixSeconds: leaseDeadline,
		State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, TermVersion: 1, PolicyVersion: 1, ReservationHeld: true,
	}
}

// item is an attestation of lease leaseID by the auditor of label, with id
// prefix(label)+suffix, the lease's content, the lease's manifest digest
// (RECORDED), and expiry now + itemLife.
func (e *env) item(label, suffix, leaseID string) *evidencev1.EvidenceItem {
	l := e.lease(leaseID)
	return &evidencev1.EvidenceItem{
		EvidenceId:        prefix(label) + suffix,
		Kind:              evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		LeaseId:           leaseID,
		ContentId:         l.GetContentId(),
		ContentVersion:    l.GetContentVersion(),
		ObservationDigest: proto.Clone(l.GetManifestDigest()).(*typesv1.Digest),
		ExpiryUnixSeconds: now + itemLife,
		Attestation:       true,
	}
}

// batch is an unsigned batch by the auditor of label at epoch epochNow,
// with business id "biz-"+suffix and batch id prefix(label)+"b-"+suffix.
// Its signature carries the key and key id of label; sign fills it.
func batch(label, suffix string, items ...*evidencev1.EvidenceItem) *evidencev1.MsgSubmitAuditBatch {
	return &evidencev1.MsgSubmitAuditBatch{
		Signer:        addr(label),
		BusinessId:    "biz-" + suffix,
		BatchId:       prefix(label) + "b-" + suffix,
		Epoch:         epochNow,
		SchemaVersion: evidence.SchemaVersion,
		Items:         items,
		AuditorSignature: &typesv1.InnerSignature{
			Domain:                  typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION,
			PublicKey:               pub(label),
			KeyId:                   "key-" + label,
			CanonicalProfileVersion: 1,
		},
	}
}

// sign signs msg's attestation payload with the key of label. A payload
// with no SVN-JCS-1 form, or above the cap, cannot be signed; the signature
// is then a valid-length signature over a fixed placeholder, which never
// verifies.
func sign(msg *evidencev1.MsgSubmitAuditBatch, label string) *evidencev1.MsgSubmitAuditBatch {
	signBytes, err := evidence.AuditSignBytes(msg)
	if err != nil {
		signBytes = []byte("no canonical payload")
	}
	msg.AuditorSignature.Signature = ed25519.Sign(testKey(label), signBytes)
	return msg
}

// signed is batch then sign with the same auditor.
func signed(label, suffix string, items ...*evidencev1.EvidenceItem) *evidencev1.MsgSubmitAuditBatch {
	return sign(batch(label, suffix, items...), label)
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
		if ev.Type == evidence.ModuleName+"_result" {
			r.idempotent = true
		}
	}
	if err == nil {
		write()
	}
	return r
}

// submit runs SubmitAuditBatch on a trial branch of ctx.
func (e *env) submit(ctx sdk.Context, msg *evidencev1.MsgSubmitAuditBatch) result {
	return e.exec(ctx, func(c sdk.Context) error { _, err := e.k.SubmitAuditBatch(c, msg); return err })
}

// requireOK fails unless r is a non-idempotent success.
func (e *env) requireOK(r result, what string) {
	e.t.Helper()
	if r.code != msgkit.OK || r.idempotent {
		e.t.Fatalf("%s: code %s idempotent %t", what, r.code, r.idempotent)
	}
}

// mustSubmit commits msg through the handler at the env's block time.
func (e *env) mustSubmit(msg *evidencev1.MsgSubmitAuditBatch) {
	e.t.Helper()
	e.requireOK(e.submit(e.fresh(), msg), "submit "+msg.GetBatchId())
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
// nothing and emitted only evidence_result.
func (e *env) requireIdempotent(r result, what string) {
	e.t.Helper()
	if r.code != msgkit.OK || !r.idempotent || !r.trialSame || len(r.events) != 1 {
		e.t.Fatalf("%s: code %s idempotent %t same %t events %d", what, r.code, r.idempotent, r.trialSame, len(r.events))
	}
}

// stored reads one record as stored.
func (e *env) stored(id string) *evidencev1.EvidenceRecord {
	e.t.Helper()
	record, found := e.k.StoredRecord(e.unmetered(), id)
	if !found {
		e.t.Fatalf("no record %q", id)
	}
	return record
}

// requireInvariants runs evidencetest.CheckInvariants over committed state.
func (e *env) requireInvariants() {
	e.t.Helper()
	if err := evidencetest.CheckInvariants(e.unmetered(), e.k, e.leases); err != nil {
		e.t.Fatal(err)
	}
}

// query runs Query/Evidence at block time unix.
func (e *env) query(unix int64, req *evidencev1.QueryEvidenceRequest) (*evidencev1.QueryEvidenceResponse, error) {
	return e.k.Evidence(e.at(unix), req)
}

// items builds n attestations of lease leaseID by label with suffixes
// tag-0 .. tag-(n-1).
func (e *env) items(label, tag, leaseID string, n int) []*evidencev1.EvidenceItem {
	out := make([]*evidencev1.EvidenceItem, n)
	for i := range out {
		out[i] = e.item(label, fmt.Sprintf("%s-%d", tag, i), leaseID)
	}
	return out
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
