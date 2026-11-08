package policy_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy/policytest"
)

// TestApplyDueOrderAndBounds: ApplyDue at height b applies every pending
// change due at or below b, in key order, and leaves the others. Block a-1
// reads the old value (effective-1) and block a the new one (effective);
// each applied change becomes the governed value, its pending row is
// deleted, and one policy_activated event per change is emitted in key
// order. A second call at the same height does nothing. A damaged row, a
// row of an unknown key, a value above the ceiling, or a sixth row stops it
// with an error before any write or event.
func TestApplyDueOrderAndBounds(t *testing.T) {
	e := newEnv(t)
	e.auth.version = 7
	mustOK(t, e.k.SetLimit(e.fresh(), msgkit.LimitMaxPageLimit, 50, "p1"))
	schedule := []struct {
		key        string
		value, act uint64
		id         string
		version    uint64
	}{
		{msgkit.LimitMaxPageLimit, 6, 20, "p2", 2},
		{msgkit.LimitMaxPackageBytes, 5, 40, "p3", 3},
		{msgkit.LimitMaxOpenOffers, 4, 20, "p4", 4},
		{msgkit.LimitMaxExpirySteps, 3, 30, "p5", 5},
		{msgkit.LimitMaxEvidenceBatch, 2, 20, "p6", 6},
	}
	latest := map[string]policytest.Decision{}
	for _, s := range schedule {
		mustOK(t, e.k.SchedulePending(e.fresh(), s.key, s.value, s.act, s.id, s.version))
		latest[s.key] = policytest.Decision{Value: s.value, Activation: s.act, PolicyVersion: s.version, ProposalID: s.id}
	}
	checkAt := func(b uint64) {
		t.Helper()
		ctx := e.at(b)
		if err := policytest.CheckInvariants(ctx, e.k, e.auth); err != nil {
			t.Fatalf("invariants at %d: %v", b, err)
		}
		if err := policytest.CheckDecisions(ctx, e.k, latest); err != nil {
			t.Fatalf("decisions at %d: %v", b, err)
		}
	}
	checkAt(h)

	// Effective-1: nothing is due at 19.
	ctx := e.at(19)
	before := e.snapshot()
	if err := e.k.ApplyDue(ctx); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, e.snapshot()) || len(ctx.EventManager().Events()) != 0 {
		t.Fatal("ApplyDue below every activation height wrote or emitted")
	}
	if v, _ := e.k.GovernedLimit(ctx, msgkit.LimitMaxPageLimit); v != 50 {
		t.Fatalf("max_page_limit at 19 is %d, want the old value 50", v)
	}
	checkAt(19)

	// Effective: the three changes due at 20 apply in key order.
	ctx = e.at(20)
	if err := e.k.ApplyDue(ctx); err != nil {
		t.Fatal(err)
	}
	events := ctx.EventManager().Events().ToABCIEvents()
	wantOrder := []struct{ key, value, id string }{
		{msgkit.LimitMaxEvidenceBatch, "2", "p6"},
		{msgkit.LimitMaxOpenOffers, "4", "p4"},
		{msgkit.LimitMaxPageLimit, "6", "p2"},
	}
	if len(events) != len(wantOrder) {
		t.Fatalf("%d events at 20, want %d", len(events), len(wantOrder))
	}
	for i, ev := range events {
		got := map[string]string{}
		for _, a := range ev.Attributes {
			got[a.Key] = a.Value
		}
		w := wantOrder[i]
		if ev.Type != policy.EventPolicyActivated || len(ev.Attributes) != 4 || got["parameter_key"] != w.key ||
			got["value"] != w.value || got["activation_height"] != "20" || got["proposal_id"] != w.id {
			t.Fatalf("event %d: %s %v, want %v", i, ev.Type, got, w)
		}
	}
	for _, w := range []struct {
		key   string
		value uint64
	}{{msgkit.LimitMaxEvidenceBatch, 2}, {msgkit.LimitMaxOpenOffers, 4}, {msgkit.LimitMaxPageLimit, 6}} {
		if v, found := e.k.GovernedLimit(ctx, w.key); !found || v != w.value {
			t.Fatalf("%s at 20: (%d, %t), want %d", w.key, v, found, w.value)
		}
		if _, pending := pendingOf(t, e, w.key); pending {
			t.Fatalf("%s kept its pending row after it applied", w.key)
		}
	}
	for _, key := range []string{msgkit.LimitMaxExpirySteps, msgkit.LimitMaxPackageBytes} {
		if _, found := e.k.GovernedLimit(ctx, key); found {
			t.Fatalf("%s applied before its height", key)
		}
	}
	checkAt(20)

	// A second call at the same height does nothing.
	ctx = e.at(20)
	before = e.snapshot()
	if err := e.k.ApplyDue(ctx); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, e.snapshot()) || len(ctx.EventManager().Events()) != 0 {
		t.Fatal("a second ApplyDue at 20 wrote or emitted")
	}

	// A height past two activations applies both, in key order.
	ctx = e.at(45)
	if err := e.k.ApplyDue(ctx); err != nil {
		t.Fatal(err)
	}
	events = ctx.EventManager().Events().ToABCIEvents()
	if len(events) != 2 || events[0].Attributes[0].Value != msgkit.LimitMaxExpirySteps ||
		events[1].Attributes[0].Value != msgkit.LimitMaxPackageBytes {
		t.Fatalf("events at 45: %v", events)
	}
	if found, err := anyPending(e); err != nil || found {
		t.Fatalf("pending rows left after every activation: %t %v", found, err)
	}
	checkAt(45)

	t.Run("refusals write nothing", func(t *testing.T) {
		ceiling, _ := msgkit.Ceiling(msgkit.LimitMaxOpenOffers)
		row := func(v uint64) []byte {
			return policy.EncodePending(policy.Pending{Activation: 20, Value: v, PolicyVersion: 2, ProposalID: "px"})
		}
		cases := []struct {
			name, want string
			apply      func(*env)
		}{
			{"undecodable row", `pending change of "max_open_offers"`, func(d *env) {
				d.store().Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), []byte{1})
			}},
			{"value above the ceiling", `pending change of "max_open_offers" holds value`, func(d *env) {
				d.store().Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), row(ceiling+1))
			}},
			{"unknown key", `pending change of "max_aaa"`, func(d *env) {
				d.store().Set(policy.PendingKey("max_aaa"), row(1))
			}},
			{"sixth row", "more than 5 rows under prefix 0x03", func(d *env) {
				d.store().Set(policy.PendingKey("max_zzz"), row(1))
			}},
		}
		for _, c := range cases {
			d := newEnv(t)
			d.auth.version = 9
			for i, key := range msgkit.LimitKeys() {
				mustOK(t, d.k.SchedulePending(d.fresh(), key, 1, 20, fmt.Sprintf("p%d", i), 2))
			}
			c.apply(d)
			ctx := d.at(100)
			before := d.snapshot()
			if err := d.k.ApplyDue(ctx); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%s: %v, want %q", c.name, err, c.want)
			}
			if !bytes.Equal(before, d.snapshot()) || len(ctx.EventManager().Events()) != 0 {
				t.Fatalf("%s: a refused ApplyDue wrote or emitted", c.name)
			}
		}
	})

	t.Run("no pending rows", func(t *testing.T) {
		d := newEnv(t)
		before := d.snapshot()
		for _, b := range []uint64{0, 1, h, 1 << 40} {
			ctx := d.at(b)
			if err := d.k.ApplyDue(ctx); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, d.snapshot()) || len(ctx.EventManager().Events()) != 0 {
				t.Fatalf("ApplyDue on an empty pending set at %d wrote or emitted", b)
			}
		}
	})
}

