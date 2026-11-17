package social

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/records"
)

type box struct {
	t   *testing.T
	svc *Service
	h   http.Handler
}

func newBox(t *testing.T, opt Options) *box {
	t.Helper()
	svc := New(opt)
	return &box{t: t, svc: svc, h: svc.Handler()}
}

func (b *box) call(method, path, token string, body any, hdr map[string]string) (int, []byte) {
	b.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			b.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range hdr {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (b *box) callRaw(method, path, token string, raw []byte, hdr map[string]string) (int, []byte, http.Header) {
	b.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range hdr {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), rec.Header()
}

func (b *box) doc(method, path, token string, body any, hdr map[string]string) (int, map[string]any, http.Header) {
	b.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			b.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range hdr {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	var out map[string]any
	if len(bytes.TrimSpace(rec.Body.Bytes())) > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			b.t.Fatalf("%s %s: %v %s", method, path, err, rec.Body.Bytes())
		}
	}
	return rec.Code, out, rec.Header()
}

func (b *box) issue(account string) string {
	b.t.Helper()
	token, err := b.svc.Issue(account)
	if err != nil {
		b.t.Fatal(err)
	}
	return token
}

func (b *box) video(token, id, title string, at int64, topics []string, available bool) {
	b.t.Helper()
	code, raw := b.call("POST", "/v1/catalog", token, map[string]any{
		"content_id": id, "title": title, "topics": topics, "published_at": at, "available": available,
	}, nil)
	if code != http.StatusCreated && code != http.StatusOK {
		b.t.Fatalf("catalog %s: %d %s", id, code, raw)
	}
}

func idsOf(t *testing.T, doc map[string]any) []string {
	t.Helper()
	raw, ok := doc["items"].([]any)
	if !ok {
		t.Fatalf("items %#v", doc["items"])
	}
	out := make([]string, 0, len(raw))
	for _, row := range raw {
		item := row.(map[string]any)
		out = append(out, item["content_id"].(string))
	}
	return out
}

func TestFollowPermissionsDuplicateLikesAndSaves(t *testing.T) {
	b := newBox(t, Options{})
	viewer := b.issue("viewer1")
	other := b.issue("other1")
	creator := b.issue("creator1")
	b.video(creator, "c-save", "harbor", 10, nil, true)

	if code, _ := b.call("POST", "/v1/follows", "", map[string]any{"account_id": "creator1"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("auth %d", code)
	}
	if code, _ := b.call("POST", "/v1/follows", viewer, map[string]any{"account_id": "viewer1"}, nil); code != http.StatusBadRequest {
		t.Fatalf("self %d", code)
	}
	payload := []byte(`{"account_id":"creator1"}`)
	code, raw, _ := b.callRaw("POST", "/v1/follows", viewer, payload, map[string]string{"Idempotency-Key": "follow-1"})
	if code != http.StatusCreated || !bytes.Contains(raw, []byte(`"duplicate":false`)) || !bytes.Contains(raw, []byte(`"chain_event":false`)) || !bytes.Contains(raw, []byte(`"public_record":false`)) {
		t.Fatalf("follow %d %s", code, raw)
	}
	code, again, _ := b.callRaw("POST", "/v1/follows", viewer, payload, map[string]string{"Idempotency-Key": "follow-1"})
	if code != http.StatusCreated || !bytes.Equal(again, raw) {
		t.Fatalf("retry %d %s", code, again)
	}
	code, dupRaw, _ := b.callRaw("POST", "/v1/follows", viewer, payload, nil)
	if code != http.StatusOK || !bytes.Contains(dupRaw, []byte(`"duplicate":true`)) {
		t.Fatalf("dup %d %s", code, dupRaw)
	}
	if code, _ := b.call("POST", "/v1/follows", viewer, map[string]any{"account_id": "other1"}, map[string]string{"Idempotency-Key": "follow-1"}); code != http.StatusConflict {
		t.Fatalf("conflict %d", code)
	}

	var likeID string
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, doc, _ := b.doc("POST", "/v1/likes", viewer, map[string]any{"content_id": "c-save"}, nil)
			if status != http.StatusCreated && status != http.StatusOK {
				t.Errorf("like %d", status)
				return
			}
			mu.Lock()
			id := doc["like_id"].(string)
			if likeID == "" {
				likeID = id
			} else if likeID != id {
				t.Errorf("like id %s %s", likeID, id)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	status, likes, _ := b.doc("GET", "/v1/likes", viewer, nil, nil)
	if status != http.StatusOK || len(likes["content_ids"].([]any)) != 1 {
		t.Fatalf("likes %#v", likes)
	}

	if code, _ := b.call("POST", "/v1/saves", viewer, map[string]any{"content_id": "c-save"}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	status, mine, _ := b.doc("GET", "/v1/saves", viewer, nil, nil)
	status2, theirs, _ := b.doc("GET", "/v1/saves", other, nil, nil)
	if status != http.StatusOK || mine["private"] != true || mine["content_ids"].([]any)[0] != "c-save" {
		t.Fatalf("save %#v", mine)
	}
	if status2 != http.StatusOK || len(theirs["content_ids"].([]any)) != 0 {
		t.Fatalf("other saves %#v", theirs)
	}
	b.call("PUT", "/v1/profile", viewer, map[string]any{"display_name": "Viewer One", "topics": []string{"boats"}}, nil)
	_, profile, _ := b.doc("GET", "/v1/profile/viewer1", other, nil, nil)
	raw = mustRaw(t, profile)
	if bytes.Contains(raw, []byte("c-save")) || bytes.Contains(raw, []byte("saves")) || bytes.Contains(raw, []byte("hidden")) {
		t.Fatalf("profile leaked %s", raw)
	}
	if code, body := b.call("GET", "/v1/saves?account_id=viewer1", other, nil, nil); code != http.StatusBadRequest || bytes.Contains(body, []byte("c-save")) {
		t.Fatalf("query %d %s", code, body)
	}
}

func TestCommentBoundariesActivityAndDeletion(t *testing.T) {
	b := newBox(t, Options{})
	b.svc.DisallowComment("zzsecretphrase")
	viewer := b.issue("viewer1")
	other := b.issue("other1")
	creator := b.issue("creator1")
	b.video(creator, "c-note", "harbor", 10, nil, true)

	code, body := b.call("POST", "/v1/comments", viewer, map[string]any{"content_id": "c-note", "text": "<b>hi</b>"}, map[string]string{"Idempotency-Key": "c-html"})
	if code != http.StatusBadRequest || !bytes.Contains(body, []byte("COMMENT_REFUSED")) || bytes.Contains(body, []byte("<b>")) {
		t.Fatalf("html %d %s", code, body)
	}
	code, body = b.call("POST", "/v1/comments", viewer, map[string]any{"content_id": "c-note", "text": "see zzsecretphrase"}, map[string]string{"Idempotency-Key": "c-secret"})
	if code != http.StatusBadRequest || bytes.Contains(body, []byte("zzsecretphrase")) {
		t.Fatalf("secret %d %s", code, body)
	}
	hello := []byte(`{"content_id":"c-note","text":"hello"}`)
	code, raw, _ := b.callRaw("POST", "/v1/comments", viewer, hello, map[string]string{"Idempotency-Key": "c-ok"})
	if code != http.StatusCreated || !bytes.Contains(raw, []byte(`"text":"hello"`)) {
		t.Fatalf("post %d %s", code, raw)
	}
	var posted map[string]any
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	commentID := posted["comment_id"].(string)
	code, again, _ := b.callRaw("POST", "/v1/comments", viewer, hello, map[string]string{"Idempotency-Key": "c-ok"})
	if code != http.StatusCreated || !bytes.Equal(again, raw) {
		t.Fatalf("replay %d %s", code, again)
	}
	_, notes, _ := b.doc("GET", "/v1/activity", creator, nil, nil)
	if len(notes["notes"].([]any)) != 1 || notes["notes"].([]any)[0].(map[string]any)["text"] != "hello" {
		t.Fatalf("activity %#v", notes)
	}
	_, outsider, _ := b.doc("GET", "/v1/activity", other, nil, nil)
	if len(outsider["notes"].([]any)) != 0 {
		t.Fatalf("outsider %#v", outsider)
	}
	if code, _ := b.call("DELETE", "/v1/comments/"+commentID, other, nil, nil); code != http.StatusForbidden {
		t.Fatalf("delete other %d", code)
	}
	if code, _ := b.call("DELETE", "/v1/comments/"+commentID, viewer, nil, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	_, left, _ := b.doc("GET", "/v1/comments/c-note", viewer, nil, nil)
	if len(left["comments"].([]any)) != 0 {
		t.Fatalf("comments %#v", left)
	}
	_, after, _ := b.doc("GET", "/v1/activity", creator, nil, nil)
	if len(after["notes"].([]any)) != 0 {
		t.Fatalf("activity after delete %#v", after)
	}
	code, report := b.call("POST", "/v1/reports", viewer, map[string]any{"text": "private complaint"}, nil)
	if code != http.StatusNotImplemented || !bytes.Contains(report, []byte("MODERATION_DEFERRED")) || bytes.Contains(report, []byte("complaint")) {
		t.Fatalf("report %d %s", code, report)
	}
	if code, _ := b.call("POST", "/v1/blocks", viewer, map[string]any{"account_id": "creator1"}, nil); code != http.StatusNotImplemented {
		t.Fatal(code)
	}
}

func TestPaginationUnderConcurrentUpdates(t *testing.T) {
	b := newBox(t, Options{})
	viewer := b.issue("viewer1")
	creator := b.issue("creator1")
	if code, _ := b.call("POST", "/v1/follows", viewer, map[string]any{"account_id": "creator1"}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	for _, row := range []struct {
		id string
		at int64
	}{{"c-a", 40}, {"c-b", 30}, {"c-c", 20}, {"c-d", 10}} {
		b.video(creator, row.id, row.id, row.at, nil, true)
	}
	limit := map[string]string{"X-SVN-Page-Limit": "2"}
	_, page, header := b.doc("GET", "/v1/feeds/following", viewer, nil, limit)
	first := idsOf(t, page)
	if strings.Join(first, ",") != "c-a,c-b" {
		t.Fatalf("page1 %v", first)
	}
	cursor := page["next_cursor"].(string)
	var second []string
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		b.video(creator, "c-new", "c-new", 50, nil, true)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, doc, _ := b.doc("GET", "/v1/feeds/following/"+cursor, viewer, nil, limit)
		second = idsOf(t, doc)
	}()
	close(start)
	wg.Wait()
	if strings.Join(second, ",") != "c-c,c-d" {
		t.Fatalf("page2 %v header %s", second, header.Get("X-SVN-Cache"))
	}
	seen := map[string]struct{}{}
	for _, id := range append(append([]string{}, first...), second...) {
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate %s", id)
		}
		seen[id] = struct{}{}
	}
	if _, ok := seen["c-new"]; ok {
		t.Fatal("newer item landed on a later page")
	}
}

func TestChronologicalFilterAndCachedPage(t *testing.T) {
	b := newBox(t, Options{})
	viewer := b.issue("viewer1")
	creator := b.issue("creator1")
	blocked := b.issue("blocked1")
	if code, _ := b.call("POST", "/v1/follows", viewer, map[string]any{"account_id": "creator1"}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	if code, _ := b.call("POST", "/v1/follows", viewer, map[string]any{"account_id": "blocked1"}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	b.video(creator, "c-old", "old", 10, nil, true)
	b.video(creator, "c-mid", "mid", 20, nil, true)
	b.video(creator, "c-new", "new", 30, nil, true)
	b.video(creator, "c-down", "down", 40, nil, false)
	b.video(blocked, "c-block", "block", 50, nil, true)
	b.svc.HideCreator("blocked1")
	_, page, header := b.doc("GET", "/v1/feeds/following", viewer, nil, nil)
	if strings.Join(idsOf(t, page), ",") != "c-new,c-mid,c-old" {
		t.Fatalf("order %v", idsOf(t, page))
	}
	if bytes.Contains(mustRaw(t, page), []byte("watch")) {
		t.Fatal("watch history in feed")
	}
	gen := header.Get("X-SVN-Cache-Generation")
	stale := append([]byte(nil), mustRaw(t, page)...)
	if code, _ := b.call("POST", "/v1/catalog/c-new/withdraw", creator, nil, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	_, fresh, freshHeader := b.doc("GET", "/v1/feeds/following", viewer, nil, map[string]string{"X-SVN-Cached-Generation": gen})
	if freshHeader.Get("X-SVN-Cache") != "refreshed" {
		t.Fatalf("cache %s", freshHeader.Get("X-SVN-Cache"))
	}
	if strings.Join(idsOf(t, fresh), ",") != "c-mid,c-old" {
		t.Fatalf("filtered %v", idsOf(t, fresh))
	}
	if !bytes.Contains(stale, []byte("c-new")) || bytes.Contains(mustRaw(t, fresh), []byte("c-new")) {
		t.Fatal("stale page was reused or the withdrawn id survived")
	}
	_, hit, hitHeader := b.doc("GET", "/v1/feeds/following", viewer, nil, map[string]string{"X-SVN-Cached-Generation": freshHeader.Get("X-SVN-Cache-Generation")})
	if hitHeader.Get("X-SVN-Cache") != "hit" || strings.Join(idsOf(t, hit), ",") != "c-mid,c-old" {
		t.Fatalf("hit %s %v", hitHeader.Get("X-SVN-Cache"), idsOf(t, hit))
	}
}

func TestRecommendationFixtureIsNotAQualityClaim(t *testing.T) {
	// Fixture order only. It does not show that the ranking is better.
	b := newBox(t, Options{})
	viewer := b.issue("viewer1")
	a := b.issue("creatora")
	d := b.issue("creatord")
	topic := b.issue("topic1")
	recent := b.issue("recent1")
	other := b.issue("other1")
	for _, id := range []string{"creatora", "creatord"} {
		if code, _ := b.call("POST", "/v1/follows", viewer, map[string]any{"account_id": id}, nil); code != http.StatusCreated {
			t.Fatal(id, code)
		}
	}
	if code, _ := b.call("PUT", "/v1/preferences", viewer, map[string]any{
		"topics": []string{"boats"}, "hidden_accounts": []string{"hidden1"},
	}, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	hidden := b.issue("hidden1")
	b.video(a, "c-both", "both", 100000, []string{"boats"}, true)
	b.video(d, "c-d1", "d1", 100000, []string{"boats"}, true)
	b.video(d, "c-d2", "d2", 99000, []string{"boats"}, true)
	b.video(d, "c-d3", "d3", 98000, []string{"boats"}, true)
	b.video(a, "c-follow", "follow", 1000, nil, true)
	b.video(topic, "c-topic", "topic", 1000, []string{"boats"}, true)
	b.video(recent, "c-recent", "recent", 95000, nil, true)
	b.video(other, "c-old", "old", 1000, nil, true)
	b.video(a, "c-gone", "gone", 100000, []string{"boats"}, true)
	if code, _ := b.call("POST", "/v1/catalog/c-gone/withdraw", a, nil, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	b.video(hidden, "c-hide", "hide", 100000, []string{"boats"}, true)
	b.video(a, "c-skip", "skip", 100000, []string{"boats"}, true)
	if code, _ := b.call("POST", "/v1/not-interested", viewer, map[string]any{"content_id": "c-skip"}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	b.video(a, "c-down", "down", 100000, []string{"boats"}, false)
	b.video(a, "c-local", "local", 100000, []string{"boats"}, true)
	b.svc.Hide("c-local")

	_, page, _ := b.doc("GET", "/v1/feeds/recommended", viewer, nil, nil)
	got := strings.Join(idsOf(t, page), ",")
	want := "c-both,c-d1,c-d2,c-follow,c-topic,c-recent"
	if got != want {
		t.Fatalf("fixture %s", got)
	}
	factors := page["scoring_factors"].([]any)
	if len(factors) != 4 || factors[0] != "followed creator +3" {
		t.Fatalf("factors %#v", factors)
	}
}

func TestRebuildSearchCoverage(t *testing.T) {
	b := newBox(t, Options{})
	viewer := b.issue("viewer1")
	creator := b.issue("creator1")
	b.video(creator, "c-keep", "harbor light", 10, []string{"boats"}, true)
	b.video(creator, "c-also", "harbor night", 9, []string{"boats"}, true)
	b.video(creator, "c-gone", "harbor gone", 8, []string{"boats"}, true)
	if code, _ := b.call("POST", "/v1/catalog/c-gone/withdraw", creator, nil, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	if code, _ := b.call("PUT", "/v1/profile", creator, map[string]any{"display_name": "Harbor Maker", "topics": []string{"boats"}}, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	code, doc, _ := b.doc("POST", "/v1/index/rebuild", viewer, nil, nil)
	if code != http.StatusOK || doc["equal"] != true || doc["permitted"] != float64(2) || doc["indexed"] != float64(2) {
		t.Fatalf("rebuild %#v", doc)
	}
	if strings.Join(stringList(t, doc["content_ids"]), ",") != "c-also,c-keep" {
		t.Fatalf("coverage %#v", doc["content_ids"])
	}
	_, videos, _ := b.doc("POST", "/v1/search", viewer, map[string]any{"kind": "video", "q": "harbor"}, nil)
	if strings.Join(hitIDs(t, videos), ",") != "c-also,c-keep" {
		t.Fatalf("search %#v", videos)
	}
	_, profiles, _ := b.doc("POST", "/v1/search", viewer, map[string]any{"kind": "profile", "q": "harbor"}, nil)
	if hitIDs(t, profiles)[0] != "creator1" {
		t.Fatalf("profile %#v", profiles)
	}
	_, topics, _ := b.doc("POST", "/v1/search", viewer, map[string]any{"kind": "topic", "q": "boats"}, nil)
	if len(hitIDs(t, topics)) != 2 {
		t.Fatalf("topics %#v", topics)
	}
}

func TestSignedFollowAndAccountDeletion(t *testing.T) {
	b := newBox(t, Options{})
	author := b.issue("author1")
	reader := b.issue("reader1")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := b.call("POST", "/v1/keys", author, map[string]any{"key_id": "k-1", "public": hex.EncodeToString(pub)}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	canon, err := Canonical("author1", "follow-1", "k-1", strings.Repeat("0", 64), "follow", "reader1", 0)
	if err != nil {
		t.Fatal(err)
	}
	sig := hex.EncodeToString(ed25519.Sign(priv, records.Message(canon)))
	code, doc, _ := b.doc("POST", "/v1/public-records", author, map[string]any{"canonical": canon, "signature": sig}, nil)
	if code != http.StatusCreated || doc["public_record"] != true || doc["chain_event"] != false {
		t.Fatalf("record %#v %d", doc, code)
	}
	_, follows, _ := b.doc("GET", "/v1/public-follows/author1", reader, nil, nil)
	if strings.Join(stringList(t, follows["follows"]), ",") != "reader1" {
		t.Fatalf("public follows %#v", follows)
	}
	if code, _ := b.call("POST", "/v1/follows", author, map[string]any{"account_id": "creator9"}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	_, follows, _ = b.doc("GET", "/v1/public-follows/author1", reader, nil, nil)
	if strings.Join(stringList(t, follows["follows"]), ",") != "reader1" {
		t.Fatalf("unsigned follow leaked %#v", follows)
	}
	bad := strings.Replace(canon, `"record_type":"follow"`, `"record_type":"post"`, 1)
	if code, _ := b.call("POST", "/v1/public-records", author, map[string]any{"canonical": bad, "signature": sig}, nil); code != http.StatusBadRequest {
		t.Fatalf("post %d", code)
	}
	if code, _ := b.call("POST", "/v1/public-records", author, map[string]any{"canonical": canon, "signature": strings.Repeat("ab", 64)}, nil); code != http.StatusBadRequest {
		t.Fatal("bad signature")
	}
	b.video(author, "c-own", "mine", 3, nil, true)
	b.call("POST", "/v1/comments", author, map[string]any{"content_id": "c-own", "text": "note"}, map[string]string{"Idempotency-Key": "del-1"})
	code, deleted, _ := b.doc("POST", "/v1/account/social-deletion", author, nil, nil)
	if code != http.StatusOK || deleted["history_erased"] != false || deleted["decision_id"] != "DEC-004" || deleted["value_version"] != float64(0) || deleted["public_records_kept"] != float64(1) {
		t.Fatalf("delete %#v", deleted)
	}
	if code, _ := b.call("GET", "/v1/saves", author, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked %d", code)
	}
	_, kept, _ := b.doc("GET", "/v1/public-records/author1", reader, nil, nil)
	if kept["history_erased"] != false || len(kept["records"].([]any)) != 1 {
		t.Fatalf("kept %#v", kept)
	}
	_, feed, _ := b.doc("GET", "/v1/feeds/recommended", reader, nil, nil)
	for _, id := range idsOf(t, feed) {
		if id == "c-own" {
			t.Fatal("deleted creator video stayed in the feed")
		}
	}
}

func TestRateLimitHidesTheBudget(t *testing.T) {
	current := time.Unix(1_700_000_000, 0).UTC()
	b := newBox(t, Options{Now: func() time.Time { return current }})
	viewer := b.issue("viewer1")
	for i := 0; i < writeBudget; i++ {
		code, raw := b.call("POST", "/v1/search", viewer, map[string]any{"kind": "video", "q": "ab"}, nil)
		if code != http.StatusOK {
			t.Fatalf("search %d %s", i, raw)
		}
	}
	code, raw := b.call("POST", "/v1/search", viewer, map[string]any{"kind": "video", "q": "ab"}, nil)
	if code != http.StatusTooManyRequests || !bytes.Contains(raw, []byte("RATE_LIMITED")) || bytes.ContainsAny(raw, "0123456789") {
		t.Fatalf("limit %d %s", code, raw)
	}
	current = current.Add(time.Minute + time.Second)
	if code, _ := b.call("POST", "/v1/search", viewer, map[string]any{"kind": "video", "q": "ab"}, nil); code != http.StatusOK {
		t.Fatalf("after window %d", code)
	}
}

func TestSecondClientReadsStableIDs(t *testing.T) {
	b := newBox(t, Options{})
	viewer := b.issue("viewer1")
	creator := b.issue("creator1")
	if code, _ := b.call("POST", "/v1/follows", viewer, map[string]any{"account_id": "creator1"}, nil); code != http.StatusCreated {
		t.Fatal(code)
	}
	b.video(creator, "c-one", "one", 2, nil, true)
	b.video(creator, "c-two", "two", 1, nil, true)
	srv := httptest.NewServer(b.h)
	t.Cleanup(srv.Close)
	got, err := FollowingIDs(srv.URL, viewer)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "c-one,c-two" {
		t.Fatalf("client %v", got)
	}
	if _, err := FollowingIDs("https://example.com", viewer); err == nil {
		t.Fatal("public host was accepted")
	}
	bin := filepath.Join(t.TempDir(), "svn-feed-client")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/svn-feed-client")
	cmd.Dir = moduleDir(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	out, err := exec.Command(bin, "-base="+srv.URL, "-token="+viewer).CombinedOutput()
	if err != nil {
		t.Fatalf("client %v %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "c-one\nc-two" {
		t.Fatalf("stdout %q", out)
	}
}

func TestCanonicalMatchesProfile(t *testing.T) {
	text, err := Canonical("author-1", "record-1", "k-old", strings.Repeat("0", 64), "follow", "creator-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(moduleDir(t), "..", ".."))
	script := `import json, sys
sys.path.insert(0, sys.argv[1])
import jcs
text = sys.argv[2]
if jcs.canonicalize(json.loads(text)) != text:
    raise SystemExit("canonical")
print("ok")
`
	cmd := exec.Command("python3", "-", filepath.Join(root, "docs/evidence/goal-02"), text)
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "ok\n" {
		t.Fatalf("jcs %v %s", err, out)
	}
}

func TestDiscoveryDocumentAndDevFlag(t *testing.T) {
	b := newBox(t, Options{})
	viewer := b.issue("viewer1")
	code, raw := b.call("GET", "/v1/discovery", viewer, nil, nil)
	if code != http.StatusOK || !bytes.Contains(raw, []byte(`"activitypub": false`)) || !bytes.Contains(raw, []byte(`"at_protocol": false`)) {
		t.Fatalf("doc %d %s", code, raw)
	}
	if code, _ := b.call("POST", "/v1/dev/session", "", map[string]any{"account_id": "dev1"}, nil); code != http.StatusNotFound {
		t.Fatalf("dev %d", code)
	}
	open := newBox(t, Options{Dev: true})
	code, doc, _ := open.doc("POST", "/v1/dev/session", "", map[string]any{"account_id": "dev1"}, nil)
	if code != http.StatusCreated || doc["account_id"] != "dev1" || doc["token"] == "" {
		t.Fatalf("dev session %#v", doc)
	}
	if code, _ := b.call("GET", "/v1/watches", viewer, nil, nil); code != http.StatusNotFound {
		t.Fatal(code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/discovery", nil)
	req.Header.Set("Authorization", "Bearer "+viewer)
	req.Header.Set("Origin", "http://127.0.0.1:9")
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:9" {
		t.Fatal(rec.Header().Get("Access-Control-Allow-Origin"))
	}
	req.Header.Set("Origin", "https://example.com")
	rec = httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("public origin was allowed")
	}
}

func TestPackageDoesNotImportChain(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("chain/")) || bytes.Contains(raw, []byte("cosmossdk.io")) {
			t.Fatal(entry.Name())
		}
	}
}

func moduleDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func mustRaw(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func stringList(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("%#v", value)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(string))
	}
	return out
}

func hitIDs(t *testing.T, doc map[string]any) []string {
	t.Helper()
	raw := doc["hits"].([]any)
	out := make([]string, 0, len(raw))
	for _, row := range raw {
		item := row.(map[string]any)
		if id, ok := item["content_id"].(string); ok {
			out = append(out, id)
		} else {
			out = append(out, item["account_id"].(string))
		}
	}
	return out
}
