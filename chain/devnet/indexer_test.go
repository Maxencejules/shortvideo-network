package devnet_test

// TestDevnetIndexer is the goal 12 acceptance test. It runs four svnd
// processes on one machine and the svn-indexer binary against a disposable
// PostgreSQL cluster. It is not an independent-operator network.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
)

func TestDevnetIndexer(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip("SVN_SKIP_DEVNET=1")
	}
	fmt.Println("label=single machine, four validator processes, private network namespace (not independent operators, not production)")
	bin := buildIndexer(t)
	home := t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	stops := make([]func() error, 4)
	for i := range stops {
		stop, err := launch(layout.Topology.Validators[i].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[i] = stop
	}
	t.Cleanup(func() {
		for _, stop := range stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	rpcs := rpcList(layout)
	if _, _, err := devnet.WaitEqual(rpcs, 3, 45*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	db := startIndexerDB(t, bin, layout.Topology.ChainID)
	cfgPath := writeIndexerConfig(t, db, layout.Topology.ChainID, layout.Topology.Validators[0].RPC, 3)
	runIndexer(t, bin, "migrate", "--config", cfgPath)

	// Injections against the real node, through a proxy, before any height is checkpointed.
	proxyChainID(t, bin, db, layout.Topology.ChainID, layout.Topology.Validators[0].RPC)
	proxyForge(t, bin, db, layout.Topology.ChainID, layout.Topology.Validators[0].RPC, "forged")
	proxyForge(t, bin, db, layout.Topology.ChainID, layout.Topology.Validators[0].RPC, "missing")
	proxyForge(t, bin, db, layout.Topology.ChainID, layout.Topology.Validators[0].RPC, "version")
	text := runIndexerOut(t, bin, "canonical", "--config", cfgPath)
	if strings.Contains(text, "content ") {
		t.Fatalf("an injection wrote a content row:\n%s", text)
	}

	d := newDemo(t, layout, home)
	d.commit("register-c1", d.service, d.registerContent(d.service, "reg-c1", "c1", 1, sizeOne))
	fault := t.TempDir()
	if err := os.WriteFile(filepath.Join(fault, "after-effects-before-checkpoint.arm"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	serve := startIndexer(t, bin, []string{"serve", "--config", cfgPath}, "SVN_FAULTPOINT_DIR="+fault)
	waitFor(t, "fault point", 90*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(fault, "after-effects-before-checkpoint.hit"))
		return err == nil
	})
	mid := runIndexerOut(t, bin, "canonical", "--config", cfgPath)
	if strings.Contains(mid, "content c1") {
		t.Fatalf("content was visible before the checkpoint:\n%s", mid)
	}
	serve.kill()

	serve = startIndexer(t, bin, []string{"serve", "--config", cfgPath}, "")
	waitFor(t, "one content row", 90*time.Second, func() bool {
		return strings.Count(runIndexerOut(t, bin, "canonical", "--config", cfgPath), "\ncontent c1 ") == 1
	})
	if strings.Contains(runIndexerOut(t, bin, "canonical", "--config", cfgPath), "\nlease ") {
		t.Fatal("a lease appeared without a lease event")
	}

	// Stop two validators. The chain cannot commit. The read becomes stale
	// and the content row does not gain a renewal or an approval.
	if _, err := stopForStall("indexer-stall-1", layout, stops, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := stopForStall("indexer-stall-2", layout, stops, 2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stale read", 30*time.Second, func() bool {
		raw := runIndexerOut(t, bin, "status", "--config", cfgPath)
		var view struct {
			Stale        bool     `json:"stale"`
			StaleReasons []string `json:"stale_reasons"`
		}
		if err := json.Unmarshal([]byte(raw), &view); err != nil {
			return false
		}
		return view.Stale && (indexerHas(view.StaleReasons, "commit_age") || indexerHas(view.StaleReasons, "unreachable") || indexerHas(view.StaleReasons, "observer_stale"))
	})
	frozen := runIndexerOut(t, bin, "canonical", "--config", cfgPath)
	if strings.Count(frozen, "\ncontent c1 ") != 1 || strings.Contains(frozen, "\nlease ") {
		t.Fatalf("stall changed the projection:\n%s", frozen)
	}

	for _, index := range []int{2, 3} {
		stop, err := launch(layout.Topology.Validators[index].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[index] = stop
	}
	if _, _, err := devnet.WaitEqual(rpcs, 1, 60*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	waitFor(t, "fresh read", 45*time.Second, func() bool {
		raw := runIndexerOut(t, bin, "status", "--config", cfgPath)
		var view struct {
			Stale bool `json:"stale"`
		}
		if err := json.Unmarshal([]byte(raw), &view); err != nil {
			return false
		}
		return !view.Stale
	})

	serve.kill()
	before := projectionLines(runIndexerOut(t, bin, "canonical", "--config", cfgPath))
	runIndexer(t, bin, "rebuild", "--config", cfgPath)
	runIndexer(t, bin, "backfill", "--config", cfgPath)
	after := projectionLines(runIndexerOut(t, bin, "canonical", "--config", cfgPath))
	if after != before {
		t.Fatalf("rebuild projection differs\nbefore:\n%s\nafter:\n%s", before, after)
	}

	psql(t, db, `UPDATE indexer.content SET manifest_sha256 = repeat('c', 64) WHERE content_id = 'c1'`)
	cmd := exec.Command(bin, "reconcile-once", "--config", cfgPath)
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil && err != nil {
		t.Fatal(err)
	}
	if cmd.ProcessState.ExitCode() != 5 {
		t.Fatalf("reconcile exit %d\n%s", cmd.ProcessState.ExitCode(), out)
	}
	if !bytes.Contains(out, []byte("divergent")) {
		t.Fatalf("reconcile output %s", out)
	}
	altered := runIndexerOut(t, bin, "canonical", "--config", cfgPath)
	if !strings.Contains(altered, strings.Repeat("c", 64)) {
		t.Fatal("reconciliation rewrote the altered manifest")
	}
	fmt.Println("phase=done")
}

func indexerHas(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func projectionLines(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "checkpoint ") || line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func buildIndexer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "svn-indexer")
	module, err := filepath.Abs(filepath.Join("..", "..", "services", "indexer"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"build", "-o", bin, "-tags", "svn_faultpoint"}
	if broadcasterRace {
		args = append(args, "-race")
	}
	args = append(args, "./cmd/svn-indexer")
	cmd := exec.Command("go", args...)
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build indexer: %v\n%s", err, out)
	}
	return bin
}

type indexerDB struct {
	socket              string
	port                int
	database            string
	ownerPassword       string
	runtimePassword     string
	ownerPasswordFile   string
	runtimePasswordFile string
}

func startIndexerDB(t *testing.T, bin, chainID string) indexerDB {
	t.Helper()
	secrets := t.TempDir()
	cmd := exec.Command(bin, "dev-db", "--chain-id", chainID, "--secrets-dir", secrets)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		SocketDir string `json:"socket_dir"`
		Port      int    `json:"port"`
		Database  string `json:"database"`
		Roles     []struct {
			Role         string `json:"role"`
			PasswordFile string `json:"password_file"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(line, &info); err != nil {
		t.Fatalf("dev-db: %v\n%s", err, line)
	}
	db := indexerDB{socket: info.SocketDir, port: info.Port, database: info.Database}
	for _, role := range info.Roles {
		pass, err := os.ReadFile(role.PasswordFile)
		if err != nil {
			t.Fatal(err)
		}
		switch role.Role {
		case "svn_indexer_owner":
			db.ownerPassword = strings.TrimSpace(string(pass))
			db.ownerPasswordFile = role.PasswordFile
		case "svn_indexer":
			db.runtimePassword = strings.TrimSpace(string(pass))
			db.runtimePasswordFile = role.PasswordFile
		}
	}
	if db.ownerPassword == "" || db.runtimePassword == "" {
		t.Fatalf("roles %+v", info.Roles)
	}
	return db
}

func writeIndexerConfig(t *testing.T, db indexerDB, chainID, rpc string, stale int) string {
	t.Helper()
	body := map[string]any{
		"chain_id": chainID,
		"rpc_url":  rpc,
		"listen":   "127.0.0.1:0",
		"database": map[string]any{
			"socket_dir": db.socket, "port": db.port, "name": db.database,
			"owner_role": "svn_indexer_owner", "owner_password_file": db.ownerPasswordFile,
			"role": "svn_indexer", "password_file": db.runtimePasswordFile,
			"application_name": "svn-indexer", "connect_timeout_seconds": 5,
		},
		"numbers": map[string]any{
			"stale_after_seconds": stale, "poll_ms": 200, "reconcile_ms": 1000, "rpc_timeout_seconds": 10,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "indexer.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type running struct {
	cmd *exec.Cmd
}

func startIndexer(t *testing.T, bin string, args []string, extraEnv string) *running {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if extraEnv != "" {
		cmd.Env = append(os.Environ(), extraEnv)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	proc := &running{cmd: cmd}
	t.Cleanup(proc.kill)
	return proc
}

func (r *running) kill() {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return
	}
	_ = r.cmd.Process.Kill()
	_, _ = r.cmd.Process.Wait()
	r.cmd.Process = nil
}

func runIndexer(t *testing.T, bin string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runIndexerOut(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func waitFor(t *testing.T, what string, timeout time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			fmt.Printf("phase=wait what=%s ok\n", what)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func psql(t *testing.T, db indexerDB, statement string) {
	t.Helper()
	cmd := exec.Command("psql", "-h", db.socket, "-p", fmt.Sprint(db.port), "-U", "svn_indexer_owner", "-d", db.database, "-c", statement)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+db.ownerPassword)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("psql: %v\n%s", err, out)
	}
}

func proxyChainID(t *testing.T, bin string, db indexerDB, chainID, rpc string) {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := forward(t, rpc, r)
		body = bytes.ReplaceAll(body, []byte(chainID), []byte(chainID+"x"))
		_, _ = w.Write(body)
	}))
	defer proxy.Close()
	cfg := writeIndexerConfig(t, db, chainID, proxy.URL, 3)
	cmd := exec.Command(bin, "backfill", "--config", cfg)
	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 3 || !bytes.Contains(out, []byte("chain id")) {
		t.Fatalf("chain id injection exit %d\n%s", cmd.ProcessState.ExitCode(), out)
	}
	fmt.Println("phase=inject-chain-id exit=3")
}

func proxyForge(t *testing.T, bin string, db indexerDB, chainID, rpc, mode string) {
	t.Helper()
	var seen []byte
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(req))
		body := forward(t, rpc, r)
		switch mode {
		case "missing":
			if bytes.Contains(req, []byte("block_results")) || strings.Contains(r.URL.Path, "block_results") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"history_unavailable","data":""}}`))
				return
			}
		case "forged":
			if bytes.Contains(req, []byte(`"method":"commit"`)) {
				body = forgeCommitHash(body)
			}
		case "version":
			if bytes.Contains(req, []byte("block_results")) || strings.Contains(r.URL.Path, "block_results") {
				seen = append([]byte(nil), body...)
				body = injectVersion(body)
			}
		}
		_, _ = w.Write(body)
	}))
	defer proxy.Close()
	cfg := writeIndexerConfig(t, db, chainID, proxy.URL, 3)
	cmd := exec.Command(bin, "backfill", "--config", cfg)
	out, _ := cmd.CombinedOutput()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	want := 3
	needle := []byte("forged")
	if mode == "missing" {
		want = 4
		needle = []byte("history unavailable")
	}
	if mode == "version" {
		needle = []byte("event version")
	}
	if code != want || !bytes.Contains(bytes.ToLower(out), needle) {
		t.Fatalf("%s injection exit %d\n%s\nbody %s", mode, code, out, seen)
	}
	fmt.Printf("phase=inject-%s exit=%d\n", mode, want)
}

func forgeCommitHash(body []byte) []byte {
	out := append([]byte(nil), body...)
	mark := []byte(`"commit":`)
	key := []byte(`"block_id":{"hash":"`)
	at := bytes.Index(out, mark)
	if at < 0 {
		return out
	}
	rel := bytes.Index(out[at:], key)
	if rel < 0 || at+rel+len(key)+64 > len(out) {
		return out
	}
	start := at + rel + len(key)
	for i := 0; i < 64; i++ {
		out[start+i] = 'a'
	}
	return out
}

func injectVersion(body []byte) []byte {
	event := []byte(`"finalize_block_events":[{"type":"svn.events.v9.ContentRegistered","attributes":[]}]`)
	for _, needle := range [][]byte{
		[]byte(`"finalize_block_events":[]`),
		[]byte(`"finalize_block_events":null`),
	} {
		if bytes.Contains(body, needle) {
			return bytes.Replace(body, needle, event, 1)
		}
	}
	return body
}

func forward(t *testing.T, rpc string, r *http.Request) []byte {
	t.Helper()
	body, _ := io.ReadAll(r.Body)
	req, err := http.NewRequest(r.Method, strings.TrimRight(rpc, "/")+r.URL.RequestURI(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
