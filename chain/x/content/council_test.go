package content

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
)

// TestForceWithdrawByCouncilOrder follows the spec force-withdraw branch of
// apply_proposal_payload: NOT_FOUND, DECISION_BLOCKED, WITHDRAWN,
// STALE_VERSION, with the reason size as a wiring precondition.
func TestForceWithdrawByCouncilOrder(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		version uint64
		reason  []byte
		devnet  bool
		want    msgkit.Code
	}{
		{"reason-31-bytes", "c1", 1, reasonDigest("r")[:31], true, msgkit.Malformed},
		{"reason-nil", "c1", 1, nil, true, msgkit.Malformed},
		{"not-found", "c-missing", 1, reasonDigest("r"), true, msgkit.NotFound},
		{"not-found-before-decision-blocked", "c-missing", 1, reasonDigest("r"), false, msgkit.NotFound},
		{"not-found-id-pattern", "C1", 1, reasonDigest("r"), true, msgkit.NotFound},
		{"decision-blocked-without-dec-003", "c1", 1, reasonDigest("r"), false, msgkit.DecisionBlocked},
		{"decision-blocked-before-withdrawn", "c2", 1, reasonDigest("r"), false, msgkit.DecisionBlocked},
		{"withdrawn", "c2", 1, reasonDigest("r"), true, msgkit.Withdrawn},
		{"withdrawn-before-stale", "c2", 7, reasonDigest("r"), true, msgkit.Withdrawn},
		{"stale-version", "c1", 2, reasonDigest("r"), true, msgkit.StaleVersion},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.mustRegister("reg-c1", "c1")
			f.mustRegister("reg-c2", "c2")
			f.mustWithdraw("wd-c2", "c2")
			f.hook.calls = nil
			before := f.snapshot()
			ctx, code := f.force(c.id, c.version, c.reason, c.devnet)
			if code != c.want {
				t.Fatalf("code = %s, want %s", code, c.want)
			}
			requireSame(t, before, f.snapshot(), c.name)
			noEvents(t, ctx, c.name)
			if len(f.hook.calls) != 0 {
				t.Fatalf("hook called on a rejected force-withdraw: %v", f.hook.calls)
			}
		})
	}
}

// TestExecuteForceWithdraw mirrors cases.py execute-force-withdraw: the
// council path writes the same tombstone rules (history_erased false,
// DEC-004, version 0) with the payload reason, needs no submitting host,
// emits ContentWithdrawn, and runs the hook once. A later register of the
// id is DUPLICATE_ID and a host withdrawal is WITHDRAWN.
func TestExecuteForceWithdraw(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	registered := f.record("c1")
	reason := reasonDigest("council complaint digest")
	ctx, code := f.force("c1", 1, reason, true)
	if code != msgkit.OK {
		t.Fatalf("force = %s", code)
	}
	want := proto.Clone(registered).(*contentv1.Content)
	want.State = contentv1.ContentState_CONTENT_STATE_WITHDRAWN
	if got := f.record("c1"); !proto.Equal(got, want) {
		t.Fatalf("record = %v, want %v", got, want)
	}
	tomb, ok := f.keeper.tombstone(f.ctx, "c1", 1)
	wantTomb := &contentv1.Tombstone{
		ContentId:            "c1",
		ContentVersion:       1,
		CommittedUnixSeconds: f.ctx.BlockTime().Unix(),
		ReasonDigest:         digestOf(reason),
		RetentionDecisionId:  RetentionDecisionID,
	}
	if !ok || !proto.Equal(tomb, wantTomb) {
		t.Fatalf("tombstone = %v, want %v", tomb, wantTomb)
	}
	events := typedEvents(t, ctx)
	if len(events) != 1 || !proto.Equal(events[0], &eventsv1.ContentWithdrawn{ContentId: "c1", ContentVersion: 1}) {
		t.Fatalf("events = %v", events)
	}
	if len(f.hook.calls) != 1 || f.hook.calls[0] != (hookCall{id: "c1", version: 1}) {
		t.Fatalf("hook calls = %v", f.hook.calls)
	}
	_, err := f.register(f.registerMsg("reg-c1-again", "c1"))
	requireCode(t, err, msgkit.DuplicateID)
	_, err = f.withdraw(f.withdrawMsg("wd-c1", "c1"))
	requireCode(t, err, msgkit.Withdrawn)
}

// TestForceWithdrawHookFailure: a failing hook leaves no write and no
// event. Its svn code passes through, so a hook FORBIDDEN reaches the
// authority keep path; any other error is MALFORMED.
func TestForceWithdrawHookFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want msgkit.Code
	}{
		{"svn-forbidden", svnerr.ErrForbidden.Wrap("synthetic"), msgkit.Forbidden},
		{"svn-bounds", svnerr.ErrBounds.Wrap("synthetic"), msgkit.Bounds},
		{"plain-error", errPlainHook, msgkit.Malformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.mustRegister("reg-c1", "c1")
			f.hook.err = c.err
			f.hook.write = []byte{0x7f, 'h'}
			before := f.snapshot()
			ctx, code := f.force("c1", 1, reasonDigest("r"), true)
			if code != c.want {
				t.Fatalf("code = %s, want %s", code, c.want)
			}
			requireSame(t, before, f.snapshot(), c.name)
			noEvents(t, ctx, c.name)
		})
	}
}

func TestCodeOfMapsEverySvnCode(t *testing.T) {
	for n := uint32(1); n <= svnerr.MaxCode; n++ {
		registered, _ := svnerr.ByCode(n)
		got := codeOf(registered.Wrap("wrapped"))
		if got.Err() == nil {
			t.Fatalf("code %d mapped to success", n)
		}
		requireCode(t, got.Err(), got)
		if want := registered.Error(); string(got) != want {
			t.Fatalf("code %d mapped to %s, want %s", n, got, want)
		}
	}
	if codeOf(errPlainHook) != msgkit.Malformed {
		t.Fatal("plain error not MALFORMED")
	}
}
