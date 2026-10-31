package genesis

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Document is the runnable genesis inventory. It is public. A private key
// field is rejected.
type Document struct {
	ChainID                         string                `json:"chain_id"`
	GenesisUnixSeconds              int64                 `json:"genesis_unix_seconds"`
	ProtocolVersion                 uint32                `json:"protocol_version"`
	MintModuleEnabled               bool                  `json:"mint_module_enabled"`
	DistributionModuleEnabled       bool                  `json:"distribution_module_enabled"`
	SlashingModuleEnabled           bool                  `json:"slashing_module_enabled"`
	StakingModuleEnabled            bool                  `json:"staking_module_enabled"`
	PermissionlessValidatorCreation bool                  `json:"permissionless_validator_creation"`
	AllowDevnetTestParameters       bool                  `json:"allow_devnet_test_parameters"`
	Validators                      []Validator           `json:"validators"`
	Submitters                      []Submitter           `json:"submitters"`
	ApprovedCouncil                 []json.RawMessage     `json:"approved_council"`
	BlockedDecisions                []BlockedDecision     `json:"blocked_decisions"`
	DevnetTestParameters            []DevnetTestParameter `json:"devnet_test_parameters"`
	Contents                        []json.RawMessage     `json:"contents"`
	Leases                          []json.RawMessage     `json:"leases"`
	Offers                          []json.RawMessage     `json:"offers"`
	EvidenceRecords                 []json.RawMessage     `json:"evidence_records"`
	SafetyCaps                      SafetyCaps            `json:"safety_caps"`
}

// Validator is a public consensus key and its voting power.
// ConsensusPubkeyHex is 32 raw bytes, hex encoded, not a private key.
type Validator struct {
	NodeID             string `json:"node_id"`
	Power              int64  `json:"power"`
	ConsensusPubkeyHex string `json:"consensus_pubkey_hex"`
}

// Submitter is an admitted application account. AddressHex is 20 bytes.
type Submitter struct {
	AddressHex string `json:"address_hex"`
	Role       string `json:"role"`
}

// BlockedDecision stays unapproved. value_version 0 means no approved value.
type BlockedDecision struct {
	DecisionID    string `json:"decision_id"`
	SchemaVersion uint32 `json:"schema_version"`
	ValueVersion  uint32 `json:"value_version"`
	Approved      bool   `json:"approved"`
}

// DevnetTestParameter is a labeled test value. It is not an approved policy.
type DevnetTestParameter struct {
	Label          string `json:"label"`
	DecisionID     string `json:"decision_id"`
	ApprovedPolicy bool   `json:"approved_policy"`
	NumericValue   uint64 `json:"numeric_value,omitempty"`
	RoleName       string `json:"role_name,omitempty"`
	DelaySeconds   int64  `json:"delay_seconds,omitempty"`
}

// SafetyCaps are the protocol v0 ceilings.
type SafetyCaps struct {
	SafetyMaxPackageBytes          uint64 `json:"safety_max_package_bytes"`
	SafetyMaxEvidenceBatch         uint32 `json:"safety_max_evidence_batch"`
	SafetyMaxExpiryStepsPerBlock   uint32 `json:"safety_max_expiry_steps_per_block"`
	SafetyMaxPageLimit             uint32 `json:"safety_max_page_limit"`
	SafetyMaxTxBytes               uint64 `json:"safety_max_tx_bytes"`
	SafetyMaxGasPerTx              uint64 `json:"safety_max_gas_per_tx"`
	SafetyMaxOpenOffersPerProvider uint32 `json:"safety_max_open_offers_per_provider"`
}

// CometIdentity is the consensus metadata that must match the document.
// Empty fields skip that comparison. A node start passes the file's chain id
// and genesis time so a hand-edited mismatch is rejected.
type CometIdentity struct {
	ChainID            string
	GenesisUnixSeconds int64
	// ValidatorPubkeyHex is the comet validator set, in any order.
	// Nil means the caller is not comparing the comet set.
	ValidatorPubkeyHex []string
	ValidatorPowers    []int64
}

const (
	RoleAuthority        = "AUTHORITY"
	RoleAdmittedService  = "ADMITTED_SERVICE"
	RoleAdmittedProvider = "ADMITTED_PROVIDER"
	RoleBroadcaster      = "BROADCASTER"
	RoleAuditor          = "AUDITOR"
)

var (
	idPattern = regexp.MustCompile(IDPattern)
	roles     = map[string]struct{}{
		RoleAuthority:        {},
		RoleAdmittedService:  {},
		RoleAdmittedProvider: {},
		RoleBroadcaster:      {},
		RoleAuditor:          {},
	}
	forbiddenJSON = []string{
		"private_key",
		"mnemonic",
		"priv_validator",
		"seed_phrase",
	}
)

