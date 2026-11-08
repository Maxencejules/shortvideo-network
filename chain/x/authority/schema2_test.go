package authority

import (
	"bytes"
	"crypto/sha256"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Engine-level tests of the schema version 2 payloads (goal 10, owner
// ruling Q1 "Minimal additive (Recommended)"; Q3 "3 of 4 + min notice
// (Recommended)", whose numbers MinActivationLeadBlocks and
// MaxActivationLeadBlocks remain PROPOSED). Each reject row asserts its
// exact code and a byte-identical authority store; the policy and account
// hooks are the unit-test fakes of hooks_test.go. The keeper-level tests
// with the real x/policy keeper are in policy_keeper_test.go.

const (
	minLead = msgkit.MinActivationLeadBlocks
	maxLead = msgkit.MaxActivationLeadBlocks
	h       = uint64(testHeight)
)

// upgradeDigest is a synthetic 32-byte release digest.
var upgradeDigest = sha256Digest(pub(41)).GetValue()

// otherDigest is a second synthetic 32-byte release digest.
var otherDigest = sha256Digest(pub(42)).GetValue()

// proposeRow is one propose of a schema version 2 payload and its code.
type proposeRow struct {
	name string
	msg  *authorityv1.MsgProposeChange
	want string
}

// runProposeRows proposes each row on a fresh ready store at testHeight. A
// refusal must leave the store byte-identical; an acceptance must store the
// record OPEN.
func runProposeRows(t *testing.T, rows []proposeRow) {
	t.Helper()
	for i, r := range rows {
		kv, seats, _ := readyStore(t)
		before := dumpKV(kv)
		id := "s2-" + strconv.Itoa(i)
		got := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, id, 1, r.msg))
		if got.Code != r.want || got.Keep || got.Idempotent {
			t.Fatalf("%s: %+v, want %q", r.name, got, r.want)
		}
		if r.want != "" {
			wantUnchanged(t, kv, before, r.name)
			continue
		}
		if rec := recordOf(t, kv, id); rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN || rec.ParameterKey != r.msg.GetParameterKey() {
			t.Fatalf("%s: stored %+v", r.name, rec)
		}
	}
}

// withKey returns msg with its parameter key replaced.
func withKey(msg *authorityv1.MsgProposeChange, key string) *authorityv1.MsgProposeChange {
	msg.ParameterKey = key
	return msg
}

// TestScheduledGovernedLimitAtPropose (D-4, D-5): a governed limit with an
// activation height is accepted when the height is in (h, h + MaxLead] of
// the proposing block (OUT_OF_RANGE at or below, BOUNDS above or above
// safe_integer_max); activation 0 is the v0 form, applied at execute. The
// stored record keeps the activation height and the canonical text carries
// activationHeight only when it is not 0.
func TestScheduledGovernedLimitAtPropose(t *testing.T) {
	key := msgkit.LimitMaxOpenOffers
	runProposeRows(t, []proposeRow{
		{"activation 0 (v0 form)", scheduledMsg(key, 8, 0), ""},
		{"activation h-1", scheduledMsg(key, 8, h-1), CodeOutOfRange},
		{"activation h", scheduledMsg(key, 8, h), CodeOutOfRange},
		{"activation h+1", scheduledMsg(key, 8, h+1), ""},
		{"activation h+max", scheduledMsg(key, 8, h+maxLead), ""},
		{"activation h+max+1", scheduledMsg(key, 8, h+maxLead+1), CodeBounds},
		{"activation above safe_integer_max", scheduledMsg(key, 8, uint64(safeMax)+1), CodeBounds},
		{"activation 2^64-1", scheduledMsg(key, 8, math.MaxUint64), CodeBounds},
		// D-1 comes first: an over-ceiling value with an activation at
		// the height is BOUNDS, not OUT_OF_RANGE.
		{"over the ceiling, activation h", rawScheduled(key, 1025, h), CodeBounds},
		// The window comes after the payload checks.
		{"parameter key of another limit, activation h", withKey(scheduledMsg(key, 8, h), msgkit.LimitMaxPageLimit), CodeMalformed},
		{"digest of another payload, activation h", func() *authorityv1.MsgProposeChange {
			m := scheduledMsg(key, 8, h)
			m.PayloadDigest = sha256Digest(pub(5))
			return m
		}(), CodeIntegrity},
	})

	kv, seats, _ := readyStore(t)
	st := loadAt(t, kv, testHeight, testTime)
	if out := st.ProposeChange(propose(seats[0].Signer, "sched", 1, scheduledMsg(key, 8, h+40))); !out.Success() {
		t.Fatal(out)
	}
	rec := recordOf(t, kv, "sched")
	if rec.Decoded.Activation != h+40 || rec.Decoded.LimitValue != 8 ||
		rec.Payload != `{"governedLimit":{"activationHeight":"45","limitKey":"max_open_offers","value":"8"}}` {
		t.Fatalf("record %+v %s", rec.Decoded, rec.Payload)
	}
	if out := st.ProposeChange(propose(seats[0].Signer, "now", 1, scheduledMsg(key, 8, 0))); !out.Success() {
		t.Fatal(out)
	}
	if rec := recordOf(t, kv, "now"); rec.Decoded.Activation != 0 || rec.Payload != `{"governedLimit":{"limitKey":"max_open_offers","value":"8"}}` {
		t.Fatalf("v0 form %+v %s", rec.Decoded, rec.Payload)
	}

	// The window wins over every later propose check.
	laterChecks(t, func() *authorityv1.MsgProposeChange { return scheduledMsg(key, 8, h) }, CodeOutOfRange)
}

