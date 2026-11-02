package evidence_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// genesisRecord is a valid record of the auditor of label for lease
// leaseID (whose content is contentID), RECORDED with the manifest digest.
func genesisRecord(label, id, batchID, leaseID, contentID string, epoch uint64, expiry int64) *evidencev1.EvidenceRecord {
	return &evidencev1.EvidenceRecord{
		EvidenceId: prefix(label) + id, BatchId: prefix(label) + batchID,
		Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION, State: evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED,
		SignerPubkey: pub(label), LeaseId: leaseID, ContentId: contentID, ContentVersion: 1,
		ObservationDigest: manifestOf(contentID), Epoch: epoch, ExpiryUnixSeconds: expiry, SchemaVersion: 1, Attestation: true,
	}
}

// validGenesis matches the leases of genesisEnv: auditor-1 batch gb1 with a
// RECORDED item on l1 and a REJECTED item on l2 in the current epoch, and
// auditor-2 batch gb2 with a RECORDED item on l3 in the previous epoch.
func validGenesis() *evidencev1.GenesisState {
	rejected := genesisRecord(auditor1, "g2", "gb1", "l2", "c2", epochNow, now+itemLife)
	rejected.State = evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED
	rejected.ObservationDigest = digestOf("other")
	return &evidencev1.GenesisState{Records: []*evidencev1.EvidenceRecord{
		genesisRecord(auditor1, "g1", "gb1", "l1", "c1", epochNow, now+itemLife),
		rejected,
		genesisRecord(auditor2, "g3", "gb2", "l3", "c3", epochNow-1, now+100),
	}}
}

