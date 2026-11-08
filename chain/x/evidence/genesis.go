package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// Module genesis. The launch genesis keeps the records empty and
// current_epoch 0 (app rule at InitChain; DR-07-02). The module itself
// validates, imports, and exports any set of records, so an exported state
// can be checked and imported into fresh stores.
//
// Export carries every record, with its REPORTED state at the export
// context's block time (a RECORDED record past its expiry is exported
// EXPIRED, deviation D-4), and current_epoch 0. It does not carry the
// business-id ledger (the v0 genesis schema has no field for it; DR-07-03,
// DR-09-18): a chain started from an export forgets those business ids.
// Batch markers, lease index rows, and auditor windows are derived state
// that InitGenesis rebuilds.

// exportJSON is the export encoding, the same options the other modules use.
var exportJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

// ParseGenesis decodes a module genesis document. Unknown fields are
// errors. An empty document is an error: the app always writes the key
// ("{}" is the empty state).
func ParseGenesis(raw json.RawMessage) (*evidencev1.GenesisState, error) {
	if len(raw) == 0 {
		return nil, errors.New("evidence genesis: empty document")
	}
	var state evidencev1.GenesisState
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("evidence genesis: %w", err)
	}
	return &state, nil
}

// MarshalGenesis is the module export encoding: protojson with proto field
// names and unpopulated fields. Compare decoded messages with proto.Equal,
// not these bytes; protojson output is not byte-stable across builds.
func MarshalGenesis(state *evidencev1.GenesisState) (json.RawMessage, error) {
	raw, err := exportJSON.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("evidence genesis: %w", err)
	}
	return raw, nil
}

// maxGenesisEpoch is the largest epoch a record can carry: its expiry is
// after the start of its epoch and at most UnixSecondsMax.
var maxGenesisEpoch = EpochAt(genesis.UnixSecondsMax)

// ValidateGenesis is a pure check of a decoded module genesis.
//
//   - current_epoch 0: the epoch is derived from the header time, not stored
//     (deviation D-3)
//   - evidence_id unique; evidence_id, batch_id, lease_id, content_id in the
//     id pattern
//   - kind AUDITOR_ATTESTATION (the only kind the message accepts, D-8)
//   - state RECORDED, EXPIRED, or REJECTED (UNSPECIFIED and unknown values
//     are invalid)
//   - signer_pubkey 32 bytes; evidence_id and batch_id start with
//     IDPrefix(AuditorAddress(signer_pubkey)) (D-17)
//   - content_version in [1, 2^53-1]
//   - observation_digest a SHA-256 digest of 32 bytes without unknown fields
//   - expiry in [1, 253402300799]; epoch at most EpochAt(253402300799); and
//     epoch*EpochSeconds < expiry < (epoch+1)*EpochSeconds +
//     MaxEvidenceLifetimeSeconds, which every recorded item satisfies (its
//     expiry is after its committed time and at most that time plus the
//     lifetime, D-9)
//   - schema_version 1; attestation true
//   - the records of one batch_id share signer_pubkey and epoch, and are at
//     most max_evidence_batch (32)
//   - at most MaxEvidencePerAuditorPerEpoch records per (auditor address,
//     epoch) (D-15)
//
// No record or digest may carry unknown fields.
func ValidateGenesis(state *evidencev1.GenesisState) error {
	if state == nil {
		return errors.New("evidence genesis: nil state")
	}
	if hasUnknown(state) {
		return errors.New("evidence genesis: unknown fields")
	}
	if state.GetCurrentEpoch() != 0 {
		return fmt.Errorf("evidence genesis: current_epoch %d, want 0 (the epoch is derived from the header time)", state.GetCurrentEpoch())
	}
	ids := make(map[string]bool, len(state.GetRecords()))
	type batchInfo struct {
		pub   []byte
		epoch uint64
		count int
	}
	batches := map[string]*batchInfo{}
	perWindow := map[string]uint64{}
	for i, record := range state.GetRecords() {
		if err := validateRecord(record); err != nil {
			return fmt.Errorf("evidence genesis: record %d: %w", i, err)
		}
		if ids[record.GetEvidenceId()] {
			return fmt.Errorf("evidence genesis: record %d: duplicate evidence_id %q", i, record.GetEvidenceId())
		}
		ids[record.GetEvidenceId()] = true
		batch, seen := batches[record.GetBatchId()]
		if !seen {
			batch = &batchInfo{pub: record.GetSignerPubkey(), epoch: record.GetEpoch()}
			batches[record.GetBatchId()] = batch
		}
		if !bytes.Equal(batch.pub, record.GetSignerPubkey()) || batch.epoch != record.GetEpoch() {
			return fmt.Errorf("evidence genesis: record %d: batch %q mixes signer keys or epochs", i, record.GetBatchId())
		}
		batch.count++
		if batch.count > int(genesis.MaxEvidenceBatch) {
			return fmt.Errorf("evidence genesis: batch %q has more than %d records", record.GetBatchId(), genesis.MaxEvidenceBatch)
		}
		window := string(EncodeWindow(record.GetEpoch(), 0)) + string(AuditorAddress(record.GetSignerPubkey()))
		perWindow[window]++
		if perWindow[window] > MaxEvidencePerAuditorPerEpoch {
			return fmt.Errorf("evidence genesis: auditor %x has more than %d records in epoch %d",
				AuditorAddress(record.GetSignerPubkey()), MaxEvidencePerAuditorPerEpoch, record.GetEpoch())
		}
	}
	return nil
}

