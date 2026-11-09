package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// effectInput is one intent's effect check (plan 8.6 step 3).
type effectInput struct {
	intentID string
	// msg is the intent's message with its pinned signer and business id.
	msg sdk.Msg
	// pinned is the account the intent's transactions are signed by: the
	// service or broadcaster key, or for a forward the provider account.
	pinned  []byte
	forward bool
	// providerID and contentID locate the held lease of a renew or end
	// when the request named them; otherwise Query/Deadlines is paged.
	providerID string
	contentID  string
}

// effectResult is the effect check's verdict for the state machine, with
// the transaction behind it.
type effectResult struct {
	// decided is false when the check cannot decide yet (an object seen
	// changed fewer than L blocks ago, a search above its page budget): the
	// caller changes nothing and tries again at a later pass.
	decided bool
	effect  outbox.Effect
	// tx is the committing transaction of EffectCommitted (the first
	// commitment) and of EffectSignerConflict; signer is its signer.
	tx     chain.Committed
	signer []byte
}

// undecided is the result of a check that cannot decide yet.
var undecided = effectResult{}

// checkEffect runs the effect check of plan 8.6 step 3 for one intent:
//
//  1. it reads the object (objectGone): the content version of a register
//     or withdrawal (Query/Content), the open offer of an accept
//     (Query/Deadlines within the page budget), the held lease of a renew or
//     end (Query/ActiveLeases with the request's locators, or
//     Query/Deadlines);
//  2. it searches the committed transactions whose typed event of the
//     intent's kind names the object (tx_search, all results within the page
//     budget or none) and compares each, first commitment first, with the
//     intent's message under the signer rule (chain.CompareEffect): the
//     first that is not about another version or term decides COMMITTED
//     (with own_transaction and whether it is a stored attempt marked DEAD),
//     OBJECT_TAKEN, OBJECT_STATE_CHANGED or a signer conflict. For a
//     forward, a transaction of the provider's own account that the
//     broadcaster did not store is the provider committing its own consent,
//     not a conflict, and is compared by its business fields;
//  3. with no deciding transaction, an object that no longer allows the
//     effect is OBJECT_STATE_CHANGED (an offer or lease ended by an expiry
//     at begin-block, a content record no transaction carries), but only
//     once the read height is at least L blocks above the height at which
//     this worker first saw the object changed: every transaction committed
//     up to that first height, the one that changed the object included, is
//     then indexed, so the search of this pass would have found it. Until
//     then the check waits. Everything else is no effect: a commitment the
//     search misses because it is not indexed yet makes the next attempt
//     fail on chain, which a later check settles.
func (w *Worker) checkEffect(ctx context.Context, in effectInput) (effectResult, error) {
	gone, objHeight, decided, err := w.objectGone(ctx, in)
	if err != nil || !decided {
		return undecided, err
	}
	cands, err := w.cfg.Chain.EffectCandidates(ctx, in.msg, w.cfg.Numbers.DeadlinePages)
	if errors.Is(err, chain.ErrSearchIncomplete) {
		w.alert("EFFECT_SEARCH_INCOMPLETE", "intent_id", in.intentID)
		return undecided, nil
	}
	if err != nil {
		return undecided, err
	}
	for _, c := range cands {
		owner, dead, err := w.hashOwner(ctx, c.Hash, in.intentID)
		if err != nil {
			return undecided, err
		}
		eff, err := w.cfg.Chain.CompareEffect(in.msg, in.pinned, c, owner)
		if err != nil {
			return undecided, err
		}
		if in.forward && eff.Verdict == chain.EffectSignerConflict {
			// The provider's own account committed the same consent
			// itself: compare it as a transaction of another intent.
			if eff, err = w.cfg.Chain.CompareEffect(in.msg, in.pinned, c, chain.HashOtherIntent); err != nil {
				return undecided, err
			}
		}
		res := effectResult{decided: true, tx: c, signer: eff.Signer}
		switch eff.Verdict {
		case chain.EffectUnrelated:
			continue
		case chain.EffectCommitted:
			res.effect = outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: c.Hash, Own: eff.OwnTransaction, DeadAttempt: dead}
		case chain.EffectSignerConflict:
			res.effect = outbox.Effect{Verdict: outbox.EffectSignerConflict}
		case chain.EffectObjectTaken:
			res.effect, res.tx = outbox.Effect{Verdict: outbox.EffectTaken}, chain.Committed{}
		case chain.EffectStateChanged:
			res.effect, res.tx = outbox.Effect{Verdict: outbox.EffectStateChanged}, chain.Committed{}
		default:
			return undecided, fmt.Errorf("worker: effect verdict %v unknown", eff.Verdict)
		}
		w.forgetGone(in.intentID)
		return res, nil
	}
	if gone {
		if first := w.goneSince(in.intentID, objHeight); objHeight < first+int64(w.cfg.Outbox.IndexingMargin) {
			return undecided, nil
		}
		w.forgetGone(in.intentID)
		return effectResult{decided: true, effect: outbox.Effect{Verdict: outbox.EffectStateChanged}}, nil
	}
	w.forgetGone(in.intentID)
	return effectResult{decided: true, effect: outbox.Effect{Verdict: outbox.EffectNone}}, nil
}

