package worker

import (
	"context"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
	"github.com/Maxencejules/shortvideo-network/services/observe/client"
)

func TestStartAttemptStopsAtTheRetryCap(t *testing.T) {
	base, token, stop, err := client.Listen(
		map[string]int64{client.KindRetries: 1},
		map[string]int64{client.KindRetries: 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	gate := ObserveGate(base, token)
	ctx := context.Background()
	if err := gate.Admit(ctx, "intent-held"); err != nil {
		t.Fatal(err)
	}
	if err := gate.Admit(ctx, "intent-held"); err != nil {
		t.Fatalf("replay consumed a second retry: %v", err)
	}
	s := &session{w: &Worker{cfg: Config{Retries: gate}}}
	_, err = s.startAttempt(ctx, store.Work{ID: "intent-next"}, []store.Attempt{{}}, nil)
	if err == nil || err.Error() != "admission refused" {
		t.Fatalf("second retry %v", err)
	}
}
