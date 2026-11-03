package app_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// spread is n items of auditor who over la, lb, and lc in order (la gets
// the first third, rounded up), every odd item REJECTED (a report digest)
// and every even item RECORDED (the lease's manifest digest), evidence ids
// prefix(who)+name+"-NN" padded to width characters with "0" before the
// counter when width > 0.
func spread(who *actor, name string, n, width int, expiry int64) []*evidencev1.EvidenceItem {
	items := make([]*evidencev1.EvidenceItem, n)
	for i := range items {
		lease, contentID := []string{"la", "lb", "lc"}[i*3/n], []string{"c1", "c2", "c3"}[i*3/n]
		observation := manifestOf(contentID)
		if i%2 == 1 {
			observation = wrongDigest(fmt.Sprintf("%s-%d", name, i))
		}
		id := fmt.Sprintf("%s%s-%02d", prefix(who), name, i)
		if width > len(id) {
			id = fmt.Sprintf("%s%s-%s%02d", prefix(who), name, strings.Repeat("0", width-len(id)), i)
		}
		items[i] = auditItem(id, lease, contentID, observation, expiry)
	}
	return items
}

// TestAuditFlowReplayAndQueries runs audits on three in-process nodes
// (cluster.block compares the app hash and every result code at each
// height): RECORDED and REJECTED items from auditor-1, a 32-item batch and
// a second auditor in the same epoch, renewals that name evidence, and an
// epoch boundary at effective-1 (the last second of epoch E: a batch for
// E commits, one for E+1 is STALE_VERSION) and effective (the first second
// of E+1: the reverse). The recorded blocks are then replayed into a fresh
// application from the same genesis: the app hash at every height, every
// result code and gas, and the final full state are equal, and
// Query/Evidence bytes (by id, by lease, page by page) are equal across
// the nodes and the replay.
func TestAuditFlowReplayAndQueries(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 3)
	fx := evidenceSetup(t, e, c)
	chainApp := c.app()
	p1, p2 := prefix(e.auditor1), prefix(e.auditor2)
	now := c.now()
	epoch := c.epoch()
	wantCode(t, "auditor-1", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "fl-1", p1+"fl-1", epoch,
		auditItem(p1+"fl-1-a", "la", "c1", manifestOf("c1"), now+900),
		auditItem(p1+"fl-1-b", "lb", "c2", wrongDigest("fl-1-b"), now+900),
	)), msgkit.OK)
	wantCode(t, "auditor-2, 32 items", e.send(t, c, e.auditor2, e.audit(t, e.auditor2, "fl-2", p2+"fl-2", epoch,
		spread(e.auditor2, "fl-2", 32, 0, now+900)...)), msgkit.OK)
	for i, row := range []struct {
		id   string
		want msgkit.Code
	}{{p1 + "fl-1-a", msgkit.Forbidden}, {p1 + "fl-1-b", msgkit.Forbidden}, {p2 + "fl-2-01", msgkit.Forbidden}} {
		wantCode(t, "renewal naming "+row.id, e.send(t, c, e.broadcaster,
			e.renewNaming(t, e.broadcaster, fmt.Sprintf("fl-rn-%d", i), "la", "p1", "c1", sizeSmall, 2, fx.deadline+leaseTerm, row.id)), row.want)
	}
	// la is renewed so it is held across the epoch boundary.
	wantCode(t, "renew la", e.send(t, c, e.broadcaster, e.renewMsg(t, e.broadcaster, "fl-rn-la", "la", "p1", "c1", sizeSmall, 2, fx.deadline+leaseTerm)), msgkit.OK)
	queryHeight := c.height()

	boundary := (int64(epoch) + 1) * evidence.EpochSeconds
	batchAt := func(id string, round uint64) *evidencev1.MsgSubmitAuditBatch {
		return e.audit(t, e.auditor1, id, p1+id, round, auditItem(p1+id+"-e", "la", "c1", manifestOf("c1"), boundary+600))
	}
	c.setUnix(boundary - 1)
	wantCode(t, "epoch E at effective-1", e.send(t, c, e.auditor1, batchAt("fl-e-early", epoch)), msgkit.OK)
	before := chainApp.FullSnapshot()
	res := e.send(t, c, e.auditor1, batchAt("fl-e1-early", epoch+1))
	wantCode(t, "epoch E+1 at effective-1", res, msgkit.StaleVersion)
	unchanged(t, "epoch E+1 at effective-1", chainApp, before, res)
	c.setUnix(boundary)
	before = chainApp.FullSnapshot()
	res = e.send(t, c, e.auditor1, batchAt("fl-e-late", epoch))
	wantCode(t, "epoch E at effective", res, msgkit.StaleVersion)
	unchanged(t, "epoch E at effective", chainApp, before, res)
	wantCode(t, "epoch E+1 at effective", e.send(t, c, e.auditor1, batchAt("fl-e1-late", epoch+1)), msgkit.OK)
	c.checkEvidenceInvariants(t, "flow")
	c.checkInvariants(t, "flow")

	fresh := c.replayFresh(t, e.doc, 0)
	if !bytes.Equal(fresh.app.FullSnapshot(), chainApp.FullSnapshot()) || !bytes.Equal(fresh.app.LastCommitID().Hash, chainApp.LastCommitID().Hash) {
		t.Fatal("replayed state or app hash differs from the cluster")
	}
	t.Logf("replayed %d blocks; app hash %X equal at every height", len(c.inputs), fresh.app.LastCommitID().Hash)

	requests := []*evidencev1.QueryEvidenceRequest{
		{EvidenceId: p1 + "fl-1-a"}, {EvidenceId: p1 + "fl-1-b"}, {EvidenceId: p2 + "fl-2-31"},
		{LeaseId: "la", Page: &typesv1.PageRequest{Limit: 100}}, {LeaseId: "lc"},
		{LeaseId: "la", EvidenceId: p1 + "fl-1-a"},
	}
	for _, height := range []int64{queryHeight, 0} {
		for _, req := range requests {
			_, want := queryEvidence(t, chainApp, req, height)
			for i, node := range c.nodes[1:] {
				if _, got := queryEvidence(t, node.app, req, height); !bytes.Equal(want, got) {
					t.Fatalf("Evidence %v at %d differs on node %d", req, height, i+1)
				}
			}
			if _, got := queryEvidence(t, fresh.app, req, height); !bytes.Equal(want, got) {
				t.Fatalf("Evidence %v at %d differs in the replay", req, height)
			}
		}
		// lb's records one page at a time.
		var ids []string
		var key []byte
		for page := 0; ; page++ {
			out, raw := queryEvidence(t, chainApp, &evidencev1.QueryEvidenceRequest{LeaseId: "lb", Page: &typesv1.PageRequest{Limit: 3, PageKey: key}}, height)
			if _, got := queryEvidence(t, fresh.app, &evidencev1.QueryEvidenceRequest{LeaseId: "lb", Page: &typesv1.PageRequest{Limit: 3, PageKey: key}}, height); !bytes.Equal(raw, got) {
				t.Fatalf("lb page %d at %d differs in the replay", page, height)
			}
			for _, rec := range out.GetRecords() {
				ids = append(ids, rec.GetEvidenceId())
			}
			key = out.GetPage().GetNextPageKey()
			if len(key) == 0 {
				break
			}
		}
		// lb holds auditor-1's fl-1-b and auditor-2's items 11-21.
		if len(ids) != 12 {
			t.Fatalf("lb at %d: %d records %v", height, len(ids), ids)
		}
	}
}