// TestGenesisValidateImportExport: one invalid document per rule of
// ValidateGenesis and of the InitGenesis cross-module checks; InitGenesis
// refuses a non-empty store (the scaffold key [0x01] included) and rebuilds
// every derived row.
func TestGenesisValidateImportExport(t *testing.T) {
	if err := evidence.ValidateGenesis(validGenesis()); err != nil {
		t.Fatal(err)
	}
	if err := evidence.ValidateGenesis(&evidencev1.GenesisState{}); err != nil {
		t.Fatal(err)
	}
	if evidence.ValidateGenesis(nil) == nil {
		t.Fatal("nil state validated")
	}
	invalid := map[string]func(g *evidencev1.GenesisState){
		"current-epoch-1": func(g *evidencev1.GenesisState) { g.CurrentEpoch = 1 },
		"state-unknown-field": func(g *evidencev1.GenesisState) {
			g.ProtoReflect().SetUnknown(protowire.AppendTag(nil, 99, protowire.VarintType))
		},
		"nil-record":           func(g *evidencev1.GenesisState) { g.Records[0] = nil },
		"record-unknown-field": func(g *evidencev1.GenesisState) { g.Records[0].ProtoReflect().SetUnknown(unknownField()) },
		"digest-unknown-field": func(g *evidencev1.GenesisState) {
			g.Records[0].ObservationDigest.ProtoReflect().SetUnknown(unknownField())
		},
		"duplicate-evidence-id": func(g *evidencev1.GenesisState) { g.Records[1].EvidenceId = g.Records[0].EvidenceId },
		"evidence-id-pattern":   func(g *evidencev1.GenesisState) { g.Records[0].EvidenceId = "E1" },
		"batch-id-pattern":      func(g *evidencev1.GenesisState) { g.Records[0].BatchId = "" },
		"lease-id-pattern":      func(g *evidencev1.GenesisState) { g.Records[0].LeaseId = "L1" },
		"content-id-pattern":    func(g *evidencev1.GenesisState) { g.Records[0].ContentId = "-c" },
		"kind-0":                func(g *evidencev1.GenesisState) { g.Records[0].Kind = 0 },
		"kind-provider-receipt": func(g *evidencev1.GenesisState) { g.Records[0].Kind = 1 },
		"kind-4":                func(g *evidencev1.GenesisState) { g.Records[0].Kind = 4 },
		"state-0":               func(g *evidencev1.GenesisState) { g.Records[0].State = 0 },
		"state-7":               func(g *evidencev1.GenesisState) { g.Records[0].State = 7 },
		"signer-pubkey-31":      func(g *evidencev1.GenesisState) { g.Records[0].SignerPubkey = g.Records[0].SignerPubkey[:31] },
		"evidence-id-prefix":    func(g *evidencev1.GenesisState) { g.Records[0].EvidenceId = prefix(auditor2) + "g1" },
		"batch-id-prefix":       func(g *evidencev1.GenesisState) { g.Records[2].BatchId = prefix(auditor1) + "gb2" },
		"content-version-0":     func(g *evidencev1.GenesisState) { g.Records[0].ContentVersion = 0 },
		"content-version-2^53":  func(g *evidencev1.GenesisState) { g.Records[0].ContentVersion = 1 << 53 },
		"digest-nil":            func(g *evidencev1.GenesisState) { g.Records[0].ObservationDigest = nil },
		"digest-algorithm-0":    func(g *evidencev1.GenesisState) { g.Records[0].ObservationDigest.Algorithm = 0 },
		"digest-31-bytes":       func(g *evidencev1.GenesisState) { g.Records[0].ObservationDigest.Value = make([]byte, 31) },
		"expiry-0":              func(g *evidencev1.GenesisState) { g.Records[0].ExpiryUnixSeconds = 0 },
		"expiry-above-9999":     func(g *evidencev1.GenesisState) { g.Records[0].ExpiryUnixSeconds = genesis.UnixSecondsMax + 1 },
		"epoch-above-9999":      func(g *evidencev1.GenesisState) { g.Records[0].Epoch = evidence.EpochAt(genesis.UnixSecondsMax) + 1 },
		"expiry-at-epoch-start": func(g *evidencev1.GenesisState) {
			g.Records[0].ExpiryUnixSeconds = int64(epochNow) * evidence.EpochSeconds
		},
		"expiry-beyond-lifetime": func(g *evidencev1.GenesisState) { g.Records[0].ExpiryUnixSeconds = int64(epochNow+1)*3600 + 86400 },
		"schema-version-0":       func(g *evidencev1.GenesisState) { g.Records[0].SchemaVersion = 0 },
		"schema-version-2":       func(g *evidencev1.GenesisState) { g.Records[0].SchemaVersion = 2 },
		"attestation-false":      func(g *evidencev1.GenesisState) { g.Records[0].Attestation = false },
		"batch-mixes-epochs": func(g *evidencev1.GenesisState) {
			g.Records[1].Epoch = epochNow - 1
			g.Records[1].ExpiryUnixSeconds = now
		},
		"batch-mixes-signer-keys": func(g *evidencev1.GenesisState) { mixKeys(g) },
		"batch-above-32-records":  func(g *evidencev1.GenesisState) { g.Records = append(g.Records, sameBatch(33, 0)...) },
		"auditor-epoch-above-cap": func(g *evidencev1.GenesisState) { g.Records = append(g.Records, overCap()...) },
	}
	for name, edit := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			g := validGenesis()
			edit(g)
			if err := evidence.ValidateGenesis(g); err == nil {
				t.Fatal("validated")
			}
			e := genesisEnv(t)
			before := e.snapshot(e.unmetered())
			if err := e.k.InitGenesis(e.unmetered(), g); err == nil {
				t.Fatal("imported")
			}
			if !bytes.Equal(before, e.snapshot(e.unmetered())) {
				t.Fatal("a refused import wrote")
			}
		})
	}
	// The boundaries of the expiry window are accepted.
	for name, expiry := range map[string]int64{
		"expiry-epoch-start-plus-1":       int64(epochNow)*3600 + 1,
		"expiry-last-second-of-lifetime":  int64(epochNow+1)*3600 + 86400 - 1,
		"expiry-at-lease-deadline-bounds": leaseDeadline,
	} {
		g := validGenesis()
		g.Records[0].ExpiryUnixSeconds = expiry
		err := evidence.ValidateGenesis(g)
		if name == "expiry-at-lease-deadline-bounds" {
			if err == nil {
				t.Fatalf("%s: an expiry 30 days after its epoch validated", name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := evidence.ValidateGenesis(&evidencev1.GenesisState{Records: append(sameBatch(32, 0), atCap()...)}); err != nil {
		t.Fatalf("32 records in a batch and 1024 per auditor and epoch: %v", err)
	}

	crossModule := map[string]func(e *env, g *evidencev1.GenesisState){
		"lease-not-found": func(_ *env, g *evidencev1.GenesisState) { g.Records[0].LeaseId = "l-none" },
		"content-id-differs": func(_ *env, g *evidencev1.GenesisState) {
			g.Records[0].ContentId = "c2"
			g.Records[0].ObservationDigest = manifestOf("c2")
		},
		"content-version-differs": func(_ *env, g *evidencev1.GenesisState) { g.Records[0].ContentVersion = 2 },
		"recorded-other-digest":   func(_ *env, g *evidencev1.GenesisState) { g.Records[0].ObservationDigest = digestOf("x") },
		"expired-other-digest": func(_ *env, g *evidencev1.GenesisState) {
			g.Records[0].State = evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED
			g.Records[0].ObservationDigest = digestOf("x")
		},
		"rejected-manifest-digest": func(_ *env, g *evidencev1.GenesisState) { g.Records[1].ObservationDigest = manifestOf("c2") },
		"expiry-after-lease-deadline": func(e *env, _ *evidencev1.GenesisState) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now + itemLife - 1 })
		},
		"epoch-after-current": func(e *env, _ *evidencev1.GenesisState) { e.ctx = e.at(now - 601) },
	}
	for name, edit := range crossModule {
		t.Run("cross-module/"+name, func(t *testing.T) {
			e := genesisEnv(t)
			g := validGenesis()
			edit(e, g)
			if err := evidence.ValidateGenesis(g); err != nil {
				t.Fatalf("the document itself is invalid: %v", err)
			}
			before := e.snapshot(e.unmetered())
			if err := e.k.InitGenesis(e.unmetered(), g); err == nil {
				t.Fatal("imported")
			}
			if !bytes.Equal(before, e.snapshot(e.unmetered())) {
				t.Fatal("a refused import wrote")
			}
		})
	}
	t.Run("requires-an-empty-store", func(t *testing.T) {
		for name, key := range map[string][]byte{"scaffold-key": {0x01}, "layout-key": msgkit.LayoutVersionKey, "window": evidence.AuditorWindowKey(addr(auditor1))} {
			e := genesisEnv(t)
			e.store(evidence.ModuleName).Set(key, []byte(`{"records":[],"current_epoch":"0"}`))
			if err := e.k.InitGenesis(e.unmetered(), &evidencev1.GenesisState{}); err == nil {
				t.Fatalf("%s: imported over a non-empty store", name)
			}
		}
	})
	t.Run("derived-rows", func(t *testing.T) {
		e := genesisEnv(t)
		g := validGenesis()
		if err := e.k.InitGenesis(e.unmetered(), g); err != nil {
			t.Fatal(err)
		}
		e.requireInvariants()
		store := e.store(evidence.ModuleName)
		for _, r := range g.GetRecords() {
			if !bytes.Equal(store.Get(evidence.BatchKey(r.GetBatchId())), evidence.Marker) ||
				!bytes.Equal(store.Get(evidence.LeaseIndexKey(r.GetLeaseId(), r.GetEvidenceId())), evidence.Marker) {
				t.Fatalf("derived rows of %q", r.GetEvidenceId())
			}
		}
		if epoch, count := e.k.WindowForTest(e.unmetered(), addr(auditor1)); epoch != epochNow || count != 2 {
			t.Fatalf("auditor-1 window (%d, %d)", epoch, count)
		}
		if epoch, count := e.k.WindowForTest(e.unmetered(), addr(auditor2)); epoch != epochNow-1 || count != 1 {
			t.Fatalf("auditor-2 window (%d, %d)", epoch, count)
		}
		// auditor-2's next batch in the current epoch starts a new window.
		e.mustSubmit(signed(auditor2, "n", e.item(auditor2, "n", "l3")))
		if epoch, count := e.k.WindowForTest(e.unmetered(), addr(auditor2)); epoch != epochNow || count != 1 {
			t.Fatalf("auditor-2 window after a batch (%d, %d)", epoch, count)
		}
		e.requireInvariants()
	})
}

