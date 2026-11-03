// Package config parses the audit producer's operator configuration.
//
// The file is JSON and is parsed strictly: exact key names (no case
// folding), no unknown key, no duplicate key, no null, no trailing data, at
// most 1 MiB. No field can disable a check: there is no "skip fetch",
// "insecure", "allow private", proxy, redirect, resolver, or dial setting,
// and every value is data (TestConfigHasNoBypass). The allowlist holds only
// exact (provider_id, host, ip, port) entries, validated by package policy.
//
// Numbers marked PROPOSED below are interim defaults of this goal, not
// approved policy; the operator may set each within its bounds.
//
// Auditor fields (owner rulings on Q1, Q2, Q4):
//
//   - auditor_address: the 40-hex address of the auditor account. `run`
//     writes it into reports without opening the key; `sign` and `submit`
//     refuse a key whose address differs
//   - key_path: the ed25519 key file (package keys); only `sign`,
//     `submit`, and the rebuild inside `submit` open it
//   - key_id: the signer_key_id label of every envelope (id pattern)
//   - evidence_lifetime_seconds: at most 86400 (x/evidence
//     MaxEvidenceLifetimeSeconds); expiry = min(header time + lifetime,
//     lease deadline)
//   - epoch_guard_seconds: no signing this close to an epoch boundary
//   - inclusion_timeout_seconds: the wait for a committed result; a lease
//     whose deadline is within guard + inclusion of the header time is not
//     audited
//   - gas_limit, max_batch_items, http_status_retries, chain_rpc_timeout_ms
//
// The lifetime must exceed guard + inclusion.
package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/batch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/verify"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
)

// Bounds.
const (
	MaxFileBytes      = 1 << 20
	MaxAudits         = 256
	MaxEndpoints      = 8
	MaxPathBytes      = 4096
	MaxSampleCount    = verify.MaxRenditions * verify.MaxSegments
	maxEndpointName   = 64
	maxChainRPCBytes  = 2048
	maxEndpointIndex  = MaxEndpoints - 1
	millisecondsLimit = uint64(24 * time.Hour / time.Millisecond)
)

// limit is one time bound in milliseconds: PROPOSED default and bounds.
type limit struct {
	name                 string
	def, lowest, highest uint64
}

// PROPOSED defaults and their bounds, in milliseconds.
var (
	connectLimit        = limit{"connect_timeout_ms", 5000, 100, 60000}
	tlsLimit            = limit{"tls_handshake_timeout_ms", 5000, 100, 60000}
	responseHeaderLimit = limit{"response_header_timeout_ms", 10000, 100, 120000}
	idleReadLimit       = limit{"idle_read_timeout_ms", 10000, 100, 120000}
	objectLimit         = limit{"object_budget_ms", 300000, 1000, 3600000}
	runLimit            = limit{"run_budget_ms", 3600000, 1000, millisecondsLimit}
	rpcLimit            = limit{"chain_rpc_timeout_ms", 10000, 100, 60000}
)

// count is one integer setting: PROPOSED default and bounds.
type count struct {
	name                 string
	def, lowest, highest int64
}

// PROPOSED auditor defaults and their bounds.
var (
	lifetimeCount  = count{"evidence_lifetime_seconds", evidence.MaxEvidenceLifetimeSeconds, 60, evidence.MaxEvidenceLifetimeSeconds}
	guardCount     = count{"epoch_guard_seconds", 120, 30, evidence.EpochSeconds / 2}
	inclusionCount = count{"inclusion_timeout_seconds", 60, 5, 600}
	gasCount       = count{"gas_limit", 2_000_000, int64(genesis.MinGasPerTx), int64(genesis.MaxGasPerTx)}
	batchCount     = count{"max_batch_items", batch.MaxItems, 1, batch.MaxItems}
	retriesCount   = count{"http_status_retries", 3, 1, 5}
)

