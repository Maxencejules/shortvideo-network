package app_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// directAdminTx is an unsigned envelope with one message under typeURL.
// Decoding fails before any signature or ante check.
func directAdminTx(t *testing.T, typeURL string, body proto.Message) []byte {
	t.Helper()
	value, err := proto.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := gogoproto.Marshal(&txtypes.Tx{
		Body:     &txtypes.TxBody{Messages: []*codectypes.Any{{TypeUrl: typeURL, Value: value}}},
		AuthInfo: &txtypes.AuthInfo{Fee: &txtypes.Fee{GasLimit: 200000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func wantDecodeError(t *testing.T, label string, codespace string, code uint32) {
	t.Helper()
	if codespace != sdkerrors.RootCodespace || code != sdkerrors.ErrTxDecode.ABCICode() {
		t.Fatalf("%s: got %s/%d want sdk/%d (tx decode)", label, codespace, code, sdkerrors.ErrTxDecode.ABCICode())
	}
}

// TestDirectAdminMessagesDoNotExist: admission, disablement, and forced
// withdrawal exist only as council payloads (DEC-009). No single-signer
// message type is registered or routed. Such a transaction is the SDK
// decode error (codespace sdk, code 2), not the protocol table's
// ERROR_CODE_FORBIDDEN (codespace svn, code 2): a recorded deviation
// (chain/x/providers/README.md, chain/x/content/README.md).
func TestDirectAdminMessagesDoNotExist(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	for _, typeURL := range []string{
		"/svn.providers.v1.MsgAdmitProvider",
		"/svn.providers.v1.MsgDisableProvider",
		"/svn.content.v1.MsgForceWithdraw",
		"/svn.authority.v1.AdmitProviderChange",
	} {
		if chainApp.MsgServiceRouter().HandlerByTypeURL(typeURL) != nil {
			t.Fatalf("%s has a message handler", typeURL)
		}
		raw := directAdminTx(t, typeURL, &providersv1.MsgRegisterProvider{Signer: n.authority.addr, ProviderId: "p1"})
		checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatal(err)
		}
		wantDecodeError(t, typeURL+" check", checked.Codespace, checked.Code)
		before := chainApp.FullSnapshot()
		res := n.deliverRaw(t, c, nil, raw)
		wantDecodeError(t, typeURL+" deliver", res.Codespace, res.Code)
		unchanged(t, typeURL, chainApp, before, res)
	}
	for _, typeURL := range []string{
		"/svn.providers.v1.MsgRegisterProvider",
		"/svn.providers.v1.MsgUpdateProvider",
		"/svn.providers.v1.MsgRotateProviderKey",
		"/svn.providers.v1.MsgDrainProvider",
		"/svn.content.v1.MsgRegisterContent",
		"/svn.content.v1.MsgWithdrawContent",
	} {
		if chainApp.MsgServiceRouter().HandlerByTypeURL(typeURL) == nil {
			t.Fatalf("%s has no handler", typeURL)
		}
	}
}

// TestInnerSignatureMessagesDecode is the regression for the goal-06 tree,
// where svn.types.v1.InnerSignature was missing from the gogo registry and
// every message carrying one failed to decode.
func TestInnerSignatureMessagesDecode(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	msgs := []struct {
		who *actor
		msg sdk.Msg
	}{
		{n.provider1, n.rotate(t, n.provider1, "rot-p1", "p1", 1, newSigningKey("p1-key-2"))},
		{n.provider1, n.drain(t, n.provider1, "drain-p1", "p1", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)},
		{n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)},
		{n.service, n.withdrawContent(t, n.service, "wd-c1", "c1", 1, reasonDigest("c1"))},
	}
	decode := chainApp.EncodingConfig().TxConfig.TxDecoder()
	// CheckTx advances the check-state sequence; it resets at Commit.
	checkSeq := map[string]uint64{}
	for _, tc := range msgs {
		seq := n.seq[tc.who.hexAddr()] + checkSeq[tc.who.hexAddr()]
		raw := signFull(t, chainApp, n.doc.ChainID, tc.who.key, n.numbers[tc.who.hexAddr()], seq, registryGas, nil, tc.msg)
		decoded, err := decode(raw)
		if err != nil {
			t.Fatalf("%T: %v", tc.msg, err)
		}
		got, ok := decoded.GetMsgs()[0].(proto.Message)
		if !ok || !proto.Equal(got, tc.msg.(proto.Message)) {
			t.Fatalf("%T: decoded message differs", tc.msg)
		}
		checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatal(err)
		}
		if checked.Code != 0 {
			t.Fatalf("%T: check tx %s/%d %s", tc.msg, checked.Codespace, checked.Code, checked.Log)
		}
		checkSeq[tc.who.hexAddr()]++
	}
	// CheckTx runs no handler. In a block, the drain reaches its handler and
	// fails there with CONFLICT because p1 is APPLIED; the other three commit.
	wantCode(t, "drain", n.send(t, c, n.provider1, msgs[1].msg), msgkit.Conflict)
	wantCode(t, "rotate", n.send(t, c, n.provider1, msgs[0].msg), msgkit.OK)
	wantCode(t, "register content", n.send(t, c, n.service, msgs[2].msg), msgkit.OK)
	wantCode(t, "withdraw content", n.send(t, c, n.service, msgs[3].msg), msgkit.OK)
}

// withUnknown appends field num carrying payload to m's unknown fields.
// Field 99 is a critical number. 1024 and 2047 have bit 0x400 set, the
// SDK's "non-critical" range, which its default decoder lets through the
// transaction body.
func withUnknown(m proto.Message, num protowire.Number, payload []byte) {
	r := m.ProtoReflect()
	extra := protowire.AppendTag(nil, num, protowire.BytesType)
	extra = protowire.AppendBytes(extra, payload)
	r.SetUnknown(append(r.GetUnknown(), extra...))
}

// signEnvelope signs a transaction built field by field, SIGN_MODE_DIRECT
// over the final body and auth info bytes, so a test can set envelope
// fields the TxBuilder does not expose and append raw bytes to the body.
// With no edit and no tail it is an ordinary valid transaction.
func (n *regNet) signEnvelope(t *testing.T, who *actor, msg sdk.Msg, edit func(body *txtypes.TxBody, auth *txtypes.AuthInfo), bodyTail []byte) []byte {
	t.Helper()
	packed, err := codectypes.NewAnyWithValue(msg)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := codectypes.NewAnyWithValue(who.key.PubKey())
	if err != nil {
		t.Fatal(err)
	}
	body := &txtypes.TxBody{Messages: []*codectypes.Any{packed}}
	auth := &txtypes.AuthInfo{
		SignerInfos: []*txtypes.SignerInfo{{
			PublicKey: pub,
			ModeInfo:  &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{Single: &txtypes.ModeInfo_Single{Mode: signingtypes.SignMode_SIGN_MODE_DIRECT}}},
			Sequence:  n.seq[who.hexAddr()],
		}},
		Fee: &txtypes.Fee{GasLimit: registryGas},
	}
	if edit != nil {
		edit(body, auth)
	}
	bodyBytes, err := gogoproto.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes = append(bodyBytes, bodyTail...)
	authBytes, err := gogoproto.Marshal(auth)
	if err != nil {
		t.Fatal(err)
	}
	signDoc, err := gogoproto.Marshal(&txtypes.SignDoc{
		BodyBytes: bodyBytes, AuthInfoBytes: authBytes, ChainId: n.doc.ChainID, AccountNumber: n.numbers[who.hexAddr()],
	})
	if err != nil {
		t.Fatal(err)
	}
	signature, err := who.key.Sign(signDoc)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := gogoproto.Marshal(&txtypes.TxRaw{BodyBytes: bodyBytes, AuthInfoBytes: authBytes, Signatures: [][]byte{signature}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// mediaPayload is 40 KiB of synthetic media, small enough that a
// transaction carrying it stays under the 64 KiB transaction bound.
var mediaPayload = bytes.Repeat(canaryMedia, 10)

// TestTransactionsRejectVideoBytes (REQ-PROD-010, REQ-OFF-001): a
// transaction cannot carry media bytes or free text in a field the chain
// does not read. Unknown fields fail decode wherever they sit, critical
// (99) or non-critical (1024, 2047), at the message top level, inside a
// Digest, inside an InnerSignature, and in the transaction body itself.
// Extension options fail decode. A memo is BOUNDS; a fee payer, a fee
// granter, or a tip is FEE_REJECTED. A digest value longer than 32 bytes is
// BOUNDS; an oversized transaction is BOUNDS in the ante handler. State is
// byte-identical after every rejection, and the payload was in the signed
// bytes each time, so no case is vacuous.
func TestTransactionsRejectVideoBytes(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)

	// The envelope helper builds valid transactions: the rejections below
	// come from the rule each case names, not from a broken signature.
	wantCode(t, "envelope baseline", n.deliverRaw(t, c, n.service, n.signEnvelope(t, n.service,
		n.registerContent(t, n.service, "reg-base", "base", 1, packageManifest("base"), sizeC1), nil, nil)), msgkit.OK)

	register := func(id string) *contentv1.MsgRegisterContent {
		return n.registerContent(t, n.service, "reg-"+id, id, 1, packageManifest(id), sizeC1)
	}
	type rejectCase struct {
		name    string
		who     *actor
		raw     func() []byte
		payload []byte
		sdkCode uint32 // codespace sdk, or 0 for a protocol code
		svn     msgkit.Code
	}
	decode := sdkerrors.ErrTxDecode.ABCICode()
	unknownIn := func(id string, num protowire.Number, where func(m *contentv1.MsgRegisterContent) proto.Message) func() []byte {
		return func() []byte {
			msg := register(id)
			withUnknown(where(msg), num, mediaPayload)
			return n.signEnvelope(t, n.service, msg, nil, nil)
		}
	}
	top := func(m *contentv1.MsgRegisterContent) proto.Message { return m }
	digest := func(m *contentv1.MsgRegisterContent) proto.Message { return m.ManifestDigest }
	intent := func(m *contentv1.MsgRegisterContent) proto.Message { return m.HostingIntent }
	bodyField := func(num protowire.Number) []byte {
		tail := protowire.AppendTag(nil, num, protowire.BytesType)
		return protowire.AppendBytes(tail, mediaPayload)
	}
	extension := func(id string) *codectypes.Any {
		// A registered type URL, so the Any resolves and the case tests the
		// extension rule, not an unknown type.
		packed, err := codectypes.NewAnyWithValue(register(id))
		if err != nil {
			t.Fatal(err)
		}
		packed.Value = append(packed.Value, bodyField(1024)...)
		return packed
	}
	memoText := canaryEmail + " " + canaryEndpoint
	cases := []rejectCase{
		{"critical field 99 at top level", n.service, unknownIn("v1", 99, top), mediaPayload, decode, ""},
		{"non-critical field 1024 at top level", n.service, unknownIn("v2", 1024, top), mediaPayload, decode, ""},
		{"non-critical field 2047 at top level", n.service, unknownIn("v3", 2047, top), mediaPayload, decode, ""},
		{"critical field 99 inside manifest_digest", n.service, unknownIn("v4", 99, digest), mediaPayload, decode, ""},
		{"non-critical field 1024 inside manifest_digest", n.service, unknownIn("v5", 1024, digest), mediaPayload, decode, ""},
		{"critical field 99 inside hosting_intent", n.service, unknownIn("v6", 99, intent), mediaPayload, decode, ""},
		{"non-critical field 2047 inside hosting_intent", n.service, unknownIn("v7", 2047, intent), mediaPayload, decode, ""},
		{"non-critical field 2047 inside endpoint_manifest_digest", n.provider1, func() []byte {
			msg := n.updateProvider(t, n.provider1, "upd-v8", "p1", capacity2, "v8")
			withUnknown(msg.EndpointManifestDigest, 2047, mediaPayload)
			return n.signEnvelope(t, n.provider1, msg, nil, nil)
		}, mediaPayload, decode, ""},
		{"non-critical field 1024 in the tx body", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v9"), nil, bodyField(1024))
		}, mediaPayload, decode, ""},
		{"non_critical_extension_options", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v10"), func(body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.NonCriticalExtensionOptions = []*codectypes.Any{extension("v10x")}
			}, nil)
		}, mediaPayload, decode, ""},
		{"extension_options", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v11"), func(body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.ExtensionOptions = []*codectypes.Any{extension("v11x")}
			}, nil)
		}, mediaPayload, decode, ""},
		{"memo with an email and a URL", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v12"), func(body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.Memo = memoText
			}, nil)
		}, []byte(memoText), 0, msgkit.Bounds},
		{"256-character memo of media", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v13"), func(body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.Memo = string(canaryMedia[:256])
			}, nil)
		}, canaryMedia[:256], 0, msgkit.Bounds},
		{"fee granter text", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v14"), func(_ *txtypes.TxBody, auth *txtypes.AuthInfo) {
				auth.Fee.Granter = canaryEmail
			}, nil)
		}, []byte(canaryEmail), 0, msgkit.FeeRejected},
		{"fee payer equal to the signer", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v15"), func(_ *txtypes.TxBody, auth *txtypes.AuthInfo) {
				auth.Fee.Payer = sdk.AccAddress(n.service.addr).String()
			}, nil)
		}, []byte(sdk.AccAddress(n.service.addr).String()), 0, msgkit.FeeRejected},
		{"tip with a tipper", n.service, func() []byte {
			return n.signEnvelope(t, n.service, register("v16"), func(_ *txtypes.TxBody, auth *txtypes.AuthInfo) {
				auth.Tip = &txtypes.Tip{Tipper: canaryUser} //nolint:staticcheck // the deprecated tip must be rejected
			}, nil)
		}, []byte(canaryUser), 0, msgkit.FeeRejected},
	}
	for _, tc := range cases {
		raw := tc.raw()
		if !bytes.Contains(raw, tc.payload) {
			t.Fatalf("%s: the payload is not in the signed bytes; the case would be vacuous", tc.name)
		}
		before := chainApp.FullSnapshot()
		accBefore := chainApp.ModuleSnapshot("acc")
		checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatal(err)
		}
		res := n.deliverRaw(t, c, tc.who, raw)
		for label, got := range map[string]struct {
			codespace string
			code      uint32
		}{"check": {checked.Codespace, checked.Code}, "deliver": {res.Codespace, res.Code}} {
			if tc.sdkCode != 0 {
				if got.codespace != sdkerrors.RootCodespace || got.code != tc.sdkCode {
					t.Fatalf("%s %s: got %s/%d want sdk/%d", tc.name, label, got.codespace, got.code, tc.sdkCode)
				}
				continue
			}
			number := uint32(typesv1.ErrorCode_value[string(tc.svn)])
			if got.codespace != "svn" || got.code != number {
				t.Fatalf("%s %s: got %s/%d want svn/%d (%s)", tc.name, label, got.codespace, got.code, number, tc.svn)
			}
		}
		if anteCommitted(res) {
			t.Fatalf("%s: the ante handler committed", tc.name)
		}
		unchanged(t, tc.name, chainApp, before, res)
		if !bytes.Equal(accBefore, chainApp.ModuleSnapshot("acc")) {
			t.Fatalf("%s: account store changed", tc.name)
		}
	}

	before := chainApp.FullSnapshot()
	msg := register("v20")
	msg.ManifestDigest.Value = append([]byte(nil), canaryMedia[:1024]...)
	res := n.send(t, c, n.service, msg)
	wantCode(t, "1 KiB digest value", res, msgkit.Bounds)
	unchanged(t, "1 KiB digest value", chainApp, before, res)

	before = chainApp.FullSnapshot()
	msg = register("v21")
	msg.ContentId = strings.Repeat("v", 70*1024)
	res = n.send(t, c, n.service, msg)
	wantCode(t, "70 KiB transaction", res, msgkit.Bounds)
	if anteCommitted(res) {
		t.Fatal("the oversized transaction passed the ante handler")
	}
	unchanged(t, "70 KiB transaction", chainApp, before, res)

	// A digest and a byte count are accepted.
	wantCode(t, "digest and length", n.send(t, c, n.service, register("v22")), msgkit.OK)
}

