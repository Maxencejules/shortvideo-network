package policy

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Module genesis (svn.policy.v1.GenesisState). The document holds
// policy_version 1, the protocol safety caps, and the eight blocked
// decision rows. The PolicyState fields of protocol schema version 2
// (governed_limits, pending_limit_changes, upgrade_plan, app_version) are
// query fields: a genesis document leaves them empty ("Genesis carries no
// governed value, pending change, or plan", docs/protocol.md), and the app
// writes the app version from the binary through InitVersions. The document
// has no field for applied upgrades or the version map. InitGenesis stores
// only the layout version and the safety caps: the policy version and the
// decision rows live in the authority store, and the app cross-checks the
// decision rows against the authority genesis in InitChain.
//
// Export carries policy_version 1, the stored caps, and the authority's
// decision rows. It is refused (ErrExportRefused) once any governance state
// exists that the document cannot carry, so restart from an export is
// unsupported after the first governed change (DR-06-01; interim DR-10-04).

// exportJSON is the export encoding, the same options the other modules use.
var exportJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

// ErrExportRefused is wrapped by ExportCheck and ExportGenesis when the
// store holds state the policy genesis document cannot carry.
var ErrExportRefused = errors.New("policy export refused")

// DefaultSafetyCaps returns the protocol safety caps (genesis.DefaultSafetyCaps)
// as the protocol message.
func DefaultSafetyCaps() *policyv1.SafetyCaps {
	caps := genesis.DefaultSafetyCaps()
	return &policyv1.SafetyCaps{
		SafetyMaxPackageBytes:          caps.SafetyMaxPackageBytes,
		SafetyMaxEvidenceBatch:         caps.SafetyMaxEvidenceBatch,
		SafetyMaxExpiryStepsPerBlock:   caps.SafetyMaxExpiryStepsPerBlock,
		SafetyMaxPageLimit:             caps.SafetyMaxPageLimit,
		SafetyMaxTxBytes:               caps.SafetyMaxTxBytes,
		SafetyMaxGasPerTx:              caps.SafetyMaxGasPerTx,
		SafetyMaxOpenOffersPerProvider: caps.SafetyMaxOpenOffersPerProvider,
	}
}

// DefaultGenesisState is the only valid document: policy_version 1, the
// protocol caps, and one unapproved row per blocked decision (schema
// version 1, value version 0), in genesis.DecisionIDs order.
func DefaultGenesisState() *policyv1.GenesisState {
	rows := make([]*typesv1.BlockedDecisionParameter, len(genesis.DecisionIDs))
	for i, id := range genesis.DecisionIDs {
		rows[i] = &typesv1.BlockedDecisionParameter{DecisionId: id, SchemaVersion: 1}
	}
	return &policyv1.GenesisState{Policy: &policyv1.PolicyState{
		PolicyVersion:    1,
		SafetyCaps:       DefaultSafetyCaps(),
		BlockedDecisions: rows,
	}}
}

// ParseGenesis decodes a module genesis document. Unknown fields are
// errors. An empty document is an error: the app always writes the key.
func ParseGenesis(raw json.RawMessage) (*policyv1.GenesisState, error) {
	if len(raw) == 0 {
		return nil, errors.New("policy genesis: empty document")
	}
	var state policyv1.GenesisState
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("policy genesis: %w", err)
	}
	return &state, nil
}

// MarshalGenesis is the module export encoding: protojson with proto field
// names and unpopulated fields. Compare decoded messages with proto.Equal,
// not these bytes; protojson output is not byte-stable across builds.
func MarshalGenesis(state *policyv1.GenesisState) (json.RawMessage, error) {
	raw, err := exportJSON.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("policy genesis: %w", err)
	}
	return raw, nil
}

func hasUnknown(m protoreflect.ProtoMessage) bool {
	return len(m.ProtoReflect().GetUnknown()) != 0
}

// ValidateGenesis is a pure check of a decoded module genesis.
//
//   - the policy state is present ("{}" is refused); no message carries
//     unknown fields
//   - governed_limits, pending_limit_changes, and upgrade_plan empty, and
//     app_version 0 (query fields of schema version 2)
//   - policy_version 1
//   - safety_caps equal to the protocol caps (genesis.DefaultSafetyCaps)
//   - blocked_decisions: exactly one row per genesis.DecisionIDs entry, each
//     unapproved, value_version 0, schema_version 1 (no document can
//     approve a decision)
func ValidateGenesis(state *policyv1.GenesisState) error {
	if state == nil {
		return errors.New("policy genesis: nil state")
	}
	policy := state.GetPolicy()
	if policy == nil {
		return errors.New("policy genesis: policy state missing")
	}
	if hasUnknown(state) || hasUnknown(policy) {
		return errors.New("policy genesis: unknown fields")
	}
	if err := validateQueryFieldsEmpty(policy); err != nil {
		return err
	}
	if policy.GetPolicyVersion() != 1 {
		return fmt.Errorf("policy genesis: policy_version %d, want 1", policy.GetPolicyVersion())
	}
	caps := policy.GetSafetyCaps()
	if caps == nil {
		return errors.New("policy genesis: safety caps missing")
	}
	if hasUnknown(caps) {
		return errors.New("policy genesis: safety caps carry unknown fields")
	}
	if !proto.Equal(caps, DefaultSafetyCaps()) {
		return errors.New("policy genesis: safety caps differ from the protocol caps")
	}
	return validateDecisions(policy.GetBlockedDecisions())
}

