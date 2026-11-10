package agent

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/disk"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/keys"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/sign"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
)

// Status is the public status document. independent_operator and
// physical_redundancy are false: this process cannot prove either.
type Status struct {
	OK                  bool          `json:"ok"`
	ProviderID          string        `json:"provider_id"`
	MachineID           string        `json:"machine_id"`
	IndependentOperator bool          `json:"independent_operator"`
	PhysicalRedundancy  bool          `json:"physical_redundancy"`
	SameMachineNote     string        `json:"same_machine_note"`
	HostingConsent      bool          `json:"hosting_consent"`
	Draining            bool          `json:"draining"`
	UsedBytes           uint64        `json:"used_bytes"`
	QuotaBytes          uint64        `json:"quota_bytes"`
	DiskAvailableBytes  uint64        `json:"disk_available_bytes"`
	ClockUncertainty    int64         `json:"clock_uncertainty_seconds"`
	ChainUncertain      bool          `json:"chain_uncertain"`
	ChainHeight         uint64        `json:"chain_height"`
	CommitmentCertainty string        `json:"commitment_certainty"`
	ChainObservation    string        `json:"chain_observation"`
	LightClient         bool          `json:"light_client"`
	IPFS                bool          `json:"ipfs"`
	KeyID               string        `json:"key_id"`
	KeyGeneration       uint64        `json:"key_generation"`
	Leases              []StatusLease `json:"leases"`
}

// StatusLease is one row of the status list.
type StatusLease struct {
	LeaseID           string `json:"lease_id"`
	ContentID         string `json:"content_id"`
	Readiness         string `json:"readiness"`
	Obligation        string `json:"obligation"`
	Advertised        bool   `json:"advertised"`
	DeadlineUnix      int64  `json:"deadline_unix"`
	DeadlineCertainty string `json:"deadline_certainty"`
	PackageBytes      uint64 `json:"package_bytes"`
}

