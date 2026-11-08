package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// EnsureSigner registers a signer address with its advisory lock key, or
// checks that the registered key is the same (ErrSignerConfig otherwise).
// The key class and key name of a signer are not stored here (goal 11 Q1).
func (s *Store) EnsureSigner(ctx context.Context, address []byte, lockKey int32) error {
	if len(address) != 20 || lockKey < 0 {
		return fmt.Errorf("%w: signer address or lock key", ErrInvalid)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO broadcaster.signers (address, lock_key) VALUES ($1, $2)
		ON CONFLICT (address) DO NOTHING`, address, lockKey); err != nil {
		return fmt.Errorf("store: register signer: %w", mapErr(err))
	}
	var got int32
	if err := s.db.QueryRowContext(ctx, `SELECT lock_key FROM broadcaster.signers WHERE address = $1`,
		address).Scan(&got); err != nil {
		return fmt.Errorf("store: read signer: %w", mapErr(err))
	}
	if got != lockKey {
		return ErrSignerConfig
	}
	return nil
}

// SignerSession is the one connection that drives a signer (plan 8.1). It
// holds the signer's session-level advisory lock; every attempt write for the
// signer runs on it, inside a transaction that first checks the lock is
// still held by this backend. If the connection dies or PostgreSQL restarts,
// the lock is gone and every later write fails, so nothing is persisted or
// marked as sent until a new session takes the lock.
//
// A session runs one transaction at a time and is not safe for concurrent
// use. While a SignerTx is open every other method returns ErrTxOpen instead
// of waiting for the connection.
type SignerSession struct {
	s       *Store
	conn    *sql.Conn
	address []byte
	key     int32
	open    *SignerTx
}

// LockSigner opens a dedicated connection and takes the signer's advisory
// lock with pg_try_advisory_lock. A lock held by another session is
// ErrSignerLockHeld; an unregistered signer is ErrNotFound.
func (s *Store) LockSigner(ctx context.Context, address []byte) (*SignerSession, error) {
	if len(address) != 20 {
		return nil, fmt.Errorf("%w: signer address", ErrInvalid)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: signer connection: %w", err)
	}
	var key int32
	err = conn.QueryRowContext(ctx, `SELECT lock_key FROM broadcaster.signers WHERE address = $1`, address).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Join(ErrNotFound, conn.Close())
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("store: read signer: %w", mapErr(err)), conn.Close())
	}
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`, lockClassSigner, key).Scan(&got); err != nil {
		return nil, errors.Join(fmt.Errorf("store: signer lock: %w", mapErr(err)), conn.Close())
	}
	if !got {
		return nil, errors.Join(ErrSignerLockHeld, conn.Close())
	}
	return &SignerSession{s: s, conn: conn, address: append([]byte(nil), address...), key: key}, nil
}

// Close rolls back a SignerTx left open, releases the advisory lock, and
// discards the connection instead of returning it to the pool: ending the
// database session releases the session-level lock even when the unlock
// fails, so no pooled connection keeps it. Close returns nil when the
// connection had already died.
func (ss *SignerSession) Close() error {
	var errs []error
	if ss.open != nil {
		if err := ss.open.Rollback(); err != nil && !errors.Is(err, sql.ErrConnDone) && !errors.Is(err, driver.ErrBadConn) {
			errs = append(errs, err)
		}
	}
	if _, err := ss.conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1, $2)`, lockClassSigner, ss.key); err != nil &&
		!errors.Is(err, sql.ErrConnDone) && !errors.Is(err, driver.ErrBadConn) {
		errs = append(errs, fmt.Errorf("store: signer unlock: %w", err))
	}
	// Returning driver.ErrBadConn from Raw makes database/sql close the
	// driver connection rather than pool it.
	if err := ss.conn.Raw(func(any) error { return driver.ErrBadConn }); err != nil &&
		!errors.Is(err, driver.ErrBadConn) && !errors.Is(err, sql.ErrConnDone) {
		errs = append(errs, fmt.Errorf("store: discard signer connection: %w", err))
	}
	if err := ss.conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
		errs = append(errs, fmt.Errorf("store: close signer connection: %w", err))
	}
	return errors.Join(errs...)
}

// begin starts a READ COMMITTED transaction on the session connection and
// checks that this backend still holds the signer lock.
func (ss *SignerSession) begin(ctx context.Context) (*sql.Tx, error) {
	if ss.open != nil {
		return nil, ErrTxOpen
	}
	tx, err := ss.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("store: begin on signer session: %w", err)
	}
	var held bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_locks
		WHERE locktype = 'advisory' AND pid = pg_backend_pid() AND granted
		  AND database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database())
		  AND classid = $1::bigint::oid AND objid = $2::bigint::oid AND objsubid = 2)`,
		lockClassSigner, ss.key).Scan(&held); err != nil {
		return nil, rollback(tx, fmt.Errorf("store: fence check: %w", err))
	}
	if !held {
		return nil, rollback(tx, ErrFenceLost)
	}
	return tx, nil
}

