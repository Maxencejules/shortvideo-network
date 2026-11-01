package authority

import (
	"bytes"
	"crypto/sha256"
	"testing"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func TestProposalPathsDoNotMutateOnReject(t *testing.T) {
	st, seats, authority := readyState()
	before := fingerprint(st)

	outsider := bytes20(9)
	rejected := []struct {
		name string
		out  Outcome
	}{
		{"unauthorized", st.ProposeChange(propose(outsider, "p-out", 1, powerMsg("v5", pub(9), 1, 20)))},
		{"wrong seat role", st.ApproveChange(&authorityv1.MsgApproveChange{Signer: authority, BusinessId: "a-auth", ProposalId: "missing"})},
	}
	for _, row := range rejected {
		if row.out.Success() || row.out.Keep {
			t.Fatalf("%s %+v", row.name, row.out)
		}
	}
	if fingerprint(st) != before {
		t.Fatal("rejected messages changed authority state")
	}

	open := st.ProposeChange(propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20)))
	if !open.Success() {
		t.Fatal(open)
	}
	if st.proposal("p-add").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
		t.Fatal(st.proposal("p-add").GetState())
	}
	again := st.ProposeChange(propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20)))
	if !again.Idempotent {
		t.Fatalf("repeat propose %+v", again)
	}
	second := st.ApproveChange(approve(seats[0], "ap-1", "p-add"))
	if !second.Success() || st.proposal("p-add").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
		t.Fatalf("one approval %+v", second)
	}
	replay := st.ApproveChange(approve(seats[0], "ap-1b", "p-add"))
	if replay.Code != CodeReplay {
		t.Fatalf("repeated vote %+v", replay)
	}
	if len(st.proposal("p-add").GetApproverKeys()) != 1 {
		t.Fatal("replay stored a second approval")
	}
	st.ApproveChange(approve(seats[1], "ap-2", "p-add"))
	queued := st.ApproveChange(approve(seats[2], "ap-3", "p-add"))
	if !queued.Success() || st.proposal("p-add").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatalf("threshold %+v state %v", queued, st.proposal("p-add").GetState())
	}
	stale := st.ApproveChange(approve(seats[3], "ap-4", "p-add"))
	if stale.Code != CodeConflict {
		t.Fatalf("stale approval %+v", stale)
	}
	early := st.ExecuteChange(execute(authority, "ex-early", "p-add"))
	// delay is 0, so execution in the same block time is allowed
	if !early.Success() {
		t.Fatalf("execute %+v", early)
	}
	if st.proposal("p-add").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal("not executed")
	}
	if len(st.Validators) != 4 || len(st.Pending) != 1 {
		t.Fatalf("scheduled set vals %d pending %d", len(st.Validators), len(st.Pending))
	}
	st.Height = 19
	if _, changed, out := st.ApplyDue(); changed || !out.Success() {
		t.Fatal("activated before the height")
	}
	st.Height = 20
	beforeSet, changed, out := st.ApplyDue()
	if !changed || !out.Success() || len(st.Validators) != 5 {
		t.Fatalf("apply %+v changed %v vals %d", out, changed, len(st.Validators))
	}
	updates, err := Changeset(beforeSet, st.Validators)
	if err != nil || len(updates) != 1 || updates[0].Power != 1 {
		t.Fatalf("updates %+v %v", updates, err)
	}
}

func TestExpiredExecuteKeepsMarkWithoutApplying(t *testing.T) {
	st, seats, authority := readyState()
	st.Time = 1_000
	msg := propose(seats[0], "p-exp", 1, powerMsg("v5", pub(9), 1, 50))
	msg.ExpiryUnixSeconds = 1_500
	if out := st.ProposeChange(msg); !out.Success() {
		t.Fatal(out)
	}
	for i, id := range []string{"e1", "e2", "e3"} {
		if out := st.ApproveChange(approve(seats[i], id, "p-exp")); !out.Success() {
			t.Fatal(out)
		}
	}
	st.Time = 1_500
	powers := len(st.Validators)
	out := st.ExecuteChange(execute(authority, "ex-exp", "p-exp"))
	if out.Code != CodeExpired || !out.Keep {
		t.Fatalf("%+v", out)
	}
	if st.proposal("p-exp").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
		t.Fatal(st.proposal("p-exp").GetState())
	}
	if len(st.Pending) != 0 || len(st.Validators) != powers {
		t.Fatal("expired execution applied a validator change")
	}
}

