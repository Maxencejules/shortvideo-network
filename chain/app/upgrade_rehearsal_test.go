package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/protobuf/proto"

	"cosmossdk.io/log/v2"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal/scenario"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases/leasestest"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy/policytest"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The upgrade rehearsal at app level (plan 12.4). Every application here is
// the real app on its own goleveldb database under t.TempDir(); a restart
// closes the database and opens it again with app.New, as svnd start does.
// Blocks go straight into FinalizeBlock with real signed transactions;
// CometBFT does not run, so nothing here shows its handshake, block sync, or
// header checks; those belong to the devnet rehearsal (plan 12.5). The
// binaries:
//
//   - v1: the production app (app version 1, no upgrade handler): svnd.
//   - v2: the app with rehearsal.Upgrades (upgrade "rehearsal-1", app
//     version 1 to 2, run by rehearsal.Handler, the handler of B2), x/leases
//     and x/evidence reporting consensus version 3, and
//     rehearsal.LeasesStep and rehearsal.EvidenceStep registered as their 2
//     to 3 migrations (BumpModuleVersionForTest). It is the in-process
//     stand-in for the rehearsal binary B2 plus a module migration (decision
//     request DR-10-36); B2 itself (svnd built with -tags svn_rehearsal)
//     registers no module step.
//   - v2-bad: v2 with rehearsal.BadUpgrades (the same upgrade run by
//     rehearsal.BadHandler, the handler of B2-bad, svnd built with -tags
//     svn_rehearsal_bad): it writes one documented extra row into the
//     x/leases store through the handler environment.
//
// Everything is built from one tree: not independently built releases, not
// signed, not reviewed source.

// rehearsalRelease is the text whose SHA-256 the plan names as its release
// digest. The digest is information for operators; the chain cannot check
// which binary a node runs (DR-10-18).
const rehearsalRelease = "svnd -tags svn_rehearsal built from the tested tree (not a release)"

// rehearsalSlack is the number of blocks the plan height lies beyond the
// minimum lead: h = execute height + MinActivationLeadBlocks + slack.
const rehearsalSlack = 2

// binaryKind is the binary a rehearsal node opens its database with.
type binaryKind int

const (
	binaryV1 binaryKind = iota
	binaryV2
	binaryV2Bad
)

func (b binaryKind) String() string {
	return [...]string{"v1", "v2", "v2-bad"}[b]
}

// openBinary opens the application database in dir with binary kind. For
// v2 and v2-bad it builds the app with SkipLoad, registers the module steps,
// and then runs the load step (LoadForTest: LoadLatestVersion and the load
// gate). It returns the app and the database, or the error of app.New or of
// the load step; on an error the database is closed.
func openBinary(t *testing.T, dir, chainID string, kind binaryKind) (*app.App, dbm.DB, error) {
	t.Helper()
	if kind == binaryV1 {
		db := openDB(t, dir)
		chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: chainID})
		if err != nil {
			_ = db.Close()
			return nil, nil, err
		}
		return chainApp, db, nil
	}
	upgrades := rehearsal.Upgrades()
	if kind == binaryV2Bad {
		upgrades = rehearsal.BadUpgrades()
	}
	return openUpgrade(t, dir, chainID, upgrades, rehearsal.LeasesStep)
}

// leasesStepFor builds the x/leases 2 to 3 migration of one binary from
// the leases store key.
type leasesStepFor func(storetypes.StoreKey) module.MigrationHandler

// openUpgrade opens the database in dir with an upgrade binary: the app
// with upgrades, x/leases at version 3 with the migration step builds,
// x/evidence at version 3 with rehearsal.EvidenceStep, then the load step.
func openUpgrade(t *testing.T, dir, chainID string, upgrades []upgrade.Upgrade, step leasesStepFor) (*app.App, dbm.DB, error) {
	t.Helper()
	db := openDB(t, dir)
	chainApp, err := upgradeApp(t, db, chainID, upgrades, step)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	if err := chainApp.LoadForTest(); err != nil {
		_ = chainApp.Close()
		_ = db.Close()
		return nil, nil, err
	}
	return chainApp, db, nil
}

func openDB(t *testing.T, dir string) dbm.DB {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, dir)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// upgradeApp builds, without loading, an upgrade binary on db (see
// openUpgrade).
func upgradeApp(t *testing.T, db dbm.DB, chainID string, upgrades []upgrade.Upgrade, step leasesStepFor) (*app.App, error) {
	t.Helper()
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: chainID, SkipLoad: true, Upgrades: upgrades})
	if err != nil {
		return nil, err
	}
	if err := chainApp.BumpModuleVersionForTest(app.ModuleLeases, step(chainApp.StoreKeyForTest(app.ModuleLeases))); err != nil {
		t.Fatal(err)
	}
	if err := chainApp.BumpModuleVersionForTest(app.ModuleEvidence, rehearsal.EvidenceStep(chainApp.StoreKeyForTest(app.ModuleEvidence))); err != nil {
		t.Fatal(err)
	}
	return chainApp, nil
}

// diskNode is one rehearsal node: a goleveldb directory, its open
// database, and the cluster node that runs on it.
type diskNode struct {
	dir    string
	db     dbm.DB
	run    *running
	closed bool
}

// opened records an application and database that now run the node.
func (d *diskNode) opened(chainApp *app.App, db dbm.DB) {
	d.run.app, d.db, d.closed = chainApp, db, false
}

// restart closes the node's application and database and opens the
// database again with kind. It fails the test unless the load gate starts
// the binary.
func (d *diskNode) restart(t *testing.T, chainID string, kind binaryKind) {
	t.Helper()
	d.close(t)
	chainApp, db, err := openBinary(t, d.dir, chainID, kind)
	if err != nil {
		t.Fatalf("restart with %s: %v", kind, err)
	}
	d.opened(chainApp, db)
}

// close closes the node's application and database, once.
func (d *diskNode) close(t *testing.T) {
	t.Helper()
	if d.closed {
		return
	}
	d.closed = true
	if err := d.run.app.Close(); err != nil {
		t.Fatal(err)
	}
	_ = d.db.Close()
}

// refusedRestart closes the node and opens its database with kind, which
// must fail with an error containing want. The node is left closed.
func (d *diskNode) refusedRestart(t *testing.T, chainID string, kind binaryKind, want string) {
	t.Helper()
	d.close(t)
	chainApp, db, err := openBinary(t, d.dir, chainID, kind)
	if err == nil {
		_ = chainApp.Close()
		_ = db.Close()
		t.Fatalf("%s started; want a refusal containing %q", kind, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s refused with %q; want %q", kind, err, want)
	}
	t.Logf("%s refused: %v", kind, err)
}

// rehearsalNet is a cluster of disk nodes running the registry devnet
// document with auditors (newEvNet), and what the scenario run recorded.
type rehearsalNet struct {
	e     *evNet
	c     *cluster
	disks []*diskNode
	// h is the plan height and exec the height of the plan's execute;
	// both are 0 until the plan phase starts.
	h, exec int64
	// offers are the scenario's offer messages by offer id, and leaseOffer
	// maps a lease id to the offer it accepted.
	offers     map[string]*leasesv1.MsgOfferLease
	leaseOffer map[string]string
	// validatorKey is the consensus key of the scenario's new validator.
	validatorKey []byte
}

// rehearsalTimes maps the scenario instants to header times: offsets in
// seconds from the genesis time. Every crossing time is after AtResume, and
// no instant lies in [AtPlan, AtResume) except AtPlan.
var rehearsalTimes = map[scenario.Instant]int64{
	scenario.AtSetup:              0,
	scenario.EvidenceExpiresEarly: 60,
	scenario.LeaseExpiresEarly:    120,
	scenario.AtPlan:               300,
	scenario.AtResume:             900,
	scenario.EvidenceCrossExpiry:  1000,
	scenario.OfferCrossExpiry:     1100,
	scenario.LeaseCrossDeadline:   1200,
	scenario.ProposalCrossExpiry:  1300,
	scenario.HeldDeadline:         1800,
}

func (r *rehearsalNet) at(i scenario.Instant) int64 {
	off, ok := rehearsalTimes[i]
	if !ok {
		panic(fmt.Sprintf("instant %d has no time", i))
	}
	return r.e.doc.GenesisUnixSeconds + off
}

// newRehearsalNet initializes nodes disk nodes with v1 from the evidence
// devnet document and commits block 1.
func newRehearsalNet(t *testing.T, nodes int) *rehearsalNet {
	t.Helper()
	e := newEvNet()
	r := &rehearsalNet{e: e, c: &cluster{}, offers: map[string]*leasesv1.MsgOfferLease{}, leaseOffer: map[string]string{},
		validatorKey: bytes32(9)}
	for i := 0; i < nodes; i++ {
		dir := t.TempDir()
		chainApp, db, err := openBinary(t, dir, e.doc.ChainID, binaryV1)
		if err != nil {
			t.Fatal(err)
		}
		modules, err := app.AppStateFromDocument(chainApp.EncodingConfig().Codec, e.doc)
		if err != nil {
			t.Fatal(err)
		}
		params := consensusParams()
		if _, err := chainApp.InitChain(&abci.RequestInitChain{
			ChainId: e.doc.ChainID, Time: e.doc.Time(), AppStateBytes: mustJSON(t, modules),
			InitialHeight: 1, Validators: validatorUpdates(t, e.doc), ConsensusParams: &params,
		}); err != nil {
			t.Fatal(err)
		}
		run := &running{app: chainApp, params: params, when: e.doc.Time()}
		r.disks = append(r.disks, &diskNode{dir: dir, db: db, run: run})
		r.c.nodes = append(r.c.nodes, run)
	}
	t.Cleanup(func() {
		for _, d := range r.disks {
			d.close(t)
		}
	})
	r.c.block(t)
	return r
}

func (r *rehearsalNet) actor(role scenario.Role) *actor {
	switch role {
	case scenario.Authority:
		return r.e.authority
	case scenario.Service:
		return r.e.service
	case scenario.Provider1:
		return r.e.provider1
	case scenario.Provider2:
		return r.e.provider2
	case scenario.Broadcaster:
		return r.e.broadcaster
	case scenario.Auditor:
		return r.e.auditor1
	case scenario.Seat1:
		return r.e.seats[0]
	case scenario.Seat2:
		return r.e.seats[1]
	case scenario.Seat3:
		return r.e.seats[2]
	}
	panic("unknown role " + string(role))
}

// evidenceID is the chain id of a scenario evidence id: the auditor's id
// prefix (deviation D-17) and the scenario id.
func (r *rehearsalNet) evidenceID(id string) string { return prefix(r.e.auditor1) + id }

// councilStepBlocks is the number of blocks one scenario council step
// takes: one per message.
func councilStepBlocks(s scenario.Council) int64 {
	n := int64(1 + s.Approvals)
	if s.Execute {
		n++
	}
	return n
}

// fixPlan sets the plan height when the plan phase starts: the blocks of
// the steps from index i to the SoftwareUpgrade council give its execute
// height E, and h = E + MinActivationLeadBlocks + rehearsalSlack.
func (r *rehearsalNet) fixPlan(t *testing.T, steps []scenario.Step, i int) {
	t.Helper()
	if r.h != 0 {
		return
	}
	height := r.c.height()
	for _, step := range steps[i:] {
		switch s := step.(type) {
		case scenario.SetTime:
		case scenario.Council:
			height += councilStepBlocks(s)
			if _, ok := s.Change.(scenario.SoftwareUpgrade); ok {
				r.exec = height
				r.h = height + int64(msgkit.MinActivationLeadBlocks) + rehearsalSlack
				return
			}
		default:
			height++
		}
	}
	t.Fatal("the scenario has no software upgrade step")
}

// payload builds the council payload of a scenario change.
func (r *rehearsalNet) payload(change scenario.Change) *authorityv1.ProposalPayload {
	switch c := change.(type) {
	case scenario.AdmitProvider:
		return admitPayload(c.ProviderID)
	case scenario.GovernedLimit:
		if c.Activation == 0 {
			return governedPayload(c.Key, c.Value)
		}
		return scheduledPayload(c.Key, c.Value, uint64(r.h+int64(c.Activation)))
	case scenario.ValidatorPower:
		return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: &authorityv1.ValidatorPowerChange{
			ConsensusPubkey: r.validatorKey,
			Power:           c.Power,
			NodeId:          c.NodeID,
			ReasonDigest:    digestOf(sha([]byte("rehearsal validator change"))),
			Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: uint64(r.h + int64(c.Activation))}},
		}}}
	case scenario.SoftwareUpgrade:
		return upgradePayload(c.Name, uint64(r.h), rehearsalRelease)
	case cancelChange:
		return cancelPayload(c.name)
	}
	panic(fmt.Sprintf("unknown change %T", change))
}

