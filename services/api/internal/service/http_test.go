package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/pgdev"
	"github.com/lib/pq"
)

var (
	testConfig pq.Config
	testDB     *sql.DB
	shortVideo []byte
	shortSum   string
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cluster, err := pgdev.Start(ctx, pgdev.Options{
		BinDir:       "/usr/lib/postgresql/18/bin",
		StartTimeout: 2 * time.Minute,
	})
	if err != nil {
		panic(err)
	}
	defer cluster.Remove()
	cfg, err := cluster.TestSuperuser("postgres")
	if err != nil {
		panic(err)
	}
	testConfig = cfg
	db, err := Open(cfg)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		panic(err)
	}
	if err := Migrate(ctx, db); err != nil {
		panic(err)
	}
	testDB = db
	shortVideo = encode("0.3")
	sum := sha256.Sum256(shortVideo)
	shortSum = hex.EncodeToString(sum[:])
	os.Exit(m.Run())
}

func encode(seconds string) []byte {
	dir, err := os.MkdirTemp("", "svn-api-video-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "clip.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=32x32:rate=10", "-t", seconds, "-pix_fmt", "yuv420p", path)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C.UTF-8", "TZ=UTC"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic(string(out) + " " + err.Error())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return body
}

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type harness struct {
	t     *testing.T
	svc   *Service
	srv   *httptest.Server
	clock *clock
	a     Fixture
	b     Fixture
	dir   string
}

func newHarness(t *testing.T, limits Limits, extra []Fixture) *harness {
	t.Helper()
	a, b := pair(t)
	fixtures := []Fixture{a, b}
	fixtures = append(fixtures, extra...)
	clk := &clock{at: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	svc, err := New(t.Context(), Options{
		Environment:       "development",
		EnableDevFixtures: true,
		Fixtures:          fixtures,
		Limits:            limits,
		QuarantineDir:     dir,
		Database:          testConfig,
		Now:               clk.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := httptest.NewServer(svc.Handler())
	t.Cleanup(srv.Close)
	return &harness{t: t, svc: svc, srv: srv, clock: clk, a: a, b: b, dir: dir}
}

var pairSeq atomic.Int64

func pair(t *testing.T) (Fixture, Fixture) {
	t.Helper()
	slug := strings.ToLower(t.Name())
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "")
	if len(slug) > 16 {
		slug = slug[:16]
	}
	n := strconv.FormatInt(pairSeq.Add(1), 10)
	return Fixture{
			Token: "dev-fixture-token-" + slug + n + "a", AccountID: "a" + slug + n + "a", Role: "creator",
		}, Fixture{
			Token: "dev-fixture-token-" + slug + n + "b", AccountID: "a" + slug + n + "b", Role: "creator",
		}
}

func wideLimits(allow int64) Limits {
	return Limits{
		Label:                      LimitsLabel,
		MaxInputBytes:              1_000_000,
		MaxDurationSeconds:         60,
		MaxDimension:               7680,
		AccountInputAllowanceBytes: allow,
		AccountOutputBudgetBytes:   1_000_000,
		OutputReservationBytes:     1000,
		TransferTTLSeconds:         3600,
		IntentRatePerMinute:        100,
		ProbeTimeoutSeconds:        20,
	}
}

func (h *harness) jsonReq(method, path, token string, body any) *http.Response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

func (h *harness) put(path, token string, body []byte) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPut, h.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Transfer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

func decode(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func errorCode(t *testing.T, body map[string]any) string {
	t.Helper()
	item, _ := body["error"].(map[string]any)
	code, _ := item["code"].(string)
	if code == "" {
		t.Fatalf("missing error code in %#v", body)
	}
	return code
}

func (h *harness) intent(token, key string, n int, extra map[string]any) (int, map[string]any) {
	h.t.Helper()
	payload := map[string]any{"idempotency_key": key, "declared_bytes": n}
	for k, v := range extra {
		payload[k] = v
	}
	res := h.jsonReq(http.MethodPost, "/upload-intents", token, payload)
	return res.StatusCode, decode(h.t, res)
}

func transfer(t *testing.T, body map[string]any) (string, string) {
	t.Helper()
	raw, _ := body["transfer"].(map[string]any)
	token, _ := raw["token"].(string)
	path, _ := raw["path"].(string)
	if token == "" || path == "" {
		t.Fatalf("missing transfer in %#v", body)
	}
	return token, path
}

func (h *harness) quota(account string) (int64, int64) {
	h.t.Helper()
	in, out, err := h.svc.store.Quota(context.Background(), account)
	if err != nil {
		h.t.Fatal(err)
	}
	return in, out
}

func TestPermittedFixtureQuarantineAndJob(t *testing.T) {
	h := newHarness(t, wideLimits(int64(len(shortVideo)*2)), nil)
	status, body := h.intent(h.a.Token, "clip-1", len(shortVideo), map[string]any{
		"filename":                "evil.mp4",
		"mime":                    "video/mp4",
		"client_duration_seconds": 59,
	})
	if status != http.StatusAccepted {
		t.Fatalf("intent %d %#v", status, body)
	}
	if body["limits_label"] != "PROPOSED" {
		t.Fatalf("label %#v", body["limits_label"])
	}
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	put := h.put(path, token, shortVideo)
	if put.StatusCode != http.StatusNoContent {
		t.Fatalf("put %d", put.StatusCode)
	}
	put.Body.Close()
	done := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": shortSum})
	doneBody := decode(t, done)
	if done.StatusCode != http.StatusAccepted {
		t.Fatalf("complete %d %#v", done.StatusCode, doneBody)
	}
	if doneBody["status"] != "PROCESSING_QUEUED" || doneBody["publication"] != "NOT_PUBLISHED" {
		t.Fatalf("status %#v", doneBody)
	}
	if doneBody["job_status"] != "QUEUED" || doneBody["job_id"] == "" {
		t.Fatalf("job %#v", doneBody)
	}
	seconds, _ := doneBody["duration_seconds"].(float64)
	if seconds < 0.2 || seconds > 0.5 {
		t.Fatalf("worker duration %v, client sent 59", seconds)
	}
	name := objectName(t, id)
	if name == "evil.mp4" || strings.Contains(name, "mp4") {
		t.Fatalf("object name follows the client filename: %s", name)
	}
	info, err := os.Stat(filepath.Join(h.dir, "quarantine", name))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	jobs, err := h.svc.store.JobCount(context.Background(), id)
	if err != nil || jobs != 1 {
		t.Fatalf("jobs %d %v", jobs, err)
	}
	var chainStatus string
	var payload []byte
	if err := testDB.QueryRow(`SELECT status, payload FROM api.chain_outbox WHERE upload_id = $1`, id).Scan(&chainStatus, &payload); err != nil {
		t.Fatal(err)
	}
	if chainStatus != "HELD" {
		t.Fatalf("chain status %s", chainStatus)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(payload, &keys); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"account_id": true, "content_id": true, "input_sha256": true,
		"input_bytes": true, "output_budget_bytes": true, "schema_version": true,
	}
	if len(keys) != len(want) {
		t.Fatalf("intent keys %#v", keys)
	}
	for key := range keys {
		if !want[key] {
			t.Fatalf("unexpected intent key %s", key)
		}
	}
	text := string(payload)
	for _, forbidden := range []string{"evil", "video/mp4", h.a.Token, "filename"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("intent payload contains %s", forbidden)
		}
	}
	reserved, _ := h.quota(h.a.AccountID)
	if reserved != int64(len(shortVideo)) {
		t.Fatalf("reserved %d", reserved)
	}
}

func TestMalformedUntrustedAndDimensions(t *testing.T) {
	limits := wideLimits(1_000_000)
	limits.MaxDimension = 16
	h := newHarness(t, limits, nil)
	status, body := h.intent(h.a.Token, "text-1", len(shortVideo), map[string]any{
		"filename": "clip.mp4", "mime": "video/mp4",
	})
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	junk := bytes.Repeat([]byte("n"), len(shortVideo))
	put := h.put(path, token, junk)
	if put.StatusCode != http.StatusNoContent {
		t.Fatalf("put %d", put.StatusCode)
	}
	put.Body.Close()
	sum := sha256.Sum256(junk)
	res := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": hex.EncodeToString(sum[:])})
	got := decode(t, res)
	if res.StatusCode != 422 || errorCode(t, got) != "MALFORMED" {
		t.Fatalf("malformed %d %#v", res.StatusCode, got)
	}
	if jobs, _ := h.svc.store.JobCount(context.Background(), id); jobs != 0 {
		t.Fatalf("malformed created a job")
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("malformed kept reservation %d", in)
	}

	status, body = h.intent(h.a.Token, "wide-1", len(shortVideo), nil)
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	id, _ = body["upload_id"].(string)
	token, path = transfer(t, body)
	put = h.put(path, token, shortVideo)
	put.Body.Close()
	res = h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": shortSum})
	got = decode(t, res)
	if res.StatusCode != 422 || errorCode(t, got) != "DIMENSIONS" {
		t.Fatalf("dimensions %d %#v", res.StatusCode, got)
	}
}

func TestOversizedBodiesAndCeiling(t *testing.T) {
	if err := Validate(Limits{
		Label: LimitsLabel, MaxInputBytes: ProposedMaxInputBytes + 1, MaxDurationSeconds: 60,
		MaxDimension: 100, AccountInputAllowanceBytes: 1, AccountOutputBudgetBytes: 1,
		OutputReservationBytes: 1, TransferTTLSeconds: 60, IntentRatePerMinute: 1, ProbeTimeoutSeconds: 1,
	}); err == nil {
		t.Fatal("config above 100000000 bytes was accepted")
	}
	if err := Validate(Limits{
		Label: LimitsLabel, MaxInputBytes: 1000, MaxDurationSeconds: 61,
		MaxDimension: 100, AccountInputAllowanceBytes: 1, AccountOutputBudgetBytes: 1,
		OutputReservationBytes: 1, TransferTTLSeconds: 60, IntentRatePerMinute: 1, ProbeTimeoutSeconds: 1,
	}); err == nil {
		t.Fatal("config above 60 seconds was accepted")
	}
	h := newHarness(t, wideLimits(1_000_000), nil)
	status, body := h.intent(h.a.Token, "too-big", int(h.svc.limits.MaxInputBytes)+1, nil)
	if status != 400 || errorCode(t, body) != "LIMIT" {
		t.Fatalf("declared %d %#v", status, body)
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("over-declared reserved %d", in)
	}
	status, body = h.intent(h.a.Token, "over-body", 4, nil)
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	token, path := transfer(t, body)
	put := h.put(path, token, []byte("12345"))
	got := decode(t, put)
	if put.StatusCode != 400 || errorCode(t, got) != "LIMIT" {
		t.Fatalf("body %d %#v", put.StatusCode, got)
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("oversized body kept reservation %d", in)
	}
	entries, err := os.ReadDir(filepath.Join(h.dir, "quarantine"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("quarantine kept %d files", len(entries))
	}
}

func TestFalseCompletionAndDuplicate(t *testing.T) {
	h := newHarness(t, wideLimits(int64(len(shortVideo)*2)), nil)
	status, body := h.intent(h.a.Token, "once", len(shortVideo), nil)
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	againStatus, again := h.intent(h.a.Token, "once", len(shortVideo), nil)
	if againStatus != http.StatusAccepted || again["upload_id"] != id {
		t.Fatalf("duplicate %#v", again)
	}
	if in, _ := h.quota(h.a.AccountID); in != int64(len(shortVideo)) {
		t.Fatalf("duplicate reserved %d", in)
	}
	conflict, conflictBody := h.intent(h.a.Token, "once", len(shortVideo)-1, nil)
	if conflict != 409 || errorCode(t, conflictBody) != "CONFLICT" {
		t.Fatalf("payload conflict %d %#v", conflict, conflictBody)
	}
	put := h.put(path, token, shortVideo)
	put.Body.Close()
	if put.StatusCode != http.StatusNoContent {
		t.Fatalf("put %d", put.StatusCode)
	}
	bad := strings.Repeat("ab", 32)
	res := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": bad})
	got := decode(t, res)
	if res.StatusCode != 409 || errorCode(t, got) != "DIGEST_MISMATCH" {
		t.Fatalf("false complete %d %#v", res.StatusCode, got)
	}
	if jobs, _ := h.svc.store.JobCount(context.Background(), id); jobs != 0 {
		t.Fatal("false completion queued a job")
	}
	view := h.jsonReq(http.MethodGet, "/uploads/"+id, h.a.Token, nil)
	viewBody := decode(t, view)
	if viewBody["status"] != "RECEIVED" {
		t.Fatalf("status after false completion %#v", viewBody)
	}
	ok := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": shortSum})
	okBody := decode(t, ok)
	if ok.StatusCode != http.StatusAccepted || okBody["status"] != "PROCESSING_QUEUED" {
		t.Fatalf("real complete %#v", okBody)
	}
	second := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": shortSum})
	secondBody := decode(t, second)
	if secondBody["job_id"] != okBody["job_id"] {
		t.Fatalf("duplicate complete changed the job %#v %#v", okBody, secondBody)
	}
	if jobs, _ := h.svc.store.JobCount(context.Background(), id); jobs != 1 {
		t.Fatalf("jobs %d", jobs)
	}
}

func TestConcurrentQuotaAndOutputBudget(t *testing.T) {
	const declared = 1000
	h := newHarness(t, wideLimits(declared), nil)
	var wg sync.WaitGroup
	codes := make([]int, 8)
	start := make(chan struct{})
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			reqBody, _ := json.Marshal(map[string]any{
				"idempotency_key": "q-" + string(rune('a'+i)),
				"declared_bytes":  declared,
			})
			req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/upload-intents", bytes.NewReader(reqBody))
			req.Header.Set("Authorization", "Bearer "+h.a.Token)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				codes[i] = 1
				return
			}
			codes[i] = res.StatusCode
			res.Body.Close()
		}(i)
	}
	close(start)
	wg.Wait()
	accepted := 0
	for _, code := range codes {
		if code == http.StatusAccepted {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d codes %v", accepted, codes)
	}
	if in, _ := h.quota(h.a.AccountID); in != declared {
		t.Fatalf("concurrent reserved %d", in)
	}

	output := wideLimits(1_000_000)
	output.AccountOutputBudgetBytes = output.OutputReservationBytes
	h2 := newHarness(t, output, nil)
	first, firstBody := h2.intent(h2.a.Token, "out-1", 100, nil)
	second, secondBody := h2.intent(h2.a.Token, "out-2", 100, nil)
	if first != http.StatusAccepted {
		t.Fatal(firstBody)
	}
	if second != 403 || errorCode(t, secondBody) != "QUOTA_OUTPUT" {
		t.Fatalf("output %d %#v", second, secondBody)
	}
	in, out := h2.quota(h2.a.AccountID)
	if in != 100 || out != output.OutputReservationBytes {
		t.Fatalf("input %d output %d", in, out)
	}
}

func TestInterruptedSweepReleasesOnce(t *testing.T) {
	h := newHarness(t, wideLimits(1_000_000), nil)
	status, body := h.intent(h.a.Token, "partial", len(shortVideo), nil)
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	put := h.put(path, token, shortVideo[:len(shortVideo)/2])
	got := decode(t, put)
	if put.StatusCode != http.StatusAccepted || got["status"] != "RECEIVING" {
		t.Fatalf("partial %#v", got)
	}
	early := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": shortSum})
	earlyBody := decode(t, early)
	if early.StatusCode != 409 || errorCode(t, earlyBody) != "STATE" {
		t.Fatalf("early complete %#v", earlyBody)
	}
	if in, _ := h.quota(h.a.AccountID); in != int64(len(shortVideo)) {
		t.Fatalf("partial released early %d", in)
	}
	h.clock.Add(2 * time.Hour)
	if _, err := h.svc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	var stored string
	var released bool
	if err := testDB.QueryRow(`SELECT status, reservation_released FROM api.uploads WHERE upload_id = $1`, id).Scan(&stored, &released); err != nil {
		t.Fatal(err)
	}
	if stored != "EXPIRED" || !released {
		t.Fatalf("after sweep status %s released %v", stored, released)
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("after sweep %d", in)
	}
	second, err := h.svc.Sweep(context.Background())
	if err != nil || second != 0 {
		t.Fatalf("second sweep %d %v", second, err)
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("second sweep changed quota %d", in)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "quarantine", objectName(t, id))); !os.IsNotExist(err) {
		t.Fatalf("abandoned file still present: %v", err)
	}
}

func TestExpiredTransfer(t *testing.T) {
	h := newHarness(t, wideLimits(1_000_000), nil)
	status, body := h.intent(h.a.Token, "expire", len(shortVideo), nil)
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	token, path := transfer(t, body)
	h.clock.Add(2 * time.Hour)
	put := h.put(path, token, shortVideo)
	got := decode(t, put)
	if put.StatusCode != 401 || errorCode(t, got) != "UNAUTHENTICATED" {
		t.Fatalf("expired %d %#v", put.StatusCode, got)
	}
	entries, _ := os.ReadDir(filepath.Join(h.dir, "quarantine"))
	if len(entries) != 0 {
		t.Fatalf("expired write stored %d files", len(entries))
	}
	if _, err := h.svc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("expired reservation %d", in)
	}
}

