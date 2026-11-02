package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch/fetchtest"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
)

func TestFetchWholePackageOverLoopbackEntry(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
	rec := &dialRecorder{}
	c.connect = rec.recordReal(c)
	s := open(t, c, testProvider, f.srv.URL)
	out, body := getWhole(t, s, len(f.pkg))
	if out.Kind != OK || out.Status != 200 || out.BytesRead != uint64(len(f.pkg)) {
		t.Fatalf("outcome %+v", out)
	}
	if !bytes.Equal(body, f.pkg) {
		t.Fatal("body differs from the package")
	}
	if got := rec.seen(); len(got) != 1 || got[0] != f.srv.Addr {
		t.Fatalf("dialed %v, want only %v", got, f.srv.Addr)
	}
}

// The same loopback URL is refused without its exact entry and fetched with
// it. Nothing reaches the server while it is refused.
func TestLoopbackURLNeedsItsExactEntry(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	other := f.srv.Addr.Port() + 1
	if other == 0 {
		other = 1
	}
	refusing := []struct {
		name    string
		entries []policy.Entry
	}{
		{"no entry", nil},
		{"other provider", []policy.Entry{loopbackEntry("p2", "127.0.0.1", f.srv)}},
		{"other host", []policy.Entry{loopbackEntry(testProvider, "localhost", f.srv)}},
		{"other port", []policy.Entry{{ProviderID: testProvider, Host: "127.0.0.1", IP: f.srv.Addr.Addr(), Port: other}}},
	}
	for _, tc := range refusing {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, f.ca, nil, testLimits(), tc.entries...)
			s, out := c.Open(context.Background(), testProvider, f.srv.URL)
			if s != nil || out.Kind != PolicyRefused || out.Refusal == nil || out.Refusal.Rule != policy.RuleAddress {
				t.Fatalf("outcome %+v", out)
			}
			if out.Refusal.Row == nil || out.Refusal.Row.Name != "Loopback" {
				t.Fatalf("row %+v", out.Refusal.Row)
			}
		})
	}
	if n := f.srv.Requests(); n != 0 {
		t.Fatalf("server saw %d requests while refused", n)
	}
	c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
	out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
	if out.Kind != OK {
		t.Fatalf("with its entry: %+v", out)
	}
}

// An entry whose name resolves to another loopback address is refused, in
// both directions: the entry names 127.0.0.1 and the name resolves to
// 127.0.0.2, or the entry names 127.0.0.2 and the name resolves to 127.0.0.1.
func TestEntryNameResolvingElsewhereRefused(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	elsewhere := policy.Entry{ProviderID: testProvider, Host: fetchtest.TestHost, IP: netip.MustParseAddr("127.0.0.2"), Port: f.srv.Addr.Port()}
	for _, tc := range []struct {
		resolved string
		entry    policy.Entry
	}{
		{"127.0.0.2", loopbackEntry(testProvider, fetchtest.TestHost, f.srv)},
		{"127.0.0.1", elsewhere},
	} {
		c := newClient(t, f.ca, resolverFor(tc.resolved), testLimits(), tc.entry)
		_, out := c.Open(context.Background(), testProvider, f.srv.URLFor(fetchtest.TestHost))
		if out.Kind != PolicyRefused || out.Refusal.Rule != policy.RuleAddress {
			t.Fatalf("resolved %s: outcome %+v", tc.resolved, out)
		}
	}
	if n := f.srv.Requests(); n != 0 {
		t.Fatalf("server saw %d requests", n)
	}
}

