package devnet_test

// Fault phases of TestDevnetBroadcasterLifecycle: A (acknowledgement versus
// commit), TB1 and TB2 (timeouts before submission), TA (timeout after
// submission), WD (worker death after commit), WP1-WP3 (build-tagged fault
// points), HD (a held request delivered after the worker died) and EX
// (expiry while no send gets through). Proxy faults are "fault injected by
// a test proxy on loopback"; fault points are "build-tagged fault point";
// kills are process crashes, not host power loss.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const broadcastMethod = "broadcast_tx_sync"

// precondition logs a fault's precondition, read with the SELECT-only
// role, and fails the phase when it was not reached.
func (n *bnet) precondition(phase, what string, ok bool, detail string) {
	n.t.Helper()
	if !ok {
		n.t.Fatalf("phase %s: precondition not reached: %s (%s)", phase, what, detail)
	}
	fmt.Printf("phase=%s precondition=%q reached detail=%q\n", phase, what, detail)
}

// waitAttempts waits until an intent's attempts satisfy cond and returns
// them.
func (n *bnet) waitAttempts(id, what string, timeout time.Duration, cond func([]bcAttemptRow) bool) []bcAttemptRow {
	n.t.Helper()
	var rows []bcAttemptRow
	bcWait(n.t, what, timeout, func() bool {
		rows = n.attempts(id)
		return cond(rows)
	}, n.serve)
	return rows
}

// armFault arms a build-tagged fault point: it fires once in the process
// whose SVN_FAULTPOINT_DIR is dir, renames the file to <point>.hit, and
// blocks the goroutine that reached it.
func (n *bnet) armFault(dir, point string) {
	n.t.Helper()
	path := filepath.Join(dir, point+".arm")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		n.t.Fatal(err)
	}
}

// waitHit waits until the fault point fired.
func (n *bnet) waitHit(dir, point string, procs ...*bcProc) {
	n.t.Helper()
	bcWait(n.t, "fault point "+point, 90*time.Second, func() bool {
		_, err := os.Lstat(filepath.Join(dir, point+".hit"))
		return err == nil
	}, procs...)
	if err := os.Remove(filepath.Join(dir, point+".hit")); err != nil {
		n.t.Fatal(err)
	}
}

// swapServe stops serve with SIGTERM and starts it again with cfg.
func (n *bnet) swapServe(cfg string) {
	n.t.Helper()
	if code := n.serve.stop(n.t); code != 0 {
		n.t.Fatalf("serve exit %d on SIGTERM: %s", code, n.serve.logTail(10))
	}
	n.serve = n.startServe("serve", cfg)
}

// slowServe is a serve configuration whose RPC timeout is 30 s, so a held
// answer leaves the attempt without a recorded outcome long enough for the
// test to read that precondition and act on it.
func (n *bnet) slowServe() string {
	n.t.Helper()
	n.serial++
	return n.writeServeConfig(fmt.Sprintf("serve-slow-%d.json", n.serial), n.listen, n.proxy.url(),
		map[string]any{"rpc_timeout_seconds": 30})
}

