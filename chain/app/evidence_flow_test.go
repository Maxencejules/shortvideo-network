package app_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// withUnknownVarint appends varint field num with value v to m's unknown
// fields (a bool field such as the reserved EvidenceItem field 9, reject).
func withUnknownVarint(m proto.Message, num protowire.Number, v uint64) {
	r := m.ProtoReflect()
	extra := protowire.AppendTag(nil, num, protowire.VarintType)
	extra = protowire.AppendVarint(extra, v)
	r.SetUnknown(append(r.GetUnknown(), extra...))
}

// TestEvidenceMessagesDecode (deviation D-19): the spec's step 1 rejects an
// unknown key (UNKNOWN_FIELD; case evidence-reject-flag-unknown, a submitter
// "reject" flag). The chain decodes protobuf: EvidenceItem reserves field 9
// and the name reject, so an item carrying field 9, any other unknown field
// inside an item, the observation digest, or the auditor signature, or an
// unknown field of the message, fails the app's strict transaction decoding
// in CheckTx and in FinalizeBlock with codespace sdk code 2
// (ErrTxDecode), before the ante handler and the handler: the sequence and
// the full state do not change. The same message without the extra field
// commits, so no case is vacuous. A reserved kind number (4, "unique
// replica") is a known enum field: it decodes, passes the ante, and the
// handler answers MALFORMED (step 10).
func TestEvidenceMessagesDecode(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	chainApp := c.app()
	p := prefix(e.auditor1)
	now := c.now()
	build := func(label string) *evidencev1.MsgSubmitAuditBatch {
		return e.audit(t, e.auditor1, "dec-"+label, p+"dec-"+label, c.epoch(),
			auditItem(p+"dec-"+label+"-1", "la", "c1", manifestOf("c1"), now+600))
	}
	decode := sdkerrors.ErrTxDecode.ABCICode()
	cases := []struct {
		name string
		edit func(m *evidencev1.MsgSubmitAuditBatch)
	}{
		{"reserved item field 9 (reject = true)", func(m *evidencev1.MsgSubmitAuditBatch) { withUnknownVarint(m.Items[0], 9, 1) }},
		{"unknown field 99 inside an item", func(m *evidencev1.MsgSubmitAuditBatch) { withUnknown(m.Items[0], 99, []byte("reject")) }},
		{"non-critical field 1024 inside an item", func(m *evidencev1.MsgSubmitAuditBatch) { withUnknown(m.Items[0], 1024, []byte("reject")) }},
		{"unknown field 99 inside the observation digest", func(m *evidencev1.MsgSubmitAuditBatch) {
			withUnknown(m.Items[0].ObservationDigest, 99, []byte("x"))
		}},
		{"unknown field 99 inside the auditor signature", func(m *evidencev1.MsgSubmitAuditBatch) {
			withUnknown(m.AuditorSignature, 99, []byte("x"))
		}},
		{"unknown field 99 at the message top level", func(m *evidencev1.MsgSubmitAuditBatch) { withUnknown(m, 99, []byte("x")) }},
	}
	for i, tc := range cases {
		msg := build(fmt.Sprintf("u%d", i))
		tc.edit(msg)
		raw := e.signEnvelope(t, e.auditor1, msg, nil, nil)
		checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatal(err)
		}
		if checked.Codespace != sdkerrors.RootCodespace || checked.Code != decode {
			t.Fatalf("%s: CheckTx %s/%d %s", tc.name, checked.Codespace, checked.Code, checked.Log)
		}
		before := chainApp.FullSnapshot()
		seq := e.seq[e.auditor1.hexAddr()]
		res := e.deliverRaw(t, c, e.auditor1, raw)
		if res.Codespace != sdkerrors.RootCodespace || res.Code != decode {
			t.Fatalf("%s: FinalizeBlock %s/%d %s", tc.name, res.Codespace, res.Code, res.Log)
		}
		if e.seq[e.auditor1.hexAddr()] != seq {
			t.Fatalf("%s: the ante handler ran", tc.name)
		}
		unchanged(t, tc.name, chainApp, before, res)
	}
	// The same shape without an extra field commits.
	base := build("base")
	wantCode(t, "base", e.deliverRaw(t, c, e.auditor1, e.signEnvelope(t, e.auditor1, base, nil, nil)), msgkit.OK)
	wantState(t, "base", chainApp, base.Items[0].EvidenceId, 0, stateRecorded)

	// Kind 4 is a reserved number, not an unknown field: the handler runs
	// and answers MALFORMED (step 10) with the state unchanged.
	reserved := build("k4")
	reserved.Items[0].Kind = evidencev1.EvidenceKind(4)
	reserved.AuditorSignature.Signature = nil
	signAudit(t, reserved, edPriv(t, e.auditor1))
	before := chainApp.FullSnapshot()
	res := e.send(t, c, e.auditor1, reserved)
	wantCode(t, "kind 4", res, msgkit.Malformed)
	if !anteCommitted(res) {
		t.Fatal("kind 4: the handler did not run")
	}
	unchanged(t, "kind 4", chainApp, before, res)
}

