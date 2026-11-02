package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"slices"
	"strconv"
	"strings"

	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
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

// State is the engine view of the authority store for one message or one
// EndBlock. It holds the decoded config key; every other object is read from
// the store when a message names it. Liveness is not a field.
//
// The engine writes the authority store only on a success or keep path,
// after every check that can fail, through one batch flushed at the end. A
// rejected message performs no Set and no Delete on it.
//
// A registry hook (Registry) writes x/providers or x/content state, not the
// authority store. It runs inside applyPayload, before the engine's last
// checks: a later OVERFLOW (policy version 2^64-1) or an INTEGRITY error from
// the open count or the record encoding can still fail the message after the
// hook wrote. That is why Keeper.transition binds Registry to a nested cache
// that it writes only when the whole message succeeds.
//
// Registry and External are not stored. Keeper.transition binds them to one
// transaction after LoadState; LoadState leaves them nil.
type State struct {
	ChainID string
	Height  int64
	Time    int64

	// Registry applies the provider and content payloads of an executed
	// proposal. nil means no registry is wired: those payloads are MALFORMED.
	Registry Registry
	// External returns the business digest another module committed for
	// id. Business ids are one protocol namespace. nil means no peers.
	External func(id string) ([]byte, bool)

	kv     KV
	config *authorityv1.GenesisState
}

// Registry is the provider and content registry as an executed council
// proposal sees it. x/providers and x/content own the records; authority
// only decides the devnet flag and passes the decoded payload. A method
// runs every check before its first write, so a failure writes nothing.
// FORBIDDEN marks the proposal REJECTED; any other failure leaves it QUEUED.
type Registry interface {
	// Admit moves an APPLIED provider to ADMITTED. devnet is true only when
	// DEC-002 is the labelled devnet test parameter with role
	// DEVNET_TEST_ONLY on a devnet chain id.
	Admit(providerID string, devnet bool) Outcome
	// Disable moves an ADMITTED or DRAINING provider to DISABLED.
	Disable(providerID string) Outcome
	// ForceWithdraw withdraws one content version and writes its tombstone
	// with the 32-byte reason digest value. devnet follows the DEC-002 rule
	// for DEC-003.
	ForceWithdraw(contentID string, version uint64, reason []byte, devnet bool) Outcome
}

// LoadState decodes the config key and returns the engine view. A missing or
// undecodable config key is an error; the keeper reports it as
// ERROR_CODE_INTEGRITY and EndBlock halts on it.
func LoadState(kv KV, chainID string, height, unix int64) (*State, error) {
	cfg, err := readConfig(kv)
	if err != nil {
		return nil, err
	}
	return &State{ChainID: chainID, Height: height, Time: unix, kv: kv, config: cfg}, nil
}

func (s *State) auth() *authorityv1.AuthorityState { return s.config.GetAuthority() }

// Validators returns a copy of the current set, sorted by consensus key.
func (s *State) Validators() []*authorityv1.ValidatorPower {
	out := cloneValidators(s.auth().GetValidators())
	slices.SortFunc(out, func(a, b *authorityv1.ValidatorPower) int {
		return bytes.Compare(a.GetConsensusPubkey(), b.GetConsensusPubkey())
	})
	return out
}

// policy is the policy version: 1 until the first execute. A stored value
// that is not 8 bytes, or is below 2, is integrity damage.
func (s *State) policy() (uint64, Outcome) {
	raw := s.kv.Get([]byte{policyKey})
	if raw == nil {
		return 1, ok()
	}
	version, good := decodeU64(raw)
	if !good || version < 2 {
		return 0, fail(CodeIntegrity)
	}
	return version, ok()
}

// batch collects the writes of one transition. flush applies them in order.
type batch struct {
	keys   [][]byte
	values [][]byte // nil value: delete
}

func (b *batch) set(key, value []byte) {
	b.keys = append(b.keys, key)
	b.values = append(b.values, value)
}

func (b *batch) del(key []byte) {
	b.keys = append(b.keys, key)
	b.values = append(b.values, nil)
}

func (s *State) flush(b *batch) {
	for i, key := range b.keys {
		if b.values[i] == nil {
			s.kv.Delete(key)
			continue
		}
		s.kv.Set(key, b.values[i])
	}
}

func (s *State) devnet(id string) *typesv1.DevnetTestParameter {
	if !strings.HasPrefix(s.ChainID, genesis.DevnetChainIDPrefix) {
		return nil
	}
	for _, row := range s.config.GetDevnetTestParameters() {
		if row.GetDecisionId() == id && row.GetLabel() == genesis.DevnetTestLabel && !row.GetApprovedPolicy() {
			return row
		}
	}
	return nil
}

