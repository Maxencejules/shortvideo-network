package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Reads the submission engine (internal/worker) needs beyond work.go. Each
// is bounded and changes nothing.

// DatabaseNow returns clock_timestamp(): the database clock that decides
// expiry, schedules and stall windows (plan section 10), never a process's
// local clock.
func (s *Store) DatabaseNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("store: read database clock: %w", mapErr(err))
	}
	return now, nil
}

// MaxTimeoutAt returns the highest timeout_height of every attempt, of any
// intent, that signer stored at sequence, and false when there is none. A
// new attempt at that sequence must carry a higher timeout height (plan
// 8.2), which PersistAttempt checks again under the signer row lock.
func (s *Store) MaxTimeoutAt(ctx context.Context, signer []byte, sequence uint64) (uint64, bool, error) {
	if len(signer) != 20 {
		return 0, false, fmt.Errorf("%w: signer address", ErrInvalid)
	}
	seq, err := toInt64("sequence", sequence)
	if err != nil {
		return 0, false, err
	}
	var top sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT max(timeout_height) FROM broadcaster.attempts
		WHERE signer_address = $1 AND sequence = $2`, signer, seq).Scan(&top); err != nil {
		return 0, false, fmt.Errorf("store: read earlier timeout heights: %w", mapErr(err))
	}
	if !top.Valid {
		return 0, false, nil
	}
	return uint64(top.Int64), true, nil
}

// PendingOwnerConfirmations lists up to limit (1 to 1,000) REGISTER_CONTENT
// intents that are COMMITTED while their content owner row is still
// PENDING, oldest first: the rows ConfirmContentOwner may make EFFECTIVE
// once the node shows our signer as the submitting host (goal 11 Q3, plan
// 5.7).
func (s *Store) PendingOwnerConfirmations(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > maxListLimit {
		return nil, fmt.Errorf("%w: limit", ErrInvalid)
	}
	return s.listIDs(ctx, `SELECT o.intent_id FROM broadcaster.content_owners o
		JOIN broadcaster.intents i ON i.intent_id = o.intent_id
		WHERE o.status = 'PENDING' AND i.state = 'COMMITTED'
		ORDER BY o.created_at, o.intent_id LIMIT $1`, limit)
}