// TestEvidenceQueryTimeThroughABCI: Query/Evidence through ABCI reports the
// state at the query context's block time, which the SDK takes from the
// committed header (latest height) or the height's commit info
// (historical height). A RECORDED record whose expiry is E reads RECORDED
// at the heights whose header time is E-2 and E-1 and EXPIRED at the
// heights whose header time is E and E+1, at the latest height and
// historically; the stored record stays RECORDED (nothing is swept,
// deviation D-4). A REJECTED record reads REJECTED at every height.
func TestEvidenceQueryTimeThroughABCI(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	chainApp := c.app()
	p := prefix(e.auditor1)
	now := c.now()
	expiry := now + 300
	recorded, rejected := p+"qt-rec", p+"qt-rej"
	wantCode(t, "batch", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "qt-1", p+"qt-1", c.epoch(),
		auditItem(recorded, "la", "c1", manifestOf("c1"), expiry),
		auditItem(rejected, "lb", "c2", wrongDigest("qt"), expiry),
	)), msgkit.OK)
	written := c.height()
	wantState(t, "after the batch", chainApp, recorded, 0, stateRecorded)

	heights := map[int64]int64{}
	for _, at := range []int64{expiry - 2, expiry - 1, expiry, expiry + 1} {
		c.blockAt(t, unixTime(at))
		heights[at] = c.height()
	}
	wantState(t, "latest (header time E+1)", chainApp, recorded, 0, stateExpired)
	wantState(t, "latest (header time E+1)", chainApp, rejected, 0, stateRejected)
	for _, row := range []struct {
		at   int64
		want evidencev1.EvidenceState
	}{
		{expiry - 2, stateRecorded}, {expiry - 1, stateRecorded}, {expiry, stateExpired}, {expiry + 1, stateExpired},
	} {
		wantState(t, fmt.Sprintf("header time E%+d", row.at-expiry), chainApp, recorded, heights[row.at], row.want)
		wantState(t, fmt.Sprintf("header time E%+d", row.at-expiry), chainApp, rejected, heights[row.at], stateRejected)
	}
	wantState(t, "height of the batch", chainApp, recorded, written, stateRecorded)
	wantState(t, "before the batch", chainApp, recorded, written-1, stateNone)
	stored, found := chainApp.StoredEvidenceForTest(recorded)
	if !found || stored.GetState() != stateRecorded {
		t.Fatalf("stored record: %v %v", stored, found)
	}
	// The lease query of the same height reports the same state.
	out, _ := queryEvidence(t, chainApp, &evidencev1.QueryEvidenceRequest{LeaseId: "la"}, heights[expiry])
	if len(out.GetRecords()) != 1 || out.GetRecords()[0].GetState() != stateExpired {
		t.Fatalf("records of la at E: %v", out.GetRecords())
	}
	c.checkEvidenceInvariants(t, "query time")
}

// TestNoEvidenceRenews: no evidence record renews or extends a lease, in
// any state (the label catalog's limitation "No evidence record renews,
// extends or revives a lease, in any state."). A renewal of a held lease,
// signed by the provider, that names a live RECORDED record is FORBIDDEN, a
// live REJECTED record FORBIDDEN, a record past its expiry EXPIRED, and an
// absent id NOT_FOUND (x/leases renew check 13; spec_machine.py:842-848).
// Each rejection leaves the full non-account state byte-identical and
// emits no typed event, and the lease query bytes do not change. The same
// renewal without an evidence id commits, so the rejections are about the
// evidence.
func TestNoEvidenceRenews(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	fx := evidenceSetup(t, e, c)
	chainApp := c.app()
	p := prefix(e.auditor1)
	now := c.now()
	live, liveRejected, short := p+"nr-rec", p+"nr-rej", p+"nr-short"
	wantCode(t, "batch", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "nr-1", p+"nr-1", c.epoch(),
		auditItem(live, "la", "c1", manifestOf("c1"), now+900),
		auditItem(liveRejected, "lb", "c2", wrongDigest("nr"), now+900),
		auditItem(short, "lc", "c3", manifestOf("c3"), now+100),
	)), msgkit.OK)
	c.setUnix(now + 200)
	c.block(t)
	wantState(t, "short", chainApp, short, 0, stateExpired)

	rows := []struct {
		name, lease, provider, content, evidenceID string
		want                                       msgkit.Code
	}{
		{"RECORDED live, own lease", "la", "p1", "c1", live, msgkit.Forbidden},
		{"RECORDED live, another lease", "lb", "p1", "c2", live, msgkit.Forbidden},
		{"REJECTED live", "lb", "p1", "c2", liveRejected, msgkit.Forbidden},
		{"RECORDED past its expiry", "lc", "p2", "c3", short, msgkit.Expired},
		{"absent evidence id", "la", "p1", "c1", p + "nr-absent", msgkit.NotFound},
	}
	for i, row := range rows {
		_, leaseBytes := queryActive(t, chainApp, row.content, row.provider, 10, nil, 0)
		before := chainApp.FullSnapshot()
		res := e.send(t, c, e.broadcaster, e.renewNaming(t, e.broadcaster, fmt.Sprintf("nr-rn-%d", i), row.lease, row.provider, row.content,
			sizeSmall, 2, fx.deadline+leaseTerm, row.evidenceID))
		wantCode(t, row.name, res, row.want)
		unchanged(t, row.name, chainApp, before, res)
		if _, after := queryActive(t, chainApp, row.content, row.provider, 10, nil, 0); !bytes.Equal(leaseBytes, after) {
			t.Fatalf("%s: lease query bytes changed", row.name)
		}
	}
	wantCode(t, "renew la without evidence", e.send(t, c, e.broadcaster,
		e.renewMsg(t, e.broadcaster, "nr-rn-ok", "la", "p1", "c1", sizeSmall, 2, fx.deadline+leaseTerm)), msgkit.OK)
	c.checkInvariants(t, "no evidence renews")
	c.checkEvidenceInvariants(t, "no evidence renews")
}

// labelCatalog is the part of the producer's label catalog
// (agents/auditor/labels/evidence-labels.json) these tests read.
type labelCatalog struct {
	States map[string]struct {
		Text string `json:"text"`
	} `json:"states"`
	Limitations []string `json:"limitations"`
}

