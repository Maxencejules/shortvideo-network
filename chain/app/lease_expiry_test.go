package app_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/log/v2"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// expiryBudget is the protocol ceiling of expiry items per block
// (bounds.json safety_max_expiry_steps_per_block, a PROPOSED safety cap,
// DR-08-07). No council has set a lower max_expiry_steps in these tests
// unless they say so.
const expiryBudget = 128

// batchSize is the number of messages per bulk setup transaction.
const batchSize = 32

// sendBatches sends msgs from who in multi-message transactions of at
// most batchSize messages, each in its own block, all with code 0.
func (n *regNet) sendBatches(t *testing.T, c *cluster, who *actor, label string, msgs []sdk.Msg) {
	t.Helper()
	for start := 0; start < len(msgs); start += batchSize {
		end := min(start+batchSize, len(msgs))
		n.sendMulti(t, c, who, fmt.Sprintf("%s %d-%d", label, start, end-1), msgs[start:end])
	}
}

// totalReserved sums the committed reserved bytes of providers.
func totalReserved(t *testing.T, chainApp *app.App, providers []string) uint64 {
	t.Helper()
	var sum uint64
	for _, id := range providers {
		sum += reserved(t, chainApp, id, 0)
	}
	return sum
}

// TestLargeExpiringCatalogue: 2,048 leases (4 providers x 512 contents)
// and 256 open offers are all due at the same header time T. BeginBlock
// expiry closes exactly min(128, remaining) items per block, in (time,
// kind, id) order (leases before offers at equal times, then id byte
// order), counted from the FinalizeBlock events with mode=BeginBlock; the
// backlog drains over the following blocks. The block at T-1 closes
// nothing (effective-1) and the block at T closes 128 (effective). While
// the backlog lasts, a renewal of an unswept past-deadline lease is
// EXPIRED (D-2), an acceptance of an unswept expired offer is EXPIRED, and
// a provider end of an unswept lease releases its bytes once (the sweep
// then skips it). Reserved bytes drop by exactly the released bytes, and
// the lease invariants hold after every block. Chain time is the block
// header time only.
func TestLargeExpiringCatalogue(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	const (
		leaseContents = 512
		offerContents = 64
		size          = uint64(1000)
	)
	providerIDs := []string{"p1", "p2", "p3", "p4"}
	for i, id := range providerIDs {
		who := n.provider1
		if i%2 == 1 {
			who = n.provider2
		}
		n.registerAdmit(t, c, who, id, capacity1)
	}
	var contents []sdk.Msg
	leaseContent := func(i int) string { return fmt.Sprintf("k%03d", i) }
	offerContent := func(i int) string { return fmt.Sprintf("x%02d", i) }
	for i := 0; i < leaseContents; i++ {
		id := leaseContent(i)
		contents = append(contents, n.registerContent(t, n.service, "reg-"+id, id, 1, packageManifest(id), size))
	}
	for i := 0; i < offerContents; i++ {
		id := offerContent(i)
		contents = append(contents, n.registerContent(t, n.service, "reg-"+id, id, 1, packageManifest(id), size))
	}
	n.sendBatches(t, c, n.service, "register contents", contents)

	due := c.now() + 20_000
	var offers, accepts, extra []sdk.Msg
	var wantLeases, wantOffers []string
	extraOffers := map[string]*leasesv1.MsgOfferLease{}
	for pi, provider := range providerIDs {
		for i := 0; i < leaseContents; i++ {
			content := leaseContent(i)
			offerID := fmt.Sprintf("o%d-%s", pi+1, content)
			leaseID := fmt.Sprintf("l%d-%s", pi+1, content)
			offer := n.offerMsg(n.service, "of-"+offerID, offerID, content, provider, size, due, due)
			offers = append(offers, offer)
			accepts = append(accepts, n.acceptMsg(t, n.broadcaster, "ac-"+leaseID, offer, leaseID))
			wantLeases = append(wantLeases, leaseID)
		}
		for i := 0; i < offerContents; i++ {
			content := offerContent(i)
			offerID := fmt.Sprintf("y%d-%s", pi+1, content)
			offer := n.offerMsg(n.service, "of-"+offerID, offerID, content, provider, size, due+1800, due)
			extra = append(extra, offer)
			extraOffers[offerID] = offer
			wantOffers = append(wantOffers, offerID)
		}
	}
	n.sendBatches(t, c, n.service, "offers", offers)
	n.sendBatches(t, c, n.broadcaster, "accepts", accepts)
	n.sendBatches(t, c, n.service, "open offers", extra)
	if got := totalReserved(t, chainApp, providerIDs); got != uint64(len(wantLeases))*size {
		t.Fatalf("reserved %d before expiry, want %d", got, uint64(len(wantLeases))*size)
	}
	c.checkInvariants(t, "catalogue set up")
	t.Logf("catalogue: %d leases and %d open offers due at header time %d", len(wantLeases), len(wantOffers), due)

	// Effective-1: the block at T-1 closes nothing.
	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(due-1))); len(items) != 0 {
		t.Fatalf("header time T-1 closed %d items", len(items))
	}

	// The first backlog block, at T, also carries three transactions that
	// run after BeginBlock: a renewal of an unswept due lease, an
	// acceptance of an unswept expired offer, and a provider end of an
	// unswept lease.
	unsweptRenew, unsweptEnd := "l4-k511", "l4-k510"
	renew := n.renewMsg(t, n.broadcaster, "rn-"+unsweptRenew, unsweptRenew, "p4", "k511", size, 2, due+leaseTerm)
	accept := n.acceptMsg(t, n.broadcaster, "ac-late", extraOffers["y1-x00"], "l-late")
	end := n.endMsg(t, n.broadcaster, "en-"+unsweptEnd, unsweptEnd, "p4", 1)
	var txs [][]byte
	for _, msg := range []sdk.Msg{renew, accept, end} {
		txs = append(txs, n.signed(t, c, n.broadcaster, registryGas, nil, msg))
		n.seq[n.broadcaster.hexAddr()]++
	}
	var want []expiryItem
	for _, id := range wantLeases {
		if id != unsweptEnd {
			want = append(want, expiryItem{kind: "lease", id: id})
		}
	}
	for _, id := range wantOffers {
		want = append(want, expiryItem{kind: "offer", id: id})
	}
	// Lease ids and offer ids were built in byte order.
	for i := 1; i < len(want); i++ {
		if want[i-1].kind == want[i].kind && want[i-1].id >= want[i].id {
			t.Fatalf("expected order is not id byte order at %d", i)
		}
	}
	reservedBefore := totalReserved(t, chainApp, providerIDs)
	res := c.blockAt(t, unixTime(due), txs...)
	wantCode(t, "renew an unswept past-deadline lease (D-2)", res.TxResults[0], msgkit.Expired)
	wantCode(t, "accept an unswept expired offer", res.TxResults[1], msgkit.Expired)
	wantCode(t, "provider end of an unswept lease", res.TxResults[2], msgkit.OK)
	wantTxEvents(t, "provider end of an unswept lease", res.TxResults[2], evEnded)
	pos := 0
	var counts []int
	for block := 0; ; block++ {
		items, released := beginBlockExpiry(t, res)
		remaining := len(want) - pos
		wantCount := min(expiryBudget, remaining)
		if len(items) != wantCount {
			t.Fatalf("block %d (height %d): closed %d items, want min(%d, %d) = %d", block, c.height(), len(items), expiryBudget, remaining, wantCount)
		}
		var leaseCount uint64
		for i, item := range items {
			if item != want[pos+i] {
				t.Fatalf("block %d item %d: %v, want %v", block, i, item, want[pos+i])
			}
			if item.kind == "lease" {
				leaseCount++
			}
		}
		if released != leaseCount*size {
			t.Fatalf("block %d released %d bytes for %d leases", block, released, leaseCount)
		}
		extraRelease := uint64(0)
		if block == 0 {
			extraRelease = size // the provider end in the same block
		}
		after := totalReserved(t, chainApp, providerIDs)
		if reservedBefore-after != released+extraRelease {
			t.Fatalf("block %d: reserved dropped %d, released %d+%d", block, reservedBefore-after, released, extraRelease)
		}
		reservedBefore = after
		c.checkInvariants(t, fmt.Sprintf("backlog block %d", block))
		pos += len(items)
		counts = append(counts, len(items))
		if pos == len(want) {
			break
		}
		res = c.blockAt(t, unixTime(c.now()+1))
	}
	if blocks := (len(want) + expiryBudget - 1) / expiryBudget; len(counts) != blocks {
		t.Fatalf("drained in %d blocks, want ceil(%d/%d) = %d", len(counts), len(want), expiryBudget, blocks)
	}
	t.Logf("drained %d items in %d blocks: %v", len(want), len(counts), counts)
	if lease := heldLease(t, chainApp, "p4", unsweptRenew, 0); lease != nil {
		t.Fatalf("%s is still held after the drain", unsweptRenew)
	}
	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(c.now()+1))); len(items) != 0 {
		t.Fatalf("after the drain: %d items", len(items))
	}
	if got := totalReserved(t, chainApp, providerIDs); got != 0 {
		t.Fatalf("reserved %d after the drain", got)
	}
	if _, held, open := deadlinePages(t, chainApp, maxUnix, 100, 0); len(held) != 0 || len(open) != 0 {
		t.Fatalf("Deadlines after the drain: %d leases, %d offers", len(held), len(open))
	}
}

