package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RecordChainHealth stores the latest node status the observer read, with
// seen_at from the database clock (plan section 10).
func (s *Store) RecordChainHealth(ctx context.Context, latestHeight uint64, latestTime time.Time) error {
	height, err := toInt64("height", latestHeight)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO broadcaster.chain_health (chain_id, latest_height, latest_time,
			seen_at, observer_instance)
		SELECT b.chain_id, $1, $2, clock_timestamp(), $3 FROM broadcaster.chain_binding b
		ON CONFLICT (chain_id) DO UPDATE SET latest_height = EXCLUDED.latest_height,
			latest_time = EXCLUDED.latest_time, seen_at = EXCLUDED.seen_at,
			observer_instance = EXCLUDED.observer_instance`, height, latestTime.UTC(), s.instance)
	if err != nil {
		return fmt.Errorf("store: record chain health: %w", mapErr(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: record chain health: %w", err)
	}
	if n != 1 {
		return ErrChainUnbound
	}
	return nil
}

// ChainHealth is the observer's latest record.
type ChainHealth struct {
	LatestHeight uint64
	LatestTime   time.Time
	SeenAt       time.Time
	// Age is the database clock minus SeenAt; the observer is fresh when it
	// is within the configured number of poll intervals.
	Age      time.Duration
	Observer string
}

// ReadChainHealth returns the latest record, or ErrNotFound before the first.
func (s *Store) ReadChainHealth(ctx context.Context) (ChainHealth, error) {
	var (
		h      ChainHealth
		height int64
		ageUs  int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT latest_height, latest_time, seen_at,
			(extract(epoch FROM clock_timestamp() - seen_at) * 1000000)::bigint, observer_instance
		FROM broadcaster.chain_health`).Scan(&height, &h.LatestTime, &h.SeenAt, &ageUs, &h.Observer)
	if err != nil {
		return ChainHealth{}, errNoRowsIsNotFound(err, "read chain health")
	}
	h.LatestHeight = uint64(height)
	h.Age = time.Duration(ageUs) * time.Microsecond
	return h, nil
}

// StallReason is why a stall episode opened.
type StallReason string

// Stall reasons.
const (
	StallHeight      StallReason = "HEIGHT_STALLED"
	StallUnreachable StallReason = "NODE_UNREACHABLE"
	StallCatchingUp  StallReason = "CATCHING_UP"
)

// StallEpisode is one stall_episodes row.
type StallEpisode struct {
	ID            int64
	Reason        StallReason
	StartedHeight uint64
	StartedAt     time.Time
}

// OpenStall opens a stall episode at height and appends a STALL_OPENED
// transitions row to every non-terminal intent, in one transaction. While it
// is open PersistAttempt refuses every draft. A second open episode is
// ErrStallOpen; more than 16,384 non-terminal intents is ErrOpenIntentsBound
// and nothing is written.
func (s *Store) OpenStall(ctx context.Context, reason StallReason, height uint64) (int64, error) {
	switch reason {
	case StallHeight, StallUnreachable, StallCatchingUp:
	default:
		return 0, fmt.Errorf("%w: stall reason", ErrInvalid)
	}
	h, err := toInt64("height", height)
	if err != nil {
		return 0, err
	}
	tx, err := s.beginTx(ctx)
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO broadcaster.stall_episodes (chain_id, reason, started_height)
		SELECT b.chain_id, $1, $2 FROM broadcaster.chain_binding b RETURNING id`, string(reason), h).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, rollback(tx, ErrChainUnbound)
	case SQLState(err) == "23505":
		return 0, rollback(tx, ErrStallOpen)
	case err != nil:
		return 0, rollback(tx, fmt.Errorf("store: open stall: %w", mapErr(err)))
	}
	obs, err := json.Marshal(map[string]any{"episode": id, "reason": reason, "height": h})
	if err != nil {
		return 0, rollback(tx, fmt.Errorf("store: observation: %w", err))
	}
	if err := s.markOpenIntents(ctx, tx, "STALL_OPENED", obs); err != nil {
		return 0, rollback(tx, err)
	}
	if err := commit(tx); err != nil {
		return 0, fmt.Errorf("store: commit stall: %w", err)
	}
	return id, nil
}

// CloseStall closes the open stall episode at height and appends a
// STALL_CLOSED row to every non-terminal intent. It returns false when no
// episode was open.
func (s *Store) CloseStall(ctx context.Context, height uint64) (bool, error) {
	h, err := toInt64("height", height)
	if err != nil {
		return false, err
	}
	tx, err := s.beginTx(ctx)
	if err != nil {
		return false, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `UPDATE broadcaster.stall_episodes SET ended_height = $1, ended_at = clock_timestamp()
		WHERE ended_at IS NULL RETURNING id`, h).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, rollback(tx, nil)
	}
	if err != nil {
		return false, rollback(tx, fmt.Errorf("store: close stall: %w", mapErr(err)))
	}
	obs, err := json.Marshal(map[string]any{"episode": id, "height": h})
	if err != nil {
		return false, rollback(tx, fmt.Errorf("store: observation: %w", err))
	}
	if err := s.markOpenIntents(ctx, tx, "STALL_CLOSED", obs); err != nil {
		return false, rollback(tx, err)
	}
	if err := commit(tx); err != nil {
		return false, fmt.Errorf("store: commit stall close: %w", err)
	}
	return true, nil
}

// maxStallIntents is the most non-terminal intents one stall transition
// marks: the hard bound on the sum of the open-intent caps, which
// register_caller enforces (0001_outbox.pgsql).
const maxStallIntents = 16384

// markOpenIntents appends reason to every non-terminal intent, locking them
// in intent id order. More than maxStallIntents non-terminal intents (caps
// lowered below the intents already open) is ErrOpenIntentsBound, and the
// caller rolls back: no intent is left without its row.
func (s *Store) markOpenIntents(ctx context.Context, tx *sql.Tx, reason string, obs json.RawMessage) error {
	rows, err := tx.QueryContext(ctx, `SELECT intent_id FROM broadcaster.intents
		WHERE state IN ('REQUESTED', 'IN_FLIGHT') ORDER BY intent_id LIMIT $1`, maxStallIntents+1)
	if err != nil {
		return fmt.Errorf("store: list open intents: %w", mapErr(err))
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return errors.Join(fmt.Errorf("store: scan open intent: %w", err), rows.Close())
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("store: list open intents: %w", err)
	}
	if len(ids) > maxStallIntents {
		return ErrOpenIntentsBound
	}
	for _, id := range ids {
		cur, err := lockIntent(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.state != StateRequested && cur.state != StateInFlight {
			continue
		}
		at := Expect{cur.state, cur.phase}
		if err := s.recordTransition(ctx, tx, cur, transition{
			intentID: id, from: &at, to: at, reason: reason, observation: obs,
		}); err != nil {
			return err
		}
	}
	return nil
}

// OpenStallEpisode returns the open stall episode, or ErrNotFound.
func (s *Store) OpenStallEpisode(ctx context.Context) (StallEpisode, error) {
	var (
		e      StallEpisode
		height int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, reason, started_height, started_at FROM broadcaster.stall_episodes
		WHERE ended_at IS NULL`).Scan(&e.ID, &e.Reason, &height, &e.StartedAt)
	if err != nil {
		return StallEpisode{}, errNoRowsIsNotFound(err, "read stall episode")
	}
	e.StartedHeight = uint64(height)
	return e, nil
}
