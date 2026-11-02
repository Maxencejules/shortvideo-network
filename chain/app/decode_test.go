package app_test

import (
	"sort"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
)

// protocolMsgs lists the input message of every method of every svn.*.v1.Msg
// service in the frozen protocol, in name order.
func protocolMsgs(t *testing.T) []protoreflect.MessageDescriptor {
	t.Helper()
	var out []protoreflect.MessageDescriptor
	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(file.Package()), "svn.") {
			return true
		}
		services := file.Services()
		for i := 0; i < services.Len(); i++ {
			if services.Get(i).Name() != "Msg" {
				continue
			}
			methods := services.Get(i).Methods()
			for j := 0; j < methods.Len(); j++ {
				out = append(out, methods.Get(j).Input())
			}
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].FullName() < out[j].FullName() })
	return out
}

// reachable adds every message type that can appear inside desc, oneof
// alternatives included.
func reachable(desc protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) {
	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		if inner := fields.Get(i).Message(); inner != nil && !seen[inner.FullName()] {
			seen[inner.FullName()] = true
			reachable(inner, seen)
		}
	}
}

// populate sets every field of m: strings "x1", integers 1, booleans true,
// bytes 32 bytes of 0x05, enums their first nonzero value, nested messages
// recursively, repeated fields one element. In each oneof it sets only the
// alternative pick selects (pick modulo the alternatives). used collects the
// message types it set.
func populate(m protoreflect.Message, pick int, used map[protoreflect.FullName]bool) {
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if oneof := fd.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
			if oneof.Fields().Get(pick%oneof.Fields().Len()) != fd {
				continue
			}
		}
		if fd.IsMap() {
			panic("populate: map fields are not in the protocol")
		}
		value := func() protoreflect.Value {
			switch fd.Kind() {
			case protoreflect.MessageKind:
				inner := m.NewField(fd)
				if fd.IsList() {
					inner = protoreflect.ValueOfMessage(m.Get(fd).List().NewElement().Message())
				}
				used[fd.Message().FullName()] = true
				populate(inner.Message(), pick, used)
				return inner
			case protoreflect.StringKind:
				return protoreflect.ValueOfString("x1")
			case protoreflect.BytesKind:
				raw := make([]byte, 32)
				for j := range raw {
					raw[j] = 5
				}
				return protoreflect.ValueOfBytes(raw)
			case protoreflect.BoolKind:
				return protoreflect.ValueOfBool(true)
			case protoreflect.EnumKind:
				values := fd.Enum().Values()
				return protoreflect.ValueOfEnum(values.Get(min(1, values.Len()-1)).Number())
			case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
				return protoreflect.ValueOfInt32(1)
			case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
				return protoreflect.ValueOfInt64(1)
			case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
				return protoreflect.ValueOfUint32(1)
			case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
				return protoreflect.ValueOfUint64(1)
			default:
				panic("populate: field kind " + fd.Kind().String())
			}
		}
		if fd.IsList() {
			m.Mutable(fd).List().Append(value())
			continue
		}
		m.Set(fd, value())
	}
}

// TestEveryProtocolMessageDecodes sends every protocol message with every
// field set, nested and repeated messages included, in enough variants to
// set every oneof alternative that is a message. The admitted service account
// signs each one. Each transaction must decode and reach its handler, which
// returns its exact svn code:
//   - authority: propose and approve UNAUTHORIZED (a service account is not a
//     seat), execute FORBIDDEN (it is not the authority executor);
//   - x/providers (Goal 07): register FORBIDDEN (the service account is not
//     an ADMITTED_PROVIDER), update, rotate and drain NOT_FOUND (no provider
//     "x1" exists);
//   - x/content (Goal 07): register and withdraw BOUNDS (the populated ids,
//     digests and sizes are outside the content bounds);
//   - leases and evidence: ERROR_CODE_FORBIDDEN from the router until Goals
//     08-09.
//
// The full authority store and the full non-account state are byte-identical
// after each (council.reject). Fails without the EvidenceItem registration: a
// MsgSubmitAuditBatch with one item fails decoding with codespace sdk, code 2.
func TestEveryProtocolMessageDecodes(t *testing.T) {
	c := newCouncil(t, nil)
	service := c.keys.service
	msgs := protocolMsgs(t)
	if len(msgs) != 14 {
		t.Fatalf("%d protocol messages, want 14", len(msgs))
	}
	// chain/README.md and the App comment say policy has no message service;
	// limits change only through a council governed-limit payload.
	policyTx, err := protoregistry.GlobalFiles.FindFileByPath("svn/policy/v1/tx.proto")
	if err != nil || policyTx.Services().Len() != 0 {
		t.Fatalf("svn/policy/v1/tx.proto: %v services, %v", policyTx, err)
	}
	for _, desc := range msgs {
		if desc.ParentFile().Package() == "svn.policy.v1" {
			t.Fatalf("%s is a policy message", desc.FullName())
		}
	}
	want := map[protoreflect.FullName]bool{}
	used := map[protoreflect.FullName]bool{}
	sent := 0
	for _, desc := range msgs {
		reachable(desc, want)
		code := uint32(2)
		protoCode := authority.CodeForbidden
		switch desc.FullName() {
		case "svn.authority.v1.MsgProposeChange", "svn.authority.v1.MsgApproveChange":
			code, protoCode = 1, authority.CodeUnauthorized
		case "svn.providers.v1.MsgUpdateProvider", "svn.providers.v1.MsgRotateProviderKey", "svn.providers.v1.MsgDrainProvider":
			code, protoCode = 16, authority.CodeNotFound
		case "svn.content.v1.MsgRegisterContent", "svn.content.v1.MsgWithdrawContent":
			code, protoCode = 19, authority.CodeBounds
		}
		var variants [][]byte
		for pick := 0; pick < 8; pick++ {
			typ, err := protoregistry.GlobalTypes.FindMessageByName(desc.FullName())
			if err != nil {
				t.Fatal(err)
			}
			msg := typ.New()
			populate(msg, pick, used)
			msg.Set(desc.Fields().ByName("signer"), protoreflect.ValueOfBytes(service.PubKey().Address()))
			raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg.Interface())
			if err != nil {
				t.Fatal(err)
			}
			duplicate := false
			for _, prev := range variants {
				duplicate = duplicate || string(prev) == string(raw)
			}
			if duplicate {
				continue
			}
			variants = append(variants, raw)
			sdkMsg, ok := msg.Interface().(sdk.Msg)
			if !ok {
				t.Fatalf("%s is not an sdk.Msg", desc.FullName())
			}
			c.reject(t, rejectRow{name: string(desc.FullName()), key: service, msg: sdkMsg,
				space: "svn", code: code, proto: protoCode, advances: true})
			sent++
		}
	}
	for name := range want {
		if !used[name] {
			t.Errorf("no variant set a %s", name)
		}
	}
	if sent != 19 {
		t.Fatalf("%d transactions, want 19", sent)
	}
	t.Logf("%d transactions, %d nested message types", sent, len(want))
}
