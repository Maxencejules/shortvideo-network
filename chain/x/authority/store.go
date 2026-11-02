package authority

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Authority store layout. One prefix byte, then the fields that name the
// object. A message reads and writes a fixed set of keys: the config key
// (0x01), the keys its own fields name, and the policy version (0x04) for a
// propose or an execute. A propose reads and writes its seat's open count
// (0x14) at the current version; a keep from OPEN or QUEUED, or a successful
// execute, reads and writes the proposer's open count at the record's base
// version. A validator-payload execute also scans the pending prefix (0x03),
// at most MaxPendingChanges entries; an execute of a registry payload reads no
// authority key beyond the common set (its hook reads and writes a fixed key
// set in x/providers or x/content). So gas is bounded and does not grow with
// past proposals; a validator execute depends on the pending count.
//
// No live chain exists, so there is no migration from the round-2 layout
// (one side document at 0x20). A store that still holds 0x20 is an old devnet
// home: EndBlock halts on it and export refuses it.
const (
	// configKey holds the deterministic proto GenesisState: validators,
	// submitters, devnet test seats, quorum loss UNSET, blocked decisions and
	// devnet test parameters. Proposals, pending entries and the approved
	// council are always empty here.
	configKey byte = 0x01
	// admitPrefix || addr(20) -> one byte, the submitter role. The ante reads it.
	admitPrefix byte = 0x02
	// pendingPrefix || kind(1) || point(8, big-endian) || proposal_id ->
	// deterministic proto PendingValidatorPower. Kind 0x01 is a height and
	// 0x02 a unix time, so key order is the spec activation_sort_key order.
	pendingPrefix byte = 0x03
	// policyKey -> 8-byte big-endian policy version. Absent means 1.
	policyKey byte = 0x04
	// proposalPrefix || proposal_id -> proposalRecord JSON.
	proposalPrefix byte = 0x10
	// businessPrefix || business_id -> 32-byte business digest.
	businessPrefix byte = 0x11
	// executedPrefix || parameter_key -> 32-byte payload digest.
	executedPrefix byte = 0x12
	// governedPrefix || limit_key -> 8-byte big-endian value.
	governedPrefix byte = 0x13
	// openPrefix || base_policy_version(8, big-endian) || seat_id -> 8-byte
	// big-endian count of that seat's OPEN or QUEUED proposals opened on that
	// base version. The key is deleted when the count reaches 0. Propose reads
	// only the current version's key, so a proposal on an older base, which
	// can never execute, stops counting when an execute bumps the version.
	openPrefix byte = 0x14
	// retiredCouncilPrefix || old_pub(32) -> seat id. Engine-only rotation.
	retiredCouncilPrefix byte = 0x15
	// retiredServicePrefix || old_addr(20) -> 0x01. Engine-only rotation.
	retiredServicePrefix byte = 0x16
	// providerPrefix is reserved: round 3 provider rows. No code writes it
	// since Goal 07; provider records are x/providers state. ExportState
	// still refuses it.
	providerPrefix byte = 0x17
	// legacySideKey is the round-2 side document. It is never written.
	legacySideKey byte = 0x20
)

// Activation kinds inside a pending key.
const (
	pendingKindHeight byte = 0x01
	pendingKindTime   byte = 0x02
)

// KV is the store view the engine reads and writes. storetypes.KVStore
// satisfies it. Every iterator the engine opens is closed before it writes.
type KV interface {
	Get(key []byte) []byte
	Has(key []byte) bool
	Set(key, value []byte)
	Delete(key []byte)
	Iterator(start, end []byte) storetypes.Iterator
}

var (
	errMissing = errors.New("authority state is missing")
	// errLegacySide is returned by EndBlock for a round-2 devnet home.
	errLegacySide = errors.New("authority store has the round-2 side document (0x20); re-initialize this devnet home; there is no migration")
)

// AdmissionKey is the ante admission row of one 20-byte account.
func AdmissionKey(addr []byte) []byte {
	return append([]byte{admitPrefix}, addr...)
}

// PendingKey is the store key of one scheduled validator change. It returns
// nil when the entry has no height or unix activation point; genesis
// validation and the engine reject such an entry before any write.
func PendingKey(entry *authorityv1.PendingValidatorPower) []byte {
	kind, point, ok := activationSlot(entry.GetActivation())
	if !ok {
		return nil
	}
	key := make([]byte, 0, 10+len(entry.GetProposalId()))
	key = append(key, pendingPrefix, kind)
	key = binary.BigEndian.AppendUint64(key, point)
	return append(key, entry.GetProposalId()...)
}

