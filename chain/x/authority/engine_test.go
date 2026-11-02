package authority

import (
	"bytes"
	"errors"
	"strconv"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Every reject row leaves the whole store byte-identical (dumpKV) and returns
// its exact code. It replaces the round-2 rows that never reached
// DUPLICATE_ID or NOT_FOUND (the duplicate reused the business id and got
// REPLAY; the missing-proposal approve was signed by a non-seat). The
// round-2 code returned the same codes for those branches; this pins them.
func TestProposalPathsDoNotMutateOnReject(t *testing.T) {
	kv, seats, authority := readyStore(t)
	st := loadAt(t, kv, testHeight, testTime)
	if out := st.ProposeChange(propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))); !out.Success() {
		t.Fatal(out)
	}

	badDigest := propose(seats[0].Signer, "p-dig", 1, powerMsg("v5", pub(9), 1, 20))
	badDigest.PayloadDigest.Value = pub(1)
	dupID := propose(seats[0].Signer, "p-add", 1, powerMsg("v6", pub(10), 1, 20))
	dupID.BusinessId = "b-fresh"
	replayBusiness := propose(seats[0].Signer, "p-other", 1, powerMsg("v6", pub(10), 1, 20))
	replayBusiness.BusinessId = "b-p-add"
	expired := propose(seats[0].Signer, "p-exp", 1, powerMsg("v6", pub(10), 1, 20))
	expired.ExpiryUnixSeconds = testTime
	unbound := propose(seats[0].Signer, "p-unbound", 1, powerMsg("v6", pub(10), 1, 20))
	unbound.ParameterKey = "max_page_limit"
	noAlgorithm := propose(seats[0].Signer, "p-algo", 1, powerMsg("v6", pub(10), 1, 20))
	noAlgorithm.PayloadDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
	expiryZero := propose(seats[0].Signer, "p-exp0", 1, powerMsg("v6", pub(10), 1, 20))
	expiryZero.ExpiryUnixSeconds = 0
	expiryOver := propose(seats[0].Signer, "p-expmax", 1, powerMsg("v6", pub(10), 1, 20))
	expiryOver.ExpiryUnixSeconds = unixMax + 1
	rows := []struct {
		name string
		run  func(*State) Outcome
		code string
	}{
		{"outsider propose", func(s *State) Outcome {
			return s.ProposeChange(propose(bytes20(9), "p-out", 1, powerMsg("v5", pub(9), 1, 20)))
		}, CodeUnauthorized},
		{"wrong-role approve", func(s *State) Outcome { return s.ApproveChange(approve(authority, "a-auth", "p-add")) }, CodeUnauthorized},
		{"bad id pattern", func(s *State) Outcome {
			return s.ProposeChange(propose(seats[0].Signer, "Bad!", 1, powerMsg("v5", pub(9), 1, 20)))
		}, CodeBounds},
		{"dec- parameter key", func(s *State) Outcome {
			msg := propose(seats[0].Signer, "p-dec", 1, governedMsg("max_page_limit", 5))
			msg.ParameterKey = "dec-008"
			return s.ProposeChange(msg)
		}, CodeForbidden},
		{"wrong payload digest", func(s *State) Outcome { return s.ProposeChange(badDigest) }, CodeIntegrity},
		// validated_payload (spec_machine.py:1203-1211): the parameter key
		// must be the one the payload binds; the digest algorithm is a proto
		// field the spec does not have, checked after the digest.
		{"parameter key not bound to the payload", func(s *State) Outcome { return s.ProposeChange(unbound) }, CodeMalformed},
		{"digest algorithm unspecified", func(s *State) Outcome { return s.ProposeChange(noAlgorithm) }, CodeMalformed},
		// check_time (spec_machine.py:1423), with a future activation.
		{"expiry 0", func(s *State) Outcome { return s.ProposeChange(expiryZero) }, CodeBounds},
		{"expiry one above unix max", func(s *State) Outcome { return s.ProposeChange(expiryOver) }, CodeBounds},
		{"activation not in the future", func(s *State) Outcome {
			return s.ProposeChange(propose(seats[0].Signer, "p-past", 1, powerMsg("v5", pub(9), 1, uint64(testHeight))))
		}, CodeOutOfRange},
		{"expiry not after block time", func(s *State) Outcome { return s.ProposeChange(expired) }, CodeExpired},
		{"stale base policy version", func(s *State) Outcome {
			return s.ProposeChange(propose(seats[0].Signer, "p-stale", 2, powerMsg("v6", pub(10), 1, 20)))
		}, CodeStale},
		{"reused business id, other payload", func(s *State) Outcome { return s.ProposeChange(replayBusiness) }, CodeReplay},
		{"duplicate proposal id, fresh business id", func(s *State) Outcome { return s.ProposeChange(dupID) }, CodeDuplicate},
		{"seat approve of a missing proposal", func(s *State) Outcome { return s.ApproveChange(approve(seats[1].Signer, "a-miss", "missing")) }, CodeNotFound},
		{"execute of an OPEN proposal", func(s *State) Outcome { return s.ExecuteChange(execute(authority, "ex-open", "p-add")) }, CodeConflict},
		{"service-signed execute", func(s *State) Outcome { return s.ExecuteChange(execute(bytes20(2), "ex-svc", "p-add")) }, CodeForbidden},
		{"seat-signed execute", func(s *State) Outcome { return s.ExecuteChange(execute(seats[0].Signer, "ex-seat", "p-add")) }, CodeUnauthorized},
	}
	for _, row := range rows {
		before := dumpKV(kv)
		out := row.run(loadAt(t, kv, testHeight, testTime))
		if out.Code != row.code || out.Keep || out.Idempotent {
			t.Fatalf("%s: got %+v, want %s", row.name, out, row.code)
		}
		wantUnchanged(t, kv, before, row.name)
	}

	before := dumpKV(kv)
	again := st.ProposeChange(propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20)))
	if !again.Idempotent {
		t.Fatalf("identical retry %+v", again)
	}
	wantUnchanged(t, kv, before, "identical retry")

	if out := st.ApproveChange(approve(seats[0].Signer, "ap-1", "p-add")); !out.Success() {
		t.Fatal(out)
	}
	before = dumpKV(kv)
	if out := st.ApproveChange(approve(seats[0].Signer, "ap-1b", "p-add")); out.Code != CodeReplay {
		t.Fatalf("repeated vote %+v", out)
	}
	wantUnchanged(t, kv, before, "repeated vote")
	if len(recordOf(t, kv, "p-add").Approvers) != 1 {
		t.Fatal("replay stored a second approval")
	}
	if out := st.ApproveChange(approve(seats[1].Signer, "ap-2", "p-add")); !out.Success() {
		t.Fatal(out)
	}
	if out := st.ApproveChange(approve(seats[2].Signer, "ap-3", "p-add")); !out.Success() {
		t.Fatal(out)
	}
	if got := recordOf(t, kv, "p-add").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal(got)
	}
	before = dumpKV(kv)
	if out := st.ApproveChange(approve(seats[3].Signer, "ap-4", "p-add")); out.Code != CodeConflict {
		t.Fatalf("approve of a QUEUED proposal %+v", out)
	}
	wantUnchanged(t, kv, before, "approve of a QUEUED proposal")
	if out := st.ExecuteChange(execute(authority, "ex-1", "p-add")); !out.Success() {
		t.Fatalf("execute %+v", out)
	}
	if got := recordOf(t, kv, "p-add").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(got)
	}
	if len(st.Validators()) != 4 || len(pendingOf(t, kv)) != 1 || st.PolicyVersion() != 2 {
		t.Fatalf("vals %d pending %d policy %d", len(st.Validators()), len(pendingOf(t, kv)), st.PolicyVersion())
	}
	early := loadAt(t, kv, 19, testTime)
	if _, changed, out := early.ApplyDue(); changed || !out.Success() {
		t.Fatal("activated before the height")
	}
	due := loadAt(t, kv, 20, testTime)
	beforeSet, changed, out := due.ApplyDue()
	if !changed || !out.Success() || len(due.Validators()) != 5 || len(pendingOf(t, kv)) != 0 {
		t.Fatalf("apply %+v changed %v vals %d", out, changed, len(due.Validators()))
	}
	updates, err := Changeset(beforeSet, due.Validators())
	if err != nil || len(updates) != 1 || updates[0].Power != 1 {
		t.Fatalf("updates %+v %v", updates, err)
	}
}

