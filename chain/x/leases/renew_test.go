package leases_test

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

type renewFault = fault[*leasesv1.MsgRenewLease]

func renewEdit(name string, want msgkit.Code, edit func(m *leasesv1.MsgRenewLease)) renewFault {
	return renewFault{name: name, want: want, apply: func(_ *env, c *call[*leasesv1.MsgRenewLease]) { edit(c.msg) }}
}

func renewEnv(name string, want msgkit.Code, edit func(e *env, c *call[*leasesv1.MsgRenewLease])) renewFault {
	return renewFault{name: name, want: want, apply: edit}
}

// heldDeadline is the deadline of the default held lease l1.
const heldDeadline = t0 + 100 + term

// endLeaseRecord rewrites a lease as ENDED without a reservation (record
// only; fixture).
func (e *env) endLeaseRecord(id string) {
	l := e.storedLease(id)
	l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_ENDED, false
	e.k.SeedLeaseForTest(e.unmetered(), l)
}

func renewLadderSetup(e *env) {
	e.seedLease(e.leaseRecord("l1", "c1", "p1"))
	e.evidence.records["ev-expired"] = evidenceRecord{expiry: t0 + 100_000, expired: true}
	e.evidence.records["ev-past"] = evidenceRecord{expiry: t0 - 1}
	e.evidence.records["ev-live"] = evidenceRecord{expiry: t0 + 100_000}
}

func signRenewCall(e *env, c *call[*leasesv1.MsgRenewLease]) {
	e.signRenew(c.msg, "p1", "key-p1")
	c.editSignature(c.msg.ProviderSignature)
}

// TestRenewCheckOrder is the RenewLease ladder: one row per check of
// op_renew (spec_machine.py:765), then the chain-only check D-2.
func TestRenewCheckOrder(t *testing.T) {
	faults := []renewFault{
		gasFault[*leasesv1.MsgRenewLease](),
		txBytesFault[*leasesv1.MsgRenewLease](),
		signerLenFault(func(m *leasesv1.MsgRenewLease, s []byte) { m.Signer = s }),
		renewEdit("presence-business-id", msgkit.Malformed, func(m *leasesv1.MsgRenewLease) { m.BusinessId = "" }),
		renewEdit("lease-not-found", msgkit.NotFound, func(m *leasesv1.MsgRenewLease) { m.LeaseId = "l-none" }),
	}
	faults = append(faults, shapeFaults[*leasesv1.MsgRenewLease](typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT)...)
	faults = append(faults,
		sigFault[*leasesv1.MsgRenewLease]("signature-ed25519-mismatch", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.Signature[0] ^= 1 }),
		renewEdit("role-none", msgkit.Unauthorized, func(m *leasesv1.MsgRenewLease) { m.Signer = outsiderAcct }),
		renewEdit("role-service", msgkit.Forbidden, func(m *leasesv1.MsgRenewLease) { m.Signer = serviceAcct }),
		renewEdit("provider-signer-not-the-provider", msgkit.Forbidden, func(m *leasesv1.MsgRenewLease) { m.Signer = provider2Acct }),
		renewEdit("manifest-algorithm-0", msgkit.Malformed, func(m *leasesv1.MsgRenewLease) { m.ManifestDigest.Algorithm = 0 }),
		renewEdit("manifest-31-bytes", msgkit.Bounds, func(m *leasesv1.MsgRenewLease) { m.ManifestDigest.Value = m.ManifestDigest.Value[:31] }),
		renewEdit("gate-business-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgRenewLease) { m.BusinessId = "_ren" }),
		renewEdit("gate-term-version-2^60", msgkit.Malformed, func(m *leasesv1.MsgRenewLease) { m.TermVersion = 1 << 60 }),
		renewEdit("gate-deadline-2^60", msgkit.Malformed, func(m *leasesv1.MsgRenewLease) { m.DeadlineUnixSeconds = 1 << 60 }),
		renewEdit("gate-package-bytes-max-uint64", msgkit.Malformed, func(m *leasesv1.MsgRenewLease) { m.PackageBytes = math.MaxUint64 }),
		replayFault(func(m *leasesv1.MsgRenewLease) string { return m.GetBusinessId() }),
		renewEnv("lease-not-held", msgkit.Conflict, func(e *env, _ *call[*leasesv1.MsgRenewLease]) { e.endLeaseRecord("l1") }),
		renewEnv("content-withdrawn", msgkit.Withdrawn, func(e *env, _ *call[*leasesv1.MsgRenewLease]) {
			e.setContentState("c1", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
		}),
		renewEdit("term-version-stale", msgkit.StaleVersion, func(m *leasesv1.MsgRenewLease) { m.TermVersion = 3 }),
		renewEdit("package-bytes-change", msgkit.Conflict, func(m *leasesv1.MsgRenewLease) { m.PackageBytes = packageSize + 1 }),
		renewEdit("manifest-change", msgkit.Conflict, func(m *leasesv1.MsgRenewLease) { m.ManifestDigest = manifestOf("c2") }),
		renewEdit("deadline-above-9999", msgkit.Bounds, func(m *leasesv1.MsgRenewLease) { m.DeadlineUnixSeconds = 253402300800 }),
		renewEnv("dec005-absent", msgkit.DecisionBlocked, func(e *env, _ *call[*leasesv1.MsgRenewLease]) { e.auth.termFound = false }),
		renewEdit("term-plus-one-second", msgkit.OutOfRange, func(m *leasesv1.MsgRenewLease) { m.DeadlineUnixSeconds = heldDeadline + term + 1 }),
		renewEdit("evidence-not-found", msgkit.NotFound, func(m *leasesv1.MsgRenewLease) { m.EvidenceId = "ev-none" }),
		renewEdit("evidence-expired", msgkit.Expired, func(m *leasesv1.MsgRenewLease) { m.EvidenceId = "ev-expired" }),
		renewEdit("evidence-expiry-passed", msgkit.Expired, func(m *leasesv1.MsgRenewLease) { m.EvidenceId = "ev-past" }),
		renewEdit("evidence-cannot-extend", msgkit.Forbidden, func(m *leasesv1.MsgRenewLease) { m.EvidenceId = "ev-live" }),
		renewEnv("d2-deadline-passed", msgkit.Expired, func(_ *env, c *call[*leasesv1.MsgRenewLease]) { c.ctx = withTime(c.ctx, heldDeadline) }),
	)
	ladder[*leasesv1.MsgRenewLease]{
		setup:  renewLadderSetup,
		base:   func(e *env) *leasesv1.MsgRenewLease { return e.renewMsg("l1", "ren-1") },
		sign:   signRenewCall,
		faults: faults,
		exec:   func(e *env, ctx sdk.Context, m *leasesv1.MsgRenewLease) result { return e.renew(ctx, m) },
	}.run(t)
}

// TestRenewPresence: an empty business_id or lease_id, or a nil
// manifest_digest or provider_signature, is MALFORMED before the lease
// lookup. An empty evidence_id is absent, not malformed.
func TestRenewPresence(t *testing.T) {
	cases := map[string]func(m *leasesv1.MsgRenewLease){
		"business-id":        func(m *leasesv1.MsgRenewLease) { m.BusinessId = "" },
		"lease-id":           func(m *leasesv1.MsgRenewLease) { m.LeaseId = "" },
		"manifest-digest":    func(m *leasesv1.MsgRenewLease) { m.ManifestDigest = nil },
		"provider-signature": func(m *leasesv1.MsgRenewLease) { m.ProviderSignature = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.seedLease(e.leaseRecord("l1", "c1", "p1"))
			msg := e.renewMsg("l1", "ren-1")
			edit(msg)
			e.requireReject(e.renew(e.fresh(), msg), msgkit.Malformed, name)
		})
	}
}

