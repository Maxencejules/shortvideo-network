// Package stage is a loopback HTTPS package source.
// Origin bytes and a repair buffer are separate maps. Neither map is a
// counted replica. The repair buffer is discarded by the caller.
package stage

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Object is one package the source can serve.
type Object struct {
	Manifest string
	Parts    map[string][]byte
}

// Stage serves /pkg/{id}/manifest and /pkg/{id}/segments/{segment}.
type Stage struct {
	URL   string
	CAPEM []byte

	mu       sync.Mutex
	origin   map[string]Object
	repair   map[string]Object
	originUp map[string]bool
	token    string
	srv      *http.Server
	ln       net.Listener
}

// Listen binds 127.0.0.1 on an ephemeral port. The certificate is generated
// in memory and is not a provider key.
func Listen(token string) (*Stage, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "svn-scheduler-stage"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Stage{
		URL: "https://" + ln.Addr().String() + "/pkg", token: token,
		origin: map[string]Object{}, repair: map[string]Object{}, originUp: map[string]bool{},
		CAPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		ln:    ln,
	}
	s.srv = &http.Server{
		Handler:           http.HandlerFunc(s.serve),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = s.srv.Serve(tls.NewListener(ln, s.srv.TLSConfig)) }()
	return s, nil
}

// Close stops the listener.
func (s *Stage) Close() error {
	return s.srv.Close()
}

// Addr is host, ip, and port for a provider destination entry.
func (s *Stage) Addr() (string, uint16) {
	tcp, ok := s.ln.Addr().(*net.TCPAddr)
	if !ok {
		return "", 0
	}
	return tcp.IP.String(), uint16(tcp.Port)
}

// PutOrigin stores the creator backup. It is not a counted replica.
func (s *Stage) PutOrigin(id string, obj Object) {
	s.mu.Lock()
	s.origin[id] = obj
	s.originUp[id] = true
	s.mu.Unlock()
}

// DisconnectOrigin makes the creator backup unreachable. Repair buffers stay.
func (s *Stage) DisconnectOrigin(id string) {
	s.mu.Lock()
	s.originUp[id] = false
	s.mu.Unlock()
}

// ConnectOrigin restores the creator backup path.
func (s *Stage) ConnectOrigin(id string) {
	s.mu.Lock()
	s.originUp[id] = true
	s.mu.Unlock()
}

// PutRepair stores bytes fetched from a survivor for one transfer.
func (s *Stage) PutRepair(id string, obj Object) {
	s.mu.Lock()
	s.repair[id] = obj
	s.mu.Unlock()
}

// ClearRepair drops the transfer buffer.
func (s *Stage) ClearRepair(id string) {
	s.mu.Lock()
	delete(s.repair, id)
	s.mu.Unlock()
}

func (s *Stage) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "pkg" {
		http.NotFound(w, r)
		return
	}
	id := parts[1]
	s.mu.Lock()
	obj, repair := s.repair[id]
	ok := repair
	if !ok {
		obj, ok = s.origin[id]
		if ok && !s.originUp[id] {
			s.mu.Unlock()
			http.Error(w, "origin disconnected", http.StatusServiceUnavailable)
			return
		}
	}
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case len(parts) == 3 && parts[2] == "manifest":
		_, _ = w.Write([]byte(obj.Manifest))
	case len(parts) == 4 && parts[2] == "segments":
		buf, found := obj.Parts[parts[3]]
		if !found {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(buf)
	default:
		http.NotFound(w, r)
	}
}
