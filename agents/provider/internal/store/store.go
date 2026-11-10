// Package store persists the agent's offers, transfers, and leases.
// The file is rewritten by rename. A crash leaves the previous file.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	Transferring      = "TRANSFERRING"
	Staged            = "STAGED"
	AcceptancePending = "ACCEPTANCE_PENDING"
	Committed         = "COMMITTED"
	Serving           = "SERVING"
	Expired           = "EXPIRED"
	Removed           = "REMOVED"
)

// Public is a retained public key. Private keys are not stored here.
type Public struct {
	KeyID      string `json:"key_id"`
	Generation uint64 `json:"generation"`
	PublicHex  string `json:"public_hex"`
}

// Lease is one local obligation.
type Lease struct {
	OfferID        string   `json:"offer_id"`
	LeaseID        string   `json:"lease_id"`
	BusinessID     string   `json:"business_id"`
	ContentID      string   `json:"content_id"`
	ContentVersion uint64   `json:"content_version"`
	PackageBytes   uint64   `json:"package_bytes"`
	ManifestSHA    string   `json:"manifest_sha256"`
	TermsSHA       string   `json:"terms_sha256"`
	Start          int64    `json:"start_unix"`
	Deadline       int64    `json:"deadline_unix"`
	Expiry         int64    `json:"expiry_unix"`
	TermVersion    uint64   `json:"term_version"`
	SchedulerHex   string   `json:"scheduler_hex"`
	Readiness      string   `json:"readiness"`
	KeyID          string   `json:"key_id"`
	PublicHex      string   `json:"public_hex"`
	SignatureHex   string   `json:"signature_hex"`
	Canonical      string   `json:"canonical"`
	ServeToken     string   `json:"serve_token"`
	Segments       []string `json:"segments"`
	Broadcast      bool     `json:"broadcast"`
}

// State is the whole local record.
type State struct {
	Generation      uint64            `json:"generation"`
	KeyID           string            `json:"key_id"`
	PublicHex       string            `json:"public_hex"`
	Previous        []Public          `json:"previous"`
	Draining        bool              `json:"draining"`
	UsedBytes       uint64            `json:"used_bytes"`
	ChainHeight     uint64            `json:"chain_height"`
	ChainHeightUnix int64             `json:"chain_height_unix"`
	Uncertain       bool              `json:"chain_uncertain"`
	Leases          map[string]*Lease `json:"leases"`
	OfferLease      map[string]string `json:"offer_lease"`
	Rotations       []string          `json:"rotations"`
	Drains          []string          `json:"drains"`
	Ends            []string          `json:"ends"`
}

// Open reads path, or returns an empty state when the file is absent.
func Open(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{Leases: map[string]*Lease{}, OfferLease: map[string]string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if st.Leases == nil {
		st.Leases = map[string]*Lease{}
	}
	if st.OfferLease == nil {
		st.OfferLease = map[string]string{}
	}
	return &st, nil
}

// Save writes path atomically.
func Save(path string, st *State) error {
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// KeyPath is the private key file for a generation.
func KeyPath(dir string, generation uint64) string {
	return filepath.Join(dir, "keys", fmt.Sprintf("%d.hex", generation))
}
