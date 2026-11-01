package authority

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// SeatAddress is the 20-byte account of an ed25519 council key.
// CometBFT and the SDK both use the first 20 bytes of SHA-256.
func SeatAddress(pub []byte) []byte {
	if len(pub) != 32 {
		return nil
	}
	return cmted25519.PubKey(pub).Address()
}

// State is the authority working set. Liveness is not a field.
type State struct {
	ChainID       string
	Height        int64
	Time          int64
	PolicyVersion uint64
	Validators    []*authorityv1.ValidatorPower
	Submitters    []*typesv1.Submitter
	Seats         []*authorityv1.DevnetTestSeat
	Proposals     []*authorityv1.Proposal
	Pending       []*authorityv1.PendingValidatorPower
	Params        []*typesv1.DevnetTestParameter
	Payloads      map[string]string
	Business      map[string][]byte
	Executed      map[string][]byte
	Providers     map[string]*ProviderRecord
	Governed      map[string]uint64
	PrevCouncil   [][]byte
	PrevService   [][]byte
	PrevProvider  map[string][][]byte
}

// ProviderRecord is the admission row authority keeps until Goal 07 owns
// the registry. It is not a capacity ledger.
type ProviderRecord struct {
	ID            string
	Account       []byte
	SigningPub    []byte
	SigningKeyID  string
	Generation    uint64
	State         string
	OperatorGroup string
	Endpoint      []byte
	Capacity      uint64
}

func (s *State) clone() *State {
	out := *s
	out.Validators = cloneValidators(s.Validators)
	out.Submitters = cloneSubmitters(s.Submitters)
	out.Seats = cloneSeats(s.Seats)
	out.Proposals = cloneProposals(s.Proposals)
	out.Pending = clonePending(s.Pending)
	out.Params = cloneParams(s.Params)
	out.Payloads = cloneStringMap(s.Payloads)
	out.Business = cloneBytesMap(s.Business)
	out.Executed = cloneBytesMap(s.Executed)
	out.Providers = cloneProviders(s.Providers)
	out.Governed = cloneUintMap(s.Governed)
	out.PrevCouncil = cloneBytesList(s.PrevCouncil)
	out.PrevService = cloneBytesList(s.PrevService)
	out.PrevProvider = cloneBytesListMap(s.PrevProvider)
	return &out
}

func (s *State) devnet(id string) *typesv1.DevnetTestParameter {
	if !strings.HasPrefix(s.ChainID, "shortvideo-dev-") {
		return nil
	}
	for _, row := range s.Params {
		if row.GetDecisionId() == id && row.GetLabel() == "PROPOSED_TEST_PARAMETER" && !row.GetApprovedPolicy() {
			return row
		}
	}
	return nil
}

func (s *State) council() Outcome {
	param := s.devnet("DEC-008")
	if param == nil || param.GetNumericValue() != ApprovedThreshold || param.GetDelaySeconds() < 0 {
		return fail(CodeDecisionBlocked)
	}
	if len(s.Seats) != ApprovedSeats {
		return fail(CodeDecisionBlocked)
	}
	return ok()
}

func (s *State) seatPub(signer []byte) []byte {
	for _, seat := range s.Seats {
		if bytes.Equal(SeatAddress(seat.GetPublicKey()), signer) {
			return seat.GetPublicKey()
		}
	}
	return nil
}

func (s *State) role(signer []byte) typesv1.SubmitterRole {
	for _, row := range s.Submitters {
		if bytes.Equal(row.GetAddress(), signer) {
			return row.GetRole()
		}
	}
	return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
}

func (s *State) proposal(id string) *authorityv1.Proposal {
	for _, row := range s.Proposals {
		if row.GetProposalId() == id {
			return row
		}
	}
	return nil
}

func (s *State) business(id string, digest []byte) Outcome {
	if !idPattern.MatchString(id) {
		if id == "" {
			return fail(CodeMalformed)
		}
		return fail(CodeBounds)
	}
	if digest == nil {
		return fail(CodeMalformed)
	}
	prev, okHave := s.Business[id]
	if !okHave {
		return ok()
	}
	if bytes.Equal(prev, digest) {
		return Outcome{Idempotent: true}
	}
	return fail(CodeReplay)
}

