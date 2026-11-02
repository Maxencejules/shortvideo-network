// Package fetch makes the producer's requests to provider endpoints under
// the destination policy (package policy, PROPOSED under decision request
// DR-09-10):
//
//   - the request path is /packages/{content_id}/{content_version}, built
//     from chain values and percent-encoded; it is never taken from a
//     response;
//   - a host name is resolved once, through Options.Resolver, when the
//     session opens; every returned address must pass policy.Vet; the
//     session dials only those addresses, by literal, and the dialer's
//     Control hook refuses a socket address that is not the vetted one.
//     Later requests in the session reuse the pinned addresses, so a DNS
//     answer that changes between check and connect cannot steer a
//     connection;
//   - SNI and certificate verification use the original host name; TLS 1.2
//     is the minimum; the roots are the system roots unless Options.RootCAs
//     names a pool (a test CA, loaded with LoadCAFile from an explicit
//     setting);
//   - no proxy: Transport.Proxy is nil, so HTTPS_PROXY, HTTP_PROXY, and
//     NO_PROXY are ignored;
//   - no redirect is followed; a 3xx is RedirectRefused;
//   - Accept-Encoding is identity and transparent decompression is off; a
//     Content-Encoding other than identity is EncodingRefused;
//   - connect, TLS-handshake, response-header, and idle-read timeouts, a
//     per-object budget, a response-header size limit, one connection per
//     session, and a body read of at most expected+1 bytes.
//
// Every result is a typed Outcome. This package does not decide whether an
// outcome is a provider failure.
package fetch

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// Bounds.
const (
	// MaxResponseHeaderBytes bounds the response status line and headers.
	MaxResponseHeaderBytes = 16 << 10
	// MaxObjectBytes is bounds.json safety_max_package_bytes: no request
	// expects more.
	MaxObjectBytes = genesis.MaxPackageBytes
	// MaxContentVersion is bounds.json safe_integer_max.
	MaxContentVersion = uint64(genesis.SafeIntegerMax)
	// MaxCAFileBytes bounds the CA file.
	MaxCAFileBytes = 1 << 20

	acceptHeader = "application/vnd.svn.v0+json"
	userAgent    = "svn-auditor/0"
)

// Resolver looks up a host name. *net.Resolver implements it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Limits are the time bounds of one session. Every value must be positive
// and Object must be at least each of the others.
type Limits struct {
	// Connect bounds DNS resolution and each TCP connect.
	Connect time.Duration
	// TLSHandshake bounds the TLS handshake.
	TLSHandshake time.Duration
	// ResponseHeader bounds the wait for the response headers after the
	// request is written.
	ResponseHeader time.Duration
	// IdleRead bounds each read and write on the connection.
	IdleRead time.Duration
	// Object bounds one Get from start to the end of the body.
	Object time.Duration
}

func (l Limits) validate() error {
	phases := []struct {
		name string
		d    time.Duration
	}{
		{"connect", l.Connect}, {"tls handshake", l.TLSHandshake},
		{"response header", l.ResponseHeader}, {"idle read", l.IdleRead},
	}
	for _, p := range phases {
		if p.d <= 0 {
			return fmt.Errorf("fetch: %s limit must be positive", p.name)
		}
		if l.Object < p.d {
			return fmt.Errorf("fetch: object budget is below the %s limit", p.name)
		}
	}
	return nil
}

// Options configure a Client.
type Options struct {
	// Policy decides which destinations are permitted. Required.
	Policy *policy.Policy
	// RootCAs replaces the system roots when non-nil.
	RootCAs *x509.CertPool
	// Resolver resolves host names; nil uses net.DefaultResolver. Whatever
	// it returns still has to pass Policy.
	Resolver Resolver
	// Limits are the time bounds.
	Limits Limits
}

// Client opens sessions to provider endpoints.
type Client struct {
	opts Options
	// connect is a test seam. Outside this package's tests it is always
	// nil and every connection goes through dialVetted. No exported field
	// or function sets it, and no configuration value reaches it.
	connect func(ctx context.Context, target netip.AddrPort) (net.Conn, error)
}

// New validates opts and returns a Client.
func New(opts Options) (*Client, error) {
	if opts.Policy == nil {
		return nil, errors.New("fetch: a policy is required")
	}
	if err := opts.Limits.validate(); err != nil {
		return nil, err
	}
	if opts.Resolver == nil {
		opts.Resolver = net.DefaultResolver
	}
	return &Client{opts: opts}, nil
}

