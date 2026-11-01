package content

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestWithdrawContentChecksInSpecOrder runs one fault per check of
// op_withdraw_content and withdraw_content on a store holding c1 (PENDING,
// host serviceAddr) and c2 (WITHDRAWN), and asserts the code, an unchanged
// store, no events, and no hook call.
func TestWithdrawContentChecksInSpecOrder(t *testing.T) {
	resign := func(f *fixture, m *contentv1.MsgWithdrawContent) { f.signWithdraw(m, DevnetRoleName, f.intent) }
	cases := []struct {
		name  string
		setup func(f *fixture)
		edit  func(f *fixture, m *contentv1.MsgWithdrawContent)
		want  msgkit.Code
	}{
		{name: "signer-21-bytes", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.Signer = append(m.Signer, 0) }, want: msgkit.Malformed},
		{name: "missing-business-id", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.BusinessId = "" }, want: msgkit.Malformed},
		{name: "missing-content-id", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentId = "" }, want: msgkit.Malformed},
		{name: "missing-reason-digest", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ReasonDigest = nil }, want: msgkit.Malformed},
		{name: "missing-hosting-intent", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.HostingIntent = nil }, want: msgkit.Malformed},
		{name: "outsider-signer", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.Signer = outsiderAddr; resign(f, m) }, want: msgkit.Unauthorized},
		{name: "provider-signer", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.Signer = providerAddr; resign(f, m) }, want: msgkit.Forbidden},
		{name: "reason-algorithm-unspecified", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ReasonDigest.Algorithm = 0 }, want: msgkit.Malformed},
		{name: "reason-31-bytes", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ReasonDigest.Value = m.ReasonDigest.Value[:31] }, want: msgkit.Bounds},
		{name: "intent-profile-2", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.HostingIntent.CanonicalProfileVersion = 2 }, want: msgkit.OutOfRange},
		{name: "intent-signature-65", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.HostingIntent.Signature = append(m.HostingIntent.Signature, 0)
		}, want: msgkit.Bounds},
		{name: "host-custody-not-assumed", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { f.signWithdraw(m, DelegateRoleName, f.intent) }, want: msgkit.Forbidden},
		{name: "intent-wrong-domain", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END
		}, want: msgkit.WrongDomain},
		{name: "production-chain-no-dec-001", setup: func(f *fixture) { delete(f.params.roles, HostingDecisionID) }, want: msgkit.DecisionBlocked},
		{name: "intent-register-payload", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) {
			reg := f.registerMsg("x", m.GetContentId())
			m.HostingIntent = reg.GetHostingIntent()
		}, want: msgkit.Unauthorized},
		{name: "intent-other-reason", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ReasonDigest.Value = reasonDigest("other") }, want: msgkit.Unauthorized},
		{name: "business-id-pattern", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.BusinessId = "Bad" }, want: msgkit.Bounds},
		{name: "replay-other-payload", setup: func(f *fixture) { f.mustRegister("wd-new", "c3") }, want: msgkit.Replay},
		{name: "not-found", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentId = "c-missing"; resign(f, m) }, want: msgkit.NotFound},
		{name: "not-found-id-pattern", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentId = "C1"; resign(f, m) }, want: msgkit.NotFound},
		{name: "not-found-terminator-in-id", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentId = "c1\x00"; resign(f, m) }, want: msgkit.NotFound},
		{name: "cross-owner", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.Signer = service2Addr; resign(f, m) }, want: msgkit.Forbidden},
		{name: "second-withdraw", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentId = "c2"; resign(f, m) }, want: msgkit.Withdrawn},
		{name: "stale-version-2", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentVersion = 2; resign(f, m) }, want: msgkit.StaleVersion},
		{name: "stale-version-0", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentVersion = 0; resign(f, m) }, want: msgkit.StaleVersion},
		{name: "cross-owner-before-withdrawn", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.ContentId = "c2"
			m.Signer = service2Addr
			resign(f, m)
		}, want: msgkit.Forbidden},
		{name: "withdrawn-before-stale-version", edit: func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.ContentId = "c2"
			m.ContentVersion = 9
			resign(f, m)
		}, want: msgkit.Withdrawn},
		{name: "replay-before-not-found", setup: func(f *fixture) { f.mustRegister("wd-new", "c3") },
			edit: func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ContentId = "c-missing"; resign(f, m) }, want: msgkit.Replay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.mustRegister("reg-c1", "c1")
			f.mustRegister("reg-c2", "c2")
			f.mustWithdraw("wd-c2", "c2")
			f.hook.calls = nil
			if c.setup != nil {
				c.setup(f)
			}
			msg := f.withdrawMsg("wd-new", "c1")
			if c.edit != nil {
				c.edit(f, msg)
			}
			before := f.snapshot()
			ctx, err := f.withdraw(msg)
			requireCode(t, err, c.want)
			requireSame(t, before, f.snapshot(), c.name)
			noEvents(t, ctx, c.name)
			if len(f.hook.calls) != 0 {
				t.Fatalf("hook called on a rejected withdrawal: %v", f.hook.calls)
			}
		})
	}
}

