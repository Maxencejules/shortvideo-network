package worker

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// decision is what one reconciliation pass read: the intent, its message,
// its live attempt with what /tx said about it, the acc_seq search hit, and
// the effect check.
type decision struct {
	work store.Work
	msg  sdk.Msg
	// live, result and committed are set for an IN_FLIGHT intent; result
	// and committed only when /tx found the attempt's hash.
	live      *store.AttemptTx
	result    *chain.CommitObservation
	committed chain.Committed
	// hit is the transaction the acc_seq search found at the live
	// attempt's sequence.
	hit *chain.Committed
	eff effectResult
}

// maxRounds bounds the observations one pass fetches for one live attempt:
// the account, the acc_seq search and the effect check, then the decision.
const maxRounds = 4

// reconcileLive runs plan 8.6 for one live attempt:
//
//   - a started send without a recorded outcome (a crash between counting
//     and recording, plan 8.3) is recorded as UNKNOWN first, so the attempt
//     can never die by (d2) through it;
//   - the stored bytes are decoded again (a failure is a fatal integrity
//     error: the signer halts, nothing is repaired);
//   - the node's latest height is read before /tx of the attempt's hash,
//     and a found result is classified (hash, signer, sequence and message
//     checked; acc_seq, never the code, says whether the sequence was
//     consumed);
//   - outbox.Reconcile decides; when it needs the account read at a height
//     of at least timeout_height + L, the acc_seq search or the effect
//     check, they are fetched and it decides again;
//   - the decision's steps are stored in one transaction, then any signer
//     halt, then a rebroadcast of the same bytes when the attempt is still
//     live and before expiry.
func (s *session) reconcileLive(ctx context.Context, att store.AttemptTx) error {
	if att.SendsStarted > att.SendsRecorded {
		for i := att.SendsRecorded; i < att.SendsStarted; i++ {
			if _, err := s.ss.RecordSendOutcome(ctx, att.ID, store.SendOutcome{Class: store.SendUnknown,
				Log: "no outcome was recorded for a started send"}); err != nil {
				return err
			}
		}
		s.w.log.Info("worker recorded unrecorded sends as unknown", "attempt_id", att.ID,
			"sends", att.SendsStarted-att.SendsRecorded)
		return nil
	}
	work, err := s.w.st.GetWork(ctx, att.IntentID)
	if err != nil {
		return err
	}
	atts, err := s.w.st.Attempts(ctx, att.IntentID)
	if err != nil {
		return err
	}
	decoded, err := s.w.cfg.Chain.Decode(att.TxBytes)
	if err != nil {
		return s.integrity(ctx, "STORED_ATTEMPT_UNDECODABLE", att.IntentID, err)
	}
	own, err := decoded.Own()
	if err != nil {
		return s.integrity(ctx, "STORED_ATTEMPT_SHAPE", att.IntentID, err)
	}
	in, err := intentView(work, atts)
	if err != nil {
		return err
	}
	a, err := attemptView(att.Attempt)
	if err != nil {
		return err
	}
	st, err := s.w.cfg.Chain.Status(ctx)
	if err != nil {
		return err
	}
	dc := decision{work: work, msg: own.Msg.Msg, live: &att}
	view := outbox.ChainView{Height: uint64(st.Height)}
	com, found, err := s.w.cfg.Chain.CommittedTx(ctx, a.Hash)
	if err != nil {
		return s.integrityOr(ctx, err, att.IntentID)
	}
	if found {
		obs, err := s.w.cfg.Chain.ClassifyCommitted(com, chain.AttemptRef{Hash: a.Hash, Signer: att.SignerAddress,
			Sequence: att.Sequence, Msg: dc.msg})
		if err != nil {
			return s.integrityOr(ctx, err, att.IntentID)
		}
		view.Tx, dc.result, dc.committed = txResult(obs), &obs, com
	}
	pass := outbox.Pass{Intent: in, Live: &a, Chain: view, Guard: outbox.Guard{Expired: !work.BeforeExpiry}}
	for round := 0; round < maxRounds; round++ {
		d, err := outbox.Reconcile(pass, s.w.cfg.Outbox)
		if err != nil {
			return err
		}
		switch {
		case d.Need&outbox.NeedEffect != 0:
			loc := locatorsOf(work)
			eff, err := s.w.checkEffect(ctx, effectInput{intentID: work.ID, msg: dc.msg, pinned: att.SignerAddress,
				forward: s.forward, providerID: loc.providerID, contentID: loc.contentID})
			if err != nil {
				return s.integrityOr(ctx, err, work.ID)
			}
			if !eff.decided {
				return nil
			}
			pass.Effect, dc.eff = eff.effect, eff
			continue
		case d.Need&outbox.NeedAccount != 0 && pass.Chain.Account == nil:
			acct, err := s.w.cfg.Chain.Account(ctx, att.SignerAddress, int64(a.TimeoutHeight+s.w.cfg.Outbox.IndexingMargin))
			var stale *chain.StaleError
			if errors.As(err, &stale) {
				return s.applyDecision(ctx, dc, d)
			}
			if err != nil {
				return err
			}
			pass.Chain.Account = &outbox.AccountRead{Sequence: acct.Sequence, Height: uint64(acct.Height)}
			continue
		case d.Need&outbox.NeedSearch != 0 && !pass.Chain.Searched:
			hit, found, err := s.w.cfg.Chain.SequenceUser(ctx, att.SignerAddress, att.Sequence)
			if err != nil {
				return s.integrityOr(ctx, err, work.ID)
			}
			pass.Chain.Searched = true
			if found {
				owner, err := s.searchOwner(ctx, att, hit.Hash)
				if err != nil {
					return err
				}
				pass.Chain.Search, dc.hit = []outbox.Hit{{Hash: hit.Hash, Owner: owner}}, &hit
			}
			continue
		}
		return s.applyDecision(ctx, dc, d)
	}
	return errors.New("worker: reconciliation did not settle within its rounds")
}