// goneSince returns the read height at which this worker first saw intent
// id's object no longer allowing its effect, recording height when it is
// the first. The record lives in memory: after a restart the wait starts
// again.
func (w *Worker) goneSince(id string, height int64) int64 {
	w.goneMu.Lock()
	defer w.goneMu.Unlock()
	if first, ok := w.gone[id]; ok {
		return first
	}
	w.gone[id] = height
	return height
}

// forgetGone drops the record of goneSince once the check decided.
func (w *Worker) forgetGone(id string) {
	w.goneMu.Lock()
	defer w.goneMu.Unlock()
	delete(w.gone, id)
}

// hashOwner says whose stored attempt hash is (chain.HashOwner) and
// whether that attempt is marked DEAD.
func (w *Worker) hashOwner(ctx context.Context, hash [32]byte, intentID string) (chain.HashOwner, bool, error) {
	a, err := w.st.AttemptByHash(ctx, hash)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return chain.HashForeign, false, nil
	case err != nil:
		return 0, false, err
	case a.IntentID == intentID:
		return chain.HashThisIntent, a.Phase == store.AttemptDead, nil
	}
	return chain.HashOtherIntent, a.Phase == store.AttemptDead, nil
}

// objectGone reads the intent's object and reports whether it no longer
// allows the intended effect, and the height of the read: an accept's offer
// is no longer open, a renew's or end's lease is no longer held, a
// register's content version has a record or tombstone, a withdrawal's
// content version has a tombstone. decided is false when the read could not
// finish within the page budget. An offer reads nothing (height 0).
func (w *Worker) objectGone(ctx context.Context, in effectInput) (gone bool, height int64, decided bool, err error) {
	switch m := in.msg.(type) {
	case *contentv1.MsgRegisterContent:
		_, found, h, err := w.cfg.Chain.Content(ctx, m.GetContentId(), m.GetContentVersion(), 0)
		return found, h, err == nil, err
	case *contentv1.MsgWithdrawContent:
		resp, found, h, err := w.cfg.Chain.Content(ctx, m.GetContentId(), m.GetContentVersion(), 0)
		return found && resp.GetTombstone() != nil, h, err == nil, err
	case *leasesv1.MsgAcceptLease:
		offer, _, h, complete, err := w.deadlineEntry(ctx, m.GetOfferId(), "")
		return offer == nil, h, complete, err
	case *leasesv1.MsgRenewLease:
		lease, h, complete, err := w.heldLease(ctx, m.GetLeaseId(), in.providerID, in.contentID)
		return lease == nil, h, complete, err
	case *leasesv1.MsgEndLease:
		lease, h, complete, err := w.heldLease(ctx, m.GetLeaseId(), in.providerID, in.contentID)
		return lease == nil, h, complete, err
	}
	return false, 0, true, nil
}

// heldLease returns the held lease leaseID: through Query/ActiveLeases when
// the request named its (provider, content) pair, otherwise by paging
// Query/Deadlines. A lease of another id held by the pair is not it.
func (w *Worker) heldLease(ctx context.Context, leaseID, providerID, contentID string) (*leasesv1.Lease, int64, bool, error) {
	if providerID != "" && contentID != "" {
		lease, found, h, err := w.cfg.Chain.HeldLease(ctx, providerID, contentID, 0)
		if err != nil {
			return nil, 0, false, err
		}
		if !found || lease.GetLeaseId() != leaseID {
			return nil, h, true, nil
		}
		return lease, h, true, nil
	}
	_, lease, h, complete, err := w.deadlineEntry(ctx, "", leaseID)
	return lease, h, complete, err
}

// deadlineEntry pages Query/Deadlines (every open offer and held lease, by
// deadline) for the open offer offerID or the held lease leaseID, reading
// at most the configured page budget, every page at least at the first
// page's height. complete is false when the budget ran out first.
func (w *Worker) deadlineEntry(ctx context.Context, offerID, leaseID string) (*leasesv1.Offer, *leasesv1.Lease, int64, bool, error) {
	var (
		pageKey []byte
		first   int64
	)
	for page := 0; page < w.cfg.Numbers.DeadlinePages; page++ {
		resp, h, err := w.cfg.Chain.Deadlines(ctx, genesis.UnixSecondsMax, pageKey, genesis.MaxPageLimit, first)
		if err != nil {
			return nil, nil, 0, false, err
		}
		if page == 0 {
			first = h
		}
		for _, o := range resp.GetOffers() {
			if offerID != "" && o.GetOfferId() == offerID {
				return o, nil, first, true, nil
			}
		}
		for _, l := range resp.GetLeases() {
			if leaseID != "" && l.GetLeaseId() == leaseID {
				return nil, l, first, true, nil
			}
		}
		pageKey = resp.GetPage().GetNextPageKey()
		if len(pageKey) == 0 {
			return nil, nil, first, true, nil
		}
	}
	return nil, nil, first, false, nil
}

// conflictSequence returns the sequence the pinned signer used in a
// committed transaction (a signer conflict), or false when it does not
// sign it.
func conflictSequence(c *chain.Codec, tx chain.Committed, pinned []byte) (uint64, bool) {
	decoded, err := c.Decode(tx.Raw)
	if err != nil {
		return 0, false
	}
	for _, s := range decoded.Signers {
		if bytes.Equal(s.Address, pinned) {
			return s.Sequence, true
		}
	}
	return 0, false
}