// Parse decodes a document and rejects unknown fields and private-key material.
func Parse(raw []byte) (Document, error) {
	if bytes.Contains(bytes.ToLower(raw), []byte("private_key")) {
		return Document{}, fmt.Errorf("genesis contains private_key")
	}
	lower := strings.ToLower(string(raw))
	for _, needle := range forbiddenJSON {
		if strings.Contains(lower, needle) {
			return Document{}, fmt.Errorf("genesis contains %s", needle)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("malformed genesis: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != nil && err.Error() != "EOF" {
		return Document{}, fmt.Errorf("malformed genesis trailing data: %w", err)
	}
	return doc, nil
}

// Validate checks the document against protocol v0. comet may be zero.
func Validate(doc Document, comet CometIdentity) error {
	if doc.ChainID == "" || len(doc.ChainID) > MaxChainIDLen {
		return fmt.Errorf("chain_id length")
	}
	if strings.ContainsAny(doc.ChainID, " \t\r\n") {
		return fmt.Errorf("chain_id whitespace")
	}
	if doc.GenesisUnixSeconds < UnixSecondsMin || doc.GenesisUnixSeconds > UnixSecondsMax {
		return fmt.Errorf("genesis_unix_seconds out of range")
	}
	if doc.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("protocol_version")
	}
	if doc.MintModuleEnabled || doc.DistributionModuleEnabled || doc.SlashingModuleEnabled || doc.StakingModuleEnabled || doc.PermissionlessValidatorCreation {
		return fmt.Errorf("economic or permissionless module flag is enabled")
	}
	if len(doc.ApprovedCouncil) != 0 {
		return fmt.Errorf("approved_council must be empty")
	}
	if len(doc.Contents) != 0 || len(doc.Leases) != 0 || len(doc.Offers) != 0 {
		return fmt.Errorf("content, leases, and offers must be empty at genesis")
	}
	if len(doc.EvidenceRecords) != 0 {
		return fmt.Errorf("evidence records must be empty at genesis")
	}
	if err := validateDecisions(doc.BlockedDecisions); err != nil {
		return err
	}
	if err := validateCaps(doc.SafetyCaps); err != nil {
		return err
	}
	if err := validateDevnet(doc); err != nil {
		return err
	}
	pubs, powers, err := validateValidators(doc.Validators)
	if err != nil {
		return err
	}
	if err := validateSubmitters(doc.Submitters); err != nil {
		return err
	}
	if comet.ChainID != "" && comet.ChainID != doc.ChainID {
		return fmt.Errorf("inconsistent chain id: document %s comet %s", doc.ChainID, comet.ChainID)
	}
	if comet.GenesisUnixSeconds != 0 && comet.GenesisUnixSeconds != doc.GenesisUnixSeconds {
		return fmt.Errorf("inconsistent genesis time")
	}
	if comet.ValidatorPubkeyHex != nil {
		if len(comet.ValidatorPubkeyHex) != len(pubs) || len(comet.ValidatorPowers) != len(powers) {
			return fmt.Errorf("inconsistent validator set length")
		}
		seen := map[string]int64{}
		for i, hexKey := range comet.ValidatorPubkeyHex {
			key := strings.ToLower(hexKey)
			if _, ok := seen[key]; ok {
				return fmt.Errorf("duplicate comet validator key")
			}
			seen[key] = comet.ValidatorPowers[i]
		}
		for i, key := range pubs {
			power, ok := seen[key]
			if !ok || power != powers[i] {
				return fmt.Errorf("comet validator set does not match the document")
			}
		}
	}
	return nil
}

func validateDecisions(rows []BlockedDecision) error {
	if len(rows) != len(DecisionIDs) {
		return fmt.Errorf("decision set")
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.DecisionID]; ok {
			return fmt.Errorf("duplicate decision %s", row.DecisionID)
		}
		seen[row.DecisionID] = struct{}{}
		if !slices.Contains(DecisionIDs, row.DecisionID) {
			return fmt.Errorf("unknown decision %s", row.DecisionID)
		}
		if row.Approved || row.ValueVersion != 0 || row.SchemaVersion != 1 {
			return fmt.Errorf("%s is not blocked", row.DecisionID)
		}
	}
	for _, id := range DecisionIDs {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("decision set missing %s", id)
		}
	}
	return nil
}

func validateCaps(caps SafetyCaps) error {
	want := DefaultSafetyCaps()
	if caps != want {
		return fmt.Errorf("safety caps do not match protocol bounds")
	}
	return nil
}