// searchOwner says whose transaction used the live attempt's sequence: a
// stored attempt (marked DEAD or not), a foreign transaction an operator
// acknowledged with signer-clear (store.Conflict CLEARED), or a foreign one
// nobody acknowledged. For a forward the account is the provider's own, so
// a transaction the broadcaster did not store is the provider's own use of
// its sequence, never a conflict.
func (s *session) searchOwner(ctx context.Context, att store.AttemptTx, hash [32]byte) (outbox.HashOwner, error) {
	stored, err := s.w.st.AttemptByHash(ctx, hash)
	switch {
	case err == nil && stored.Phase == store.AttemptDead:
		return outbox.OwnerOursDead, nil
	case err == nil:
		return outbox.OwnerOurs, nil
	case !errors.Is(err, store.ErrNotFound):
		return 0, err
	case s.forward:
		return outbox.OwnerForeignCleared, nil
	}
	status, err := s.w.st.Conflict(ctx, att.SignerAddress, att.Sequence, hash)
	if err != nil {
		return 0, err
	}
	if status == store.ConflictCleared {
		return outbox.OwnerForeignCleared, nil
	}
	return outbox.OwnerForeign, nil
}

// applyDecision stores the decision's steps, then rebroadcasts the live
// attempt's bytes when the decision allows it.
func (s *session) applyDecision(ctx context.Context, dc decision, d outbox.Decision) error {
	if err := s.apply(ctx, dc, d.Steps); err != nil {
		return err
	}
	if d.Rebroadcast && dc.live != nil {
		return s.rebroadcast(ctx, *dc.live)
	}
	return nil
}

