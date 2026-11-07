// Package fetchtest provides loopback HTTPS test endpoints for the audit
// producer's tests: a runtime CA, leaf certificates, and an httptest TLS
// server on 127.0.0.1 that serves GET /packages/{content_id}/{content_version}
// from synthetic bytes generated at test time, with optional single-range
// support and misbehaving variants.
//
// Label: "loopback test endpoint, synthetic package, not a provider agent".
// Nothing here is a provider agent or a mock of the provider protocol: it is
// a real TLS server on loopback. Only tests import this package
// (TestFetchtestOnlyInTests enforces it); it never writes media or keys to
// the repository.
package fetchtest

import (
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Label names what these endpoints are, for test logs and evidence.
const Label = "loopback test endpoint, synthetic package, not a provider agent"

// TestHost is the host name the default leaf certificate covers besides
// 127.0.0.1 and localhost. Tests map it to 127.0.0.1 with an injected
// resolver.
const TestHost = "audit.test"

// CA is a runtime certificate authority. Its key lives only in memory.
type CA struct {
	Pool *x509.CertPool
	PEM  []byte
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewCA creates a fresh CA.
func NewCA(t testing.TB) *CA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: "svn auditor test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("ca certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ca parse: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &CA{
		Pool: pool,
		PEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		cert: cert,
		key:  key,
	}
}

func serial(t testing.TB) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	return n
}

// WriteFile writes the CA certificate (no key) to dir and returns the path.
func (ca *CA) WriteFile(t testing.TB, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "test-ca.crt")
	if err := os.WriteFile(path, ca.PEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return path
}

// Leaf issues a server certificate for names (DNS names or IP literals).
func (ca *CA) Leaf(t testing.TB, names ...string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: "svn auditor test endpoint"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, name)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("leaf certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// Behavior selects how the server answers.
type Behavior int

// Server behaviors.
const (
	// Correct serves the package, and a single "bytes=a-b" range as 206.
	Correct Behavior = iota
	// IgnoreRange answers a range request with 200 and the whole package.
	IgnoreRange
	// WrongContentRange answers a range request with 206, the requested
	// bytes, and a Content-Range for another range.
	WrongContentRange
	// WrongContentRangeTotal answers a range request with 206, the
	// requested bytes, and a Content-Range for the requested range of a
	// package one byte longer.
	WrongContentRangeTotal
	// TruncateClean sends half the body without a Content-Length and ends
	// the chunked body cleanly.
	TruncateClean
	// TruncateCloseDelimited sends a response with neither a Content-Length
	// nor chunked framing, half the body, and then closes the TCP socket
	// without a TLS close_notify.
	TruncateCloseDelimited
	// TruncateCloseNotify sends a response with neither a Content-Length nor
	// chunked framing, half the body, a TLS close_notify, and then closes the
	// TCP socket.
	TruncateCloseNotify
	// TruncateDeclared declares the full length, sends half, and closes the
	// TCP socket without a TLS close_notify and without a reset.
	TruncateDeclared
	// DeclareShort declares and sends a Content-Length of half the body.
	DeclareShort
	// ResetMidBody declares the full length, sends half, and resets the
	// TCP connection.
	ResetMidBody
	// OversizedChunked sends the body plus 1024 extra bytes without a
	// Content-Length.
	OversizedChunked
	// OversizedDeclared declares and sends the body plus 1024 bytes.
	OversizedDeclared
	// Gzip sends the gzip-compressed body with Content-Encoding: gzip.
	Gzip
	// GzipSecondLine sends the gzip-compressed body with two
	// Content-Encoding header lines: "identity", then "gzip".
	GzipSecondLine
	// Redirect answers Config.RedirectStatus (302 when 0) with Location: the
	// cloud metadata address.
	Redirect
	// NotFound answers 404.
	NotFound
	// ServerError answers 500.
	ServerError
	// StallBody declares the full length, sends half, then sends nothing
	// until the client gives up or the test ends.
	StallBody
	// StallHeaders sends nothing until the client gives up or the test ends.
	StallHeaders
	// TrickleBody declares the full length and sends one byte every 20 ms,
	// so no idle timeout fires and only the per-object budget ends it.
	TrickleBody
	// HugeHeader answers 200 with a 32 KiB response header.
	HugeHeader
)

// MetadataURL is the Location of the Redirect behavior.
const MetadataURL = "https://169.254.169.254/latest/meta-data/"

// Config describes one test server.
type Config struct {
	CA             *CA
	ContentID      string
	ContentVersion uint64
	// Package is the synthetic package. Corrupt it before Start for a
	// corrupted-segment variant.
	Package  []byte
	Behavior Behavior
	// Names are the leaf certificate names. Default: 127.0.0.1, localhost,
	// TestHost.
	Names []string
	// RedirectStatus is the status of the Redirect behavior; 0 means 302.
	RedirectStatus int
}

// Server is a running loopback HTTPS endpoint.
type Server struct {
	// URL is the base URL by IP literal: https://127.0.0.1:port.
	URL string
	// Addr is the listening address.
	Addr     netip.AddrPort
	requests atomic.Int64
	ranges   atomic.Int64
}

// Requests counts requests that reached the handler.
func (s *Server) Requests() int64 { return s.requests.Load() }

// RangeRequests counts requests that carried a Range header.
func (s *Server) RangeRequests() int64 { return s.ranges.Load() }

// URLFor returns the base URL with host in place of 127.0.0.1.
func (s *Server) URLFor(host string) string {
	return "https://" + net.JoinHostPort(host, strconv.Itoa(int(s.Addr.Port())))
}

// Start runs a server until the test ends.
func Start(t testing.TB, cfg Config) *Server {
	t.Helper()
	names := cfg.Names
	if len(names) == 0 {
		names = []string{"127.0.0.1", "localhost", TestHost}
	}
	srv := &Server{}
	stop := make(chan struct{})
	path := fmt.Sprintf("/packages/%s/%d", cfg.ContentID, cfg.ContentVersion)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.requests.Add(1)
		if r.Header.Get("Range") != "" {
			srv.ranges.Add(1)
		}
		if r.Method != http.MethodGet || r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		serve(w, r, cfg, stop)
	})
	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{cfg.CA.Leaf(t, names...)}, MinVersion: tls.VersionTLS12}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	t.Cleanup(func() { close(stop) })
	addr, err := netip.ParseAddrPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("listener address: %v", err)
	}
	srv.Addr = addr
	srv.URL = "https://" + addr.String()
	return srv
}