func readLabels(t *testing.T) labelCatalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "agents", "auditor", "labels", "evidence-labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out labelCatalog
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestEvidenceCannotExtendOrReviveLease (REQ-INV-005; goal: "a stale or
// passing audit must not revive expired or withdrawn hosting"). A RECORDED
// record E1 of lease la: a renewal naming it is FORBIDDEN at E1's expiry
// minus one second and EXPIRED at its expiry. Then, for an ended lease
// (EndLease), a lease whose content is withdrawn, and a lease that expired
// by header time and was swept, a new audit, passing (the manifest digest)
// or failing, is CONFLICT, WITHDRAWN, and CONFLICT with the full state
// unchanged. The RECORDED records written while those leases were held
// still read RECORDED by lease until their own expiry, which is at most the
// lease deadline at recording (deviation D-18): a past observation. The
// label catalog's RECORDED text says it does not show that the lease is
// held or the content is available now. No lease, provider, or content
// byte changed because of any evidence message.
func TestEvidenceCannotExtendOrReviveLease(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	fx := evidenceSetup(t, e, c)
	chainApp := c.app()
	p := prefix(e.auditor1)
	t0 := c.now()
	e1 := p + "rv-e1"
	// Records of le and lw expire at t0+1700, before their deadline
	// (t0+1800); the record of lx expires at its deadline (t0+900), the
	// latest expiry T7 allows.
	items := []*evidencev1.EvidenceItem{
		auditItem(e1, "la", "c1", manifestOf("c1"), t0+600),
		auditItem(p+"rv-le", "le", "c4", manifestOf("c4"), t0+1700),
		auditItem(p+"rv-lw", "lw", "c5", manifestOf("c5"), t0+1700),
		auditItem(p+"rv-lx", "lx", "c6", manifestOf("c6"), t0+900),
	}
	others := otherStores(chainApp)
	wantCode(t, "batch", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "rv-1", p+"rv-1", c.epoch(), items...)), msgkit.OK)
	sameBytes(t, "a batch writes only the evidence store", others, otherStores(chainApp))

	renew := func(label string, at int64, want msgkit.Code) {
		t.Helper()
		c.setUnix(at)
		before := chainApp.FullSnapshot()
		res := e.send(t, c, e.broadcaster, e.renewNaming(t, e.broadcaster, label, "la", "p1", "c1", sizeSmall, 2, fx.deadline+leaseTerm, e1))
		wantCode(t, label, res, want)
		unchanged(t, label, chainApp, before, res)
	}
	renew("rv-renew-expiry-1", t0+599, msgkit.Forbidden)
	renew("rv-renew-expiry", t0+600, msgkit.Expired)

	wantCode(t, "end le", e.send(t, c, e.broadcaster, e.endMsg(t, e.broadcaster, "rv-end-le", "le", "p1", 1)), msgkit.OK)
	wantCode(t, "withdraw c5", e.send(t, c, e.service, e.withdrawContent(t, e.service, "rv-wd-c5", "c5", 1, reasonDigest("c5"))), msgkit.OK)
	c.setUnix(t0 + 900) // lx deadline: BeginBlock expiry sweeps it
	c.block(t)
	if lease := heldLeaseState(t, chainApp, "lx"); lease.GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED {
		t.Fatalf("lx: %v", lease.GetState())
	}
	now := c.now()
	for i, row := range []struct {
		lease, content string
		want           msgkit.Code
		reported       evidencev1.EvidenceState
	}{
		{"le", "c4", msgkit.Conflict, stateRecorded},
		{"lw", "c5", msgkit.Withdrawn, stateRecorded},
		{"lx", "c6", msgkit.Conflict, stateExpired},
	} {
		for j, observation := range [][]byte{manifestOf(row.content), wrongDigest(row.lease)} {
			label := fmt.Sprintf("stale audit of %s (%d)", row.lease, j)
			id := fmt.Sprintf("rv-late-%d-%d", i, j)
			before := chainApp.FullSnapshot()
			res := e.send(t, c, e.auditor1, e.audit(t, e.auditor1, id, p+id, c.epoch(),
				auditItem(p+id+"-1", row.lease, row.content, observation, now+60)))
			wantCode(t, label, res, row.want)
			unchanged(t, label, chainApp, before, res)
		}
		out, _ := queryEvidence(t, chainApp, &evidencev1.QueryEvidenceRequest{LeaseId: row.lease}, 0)
		if len(out.GetRecords()) != 1 || out.GetRecords()[0].GetState() != row.reported {
			t.Fatalf("%s: records %v", row.lease, out.GetRecords())
		}
	}
	text := readLabels(t).States["RECORDED"].Text
	if !strings.Contains(text, "It does not show that the lease is held or the content is available now.") {
		t.Fatalf("RECORDED label: %q", text)
	}
	c.setUnix(t0 + 1700)
	c.block(t)
	for _, lease := range []string{"le", "lw"} {
		wantState(t, "past expiry", chainApp, p+"rv-"+lease, 0, stateExpired)
	}
	c.checkInvariants(t, "revive")
	c.checkEvidenceInvariants(t, "revive")
}

// heldLeaseState reads one lease in any state from the committed state.
func heldLeaseState(t *testing.T, chainApp *app.App, leaseID string) *leasesv1.Lease {
	t.Helper()
	lease, found := chainApp.LeaseForTest(leaseID)
	if !found {
		t.Fatalf("lease %s not found", leaseID)
	}
	return lease
}

