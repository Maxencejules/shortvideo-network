// Package policy holds the broadcaster's authorization rules as plain
// functions over typed inputs (plan sections 5.7 and 7.1-7.6, under the
// owner's answers of 2026-11-08: Q1 "Both keys, separate signer
// (Recommended)", Q2 "Relay + verbatim forward (Recommended)", Q3
// "Operator-registered intent keys (Recommended)", Q4 "Minimal + clear
// unsent (Recommended)"). The rules themselves are PROPOSED: the boundary
// document (DR-11-01) is not approved.
//
//   - kinds.go: the request kinds, the key class that signs each, and the
//     per-class type URL allowlist that serve and the signer both check.
//   - governance.go: the start-up role check of the configured keys
//     against Query/Governance.
//   - registry.go: the operator's intent-key registry (public keys only).
//   - intent.go: hosting-intent verification and the publication check.
//   - relay.go: the provider signature pre-check of relayed lease messages.
//   - forward.go: the checks on a provider-signed transaction forwarded
//     verbatim.
//   - permissions.go: caller kinds, the authorized request context, and
//     the object permissions over ownership states the store provides.
//
// The chain verifies every signature again; these checks fail fast and
// keep the broadcaster from being a confused deputy. Ownership and intent
// state come from the store; chain state comes from internal/chain reads
// of one trusted node. Nothing here signs anything.
package policy

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"

	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Kind is a request kind (plan 5.1). The kind fixes the chain message and
// the key class; the caller never chooses the signer.
type Kind string

// Kinds. No kind exists for provider registry messages, council messages,
// MsgExecuteChange, EndLease with authority_end (DR-11-12) or audit
// batches (Q2: audit batches stay on the auditor's own path).
const (
	KindRegisterContent   Kind = "REGISTER_CONTENT"
	KindWithdrawContent   Kind = "WITHDRAW_CONTENT"
	KindOfferLease        Kind = "OFFER_LEASE"
	KindRelayAcceptLease  Kind = "RELAY_ACCEPT_LEASE"
	KindRelayRenewLease   Kind = "RELAY_RENEW_LEASE"
	KindRelayEndLease     Kind = "RELAY_END_LEASE"
	KindForwardProviderTx Kind = "FORWARD_PROVIDER_TX"
)

// Class is a broadcaster key class (plan 7.1). The forward kind has no
// class: the provider's own account signs it and no broadcaster key is
// used.
type Class string

// Key classes.
const (
	ClassService     Class = "SERVICE"
	ClassBroadcaster Class = "BROADCASTER"
)

// Permission is one grant of the authorized request context (plan 5.5).
type Permission string

// Permissions.
const (
	PermPublishContent      Permission = "PUBLISH_CONTENT"
	PermWithdrawContent     Permission = "WITHDRAW_CONTENT"
	PermScheduleHosting     Permission = "SCHEDULE_HOSTING"
	PermRelayProviderSigned Permission = "RELAY_PROVIDER_SIGNED"
)

// Type URLs of the routed messages, computed from the generated types.
var (
	TypeRegisterContent = sdk.MsgTypeURL(&contentv1.MsgRegisterContent{})
	TypeWithdrawContent = sdk.MsgTypeURL(&contentv1.MsgWithdrawContent{})
	TypeOfferLease      = sdk.MsgTypeURL(&leasesv1.MsgOfferLease{})
	TypeAcceptLease     = sdk.MsgTypeURL(&leasesv1.MsgAcceptLease{})
	TypeRenewLease      = sdk.MsgTypeURL(&leasesv1.MsgRenewLease{})
	TypeEndLease        = sdk.MsgTypeURL(&leasesv1.MsgEndLease{})
)

// Spec is the fixed routing of one kind.
type Spec struct {
	Kind Kind
	// Class is the key class that signs the outer transaction; empty for
	// the forward kind.
	Class Class
	// TypeURL is the chain message the kind produces; empty for the
	// forward kind, which carries one of RelayTypeURLs.
	TypeURL string
	// Permission is the grant the authorized request context must carry.
	Permission Permission
}