// apply stores the steps of one decision in one SignerTx, in order: the
// attempt's resolution or death first, then the intent transition it
// causes (COMMITTED with its commitment row, FAILED, RETRY_WAIT with a new
// business attempt, or AWAITING_DEADLINE). The database checks at commit
// that the intent and its attempts agree. After the commit it halts the
// signer and raises the alerts the steps name, and for a COMMITTED
// registration it confirms the content owner row.
func (s *session) apply(ctx context.Context, dc decision, steps []outbox.Step) error {
	var (
		commitment *store.Commitment
		write      bool
	)
	for _, step := range steps {
		switch step.ID {
		case outbox.T11, outbox.T12, outbox.T5:
			c, err := s.commitmentOf(ctx, dc, step.ID)
			if err != nil {
				return s.integrityOr(ctx, err, dc.work.ID)
			}
			commitment = &c
		}
		if step.Changed() || (step.HasAttempt && !step.Attempt.Phase.Live()) {
			write = true
		}
	}
	if write {
		if err := s.store(ctx, dc, steps, commitment); err != nil {
			if errors.Is(err, store.ErrEvidence) || errors.Is(err, store.ErrLiveAttempt) {
				return s.integrity(ctx, "STORE_REFUSED_DECISION", dc.work.ID, err)
			}
			return err
		}
	}
	var errs []error
	for _, step := range steps {
		if step.Alert != 0 {
			s.w.alert(step.Alert.String(), "intent_id", dc.work.ID, "transition", step.ID.String())
		}
		errs = append(errs, s.halt(ctx, step.Halt, dc))
		if step.After.State == outbox.StateCommitted && dc.work.Kind == store.KindRegisterContent {
			errs = append(errs, s.w.confirmOwner(ctx, dc.work.ID))
		}
	}
	if dc.live != nil {
		for _, step := range steps {
			if step.HasAttempt && !step.Attempt.Phase.Live() {
				delete(s.pace, dc.live.ID)
			}
		}
	}
	return errors.Join(errs...)
}