// Ported from round 2: an execute after expiry keeps the EXPIRED mark and
// schedules nothing.
func TestExpiredExecuteKeepsMarkWithoutApplying(t *testing.T) {
	kv, seats, authority := readyStore(t)
	st := loadAt(t, kv, testHeight, 1_000)
	msg := propose(seats[0].Signer, "p-exp", 1, powerMsg("v5", pub(9), 1, 50))
	msg.ExpiryUnixSeconds = 1_500
	if out := st.ProposeChange(msg); !out.Success() {
		t.Fatal(out)
	}
	for i := 0; i < 3; i++ {
		if out := st.ApproveChange(approve(seats[i].Signer, "e"+strconv.Itoa(i), "p-exp")); !out.Success() {
			t.Fatal(out)
		}
	}
	late := loadAt(t, kv, testHeight, 1_500)
	out := late.ExecuteChange(execute(authority, "ex-exp", "p-exp"))
	if out.Code != CodeExpired || !out.Keep {
		t.Fatalf("%+v", out)
	}
	if got := recordOf(t, kv, "p-exp").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
		t.Fatal(got)
	}
	if len(pendingOf(t, kv)) != 0 || len(late.Validators()) != 4 || openOf(t, kv, seats[0].ID) != 0 {
		t.Fatal("expired execution applied a validator change or kept the open count")
	}
	if kv.Has(businessKey("ex-exp")) {
		t.Fatal("a keep outcome recorded the business id")
	}
}

// Ported from round 2: the DEC-008 delay holds execution, and a removal that
// leaves three validators is FORBIDDEN and marks the proposal REJECTED.
func TestDelayAndForbiddenRemoval(t *testing.T) {
	kv, seats, authority := readyStore(t)
	mutateConfig(t, kv, func(cfg *authorityv1.GenesisState) { cfg.DevnetTestParameters[0].DelaySeconds = 10 })
	queueAt(t, kv, seats, testHeight, testTime, "p-delay", powerMsg("v5", pub(9), 1, 30))
	if got := recordOf(t, kv, "p-delay").ExecuteAfterUnixSeconds; got != testTime+10 {
		t.Fatal(got)
	}
	before := dumpKV(kv)
	if out := loadAt(t, kv, testHeight, testTime+9).ExecuteChange(execute(authority, "ex-d", "p-delay")); out.Code != CodeOutOfRange {
		t.Fatalf("delay %+v", out)
	}
	wantUnchanged(t, kv, before, "early execute")
	if out := loadAt(t, kv, testHeight, testTime+10).ExecuteChange(execute(authority, "ex-d2", "p-delay")); !out.Success() {
		t.Fatal(out)
	}

	kv2, seats2, authority2 := readyStore(t)
	queueAt(t, kv2, seats2, testHeight, testTime, "p-rm", removalMsg("v1", pub(1), 30))
	out := loadAt(t, kv2, testHeight, testTime).ExecuteChange(execute(authority2, "ex-rm", "p-rm"))
	if out.Code != CodeForbidden || !out.Keep {
		t.Fatalf("removal %+v", out)
	}
	if recordOf(t, kv2, "p-rm").state() != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED || len(pendingOf(t, kv2)) != 0 {
		t.Fatal("forbidden removal was scheduled")
	}
}