// devnetRole reports whether decision id is the labelled devnet test
// parameter with role DEVNET_TEST_ONLY on this chain.
func (s *State) devnetRole(id string) bool {
	param := s.devnet(id)
	return param != nil && param.GetRoleName() == devnetTestRole
}

// council is spec council_ready plus the approved DEC-008 shape.
func (s *State) council() Outcome {
	param := s.devnet("DEC-008")
	if param == nil || param.GetNumericValue() != ApprovedThreshold || param.GetDelaySeconds() < 0 || param.GetDelaySeconds() > unixMax {
		return fail(CodeDecisionBlocked)
	}
	if len(s.auth().GetDevnetTestSeats()) != ApprovedSeats {
		return fail(CodeDecisionBlocked)
	}
	return ok()
}

func (s *State) seatOf(signer []byte) *authorityv1.DevnetTestSeat {
	for _, seat := range s.auth().GetDevnetTestSeats() {
		if bytes.Equal(SeatAddress(seat.GetPublicKey()), signer) {
			return seat
		}
	}
	return nil
}

func (s *State) role(signer []byte) typesv1.SubmitterRole {
	for _, row := range s.auth().GetSubmitters() {
		if bytes.Equal(row.GetAddress(), signer) {
			return row.GetRole()
		}
	}
	return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
}

// business is spec business_gate. It reads only the message's own id: first
// authority's own row (0x11), then, when that is absent, the peer ledgers
// through External (one protocol namespace). Each caller has already refused
// an empty id as MALFORMED (require_fields), so a non-matching id here is
// BOUNDS. A peer value that is not 32 bytes is INTEGRITY, like an own value.
func (s *State) business(id string, digest []byte) Outcome {
	if !idPattern.MatchString(id) {
		return fail(CodeBounds)
	}
	if len(digest) != sha256.Size {
		return fail(CodeMalformed)
	}
	prev := s.kv.Get(businessKey(id))
	if prev == nil && s.External != nil {
		if peer, found := s.External(id); found {
			prev = append([]byte{}, peer...) // non-nil, so an empty peer value is INTEGRITY below
		}
	}
	if prev == nil {
		return ok()
	}
	if len(prev) != sha256.Size {
		return fail(CodeIntegrity)
	}
	if bytes.Equal(prev, digest) {
		return Outcome{Idempotent: true}
	}
	return fail(CodeReplay)
}

// record reads one proposal. An id outside the pattern cannot name a stored
// record, so it is NOT_FOUND without a store read.
func (s *State) record(id string) (*proposalRecord, Outcome) {
	if !idPattern.MatchString(id) {
		return nil, fail(CodeNotFound)
	}
	raw := s.kv.Get(proposalKey(id))
	if raw == nil {
		return nil, fail(CodeNotFound)
	}
	rec, err := decodeRecord(id, raw)
	if err != nil {
		return nil, fail(CodeIntegrity)
	}
	return rec, ok()
}

func putRecord(b *batch, rec *proposalRecord) Outcome {
	raw, err := encodeRecord(rec)
	if err != nil {
		return fail(CodeIntegrity)
	}
	b.set(proposalKey(rec.ProposalID), raw)
	return ok()
}

// openCount is the number of OPEN or QUEUED proposals of seatID opened on
// base version version.
func (s *State) openCount(seatID string, version uint64) (uint64, Outcome) {
	raw := s.kv.Get(openKey(seatID, version))
	if raw == nil {
		return 0, ok()
	}
	count, good := decodeU64(raw)
	if !good || count == 0 {
		return 0, fail(CodeIntegrity)
	}
	return count, ok()
}

// release takes rec out of its proposer's open count for its base version.
// It runs once per proposal, on its transition out of OPEN or QUEUED. A count
// that would go below zero is integrity damage.
func (s *State) release(b *batch, rec *proposalRecord) Outcome {
	count, out := s.openCount(rec.Proposer, rec.BasePolicyVersion)
	if !out.Success() {
		return out
	}
	key := openKey(rec.Proposer, rec.BasePolicyVersion)
	switch count {
	case 0:
		return fail(CodeIntegrity)
	case 1:
		b.del(key)
	default:
		b.set(key, encodeU64(count-1))
	}
	return ok()
}

