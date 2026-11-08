package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// TestSecondWorkerDoesNotDriveSigner (plan 8.1, 13.4 CS at unit level):
// a second worker on the same database cannot take a signer whose lock the
// first holds, so it signs, stores and sends nothing for it. When the first
// worker's lock connection is terminated, its next pass fails and drops the
// session; the second worker then takes the lock, settles the live attempt
// and drives the signer's queue. Both workers run in this test process,
// each with its own database connection: the advisory lock belongs to the
// connection. The cmd smoke test runs the same refusal across two OS
// processes (reconcile-once while serve runs).
func TestSecondWorkerDoesNotDriveSigner(t *testing.T) {
	e := newEnv(t)
	first := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s1 := e.session(policy.ClassService)
	e.pass(s1)
	e.wantState(first, "IN_FLIGHT", "ACKNOWLEDGED", "")
	w2, err := New(e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	s2 := newSession(w2, false, w2.keys[policy.ClassService])
	t.Cleanup(s2.close)
	second := e.enqueue(principalApp, "reg-2", policy.KindRegisterContent, e.registerMsg("c2"), time.Hour)
	calls := e.signer.callCount()
	e.pass(s2)
	if s2.ss != nil || len(e.attempts(second)) != 0 || e.signer.callCount() != calls {
		t.Fatal("the second worker drove a signer the first holds")
	}
	if n := e.count(`SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_catalog.pg_locks
		WHERE locktype = 'advisory' AND classid = $1::bigint::oid AND objid = $2::bigint::oid AND objsubid = 2) t`,
		0x53564e01, 1); n != 1 {
		t.Fatalf("terminated %d lock holders, want 1", n)
	}
	e.node.block()
	if err := s1.pass(e.ctx); err == nil {
		t.Fatal("a pass on a terminated lock connection succeeded")
	}
	if s1.ss != nil {
		t.Fatal("the first worker kept a session without its lock")
	}
	e.pass(s2)
	e.wantState(first, "COMMITTED", "", "")
	if len(e.attempts(second)) != 1 {
		t.Fatal("the second worker did not drive the queue after taking the lock")
	}
}

// TestReconcileOnceTakesLock (plan 8.1 m10): reconcile-once takes the same
// advisory locks and reports ErrLockHeld, naming the session whose lock is
// held and without touching that signer, while another session holds one;
// with the locks free it runs one pass of every loop.
func TestReconcileOnceTakesLock(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	holder := e.session(policy.ClassService)
	if err := holder.open(e.ctx); err != nil {
		t.Fatal(err)
	}
	err := e.w.ReconcileOnce(e.ctx)
	if !errors.Is(err, ErrLockHeld) || !strings.Contains(err.Error(), "("+e.service.info.Name+")") ||
		strings.Contains(err.Error(), "("+e.relayer.info.Name+")") || strings.Contains(err.Error(), "(forwarder)") {
		t.Fatalf("reconcile-once with the service lock held: %v, want ErrLockHeld naming the service session only", err)
	}
	if len(e.attempts(id)) != 0 {
		t.Fatal("reconcile-once drove a signer another session holds")
	}
	holder.close()
	if err := e.w.ReconcileOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.attempts(id)) != 1 {
		t.Fatal("reconcile-once did not run the signer's pass")
	}
	if _, err := e.st.ReadChainHealth(e.ctx); err != nil {
		t.Fatalf("reconcile-once did not run the observer: %v", err)
	}
}

// TestSignerRefusalOutcomes (plan 13.4 SG at unit level): an unreachable
// signer and a refusal that is not about the message (no fresh height)
// leave the intent REQUESTED with nothing stored or sent; a refusal of the
// message (an unregistered hosting-intent key) fails the intent's
// validation.
func TestSignerRefusalOutcomes(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.signer.refuse = errors.New("dial unix /run/signer.sock: connect: no such file or directory")
	if err := s.pass(e.ctx); err != nil {
		t.Fatalf("an unreachable signer failed the pass: %v", err)
	}
	e.wantState(id, "REQUESTED", "QUEUED", "")
	e.signer.refuse = &policy.Refusal{Code: policy.CodeNoFreshHeight}
	e.pass(s)
	e.wantState(id, "REQUESTED", "QUEUED", "")
	if !e.logged("SIGNER_REFUSED") || len(e.attempts(id)) != 0 || e.node.broadcastCount() != 0 {
		t.Fatal("a transient signer refusal stored or sent something, or raised no alert")
	}
	e.signer.refuse = &policy.Refusal{Code: policy.CodeIntentKeyUnregistered}
	e.pass(s)
	e.wantState(id, "FAILED", "", "UNAUTHORIZED")
	if len(e.attempts(id)) != 0 {
		t.Fatal("a refused message was stored")
	}
}

