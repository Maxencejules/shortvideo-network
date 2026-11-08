package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/faultpoint"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// session drives the intents of one key signer, or, as the forwarder,
// every forwarded intent, over one store.SignerSession: the connection
// that holds the advisory lock (plan 8.1). It is used by one goroutine.
type session struct {
	w       *Worker
	forward bool
	// key is the key signer's identity; zero for the forwarder.
	key signer.KeyInfo
	ss  *store.SignerSession
	// pace holds, per live attempt, the earliest time of its next
	// rebroadcast and the number of rebroadcasts so far (plan 8.6 step 2:
	// at most once per backoff step).
	pace map[string]paceState
}

// paceState is one attempt's rebroadcast schedule.
type paceState struct {
	next  time.Time
	sends int
}

func newSession(w *Worker, forward bool, key signer.KeyInfo) *session {
	return &session{w: w, forward: forward, key: key, pace: map[string]paceState{}}
}

// name identifies the session in logs: the key name, or "forwarder".
func (s *session) name() string {
	if s.forward {
		return "forwarder"
	}
	return s.key.Name
}

// open takes the session's advisory lock unless it holds it already
// (store.ErrSignerLockHeld when another process holds it).
func (s *session) open(ctx context.Context) error {
	if s.ss != nil {
		return nil
	}
	var (
		ss  *store.SignerSession
		err error
	)
	if s.forward {
		ss, err = s.w.st.LockForwarder(ctx)
	} else {
		ss, err = s.w.st.LockSigner(ctx, s.key.Address)
	}
	if err != nil {
		return err
	}
	s.ss = ss
	return nil
}

// close releases the lock and the connection.
func (s *session) close() {
	if s.ss == nil {
		return
	}
	if err := s.ss.Close(); err != nil {
		s.w.log.Warn("worker session close failed", "session", s.name(), "error", err.Error())
	}
	s.ss = nil
}

// healthy reports whether the session still holds its lock on a live
// connection: a transaction opens and the fence check passes.
func (s *session) healthy(ctx context.Context) bool {
	tx, err := s.ss.Begin(ctx)
	if err != nil {
		return false
	}
	return tx.Rollback() == nil
}

// pass runs one pass of the session. While another process holds the lock
// it does nothing. After a failed pass whose connection lost the lock or
// died, it closes the session; the next pass takes the lock again, so
// nothing is persisted or counted as sent without the lock (plan 8.1).
func (s *session) pass(ctx context.Context) error {
	if err := s.open(ctx); err != nil {
		if errors.Is(err, store.ErrSignerLockHeld) {
			return nil
		}
		return err
	}
	var err error
	if s.forward {
		err = s.forwardPass(ctx)
	} else {
		err = s.signerPass(ctx)
	}
	if err != nil && ctx.Err() == nil && !s.healthy(ctx) {
		s.w.log.Warn("worker session lost its lock or connection", "session", s.name())
		s.close()
	}
	return err
}

// stop reports whether an error ends the pass: the lock or the connection
// is gone, or the context ended. Any other error concerns one intent; the
// pass logs it and goes on.
func stop(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, store.ErrFenceLost) || errors.Is(err, store.ErrTxOpen)
}

// signerPass reconciles the key signer's live attempt, if any, and then,
// with no live attempt left (one live transaction per signer, DR-11-26),
// the signer's due intents in schedule order until one starts an attempt.
func (s *session) signerPass(ctx context.Context) error {
	live, err := s.w.st.LiveAttempt(ctx, s.key.Address)
	switch {
	case err == nil:
		if err := s.reconcileLive(ctx, live); err != nil {
			return err
		}
		if _, err := s.w.st.LiveAttempt(ctx, s.key.Address); !errors.Is(err, store.ErrNotFound) {
			return err
		}
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	ids, err := s.w.st.DueIntents(ctx, s.key.Address, batch)
	if err != nil {
		return err
	}
	for _, id := range ids {
		started, err := s.reconcileRequested(ctx, id)
		if stop(ctx, err) {
			return err
		}
		if err != nil {
			s.w.log.Warn("worker intent pass failed", "session", s.name(), "intent_id", id, "error", err.Error())
			continue
		}
		if started {
			return nil
		}
	}
	return nil
}

// forwardPass reconciles every live forwarded attempt, then the due
// forwarded intents (plan 7.3 P3). Forwarded transactions use the
// providers' own accounts, so several can be live at once.
func (s *session) forwardPass(ctx context.Context) error {
	lives, err := s.w.st.LiveForwardAttempts(ctx, batch)
	if err != nil {
		return err
	}
	for _, live := range lives {
		err := s.reconcileLive(ctx, live)
		if stop(ctx, err) {
			return err
		}
		if err != nil {
			s.w.log.Warn("worker forward pass failed", "intent_id", live.IntentID, "error", err.Error())
		}
	}
	ids, err := s.w.st.DueForwards(ctx, batch)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_, err := s.reconcileRequested(ctx, id)
		if stop(ctx, err) {
			return err
		}
		if err != nil {
			s.w.log.Warn("worker forward pass failed", "intent_id", id, "error", err.Error())
		}
	}
	return nil
}

