package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// SignerTx groups the writes that follow one reconciliation decision (plan
// 8.6, transitions T5-T7 and T11-T15) into one transaction on the signer
// session: an attempt resolution and the intent transition it causes commit
// together or not at all. At Commit the database checks that an IN_FLIGHT
// intent has exactly one live attempt, any other intent none, and that a
// COMMITTED intent has its commitment row; a violation is ErrLiveAttempt or
// ErrEvidence and nothing is written.
type SignerTx struct {
	ss *SignerSession
	tx *sql.Tx
}

// Begin starts a SignerTx after checking the session still holds the signer
// lock. Commit or Rollback ends it; Close rolls back one left open.
func (ss *SignerSession) Begin(ctx context.Context) (*SignerTx, error) {
	tx, err := ss.begin(ctx)
	if err != nil {
		return nil, err
	}
	t := &SignerTx{ss: ss, tx: tx}
	ss.open = t
	return t, nil
}

// Commit commits the transaction.
func (t *SignerTx) Commit() error {
	t.release()
	return commit(t.tx)
}

// Rollback abandons the transaction; after Commit it does nothing.
func (t *SignerTx) Rollback() error {
	t.release()
	return rollback(t.tx, nil)
}

// release marks the session free for its next transaction.
func (t *SignerTx) release() {
	if t.ss.open == t {
		t.ss.open = nil
	}
}

// CommittedResult is an attempt's result read from the chain by /tx(H), with
// the returned bytes hashing to H (plan 8.4).
type CommittedResult struct {
	// TxHash is H; ResolveCommitted refuses it unless it is the attempt's
	// tx_hash, so a result read for other bytes (such as another attempt at
	// the same sequence) is never stored on this attempt.
	TxHash    [32]byte
	Height    uint64
	Index     uint32
	Code      uint32
	Codespace string
	// AccSeqPresent says the result carries tx.acc_seq=<signer>/<sequence>
	// of this attempt: the ante passed and the sequence was consumed.
	AccSeqPresent bool
	GasUsed       uint64
	// Idempotent says a code-0 result carries only <module>_result
	// idempotent=true and no typed event.
	Idempotent bool
}

// phase returns the terminal phase of r: code 0 is COMMITTED_OK or
// COMMITTED_IDEMPOTENT; a non-zero code is EXECUTION_FAILED when tx.acc_seq
// is present and INCLUDED_ANTE_FAILED otherwise (the rule reads acc_seq,
// never the code).
func (r CommittedResult) phase() AttemptPhase {
	switch {
	case r.Code == 0 && r.Idempotent:
		return AttemptCommittedIdempotent
	case r.Code == 0:
		return AttemptCommittedOK
	case r.AccSeqPresent:
		return AttemptExecutionFailed
	default:
		return AttemptIncludedAnteFailed
	}
}

