package authority

import (
	"math"
	"strconv"
	"testing"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// wantRefused runs one message and requires its exact code, no keep, no
// idempotent retry, and a byte-identical store.
func wantRefused(t *testing.T, kv KV, name, code string, run func() Outcome) {
	t.Helper()
	before := dumpKV(kv)
	out := run()
	if out.Code != code || out.Keep || out.Idempotent {
		t.Fatalf("%s: got %+v, want %s", name, out, code)
	}
	wantUnchanged(t, kv, before, name)
}

// wantState requires the committed state of one proposal.
func wantState(t *testing.T, kv KV, id string, want authorityv1.ProposalState) {
	t.Helper()
	if got := recordOf(t, kv, id).state(); got != want {
		t.Fatalf("%s is %v, want %v", id, got, want)
	}
}

// council() is spec council_ready plus the approved DEC-008 shape. Every
// council message is DECISION_BLOCKED unless the chain id has the devnet
// prefix and the config holds the DEC-008 test parameter (devnet label, not
// approved policy) with threshold 3, a delay in [0, unix max], and exactly
// four seats. One row per failure mode. Each row refuses Propose, Approve and
// Execute with DECISION_BLOCKED and leaves the store byte-identical; the
// control row shows the same three messages succeed on a ready council.
// Pins docs/architecture/authority-authorization.md ("a chain that is not a
// shortvideo-dev- devnet with the test parameter is
// ERROR_CODE_DECISION_BLOCKED"); no earlier test asserted this code.
func TestCouncilGateIsDecisionBlocked(t *testing.T) {
	param := func(edit func(*typesv1.DevnetTestParameter)) func(*authorityv1.GenesisState) {
		return func(cfg *authorityv1.GenesisState) { edit(cfg.DevnetTestParameters[0]) }
	}
	rows := []struct {
		name    string
		chainID string
		edit    func(*authorityv1.GenesisState)
	}{
		{"control: ready devnet council", testChainID, nil},
		{"chain id without the devnet prefix", "shortvideo-beta-unit", nil},
		{"no DEC-008 parameter", testChainID, func(cfg *authorityv1.GenesisState) { cfg.DevnetTestParameters = nil }},
		{"test parameter for another decision", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.DecisionId = "DEC-002" })},
		{"threshold 0", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.NumericValue = 0 })},
		{"threshold 2", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.NumericValue = 2 })},
		{"threshold 4", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.NumericValue = 4 })},
		{"negative delay", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.DelaySeconds = -1 })},
		{"delay above unix max", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.DelaySeconds = unixMax + 1 })},
		{"parameter without the devnet label", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.Label = "APPROVED_POLICY" })},
		{"parameter marked approved policy", testChainID, param(func(p *typesv1.DevnetTestParameter) { p.ApprovedPolicy = true })},
		{"three seats", testChainID, func(cfg *authorityv1.GenesisState) {
			cfg.Authority.DevnetTestSeats = cfg.Authority.DevnetTestSeats[:3]
		}},
		{"five seats", testChainID, func(cfg *authorityv1.GenesisState) {
			cfg.Authority.DevnetTestSeats = append(cfg.Authority.DevnetTestSeats,
				&authorityv1.DevnetTestSeat{SeatId: "seat-5", PublicKey: pub(34)})
		}},
	}
	for _, row := range rows {
		kv, seats, authority := readyStore(t)
		// Made while the council was ready: p-open has two approvals, a third
		// would queue it; p-queued is QUEUED and executable.
		st := loadAt(t, kv, testHeight, testTime)
		if out := st.ProposeChange(propose(seats[0].Signer, "p-open", 1, governedMsg("max_page_limit", 10))); !out.Success() {
			t.Fatal(out)
		}
		for i := 0; i < 2; i++ {
			if out := st.ApproveChange(approve(seats[i].Signer, "a-open-"+strconv.Itoa(i), "p-open")); !out.Success() {
				t.Fatal(out)
			}
		}
		queueAt(t, kv, seats, testHeight, testTime, "p-queued", governedMsg("max_evidence_batch", 7))
		if row.edit != nil {
			mutateConfig(t, kv, row.edit)
		}
		at := func() *State {
			st, err := LoadState(kv, row.chainID, testHeight, testTime)
			if err != nil {
				t.Fatal(err)
			}
			return st
		}
		msgs := []struct {
			what string
			run  func() Outcome
		}{
			{"propose", func() Outcome {
				return at().ProposeChange(propose(seats[2].Signer, "p-new", 1, governedMsg("max_open_offers", 3)))
			}},
			{"approve", func() Outcome { return at().ApproveChange(approve(seats[2].Signer, "a-open-2", "p-open")) }},
			{"execute", func() Outcome { return at().ExecuteChange(execute(authority, "ex-queued", "p-queued")) }},
		}
		for _, msg := range msgs {
			name := row.name + ": " + msg.what
			if row.edit == nil && row.chainID == testChainID {
				if out := msg.run(); !out.Success() {
					t.Fatalf("%s: %+v", name, out)
				}
				continue
			}
			wantRefused(t, kv, name, CodeDecisionBlocked, msg.run)
		}
	}
}

