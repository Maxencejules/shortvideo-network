package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// businessIDPrefix starts every business id this package draws.
const businessIDPrefix = "bcast"

// lowerBase32 is RFC 4648 base32 in lowercase without padding, so a drawn
// business id matches the chain id pattern ^[a-z0-9][a-z0-9._-]{0,63}$.
var lowerBase32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newBusinessID draws "bcast" and 128 random bits in lowercase base32: 31
// characters (DR-11-07: random, unpredictable, reused by every attempt of
// one business attempt).
func newBusinessID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("store: random business id: %w", err)
	}
	return businessIDPrefix + lowerBase32.EncodeToString(b), nil
}

// Expect is the (state, phase) a compare-and-set update requires.
type Expect struct {
	State State
	Phase Phase
}

// EnqueueRequest is one HTTP-path intent, already validated by the caller of
// this package (plan 6.3).
type EnqueueRequest struct {
	CallerPrincipal string
	IdempotencyKey  string
	// Kind is one of the request kinds (KindRegisterContent, ...).
	Kind string
	// Request is the deterministic protobuf of the chain message, 1 to
	// 65,536 bytes; for KindForwardProvider it is the provider-signed
	// transaction, verbatim.
	Request       []byte
	RequestDigest [32]byte
	// AuthContext is a JSON object of at most Limits.AuthContextBytes. For
	// KindRegisterContent its "principal_id" becomes the content owner
	// principal.
	AuthContext json.RawMessage
	// SignerAddress is the 20-byte account whose sequence the transaction
	// uses: a registered key signer of the kind's class, or, for
	// KindForwardProvider, the provider account that signed the forwarded
	// transaction (registered here as a FORWARD_SOURCE signer).
	SignerAddress []byte
	// Object is the chain object the request acts on.
	Object ObjectRef
	// Forward is required for KindForwardProvider and refused otherwise.
	Forward *ForwardFields
	// ExpiresIn is within [Limits.MinLifetime, Limits.MaxLifetime].
	ExpiresIn time.Duration
}

// EnqueueResult reports the intent of an enqueue.
type EnqueueResult struct {
	IntentID string
	// Created is false when the same caller and idempotency key already had
	// an intent with the same request digest.
	Created    bool
	BusinessID string
}

// invalidRowStates are the SQLSTATEs with which PostgreSQL refuses an intent
// row built from caller input that passed checkAuthContext: a CHECK
// violation (23514; the auth context's jsonb text, which adds a space after
// every ':' and ',' and spells numbers in full, is measured by the column
// CHECK, not by its raw length), an invalid json token (22P02), an
// unsupported Unicode escape such as \u0000 (22P05) and a number outside
// the numeric range (22003).
var invalidRowStates = map[string]bool{"23514": true, "22P02": true, "22P05": true, "22003": true}

func (s *Store) checkAuthContext(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || len(raw) > s.lim.AuthContextBytes {
		return "", fmt.Errorf("%w: auth context size", ErrInvalid)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return "", fmt.Errorf("%w: auth context is not a JSON object", ErrInvalid)
	}
	return string(raw), nil
}