// AttemptDraft is a signed transaction the worker wants to persist (plan 8.3
// step 1). The signer is the session's.
type AttemptDraft struct {
	IntentID      string
	AccountNumber uint64
	Sequence      uint64
	TimeoutHeight uint64
	Gas           uint64
	TxBytes       []byte
	TxHash        [32]byte
}

// PersistRefusal says why PersistAttempt discarded a draft.
type PersistRefusal string

// Persist refusals. The worker discards the signed bytes; they were never
// sent, so the sequence is not used on chain.
const (
	RefusedNone            PersistRefusal = ""
	RefusedNotRequested    PersistRefusal = "NOT_REQUESTED"
	RefusedNotValidated    PersistRefusal = "NOT_VALIDATED"
	RefusedWrongSigner     PersistRefusal = "WRONG_SIGNER"
	RefusedExpired         PersistRefusal = "EXPIRED"
	RefusedAttemptCap      PersistRefusal = "ATTEMPT_CAP"
	RefusedBusinessIDCap   PersistRefusal = "BUSINESS_ID_ATTEMPT_CAP"
	RefusedStalled         PersistRefusal = "STALLED"
	RefusedSignerHalted    PersistRefusal = "SIGNER_HALTED"
	RefusedSequenceUnknown PersistRefusal = "SEQUENCE_UNKNOWN"
	RefusedAccountMismatch PersistRefusal = "ACCOUNT_MISMATCH"
	RefusedSequenceMoved   PersistRefusal = "SEQUENCE_MOVED"
	RefusedSignerBusy      PersistRefusal = "SIGNER_BUSY"
	RefusedTimeoutNotAbove PersistRefusal = "TIMEOUT_NOT_ABOVE_EARLIER"
)

// PersistResult is the outcome of PersistAttempt: an attempt id, or a
// refusal with nothing written.
type PersistResult struct {
	AttemptID string
	Refusal   PersistRefusal
}