// rawScheduled is scheduledMsg for a value the chain refuses at D-1.
func rawScheduled(key string, value, activation uint64) *authorityv1.MsgProposeChange {
	canon := `{"governedLimit":{"activationHeight":"` + strconv.FormatUint(activation, 10) + `","limitKey":"` + key +
		`","value":"` + strconv.FormatUint(value, 10) + `"}}`
	return rawPayloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
		GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value, ActivationHeight: activation},
	}}, key, canon)
}

// laterChecks proposes bad() under each later failing condition (expired
// expiry, stale base, duplicate id, replayed business id) on one store and
// requires want each time with the store unchanged: the schema version 2
// checks run right after the payload checks, as D-1 does.
func laterChecks(t *testing.T, bad func() *authorityv1.MsgProposeChange, want string) {
	t.Helper()
	kv, seats, _ := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "taken", governedMsg(msgkit.LimitMaxPageLimit, 10))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(bytes20(1), "ex-taken", "taken")); !out.Success() {
		t.Fatal(out)
	}
	rows := []struct {
		name string
		msg  *authorityv1.MsgProposeChange
	}{
		{"expired expiry", func() *authorityv1.MsgProposeChange {
			m := propose(seats[1].Signer, "late", 2, bad())
			m.ExpiryUnixSeconds = testTime
			return m
		}()},
		{"stale base", propose(seats[1].Signer, "stale", 1, bad())},
		{"duplicate proposal id", func() *authorityv1.MsgProposeChange {
			m := propose(seats[1].Signer, "taken", 2, bad())
			m.BusinessId = "b-dup"
			return m
		}()},
		{"replayed business id", func() *authorityv1.MsgProposeChange {
			m := propose(seats[1].Signer, "replay", 2, bad())
			m.BusinessId = "b-taken"
			return m
		}()},
	}
	for _, r := range rows {
		wantRefused(t, kv, r.name, want, func() Outcome { return loadAt(t, kv, testHeight, testTime).ProposeChange(r.msg) })
	}
}