// The execute stale gate (spec op_execute, spec_machine.py:1509-1513): a
// QUEUED proposal on an older base policy version is STALE_VERSION, or
// CONFLICT when its parameter key was executed since with another payload
// digest. The same digest is STALE. Each refusal leaves the store
// byte-identical and the proposal QUEUED. No earlier test asserted either
// code: turning the gate off executes stale proposals, and swapping its arms
// returns the wrong code.
func TestStaleAndConflictingExecutes(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "first", governedMsg("max_page_limit", 10))
	queueAt(t, kv, seats, testHeight, testTime, "other-value", governedMsg("max_page_limit", 11))
	queueAt(t, kv, seats, testHeight, testTime, "same-value", governedMsg("max_page_limit", 10))
	queueAt(t, kv, seats, testHeight, testTime, "other-key", governedMsg("max_open_offers", 3))
	queueAt(t, kv, seats, testHeight, testTime, "validator", powerMsg("v5", pub(9), 1, 30))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-first", "first")); !out.Success() {
		t.Fatal(out)
	}
	if got := loadAt(t, kv, testHeight, testTime).PolicyVersion(); got != 2 {
		t.Fatalf("policy version %d", got)
	}
	for _, row := range []struct{ id, code string }{
		{"other-value", CodeConflict},
		{"same-value", CodeStale},
		{"other-key", CodeStale},
		{"validator", CodeStale},
	} {
		wantRefused(t, kv, "execute "+row.id, row.code, func() Outcome {
			return loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-"+row.id, row.id))
		})
		wantState(t, kv, row.id, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	}
	if len(pendingOf(t, kv)) != 0 {
		t.Fatal("a stale validator change was scheduled")
	}
}

// Two changes in one activation group may not name the same node or the
// same consensus key (spec project_power_changes, spec_machine.py:1279-1295):
// the second execute is CONFLICT, the proposal stays QUEUED, and the store is
// byte-identical. The same node at another height is a control that
// executes. No earlier test reached either rule.
func TestSameActivationGroupConflicts(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "first", powerMsg("v5", pub(9), 1, 30))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-first", "first")); !out.Success() {
		t.Fatal(out)
	}
	queueAt(t, kv, seats, testHeight, testTime, "same-node", powerMsg("v5", pub(10), 1, 30))
	queueAt(t, kv, seats, testHeight, testTime, "same-key", powerMsg("v6", pub(9), 1, 30))
	queueAt(t, kv, seats, testHeight, testTime, "next-height", powerMsg("v5", pub(11), 1, 31))
	for _, id := range []string{"same-node", "same-key"} {
		wantRefused(t, kv, "execute "+id, CodeConflict, func() Outcome {
			return loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-"+id, id))
		})
		wantState(t, kv, id, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	}
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-next", "next-height")); !out.Success() {
		t.Fatalf("the same node at another height %+v", out)
	}
	if got := len(pendingOf(t, kv)); got != 2 {
		t.Fatalf("%d pending entries", got)
	}
}

