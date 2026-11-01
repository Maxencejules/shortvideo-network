package app_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The registry tests run a real application (IAVL on memdb, ante handler,
// message router, EndBlock, Commit) and deliver signed transactions block by
// block. Nothing here is a mock. Off-chain inputs are synthetic and carry
// canaries; only their digests reach a transaction.

const registryGas = uint64(2_000_000)

// Synthetic off-chain data. None of it may appear on chain.
var (
	canaryEndpoint = "https://canary-endpoint.invalid/segments/canary-7f3a"
	canaryEmail    = "creator-canary@example.invalid"
	canaryUser     = "canary_user_7f3a"
	canaryWatch    = "watch_event:canary-viewer-7f3a:segment-0003"
	canaryReport   = "complaint:canary-narrative-7f3a"
	canaryMedia    = bytes.Repeat([]byte("CANARY-MEDIA-PAYLOAD-7f3a-"), 4096/26+1)[:4096]
)

func canaries() [][]byte {
	return [][]byte{
		[]byte(canaryEndpoint),
		[]byte("canary-endpoint.invalid"),
		[]byte(canaryEmail),
		[]byte(canaryUser),
		[]byte(canaryWatch),
		[]byte(canaryReport),
		canaryMedia,
		canaryMedia[:64],
	}
}

type actor struct {
	name string
	key  cryptotypes.PrivKey
	addr []byte
}

func (a *actor) hexAddr() string { return hex.EncodeToString(a.addr) }

func newSecp(name string) *actor {
	key := sdksecp.GenPrivKey()
	return &actor{name: name, key: key, addr: key.PubKey().Address()}
}

func newSeat(name string) *actor {
	key := sdked.GenPrivKey()
	return &actor{name: name, key: key, addr: key.PubKey().Address()}
}

// signingKey is one provider's ed25519 signing key (not an account key).
type signingKey struct {
	id   string
	priv ed25519.PrivateKey
}

func (k signingKey) pub() []byte { return []byte(k.priv.Public().(ed25519.PublicKey)) }

func newSigningKey(id string) signingKey {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	return signingKey{id: id, priv: priv}
}

// regNet is one registry devnet document and its runtime keys.
type regNet struct {
	doc       genesis.Document
	authority *actor
	service   *actor
	service2  *actor
	provider1 *actor
	provider2 *actor
	seats     []*actor
	numbers   map[string]uint64
	seq       map[string]uint64
	policy    uint64
	intent    signingKey
	signing   map[string]signingKey
}

// newRegNet builds the devnet document: authority, two services, two
// providers, four council seats, and the DEC-001/002/003/005/008 devnet
// test parameters. drop removes devnet parameters by decision id.
func newRegNet(drop ...string) *regNet {
	n := &regNet{
		authority: newSecp("authority"),
		service:   newSecp("service"),
		service2:  newSecp("service-2"),
		provider1: newSecp("provider-1"),
		provider2: newSecp("provider-2"),
		seq:       map[string]uint64{},
		policy:    1,
		intent:    newSigningKey("intent-1"),
		signing:   map[string]signingKey{},
	}
	doc := sampleDocument()
	doc.ChainID = "shortvideo-dev-registry"
	doc.Submitters = []genesis.Submitter{
		{AddressHex: n.authority.hexAddr(), Role: genesis.RoleAuthority},
		{AddressHex: n.service.hexAddr(), Role: genesis.RoleAdmittedService},
		{AddressHex: n.service2.hexAddr(), Role: genesis.RoleAdmittedService},
		{AddressHex: n.provider1.hexAddr(), Role: genesis.RoleAdmittedProvider},
		{AddressHex: n.provider2.hexAddr(), Role: genesis.RoleAdmittedProvider},
	}
	doc.DevnetTestSeats = make([]genesis.DevnetTestSeat, 4)
	for i := range doc.DevnetTestSeats {
		seat := newSeat(fmt.Sprintf("seat-%d", i+1))
		n.seats = append(n.seats, seat)
		doc.DevnetTestSeats[i] = genesis.DevnetTestSeat{
			SeatID:       seat.name,
			PublicKeyHex: hex.EncodeToString(seat.key.PubKey().Bytes()),
		}
		doc.Submitters = append(doc.Submitters, genesis.Submitter{AddressHex: seat.hexAddr(), Role: genesis.RoleBroadcaster})
	}
	params := []genesis.DevnetTestParameter{
		{Label: genesis.DevnetTestLabel, DecisionID: "DEC-001", RoleName: "DEVNET_TEST_ONLY"},
		{Label: genesis.DevnetTestLabel, DecisionID: "DEC-002", RoleName: "DEVNET_TEST_ONLY"},
		{Label: genesis.DevnetTestLabel, DecisionID: "DEC-003", RoleName: "DEVNET_TEST_ONLY"},
		{Label: genesis.DevnetTestLabel, DecisionID: "DEC-005", NumericValue: 3600},
		{Label: genesis.DevnetTestLabel, DecisionID: "DEC-008", NumericValue: 3, DelaySeconds: 0},
	}
	doc.DevnetTestParameters = nil
	for _, row := range params {
		skip := false
		for _, id := range drop {
			skip = skip || row.DecisionID == id
		}
		if !skip {
			doc.DevnetTestParameters = append(doc.DevnetTestParameters, row)
		}
	}
	n.doc = doc
	n.numbers = accountNumbers(doc)
	return n
}

// cluster delivers the same blocks to every node and checks that the
// committed app hash and every result code agree at each height.
type cluster struct {
	nodes  []*running
	blocks []*abci.ResponseFinalizeBlock
	hashes [][]byte
}

func (n *regNet) start(t *testing.T, nodes int) *cluster {
	t.Helper()
	c := &cluster{}
	for i := 0; i < nodes; i++ {
		c.nodes = append(c.nodes, boot(t, n.doc))
	}
	c.block(t)
	return c
}

func (c *cluster) app() *app.App { return c.nodes[0].app }

func (c *cluster) height() int64 { return c.nodes[0].height }

// block finalizes and commits one block with txs on every node.
func (c *cluster) block(t *testing.T, txs ...[]byte) *abci.ResponseFinalizeBlock {
	t.Helper()
	var first *abci.ResponseFinalizeBlock
	var hash []byte
	for i, node := range c.nodes {
		node.height++
		res, err := node.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: node.height,
			Time:   node.when,
			Hash:   bytes32(byte(node.height)),
			Txs:    txs,
		})
		if err != nil {
			t.Fatalf("node %d height %d: %v", i, node.height, err)
		}
		if _, err := node.app.Commit(); err != nil {
			t.Fatal(err)
		}
		node.last = res
		got := node.app.LastCommitID().Hash
		if i == 0 {
			first, hash = res, got
			continue
		}
		if !bytes.Equal(hash, got) {
			t.Fatalf("height %d: node %d app hash %X, node 0 %X", node.height, i, got, hash)
		}
		if len(res.TxResults) != len(first.TxResults) {
			t.Fatalf("height %d: result count differs", node.height)
		}
		for j := range res.TxResults {
			if res.TxResults[j].Code != first.TxResults[j].Code || res.TxResults[j].Codespace != first.TxResults[j].Codespace {
				t.Fatalf("height %d tx %d: node %d code %s/%d, node 0 %s/%d", node.height, j, i,
					res.TxResults[j].Codespace, res.TxResults[j].Code, first.TxResults[j].Codespace, first.TxResults[j].Code)
			}
		}
	}
	c.blocks = append(c.blocks, first)
	c.hashes = append(c.hashes, append([]byte(nil), hash...))
	return first
}

