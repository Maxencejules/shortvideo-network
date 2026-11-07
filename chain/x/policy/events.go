package policy

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// Untyped events of the council writer methods and of BeginBlock (interim
// choice DR-10-22). They are not in the protocol event list
// (docs/protocol.md); decision request DR-10-40 records them as event-list
// additions, and an indexer must not consume them until that is decided.
// The writers run on the executing proposal's nested cache, whose events
// reach the block only when the whole message succeeds.
const (
	// EventPolicyPending: a governed change was scheduled, or an immediate
	// change deleted a pending one. activation_height is 0 for an immediate
	// change; superseded_proposal_id is empty when nothing was replaced.
	EventPolicyPending = "policy_pending"
	// EventPolicyActivated: BeginBlock applied a pending governed change at
	// its activation height.
	EventPolicyActivated = "policy_activated"
	// EventUpgradePlan: an upgrade plan was scheduled, replaced, or
	// cancelled.
	EventUpgradePlan = "upgrade_plan"

	AttrParameterKey         = "parameter_key"
	AttrValue                = "value"
	AttrActivationHeight     = "activation_height"
	AttrProposalID           = "proposal_id"
	AttrSupersededProposalID = "superseded_proposal_id"
	AttrName                 = "name"
	AttrHeight               = "height"
	AttrReleaseDigest        = "release_digest"
	AttrAction               = "action"
	AttrReplacedName         = "replaced_name"

	PlanActionScheduled = "scheduled"
	PlanActionReplaced  = "replaced"
	PlanActionCancelled = "cancelled"
)

func emitPending(ctx sdk.Context, key string, value, activation uint64, proposalID, supersededID string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventPolicyPending,
		sdk.NewAttribute(AttrParameterKey, key),
		sdk.NewAttribute(AttrValue, msgkit.Dec(value)),
		sdk.NewAttribute(AttrActivationHeight, msgkit.Dec(activation)),
		sdk.NewAttribute(AttrProposalID, proposalID),
		sdk.NewAttribute(AttrSupersededProposalID, supersededID),
	))
}

// emitActivated emits policy_activated for one applied pending change.
func emitActivated(ctx sdk.Context, row keyedPending) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventPolicyActivated,
		sdk.NewAttribute(AttrParameterKey, row.Key),
		sdk.NewAttribute(AttrValue, msgkit.Dec(row.Value)),
		sdk.NewAttribute(AttrActivationHeight, msgkit.Dec(row.Activation)),
		sdk.NewAttribute(AttrProposalID, row.ProposalID),
	))
}

// emitPlan emits upgrade_plan for plan. replacedName is the name of the
// plan a "replaced" action overwrote, and empty for the other actions.
func emitPlan(ctx sdk.Context, plan Plan, action, replacedName string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventUpgradePlan,
		sdk.NewAttribute(AttrName, plan.Name),
		sdk.NewAttribute(AttrHeight, msgkit.Dec(plan.Height)),
		sdk.NewAttribute(AttrReleaseDigest, msgkit.Hex(plan.Digest)),
		sdk.NewAttribute(AttrProposalID, plan.ProposalID),
		sdk.NewAttribute(AttrAction, action),
		sdk.NewAttribute(AttrReplacedName, replacedName),
	))
}
