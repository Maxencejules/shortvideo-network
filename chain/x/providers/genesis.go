package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Module genesis. The launch genesis keeps the provider list empty (goal 05
// rule, enforced by the app at InitChain; DR-07-02). The module itself
// validates, imports, and exports any list so an exported catalogue can be
// checked and re-imported into a fresh store. Business ids are not part of
// the v0 genesis schema and are not exported (DR-07-03).

// exportJSON is the export encoding, the same options x/authority uses.
var exportJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

// ParseGenesis decodes a module genesis document. Unknown fields are
// errors. An empty document is an error: the app always writes the key.
func ParseGenesis(raw json.RawMessage) (*providersv1.GenesisState, error) {
	if len(raw) == 0 {
		return nil, errors.New("providers genesis: empty document")
	}
	var state providersv1.GenesisState
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("providers genesis: %w", err)
	}
	return &state, nil
}

// ValidateGenesis is a pure check of a decoded module genesis:
//
//   - provider_id in the id pattern and unique
//   - account 20 bytes; signing_pubkey 32 bytes; signing_key_id in the id pattern
//   - key_generation >= 1; previous_signing_keys has key_generation-1 entries
//     with generations 1, 2, ... in order, 32-byte keys, and retired times in
//     [1, 253402300799] that never decrease
//   - endpoint_manifest_digest a SHA-256 digest of 32 bytes
//   - state APPLIED, ADMITTED, DRAINING, or DISABLED
//   - admitted_capacity_bytes >= 1; reserved_bytes <= admitted_capacity_bytes
//   - operator_group_id in the id pattern
//   - no unknown fields in any record
//
// A retired key may equal the current key: the v0 spec lets a provider
// rotate back to an earlier key, and the chain must be able to export what
// it committed.
func ValidateGenesis(state *providersv1.GenesisState) error {
	if state == nil {
		return errors.New("providers genesis: nil state")
	}
	if len(state.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("providers genesis: unknown fields")
	}
	seen := make(map[string]bool, len(state.GetProviders()))
	for i, record := range state.GetProviders() {
		if err := validateProvider(record); err != nil {
			return fmt.Errorf("providers genesis: provider %d: %w", i, err)
		}
		if seen[record.GetProviderId()] {
			return fmt.Errorf("providers genesis: provider %d: duplicate provider_id %q", i, record.GetProviderId())
		}
		seen[record.GetProviderId()] = true
	}
	return nil
}

func hasUnknown(m protoreflect.ProtoMessage) bool {
	return len(m.ProtoReflect().GetUnknown()) != 0
}

func validateProvider(p *providersv1.Provider) error {
	if p == nil {
		return errors.New("nil record")
	}
	if hasUnknown(p) {
		return errors.New("unknown fields")
	}
	if !msgkit.ValidID(p.GetProviderId()) {
		return fmt.Errorf("provider_id %q outside the id pattern", p.GetProviderId())
	}
	if len(p.GetAccount()) != msgkit.AccountBytes {
		return fmt.Errorf("account is %d bytes, want %d", len(p.GetAccount()), msgkit.AccountBytes)
	}
	if len(p.GetSigningPubkey()) != msgkit.PublicKeyBytes {
		return fmt.Errorf("signing_pubkey is %d bytes, want %d", len(p.GetSigningPubkey()), msgkit.PublicKeyBytes)
	}
	if !msgkit.ValidID(p.GetSigningKeyId()) {
		return fmt.Errorf("signing_key_id %q outside the id pattern", p.GetSigningKeyId())
	}
	if p.GetKeyGeneration() < 1 {
		return errors.New("key_generation is 0")
	}
	if err := validatePreviousKeys(p.GetKeyGeneration(), p.GetPreviousSigningKeys()); err != nil {
		return err
	}
	if p.GetEndpointManifestDigest() != nil && hasUnknown(p.GetEndpointManifestDigest()) {
		return errors.New("endpoint_manifest_digest has unknown fields")
	}
	if _, code := msgkit.CheckDigest(p.GetEndpointManifestDigest()); code != msgkit.OK {
		return fmt.Errorf("endpoint_manifest_digest: %s", code)
	}
	switch p.GetState() {
	case providersv1.ProviderState_PROVIDER_STATE_APPLIED,
		providersv1.ProviderState_PROVIDER_STATE_ADMITTED,
		providersv1.ProviderState_PROVIDER_STATE_DRAINING,
		providersv1.ProviderState_PROVIDER_STATE_DISABLED:
	default:
		return fmt.Errorf("state %d is not a provider state", int32(p.GetState()))
	}
	if p.GetAdmittedCapacityBytes() < 1 {
		return errors.New("admitted_capacity_bytes is 0")
	}
	if p.GetReservedBytes() > p.GetAdmittedCapacityBytes() {
		return fmt.Errorf("reserved_bytes %d above admitted_capacity_bytes %d", p.GetReservedBytes(), p.GetAdmittedCapacityBytes())
	}
	if !msgkit.ValidID(p.GetOperatorGroupId()) {
		return fmt.Errorf("operator_group_id %q outside the id pattern", p.GetOperatorGroupId())
	}
	return nil
}