// TestAuditRetryScenarios: the business id is the retry handle (business
// gate, step 15). The same business id and payload in a second transaction
// (a new hash: the next account sequence) is an idempotent success: code
// 0, no record or event besides evidence_result idempotent=true, and the
// full non-account state unchanged. This holds in a later block and in the
// same block. The same business id with another payload is REPLAY. A lease
// business id reused by an audit, and an audit business id reused by a
// lease message, are REPLAY (one namespace). The same payload resent by
// another auditor fails the key binding (UNAUTHORIZED, deviation D-2).
func TestAuditRetryScenarios(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	chainApp := c.app()
	p := prefix(e.auditor1)
	now := c.now()
	batch := func(bid, batchID, evidenceID string) *evidencev1.MsgSubmitAuditBatch {
		return e.audit(t, e.auditor1, bid, p+batchID, c.epoch(), auditItem(p+evidenceID, "la", "c1", manifestOf("c1"), now+600))
	}
	first := batch("rt-1", "rt-1", "rt-1-e")
	res := e.send(t, c, e.auditor1, first)
	wantCode(t, "first", res, msgkit.OK)
	if accepted, states, idem := evidenceEvents(t, res); len(accepted) != 1 || len(states) != 1 || idem {
		t.Fatalf("first: events %v %v %v", accepted, states, idem)
	}
	rawFirst := e.signedAudit(t, c, e.auditor1, first)
	before := chainApp.FullSnapshot()
	res = e.deliverRaw(t, c, e.auditor1, rawFirst)
	wantCode(t, "retry in a later block", res, msgkit.OK)
	if accepted, states, idem := evidenceEvents(t, res); len(accepted) != 0 || len(states) != 0 || !idem {
		t.Fatalf("retry: events %v %v %v", accepted, states, idem)
	}
	sameBytes(t, "retry in a later block", before, chainApp.FullSnapshot())

	// Two transactions with one business id and payload in one block.
	second := batch("rt-2", "rt-2", "rt-2-e")
	rawA := e.signedAudit(t, c, e.auditor1, second)
	e.seq[e.auditor1.hexAddr()]++
	rawB := e.signedAudit(t, c, e.auditor1, second)
	e.seq[e.auditor1.hexAddr()]++
	if bytes.Equal(rawA, rawB) {
		t.Fatal("the two transactions have one hash")
	}
	block := c.block(t, rawA, rawB)
	wantCode(t, "same block, first", block.TxResults[0], msgkit.OK)
	wantCode(t, "same block, second", block.TxResults[1], msgkit.OK)
	if accepted, _, idem := evidenceEvents(t, block.TxResults[0]); len(accepted) != 1 || idem {
		t.Fatalf("same block, first: %v %v", accepted, idem)
	}
	if accepted, _, idem := evidenceEvents(t, block.TxResults[1]); len(accepted) != 0 || !idem {
		t.Fatalf("same block, second: %v %v", accepted, idem)
	}

	reject := func(label string, who *actor, msg sdk.Msg, want msgkit.Code) {
		t.Helper()
		before := chainApp.FullSnapshot()
		res := e.send(t, c, who, msg)
		wantCode(t, label, res, want)
		unchanged(t, label, chainApp, before, res)
	}
	changed := batch("rt-1", "rt-1-other", "rt-1-other-e")
	reject("same business id, another payload", e.auditor1, changed, msgkit.Replay)
	reject("lease business id reused by an audit", e.auditor1, batch("ac-la", "rt-3", "rt-3-e"), msgkit.Replay)
	reject("audit business id reused by a lease message", e.broadcaster, e.endMsg(t, e.broadcaster, "rt-1", "le", "p1", 1), msgkit.Replay)
	// auditor-2 sends auditor-1's business id and signed payload as its own
	// message: the business payload excludes the signer, so the spec would
	// answer idempotent; the key does not derive auditor-2's address.
	other := proto.Clone(first).(*evidencev1.MsgSubmitAuditBatch)
	other.Signer = e.auditor2.addr
	reject("auditor-2 resends auditor-1's batch", e.auditor2, other, msgkit.Unauthorized)
	c.checkEvidenceInvariants(t, "retry")
}

