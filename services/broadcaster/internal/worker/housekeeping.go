package worker

import (
	"context"
	"errors"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// housekeepingPass runs, each over at most one batch: expiry of REQUESTED
// intents whose expires_at passed on the database clock (T4), the worker's
// full validation of SQL-path intents (plan 6.3, T2), and the EFFECTIVE
// promotion of the owner rows of COMMITTED registrations (plan 5.7). Each
// intent's failure is logged and the pass goes on.
func (w *Worker) housekeepingPass(ctx context.Context) error {
	var errs []error
	expired, err := w.st.ExpiredRequested(ctx, batch)
	errs = append(errs, err)
	for _, id := range expired {
		if err := ignoreStateChanged(w.st.Expire(ctx, id)); err != nil {
			w.log.Warn("worker expiry failed", "intent_id", id, "error", err.Error())
		}
	}
	pending, err := w.st.UnvalidatedIntents(ctx, batch)
	errs = append(errs, err)
	for _, u := range pending {
		if err := w.validateOne(ctx, u); err != nil {
			w.log.Warn("worker validation failed", "intent_id", u.ID, "error", err.Error())
		}
	}
	owners, err := w.st.PendingOwnerConfirmations(ctx, batch)
	errs = append(errs, err)
	for _, id := range owners {
		if err := w.confirmOwner(ctx, id); err != nil {
			w.log.Warn("worker owner confirmation failed", "intent_id", id, "error", err.Error())
		}
	}
	return errors.Join(errs...)
}

// validateOne validates one SQL-path intent and stores the result: its
// validation (business id, owner row, forwarded fields), or FAILED with
// the T2 reason. A content id another owner row holds and forwarded bytes
// another intent tracks fail it as INVALID. A read that failed leaves it
// for the next pass.
func (w *Worker) validateOne(ctx context.Context, u store.Unvalidated) error {
	work, err := w.st.GetWork(ctx, u.ID)
	if err != nil {
		return err
	}
	if work.State != store.StateRequested || work.Validated || !work.BeforeExpiry {
		return nil
	}
	v, reason, err := w.validateSQL(ctx, u, work)
	if err != nil {
		return err
	}
	if reason == "" {
		err = w.st.MarkValidated(ctx, u.ID, v)
		if errors.Is(err, store.ErrContentUnavailable) || errors.Is(err, store.ErrTxTracked) {
			reason = reasonInvalid
		} else {
			return ignoreStateChanged(err)
		}
	}
	w.log.Info("worker validation refused", "intent_id", u.ID, "reason", reason)
	return ignoreStateChanged(w.st.FailValidation(ctx, u.ID, reason))
}

// confirmOwner makes the PENDING owner row of a COMMITTED registration
// EFFECTIVE once Query/Content shows a live record of the content version
// whose submitting host is the row's signer (store.ConfirmContentOwner
// checks the host and the committing signer). A record the node does not
// show yet, or a tombstone, leaves the row PENDING; a record hosted by
// another account is an alert and leaves it PENDING.
func (w *Worker) confirmOwner(ctx context.Context, intentID string) error {
	work, err := w.st.GetWork(ctx, intentID)
	if err != nil {
		return err
	}
	if work.Object == nil || work.Object.Type != store.ObjectContent {
		return nil
	}
	resp, found, _, err := w.cfg.Chain.Content(ctx, work.Object.ID, work.Object.Version, 0)
	if err != nil || !found || resp.GetContent() == nil || resp.GetTombstone() != nil {
		return err
	}
	err = w.st.ConfirmContentOwner(ctx, intentID, resp.GetContent().GetSubmittingHost())
	if errors.Is(err, store.ErrInvalid) {
		w.alert("CONTENT_HOST_MISMATCH", "intent_id", intentID)
		return nil
	}
	return ignoreStateChanged(err)
}