// ResolveCommitted records a committed result on a live attempt of the
// session's signer and raises the signer's observed height to the committed
// height, so a later SyncSequence answered from older state is
// ErrStaleObservation. A terminal attempt is ErrImmutable: a result for an
// attempt already marked DEAD is the 8.5 integrity alert and nothing is
// rewritten. A result for another tx_hash, a code-0 result without
// tx.acc_seq, or an idempotent flag on a non-zero code, is ErrInvalid.
func (t *SignerTx) ResolveCommitted(ctx context.Context, attemptID string, r CommittedResult) error {
	if r.Code == 0 && !r.AccSeqPresent {
		return fmt.Errorf("%w: a committed success always consumes the sequence", ErrInvalid)
	}
	if r.Code != 0 && r.Idempotent {
		return fmt.Errorf("%w: idempotent needs code 0", ErrInvalid)
	}
	if r.Height == 0 || !codespaceRE.MatchString(r.Codespace) {
		return fmt.Errorf("%w: height or codespace", ErrInvalid)
	}
	height, err := toInt64("height", r.Height)
	if err != nil {
		return err
	}
	gasUsed, err := toInt64("gas used", r.GasUsed)
	if err != nil {
		return err
	}
	cur, a, err := t.ss.lockAttempt(ctx, t.tx, attemptID)
	if err != nil {
		return err
	}
	if !a.phase.Live() {
		return fmt.Errorf("%w: attempt is %s", ErrImmutable, a.phase)
	}
	if !bytes.Equal(a.txHash, r.TxHash[:]) {
		return fmt.Errorf("%w: result is for another transaction hash", ErrInvalid)
	}
	phase := r.phase()
	obs, err := json.Marshal(map[string]any{"height": height, "code": r.Code, "codespace": r.Codespace, "phase": phase})
	if err != nil {
		return fmt.Errorf("store: observation: %w", err)
	}
	res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.attempts SET phase = $2, committed_height = $3,
		committed_index = $4, committed_code = $5, committed_codespace = $6, acc_seq_present = $7, gas_used = $8
		WHERE attempt_id = $1 AND phase = $9`,
		attemptID, string(phase), height, int64(r.Index), int64(r.Code), r.Codespace, r.AccSeqPresent, gasUsed,
		string(a.phase))
	if err := oneRow(res, err, "resolve attempt"); err != nil {
		return err
	}
	res, err = t.tx.ExecContext(ctx, `UPDATE broadcaster.signers SET observed_height = GREATEST(observed_height, $2)
		WHERE address = $1`, t.ss.address, height)
	if err := oneRow(res, err, "raise observed height"); err != nil {
		return err
	}
	return t.observe(ctx, cur, attemptID, "ATTEMPT_"+string(phase), obs)
}

// DeadReason is the rule that killed an attempt (plan 8.5).
type DeadReason string

// Dead reasons.
const (
	DeadD2Refused        DeadReason = "D2_REFUSED"
	DeadD3DeadlinePassed DeadReason = "D3_DEADLINE_PASSED"
	DeadD3SequenceUsed   DeadReason = "D3_SEQUENCE_USED"
)

// MarkDead marks a live attempt of the session's signer DEAD. The database
// refuses D2_REFUSED unless at least one send was started, every send was
// recorded and every recorded outcome was a counted refusal (ErrInvalid via
// the check constraint, SQLSTATE 23514).
func (t *SignerTx) MarkDead(ctx context.Context, attemptID string, reason DeadReason) error {
	switch reason {
	case DeadD2Refused, DeadD3DeadlinePassed, DeadD3SequenceUsed:
	default:
		return fmt.Errorf("%w: dead reason", ErrInvalid)
	}
	cur, a, err := t.ss.lockAttempt(ctx, t.tx, attemptID)
	if err != nil {
		return err
	}
	if !a.phase.Live() {
		return fmt.Errorf("%w: attempt is %s", ErrImmutable, a.phase)
	}
	res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.attempts SET phase = 'DEAD', dead_reason = $2,
		dead_at = clock_timestamp() WHERE attempt_id = $1 AND phase = $3`, attemptID, string(reason), string(a.phase))
	if err := oneRow(res, err, "mark attempt dead"); err != nil {
		if SQLState(err) == "23514" {
			return fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		return err
	}
	obs, err := json.Marshal(map[string]any{"dead_reason": reason})
	if err != nil {
		return fmt.Errorf("store: observation: %w", err)
	}
	return t.observe(ctx, cur, attemptID, "ATTEMPT_DEAD", obs)
}

// observe appends an observation row (state and phase unchanged) for an
// intent locked in this transaction.
func (t *SignerTx) observe(ctx context.Context, cur lockedIntent, attemptID, reason string, obs json.RawMessage) error {
	from := Expect{cur.state, cur.phase}
	return t.ss.s.recordTransition(ctx, t.tx, cur, transition{
		intentID: cur.id, attemptID: attemptID, from: &from, to: from, reason: reason, observation: obs,
	})
}

// lockExpected locks the intent and checks it is in the expected state and
// phase.
func (t *SignerTx) lockExpected(ctx context.Context, intentID string, from Expect) (lockedIntent, error) {
	cur, err := lockIntent(ctx, t.tx, intentID)
	if err != nil {
		return lockedIntent{}, err
	}
	if cur.state != from.State || cur.phase != from.Phase {
		return lockedIntent{}, fmt.Errorf("%w: intent is %s/%s", ErrStateChanged, cur.state, cur.phase)
	}
	if !bytes.Equal(cur.signerAddress, t.ss.address) {
		return lockedIntent{}, fmt.Errorf("%w: intent belongs to another signer", ErrStateChanged)
	}
	return cur, nil
}

