package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// errNoRowsIsNotFound maps sql.ErrNoRows to ErrNotFound and wraps any other
// error.
func errNoRowsIsNotFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("store: %s: %w", what, mapErr(err))
}

// Intent is the status view of one intent (plan 5.8): states, digests,
// the validated object, the latest attempt's answer, the live attempt's
// landing height and, for a COMMITTED intent, its commitment. It carries no
// request bytes, no request JSON and no authorization context.
type Intent struct {
	ID                    string
	CallerPrincipal       string
	IdempotencyKey        string
	Kind                  string
	Entry                 string
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
	// RequestDigest is nil for an unvalidated SQL-path intent; SQLDigest is
	// nil for an HTTP-path intent.
	RequestDigest []byte
	SQLDigest     []byte
	// AuthContextDigest is the SHA-256 of the authorization context's jsonb
	// text: computed from the stored context, or the digest kept by a Q4
	// clearing.
	AuthContextDigest []byte
	// ClearedAt is set when a Q4 clearing removed the stored authorizations.
	ClearedAt *time.Time
	// Object and SignerAddress are nil before validation.
	Object        *ObjectRef
	SignerAddress []byte
	// LastOutcome and LastAck are the latest attempt's last send outcome
	// class and its last CheckTx code (an ACK or a rejection).
	LastOutcome string
	LastAck     *ChainCode
	// LandingHeight is the timeout height of the live attempt, the last
	// height at which it can still commit; 0 without a live attempt.
	LandingHeight uint64
	// Commitment is set for a COMMITTED intent.
	Commitment *CommitmentRecord
}

// CommitmentRecord is a stored commitments row (plan 9.5).
type CommitmentRecord struct {
	ChainID           string
	BusinessID        string
	Height            uint64
	BlockHash         []byte
	TxHash            []byte
	FirstCommitTxHash []byte
	TxIndex           uint32
	MsgIndex          uint32
	Code              uint32
	Codespace         string
	GasWanted         uint64
	GasUsed           uint64
	Events            []byte
	IdempotentReplay  bool
	CommittingSigner  []byte
	RecordedAt        time.Time
}

// intentSelect reads the Intent view; the caller appends WHERE, ORDER BY
// and LIMIT clauses over alias i.
const intentSelect = `SELECT i.intent_id, i.caller_principal, i.idempotency_key, i.kind, i.entry, i.state, i.phase,
		i.reason, i.failure_code, i.failure_codespace, i.business_id, i.attempt_count, i.created_at, i.expires_at,
		i.final_at, i.committed_tx_hash, i.own_transaction, i.transitions_recorded, i.transitions_suppressed,
		i.request_digest, i.sql_digest,
		COALESCE(i.auth_context_digest, sha256(convert_to(i.auth_context::text, 'UTF8'))),
		i.cleared_at, i.object_type, i.object_id, i.object_version, i.signer_address,
		la.last_outcome, la.ack_code, la.ack_codespace, lv.timeout_height,
		c.chain_id, c.business_id, c.height, c.block_hash, c.tx_hash, c.first_commit_tx_hash, c.tx_index,
		c.msg_index, c.code, c.codespace, c.gas_wanted, c.gas_used, c.events::text, c.idempotent_replay,
		c.committing_signer, c.recorded_at
	FROM broadcaster.intents i
	LEFT JOIN LATERAL (SELECT a.last_outcome, a.ack_code, a.ack_codespace FROM broadcaster.attempts a
		WHERE a.intent_id = i.intent_id ORDER BY a.n DESC LIMIT 1) la ON true
	LEFT JOIN LATERAL (SELECT a.timeout_height FROM broadcaster.attempts a
		WHERE a.intent_id = i.intent_id AND a.dead_at IS NULL AND a.committed_height IS NULL LIMIT 1) lv ON true
	LEFT JOIN broadcaster.commitments c ON c.intent_id = i.intent_id `