// A proposal on an older base policy version can never execute (it is STALE
// or CONFLICT), so it stops counting against its seat's open quota once an
// execute bumps the version (DR-06-03). Seat-1 opens 32 proposals, 31 of
// them with expiry unix_seconds_max, and queues half. One unrelated execute
// by other seats makes all 32 stale. Seat-1 can then open 32 more on the new
// base, and the 33rd is BOUNDS again. An EXPIRED mark on a stale proposal
// releases the count of its own base. Fails on the round-3 review tree, which
// kept one count per seat: seat-1 stayed at its limit until year 9999.
func TestStaleProposalsStopCounting(t *testing.T) {
	kv, seats, authority := readyStore(t)
	openFrom := func(id string, base uint64, expiry int64, value uint64) Outcome {
		msg := propose(seats[0].Signer, id, base, governedMsg("max_page_limit", value))
		msg.ExpiryUnixSeconds = expiry
		return loadAt(t, kv, testHeight, testTime).ProposeChange(msg)
	}
	ids := make([]string, MaxOpenProposalsPerSeat)
	for i := range ids {
		ids[i] = "s1-" + strconv.Itoa(i)
		expiry := unixMax
		if i == 0 {
			expiry = testTime + 100
		}
		if out := openFrom(ids[i], 1, expiry, uint64(1+i)); !out.Success() {
			t.Fatalf("open %s %+v", ids[i], out)
		}
	}
	for _, id := range ids[1:17] {
		for s := 1; s <= ApprovedThreshold; s++ {
			if out := loadAt(t, kv, testHeight, testTime).ApproveChange(approve(seats[s].Signer, "a-"+id+"-"+strconv.Itoa(s), id)); !out.Success() {
				t.Fatalf("approve %s %+v", id, out)
			}
		}
		wantState(t, kv, id, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	}
	wantRefused(t, kv, "33rd proposal on base 1", CodeBounds, func() Outcome {
		return openFrom("s1-over", 1, unixMax, 99)
	})

	// Seats 2-4 execute an unrelated change: the policy version becomes 2.
	queueAt(t, kv, rotate(seats, 1), testHeight, testTime, "unrelated", governedMsg("max_evidence_batch", 9))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-unrelated", "unrelated")); !out.Success() {
		t.Fatal(out)
	}
	for _, id := range ids[1:17] {
		wantRefused(t, kv, "execute stale "+id, CodeStale, func() Outcome {
			return loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-"+id, id))
		})
	}
	if got := openAt(t, kv, seats[0].ID, 1); got != MaxOpenProposalsPerSeat {
		t.Fatalf("base-1 count %d", got)
	}
	if got := openAt(t, kv, seats[0].ID, 2); got != 0 {
		t.Fatalf("base-2 count %d", got)
	}

	for i := 0; i < MaxOpenProposalsPerSeat; i++ {
		id := "s1-new-" + strconv.Itoa(i)
		if out := openFrom(id, 2, unixMax, uint64(1+i)); !out.Success() {
			t.Fatalf("proposal %d on base 2 after the stale ones %+v", i+1, out)
		}
	}
	wantRefused(t, kv, "33rd proposal on base 2", CodeBounds, func() Outcome {
		return openFrom("s1-new-over", 2, unixMax, 99)
	})

	// s1-0 expires. An approve marks it EXPIRED and releases its base-1 slot.
	late := loadAt(t, kv, testHeight, testTime+100)
	if out := late.ApproveChange(approve(seats[1].Signer, "a-expire", ids[0])); out.Code != CodeExpired || !out.Keep {
		t.Fatalf("expire %+v", out)
	}
	if one, two := openAt(t, kv, seats[0].ID, 1), openAt(t, kv, seats[0].ID, 2); one != MaxOpenProposalsPerSeat-1 || two != MaxOpenProposalsPerSeat {
		t.Fatalf("after the EXPIRED mark: base-1 count %d, base-2 count %d", one, two)
	}
}