// Commitment is the canonical commitment evidence of plan 9.5.
type Commitment struct {
	Height    uint64
	BlockHash [32]byte
	// TxHash is the transaction this intent's result was read from;
	// FirstCommitTxHash is the first commitment of the effect. They differ
	// only for an idempotent replay.
	TxHash            [32]byte
	FirstCommitTxHash [32]byte
	TxIndex           uint32
	Codespace         string
	GasWanted         uint64
	GasUsed           uint64
	// Events is a JSON array of the typed events with their positions, at
	// most 65,536 bytes.
	Events           json.RawMessage
	CommittingSigner []byte
}

// CommitIntent is T5, T11 or T12: the intent, in the expected non-terminal
// state, becomes COMMITTED with its commitment row and own_transaction, and
// loses the reason a RETRY_WAIT or AWAITING_DEADLINE phase gave it. The
// commitment's chain id and business id are the intent's. At Commit the
// database checks the evidence against the intent's attempts
// (0001_outbox.pgsql, check_committed_evidence): own_transaction is true
// exactly when FirstCommitTxHash is an attempt of this intent resolved
// COMMITTED_OK, and an idempotent replay's TxHash is an attempt of this
// intent resolved COMMITTED_IDEMPOTENT; otherwise ErrEvidence.
func (t *SignerTx) CommitIntent(ctx context.Context, intentID string, from Expect, c Commitment, ownTransaction bool) error {
	if from.State != StateRequested && from.State != StateInFlight {
		return fmt.Errorf("%w: COMMITTED only from REQUESTED or IN_FLIGHT", ErrInvalid)
	}
	if c.Height == 0 || len(c.CommittingSigner) != 20 || !codespaceRE.MatchString(c.Codespace) {
		return fmt.Errorf("%w: commitment", ErrInvalid)
	}
	if len(c.Events) == 0 || len(c.Events) > maxEventsBytes || !json.Valid(c.Events) {
		return fmt.Errorf("%w: commitment events", ErrInvalid)
	}
	height, err := toInt64("height", c.Height)
	if err != nil {
		return err
	}
	gasWanted, err := toInt64("gas wanted", c.GasWanted)
	if err != nil {
		return err
	}
	gasUsed, err := toInt64("gas used", c.GasUsed)
	if err != nil {
		return err
	}
	cur, err := t.lockExpected(ctx, intentID, from)
	if err != nil {
		return err
	}
	obs, err := json.Marshal(map[string]any{"height": height, "first_commit_tx_hash": hex.EncodeToString(c.FirstCommitTxHash[:]),
		"own_transaction": ownTransaction})
	if err != nil {
		return fmt.Errorf("store: observation: %w", err)
	}
	if err := t.ss.s.recordTransition(ctx, t.tx, cur, transition{
		intentID: intentID, from: &from, to: Expect{StateCommitted, PhaseNone}, reason: "COMMITTED", observation: obs,
	}); err != nil {
		return err
	}
	res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.intents SET state = 'COMMITTED', phase = NULL, reason = NULL,
		final_at = clock_timestamp(), committed_tx_hash = $2, own_transaction = $3
		WHERE intent_id = $1 AND state = $4 AND phase = $5`,
		intentID, c.FirstCommitTxHash[:], ownTransaction, string(from.State), string(from.Phase))
	if err := oneRow(res, err, "commit intent"); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO broadcaster.commitments (intent_id, chain_id, business_id,
			height, block_hash, tx_hash, first_commit_tx_hash, tx_index, msg_index, code, codespace, gas_wanted,
			gas_used, events, idempotent_replay, committing_signer)
		SELECT $1, i.chain_id, i.business_id, $2, $3, $4, $5, $6, 0, 0, $7, $8, $9, $10::jsonb, $11, $12
		FROM broadcaster.intents i WHERE i.intent_id = $1`,
		intentID, height, c.BlockHash[:], c.TxHash[:], c.FirstCommitTxHash[:], int64(c.TxIndex), c.Codespace,
		gasWanted, gasUsed, string(c.Events), c.TxHash != c.FirstCommitTxHash, c.CommittingSigner); err != nil {
		return fmt.Errorf("store: insert commitment: %w", mapErr(err))
	}
	return nil
}