// EnqueueHTTP inserts an intent, its first business attempt, the content
// owner row of a REGISTER_CONTENT intent, the forwarded_txs row of a
// FORWARD_PROVIDER_TX intent and the first transitions row in one
// transaction, after taking the caller's row lock under the configured lock
// and statement timeouts, and notifies WorkChannel at commit. The same
// caller and idempotency key with the same digest returns the existing
// intent; with another digest it is ErrIdempotencyConflict. A full caller is
// ErrCapReached; a lock wait beyond the timeout is ErrLockTimeout; a content
// id that another non-VOID owner row holds is ErrContentUnavailable;
// forwarded bytes another intent tracks are ErrTxTracked; a signer of
// another class than the kind's is ErrSignerClass. Nothing is written on any
// error.
func (s *Store) EnqueueHTTP(ctx context.Context, r EnqueueRequest) (EnqueueResult, error) {
	switch {
	case !idRE.MatchString(r.CallerPrincipal):
		return EnqueueResult{}, fmt.Errorf("%w: caller principal", ErrInvalid)
	case !idRE.MatchString(r.IdempotencyKey):
		return EnqueueResult{}, fmt.Errorf("%w: idempotency key", ErrInvalid)
	case len(r.Request) == 0 || len(r.Request) > maxIntentBytes:
		return EnqueueResult{}, fmt.Errorf("%w: request size", ErrInvalid)
	case len(r.SignerAddress) != 20:
		return EnqueueResult{}, fmt.Errorf("%w: signer address", ErrInvalid)
	case r.ExpiresIn < s.lim.MinLifetime || r.ExpiresIn > s.lim.MaxLifetime:
		return EnqueueResult{}, fmt.Errorf("%w: expires_in outside the configured lifetime", ErrInvalid)
	}
	v, err := checkValidated(r.Kind, r.Request, r.Object, r.Forward)
	if err != nil {
		return EnqueueResult{}, err
	}
	authContext, err := s.checkAuthContext(r.AuthContext)
	if err != nil {
		return EnqueueResult{}, err
	}
	businessID, err := businessIDFor(v)
	if err != nil {
		return EnqueueResult{}, err
	}
	tx, err := s.beginTx(ctx)
	if err != nil {
		return EnqueueResult{}, err
	}
	res, err := s.enqueueHTTPTx(ctx, tx, r, v, authContext, businessID)
	if err != nil {
		return EnqueueResult{}, rollback(tx, err)
	}
	if err := commit(tx); err != nil {
		return EnqueueResult{}, fmt.Errorf("store: commit enqueue: %w", err)
	}
	return res, nil
}

// businessIDFor returns the business id of a validated intent: the
// forwarded message's own for a forward, else a fresh random one.
func businessIDFor(v validFields) (string, error) {
	if v.forward != nil {
		return v.forward.BusinessID, nil
	}
	return newBusinessID()
}

func (s *Store) enqueueHTTPTx(ctx context.Context, tx *sql.Tx, r EnqueueRequest, v validFields, authContext, businessID string) (EnqueueResult, error) {
	for name, d := range map[string]time.Duration{
		"lock_timeout":      s.lim.EnqueueLockTimeout,
		"statement_timeout": s.lim.EnqueueStatementTimeout,
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, true)`,
			name, strconv.FormatInt(d.Milliseconds(), 10)+"ms"); err != nil {
			return EnqueueResult{}, fmt.Errorf("store: set %s: %w", name, err)
		}
	}
	var openCap int
	err := tx.QueryRowContext(ctx, `UPDATE broadcaster.caller_limits SET enqueue_count = enqueue_count + 1
		WHERE principal = $1 RETURNING open_intent_cap`, r.CallerPrincipal).Scan(&openCap)
	if errors.Is(err, sql.ErrNoRows) {
		return EnqueueResult{}, ErrCallerUnknown
	}
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("store: caller lock: %w", mapErr(err))
	}

	var (
		existingID     string
		existingEntry  string
		existingDigest []byte
		existingBizID  sql.NullString
	)
	err = tx.QueryRowContext(ctx, `SELECT intent_id, entry, request_digest, business_id FROM broadcaster.intents
		WHERE caller_principal = $1 AND idempotency_key = $2`,
		r.CallerPrincipal, r.IdempotencyKey).Scan(&existingID, &existingEntry, &existingDigest, &existingBizID)
	switch {
	case err == nil:
		// A SQL-path intent's digest is of another form, so the same key
		// from the SQL path is never the same request.
		if existingEntry != "HTTP" || !bytes.Equal(existingDigest, r.RequestDigest[:]) {
			return EnqueueResult{}, ErrIdempotencyConflict
		}
		return EnqueueResult{IntentID: existingID, Created: false, BusinessID: existingBizID.String}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return EnqueueResult{}, fmt.Errorf("store: read idempotency key: %w", mapErr(err))
	}

	var open int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM broadcaster.intents
		WHERE caller_principal = $1 AND state IN ('REQUESTED', 'IN_FLIGHT')`, r.CallerPrincipal).Scan(&open); err != nil {
		return EnqueueResult{}, fmt.Errorf("store: count open intents: %w", mapErr(err))
	}
	if open >= openCap {
		return EnqueueResult{}, ErrCapReached
	}
	if v.forward != nil {
		if err := ensureForwardSource(ctx, tx, r.SignerAddress); err != nil {
			return EnqueueResult{}, err
		}
	}

	var intentID string
	err = tx.QueryRowContext(ctx, `WITH t AS (SELECT clock_timestamp() AS now)
		INSERT INTO broadcaster.intents (caller_principal, idempotency_key, kind, entry, request_digest, request,
			auth_context, validated, chain_id, signer_address, business_id, business_attempt, object_type,
			object_id, object_version, state, phase, created_at, expires_at, next_attempt_at, transitions_recorded)
		SELECT $1, $2, $3, 'HTTP', $4, $5, $6::jsonb, true, b.chain_id, $7, $8, 1, $9, $10, $11, 'REQUESTED', 'QUEUED',
			t.now, t.now + $12::bigint * interval '1 microsecond', t.now, 1
		FROM t, broadcaster.chain_binding b
		RETURNING intent_id`,
		r.CallerPrincipal, r.IdempotencyKey, r.Kind, r.RequestDigest[:], r.Request, authContext,
		r.SignerAddress, businessID, v.objectType, v.objectID, v.objectVersion,
		r.ExpiresIn.Microseconds()).Scan(&intentID)
	if errors.Is(err, sql.ErrNoRows) {
		return EnqueueResult{}, ErrChainUnbound
	}
	if invalidRowStates[SQLState(err)] {
		return EnqueueResult{}, fmt.Errorf("%w: intent row: %w", ErrInvalid, err)
	}
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("store: insert intent: %w", mapErr(err))
	}
	if err := insertValidatedRows(ctx, tx, intentID, r.Kind, 1, businessID, v); err != nil {
		return EnqueueResult{}, err
	}
	if err := s.insertTransition(ctx, tx, transition{
		intentID: intentID, to: Expect{StateRequested, PhaseQueued}, reason: "CREATED",
	}); err != nil {
		return EnqueueResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_notify($1, '')`, WorkChannel); err != nil {
		return EnqueueResult{}, fmt.Errorf("store: notify: %w", err)
	}
	return EnqueueResult{IntentID: intentID, Created: true, BusinessID: businessID}, nil
}

// ensureForwardSource registers address as a FORWARD_SOURCE signer unless it
// is registered; a key signer at that address makes the intent insert fail
// with ErrSignerClass.
func ensureForwardSource(ctx context.Context, tx *sql.Tx, address []byte) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO broadcaster.signers (address, class) VALUES ($1, 'FORWARD_SOURCE')
		ON CONFLICT (address) DO NOTHING`, address); err != nil {
		return fmt.Errorf("store: register forward source: %w", mapErr(err))
	}
	return nil
}