func activationSlot(point *authorityv1.ActivationPoint) (byte, uint64, bool) {
	switch body := point.GetPoint().(type) {
	case *authorityv1.ActivationPoint_Height:
		return pendingKindHeight, body.Height, true
	case *authorityv1.ActivationPoint_UnixSeconds:
		if body.UnixSeconds < 0 {
			return 0, 0, false
		}
		return pendingKindTime, uint64(body.UnixSeconds), true
	default:
		return 0, 0, false
	}
}

func prefixed(prefix byte, id string) []byte {
	return append([]byte{prefix}, id...)
}

func proposalKey(id string) []byte    { return prefixed(proposalPrefix, id) }
func businessKey(id string) []byte    { return prefixed(businessPrefix, id) }
func executedKey(param string) []byte { return prefixed(executedPrefix, param) }
func governedKey(limit string) []byte { return prefixed(governedPrefix, limit) }

// openKey is the open count of one seat for proposals on one base version.
func openKey(seatID string, version uint64) []byte {
	key := make([]byte, 0, 9+len(seatID))
	key = append(key, openPrefix)
	key = binary.BigEndian.AppendUint64(key, version)
	return append(key, seatID...)
}

func retiredCouncilKey(pub []byte) []byte {
	return append([]byte{retiredCouncilPrefix}, pub...)
}

func retiredServiceKey(addr []byte) []byte {
	return append([]byte{retiredServicePrefix}, addr...)
}

// prefixEnd is the exclusive end of a one-byte prefix range.
func prefixEnd(prefix byte) []byte {
	return []byte{prefix + 1}
}

func encodeU64(value uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, value)
}

func decodeU64(raw []byte) (uint64, bool) {
	if len(raw) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(raw), true
}

// marshalState is the only consensus encoding for the config key and the
// pending entries. protojson is not stored. Its output changes across builds.
func marshalState(msg proto.Message) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(msg)
}

// proposalRecord is one proposal and its decoded payload. JSON with a fixed
// field order, no maps and no omitempty. Slices are never nil when stored,
// so a record has one encoding.
type proposalRecord struct {
	ProposalID              string      `json:"proposal_id"`
	ParameterKey            string      `json:"parameter_key"`
	BasePolicyVersion       uint64      `json:"base_policy_version"`
	PayloadDigest           []byte      `json:"payload_digest"`
	State                   int32       `json:"state"`
	ExpiryUnixSeconds       int64       `json:"expiry_unix_seconds"`
	ExecuteAfterUnixSeconds int64       `json:"execute_after_unix_seconds"`
	ApproverKeys            [][]byte    `json:"approver_keys"`
	BusinessID              string      `json:"business_id"`
	Proposer                string      `json:"proposer"`
	Approvers               []string    `json:"approvers"`
	Payload                 string      `json:"payload"`
	Decoded                 decodedWire `json:"decoded"`
}

func (r *proposalRecord) state() authorityv1.ProposalState {
	return authorityv1.ProposalState(r.State)
}

func (r *proposalRecord) open() bool {
	return r.state() == authorityv1.ProposalState_PROPOSAL_STATE_OPEN ||
		r.state() == authorityv1.ProposalState_PROPOSAL_STATE_QUEUED
}

// decodedWire is the payload decoded at propose time. Execute uses it and
// does not parse the canonical JSON again. A force_withdraw record keeps its
// content id, version and the 32-byte reason digest value (in reason).
type decodedWire struct {
	Kind           string `json:"kind"`
	NodeID         string `json:"node_id"`
	PubKey         []byte `json:"pub_key"`
	Power          int64  `json:"power"`
	Reason         []byte `json:"reason"`
	ByHeight       bool   `json:"by_height"`
	Height         uint64 `json:"height"`
	Unix           int64  `json:"unix"`
	LimitKey       string `json:"limit_key"`
	LimitValue     uint64 `json:"limit_value"`
	ProviderID     string `json:"provider_id"`
	ContentID      string `json:"content_id"`
	ContentVersion uint64 `json:"content_version"`
}

func (d decodedWire) power() PowerChange {
	return PowerChange{
		NodeID: d.NodeID, PubKey: append([]byte(nil), d.PubKey...), Power: d.Power,
		Reason: append([]byte(nil), d.Reason...), ByHeight: d.ByHeight, Height: d.Height, Unix: d.Unix,
	}
}

