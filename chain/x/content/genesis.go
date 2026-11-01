package content

import (
	"encoding/json"
	"fmt"
	"sort"

	"google.golang.org/protobuf/encoding/protojson"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// Module genesis.
//
// The launch rule is unchanged: the application's InitChain keeps the
// content catalogue empty (goal 05, REQ-GEN-002, and the proto comment
// "Genesis content is empty"). This module still validates, imports, and
// exports a non-empty catalogue, so committed state round-trips through
// export and import. Business ids are not part of the v0 genesis schema
// and are not exported (DR-07-03).

var genesisJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

// ParseGenesis decodes module genesis JSON strictly: unknown fields and
// malformed values are errors.
func ParseGenesis(raw json.RawMessage) (*contentv1.GenesisState, error) {
	state := &contentv1.GenesisState{}
	if err := (protojson.UnmarshalOptions{}).Unmarshal(raw, state); err != nil {
		return nil, fmt.Errorf("content genesis: %w", err)
	}
	return state, nil
}

// MarshalGenesis encodes module genesis JSON with proto field names and
// every field present.
func MarshalGenesis(state *contentv1.GenesisState) (json.RawMessage, error) {
	raw, err := genesisJSON.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("content genesis: %w", err)
	}
	return raw, nil
}

// ValidateGenesis checks a decoded catalogue. It is pure.
//
// Content rows: content_id in the id pattern and unique; content_version
// 1; manifest_digest SHA-256 with 32 bytes; package_bytes in
// 1..268435456 (the protocol cap; a governed lower limit applies to new
// registrations only); submitting_host 20 bytes; state PENDING or
// WITHDRAWN.
//
// Tombstones: exactly one per WITHDRAWN row, matching its id and version,
// and none for any other row; committed_unix_seconds in
// 1..253402300799; reason_digest SHA-256 with 32 bytes; history_erased
// false; retention_decision_id DEC-004; retention_value_version 0.
func ValidateGenesis(state *contentv1.GenesisState) error {
	if state == nil {
		return fmt.Errorf("content genesis: nil state")
	}
	withdrawn := map[string]uint64{}
	seen := map[string]bool{}
	for i, row := range state.GetContents() {
		if err := validateContentRow(row); err != nil {
			return fmt.Errorf("content genesis: contents[%d]: %w", i, err)
		}
		if seen[row.GetContentId()] {
			return fmt.Errorf("content genesis: contents[%d]: content_id %q repeated", i, row.GetContentId())
		}
		seen[row.GetContentId()] = true
		if row.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
			withdrawn[row.GetContentId()] = row.GetContentVersion()
		}
	}
	tombstoned := map[string]bool{}
	for i, tomb := range state.GetTombstones() {
		if err := validateTombstone(tomb); err != nil {
			return fmt.Errorf("content genesis: tombstones[%d]: %w", i, err)
		}
		version, ok := withdrawn[tomb.GetContentId()]
		if !ok || version != tomb.GetContentVersion() {
			return fmt.Errorf("content genesis: tombstones[%d]: no WITHDRAWN content %q version %d", i, tomb.GetContentId(), tomb.GetContentVersion())
		}
		if tombstoned[tomb.GetContentId()] {
			return fmt.Errorf("content genesis: tombstones[%d]: content %q has a second tombstone", i, tomb.GetContentId())
		}
		tombstoned[tomb.GetContentId()] = true
	}
	ids := make([]string, 0, len(withdrawn))
	for id := range withdrawn {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !tombstoned[id] {
			return fmt.Errorf("content genesis: WITHDRAWN content %q has no tombstone", id)
		}
	}
	return nil
}

