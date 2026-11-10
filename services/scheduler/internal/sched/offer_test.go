package sched

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
)

func TestOfferBodyUsesManifestAndTerm(t *testing.T) {
	manifest := hex.EncodeToString(bytes32(0xab))
	sched := bytes32(0x11)[:20]
	body, err := offerBody(Package{
		ContentID: "c1", Version: 1, ManifestHex: manifest, TermsHex: hex.EncodeToString(bytes32(0x22)),
		Scheduler: sched, TermSeconds: 3600,
		Segments: []policy.Segment{{ID: "native-0", SHA256: hex.EncodeToString(bytes32(0x01)), ByteCount: 4}},
	}, "p1", "oc1p1", "lc1p1", "https://127.0.0.1:1/pkg/c1")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["manifest_digest_hex"] != manifest {
		t.Fatalf("manifest %v", doc["manifest_digest_hex"])
	}
	if doc["scheduler_hex"] != hex.EncodeToString(sched) {
		t.Fatalf("scheduler %v", doc["scheduler_hex"])
	}
	if doc["deadline_unix_seconds"] == doc["start_unix_seconds"] {
		t.Fatal("deadline did not move")
	}
}

func TestCommitOfferRefusesBeforeTheProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer srv.Close()
	s := New(Config{OfferCommitURL: srv.URL})
	if err := s.commitOffer(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("a failed commit was accepted")
	}
}

func bytes32(b byte) []byte {
	return []byte{b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b, b}
}