func TestMixedResolutionRefusedAsAWhole(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	for _, answer := range [][]string{
		{"127.0.0.1", "10.0.0.1"},
		{"10.0.0.1", "127.0.0.1"},
		{"8.8.8.8", "192.168.1.1"},
		{"127.0.0.1", "::ffff:169.254.169.254"},
	} {
		res := resolverFor(answer...)
		c := newClient(t, f.ca, res, testLimits(), loopbackEntry(testProvider, fetchtest.TestHost, f.srv))
		rec := &dialRecorder{}
		c.connect = rec.recordReal(c)
		s, out := c.Open(context.Background(), testProvider, f.srv.URLFor(fetchtest.TestHost))
		if s != nil || out.Kind != PolicyRefused {
			t.Fatalf("%v: outcome %+v", answer, out)
		}
		if len(rec.seen()) != 0 {
			t.Fatalf("%v: dialed %v", answer, rec.seen())
		}
	}
	if n := f.srv.Requests(); n != 0 {
		t.Fatalf("server saw %d requests", n)
	}
}

// A rebinding resolver answers 127.0.0.1 (allowlisted) first and 10.0.0.1
// after. The session resolves once and keeps dialing the pinned address,
// also for a second request on a new connection.
func TestNoSecondLookup(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	res := &fakeResolver{answers: [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("10.0.0.1")},
	}}
	c := newClient(t, f.ca, res, testLimits(), loopbackEntry(testProvider, fetchtest.TestHost, f.srv))
	rec := &dialRecorder{}
	c.connect = rec.recordReal(c)
	s := open(t, c, testProvider, f.srv.URLFor(fetchtest.TestHost))
	for i := 0; i < 2; i++ {
		if out, _ := getWhole(t, s, len(f.pkg)); out.Kind != OK {
			t.Fatalf("request %d: %+v", i, out)
		}
		s.Close()
	}
	if n := res.count(); n != 1 {
		t.Fatalf("resolver called %d times, want 1", n)
	}
	got := rec.seen()
	if len(got) != 2 || got[0] != f.srv.Addr || got[1] != f.srv.Addr {
		t.Fatalf("dialed %v", got)
	}
}

// The test seam: the policy sees a global address, the socket goes to the
// loopback server. It shows that a global destination needs no entry on 443,
// that another port needs an exact entry, and that the dialer is handed the
// vetted (unmapped) address and nothing else.
func TestGlobalDestinationThroughSeam(t *testing.T) {
	f := startServer(t, fetchtest.Correct, fetchtest.TestHost)
	cases := []struct {
		name     string
		resolved string
		url      string
		entries  []policy.Entry
		wantDial string
		refused  policy.Rule
	}{
		{name: "global on 443", resolved: "8.8.8.8", url: "https://audit.test/", wantDial: "8.8.8.8:443"},
		{name: "global ipv6 on 443", resolved: "2606:4700:4700::1111", url: "https://audit.test/", wantDial: "[2606:4700:4700::1111]:443"},
		{name: "mapped global dialed as IPv4", resolved: "::ffff:8.8.8.8", url: "https://audit.test/", wantDial: "8.8.8.8:443"},
		{name: "global on 8443 without entry", resolved: "8.8.8.8", url: "https://audit.test:8443/", refused: policy.RulePort},
		{
			name: "global on 8443 with entry", resolved: "8.8.8.8", url: "https://audit.test:8443/",
			entries:  []policy.Entry{{ProviderID: testProvider, Host: "audit.test", IP: netip.MustParseAddr("8.8.8.8"), Port: 8443}},
			wantDial: "8.8.8.8:8443",
		},
		{name: "NAT64 of a global address", resolved: "64:ff9b::808:808", url: "https://audit.test/", refused: policy.RuleAddress},
		{name: "6to4 of a global address", resolved: "2002:808:808::1", url: "https://audit.test/", refused: policy.RuleAddress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := resolverFor(tc.resolved)
			c := newClient(t, f.ca, res, testLimits(), tc.entries...)
			rec := &dialRecorder{}
			c.connect = rec.redirectTo(f.srv)
			s, out := c.Open(context.Background(), testProvider, tc.url)
			if tc.refused != "" {
				if s != nil || out.Kind != PolicyRefused || out.Refusal.Rule != tc.refused {
					t.Fatalf("outcome %+v", out)
				}
				return
			}
			if out.Kind != OK {
				t.Fatalf("open %+v", out)
			}
			defer s.Close()
			got, _ := getWhole(t, s, len(f.pkg))
			if got.Kind != OK {
				t.Fatalf("get %+v", got)
			}
			if dials := rec.seen(); len(dials) != 1 || dials[0] != netip.MustParseAddrPort(tc.wantDial) {
				t.Fatalf("dialed %v, want %s", dials, tc.wantDial)
			}
		})
	}
}