// PersistAttempt is T8, plan 8.3 step 1, in one transaction: lock the intent
// row (REQUESTED/QUEUED or RETRY_WAIT, validated, this signer, before expiry
// on the database clock, below both attempt caps, no open stall episode),
// then the signer row (not halted, account number and next sequence equal to
// the draft's, no live attempt of the signer, timeout height strictly above
// every earlier attempt at this sequence), insert the attempt with phase
// SIGNED and sends_started 0, advance next_sequence, set the intent to
// IN_FLIGHT/SIGNED and append a transitions row. Any failed condition rolls
// back and returns a refusal.
func (ss *SignerSession) PersistAttempt(ctx context.Context, d AttemptDraft) (PersistResult, error) {
	if !uuidRE.MatchString(d.IntentID) {
		return PersistResult{}, fmt.Errorf("%w: intent id", ErrInvalid)
	}
	if len(d.TxBytes) == 0 || len(d.TxBytes) > maxIntentBytes || sha256.Sum256(d.TxBytes) != d.TxHash {
		return PersistResult{}, fmt.Errorf("%w: tx bytes or hash", ErrInvalid)
	}
	if d.Gas < 1 || d.Gas > maxGas || d.TimeoutHeight == 0 {
		return PersistResult{}, fmt.Errorf("%w: gas or timeout height", ErrInvalid)
	}
	acct, err := toInt64("account number", d.AccountNumber)
	if err != nil {
		return PersistResult{}, err
	}
	seq, err := toInt64("sequence", d.Sequence)
	if err != nil {
		return PersistResult{}, err
	}
	if seq == 1<<63-1 {
		return PersistResult{}, fmt.Errorf("%w: sequence has no successor", ErrInvalid)
	}
	timeout, err := toInt64("timeout height", d.TimeoutHeight)
	if err != nil {
		return PersistResult{}, err
	}
	tx, err := ss.begin(ctx)
	if err != nil {
		return PersistResult{}, err
	}
	res, err := ss.persistTx(ctx, tx, d, acct, seq, timeout)
	if err != nil || res.Refusal != RefusedNone {
		return res, rollback(tx, err)
	}
	if err := commit(tx); err != nil {
		return PersistResult{}, fmt.Errorf("store: commit attempt: %w", err)
	}
	return res, nil
}