// insertValidatedRows writes what a validated intent needs next to its row:
// business attempt n with businessID (FORWARDED for a forward), the PENDING
// content owner row of a REGISTER_CONTENT intent, derived from the intent
// row (object id and version, caller, signer, and the authorization
// context's principal_id), and the forwarded_txs row of a forward.
func insertValidatedRows(ctx context.Context, tx *sql.Tx, intentID, kind string, n int, businessID string, v validFields) error {
	source := "DRAWN"
	if v.forward != nil {
		source = "FORWARDED"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO broadcaster.business_attempts (intent_id, n, business_id, source)
		VALUES ($1, $2, $3, $4)`, intentID, n, businessID, source); err != nil {
		return fmt.Errorf("store: insert business attempt: %w", mapErr(err))
	}
	if kind == KindRegisterContent {
		var id int64
		err := tx.QueryRowContext(ctx, `INSERT INTO broadcaster.content_owners (content_id, content_version,
				caller_principal, principal_id, signer_address, intent_id, status)
			SELECT i.object_id, i.object_version, i.caller_principal, i.auth_context ->> 'principal_id',
				i.signer_address, i.intent_id, 'PENDING'
			FROM broadcaster.intents i WHERE i.intent_id = $1
			ON CONFLICT (content_id) WHERE status <> 'VOID' DO NOTHING
			RETURNING id`, intentID).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrContentUnavailable
		case SQLState(err) == "23502" || SQLState(err) == "23514":
			return fmt.Errorf("%w: content owner principal: %w", ErrInvalid, err)
		case err != nil:
			return fmt.Errorf("store: insert content owner: %w", mapErr(err))
		}
	}
	if f := v.forward; f != nil {
		var got string
		err := tx.QueryRowContext(ctx, `INSERT INTO broadcaster.forwarded_txs (intent_id, tx_hash, message_type,
				outer_signer, provider_id, account_number, sequence, timeout_height, gas)
			SELECT i.intent_id, $2, $3, i.signer_address, $4, $5, $6, $7, $8
			FROM broadcaster.intents i WHERE i.intent_id = $1
			ON CONFLICT (tx_hash) DO NOTHING
			RETURNING intent_id`, intentID, f.txHash[:], f.MessageType, f.ProviderID, f.accountNumber, f.sequence,
			f.timeoutHeight, f.gas).Scan(&got)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTxTracked
		}
		if err != nil {
			return fmt.Errorf("store: insert forwarded transaction: %w", mapErr(err))
		}
	}
	return nil
}

// Validation is what the worker's full validation of a SQL-path intent
// produced; its fields mean what the same EnqueueRequest fields mean.
type Validation struct {
	Request       []byte
	RequestDigest [32]byte
	SignerAddress []byte
	Object        ObjectRef
	Forward       *ForwardFields
}

// MarkValidated stores the validation of a SQL-path intent that is still
// REQUESTED/QUEUED and unvalidated: its business id (drawn, or the forwarded
// message's), business attempt 1, the content owner row of a
// REGISTER_CONTENT intent, the forwarded_txs row of a forward and a
// transitions row. Another state is ErrStateChanged; a held content id is
// ErrContentUnavailable and forwarded bytes another intent tracks are
// ErrTxTracked, with nothing written (the worker then fails the intent with
// FailValidation).
func (s *Store) MarkValidated(ctx context.Context, intentID string, v Validation) error {
	if !uuidRE.MatchString(intentID) {
		return fmt.Errorf("%w: intent id", ErrInvalid)
	}
	if len(v.Request) == 0 || len(v.Request) > maxIntentBytes || len(v.SignerAddress) != 20 {
		return fmt.Errorf("%w: validation", ErrInvalid)
	}
	tx, err := s.beginTx(ctx)
	if err != nil {
		return err
	}
	if err := s.markValidatedTx(ctx, tx, intentID, v); err != nil {
		return rollback(tx, err)
	}
	return commit(tx)
}

func (s *Store) markValidatedTx(ctx context.Context, tx *sql.Tx, intentID string, v Validation) error {
	cur, err := lockIntent(ctx, tx, intentID)
	if err != nil {
		return err
	}
	if cur.state != StateRequested || cur.phase != PhaseQueued || cur.validated || cur.entry != "SQL" {
		return fmt.Errorf("%w: intent is not an unvalidated queued SQL-path intent", ErrStateChanged)
	}
	checked, err := checkValidated(cur.kind, v.Request, v.Object, v.Forward)
	if err != nil {
		return err
	}
	businessID, err := businessIDFor(checked)
	if err != nil {
		return err
	}
	if checked.forward != nil {
		if err := ensureForwardSource(ctx, tx, v.SignerAddress); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.intents SET validated = true, request = $2,
		request_digest = $3, signer_address = $4, business_id = $5, business_attempt = 1, object_type = $6,
		object_id = $7, object_version = $8
		WHERE intent_id = $1 AND state = 'REQUESTED' AND phase = 'QUEUED' AND NOT validated`,
		intentID, v.Request, v.RequestDigest[:], v.SignerAddress, businessID, checked.objectType, checked.objectID,
		checked.objectVersion)
	if err := oneRow(res, err, "mark validated"); err != nil {
		return err
	}
	if err := insertValidatedRows(ctx, tx, intentID, cur.kind, 1, businessID, checked); err != nil {
		return err
	}
	return s.recordTransition(ctx, tx, cur, transition{
		intentID: intentID, to: Expect{StateRequested, PhaseQueued}, reason: "VALIDATED",
	})
}

