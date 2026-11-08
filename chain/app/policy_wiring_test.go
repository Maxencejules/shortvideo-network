package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"cosmossdk.io/log/v2"

	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const governanceGolden = "testdata/governance-events.golden.json"

func scheduledPayload(key string, value, activation uint64) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
		GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value, ActivationHeight: activation},
	}}
}

func upgradePayload(name string, height uint64, release string) *authorityv1.ProposalPayload {
	sum := sha256.Sum256([]byte(release))
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_SoftwareUpgrade{
		SoftwareUpgrade: &authorityv1.SoftwareUpgradeChange{Name: name, Height: height, ReleaseDigest: digestOf(sum[:])},
	}}
}

func cancelPayload(name string) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_CancelUpgrade{
		CancelUpgrade: &authorityv1.CancelUpgradeChange{Name: name},
	}}
}

func seatPayload(seatID string, pub []byte) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_CouncilSeat{
		CouncilSeat: &authorityv1.CouncilSeatChange{SeatId: seatID, NewPublicKey: pub},
	}}
}

// councilBlocks is the number of blocks n.council takes: propose, three
// approvals, and the execute, one transaction per block.
const councilBlocks = 5

// governanceFlow is what runGovernanceFlow leaves behind.
type governanceFlow struct {
	activation uint64
	newSeatKey []byte
}

// runGovernanceFlow executes, through real signed transactions on c, one
// council proposal of every governance kind and reaches the activation
// height of a scheduled change:
//
//   - gov-imm: max_open_offers set to 7 at once
//   - gov-sched: max_open_offers set to 9 at activation height a (two blocks
//     after its execute); the flow then commits blocks up to a
//   - gov-sched-2, then gov-imm-2: max_page_limit scheduled far ahead, then
//     set to 50 at once, which deletes the pending change (superseded)
//   - up-1, then up-2: an upgrade plan, replaced by a plan of another name
//   - cancel-up-2: the plan up-2 cancelled before its height
//   - seat-4: council seat seat-4 gets a new key
func runGovernanceFlow(t *testing.T, n *regNet, c *cluster) governanceFlow {
	t.Helper()
	var flow governanceFlow
	n.council(t, c, "gov-imm", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 7))
	flow.activation = uint64(c.height()) + councilBlocks + 2
	n.council(t, c, "gov-sched", msgkit.LimitMaxOpenOffers, scheduledPayload(msgkit.LimitMaxOpenOffers, 9, flow.activation))
	for uint64(c.height()) < flow.activation {
		c.block(t)
	}
	far := uint64(c.height()) + councilBlocks + 1000
	n.council(t, c, "gov-sched-2", msgkit.LimitMaxPageLimit, scheduledPayload(msgkit.LimitMaxPageLimit, 40, far))
	n.council(t, c, "gov-imm-2", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 50))
	planAt := uint64(c.height()) + councilBlocks + msgkit.MinActivationLeadBlocks + 10
	n.council(t, c, "up-1", "upgrade", upgradePayload("up-1", planAt, "release up-1"))
	n.council(t, c, "up-2", "upgrade", upgradePayload("up-2", planAt+5, "release up-2"))
	n.council(t, c, "cancel-up-2", "upgrade", cancelPayload("up-2"))
	flow.newSeatKey = sdked.GenPrivKey().PubKey().Bytes()
	n.council(t, c, "seat-4-key", "seat-seat-4", seatPayload("seat-4", flow.newSeatKey))
	return flow
}

// governanceEventKeys are the attribute keys of the untyped governance
// events of x/policy (DR-10-22, DR-10-40), in emission order.
var governanceEventKeys = map[string][]string{
	policy.EventPolicyPending:   {policy.AttrParameterKey, policy.AttrValue, policy.AttrActivationHeight, policy.AttrProposalID, policy.AttrSupersededProposalID},
	policy.EventPolicyActivated: {policy.AttrParameterKey, policy.AttrValue, policy.AttrActivationHeight, policy.AttrProposalID},
	policy.EventUpgradePlan:     {policy.AttrName, policy.AttrHeight, policy.AttrReleaseDigest, policy.AttrProposalID, policy.AttrAction, policy.AttrReplacedName},
}