// Config is a validated configuration.
type Config struct {
	// ChainRPC is the operator's CometBFT RPC base URL. It is trusted
	// configuration, not a provider endpoint, so the destination policy
	// does not apply to it (it may be loopback).
	ChainRPC string
	// CAFile is the explicit test CA file; "" means the system roots.
	CAFile string
	// StoreRoot is the report store root.
	StoreRoot string
	// Limits are the per-request time bounds.
	Limits fetch.Limits
	// RunBudget bounds one producer run.
	RunBudget time.Duration
	// Allowlist is the validated allowlist.
	Allowlist []policy.Entry
	Audits    []Audit
	// AuditorAddress is the auditor account's 20-byte address.
	AuditorAddress []byte
	// KeyPath is the auditor key file; `run` never opens it.
	KeyPath string
	// KeyID is the signer_key_id label.
	KeyID string
	// Window holds the epoch guard, inclusion timeout, and evidence
	// lifetime, in seconds.
	Window batch.Window
	// GasLimit is the declared gas limit of each transaction.
	GasLimit uint64
	// MaxBatchItems bounds items per batch; the chain's governed limit
	// decides.
	MaxBatchItems int
	// StatusRetries is how many times an audit answered with a 4xx or 5xx
	// status other than 404 and 410 is run again before it is FAIL.
	StatusRetries int
	// RPCTimeout bounds each chain RPC request.
	RPCTimeout time.Duration
}

// Endpoint is one operator-configured provider endpoint.
type Endpoint struct {
	Name string
	URL  string
}

// Audit is one configured audit.
type Audit struct {
	LeaseID    string
	ProviderID string
	ContentID  string
	// EndpointIndex names the endpoint: an index into the capability
	// manifest's endpoint list when CapabilityManifestPath is set, else
	// into Endpoints.
	EndpointIndex          int
	ManifestPath           string
	CapabilityManifestPath string
	Endpoints              []Endpoint
	Mode                   verify.Mode
	SampleCount            int
}

// The JSON shape. Pointers mark optional or presence-checked fields.
type fileConfig struct {
	ChainRPC  string      `json:"chain_rpc"`
	CAFile    *string     `json:"ca_file"`
	StoreRoot string      `json:"store_root"`
	Limits    *fileLimits `json:"limits"`
	Allowlist []fileEntry `json:"allowlist"`
	Audits    []fileAudit `json:"audits"`

	AuditorAddress  string  `json:"auditor_address"`
	KeyPath         string  `json:"key_path"`
	KeyID           string  `json:"key_id"`
	Lifetime        *int64  `json:"evidence_lifetime_seconds"`
	Guard           *int64  `json:"epoch_guard_seconds"`
	Inclusion       *int64  `json:"inclusion_timeout_seconds"`
	GasLimit        *int64  `json:"gas_limit"`
	MaxBatchItems   *int64  `json:"max_batch_items"`
	StatusRetries   *int64  `json:"http_status_retries"`
	ChainRPCTimeout *uint64 `json:"chain_rpc_timeout_ms"`
}

type fileLimits struct {
	ConnectTimeoutMS        *uint64 `json:"connect_timeout_ms"`
	TLSHandshakeTimeoutMS   *uint64 `json:"tls_handshake_timeout_ms"`
	ResponseHeaderTimeoutMS *uint64 `json:"response_header_timeout_ms"`
	IdleReadTimeoutMS       *uint64 `json:"idle_read_timeout_ms"`
	ObjectBudgetMS          *uint64 `json:"object_budget_ms"`
	RunBudgetMS             *uint64 `json:"run_budget_ms"`
}

type fileEntry struct {
	ProviderID string `json:"provider_id"`
	Host       string `json:"host"`
	IP         string `json:"ip"`
	Port       uint16 `json:"port"`
}

