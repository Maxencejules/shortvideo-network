package evidence

import (
	"encoding/binary"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Store layout. Record values are deterministic protobuf, never protojson.
//
//	0x00                                 -> layout version 2, 8 bytes BE (msgkit.LayoutVersionKey)
//	0x01 | evidence_id                   -> svn.evidence.v1.EvidenceRecord (never rewritten or deleted)
//	0x02 | batch_id                      -> 0x01 (batch id used; never deleted)
//	0x03 | business_id                   -> 32-byte business digest (msgkit.Ledger)
//	0x04 | lease_id 0x00 evidence_id     -> 0x01 (records by lease; never deleted)
//	0x05 | auditor address (20 bytes)    -> epoch (8 bytes BE) || count (8 bytes BE): the auditor's
//	                                        open window (deviation D-15); overwritten by its next
//	                                        batch, never deleted
//
// Ids match ^[a-z0-9][a-z0-9._-]{0,63}$, so an id never contains 0x00 and
// key byte order is id byte order; the 0x00 after a lease id stops "l1"
// from matching "l10". Every value is non-empty: the goleveldb backend
// reports a key with an empty value as absent (msgkit.LayoutVersionKey).
//
// A record has a constant size: four ids of at most 64 bytes, a 32-byte
// digest, a 32-byte public key, and fixed scalars. Nothing grows inside a
// value. Batch markers, lease index rows, and auditor windows are derived
// state: InitGenesis rebuilds them from the records and ExportGenesis does
// not export them. No expiry index exists: expiry is computed at read time
// (deviation D-4).
const (
	RecordPrefix        byte = 0x01
	BatchPrefix         byte = 0x02
	BusinessPrefix      byte = 0x03
	LeaseIndexPrefix    byte = 0x04
	AuditorWindowPrefix byte = 0x05
)

// Marker is the value of every row that only needs to exist. It is for
// comparisons; do not modify it. Writes store a fresh copy (marker).
var Marker = []byte{0x01}

// marker is a fresh copy of Marker for one write.
func marker() []byte { return []byte{0x01} }

// windowValueLen is the length of an auditor window value.
const windowValueLen = 16

func idKey(prefix byte, id string) []byte {
	out := make([]byte, 0, 1+len(id))
	out = append(out, prefix)
	return append(out, id...)
}

// RecordKey is the store key of one evidence record.
func RecordKey(id string) []byte { return idKey(RecordPrefix, id) }

// BatchKey is the used-batch marker of one batch id.
func BatchKey(id string) []byte { return idKey(BatchPrefix, id) }

// leaseScope is LeaseIndexPrefix | lease_id | 0x00: every index row of one
// lease.
func leaseScope(leaseID string) []byte {
	out := make([]byte, 0, 2+len(leaseID))
	out = append(out, LeaseIndexPrefix)
	out = append(out, leaseID...)
	return append(out, 0x00)
}

// LeaseIndexKey is the index row of one record under its lease.
func LeaseIndexKey(leaseID, evidenceID string) []byte {
	return append(leaseScope(leaseID), evidenceID...)
}

// AuditorWindowKey is the window row of one 20-byte auditor address.
func AuditorWindowKey(addr []byte) []byte {
	out := make([]byte, 0, 1+len(addr))
	out = append(out, AuditorWindowPrefix)
	return append(out, addr...)
}

// EncodeWindow is the value of an auditor window row.
func EncodeWindow(epoch, count uint64) []byte {
	out := binary.BigEndian.AppendUint64(make([]byte, 0, windowValueLen), epoch)
	return binary.BigEndian.AppendUint64(out, count)
}

// DecodeWindow reads an auditor window value. A value that is not 16 bytes,
// or a count of 0 (a window row is written only by a batch of at least one
// item), is an error.
func DecodeWindow(raw []byte) (epoch, count uint64, err error) {
	if len(raw) != windowValueLen {
		return 0, 0, fmt.Errorf("evidence: window value is %d bytes, want %d", len(raw), windowValueLen)
	}
	epoch, count = binary.BigEndian.Uint64(raw[:8]), binary.BigEndian.Uint64(raw[8:])
	if count == 0 {
		return 0, 0, fmt.Errorf("evidence: window value stores count 0")
	}
	return epoch, count, nil
}

var deterministic = proto.MarshalOptions{Deterministic: true}

func (k Keeper) kv(ctx sdk.Context) storetypes.KVStore { return ctx.KVStore(k.key) }

func (k Keeper) has(ctx sdk.Context, key []byte) bool { return k.kv(ctx).Has(key) }

// loadRecord reads one record. An id outside the id pattern cannot have a
// record, so it returns not found without a store read. A stored value that
// does not decode, or whose id does not match its key, is an error.
func (k Keeper) loadRecord(ctx sdk.Context, id string) (*evidencev1.EvidenceRecord, bool, error) {
	if !msgkit.ValidID(id) {
		return nil, false, nil
	}
	raw := k.kv(ctx).Get(RecordKey(id))
	if raw == nil {
		return nil, false, nil
	}
	record, err := decodeRecord(RecordKey(id), raw)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

// mustLoadRecord is loadRecord for message and handoff paths. The module
// writes only records it validated, so a record that does not decode is an
// invariant violation. It panics; the SDK turns a handler panic into a
// failed transaction on every node alike, and nothing of that transaction is
// written.
func (k Keeper) mustLoadRecord(ctx sdk.Context, id string) (*evidencev1.EvidenceRecord, bool) {
	record, found, err := k.loadRecord(ctx, id)
	if err != nil {
		panic(err.Error())
	}
	return record, found
}

func decodeRecord(key, raw []byte) (*evidencev1.EvidenceRecord, error) {
	var record evidencev1.EvidenceRecord
	if err := proto.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("evidence: stored record %q does not decode: %w", key[1:], err)
	}
	if record.GetEvidenceId() != string(key[1:]) {
		return nil, fmt.Errorf("evidence: stored record under %q names %q", key[1:], record.GetEvidenceId())
	}
	return &record, nil
}

// marshal encodes a validated state message. Marshaling such a message
// cannot fail; a failure is an invariant violation and panics.
func marshal(m proto.Message, what string) []byte {
	raw, err := deterministic.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("evidence: marshal %s: %v", what, err))
	}
	return raw
}