// TestStateSchemaHasNoPayloadField walks the stored registry messages by
// reflection. Every bytes and string field must have a bound that genesis
// validation enforces; a new unbounded field fails the walk.
func TestStateSchemaHasNoPayloadField(t *testing.T) {
	bounded := map[string]func(t *testing.T){}
	provBase := func() *providersv1.GenesisState {
		return &providersv1.GenesisState{Providers: []*providersv1.Provider{{
			ProviderId:    "p1",
			Account:       bytes20(1),
			SigningPubkey: bytes32(2),
			SigningKeyId:  "k1",
			KeyGeneration: 2,
			PreviousSigningKeys: []*providersv1.PreviousSigningKey{{
				PublicKey: bytes32(3), Generation: 1, RetiredUnixSeconds: 1_700_000_000,
			}},
			EndpointManifestDigest: digestOf(bytes32(4)),
			State:                  providersv1.ProviderState_PROVIDER_STATE_ADMITTED,
			AdmittedCapacityBytes:  10,
			OperatorGroupId:        groupDevnet,
		}}}
	}
	contBase := func() *contentv1.GenesisState {
		return &contentv1.GenesisState{
			Contents: []*contentv1.Content{{
				ContentId: "c1", ContentVersion: 1, ManifestDigest: digestOf(bytes32(5)), PackageBytes: 10,
				SubmittingHost: bytes20(2), State: contentv1.ContentState_CONTENT_STATE_WITHDRAWN,
			}},
			Tombstones: []*contentv1.Tombstone{{
				ContentId: "c1", ContentVersion: 1, CommittedUnixSeconds: 1_700_000_000,
				ReasonDigest: digestOf(bytes32(6)), RetentionDecisionId: content.RetentionDecisionID,
			}},
		}
	}
	if err := providers.ValidateGenesis(provBase()); err != nil {
		t.Fatal(err)
	}
	if err := content.ValidateGenesis(contBase()); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 65)
	provCase := func(field string, mutate func(p *providersv1.Provider)) {
		bounded[field] = func(t *testing.T) {
			state := provBase()
			mutate(state.Providers[0])
			if err := providers.ValidateGenesis(state); err == nil {
				t.Fatalf("%s: out-of-bound value accepted", field)
			}
		}
	}
	contCase := func(field string, mutate func(s *contentv1.GenesisState)) {
		bounded[field] = func(t *testing.T) {
			state := contBase()
			mutate(state)
			if err := content.ValidateGenesis(state); err == nil {
				t.Fatalf("%s: out-of-bound value accepted", field)
			}
		}
	}
	provCase("svn.providers.v1.Provider.provider_id", func(p *providersv1.Provider) { p.ProviderId = long })
	provCase("svn.providers.v1.Provider.account", func(p *providersv1.Provider) { p.Account = bytes32(1) })
	provCase("svn.providers.v1.Provider.signing_pubkey", func(p *providersv1.Provider) { p.SigningPubkey = append(bytes32(2), 0) })
	provCase("svn.providers.v1.Provider.signing_key_id", func(p *providersv1.Provider) { p.SigningKeyId = long })
	provCase("svn.providers.v1.Provider.operator_group_id", func(p *providersv1.Provider) { p.OperatorGroupId = long })
	provCase("svn.providers.v1.Provider.previous_signing_keys.public_key", func(p *providersv1.Provider) {
		p.PreviousSigningKeys[0].PublicKey = canaryMedia[:1024]
	})
	provCase("svn.providers.v1.Provider.endpoint_manifest_digest.value", func(p *providersv1.Provider) { p.EndpointManifestDigest.Value = canaryMedia[:1024] })
	contCase("svn.content.v1.Content.content_id", func(s *contentv1.GenesisState) { s.Contents[0].ContentId = long })
	contCase("svn.content.v1.Content.submitting_host", func(s *contentv1.GenesisState) { s.Contents[0].SubmittingHost = canaryMedia[:1024] })
	contCase("svn.content.v1.Tombstone.content_id", func(s *contentv1.GenesisState) { s.Tombstones[0].ContentId = long })
	contCase("svn.content.v1.Tombstone.retention_decision_id", func(s *contentv1.GenesisState) { s.Tombstones[0].RetentionDecisionId = long })
	contCase("svn.content.v1.Content.manifest_digest.value", func(s *contentv1.GenesisState) { s.Contents[0].ManifestDigest.Value = canaryMedia[:1024] })
	contCase("svn.content.v1.Tombstone.reason_digest.value", func(s *contentv1.GenesisState) { s.Tombstones[0].ReasonDigest.Value = canaryMedia[:1024] })

	// Every bytes or string field reachable from a stored record, named by
	// its path from the record.
	found := map[string]bool{}
	var walk func(d protoreflect.MessageDescriptor, path string)
	walk = func(d protoreflect.MessageDescriptor, path string) {
		fields := d.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			name := path + "." + string(f.Name())
			if f.IsMap() {
				t.Fatalf("%s is a map", name)
			}
			switch f.Kind() {
			case protoreflect.BytesKind, protoreflect.StringKind:
				found[name] = true
			case protoreflect.MessageKind:
				if f.Message().FullName() == "google.protobuf.Any" {
					t.Fatalf("%s holds an Any", name)
				}
				walk(f.Message(), name)
			}
		}
	}
	for _, m := range []proto.Message{&providersv1.Provider{}, &contentv1.Content{}, &contentv1.Tombstone{}} {
		d := m.ProtoReflect().Descriptor()
		walk(d, string(d.FullName()))
	}
	names := sortedNames(found)
	for _, name := range names {
		check, ok := bounded[name]
		if !ok {
			t.Fatalf("%s has no tested bound", name)
		}
		t.Run(name, check)
	}
	for _, name := range sortedNames(bounded) {
		if !found[name] {
			t.Fatalf("bound for %s matches no stored field", name)
		}
	}
	if len(names) < 13 {
		t.Fatalf("walk found only %v", names)
	}
}