func unknownField() []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, 99, protowire.VarintType), 1)
}

// collisionRecords are two records of one batch on lease l1, one under each
// key of evidencetest.PrefixCollisionKeys (two addresses with one id
// prefix), in the current epoch. Every record rule holds for each of them,
// the prefix rule included; batchOf names their batch ids.
func collisionRecords(batchOf func(i int) string) []*evidencev1.EvidenceRecord {
	a, b := evidencetest.PrefixCollisionKeys()
	p := evidence.IDPrefix(evidence.AuditorAddress(a))
	out := make([]*evidencev1.EvidenceRecord, 2)
	for i, key := range [][]byte{a, b} {
		r := genesisRecord(auditor1, "", "", "l1", "c1", epochNow, now+itemLife)
		r.EvidenceId, r.BatchId, r.SignerPubkey = p+fmt.Sprintf("m%d", i), p+batchOf(i), key
		out[i] = r
	}
	return out
}

// mixKeys adds a batch whose two records are signed by two keys with one id
// prefix (evidencetest.PrefixCollisionKeys): the batch signer-key rule
// alone refuses it (TestGenesisBatchConsistency checks the message). With
// two label keys the record-level prefix rule would refuse the document
// first.
func mixKeys(g *evidencev1.GenesisState) {
	g.Records = append(g.Records, collisionRecords(func(int) string { return "mixed" })...)
}