// TestDishonestAdmittedAuditorInApp (plan section 18, chain part): two
// AUDITOR accounts of one operator on one machine. auditor-1 reports
// honestly in this scenario: lb FAIL (a report digest, REJECTED) and la
// PASS (la's manifest digest, RECORDED). auditor-2 fetches nothing and
// signs a false PASS for lb (lb's public manifest digest) and a false FAIL
// for la (a fabricated report digest). Both commit with code 0: RECORDED
// for lb and REJECTED for la, beside auditor-1's opposite records. A valid
// auditor signature shows which admitted key made the statement; it does
// not make the statement true. No lease, provider, content, or authority
// byte changes. The limits still bind auditor-2: an expiry 86401 s ahead is
// BOUNDS (D-9), another content version CONFLICT (D-10), a copy of
// auditor-1's not-yet-committed batch with auditor-1's ids re-signed by
// auditor-2 FORBIDDEN (D-17, after which auditor-1's own batch commits),
// and after 1024 items in one epoch the next item is BOUNDS until the next
// epoch (D-15: per auditor and per epoch, not a lifetime cap). Renewals
// naming the false records are FORBIDDEN.
func TestDishonestAdmittedAuditorInApp(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	fx := evidenceSetup(t, e, c)
	chainApp := c.app()
	p1, p2 := prefix(e.auditor1), prefix(e.auditor2)
	now := c.now()
	before := otherStores(chainApp)
	wantCode(t, "auditor-1 honest", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "dh-1", p1+"dh-1", c.epoch(),
		auditItem(p1+"dh-e1", "lb", "c2", wrongDigest("honest-fail-lb"), now+900),
		auditItem(p1+"dh-e3", "la", "c1", manifestOf("c1"), now+900),
	)), msgkit.OK)
	wantCode(t, "auditor-2 false PASS and false FAIL", e.send(t, c, e.auditor2, e.audit(t, e.auditor2, "dh-2", p2+"dh-2", c.epoch(),
		auditItem(p2+"dh-e2", "lb", "c2", manifestOf("c2"), now+900),
		auditItem(p2+"dh-e4", "la", "c1", wrongDigest("fabricated-fail-la"), now+900),
	)), msgkit.OK)
	sameBytes(t, "false attestations change no other store", before, otherStores(chainApp))
	for id, want := range map[string]evidencev1.EvidenceState{
		p1 + "dh-e1": stateRejected, p1 + "dh-e3": stateRecorded, p2 + "dh-e2": stateRecorded, p2 + "dh-e4": stateRejected,
	} {
		wantState(t, "dishonest", chainApp, id, 0, want)
	}
	readme, err := os.ReadFile(filepath.Join("..", "x", "evidence", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sentence := range []string{
		"A valid auditor signature shows which admitted key made the statement. It does not make the statement true.",
	} {
		if !strings.Contains(strings.Join(strings.Fields(string(readme)), " "), sentence) {
			t.Fatalf("x/evidence README lacks %q", sentence)
		}
	}
	limits := readLabels(t).Limitations
	if !containsString(limits, "A valid signature does not make an observation true.") || !containsString(limits, "Auditors can lie.") {
		t.Fatalf("label limitations: %q", limits)
	}

	reject := func(label string, who *actor, msg sdk.Msg, want msgkit.Code) {
		t.Helper()
		snapshot := chainApp.FullSnapshot()
		res := e.send(t, c, who, msg)
		wantCode(t, label, res, want)
		unchanged(t, label, chainApp, snapshot, res)
	}
	reject("lifetime above 86400 s (D-9)", e.auditor2, e.audit(t, e.auditor2, "dh-3", p2+"dh-3", c.epoch(),
		auditItem(p2+"dh-3-e", "la", "c1", manifestOf("c1"), now+evidence.MaxEvidenceLifetimeSeconds+1)), msgkit.Bounds)
	otherVersion := auditItem(p2+"dh-4-e", "la", "c1", manifestOf("c1"), now+600)
	otherVersion.ContentVersion = 2
	reject("another content version (D-10)", e.auditor2, e.audit(t, e.auditor2, "dh-4", p2+"dh-4", c.epoch(), otherVersion), msgkit.Conflict)
	pending := e.audit(t, e.auditor1, "dh-5", p1+"dh-5", c.epoch(), auditItem(p1+"dh-5-e", "la", "c1", manifestOf("c1"), now+600))
	reject("auditor-1's ids re-signed by auditor-2 (D-17)", e.auditor2,
		e.audit(t, e.auditor2, "dh-5-copy", p1+"dh-5", c.epoch(), auditItem(p1+"dh-5-e", "la", "c1", manifestOf("c1"), now+600)), msgkit.Forbidden)
	wantCode(t, "auditor-1's pending batch", e.send(t, c, e.auditor1, pending), msgkit.OK)

	for i, id := range []string{p2 + "dh-e2", p2 + "dh-e4"} {
		reject(fmt.Sprintf("renewal naming false record %d", i), e.broadcaster,
			e.renewNaming(t, e.broadcaster, fmt.Sprintf("dh-rn-%d", i), "la", "p1", "c1", sizeSmall, 2, fx.deadline+leaseTerm, id), msgkit.Forbidden)
	}

	// The per-auditor per-epoch cap. la is renewed first so it is held in
	// the next epoch.
	wantCode(t, "renew la", e.send(t, c, e.broadcaster, e.renewMsg(t, e.broadcaster, "dh-rn-la", "la", "p1", "c1", sizeSmall, 2, fx.deadline+leaseTerm)), msgkit.OK)
	afterRenew := otherStores(chainApp)
	stored := 2 // auditor-2's items in this epoch so far
	for b := 0; stored < int(evidence.MaxEvidencePerAuditorPerEpoch); b++ {
		n := min(int(genesis.MaxEvidenceBatch), int(evidence.MaxEvidencePerAuditorPerEpoch)-stored)
		items := make([]*evidencev1.EvidenceItem, n)
		for i := range items {
			items[i] = auditItem(fmt.Sprintf("%sdh-cap-%02d-%02d", p2, b, i), "la", "c1", manifestOf("c1"), now+600)
		}
		wantCode(t, fmt.Sprintf("cap batch %d", b), e.send(t, c, e.auditor2, e.audit(t, e.auditor2, fmt.Sprintf("dh-cap-%02d", b), fmt.Sprintf("%sdh-cap-%02d", p2, b), c.epoch(), items...)), msgkit.OK)
		stored += n
	}
	reject("item 1025 in the epoch (D-15)", e.auditor2, e.audit(t, e.auditor2, "dh-cap-over", p2+"dh-cap-over", c.epoch(),
		auditItem(p2+"dh-cap-over-e", "la", "c1", manifestOf("c1"), now+600)), msgkit.Bounds)
	wantCode(t, "auditor-1 is not capped by auditor-2", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "dh-6", p1+"dh-6", c.epoch(),
		auditItem(p1+"dh-6-e", "la", "c1", manifestOf("c1"), now+600))), msgkit.OK)
	sameBytes(t, "1024 attestations change no other store", afterRenew, otherStores(chainApp))
	next := (int64(c.epoch()) + 1) * evidence.EpochSeconds
	c.setUnix(next + 10)
	wantCode(t, "auditor-2 in the next epoch", e.send(t, c, e.auditor2, e.audit(t, e.auditor2, "dh-cap-next", p2+"dh-cap-next", c.epoch(),
		auditItem(p2+"dh-cap-next-e", "la", "c1", manifestOf("c1"), next+600))), msgkit.OK)
	c.checkInvariants(t, "dishonest")
	c.checkEvidenceInvariants(t, "dishonest")
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// TestSeatEvidenceRefusedForTheSeatRule (DR-06-06, positive control): a
// council seat signs a SubmitAuditBatch that is valid in every field. Its
// inner signature is by the seat's own ed25519 key, so it even passes the
// D-2 key binding (the seat address derives from that key), and its ids
// carry the seat's prefix. The ante refuses it with ERROR_CODE_FORBIDDEN in
// CheckTx and in FinalizeBlock before any handler; the seat's sequence and
// the full state do not change. The same batch shape signed and sent by an
// AUDITOR account commits: the seat row is refused for the seat rule, not
// for its content.
func TestSeatEvidenceRefusedForTheSeatRule(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	chainApp := c.app()
	now := c.now()
	shape := func(who *actor) *evidencev1.MsgSubmitAuditBatch {
		p := prefix(who)
		return e.audit(t, who, "seat-au-"+who.name, p+"seat-au", c.epoch(), auditItem(p+"seat-au-e", "la", "c1", manifestOf("c1"), now+600))
	}
	seat := e.seats[0]
	msg := shape(seat)
	if code := evidence.VerifyAuditSignature(msg); code != msgkit.OK {
		t.Fatalf("the seat batch is not valid in content: %s", code)
	}
	raw := e.signedAudit(t, c, seat, msg)
	checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
	if err != nil {
		t.Fatal(err)
	}
	if checked.Codespace != "svn" || checked.Code != uint32(2) || !strings.Contains(checked.Log, seatOnlyLog) {
		t.Fatalf("CheckTx: %s/%d %s", checked.Codespace, checked.Code, checked.Log)
	}
	before := chainApp.FullSnapshot()
	seq, _ := chainApp.AccountSequenceForTest(seat.addr)
	res := e.deliverRaw(t, c, seat, raw)
	wantCode(t, "seat batch", res, msgkit.Forbidden)
	if !strings.Contains(res.Log, seatOnlyLog) || anteCommitted(res) {
		t.Fatalf("seat batch: refused outside the ante: %s", res.Log)
	}
	if after, _ := chainApp.AccountSequenceForTest(seat.addr); after != seq {
		t.Fatalf("seat sequence %d -> %d", seq, after)
	}
	unchanged(t, "seat batch", chainApp, before, res)
	auditorMsg := shape(e.auditor1)
	wantCode(t, "the same shape from auditor-1", e.send(t, c, e.auditor1, auditorMsg), msgkit.OK)
	wantState(t, "auditor-1", chainApp, auditorMsg.Items[0].EvidenceId, 0, stateRecorded)
}