// keepAs stores a keep outcome: the record gets its terminal mark, and the
// proposer's open count drops if the record was OPEN or QUEUED. An EXECUTED
// proposal relabelled EXPIRED (spec op_execute) does not touch the count.
func (s *State) keepAs(rec *proposalRecord, state authorityv1.ProposalState, code string) Outcome {
	var b batch
	if rec.open() {
		if out := s.release(&b, rec); !out.Success() {
			return out
		}
	}
	rec.State = int32(state)
	if out := putRecord(&b, rec); !out.Success() {
		return out
	}
	s.flush(&b)
	return keep(code)
}

// ProposeChange opens one proposal. The signer must be a devnet test seat.
// Check order follows spec op_propose (spec_machine.py:1403-1449).
func (s *State) ProposeChange(msg *authorityv1.MsgProposeChange) Outcome {
	// require_fields (1405). proto3 has no absent integer, so only strings,
	// bytes and messages can be missing here.
	if msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProposalId() == "" ||
		msg.GetParameterKey() == "" || msg.GetPayload() == nil || msg.GetPayloadDigest() == nil {
		return fail(CodeMalformed)
	}
	if ready := s.council(); !ready.Success() { // 1408
		return ready
	}
	seat := s.seatOf(msg.GetSigner())
	if seat == nil { // 1411
		return fail(CodeUnauthorized)
	}
	if !idPattern.MatchString(msg.GetProposalId()) || !idPattern.MatchString(msg.GetParameterKey()) { // 1413-1416
		return fail(CodeBounds)
	}
	if strings.HasPrefix(msg.GetParameterKey(), "dec-") { // 1417
		return fail(CodeForbidden)
	}
	// validated_payload (1191-1210): parse, digest, parameter key, activation.
	change, kind, canon, parsed := CanonicalFromProto(msg.GetPayload(), msg.GetPayloadDigest().GetValue())
	if !parsed.Success() {
		return parsed
	}
	if msg.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {
		return fail(CodeMalformed)
	}
	if msg.GetParameterKey() != parameterKey(kind, change.NodeID, limitKey(msg), providerID(msg), contentID(msg)) {
		return fail(CodeMalformed)
	}
	if kind == kindValidatorPower && !activationFuture(activationPoint(change), s.Height, s.Time) {
		return fail(CodeOutOfRange)
	}
	if msg.GetExpiryUnixSeconds() < unixMin || msg.GetExpiryUnixSeconds() > unixMax { // check_time 1423
		return fail(CodeBounds)
	}
	digest := businessDigest(map[string]any{
		"base_policy_version": strconv.FormatUint(msg.GetBasePolicyVersion(), 10),
		"expiry_unix_seconds": strconv.FormatInt(msg.GetExpiryUnixSeconds(), 10),
		"parameter_key":       msg.GetParameterKey(),
		"payload":             canon,
		"proposal_id":         msg.GetProposalId(),
	})
	if gate := s.business(msg.GetBusinessId(), digest); !gate.Success() || gate.Idempotent { // 1429
		return gate
	}
	if s.kv.Has(proposalKey(msg.GetProposalId())) { // 1432
		return fail(CodeDuplicate)
	}
	if msg.GetExpiryUnixSeconds() <= s.Time { // 1434
		return fail(CodeExpired)
	}
	version, out := s.policy()
	if !out.Success() {
		return out
	}
	if msg.GetBasePolicyVersion() != version { // 1436
		return fail(CodeStale)
	}
	// Go only (DR-06-03): the per-seat open quota is the last check. It
	// counts only proposals on the current base version: one on an older
	// base can never execute, so it no longer holds a slot.
	open, out := s.openCount(seat.GetSeatId(), version)
	if !out.Success() {
		return out
	}
	if open >= MaxOpenProposalsPerSeat {
		return fail(CodeBounds)
	}
	rec := &proposalRecord{
		ProposalID:        msg.GetProposalId(),
		ParameterKey:      msg.GetParameterKey(),
		BasePolicyVersion: msg.GetBasePolicyVersion(),
		PayloadDigest:     bytes.Clone(msg.GetPayloadDigest().GetValue()),
		State:             int32(authorityv1.ProposalState_PROPOSAL_STATE_OPEN),
		ExpiryUnixSeconds: msg.GetExpiryUnixSeconds(),
		BusinessID:        msg.GetBusinessId(),
		Proposer:          seat.GetSeatId(),
		Payload:           canon,
		Decoded:           decodedFrom(kind, change, msg),
	}
	var b batch
	if out := putRecord(&b, rec); !out.Success() {
		return out
	}
	b.set(businessKey(msg.GetBusinessId()), digest)
	b.set(openKey(seat.GetSeatId(), version), encodeU64(open+1))
	s.flush(&b)
	return ok()
}