// fixtureRow is one measured fixture.
type fixtureRow struct {
	name                                 string
	items, txBytes, payloadBytes         int
	gasUsed, simulatedGas                int64
	resultBytes, eventBytes              int
	storeKeys, storeBytes, maxRecordSize int
	code                                 msgkit.Code
}

// TestAuditBatchMeasurements measures declared fixtures through
// FinalizeBlock on one machine (plan section 19; per-fixture measurement
// on one machine; no throughput or network-capacity claim):
//
//   - F1: 1 item of la, RECORDED, short ids
//   - F8: 8 items (la x3, lb x3, lc x2), RECORDED and REJECTED alternating
//   - F32: 32 items (la x11, lb x11, lc x10), alternating, short ids
//   - F32max: as F32 with 64-character evidence ids, batch id, and
//     business id
//   - F32rej: 32 items, all REJECTED
//   - Rcount: the largest item count of short ids whose canonical payload
//     still fits in 65536 bytes, refused at step 11 (BOUNDS) after the
//     signature step walked every item
//   - Rpayload: the smallest item count of maximal ids whose canonical
//     payload is above 65536 bytes, refused at step 8 (BOUNDS)
//   - Q100: one Query/Evidence page of 100 records of one lease
//
// For each fixture: transaction bytes, committed gas_used, simulated gas
// (asserted equal to committed), ExecTxResult bytes and event bytes, the
// growth of the evidence store in keys and bytes (logical KV bytes of the
// evidence store, not IAVL or disk bytes), the largest encoded record, and
// the canonical payload bytes. Gas is a flat 50,000 per message plus the
// SDK's ante and store gas; it is not CPU time.
func TestAuditBatchMeasurements(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	chainApp := c.app()
	now := c.now()
	epoch := c.epoch()
	p := prefix(e.auditor2)
	measure := func(name string, msg *evidencev1.MsgSubmitAuditBatch, want msgkit.Code) fixtureRow {
		t.Helper()
		raw := e.signed(t, c, e.auditor2, registryGas, nil, msg)
		info, _, simErr := chainApp.Simulate(raw)
		keysBefore, bytesBefore := chainApp.StoreRowsForTest(app.ModuleEvidence)
		res := e.deliverRaw(t, c, e.auditor2, raw)
		wantCode(t, name, res, want)
		keysAfter, bytesAfter := chainApp.StoreRowsForTest(app.ModuleEvidence)
		row := fixtureRow{name: name, items: len(msg.GetItems()), txBytes: len(raw), gasUsed: res.GasUsed, code: want,
			storeKeys: keysAfter - keysBefore, storeBytes: bytesAfter - bytesBefore}
		if want == msgkit.OK {
			if simErr != nil || int64(info.GasUsed) != res.GasUsed {
				t.Fatalf("%s: simulated gas %v (%v), committed %d", name, info, simErr, res.GasUsed)
			}
			row.simulatedGas = int64(info.GasUsed)
		} else if simErr == nil || row.storeKeys != 0 {
			t.Fatalf("%s: simulation accepted a rejected fixture, or the store grew", name)
		}
		if text, err := evidence.AuditCanonical(msg); err == nil {
			row.payloadBytes = len(text)
		} else if !errors.Is(err, evidence.ErrPayloadTooLarge) {
			t.Fatal(err)
		}
		resultRaw, err := res.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		row.resultBytes = len(resultRaw)
		for _, ev := range res.Events {
			row.eventBytes += len(ev.Type)
			for _, attr := range ev.Attributes {
				row.eventBytes += len(attr.Key) + len(attr.Value)
			}
		}
		for _, it := range msg.GetItems() {
			if stored, found := chainApp.StoredEvidenceForTest(it.GetEvidenceId()); found && want == msgkit.OK {
				enc, err := proto.MarshalOptions{Deterministic: true}.Marshal(stored)
				if err != nil {
					t.Fatal(err)
				}
				row.maxRecordSize = max(row.maxRecordSize, len(enc))
			}
		}
		codeName := string(row.code)
		if row.code == msgkit.OK {
			codeName = "OK"
		}
		t.Logf("fixture=%s items=%d code=%s tx_bytes=%d gas_used=%d simulated_gas=%d result_bytes=%d event_bytes=%d evidence_store_keys=+%d evidence_store_bytes=+%d max_record_bytes=%d canonical_payload_bytes=%d label=%q",
			row.name, row.items, codeName, row.txBytes, row.gasUsed, row.simulatedGas, row.resultBytes, row.eventBytes, row.storeKeys, row.storeBytes,
			row.maxRecordSize, row.payloadBytes, "per-fixture measurement on one machine; no throughput or network-capacity claim")
		return row
	}
	short := func(name string, items []*evidencev1.EvidenceItem) *evidencev1.MsgSubmitAuditBatch {
		return e.audit(t, e.auditor2, "ms-"+name, p+name, epoch, items...)
	}
	f1 := measure("F1", short("f1", spread(e.auditor2, "f1", 1, 0, now+600)), msgkit.OK)
	f8 := measure("F8", short("f8", spread(e.auditor2, "f8", 8, 0, now+600)), msgkit.OK)
	f32 := measure("F32", short("f32", spread(e.auditor2, "f32", 32, 0, now+600)), msgkit.OK)
	maxID := func(label string) string { return p + label + strings.Repeat("x", 64-len(p)-len(label)) }
	f32max := measure("F32max", e.audit(t, e.auditor2, maxID("bid-"), maxID("batch-"), epoch, spread(e.auditor2, "fm", 32, 64, now+600)...), msgkit.OK)
	rejected := spread(e.auditor2, "fr", 32, 0, now+600)
	for i, it := range rejected {
		it.ObservationDigest = digestOf(wrongDigest(fmt.Sprintf("fr-%d", i)))
	}
	f32rej := measure("F32rej", short("f32rej", rejected), msgkit.OK)
	// Rcount: the largest count of short-id items whose payload fits.
	pub2 := []byte(edPriv(t, e.auditor2).Public().(ed25519.PublicKey))
	count := int(genesis.MaxEvidenceBatch)
	for {
		probe := unsignedBatch(e.auditor2, pub2, "key-auditor-2", "ms-rcount", p+"rcount", epoch, spread(e.auditor2, "rc", count+1, 0, now+600)...)
		if _, err := evidence.AuditCanonical(probe); err != nil {
			break
		}
		count++
	}
	rcount := measure("Rcount", short("rcount", spread(e.auditor2, "rc", count, 0, now+600)), msgkit.Bounds)
	// Rpayload: the smallest count of maximal-id items above the cap.
	n := int(genesis.MaxEvidenceBatch)
	var oversize *evidencev1.MsgSubmitAuditBatch
	for {
		oversize = unsignedBatch(e.auditor2, pub2, "key-auditor-2", "ms-rpayload", p+"rpayload", epoch,
			spread(e.auditor2, "rp", n, 64, now+600)...)
		if _, err := evidence.AuditCanonical(oversize); errors.Is(err, evidence.ErrPayloadTooLarge) {
			break
		}
		n++
	}
	oversize.AuditorSignature.Signature = make([]byte, 64)
	rpayload := measure("Rpayload", oversize, msgkit.Bounds)
	for _, row := range []fixtureRow{rcount, rpayload} {
		if row.txBytes > 65536 {
			t.Fatalf("%s: transaction %d bytes is above the ante bound", row.name, row.txBytes)
		}
	}
	// Keys one batch of N items adds: N records, N lease index rows, the
	// batch marker, and the business id; the auditor's window row is added
	// by its first batch (F1) and overwritten afterwards.
	for _, row := range []fixtureRow{f1, f8, f32, f32max, f32rej} {
		want := 2*row.items + 2
		if row.name == "F1" {
			want++
		}
		if row.storeKeys != want {
			t.Fatalf("%s added %d evidence store keys, want %d", row.name, row.storeKeys, want)
		}
	}
	if f32max.maxRecordSize <= f32.maxRecordSize {
		t.Fatalf("F32max records (%d bytes) are not larger than F32 records (%d bytes)", f32max.maxRecordSize, f32.maxRecordSize)
	}

	// Q100: 100 more records of la in four batches of 25, then one page of
	// 100 by lease.
	for b := 0; b < 4; b++ {
		items := make([]*evidencev1.EvidenceItem, 25)
		for i := range items {
			items[i] = auditItem(fmt.Sprintf("%sq-%d-%02d", p, b, i), "la", "c1", manifestOf("c1"), now+600)
		}
		wantCode(t, fmt.Sprintf("q100 batch %d", b), e.send(t, c, e.auditor2, short(fmt.Sprintf("q100-%d", b), items)), msgkit.OK)
	}
	req := &evidencev1.QueryEvidenceRequest{LeaseId: "la", Page: &typesv1.PageRequest{Limit: 100}}
	used, limit, value := queryGas(t, chainApp, "/svn.evidence.v1.Query/Evidence", req)
	var page evidencev1.QueryEvidenceResponse
	if err := proto.Unmarshal(value, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.GetRecords()) != 100 || len(page.GetPage().GetNextPageKey()) == 0 || used >= limit {
		t.Fatalf("Q100: %d records, next key %x, gas %d of %d", len(page.GetRecords()), page.GetPage().GetNextPageKey(), used, limit)
	}
	t.Logf("fixture=Q100 records=%d response_bytes=%d query_gas=%d query_gas_limit=%d label=%q", len(page.GetRecords()), len(value), used, limit,
		"per-fixture measurement on one machine; no throughput or network-capacity claim")
	t.Logf("fixture counts: Rcount=%d items (step 11), Rpayload=%d items (step 8); inner verifications per batch: 1 (a design constant of deviation D-1, not a measurement)", count, n)
	c.checkEvidenceInvariants(t, "measurements")
}

