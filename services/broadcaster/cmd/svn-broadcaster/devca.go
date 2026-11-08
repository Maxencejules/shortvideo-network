package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// dev-ca bounds.
const (
	minCADays     = 1
	maxCADays     = 30
	defaultCADays = 7
	maxPrincipals = 64
	// principalURIPrefix is the caller certificate's URI SAN prefix (plan
	// 5.6); the API reads the principal id after it.
	principalURIPrefix = "svn://principal/"
)

// principalList is the repeatable --principal flag.
type principalList []string

func (p *principalList) String() string { return fmt.Sprint([]string(*p)) }

func (p *principalList) Set(v string) error {
	if len(*p) == maxPrincipals {
		return fmt.Errorf("at most %d principals", maxPrincipals)
	}
	if !msgkit.ValidID(v) {
		return errors.New("a principal must match ^[a-z0-9][a-z0-9._-]{0,63}$")
	}
	for _, have := range *p {
		if have == v {
			return errors.New("a principal is named twice")
		}
	}
	*p = append(*p, v)
	return nil
}

// caFiles is dev-ca's output on standard output: the paths it wrote.
type caFiles struct {
	Dir        string       `json:"dir"`
	CACert     string       `json:"ca_cert_file"`
	ServerCert string       `json:"server_cert_file"`
	ServerKey  string       `json:"server_key_file"`
	Clients    []clientFile `json:"clients"`
	NotAfter   time.Time    `json:"not_after"`
}

type clientFile struct {
	Principal string `json:"principal"`
	Cert      string `json:"cert_file"`
	Key       string `json:"key_file"`
}

// runDevCA is the dev-ca subcommand (plan 5.6, DR-11-32): a development
// CA generated at run time, a server certificate for the loopback
// addresses, and one client certificate per caller principal whose only
// URI SAN is svn://principal/<id>. The CA's key exists in memory only, so
// no later certificate can be issued under this CA. The output directory
// must not exist; it is created mode 0700, or 0711 when --serve-uid names
// another uid, which then owns the server key file.
func runDevCA(_ context.Context, args []string, env processEnv, log *slog.Logger) error {
	flags := newFlags("dev-ca", env)
	out := flags.String("out", "", "absolute path of the directory to create (required; must not exist)")
	var principals principalList
	flags.Var(&principals, "principal", "a caller principal id to issue a client certificate for (repeatable; at least one)")
	days := flags.Int("days", defaultCADays, fmt.Sprintf("validity in days, %d to %d", minCADays, maxCADays))
	serveUID := flags.Int64("serve-uid", -1, "uid that owns the server key file (default: this process's uid; another uid needs root)")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	switch {
	case *out == "" || !filepath.IsAbs(*out) || filepath.Clean(*out) != *out:
		return usageError(errors.New("--out must be a clean absolute path"))
	case len(principals) == 0:
		return usageError(errors.New("at least one --principal is required"))
	case *days < minCADays || *days > maxCADays:
		return usageError(fmt.Errorf("--days must be %d to %d", minCADays, maxCADays))
	}
	owner, err := fileOwner(*serveUID, env.euid)
	if err != nil {
		return err
	}
	files, err := writeDevCA(*out, principals, time.Duration(*days)*24*time.Hour, owner, env.euid)
	if err != nil {
		return err
	}
	log.Info("development CA written", "dir", files.Dir, "clients", int64(len(files.Clients)))
	return json.NewEncoder(env.stdout).Encode(files)
}

// fileOwner returns the uid that owns the files the serve process reads:
// flag, or euid when the flag is unset (-1). Another uid needs root.
func fileOwner(flag int64, euid uint32) (uint32, error) {
	switch {
	case flag == -1:
		return euid, nil
	case flag < 1 || flag > 1<<31-1:
		return 0, usageError(errors.New("--serve-uid must be a non-root uid"))
	case uint32(flag) != euid && euid != 0:
		return 0, refusedError(errors.New("--serve-uid names another uid and this process is not root"))
	}
	return uint32(flag), nil
}