// ApproveChange records one seat. The same seat again is a replay, also
// after its key rotates: approvals are deduplicated by seat id.
// Check order follows spec op_approve (spec_machine.py:1452-1482).
func (s *State) ApproveChange(msg *authorityv1.MsgApproveChange) Outcome {
	if msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProposalId() == "" { // 1455
		return fail(CodeMalformed)
	}
	if ready := s.council(); !ready.Success() { // 1457
		return ready
	}
	seat := s.seatOf(msg.GetSigner())
	if seat == nil { // 1460
		return fail(CodeUnauthorized)
	}
	digest := businessDigest(map[string]any{
		"proposal_id": msg.GetProposalId(),
		"signer":      hex.EncodeToString(msg.GetSigner()),
	})
	if gate := s.business(msg.GetBusinessId(), digest); !gate.Success() || gate.Idempotent { // 1462
		return gate
	}
	rec, out := s.record(msg.GetProposalId()) // 1465-1467
	if !out.Success() {
		return out
	}
	if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN { // 1468
		return fail(CodeConflict)
	}
	if s.Time >= rec.ExpiryUnixSeconds { // 1470-1472
		return s.keepAs(rec, authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED, CodeExpired)
	}
	if slices.Contains(rec.Approvers, seat.GetSeatId()) { // 1473
		return fail(CodeReplay)
	}
	// Go only: the execute-after time must stay inside the unix bound.
	delay := s.devnet("DEC-008").GetDelaySeconds()
	if len(rec.Approvers)+1 >= ApprovedThreshold && s.Time > unixMax-delay {
		return fail(CodeOverflow)
	}
	rec.Approvers = append(rec.Approvers, seat.GetSeatId())
	slices.Sort(rec.Approvers)
	rec.ApproverKeys = append(rec.ApproverKeys, bytes.Clone(seat.GetPublicKey()))
	slices.SortFunc(rec.ApproverKeys, bytes.Compare)
	if len(rec.Approvers) >= ApprovedThreshold {
		rec.State = int32(authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
		rec.ExecuteAfterUnixSeconds = s.Time + delay
	}
	var b batch
	if out := putRecord(&b, rec); !out.Success() {
		return out
	}
	b.set(businessKey(msg.GetBusinessId()), digest)
	s.flush(&b)
	return ok()
}

// ExecuteChange applies or schedules the stored payload.
// Check order follows spec op_execute (spec_machine.py:1485-1527).
func (s *State) ExecuteChange(msg *authorityv1.MsgExecuteChange) Outcome {
	if msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProposalId() == "" { // 1488
		return fail(CodeMalformed)
	}
	if role := s.role(msg.GetSigner()); role != typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY { // check_role 1490
		if role == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			return fail(CodeUnauthorized)
		}
		return fail(CodeForbidden)
	}
	if ready := s.council(); !ready.Success() { // 1493
		return ready
	}
	digest := businessDigest(map[string]any{"proposal_id": msg.GetProposalId()})
	if gate := s.business(msg.GetBusinessId(), digest); !gate.Success() || gate.Idempotent { // 1496
		return gate
	}
	rec, out := s.record(msg.GetProposalId()) // 1499-1501
	if !out.Success() {
		return out
	}
	// Expiry is tested before the QUEUED check, as in op_execute (1502-1504).
	if s.Time >= rec.ExpiryUnixSeconds {
		return s.keepAs(rec, authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED, CodeExpired)
	}
	if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		return fail(CodeConflict)
	}
	if s.Time < rec.ExecuteAfterUnixSeconds {
		return fail(CodeOutOfRange)
	}
	version, out := s.policy()
	if !out.Success() {
		return out
	}
	if rec.BasePolicyVersion != version {
		prev := s.kv.Get(executedKey(rec.ParameterKey))
		if prev != nil && !bytes.Equal(prev, rec.PayloadDigest) {
			return fail(CodeConflict)
		}
		return fail(CodeStale)
	}
	var b batch
	if applied := s.applyPayload(&b, rec); !applied.Success() {
		if applied.Code == CodeForbidden {
			return s.keepAs(rec, authorityv1.ProposalState_PROPOSAL_STATE_REJECTED, CodeForbidden)
		}
		return applied
	}
	if version == math.MaxUint64 {
		return fail(CodeOverflow)
	}
	if out := s.release(&b, rec); !out.Success() {
		return out
	}
	rec.State = int32(authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED)
	if out := putRecord(&b, rec); !out.Success() {
		return out
	}
	b.set(executedKey(rec.ParameterKey), bytes.Clone(rec.PayloadDigest))
	b.set([]byte{policyKey}, encodeU64(version+1))
	b.set(businessKey(msg.GetBusinessId()), digest)
	s.flush(&b)
	return ok()
}