// signed returns the signed bytes of msg from who at its next sequence.
func (n *regNet) signed(t *testing.T, c *cluster, who *actor, gas uint64, fee sdk.Coins, msg sdk.Msg) []byte {
	t.Helper()
	return signFull(t, c.app(), n.doc.ChainID, who.key, n.numbers[who.hexAddr()], n.seq[who.hexAddr()], gas, fee, msg)
}

// send signs, delivers one tx in its own block, and advances who's sequence
// when the ante handler committed (a "tx" event carries acc_seq).
func (n *regNet) send(t *testing.T, c *cluster, who *actor, msg sdk.Msg) *abci.ExecTxResult {
	t.Helper()
	return n.sendWith(t, c, who, registryGas, nil, msg)
}

func (n *regNet) sendWith(t *testing.T, c *cluster, who *actor, gas uint64, fee sdk.Coins, msg sdk.Msg) *abci.ExecTxResult {
	t.Helper()
	return n.deliverRaw(t, c, who, n.signed(t, c, who, gas, fee, msg))
}

func (n *regNet) deliverRaw(t *testing.T, c *cluster, who *actor, raw []byte) *abci.ExecTxResult {
	t.Helper()
	res := c.block(t, raw)
	if len(res.TxResults) != 1 {
		t.Fatalf("tx results %d", len(res.TxResults))
	}
	result := res.TxResults[0]
	if who != nil && anteCommitted(result) {
		n.seq[who.hexAddr()]++
	}
	return result
}

