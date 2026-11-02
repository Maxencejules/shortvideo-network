package authority

import (
	"bytes"
	"fmt"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// GenesisPoint is where the chain starts. InitChain passes it so a pending
// genesis entry must lie in the future of the first block.
type GenesisPoint struct {
	InitialHeight      int64
	GenesisUnixSeconds int64
}

// genesisError carries the protocol code of the rule that failed.
func genesisError(code, format string, args ...any) error {
	return fmt.Errorf("authority genesis: %s: %s", code, fmt.Sprintf(format, args...))
}

// ValidateGenesisState checks an authority genesis state. With a nil point it
// checks everything except the start point. Rules, in order:
//
//  1. Shape: the authority state exists; no proposals (the proto has no
//     payload, so a genesis proposal could never execute); no approved
//     council (spec guard_decisions); quorum loss UNSET (DEC-008); seats and
//     submitters well formed; a seat address is a submitter only as
//     BROADCASTER (DR-07-11, stricter than spec).
//  2. Validators: InitialUpdates rules, node ids, and the spec
//     power_set_invariant (spec validate_genesis_view).
//  3. Pending entries: at most MaxPendingChanges, each well formed, distinct
//     proposal ids (distinct ids give distinct PendingKeys).
//  4. Projection of every pending entry on the genesis validators, as Execute
//     projects a new entry (DEC-012: mixed height and time kinds conflict).
//  5. With a point: every entry activates at or after the first block.
func ValidateGenesisState(state *authorityv1.GenesisState, at *GenesisPoint) error {
	auth := state.GetAuthority()
	if auth == nil {
		return genesisError(CodeMalformed, "authority state is missing")
	}
	if len(auth.GetProposals()) != 0 {
		return genesisError(CodeMalformed, "genesis proposals are not supported: the proto has no payload, so they could never execute")
	}
	if len(auth.GetApprovedCouncil()) != 0 {
		return genesisError(CodeForbidden, "approved_council must be empty while DEC-008 is open")
	}
	if auth.GetQuorumLoss() != authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET {
		return genesisError(CodeForbidden, "quorum_loss must be UNSET while DEC-008 is open")
	}
	if err := validateSeats(auth.GetDevnetTestSeats()); err != nil {
		return err
	}
	if err := validateSubmitters(auth.GetSubmitters()); err != nil {
		return err
	}
	if err := validateSeatRoles(auth.GetDevnetTestSeats(), auth.GetSubmitters()); err != nil {
		return err
	}
	if err := validateGenesisValidators(auth.GetValidators()); err != nil {
		return err
	}
	pending := auth.GetPendingValidatorPower()
	if len(pending) > MaxPendingChanges {
		return genesisError(CodeBounds, "%d pending validator changes, more than %d", len(pending), MaxPendingChanges)
	}
	ids := map[string]struct{}{}
	for _, entry := range pending {
		if code := checkPendingEntry(entry); code != "" {
			return genesisError(code, "pending entry %q", entry.GetProposalId())
		}
		if _, dup := ids[entry.GetProposalId()]; dup {
			return genesisError(CodeDuplicate, "pending proposal id %q appears twice", entry.GetProposalId())
		}
		ids[entry.GetProposalId()] = struct{}{}
	}
	if out := projectAll(auth.GetValidators(), pending); !out.Success() {
		return genesisError(out.Code, "pending validator changes do not project on the genesis set")
	}
	if at == nil {
		return nil
	}
	if at.InitialHeight < 1 {
		return genesisError(CodeMalformed, "initial height %d", at.InitialHeight)
	}
	for _, entry := range pending {
		if !activationFuture(entry.GetActivation(), at.InitialHeight-1, at.GenesisUnixSeconds) {
			return genesisError(CodeOutOfRange, "pending entry %q activates before the first block", entry.GetProposalId())
		}
	}
	return nil
}

func validateSeats(seats []*authorityv1.DevnetTestSeat) error {
	if len(seats) != 0 && len(seats) != ApprovedSeats {
		return genesisError(CodeBounds, "%d devnet test seats; the council shape is %d", len(seats), ApprovedSeats)
	}
	ids := map[string]struct{}{}
	keys := map[string]struct{}{}
	for _, seat := range seats {
		if !idPattern.MatchString(seat.GetSeatId()) || len(seat.GetPublicKey()) != 32 {
			return genesisError(CodeBounds, "seat %q", seat.GetSeatId())
		}
		if _, dup := ids[seat.GetSeatId()]; dup {
			return genesisError(CodeDuplicate, "seat id %q appears twice", seat.GetSeatId())
		}
		ids[seat.GetSeatId()] = struct{}{}
		if _, dup := keys[string(seat.GetPublicKey())]; dup {
			return genesisError(CodeDuplicate, "seat key appears twice")
		}
		keys[string(seat.GetPublicKey())] = struct{}{}
	}
	return nil
}

func validateSubmitters(rows []*typesv1.Submitter) error {
	seen := map[string]struct{}{}
	for _, row := range rows {
		if len(row.GetAddress()) != 20 {
			return genesisError(CodeBounds, "submitter address length %d", len(row.GetAddress()))
		}
		if !roleKnown(row.GetRole()) {
			return genesisError(CodeMalformed, "submitter role %v", row.GetRole())
		}
		if _, dup := seen[string(row.GetAddress())]; dup {
			return genesisError(CodeDuplicate, "submitter %x appears twice", row.GetAddress())
		}
		seen[string(row.GetAddress())] = struct{}{}
	}
	return nil
}

// validateSeatRoles fails closed when a seat address is a submitter with
// any role other than BROADCASTER. The row is how a seat key passes the ante;
// AUTHORITY, ADMITTED_SERVICE and ADMITTED_PROVIDER would let a council key
// execute, register content or register a provider. genesis.Validate applies
// the same rule to the document. Interim answer to DR-07-11 (with DR-06-06).
func validateSeatRoles(seats []*authorityv1.DevnetTestSeat, rows []*typesv1.Submitter) error {
	for _, seat := range seats {
		addr := SeatAddress(seat.GetPublicKey())
		for _, row := range rows {
			if bytes.Equal(row.GetAddress(), addr) && row.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER {
				return genesisError(CodeForbidden, "seat %q address is a %v submitter; a seat address may only be BROADCASTER", seat.GetSeatId(), row.GetRole())
			}
		}
	}
	return nil
}

func validateGenesisValidators(rows []*authorityv1.ValidatorPower) error {
	nodes := map[string]struct{}{}
	keys := map[string]struct{}{}
	for _, row := range rows {
		if !idPattern.MatchString(row.GetNodeId()) || len(row.GetConsensusPubkey()) != 32 {
			return genesisError(CodeBounds, "validator %q", row.GetNodeId())
		}
		if _, dup := nodes[row.GetNodeId()]; dup {
			return genesisError(CodeDuplicate, "validator node id %q appears twice", row.GetNodeId())
		}
		nodes[row.GetNodeId()] = struct{}{}
		if _, dup := keys[string(row.GetConsensusPubkey())]; dup {
			return genesisError(CodeDuplicate, "validator key of %q appears twice", row.GetNodeId())
		}
		keys[string(row.GetConsensusPubkey())] = struct{}{}
	}
	// Count, positive power, safe total and the max*3 < total share rule.
	if out := powerInvariant(validatorMap(rows)); !out.Success() {
		return genesisError(out.Code, "validator set fails the power invariant")
	}
	if _, err := InitialUpdates(rows); err != nil {
		return genesisError(CodeBounds, "%v", err)
	}
	return nil
}

// checkPendingEntry returns the code of the first field rule an entry fails.
func checkPendingEntry(entry *authorityv1.PendingValidatorPower) string {
	if !idPattern.MatchString(entry.GetProposalId()) || !idPattern.MatchString(entry.GetNodeId()) {
		return CodeBounds
	}
	if len(entry.GetConsensusPubkey()) != 32 {
		return CodeBounds
	}
	if entry.GetPower() < 0 || entry.GetPower() > safeMax {
		return CodeBounds
	}
	reason := entry.GetReasonDigest()
	if reason.GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {
		return CodeMalformed
	}
	if len(reason.GetValue()) != 32 {
		return CodeBounds
	}
	switch body := entry.GetActivation().GetPoint().(type) {
	case *authorityv1.ActivationPoint_Height:
		if body.Height < 1 || body.Height > uint64(safeMax) {
			return CodeBounds
		}
	case *authorityv1.ActivationPoint_UnixSeconds:
		if body.UnixSeconds < unixMin || body.UnixSeconds > unixMax {
			return CodeBounds
		}
	default:
		return CodeMalformed
	}
	return ""
}

func validatorMap(rows []*authorityv1.ValidatorPower) map[string]projected {
	out := make(map[string]projected, len(rows))
	for _, row := range rows {
		out[row.GetNodeId()] = projected{pub: bytes.Clone(row.GetConsensusPubkey()), power: row.GetPower()}
	}
	return out
}