func (ss *SignerSession) persistTx(ctx context.Context, tx *sql.Tx, d AttemptDraft, acct, seq, timeout int64) (PersistResult, error) {
	refuse := func(r PersistRefusal) (PersistResult, error) { return PersistResult{Refusal: r}, nil }
	cur, err := lockIntent(ctx, tx, d.IntentID)
	if err != nil {
		return PersistResult{}, err
	}
	switch {
	case cur.state != StateRequested || (cur.phase != PhaseQueued && cur.phase != PhaseRetryWait):
		return refuse(RefusedNotRequested)
	case !cur.validated:
		return refuse(RefusedNotValidated)
	case !bytes.Equal(cur.signerAddress, ss.address):
		return refuse(RefusedWrongSigner)
	case !cur.beforeExpiry:
		return refuse(RefusedExpired)
	case cur.attemptCount >= ss.s.lim.AttemptsPerIntent:
		return refuse(RefusedAttemptCap)
	}
	var underBusinessID int
	var stalled bool
	if err := tx.QueryRowContext(ctx, `SELECT
			(SELECT count(*) FROM broadcaster.attempts WHERE intent_id = $1 AND business_n = $2),
			EXISTS (SELECT 1 FROM broadcaster.stall_episodes WHERE ended_at IS NULL)`,
		d.IntentID, cur.businessAttempt.Int32).Scan(&underBusinessID, &stalled); err != nil {
		return PersistResult{}, fmt.Errorf("store: read attempt counts: %w", mapErr(err))
	}
	if underBusinessID >= ss.s.lim.AttemptsPerBusinessID {
		return refuse(RefusedBusinessIDCap)
	}
	if stalled {
		return refuse(RefusedStalled)
	}

	var (
		account, next sql.NullInt64
		halted        sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `SELECT account_number, next_sequence, halted_reason
		FROM broadcaster.signers WHERE address = $1 FOR UPDATE`, ss.address).Scan(&account, &next, &halted); err != nil {
		return PersistResult{}, fmt.Errorf("store: lock signer: %w", mapErr(err))
	}
	switch {
	case halted.Valid:
		return refuse(RefusedSignerHalted)
	case !account.Valid || !next.Valid:
		return refuse(RefusedSequenceUnknown)
	case account.Int64 != acct:
		return refuse(RefusedAccountMismatch)
	case next.Int64 != seq:
		return refuse(RefusedSequenceMoved)
	}
	var busy bool
	var maxTimeout sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT
			EXISTS (SELECT 1 FROM broadcaster.attempts WHERE signer_address = $1
			        AND dead_at IS NULL AND committed_height IS NULL),
			(SELECT max(timeout_height) FROM broadcaster.attempts WHERE signer_address = $1 AND sequence = $2)`,
		ss.address, seq).Scan(&busy, &maxTimeout); err != nil {
		return PersistResult{}, fmt.Errorf("store: read signer attempts: %w", mapErr(err))
	}
	if busy {
		return refuse(RefusedSignerBusy)
	}
	if maxTimeout.Valid && timeout <= maxTimeout.Int64 {
		return refuse(RefusedTimeoutNotAbove)
	}

	var attemptID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO broadcaster.attempts (intent_id, n, business_n, signer_address,
			account_number, sequence, timeout_height, gas, tx_bytes, tx_hash, phase, worker_instance)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'SIGNED', $11) RETURNING attempt_id`,
		d.IntentID, cur.attemptCount+1, cur.businessAttempt.Int32, ss.address, acct, seq, timeout, int64(d.Gas),
		d.TxBytes, d.TxHash[:], ss.s.instance).Scan(&attemptID); err != nil {
		return PersistResult{}, fmt.Errorf("store: insert attempt: %w", mapErr(err))
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.signers SET next_sequence = $2 + 1
		WHERE address = $1 AND next_sequence = $2`, ss.address, seq)
	if err := oneRow(res, err, "advance sequence"); err != nil {
		return PersistResult{}, err
	}
	from := Expect{cur.state, cur.phase}
	obs, err := json.Marshal(map[string]any{"sequence": seq, "timeout_height": timeout, "tx_hash": hex.EncodeToString(d.TxHash[:])})
	if err != nil {
		return PersistResult{}, fmt.Errorf("store: observation: %w", err)
	}
	if err := ss.s.recordTransition(ctx, tx, cur, transition{
		intentID: d.IntentID, attemptID: attemptID, from: &from, to: Expect{StateInFlight, PhaseSigned},
		reason: "ATTEMPT_PERSISTED", observation: obs,
	}); err != nil {
		return PersistResult{}, err
	}
	res, err = tx.ExecContext(ctx, `UPDATE broadcaster.intents SET state = 'IN_FLIGHT', phase = 'SIGNED',
		attempt_count = attempt_count + 1 WHERE intent_id = $1 AND state = 'REQUESTED' AND phase = $2`,
		d.IntentID, string(cur.phase))
	if err := oneRow(res, err, "set intent in flight"); err != nil {
		return PersistResult{}, err
	}
	return PersistResult{AttemptID: attemptID}, nil
}

// lockedAttempt is an attempt row read under FOR UPDATE.
type lockedAttempt struct {
	id            string
	phase         AttemptPhase
	txHash        []byte
	sendsStarted  int
	sendsRecorded int
	lastOutcome   sql.NullString
}

// lockAttempt locks the attempt's intent row, then the attempt row, which
// must belong to the session's signer.
func (ss *SignerSession) lockAttempt(ctx context.Context, tx *sql.Tx, attemptID string) (lockedIntent, lockedAttempt, error) {
	if !uuidRE.MatchString(attemptID) {
		return lockedIntent{}, lockedAttempt{}, fmt.Errorf("%w: attempt id", ErrInvalid)
	}
	var intentID string
	err := tx.QueryRowContext(ctx, `SELECT intent_id FROM broadcaster.attempts WHERE attempt_id = $1 AND signer_address = $2`,
		attemptID, ss.address).Scan(&intentID)
	if errors.Is(err, sql.ErrNoRows) {
		return lockedIntent{}, lockedAttempt{}, ErrNotFound
	}
	if err != nil {
		return lockedIntent{}, lockedAttempt{}, fmt.Errorf("store: find attempt: %w", mapErr(err))
	}
	cur, err := lockIntent(ctx, tx, intentID)
	if err != nil {
		return lockedIntent{}, lockedAttempt{}, err
	}
	var a lockedAttempt
	if err := tx.QueryRowContext(ctx, `SELECT attempt_id, phase, tx_hash, sends_started, sends_recorded, last_outcome
		FROM broadcaster.attempts WHERE attempt_id = $1 FOR UPDATE`, attemptID).Scan(
		&a.id, &a.phase, &a.txHash, &a.sendsStarted, &a.sendsRecorded, &a.lastOutcome); err != nil {
		return lockedIntent{}, lockedAttempt{}, fmt.Errorf("store: lock attempt: %w", mapErr(err))
	}
	return cur, a, nil
}

// BeginSend is plan 8.3 step 2: before every send of an attempt's bytes,
// count the send as started, committed on the session connection. It
// returns false, and the worker sends nothing, unless the attempt is live and
// its intent is IN_FLIGHT, not AWAITING_DEADLINE, and before expires_at on
// the database clock.
func (ss *SignerSession) BeginSend(ctx context.Context, attemptID string) (bool, error) {
	tx, err := ss.begin(ctx)
	if err != nil {
		return false, err
	}
	cur, a, err := ss.lockAttempt(ctx, tx, attemptID)
	if err != nil {
		return false, rollback(tx, err)
	}
	if !a.phase.Live() || cur.state != StateInFlight || cur.phase == PhaseAwaitingDeadline || !cur.beforeExpiry {
		return false, rollback(tx, nil)
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.attempts SET sends_started = sends_started + 1
		WHERE attempt_id = $1 AND sends_started = $2`, attemptID, a.sendsStarted)
	if err := oneRow(res, err, "count started send"); err != nil {
		return false, rollback(tx, err)
	}
	if err := commit(tx); err != nil {
		return false, fmt.Errorf("store: commit started send: %w", err)
	}
	return true, nil
}

