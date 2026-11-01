package providers

import (
	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Handoff API for the lease module (goal 08). These reads never write.
// Reserve and release accounting is goal 08; it extends this package.

// Provider returns a copy of one provider record.
func (k Keeper) Provider(ctx sdk.Context, providerID string) (*providersv1.Provider, bool) {
	record, found := k.mustLoad(ctx, providerID)
	if !found {
		return nil, false
	}
	return proto.Clone(record).(*providersv1.Provider), true
}

// CheckNewObligation is the provider part of op_offer (spec_machine.py:583)
// and the provider-state part of op_accept (:685): a new obligation needs
// an ADMITTED provider.
//
//   - no record: NOT_FOUND
//   - APPLIED, DRAINING, or DISABLED: FORBIDDEN
//
// Existing obligations of a DRAINING or DISABLED provider are not checked
// here; they end only by their own legal transition.
func (k Keeper) CheckNewObligation(ctx sdk.Context, providerID string) (*providersv1.Provider, msgkit.Code) {
	record, found := k.Provider(ctx, providerID)
	if !found {
		return nil, msgkit.NotFound
	}
	if record.GetState() != providersv1.ProviderState_PROVIDER_STATE_ADMITTED {
		return nil, msgkit.Forbidden
	}
	return record, msgkit.OK
}

// VerifyProviderSignature checks an inner provider signature in domain over
// canonical (SVN-JCS-1 text) against the provider's current key only. A
// retired key never verifies ("the old key is retained and cannot sign new
// accepts"). Order: no record NOT_FOUND, then msgkit.VerifyInner (structure
// MALFORMED / WRONG_DOMAIN / OUT_OF_RANGE / BOUNDS, then key id, public key,
// and ed25519 UNAUTHORIZED).
func (k Keeper) VerifyProviderSignature(ctx sdk.Context, providerID string, domain typesv1.SignatureDomain, canonical string, sig *typesv1.InnerSignature) msgkit.Code {
	record, found := k.mustLoad(ctx, providerID)
	if !found {
		return msgkit.NotFound
	}
	return msgkit.VerifyInner(sig, domain, record.GetSigningKeyId(), record.GetSigningPubkey(), canonical)
}