func TestCrossAccountDenied(t *testing.T) {
	h := newHarness(t, wideLimits(int64(len(shortVideo)*2)), nil)
	status, body := h.intent(h.a.Token, "owned", len(shortVideo), nil)
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	put := h.put(path, token, shortVideo)
	put.Body.Close()
	for _, call := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/uploads/" + id},
		{http.MethodDelete, "/uploads/" + id},
		{http.MethodPost, "/uploads/" + id + "/complete"},
	} {
		var payload any
		if call.method == http.MethodPost {
			payload = map[string]any{"digest": shortSum}
		}
		res := h.jsonReq(call.method, call.path, h.b.Token, payload)
		got := decode(t, res)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %d %#v", call.method, res.StatusCode, got)
		}
		encoded, _ := json.Marshal(got)
		if strings.Contains(string(encoded), h.a.AccountID) {
			t.Fatalf("denial leaked the owner: %s", encoded)
		}
	}
	sessionPut, err := http.NewRequest(http.MethodPut, h.srv.URL+path, bytes.NewReader(shortVideo))
	if err != nil {
		t.Fatal(err)
	}
	sessionPut.Header.Set("Authorization", "Bearer "+h.a.Token)
	res, err := http.DefaultClient.Do(sessionPut)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("bearer put %d", res.StatusCode)
	}
	res.Body.Close()
	statusReq := h.jsonReq(http.MethodGet, "/uploads/"+id, token, nil)
	if statusReq.StatusCode != 401 {
		t.Fatalf("transfer token used as a session: %d", statusReq.StatusCode)
	}
	statusReq.Body.Close()
	anon := h.jsonReq(http.MethodGet, "/uploads/"+id, "", nil)
	if anon.StatusCode != 401 {
		t.Fatalf("anonymous %d", anon.StatusCode)
	}
	anon.Body.Close()
}

