package app_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
)

// govTx is one transaction of a governance test block: its signer and
// message.
type govTx struct {
	who *actor
	msg sdk.Msg
}

func (n *regNet) blockTxs(t *testing.T, c *cluster, txs []govTx) *abci.ResponseFinalizeBlock {
	t.Helper()
	whos := make([]*actor, len(txs))
	msgs := make([]sdk.Msg, len(txs))
	for i, tx := range txs {
		whos[i], msgs[i] = tx.who, tx.msg
	}
	return n.blockOf(t, c, whos, msgs)
}

// wantIdempotent requires a replayed governance transaction: code 0, an
// authority_result event with idempotent=true, and no typed event.
func wantIdempotent(t *testing.T, label string, res *abci.ExecTxResult) {
	t.Helper()
	wantCode(t, label, res, msgkit.OK)
	if !hasIdempotentEvent(res, authority.ModuleName) {
		t.Fatalf("%s: not marked idempotent: %v", label, res.Events)
	}
	if typed := typedEvents(res.Events); len(typed) != 0 {
		t.Fatalf("%s: the replay emitted %d typed events", label, len(typed))
	}
}

// wantTransition requires the first transaction of a business id: code 0
// and no idempotent mark.
func wantTransition(t *testing.T, label string, res *abci.ExecTxResult) {
	t.Helper()
	wantCode(t, label, res, msgkit.OK)
	if hasIdempotentEvent(res, authority.ModuleName) {
		t.Fatalf("%s: the first transaction is marked idempotent", label)
	}
}

