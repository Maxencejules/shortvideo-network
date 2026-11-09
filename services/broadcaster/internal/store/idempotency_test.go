package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// TestIntentByIdempotencyKey checks the caller-scoped read by idempotency
// key: it returns the same view as GetIntent for the caller's own key, an
// SQL-path intent under its key, and ErrNotFound for another caller's key,
// an unused key and malformed arguments.
func TestIntentByIdempotencyKey(t *testing.T) {
	e := newEnv(t)
	e.principal("a", 16)
	e.principal("b", 16)
	signer := e.signer(1)
	own := e.enqueue("a", "idem-1", signer, time.Hour)
	theirs := e.enqueue("b", "idem-1", signer, time.Hour)

	got, err := e.st.IntentByIdempotencyKey(e.ctx, "a", "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	want, err := e.st.GetIntent(e.ctx, "a", own.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("view by key %+v differs from GetIntent %+v", got, want)
	}
	if got, err := e.st.IntentByIdempotencyKey(e.ctx, "b", "idem-1"); err != nil || got.ID != theirs.IntentID ||
		got.CallerPrincipal != "b" {
		t.Fatalf("caller b's key: %+v %v", got, err)
	}

	callerDB := e.caller("s", 8)
	var sqlID string
	if err := callerDB.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.enqueue_intent($1, $2::jsonb, $3::jsonb,
		$4, $5::interval)`, KindOfferLease, `{"a":1}`, `{"principal_id":"p1"}`, "sql-1", "1 hour").Scan(&sqlID); err != nil {
		t.Fatal(err)
	}
	if got, err := e.st.IntentByIdempotencyKey(e.ctx, "s", "sql-1"); err != nil || got.ID != sqlID || got.Entry != "SQL" ||
		got.RequestDigest != nil || len(got.SQLDigest) != 32 {
		t.Fatalf("SQL-path intent by key: %+v %v", got, err)
	}

	for _, c := range []struct{ caller, key string }{
		{"a", "unused"},
		{"c", "idem-1"},
		{"a", "sql-1"},
		{"A", "idem-1"},
		{"a", "Idem-1"},
		{"a", ""},
		{"", "idem-1"},
		{"a", "idem-1' OR '1'='1"},
	} {
		if _, err := e.st.IntentByIdempotencyKey(e.ctx, c.caller, c.key); !errors.Is(err, ErrNotFound) {
			t.Errorf("caller %q key %q: want ErrNotFound, got %v", c.caller, c.key, err)
		}
	}
}