// TestUnauthorizedRefusalHaltsSigner (plan 8.7): CheckTx refuses every send
// with svn UNAUTHORIZED; the attempt dies by (d2), the intent fails with
// REJECTED{svn 1}, and the signer halts with UNAUTHORIZED.
func TestUnauthorizedRefusalHaltsSigner(t *testing.T) {
	e := newEnv(t)
	e.node.onBroadcast = refuseAll(typesv1.ErrorCode_ERROR_CODE_UNAUTHORIZED)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.pass(s)
	e.wantState(id, "FAILED", "", "REJECTED")
	sg, err := e.st.GetSigner(e.ctx, e.service.info.Address)
	if err != nil || sg.HaltedReason != "UNAUTHORIZED" {
		t.Fatalf("signer %+v %v, want halted UNAUTHORIZED", sg, err)
	}
}

// TestSdk4RetryWaitsTwoBlocks (DR-11-18): an attempt refused at CheckTx
// with sdk code 4 dies by (d2) and the intent retries; the next attempt is
// built at least two blocks after the dead one, so it does not start one
// block later even when it is due.
func TestSdk4RetryWaitsTwoBlocks(t *testing.T) {
	e := newEnv(t)
	refused := false
	e.node.onBroadcast = func(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
		if refused {
			return nil, nil, false
		}
		refused = true
		h := sumOf(raw)
		return &coretypes.ResultBroadcastTx{Code: 4, Codespace: "sdk", Hash: h[:]}, nil, true
	}
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.pass(s)
	atts := e.attempts(id)
	if atts[0].DeadReason != string(store.DeadD2Refused) {
		t.Fatalf("first attempt %+v, want DEAD D2_REFUSED", atts[0])
	}
	time.Sleep(e.cfg.Numbers.BackoffCap)
	for i := 0; i < 2; i++ {
		e.pass(s)
		if len(e.attempts(id)) != 1 {
			t.Fatalf("a retry after sdk 4 started %d blocks after the dead attempt", i)
		}
		e.node.block()
	}
	e.pass(s)
	atts = e.attempts(id)
	if len(atts) != 2 || atts[1].TimeoutHeight < atts[0].TimeoutHeight+outbox.Sdk4RetryGapBlocks {
		t.Fatalf("retry %+v after %+v", atts, atts[0])
	}
}

// TestRunCommitsAndStops runs the worker's loops as the service does: an
// enqueued registration is signed, sent and, once a block holds it,
// COMMITTED by the loops alone, and Run returns when its context ends.
func TestRunCommitsAndStops(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan error, 1)
	go func() { done <- e.w.Run(ctx) }()
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	waitFor(t, "acknowledged", 30*time.Second, func() bool { return e.intent(id).phase == "ACKNOWLEDGED" })
	e.node.block()
	waitFor(t, "committed", 30*time.Second, func() bool { return e.intent(id).state == "COMMITTED" })
	waitFor(t, "owner confirmed", 30*time.Second, func() bool { return e.ownerStatus(id) == "EFFECTIVE" })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	if _, err := e.st.ReadChainHealth(e.ctx); err != nil {
		t.Fatalf("Run did not run the observer: %v", err)
	}
}