// RotateCouncilSeat moves the seat's submitter row and its ante admission
// row (0x02) from the old seat address to the new one, keeping the role; the
// retired key's account is no longer admitted (DR-06-06). A new address that
// is already a submitter is DUPLICATE_ID and changes nothing. Fails on the
// round-3 review tree, which rewrote only the seat key and left the old
// address admitted and the new one not.
func TestSeatRotationMovesAdmission(t *testing.T) {
	gen, seats, _ := readyGenesis()
	for _, seat := range seats {
		gen.Authority.Submitters = append(gen.Authority.Submitters, &typesv1.Submitter{
			Address: seat.Signer, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
		})
	}
	// A broadcaster that is not a seat, whose account is the address of pub(42).
	taken := pub(42)
	gen.Authority.Submitters = append(gen.Authority.Submitters, &typesv1.Submitter{
		Address: SeatAddress(taken), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
	})
	kv := storeFrom(t, gen)
	newPub := pub(41)
	newAddr := SeatAddress(newPub)
	if out := loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[0].ID, newPub); !out.Success() {
		t.Fatal(out)
	}
	if kv.Has(AdmissionKey(seats[0].Signer)) {
		t.Fatal("the retired seat key's account is still admitted")
	}
	if got := kv.Get(AdmissionKey(newAddr)); len(got) != 1 || got[0] != byte(typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER) {
		t.Fatalf("new seat admission row %x", got)
	}
	found := 0
	for _, row := range loadAt(t, kv, testHeight, testTime).auth().GetSubmitters() {
		switch {
		case string(row.GetAddress()) == string(seats[0].Signer):
			t.Fatal("the retired seat address is still a submitter")
		case string(row.GetAddress()) == string(newAddr):
			found++
			if row.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER {
				t.Fatalf("moved row has role %v", row.GetRole())
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d submitter rows for the new seat address", found)
	}
	// seat-2 rotates to a key whose account is already a submitter.
	wantRefused(t, kv, "rotation onto a submitter address", CodeDuplicate, func() Outcome {
		return loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[1].ID, taken)
	})
}

// The engine-only rotations refuse a seat id outside the id pattern or a
// council key that is not 32 bytes (BOUNDS), a seat id that names no seat
// (NOT_FOUND), and a service rotation onto an address that is already a
// submitter, or onto a bad address (DUPLICATE_ID, BOUNDS). Each reject has
// its exact code and leaves the store byte-identical. The control rotations
// then succeed on the same store, so no row is refused for another reason.
// The second fix-round review found these branches unreached: changing any
// of their codes passed every suite.
func TestEngineOnlyRotationRejects(t *testing.T) {
	kv, seats, authority := readyStore(t)
	service := bytes20(2)
	rows := []struct {
		name string
		code string
		run  func(*State) Outcome
	}{
		{"seat id outside the pattern", CodeBounds, func(st *State) Outcome { return st.RotateCouncilSeat("Seat 1!", pub(41)) }},
		{"empty seat id", CodeBounds, func(st *State) Outcome { return st.RotateCouncilSeat("", pub(41)) }},
		{"31-byte seat key", CodeBounds, func(st *State) Outcome { return st.RotateCouncilSeat(seats[0].ID, pub(41)[:31]) }},
		{"33-byte seat key", CodeBounds, func(st *State) Outcome { return st.RotateCouncilSeat(seats[0].ID, append(pub(41), 0)) }},
		{"unknown seat", CodeNotFound, func(st *State) Outcome { return st.RotateCouncilSeat("seat-9", pub(41)) }},
		{"service onto the authority account", CodeDuplicate, func(st *State) Outcome { return st.RotateServiceSubmitter(service, authority) }},
		{"19-byte new service address", CodeBounds, func(st *State) Outcome { return st.RotateServiceSubmitter(service, bytes20(7)[:19]) }},
		{"19-byte old service address", CodeBounds, func(st *State) Outcome { return st.RotateServiceSubmitter(service[:19], bytes20(7)) }},
		{"same service address", CodeBounds, func(st *State) Outcome { return st.RotateServiceSubmitter(service, service) }},
	}
	for _, row := range rows {
		wantRefused(t, kv, row.name, row.code, func() Outcome { return row.run(loadAt(t, kv, testHeight, testTime)) })
	}
	if out := loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[0].ID, pub(41)); !out.Success() {
		t.Fatalf("control seat rotation %+v", out)
	}
	if out := loadAt(t, kv, testHeight, testTime).RotateServiceSubmitter(service, bytes20(7)); !out.Success() {
		t.Fatalf("control service rotation %+v", out)
	}
}