// leaseExpirySetup commits p1, p2, contents c1..c4, six leases (c1..c3 on
// both providers) due at due, and two offers (c4 on both) expiring at due.
func leaseExpirySetup(t *testing.T, n *regNet, c *cluster, due int64) {
	t.Helper()
	leaseBasics(t, n, c)
	wantCode(t, "register c4", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c4", "c4", 1, packageManifest("c4"), sizeSmall)), msgkit.OK)
	for _, provider := range []string{"p1", "p2"} {
		for _, content := range []string{"c1", "c2", "c3"} {
			offer := n.offerMsg(n.service, "of-"+provider+content, "o-"+provider+content, content, provider, sizeSmall, due, due)
			wantCode(t, "offer", n.send(t, c, n.service, offer), msgkit.OK)
			wantCode(t, "accept", n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-"+provider+content, offer, "l-"+provider+content)), msgkit.OK)
		}
		wantCode(t, "open offer", n.send(t, c, n.service, n.offerMsg(n.service, "of-"+provider+"c4", "o-"+provider+"c4", "c4", provider, sizeSmall, due+1800, due)), msgkit.OK)
	}
}

// TestExpiryInvariantViolationHalts: when a due lease cannot be released
// (its provider's reserved bytes are below the lease's bytes, written by a
// test-only writer outside any transaction), BeginBlock expiry returns an
// error and FinalizeBlock fails: the block is not committed
// (LastCommitID unchanged), retrying fails the same way, and a second node
// with the same state fails with the same error. A node in this state
// halts instead of releasing bytes it does not hold (fail closed,
// DR-08-07).
func TestExpiryInvariantViolationHalts(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 2)
	due := c.now() + 3000
	leaseExpirySetup(t, n, c, due)
	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(due-1))); len(items) != 0 {
		t.Fatalf("header time T-1 closed %v", items)
	}
	for _, node := range c.nodes {
		if err := node.app.WriteProviderReservedForTest("p1", 0); err != nil {
			t.Fatal(err)
		}
	}
	var errs []string
	for i, node := range c.nodes {
		committed := node.app.LastCommitID()
		for attempt := 0; attempt < 2; attempt++ {
			_, err := node.app.FinalizeBlock(&abci.RequestFinalizeBlock{
				Height: node.height + 1,
				Time:   time.Unix(due, 0).UTC(),
				Hash:   bytes32(byte(node.height + 1)),
			})
			if err == nil {
				t.Fatalf("node %d attempt %d: a block released bytes the provider does not hold", i, attempt)
			}
			if got := node.app.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
				t.Fatalf("node %d: the failed block moved the commit to %d %X", i, got.Version, got.Hash)
			}
			if !strings.Contains(err.Error(), "leases") {
				t.Fatalf("node %d: error does not name x/leases: %v", i, err)
			}
			errs = append(errs, err.Error())
		}
	}
	for _, e := range errs[1:] {
		if e != errs[0] {
			t.Fatalf("nodes failed differently: %q vs %q", errs[0], e)
		}
	}
	t.Logf("FinalizeBlock error on both nodes: %s", errs[0])
}