// TestGovernanceEventSchemas runs the governance flow and pins, in a golden
// file, every governance event of the committed blocks in order: the typed
// svn.events.v1.PolicyChanged and ProposalExecuted of each of the eight
// executes (D-2; each parses strictly with msgkit.ParseCommitted and ends
// with msg_index), and the untyped policy_pending (two schedules and one
// supersede), policy_activated (BeginBlock at the activation height), and
// upgrade_plan (scheduled, replaced, cancelled) events with exactly their
// attribute keys. The three earlier golden files are separate. Run with
// -update to rewrite the golden file.
func TestGovernanceEventSchemas(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runGovernanceFlow(t, n, c)

	var got []goldenEvent
	counts := map[string]int{}
	for _, ev := range allEvents(c) {
		row := goldenEvent{Type: ev.Type}
		switch {
		case ev.Type == string(proto.MessageName(&eventsv1.PolicyChanged{})) || ev.Type == string(proto.MessageName(&eventsv1.ProposalExecuted{})):
			msg, index, err := msgkit.ParseCommitted(ev)
			if err != nil || index != 0 {
				t.Fatalf("%s: msg_index %d: %v", ev.Type, index, err)
			}
			if string(msg.ProtoReflect().Descriptor().FullName()) != ev.Type {
				t.Fatalf("event type %s parsed as %s", ev.Type, msg.ProtoReflect().Descriptor().FullName())
			}
			for _, attr := range ev.Attributes[:len(ev.Attributes)-1] {
				row.Attributes = append(row.Attributes, goldenAttr{Key: attr.Key, Value: attr.Value})
			}
		case governanceEventKeys[ev.Type] != nil:
			var keys []string
			for _, attr := range ev.Attributes {
				if attr.Key == "msg_index" || attr.Key == "mode" {
					continue
				}
				keys = append(keys, attr.Key)
				row.Attributes = append(row.Attributes, goldenAttr{Key: attr.Key, Value: attr.Value})
			}
			if strings.Join(keys, ",") != strings.Join(governanceEventKeys[ev.Type], ",") {
				t.Fatalf("%s keys %v, want %v", ev.Type, keys, governanceEventKeys[ev.Type])
			}
		default:
			continue
		}
		counts[ev.Type]++
		got = append(got, row)
	}
	want := map[string]int{
		string(proto.MessageName(&eventsv1.PolicyChanged{})):    8,
		string(proto.MessageName(&eventsv1.ProposalExecuted{})): 8,
		policy.EventPolicyPending:                               3,
		policy.EventPolicyActivated:                             1,
		policy.EventUpgradePlan:                                 3,
	}
	if len(counts) != len(want) {
		t.Fatalf("event counts %v want %v", counts, want)
	}
	for name, count := range want {
		if counts[name] != count {
			t.Fatalf("event counts %v want %v", counts, want)
		}
	}
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(governanceGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(governanceGolden, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(governanceGolden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(golden, raw) {
		t.Fatalf("governance events differ from %s; rerun with -update after a reviewed schema change\n%s", governanceGolden, raw)
	}
}

// queryPolicy runs Query/Policy through ABCI on the committed state.
func queryPolicy(t *testing.T, chainApp *app.App) *policyv1.PolicyState {
	t.Helper()
	raw, res := query(t, chainApp, "/svn.policy.v1.Query/Policy", &policyv1.QueryPolicyRequest{}, 0)
	if res.Code != 0 {
		t.Fatalf("Query/Policy: %s/%d %s", res.Codespace, res.Code, res.Log)
	}
	var out policyv1.QueryPolicyResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.GetState()
}

// TestPolicyQueryThroughABCI: svn.policy.v1.Query/Policy is registered on
// the gRPC query router and answers through ABCI. On a new chain it returns
// policy version 1, the protocol safety caps, the eight blocked decision
// rows of the authority genesis, every governed limit at its protocol
// ceiling, no pending change, no plan, and app version 1. After the
// governance flow it returns the executed values (max_open_offers 9 after
// its activation, max_page_limit 50), no pending change (the scheduled
// change applied, the other was superseded), no plan (cancelled), and the
// policy version after eight executes.
func TestPolicyQueryThroughABCI(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	fresh := queryPolicy(t, c.app())
	if fresh.GetPolicyVersion() != 1 || fresh.GetAppVersion() != app.GenesisAppVersion || len(fresh.GetBlockedDecisions()) != len(genesis.DecisionIDs) ||
		len(fresh.GetPendingLimitChanges()) != 0 || fresh.GetUpgradePlan() != nil {
		t.Fatalf("fresh policy %v", fresh)
	}
	if !proto.Equal(fresh.GetSafetyCaps(), policy.DefaultSafetyCaps()) {
		t.Fatalf("safety caps %v", fresh.GetSafetyCaps())
	}
	if len(fresh.GetGovernedLimits()) != 5 {
		t.Fatalf("%d governed limits", len(fresh.GetGovernedLimits()))
	}
	for _, row := range fresh.GetGovernedLimits() {
		ceiling, known := msgkit.Ceiling(row.GetLimitKey())
		if !known || row.GetValue() != ceiling {
			t.Fatalf("fresh governed limit %s = %d, ceiling %d", row.GetLimitKey(), row.GetValue(), ceiling)
		}
	}
	runGovernanceFlow(t, n, c)
	after := queryPolicy(t, c.app())
	if after.GetPolicyVersion() != n.policy || n.policy != 9 {
		t.Fatalf("policy version %d, tracked %d, want 9", after.GetPolicyVersion(), n.policy)
	}
	values := map[string]uint64{}
	for _, row := range after.GetGovernedLimits() {
		values[row.GetLimitKey()] = row.GetValue()
	}
	if values[msgkit.LimitMaxOpenOffers] != 9 || values[msgkit.LimitMaxPageLimit] != 50 {
		t.Fatalf("governed limits after the flow %v", values)
	}
	if len(after.GetPendingLimitChanges()) != 0 || after.GetUpgradePlan() != nil || after.GetAppVersion() != app.GenesisAppVersion {
		t.Fatalf("policy after the flow %v", after)
	}
}

// TestGovernanceQueryThroughABCI: svn.authority.v1.Query/Governance is
// registered on the gRPC query router and answers through ABCI with the
// council state, a page, and the devnet test parameters in effect (the
// devnet chain's five rows); a limit above the max_page_limit cap is
// ERROR_CODE_BOUNDS.
func TestGovernanceQueryThroughABCI(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	raw, res := query(t, c.app(), "/svn.authority.v1.Query/Governance", &authorityv1.QueryGovernanceRequest{
		Page: &typesv1.PageRequest{Limit: 100},
	}, 0)
	if res.Code != 0 {
		t.Fatalf("Query/Governance: %s/%d %s", res.Codespace, res.Code, res.Log)
	}
	var out authorityv1.QueryGovernanceResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	state := out.GetState()
	if len(state.GetDevnetTestSeats()) != 4 || len(state.GetValidators()) != len(n.doc.Validators) || len(state.GetSubmitters()) != len(n.doc.Submitters) {
		t.Fatalf("governance state %v", state)
	}
	if len(out.GetDevnetTestParameters()) != len(n.doc.DevnetTestParameters) {
		t.Fatalf("%d devnet test parameters, want %d", len(out.GetDevnetTestParameters()), len(n.doc.DevnetTestParameters))
	}
	_, over := query(t, c.app(), "/svn.authority.v1.Query/Governance", &authorityv1.QueryGovernanceRequest{
		Page: &typesv1.PageRequest{Limit: genesis.MaxPageLimit + 1},
	}, 0)
	if over.Code == 0 || !strings.Contains(over.Log, string(msgkit.Bounds)) {
		t.Fatalf("limit above the cap: code %d log %s", over.Code, over.Log)
	}
}

// TestGenesisPolicyRules: InitChain refuses, before any module writes, a
// policy genesis whose blocked decision rows differ from the authority
// genesis rows (another order, or a changed row) or that sets a query field
// of schema version 2 (app_version, governed_limits), and genesis consensus
// params whose Version.App is not app.GenesisAppVersion (2, 0, no version
// params, or no consensus params). An accepted InitChain writes the
// binary's module version map and app version 1 into x/policy, and the
// policy version is 1.
func TestGenesisPolicyRules(t *testing.T) {
	n := newRegNet()
	codec := codecFromApp(t, n.doc.ChainID)
	base, err := app.AppStateFromDocument(codec, n.doc)
	if err != nil {
		t.Fatal(err)
	}
	editPolicy := func(edit func(*policyv1.GenesisState)) map[string]json.RawMessage {
		t.Helper()
		modules := map[string]json.RawMessage{}
		for name, raw := range base {
			modules[name] = raw
		}
		state, err := policy.ParseGenesis(base[app.ModulePolicy])
		if err != nil {
			t.Fatal(err)
		}
		edit(state)
		raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		modules[app.ModulePolicy] = raw
		return modules
	}
	for _, c := range []struct {
		name    string
		modules map[string]json.RawMessage
		want    string
	}{
		{"decision rows reordered", editPolicy(func(s *policyv1.GenesisState) {
			rows := s.Policy.BlockedDecisions
			rows[0], rows[1] = rows[1], rows[0]
		}), "policy genesis blocked decision row 0 (DEC-002) differs from the authority genesis row (DEC-001)"},
		{"decision row changed", editPolicy(func(s *policyv1.GenesisState) {
			s.Policy.BlockedDecisions[3].SchemaVersion = 2
		}), "DEC-004 is not blocked"},
		{"app version set", editPolicy(func(s *policyv1.GenesisState) { s.Policy.AppVersion = 1 }), "app_version 1"},
		{"governed limit set", editPolicy(func(s *policyv1.GenesisState) {
			s.Policy.GovernedLimits = []*policyv1.GovernedLimitValue{{LimitKey: msgkit.LimitMaxOpenOffers, Value: 1}}
		}), "genesis carries no governed value"},
	} {
		t.Run(c.name, func(t *testing.T) {
			chainApp, err := initChain(t, n.doc, c.modules, 1)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("InitChain: %v, want %q", err, c.want)
			}
			for _, store := range []string{app.ModulePolicy, authority.ModuleName, app.ModuleProviders} {
				if keys := chainApp.FinalizeStoreRowsForTest(store); keys != 0 {
					t.Fatalf("InitChain refused but wrote %d %s rows", keys, store)
				}
			}
		})
	}
	for _, c := range []struct {
		name   string
		params func(*abci.RequestInitChain)
	}{
		{"Version.App 2", func(req *abci.RequestInitChain) { req.ConsensusParams.Version.App = 2 }},
		{"Version.App 0", func(req *abci.RequestInitChain) { req.ConsensusParams.Version.App = 0 }},
		{"no version params", func(req *abci.RequestInitChain) { req.ConsensusParams.Version = nil }},
		{"no consensus params", func(req *abci.RequestInitChain) { req.ConsensusParams = nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			chainApp := newTestApp(t, n.doc.ChainID, nil)
			params := consensusParams()
			req := &abci.RequestInitChain{
				ChainId: n.doc.ChainID, Time: n.doc.Time(), AppStateBytes: mustJSON(t, base),
				InitialHeight: 1, Validators: validatorUpdates(t, n.doc), ConsensusParams: &params,
			}
			c.params(req)
			if _, err := chainApp.InitChain(req); err == nil || !strings.Contains(err.Error(), "genesis consensus params Version.App must be 1") {
				t.Fatalf("InitChain: %v", err)
			}
		})
	}
	chainApp, err := initChain(t, n.doc, base, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chainApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: n.doc.Time(), Hash: bytes32(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := chainApp.Commit(); err != nil {
		t.Fatal(err)
	}
	ctx := chainApp.CommittedContextForTest()
	keeper := chainApp.PolicyKeeperForTest()
	if got := keeper.AppVersion(ctx); got != app.GenesisAppVersion {
		t.Fatalf("stored app version %d", got)
	}
	stored := keeper.VersionMap(ctx)
	declared := chainApp.Binary().Modules
	if len(stored) != len(declared) || len(stored) != 8 || stored[authorityAuthModule] != 7 {
		t.Fatalf("stored version map %v, binary %v", stored, declared)
	}
	for name, version := range declared {
		if stored[name] != version {
			t.Fatalf("stored version map %v, binary %v", stored, declared)
		}
	}
	if got := chainApp.AppVersion(); got != app.GenesisAppVersion {
		t.Fatalf("ABCI Info app version %d after the first commit", got)
	}
}

// authorityAuthModule is the auth module's name in the version map; svnd
// starts it at consensus version 7 (DR-10-24).
const authorityAuthModule = "auth"

// TestExportRefusesGovernanceState: App.Export refuses, with "export
// refused:" and the policy prefix, once the policy store holds a governed
// value (an executed immediate change) or a pending change (a scheduled one
// before its height), the same way it refuses authority side state; a new
// chain still exports. Every council execute also leaves authority side
// state, which the authority check refuses first, so the policy check is
// also shown alone: a governed value written by a test hook on a new chain
// (authority store untouched) is refused with the policy message instead of
// the policy module export panicking.
func TestExportRefusesGovernanceState(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	if _, err := c.app().Export(); err != nil {
		t.Fatalf("fresh export: %v", err)
	}
	alone := newRegNet()
	a := alone.start(t, 1)
	a.app().WriteCommittedRowForTest(app.ModulePolicy, policy.GovernedKey(msgkit.LimitMaxOpenOffers), policy.EncodeU64(7))
	if _, err := a.app().Export(); err == nil || !strings.HasPrefix(err.Error(), "export refused: policy export refused: the store holds governed values (prefix 0x02)") {
		t.Fatalf("export with a governed value and no authority side state: %v", err)
	}
	if err := c.app().PolicyKeeperForTest().ExportCheck(c.app().CommittedContextForTest()); err != nil {
		t.Fatalf("fresh policy export check: %v", err)
	}
	n.council(t, c, "gov-imm", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 7))
	if _, err := c.app().Export(); err == nil || !strings.HasPrefix(err.Error(), "export refused:") {
		t.Fatalf("export after a council execute: %v", err)
	}
	err := c.app().PolicyKeeperForTest().ExportCheck(c.app().CommittedContextForTest())
	if err == nil || !strings.Contains(err.Error(), "governed values (prefix 0x02)") {
		t.Fatalf("policy export check with a governed value: %v", err)
	}
	m := newRegNet()
	d := m.start(t, 1)
	m.council(t, d, "gov-sched", msgkit.LimitMaxOpenOffers, scheduledPayload(msgkit.LimitMaxOpenOffers, 9, uint64(d.height())+councilBlocks+100))
	err = d.app().PolicyKeeperForTest().ExportCheck(d.app().CommittedContextForTest())
	if err == nil || !strings.Contains(err.Error(), "pending governed changes (prefix 0x03)") {
		t.Fatalf("policy export check with a pending change: %v", err)
	}
	if _, err := d.app().Export(); err == nil || !strings.HasPrefix(err.Error(), "export refused:") {
		t.Fatalf("export with a pending change: %v", err)
	}
}

// TestCouncilSeatCreatesTheNewKeyAccount: an executed council seat payload
// creates the auth account of the new key's address in the execute
// (DR-10-35), through the app's account adapter; the address had no account
// before.
func TestCouncilSeatCreatesTheNewKeyAccount(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	pub := sdked.GenPrivKey().PubKey().Bytes()
	addr := authority.SeatAddress(pub)
	if c.app().AccountExistsForTest(addr) {
		t.Fatal("the new key's address has an account before the execute")
	}
	n.councilReady(t, c, "seat-4-key", "seat-seat-4", seatPayload("seat-4", pub))
	if c.app().AccountExistsForTest(addr) {
		t.Fatal("approvals created the account")
	}
	res := n.execute(t, c, "seat-4-key")
	wantCode(t, "execute seat-4-key", res, msgkit.OK)
	if code := authorityResultCode(res); code != "" {
		t.Fatalf("execute kept with %s", code)
	}
	if !c.app().AccountExistsForTest(addr) {
		t.Fatal("the executed council seat payload created no account for the new key")
	}
}

// TestUpgradePostConditionClasses pins the upgrade runner's I13 checks of
// the app, in order: every authority prefix class, the other authority rows
// (everything but the policy version, which an upgrade moves by one), the
// x/policy safety caps, governed values, pending changes, applied upgrades,
// and plan names, and the other x/policy rows (everything but the rows the
// runner checks itself: the layout key, the app version, the version map,
// and the plan).
func TestUpgradePostConditionClasses(t *testing.T) {
	got := newTestApp(t, "shortvideo-dev-i13", nil).PostConditionNamesForTest()
	want := []string{
		"authority layout version",
		"authority config",
		"authority admission rows",
		"authority pending validator changes",
		"authority proposals",
		"authority business ids",
		"authority executed digests",
		"authority open counts",
		"authority retired seat keys",
		"authority retired service addresses",
		"authority other rows",
		"policy safety caps",
		"policy governed values",
		"policy pending changes",
		"policy applied upgrades",
		"policy plan names",
		"policy other rows",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("I13 classes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestGenesisSeatRulesD7D10: the document rule (genesis.Validate) and the
// authority genesis rule use one seat id bound, and InitChain refuses a
// 60-character seat id (D-10) and a seat key that is a validator consensus
// key (D-7) through genesis.Validate, before any module writes.
func TestGenesisSeatRulesD7D10(t *testing.T) {
	if genesis.MaxSeatIDLength != authority.MaxSeatIDLength {
		t.Fatalf("genesis seat id bound %d, authority %d", genesis.MaxSeatIDLength, authority.MaxSeatIDLength)
	}
	for _, c := range []struct {
		name string
		edit func(*genesis.Document)
		want string
	}{
		{"seat id of 60 characters", func(doc *genesis.Document) {
			doc.DevnetTestSeats[0].SeatID = strings.Repeat("s", genesis.MaxSeatIDLength+1)
		}, "is longer than 59 characters"},
		{"seat key is a validator key", func(doc *genesis.Document) {
			doc.DevnetTestSeats[2].PublicKeyHex = doc.Validators[1].ConsensusPubkeyHex
		}, "key is a validator consensus key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			n := newRegNet()
			good, err := app.AppStateFromDocument(codecFromApp(t, n.doc.ChainID), n.doc)
			if err != nil {
				t.Fatal(err)
			}
			doc := n.doc
			doc.DevnetTestSeats = append([]genesis.DevnetTestSeat(nil), n.doc.DevnetTestSeats...)
			c.edit(&doc)
			if err := genesis.Validate(doc, genesis.CometIdentity{}); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("genesis.Validate: %v", err)
			}
			modules := map[string]json.RawMessage{}
			for name, raw := range good {
				modules[name] = raw
			}
			chainApp := newTestApp(t, n.doc.ChainID, nil)
			params := consensusParams()
			modules[authority.ModuleName] = seatsReplaced(t, good[authority.ModuleName], doc.DevnetTestSeats)
			_, err = chainApp.InitChain(&abci.RequestInitChain{
				ChainId: n.doc.ChainID, Time: n.doc.Time(), AppStateBytes: mustJSON(t, modules),
				InitialHeight: 1, Validators: validatorUpdates(t, n.doc), ConsensusParams: &params,
			})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("InitChain: %v, want %q", err, c.want)
			}
			for _, store := range []string{app.ModulePolicy, authority.ModuleName, app.ModuleProviders} {
				if keys := chainApp.FinalizeStoreRowsForTest(store); keys != 0 {
					t.Fatalf("InitChain refused but wrote %d %s rows", keys, store)
				}
			}
		})
	}
}

// seatsReplaced is the authority module genesis raw with its devnet test
// seats replaced by seats (protojson, proto field names).
func seatsReplaced(t *testing.T, raw json.RawMessage, seats []genesis.DevnetTestSeat) json.RawMessage {
	t.Helper()
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	rows := make([]*authorityv1.DevnetTestSeat, len(seats))
	for i, seat := range seats {
		pub, err := hex.DecodeString(seat.PublicKeyHex)
		if err != nil {
			t.Fatal(err)
		}
		rows[i] = &authorityv1.DevnetTestSeat{SeatId: seat.SeatID, PublicKey: pub}
	}
	state.Authority.DevnetTestSeats = rows
	out, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&state)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPreBlockReturnsAnErrorOnADamagedAppVersion: the upgrade step runs
// first in PreBlock and reads the stored app version (x/policy 0x04) every
// block. A stored value of 3 bytes, written to the working state outside
// any transaction, makes FinalizeBlock return an error naming the upgrade
// step (the keeper's panic becomes that error) instead of panicking, a
// second attempt at the same height fails the same way, and nothing is
// committed.
func TestPreBlockReturnsAnErrorOnADamagedAppVersion(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	node := c.nodes[0]
	node.app.WriteWorkingRowForTest(app.ModulePolicy, []byte{0x04}, []byte{1, 2, 3})
	committed := node.app.LastCommitID()
	for attempt := 0; attempt < 2; attempt++ {
		_, err := node.app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: node.height + 1, Time: node.when, Hash: bytes32(byte(node.height + 1))})
		if err == nil || !strings.Contains(err.Error(), "upgrade step at height ") || !strings.Contains(err.Error(), "committed state does not decode") {
			t.Fatalf("attempt %d: FinalizeBlock on a damaged app version: %v", attempt, err)
		}
		if got := node.app.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
			t.Fatalf("attempt %d: the commit moved to %d", attempt, got.Version)
		}
	}
}