func TestNoPlaybackAndNotPublished(t *testing.T) {
	h := newHarness(t, wideLimits(int64(len(shortVideo)*2)), nil)
	_, body := h.intent(h.a.Token, "pub", len(shortVideo), nil)
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	put := h.put(path, token, shortVideo)
	put.Body.Close()
	done := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": shortSum})
	doneBody := decode(t, done)
	if doneBody["status"] != "PROCESSING_QUEUED" || doneBody["publication"] != "NOT_PUBLISHED" {
		t.Fatalf("%#v", doneBody)
	}
	publish := h.jsonReq(http.MethodPost, "/publish", h.a.Token, map[string]any{})
	pubBody := decode(t, publish)
	if publish.StatusCode != http.StatusForbidden || errorCode(t, pubBody) != "PUBLICATION_BLOCKED" {
		t.Fatalf("publish %#v", pubBody)
	}
	after := h.jsonReq(http.MethodGet, "/uploads/"+id, h.a.Token, nil)
	afterBody := decode(t, after)
	if afterBody["status"] != "PROCESSING_QUEUED" {
		t.Fatalf("publish changed status %#v", afterBody)
	}
	for _, path := range []string{
		"/uploads/" + id + "/content",
		"/playback/" + id,
		"/media/" + id + "/index.m3u8",
		"/quarantine/" + objectName(t, id),
		"/signup",
	} {
		res, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == http.StatusOK || bytes.Contains(payload, shortVideo[:8]) {
			t.Fatalf("%s played bytes status %d", path, res.StatusCode)
		}
	}
	signup := h.jsonReq(http.MethodPost, "/accounts", h.a.Token, map[string]any{"name": "nope"})
	if signup.StatusCode != http.StatusNotFound {
		t.Fatalf("signup %d", signup.StatusCode)
	}
	signup.Body.Close()
	if _, err := testDB.Exec(`UPDATE api.uploads SET status = 'PUBLISHED' WHERE upload_id = $1`, id); err == nil {
		t.Fatal("database accepted PUBLISHED")
	}
	var published int
	if err := testDB.QueryRow(`SELECT count(*) FROM api.uploads WHERE status = 'PUBLISHED'`).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != 0 {
		t.Fatalf("published rows %d", published)
	}
}