func (s *State) commitBusiness(id string, digest []byte) {
	if s.Business == nil {
		s.Business = map[string][]byte{}
	}
	s.Business[id] = append([]byte(nil), digest...)
}

// ProposeChange opens one proposal. The signer must be a devnet test seat.
func (s *State) ProposeChange(msg *authorityv1.MsgProposeChange) Outcome {
	if ready := s.council(); !ready.Success() {
		return ready
	}
	if msg == nil || len(msg.GetSigner()) != 20 {
		return fail(CodeMalformed)
	}
	pub := s.seatPub(msg.GetSigner())
	if pub == nil {
		return fail(CodeUnauthorized)
	}
	if !idPattern.MatchString(msg.GetProposalId()) || !idPattern.MatchString(msg.GetParameterKey()) {
		return fail(CodeBounds)
	}
	if strings.HasPrefix(msg.GetParameterKey(), "dec-") {
		return fail(CodeForbidden)
	}
	change, kind, canon, parsed := CanonicalFromProto(msg.GetPayload(), msg.GetPayloadDigest().GetValue())
	if !parsed.Success() {
		return parsed
	}
	if msg.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {
		return fail(CodeMalformed)
	}
	wantKey := parameterKey(kind, change.NodeID, limitKey(msg), providerID(msg), contentID(msg))
	if msg.GetParameterKey() != wantKey {
		return fail(CodeMalformed)
	}
	if msg.GetExpiryUnixSeconds() < unixMin || msg.GetExpiryUnixSeconds() > unixMax {
		return fail(CodeBounds)
	}
	if kind == "validator_power" && !activationFuture(change, s.Height, s.Time) {
		return fail(CodeOutOfRange)
	}
	digest := businessDigest(map[string]any{
		"base_policy_version": strconv.FormatUint(msg.GetBasePolicyVersion(), 10),
		"expiry_unix_seconds": strconv.FormatInt(msg.GetExpiryUnixSeconds(), 10),
		"parameter_key":       msg.GetParameterKey(),
		"payload":             canon,
		"proposal_id":         msg.GetProposalId(),
	})
	gate := s.business(msg.GetBusinessId(), digest)
	if !gate.Success() || gate.Idempotent {
		return gate
	}
	if s.proposal(msg.GetProposalId()) != nil {
		return fail(CodeDuplicate)
	}
	if msg.GetExpiryUnixSeconds() <= s.Time {
		return fail(CodeExpired)
	}
	if msg.GetBasePolicyVersion() != s.PolicyVersion {
		return fail(CodeStale)
	}
	if len(s.Proposals) >= MaxRecords {
		return fail(CodeBounds)
	}
	if s.Payloads == nil {
		s.Payloads = map[string]string{}
	}
	s.Payloads[msg.GetProposalId()] = canon
	s.Proposals = append(s.Proposals, &authorityv1.Proposal{
		ProposalId:        msg.GetProposalId(),
		ParameterKey:      msg.GetParameterKey(),
		BasePolicyVersion: msg.GetBasePolicyVersion(),
		PayloadDigest:     msg.GetPayloadDigest(),
		State:             authorityv1.ProposalState_PROPOSAL_STATE_OPEN,
		ExpiryUnixSeconds: msg.GetExpiryUnixSeconds(),
		BusinessId:        msg.GetBusinessId(),
	})
	s.commitBusiness(msg.GetBusinessId(), digest)
	return ok()
}

