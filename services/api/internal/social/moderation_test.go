package social

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/moderation"
)

func TestModerationQueueBlocksAppealsAndTakedown(t *testing.T) {
	b := newBox(t, Options{Dev: true})
	viewer := b.issue("viewer1")
	creator := b.issue("creator1")
	stranger := b.issue("stranger1")
	moderator := b.issue("moderator1")
	b.video(creator, "c-harbor", "harbor", 20, []string{"boats"}, true)
	if code, raw := b.call("POST", "/v1/dev/moderator", "", map[string]any{"account_id": "moderator1"}, nil); code != http.StatusOK || bytes.Contains(raw, []byte("complaint")) {
		t.Fatalf("grant %d %s", code, raw)
	}

	code, report := b.call("POST", "/v1/reports", viewer, map[string]any{
		"target_type": "content", "target_id": "c-harbor", "category": "other", "narrative": "private complaint",
	}, nil)
	if code != http.StatusCreated || bytes.Contains(report, []byte("complaint")) {
		t.Fatalf("report %d %s", code, report)
	}
	var filed map[string]any
	if err := json.Unmarshal(report, &filed); err != nil {
		t.Fatal(err)
	}
	caseID, _ := filed["case_id"].(string)
	if caseID == "" {
		t.Fatal(report)
	}
	code, again := b.call("POST", "/v1/reports", viewer, map[string]any{
		"target_type": "content", "target_id": "c-harbor", "category": "spam", "narrative": "private complaint",
	}, nil)
	if code != http.StatusOK || !bytes.Contains(again, []byte(`"status":"spam"`)) || bytes.Contains(again, []byte("complaint")) {
		t.Fatalf("spam %d %s", code, again)
	}
	if code, raw := b.call("GET", "/v1/moderation/queue", stranger, nil, nil); code != http.StatusForbidden || bytes.Contains(raw, []byte("complaint")) {
		t.Fatalf("queue stranger %d %s", code, raw)
	}
	if code, raw := b.call("GET", "/v1/moderation/cases/"+caseID, stranger, nil, nil); code != http.StatusForbidden || bytes.Contains(raw, []byte("complaint")) || bytes.Contains(raw, []byte("viewer1")) {
		t.Fatalf("case stranger %d %s", code, raw)
	}
	if code, raw := b.call("GET", "/v1/moderation/cases/"+caseID, viewer, nil, nil); code != http.StatusOK || bytes.Contains(raw, []byte("complaint")) || bytes.Contains(raw, []byte("complainant")) {
		t.Fatalf("case reporter %d %s", code, raw)
	}
	if code, raw := b.call("POST", "/v1/moderation/cases/"+caseID+"/decision", stranger, map[string]any{"outcome": "remove", "reason_code": "placeholder"}, nil); code != http.StatusForbidden || bytes.Contains(raw, []byte("complaint")) {
		t.Fatalf("decide stranger %d %s", code, raw)
	}
	code, decided := b.call("POST", "/v1/moderation/cases/"+caseID+"/decision", moderator, map[string]any{"outcome": "remove", "reason_code": "placeholder"}, nil)
	if code != http.StatusOK || bytes.Contains(decided, []byte("complaint")) || !bytes.Contains(decided, []byte(`"narrative_retained":false`)) {
		t.Fatalf("decide %d %s", code, decided)
	}
	if code, feed := b.call("GET", "/v1/feeds/recommended", viewer, nil, nil); code != http.StatusOK || bytes.Contains(feed, []byte("c-harbor")) {
		t.Fatalf("feed %d %s", code, feed)
	}
	code, appeal := b.call("POST", "/v1/moderation/cases/"+caseID+"/appeal", creator, map[string]any{"statement": "please review"}, nil)
	if code != http.StatusCreated || bytes.Contains(appeal, []byte("please review")) {
		t.Fatalf("appeal %d %s", code, appeal)
	}
	if code, raw := b.call("POST", "/v1/moderation/cases/"+caseID+"/escalate", stranger, map[string]any{"note": "secret note"}, nil); code != http.StatusForbidden || bytes.Contains(raw, []byte("secret note")) {
		t.Fatalf("escalate %d %s", code, raw)
	}
	if code, raw := b.call("POST", "/v1/moderation/cases/"+caseID+"/escalate", creator, map[string]any{"note": "secret note"}, nil); code != http.StatusOK || !bytes.Contains(raw, []byte(`"escalation_status":"requested"`)) || bytes.Contains(raw, []byte("secret note")) {
		t.Fatalf("requested %d %s", code, raw)
	}
	if code, raw := b.call("POST", "/v1/moderation/cases/"+caseID+"/escalate", moderator, map[string]any{"note": "secret note"}, nil); code != http.StatusOK || !bytes.Contains(raw, []byte(`"escalation_status":"escalated"`)) || bytes.Contains(raw, []byte("secret note")) {
		t.Fatalf("escalated %d %s", code, raw)
	}
	if code, raw := b.call("POST", "/v1/moderation/cases/"+caseID+"/decision", moderator, map[string]any{"outcome": "reversed", "reason_code": "placeholder"}, nil); code != http.StatusOK || !bytes.Contains(raw, []byte(`"appeal_status":"reversed"`)) {
		t.Fatalf("reverse %d %s", code, raw)
	}
	if code, feed := b.call("GET", "/v1/feeds/recommended", viewer, nil, nil); code != http.StatusOK || !bytes.Contains(feed, []byte("c-harbor")) {
		t.Fatalf("restored %d %s", code, feed)
	}
	if code, raw := b.call("GET", "/v1/moderation/audit", viewer, nil, nil); code != http.StatusForbidden {
		t.Fatalf("audit viewer %d %s", code, raw)
	}
	if code, raw := b.call("GET", "/v1/moderation/audit", moderator, nil, nil); code != http.StatusOK || bytes.Contains(raw, []byte("complaint")) || bytes.Contains(raw, []byte("please review")) || !bytes.Contains(raw, []byte("decision")) {
		t.Fatalf("audit %d %s", code, raw)
	}

	b.video(viewer, "c-view", "dock", 15, nil, true)
	if code, raw := b.call("POST", "/v1/blocks", viewer, map[string]any{"account_id": "creator1"}, nil); code != http.StatusCreated {
		t.Fatalf("block %d %s", code, raw)
	}
	if code, raw := b.call("POST", "/v1/comments", creator, map[string]any{"content_id": "c-view", "text": "hello"}, map[string]string{"Idempotency-Key": "m-block"}); code != http.StatusForbidden || !bytes.Contains(raw, []byte("BLOCKED")) {
		t.Fatalf("comment %d %s", code, raw)
	}
	if code, raw := b.call("POST", "/v1/follows", creator, map[string]any{"account_id": "viewer1"}, nil); code != http.StatusForbidden {
		t.Fatalf("follow %d %s", code, raw)
	}
	if code, feed := b.call("GET", "/v1/feeds/recommended", viewer, nil, nil); code != http.StatusOK || bytes.Contains(feed, []byte("c-harbor")) {
		t.Fatalf("blocked feed %d %s", code, feed)
	}

	other := b.issue("creator2")
	b.video(other, "c-other", "pier", 30, nil, true)
	if code, raw := b.call("POST", "/v1/moderation/accounts/creator2/takedown", viewer, nil, nil); code != http.StatusForbidden {
		t.Fatalf("takedown viewer %d %s", code, raw)
	}
	code, taken := b.call("POST", "/v1/moderation/accounts/creator2/takedown", moderator, nil, nil)
	if code != http.StatusOK || !bytes.Contains(taken, []byte(`"universal_deletion":false`)) || !bytes.Contains(taken, []byte(`"history_erased":false`)) || !bytes.Contains(taken, []byte("DEC-004")) {
		t.Fatalf("takedown %d %s", code, taken)
	}
	if code, _ := b.call("GET", "/v1/feeds/recommended", viewer, nil, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	if code, raw := b.call("POST", "/v1/comments", other, map[string]any{"content_id": "c-harbor", "text": "still here"}, map[string]string{"Idempotency-Key": "m-down"}); code != http.StatusUnauthorized {
		t.Fatalf("suspended %d %s", code, raw)
	}
	if _, err := b.svc.Issue("creator2"); err == nil {
		t.Fatal("suspended account received a new session")
	}
	code, policy := b.call("GET", "/v1/policy", "", nil, nil)
	if code != http.StatusOK || bytes.Contains(policy, []byte(`"legal_approval":true`)) || !bytes.Contains(policy, []byte("PLACEHOLDER")) || !bytes.Contains(policy, []byte(`"public_launch":"DISABLED"`)) {
		t.Fatalf("policy %d %s", code, policy)
	}
}

func TestCreatorRemovalDoesNotWaitAndDoesNotRevive(t *testing.T) {
	b := newBox(t, Options{Dev: true})
	creator := b.issue("creator1")
	viewer := b.issue("viewer1")
	b.video(creator, "c-harbor", "harbor", 20, nil, true)
	pipe := moderation.NewPipeline()
	pipe.Set("c-harbor", moderation.StageRepair)
	start := time.Unix(1_700_000_000, 0)
	now := start
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = w.Write([]byte(`{"endpoint":"/v1/policy/delist","cache_generation":3,"bytes_deleted":true}`))
	}))
	defer up.Close()
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	down.Close()
	b.svc.setRemoval(moderation.Options{
		Now: func() time.Time { return now }, Pipeline: pipe,
		Chain: moderation.Chain{Stalled: true, Commit: func() (bool, error) {
			t.Fatal("chain commit was attempted during a stall")
			return true, nil
		}},
		Endpoints: []moderation.Endpoint{
			{Name: "upload", Kind: "controlled", URL: up.URL},
			{Name: "host-down", Kind: "controlled", URL: down.URL},
			{Name: "independent", Kind: "independent", Delist: func(string, time.Time) (moderation.Ack, error) {
				return moderation.Ack{}, http.ErrServerClosed
			}},
		},
	})
	_, before := b.call("GET", "/v1/feeds/recommended", viewer, nil, nil)
	code, raw := b.call("POST", "/v1/removal", creator, map[string]any{"content_id": "c-harbor", "confirm": "withdraw"}, nil)
	if code != http.StatusAccepted || bytes.Contains(raw, []byte(`"waited_for_finality":true`)) || bytes.Contains(raw, []byte(`"universal_deletion":true`)) || bytes.Contains(raw, []byte(`"successful_deletion":true`)) {
		t.Fatalf("removal %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte(`"ledger_retained":true`)) || !bytes.Contains(raw, []byte("independent")) {
		t.Fatalf("disclosure %s", raw)
	}
	if pipe.AllowRepair("c-harbor") {
		t.Fatal("repair revived withdrawn content")
	}
	code, feed := b.call("GET", "/v1/feeds/recommended", viewer, nil, nil)
	if code != http.StatusOK || bytes.Contains(feed, []byte("c-harbor")) {
		t.Fatalf("feed %d %s", code, feed)
	}
	if bytes.Equal(before, feed) {
		t.Fatal("cache was not invalidated")
	}
	headerCode, _, header := b.doc("GET", "/v1/feeds/recommended", viewer, nil, map[string]string{"X-SVN-Cached-Generation": "0"})
	if headerCode != http.StatusOK || header.Get("X-SVN-Cache-Generation") == "" || header.Get("X-SVN-Cache") == "hit" {
		t.Fatalf("cache header %d %s %s", headerCode, header.Get("X-SVN-Cache-Generation"), header.Get("X-SVN-Cache"))
	}
}