// runSteps delivers every scenario step with the app-level helpers; each
// message must return code 0 (and an execute must not be kept).
func (r *rehearsalNet) runSteps(t *testing.T, steps []scenario.Step) {
	t.Helper()
	e, c := r.e, r.c
	for i, step := range steps {
		bid := "bid-" + step.Label()
		switch s := step.(type) {
		case scenario.SetTime:
			if s.At == scenario.AtPlan {
				r.fixPlan(t, steps, i)
			}
			c.setUnix(r.at(s.At))
		case scenario.RegisterProvider:
			who := r.actor(s.Signer)
			wantCode(t, s.Name, e.send(t, c, who, e.registerProvider(t, who, bid, s.ProviderID, groupDevnet, capacity1)), msgkit.OK)
		case scenario.DrainProvider:
			who := r.actor(s.Signer)
			wantCode(t, s.Name, e.send(t, c, who, e.drain(t, who, bid, s.ProviderID, 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)), msgkit.OK)
		case scenario.RegisterContent:
			wantCode(t, s.Name, e.send(t, c, e.service, e.registerContent(t, e.service, bid, s.ContentID, 1, packageManifest(s.ContentID), sizeSmall)), msgkit.OK)
		case scenario.Offer:
			msg := e.offerMsg(e.service, bid, s.OfferID, s.ContentID, s.ProviderID, sizeSmall, r.at(s.Deadline), r.at(s.Expiry))
			r.offers[s.OfferID] = msg
			wantCode(t, s.Name, e.send(t, c, e.service, msg), msgkit.OK)
		case scenario.Accept:
			r.leaseOffer[s.LeaseID] = s.OfferID
			wantCode(t, s.Name, e.send(t, c, e.broadcaster, e.acceptMsg(t, e.broadcaster, bid, r.offers[s.OfferID], s.LeaseID)), msgkit.OK)
		case scenario.Renew:
			offer := r.offers[r.leaseOffer[s.LeaseID]]
			msg := e.renewMsg(t, e.broadcaster, bid, s.LeaseID, offer.GetProviderId(), offer.GetContentId(), sizeSmall, 2, offer.GetDeadlineUnixSeconds()+leaseTerm)
			wantCode(t, s.Name, e.send(t, c, e.broadcaster, msg), msgkit.OK)
		case scenario.End:
			offer := r.offers[r.leaseOffer[s.LeaseID]]
			wantCode(t, s.Name, e.send(t, c, e.broadcaster, e.endMsg(t, e.broadcaster, bid, s.LeaseID, offer.GetProviderId(), 1)), msgkit.OK)
		case scenario.Audit:
			observation := manifestOf(s.ContentID)
			if !s.Matches {
				observation = wrongDigest(s.Name)
			}
			item := auditItem(r.evidenceID(s.EvidenceID), s.LeaseID, s.ContentID, observation, r.at(s.Expiry))
			msg := e.audit(t, e.auditor1, bid, prefix(e.auditor1)+"b-"+s.Name, c.epoch(), item)
			wantCode(t, s.Name, e.send(t, c, e.auditor1, msg), msgkit.OK)
		case scenario.Council:
			r.council(t, s)
		default:
			t.Fatalf("step %s: unknown kind %T", step.Label(), step)
		}
	}
}

// council delivers one scenario council step: the propose, the approvals,
// and the execute, one block each.
func (r *rehearsalNet) council(t *testing.T, s scenario.Council) {
	t.Helper()
	e, c := r.e, r.c
	msg := e.proposal(t, s.ProposalID, s.Change.ParameterKey(), r.payload(s.Change))
	if s.Expiry != 0 {
		msg.ExpiryUnixSeconds = r.at(s.Expiry)
	}
	wantCode(t, "propose "+s.ProposalID, e.send(t, c, e.seats[0], msg), msgkit.OK)
	for i := 0; i < s.Approvals; i++ {
		seat := e.seats[i]
		wantCode(t, fmt.Sprintf("approve %s by %s", s.ProposalID, seat.name), e.send(t, c, seat, &authorityv1.MsgApproveChange{
			Signer: seat.addr, BusinessId: fmt.Sprintf("%s-ap-%d", s.ProposalID, i+1), ProposalId: s.ProposalID,
		}), msgkit.OK)
	}
	if !s.Execute {
		return
	}
	res := e.execute(t, c, s.ProposalID)
	wantCode(t, "execute "+s.ProposalID, res, msgkit.OK)
	if code := authorityResultCode(res); code != "" {
		t.Fatalf("execute %s kept with %s", s.ProposalID, code)
	}
	e.policy++
	if _, ok := s.Change.(scenario.SoftwareUpgrade); ok && c.height() != r.exec {
		t.Fatalf("the plan executed at height %d, the plan height was fixed for %d", c.height(), r.exec)
	}
}

// blocksTo commits empty blocks until the last committed height is to.
func (r *rehearsalNet) blocksTo(t *testing.T, to int64) {
	t.Helper()
	for r.c.height() < to {
		r.c.block(t)
	}
}

// census reads the scenario census from the committed state of chainApp:
// providers through Query/Provider, leases through the lease records, open
// offers through Query/Deadlines, evidence through Query/Evidence (state
// reported at the last block time), proposals through the proposal
// records, pending validator changes from the authority store (prefix
// 0x03), pending limit changes through Query/Policy, and governed values
// through the x/policy read surface.
func (r *rehearsalNet) census(t *testing.T, chainApp *app.App) scenario.Census {
	t.Helper()
	got := scenario.Census{}
	for _, id := range []string{scenario.ProviderAdmitted, scenario.ProviderDraining} {
		p, _ := queryProvider(t, chainApp, id, 0)
		switch p.GetState() {
		case providersv1.ProviderState_PROVIDER_STATE_ADMITTED:
			got[scenario.ProvidersAdmitted]++
		case providersv1.ProviderState_PROVIDER_STATE_DRAINING:
			got[scenario.ProvidersDraining]++
		}
	}
	for _, id := range []string{scenario.LeaseRenewed, scenario.LeaseCross, scenario.LeaseExpired, scenario.LeaseEnded, scenario.LeaseHeld} {
		lease, ok := chainApp.LeaseForTest(id)
		if !ok {
			t.Fatalf("census: no lease %s", id)
		}
		switch lease.GetState() {
		case leasesv1.LeaseState_LEASE_STATE_ACTIVE:
			got[scenario.LeasesActive]++
		case leasesv1.LeaseState_LEASE_STATE_RENEWED:
			got[scenario.LeasesRenewed]++
		case leasesv1.LeaseState_LEASE_STATE_EXPIRED:
			got[scenario.LeasesExpired]++
		case leasesv1.LeaseState_LEASE_STATE_ENDED:
			got[scenario.LeasesEnded]++
		}
	}
	_, _, open := deadlinePages(t, chainApp, math.MaxInt32, 20, 0)
	for _, offer := range open {
		if offer.GetOfferId() == scenario.OfferOpen || offer.GetOfferId() == scenario.OfferCross {
			got[scenario.OffersOpen]++
		}
	}
	for _, id := range []string{scenario.EvidenceRecorded, scenario.EvidenceRejected, scenario.EvidenceExpired, scenario.EvidenceCross} {
		switch recordState(t, chainApp, r.evidenceID(id), 0) {
		case stateRecorded:
			got[scenario.EvidenceRecordedClass]++
		case stateRejected:
			got[scenario.EvidenceRejectedClass]++
		case stateExpired:
			got[scenario.EvidenceExpiredClass]++
		}
	}
	for _, id := range []string{"admit-p1", "admit-p2", scenario.ProposalGoverned, scenario.ProposalPending, scenario.ProposalValidator,
		scenario.ProposalPlan, scenario.ProposalQueued, scenario.ProposalOpen} {
		state, found, err := chainApp.ProposalStateForTest(id)
		if err != nil || !found {
			t.Fatalf("census: proposal %s found %t: %v", id, found, err)
		}
		switch state {
		case authorityv1.ProposalState_PROPOSAL_STATE_OPEN:
			got[scenario.ProposalsOpen]++
		case authorityv1.ProposalState_PROPOSAL_STATE_QUEUED:
			got[scenario.ProposalsQueued]++
		case authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED:
			got[scenario.ProposalsExecuted]++
		}
	}
	got[scenario.PendingValidatorChanges] = len(storeRows(chainApp, authority.ModuleName, 0x03))
	got[scenario.PendingLimitChanges] = len(queryPolicy(t, chainApp).GetPendingLimitChanges())
	ctx := chainApp.CommittedContextForTest()
	for _, key := range governedKeys() {
		if _, found := chainApp.PolicyKeeperForTest().GovernedLimit(ctx, key); found {
			got[scenario.GovernedValues]++
		}
	}
	return got
}

func governedKeys() []string {
	return []string{msgkit.LimitMaxEvidenceBatch, msgkit.LimitMaxPageLimit, msgkit.LimitMaxOpenOffers, msgkit.LimitMaxExpirySteps, msgkit.LimitMaxPackageBytes}
}