// Ported from round 2: a due removal bound to the wrong key halts, and seat
// and service rotations never touch consensus keys. Provider signing-key
// rotation moved to x/providers in Goal 07 (TestRotationTouchesOnlyTheProvidersStore
// there).
func TestRemovalBindHaltsAndRotationsStaySeparate(t *testing.T) {
	kv, seats, _ := readyStore(t)
	putPendingRaw(t, kv, heightEntry("commit-bad-key", "v1", pub(9), 0, 1))
	before := dumpKV(kv)
	_, changed, out := loadAt(t, kv, 1, testTime).ApplyDue()
	if changed || out.Code != CodeConflict {
		t.Fatalf("bind %+v changed %v", out, changed)
	}
	if RemovalBindCommit != "halt" {
		t.Fatal(RemovalBindCommit)
	}
	wantUnchanged(t, kv, before, "removal-bind halt")
	if _, _, again := loadAt(t, kv, 2, testTime).ApplyDue(); again.Code != CodeConflict {
		t.Fatal("a second block would have continued")
	}
	kv.Delete(PendingKey(heightEntry("commit-bad-key", "v1", pub(9), 0, 1)))

	st := loadAt(t, kv, testHeight, testTime)
	validators := st.Validators()
	if out := st.RotateCouncilSeat(seats[0].ID, pub(21)); !out.Success() {
		t.Fatal(out)
	}
	if got := string(kv.Get(retiredCouncilKey(seats[0].Pub))); got != seats[0].ID {
		t.Fatalf("retired council key %q", got)
	}
	before = dumpKV(kv)
	if out := loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[0].ID, seats[0].Pub); out.Code != CodeConflict {
		t.Fatalf("retired council key came back %+v", out)
	}
	if out := loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[0].ID, seats[1].Pub); out.Code != CodeDuplicate {
		t.Fatalf("another seat's key %+v", out)
	}
	wantUnchanged(t, kv, before, "rejected seat rotation")

	service := bytes20(2)
	st = loadAt(t, kv, testHeight, testTime)
	if out := st.RotateServiceSubmitter(service, bytes20(6)); !out.Success() {
		t.Fatal(out)
	}
	if kv.Has(AdmissionKey(service)) || !kv.Has(AdmissionKey(bytes20(6))) || !kv.Has(retiredServiceKey(service)) {
		t.Fatal("service rotation did not move the admission row")
	}
	before = dumpKV(kv)
	if out := loadAt(t, kv, testHeight, testTime).RotateServiceSubmitter(bytes20(6), service); out.Code != CodeConflict {
		t.Fatalf("retired service address came back %+v", out)
	}
	if out := loadAt(t, kv, testHeight, testTime).RotateServiceSubmitter(bytes20(1), bytes20(7)); out.Code != CodeNotFound {
		t.Fatalf("authority account rotated as a service %+v", out)
	}
	wantUnchanged(t, kv, before, "rejected service rotation")
	after := loadAt(t, kv, testHeight, testTime).Validators()
	for i := range validators {
		if !bytes.Equal(validators[i].GetConsensusPubkey(), after[i].GetConsensusPubkey()) || validators[i].GetPower() != after[i].GetPower() {
			t.Fatal("a seat or service rotation changed the validator set")
		}
	}
}

// DEC-012: a time activation while a height entry is pending is CONFLICT and
// the proposal stays QUEUED. Ported from round 2.
func TestMixedActivationConflicts(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "height-1", powerMsg("v5", pub(9), 1, 30))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-h", "height-1")); !out.Success() {
		t.Fatal(out)
	}
	queueAt(t, kv, seats, testHeight, testTime, "time-1", timePower("v6", pub(10), 1, testTime+100))
	before := dumpKV(kv)
	out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-t", "time-1"))
	if out.Code != CodeConflict || out.Keep {
		t.Fatalf("mixed activation %+v", out)
	}
	wantUnchanged(t, kv, before, "mixed activation")
	if got := recordOf(t, kv, "time-1").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatal(got)
	}
}

