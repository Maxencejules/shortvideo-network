package chain

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// ErrNotRouted is a message this package reconciles no effect for: every
// message except the six content and lease messages below, and EndLease
// with authority_end (DR-11-12).
var ErrNotRouted = errors.New("chain: message type is not reconciled by the broadcaster")

// ObjectKey names the chain object an intended message acts on and the
// typed event a first commitment of that action emits (chain module
// READMEs: x/content "Events", x/leases "Events").
type ObjectKey struct {
	// TypeURL is the intended message's type URL.
	TypeURL string
	// EventType is the full name of the typed event of a first commitment.
	EventType string
	// Module names the "<Module>_result idempotent=true" event of an
	// idempotent replay.
	Module string
	// IDKey and ID are the event attribute that names the object and its
	// value; VersionKey and Version narrow it to one content version or
	// one lease term (VersionKey is empty when the kind has none).
	IDKey      string
	ID         string
	VersionKey string
	Version    uint64
	// SignerBound is the signer rule of plan 8.6: the committing signer of
	// an offer, a register or a withdraw must be the intent's pinned
	// service address. For the relay messages (accept, renew, provider
	// end) a BROADCASTER or the provider's own account may commit.
	SignerBound bool
}

// ObjectOf returns the object key of an intended message. The object id
// must match the protocol id pattern, so the key's query string carries no
// quote.
func ObjectOf(msg sdk.Msg) (ObjectKey, error) {
	var key ObjectKey
	switch m := msg.(type) {
	case *contentv1.MsgRegisterContent:
		key = ObjectKey{EventType: "svn.events.v1.ContentRegistered", Module: content.ModuleName, IDKey: "content_id",
			ID: m.GetContentId(), VersionKey: "content_version", Version: m.GetContentVersion(), SignerBound: true}
	case *contentv1.MsgWithdrawContent:
		key = ObjectKey{EventType: "svn.events.v1.ContentWithdrawn", Module: content.ModuleName, IDKey: "content_id",
			ID: m.GetContentId(), VersionKey: "content_version", Version: m.GetContentVersion(), SignerBound: true}
	case *leasesv1.MsgOfferLease:
		key = ObjectKey{EventType: "svn.events.v1.LeaseOffered", Module: leases.ModuleName, IDKey: "offer_id",
			ID: m.GetOfferId(), SignerBound: true}
	case *leasesv1.MsgAcceptLease:
		key = ObjectKey{EventType: "svn.events.v1.LeaseAccepted", Module: leases.ModuleName, IDKey: "lease_id", ID: m.GetLeaseId()}
	case *leasesv1.MsgRenewLease:
		key = ObjectKey{EventType: "svn.events.v1.LeaseRenewed", Module: leases.ModuleName, IDKey: "lease_id",
			ID: m.GetLeaseId(), VersionKey: "term_version", Version: m.GetTermVersion()}
	case *leasesv1.MsgEndLease:
		if m.GetAuthorityEnd() {
			return ObjectKey{}, fmt.Errorf("%w: EndLease with authority_end", ErrNotRouted)
		}
		key = ObjectKey{EventType: "svn.events.v1.LeaseEnded", Module: leases.ModuleName, IDKey: "lease_id", ID: m.GetLeaseId()}
	default:
		if msg == nil {
			return ObjectKey{}, fmt.Errorf("%w: nil message", ErrNotRouted)
		}
		return ObjectKey{}, fmt.Errorf("%w: %s", ErrNotRouted, sdk.MsgTypeURL(msg))
	}
	if !msgkit.ValidID(key.ID) {
		return ObjectKey{}, fmt.Errorf("chain: %s object id %q is outside the protocol id pattern", key.EventType, clip(key.ID))
	}
	key.TypeURL = sdk.MsgTypeURL(msg)
	return key, nil
}

// Query is the tx_search query for the first commitments of the object's
// kind: one condition on the object id. Every other field, the version
// included, is checked on the decoded result, so the query can only
// return too much, never too little.
func (k ObjectKey) Query() string {
	return fmt.Sprintf("%s.%s='%s'", k.EventType, k.IDKey, k.ID)
}