// storeRows is every committed row of store name, as a key-to-value map;
// with prefixes, only the rows whose first key byte is one of them.
func storeRows(chainApp *app.App, name string, prefixes ...byte) map[string][]byte {
	store := chainApp.CommittedContextForTest().KVStore(chainApp.StoreKeyForTest(name))
	out := map[string][]byte{}
	it := store.Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		if len(prefixes) > 0 && bytes.IndexByte(prefixes, it.Key()[0]) < 0 {
			continue
		}
		out[string(it.Key())] = bytes.Clone(it.Value())
	}
	return out
}

// mountedStores is every store the app mounts: the auth store and the
// SnapshotStores.
func mountedStores() []string {
	return append([]string{"acc"}, app.SnapshotStores()...)
}

// storeDigests is the SHA-256 of ModuleSnapshot of every mounted store.
func storeDigests(chainApp *app.App) map[string]string {
	out := map[string]string{}
	for _, name := range mountedStores() {
		sum := sha256.Sum256(chainApp.ModuleSnapshot(name))
		out[name] = hex.EncodeToString(sum[:])
	}
	return out
}

// sameStoreDigests fails unless every node of c has the same per-store
// digests, and returns node 0's.
func sameStoreDigests(t *testing.T, label string, c *cluster) map[string]string {
	t.Helper()
	first := storeDigests(c.nodes[0].app)
	for i, node := range c.nodes[1:] {
		got := storeDigests(node.app)
		for _, name := range mountedStores() {
			if got[name] != first[name] {
				t.Fatalf("%s: node %d store %s digest %s, node 0 %s", label, i+1, name, got[name], first[name])
			}
		}
	}
	return first
}

// changedKeys returns, per store, the keys whose value differs between two
// row maps (added, removed, or changed), sorted.
func changedKeys(before, after map[string]map[string][]byte) map[string][]string {
	out := map[string][]string{}
	for name := range before {
		var keys []string
		for key, value := range before[name] {
			if got, ok := after[name][key]; !ok || !bytes.Equal(got, value) {
				keys = append(keys, hex.EncodeToString([]byte(key)))
			}
		}
		for key := range after[name] {
			if _, ok := before[name][key]; !ok {
				keys = append(keys, hex.EncodeToString([]byte(key)))
			}
		}
		if len(keys) > 0 {
			sort.Strings(keys)
			out[name] = keys
		}
	}
	return out
}

func allRows(chainApp *app.App) map[string]map[string][]byte {
	out := map[string]map[string][]byte{}
	for _, name := range app.SnapshotStores() {
		out[name] = storeRows(chainApp, name)
	}
	return out
}

// readers are read-only keepers over the app's own stores, for the
// invariant checkers.
type readers struct {
	authority authority.Keeper
	policy    policy.Keeper
	providers providers.Keeper
	content   content.Keeper
	leases    leases.Keeper
	evidence  evidence.Keeper
}

func readersOf(chainApp *app.App) readers {
	key := chainApp.StoreKeyForTest
	read := authority.NewKeeper(key(authority.ModuleName))
	pol := policy.NewKeeper(key(app.ModulePolicy), policy.Deps{Versions: read, Decisions: read})
	prov := providers.NewKeeper(key(app.ModuleProviders), providers.Deps{Roles: read, Limits: pol})
	cont := content.NewKeeper(key(app.ModuleContent), content.Deps{Roles: read, Params: read, Limits: pol})
	lease := leases.NewKeeper(key(app.ModuleLeases), leases.Deps{Roles: read, Limits: pol, Params: read, Policy: read, Providers: prov, Content: cont})
	ev := evidence.NewKeeper(key(app.ModuleEvidence), evidence.Deps{Roles: read, Limits: pol, Leases: lease, Content: cont})
	return readers{authority: read, policy: pol, providers: prov, content: cont, leases: lease, evidence: ev}
}

// checkInvariantsAt runs the lease (I1-I7, I9, I10), evidence, and policy
// invariant checkers on the committed state of chainApp, whose x/leases and
// x/evidence stores keep layout version layout. The checkers read layout 2
// only; the layout-3 stores of rehearsal.LeasesStep and EvidenceStep have
// the layout-2 rows with layout key 3, so for layout 3 the checkers run on
// a cache branch whose two layout keys are set back to 2 (the branch is
// discarded). The policy checker runs at the policy layout.
func checkInvariantsAt(t *testing.T, label string, chainApp *app.App, layout uint64) {
	t.Helper()
	ctx, _ := chainApp.CommittedContextForTest().CacheContext()
	for _, name := range []string{app.ModuleLeases, app.ModuleEvidence} {
		store := ctx.KVStore(chainApp.StoreKeyForTest(name))
		version, found, err := msgkit.ReadLayoutVersion(store)
		if err != nil || !found || version != layout {
			t.Fatalf("%s: %s layout %d found %t (%v), want %d", label, name, version, found, err, layout)
		}
		msgkit.WriteLayoutVersion(store, leases.ConsensusVersion)
	}
	rd := readersOf(chainApp)
	if err := leasestest.CheckInvariants(ctx, rd.leases, rd.providers, nil); err != nil {
		t.Fatalf("%s: lease invariants: %v", label, err)
	}
	if err := leasestest.CheckOfferIntegrity(ctx, rd.leases, rd.content); err != nil {
		t.Fatalf("%s: offer integrity: %v", label, err)
	}
	if err := evidencetest.CheckInvariants(ctx, rd.evidence, rd.leases); err != nil {
		t.Fatalf("%s: evidence invariants: %v", label, err)
	}
	if err := policytest.CheckInvariants(ctx, rd.policy, rd.authority); err != nil {
		t.Fatalf("%s: policy invariants: %v", label, err)
	}
}

// abciAppVersion is the app version ABCI Info reports.
func abciAppVersion(t *testing.T, chainApp *app.App) uint64 {
	t.Helper()
	info, err := chainApp.Info(&abci.RequestInfo{})
	if err != nil {
		t.Fatal(err)
	}
	return info.GetAppVersion()
}

// approveRaw is an approve of proposal by seat with business id bid,
// signed at the seat's next sequence.
func (r *rehearsalNet) approveRaw(t *testing.T, seat *actor, bid, proposal string) []byte {
	t.Helper()
	return r.e.signed(t, r.c, seat, registryGas, nil, &authorityv1.MsgApproveChange{Signer: seat.addr, BusinessId: bid, ProposalId: proposal})
}

// runToPlan runs the scenario steps and commits empty blocks to h-1.
func (r *rehearsalNet) runToPlan(t *testing.T) {
	t.Helper()
	r.runSteps(t, scenario.Steps())
	if r.h == 0 || r.h-r.exec < int64(msgkit.MinActivationLeadBlocks) {
		t.Fatalf("plan height %d, execute height %d: below the minimum lead", r.h, r.exec)
	}
	r.blocksTo(t, r.h-1)
}