// finalizeRecovering runs FinalizeBlock at node's next height and returns
// what it panicked with, or nil and its result when it returned.
func finalizeRecovering(node *running, when time.Time, txs ...[]byte) (panicked any, res *abci.ResponseFinalizeBlock, err error) {
	defer func() { panicked = recover() }()
	res, err = node.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: node.height + 1,
		Time:   when,
		Hash:   bytes32(byte(node.height + 1)),
		Txs:    txs,
	})
	return nil, res, err
}

// TestDamagedExpiryLimitHalts: x/leases BeginBlock reads the governed
// max_expiry_steps (authority key 0x13 || "max_expiry_steps") in every
// block, through x/authority GovernedLimit, which panics on a stored value
// that is not 8 bytes in 1..ceiling. A council GovernedLimitChange sets
// the value to 2 and a block commits with it. A test-only writer then
// replaces the stored value in the uncommitted working state, outside any
// transaction, with a value x/authority never writes: 3 bytes, the 8-byte
// value 0, or the 8-byte value ceiling+1. On both nodes the next
// FinalizeBlock, which carries a valid OfferLease and has nothing due,
// panics with GovernedLimit's message instead of returning, and a second
// attempt at the same height panics the same way. The test also checks
// that LastCommitID, the full non-account state and the offer sender's
// account sequence are unchanged, which follows from the panic:
// FinalizeBlock writes the store only after it returns (SDK workingHash),
// and nothing calls Commit. The test calls FinalizeBlock directly; it does
// not run CometBFT, and it does not restart a node on a committed damaged
// value.
func TestDamagedExpiryLimitHalts(t *testing.T) {
	ceiling, ok := msgkit.Ceiling(msgkit.LimitMaxExpirySteps)
	if !ok {
		t.Fatal("max_expiry_steps has no ceiling")
	}
	for _, damage := range []struct {
		name string
		raw  []byte
	}{
		{"3 bytes", []byte{1, 2, 3}},
		{"value 0", binary.BigEndian.AppendUint64(nil, 0)},
		{"ceiling+1", binary.BigEndian.AppendUint64(nil, ceiling+1)},
	} {
		t.Run(damage.name, func(t *testing.T) {
			n := newRegNet()
			c := n.start(t, 2)
			leaseBasics(t, n, c)
			n.council(t, c, "prop-expiry-2", msgkit.LimitMaxExpirySteps, governedPayload(msgkit.LimitMaxExpirySteps, 2))
			c.block(t)
			now := c.now()
			raw := n.signed(t, c, n.service, registryGas, nil, n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeSmall, now+1800, now+600))
			if _, _, err := c.app().Simulate(raw); err != nil {
				t.Fatalf("the block's offer does not succeed on the undamaged state: %v", err)
			}
			for _, node := range c.nodes {
				if err := node.app.WriteGovernedLimitForTest(msgkit.LimitMaxExpirySteps, damage.raw); err != nil {
					t.Fatal(err)
				}
			}
			want := fmt.Sprintf("authority: governed limit %q is not an 8-byte value in 1..ceiling (%d bytes stored)",
				msgkit.LimitMaxExpirySteps, len(damage.raw))
			for i, node := range c.nodes {
				committed := node.app.LastCommitID()
				before := node.app.FullSnapshot()
				seq, known := node.app.AccountSequenceForTest(n.service.addr)
				if !known {
					t.Fatalf("node %d: no account for the service", i)
				}
				for attempt := 0; attempt < 2; attempt++ {
					panicked, res, err := finalizeRecovering(node, node.when, raw)
					if panicked == nil {
						t.Fatalf("node %d attempt %d: FinalizeBlock returned (err %v, result %v) on a damaged max_expiry_steps", i, attempt, err, res)
					}
					if got := fmt.Sprint(panicked); got != want {
						t.Fatalf("node %d attempt %d: panic %q, want %q", i, attempt, got, want)
					}
					if got := node.app.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
						t.Fatalf("node %d attempt %d: the commit moved to %d %X", i, attempt, got.Version, got.Hash)
					}
					if !bytes.Equal(before, node.app.FullSnapshot()) {
						t.Fatalf("node %d attempt %d: the state changed", i, attempt)
					}
					if after, _ := node.app.AccountSequenceForTest(n.service.addr); after != seq {
						t.Fatalf("node %d attempt %d: the sender's sequence moved from %d to %d", i, attempt, seq, after)
					}
				}
			}
			t.Logf("FinalizeBlock panic on both nodes, twice each: %s", want)
		})
	}
}

