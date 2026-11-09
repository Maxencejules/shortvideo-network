package api

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/url"
	"testing"
)

// TestClientCertRequired checks plan 5.6 (DR-11-32): the TLS 1.3 listener
// requires and verifies a client certificate from the configured CA, so a
// client without one, or with one from another CA, gets no HTTP answer at
// all; a verified certificate without exactly one svn://principal/<id>
// URI SAN is 401; a verified principal that is not a configured caller is
// 403; a configured caller is served.
func TestClientCertRequired(t *testing.T) {
	e := newEnv(t)
	if _, err := httpClient(e.pki.caPool, nil).Get(e.base + "/v1/health"); err == nil {
		t.Fatal("a client without a certificate was served")
	}
	foreign := newPKI(t)
	fc := foreign.client("svn://principal/app-fixture")
	if _, err := httpClient(e.pki.caPool, &fc).Get(e.base + "/v1/health"); err == nil {
		t.Fatal("a certificate from another CA was served")
	}
	tls12 := e.pki.client("svn://principal/app-fixture")
	old := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: e.pki.caPool, Certificates: []tls.Certificate{tls12}, MaxVersion: tls.VersionTLS12,
	}}}
	if _, err := old.Get(e.base + "/v1/health"); err == nil {
		t.Fatal("a TLS 1.2 client was served")
	}

	for name, uris := range map[string][]string{
		"no URI SAN":          nil,
		"two principals":      {"svn://principal/app-fixture", "svn://principal/other-app"},
		"principal and other": {"svn://principal/app-fixture", "https://example.org/x"},
		"other scheme":        {"https://principal/app-fixture"},
		"other host":          {"svn://caller/app-fixture"},
		"bad id":              {"svn://principal/App%20Fixture"},
		"nested path":         {"svn://principal/app-fixture/extra"},
		"query":               {"svn://principal/app-fixture?admin=1"},
		"user info":           {"svn://root@principal/app-fixture"},
		"port":                {"svn://principal:1/app-fixture"},
		"empty id":            {"svn://principal/"},
	} {
		cert := e.pki.client(uris...)
		a := e.do(httpClient(e.pki.caPool, &cert), http.MethodGet, "/v1/health", nil)
		if a.status != http.StatusUnauthorized || a.code(t) != codeUnauthenticated {
			t.Fatalf("%s: %d %s", name, a.status, a.body)
		}
	}
	if a := e.do(e.as("not-configured"), http.MethodGet, "/v1/health", nil); a.status != http.StatusForbidden {
		t.Fatalf("unconfigured principal: %d %s", a.status, a.body)
	}
	if a := e.do(e.as("app-fixture"), http.MethodGet, "/v1/health", nil); a.status != http.StatusOK {
		t.Fatalf("configured caller: %d %s", a.status, a.body)
	}
}

func TestPrincipalOf(t *testing.T) {
	leaf := func(uris ...string) *tls.ConnectionState {
		c := &x509.Certificate{}
		for _, s := range uris {
			u, err := url.Parse(s)
			if err != nil {
				t.Fatal(err)
			}
			c.URIs = append(c.URIs, u)
		}
		return &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{c}}}
	}
	if p, err := PrincipalOf(leaf("svn://principal/app-fixture")); err != nil || p != "app-fixture" {
		t.Fatalf("%q %v", p, err)
	}
	for _, st := range []*tls.ConnectionState{nil, {}, {VerifiedChains: [][]*x509.Certificate{{}}},
		leaf("svn://principal/a#frag"), leaf("svn:opaque"), leaf("svn://principal/a?")} {
		if _, err := PrincipalOf(st); err == nil {
			t.Fatalf("%v accepted", st)
		}
	}
}