// TestReplayGovernanceBusinessIDs (REQ-INV-003, governance half): one
// business id and payload delivered in two transactions with distinct
// bytes (another sequence, or another seat's signature for a propose,
// whose business digest does not cover the signer) gives one transition.
// A replay chain (a goleveldb node and a memdb node that cluster.block
// keeps at one app hash) receives every first transaction and its replays;
// a reference chain from the same genesis receives the first transactions
// only, at the same heights and header times. After every block the full
// non-account state and the set of auth accounts of the two chains are
// equal (an account number depends on the chain's app hash, so numbers are
// not compared), so no replay changed anything; each replay is code 0 with
// authority_result idempotent=true and no typed event, and only the first
// execute of a proposal emits PolicyChanged and ProposalExecuted. Covered:
// propose (same seat twice in one block; two seats in one block, both
// orders), approve and execute (twice in one block, again in a later
// block), and the execute of a scheduled governed limit, a software
// upgrade, a cancel, and a council seat change (the seat account is
// created once). A changed payload, or another seat's approval, under a
// used business id, and a business id an x/providers registration took
// (and the reverse), are ERROR_CODE_REPLAY with the state unchanged. The
// goleveldb node is then stopped and reopened (load gate row 1,
// Query/Policy and the full state unchanged), and the same replays after
// the restart are still idempotent. The replay-after-upgrade part (a pre-h
// business id after an applied upgrade) needs the rehearsal binary and is
// P5a's (plan 12.4).
func TestReplayGovernanceBusinessIDs(t *testing.T) {
	n := newRegNet()
	dir := t.TempDir()
	disk, db := openDiskApp(t, n.doc.ChainID, dir)
	modules, err := app.AppStateFromDocument(disk.EncodingConfig().Codec, n.doc)
	if err != nil {
		t.Fatal(err)
	}
	params := consensusParams()
	if _, err := disk.InitChain(&abci.RequestInitChain{
		ChainId: n.doc.ChainID, Time: n.doc.Time(), AppStateBytes: mustJSON(t, modules),
		InitialHeight: 1, Validators: validatorUpdates(t, n.doc), ConsensusParams: &params,
	}); err != nil {
		t.Fatal(err)
	}
	c := &cluster{nodes: []*running{{app: disk, params: params, when: n.doc.Time()}, bootWith(t, n.doc, nil)}}
	c.block(t)
	refCopy := *n
	refCopy.seq = map[string]uint64{}
	r := &refCopy
	ref := r.start(t, 1)

	same := func(label string) {
		t.Helper()
		if !bytes.Equal(c.app().FullSnapshot(), ref.app().FullSnapshot()) {
			t.Fatalf("%s: the replay chain's full state differs from the reference chain", label)
		}
		got, want := c.app().AccountAddressesForTest(), ref.app().AccountAddressesForTest()
		if len(got) != len(want) {
			t.Fatalf("%s: %d accounts on the replay chain, %d on the reference chain", label, len(got), len(want))
		}
		for i := range got {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("%s: account %d is %x on the replay chain, %x on the reference chain", label, i, got[i], want[i])
			}
		}
	}
	// step delivers one block on each chain: replay on the replay chain,
	// first on the reference chain.
	step := func(label string, replay, first []govTx) []*abci.ExecTxResult {
		t.Helper()
		res := n.blockTxs(t, c, replay)
		r.blockTxs(t, ref, first)
		same(label)
		return res.TxResults
	}
	both := func(label string, txs ...govTx) []*abci.ExecTxResult {
		t.Helper()
		res := step(label, txs, txs)
		for i, result := range res {
			wantCode(t, fmt.Sprintf("%s tx %d", label, i), result, msgkit.OK)
		}
		return res
	}
	executed := func() {
		n.policy++
		r.policy++
	}
	s1, s2, s3 := n.seats[0], n.seats[1], n.seats[2]
	far := nextHeight(c) + 10_000

	both("register p1", govTx{n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)})

	p := n.proposal(t, "rp-p", msgkit.LimitMaxOpenOffers, scheduledPayload(msgkit.LimitMaxOpenOffers, 9, far))
	res := step("propose rp-p twice in one block", []govTx{{s1, p}, {s1, p}}, []govTx{{s1, p}})
	wantTransition(t, "propose rp-p", res[0])
	wantIdempotent(t, "propose rp-p again", res[1])

	q := n.proposal(t, "rp-q", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 50))
	q2 := n.govProposal(t, s2, "rp-q", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 50))
	res = step("propose rp-q by seat-1 then seat-2", []govTx{{s1, q}, {s2, q2}}, []govTx{{s1, q}})
	wantTransition(t, "propose rp-q by seat-1", res[0])
	wantIdempotent(t, "propose rp-q by seat-2", res[1])
	rq := n.proposal(t, "rp-r", msgkit.LimitMaxPackageBytes, governedPayload(msgkit.LimitMaxPackageBytes, 1000))
	rq2 := n.govProposal(t, s2, "rp-r", msgkit.LimitMaxPackageBytes, governedPayload(msgkit.LimitMaxPackageBytes, 1000))
	res = step("propose rp-r by seat-2 then seat-1", []govTx{{s2, rq2}, {s1, rq}}, []govTx{{s2, rq2}})
	wantTransition(t, "propose rp-r by seat-2", res[0])
	wantIdempotent(t, "propose rp-r by seat-1", res[1])

	ap := approval(s1, "rp-p-ap-1", "rp-p")
	res = step("approve rp-p twice in one block", []govTx{{s1, ap}, {s1, ap}}, []govTx{{s1, ap}})
	wantTransition(t, "approve rp-p", res[0])
	wantIdempotent(t, "approve rp-p again", res[1])
	res = step("approve rp-p in a later block", []govTx{{s1, ap}}, nil)
	wantIdempotent(t, "approve rp-p in a later block", res[0])
	both("approve rp-p by seats 2 and 3", govTx{s2, approval(s2, "rp-p-ap-2", "rp-p")}, govTx{s3, approval(s3, "rp-p-ap-3", "rp-p")})

	ex := execution(n.authority, "ex-rp-p", "rp-p")
	res = step("execute rp-p twice in one block", []govTx{{n.authority, ex}, {n.authority, ex}}, []govTx{{n.authority, ex}})
	executed()
	wantTransition(t, "execute rp-p", res[0])
	if typed := typedEvents(res[0].Events); len(typed) != 2 {
		t.Fatalf("execute rp-p: %d typed events, want PolicyChanged and ProposalExecuted", len(typed))
	}
	wantIdempotent(t, "execute rp-p again", res[1])
	res = step("execute rp-p in a later block", []govTx{{n.authority, ex}}, nil)
	wantIdempotent(t, "execute rp-p in a later block", res[0])

	changed := n.proposal(t, "rp-p2", msgkit.LimitMaxOpenOffers, scheduledPayload(msgkit.LimitMaxOpenOffers, 8, far))
	changed.BusinessId = p.BusinessId
	res = step("propose under a used business id, changed payload", []govTx{{s1, changed}}, nil)
	wantCode(t, "changed payload", res[0], msgkit.Replay)
	res = step("another seat's approval under a used business id", []govTx{{s2, approval(s2, "rp-p-ap-1", "rp-q")}}, nil)
	wantCode(t, "another seat's approval", res[0], msgkit.Replay)
	taken := n.proposal(t, "rp-taken", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 6))
	taken.BusinessId = "reg-p1"
	res = step("propose under a provider registration's business id", []govTx{{s1, taken}}, nil)
	wantCode(t, "provider business id", res[0], msgkit.Replay)
	res = step("content registration under a proposal's business id", []govTx{{n.service, n.registerContent(t, n.service, p.BusinessId, "rp-c1", 1, packageManifest("rp-c1"), sizeC1)}}, nil)
	wantCode(t, "proposal business id in x/content", res[0], msgkit.Replay)

	seatKey := sdked.GenPrivKey().PubKey().Bytes()
	for _, k := range []governanceKind{
		{"software upgrade", "upgrade", upgradePayload("rp-up", far, "release rp-up")},
		{"cancel upgrade", "upgrade", cancelPayload("rp-up")},
		{"council seat", "seat-seat-4", seatPayload("seat-4", seatKey)},
	} {
		id := "rp-" + strings.ReplaceAll(k.name, " ", "-")
		both(k.name+": propose", govTx{s1, n.proposal(t, id, k.key, k.payload)})
		for i, seat := range []*actor{s1, s2, s3} {
			both(k.name+": approve", govTx{seat, approval(seat, fmt.Sprintf("%s-ap-%d", id, i), id)})
		}
		kex := execution(n.authority, "ex-"+id, id)
		res = step(k.name+": execute twice in one block", []govTx{{n.authority, kex}, {n.authority, kex}}, []govTx{{n.authority, kex}})
		executed()
		wantTransition(t, k.name+": execute", res[0])
		wantIdempotent(t, k.name+": execute again", res[1])
		res = step(k.name+": execute in a later block", []govTx{{n.authority, kex}}, nil)
		wantIdempotent(t, k.name+": execute in a later block", res[0])
	}
	if !c.app().AccountExistsForTest(authority.SeatAddress(seatKey)) {
		t.Fatal("the council seat execute created no account")
	}
	state := queryPolicy(t, c.app())
	if state.GetPolicyVersion() != n.policy || len(state.GetPendingLimitChanges()) != 1 || state.GetUpgradePlan() != nil {
		t.Fatalf("policy before the restart (tracked version %d): %v", n.policy, state)
	}

	committed := disk.LastCommitID()
	snapshot := disk.FullSnapshot()
	policyBefore, _ := query(t, disk, "/svn.policy.v1.Query/Policy", &policyv1.QueryPolicyRequest{}, 0)
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	disk, db = openDiskApp(t, n.doc.ChainID, dir)
	defer func() { _ = db.Close() }()
	c.nodes[0].app = disk
	if got := disk.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
		t.Fatalf("reopened at %d %X, committed %d %X", got.Version, got.Hash, committed.Version, committed.Hash)
	}
	if !bytes.Equal(snapshot, disk.FullSnapshot()) {
		t.Fatal("the reopened state differs")
	}
	if policyAfter, _ := query(t, disk, "/svn.policy.v1.Query/Policy", &policyv1.QueryPolicyRequest{}, 0); !bytes.Equal(policyBefore, policyAfter) {
		t.Fatal("Query/Policy differs after the restart")
	}
	if decision := disk.GateDecisionForTest(); decision.Row != 1 || decision.Window {
		t.Fatalf("load gate decision after the restart %+v", decision)
	}
	res = step("replays after the restart", []govTx{{n.authority, ex}, {s1, p}, {s1, ap}, {n.authority, execution(n.authority, "ex-rp-council-seat", "rp-council-seat")}}, nil)
	for i, label := range []string{"execute rp-p", "propose rp-p", "approve rp-p", "execute the seat change"} {
		wantIdempotent(t, label+" after the restart", res[i])
	}
}