// TestSecpAuditorAccountCannotBind (owner ruling on Q1: the attestation key
// must derive the submitting account's address): an AUDITOR account with a
// secp256k1 account key cannot make a binding attestation. Its batch with a
// fresh ed25519 attestation key, or with auditor-1's key, is UNAUTHORIZED at
// step 7 with the full state unchanged; a BROADCASTER relaying a valid
// auditor-1 batch is FORBIDDEN at step 3 (no relay).
func TestSecpAuditorAccountCannotBind(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	chainApp := c.app()
	now := c.now()
	ps := prefix(e.auditorSecp)
	fresh := newEdAccount("fresh-attestation-key")
	for _, key := range []*actor{fresh, e.auditor1} {
		priv := edPriv(t, key)
		msg := unsignedBatch(e.auditorSecp, priv.Public().(ed25519.PublicKey), "key-"+key.name, "secp-"+key.name, ps+"secp-"+key.name, c.epoch(),
			auditItem(ps+"secp-"+key.name+"-e", "la", "c1", manifestOf("c1"), now+600))
		signAudit(t, msg, priv)
		before := chainApp.FullSnapshot()
		res := e.send(t, c, e.auditorSecp, msg)
		wantCode(t, "secp auditor with key "+key.name, res, msgkit.Unauthorized)
		if !anteCommitted(res) {
			t.Fatalf("key %s: the handler did not run", key.name)
		}
		unchanged(t, "secp auditor with key "+key.name, chainApp, before, res)
	}
	p := prefix(e.auditor1)
	valid := e.audit(t, e.auditor1, "relay-1", p+"relay-1", c.epoch(), auditItem(p+"relay-1-e", "la", "c1", manifestOf("c1"), now+600))
	relay := proto.Clone(valid).(*evidencev1.MsgSubmitAuditBatch)
	relay.Signer = e.broadcaster.addr
	before := chainApp.FullSnapshot()
	res := e.send(t, c, e.broadcaster, relay)
	wantCode(t, "broadcaster relay", res, msgkit.Forbidden)
	unchanged(t, "broadcaster relay", chainApp, before, res)
	wantCode(t, "auditor-1 sends its own batch", e.send(t, c, e.auditor1, valid), msgkit.OK)
}

// TestGenesisRefusesAuditorPrefixCollision (DR-09-30, owner ruling on Q2):
// every evidence id and batch id must start with IDPrefix of the auditor's
// address (its first 8 bytes in hex), so app genesis refuses two AUDITOR
// submitters whose addresses share those bytes. The two addresses are
// derived from evidencetest.PrefixCollisionKeys, two synthetic 32-byte
// values (no private key exists for them) whose SHA-256 digests share their
// first 8 bytes; genesis rows carry addresses, so no key is needed.
// AppStateFromDocument (devnet init), InitChain, and
// ValidateAuthorityGenesis (svnd genesis validate) refuse the document. Two
// AUDITOR rows with distinct prefixes, and one colliding address under
// another role, are accepted.
func TestGenesisRefusesAuditorPrefixCollision(t *testing.T) {
	a, b := evidencetestPrefixCollision(t)
	addrA, addrB := evidence.AuditorAddress(a), evidence.AuditorAddress(b)
	if bytes.Equal(addrA, addrB) || evidence.IDPrefix(addrA) != evidence.IDPrefix(addrB) {
		t.Fatalf("not a prefix collision: %x %x", addrA, addrB)
	}
	want := "share the evidence id prefix"
	withRows := func(rows ...genesis.Submitter) *regNet {
		n := newRegNet()
		n.doc.Submitters = append(n.doc.Submitters, rows...)
		return n
	}
	collide := withRows(
		genesis.Submitter{AddressHex: hex.EncodeToString(addrA), Role: genesis.RoleAuditor},
		genesis.Submitter{AddressHex: hex.EncodeToString(addrB), Role: genesis.RoleAuditor},
	)
	if _, err := app.AppStateFromDocument(codecFromApp(t, collide.doc.ChainID), collide.doc); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("AppStateFromDocument: %v", err)
	}
	// InitChain: a document with auditor A, then auditor B added to the
	// authority module genesis.
	single := withRows(genesis.Submitter{AddressHex: hex.EncodeToString(addrA), Role: genesis.RoleAuditor})
	modules, err := app.AppStateFromDocument(codecFromApp(t, single.doc.ChainID), single.doc)
	if err != nil {
		t.Fatal(err)
	}
	modules[authority.ModuleName] = addAuditorRow(t, modules[authority.ModuleName], addrB)
	if err := app.ValidateAuthorityGenesis(modules, 1, single.doc.GenesisUnixSeconds); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateAuthorityGenesis: %v", err)
	}
	chainApp := newTestApp(t, single.doc.ChainID, nil)
	params := consensusParams()
	if _, err := chainApp.InitChain(&abci.RequestInitChain{
		ChainId: single.doc.ChainID, Time: single.doc.Time(), AppStateBytes: mustJSON(t, modules),
		InitialHeight: 1, Validators: validatorUpdates(t, single.doc), ConsensusParams: &params,
	}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("InitChain: %v", err)
	}

	other := newEdAccount("auditor-other")
	for name, n := range map[string]*regNet{
		"distinct prefixes": withRows(
			genesis.Submitter{AddressHex: hex.EncodeToString(addrA), Role: genesis.RoleAuditor},
			genesis.Submitter{AddressHex: other.hexAddr(), Role: genesis.RoleAuditor},
		),
		"colliding address under another role": withRows(
			genesis.Submitter{AddressHex: hex.EncodeToString(addrA), Role: genesis.RoleAuditor},
			genesis.Submitter{AddressHex: hex.EncodeToString(addrB), Role: genesis.RoleBroadcaster},
		),
	} {
		mods, err := app.AppStateFromDocument(codecFromApp(t, n.doc.ChainID), n.doc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := app.ValidateAuthorityGenesis(mods, 1, n.doc.GenesisUnixSeconds); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		boot(t, n.doc)
	}
}

