package policy

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
)

// Policy is svn.policy.v1.Query/Policy (deviation D-9: the spec machine has
// no query semantics). It reads committed state at the query height and
// never writes. The response is the protocol v0 PolicyState:
//
//   - policy_version: the council policy version (Deps.Versions)
//   - safety_caps: the stored caps (0x01)
//   - blocked_decisions: the authority's blocked decision rows
//     (Deps.Decisions), copied
//
// The v0 PolicyState has no field for governed values, pending changes, the
// upgrade plan, or the app version, so this query does not return them.
// The response is bounded: three fields, eight decision rows. A missing or
// damaged caps row is a plain error.
func (k Keeper) Policy(goCtx context.Context, _ *policyv1.QueryPolicyRequest) (*policyv1.QueryPolicyResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	caps, err := k.SafetyCaps(ctx)
	if err != nil {
		return nil, err
	}
	return &policyv1.QueryPolicyResponse{State: &policyv1.PolicyState{
		PolicyVersion:    k.deps.Versions.PolicyVersion(ctx),
		SafetyCaps:       caps,
		BlockedDecisions: cloneDecisions(k.deps.Decisions.BlockedDecisions(ctx)),
	}}, nil
}

// sdkContext unwraps the SDK context of a query handler. A plain context
// is MALFORMED.
func sdkContext(goCtx context.Context) (sdk.Context, error) {
	if ctx, ok := goCtx.(sdk.Context); ok {
		return ctx, nil
	}
	if ctx, ok := goCtx.Value(sdk.SdkContextKey).(sdk.Context); ok {
		return ctx, nil
	}
	return sdk.Context{}, svnerr.ErrMalformed.Wrap("policy: no SDK context")
}
