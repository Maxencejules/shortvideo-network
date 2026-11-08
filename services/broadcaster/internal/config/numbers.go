package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// Numbers are the PROPOSED numbers of goal 11 plan section 11 (DR-11-10).
// Every value is new and none is an owner decision or a service level.
// Each field has hard lower and upper bounds in numberBounds; a value
// outside them refuses the configuration. A field the configuration file
// leaves out takes its PROPOSED value (ProposedNumbers), and Describe
// lists every value in use with that label.
type Numbers struct {
	// TimeoutOffsetBlocks is K: an attempt's timeout_height is the latest
	// height plus K.
	TimeoutOffsetBlocks int64 `json:"timeout_offset_blocks"`
	// SignerMaxTimeoutOffsetBlocks is K_max: the signer refuses a
	// timeout_height above its own fresh height plus K_max. K <= K_max.
	SignerMaxTimeoutOffsetBlocks int64 `json:"signer_max_timeout_offset_blocks"`
	// IndexingMarginBlocks is L of the dead rule (d3).
	IndexingMarginBlocks int64 `json:"indexing_margin_blocks"`
	// StallWindowSeconds is T: a stall episode opens when the height has
	// not advanced for T seconds of database clock.
	StallWindowSeconds int64 `json:"stall_window_seconds"`
	// ObserverFreshnessPolls: the observer is fresh while its last record
	// is within this many poll intervals of the database clock.
	ObserverFreshnessPolls int64 `json:"observer_freshness_polls"`
	// AttemptsPerBusinessID, BusinessAttempts and AttemptsPerIntent are
	// the attempt caps of plan 8.7.
	AttemptsPerBusinessID int64 `json:"attempts_per_business_id"`
	BusinessAttempts      int64 `json:"business_attempts"`
	AttemptsPerIntent     int64 `json:"attempts_per_intent"`
	// BackoffBaseMillis and BackoffCapSeconds bound the full-jitter retry
	// delay. Base <= cap.
	BackoffBaseMillis int64 `json:"backoff_base_ms"`
	BackoffCapSeconds int64 `json:"backoff_cap_seconds"`
	// IntentLifetimeMinSeconds and IntentLifetimeMaxSeconds bound a
	// request's expires_in. Min <= max.
	IntentLifetimeMinSeconds int64 `json:"intent_lifetime_min_seconds"`
	IntentLifetimeMaxSeconds int64 `json:"intent_lifetime_max_seconds"`
	// OpenIntentsPerCaller is the open-intent cap of a caller whose entry
	// names none.
	OpenIntentsPerCaller int64 `json:"open_intents_per_caller"`
	// CallerCapsSumMax bounds the sum of every caller's open-intent cap:
	// the global bound on open intents.
	CallerCapsSumMax int64 `json:"caller_caps_sum_max"`
	// RelayLocalExpirySeconds bounds the lifetime of a RENEW or END relay
	// (owner answer Q4): its stored provider signature never expires on
	// chain, so the request may not live longer than this.
	RelayLocalExpirySeconds int64 `json:"relay_local_expiry_seconds"`
	// ForwardWindowBlocks bounds how far above the observed height a
	// forwarded transaction's timeout_height may be.
	ForwardWindowBlocks int64 `json:"forward_window_blocks"`
	// DeclaredGas is the gas limit declared for every key-signed kind
	// (DR-11-27 fallback, orchestrator ruling R3).
	DeclaredGas int64 `json:"declared_gas"`
	// WithdrawSimulationMarginPercent is added to a simulated gas.
	WithdrawSimulationMarginPercent int64 `json:"withdraw_simulation_margin_percent"`
	// RPCTimeoutSeconds bounds each node RPC request.
	RPCTimeoutSeconds int64 `json:"rpc_timeout_seconds"`
	// PollIntervalMillis is the worker's and observer's poll interval.
	PollIntervalMillis int64 `json:"poll_interval_ms"`
	// ListPageSize is the largest page GET /v1/intents returns.
	ListPageSize int64 `json:"list_page_size"`
	// Sdk4Retries bounds the attempts that may die with a CheckTx sdk 4
	// refusal (DR-11-18).
	Sdk4Retries int64 `json:"sdk4_retries"`
	// RequestBodyBytes caps a POST body.
	RequestBodyBytes int64 `json:"request_body_bytes"`
	// AuthContextBytes caps a stored authorization context.
	AuthContextBytes int64 `json:"auth_context_bytes"`
	// DeadlinePageBudget bounds the Query/Deadlines pages read to find an
	// open offer (pages of 100).
	DeadlinePageBudget int64 `json:"deadline_page_budget"`
	// AckLogBytes clips a stored CheckTx log.
	AckLogBytes int64 `json:"ack_log_bytes"`
	// ObservationBytes caps a transitions observation.
	ObservationBytes int64 `json:"observation_bytes"`
	// TransitionsPerIntent: transitions rows per intent before only a
	// counter grows.
	TransitionsPerIntent int64 `json:"transitions_per_intent"`
	// EnqueueLockTimeoutMillis and EnqueueStatementTimeoutMillis bound the
	// HTTP-path enqueue transaction.
	EnqueueLockTimeoutMillis      int64 `json:"enqueue_lock_timeout_ms"`
	EnqueueStatementTimeoutMillis int64 `json:"enqueue_statement_timeout_ms"`
}

