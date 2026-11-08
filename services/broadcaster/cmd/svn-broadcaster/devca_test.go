package main

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDevCAArguments(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ca")
	for _, tc := range []struct {
		what     string
		args     []string
		code     int
		fragment string
	}{
		{"no --out", []string{"--principal", "a"}, exitUsage, "--out"},
		{"relative --out", []string{"--out", "ca", "--principal", "a"}, exitUsage, "--out"},
		{"unclean --out", []string{"--out", dir + "/x/../ca", "--principal", "a"}, exitUsage, "--out"},
		{"no principal", []string{"--out", out}, exitUsage, "--principal"},
		{"bad principal", []string{"--out", out, "--principal", "App"}, exitUsage, "principal"},
		{"principal twice", []string{"--out", out, "--principal", "a", "--principal", "a"}, exitUsage, "twice"},
		{"zero days", []string{"--out", out, "--principal", "a", "--days", "0"}, exitUsage, "--days"},
		{"too many days", []string{"--out", out, "--principal", "a", "--days", "31"}, exitUsage, "--days"},
		{"root serve uid", []string{"--out", out, "--principal", "a", "--serve-uid", "0"}, exitUsage, "non-root"},
		{"existing --out", []string{"--out", dir, "--principal", "a"}, exitUsage, "already exists"},
	} {
		runCmd(t, append([]string{"dev-ca"}, tc.args...)...).want(t, tc.what, tc.code, tc.fragment)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatalf("a refused dev-ca left %s: %v", out, err)
	}
	// Another serve uid needs root.
	runWith(t, []string{"PATH=/usr/bin"}, 1000, "dev-ca", "--out", out, "--principal", "a", "--serve-uid", "2000").want(t,
		"another serve uid without root", exitRefused, "not root")
	var many []string
	for i := 0; i <= maxPrincipals; i++ {
		many = append(many, "--principal", "p"+strconv.Itoa(i))
	}
	runCmd(t, append([]string{"dev-ca", "--out", out}, many...)...).want(t, "too many principals", exitUsage, "at most")
}