// applyPayload checks the stored payload and adds its authority writes to b.
// It writes nothing to the authority store itself; a failure leaves b to be
// discarded. A registry payload adds nothing to b: it calls Registry, which
// writes to x/providers or x/content. The keeper binds Registry to a nested
// cache that it writes only when the whole message succeeds (msg.go).
func (s *State) applyPayload(b *batch, rec *proposalRecord) Outcome {
	if rec.Payload == "" || !digestMatches(rec.Payload, rec.PayloadDigest) {
		return fail(CodeIntegrity)
	}
	decoded := rec.Decoded
	switch decoded.Kind {
	case kindValidatorPower:
		return s.schedule(b, rec.ProposalID, decoded.power())
	case kindGovernedLimit:
		ceiling, known := governedCeiling(decoded.LimitKey)
		if !known || decoded.LimitValue < 1 || decoded.LimitValue > ceiling {
			return fail(CodeBounds)
		}
		b.set(governedKey(decoded.LimitKey), encodeU64(decoded.LimitValue))
		return ok()
	case kindAdmitProvider:
		if s.Registry == nil {
			return fail(CodeMalformed)
		}
		return registryOutcome(s.Registry.Admit(decoded.ProviderID, s.devnetRole("DEC-002")))
	case kindDisableProvider:
		if s.Registry == nil {
			return fail(CodeMalformed)
		}
		return registryOutcome(s.Registry.Disable(decoded.ProviderID))
	case kindForceWithdraw:
		if s.Registry == nil {
			return fail(CodeMalformed)
		}
		return registryOutcome(s.Registry.ForceWithdraw(decoded.ContentID, decoded.ContentVersion,
			bytes.Clone(decoded.Reason), s.devnetRole("DEC-003")))
	default:
		return fail(CodeIntegrity)
	}
}

// registryOutcome keeps only the code. A registry cannot ask for a keep or
// an idempotent result; ExecuteChange decides those.
func registryOutcome(out Outcome) Outcome {
	if out.Success() {
		return ok()
	}
	return fail(out.Code)
}

// schedule adds one pending validator change after projecting it with every
// entry already pending (spec apply_proposal_payload, 1344-1359).
func (s *State) schedule(b *batch, proposalID string, change PowerChange) Outcome {
	point := activationPoint(change)
	if !activationFuture(point, s.Height, s.Time) {
		return fail(CodeOutOfRange)
	}
	entry := &authorityv1.PendingValidatorPower{
		ProposalId:      proposalID,
		ConsensusPubkey: change.PubKey,
		Power:           change.Power,
		NodeId:          change.NodeID,
		ReasonDigest:    &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: change.Reason},
		Activation:      point,
	}
	pending, err := readPending(s.kv)
	if err != nil {
		return fail(CodeIntegrity)
	}
	// A genesis entry can carry the id of a later council proposal. The
	// spec leaves a same-id item out of the projection; its pending ids are
	// always executed proposal ids, so that never happens there. Here it
	// would let EndBlock apply a set the projection never checked, so a
	// same-id entry (which also covers the same key) is a CONFLICT.
	for _, item := range pending {
		if item.GetProposalId() == proposalID {
			return fail(CodeConflict)
		}
	}
	if out := projectAll(s.auth().GetValidators(), append(pending, entry)); !out.Success() {
		return out
	}
	// Go only (DR-06-04): checked after the projection, as the last rule.
	if len(pending) >= MaxPendingChanges {
		return fail(CodeBounds)
	}
	raw, err := marshalState(entry)
	if err != nil {
		return fail(CodeIntegrity)
	}
	b.set(PendingKey(entry), raw)
	return ok()
}