// TestInitChainRefusesALayoutTheVersionMapDoesNotName: InitChain checks,
// after every module's InitGenesis, that each store with a layout key holds
// the version the binary's version map names. A binary whose leases module
// reports consensus version 3 (BumpModuleVersionForTest; app version still
// 1) while the leases InitGenesis writes layout 2 is refused, so the
// persisted version map can never disagree with the stores it describes.
// The same document initializes the unchanged binary.
func TestInitChainRefusesALayoutTheVersionMapDoesNotName(t *testing.T) {
	n := newRegNet()
	modules, err := app.AppStateFromDocument(codecFromApp(t, n.doc.ChainID), n.doc)
	if err != nil {
		t.Fatal(err)
	}
	bumped, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: dbm.NewMemDB(), ChainID: n.doc.ChainID, SkipLoad: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := bumped.BumpModuleVersionForTest(app.ModuleLeases, func(sdk.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := bumped.LoadForTest(); err != nil {
		t.Fatal(err)
	}
	if got := bumped.Binary(); got.AppVersion != app.GenesisAppVersion || got.Modules[app.ModuleLeases] != 3 {
		t.Fatalf("bumped binary %+v", got)
	}
	params := consensusParams()
	_, err = bumped.InitChain(&abci.RequestInitChain{
		ChainId: n.doc.ChainID, Time: n.doc.Time(), AppStateBytes: mustJSON(t, modules),
		InitialHeight: 1, Validators: validatorUpdates(t, n.doc), ConsensusParams: &params,
	})
	if err == nil || !strings.Contains(err.Error(), "leases store after genesis has layout version 2 (found true); its module writes 2 and this binary's version map says 3") {
		t.Fatalf("InitChain with a version map that names leases 3: %v", err)
	}
	if _, err := initChain(t, n.doc, modules, 1); err != nil {
		t.Fatalf("the unchanged binary: %v", err)
	}
}
