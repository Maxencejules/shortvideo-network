package authority

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// PolicyWriter is the policy store as an executed council proposal sees it.
// applyPayload is its only caller (TestPolicyWriterCallSites). Every method
// runs its checks before its first write, so a refusal writes nothing.
// FORBIDDEN would mark the proposal REJECTED; any other refusal leaves it
// QUEUED, as for a Registry hook. Keeper.transition binds it to x/policy
// (PolicyKeeper) on the nested cache of one message (boundPolicy).
type PolicyWriter interface {
	// SetLimit sets the governed limit key to value at once (activation
	// height 0) and deletes any pending change of key. A key outside the
	// governed_limit_caps table, or a value outside 1..ceiling, is BOUNDS.
	SetLimit(key string, value uint64, proposalID string) Outcome
	// SchedulePending stores a change of key to value that takes effect at
	// height activation, with the policy version the execute produced. It
	// replaces any pending change of key.
	SchedulePending(key string, value, activation uint64, proposalID string, policyVersion uint64) Outcome
	// ScheduleUpgrade stores the upgrade plan name at height with the
	// 32-byte release digest, replacing any pending plan. A name that an
	// earlier plan used with another digest is CONFLICT.
	ScheduleUpgrade(name string, height uint64, digest []byte, proposalID string) Outcome
	// CancelUpgrade deletes the pending plan name.
	CancelUpgrade(name string) Outcome
	// UpgradeApplied reports whether the upgrade name has applied.
	UpgradeApplied(name string) bool
}

// Accounts creates the auth account of an address that is about to sign:
// the account of a new council seat key (DR-10-35, goal 06 follow-up "Any
// future message path for RotateCouncilSeat must create the auth account
// for the new seat address"). applyPayload calls it for a council seat
// payload after every check of the replacement, on the same nested cache
// as the other hooks, so a refused or failed execute creates no account.
// An address that already has an account is a success.
type Accounts interface {
	EnsureAccount(addr []byte) Outcome
}

// PolicyKeeper is the part of x/policy this module uses: the writer methods
// an executed proposal calls, and the governed-value read of Query/
// Governance (max_page_limit). x/policy's Keeper implements it; authority
// never imports that package. Each writer method checks before its first
// write, emits its own events on ctx, and returns the protocol code.
type PolicyKeeper interface {
	SetLimit(ctx sdk.Context, key string, value uint64, proposalID string) msgkit.Code
	SchedulePending(ctx sdk.Context, key string, value, activation uint64, proposalID string, policyVersion uint64) msgkit.Code
	ScheduleUpgrade(ctx sdk.Context, name string, height uint64, digest []byte, proposalID string) msgkit.Code
	CancelUpgrade(ctx sdk.Context, name string) msgkit.Code
	UpgradeApplied(ctx sdk.Context, name string) bool
	GovernedLimit(ctx sdk.Context, key string) (uint64, bool)
}

// AccountKeeper creates an auth account for a 20-byte address when it has
// none, and returns the protocol code (OK when the account exists
// afterwards). The app implements it over the auth module's account keeper.
type AccountKeeper interface {
	EnsureAccount(ctx sdk.Context, addr []byte) msgkit.Code
}

// WithPolicy returns a copy of k that executes governed limit and upgrade
// payloads through p and reads the governed max_page_limit from it. A nil
// p is a wiring error and panics before the chain starts.
func (k Keeper) WithPolicy(p PolicyKeeper) Keeper {
	if p == nil {
		panic("authority: WithPolicy needs the policy keeper")
	}
	k.policy = p
	return k
}

// WithAccounts returns a copy of k that creates the account of a new council
// seat key through a. A nil a is a wiring error and panics before the chain
// starts.
func (k Keeper) WithAccounts(a AccountKeeper) Keeper {
	if a == nil {
		panic("authority: WithAccounts needs the account keeper")
	}
	k.accounts = a
	return k
}

