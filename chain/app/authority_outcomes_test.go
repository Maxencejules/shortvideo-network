package app_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestProviderMessagesReachTheRegistryAndDecode: provider messages are
// x/providers handlers (Goal 07 ended goal 06's router FORBIDDEN, D1). A
// fully populated register from the provider account commits (code 0): the
// providers store gains the record and the authority store is byte-identical.
// A signed rotation decodes (it carries an InnerSignature) and reaches the
// rotate handler, which checks the inner signature against the current key
// (op_rotate order, chain/x/providers/README.md): the garbage signature is
// ERROR_CODE_UNAUTHORIZED from the svn codespace, the authority store is
// byte-identical, and the full non-account state is unchanged.
func TestProviderMessagesReachTheRegistryAndDecode(t *testing.T) {
	provider := sdksecp.GenPrivKey()
	c := newCouncil(t, func(doc *genesis.Document) {
		doc.Submitters = append(doc.Submitters, genesis.Submitter{
			AddressHex: hexAddr(provider), Role: genesis.RoleAdmittedProvider,
		})
	})
	digest := &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes32(41)}
	register := &providersv1.MsgRegisterProvider{
		Signer:                 provider.PubKey().Address(),
		BusinessId:             "reg-1",
		ProviderId:             "provider-1",
		SigningPubkey:          bytes32(40),
		SigningKeyId:           "key-1",
		EndpointManifestDigest: digest,
		DeclaredCapacityBytes:  1 << 30,
		OperatorGroupId:        "devnet-test-only",
	}
	rotate := &providersv1.MsgRotateProviderKey{
		Signer:             provider.PubKey().Address(),
		BusinessId:         "rot-1",
		ProviderId:         "provider-1",
		NewSigningPubkey:   bytes32(42),
		NewSigningKeyId:    "key-2",
		ExpectedGeneration: 1,
		CurrentKeySignature: &typesv1.InnerSignature{
			Domain:                  typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE,
			PublicKey:               bytes32(40),
			Signature:               bytes.Repeat([]byte{5}, 64),
			KeyId:                   "key-1",
			CanonicalProfileVersion: 1,
		},
	}
	authorityBefore := c.snapshot()
	providersBefore := c.run.app.ModuleSnapshot(app.ModuleProviders)
	c.ok(t, provider, register)
	if !bytes.Equal(authorityBefore, c.snapshot()) {
		t.Fatal("RegisterProvider changed the authority store")
	}
	if bytes.Equal(providersBefore, c.run.app.ModuleSnapshot(app.ModuleProviders)) {
		t.Fatal("RegisterProvider did not write the providers store")
	}
	full := c.run.app.FullSnapshot()
	c.reject(t, rejectRow{name: "RotateProviderKey with an InnerSignature", key: provider, msg: rotate,
		space: "svn", code: 1, proto: authority.CodeUnauthorized, advances: true})
	if !bytes.Equal(full, c.run.app.FullSnapshot()) {
		t.Fatal("the rejected rotation changed the full state")
	}
}

// authorityResult returns the attributes of the single authority_result
// event of one transaction, from the typed ABCI events, without the SDK's
// msg_index attribute.
func authorityResult(t *testing.T, result *abci.ExecTxResult) map[string]string {
	t.Helper()
	var found map[string]string
	for _, event := range result.Events {
		if event.Type != "authority_result" {
			continue
		}
		if found != nil {
			t.Fatal("two authority_result events")
		}
		found = map[string]string{}
		for _, attr := range event.Attributes {
			found[attr.Key] = attr.Value
		}
	}
	if found == nil {
		t.Fatalf("no authority_result event in %v", result.Events)
	}
	// The SDK adds msg_index to every message event. One message per tx.
	if index, ok := found["msg_index"]; ok {
		if index != "0" {
			t.Fatalf("msg_index %s", index)
		}
		delete(found, "msg_index")
	}
	return found
}

