package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// observer is the chain observer of plan section 10. Every pass reads the
// node's /status and the database clock:
//
//   - a status read records chain_health (latest height, block time,
//     seen_at on the database clock), which keeps the observer fresh for
//     the API's overlay;
//   - a stall episode opens when the height has not advanced for T of
//     database clock (HEIGHT_STALLED), or when every read for T failed: the
//     node unreachable (NODE_UNREACHABLE) or catching up (CATCHING_UP). A
//     failed read records nothing, so the observer goes stale and the API
//     shows OBSERVER_UNAVAILABLE, never a stall claim, until a read
//     succeeds again (m4);
//   - the open episode closes when a read shows a height above the height
//     the episode opened at.
//
// While an episode is open no attempt is persisted (the store refuses
// it); live attempts are still reconciled and rebroadcast before expiry.
type observer struct {
	w  *Worker
	mu sync.Mutex
	// height is the latest height read and since is the database clock
	// when it was first read.
	height int64
	since  time.Time
	// failing is the stall reason of the current run of failed reads, and
	// failSince the database clock of its first failure.
	failing   store.StallReason
	failSince time.Time
}

func (o *observer) pass(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	now, err := o.w.st.DatabaseNow(ctx)
	if err != nil {
		return err
	}
	st, err := o.w.cfg.Chain.Status(ctx)
	if err != nil {
		return o.failed(ctx, now, err)
	}
	o.failing = ""
	if err := o.w.st.RecordChainHealth(ctx, uint64(st.Height), st.Time); err != nil {
		return err
	}
	if st.Height > o.height {
		o.height, o.since = st.Height, now
		return o.closeBelow(ctx, st.Height)
	}
	if now.Sub(o.since) >= o.w.cfg.Numbers.StallWindow {
		return o.open(ctx, store.StallHeight)
	}
	return nil
}

// failed handles a status read that failed with cause.
func (o *observer) failed(ctx context.Context, now time.Time, cause error) error {
	reason := store.StallUnreachable
	switch {
	case errors.Is(cause, chain.ErrCatchingUp):
		reason = store.StallCatchingUp
	case errors.Is(cause, chain.ErrChainID):
		o.w.alert("NODE_CHAIN_ID_MISMATCH")
	}
	if o.failing != reason {
		o.failing, o.failSince = reason, now
	}
	if now.Sub(o.failSince) >= o.w.cfg.Numbers.StallWindow {
		if err := o.open(ctx, reason); err != nil {
			return errors.Join(cause, err)
		}
	}
	return cause
}

// open opens a stall episode for reason at the latest height read, unless
// one is open. More open intents than one stall transition may mark is an
// integrity alert and opens nothing.
func (o *observer) open(ctx context.Context, reason store.StallReason) error {
	if _, err := o.w.st.OpenStallEpisode(ctx); !errors.Is(err, store.ErrNotFound) {
		return err
	}
	_, err := o.w.st.OpenStall(ctx, reason, uint64(max(o.height, 0)))
	switch {
	case errors.Is(err, store.ErrStallOpen):
		return nil
	case errors.Is(err, store.ErrOpenIntentsBound):
		o.w.alert("OPEN_INTENTS_BOUND", "stall_reason", string(reason))
		return nil
	case err == nil:
		o.w.alert("STALL_OPENED", "stall_reason", string(reason), "height", o.height)
	}
	return err
}

// closeBelow closes the open stall episode when height is above the height
// it opened at.
func (o *observer) closeBelow(ctx context.Context, height int64) error {
	ep, err := o.w.st.OpenStallEpisode(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if uint64(height) <= ep.StartedHeight {
		return nil
	}
	_, err = o.w.st.CloseStall(ctx, uint64(height))
	if errors.Is(err, store.ErrOpenIntentsBound) {
		o.w.alert("OPEN_INTENTS_BOUND", "stall_episode", ep.ID)
		return nil
	}
	if err == nil {
		o.w.log.Info("worker stall closed", "episode", ep.ID, "height", height)
	}
	return err
}
