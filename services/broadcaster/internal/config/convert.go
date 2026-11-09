package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config/secretfile"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/worker"
)

// The conversions below hand each package its part of the configuration
// as that package's own typed value. Each consumer validates its value
// again against its own bounds.

// maxPasswordBytes is the longest database password (store.ConnConfig).
const maxPasswordBytes = 256

// ConnConfig reads the database password file and returns the explicit
// connection settings: store.OpenDB turns them into a pq.Config field by
// field and opens it with pq.NewConnectorConfig, which reads no PG*
// variable and no password file of its own (plan 7.7).
func (c *Config) ConnConfig() (store.ConnConfig, error) {
	if c.Database == nil {
		return store.ConnConfig{}, fmt.Errorf("%w: database", errMissing)
	}
	d := c.Database
	raw, err := secretfile.Read(d.PasswordFile, maxPasswordBytes+1)
	if err != nil {
		return store.ConnConfig{}, fmt.Errorf("config: database password file: %w", err)
	}
	password := string(bytes.TrimSuffix(raw, []byte("\n")))
	if password == "" || len(password) > maxPasswordBytes || bytes.ContainsAny([]byte(password), "\x00\r\n") {
		return store.ConnConfig{}, errors.New("config: database password file must hold one line of 1 to 256 bytes")
	}
	return store.ConnConfig{
		SocketDir: d.SocketDir, Port: d.Port, Database: d.Name, User: d.Role, Password: password,
		ApplicationName: d.ApplicationName, ConnectTimeout: time.Duration(d.ConnectTimeoutSeconds) * time.Second,
	}, nil
}

// StoreLimits are the numbers the outbox store applies.
func (c *Config) StoreLimits() store.Limits {
	n := c.Numbers
	return store.Limits{
		TransitionsPerIntent:    int(n.TransitionsPerIntent),
		ObservationBytes:        int(n.ObservationBytes),
		AckLogBytes:             int(n.AckLogBytes),
		AttemptsPerIntent:       int(n.AttemptsPerIntent),
		AttemptsPerBusinessID:   int(n.AttemptsPerBusinessID),
		BusinessAttempts:        int(n.BusinessAttempts),
		AuthContextBytes:        int(n.AuthContextBytes),
		MinLifetime:             n.MinLifetime(),
		MaxLifetime:             n.MaxLifetime(),
		EnqueueLockTimeout:      n.EnqueueLockTimeout(),
		EnqueueStatementTimeout: n.EnqueueStatementTimeout(),
	}
}

// OutboxLimits are the numbers the state machine's guards use.
func (c *Config) OutboxLimits() outbox.Limits {
	n := c.Numbers
	return outbox.Limits{
		IndexingMargin:        uint64(n.IndexingMarginBlocks),
		AttemptsPerBusinessID: int(n.AttemptsPerBusinessID),
		BusinessAttempts:      int(n.BusinessAttempts),
		AttemptsPerIntent:     int(n.AttemptsPerIntent),
		Sdk4Retries:           int(n.Sdk4Retries),
	}
}

// PolicyCallers returns the caller principals as the policy package and
// the API see them.
func (c *Config) PolicyCallers() []policy.Caller {
	out := make([]policy.Caller, 0, len(c.Callers))
	for _, cl := range c.Callers {
		kinds := make([]policy.Kind, len(cl.Kinds))
		for i, k := range cl.Kinds {
			kinds[i] = policy.Kind(k)
		}
		out = append(out, policy.Caller{Principal: cl.Principal, Kinds: kinds, BoundProviderID: cl.BoundProviderID})
	}
	return out
}

// CallerRegistration is what migrate --callers registers for one caller
// principal (store.RegisterCaller): its open-intent cap and its SQL-path
// role, empty for an HTTP-only caller.
type CallerRegistration struct {
	Principal     string
	OpenIntentCap int
	SQLRole       string
}

// CallerRegistrations returns the registration of every configured
// caller, in the configuration's order.
func (c *Config) CallerRegistrations() []CallerRegistration {
	out := make([]CallerRegistration, 0, len(c.Callers))
	for _, cl := range c.Callers {
		out = append(out, CallerRegistration{Principal: cl.Principal, OpenIntentCap: int(c.callerCap(cl)), SQLRole: cl.SQLRole})
	}
	return out
}

// GasFor is the gas limit declared for a key-signed kind:
// declared_gas_by_kind when set, else numbers.declared_gas. The forward
// kind has none: the provider signed its own limit.
func (c *Config) GasFor(kind policy.Kind) (uint64, error) {
	spec, err := policy.SpecOf(kind)
	if err != nil {
		return 0, err
	}
	if spec.Class == "" {
		return 0, errors.New("config: no broadcaster key signs this kind")
	}
	if g, ok := c.DeclaredGasByKind[string(kind)]; ok {
		return uint64(g), nil
	}
	return uint64(c.Numbers.DeclaredGas), nil
}