// Every 3xx is refused and not followed, including the codes Go's client
// would otherwise follow (301, 302, 303, 307, 308).
func TestRedirectNotFollowed(t *testing.T) {
	for _, code := range []int{300, 301, 302, 303, 307, 308} {
		ca := fetchtest.NewCA(t)
		_, pkg := fetchtest.Synthetic("fetch-test", []int{4096})
		srv := fetchtest.Start(t, fetchtest.Config{
			CA: ca, ContentID: testContent, ContentVersion: testVersion, Package: pkg,
			Behavior: fetchtest.Redirect, RedirectStatus: code,
		})
		c := newClient(t, ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", srv))
		rec := &dialRecorder{}
		c.connect = rec.recordReal(c)
		out, body := getWhole(t, open(t, c, testProvider, srv.URL), len(pkg))
		want := sha256.Sum256([]byte(fetchtest.MetadataURL))
		if out.Kind != RedirectRefused || out.Status != code || out.LocationSHA256 == nil || *out.LocationSHA256 != want || len(body) != 0 {
			t.Fatalf("%d: outcome %+v", code, out)
		}
		if strings.Contains(out.Detail, "169.254") {
			t.Fatalf("%d: detail carries the Location value", code)
		}
		if n := srv.Requests(); n != 1 {
			t.Fatalf("%d: server saw %d requests", code, n)
		}
		for _, d := range rec.seen() {
			if d != srv.Addr {
				t.Fatalf("%d: dialed %v", code, d)
			}
		}
	}
}

func TestHTTPStatusOutcomes(t *testing.T) {
	for _, tc := range []struct {
		b    fetchtest.Behavior
		code int
	}{{fetchtest.NotFound, 404}, {fetchtest.ServerError, 500}} {
		f := startServer(t, tc.b)
		c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != HTTPStatus || out.Status != tc.code || out.BytesRead != 0 {
			t.Fatalf("%d: outcome %+v", tc.code, out)
		}
	}
}

// A gzip Content-Encoding is refused, also when it is on a second header
// line after "identity".
func TestCompressedResponseRefused(t *testing.T) {
	for _, b := range []fetchtest.Behavior{fetchtest.Gzip, fetchtest.GzipSecondLine} {
		f := startServer(t, b)
		c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, body := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != EncodingRefused || len(body) != 0 {
			t.Fatalf("behavior %d: outcome %+v, %d body bytes", b, out, len(body))
		}
	}
}

func TestOversizedBody(t *testing.T) {
	t.Run("undeclared length stops at expected+1", func(t *testing.T) {
		f := startServer(t, fetchtest.OversizedChunked)
		c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, body := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != TooLarge || out.BytesRead != uint64(len(f.pkg))+1 || len(body) != len(f.pkg) {
			t.Fatalf("outcome %+v, sink got %d", out, len(body))
		}
	})
	t.Run("declared length", func(t *testing.T) {
		f := startServer(t, fetchtest.OversizedDeclared)
		c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != TooLarge || out.BytesRead != 0 {
			t.Fatalf("outcome %+v", out)
		}
	})
}

// A short body is LengthMismatch only when the provider's own framing says
// so inside TLS: a declared Content-Length below the expected size, or a
// chunked body whose last chunk arrives early. A body cut inside a declared
// length, or a body delimited by the connection close, is ConnectionReset:
// a cut at a TLS record boundary without close_notify reads as a clean
// io.EOF, and the client cannot tell it from close_notify, so an on-path
// party could forge it.
func TestShortBodyVersusReset(t *testing.T) {
	half := func(n int) uint64 { return uint64(n / 2) }
	cases := []struct {
		b         fetchtest.Behavior
		kind      Kind
		bytesRead func(n int) uint64
	}{
		{fetchtest.TruncateClean, LengthMismatch, half},
		{fetchtest.DeclareShort, LengthMismatch, func(int) uint64 { return 0 }},
		{fetchtest.ResetMidBody, ConnectionReset, nil},
		{fetchtest.TruncateDeclared, ConnectionReset, half},
		{fetchtest.TruncateCloseDelimited, ConnectionReset, half},
		{fetchtest.TruncateCloseNotify, ConnectionReset, half},
	}
	for _, tc := range cases {
		f := startServer(t, tc.b)
		c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != tc.kind {
			t.Fatalf("behavior %d: outcome %+v", tc.b, out)
		}
		if tc.bytesRead != nil && out.BytesRead != tc.bytesRead(len(f.pkg)) {
			t.Fatalf("behavior %d: bytes read %d", tc.b, out.BytesRead)
		}
		if tc.kind == ConnectionReset && out.BytesRead >= uint64(len(f.pkg)) {
			t.Fatalf("reset after %d bytes", out.BytesRead)
		}
	}
}

func TestSlowBodyTimesOutInsideTheBudget(t *testing.T) {
	t.Run("stalled body: idle read timeout", func(t *testing.T) {
		f := startServer(t, fetchtest.StallBody)
		limits := testLimits()
		limits.IdleRead = 300 * time.Millisecond
		limits.Object = 5 * time.Second
		c := newClient(t, f.ca, nil, limits, loopbackEntry(testProvider, "127.0.0.1", f.srv))
		start := time.Now()
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != Timeout {
			t.Fatalf("outcome %+v", out)
		}
		if elapsed := time.Since(start); elapsed >= limits.Object {
			t.Fatalf("took %v, budget %v", elapsed, limits.Object)
		}
	})
	t.Run("stalled headers", func(t *testing.T) {
		f := startServer(t, fetchtest.StallHeaders)
		limits := testLimits()
		limits.ResponseHeader = 300 * time.Millisecond
		limits.Object = 5 * time.Second
		c := newClient(t, f.ca, nil, limits, loopbackEntry(testProvider, "127.0.0.1", f.srv))
		start := time.Now()
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != Timeout || time.Since(start) >= limits.Object {
			t.Fatalf("outcome %+v after %v", out, time.Since(start))
		}
	})
	t.Run("trickling body: per-object budget", func(t *testing.T) {
		f := startServer(t, fetchtest.TrickleBody)
		limits := testLimits()
		limits.IdleRead = 2 * time.Second
		limits.Object = 400 * time.Millisecond
		limits.Connect, limits.TLSHandshake, limits.ResponseHeader = limits.Object, limits.Object, limits.Object
		limits.IdleRead = limits.Object
		c := newClient(t, f.ca, nil, limits, loopbackEntry(testProvider, "127.0.0.1", f.srv))
		start := time.Now()
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		elapsed := time.Since(start)
		if out.Kind != Timeout || out.BytesRead == 0 || out.BytesRead >= uint64(len(f.pkg)) {
			t.Fatalf("outcome %+v", out)
		}
		if elapsed > limits.Object+time.Second {
			t.Fatalf("took %v, budget %v", elapsed, limits.Object)
		}
	})
}

func TestTLSFailures(t *testing.T) {
	t.Run("untrusted CA", func(t *testing.T) {
		f := startServer(t, fetchtest.Correct)
		other := fetchtest.NewCA(t)
		c := newClient(t, other, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != TLSFailed {
			t.Fatalf("outcome %+v", out)
		}
	})
	t.Run("host name mismatch", func(t *testing.T) {
		f := startServer(t, fetchtest.Correct, "other.test")
		c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != TLSFailed {
			t.Fatalf("outcome %+v", out)
		}
	})
	t.Run("system roots do not trust the test CA", func(t *testing.T) {
		f := startServer(t, fetchtest.Correct)
		c := newClient(t, nil, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
		if out.Kind != TLSFailed {
			t.Fatalf("outcome %+v", out)
		}
	})
}

func TestRanges(t *testing.T) {
	get := func(t *testing.T, b fetchtest.Behavior) (Outcome, []byte, []byte) {
		f := startServer(t, b)
		c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
		s := open(t, c, testProvider, f.srv.URL)
		var buf bytes.Buffer
		rg := &Range{First: 4096, Last: 8191, Total: uint64(len(f.pkg))}
		out, err := s.Get(context.Background(), Request{
			ContentID: testContent, ContentVersion: testVersion, Range: rg, Expected: 4096, Sink: &buf,
		})
		if err != nil {
			t.Fatal(err)
		}
		return out, buf.Bytes(), f.pkg[4096:8192]
	}
	if out, body, want := get(t, fetchtest.Correct); out.Kind != OK || out.Status != 206 || !bytes.Equal(body, want) {
		t.Fatalf("correct range: %+v", out)
	}
	if out, body, _ := get(t, fetchtest.IgnoreRange); out.Kind != RangeUnsupported || out.Status != 200 || len(body) != 0 {
		t.Fatalf("ignored range: %+v", out)
	}
	if out, body, _ := get(t, fetchtest.WrongContentRange); out.Kind != RangeUnsupported || out.Status != 206 || len(body) != 0 {
		t.Fatalf("wrong content-range: %+v", out)
	}
	if out, body, _ := get(t, fetchtest.WrongContentRangeTotal); out.Kind != RangeUnsupported || out.Status != 206 || len(body) != 0 {
		t.Fatalf("wrong content-range total: %+v", out)
	}
}

func TestOversizedHeadersAreAProtocolError(t *testing.T) {
	f := startServer(t, fetchtest.HugeHeader)
	c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
	out, _ := getWhole(t, open(t, c, testProvider, f.srv.URL), len(f.pkg))
	if out.Kind != ProtocolError {
		t.Fatalf("outcome %+v", out)
	}
}

func TestConnectFailed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddrPort(ln.Addr().String())
	ln.Close()
	entry := policy.Entry{ProviderID: testProvider, Host: "127.0.0.1", IP: addr.Addr(), Port: addr.Port()}
	c := newClient(t, fetchtest.NewCA(t), nil, testLimits(), entry)
	out, _ := getWhole(t, open(t, c, testProvider, "https://"+addr.String()), 10)
	if out.Kind != ConnectFailed {
		t.Fatalf("outcome %+v", out)
	}
}

func TestDNSFailures(t *testing.T) {
	cases := []struct {
		name     string
		res      *fakeResolver
		notFound bool
	}{
		{"not found", &fakeResolver{err: &net.DNSError{Err: "no such host", Name: "audit.test", IsNotFound: true}}, true},
		{"server failure", &fakeResolver{err: &net.DNSError{Err: "server misbehaving", Name: "audit.test"}}, false},
		{"empty answer", &fakeResolver{answers: [][]netip.Addr{nil}}, false},
	}
	for _, tc := range cases {
		c := newClient(t, nil, tc.res, testLimits())
		s, out := c.Open(context.Background(), testProvider, "https://audit.test/")
		if s != nil || out.Kind != DNSFailure || out.NotFound != tc.notFound {
			t.Fatalf("%s: outcome %+v", tc.name, out)
		}
	}
}

func TestURLRulesRefuseBeforeResolving(t *testing.T) {
	res := resolverFor("8.8.8.8")
	c := newClient(t, nil, res, testLimits())
	for _, raw := range []string{
		"http://audit.test/",
		"https://user@audit.test/",
		"https://audit.test/#f",
		"https://audit.test/x",
		"https://exämple.test/",
		"https://audit.test/" + strings.Repeat("a", 2049),
	} {
		if _, out := c.Open(context.Background(), testProvider, raw); out.Kind != PolicyRefused {
			t.Fatalf("%.40q: outcome %+v", raw, out)
		}
	}
	if n := res.count(); n != 0 {
		t.Fatalf("resolver called %d times", n)
	}
}

func TestRequestValidation(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
	s := open(t, c, testProvider, f.srv.URL)
	ok := Request{ContentID: testContent, ContentVersion: 1, Expected: 10, Sink: io.Discard}
	bad := []func(r *Request){
		func(r *Request) { r.ContentID = "../etc" },
		func(r *Request) { r.ContentID = "" },
		func(r *Request) { r.ContentVersion = 0 },
		func(r *Request) { r.ContentVersion = MaxContentVersion + 1 },
		func(r *Request) { r.Expected = 0 },
		func(r *Request) { r.Expected = MaxObjectBytes + 1 },
		func(r *Request) { r.Sink = nil },
		func(r *Request) { r.Range = &Range{First: 5, Last: 4, Total: 100} },
		func(r *Request) { r.Range = &Range{First: 0, Last: 9, Total: 9} },
		func(r *Request) { r.Range = &Range{First: 0, Last: 8, Total: 100} },
	}
	for i, mutate := range bad {
		r := ok
		mutate(&r)
		if _, err := s.Get(context.Background(), r); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	if n := f.srv.Requests(); n != 0 {
		t.Fatalf("server saw %d requests", n)
	}
}

type failingSink struct{}

func (failingSink) Write([]byte) (int, error) { return 0, errors.New("sink full") }

func TestSinkErrorIsReturned(t *testing.T) {
	f := startServer(t, fetchtest.Correct)
	c := newClient(t, f.ca, nil, testLimits(), loopbackEntry(testProvider, "127.0.0.1", f.srv))
	s := open(t, c, testProvider, f.srv.URL)
	_, err := s.Get(context.Background(), Request{ContentID: testContent, ContentVersion: 1, Expected: uint64(len(f.pkg)), Sink: failingSink{}})
	if err == nil {
		t.Fatal("sink error lost")
	}
}

func TestPackagePath(t *testing.T) {
	got, err := PackagePath("c-1.x_y", 42)
	if err != nil || got != "/packages/c-1.x_y/42" {
		t.Fatalf("%q %v", got, err)
	}
	for _, id := range []string{"", "C1", "a/b", "a b", "-a", strings.Repeat("a", 65)} {
		if _, err := PackagePath(id, 1); err == nil {
			t.Fatalf("%q accepted", id)
		}
	}
}

// The transport has no proxy, no transparent decompression, a TLS 1.2
// minimum, a header size bound, and refuses redirects.
func TestTransportSettings(t *testing.T) {
	c := newClient(t, nil, nil, testLimits(), policy.Entry{ProviderID: testProvider, Host: "127.0.0.1", IP: netip.MustParseAddr("127.0.0.1"), Port: 8443})
	s := open(t, c, testProvider, "https://127.0.0.1:8443/")
	tr := s.transport
	if tr.Proxy != nil || !tr.DisableCompression || tr.ForceAttemptHTTP2 || tr.MaxResponseHeaderBytes != MaxResponseHeaderBytes || tr.MaxConnsPerHost != 1 {
		t.Fatalf("transport %+v", tr)
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("tls config")
	}
	if err := s.http.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy %v", err)
	}
	if _, err := s.dial(context.Background(), "tcp", "127.0.0.1:1"); !errors.Is(err, errUnexpectedDial) {
		t.Fatalf("dial to another address: %v", err)
	}
	if _, err := s.dial(context.Background(), "tcp", "proxy.test:3128"); !errors.Is(err, errUnexpectedDial) {
		t.Fatalf("dial to a proxy: %v", err)
	}
}

func TestControlHookRefusesOtherAddresses(t *testing.T) {
	control := controlFor(netip.MustParseAddrPort("127.0.0.1:8443"))
	if err := control("tcp4", "127.0.0.1:8443", nil); err != nil {
		t.Fatalf("vetted address refused: %v", err)
	}
	for _, other := range []string{"127.0.0.2:8443", "127.0.0.1:8444", "[::1]:8443", "10.0.0.1:8443", "garbage"} {
		if err := control("tcp4", other, nil); !errors.Is(err, errSocketMismatch) {
			t.Fatalf("%s: %v", other, err)
		}
	}
}

func TestNewValidatesOptions(t *testing.T) {
	pol, err := policy.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Limits: testLimits()}); err == nil {
		t.Fatal("nil policy accepted")
	}
	for i, mutate := range []func(l *Limits){
		func(l *Limits) { l.Connect = 0 },
		func(l *Limits) { l.TLSHandshake = -1 },
		func(l *Limits) { l.ResponseHeader = 0 },
		func(l *Limits) { l.IdleRead = 0 },
		func(l *Limits) { l.Object = l.IdleRead - 1 },
	} {
		l := testLimits()
		mutate(&l)
		if _, err := New(Options{Policy: pol, Limits: l}); err == nil {
			t.Fatalf("limits case %d accepted", i)
		}
	}
}