func hasUnknown(m protoreflect.ProtoMessage) bool {
	return len(m.ProtoReflect().GetUnknown()) != 0
}

func validateRecord(r *evidencev1.EvidenceRecord) error {
	if r == nil {
		return errors.New("nil record")
	}
	if hasUnknown(r) {
		return errors.New("unknown fields")
	}
	for _, id := range []string{r.GetEvidenceId(), r.GetBatchId(), r.GetLeaseId(), r.GetContentId()} {
		if !msgkit.ValidID(id) {
			return fmt.Errorf("id %q outside the id pattern", id)
		}
	}
	if r.GetKind() != evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION {
		return fmt.Errorf("kind %s, want AUDITOR_ATTESTATION", KindName(r.GetKind()))
	}
	switch r.GetState() {
	case evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED, evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED,
		evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED:
	default:
		return fmt.Errorf("state %d is not an evidence state", int32(r.GetState()))
	}
	if len(r.GetSignerPubkey()) != msgkit.PublicKeyBytes {
		return fmt.Errorf("signer_pubkey is %d bytes, want %d", len(r.GetSignerPubkey()), msgkit.PublicKeyBytes)
	}
	signer := AuditorAddress(r.GetSignerPubkey())
	if !OwnsID(signer, r.GetEvidenceId()) || !OwnsID(signer, r.GetBatchId()) {
		return fmt.Errorf("evidence_id %q or batch_id %q does not start with the signer prefix %q", r.GetEvidenceId(), r.GetBatchId(), IDPrefix(signer))
	}
	if r.GetContentVersion() < 1 || r.GetContentVersion() > uint64(genesis.SafeIntegerMax) {
		return fmt.Errorf("content_version %d outside [1, %d]", r.GetContentVersion(), genesis.SafeIntegerMax)
	}
	if d := r.GetObservationDigest(); d != nil && hasUnknown(d) {
		return errors.New("observation_digest has unknown fields")
	}
	if _, code := msgkit.CheckDigest(r.GetObservationDigest()); code != msgkit.OK {
		return fmt.Errorf("observation_digest: %s", code)
	}
	expiry := r.GetExpiryUnixSeconds()
	if expiry < genesis.UnixSecondsMin || expiry > genesis.UnixSecondsMax {
		return fmt.Errorf("expiry_unix_seconds %d outside [%d, %d]", expiry, genesis.UnixSecondsMin, genesis.UnixSecondsMax)
	}
	if r.GetEpoch() > maxGenesisEpoch {
		return fmt.Errorf("epoch %d above %d", r.GetEpoch(), maxGenesisEpoch)
	}
	epochStart := int64(r.GetEpoch()) * EpochSeconds
	if expiry <= epochStart || expiry >= epochStart+EpochSeconds+MaxEvidenceLifetimeSeconds {
		return fmt.Errorf("expiry_unix_seconds %d outside epoch %d's window (%d, %d)", expiry, r.GetEpoch(),
			epochStart, epochStart+EpochSeconds+MaxEvidenceLifetimeSeconds)
	}
	if r.GetSchemaVersion() != SchemaVersion {
		return fmt.Errorf("schema_version %d, want %d", r.GetSchemaVersion(), SchemaVersion)
	}
	if !r.GetAttestation() {
		return errors.New("attestation is false")
	}
	return nil
}