// ApproveChange records one seat. The same seat again is a replay.
func (s *State) ApproveChange(msg *authorityv1.MsgApproveChange) Outcome {
	if ready := s.council(); !ready.Success() {
		return ready
	}
	if msg == nil || len(msg.GetSigner()) != 20 || !idPattern.MatchString(msg.GetProposalId()) {
		return fail(CodeMalformed)
	}
	pub := s.seatPub(msg.GetSigner())
	if pub == nil {
		return fail(CodeUnauthorized)
	}
	digest := businessDigest(map[string]any{
		"proposal_id": msg.GetProposalId(),
		"signer":      hex.EncodeToString(msg.GetSigner()),
	})
	gate := s.business(msg.GetBusinessId(), digest)
	if !gate.Success() || gate.Idempotent {
		return gate
	}
	proposal := s.proposal(msg.GetProposalId())
	if proposal == nil {
		return fail(CodeNotFound)
	}
	if proposal.GetState() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
		return fail(CodeConflict)
	}
	if s.Time >= proposal.GetExpiryUnixSeconds() {
		proposal.State = authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED
		return keep(CodeExpired)
	}
	for _, have := range proposal.GetApproverKeys() {
		if bytes.Equal(have, pub) {
			return fail(CodeReplay)
		}
	}
	proposal.ApproverKeys = append(proposal.ApproverKeys, append([]byte(nil), pub...))
	slices.SortFunc(proposal.ApproverKeys, bytes.Compare)
	if len(proposal.ApproverKeys) >= ApprovedThreshold {
		proposal.State = authorityv1.ProposalState_PROPOSAL_STATE_QUEUED
		proposal.ExecuteAfterUnixSeconds = s.Time + s.devnet("DEC-008").GetDelaySeconds()
	}
	s.commitBusiness(msg.GetBusinessId(), digest)
	return ok()
}

// ExecuteChange applies or schedules the stored payload.
func (s *State) ExecuteChange(msg *authorityv1.MsgExecuteChange) Outcome {
	if msg == nil || len(msg.GetSigner()) != 20 {
		return fail(CodeMalformed)
	}
	if s.role(msg.GetSigner()) != typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY {
		if s.role(msg.GetSigner()) == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			return fail(CodeUnauthorized)
		}
		return fail(CodeForbidden)
	}
	if ready := s.council(); !ready.Success() {
		return ready
	}
	if !idPattern.MatchString(msg.GetProposalId()) {
		return fail(CodeBounds)
	}
	digest := businessDigest(map[string]any{"proposal_id": msg.GetProposalId()})
	gate := s.business(msg.GetBusinessId(), digest)
	if !gate.Success() || gate.Idempotent {
		return gate
	}
	proposal := s.proposal(msg.GetProposalId())
	if proposal == nil {
		return fail(CodeNotFound)
	}
	if s.Time >= proposal.GetExpiryUnixSeconds() {
		proposal.State = authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED
		return keep(CodeExpired)
	}
	if proposal.GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		return fail(CodeConflict)
	}
	if s.Time < proposal.GetExecuteAfterUnixSeconds() {
		return fail(CodeOutOfRange)
	}
	if proposal.GetBasePolicyVersion() != s.PolicyVersion {
		prev := s.Executed[proposal.GetParameterKey()]
		if prev != nil && !bytes.Equal(prev, proposal.GetPayloadDigest().GetValue()) {
			return fail(CodeConflict)
		}
		return fail(CodeStale)
	}
	applied := s.applyPayload(proposal)
	if !applied.Success() {
		if applied.Code == CodeForbidden {
			proposal.State = authorityv1.ProposalState_PROPOSAL_STATE_REJECTED
			return keep(CodeForbidden)
		}
		delete(s.Business, msg.GetBusinessId())
		return applied
	}
	if s.Executed == nil {
		s.Executed = map[string][]byte{}
	}
	s.Executed[proposal.GetParameterKey()] = append([]byte(nil), proposal.GetPayloadDigest().GetValue()...)
	s.PolicyVersion++
	proposal.State = authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED
	s.commitBusiness(msg.GetBusinessId(), digest)
	return ok()
}