// TestSoftwareUpgradeAtPropose (D-5, Q3 (a)): the name must match the id
// pattern and the release digest must name SHA-256 (MALFORMED) and hold 32
// bytes (BOUNDS), at parse; the parameter key is "upgrade" (MALFORMED
// otherwise); the plan height must be in [h + MinLead, h + MaxLead] of the
// proposing block and at most safe_integer_max (OUT_OF_RANGE below, BOUNDS
// above), checked after the parameter key binding.
func TestSoftwareUpgradeAtPropose(t *testing.T) {
	upgrade := func(name string, height uint64, digest *typesv1.Digest) *authorityv1.MsgProposeChange {
		return rawPayloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_SoftwareUpgrade{
			SoftwareUpgrade: &authorityv1.SoftwareUpgradeChange{Name: name, Height: height, ReleaseDigest: digest},
		}}, upgradeParameterKey, "{}")
	}
	sha := func(value []byte) *typesv1.Digest {
		return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: value}
	}
	runProposeRows(t, []proposeRow{
		{"empty name", upgrade("", h+minLead, sha(upgradeDigest)), CodeBounds},
		{"upper-case name", upgrade("Up-1", h+minLead, sha(upgradeDigest)), CodeBounds},
		{"65-character name", upgrade(strings.Repeat("u", 65), h+minLead, sha(upgradeDigest)), CodeBounds},
		{"no release digest", upgrade("up-1", h+minLead, nil), CodeMalformed},
		{"release digest without algorithm", upgrade("up-1", h+minLead, &typesv1.Digest{Value: upgradeDigest}), CodeMalformed},
		{"31-byte release digest", upgrade("up-1", h+minLead, sha(upgradeDigest[:31])), CodeBounds},
		{"33-byte release digest", upgrade("up-1", h+minLead, sha(append(bytes.Clone(upgradeDigest), 0))), CodeBounds},
		{"height 0", upgradeMsg("up-1", 0, upgradeDigest), CodeOutOfRange},
		{"height h", upgradeMsg("up-1", h, upgradeDigest), CodeOutOfRange},
		{"height h+min-1", upgradeMsg("up-1", h+minLead-1, upgradeDigest), CodeOutOfRange},
		{"height h+min", upgradeMsg("up-1", h+minLead, upgradeDigest), ""},
		{"height h+max", upgradeMsg("up-1", h+maxLead, upgradeDigest), ""},
		{"height h+max+1", upgradeMsg("up-1", h+maxLead+1, upgradeDigest), CodeBounds},
		{"height above safe_integer_max", upgradeMsg("up-1", uint64(safeMax)+1, upgradeDigest), CodeBounds},
		{"parameter key upgrade-1", withKey(upgradeMsg("up-1", h+minLead, upgradeDigest), "upgrade-1"), CodeMalformed},
		{"parameter key of a seat", withKey(upgradeMsg("up-1", h+minLead, upgradeDigest), "seat-1"), CodeMalformed},
		{"parameter key bound, height below the window", withKey(upgradeMsg("up-1", h, upgradeDigest), "up-1"), CodeMalformed},
	})

	kv, seats, _ := readyStore(t)
	if out := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, "up", 1, upgradeMsg("up-1", 300, upgradeDigest))); !out.Success() {
		t.Fatal(out)
	}
	rec := recordOf(t, kv, "up")
	if rec.Decoded.Kind != kindSoftwareUpgrade || rec.Decoded.UpgradeName != "up-1" || rec.Decoded.UpgradeHeight != 300 ||
		!bytes.Equal(rec.Decoded.UpgradeDigest, upgradeDigest) || rec.ParameterKey != upgradeParameterKey {
		t.Fatalf("record %+v", rec.Decoded)
	}
	laterChecks(t, func() *authorityv1.MsgProposeChange { return upgradeMsg("up-1", h+minLead-1, upgradeDigest) }, CodeOutOfRange)
}

// TestCancelUpgradeAtPropose (D-5): the name must match the id pattern
// (BOUNDS) and the parameter key is "upgrade" (MALFORMED otherwise). No
// state is read at propose: a cancel of a plan that does not exist is
// accepted and refused at execute.
func TestCancelUpgradeAtPropose(t *testing.T) {
	cancel := func(name string) *authorityv1.MsgProposeChange {
		return rawPayloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_CancelUpgrade{
			CancelUpgrade: &authorityv1.CancelUpgradeChange{Name: name},
		}}, upgradeParameterKey, "{}")
	}
	runProposeRows(t, []proposeRow{
		{"empty name", cancel(""), CodeBounds},
		{"upper-case name", cancel("UP"), CodeBounds},
		{"parameter key cancel", withKey(cancelMsg("up-1"), "cancel"), CodeMalformed},
		{"no plan pending", cancelMsg("up-1"), ""},
	})
}

// TestCouncilSeatAtPropose (D-5): the seat id must match the id pattern and
// the key must be 32 bytes (BOUNDS); the parameter key is seat-<seat_id>
// (MALFORMED otherwise). No state is read at propose: an absent seat is
// accepted and refused at execute.
func TestCouncilSeatAtPropose(t *testing.T) {
	seat := func(id string, key []byte) *authorityv1.MsgProposeChange {
		return rawPayloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_CouncilSeat{
			CouncilSeat: &authorityv1.CouncilSeatChange{SeatId: id, NewPublicKey: key},
		}}, "seat-"+id, "{}")
	}
	runProposeRows(t, []proposeRow{
		{"empty seat id", seat("", pub(80)), CodeBounds},
		{"upper-case seat id", seat("Seat-1", pub(80)), CodeBounds},
		{"no key", seat("seat-1", nil), CodeBounds},
		{"31-byte key", seat("seat-1", pub(80)[:31]), CodeBounds},
		{"33-byte key", seat("seat-1", append(pub(80), 0)), CodeBounds},
		{"parameter key of another seat", withKey(seatMsg("seat-1", pub(80)), "seat-seat-2"), CodeMalformed},
		{"parameter key without the prefix", withKey(seatMsg("seat-1", pub(80)), "seat-1"), CodeMalformed},
		{"absent seat", seatMsg("seat-9", pub(80)), ""},
		{"accepted", seatMsg("seat-1", pub(80)), ""},
	})
	kv, seats, _ := readyStore(t)
	if out := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, "s", 1, seatMsg("seat-2", pub(80)))); !out.Success() {
		t.Fatal(out)
	}
	if rec := recordOf(t, kv, "s"); rec.Decoded.SeatID != "seat-2" || !bytes.Equal(rec.Decoded.SeatKey, pub(80)) ||
		rec.Payload != `{"councilSeat":{"newPublicKey":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAFA=","seatId":"seat-2"}}` {
		t.Fatalf("record %+v %s", rec.Decoded, rec.Payload)
	}
}

