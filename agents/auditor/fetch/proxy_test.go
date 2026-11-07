package fetch

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch/fetchtest"
)

const (
	proxyChildEnv  = "SVN_AUDITOR_PROXY_CHILD"
	proxyResultEnv = "SVN_AUDITOR_PROXY_RESULT"
)

// proxyChildResult is what the child process reports to the parent.
type proxyChildResult struct {
	// EnvironmentRoutesToProxy is true when http.ProxyFromEnvironment, in
	// the child's environment, would send the test request to the proxy.
	EnvironmentRoutesToProxy bool     `json:"environment_routes_to_proxy"`
	Kind                     string   `json:"kind"`
	BytesRead                uint64   `json:"bytes_read"`
	Dials                    []string `json:"dials"`
	Allowlisted              []string `json:"allowlisted"`
}

// TestFetchIgnoresProxyEnvironment runs the fetch in a re-executed child
// process, because Go reads the proxy variables once per process. The child
// gets HTTPS_PROXY and HTTP_PROXY (both cases) pointing at a counting TCP
// listener and empty NO_PROXY/no_proxy. It fetches
// https://audit.test:<port>/..., a non-loopback host name (Go never proxies
// localhost or a loopback literal, so a loopback URL would prove nothing),
// which the injected resolver maps to the loopback test server and an exact
// allowlist entry permits. The fetch must succeed, every dial must be an
// allowlisted address, and the listener must accept no connection.
func TestFetchIgnoresProxyEnvironment(t *testing.T) {
	if os.Getenv(proxyChildEnv) == "1" {
		runProxyChild(t)
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conn.Close()
		}
	}()
	result := filepath.Join(t.TempDir(), "child.json")
	proxyURL := "http://" + ln.Addr().String()
	env := []string{proxyChildEnv + "=1", proxyResultEnv + "=" + result}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY", "REQUEST_METHOD", proxyChildEnv, proxyResultEnv:
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HTTPS_PROXY="+proxyURL, "https_proxy="+proxyURL,
		"HTTP_PROXY="+proxyURL, "http_proxy="+proxyURL,
		"NO_PROXY=", "no_proxy=",
	)
	cmd := exec.Command(os.Args[0], "-test.run=^TestFetchIgnoresProxyEnvironment$", "-test.count=1", "-test.v")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	ln.Close()
	<-done
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("child wrote no result (did it run?): %v\n%s", err, out)
	}
	var got proxyChildResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !got.EnvironmentRoutesToProxy {
		t.Fatal("the child environment would not proxy the request; the test proves nothing")
	}
	if got.Kind != string(OK) || got.BytesRead == 0 {
		t.Fatalf("child fetch %+v", got)
	}
	if len(got.Dials) == 0 {
		t.Fatal("child recorded no dial")
	}
	for _, d := range got.Dials {
		if !contains(got.Allowlisted, d) {
			t.Fatalf("dialed %s, allowlisted %v", d, got.Allowlisted)
		}
	}
	if n := accepted.Load(); n != 0 {
		t.Fatalf("the proxy listener accepted %d connections", n)
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func runProxyChild(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	base := f.srv.URLFor(fetchtest.TestHost)
	probe, err := http.NewRequest(http.MethodGet, base+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := http.ProxyFromEnvironment(probe)
	if err != nil {
		t.Fatal(err)
	}
	entry := loopbackEntry(testProvider, fetchtest.TestHost, f.srv)
	res := &fakeResolver{answers: [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}}}
	c := newClient(t, f.ca, res, testLimits(), entry)
	rec := &dialRecorder{}
	c.connect = rec.recordReal(c)
	s, opened := c.Open(context.Background(), testProvider, base)
	result := proxyChildResult{
		EnvironmentRoutesToProxy: proxy != nil && proxy.Host == os.Getenv("HTTPS_PROXY")[len("http://"):],
		Kind:                     string(opened.Kind),
		Allowlisted:              []string{netip.AddrPortFrom(entry.IP, entry.Port).String()},
	}
	if s != nil {
		out, _ := getWhole(t, s, len(f.pkg))
		s.Close()
		result.Kind, result.BytesRead = string(out.Kind), out.BytesRead
	}
	for _, d := range rec.seen() {
		result.Dials = append(result.Dials, d.String())
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv(proxyResultEnv), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if result.Kind != string(OK) {
		t.Fatalf("child fetch %+v", result)
	}
}