// TestKeepOutcomesReportTheirCode pins recorded deviation DR-06-05. A spec
// keep transition commits with ABCI code 0, because SDK v0.55 discards the
// message branch on a handler error. The protocol code is on the
// authority_result event, and the stored mark persists.
func TestKeepOutcomesReportTheirCode(t *testing.T) {
	c := newCouncil(t, nil)
	s1, s2, s3 := c.seat(0), c.seat(1), c.seat(2)
	auth := c.keys.authority
	start := c.run.when

	// Approve after expiry: EXPIRED is stored.
	c.ok(t, s1, change{id: "soon", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000, expiry: start.Unix() + 50}.msg(t, s1))
	c.run.when = start.Add(60 * time.Second)
	before := c.snapshot()
	late := c.ok(t, s2, approveMsg(s2, "late-ap", "soon"))
	if got := authorityResult(t, late); len(got) != 1 || got["code"] != authority.CodeExpired {
		t.Fatalf("late approve event %v", got)
	}
	if bytes.Equal(before, c.snapshot()) {
		t.Fatal("the EXPIRED mark was not stored")
	}
	if again := c.send(t, s3, approveMsg(s3, "late-ap-2", "soon")); again.Codespace != "svn" || again.Code != 14 {
		t.Fatalf("approve after the EXPIRED mark: codespace %q code %d log %s", again.Codespace, again.Code, again.Log)
	}

	// Execute of a removal that leaves three validators: REJECTED is stored
	// and nothing is scheduled.
	activation := uint64(c.run.height + 30)
	c.queue(t, 0, change{id: "rm-v4", base: 1, node: "v4", pub: bytes32(4), power: 0, height: activation}.msg(t, s1))
	before = c.snapshot()
	rejected := c.ok(t, auth, executeMsg(auth, "ex-rm", "rm-v4"))
	if got := authorityResult(t, rejected); len(got) != 1 || got["code"] != authority.CodeForbidden {
		t.Fatalf("forbidden execute event %v", got)
	}
	if bytes.Equal(before, c.snapshot()) {
		t.Fatal("the REJECTED mark was not stored")
	}
	if again := c.send(t, auth, executeMsg(auth, "ex-rm-2", "rm-v4")); again.Codespace != "svn" || again.Code != 14 {
		t.Fatalf("execute after the REJECTED mark: codespace %q code %d log %s", again.Codespace, again.Code, again.Log)
	}
	for c.run.height <= int64(activation) {
		c.run.deliver(t, nil)
		if len(c.run.last.ValidatorUpdates) != 0 {
			t.Fatalf("a rejected removal changed the set at height %d", c.run.height)
		}
	}

	// Identical retry of a propose: idempotent, nothing written.
	retry := change{id: "retry", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, s3)
	c.ok(t, s3, retry)
	before = c.snapshot()
	same := c.ok(t, s3, retry)
	if got := authorityResult(t, same); len(got) != 1 || got["idempotent"] != "true" {
		t.Fatalf("retry event %v", got)
	}
	if !bytes.Equal(before, c.snapshot()) {
		t.Fatal("an idempotent retry changed the authority store")
	}
}

// TestPowerOnlyChangeCommitted changes one validator's power and keeps its
// key. Genesis powers are 2, so 1,2,2,2 keeps max*3 < total.
func TestPowerOnlyChangeCommitted(t *testing.T) {
	c := newCouncil(t, func(doc *genesis.Document) {
		for i := range doc.Validators {
			doc.Validators[i].Power = 2
		}
	})
	auth := c.keys.authority
	activation := uint64(c.run.height + 20)
	c.queue(t, 0, change{id: "v1-down", base: 1, node: "v1", pub: bytes32(1), power: 1, height: activation}.msg(t, c.seat(0)))
	c.ok(t, auth, executeMsg(auth, "ex-v1", "v1-down"))
	for c.run.height < int64(activation) {
		c.run.deliver(t, nil)
		if c.run.height < int64(activation) && len(c.run.last.ValidatorUpdates) != 0 {
			t.Fatalf("updates at height %d before activation %d", c.run.height, activation)
		}
	}
	updates := c.run.last.ValidatorUpdates
	if len(updates) != 1 || updates[0].Power != 1 || !bytes.Equal(updates[0].PubKey.GetEd25519(), bytes32(1)) {
		t.Fatalf("activation updates %+v", updates)
	}
	raw, err := c.run.app.AuthorityConsensusBytes()
	if err != nil {
		t.Fatal(err)
	}
	var state authorityv1.GenesisState
	if err := proto.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	for _, row := range state.GetAuthority().GetValidators() {
		want := int64(2)
		if row.GetNodeId() == "v1" {
			want = 1
			if !bytes.Equal(row.GetConsensusPubkey(), bytes32(1)) {
				t.Fatal("v1 key changed")
			}
		}
		if row.GetPower() != want {
			t.Fatalf("%s power %d, want %d", row.GetNodeId(), row.GetPower(), want)
		}
	}
}

// TestAnteRejectsNonAdmittedAccount (REQ-CHAIN-008): an account with an
// auth record but no admission row cannot write. The ante refuses it before
// the handler and before the sequence moves.
func TestAnteRejectsNonAdmittedAccount(t *testing.T) {
	c := newCouncil(t, nil)
	s1, s2 := c.seat(0), c.seat(1)
	if res := c.send(t, s1, &authorityv1.MsgProposeChange{Signer: s1.PubKey().Address()}); res.Codespace != "svn" || res.Code != 7 {
		t.Fatalf("admitted seat: codespace %q code %d log %s", res.Codespace, res.Code, res.Log)
	}
	c.run.app.RevokeAdmissionForTest(s1.PubKey().Address())
	c.run.deliver(t, nil)
	valid := change{id: "after-revoke", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, s1)
	c.reject(t, rejectRow{name: "propose from a seat without an admission row", key: s1, msg: valid,
		space: "svn", code: 1, proto: "signer is not an admitted application account"})
	if res := c.send(t, s2, &authorityv1.MsgProposeChange{Signer: s2.PubKey().Address()}); res.Codespace != "svn" || res.Code != 7 {
		t.Fatalf("other seat: codespace %q code %d log %s", res.Codespace, res.Code, res.Log)
	}
}