// Session is an opened endpoint: the base URL passed the URL rules and its
// addresses were resolved once and vetted. It holds at most one connection.
type Session struct {
	client    *Client
	endpoint  policy.Endpoint
	targets   []netip.AddrPort
	transport *http.Transport
	http      *http.Client
}

// Open applies the URL rules to baseURL, resolves its host once (an IP
// literal is not resolved), and vets every address for providerID's audit.
// On success the Outcome is OK and the session is non-nil. Nothing is
// dialed by Open.
func (c *Client) Open(ctx context.Context, providerID, baseURL string) (*Session, Outcome) {
	ep, err := policy.ParseEndpoint(baseURL)
	if err != nil {
		return nil, refused(err)
	}
	addrs := []netip.Addr{ep.Literal}
	if !ep.Literal.IsValid() {
		lookupCtx, cancel := context.WithTimeout(ctx, c.opts.Limits.Connect)
		addrs, err = c.opts.Resolver.LookupNetIP(lookupCtx, "ip", ep.Host)
		cancel()
		if err != nil {
			var dnsErr *net.DNSError
			notFound := errors.As(err, &dnsErr) && dnsErr.IsNotFound
			return nil, Outcome{Kind: DNSFailure, NotFound: notFound, Detail: "resolver error"}
		}
		if len(addrs) == 0 {
			return nil, Outcome{Kind: DNSFailure, Detail: "resolver returned no address"}
		}
	}
	targets, err := c.opts.Policy.Vet(providerID, ep, addrs)
	if err != nil {
		return nil, refused(err)
	}
	s := &Session{client: c, endpoint: ep, targets: targets}
	s.transport = &http.Transport{
		Proxy:       nil,
		DialContext: s.dial,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    c.opts.RootCAs,
		},
		TLSHandshakeTimeout:    c.opts.Limits.TLSHandshake,
		ResponseHeaderTimeout:  c.opts.Limits.ResponseHeader,
		MaxResponseHeaderBytes: MaxResponseHeaderBytes,
		DisableCompression:     true,
		MaxConnsPerHost:        1,
		MaxIdleConnsPerHost:    1,
		IdleConnTimeout:        c.opts.Limits.IdleRead,
	}
	s.http = &http.Client{
		Transport: s.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return s, Outcome{Kind: OK}
}

func refused(err error) Outcome {
	var r *policy.Refusal
	if errors.As(err, &r) {
		return Outcome{Kind: PolicyRefused, Refusal: r, Detail: r.Error()}
	}
	return Outcome{Kind: PolicyRefused, Detail: "policy refused"}
}

// Close closes the session's idle connection.
func (s *Session) Close() { s.transport.CloseIdleConnections() }

// dial is the transport's DialContext. It connects only to the vetted
// addresses of the session's own endpoint, in resolver order.
func (s *Session) dial(ctx context.Context, _, addr string) (net.Conn, error) {
	if addr != s.endpoint.DialAuthority() {
		return nil, &dialError{err: errUnexpectedDial}
	}
	var last error
	for _, target := range s.targets {
		var conn net.Conn
		var err error
		if s.client.connect != nil {
			conn, err = s.client.connect(ctx, target)
		} else {
			conn, err = s.client.dialVetted(ctx, target)
		}
		if err == nil {
			return &idleConn{Conn: conn, idle: s.client.opts.Limits.IdleRead}, nil
		}
		last = err
	}
	return nil, &dialError{err: last}
}

// Range is an inclusive byte range of an object of Total bytes.
type Range struct {
	First, Last, Total uint64
}

// Request is one GET of /packages/{content_id}/{content_version}.
type Request struct {
	ContentID      string
	ContentVersion uint64
	// Range is nil for the whole object (status 200 expected).
	Range *Range
	// Expected is the number of body bytes expected: the object size, or
	// Last-First+1 for a range.
	Expected uint64
	// Sink receives the body, at most Expected bytes. A Sink error ends
	// the request and is returned as Get's error.
	Sink io.Writer
}

var idPattern = regexp.MustCompile(genesis.IDPattern)

// PackagePath returns /packages/{content_id}/{content_version} with each
// segment percent-encoded. contentID must match the protocol id pattern and
// version must be 1 to 2^53-1.
func PackagePath(contentID string, version uint64) (string, error) {
	if !idPattern.MatchString(contentID) {
		return "", fmt.Errorf("fetch: content id must match %s", genesis.IDPattern)
	}
	if version == 0 || version > MaxContentVersion {
		return "", errors.New("fetch: content version out of range")
	}
	return "/packages/" + url.PathEscape(contentID) + "/" + url.PathEscape(strconv.FormatUint(version, 10)), nil
}