func validateContentRow(row *contentv1.Content) error {
	if row == nil {
		return fmt.Errorf("nil row")
	}
	if code := msgkit.CheckID(row.GetContentId()); code != msgkit.OK {
		return fmt.Errorf("content_id %q: %s", row.GetContentId(), code)
	}
	if row.GetContentVersion() != v0ContentVersion {
		return fmt.Errorf("content_version %d: v0 records use version 1", row.GetContentVersion())
	}
	if _, code := msgkit.CheckDigest(row.GetManifestDigest()); code != msgkit.OK {
		return fmt.Errorf("manifest_digest: %s", code)
	}
	if row.GetPackageBytes() < 1 || row.GetPackageBytes() > genesis.MaxPackageBytes {
		return fmt.Errorf("package_bytes %d outside 1..%d", row.GetPackageBytes(), genesis.MaxPackageBytes)
	}
	if len(row.GetSubmittingHost()) != msgkit.AccountBytes {
		return fmt.Errorf("submitting_host is %d bytes, want %d", len(row.GetSubmittingHost()), msgkit.AccountBytes)
	}
	switch row.GetState() {
	case contentv1.ContentState_CONTENT_STATE_PENDING, contentv1.ContentState_CONTENT_STATE_WITHDRAWN:
	default:
		return fmt.Errorf("state %s is not PENDING or WITHDRAWN", row.GetState())
	}
	return nil
}

func validateTombstone(tomb *contentv1.Tombstone) error {
	if tomb == nil {
		return fmt.Errorf("nil tombstone")
	}
	if code := msgkit.CheckID(tomb.GetContentId()); code != msgkit.OK {
		return fmt.Errorf("content_id %q: %s", tomb.GetContentId(), code)
	}
	if t := tomb.GetCommittedUnixSeconds(); t < genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {
		return fmt.Errorf("committed_unix_seconds %d outside %d..%d", t, genesis.UnixSecondsMin, genesis.UnixSecondsMax)
	}
	if _, code := msgkit.CheckDigest(tomb.GetReasonDigest()); code != msgkit.OK {
		return fmt.Errorf("reason_digest: %s", code)
	}
	if tomb.GetHistoryErased() {
		return fmt.Errorf("history_erased must be false")
	}
	if tomb.GetRetentionDecisionId() != RetentionDecisionID {
		return fmt.Errorf("retention_decision_id %q, want %s", tomb.GetRetentionDecisionId(), RetentionDecisionID)
	}
	if tomb.GetRetentionValueVersion() != 0 {
		return fmt.Errorf("retention_value_version %d, want 0 while DEC-004 is unset", tomb.GetRetentionValueVersion())
	}
	return nil
}

// InitGenesis validates state and writes every row. Rows are written by
// key, so the stored order does not depend on the input order. It then
// writes the store layout version (msgkit.LayoutVersionKey), so the store
// is never empty and reloads from a node's database.
func (k Keeper) InitGenesis(ctx sdk.Context, state *contentv1.GenesisState) error {
	if err := ValidateGenesis(state); err != nil {
		return err
	}
	for _, row := range state.GetContents() {
		k.setContent(ctx, &contentv1.Content{
			ContentId:      row.GetContentId(),
			ContentVersion: row.GetContentVersion(),
			ManifestDigest: sha256Digest(row.GetManifestDigest().GetValue()),
			PackageBytes:   row.GetPackageBytes(),
			SubmittingHost: append([]byte(nil), row.GetSubmittingHost()...),
			State:          row.GetState(),
		})
	}
	for _, tomb := range state.GetTombstones() {
		k.setTombstone(ctx, &contentv1.Tombstone{
			ContentId:             tomb.GetContentId(),
			ContentVersion:        tomb.GetContentVersion(),
			CommittedUnixSeconds:  tomb.GetCommittedUnixSeconds(),
			ReasonDigest:          sha256Digest(tomb.GetReasonDigest().GetValue()),
			HistoryErased:         false,
			RetentionDecisionId:   RetentionDecisionID,
			RetentionValueVersion: 0,
		})
	}
	msgkit.WriteLayoutVersion(ctx.KVStore(k.storeKey), ConsensusVersion)
	return nil
}

// ExportGenesis reads every content row and tombstone in key order
// (content id bytes, then version).
func (k Keeper) ExportGenesis(ctx sdk.Context) *contentv1.GenesisState {
	return &contentv1.GenesisState{
		Contents:   k.allContents(ctx),
		Tombstones: k.allTombstones(ctx),
	}
}