func (s *State) applyPayload(proposal *authorityv1.Proposal) Outcome {
	canon := s.Payloads[proposal.GetProposalId()]
	if canon == "" || !digestMatches(canon, proposal.GetPayloadDigest().GetValue()) {
		return fail(CodeIntegrity)
	}
	// The stored canonical form is the authority record. Re-parse is bounded.
	if _, out := ParsePayloadJSON([]byte(canon)); !out.Success() {
		return out
	}
	kind := payloadKind(canon)
	switch kind {
	case "validatorPower":
		change, _, out := validatorFromCanonical(canon)
		if !out.Success() {
			return out
		}
		if !activationFuture(change, s.Height, s.Time) {
			return fail(CodeOutOfRange)
		}
		pending := &authorityv1.PendingValidatorPower{
			ProposalId:      proposal.GetProposalId(),
			ConsensusPubkey: change.PubKey,
			Power:           change.Power,
			NodeId:          change.NodeID,
			ReasonDigest:    &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: change.Reason},
			Activation:      activationPoint(change),
		}
		if out := s.project(pending); !out.Success() {
			return out
		}
		if len(s.Pending) >= MaxRecords {
			return fail(CodeBounds)
		}
		s.Pending = append(s.Pending, pending)
		return ok()
	case "governedLimit":
		return s.applyGoverned(canon)
	case "admitProvider":
		return s.admit(providerFrom(canon))
	case "disableProvider":
		return s.disable(providerFrom(canon))
	case "forceWithdraw":
		return fail(CodeNotFound)
	default:
		return fail(CodeMalformed)
	}
}

func (s *State) project(extra *authorityv1.PendingValidatorPower) Outcome {
	items := append(clonePending(s.Pending), extra)
	kinds := map[string]struct{}{}
	for _, item := range items {
		kinds[activationKind(item.GetActivation())] = struct{}{}
	}
	if len(kinds) > 1 {
		return fail(CodeConflict)
	}
	slices.SortFunc(items, func(a, b *authorityv1.PendingValidatorPower) int {
		if cmp := strings.Compare(activationKind(a.GetActivation()), activationKind(b.GetActivation())); cmp != 0 {
			return cmp
		}
		if ap, bp := activationValue(a.GetActivation()), activationValue(b.GetActivation()); ap != bp {
			if ap < bp {
				return -1
			}
			return 1
		}
		return strings.Compare(a.GetProposalId(), b.GetProposalId())
	})
	vals := map[string]*authorityv1.ValidatorPower{}
	for _, row := range s.Validators {
		vals[row.GetNodeId()] = &authorityv1.ValidatorPower{
			ConsensusPubkey: append([]byte(nil), row.GetConsensusPubkey()...),
			Power:           row.GetPower(),
			NodeId:          row.GetNodeId(),
		}
	}
	for index := 0; index < len(items); {
		point := activationValue(items[index].GetActivation())
		kind := activationKind(items[index].GetActivation())
		var group []*authorityv1.PendingValidatorPower
		for index < len(items) && activationKind(items[index].GetActivation()) == kind && activationValue(items[index].GetActivation()) == point {
			group = append(group, items[index])
			index++
		}
		seenNode := map[string]struct{}{}
		seenKey := map[string]struct{}{}
		for _, item := range group {
			if _, ok := seenNode[item.GetNodeId()]; ok {
				return fail(CodeConflict)
			}
			seenNode[item.GetNodeId()] = struct{}{}
			if item.GetPower() == 0 {
				current := vals[item.GetNodeId()]
				if current == nil || !bytes.Equal(current.GetConsensusPubkey(), item.GetConsensusPubkey()) {
					return fail(CodeConflict)
				}
			}
			key := string(item.GetConsensusPubkey())
			if _, ok := seenKey[key]; ok {
				return fail(CodeConflict)
			}
			seenKey[key] = struct{}{}
		}
		for _, item := range group {
			if item.GetPower() > 0 {
				for id, row := range vals {
					if id != item.GetNodeId() && bytes.Equal(row.GetConsensusPubkey(), item.GetConsensusPubkey()) {
						return fail(CodeDuplicate)
					}
				}
				vals[item.GetNodeId()] = &authorityv1.ValidatorPower{
					ConsensusPubkey: append([]byte(nil), item.GetConsensusPubkey()...),
					Power:           item.GetPower(),
					NodeId:          item.GetNodeId(),
				}
				continue
			}
			current := vals[item.GetNodeId()]
			if current == nil {
				return fail(CodeNotFound)
			}
			if !bytes.Equal(current.GetConsensusPubkey(), item.GetConsensusPubkey()) {
				return fail(CodeConflict)
			}
			delete(vals, item.GetNodeId())
		}
		if out := powerInvariant(vals); !out.Success() {
			return out
		}
	}
	return ok()
}