// TestUpgradeRehearsalInProcess is the app-level rehearsal (plan 12.4) on
// two disk nodes that receive the same signed transactions:
//
//  1. The scenario (plan 12.3) runs under v1; the council schedules
//     rehearsal-1 through real transactions with h = execute height + the
//     minimum lead + 2 (MinActivationLeadBlocks, PROPOSED). h-1 commits
//     under v1 with the plan stored; ABCI Info reports app version 1. The
//     census at h-1 equals scenario.Want(BeforeUpgrade), every class above 0;
//     the lease, evidence, and policy invariants hold. App.Export is
//     refused. Simulate shows the QUEUED proposal's execute would succeed.
//  2. FinalizeBlock(h) under v1 returns UPGRADE "rehearsal-1" NEEDED on both
//     nodes; the finalize branch equals the committed state (nothing of h is
//     written) and the commit does not move.
//  3. A real restart with v1 is refused by load gate row 2.
//  4. A restart with v2 starts in the upgrade window (row 3).
//     FinalizeBlock(h) applies the upgrade on both nodes (wall time
//     logged): Version.App 2 in the block's consensus param update, the
//     same app hash on both nodes, and ABCI Info reports app version 2 after
//     the commit.
//  5. The state difference between h-1 and h is exactly the upgrade rows:
//     x/policy app version, the leases and evidence version map rows, the
//     plan (removed), the applied record (added); the consensus params row;
//     the authority policy version; the leases and evidence layout keys.
//  6. The census after the upgrade equals the one at h-1; invariants hold
//     at layout 3. The QUEUED proposal's execute at h+1 is STALE_VERSION
//     (owner ruling Q2, "Council plan, fail closed (Recommended)":
//     approved-but-unexecuted proposals become stale at the upgrade). At
//     h+2 a pre-upgrade council business id replays idempotently with no
//     second effect. Every crossing transition holds at effective-1 and
//     effective under v2; then the census equals
//     scenario.Want(AfterTransitions), and the pre-upgrade business id
//     with another proposal is REPLAY.
//  7. App.Export is refused after the upgrade with the same error; a
//     restart with v1 on the migrated state is refused by gate row 5; v2
//     restarts (row 1) and the nodes keep agreeing.
//  8. A two-phase replay of every block into a fresh database (v1 for
//     1..h-1, close, v2 for h..tip) reproduces every app hash.
//
// Per-store digests of every mounted store are equal on both nodes at h-1,
// h, and h+3. Block h+1 keeps the time of h (the time before a halt); h+2
// runs at AtResume (the first time after it).
func TestUpgradeRehearsalInProcess(t *testing.T) {
	r := newRehearsalNet(t, 2)
	e, c := r.e, r.c
	r.runToPlan(t)
	h := r.h
	t.Logf("plan rehearsal-1 executed at height %d, plan height h=%d (lead %d, minimum %d PROPOSED)", r.exec, h, h-r.exec, msgkit.MinActivationLeadBlocks)

	// 1. h-1 under v1.
	plan := queryPolicy(t, c.app()).GetUpgradePlan()
	if plan.GetName() != rehearsal.UpgradeName || plan.GetHeight() != uint64(h) || queryPolicy(t, c.app()).GetAppVersion() != 1 {
		t.Fatalf("at h-1 Query/Policy plan %v app version %d", plan, queryPolicy(t, c.app()).GetAppVersion())
	}
	for i, node := range c.nodes {
		if got := abciAppVersion(t, node.app); got != 1 {
			t.Fatalf("node %d ABCI app version %d at h-1", i, got)
		}
	}
	sameStoreDigests(t, "h-1", c)
	before := r.census(t, c.app())
	t.Logf("census at h-1=%d: %s", h-1, before)
	if err := before.Check(scenario.Want(scenario.BeforeUpgrade), scenario.BeforeUpgrade); err != nil {
		t.Fatal(err)
	}
	checkInvariantsAt(t, "h-1", c.app(), 2)
	_, exportBefore := c.app().Export()
	if exportBefore == nil || !strings.HasPrefix(exportBefore.Error(), "export refused: ") {
		t.Fatalf("export at h-1: %v", exportBefore)
	}
	queuedExec := e.signed(t, c, e.authority, registryGas, nil, &authorityv1.MsgExecuteChange{
		Signer: e.authority.addr, BusinessId: "ex-" + scenario.ProposalQueued, ProposalId: scenario.ProposalQueued})
	if _, _, err := c.app().Simulate(queuedExec); err != nil {
		t.Fatalf("at h-1 the QUEUED proposal's execute does not simulate: %v", err)
	}
	rowsBefore := allRows(c.app())

	// 2. FinalizeBlock(h) under v1 fails before writing.
	wantNeeded := fmt.Sprintf("UPGRADE %q NEEDED at height %d release_digest=", rehearsal.UpgradeName, h)
	for i, node := range c.nodes {
		committed := node.app.LastCommitID()
		snap := node.app.FullSnapshot()
		panicked, res, err := finalizeRecovering(node, node.when)
		if panicked != nil || err == nil || !strings.Contains(err.Error(), wantNeeded) {
			t.Fatalf("node %d: v1 FinalizeBlock(h) panic %v result %v err %v; want %q", i, panicked, res, err, wantNeeded)
		}
		if !bytes.Equal(node.app.FinalizeFullSnapshotForTest(), snap) {
			t.Fatalf("node %d: the failed block h wrote to the finalize branch", i)
		}
		if got := node.app.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
			t.Fatalf("node %d: the commit moved to %d", i, got.Version)
		}
		t.Logf("node %d v1 at h: %v", i, err)
	}

	// 3. and 4. Restart: v1 refused (row 2), v2 starts in the window (row 3).
	for i, d := range r.disks {
		d.refusedRestart(t, e.doc.ChainID, binaryV1, fmt.Sprintf("load gate at height %d refused this binary (row 2): upgrade %s is due at height %d", h-1, rehearsal.UpgradeName, h))
		chainApp, db, err := openBinary(t, d.dir, e.doc.ChainID, binaryV2)
		if err != nil {
			t.Fatalf("node %d: v2 at h-1: %v", i, err)
		}
		d.opened(chainApp, db)
		if got := chainApp.GateDecisionForTest(); got.Row != 3 || !got.Window || got.Upgrade.Name != rehearsal.UpgradeName {
			t.Fatalf("node %d: v2 gate decision %+v, want row 3 in the window", i, got)
		}
		if got := abciAppVersion(t, chainApp); got != 1 {
			t.Fatalf("node %d: ABCI app version %d in the window, want the committed 1", i, got)
		}
	}
	started := time.Now()
	res := c.block(t)
	t.Logf("FinalizeBlock(h) with the upgrade on %d nodes: %s wall time (both nodes, Commit included)", len(c.nodes), time.Since(started))
	if v := res.GetConsensusParamUpdates().GetVersion(); v == nil || v.GetApp() != rehearsal.ToAppVersion {
		t.Fatalf("block h consensus param update Version %v, want app %d", v, rehearsal.ToAppVersion)
	}
	upgradeEvents := 0
	for _, ev := range res.Events {
		if ev.Type == upgrade.EventUpgradeApplied {
			upgradeEvents++
		}
	}
	if upgradeEvents != 1 {
		t.Fatalf("block h has %d upgrade_applied events", upgradeEvents)
	}
	for i, node := range c.nodes {
		if got := abciAppVersion(t, node.app); got != rehearsal.ToAppVersion {
			t.Fatalf("node %d ABCI app version %d after the commit of h", i, got)
		}
	}
	state := queryPolicy(t, c.app())
	if state.GetAppVersion() != rehearsal.ToAppVersion || state.GetUpgradePlan() != nil {
		t.Fatalf("after h Query/Policy app version %d plan %v", state.GetAppVersion(), state.GetUpgradePlan())
	}
	sameStoreDigests(t, "h", c)

	// 5. The state difference at h is exactly the upgrade rows.
	changed := changedKeys(rowsBefore, allRows(c.app()))
	want := map[string][]string{
		app.ModulePolicy: sortedHex(policy.AppVersionKey(), policy.VersionMapKey(app.ModuleEvidence), policy.VersionMapKey(app.ModuleLeases),
			policy.PlanKey(), policy.AppliedKey(rehearsal.UpgradeName)),
		app.ModuleSVN:        sortedHex([]byte{0x03}),
		authority.ModuleName: sortedHex([]byte{0x04}),
		app.ModuleLeases:     sortedHex(msgkit.LayoutVersionKey),
		app.ModuleEvidence:   sortedHex(msgkit.LayoutVersionKey),
	}
	if fmt.Sprint(changed) != fmt.Sprint(want) {
		t.Fatalf("rows changed at h:\n got %v\nwant %v", changed, want)
	}

	// 6. After the upgrade: census, invariants, STALE, business id replay.
	after := r.census(t, c.app())
	t.Logf("census at h=%d: %s", h, after)
	if err := after.Check(scenario.Want(scenario.AfterUpgrade), scenario.AfterUpgrade); err != nil {
		t.Fatal(err)
	}
	checkInvariantsAt(t, "h", c.app(), 3)
	wantCode(t, "execute the QUEUED proposal after the upgrade", e.deliverRaw(t, c, e.authority, queuedExec), msgkit.StaleVersion)
	if state, _, _ := c.app().ProposalStateForTest(scenario.ProposalQueued); state != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatalf("the QUEUED proposal is %s after its stale execute", state)
	}
	// h+2 is the first block after the halt: its time jumps to AtResume.
	c.setUnix(r.at(scenario.AtResume))
	full := c.app().FullSnapshot()
	replay := e.deliverRaw(t, c, e.seats[0], r.approveRaw(t, e.seats[0], scenario.ProposalQueued+"-ap-1", scenario.ProposalQueued))
	wantCode(t, "pre-upgrade approve business id after the upgrade", replay, msgkit.OK)
	if !idempotentResult(replay) {
		t.Fatalf("the pre-upgrade business id replay is not idempotent: %v", replay.Events)
	}
	if !bytes.Equal(full, c.app().FullSnapshot()) {
		t.Fatal("the pre-upgrade business id replay changed the state")
	}
	if c.height() != h+2 {
		t.Fatalf("height %d, want h+2", c.height())
	}
	r.transitions(t)
	wantCode(t, "pre-upgrade business id for another proposal", e.deliverRaw(t, c, e.seats[0],
		r.approveRaw(t, e.seats[0], scenario.ProposalQueued+"-ap-1", scenario.ProposalOpen)), msgkit.Replay)
	final := r.census(t, c.app())
	t.Logf("census at %d after the transitions: %s", c.height(), final)
	if err := final.Check(scenario.Want(scenario.AfterTransitions), scenario.AfterTransitions); err != nil {
		t.Fatal(err)
	}
	checkInvariantsAt(t, "after the transitions", c.app(), 3)

	// 7. Export after the upgrade; restart with v1 refused, with v2 started.
	_, exportAfter := c.app().Export()
	if exportAfter == nil || exportAfter.Error() != exportBefore.Error() {
		t.Fatalf("export after the upgrade: %v; at h-1: %v", exportAfter, exportBefore)
	}
	tip := c.height()
	r.disks[0].refusedRestart(t, e.doc.ChainID, binaryV1, fmt.Sprintf("load gate at height %d refused this binary (row 5): state is newer than this binary (app version 2, binary 1)", tip))
	chainApp, db, err := openBinary(t, r.disks[0].dir, e.doc.ChainID, binaryV2)
	if err != nil {
		t.Fatal(err)
	}
	r.disks[0].opened(chainApp, db)
	if got := chainApp.GateDecisionForTest(); got.Row != 1 || got.Window {
		t.Fatalf("v2 restart after the upgrade: gate %+v, want row 1", got)
	}
	c.block(t)
	c.block(t)

	// 8. Two-phase replay into a fresh database.
	r.replayTwoPhase(t)
}

// sortedHex is the hex of each key, sorted.
func sortedHex(keys ...[]byte) []string {
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = hex.EncodeToString(key)
	}
	sort.Strings(out)
	return out
}