// WorkerNumbers returns the PROPOSED numbers the submission engine uses
// (worker.Numbers), with the declared gas of every key-signed kind.
func (c *Config) WorkerNumbers() (worker.Numbers, error) {
	n := c.Numbers
	gas := map[policy.Kind]uint64{}
	for _, k := range policy.Kinds() {
		spec, err := policy.SpecOf(k)
		if err != nil {
			return worker.Numbers{}, err
		}
		if spec.Class == "" {
			continue
		}
		g, err := c.GasFor(k)
		if err != nil {
			return worker.Numbers{}, err
		}
		gas[k] = g
	}
	out := worker.Numbers{
		TimeoutOffset:         uint64(n.TimeoutOffsetBlocks),
		PollInterval:          n.PollInterval(),
		StallWindow:           n.StallWindow(),
		BackoffBase:           n.BackoffBase(),
		BackoffCap:            n.BackoffCap(),
		DeclaredGas:           gas,
		WithdrawMarginPercent: uint64(n.WithdrawSimulationMarginPercent),
		DeadlinePages:         int(n.DeadlinePageBudget),
		AuthContextBytes:      int(n.AuthContextBytes),
		MinLifetime:           n.MinLifetime(),
		MaxLifetime:           n.MaxLifetime(),
		RelayLocalExpiry:      n.RelayLocalExpiry(),
		ForwardWindow:         uint64(n.ForwardWindowBlocks),
	}
	if err := out.Validate(); err != nil {
		return worker.Numbers{}, err
	}
	return out, nil
}

// SignerConfig returns the signer process's configuration (Logger and
// BeforeReply stay unset: the binary sets them).
func (c *Config) SignerConfig() (signer.Config, error) {
	if err := c.RequireSigner(); err != nil {
		return signer.Config{}, err
	}
	keys := make([]signer.KeyConfig, len(c.Keys))
	for i, k := range c.Keys {
		keys[i] = signer.KeyConfig{Name: k.Name, Class: policy.Class(k.Class), Path: k.Path}
	}
	n := c.Numbers
	return signer.Config{
		ChainID:          c.ChainID,
		RPCURL:           c.RPCURL,
		RPCTimeout:       n.RPCTimeout(),
		PollInterval:     n.PollInterval(),
		FreshnessPolls:   int(n.ObserverFreshnessPolls),
		MaxTimeoutOffset: uint64(n.SignerMaxTimeoutOffsetBlocks),
		SocketPath:       c.Signer.SocketPath,
		Boundary:         c.Signer.Boundary(),
		ServeUID:         c.Signer.ServeUID,
		Keys:             keys,
		RegistryPath:     c.IntentKeyRegistry,
	}, nil
}

// APIConfig returns serve's API configuration.
func (c *Config) APIConfig() (api.Config, error) {
	if err := c.RequireServe(); err != nil {
		return api.Config{}, err
	}
	n := c.Numbers
	return api.Config{
		ChainID:             c.ChainID,
		ListenAddress:       c.Listen.Address,
		TLSCertFile:         c.Listen.TLSCertFile,
		TLSKeyFile:          c.Listen.TLSKeyFile,
		ClientCAFile:        c.Listen.ClientCAFile,
		Callers:             c.PolicyCallers(),
		MaxBodyBytes:        int(n.RequestBodyBytes),
		MaxAuthContextBytes: int(n.AuthContextBytes),
		PageSize:            int(n.ListPageSize),
		MinLifetime:         n.MinLifetime(),
		MaxLifetime:         n.MaxLifetime(),
		RelayLocalExpiry:    n.RelayLocalExpiry(),
		ForwardWindow:       uint64(n.ForwardWindowBlocks),
		DeadlinePages:       int(n.DeadlinePageBudget),
		ObserverFreshness:   n.ObserverFreshness(),
	}, nil
}

// StatusReader is the node's latest header (*chain.Client).
type StatusReader interface {
	Status(ctx context.Context) (chain.Status, error)
}

// ErrChainIDMismatch is a node or a database that serves another chain id
// than the configured one.
var ErrChainIDMismatch = errors.New("config: the node or the database serves another chain id")

// VerifyChainID is the start-up chain id check of plan section 11 and 7.6:
// the node's /status network must equal the configured chain id (the
// chain client refuses another one too; this check does not rely on it),
// and so must the database binding, read by checkDB (store.CheckChain over
// the process's connection). serve runs it before it serves, and the
// worker before each signing round.
func (c *Config) VerifyChainID(ctx context.Context, node StatusReader, checkDB func(ctx context.Context, chainID string) error) error {
	if node == nil || checkDB == nil {
		return errors.New("config: chain id check needs a node and a database check")
	}
	st, err := node.Status(ctx)
	if err != nil {
		return fmt.Errorf("config: read node status: %w", err)
	}
	if st.ChainID != c.ChainID {
		return fmt.Errorf("%w: node", ErrChainIDMismatch)
	}
	if err := checkDB(ctx, c.ChainID); err != nil {
		if errors.Is(err, store.ErrChainMismatch) {
			return fmt.Errorf("%w: database", ErrChainIDMismatch)
		}
		return fmt.Errorf("config: database chain binding: %w", err)
	}
	return nil
}