// The connect seam cannot be reached from outside this package: Options has
// exactly these four fields (no dial hook), New leaves the seam nil, and no
// configuration value is a function.
func TestSeamNotReachableThroughOptions(t *testing.T) {
	typ := reflect.TypeOf(Options{})
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Name)
		if typ.Field(i).Type.Kind() == reflect.Func {
			t.Fatalf("Options.%s is a function", typ.Field(i).Name)
		}
	}
	if strings.Join(names, ",") != "Policy,RootCAs,Resolver,Limits" {
		t.Fatalf("Options fields %v", names)
	}
	pol, _ := policy.New(nil)
	c, err := New(Options{Policy: pol, Limits: testLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if c.connect != nil {
		t.Fatal("New set the test seam")
	}
	if _, ok := reflect.TypeOf(Client{}).FieldByName("connect"); !ok {
		t.Fatal("seam field renamed; update the scan in the module root test")
	}
}

func TestLoadCAFile(t *testing.T) {
	dir := t.TempDir()
	ca := fetchtest.NewCA(t)
	good := ca.WriteFile(t, dir)
	if pool, err := LoadCAFile(good); err != nil || pool == nil {
		t.Fatalf("valid ca file: %v", err)
	}
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	keyBlock := []byte("-----BEGIN " + "PRIVATE KEY-----\nAAAA\n-----END " + "PRIVATE KEY-----\n")
	bad := map[string]string{
		"empty":         write("empty.crt", nil),
		"garbage":       write("garbage.crt", []byte("not pem")),
		"trailing data": write("trailing.crt", append(append([]byte(nil), ca.PEM...), []byte("junk")...)),
		"key block":     write("key.crt", keyBlock),
		"bad cert":      write("badcert.crt", []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")),
		"too large":     write("large.crt", bytes.Repeat([]byte("a"), MaxCAFileBytes+1)),
		"directory":     dir,
		"missing":       filepath.Join(dir, "missing.crt"),
	}
	for name, path := range bad {
		if _, err := LoadCAFile(path); err == nil {
			t.Fatalf("%s accepted", name)
		} else if strings.Contains(err.Error(), "AAAA") || strings.Contains(err.Error(), "junk") {
			t.Fatalf("%s: error echoes file contents: %v", name, err)
		}
	}
}