// ChainCode is a chain result code with its codespace.
type ChainCode struct {
	Code      uint32
	Codespace string
}

// FailIntent is T6, T7, T13 or T14 ending in FAILED: the intent, in the
// expected non-terminal state, becomes FAILED with reason
// (^[A-Z][A-Z0-9_]{0,39}$) and an optional chain code.
func (t *SignerTx) FailIntent(ctx context.Context, intentID string, from Expect, reason string, code *ChainCode) error {
	if from.State != StateRequested && from.State != StateInFlight {
		return fmt.Errorf("%w: FAILED only from REQUESTED or IN_FLIGHT", ErrInvalid)
	}
	if !codeRE.MatchString(reason) {
		return fmt.Errorf("%w: reason", ErrInvalid)
	}
	var fcode sql.NullInt64
	var fspace sql.NullString
	if code != nil {
		if !printableRE.MatchString(code.Codespace) {
			return fmt.Errorf("%w: codespace", ErrInvalid)
		}
		fcode = sql.NullInt64{Int64: int64(code.Code), Valid: true}
		fspace = sql.NullString{String: code.Codespace, Valid: true}
	}
	cur, err := t.lockExpected(ctx, intentID, from)
	if err != nil {
		return err
	}
	if err := t.ss.s.recordTransition(ctx, t.tx, cur, transition{
		intentID: intentID, from: &from, to: Expect{StateFailed, PhaseNone}, reason: reason,
	}); err != nil {
		return err
	}
	res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.intents SET state = 'FAILED', phase = NULL, reason = $2,
		failure_code = $3, failure_codespace = $4, final_at = clock_timestamp()
		WHERE intent_id = $1 AND state = $5 AND phase = $6`,
		intentID, reason, fcode, fspace, string(from.State), string(from.Phase))
	return oneRow(res, err, "fail intent")
}

// RetryWait is T13 or T14 ending in REQUESTED/RETRY_WAIT: the next attempt
// may start after delay (0 to 600 s) on the database clock; next_attempt_at
// is stored, so a restart keeps the schedule.
func (t *SignerTx) RetryWait(ctx context.Context, intentID string, from Expect, delay time.Duration, reason string) error {
	if from.State != StateInFlight && from.State != StateRequested {
		return fmt.Errorf("%w: RETRY_WAIT only from IN_FLIGHT or REQUESTED", ErrInvalid)
	}
	if delay < 0 || delay > maxBackoff || !codeRE.MatchString(reason) {
		return fmt.Errorf("%w: delay or reason", ErrInvalid)
	}
	cur, err := t.lockExpected(ctx, intentID, from)
	if err != nil {
		return err
	}
	if err := t.ss.s.recordTransition(ctx, t.tx, cur, transition{
		intentID: intentID, from: &from, to: Expect{StateRequested, PhaseRetryWait}, reason: reason,
	}); err != nil {
		return err
	}
	res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.intents SET state = 'REQUESTED', phase = 'RETRY_WAIT',
		reason = $2, next_attempt_at = clock_timestamp() + $3::bigint * interval '1 microsecond'
		WHERE intent_id = $1 AND state = $4 AND phase = $5`,
		intentID, reason, delay.Microseconds(), string(from.State), string(from.Phase))
	return oneRow(res, err, "retry wait")
}