// More than 128 terminal proposals of each kind do not block a new one.
// Fails on the old code: it counted every stored proposal and returned BOUNDS
// at the 129th proposal for the life of the chain.
func TestTerminalProposalsDoNotBlock(t *testing.T) {
	kv, seats, authority := readyStore(t)
	const each = 130
	now := testTime
	for i := 0; i < each; i++ {
		id := "x-" + strconv.Itoa(i)
		st := loadAt(t, kv, testHeight, now)
		msg := propose(seats[i%4].Signer, id, st.PolicyVersion(), governedMsg("max_page_limit", 10))
		msg.ExpiryUnixSeconds = now + 5
		if out := st.ProposeChange(msg); !out.Success() {
			t.Fatalf("propose %s %+v", id, out)
		}
		now += 10
		if out := loadAt(t, kv, testHeight, now).ApproveChange(approve(seats[(i+1)%4].Signer, "ax-"+strconv.Itoa(i), id)); out.Code != CodeExpired || !out.Keep {
			t.Fatalf("expire %s %+v", id, out)
		}
	}
	for i := 0; i < each; i++ {
		id := "r-" + strconv.Itoa(i)
		queueAt(t, kv, rotate(seats, i), testHeight, now, id, removalMsg("v1", pub(1), 100))
		if out := loadAt(t, kv, testHeight, now).ExecuteChange(execute(authority, "er-"+strconv.Itoa(i), id)); out.Code != CodeForbidden || !out.Keep {
			t.Fatalf("reject %s %+v", id, out)
		}
	}
	for i := 0; i < each; i++ {
		id := "e-" + strconv.Itoa(i)
		queueAt(t, kv, rotate(seats, i), testHeight, now, id, governedMsg("max_page_limit", uint64(1+i%100)))
		if out := loadAt(t, kv, testHeight, now).ExecuteChange(execute(authority, "ee-"+strconv.Itoa(i), id)); !out.Success() {
			t.Fatalf("execute %s %+v", id, out)
		}
	}
	if got := countPrefix(kv, proposalPrefix); got != 3*each {
		t.Fatalf("%d proposal records", got)
	}
	if got := countPrefix(kv, openPrefix); got != 0 {
		t.Fatalf("%d open counts after only terminal proposals", got)
	}
	st := loadAt(t, kv, testHeight, now)
	if out := st.ProposeChange(propose(seats[0].Signer, "fresh", st.PolicyVersion(), governedMsg("max_page_limit", 7))); !out.Success() {
		t.Fatalf("new proposal after %d terminal proposals %+v", 3*each, out)
	}
}

// rotate puts seat i%4 first, so queueAt proposes from every seat in turn.
func rotate(seats []testSeat, i int) []testSeat {
	out := make([]testSeat, 0, len(seats))
	for j := range seats {
		out = append(out, seats[(i+j)%len(seats)])
	}
	return out
}

// One seat at its open quota gets BOUNDS; another seat can still propose; a
// slot comes back when one of its proposals leaves OPEN. Fails on the old
// code: one seat could open all 128 proposals and lock out the others.
func TestSeatQuotaIsPerSeat(t *testing.T) {
	kv, seats, _ := readyStore(t)
	proposeFrom := func(seat testSeat, id string, expiry int64, now int64) Outcome {
		st := loadAt(t, kv, testHeight, now)
		msg := propose(seat.Signer, id, 1, governedMsg("max_page_limit", 10))
		msg.ExpiryUnixSeconds = expiry
		return st.ProposeChange(msg)
	}
	for i := 0; i < MaxOpenProposalsPerSeat-1; i++ {
		expiry := testExpiry
		if i == 0 {
			expiry = testTime + 5
		}
		if out := proposeFrom(seats[0], "q-"+strconv.Itoa(i), expiry, testTime); !out.Success() {
			t.Fatalf("open %d %+v", i, out)
		}
	}
	if out := proposeFrom(seats[0], "q-limit", testExpiry, testTime); !out.Success() {
		t.Fatalf("the %dth open proposal %+v", MaxOpenProposalsPerSeat, out)
	}
	if got := openOf(t, kv, seats[0].ID); got != MaxOpenProposalsPerSeat {
		t.Fatal(got)
	}
	before := dumpKV(kv)
	if out := proposeFrom(seats[0], "q-over", testExpiry, testTime); out.Code != CodeBounds {
		t.Fatalf("over the seat quota %+v", out)
	}
	wantUnchanged(t, kv, before, "seat quota reject")
	for s := 1; s < 4; s++ {
		for i := 0; i < MaxOpenProposalsPerSeat; i++ {
			if out := proposeFrom(seats[s], "s"+strconv.Itoa(s)+"-"+strconv.Itoa(i), testExpiry, testTime); !out.Success() {
				t.Fatalf("seat %d proposal %d %+v", s, i, out)
			}
		}
	}
	var total uint64
	for _, seat := range seats {
		total += openOf(t, kv, seat.ID)
	}
	if total != 4*MaxOpenProposalsPerSeat {
		t.Fatalf("%d open proposals in all", total)
	}
	before = dumpKV(kv)
	if out := proposeFrom(seats[3], "s3-over", testExpiry, testTime); out.Code != CodeBounds {
		t.Fatalf("seat 4 over its quota %+v", out)
	}
	wantUnchanged(t, kv, before, "seat 4 quota reject")
	// q-0 expires; an approve marks it EXPIRED and frees one slot of seat-1.
	late := testTime + 10
	if out := loadAt(t, kv, testHeight, late).ApproveChange(approve(seats[1].Signer, "a-q0", "q-0")); out.Code != CodeExpired || !out.Keep {
		t.Fatalf("expire q-0 %+v", out)
	}
	if got := openOf(t, kv, seats[0].ID); got != MaxOpenProposalsPerSeat-1 {
		t.Fatal(got)
	}
	if out := proposeFrom(seats[0], "q-again", testExpiry, late); !out.Success() {
		t.Fatalf("freed slot %+v", out)
	}
}

