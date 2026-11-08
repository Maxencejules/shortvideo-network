package store

import (
	"errors"
	"testing"
	"time"
)

// TestDatabaseNowAdvances: DatabaseNow reads the database clock, which
// moves forward between two reads.
func TestDatabaseNowAdvances(t *testing.T) {
	e := newEnv(t)
	a, err := e.st.DatabaseNow(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	b, err := e.st.DatabaseNow(e.ctx)
	if err != nil || !b.After(a) {
		t.Fatalf("database clock %v then %v (%v)", a, b, err)
	}
}

// TestMaxTimeoutAt: the highest timeout height of the signer's attempts at
// one sequence, across intents; another sequence or signer has none.
func TestMaxTimeoutAt(t *testing.T) {
	e := newEnv(t)
	e.principal("pa", 8)
	s1, s2 := e.signer(1), e.signer(2)
	if _, ok, err := e.st.MaxTimeoutAt(e.ctx, s1, 0); err != nil || ok {
		t.Fatalf("no attempt yet: %v %v", ok, err)
	}
	if _, _, err := e.st.MaxTimeoutAt(e.ctx, s1[:19], 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short signer: %v", err)
	}
	if _, _, err := e.st.MaxTimeoutAt(e.ctx, s1, 1<<63); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sequence above the bigint range: %v", err)
	}
	first := e.enqueue("pa", "k1", s1, time.Hour)
	ss := e.lock(s1)
	defer ss.Close()
	attempt := e.persist(ss, first.IntentID, 0, 20)
	killAttempt(t, ss, first.IntentID, attempt, Expect{StateInFlight, PhaseSigned})
	if err := ss.SyncSequence(e.ctx, 7, 0, 2); err != nil {
		t.Fatal(err)
	}
	second := e.enqueue("pa", "k2", s1, time.Hour)
	e.persist(ss, second.IntentID, 0, 30)
	top, ok, err := e.st.MaxTimeoutAt(e.ctx, s1, 0)
	if err != nil || !ok || top != 30 {
		t.Fatalf("max timeout at sequence 0: %d %v %v", top, ok, err)
	}
	if _, ok, err := e.st.MaxTimeoutAt(e.ctx, s1, 1); err != nil || ok {
		t.Fatalf("sequence 1: %v %v", ok, err)
	}
	if _, ok, err := e.st.MaxTimeoutAt(e.ctx, s2, 0); err != nil || ok {
		t.Fatalf("another signer: %v %v", ok, err)
	}
}

// TestPendingOwnerConfirmations lists exactly the COMMITTED registrations
// whose owner row is still PENDING: not a REQUESTED one, not a confirmed
// one.
func TestPendingOwnerConfirmations(t *testing.T) {
	e := newEnv(t)
	e.principal("pa", 8)
	signer := e.signer(1)
	if _, err := e.st.PendingOwnerConfirmations(e.ctx, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("limit 0: %v", err)
	}
	if _, err := e.st.PendingOwnerConfirmations(e.ctx, 1001); !errors.Is(err, ErrInvalid) {
		t.Fatalf("limit 1001: %v", err)
	}
	committed, err := e.registerContent("pa", "reg-1", "content-a", "alice", signer)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := e.registerContent("pa", "reg-2", "content-b", "alice", signer)
	if err != nil {
		t.Fatal(err)
	}
	commitIntent(t, e, signer, committed.IntentID, 40)
	ids, err := e.st.PendingOwnerConfirmations(e.ctx, 10)
	if err != nil || len(ids) != 1 || ids[0] != committed.IntentID {
		t.Fatalf("pending confirmations %v %v, want only the committed one (not %s)", ids, err, requested.IntentID)
	}
	if err := e.st.ConfirmContentOwner(e.ctx, committed.IntentID, signer); err != nil {
		t.Fatal(err)
	}
	if ids, err := e.st.PendingOwnerConfirmations(e.ctx, 10); err != nil || len(ids) != 0 {
		t.Fatalf("after the confirmation: %v %v", ids, err)
	}
}