// idempotentResult reports whether a result carries authority_result
// idempotent=true.
func idempotentResult(res *abci.ExecTxResult) bool {
	for _, ev := range res.Events {
		if ev.Type != "authority_result" {
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

// transitions asserts every scenario transition at effective-1 and at
// effective, from height h+3 on: the governed activation (h+3), the
// validator change (h+4), then each crossing time.
func (r *rehearsalNet) transitions(t *testing.T) {
	t.Helper()
	e, c := r.e, r.c
	ctx := func() sdk.Context { return c.app().CommittedContextForTest() }
	for _, tr := range scenario.Transitions() {
		switch tr.Object {
		case scenario.PendingKey:
			at := r.h + int64(tr.At)
			if c.height() != at-1 {
				t.Fatalf("%s: height %d, want %d", tr.Object, c.height(), at-1)
			}
			if _, found := c.app().PolicyKeeperForTest().GovernedLimit(ctx(), tr.Object); found || len(queryPolicy(t, c.app()).GetPendingLimitChanges()) != 1 {
				t.Fatalf("%s at a-1=%d: a value is set or the change is not pending", tr.Object, at-1)
			}
			c.block(t)
			value, found := c.app().PolicyKeeperForTest().GovernedLimit(ctx(), tr.Object)
			if !found || fmt.Sprint(value) != tr.After || len(queryPolicy(t, c.app()).GetPendingLimitChanges()) != 0 {
				t.Fatalf("%s at a=%d: value %d found %t", tr.Object, at, value, found)
			}
		case scenario.ValidatorNode:
			at := r.h + int64(tr.At)
			if c.height() != at-1 || len(c.nodes[0].last.ValidatorUpdates) != 0 || len(storeRows(c.app(), authority.ModuleName, 0x03)) != 1 {
				t.Fatalf("%s at %d: updates %v", tr.Object, c.height(), c.nodes[0].last.ValidatorUpdates)
			}
			res := c.block(t)
			if len(res.ValidatorUpdates) != 1 || res.ValidatorUpdates[0].Power != scenario.ValidatorPowerN || len(storeRows(c.app(), authority.ModuleName, 0x03)) != 0 {
				t.Fatalf("%s at %d: updates %v", tr.Object, c.height(), res.ValidatorUpdates)
			}
		case scenario.EvidenceCross:
			id := r.evidenceID(tr.Object)
			c.blockAt(t, unixTime(r.at(tr.Effective)-1))
			wantState(t, tr.Object+" at effective-1", c.app(), id, 0, stateRecorded)
			c.blockAt(t, unixTime(r.at(tr.Effective)))
			wantState(t, tr.Object+" at effective", c.app(), id, 0, stateExpired)
		case scenario.OfferCross:
			c.blockAt(t, unixTime(r.at(tr.Effective)-1))
			if !offerOpen(t, c.app(), tr.Object) {
				t.Fatalf("%s closed at effective-1", tr.Object)
			}
			c.blockAt(t, unixTime(r.at(tr.Effective)))
			if offerOpen(t, c.app(), tr.Object) {
				t.Fatalf("%s open at effective", tr.Object)
			}
		case scenario.LeaseCross:
			c.blockAt(t, unixTime(r.at(tr.Effective)-1))
			if lease, _ := c.app().LeaseForTest(tr.Object); lease.GetState() != leasesv1.LeaseState_LEASE_STATE_ACTIVE {
				t.Fatalf("%s at effective-1: %s", tr.Object, lease.GetState())
			}
			c.blockAt(t, unixTime(r.at(tr.Effective)))
			if lease, _ := c.app().LeaseForTest(tr.Object); lease.GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED {
				t.Fatalf("%s at effective: %s", tr.Object, lease.GetState())
			}
		case scenario.ProposalOpen:
			c.setUnix(r.at(tr.Effective) - 1)
			wantCode(t, "approve the OPEN proposal at effective-1", e.deliverRaw(t, c, e.seats[2],
				r.approveRaw(t, e.seats[2], scenario.ProposalOpen+"-ap-3", scenario.ProposalOpen)), msgkit.OK)
			if state, _, _ := c.app().ProposalStateForTest(tr.Object); state.String() != "PROPOSAL_STATE_"+tr.Before {
				t.Fatalf("%s at effective-1: %s", tr.Object, state)
			}
			c.setUnix(r.at(tr.Effective))
			res := e.execute(t, c, tr.Object)
			wantCode(t, "execute the proposal at its expiry", res, msgkit.OK)
			if authorityResultCode(res) != string(msgkit.Expired) {
				t.Fatalf("execute at the expiry: result %q", authorityResultCode(res))
			}
			if state, _, _ := c.app().ProposalStateForTest(tr.Object); state.String() != "PROPOSAL_STATE_"+tr.After {
				t.Fatalf("%s at effective: %s", tr.Object, state)
			}
		default:
			t.Fatalf("transition %s has no check", tr.Object)
		}
		t.Logf("transition %s: %s -> %s (class %s) at height %d", tr.Object, tr.Before, tr.After, tr.Class, c.height())
		if c.height() == r.h+3 {
			sameStoreDigests(t, "h+3", c)
		}
	}
}

// offerOpen reports whether offer id is open at the last committed block.
func offerOpen(t *testing.T, chainApp *app.App, id string) bool {
	t.Helper()
	_, _, open := deadlinePages(t, chainApp, math.MaxInt32, 20, 0)
	for _, offer := range open {
		if offer.GetOfferId() == id {
			return true
		}
	}
	return false
}

// replayTwoPhase replays every committed block of r into a fresh database:
// v1 for heights 1..h-1, then a restart with v2 (gate row 3) for h..tip.
// Every app hash must equal the cluster's.
func (r *rehearsalNet) replayTwoPhase(t *testing.T) {
	t.Helper()
	e, c := r.e, r.c
	dir := t.TempDir()
	chainApp, db, err := openBinary(t, dir, e.doc.ChainID, binaryV1)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := app.AppStateFromDocument(chainApp.EncodingConfig().Codec, e.doc)
	if err != nil {
		t.Fatal(err)
	}
	params := consensusParams()
	if _, err := chainApp.InitChain(&abci.RequestInitChain{
		ChainId: e.doc.ChainID, Time: e.doc.Time(), AppStateBytes: mustJSON(t, modules),
		InitialHeight: 1, Validators: validatorUpdates(t, e.doc), ConsensusParams: &params,
	}); err != nil {
		t.Fatal(err)
	}
	for i, in := range c.inputs {
		if in.height == r.h {
			if err := chainApp.Close(); err != nil {
				t.Fatal(err)
			}
			_ = db.Close()
			if chainApp, db, err = openBinary(t, dir, e.doc.ChainID, binaryV2); err != nil {
				t.Fatalf("replay: v2 at h-1: %v", err)
			}
			if got := chainApp.GateDecisionForTest(); got.Row != 3 {
				t.Fatalf("replay: v2 gate %+v", got)
			}
		}
		if _, err := chainApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: in.height, Time: in.time, Hash: bytes32(byte(in.height)), Txs: in.txs}); err != nil {
			t.Fatalf("replay height %d: %v", in.height, err)
		}
		if _, err := chainApp.Commit(); err != nil {
			t.Fatal(err)
		}
		if got := chainApp.LastCommitID().Hash; !bytes.Equal(got, c.hashes[i]) {
			t.Fatalf("replay height %d: app hash %X, cluster %X", in.height, got, c.hashes[i])
		}
	}
	t.Logf("two-phase replay reproduced %d app hashes (v1 1..%d, v2 %d..%d)", len(c.inputs), r.h-1, r.h, c.inputs[len(c.inputs)-1].height)
	_ = chainApp.Close()
	_ = db.Close()
}

// planSteps is the scenario reduced to the plan: the council schedules
// rehearsal-1 through real transactions and nothing else.
func planSteps() []scenario.Step {
	return []scenario.Step{
		scenario.SetTime{Name: "time-plan", At: scenario.AtPlan},
		scenario.Council{Name: "plan", ProposalID: scenario.ProposalPlan, Change: scenario.SoftwareUpgrade{Name: rehearsal.UpgradeName}, Approvals: 3, Execute: true},
	}
}

// newPlanNet is a rehearsal net of nodes disk nodes on which the council
// scheduled rehearsal-1 (planSteps); the last committed height is the
// plan's execute height.
func newPlanNet(t *testing.T, nodes int) *rehearsalNet {
	t.Helper()
	r := newRehearsalNet(t, nodes)
	r.runSteps(t, planSteps())
	return r
}

// gateRow restarts d with kind and requires the load gate to start it with
// row (row 3: in the upgrade window).
func gateRow(t *testing.T, d *diskNode, chainID string, kind binaryKind, row int) {
	t.Helper()
	d.restart(t, chainID, kind)
	if got := d.run.app.GateDecisionForTest(); got.Row != row || got.Window != (row == 3) {
		t.Fatalf("%s: gate decision %+v, want row %d", kind, got, row)
	}
	t.Logf("%s at height %d: gate row %d", kind, d.run.height, row)
}

// TestLoadGateTable runs every gate row of plan 11.4 on a real reopened
// goleveldb database, along one chain:
//
//   - height 1, no plan: v1 starts (row 1); v2 is refused (row 6: one app
//     version behind and no plan).
//   - after the plan's execute (plan above L+1): v1 starts (row 1); v2 is
//     refused (row 4: too early).
//   - at h-1: v1 is refused (row 2: the upgrade is due); v2 starts in the
//     upgrade window (row 3) and applies the upgrade at h.
//   - after h: v2 starts (row 1); v1 is refused (row 5: state is newer).
func TestLoadGateTable(t *testing.T) {
	r := newRehearsalNet(t, 1)
	d, id := r.disks[0], r.e.doc.ChainID
	refusal := func(row int, text string) string {
		return fmt.Sprintf("load gate at height %d refused this binary (row %d): %s", d.run.height, row, text)
	}
	gateRow(t, d, id, binaryV1, 1)
	d.refusedRestart(t, id, binaryV2, refusal(6, "stored app version 1 is one behind this binary (2) and no upgrade plan is stored"))
	gateRow(t, d, id, binaryV1, 1)

	r.runSteps(t, planSteps())
	gateRow(t, d, id, binaryV1, 1)
	d.refusedRestart(t, id, binaryV2, refusal(4, fmt.Sprintf("too early: run the previous binary until height %d", r.h-1)))
	gateRow(t, d, id, binaryV1, 1)

	r.blocksTo(t, r.h-1)
	d.refusedRestart(t, id, binaryV1, refusal(2, fmt.Sprintf("upgrade %s is due at height %d", rehearsal.UpgradeName, r.h)))
	gateRow(t, d, id, binaryV2, 3)
	r.c.block(t)

	gateRow(t, d, id, binaryV2, 1)
	d.refusedRestart(t, id, binaryV1, refusal(5, "state is newer than this binary (app version 2, binary 1)"))
	gateRow(t, d, id, binaryV2, 1)
	r.c.block(t)
}

// TestPreBlockRefusesAppVersionMismatch: the per-block app-version rule
// (deviation D-6) holds even when the load gate is bypassed. A v2 app
// loaded with LoadLatestVersion only (no gate) on state at app version 1,
// below the plan height, fails FinalizeBlock with "stored app version 1,
// binary 2: wrong binary"; a v1 app loaded the same way on upgraded state
// fails with "stored app version 2, binary 1". Each time the finalize
// branch equals the committed state and the commit does not move.
func TestPreBlockRefusesAppVersionMismatch(t *testing.T) {
	r := newPlanNet(t, 1)
	d, id := r.disks[0], r.e.doc.ChainID
	ungated := func(kind binaryKind) {
		t.Helper()
		d.close(t)
		db := openDB(t, d.dir)
		var chainApp *app.App
		var err error
		if kind == binaryV1 {
			chainApp, err = app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: id, SkipLoad: true})
		} else {
			chainApp, err = upgradeApp(t, db, id, rehearsal.Upgrades(), rehearsal.LeasesStep)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := chainApp.LoadLatestVersion(); err != nil {
			t.Fatal(err)
		}
		d.opened(chainApp, db)
	}
	refused := func(stored, binary uint64) {
		t.Helper()
		committed, snap := d.run.app.LastCommitID(), d.run.app.FullSnapshot()
		want := fmt.Sprintf("stored app version %d, binary %d: wrong binary for height %d", stored, binary, d.run.height+1)
		panicked, _, err := finalizeRecovering(d.run, d.run.when)
		if panicked != nil || err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("FinalizeBlock(%d): panic %v err %v; want %q", d.run.height+1, panicked, err, want)
		}
		if !bytes.Equal(d.run.app.FinalizeFullSnapshotForTest(), snap) {
			t.Fatal("the refused block wrote to the finalize branch")
		}
		if got := d.run.app.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
			t.Fatalf("the commit moved to %d", got.Version)
		}
		t.Logf("refused: %v", err)
	}
	ungated(binaryV2)
	refused(1, 2)
	gateRow(t, d, id, binaryV1, 1)
	r.blocksTo(t, r.h-1)
	gateRow(t, d, id, binaryV2, 3)
	r.c.block(t)
	r.c.block(t)
	ungated(binaryV1)
	refused(2, 1)
}

