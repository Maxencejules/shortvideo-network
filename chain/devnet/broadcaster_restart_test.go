package devnet_test

// Restart phases of TestDevnetBroadcasterLifecycle: PG (PostgreSQL frozen,
// then crash-restarted), SG (signer death at a build-tagged fault point),
// BR (seeded SIGTERM and SIGKILL of serve during a 50-intent batch) and RS
// (the DR-10-41 window after a validator restart). Process kills are
// process crashes, not host power loss.

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
)

// postgresProcesses returns the postmaster pid (from postmaster.pid) and
// the pids of its children.
func (n *bnet) postgresProcesses() (int, []int) {
	n.t.Helper()
	raw, err := os.ReadFile(filepath.Join(n.db.ClusterDir, "data", "postmaster.pid"))
	if err != nil {
		n.t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(raw), "\n")
	postmaster, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil {
		n.t.Fatalf("postmaster.pid: %v", err)
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		n.t.Fatal(err)
	}
	var children []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// /proc/<pid>/stat: pid (comm) state ppid ...; comm may hold spaces.
		rest := string(stat[strings.LastIndexByte(string(stat), ')')+1:])
		fields := strings.Fields(rest)
		if len(fields) > 1 && fields[1] == strconv.Itoa(postmaster) {
			children = append(children, pid)
		}
	}
	return postmaster, children
}

// runtimeBackend returns the pid of a PostgreSQL backend serving the
// broadcaster's runtime role.
func (n *bnet) runtimeBackend(children []int) int {
	n.t.Helper()
	for _, pid := range children {
		cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
		if err == nil && strings.Contains(string(cmdline), "postgres: "+roleRuntime+" ") {
			return pid
		}
	}
	n.t.Fatalf("no backend of %s among %v", roleRuntime, children)
	return 0
}

// broadcastArrivals counts the broadcast requests that reached the proxy:
// every request is counted once, when it arrives (held, dropped, reset or
// forwarded at once); a later release or discard of a held request is not
// an arrival.
func (n *bnet) broadcastArrivals() int {
	c := 0
	for _, e := range n.proxy.eventLog() {
		if e.method == broadcastMethod && (e.outcome == "held" || e.outcome == "dropped" || e.outcome == "reset" || e.outcome == "forwarded") {
			c++
		}
	}
	return c
}

// pgControlResend measures how long a healthy database lets a discarded
// send wait before the worker sends the attempt again: the proxy holds one
// register's broadcast and discards it (the caller sees the connection
// closed), the worker records the unknown outcome, looks the hash up and
// resends the same bytes. It returns that time; the intent commits.
func (n *bnet) pgControlResend(name string) time.Duration {
	n.t.Helper()
	n.proxy.setRule(broadcastMethod, proxyHold, 1)
	v := n.create(callerApp, n.registerReq(creatorOne, "cpg0", sizeOne))
	bcWait(n.t, "the held control broadcast", 60*time.Second, func() bool { return len(n.proxy.heldTx(broadcastMethod)) == 1 }, n.serve)
	held := n.proxy.heldTx(broadcastMethod)[0]
	from := len(n.proxy.forwardedTxs())
	discarded := time.Now()
	n.proxy.release(broadcastMethod, false)
	bcWait(n.t, "the control resend", 30*time.Second, func() bool {
		for _, h := range n.proxy.forwardedTxs()[from:] {
			if h == held {
				return true
			}
		}
		return false
	}, n.serve)
	took := time.Since(discarded)
	n.committed(name, v.IntentID)
	return took
}