type fileEndpoint struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type fileAudit struct {
	LeaseID                string         `json:"lease_id"`
	ProviderID             string         `json:"provider_id"`
	ContentID              string         `json:"content_id"`
	EndpointIndex          *int           `json:"endpoint_index"`
	ManifestPath           string         `json:"manifest_path"`
	CapabilityManifestPath *string        `json:"capability_manifest_path"`
	Endpoints              []fileEndpoint `json:"endpoints"`
	Mode                   string         `json:"mode"`
	SampleCount            *int           `json:"sample_count"`
}

// ErrInvalid wraps every configuration error.
var ErrInvalid = errors.New("config invalid")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Load reads and parses a configuration file: a regular file of at most
// MaxFileBytes.
func Load(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, invalid("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return Parse(raw)
}

// Parse parses and validates a configuration.
func Parse(raw []byte) (*Config, error) {
	if len(raw) > MaxFileBytes {
		return nil, invalid("file above %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(raw) {
		return nil, invalid("not UTF-8")
	}
	walker := json.NewDecoder(bytes.NewReader(raw))
	if err := strictWalk(walker, reflect.TypeOf(fileConfig{}), "config"); err != nil {
		return nil, err
	}
	if _, err := walker.Token(); !errors.Is(err, io.EOF) {
		return nil, invalid("trailing data")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var f fileConfig
	if err := decoder.Decode(&f); err != nil {
		return nil, invalid("%v", err)
	}
	return f.validate()
}

// jsonFields maps the exact JSON names of a struct type to field types.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" || !f.IsExported() {
			continue
		}
		fields[name] = f.Type
	}
	return fields
}