// TestNewerBinaryRefusesInitChain: the v2 app (app version 2) refuses to
// initialize a chain from the devnet genesis (case I7 at InitChain): a new
// node starts with the genesis binary. No module store holds a row after
// the refusal; the svn store holds only the consensus params row BaseApp
// writes before it calls the InitChainer.
func TestNewerBinaryRefusesInitChain(t *testing.T) {
	e := newEvNet()
	chainApp, db, err := openBinary(t, t.TempDir(), e.doc.ChainID, binaryV2)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer chainApp.Close()
	modules, err := app.AppStateFromDocument(chainApp.EncodingConfig().Codec, e.doc)
	if err != nil {
		t.Fatal(err)
	}
	params := consensusParams()
	_, err = chainApp.InitChain(&abci.RequestInitChain{
		ChainId: e.doc.ChainID, Time: e.doc.Time(), AppStateBytes: mustJSON(t, modules),
		InitialHeight: 1, Validators: validatorUpdates(t, e.doc), ConsensusParams: &params,
	})
	want := "this binary runs app version 2, and a chain starts at app version 1"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("InitChain with v2: %v; want %q", err, want)
	}
	for _, name := range mountedStores() {
		rows, wantRows := chainApp.FinalizeStoreRowsForTest(name), 0
		if name == app.ModuleSVN {
			wantRows = 1
		}
		if rows != wantRows {
			t.Fatalf("store %s holds %d rows after the refused InitChain, want %d", name, rows, wantRows)
		}
	}
	t.Logf("InitChain refused: %v", err)
}

// TestStoreAbsentFromRootRefused (case I5): a binary that mounts a store the
// committed root does not hold fails at LoadLatestVersion, before the load
// gate; svnd exits 1 there. The goal-10 binaries add no store
// (StoreUpgrades are deferred, DR-10-33).
func TestStoreAbsentFromRootRefused(t *testing.T) {
	r := newRehearsalNet(t, 1)
	d, id := r.disks[0], r.e.doc.ChainID
	d.close(t)
	db := openDB(t, d.dir)
	defer db.Close()
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: id, SkipLoad: true})
	if err != nil {
		t.Fatal(err)
	}
	chainApp.MountStoreForTest("rehearsal")
	err = chainApp.LoadForTest()
	if err == nil || !strings.Contains(err.Error(), "version of store rehearsal mismatch root store's version") {
		t.Fatalf("load with an extra store: %v", err)
	}
	t.Logf("refused: %v", err)
}

// failingUpgrades is rehearsal-1 with a handler that runs the module
// migrations (which write the leases and evidence stores in the runner's
// cache) and then returns an error.
func failingUpgrades() []upgrade.Upgrade {
	return []upgrade.Upgrade{{
		Name: rehearsal.UpgradeName, FromAppVersion: rehearsal.FromAppVersion, ToAppVersion: rehearsal.ToAppVersion,
		Handler: func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
			if _, err := migrate(ctx, from); err != nil {
				return nil, err
			}
			return nil, errors.New("synthetic rehearsal handler failure")
		},
	}}
}

// TestUpgradeHandlerErrorHalts (case I8): a handler that fails on every
// node halts every node at h identically. The handler's migrations wrote
// the leases and evidence stores in the runner's cache, yet the finalize
// branch equals the committed state (nothing of h is written) and the
// commit does not move. After a real restart the same binary starts in the
// window again and fails h again with the same error (the plan stays due).
// A corrected binary for the same plan name then applies the upgrade and
// both nodes agree.
func TestUpgradeHandlerErrorHalts(t *testing.T) {
	r := newPlanNet(t, 2)
	id := r.e.doc.ChainID
	r.blocksTo(t, r.h-1)
	want := fmt.Sprintf("upgrade %s at height %d: synthetic rehearsal handler failure", rehearsal.UpgradeName, r.h)
	for attempt := 1; attempt <= 2; attempt++ {
		var errs []string
		for i, d := range r.disks {
			d.close(t)
			chainApp, db, err := openUpgrade(t, d.dir, id, failingUpgrades(), rehearsal.LeasesStep)
			if err != nil {
				t.Fatalf("attempt %d node %d: %v", attempt, i, err)
			}
			d.opened(chainApp, db)
			if got := chainApp.GateDecisionForTest(); got.Row != 3 {
				t.Fatalf("attempt %d node %d: gate %+v", attempt, i, got)
			}
			committed, snap := chainApp.LastCommitID(), chainApp.FullSnapshot()
			panicked, _, err := finalizeRecovering(d.run, d.run.when)
			if panicked != nil || err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("attempt %d node %d: panic %v err %v; want %q", attempt, i, panicked, err, want)
			}
			if !bytes.Equal(chainApp.FinalizeFullSnapshotForTest(), snap) {
				t.Fatalf("attempt %d node %d: the failed upgrade wrote to the finalize branch", attempt, i)
			}
			if got := chainApp.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
				t.Fatalf("attempt %d node %d: the commit moved", attempt, i)
			}
			errs = append(errs, err.Error())
		}
		if errs[0] != errs[1] {
			t.Fatalf("attempt %d: the nodes failed differently: %q vs %q", attempt, errs[0], errs[1])
		}
		t.Logf("attempt %d: both nodes halted at h=%d: %s", attempt, r.h, errs[0])
	}
	for _, d := range r.disks {
		gateRow(t, d, id, binaryV2, 3)
	}
	r.c.block(t)
	if got := queryPolicy(t, r.c.app()).GetAppVersion(); got != rehearsal.ToAppVersion {
		t.Fatalf("app version %d after the corrected binary", got)
	}
}

// protectedWrite is one write a wrong module migration makes into a row
// the upgrade runner protects, and the error that refuses it.
type protectedWrite struct {
	name  string
	write func(t *testing.T, chainApp *app.App, ctx sdk.Context)
	want  string
}

// rowWrite adds the row key -> 0x01 to store name.
func rowWrite(name string, key []byte) func(*testing.T, *app.App, sdk.Context) {
	return func(_ *testing.T, chainApp *app.App, ctx sdk.Context) {
		ctx.KVStore(chainApp.StoreKeyForTest(name)).Set(key, []byte{0x01})
	}
}

// writeSite is where a wrong upgrade makes its protected write.
type writeSite int

const (
	// inMigration: in the x/leases module migration, after
	// rehearsal.LeasesStep.
	inMigration writeSite = iota
	// inHandler: in the upgrade handler, after rehearsal.Handler, through
	// the handler environment the runner gives it (envWrite).
	inHandler
)

// refuseWrongMigrations is refuseWrongUpgrades with every write made in the
// x/leases module migration.
func refuseWrongMigrations(t *testing.T, writes []protectedWrite) {
	t.Helper()
	refuseWrongUpgrades(t, inMigration, writes)
}

// refuseWrongUpgrades restarts the one node of r (at h-1) once per write
// with a binary that makes the write at site: in its x/leases migration
// (rehearsal.LeasesStep, then the write) or in its upgrade handler
// (rehearsal.Handler, then the write). FinalizeBlock(h) must fail with the
// write's error, write nothing to the finalize branch, and leave the
// commit. After every write, the honest v2 binary applies the upgrade on the
// same state.
func refuseWrongUpgrades(t *testing.T, site writeSite, writes []protectedWrite) {
	t.Helper()
	r := newPlanNet(t, 1)
	d, id := r.disks[0], r.e.doc.ChainID
	r.blocksTo(t, r.h-1)
	for _, w := range writes {
		d.close(t)
		var target *app.App
		step := func(key storetypes.StoreKey) module.MigrationHandler {
			honest := rehearsal.LeasesStep(key)
			return func(ctx sdk.Context) error {
				if err := honest(ctx); err != nil {
					return err
				}
				if site == inMigration {
					w.write(t, target, ctx)
				}
				return nil
			}
		}
		upgrades := rehearsal.Upgrades()
		if site == inHandler {
			upgrades[0].Handler = func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
				migrated, err := rehearsal.Handler(ctx, from, migrate)
				if err != nil {
					return nil, err
				}
				w.write(t, target, ctx)
				return migrated, nil
			}
		}
		chainApp, db, err := openUpgrade(t, d.dir, id, upgrades, step)
		if err != nil {
			t.Fatalf("%s: %v", w.name, err)
		}
		target = chainApp
		d.opened(chainApp, db)
		committed, snap := chainApp.LastCommitID(), chainApp.FullSnapshot()
		panicked, _, err := finalizeRecovering(d.run, d.run.when)
		if panicked != nil || err == nil || !strings.Contains(err.Error(), w.want) {
			t.Fatalf("%s: panic %v err %v; want %q", w.name, panicked, err, w.want)
		}
		if !bytes.Equal(chainApp.FinalizeFullSnapshotForTest(), snap) {
			t.Fatalf("%s: the refused upgrade wrote to the finalize branch", w.name)
		}
		if got := chainApp.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
			t.Fatalf("%s: the commit moved", w.name)
		}
		t.Logf("%s refused: %v", w.name, err)
	}
	gateRow(t, d, id, binaryV2, 3)
	r.c.block(t)
}

// changedClass is the runner's refusal of a changed I13 row class.
func changedClass(class string) string {
	return fmt.Sprintf("upgrade %s changed %s", rehearsal.UpgradeName, class)
}

// TestMigrationCannotChangeAuthorityRows: for every I13 row class of the
// upgrade runner (the authority prefixes, the other authority rows, the
// x/policy prefixes, and the other x/policy rows), a module migration that
// adds one row in that class is refused with "upgrade rehearsal-1 changed
// <class>" and nothing of h is written. The class list is the app's own
// (PostConditionNamesForTest); a class without a write here fails the test.
func TestMigrationCannotChangeAuthorityRows(t *testing.T) {
	keys := map[string][2]any{
		"authority layout version":            {authority.ModuleName, []byte{0x00, 0x7A}},
		"authority config":                    {authority.ModuleName, []byte{0x01, 0x7A}},
		"authority admission rows":            {authority.ModuleName, []byte{0x02, 0x7A}},
		"authority pending validator changes": {authority.ModuleName, []byte{0x03, 0x7A}},
		"authority proposals":                 {authority.ModuleName, []byte{0x10, 0x7A}},
		"authority business ids":              {authority.ModuleName, []byte{0x11, 0x7A}},
		"authority executed digests":          {authority.ModuleName, []byte{0x12, 0x7A}},
		"authority open counts":               {authority.ModuleName, []byte{0x14, 0x7A}},
		"authority retired seat keys":         {authority.ModuleName, []byte{0x15, 0x7A}},
		"authority retired service addresses": {authority.ModuleName, []byte{0x16, 0x7A}},
		"authority other rows":                {authority.ModuleName, []byte{0x20, 0x7A}},
		"policy safety caps":                  {app.ModulePolicy, []byte{0x01, 0x7A}},
		"policy governed values":              {app.ModulePolicy, []byte{policy.GovernedPrefix, 0x7A}},
		"policy pending changes":              {app.ModulePolicy, []byte{policy.PendingPrefix, 0x7A}},
		"policy applied upgrades":             {app.ModulePolicy, policy.AppliedKey("ghost")},
		"policy plan names":                   {app.ModulePolicy, policy.PlanNameKey("future")},
		"policy other rows":                   {app.ModulePolicy, []byte{0x09}},
	}
	names := newTestApp(t, "shortvideo-dev-names", nil).PostConditionNamesForTest()
	var writes []protectedWrite
	for _, class := range names {
		key, ok := keys[class]
		if !ok {
			t.Fatalf("I13 class %q has no write in this test", class)
		}
		writes = append(writes, protectedWrite{name: class, write: rowWrite(key[0].(string), key[1].([]byte)), want: changedClass(class)})
	}
	if len(writes) != len(keys) {
		t.Fatalf("%d I13 classes, %d writes", len(names), len(keys))
	}
	refuseWrongMigrations(t, writes)
}