// SendClass is the class of one broadcast answer (plan 8.4).
type SendClass string

// Send classes.
const (
	SendUnknown         SendClass = "UNKNOWN"
	SendCache           SendClass = "CACHE"
	SendNodeRefused     SendClass = "NODE_REFUSED"
	SendMempoolRejected SendClass = "MEMPOOL_REJECTED"
	SendAck             SendClass = "ACK"
)

// SendOutcome is a classified broadcast answer. CountsAsRefusal is the
// classifier's verdict for (d2); RecordSendOutcome refuses it unless it
// equals the rule of plan 8.4 (countsAsRefusal). Code and Codespace are the
// CheckTx result of MEMPOOL_REJECTED (a non-zero code with its codespace)
// and ACK (code 0, no codespace); every other class carries neither. Log is
// clipped to Limits.AckLogBytes.
type SendOutcome struct {
	Class           SendClass
	CountsAsRefusal bool
	Code            uint32
	Codespace       string
	Log             string
}

// countsAsRefusal is the (d2) rule of plan 8.4: a node refusal counts, and
// a CheckTx rejection counts unless it is sdk 32 (after a node restart
// emptied its cache, the bytes of a committed transaction get sdk 32).
// UNKNOWN, CACHE and ACK never count.
func countsAsRefusal(o SendOutcome) bool {
	switch o.Class {
	case SendNodeRefused:
		return true
	case SendMempoolRejected:
		return o.Codespace != "sdk" || o.Code != 32
	}
	return false
}