// TestAuthorityMessagesCarryNoMediaFields (REQ-ARCH-005): the three
// authority messages and every message they nest have no media, owner, or
// personal field, and no open bytes field. The only bytes fields are the
// 20-byte signer, the 32-byte consensus key, and digest values.
func TestAuthorityMessagesCarryNoMediaFields(t *testing.T) {
	service := authorityv1.File_svn_authority_v1_tx_proto.Services().ByName("Msg")
	if service == nil || service.Methods().Len() != 3 {
		t.Fatal("svn.authority.v1.Msg must have three methods")
	}
	forbidden := []string{"owner", "creator", "uploader", "media", "video", "manifest", "blob", "chunk", "url", "uri", "email", "title", "caption"}
	boundedBytes := map[protoreflect.FullName]bool{
		"svn.authority.v1.MsgProposeChange.signer":               true,
		"svn.authority.v1.MsgApproveChange.signer":               true,
		"svn.authority.v1.MsgExecuteChange.signer":               true,
		"svn.authority.v1.ValidatorPowerChange.consensus_pubkey": true,
		"svn.types.v1.Digest.value":                              true,
	}
	visited := map[protoreflect.FullName]bool{}
	var walk func(protoreflect.MessageDescriptor)
	walk = func(md protoreflect.MessageDescriptor) {
		if visited[md.FullName()] {
			return
		}
		visited[md.FullName()] = true
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			field := fields.Get(i)
			name := strings.ToLower(string(field.Name()))
			for _, word := range forbidden {
				if strings.Contains(name, word) {
					t.Errorf("%s names %q", field.FullName(), word)
				}
			}
			if field.Kind() == protoreflect.BytesKind && !boundedBytes[field.FullName()] {
				t.Errorf("%s is an open bytes field", field.FullName())
			}
			if field.Kind() == protoreflect.MessageKind {
				walk(field.Message())
			}
		}
	}
	for i := 0; i < service.Methods().Len(); i++ {
		walk(service.Methods().Get(i).Input())
	}
	for _, name := range []protoreflect.FullName{
		"svn.authority.v1.ValidatorPowerChange", "svn.authority.v1.GovernedLimitChange",
		"svn.authority.v1.AdmitProviderChange", "svn.authority.v1.DisableProviderChange",
		"svn.authority.v1.ForceWithdrawChange", "svn.types.v1.Digest",
	} {
		if !visited[name] {
			t.Errorf("the walk did not reach %s", name)
		}
	}
}

// TestCouncilIsDecisionBlockedOffDevnet: on a chain without a ready devnet
// council, every council message is ERROR_CODE_DECISION_BLOCKED (svn code
// 17) and leaves the whole authority store byte-identical. One chain has a
// chain id without the devnet prefix and no devnet test parameters. The
// other is a devnet whose DEC-008 test parameter has no threshold, so it has
// no seats. The former seat accounts stay BROADCASTER submitters, so their
// transactions pass the ante and reach the handler.
func TestCouncilIsDecisionBlockedOffDevnet(t *testing.T) {
	for _, chain := range []struct {
		name string
		edit func(*genesis.Document)
	}{
		{"chain id without the devnet prefix", func(doc *genesis.Document) {
			doc.ChainID = "shortvideo-beta-unit"
			doc.AllowDevnetTestParameters = false
			doc.DevnetTestParameters = nil
			doc.DevnetTestSeats = nil
		}},
		{"devnet DEC-008 parameter without the threshold", func(doc *genesis.Document) {
			doc.DevnetTestParameters[0].NumericValue = 0
			doc.DevnetTestSeats = nil
		}},
	} {
		c := newCouncil(t, chain.edit)
		auth, seat := c.keys.authority, c.seat(0)
		blocked := func(name string, key cryptotypes.PrivKey, msg sdk.Msg) {
			t.Helper()
			c.reject(t, rejectRow{name: chain.name + ": " + name, key: key, msg: msg,
				space: "svn", code: 17, proto: authority.CodeDecisionBlocked, advances: true})
		}
		blocked("authority propose", auth, change{id: "p-1", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, auth))
		blocked("former seat propose", seat, change{id: "p-2", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, seat))
		blocked("former seat approve", seat, approveMsg(seat, "ap-1", "p-1"))
		blocked("authority execute", auth, executeMsg(auth, "ex-1", "p-1"))
	}
}