// The open count changes once per proposal, only when it leaves OPEN or
// QUEUED. Fails on the old code, which had no open count; a decrement on
// the EXECUTED->EXPIRED relabel fails here with INTEGRITY (underflow).
func TestOpenCountBookkeeping(t *testing.T) {
	kv, seats, authority := readyStore(t)
	seat := seats[0].ID
	at := func(unix int64) *State { return loadAt(t, kv, testHeight, unix) }
	near := func(id string, msg *authorityv1.MsgProposeChange, expiry int64) {
		st := at(testTime)
		p := propose(seats[0].Signer, id, st.PolicyVersion(), msg)
		p.ExpiryUnixSeconds = expiry
		if out := st.ProposeChange(p); !out.Success() {
			t.Fatalf("propose %s %+v", id, out)
		}
	}
	approveAll := func(id string, unix int64) {
		for i := 0; i < ApprovedThreshold; i++ {
			if out := at(unix).ApproveChange(approve(seats[i].Signer, "ap-"+id+"-"+strconv.Itoa(i), id)); !out.Success() {
				t.Fatalf("approve %s %+v", id, out)
			}
		}
	}
	want := func(step string, n uint64) {
		t.Helper()
		if got := openOf(t, kv, seat); got != n {
			t.Fatalf("%s: open count %d, want %d", step, got, n)
		}
	}

	near("a", governedMsg("max_page_limit", 10), testTime+100)
	want("propose a", 1)
	approveAll("a", testTime)
	want("queue a", 1)
	if out := at(testTime).ExecuteChange(execute(authority, "ex-a", "a")); !out.Success() {
		t.Fatal(out)
	}
	want("execute a", 0)
	if out := at(testTime + 100).ExecuteChange(execute(authority, "ex-a2", "a")); out.Code != CodeExpired || !out.Keep {
		t.Fatalf("relabel %+v", out)
	}
	if recordOf(t, kv, "a").state() != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
		t.Fatal("EXECUTED proposal was not relabelled EXPIRED")
	}
	want("EXECUTED -> EXPIRED relabel", 0)

	near("b", governedMsg("max_page_limit", 11), testTime+200)
	approveAll("b", testTime)
	want("queue b", 1)
	if out := at(testTime + 200).ExecuteChange(execute(authority, "ex-b", "b")); out.Code != CodeExpired || !out.Keep {
		t.Fatal(out)
	}
	want("QUEUED -> EXPIRED", 0)

	near("c", governedMsg("max_page_limit", 12), testTime+300)
	want("propose c", 1)
	if out := at(testTime + 300).ApproveChange(approve(seats[1].Signer, "ap-c", "c")); out.Code != CodeExpired || !out.Keep {
		t.Fatal(out)
	}
	want("OPEN -> EXPIRED", 0)
	if out := at(testTime + 300).ExecuteChange(execute(authority, "ex-c", "c")); out.Code != CodeExpired || !out.Keep {
		t.Fatal(out)
	}
	want("EXPIRED -> EXPIRED", 0)

	near("d", removalMsg("v1", pub(1), 100), testTime+400)
	approveAll("d", testTime)
	if out := at(testTime).ExecuteChange(execute(authority, "ex-d", "d")); out.Code != CodeForbidden || !out.Keep {
		t.Fatal(out)
	}
	want("QUEUED -> REJECTED", 0)
	if out := at(testTime + 400).ExecuteChange(execute(authority, "ex-d2", "d")); out.Code != CodeExpired || !out.Keep {
		t.Fatal(out)
	}
	want("REJECTED -> EXPIRED relabel", 0)

	// A missing count on a QUEUED proposal is integrity damage, and the
	// execute writes nothing.
	near("e", governedMsg("max_page_limit", 13), testTime+500)
	approveAll("e", testTime)
	kv.Delete(openKey(seat, recordOf(t, kv, "e").BasePolicyVersion))
	before := dumpKV(kv)
	if out := at(testTime).ExecuteChange(execute(authority, "ex-e", "e")); out.Code != CodeIntegrity {
		t.Fatalf("underflow %+v", out)
	}
	wantUnchanged(t, kv, before, "open count underflow")
}

// A third approval whose execute-after time would pass the unix bound is
// OVERFLOW. Covers the round-2 bound that had no test; fails if the guard is
// removed (the proposal would queue with execute_after past unix max).
func TestApproveThresholdOverflow(t *testing.T) {
	for _, row := range []struct {
		name  string
		delay int64
		code  string
	}{
		{"one second over", unixMax - testTime + 1, CodeOverflow},
		{"exactly unix max", unixMax - testTime, ""},
	} {
		kv, seats, _ := readyStore(t)
		mutateConfig(t, kv, func(cfg *authorityv1.GenesisState) { cfg.DevnetTestParameters[0].DelaySeconds = row.delay })
		st := loadAt(t, kv, testHeight, testTime)
		if out := st.ProposeChange(propose(seats[0].Signer, "p-ov", 1, governedMsg("max_page_limit", 10))); !out.Success() {
			t.Fatal(out)
		}
		for i := 0; i < 2; i++ {
			if out := st.ApproveChange(approve(seats[i].Signer, "a"+strconv.Itoa(i), "p-ov")); !out.Success() {
				t.Fatalf("%s approval %d %+v", row.name, i, out)
			}
		}
		before := dumpKV(kv)
		out := st.ApproveChange(approve(seats[2].Signer, "a2", "p-ov"))
		if out.Code != row.code {
			t.Fatalf("%s: %+v", row.name, out)
		}
		rec := recordOf(t, kv, "p-ov")
		if row.code != "" {
			wantUnchanged(t, kv, before, row.name)
			if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN || len(rec.Approvers) != 2 {
				t.Fatalf("%s: state %v approvers %d", row.name, rec.state(), len(rec.Approvers))
			}
			continue
		}
		if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED || rec.ExecuteAfterUnixSeconds != unixMax {
			t.Fatalf("%s: state %v after %d", row.name, rec.state(), rec.ExecuteAfterUnixSeconds)
		}
	}
}