func TestDelayAndForbiddenRemoval(t *testing.T) {
	st, seats, authority := readyState()
	st.Params[0].DelaySeconds = 10
	if out := st.ProposeChange(propose(seats[0], "p-delay", 1, powerMsg("v5", pub(9), 1, 30))); !out.Success() {
		t.Fatal(out)
	}
	for i, id := range []string{"d1", "d2", "d3"} {
		if out := st.ApproveChange(approve(seats[i], id, "p-delay")); !out.Success() {
			t.Fatal(out)
		}
	}
	if st.proposal("p-delay").GetExecuteAfterUnixSeconds() != st.Time+10 {
		t.Fatal(st.proposal("p-delay").GetExecuteAfterUnixSeconds())
	}
	early := st.ExecuteChange(execute(authority, "ex-d", "p-delay"))
	if early.Code != CodeOutOfRange {
		t.Fatalf("delay %+v", early)
	}
	if st.proposal("p-delay").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal("early execute changed the proposal")
	}
	st.Time += 10
	if out := st.ExecuteChange(execute(authority, "ex-d2", "p-delay")); !out.Success() {
		t.Fatal(out)
	}

	// Removing every validator is forbidden and marks the proposal rejected.
	st2, seats2, authority2 := readyState()
	bad := st2.ProposeChange(propose(seats2[0], "p-all", 1, powerMsg("v1", pub(1), 0, 30)))
	if !bad.Success() {
		t.Fatal(bad)
	}
	for i, id := range []string{"r1", "r2", "r3"} {
		if out := st2.ApproveChange(approve(seats2[i], id, "p-all")); !out.Success() {
			t.Fatal(out)
		}
	}
	out := st2.ExecuteChange(execute(authority2, "ex-all", "p-all"))
	if out.Code != CodeForbidden || !out.Keep {
		t.Fatalf("removal %+v", out)
	}
	if st2.proposal("p-all").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED || len(st2.Pending) != 0 {
		t.Fatal("forbidden removal was scheduled")
	}
}

func TestRemovalBindHaltsAndRotationsStaySeparate(t *testing.T) {
	st, _, _ := readyState()
	st.Pending = []*authorityv1.PendingValidatorPower{{
		ProposalId:      "commit-bad-key",
		NodeId:          "v1",
		ConsensusPubkey: pub(9),
		Power:           0,
		Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: 1}},
	}}
	st.Height = 1
	fingerprintBefore := fingerprint(st)
	_, changed, out := st.ApplyDue()
	if changed || out.Code != CodeConflict {
		t.Fatalf("bind %+v changed %v", out, changed)
	}
	if RemovalBindCommit != "halt" {
		t.Fatal(RemovalBindCommit)
	}
	if fingerprint(st) != fingerprintBefore {
		t.Fatal("halt dropped or applied the bad removal")
	}
	_, _, again := st.ApplyDue()
	if again.Code != CodeConflict {
		t.Fatal("a second block would have continued")
	}

	// Provider signing-key rotation moved to x/providers in Goal 07
	// (TestRotationTouchesOnlyTheProvidersStore there). Council and service
	// rotations stay here and touch no consensus key.
	councilBefore := append([]byte(nil), st.Seats[0].GetPublicKey()...)
	serviceBefore := append([]byte(nil), st.Submitters[1].GetAddress()...)
	if out := st.RotateCouncilSeat("seat-1", pub(21)); !out.Success() {
		t.Fatal(out)
	}
	if bytes.Equal(st.Validators[0].GetConsensusPubkey(), st.Seats[0].GetPublicKey()) {
		t.Fatal("council rotation wrote a consensus key")
	}
	if !bytes.Equal(st.PrevCouncil[0], councilBefore) {
		t.Fatal("old council key was not retained")
	}
	if out := st.RotateServiceSubmitter(serviceBefore, bytes20(6)); !out.Success() {
		t.Fatal(out)
	}
	if bytes.Equal(st.Submitters[1].GetAddress(), serviceBefore) {
		t.Fatal("service address did not rotate")
	}
	for _, row := range st.Validators {
		if bytes.Equal(row.GetConsensusPubkey(), st.Submitters[1].GetAddress()) {
			t.Fatal("service rotation touched a validator")
		}
	}
}

