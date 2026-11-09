package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// PersistForward is T8 for a forwarded intent (goal 11 Q2, plan 7.3 P3 and
// 8.3 step 1), on the forwarder session, in one transaction: lock the intent
// (REQUESTED/QUEUED, validated, FORWARD_PROVIDER_TX, before expires_at on
// the database clock, no attempt yet, no open stall episode), then its
// FORWARD_SOURCE signer row (no live attempt at the forwarded sequence),
// insert attempt 1 from the stored bytes and forwarded_txs fields with phase
// SIGNED and sends_started 0, set the intent to IN_FLIGHT/SIGNED and append
// a transitions row. Nothing is signed, edited or allocated: the attempt is
// the forwarded transaction itself (the database checks its hash, account
// number, sequence, timeout height and gas against forwarded_txs). Any
// failed condition rolls back and returns a refusal. A key signer session is
// ErrInvalid.
func (ss *SignerSession) PersistForward(ctx context.Context, intentID string) (PersistResult, error) {
	if !ss.forwarder() {
		return PersistResult{}, fmt.Errorf("%w: forwarded intents are persisted by the forwarder session", ErrInvalid)
	}
	if !uuidRE.MatchString(intentID) {
		return PersistResult{}, fmt.Errorf("%w: intent id", ErrInvalid)
	}
	tx, err := ss.begin(ctx)
	if err != nil {
		return PersistResult{}, err
	}
	res, err := ss.persistForwardTx(ctx, tx, intentID)
	if err != nil || res.Refusal != RefusedNone {
		return res, rollback(tx, err)
	}
	if err := commit(tx); err != nil {
		return PersistResult{}, fmt.Errorf("store: commit forwarded attempt: %w", err)
	}
	return res, nil
}

func (ss *SignerSession) persistForwardTx(ctx context.Context, tx *sql.Tx, intentID string) (PersistResult, error) {
	refuse := func(r PersistRefusal) (PersistResult, error) { return PersistResult{Refusal: r}, nil }
	cur, err := lockIntent(ctx, tx, intentID)
	if err != nil {
		return PersistResult{}, err
	}
	switch {
	case cur.state != StateRequested || cur.phase != PhaseQueued:
		return refuse(RefusedNotRequested)
	case !cur.validated:
		return refuse(RefusedNotValidated)
	case cur.class != ClassForwardSource:
		return refuse(RefusedWrongSigner)
	case !cur.beforeExpiry:
		return refuse(RefusedExpired)
	case cur.attemptCount != 0:
		return refuse(RefusedAttemptCap)
	}
	var stalled bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM broadcaster.stall_episodes WHERE ended_at IS NULL)`).
		Scan(&stalled); err != nil {
		return PersistResult{}, fmt.Errorf("store: read stall: %w", mapErr(err))
	}
	if stalled {
		return refuse(RefusedStalled)
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT f.sequence FROM broadcaster.forwarded_txs f
		JOIN broadcaster.signers s ON s.address = f.outer_signer
		WHERE f.intent_id = $1 FOR UPDATE OF s`, intentID).Scan(&seq); err != nil {
		return PersistResult{}, fmt.Errorf("store: lock forward source: %w", mapErr(err))
	}
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM broadcaster.attempts
		WHERE signer_address = $1 AND sequence = $2 AND dead_at IS NULL AND committed_height IS NULL)`,
		cur.signerAddress, seq).Scan(&busy); err != nil {
		return PersistResult{}, fmt.Errorf("store: read forward source attempts: %w", mapErr(err))
	}
	if busy {
		return refuse(RefusedSignerBusy)
	}
	var (
		attemptID string
		timeout   int64
		hash      []byte
	)
	if err := tx.QueryRowContext(ctx, `INSERT INTO broadcaster.attempts (intent_id, n, business_n, signer_address,
			account_number, sequence, timeout_height, gas, tx_bytes, tx_hash, phase, worker_instance)
		SELECT i.intent_id, 1, 1, i.signer_address, f.account_number, f.sequence, f.timeout_height, f.gas, i.request,
			f.tx_hash, 'SIGNED', $2
		FROM broadcaster.intents i JOIN broadcaster.forwarded_txs f ON f.intent_id = i.intent_id
		WHERE i.intent_id = $1
		RETURNING attempt_id, timeout_height, tx_hash`, intentID, ss.s.instance).Scan(&attemptID, &timeout, &hash); err != nil {
		return PersistResult{}, fmt.Errorf("store: insert forwarded attempt: %w", mapErr(err))
	}
	obs, err := json.Marshal(map[string]any{"sequence": seq, "timeout_height": timeout, "tx_hash": hex.EncodeToString(hash),
		"forwarded": true})
	if err != nil {
		return PersistResult{}, fmt.Errorf("store: observation: %w", err)
	}
	from := Expect{cur.state, cur.phase}
	if err := ss.s.recordTransition(ctx, tx, cur, transition{
		intentID: intentID, attemptID: attemptID, from: &from, to: Expect{StateInFlight, PhaseSigned},
		reason: "ATTEMPT_PERSISTED", observation: obs,
	}); err != nil {
		return PersistResult{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.intents SET state = 'IN_FLIGHT', phase = 'SIGNED',
		attempt_count = 1 WHERE intent_id = $1 AND state = 'REQUESTED' AND phase = 'QUEUED' AND attempt_count = 0`, intentID)
	if err := oneRow(res, err, "set forwarded intent in flight"); err != nil {
		return PersistResult{}, err
	}
	return PersistResult{AttemptID: attemptID}, nil
}

// DueForwards lists up to limit (1 to 1,000) validated REQUESTED/QUEUED
// FORWARD_PROVIDER_TX intents whose next_attempt_at has passed on the
// database clock, oldest schedule first: the forwarder's work list.
func (s *Store) DueForwards(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > maxListLimit {
		return nil, fmt.Errorf("%w: limit", ErrInvalid)
	}
	return s.listIDs(ctx, `SELECT i.intent_id FROM broadcaster.intents i JOIN broadcaster.kinds k ON k.kind = i.kind
		WHERE k.signer_class = 'FORWARD_SOURCE' AND i.state = 'REQUESTED' AND i.phase = 'QUEUED' AND i.validated
		  AND i.attempt_count = 0 AND i.next_attempt_at <= clock_timestamp()
		ORDER BY i.next_attempt_at, i.created_at, i.intent_id LIMIT $1`, limit)
}

// LiveForwardAttempts lists up to limit (1 to 1,000) live attempts of
// FORWARD_SOURCE accounts with their bytes, oldest first: what the
// forwarder session reconciles and rebroadcasts.
func (s *Store) LiveForwardAttempts(ctx context.Context, limit int) ([]AttemptTx, error) {
	if limit < 1 || limit > maxListLimit {
		return nil, fmt.Errorf("%w: limit", ErrInvalid)
	}
	return s.queryAttempts(ctx, true, `JOIN broadcaster.signers s ON s.address = a.signer_address
		WHERE s.class = 'FORWARD_SOURCE' AND a.dead_at IS NULL AND a.committed_height IS NULL
		ORDER BY a.created_at, a.attempt_id LIMIT $1`, limit)
}
