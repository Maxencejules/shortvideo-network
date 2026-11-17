package sched

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
)

func TestLocalDelistDoesNotWaitForAStalledChain(t *testing.T) {
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/status":
			_, _ = w.Write([]byte(`{"stale":true,"stale_reasons":["stall"],"chain_id":"dev"}`))
		case "/v1/leases":
			_, _ = w.Write([]byte(`{"leases":[{"ID":"l1","ProviderID":"p1","ContentID":"c1","Version":1,"State":"ACTIVE","Deadline":10,"Active":true}]}`))
		case "/v1/content":
			_, _ = w.Write([]byte(`{"content":[{"ID":"c1","Version":1,"Withdrawn":false}]}`))
		default:
			_, _ = w.Write([]byte(`{"providers":[]}`))
		}
	}))
	defer indexer.Close()
	s := New(Config{
		IndexerURL: indexer.URL, NoticeSeconds: 100, Target: 1,
		Now:     func() time.Time { return time.Unix(50, 0) },
		Control: func(*http.Request) bool { return true },
	})
	pkg := Package{ContentID: "c1", Version: 1}
	view, err := s.Publish(context.Background(), pkg, true, "approval")
	if err != nil {
		t.Fatal(err)
	}
	if view.Publication != policy.QueuedNotCommitted {
		t.Fatalf("stall %s %s", view.Publication, view.PublicationReason)
	}
	if err := s.DelistLocal(context.Background(), "c1", 1); err != nil {
		t.Fatal(err)
	}
	view, err = s.Publish(context.Background(), pkg, true, "approval")
	if err != nil || view.Publication != policy.PublicationWithheld || view.PublicationReason != "local_delist" || view.Renewal != policy.RenewalNone {
		t.Fatalf("publish after delist %+v %v", view, err)
	}
	if len(s.Posted()) != 0 {
		t.Fatal(s.Posted())
	}
	job, err := s.Repair(context.Background(), "c1", 1)
	if err != nil || job.State != policy.JobWithdrawn {
		t.Fatalf("repair %+v %v", job, err)
	}
	if len(s.Posted()) != 0 {
		t.Fatal("repair posted an offer")
	}
	if code, body := s.Distribute(context.Background(), "c1", 1); code != http.StatusGone || len(body) != 0 {
		t.Fatalf("distribute %d %d", code, len(body))
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/delist", strings.NewReader(`{"content_id":"c1","version":1}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"waited_for_finality":false`) || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("http %d %s", rec.Code, rec.Body.String())
	}
}