var specs = []Spec{
	{KindRegisterContent, ClassService, TypeRegisterContent, PermPublishContent},
	{KindWithdrawContent, ClassService, TypeWithdrawContent, PermWithdrawContent},
	{KindOfferLease, ClassService, TypeOfferLease, PermScheduleHosting},
	{KindRelayAcceptLease, ClassBroadcaster, TypeAcceptLease, PermRelayProviderSigned},
	{KindRelayRenewLease, ClassBroadcaster, TypeRenewLease, PermRelayProviderSigned},
	{KindRelayEndLease, ClassBroadcaster, TypeEndLease, PermRelayProviderSigned},
	{KindForwardProviderTx, "", "", PermRelayProviderSigned},
}

// Kinds returns every kind in a fixed order.
func Kinds() []Kind {
	out := make([]Kind, len(specs))
	for i, s := range specs {
		out[i] = s.Kind
	}
	return out
}

// SpecOf returns the routing of kind. An unknown kind is CodeUnknownKind.
func SpecOf(kind Kind) (Spec, error) {
	for _, s := range specs {
		if s.Kind == kind {
			return s, nil
		}
	}
	return Spec{}, refuse(CodeUnknownKind, "kind is not one of the routed kinds")
}

// RequiredRole is the on-chain role a key of the class must have
// (Query/Governance at start-up).
func RequiredRole(class Class) (typesv1.SubmitterRole, bool) {
	switch class {
	case ClassService:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE, true
	case ClassBroadcaster:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER, true
	}
	return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED, false
}

// AllowedTypeURLs returns the type URL allowlist of class (plan 7.1): the
// service key signs register, withdraw and offer; the broadcaster key
// signs accept, renew and the provider-signed end. Any other class has
// none.
func AllowedTypeURLs(class Class) []string {
	var out []string
	for _, s := range specs {
		if s.Class == class && class != "" {
			out = append(out, s.TypeURL)
		}
	}
	return out
}

// Allowed reports whether class's allowlist holds typeURL.
func Allowed(class Class, typeURL string) bool { return contains(AllowedTypeURLs(class), typeURL) }

// RelayTypeURLs are the messages a forwarded provider transaction may
// carry: the provider's own accept, renew and end.
func RelayTypeURLs() []string { return []string{TypeAcceptLease, TypeRenewLease, TypeEndLease} }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// CheckSignable is the allowlist check that serve runs before it asks the
// signer and that the signer runs again on what it received: msg's type
// URL is in class's allowlist, msg names signer as its only signer, and an
// EndLease has authority_end false. signer is the key's 20-byte address.
func CheckSignable(class Class, signer []byte, msg sdk.Msg) error {
	if msg == nil {
		return refuse(CodeInvalid, "no message")
	}
	typeURL := sdk.MsgTypeURL(msg)
	if !Allowed(class, typeURL) {
		return refuse(CodeTypeNotAllowed, "class %s may not sign %s", class, typeURL)
	}
	got, ok := messageSigner(msg)
	if !ok || len(signer) != genesis.AccountBytes || !bytes.Equal(got, signer) {
		return refuse(CodeSignerMismatch, "message signer is not the key address")
	}
	if end, ok := msg.(*leasesv1.MsgEndLease); ok && end.GetAuthorityEnd() {
		return refuse(CodeAuthorityEnd, "EndLease with authority_end is never signed by the broadcaster")
	}
	return nil
}

// messageSigner returns the signer field of a routed message.
func messageSigner(msg sdk.Msg) ([]byte, bool) {
	switch m := msg.(type) {
	case *contentv1.MsgRegisterContent:
		return m.GetSigner(), true
	case *contentv1.MsgWithdrawContent:
		return m.GetSigner(), true
	case *leasesv1.MsgOfferLease:
		return m.GetSigner(), true
	case *leasesv1.MsgAcceptLease:
		return m.GetSigner(), true
	case *leasesv1.MsgRenewLease:
		return m.GetSigner(), true
	case *leasesv1.MsgEndLease:
		return m.GetSigner(), true
	}
	return nil, false
}