func TestAdmissionAndMixedActivation(t *testing.T) {
	st, seats, authoritySigner := readyState()
	st.Params = append(st.Params, &typesv1.DevnetTestParameter{
		Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-002", RoleName: "DEVNET_TEST_ONLY",
	})
	// The provider record lives in x/providers. The fake registry records
	// the call authority makes and admits.
	registry := &fakeRegistry{}
	st.Registry = registry
	payload := &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{
		AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: "p1"},
	}}
	digest, out := PayloadDigest(payload)
	if !out.Success() {
		t.Fatal(out)
	}
	msg := &authorityv1.MsgProposeChange{
		ParameterKey: "admit-p1", Payload: payload,
		PayloadDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digest},
	}
	if out := st.ProposeChange(propose(seats[0], "admit-p1", 1, msg)); !out.Success() {
		t.Fatal(out)
	}
	for i, id := range []string{"ad1", "ad2", "ad3"} {
		if out := st.ApproveChange(approve(seats[i], id, "admit-p1")); !out.Success() {
			t.Fatal(out)
		}
	}
	validatorsBefore := cloneValidators(st.Validators)
	pendingBefore := len(st.Pending)
	if out := st.ExecuteChange(execute(authoritySigner, "ex-admit", "admit-p1")); !out.Success() {
		t.Fatal(out)
	}
	want := []registryCall{{Method: "admit", ID: "p1", Devnet: true}}
	if !sameCalls(registry.calls, want) {
		t.Fatalf("registry calls %+v want %+v", registry.calls, want)
	}
	if !sameValidators(st.Validators, validatorsBefore) || len(st.Pending) != pendingBefore {
		t.Fatal("admission changed the validator set or scheduled a power change")
	}
	if st.proposal("admit-p1").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(st.proposal("admit-p1").GetState())
	}
	if out := st.ProposeChange(propose(seats[0], "height-1", st.PolicyVersion, powerMsg("v5", pub(9), 1, 30))); !out.Success() {
		t.Fatal(out)
	}
	for i, id := range []string{"h1", "h2", "h3"} {
		if out := st.ApproveChange(approve(seats[i], id, "height-1")); !out.Success() {
			t.Fatal(out)
		}
	}
	if out := st.ExecuteChange(execute(authoritySigner, "ex-height", "height-1")); !out.Success() {
		t.Fatal(out)
	}
	direct := st.ProposeChange(propose(seats[0], "time-1", st.PolicyVersion, timePower("v6", pub(10), 1, st.Time+100)))
	if !direct.Success() {
		t.Fatal(direct)
	}
	for i, id := range []string{"tm1", "tm2", "tm3"} {
		if out := st.ApproveChange(approve(seats[i], id, "time-1")); !out.Success() {
			t.Fatal(out)
		}
	}
	mixed := st.ExecuteChange(execute(authoritySigner, "ex-time", "time-1"))
	if mixed.Code != CodeConflict || mixed.Keep {
		t.Fatalf("mixed activation %+v", mixed)
	}
	if st.proposal("time-1").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal(st.proposal("time-1").GetState())
	}
}

func timePower(node string, key []byte, power, unix int64) *authorityv1.MsgProposeChange {
	body := &authorityv1.ValidatorPowerChange{
		ConsensusPubkey: key, Power: power, NodeId: node,
		ReasonDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(7)},
		Activation:   &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: unix}},
	}
	_, canon, out := validatorCanonical(body)
	if !out.Success() {
		panic(out.Code)
	}
	sum := sha256.Sum256([]byte(canon))
	return &authorityv1.MsgProposeChange{
		ParameterKey: "validator-" + node,
		Payload:      &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}},
		PayloadDigest: &typesv1.Digest{
			Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:],
		},
	}
}

func TestQuorumUsesPowerNotCount(t *testing.T) {
	// 2,2,2,2,1. max*3 = 6 < 9. One power-1 offline still has quorum.
	// Two power-2 offline leaves 5 of 9, which is not more than two thirds,
	// even though three nodes remain.
	powers := []int64{2, 2, 2, 2, 1}
	if !PowerQuorum(powers, []bool{false, false, false, false, true}) {
		t.Fatal("power 1 offline halted")
	}
	if PowerQuorum(powers, []bool{true, true, false, false, false}) {
		t.Fatal("two power-2 validators still counted as quorum")
	}
	if PowerQuorum([]int64{1, 1, 1, 1}, []bool{true, true, false, false}) {
		t.Fatal("half the power counted as more than two thirds")
	}
}

func TestPayloadVectorDigest(t *testing.T) {
	canon, out := ParsePayloadJSON([]byte(`{"validatorPower":{"power":"1","nodeId":"v5","reasonDigest":{"value":"CX2wA0prEclni4NHxA3ZzCI+No6UaDVlor0CjMc0+FA=","algorithm":"DIGEST_ALGORITHM_SHA256"},"consensusPubkey":"6KCHbf6Mj7qib+NE/UMKju7jD63veJqJ+OatCH9lUKs=","activation":{"height":"3"}}}`))
	if !out.Success() {
		t.Fatal(out)
	}
	sum := sha256.Sum256([]byte(canon))
	got := hexEncode(sum[:])
	if got != "be9df594c416ecbaf50512e298a73fecf9f7a56e7ac83a0cf4edf87a6b522c48" {
		t.Fatalf("digest %s canonical %s", got, canon)
	}
}