// putQueuedPayload writes a QUEUED proposal of msg's payload on base version
// base straight into the store, approved by seats 1..3 with execute_after at
// testTime, plus the proposer's open count. It skips the propose checks
// after the payload parse, so it reaches execute-side checks no message can
// reach (a height window that only a later block could violate the other
// way). msg must parse.
func putQueuedPayload(t testing.TB, kv KV, seats []testSeat, id string, base uint64, msg *authorityv1.MsgProposeChange) {
	t.Helper()
	change, kind, canon, out := canonicalOf(msg.GetPayload())
	if !out.Success() {
		t.Fatal(out)
	}
	sum := sha256.Sum256([]byte(canon))
	approvers := []string{seats[0].ID, seats[1].ID, seats[2].ID}
	keys := [][]byte{bytes.Clone(seats[0].Pub), bytes.Clone(seats[1].Pub), bytes.Clone(seats[2].Pub)}
	slices.Sort(approvers)
	slices.SortFunc(keys, bytes.Compare)
	decoded := decodedFrom(kind, change, msg)
	rec := &proposalRecord{
		ProposalID: id, ParameterKey: parameterKey(decoded), BasePolicyVersion: base, PayloadDigest: sum[:],
		State:             int32(authorityv1.ProposalState_PROPOSAL_STATE_QUEUED),
		ExpiryUnixSeconds: testExpiry, ExecuteAfterUnixSeconds: testTime,
		ApproverKeys: keys, BusinessID: "b-" + id, Proposer: seats[0].ID, Approvers: approvers,
		Payload: canon, Decoded: decoded,
	}
	raw, err := encodeRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	kv.Set(proposalKey(id), raw)
	kv.Set(openKey(seats[0].ID, base), encodeU64(1))
}

// executeRefused executes proposal id at height and requires code, no keep,
// a byte-identical authority store, the proposal still QUEUED, no typed
// execute event, no account created and no policy writer call (an
// authority-side refusal).
func executeRefused(t *testing.T, kv KV, authority []byte, height int64, name, id, code string) {
	t.Helper()
	executeRefusal(t, kv, authority, height, name, id, code, false)
}

// executeRefusedByWriter is executeRefused for a refusal the policy writer
// returns: the writer is called, and the fake writes nothing on a refusal.
func executeRefusedByWriter(t *testing.T, kv KV, authority []byte, height int64, name, id, code string) {
	t.Helper()
	before := policyFor(t, kv).clone()
	executeRefusal(t, kv, authority, height, name, id, code, true)
	if !policyFor(t, kv).sameState(before) {
		t.Fatalf("%s: the refusing writer changed the policy state", name)
	}
}