// envWrite sets key to value in store name through the handler
// environment of ctx (upgrade.EnvOf), or deletes key when value is nil.
func envWrite(name string, key, value []byte) func(*testing.T, *app.App, sdk.Context) {
	return func(t *testing.T, _ *app.App, ctx sdk.Context) {
		env, found := upgrade.EnvOf(ctx)
		if !found {
			t.Error("the handler context carries no handler environment")
			return
		}
		store, found := env.KVStore(ctx, name)
		if !found {
			t.Errorf("the handler environment has no store %s", name)
			return
		}
		if value == nil {
			store.Delete(key)
			return
		}
		store.Set(key, value)
	}
}

// TestUpgradeHandlerEnvCannotChangeProtectedRows (ORCH-7): an upgrade handler that
// writes through its handler environment meets the runner's checks
// unchanged. For every I13 row class (the app's own list,
// PostConditionNamesForTest) a handler that adds one row of that class is
// refused with "upgrade rehearsal-1 changed <class>", and a handler that
// writes a row the runner owns (the authority policy version, the x/policy
// app version and plan, the consensus params) is refused with the runner's
// own message; nothing of h is written, the commit does not move, and the
// honest v2 binary then applies on the same state. A handler write outside
// every check applies: TestWrongMigrationDiverges (B2-bad).
func TestUpgradeHandlerEnvCannotChangeProtectedRows(t *testing.T) {
	keys := map[string][2]any{
		"authority layout version":            {authority.ModuleName, []byte{0x00, 0x7A}},
		"authority config":                    {authority.ModuleName, []byte{0x01, 0x7A}},
		"authority admission rows":            {authority.ModuleName, []byte{0x02, 0x7A}},
		"authority pending validator changes": {authority.ModuleName, []byte{0x03, 0x7A}},
		"authority proposals":                 {authority.ModuleName, []byte{0x10, 0x7A}},
		"authority business ids":              {authority.ModuleName, []byte{0x11, 0x7A}},
		"authority executed digests":          {authority.ModuleName, []byte{0x12, 0x7A}},
		"authority open counts":               {authority.ModuleName, []byte{0x14, 0x7A}},
		"authority retired seat keys":         {authority.ModuleName, []byte{0x15, 0x7A}},
		"authority retired service addresses": {authority.ModuleName, []byte{0x16, 0x7A}},
		"authority other rows":                {authority.ModuleName, []byte{0x04, 0x7A}},
		"policy safety caps":                  {app.ModulePolicy, []byte{0x01, 0x7A}},
		"policy governed values":              {app.ModulePolicy, []byte{policy.GovernedPrefix, 0x7A}},
		"policy pending changes":              {app.ModulePolicy, []byte{policy.PendingPrefix, 0x7A}},
		"policy applied upgrades":             {app.ModulePolicy, policy.AppliedKey("ghost")},
		"policy plan names":                   {app.ModulePolicy, policy.PlanNameKey("future")},
		"policy other rows":                   {app.ModulePolicy, []byte{0x06, 0x7A}},
	}
	names := newTestApp(t, "shortvideo-dev-env-names", nil).PostConditionNamesForTest()
	var writes []protectedWrite
	for _, class := range names {
		key, ok := keys[class]
		if !ok {
			t.Fatalf("I13 class %q has no write in this test", class)
		}
		writes = append(writes, protectedWrite{name: class, write: envWrite(key[0].(string), key[1].([]byte), []byte{0x01}), want: changedClass(class)})
	}
	if len(writes) != len(keys) {
		t.Fatalf("%d I13 classes, %d writes", len(names), len(keys))
	}
	writes = append(writes,
		protectedWrite{name: "authority policy version", write: envWrite(authority.ModuleName, []byte{0x04}, policy.EncodeU64(99)),
			want: fmt.Sprintf("upgrade %s handler changed the policy version from", rehearsal.UpgradeName)},
		protectedWrite{name: "policy app version", write: envWrite(app.ModulePolicy, policy.AppVersionKey(), policy.EncodeU64(rehearsal.ToAppVersion)),
			want: fmt.Sprintf("upgrade %s handler changed the stored app version from 1 to 2", rehearsal.UpgradeName)},
		protectedWrite{name: "policy plan deleted", write: envWrite(app.ModulePolicy, policy.PlanKey(), nil),
			want: fmt.Sprintf("upgrade %s handler changed or removed the upgrade plan", rehearsal.UpgradeName)},
		protectedWrite{name: "block max gas raised", write: func(t *testing.T, _ *app.App, ctx sdk.Context) {
			env, _ := upgrade.EnvOf(ctx)
			store, _ := env.KVStore(ctx, app.ModuleSVN)
			var params cmtproto.ConsensusParams
			if err := params.Unmarshal(store.Get([]byte{0x03})); err != nil || params.Block == nil {
				t.Errorf("consensus params: %v", err)
				return
			}
			params.Block.MaxGas++
			raw, err := params.Marshal()
			if err != nil {
				t.Error(err)
				return
			}
			store.Set([]byte{0x03}, raw)
		}, want: fmt.Sprintf("upgrade %s handler changed the consensus params", rehearsal.UpgradeName)},
	)
	refuseWrongUpgrades(t, inHandler, writes)
}

// TestMigrationCannotAddRowsBesideThePolicyVersion: the runner checks the
// authority policy version key (0x04) itself, so only that exact key is left
// out of "authority other rows". A module migration that adds a longer key
// starting with 0x04 is refused as a change of "authority other rows".
func TestMigrationCannotAddRowsBesideThePolicyVersion(t *testing.T) {
	refuseWrongMigrations(t, []protectedWrite{{
		name:  "row under the policy version prefix",
		write: rowWrite(authority.ModuleName, []byte{0x04, 0x7A}),
		want:  changedClass("authority other rows"),
	}})
}

// TestUpgradeCannotChangePlanNameOrRunnerKeyRows (security review, Wave 2):
// the plan-name rows (0x08) hold the rule of the owner's Q3 option "3 of 4 +
// min notice (Recommended)" that a plan name can never be reused with a
// different release digest, so a handler that deletes the plan-name row of
// the upgrade being applied, or rewrites it to another digest, is refused
// ("policy plan names"). A handler that adds an applied row (0x07, the
// ORCH-10 rule that an applied name is DUPLICATE_ID) is refused as a change
// of "policy applied upgrades" (TestUpgradeHandlerEnvCannotChangeProtectedRows).
// The runner checks the layout key, the app version, and the plan only as
// exact keys, so a longer key that starts with one of those bytes, or a key
// under no known prefix, is a change of "policy other rows". Nothing of h is
// written, and the honest v2 binary then applies on the same state.
func TestUpgradeCannotChangePlanNameOrRunnerKeyRows(t *testing.T) {
	writes := []protectedWrite{
		{name: "plan-name row of the applied upgrade deleted", write: envWrite(app.ModulePolicy, policy.PlanNameKey(rehearsal.UpgradeName), nil),
			want: changedClass("policy plan names")},
		{name: "plan-name row rewritten to another digest", write: envWrite(app.ModulePolicy, policy.PlanNameKey(rehearsal.UpgradeName), bytes.Repeat([]byte{0xEE}, 32)),
			want: changedClass("policy plan names")},
	}
	for _, key := range [][]byte{{0x00, 0x7A}, {0x04, 0x7A}, {0x06, 0x7A}, {0x09}} {
		writes = append(writes, protectedWrite{name: fmt.Sprintf("policy row %x", key), write: envWrite(app.ModulePolicy, key, []byte{0x01}),
			want: changedClass("policy other rows")})
	}
	refuseWrongUpgrades(t, inHandler, writes)
}

// TestMigrationCannotChangeSafetyCaps: a module migration that rewrites the
// x/policy safety caps row with one cap lowered by one is refused ("policy
// safety caps"); nothing of h is written.
func TestMigrationCannotChangeSafetyCaps(t *testing.T) {
	refuseWrongMigrations(t, []protectedWrite{{
		name: "safety cap lowered",
		write: func(t *testing.T, chainApp *app.App, ctx sdk.Context) {
			store := ctx.KVStore(chainApp.StoreKeyForTest(app.ModulePolicy))
			caps, err := policy.DecodeSafetyCaps(store.Get(policy.SafetyCapsKey()))
			if err != nil {
				t.Error(err)
				return
			}
			caps.SafetyMaxGasPerTx--
			store.Set(policy.SafetyCapsKey(), policy.EncodeSafetyCaps(caps))
		},
		want: changedClass("policy safety caps"),
	}})
}

// TestMigrationCannotApproveDecision: a module migration that rewrites the
// authority config with one blocked decision row approved (approved true,
// value version 1) is refused ("authority config"); no decision row is
// ever approved by an upgrade.
func TestMigrationCannotApproveDecision(t *testing.T) {
	refuseWrongMigrations(t, []protectedWrite{{
		name: "decision row approved",
		write: func(t *testing.T, chainApp *app.App, ctx sdk.Context) {
			store := ctx.KVStore(chainApp.StoreKeyForTest(authority.ModuleName))
			var config authorityv1.GenesisState
			if err := proto.Unmarshal(store.Get([]byte{0x01}), &config); err != nil || len(config.GetBlockedDecisions()) == 0 {
				t.Errorf("authority config: %v (%d decision rows)", err, len(config.GetBlockedDecisions()))
				return
			}
			config.BlockedDecisions[0].Approved = true
			config.BlockedDecisions[0].ValueVersion = 1
			raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(&config)
			if err != nil {
				t.Error(err)
				return
			}
			store.Set([]byte{0x01}, raw)
		},
		want: changedClass("authority config"),
	}})
}

// TestMigrationCannotChangeBlockParams: a module migration that raises the
// stored block max gas by one is refused (the runner compares every
// consensus param); nothing of h is written.
func TestMigrationCannotChangeBlockParams(t *testing.T) {
	refuseWrongMigrations(t, []protectedWrite{{
		name: "block max gas raised",
		write: func(t *testing.T, chainApp *app.App, ctx sdk.Context) {
			store := ctx.KVStore(chainApp.StoreKeyForTest(app.ModuleSVN))
			var params cmtproto.ConsensusParams
			if err := params.Unmarshal(store.Get([]byte{0x03})); err != nil || params.Block == nil {
				t.Errorf("consensus params: %v", err)
				return
			}
			params.Block.MaxGas++
			raw, err := params.Marshal()
			if err != nil {
				t.Error(err)
				return
			}
			store.Set([]byte{0x03}, raw)
		},
		want: fmt.Sprintf("upgrade %s handler changed the consensus params", rehearsal.UpgradeName),
	}})
}

