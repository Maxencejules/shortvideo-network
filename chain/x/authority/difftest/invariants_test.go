package difftest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// safeMax is bounds.json safe_integer_max.
const safeMax int64 = 9007199254740991

// checkInvariants checks the council invariants of plan section 10 that a
// keeper-level harness can see without x/policy or the app (I3-I6 and I13
// need them): I1 in its exact form, I2, I7, I8, I9, I10, and I11. It reads
// committed state at ctx and returns the first violation.
func (h *harness) checkInvariants(ctx sdk.Context) error {
	return h.checkCommitted(ctx, h.read(ctx))
}

// checkCommitted is checkInvariants over a read of the committed state.
func (h *harness) checkCommitted(ctx sdk.Context, c *committed) error {
	if v := c.version; v != 1+h.executes {
		return fmt.Errorf("I1: policy version %d, want 1 + %d successful executes", v, h.executes)
	}
	store := ctx.KVStore(h.keys[0])
	var err error
	eachRow(store, prefixGoverned, func(key, value []byte) {
		ceiling, known := msgkit.Ceiling(string(key[1:]))
		if len(value) != 8 {
			err = fmt.Errorf("I2: governed row %q holds %d bytes", key[1:], len(value))
			return
		}
		got := binary.BigEndian.Uint64(value)
		if err == nil && (!known || got < 1 || got > ceiling) {
			err = fmt.Errorf("I2: governed row %q=%d outside the msgkit table", key[1:], got)
		}
	})
	if err != nil {
		return err
	}
	if err := h.checkConfig(c.cfg); err != nil {
		return err
	}
	if err := checkSeats(store, c.cfg, c.gov); err != nil {
		return err
	}
	if err := h.checkProposals(ctx, c.records); err != nil {
		return err
	}
	return checkPowerSets(c.gov.GetValidators(), c.gov.GetPendingValidatorPower())
}

// checkConfig is I7: decision rows, devnet rows, approved council and
// quorum loss equal genesis; the seats keep their ids.
func (h *harness) checkConfig(cfg *authorityv1.GenesisState) error {
	gen := h.genesisConfig
	if len(cfg.GetBlockedDecisions()) != len(gen.GetBlockedDecisions()) {
		return fmt.Errorf("I7: %d decision rows, genesis has %d", len(cfg.GetBlockedDecisions()), len(gen.GetBlockedDecisions()))
	}
	for i, row := range cfg.GetBlockedDecisions() {
		if !proto.Equal(row, gen.GetBlockedDecisions()[i]) || row.GetApproved() || row.GetValueVersion() != 0 {
			return fmt.Errorf("I7: decision row %s changed or approved", row.GetDecisionId())
		}
	}
	if len(cfg.GetDevnetTestParameters()) != len(gen.GetDevnetTestParameters()) {
		return fmt.Errorf("I7: devnet rows changed")
	}
	for i, row := range cfg.GetDevnetTestParameters() {
		if !proto.Equal(row, gen.GetDevnetTestParameters()[i]) {
			return fmt.Errorf("I7: devnet row %s changed", row.GetDecisionId())
		}
	}
	auth := cfg.GetAuthority()
	if len(auth.GetApprovedCouncil()) != 0 || auth.GetQuorumLoss() != authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET {
		return fmt.Errorf("I7: approved council %d, quorum loss %v", len(auth.GetApprovedCouncil()), auth.GetQuorumLoss())
	}
	if len(auth.GetProposals()) != 0 || len(auth.GetPendingValidatorPower()) != 0 {
		return fmt.Errorf("I7: the config key holds proposals or pending entries")
	}
	return nil
}

// checkSeats is I8: exactly four seats with distinct keys, none retired,
// none a current or pending validator key; no validator key a retired seat
// key; no seat account a submitter.
func checkSeats(store interface {
	Has([]byte) bool
}, cfg *authorityv1.GenesisState, gov *authorityv1.AuthorityState) error {
	seats := cfg.GetAuthority().GetDevnetTestSeats()
	if len(seats) != authority.ApprovedSeats {
		return fmt.Errorf("I8: %d seats", len(seats))
	}
	consensus := map[string]bool{}
	for _, row := range gov.GetValidators() {
		consensus[string(row.GetConsensusPubkey())] = true
		if store.Has(append([]byte{prefixRetired}, row.GetConsensusPubkey()...)) {
			return fmt.Errorf("I8: validator %s holds a retired seat key", row.GetNodeId())
		}
	}
	for _, item := range gov.GetPendingValidatorPower() {
		consensus[string(item.GetConsensusPubkey())] = true
		if store.Has(append([]byte{prefixRetired}, item.GetConsensusPubkey()...)) {
			return fmt.Errorf("I8: pending change %s names a retired seat key", item.GetProposalId())
		}
	}
	keys := map[string]bool{}
	for _, seat := range seats {
		pub := seat.GetPublicKey()
		if keys[string(pub)] {
			return fmt.Errorf("I8: two seats share a key")
		}
		keys[string(pub)] = true
		if consensus[string(pub)] {
			return fmt.Errorf("I8: seat %s key is a validator consensus key", seat.GetSeatId())
		}
		if store.Has(append([]byte{prefixRetired}, pub...)) {
			return fmt.Errorf("I8: seat %s key is retired", seat.GetSeatId())
		}
		addr := authority.SeatAddress(pub)
		for _, row := range cfg.GetAuthority().GetSubmitters() {
			if bytes.Equal(row.GetAddress(), addr) {
				return fmt.Errorf("I8: seat %s account is a submitter", seat.GetSeatId())
			}
		}
	}
	return nil
}

