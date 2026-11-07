package difftest

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
)

// Measurements and benchmarks on declared fixtures (plan section 19). They
// run the real keepers of this package's harness, keeper level: they
// exclude the ante handler (transaction signature checks and its gas),
// block execution, and commit. The app-level counterpart through
// FinalizeBlock is TestAuditBatchMeasurements in chain/app. Every figure is
// a per-fixture measurement on one machine; none is a throughput or
// network-capacity claim, and none is extrapolated.

// measureLabel is printed with every measured row.
const measureLabel = "per-fixture keeper-level measurement on one machine; no throughput or network-capacity claim"

// benchState is the prepared pre-state of the measured fixtures: providers
// p0-p2, contents c0-c4, held leases sl0-sl3 (deadline long; sl<i> holds
// content c<i>), content c3 withdrawn (its lease sl3 stays held), and an
// open offer "bo" (provider p1, content c4) for the AcceptLease baseline.
// Every step of the setup is checked to succeed.
type benchState struct {
	f      *fixture
	h      *harness
	accept *step
}

func newBenchState(tb testing.TB, checks bool) *benchState {
	tb.Helper()
	f := newFixture(nil, long, long, long, long)
	f.cfg.trialCheck, f.cfg.invariants = checks, checks
	f.withdraw("c3")
	f.add(buildRegisterContent("content-c4", "c4", contentBytes))
	offer := offerParams{
		business: "bench-offer", offerID: "bo", contentID: "c4", providerID: "p1", size: contentBytes,
		deadline: long, expiry: startTime + 600,
	}
	f.add(buildOffer(offer))
	h := newHarness(tb, f.cfg)
	for i, s := range f.steps {
		if out := h.apply(s); out.code != msgkit.OK {
			tb.Fatalf("bench setup step %d %s: %s", i, s.op, out.code)
		}
	}
	return &benchState{f: f, h: h, accept: buildAccept("bench-accept", offerRecord(offer), "bl", "p1-k1")}
}

// spread is n items of signer over sl0, sl1, sl2 in order (sl0 gets the
// first third, rounded up; F8 is 3, 3, 2 and F32 is 11, 11, 10), even
// items RECORDED (the lease's manifest digest) and odd items REJECTED
// (another digest), or every item REJECTED. Evidence ids are
// prefix + name + "-NN", padded with "0" before the counter to width
// characters when width is larger. Every expiry is now + 600.
func (bs *benchState) spread(signer []byte, name string, n, width int, rejectAll bool) []auditItem {
	items := make([]auditItem, n)
	for i := range items {
		leaseID := []string{"sl0", "sl1", "sl2"}[i*3/n]
		it := bs.f.item(signer, leaseID, 600)
		id := fmt.Sprintf("%s%s-%02d", prefixOf(signer), name, i)
		if width > len(id) {
			id = fmt.Sprintf("%s%s-%s%02d", prefixOf(signer), name, strings.Repeat("0", width-len(id)), i)
		}
		it.evidenceID = id
		if rejectAll || i%2 == 1 {
			it.digest = sha("observed other bytes:" + id)
		}
		items[i] = it
	}
	return items
}

// maxID is a 64-character id with signer's prefix.
func maxID(signer []byte, label string) string {
	p := prefixOf(signer) + label
	return p + strings.Repeat("x", 64-len(p))
}

// measuredFixture is one declared fixture: the message and the code the
// chain must return.
type measuredFixture struct {
	name  string
	step  *step
	items int
	want  msgkit.Code
	// what says where a rejected fixture stops.
	what string
}