// TestWrongMigrationDiverges: of two nodes at h-1, one opens v2 and one
// v2-bad (rehearsal.BadHandler, the handler of B2-bad: rehearsal.Handler,
// then one extra row, BadRowKey -> BadRowValue, written into the x/leases
// store through the handler environment). The result is a divergence, not
// a halt: both nodes apply the upgrade at h (app version 2), because the
// row is outside every check of the upgrade runner (x/leases is not an I13
// class), so the wrong node commits another app hash. The two states differ
// exactly in that row, and the lease invariants refuse it on the wrong node
// only. Under CometBFT the wrong node then halts at h+1 ("+2/3 committed an
// invalid block", case I6); this test does not run CometBFT.
func TestWrongMigrationDiverges(t *testing.T) {
	r := newPlanNet(t, 2)
	id := r.e.doc.ChainID
	r.blocksTo(t, r.h-1)
	gateRow(t, r.disks[0], id, binaryV2, 3)
	gateRow(t, r.disks[1], id, binaryV2Bad, 3)
	var hashes [][]byte
	for i, node := range r.c.nodes {
		node.height++
		if _, err := node.app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: node.height, Time: node.when, Hash: bytes32(byte(node.height))}); err != nil {
			t.Fatalf("node %d at h: %v", i, err)
		}
		if _, err := node.app.Commit(); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, node.app.LastCommitID().Hash)
	}
	if bytes.Equal(hashes[0], hashes[1]) {
		t.Fatal("v2 and v2-bad committed the same app hash")
	}
	for i, node := range r.c.nodes {
		if got := queryPolicy(t, node.app).GetAppVersion(); got != rehearsal.ToAppVersion {
			t.Fatalf("node %d: app version %d after h", i, got)
		}
	}
	changed := changedKeys(allRows(r.c.nodes[0].app), allRows(r.c.nodes[1].app))
	if want := map[string][]string{app.ModuleLeases: sortedHex(rehearsal.BadRowKey)}; fmt.Sprint(changed) != fmt.Sprint(want) {
		t.Fatalf("the states differ in %v, want %v", changed, want)
	}
	checkInvariantsAt(t, "v2", r.c.nodes[0].app, 3)
	ctx, _ := r.c.nodes[1].app.CommittedContextForTest().CacheContext()
	msgkit.WriteLayoutVersion(ctx.KVStore(r.c.nodes[1].app.StoreKeyForTest(app.ModuleLeases)), leases.ConsensusVersion)
	rd := readersOf(r.c.nodes[1].app)
	if err := leasestest.CheckInvariants(ctx, rd.leases, rd.providers, nil); err == nil {
		t.Fatal("the lease invariants accept the wrong migration's row")
	} else {
		t.Logf("v2-bad: app hash %X (v2 %X); lease invariants: %v", hashes[1], hashes[0], err)
	}
}

// TestUpgradeWindowCheckTxAndQueries records the upgrade window (decision
// request DR-10-41, interim: documented, not refused). v2 started at h-1
// (gate row 3) answers queries with its own code on app-version-1 state
// until h commits: Query/Policy reports app version 1 and the due plan, and
// ABCI Info reports app version 1. CheckTx of a correctly signed council
// approve fails in the window with sdk code 4 (signature verification),
// because after any load the SDK check state has height 0 until the first
// Commit and the SDK then verifies signatures with account number 0. The
// same happens after a plain v1 restart at a normal height (control), and
// both CheckTx calls pass once a block commits. After h commits,
// Query/Policy reports app version 2 and no plan.
func TestUpgradeWindowCheckTxAndQueries(t *testing.T) {
	r := newPlanNet(t, 1)
	e, d, id := r.e, r.disks[0], r.e.doc.ChainID
	for i := 0; i < 2; i++ {
		wantCode(t, "open proposal", e.send(t, r.c, e.seats[0], e.proposal(t, fmt.Sprint("window-", i), msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 30+uint64(i)))), msgkit.OK)
	}
	checkTx := func(label string, raw []byte, wantCode uint32) {
		t.Helper()
		res, err := d.run.app.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil || res.GetCode() != wantCode {
			t.Fatalf("%s: CheckTx err %v code %s/%d log %s, want code %d", label, err, res.GetCodespace(), res.GetCode(), res.GetLog(), wantCode)
		}
		t.Logf("%s: CheckTx code %s/%d %s", label, res.GetCodespace(), res.GetCode(), res.GetLog())
	}
	const sigFailed = 4
	// The probes need a signer whose account number is not 0: before the
	// first commit after a load the check state verifies signatures with
	// account number 0, so a signer numbered 0 would pass. Account numbers
	// follow address order and the test keys are new on every run, so the
	// probes use the first approving seat whose number is not 0; at most
	// one account is numbered 0.
	var approver *actor
	for _, seat := range e.seats[1:] {
		if e.numbers[seat.hexAddr()] != 0 {
			approver = seat
			break
		}
	}
	if approver == nil {
		t.Fatal("every approving seat has account number 0")
	}
	t.Logf("probe signer %s, account number %d", approver.name, e.numbers[approver.hexAddr()])
	gateRow(t, d, id, binaryV1, 1)
	control := r.approveRaw(t, approver, "window-ap-0", "window-0")
	checkTx("v1 after a restart, before a commit", control, sigFailed)
	r.c.block(t)
	checkTx("v1 after a commit", control, 0)

	r.blocksTo(t, r.h-1)
	gateRow(t, d, id, binaryV2, 3)
	inWindow := r.approveRaw(t, approver, "window-ap-1", "window-1")
	checkTx("v2 in the upgrade window", inWindow, sigFailed)
	state := queryPolicy(t, d.run.app)
	if state.GetAppVersion() != 1 || state.GetUpgradePlan().GetName() != rehearsal.UpgradeName || abciAppVersion(t, d.run.app) != 1 {
		t.Fatalf("in the window: app version %d plan %v ABCI %d", state.GetAppVersion(), state.GetUpgradePlan(), abciAppVersion(t, d.run.app))
	}
	r.c.block(t)
	checkTx("v2 after h commits", inWindow, 0)
	if state := queryPolicy(t, d.run.app); state.GetAppVersion() != 2 || state.GetUpgradePlan() != nil {
		t.Fatalf("after h: app version %d plan %v", state.GetAppVersion(), state.GetUpgradePlan())
	}
}

// TestCancelBeforeHeightRemovesPlan: the council cancels the plan before
// its height through real transactions (cancel_upgrade, the only cancel
// path under owner ruling Q2). Query/Policy then has no plan; v1 runs
// through the old plan height with app version 1, and v2 is refused at
// the old h-1 (row 6: one app version behind and no plan).
func TestCancelBeforeHeightRemovesPlan(t *testing.T) {
	r := newPlanNet(t, 1)
	d, id := r.disks[0], r.e.doc.ChainID
	r.council(t, scenario.Council{Name: "cancel", ProposalID: "cancel-rehearsal",
		Change: cancelChange{rehearsal.UpgradeName}, Approvals: 3, Execute: true})
	if plan := queryPolicy(t, r.c.app()).GetUpgradePlan(); plan != nil {
		t.Fatalf("plan %v after the cancel", plan)
	}
	r.blocksTo(t, r.h-1)
	d.refusedRestart(t, id, binaryV2, fmt.Sprintf("load gate at height %d refused this binary (row 6): stored app version 1 is one behind this binary (2) and no upgrade plan is stored", r.h-1))
	gateRow(t, d, id, binaryV1, 1)
	r.blocksTo(t, r.h+1)
	if got := queryPolicy(t, r.c.app()).GetAppVersion(); got != 1 {
		t.Fatalf("app version %d after the cancelled plan height", got)
	}
}

// cancelChange is the cancel_upgrade change of the cancel test. The
// scenario has no cancel step; the test adds it.
type cancelChange struct{ name string }

func (cancelChange) ParameterKey() string { return "upgrade" }

// TestRehearsalBinaryVersionAndLinks builds the rehearsal binaries from the
// one svnd main package with the module cache only: B2 (-tags
// svn_rehearsal) and B2-bad (-tags svn_rehearsal_bad). The `version` of
// each reports app version 2, the upgrade rehearsal-1 from 1 to 2 (the same
// plan name for both), and the same module versions as svnd (neither runs
// a module step). Their symbols hold no test hook of this repository
// (ForTest) and no test-support package (leasestest, evidencetest,
// policytest, msgkittest); TestLinkedBinaryOmitsEconomicModules scans them
// for economic modules. Controls: each links the upgrade runner's PreBlock
// and its own handler, B2 links rehearsal.Handler and not
// rehearsal.BadHandler, and B2-bad links rehearsal.BadHandler. Not
// releases: built from one tree, not signed, not reviewed source.
func TestRehearsalBinaryVersionAndLinks(t *testing.T) {
	declared, err := app.DeclaredBinary(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app_version 2", "upgrade rehearsal-1 from_app_version 1 to_app_version 2"}
	for name, v := range declared.Modules {
		want = append(want, fmt.Sprintf("module %s %d", name, v))
	}
	const repo = "github.com/Maxencejules/shortvideo-network/"
	for _, b := range []struct {
		tag, links, omits string
	}{
		{"svn_rehearsal", repo + "chain/internal/rehearsal.Handler", repo + "chain/internal/rehearsal.BadHandler"},
		{"svn_rehearsal_bad", repo + "chain/internal/rehearsal.BadHandler", ""},
	} {
		res := built(t, b.tag)
		t.Logf("svnd -tags %s: sha256 %s\n%s", b.tag, res.digest, res.version)
		lines := strings.Split(res.version, "\n")
		for _, line := range want {
			found := false
			for _, got := range lines {
				found = found || got == line
			}
			if !found {
				t.Errorf("%s: version output has no line %q:\n%s", b.tag, line, res.version)
			}
		}
		if strings.Contains(res.version, "upgrades none") {
			t.Errorf("%s: version output says upgrades none:\n%s", b.tag, res.version)
		}
		testSupport := []string{repo + "chain/x/leases/leasestest", repo + "chain/x/evidence/evidencetest", repo + "chain/x/policy/policytest", repo + "chain/msgkit/msgkittest"}
		for _, line := range strings.Split(res.symbols, "\n") {
			if !strings.Contains(line, repo) {
				continue
			}
			if strings.Contains(line, "ForTest") {
				t.Errorf("%s links a test hook: %s", b.tag, line)
			}
			for _, pkg := range testSupport {
				if strings.Contains(line, pkg) {
					t.Errorf("%s links test support code: %s", b.tag, line)
				}
			}
		}
		for _, control := range []string{b.links, repo + "chain/upgrade.(*Runner).PreBlock"} {
			if !strings.Contains(res.symbols, control) {
				t.Errorf("%s: control symbol %s is not in the listing", b.tag, control)
			}
		}
		if b.omits != "" && strings.Contains(res.symbols, b.omits) {
			t.Errorf("%s links %s", b.tag, b.omits)
		}
	}
}