// ApplyDue applies validator changes whose activation point has been reached.
// A removal whose key does not match halts: the caller must not save.
func (s *State) ApplyDue() (before []*authorityv1.ValidatorPower, changed bool, out Outcome) {
	var due []*authorityv1.PendingValidatorPower
	var wait []*authorityv1.PendingValidatorPower
	for _, item := range s.Pending {
		if activationDue(item.GetActivation(), s.Height, s.Time) {
			due = append(due, item)
		} else {
			wait = append(wait, item)
		}
	}
	if len(due) == 0 {
		return nil, false, ok()
	}
	slices.SortFunc(due, func(a, b *authorityv1.PendingValidatorPower) int {
		if ap, bp := activationValue(a.GetActivation()), activationValue(b.GetActivation()); ap != bp {
			if ap < bp {
				return -1
			}
			return 1
		}
		return strings.Compare(a.GetProposalId(), b.GetProposalId())
	})
	before = cloneValidators(s.Validators)
	working := cloneValidators(s.Validators)
	find := func(id string) *authorityv1.ValidatorPower {
		for _, row := range working {
			if row.GetNodeId() == id {
				return row
			}
		}
		return nil
	}
	for _, item := range due {
		if item.GetPower() == 0 {
			current := find(item.GetNodeId())
			if current == nil {
				return before, false, fail(CodeNotFound)
			}
			if !bytes.Equal(current.GetConsensusPubkey(), item.GetConsensusPubkey()) {
				// Halt. The working copy is discarded. The pending entry stays.
				return before, false, fail(CodeConflict)
			}
			working = slices.DeleteFunc(working, func(row *authorityv1.ValidatorPower) bool {
				return row.GetNodeId() == item.GetNodeId()
			})
			continue
		}
		if current := find(item.GetNodeId()); current == nil {
			working = append(working, &authorityv1.ValidatorPower{
				ConsensusPubkey: append([]byte(nil), item.GetConsensusPubkey()...),
				Power:           item.GetPower(),
				NodeId:          item.GetNodeId(),
				ReasonDigest:    item.GetReasonDigest(),
			})
		} else {
			current.ConsensusPubkey = append([]byte(nil), item.GetConsensusPubkey()...)
			current.Power = item.GetPower()
			current.ReasonDigest = item.GetReasonDigest()
		}
	}
	vals := map[string]*authorityv1.ValidatorPower{}
	for _, row := range working {
		vals[row.GetNodeId()] = row
	}
	if out := powerInvariant(vals); !out.Success() {
		return before, false, out
	}
	s.Validators = working
	s.Pending = wait
	slices.SortFunc(s.Validators, func(a, b *authorityv1.ValidatorPower) int {
		return bytes.Compare(a.GetConsensusPubkey(), b.GetConsensusPubkey())
	})
	return before, true, ok()
}

func (s *State) validator(id string) *authorityv1.ValidatorPower {
	for _, row := range s.Validators {
		if row.GetNodeId() == id {
			return row
		}
	}
	return nil
}

func powerInvariant(vals map[string]*authorityv1.ValidatorPower) Outcome {
	if len(vals) < ApprovedSeats {
		return fail(CodeForbidden)
	}
	var total int64
	var maxPower int64
	seen := map[string]struct{}{}
	for _, row := range vals {
		if row.GetPower() <= 0 {
			return fail(CodeForbidden)
		}
		key := string(row.GetConsensusPubkey())
		if _, ok := seen[key]; ok {
			return fail(CodeDuplicate)
		}
		seen[key] = struct{}{}
		if row.GetPower() > safeMax-total {
			return fail(CodeOverflow)
		}
		total += row.GetPower()
		if row.GetPower() > maxPower {
			maxPower = row.GetPower()
		}
	}
	if total > safeMax || maxPower*3 >= total {
		return fail(CodeForbidden)
	}
	return ok()
}