// RecordSendOutcome is plan 8.3 step 4: it records one started send's
// outcome. It raises sends_recorded and the counter of the class, raises the
// attempt phase to the highest knowledge seen (ACK: ACKNOWLEDGED; UNKNOWN or
// CACHE: UNCERTAIN; a refusal: unchanged) and mirrors it on the intent
// unless the intent is AWAITING_DEADLINE (T9, T10). A transitions row is
// appended when the intent phase or the outcome class changes; otherwise
// only transitions_suppressed grows. It returns false when the attempt is no
// longer live (its outcome no longer matters). Recording more outcomes than
// started sends is ErrStateChanged. An outcome whose code does not fit its
// class, or whose CountsAsRefusal differs from the (d2) rule, is ErrInvalid,
// so a classifier error cannot let an attempt die by (d2).
func (ss *SignerSession) RecordSendOutcome(ctx context.Context, attemptID string, o SendOutcome) (bool, error) {
	switch o.Class {
	case SendUnknown, SendCache, SendNodeRefused:
		if o.Code != 0 || o.Codespace != "" {
			return false, fmt.Errorf("%w: %s carries no CheckTx code", ErrInvalid, o.Class)
		}
	case SendAck:
		if o.Code != 0 || o.Codespace != "" {
			return false, fmt.Errorf("%w: ACK is CheckTx code 0", ErrInvalid)
		}
	case SendMempoolRejected:
		if o.Code == 0 || !printableRE.MatchString(o.Codespace) {
			return false, fmt.Errorf("%w: a CheckTx rejection has a non-zero code and a codespace", ErrInvalid)
		}
	default:
		return false, fmt.Errorf("%w: send class", ErrInvalid)
	}
	if o.CountsAsRefusal != countsAsRefusal(o) {
		return false, fmt.Errorf("%w: refusal flag differs from the (d2) rule for %s", ErrInvalid, o.Class)
	}
	tx, err := ss.begin(ctx)
	if err != nil {
		return false, err
	}
	ok, err := ss.recordSendTx(ctx, tx, attemptID, o)
	if err != nil || !ok {
		return false, rollback(tx, err)
	}
	if err := commit(tx); err != nil {
		return false, fmt.Errorf("store: commit send outcome: %w", err)
	}
	return true, nil
}

