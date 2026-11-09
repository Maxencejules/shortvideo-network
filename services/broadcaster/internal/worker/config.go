package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// RequestSignature asks the signer process for one outer transaction. In
// the service it is the signer client's request method, which runs the
// allowlist check before it asks and accepts only a transaction it rebuilds
// byte for byte from the request; a refusal is a *policy.Refusal.
type RequestSignature func(ctx context.Context, req signer.SignRequest) (signer.Signed, error)

// Numbers are the PROPOSED numbers of plan section 11 (DR-11-10) the worker
// uses. The configuration supplies them within their hard bounds; none is
// an owner decision.
type Numbers struct {
	// TimeoutOffset is K: a new attempt's timeout_height is the latest
	// committed height plus K (5 to 1,000 blocks).
	TimeoutOffset uint64
	// PollInterval paces every loop (100 ms to 10 s).
	PollInterval time.Duration
	// StallWindow is T: a stall episode opens when the height has not
	// advanced, or the node has not answered, for T of database clock (3 s
	// to 600 s).
	StallWindow time.Duration
	// BackoffBase and BackoffCap bound the full-jitter retry delay (0.1 s
	// to 10 s, 1 s to 600 s, base <= cap).
	BackoffBase time.Duration
	BackoffCap  time.Duration
	// DeclaredGas is the gas limit of each key-signed kind (1 to
	// 20,000,000). A withdrawal is sized by simulation and uses its entry
	// only when the node refuses to simulate it.
	DeclaredGas map[policy.Kind]uint64
	// WithdrawMarginPercent is added to a withdrawal's simulated gas (0 to
	// 100).
	WithdrawMarginPercent uint64
	// DeadlinePages bounds the pages of 100 read from Query/Deadlines to
	// find an open offer or a held lease, and from tx_search for the effect
	// check (1 to 50).
	DeadlinePages int
	// AuthContextBytes caps an authorization context (256 to 8,192).
	AuthContextBytes int
	// MinLifetime and MaxLifetime bound an intent's lifetime; a RENEW or
	// END relay lives at most RelayLocalExpiry (owner answer Q4).
	MinLifetime      time.Duration
	MaxLifetime      time.Duration
	RelayLocalExpiry time.Duration
	// ForwardWindow bounds a forwarded transaction's timeout_height above
	// the observed height (5 to 1,000 blocks).
	ForwardWindow uint64
}

// keySignedKinds are the kinds a broadcaster key signs; each needs a
// declared gas.
var keySignedKinds = []policy.Kind{policy.KindRegisterContent, policy.KindWithdrawContent, policy.KindOfferLease,
	policy.KindRelayAcceptLease, policy.KindRelayRenewLease, policy.KindRelayEndLease}