// sameBatch is n records of one batch of auditor-1 on lease l1, ids
// s<start>..s<start+n-1>.
func sameBatch(n, start int) []*evidencev1.EvidenceRecord {
	out := make([]*evidencev1.EvidenceRecord, n)
	for i := range out {
		out[i] = genesisRecord(auditor1, fmt.Sprintf("s%04d", start+i), "big", "l1", "c1", epochNow, now+itemLife)
	}
	return out
}

// atCap is 1024 records of auditor-2 in one epoch, in 32 batches of 32.
func atCap() []*evidencev1.EvidenceRecord {
	var out []*evidencev1.EvidenceRecord
	for i := 0; i < int(evidence.MaxEvidencePerAuditorPerEpoch); i++ {
		out = append(out, genesisRecord(auditor2, fmt.Sprintf("cap%04d", i), fmt.Sprintf("cb%02d", i/32), "l3", "c3", epochNow, now+itemLife))
	}
	return out
}

// overCap is atCap plus one record in the same epoch.
func overCap() []*evidencev1.EvidenceRecord {
	return append(atCap(), genesisRecord(auditor2, "cap-over", "cb-over", "l3", "c3", epochNow, now+itemLife))
}

// TestGenesisBatchConsistency: the records of one batch id share their
// epoch and their signer key, each half reached alone. The signer-key half
// uses two keys whose addresses share their id prefix
// (evidencetest.PrefixCollisionKeys); the same two records in two batches
// validate, so only the batch rule refuses the mixed batch. With two label
// keys the record-level prefix rule refuses first.
func TestGenesisBatchConsistency(t *testing.T) {
	g := validGenesis()
	g.Records[1].Epoch = epochNow - 1
	g.Records[1].ExpiryUnixSeconds = now
	if err := evidence.ValidateGenesis(g); err == nil || !strings.Contains(err.Error(), "mixes") {
		t.Fatalf("mixed epochs: %v", err)
	}
	g = validGenesis()
	g.Records = append(g.Records, collisionRecords(func(i int) string { return fmt.Sprintf("own%d", i) })...)
	if err := evidence.ValidateGenesis(g); err != nil {
		t.Fatalf("two prefix-sharing keys in two batches: %v", err)
	}
	g = validGenesis()
	mixKeys(g)
	if err := evidence.ValidateGenesis(g); err == nil || !strings.Contains(err.Error(), "mixes signer keys or epochs") {
		t.Fatalf("mixed signer keys: %v", err)
	}
	g = validGenesis()
	g.Records[1].SignerPubkey = pub(auditor2)
	if err := evidence.ValidateGenesis(g); err == nil || !strings.Contains(err.Error(), "signer prefix") {
		t.Fatalf("a record of a label key without its prefix: %v", err)
	}
}

