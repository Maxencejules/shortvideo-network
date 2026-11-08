package store

import "context"

// IntentByIdempotencyKey returns the caller's own intent under an
// idempotency key, in the view GetIntent returns. The key is unique per
// caller (UNIQUE(caller_principal, idempotency_key)), so at most one row
// matches. An invalid principal or key, an unused key and another
// caller's key are all ErrNotFound. The API reads it before its policy
// checks, so a retry of an accepted request gets its intent back even
// after the chain state those checks read has changed.
func (s *Store) IntentByIdempotencyKey(ctx context.Context, callerPrincipal, idempotencyKey string) (Intent, error) {
	if !idRE.MatchString(callerPrincipal) || !idRE.MatchString(idempotencyKey) {
		return Intent{}, ErrNotFound
	}
	it, err := scanIntent(s.db.QueryRowContext(ctx, intentSelect+`WHERE i.caller_principal = $1 AND i.idempotency_key = $2`,
		callerPrincipal, idempotencyKey))
	if err != nil {
		return Intent{}, errNoRowsIsNotFound(err, "read intent by idempotency key")
	}
	return it, nil
}