// governedCeiling is the protocol ceiling of each governed limit.
func governedCeiling(key string) (uint64, bool) {
	switch key {
	case "max_evidence_batch":
		return uint64(genesis.MaxEvidenceBatch), true
	case "max_page_limit":
		return uint64(genesis.MaxPageLimit), true
	case "max_open_offers":
		return uint64(genesis.MaxOpenOffersPerProvider), true
	case "max_expiry_steps":
		return uint64(genesis.MaxExpiryStepsPerBlock), true
	case "max_package_bytes":
		return genesis.MaxPackageBytes, true
	default:
		return 0, false
	}
}

// projected is one validator in a projection: its key and power.
type projected struct {
	pub   []byte
	power int64
}

// projectAll is spec project_power_changes over the given validators and
// items. Execute passes every pending entry plus the new one; genesis
// validation passes the genesis entries. Map iteration below never decides
// a result: each loop returns the same code whichever entry fails first.
func projectAll(validators []*authorityv1.ValidatorPower, items []*authorityv1.PendingValidatorPower) Outcome {
	if len(items) == 0 {
		return ok()
	}
	ordered := slices.Clone(items)
	firstKind, _, good := activationSlot(ordered[0].GetActivation())
	if !good {
		return fail(CodeMalformed)
	}
	for _, item := range ordered {
		kind, _, good := activationSlot(item.GetActivation())
		if !good {
			return fail(CodeMalformed)
		}
		if kind != firstKind { // DEC-012
			return fail(CodeConflict)
		}
	}
	slices.SortFunc(ordered, comparePending)
	vals := validatorMap(validators)
	for index := 0; index < len(ordered); {
		_, point, _ := activationSlot(ordered[index].GetActivation())
		var group []*authorityv1.PendingValidatorPower
		for index < len(ordered) {
			_, next, _ := activationSlot(ordered[index].GetActivation())
			if next != point {
				break
			}
			group = append(group, ordered[index])
			index++
		}
		nodes := map[string]struct{}{}
		keys := map[string]struct{}{}
		for _, item := range group {
			if _, dup := nodes[item.GetNodeId()]; dup {
				return fail(CodeConflict)
			}
			nodes[item.GetNodeId()] = struct{}{}
			if item.GetPower() == 0 {
				// Bind a removal to the key the validator holds now.
				current, have := vals[item.GetNodeId()]
				if !have || !bytes.Equal(current.pub, item.GetConsensusPubkey()) {
					return fail(CodeConflict)
				}
			}
			if _, dup := keys[string(item.GetConsensusPubkey())]; dup {
				return fail(CodeConflict)
			}
			keys[string(item.GetConsensusPubkey())] = struct{}{}
		}
		for _, item := range group {
			if item.GetPower() == 0 {
				delete(vals, item.GetNodeId())
				continue
			}
			for node, row := range vals {
				if node != item.GetNodeId() && bytes.Equal(row.pub, item.GetConsensusPubkey()) {
					return fail(CodeDuplicate)
				}
			}
			vals[item.GetNodeId()] = projected{pub: bytes.Clone(item.GetConsensusPubkey()), power: item.GetPower()}
		}
		if out := powerInvariant(vals); !out.Success() {
			return out
		}
	}
	return ok()
}

// comparePending is spec activation_sort_key: kind, point, proposal id. It
// is also the order of the pending keys.
func comparePending(a, b *authorityv1.PendingValidatorPower) int {
	ak, ap, _ := activationSlot(a.GetActivation())
	bk, bp, _ := activationSlot(b.GetActivation())
	if ak != bk {
		return int(ak) - int(bk)
	}
	if ap != bp {
		if ap < bp {
			return -1
		}
		return 1
	}
	return strings.Compare(a.GetProposalId(), b.GetProposalId())
}

// powerInvariant is spec power_set_invariant (spec_machine.py:321-338): at
// least four validators, every power positive, total at most the safe
// integer maximum, and max_power*3 < total. Every failure is FORBIDDEN.
// The sum is checked before each addition, so it cannot overflow int64.
func powerInvariant(vals map[string]projected) Outcome {
	if len(vals) < genesis.MinValidators {
		return fail(CodeForbidden)
	}
	var total, maxPower int64
	for _, row := range vals {
		if row.power <= 0 || row.power > safeMax-total {
			return fail(CodeForbidden)
		}
		total += row.power
		maxPower = max(maxPower, row.power)
	}
	if maxPower*3 >= total {
		return fail(CodeForbidden)
	}
	return ok()
}