// After a seat key rotates, the same seat cannot approve again with the new
// key. Fails on the old code, which deduplicated by key and queued the
// proposal with two seats.
func TestApprovalDedupesBySeatAfterRotation(t *testing.T) {
	kv, seats, _ := readyStore(t)
	st := loadAt(t, kv, testHeight, testTime)
	if out := st.ProposeChange(propose(seats[0].Signer, "p-rot", 1, governedMsg("max_page_limit", 10))); !out.Success() {
		t.Fatal(out)
	}
	if out := st.ApproveChange(approve(seats[0].Signer, "a-1", "p-rot")); !out.Success() {
		t.Fatal(out)
	}
	if out := loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[0].ID, pub(40)); !out.Success() {
		t.Fatal(out)
	}
	rotated := loadAt(t, kv, testHeight, testTime)
	before := dumpKV(kv)
	if out := rotated.ApproveChange(approve(SeatAddress(pub(40)), "a-1b", "p-rot")); out.Code != CodeReplay {
		t.Fatalf("rotated seat approved again %+v", out)
	}
	wantUnchanged(t, kv, before, "rotated seat replay")
	if out := rotated.ApproveChange(approve(seats[0].Signer, "a-old", "p-rot")); out.Code != CodeUnauthorized {
		t.Fatalf("retired seat key still signs %+v", out)
	}
	for i := 1; i < 3; i++ {
		if out := rotated.ApproveChange(approve(seats[i].Signer, "a-"+strconv.Itoa(i+1), "p-rot")); !out.Success() {
			t.Fatal(out)
		}
	}
	rec := recordOf(t, kv, "p-rot")
	if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED || len(rec.Approvers) != 3 || len(rec.ApproverKeys) != 3 {
		t.Fatalf("state %v approvers %v keys %d", rec.state(), rec.Approvers, len(rec.ApproverKeys))
	}
	for _, used := range [][]byte{seats[0].Pub, seats[1].Pub, seats[2].Pub} {
		found := false
		for _, have := range rec.ApproverKeys {
			found = found || bytes.Equal(have, used)
		}
		if !found {
			t.Fatalf("approver keys do not record the key %x", used)
		}
	}
}

// An add that reuses another validator's key is DUPLICATE_ID; the proposal
// stays QUEUED. Coverage for a round-2 arm with no executed test; fails if
// the duplicate arm in projectAll is removed.
func TestDuplicateConsensusKey(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "dup", powerMsg("v5", pub(1), 1, 30))
	before := dumpKV(kv)
	out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-dup", "dup"))
	if out.Code != CodeDuplicate || out.Keep {
		t.Fatalf("%+v", out)
	}
	wantUnchanged(t, kv, before, "duplicate key")
}

// Raising one of four power-1 validators to 2 breaks max*3 < total: FORBIDDEN
// and REJECTED. Fails if the share clause of powerInvariant is dropped
// (round-2 mutant that passed every unit test).
func TestShareCapRejects(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "share", powerMsg("v1", pub(1), 2, 30))
	out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-share", "share"))
	if out.Code != CodeForbidden || !out.Keep {
		t.Fatalf("%+v", out)
	}
	if recordOf(t, kv, "share").state() != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED || len(pendingOf(t, kv)) != 0 {
		t.Fatal("share-cap change was scheduled")
	}
}

// A total above the safe integer maximum is FORBIDDEN and REJECTED, as spec
// power_set_invariant says. Fails on the old code: OVERFLOW, proposal QUEUED.
func TestPowerTotalAboveSafeMax(t *testing.T) {
	gen, seats, authority := readyGenesis()
	const big = int64(1)<<51 - 1
	for _, row := range gen.Authority.Validators {
		row.Power = big
	}
	kv := storeFrom(t, gen)
	queueAt(t, kv, seats, testHeight, testTime, "big", powerMsg("v5", pub(9), big, 30))
	out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-big", "big"))
	if out.Code != CodeForbidden || !out.Keep {
		t.Fatalf("%+v", out)
	}
	if recordOf(t, kv, "big").state() != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED || len(pendingOf(t, kv)) != 0 {
		t.Fatal("over-total change was scheduled")
	}
}

// A power-only change keeps the key and changes one power at the activation
// height. Requirement 2; no round-2 test executed it (coverage: round-2
// behaviour was the same).
func TestPowerOnlyChange(t *testing.T) {
	gen, seats, authority := readyGenesis()
	for _, row := range gen.Authority.Validators {
		row.Power = 2
	}
	kv := storeFrom(t, gen)
	queueAt(t, kv, seats, testHeight, testTime, "lower", powerMsg("v1", pub(1), 1, 20))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-lower", "lower")); !out.Success() {
		t.Fatal(out)
	}
	if len(pendingOf(t, kv)) != 1 {
		t.Fatal("power-only change was not scheduled")
	}
	if _, changed, out := loadAt(t, kv, 19, testTime).ApplyDue(); changed || !out.Success() {
		t.Fatal("applied before its height")
	}
	st := loadAt(t, kv, 20, testTime)
	before, changed, out := st.ApplyDue()
	if !changed || !out.Success() {
		t.Fatal(out)
	}
	for _, row := range st.Validators() {
		want := int64(2)
		if row.GetNodeId() == "v1" {
			want = 1
			if !bytes.Equal(row.GetConsensusPubkey(), pub(1)) {
				t.Fatal("power-only change replaced the key")
			}
		}
		if row.GetPower() != want {
			t.Fatalf("%s power %d", row.GetNodeId(), row.GetPower())
		}
	}
	updates, err := Changeset(before, st.Validators())
	if err != nil || len(updates) != 1 || updates[0].Power != 1 || !bytes.Equal(updates[0].PubKey.GetEd25519(), pub(1)) {
		t.Fatalf("updates %+v %v", updates, err)
	}
}