// anteCommitted reports whether the ante handler ran to the end: the
// signature decorator's acc_seq attribute is only in committed ante events.
func anteCommitted(res *abci.ExecTxResult) bool {
	for _, ev := range res.Events {
		if ev.Type != sdk.EventTypeTx {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == sdk.AttributeKeyAccountSequence {
				return true
			}
		}
	}
	return false
}

// wantCode asserts the typed result: code 0, or codespace svn and the
// protocol number of want.
func wantCode(t *testing.T, label string, res *abci.ExecTxResult, want msgkit.Code) {
	t.Helper()
	if want == msgkit.OK {
		if res.Code != 0 {
			t.Fatalf("%s: code %s/%d log %s", label, res.Codespace, res.Code, res.Log)
		}
		return
	}
	number := uint32(typesv1.ErrorCode_value[string(want)])
	if number == 0 {
		t.Fatalf("%s: %s is not a protocol code", label, want)
	}
	if res.Codespace != "svn" || res.Code != number {
		t.Fatalf("%s: got %s/%d want svn/%d (%s) log %s", label, res.Codespace, res.Code, number, want, res.Log)
	}
}

// typedEvents returns the svn.events.v1 events of one result.
func typedEvents(events []abci.Event) []abci.Event {
	var out []abci.Event
	for _, ev := range events {
		if strings.HasPrefix(ev.Type, "svn.events.v1.") {
			out = append(out, ev)
		}
	}
	return out
}

func sha(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

func digestOf(value []byte) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: append([]byte(nil), value...)}
}

// manifestDigest is the SHA-256 of a synthetic capability manifest whose
// endpoint URL is a canary. Only the digest goes on chain.
func manifestDigest(t testing.TB, providerID, keyID string, capacity uint64, salt string) *typesv1.Digest {
	t.Helper()
	result, err := capability.Encode(capability.Manifest{
		ProviderID:            providerID,
		SigningKeyID:          keyID,
		DeclaredCapacityBytes: capacity,
		Endpoints:             []capability.Endpoint{{Name: "segments-" + salt, URL: canaryEndpoint + "/" + salt}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Canonical, canaryEndpoint) {
		t.Fatal("capability manifest lost its canary")
	}
	return result.Digest()
}

// packageManifest is a synthetic off-chain package manifest: media bytes,
// creator email, username, and a watch event. Only its SHA-256 is used.
func packageManifest(contentID string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`{"content":"` + contentID + `","creator":"` + canaryEmail + `","user":"` + canaryUser + `","watch":"` + canaryWatch + `","media":"`)
	buf.Write(canaryMedia)
	buf.WriteString(`"}`)
	return buf.Bytes()
}

// reasonDigest is the SHA-256 of a synthetic complaint narrative.
func reasonDigest(label string) []byte { return sha([]byte(canaryReport + ":" + label)) }

func (n *regNet) registerProvider(t testing.TB, who *actor, bid, providerID, group string, capacity uint64) *providersv1.MsgRegisterProvider {
	t.Helper()
	// The off-chain manifest needs valid ids; an out-of-pattern provider id
	// (a BOUNDS case) gets a manifest for a placeholder id.
	manifestID := providerID
	if !msgkit.ValidID(providerID) {
		manifestID = "invalid-id"
	}
	key, ok := n.signing[providerID]
	if !ok {
		key = newSigningKey(manifestID + "-key-1")
		n.signing[providerID] = key
	}
	return &providersv1.MsgRegisterProvider{
		Signer:                 who.addr,
		BusinessId:             bid,
		ProviderId:             providerID,
		SigningPubkey:          key.pub(),
		SigningKeyId:           key.id,
		EndpointManifestDigest: manifestDigest(t, manifestID, key.id, capacity, "r"),
		DeclaredCapacityBytes:  capacity,
		OperatorGroupId:        group,
	}
}

func (n *regNet) updateProvider(t testing.TB, who *actor, bid, providerID string, capacity uint64, salt string) *providersv1.MsgUpdateProvider {
	t.Helper()
	keyID := n.signing[providerID].id
	if keyID == "" {
		keyID = "unregistered-key"
	}
	return &providersv1.MsgUpdateProvider{
		Signer:                 who.addr,
		BusinessId:             bid,
		ProviderId:             providerID,
		EndpointManifestDigest: manifestDigest(t, providerID, keyID, capacity, salt),
		DeclaredCapacityBytes:  capacity,
	}
}

func innerSig(domain typesv1.SignatureDomain, key signingKey, signBytes []byte) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  domain,
		PublicKey:               key.pub(),
		Signature:               ed25519.Sign(key.priv, signBytes),
		KeyId:                   key.id,
		CanonicalProfileVersion: 1,
	}
}