// openDiskApp opens (or reopens) an application on a goleveldb database
// in dir, the way svnd start does.
func openDiskApp(t *testing.T, chainID, dir string) (*app.App, dbm.DB) {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, dir)
	if err != nil {
		t.Fatal(err)
	}
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: chainID})
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return chainApp, db
}

// TestRestartContinuesExpiry: a node on goleveldb is closed and reopened
// in the middle of an expiry backlog (council max_expiry_steps = 2, eight
// items due at T) and continues the drain. A second node on memdb runs
// the same blocks without a restart; cluster.block compares the app hash
// and every result code of both nodes at every height, before and after
// the restart, and their final full state is equal.
func TestRestartContinuesExpiry(t *testing.T) {
	n := newRegNet()
	dir := t.TempDir()
	disk, db := openDiskApp(t, n.doc.ChainID, dir)
	modules, err := app.AppStateFromDocument(disk.EncodingConfig().Codec, n.doc)
	if err != nil {
		t.Fatal(err)
	}
	params := consensusParams()
	if _, err := disk.InitChain(&abci.RequestInitChain{
		ChainId: n.doc.ChainID, Time: n.doc.Time(), AppStateBytes: mustJSON(t, modules),
		InitialHeight: 1, Validators: validatorUpdates(t, n.doc), ConsensusParams: &params,
	}); err != nil {
		t.Fatal(err)
	}
	c := &cluster{nodes: []*running{{app: disk, params: params, when: n.doc.Time()}, bootWith(t, n.doc, nil)}}
	c.block(t)
	due := c.now() + 3000
	leaseExpirySetup(t, n, c, due)
	n.council(t, c, "prop-expiry-2", msgkit.LimitMaxExpirySteps, governedPayload(msgkit.LimitMaxExpirySteps, 2))
	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(due-1))); len(items) != 0 {
		t.Fatalf("header time T-1 closed %v", items)
	}
	want := []string{
		"[lease:l-p1c1 lease:l-p1c2]", "[lease:l-p1c3 lease:l-p2c1]",
		"[lease:l-p2c2 lease:l-p2c3]", "[offer:o-p1c4 offer:o-p2c4]",
	}
	for i, w := range want {
		items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(due+int64(i))))
		if fmt.Sprint(items) != w {
			t.Fatalf("drain block %d: %v, want %s", i, items, w)
		}
		if i == 0 {
			committed := disk.LastCommitID()
			snapshot := disk.FullSnapshot()
			if err := disk.Close(); err != nil {
				t.Fatal(err)
			}
			_ = db.Close()
			disk, db = openDiskApp(t, n.doc.ChainID, dir)
			if got := disk.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
				t.Fatalf("reopened at %d %X, committed %d %X", got.Version, got.Hash, committed.Version, committed.Hash)
			}
			if !bytes.Equal(snapshot, disk.FullSnapshot()) {
				t.Fatal("reopened state differs")
			}
			c.nodes[0].app = disk
			t.Logf("restarted the goleveldb node at height %d with %d items left", committed.Version, 6)
		}
		c.checkInvariants(t, fmt.Sprintf("drain block %d", i))
	}
	defer db.Close()
	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(due+int64(len(want))))); len(items) != 0 {
		t.Fatalf("after the drain: %v", items)
	}
	if !bytes.Equal(c.nodes[0].app.FullSnapshot(), c.nodes[1].app.FullSnapshot()) {
		t.Fatal("the restarted node's full state differs")
	}
}

