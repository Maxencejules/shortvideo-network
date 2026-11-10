// Package agent is the storage provider's local state machine.
// Readiness is STAGED, ACCEPTANCE_PENDING, COMMITTED, or SERVING.
// A chain observation is not a light-client proof. Several identities in
// this process are not independent operators.
package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/config"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/keys"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
)

// Error is a refusal with an HTTP status. Code is a short stable string.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string { return e.Code }

func fail(status int, code string) error { return &Error{Status: status, Code: code} }

// Agent is one provider process.
type Agent struct {
	cfg      config.File
	mu       sync.Mutex
	st       *store.State
	key      keys.Key
	url      string
	token    string
	source   string
	heightAt time.Time
	sourceC  *tls.Config
	chainC   *tls.Config
	cancel   context.CancelFunc
	ctx      context.Context
}

// Open loads the key and state for cfg. Hosting consent was checked when
// the config was loaded. dataDir is cfg.DataDir.
func Open(cfg config.File) (*Agent, error) {
	st, err := store.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	if st.Generation == 0 {
		st.Generation = 1
		st.KeyID = cfg.KeyID
	}
	key, err := keys.Load(store.KeyPath(cfg.DataDir, st.Generation))
	if err != nil {
		return nil, err
	}
	pub := hex.EncodeToString(key.Public())
	if st.PublicHex == "" {
		st.PublicHex = pub
	}
	if st.PublicHex != pub || st.KeyID == "" {
		return nil, fmt.Errorf("agent: key file does not match state")
	}
	admin, err := os.ReadFile(filepath.Join(cfg.DataDir, "admin.token"))
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(admin))
	if token == "" {
		return nil, fmt.Errorf("agent: admin token is empty")
	}
	sourceRaw, err := os.ReadFile(filepath.Join(cfg.DataDir, "source.token"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	changed := time.Time{}
	if st.ChainHeightUnix != 0 {
		changed = time.Unix(st.ChainHeightUnix, 0)
	}
	a := &Agent{
		cfg: cfg, st: st, key: key, token: token, heightAt: changed,
		source: strings.TrimSpace(string(sourceRaw)),
		ctx:    ctx, cancel: cancel,
	}
	if cfg.SourceCA != "" {
		a.sourceC, err = clientTLS(cfg.SourceCA)
		if err != nil {
			cancel()
			return nil, err
		}
	}
	if cfg.ChainCA != "" {
		a.chainC, err = clientTLS(cfg.ChainCA)
		if err != nil {
			cancel()
			return nil, err
		}
	}
	if err := a.reconcileDisk(); err != nil {
		cancel()
		return nil, err
	}
	if err := a.save(); err != nil {
		cancel()
		return nil, err
	}
	return a, nil
}

// Close stops fetches.
func (a *Agent) Close() { a.cancel() }

// SetURL records the HTTPS URL of this process after it binds.
func (a *Agent) SetURL(raw string) {
	a.mu.Lock()
	a.url = raw
	a.mu.Unlock()
}

// Authorize reports whether the admin bearer matches. A mismatch changes nothing.
func (a *Agent) Authorize(got string) bool {
	return got != "" && got == a.token
}

func (a *Agent) save() error {
	return store.Save(filepath.Join(a.cfg.DataDir, "state.json"), a.st)
}

func clientTLS(path string) (*tls.Config, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("agent: ca %s", path)
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

// reconcileDisk drops a partial transfer and recomputes used bytes from
// the leases whose files are still present.
func (a *Agent) reconcileDisk() error {
	var used uint64
	for id, lease := range a.st.Leases {
		if lease.Readiness == store.Transferring {
			os.RemoveAll(filepath.Join(a.cfg.DataDir, "staging", id))
			delete(a.st.Leases, id)
			delete(a.st.OfferLease, lease.OfferID)
			continue
		}
		if lease.Readiness == store.Removed || lease.Readiness == store.Expired {
			continue
		}
		if !filesPresent(a.cfg.DataDir, lease) {
			lease.Readiness = store.Removed
			os.RemoveAll(packageDir(a.cfg.DataDir, lease))
			continue
		}
		if used > ^uint64(0)-lease.PackageBytes {
			return fmt.Errorf("agent: used bytes overflow")
		}
		used += lease.PackageBytes
	}
	a.st.UsedBytes = used
	return nil
}

func filesPresent(dir string, lease *store.Lease) bool {
	for _, id := range lease.Segments {
		if _, err := os.Stat(filepath.Join(packageDir(dir, lease), id)); err != nil {
			return false
		}
	}
	return len(lease.Segments) > 0
}

func packageDir(dir string, lease *store.Lease) string {
	return filepath.Join(dir, "packages", lease.LeaseID)
}