// TestRenewPastDeadlineIsExpiredD2: a held lease whose deadline is not
// after committed time cannot renew, whether or not expiry has swept it
// (the spec machine renews an unswept one). Effective-1 (deadline-1)
// renews; effective (deadline) and later are EXPIRED and leave the lease
// unchanged.
func TestRenewPastDeadlineIsExpiredD2(t *testing.T) {
	e := newEnv(t)
	e.seedLease(e.leaseRecord("l1", "c1", "p1"))
	msg := e.renewMsg("l1", "ren-1")
	before := e.storedLease("l1")
	for _, now := range []int64{heldDeadline, heldDeadline + 5} {
		e.requireReject(e.renew(e.at(now), msg), msgkit.Expired, "renew at or after the deadline")
		if !proto.Equal(e.storedLease("l1"), before) {
			t.Fatal("lease changed")
		}
	}
	e.requireOK(e.renew(e.at(heldDeadline-1), msg), "renew at deadline-1")
	if got := e.storedLease("l1").GetDeadlineUnixSeconds(); got != heldDeadline+term {
		t.Fatalf("deadline %d", got)
	}
}

// TestRenewKeepsCommittedBytes (REQ-SEP-001): a renewal cannot change the
// package bytes or the manifest digest (CONFLICT); it moves the deadline
// by exactly one DEC-005 term, adds one to the term version, sets RENEWED,
// moves the deadline row, and leaves the reserved bytes alone. Early
// renewals stack; a stale term version is STALE_VERSION. A DRAINING or
// DISABLED provider can still renew (v0 spec, decision request DR-08-01).
func TestRenewKeepsCommittedBytes(t *testing.T) {
	e := newEnv(t)
	lease := e.mustLease("l1", "c1", "p1")
	changed := e.renewMsg("l1", "ren-bytes")
	changed.PackageBytes = 2 * packageSize
	e.signRenew(changed, "p1", "key-p1")
	e.requireReject(e.renew(e.fresh(), changed), msgkit.Conflict, "bytes change")
	digest := e.renewMsg("l1", "ren-digest")
	digest.ManifestDigest = manifestOf("c9")
	e.signRenew(digest, "p1", "key-p1")
	e.requireReject(e.renew(e.fresh(), digest), msgkit.Conflict, "digest change")

	first := e.renewMsg("l1", "ren-1")
	r := e.renew(e.fresh(), first)
	e.requireOK(r, "first renewal")
	got := e.storedLease("l1")
	if got.GetTermVersion() != 2 || got.GetDeadlineUnixSeconds() != lease.GetDeadlineUnixSeconds()+term ||
		got.GetState() != leasesv1.LeaseState_LEASE_STATE_RENEWED || !got.GetReservationHeld() {
		t.Fatalf("renewed lease %v", got)
	}
	unchanged := proto.Clone(got).(*leasesv1.Lease)
	unchanged.TermVersion, unchanged.DeadlineUnixSeconds, unchanged.State = 1, lease.GetDeadlineUnixSeconds(), lease.GetState()
	if !proto.Equal(unchanged, lease) {
		t.Fatalf("a renewal changed an immutable term: %v", got)
	}
	if e.provider("p1").GetReservedBytes() != packageSize {
		t.Fatal("renewal changed the reserved bytes")
	}
	store := e.leaseStore()
	if store.Has(leases.LeaseDeadlineKey(lease.GetDeadlineUnixSeconds(), "l1")) ||
		!bytes.Equal(store.Get(leases.LeaseDeadlineKey(got.GetDeadlineUnixSeconds(), "l1")), leases.Marker) {
		t.Fatal("deadline row did not move")
	}
	parsed, err := msgkit.ParseTyped(r.events[0])
	if err != nil || len(r.events) != 1 || !proto.Equal(parsed, &eventsv1.LeaseRenewed{LeaseId: "l1", TermVersion: 2,
		DeadlineUnixSeconds: got.GetDeadlineUnixSeconds()}) {
		t.Fatalf("event %v %v", parsed, err)
	}
	e.requireIdempotent(e.renew(e.fresh(), first), "retry")

	stale := e.renewMsg("l1", "ren-stale")
	stale.TermVersion = 2
	stale.DeadlineUnixSeconds = got.GetDeadlineUnixSeconds() + term
	e.signRenew(stale, "p1", "key-p1")
	e.requireReject(e.renew(e.fresh(), stale), msgkit.StaleVersion, "stale term version")

	// Early renewals stack: the deadline moves one term per renewal.
	for i, state := range []providersv1.ProviderState{
		providersv1.ProviderState_PROVIDER_STATE_DRAINING, providersv1.ProviderState_PROVIDER_STATE_DISABLED,
	} {
		e.editProvider("p1", func(p *providersv1.Provider) { p.State = state })
		before := e.storedLease("l1")
		e.requireOK(e.renew(e.fresh(), e.renewMsg("l1", fmt.Sprintf("ren-stack-%d", i))), state.String())
		after := e.storedLease("l1")
		if after.GetTermVersion() != before.GetTermVersion()+1 || after.GetDeadlineUnixSeconds() != before.GetDeadlineUnixSeconds()+term {
			t.Fatalf("renewal %d: %v", i, after)
		}
	}
	if e.storedLease("l1").GetDeadlineUnixSeconds() != lease.GetDeadlineUnixSeconds()+3*term {
		t.Fatal("renewals did not stack")
	}
	if e.provider("p1").GetReservedBytes() != packageSize {
		t.Fatal("renewals changed the reserved bytes")
	}
	e.requireInvariants(nil)
}

