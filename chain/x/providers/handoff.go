package providers

import (
	"math"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Handoff API for the lease module (goal 08). Provider, CheckNewObligation,
// VerifyProviderSignature, and ReserveProbe never write. Reserve and
// Release are the capacity accounting x/leases runs at accept, end, and
// expiry; they write only reserved_bytes.

// Provider returns a copy of one provider record without its retired keys
// (previous_signing_keys is empty). The read is a constant amount of work,
// however many times the provider rotated, so a consensus path can call it.
// Query/Provider returns the retired keys.
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
// canonical (SVN-JCS-1 text) against the provider's current key only. Only
// the current key verifies; a key the provider retired does not while it
// stays retired ("the old key is retained and cannot sign new accepts").
// The v0 spec lets a provider rotate back to a retired key (rotation
// compares the new key with the current key only); that key is then the
// current key and verifies again. Order: no record NOT_FOUND, then
// msgkit.VerifyInner (structure MALFORMED / WRONG_DOMAIN / OUT_OF_RANGE /
// BOUNDS, then key id, public key, and ed25519 UNAUTHORIZED).
func (k Keeper) VerifyProviderSignature(ctx sdk.Context, providerID string, domain typesv1.SignatureDomain, canonical string, sig *typesv1.InnerSignature) msgkit.Code {
	record, found := k.mustLoad(ctx, providerID)
	if !found {
		return msgkit.NotFound
	}
	return msgkit.VerifyInner(sig, domain, record.GetSigningKeyId(), record.GetSigningPubkey(), canonical)
}

// ReserveProbe is reserve_probe (spec_machine.py:677): the capacity check
// of op_offer, without a write. It runs the checks of Reserve, in the same
// order, and returns the code Reserve would return:
//
//   - amount 0: MALFORMED (a wiring error: a lease always holds at least
//     one byte, so the lease module never asks for 0)
//   - no record (an id outside the id pattern has none): NOT_FOUND
//   - amount > MaxUint64 - reserved_bytes: OVERFLOW
//   - reserved_bytes + amount > admitted_capacity_bytes: CAPACITY
//
// There is no provider-state check: the spec machine has none, and
// CheckNewObligation is the state check of a new obligation. A stored
// record that does not decode is an invariant violation and panics, as in
// every handoff read (mustLoad); the SDK fails the transaction on every
// node alike.
func (k Keeper) ReserveProbe(ctx sdk.Context, providerID string, amount uint64) msgkit.Code {
	_, code := k.reserveCheck(ctx, providerID, amount)
	return code
}

// Reserve is reserve (spec_machine.py:286): the checks of ReserveProbe,
// then reserved_bytes += amount. It works in every provider state. A
// non-OK code writes nothing. Success rewrites the provider record with
// only reserved_bytes changed; the record has a constant size (retired
// keys are rows of their own), so the write costs the same however many
// times the provider rotated. It emits no event: the lease event that
// causes it carries the bytes.
func (k Keeper) Reserve(ctx sdk.Context, providerID string, amount uint64) msgkit.Code {
	record, code := k.reserveCheck(ctx, providerID, amount)
	if code != msgkit.OK {
		return code
	}
	record.ReservedBytes += amount
	k.save(ctx, record)
	return msgkit.OK
}

// Release is release (spec_machine.py:296): reserved_bytes -= amount.
//
//   - amount 0: MALFORMED (a wiring error, as in ReserveProbe)
//   - no record: NOT_FOUND
//   - amount > reserved_bytes: UNDERFLOW
//
// It works in every provider state: a draining or disabled provider's
// obligations still end and release. A non-OK code writes nothing.
// Success rewrites the record with only reserved_bytes changed and emits
// no event.
func (k Keeper) Release(ctx sdk.Context, providerID string, amount uint64) msgkit.Code {
	if amount == 0 {
		return msgkit.Malformed
	}
	record, found := k.mustLoad(ctx, providerID)
	if !found {
		return msgkit.NotFound
	}
	if amount > record.GetReservedBytes() {
		return msgkit.Underflow
	}
	record.ReservedBytes -= amount
	k.save(ctx, record)
	return msgkit.OK
}

// reserveCheck runs the checks of reserve and reserve_probe and returns the
// loaded record when they pass. The overflow check comes before the
// capacity check, as in the spec, and is written so that it never wraps.
func (k Keeper) reserveCheck(ctx sdk.Context, providerID string, amount uint64) (*providersv1.Provider, msgkit.Code) {
	if amount == 0 {
		return nil, msgkit.Malformed
	}
	record, found := k.mustLoad(ctx, providerID)
	if !found {
		return nil, msgkit.NotFound
	}
	reserved := record.GetReservedBytes()
	if amount > math.MaxUint64-reserved {
		return nil, msgkit.Overflow
	}
	if reserved+amount > record.GetAdmittedCapacityBytes() {
		return nil, msgkit.Capacity
	}
	return record, msgkit.OK
}