// TestExportRefusesEvidenceState: App.Export refuses, with an error, any
// evidence state besides the layout key. Its context has no block time, so
// the module export of a record would fail (ErrNoBlockTime) inside the SDK
// module manager's goroutine, where a panic ends the process; the app
// check runs before the module export. Shown twice: after a committed
// batch (where the catalogue launch rule would refuse too), and on a chain
// with no provider, content, or lease, with one record row written outside
// ABCI (where only this check refuses). A fresh chain still exports.
func TestExportRefusesEvidenceState(t *testing.T) {
	want := "export refused: the evidence store holds evidence state"
	e := newEvNet()
	c := e.start(t, 1)
	if _, err := c.app().Export(); err != nil {
		t.Fatalf("fresh chain: %v", err)
	}
	evidenceSetup(t, e, c)
	p := prefix(e.auditor1)
	wantCode(t, "batch", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "ex-1", p+"ex-1", c.epoch(),
		auditItem(p+"ex-1-e", "la", "c1", manifestOf("c1"), c.now()+600))), msgkit.OK)
	if _, err := c.app().Export(); err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("after a batch: %v", err)
	}

	bare := newEvNet()
	node := boot(t, bare.doc)
	node.deliver(t, nil)
	stored, found := c.app().StoredEvidenceForTest(p + "ex-1-e")
	if !found {
		t.Fatal("no stored record")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	node.app.WriteEvidenceRowForTest(evidence.RecordKey(stored.GetEvidenceId()), raw)
	if _, err := node.app.Export(); err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("record row on a bare chain: %v", err)
	}
}

// TestEvidenceGenesisRoundTripFromCommittedState: the x/evidence module
// genesis round trip from committed app state, with the real lease and
// content keepers (App.Export refuses evidence state,
// TestExportRefusesEvidenceState, and a launch genesis carries none,
// TestLaunchGenesisRejectsEvidence). Two auditors record over three leases:
// RECORDED and REJECTED records, one RECORDED record whose expiry is the
// committed header time. The module export at that time carries every
// record in evidence id order with its reported state: that record EXPIRED
// (stored RECORDED), the others as stored, current_epoch 0; the export one
// second earlier carries it RECORDED. Each export, encoded and decoded as
// genesis JSON, imports into a discarded branch of the committed state
// whose evidence store was emptied (InitGenesis runs the I5, I6, and I13
// checks against the committed leases); the re-export equals the export,
// the invariants hold, and the imported rows equal the committed rows
// except that the business-id rows (prefix 0x03) are absent (business ids
// are not in the v0 genesis schema; DR-07-03, DR-09-18) and, in the later
// export, the expired record is stored EXPIRED. The committed state does
// not change.
func TestEvidenceGenesisRoundTripFromCommittedState(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	chainApp := c.app()
	p1, p2 := prefix(e.auditor1), prefix(e.auditor2)
	now := c.now()
	short, rejected, recorded, other := p1+"gr-short", p1+"gr-rej", p1+"gr-rec", p2+"gr-a2"
	wantCode(t, "auditor-1", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "gr-1", p1+"gr-1", c.epoch(),
		auditItem(short, "la", "c1", manifestOf("c1"), now+300),
		auditItem(rejected, "lb", "c2", wrongDigest("gr"), now+900),
		auditItem(recorded, "lc", "c3", manifestOf("c3"), now+900),
	)), msgkit.OK)
	wantCode(t, "auditor-2", e.send(t, c, e.auditor2, e.audit(t, e.auditor2, "gr-2", p2+"gr-2", c.epoch(),
		auditItem(other, "la", "c1", manifestOf("c1"), now+900))), msgkit.OK)
	c.setUnix(now + 300)
	c.block(t)
	before := chainApp.FullSnapshot()
	for _, row := range []struct {
		at      int64
		expired bool
	}{{now + 299, false}, {now + 300, true}} {
		label := fmt.Sprintf("export at %d", row.at)
		rt, err := chainApp.EvidenceGenesisRoundTripForTest(row.at)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		shortState := stateRecorded
		if row.expired {
			shortState = stateExpired
		}
		want := map[string]evidencev1.EvidenceState{short: shortState, rejected: stateRejected, recorded: stateRecorded, other: stateRecorded}
		records := rt.Exported.GetRecords()
		if len(records) != len(want) || rt.Exported.GetCurrentEpoch() != 0 {
			t.Fatalf("%s: %d records, current_epoch %d", label, len(records), rt.Exported.GetCurrentEpoch())
		}
		for i, rec := range records {
			if i > 0 && records[i-1].GetEvidenceId() >= rec.GetEvidenceId() {
				t.Fatalf("%s: records not in evidence id order", label)
			}
			if rec.GetState() != want[rec.GetEvidenceId()] {
				t.Fatalf("%s: %s exported %s, want %s", label, rec.GetEvidenceId(), rec.GetState(), want[rec.GetEvidenceId()])
			}
		}
		if !proto.Equal(rt.Exported, rt.Reexported) {
			t.Fatalf("%s: the re-export differs from the export", label)
		}
		if rt.Invariants != nil {
			t.Fatalf("%s: invariants after import: %v", label, rt.Invariants)
		}
		business := 0
		for key, value := range rt.Committed {
			got, imported := rt.Imported[key]
			switch {
			case key[0] == evidence.BusinessPrefix:
				business++
				if imported {
					t.Fatalf("%s: business row %x imported", label, key)
				}
			case !imported:
				t.Fatalf("%s: row %x missing after import", label, key)
			case row.expired && key == string(evidence.RecordKey(short)):
				var stored evidencev1.EvidenceRecord
				if err := proto.Unmarshal(value, &stored); err != nil || stored.GetState() != stateRecorded {
					t.Fatalf("%s: committed %s: %v %v", label, short, stored.GetState(), err)
				}
				stored.State = stateExpired
				wantBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(&stored)
				if err != nil {
					t.Fatal(err)
				}
				sameBytes(t, label+": the expired record imported EXPIRED", wantBytes, got)
			default:
				sameBytes(t, fmt.Sprintf("%s: row %x", label, key), value, got)
			}
		}
		if business != 2 || len(rt.Imported) != len(rt.Committed)-business {
			t.Fatalf("%s: %d business rows, %d committed rows, %d imported rows", label, business, len(rt.Committed), len(rt.Imported))
		}
	}
	sameBytes(t, "the round trip writes nothing", before, chainApp.FullSnapshot())
	c.checkEvidenceInvariants(t, "round trip")
}