// strictWalk reads one JSON value and checks it against t: objects only
// where t is a struct, with exact known keys and no duplicate; arrays only
// where t is a slice; no null anywhere. Scalar types are checked by the
// decode that follows.
func strictWalk(dec *json.Decoder, t reflect.Type, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return invalid("%s: %v", path, err)
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch v := tok.(type) {
	case nil:
		return invalid("%s: null is not allowed", path)
	case json.Delim:
		switch {
		case v == '{' && t.Kind() == reflect.Struct:
			fields := jsonFields(t)
			seen := map[string]bool{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return invalid("%s: %v", path, err)
				}
				key, _ := keyTok.(string)
				ft, ok := fields[key]
				if !ok {
					return invalid("%s: unknown key %q", path, key)
				}
				if seen[key] {
					return invalid("%s: duplicate key %q", path, key)
				}
				seen[key] = true
				if err := strictWalk(dec, ft, path+"."+key); err != nil {
					return err
				}
			}
		case v == '[' && t.Kind() == reflect.Slice:
			for i := 0; dec.More(); i++ {
				if err := strictWalk(dec, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		default:
			return invalid("%s: unexpected %v", path, v)
		}
		if _, err := dec.Token(); err != nil {
			return invalid("%s: %v", path, err)
		}
	}
	return nil
}

var idPattern = regexp.MustCompile(genesis.IDPattern)

func checkPath(name, path string) error {
	if path == "" || len(path) > MaxPathBytes || strings.ContainsRune(path, 0) {
		return invalid("%s must be 1 to %d bytes without NUL", name, MaxPathBytes)
	}
	return nil
}

func (f fileConfig) validate() (*Config, error) {
	c := &Config{StoreRoot: f.StoreRoot}
	if err := checkChainRPC(f.ChainRPC); err != nil {
		return nil, err
	}
	c.ChainRPC = f.ChainRPC
	if err := checkPath("store_root", f.StoreRoot); err != nil {
		return nil, err
	}
	if f.CAFile != nil {
		if err := checkPath("ca_file", *f.CAFile); err != nil {
			return nil, err
		}
		c.CAFile = *f.CAFile
	}
	if err := f.limits(c); err != nil {
		return nil, err
	}
	if err := f.auditor(c); err != nil {
		return nil, err
	}
	for i, e := range f.Allowlist {
		ip, err := netip.ParseAddr(e.IP)
		if err != nil {
			return nil, invalid("allowlist[%d].ip is not an IP address", i)
		}
		c.Allowlist = append(c.Allowlist, policy.Entry{ProviderID: e.ProviderID, Host: e.Host, IP: ip, Port: e.Port})
	}
	if _, err := policy.New(c.Allowlist); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(f.Audits) < 1 || len(f.Audits) > MaxAudits {
		return nil, invalid("audits must hold 1 to %d items", MaxAudits)
	}
	leases := map[string]bool{}
	for i, a := range f.Audits {
		audit, err := a.validate(fmt.Sprintf("audits[%d]", i))
		if err != nil {
			return nil, err
		}
		if leases[audit.LeaseID] {
			return nil, invalid("audits[%d].lease_id is a duplicate", i)
		}
		leases[audit.LeaseID] = true
		c.Audits = append(c.Audits, audit)
	}
	return c, nil
}

var addressPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (c count) value(v *int64) (int64, error) {
	n := c.def
	if v != nil {
		n = *v
	}
	if n < c.lowest || n > c.highest {
		return 0, invalid("%s must be %d to %d", c.name, c.lowest, c.highest)
	}
	return n, nil
}

// auditor validates the auditor fields.
func (f fileConfig) auditor(c *Config) error {
	if !addressPattern.MatchString(f.AuditorAddress) {
		return invalid("auditor_address must be 40 lower-case hex characters")
	}
	c.AuditorAddress, _ = hex.DecodeString(f.AuditorAddress)
	if err := checkPath("key_path", f.KeyPath); err != nil {
		return err
	}
	c.KeyPath = f.KeyPath
	if !idPattern.MatchString(f.KeyID) {
		return invalid("key_id must match %s", genesis.IDPattern)
	}
	c.KeyID = f.KeyID
	values := []struct {
		c   count
		v   *int64
		out *int64
	}{
		{lifetimeCount, f.Lifetime, &c.Window.Lifetime},
		{guardCount, f.Guard, &c.Window.Guard},
		{inclusionCount, f.Inclusion, &c.Window.Inclusion},
	}
	for _, v := range values {
		n, err := v.c.value(v.v)
		if err != nil {
			return err
		}
		*v.out = n
	}
	if err := c.Window.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	gas, err := gasCount.value(f.GasLimit)
	if err != nil {
		return err
	}
	items, err := batchCount.value(f.MaxBatchItems)
	if err != nil {
		return err
	}
	retries, err := retriesCount.value(f.StatusRetries)
	if err != nil {
		return err
	}
	c.GasLimit, c.MaxBatchItems, c.StatusRetries = uint64(gas), int(items), int(retries)
	c.RPCTimeout, err = millis(rpcLimit, f.ChainRPCTimeout)
	return err
}

func checkChainRPC(raw string) error {
	if raw == "" || len(raw) > maxChainRPCBytes {
		return invalid("chain_rpc must be 1 to %d bytes", maxChainRPCBytes)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || (u.Path != "" && u.Path != "/") {
		return invalid("chain_rpc must be an http or https base URL without userinfo, path, query, or fragment")
	}
	return nil
}

func millis(l limit, value *uint64) (time.Duration, error) {
	ms := l.def
	if value != nil {
		ms = *value
	}
	if ms < l.lowest || ms > l.highest {
		return 0, invalid("limits.%s must be %d to %d", l.name, l.lowest, l.highest)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func (f fileConfig) limits(c *Config) error {
	var l fileLimits
	if f.Limits != nil {
		l = *f.Limits
	}
	targets := []struct {
		l   limit
		v   *uint64
		out *time.Duration
	}{
		{connectLimit, l.ConnectTimeoutMS, &c.Limits.Connect},
		{tlsLimit, l.TLSHandshakeTimeoutMS, &c.Limits.TLSHandshake},
		{responseHeaderLimit, l.ResponseHeaderTimeoutMS, &c.Limits.ResponseHeader},
		{idleReadLimit, l.IdleReadTimeoutMS, &c.Limits.IdleRead},
		{objectLimit, l.ObjectBudgetMS, &c.Limits.Object},
		{runLimit, l.RunBudgetMS, &c.RunBudget},
	}
	for _, target := range targets {
		d, err := millis(target.l, target.v)
		if err != nil {
			return err
		}
		*target.out = d
	}
	for _, phase := range []time.Duration{c.Limits.Connect, c.Limits.TLSHandshake, c.Limits.ResponseHeader, c.Limits.IdleRead} {
		if c.Limits.Object < phase {
			return invalid("limits.object_budget_ms must be at least every phase timeout")
		}
	}
	if c.RunBudget < c.Limits.Object {
		return invalid("limits.run_budget_ms must be at least object_budget_ms")
	}
	return nil
}

func (a fileAudit) validate(where string) (Audit, error) {
	out := Audit{LeaseID: a.LeaseID, ProviderID: a.ProviderID, ContentID: a.ContentID, ManifestPath: a.ManifestPath}
	for _, id := range []struct{ name, value string }{
		{"lease_id", a.LeaseID}, {"provider_id", a.ProviderID}, {"content_id", a.ContentID},
	} {
		if !idPattern.MatchString(id.value) {
			return Audit{}, invalid("%s.%s must match %s", where, id.name, genesis.IDPattern)
		}
	}
	if err := checkPath(where+".manifest_path", a.ManifestPath); err != nil {
		return Audit{}, err
	}
	if a.EndpointIndex == nil || *a.EndpointIndex < 0 || *a.EndpointIndex > maxEndpointIndex {
		return Audit{}, invalid("%s.endpoint_index must be 0 to %d", where, maxEndpointIndex)
	}
	out.EndpointIndex = *a.EndpointIndex
	switch {
	case a.CapabilityManifestPath != nil && a.Endpoints != nil:
		return Audit{}, invalid("%s: set capability_manifest_path or endpoints, not both", where)
	case a.CapabilityManifestPath != nil:
		if err := checkPath(where+".capability_manifest_path", *a.CapabilityManifestPath); err != nil {
			return Audit{}, err
		}
		out.CapabilityManifestPath = *a.CapabilityManifestPath
	default:
		if len(a.Endpoints) < 1 || len(a.Endpoints) > MaxEndpoints {
			return Audit{}, invalid("%s.endpoints must hold 1 to %d items", where, MaxEndpoints)
		}
		for j, e := range a.Endpoints {
			if n := utf8.RuneCountInString(e.Name); n < 1 || n > maxEndpointName {
				return Audit{}, invalid("%s.endpoints[%d].name must be 1 to %d characters", where, j, maxEndpointName)
			}
			if _, err := policy.ParseEndpoint(e.URL); err != nil {
				return Audit{}, fmt.Errorf("%w: %s.endpoints[%d].url: %w", ErrInvalid, where, j, err)
			}
			out.Endpoints = append(out.Endpoints, Endpoint{Name: e.Name, URL: e.URL})
		}
		if out.EndpointIndex >= len(out.Endpoints) {
			return Audit{}, invalid("%s.endpoint_index is past the endpoints list", where)
		}
	}
	switch verify.Mode(a.Mode) {
	case verify.WholePackage:
		if a.SampleCount != nil {
			return Audit{}, invalid("%s: a whole-package audit takes no sample_count", where)
		}
	case verify.SampledSegments:
		if a.SampleCount == nil || *a.SampleCount < 1 || *a.SampleCount > MaxSampleCount {
			return Audit{}, invalid("%s.sample_count must be 1 to %d", where, MaxSampleCount)
		}
		out.SampleCount = *a.SampleCount
	default:
		return Audit{}, invalid("%s.mode must be %s or %s", where, verify.WholePackage, verify.SampledSegments)
	}
	out.Mode = verify.Mode(a.Mode)
	return out, nil
}
