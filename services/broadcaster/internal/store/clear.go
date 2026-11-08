package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// The Q4 clearing step (goal 11 Q4 "Minimal + clear unsent (Recommended)";
// 0002_answers.pgsql clear_unsent). Every transition into FAILED runs it in
// the same transaction: failRequestedTx (T2, T3, T4) and SignerTx.FailIntent
// (T6, T7, T13, T14). When no attempt of the intent ever started a send
// (sends_started is 0 on every attempt, or there is no attempt), the
// intent's request (inner signatures, hosting intent, or forwarded
// transaction), SQL-path request JSON, authorization context and every
// attempt's transaction bytes are cleared, and request_digest, sql_digest,
// tx_hash and auth_context_digest are kept. The database refuses a commit
// that leaves such an intent uncleared (SV011), and the terminal-row
// triggers accept this clearing and no other change.

// failingObservation reports whether the intent locked in tx will be
// cleared when it becomes FAILED (no attempt of it started a send) and
// returns the observation of its FAILED transitions row, which says so.
func failingObservation(ctx context.Context, tx *sql.Tx, intentID string) (bool, json.RawMessage, error) {
	var started int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(sends_started), 0) FROM broadcaster.attempts
		WHERE intent_id = $1`, intentID).Scan(&started); err != nil {
		return false, nil, fmt.Errorf("store: count started sends: %w", mapErr(err))
	}
	if started > 0 {
		return false, nil, nil
	}
	obs, err := json.Marshal(map[string]any{"authorizations_cleared": true})
	if err != nil {
		return false, nil, fmt.Errorf("store: observation: %w", err)
	}
	return true, obs, nil
}

// settleFailed runs after the update that made intentID FAILED, in the same
// transaction: it voids the intent's PENDING content owner row (Q3) and runs
// the clearing step, which must clear exactly when wantClear says.
func settleFailed(ctx context.Context, tx *sql.Tx, intentID string, wantClear bool) error {
	if _, err := tx.ExecContext(ctx, `UPDATE broadcaster.content_owners SET status = 'VOID',
		status_at = clock_timestamp() WHERE intent_id = $1 AND status = 'PENDING'`, intentID); err != nil {
		return fmt.Errorf("store: void content owner: %w", mapErr(err))
	}
	var cleared bool
	if err := tx.QueryRowContext(ctx, `SELECT broadcaster.clear_unsent($1)`, intentID).Scan(&cleared); err != nil {
		return fmt.Errorf("store: clear unsent authorizations: %w", mapErr(err))
	}
	if cleared != wantClear {
		return fmt.Errorf("%w: clearing step disagrees with the started sends", ErrStateChanged)
	}
	return nil
}
