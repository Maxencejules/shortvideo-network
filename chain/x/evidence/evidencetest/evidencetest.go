// Package evidencetest is test support for x/evidence: an invariant checker
// over an evidence store, a byte snapshot of several stores, and a synthetic
// pair of signer keys whose addresses share their id prefix
// (PrefixCollisionKeys). It never runs in chain execution. Besides _test.go
// files, its one importer is the state check, chain/internal/statecheck,
// which only svnd built with the build tag svn_statecheck links (a test
// tool, not a release); the default svnd build does not link it.
//
// CheckInvariants checks the state-local invariants of
// chain/x/evidence/README.md over one context (committed state or a cached
// branch):
//
//	I1  every record has its lease index row 0x04|lease|0x00|id, and every lease index
//	    row (value 0x01) names a record of that lease
//	I2  every record's batch_id has a 0x02 marker (value 0x01), every marker has at least
//	    one record, and the records of one batch share signer key and epoch and are at
//	    most 32
//	I4  the stored state is RECORDED, EXPIRED, or REJECTED
//	I5  RECORDED or EXPIRED iff the observation digest equals the lease's manifest
//	    digest; REJECTED iff it differs
//	I6  a record's content_id and content_version equal its lease's
//	I7  attestation true, schema_version 1, signer_pubkey 32 bytes, kind
//	    AUDITOR_ATTESTATION, ids in the pattern, a SHA-256 digest of 32 bytes,
//	    content_version >= 1, expiry in [1, 253402300799]; the record decodes and
//	    names its key
//	I8  one window row per auditor with records: its epoch is the auditor's largest record
//	    epoch, its count the auditor's records in that epoch, at most the cap; no other
//	    window row
//	I12 every evidence id and batch id starts with the IDPrefix of its record's auditor
//	I13 a record's expiry is at most its lease's deadline
//
// The layout key must hold version 2, every business row must hold a
// 32-byte digest under a valid id, and no row may sit under an unknown
// prefix. I3 (records are never rewritten), I9 (no write to another
// module's store), I10, and I11 are properties of transitions and of the
// code: the tests that name them check them with snapshots.
package evidencetest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// CheckInvariants returns the first violated invariant, or nil. The error
// text starts with the invariant name (for example "I5:"). leases is the
// lease read surface (x/leases in production, a fake in unit tests). The
// store is read without gas.
func CheckInvariants(ctx sdk.Context, k evidence.Keeper, leases evidence.Leases) error {
	rows, err := readRows(ctx, k.StoreKey())
	if err != nil {
		return err
	}
	if err := checkLayout(rows); err != nil {
		return err
	}
	records, err := decodeRecords(rows)
	if err != nil {
		return err
	}
	if err := checkDerivedRows(rows, records); err != nil {
		return err
	}
	return checkLeases(ctx, records, leases)
}

// StoreSnapshot is msgkit.StoreSnapshot of each store, in argument order,
// each preceded by its store name and length (8-byte big-endian lengths).
// Two calls return equal bytes exactly when every store holds the same
// pairs. It reads through ctx's multistore without gas, so it works on a
// cached branch (ctx.CacheContext) to compare a trial state.
func StoreSnapshot(ctx sdk.Context, keys ...*storetypes.KVStoreKey) []byte {
	var out []byte
	for _, key := range keys {
		snapshot := msgkit.StoreSnapshot(ctx.MultiStore().GetKVStore(key))
		out = binary.BigEndian.AppendUint64(out, uint64(len(key.Name())))
		out = append(out, key.Name()...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(snapshot)))
		out = append(out, snapshot...)
	}
	return out
}