// TestWithdrawContentWritesTombstone checks the WITHDRAWN record, the
// tombstone fields, the event, and the single hook call.
func TestWithdrawContentWritesTombstone(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	registered := f.record("c1")
	ctx, err := f.withdraw(f.withdrawMsg("wd-c1", "c1"))
	if err != nil {
		t.Fatal(err)
	}
	got := f.record("c1")
	want := proto.Clone(registered).(*contentv1.Content)
	want.State = contentv1.ContentState_CONTENT_STATE_WITHDRAWN
	if !proto.Equal(got, want) {
		t.Fatalf("record = %v, want %v (only the state may change)", got, want)
	}
	tomb, ok := f.keeper.tombstone(f.ctx, "c1", 1)
	if !ok {
		t.Fatal("no tombstone")
	}
	wantTomb := &contentv1.Tombstone{
		ContentId:             "c1",
		ContentVersion:        1,
		CommittedUnixSeconds:  f.ctx.BlockTime().Unix(),
		ReasonDigest:          digestOf(reasonDigest("c1")),
		HistoryErased:         false,
		RetentionDecisionId:   "DEC-004",
		RetentionValueVersion: 0,
	}
	if !proto.Equal(tomb, wantTomb) {
		t.Fatalf("tombstone = %v, want %v", tomb, wantTomb)
	}
	events := typedEvents(t, ctx)
	wantEvent := &eventsv1.ContentWithdrawn{ContentId: "c1", ContentVersion: 1, HistoryErased: false}
	if len(events) != 1 || !proto.Equal(events[0], wantEvent) {
		t.Fatalf("events = %v, want [%v]", events, wantEvent)
	}
	if len(f.hook.calls) != 1 || f.hook.calls[0] != (hookCall{id: "c1", version: 1}) {
		t.Fatalf("hook calls = %v, want one (c1, 1)", f.hook.calls)
	}
}

// TestWithdrawnBlocksRevival mirrors cases.py withdrawn-blocks-revival:
// after a withdrawal, registering the same id under a new business id is
// DUPLICATE_ID and the tombstone keeps history_erased false.
func TestWithdrawnBlocksRevival(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("content-c1", "c1")
	f.mustWithdraw("withdraw-c1b", "c1")
	before := f.snapshot()
	_, err := f.register(f.registerMsg("content-c1-again", "c1"))
	requireCode(t, err, msgkit.DuplicateID)
	requireSame(t, before, f.snapshot(), "revival")
	tomb, _ := f.keeper.tombstone(f.ctx, "c1", 1)
	if tomb.GetHistoryErased() {
		t.Fatal("tombstone erased history")
	}
	if got := f.record("c1").GetState(); got != contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		t.Fatalf("state %s after a revival attempt", got)
	}
}

// TestSecondWithdraw mirrors cases.py second-withdraw.
func TestSecondWithdraw(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("content-c1", "c1")
	f.mustWithdraw("withdraw-once", "c1")
	before := f.snapshot()
	_, err := f.withdraw(f.withdrawMsg("withdraw-twice", "c1"))
	requireCode(t, err, msgkit.Withdrawn)
	requireSame(t, before, f.snapshot(), "second withdraw")
	if len(f.hook.calls) != 1 {
		t.Fatalf("hook calls %v, want exactly one", f.hook.calls)
	}
}

// TestWithdrawContentIdempotent: the same withdrawal under the same
// business id is idempotent success, not WITHDRAWN: the business gate runs
// before the state checks.
func TestWithdrawContentIdempotent(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	msg := f.withdrawMsg("wd-c1", "c1")
	if _, err := f.withdraw(msg); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot()
	ctx, err := f.withdraw(proto.Clone(msg).(*contentv1.MsgWithdrawContent))
	requireCode(t, err, msgkit.OK)
	requireSame(t, before, f.snapshot(), "idempotent withdraw")
	if !idempotentEvent(ctx) || len(typedEvents(t, ctx)) != 0 {
		t.Fatal("idempotent withdraw must emit only content_result idempotent=true")
	}
	if len(f.hook.calls) != 1 {
		t.Fatalf("hook ran on an idempotent replay: %v", f.hook.calls)
	}
}

// TestWithdrawHookFailureWritesNothing: a failing hook aborts the whole
// withdrawal, including the hook's own write, the business id, the
// tombstone, and the event.
func TestWithdrawHookFailureWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	f.hook.err = errPlainHook
	f.hook.write = []byte{0x7f, 'x'}
	before := f.snapshot()
	ctx, err := f.withdraw(f.withdrawMsg("wd-c1", "c1"))
	if err == nil {
		t.Fatal("withdrawal succeeded with a failing hook")
	}
	requireSame(t, before, f.snapshot(), "failed hook")
	noEvents(t, ctx, "failed hook")
	if _, ok := f.keeper.BusinessDigest(f.ctx, "wd-c1"); ok {
		t.Fatal("business id committed by a failed withdrawal")
	}
}

// TestWithdrawBlockTimeOutsideRange: a block time outside the protocol
// range is OUT_OF_RANGE after every protocol check, so exported tombstones
// always validate.
func TestWithdrawBlockTimeOutsideRange(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	before := f.snapshot()
	ctx := f.tx().WithBlockTime(f.ctx.BlockTime().AddDate(-100, 0, 0))
	_, err := f.keeper.WithdrawContent(ctx, f.withdrawMsg("wd-c1", "c1"))
	requireCode(t, err, msgkit.OutOfRange)
	requireSame(t, before, f.snapshot(), "block time")
	if code := f.keeper.ForceWithdrawByCouncil(ctx, "c1", 1, reasonDigest("r"), true); code != msgkit.OutOfRange {
		t.Fatalf("force with block time before 1970 = %s", code)
	}
	late := f.tx().WithBlockTime(f.ctx.BlockTime().AddDate(8000, 0, 0))
	_, err = f.keeper.WithdrawContent(late, f.withdrawMsg("wd-c1", "c1"))
	requireCode(t, err, msgkit.OutOfRange)
	requireSame(t, before, f.snapshot(), "force block time")
}
