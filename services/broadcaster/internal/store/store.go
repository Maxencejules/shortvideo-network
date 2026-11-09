// Package store is the broadcaster's PostgreSQL outbox: the connection
// settings, the forward-only migration runner, and every query the service
// runs against schema broadcaster.
//
// Plan sections (goal 11 plan revision 2): 6.1-6.4 schema, roles and
// migrations; 6.3 enqueue in one transaction; 8.1 per-signer advisory-lock
// fencing; 8.3 persist, then broadcast, with sends counted before they
// happen; 9.3 a transitions row on every change and terminal rows that never
// change. The state machine's decisions (which transition follows which
// observation) belong to internal/outbox; this package stores them with
// compare-and-set updates and lets the database refuse rows that break the
// schema's invariants:
//
//   - a COMMITTED or FAILED intent and a terminal attempt never change;
//   - an IN_FLIGHT intent has exactly one live attempt and any other intent
//     has none (checked at commit);
//   - a COMMITTED intent has its commitment row (checked at commit);
//   - an attempt is dead by (d2) only with at least one started send, every
//     send recorded and every outcome a counted refusal;
//   - attempt counters only grow and an attempt's phase only gains
//     knowledge;
//   - transitions and commitments are append-only.
//
// Every statement is parameterized. Identifiers that cannot be parameters
// (GRANT targets) are quoted by the server with format(%I) inside the
// owner-only register_caller function. Error values of this package never
// include argument values; database errors are wrapped with their SQLSTATE
// and primary message.
//
// The owner's answers to goal 11 Q1-Q4 (2026-11-08T07:44Z, all four the
// recommended option) add, in migration 0002 and the matching functions:
//
//   - Q1: request kinds bound to a signer class, and key signers registered
//     with their class and key name (RegisterSigner);
//   - Q2: FORWARD_PROVIDER_TX, a provider-signed transaction stored and sent
//     verbatim, driven by the forwarder session (LockForwarder,
//     PersistForward);
//   - Q3: content owner rows, PENDING at enqueue, EFFECTIVE only after the
//     chain confirmed our registration, VOID when the register failed
//     (ConfirmContentOwner, ContentOwnerFor, CallerScheduledOffer);
//   - Q4: a FAILED intent none of whose attempts started a send loses its
//     request, request JSON, authorization context and attempt bytes in the
//     transaction that failed it; their digests stay.
//
// Reasons are checked for syntax only; the reason set is internal/outbox's.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
)

const (
	// OwnerRole owns schema broadcaster and runs migrations and the operator
	// writes (chain binding, callers).
	OwnerRole = "svn_broadcaster_owner"
	// RuntimeRole is the role of the running service: DML without DELETE on
	// the outbox tables, read-only on the owner-written tables, no DDL.
	RuntimeRole = "svn_broadcaster"
	// WorkChannel is the LISTEN/NOTIFY channel an enqueue notifies at commit.
	WorkChannel = "broadcaster_work"

	// lockClassMigration, lockClassSigner and lockClassForwarder are the
	// first keys of the two-key advisory locks of the migration runner
	// (second key 0), of key signer sessions (second key: the signer's lock
	// key) and of the one forwarder session (second key 0).
	// register_caller (0001_outbox.pgsql) takes lockClassMigration with
	// second key 1.
	lockClassMigration int32 = 0x53564e00
	lockClassSigner    int32 = 0x53564e01
	lockClassForwarder int32 = 0x53564e02

	// maxIntentBytes bounds request, request_json and tx bytes: the chain's
	// MaxTxBytes (chain/genesis/bounds.go).
	maxIntentBytes = 65536
	// maxGas is the chain's upper gas bound (chain/app/ante.go).
	maxGas = 20_000_000
	// maxEventsBytes bounds the commitment's typed events.
	maxEventsBytes = 65536
	// maxBackoff is the hard upper bound of a retry delay (section 11).
	maxBackoff = 600 * time.Second
	// maxListLimit bounds every list query.
	maxListLimit = 1000
)