// PowerQuorum reports whether the online power is more than two thirds.
// It is not consensus state. Callers pass the offline set from outside.
func PowerQuorum(powers []int64, offline []bool) bool {
	if len(powers) != len(offline) {
		return false
	}
	var total, available int64
	for i, power := range powers {
		if power <= 0 {
			return false
		}
		if power > safeMax-total {
			return false
		}
		total += power
		if !offline[i] {
			available += power
		}
	}
	if total <= 0 || total > safeMax {
		return false
	}
	return available*3 > total*2
}

func (s *State) RegisterProvider(signer []byte, id string, pub []byte, keyID string, group string, capacity uint64, endpoint []byte) Outcome {
	if s.role(signer) != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER {
		if s.role(signer) == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			return fail(CodeUnauthorized)
		}
		return fail(CodeForbidden)
	}
	if !idPattern.MatchString(id) || !idPattern.MatchString(keyID) || len(pub) != 32 || len(endpoint) != 32 || capacity < 1 {
		return fail(CodeBounds)
	}
	if s.Providers[id] != nil {
		return fail(CodeDuplicate)
	}
	if len(s.Providers) >= MaxRecords {
		return fail(CodeBounds)
	}
	if s.Providers == nil {
		s.Providers = map[string]*ProviderRecord{}
	}
	s.Providers[id] = &ProviderRecord{
		ID: id, Account: append([]byte(nil), signer...), SigningPub: append([]byte(nil), pub...),
		SigningKeyID: keyID, Generation: 1, State: "APPLIED", OperatorGroup: group,
		Endpoint: append([]byte(nil), endpoint...), Capacity: capacity,
	}
	return ok()
}

func (s *State) admit(id string) Outcome {
	row := s.Providers[id]
	if row == nil {
		return fail(CodeNotFound)
	}
	param := s.devnet("DEC-002")
	if param == nil || param.GetRoleName() != "DEVNET_TEST_ONLY" {
		return fail(CodeDecisionBlocked)
	}
	if row.OperatorGroup != "devnet-test-only" || row.State != "APPLIED" {
		return fail(CodeForbidden)
	}
	row.State = "ADMITTED"
	return ok()
}

func (s *State) disable(id string) Outcome {
	row := s.Providers[id]
	if row == nil {
		return fail(CodeNotFound)
	}
	if row.State != "ADMITTED" && row.State != "DRAINING" {
		return fail(CodeConflict)
	}
	row.State = "DISABLED"
	return ok()
}

func (s *State) applyGoverned(canon string) Outcome {
	// Caps are the protocol ceilings. A value above its own ceiling is bounds.
	caps := map[string]uint64{
		"max_evidence_batch": 32,
		"max_page_limit":     100,
		"max_open_offers":    1024,
		"max_expiry_steps":   128,
		"max_package_bytes":  268435456,
	}
	// The canonical object is small. Pull the two fields by a second parse
	// through the known shape stored at propose time.
	key, value, out := governedFields(canon)
	if !out.Success() {
		return out
	}
	ceiling, known := caps[key]
	if !known || value > ceiling || value < 1 {
		return fail(CodeBounds)
	}
	if s.Governed == nil {
		s.Governed = map[string]uint64{}
	}
	s.Governed[key] = value
	return ok()
}

