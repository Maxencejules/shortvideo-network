// Package config is the broadcaster's typed configuration (goal 11 plan
// section 11): one JSON file per process, decoded strictly, with no default
// for the chain id or the node RPC URL, hard bounds on every PROPOSED
// number, an explicit database connection built field by field, and a
// refusal of every PG* and PQGO_DEBUG environment variable.
//
// The file has these sections; which ones a process needs is checked by
// RequireServe, RequireSigner and RequireDatabase:
//
//	chain_id, rpc_url, allow_remote_rpc   the chain and its one trusted node
//	database                              the outbox database (Unix socket)
//	listen                                the mTLS loopback listener of serve
//	signer                                the signer socket and the two uids
//	keys                                  the signer's key files (signer only)
//	intent_key_registry                   the operator's public-key registry
//	callers                               caller principals, kinds and caps
//	numbers, declared_gas_by_kind         the PROPOSED numbers
//
// There is no hosting-intent key field anywhere (owner answer Q3): the
// broadcaster holds no intent key, and the strict decoder refuses an
// unknown field. Development principals and development keys refuse a
// chain id without the shortvideo-dev- prefix (docs/protocol.md:136).
package config

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config/strictjson"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// MaxFileBytes bounds a configuration file.
const MaxFileBytes = 256 << 10

// Bounds of fields that are not PROPOSED numbers.
const (
	// MaxSocketPath is the Unix socket path limit (sun_path, 108 bytes
	// with the terminating zero).
	MaxSocketPath = 107
	// MaxCallers bounds the configured caller principals.
	MaxCallers = 64
	// MinConnectTimeoutSeconds and MaxConnectTimeoutSeconds bound a
	// database connection attempt (store.ConnConfig's bounds).
	MinConnectTimeoutSeconds = 1
	MaxConnectTimeoutSeconds = 60
	// MaxCallerCap is the hard upper bound of one caller's open-intent
	// cap (the outbox's register_caller bound).
	MaxCallerCap = 4096
)

var (
	chainIDRE  = regexp.MustCompile(`^[!-~]{1,50}$`)
	pgNameRE   = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
	keyNameRE  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	appNameRE  = regexp.MustCompile(`^[!-~]{0,63}$`)
	errMissing = errors.New("config: required section missing")
)

// Config is one process's configuration as read from its file.
type Config struct {
	// ChainID is required and has no default. It must equal the node's
	// /status network at start (VerifyChainID) and the database binding.
	ChainID string `json:"chain_id"`
	// RPCURL is the CometBFT RPC base URL of the one node the broadcaster
	// trusts for reads. It is required and has no default: http on a
	// loopback IP literal, or https on another host only with
	// AllowRemoteRPC.
	RPCURL         string `json:"rpc_url"`
	AllowRemoteRPC bool   `json:"allow_remote_rpc"`

	Database *Database `json:"database"`
	Listen   *Listen   `json:"listen"`
	Signer   *Signer   `json:"signer"`
	Keys     []Key     `json:"keys"`
	// IntentKeyRegistry is the path of the operator's intent-key registry,
	// a file of public keys only (plan 7.4).
	IntentKeyRegistry string   `json:"intent_key_registry"`
	Callers           []Caller `json:"callers"`

	// Numbers are the PROPOSED numbers; a number the file leaves out keeps
	// its PROPOSED value.
	Numbers Numbers `json:"numbers"`
	// DeclaredGasByKind overrides numbers.declared_gas for a key-signed
	// kind.
	DeclaredGasByKind map[string]int64 `json:"declared_gas_by_kind"`
}