// TestEvidenceCannotExtendLease: a renewal that names evidence never
// extends the lease. Live evidence is FORBIDDEN, expired evidence (flag or
// expiry not after committed time) EXPIRED, absent evidence or a nil
// evidence reader (goal 08 wiring) NOT_FOUND. The lease is unchanged.
func TestEvidenceCannotExtendLease(t *testing.T) {
	e := newEnv(t)
	e.seedLease(e.leaseRecord("l1", "c1", "p1"))
	e.evidence.records["ev-live"] = evidenceRecord{expiry: t0 + 1000}
	e.evidence.records["ev-flag"] = evidenceRecord{expiry: t0 + 1000, expired: true}
	e.evidence.records["ev-now"] = evidenceRecord{expiry: t0}
	before := e.storedLease("l1")
	cases := []struct {
		id   string
		want msgkit.Code
	}{
		{"ev-live", msgkit.Forbidden},
		{"ev-flag", msgkit.Expired},
		{"ev-now", msgkit.Expired},
		{"ev-none", msgkit.NotFound},
	}
	for _, tc := range cases {
		msg := e.renewMsg("l1", "ren-"+tc.id)
		msg.EvidenceId = tc.id
		e.signRenew(msg, "p1", "key-p1")
		e.requireReject(e.renew(e.fresh(), msg), tc.want, tc.id)
	}
	// Without an evidence reader (x/evidence is goal 09) every evidence id
	// is NOT_FOUND.
	noEvidence := leases.NewKeeper(e.keys[leases.ModuleName], leases.Deps{
		Roles: e.auth, Limits: e.auth, Params: e.auth, Policy: e.auth, Providers: e.providers, Content: e.content,
	})
	msg := e.renewMsg("l1", "ren-nil-reader")
	msg.EvidenceId = "ev-live"
	e.signRenew(msg, "p1", "key-p1")
	r := e.exec(e.fresh(), func(c sdk.Context) error { _, err := noEvidence.RenewLease(c, msg); return err })
	e.requireReject(r, msgkit.NotFound, "nil evidence reader")
	if !proto.Equal(e.storedLease("l1"), before) {
		t.Fatal("lease changed")
	}
}