// TestConfigValidation: New refuses a configuration with a missing
// dependency, a key set that is not one SERVICE and one BROADCASTER key, a
// number outside its hard bound, a declared gas that is missing, out of
// bounds or for a kind no key signs, or a repeated caller.
func TestConfigValidation(t *testing.T) {
	e := newEnv(t)
	cases := map[string]func(c *Config){
		"no store":         func(c *Config) { c.Store = nil },
		"no chain":         func(c *Config) { c.Chain = nil },
		"no signature":     func(c *Config) { c.RequestSignature = nil },
		"no registry":      func(c *Config) { c.Registry = nil },
		"one key":          func(c *Config) { c.Keys = c.Keys[:1] },
		"two service keys": func(c *Config) { c.Keys = []signer.KeyInfo{c.Keys[0], c.Keys[0]} },
		"unknown class":    func(c *Config) { k := c.Keys[1]; k.Class = "AUDITOR"; c.Keys = []signer.KeyInfo{c.Keys[0], k} },
		"address not its key": func(c *Config) {
			k := c.Keys[1]
			k.Address = c.Keys[0].Address
			c.Keys = []signer.KeyInfo{c.Keys[0], k}
		},
		"offset below K":    func(c *Config) { c.Numbers.TimeoutOffset = 4 },
		"poll above bound":  func(c *Config) { c.Numbers.PollInterval = 11 * time.Second },
		"base above cap":    func(c *Config) { c.Numbers.BackoffBase, c.Numbers.BackoffCap = 2*time.Second, time.Second },
		"lifetime inverted": func(c *Config) { c.Numbers.MinLifetime, c.Numbers.MaxLifetime = time.Hour, time.Minute },
		"margin above 100":  func(c *Config) { c.Numbers.WithdrawMarginPercent = 101 },
		"pages zero":        func(c *Config) { c.Numbers.DeadlinePages = 0 },
		"gas missing":       func(c *Config) { c.Numbers.DeclaredGas = map[policy.Kind]uint64{policy.KindOfferLease: 1} },
		"gas zero": func(c *Config) {
			c.Numbers.DeclaredGas = cloneGas(c.Numbers.DeclaredGas)
			c.Numbers.DeclaredGas[policy.KindOfferLease] = 0
		},
		"gas for forward": func(c *Config) {
			c.Numbers.DeclaredGas = cloneGas(c.Numbers.DeclaredGas)
			c.Numbers.DeclaredGas[policy.KindForwardProviderTx] = 1
		},
		"outbox limits":   func(c *Config) { c.Outbox.IndexingMargin = 0 },
		"repeated caller": func(c *Config) { c.Callers = append(c.Callers, c.Callers[0]) },
	}
	for name, mutate := range cases {
		c := e.cfg
		c.Keys = append([]signer.KeyInfo(nil), e.cfg.Keys...)
		mutate(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: New accepted the configuration", name)
		}
	}
	if _, err := LockKeyOf("AUDITOR"); err == nil || !strings.Contains(err.Error(), "no lock key") {
		t.Fatalf("lock key of another class: %v", err)
	}
}

