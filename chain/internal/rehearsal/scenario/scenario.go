// Package scenario is the representative state of the goal-10 upgrade
// rehearsal (plan 12.3) as typed steps, the transitions that cross the
// upgrade, and the census. The app-level rehearsal and the devnet
// rehearsal run the same steps; each level builds, signs, and delivers the
// messages with its own helpers, maps the symbolic times and heights to
// real ones, and reads the census from its own queries. The package imports
// only msgkit, so chain/app tests import it without an import cycle.
//
// Order of the run: Steps (setup, every step at AtSetup or AtPlan block
// times), the plan at height h, blocks up to h-1, the upgrade at h, then
// Transitions, each asserted at effective-1 and at effective. No deadline,
// expiry, or activation of the scenario falls in [time(h-1), time(h+2)]:
// every block from the first AtPlan step to h+1 runs at AtPlan, h+2 runs at
// AtResume, and every crossing time is after AtResume.
package scenario

import (
	"fmt"
	"strings"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// Role is the account that signs a step.
type Role string

const (
	Authority   Role = "authority"
	Service     Role = "service"
	Provider1   Role = "provider-1"
	Provider2   Role = "provider-2"
	Broadcaster Role = "broadcaster"
	Auditor     Role = "auditor-1"
	Seat1       Role = "seat-1"
	Seat2       Role = "seat-2"
	Seat3       Role = "seat-3"
)

// Instant is a symbolic block time. A level maps each instant to unix
// seconds in this order (each strictly after the one before): AtSetup,
// EvidenceExpiresEarly, LeaseExpiresEarly, AtPlan, AtResume,
// EvidenceCrossExpiry, OfferCrossExpiry, LeaseCrossDeadline,
// ProposalCrossExpiry, HeldDeadline. A lease deadline is at most one DEC-005
// term after the time of its offer, and an evidence expiry is at most its
// lease's deadline.
type Instant int

const (
	// AtSetup is the block time of the setup steps.
	AtSetup Instant = iota + 1
	// EvidenceExpiresEarly is the expiry of the record that reads EXPIRED
	// before the plan.
	EvidenceExpiresEarly
	// LeaseExpiresEarly is the deadline of the lease that BeginBlock
	// expires before the plan.
	LeaseExpiresEarly
	// AtPlan is the block time of every block from the first council step
	// of the plan phase to h+1 (h+1 keeps the time before the halt).
	AtPlan
	// AtResume is the block time of h+2, the first block after the halt.
	AtResume
	// EvidenceCrossExpiry is the expiry of the record that reads RECORDED
	// at h-1 and EXPIRED after the upgrade.
	EvidenceCrossExpiry
	// OfferCrossExpiry is the expiry of the offer that is open at h-1 and
	// closed by BeginBlock after the upgrade.
	OfferCrossExpiry
	// LeaseCrossDeadline is the deadline of the lease that is held at h-1
	// and expired by BeginBlock after the upgrade.
	LeaseCrossDeadline
	// ProposalCrossExpiry is the expiry of the proposal that is OPEN at
	// h-1, takes an approval at ProposalCrossExpiry-1 after the upgrade,
	// and is EXPIRED by an execute at ProposalCrossExpiry.
	ProposalCrossExpiry
	// HeldDeadline is the deadline of the leases that stay held through
	// the run (the renewed lease's first deadline, and the offer that stays
	// open).
	HeldDeadline
)

// Instants are every instant in time order.
func Instants() []Instant {
	return []Instant{AtSetup, EvidenceExpiresEarly, LeaseExpiresEarly, AtPlan, AtResume,
		EvidenceCrossExpiry, OfferCrossExpiry, LeaseCrossDeadline, ProposalCrossExpiry, HeldDeadline}
}

// AfterPlan is a symbolic height: the plan height h plus the value.
type AfterPlan int64

const (
	// GovernedActivation is the activation height of the pending governed
	// change (h+3).
	GovernedActivation AfterPlan = 3
	// ValidatorActivation is the activation height of the pending
	// validator change (h+4).
	ValidatorActivation AfterPlan = 4
)

// Step is one scenario step. Each concrete type below is one message kind
// (or, for Council, the council message sequence of one proposal).
type Step interface {
	// Label names the step; levels derive business ids from it.
	Label() string
}

// SetTime sets the block time of the next blocks to At.
type SetTime struct {
	Name string
	At   Instant
}

// RegisterProvider registers ProviderID from Signer, which also holds the
// provider's signing key.
type RegisterProvider struct {
	Name       string
	Signer     Role
	ProviderID string
}

// DrainProvider is a provider-signed drain of ProviderID from Signer.
type DrainProvider struct {
	Name       string
	Signer     Role
	ProviderID string
}

// RegisterContent registers ContentID, version 1, from Service.
type RegisterContent struct {
	Name      string
	ContentID string
}

// Offer is an OfferLease from Service of ContentID (version 1) to
// ProviderID with a DEC-005 term ending at Deadline, open until Expiry.
type Offer struct {
	Name       string
	OfferID    string
	ContentID  string
	ProviderID string
	Deadline   Instant
	Expiry     Instant
}

// Accept is an AcceptLease of OfferID as LeaseID, signed by the offer's
// provider and relayed by Broadcaster.
type Accept struct {
	Name    string
	OfferID string
	LeaseID string
}

// Renew is a provider-signed renewal of LeaseID by one DEC-005 term (term
// version 2), relayed by Broadcaster.
type Renew struct {
	Name    string
	LeaseID string
}

// End is a provider-signed EndLease of LeaseID at term version 1, relayed
// by Broadcaster.
type End struct {
	Name    string
	LeaseID string
}

// Audit is a one-item batch from Auditor attesting LeaseID of ContentID:
// a RECORDED item when Matches (the observation is the lease's manifest
// digest), a REJECTED item otherwise. The item expires at Expiry.
type Audit struct {
	Name       string
	EvidenceID string
	LeaseID    string
	ContentID  string
	Matches    bool
	Expiry     Instant
}

// Council is the council message sequence of one proposal: a propose from
// Seat1, Approvals approvals from Seat1, Seat2, Seat3 in that order, and,
// when Execute, an execute from Authority. Every message must return
// msgkit.OK. A zero Expiry is the level's far expiry (after the run).
type Council struct {
	Name       string
	ProposalID string
	Change     Change
	Approvals  int
	Execute    bool
	Expiry     Instant
}

func (s SetTime) Label() string          { return s.Name }
func (s RegisterProvider) Label() string { return s.Name }
func (s DrainProvider) Label() string    { return s.Name }
func (s RegisterContent) Label() string  { return s.Name }
func (s Offer) Label() string            { return s.Name }
func (s Accept) Label() string           { return s.Name }
func (s Renew) Label() string            { return s.Name }
func (s End) Label() string              { return s.Name }
func (s Audit) Label() string            { return s.Name }
func (s Council) Label() string          { return s.Name }

// Change is the payload of a council proposal.
type Change interface {
	// ParameterKey is the proposal's parameter key.
	ParameterKey() string
}

// AdmitProvider admits ProviderID.
type AdmitProvider struct{ ProviderID string }

// GovernedLimit sets Key to Value: at once when Activation is 0, otherwise
// at height h+Activation.
type GovernedLimit struct {
	Key        string
	Value      uint64
	Activation AfterPlan
}

// ValidatorPower schedules NodeID at Power at height h+Activation (a new
// validator; the level chooses its consensus key).
type ValidatorPower struct {
	NodeID     string
	Power      int64
	Activation AfterPlan
}

// SoftwareUpgrade schedules upgrade Name at the plan height h. The level
// chooses h at least msgkit.MinActivationLeadBlocks after the execute
// height and the release digest.
type SoftwareUpgrade struct{ Name string }

func (c AdmitProvider) ParameterKey() string   { return "admit-" + c.ProviderID }
func (c GovernedLimit) ParameterKey() string   { return c.Key }
func (c ValidatorPower) ParameterKey() string  { return "validator-" + c.NodeID }
func (c SoftwareUpgrade) ParameterKey() string { return "upgrade" }

// Object ids of the scenario.
const (
	ProviderAdmitted = "p1"
	ProviderDraining = "p2"

	LeaseRenewed = "l-renewed" // held through the run (RENEWED)
	LeaseCross   = "l-cross"   // ACTIVE at h-1, EXPIRED at LeaseCrossDeadline
	LeaseExpired = "l-expired" // EXPIRED by BeginBlock before the plan
	LeaseEnded   = "l-ended"   // ENDED by its provider
	LeaseHeld    = "l-held"    // ACTIVE through the run, on the draining provider

	OfferOpen  = "o-open"  // open through the run (p1, c5)
	OfferCross = "o-cross" // open at h-1, closed at OfferCrossExpiry (p1, c6)

	EvidenceRecorded = "e-recorded" // RECORDED through the run
	EvidenceRejected = "e-rejected" // REJECTED
	EvidenceExpired  = "e-expired"  // EXPIRED at read time before the plan
	EvidenceCross    = "e-cross"    // RECORDED at h-1, EXPIRED at EvidenceCrossExpiry

	ProposalPlan      = "plan-rehearsal"  // the executed software_upgrade
	ProposalQueued    = "queued-limit"    // QUEUED at h-1; its execute after h is STALE_VERSION
	ProposalOpen      = "open-limit"      // OPEN at h-1 (two approvals), crosses ProposalCrossExpiry
	ProposalGoverned  = "governed-now"    // executed governed value
	ProposalPending   = "governed-later"  // executed scheduled governed change, activates at h+3
	ProposalValidator = "validator-later" // executed validator change, activates at h+4

	ValidatorNode = "v5"

	GovernedKey     = msgkit.LimitMaxOpenOffers
	GovernedValue   = uint64(7)
	PendingKey      = msgkit.LimitMaxPageLimit
	PendingValue    = uint64(40)
	QueuedKey       = msgkit.LimitMaxEvidenceBatch
	QueuedValue     = uint64(9)
	OpenKey         = msgkit.LimitMaxExpirySteps
	OpenValue       = uint64(60)
	ValidatorPowerN = int64(1)
)

// Steps are the scenario steps in delivery order. Every message returns
// msgkit.OK. The council proposals that must stay unexecuted (QUEUED and
// OPEN) come after the last execute before the upgrade, the plan's, so
// their base policy version is the one the upgrade bumps.
func Steps() []Step {
	return []Step{
		SetTime{"time-setup", AtSetup},
		RegisterProvider{"reg-p1", Provider1, ProviderAdmitted},
		Council{Name: "admit-p1", ProposalID: "admit-p1", Change: AdmitProvider{ProviderAdmitted}, Approvals: 3, Execute: true},
		RegisterProvider{"reg-p2", Provider2, ProviderDraining},
		Council{Name: "admit-p2", ProposalID: "admit-p2", Change: AdmitProvider{ProviderDraining}, Approvals: 3, Execute: true},
		RegisterContent{"reg-c1", "c1"},
		RegisterContent{"reg-c2", "c2"},
		RegisterContent{"reg-c3", "c3"},
		RegisterContent{"reg-c4", "c4"},
		RegisterContent{"reg-c5", "c5"},
		RegisterContent{"reg-c6", "c6"},
		Offer{"offer-renewed", "o-renewed", "c1", ProviderAdmitted, HeldDeadline, AtPlan},
		Accept{"accept-renewed", "o-renewed", LeaseRenewed},
		Renew{"renew-renewed", LeaseRenewed},
		Offer{"offer-cross", "o-lcross", "c2", ProviderAdmitted, LeaseCrossDeadline, AtPlan},
		Accept{"accept-cross", "o-lcross", LeaseCross},
		Offer{"offer-expired", "o-lexpired", "c3", ProviderAdmitted, LeaseExpiresEarly, EvidenceExpiresEarly},
		Accept{"accept-expired", "o-lexpired", LeaseExpired},
		Offer{"offer-ended", "o-lended", "c4", ProviderAdmitted, HeldDeadline, AtPlan},
		Accept{"accept-ended", "o-lended", LeaseEnded},
		End{"end-ended", LeaseEnded},
		Offer{"offer-held", "o-lheld", "c5", ProviderDraining, HeldDeadline, AtPlan},
		Accept{"accept-held", "o-lheld", LeaseHeld},
		Offer{"offer-open", OfferOpen, "c5", ProviderAdmitted, HeldDeadline, HeldDeadline},
		Offer{"offer-ocross", OfferCross, "c6", ProviderAdmitted, HeldDeadline, OfferCrossExpiry},
		Audit{"audit-recorded", EvidenceRecorded, LeaseRenewed, "c1", true, HeldDeadline},
		Audit{"audit-rejected", EvidenceRejected, LeaseRenewed, "c1", false, HeldDeadline},
		Audit{"audit-expired", EvidenceExpired, LeaseExpired, "c3", true, EvidenceExpiresEarly},
		Audit{"audit-cross", EvidenceCross, LeaseCross, "c2", true, EvidenceCrossExpiry},
		DrainProvider{"drain-p2", Provider2, ProviderDraining},
		SetTime{"time-plan", AtPlan},
		Council{Name: "governed-now", ProposalID: ProposalGoverned, Change: GovernedLimit{GovernedKey, GovernedValue, 0}, Approvals: 3, Execute: true},
		Council{Name: "governed-later", ProposalID: ProposalPending, Change: GovernedLimit{PendingKey, PendingValue, GovernedActivation}, Approvals: 3, Execute: true},
		Council{Name: "validator-later", ProposalID: ProposalValidator, Change: ValidatorPower{ValidatorNode, ValidatorPowerN, ValidatorActivation}, Approvals: 3, Execute: true},
		Council{Name: "plan", ProposalID: ProposalPlan, Change: SoftwareUpgrade{"rehearsal-1"}, Approvals: 3, Execute: true},
		Council{Name: "queued", ProposalID: ProposalQueued, Change: GovernedLimit{QueuedKey, QueuedValue, 0}, Approvals: 3},
		Council{Name: "open", ProposalID: ProposalOpen, Change: GovernedLimit{OpenKey, OpenValue, 0}, Approvals: 2, Expiry: ProposalCrossExpiry},
	}
}

// Class is one census class.
type Class string

const (
	ProvidersAdmitted       Class = "providers ADMITTED"
	ProvidersDraining       Class = "providers DRAINING"
	LeasesActive            Class = "leases ACTIVE"
	LeasesRenewed           Class = "leases RENEWED"
	LeasesExpired           Class = "leases EXPIRED"
	LeasesEnded             Class = "leases ENDED"
	OffersOpen              Class = "offers open"
	EvidenceRecordedClass   Class = "evidence RECORDED"
	EvidenceRejectedClass   Class = "evidence REJECTED"
	EvidenceExpiredClass    Class = "evidence EXPIRED (read time)"
	ProposalsOpen           Class = "proposals OPEN"
	ProposalsQueued         Class = "proposals QUEUED"
	ProposalsExecuted       Class = "proposals EXECUTED"
	PendingValidatorChanges Class = "pending validator changes"
	PendingLimitChanges     Class = "pending limit changes"
	GovernedValues          Class = "governed values"
)

// Classes are every census class in print order.
func Classes() []Class {
	return []Class{ProvidersAdmitted, ProvidersDraining, LeasesActive, LeasesRenewed, LeasesExpired, LeasesEnded,
		OffersOpen, EvidenceRecordedClass, EvidenceRejectedClass, EvidenceExpiredClass,
		ProposalsOpen, ProposalsQueued, ProposalsExecuted, PendingValidatorChanges, PendingLimitChanges, GovernedValues}
}

// Census is the number of scenario objects in each class. A level fills it
// from its own reads of committed state.
type Census map[Class]int

// Phase is a point of the run where the census is taken.
type Phase int

const (
	// BeforeUpgrade is height h-1 (the last block under the old binary).
	BeforeUpgrade Phase = iota + 1
	// AfterUpgrade is a height after the upgrade and before any crossing
	// time or activation (h+1 or h+2).
	AfterUpgrade
	// AfterTransitions is after every transition of Transitions.
	AfterTransitions
)

// Want is the census the scenario produces at phase. Every class is above
// 0 at BeforeUpgrade and AfterUpgrade, so a builder slip that drops an
// object cannot shrink coverage silently. The upgrade itself moves no
// object between classes; only the transitions after it do.
func Want(phase Phase) Census {
	c := Census{
		ProvidersAdmitted:       1,
		ProvidersDraining:       1,
		LeasesActive:            2, // l-cross, l-held
		LeasesRenewed:           1,
		LeasesExpired:           1,
		LeasesEnded:             1,
		OffersOpen:              2,
		EvidenceRecordedClass:   2, // e-recorded, e-cross
		EvidenceRejectedClass:   1,
		EvidenceExpiredClass:    1,
		ProposalsOpen:           1,
		ProposalsQueued:         1,
		ProposalsExecuted:       6, // admit-p1, admit-p2, governed-now, governed-later, validator-later, plan
		PendingValidatorChanges: 1,
		PendingLimitChanges:     1,
		GovernedValues:          1,
	}
	if phase == AfterTransitions {
		c[LeasesActive]--
		c[LeasesExpired]++
		c[OffersOpen]--
		c[EvidenceRecordedClass]--
		c[EvidenceExpiredClass]++
		c[ProposalsOpen]--           // approved at ProposalCrossExpiry-1, then expired
		c[PendingValidatorChanges]-- // applied at h+4
		c[PendingLimitChanges]--     // activated at h+3
		c[GovernedValues]++          // max_page_limit now has a governed value
	}
	return c
}

// Check returns an error naming every class whose count differs from
// want, and every class of want that is 0 at BeforeUpgrade or
// AfterUpgrade.
func (c Census) Check(want Census, phase Phase) error {
	var problems []string
	for _, class := range Classes() {
		if c[class] != want[class] {
			problems = append(problems, fmt.Sprintf("%s: %d, want %d", class, c[class], want[class]))
		}
		if phase != AfterTransitions && want[class] < 1 {
			problems = append(problems, fmt.Sprintf("%s: the scenario requires at least one", class))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("census: %s", strings.Join(problems, "; "))
	}
	return nil
}

// String prints one "class=count" field per class, in Classes order.
func (c Census) String() string {
	fields := make([]string, len(Classes()))
	for i, class := range Classes() {
		fields[i] = fmt.Sprintf("%s=%d", class, c[class])
	}
	return strings.Join(fields, " ")
}

// Transition is one scenario object that crosses the upgrade and changes
// under the new binary at Effective (a time) or at height h+At. A level
// asserts Before at effective-1 and After at effective.
type Transition struct {
	Object    string
	Class     Class
	Effective Instant
	At        AfterPlan
	Before    string
	After     string
}

// Transitions are the crossing objects, in the order their effective
// points come: the governed activation at h+3, the validator change at
// h+4, then the crossing times in Instants order. State names are the
// protocol enum names without their prefix, or a value for a governed
// key.
func Transitions() []Transition {
	return []Transition{
		{Object: PendingKey, Class: PendingLimitChanges, At: GovernedActivation, Before: "pending", After: "40"},
		{Object: ValidatorNode, Class: PendingValidatorChanges, At: ValidatorActivation, Before: "pending", After: "applied"},
		{Object: EvidenceCross, Class: EvidenceRecordedClass, Effective: EvidenceCrossExpiry, Before: "RECORDED", After: "EXPIRED"},
		{Object: OfferCross, Class: OffersOpen, Effective: OfferCrossExpiry, Before: "open", After: "closed"},
		{Object: LeaseCross, Class: LeasesActive, Effective: LeaseCrossDeadline, Before: "ACTIVE", After: "EXPIRED"},
		{Object: ProposalOpen, Class: ProposalsOpen, Effective: ProposalCrossExpiry, Before: "QUEUED", After: "EXPIRED"},
	}
}
