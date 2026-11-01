package app_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	groupDevnet = providers.DevnetOperatorGroup
	capacity1   = uint64(1 << 30)
	capacity2   = uint64(3 << 29)
	sizeC1      = uint64(4_000_000)
	sizeC2      = uint64(9_000_000)
)

// flowRecord holds the heights the query tests read.
type flowRecord struct {
	rotation       int64
	beforeWithdraw int64
	afterWithdraw  int64
}

// runRegistryFlow drives the committed registry flow: register p1 and p2,
// council admit both, update p1, rotate p1, drain p2, council disable p2,
// register c1 and c2, withdraw c1, council force-withdraw c2, and an
// idempotent replay. Rejections are interleaved; each asserts its code.
func runRegistryFlow(t *testing.T, n *regNet, c *cluster) flowRecord {
	t.Helper()
	var rec flowRecord
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	wantCode(t, "register p2", n.send(t, c, n.provider2, n.registerProvider(t, n.provider2, "reg-p2", "p2", groupDevnet, capacity1)), msgkit.OK)
	wantCode(t, "cross-owner update", n.send(t, c, n.provider2, n.updateProvider(t, n.provider2, "upd-p1-x", "p1", capacity2, "x")), msgkit.Forbidden)

	n.council(t, c, "prop-admit-p1", "admit-p1", admitPayload("p1"))
	n.council(t, c, "prop-admit-p2", "admit-p2", admitPayload("p2"))

	wantCode(t, "update p1", n.send(t, c, n.provider1, n.updateProvider(t, n.provider1, "upd-p1-1", "p1", capacity2, "u1")), msgkit.OK)
	next := newSigningKey("p1-key-2")
	wantCode(t, "rotate p1", n.send(t, c, n.provider1, n.rotate(t, n.provider1, "rot-p1-1", "p1", 1, next)), msgkit.OK)
	rec.rotation = c.height()
	n.signing["p1"] = next
	wantCode(t, "drain p2", n.send(t, c, n.provider2, n.drain(t, n.provider2, "drain-p2", "p2", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)), msgkit.OK)
	n.council(t, c, "prop-disable-p2", "disable-p2", disablePayload("p2"))

	wantCode(t, "register c1", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)), msgkit.OK)
	wantCode(t, "register c2", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c2", "c2", 1, packageManifest("c2"), sizeC2)), msgkit.OK)
	wantCode(t, "replayed business id", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c1", "c3", 1, packageManifest("c3"), sizeC1)), msgkit.Replay)
	rec.beforeWithdraw = c.height()
	wantCode(t, "withdraw c1", n.send(t, c, n.service, n.withdrawContent(t, n.service, "wd-c1", "c1", 1, reasonDigest("c1"))), msgkit.OK)
	rec.afterWithdraw = c.height()
	wantCode(t, "second withdraw", n.send(t, c, n.service, n.withdrawContent(t, n.service, "wd-c1-again", "c1", 1, reasonDigest("c1-again"))), msgkit.Withdrawn)
	n.council(t, c, "prop-withdraw-c2", "withdraw-c2", forceWithdrawPayload("c2", 1, reasonDigest("c2")))
	replay := n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1))
	wantCode(t, "idempotent replay", replay, msgkit.OK)
	if len(typedEvents(replay.Events)) != 0 {
		t.Fatalf("idempotent replay emitted %d typed events", len(typedEvents(replay.Events)))
	}
	return rec
}