// addRecord writes one record and its lease index row. The caller writes
// the batch marker once per batch.
func (k Keeper) addRecord(ctx sdk.Context, record *evidencev1.EvidenceRecord) {
	store := k.kv(ctx)
	store.Set(RecordKey(record.GetEvidenceId()), marshal(record, "record "+record.GetEvidenceId()))
	store.Set(LeaseIndexKey(record.GetLeaseId(), record.GetEvidenceId()), marker())
}

// markBatch writes the used-batch marker of one batch id.
func (k Keeper) markBatch(ctx sdk.Context, batchID string) {
	k.kv(ctx).Set(BatchKey(batchID), marker())
}

// mustLoadWindow reads the window row of one auditor: the epoch of its last
// batch and the items it recorded in that epoch. An absent row is (0, 0). A
// value that does not decode is an invariant violation and panics.
func (k Keeper) mustLoadWindow(ctx sdk.Context, addr []byte) (epoch, count uint64) {
	raw := k.kv(ctx).Get(AuditorWindowKey(addr))
	if raw == nil {
		return 0, 0
	}
	epoch, count, err := DecodeWindow(raw)
	if err != nil {
		panic(err.Error())
	}
	return epoch, count
}

func (k Keeper) saveWindow(ctx sdk.Context, addr []byte, epoch, count uint64) {
	k.kv(ctx).Set(AuditorWindowKey(addr), EncodeWindow(epoch, count))
}

// iterate visits every row under one prefix in key byte order. The first
// error stops the walk. visit must not keep the slices.
func (k Keeper) iterate(ctx sdk.Context, prefix byte, visit func(key, value []byte) error) (err error) {
	start := []byte{prefix}
	it := k.kv(ctx).Iterator(start, storetypes.PrefixEndBytes(start))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("evidence: close iterator: %w", closeErr)
		}
	}()
	for ; it.Valid(); it.Next() {
		if visitErr := visit(it.Key(), it.Value()); visitErr != nil {
			return visitErr
		}
	}
	// Iterator.Error is not consulted: a cachekv merge iterator reports an
	// exhausted range as an error ("invalid cacheMergeIterator").
	return nil
}

// sha256Digest is a stored SHA-256 digest with a copy of value.
func sha256Digest(value []byte) *typesv1.Digest {
	return &typesv1.Digest{
		Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
		Value:     append([]byte(nil), value...),
	}
}