// scanIntent scans one intentSelect row.
func scanIntent(sc interface{ Scan(...any) error }) (Intent, error) {
	var (
		it                           Intent
		phase, reason, codespace     sql.NullString
		bizID, objType, objID        sql.NullString
		lastOutcome, ackSpace        sql.NullString
		code, objVer, ackCode, land  sql.NullInt64
		final, cleared               sql.NullTime
		own                          sql.NullBool
		cChain, cBiz, cSpace, cEvent sql.NullString
		cHeight, cIndex, cMsg, cCode sql.NullInt64
		cWanted, cUsed               sql.NullInt64
		cBlock, cTx, cFirst, cSigner []byte
		cReplay                      sql.NullBool
		cAt                          sql.NullTime
	)
	if err := sc.Scan(&it.ID, &it.CallerPrincipal, &it.IdempotencyKey, &it.Kind, &it.Entry, &it.State, &phase, &reason,
		&code, &codespace, &bizID, &it.AttemptCount, &it.CreatedAt, &it.ExpiresAt, &final, &it.CommittedTxHash, &own,
		&it.TransitionsRecorded, &it.TransitionsSuppressed, &it.RequestDigest, &it.SQLDigest, &it.AuthContextDigest,
		&cleared, &objType, &objID, &objVer, &it.SignerAddress, &lastOutcome, &ackCode, &ackSpace, &land,
		&cChain, &cBiz, &cHeight, &cBlock, &cTx, &cFirst, &cIndex, &cMsg, &cCode, &cSpace, &cWanted, &cUsed, &cEvent,
		&cReplay, &cSigner, &cAt); err != nil {
		return Intent{}, err
	}
	it.Phase = Phase(phase.String)
	it.Reason = reason.String
	it.BusinessID = bizID.String
	it.LastOutcome = lastOutcome.String
	it.LandingHeight = uint64(land.Int64)
	if code.Valid {
		it.Failure = &ChainCode{Code: uint32(code.Int64), Codespace: codespace.String}
	}
	if final.Valid {
		it.FinalAt = &final.Time
	}
	if cleared.Valid {
		it.ClearedAt = &cleared.Time
	}
	if own.Valid {
		it.OwnTransaction = &own.Bool
	}
	if objType.Valid {
		it.Object = &ObjectRef{Type: ObjectType(objType.String), ID: objID.String, Version: uint64(objVer.Int64)}
	}
	if ackCode.Valid {
		it.LastAck = &ChainCode{Code: uint32(ackCode.Int64), Codespace: ackSpace.String}
	}
	if cHeight.Valid {
		it.Commitment = &CommitmentRecord{ChainID: cChain.String, BusinessID: cBiz.String, Height: uint64(cHeight.Int64),
			BlockHash: cBlock, TxHash: cTx, FirstCommitTxHash: cFirst, TxIndex: uint32(cIndex.Int64),
			MsgIndex: uint32(cMsg.Int64), Code: uint32(cCode.Int64), Codespace: cSpace.String,
			GasWanted: uint64(cWanted.Int64), GasUsed: uint64(cUsed.Int64), Events: []byte(cEvent.String),
			IdempotentReplay: cReplay.Bool, CommittingSigner: cSigner, RecordedAt: cAt.Time}
	}
	return it, nil
}

// GetIntent returns the caller's own intent. Another caller's intent and an
// unknown id are both ErrNotFound, so ids cannot be probed (plan 5.7).
func (s *Store) GetIntent(ctx context.Context, callerPrincipal, intentID string) (Intent, error) {
	if !idRE.MatchString(callerPrincipal) || !uuidRE.MatchString(intentID) {
		return Intent{}, ErrNotFound
	}
	it, err := scanIntent(s.db.QueryRowContext(ctx, intentSelect+`WHERE i.intent_id = $1 AND i.caller_principal = $2`,
		intentID, callerPrincipal))
	if err != nil {
		return Intent{}, errNoRowsIsNotFound(err, "read intent")
	}
	return it, nil
}

// IntentFilter selects a page of a caller's intents (plan 5.2 GET
// /v1/intents).
type IntentFilter struct {
	// States keeps intents in these states; empty keeps every state.
	States []State
	// After is the id of the last intent of the previous page, empty for
	// the first page. It must be one of the caller's intents.
	After string
	// Limit is the page size, 1 to 100.
	Limit int
}