func normalizeRecord(rec *proposalRecord) {
	if rec.ApproverKeys == nil {
		rec.ApproverKeys = [][]byte{}
	}
	if rec.Approvers == nil {
		rec.Approvers = []string{}
	}
	if rec.PayloadDigest == nil {
		rec.PayloadDigest = []byte{}
	}
	if rec.Decoded.PubKey == nil {
		rec.Decoded.PubKey = []byte{}
	}
	if rec.Decoded.Reason == nil {
		rec.Decoded.Reason = []byte{}
	}
}

func encodeRecord(rec *proposalRecord) ([]byte, error) {
	normalizeRecord(rec)
	return json.Marshal(rec)
}

// decodeStrict decodes one JSON value and requires that encoding it again
// gives the stored bytes. A value written by another encoder is integrity
// damage.
func decodeStrict(raw []byte, out any, encode func() ([]byte, error)) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data")
	}
	again, err := encode()
	if err != nil {
		return err
	}
	if !bytes.Equal(again, raw) {
		return fmt.Errorf("not the canonical encoding")
	}
	return nil
}

func decodeRecord(id string, raw []byte) (*proposalRecord, error) {
	var rec proposalRecord
	if err := decodeStrict(raw, &rec, func() ([]byte, error) { return encodeRecord(&rec) }); err != nil {
		return nil, fmt.Errorf("proposal %s: %w", id, err)
	}
	if rec.ProposalID != id || len(rec.PayloadDigest) != 32 {
		return nil, fmt.Errorf("proposal %s: record does not match its key", id)
	}
	if _, known := authorityv1.ProposalState_name[rec.State]; !known || rec.State == 0 {
		return nil, fmt.Errorf("proposal %s: state %d", id, rec.State)
	}
	return &rec, nil
}

func decodePending(key, raw []byte) (*authorityv1.PendingValidatorPower, error) {
	var entry authorityv1.PendingValidatorPower
	if err := proto.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("pending entry: %w", err)
	}
	if !bytes.Equal(PendingKey(&entry), key) {
		return nil, fmt.Errorf("pending entry %s does not match its key", entry.GetProposalId())
	}
	return &entry, nil
}

