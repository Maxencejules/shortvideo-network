// Package config loads the operator file. The file has no key bytes and no
// admin token. Hosting consent must be true or the agent does not start.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// Destination is one exact host, IP, and port the operator named.
// There is no CIDR and no switch that allows every private address.
type Destination struct {
	Host string `json:"host"`
	IP   string `json:"ip"`
	Port uint16 `json:"port"`
}

// File is the operator configuration.
type File struct {
	ProviderID              string        `json:"provider_id"`
	KeyID                   string        `json:"key_id"`
	MachineID               string        `json:"machine_id"`
	Listen                  string        `json:"listen"`
	DataDir                 string        `json:"data_dir"`
	HostingConsent          bool          `json:"hosting_consent"`
	DeclaredCapacityBytes   uint64        `json:"declared_capacity_bytes"`
	QuotaBytes              uint64        `json:"quota_bytes"`
	MinFreeBytes            uint64        `json:"min_free_bytes"`
	BandwidthBytesPerSecond uint64        `json:"bandwidth_bytes_per_second"`
	TransferBudgetSeconds   uint64        `json:"transfer_budget_seconds"`
	MaxPackageBytes         uint64        `json:"max_package_bytes"`
	MaxTermSeconds          int64         `json:"max_term_seconds"`
	ClockUncertaintySeconds int64         `json:"clock_uncertainty_seconds"`
	StallAfterSeconds       int64         `json:"stall_after_seconds"`
	ChainID                 string        `json:"chain_id"`
	ChainURL                string        `json:"chain_url"`
	SourceCA                string        `json:"source_ca"`
	ChainCA                 string        `json:"chain_ca"`
	Destinations            []Destination `json:"destinations"`
	ChainDestinations       []Destination `json:"chain_destinations"`
}

// Load reads path. Unknown fields are rejected.
func Load(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("config: %w", err)
	}
	if err := f.validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

func (f File) validate() error {
	if !f.HostingConsent {
		return fmt.Errorf("config: hosting_consent is false")
	}
	if !msgkit.ValidID(f.ProviderID) || !msgkit.ValidID(f.KeyID) || !msgkit.ValidID(f.MachineID) {
		return fmt.Errorf("config: provider_id, key_id, and machine_id must be ids")
	}
	host, port, err := net.SplitHostPort(f.Listen)
	if err != nil {
		return fmt.Errorf("config: listen: %w", err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() || port == "" {
		return fmt.Errorf("config: listen must be a loopback address")
	}
	if f.DataDir == "" || strings.Contains(f.DataDir, "..") {
		return fmt.Errorf("config: data_dir is empty or contains ..")
	}
	if f.DeclaredCapacityBytes == 0 || f.QuotaBytes == 0 || f.QuotaBytes > f.DeclaredCapacityBytes {
		return fmt.Errorf("config: quota must be within declared capacity")
	}
	if f.DeclaredCapacityBytes > genesis.MaxPackageBytes && f.QuotaBytes > genesis.MaxPackageBytes {
		return fmt.Errorf("config: quota above the protocol package cap")
	}
	if f.BandwidthBytesPerSecond == 0 || f.TransferBudgetSeconds == 0 || f.TransferBudgetSeconds > 3600 {
		return fmt.Errorf("config: bandwidth budget must be 1 to 3600 seconds")
	}
	if f.MaxPackageBytes == 0 || f.MaxPackageBytes > genesis.MaxPackageBytes {
		return fmt.Errorf("config: max_package_bytes must be 1 to %d", genesis.MaxPackageBytes)
	}
	if f.MaxTermSeconds <= 0 || f.ClockUncertaintySeconds < 0 || f.StallAfterSeconds <= 0 {
		return fmt.Errorf("config: term, uncertainty, or stall bound is unset")
	}
	if f.ChainID == "" || len(f.ChainID) > genesis.MaxChainIDLen {
		return fmt.Errorf("config: chain_id must be 1 to %d bytes", genesis.MaxChainIDLen)
	}
	if !strings.HasPrefix(f.ChainURL, "https://") {
		return fmt.Errorf("config: chain_url must be https")
	}
	if err := checkDest(f.Destinations); err != nil {
		return fmt.Errorf("config: destinations: %w", err)
	}
	if err := checkDest(f.ChainDestinations); err != nil {
		return fmt.Errorf("config: chain_destinations: %w", err)
	}
	return nil
}

func checkDest(list []Destination) error {
	if len(list) > 32 {
		return fmt.Errorf("more than 32 entries")
	}
	for _, d := range list {
		addr, err := netip.ParseAddr(d.IP)
		if err != nil || d.Port == 0 || d.Host == "" {
			return fmt.Errorf("entry needs host, ip, and port")
		}
		if !listable(addr) {
			return fmt.Errorf("entry %s is not an allowed private or global address", d.IP)
		}
		if d.Host != d.IP && !hostnameOK(d.Host) {
			return fmt.Errorf("host %q is not the ip or a dns name", d.Host)
		}
	}
	return nil
}

// listable reports whether an operator may name addr. Link-local,
// unspecified, and multicast addresses stay refused, including 169.254.0.0/16.
// Loopback and private addresses may be named one at a time.
func listable(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	v := policy.Classify(addr)
	if v.Global {
		return true
	}
	return addr.IsLoopback() || addr.IsPrivate()
}

func hostnameOK(host string) bool {
	if len(host) > 253 || strings.Contains(host, "..") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
	}
	return true
}
