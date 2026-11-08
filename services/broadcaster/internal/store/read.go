package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// errNoRowsIsNotFound maps sql.ErrNoRows to ErrNotFound and wraps any other
// error.
func errNoRowsIsNotFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("store: %s: %w", what, mapErr(err))
}

// Intent is the status view of one intent. It carries no request bytes and
// no authorization context.
type Intent struct {
	ID                    string
	CallerPrincipal       string
	IdempotencyKey        string
	Kind                  string
	State                 State
	Phase                 Phase
	Reason                string
	Failure               *ChainCode
	BusinessID            string
	AttemptCount          int
	CreatedAt             time.Time
	ExpiresAt             time.Time
	FinalAt               *time.Time
	CommittedTxHash       []byte
	OwnTransaction        *bool
	TransitionsRecorded   int
	TransitionsSuppressed int
}

// GetIntent returns the caller's own intent. Another caller's intent and an
// unknown id are both ErrNotFound, so ids cannot be probed (plan 5.7).
func (s *Store) GetIntent(ctx context.Context, callerPrincipal, intentID string) (Intent, error) {
	if !idRE.MatchString(callerPrincipal) || !uuidRE.MatchString(intentID) {
		return Intent{}, ErrNotFound
	}
	var (
		it               Intent
		phase, reason    sql.NullString
		code             sql.NullInt64
		codespace, bizID sql.NullString
		final            sql.NullTime
		own              sql.NullBool
	)
	err := s.db.QueryRowContext(ctx, `SELECT intent_id, caller_principal, idempotency_key, kind, state, phase, reason,
			failure_code, failure_codespace, business_id, attempt_count, created_at, expires_at, final_at,
			committed_tx_hash, own_transaction, transitions_recorded, transitions_suppressed
		FROM broadcaster.intents WHERE intent_id = $1 AND caller_principal = $2`, intentID, callerPrincipal).Scan(
		&it.ID, &it.CallerPrincipal, &it.IdempotencyKey, &it.Kind, &it.State, &phase, &reason, &code, &codespace,
		&bizID, &it.AttemptCount, &it.CreatedAt, &it.ExpiresAt, &final, &it.CommittedTxHash, &own,
		&it.TransitionsRecorded, &it.TransitionsSuppressed)
	if err != nil {
		return Intent{}, errNoRowsIsNotFound(err, "read intent")
	}
	it.Phase = Phase(phase.String)
	it.Reason = reason.String
	it.BusinessID = bizID.String
	if code.Valid {
		it.Failure = &ChainCode{Code: uint32(code.Int64), Codespace: codespace.String}
	}
	if final.Valid {
		it.FinalAt = &final.Time
	}
	if own.Valid {
		it.OwnTransaction = &own.Bool
	}
	return it, nil
}

// DueIntents lists up to limit (1 to 1,000) validated REQUESTED intents of a
// signer whose next_attempt_at has passed on the database clock, oldest
// schedule first.
func (s *Store) DueIntents(ctx context.Context, signer []byte, limit int) ([]string, error) {
	if len(signer) != 20 || limit < 1 || limit > maxListLimit {
		return nil, fmt.Errorf("%w: signer or limit", ErrInvalid)
	}
	return s.listIDs(ctx, `SELECT intent_id FROM broadcaster.intents
		WHERE signer_address = $1 AND state = 'REQUESTED' AND validated AND next_attempt_at <= clock_timestamp()
		ORDER BY next_attempt_at, created_at, intent_id LIMIT $2`, signer, limit)
}

// Unvalidated is a SQL-path intent waiting for the worker's validation.
type Unvalidated struct {
	ID              string
	CallerPrincipal string
	Kind            string
	// RequestJSON and AuthContext are the jsonb texts as stored.
	RequestJSON []byte
	AuthContext []byte
	SQLDigest   []byte
}

