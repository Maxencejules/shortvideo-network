package chain

import (
	"errors"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// objectFields are, per message, the fields that name the object: a
// candidate that changes one of them emits a typed event for another
// object and is EffectUnrelated rather than EffectObjectTaken.
var objectFields = map[string]map[string]bool{
	"register": {"content_id": true, "content_version": true},
	"withdraw": {"content_id": true, "content_version": true},
	"offer":    {"offer_id": true},
	"accept":   {"lease_id": true},
	"renew":    {"lease_id": true, "term_version": true},
	"end":      {"lease_id": true},
}

// signerBound are the messages the signer rule binds to the pinned
// service address.
var signerBound = map[string]bool{"register": true, "withdraw": true, "offer": true}

// withSigner returns a copy of msg signed by signer.
func withSigner(t *testing.T, msg sdk.Msg, signer []byte) sdk.Msg {
	t.Helper()
	m := proto.Clone(msg.(proto.Message))
	m.ProtoReflect().Set(m.ProtoReflect().Descriptor().Fields().ByName("signer"), protoreflect.ValueOfBytes(signer))
	return m.(sdk.Msg)
}

// mutated returns a copy of msg with field fd changed: strings get a
// suffix, integers +1, booleans flip, bytes change their first byte, and
// a message field changes the first byte of its first bytes field.
func mutated(t *testing.T, msg sdk.Msg, fd protoreflect.FieldDescriptor) sdk.Msg {
	t.Helper()
	m := proto.Clone(msg.(proto.Message)).ProtoReflect()
	v := m.Get(fd)
	switch fd.Kind() {
	case protoreflect.StringKind:
		m.Set(fd, protoreflect.ValueOfString(v.String()+"x"))
	case protoreflect.Uint64Kind:
		m.Set(fd, protoreflect.ValueOfUint64(v.Uint()+1))
	case protoreflect.Uint32Kind:
		m.Set(fd, protoreflect.ValueOfUint32(uint32(v.Uint())+1))
	case protoreflect.Int64Kind:
		m.Set(fd, protoreflect.ValueOfInt64(v.Int()+1))
	case protoreflect.BoolKind:
		m.Set(fd, protoreflect.ValueOfBool(!v.Bool()))
	case protoreflect.BytesKind:
		b := append([]byte(nil), v.Bytes()...)
		b[0] ^= 1
		m.Set(fd, protoreflect.ValueOfBytes(b))
	case protoreflect.MessageKind:
		inner := m.Mutable(fd).Message()
		fields := inner.Descriptor().Fields()
		for i := 0; i < fields.Len(); i++ {
			ifd := fields.Get(i)
			if ifd.Kind() == protoreflect.BytesKind {
				b := append([]byte(nil), inner.Get(ifd).Bytes()...)
				b[0] ^= 1
				inner.Set(ifd, protoreflect.ValueOfBytes(b))
				break
			}
		}
	default:
		t.Fatalf("field %s of kind %s has no mutation", fd.Name(), fd.Kind())
	}
	return m.Interface().(sdk.Msg)
}

// TestEffectComparison: per kind, the committed transaction found by the
// typed event search is compared with the intent (plan 8.6 step 3, M7).
//   - Equal fields signed by the pinned key: COMMITTED, owned only when the
//     hash is this intent's attempt (another intent's attempt: not owned,
//     DR-11-28).
//   - The pinned key with a hash the broadcaster never stored:
//     SIGNER_CONFLICT, for every kind.
//   - Another signer: OBJECT_TAKEN for an offer, a register or a withdraw;
//     COMMITTED, not owned, for a relay kind.
//   - Every business field changes the result: OBJECT_TAKEN naming the
//     field, or UNRELATED when the field names the object.
//   - The business id does not change the result.
//   - This intent's hash signed by another key or carrying other fields is
//     an IntegrityError.
func TestEffectComparison(t *testing.T) {
	c := testCodec(t)
	ours, otherSvc := testKey("pinned"), testKey("another account")
	pinned, otherAddr := ours.PubKey().Address(), otherSvc.PubKey().Address()
	sign := func(key *secp256k1.PrivKey, msg sdk.Msg, seq uint64) Committed {
		raw, _ := signed(t, c, key, msg, stdInput(seq))
		return committed(raw, 0, "", accSeqEvent(t, c, key.PubKey().Address(), seq), typedEvent(t, eventFor(t, msg), 0))
	}
	verdict := func(name string, intended sdk.Msg, com Committed, owner HashOwner, want EffectVerdict, own bool, field string) {
		t.Helper()
		got, err := c.CompareEffect(intended, pinned, com, owner)
		if err != nil || got.Verdict != want || got.OwnTransaction != own || got.Field != field || got.Hash != com.Hash {
			t.Fatalf("%s: got %s own=%t field=%q %v, want %s own=%t field=%q", name, got.Verdict, got.OwnTransaction, got.Field, err, want, own, field)
		}
	}
	integrity := func(name string, intended sdk.Msg, com Committed, owner HashOwner) {
		t.Helper()
		var ie *IntegrityError
		if _, err := c.CompareEffect(intended, pinned, com, owner); !errors.As(err, &ie) {
			t.Fatalf("%s: %v, want an IntegrityError", name, err)
		}
	}
	for _, tc := range sixMsgs(pinned) {
		t.Run(tc.name, func(t *testing.T) {
			same := sign(ours, tc.msg, 3)
			verdict("ours", tc.msg, same, HashThisIntent, EffectCommitted, true, "")
			verdict("another intent's attempt", tc.msg, same, HashOtherIntent, EffectCommitted, false, "")
			// For the relay kinds this is goal 11 Wave 0 B2 deviation 3,
			// stricter than plan 8.6 step 3, which ignores the signer for
			// relay kinds; pending the orchestrator's ruling, evidence lists
			// it as a deviation, not as a plan-conformant check.
			verdict("pinned key, foreign hash", tc.msg, same, HashForeign, EffectSignerConflict, false, "")

			byOther := sign(otherSvc, withSigner(t, tc.msg, otherAddr), 4)
			if signerBound[tc.name] {
				verdict("another account", tc.msg, byOther, HashForeign, EffectObjectTaken, false, "signer")
				verdict("another intent on another key", tc.msg, byOther, HashOtherIntent, EffectObjectTaken, false, "signer")
			} else {
				verdict("another account", tc.msg, byOther, HashForeign, EffectCommitted, false, "")
				verdict("another intent on another key", tc.msg, byOther, HashOtherIntent, EffectCommitted, false, "")
			}
			integrity("this intent's hash, another key", tc.msg, byOther, HashThisIntent)

			bid := proto.Clone(tc.msg.(proto.Message)).(sdk.Msg)
			bid.(proto.Message).ProtoReflect().Set(bid.(proto.Message).ProtoReflect().Descriptor().Fields().ByName("business_id"), protoreflect.ValueOfString("another-business-id"))
			verdict("another business id", tc.msg, sign(ours, bid, 5), HashOtherIntent, EffectCommitted, false, "")

			fields := tc.msg.(proto.Message).ProtoReflect().Descriptor().Fields()
			checked := 0
			for i := 0; i < fields.Len(); i++ {
				fd := fields.Get(i)
				name := string(fd.Name())
				if name == "signer" || name == "business_id" {
					continue
				}
				changed := mutated(t, tc.msg, fd)
				if proto.Equal(changed.(proto.Message), tc.msg.(proto.Message)) {
					t.Fatalf("mutation of %s changed nothing", name)
				}
				com := sign(ours, changed, 6)
				if objectFields[tc.name][name] {
					verdict("object field "+name, tc.msg, com, HashOtherIntent, EffectUnrelated, false, "")
					continue
				}
				verdict("business field "+name, tc.msg, com, HashOtherIntent, EffectObjectTaken, false, name)
				verdict("business field "+name+", foreign account", tc.msg, sign(otherSvc, withSigner(t, changed, otherAddr), 7), HashForeign, EffectObjectTaken, false, map[bool]string{true: "signer", false: name}[signerBound[tc.name]])
				integrity("this intent's hash, field "+name, tc.msg, com, HashThisIntent)
				checked++
			}
			if checked == 0 {
				t.Fatal("no business field checked")
			}
		})
	}
}

// TestEffectStateChangedAndRefusals: a typed event of the object emitted
// by a transaction of another message type (a council force-withdraw in
// MsgExecuteChange) is OBJECT_STATE_CHANGED; the committing message is the
// one at the event's msg_index in a multi-message transaction; an
// authority end of the lease a provider end names is OBJECT_TAKEN on the
// first business field that differs (provider_signature, absent from the
// authority end); a failed transaction, an unknown hash owner, a short
// pinned signer and an unrouted message are refused.
func TestEffectStateChangedAndRefusals(t *testing.T) {
	c := testCodec(t)
	ours, authority := testKey("pinned"), testKey("authority")
	pinned := ours.PubKey().Address()
	intended := withdrawMsg(pinned)

	exec := &authorityv1.MsgExecuteChange{Signer: authority.PubKey().Address(), BusinessId: "ex-fw-c1", ProposalId: "prop-fw-c1"}
	raw, _ := signed(t, c, authority, exec, stdInput(1))
	forced := committed(raw, 0, "", accSeqEvent(t, c, authority.PubKey().Address(), 1),
		typedEvent(t, &eventsv1.ContentWithdrawn{ContentId: "c1", ContentVersion: 1}, 0))
	got, err := c.CompareEffect(intended, pinned, forced, HashForeign)
	if err != nil || got.Verdict != EffectStateChanged || got.TypeURL != sdk.MsgTypeURL(exec) {
		t.Fatalf("force-withdraw: %+v %v", got, err)
	}

	first, second := offerMsg(pinned), offerMsg(pinned)
	first.OfferId, first.BusinessId = "o0", "of-o0"
	multi := signedMulti(t, c, ours, 2, first, second)
	com := committed(multi, 0, "", accSeqEvent(t, c, pinned, 2), typedEvent(t, eventFor(t, first), 0), typedEvent(t, eventFor(t, second), 1))
	if got, err := c.CompareEffect(offerMsg(pinned), pinned, com, HashOtherIntent); err != nil || got.Verdict != EffectCommitted {
		t.Fatalf("message at msg_index 1: %+v %v", got, err)
	}
	changed := offerMsg(pinned)
	changed.PackageBytes++
	multi = signedMulti(t, c, ours, 2, first, changed)
	com = committed(multi, 0, "", accSeqEvent(t, c, pinned, 2), typedEvent(t, eventFor(t, first), 0), typedEvent(t, eventFor(t, changed), 1))
	if got, err := c.CompareEffect(offerMsg(pinned), pinned, com, HashOtherIntent); err != nil || got.Verdict != EffectObjectTaken || got.Field != "package_bytes" {
		t.Fatalf("other fields at msg_index 1: %+v %v", got, err)
	}
	bad := committed(multi, 0, "", accSeqEvent(t, c, pinned, 2), typedEvent(t, eventFor(t, offerMsg(pinned)), 5))
	var ie *IntegrityError
	if _, err := c.CompareEffect(offerMsg(pinned), pinned, bad, HashOtherIntent); !errors.As(err, &ie) {
		t.Fatalf("msg_index beyond the messages: %v", err)
	}
	twice := committed(multi, 0, "", typedEvent(t, eventFor(t, offerMsg(pinned)), 1), typedEvent(t, eventFor(t, offerMsg(pinned)), 1))
	if _, err := c.CompareEffect(offerMsg(pinned), pinned, twice, HashOtherIntent); !errors.As(err, &ie) {
		t.Fatalf("event twice: %v", err)
	}

	authorityEnd := endMsg(authority.PubKey().Address())
	authorityEnd.AuthorityEnd, authorityEnd.ProviderSignature = true, nil
	raw, _ = signed(t, c, authority, authorityEnd, stdInput(2))
	ended := committed(raw, 0, "", accSeqEvent(t, c, authority.PubKey().Address(), 2), typedEvent(t, eventFor(t, authorityEnd), 0))
	if got, err := c.CompareEffect(endMsg(pinned), pinned, ended, HashForeign); err != nil || got.Verdict != EffectObjectTaken || got.Field != "provider_signature" {
		t.Fatalf("authority end against a provider end: %+v %v", got, err)
	}
	if _, err := c.CompareEffect(authorityEnd, pinned, ended, HashForeign); !errors.Is(err, ErrNotRouted) {
		t.Fatalf("authority end intended: %v", err)
	}

	raw, _ = signed(t, c, ours, intended, stdInput(3))
	failed := committed(raw, 5, "svn", typedEvent(t, eventFor(t, intended), 0))
	if _, err := c.CompareEffect(intended, pinned, failed, HashThisIntent); !errors.Is(err, ErrResponse) {
		t.Fatalf("failed transaction: %v", err)
	}
	okCom := committed(raw, 0, "", typedEvent(t, eventFor(t, intended), 0))
	if _, err := c.CompareEffect(intended, pinned, okCom, HashOwner(9)); err == nil {
		t.Fatal("unknown hash owner accepted")
	}
	if _, err := c.CompareEffect(intended, pinned[:19], okCom, HashThisIntent); err == nil {
		t.Fatal("19-byte pinned signer accepted")
	}
	noEvent := committed(raw, 0, "", abci.Event{Type: "message"})
	if got, err := c.CompareEffect(intended, pinned, noEvent, HashThisIntent); err != nil || got.Verdict != EffectUnrelated {
		t.Fatalf("no typed event: %+v %v", got, err)
	}
}

// TestObjectOfRefusesUnrouted: only the six content and lease messages
// have an object key; provider registry, council and audit messages and an
// authority end are not routed (DR-11-12).
func TestObjectOfRefusesUnrouted(t *testing.T) {
	signer := testKey("x").PubKey().Address()
	authorityEnd := endMsg(signer)
	authorityEnd.AuthorityEnd = true
	for _, msg := range []sdk.Msg{
		&providersv1.MsgRegisterProvider{Signer: signer}, &providersv1.MsgRotateProviderKey{Signer: signer},
		&authorityv1.MsgExecuteChange{Signer: signer}, &authorityv1.MsgProposeChange{Signer: signer},
		&evidencev1.MsgSubmitAuditBatch{Signer: signer}, authorityEnd, nil,
	} {
		if _, err := ObjectOf(msg); !errors.Is(err, ErrNotRouted) {
			t.Fatalf("%T: %v", msg, err)
		}
	}
	empty := &contentv1.MsgRegisterContent{Signer: signer}
	if _, err := ObjectOf(empty); err == nil {
		t.Fatal("empty content id accepted")
	}
	for _, tc := range sixMsgs(signer) {
		key, err := ObjectOf(tc.msg)
		if err != nil || key.TypeURL != sdk.MsgTypeURL(tc.msg) || key.SignerBound != signerBound[tc.name] {
			t.Fatalf("%s: %+v %v", tc.name, key, err)
		}
	}
}