// phasePostgres is phase PG, a crash-recovery demonstration with one
// meaningful negative check. Control: with a healthy database, a
// discarded send is sent again after C seconds. Then, with one intent
// ACKNOWLEDGED (its commit observation held by the proxy), one SIGNED (its
// broadcast held) and one QUEUED behind it, every PostgreSQL process is
// frozen (SIGSTOP) and the held broadcast is discarded at once: the worker
// must record that unknown outcome before it may send again, so it sends
// nothing while the database is frozen, for C + 2 s (at least 4 s). Then
// one runtime backend is killed and the processes continued: the
// postmaster treats the backend's death as a crash, ends every connection,
// recovers and accepts connections again (a PostgreSQL crash restart, the
// postmaster pid unchanged, because dev-db removes the cluster when it
// exits; a process crash, not a host power loss). After the proxy
// releases, every intent reaches the chain's truth. The harness role is
// SELECT-only, so no history is edited by hand.
func phasePostgres(n *bnet, name string) {
	n.swapServe(n.slowServe())
	control := n.pgControlResend(name)
	frozen := max(4*time.Second, control+2*time.Second)
	offer := n.relaySetup(name, "cpg", "opg", "p2")
	// Only the relay's commit observation is held: its tx lookups and the
	// searches that name its hash, its lease or the broadcaster's sequence.
	n.proxy.holdReadsAfterBroadcast("'lpg'", "acc_seq='"+bech(n.bcastAddr())+"/")
	relay := n.create(callerApp, n.acceptReq(creatorOne, offer, "lpg", n.acceptSig(offer, "lpg", n.d.keys["p2"])))
	n.waitAttempts(relay.IntentID, "the acknowledged relay", 60*time.Second, func(a []bcAttemptRow) bool {
		return len(a) == 1 && a[0].Phase == "ACKNOWLEDGED"
	})
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	signed := n.create(callerApp, n.registerReq(creatorOne, "cpg1", sizeOne))
	bcWait(n.t, "the held register", 60*time.Second, func() bool { return len(n.proxy.heldTx(broadcastMethod)) >= 1 }, n.serve)
	queued := n.create(callerApp, n.registerReq(creatorOne, "cpg2", sizeOne))
	bcWait(n.t, "the queued register", 30*time.Second, func() bool {
		r := n.intentRow(queued.IntentID)
		return r.State == "REQUESTED" && r.Phase == "QUEUED" && r.Validated
	}, n.serve)
	states := []string{}
	for _, id := range []string{relay.IntentID, signed.IntentID, queued.IntentID} {
		r := n.intentRow(id)
		states = append(states, r.State+"/"+r.Phase)
	}
	n.precondition(name, "one intent each ACKNOWLEDGED, SIGNED and QUEUED",
		states[0] == "IN_FLIGHT/ACKNOWLEDGED" && states[1] == "IN_FLIGHT/SIGNED" && states[2] == "REQUESTED/QUEUED", strings.Join(states, ","))

	postmaster, children := n.postgresProcesses()
	backend := n.runtimeBackend(children)
	all := append([]int{postmaster}, children...)
	for _, pid := range all {
		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			n.t.Fatalf("SIGSTOP %d: %v", pid, err)
		}
	}
	before := n.broadcastArrivals()
	// The SIGNED register's send now fails: its outcome must be recorded
	// before the worker may send again, and the database cannot record it.
	if discarded := n.proxy.release(broadcastMethod, false); discarded != 1 {
		n.t.Fatalf("phase %s: %d held broadcasts discarded, want the SIGNED register's", name, discarded)
	}
	time.Sleep(frozen)
	during := n.broadcastArrivals()
	if err := syscall.Kill(backend, syscall.SIGKILL); err != nil {
		n.t.Fatalf("SIGKILL backend %d: %v", backend, err)
	}
	for _, pid := range all {
		_ = syscall.Kill(pid, syscall.SIGCONT)
	}
	if during != before {
		n.t.Fatalf("phase %s: the proxy saw %d broadcast requests while PostgreSQL was frozen (a healthy database resent after %s)",
			name, during-before, control.Round(time.Millisecond))
	}
	// The crash restart replaces every child of the postmaster (backends
	// and auxiliary processes) while the postmaster pid stays the same.
	bcWait(n.t, "the PostgreSQL crash restart", 2*time.Minute, func() bool {
		pm, now := n.postgresProcesses()
		if pm != postmaster {
			n.t.Fatalf("phase %s: the postmaster pid changed from %d to %d", name, postmaster, pm)
		}
		for _, pid := range children {
			if containsPID(now, pid) {
				return false
			}
		}
		ctx, cancel := bcCtx()
		defer cancel()
		var one int
		return n.reader.QueryRowContext(ctx, `SELECT 1`).Scan(&one) == nil
	})
	n.proxy.clearRules()
	n.proxy.release(broadcastMethod, true)
	n.proxy.release("tx", false)
	n.proxy.release("tx_search", false)
	if n.serve.exited() {
		fmt.Printf("phase=%s serve exited %d after the database crash; restarted\n", name, n.serve.exitCode())
		n.restartServe()
	}
	for _, id := range []string{relay.IntentID, signed.IntentID, queued.IntentID} {
		n.committed(name, id)
	}
	n.swapServe(n.serveCfg)
	fmt.Printf("phase=%s control_resend_seconds=%.1f preconditions=%s held_send_discarded_while_frozen=1 postgres_frozen_seconds=%.1f broadcast_requests_while_frozen=0 killed_backend=%d replaced_children=%d postmaster=%d (crash restart with the postmaster pid unchanged; process crash, not host power loss) result=3 COMMITTED\n",
		name, control.Seconds(), strings.Join(states, ","), frozen.Seconds(), backend, len(children), postmaster)
}

func containsPID(pids []int, pid int) bool {
	for _, p := range pids {
		if p == pid {
			return true
		}
	}
	return false
}