// policyStoreAt is every x/policy row as committed at height
// (StoreAtForTest). The layout version, safety caps, app version, and
// version map rows exist from InitChain on, so it is never empty.
func policyStoreAt(t *testing.T, chainApp *app.App, height int64) []byte {
	t.Helper()
	rows, err := chainApp.StoreAtForTest(app.ModulePolicy, height)
	if err != nil {
		t.Fatalf("policy store at height %d: %v", height, err)
	}
	if len(rows) == 0 {
		t.Fatalf("policy store at height %d is empty", height)
	}
	return rows
}

// snapshotRows decodes a msgkit.StoreSnapshot into its pairs.
func snapshotRows(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	rows := map[string][]byte{}
	for len(raw) > 0 {
		if len(raw) < 8 {
			t.Fatal("truncated snapshot")
		}
		keyLen := binary.BigEndian.Uint64(raw[:8])
		if uint64(len(raw)-8) < keyLen+8 {
			t.Fatal("truncated snapshot key")
		}
		key := raw[8 : 8+keyLen]
		raw = raw[8+keyLen:]
		valueLen := binary.BigEndian.Uint64(raw[:8])
		if uint64(len(raw)-8) < valueLen {
			t.Fatal("truncated snapshot value")
		}
		rows[string(key)] = raw[8 : 8+valueLen]
		raw = raw[8+valueLen:]
	}
	return rows
}

