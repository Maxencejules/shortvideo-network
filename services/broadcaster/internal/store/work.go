package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Reads for the worker (plan section 8). Every one is bounded and returns
// typed rows; none changes anything.

// Work is everything the worker needs to validate, re-validate before
// signing (plan 6.3) and build an attempt for one intent.
type Work struct {
	ID              string
	CallerPrincipal string
	Kind            string
	Entry           string
	State           State
	Phase           Phase
	Validated       bool
	// Request is the stored request (the chain message protobuf, or the
	// forwarded transaction); nil before validation and after a Q4 clearing.
	Request []byte
	// RequestJSON is a SQL-path intent's request; AuthContext is the stored
	// authorization context. Both are nil after a Q4 clearing.
	RequestJSON     []byte
	AuthContext     []byte
	RequestDigest   []byte
	SignerAddress   []byte
	BusinessID      string
	BusinessAttempt int
	AttemptCount    int
	CreatedAt       time.Time
	ExpiresAt       time.Time
	NextAttemptAt   time.Time
	// BeforeExpiry is clock_timestamp() < expires_at on the database clock.
	BeforeExpiry bool
	// Object is nil before validation; Forward is set for a validated
	// FORWARD_PROVIDER_TX intent.
	Object  *ObjectRef
	Forward *ForwardRecord
}

// ForwardRecord is a stored forwarded_txs row.
type ForwardRecord struct {
	ForwardFields
	TxHash      []byte
	OuterSigner []byte
	RecordedAt  time.Time
}

// GetWork returns intent intentID for the worker, whatever its caller.
func (s *Store) GetWork(ctx context.Context, intentID string) (Work, error) {
	if !uuidRE.MatchString(intentID) {
		return Work{}, fmt.Errorf("%w: intent id", ErrInvalid)
	}
	var (
		w                     Work
		phase, bizID, objType sql.NullString
		objID, reqJSON, auth  sql.NullString
		bizN, objVer          sql.NullInt64
		fType, fProvider      sql.NullString
		fAcct, fSeq, fTimeout sql.NullInt64
		fGas                  sql.NullInt64
		fHash, fSigner        []byte
		fAt                   sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `SELECT i.intent_id, i.caller_principal, i.kind, i.entry, i.state, i.phase,
			i.validated, i.request, i.request_json::text, i.auth_context::text, i.request_digest, i.signer_address,
			i.business_id, i.business_attempt, i.attempt_count, i.created_at, i.expires_at, i.next_attempt_at,
			clock_timestamp() < i.expires_at, i.object_type, i.object_id, i.object_version,
			f.message_type, f.provider_id, f.account_number, f.sequence, f.timeout_height, f.gas, f.tx_hash,
			f.outer_signer, f.recorded_at
		FROM broadcaster.intents i LEFT JOIN broadcaster.forwarded_txs f ON f.intent_id = i.intent_id
		WHERE i.intent_id = $1`, intentID).Scan(&w.ID, &w.CallerPrincipal, &w.Kind, &w.Entry, &w.State, &phase,
		&w.Validated, &w.Request, &reqJSON, &auth, &w.RequestDigest, &w.SignerAddress, &bizID, &bizN, &w.AttemptCount,
		&w.CreatedAt, &w.ExpiresAt, &w.NextAttemptAt, &w.BeforeExpiry, &objType, &objID, &objVer,
		&fType, &fProvider, &fAcct, &fSeq, &fTimeout, &fGas, &fHash, &fSigner, &fAt)
	if err != nil {
		return Work{}, errNoRowsIsNotFound(err, "read work")
	}
	w.Phase = Phase(phase.String)
	w.BusinessID = bizID.String
	w.BusinessAttempt = int(bizN.Int64)
	if reqJSON.Valid {
		w.RequestJSON = []byte(reqJSON.String)
	}
	if auth.Valid {
		w.AuthContext = []byte(auth.String)
	}
	if objType.Valid {
		w.Object = &ObjectRef{Type: ObjectType(objType.String), ID: objID.String, Version: uint64(objVer.Int64)}
	}
	if fType.Valid {
		w.Forward = &ForwardRecord{
			ForwardFields: ForwardFields{MessageType: fType.String, ProviderID: fProvider.String, BusinessID: w.BusinessID,
				AccountNumber: uint64(fAcct.Int64), Sequence: uint64(fSeq.Int64), TimeoutHeight: uint64(fTimeout.Int64),
				Gas: uint64(fGas.Int64)},
			TxHash: fHash, OuterSigner: fSigner, RecordedAt: fAt.Time,
		}
	}
	return w, nil
}

