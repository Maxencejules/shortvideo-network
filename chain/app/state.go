package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	ModuleSVN       = "svn"
	ModulePolicy    = "policy"
	ModuleProviders = "providers"
	ModuleContent   = "content"
	ModuleLeases    = "leases"
	ModuleEvidence  = "evidence"
)

var protoJSON = protojson.MarshalOptions{UseProtoNames: true}

// Header is the svn module's copy of the genesis metadata.
type Header struct {
	ChainID                         string `json:"chain_id"`
	GenesisUnixSeconds              int64  `json:"genesis_unix_seconds"`
	ProtocolVersion                 uint32 `json:"protocol_version"`
	MintModuleEnabled               bool   `json:"mint_module_enabled"`
	DistributionModuleEnabled       bool   `json:"distribution_module_enabled"`
	SlashingModuleEnabled           bool   `json:"slashing_module_enabled"`
	StakingModuleEnabled            bool   `json:"staking_module_enabled"`
	PermissionlessValidatorCreation bool   `json:"permissionless_validator_creation"`
	AllowDevnetTestParameters       bool   `json:"allow_devnet_test_parameters"`
}

// AppStateFromDocument splits a validated document into module genesis JSON.
func AppStateFromDocument(cdc codec.Codec, doc genesis.Document) (map[string]json.RawMessage, error) {
	if err := genesis.Validate(doc, genesis.CometIdentity{}); err != nil {
		return nil, err
	}
	header, err := json.Marshal(headerFrom(doc))
	if err != nil {
		return nil, err
	}
	authorityMsg, err := authorityState(doc)
	if err != nil {
		return nil, err
	}
	authorityRaw, err := protoJSON.Marshal(authorityMsg)
	if err != nil {
		return nil, err
	}
	policyRaw, err := protoJSON.Marshal(policyState(doc))
	if err != nil {
		return nil, err
	}
	providersRaw, err := protoJSON.Marshal(&providersv1.GenesisState{})
	if err != nil {
		return nil, err
	}
	contentRaw, err := protoJSON.Marshal(&contentv1.GenesisState{})
	if err != nil {
		return nil, err
	}
	leasesRaw, err := protoJSON.Marshal(&leasesv1.GenesisState{})
	if err != nil {
		return nil, err
	}
	evidenceRaw, err := protoJSON.Marshal(&evidencev1.GenesisState{})
	if err != nil {
		return nil, err
	}
	authRaw, err := authGenesis(cdc, doc)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{
		authtypes.ModuleName: authRaw,
		ModuleSVN:            header,
		ModulePolicy:         policyRaw,
		ModuleProviders:      providersRaw,
		ModuleContent:        contentRaw,
		ModuleLeases:         leasesRaw,
		ModuleEvidence:       evidenceRaw,
		"authority":          authorityRaw,
	}, nil
}

func headerFrom(doc genesis.Document) Header {
	return Header{
		ChainID:                         doc.ChainID,
		GenesisUnixSeconds:              doc.GenesisUnixSeconds,
		ProtocolVersion:                 doc.ProtocolVersion,
		MintModuleEnabled:               doc.MintModuleEnabled,
		DistributionModuleEnabled:       doc.DistributionModuleEnabled,
		SlashingModuleEnabled:           doc.SlashingModuleEnabled,
		StakingModuleEnabled:            doc.StakingModuleEnabled,
		PermissionlessValidatorCreation: doc.PermissionlessValidatorCreation,
		AllowDevnetTestParameters:       doc.AllowDevnetTestParameters,
	}
}

func authorityState(doc genesis.Document) (*authorityv1.GenesisState, error) {
	validators := make([]*authorityv1.ValidatorPower, len(doc.Validators))
	for i, row := range doc.Validators {
		raw, err := hex.DecodeString(row.ConsensusPubkeyHex)
		if err != nil {
			return nil, fmt.Errorf("validator %s key: %w", row.NodeID, err)
		}
		validators[i] = &authorityv1.ValidatorPower{
			ConsensusPubkey: raw,
			Power:           row.Power,
			NodeId:          row.NodeID,
		}
	}
	slices.SortFunc(validators, func(a, b *authorityv1.ValidatorPower) int {
		return bytes.Compare(a.GetConsensusPubkey(), b.GetConsensusPubkey())
	})
	submitters := make([]*typesv1.Submitter, len(doc.Submitters))
	for i, row := range doc.Submitters {
		raw, err := hex.DecodeString(row.AddressHex)
		if err != nil {
			return nil, fmt.Errorf("submitter %s: %w", row.AddressHex, err)
		}
		submitters[i] = &typesv1.Submitter{Address: raw, Role: role(row.Role)}
	}
	slices.SortFunc(submitters, func(a, b *typesv1.Submitter) int {
		return bytes.Compare(a.GetAddress(), b.GetAddress())
	})
	return &authorityv1.GenesisState{
		Authority: &authorityv1.AuthorityState{
			Validators: validators,
			Submitters: submitters,
			QuorumLoss: authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET,
		},
		BlockedDecisions:     blocked(doc.BlockedDecisions),
		DevnetTestParameters: devnetTests(doc.DevnetTestParameters),
	}, nil
}