func anyPending(e *env) (bool, error) {
	it := e.store().Iterator([]byte{policy.PendingPrefix}, []byte{policy.PendingPrefix + 1})
	found := it.Valid()
	return found, it.Close()
}

// model is the expected council state of runSequence.
type model struct {
	height     uint64
	version    uint64
	appVersion uint64
	latest     map[string]policytest.Decision
	plan       *policy.Plan
	names      map[string][]byte
	applied    map[string]bool
	outcomes   map[string]int
}

// sequenceSeeds reach every outcome TestGovernedChangeSequenceReachesEveryOutcome
// lists; FuzzGovernedChangeSequence starts from them.
var sequenceSeeds = [][]byte{
	// schedule max_expiry_steps=5 at +3; immediate max_evidence_batch=9;
	// a block; another execute; a scheduled change at +0 (OUT_OF_RANGE).
	{2, 1, 5, 3, 1, 0, 9, 0, 0, 0, 0, 3, 3, 0, 0, 0, 2, 2, 7, 0},
	// value 0 and value above the ceiling (BOUNDS) for both writers; a
	// scheduled change that applies at its exact height.
	{1, 0, 0, 0, 1, 0, 200, 0, 2, 0, 0, 2, 2, 0, 33, 2, 2, 4, 9, 1, 0, 0, 0, 0},
	// plan up-a, cancel of another name (NOT_FOUND), a plan below the
	// minimum lead (OUT_OF_RANGE), up-a with another digest (CONFLICT), a
	// long jump to the plan height (record), up-a again (DUPLICATE_ID),
	// plan up-b, cancel it, up-b with another digest after the cancel
	// (CONFLICT), up-b again with its digest, a jump past the height of a
	// pending change (late activation).
	{
		4, 0, 0, 1, 5, 1, 0, 0, 4, 1, 1, 0, 4, 0, 1, 1, 2, 3, 4, 7, 0, 0, 0, 0xff,
		4, 0, 0, 1, 4, 1, 0, 1, 5, 1, 0, 0, 4, 1, 1, 1, 4, 1, 0, 2, 2, 3, 5, 7, 0, 0, 0, 0x8a,
	},
}