// reconcileRequested runs plan 8.6 for one validated REQUESTED intent: an
// expired one fails (T4); otherwise the effect check runs first and may
// settle it (T5, T6, T7) or halt the signer (T16); only when it found no
// effect and every guard of T8 holds is the request validated again and a
// new attempt built (or, for a forward, the stored bytes persisted). It
// reports whether an attempt started.
func (s *session) reconcileRequested(ctx context.Context, intentID string) (bool, error) {
	work, err := s.w.st.GetWork(ctx, intentID)
	if err != nil {
		return false, err
	}
	if work.State != store.StateRequested || !work.Validated {
		return false, nil
	}
	if !work.BeforeExpiry {
		return false, ignoreStateChanged(s.w.st.Expire(ctx, intentID))
	}
	atts, err := s.w.st.Attempts(ctx, intentID)
	if err != nil {
		return false, err
	}
	in, err := intentView(work, atts)
	if err != nil {
		return false, err
	}
	msg, err := messageOf(s.w.cfg.Chain.Codec, work)
	if err != nil {
		s.w.alert("STORED_REQUEST_UNREADABLE", "intent_id", intentID, "error", err.Error())
		return false, err
	}
	loc := locatorsOf(work)
	eff, err := s.w.checkEffect(ctx, effectInput{intentID: intentID, msg: msg, pinned: work.SignerAddress, forward: s.forward,
		providerID: loc.providerID, contentID: loc.contentID})
	if err != nil {
		return false, s.integrityOr(ctx, err, intentID)
	}
	if !eff.decided {
		return false, nil
	}
	guard, err := s.guard(ctx)
	if err != nil {
		return false, err
	}
	d, err := outbox.Reconcile(outbox.Pass{Intent: in, Effect: eff.effect, Guard: guard}, s.w.cfg.Outbox)
	if err != nil {
		return false, err
	}
	if len(d.Steps) > 0 {
		return false, s.apply(ctx, decision{work: work, msg: msg, eff: eff}, d.Steps)
	}
	if !d.Start {
		return false, nil
	}
	reason, err := s.w.revalidate(ctx, work, msg)
	if err != nil {
		return false, err
	}
	if reason != "" {
		s.w.log.Info("worker validation refused before signing", "intent_id", intentID, "reason", reason)
		return false, ignoreStateChanged(s.w.st.FailValidation(ctx, intentID, reason))
	}
	if s.forward {
		return s.persistForward(ctx, work)
	}
	return s.startAttempt(ctx, work, atts, msg)
}

// guard returns the facts of the T8 guards this session can read: an open
// stall episode and a halted signer. The intent is due (listed by its
// schedule), its signer is idle (no live attempt was found first) and the
// lock is held; PersistAttempt checks every guard again under the row
// locks.
func (s *session) guard(ctx context.Context) (outbox.Guard, error) {
	g := outbox.Guard{Due: true, SignerIdle: true, LockHeld: true}
	_, err := s.w.st.OpenStallEpisode(ctx)
	switch {
	case err == nil:
		g.Stalled = true
	case !errors.Is(err, store.ErrNotFound):
		return outbox.Guard{}, err
	}
	if !s.forward {
		sg, err := s.w.st.GetSigner(ctx, s.key.Address)
		if err != nil {
			return outbox.Guard{}, err
		}
		g.SignerHalted = sg.HaltedReason != ""
	}
	return g, nil
}

// persistForward is T8 of a forwarded intent: the stored bytes become
// attempt 1 and are sent.
func (s *session) persistForward(ctx context.Context, work store.Work) (bool, error) {
	res, err := s.ss.PersistForward(ctx, work.ID)
	if err != nil {
		return false, err
	}
	if res.Refusal != store.RefusedNone {
		s.w.log.Info("worker forward not persisted", "intent_id", work.ID, "refusal", string(res.Refusal))
		return false, nil
	}
	faultpoint.Hit(faultpoint.AfterPersistCommit)
	return true, s.send(ctx, res.AttemptID, work.Request)
}

// send is plan 8.3 steps 2 to 4 for one send of an attempt's stored bytes:
// count the send as started (committed), broadcast, record the outcome. If
// the count is refused (the attempt is no longer live, its intent is
// awaiting its deadline or expired) nothing is sent. A failure to record
// leaves the send unrecorded, which counts as UNKNOWN.
func (s *session) send(ctx context.Context, attemptID string, raw []byte) error {
	ok, err := s.ss.BeginSend(ctx, attemptID)
	if err != nil || !ok {
		return err
	}
	faultpoint.Hit(faultpoint.AfterSendsStarted)
	obs, err := s.w.cfg.Chain.Broadcast(ctx, raw)
	if err != nil {
		obs = chain.SendObservation{Outcome: chain.SendUnknown, Detail: "broadcast refused before sending"}
	}
	faultpoint.Hit(faultpoint.AfterBroadcastBeforeRecord)
	p := s.pace[attemptID]
	p.sends++
	p.next = time.Now().Add(s.w.paceStep(p.sends))
	s.pace[attemptID] = p
	if _, err := s.ss.RecordSendOutcome(ctx, attemptID, storeOutcome(obs)); err != nil {
		return err
	}
	s.w.log.Info("worker send", "session", s.name(), "attempt_id", attemptID, "outcome", obs.Outcome.String(),
		"codespace", obs.Codespace, "code", obs.Code)
	return nil
}

// rebroadcast sends a live attempt's stored bytes again, never re-signed
// bytes, at most once per backoff step (plan 8.6 step 2).
func (s *session) rebroadcast(ctx context.Context, att store.AttemptTx) error {
	if p, ok := s.pace[att.ID]; ok && time.Now().Before(p.next) {
		return nil
	}
	if att.BytesCleared || len(att.TxBytes) == 0 {
		return fmt.Errorf("worker: live attempt %s has no stored bytes", att.ID)
	}
	return s.send(ctx, att.ID, att.TxBytes)
}

// ignoreStateChanged drops store.ErrStateChanged: another writer changed
// the row first, which the next pass reads.
func ignoreStateChanged(err error) error {
	if errors.Is(err, store.ErrStateChanged) {
		return nil
	}
	return err
}
