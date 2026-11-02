package app_test

import (
	"bytes"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

const (
	sizeC3   = uint64(4_000_000)
	sizeC4   = uint64(4_000_000)
	sizeEnc2 = uint64(3_000_000)
	// maxUnix is the protocol's largest time (9999-12-31T23:59:59Z).
	maxUnix = int64(253402300799)
)

// leaseFlowRecord holds the heights and values the lease flow tests read
// back.
type leaseFlowRecord struct {
	// policy is the authority policy version when each lease was accepted.
	policy map[string]uint64
	// queryHeight is a height where l1, l4, and l7 are held on p1.
	queryHeight int64
	// offerOpen and offerClosed bracket the BeginBlock expiry of offer o6
	// (header time expiry-1 and expiry).
	offerOpen, offerClosed int64
	// leaseHeld and leaseExpired bracket the BeginBlock expiry of lease l5
	// (header time deadline-1 and deadline).
	leaseHeld, leaseExpired int64
	// drain is the heights of the final drain with max_expiry_steps = 1.
	drain []int64
}

// runLeaseFlow drives the committed lease flow on c: register and admit p1
// and p2, register c1..c4 and c1-enc2, two replicas of c1 (p1 by a
// broadcaster relay, p2 by the provider itself), a new encoding as its own
// content id, renewal, provider end, authority end, offer and lease expiry
// by header time, withdrawal voiding open offers, council disable of p2
// with renewal still allowed, a repair handoff of c2 to p1, and a council
// max_expiry_steps = 1 drain. Rejections are interleaved; each asserts its
// code and that the full non-account state is unchanged.
func runLeaseFlow(t *testing.T, n *regNet, c *cluster) leaseFlowRecord {
	t.Helper()
	rec := leaseFlowRecord{policy: map[string]uint64{}}
	reject := func(label string, who *actor, msg sdk.Msg, want msgkit.Code) {
		t.Helper()
		before := c.app().FullSnapshot()
		res := n.send(t, c, who, msg)
		wantCode(t, label, res, want)
		unchanged(t, label, c.app(), before, res)
		wantTxEvents(t, label, res)
	}
	ok := func(label string, who *actor, msg sdk.Msg, events ...string) {
		t.Helper()
		res := n.send(t, c, who, msg)
		wantCode(t, label, res, msgkit.OK)
		wantTxEvents(t, label, res, events...)
	}

	n.registerAdmit(t, c, n.provider1, "p1", capacity1)
	n.registerAdmit(t, c, n.provider2, "p2", capacity1)
	for _, row := range []struct {
		id   string
		size uint64
	}{{"c1", sizeC1}, {"c2", sizeC2}, {"c3", sizeC3}, {"c4", sizeC4}, {"c1-enc2", sizeEnc2}} {
		wantCode(t, "register "+row.id, n.send(t, c, n.service, n.registerContent(t, n.service, "reg-"+row.id, row.id, 1, packageManifest(row.id), row.size)), msgkit.OK)
	}

	c.setUnix(c.now() + 100)
	now := c.now()
	deadline := now + 1800
	expiry := now + 600

	// Two replicas of c1: p1 through a broadcaster relay, p2 by its own
	// provider account.
	o1 := n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeC1, deadline, expiry)
	ok("offer o1", n.service, o1, evOffered)
	ok("accept l1 (broadcaster relay)", n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l1", o1, "l1"), evAccepted)
	rec.policy["l1"] = n.policy
	before := c.app().FullSnapshot()
	retry := n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l1", o1, "l1"))
	wantCode(t, "idempotent retry of accept l1", retry, msgkit.OK)
	wantTxEvents(t, "idempotent retry of accept l1", retry, evResult)
	if !bytes.Equal(before, c.app().FullSnapshot()) {
		t.Fatal("the idempotent retry changed the full state")
	}
	o2 := n.offerMsg(n.service, "of-o2", "o2", "c1", "p2", sizeC1, deadline, expiry)
	ok("offer o2", n.service, o2, evOffered)
	ok("accept l2 (provider account)", n.provider2, n.acceptMsg(t, n.provider2, "ac-l2", o2, "l2"), evAccepted)
	rec.policy["l2"] = n.policy
	reject("second replica on p1 (REQ-INV-001)", n.service, n.offerMsg(n.service, "of-o1b", "o1b", "c1", "p1", sizeC1, deadline, expiry), msgkit.DuplicateID)

	// A service key presented as the provider's acceptance (REQ-CHAIN-012).
	o3 := n.offerMsg(n.service, "of-o3", "o3", "c2", "p1", sizeC2, deadline, expiry)
	ok("offer o3", n.service, o3, evOffered)
	forged := n.acceptMsg(t, n.broadcaster, "ac-l3-forged", o3, "l3")
	forged.ProviderAcceptance = acceptanceBy(t, o3, "l3", signingKey{id: n.signing["p1"].id, priv: n.intent.priv})
	reject("service key as provider acceptance", n.broadcaster, forged, msgkit.Unauthorized)
	ok("accept l3", n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l3", o3, "l3"), evAccepted)
	rec.policy["l3"] = n.policy

	// REQ-SEP-001: a new encoding is registered as its own content id and
	// gets its own lease while l1 holds c1.
	o4 := n.offerMsg(n.service, "of-o4", "o4", "c1-enc2", "p1", sizeEnc2, deadline, expiry)
	ok("offer o4 (new encoding)", n.service, o4, evOffered)
	ok("accept l4", n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l4", o4, "l4"), evAccepted)
	rec.policy["l4"] = n.policy

	// Renewal keeps the committed bytes and digest.
	ok("renew l1", n.broadcaster, n.renewMsg(t, n.broadcaster, "rn-l1-2", "l1", "p1", "c1", sizeC1, 2, deadline+leaseTerm), evRenewed)
	reject("stale renewal of l1", n.broadcaster, n.renewMsg(t, n.broadcaster, "rn-l1-2b", "l1", "p1", "c1", sizeC1, 2, deadline+2*leaseTerm), msgkit.StaleVersion)
	reject("renewal with other bytes", n.broadcaster, n.renewMsg(t, n.broadcaster, "rn-l1-3x", "l1", "p1", "c1", sizeC1+1, 3, deadline+2*leaseTerm), msgkit.Conflict)

	// Provider end and authority end release exactly once.
	ok("provider end l2", n.provider2, n.endMsg(t, n.provider2, "en-l2", "l2", "p2"), evEnded)
	reject("second end of l2", n.provider2, n.endMsg(t, n.provider2, "en-l2-again", "l2", "p2"), msgkit.Underflow)
	ok("authority end l3", n.authority, n.authorityEndMsg("en-l3", "l3"), evEnded)

	// Expiry by header time: o6 is never accepted; l5 runs to its deadline.
	deadline5 := now + 900
	o5 := n.offerMsg(n.service, "of-o5", "o5", "c3", "p1", sizeC3, deadline5, expiry)
	ok("offer o5", n.service, o5, evOffered)
	ok("accept l5", n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l5", o5, "l5"), evAccepted)
	rec.policy["l5"] = n.policy
	expiry6 := now + 300
	ok("offer o6", n.service, n.offerMsg(n.service, "of-o6", "o6", "c3", "p2", sizeC3, deadline, expiry6), evOffered)

	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(expiry6-1))); len(items) != 0 {
		t.Fatalf("header time expiry-1 expired %v", items)
	}
	rec.offerOpen = c.height()
	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(expiry6))); fmt.Sprint(items) != "[offer:o6]" {
		t.Fatalf("header time expiry expired %v, want offer o6", items)
	}
	rec.offerClosed = c.height()
	if items, _ := beginBlockExpiry(t, c.blockAt(t, unixTime(deadline5-1))); len(items) != 0 {
		t.Fatalf("header time deadline-1 expired %v", items)
	}
	rec.leaseHeld = c.height()
	items, released := beginBlockExpiry(t, c.blockAt(t, unixTime(deadline5)))
	if fmt.Sprint(items) != "[lease:l5]" || released != sizeC3 {
		t.Fatalf("header time deadline expired %v releasing %d, want lease l5 releasing %d", items, released, sizeC3)
	}
	rec.leaseExpired = c.height()

	// Withdrawal voids every open offer of the content in the withdrawal
	// transaction; the voided offer cannot be accepted and no new offer
	// can name the content.
	now = c.now()
	o7 := n.offerMsg(n.service, "of-o7", "o7", "c4", "p1", sizeC4, now+1800, now+600)
	ok("offer o7", n.service, o7, evOffered)
	ok("offer o8", n.service, n.offerMsg(n.service, "of-o8", "o8", "c4", "p2", sizeC4, now+1800, now+600), evOffered)
	ok("withdraw c4", n.service, n.withdrawContent(t, n.service, "wd-c4", "c4", 1, reasonDigest("c4")), evClosed, evClosed)
	reject("accept a voided offer", n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l8", o7, "l8"), msgkit.NotFound)
	reject("offer withdrawn content", n.service, n.offerMsg(n.service, "of-o12", "o12", "c4", "p1", sizeC4, now+1800, now+600), msgkit.Withdrawn)

	// A p2 replica of c2, then council disable of p2: new obligations are
	// FORBIDDEN, renewal stays allowed (DR-08-01), authority end releases.
	o9 := n.offerMsg(n.service, "of-o9", "o9", "c2", "p2", sizeC2, now+1800, now+600)
	ok("offer o9", n.service, o9, evOffered)
	ok("accept l6", n.provider2, n.acceptMsg(t, n.provider2, "ac-l6", o9, "l6"), evAccepted)
	rec.policy["l6"] = n.policy
	n.council(t, c, "prop-disable-p2", "disable-p2", disablePayload("p2"))
	reject("offer to a disabled provider", n.service, n.offerMsg(n.service, "of-o10", "o10", "c3", "p2", sizeC3, now+1800, now+600), msgkit.Forbidden)
	ok("renew l6 on a disabled provider", n.provider2, n.renewMsg(t, n.provider2, "rn-l6-2", "l6", "p2", "c2", sizeC2, 2, now+1800+leaseTerm), evRenewed)

	// Repair handoff: c2 moves to p1 (its earlier p1 lease l3 ended).
	o11 := n.offerMsg(n.service, "of-o11", "o11", "c2", "p1", sizeC2, now+1800, now+600)
	ok("offer o11 (repair)", n.service, o11, evOffered)
	ok("accept l7 (repair)", n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l7", o11, "l7"), evAccepted)
	rec.policy["l7"] = n.policy
	ok("authority end l6", n.authority, n.authorityEndMsg("en-l6", "l6"), evEnded)
	rec.queryHeight = c.height()
	// o13 stays open past the end of the flow (its expiry is a day away).
	ok("offer o13", n.service, n.offerMsg(n.service, "of-o13", "o13", "c3", "p1", sizeC3, c.now()+1800, c.now()+86000), evOffered)

	// Governed expiry budget: one item per block.
	n.council(t, c, "prop-expiry-1", msgkit.LimitMaxExpirySteps, governedPayload(msgkit.LimitMaxExpirySteps, 1))
	// l4 (deadline), l7 (now+1800), l1 (deadline+3600) are due together.
	c.setUnix(deadline + leaseTerm)
	for _, want := range []string{"[lease:l4]", "[lease:l7]", "[lease:l1]"} {
		got, _ := beginBlockExpiry(t, c.block(t))
		if fmt.Sprint(got) != want {
			t.Fatalf("budget-1 drain: got %v, want %s", got, want)
		}
		rec.drain = append(rec.drain, c.height())
	}
	if got, _ := beginBlockExpiry(t, c.block(t)); len(got) != 0 {
		t.Fatalf("after the drain: %v", got)
	}
	c.checkInvariants(t, "end of the lease flow")
	return rec
}

// leaseQueries runs the flow's query set on chainApp at height and returns
// each query's page bytes by name.
func leaseQueries(t *testing.T, chainApp *app.App, height int64) map[string][][]byte {
	t.Helper()
	out := map[string][][]byte{}
	out["active-c1-limit-1"], _ = activePages(t, chainApp, "c1", "", 1, height)
	out["active-p1-limit-1"], _ = activePages(t, chainApp, "", "p1", 1, height)
	out["active-p1-limit-100"], _ = activePages(t, chainApp, "", "p1", 100, height)
	out["active-pair-p1-c1"], _ = activePages(t, chainApp, "c1", "p1", 100, height)
	out["deadlines-limit-1"], _, _ = deadlinePages(t, chainApp, maxUnix, 1, height)
	out["deadlines-limit-100"], _, _ = deadlinePages(t, chainApp, maxUnix, 100, height)
	return out
}

func sameQueries(t *testing.T, label string, a, b map[string][][]byte) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%s: query sets differ", label)
	}
	for name, pages := range a {
		other := b[name]
		if len(pages) != len(other) {
			t.Fatalf("%s: %s has %d pages, other %d", label, name, len(pages), len(other))
		}
		for i := range pages {
			if !bytes.Equal(pages[i], other[i]) {
				t.Fatalf("%s: %s page %d differs", label, name, i)
			}
		}
	}
}