// A due entry that would leave three validators halts ApplyDue with
// FORBIDDEN and writes nothing. Fails if ApplyDue stops checking the
// resulting set (the entry can only come from a damaged store).
func TestApplyDueHaltsOnBrokenSet(t *testing.T) {
	kv, _, _ := readyStore(t)
	putPendingRaw(t, kv, heightEntry("bad-rm", "v1", pub(1), 0, 1))
	before := dumpKV(kv)
	_, changed, out := loadAt(t, kv, 1, testTime).ApplyDue()
	if changed || out.Code != CodeForbidden {
		t.Fatalf("%+v changed %v", out, changed)
	}
	wantUnchanged(t, kv, before, "halt on a broken set")
}

// A due entry that gives two validators the same consensus key halts
// ApplyDue with DUPLICATE_ID, and a due removal of a node that is not in the
// set halts with NOT_FOUND. Nothing is written, the entry stays, and the
// keeper's EndBlock returns the matching svn error, so FinalizeBlock fails.
// Execute's projection refuses both, so only a damaged store holds them.
// The second fix-round review found that deleting the duplicate-key loop
// passed every suite.
func TestApplyDueHaltsOnDamagedEntries(t *testing.T) {
	for _, row := range []struct {
		name  string
		code  string
		err   error
		entry *authorityv1.PendingValidatorPower
	}{
		{"add with v1's key", CodeDuplicate, svnerr.ErrDuplicate, heightEntry("dup-add", "v5", pub(1), 1, 1)},
		{"key change onto v2's key", CodeDuplicate, svnerr.ErrDuplicate, heightEntry("dup-key", "v1", pub(2), 1, 1)},
		{"removal of a node not in the set", CodeNotFound, svnerr.ErrNotFound, heightEntry("rm-none", "v9", pub(9), 0, 1)},
	} {
		kv, _, _ := readyStore(t)
		putPendingRaw(t, kv, row.entry)
		before := dumpKV(kv)
		_, changed, out := loadAt(t, kv, 1, testTime).ApplyDue()
		if changed || out.Code != row.code {
			t.Fatalf("%s: %+v changed %v", row.name, out, changed)
		}
		wantUnchanged(t, kv, before, row.name)

		env := readyKeeper(t)
		putPendingRaw(t, env.kv(), row.entry)
		before = dumpKV(env.kv())
		env.height = 1
		if updates, err := env.keeper.EndBlock(env.blockCtx()); !errors.Is(err, row.err) || len(updates) != 0 {
			t.Fatalf("%s: EndBlock %v %v", row.name, updates, err)
		}
		wantUnchanged(t, env.kv(), before, row.name+" (EndBlock)")
	}
}

// A council proposal that reuses the proposal id of a pending genesis entry
// is CONFLICT at execute, with the same key and with another key. Fails on
// the old code, which scheduled both entries.
func TestPendingKeyCollisionConflicts(t *testing.T) {
	for _, height := range []uint64{50, 60} {
		gen, seats, authority := readyGenesis()
		gen.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("x", "v5", pub(9), 1, 50)}
		kv := storeFrom(t, gen)
		queueAt(t, kv, seats, testHeight, testTime, "x", powerMsg("v6", pub(10), 1, height))
		before := dumpKV(kv)
		out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-x", "x"))
		if out.Code != CodeConflict || out.Keep {
			t.Fatalf("height %d: %+v", height, out)
		}
		wantUnchanged(t, kv, before, "pending id collision")
		if recordOf(t, kv, "x").state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatal("collision moved the proposal")
		}
	}
}