// runSequence drives the keeper with a bounded sequence of council writes
// (immediate and scheduled governed changes, plans and cancels, executes of
// other kinds), BeginBlocks at increasing heights, and the upgrade record
// at a plan's height, against a model. After every step it checks the code
// each writer returned, that a refused write left the store and the events
// unchanged, every keeper-level invariant (policytest.CheckInvariants), and
// the exact form of I3, the latest executed decision for a key wins
// (policytest.CheckDecisions). It returns how often each outcome happened.
func runSequence(t *testing.T, ops []byte) map[string]int {
	t.Helper()
	if len(ops) > 256 {
		ops = ops[:256]
	}
	e := newEnv(t)
	m := &model{height: h, version: 1, appVersion: 1, latest: map[string]policytest.Decision{},
		names: map[string][]byte{}, applied: map[string]bool{}, outcomes: map[string]int{}}
	keys := msgkit.LimitKeys()
	upgradeNames := []string{"up-a", "up-b", "up-c"}
	digests := [][]byte{digestOf("d0"), digestOf("d1")}
	for i := 0; i+3 < len(ops); i += 4 {
		op, a, b, c := ops[i]%6, ops[i+1], ops[i+2], ops[i+3]
		id := fmt.Sprintf("p%d", i)
		switch op {
		case 0:
			nextBlock(t, e, m, c)
		case 1:
			key := keys[int(a)%len(keys)]
			value := uint64(b)
			want := msgkit.OK
			if ceiling, _ := msgkit.Ceiling(key); value < 1 || value > ceiling {
				want = msgkit.Bounds
			}
			if write(t, e, m, "SetLimit", want, func(ctx sdk.Context) msgkit.Code { return e.k.SetLimit(ctx, key, value, id) }) {
				m.version++
				m.latest[key] = policytest.Decision{Value: value, PolicyVersion: m.version, ProposalID: id}
			}
		case 2:
			key := keys[int(a)%len(keys)]
			value := uint64(b)
			activation := m.height + uint64(c%8)
			want := msgkit.OK
			if ceiling, _ := msgkit.Ceiling(key); value < 1 || value > ceiling {
				want = msgkit.Bounds
			} else if activation <= m.height {
				want = msgkit.OutOfRange
			}
			pv := m.version + 1
			if write(t, e, m, "SchedulePending", want, func(ctx sdk.Context) msgkit.Code {
				return e.k.SchedulePending(ctx, key, value, activation, id, pv)
			}) {
				m.version = pv
				m.latest[key] = policytest.Decision{Value: value, Activation: activation, PolicyVersion: pv, ProposalID: id}
			}
		case 3:
			m.version++ // an execute of another payload kind
		case 4:
			name := upgradeNames[int(a)%len(upgradeNames)]
			digest := digests[int(b)%len(digests)]
			height := m.height + msgkit.MinActivationLeadBlocks + uint64(c%3)
			if c%5 == 0 {
				height = m.height + msgkit.MinActivationLeadBlocks - 1
			}
			want := msgkit.OK
			switch first, used := m.names[name]; {
			case height-m.height < msgkit.MinActivationLeadBlocks:
				want = msgkit.OutOfRange
			case m.applied[name]:
				want = msgkit.DuplicateID
			case used && !bytes.Equal(first, digest):
				want = msgkit.Conflict
			}
			if write(t, e, m, "ScheduleUpgrade", want, func(ctx sdk.Context) msgkit.Code {
				return e.k.ScheduleUpgrade(ctx, name, height, digest, id)
			}) {
				m.version++
				m.plan = &policy.Plan{Name: name, Height: height, Digest: digest, ProposalID: id}
				if _, used := m.names[name]; !used {
					m.names[name] = digest
				}
			}
		case 5:
			name := upgradeNames[int(a)%len(upgradeNames)]
			want := msgkit.NotFound
			if m.plan != nil && m.plan.Name == name {
				want = msgkit.OK
			}
			if write(t, e, m, "CancelUpgrade", want, func(ctx sdk.Context) msgkit.Code { return e.k.CancelUpgrade(ctx, name) }) {
				m.version++
				m.plan = nil
			}
		}
		e.auth.version = m.version
		checkModel(t, e, m)
	}
	return m.outcomes
}