// rotate builds a rotation signed by the provider's current key. It does
// not change n.signing; commitRotation does after the tx commits.
func (n *regNet) rotate(t testing.TB, who *actor, bid, providerID string, expected uint64, next signingKey) *providersv1.MsgRotateProviderKey {
	t.Helper()
	signBytes, err := providers.RotateSignBytes(providerID, next.id, next.pub(), expected)
	if err != nil {
		t.Fatal(err)
	}
	return &providersv1.MsgRotateProviderKey{
		Signer:              who.addr,
		BusinessId:          bid,
		ProviderId:          providerID,
		NewSigningPubkey:    next.pub(),
		NewSigningKeyId:     next.id,
		ExpectedGeneration:  expected,
		CurrentKeySignature: innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE, n.signing[providerID], signBytes),
	}
}

func (n *regNet) drain(t testing.TB, who *actor, bid, providerID string, generation uint64, domain typesv1.SignatureDomain) *providersv1.MsgDrainProvider {
	t.Helper()
	signBytes, err := providers.DrainSignBytes(providerID, generation)
	if err != nil {
		t.Fatal(err)
	}
	if domain != typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN {
		// Same canonical payload under another domain string.
		name, _ := msgkit.DomainString(domain)
		signBytes = append([]byte(name), signBytes[bytes.IndexByte(signBytes, 0):]...)
	}
	return &providersv1.MsgDrainProvider{
		Signer:            who.addr,
		BusinessId:        bid,
		ProviderId:        providerID,
		ProviderSignature: innerSig(domain, n.signing[providerID], signBytes),
	}
}

func (n *regNet) registerContent(t testing.TB, who *actor, bid, contentID string, version uint64, manifest []byte, size uint64) *contentv1.MsgRegisterContent {
	t.Helper()
	sum := sha(manifest)
	signBytes, err := content.RegisterIntentSignBytes(who.addr, contentID, version, sum, size, content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	return &contentv1.MsgRegisterContent{
		Signer:         who.addr,
		BusinessId:     bid,
		ContentId:      contentID,
		ContentVersion: version,
		ManifestDigest: digestOf(sum),
		PackageBytes:   size,
		HostingIntent:  innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, n.intent, signBytes),
	}
}

func (n *regNet) withdrawContent(t testing.TB, who *actor, bid, contentID string, version uint64, reason []byte) *contentv1.MsgWithdrawContent {
	t.Helper()
	signBytes, err := content.WithdrawIntentSignBytes(who.addr, contentID, version, reason, content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	return &contentv1.MsgWithdrawContent{
		Signer:         who.addr,
		BusinessId:     bid,
		ContentId:      contentID,
		ContentVersion: version,
		ReasonDigest:   digestOf(reason),
		HostingIntent:  innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, n.intent, signBytes),
	}
}

// proposal builds a ProposeChange from seat-1 at the tracked policy version.
func (n *regNet) proposal(t testing.TB, id, parameterKey string, payload *authorityv1.ProposalPayload) *authorityv1.MsgProposeChange {
	t.Helper()
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		t.Fatal(out.Code)
	}
	return &authorityv1.MsgProposeChange{
		Signer:            n.seats[0].addr,
		BusinessId:        "pb-" + id,
		ProposalId:        id,
		ParameterKey:      parameterKey,
		BasePolicyVersion: n.policy,
		Payload:           payload,
		PayloadDigest:     digestOf(digest),
		ExpiryUnixSeconds: 1_800_000_000,
	}
}

func admitPayload(providerID string) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{
		AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: providerID},
	}}
}

func disablePayload(providerID string) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_DisableProvider{
		DisableProvider: &authorityv1.DisableProviderChange{ProviderId: providerID},
	}}
}

func forceWithdrawPayload(contentID string, version uint64, reason []byte) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ForceWithdraw{
		ForceWithdraw: &authorityv1.ForceWithdrawChange{ContentId: contentID, ContentVersion: version, ReasonDigest: digestOf(reason)},
	}}
}