func policyState(doc genesis.Document) *policyv1.GenesisState {
	caps := doc.SafetyCaps
	return &policyv1.GenesisState{
		Policy: &policyv1.PolicyState{
			PolicyVersion: 1,
			SafetyCaps: &policyv1.SafetyCaps{
				SafetyMaxPackageBytes:          caps.SafetyMaxPackageBytes,
				SafetyMaxEvidenceBatch:         caps.SafetyMaxEvidenceBatch,
				SafetyMaxExpiryStepsPerBlock:   caps.SafetyMaxExpiryStepsPerBlock,
				SafetyMaxPageLimit:             caps.SafetyMaxPageLimit,
				SafetyMaxTxBytes:               caps.SafetyMaxTxBytes,
				SafetyMaxGasPerTx:              caps.SafetyMaxGasPerTx,
				SafetyMaxOpenOffersPerProvider: caps.SafetyMaxOpenOffersPerProvider,
			},
			BlockedDecisions: blocked(doc.BlockedDecisions),
		},
	}
}

func blocked(rows []genesis.BlockedDecision) []*typesv1.BlockedDecisionParameter {
	out := make([]*typesv1.BlockedDecisionParameter, len(rows))
	for i, row := range rows {
		out[i] = &typesv1.BlockedDecisionParameter{
			DecisionId:    row.DecisionID,
			SchemaVersion: row.SchemaVersion,
			ValueVersion:  row.ValueVersion,
			Approved:      row.Approved,
		}
	}
	slices.SortFunc(out, func(a, b *typesv1.BlockedDecisionParameter) int {
		return strings.Compare(a.GetDecisionId(), b.GetDecisionId())
	})
	return out
}

func devnetTests(rows []genesis.DevnetTestParameter) []*typesv1.DevnetTestParameter {
	out := make([]*typesv1.DevnetTestParameter, len(rows))
	for i, row := range rows {
		out[i] = &typesv1.DevnetTestParameter{
			Label:          row.Label,
			DecisionId:     row.DecisionID,
			ApprovedPolicy: row.ApprovedPolicy,
			NumericValue:   row.NumericValue,
			RoleName:       row.RoleName,
			DelaySeconds:   row.DelaySeconds,
		}
	}
	slices.SortFunc(out, func(a, b *typesv1.DevnetTestParameter) int {
		if cmp := strings.Compare(a.GetDecisionId(), b.GetDecisionId()); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.GetLabel(), b.GetLabel()); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.GetRoleName(), b.GetRoleName()); cmp != 0 {
			return cmp
		}
		if a.GetNumericValue() < b.GetNumericValue() {
			return -1
		}
		if a.GetNumericValue() > b.GetNumericValue() {
			return 1
		}
		if a.GetDelaySeconds() < b.GetDelaySeconds() {
			return -1
		}
		if a.GetDelaySeconds() > b.GetDelaySeconds() {
			return 1
		}
		return 0
	})
	return out
}

func role(name string) typesv1.SubmitterRole {
	switch name {
	case genesis.RoleAuthority:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY
	case genesis.RoleAdmittedService:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE
	case genesis.RoleAdmittedProvider:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER
	case genesis.RoleBroadcaster:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER
	case genesis.RoleAuditor:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR
	default:
		return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
	}
}

func authGenesis(cdc codec.Codec, doc genesis.Document) (json.RawMessage, error) {
	rows := slices.Clone(doc.Submitters)
	slices.SortFunc(rows, func(a, b genesis.Submitter) int {
		return strings.Compare(strings.ToLower(a.AddressHex), strings.ToLower(b.AddressHex))
	})
	accounts := make([]authtypes.GenesisAccount, 0, len(rows))
	for i, row := range rows {
		raw, err := hex.DecodeString(row.AddressHex)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, authtypes.NewBaseAccount(sdk.AccAddress(raw), nil, uint64(i), 0))
	}
	state := authtypes.NewGenesisState(authtypes.DefaultParams(), accounts)
	return cdc.MarshalJSON(state)
}