// ApplyDue applies the validator changes whose activation point has been
// reached (spec apply_due_validator_power). It reads the pending entries
// (at most MaxPendingChanges) and writes the new set and the deletions only
// on success. A removal whose key does not match, or a resulting set that
// breaks the invariant, halts: nothing is written and the entry stays.
func (s *State) ApplyDue() (before []*authorityv1.ValidatorPower, changed bool, out Outcome) {
	pending, err := readPending(s.kv)
	if err != nil {
		return nil, false, fail(CodeIntegrity)
	}
	var due []*authorityv1.PendingValidatorPower
	for _, item := range pending {
		if activationDue(item.GetActivation(), s.Height, s.Time) {
			due = append(due, item)
		}
	}
	if len(due) == 0 {
		return nil, false, ok()
	}
	// readPending returns key order, which is the spec due order.
	before = s.Validators()
	working := s.Validators()
	find := func(id string) *authorityv1.ValidatorPower {
		for _, row := range working {
			if row.GetNodeId() == id {
				return row
			}
		}
		return nil
	}
	for _, item := range due {
		current := find(item.GetNodeId())
		if item.GetPower() == 0 {
			if current == nil {
				return before, false, fail(CodeNotFound)
			}
			if !bytes.Equal(current.GetConsensusPubkey(), item.GetConsensusPubkey()) {
				// Halt (RemovalBindCommit). The working copy is discarded.
				return before, false, fail(CodeConflict)
			}
			working = slices.DeleteFunc(working, func(row *authorityv1.ValidatorPower) bool {
				return row.GetNodeId() == item.GetNodeId()
			})
			continue
		}
		if current == nil {
			working = append(working, &authorityv1.ValidatorPower{
				ConsensusPubkey: bytes.Clone(item.GetConsensusPubkey()),
				Power:           item.GetPower(),
				NodeId:          item.GetNodeId(),
				ReasonDigest:    item.GetReasonDigest(),
			})
			continue
		}
		current.ConsensusPubkey = bytes.Clone(item.GetConsensusPubkey())
		current.Power = item.GetPower()
		current.ReasonDigest = item.GetReasonDigest()
	}
	keys := map[string]struct{}{}
	for _, row := range working {
		if _, dup := keys[string(row.GetConsensusPubkey())]; dup {
			return before, false, fail(CodeDuplicate)
		}
		keys[string(row.GetConsensusPubkey())] = struct{}{}
	}
	if out := powerInvariant(validatorMap(working)); !out.Success() {
		return before, false, out
	}
	slices.SortFunc(working, func(a, b *authorityv1.ValidatorPower) int {
		return bytes.Compare(a.GetConsensusPubkey(), b.GetConsensusPubkey())
	})
	cfg := proto.Clone(s.config).(*authorityv1.GenesisState)
	cfg.Authority.Validators = working
	raw, err := marshalState(cfg)
	if err != nil {
		return before, false, fail(CodeIntegrity)
	}
	var b batch
	b.set([]byte{configKey}, raw)
	for _, item := range due {
		b.del(PendingKey(item))
	}
	s.flush(&b)
	s.config = cfg
	return before, true, ok()
}

// RotateCouncilSeat replaces one council key. It does not touch consensus
// keys or provider signing keys. There is no single-signature message for
// it; it is engine-only. The old key is retired (0x15) and cannot return.
//
// A seat account passes the ante only through a submitter row (DR-06-06).
// When the old seat address has one, the rotation moves that row and its
// admission row (0x02) to the new seat address in the same batch, so the
// retired key is no longer admitted and the new key is. A new address that
// is already a submitter is DUPLICATE_ID.
func (s *State) RotateCouncilSeat(seatID string, newPub []byte) Outcome {
	if !idPattern.MatchString(seatID) || len(newPub) != 32 {
		return fail(CodeBounds)
	}
	cfg := proto.Clone(s.config).(*authorityv1.GenesisState)
	var seat *authorityv1.DevnetTestSeat
	for _, row := range cfg.GetAuthority().GetDevnetTestSeats() {
		if row.GetSeatId() == seatID {
			seat = row
			continue
		}
		if bytes.Equal(row.GetPublicKey(), newPub) {
			return fail(CodeDuplicate)
		}
	}
	if seat == nil {
		return fail(CodeNotFound)
	}
	if bytes.Equal(seat.GetPublicKey(), newPub) || s.kv.Has(retiredCouncilKey(newPub)) {
		return fail(CodeConflict)
	}
	old := bytes.Clone(seat.GetPublicKey())
	oldAddr, newAddr := SeatAddress(old), SeatAddress(newPub)
	var moved *typesv1.Submitter
	for _, row := range cfg.GetAuthority().GetSubmitters() {
		if bytes.Equal(row.GetAddress(), newAddr) {
			return fail(CodeDuplicate)
		}
		if bytes.Equal(row.GetAddress(), oldAddr) {
			moved = row
		}
	}
	seat.PublicKey = bytes.Clone(newPub)
	if moved != nil {
		moved.Address = bytes.Clone(newAddr)
	}
	raw, err := marshalState(cfg)
	if err != nil {
		return fail(CodeIntegrity)
	}
	var b batch
	b.set([]byte{configKey}, raw)
	b.set(retiredCouncilKey(old), []byte(seatID))
	if moved != nil {
		b.del(AdmissionKey(oldAddr))
		b.set(AdmissionKey(newAddr), []byte{byte(moved.GetRole())})
	}
	s.flush(&b)
	s.config = cfg
	return ok()
}