func (ss *SignerSession) recordSendTx(ctx context.Context, tx *sql.Tx, attemptID string, o SendOutcome) (bool, error) {
	cur, a, err := ss.lockAttempt(ctx, tx, attemptID)
	if err != nil {
		return false, err
	}
	if !a.phase.Live() {
		return false, nil
	}
	if a.sendsRecorded >= a.sendsStarted {
		return false, fmt.Errorf("%w: no started send left to record", ErrStateChanged)
	}
	newPhase := a.phase
	var unknown, cache, refusal int
	acked := false
	switch o.Class {
	case SendUnknown:
		unknown = 1
		newPhase = AttemptUncertain
	case SendCache:
		cache = 1
		newPhase = AttemptUncertain
	case SendAck:
		acked = true
		newPhase = AttemptAcknowledged
	}
	if o.CountsAsRefusal {
		refusal = 1
	}
	if newPhase.knowledge() < a.phase.knowledge() {
		newPhase = a.phase
	}
	var code sql.NullInt64
	var codespace, log sql.NullString
	if o.Class == SendMempoolRejected || o.Class == SendAck {
		code = sql.NullInt64{Int64: int64(o.Code), Valid: true}
		codespace = sql.NullString{String: o.Codespace, Valid: true}
		log = sql.NullString{String: clip(o.Log, ss.s.lim.AckLogBytes), Valid: true}
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.attempts SET sends_recorded = sends_recorded + 1,
			unknown_sends = unknown_sends + $3, cache_answers = cache_answers + $4,
			counted_refusals = counted_refusals + $5, acked = acked OR $6, phase = $7, last_outcome = $8,
			ack_code = COALESCE($9, ack_code), ack_codespace = COALESCE($10, ack_codespace),
			ack_log = COALESCE($11, ack_log)
		WHERE attempt_id = $1 AND sends_recorded = $2`,
		attemptID, a.sendsRecorded, unknown, cache, refusal, acked, string(newPhase), string(o.Class),
		code, codespace, log)
	if err := oneRow(res, err, "record send outcome"); err != nil {
		return false, err
	}

	newIntentPhase := cur.phase
	if cur.state == StateInFlight && cur.phase != PhaseAwaitingDeadline {
		newIntentPhase = Phase(newPhase)
	}
	classChanged := !a.lastOutcome.Valid || a.lastOutcome.String != string(o.Class)
	if newIntentPhase == cur.phase && !classChanged {
		res, err := tx.ExecContext(ctx, `UPDATE broadcaster.intents SET transitions_suppressed = transitions_suppressed + 1
			WHERE intent_id = $1`, cur.id)
		return true, oneRow(res, err, "suppress transition")
	}
	obs, err := json.Marshal(map[string]any{"class": o.Class, "code": o.Code, "codespace": o.Codespace})
	if err != nil {
		return false, fmt.Errorf("store: observation: %w", err)
	}
	from := Expect{cur.state, cur.phase}
	if err := ss.s.recordTransition(ctx, tx, cur, transition{
		intentID: cur.id, attemptID: attemptID, from: &from, to: Expect{cur.state, newIntentPhase},
		reason: "SEND_" + string(o.Class), observation: obs,
	}); err != nil {
		return false, err
	}
	if newIntentPhase != cur.phase {
		res, err := tx.ExecContext(ctx, `UPDATE broadcaster.intents SET phase = $2
			WHERE intent_id = $1 AND state = 'IN_FLIGHT' AND phase = $3`, cur.id, string(newIntentPhase), string(cur.phase))
		if err := oneRow(res, err, "raise intent phase"); err != nil {
			return false, err
		}
	}
	return true, nil
}

// SyncSequence stores the signer's committed account number and next
// sequence read from the chain at observedHeight (plan 8.1). It refuses
// while the signer has a live attempt (ErrSignerBusy) and when
// observedHeight is below the stored observed height (ErrStaleObservation:
// the node answered from older state).
func (ss *SignerSession) SyncSequence(ctx context.Context, accountNumber, nextSequence, observedHeight uint64) error {
	acct, err := toInt64("account number", accountNumber)
	if err != nil {
		return err
	}
	next, err := toInt64("sequence", nextSequence)
	if err != nil {
		return err
	}
	height, err := toInt64("height", observedHeight)
	if err != nil {
		return err
	}
	tx, err := ss.begin(ctx)
	if err != nil {
		return err
	}
	if err := ss.syncTx(ctx, tx, acct, next, height); err != nil {
		return rollback(tx, err)
	}
	return commit(tx)
}

func (ss *SignerSession) syncTx(ctx context.Context, tx *sql.Tx, acct, next, height int64) error {
	var stored int64
	if err := tx.QueryRowContext(ctx, `SELECT observed_height FROM broadcaster.signers WHERE address = $1 FOR UPDATE`,
		ss.address).Scan(&stored); err != nil {
		return fmt.Errorf("store: lock signer: %w", mapErr(err))
	}
	if height < stored {
		return ErrStaleObservation
	}
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM broadcaster.attempts WHERE signer_address = $1
		AND dead_at IS NULL AND committed_height IS NULL)`, ss.address).Scan(&busy); err != nil {
		return fmt.Errorf("store: read live attempts: %w", mapErr(err))
	}
	if busy {
		return ErrSignerBusy
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.signers SET account_number = $2, next_sequence = $3,
		observed_height = $4 WHERE address = $1`, ss.address, acct, next, height)
	return oneRow(res, err, "sync sequence")
}

// Halt stops the signer with reason (^[A-Z][A-Z0-9_]{0,39}$); PersistAttempt
// refuses a halted signer. A signer that is already halted keeps its first
// reason. SIGNER_CONFLICT is ErrInvalid here: only HaltForConflict sets it,
// together with the signer event that ClearConflict acknowledges.
func (ss *SignerSession) Halt(ctx context.Context, reason string) error {
	if !codeRE.MatchString(reason) || reason == haltSignerConflict {
		return fmt.Errorf("%w: halt reason", ErrInvalid)
	}
	tx, err := ss.begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE broadcaster.signers SET halted_reason = $2, halted_at = clock_timestamp()
		WHERE address = $1 AND halted_reason IS NULL`, ss.address, reason); err != nil {
		return rollback(tx, fmt.Errorf("store: halt signer: %w", mapErr(err)))
	}
	return commit(tx)
}