// TestEvidenceGenesisRoundTrip: records committed through the handler in
// two epochs are exported at a time when some RECORDED records are past
// their expiry and imported into fresh stores at the same time. Every
// record's reported state is equal; stored bytes are equal except exactly
// the RECORDED-to-EXPIRED change of records whose expiry <= export time;
// batch markers, lease index rows, and windows are equal; the business-id
// rows are not exported (a tested restriction, DR-07-03, DR-09-18).
func TestEvidenceGenesisRoundTrip(t *testing.T) {
	e := newEnv(t)
	rejected := e.item(auditor1, "e2", "l2")
	rejected.ObservationDigest = digestOf("other")
	short := e.item(auditor1, "e3", "l3")
	short.ExpiryUnixSeconds = now + 60
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1"), rejected, short))
	e.mustSubmit(signed(auditor2, "a2-1", e.item(auditor2, "e1", "l1")))
	later := now + evidence.EpochSeconds
	next := batch(auditor1, "2", e.item(auditor1, "e4", "l1"))
	next.Epoch = evidence.EpochAt(later)
	next.Items[0].ExpiryUnixSeconds = later + itemLife
	e.requireOK(e.submit(e.at(later), sign(next, auditor1)), "second epoch")
	exportAt := later + 10 // e3 (now+60) and e1 (now+3600) are past expiry, e4 is not
	e.ctx = e.at(exportAt)
	e.requireInvariants()
	exported, err := e.k.ExportGenesis(e.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	if exported.GetCurrentEpoch() != 0 || len(exported.GetRecords()) != 5 {
		t.Fatalf("export %v", exported)
	}
	raw, err := evidence.MarshalGenesis(exported)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := evidence.ParseGenesis(raw)
	if err != nil || !proto.Equal(parsed, exported) {
		t.Fatalf("parse %v", err)
	}
	fresh := genesisEnv(t)
	fresh.ctx = fresh.at(exportAt)
	if err := fresh.k.InitGenesis(fresh.unmetered(), parsed); err != nil {
		t.Fatal(err)
	}
	fresh.requireInvariants()
	expiredOnExport := map[string]bool{prefix(auditor1) + "e1": true, prefix(auditor1) + "e3": true, prefix(auditor2) + "e1": true}
	for _, record := range exported.GetRecords() {
		id := record.GetEvidenceId()
		old, imported := e.stored(id), fresh.stored(id)
		if evidence.ReportedState(old, exportAt) != evidence.ReportedState(imported, exportAt) {
			t.Fatalf("%s: reported state changed", id)
		}
		if expiredOnExport[id] {
			if old.GetState() != evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED || imported.GetState() != evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED {
				t.Fatalf("%s: stored %s then %s", id, old.GetState(), imported.GetState())
			}
			imported.State = old.GetState()
		}
		if !proto.Equal(old, imported) {
			t.Fatalf("%s: record changed\n old %v\n new %v", id, old, imported)
		}
	}
	rowsUnder := func(env *env, p byte) map[string]string {
		out := map[string]string{}
		it := env.store(evidence.ModuleName).Iterator([]byte{p}, []byte{p + 1})
		defer it.Close()
		for ; it.Valid(); it.Next() {
			out[string(it.Key())] = string(it.Value())
		}
		return out
	}
	for _, p := range []byte{evidence.BatchPrefix, evidence.LeaseIndexPrefix, evidence.AuditorWindowPrefix} {
		if fmt.Sprint(rowsUnder(e, p)) != fmt.Sprint(rowsUnder(fresh, p)) {
			t.Fatalf("derived rows under 0x%02x differ", p)
		}
	}
	if len(rowsUnder(e, evidence.BusinessPrefix)) != 3 || len(rowsUnder(fresh, evidence.BusinessPrefix)) != 0 {
		t.Fatal("business ids: the restriction (not exported) does not hold as stated")
	}
	// A retry of an exported batch on the imported chain is no longer
	// idempotent: the business id is forgotten and the batch id is used.
	fresh.requireReject(fresh.submit(fresh.at(exportAt), next), msgkit.DuplicateID, "retry after import")
	if err := evidencetest.CheckInvariants(fresh.unmetered(), fresh.k, fresh.leases); err != nil {
		t.Fatal(err)
	}
}

// FuzzEvidenceGenesisJSON: any document either fails to parse, fails
// ValidateGenesis, fails the cross-module checks with the store untouched,
// or imports into a store that satisfies every invariant and exports back
// to a document that imports again.
func FuzzEvidenceGenesisJSON(f *testing.F) {
	valid, err := evidence.MarshalGenesis(validGenesis())
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte(valid))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"records":[],"current_epoch":"1"}`))
	f.Add([]byte(`{"records":[{"evidence_id":"x"}]}`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, raw []byte) {
		state, err := evidence.ParseGenesis(raw)
		if err != nil {
			return
		}
		if evidence.ValidateGenesis(state) != nil {
			return
		}
		e := genesisEnv(t)
		before := e.snapshot(e.unmetered())
		if err := e.k.InitGenesis(e.unmetered(), state); err != nil {
			if !bytes.Equal(before, e.snapshot(e.unmetered())) {
				t.Fatal("a refused import wrote")
			}
			return
		}
		e.requireInvariants()
		exported, err := e.k.ExportGenesis(e.unmetered())
		if err != nil {
			t.Fatal(err)
		}
		again := genesisEnv(t)
		if err := again.k.InitGenesis(again.unmetered(), exported); err != nil {
			t.Fatalf("export does not import: %v", err)
		}
	})
}

// TestModuleGenesis: the module's genesis methods parse strictly, record
// the InitGenesis call, import, and export; an invalid document panics.
func TestModuleGenesis(t *testing.T) {
	e := genesisEnv(t)
	order := &recorder{}
	am := evidence.NewAppModule(e.k, order)
	raw, err := evidence.MarshalGenesis(validGenesis())
	if err != nil {
		t.Fatal(err)
	}
	if err := am.ValidateGenesis(nil, nil, raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"records":[{"evidence_id":"E"}]}`, `{"unknown":1}`, ``, `{"current_epoch":"3"}`} {
		if err := am.ValidateGenesis(nil, nil, []byte(bad)); err == nil {
			t.Fatalf("%q validated", bad)
		}
	}
	if err := am.ValidateGenesis(nil, nil, am.DefaultGenesis(nil)); err != nil {
		t.Fatal(err)
	}
	am.InitGenesis(e.unmetered(), nil, raw)
	if len(order.names) != 1 || order.names[0] != evidence.ModuleName {
		t.Fatalf("order %v", order.names)
	}
	exported, err := evidence.ParseGenesis(am.ExportGenesis(e.unmetered(), nil))
	if err != nil || len(exported.GetRecords()) != 3 {
		t.Fatalf("export %v %v", exported, err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("InitGenesis over a non-empty store did not panic")
		}
	}()
	am.InitGenesis(e.unmetered(), nil, raw)
}

type recorder struct{ names []string }

func (r *recorder) Record(name string) { r.names = append(r.names, name) }