// matches reports whether a typed event names the key's object.
func (k ObjectKey) matches(ev proto.Message) bool {
	switch e := ev.(type) {
	case *eventsv1.ContentRegistered:
		return k.EventType == "svn.events.v1.ContentRegistered" && e.GetContentId() == k.ID && e.GetContentVersion() == k.Version
	case *eventsv1.ContentWithdrawn:
		return k.EventType == "svn.events.v1.ContentWithdrawn" && e.GetContentId() == k.ID && e.GetContentVersion() == k.Version
	case *eventsv1.LeaseOffered:
		return k.EventType == "svn.events.v1.LeaseOffered" && e.GetOfferId() == k.ID
	case *eventsv1.LeaseAccepted:
		return k.EventType == "svn.events.v1.LeaseAccepted" && e.GetLeaseId() == k.ID
	case *eventsv1.LeaseRenewed:
		return k.EventType == "svn.events.v1.LeaseRenewed" && e.GetLeaseId() == k.ID && e.GetTermVersion() == k.Version
	case *eventsv1.LeaseEnded:
		return k.EventType == "svn.events.v1.LeaseEnded" && e.GetLeaseId() == k.ID
	}
	return false
}

// HashOwner says whose stored attempt a committing transaction's hash is.
type HashOwner uint8

// Hash owners.
const (
	// HashForeign: no attempt the broadcaster stored has this hash.
	HashForeign HashOwner = iota + 1
	// HashThisIntent: one of this intent's attempts.
	HashThisIntent
	// HashOtherIntent: an attempt of another intent of this broadcaster.
	HashOtherIntent
)

// EffectVerdict is the result of comparing one committed transaction with
// an intended message (plan 8.6 step 3).
type EffectVerdict uint8

// Effect verdicts.
const (
	// EffectUnrelated: the transaction's typed event of the kind names
	// another object (another content version, another lease term).
	EffectUnrelated EffectVerdict = iota + 1
	// EffectCommitted: the intended effect exists. OwnTransaction says
	// whether this intent's attempt committed it.
	EffectCommitted
	// EffectSignerConflict: a pinned key of the broadcaster signed a
	// committed transaction the broadcaster never stored (DR-11-15). The
	// signer halts; the intent does not change.
	EffectSignerConflict
	// EffectObjectTaken: the object was committed by another account (the
	// signer rule) or with other business fields.
	EffectObjectTaken
	// EffectStateChanged: a transaction of another message type changed
	// the object (a council force-withdraw inside MsgExecuteChange). Never
	// a commitment of the intent.
	EffectStateChanged
)

func (v EffectVerdict) String() string {
	switch v {
	case EffectUnrelated:
		return "UNRELATED"
	case EffectCommitted:
		return "COMMITTED"
	case EffectSignerConflict:
		return "SIGNER_CONFLICT"
	case EffectObjectTaken:
		return "OBJECT_TAKEN"
	case EffectStateChanged:
		return "OBJECT_STATE_CHANGED"
	}
	return "EffectVerdict(" + strconv.Itoa(int(v)) + ")"
}

// Effect is the comparison of one committed transaction with an intended
// message.
type Effect struct {
	Verdict        EffectVerdict
	OwnTransaction bool
	// Field is, for EffectObjectTaken, "signer" or the first business field
	// that differs.
	Field string
	// Signer and TypeURL are the committing message's signer and type.
	Signer  []byte
	TypeURL string
	Hash    [32]byte
	Height  int64
}

// fieldSigner and fieldBusinessID are the message fields the business
// comparison skips: the signer is judged by the signer rule, and the
// business id is drawn by the broadcaster per business attempt (plan 8.2,
// 8.6 step 5), so a second intent with the same effect has its own
// (DR-11-28).
const (
	fieldSigner     = "signer"
	fieldBusinessID = "business_id"
)