// State is an intent state (plan 9.1).
type State string

// Intent states.
const (
	StateRequested State = "REQUESTED"
	StateInFlight  State = "IN_FLIGHT"
	StateCommitted State = "COMMITTED"
	StateFailed    State = "FAILED"
)

// Phase is the phase of a REQUESTED or IN_FLIGHT intent; terminal intents
// have no phase (PhaseNone).
type Phase string

// Intent phases.
const (
	PhaseNone             Phase = ""
	PhaseQueued           Phase = "QUEUED"
	PhaseRetryWait        Phase = "RETRY_WAIT"
	PhaseSigned           Phase = "SIGNED"
	PhaseAcknowledged     Phase = "ACKNOWLEDGED"
	PhaseUncertain        Phase = "UNCERTAIN"
	PhaseAwaitingDeadline Phase = "AWAITING_DEADLINE"
)

// AttemptPhase is an attempt phase (plan 9.2). The first three are live.
type AttemptPhase string

// Attempt phases.
const (
	AttemptSigned              AttemptPhase = "SIGNED"
	AttemptUncertain           AttemptPhase = "UNCERTAIN"
	AttemptAcknowledged        AttemptPhase = "ACKNOWLEDGED"
	AttemptCommittedOK         AttemptPhase = "COMMITTED_OK"
	AttemptCommittedIdempotent AttemptPhase = "COMMITTED_IDEMPOTENT"
	AttemptExecutionFailed     AttemptPhase = "EXECUTION_FAILED"
	AttemptIncludedAnteFailed  AttemptPhase = "INCLUDED_ANTE_FAILED"
	AttemptDead                AttemptPhase = "DEAD"
)

// Live reports whether the phase is a live (non-terminal) attempt phase.
func (p AttemptPhase) Live() bool {
	return p == AttemptSigned || p == AttemptUncertain || p == AttemptAcknowledged
}

// knowledge orders the live phases: SIGNED < UNCERTAIN < ACKNOWLEDGED.
func (p AttemptPhase) knowledge() int {
	switch p {
	case AttemptSigned:
		return 1
	case AttemptUncertain:
		return 2
	case AttemptAcknowledged:
		return 3
	}
	return 0
}

// Errors. Their text never includes an argument value.
var (
	ErrInvalid             = errors.New("store: invalid argument")
	ErrNotFound            = errors.New("store: not found")
	ErrStateChanged        = errors.New("store: row is not in the expected state")
	ErrImmutable           = errors.New("store: terminal row or immutable column")
	ErrAppendOnly          = errors.New("store: append-only table")
	ErrCallerUnknown       = errors.New("store: caller is not registered")
	ErrCapReached          = errors.New("store: open-intent cap reached")
	ErrIdempotencyConflict = errors.New("store: idempotency key in use with another request")
	ErrIsolation           = errors.New("store: transaction isolation is not READ COMMITTED")
	ErrEvidence            = errors.New("store: committed intent and commitment disagree")
	ErrLiveAttempt         = errors.New("store: intent state and live attempts disagree")
	ErrChainUnbound        = errors.New("store: no chain id is bound")
	ErrChainMismatch       = errors.New("store: database is bound to another chain id")
	ErrLockTimeout         = errors.New("store: lock wait timed out")
	ErrStatementTimeout    = errors.New("store: statement timed out")
	ErrSignerLockHeld      = errors.New("store: signer lock is held by another session")
	ErrFenceLost           = errors.New("store: signer session no longer holds its advisory lock")
	ErrSignerConfig        = errors.New("store: signer is registered with another lock key")
	ErrSignerBusy          = errors.New("store: signer has a live attempt")
	ErrStaleObservation    = errors.New("store: observation height below the stored height")
	ErrBusinessAttemptCap  = errors.New("store: business attempt cap reached")
	ErrStallOpen           = errors.New("store: a stall episode is already open")
	ErrTxOpen              = errors.New("store: a SignerTx is open on this signer session")
	ErrConflictCleared     = errors.New("store: an operator already cleared this signer event")
	ErrOpenIntentsBound    = errors.New("store: more open intents than one stall transition covers")
	ErrUnsentNotCleared    = errors.New("store: a FAILED intent that never started a send keeps its authorizations")
	ErrSignerClass         = errors.New("store: signer class does not fit the intent's kind")
	ErrForward             = errors.New("store: forwarded transaction and its intent or attempt disagree")
	ErrContentOwner        = errors.New("store: content owner row and its register intent disagree")
	// ErrContentUnavailable is the one answer for a content id that another
	// non-VOID owner row holds (plan 5.2 CONTENT_ID_UNAVAILABLE); it never
	// says whose.
	ErrContentUnavailable = errors.New("store: content id unavailable")
	// ErrTxTracked: another intent already tracks these forwarded bytes.
	ErrTxTracked = errors.New("store: transaction already tracked by another intent")
)