// TestMigrationsNoOpInApp: in the app, both registries report consensus
// version 2 (version 1 was the goal-05/06 scaffold slot), and running the
// app's migrations from that version map is a no-op. The SDK returns before it looks for a step when from equals to,
// so that half proves only that nothing else runs; the registration path is
// TestRegistryMigrationPathInApp, and the module tables are
// TestMigrationTable (x/providers) and TestRegisterMigrationsPath
// (x/content).
func TestMigrationsNoOpInApp(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runRegistryFlow(t, n, c)
	chainApp := c.app()
	from := chainApp.VersionMapForTest()
	if from[app.ModuleProviders] != providers.ConsensusVersion || from[app.ModuleContent] != content.ConsensusVersion || providers.ConsensusVersion != 2 || content.ConsensusVersion != 2 {
		t.Fatalf("version map %v", from)
	}
	before := chainApp.FullSnapshot()
	ctx := chainApp.NewUncachedContext(false, cmtproto.Header{ChainID: n.doc.ChainID, Height: c.height()})
	to, err := chainApp.RunMigrationsForTest(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedNames(from) {
		if to[name] != from[name] {
			t.Fatalf("%s moved from %d to %d", name, from[name], to[name])
		}
	}
	if len(to) != len(from) {
		t.Fatalf("version maps %v -> %v", from, to)
	}
	if !bytes.Equal(before, chainApp.FullSnapshot()) {
		t.Fatal("a no-op migration changed state")
	}
}

// TestRegistryMigrationPathInApp drives the migration path through the
// app's own configurator: a test-only 2 -> 3 step for each registry module
// registers without a duplicate (the module registered no version-2 step),
// runs exactly once on the committed stores (it reads a record the flow
// wrote), and moves the version map to 3. The step writes nothing.
func TestRegistryMigrationPathInApp(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  []byte
	}{
		{app.ModuleProviders, providers.ProviderKey("p1")},
		// content.ContentPrefix | "c1" | 0x00 | version 1 (8 bytes big endian)
		{app.ModuleContent, append(append([]byte{content.ContentPrefix}, "c1\x00"...), 0, 0, 0, 0, 0, 0, 0, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newRegNet()
			c := n.start(t, 1)
			runRegistryFlow(t, n, c)
			chainApp := c.app()
			before := chainApp.FullSnapshot()
			ctx := chainApp.NewUncachedContext(false, cmtproto.Header{ChainID: n.doc.ChainID, Height: c.height()})
			ran := 0
			to, err := chainApp.RunRegistryMigrationForTest(ctx, tc.name, func(stepCtx sdk.Context) error {
				ran++
				if stepCtx.KVStore(chainApp.StoreKeyForTest(tc.name)).Get(tc.key) == nil {
					return fmt.Errorf("the step does not see the committed record %x", tc.key)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if ran != 1 || to[tc.name] != 3 || len(to) != 1 {
				t.Fatalf("step ran %d times, version map %v", ran, to)
			}
			if !bytes.Equal(before, chainApp.FullSnapshot()) {
				t.Fatal("a step that writes nothing changed state")
			}
		})
	}
}