// evidenceEvent is one golden row of an evidence event. Auditor id
// prefixes are written as "<auditor-1>." and "<auditor-2>.": the auditor
// keys are generated per run.
type evidenceEvent struct {
	Height     int64        `json:"height"`
	Type       string       `json:"type"`
	Attributes []goldenAttr `json:"attributes"`
}

const evidenceEventsGolden = "testdata/evidence-events.golden.json"

// TestEvidenceEventSchemas: every x/evidence event of a fixed flow (a
// RECORDED and a REJECTED item of auditor-1, an idempotent resend, a
// rejected batch, an auditor-2 batch) has its schema: the typed
// svn.events.v1.EvidenceAccepted parses with msgkit.ParseCommitted
// (evidence_id, batch_id, kind AUDITOR_ATTESTATION, attestation true, then
// msg_index), each is followed by evidence_state (evidence_id, lease_id,
// state RECORDED or REJECTED, msg_index), an idempotent resend emits only
// evidence_result idempotent=true, and a rejected message emits nothing.
// The rows match testdata/evidence-events.golden.json (-update rewrites
// it). Every event type and key passes the bounds.json forbidden-name scan.
func TestEvidenceEventSchemas(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	from := c.height()
	p1, p2 := prefix(e.auditor1), prefix(e.auditor2)
	now := c.now()
	first := e.audit(t, e.auditor1, "ev-1", p1+"ev-1", c.epoch(),
		auditItem(p1+"ev-1-a", "la", "c1", manifestOf("c1"), now+600), auditItem(p1+"ev-1-b", "lb", "c2", wrongDigest("ev-1-b"), now+600))
	wantCode(t, "first", e.send(t, c, e.auditor1, first), msgkit.OK)
	wantCode(t, "resend", e.send(t, c, e.auditor1, first), msgkit.OK)
	wantCode(t, "rejected", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "ev-2", p1+"ev-2", c.epoch(),
		auditItem(p1+"ev-2-a", "l404", "c1", manifestOf("c1"), now+600))), msgkit.NotFound)
	wantCode(t, "auditor-2", e.send(t, c, e.auditor2, e.audit(t, e.auditor2, "ev-3", p2+"ev-3", c.epoch(),
		auditItem(p2+"ev-3-a", "lc", "c3", manifestOf("c3"), now+600))), msgkit.OK)

	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	normalize := strings.NewReplacer(p1, "<auditor-1>.", p2, "<auditor-2>.")
	var rows []evidenceEvent
	for i, block := range c.blocks {
		height := c.inputs[i].height
		if height <= from {
			continue
		}
		for _, res := range block.TxResults {
			for _, ev := range res.Events {
				if !strings.HasPrefix(ev.Type, "svn.events.v1.Evidence") && !strings.HasPrefix(ev.Type, "evidence_") {
					continue
				}
				n := len(ev.Attributes)
				if n == 0 || ev.Attributes[n-1].Key != msgkit.MsgIndexKey || ev.Attributes[n-1].Value != "0" {
					t.Fatalf("%s does not end with msg_index 0: %v", ev.Type, ev.Attributes)
				}
				if hits := msgkittest.NameHits(ev.Type, patterns, exemptions); len(hits) != 0 {
					t.Fatalf("event type %s matches %v", ev.Type, hits)
				}
				row := evidenceEvent{Height: height - from, Type: ev.Type}
				var keys []string
				for _, attr := range ev.Attributes[:n-1] {
					if hits := msgkittest.NameHits(attr.Key, patterns, exemptions); len(hits) != 0 {
						t.Fatalf("%s key %s matches %v", ev.Type, attr.Key, hits)
					}
					keys = append(keys, attr.Key)
					row.Attributes = append(row.Attributes, goldenAttr{Key: attr.Key, Value: normalize.Replace(attr.Value)})
				}
				switch ev.Type {
				case "svn.events.v1.EvidenceAccepted":
					msg, index, err := msgkit.ParseCommitted(ev)
					if err != nil || index != 0 || string(proto.MessageName(msg)) != ev.Type {
						t.Fatalf("%s: %v %d %v", ev.Type, msg, index, err)
					}
					if strings.Join(keys, ",") != "evidence_id,batch_id,kind,attestation" || ev.Attributes[2].Value != "AUDITOR_ATTESTATION" || ev.Attributes[3].Value != "true" {
						t.Fatalf("%s attributes %v", ev.Type, ev.Attributes)
					}
				case evidence.EventState:
					if strings.Join(keys, ",") != "evidence_id,lease_id,state" || (ev.Attributes[2].Value != "RECORDED" && ev.Attributes[2].Value != "REJECTED") {
						t.Fatalf("%s attributes %v", ev.Type, ev.Attributes)
					}
				case "evidence_result":
					if strings.Join(keys, ",") != "idempotent" || ev.Attributes[0].Value != "true" {
						t.Fatalf("%s attributes %v", ev.Type, ev.Attributes)
					}
				default:
					t.Fatalf("unexpected evidence event %s", ev.Type)
				}
				rows = append(rows, row)
			}
		}
	}
	if len(rows) != 2+2+1+2 {
		t.Fatalf("%d evidence events: %v", len(rows), rows)
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(evidenceEventsGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(evidenceEventsGolden, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(evidenceEventsGolden)
	if err != nil {
		t.Fatalf("%v (run go test ./app -run TestEvidenceEventSchemas -update to create it)", err)
	}
	if !bytes.Equal(golden, raw) {
		t.Fatalf("evidence events differ from %s:\n%s", evidenceEventsGolden, raw)
	}
}

// TestNoPrivateDataInEvidenceStateOrEvents runs audits whose off-chain
// inputs carry the synthetic canaries (an endpoint URL, an email, a
// username, a watch event, a complaint narrative, media bytes): only their
// SHA-256 reaches a transaction (as a REJECTED observation digest). Every
// store, every committed transaction, and every event of the run hold no
// canary and no URL scheme. Opaque ids and digests stay linkable by anyone
// holding their preimage.
func TestNoPrivateDataInEvidenceStateOrEvents(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	p := prefix(e.auditor1)
	now := c.now()
	var items []*evidencev1.EvidenceItem
	for i, canary := range canaries() {
		items = append(items, auditItem(fmt.Sprintf("%spd-%d", p, i), "la", "c1", sha(canary), now+600))
	}
	wantCode(t, "canary digests", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "pd-1", p+"pd-1", c.epoch(), items...)), msgkit.OK)
	list := canaries()
	web := [][]byte{[]byte("https://"), []byte("http://")}
	for _, name := range append(app.SnapshotStores(), "acc") {
		blob := c.app().ModuleSnapshot(name)
		if hit, found := msgkittest.FindCanary(blob, list...); found {
			t.Fatalf("store %s holds %s", name, hit)
		}
		for _, w := range web {
			if bytes.Contains(blob, w) {
				t.Fatalf("store %s holds %q", name, w)
			}
		}
	}
	var scanned int
	for i, block := range c.blocks {
		for _, tx := range c.inputs[i].txs {
			if hit, found := msgkittest.FindCanary(tx, list...); found {
				t.Fatalf("height %d tx holds %s", c.inputs[i].height, hit)
			}
		}
		var events []abci.Event
		events = append(events, block.Events...)
		for _, res := range block.TxResults {
			events = append(events, res.Events...)
		}
		for _, ev := range events {
			var blob []byte
			blob = append(blob, ev.Type...)
			for _, attr := range ev.Attributes {
				blob = append(append(blob, attr.Key...), attr.Value...)
			}
			if hit, found := msgkittest.FindCanary(blob, list...); found {
				t.Fatalf("event %s holds %s", ev.Type, hit)
			}
			for _, w := range web {
				if bytes.Contains(blob, w) {
					t.Fatalf("event %s holds %q", ev.Type, w)
				}
			}
			scanned++
		}
	}
	t.Logf("%d stores, %d blocks, %d events scanned", len(app.SnapshotStores())+1, len(c.blocks), scanned)
}

