package store

import (
	"crypto/sha256"
	"fmt"
)

// SignerClass is the class of the account whose sequence an intent's
// transaction uses (0002_answers.pgsql). SERVICE and BROADCASTER are the two
// keys the signer process holds (goal 11 Q1); FORWARD_SOURCE is a provider
// account whose own signed transaction the broadcaster forwards verbatim
// (Q2): it holds no key here and no sequence is allocated for it.
type SignerClass string

// Signer classes.
const (
	ClassService       SignerClass = "SERVICE"
	ClassBroadcaster   SignerClass = "BROADCASTER"
	ClassForwardSource SignerClass = "FORWARD_SOURCE"
)

// ObjectType is the kind of chain object an intent acts on.
type ObjectType string

// Object types.
const (
	ObjectContent ObjectType = "CONTENT"
	ObjectOffer   ObjectType = "OFFER"
	ObjectLease   ObjectType = "LEASE"
)

// Request kinds (plan 5.1 under the Q1 and Q2 answers). The broadcaster.kinds
// table holds the same list; TestKindsMatchDatabase compares them.
const (
	KindRegisterContent  = "REGISTER_CONTENT"
	KindWithdrawContent  = "WITHDRAW_CONTENT"
	KindOfferLease       = "OFFER_LEASE"
	KindRelayAcceptLease = "RELAY_ACCEPT_LEASE"
	KindRelayRenewLease  = "RELAY_RENEW_LEASE"
	KindRelayEndLease    = "RELAY_END_LEASE"
	KindForwardProvider  = "FORWARD_PROVIDER_TX"
)

// kindSpec is what a kind fixes: the signer class, and the object type
// (empty for FORWARD_PROVIDER_TX, where the forwarded message type decides).
type kindSpec struct {
	class  SignerClass
	object ObjectType
}

var kindSpecs = map[string]kindSpec{
	KindRegisterContent:  {ClassService, ObjectContent},
	KindWithdrawContent:  {ClassService, ObjectContent},
	KindOfferLease:       {ClassService, ObjectOffer},
	KindRelayAcceptLease: {ClassBroadcaster, ObjectOffer},
	KindRelayRenewLease:  {ClassBroadcaster, ObjectLease},
	KindRelayEndLease:    {ClassBroadcaster, ObjectLease},
	KindForwardProvider:  {ClassForwardSource, ""},
}

// KindSignerClass returns the signer class of kind and whether kind is one
// of the request kinds.
func KindSignerClass(kind string) (SignerClass, bool) {
	spec, ok := kindSpecs[kind]
	return spec.class, ok
}

// The message types a forwarded provider transaction may carry (plan 7.3
// P3) and the object each acts on.
const (
	MsgAcceptLease = "/svn.leases.v1.MsgAcceptLease"
	MsgRenewLease  = "/svn.leases.v1.MsgRenewLease"
	MsgEndLease    = "/svn.leases.v1.MsgEndLease"
)

var forwardObject = map[string]ObjectType{
	MsgAcceptLease: ObjectOffer,
	MsgRenewLease:  ObjectLease,
	MsgEndLease:    ObjectLease,
}

// ObjectRef is the chain object a validated intent acts on, decoded from its
// request by the validating caller: the content id and version of
// REGISTER_CONTENT and WITHDRAW_CONTENT, the offer id of OFFER_LEASE and
// RELAY_ACCEPT_LEASE (and of a forwarded acceptance), the lease id of
// RELAY_RENEW_LEASE and RELAY_END_LEASE (and of a forwarded renewal or end).
// Version is used for CONTENT only.
type ObjectRef struct {
	Type    ObjectType
	ID      string
	Version uint64
}