// UnvalidatedIntents lists up to limit (1 to 1,000) REQUESTED SQL-path
// intents not yet validated, oldest first.
func (s *Store) UnvalidatedIntents(ctx context.Context, limit int) ([]Unvalidated, error) {
	if limit < 1 || limit > maxListLimit {
		return nil, fmt.Errorf("%w: limit", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT intent_id, caller_principal, kind, request_json::text,
			auth_context::text, sql_digest
		FROM broadcaster.intents WHERE state = 'REQUESTED' AND NOT validated
		ORDER BY created_at, intent_id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list unvalidated: %w", mapErr(err))
	}
	defer rows.Close()
	var out []Unvalidated
	for rows.Next() {
		var u Unvalidated
		if err := rows.Scan(&u.ID, &u.CallerPrincipal, &u.Kind, &u.RequestJSON, &u.AuthContext, &u.SQLDigest); err != nil {
			return nil, fmt.Errorf("store: scan unvalidated: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list unvalidated: %w", err)
	}
	return out, nil
}

func (s *Store) listIDs(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", mapErr(err))
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}
	return ids, nil
}

// Attempt is one attempts row without its transaction bytes.
type Attempt struct {
	ID              string
	N               int
	BusinessN       int
	Sequence        uint64
	TimeoutHeight   uint64
	TxHash          []byte
	Phase           AttemptPhase
	SendsStarted    int
	SendsRecorded   int
	UnknownSends    int
	CacheAnswers    int
	CountedRefusals int
	Acked           bool
	DeadReason      string
	CommittedHeight uint64
	CommittedCode   *ChainCode
	AccSeqPresent   *bool
	WorkerInstance  string
}

// Attempts lists an intent's attempts in order (at most 32).
func (s *Store) Attempts(ctx context.Context, intentID string) ([]Attempt, error) {
	if !uuidRE.MatchString(intentID) {
		return nil, fmt.Errorf("%w: intent id", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT attempt_id, n, business_n, sequence, timeout_height, tx_hash, phase,
			sends_started, sends_recorded, unknown_sends, cache_answers, counted_refusals, acked, dead_reason,
			committed_height, committed_code, committed_codespace, acc_seq_present, worker_instance
		FROM broadcaster.attempts WHERE intent_id = $1 ORDER BY n LIMIT 32`, intentID)
	if err != nil {
		return nil, fmt.Errorf("store: list attempts: %w", mapErr(err))
	}
	defer rows.Close()
	var out []Attempt
	for rows.Next() {
		var (
			a               Attempt
			seq, timeout    int64
			dead, codespace sql.NullString
			height, code    sql.NullInt64
			accSeq          sql.NullBool
		)
		if err := rows.Scan(&a.ID, &a.N, &a.BusinessN, &seq, &timeout, &a.TxHash, &a.Phase, &a.SendsStarted,
			&a.SendsRecorded, &a.UnknownSends, &a.CacheAnswers, &a.CountedRefusals, &a.Acked, &dead, &height, &code,
			&codespace, &accSeq, &a.WorkerInstance); err != nil {
			return nil, fmt.Errorf("store: scan attempt: %w", err)
		}
		a.Sequence, a.TimeoutHeight = uint64(seq), uint64(timeout)
		a.DeadReason = dead.String
		a.CommittedHeight = uint64(height.Int64)
		if code.Valid {
			a.CommittedCode = &ChainCode{Code: uint32(code.Int64), Codespace: codespace.String}
		}
		if accSeq.Valid {
			a.AccSeqPresent = &accSeq.Bool
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list attempts: %w", err)
	}
	return out, nil
}

// Transition is one transitions row: the failure trace of an intent.
type Transition struct {
	ID             int64
	AttemptID      string
	At             time.Time
	From           *Expect
	To             Expect
	Reason         string
	Observation    []byte
	WorkerInstance string
}

// Transitions lists an intent's transitions in order (at most 4,096).
func (s *Store) Transitions(ctx context.Context, intentID string) ([]Transition, error) {
	if !uuidRE.MatchString(intentID) {
		return nil, fmt.Errorf("%w: intent id", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, attempt_id, at, from_state, from_phase, to_state, to_phase, reason,
			observation::text, worker_instance
		FROM broadcaster.transitions WHERE intent_id = $1 ORDER BY id LIMIT 4096`, intentID)
	if err != nil {
		return nil, fmt.Errorf("store: list transitions: %w", mapErr(err))
	}
	defer rows.Close()
	var out []Transition
	for rows.Next() {
		var (
			t                               Transition
			attempt, fromS, fromP, toP, obs sql.NullString
		)
		if err := rows.Scan(&t.ID, &attempt, &t.At, &fromS, &fromP, &t.To.State, &toP, &t.Reason, &obs,
			&t.WorkerInstance); err != nil {
			return nil, fmt.Errorf("store: scan transition: %w", err)
		}
		t.AttemptID = attempt.String
		if fromS.Valid {
			t.From = &Expect{State: State(fromS.String), Phase: Phase(fromP.String)}
		}
		t.To.Phase = Phase(toP.String)
		if obs.Valid {
			t.Observation = []byte(obs.String)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list transitions: %w", err)
	}
	return out, nil
}

// ConflictStatus is what signer_events records about one foreign use of a
// signer sequence (plan 8.1, DR-11-15).
type ConflictStatus string

// Conflict statuses.
const (
	// ConflictNone: no event is recorded for the hash at that sequence.
	ConflictNone ConflictStatus = "NONE"
	// ConflictOpen: the event is recorded and not cleared.
	ConflictOpen ConflictStatus = "OPEN"
	// ConflictCleared: an operator acknowledged it with signer-clear.
	ConflictCleared ConflictStatus = "CLEARED"
)

// Conflict reports what signer_events records about the foreign transaction
// foreignTxHash at sequence of the 20-byte signer. The worker maps
// ConflictCleared to an acknowledged foreign transaction.
func (s *Store) Conflict(ctx context.Context, signer []byte, sequence uint64, foreignTxHash [32]byte) (ConflictStatus, error) {
	if len(signer) != 20 {
		return "", fmt.Errorf("%w: signer address", ErrInvalid)
	}
	seq, err := toInt64("sequence", sequence)
	if err != nil {
		return "", err
	}
	var cleared bool
	err = s.db.QueryRowContext(ctx, `SELECT cleared_at IS NOT NULL FROM broadcaster.signer_events
		WHERE signer_address = $1 AND sequence = $2 AND foreign_tx_hash = $3`, signer, seq, foreignTxHash[:]).Scan(&cleared)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ConflictNone, nil
	case err != nil:
		return "", fmt.Errorf("store: read signer event: %w", mapErr(err))
	case cleared:
		return ConflictCleared, nil
	}
	return ConflictOpen, nil
}