// The council admit, disable and force-withdraw payloads run through the
// registry hooks (DEC-009); x/providers and x/content own the records, so
// this test uses the deterministic specRegistry. Every reject leaves the
// whole authority store byte-identical (dumpKV) and the registry unchanged.
// Ported from the round-2 admission test (TestAdmissionAndMixedActivation
// in goal 07); fails if a payload stops calling the registry, passes the
// wrong DEC-002 or DEC-003 flag, or writes on a reject.
func TestAdmitAndDisableRows(t *testing.T) {
	gen, seats, authority := readyGenesis()
	gen.DevnetTestParameters = append(gen.DevnetTestParameters,
		devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
	kv := storeFrom(t, gen)
	newRegistry := func() *specRegistry {
		r := emptySpecRegistry()
		r.providers = map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"}
		r.groups = map[string]string{"p1": "devnet-test-only", "p2": "other-group", "p3": "devnet-test-only"}
		r.contents = map[string]uint64{"c1": 1}
		return r
	}
	registry := newRegistry()
	admit := func(id string) *authorityv1.MsgProposeChange { return admitProposal(id) }
	disable := func(id string) *authorityv1.MsgProposeChange { return disableProposal(id) }
	reason := pub(7)
	withdraw := func(id string) *authorityv1.MsgProposeChange { return withdrawProposal(id, 1, reason) }
	run := func(id string, msg *authorityv1.MsgProposeChange) Outcome {
		queueAt(t, kv, seats, testHeight, testTime, id, msg)
		registry.calls = nil
		return executeWith(t, kv, registry, testTime, authority, "ex-"+id, id)
	}
	validators := loadAt(t, kv, testHeight, testTime).Validators()
	executed := func(id string) {
		t.Helper()
		if got := recordOf(t, kv, id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
			t.Fatalf("%s is %v", id, got)
		}
		if !sameValidators(loadAt(t, kv, testHeight, testTime).Validators(), validators) || countPrefix(kv, pendingPrefix) != 0 {
			t.Fatalf("%s changed the validator set or scheduled a power change", id)
		}
	}

	if out := run("ad-p1", admit("p1")); !out.Success() || registry.providers["p1"] != "ADMITTED" {
		t.Fatalf("admit %+v %s", out, registry.providers["p1"])
	}
	if !sameCalls(registry.calls, []registryCall{{Method: "admit", ID: "p1", Devnet: true}}) {
		t.Fatalf("admit calls %+v", registry.calls)
	}
	executed("ad-p1")
	if out := run("di-p1", disable("p1")); !out.Success() || registry.providers["p1"] != "DISABLED" {
		t.Fatalf("disable %+v %s", out, registry.providers["p1"])
	}
	if !sameCalls(registry.calls, []registryCall{{Method: "disable", ID: "p1"}}) {
		t.Fatalf("disable calls %+v", registry.calls)
	}
	executed("di-p1")
	saved := registry.clone()
	if out := run("ad-p2", admit("p2")); out.Code != CodeForbidden || !out.Keep || registry.providers["p2"] != "APPLIED" {
		t.Fatalf("group mismatch %+v", out)
	}
	if recordOf(t, kv, "ad-p2").state() != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED || !registry.equal(saved) {
		t.Fatal("group mismatch was not REJECTED, or wrote to the registry")
	}
	rejects := []struct {
		id   string
		msg  *authorityv1.MsgProposeChange
		code string
	}{
		{"ad-p9", admit("p9"), CodeNotFound},
		{"di-p3", disable("p3"), CodeConflict},
		{"fw-c404", withdraw("c404"), CodeNotFound},
	}
	for _, r := range rejects {
		queueAt(t, kv, seats, testHeight, testTime, r.id, r.msg)
		before, saved := dumpKV(kv), registry.clone()
		if out := executeWith(t, kv, registry, testTime, authority, "ex-"+r.id, r.id); out.Code != r.code || out.Keep {
			t.Fatalf("%s: %+v", r.id, out)
		}
		wantUnchanged(t, kv, before, r.id)
		if !registry.equal(saved) {
			t.Fatalf("%s wrote to the registry", r.id)
		}
		if got := recordOf(t, kv, r.id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatalf("%s is %v", r.id, got)
		}
	}
	if out := run("fw-c1", withdraw("c1")); !out.Success() || !registry.withdrawn["c1"] {
		t.Fatalf("force-withdraw %+v %v", out, registry.withdrawn)
	}
	if !sameCalls(registry.calls, []registryCall{{Method: "force_withdraw", ID: "c1", Version: 1, Reason: reason, Devnet: true}}) {
		t.Fatalf("force-withdraw calls %+v", registry.calls)
	}
	executed("fw-c1")

	blocked, seats2, authority2 := readyGenesis()
	kv2 := storeFrom(t, blocked)
	registry2 := newRegistry()
	queueAt(t, kv2, seats2, testHeight, testTime, "ad-p1", admit("p1"))
	before, saved2 := dumpKV(kv2), registry2.clone()
	if out := executeWith(t, kv2, registry2, testTime, authority2, "ex-ad", "ad-p1"); out.Code != CodeDecisionBlocked || out.Keep {
		t.Fatalf("admit without DEC-002 %+v", out)
	}
	if !sameCalls(registry2.calls, []registryCall{{Method: "admit", ID: "p1", Devnet: false}}) {
		t.Fatalf("admit without DEC-002 calls %+v", registry2.calls)
	}
	wantUnchanged(t, kv2, before, "admit without DEC-002")
	if !registry2.equal(saved2) {
		t.Fatal("admit without DEC-002 wrote to the registry")
	}
}

// A stored value that does not decode is INTEGRITY inside a message, and
// nothing is written. Fails if a record decode error is ignored.
func TestDamagedRecordIsIntegrity(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "dmg", governedMsg("max_page_limit", 10))
	raw := kv.Get(proposalKey("dmg"))
	kv.Set(proposalKey("dmg"), append(append([]byte(nil), raw...), ' '))
	before := dumpKV(kv)
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-dmg", "dmg")); out.Code != CodeIntegrity {
		t.Fatalf("%+v", out)
	}
	wantUnchanged(t, kv, before, "damaged proposal record")
	kv.Set(businessKey("b-short"), []byte{1})
	short := propose(seats[1].Signer, "p-short", 1, governedMsg("max_page_limit", 10))
	short.BusinessId = "b-short"
	before = dumpKV(kv)
	if out := loadAt(t, kv, testHeight, testTime).ProposeChange(short); out.Code != CodeIntegrity {
		t.Fatalf("short business digest %+v", out)
	}
	wantUnchanged(t, kv, before, "damaged business digest")
	kv.Set([]byte{configKey}, []byte{0xff})
	if _, err := LoadState(kv, testChainID, testHeight, testTime); err == nil {
		t.Fatal("damaged config loaded")
	}
}