// TestEvidenceQueryGasAndBounds: Query/Evidence through the gRPC query
// router (the path ABCI queries take) runs on the node's finite query gas
// meter, refuses a request without an id (MALFORMED), an id outside the id
// pattern, a page limit above the effective cap, and a page key outside
// the id pattern (BOUNDS), and a page key with an evidence id (BOUNDS); a
// limit of 0 is the cap. Each refusal is a query error with the svn code.
func TestEvidenceQueryGasAndBounds(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	evidenceSetup(t, e, c)
	p := prefix(e.auditor1)
	now := c.now()
	wantCode(t, "batch", e.send(t, c, e.auditor1, e.audit(t, e.auditor1, "qb-1", p+"qb-1", c.epoch(), spread(e.auditor1, "qb", 6, 0, now+600)...)), msgkit.OK)
	chainApp := c.app()
	path := "/svn.evidence.v1.Query/Evidence"
	for _, row := range []struct {
		name string
		req  *evidencev1.QueryEvidenceRequest
		want msgkit.Code
	}{
		{"no id", &evidencev1.QueryEvidenceRequest{}, msgkit.Malformed},
		{"evidence id outside the pattern", &evidencev1.QueryEvidenceRequest{EvidenceId: "QB"}, msgkit.Bounds},
		{"lease id outside the pattern", &evidencev1.QueryEvidenceRequest{LeaseId: "LA"}, msgkit.Bounds},
		{"limit above the cap", &evidencev1.QueryEvidenceRequest{LeaseId: "la", Page: &typesv1.PageRequest{Limit: 101}}, msgkit.Bounds},
		{"page key outside the pattern", &evidencev1.QueryEvidenceRequest{LeaseId: "la", Page: &typesv1.PageRequest{PageKey: []byte("KEY")}}, msgkit.Bounds},
		{"page key with an evidence id", &evidencev1.QueryEvidenceRequest{EvidenceId: p + "qb-00", Page: &typesv1.PageRequest{PageKey: []byte(p + "qb-00")}}, msgkit.Bounds},
	} {
		assertQueryCode(t, chainApp, path, row.req, row.want)
	}
	used, limit, value := queryGas(t, chainApp, path, &evidencev1.QueryEvidenceRequest{LeaseId: "la", Page: &typesv1.PageRequest{Limit: 0}})
	var out evidencev1.QueryEvidenceResponse
	if err := proto.Unmarshal(value, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.GetRecords()) != 2 || used == 0 || used >= limit {
		t.Fatalf("limit 0: %d records, gas %d of %d", len(out.GetRecords()), used, limit)
	}
	t.Logf("Query/Evidence by lease, 2 records: gas %d of the %d query limit", used, limit)
	// The lease's records are ordered by evidence id; their leases bind.
	for _, rec := range out.GetRecords() {
		if rec.GetLeaseId() != "la" {
			t.Fatalf("record of %s in la's page", rec.GetLeaseId())
		}
	}
}
