package content

import (
	"sort"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// contentRejectClasses is every protocol code a content message or the
// council force-withdraw can return. CONFLICT and INTEGRITY come only from
// the read-only lease handoff (CheckOfferable).
var contentRejectClasses = []msgkit.Code{
	msgkit.Unauthorized, msgkit.Forbidden, msgkit.Replay, msgkit.DuplicateID, msgkit.StaleVersion,
	msgkit.Malformed, msgkit.OutOfRange, msgkit.WrongDomain, msgkit.Withdrawn, msgkit.NotFound,
	msgkit.DecisionBlocked, msgkit.Bounds,
}

// TestRejectedContentMessagesLeaveTheStore: for every reject class, a
// rejected message leaves every mounted store (content, authority,
// providers, leases, evidence) byte-identical and emits no event. The
// keeper itself writes nothing before it fails; the SDK message branch
// discard is a second layer, not the reason this holds.
func TestRejectedContentMessagesLeaveTheStore(t *testing.T) {
	type run func(f *fixture, ctx sdk.Context) msgkit.Code
	viaRegister := func(edit func(f *fixture, m *contentv1.MsgRegisterContent)) run {
		return func(f *fixture, ctx sdk.Context) msgkit.Code {
			m := f.registerMsg("reg-new", "c-new")
			edit(f, m)
			_, err := f.keeper.RegisterContent(ctx, m)
			return codeFromErr(f.t, err)
		}
	}
	viaWithdraw := func(edit func(f *fixture, m *contentv1.MsgWithdrawContent)) run {
		return func(f *fixture, ctx sdk.Context) msgkit.Code {
			m := f.withdrawMsg("wd-new", "c1")
			edit(f, m)
			_, err := f.keeper.WithdrawContent(ctx, m)
			return codeFromErr(f.t, err)
		}
	}
	viaForce := func(id string, version uint64, devnet bool) run {
		return func(f *fixture, ctx sdk.Context) msgkit.Code {
			return f.keeper.ForceWithdrawByCouncil(ctx, id, version, reasonDigest("r"), devnet)
		}
	}
	cases := []struct {
		name string
		want msgkit.Code
		run  run
	}{
		{"register-unauthorized", msgkit.Unauthorized, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) { m.Signer = outsiderAddr })},
		{"register-forbidden", msgkit.Forbidden, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) { m.Signer = providerAddr })},
		{"register-forbidden-delegate", msgkit.Forbidden, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) {
			f.signRegister(m, DelegateRoleName, f.intent)
		})},
		{"register-replay", msgkit.Replay, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) { m.BusinessId = "reg-c1" })},
		{"register-duplicate", msgkit.DuplicateID, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.ContentId = "c1"
			f.signRegister(m, DevnetRoleName, f.intent)
		})},
		{"register-malformed", msgkit.Malformed, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) { m.ManifestDigest.Algorithm = 0 })},
		{"register-out-of-range", msgkit.OutOfRange, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentVersion = 2 })},
		{"register-bounds-digest", msgkit.Bounds, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) { m.ManifestDigest.Value = make([]byte, 31) })},
		{"register-bounds-package", msgkit.Bounds, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) { m.PackageBytes = 1 << 40 })},
		{"register-wrong-domain", msgkit.WrongDomain, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_CAPABILITY
		})},
		{"register-decision-blocked", msgkit.DecisionBlocked, viaRegister(func(f *fixture, m *contentv1.MsgRegisterContent) {
			delete(f.params.roles, HostingDecisionID)
		})},
		{"withdraw-unauthorized", msgkit.Unauthorized, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) { m.HostingIntent.Signature[9] ^= 1 })},
		{"withdraw-forbidden-cross-owner", msgkit.Forbidden, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.Signer = service2Addr
			f.signWithdraw(m, DevnetRoleName, f.intent)
		})},
		{"withdraw-replay", msgkit.Replay, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) { m.BusinessId = "wd-c2" })},
		{"withdraw-stale", msgkit.StaleVersion, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.ContentVersion = 3
			f.signWithdraw(m, DevnetRoleName, f.intent)
		})},
		{"withdraw-withdrawn", msgkit.Withdrawn, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.ContentId = "c2"
			f.signWithdraw(m, DevnetRoleName, f.intent)
		})},
		{"withdraw-not-found", msgkit.NotFound, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.ContentId = "c404"
			f.signWithdraw(m, DevnetRoleName, f.intent)
		})},
		{"withdraw-malformed", msgkit.Malformed, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) { m.ReasonDigest = nil })},
		{"withdraw-out-of-range", msgkit.OutOfRange, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.HostingIntent.CanonicalProfileVersion = 9
		})},
		{"withdraw-wrong-domain", msgkit.WrongDomain, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) {
			m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PUBLIC_RECORD
		})},
		{"withdraw-decision-blocked", msgkit.DecisionBlocked, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) {
			f.params.roles[HostingDecisionID] = "SOMETHING_ELSE"
		})},
		{"withdraw-bounds", msgkit.Bounds, viaWithdraw(func(f *fixture, m *contentv1.MsgWithdrawContent) { m.BusinessId = "WD" })},
		{"force-not-found", msgkit.NotFound, viaForce("c404", 1, true)},
		{"force-decision-blocked", msgkit.DecisionBlocked, viaForce("c1", 1, false)},
		{"force-withdrawn", msgkit.Withdrawn, viaForce("c2", 1, true)},
		{"force-stale", msgkit.StaleVersion, viaForce("c1", 2, true)},
	}
	covered := map[msgkit.Code]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixtureWith(t, fixtureOptions{extra: []string{"authority", "evidence", "leases", "providers"}})
			f.mustRegister("reg-c1", "c1")
			f.mustRegister("reg-c2", "c2")
			f.mustWithdraw("wd-c2", "c2")
			f.hook.calls = nil
			before := f.allSnapshot()
			ctx := f.tx()
			if got := c.run(f, ctx); got != c.want {
				t.Fatalf("code %s, want %s", got, c.want)
			}
			requireSame(t, before, f.allSnapshot(), c.name)
			noEvents(t, ctx, c.name)
			if len(f.hook.calls) != 0 {
				t.Fatalf("hook ran: %v", f.hook.calls)
			}
		})
		covered[c.want] = true
	}
	var missing []string
	for _, class := range contentRejectClasses {
		if !covered[class] {
			missing = append(missing, string(class))
		}
	}
	sort.Strings(missing)
	if len(missing) != 0 {
		t.Fatalf("reject classes without a snapshot case: %v", missing)
	}
}