// LiveAttempt returns the key signer's live attempt with its bytes, or
// ErrNotFound when it has none (plan 8.1: at most one per key signer).
func (s *Store) LiveAttempt(ctx context.Context, signer []byte) (AttemptTx, error) {
	if len(signer) != 20 {
		return AttemptTx{}, fmt.Errorf("%w: signer address", ErrInvalid)
	}
	atts, err := s.queryAttempts(ctx, true, `WHERE a.signer_address = $1 AND a.dead_at IS NULL
		AND a.committed_height IS NULL ORDER BY a.created_at LIMIT 2`, signer)
	switch {
	case err != nil:
		return AttemptTx{}, err
	case len(atts) == 0:
		return AttemptTx{}, ErrNotFound
	case len(atts) > 1:
		return AttemptTx{}, fmt.Errorf("%w: signer has more than one live attempt", ErrLiveAttempt)
	}
	return atts[0], nil
}

// AttemptByHash returns the attempt whose tx_hash is hash, of any signer and
// intent, or ErrNotFound: plan 8.5 and 8.6 ask whether a committing or
// sequence-using transaction is one of ours.
func (s *Store) AttemptByHash(ctx context.Context, hash [32]byte) (Attempt, error) {
	atts, err := s.queryAttempts(ctx, false, `WHERE a.tx_hash = $1`, hash[:])
	if err != nil {
		return Attempt{}, err
	}
	if len(atts) == 0 {
		return Attempt{}, ErrNotFound
	}
	return atts[0].Attempt, nil
}

// ExpiredRequested lists up to limit (1 to 1,000) REQUESTED intents of any
// signer or entry, validated or not, whose expires_at has passed on the
// database clock, oldest expiry first: the candidates of T4 (Expire).
func (s *Store) ExpiredRequested(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > maxListLimit {
		return nil, fmt.Errorf("%w: limit", ErrInvalid)
	}
	return s.listIDs(ctx, `SELECT intent_id FROM broadcaster.intents
		WHERE state = 'REQUESTED' AND expires_at <= clock_timestamp()
		ORDER BY expires_at, intent_id LIMIT $1`, limit)
}

// Signer is a stored signers row.
type Signer struct {
	Address []byte
	Class   SignerClass
	// KeyName and LockKey are set for a key signer.
	KeyName string
	LockKey *int32
	// AccountNumber and NextSequence are set once a key signer was synced.
	AccountNumber  *uint64
	NextSequence   *uint64
	ObservedHeight uint64
	HaltedReason   string
}

const signerColumns = `address, class, key_name, lock_key, account_number, next_sequence, observed_height, halted_reason`

func scanSigner(sc interface{ Scan(...any) error }) (Signer, error) {
	var (
		sg                Signer
		keyName, halted   sql.NullString
		lockKey           sql.NullInt32
		acct, next, obsHt sql.NullInt64
	)
	if err := sc.Scan(&sg.Address, &sg.Class, &keyName, &lockKey, &acct, &next, &obsHt, &halted); err != nil {
		return Signer{}, err
	}
	sg.KeyName = keyName.String
	sg.HaltedReason = halted.String
	sg.ObservedHeight = uint64(obsHt.Int64)
	if lockKey.Valid {
		sg.LockKey = &lockKey.Int32
	}
	if acct.Valid {
		a, n := uint64(acct.Int64), uint64(next.Int64)
		sg.AccountNumber, sg.NextSequence = &a, &n
	}
	return sg, nil
}

// GetSigner returns the signers row of address, or ErrNotFound.
func (s *Store) GetSigner(ctx context.Context, address []byte) (Signer, error) {
	if len(address) != 20 {
		return Signer{}, fmt.Errorf("%w: signer address", ErrInvalid)
	}
	sg, err := scanSigner(s.db.QueryRowContext(ctx, `SELECT `+signerColumns+` FROM broadcaster.signers WHERE address = $1`,
		address))
	if err != nil {
		return Signer{}, errNoRowsIsNotFound(err, "read signer")
	}
	return sg, nil
}

// KeySigners lists the SERVICE and BROADCASTER signers by key name (at most
// 64), so serve can compare the database with the signer process's keys at
// start.
func (s *Store) KeySigners(ctx context.Context) ([]Signer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+signerColumns+` FROM broadcaster.signers
		WHERE class IN ('SERVICE', 'BROADCASTER') ORDER BY key_name LIMIT 64`)
	if err != nil {
		return nil, fmt.Errorf("store: list signers: %w", mapErr(err))
	}
	defer rows.Close()
	var out []Signer
	for rows.Next() {
		sg, err := scanSigner(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan signer: %w", err)
		}
		out = append(out, sg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list signers: %w", err)
	}
	return out, nil
}