// TestDecisionBlockedWithoutDEC005: without the labelled DEC-005 devnet
// number (or with 0) every offer and renewal is DECISION_BLOCKED; a term
// other than the number is OUT_OF_RANGE.
func TestDecisionBlockedWithoutDEC005(t *testing.T) {
	e := newEnv(t)
	e.seedLease(e.leaseRecord("l1", "c1", "p1"))
	offer := e.offerMsg("o1", "c2", "p1")
	renew := e.renewMsg("l1", "ren-1")
	for _, setup := range []func(){
		func() { e.auth.termFound = false },
		func() { e.auth.termFound, e.auth.term = true, 0 },
	} {
		setup()
		e.requireReject(e.offer(e.fresh(), offer), msgkit.DecisionBlocked, "offer")
		e.requireReject(e.renew(e.fresh(), renew), msgkit.DecisionBlocked, "renew")
	}
	e.auth.term = uint64(term) + 60
	e.requireReject(e.offer(e.fresh(), offer), msgkit.OutOfRange, "offer term differs")
	e.requireReject(e.renew(e.fresh(), renew), msgkit.OutOfRange, "renew term differs")
	e.auth.term = uint64(term)
	e.requireOK(e.offer(e.fresh(), offer), "offer with the devnet number")
	e.requireOK(e.renew(e.fresh(), renew), "renew with the devnet number")
}

// TestRetryAfterKeyRotation: RenewLease and EndLease verify the provider
// signature against the provider's CURRENT key before the business gate
// (spec_machine.py order). A renewal or an end that committed and is
// resubmitted under the same business id after the provider rotated its
// key is therefore UNAUTHORIZED, with the state unchanged, not an
// idempotent success (DR-08-12). AcceptLease runs its gate first, so its
// retry stays idempotent. A broadcaster outbox reads the committed result
// by transaction hash or lease state instead of resubmitting.
func TestRetryAfterKeyRotation(t *testing.T) {
	e := newEnv(t)
	e.mustOffer(e.offerMsg("o-l1", "c1", "p1"))
	accept := e.acceptMsg("o-l1", "l1")
	e.requireOK(e.accept(e.fresh(), accept), "accept")
	e.mustLease("l2", "c2", "p1")
	renew := e.renewMsg("l1", "ren-l1")
	e.requireOK(e.renew(e.fresh(), renew), "renew")
	end := e.endMsg("l2", "end-l2")
	e.requireOK(e.end(e.fresh(), end), "end")
	e.requireIdempotent(e.renew(e.fresh(), renew), "renew retry before rotation")
	e.requireIdempotent(e.end(e.fresh(), end), "end retry before rotation")

	e.editProvider("p1", func(p *providersv1.Provider) {
		p.SigningPubkey, p.SigningKeyId, p.KeyGeneration = pub("p1-new"), "key-p1b", 2
	})
	e.requireReject(e.renew(e.fresh(), renew), msgkit.Unauthorized, "renew retry after rotation")
	e.requireReject(e.end(e.fresh(), end), msgkit.Unauthorized, "end retry after rotation")
	e.requireIdempotent(e.accept(e.fresh(), accept), "accept retry after rotation")
	if got := e.storedLease("l1"); got.GetTermVersion() != 2 || got.GetState() != leasesv1.LeaseState_LEASE_STATE_RENEWED {
		t.Fatalf("l1 after the refused retry: %v", got)
	}
	if got := e.storedLease("l2"); got.GetState() != leasesv1.LeaseState_LEASE_STATE_ENDED {
		t.Fatalf("l2 after the refused retry: %v", got)
	}
	e.requireInvariants(nil)
}
