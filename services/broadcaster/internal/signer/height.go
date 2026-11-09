package signer

import (
	"context"
	"sync"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
)

// statusReader is the /status read (*chain.Client): it refuses a node of
// another chain id or one that is catching up.
type statusReader interface {
	Status(ctx context.Context) (chain.Status, error)
}

// heightSource is the signer's own view of the chain height, for its
// timeout bound (plan 7.2, M3): it reads /status of its node at most once
// per poll interval, and a height read longer ago than the freshness
// window is not fresh. The clock is the signer process's monotonic clock;
// it only decides when to stop signing, never a chain result.
type heightSource struct {
	mu        sync.Mutex
	reader    statusReader
	poll      time.Duration
	freshness time.Duration
	now       func() time.Time
	lastTry   time.Time
	height    int64
	readAt    time.Time
}

func newHeightSource(r statusReader, poll, freshness time.Duration) *heightSource {
	return &heightSource{reader: r, poll: poll, freshness: freshness, now: time.Now}
}

// fresh returns the latest height read and whether it is fresh. It reads
// /status first when the last attempt is at least one poll interval old.
// A failed read keeps the previous height, which goes stale on its own.
func (h *heightSource) fresh(ctx context.Context) (int64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastTry.IsZero() || h.now().Sub(h.lastTry) >= h.poll {
		h.lastTry = h.now()
		if st, err := h.reader.Status(ctx); err == nil && st.Height > 0 {
			h.height, h.readAt = st.Height, h.now()
		}
	}
	if h.height < 1 || h.now().Sub(h.readAt) > h.freshness {
		return 0, false
	}
	return h.height, true
}