// haltSignerConflict is the halted_reason of a signer halted for a foreign
// use of its sequence.
const haltSignerConflict = "SIGNER_CONFLICT"

// HaltForConflict records a foreign use of the signer's sequence (a
// signer_events row) and halts the signer with SIGNER_CONFLICT (DR-11-15).
// Detecting an uncleared event again changes nothing. An event an operator
// already cleared with signer-clear is ErrConflictCleared and the signer is
// not halted: the worker treats that hash as an acknowledged foreign
// transaction (plan 8.1), and a halt that no uncleared event explains could
// never be lifted by ClearConflict.
func (ss *SignerSession) HaltForConflict(ctx context.Context, sequence uint64, foreignTxHash [32]byte) error {
	seq, err := toInt64("sequence", sequence)
	if err != nil {
		return err
	}
	tx, err := ss.begin(ctx)
	if err != nil {
		return err
	}
	var cleared bool
	err = tx.QueryRowContext(ctx, `INSERT INTO broadcaster.signer_events (signer_address, sequence, foreign_tx_hash)
		VALUES ($1, $2, $3) ON CONFLICT (signer_address, sequence, foreign_tx_hash) DO NOTHING RETURNING false`,
		ss.address, seq, foreignTxHash[:]).Scan(&cleared)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT cleared_at IS NOT NULL FROM broadcaster.signer_events
			WHERE signer_address = $1 AND sequence = $2 AND foreign_tx_hash = $3`,
			ss.address, seq, foreignTxHash[:]).Scan(&cleared)
	}
	if err != nil {
		return rollback(tx, fmt.Errorf("store: record signer event: %w", mapErr(err)))
	}
	if cleared {
		return rollback(tx, ErrConflictCleared)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE broadcaster.signers SET halted_reason = $2,
		halted_at = clock_timestamp() WHERE address = $1 AND halted_reason IS NULL`, ss.address, haltSignerConflict); err != nil {
		return rollback(tx, fmt.Errorf("store: halt signer: %w", mapErr(err)))
	}
	return commit(tx)
}

// ClearConflict records the operator's acknowledgement of one signer event
// (the signer-clear command, named in command) and, when the signer was
// halted for SIGNER_CONFLICT and no uncleared event remains, lifts the halt.
// An unknown or already cleared event is ErrNotFound.
func (ss *SignerSession) ClearConflict(ctx context.Context, sequence uint64, foreignTxHash [32]byte, command string) error {
	if !commandRE.MatchString(command) {
		return fmt.Errorf("%w: command", ErrInvalid)
	}
	seq, err := toInt64("sequence", sequence)
	if err != nil {
		return err
	}
	tx, err := ss.begin(ctx)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.signer_events SET cleared_at = clock_timestamp(),
		cleared_by_command = $4 WHERE signer_address = $1 AND sequence = $2 AND foreign_tx_hash = $3
		AND cleared_at IS NULL`, ss.address, seq, foreignTxHash[:], command)
	if err := oneRow(res, err, "clear signer event"); err != nil {
		if errors.Is(err, ErrStateChanged) {
			err = ErrNotFound
		}
		return rollback(tx, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE broadcaster.signers SET halted_reason = NULL, halted_at = NULL
		WHERE address = $1 AND halted_reason = $2 AND NOT EXISTS (
			SELECT 1 FROM broadcaster.signer_events WHERE signer_address = $1 AND cleared_at IS NULL)`,
		ss.address, haltSignerConflict); err != nil {
		return rollback(tx, fmt.Errorf("store: lift halt: %w", mapErr(err)))
	}
	return commit(tx)
}