func cloneGas(m map[policy.Kind]uint64) map[policy.Kind]uint64 {
	out := map[policy.Kind]uint64{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TestForeignUseOfLiveSequenceHalts (DR-11-15, plan 13.4 MR at unit
// level): our attempt's sends are lost while a transaction the broadcaster
// never stored, signed with the service key, commits at the attempt's
// sequence. Once the height reaches timeout_height + L, the account
// sequence is above the attempt's and the acc_seq search names the foreign
// hash: the attempt stays live, the signer halts with SIGNER_CONFLICT, and
// nothing more is sent. After the operator's signer-clear the attempt dies
// as D3_SEQUENCE_USED and the intent's next attempt uses the next sequence.
func TestForeignUseOfLiveSequenceHalts(t *testing.T) {
	e := newEnv(t)
	e.node.onBroadcast = dropAll
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	att := e.attempts(id)[0]
	foreign := withSigner(offerMsg("o-foreign", "c9"), e.service.info.Address)
	setBusinessID(foreign, "offer-foreign")
	raw, foreignHash := e.signWith(e.service, foreign, att.Sequence)
	e.node.onBroadcast = nil
	e.node.commitDirect(raw)
	e.node.onBroadcast = dropAll
	e.node.blocks(int(att.TimeoutHeight) + int(outboxLimits().IndexingMargin) - int(e.node.heightNow()))
	e.pass(s)
	sg, err := e.st.GetSigner(e.ctx, e.service.info.Address)
	if err != nil || sg.HaltedReason != "SIGNER_CONFLICT" {
		t.Fatalf("signer %+v %v, want SIGNER_CONFLICT", sg, err)
	}
	if got := e.attempts(id)[0]; !got.Phase.Live() {
		t.Fatalf("attempt %s after a foreign use of its sequence, want live", got.Phase)
	}
	if status, err := e.st.Conflict(e.ctx, e.service.info.Address, att.Sequence, foreignHash); err != nil || status != store.ConflictOpen {
		t.Fatalf("signer event %s %v", status, err)
	}
	if err := s.ss.ClearConflict(e.ctx, att.Sequence, foreignHash, "signer-clear"); err != nil {
		t.Fatal(err)
	}
	e.node.onBroadcast = nil
	e.pass(s)
	if got := e.attempts(id)[0]; got.DeadReason != string(store.DeadD3SequenceUsed) {
		t.Fatalf("attempt %s %s after signer-clear, want DEAD D3_SEQUENCE_USED", got.Phase, got.DeadReason)
	}
	e.until("retry at the next sequence", 100, func() { e.pass(s) }, func() bool { return len(e.attempts(id)) == 2 })
	if got := e.attempts(id)[1]; got.Sequence != att.Sequence+1 {
		t.Fatalf("retry at sequence %d, want %d", got.Sequence, att.Sequence+1)
	}
}

// TestCommittedBytesMismatchHaltsSigner (plan 8.4, 8.5): /tx answers the
// attempt's hash with other bytes. The chain client refuses the answer as
// an integrity error; the worker raises the alert, halts the signer with
// INTEGRITY and changes neither the attempt nor the intent.
func TestCommittedBytesMismatchHaltsSigner(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.node.block()
	att := e.attempts(id)[0]
	e.node.mu.Lock()
	e.node.txs[sumOf32(att.TxHash)].Tx = []byte("other bytes")
	e.node.mu.Unlock()
	e.pass(s)
	e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
	sg, err := e.st.GetSigner(e.ctx, e.service.info.Address)
	if err != nil || sg.HaltedReason != "INTEGRITY" || !e.logged("CHAIN_INTEGRITY") {
		t.Fatalf("signer %+v %v, want halted INTEGRITY with an alert", sg, err)
	}
	if got := e.attempts(id)[0]; got.Phase != store.AttemptAcknowledged {
		t.Fatalf("attempt %s after an integrity error, want unchanged", got.Phase)
	}
}

// TestSendNeedsCountedStart (plan 8.3 step 2): a send happens only after
// the store counted it; for an attempt that is no longer live the count is
// refused and nothing is broadcast.
func TestSendNeedsCountedStart(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	att := e.attempts(id)[0]
	tx, err := s.ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkDead(e.ctx, att.ID, store.DeadD3DeadlinePassed); err != nil {
		t.Fatal(err)
	}
	if err := tx.RetryWait(e.ctx, id, store.Expect{State: store.StateInFlight, Phase: store.PhaseAcknowledged}, 5*time.Minute, "D3_DEADLINE_PASSED"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	sent := e.node.broadcastCount()
	live, err := e.st.AttemptByHash(e.ctx, sumOf32(att.TxHash))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.send(e.ctx, live.ID, []byte("bytes of a dead attempt")); err != nil {
		t.Fatal(err)
	}
	if e.node.broadcastCount() != sent {
		t.Fatal("bytes of a dead attempt were broadcast")
	}
}

// TestSignedTransactionMismatchIsRefused (plan 8.2): the worker decodes
// the signer's transaction again and refuses one that is not exactly the
// requested message, sequence, gas and timeout height: nothing is stored or
// sent, the alert is raised and the signer halts with INTEGRITY.
func TestSignedTransactionMismatchIsRefused(t *testing.T) {
	cases := map[string]func(req signer.SignRequest) signer.SignRequest{
		"sequence": func(req signer.SignRequest) signer.SignRequest { req.Sequence++; return req },
		"gas":      func(req signer.SignRequest) signer.SignRequest { req.Gas--; return req },
		"timeout":  func(req signer.SignRequest) signer.SignRequest { req.TimeoutHeight++; return req },
		"message": func(req signer.SignRequest) signer.SignRequest {
			m := withSigner(e2eOffer(), req.Msg.(interface{ GetSigner() []byte }).GetSigner())
			req.Msg = m
			return req
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.signer.tamper = tamper
			id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
			s := e.session(policy.ClassService)
			e.pass(s)
			sg, err := e.st.GetSigner(e.ctx, e.service.info.Address)
			if err != nil || sg.HaltedReason != "INTEGRITY" || !e.logged("SIGNED_TX_MISMATCH") {
				t.Fatalf("signer %+v %v, want halted INTEGRITY with an alert", sg, err)
			}
			if len(e.attempts(id)) != 0 || e.node.broadcastCount() != 0 {
				t.Fatal("a mismatched signed transaction was stored or sent")
			}
		})
	}
}

// e2eOffer is an offer message used as another message than the request.
func e2eOffer() *leasesv1.MsgOfferLease { return offerMsg("o-other", "c1") }

// TestEffectSkipsOtherVersions (plan 8.6 step 3): the typed event search
// for a content id returns the registrations of every version; one of
// another version does not decide the effect, and the first one of the
// intent's version does: here another service's registration of version 2
// makes our version 2 register OBJECT_TAKEN, with nothing signed.
func TestEffectSkipsOtherVersions(t *testing.T) {
	e := newEnv(t)
	other := newKeyFixture("service-2", policy.ClassService)
	e.node.addAccount(other.info.Address, 9)
	e.signer.keys[other.info.Name] = other
	for v := uint64(1); v <= 2; v++ {
		m := withSigner(e.registerMsgVersion("c1", v), other.info.Address)
		setBusinessID(m, fmt.Sprintf("reg-other-%d", v))
		raw, _ := e.signWith(other, m, v-1)
		e.node.commitDirect(raw)
	}
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsgVersion("c1", 2), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.wantState(id, "FAILED", "", "OBJECT_TAKEN")
	if e.signer.callCount() != 2 || e.node.broadcastCount() != 0 {
		t.Fatal("a register of a taken version was signed or sent")
	}
}