func (r Request) validate() (string, error) {
	path, err := PackagePath(r.ContentID, r.ContentVersion)
	if err != nil {
		return "", err
	}
	if r.Sink == nil {
		return "", errors.New("fetch: a sink is required")
	}
	if r.Expected == 0 || r.Expected > MaxObjectBytes {
		return "", errors.New("fetch: expected size out of range")
	}
	if rg := r.Range; rg != nil {
		if rg.Last < rg.First || rg.Last >= rg.Total || rg.Total > MaxObjectBytes || rg.Last-rg.First+1 != r.Expected {
			return "", errors.New("fetch: range does not match the expected size")
		}
	}
	return path, nil
}

// phase records what the transport reached. Trace callbacks may run on
// transport goroutines, so access is locked.
type phase struct {
	mu        sync.Mutex
	tlsErr    error
	firstByte bool
}

func (p *phase) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			p.mu.Lock()
			p.tlsErr = err
			p.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			p.mu.Lock()
			p.firstByte = true
			p.mu.Unlock()
		},
	}
}

func (p *phase) state() (firstByte bool, tlsErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.firstByte, p.tlsErr
}

// sinkWriter remembers the Sink's error so Get can tell it from a body
// read error.
type sinkWriter struct {
	w   io.Writer
	err error
}

func (s *sinkWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil {
		s.err = err
	}
	return n, err
}

// Get performs one request within the per-object budget. The error is
// non-nil only for an invalid Request or a Sink error; every network and
// provider result is in the Outcome.
func (s *Session) Get(ctx context.Context, req Request) (Outcome, error) {
	path, err := req.validate()
	if err != nil {
		return Outcome{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.client.opts.Limits.Object)
	defer cancel()
	var ph phase
	ctx = httptrace.WithClientTrace(ctx, ph.trace())
	target := &url.URL{Scheme: "https", Host: s.endpoint.Authority(), Path: path}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Outcome{}, fmt.Errorf("fetch: build request: %w", err)
	}
	httpReq.Header.Set("Accept", acceptHeader)
	httpReq.Header.Set("Accept-Encoding", "identity")
	httpReq.Header.Set("User-Agent", userAgent)
	if rg := req.Range; rg != nil {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rg.First, rg.Last))
	}
	resp, err := s.http.Do(httpReq)
	if err != nil {
		return requestError(err, &ph), nil
	}
	defer resp.Body.Close()
	if out, done := checkResponse(resp, req); done {
		return out, nil
	}
	return readBody(resp, req)
}

// requestError maps an error from http.Client.Do by the phase it reached.
func requestError(err error, ph *phase) Outcome {
	firstByte, tlsErr := ph.state()
	var de *dialError
	switch {
	case errors.As(err, &de):
		if isTimeout(de.err) {
			return Outcome{Kind: Timeout, Detail: "connect timeout"}
		}
		return Outcome{Kind: ConnectFailed, Detail: "connect failed"}
	case tlsErr != nil:
		if isTimeout(tlsErr) {
			return Outcome{Kind: Timeout, Detail: "tls handshake timeout"}
		}
		return Outcome{Kind: TLSFailed, Detail: "tls handshake failed"}
	case isTimeout(err):
		return Outcome{Kind: Timeout, Detail: "no response before the deadline"}
	case firstByte:
		return Outcome{Kind: ProtocolError, Detail: "response is not valid HTTP/1.1 or its headers are too large"}
	default:
		return Outcome{Kind: ConnectionReset, Detail: "connection ended before a response"}
	}
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// checkResponse applies the status, encoding, range, and declared-length
// rules. done is true when the outcome is decided without reading the body.
func checkResponse(resp *http.Response, req Request) (Outcome, bool) {
	out := Outcome{Status: resp.StatusCode}
	switch code := resp.StatusCode; {
	case code >= 300 && code <= 399:
		out.Kind, out.Detail = RedirectRefused, "redirect not followed"
		if loc := resp.Header.Get("Location"); loc != "" {
			sum := sha256.Sum256([]byte(loc))
			out.LocationSHA256 = &sum
		}
		return out, true
	case req.Range == nil && code != http.StatusOK:
		out.Kind, out.Detail = HTTPStatus, "status is not 200"
		return out, true
	case req.Range != nil && code == http.StatusOK:
		out.Kind, out.Detail = RangeUnsupported, "200 in answer to a range request"
		return out, true
	case req.Range != nil && code != http.StatusPartialContent:
		out.Kind, out.Detail = HTTPStatus, "status is not 206"
		return out, true
	}
	if !identityOnly(resp.Header.Values("Content-Encoding")) {
		out.Kind, out.Detail = EncodingRefused, "content encoding is not identity"
		return out, true
	}
	if rg := req.Range; rg != nil {
		want := fmt.Sprintf("bytes %d-%d/%d", rg.First, rg.Last, rg.Total)
		if got := resp.Header.Values("Content-Range"); len(got) != 1 || got[0] != want {
			out.Kind, out.Detail = RangeUnsupported, "content-range is not the requested range"
			return out, true
		}
	}
	switch declared := resp.ContentLength; {
	case declared > int64(req.Expected):
		out.Kind, out.Detail = TooLarge, "declared content length above the expected size"
		return out, true
	case declared >= 0 && declared < int64(req.Expected):
		out.Kind, out.Detail = LengthMismatch, "declared content length below the expected size"
		return out, true
	}
	return out, false
}

func identityOnly(values []string) bool {
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			token = strings.ToLower(strings.TrimSpace(token))
			if token != "" && token != "identity" {
				return false
			}
		}
	}
	return true
}