// readPending returns every scheduled change in key order. More than
// MaxPendingChanges entries is integrity damage: Execute and genesis
// validation never store more, so the read stays bounded.
func readPending(kv KV) ([]*authorityv1.PendingValidatorPower, error) {
	iter := kv.Iterator([]byte{pendingPrefix}, prefixEnd(pendingPrefix))
	defer iter.Close()
	var out []*authorityv1.PendingValidatorPower
	for ; iter.Valid(); iter.Next() {
		if len(out) == MaxPendingChanges {
			return nil, fmt.Errorf("more than %d pending validator changes", MaxPendingChanges)
		}
		entry, err := decodePending(iter.Key(), iter.Value())
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func hasPending(kv KV) bool {
	iter := kv.Iterator([]byte{pendingPrefix}, prefixEnd(pendingPrefix))
	defer iter.Close()
	return iter.Valid()
}

func readConfig(kv KV) (*authorityv1.GenesisState, error) {
	raw := kv.Get([]byte{configKey})
	if raw == nil {
		return nil, errMissing
	}
	return decodeConfig(raw)
}

func decodeConfig(raw []byte) (*authorityv1.GenesisState, error) {
	var cfg authorityv1.GenesisState
	if err := proto.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if cfg.GetAuthority() == nil {
		return nil, fmt.Errorf("config has no authority state")
	}
	auth := cfg.GetAuthority()
	if len(auth.GetProposals()) != 0 || len(auth.GetPendingValidatorPower()) != 0 || len(auth.GetApprovedCouncil()) != 0 {
		return nil, fmt.Errorf("config holds proposals, pending entries or an approved council (old layout)")
	}
	return &cfg, nil
}

// configFrom is the config-key form of a genesis state: the same message
// with the proposals, the pending entries and the approved council cleared.
// For a genesis that has none of them it encodes to the same bytes.
func configFrom(state *authorityv1.GenesisState) *authorityv1.GenesisState {
	cfg := proto.Clone(state).(*authorityv1.GenesisState)
	cfg.Authority.Proposals = nil
	cfg.Authority.PendingValidatorPower = nil
	cfg.Authority.ApprovedCouncil = nil
	return cfg
}

// InitStore writes a genesis state into an empty authority store: the config
// key, one admission row per submitter, and one key per pending entry. It
// validates the state first (without a chain start point; the app InitChainer
// checks the start point before this runs).
func InitStore(kv KV, state *authorityv1.GenesisState) error {
	if err := ValidateGenesisState(state, nil); err != nil {
		return err
	}
	iter := kv.Iterator(nil, nil)
	used := iter.Valid()
	iter.Close()
	if used {
		return fmt.Errorf("authority store is not empty")
	}
	type write struct{ key, value []byte }
	var writes []write
	raw, err := marshalState(configFrom(state))
	if err != nil {
		return err
	}
	writes = append(writes, write{[]byte{configKey}, raw})
	for _, row := range state.GetAuthority().GetSubmitters() {
		writes = append(writes, write{AdmissionKey(row.GetAddress()), []byte{byte(row.GetRole())}})
	}
	for _, entry := range state.GetAuthority().GetPendingValidatorPower() {
		value, err := marshalState(entry)
		if err != nil {
			return err
		}
		writes = append(writes, write{PendingKey(entry), value})
	}
	for _, w := range writes {
		kv.Set(w.key, w.value)
	}
	return nil
}

// ErrExportRefused is wrapped by ExportState when the store holds state the
// frozen authority genesis proto has no field for (DR-06-01).
var ErrExportRefused = errors.New("authority export refused")

func prefixName(prefix byte) string {
	switch prefix {
	case policyKey:
		return "policy version"
	case proposalPrefix:
		return "proposal records"
	case businessPrefix:
		return "business ids"
	case executedPrefix:
		return "executed parameter digests"
	case governedPrefix:
		return "governed limits"
	case openPrefix:
		return "open-proposal counts"
	case retiredCouncilPrefix:
		return "retired council keys"
	case retiredServicePrefix:
		return "retired service addresses"
	case providerPrefix:
		return "provider rows"
	case legacySideKey:
		return "round-2 side document"
	default:
		return "unknown key"
	}
}

// ExportState returns the genesis form of the store: the config key plus the
// pending entries in key order. It refuses, with ErrExportRefused naming the
// first such prefix, when the store holds anything else: the policy version,
// proposal records and payloads, business ids, the executed set, governed
// limits, open counts, retired keys, a key under the reserved provider-row
// prefix (0x17) or the round-2 side document. The genesis proto cannot carry them, and dropping them would
// lose replay protection and council state on import.
func ExportState(kv KV) (*authorityv1.GenesisState, error) {
	var cfg *authorityv1.GenesisState
	var pending []*authorityv1.PendingValidatorPower
	admitted := map[string]byte{}
	iter := kv.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		switch {
		case len(key) == 1 && key[0] == configKey:
			var err error
			if cfg, err = decodeConfig(iter.Value()); err != nil {
				return nil, err
			}
		case key[0] == admitPrefix && len(key) == 21 && len(iter.Value()) == 1:
			admitted[string(key[1:])] = iter.Value()[0]
		case key[0] == pendingPrefix:
			entry, err := decodePending(key, iter.Value())
			if err != nil {
				return nil, err
			}
			pending = append(pending, entry)
		default:
			return nil, fmt.Errorf("%w: the store holds %s (prefix 0x%02x), which the authority genesis proto cannot carry", ErrExportRefused, prefixName(key[0]), key[0])
		}
	}
	if cfg == nil {
		return nil, errMissing
	}
	submitters := cfg.GetAuthority().GetSubmitters()
	if len(submitters) != len(admitted) {
		return nil, fmt.Errorf("authority export: %d admission rows for %d submitters", len(admitted), len(submitters))
	}
	for _, row := range submitters {
		role, ok := admitted[string(row.GetAddress())]
		if !ok || role != byte(row.GetRole()) {
			return nil, fmt.Errorf("authority export: admission row does not match submitter %x", row.GetAddress())
		}
	}
	if len(pending) > MaxPendingChanges {
		return nil, fmt.Errorf("authority export: more than %d pending validator changes", MaxPendingChanges)
	}
	cfg.Authority.PendingValidatorPower = pending
	return cfg, nil
}

func roleKnown(role typesv1.SubmitterRole) bool {
	_, known := typesv1.SubmitterRole_name[int32(role)]
	return known && role != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
}