func executeRefusal(t *testing.T, kv KV, authority []byte, height int64, name, id, code string, writer bool) {
	t.Helper()
	calls := len(policyFor(t, kv).calls)
	accounts := len(accountsFor(t, kv).created)
	var st *State
	wantRefused(t, kv, name, code, func() Outcome {
		st = loadAt(t, kv, height, testTime)
		return st.ExecuteChange(execute(authority, "ex-"+id+"-"+strconv.FormatInt(height, 10), id))
	})
	wantState(t, kv, id, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	if st.executed != nil {
		t.Fatalf("%s: executed %+v", name, st.executed)
	}
	if got := policyFor(t, kv).calls[calls:]; !writer && slices.ContainsFunc(got, func(c string) bool { return !strings.HasPrefix(c, "UpgradeApplied ") }) {
		t.Fatalf("%s: policy writer called %v", name, got)
	}
	if len(accountsFor(t, kv).created) != accounts {
		t.Fatalf("%s: an account was created", name)
	}
}

// TestScheduledGovernedLimitAtExecute (D-4): the activation height is
// checked again against the executing block: OUT_OF_RANGE at or below it,
// BOUNDS above the lead (reachable only through a record written directly,
// since a later block only narrows the upper side), both leaving the
// proposal QUEUED and calling no writer. In the window the execute calls
// SchedulePending with the policy version the execute produces; activation
// 0 calls SetLimit. At policy version 2^64-1 the execute is OVERFLOW before
// the writer is called. A writer refusal is the execute's code.
func TestScheduledGovernedLimitAtExecute(t *testing.T) {
	key := msgkit.LimitMaxOpenOffers
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "sa", scheduledMsg(key, 8, h+10))
	executeRefused(t, kv, authority, testHeight+10, "execute at the activation height", "sa", CodeOutOfRange)
	executeRefused(t, kv, authority, testHeight+11, "execute above the activation height", "sa", CodeOutOfRange)
	st := loadAt(t, kv, testHeight+9, testTime)
	if out := st.ExecuteChange(execute(authority, "ex-sa", "sa")); !out.Success() {
		t.Fatalf("execute at activation-1: %+v", out)
	}
	if p := policyFor(t, kv).pending[key]; p != (fakePending{value: 8, activation: h + 10, version: 2, proposalID: "sa"}) {
		t.Fatalf("pending %+v", p)
	}
	if _, set := policyFor(t, kv).limit(key); set {
		t.Fatal("a scheduled change set the value at execute")
	}
	if st.executed == nil || st.executed.version != 2 {
		t.Fatalf("executed %+v", st.executed)
	}

	// Activation h+max proposed and executed in the same block: accepted.
	queueAt(t, kv, seats, testHeight, testTime, "smax", scheduledMsg(key, 9, h+maxLead))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-smax", "smax")); !out.Success() {
		t.Fatalf("activation h+max: %+v", out)
	}
	if p := policyFor(t, kv).pending[key]; p.activation != h+maxLead || p.version != 3 {
		t.Fatalf("pending %+v", p)
	}

	// Activation h+max+1 at the executing block: BOUNDS.
	putQueuedPayload(t, kv, seats, "sover", 3, scheduledMsg(key, 9, h+maxLead+1))
	executeRefused(t, kv, authority, testHeight, "activation h+max+1", "sover", CodeBounds)

	// Activation 0: SetLimit.
	queueAt(t, kv, seats, testHeight, testTime, "snow", scheduledMsg(key, 7, 0))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-snow", "snow")); !out.Success() {
		t.Fatal(out)
	}
	if value, set := policyFor(t, kv).limit(key); !set || value != 7 || len(policyFor(t, kv).pending) != 0 {
		t.Fatalf("immediate change: %d %t, pending %v", value, set, policyFor(t, kv).pending)
	}

	// A writer refusal fails the execute with the writer's code.
	queueAt(t, kv, seats, testHeight, testTime, "sfail", scheduledMsg(key, 6, h+5))
	policyFor(t, kv).fail = CodeConflict
	wantRefused(t, kv, "writer refusal", CodeConflict, func() Outcome {
		return loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-sfail", "sfail"))
	})
	wantState(t, kv, "sfail", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	policyFor(t, kv).fail = ""

	// Policy version 2^64-1: OVERFLOW before SchedulePending.
	okv, oseats, oauthority := readyStore(t)
	okv.Set([]byte{policyKey}, encodeU64(math.MaxUint64))
	putQueuedPayload(t, okv, oseats, "smaxv", math.MaxUint64, scheduledMsg(key, 6, h+5))
	executeRefused(t, okv, oauthority, testHeight, "scheduled change at version 2^64-1", "smaxv", CodeOverflow)
}

// TestSoftwareUpgradeAtExecute (Q3 (a)): the plan height is checked again
// against the executing block with the minimum lead: a plan proposed at
// h + MinLead executes at h and is OUT_OF_RANGE at h+1 (the proposal stays
// QUEUED); above the maximum lead it is BOUNDS. Then an applied name is
// DUPLICATE_ID before ScheduleUpgrade is called; a name an earlier plan used
// with another digest is the writer's CONFLICT; success calls
// ScheduleUpgrade with the name, height, digest and proposal id.
func TestSoftwareUpgradeAtExecute(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "ua", upgradeMsg("up-1", h+minLead, upgradeDigest))
	executeRefused(t, kv, authority, testHeight+1, "execute one block later (lead min-1)", "ua", CodeOutOfRange)
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-ua", "ua")); !out.Success() {
		t.Fatalf("lead min: %+v", out)
	}
	plan := policyFor(t, kv).plan
	if plan == nil || plan.name != "up-1" || plan.height != h+minLead || !bytes.Equal(plan.digest, upgradeDigest) || plan.proposalID != "ua" {
		t.Fatalf("plan %+v", plan)
	}
	if calls := policyFor(t, kv).calls; !slices.Equal(calls[len(calls)-2:], []string{"UpgradeApplied up-1", "ScheduleUpgrade up-1"}) {
		t.Fatalf("calls %v", calls)
	}

	// Lead max at the executing block: accepted; max+1: BOUNDS.
	queueAt(t, kv, seats, testHeight, testTime, "umax", upgradeMsg("up-2", h+maxLead, upgradeDigest))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-umax", "umax")); !out.Success() {
		t.Fatalf("lead max: %+v", out)
	}
	putQueuedPayload(t, kv, seats, "uover", 3, upgradeMsg("up-3", h+maxLead+1, upgradeDigest))
	executeRefused(t, kv, authority, testHeight, "lead max+1", "uover", CodeBounds)

	// A name reused with another digest: the writer's CONFLICT.
	queueAt(t, kv, seats, testHeight, testTime, "ureuse", upgradeMsg("up-1", h+minLead, otherDigest))
	executeRefusedByWriter(t, kv, authority, testHeight, "name reused with another digest", "ureuse", CodeConflict)

	// An applied name: DUPLICATE_ID, and ScheduleUpgrade is not called.
	policyFor(t, kv).applied["up-4"] = true
	queueAt(t, kv, seats, testHeight, testTime, "uapplied", upgradeMsg("up-4", h+minLead, upgradeDigest))
	executeRefused(t, kv, authority, testHeight, "applied name", "uapplied", CodeDuplicate)
	if calls := policyFor(t, kv).calls; calls[len(calls)-1] != "UpgradeApplied up-4" {
		t.Fatalf("calls %v", calls)
	}
}