// FuzzGovernedChangeSequence runs runSequence on arbitrary operation bytes.
func FuzzGovernedChangeSequence(f *testing.F) {
	for _, seed := range sequenceSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, ops []byte) { runSequence(t, ops) })
}

// TestGovernedChangeSequenceReachesEveryOutcome: the seed sequences reach
// every writer outcome the model predicts, an activation at its exact
// height and a late one, and an upgrade record, so the fuzz target starts
// from states where each check matters.
func TestGovernedChangeSequenceReachesEveryOutcome(t *testing.T) {
	total := map[string]int{}
	for _, seed := range sequenceSeeds {
		for outcome, n := range runSequence(t, seed) {
			total[outcome] += n
		}
	}
	outcome := func(name string, code msgkit.Code) string { return name + " " + string(code) }
	for _, want := range []string{
		outcome("SetLimit", msgkit.OK), outcome("SetLimit", msgkit.Bounds),
		outcome("SchedulePending", msgkit.OK), outcome("SchedulePending", msgkit.Bounds), outcome("SchedulePending", msgkit.OutOfRange),
		outcome("ScheduleUpgrade", msgkit.OK), outcome("ScheduleUpgrade", msgkit.OutOfRange),
		outcome("ScheduleUpgrade", msgkit.Conflict), outcome("ScheduleUpgrade", msgkit.DuplicateID),
		outcome("CancelUpgrade", msgkit.OK), outcome("CancelUpgrade", msgkit.NotFound),
		"activated exact", "activated late", "record",
	} {
		if total[want] == 0 {
			t.Errorf("no seed reaches %q (reached %v)", want, total)
		}
	}
}