// readRows returns every row of the store keyed by the key bytes.
func readRows(ctx sdk.Context, key *storetypes.KVStoreKey) (map[string][]byte, error) {
	if key == nil {
		return nil, fmt.Errorf("evidencetest: nil store key")
	}
	store := ctx.MultiStore().GetKVStore(key)
	it := store.Iterator(nil, nil)
	defer it.Close()
	rows := map[string][]byte{}
	for ; it.Valid(); it.Next() {
		rows[string(it.Key())] = append([]byte(nil), it.Value()...)
	}
	return rows, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func checkLayout(rows map[string][]byte) error {
	raw, found := rows[string(msgkit.LayoutVersionKey)]
	if !found {
		return fmt.Errorf("layout: no layout version key")
	}
	if len(raw) != 8 || binary.BigEndian.Uint64(raw) != evidence.ConsensusVersion {
		return fmt.Errorf("layout: version row %x, want %d", raw, evidence.ConsensusVersion)
	}
	for _, key := range sortedKeys(rows) {
		if key == string(msgkit.LayoutVersionKey) {
			continue
		}
		switch key[0] {
		case evidence.RecordPrefix, evidence.BatchPrefix, evidence.BusinessPrefix, evidence.LeaseIndexPrefix,
			evidence.AuditorWindowPrefix:
		default:
			return fmt.Errorf("layout: row %x under an unknown prefix", key)
		}
		if key[0] == evidence.BusinessPrefix {
			if !msgkit.ValidID(key[1:]) || len(rows[key]) != msgkit.DigestBytes {
				return fmt.Errorf("layout: business row %x holds %d bytes", key, len(rows[key]))
			}
		}
	}
	return nil
}

// decodeRecords decodes every record row and checks I4, I7, and I12.
func decodeRecords(rows map[string][]byte) (map[string]*evidencev1.EvidenceRecord, error) {
	records := map[string]*evidencev1.EvidenceRecord{}
	for _, key := range sortedKeys(rows) {
		if key[0] != evidence.RecordPrefix {
			continue
		}
		var record evidencev1.EvidenceRecord
		if err := proto.Unmarshal(rows[key], &record); err != nil {
			return nil, fmt.Errorf("I7: record %x does not decode: %v", key, err)
		}
		id := key[1:]
		if record.GetEvidenceId() != id {
			return nil, fmt.Errorf("I7: record under %q names %q", id, record.GetEvidenceId())
		}
		switch record.GetState() {
		case evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED, evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED,
			evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED:
		default:
			return nil, fmt.Errorf("I4: record %q stores state %d", id, int32(record.GetState()))
		}
		if err := checkRecordFields(&record); err != nil {
			return nil, fmt.Errorf("I7: record %q: %v", id, err)
		}
		prefix := evidence.IDPrefix(evidence.AuditorAddress(record.GetSignerPubkey()))
		if !strings.HasPrefix(id, prefix) || !strings.HasPrefix(record.GetBatchId(), prefix) {
			return nil, fmt.Errorf("I12: record %q of batch %q does not carry its auditor's prefix %q", id, record.GetBatchId(), prefix)
		}
		records[id] = &record
	}
	return records, nil
}

func checkRecordFields(r *evidencev1.EvidenceRecord) error {
	for _, id := range []string{r.GetBatchId(), r.GetLeaseId(), r.GetContentId()} {
		if !msgkit.ValidID(id) {
			return fmt.Errorf("id %q outside the id pattern", id)
		}
	}
	if !r.GetAttestation() || r.GetSchemaVersion() != evidence.SchemaVersion {
		return fmt.Errorf("attestation %t schema_version %d", r.GetAttestation(), r.GetSchemaVersion())
	}
	if len(r.GetSignerPubkey()) != msgkit.PublicKeyBytes {
		return fmt.Errorf("signer_pubkey is %d bytes", len(r.GetSignerPubkey()))
	}
	if r.GetKind() != evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION {
		return fmt.Errorf("kind %s", evidence.KindName(r.GetKind()))
	}
	if _, code := msgkit.CheckDigest(r.GetObservationDigest()); code != msgkit.OK {
		return fmt.Errorf("observation digest %s", code)
	}
	if r.GetContentVersion() < 1 {
		return fmt.Errorf("content_version 0")
	}
	if t := r.GetExpiryUnixSeconds(); t < genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {
		return fmt.Errorf("expiry %d out of range", t)
	}
	return nil
}

// checkDerivedRows checks I1, I2, and I8.
func checkDerivedRows(rows map[string][]byte, records map[string]*evidencev1.EvidenceRecord) error {
	type batchInfo struct {
		pub   []byte
		epoch uint64
		count int
	}
	batches := map[string]*batchInfo{}
	type window struct{ epoch, count uint64 }
	windows := map[string]*window{}
	for _, id := range sortedKeys(records) {
		record := records[id]
		if !bytes.Equal(rows[string(evidence.LeaseIndexKey(record.GetLeaseId(), id))], evidence.Marker) {
			return fmt.Errorf("I1: record %q has no lease index row", id)
		}
		batch, seen := batches[record.GetBatchId()]
		if !seen {
			batch = &batchInfo{pub: record.GetSignerPubkey(), epoch: record.GetEpoch()}
			batches[record.GetBatchId()] = batch
		}
		if !bytes.Equal(batch.pub, record.GetSignerPubkey()) || batch.epoch != record.GetEpoch() {
			return fmt.Errorf("I2: batch %q mixes signer keys or epochs", record.GetBatchId())
		}
		batch.count++
		if batch.count > int(genesis.MaxEvidenceBatch) {
			return fmt.Errorf("I2: batch %q has more than %d records", record.GetBatchId(), genesis.MaxEvidenceBatch)
		}
		addr := string(evidence.AuditorAddress(record.GetSignerPubkey()))
		w, ok := windows[addr]
		switch {
		case !ok:
			windows[addr] = &window{epoch: record.GetEpoch(), count: 1}
		case record.GetEpoch() > w.epoch:
			w.epoch, w.count = record.GetEpoch(), 1
		case record.GetEpoch() == w.epoch:
			w.count++
		}
	}
	for _, key := range sortedKeys(rows) {
		switch key[0] {
		case evidence.LeaseIndexPrefix:
			if !bytes.Equal(rows[key], evidence.Marker) {
				return fmt.Errorf("I1: lease index row %x holds %x", key, rows[key])
			}
			sep := strings.IndexByte(key[1:], 0x00)
			if sep < 0 {
				return fmt.Errorf("I1: lease index row %x has no separator", key)
			}
			leaseID, id := key[1:1+sep], key[2+sep:]
			record, found := records[id]
			if !found || record.GetLeaseId() != leaseID {
				return fmt.Errorf("I1: lease index row %x names no record of lease %q", key, leaseID)
			}
		case evidence.BatchPrefix:
			if !bytes.Equal(rows[key], evidence.Marker) {
				return fmt.Errorf("I2: batch marker %x holds %x", key, rows[key])
			}
			if _, found := batches[key[1:]]; !found {
				return fmt.Errorf("I2: batch marker %q has no record", key[1:])
			}
		case evidence.AuditorWindowPrefix:
			epoch, count, err := evidence.DecodeWindow(rows[key])
			if err != nil {
				return fmt.Errorf("I8: window row %x: %v", key, err)
			}
			want, found := windows[key[1:]]
			if !found {
				return fmt.Errorf("I8: window row %x for an auditor without records", key)
			}
			if epoch != want.epoch || count != want.count || count > evidence.MaxEvidencePerAuditorPerEpoch {
				return fmt.Errorf("I8: window row %x holds (%d, %d), records give (%d, %d)", key, epoch, count, want.epoch, want.count)
			}
		}
	}
	for _, batchID := range sortedKeys(batches) {
		if _, found := rows[string(evidence.BatchKey(batchID))]; !found {
			return fmt.Errorf("I2: batch %q has no marker", batchID)
		}
	}
	for _, addr := range sortedKeys(windows) {
		if _, found := rows[string(evidence.AuditorWindowKey([]byte(addr)))]; !found {
			return fmt.Errorf("I8: auditor %x has records and no window row", addr)
		}
	}
	return nil
}

// checkLeases checks I5, I6, and I13 against the lease read surface.
func checkLeases(ctx sdk.Context, records map[string]*evidencev1.EvidenceRecord, leases evidence.Leases) error {
	if leases == nil {
		return fmt.Errorf("evidencetest: nil lease surface")
	}
	for _, id := range sortedKeys(records) {
		record := records[id]
		lease, found := leases.Lease(ctx, record.GetLeaseId())
		if !found {
			return fmt.Errorf("I6: record %q names absent lease %q", id, record.GetLeaseId())
		}
		if lease.GetContentId() != record.GetContentId() || lease.GetContentVersion() != record.GetContentVersion() {
			return fmt.Errorf("I6: record %q content (%q, %d), lease (%q, %d)", id, record.GetContentId(),
				record.GetContentVersion(), lease.GetContentId(), lease.GetContentVersion())
		}
		manifest := lease.GetManifestDigest()
		matches := manifest.GetAlgorithm() == record.GetObservationDigest().GetAlgorithm() &&
			bytes.Equal(manifest.GetValue(), record.GetObservationDigest().GetValue())
		if (record.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED) == matches {
			return fmt.Errorf("I5: record %q is %s and its digest equal to the manifest is %t", id, record.GetState(), matches)
		}
		if record.GetExpiryUnixSeconds() > lease.GetDeadlineUnixSeconds() {
			return fmt.Errorf("I13: record %q expiry %d after lease deadline %d", id, record.GetExpiryUnixSeconds(), lease.GetDeadlineUnixSeconds())
		}
	}
	return nil
}