// phaseAckCommit is phase A: an acceptance passes CheckTx and fails with
// CAPACITY in its block. While the proxy holds the broadcaster's tx and
// tx_search calls (from the moment the broadcast reaches the node), the
// test reads the committed failure from a node and
// the API still shows the acknowledged attempt; after the release the
// intent is FAILED(EXECUTION_FAILED, CAPACITY) and p3's reservation is
// unchanged.
func phaseAckCommit(n *bnet, name string) {
	n.ownContent(name, callerApp, sizeTwo, "ca1")
	n.ownContent(name, callerApp, sizeOne, "ca2")
	deadline, expiry := n.terms()
	big := n.offerOf("oa1", "ca1", "p3", sizeTwo, deadline, expiry)
	small := n.offerOf("oa2", "ca2", "p3", sizeOne, deadline, expiry)
	n.offerCommitted(name, callerApp, big)
	n.offerCommitted(name, callerApp, small)
	n.leaseCommitted(name, big, "la1")
	before := n.d.reservedAt(n.d.clients[0], "p3", n.d.height())

	n.proxy.holdReadsAfterBroadcast()
	v := n.create(callerApp, n.acceptReq(creatorOne, small, "la2", n.acceptSig(small, "la2", n.d.keys["p3"])))
	rows := n.waitAttempts(v.IntentID, "the acknowledged acceptance", 60*time.Second, func(a []bcAttemptRow) bool {
		return len(a) == 1 && a[0].Phase == "ACKNOWLEDGED"
	})
	res := n.committedTx(rows[0].Hash, 60*time.Second)
	capacity := uint32(typesv1.ErrorCode_ERROR_CODE_CAPACITY)
	if res.TxResult.Codespace != "svn" || res.TxResult.Code != capacity {
		n.t.Fatalf("phase %s: committed %s/%d, want svn/%d", name, res.TxResult.Codespace, res.TxResult.Code, capacity)
	}
	shown := n.view(v.IntentID)
	if shown.Status != stSubmitted || shown.Detail.Phase != "ACKNOWLEDGED" {
		n.t.Fatalf("phase %s: while the reads are held the API shows %s, want SUBMITTED/ACKNOWLEDGED", name, shown)
	}
	fmt.Printf("phase=%s ack=0 committed=svn/%d height=%d tx=%X api_while_held=%s/%s (fault injected by a test proxy on loopback: tx and tx_search held)\n",
		name, res.TxResult.Code, res.Height, rows[0].Hash, shown.Status, shown.Detail.Phase)
	n.proxy.clearRules()
	n.proxy.release("tx", false)
	n.proxy.release("tx_search", false)
	v = n.failed(name, v.IntentID, "EXECUTION_FAILED")
	if v.Failure.Code != capacity || v.Failure.Codespace != "svn" {
		n.t.Fatalf("phase %s: failure %s/%d, want svn/%d", name, v.Failure.Codespace, v.Failure.Code, capacity)
	}
	if after := n.d.reservedAt(n.d.clients[0], "p3", n.d.height()); after != before {
		n.t.Fatalf("phase %s: p3 reserved %d after the failed acceptance, %d before", name, after, before)
	}
	fmt.Printf("phase=%s result=FAILED(EXECUTION_FAILED svn/%d) p3_reserved=%d unchanged\n", name, capacity, before)
}

// phaseTimeoutBefore is phase TB1: the proxy drops the first broadcast
// request. The intent goes UNCERTAIN, the broadcaster looks the hash up
// with /tx before it sends again, every forwarded broadcast carries the
// same bytes, there is one attempt and one transaction at its sequence.
func phaseTimeoutBefore(n *bnet, name string) {
	from := len(n.proxy.forwardedTxs())
	events := len(n.proxy.eventLog())
	n.proxy.setRule(broadcastMethod, proxyDropRequest, 1)
	v := n.committed(name, n.create(callerApp, n.registerReq(creatorOne, "ctb1", sizeOne)).IntentID)
	rows := n.attempts(v.IntentID)
	if len(rows) != 1 {
		n.t.Fatalf("phase %s: %d attempts, want 1: %v", name, len(rows), rows)
	}
	h := rows[0].Hash
	uncertain := false
	for _, tr := range n.transitionsOf(v.IntentID) {
		if tr.To == "IN_FLIGHT/UNCERTAIN" {
			uncertain = true
		}
	}
	if !uncertain {
		n.t.Fatalf("phase %s: the intent never went UNCERTAIN after the dropped broadcast", name)
	}
	// Order of what the proxy saw: the dropped broadcast, then a /tx
	// lookup of the same hash, then the next forwarded broadcast.
	log := n.proxy.eventLog()[events:]
	dropped, lookup, resent := -1, -1, -1
	for i, e := range log {
		switch {
		case dropped < 0 && e.method == broadcastMethod && e.outcome == "dropped":
			dropped = i
		case dropped >= 0 && lookup < 0 && e.method == "tx" && bytes.Equal(e.hash[:], h):
			lookup = i
		case dropped >= 0 && resent < 0 && e.method == broadcastMethod && e.outcome == "forwarded":
			resent = i
		}
	}
	if dropped < 0 || lookup < 0 || resent < 0 || lookup > resent {
		n.t.Fatalf("phase %s: proxy order dropped=%d tx-lookup=%d resend=%d, want a /tx lookup of %X before the resend", name, dropped, lookup, resent, h)
	}
	sent := n.forwardedAllHash(name, from, h)
	onChain := n.accSeqHashes(n.serviceAddr(), rows[0].Sequence)
	if len(onChain) != 1 || !bytes.Equal(onChain[0], h) {
		n.t.Fatalf("phase %s: sequence %d names %v, want only %X", name, rows[0].Sequence, sortedHex(onChain), h)
	}
	fmt.Printf("phase=%s dropped_first_broadcast=1 (fault injected by a test proxy on loopback) went_uncertain=true tx_lookup_before_resend=true forwarded=%d all_hash=%X attempts=1 acc_seq=%d txs_at_sequence=1\n",
		name, sent, h, rows[0].Sequence)
}