// governedExecutes are the parameter keys of the PolicyChanged events of
// the successful transactions of one block that name a governed limit key:
// the executed council governed-limit changes of that block.
func governedExecutes(t *testing.T, res *abci.ResponseFinalizeBlock) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, result := range res.TxResults {
		if result.Code != 0 {
			continue
		}
		for _, ev := range result.Events {
			if ev.Type != string(proto.MessageName(&eventsv1.PolicyChanged{})) {
				continue
			}
			msg, _, err := msgkit.ParseCommitted(ev)
			if err != nil {
				t.Fatal(err)
			}
			key := msg.(*eventsv1.PolicyChanged).GetParameterKey()
			if _, governed := msgkit.Ceiling(key); governed {
				keys[key] = true
			}
		}
	}
	return keys
}

// TestPolicyStoreUntouchedByOtherFlows (invariant I12): the x/policy store
// changes only through a council governed-limit, upgrade, or cancel
// execute, x/policy BeginBlock activation, and the upgrade runner. The
// registry flow (registrations, updates, a rotation, a drain, council
// admit, disable, and force-withdraw executes, withdrawals), the lease
// flow (offers, accepts, renewals, ends, a council disable, BeginBlock
// expiry), and an evidence flow (leases, a recorded and a rejected
// attestation) are read from the versioned x/policy store of every
// committed height: a row changes only in a block whose successful
// transactions include the execute of a governed limit change of key K,
// and then the only row that changes is K's governed value (0x02 || K).
// The registry and evidence flows change no row (their council executes
// still move the policy version, which x/authority holds); the lease flow
// changes one, at its council max_expiry_steps execute; a governed change
// executed after the registry flow is seen as one change.
func TestPolicyStoreUntouchedByOtherFlows(t *testing.T) {
	changes := func(t *testing.T, label string, c *cluster, from int64) int {
		t.Helper()
		if c.height()-from < 10 {
			t.Fatalf("%s: only %d blocks", label, c.height()-from)
		}
		prev := snapshotRows(t, policyStoreAt(t, c.app(), from))
		count := 0
		for h := from + 1; h <= c.height(); h++ {
			cur := snapshotRows(t, policyStoreAt(t, c.app(), h))
			var diff []string
			for key, value := range cur {
				if old, found := prev[key]; !found || !bytes.Equal(old, value) {
					diff = append(diff, key)
				}
			}
			for key := range prev {
				if _, found := cur[key]; !found {
					diff = append(diff, key)
				}
			}
			if len(diff) != 0 {
				executed := governedExecutes(t, c.blocks[h-1])
				if len(diff) != 1 || diff[0][0] != policy.GovernedPrefix || !executed[diff[0][1:]] {
					t.Fatalf("%s: height %d changed policy rows %q; governed executes in the block: %v", label, h, diff, executed)
				}
				count++
			}
			prev = cur
		}
		return count
	}
	t.Run("registry flow", func(t *testing.T) {
		n := newRegNet()
		c := n.start(t, 1)
		runRegistryFlow(t, n, c)
		if got := changes(t, "registry flow", c, 1); got != 0 {
			t.Fatalf("registry flow: %d policy changes", got)
		}
		if v := queryPolicy(t, c.app()).GetPolicyVersion(); v != n.policy || v == 1 {
			t.Fatalf("policy version %d after the registry flow, tracked %d", v, n.policy)
		}
		n.council(t, c, "pu-limit", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 50))
		if got := changes(t, "registry flow and a governed change", c, 1); got != 1 {
			t.Fatalf("a governed change after the registry flow: %d policy changes", got)
		}
	})
	t.Run("lease flow", func(t *testing.T) {
		n := newRegNet()
		c := n.start(t, 1)
		runLeaseFlow(t, n, c)
		if got := changes(t, "lease flow", c, 1); got != 1 {
			t.Fatalf("lease flow: %d policy changes, want 1 (its max_expiry_steps execute)", got)
		}
	})
	t.Run("evidence flow", func(t *testing.T) {
		e := newEvNet()
		c := e.start(t, 1)
		fx := evidenceSetup(t, e, c)
		auditor := prefix(e.auditor1)
		wantCode(t, "audit batch", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "pu-au", auditor+"pu-au", c.epoch(),
			auditItem(auditor+"pu-e1", "la", "c1", manifestOf("c1"), fx.setupNow+600),
			auditItem(auditor+"pu-e2", "lb", "c2", wrongDigest("lb"), fx.setupNow+600))), msgkit.OK)
		if got := changes(t, "evidence flow", c, 1); got != 0 {
			t.Fatalf("evidence flow: %d policy changes", got)
		}
	})
}