// Validate checks every number against its hard bound.
func (n Numbers) Validate() error {
	type span struct {
		name      string
		v, lo, hi int64
	}
	spans := []span{
		{"TimeoutOffset", int64(min(n.TimeoutOffset, 1<<62)), chain.MinTimeoutOffset, chain.MaxTimeoutOffset},
		{"PollInterval", int64(n.PollInterval), int64(100 * time.Millisecond), int64(10 * time.Second)},
		{"StallWindow", int64(n.StallWindow), int64(3 * time.Second), int64(600 * time.Second)},
		{"BackoffBase", int64(n.BackoffBase), int64(100 * time.Millisecond), int64(10 * time.Second)},
		{"BackoffCap", int64(n.BackoffCap), int64(time.Second), int64(600 * time.Second)},
		{"WithdrawMarginPercent", int64(min(n.WithdrawMarginPercent, 1<<62)), 0, chain.MaxGasMarginPercent},
		{"DeadlinePages", int64(n.DeadlinePages), policy.MinDeadlinePages, policy.MaxDeadlinePages},
		{"AuthContextBytes", int64(n.AuthContextBytes), 256, request.MaxAuthContextBytes},
		{"MinLifetime", int64(n.MinLifetime), int64(5 * time.Second), int64(7 * 24 * time.Hour)},
		{"MaxLifetime", int64(n.MaxLifetime), int64(5 * time.Second), int64(7 * 24 * time.Hour)},
		{"RelayLocalExpiry", int64(n.RelayLocalExpiry), int64(60 * time.Second), int64(24 * time.Hour)},
		{"ForwardWindow", int64(min(n.ForwardWindow, 1<<62)), policy.MinForwardWindow, policy.MaxForwardWindow},
	}
	for _, s := range spans {
		if s.v < s.lo || s.v > s.hi {
			return fmt.Errorf("worker: %s outside [%d, %d]", s.name, s.lo, s.hi)
		}
	}
	switch {
	case n.BackoffBase > n.BackoffCap:
		return errors.New("worker: BackoffBase above BackoffCap")
	case n.MinLifetime > n.MaxLifetime:
		return errors.New("worker: MinLifetime above MaxLifetime")
	}
	for _, k := range keySignedKinds {
		g, ok := n.DeclaredGas[k]
		if !ok || g < genesis.MinGasPerTx || g > genesis.MaxGasPerTx {
			return fmt.Errorf("worker: declared gas of %s missing or outside [%d, %d]", k, genesis.MinGasPerTx, genesis.MaxGasPerTx)
		}
	}
	if len(n.DeclaredGas) != len(keySignedKinds) {
		return errors.New("worker: declared gas for a kind no key signs")
	}
	return nil
}

// Config is everything the worker needs. Every field is required except
// Logger.
type Config struct {
	Store *store.Store
	// Chain reads the one configured node and builds and decodes
	// transactions for the configured chain id.
	Chain            *chain.Client
	RequestSignature RequestSignature
	// Keys are the signer process's keys (signer client Keys): one SERVICE
	// key and one BROADCASTER key.
	Keys []signer.KeyInfo
	// Registry is the operator's intent-key registry (public keys only).
	Registry *policy.Registry
	// Callers are the configured callers; an intent of a caller that is no
	// longer configured fails its validation (UNAUTHORIZED).
	Callers []policy.Caller
	Outbox  outbox.Limits
	Numbers Numbers
	// Logger receives one record per decision; nil discards. It must
	// redact (internal/logx): the worker logs ids, hashes and codes only.
	Logger *slog.Logger
}

// LockKeyOf is the advisory lock key the worker registers for a signer of
// class: one key per class, so the value is stable across restarts.
func LockKeyOf(class policy.Class) (int32, error) {
	switch class {
	case policy.ClassService:
		return 1, nil
	case policy.ClassBroadcaster:
		return 2, nil
	}
	return 0, fmt.Errorf("worker: no lock key for class %q", class)
}

// validate checks the configuration.
func (c Config) validate() error {
	switch {
	case c.Store == nil || c.Chain == nil || c.RequestSignature == nil || c.Registry == nil:
		return errors.New("worker: store, chain client, signature request and registry are required")
	case len(c.Keys) != 2:
		return errors.New("worker: exactly one SERVICE and one BROADCASTER key are required")
	}
	if err := c.Outbox.Validate(); err != nil {
		return err
	}
	if err := c.Numbers.Validate(); err != nil {
		return err
	}
	seen := map[policy.Class]bool{}
	for _, k := range c.Keys {
		if _, err := LockKeyOf(k.Class); err != nil {
			return err
		}
		if seen[k.Class] || k.PubKey == nil || len(k.Address) != genesis.AccountBytes ||
			!bytes.Equal(k.PubKey.Address(), k.Address) || k.Name == "" {
			return fmt.Errorf("worker: key of class %s is repeated or malformed", k.Class)
		}
		seen[k.Class] = true
	}
	principals := map[string]bool{}
	for _, cl := range c.Callers {
		if cl.Principal == "" || principals[cl.Principal] {
			return errors.New("worker: a caller principal is empty or repeated")
		}
		principals[cl.Principal] = true
	}
	return nil
}
