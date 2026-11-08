package policy

import (
	"bytes"
	"context"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// PrincipalKindDevFixture is the only principal kind of goal 11: a
// development fixture principal (plan 5.5). Development principals are
// refused off a shortvideo-dev- chain by the API and config (plan 5.6).
const PrincipalKindDevFixture = "DEV_FIXTURE"

// MaxPermissions bounds the permission list of one context: each of the
// four permissions at most once.
const MaxPermissions = 4

// AuthContext is the authorized request context a caller attaches (plan
// 5.5, owner answer Q4 "Minimal + clear unsent (Recommended)"): opaque ids
// and an enum list, nothing else. Its JSON shape is these four fields; a
// decoder that refuses unknown fields refuses every other key (an email,
// an address, a token, a header copy, free text).
type AuthContext struct {
	PrincipalKind string       `json:"principal_kind"`
	PrincipalID   string       `json:"principal_id"`
	Permissions   []Permission `json:"permissions"`
	PolicyRef     string       `json:"policy_ref"`
}

// knownPermissions lists every Permission.
var knownPermissions = map[Permission]bool{
	PermPublishContent: true, PermWithdrawContent: true, PermScheduleHosting: true, PermRelayProviderSigned: true,
}

// Validate checks the field rules of plan 5.5: principal kind
// DEV_FIXTURE, principal id and policy ref in the protocol id pattern, and
// one to MaxPermissions known permissions without repeats.
func (a AuthContext) Validate() error {
	if a.PrincipalKind != PrincipalKindDevFixture {
		return refuse(CodeInvalid, "auth context principal_kind must be %s", PrincipalKindDevFixture)
	}
	if !msgkit.ValidID(a.PrincipalID) {
		return refuse(CodeInvalid, "auth context principal_id is outside the id pattern")
	}
	if !msgkit.ValidID(a.PolicyRef) {
		return refuse(CodeInvalid, "auth context policy_ref is outside the id pattern")
	}
	if len(a.Permissions) == 0 || len(a.Permissions) > MaxPermissions {
		return refuse(CodeInvalid, "auth context needs 1 to %d permissions", MaxPermissions)
	}
	seen := map[Permission]bool{}
	for _, p := range a.Permissions {
		if !knownPermissions[p] {
			return refuse(CodeInvalid, "auth context names an unknown permission")
		}
		if seen[p] {
			return refuse(CodeInvalid, "auth context repeats a permission")
		}
		seen[p] = true
	}
	return nil
}

// Canonical returns a copy with the permissions sorted, the form the
// request digest covers (plan 5.4).
func (a AuthContext) Canonical() AuthContext {
	out := a
	out.Permissions = append([]Permission(nil), a.Permissions...)
	sort.Slice(out.Permissions, func(i, j int) bool { return out.Permissions[i] < out.Permissions[j] })
	return out
}

// Grants reports whether the context carries p.
func (a AuthContext) Grants(p Permission) bool {
	for _, have := range a.Permissions {
		if have == p {
			return true
		}
	}
	return false
}

// Caller is one authenticated caller's configuration (plan 5.7): the
// kinds it may send and an optional provider binding for a provider-agent
// principal.
type Caller struct {
	Principal       string
	Kinds           []Kind
	BoundProviderID string
}

// CheckKind refuses a kind the caller may not send (an unknown kind is
// CodeUnknownKind, a known one not granted is CodeKindNotAllowed).
func (c Caller) CheckKind(kind Kind) error {
	if _, err := SpecOf(kind); err != nil {
		return err
	}
	for _, k := range c.Kinds {
		if k == kind {
			return nil
		}
	}
	return refuse(CodeKindNotAllowed, "caller may not send %s", kind)
}

// CheckGrant refuses a request whose context lacks the kind's permission.
func CheckGrant(kind Kind, ctx AuthContext) error {
	spec, err := SpecOf(kind)
	if err != nil {
		return err
	}
	if err := ctx.Validate(); err != nil {
		return err
	}
	if !ctx.Grants(spec.Permission) {
		return refuse(CodePermission, "auth context does not grant %s", spec.Permission)
	}
	return nil
}

// OwnerStatus is the state of a content ownership row (plan 5.7, DR-11-34).
type OwnerStatus string

// Owner states.
const (
	OwnerPending   OwnerStatus = "PENDING"
	OwnerEffective OwnerStatus = "EFFECTIVE"
	OwnerVoid      OwnerStatus = "VOID"
)

// ContentOwner is the store's ownership row of one content version.
type ContentOwner struct {
	ContentID       string
	ContentVersion  uint64
	CallerPrincipal string
	PrincipalID     string
	SignerAddress   []byte
	Status          OwnerStatus
}

// notVisible is the one uniform answer for an object the caller may not
// act on: absent, another caller's or another principal's, not yet
// effective, not hosted by our service address, or withdrawn. It never
// says which (DR-11-34).
func notVisible() error { return refuse(CodeNotVisible, "object not visible to this caller") }

// unavailable is the one uniform answer for a content id a register may
// not use (CONTENT_ID_UNAVAILABLE, plan 5.2).
func unavailable() error { return refuse(CodeContentIDTaken, "content id unavailable") }

// hostedBy reports whether a Query/Content answer is a live record (no
// tombstone) whose submitting host is service.
func hostedBy(onchain *contentv1.QueryContentResponse, service []byte) bool {
	return onchain != nil && onchain.GetTombstone() == nil && onchain.GetContent() != nil &&
		len(service) == genesis.AccountBytes && bytes.Equal(onchain.GetContent().GetSubmittingHost(), service)
}

// PublicationInput is what serve knows when it checks a REGISTER_CONTENT
// or WITHDRAW_CONTENT request (plan 7.4).
type PublicationInput struct {
	Caller Caller
	Auth   AuthContext
	// Msg is the MsgRegisterContent or MsgWithdrawContent to sign, with
	// the pinned service address as signer.
	Msg sdk.Msg
	// Service is the pinned service signer address (the submitting host).
	Service []byte
	// Registry is the operator's intent-key registry.
	Registry *Registry
	// Owner is the store's non-VOID ownership row of the content version,
	// nil when there is none.
	Owner *ContentOwner
	// OnChain is Query/Content for the content version; nil when the
	// chain has neither a record nor a tombstone.
	OnChain *contentv1.QueryContentResponse
}

// CheckPublication is serve's publication-permission check (plan 7.4 and
// 5.7, owner answer Q3), run at enqueue and again before signing:
//
//  1. the caller may send the kind, and the context grants
//     PUBLISH_CONTENT (register) or WITHDRAW_CONTENT (withdraw);
//  2. the message names the pinned service address as signer;
//  3. the hosting intent's public key is the key the registry lists for
//     the context's principal, and the intent verifies over the payload
//     rebuilt for the pinned service address as submitting host;
//  4. register: no non-VOID ownership row exists and the chain has no
//     record or tombstone of the version (else CONTENT_ID_UNAVAILABLE);
//     withdraw: the ownership row is EFFECTIVE for this caller, principal
//     and service address, and the chain shows a live record hosted by the
//     service address (else the uniform NOT_VISIBLE).
func CheckPublication(in PublicationInput) error {
	var kind Kind
	switch in.Msg.(type) {
	case *contentv1.MsgRegisterContent:
		kind = KindRegisterContent
	case *contentv1.MsgWithdrawContent:
		kind = KindWithdrawContent
	default:
		return refuse(CodeInvalid, "not a content message")
	}
	if err := in.Caller.CheckKind(kind); err != nil {
		return err
	}
	if err := CheckGrant(kind, in.Auth); err != nil {
		return err
	}
	if err := CheckSignable(ClassService, in.Service, in.Msg); err != nil {
		return err
	}
	if in.Registry == nil {
		return refuse(CodeIntentKeyUnregistered, "no intent-key registry loaded")
	}
	registered, ok := in.Registry.KeyFor(in.Auth.PrincipalID)
	if !ok {
		return refuse(CodeIntentKeyUnregistered, "no intent key is registered for the principal")
	}
	pub, err := VerifyHostingIntent(in.Msg, in.Service)
	if err != nil {
		return err
	}
	if !bytes.Equal(pub, registered) {
		return refuse(CodeIntentKeyUnregistered, "hosting intent key is not the principal's registered key")
	}
	switch m := in.Msg.(type) {
	case *contentv1.MsgRegisterContent:
		if in.Owner != nil && in.Owner.Status != OwnerVoid {
			return unavailable()
		}
		if in.OnChain != nil && (in.OnChain.GetContent() != nil || in.OnChain.GetTombstone() != nil) {
			return unavailable()
		}
	case *contentv1.MsgWithdrawContent:
		if !ownsEffective(in.Owner, m.GetContentId(), m.GetContentVersion(), in.Caller.Principal, in.Service) ||
			in.Owner.PrincipalID != in.Auth.PrincipalID || !hostedBy(in.OnChain, in.Service) {
			return notVisible()
		}
	}
	return nil
}

// ownsEffective reports whether owner is the EFFECTIVE row of the content
// version for caller, pinned to service.
func ownsEffective(owner *ContentOwner, contentID string, version uint64, caller string, service []byte) bool {
	return owner != nil && owner.Status == OwnerEffective && owner.ContentID == contentID &&
		owner.ContentVersion == version && owner.CallerPrincipal == caller && bytes.Equal(owner.SignerAddress, service)
}

// OfferInput is what serve knows when it checks an OFFER_LEASE request.
type OfferInput struct {
	Caller  Caller
	Auth    AuthContext
	Msg     sdk.Msg
	Service []byte
	Owner   *ContentOwner
	OnChain *contentv1.QueryContentResponse
}

// CheckOffer is the OFFER_LEASE object permission (plan 5.7), run at
// enqueue and again before signing: the kind and SCHEDULE_HOSTING, the
// pinned service address as signer, an EFFECTIVE ownership row of this
// caller for the offered content version, and a live (not withdrawn)
// record hosted by the service address on chain. A PENDING row does not
// count: ownership counts only after the chain shows our registration.
func CheckOffer(in OfferInput) error {
	if err := in.Caller.CheckKind(KindOfferLease); err != nil {
		return err
	}
	if err := CheckGrant(KindOfferLease, in.Auth); err != nil {
		return err
	}
	if err := CheckSignable(ClassService, in.Service, in.Msg); err != nil {
		return err
	}
	offer, ok := in.Msg.(*leasesv1.MsgOfferLease)
	if !ok {
		return refuse(CodeInvalid, "not an offer")
	}
	if !ownsEffective(in.Owner, offer.GetContentId(), offer.GetContentVersion(), in.Caller.Principal, in.Service) ||
		!hostedBy(in.OnChain, in.Service) {
		return notVisible()
	}
	return nil
}

// OwnerBecomesEffective is the promotion rule of plan 5.7: a PENDING row
// becomes EFFECTIVE only when its REGISTER intent is COMMITTED with the
// pinned service address as committing signer and Query/Content shows that
// address as the live record's submitting host.
func OwnerBecomesEffective(owner ContentOwner, committingSigner []byte, onchain *contentv1.QueryContentResponse) bool {
	return owner.Status == OwnerPending && len(owner.SignerAddress) == genesis.AccountBytes &&
		bytes.Equal(committingSigner, owner.SignerAddress) && hostedBy(onchain, owner.SignerAddress) &&
		onchain.GetContent().GetContentId() == owner.ContentID && onchain.GetContent().GetContentVersion() == owner.ContentVersion
}

// RelayAuthority is how a caller may relay a provider's signed lease
// message (plan 5.7): it is bound to the provider, or it owns a COMMITTED
// OFFER_LEASE intent for the offer that our service address committed.
type RelayAuthority struct {
	Caller Caller
	// OwnsCommittedOffer reports whether the caller owns a COMMITTED
	// OFFER_LEASE intent for offerID whose committing signer is the
	// pinned service address (a store read).
	OwnsCommittedOffer func(ctx context.Context, offerID string) (bool, error)
}

// CheckRelay is the relay object permission, after the provider
// signature pre-check found the offer and provider the message acts on
// (RelayResult): the caller is bound to that provider, or owns the
// committed offer. Otherwise RELAY_NOT_ALLOWED.
func (a RelayAuthority) CheckRelay(ctx context.Context, kind Kind, auth AuthContext, found RelayResult) error {
	if err := a.Caller.CheckKind(kind); err != nil {
		return err
	}
	if err := CheckGrant(kind, auth); err != nil {
		return err
	}
	if a.Caller.BoundProviderID != "" && a.Caller.BoundProviderID == found.ProviderID {
		return nil
	}
	if a.OwnsCommittedOffer == nil || found.OfferID == "" {
		return refuse(CodeRelayNotAllowed, "caller is not bound to the provider and owns no committed offer")
	}
	owns, err := a.OwnsCommittedOffer(ctx, found.OfferID)
	if err != nil {
		return err
	}
	if !owns {
		return refuse(CodeRelayNotAllowed, "caller is not bound to the provider and owns no committed offer")
	}
	return nil
}