// phaseSignerDeath is phase SG: the signer dies at the build-tagged fault
// point after-decode-before-reply. No attempt row, no consumed sequence
// and no broadcast follow; the intent stays REQUESTED; after the signer
// restarts it commits.
func phaseSignerDeath(n *bnet, name string) {
	n.d.syncSequence(n.service)
	seq := n.d.seq[n.service.addr]
	forwarded := n.proxy.count(broadcastMethod).Forwarded
	n.armFault(n.faultSign, "after-decode-before-reply")
	v := n.create(callerApp, n.registerReq(creatorOne, "csg", sizeOne))
	n.waitHit(n.faultSign, "after-decode-before-reply", n.signer)
	n.signer.kill(n.t)
	time.Sleep(2 * time.Second)
	r := n.intentRow(v.IntentID)
	n.d.syncSequence(n.service)
	n.precondition(name, "signer killed after decoding: no attempt, no consumed sequence, no broadcast, intent REQUESTED",
		len(n.attempts(v.IntentID)) == 0 && r.State == "REQUESTED" && n.d.seq[n.service.addr] == seq &&
			n.proxy.count(broadcastMethod).Forwarded == forwarded, r.String())
	n.startSigner()
	v = n.committed(name, v.IntentID)
	fmt.Printf("phase=%s point=after-decode-before-reply (build-tagged fault point) killed=signer attempts_while_dead=0 sequence_unchanged=%d result=COMMITTED tx=%s\n",
		name, seq, v.Commitment.FirstCommitTxHash)
}

// phaseRestarts is phase BR: a 50-intent batch across both signers (34
// REGISTER_CONTENT on the service key, 16 RELAY_ACCEPT_LEASE on the
// broadcaster key) while serve is stopped a fixed number of times, with
// SIGTERM or SIGKILL at seeded random points. After the faults stop every
// intent is terminal within a bounded wait, and the accounting check
// compares each one's API status with a recomputation from chain reads.
func phaseRestarts(n *bnet, name string) {
	const seed = 1108
	rng := rand.New(rand.NewPCG(seed, seed))
	var contents []string
	for i := 0; i < 16; i++ {
		contents = append(contents, fmt.Sprintf("cbr-r%02d", i))
	}
	n.ownContent(name, callerApp, sizeOne, contents...)
	deadline, expiry := n.terms()
	var offers []bcIntentReq
	for i, c := range contents {
		offers = append(offers, n.offerReq(creatorOne, n.offerOf(fmt.Sprintf("obr-%02d", i), c, "p2", sizeOne, deadline, expiry)))
	}
	n.createAll(name, offers)
	var batch []bcIntentReq
	for i := 0; i < 34; i++ {
		batch = append(batch, n.registerReq(creatorOne, fmt.Sprintf("cbr-b%02d", i), sizeOne))
	}
	for i, c := range contents {
		offer := n.offerOf(fmt.Sprintf("obr-%02d", i), c, "p2", sizeOne, deadline, expiry)
		lease := fmt.Sprintf("lbr-%02d", i)
		batch = append(batch, n.acceptReq(creatorOne, offer, lease, n.acceptSig(offer, lease, n.d.keys["p2"])))
	}
	rng.Shuffle(len(batch), func(i, j int) { batch[i], batch[j] = batch[j], batch[i] })
	ids := n.postAll(batch)
	var faults []string
	for i := 0; i < 6; i++ {
		wait := time.Duration(300+rng.IntN(1700)) * time.Millisecond
		time.Sleep(wait)
		if rng.IntN(2) == 0 {
			if code := n.serve.stop(n.t); code != 0 {
				n.t.Fatalf("phase %s: serve exit %d on SIGTERM", name, code)
			}
			faults = append(faults, fmt.Sprintf("SIGTERM@+%dms", wait.Milliseconds()))
		} else {
			n.serve.kill(n.t)
			faults = append(faults, fmt.Sprintf("SIGKILL@+%dms", wait.Milliseconds()))
		}
		n.restartServe()
	}
	started := time.Now()
	for _, id := range ids {
		n.committed(name, id)
	}
	fmt.Printf("phase=%s seed=%d batch=50 service=34 broadcaster=16 faults=%s all_committed_after=%.1fs\n",
		name, seed, strings.Join(faults, ","), time.Since(started).Seconds())
}

