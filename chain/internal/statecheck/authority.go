package statecheck

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The x/authority store layout (layout version 2, chain/x/authority
// store.go): one prefix byte, then the fields that name the object.
const (
	authorityLayoutKey      byte = 0x00
	authorityConfigKey      byte = 0x01
	authorityAdmitPrefix    byte = 0x02
	authorityPendingPrefix  byte = 0x03
	authorityPolicyKey      byte = 0x04
	authorityProposalPrefix byte = 0x10
	authorityBusinessPrefix byte = 0x11
	authorityExecutedPrefix byte = 0x12
	authorityOpenPrefix     byte = 0x14
	authorityRetiredSeat    byte = 0x15
	authorityRetiredService byte = 0x16
)

// authorityPrefixes are the first key bytes of the authority layout. The
// three single-key rows (layout, config, policy version) must be exactly
// one byte long. Every other first byte (among them the retired 0x13 and
// 0x17 and the round-2 side document 0x20) is outside the layout.
var authorityPrefixes = map[byte]string{
	authorityLayoutKey:      "layout version",
	authorityConfigKey:      "config",
	authorityAdmitPrefix:    "admission row",
	authorityPendingPrefix:  "pending validator change",
	authorityPolicyKey:      "policy version",
	authorityProposalPrefix: "proposal record",
	authorityBusinessPrefix: "business id",
	authorityExecutedPrefix: "executed digest",
	authorityOpenPrefix:     "open count",
	authorityRetiredSeat:    "retired seat key",
	authorityRetiredService: "retired service address",
}

// maxGovernancePages bounds the Query/Governance walk. Each page holds at
// least one item, so the walk of a store with fewer items ends before it.
const maxGovernancePages = 1 << 24

// storedProposal is the part of a stored proposal record (chain/x/authority
// store.go proposalRecord, JSON) the council invariants read: the approving
// seat ids and the proposer are not in the Query/Governance proposal view.
// Query/Governance has already decoded every record strictly.
type storedProposal struct {
	ProposalID        string   `json:"proposal_id"`
	BasePolicyVersion uint64   `json:"base_policy_version"`
	State             int32    `json:"state"`
	ApproverKeys      [][]byte `json:"approver_keys"`
	Proposer          string   `json:"proposer"`
	Approvers         []string `json:"approvers"`
}

// checkAuthority checks the x/authority invariants a single state can show
// (plan section 10, state-only forms) and returns the first violation; the
// error text starts with the invariant name:
//
//	layout  the layout key holds authority.ConsensusVersion; every row sits under a
//	        prefix of the layout; the layout and config rows are single keys
//	I13     (exact-key rule) the policy version row is the exact key {0x04}: no row is
//	        beside it, so the I13 digest's one exempt key is exactly that row
//	I7      the config key holds no proposal and no pending change; every decision row
//	        is unapproved with value version 0; the approved council is empty and quorum
//	        loss UNSET
//	I7, I8, I11  authority.ValidateGenesisState on the stored config with the stored
//	        pending changes: well-formed seats, submitters, and validators; no seat
//	        address a submitter; no seat key a current or pending consensus key; the
//	        validator set meets the power invariant (DEC-011), and so does every
//	        activation point of the pending changes (DEC-012)
//	I8      no seat key, validator key, or pending consensus key is a retired seat key
//	I1      (state-only form) policy version >= 1 + the EXECUTED proposals
//	I9      every OPEN or QUEUED proposal has as many approver keys as approving seats,
//	        distinct seats and distinct keys; a QUEUED one has at least the threshold
//	I10     the open counts (0x14) equal the OPEN and QUEUED proposals per (base
//	        version, proposer)
//
// The pending changes and the proposal records are read through
// Query/Governance, which decodes each with the authority module's own
// strict decoders; a record or pending change that does not decode fails
// the check. k must carry the x/policy keeper (WithPolicy): the walk reads
// the governed page limit.
func checkAuthority(ctx sdk.Context, k authority.Keeper, key *storetypes.KVStoreKey) error {
	kv := ctx.KVStore(key)
	raw, counts, err := authorityRows(kv)
	if err != nil {
		return err
	}
	encoded, err := k.ConsensusBytes(ctx)
	if err != nil {
		return fmt.Errorf("I7: config key: %w", err)
	}
	var cfg authorityv1.GenesisState
	if err := proto.Unmarshal(encoded, &cfg); err != nil {
		return fmt.Errorf("I7: config key does not decode: %w", err)
	}
	if len(cfg.GetAuthority().GetProposals()) != 0 || len(cfg.GetAuthority().GetPendingValidatorPower()) != 0 {
		return errors.New("I7: the config key holds proposals or pending changes")
	}
	for _, row := range cfg.GetBlockedDecisions() {
		if row.GetApproved() || row.GetValueVersion() != 0 {
			return fmt.Errorf("I7: decision row %s is approved or has value version %d", row.GetDecisionId(), row.GetValueVersion())
		}
	}
	pending, proposals, err := governance(ctx, k)
	if err != nil {
		return err
	}
	withPending := proto.Clone(&cfg).(*authorityv1.GenesisState)
	withPending.Authority.PendingValidatorPower = pending
	if err := authority.ValidateGenesisState(withPending, nil); err != nil {
		return fmt.Errorf("I7, I8, I11: %w", err)
	}
	if err := checkRetired(kv, cfg.GetAuthority(), pending); err != nil {
		return err
	}
	executed := uint64(0)
	for _, p := range proposals {
		if p.GetState() == authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
			executed++
		}
	}
	if version := k.PolicyVersion(ctx); version < 1+executed {
		return fmt.Errorf("I1: policy version %d, below 1 + %d executed proposals", version, executed)
	}
	if len(raw) != len(proposals) {
		return fmt.Errorf("I9: %d proposal rows, Query/Governance returned %d proposals", len(raw), len(proposals))
	}
	return checkProposals(raw, counts)
}

