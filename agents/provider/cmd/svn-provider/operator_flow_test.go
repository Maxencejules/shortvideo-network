package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/operator/dash"
)

func TestDashboardOnboardsAcceptsDeliversDrainsAndHandsOff(t *testing.T) {
	dir := t.TempDir()
	src := newPackageServer(t, "source-token")
	defer src.close()
	chain := newChainServer(t)
	defer chain.close()
	srcCA := writeCert(t, filepath.Join(dir, "source-ca.pem"), src.srv)
	chainCA := writeCert(t, filepath.Join(dir, "chain-ca.pem"), chain.srv)
	alpha := newRunning(t, "alpha", "ak", "vm-1", 1<<20, 0, 1<<20, 30, 2, 1, src, chain, srcCA, chainCA)
	beta := newRunning(t, "beta", "bk", "vm-1", 1<<20, 0, 1<<20, 30, 2, 1, src, chain, srcCA, chainCA)
	defer alpha.stop()
	defer beta.stop()

	svc, err := dash.New(dash.Config{
		DevSessions: true,
		Provider: &dash.Peer{
			ID: "alpha", BaseURL: alpha.url,
			CAFile: filepath.Join(alpha.dir, "tls.crt"), TokenFile: filepath.Join(alpha.dir, "admin.token"),
		},
		Successor: &dash.Peer{
			ID: "beta", BaseURL: beta.url,
			CAFile: filepath.Join(beta.dir, "tls.crt"), TokenFile: filepath.Join(beta.dir, "admin.token"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(svc.AdminHandler())
	defer admin.Close()
	public := httptest.NewServer(svc.PublicHandler())
	defer public.Close()
	token := dashSession(t, admin.URL, "provider")
	viewer := dashSession(t, admin.URL, "viewer")
	moderator := dashSession(t, admin.URL, "moderator")
	council := dashSession(t, admin.URL, "council")

	onboard := dashPost(t, admin.URL+"/v1/onboarding", token, map[string]any{
		"roles": []string{"storage_delivery", "processing"}, "operator_label": "demo", "failure_domain": "single-machine demo",
	}, 200)
	if onboard["validator_selected"] != false || onboard["storage_forces_validator"] != false || onboard["single_machine_demo"] != true {
		t.Fatalf("onboard %+v", onboard)
	}
	fmt.Printf("ONBOARD storage=%t validator=%t forces_validator=%t single_machine=%t\n",
		onboard["storage_selected"], onboard["validator_selected"], onboard["storage_forces_validator"], onboard["single_machine_demo"])

	shared := putPackage(t, src, "cshare")
	alpha.approve(t, shared.id)
	accepted := alpha.accept(t, shared, 200)
	if accepted.Readiness != "ACCEPTANCE_PENDING" {
		t.Fatalf("acceptance %+v", accepted)
	}
	chain.set(shared.lease, "ACTIVE")
	sum, status := alpha.fetchPackage(t, shared.content, accepted.ServeToken)
	if status != 200 || sum != shared.sum {
		t.Fatalf("delivery %d", status)
	}
	body := dashGet(t, public.URL+"/v1/status")
	if strings.Contains(body, alpha.adminToken(t)) {
		t.Fatal("status included the admin token")
	}
	if !strings.Contains(body, `"independence_proven":false`) || !strings.Contains(body, "ATTESTATION_NOT_STORAGE_PROOF") {
		t.Fatal(body)
	}
	if !strings.Contains(body, `"name":"bytes_served"`) || !strings.Contains(body, "cshare") {
		t.Fatal(body)
	}
	used := alpha.status(t).UsedBytes
	fmt.Printf("DELIVERY status=%d used_bytes=%d disk_available_bytes=%d\n", status, used, alpha.status(t).DiskAvailableBytes)
	if used != shared.total {
		t.Fatalf("used %d package %d", used, shared.total)
	}

	changed := dashPost(t, admin.URL+"/v1/limits", token, map[string]any{
		"quota_bytes": used, "bandwidth_bytes_per_second": 1 << 20, "confirm": "CHANGE LIMITS",
	}, 200)
	if changed["chain_submitted"] != false {
		t.Fatal(changed)
	}
	next := putPackage(t, src, "cnext")
	alpha.approve(t, next.id)
	code, msg := alpha.acceptCode(t, next)
	if code == 200 || msg != "insufficient quota" {
		t.Fatalf("exhaust %d %s", code, msg)
	}
	fmt.Printf("EXHAUST quota=%d status=%d error=%s\n", used, code, msg)

	beta.approve(t, shared.id)
	betaAccept := beta.accept(t, shared, 200)
	chain.set(shared.lease, "ACTIVE")
	if _, bstatus := beta.fetchPackage(t, shared.content, betaAccept.ServeToken); bstatus != 200 {
		t.Fatalf("successor delivery %d", bstatus)
	}
	before := chain.n()
	drained := dashPost(t, admin.URL+"/v1/drain", token, map[string]any{"confirm": "DRAIN"}, 200)
	if drained["chain_submitted"] != false || alpha.status(t).Draining != true {
		t.Fatalf("drain %+v", drained)
	}
	if got := alpha.getPackage(t, shared.content, accepted.ServeToken); got != 200 {
		t.Fatalf("drain stopped serving %d", got)
	}
	again := putPackage(t, src, "cdrain")
	alpha.approve(t, again.id)
	if _, msg := alpha.acceptCode(t, again); msg != "draining" {
		t.Fatalf("stop offers: %s", msg)
	}
	fmt.Printf("DRAIN serving=%d new_offer=draining\n", 200)

	handed := dashPost(t, admin.URL+"/v1/handoff", token, map[string]any{"confirm": "HANDOFF"}, 200)
	alphaCode := alpha.getPackage(t, shared.content, accepted.ServeToken)
	_, betaCode := beta.fetchPackage(t, shared.content, betaAccept.ServeToken)
	if handed["chain_submitted"] != false || alphaCode == 200 || betaCode != 200 {
		t.Fatalf("handoff %+v alpha %d beta %d", handed, alphaCode, betaCode)
	}
	if chain.n() != before {
		t.Fatalf("handoff submitted %d chain calls", chain.n()-before)
	}
	fmt.Printf("HANDOFF alpha=%d beta=%d used_after=%d broadcasts_added=%d\n", alphaCode, betaCode, alpha.status(t).UsedBytes, chain.n()-before)

	if code := dashCode(t, admin.URL+"/v1/drain", viewer, map[string]any{"confirm": "DRAIN"}); code != 403 {
		t.Fatalf("viewer drain %d", code)
	}
	if dashGetCode(t, admin.URL+"/v1/moderation/queue", token) != 403 {
		t.Fatal("provider read the queue")
	}
	if dashGetCode(t, admin.URL+"/v1/moderation/queue", moderator) != 503 {
		t.Fatal("moderator queue should be unconfigured, not an empty success")
	}
	gov := dashPost(t, admin.URL+"/v1/governance", council, map[string]any{"sign": true}, 403)
	if gov["error"] != "GOVERNANCE_NOT_SUBMITTED" || gov["chain_submitted"] != false || chain.n() != before {
		t.Fatalf("governance %+v broadcasts %d", gov, chain.n()-before)
	}
	disabled := dashPost(t, admin.URL+"/v1/endpoints/disable", token, map[string]any{"confirm": "DISABLE ENDPOINT"}, 200)
	if disabled["chain_submitted"] != false {
		t.Fatal(disabled)
	}
	if got := alpha.getPackage(t, shared.content, accepted.ServeToken); got != 503 {
		t.Fatalf("disabled endpoint %d", got)
	}
	fmt.Printf("DISABLE package_status=%d chain_submitted=false\n", 503)
}

func dashSession(t *testing.T, admin, role string) string {
	t.Helper()
	doc := dashPost(t, admin+"/v1/dev/session", "", map[string]any{"role": role, "account_id": role + "1"}, 200)
	token, _ := doc["token"].(string)
	if token == "" {
		t.Fatal(doc)
	}
	return token
}

func dashPost(t *testing.T, url, token string, body map[string]any, want int) map[string]any {
	t.Helper()
	code, raw := dashSend(t, http.MethodPost, url, token, body)
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil || code != want {
		t.Fatalf("%s %d %s", url, code, raw)
	}
	return doc
}

func dashCode(t *testing.T, url, token string, body map[string]any) int {
	t.Helper()
	code, _ := dashSend(t, http.MethodPost, url, token, body)
	return code
}

func dashGet(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("get %d %s", res.StatusCode, buf)
	}
	return string(buf)
}

func dashGetCode(t *testing.T, url, token string) int {
	t.Helper()
	code, _ := dashSend(t, http.MethodGet, url, token, nil)
	return code
}

func dashSend(t *testing.T, method, url, token string, body map[string]any) (int, []byte) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, url, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	return res.StatusCode, buf
}