// RotateProviderKey changes the provider signing key only.
func (s *State) RotateProviderKey(signer []byte, id, newKeyID string, newPub, sig []byte, generation uint64) Outcome {
	row := s.Providers[id]
	if row == nil {
		return fail(CodeNotFound)
	}
	if !bytes.Equal(row.Account, signer) {
		return fail(CodeForbidden)
	}
	if !idPattern.MatchString(newKeyID) || len(newPub) != 32 || len(sig) != 64 {
		return fail(CodeBounds)
	}
	canon, err := jcs.Canonicalize(map[string]any{
		"expectedGeneration": strconv.FormatUint(generation, 10),
		"newSigningKeyId":    newKeyID,
		"newSigningPubkey":   hex.EncodeToString(newPub),
		"providerId":         id,
	})
	if err != nil {
		return fail(CodeMalformed)
	}
	msg := append(append([]byte(rotateDomain), 0), []byte(canon)...)
	if !ed25519.Verify(row.SigningPub, msg, sig) {
		return fail(CodeUnauthorized)
	}
	if generation != row.Generation {
		return fail(CodeStale)
	}
	if newKeyID == row.SigningKeyID || bytes.Equal(newPub, row.SigningPub) {
		return fail(CodeConflict)
	}
	if s.PrevProvider == nil {
		s.PrevProvider = map[string][][]byte{}
	}
	s.PrevProvider[id] = append(s.PrevProvider[id], append([]byte(nil), row.SigningPub...))
	row.SigningPub = append([]byte(nil), newPub...)
	row.SigningKeyID = newKeyID
	row.Generation++
	return ok()
}

// RotateCouncilSeat replaces one council key. It does not touch consensus
// keys or provider signing keys. There is no single-signature message for it.
func (s *State) RotateCouncilSeat(seatID string, newPub []byte) Outcome {
	if !idPattern.MatchString(seatID) || len(newPub) != 32 {
		return fail(CodeBounds)
	}
	var seat *authorityv1.DevnetTestSeat
	for _, row := range s.Seats {
		if row.GetSeatId() == seatID {
			seat = row
		}
		if bytes.Equal(row.GetPublicKey(), newPub) && row.GetSeatId() != seatID {
			return fail(CodeDuplicate)
		}
	}
	if seat == nil {
		return fail(CodeNotFound)
	}
	s.PrevCouncil = append(s.PrevCouncil, append([]byte(nil), seat.GetPublicKey()...))
	seat.PublicKey = append([]byte(nil), newPub...)
	return ok()
}

// RotateServiceSubmitter replaces an admitted service account. The old
// address is retained and is not a consensus key.
func (s *State) RotateServiceSubmitter(oldAddr, newAddr []byte) Outcome {
	if len(oldAddr) != 20 || len(newAddr) != 20 || bytes.Equal(oldAddr, newAddr) {
		return fail(CodeBounds)
	}
	var found *typesv1.Submitter
	for _, row := range s.Submitters {
		if bytes.Equal(row.GetAddress(), newAddr) {
			return fail(CodeDuplicate)
		}
		if bytes.Equal(row.GetAddress(), oldAddr) {
			found = row
		}
	}
	if found == nil || found.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE {
		return fail(CodeNotFound)
	}
	s.PrevService = append(s.PrevService, append([]byte(nil), oldAddr...))
	found.Address = append([]byte(nil), newAddr...)
	return ok()
}

func activationFuture(change PowerChange, height, unix int64) bool {
	if change.ByHeight {
		return int64(change.Height) > height
	}
	return change.Unix > unix
}

func activationDue(point *authorityv1.ActivationPoint, height, unix int64) bool {
	if point.GetHeight() != 0 && point.GetUnixSeconds() == 0 && point.GetPoint() != nil {
		if _, ok := point.GetPoint().(*authorityv1.ActivationPoint_Height); ok {
			return height >= int64(point.GetHeight())
		}
	}
	if _, ok := point.GetPoint().(*authorityv1.ActivationPoint_UnixSeconds); ok {
		return unix >= point.GetUnixSeconds()
	}
	if _, ok := point.GetPoint().(*authorityv1.ActivationPoint_Height); ok {
		return height >= int64(point.GetHeight())
	}
	return false
}

func activationKind(point *authorityv1.ActivationPoint) string {
	if _, ok := point.GetPoint().(*authorityv1.ActivationPoint_Height); ok {
		return "height"
	}
	return "time"
}

func activationValue(point *authorityv1.ActivationPoint) int64 {
	if _, ok := point.GetPoint().(*authorityv1.ActivationPoint_Height); ok {
		return int64(point.GetHeight())
	}
	return point.GetUnixSeconds()
}

func activationPoint(change PowerChange) *authorityv1.ActivationPoint {
	if change.ByHeight {
		return &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: change.Height}}
	}
	return &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: change.Unix}}
}