// TestCancelUpgradeAtExecute: a cancel with no pending plan, or of a plan of
// another name, is the writer's NOT_FOUND and leaves the proposal QUEUED; a
// cancel of the pending plan deletes it.
func TestCancelUpgradeAtExecute(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "c1", cancelMsg("up-1"))
	executeRefusedByWriter(t, kv, authority, testHeight, "no pending plan", "c1", CodeNotFound)
	queueAt(t, kv, seats, testHeight, testTime, "u2", upgradeMsg("up-2", h+minLead, upgradeDigest))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-u2", "u2")); !out.Success() {
		t.Fatal(out)
	}
	queueAt(t, kv, seats, testHeight, testTime, "c2", cancelMsg("up-1"))
	executeRefusedByWriter(t, kv, authority, testHeight, "plan of another name", "c2", CodeNotFound)
	queueAt(t, kv, seats, testHeight, testTime, "c3", cancelMsg("up-2"))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-c3", "c3")); !out.Success() {
		t.Fatal(out)
	}
	if policyFor(t, kv).plan != nil {
		t.Fatalf("plan %+v after the cancel", policyFor(t, kv).plan)
	}
}

// TestCouncilSeatAtExecute (Q1 (a); D-7; DR-10-35): every refusal of the
// protocol's council seat change row, in order, with its exact code, the
// proposal QUEUED, the store unchanged and no account created; then a
// success: the seat keeps its id with the new key, the old key is retired
// (0x15) for good, the new key's account is created, the policy version
// moves, the old key can no longer propose and the new key can.
func TestCouncilSeatAtExecute(t *testing.T) {
	kv, seats, authority := readyStore(t)
	// A submitter whose address is the account of pub(81), and a validator
	// change that schedules consensus key pub(5).
	mutateConfig(t, kv, func(cfg *authorityv1.GenesisState) {
		cfg.Authority.Submitters = append(cfg.Authority.Submitters, &typesv1.Submitter{
			Address: SeatAddress(pub(81)), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
		})
	})
	queueAt(t, kv, seats, testHeight, testTime, "vadd", powerMsg("v5", pub(5), 1, h+50))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-vadd", "vadd")); !out.Success() {
		t.Fatal(out)
	}
	// Retire pub(70) with an engine-only rotation of seat-4 and back.
	st := loadAt(t, kv, testHeight, testTime)
	if out := st.RotateCouncilSeat(seats[3].ID, pub(70)); !out.Success() {
		t.Fatal(out)
	}
	if out := loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[3].ID, pub(71)); !out.Success() {
		t.Fatal(out)
	}

	rows := []struct {
		name, seat string
		key        []byte
		want       string
	}{
		{"absent seat", "seat-9", pub(80), CodeNotFound},
		{"absent seat with another seat's key", "seat-9", seats[0].Pub, CodeNotFound},
		{"another seat's key", seats[0].ID, seats[1].Pub, CodeDuplicate},
		{"the seat's own key", seats[0].ID, seats[0].Pub, CodeConflict},
		{"a retired key", seats[0].ID, pub(70), CodeConflict},
		{"a submitter's address", seats[0].ID, pub(81), CodeDuplicate},
		{"a current validator key (D-7)", seats[0].ID, pub(1), CodeDuplicate},
		{"a pending validator key (D-7)", seats[0].ID, pub(5), CodeDuplicate},
	}
	for i, r := range rows {
		id := "seat-r" + strconv.Itoa(i)
		queueAt(t, kv, seats, testHeight, testTime, id, seatMsg(r.seat, r.key))
		executeRefused(t, kv, authority, testHeight, r.name, id, r.want)
	}

	// No account hook: MALFORMED. An account hook refusal: its code.
	queueAt(t, kv, seats, testHeight, testTime, "seat-nohook", seatMsg(seats[0].ID, pub(82)))
	wantRefused(t, kv, "no account hook", CodeMalformed, func() Outcome {
		st := loadAt(t, kv, testHeight, testTime)
		st.Accounts = nil
		return st.ExecuteChange(execute(authority, "ex-seat-nohook", "seat-nohook"))
	})
	accountsFor(t, kv).fail = CodeIntegrity
	executeRefused(t, kv, authority, testHeight, "account hook refusal", "seat-nohook", CodeIntegrity)
	accountsFor(t, kv).fail = ""

	// A proposal approved by seat-1's key and still QUEUED.
	queueAt(t, kv, seats, testHeight, testTime, "inflight", governedMsg(msgkit.LimitMaxPageLimit, 9))
	version := loadAt(t, kv, testHeight, testTime).PolicyVersion()
	st = loadAt(t, kv, testHeight, testTime)
	if out := st.ExecuteChange(execute(authority, "ex-seat-nohook", "seat-nohook")); !out.Success() {
		t.Fatalf("seat replacement: %+v", out)
	}
	cfg, err := readConfig(kv)
	if err != nil {
		t.Fatal(err)
	}
	if got := seatAt(cfg.GetAuthority().GetDevnetTestSeats(), SeatAddress(pub(82))); got == nil || got.GetSeatId() != seats[0].ID {
		t.Fatalf("seat for the new key %v", got)
	}
	if seatAt(cfg.GetAuthority().GetDevnetTestSeats(), seats[0].Signer) != nil || len(cfg.GetAuthority().GetDevnetTestSeats()) != ApprovedSeats {
		t.Fatal("the old key still holds a seat, or the council size changed")
	}
	if got := kv.Get(retiredCouncilKey(seats[0].Pub)); string(got) != seats[0].ID {
		t.Fatalf("retired row %q", got)
	}
	if created := accountsFor(t, kv).created; len(created) != 1 || !bytes.Equal(created[0], SeatAddress(pub(82))) {
		t.Fatalf("accounts %x", created)
	}
	if st.seatOf(SeatAddress(pub(82))) == nil || st.seatOf(seats[0].Signer) != nil {
		t.Fatal("the executing State did not take the new config")
	}
	if got := loadAt(t, kv, testHeight, testTime).PolicyVersion(); got != version+1 {
		t.Fatalf("policy version %d after %d", got, version)
	}
	if AdmissionOf(kv, seats[0].Signer) != NotAdmitted || AdmissionOf(kv, SeatAddress(pub(82))) != AdmittedSeat {
		t.Fatal("ante admission did not move to the new key")
	}

	// The in-flight proposal is on an older base: STALE_VERSION.
	executeRefused(t, kv, authority, testHeight, "in-flight proposal after the replacement", "inflight", CodeStale)
	// The old key is not a seat; the new key proposes and approves.
	base := loadAt(t, kv, testHeight, testTime).PolicyVersion()
	wantRefused(t, kv, "old key proposes", CodeUnauthorized, func() Outcome {
		return loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, "old-key", base, governedMsg(msgkit.LimitMaxPageLimit, 8)))
	})
	if out := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(SeatAddress(pub(82)), "new-key", base, governedMsg(msgkit.LimitMaxPageLimit, 8))); !out.Success() {
		t.Fatalf("new key proposes: %+v", out)
	}
	if out := loadAt(t, kv, testHeight, testTime).ApproveChange(approve(SeatAddress(pub(82)), "ap-new-key", "new-key")); !out.Success() {
		t.Fatalf("new key approves: %+v", out)
	}
	// The retired key can never return.
	queueAt(t, kv, []testSeat{{ID: seats[0].ID, Signer: SeatAddress(pub(82))}, seats[1], seats[2]}, testHeight, testTime, "back", seatMsg(seats[1].ID, seats[0].Pub))
	executeRefused(t, kv, authority, testHeight, "retired key back on another seat", "back", CodeConflict)
}

