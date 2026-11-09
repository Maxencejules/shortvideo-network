package outbox

import (
	"errors"
	"strconv"
)

// The enumerations below use the zero value for "not set". For State,
// Phase, AttemptPhase, DeadReason and SendOutcome, String returns the name
// that the CHECK constraints of the outbox schema list (internal/store
// migration 0001), and the Parse functions accept exactly those names.

// State is an intent's state (plan 9.1). The zero value is "no intent yet",
// the From state of T1.
type State uint8

// Intent states.
const (
	StateRequested State = iota + 1
	StateInFlight
	StateCommitted
	StateFailed
)

var stateNames = []string{"", "REQUESTED", "IN_FLIGHT", "COMMITTED", "FAILED"}

func (s State) String() string { return enumName(stateNames, uint8(s), "State") }

// Terminal reports whether s is COMMITTED or FAILED.
func (s State) Terminal() bool { return s == StateCommitted || s == StateFailed }

// ParseState parses a stored state name.
func ParseState(name string) (State, error) {
	v, err := enumParse(stateNames, name, "state")
	return State(v), err
}

// Phase is a non-terminal intent's phase (plan 9.1). REQUESTED intents are
// QUEUED or RETRY_WAIT; IN_FLIGHT intents are SIGNED, ACKNOWLEDGED,
// UNCERTAIN or AWAITING_DEADLINE. Terminal intents have no phase.
type Phase uint8

// Intent phases.
const (
	PhaseQueued Phase = iota + 1
	PhaseRetryWait
	PhaseSigned
	PhaseAcknowledged
	PhaseUncertain
	PhaseAwaitingDeadline
)

var phaseNames = []string{"", "QUEUED", "RETRY_WAIT", "SIGNED", "ACKNOWLEDGED", "UNCERTAIN", "AWAITING_DEADLINE"}

func (p Phase) String() string { return enumName(phaseNames, uint8(p), "Phase") }

// ParsePhase parses a stored intent phase name.
func ParsePhase(name string) (Phase, error) {
	v, err := enumParse(phaseNames, name, "phase")
	return Phase(v), err
}

// AttemptPhase is an attempt's phase (plan 9.2). SIGNED, UNCERTAIN and
// ACKNOWLEDGED are live and ordered by knowledge; the other five are
// terminal and never change.
type AttemptPhase uint8

// Attempt phases. The live phases are declared in knowledge order, so a
// larger value is more knowledge.
const (
	AttemptSigned AttemptPhase = iota + 1
	AttemptUncertain
	AttemptAcknowledged
	AttemptCommittedOK
	AttemptCommittedIdempotent
	AttemptExecutionFailed
	AttemptIncludedAnteFailed
	AttemptDead
)

var attemptPhaseNames = []string{"", "SIGNED", "UNCERTAIN", "ACKNOWLEDGED", "COMMITTED_OK",
	"COMMITTED_IDEMPOTENT", "EXECUTION_FAILED", "INCLUDED_ANTE_FAILED", "DEAD"}

func (p AttemptPhase) String() string { return enumName(attemptPhaseNames, uint8(p), "AttemptPhase") }

// Live reports whether an attempt in phase p may still commit.
func (p AttemptPhase) Live() bool {
	return p == AttemptSigned || p == AttemptUncertain || p == AttemptAcknowledged
}

// ParseAttemptPhase parses a stored attempt phase name.
func ParseAttemptPhase(name string) (AttemptPhase, error) {
	v, err := enumParse(attemptPhaseNames, name, "attempt phase")
	return AttemptPhase(v), err
}

// DeadReason says which clause of the dead rule (plan 8.5) holds for a DEAD
// attempt.
type DeadReason uint8

// Dead reasons.
const (
	// DeadRefused is (d2): every started send was recorded as a counted
	// refusal.
	DeadRefused DeadReason = iota + 1
	// DeadDeadlinePassed is (d3) by the account sequence: the height passed
	// timeout_height plus the indexing margin and the account sequence,
	// read at that height or later, is at most the attempt's sequence.
	DeadDeadlinePassed
	// DeadSequenceUsed is (d3) by the acc_seq search: another transaction
	// used the attempt's sequence.
	DeadSequenceUsed
)

var deadReasonNames = []string{"", "D2_REFUSED", "D3_DEADLINE_PASSED", "D3_SEQUENCE_USED"}

func (r DeadReason) String() string { return enumName(deadReasonNames, uint8(r), "DeadReason") }

// ParseDeadReason parses a stored dead reason.
func ParseDeadReason(name string) (DeadReason, error) {
	v, err := enumParse(deadReasonNames, name, "dead reason")
	return DeadReason(v), err
}

// Reason is the reason of a FAILED intent (plan 9.1), plus
// ReasonExpiredAwaitingChain, which Present shows for an IN_FLIGHT intent
// whose expiry passed while its attempt may still commit.
// ReasonProviderMustResign is the reason of a forwarded provider-signed
// intent (plan 7.3 P3, owner answer Q2) whose bytes can no longer commit:
// the worker writes it instead of a retry, because only the provider can
// sign new bytes; the state machine never produces it.
type Reason uint8

