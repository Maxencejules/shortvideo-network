package fetch

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch/fetchtest"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
)

const (
	testProvider = "p1"
	testContent  = "c1"
	testVersion  = 1
)

func testLimits() Limits {
	return Limits{
		Connect:        2 * time.Second,
		TLSHandshake:   2 * time.Second,
		ResponseHeader: 2 * time.Second,
		IdleRead:       2 * time.Second,
		Object:         10 * time.Second,
	}
}

// fakeResolver answers each lookup with the next answer in turn; the last
// answer repeats. It counts calls.
type fakeResolver struct {
	mu      sync.Mutex
	answers [][]netip.Addr
	err     error
	calls   int
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	i := min(r.calls-1, len(r.answers)-1)
	return r.answers[i], nil
}

func (r *fakeResolver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func resolverFor(addrs ...string) *fakeResolver {
	var answer []netip.Addr
	for _, a := range addrs {
		answer = append(answer, netip.MustParseAddr(a))
	}
	return &fakeResolver{answers: [][]netip.Addr{answer}}
}

// dialRecorder wraps the connect step and records every vetted target.
type dialRecorder struct {
	mu      sync.Mutex
	targets []netip.AddrPort
}

func (d *dialRecorder) record(target netip.AddrPort) {
	d.mu.Lock()
	d.targets = append(d.targets, target)
	d.mu.Unlock()
}

func (d *dialRecorder) seen() []netip.AddrPort {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]netip.AddrPort(nil), d.targets...)
}

// recordReal records and then dials the vetted target for real.
func (d *dialRecorder) recordReal(c *Client) func(context.Context, netip.AddrPort) (net.Conn, error) {
	return func(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
		d.record(target)
		return c.dialVetted(ctx, target)
	}
}

// redirectTo records the vetted target and connects to srv instead: the
// test seam that lets a global address reach a loopback server.
func (d *dialRecorder) redirectTo(srv *fetchtest.Server) func(context.Context, netip.AddrPort) (net.Conn, error) {
	return func(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
		d.record(target)
		var nd net.Dialer
		return nd.DialContext(ctx, "tcp", srv.Addr.String())
	}
}

func loopbackEntry(provider, host string, srv *fetchtest.Server) policy.Entry {
	return policy.Entry{ProviderID: provider, Host: host, IP: srv.Addr.Addr(), Port: srv.Addr.Port()}
}

func newClient(t *testing.T, ca *fetchtest.CA, res Resolver, limits Limits, entries ...policy.Entry) *Client {
	t.Helper()
	pol, err := policy.New(entries)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	opts := Options{Policy: pol, Limits: limits, Resolver: res}
	if ca != nil {
		opts.RootCAs = ca.Pool
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

type fixture struct {
	ca  *fetchtest.CA
	pkg []byte
	srv *fetchtest.Server
}

func startServer(t *testing.T, b fetchtest.Behavior, names ...string) fixture {
	t.Helper()
	ca := fetchtest.NewCA(t)
	_, pkg := fetchtest.Synthetic("fetch-test", []int{4096, 4096, 1000})
	srv := fetchtest.Start(t, fetchtest.Config{
		CA: ca, ContentID: testContent, ContentVersion: testVersion, Package: pkg, Behavior: b, Names: names,
	})
	t.Logf("%s at %s", fetchtest.Label, srv.URL)
	return fixture{ca: ca, pkg: pkg, srv: srv}
}

func open(t *testing.T, c *Client, provider, base string) *Session {
	t.Helper()
	s, out := c.Open(context.Background(), provider, base)
	if out.Kind != OK {
		t.Fatalf("open %s: %+v", base, out)
	}
	t.Cleanup(s.Close)
	return s
}

func getWhole(t *testing.T, s *Session, size int) (Outcome, []byte) {
	t.Helper()
	var buf bytes.Buffer
	out, err := s.Get(context.Background(), Request{
		ContentID: testContent, ContentVersion: testVersion, Expected: uint64(size), Sink: &buf,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return out, buf.Bytes()
}
