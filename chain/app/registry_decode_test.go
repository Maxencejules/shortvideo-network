package app_test

import (
	"bytes"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
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
// message type is registered or routed.
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

// withUnknown appends field 99 carrying payload to m's unknown fields.
func withUnknown(m proto.Message, payload []byte) {
	r := m.ProtoReflect()
	extra := protowire.AppendTag(nil, 99, protowire.BytesType)
	extra = protowire.AppendBytes(extra, payload)
	r.SetUnknown(append(r.GetUnknown(), extra...))
}

// TestTransactionsRejectVideoBytes: a transaction cannot carry media bytes.
// Unknown fields fail decode wherever they sit; a digest value longer than
// 32 bytes is BOUNDS; an oversized transaction is BOUNDS in the ante
// handler. State is unchanged in every case.
func TestTransactionsRejectVideoBytes(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	unknownCases := []struct {
		name  string
		build func() *contentv1.MsgRegisterContent
	}{
		{"top level", func() *contentv1.MsgRegisterContent {
			msg := n.registerContent(t, n.service, "reg-v1", "v1", 1, packageManifest("v1"), sizeC1)
			withUnknown(msg, canaryMedia)
			return msg
		}},
		{"inside hosting_intent", func() *contentv1.MsgRegisterContent {
			msg := n.registerContent(t, n.service, "reg-v2", "v2", 1, packageManifest("v2"), sizeC1)
			withUnknown(msg.HostingIntent, canaryMedia)
			return msg
		}},
		{"inside manifest_digest", func() *contentv1.MsgRegisterContent {
			msg := n.registerContent(t, n.service, "reg-v3", "v3", 1, packageManifest("v3"), sizeC1)
			withUnknown(msg.ManifestDigest, canaryMedia)
			return msg
		}},
	}
	for _, tc := range unknownCases {
		raw := n.signed(t, c, n.service, registryGas, nil, tc.build())
		if !bytes.Contains(raw, canaryMedia) {
			t.Fatalf("%s: the media payload is not in the signed bytes; the case would be vacuous", tc.name)
		}
		before := chainApp.FullSnapshot()
		res := n.deliverRaw(t, c, n.service, raw)
		wantDecodeError(t, tc.name, res.Codespace, res.Code)
		unchanged(t, tc.name, chainApp, before, res)
	}

	before := chainApp.FullSnapshot()
	msg := n.registerContent(t, n.service, "reg-v4", "v4", 1, packageManifest("v4"), sizeC1)
	msg.ManifestDigest.Value = append([]byte(nil), canaryMedia[:1024]...)
	res := n.send(t, c, n.service, msg)
	wantCode(t, "1 KiB digest value", res, msgkit.Bounds)
	unchanged(t, "1 KiB digest value", chainApp, before, res)

	before = chainApp.FullSnapshot()
	msg = n.registerContent(t, n.service, "reg-v5", "v5", 1, packageManifest("v5"), sizeC1)
	msg.ContentId = strings.Repeat("v", 70*1024)
	res = n.send(t, c, n.service, msg)
	wantCode(t, "70 KiB transaction", res, msgkit.Bounds)
	if anteCommitted(res) {
		t.Fatal("the oversized transaction passed the ante handler")
	}
	unchanged(t, "70 KiB transaction", chainApp, before, res)

	// A digest and a byte count are accepted.
	wantCode(t, "digest and length", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-v6", "v6", 1, packageManifest("v6"), sizeC1)), msgkit.OK)
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

func TestMigrationsNoOpInApp(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runRegistryFlow(t, n, c)
	chainApp := c.app()
	from := chainApp.VersionMapForTest()
	if from[app.ModuleProviders] != providers.ConsensusVersion || from[app.ModuleContent] != content.ConsensusVersion || providers.ConsensusVersion != 1 || content.ConsensusVersion != 1 {
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