// TestGovernanceSimulateEstimatesGas (TestSimulateEstimatesGas for the
// schema-version-2 kinds): the propose, the three approvals, and the
// execute of a scheduled governed limit, a software upgrade, a cancel, and
// a council seat change each simulate on the committed state without
// changing it, and the simulated gas equals the gas of the same bytes
// committed.
func TestGovernanceSimulateEstimatesGas(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	step := func(label string, who *actor, msg sdk.Msg) {
		t.Helper()
		raw := n.signed(t, c, who, registryGas, nil, msg)
		before := takeGovSnap(t, chainApp)
		info, result, err := chainApp.Simulate(raw)
		if err != nil || result == nil {
			t.Fatalf("%s: simulate: %v", label, err)
		}
		before.same(t, label+" (simulated)", chainApp, nil)
		res := n.deliverRaw(t, c, who, raw)
		wantCode(t, label, res, msgkit.OK)
		if info.GasUsed != uint64(res.GasUsed) || info.GasUsed < msgkit.FixedMessageGas {
			t.Fatalf("%s: simulated gas %d, committed gas %d", label, info.GasUsed, res.GasUsed)
		}
		t.Logf("%s: simulated gas %d, committed gas %d", label, info.GasUsed, res.GasUsed)
	}
	far := nextHeight(c) + 10_000
	for _, k := range []governanceKind{
		{"scheduled limit", msgkit.LimitMaxOpenOffers, scheduledPayload(msgkit.LimitMaxOpenOffers, 9, far)},
		{"software upgrade", "upgrade", upgradePayload("sim-up", far, "release sim-up")},
		{"cancel upgrade", "upgrade", cancelPayload("sim-up")},
		{"council seat", "seat-seat-4", seatPayload("seat-4", sdked.GenPrivKey().PubKey().Bytes())},
	} {
		id := "sim-" + strings.ReplaceAll(k.name, " ", "-")
		step(k.name+": propose", n.seats[0], n.proposal(t, id, k.key, k.payload))
		for i := 0; i < 3; i++ {
			step(k.name+": approve", n.seats[i], approval(n.seats[i], fmt.Sprintf("%s-ap-%d", id, i), id))
		}
		step(k.name+": execute", n.authority, execution(n.authority, "ex-"+id, id))
		n.policy++
	}
}

// TestGenesisPolicyRefusesVersionAndScheduleFields (TestGenesisPolicyRules
// for the fields it does not cover): InitChain refuses, before any module
// writes, a policy genesis whose policy_version is not 1, or that carries
// a pending governed change or an upgrade plan (genesis carries none), with
// the x/policy, x/authority, and x/providers stores empty afterwards.
func TestGenesisPolicyRefusesVersionAndScheduleFields(t *testing.T) {
	n := newRegNet()
	base, err := app.AppStateFromDocument(codecFromApp(t, n.doc.ChainID), n.doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*policyv1.GenesisState)
		want string
	}{
		{"policy version 2", func(s *policyv1.GenesisState) { s.Policy.PolicyVersion = 2 }, "policy_version 2, want 1"},
		{"policy version 0", func(s *policyv1.GenesisState) { s.Policy.PolicyVersion = 0 }, "policy_version 0, want 1"},
		{"pending change", func(s *policyv1.GenesisState) {
			s.Policy.PendingLimitChanges = []*policyv1.PendingLimitChange{{
				LimitKey: msgkit.LimitMaxOpenOffers, Value: 5, ActivationHeight: 10, ProposalId: "gp-1", PolicyVersion: 2,
			}}
		}, "genesis carries no pending change"},
		{"upgrade plan", func(s *policyv1.GenesisState) {
			s.Policy.UpgradePlan = &policyv1.UpgradePlan{Name: "gp-up", Height: 500, ReleaseDigest: digestOf(bytes32(3)), ProposalId: "gp-2"}
		}, "genesis carries no plan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := policy.ParseGenesis(base[app.ModulePolicy])
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(state)
			raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			modules := map[string]json.RawMessage{}
			for name, value := range base {
				modules[name] = value
			}
			modules[app.ModulePolicy] = raw
			chainApp, err := initChain(t, n.doc, modules, 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("InitChain: %v, want %q", err, tc.want)
			}
			for _, store := range []string{app.ModulePolicy, authority.ModuleName, app.ModuleProviders} {
				if keys := chainApp.FinalizeStoreRowsForTest(store); keys != 0 {
					t.Fatalf("InitChain refused but wrote %d %s rows", keys, store)
				}
			}
		})
	}
}

