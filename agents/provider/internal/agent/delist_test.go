package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/config"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
)

func TestDelistedContentIsNotAdmitted(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{
		cfg: config.File{DataDir: dir},
		st:  &store.State{Leases: map[string]*store.Lease{}, OfferLease: map[string]string{}},
	}
	doc, err := a.Delist("c-harbor")
	if err != nil || doc["chain_end"] != false || doc["waited_for_finality"] != false {
		t.Fatal(err, doc)
	}
	_, err = a.Accept("offer1", []byte(`{"offer_id":"offer1","content_id":"c-harbor"}`))
	var got *Error
	if !errors.As(err, &got) || got.Status != 410 || got.Code != "delisted" {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil || !bytes.Contains(raw, []byte("c-harbor")) {
		t.Fatalf("%v %s", err, raw)
	}
}