func businessDigest(fields map[string]any) []byte {
	canon, err := jcs.Canonicalize(fields)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256([]byte(canon))
	return sum[:]
}

func limitKey(msg *authorityv1.MsgProposeChange) string {
	if body := msg.GetPayload().GetGovernedLimit(); body != nil {
		return body.GetLimitKey()
	}
	return ""
}

func providerID(msg *authorityv1.MsgProposeChange) string {
	if body := msg.GetPayload().GetAdmitProvider(); body != nil {
		return body.GetProviderId()
	}
	if body := msg.GetPayload().GetDisableProvider(); body != nil {
		return body.GetProviderId()
	}
	return ""
}

func contentID(msg *authorityv1.MsgProposeChange) string {
	if body := msg.GetPayload().GetForceWithdraw(); body != nil {
		return body.GetContentId()
	}
	return ""
}

func payloadKind(canon string) string {
	switch {
	case strings.Contains(canon, `"validatorPower"`):
		return "validatorPower"
	case strings.Contains(canon, `"governedLimit"`):
		return "governedLimit"
	case strings.Contains(canon, `"admitProvider"`):
		return "admitProvider"
	case strings.Contains(canon, `"disableProvider"`):
		return "disableProvider"
	case strings.Contains(canon, `"forceWithdraw"`):
		return "forceWithdraw"
	default:
		return ""
	}
}

func providerFrom(canon string) string {
	const marker = `"providerId":"`
	i := strings.Index(canon, marker)
	if i < 0 {
		return ""
	}
	rest := canon[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func validatorFromCanonical(canon string) (PowerChange, string, Outcome) {
	// Rebuild through the protobuf the keeper already checked at propose time.
	// The canonical string is hashed again. Fields are read from that string
	// only for the values the proposer stored.
	change := PowerChange{}
	pub, okPub := jsonString(canon, "consensusPubkey")
	node, okNode := jsonString(canon, "nodeId")
	if !okPub || !okNode {
		return PowerChange{}, "", fail(CodeMalformed)
	}
	raw, err := decodeB64(pub)
	if err != nil || len(raw) != 32 {
		return PowerChange{}, "", fail(CodeBounds)
	}
	change.PubKey = raw
	change.NodeID = node
	if power, ok := jsonString(canon, "power"); ok {
		n, err := strconv.ParseInt(power, 10, 64)
		if err != nil || n <= 0 {
			return PowerChange{}, "", fail(CodeMalformed)
		}
		change.Power = n
	}
	if height, ok := jsonString(canon, "height"); ok {
		n, err := strconv.ParseUint(height, 10, 64)
		if err != nil || n < 1 {
			return PowerChange{}, "", fail(CodeMalformed)
		}
		change.ByHeight = true
		change.Height = n
	} else if unix, ok := jsonString(canon, "unixSeconds"); ok {
		n, err := strconv.ParseInt(unix, 10, 64)
		if err != nil {
			return PowerChange{}, "", fail(CodeMalformed)
		}
		change.Unix = n
	} else {
		return PowerChange{}, "", fail(CodeMalformed)
	}
	reason, okReason := jsonString(canon, "value")
	if !okReason {
		return PowerChange{}, "", fail(CodeMalformed)
	}
	rawReason, err := decodeB64(reason)
	if err != nil || len(rawReason) != 32 {
		return PowerChange{}, "", fail(CodeBounds)
	}
	change.Reason = rawReason
	return change, canon, ok()
}

func governedFields(canon string) (string, uint64, Outcome) {
	key, okKey := jsonString(canon, "limitKey")
	value, okValue := jsonString(canon, "value")
	if !okKey || !okValue {
		return "", 0, fail(CodeMalformed)
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return "", 0, fail(CodeMalformed)
	}
	return key, n, ok()
}

func jsonString(canon, key string) (string, bool) {
	marker := `"` + key + `":"`
	i := strings.Index(canon, marker)
	if i < 0 {
		return "", false
	}
	rest := canon[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

func decodeB64(text string) ([]byte, error) {
	return b64Decode(text)
}