// ListIntents returns a page of the caller's own intents in creation order
// (created_at, then id). A full page means more may follow: pass the last
// id as the next After. An After that is not one of the caller's intents is
// ErrNotFound.
func (s *Store) ListIntents(ctx context.Context, callerPrincipal string, f IntentFilter) ([]Intent, error) {
	if !idRE.MatchString(callerPrincipal) || f.Limit < 1 || f.Limit > 100 {
		return nil, fmt.Errorf("%w: caller or limit", ErrInvalid)
	}
	states := []string{string(StateRequested), string(StateInFlight), string(StateCommitted), string(StateFailed)}
	if len(f.States) > 0 {
		states = states[:0]
		for _, st := range f.States {
			switch st {
			case StateRequested, StateInFlight, StateCommitted, StateFailed:
				states = append(states, string(st))
			default:
				return nil, fmt.Errorf("%w: state", ErrInvalid)
			}
		}
	}
	afterAt := time.Time{}
	afterID := "00000000-0000-0000-0000-000000000000"
	if f.After != "" {
		if !uuidRE.MatchString(f.After) {
			return nil, ErrNotFound
		}
		if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM broadcaster.intents
			WHERE intent_id = $1 AND caller_principal = $2`, f.After, callerPrincipal).Scan(&afterAt); err != nil {
			return nil, errNoRowsIsNotFound(err, "read page token")
		}
		afterID = f.After
	}
	rows, err := s.db.QueryContext(ctx, intentSelect+`WHERE i.caller_principal = $1 AND i.state = ANY($2::text[])
		AND (i.created_at, i.intent_id) > ($3::timestamptz, $4::uuid)
		ORDER BY i.created_at, i.intent_id LIMIT $5`,
		callerPrincipal, pq.Array(states), afterAt, afterID, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("store: list intents: %w", mapErr(err))
	}
	defer rows.Close()
	var out []Intent
	for rows.Next() {
		it, err := scanIntent(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan intent: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list intents: %w", err)
	}
	return out, nil
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
	ID            string
	IntentID      string
	N             int
	BusinessN     int
	SignerAddress []byte
	AccountNumber uint64
	Sequence      uint64
	TimeoutHeight uint64
	Gas           uint64
	TxHash        []byte
	// BytesCleared says a Q4 clearing removed the transaction bytes.
	BytesCleared    bool
	Phase           AttemptPhase
	SendsStarted    int
	SendsRecorded   int
	UnknownSends    int
	CacheAnswers    int
	CountedRefusals int
	Acked           bool
	LastOutcome     string
	// Ack is the CheckTx code of the last ACK or rejection recorded.
	Ack             *ChainCode
	DeadReason      string
	CommittedHeight uint64
	CommittedCode   *ChainCode
	AccSeqPresent   *bool
	WorkerInstance  string
}

// AttemptTx is an attempt with its stored transaction bytes, the bytes a
// rebroadcast sends unchanged.
type AttemptTx struct {
	Attempt
	TxBytes []byte
}

// attemptColumns are the columns scanAttempt reads, over alias a.
const attemptColumns = `a.attempt_id, a.intent_id, a.n, a.business_n, a.signer_address, a.account_number, a.sequence,
	a.timeout_height, a.gas, a.tx_hash, a.tx_bytes IS NULL, a.phase, a.sends_started, a.sends_recorded,
	a.unknown_sends, a.cache_answers, a.counted_refusals, a.acked, a.last_outcome, a.ack_code, a.ack_codespace,
	a.dead_reason, a.committed_height, a.committed_code, a.committed_codespace, a.acc_seq_present,
	a.worker_instance`

// scanAttempt scans attemptColumns, followed by tx_bytes when withBytes.
func scanAttempt(sc interface{ Scan(...any) error }, withBytes bool) (AttemptTx, error) {
	var (
		a                                      AttemptTx
		acct, seq, timeout, gas                int64
		lastOutcome, ackSpace, dead, codespace sql.NullString
		ackCode, height, code                  sql.NullInt64
		accSeq                                 sql.NullBool
	)
	dest := []any{&a.ID, &a.IntentID, &a.N, &a.BusinessN, &a.SignerAddress, &acct, &seq, &timeout, &gas, &a.TxHash,
		&a.BytesCleared, &a.Phase, &a.SendsStarted, &a.SendsRecorded, &a.UnknownSends, &a.CacheAnswers,
		&a.CountedRefusals, &a.Acked, &lastOutcome, &ackCode, &ackSpace, &dead, &height, &code, &codespace, &accSeq,
		&a.WorkerInstance}
	if withBytes {
		dest = append(dest, &a.TxBytes)
	}
	if err := sc.Scan(dest...); err != nil {
		return AttemptTx{}, err
	}
	a.AccountNumber, a.Sequence, a.TimeoutHeight, a.Gas = uint64(acct), uint64(seq), uint64(timeout), uint64(gas)
	a.LastOutcome = lastOutcome.String
	a.DeadReason = dead.String
	a.CommittedHeight = uint64(height.Int64)
	if ackCode.Valid {
		a.Ack = &ChainCode{Code: uint32(ackCode.Int64), Codespace: ackSpace.String}
	}
	if code.Valid {
		a.CommittedCode = &ChainCode{Code: uint32(code.Int64), Codespace: codespace.String}
	}
	if accSeq.Valid {
		a.AccSeqPresent = &accSeq.Bool
	}
	return a, nil
}

// queryAttempts runs SELECT attemptColumns [, tx_bytes] FROM attempts a
// followed by tail.
func (s *Store) queryAttempts(ctx context.Context, withBytes bool, tail string, args ...any) ([]AttemptTx, error) {
	cols := attemptColumns
	if withBytes {
		cols += `, a.tx_bytes`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM broadcaster.attempts a `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list attempts: %w", mapErr(err))
	}
	defer rows.Close()
	var out []AttemptTx
	for rows.Next() {
		a, err := scanAttempt(rows, withBytes)
		if err != nil {
			return nil, fmt.Errorf("store: scan attempt: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list attempts: %w", err)
	}
	return out, nil
}

// Attempts lists an intent's attempts in order (at most 32).
func (s *Store) Attempts(ctx context.Context, intentID string) ([]Attempt, error) {
	if !uuidRE.MatchString(intentID) {
		return nil, fmt.Errorf("%w: intent id", ErrInvalid)
	}
	atts, err := s.queryAttempts(ctx, false, `WHERE a.intent_id = $1 ORDER BY a.n LIMIT 32`, intentID)
	if err != nil {
		return nil, err
	}
	out := make([]Attempt, len(atts))
	for i, a := range atts {
		out[i] = a.Attempt
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