// sqlstateErrors maps the schema's SQLSTATEs (0001_outbox.pgsql and
// 0002_answers.pgsql headers) and two PostgreSQL ones to this package's
// errors.
var sqlstateErrors = map[string]error{
	"SV001": ErrImmutable,
	"SV002": ErrAppendOnly,
	"SV003": ErrCallerUnknown,
	"SV004": ErrInvalid,
	"SV005": ErrCapReached,
	"SV006": ErrIdempotencyConflict,
	"SV007": ErrIsolation,
	"SV008": ErrEvidence,
	"SV009": ErrLiveAttempt,
	"SV010": ErrChainUnbound,
	"SV011": ErrUnsentNotCleared,
	"SV012": ErrSignerClass,
	"SV013": ErrForward,
	"SV014": ErrContentOwner,
	"55P03": ErrLockTimeout,
	"57014": ErrStatementTimeout,
}

// mapErr wraps a database error with this package's error for its SQLSTATE.
// The wrapped *pq.Error formats as its primary message and code only.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var pe *pq.Error
	if errors.As(err, &pe) {
		if e, ok := sqlstateErrors[string(pe.Code)]; ok {
			return fmt.Errorf("%w: %w", e, err)
		}
	}
	return err
}

// SQLState returns the SQLSTATE of a database error, or "".
func SQLState(err error) string {
	var pe *pq.Error
	if errors.As(err, &pe) {
		return string(pe.Code)
	}
	return ""
}