// Reasons.
const (
	ReasonExecutionFailed Reason = iota + 1
	ReasonRejected
	ReasonExpiredNotSent
	ReasonExpiredDead
	ReasonAttemptsExhausted
	ReasonObjectTaken
	ReasonObjectStateChanged
	ReasonBusinessIDTaken
	ReasonUnauthorized
	ReasonInvalid
	ReasonProviderSignatureInvalid
	ReasonCancelled
	ReasonExpiredAwaitingChain
	ReasonProviderMustResign
)

var reasonNames = []string{"", "EXECUTION_FAILED", "REJECTED", "EXPIRED_NOT_SENT", "EXPIRED_DEAD",
	"ATTEMPTS_EXHAUSTED", "OBJECT_TAKEN", "OBJECT_STATE_CHANGED", "BUSINESS_ID_TAKEN", "UNAUTHORIZED",
	"INVALID", "PROVIDER_SIGNATURE_INVALID", "CANCELLED", "EXPIRED_AWAITING_CHAIN", "PROVIDER_MUST_RESIGN"}

func (r Reason) String() string { return enumName(reasonNames, uint8(r), "Reason") }

// failure reports whether r is a reason a FAILED intent can carry.
func (r Reason) failure() bool {
	return (r >= ReasonExecutionFailed && r <= ReasonCancelled) || r == ReasonProviderMustResign
}

// withCode reports whether a FAILED intent with reason r carries the chain
// code that caused it.
func (r Reason) withCode() bool { return r == ReasonExecutionFailed || r == ReasonRejected }

// ParseReason parses a stored failure reason. EXPIRED_AWAITING_CHAIN is a
// display reason and is never stored, so it is refused.
func ParseReason(name string) (Reason, error) {
	v, err := enumParse(reasonNames, name, "reason")
	if err == nil && !Reason(v).failure() {
		return 0, errors.New("outbox: reason " + strconv.Quote(name) + " is not a failure reason")
	}
	return Reason(v), err
}

// SendOutcome is the class of one started send's answer (plan 8.4).
type SendOutcome uint8

// Send outcomes.
const (
	// SendUnknown: transport error, timeout, reset, no confirmation, or a
	// started send whose outcome was never recorded. The bytes may be in a
	// mempool.
	SendUnknown SendOutcome = iota + 1
	// SendCache: JSON-RPC "tx already exists in cache": the node holds the
	// bytes or committed them.
	SendCache
	// SendNodeRefused: a JSON-RPC refusal before the mempool took the bytes.
	SendNodeRefused
	// SendMempoolRejected: CheckTx answered a non-zero code.
	SendMempoolRejected
	// SendAck: CheckTx answered code 0. It is never a commit.
	SendAck
)

var sendOutcomeNames = []string{"", "UNKNOWN", "CACHE", "NODE_REFUSED", "MEMPOOL_REJECTED", "ACK"}

func (o SendOutcome) String() string { return enumName(sendOutcomeNames, uint8(o), "SendOutcome") }

// ParseSendOutcome parses a stored send outcome.
func ParseSendOutcome(name string) (SendOutcome, error) {
	v, err := enumParse(sendOutcomeNames, name, "send outcome")
	return SendOutcome(v), err
}

// HaltReason says why a decision halts the attempt's signer. A halted
// signer signs nothing until an operator clears it (plan 8.1).
type HaltReason uint8

// Halt reasons.
const (
	// HaltSignerConflict: a transaction the broadcaster never stored used
	// the signer's key (DR-11-15).
	HaltSignerConflict HaltReason = iota + 1
	// HaltIntegrity: a chain answer contradicts the outbox (a committed
	// DEAD attempt, an acc_seq of another sequence, a hash mismatch).
	HaltIntegrity
	// HaltUnauthorized: CheckTx refused the signer with svn UNAUTHORIZED
	// (plan 8.7).
	HaltUnauthorized
)

var haltReasonNames = []string{"", "SIGNER_CONFLICT", "INTEGRITY", "UNAUTHORIZED"}

func (h HaltReason) String() string { return enumName(haltReasonNames, uint8(h), "HaltReason") }

// Alert names an observation an operator must see. An alert never changes
// a state by itself; the decision that carries it says what changes.
type Alert uint8