// Database is the outbox connection. Only a Unix socket is supported: the
// development cluster listens on a socket only, and the store builds its
// pq.Config for a socket with SSL off (internal/store ConnConfig).
type Database struct {
	SocketDir string `json:"socket_dir"`
	Port      uint16 `json:"port"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	// PasswordFile holds the role's password and nothing else (a trailing
	// newline is ignored). It must be owned by the reading uid with no
	// group or other permission bit (internal/config/secretfile).
	PasswordFile          string `json:"password_file"`
	ApplicationName       string `json:"application_name"`
	ConnectTimeoutSeconds int64  `json:"connect_timeout_seconds"`
}

// Listen is serve's mTLS listener (plan 5.6).
type Listen struct {
	// Address is a loopback IP literal and a port, 127.0.0.1:8443 or
	// [::1]:8443. A host name is refused: it could resolve elsewhere.
	Address string `json:"address"`
	// TLSCertFile and TLSKeyFile are the server certificate chain and its
	// key (PEM); ClientCAFile holds the CA certificates that sign caller
	// certificates.
	TLSCertFile  string `json:"tls_cert_file"`
	TLSKeyFile   string `json:"tls_key_file"`
	ClientCAFile string `json:"client_ca_file"`
}

// Signer is the signer process's socket and the two process uids (plan
// 4.1, owner answer Q1 "Both keys, separate signer (Recommended)"). Two
// distinct uids give the process boundary; one uid for both is the
// same-uid fallback, which separates key bytes in memory only. Neither uid
// may be root.
type Signer struct {
	SocketPath string `json:"socket_path"`
	ServeUID   uint32 `json:"serve_uid"`
	SignerUID  uint32 `json:"signer_uid"`
}

// Key is one key file the signer reads: a SERVICE key and a BROADCASTER
// key at most (Q1).
type Key struct {
	Name  string `json:"name"`
	Class string `json:"class"`
	Path  string `json:"path"`
}

// Caller is one caller principal (plan 5.7): the kinds it may send, its
// open-intent cap (numbers.open_intents_per_caller when left out), an
// optional provider binding for a provider-agent principal, and the
// optional PostgreSQL role of the caller service's SQL path (plan 5.6),
// which `svn-broadcaster migrate --callers` maps to the principal. The
// role must already exist; it is never the owner or the runtime role.
type Caller struct {
	Principal       string   `json:"principal"`
	Kinds           []string `json:"kinds"`
	OpenIntentCap   *int64   `json:"open_intent_cap"`
	BoundProviderID string   `json:"bound_provider_id"`
	SQLRole         string   `json:"sql_role"`
}

// Load reads, decodes and validates the configuration file at path. The
// file must be a regular file reached without a symlink, at most
// MaxFileBytes. It holds no secret: the database password is in its own
// file.
func Load(path string) (*Config, error) {
	raw, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse decodes and validates configuration file contents.
func Parse(raw []byte) (*Config, error) {
	if len(raw) > MaxFileBytes {
		return nil, fmt.Errorf("config: file above %d bytes", MaxFileBytes)
	}
	c := &Config{Numbers: ProposedNumbers()}
	if err := strictjson.Decode(raw, c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func readFile(path string) ([]byte, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("config: file path must be a clean absolute path")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("config: open: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("config: stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("config: file is not a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("config: read: %w", err)
	}
	return raw, nil
}

// Validate checks every section that is present. Which sections a process
// needs is checked by the Require methods.
func (c *Config) Validate() error {
	if !chainIDRE.MatchString(c.ChainID) {
		return errors.New("config: chain_id is required: 1 to 50 printable ASCII bytes")
	}
	if err := checkRPCURL(c.RPCURL, c.AllowRemoteRPC); err != nil {
		return err
	}
	if err := c.Numbers.Validate(); err != nil {
		return err
	}
	if c.Database != nil {
		if err := c.Database.validate(); err != nil {
			return err
		}
	}
	if c.Listen != nil {
		if err := c.Listen.validate(); err != nil {
			return err
		}
	}
	if c.Signer != nil {
		if err := c.Signer.validate(); err != nil {
			return err
		}
	}
	if c.IntentKeyRegistry != "" && !cleanAbs(c.IntentKeyRegistry) {
		return errors.New("config: intent_key_registry must be a clean absolute path")
	}
	if err := c.validateKeys(); err != nil {
		return err
	}
	if err := c.validateCallers(); err != nil {
		return err
	}
	return c.validateGas()
}

// devChain reports whether the chain id carries the development prefix.
func (c *Config) devChain() bool {
	return strings.HasPrefix(c.ChainID, genesis.DevnetChainIDPrefix)
}

func cleanAbs(p string) bool { return p != "" && filepath.IsAbs(p) && filepath.Clean(p) == p }

// checkRPCURL applies the rpc_url rule: an http or https base URL without
// userinfo, path, query or fragment and with an explicit port; http only on
// a loopback IP literal; any other host only over https with
// allow_remote_rpc.
func checkRPCURL(raw string, allowRemote bool) error {
	if raw == "" {
		return errors.New("config: rpc_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("config: rpc_url must be an http or https base URL without userinfo, path, query or fragment")
	}
	if port, err := strconv.Atoi(u.Port()); err != nil || port < 1 || port > 65535 {
		return errors.New("config: rpc_url needs an explicit port")
	}
	addr, err := netip.ParseAddr(u.Hostname())
	loopback := err == nil && addr.IsLoopback()
	switch {
	case loopback:
		return nil
	case u.Scheme != "https":
		return errors.New("config: rpc_url on a non-loopback host must use https")
	case !allowRemote:
		return errors.New("config: rpc_url on a non-loopback host needs allow_remote_rpc")
	}
	return nil
}

func (d *Database) validate() error {
	switch {
	case !cleanAbs(d.SocketDir):
		return errors.New("config: database.socket_dir must be a clean absolute path")
	case d.Port == 0:
		return errors.New("config: database.port is required")
	case !pgNameRE.MatchString(d.Name):
		return errors.New("config: database.name must match ^[a-z_][a-z0-9_]{0,62}$")
	case !pgNameRE.MatchString(d.Role):
		return errors.New("config: database.role must match ^[a-z_][a-z0-9_]{0,62}$")
	case !cleanAbs(d.PasswordFile):
		return errors.New("config: database.password_file must be a clean absolute path")
	case !appNameRE.MatchString(d.ApplicationName):
		return errors.New("config: database.application_name must be at most 63 printable ASCII bytes")
	case d.ConnectTimeoutSeconds < MinConnectTimeoutSeconds || d.ConnectTimeoutSeconds > MaxConnectTimeoutSeconds:
		return fmt.Errorf("config: database.connect_timeout_seconds outside [%d, %d]", MinConnectTimeoutSeconds, MaxConnectTimeoutSeconds)
	}
	return nil
}

// LoopbackAddress parses a listen address and accepts only a loopback IP
// literal with a port of 1 to 65535.
func LoopbackAddress(s string) (netip.AddrPort, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil || !ap.Addr().IsLoopback() || ap.Addr().Zone() != "" || ap.Port() == 0 {
		return netip.AddrPort{}, errors.New("config: listen address must be a loopback IP literal and a nonzero port")
	}
	return ap, nil
}

func (l *Listen) validate() error {
	if _, err := LoopbackAddress(l.Address); err != nil {
		return err
	}
	for name, p := range map[string]string{
		"listen.tls_cert_file": l.TLSCertFile, "listen.tls_key_file": l.TLSKeyFile, "listen.client_ca_file": l.ClientCAFile,
	} {
		if !cleanAbs(p) {
			return fmt.Errorf("config: %s must be a clean absolute path", name)
		}
	}
	return nil
}

func (s *Signer) validate() error {
	switch {
	case !cleanAbs(s.SocketPath) || len(s.SocketPath) > MaxSocketPath:
		return fmt.Errorf("config: signer.socket_path must be a clean absolute path of at most %d bytes", MaxSocketPath)
	case s.ServeUID == 0 || s.SignerUID == 0:
		return errors.New("config: signer.serve_uid and signer.signer_uid are required and may not be root")
	}
	return nil
}

// Boundary is the process boundary the two uids give.
func (s *Signer) Boundary() policy.Boundary {
	if s.ServeUID != s.SignerUID {
		return policy.DistinctUIDs
	}
	return policy.SameUID
}

func (c *Config) validateKeys() error {
	if len(c.Keys) == 0 {
		return nil
	}
	if !c.devChain() {
		return fmt.Errorf("config: development keys refuse a chain id without the %s prefix", genesis.DevnetChainIDPrefix)
	}
	if len(c.Keys) > 2 {
		return errors.New("config: at most two keys, one SERVICE and one BROADCASTER")
	}
	names, classes := map[string]bool{}, map[string]bool{}
	for i, k := range c.Keys {
		switch {
		case !keyNameRE.MatchString(k.Name):
			return fmt.Errorf("config: keys[%d].name must match ^[a-z][a-z0-9_-]{0,31}$", i)
		case k.Class != string(policy.ClassService) && k.Class != string(policy.ClassBroadcaster):
			return fmt.Errorf("config: keys[%d].class must be SERVICE or BROADCASTER", i)
		case !cleanAbs(k.Path):
			return fmt.Errorf("config: keys[%d].path must be a clean absolute path", i)
		case names[k.Name]:
			return fmt.Errorf("config: keys[%d].name is used twice", i)
		case classes[k.Class]:
			return fmt.Errorf("config: keys[%d].class is configured twice", i)
		}
		names[k.Name], classes[k.Class] = true, true
	}
	return nil
}

// callerCap returns a caller's open-intent cap.
func (c *Config) callerCap(cl Caller) int64 {
	if cl.OpenIntentCap != nil {
		return *cl.OpenIntentCap
	}
	return c.Numbers.OpenIntentsPerCaller
}

func (c *Config) validateCallers() error {
	if len(c.Callers) == 0 {
		return nil
	}
	if !c.devChain() {
		return fmt.Errorf("config: development principals refuse a chain id without the %s prefix", genesis.DevnetChainIDPrefix)
	}
	if len(c.Callers) > MaxCallers {
		return fmt.Errorf("config: more than %d callers", MaxCallers)
	}
	principals, roles := map[string]bool{}, map[string]bool{}
	var sum int64
	for i, cl := range c.Callers {
		if !msgkit.ValidID(cl.Principal) {
			return fmt.Errorf("config: callers[%d].principal is outside the id pattern", i)
		}
		if principals[cl.Principal] {
			return fmt.Errorf("config: callers[%d].principal is listed twice", i)
		}
		principals[cl.Principal] = true
		if len(cl.Kinds) == 0 || len(cl.Kinds) > len(policy.Kinds()) {
			return fmt.Errorf("config: callers[%d].kinds needs 1 to %d kinds", i, len(policy.Kinds()))
		}
		seen := map[string]bool{}
		for _, k := range cl.Kinds {
			if _, err := policy.SpecOf(policy.Kind(k)); err != nil {
				return fmt.Errorf("config: callers[%d].kinds names an unknown kind", i)
			}
			if seen[k] {
				return fmt.Errorf("config: callers[%d].kinds repeats a kind", i)
			}
			seen[k] = true
		}
		if cl.BoundProviderID != "" && !msgkit.ValidID(cl.BoundProviderID) {
			return fmt.Errorf("config: callers[%d].bound_provider_id is outside the id pattern", i)
		}
		if cl.SQLRole != "" {
			switch {
			case !pgNameRE.MatchString(cl.SQLRole):
				return fmt.Errorf("config: callers[%d].sql_role must match ^[a-z_][a-z0-9_]{0,62}$", i)
			case cl.SQLRole == store.OwnerRole || cl.SQLRole == store.RuntimeRole:
				return fmt.Errorf("config: callers[%d].sql_role may not be the owner or the runtime role", i)
			case roles[cl.SQLRole]:
				return fmt.Errorf("config: callers[%d].sql_role is used twice", i)
			}
			roles[cl.SQLRole] = true
		}
		capacity := c.callerCap(cl)
		if capacity < 1 || capacity > MaxCallerCap {
			return fmt.Errorf("config: callers[%d].open_intent_cap outside [1, %d]", i, MaxCallerCap)
		}
		sum += capacity
	}
	if sum > c.Numbers.CallerCapsSumMax {
		return fmt.Errorf("config: the callers' open-intent caps sum to %d, above caller_caps_sum_max %d", sum, c.Numbers.CallerCapsSumMax)
	}
	return nil
}

func (c *Config) validateGas() error {
	b := numberBounds["declared_gas"]
	for kind, gas := range c.DeclaredGasByKind {
		spec, err := policy.SpecOf(policy.Kind(kind))
		if err != nil {
			return errors.New("config: declared_gas_by_kind names an unknown kind")
		}
		if spec.Class == "" {
			return errors.New("config: declared_gas_by_kind names a kind no broadcaster key signs")
		}
		if gas < b.lo || gas > b.hi {
			return fmt.Errorf("config: declared_gas_by_kind outside the declared_gas hard bounds [%d, %d]", b.lo, b.hi)
		}
	}
	return nil
}

// RequireServe checks that the sections serve needs are present (database,
// listen, signer, intent_key_registry, at least one caller) and that the
// signer's key section is absent: serve never names a key file.
func (c *Config) RequireServe() error {
	switch {
	case c.Database == nil:
		return fmt.Errorf("%w: database", errMissing)
	case c.Listen == nil:
		return fmt.Errorf("%w: listen", errMissing)
	case c.Signer == nil:
		return fmt.Errorf("%w: signer", errMissing)
	case c.IntentKeyRegistry == "":
		return fmt.Errorf("%w: intent_key_registry", errMissing)
	case len(c.Callers) == 0:
		return fmt.Errorf("%w: callers", errMissing)
	case len(c.Keys) != 0:
		return errors.New("config: serve's configuration may not name key files")
	}
	return c.RequireRuntimeDatabase()
}

// RequireSigner checks that the sections the signer needs are present
// (signer, keys, intent_key_registry) and that the database, listener and
// callers are absent: the signer reaches neither the outbox nor callers.
func (c *Config) RequireSigner() error {
	switch {
	case c.Signer == nil:
		return fmt.Errorf("%w: signer", errMissing)
	case len(c.Keys) == 0:
		return fmt.Errorf("%w: keys", errMissing)
	case c.IntentKeyRegistry == "":
		return fmt.Errorf("%w: intent_key_registry", errMissing)
	case c.Database != nil || c.Listen != nil || len(c.Callers) != 0:
		return errors.New("config: the signer's configuration may not name the database, the listener or callers")
	case !strings.HasPrefix(c.RPCURL, "http://"):
		return errors.New("config: the signer reads its node over http on loopback only")
	}
	return nil
}

// RequireDatabase checks that the database section is present (status,
// which only reads, under either role).
func (c *Config) RequireDatabase() error {
	if c.Database == nil {
		return fmt.Errorf("%w: database", errMissing)
	}
	return nil
}

// RequireRuntimeDatabase checks that the database section names the
// runtime role (serve, reconcile-once, signer-clear): those processes
// write the outbox and never need the owner role's DDL.
func (c *Config) RequireRuntimeDatabase() error {
	if err := c.RequireDatabase(); err != nil {
		return err
	}
	if c.Database.Role != store.RuntimeRole {
		return fmt.Errorf("config: database.role must be the runtime role %s", store.RuntimeRole)
	}
	return nil
}

// RequireMigrate checks the configuration of migrate: the database section
// names the owner role, the only role that runs migrations and writes the
// chain binding and the callers, and no key file is named.
func (c *Config) RequireMigrate() error {
	if err := c.RequireDatabase(); err != nil {
		return err
	}
	switch {
	case c.Database.Role != store.OwnerRole:
		return fmt.Errorf("config: migrate needs database.role to be the owner role %s", store.OwnerRole)
	case len(c.Keys) != 0:
		return errors.New("config: migrate's configuration may not name key files")
	}
	return nil
}

// CheckProcessUID refuses to run a process under a uid the configuration
// does not name for it: serve under signer.serve_uid, the signer under
// signer.signer_uid. Neither runs as root.
func (c *Config) CheckProcessUID(signerProcess bool, euid uint32) error {
	if c.Signer == nil {
		return fmt.Errorf("%w: signer", errMissing)
	}
	want := c.Signer.ServeUID
	if signerProcess {
		want = c.Signer.SignerUID
	}
	if euid == 0 || euid != want {
		return errors.New("config: this process runs under another uid than the configuration names for it")
	}
	return nil
}