// store writes the steps in one SignerTx.
func (s *session) store(ctx context.Context, dc decision, steps []outbox.Step, commitment *store.Commitment) error {
	tx, err := s.ss.Begin(ctx)
	if err != nil {
		return err
	}
	if err := s.storeSteps(ctx, tx, dc, steps, commitment); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (s *session) storeSteps(ctx context.Context, tx *store.SignerTx, dc decision, steps []outbox.Step, commitment *store.Commitment) error {
	from := store.Expect{State: dc.work.State, Phase: dc.work.Phase}
	id := dc.work.ID
	for _, step := range steps {
		if step.HasAttempt && dc.live != nil {
			switch step.Attempt.Phase {
			case outbox.AttemptCommittedOK, outbox.AttemptCommittedIdempotent, outbox.AttemptExecutionFailed,
				outbox.AttemptIncludedAnteFailed:
				if dc.result == nil {
					return errors.New("worker: a resolved attempt without its committed result")
				}
				if err := tx.ResolveCommitted(ctx, dc.live.ID, committedResult(*dc.result)); err != nil {
					return err
				}
			case outbox.AttemptDead:
				if err := tx.MarkDead(ctx, dc.live.ID, store.DeadReason(step.Attempt.Dead.String())); err != nil {
					return err
				}
			}
		}
		var err error
		switch step.ID {
		case outbox.T11, outbox.T12, outbox.T5:
			if commitment == nil {
				return errors.New("worker: COMMITTED without its commitment")
			}
			err = tx.CommitIntent(ctx, id, from, *commitment, step.After.Own)
		case outbox.T6:
			err = tx.FailIntent(ctx, id, from, outbox.ReasonObjectTaken.String(), nil)
		case outbox.T7:
			err = tx.FailIntent(ctx, id, from, outbox.ReasonObjectStateChanged.String(), nil)
		case outbox.T13, outbox.T14:
			err = s.storeRetry(ctx, tx, id, from, step)
		case outbox.T15:
			err = tx.AwaitDeadline(ctx, id, from)
		case outbox.T4:
			err = errors.New("worker: expiry of a REQUESTED intent is stored by the store's Expire")
		}
		if err != nil {
			return err
		}
		if step.Changed() {
			from = expectOf(step.After)
		}
	}
	return nil
}

// storeRetry stores the end of T13 or T14: FAILED with the reason and code
// the retry rules chose, or REQUESTED/RETRY_WAIT after a full-jitter delay,
// with a new business attempt after a committed REPLAY. A forwarded
// intent's bytes are the provider's and cannot be signed again, so a retry
// fails it with PROVIDER_MUST_RESIGN (plan 7.3 P3).
func (s *session) storeRetry(ctx context.Context, tx *store.SignerTx, id string, from store.Expect, step outbox.Step) error {
	if step.After.State == outbox.StateFailed {
		var code *store.ChainCode
		if step.After.Code != (outbox.Code{}) {
			code = &store.ChainCode{Code: step.After.Code.Number, Codespace: step.After.Code.Codespace}
		}
		return tx.FailIntent(ctx, id, from, step.After.Reason.String(), code)
	}
	if s.forward {
		return tx.FailIntent(ctx, id, from, reasonProviderMustResign, nil)
	}
	reason := step.Attempt.Phase.String()
	if step.Attempt.Phase == outbox.AttemptDead {
		reason = step.Attempt.Dead.String()
	}
	if err := tx.RetryWait(ctx, id, from, s.w.backoff(step.After.Attempts), reason); err != nil {
		return err
	}
	if step.NewBusinessAttempt {
		if _, err := tx.OpenBusinessAttempt(ctx, id, store.Expect{State: store.StateRequested, Phase: store.PhaseRetryWait},
			outbox.ReasonBusinessIDTaken.String()); err != nil {
			return err
		}
	}
	return nil
}

// reasonProviderMustResign fails a forwarded intent whose bytes can no
// longer commit (plan 7.3 P3): only the provider can sign new ones.
const reasonProviderMustResign = "PROVIDER_MUST_RESIGN"

// expectOf is the store's (state, phase) of a state machine intent.
func expectOf(in outbox.Intent) store.Expect {
	e := store.Expect{State: store.State(in.State.String())}
	if in.Phase != 0 {
		e.Phase = store.Phase(in.Phase.String())
	}
	return e
}

// commitmentOf builds the commitment evidence of plan 9.5 for a T11, T12 or
// T5 step: the block identity read from /commit at the committing height,
// the typed events with their positions, the committing signer. T11 is the
// live attempt's own transaction. T12 and T5 are the first commitment the
// effect check found; T12 records the live attempt's idempotent replay as
// the transaction its result was read from.
func (s *session) commitmentOf(ctx context.Context, dc decision, id outbox.TransitionID) (store.Commitment, error) {
	tx, signerAddr := dc.eff.tx, dc.eff.signer
	if id == outbox.T11 {
		if dc.live == nil || dc.result == nil {
			return store.Commitment{}, errors.New("worker: T11 without a committed attempt")
		}
		tx, signerAddr = dc.committed, dc.live.SignerAddress
	}
	if tx.Height < 1 || len(signerAddr) != 20 {
		return store.Commitment{}, errors.New("worker: commitment without its transaction or signer")
	}
	block, err := s.w.cfg.Chain.Commit(ctx, tx.Height)
	if err != nil {
		return store.Commitment{}, err
	}
	if len(block.Hash) != 32 {
		return store.Commitment{}, &chain.IntegrityError{Reason: fmt.Sprintf("block hash of %d bytes at height %d", len(block.Hash), tx.Height)}
	}
	events, err := eventsJSON(tx.Events)
	if err != nil {
		return store.Commitment{}, err
	}
	c := store.Commitment{Height: uint64(tx.Height), TxHash: tx.Hash, FirstCommitTxHash: tx.Hash, TxIndex: tx.Index,
		Codespace: tx.Codespace, GasWanted: uint64(max(tx.GasWanted, 0)), GasUsed: uint64(max(tx.GasUsed, 0)), Events: events,
		CommittingSigner: signerAddr}
	copy(c.BlockHash[:], block.Hash)
	if id == outbox.T12 {
		c.TxHash = dc.result.Hash
	}
	return c, nil
}

// halt stops the key signer for a step's halt reason: a signer conflict
// with the foreign transaction and the sequence it used, an integrity
// alert, or an UNAUTHORIZED refusal. A conflict an operator already cleared
// halts nothing (store.ErrConflictCleared); the intent stays as it is and
// the alert says so. The forwarder halts nothing: a provider's own use of
// its sequence is no conflict.
func (s *session) halt(ctx context.Context, h outbox.HaltReason, dc decision) error {
	if h == 0 {
		return nil
	}
	if s.forward {
		s.w.alert("FORWARD_HOLD", "intent_id", dc.work.ID, "halt", h.String())
		return nil
	}
	switch h {
	case outbox.HaltSignerConflict:
		hash, seq, ok := s.conflictOf(dc)
		if !ok {
			return s.halted(ctx, "INTEGRITY", dc.work.ID)
		}
		err := s.ss.HaltForConflict(ctx, seq, hash)
		if errors.Is(err, store.ErrConflictCleared) {
			s.w.alert("SIGNER_CONFLICT_CLEARED", "intent_id", dc.work.ID, "sequence", seq)
			return nil
		}
		if err == nil {
			s.w.alert("SIGNER_HALTED", "signer", s.key.Name, "reason", "SIGNER_CONFLICT", "sequence", seq)
		}
		return err
	case outbox.HaltIntegrity:
		return s.halted(ctx, "INTEGRITY", dc.work.ID)
	case outbox.HaltUnauthorized:
		return s.halted(ctx, "UNAUTHORIZED", dc.work.ID)
	}
	return fmt.Errorf("worker: halt reason %v unknown", h)
}

// conflictOf returns the foreign transaction of a signer conflict and the
// sequence it used: the acc_seq hit at the live attempt's sequence, or the
// committing transaction the effect check found, signed by our key.
func (s *session) conflictOf(dc decision) ([32]byte, uint64, bool) {
	if dc.hit != nil && dc.live != nil {
		return dc.hit.Hash, dc.live.Sequence, true
	}
	if dc.eff.effect.Verdict == outbox.EffectSignerConflict {
		seq, ok := conflictSequence(s.w.cfg.Chain.Codec, dc.eff.tx, s.key.Address)
		return dc.eff.tx.Hash, seq, ok
	}
	return [32]byte{}, 0, false
}

// halted halts the key signer with reason and raises the alert.
func (s *session) halted(ctx context.Context, reason, intentID string) error {
	s.w.alert("SIGNER_HALTED", "signer", s.key.Name, "reason", reason, "intent_id", intentID)
	return s.ss.Halt(ctx, reason)
}

// integrity handles an answer that contradicts the outbox (plan 8.4, 8.5):
// the alert, and for a key signer the halt. Nothing is rewritten.
func (s *session) integrity(ctx context.Context, code, intentID string, cause error) error {
	s.w.alert(code, "intent_id", intentID, "error", cause.Error())
	if s.forward {
		return nil
	}
	return s.halted(ctx, "INTEGRITY", intentID)
}

// integrityOr routes a chain.IntegrityError to integrity and returns any
// other error as it is.
func (s *session) integrityOr(ctx context.Context, err error, intentID string) error {
	var ie *chain.IntegrityError
	if errors.As(err, &ie) {
		return s.integrity(ctx, "CHAIN_INTEGRITY", intentID, err)
	}
	return err
}

// backoff is the full-jitter retry delay before the next attempt of an
// intent that has stored attempts attempts (plan 8.7): uniform in [0,
// min(cap, base * 2^(attempts-1))].
func (w *Worker) backoff(attempts int) time.Duration {
	ceiling := w.paceStep(attempts)
	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}

// paceStep is base * 2^(n-1), at most the cap: the backoff ceiling after n
// tries, and the least time between two rebroadcasts of one attempt after
// its n-th send.
func (w *Worker) paceStep(n int) time.Duration {
	step := w.cfg.Numbers.BackoffBase
	for i := 1; i < n && step < w.cfg.Numbers.BackoffCap; i++ {
		step *= 2
	}
	return min(step, w.cfg.Numbers.BackoffCap)
}