// readBody copies exactly Expected bytes to the Sink, then reads one more
// byte to tell a clean end of body from an oversized one.
func readBody(resp *http.Response, req Request) (Outcome, error) {
	out := Outcome{Status: resp.StatusCode}
	sink := &sinkWriter{w: req.Sink}
	n, err := io.CopyN(sink, resp.Body, int64(req.Expected))
	out.BytesRead = uint64(n)
	if sink.err != nil {
		return Outcome{}, fmt.Errorf("fetch: sink: %w", sink.err)
	}
	if err != nil {
		return bodyError(resp, out, err), nil
	}
	var extra [1]byte
	for {
		m, err := resp.Body.Read(extra[:])
		if m > 0 {
			out.BytesRead++
			out.Kind, out.Detail = TooLarge, "body continues past the expected size"
			return out, nil
		}
		if errors.Is(err, io.EOF) {
			out.Kind = OK
			return out, nil
		}
		if err != nil {
			return bodyError(resp, out, err), nil
		}
	}
}

// bodyError maps a body read error. Only the last chunk of a chunked body is
// an end of body that TLS authenticates, so only there is io.EOF before the
// expected size LengthMismatch. A body delimited by the connection close
// also reads io.EOF when the TCP connection is cut at a TLS record boundary
// without close_notify, which an on-path party can cause, and the client
// cannot tell that from close_notify: it is ConnectionReset. A cut inside a
// declared Content-Length or inside a chunk reads io.ErrUnexpectedEOF and is
// ConnectionReset too.
func bodyError(resp *http.Response, out Outcome, err error) Outcome {
	switch {
	case errors.Is(err, io.EOF) && chunked(resp):
		out.Kind, out.Detail = LengthMismatch, "chunked body ended before the expected size"
	case errors.Is(err, io.EOF):
		out.Kind, out.Detail = ConnectionReset, "body ended without authenticated framing"
	case isTimeout(err):
		out.Kind, out.Detail = Timeout, "body read timed out"
	default:
		out.Kind, out.Detail = ConnectionReset, "body ended without a clean close"
	}
	return out
}

func chunked(resp *http.Response) bool {
	return len(resp.TransferEncoding) == 1 && resp.TransferEncoding[0] == "chunked"
}

// LoadCAFile reads a PEM file of CA certificates for Options.RootCAs. The
// pool holds only these certificates: the system roots are not added. The
// file must be a regular file of at most 1 MiB holding only CERTIFICATE
// blocks. Error text never includes file contents.
func LoadCAFile(path string) (*x509.CertPool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("fetch: ca file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > MaxCAFileBytes {
		return nil, errors.New("fetch: ca file must be a regular file of at most 1 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fetch: ca file: %w", err)
	}
	if len(data) > MaxCAFileBytes {
		return nil, errors.New("fetch: ca file must be a regular file of at most 1 MiB")
	}
	pool := x509.NewCertPool()
	count := 0
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			if strings.TrimSpace(string(rest)) != "" {
				return nil, errors.New("fetch: ca file has data outside PEM blocks")
			}
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, errors.New("fetch: ca file holds a PEM block that is not a certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, errors.New("fetch: ca file holds an invalid certificate")
		}
		pool.AddCert(cert)
		count++
	}
	if count == 0 {
		return nil, errors.New("fetch: ca file holds no certificate")
	}
	return pool, nil
}