var (
	idRE        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	codeRE      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,39}$`)
	printableRE = regexp.MustCompile(`^[!-~]{1,64}$`)
	codespaceRE = regexp.MustCompile(`^[!-~]{0,64}$`)
	chainIDRE   = regexp.MustCompile(`^[!-~]{1,50}$`)
	uuidRE      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	commandRE   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	roleRE      = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
)

// Limits are the PROPOSED numbers (plan section 11, DR-11-10) this package
// applies. Validate checks each against its hard bound; the values come from
// the configuration.
type Limits struct {
	// TransitionsPerIntent: transitions rows per intent before only the
	// suppressed counter grows. Hard bounds 16 to 4,096.
	TransitionsPerIntent int
	// ObservationBytes caps a transitions observation. 256 to 8,192.
	ObservationBytes int
	// AckLogBytes clips a stored CheckTx log. 0 to 4,096.
	AckLogBytes int
	// AttemptsPerIntent caps attempts of one intent. 1 to 32.
	AttemptsPerIntent int
	// AttemptsPerBusinessID caps attempts under one business id. 1 to 16.
	AttemptsPerBusinessID int
	// BusinessAttempts caps business ids of one intent. 1 to 8.
	BusinessAttempts int
	// AuthContextBytes caps the stored authorization context. 256 to 8,192.
	AuthContextBytes int
	// MinLifetime and MaxLifetime bound an intent's expires_in.
	// Hard bounds 5 s to 7 d, MinLifetime <= MaxLifetime.
	MinLifetime time.Duration
	MaxLifetime time.Duration
	// EnqueueLockTimeout and EnqueueStatementTimeout bound the HTTP-path
	// enqueue transaction. 0.1 s to 30 s and 1 s to 60 s.
	EnqueueLockTimeout      time.Duration
	EnqueueStatementTimeout time.Duration
}

// Validate checks every limit against its hard bound.
func (l Limits) Validate() error {
	type bound struct {
		name      string
		v, lo, hi int64
	}
	for _, b := range []bound{
		{"TransitionsPerIntent", int64(l.TransitionsPerIntent), 16, 4096},
		{"ObservationBytes", int64(l.ObservationBytes), 256, 8192},
		{"AckLogBytes", int64(l.AckLogBytes), 0, 4096},
		{"AttemptsPerIntent", int64(l.AttemptsPerIntent), 1, 32},
		{"AttemptsPerBusinessID", int64(l.AttemptsPerBusinessID), 1, 16},
		{"BusinessAttempts", int64(l.BusinessAttempts), 1, 8},
		{"AuthContextBytes", int64(l.AuthContextBytes), 256, 8192},
		{"MinLifetime", int64(l.MinLifetime), int64(5 * time.Second), int64(7 * 24 * time.Hour)},
		{"MaxLifetime", int64(l.MaxLifetime), int64(5 * time.Second), int64(7 * 24 * time.Hour)},
		{"EnqueueLockTimeout", int64(l.EnqueueLockTimeout), int64(100 * time.Millisecond), int64(30 * time.Second)},
		{"EnqueueStatementTimeout", int64(l.EnqueueStatementTimeout), int64(time.Second), int64(60 * time.Second)},
	} {
		if b.v < b.lo || b.v > b.hi {
			return fmt.Errorf("%w: %s outside [%d, %d]", ErrInvalid, b.name, b.lo, b.hi)
		}
	}
	if l.MinLifetime > l.MaxLifetime {
		return fmt.Errorf("%w: MinLifetime above MaxLifetime", ErrInvalid)
	}
	return nil
}

// Store runs the outbox queries on one connection pool.
type Store struct {
	db       *sql.DB
	lim      Limits
	instance string
}

// New returns a Store over db. instance identifies the writing process in
// attempts and transitions rows (a boot nonce plus pid; printable ASCII, at
// most 64 bytes).
func New(db *sql.DB, lim Limits, instance string) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil db", ErrInvalid)
	}
	if err := lim.Validate(); err != nil {
		return nil, err
	}
	if !printableRE.MatchString(instance) {
		return nil, fmt.Errorf("%w: instance", ErrInvalid)
	}
	return &Store{db: db, lim: lim, instance: instance}, nil
}

// clip returns s as valid UTF-8 without NUL bytes, cut at a rune boundary to
// at most n bytes.
func clip(s string, n int) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "�")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// toInt64 converts a chain uint64 to a bigint parameter.
func toInt64(name string, v uint64) (int64, error) {
	if v > 1<<63-1 {
		return 0, fmt.Errorf("%w: %s above the bigint range", ErrInvalid, name)
	}
	return int64(v), nil
}

// rollback ends tx after an error and joins a rollback failure to err.
func rollback(tx *sql.Tx, err error) error {
	if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
		return errors.Join(err, fmt.Errorf("store: rollback: %w", rbErr))
	}
	return err
}

// commit commits tx and maps a deferred-constraint failure.
func commit(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return mapErr(err)
	}
	return nil
}

// beginTx starts a READ COMMITTED transaction on the pool.
func (s *Store) beginTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("store: begin: %w", err)
	}
	return tx, nil
}