// Alerts.
const (
	// AlertHashMismatch: /tx returned bytes that do not hash to the
	// requested hash.
	AlertHashMismatch Alert = iota + 1
	// AlertAccSeqForeign: a committed result carries tx.acc_seq of another
	// account or sequence.
	AlertAccSeqForeign
	// AlertResultInconsistent: a committed result is neither a success with
	// exactly one of the typed event or the idempotent marker, nor a
	// failure without both.
	AlertResultInconsistent
	// AlertDeadAttemptCommitted: a stored attempt marked DEAD committed or
	// used its sequence (plan 8.5 integrity rule). Fatal.
	AlertDeadAttemptCommitted
	// AlertSignerConflict: a transaction the broadcaster never stored used
	// the signer's key or sequence.
	AlertSignerConflict
	// AlertSequenceAdvancedNoHit: the account sequence passed the attempt's
	// sequence and the acc_seq search found nothing; the attempt stays live.
	AlertSequenceAdvancedNoHit
	// AlertSearchInconsistent: the acc_seq search returned more than one
	// hash, or this attempt's hash while /tx does not find it.
	AlertSearchInconsistent
	// AlertFirstCommitNotFound: an idempotent replay committed and the
	// effect check has not found the first commitment yet.
	AlertFirstCommitNotFound
	// AlertEffectInconsistent: the effect check contradicts the attempt's
	// committed result (an idempotent replay whose effect is taken or
	// changed, a failed or dead attempt named as the commitment).
	AlertEffectInconsistent
	// AlertSdk4Exhausted: CheckTx refused attempts with sdk code 4 more
	// times than the retry bound allows (DR-11-18).
	AlertSdk4Exhausted
)

var alertNames = []string{"", "HASH_MISMATCH", "ACC_SEQ_FOREIGN", "RESULT_INCONSISTENT",
	"DEAD_ATTEMPT_COMMITTED", "SIGNER_CONFLICT", "SEQUENCE_ADVANCED_NO_HIT", "SEARCH_INCONSISTENT",
	"FIRST_COMMIT_NOT_FOUND", "EFFECT_INCONSISTENT", "SDK4_EXHAUSTED"}

func (a Alert) String() string { return enumName(alertNames, uint8(a), "Alert") }

// TransitionID names a row of the transition table (plan 9.3). TNone means
// no table row applies: a counter update, a hold, or an observation row.
type TransitionID uint8

// Transitions.
const (
	TNone TransitionID = iota
	T1
	T2
	T3
	T4
	T5
	T6
	T7
	T8
	T9
	T10
	T11
	T12
	T13
	T14
	T15
	T16
	T17
)

// NumTransitions is the number of table rows, T1 to T17.
const NumTransitions = 17

func (id TransitionID) String() string {
	if id == TNone {
		return "NONE"
	}
	if id <= T17 {
		return "T" + strconv.Itoa(int(id))
	}
	return "TransitionID(" + strconv.Itoa(int(id)) + ")"
}

// Code is a chain result code: an ABCI codespace and number. Number 0 is
// success.
type Code struct {
	Codespace string
	Number    uint32
}

// Codespaces of the codes the retry rules name.
const (
	CodespaceSDK = "sdk"
	CodespaceSVN = "svn"
)

// Codes the retry rules of plan 8.7 name. Their numbers are those of the
// pinned SDK (types/errors) and of svn.types.v1.ErrorCode; the outboxcheck
// test compares them with both.
var (
	CodeSDKUnauthorized  = Code{Codespace: CodespaceSDK, Number: 4}  // signature verification
	CodeSDKOutOfGas      = Code{Codespace: CodespaceSDK, Number: 11} // includes the block gas meter
	CodeSDKTimeoutHeight = Code{Codespace: CodespaceSDK, Number: 30}
	CodeSDKWrongSequence = Code{Codespace: CodespaceSDK, Number: 32}
	CodeSDKTimeout       = Code{Codespace: CodespaceSDK, Number: 42} // timeout_timestamp
	CodeSVNUnauthorized  = Code{Codespace: CodespaceSVN, Number: 1}
	CodeSVNReplay        = Code{Codespace: CodespaceSVN, Number: 3}
)

// MaxCodespaceLen bounds a codespace, as the outbox schema does.
const MaxCodespaceLen = 64

// OK reports whether c is success.
func (c Code) OK() bool { return c.Number == 0 }

func (c Code) String() string {
	return c.Codespace + "/" + strconv.FormatUint(uint64(c.Number), 10)
}

// validate checks the codespace bounds: at most MaxCodespaceLen printable
// ASCII bytes, and not empty for a failure code.
func (c Code) validate() error {
	if len(c.Codespace) > MaxCodespaceLen {
		return errors.New("outbox: codespace longer than 64 bytes")
	}
	for i := 0; i < len(c.Codespace); i++ {
		if b := c.Codespace[i]; b < '!' || b > '~' {
			return errors.New("outbox: codespace has a byte outside printable ASCII")
		}
	}
	if c.Number != 0 && c.Codespace == "" {
		return errors.New("outbox: failure code without a codespace")
	}
	return nil
}

func enumName(names []string, v uint8, typ string) string {
	if int(v) < len(names) && names[v] != "" {
		return names[v]
	}
	return typ + "(" + strconv.Itoa(int(v)) + ")"
}

// maxNameLen bounds a name that an error message quotes; every stored name
// is shorter (the schema allows 40 bytes).
const maxNameLen = 40

func enumParse(names []string, name, what string) (uint8, error) {
	for i, n := range names {
		if n != "" && n == name {
			return uint8(i), nil
		}
	}
	if len(name) > maxNameLen {
		return 0, errors.New("outbox: unknown " + what + " longer than 40 bytes")
	}
	return 0, errors.New("outbox: unknown " + what + " " + strconv.Quote(name))
}