// fixtures builds the declared fixtures of plan section 19 by auditor A, in
// the current epoch, each with a valid signature:
//
//   - F1: 1 item of sl0, RECORDED, short ids;
//   - F8: 8 items (sl0 x3, sl1 x3, sl2 x2), RECORDED and REJECTED alternating;
//   - F32: 32 items (sl0 x11, sl1 x11, sl2 x10), alternating, short ids;
//   - F32max: as F32 with 64-character evidence ids, batch id, and business id;
//   - F32rej: 32 items, all REJECTED;
//   - F32tail: as F32, but the last item audits sl3, whose content c3 is
//     withdrawn: rejected at the last item check (T8, WITHDRAWN) after
//     every earlier check walked all 32 items;
//   - Rcount: the largest item count of short ids whose canonical payload
//     still fits in 65536 bytes: rejected at the count check (step 11,
//     BOUNDS) after the signature step verified it over every item;
//   - Rpayload: the smallest item count of 64-character ids whose canonical
//     payload is above 65536 bytes: rejected at the signature step
//     (step 8, BOUNDS).
//
// Rcount and Rpayload messages stay within the 65536-byte transaction bound
// of msgkit.Enforce (step 1), which the harness applies to the encoded
// message.
func (bs *benchState) fixtures(tb testing.TB) []measuredFixture {
	tb.Helper()
	a := auditorA
	short := func(name string, items []auditItem) *step {
		return auditParams{
			signer: a, business: "ms-" + name, batchID: prefixOf(a) + name, epoch: epochOf(bs.f.now),
			items: items, variant: sigValid,
		}.build("")
	}
	tail := bs.spread(a, "f32t", 32, 0, false)
	last := bs.f.item(a, "sl3", 600)
	last.evidenceID = prefixOf(a) + "f32t-31"
	tail[31] = last
	out := []measuredFixture{
		{name: "F1", step: short("f1", bs.spread(a, "f1", 1, 0, false)), want: msgkit.OK},
		{name: "F8", step: short("f8", bs.spread(a, "f8", 8, 0, false)), want: msgkit.OK},
		{name: "F32", step: short("f32", bs.spread(a, "f32", 32, 0, false)), want: msgkit.OK},
		{name: "F32max", step: auditParams{
			signer: a, business: maxID(a, "bid-"), batchID: maxID(a, "batch-"), epoch: epochOf(bs.f.now),
			items: bs.spread(a, "fm", 32, 64, false), variant: sigValid,
		}.build(""), want: msgkit.OK},
		{name: "F32rej", step: short("f32rej", bs.spread(a, "fr", 32, 0, true)), want: msgkit.OK},
		{name: "F32tail", step: short("f32tail", tail), want: msgkit.Withdrawn, what: "T8, item 32"},
	}
	fits := func(s *step) bool {
		_, err := evidence.AuditCanonical(auditBatch(s))
		if err != nil && !errors.Is(err, evidence.ErrPayloadTooLarge) {
			tb.Fatalf("canonical payload: %v", err)
		}
		return err == nil
	}
	count := int(genesis.MaxEvidenceBatch)
	for fits(short("rcount", bs.spread(a, "rc", count+1, 0, false))) {
		count++
	}
	out = append(out, measuredFixture{name: "Rcount", step: short("rcount", bs.spread(a, "rc", count, 0, false)),
		want: msgkit.Bounds, what: fmt.Sprintf("step 11 after the signature over %d items", count)})
	n := int(genesis.MaxEvidenceBatch)
	for fits(short("rpayload", bs.spread(a, "rp", n, 64, false))) {
		n++
	}
	out = append(out, measuredFixture{name: "Rpayload", step: short("rpayload", bs.spread(a, "rp", n, 64, false)),
		want: msgkit.Bounds, what: fmt.Sprintf("step 8, %d items above the payload cap", n)})
	for i := range out {
		out[i].items = len(stepItems(out[i].step))
		if size := proto.Size(out[i].step.msg); uint64(size) > genesis.MaxTxBytes {
			tb.Fatalf("%s: message %d bytes is above the transaction bound", out[i].name, size)
		}
	}
	return out
}

// storeRows is the key count and the logical key + value bytes of one
// store (not IAVL or disk bytes).
func storeRows(ctx sdk.Context, key *storetypes.KVStoreKey) (keys, size int) {
	it := ctx.KVStore(key).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		keys++
		size += len(it.Key()) + len(it.Value())
	}
	return keys, size
}

// measuredRow is one logged measurement.
type measuredRow struct {
	name                       string
	items, msgBytes, payload   int
	code                       msgkit.Code
	gas                        uint64
	eventBytes                 int
	storeKeys, storeBytes, rec int
}