// policyWriter binds the policy keeper to one transaction context. It
// returns nil when no policy keeper is wired, so the engine reports
// governed limit and upgrade payloads as MALFORMED.
func (k Keeper) policyWriter(ctx sdk.Context) PolicyWriter {
	if k.policy == nil {
		return nil
	}
	return boundPolicy{ctx: ctx, keeper: k.policy}
}

// accountCreator binds the account keeper to one transaction context. It
// returns nil when no account keeper is wired, so the engine reports a
// council seat payload as MALFORMED.
func (k Keeper) accountCreator(ctx sdk.Context) Accounts {
	if k.accounts == nil {
		return nil
	}
	return boundAccounts{ctx: ctx, keeper: k.accounts}
}

// boundPolicy is PolicyWriter over x/policy on one context.
type boundPolicy struct {
	ctx    sdk.Context
	keeper PolicyKeeper
}

func (p boundPolicy) SetLimit(key string, value uint64, proposalID string) Outcome {
	return fromCode(p.keeper.SetLimit(p.ctx, key, value, proposalID))
}

func (p boundPolicy) SchedulePending(key string, value, activation uint64, proposalID string, policyVersion uint64) Outcome {
	return fromCode(p.keeper.SchedulePending(p.ctx, key, value, activation, proposalID, policyVersion))
}

func (p boundPolicy) ScheduleUpgrade(name string, height uint64, digest []byte, proposalID string) Outcome {
	return fromCode(p.keeper.ScheduleUpgrade(p.ctx, name, height, digest, proposalID))
}

func (p boundPolicy) CancelUpgrade(name string) Outcome {
	return fromCode(p.keeper.CancelUpgrade(p.ctx, name))
}

func (p boundPolicy) UpgradeApplied(name string) bool {
	return p.keeper.UpgradeApplied(p.ctx, name)
}

// boundAccounts is Accounts over the account keeper on one context.
type boundAccounts struct {
	ctx    sdk.Context
	keeper AccountKeeper
}

func (a boundAccounts) EnsureAccount(addr []byte) Outcome {
	return fromCode(a.keeper.EnsureAccount(a.ctx, addr))
}

// governedInRange reports whether key is a governed limit key of the
// msgkit table (bounds.json governed_limit_caps) and value is in
// 1..its ceiling. ProposeChange (D-1) and applyPayload both apply it.
func governedInRange(key string, value uint64) bool {
	ceiling, known := msgkit.Ceiling(key)
	return known && value >= 1 && value <= ceiling
}

// heightLead checks a future height named by a payload against the block at
// height now, at propose and again at execute: ERROR_CODE_OUT_OF_RANGE when
// target is not at least minLead blocks above now, ERROR_CODE_BOUNDS when it
// is more than msgkit.MaxActivationLeadBlocks (PROPOSED) above now or above
// safe_integer_max. The empty string means the height is in the window.
// A governed limit activation passes minLead 1 (strictly above now); an
// upgrade plan height passes msgkit.MinActivationLeadBlocks (PROPOSED,
// owner option text Q3 (a)).
func heightLead(target uint64, now int64, minLead uint64) string {
	base := uint64(max(now, 0))
	if target <= base || target-base < minLead {
		return CodeOutOfRange
	}
	if target-base > msgkit.MaxActivationLeadBlocks || target > uint64(safeMax) {
		return CodeBounds
	}
	return ""
}

// leadCode is heightLead for the payload that names a future height: a
// governed limit with an activation height (minLead 1) and a software
// upgrade (minLead msgkit.MinActivationLeadBlocks). Every other payload, and
// a governed limit applied at execute (activation 0), passes.
func (d decodedWire) leadCode(now int64) string {
	switch {
	case d.Kind == kindGovernedLimit && d.Activation != 0:
		return heightLead(d.Activation, now, 1)
	case d.Kind == kindSoftwareUpgrade:
		return heightLead(d.UpgradeHeight, now, msgkit.MinActivationLeadBlocks)
	default:
		return ""
	}
}
