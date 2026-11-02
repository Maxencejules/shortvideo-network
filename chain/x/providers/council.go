package providers

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Council hooks. There is no single-signature admit or disable message
// (DEC-009). x/authority calls these while it executes an approved
// AdmitProviderChange or DisableProviderChange, inside the ExecuteChange
// transaction. The checks are apply_proposal_payload (spec_machine.py:1335).
// Every check runs before the one write, so a non-OK code leaves the store
// untouched. x/authority keeps the proposal as REJECTED on FORBIDDEN and
// fails the transaction on any other code.

// AdmitByCouncil moves an APPLIED provider to ADMITTED.
//
//   - no record: NOT_FOUND
//   - devnet false (DEC-002 is not the DEVNET_TEST_ONLY test parameter on
//     this chain; x/authority decides): DECISION_BLOCKED
//   - operator group other than "devnet-test-only", or state other than
//     APPLIED: FORBIDDEN
//
// The operator group id is a label. Admission is not evidence of an
// independent operator; the DEC-002 membership policy stays blocked.
func (k Keeper) AdmitByCouncil(ctx sdk.Context, providerID string, devnet bool) msgkit.Code {
	record, found := k.mustLoad(ctx, providerID)
	if !found {
		return msgkit.NotFound
	}
	if !devnet {
		return msgkit.DecisionBlocked
	}
	if record.GetOperatorGroupId() != DevnetOperatorGroup || record.GetState() != providersv1.ProviderState_PROVIDER_STATE_APPLIED {
		return msgkit.Forbidden
	}
	record.State = providersv1.ProviderState_PROVIDER_STATE_ADMITTED
	k.save(ctx, record)
	emitStateChanged(ctx, record)
	return msgkit.OK
}

// DisableByCouncil moves an ADMITTED or DRAINING provider to DISABLED, a
// terminal state.
//
//   - no record: NOT_FOUND
//   - state other than ADMITTED or DRAINING: CONFLICT
//
// Disablement deletes nothing and releases nothing: reserved bytes, the
// key history, and existing obligations stay until their own legal end.
func (k Keeper) DisableByCouncil(ctx sdk.Context, providerID string) msgkit.Code {
	record, found := k.mustLoad(ctx, providerID)
	if !found {
		return msgkit.NotFound
	}
	switch record.GetState() {
	case providersv1.ProviderState_PROVIDER_STATE_ADMITTED, providersv1.ProviderState_PROVIDER_STATE_DRAINING:
	default:
		return msgkit.Conflict
	}
	record.State = providersv1.ProviderState_PROVIDER_STATE_DISABLED
	k.save(ctx, record)
	emitStateChanged(ctx, record)
	return msgkit.OK
}