// checkProposals is I9 and I10: every OPEN or QUEUED proposal has distinct
// approver seat ids whose keys are their seats' keys, a QUEUED one has at
// least the threshold, and the open counts (0x14) equal the OPEN and QUEUED
// proposals per (base version, proposer). The proposer each record names is
// the seat that proposed it.
func (h *harness) checkProposals(ctx sdk.Context, records []storedRecord) error {
	want := map[string]uint64{}
	for _, rec := range records {
		state := authorityv1.ProposalState(rec.State)
		if proposer, ok := h.proposers[rec.ProposalID]; !ok || proposer != rec.Proposer {
			return fmt.Errorf("I10: proposal %s names proposer %q, the harness saw %q", rec.ProposalID, rec.Proposer, proposer)
		}
		if len(rec.ApproverKeys) != len(rec.Approvers) {
			return fmt.Errorf("I9: proposal %s has %d approver keys for %d approvers", rec.ProposalID, len(rec.ApproverKeys), len(rec.Approvers))
		}
		seen := map[string]bool{}
		for _, id := range rec.Approvers {
			if seen[id] {
				return fmt.Errorf("I9: proposal %s counts seat %s twice", rec.ProposalID, id)
			}
			seen[id] = true
		}
		for _, key := range rec.ApproverKeys {
			if !seen[seatOfKey(key)] {
				return fmt.Errorf("I9: proposal %s approver key %x is not an approving seat's key", rec.ProposalID, key)
			}
		}
		switch state {
		case authorityv1.ProposalState_PROPOSAL_STATE_QUEUED:
			if len(rec.Approvers) < authority.ApprovedThreshold {
				return fmt.Errorf("I9: QUEUED proposal %s has %d approvals", rec.ProposalID, len(rec.Approvers))
			}
			fallthrough
		case authorityv1.ProposalState_PROPOSAL_STATE_OPEN:
			want[openKey(rec.Proposer, rec.BasePolicyVersion)]++
		}
	}
	got := map[string]uint64{}
	var err error
	eachRow(ctx.KVStore(h.keys[0]), prefixOpen, func(key, value []byte) {
		if len(value) != 8 || binary.BigEndian.Uint64(value) == 0 {
			err = fmt.Errorf("I10: open count %x holds %x", key, value)
			return
		}
		got[string(key)] = binary.BigEndian.Uint64(value)
	})
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return fmt.Errorf("I10: %d open-count rows, %d expected", len(got), len(want))
	}
	for key, n := range want {
		if got[key] != n {
			return fmt.Errorf("I10: open count %x is %d, want %d", key, got[key], n)
		}
	}
	return nil
}

// openKey is 0x14 || base(8, big-endian) || seat id.
func openKey(seat string, base uint64) string {
	return string(append(binary.BigEndian.AppendUint64([]byte{prefixOpen}, base), seat...))
}

// checkPowerSets is I11: the validator set meets DEC-011 (spec
// power_set_invariant) with distinct keys, the pending changes share one
// activation kind (DEC-012), and the set meets DEC-011 after every
// activation point of the pending changes, applied in store order.
func checkPowerSets(validators []*authorityv1.ValidatorPower, pending []*authorityv1.PendingValidatorPower) error {
	set := map[string]int64{}
	keys := map[string]string{}
	for _, row := range validators {
		set[row.GetNodeId()] = row.GetPower()
		if other, dup := keys[string(row.GetConsensusPubkey())]; dup {
			return fmt.Errorf("I11: validators %s and %s share a key", other, row.GetNodeId())
		}
		keys[string(row.GetConsensusPubkey())] = row.GetNodeId()
	}
	if err := powerInvariant(set); err != nil {
		return fmt.Errorf("I11: current set: %w", err)
	}
	kind := func(item *authorityv1.PendingValidatorPower) (bool, uint64) {
		if point, ok := item.GetActivation().GetPoint().(*authorityv1.ActivationPoint_Height); ok {
			return true, point.Height
		}
		return false, uint64(item.GetActivation().GetUnixSeconds())
	}
	for i := 0; i < len(pending); {
		byHeight, point := kind(pending[i])
		if first, _ := kind(pending[0]); first != byHeight {
			return fmt.Errorf("I11: pending changes mix height and time activations")
		}
		for ; i < len(pending); i++ {
			itemHeight, itemPoint := kind(pending[i])
			if itemHeight != byHeight || itemPoint != point {
				break
			}
			if pending[i].GetPower() == 0 {
				delete(set, pending[i].GetNodeId())
			} else {
				set[pending[i].GetNodeId()] = pending[i].GetPower()
			}
		}
		if err := powerInvariant(set); err != nil {
			return fmt.Errorf("I11: after activation point %d: %w", point, err)
		}
	}
	return nil
}

// powerInvariant is spec power_set_invariant.
func powerInvariant(set map[string]int64) error {
	if len(set) < 4 {
		return fmt.Errorf("%d validators", len(set))
	}
	powers := make([]int64, 0, len(set))
	var total int64
	for _, p := range set {
		if p <= 0 || p > safeMax-total {
			return fmt.Errorf("power %d", p)
		}
		total += p
		powers = append(powers, p)
	}
	if slices.Max(powers)*3 >= total {
		return fmt.Errorf("max power %d of total %d", slices.Max(powers), total)
	}
	return nil
}