// measure delivers one step on bs.h (committing it when accepted) and
// returns its row: the encoded message bytes, the keeper-level gas
// (msgkit.FixedMessageGas plus the store gas of the handler, without any
// ante gas; gas is not CPU time), the event bytes, the growth of the
// evidence store, the largest stored record, and the canonical payload
// bytes (0 when above the cap).
func (bs *benchState) measure(t *testing.T, name string, s *step, want msgkit.Code) measuredRow {
	t.Helper()
	ctx := bs.h.unmetered()
	keysBefore, bytesBefore := storeRows(ctx, bs.h.keys[keyEvidence])
	out := bs.h.apply(s)
	if out.code != want {
		t.Fatalf("%s: code %s, want %s", name, out.code, want)
	}
	keysAfter, bytesAfter := storeRows(bs.h.unmetered(), bs.h.keys[keyEvidence])
	row := measuredRow{
		name: name, msgBytes: proto.Size(s.msg), code: out.code, gas: out.gas, eventBytes: out.eventBytes,
		storeKeys: keysAfter - keysBefore, storeBytes: bytesAfter - bytesBefore,
	}
	if s.op == opAudit {
		row.items = len(stepItems(s))
		if text, err := evidence.AuditCanonical(auditBatch(s)); err == nil {
			row.payload = len(text)
		}
		if want == msgkit.OK {
			store := bs.h.unmetered().KVStore(bs.h.keys[keyEvidence])
			for _, it := range stepItems(s) {
				row.rec = max(row.rec, len(store.Get(evidence.RecordKey(it.GetEvidenceId()))))
			}
		}
	}
	return row
}

func (r measuredRow) log(t *testing.T, note string) {
	t.Helper()
	code := string(r.code)
	if r.code == msgkit.OK {
		code = "OK"
	}
	perItem := func(v int) string {
		if r.items == 0 {
			return "-"
		}
		return fmt.Sprintf("%.1f", float64(v)/float64(r.items))
	}
	t.Logf("fixture=%s items=%d code=%s msg_bytes=%d keeper_gas=%d event_bytes=%d evidence_store_keys=+%d evidence_store_bytes=+%d "+
		"max_record_bytes=%d canonical_payload_bytes=%d per_item: msg_bytes=%s keeper_gas=%s evidence_store_bytes=%s%s label=%q",
		r.name, r.items, code, r.msgBytes, r.gas, r.eventBytes, r.storeKeys, r.storeBytes, r.rec, r.payload,
		perItem(r.msgBytes), perItem(int(r.gas)), perItem(r.storeBytes), note, measureLabel)
}

// TestSubmitAuditBatchMeasurements measures the declared fixtures
// (benchState.fixtures) on the real keepers, delivered one after another
// on one prepared state (accepted batches are committed), and an
// AcceptLease on the same state as the lease-message baseline. It logs,
// per fixture and per item, the encoded message bytes, the keeper-level
// gas, the event bytes, the growth of the evidence store in keys and
// logical bytes, the largest stored record, and the canonical payload
// bytes, and it checks:
//
//   - an accepted batch of N items adds exactly 2N + 2 evidence store keys
//     (N records, N lease-index rows, the batch marker, the business id),
//     plus the auditor's window row on its first batch (F1);
//   - a rejected fixture adds no key and no byte;
//   - 64-character ids make larger records than short ids;
//   - Rcount and Rpayload are the computed item counts of plan section 19.
//
// The trial and I9 checks of the harness run on every step. Inner
// signature verifications per batch: 1 (a design constant of deviation
// D-1, not a measurement). CPU time is BenchmarkSubmitAuditBatch's.
func TestSubmitAuditBatchMeasurements(t *testing.T) {
	bs := newBenchState(t, true)
	rows := map[string]measuredRow{}
	for _, fx := range bs.fixtures(t) {
		row := bs.measure(t, fx.name, fx.step, fx.want)
		note := ""
		if fx.what != "" {
			note = " rejected_at=" + strings.ReplaceAll(fx.what, " ", "_")
		}
		row.log(t, note)
		rows[fx.name] = row
	}
	baseline := bs.measure(t, "AcceptLease", bs.accept, msgkit.OK)
	baseline.log(t, " baseline=lease_message")

	for _, name := range []string{"F1", "F8", "F32", "F32max", "F32rej"} {
		r := rows[name]
		want := 2*r.items + 2
		if name == "F1" {
			want++
		}
		if r.storeKeys != want {
			t.Errorf("%s added %d evidence store keys, want %d", name, r.storeKeys, want)
		}
	}
	for _, name := range []string{"F32tail", "Rcount", "Rpayload"} {
		if r := rows[name]; r.storeKeys != 0 || r.storeBytes != 0 {
			t.Errorf("rejected %s grew the evidence store by %d keys, %d bytes", name, r.storeKeys, r.storeBytes)
		}
	}
	if rows["F32max"].rec <= rows["F32"].rec {
		t.Errorf("F32max records (%d bytes) are not larger than F32 records (%d bytes)", rows["F32max"].rec, rows["F32"].rec)
	}
	if rows["Rcount"].items <= int(genesis.MaxEvidenceBatch) || rows["Rpayload"].payload != 0 || rows["Rcount"].payload == 0 {
		t.Errorf("Rcount %d items (payload %d bytes), Rpayload payload %d bytes", rows["Rcount"].items, rows["Rcount"].payload, rows["Rpayload"].payload)
	}
	if baseline.storeKeys != 0 {
		t.Errorf("the AcceptLease baseline wrote %d evidence store keys", baseline.storeKeys)
	}
	t.Logf("fixture counts: Rcount=%d items (step 11), Rpayload=%d items (step 8); inner signature verifications per batch: 1 (a design constant of deviation D-1, not a measurement)",
		rows["Rcount"].items, rows["Rpayload"].items)
}