// authorityRows walks every authority row once, in key order: it checks the
// layout and the exact-key rule, and returns the raw proposal records and
// the open counts by key.
func authorityRows(kv storetypes.KVStore) (records [][]byte, counts map[string]uint64, err error) {
	counts = map[string]uint64{}
	layout := false
	it := kv.Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		key, value := it.Key(), it.Value()
		name, known := authorityPrefixes[key[0]]
		switch {
		case !known:
			return nil, nil, fmt.Errorf("layout: authority row %x is under prefix 0x%02x, outside the layout", key, key[0])
		case key[0] == authorityPolicyKey && len(key) != 1:
			return nil, nil, fmt.Errorf("I13: authority row %x is beside the policy version key {0x04}", key)
		case (key[0] == authorityLayoutKey || key[0] == authorityConfigKey) && len(key) != 1:
			return nil, nil, fmt.Errorf("layout: authority row %x is beside the %s key", key, name)
		case key[0] == authorityLayoutKey:
			if len(value) != 8 || binary.BigEndian.Uint64(value) != authority.ConsensusVersion {
				return nil, nil, fmt.Errorf("layout: authority layout version %x, want %d", value, authority.ConsensusVersion)
			}
			layout = true
		case key[0] == authorityProposalPrefix:
			records = append(records, bytes.Clone(value))
		case key[0] == authorityOpenPrefix:
			if len(value) != 8 || binary.BigEndian.Uint64(value) == 0 {
				return nil, nil, fmt.Errorf("I10: open count %x holds %x", key, value)
			}
			counts[string(key)] = binary.BigEndian.Uint64(value)
		}
	}
	if !layout {
		return nil, nil, errors.New("layout: the authority store has no layout version key")
	}
	return records, counts, nil
}

// governance walks Query/Governance from the first page to the last and
// returns the pending changes and the proposals.
func governance(ctx sdk.Context, k authority.Keeper) ([]*authorityv1.PendingValidatorPower, []*authorityv1.Proposal, error) {
	var pending []*authorityv1.PendingValidatorPower
	var proposals []*authorityv1.Proposal
	var next []byte
	for page := 0; page < maxGovernancePages; page++ {
		res, err := k.Governance(ctx, &authorityv1.QueryGovernanceRequest{Page: &typesv1.PageRequest{PageKey: next}})
		if err != nil {
			return nil, nil, fmt.Errorf("Query/Governance: %w", err)
		}
		pending = append(pending, res.GetState().GetPendingValidatorPower()...)
		proposals = append(proposals, res.GetState().GetProposals()...)
		next = res.GetPage().GetNextPageKey()
		if len(next) == 0 {
			return pending, proposals, nil
		}
	}
	return nil, nil, fmt.Errorf("Query/Governance: more than %d pages", maxGovernancePages)
}

// checkRetired is the retired-key part of I8.
func checkRetired(kv storetypes.KVStore, auth *authorityv1.AuthorityState, pending []*authorityv1.PendingValidatorPower) error {
	retired := func(pub []byte) bool {
		return kv.Has(append([]byte{authorityRetiredSeat}, pub...))
	}
	for _, seat := range auth.GetDevnetTestSeats() {
		if retired(seat.GetPublicKey()) {
			return fmt.Errorf("I8: seat %s key is retired", seat.GetSeatId())
		}
	}
	for _, row := range auth.GetValidators() {
		if retired(row.GetConsensusPubkey()) {
			return fmt.Errorf("I8: validator %s holds a retired seat key", row.GetNodeId())
		}
	}
	for _, entry := range pending {
		if retired(entry.GetConsensusPubkey()) {
			return fmt.Errorf("I8: pending change %s names a retired seat key", entry.GetProposalId())
		}
	}
	return nil
}