// phaseTimeoutDeadline is phase TB2: the proxy drops every broadcast until
// the chain height passes T+L. Below T+L the first attempt stays live; it
// then dies by (d3); a new attempt with the same sequence and business id
// and a higher timeout height commits; one effect; p2 reserves one
// package.
func phaseTimeoutDeadline(n *bnet, name string) {
	n.ownContent(name, callerApp, sizeOne, "ctb2")
	deadline, expiry := n.terms()
	offer := n.offerOf("otb2", "ctb2", "p2", sizeOne, deadline, expiry)
	n.offerCommitted(name, callerApp, offer)
	before := n.d.reservedAt(n.d.clients[0], "p2", n.d.height())
	n.proxy.setRule(broadcastMethod, proxyDropRequest, -1)
	v := n.create(callerApp, n.acceptReq(creatorOne, offer, "ltb2", n.acceptSig(offer, "ltb2", n.d.keys["p2"])))
	first := n.waitAttempts(v.IntentID, "the first attempt", 60*time.Second, func(a []bcAttemptRow) bool { return len(a) >= 1 })[0]
	margin := int64(n.numbers["indexing_margin_blocks"].(int))
	dead := first.TimeoutHeight + margin
	// The (d3) boundary: node 0 is the broadcaster's only node, so while a
	// height read from node 0 after the attempt row is still below T+L,
	// the broadcaster cannot have judged the attempt at T+L, and the row
	// must show it live.
	samples := 0
	for limit := time.Now().Add(90 * time.Second); n.d.height() < dead; {
		if time.Now().After(limit) {
			n.t.Fatalf("phase %s: height %d not reached", name, dead)
		}
		a := n.attempts(v.IntentID)[0]
		if h := n.d.height(); h < dead {
			samples++
			if a.DeadReason != "" {
				n.t.Fatalf("phase %s: attempt %s dead at node height %d, below T+L = %d", name, a, h, dead)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if samples == 0 {
		n.t.Fatalf("phase %s: no sample below T+L = %d", name, dead)
	}
	if _, _, err := waitHeight(n.rpcs[0], dead+1, 90*time.Second); err != nil {
		n.t.Fatal(err)
	}
	n.waitAttempts(v.IntentID, "the first attempt dead by (d3)", 60*time.Second, func(a []bcAttemptRow) bool {
		return a[0].DeadReason == "D3_DEADLINE_PASSED"
	})
	dropped := n.proxy.count(broadcastMethod).Dropped
	n.proxy.clearRules()
	v = n.committed(name, v.IntentID)
	rows := n.attempts(v.IntentID)
	last := rows[len(rows)-1]
	if len(rows) < 2 || last.Phase != "COMMITTED_OK" || last.Sequence != first.Sequence || last.BusinessN != first.BusinessN ||
		last.TimeoutHeight <= first.TimeoutHeight {
		n.t.Fatalf("phase %s: attempts %v, want a later attempt with the same sequence and business id and a higher timeout height", name, rows)
	}
	for _, a := range rows[:len(rows)-1] {
		if a.DeadReason == "" {
			n.t.Fatalf("phase %s: an earlier attempt is not dead: %v", name, a)
		}
	}
	if after := n.d.reservedAt(n.d.clients[0], "p2", n.d.height()); after != before+sizeOne {
		n.t.Fatalf("phase %s: p2 reserved %d, want %d (one package)", name, after, before+sizeOne)
	}
	fmt.Printf("phase=%s dropped_broadcasts=%d (fault injected by a test proxy on loopback) live_below_t_plus_l_samples=%d t_plus_l=%d first=%s committed=%s attempts=%d p2_reserved_delta=%d\n",
		name, dropped, samples, dead, rows[0], last, len(rows), sizeOne)
}

// waitHeight waits until the node at rpc reports a height of at least h.
func waitHeight(rpc string, h int64, timeout time.Duration) (int64, string, error) {
	return devnet.WaitAbove([]string{rpc}, h-1, timeout)
}

// phaseTimeoutAfter is phase TA: the proxy forwards the broadcast and drops
// the answer. The intent goes UNCERTAIN, /tx looks the hash up before any
// second send, every forwarded broadcast carries the same bytes, and the
// intent is COMMITTED with that hash.
func phaseTimeoutAfter(n *bnet, name string) {
	from := len(n.proxy.forwardedTxs())
	events := len(n.proxy.eventLog())
	droppedBefore := n.proxy.count(broadcastMethod).AnswerDropped
	n.proxy.setRule(broadcastMethod, proxyDropResponse, 1)
	v := n.committed(name, n.create(callerApp, n.registerReq(creatorOne, "cta", sizeOne)).IntentID)
	rows := n.attempts(v.IntentID)
	if len(rows) != 1 || !bytes.Equal(rows[0].Hash, n.hashOf(v.Commitment.FirstCommitTxHash)) {
		n.t.Fatalf("phase %s: attempts %v, commitment %s", name, rows, v.Commitment.FirstCommitTxHash)
	}
	h := rows[0].Hash
	if got := n.proxy.count(broadcastMethod).AnswerDropped - droppedBefore; got != 1 {
		n.t.Fatalf("phase %s: the proxy dropped %d broadcast answers, want 1", name, got)
	}
	uncertain := false
	for _, tr := range n.transitionsOf(v.IntentID) {
		uncertain = uncertain || tr.To == "IN_FLIGHT/UNCERTAIN"
	}
	if !uncertain {
		n.t.Fatalf("phase %s: the intent never went UNCERTAIN after the dropped answer", name)
	}
	// Order of what the proxy saw: the dropped answer of h, then a /tx
	// lookup of h, before any second forwarded broadcast.
	log := n.proxy.eventLog()[events:]
	dropped, lookup, resent := -1, -1, -1
	for i, e := range log {
		switch {
		case dropped < 0 && e.method == broadcastMethod && e.outcome == "answer-dropped" && bytes.Equal(e.hash[:], h):
			dropped = i
		case dropped >= 0 && lookup < 0 && e.method == "tx" && bytes.Equal(e.hash[:], h):
			lookup = i
		case dropped >= 0 && resent < 0 && e.method == broadcastMethod && e.outcome == "forwarded":
			resent = i
		}
	}
	if dropped < 0 || lookup < 0 || (resent >= 0 && resent < lookup) {
		n.t.Fatalf("phase %s: proxy order answer-dropped=%d tx-lookup=%d second-send=%d, want a /tx lookup of %X before any second send", name, dropped, lookup, resent, h)
	}
	sent := n.forwardedAllHash(name, from, h)
	if sent < 1 {
		n.t.Fatalf("phase %s: no broadcast was forwarded", name)
	}
	fmt.Printf("phase=%s dropped_answer=1 (fault injected by a test proxy on loopback) went_uncertain=true tx_lookup_before_any_resend=true forwarded=%d all_hash=%X committed_with_that_hash=true\n", name, sent, h)
}

// phaseWorkerDeath is phase WD: the proxy forwards the broadcast and holds
// its answer and the broadcaster's tx and tx_search calls; the test sees
// the commit on a node; with the attempt SIGNED and no outcome recorded,
// serve is killed, then restarted with pass-through. COMMITTED from the
// persisted hash, no new attempt, every forwarded broadcast the same
// bytes, the signer's next sequence equal to the chain's.
func phaseWorkerDeath(n *bnet, name string) {
	n.swapServe(n.slowServe())
	from := len(n.proxy.forwardedTxs())
	n.proxy.setRule(broadcastMethod, proxyHoldResponse, -1)
	n.proxy.holdReadsAfterBroadcast()
	v := n.create(callerApp, n.registerReq(creatorOne, "cwd", sizeOne))
	rows := n.waitAttempts(v.IntentID, "the persisted attempt", 60*time.Second, func(a []bcAttemptRow) bool { return len(a) == 1 })
	bcWait(n.t, "the forwarded broadcast", 30*time.Second, func() bool { return len(n.proxy.forwardedTxs()) > from })
	res := n.committedTx(rows[0].Hash, 60*time.Second)
	a := n.attempts(v.IntentID)[0]
	n.precondition(name, "attempt SIGNED, no recorded outcome, transaction committed on chain",
		a.Phase == "SIGNED" && a.SendsRecorded == 0 && res.TxResult.Code == 0, a.String())
	n.serve.kill(n.t)
	n.proxy.clearRules()
	n.proxy.release(broadcastMethod, false)
	n.proxy.release("tx", false)
	n.proxy.release("tx_search", false)
	n.restartServe()
	v = n.committed(name, v.IntentID)
	rows = n.attempts(v.IntentID)
	if len(rows) != 1 || !bytes.Equal(n.hashOf(v.Commitment.FirstCommitTxHash), rows[0].Hash) {
		n.t.Fatalf("phase %s: attempts %v after the restart, want the one persisted attempt", name, rows)
	}
	sent := n.forwardedAllHash(name, from, rows[0].Hash)
	n.d.syncSequence(n.service)
	if s := n.signerRow(n.serviceAddr()); !s.NextSequence.Valid || uint64(s.NextSequence.Int64) != n.d.seq[n.service.addr] {
		n.t.Fatalf("phase %s: signer next_sequence %v, chain sequence %d", name, s.NextSequence, n.d.seq[n.service.addr])
	}
	fmt.Printf("phase=%s killed=serve (SIGKILL, process crash) after_commit_height=%d attempts=1 forwarded=%d all_hash=%X next_sequence=chain\n",
		name, res.Height, sent, rows[0].Hash)
}

// faultPhase runs one build-tagged fault point phase on a REGISTER_CONTENT
// for contentID: arm point, create the intent, wait for the point, check
// the precondition, SIGKILL serve, restart it, and require one effect and
// the attempts check.
func (n *bnet) faultPhase(name, point, contentID string, precondition func(a bcAttemptRow, forwarded int) (bool, string),
	after func(v bcView, rows []bcAttemptRow)) {
	n.t.Helper()
	from := len(n.proxy.forwardedTxs())
	n.armFault(n.faultServe, point)
	v := n.create(callerApp, n.registerReq(creatorOne, contentID, sizeOne))
	n.waitHit(n.faultServe, point, n.serve)
	rows := n.attempts(v.IntentID)
	if len(rows) != 1 {
		n.t.Fatalf("phase %s: %d attempts at the fault point", name, len(rows))
	}
	ok, detail := precondition(rows[0], len(n.proxy.forwardedTxs())-from)
	n.precondition(name, "fault point "+point, ok, detail)
	n.serve.kill(n.t)
	n.restartServe()
	v = n.committed(name, v.IntentID)
	rows = n.attempts(v.IntentID)
	after(v, rows)
	sent := 0
	for _, h := range n.proxy.forwardedTxs()[from:] {
		hit := false
		for _, a := range rows {
			hit = hit || bytes.Equal(h[:], a.Hash)
		}
		if !hit {
			n.t.Fatalf("phase %s: the proxy forwarded %X, not an attempt of this intent", name, h)
		}
		sent++
	}
	fmt.Printf("phase=%s point=%s (build-tagged fault point) killed=serve attempts=%d forwarded=%d committed=%s\n",
		name, point, len(rows), sent, v.Commitment.FirstCommitTxHash)
}

// phaseFaultPersist is phase WP1: death right after the attempt row
// committed, before any send was counted. The restart sends the stored
// bytes; one effect.
func phaseFaultPersist(n *bnet, name string) {
	n.faultPhase(name, "after-persist-commit", "cwp1", func(a bcAttemptRow, forwarded int) (bool, string) {
		return a.Phase == "SIGNED" && a.SendsStarted == 0 && forwarded == 0, fmt.Sprintf("%s forwarded=%d", a, forwarded)
	}, func(v bcView, rows []bcAttemptRow) {
		if len(rows) != 1 || !bytes.Equal(rows[0].Hash, n.hashOf(v.Commitment.FirstCommitTxHash)) {
			n.t.Fatalf("phase %s: attempts %v, want the stored attempt committed", name, rows)
		}
	})
}

// phaseFaultSendsStarted is phase WP2: death after the send was counted
// and before it left. The restart never marks the attempt dead by (d2): it
// resends or waits for (d3); one effect.
func phaseFaultSendsStarted(n *bnet, name string) {
	n.faultPhase(name, "after-sends-started", "cwp2", func(a bcAttemptRow, forwarded int) (bool, string) {
		return a.SendsStarted == 1 && a.SendsRecorded == 0 && forwarded == 0, fmt.Sprintf("%s forwarded=%d", a, forwarded)
	}, func(v bcView, rows []bcAttemptRow) {
		for _, a := range rows {
			if a.DeadReason == "D2_REFUSED" {
				n.t.Fatalf("phase %s: attempt marked dead by (d2) after an unrecorded send: %v", name, a)
			}
		}
	})
}

// phaseFaultAfterBroadcast is phase WP3: death after the broadcast left and
// before its outcome was recorded. The restart finds the commit by /tx;
// COMMITTED; no new attempt.
func phaseFaultAfterBroadcast(n *bnet, name string) {
	n.faultPhase(name, "after-broadcast-before-record", "cwp3", func(a bcAttemptRow, forwarded int) (bool, string) {
		return a.SendsStarted == 1 && a.SendsRecorded == 0 && forwarded == 1, fmt.Sprintf("%s forwarded=%d", a, forwarded)
	}, func(v bcView, rows []bcAttemptRow) {
		if len(rows) != 1 || !bytes.Equal(rows[0].Hash, n.hashOf(v.Commitment.FirstCommitTxHash)) {
			n.t.Fatalf("phase %s: attempts %v, want the one attempt committed", name, rows)
		}
	})
}

// phaseHeldDelivered is phase HD: the proxy holds a broadcast request and
// serve is killed. (a) The proxy forwards the held request after the death;
// it commits, and the restarted serve finds the commit by /tx: one attempt,
// committed, and no broadcast after the restart. (b) The proxy discards
// it; the restarted serve sends the attempt's stored bytes again (one
// attempt), or, once T+L passed, kills it by (d3) and commits a second
// attempt. Both have one effect.
func phaseHeldDelivered(n *bnet, name string) {
	for _, forward := range []bool{true, false} {
		label := map[bool]string{true: "a-forwarded", false: "b-discarded"}[forward]
		content := map[bool]string{true: "chda", false: "chdb"}[forward]
		from := len(n.proxy.forwardedTxs())
		n.proxy.setRule(broadcastMethod, proxyHold, 1)
		v := n.create(callerApp, n.registerReq(creatorOne, content, sizeOne))
		bcWait(n.t, "the held broadcast", 60*time.Second, func() bool { return len(n.proxy.heldTx(broadcastMethod)) == 1 }, n.serve)
		a := n.attempts(v.IntentID)
		n.precondition(name+"-"+label, "one attempt, its broadcast held by the proxy", len(a) == 1 && a[0].SendsStarted >= 1,
			fmt.Sprint(a))
		n.serve.kill(n.t)
		n.proxy.clearRules()
		n.proxy.release(broadcastMethod, forward)
		if forward {
			n.committedTx(a[0].Hash, 60*time.Second)
		}
		restartAt := len(n.proxy.forwardedTxs())
		n.restartServe()
		v = n.committed(name, v.IntentID)
		rows := n.attempts(v.IntentID)
		for _, h := range n.proxy.forwardedTxs()[from:] {
			hit := false
			for _, r := range rows {
				hit = hit || bytes.Equal(h[:], r.Hash)
			}
			if !hit {
				n.t.Fatalf("phase %s: the proxy forwarded %X, not an attempt of this intent", name, h)
			}
		}
		after := n.proxy.forwardedTxs()[restartAt:]
		committedHash := n.hashOf(v.Commitment.FirstCommitTxHash)
		outcome := ""
		switch {
		case forward:
			// (a) Reconcile finds the commit; nothing is sent again.
			if len(rows) != 1 || !bytes.Equal(rows[0].Hash, committedHash) || len(after) != 0 {
				n.t.Fatalf("phase %s-%s: attempts %v, %d broadcasts after the restart; want the one attempt committed and none", name, label, rows, len(after))
			}
			outcome = "reconciled_by_tx forwards_after_restart=0"
		case len(rows) == 1:
			// (b) The attempt is still live: its stored bytes are sent again.
			if !bytes.Equal(rows[0].Hash, committedHash) || len(after) == 0 {
				n.t.Fatalf("phase %s-%s: attempt %v, %d broadcasts after the restart; want its bytes resent and committed", name, label, rows, len(after))
			}
			for _, h := range after {
				if !bytes.Equal(h[:], rows[0].Hash) {
					n.t.Fatalf("phase %s-%s: the restarted serve forwarded %X, not the attempt's %X", name, label, h, rows[0].Hash)
				}
			}
			outcome = fmt.Sprintf("resent_same_bytes forwards_after_restart=%d", len(after))
		default:
			// (b) T+L passed first: (d3), then one new attempt commits.
			if len(rows) != 2 || rows[0].DeadReason != "D3_DEADLINE_PASSED" || !bytes.Equal(rows[1].Hash, committedHash) {
				n.t.Fatalf("phase %s-%s: attempts %v; want (d3) then one new attempt committed", name, label, rows)
			}
			outcome = "d3_then_one_new_attempt"
		}
		fmt.Printf("phase=%s case=%s held_request=%s (fault injected by a test proxy on loopback) killed=serve attempts=%d outcome=%s committed=%s\n",
			name, label, map[bool]string{true: "forwarded after the death", false: "discarded"}[forward], len(rows), outcome, v.Commitment.FirstCommitTxHash)
	}
}

// phaseExpired is phase EX: the proxy drops every broadcast; the intent's
// expires_at passes, then its attempt's timeout height plus L. The intent
// is FAILED(EXPIRED_DEAD); the proxy saw one broadcast request for every
// send the outbox counted, and none later than expires_at plus the RPC
// timeout. The outbox refuses to count a send after expires_at on the
// database clock (BeginSend); a send counted just before it reaches the
// proxy within the RPC timeout or never.
func phaseExpired(n *bnet, name string) {
	forwarded := n.proxy.count(broadcastMethod).Forwarded
	n.proxy.setRule(broadcastMethod, proxyDropRequest, -1)
	req := n.registerReq(creatorOne, "cex", sizeOne)
	req.ExpiresInSeconds = 6
	v := n.create(callerApp, req)
	v = n.failed(name, v.IntentID, "EXPIRED_DEAD")
	n.proxy.clearRules()
	last := time.Time{}
	for _, e := range n.proxy.eventLog() {
		if e.method == broadcastMethod && e.at.After(last) {
			last = e.at
		}
	}
	if n.proxy.count(broadcastMethod).Forwarded != forwarded {
		n.t.Fatalf("phase %s: a broadcast was forwarded while every broadcast was dropped", name)
	}
	rpcTimeout := time.Duration(n.numbers["rpc_timeout_seconds"].(int)) * time.Second
	if last.After(v.ExpiresAt.Add(rpcTimeout)) {
		n.t.Fatalf("phase %s: a broadcast request reached the proxy at %s, after expires_at %s plus the RPC timeout %s",
			name, last.UTC().Format(time.RFC3339Nano), v.ExpiresAt.UTC().Format(time.RFC3339Nano), rpcTimeout)
	}
	rows := n.attempts(v.IntentID)
	started, requests := 0, 0
	for _, a := range rows {
		started += a.SendsStarted
		for _, e := range n.proxy.eventLog() {
			if e.method == broadcastMethod && bytes.Equal(e.hash[:], a.Hash) {
				requests++
			}
		}
	}
	if requests != started {
		n.t.Fatalf("phase %s: the outbox counted %d sends and the proxy saw %d broadcast requests of its attempts", name, started, requests)
	}
	fmt.Printf("phase=%s result=FAILED(EXPIRED_DEAD) expires_at=%s last_broadcast_request=%s (dropped by a test proxy on loopback) last_minus_expiry=%s bound=rpc_timeout_%s sends_counted=%d requests_seen=%d forwarded=0\n",
		name, v.ExpiresAt.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano), last.Sub(v.ExpiresAt).Round(time.Millisecond), rpcTimeout, started, requests)
}