func serve(w http.ResponseWriter, r *http.Request, cfg Config, stop <-chan struct{}) {
	pkg, b := cfg.Package, cfg.Behavior
	half := pkg[:len(pkg)/2]
	switch b {
	case Redirect:
		status := cfg.RedirectStatus
		if status == 0 {
			status = http.StatusFound
		}
		w.Header().Set("Location", MetadataURL)
		w.WriteHeader(status)
	case NotFound:
		http.NotFound(w, r)
	case ServerError:
		w.WriteHeader(http.StatusInternalServerError)
	case Gzip, GzipSecondLine:
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write(pkg)
		_ = zw.Close()
		if b == GzipSecondLine {
			w.Header().Add("Content-Encoding", "identity")
		}
		w.Header().Add("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		_, _ = w.Write(buf.Bytes())
	case TruncateClean:
		w.(http.Flusher).Flush()
		_, _ = w.Write(half)
	case TruncateCloseDelimited, TruncateCloseNotify:
		hijackAndClose(w, "HTTP/1.1 200 OK\r\nConnection: close\r\n\r\n", half, b == TruncateCloseNotify)
	case TruncateDeclared:
		hijackAndClose(w, fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(pkg)), half, false)
	case DeclareShort:
		w.Header().Set("Content-Length", strconv.Itoa(len(half)))
		_, _ = w.Write(half)
	case ResetMidBody:
		resetMidBody(w, pkg)
	case OversizedChunked:
		w.(http.Flusher).Flush()
		_, _ = w.Write(pkg)
		_, _ = w.Write(make([]byte, 1024))
	case OversizedDeclared:
		w.Header().Set("Content-Length", strconv.Itoa(len(pkg)+1024))
		_, _ = w.Write(pkg)
		_, _ = w.Write(make([]byte, 1024))
	case StallHeaders:
		wait(r, stop)
	case TrickleBody:
		w.Header().Set("Content-Length", strconv.Itoa(len(pkg)))
		w.WriteHeader(http.StatusOK)
		for _, c := range pkg {
			if _, err := w.Write([]byte{c}); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	case HugeHeader:
		w.Header().Set("X-Pad", strings.Repeat("a", 32<<10))
		w.Header().Set("Content-Length", strconv.Itoa(len(pkg)))
		_, _ = w.Write(pkg)
	case StallBody:
		w.Header().Set("Content-Length", strconv.Itoa(len(pkg)))
		_, _ = w.Write(half)
		w.(http.Flusher).Flush()
		wait(r, stop)
	default:
		serveRange(w, r, pkg, b)
	}
}

func wait(r *http.Request, stop <-chan struct{}) {
	select {
	case <-r.Context().Done():
	case <-stop:
	}
}

// resetMidBody writes a response that declares the full length, sends
// half of it, and closes the TCP socket with SO_LINGER 0 (a reset).
func resetMidBody(w http.ResponseWriter, pkg []byte) {
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(pkg))
	_, _ = rw.Write(pkg[:len(pkg)/2])
	_ = rw.Flush()
	if tc, ok := conn.(*tls.Conn); ok {
		if raw, ok := tc.NetConn().(*net.TCPConn); ok {
			_ = raw.SetLinger(0)
			_ = raw.Close()
			return
		}
	}
	_ = conn.Close()
}

