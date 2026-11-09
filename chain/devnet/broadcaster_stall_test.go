package devnet_test

// TestDevnetBroadcasterQuorumStall (plan 13.4, F1): a separate devnet whose
// quorum is lost while one attempt is in flight. Single machine, four
// validator processes, private network namespace.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
)

// TestDevnetBroadcasterQuorumStall stops two of four validators with the
// existing stall helpers and checks that the broadcaster neither starts a
// new attempt nor reports COMMITTED without a committed transaction while
// the chain cannot commit, that an intent with nothing sent fails at its
// expiry, that the in-flight intent shows PENDING_STALL over ACKNOWLEDGED
// and, once its expiry passes during the stall, waits for the chain
// (AWAITING_DEADLINE, EXPIRED_AWAITING_CHAIN) with no send after it, that
// the in-flight transaction and the queued intent commit once quorum
// returns, and that a lost link to the node is reported as
// OBSERVER_UNAVAILABLE, not as a stall.
func TestDevnetBroadcasterQuorumStall(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip("SVN_SKIP_DEVNET=1")
	}
	fmt.Println("label=single machine, four validator processes, private network namespace (not independent operators, not production)")
	n := newBnet(t)
	d := n.d
	d.commit("setup-register-p2", d.provider2, d.registerProvider(d.provider2, "reg-p2", "p2", capacityOne))
	d.council("setup-admit-p2", "prop-admit-p2", "admit-p2", admitChange("p2"))
	n.providers = []string{"p2"}
	offer := n.relaySetup("setup", "cst", "ost", "p2")
	n.checkAccounting("setup", false)
	// A 30 s RPC timeout keeps the held broadcast's caller waiting across
	// the stall's start; T = 3 s opens the stall episode quickly.
	n.serial++
	n.swapServe(n.writeServeConfig(fmt.Sprintf("serve-stall-%d.json", n.serial), n.listen, n.proxy.url(),
		map[string]any{"rpc_timeout_seconds": 30, "stall_window_seconds": 3}))

	// 1. Quiesce: the proxy holds the broadcaster's broadcast of the one
	// in-flight relay; no other intent is open and every node's mempool is
	// empty. No offer or lease deadline falls inside the window (the offer
	// expires in 3000 s). The relay expires in 35 s: after the held
	// broadcast is released (which happens within the 30 s RPC timeout of
	// its send, or the release would find no caller) and while the chain
	// is still stalled.
	n.proxy.setRule(broadcastMethod, proxyHold, 1)
	relay := n.acceptReq(creatorOne, offer, "lst", n.acceptSig(offer, "lst", d.keys["p2"]))
	relay.ExpiresInSeconds = 35
	inflight := n.create(callerApp, relay)
	bcWait(t, "the held relay broadcast", 60*time.Second, func() bool { return len(n.proxy.heldTx(broadcastMethod)) == 1 }, n.serve)
	bcWait(t, "empty mempools before the stall", time.Minute, func() bool {
		snap := n.allIntents()
		for _, r := range snap {
			if r.ID != inflight.IntentID && !r.terminal() {
				return false
			}
		}
		for _, client := range d.clients {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			res, err := client.NumUnconfirmedTxs(ctx)
			cancel()
			if err != nil || res.Total != 0 {
				return false
			}
		}
		return true
	}, n.serve)
	held := n.proxy.heldTx(broadcastMethod)[0]

	// 2. Stop v4 by hand, then v3 with stopForStall, then observeStall, as
	// the two-down pattern of prove_test.go does: after v4's stop, v1 to v3
	// commit two blocks above the height read after the stop, so the block
	// in flight at v4's stop (which can carry v4's precommit) is committed
	// before stopForStall reads its threshold.
	n.stopValidator(3)
	oneDown, err := devnet.HighWater(n.rpcs[:3])
	if err != nil {
		t.Fatal(tail(n.layout, err))
	}
	if _, _, err := devnet.WaitAbove(n.rpcs[:3], oneDown.Height+1, 30*time.Second); err != nil {
		t.Fatal(tail(n.layout, err))
	}
	start, err := stopForStall("broadcaster-stall", n.layout, n.stops, 2)
	if err != nil {
		t.Fatal(tail(n.layout, err))
	}
	stall := observeStall(t, "broadcaster-stall", start, stoppedSigned(t, consensusKeys(t, n.layout), start))

	// 3. The in-flight attempt: the held broadcast goes to node 0, whose
	// CheckTx runs without quorum; then one queued and one short-expiry
	// intent.
	forwardsBefore := len(n.proxy.forwardedTxs())
	n.proxy.release(broadcastMethod, true)
	n.waitAttempts(inflight.IntentID, "the acknowledged in-flight attempt", 60*time.Second, func(a []bcAttemptRow) bool {
		return len(a) == 1 && a[0].Phase == "ACKNOWLEDGED"
	})
	if time.Now().After(inflight.ExpiresAt) {
		t.Fatalf("stall: the in-flight relay expired at %s, before its acknowledgement", inflight.ExpiresAt.UTC().Format(time.RFC3339Nano))
	}
	acked := n.waitView(inflight.IntentID, "PENDING_STALL over ACKNOWLEDGED", 30*time.Second, func(v bcView) bool {
		return v.Status == stStall && v.Detail.Phase == "ACKNOWLEDGED"
	})
	queued := n.create(callerApp, n.registerReq(creatorOne, "cst-queued", sizeOne))
	short := n.registerReq(creatorOne, "cst-short", sizeOne)
	short.ExpiresInSeconds = 6
	expiring := n.create(callerApp, short)
	fmt.Printf("phase=stall in_flight=%s acknowledged=true held_tx=%X queued=%s short_expiry=%s\n", inflight.IntentID, held, queued.IntentID, expiring.IntentID)

	// 4. Keep the stall for at least the minimum lifetime + T + 2 polls,
	// and past the in-flight relay's expiry by T + 2 polls + 2 s,
	// re-reading the heights at every sample.
	keep := time.Duration(5+3)*time.Second + 2*200*time.Millisecond + 4*time.Second
	deadline := time.Now().Add(keep)
	if past := inflight.ExpiresAt.Add(3*time.Second + 2*200*time.Millisecond + 2*time.Second); past.After(deadline) {
		deadline = past
	}
	samples := 0
	for time.Now().Before(deadline) {
		rows, err := devnet.Snapshot(start.running)
		if err != nil {
			t.Fatal(err)
		}
		for i, row := range rows {
			if row.Height > stall {
				t.Fatalf("stall: node %d reached height %d above the stall height %d", i, row.Height, stall)
			}
		}
		samples++
		time.Sleep(time.Second)
	}

	// 5. During the stall. The in-flight relay expired with its attempt
	// live: it waits for the chain (AWAITING_DEADLINE, shown with
	// EXPIRED_AWAITING_CHAIN under PENDING_STALL), and no broadcast request
	// reached the proxy later than its expiry plus 2 s (a send counted
	// before expires_at reaches the proxy on loopback within that time).
	expired := n.intentRow(inflight.IntentID)
	ev := n.view(inflight.IntentID)
	if expired.State != "IN_FLIGHT" || expired.Phase != "AWAITING_DEADLINE" || ev.Status != stStall ||
		ev.Detail.Phase != "AWAITING_DEADLINE" || ev.Detail.Reason != "EXPIRED_AWAITING_CHAIN" {
		t.Fatalf("stall: the expired in-flight relay is %s and shows %s (reason %q), want IN_FLIGHT/AWAITING_DEADLINE under PENDING_STALL with EXPIRED_AWAITING_CHAIN",
			expired, ev, ev.Detail.Reason)
	}
	for _, e := range n.proxy.eventLog() {
		if e.method == broadcastMethod && e.at.After(inflight.ExpiresAt.Add(2*time.Second)) &&
			(e.outcome == "held" || e.outcome == "dropped" || e.outcome == "reset" || e.outcome == "forwarded") {
			t.Fatalf("stall: a broadcast request (%s, %X) reached the proxy at %s, after the in-flight relay's expiry %s",
				e.outcome, e.hash, e.at.UTC().Format(time.RFC3339Nano), inflight.ExpiresAt.UTC().Format(time.RFC3339Nano))
		}
	}
	if rows := n.attempts(queued.IntentID); len(rows) != 0 {
		t.Fatalf("stall: the queued intent has %d attempts while the chain is stalled", len(rows))
	}
	for _, h := range n.proxy.forwardedTxs()[forwardsBefore:] {
		if !bytes.Equal(h[:], held[:]) {
			t.Fatalf("stall: the proxy forwarded %X while stalled; only the in-flight %X was released", h, held)
		}
	}
	reason, since, open := n.openStallEpisode()
	if !open {
		t.Fatal("stall: no open stall episode")
	}
	for _, id := range []string{inflight.IntentID, queued.IntentID, expiring.IntentID} {
		n.committedIffOnChain("stall", id)
	}
	n.failed("stall", expiring.IntentID, "EXPIRED_NOT_SENT")
	q := n.intentRow(queued.IntentID)
	qv := n.view(queued.IntentID)
	if q.State != "REQUESTED" || q.BusinessID == "" || len(n.objectEffects(bcObject{Event: evRegistered, ID: "cst-queued", Version: 1, HasVersion: true})) != 0 {
		t.Fatalf("stall: the queued intent is %s, want REQUESTED with a business id and no transaction", q)
	}
	if qv.Status != stStall || qv.Detail.Phase != "QUEUED" {
		t.Fatalf("stall: the queued intent shows %s, want PENDING_STALL over QUEUED", qv)
	}
	fmt.Printf("phase=stall samples=%d stall_height=%d episode=%s since_height=%d in_flight_api=%s/%s then_expired=%s/%s(%s) sends_after_expiry=0 queued_attempts=0 forwards_while_stalled=released-in-flight-only committed_shown=none short_expiry=FAILED(EXPIRED_NOT_SENT) queued=%s/%s business_id=%s api=%s\n",
		samples, stall, reason, since, acked.Status, acked.Detail.Phase, ev.Status, ev.Detail.Phase, ev.Detail.Reason, q.State, q.Phase, q.BusinessID, qv.Status)

	// 6. Quorum returns: the in-flight transaction (the mempool has no TTL
	// and the height did not advance) and the queued intent commit; the
	// episode closes.
	n.startValidator(2)
	n.startValidator(3)
	if _, _, err := devnet.WaitAbove(n.rpcs, stall+1, 2*time.Minute); err != nil {
		t.Fatal(tail(n.layout, err))
	}
	v := n.committed("recovery", inflight.IntentID)
	if rows := n.attempts(inflight.IntentID); len(rows) != 1 || !bytes.Equal(rows[0].Hash, held[:]) || !bytes.Equal(n.hashOf(v.Commitment.FirstCommitTxHash), held[:]) {
		t.Fatalf("recovery: the in-flight intent committed %s with attempts %v, want its one held attempt", v.Commitment.FirstCommitTxHash, rows)
	}
	n.committed("recovery", queued.IntentID)
	bcWait(t, "the stall episode closes", time.Minute, func() bool {
		_, _, open := n.openStallEpisode()
		return !open
	}, n.serve)
	fmt.Printf("phase=recovery in_flight_committed=%s height=%d attempts=1 queued_committed=true episode_closed=true\n", v.Commitment.FirstCommitTxHash, v.Commitment.Height)
	n.checkAccounting("recovery", false)

	// 7. The observer: only serve's link to the node fails (the proxy
	// resets status requests) while the chain runs. An open intent shows
	// OBSERVER_UNAVAILABLE, not PENDING_STALL.
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	probe := n.create(callerApp, n.registerReq(creatorOne, "cst-observer", sizeOne))
	bcWait(t, "the held probe broadcast", 60*time.Second, func() bool { return len(n.proxy.heldTx(broadcastMethod)) >= 1 }, n.serve)
	n.proxy.setRule("status", proxyReset, -1)
	pv := n.waitView(probe.IntentID, "OBSERVER_UNAVAILABLE", time.Minute, func(v bcView) bool { return v.Detail.Observer == "OBSERVER_UNAVAILABLE" })
	if pv.Status == stStall {
		t.Fatalf("observer: a lost node link is shown as a stall: %s", pv)
	}
	if d.height() <= stall+1 {
		t.Fatalf("observer: the chain did not advance while the link was reset")
	}
	fmt.Printf("phase=observer link=status reset by a test proxy on loopback api=%s observer=%s chain_height=%d\n", pv.Status, pv.Detail.Observer, d.height())
	n.proxy.clearRules()
	n.proxy.release(broadcastMethod, true)
	n.committed("observer", probe.IntentID)

	n.checkAccounting("Z", true)
	n.printTraces("Z")
	n.checkRaceReports("Z")
	n.stopAll()
	fmt.Printf("test=TestDevnetBroadcasterQuorumStall seconds=%.1f ci_go_test_timeout=12m\n", time.Since(n.start).Seconds())
}

// committedIffOnChain checks the stall rule for one intent: the API shows
// COMMITTED if and only if node 0 holds a committed transaction for it.
func (n *bnet) committedIffOnChain(phase, id string) {
	n.t.Helper()
	v := n.view(id)
	onChain := false
	for _, a := range n.attempts(id) {
		if _, ok := n.txOnChain(a.Hash); ok {
			onChain = true
		}
	}
	if (v.Status == stCommitted) != onChain {
		n.t.Fatalf("phase %s: %s shows %s while node 0 holds its transaction: %t", phase, id, v.Status, onChain)
	}
}