// RotateServiceSubmitter replaces an admitted service account and its ante
// admission row. It is engine-only. The old address is retired (0x16); it is
// not a consensus key.
func (s *State) RotateServiceSubmitter(oldAddr, newAddr []byte) Outcome {
	if len(oldAddr) != 20 || len(newAddr) != 20 || bytes.Equal(oldAddr, newAddr) {
		return fail(CodeBounds)
	}
	cfg := proto.Clone(s.config).(*authorityv1.GenesisState)
	var found *typesv1.Submitter
	for _, row := range cfg.GetAuthority().GetSubmitters() {
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
	if s.kv.Has(retiredServiceKey(newAddr)) {
		return fail(CodeConflict)
	}
	found.Address = bytes.Clone(newAddr)
	raw, err := marshalState(cfg)
	if err != nil {
		return fail(CodeIntegrity)
	}
	var b batch
	b.set([]byte{configKey}, raw)
	b.del(AdmissionKey(oldAddr))
	b.set(AdmissionKey(newAddr), []byte{byte(found.GetRole())})
	b.set(retiredServiceKey(oldAddr), []byte{0x01})
	s.flush(&b)
	s.config = cfg
	return ok()
}

func activationFuture(point *authorityv1.ActivationPoint, height, unix int64) bool {
	switch body := point.GetPoint().(type) {
	case *authorityv1.ActivationPoint_Height:
		return body.Height > uint64(max(height, 0))
	case *authorityv1.ActivationPoint_UnixSeconds:
		return body.UnixSeconds > unix
	default:
		return false
	}
}

func activationDue(point *authorityv1.ActivationPoint, height, unix int64) bool {
	switch body := point.GetPoint().(type) {
	case *authorityv1.ActivationPoint_Height:
		return height >= 0 && uint64(height) >= body.Height
	case *authorityv1.ActivationPoint_UnixSeconds:
		return unix >= body.UnixSeconds
	default:
		return false
	}
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

func decodedFrom(kind string, change PowerChange, msg *authorityv1.MsgProposeChange) decodedWire {
	out := decodedWire{Kind: kind}
	switch kind {
	case kindValidatorPower:
		out.NodeID = change.NodeID
		out.PubKey = bytes.Clone(change.PubKey)
		out.Power = change.Power
		out.Reason = bytes.Clone(change.Reason)
		out.ByHeight = change.ByHeight
		out.Height = change.Height
		out.Unix = change.Unix
	case kindGovernedLimit:
		out.LimitKey = limitKey(msg)
		out.LimitValue = msg.GetPayload().GetGovernedLimit().GetValue()
	case kindAdmitProvider, kindDisableProvider:
		out.ProviderID = providerID(msg)
	case kindForceWithdraw:
		// withdrawCanonical has validated the id pattern, a version of at
		// least 1 and a 32-byte SHA-256 reason digest.
		body := msg.GetPayload().GetForceWithdraw()
		out.ContentID = contentID(msg)
		out.ContentVersion = body.GetContentVersion()
		out.Reason = bytes.Clone(body.GetReasonDigest().GetValue())
	}
	return out
}

func limitKey(msg *authorityv1.MsgProposeChange) string {
	return msg.GetPayload().GetGovernedLimit().GetLimitKey()
}

func providerID(msg *authorityv1.MsgProposeChange) string {
	if body := msg.GetPayload().GetAdmitProvider(); body != nil {
		return body.GetProviderId()
	}
	return msg.GetPayload().GetDisableProvider().GetProviderId()
}

func contentID(msg *authorityv1.MsgProposeChange) string {
	return msg.GetPayload().GetForceWithdraw().GetContentId()
}