// checkProposals is I9 and I10 over the raw proposal records and the open
// counts.
func checkProposals(records [][]byte, counts map[string]uint64) error {
	want := map[string]uint64{}
	for _, raw := range records {
		var p storedProposal
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("I9: proposal record does not decode: %w", err)
		}
		state := authorityv1.ProposalState(p.State)
		if state != authorityv1.ProposalState_PROPOSAL_STATE_OPEN && state != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			continue
		}
		if len(p.ApproverKeys) != len(p.Approvers) {
			return fmt.Errorf("I9: proposal %s has %d approver keys for %d approvers", p.ProposalID, len(p.ApproverKeys), len(p.Approvers))
		}
		seats, keys := map[string]bool{}, map[string]bool{}
		for i, seat := range p.Approvers {
			if seats[seat] || keys[string(p.ApproverKeys[i])] {
				return fmt.Errorf("I9: proposal %s counts seat %s or its key twice", p.ProposalID, seat)
			}
			seats[seat], keys[string(p.ApproverKeys[i])] = true, true
		}
		if state == authorityv1.ProposalState_PROPOSAL_STATE_QUEUED && len(p.Approvers) < authority.ApprovedThreshold {
			return fmt.Errorf("I9: QUEUED proposal %s has %d approvals, below %d", p.ProposalID, len(p.Approvers), authority.ApprovedThreshold)
		}
		key := append(binary.BigEndian.AppendUint64([]byte{authorityOpenPrefix}, p.BasePolicyVersion), p.Proposer...)
		want[string(key)]++
	}
	if len(counts) != len(want) {
		return fmt.Errorf("I10: %d open-count rows, %d (base version, proposer) pairs with OPEN or QUEUED proposals", len(counts), len(want))
	}
	for _, key := range sortedNames(want) {
		if counts[key] != want[key] {
			return fmt.Errorf("I10: open count %x is %d, want %d", key, counts[key], want[key])
		}
	}
	return nil
}

// i13Skipped are the x/policy rows left out of the I13 digest, as the
// upgrade runner's I13 self-check (chain/app upgrades.go rowClasses) leaves
// them out of its classes: the rows the runner checks itself (the version
// map prefix 0x05, and the exact keys of the layout version, the app
// version, and the plan). The digest also leaves out the applied upgrades
// (prefix 0x07), which the runner protects before its record step and
// which that step then extends by the applied row: the digest compares a
// state before an upgrade with the state after it, where that row is new.
var (
	i13SkippedPrefixes = []byte{policy.VersionMapPrefix, policy.AppliedPrefix}
	i13SkippedKeys     = [][]byte{msgkit.LayoutVersionKey, policy.AppVersionKey(), policy.PlanKey()}
)

// i13PolicyRow reports whether the x/policy row key is in the I13 digest.
func i13PolicyRow(key []byte) bool {
	if bytes.IndexByte(i13SkippedPrefixes, key[0]) >= 0 {
		return false
	}
	for _, exact := range i13SkippedKeys {
		if bytes.Equal(key, exact) {
			return false
		}
	}
	return true
}

// i13Rows encodes the rows the upgrade runner's I13 self-check protects and
// an upgrade's record step leaves as they are, in key order: every
// x/authority row except the exact key {0x04}, then every x/policy row
// i13PolicyRow keeps (the safety caps, governed values, pending changes,
// plan names, and every row under another prefix). Each row is the key
// length (8 bytes big-endian), the key, the value length, the value; each
// store's part is preceded by its name and the part's length, so two states
// share an encoding exactly when every such row is equal.
func i13Rows(ctx sdk.Context, keys map[string]*storetypes.KVStoreKey) []byte {
	var out []byte
	appendPart := func(name string, part []byte) {
		out = binary.BigEndian.AppendUint64(out, uint64(len(name)))
		out = append(out, name...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(part)))
		out = append(out, part...)
	}
	appendRow := func(part []byte, key, value []byte) []byte {
		part = binary.BigEndian.AppendUint64(part, uint64(len(key)))
		part = append(part, key...)
		part = binary.BigEndian.AppendUint64(part, uint64(len(value)))
		return append(part, value...)
	}
	var auth []byte
	it := ctx.KVStore(keys[authority.ModuleName]).Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		if bytes.Equal(it.Key(), []byte{authorityPolicyKey}) {
			continue
		}
		auth = appendRow(auth, it.Key(), it.Value())
	}
	it.Close()
	appendPart(authority.ModuleName, auth)
	var pol []byte
	it = ctx.KVStore(keys[app.ModulePolicy]).Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		if i13PolicyRow(it.Key()) {
			pol = appendRow(pol, it.Key(), it.Value())
		}
	}
	it.Close()
	appendPart(app.ModulePolicy, pol)
	return out
}