// TestWithdrawalAtCapGasInApp: 256 open offers (the PROPOSED per-content
// cap MaxOpenOffersPerContent, DR-08-02) on one content, across two
// providers, from the two admitted services: the first one's 129th offer
// is BOUNDS (D-10) while the content has free slots, and the 257th, sent by
// a third admitted service that holds no offer of the content (so D-10
// cannot be what rejects it), is BOUNDS (D-6), both with the state
// unchanged. A
// WithdrawContent message and a council force-withdraw (ExecuteChange)
// each void all 256 offers of their content in one committed transaction,
// emitting 256 leases_offer_closed WITHDRAWN events, with committed gas
// below withdrawalGasCeiling, half the per-transaction gas cap. Each
// withdrawal transaction is first simulated: simulation changes no
// committed state and reports exactly the gas the committed transaction of
// the same bytes uses, so a client can size its gas limit with it. The
// voided offers' counters are gone (invariants).
func TestWithdrawalAtCapGasInApp(t *testing.T) {
	const withdrawalGasCeiling = 10_000_000
	n := newRegNet()
	service3 := newSecp("service-3")
	n.doc.Submitters = append(n.doc.Submitters, genesis.Submitter{AddressHex: service3.hexAddr(), Role: genesis.RoleAdmittedService})
	n.numbers = accountNumbers(n.doc)
	c := n.start(t, 1)
	leaseBasics(t, n, c)
	chainApp := c.app()
	now := c.now()
	fill := func(content string) {
		t.Helper()
		for half, who := range []*actor{n.service, n.service2} {
			var msgs []sdk.Msg
			for i := 0; i < leases.MaxOpenOffersPerContentPerScheduler; i++ {
				provider := []string{"p1", "p2"}[i%2]
				id := fmt.Sprintf("w-%s-%d-%03d", content, half, i)
				msgs = append(msgs, n.offerMsg(who, "of-"+id, id, content, provider, sizeSmall, now+1800, now+600))
			}
			n.sendBatches(t, c, who, fmt.Sprintf("offers on %s by scheduler %d", content, half), msgs)
			if half == 0 {
				// One scheduler stops at its quota while the content has
				// free slots (D-10).
				before := chainApp.FullSnapshot()
				res := n.send(t, c, who, n.offerMsg(who, "of-"+content+"-129", content+"-129", content, "p1", sizeSmall, now+1800, now+600))
				wantCode(t, "offer 129 on "+content+" by one scheduler (D-10)", res, msgkit.Bounds)
				unchanged(t, "offer 129 on "+content, chainApp, before, res)
			}
		}
		// The 257th offer comes from a scheduler below its D-10 quota.
		before := chainApp.FullSnapshot()
		res := n.send(t, c, service3, n.offerMsg(service3, "of-"+content+"-257", content+"-257", content, "p1", sizeSmall, now+1800, now+600))
		wantCode(t, "offer 257 on "+content+" by a third scheduler (D-6)", res, msgkit.Bounds)
		unchanged(t, "offer 257 on "+content, chainApp, before, res)
	}
	// simulateThenDeliver simulates the signed bytes, requires an
	// unchanged committed state, delivers the same bytes, and requires the
	// simulated gas to equal the committed gas.
	simulateThenDeliver := func(label string, who *actor, msg sdk.Msg) *abci.ExecTxResult {
		t.Helper()
		raw := n.signed(t, c, who, genesis.MaxGasPerTx, nil, msg)
		before := chainApp.FullSnapshot()
		info, _, err := chainApp.Simulate(raw)
		if err != nil {
			t.Fatalf("%s: simulate: %v", label, err)
		}
		if !bytes.Equal(before, chainApp.FullSnapshot()) {
			t.Fatalf("%s: simulation changed committed state", label)
		}
		res := n.deliverRaw(t, c, who, raw)
		wantCode(t, label, res, msgkit.OK)
		if info.GasUsed != uint64(res.GasUsed) {
			t.Fatalf("%s: simulated gas %d, committed gas %d", label, info.GasUsed, res.GasUsed)
		}
		t.Logf("%s at %d open offers: simulated gas %d equals committed gas %d", label, leases.MaxOpenOffersPerContent, info.GasUsed, res.GasUsed)
		return res
	}
	closedEvents := func(label string, res *abci.ExecTxResult) {
		t.Helper()
		var closed int
		for _, ev := range txEventTypes(res) {
			if ev == evClosed {
				closed++
			}
		}
		if closed != leases.MaxOpenOffersPerContent {
			t.Fatalf("%s: %d leases_offer_closed events, want %d", label, closed, leases.MaxOpenOffersPerContent)
		}
		if res.GasUsed <= 0 || res.GasUsed >= withdrawalGasCeiling {
			t.Fatalf("%s: committed gas %d, ceiling %d", label, res.GasUsed, withdrawalGasCeiling)
		}
		t.Logf("%s at %d open offers: committed gas %d (gas wanted %d)", label, leases.MaxOpenOffersPerContent, res.GasUsed, res.GasWanted)
	}

	fill("c1")
	res := simulateThenDeliver("WithdrawContent c1", n.service, n.withdrawContent(t, n.service, "wd-c1", "c1", 1, reasonDigest("c1")))
	closedEvents("WithdrawContent", res)

	fill("c2")
	n.councilReady(t, c, "prop-withdraw-c2", "withdraw-c2", forceWithdrawPayload("c2", 1, reasonDigest("c2")))
	res = simulateThenDeliver("council force-withdraw c2", n.authority, &authorityv1.MsgExecuteChange{Signer: n.authority.addr, BusinessId: "ex-prop-withdraw-c2", ProposalId: "prop-withdraw-c2"})
	if authorityResultCode(res) != "" {
		t.Fatalf("force-withdraw kept with %s", authorityResultCode(res))
	}
	n.policy++
	closedEvents("council ForceWithdraw", res)

	if _, _, open := deadlinePages(t, chainApp, maxUnix, 100, 0); len(open) != 0 {
		t.Fatalf("%d offers open after both withdrawals", len(open))
	}
	c.checkInvariants(t, "after the withdrawals")
}