// validateQueryFieldsEmpty refuses a document that sets any query field of
// schema version 2.
func validateQueryFieldsEmpty(policy *policyv1.PolicyState) error {
	switch {
	case len(policy.GetGovernedLimits()) != 0:
		return fmt.Errorf("policy genesis: %d governed_limits rows; genesis carries no governed value", len(policy.GetGovernedLimits()))
	case len(policy.GetPendingLimitChanges()) != 0:
		return fmt.Errorf("policy genesis: %d pending_limit_changes rows; genesis carries no pending change", len(policy.GetPendingLimitChanges()))
	case policy.GetUpgradePlan() != nil:
		return errors.New("policy genesis: upgrade_plan is set; genesis carries no plan")
	case policy.GetAppVersion() != 0:
		return fmt.Errorf("policy genesis: app_version %d; the app version comes from the binary at InitChain", policy.GetAppVersion())
	}
	return nil
}

func validateDecisions(rows []*typesv1.BlockedDecisionParameter) error {
	known := make(map[string]bool, len(genesis.DecisionIDs))
	for _, id := range genesis.DecisionIDs {
		known[id] = true
	}
	if len(rows) != len(genesis.DecisionIDs) {
		return fmt.Errorf("policy genesis: %d blocked decision rows, want %d", len(rows), len(genesis.DecisionIDs))
	}
	seen := make(map[string]bool, len(rows))
	for i, row := range rows {
		if row == nil || hasUnknown(row) {
			return fmt.Errorf("policy genesis: blocked decision row %d is nil or carries unknown fields", i)
		}
		id := row.GetDecisionId()
		if !known[id] {
			return fmt.Errorf("policy genesis: unknown decision %q", id)
		}
		if seen[id] {
			return fmt.Errorf("policy genesis: duplicate decision %s", id)
		}
		seen[id] = true
		if row.GetApproved() || row.GetValueVersion() != 0 || row.GetSchemaVersion() != 1 {
			return fmt.Errorf("policy genesis: %s is not blocked (approved %t, value_version %d, schema_version %d)",
				id, row.GetApproved(), row.GetValueVersion(), row.GetSchemaVersion())
		}
	}
	return nil
}

// InitGenesis validates state and writes it into an empty store: the layout
// version (msgkit.LayoutVersionKey), then the safety caps (0x01). A store
// holding any key (the scaffold key [0x01], a layout key, or any row) is an
// error. Nothing is written on an error.
func (k Keeper) InitGenesis(ctx sdk.Context, state *policyv1.GenesisState) error {
	if err := ValidateGenesis(state); err != nil {
		return err
	}
	store := k.kv(ctx)
	occupied, err := anyUnder(store, nil)
	if err != nil {
		return err
	}
	if occupied {
		return errors.New("policy genesis: store is not empty")
	}
	msgkit.WriteLayoutVersion(store, ConsensusVersion)
	store.Set(SafetyCapsKey(), EncodeSafetyCaps(state.GetPolicy().GetSafetyCaps()))
	return nil
}

// refusedRows are the rows the policy genesis document has no field for,
// in check order.
var refusedRows = []struct {
	prefix []byte
	what   string
}{
	{[]byte{GovernedPrefix}, "governed values"},
	{[]byte{PendingPrefix}, "pending governed changes"},
	{PlanKey(), "an upgrade plan"},
	{[]byte{AppliedPrefix}, "applied upgrades"},
	{[]byte{PlanNamePrefix}, "plan names"},
}

// ExportCheck refuses, with ErrExportRefused naming the prefix, while the
// store holds a governed value (0x02), a pending change (0x03), a plan
// (0x06), an applied upgrade (0x07), or a plan name (0x08), or while the
// app version (0x04) is not 1 (absent included), or the policy version is
// not 1. The version map
// (0x05) is derived from the binary and is not exported. App.Export calls
// it before the module manager export, because a module ExportGenesis can
// only panic. It reads at most one key per refused prefix.
func (k Keeper) ExportCheck(ctx sdk.Context) error {
	store := k.kv(ctx)
	for _, row := range refusedRows {
		found, err := anyUnder(store, row.prefix)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("%w: the store holds %s (prefix 0x%02x), which the policy genesis proto cannot carry", ErrExportRefused, row.what, row.prefix[0])
		}
	}
	appVersion, err := loadAppVersion(store)
	if err != nil {
		return err
	}
	if appVersion != 1 {
		return fmt.Errorf("%w: the app version (prefix 0x%02x) is %d, and a genesis document starts at app version 1", ErrExportRefused, appVersionByte, appVersion)
	}
	if v := k.deps.Versions.PolicyVersion(ctx); v != 1 {
		return fmt.Errorf("%w: the policy version is %d, and a genesis document carries policy_version 1", ErrExportRefused, v)
	}
	return nil
}

// ExportGenesis runs ExportCheck, then returns policy_version 1, the stored
// caps, and the blocked decision rows of Deps.Decisions. The result must
// pass ValidateGenesis, so an export is always importable.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*policyv1.GenesisState, error) {
	if err := k.ExportCheck(ctx); err != nil {
		return nil, err
	}
	caps, err := k.SafetyCaps(ctx)
	if err != nil {
		return nil, err
	}
	state := &policyv1.GenesisState{Policy: &policyv1.PolicyState{
		PolicyVersion:    1,
		SafetyCaps:       caps,
		BlockedDecisions: cloneDecisions(k.deps.Decisions.BlockedDecisions(ctx)),
	}}
	if err := ValidateGenesis(state); err != nil {
		return nil, fmt.Errorf("policy export: %w", err)
	}
	return state, nil
}

func cloneDecisions(rows []*typesv1.BlockedDecisionParameter) []*typesv1.BlockedDecisionParameter {
	out := make([]*typesv1.BlockedDecisionParameter, len(rows))
	for i, row := range rows {
		if row != nil {
			out[i] = proto.Clone(row).(*typesv1.BlockedDecisionParameter)
		}
	}
	return out
}