func TestFuzzBoundedPayload(t *testing.T) {
	var seed int64 = 6
	next := func() byte {
		seed = seed*6364136223846793005 + 1
		return byte(seed >> 33)
	}
	for i := 0; i < 200; i++ {
		n := int(next()%64) + 1
		raw := make([]byte, n)
		for j := range raw {
			raw[j] = next()
		}
		_, _ = ParsePayloadJSON(raw)
	}
	if _, out := ParsePayloadJSON(bytes.Repeat([]byte{'a'}, 9000)); out.Code != CodeBounds {
		t.Fatal(out)
	}
}

func FuzzParsePayload(f *testing.F) {
	f.Add([]byte(`{"validatorPower":{"nodeId":"v5","activation":{"height":"3"}}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4096 {
			raw = raw[:4096]
		}
		_, _ = ParsePayloadJSON(raw)
	})
}

func readyState() (*State, [][]byte, []byte) {
	vals := make([]*authorityv1.ValidatorPower, 4)
	seats := make([]*authorityv1.DevnetTestSeat, 4)
	signers := make([][]byte, 4)
	for i := 0; i < 4; i++ {
		vals[i] = &authorityv1.ValidatorPower{ConsensusPubkey: pub(byte(i + 1)), Power: 1, NodeId: "v" + string(rune('1'+i))}
		key := pub(byte(30 + i))
		seats[i] = &authorityv1.DevnetTestSeat{SeatId: "seat-" + string(rune('1'+i)), PublicKey: key}
		signers[i] = SeatAddress(key)
	}
	authority := bytes20(1)
	service := bytes20(2)
	st := &State{
		ChainID:       "shortvideo-dev-unit",
		Height:        5,
		Time:          1_700_000_000,
		PolicyVersion: 1,
		Validators:    vals,
		Submitters: []*typesv1.Submitter{
			{Address: authority, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY},
			{Address: service, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
		},
		Seats: seats,
		Params: []*typesv1.DevnetTestParameter{{
			Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-008", NumericValue: 3, DelaySeconds: 0,
		}},
		Payloads: map[string]string{},
		Business: map[string][]byte{},
		Executed: map[string][]byte{},
	}
	return st, signers, authority
}

func propose(signer []byte, id string, base uint64, msg *authorityv1.MsgProposeChange) *authorityv1.MsgProposeChange {
	msg.Signer = signer
	msg.BusinessId = "b-" + id
	msg.ProposalId = id
	msg.BasePolicyVersion = base
	msg.ExpiryUnixSeconds = 1_800_000_000
	return msg
}

func powerMsg(node string, key []byte, power int64, height uint64) *authorityv1.MsgProposeChange {
	body := &authorityv1.ValidatorPowerChange{
		ConsensusPubkey: key,
		Power:           power,
		NodeId:          node,
		ReasonDigest: &typesv1.Digest{
			Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
			Value:     pub(7),
		},
		Activation: &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: height}},
	}
	change, canon, out := validatorCanonical(body)
	if !out.Success() {
		panic(out.Code)
	}
	sum := sha256.Sum256([]byte(canon))
	_ = change
	return &authorityv1.MsgProposeChange{
		ParameterKey: "validator-" + node,
		Payload:      &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}},
		PayloadDigest: &typesv1.Digest{
			Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
			Value:     sum[:],
		},
	}
}

func approve(signer []byte, id, proposal string) *authorityv1.MsgApproveChange {
	return &authorityv1.MsgApproveChange{Signer: signer, BusinessId: id, ProposalId: proposal}
}

func execute(signer []byte, id, proposal string) *authorityv1.MsgExecuteChange {
	return &authorityv1.MsgExecuteChange{Signer: signer, BusinessId: id, ProposalId: proposal}
}

func pub(last byte) []byte {
	out := make([]byte, 32)
	out[31] = last
	return out
}

func bytes20(last byte) []byte {
	out := make([]byte, 20)
	out[19] = last
	return out
}

func fingerprint(st *State) string {
	var buf bytes.Buffer
	for _, row := range st.Validators {
		buf.WriteString(row.GetNodeId())
		buf.Write(row.GetConsensusPubkey())
	}
	buf.WriteString("|")
	for _, row := range st.Proposals {
		buf.WriteString(row.GetProposalId())
		buf.WriteString(row.GetState().String())
	}
	buf.WriteString("|")
	for _, row := range st.Pending {
		buf.WriteString(row.GetProposalId())
	}
	sum := sha256.Sum256(buf.Bytes())
	return hexEncode(sum[:])
}

func hexEncode(raw []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(raw)*2)
	for i, b := range raw {
		out[i*2] = digits[b>>4]
		out[i*2+1] = digits[b&0x0f]
	}
	return string(out)
}
