package dash

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRolesOnboardingStaleAndActions(t *testing.T) {
	secret := "admin-token-not-a-key"
	narrative := "please review this complaint text"
	var prov providerState
	prov.used = 40
	prov.quota = 100
	prov.declared = 100
	prov.bandwidth = 1000
	prov.disk = 5000
	prov.fetched = 40
	prov.served = 40
	prov.chain = 7
	prov.uncertain = true
	prov.leases = []leaseDoc{{LeaseID: "lshare", ContentID: "cshare", Readiness: "SERVING", PackageBytes: 40}}
	prov.token = secret
	provider := httptest.NewServer(prov.handler())
	defer provider.Close()
	successor := &providerState{token: "other-token", used: 40, quota: 100, declared: 100, disk: 5000}
	succSrv := httptest.NewServer(successor.handler())
	defer succSrv.Close()
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/status":
			_, _ = w.Write([]byte(`{"indexed_height":4,"canonical_height":9,"stale":true,"stale_reasons":["commit_age"]}`))
		case "/v1/deadlines":
			_, _ = w.Write([]byte(`{"deadlines":[{"kind":"lease","id":"lshare","deadline_unix_seconds":10,"expiry_known":true}]}`))
		case "/v1/policy":
			_, _ = w.Write([]byte(`{"present":true,"policy":{"Version":3,"ParameterKey":"page"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer indexer.Close()
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer discovery-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		_, _ = w.Write([]byte(`{"queue":[{"case_id":"case1","status":"open","target_type":"content","target_id":"cshare","reason_code":"placeholder","narrative":"` + narrative + `","complainant":"viewer1"}]}`))
	}))
	defer discovery.Close()
	gov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("governance method %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"proposals":[{"id":"p1","state":"OPEN","approvals":2,"approval_target":3,"execute_after_unix":100,"activation_height":50,"parameter_key":"page","approver_keys":["secret"]}],"upgrade":{"name":"plan-a","height":50,"state":"QUEUED"}}`))
	}))
	defer gov.Close()
	queue := t.TempDir()
	for _, name := range []string{"queued", "running", "ready", "failed"} {
		if err := os.MkdirAll(filepath.Join(queue, "jobs", name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(queue, "jobs", "queued", "job1.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{
		DevSessions: true, ChainID: "shortvideo-dev-1",
		Provider:       &Peer{ID: "alpha", BaseURL: provider.URL, Token: secret},
		Successor:      &Peer{ID: "beta", BaseURL: succSrv.URL, Token: "other-token"},
		IndexerURL:     indexer.URL,
		DiscoveryURL:   discovery.URL,
		DiscoveryToken: "discovery-token",
		MediaQueue:     queue,
		GovernanceURL:  gov.URL,
		Now:            func() time.Time { return time.Unix(100, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(svc.AdminHandler())
	defer admin.Close()
	public := httptest.NewServer(svc.PublicHandler())
	defer public.Close()
	svc.SetOrigins(public.URL, admin.URL)

	viewer := session(t, admin.URL, "viewer")
	providerRole := session(t, admin.URL, "provider")
	moderator := session(t, admin.URL, "moderator")
	council := session(t, admin.URL, "council")

	status := publicJSON(t, public.URL+"/v1/status")
	if strings.Contains(status, secret) || strings.Contains(status, narrative) {
		t.Fatal("public status leaked a token or a complaint")
	}
	if !strings.Contains(status, `"single_machine_demo":true`) || !strings.Contains(status, `"independence_proven":false`) {
		t.Fatal(status)
	}
	disk := reading(t, status, "local_disk_available_bytes")
	if disk.Class != "measured" || disk.Value.(float64) <= 0 {
		t.Fatalf("disk %+v", disk)
	}
	t.Logf("local_disk_available_bytes=%v class=%s", disk.Value, disk.Class)
	if reading(t, status, "chain_height").Class != "stale" {
		t.Fatal("uncertain chain was not stale")
	}
	if reading(t, status, "index_height").Class != "stale" || reading(t, status, "overdue").Class != "stale" {
		t.Fatal("stale indexer was presented as current")
	}
	if reading(t, status, "policy_version").Class != "stale" {
		t.Fatal("policy row on a stale index was current")
	}
	jobs := reading(t, status, "job_queue")
	if jobs.Class != "measured" {
		t.Fatal(jobs)
	}
	if reading(t, status, "scheduler_queue").Class != "absent" {
		t.Fatal("scheduler queue was invented")
	}
	if reading(t, status, "used_bytes").Class != "measured" || reading(t, status, "quota_bytes").Class != "configured" {
		t.Fatal("quota and used were not distinguished")
	}
	if code := postCode(t, public.URL+"/v1/drain", providerRole, map[string]any{"confirm": confirmDrain}); code != 405 {
		t.Fatalf("public drain %d", code)
	}

	onboard := postJSON(t, admin.URL+"/v1/onboarding", providerRole, map[string]any{
		"roles": []string{"storage_delivery"}, "operator_label": "demo", "failure_domain": "single-machine demo",
	}, 200)
	if onboard["storage_forces_validator"] != false || onboard["validator_selected"] != false || onboard["independence_proven"] != false || onboard["single_machine_demo"] != true {
		t.Fatalf("onboard %+v", onboard)
	}
	if strings.Contains(mustJSON(onboard), "universal minimum") == false {
		t.Fatalf("observation note missing: %+v", onboard)
	}
	if code := postCode(t, admin.URL+"/v1/onboarding", providerRole, map[string]any{
		"roles": []string{"storage_delivery"}, "operator_label": "demo", "failure_domain": "single-machine demo", "extra_field": "no",
	}); code != 400 {
		t.Fatalf("unknown field accepted %d", code)
	}
	if code := postCode(t, admin.URL+"/v1/onboarding", viewer, map[string]any{
		"roles": []string{"validator"}, "operator_label": "demo", "failure_domain": "single-machine demo",
	}); code != 403 {
		t.Fatalf("viewer onboard %d", code)
	}

	for _, role := range []string{viewer, moderator, council} {
		if code := postCode(t, admin.URL+"/v1/drain", role, map[string]any{"confirm": confirmDrain}); code != 403 {
			t.Fatalf("drain role %d", code)
		}
		if code := postCode(t, admin.URL+"/v1/limits", role, map[string]any{"quota_bytes": 50, "bandwidth_bytes_per_second": 10, "confirm": confirmLimits}); code != 403 {
			t.Fatalf("limits role %d", code)
		}
	}
	if code := getCode(t, admin.URL+"/v1/moderation/queue", providerRole); code != 403 {
		t.Fatalf("provider queue %d", code)
	}
	queueDoc := getAuth(t, admin.URL+"/v1/moderation/queue", moderator, 200)
	if strings.Contains(queueDoc, narrative) || strings.Contains(queueDoc, "viewer1") || !strings.Contains(queueDoc, "case1") {
		t.Fatal(queueDoc)
	}
	if code := postCode(t, admin.URL+"/v1/governance", council, map[string]any{"proposal": "p1"}); code != 403 {
		t.Fatalf("governance %d", code)
	}
	govBody := postBody(t, admin.URL+"/v1/governance", council, map[string]any{"proposal": "p1"})
	if !strings.Contains(govBody, "GOVERNANCE_NOT_SUBMITTED") || !strings.Contains(govBody, `"chain_submitted":false`) {
		t.Fatal(govBody)
	}
	if code := postCode(t, admin.URL+"/v1/policy", providerRole, map[string]any{"declared_capacity_bytes": 1}); code != 403 {
		t.Fatalf("policy %d", code)
	}
	audit := getAuth(t, admin.URL+"/v1/audit", council, 200)
	if !strings.Contains(audit, "not_submitted") || strings.Contains(audit, secret) {
		t.Fatal(audit)
	}
	if code := postCode(t, admin.URL+"/v1/audit", viewer, nil); code != 403 && code != 405 {
		// GET with a helper that uses GET
	}
	if getCode(t, admin.URL+"/v1/audit", viewer) != 403 {
		t.Fatal("viewer read the audit")
	}

	councilDoc := publicJSON(t, public.URL+"/v1/council")
	if strings.Contains(councilDoc, "approver_keys") || strings.Contains(councilDoc, "secret") {
		t.Fatal(councilDoc)
	}
	if !strings.Contains(councilDoc, `"id":"p1"`) || !strings.Contains(councilDoc, "does not accept a consensus private key") {
		t.Fatal(councilDoc)
	}
	if !strings.Contains(councilDoc, `"activation_height":50`) || !strings.Contains(councilDoc, `"approvals":2`) {
		t.Fatal(councilDoc)
	}

	changed := postJSON(t, admin.URL+"/v1/limits", providerRole, map[string]any{
		"quota_bytes": 40, "bandwidth_bytes_per_second": 10, "confirm": confirmLimits,
	}, 200)
	if changed["chain_submitted"] != false || prov.quota != 40 || prov.bandwidth != 10 {
		t.Fatalf("limits %+v quota %d", changed, prov.quota)
	}
	if code := postCode(t, admin.URL+"/v1/limits", providerRole, map[string]any{
		"quota_bytes": 40, "bandwidth_bytes_per_second": 10, "confirm": confirmLimits, "declared_capacity_bytes": 10,
	}); code != 403 {
		t.Fatalf("declared capacity %d", code)
	}
	drained := postJSON(t, admin.URL+"/v1/drain", providerRole, map[string]any{"confirm": confirmDrain}, 200)
	if drained["chain_submitted"] != false || prov.draining != true {
		t.Fatalf("drain %+v flag %t", drained, prov.draining)
	}
	refused := postJSON(t, admin.URL+"/v1/handoff", providerRole, map[string]any{"confirm": confirmHandoff}, 409)
	if refused["error"] != "last_needed_copy" || refused["chain_submitted"] != false || prov.withdraws != 0 {
		t.Fatalf("early handoff %+v withdraws %d", refused, prov.withdraws)
	}
	successor.leases = []leaseDoc{{LeaseID: "lother", ContentID: "cshare", Readiness: "SERVING", PackageBytes: 40}}
	handed := postJSON(t, admin.URL+"/v1/handoff", providerRole, map[string]any{"confirm": confirmHandoff}, 200)
	if handed["chain_submitted"] != false || prov.withdraws != 1 || prov.used != 0 {
		t.Fatalf("handoff %+v withdraws %d used %d", handed, prov.withdraws, prov.used)
	}
	detail, _ := handed["detail"].(map[string]any)
	if detail["chain_end"] != false {
		t.Fatalf("chain end %+v", handed)
	}
	renewed := postJSON(t, admin.URL+"/v1/renewals/review", providerRole, map[string]any{
		"lease_id": "lshare", "confirm": confirmRenewal, "decision": "hold",
	}, 200)
	if renewed["result"] != "pending" || renewed["chain_submitted"] != false {
		t.Fatalf("renewal %+v", renewed)
	}
	disabled := postJSON(t, admin.URL+"/v1/endpoints/disable", providerRole, map[string]any{"confirm": confirmDisable}, 200)
	if disabled["chain_submitted"] != false || prov.disabled != true {
		t.Fatalf("disable %+v", disabled)
	}
}

func TestFreshIndexIsNotMarkedStaleAndMissingCountersStayAbsent(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"used_bytes":0,"quota_bytes":8,"chain_height":3,"chain_uncertain":false,"leases":[]}`))
	}))
	defer provider.Close()
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			_, _ = w.Write([]byte(`{"indexed_height":3,"canonical_height":3,"stale":false,"stale_reasons":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"deadlines":[]}`))
	}))
	defer indexer.Close()
	svc, err := New(Config{
		Provider:   &Peer{ID: "alpha", BaseURL: provider.URL},
		IndexerURL: indexer.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := svc.Snapshot(t.Context())
	raw, _ := json.Marshal(snap)
	text := string(raw)
	if reading(t, text, "index_height").Class != "measured" || reading(t, text, "chain_height").Class != "measured" {
		t.Fatal(text)
	}
	if reading(t, text, "used_bytes").Class != "measured" || reading(t, text, "used_bytes").Value.(float64) != 0 {
		t.Fatal("a reported zero was dropped")
	}
	if reading(t, text, "bytes_served").Class != "absent" || reading(t, text, "bandwidth_bytes_per_second").Class != "absent" {
		t.Fatal("missing counters were filled in")
	}
	if reading(t, text, "overdue").Class != "measured" {
		t.Fatal(reading(t, text, "overdue"))
	}
}

func TestDevSessionRouteIsAbsentWhenDisabled(t *testing.T) {
	svc, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(svc.AdminHandler())
	defer admin.Close()
	if code := postCode(t, admin.URL+"/v1/dev/session", "", map[string]any{"role": "viewer", "account_id": "viewer1"}); code != 404 {
		t.Fatalf("dev session %d", code)
	}
}

type providerState struct {
	token     string
	used      uint64
	quota     uint64
	declared  uint64
	bandwidth uint64
	disk      uint64
	fetched   uint64
	served    uint64
	chain     uint64
	uncertain bool
	draining  bool
	disabled  bool
	withdraws int
	leases    []leaseDoc
}

func (p *providerState) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/") {
			if r.Header.Get("Authorization") != "Bearer "+p.token {
				http.Error(w, `{"error":"unauthorized"}`, 401)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/status":
			doc := map[string]any{
				"ok": true, "independent_operator": true, "draining": p.draining,
				"used_bytes": p.used, "quota_bytes": p.quota, "declared_capacity_bytes": p.declared,
				"bandwidth_bytes_per_second": p.bandwidth, "bytes_fetched": p.fetched, "bytes_served": p.served,
				"disk_available_bytes": p.disk, "endpoint_enabled": !p.disabled,
				"evidence_mode": "ATTESTATION_NOT_STORAGE_PROOF", "software_version": "development",
				"chain_uncertain": p.uncertain, "chain_height": p.chain, "leases": p.leases,
			}
			_ = json.NewEncoder(w).Encode(doc)
		case "/admin/limits":
			var in struct {
				Quota     uint64 `json:"quota_bytes"`
				Bandwidth uint64 `json:"bandwidth_bytes_per_second"`
				Confirm   string `json:"confirm"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil || in.Confirm != confirmLimits {
				http.Error(w, `{"error":"confirm"}`, 400)
				return
			}
			if in.Quota > p.declared {
				http.Error(w, `{"error":"quota"}`, 400)
				return
			}
			p.quota = in.Quota
			p.bandwidth = in.Bandwidth
			_, _ = w.Write([]byte(`{"chain_submitted":false}`))
		case "/admin/drain":
			p.draining = true
			_, _ = w.Write([]byte(`{"draining":true}`))
		case "/admin/disable":
			p.disabled = true
			_, _ = w.Write([]byte(`{"endpoint_enabled":false,"chain_submitted":false}`))
		case "/admin/withdraw":
			p.withdraws++
			p.used = 0
			p.leases = nil
			_, _ = w.Write([]byte(`{"readiness":"REMOVED","chain_end":false}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func session(t *testing.T, admin, role string) string {
	t.Helper()
	doc := postJSON(t, admin+"/v1/dev/session", "", map[string]any{"role": role, "account_id": role + "1"}, 200)
	token, _ := doc["token"].(string)
	if token == "" || doc["consensus_key"] != false {
		t.Fatal(doc)
	}
	return token
}

func publicJSON(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("%s %d %s", url, res.StatusCode, buf)
	}
	return string(buf)
}

func reading(t *testing.T, body, name string) Reading {
	t.Helper()
	var snap struct {
		Readings []Reading `json:"readings"`
	}
	if json.Unmarshal([]byte(body), &snap) != nil {
		t.Fatal(body)
	}
	for _, row := range snap.Readings {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("missing %s in %s", name, body)
	return Reading{}
}

func postJSON(t *testing.T, url, token string, body map[string]any, want int) map[string]any {
	t.Helper()
	code, raw := postOnce(t, url, token, body)
	var doc map[string]any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		t.Fatal(raw)
	}
	if code != want {
		t.Fatalf("%s %d %s", url, code, raw)
	}
	return doc
}

func postBody(t *testing.T, url, token string, body map[string]any) string {
	t.Helper()
	_, raw := postOnce(t, url, token, body)
	return raw
}

func postCode(t *testing.T, url, token string, body map[string]any) int {
	t.Helper()
	code, _ := postOnce(t, url, token, body)
	return code
}

func postOnce(t *testing.T, url, token string, body map[string]any) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

func getAuth(t *testing.T, url, token string, want int) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		t.Fatalf("%s %d %s", url, res.StatusCode, buf)
	}
	return string(buf)
}

func getCode(t *testing.T, url, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