// DocumentFromModules rebuilds the inventory from module genesis JSON.
func DocumentFromModules(modules map[string]json.RawMessage) (genesis.Document, error) {
	var header Header
	if err := json.Unmarshal(modules[ModuleSVN], &header); err != nil {
		return genesis.Document{}, fmt.Errorf("svn header: %w", err)
	}
	var authority authorityv1.GenesisState
	if err := protojson.Unmarshal(modules["authority"], &authority); err != nil {
		return genesis.Document{}, fmt.Errorf("authority: %w", err)
	}
	var policy policyv1.GenesisState
	if err := protojson.Unmarshal(modules[ModulePolicy], &policy); err != nil {
		return genesis.Document{}, fmt.Errorf("policy: %w", err)
	}
	if err := emptyCatalog(modules); err != nil {
		return genesis.Document{}, err
	}
	doc := genesis.Document{
		ChainID:                         header.ChainID,
		GenesisUnixSeconds:              header.GenesisUnixSeconds,
		ProtocolVersion:                 header.ProtocolVersion,
		MintModuleEnabled:               header.MintModuleEnabled,
		DistributionModuleEnabled:       header.DistributionModuleEnabled,
		SlashingModuleEnabled:           header.SlashingModuleEnabled,
		StakingModuleEnabled:            header.StakingModuleEnabled,
		PermissionlessValidatorCreation: header.PermissionlessValidatorCreation,
		AllowDevnetTestParameters:       header.AllowDevnetTestParameters,
		ApprovedCouncil:                 []json.RawMessage{},
		Contents:                        []json.RawMessage{},
		Leases:                          []json.RawMessage{},
		Offers:                          []json.RawMessage{},
		EvidenceRecords:                 []json.RawMessage{},
	}
	for _, row := range authority.GetAuthority().GetValidators() {
		doc.Validators = append(doc.Validators, genesis.Validator{
			NodeID:             row.GetNodeId(),
			Power:              row.GetPower(),
			ConsensusPubkeyHex: hex.EncodeToString(row.GetConsensusPubkey()),
		})
	}
	for _, row := range authority.GetAuthority().GetSubmitters() {
		doc.Submitters = append(doc.Submitters, genesis.Submitter{
			AddressHex: hex.EncodeToString(row.GetAddress()),
			Role:       roleName(row.GetRole()),
		})
	}
	for _, row := range authority.GetBlockedDecisions() {
		doc.BlockedDecisions = append(doc.BlockedDecisions, genesis.BlockedDecision{
			DecisionID:    row.GetDecisionId(),
			SchemaVersion: row.GetSchemaVersion(),
			ValueVersion:  row.GetValueVersion(),
			Approved:      row.GetApproved(),
		})
	}
	for _, row := range authority.GetDevnetTestParameters() {
		doc.DevnetTestParameters = append(doc.DevnetTestParameters, genesis.DevnetTestParameter{
			Label:          row.GetLabel(),
			DecisionID:     row.GetDecisionId(),
			ApprovedPolicy: row.GetApprovedPolicy(),
			NumericValue:   row.GetNumericValue(),
			RoleName:       row.GetRoleName(),
			DelaySeconds:   row.GetDelaySeconds(),
		})
	}
	caps := policy.GetPolicy().GetSafetyCaps()
	doc.SafetyCaps = genesis.SafetyCaps{
		SafetyMaxPackageBytes:          caps.GetSafetyMaxPackageBytes(),
		SafetyMaxEvidenceBatch:         caps.GetSafetyMaxEvidenceBatch(),
		SafetyMaxExpiryStepsPerBlock:   caps.GetSafetyMaxExpiryStepsPerBlock(),
		SafetyMaxPageLimit:             caps.GetSafetyMaxPageLimit(),
		SafetyMaxTxBytes:               caps.GetSafetyMaxTxBytes(),
		SafetyMaxGasPerTx:              caps.GetSafetyMaxGasPerTx(),
		SafetyMaxOpenOffersPerProvider: caps.GetSafetyMaxOpenOffersPerProvider(),
	}
	return doc, nil
}

func emptyCatalog(modules map[string]json.RawMessage) error {
	var providers providersv1.GenesisState
	if err := protojson.Unmarshal(modules[ModuleProviders], &providers); err != nil {
		return err
	}
	if len(providers.GetProviders()) != 0 {
		return fmt.Errorf("providers are not empty")
	}
	var content contentv1.GenesisState
	if err := protojson.Unmarshal(modules[ModuleContent], &content); err != nil {
		return err
	}
	if len(content.GetContents()) != 0 || len(content.GetTombstones()) != 0 {
		return fmt.Errorf("content is not empty")
	}
	var leases leasesv1.GenesisState
	if err := protojson.Unmarshal(modules[ModuleLeases], &leases); err != nil {
		return err
	}
	if len(leases.GetOffers()) != 0 || len(leases.GetLeases()) != 0 {
		return fmt.Errorf("leases are not empty")
	}
	var evidence evidencev1.GenesisState
	if err := protojson.Unmarshal(modules[ModuleEvidence], &evidence); err != nil {
		return err
	}
	if len(evidence.GetRecords()) != 0 || evidence.GetCurrentEpoch() != 0 {
		return fmt.Errorf("evidence is not empty")
	}
	return nil
}

func roleName(role typesv1.SubmitterRole) string {
	switch role {
	case typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY:
		return genesis.RoleAuthority
	case typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE:
		return genesis.RoleAdmittedService
	case typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER:
		return genesis.RoleAdmittedProvider
	case typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER:
		return genesis.RoleBroadcaster
	case typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR:
		return genesis.RoleAuditor
	default:
		return ""
	}
}