// TestRotateCouncilSeatProtocolOrder: the engine-only rotation shares the
// payload's checks (replaceSeat), so an absent seat is NOT_FOUND before a key
// that another seat holds is DUPLICATE_ID, as in the protocol's council
// seat change row. Goal 06's rotation checked the other seats' keys first;
// no test pinned that combination.
func TestRotateCouncilSeatProtocolOrder(t *testing.T) {
	kv, seats, _ := readyStore(t)
	wantRefused(t, kv, "absent seat with another seat's key", CodeNotFound, func() Outcome {
		return loadAt(t, kv, testHeight, testTime).RotateCouncilSeat("seat-9", seats[1].Pub)
	})
	wantRefused(t, kv, "present seat with another seat's key", CodeDuplicate, func() Outcome {
		return loadAt(t, kv, testHeight, testTime).RotateCouncilSeat(seats[0].ID, seats[1].Pub)
	})
}

// TestHooklessExecuteIsMalformed: without a policy writer a governed limit
// (immediate or scheduled), a software upgrade and a cancel are MALFORMED;
// without an account hook a council seat change is MALFORMED; each leaves
// the store byte-identical and the proposal QUEUED, as a registry payload
// without a registry does.
func TestHooklessExecuteIsMalformed(t *testing.T) {
	kv, seats, authority := readyStore(t)
	for _, row := range []struct {
		id  string
		msg *authorityv1.MsgProposeChange
	}{
		{"n-limit", governedMsg(msgkit.LimitMaxPageLimit, 9)},
		{"n-sched", scheduledMsg(msgkit.LimitMaxPageLimit, 9, h+5)},
		{"n-upgrade", upgradeMsg("up-1", h+minLead, upgradeDigest)},
		{"n-cancel", cancelMsg("up-1")},
		{"n-seat", seatMsg(seats[0].ID, pub(80))},
	} {
		queueAt(t, kv, seats, testHeight, testTime, row.id, row.msg)
		wantRefused(t, kv, row.id, CodeMalformed, func() Outcome {
			st := loadAt(t, kv, testHeight, testTime)
			st.Policy, st.Accounts = nil, nil
			return st.ExecuteChange(execute(authority, "ex-"+row.id, row.id))
		})
		wantState(t, kv, row.id, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	}
}

// TestHeightWindowSafeIntegerCap: near the top of the safe integer range the
// window's absolute cap decides, not the lead: in a block at height
// 2^53-1-200 a governed activation and a plan height of 2^53-1 are accepted
// and 2^53 is BOUNDS although its lead (201) is inside the window.
func TestHeightWindowSafeIntegerCap(t *testing.T) {
	top := uint64(safeMax)
	height := int64(top - 200)
	for _, r := range []struct {
		name string
		msg  *authorityv1.MsgProposeChange
		want string
	}{
		{"activation 2^53-1", scheduledMsg(msgkit.LimitMaxOpenOffers, 8, top), ""},
		{"activation 2^53", scheduledMsg(msgkit.LimitMaxOpenOffers, 8, top+1), CodeBounds},
		{"plan height 2^53-1", upgradeMsg("up-1", top, upgradeDigest), ""},
		{"plan height 2^53", upgradeMsg("up-1", top+1, upgradeDigest), CodeBounds},
	} {
		kv, seats, _ := readyStore(t)
		before := dumpKV(kv)
		got := loadAt(t, kv, height, testTime).ProposeChange(propose(seats[0].Signer, "top", 1, r.msg))
		if got.Code != r.want {
			t.Fatalf("%s: %+v, want %q", r.name, got, r.want)
		}
		if r.want != "" {
			wantUnchanged(t, kv, before, r.name)
		}
	}
}

// TestFailedSeatExecuteKeepsTheStateConfig: a council seat execute that
// passes every seat check and then fails a later engine check (OVERFLOW at
// policy version 2^64-1) writes nothing, and a later successful execute on
// the same State does not pick up the config the failed one staged.
func TestFailedSeatExecuteKeepsTheStateConfig(t *testing.T) {
	kv, seats, authority := readyStore(t)
	kv.Set([]byte{policyKey}, encodeU64(math.MaxUint64))
	putQueuedPayload(t, kv, seats, "seat", math.MaxUint64, seatMsg(seats[0].ID, pub(80)))
	st := loadAt(t, kv, testHeight, testTime)
	wantRefused(t, kv, "seat change at version 2^64-1", CodeOverflow, func() Outcome {
		return st.ExecuteChange(execute(authority, "ex-seat", "seat"))
	})
	kv.Set([]byte{policyKey}, encodeU64(2))
	putQueuedPayload(t, kv, seats, "limit", 2, governedMsg(msgkit.LimitMaxPageLimit, 9))
	if out := st.ExecuteChange(execute(authority, "ex-limit", "limit")); !out.Success() {
		t.Fatal(out)
	}
	if st.seatOf(seats[0].Signer) == nil || st.seatOf(SeatAddress(pub(80))) != nil {
		t.Fatal("the State took the config of the failed seat execute")
	}
}