// councilReady proposes and collects three approvals. Every step must
// commit with code 0.
func (n *regNet) councilReady(t *testing.T, c *cluster, id, parameterKey string, payload *authorityv1.ProposalPayload) {
	t.Helper()
	wantCode(t, "propose "+id, n.send(t, c, n.seats[0], n.proposal(t, id, parameterKey, payload)), msgkit.OK)
	for i := 0; i < 3; i++ {
		seat := n.seats[i]
		wantCode(t, fmt.Sprintf("approve %s by %s", id, seat.name), n.send(t, c, seat, &authorityv1.MsgApproveChange{
			Signer: seat.addr, BusinessId: fmt.Sprintf("%s-ap-%d", id, i+1), ProposalId: id,
		}), msgkit.OK)
	}
}

// execute sends ExecuteChange from the authority account.
func (n *regNet) execute(t *testing.T, c *cluster, id string) *abci.ExecTxResult {
	t.Helper()
	return n.send(t, c, n.authority, &authorityv1.MsgExecuteChange{Signer: n.authority.addr, BusinessId: "ex-" + id, ProposalId: id})
}

// council runs a proposal to an applied execution and advances the policy
// version, as the engine does after every executed proposal.
func (n *regNet) council(t *testing.T, c *cluster, id, parameterKey string, payload *authorityv1.ProposalPayload) *abci.ExecTxResult {
	t.Helper()
	n.councilReady(t, c, id, parameterKey, payload)
	res := n.execute(t, c, id)
	wantCode(t, "execute "+id, res, msgkit.OK)
	if authorityResult(res) != "" {
		t.Fatalf("execute %s kept with %s", id, authorityResult(res))
	}
	n.policy++
	return res
}

// authorityResult is the code of a kept execution (REJECTED or EXPIRED).
func authorityResult(res *abci.ExecTxResult) string {
	for _, ev := range res.Events {
		if ev.Type != "authority_result" {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == "code" {
				return attr.Value
			}
		}
	}
	return ""
}

// query runs one ABCI query at height (0 = latest) and returns the bytes.
func query(t *testing.T, chainApp *app.App, path string, req proto.Message, height int64) ([]byte, *abci.ResponseQuery) {
	t.Helper()
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := chainApp.Query(context.Background(), &abci.RequestQuery{Path: path, Data: data, Height: height})
	if err != nil {
		t.Fatal(err)
	}
	return res.Value, res
}

func queryProvider(t *testing.T, chainApp *app.App, id string, height int64) (*providersv1.Provider, []byte) {
	t.Helper()
	raw, res := query(t, chainApp, "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: id}, height)
	if res.Code != 0 {
		t.Fatalf("provider %s at %d: %s/%d %s", id, height, res.Codespace, res.Code, res.Log)
	}
	var out providersv1.QueryProviderResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.GetProvider(), raw
}

func queryContent(t *testing.T, chainApp *app.App, id string, version uint64, height int64) (*contentv1.QueryContentResponse, []byte) {
	t.Helper()
	raw, res := query(t, chainApp, "/svn.content.v1.Query/Content", &contentv1.QueryContentRequest{ContentId: id, ContentVersion: version}, height)
	if res.Code != 0 {
		t.Fatalf("content %s at %d: %s/%d %s", id, height, res.Codespace, res.Code, res.Log)
	}
	var out contentv1.QueryContentResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return &out, raw
}

// providerPages walks svn.providers.v1.Query/Providers with limit and
// returns every page's raw bytes and the providers in order.
func providerPages(t *testing.T, chainApp *app.App, limit uint32, height int64) ([][]byte, []*providersv1.Provider) {
	t.Helper()
	var pages [][]byte
	var all []*providersv1.Provider
	var key []byte
	for i := 0; i < 1000; i++ {
		raw, res := query(t, chainApp, "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{
			Page: &typesv1.PageRequest{Limit: limit, PageKey: key},
		}, height)
		if res.Code != 0 {
			t.Fatalf("providers page %d: %s/%d %s", i, res.Codespace, res.Code, res.Log)
		}
		var out providersv1.QueryProvidersResponse
		if err := proto.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if uint32(len(out.GetProviders())) > limit {
			t.Fatalf("page %d has %d providers above limit %d", i, len(out.GetProviders()), limit)
		}
		pages = append(pages, raw)
		all = append(all, out.GetProviders()...)
		key = out.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return pages, all
		}
	}
	t.Fatal("pagination did not end")
	return nil, nil
}

// sortedNames returns map keys in order.
func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