// hijackAndClose writes head and body on the raw TLS connection, then ends
// it: with a TLS close_notify when closeNotify is set, else by closing the
// TCP socket under the TLS layer (a FIN at a record boundary, no alert, no
// reset).
func hijackAndClose(w http.ResponseWriter, head string, body []byte, closeNotify bool) {
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	_, _ = rw.WriteString(head)
	_, _ = rw.Write(body)
	_ = rw.Flush()
	if tc, ok := conn.(*tls.Conn); ok && !closeNotify {
		_ = tc.NetConn().Close()
		return
	}
	_ = conn.Close()
}

func serveRange(w http.ResponseWriter, r *http.Request, pkg []byte, b Behavior) {
	header := r.Header.Get("Range")
	if header == "" || b == IgnoreRange {
		w.Header().Set("Content-Length", strconv.Itoa(len(pkg)))
		_, _ = w.Write(pkg)
		return
	}
	first, last, ok := parseRange(header, len(pkg))
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(pkg)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	reported := fmt.Sprintf("bytes %d-%d/%d", first, last, len(pkg))
	switch b {
	case WrongContentRange:
		reported = fmt.Sprintf("bytes %d-%d/%d", first+1, last+1, len(pkg))
	case WrongContentRangeTotal:
		reported = fmt.Sprintf("bytes %d-%d/%d", first, last, len(pkg)+1)
	}
	w.Header().Set("Content-Range", reported)
	w.Header().Set("Content-Length", strconv.Itoa(last-first+1))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(pkg[first : last+1])
}

func parseRange(header string, size int) (int, int, bool) {
	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	a, b, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, false
	}
	first, err1 := strconv.Atoi(a)
	last, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || first < 0 || last < first || last >= size {
		return 0, 0, false
	}
	return first, last, true
}

// Segment is one segment of a synthetic package.
type Segment struct {
	ID     string
	Bytes  []byte
	SHA256 [32]byte
}

// Synthetic returns deterministic segments of the given sizes and their
// concatenation in order. The bytes are a SHA-256 counter stream keyed by
// label; nothing is media.
func Synthetic(label string, sizes []int) ([]Segment, []byte) {
	var pkg []byte
	segments := make([]Segment, 0, len(sizes))
	counter := 0
	for i, size := range sizes {
		data := make([]byte, 0, size)
		for len(data) < size {
			block := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", label, counter)))
			counter++
			data = append(data, block[:min(len(block), size-len(data))]...)
		}
		segments = append(segments, Segment{ID: fmt.Sprintf("s%04d", i), Bytes: data, SHA256: sha256.Sum256(data)})
		pkg = append(pkg, data...)
	}
	return segments, pkg
}
