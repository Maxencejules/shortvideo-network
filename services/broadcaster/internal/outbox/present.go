package outbox

// Status is the caller-visible OperationStatus (frozen in
// svn.offchain.v1.OperationStatus; DR-11-09 adds no value).
type Status uint8

// Statuses.
const (
	StatusRequested Status = iota + 1
	StatusSubmitted
	StatusCommitted
	StatusFailed
	StatusPendingStall
)

var statusNames = []string{"", "OPERATION_STATUS_REQUESTED", "OPERATION_STATUS_SUBMITTED",
	"OPERATION_STATUS_COMMITTED", "OPERATION_STATUS_FAILED", "OPERATION_STATUS_PENDING_STALL"}

func (s Status) String() string { return enumName(statusNames, uint8(s), "Status") }

// Observer is the caller-visible state of the chain observer
// (detail.observer).
type Observer uint8

// Observer states.
const (
	ObserverFresh Observer = iota + 1
	ObserverUnavailable
)

var observerNames = []string{"", "FRESH", "OBSERVER_UNAVAILABLE"}

func (o Observer) String() string { return enumName(observerNames, uint8(o), "Observer") }

// Overlay holds the read-time facts of section 10.
type Overlay struct {
	// StallOpen: a stall episode is open.
	StallOpen bool
	// ObserverFresh: chain_health.seen_at is within the freshness window of
	// the database clock.
	ObserverFresh bool
}

// View is the caller-visible status of one intent (plan 5.8).
type View struct {
	Status Status
	// Phase is detail.phase; zero for a COMMITTED or FAILED intent.
	Phase Phase
	// Reason is the failure reason of a FAILED intent, or
	// EXPIRED_AWAITING_CHAIN for an AWAITING_DEADLINE intent.
	Reason Reason
	// Code is the chain code of a FAILED intent that has one.
	Code     Code
	Observer Observer
	// Stall: detail.stall is shown, because a stall episode is open and the
	// observer is fresh.
	Stall bool
}

// Present maps an intent and the read-time overlay to the caller-visible
// status (plan 9.1, section 10). REQUESTED shows REQUESTED and IN_FLIGHT
// shows SUBMITTED, with the phase as detail ("uncertain" is SUBMITTED with
// phase UNCERTAIN). A non-terminal intent shows PENDING_STALL only while a
// stall episode is open and the observer is fresh; with a stale observer it
// shows its underlying status and OBSERVER_UNAVAILABLE, never a stall claim.
// The overlay never makes an intent COMMITTED and never hides a COMMITTED or
// FAILED one.
func Present(in Intent, o Overlay) (View, error) {
	if err := in.validate(); err != nil {
		return View{}, err
	}
	v := View{Phase: in.Phase, Observer: ObserverUnavailable}
	if o.ObserverFresh {
		v.Observer = ObserverFresh
	}
	switch in.State {
	case StateCommitted:
		v.Status = StatusCommitted
		return v, nil
	case StateFailed:
		v.Status, v.Reason, v.Code = StatusFailed, in.Reason, in.Code
		return v, nil
	case StateRequested:
		v.Status = StatusRequested
	case StateInFlight:
		v.Status = StatusSubmitted
		if in.Phase == PhaseAwaitingDeadline {
			v.Reason = ReasonExpiredAwaitingChain
		}
	}
	if o.StallOpen && o.ObserverFresh {
		v.Status, v.Stall = StatusPendingStall, true
	}
	return v, nil
}