// lockedIntent is an intent row read under FOR UPDATE.
type lockedIntent struct {
	id              string
	kind            string
	class           SignerClass
	state           State
	phase           Phase
	validated       bool
	entry           string
	callerPrincipal string
	signerAddress   []byte
	businessID      sql.NullString
	businessAttempt sql.NullInt32
	attemptCount    int
	beforeExpiry    bool
	recorded        int
	lastReason      sql.NullString
}

// lockIntent reads the intent row with FOR UPDATE. Lock order everywhere is
// intent row, then signer row, then attempt rows.
func lockIntent(ctx context.Context, tx *sql.Tx, intentID string) (lockedIntent, error) {
	if !uuidRE.MatchString(intentID) {
		return lockedIntent{}, fmt.Errorf("%w: intent id", ErrInvalid)
	}
	var (
		li    lockedIntent
		phase sql.NullString
	)
	err := tx.QueryRowContext(ctx, `SELECT i.intent_id, i.kind, k.signer_class, i.state, i.phase, i.validated, i.entry,
			i.caller_principal, i.signer_address, i.business_id, i.business_attempt, i.attempt_count,
			clock_timestamp() < i.expires_at, i.transitions_recorded,
			(SELECT t.reason FROM broadcaster.transitions t WHERE t.intent_id = i.intent_id ORDER BY t.id DESC LIMIT 1)
		FROM broadcaster.intents i JOIN broadcaster.kinds k ON k.kind = i.kind
		WHERE i.intent_id = $1 FOR UPDATE OF i`, intentID).Scan(
		&li.id, &li.kind, &li.class, &li.state, &phase, &li.validated, &li.entry, &li.callerPrincipal,
		&li.signerAddress, &li.businessID, &li.businessAttempt, &li.attemptCount, &li.beforeExpiry, &li.recorded,
		&li.lastReason)
	if errors.Is(err, sql.ErrNoRows) {
		return lockedIntent{}, ErrNotFound
	}
	if err != nil {
		return lockedIntent{}, fmt.Errorf("store: lock intent: %w", mapErr(err))
	}
	li.phase = Phase(phase.String)
	return li, nil
}