// DefaultSafetyCaps returns the protocol ceilings.
func DefaultSafetyCaps() SafetyCaps {
	return SafetyCaps{
		SafetyMaxPackageBytes:          MaxPackageBytes,
		SafetyMaxEvidenceBatch:         MaxEvidenceBatch,
		SafetyMaxExpiryStepsPerBlock:   MaxExpiryStepsPerBlock,
		SafetyMaxPageLimit:             MaxPageLimit,
		SafetyMaxTxBytes:               MaxTxBytes,
		SafetyMaxGasPerTx:              MaxGasPerTx,
		SafetyMaxOpenOffersPerProvider: MaxOpenOffersPerProvider,
	}
}

func validateDevnet(doc Document) error {
	if !doc.AllowDevnetTestParameters {
		if len(doc.DevnetTestParameters) != 0 {
			return fmt.Errorf("devnet test parameters require the devnet flag and chain-id prefix")
		}
		return nil
	}
	if !strings.HasPrefix(doc.ChainID, DevnetChainIDPrefix) {
		return fmt.Errorf("devnet chain id")
	}
	for _, row := range doc.DevnetTestParameters {
		if row.Label != DevnetTestLabel || row.ApprovedPolicy {
			return fmt.Errorf("devnet test label")
		}
		if !slices.Contains(DecisionIDs, row.DecisionID) {
			return fmt.Errorf("devnet test decision %s", row.DecisionID)
		}
	}
	return nil
}

func validateValidators(rows []Validator) ([]string, []int64, error) {
	if len(rows) < MinValidators {
		return nil, nil, fmt.Errorf("validator count %d is below %d", len(rows), MinValidators)
	}
	pubs := make([]string, 0, len(rows))
	powers := make([]int64, 0, len(rows))
	seenPub := map[string]struct{}{}
	seenNode := map[string]struct{}{}
	var total int64
	var maxPower int64
	for _, row := range rows {
		if !idPattern.MatchString(row.NodeID) {
			return nil, nil, fmt.Errorf("validator node id %q", row.NodeID)
		}
		if _, ok := seenNode[row.NodeID]; ok {
			return nil, nil, fmt.Errorf("duplicate validator node id %s", row.NodeID)
		}
		seenNode[row.NodeID] = struct{}{}
		raw, err := hex.DecodeString(row.ConsensusPubkeyHex)
		if err != nil || len(raw) != ConsensusKeyBytes {
			return nil, nil, fmt.Errorf("validator key %s", row.NodeID)
		}
		key := strings.ToLower(row.ConsensusPubkeyHex)
		if _, ok := seenPub[key]; ok {
			return nil, nil, fmt.Errorf("duplicate validator key")
		}
		seenPub[key] = struct{}{}
		if row.Power <= 0 {
			return nil, nil, fmt.Errorf("nonpositive validator power")
		}
		if maxPower == 0 {
			maxPower = row.Power
		}
		if row.Power != maxPower {
			return nil, nil, fmt.Errorf("validator powers are not equal")
		}
		if total > int64(^uint64(0)>>1)-row.Power {
			return nil, nil, fmt.Errorf("validator power overflow")
		}
		total += row.Power
		pubs = append(pubs, key)
		powers = append(powers, row.Power)
	}
	// Equal positive powers and at least four validators imply max*3 < total
	// only when the count is at least 4. The count check is above. Keep the
	// share test explicit so a later edit cannot drop it.
	if maxPower*3 >= total {
		return nil, nil, fmt.Errorf("validator power share")
	}
	return pubs, powers, nil
}

func validateSubmitters(rows []Submitter) error {
	if len(rows) == 0 {
		return fmt.Errorf("admitted application accounts are empty")
	}
	seen := map[string]struct{}{}
	var authority int
	for _, row := range rows {
		raw, err := hex.DecodeString(row.AddressHex)
		if err != nil || len(raw) != AccountBytes {
			return fmt.Errorf("submitter address")
		}
		key := strings.ToLower(row.AddressHex)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate submitter")
		}
		seen[key] = struct{}{}
		if _, ok := roles[row.Role]; !ok {
			return fmt.Errorf("submitter role %q", row.Role)
		}
		if row.Role == RoleAuthority {
			authority++
		}
	}
	if authority == 0 {
		return fmt.Errorf("initial authority account is missing")
	}
	return nil
}

// Time returns the document time in UTC.
func (doc Document) Time() time.Time {
	return time.Unix(doc.GenesisUnixSeconds, 0).UTC()
}
