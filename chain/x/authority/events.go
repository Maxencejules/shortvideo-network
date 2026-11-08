package authority

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
)

// Events. A successful ExecuteChange of any payload kind emits two typed
// events (svn.events.v1, through msgkit.EmitTyped), in this order, after the
// events of a registry hook:
//
//	svn.events.v1.PolicyChanged{policy_version (the new version), parameter_key}
//	svn.events.v1.ProposalExecuted{proposal_id, policy_version (the new version)}
//
// PolicyChanged is the event spec op_execute emits (spec_machine.py:1524-1527).
// ProposalExecuted is D-2: the spec emits none, and docs/protocol.md lists it
// (goal 06 follow-up X5). A propose, an approve, an idempotent retry, a keep
// (EXPIRED or REJECTED) and a refusal emit no typed event. Keep, refusal and
// idempotent outcomes emit the untyped authority_result event (msg.go,
// DR-06-05).

// emitExecuted emits the typed events of one successful execute on ctx. A
// nil change (any other outcome) emits nothing.
func emitExecuted(ctx sdk.Context, done *executedChange) error {
	if done == nil {
		return nil
	}
	if err := msgkit.EmitTyped(ctx, &eventsv1.PolicyChanged{
		PolicyVersion: done.version,
		ParameterKey:  done.parameterKey,
	}); err != nil {
		return err
	}
	return msgkit.EmitTyped(ctx, &eventsv1.ProposalExecuted{
		ProposalId:    done.proposalID,
		PolicyVersion: done.version,
	})
}
