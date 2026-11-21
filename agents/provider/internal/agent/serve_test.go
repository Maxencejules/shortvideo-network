package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/config"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
)

func TestRemovedLeaseDoesNotHideTheServingCopy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "packages", "l2"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "l2", "seg"), []byte("clip"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Unix() + 3600
	a := &Agent{
		cfg: config.File{DataDir: dir},
		ctx: context.Background(),
		st: &store.State{
			Leases: map[string]*store.Lease{
				"l1": {
					LeaseID: "l1", OfferID: "o1", ContentID: "c1", ContentVersion: 1,
					Readiness: store.Removed, Deadline: deadline,
				},
				"l2": {
					LeaseID: "l2", OfferID: "o2", ContentID: "c1", ContentVersion: 1,
					Readiness: store.Serving, ServeToken: "tok", Deadline: deadline,
					Segments: []string{"seg"},
				},
			},
			OfferLease: map[string]string{},
		},
	}
	for i := 0; i < 40; i++ {
		got, err := a.Package("c1", "1", "tok")
		if err != nil || string(got) != "clip" {
			t.Fatalf("serving copy %d %q %v", i, got, err)
		}
		_, err = a.Package("c1", "1", "other")
		var denied *Error
		if !errors.As(err, &denied) || denied.Status != 401 {
			t.Fatalf("second requester %d %v", i, err)
		}
	}
}