// bound is one number's hard bounds and PROPOSED value.
type bound struct {
	lo, hi, proposed int64
}

// numberBounds holds the hard bounds and the PROPOSED value of every
// Numbers field, keyed by its JSON name (plan section 11 table). Where
// another package enforces the same bound, its constant is used.
var numberBounds = map[string]bound{
	"timeout_offset_blocks":              {chain.MinTimeoutOffset, chain.MaxTimeoutOffset, 40},
	"signer_max_timeout_offset_blocks":   {chain.MinTimeoutOffset, chain.MaxTimeoutOffset, 1000},
	"indexing_margin_blocks":             {1, 20, 2},
	"stall_window_seconds":               {3, 600, 10},
	"observer_freshness_polls":           {2, 20, 4},
	"attempts_per_business_id":           {1, 16, 8},
	"business_attempts":                  {1, 8, 4},
	"attempts_per_intent":                {1, 32, 16},
	"backoff_base_ms":                    {100, 10_000, 1000},
	"backoff_cap_seconds":                {1, 600, 60},
	"intent_lifetime_min_seconds":        {5, 7 * 24 * 3600, 30},
	"intent_lifetime_max_seconds":        {5, 7 * 24 * 3600, 24 * 3600},
	"open_intents_per_caller":            {1, 4096, 256},
	"caller_caps_sum_max":                {1, 16_384, 4096},
	"relay_local_expiry_seconds":         {60, 86_400, 3600},
	"forward_window_blocks":              {policy.MinForwardWindow, policy.MaxForwardWindow, 100},
	"declared_gas":                       {int64(genesis.MinGasPerTx), int64(genesis.MaxGasPerTx), 2_000_000},
	"withdraw_simulation_margin_percent": {0, chain.MaxGasMarginPercent, 30},
	"rpc_timeout_seconds":                {1, 60, 10},
	"poll_interval_ms":                   {100, 10_000, 500},
	"list_page_size":                     {1, 100, 100},
	"sdk4_retries":                       {1, 10, 3},
	"request_body_bytes":                 {1024, int64(genesis.MaxTxBytes), 65_536},
	"auth_context_bytes":                 {256, 8192, 2048},
	"deadline_page_budget":               {policy.MinDeadlinePages, policy.MaxDeadlinePages, 10},
	"ack_log_bytes":                      {0, 4096, 512},
	"observation_bytes":                  {256, 8192, 2048},
	"transitions_per_intent":             {16, 4096, 256},
	"enqueue_lock_timeout_ms":            {100, 30_000, 2000},
	"enqueue_statement_timeout_ms":       {1000, 60_000, 5000},
}

// numberField is one Numbers field: its JSON name and a pointer to its
// value.
type numberField struct {
	name string
	v    *int64
}

// numberFields returns every Numbers field in declaration order.
func (n *Numbers) numberFields() []numberField {
	rv := reflect.ValueOf(n).Elem()
	rt := rv.Type()
	out := make([]numberField, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		out = append(out, numberField{name, rv.Field(i).Addr().Interface().(*int64)})
	}
	return out
}

// ProposedNumbers returns every number at its PROPOSED value.
func ProposedNumbers() Numbers {
	var n Numbers
	for _, f := range n.numberFields() {
		*f.v = numberBounds[f.name].proposed
	}
	return n
}