// StatusReport refreshes chain observations and returns the status.
func (a *Agent) StatusReport() (Status, error) {
	a.refresh()
	avail, err := disk.Available(a.cfg.DataDir)
	if err != nil {
		return Status{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.status(avail)
	return out, nil
}

func (a *Agent) status(avail uint64) Status {
	certainty := "observed"
	if a.st.Uncertain {
		certainty = "uncertain"
	}
	rows := make([]StatusLease, 0, len(a.st.Leases))
	now := time.Now().Unix()
	for _, lease := range a.st.Leases {
		obligation := ""
		if lease.Readiness == store.Committed || lease.Readiness == store.Serving {
			obligation = store.Committed
		}
		edge := "outside"
		if lease.Deadline-now <= a.cfg.ClockUncertaintySeconds && now-lease.Deadline <= a.cfg.ClockUncertaintySeconds {
			edge = "uncertain"
		}
		rows = append(rows, StatusLease{
			LeaseID: lease.LeaseID, ContentID: lease.ContentID, Readiness: lease.Readiness,
			Obligation: obligation, Advertised: lease.Readiness == store.Serving,
			DeadlineUnix: lease.Deadline, DeadlineCertainty: edge, PackageBytes: lease.PackageBytes,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].LeaseID < rows[j].LeaseID })
	return Status{
		OK: true, ProviderID: a.cfg.ProviderID, MachineID: a.cfg.MachineID,
		IndependentOperator: false, PhysicalRedundancy: false,
		SameMachineNote: "several identities on one machine are not independent ownership or physical redundancy",
		HostingConsent:  a.cfg.HostingConsent, Draining: a.st.Draining,
		UsedBytes: a.st.UsedBytes, QuotaBytes: a.cfg.QuotaBytes, DiskAvailableBytes: avail,
		ClockUncertainty: a.cfg.ClockUncertaintySeconds, ChainUncertain: a.st.Uncertain,
		ChainHeight: a.st.ChainHeight, CommitmentCertainty: certainty,
		ChainObservation: "operator_endpoint", LightClient: false, IPFS: false,
		KeyID: a.st.KeyID, KeyGeneration: a.st.Generation, Leases: rows,
	}
}

// Enrollment is the public enrollment record.
func (a *Agent) Enrollment() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return map[string]any{
		"provider_id": a.cfg.ProviderID, "key_id": a.st.KeyID,
		"public_key_hex": a.st.PublicHex, "address_hex": hex.EncodeToString(a.key.Address()),
		"machine_id": a.cfg.MachineID, "hosting_consent": true, "production_keys": false,
		"independent_operator": false,
	}
}

// CapabilityDocument signs the current endpoint manifest.
func (a *Agent) CapabilityDocument() (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.url == "" {
		return nil, fail(503, "not listening")
	}
	res, sig, err := sign.Capability(a.key.Private(), capability.Manifest{
		ProviderID: a.cfg.ProviderID, SigningKeyID: a.st.KeyID,
		DeclaredCapacityBytes: a.cfg.DeclaredCapacityBytes,
		Endpoints:             []capability.Endpoint{{Name: "https", URL: a.url}},
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"canonical": res.Canonical, "digest_hex": hex.EncodeToString(res.SHA256[:]),
		"signature_hex":  hex.EncodeToString(sig.GetSignature()),
		"public_key_hex": hex.EncodeToString(sig.GetPublicKey()), "key_id": sig.GetKeyId(),
	}, nil
}

// Inventory signs the current inventory. It is an attestation, not a storage proof.
func (a *Agent) Inventory() (map[string]any, error) {
	a.refresh()
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.st.Leases))
	for id := range a.st.Leases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]any, 0, len(ids))
	for _, id := range ids {
		lease := a.st.Leases[id]
		if lease.Readiness == store.Transferring {
			continue
		}
		entries = append(entries, map[string]any{
			"contentId": lease.ContentID, "contentVersion": strconv.FormatUint(lease.ContentVersion, 10),
			"leaseId": lease.LeaseID, "packageBytes": strconv.FormatUint(lease.PackageBytes, 10),
			"readiness": lease.Readiness, "sha256": lease.ManifestSHA,
		})
	}
	fields := map[string]any{
		"claimLimit": "ATTESTATION_NOT_STORAGE_PROOF",
		"entries":    entries, "generatedUnixSeconds": strconv.FormatInt(time.Now().Unix(), 10),
		"independentOperator": false, "machineId": a.cfg.MachineID, "physicalRedundancy": false,
		"providerId": a.cfg.ProviderID, "quotaBytes": strconv.FormatUint(a.cfg.QuotaBytes, 10),
		"usedBytes": strconv.FormatUint(a.st.UsedBytes, 10),
	}
	sig, canonical, err := sign.Inventory(a.key.Private(), a.st.KeyID, fields)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"canonical": canonical, "signature_hex": hex.EncodeToString(sig.GetSignature()),
		"public_key_hex": hex.EncodeToString(sig.GetPublicKey()), "key_id": a.st.KeyID,
		"claim_limit": "ATTESTATION_NOT_STORAGE_PROOF",
	}, nil
}