// write runs one council writer in a block at the model height, asserts its
// code, and, for a refusal, an unchanged store and no event. It counts the
// outcome under "<name> <code>" and reports whether the write succeeded.
func write(t *testing.T, e *env, m *model, name string, want msgkit.Code, op func(sdk.Context) msgkit.Code) bool {
	t.Helper()
	ctx := e.at(m.height)
	before := e.snapshot()
	if got := op(ctx); got != want {
		t.Fatalf("%s: code %q, want %q", name, got, want)
	}
	if want != msgkit.OK && (!bytes.Equal(before, e.snapshot()) || len(ctx.EventManager().Events()) != 0) {
		t.Fatalf("%s: a refused write (%q) wrote or emitted", name, want)
	}
	m.outcomes[name+" "+string(want)]++
	return want == msgkit.OK
}

// nextBlock moves the model to a later block: one to four heights, or up to
// 128 when c has its high bit set, but never past a stored plan's height.
// At the plan height it records the upgrade first, as the upgrade runner
// does in PreBlock; then it runs BeginBlock.
func nextBlock(t *testing.T, e *env, m *model, c byte) {
	t.Helper()
	step := uint64(c%4) + 1
	if c&0x80 != 0 {
		step = uint64(c & 0x7f)
		if step == 0 {
			step = 1
		}
	}
	m.height += step
	if m.plan != nil && m.plan.Height <= m.height {
		m.height = m.plan.Height
		if err := e.k.RecordUpgrade(e.at(m.height), m.plan.Name, m.height, testVersionMap(), m.appVersion+1); err != nil {
			t.Fatalf("record %q at %d: %v", m.plan.Name, m.height, err)
		}
		m.appVersion++
		m.applied[m.plan.Name] = true
		m.plan = nil
		m.version++ // the upgrade bumps the policy version (DR-10-38)
		m.outcomes["record"]++
	}
	ctx := e.at(m.height)
	if err := e.k.ApplyDue(ctx); err != nil {
		t.Fatalf("ApplyDue at %d: %v", m.height, err)
	}
	for _, ev := range ctx.EventManager().Events().ToABCIEvents() {
		for _, attr := range ev.Attributes {
			if attr.Key == policy.AttrActivationHeight {
				if attr.Value == msgkit.Dec(m.height) {
					m.outcomes["activated exact"]++
				} else {
					m.outcomes["activated late"]++
				}
			}
		}
	}
}

func checkModel(t *testing.T, e *env, m *model) {
	t.Helper()
	ctx := e.at(m.height)
	if err := policytest.CheckInvariants(ctx, e.k, e.auth); err != nil {
		t.Fatalf("invariants at %d: %v", m.height, err)
	}
	if err := policytest.CheckDecisions(ctx, e.k, m.latest); err != nil {
		t.Fatalf("decisions at %d: %v", m.height, err)
	}
	plan, found := e.k.Plan(ctx)
	if found != (m.plan != nil) || (found && !equalPlan(plan, *m.plan)) {
		t.Fatalf("plan %+v (%t), model %+v", plan, found, m.plan)
	}
	if e.k.AppVersion(ctx) != m.appVersion {
		t.Fatalf("app version %d, model %d", e.k.AppVersion(ctx), m.appVersion)
	}
	for name, digest := range m.names {
		if !bytes.Equal(e.store().Get(policy.PlanNameKey(name)), digest) {
			t.Fatalf("plan-name row of %q differs from the model", name)
		}
		if e.k.UpgradeApplied(ctx, name) != m.applied[name] {
			t.Fatalf("applied %q differs from the model", name)
		}
	}
}