// An activation height must be in [1, 2^53-1] (spec parse_activation,
// spec_machine.py:1061-1065). Anything else is MALFORMED and changes
// nothing; 2^53-1 is accepted. Heights of 2^63 and above were accepted on
// the round-3 review tree: the bound compared int64(height), which is
// negative there.
func TestActivationHeightBounds(t *testing.T) {
	kv, seats, _ := readyStore(t)
	for _, height := range []uint64{0, uint64(safeMax) + 1, 1 << 63, math.MaxUint64} {
		body := &authorityv1.ValidatorPowerChange{
			ConsensusPubkey: pub(9), Power: 1, NodeId: "v5",
			ReasonDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(7)},
			Activation:   &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: height}},
		}
		msg := propose(seats[0].Signer, "p-height", 1, &authorityv1.MsgProposeChange{
			ParameterKey:  "validator-v5",
			Payload:       &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}},
			PayloadDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(8)},
		})
		wantRefused(t, kv, "activation height "+strconv.FormatUint(height, 10), CodeMalformed, func() Outcome {
			return loadAt(t, kv, testHeight, testTime).ProposeChange(msg)
		})
	}
	top := propose(seats[0].Signer, "p-top", 1, powerMsg("v5", pub(9), 1, uint64(safeMax)))
	if out := loadAt(t, kv, testHeight, testTime).ProposeChange(top); !out.Success() {
		t.Fatalf("activation height 2^53-1 %+v", out)
	}
}

// The policy version is a uint64 that each execute bumps by one. At
// 2^64-1 the next execute would wrap, so it is OVERFLOW: the store is
// byte-identical and the proposal stays QUEUED. One below that, the execute
// succeeds and stores 2^64-1. The version is written straight into the store
// here; reaching it by executes is not practical. Kills the mutant that
// weakens the guard, which no earlier test reached.
func TestPolicyVersionOverflow(t *testing.T) {
	for _, row := range []struct {
		name    string
		version uint64
		code    string
	}{
		{"one below the maximum", math.MaxUint64 - 1, ""},
		{"at the maximum", math.MaxUint64, CodeOverflow},
	} {
		kv, seats, authority := readyStore(t)
		kv.Set([]byte{policyKey}, encodeU64(row.version))
		queueAt(t, kv, seats, testHeight, testTime, "p-top", governedMsg("max_page_limit", 10))
		if got := recordOf(t, kv, "p-top").BasePolicyVersion; got != row.version {
			t.Fatalf("%s: base %d", row.name, got)
		}
		run := func() Outcome {
			return loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-top", "p-top"))
		}
		if row.code != "" {
			wantRefused(t, kv, row.name, row.code, run)
			wantState(t, kv, "p-top", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
			continue
		}
		if out := run(); !out.Success() {
			t.Fatalf("%s: %+v", row.name, out)
		}
		if got, good := decodeU64(kv.Get([]byte{policyKey})); !good || got != math.MaxUint64 {
			t.Fatalf("%s: stored version %d", row.name, got)
		}
	}
}
