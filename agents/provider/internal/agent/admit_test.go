package agent

import (
	"errors"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/config"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
	"github.com/Maxencejules/shortvideo-network/services/observe/client"
)

func TestStorageReservationStopsAtTheCap(t *testing.T) {
	base, token, stop, err := client.Listen(
		map[string]int64{client.KindStorage: 100},
		map[string]int64{client.KindStorage: 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	a := &Agent{
		cfg: config.File{
			DataDir: t.TempDir(), QuotaBytes: 10000, DeclaredCapacityBytes: 10000,
			BandwidthBytesPerSecond: 10000, TransferBudgetSeconds: 10,
		},
		st:      &store.State{Leases: map[string]*store.Lease{}, OfferLease: map[string]string{}},
		observe: client.New(base, token),
	}
	if err := a.reserve(60, "offer-1"); err != nil {
		t.Fatal(err)
	}
	if err := a.reserve(60, "offer-1"); err != nil {
		t.Fatalf("replay reserved twice: %v", err)
	}
	err = a.reserve(60, "offer-2")
	var got *Error
	if !errors.As(err, &got) || got.Code != "admission" {
		t.Fatalf("second reservation %v", err)
	}
	a.observe = client.New("http://127.0.0.1:1", token)
	err = a.reserve(10, "offer-3")
	if !errors.As(err, &got) || got.Code != "admission_unreachable" {
		t.Fatalf("down %v", err)
	}
}