// Receipt signs a transfer receipt for a lease the provider has checked.
func (a *Agent) Receipt(id string) (map[string]any, error) {
	if !msgkit.ValidID(id) {
		return nil, fail(400, "malformed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lease := a.st.Leases[id]
	if lease == nil || lease.SignatureHex == "" {
		return nil, fail(404, "not found")
	}
	fields := map[string]any{
		"claimLimit": "ATTESTATION_NOT_STORAGE_PROOF", "contentId": lease.ContentID,
		"contentVersion": strconv.FormatUint(lease.ContentVersion, 10),
		"manifestDigest": map[string]any{"algorithm": "DIGEST_ALGORITHM_SHA256", "value": lease.ManifestSHA},
		"packageBytes":   strconv.FormatUint(lease.PackageBytes, 10), "providerId": a.cfg.ProviderID,
		"readiness": lease.Readiness, "transferId": lease.LeaseID,
	}
	sig, canonical, err := sign.Receipt(a.key.Private(), a.st.KeyID, fields)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"canonical": canonical, "signature_hex": hex.EncodeToString(sig.GetSignature()),
		"public_key_hex": hex.EncodeToString(sig.GetPublicKey()), "key_id": a.st.KeyID,
	}, nil
}

// Package returns the stored bytes when the lease is SERVING and token matches.
func (a *Agent) Package(contentID, version, token string) ([]byte, error) {
	if !msgkit.ValidID(contentID) {
		return nil, fail(400, "malformed")
	}
	ver, err := strconv.ParseUint(version, 10, 64)
	if err != nil || ver == 0 {
		return nil, fail(400, "malformed")
	}
	a.refresh()
	a.mu.Lock()
	defer a.mu.Unlock()
	var lease *store.Lease
	for _, item := range a.st.Leases {
		if item.ContentID == contentID && item.ContentVersion == ver {
			lease = item
			break
		}
	}
	if lease == nil || token == "" || token != lease.ServeToken {
		if lease != nil && lease.Readiness == store.Serving && token != lease.ServeToken {
			return nil, fail(401, "unauthorized")
		}
		if lease != nil && (lease.Readiness == store.Expired || lease.Readiness == store.Removed) {
			return nil, fail(410, "gone")
		}
		return nil, fail(404, "not found")
	}
	if lease.Readiness != store.Serving {
		return nil, fail(404, "not found")
	}
	var out []byte
	for _, id := range lease.Segments {
		buf, err := os.ReadFile(filepath.Join(packageDir(a.cfg.DataDir, lease), id))
		if err != nil {
			return nil, fail(404, "not found")
		}
		out = append(out, buf...)
	}
	return out, nil
}

// Drain stops new offers and signs the drain payload. Accepted work stays.
func (a *Agent) Drain() (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sig, canonical, err := sign.Drain(a.key.Private(), a.st.KeyID, a.cfg.ProviderID, a.st.Generation)
	if err != nil {
		return nil, err
	}
	a.st.Draining = true
	a.st.Drains = append(a.st.Drains, hex.EncodeToString(sig.GetSignature()))
	if err := a.save(); err != nil {
		return nil, err
	}
	return map[string]any{
		"draining": true, "canonical": canonical,
		"signature_hex": hex.EncodeToString(sig.GetSignature()), "key_id": a.st.KeyID,
	}, nil
}

// Withdraw removes one lease locally and signs an end. It is not a chain commit.
func (a *Agent) Withdraw(id string) (map[string]any, error) {
	if !msgkit.ValidID(id) {
		return nil, fail(400, "malformed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lease := a.st.Leases[id]
	if lease == nil {
		return nil, fail(404, "not found")
	}
	sig, canonical, err := sign.End(a.key.Private(), a.st.KeyID, lease.LeaseID, a.cfg.ProviderID, lease.TermVersion)
	if err != nil {
		return nil, err
	}
	if lease.Readiness != store.Removed && lease.Readiness != store.Expired {
		removePackage(a.cfg.DataDir, lease)
		a.release(lease.PackageBytes)
	}
	lease.Readiness = store.Removed
	lease.ServeToken = ""
	a.st.Ends = append(a.st.Ends, hex.EncodeToString(sig.GetSignature()))
	if err := a.save(); err != nil {
		return nil, err
	}
	return map[string]any{
		"readiness": store.Removed, "canonical": canonical,
		"signature_hex": hex.EncodeToString(sig.GetSignature()), "chain_end": false,
	}, nil
}

// Rotate creates the next development key and signs the rotation with the current one.
// The old private key is not retained. A pending acceptance blocks rotation.
func (a *Agent) Rotate() (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, lease := range a.st.Leases {
		if lease.Readiness == store.AcceptancePending || lease.Readiness == store.Transferring {
			return nil, fail(409, "pending")
		}
	}
	next, err := keys.Generate()
	if err != nil {
		return nil, err
	}
	gen := a.st.Generation + 1
	newID := fmt.Sprintf("%s.r%d", a.cfg.KeyID, gen)
	if !msgkit.ValidID(newID) {
		return nil, fail(400, "key id")
	}
	sig, canonical, err := sign.Rotate(a.key.Private(), a.st.KeyID, a.cfg.ProviderID, newID, next.Public(), a.st.Generation)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(a.cfg.DataDir, "keys"), 0o700); err != nil {
		return nil, err
	}
	if err := keys.Write(store.KeyPath(a.cfg.DataDir, gen), next); err != nil {
		return nil, err
	}
	a.st.Previous = append(a.st.Previous, store.Public{KeyID: a.st.KeyID, Generation: a.st.Generation, PublicHex: a.st.PublicHex})
	a.st.Generation = gen
	a.st.KeyID = newID
	a.st.PublicHex = hex.EncodeToString(next.Public())
	a.key = next
	a.st.Rotations = append(a.st.Rotations, hex.EncodeToString(sig.GetSignature()))
	if err := a.save(); err != nil {
		return nil, err
	}
	return map[string]any{
		"key_id": newID, "generation": gen, "public_key_hex": a.st.PublicHex,
		"canonical": canonical, "signature_hex": hex.EncodeToString(sig.GetSignature()),
	}, nil
}