// Validate checks every number against its hard bounds, then the
// relations between numbers: K <= K_max, backoff base <= cap, lifetime min
// <= max, a relay local expiry no shorter than the shortest lifetime (else
// no RENEW or END relay could be accepted), and the default per-caller cap
// within the global bound.
func (n Numbers) Validate() error {
	if err := n.checkBounds(); err != nil {
		return err
	}
	switch {
	case n.TimeoutOffsetBlocks > n.SignerMaxTimeoutOffsetBlocks:
		return fmt.Errorf("config: timeout_offset_blocks above signer_max_timeout_offset_blocks")
	case n.BackoffBaseMillis > n.BackoffCapSeconds*1000:
		return fmt.Errorf("config: backoff_base_ms above backoff_cap_seconds")
	case n.IntentLifetimeMinSeconds > n.IntentLifetimeMaxSeconds:
		return fmt.Errorf("config: intent_lifetime_min_seconds above intent_lifetime_max_seconds")
	case n.RelayLocalExpirySeconds < n.IntentLifetimeMinSeconds:
		return fmt.Errorf("config: relay_local_expiry_seconds below intent_lifetime_min_seconds, so no relay could be accepted")
	case n.OpenIntentsPerCaller > n.CallerCapsSumMax:
		return fmt.Errorf("config: open_intents_per_caller above caller_caps_sum_max")
	}
	return nil
}

// checkBounds checks every number against its hard bounds.
func (n Numbers) checkBounds() error {
	for _, f := range n.numberFields() {
		b, ok := numberBounds[f.name]
		if !ok {
			return fmt.Errorf("config: number %s has no hard bounds", f.name)
		}
		if *f.v < b.lo || *f.v > b.hi {
			return fmt.Errorf("config: number %s outside its hard bounds [%d, %d]", f.name, b.lo, b.hi)
		}
	}
	return nil
}

// Describe lists every number in use, sorted by name, each labelled
// PROPOSED, for the start-up log line (plan 13.4 F11: a run prints the
// values it used).
func (n Numbers) Describe() []string {
	var out []string
	for _, f := range n.numberFields() {
		out = append(out, fmt.Sprintf("%s=%d (PROPOSED)", f.name, *f.v))
	}
	sort.Strings(out)
	return out
}

// Duration helpers for the consumers of the numbers.

// PollInterval is the worker's and observer's poll interval.
func (n Numbers) PollInterval() time.Duration {
	return time.Duration(n.PollIntervalMillis) * time.Millisecond
}

// ObserverFreshness is the age at which the observer's last record is no
// longer fresh.
func (n Numbers) ObserverFreshness() time.Duration {
	return time.Duration(n.ObserverFreshnessPolls) * n.PollInterval()
}

// RPCTimeout bounds each node RPC request.
func (n Numbers) RPCTimeout() time.Duration { return time.Duration(n.RPCTimeoutSeconds) * time.Second }

// StallWindow is T.
func (n Numbers) StallWindow() time.Duration {
	return time.Duration(n.StallWindowSeconds) * time.Second
}

// BackoffBase and BackoffCap bound the retry delay.
func (n Numbers) BackoffBase() time.Duration {
	return time.Duration(n.BackoffBaseMillis) * time.Millisecond
}

// BackoffCap is the largest retry delay.
func (n Numbers) BackoffCap() time.Duration { return time.Duration(n.BackoffCapSeconds) * time.Second }

// MinLifetime and MaxLifetime bound expires_in.
func (n Numbers) MinLifetime() time.Duration {
	return time.Duration(n.IntentLifetimeMinSeconds) * time.Second
}

// MaxLifetime is the longest expires_in.
func (n Numbers) MaxLifetime() time.Duration {
	return time.Duration(n.IntentLifetimeMaxSeconds) * time.Second
}

// RelayLocalExpiry is the longest lifetime of a RENEW or END relay.
func (n Numbers) RelayLocalExpiry() time.Duration {
	return time.Duration(n.RelayLocalExpirySeconds) * time.Second
}

// EnqueueLockTimeout and EnqueueStatementTimeout bound the enqueue
// transaction.
func (n Numbers) EnqueueLockTimeout() time.Duration {
	return time.Duration(n.EnqueueLockTimeoutMillis) * time.Millisecond
}

// EnqueueStatementTimeout bounds one enqueue statement.
func (n Numbers) EnqueueStatementTimeout() time.Duration {
	return time.Duration(n.EnqueueStatementTimeoutMillis) * time.Millisecond
}