// CompareEffect compares the committed transaction com, found by the
// object key's typed event search, with the intended message (plan 8.6
// step 3). pinned is the intent's pinned outer signer: the service address
// of an offer, register or withdraw, the BROADCASTER address of a relay.
// owner says whose stored attempt com's hash is.
//
// The committing message is the one at the typed event's msg_index. Its
// business fields (every field but signer and business_id) must equal the
// intended message's. The signer rule:
//   - a signer-bound message (offer, register, withdraw) committed by
//     another account is EffectObjectTaken;
//   - a transaction signed by the pinned key whose hash the broadcaster
//     never stored is EffectSignerConflict, for every message type;
//   - a relay message committed by another account with equal fields is
//     EffectCommitted, not owned.
//
// A hash of this intent's attempt that is signed by another key or
// carries other fields is an IntegrityError.
func (c *Codec) CompareEffect(intended sdk.Msg, pinned []byte, com Committed, owner HashOwner) (Effect, error) {
	key, err := ObjectOf(intended)
	if err != nil {
		return Effect{}, err
	}
	if len(pinned) != genesis.AccountBytes {
		return Effect{}, fmt.Errorf("chain: pinned signer must be %d bytes", genesis.AccountBytes)
	}
	if owner != HashForeign && owner != HashThisIntent && owner != HashOtherIntent {
		return Effect{}, fmt.Errorf("chain: unknown hash owner %d", owner)
	}
	if com.Code != 0 {
		return Effect{}, fmt.Errorf("%w: typed event search returned failed transaction %X", ErrResponse, com.Hash)
	}
	events, err := TypedEvents(com.Events)
	if err != nil {
		return Effect{}, err
	}
	found, index := false, uint32(0)
	for _, ev := range events {
		if ev.Type == key.EventType && key.matches(ev.Msg) {
			if found {
				return Effect{}, &IntegrityError{Reason: fmt.Sprintf("%X emits %s twice for one object", com.Hash, key.EventType)}
			}
			found, index = true, ev.MsgIndex
		}
	}
	out := Effect{Hash: com.Hash, Height: com.Height}
	if !found {
		out.Verdict = EffectUnrelated
		return out, nil
	}
	decoded, err := c.Decode(com.Raw)
	if err != nil {
		return Effect{}, &IntegrityError{Reason: fmt.Sprintf("committed %X does not decode: %v", com.Hash, err)}
	}
	if int(index) >= len(decoded.Msgs) {
		return Effect{}, &IntegrityError{Reason: fmt.Sprintf("%X event msg_index %d for %d messages", com.Hash, index, len(decoded.Msgs))}
	}
	committed := decoded.Msgs[index]
	out.Signer, out.TypeURL = committed.Signer, committed.TypeURL
	if committed.TypeURL != key.TypeURL {
		out.Verdict = EffectStateChanged
		return out, nil
	}
	ourKey := string(committed.Signer) == string(pinned)
	if owner == HashThisIntent && !ourKey {
		return Effect{}, &IntegrityError{Reason: fmt.Sprintf("attempt %X is signed by another key than the pinned signer", com.Hash)}
	}
	if ourKey && owner == HashForeign {
		out.Verdict = EffectSignerConflict
		return out, nil
	}
	if key.SignerBound && !ourKey {
		out.Verdict, out.Field = EffectObjectTaken, fieldSigner
		return out, nil
	}
	field, err := firstDifference(intended, committed.Msg)
	if err != nil {
		return Effect{}, err
	}
	if field != "" {
		if owner == HashThisIntent {
			return Effect{}, &IntegrityError{Reason: fmt.Sprintf("attempt %X carries another %s than the intent", com.Hash, field)}
		}
		out.Verdict, out.Field = EffectObjectTaken, field
		return out, nil
	}
	out.Verdict, out.OwnTransaction = EffectCommitted, owner == HashThisIntent
	return out, nil
}

// firstDifference returns the name of the first field, in field-number
// order, whose presence or value differs between two messages of the same
// type, skipping signer and business_id; "" when every other field is
// equal.
func firstDifference(a, b sdk.Msg) (string, error) {
	av, aok := a.(proto.Message)
	bv, bok := b.(proto.Message)
	if !aok || !bok {
		return "", errors.New("chain: messages are not protobuf messages")
	}
	am, bm := av.ProtoReflect(), bv.ProtoReflect()
	if am.Descriptor().FullName() != bm.Descriptor().FullName() {
		return "", fmt.Errorf("chain: comparing %s with %s", am.Descriptor().FullName(), bm.Descriptor().FullName())
	}
	fields := am.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if name := string(fd.Name()); name == fieldSigner || name == fieldBusinessID {
			continue
		}
		if am.Has(fd) != bm.Has(fd) || !valueEqual(fd, am.Get(fd), bm.Get(fd)) {
			return string(fd.Name()), nil
		}
	}
	if len(am.GetUnknown()) != 0 || len(bm.GetUnknown()) != 0 {
		return "", errors.New("chain: message carries unknown fields")
	}
	return "", nil
}

// valueEqual compares one field's values; message values with
// proto.Equal, scalars and bytes with protoreflect.Value.Equal.
func valueEqual(fd protoreflect.FieldDescriptor, a, b protoreflect.Value) bool {
	if fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap() {
		return proto.Equal(a.Message().Interface(), b.Message().Interface())
	}
	return a.Equal(b)
}

// EffectCandidates returns the committed transactions whose typed event of
// the intended message's kind names the object's id (ObjectKey.Query),
// reading at most pageBudget pages. Each is then judged by CompareEffect.
func (c *Client) EffectCandidates(ctx context.Context, intended sdk.Msg, pageBudget int) ([]Committed, error) {
	key, err := ObjectOf(intended)
	if err != nil {
		return nil, err
	}
	return c.TxSearch(ctx, key.Query(), pageBudget)
}