// ForwardFields are the decoded and validated fields of a provider-signed
// transaction that FORWARD_PROVIDER_TX carries verbatim (plan 7.3 P3). The
// transaction bytes are the request; their SHA-256 is the transaction hash.
type ForwardFields struct {
	// MessageType is the one message's type URL: MsgAcceptLease,
	// MsgRenewLease or MsgEndLease.
	MessageType string
	// ProviderID is the provider whose registered account is the outer
	// signer.
	ProviderID string
	// BusinessID is the message's business id, chosen by the provider.
	BusinessID string
	// AccountNumber, Sequence, TimeoutHeight and Gas are the signed
	// envelope's values.
	AccountNumber uint64
	Sequence      uint64
	TimeoutHeight uint64
	Gas           uint64
}

// validFields is what checkValidated returns for the database: the object
// columns and, for a forward, the converted envelope values and hash.
type validFields struct {
	objectType    string
	objectID      string
	objectVersion *int64
	forward       *forwardRow
}

// forwardRow is a ForwardFields converted for the forwarded_txs row.
type forwardRow struct {
	ForwardFields
	txHash        [32]byte
	accountNumber int64
	sequence      int64
	timeoutHeight int64
	gas           int64
}

// checkValidated checks what a validated intent of kind carries: the object
// matches the kind (or the forwarded message type), the forward fields are
// present exactly for FORWARD_PROVIDER_TX, and every value is within the
// schema's bounds. request is the stored request; for a forward it is the
// forwarded transaction, whose hash it computes.
func checkValidated(kind string, request []byte, obj ObjectRef, fwd *ForwardFields) (validFields, error) {
	spec, ok := kindSpecs[kind]
	if !ok {
		return validFields{}, fmt.Errorf("%w: kind", ErrInvalid)
	}
	want := spec.object
	var v validFields
	switch {
	case spec.class == ClassForwardSource && fwd == nil:
		return validFields{}, fmt.Errorf("%w: a forwarded intent needs its forward fields", ErrInvalid)
	case spec.class != ClassForwardSource && fwd != nil:
		return validFields{}, fmt.Errorf("%w: forward fields on a key-signed kind", ErrInvalid)
	case fwd != nil:
		fobj, ok := forwardObject[fwd.MessageType]
		if !ok {
			return validFields{}, fmt.Errorf("%w: forwarded message type", ErrInvalid)
		}
		want = fobj
		row, err := fwd.row(request)
		if err != nil {
			return validFields{}, err
		}
		v.forward = &row
	}
	if obj.Type != want || !idRE.MatchString(obj.ID) {
		return validFields{}, fmt.Errorf("%w: object", ErrInvalid)
	}
	v.objectType, v.objectID = string(obj.Type), obj.ID
	switch {
	case obj.Type == ObjectContent:
		ver, err := toInt64("content version", obj.Version)
		if err != nil {
			return validFields{}, err
		}
		v.objectVersion = &ver
	case obj.Version != 0:
		return validFields{}, fmt.Errorf("%w: object version outside CONTENT", ErrInvalid)
	}
	return v, nil
}

// row checks the forward fields and converts them.
func (f ForwardFields) row(request []byte) (forwardRow, error) {
	if !idRE.MatchString(f.ProviderID) || !idRE.MatchString(f.BusinessID) {
		return forwardRow{}, fmt.Errorf("%w: provider or business id", ErrInvalid)
	}
	if f.TimeoutHeight == 0 || f.Gas < 1 || f.Gas > maxGas {
		return forwardRow{}, fmt.Errorf("%w: timeout height or gas", ErrInvalid)
	}
	r := forwardRow{ForwardFields: f, txHash: sha256.Sum256(request), gas: int64(f.Gas)}
	var err error
	if r.accountNumber, err = toInt64("account number", f.AccountNumber); err != nil {
		return forwardRow{}, err
	}
	if r.sequence, err = toInt64("sequence", f.Sequence); err != nil {
		return forwardRow{}, err
	}
	if r.timeoutHeight, err = toInt64("timeout height", f.TimeoutHeight); err != nil {
		return forwardRow{}, err
	}
	return r, nil
}