// postAll creates every request at once (concurrently), retrying the
// documented 503 answer (the retries are printed), and returns the intent
// ids in request order.
func (n *bnet) postAll(reqs []bcIntentReq) []string {
	n.t.Helper()
	bodies := make([][]byte, len(reqs))
	for i, req := range reqs {
		body, err := json.Marshal(req)
		if err != nil {
			n.t.Fatal(err)
		}
		bodies[i] = body
	}
	results := n.postConcurrently(callerApp, n.apiURL()+"/v1/intents", bodies)
	fmt.Printf("concurrent_creates=%d retried_503=%d\n", len(reqs), retriesOf(results))
	ids := make([]string, len(reqs))
	for i, r := range results {
		if r.status != http.StatusCreated {
			n.t.Fatalf("create %s: %+v", reqs[i].IdempotencyKey, r)
		}
		obj := reqs[i].object
		obj.Caller = callerApp
		n.objects[r.id] = obj
		ids[i] = r.id
	}
	return ids
}

// createAll creates every request concurrently and waits until each is
// COMMITTED.
func (n *bnet) createAll(phase string, reqs []bcIntentReq) {
	n.t.Helper()
	for _, id := range n.postAll(reqs) {
		n.committed(phase, id)
	}
}

// phaseValidatorRestart is phase RS (DR-10-41): two validators stop, so the
// chain cannot commit; validator 0 (the broadcaster's node) restarts, and
// until it commits a block its CheckTx verifies with account number 0. An
// intent of the signer whose account number is not 0 gets CheckTx sdk 4,
// recorded in transitions; once the stopped validators return and the
// height advances, the intent is COMMITTED.
func phaseValidatorRestart(n *bnet, name string) {
	d := n.d
	signer, kind := n.service, "REGISTER_CONTENT"
	var req bcIntentReq
	if d.numbers[n.service.addr] == 0 {
		signer, kind = n.bcast, "RELAY_ACCEPT_LEASE"
		offer := n.relaySetup(name, "crs", "ors", "p2")
		req = n.acceptReq(creatorOne, offer, "lrs", n.acceptSig(offer, "lrs", d.keys["p2"]))
	} else {
		req = n.registerReq(creatorOne, "crs", sizeOne)
	}
	fmt.Printf("phase=%s signer=%s account_number=%d kind=%s (the signer with a nonzero account number)\n", name, signer.name, d.numbers[signer.addr], kind)
	n.serial++
	n.swapServe(n.writeServeConfig(fmt.Sprintf("serve-rs-%d.json", n.serial), n.listen, n.proxy.url(), map[string]any{"stall_window_seconds": 120}))
	n.stopValidator(3)
	n.stopValidator(2)
	// Validator 0 must not be behind validator 1 when it restarts: a block
	// it fetched from its peer would commit and close the DR-10-41 window
	// before the broadcaster's send. The two running validators settle at
	// one height and app hash first.
	var stalledAt int64
	bcWait(n.t, "validators 0 and 1 settled at one height", time.Minute, func() bool {
		h, err := devnet.Stable(n.rpcs[:2], 2*time.Second)
		stalledAt = h
		return err == nil
	})
	n.stopValidator(0)
	n.startValidator(0)
	waitRPC(n.t, d.clients[0], 60*time.Second)
	v := n.create(callerApp, req)
	bcWait(n.t, "a CheckTx sdk 4 refusal in transitions", 90*time.Second, func() bool { return n.sawSdk4(v.IntentID) }, n.serve)
	// The chain did not commit meanwhile: at most the block in flight at
	// the stop is above the height read before it.
	if h := d.height(); h > stalledAt+1 {
		n.t.Fatalf("phase %s: the chain reached height %d with two validators stopped (stalled at %d)", name, h, stalledAt)
	}
	n.startValidator(2)
	n.startValidator(3)
	if _, _, err := waitHeight(n.rpcs[0], stalledAt+3, 2*time.Minute); err != nil {
		n.t.Fatal(tail(n.layout, err))
	}
	v = n.committed(name, v.IntentID)
	n.swapServe(n.serveCfg)
	fmt.Printf("phase=%s stalled_at=%d restarted=validator0(%s) stopped=validator2(%s),validator3(%s) checktx=sdk/4 recorded_in_transitions=true result=COMMITTED height=%d\n",
		name, stalledAt, n.layout.Topology.Validators[0].Name, n.layout.Topology.Validators[2].Name, n.layout.Topology.Validators[3].Name,
		v.Commitment.Height)
}

// sawSdk4 reports whether a transitions row of the intent records a
// CheckTx refusal with sdk code 4.
func (n *bnet) sawSdk4(id string) bool {
	for _, tr := range n.transitionsOf(id) {
		var obs struct {
			Class     string `json:"class"`
			Code      int64  `json:"code"`
			Codespace string `json:"codespace"`
		}
		if tr.Observation != "" && json.Unmarshal([]byte(tr.Observation), &obs) == nil &&
			obs.Class == "MEMPOOL_REJECTED" && obs.Codespace == "sdk" && obs.Code == 4 {
			return true
		}
	}
	return false
}