// InitGenesis validates state, checks it against the lease registry, and
// writes it into an empty store.
//
// The store must be empty: a store holding any key (the goal-05 scaffold
// key [0x01], a layout key, or a record) is an error. Cross-module checks run
// through Deps before any write (x/leases imports its genesis first):
//
//   - the record's lease exists with the record's content_id and
//     content_version (I6)
//   - a RECORDED or EXPIRED record's observation digest equals the lease's
//     manifest digest, and a REJECTED record's differs (I5)
//   - the record's expiry is at most the lease's deadline (I13)
//   - the record's epoch is at most EpochAt(genesis block time): no record
//     comes from a later round, so an auditor window is never ahead of the
//     current epoch
//
// Writes: the layout version (msgkit.LayoutVersionKey), then the records in
// evidence id byte order with their lease index rows, the batch marker of
// every batch id, and one window row per auditor address
// (AuditorAddress(signer_pubkey)): its largest record epoch and the number
// of its records in that epoch.
func (k Keeper) InitGenesis(ctx sdk.Context, state *evidencev1.GenesisState) error {
	if err := ValidateGenesis(state); err != nil {
		return err
	}
	it := k.kv(ctx).Iterator(nil, nil)
	occupied := it.Valid()
	if err := it.Close(); err != nil {
		return fmt.Errorf("evidence genesis: close iterator: %w", err)
	}
	if occupied {
		return errors.New("evidence genesis: store is not empty")
	}
	records := append([]*evidencev1.EvidenceRecord(nil), state.GetRecords()...)
	sort.Slice(records, func(i, j int) bool { return records[i].GetEvidenceId() < records[j].GetEvidenceId() })
	current := EpochAt(ctx.BlockTime().Unix())
	for _, record := range records {
		if err := k.checkGenesisLease(ctx, record, current); err != nil {
			return err
		}
	}

	msgkit.WriteLayoutVersion(k.kv(ctx), ConsensusVersion)
	type window struct{ epoch, count uint64 }
	windows := map[string]*window{}
	for _, record := range records {
		k.addRecord(ctx, normalizeRecord(record))
		k.markBatch(ctx, record.GetBatchId())
		addr := string(AuditorAddress(record.GetSignerPubkey()))
		w, seen := windows[addr]
		switch {
		case !seen:
			windows[addr] = &window{epoch: record.GetEpoch(), count: 1}
		case record.GetEpoch() > w.epoch:
			w.epoch, w.count = record.GetEpoch(), 1
		case record.GetEpoch() == w.epoch:
			w.count++
		}
	}
	addrs := make([]string, 0, len(windows))
	for addr := range windows {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		k.saveWindow(ctx, []byte(addr), windows[addr].epoch, windows[addr].count)
	}
	return nil
}

// checkGenesisLease runs the cross-module checks of one record. It writes
// nothing.
func (k Keeper) checkGenesisLease(ctx sdk.Context, record *evidencev1.EvidenceRecord, current uint64) error {
	id := record.GetEvidenceId()
	lease, found := k.deps.Leases.Lease(ctx, record.GetLeaseId())
	if !found {
		return fmt.Errorf("evidence genesis: record %q: lease %q not found", id, record.GetLeaseId())
	}
	if lease.GetContentId() != record.GetContentId() || lease.GetContentVersion() != record.GetContentVersion() {
		return fmt.Errorf("evidence genesis: record %q: content (%q, %d) differs from lease %q (%q, %d)", id,
			record.GetContentId(), record.GetContentVersion(), lease.GetLeaseId(), lease.GetContentId(), lease.GetContentVersion())
	}
	matches := matchesManifest(record.GetObservationDigest().GetValue(), lease)
	rejected := record.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED
	if rejected && matches {
		return fmt.Errorf("evidence genesis: record %q: REJECTED with the lease's manifest digest", id)
	}
	if !rejected && !matches {
		return fmt.Errorf("evidence genesis: record %q: %s with a digest other than the lease's manifest digest", id, StateName(record.GetState()))
	}
	if record.GetExpiryUnixSeconds() > lease.GetDeadlineUnixSeconds() {
		return fmt.Errorf("evidence genesis: record %q: expiry %d after lease deadline %d", id,
			record.GetExpiryUnixSeconds(), lease.GetDeadlineUnixSeconds())
	}
	if record.GetEpoch() > current {
		return fmt.Errorf("evidence genesis: record %q: epoch %d after the current epoch %d", id, record.GetEpoch(), current)
	}
	return nil
}

// normalizeRecord is the stored form of a validated record: the digest
// rebuilt from its value and the public key copied.
func normalizeRecord(r *evidencev1.EvidenceRecord) *evidencev1.EvidenceRecord {
	return &evidencev1.EvidenceRecord{
		EvidenceId:        r.GetEvidenceId(),
		BatchId:           r.GetBatchId(),
		Kind:              r.GetKind(),
		State:             r.GetState(),
		SignerPubkey:      append([]byte(nil), r.GetSignerPubkey()...),
		LeaseId:           r.GetLeaseId(),
		ContentId:         r.GetContentId(),
		ContentVersion:    r.GetContentVersion(),
		ObservationDigest: sha256Digest(r.GetObservationDigest().GetValue()),
		Epoch:             r.GetEpoch(),
		ExpiryUnixSeconds: r.GetExpiryUnixSeconds(),
		SchemaVersion:     r.GetSchemaVersion(),
		Attestation:       r.GetAttestation(),
	}
}

// ExportGenesis reads every record in evidence id byte order, each with its
// reported state at the context's block time (ReportedState), and
// current_epoch 0. A stored record that does not decode is an error. A
// context without a block time (below 1, such as the zero header App.Export
// builds today) exports an empty store as the empty state and refuses a
// store with any record (ErrNoBlockTime): the stored state is never exported
// in place of the reported one.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*evidencev1.GenesisState, error) {
	state := &evidencev1.GenesisState{}
	now := ctx.BlockTime().Unix()
	if err := k.iterate(ctx, RecordPrefix, func(key, value []byte) error {
		record, err := decodeRecord(key, value)
		if err != nil {
			return err
		}
		report, err := reported(record, now)
		if err != nil {
			return err
		}
		state.Records = append(state.Records, report)
		return nil
	}); err != nil {
		return nil, err
	}
	return state, nil
}