// benchDeliver runs s on a cached branch of bs's committed state per
// iteration and discards the branch, so every iteration starts from the
// same pre-state. The timer covers only the message server call: building
// the branch, the gas meter, and the event manager, and checking the code,
// are outside it. It reports the keeper-level gas, the item count, and the
// encoded message bytes per operation, and the time per item.
func (bs *benchState) benchDeliver(b *testing.B, s *step, want msgkit.Code) {
	b.Helper()
	txBytes := make([]byte, proto.Size(s.msg))
	if s.op != opAudit {
		txBytes = make([]byte, txBytesLen)
	}
	var gas uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx := bs.h.ctx.WithEventManager(sdk.NewEventManager()).
			WithGasMeter(storetypes.NewGasMeter(txGasLimit)).WithTxBytes(txBytes)
		cache, _ := ctx.CacheContext()
		b.StartTimer()
		err := bs.h.dispatch(cache, s.msg)
		b.StopTimer()
		if code := codeOf(b, err); code != want {
			b.Fatalf("code %s, want %s", code, want)
		}
		gas = ctx.GasMeter().GasConsumed()
		b.StartTimer()
	}
	b.StopTimer()
	items := 0
	if s.op == opAudit {
		items = len(stepItems(s))
	}
	b.ReportMetric(float64(gas), "gas/op")
	b.ReportMetric(float64(proto.Size(s.msg)), "msgbytes/op")
	if items > 0 {
		b.ReportMetric(float64(items), "items/op")
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(items), "ns/item")
	}
}

// BenchmarkSubmitAuditBatch measures the x/evidence message server on the
// declared fixtures (benchState.fixtures: F1, F8, F32, F32max, F32rej, and
// the rejected worst cases F32tail, Rcount, Rpayload) on the real keepers,
// each iteration from the same prepared state (benchDeliver). Run it with
// -benchmem; one machine, CPU from the run's runtime record, not a
// throughput figure. Gas is not CPU time.
func BenchmarkSubmitAuditBatch(b *testing.B) {
	bs := newBenchState(b, false)
	for _, fx := range bs.fixtures(b) {
		b.Run(fx.name, func(b *testing.B) { bs.benchDeliver(b, fx.step, fx.want) })
	}
}

// BenchmarkAcceptLeaseBaseline measures one AcceptLease (a broadcaster
// relaying the provider's acceptance of the open offer "bo") on the same
// prepared state and in the same way as BenchmarkSubmitAuditBatch, as the
// lease-message baseline of plan section 19.
func BenchmarkAcceptLeaseBaseline(b *testing.B) {
	bs := newBenchState(b, false)
	bs.benchDeliver(b, bs.accept, msgkit.OK)
}

// TestBenchFixturesReturnTheirCodes runs every fixture of the benchmarks
// once, as benchDeliver does, on a cached branch of one fresh prepared
// state: each returns its declared code from that state, independent of
// the delivery order of TestSubmitAuditBatchMeasurements.
func TestBenchFixturesReturnTheirCodes(t *testing.T) {
	bs := newBenchState(t, true)
	for _, fx := range append(bs.fixtures(t), measuredFixture{name: "AcceptLease", step: bs.accept, want: msgkit.OK}) {
		ctx := bs.h.ctx.WithEventManager(sdk.NewEventManager()).WithGasMeter(storetypes.NewGasMeter(txGasLimit)).
			WithTxBytes(make([]byte, proto.Size(fx.step.msg)))
		cache, _ := ctx.CacheContext()
		if code := codeOf(t, bs.h.dispatch(cache, fx.step.msg)); code != fx.want {
			t.Errorf("%s: code %s from the prepared state, want %s", fx.name, code, fx.want)
		}
	}
}