func TestRateLimitRoleAndProductionFixtures(t *testing.T) {
	limits := wideLimits(1_000_000)
	limits.IntentRatePerMinute = 1
	h := newHarness(t, limits, nil)
	first, _ := h.intent(h.a.Token, "rate-1", 10, nil)
	second, secondBody := h.intent(h.a.Token, "rate-2", 10, nil)
	if first != http.StatusAccepted || second != 429 || errorCode(t, secondBody) != "RATE_LIMIT" {
		t.Fatalf("rate %d %d %#v", first, second, secondBody)
	}
	viewer := Fixture{Token: "dev-fixture-token-view" + h.a.AccountID, AccountID: "view" + h.a.AccountID[4:], Role: "viewer"}
	if !accountRE.MatchString(viewer.AccountID) {
		t.Fatal(viewer.AccountID)
	}
	dir := t.TempDir()
	clk := &clock{at: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	svc, err := New(t.Context(), Options{
		Environment: "development", EnableDevFixtures: true,
		Fixtures: []Fixture{h.a, viewer}, Limits: wideLimits(1000),
		QuarantineDir: dir, Database: testConfig, Now: clk.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := httptest.NewServer(svc.Handler())
	t.Cleanup(srv.Close)
	reqBody, _ := json.Marshal(map[string]any{"idempotency_key": "view-1", "declared_bytes": 10})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/upload-intents", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+viewer.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, res)
	if res.StatusCode != 403 || errorCode(t, got) != "ROLE" {
		t.Fatalf("viewer %#v", got)
	}

	if _, err := New(t.Context(), Options{
		Environment: "production", EnableDevFixtures: true, Fixtures: []Fixture{h.a},
		Limits: wideLimits(1000), QuarantineDir: t.TempDir(), Database: testConfig,
		WebAuthn: testWebAuthn(),
	}); err == nil {
		t.Fatal("production accepted dev fixtures")
	}
	if _, err := New(t.Context(), Options{
		Environment: "production", Limits: wideLimits(1000), QuarantineDir: t.TempDir(),
		Database: testConfig, WebAuthn: testWebAuthn(), Verifier: rejectAll{},
	}); err == nil {
		t.Fatal("production accepted an injected verifier")
	}
	prod, err := New(t.Context(), Options{
		Environment: "production", Limits: wideLimits(1000), QuarantineDir: t.TempDir(),
		Database: testConfig, WebAuthn: testWebAuthn(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prod.Close() })
	prodSrv := httptest.NewServer(prod.Handler())
	t.Cleanup(prodSrv.Close)
	req, _ = http.NewRequest(http.MethodPost, prodSrv.URL+"/upload-intents", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+h.a.Token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("dev fixture token on production: %d", res.StatusCode)
	}
	res.Body.Close()

	root := moduleRoot(t)
	for _, name := range []string{"auth.go", "http.go", "store.go", "service.go"} {
		text, err := os.ReadFile(filepath.Join(root, "internal", "service", name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(text, []byte("acct-dev-a")) || bytes.Contains(text, []byte("dev-fixture-token-creator")) {
			t.Fatalf("%s hard-codes a fixture account", name)
		}
	}
	unknown := h.jsonReq(http.MethodPost, "/upload-intents", h.a.Token, map[string]any{
		"idempotency_key": "who", "declared_bytes": 10, "account_id": h.b.AccountID,
	})
	unknownBody := decode(t, unknown)
	if unknown.StatusCode != 400 {
		t.Fatalf("account id in the body was accepted: %#v", unknownBody)
	}
}

type rejectAll struct{}

func (rejectAll) Verify(string) (Principal, error) { return Principal{}, errUnauthenticated }

func TestDurationIgnoresClientMetadata(t *testing.T) {
	long := encode("2")
	sum := sha256.Sum256(long)
	limits := wideLimits(int64(len(long) * 2))
	limits.MaxDurationSeconds = 1
	h := newHarness(t, limits, nil)
	status, body := h.intent(h.a.Token, "long", len(long), map[string]any{"client_duration_seconds": 0.1})
	if status != http.StatusAccepted {
		t.Fatal(body)
	}
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	put := h.put(path, token, long)
	put.Body.Close()
	res := h.jsonReq(http.MethodPost, "/uploads/"+id+"/complete", h.a.Token, map[string]any{"digest": hex.EncodeToString(sum[:])})
	got := decode(t, res)
	if res.StatusCode != 422 || errorCode(t, got) != "DURATION" {
		t.Fatalf("duration %d %#v", res.StatusCode, got)
	}
	if jobs, _ := h.svc.store.JobCount(context.Background(), id); jobs != 0 {
		t.Fatal("over-duration queued a job")
	}
}

func TestRestartDoesNotDoubleJobOrLeakQuota(t *testing.T) {
	limits := wideLimits(int64(len(shortVideo) * 2))
	a, _ := pair(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	writeConfig(t, cfgPath, a, limits)
	bin := filepath.Join(dir, "svn-api")
	build := exec.Command("go", "build", "-o", bin, "./cmd/svn-api")
	build.Dir = moduleRoot(t)
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	addr := filepath.Join(dir, "addr")
	cmd := startAPI(t, bin, cfgPath, dir, addr, "2s")
	base := "http://" + waitAddr(t, addr)
	client := &http.Client{Timeout: 10 * time.Second}
	payload, _ := json.Marshal(map[string]any{"idempotency_key": "restart", "declared_bytes": len(shortVideo)})
	req, _ := http.NewRequest(http.MethodPost, base+"/upload-intents", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+a.Token)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := decode(t, res)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("intent %#v", body)
	}
	id, _ := body["upload_id"].(string)
	token, path := transfer(t, body)
	putReq, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(shortVideo))
	putReq.Header.Set("Authorization", "Transfer "+token)
	putRes, err := client.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putRes.Body.Close()
	if putRes.StatusCode != http.StatusNoContent {
		t.Fatalf("put %d", putRes.StatusCode)
	}
	completeBody, _ := json.Marshal(map[string]any{"digest": shortSum})
	done := make(chan struct{})
	go func() {
		defer close(done)
		creq, _ := http.NewRequest(http.MethodPost, base+"/uploads/"+id+"/complete", bytes.NewReader(completeBody))
		creq.Header.Set("Authorization", "Bearer "+a.Token)
		cresp, err := client.Do(creq)
		if err == nil {
			cresp.Body.Close()
		}
	}()
	waitStatus(t, client, base+"/uploads/"+id, a.Token, "PROBING")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	<-done
	_ = os.Remove(addr)
	cmd = startAPI(t, bin, cfgPath, dir, addr, "")
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	base = "http://" + waitAddr(t, addr)
	got := waitStatus(t, client, base+"/uploads/"+id, a.Token, "PROCESSING_QUEUED")
	jobs, err := NewStore(testDB).JobCount(context.Background(), id)
	if err != nil || jobs != 1 {
		t.Fatalf("jobs after restart %d %v", jobs, err)
	}
	in, _, err := NewStore(testDB).Quota(context.Background(), a.AccountID)
	if err != nil || in != int64(len(shortVideo)) {
		t.Fatalf("quota after restart %d %v", in, err)
	}
	creq, _ := http.NewRequest(http.MethodPost, base+"/uploads/"+id+"/complete", bytes.NewReader(completeBody))
	creq.Header.Set("Authorization", "Bearer "+a.Token)
	cresp, err := client.Do(creq)
	if err != nil {
		t.Fatal(err)
	}
	again := decode(t, cresp)
	if again["job_id"] != got["job_id"] {
		t.Fatalf("restart created another job %#v %#v", got, again)
	}
	jobs, _ = NewStore(testDB).JobCount(context.Background(), id)
	if jobs != 1 {
		t.Fatalf("jobs after second complete %d", jobs)
	}
	in, _, _ = NewStore(testDB).Quota(context.Background(), a.AccountID)
	if in != int64(len(shortVideo)) {
		t.Fatalf("quota leaked %d", in)
	}
}

func TestCancelReleasesOnce(t *testing.T) {
	h := newHarness(t, wideLimits(1_000_000), nil)
	_, body := h.intent(h.a.Token, "cancel", 20, nil)
	id, _ := body["upload_id"].(string)
	res := h.jsonReq(http.MethodDelete, "/uploads/"+id, h.a.Token, nil)
	got := decode(t, res)
	if res.StatusCode != http.StatusOK || got["status"] != "CANCELLED" {
		t.Fatalf("cancel %#v", got)
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("cancel reserved %d", in)
	}
	res = h.jsonReq(http.MethodDelete, "/uploads/"+id, h.a.Token, nil)
	got = decode(t, res)
	if got["status"] != "CANCELLED" {
		t.Fatalf("second cancel %#v", got)
	}
	if in, _ := h.quota(h.a.AccountID); in != 0 {
		t.Fatalf("second cancel reserved %d", in)
	}
}

func TestDevelopmentConfigIsProposed(t *testing.T) {
	root := moduleRoot(t)
	file, err := LoadFile(filepath.Join(root, "fixtures", "development.json"))
	if err != nil {
		t.Fatal(err)
	}
	if file.PublicLaunch || !file.EnableDevFixtures || file.Environment != "development" {
		t.Fatalf("%#v", file)
	}
	if file.Limits.MaxInputBytes != ProposedMaxInputBytes || file.Limits.MaxDurationSeconds != ProposedMaxDurationSeconds {
		t.Fatalf("caps %#v", file.Limits)
	}
	if file.Limits.Label != "PROPOSED" {
		t.Fatal(file.Limits.Label)
	}
}

func objectName(t *testing.T, id string) string {
	t.Helper()
	var name string
	if err := testDB.QueryRow(`SELECT object_name FROM api.uploads WHERE upload_id = $1`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func writeConfig(t *testing.T, path string, fixture Fixture, limits Limits) {
	t.Helper()
	body, err := json.Marshal(File{
		Environment: "development", EnableDevFixtures: true, PublicLaunch: false,
		Listen: "127.0.0.1:0", Fixtures: []Fixture{fixture}, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func startAPI(t *testing.T, bin, cfg, dir, addr, delay string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C.UTF-8",
		"TZ=UTC",
		"SVN_API_CONFIG=" + cfg,
		"SVN_API_QUARANTINE_DIR=" + dir,
		"SVN_API_LISTEN=127.0.0.1:0",
		"SVN_API_ADDR_FILE=" + addr,
		"SVN_API_PGHOST=" + testConfig.Host,
		"SVN_API_PGPORT=5432",
		"SVN_API_PGDATABASE=" + testConfig.Database,
		"SVN_API_PGUSER=" + testConfig.User,
		"SVN_API_PGPASSWORD=" + testConfig.Password,
	}
	if delay != "" {
		cmd.Env = append(cmd.Env, "SVN_API_PROBE_DELAY="+delay)
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func waitAddr(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil && len(bytes.TrimSpace(body)) > 0 {
			return string(bytes.TrimSpace(body))
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not write its address")
	return ""
}

func waitStatus(t *testing.T, client *http.Client, url, token, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err == nil {
			last = decode(t, res)
			if last["status"] == want {
				return last
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("status did not become %s; last %#v", want, last)
	return nil
}
