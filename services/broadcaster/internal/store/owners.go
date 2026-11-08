package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Content ownership (goal 11 Q3 "Operator-registered intent keys
// (Recommended)", plan 5.7). A REGISTER_CONTENT intent's enqueue (or, on the
// SQL path, its validation) inserts a PENDING owner row derived from the
// intent row; the transaction that fails the intent makes it VOID;
// ConfirmContentOwner makes it EFFECTIVE once the intent is COMMITTED and
// the chain showed our signer as submitting host. Only an EFFECTIVE row of
// the same caller lets that caller withdraw the content or schedule offers
// for it; the policy and API packages decide, from ContentOwnerFor and
// CallerScheduledOffer.

// OwnerStatus is a content owner row's status.
type OwnerStatus string

// Owner statuses.
const (
	OwnerPending   OwnerStatus = "PENDING"
	OwnerEffective OwnerStatus = "EFFECTIVE"
	OwnerVoid      OwnerStatus = "VOID"
)

// ContentOwner is a content owner row.
type ContentOwner struct {
	ContentID       string
	ContentVersion  uint64
	CallerPrincipal string
	PrincipalID     string
	SignerAddress   []byte
	IntentID        string
	Status          OwnerStatus
	CreatedAt       time.Time
	StatusAt        time.Time
}

// ContentOwnerFor returns the non-VOID owner row of contentID when it is the
// caller's own (PENDING or EFFECTIVE). No row, and another caller's row, are
// both ErrNotFound, so a caller learns nothing about another caller's
// content (plan 5.7 uniform answer).
func (s *Store) ContentOwnerFor(ctx context.Context, callerPrincipal, contentID string) (ContentOwner, error) {
	if !idRE.MatchString(callerPrincipal) || !idRE.MatchString(contentID) {
		return ContentOwner{}, ErrNotFound
	}
	var (
		o   ContentOwner
		ver int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT content_id, content_version, caller_principal, principal_id,
			signer_address, intent_id, status, created_at, status_at
		FROM broadcaster.content_owners WHERE content_id = $1 AND status <> 'VOID' AND caller_principal = $2`,
		contentID, callerPrincipal).Scan(&o.ContentID, &ver, &o.CallerPrincipal, &o.PrincipalID, &o.SignerAddress,
		&o.IntentID, &o.Status, &o.CreatedAt, &o.StatusAt)
	if err != nil {
		return ContentOwner{}, errNoRowsIsNotFound(err, "read content owner")
	}
	o.ContentVersion = uint64(ver)
	return o, nil
}

// CallerScheduledOffer reports whether the caller owns an OFFER_LEASE intent
// for offerID that is COMMITTED with the intent's own service signer as the
// committing signer: the relay permission of plan 5.7 for a caller not bound
// to the provider.
func (s *Store) CallerScheduledOffer(ctx context.Context, callerPrincipal, offerID string) (bool, error) {
	if !idRE.MatchString(callerPrincipal) || !idRE.MatchString(offerID) {
		return false, fmt.Errorf("%w: caller or offer id", ErrInvalid)
	}
	var ok bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM broadcaster.intents i
			JOIN broadcaster.commitments c ON c.intent_id = i.intent_id
			WHERE i.validated AND i.kind = 'OFFER_LEASE' AND i.object_id = $2 AND i.object_type = 'OFFER'
			  AND i.caller_principal = $1 AND i.state = 'COMMITTED' AND c.committing_signer = i.signer_address)`,
		callerPrincipal, offerID).Scan(&ok); err != nil {
		return false, fmt.Errorf("store: read scheduled offer: %w", mapErr(err))
	}
	return ok, nil
}

// ConfirmContentOwner makes the PENDING owner row of REGISTER_CONTENT intent
// intentID EFFECTIVE (plan 5.7), given observedHost, the submitting host the
// worker read from Query/Content. It requires the intent to be COMMITTED
// with the row's signer as committing signer and observedHost to equal that
// signer. A row that is not PENDING or an intent that is not COMMITTED is
// ErrStateChanged; another host is ErrInvalid. Nothing is written on any
// error, so a stale read leaves the row PENDING for a later confirmation.
func (s *Store) ConfirmContentOwner(ctx context.Context, intentID string, observedHost []byte) error {
	if !uuidRE.MatchString(intentID) || len(observedHost) != 20 {
		return fmt.Errorf("%w: intent id or host", ErrInvalid)
	}
	tx, err := s.beginTx(ctx)
	if err != nil {
		return err
	}
	if err := confirmOwnerTx(ctx, tx, intentID, observedHost); err != nil {
		return rollback(tx, err)
	}
	return commit(tx)
}

func confirmOwnerTx(ctx context.Context, tx *sql.Tx, intentID string, observedHost []byte) error {
	var (
		status    OwnerStatus
		signer    []byte
		state     State
		committer []byte
	)
	err := tx.QueryRowContext(ctx, `SELECT o.status, o.signer_address, i.state, c.committing_signer
		FROM broadcaster.content_owners o JOIN broadcaster.intents i ON i.intent_id = o.intent_id
		LEFT JOIN broadcaster.commitments c ON c.intent_id = o.intent_id
		WHERE o.intent_id = $1 FOR UPDATE OF o`, intentID).Scan(&status, &signer, &state, &committer)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: lock content owner: %w", mapErr(err))
	}
	switch {
	case status != OwnerPending || state != StateCommitted:
		return fmt.Errorf("%w: owner row is %s, intent is %s", ErrStateChanged, status, state)
	case !bytes.Equal(observedHost, signer) || !bytes.Equal(committer, signer):
		return fmt.Errorf("%w: submitting host or committing signer is not the owner row's signer", ErrInvalid)
	}
	res, err := tx.ExecContext(ctx, `UPDATE broadcaster.content_owners SET status = 'EFFECTIVE',
		status_at = clock_timestamp() WHERE intent_id = $1 AND status = 'PENDING'`, intentID)
	return oneRow(res, err, "confirm content owner")
}