// transition is one transitions row.
type transition struct {
	intentID    string
	attemptID   string
	from        *Expect
	to          Expect
	reason      string
	observation json.RawMessage
}

// recordTransition appends tr for the locked intent cur, or, once the intent
// has Limits.TransitionsPerIntent rows, only raises transitions_suppressed.
// It must run before an update that makes the intent terminal, because a
// terminal row never changes.
func (s *Store) recordTransition(ctx context.Context, tx *sql.Tx, cur lockedIntent, tr transition) error {
	if cur.recorded >= s.lim.TransitionsPerIntent {
		res, err := tx.ExecContext(ctx, `UPDATE broadcaster.intents SET transitions_suppressed = transitions_suppressed + 1
			WHERE intent_id = $1`, cur.id)
		return oneRow(res, err, "suppress transition")
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.intents SET transitions_recorded = transitions_recorded + 1
		WHERE intent_id = $1 AND transitions_recorded = $2`, cur.id, cur.recorded)
	if err := oneRow(res, err, "count transition"); err != nil {
		return err
	}
	return s.insertTransition(ctx, tx, tr)
}

// insertTransition writes the transitions row itself.
func (s *Store) insertTransition(ctx context.Context, tx *sql.Tx, tr transition) error {
	if !codeRE.MatchString(tr.reason) {
		return fmt.Errorf("%w: transition reason", ErrInvalid)
	}
	var obs sql.NullString
	if len(tr.observation) > 0 {
		if len(tr.observation) > s.lim.ObservationBytes || !json.Valid(tr.observation) {
			return fmt.Errorf("%w: observation", ErrInvalid)
		}
		obs = sql.NullString{String: string(tr.observation), Valid: true}
	}
	var fromState, fromPhase, toPhase, attempt sql.NullString
	if tr.from != nil {
		fromState = sql.NullString{String: string(tr.from.State), Valid: true}
		fromPhase = sql.NullString{String: string(tr.from.Phase), Valid: tr.from.Phase != PhaseNone}
	}
	toPhase = sql.NullString{String: string(tr.to.Phase), Valid: tr.to.Phase != PhaseNone}
	attempt = sql.NullString{String: tr.attemptID, Valid: tr.attemptID != ""}
	if _, err := tx.ExecContext(ctx, `INSERT INTO broadcaster.transitions (intent_id, attempt_id, from_state,
			from_phase, to_state, to_phase, reason, observation, worker_instance)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)`,
		tr.intentID, attempt, fromState, fromPhase, string(tr.to.State), toPhase, tr.reason, obs, s.instance); err != nil {
		return fmt.Errorf("store: insert transition: %w", mapErr(err))
	}
	return nil
}

// oneRow checks that an UPDATE changed exactly one row; zero rows is a lost
// compare-and-set.
func oneRow(res sql.Result, err error, what string) error {
	if err != nil {
		return fmt.Errorf("store: %s: %w", what, mapErr(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: %s: %w", what, err)
	}
	if n != 1 {
		return fmt.Errorf("%w: %s changed %d rows", ErrStateChanged, what, n)
	}
	return nil
}

// t2Reasons are the reasons of T2 (worker validation fails, plan 9.3).
var t2Reasons = map[string]bool{"UNAUTHORIZED": true, "INVALID": true, "PROVIDER_SIGNATURE_INVALID": true}

// FailValidation is T2: a REQUESTED intent whose validation failed becomes
// FAILED with reason UNAUTHORIZED, INVALID or PROVIDER_SIGNATURE_INVALID. A
// REQUESTED intent has no live attempt (checked at commit), so no
// transaction can still commit under the FAILED row.
func (s *Store) FailValidation(ctx context.Context, intentID, reason string) error {
	if !t2Reasons[reason] {
		return fmt.Errorf("%w: T2 reason", ErrInvalid)
	}
	return s.failRequested(ctx, intentID, func(cur lockedIntent) (string, error) {
		return reason, nil
	})
}

// Cancel is T3: the caller's own intent, while REQUESTED with no attempt
// ever persisted, becomes FAILED(CANCELLED). Another caller's intent is
// ErrNotFound; an intent with an attempt is ErrStateChanged (signed bytes
// cannot be unsent).
func (s *Store) Cancel(ctx context.Context, callerPrincipal, intentID string) error {
	return s.failRequested(ctx, intentID, func(cur lockedIntent) (string, error) {
		if cur.callerPrincipal != callerPrincipal {
			return "", ErrNotFound
		}
		if cur.attemptCount != 0 {
			return "", fmt.Errorf("%w: an attempt was persisted", ErrStateChanged)
		}
		return "CANCELLED", nil
	})
}

// Expire is T4: a REQUESTED intent whose expires_at has passed on the
// database clock becomes FAILED(EXPIRED_NOT_SENT) when no attempt was ever
// persisted, else FAILED(EXPIRED_DEAD). Before expiry it is ErrStateChanged.
func (s *Store) Expire(ctx context.Context, intentID string) error {
	return s.failRequested(ctx, intentID, func(cur lockedIntent) (string, error) {
		if cur.beforeExpiry {
			return "", fmt.Errorf("%w: not expired", ErrStateChanged)
		}
		if cur.attemptCount == 0 {
			return "EXPIRED_NOT_SENT", nil
		}
		return "EXPIRED_DEAD", nil
	})
}

// failRequested runs T2, T3 or T4 under the intent row lock: guard decides
// the reason or refuses; the update applies only WHERE state = 'REQUESTED'.
func (s *Store) failRequested(ctx context.Context, intentID string, guard func(lockedIntent) (string, error)) error {
	tx, err := s.beginTx(ctx)
	if err != nil {
		return err
	}
	if err := s.failRequestedTx(ctx, tx, intentID, guard); err != nil {
		return rollback(tx, err)
	}
	return commit(tx)
}

func (s *Store) failRequestedTx(ctx context.Context, tx *sql.Tx, intentID string, guard func(lockedIntent) (string, error)) error {
	cur, err := lockIntent(ctx, tx, intentID)
	if err != nil {
		return err
	}
	reason, err := guard(cur)
	if err != nil {
		return err
	}
	if cur.state != StateRequested {
		return fmt.Errorf("%w: intent is %s", ErrStateChanged, cur.state)
	}
	clear, obs, err := failingObservation(ctx, tx, intentID)
	if err != nil {
		return err
	}
	if err := s.recordTransition(ctx, tx, cur, transition{
		intentID: intentID, from: &Expect{cur.state, cur.phase}, to: Expect{StateFailed, PhaseNone}, reason: reason,
		observation: obs,
	}); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.intents SET state = 'FAILED', phase = NULL, reason = $2,
		final_at = clock_timestamp() WHERE intent_id = $1 AND state = 'REQUESTED'`, intentID, reason)
	if err := oneRow(res, err, "fail requested intent"); err != nil {
		return err
	}
	return settleFailed(ctx, tx, intentID, clear)
}
