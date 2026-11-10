package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/sched"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/stage"
)

func buildProvider(t *testing.T) string {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	bin := filepath.Join(t.TempDir(), "svn-provider")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/svn-provider")
	cmd.Dir = filepath.Join(root, "agents", "provider")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build provider: %v %s", err, out)
	}
	return bin
}

type provider struct {
	t     *testing.T
	bin   string
	id    string
	cfg   string
	dir   string
	ca    string
	url   string
	admin string
	cmd   *exec.Cmd
	buf   *bytes.Buffer
}

func startProvider(t *testing.T, bin, id string, quota uint64, src *stage.Stage, chain *chainFix) *provider {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "data")
	cfg := filepath.Join(base, "config.json")
	host, port := src.Addr()
	ch, cp, err := net.SplitHostPort(chain.addr)
	if err != nil {
		t.Fatal(err)
	}
	csp, _ := strconv.Atoi(cp)
	body := map[string]any{
		"provider_id": id, "key_id": "k" + id, "machine_id": "vm-1", "listen": "127.0.0.1:0",
		"data_dir": dir, "hosting_consent": true, "declared_capacity_bytes": quota, "quota_bytes": quota,
		"min_free_bytes": 0, "bandwidth_bytes_per_second": 1 << 20, "transfer_budget_seconds": 30,
		"max_package_bytes": 1 << 20, "max_term_seconds": 100000, "clock_uncertainty_seconds": 0,
		"stall_after_seconds": 3600, "chain_id": "shortvideo-dev-1", "chain_url": "https://" + chain.addr,
		"source_ca": filepath.Join(base, "source.pem"), "chain_ca": filepath.Join(base, "chain.pem"),
		"destinations":       []map[string]any{{"host": host, "ip": host, "port": port}},
		"chain_destinations": []map[string]any{{"host": ch, "ip": ch, "port": csp}},
	}
	raw, _ := json.MarshalIndent(body, "", "  ")
	if err := os.WriteFile(cfg, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(body["source_ca"].(string), src.CAPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(body["chain_ca"].(string), chain.pem, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "init", cfg).CombinedOutput()
	if err != nil {
		t.Fatalf("init %s: %v %s", id, err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.token"), []byte("source-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approvals.json"), []byte("{\"offer_ids\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	admin, err := os.ReadFile(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	item := &provider{
		t: t, bin: bin, id: id, cfg: cfg, dir: dir, ca: filepath.Join(dir, "tls.crt"),
		admin: strings.TrimSpace(string(admin)), buf: &bytes.Buffer{},
	}
	item.start(t)
	t.Cleanup(item.stop)
	return item
}

func (p *provider) start(t *testing.T) {
	t.Helper()
	_ = os.Remove(filepath.Join(p.dir, "listen.json"))
	p.buf = &bytes.Buffer{}
	cmd := exec.Command(p.bin, "run", p.cfg)
	cmd.Stdout = p.buf
	cmd.Stderr = p.buf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p.cmd = cmd
	var listen struct {
		URL string `json:"url"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(filepath.Join(p.dir, "listen.json"))
		if err == nil && json.Unmarshal(body, &listen) == nil && listen.URL != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if listen.URL == "" {
		t.Fatalf("listen %s: %s", p.id, p.buf.String())
	}
	p.url = strings.TrimRight(listen.URL, "/")
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := p.client().Get(p.url + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("health %s: %s", p.id, p.buf.String())
}

func (p *provider) stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
		p.cmd = nil
	}
}

func (p *provider) client() *http.Client {
	pool := x509.NewCertPool()
	pemBytes, err := os.ReadFile(p.ca)
	if err != nil || !pool.AppendCertsFromPEM(pemBytes) {
		p.t.Fatalf("ca %s: %v", p.id, err)
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
}

func (p *provider) approve(offer string) error {
	path := filepath.Join(p.dir, "approvals.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var file struct {
		OfferIDs []string `json:"offer_ids"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return errors.New("approvals")
	}
	for _, id := range file.OfferIDs {
		if id == offer {
			return nil
		}
	}
	file.OfferIDs = append(file.OfferIDs, offer)
	body, _ := json.Marshal(file)
	return os.WriteFile(path, body, 0o644)
}

func (p *provider) packageStatus(t *testing.T, content, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, p.url+"/packages/"+content+"/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := p.client().Do(req)
	if err != nil {
		return 0
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func (p *provider) packageBody(t *testing.T, content, token string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, p.url+"/packages/"+content+"/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := p.client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("package %s %d %s", p.id, res.StatusCode, buf)
	}
	return buf
}

type chainFix struct {
	srv       *httptest.Server
	addr      string
	pem       []byte
	mu        sync.Mutex
	height    uint64
	txs       int
	stale     bool
	leases    []sched.Posted
	active    map[string]bool
	reserved  map[string]uint64
	content   map[string]bool
	gone      map[string]bool
	deadlines map[string]int64
}

func newChain() *chainFix {
	c := &chainFix{
		active: map[string]bool{}, reserved: map[string]uint64{},
		content: map[string]bool{}, gone: map[string]bool{}, deadlines: map[string]int64{},
	}
	c.srv = httptest.NewTLSServer(http.HandlerFunc(c.serveChain))
	c.addr = c.srv.Listener.Addr().String()
	c.pem = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.srv.Certificate().Raw})
	return c
}

func (c *chainFix) close() { c.srv.Close() }

func (c *chainFix) addContent(id string, deadline int64) {
	c.mu.Lock()
	c.content[id] = true
	c.deadlines[id] = deadline
	c.mu.Unlock()
}

func (c *chainFix) setWithdrawn(id string) {
	c.mu.Lock()
	c.gone[id] = true
	c.content[id] = true
	c.mu.Unlock()
}

func (c *chainFix) commit(p sched.Posted) bool {
	for _, item := range c.leases {
		if item.ProviderID == p.ProviderID && item.ContentID == p.ContentID && item.Version == p.Version && c.active[item.LeaseID] {
			return false
		}
	}
	c.leases = append(c.leases, p)
	c.active[p.LeaseID] = true
	c.reserved[p.ProviderID] += p.PackageBytes
	return true
}

func (c *chainFix) activeCount(content string) int {
	n := 0
	for _, item := range c.leases {
		if item.ContentID == content && c.active[item.LeaseID] {
			n++
		}
	}
	return n
}

func (c *chainFix) serveChain(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/status":
		c.mu.Lock()
		c.height++
		h := c.height
		c.mu.Unlock()
		fmt.Fprintf(w, `{"result":{"sync_info":{"latest_block_height":"%d","catching_up":false}}}`, h)
	case r.URL.Path == "/account":
		_, _ = w.Write([]byte(`{"account_number":"0","sequence":"0"}`))
	case strings.HasPrefix(r.URL.Path, "/lease/"):
		id := strings.TrimPrefix(r.URL.Path, "/lease/")
		c.mu.Lock()
		on := c.active[id]
		c.mu.Unlock()
		if !on {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"state":"ACTIVE"}`)
	case r.URL.Path == "/broadcast" && r.Method == http.MethodPost:
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		c.mu.Lock()
		c.txs++
		c.mu.Unlock()
		_, _ = w.Write([]byte(`{"code":0}`))
	default:
		http.NotFound(w, r)
	}
}

func (c *chainFix) serveIndex(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/status":
		fmt.Fprintf(w, `{"stale":%t,"stale_reasons":[],"chain_id":"shortvideo-dev-1"}`, c.stale)
	case "/v1/providers":
		_, _ = w.Write([]byte(`{"providers":[{"ID":"p1","State":"ADMITTED"},{"ID":"p2","State":"ADMITTED"},{"ID":"p3","State":"ADMITTED"},{"ID":"p4","State":"ADMITTED"},{"ID":"pcap","State":"ADMITTED"}]}`))
	case "/v1/content":
		rows := []map[string]any{}
		for id := range c.content {
			rows = append(rows, map[string]any{"ID": id, "Version": 1, "Withdrawn": c.gone[id]})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": rows})
	case "/v1/leases":
		rows := []map[string]any{}
		for _, item := range c.leases {
			rows = append(rows, map[string]any{
				"ID": item.LeaseID, "ProviderID": item.ProviderID, "ContentID": item.ContentID,
				"Version": item.Version, "State": "ACTIVE", "Term": 1, "Deadline": c.deadlines[item.ContentID],
				"Active": c.active[item.LeaseID],
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"leases": rows})
	default:
		http.NotFound(w, r)
	}
}

func applyLoop(chain *chainFix, coordinator *sched.Sched, stop <-chan struct{}) {
	applied := 0
	for {
		select {
		case <-stop:
			return
		default:
		}
		posted := coordinator.Posted()
		chain.mu.Lock()
		for applied < len(posted) {
			if !chain.commit(posted[applied]) {
				break
			}
			applied++
		}
		chain.mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
}

func skipped(coordinator *sched.Sched, provider, reason string) bool {
	for _, line := range coordinator.Traces() {
		if strings.Contains(line, "skip provider="+provider+" reason="+reason) {
			return true
		}
	}
	return false
}

var (
	shaMu sync.Mutex
	shas  = map[string]string{}
)

func remember(content, sum string) {
	shaMu.Lock()
	shas[content] = sum
	shaMu.Unlock()
}

func pubSHA(t *testing.T, content string) string {
	t.Helper()
	shaMu.Lock()
	defer shaMu.Unlock()
	sum := shas[content]
	if sum == "" {
		t.Fatal("missing sha")
	}
	return sum
}

func hexSum(buf []byte) string {
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

func knownBytes(content string) []byte {
	buf := make([]byte, 64)
	sum := sha256.Sum256([]byte(content))
	copy(buf, sum[:])
	copy(buf[32:], sum[:])
	buf[32] ^= 0xff
	return buf
}

func packageFor(content string, deadline int64) sched.Package {
	raw := knownBytes(content)
	segs := []policy.Segment{
		{ID: "s0", SHA256: hexSum(raw[:32]), ByteCount: 32},
		{ID: "s1", SHA256: hexSum(raw[32:]), ByteCount: 32},
	}
	terms := sha256.Sum256([]byte("terms:" + content))
	return sched.Package{
		ContentID: content, Version: 1, Segments: segs, Deadline: deadline,
		ExpectedSHA: hexSum(raw), TermsHex: hex.EncodeToString(terms[:]),
	}
}

func putOrigin(src *stage.Stage, content string) {
	pkg := packageFor(content, time.Now().Unix()+3600)
	parts := map[string][]byte{}
	raw := knownBytes(content)
	off := 0
	for _, seg := range pkg.Segments {
		parts[seg.ID] = append([]byte(nil), raw[off:off+int(seg.ByteCount)]...)
		off += int(seg.ByteCount)
	}
	src.PutOrigin(content, stage.Object{
		Manifest: policy.ManifestText(content, 1, 64, pkg.Segments), Parts: parts,
	})
	remember(content, pkg.ExpectedSHA)
}

func leaseToken(coordinator *sched.Sched, content, provider string) string {
	for _, post := range coordinator.Posted() {
		if post.ContentID == content && post.ProviderID == provider {
			return coordinator.Token(post.LeaseID)
		}
	}
	return ""
}