// TestDevCAWritesCertificates checks the development PKI of plan 5.6: a CA
// that can sign only leaf certificates, a server certificate valid for the
// loopback addresses only, one client certificate per principal whose only
// URI SAN is svn://principal/<id>, private files 0600, a 0700 directory,
// and no CA key on disk.
func TestDevCAWritesCertificates(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ca")
	r := runCmd(t, "dev-ca", "--out", out, "--principal", "app-fixture", "--principal", "provider-agent", "--days", "2")
	r.want(t, "dev-ca", exitOK)
	var files caFiles
	if err := json.Unmarshal([]byte(r.stdout), &files); err != nil {
		t.Fatalf("output %q: %v", r.stdout, err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 7 {
		t.Fatalf("wrote %d files, want 7 (CA, server pair, two client pairs)", len(entries))
	}
	assertMode(t, out, 0o700|os.ModeDir)
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		block, rest := pem.Decode(raw)
		if block == nil || len(rest) != 0 {
			t.Fatalf("%s is not one PEM block", e.Name())
		}
		if strings.HasSuffix(e.Name(), "-key.pem") {
			assertMode(t, filepath.Join(out, e.Name()), 0o600)
			if block.Type != "PRIVATE KEY" {
				t.Fatalf("%s holds %s", e.Name(), block.Type)
			}
		} else {
			assertMode(t, filepath.Join(out, e.Name()), 0o644)
			if block.Type != "CERTIFICATE" {
				t.Fatalf("%s holds %s: the CA key or another secret is on disk", e.Name(), block.Type)
			}
		}
	}
	ca := parseCert(t, files.CACert)
	if !ca.IsCA || !ca.MaxPathLenZero || ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatalf("CA %+v", ca)
	}
	if d := ca.NotAfter.Sub(time.Now()); d < 47*time.Hour || d > 49*time.Hour {
		t.Fatalf("CA valid for %v, want 2 days", d)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	server := parseCert(t, files.ServerCert)
	for _, host := range []string{"127.0.0.1", "::1"} {
		if _, err := server.Verify(x509.VerifyOptions{Roots: pool, DNSName: host,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			t.Fatalf("server certificate for %s: %v", host, err)
		}
	}
	if len(server.DNSNames) != 0 || len(server.URIs) != 0 || len(server.IPAddresses) != 2 ||
		!server.IPAddresses[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("server certificate names %v %v %v", server.DNSNames, server.URIs, server.IPAddresses)
	}
	if _, err := server.Verify(x509.VerifyOptions{Roots: pool, DNSName: "10.0.0.1"}); err == nil {
		t.Fatal("server certificate valid for a non-loopback address")
	}
	if _, err := tls.LoadX509KeyPair(files.ServerCert, files.ServerKey); err != nil {
		t.Fatalf("server pair: %v", err)
	}
	if len(files.Clients) != 2 {
		t.Fatalf("clients %+v", files.Clients)
	}
	for _, c := range files.Clients {
		cert := parseCert(t, c.Cert)
		if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			t.Fatalf("client %s: %v", c.Principal, err)
		}
		if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
			t.Fatalf("client %s certificate serves as a server certificate", c.Principal)
		}
		if len(cert.URIs) != 1 || cert.URIs[0].String() != principalURIPrefix+c.Principal || len(cert.IPAddresses) != 0 {
			t.Fatalf("client %s names %v %v", c.Principal, cert.URIs, cert.IPAddresses)
		}
		pair, err := tls.LoadX509KeyPair(c.Cert, c.Key)
		if err != nil {
			t.Fatalf("client %s pair: %v", c.Principal, err)
		}
		if _, ok := pair.PrivateKey.(*ecdsa.PrivateKey); !ok {
			t.Fatalf("client %s key type %T", c.Principal, pair.PrivateKey)
		}
	}
	if os.Geteuid() != 0 {
		t.Logf("euid %d is not root: the --serve-uid case (server key owned by another uid) needs root and did not run", os.Geteuid())
	} else {
		// Under root another uid can own the server key, and the
		// directory becomes traversable (0711) for it.
		out2 := filepath.Join(t.TempDir(), "ca2")
		runCmd(t, "dev-ca", "--out", out2, "--principal", "a", "--serve-uid", "3710099").want(t, "dev-ca for another uid", exitOK)
		info, err := os.Stat(filepath.Join(out2, "server-key.pem"))
		if err != nil {
			t.Fatal(err)
		}
		if st := info.Sys().(*syscall.Stat_t); st.Uid != 3710099 || info.Mode().Perm() != 0o600 {
			t.Fatalf("server key owned by %d mode %v", st.Uid, info.Mode())
		}
		assertMode(t, out2, 0o711|os.ModeDir)
		if err := os.RemoveAll(out2); err != nil {
			t.Fatal(err)
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != want {
		t.Fatalf("%s mode %v, want %v", path, info.Mode(), want)
	}
}

func parseCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("%s: no PEM block", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestDevDBArguments(t *testing.T) {
	secrets := t.TempDir()
	if err := os.Chmod(secrets, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(secrets, "plain")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	open := t.TempDir()
	if err := os.Chmod(open, 0o775); err != nil {
		t.Fatal(err)
	}
	// Every case names a bin dir without PostgreSQL, so no case can start
	// a cluster even if the check it targets were missing.
	nobin := filepath.Join(secrets, "nobin")
	ok := []string{"--chain-id", "shortvideo-dev-x", "--secrets-dir", secrets}
	for _, tc := range []struct {
		what     string
		args     []string
		code     int
		fragment string
	}{
		{"no chain id", []string{"--secrets-dir", secrets}, exitRefused, "shortvideo-dev-"},
		{"beta chain id", []string{"--chain-id", "shortvideo-beta-1", "--secrets-dir", secrets}, exitRefused, "shortvideo-dev-"},
		{"long chain id", []string{"--chain-id", "shortvideo-dev-" + strings.Repeat("x", 40), "--secrets-dir", secrets}, exitRefused, "at most"},
		{"start timeout 0", append([]string{"--start-timeout", "0s"}, ok...), exitUsage, "--start-timeout"},
		{"start timeout 10m", append([]string{"--start-timeout", "10m"}, ok...), exitUsage, "--start-timeout"},
		{"relative bin dir", append([]string{"--bin-dir", "bin"}, ok...), exitUsage, "--bin-dir"},
		{"no secrets dir", []string{"--chain-id", "shortvideo-dev-x"}, exitUsage, "--secrets-dir"},
		{"relative secrets dir", []string{"--chain-id", "shortvideo-dev-x", "--secrets-dir", "s"}, exitUsage, "--secrets-dir"},
		{"absent secrets dir", []string{"--chain-id", "shortvideo-dev-x", "--secrets-dir", filepath.Join(secrets, "absent")}, exitRefused, "--secrets-dir"},
		{"secrets file", []string{"--chain-id", "shortvideo-dev-x", "--secrets-dir", file}, exitRefused, "not a real directory"},
		{"group-writable secrets dir", []string{"--chain-id", "shortvideo-dev-x", "--secrets-dir", open}, exitRefused, "write bit"},
		{"bad login role", append([]string{"--login-role", "App"}, ok...), exitUsage, "role"},
		{"reserved login role", append([]string{"--login-role", "pg_monitor"}, ok...), exitUsage, "reserved"},
		{"owner role as login role", append([]string{"--login-role", "svn_broadcaster_owner"}, ok...), exitUsage, "owner"},
		{"login role twice", append([]string{"--login-role", "a_sql", "--login-role", "a_sql"}, ok...), exitUsage, "twice"},
		{"root serve uid", append([]string{"--serve-uid", "0"}, ok...), exitUsage, "non-root"},
	} {
		args := append([]string{"dev-db"}, tc.args...)
		if !strings.Contains(tc.what, "bin dir") {
			args = append(args, "--bin-dir", nobin)
		}
		runCmd(t, args...).want(t, tc.what, tc.code, tc.fragment)
	}
	stale := filepath.Join(secrets, "svn_broadcaster.password")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	runCmd(t, append([]string{"dev-db", "--bin-dir", nobin}, ok...)...).want(t, "stale password file", exitRefused,
		"already holds svn_broadcaster.password")
	if err := os.Remove(stale); err != nil {
		t.Fatal(err)
	}
	runWith(t, []string{"PATH=/usr/bin"}, uint32(os.Geteuid())+1, append([]string{"dev-db", "--bin-dir", nobin}, ok...)...).want(t,
		"secrets dir of another uid", exitRefused, "not owned")
	runWith(t, []string{"PATH=/usr/bin"}, 1000, append([]string{"dev-db", "--serve-uid", "2000", "--bin-dir", nobin}, ok...)...).want(t,
		"another serve uid without root", exitRefused, "not root")
	// A bin dir without PostgreSQL fails before any directory is made.
	before := svnPGDirs(t)
	runCmd(t, append([]string{"dev-db", "--bin-dir", nobin}, ok...)...).want(t,
		"bin dir without postgres", exitFailure, "start PostgreSQL")
	if after := svnPGDirs(t); len(after) != len(before) {
		t.Fatalf("a failed dev-db left a cluster directory: %v", after)
	}
	if entries, err := os.ReadDir(secrets); err != nil || len(entries) != 1 {
		t.Fatalf("a refused dev-db wrote to the secrets directory: %v %v", entries, err)
	}
}

// svnPGDirs lists os.TempDir()/svn-pg-* entries.
func svnPGDirs(t *testing.T) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(os.TempDir(), "svn-pg-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