func validatePreviousKeys(generation uint64, previous []*providersv1.PreviousSigningKey) error {
	if uint64(len(previous)) != generation-1 {
		return fmt.Errorf("%d previous_signing_keys for key_generation %d, want %d", len(previous), generation, generation-1)
	}
	var lastRetired int64
	for i, key := range previous {
		if key == nil {
			return fmt.Errorf("previous_signing_keys[%d] is nil", i)
		}
		if hasUnknown(key) {
			return fmt.Errorf("previous_signing_keys[%d] has unknown fields", i)
		}
		if key.GetGeneration() != uint64(i)+1 {
			return fmt.Errorf("previous_signing_keys[%d] generation %d, want %d", i, key.GetGeneration(), i+1)
		}
		if len(key.GetPublicKey()) != msgkit.PublicKeyBytes {
			return fmt.Errorf("previous_signing_keys[%d] public_key is %d bytes", i, len(key.GetPublicKey()))
		}
		retired := key.GetRetiredUnixSeconds()
		if retired < genesis.UnixSecondsMin || retired > genesis.UnixSecondsMax {
			return fmt.Errorf("previous_signing_keys[%d] retired_unix_seconds %d out of range", i, retired)
		}
		if retired < lastRetired {
			return fmt.Errorf("previous_signing_keys[%d] retired_unix_seconds %d before %d", i, retired, lastRetired)
		}
		lastRetired = retired
	}
	return nil
}

// InitGenesis validates state and writes its records into an empty store in
// id byte order, so the IAVL tree shape does not depend on the order of the
// document. A store that already holds a provider is an error. It then
// writes the store layout version (msgkit.LayoutVersionKey), so the store
// is never empty and reloads from a node's database.
func (k Keeper) InitGenesis(ctx sdk.Context, state *providersv1.GenesisState) error {
	if err := ValidateGenesis(state); err != nil {
		return err
	}
	prefix := []byte{ProviderKeyPrefix}
	it := ctx.KVStore(k.key).Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	occupied := it.Valid()
	if err := it.Close(); err != nil {
		return fmt.Errorf("providers genesis: close iterator: %w", err)
	}
	if occupied {
		return errors.New("providers genesis: store already holds providers")
	}
	records := append([]*providersv1.Provider(nil), state.GetProviders()...)
	sort.Slice(records, func(i, j int) bool { return records[i].GetProviderId() < records[j].GetProviderId() })
	for _, record := range records {
		// The record is stored without its retired keys; each retired key
		// is a row of its own (store layout).
		stored := proto.Clone(record).(*providersv1.Provider)
		stored.PreviousSigningKeys = nil
		k.save(ctx, stored)
		for _, retired := range record.GetPreviousSigningKeys() {
			k.saveRetired(ctx, record.GetProviderId(), retired)
		}
	}
	msgkit.WriteLayoutVersion(ctx.KVStore(k.key), ConsensusVersion)
	return nil
}

// ExportGenesis reads every provider in id byte order, with its retired
// keys in previous_signing_keys.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*providersv1.GenesisState, error) {
	var records []*providersv1.Provider
	if err := k.iterateProviders(ctx, func(record *providersv1.Provider) error {
		records = append(records, record)
		return nil
	}); err != nil {
		return nil, err
	}
	state := &providersv1.GenesisState{}
	for _, record := range records {
		full, err := k.withHistory(ctx, record)
		if err != nil {
			return nil, err
		}
		state.Providers = append(state.Providers, full)
	}
	return state, nil
}

// MarshalGenesis is the module export encoding: protojson with proto field
// names and unpopulated fields. Compare decoded messages with proto.Equal,
// not these bytes; protojson output is not byte-stable across builds.
func MarshalGenesis(state *providersv1.GenesisState) (json.RawMessage, error) {
	raw, err := exportJSON.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("providers genesis: %w", err)
	}
	return raw, nil
}