// AwaitDeadline is T15: an IN_FLIGHT intent whose expires_at has passed on
// the database clock keeps its live attempt and moves to AWAITING_DEADLINE
// (reason EXPIRED_AWAITING_CHAIN); BeginSend refuses it from then on.
func (t *SignerTx) AwaitDeadline(ctx context.Context, intentID string, from Expect) error {
	if from.State != StateInFlight || from.Phase == PhaseAwaitingDeadline {
		return fmt.Errorf("%w: AWAITING_DEADLINE only from a sending IN_FLIGHT phase", ErrInvalid)
	}
	cur, err := t.lockExpected(ctx, intentID, from)
	if err != nil {
		return err
	}
	if cur.beforeExpiry {
		return fmt.Errorf("%w: not expired", ErrStateChanged)
	}
	if err := t.ss.s.recordTransition(ctx, t.tx, cur, transition{
		intentID: intentID, from: &from, to: Expect{StateInFlight, PhaseAwaitingDeadline}, reason: "EXPIRED_AWAITING_CHAIN",
	}); err != nil {
		return err
	}
	res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.intents SET phase = 'AWAITING_DEADLINE',
		reason = 'EXPIRED_AWAITING_CHAIN' WHERE intent_id = $1 AND state = 'IN_FLIGHT' AND phase = $2`,
		intentID, string(from.Phase))
	return oneRow(res, err, "await deadline")
}

// OpenBusinessAttempt is plan 8.6 step 5: a REQUESTED intent whose business
// id was taken by another payload gets a new random business id, bounded by
// Limits.BusinessAttempts (ErrBusinessAttemptCap at the bound). The previous
// business attempt keeps outcome (^[A-Z][A-Z0-9_]{0,39}$). It returns the new
// business id.
func (t *SignerTx) OpenBusinessAttempt(ctx context.Context, intentID string, from Expect, outcome string) (string, error) {
	if from.State != StateRequested || !codeRE.MatchString(outcome) {
		return "", fmt.Errorf("%w: business attempt", ErrInvalid)
	}
	cur, err := t.lockExpected(ctx, intentID, from)
	if err != nil {
		return "", err
	}
	if !cur.businessAttempt.Valid {
		return "", fmt.Errorf("%w: intent not validated", ErrStateChanged)
	}
	n := int(cur.businessAttempt.Int32)
	if n >= t.ss.s.lim.BusinessAttempts {
		return "", ErrBusinessAttemptCap
	}
	businessID, err := newBusinessID()
	if err != nil {
		return "", err
	}
	res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.business_attempts SET outcome = $3
		WHERE intent_id = $1 AND n = $2 AND outcome IS NULL`, intentID, n, outcome)
	if err := oneRow(res, err, "close business attempt"); err != nil {
		return "", err
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO broadcaster.business_attempts (intent_id, n, business_id)
		VALUES ($1, $2, $3)`, intentID, n+1, businessID); err != nil {
		return "", fmt.Errorf("store: insert business attempt: %w", mapErr(err))
	}
	obs, err := json.Marshal(map[string]any{"business_attempt": n + 1, "previous_outcome": outcome})
	if err != nil {
		return "", fmt.Errorf("store: observation: %w", err)
	}
	if err := t.ss.s.recordTransition(ctx, t.tx, cur, transition{
		intentID: intentID, from: &from, to: from, reason: "BUSINESS_ATTEMPT_OPENED", observation: obs,
	}); err != nil {
		return "", err
	}
	res, err = t.tx.ExecContext(ctx, `UPDATE broadcaster.intents SET business_id = $2, business_attempt = $3
		WHERE intent_id = $1 AND business_attempt = $4`, intentID, businessID, n+1, n)
	if err := oneRow(res, err, "set business attempt"); err != nil {
		return "", err
	}
	return businessID, nil
}

// RecordObservation appends an observation row for a non-terminal intent of
// the session's signer without changing its state: a row when reason (the
// observation class) differs from the intent's last transitions reason,
// otherwise only transitions_suppressed grows (m11).
func (t *SignerTx) RecordObservation(ctx context.Context, intentID, reason string, observation json.RawMessage) error {
	if !codeRE.MatchString(reason) {
		return fmt.Errorf("%w: reason", ErrInvalid)
	}
	cur, err := lockIntent(ctx, t.tx, intentID)
	if err != nil {
		return err
	}
	if cur.state == StateCommitted || cur.state == StateFailed {
		return fmt.Errorf("%w: intent is %s", ErrImmutable, cur.state)
	}
	if !bytes.Equal(cur.signerAddress, t.ss.address) {
		return fmt.Errorf("%w: intent belongs to another signer", ErrStateChanged)
	}
	if cur.lastReason.Valid && cur.lastReason.String == reason {
		res, err := t.tx.ExecContext(ctx, `UPDATE broadcaster.intents SET transitions_suppressed = transitions_suppressed + 1
			WHERE intent_id = $1`, intentID)
		return oneRow(res, err, "suppress observation")
	}
	return t.observe(ctx, cur, "", reason, observation)
}
