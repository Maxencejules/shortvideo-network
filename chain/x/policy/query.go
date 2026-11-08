package policy

import (
	"context"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Policy is svn.policy.v1.Query/Policy (deviation D-9: the spec machine has
// no query semantics). It reads committed state at the query height and
// never writes. The response is the PolicyState of protocol schema version
// 2 (docs/protocol.md, "Goal 10 additive revision"):
//
//   - policy_version: the council policy version (Deps.Versions). It names
//     the executed decisions, not the limits in force: a scheduled change
//     moved it at execute and applies later.
//   - safety_caps: the stored caps (0x01)
//   - blocked_decisions: the authority's blocked decision rows
//     (Deps.Decisions), copied
//   - governed_limits: the value in force for each of the five governed
//     limit keys, in key order: the governed value (0x02) when one is set,
//     else the key's ceiling (msgkit.Effective)
//   - pending_limit_changes: every pending change (0x03), in key order, with
//     its activation height, proposal id, and the policy version its execute
//     produced. The limits in force at a later height are governed_limits
//     with these rows applied at their heights.
//   - upgrade_plan: the stored plan (0x06), absent when none
//   - app_version: the stored app version (0x04), 0 when absent
//
// The response is bounded: five governed values, at most five pending
// changes, at most one plan, eight decision rows. A missing or damaged row
// is a plain error, never a panic.
func (k Keeper) Policy(goCtx context.Context, _ *policyv1.QueryPolicyRequest) (*policyv1.QueryPolicyResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	caps, err := k.SafetyCaps(ctx)
	if err != nil {
		return nil, err
	}
	store := k.kv(ctx)
	governed, err := governedInForce(store)
	if err != nil {
		return nil, err
	}
	rows, err := readPending(store)
	if err != nil {
		return nil, err
	}
	plan, planned, err := loadPlan(store)
	if err != nil {
		return nil, err
	}
	appVersion, err := loadAppVersion(store)
	if err != nil {
		return nil, err
	}
	state := &policyv1.PolicyState{
		PolicyVersion:       k.deps.Versions.PolicyVersion(ctx),
		SafetyCaps:          caps,
		BlockedDecisions:    cloneDecisions(k.deps.Decisions.BlockedDecisions(ctx)),
		GovernedLimits:      governed,
		PendingLimitChanges: pendingMessages(rows),
		AppVersion:          appVersion,
	}
	if planned {
		state.UpgradePlan = planMessage(plan)
	}
	return &policyv1.QueryPolicyResponse{State: state}, nil
}

// governedInForce returns the value in force of every governed limit key,
// in key order.
func governedInForce(store storetypes.KVStore) ([]*policyv1.GovernedLimitValue, error) {
	keys := msgkit.LimitKeys()
	out := make([]*policyv1.GovernedLimitValue, 0, len(keys))
	for _, key := range keys {
		value, found, err := loadGoverned(store, key)
		if err != nil {
			return nil, err
		}
		out = append(out, &policyv1.GovernedLimitValue{LimitKey: key, Value: msgkit.Effective(key, value, found)})
	}
	return out, nil
}

func pendingMessages(rows []keyedPending) []*policyv1.PendingLimitChange {
	out := make([]*policyv1.PendingLimitChange, 0, len(rows))
	for _, row := range rows {
		out = append(out, &policyv1.PendingLimitChange{
			LimitKey:         row.Key,
			Value:            row.Value,
			ActivationHeight: row.Activation,
			ProposalId:       row.ProposalID,
			PolicyVersion:    row.PolicyVersion,
		})
	}
	return out
}

func planMessage(plan Plan) *policyv1.UpgradePlan {
	return &policyv1.UpgradePlan{
		Name:   plan.Name,
		Height: plan.Height,
		ReleaseDigest: &typesv1.Digest{
			Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
			Value:     append([]byte(nil), plan.Digest...),
		},
		ProposalId: plan.ProposalID,
	}
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