// writeDevCA creates dir and writes the CA certificate, the server
// certificate and key, and the client certificates and keys.
func writeDevCA(dir string, principals []string, validity time.Duration, serveUID, euid uint32) (caFiles, error) {
	now := time.Now().UTC().Truncate(time.Second)
	notBefore, notAfter := now.Add(-time.Hour), now.Add(validity)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return caFiles{}, fmt.Errorf("generate CA key: %w", err)
	}
	caTmpl := &x509.Certificate{
		Subject:   pkix.Name{CommonName: "svn-broadcaster development CA"},
		NotBefore: notBefore, NotAfter: notAfter,
		IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	if caTmpl.SerialNumber, err = serial(); err != nil {
		return caFiles{}, err
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return caFiles{}, fmt.Errorf("create CA certificate: %w", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return caFiles{}, fmt.Errorf("parse CA certificate: %w", err)
	}
	mode := os.FileMode(0o700)
	if serveUID != euid {
		mode = 0o711
	}
	if err := os.Mkdir(dir, mode); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return caFiles{}, usageError(errors.New("--out already exists"))
		}
		return caFiles{}, fmt.Errorf("create output directory: %w", err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		return caFiles{}, fmt.Errorf("chmod output directory: %w", err)
	}
	files := caFiles{Dir: dir, CACert: filepath.Join(dir, "ca.pem"), ServerCert: filepath.Join(dir, "server.pem"),
		ServerKey: filepath.Join(dir, "server-key.pem"), NotAfter: notAfter}
	if err := writeNew(files.CACert, pemBlock("CERTIFICATE", caDER), 0o644, euid, euid); err != nil {
		return caFiles{}, err
	}
	server := &x509.Certificate{
		Subject:     pkix.Name{CommonName: "svn-broadcaster"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if err := issue(server, ca, caKey, notBefore, notAfter, files.ServerCert, files.ServerKey, serveUID, euid); err != nil {
		return caFiles{}, err
	}
	for _, p := range principals {
		u, err := url.Parse(principalURIPrefix + p)
		if err != nil {
			return caFiles{}, fmt.Errorf("principal URI: %w", err)
		}
		cf := clientFile{Principal: p, Cert: filepath.Join(dir, "client-"+p+".pem"), Key: filepath.Join(dir, "client-"+p+"-key.pem")}
		client := &x509.Certificate{
			Subject:     pkix.Name{CommonName: p},
			URIs:        []*url.URL{u},
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		if err := issue(client, ca, caKey, notBefore, notAfter, cf.Cert, cf.Key, euid, euid); err != nil {
			return caFiles{}, err
		}
		files.Clients = append(files.Clients, cf)
	}
	return files, nil
}

// issue creates a P-256 key and a certificate from tmpl signed by the CA,
// and writes the certificate (0644) and the key (0600, owned by owner).
func issue(tmpl, ca *x509.Certificate, caKey *ecdsa.PrivateKey, notBefore, notAfter time.Time,
	certPath, keyPath string, owner, euid uint32) error {
	leaf, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	if tmpl.SerialNumber, err = serial(); err != nil {
		return err
	}
	tmpl.NotBefore, tmpl.NotAfter = notBefore, notAfter
	tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &leaf.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leaf)
	if err != nil {
		return fmt.Errorf("encode key: %w", err)
	}
	if err := writeNew(certPath, pemBlock("CERTIFICATE", der), 0o644, euid, euid); err != nil {
		return err
	}
	return writeNew(keyPath, pemBlock("PRIVATE KEY", keyDER), 0o600, owner, euid)
}

// serial returns a random 127-bit certificate serial number.
func serial() (*big.Int, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, fmt.Errorf("serial number: %w", err)
	}
	return n.Add(n, big.NewInt(1)), nil
}

func pemBlock(kind string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der})
}

// writeNew creates path (it must not exist and is not followed if it is a
// symlink), writes data, sets mode, and gives the file to owner (uid and
// gid) when owner is not euid, this process's uid.
func writeNew(path string, data []byte, mode os.FileMode, owner, euid uint32) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Chmod(mode)
	}
	if werr == nil && owner != euid {
		werr = f.Chown(int(owner), int(owner))
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), werr)
	}
	return nil
}
