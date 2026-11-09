// Package config loads the indexer configuration. Unknown JSON fields are
// refused. There is no default chain id and no default gateway.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// File is the process configuration.
type File struct {
	ChainID  string   `json:"chain_id"`
	RPCURL   string   `json:"rpc_url"`
	Listen   string   `json:"listen"`
	Database Database `json:"database"`
	Numbers  Numbers  `json:"numbers"`
}

// Database is the Unix-socket connection.
type Database struct {
	SocketDir             string `json:"socket_dir"`
	Port                  uint16 `json:"port"`
	Name                  string `json:"name"`
	OwnerRole             string `json:"owner_role"`
	OwnerPasswordFile     string `json:"owner_password_file"`
	Role                  string `json:"role"`
	PasswordFile          string `json:"password_file"`
	ApplicationName       string `json:"application_name"`
	ConnectTimeoutSeconds int    `json:"connect_timeout_seconds"`
}

// Numbers are PROPOSED timings. They are not a service level.
type Numbers struct {
	StaleAfterSeconds int `json:"stale_after_seconds"`
	PollMS            int `json:"poll_ms"`
	ReconcileMS       int `json:"reconcile_ms"`
	RPCTimeoutSeconds int `json:"rpc_timeout_seconds"`
}

// ErrConfig is a refused configuration.
var ErrConfig = errors.New("config: refused")

// ErrEnvironment is a PG* variable or PQGO_DEBUG.
var ErrEnvironment = errors.New("config: environment")

// Load reads path and checks it.
func Load(path string) (File, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("%w: %v", ErrConfig, err)
	}
	if err := f.check(); err != nil {
		return File{}, err
	}
	return f, nil
}

func (f *File) check() error {
	if !strings.HasPrefix(f.ChainID, genesis.DevnetChainIDPrefix) || len(f.ChainID) > genesis.MaxChainIDLen {
		return fmt.Errorf("%w: chain id must start with %s", ErrConfig, genesis.DevnetChainIDPrefix)
	}
	u, err := url.Parse(f.RPCURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("%w: rpc_url", ErrConfig)
	}
	host := u.Hostname()
	if u.Scheme == "http" && host != "localhost" && !isLoopback(host) {
		return fmt.Errorf("%w: plain http rpc must be loopback", ErrConfig)
	}
	if f.Listen == "" {
		f.Listen = "127.0.0.1:0"
	}
	lnHost, _, err := net.SplitHostPort(f.Listen)
	if err != nil || !isLoopback(lnHost) {
		return fmt.Errorf("%w: listen must be a loopback host:port", ErrConfig)
	}
	if !filepath.IsAbs(f.Database.SocketDir) || f.Database.Port == 0 || f.Database.Name == "" {
		return fmt.Errorf("%w: database", ErrConfig)
	}
	if f.Database.OwnerRole == "" || f.Database.Role == "" || f.Database.OwnerPasswordFile == "" || f.Database.PasswordFile == "" {
		return fmt.Errorf("%w: database roles", ErrConfig)
	}
	if f.Database.ConnectTimeoutSeconds == 0 {
		f.Database.ConnectTimeoutSeconds = 5
	}
	if f.Database.ConnectTimeoutSeconds < 1 || f.Database.ConnectTimeoutSeconds > 60 {
		return fmt.Errorf("%w: connect timeout", ErrConfig)
	}
	if f.Database.ApplicationName == "" {
		f.Database.ApplicationName = "svn-indexer"
	}
	f.Numbers.applyDefaults()
	return f.Numbers.check()
}

func (n *Numbers) applyDefaults() {
	if n.StaleAfterSeconds == 0 {
		n.StaleAfterSeconds = 8
	}
	if n.PollMS == 0 {
		n.PollMS = 500
	}
	if n.ReconcileMS == 0 {
		n.ReconcileMS = 5000
	}
	if n.RPCTimeoutSeconds == 0 {
		n.RPCTimeoutSeconds = 10
	}
}

func (n Numbers) check() error {
	if n.StaleAfterSeconds < 3 || n.StaleAfterSeconds > 600 {
		return fmt.Errorf("%w: stale_after_seconds", ErrConfig)
	}
	if n.PollMS < 100 || n.PollMS > 10000 {
		return fmt.Errorf("%w: poll_ms", ErrConfig)
	}
	if n.ReconcileMS < 1000 || n.ReconcileMS > 3_600_000 {
		return fmt.Errorf("%w: reconcile_ms", ErrConfig)
	}
	if n.RPCTimeoutSeconds < 1 || n.RPCTimeoutSeconds > 60 {
		return fmt.Errorf("%w: rpc_timeout_seconds", ErrConfig)
	}
	return nil
}

// StaleAfter is the commit-age threshold. It is a PROPOSED number.
func (n Numbers) StaleAfter() time.Duration { return time.Duration(n.StaleAfterSeconds) * time.Second }

// Poll is the serve loop interval.
func (n Numbers) Poll() time.Duration { return time.Duration(n.PollMS) * time.Millisecond }

// ReconcileEvery is the reconciliation interval.
func (n Numbers) ReconcileEvery() time.Duration {
	return time.Duration(n.ReconcileMS) * time.Millisecond
}

// RPCTimeout bounds one gateway call.
func (n Numbers) RPCTimeout() time.Duration {
	return time.Duration(n.RPCTimeoutSeconds) * time.Second
}

// ObserverFresh is four poll intervals.
func (n Numbers) ObserverFresh() time.Duration { return 4 * n.Poll() }

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckEnvironment refuses PG* and PQGO_DEBUG. The error names the variables.
func CheckEnvironment(environ []string) error {
	var names []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "PG") || name == "PQGO_DEBUG" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("%w: unset %s", ErrEnvironment, strings.Join(names, ", "))
}

// Password reads a password file and trims one trailing newline.
func Password(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}