// TestLeaseFlowReplayAndQueries runs the lease flow on three in-process
// nodes (cluster.block compares the app hash and every result code at each
// height), then replays the recorded blocks (height, header time,
// transactions) into a fresh application from the same genesis: the app
// hash at every height, every result code and gas, and the final full
// state must be equal. ActiveLeases and Deadlines bytes are equal across
// the nodes and the replay, and the decoded leases bind the offer terms,
// the reservation, and the authority policy version at acceptance.
func TestLeaseFlowReplayAndQueries(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 3)
	rec := runLeaseFlow(t, n, c)

	fresh := c.replayFresh(t, n.doc, 0)
	if !bytes.Equal(fresh.app.FullSnapshot(), c.app().FullSnapshot()) {
		t.Fatal("replayed full state differs from the cluster")
	}
	if !bytes.Equal(fresh.app.LastCommitID().Hash, c.app().LastCommitID().Hash) {
		t.Fatal("replayed app hash differs")
	}
	t.Logf("replayed %d blocks; app hash %X equal at every height", len(c.inputs), fresh.app.LastCommitID().Hash)

	h := rec.queryHeight
	want := leaseQueries(t, c.app(), h)
	for i, node := range c.nodes[1:] {
		sameQueries(t, fmt.Sprintf("node %d", i+1), want, leaseQueries(t, node.app, h))
	}
	sameQueries(t, "replay", want, leaseQueries(t, fresh.app, h))
	if len(want["active-p1-limit-1"]) != 3 || len(want["active-p1-limit-100"]) != 1 || len(want["deadlines-limit-1"]) != 3 {
		t.Fatalf("page counts: active p1 limit 1 %d, limit 100 %d, deadlines limit 1 %d",
			len(want["active-p1-limit-1"]), len(want["active-p1-limit-100"]), len(want["deadlines-limit-1"]))
	}

	// Decoded values at the query height.
	_, onP1 := activePages(t, c.app(), "", "p1", 100, h)
	var ids []string
	byID := map[string]*leasesv1.Lease{}
	for _, lease := range onP1 {
		ids = append(ids, lease.GetLeaseId())
		byID[lease.GetLeaseId()] = lease
	}
	if fmt.Sprint(ids) != "[l1 l4 l7]" {
		t.Fatalf("held leases on p1 at %d: %v", h, ids)
	}
	l1 := byID["l1"]
	if l1.GetOfferId() != "o1" || l1.GetContentId() != "c1" || l1.GetContentVersion() != 1 || l1.GetProviderId() != "p1" ||
		!proto.Equal(l1.GetManifestDigest(), contentManifest("c1")) || !proto.Equal(l1.GetTermsDigest(), termsDigest("o1")) ||
		l1.GetPackageBytes() != sizeC1 || l1.GetState() != leasesv1.LeaseState_LEASE_STATE_RENEWED || l1.GetTermVersion() != 2 ||
		!l1.GetReservationHeld() || l1.GetPolicyVersion() != rec.policy["l1"] {
		t.Fatalf("l1 at %d: %v", h, l1)
	}
	if l1.GetDeadlineUnixSeconds()-l1.GetStartUnixSeconds() != 2*leaseTerm {
		t.Fatalf("l1 start %d deadline %d after one renewal", l1.GetStartUnixSeconds(), l1.GetDeadlineUnixSeconds())
	}
	if byID["l4"].GetContentId() != "c1-enc2" || byID["l4"].GetPackageBytes() != sizeEnc2 || byID["l4"].GetState() != leasesv1.LeaseState_LEASE_STATE_ACTIVE {
		t.Fatalf("l4 at %d: %v", h, byID["l4"])
	}
	// p2 was disabled by an executed proposal before l7 was accepted.
	if byID["l7"].GetPolicyVersion() != rec.policy["l7"] || rec.policy["l7"] != rec.policy["l1"]+1 {
		t.Fatalf("l7 policy %d, recorded %v", byID["l7"].GetPolicyVersion(), rec.policy)
	}
	if got := reserved(t, c.app(), "p1", h); got != sizeC1+sizeEnc2+sizeC2 {
		t.Fatalf("p1 reserved at %d: %d", h, got)
	}
	if got := reserved(t, c.app(), "p2", h); got != 0 {
		t.Fatalf("p2 reserved at %d: %d", h, got)
	}
	_, c1Leases := activePages(t, c.app(), "c1", "", 100, h)
	if len(c1Leases) != 1 || c1Leases[0].GetLeaseId() != "l1" {
		t.Fatalf("ActiveLeases(c1) at %d: %v", h, c1Leases)
	}
	_, held, open := deadlinePages(t, c.app(), maxUnix, 100, h)
	var order []string
	for _, lease := range held {
		order = append(order, lease.GetLeaseId())
	}
	if fmt.Sprint(order) != "[l4 l7 l1]" || len(open) != 0 {
		t.Fatalf("Deadlines at %d: leases %v, %d offers", h, order, len(open))
	}

	// Effective-1 and effective of the two expiries, on every node.
	for i, node := range c.nodes {
		if heldLease(t, node.app, "p1", "l5", rec.leaseHeld) == nil || heldLease(t, node.app, "p1", "l5", rec.leaseExpired) != nil {
			t.Fatalf("node %d: l5 not held at %d or still held at %d", i, rec.leaseHeld, rec.leaseExpired)
		}
		if diff := reserved(t, node.app, "p1", rec.leaseHeld) - reserved(t, node.app, "p1", rec.leaseExpired); diff != sizeC3 {
			t.Fatalf("node %d: expiry of l5 released %d", i, diff)
		}
		_, _, openBefore := deadlinePages(t, node.app, maxUnix, 100, rec.offerOpen)
		_, _, openAfter := deadlinePages(t, node.app, maxUnix, 100, rec.offerClosed)
		if len(openBefore) != 1 || openBefore[0].GetOfferId() != "o6" || len(openAfter) != 0 {
			t.Fatalf("node %d: open offers %d at %d, %d at %d", i, len(openBefore), rec.offerOpen, len(openAfter), rec.offerClosed)
		}
	}
	// After the drain nothing is held and nothing is reserved; o13 is open.
	if _, all := activePages(t, c.app(), "", "p1", 100, 0); len(all) != 0 {
		t.Fatalf("held after the drain: %v", all)
	}
	if _, held, open := deadlinePages(t, c.app(), maxUnix, 100, 0); len(held) != 0 || len(open) != 1 || open[0].GetOfferId() != "o13" {
		t.Fatalf("Deadlines after the drain: %d leases, offers %v", len(held), open)
	}
	if got := reserved(t, c.app(), "p1", 0); got != 0 {
		t.Fatalf("p1 reserved after the drain: %d", got)
	}
}