// TestAuditWithoutDevnetParams: on a chain without the labelled DEC-001 and
// DEC-005 devnet parameters no content or lease can exist, so a
// well-formed, correctly signed batch ends NOT_FOUND (step 18, lease absent)
// with the full state unchanged.
func TestAuditWithoutDevnetParams(t *testing.T) {
	e := newEvNet("DEC-001", "DEC-005")
	c := e.start(t, 1)
	chainApp := c.app()
	p := prefix(e.auditor1)
	before := chainApp.FullSnapshot()
	res := e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "np-1", p+"np-1", c.epoch(),
		auditItem(p+"np-1-e", "la", "c1", manifestOf("c1"), c.now()+600)))
	wantCode(t, "no lease", res, msgkit.NotFound)
	unchanged(t, "no lease", chainApp, before, res)
}

// evidencetestPrefixCollision returns evidencetest.PrefixCollisionKeys.
func evidencetestPrefixCollision(t *testing.T) (a, b []byte) {
	t.Helper()
	a, b = evidencetest.PrefixCollisionKeys()
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("collision keys of %d and %d bytes", len(a), len(b))
	}
	return a, b
}

// addAuditorRow adds an AUDITOR submitter row for addr to an authority
// module genesis document.
func addAuditorRow(t *testing.T, raw json.RawMessage, addr []byte) json.RawMessage {
	t.Helper()
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state.Authority.Submitters = append(state.Authority.Submitters, &typesv1.Submitter{Address: addr, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR})
	out, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&state)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLaunchGenesisRejectsEvidence: the launch rule (emptyCatalog) refuses
// a genesis whose evidence module holds a record (here one exported from a
// committed batch, with its reported state) or a current_epoch other than
// 0, both through InitChain and through DocumentFromModules (svnd genesis
// validate). The empty evidence genesis is accepted.
func TestLaunchGenesisRejectsEvidence(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	p := prefix(e.auditor1)
	wantCode(t, "batch", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "lg-1", p+"lg-1", c.epoch(),
		auditItem(p+"lg-1-e", "la", "c1", manifestOf("c1"), c.now()+600))), msgkit.OK)
	stored, found := c.app().StoredEvidenceForTest(p + "lg-1-e")
	if !found {
		t.Fatal("no stored record")
	}
	original, err := app.AppStateFromDocument(codecFromApp(t, e.doc.ChainID), e.doc)
	if err != nil {
		t.Fatal(err)
	}
	withEvidence := func(state *evidencev1.GenesisState) map[string]json.RawMessage {
		raw, err := evidence.MarshalGenesis(state)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]json.RawMessage{}
		for k, v := range original {
			out[k] = v
		}
		out[app.ModuleEvidence] = raw
		return out
	}
	initChain := func(modules map[string]json.RawMessage) error {
		fresh := newTestApp(t, e.doc.ChainID, nil)
		params := consensusParams()
		_, err := fresh.InitChain(&abci.RequestInitChain{
			ChainId: e.doc.ChainID, Time: e.doc.Time(), AppStateBytes: mustJSON(t, modules),
			InitialHeight: 1, Validators: validatorUpdates(t, e.doc), ConsensusParams: &params,
		})
		return err
	}
	for name, state := range map[string]*evidencev1.GenesisState{
		"a record":        {Records: []*evidencev1.EvidenceRecord{stored}},
		"current_epoch 1": {CurrentEpoch: 1},
	} {
		modules := withEvidence(state)
		if err := initChain(modules); err == nil || !strings.Contains(err.Error(), "evidence is not empty") {
			t.Fatalf("%s: InitChain: %v", name, err)
		}
		if _, err := app.DocumentFromModules(modules); err == nil || !strings.Contains(err.Error(), "evidence is not empty") {
			t.Fatalf("%s: DocumentFromModules: %v", name, err)
		}
	}
	if err := initChain(withEvidence(&evidencev1.GenesisState{})); err != nil {
		t.Fatalf("empty evidence genesis: %v", err)
	}
}

// TestAcceptanceSignatureCoversDigestBytesTerms (REQ-EVID-002, app level,
// real keys): the provider's acceptance signs the package's manifest
// digest, its byte count, and the terms digest. An acceptance signed by
// the provider's registered key over an offer with another manifest
// digest, another byte count, or another terms digest, relayed for the
// real offer, is UNAUTHORIZED with the full state unchanged; the
// acceptance over the real offer then commits. Acceptance is a promise,
// not possession.
func TestAcceptanceSignatureCoversDigestBytesTerms(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	e.registerAdmit(t, c, e.provider1, "p1", capacity1)
	wantCode(t, "register c1", e.send(t, c, e.service, e.registerContent(t, e.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeSmall)), msgkit.OK)
	now := c.now()
	offer := e.offerMsg(e.service, "of-a1", "o-a1", "c1", "p1", sizeSmall, now+1800, now+600)
	wantCode(t, "offer", e.send(t, c, e.service, offer), msgkit.OK)
	chainApp := c.app()
	for i, edit := range []struct {
		name string
		edit func(o *leasesv1.MsgOfferLease)
	}{
		{"another manifest digest", func(o *leasesv1.MsgOfferLease) { o.ManifestDigest = contentManifest("c2") }},
		{"another byte count", func(o *leasesv1.MsgOfferLease) { o.PackageBytes = sizeSmall + 1 }},
		{"another terms digest", func(o *leasesv1.MsgOfferLease) { o.TermsDigest = termsDigest("o-other") }},
	} {
		signed := proto.Clone(offer).(*leasesv1.MsgOfferLease)
		edit.edit(signed)
		msg := e.acceptMsg(t, e.broadcaster, fmt.Sprintf("ac-x%d", i), offer, "l-a1")
		msg.ProviderAcceptance = acceptanceBy(t, signed, "l-a1", e.signing["p1"])
		before := chainApp.FullSnapshot()
		res := e.send(t, c, e.broadcaster, msg)
		wantCode(t, edit.name, res, msgkit.Unauthorized)
		if !anteCommitted(res) {
			t.Fatalf("%s: refused by the ante", edit.name)
		}
		unchanged(t, edit.name, chainApp, before, res)
	}
	wantCode(t, "the real acceptance", e.send(t, c, e.broadcaster, e.acceptMsg(t, e.broadcaster, "ac-a1", offer, "l-a1")), msgkit.OK)
}