func TestRegistryFlowReplayAndQueries(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 2)
	rec := runRegistryFlow(t, n, c)
	// cluster.block compared both nodes' app hash at every height.
	if len(c.hashes) < 30 {
		t.Fatalf("flow committed only %d blocks", len(c.hashes))
	}

	// Committed records, read through ABCI query on both nodes.
	ids := []string{"p1", "p2"}
	type nodeView struct {
		providers map[string][]byte
		pages1    [][]byte
		pages100  [][]byte
		content   map[string][]byte
	}
	views := make([]nodeView, len(c.nodes))
	for i, node := range c.nodes {
		view := nodeView{providers: map[string][]byte{}, content: map[string][]byte{}}
		for _, id := range ids {
			_, raw := queryProvider(t, node.app, id, 0)
			view.providers[id] = raw
		}
		view.pages1, _ = providerPages(t, node.app, 1, 0)
		view.pages100, _ = providerPages(t, node.app, 100, 0)
		for _, id := range []string{"c1", "c2"} {
			_, raw := queryContent(t, node.app, id, 1, 0)
			view.content[id] = raw
		}
		views[i] = view
	}
	for i := 1; i < len(views); i++ {
		for _, id := range ids {
			if !bytes.Equal(views[0].providers[id], views[i].providers[id]) {
				t.Fatalf("provider %s query bytes differ on node %d", id, i)
			}
		}
		for _, id := range []string{"c1", "c2"} {
			if !bytes.Equal(views[0].content[id], views[i].content[id]) {
				t.Fatalf("content %s query bytes differ on node %d", id, i)
			}
		}
		if len(views[0].pages1) != len(views[i].pages1) || len(views[0].pages100) != len(views[i].pages100) {
			t.Fatal("page counts differ")
		}
		for p := range views[0].pages1 {
			if !bytes.Equal(views[0].pages1[p], views[i].pages1[p]) {
				t.Fatalf("Providers page %d differs on node %d", p, i)
			}
		}
		for p := range views[0].pages100 {
			if !bytes.Equal(views[0].pages100[p], views[i].pages100[p]) {
				t.Fatalf("Providers limit-100 page %d differs on node %d", p, i)
			}
		}
	}
	if len(views[0].pages1) != 2 || len(views[0].pages100) != 1 {
		t.Fatalf("pages with limit 1: %d, limit 100: %d", len(views[0].pages1), len(views[0].pages100))
	}

	// Query results equal the records decoded from Export.
	exported, err := c.app().Export()
	if err != nil {
		t.Fatal(err)
	}
	provState, err := providers.ParseGenesis(exported[app.ModuleProviders])
	if err != nil {
		t.Fatal(err)
	}
	_, walked := providerPages(t, c.app(), 1, 0)
	if len(walked) != len(provState.GetProviders()) {
		t.Fatalf("paged %d providers, exported %d", len(walked), len(provState.GetProviders()))
	}
	for i, record := range provState.GetProviders() {
		if !proto.Equal(record, walked[i]) {
			t.Fatalf("provider %d: paged record differs from export", i)
		}
		got, _ := queryProvider(t, c.app(), record.GetProviderId(), 0)
		if !proto.Equal(record, got) {
			t.Fatalf("provider %s: query differs from export", record.GetProviderId())
		}
	}
	p1, _ := queryProvider(t, c.app(), "p1", 0)
	if p1.GetState() != providersv1.ProviderState_PROVIDER_STATE_ADMITTED || p1.GetKeyGeneration() != 2 ||
		len(p1.GetPreviousSigningKeys()) != 1 || p1.GetAdmittedCapacityBytes() != capacity2 ||
		p1.GetSigningKeyId() != "p1-key-2" || !bytes.Equal(p1.GetAccount(), n.provider1.addr) {
		t.Fatalf("p1 record %v", p1)
	}
	p2, _ := queryProvider(t, c.app(), "p2", 0)
	if p2.GetState() != providersv1.ProviderState_PROVIDER_STATE_DISABLED {
		t.Fatalf("p2 state %v", p2.GetState())
	}

	contState, err := content.ParseGenesis(exported[app.ModuleContent])
	if err != nil {
		t.Fatal(err)
	}
	if len(contState.GetContents()) != 2 || len(contState.GetTombstones()) != 2 {
		t.Fatalf("exported %d contents %d tombstones", len(contState.GetContents()), len(contState.GetTombstones()))
	}
	tombs := map[string]*contentv1.Tombstone{}
	for _, tomb := range contState.GetTombstones() {
		tombs[tomb.GetContentId()] = tomb
	}
	for _, row := range contState.GetContents() {
		got, _ := queryContent(t, c.app(), row.GetContentId(), row.GetContentVersion(), 0)
		if !proto.Equal(row, got.GetContent()) || !proto.Equal(tombs[row.GetContentId()], got.GetTombstone()) {
			t.Fatalf("content %s: query differs from export", row.GetContentId())
		}
		if row.GetState() != contentv1.ContentState_CONTENT_STATE_WITHDRAWN || !bytes.Equal(row.GetSubmittingHost(), n.service.addr) {
			t.Fatalf("content %s: %v", row.GetContentId(), row)
		}
	}
	if !bytes.Equal(tombs["c2"].GetReasonDigest().GetValue(), reasonDigest("c2")) || tombs["c1"].GetHistoryErased() {
		t.Fatal("tombstone fields")
	}

	// Historical queries read the committed version at a height.
	before, rawBefore := queryContent(t, c.app(), "c1", 1, rec.beforeWithdraw)
	if before.GetContent().GetState() != contentv1.ContentState_CONTENT_STATE_PENDING || before.GetTombstone() != nil {
		t.Fatalf("c1 at %d: %v", rec.beforeWithdraw, before)
	}
	after, rawAfter := queryContent(t, c.app(), "c1", 1, rec.afterWithdraw)
	if after.GetContent().GetState() != contentv1.ContentState_CONTENT_STATE_WITHDRAWN || after.GetTombstone() == nil {
		t.Fatalf("c1 at %d: %v", rec.afterWithdraw, after)
	}
	for i := 1; i < len(c.nodes); i++ {
		_, otherBefore := queryContent(t, c.nodes[i].app, "c1", 1, rec.beforeWithdraw)
		_, otherAfter := queryContent(t, c.nodes[i].app, "c1", 1, rec.afterWithdraw)
		if !bytes.Equal(rawBefore, otherBefore) || !bytes.Equal(rawAfter, otherAfter) {
			t.Fatalf("historical query bytes differ on node %d", i)
		}
	}
	atRotation, _ := queryProvider(t, c.app(), "p1", rec.rotation-1)
	if atRotation.GetKeyGeneration() != 1 {
		t.Fatalf("p1 generation at %d: %d", rec.rotation-1, atRotation.GetKeyGeneration())
	}
	rotated, _ := queryProvider(t, c.app(), "p1", rec.rotation)
	if rotated.GetKeyGeneration() != 2 {
		t.Fatalf("p1 generation at %d: %d", rec.rotation, rotated.GetKeyGeneration())
	}

	// Query rejections carry svn codes.
	assertQueryCode(t, c.app(), "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: "absent"}, msgkit.NotFound)
	assertQueryCode(t, c.app(), "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{Limit: 101}}, msgkit.Bounds)
	assertQueryCode(t, c.app(), "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{Limit: 1, PageKey: bytes.Repeat([]byte("a"), 65)}}, msgkit.Bounds)
	assertQueryCode(t, c.app(), "/svn.content.v1.Query/Content", &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 2}, msgkit.NotFound)
}

// assertQueryCode checks a rejected ABCI query. The SDK's gRPC query path
// (baseapp gRPCErrorToSDKError) maps an svn error, whose gRPC status code is
// Unknown, to sdk ErrUnknownRequest (code 6) and keeps the error text, so
// the protocol name is in the log, not in the code. The keepers' query tests
// assert the typed svn error directly.
func assertQueryCode(t *testing.T, chainApp *app.App, path string, req proto.Message, want msgkit.Code) {
	t.Helper()
	_, res := query(t, chainApp, path, req, 0)
	if res.Codespace != sdkerrors.RootCodespace || res.Code != sdkerrors.ErrUnknownRequest.ABCICode() || !strings.Contains(res.Log, string(want)) {
		t.Fatalf("%s: got %s/%d %q want sdk/%d with %s", path, res.Codespace, res.Code, res.Log, sdkerrors.ErrUnknownRequest.ABCICode(), want)
	}
}

// fakeReads is the authority read surface for a standalone keeper in the
// round-trip test. It never grants a role; import does not need one.
type fakeReads struct{}

func (fakeReads) Role(sdk.Context, []byte) typesv1.SubmitterRole {
	return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
}
func (fakeReads) GovernedLimit(sdk.Context, string) (uint64, bool) { return 0, false }
func (fakeReads) DevnetRole(sdk.Context, string) (string, bool)    { return "", false }

// prefixPairs keeps the pairs of a msgkit.StoreSnapshot encoding whose key
// starts with prefix, re-encoded in the same form.
func prefixPairs(t *testing.T, snapshot []byte, prefix byte) []byte {
	t.Helper()
	var out []byte
	for len(snapshot) > 0 {
		if len(snapshot) < 8 {
			t.Fatal("truncated snapshot")
		}
		keyLen := binary.BigEndian.Uint64(snapshot[:8])
		if uint64(len(snapshot)-8) < keyLen+8 {
			t.Fatal("truncated snapshot key")
		}
		key := snapshot[8 : 8+keyLen]
		rest := snapshot[8+keyLen:]
		valueLen := binary.BigEndian.Uint64(rest[:8])
		if uint64(len(rest)-8) < valueLen {
			t.Fatal("truncated snapshot value")
		}
		pair := snapshot[:8+keyLen+8+valueLen]
		if len(key) > 0 && key[0] == prefix {
			out = append(out, pair...)
		}
		snapshot = snapshot[len(pair):]
	}
	return out
}

func TestGenesisRoundTripFromCommittedState(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runRegistryFlow(t, n, c)
	exported, err := c.app().Export()
	if err != nil {
		t.Fatal(err)
	}
	committedProviders := c.app().ModuleSnapshot(app.ModuleProviders)
	committedContent := c.app().ModuleSnapshot(app.ModuleContent)

	provState, err := providers.ParseGenesis(exported[app.ModuleProviders])
	if err != nil {
		t.Fatal(err)
	}
	if err := providers.ValidateGenesis(provState); err != nil {
		t.Fatal(err)
	}
	if len(provState.GetProviders()) != 2 {
		t.Fatalf("exported providers %d", len(provState.GetProviders()))
	}
	ctx, keys := msgkittest.NewContext(t, providers.ModuleName, content.ModuleName)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	provKeeper := providers.NewKeeper(keys[providers.ModuleName], providers.Deps{Roles: fakeReads{}, Limits: fakeReads{}})
	if err := provKeeper.InitGenesis(ctx, provState); err != nil {
		t.Fatal(err)
	}
	again, err := provKeeper.ExportGenesis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(provState, again) {
		t.Fatal("providers export -> import -> export changed the state")
	}
	imported := msgkit.StoreSnapshot(ctx.KVStore(keys[providers.ModuleName]))
	if len(prefixPairs(t, committedProviders, providers.ProviderKeyPrefix)) == 0 ||
		!bytes.Equal(prefixPairs(t, committedProviders, providers.ProviderKeyPrefix), prefixPairs(t, imported, providers.ProviderKeyPrefix)) {
		t.Fatal("imported provider records differ from the committed store")
	}

	contState, err := content.ParseGenesis(exported[app.ModuleContent])
	if err != nil {
		t.Fatal(err)
	}
	if err := content.ValidateGenesis(contState); err != nil {
		t.Fatal(err)
	}
	contKeeper := content.NewKeeper(keys[content.ModuleName], content.Deps{Roles: fakeReads{}, Params: fakeReads{}, Limits: fakeReads{}})
	if err := contKeeper.InitGenesis(ctx, contState); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(contState, contKeeper.ExportGenesis(ctx)) {
		t.Fatal("content export -> import -> export changed the state")
	}
	importedContent := msgkit.StoreSnapshot(ctx.KVStore(keys[content.ModuleName]))
	for _, prefix := range []byte{content.ContentPrefix, content.TombstonePrefix} {
		if len(prefixPairs(t, committedContent, prefix)) == 0 ||
			!bytes.Equal(prefixPairs(t, committedContent, prefix), prefixPairs(t, importedContent, prefix)) {
			t.Fatalf("imported content prefix %#x differs from the committed store", prefix)
		}
	}
	// Business ids are not in the v0 genesis schema (DR-07-03): the
	// committed store has them, the import does not.
	if len(prefixPairs(t, committedContent, content.BusinessPrefix)) == 0 || len(prefixPairs(t, importedContent, content.BusinessPrefix)) != 0 {
		t.Fatal("business prefix expectation")
	}
}

func TestLaunchGenesisRejectsNonEmptyCatalog(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runRegistryFlow(t, n, c)
	exported, err := c.app().Export()
	if err != nil {
		t.Fatal(err)
	}
	empty := func(name string) json.RawMessage {
		var raw json.RawMessage
		var err error
		switch name {
		case app.ModuleProviders:
			raw, err = providers.MarshalGenesis(&providersv1.GenesisState{})
		case app.ModuleContent:
			raw, err = content.MarshalGenesis(&contentv1.GenesisState{})
		}
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	initChain := func(modules map[string]json.RawMessage) error {
		fresh := newTestApp(t, n.doc.ChainID, nil)
		params := consensusParams()
		_, err := fresh.InitChain(&abci.RequestInitChain{
			ChainId:         n.doc.ChainID,
			Time:            n.doc.Time(),
			AppStateBytes:   mustJSON(t, modules),
			InitialHeight:   1,
			Validators:      validatorUpdates(t, n.doc),
			ConsensusParams: &params,
		})
		return err
	}
	with := func(providersRaw, contentRaw json.RawMessage) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for k, v := range exported {
			out[k] = v
		}
		out[app.ModuleProviders] = providersRaw
		out[app.ModuleContent] = contentRaw
		return out
	}
	if err := initChain(with(exported[app.ModuleProviders], empty(app.ModuleContent))); err == nil {
		t.Fatal("InitChain accepted a non-empty provider catalogue")
	}
	if err := initChain(with(empty(app.ModuleProviders), exported[app.ModuleContent])); err == nil {
		t.Fatal("InitChain accepted a non-empty content catalogue")
	}
	// The same exported state with an empty catalogue starts: the catalogue
	// is the only reason for the two rejections above.
	if err := initChain(with(empty(app.ModuleProviders), empty(app.ModuleContent))); err != nil {
		t.Fatalf("exported state with an empty catalogue: %v", err)
	}
}