// TestExportRefusesUpgradePlanState (TestExportRefusesGovernanceState for
// the upgrade rows): once a council software upgrade has executed, the
// x/policy export check refuses with the plan prefix (0x06) and App.Export
// refuses; after the council cancels the plan, the plan-name row (0x08,
// kept for good so a name is never reused with another digest) still
// refuses the export.
func TestExportRefusesUpgradePlanState(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	n.council(t, c, "er-up", "upgrade", upgradePayload("er-up", nextHeight(c)+5000, "release er-up"))
	err := chainApp.PolicyKeeperForTest().ExportCheck(chainApp.CommittedContextForTest())
	if err == nil || !strings.Contains(err.Error(), "an upgrade plan (prefix 0x06)") {
		t.Fatalf("export check with a plan: %v", err)
	}
	if _, err := chainApp.Export(); err == nil || !strings.HasPrefix(err.Error(), "export refused:") {
		t.Fatalf("export with a plan: %v", err)
	}
	n.council(t, c, "er-cancel", "upgrade", cancelPayload("er-up"))
	if plan := queryPolicy(t, chainApp).GetUpgradePlan(); plan != nil {
		t.Fatalf("plan after the cancel: %v", plan)
	}
	err = chainApp.PolicyKeeperForTest().ExportCheck(chainApp.CommittedContextForTest())
	if err == nil || !strings.Contains(err.Error(), "plan names (prefix 0x08)") {
		t.Fatalf("export check after the cancel: %v", err)
	}
	if _, err := chainApp.Export(); err == nil || !strings.HasPrefix(err.Error(), "export refused:") {
		t.Fatalf("export after the cancel: %v", err)
	}
}

// TestGovernanceQueryDevnetParametersAreLabelled (TestGovernanceQueryThroughABCI
// for the row contents): every devnet test parameter Query/Governance
// returns keeps the label PROPOSED_TEST_PARAMETER and approved_policy
// false, and the rows equal the genesis document's, the DEC-008 review
// delay included (here a labelled 100 seconds), so the delay in use is
// published and is not an approved policy.
func TestGovernanceQueryDevnetParametersAreLabelled(t *testing.T) {
	n := newRegNet()
	for i := range n.doc.DevnetTestParameters {
		if n.doc.DevnetTestParameters[i].DecisionID == "DEC-008" {
			n.doc.DevnetTestParameters[i].DelaySeconds = 100
		}
	}
	c := n.start(t, 1)
	raw, res := query(t, c.app(), "/svn.authority.v1.Query/Governance", &authorityv1.QueryGovernanceRequest{}, 0)
	if res.Code != 0 {
		t.Fatalf("Query/Governance: %s/%d %s", res.Codespace, res.Code, res.Log)
	}
	var out authorityv1.QueryGovernanceResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	rows := out.GetDevnetTestParameters()
	if len(rows) != len(n.doc.DevnetTestParameters) {
		t.Fatalf("%d rows, genesis has %d", len(rows), len(n.doc.DevnetTestParameters))
	}
	byID := map[string]genesis.DevnetTestParameter{}
	for _, row := range n.doc.DevnetTestParameters {
		byID[row.DecisionID] = row
	}
	for _, row := range rows {
		want, known := byID[row.GetDecisionId()]
		if !known || row.GetLabel() != genesis.DevnetTestLabel || row.GetApprovedPolicy() ||
			row.GetNumericValue() != want.NumericValue || row.GetRoleName() != want.RoleName || row.GetDelaySeconds() != want.DelaySeconds {
			t.Fatalf("devnet test parameter %v, genesis %+v", row, want)
		}
	}
	if byID["DEC-008"].DelaySeconds != 100 {
		t.Fatal("the fixture lost its DEC-008 delay")
	}
}
