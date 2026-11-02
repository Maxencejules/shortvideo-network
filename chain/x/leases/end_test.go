package leases_test

import (
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

type endFault = fault[*leasesv1.MsgEndLease]

func endEdit(name string, want msgkit.Code, edit func(m *leasesv1.MsgEndLease)) endFault {
	return endFault{name: name, want: want, apply: func(_ *env, c *call[*leasesv1.MsgEndLease]) { edit(c.msg) }}
}

func endEnv(name string, want msgkit.Code, edit func(e *env)) endFault {
	return endFault{name: name, want: want, apply: func(e *env, _ *call[*leasesv1.MsgEndLease]) { edit(e) }}
}

func endLadderSetup(e *env) { e.seedLease(e.leaseRecord("l1", "c1", "p1")) }

func signEndCall(e *env, c *call[*leasesv1.MsgEndLease]) {
	e.signEnd(c.msg, "p1", "p1", "key-p1", 1)
	c.editSignature(c.msg.ProviderSignature)
}

// The accounting faults shared by both end paths.
func endAccountingFaults() []endFault {
	return []endFault{
		endEdit("gate-business-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgEndLease) { m.BusinessId = "END-1" }),
		replayFault(func(m *leasesv1.MsgEndLease) string { return m.GetBusinessId() }),
		endEnv("not-held", msgkit.Underflow, func(e *env) { e.endLeaseRecord("l1") }),
		endEnv("release-underflow", msgkit.Underflow, func(e *env) {
			e.editProvider("p1", func(p *providersv1.Provider) { p.ReservedBytes = packageSize - 1 })
		}),
	}
}

// TestEndCheckOrder is the EndLease ladder for a provider-signed end: one
// row per check of op_end (spec_machine.py:858) with authority_end false.
func TestEndCheckOrder(t *testing.T) {
	faults := []endFault{
		gasFault[*leasesv1.MsgEndLease](),
		txBytesFault[*leasesv1.MsgEndLease](),
		signerLenFault(func(m *leasesv1.MsgEndLease, s []byte) { m.Signer = s }),
		endEdit("presence-business-id", msgkit.Malformed, func(m *leasesv1.MsgEndLease) { m.BusinessId = "" }),
		endEdit("lease-not-found", msgkit.NotFound, func(m *leasesv1.MsgEndLease) { m.LeaseId = "l-none" }),
		{name: "provider-signature-absent", want: msgkit.Malformed, apply: func(_ *env, c *call[*leasesv1.MsgEndLease]) { c.unsigned = true }},
	}
	faults = append(faults, shapeFaults[*leasesv1.MsgEndLease](typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW)...)
	faults = append(faults,
		sigFault[*leasesv1.MsgEndLease]("signature-ed25519-mismatch", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.Signature[0] ^= 1 }),
		// D-12: the end is signed for term 1; the stored lease is at term 2.
		endEnv("signature-earlier-term-version", msgkit.Unauthorized, func(e *env) {
			renewed := e.storedLease("l1")
			renewed.TermVersion = 2
			e.k.SeedLeaseForTest(e.unmetered(), renewed)
		}),
		// op_end returns FORBIDDEN for a signer without a role (:893).
		endEdit("role-none", msgkit.Forbidden, func(m *leasesv1.MsgEndLease) { m.Signer = outsiderAcct }),
		endEdit("role-service", msgkit.Forbidden, func(m *leasesv1.MsgEndLease) { m.Signer = serviceAcct }),
		endEdit("provider-signer-not-the-provider", msgkit.Forbidden, func(m *leasesv1.MsgEndLease) { m.Signer = provider2Acct }),
	)
	faults = append(faults, endAccountingFaults()...)
	ladder[*leasesv1.MsgEndLease]{
		setup: endLadderSetup,
		base: func(*env) *leasesv1.MsgEndLease {
			return &leasesv1.MsgEndLease{Signer: broadcasterAcct, BusinessId: "end-1", LeaseId: "l1"}
		},
		sign:   signEndCall,
		faults: faults,
		exec:   func(e *env, ctx sdk.Context, m *leasesv1.MsgEndLease) result { return e.end(ctx, m) },
	}.run(t)
}

// TestAuthorityEndCheckOrder is the EndLease ladder for authority_end: one
// AUTHORITY signature, outside DEC-009; a provider signature is
// UNKNOWN_FIELD.
func TestAuthorityEndCheckOrder(t *testing.T) {
	faults := []endFault{
		gasFault[*leasesv1.MsgEndLease](),
		txBytesFault[*leasesv1.MsgEndLease](),
		signerLenFault(func(m *leasesv1.MsgEndLease, s []byte) { m.Signer = s }),
		endEdit("presence-business-id", msgkit.Malformed, func(m *leasesv1.MsgEndLease) { m.BusinessId = "" }),
		endEdit("lease-not-found", msgkit.NotFound, func(m *leasesv1.MsgEndLease) { m.LeaseId = "l-none" }),
		endEdit("role-none", msgkit.Unauthorized, func(m *leasesv1.MsgEndLease) { m.Signer = outsiderAcct }),
		endEdit("role-broadcaster", msgkit.Forbidden, func(m *leasesv1.MsgEndLease) { m.Signer = broadcasterAcct }),
		{name: "provider-signature-present", want: msgkit.UnknownField, apply: func(e *env, c *call[*leasesv1.MsgEndLease]) {
			e.signEnd(c.msg, "p1", "p1", "key-p1", 1)
		}},
	}
	faults = append(faults, endAccountingFaults()...)
	ladder[*leasesv1.MsgEndLease]{
		setup:  endLadderSetup,
		base:   func(*env) *leasesv1.MsgEndLease { return authorityEndMsg("l1", "end-a") },
		faults: faults,
		exec:   func(e *env, ctx sdk.Context, m *leasesv1.MsgEndLease) result { return e.end(ctx, m) },
	}.run(t)
}

// TestEndPresence: an empty business_id or lease_id is MALFORMED before
// the lease lookup.
func TestEndPresence(t *testing.T) {
	for name, edit := range map[string]func(m *leasesv1.MsgEndLease){
		"business-id": func(m *leasesv1.MsgEndLease) { m.BusinessId = "" },
		"lease-id":    func(m *leasesv1.MsgEndLease) { m.LeaseId = "" },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.seedLease(e.leaseRecord("l1", "c1", "p1"))
			msg := e.endMsg("l1", "end-1")
			edit(msg)
			e.requireReject(e.end(e.fresh(), msg), msgkit.Malformed, name)
		})
	}
}

// TestEndReleasesOnce (REQ-TX-005): a provider end and an authority end
// each release the package bytes exactly once and leave the lease ENDED
// without a reservation; a retry with the same business id and payload is
// idempotent; a new business id is UNDERFLOW; an end after expiry is
// UNDERFLOW. Ending works for a disabled provider and for withdrawn
// content.
func TestEndReleasesOnce(t *testing.T) {
	e := newEnv(t)
	e.mustLease("l1", "c1", "p1")
	e.mustLease("l2", "c2", "p1")
	e.mustLease("l3", "c3", "p2")
	if got := e.provider("p1").GetReservedBytes(); got != 2*packageSize {
		t.Fatalf("reserved %d", got)
	}

	end := e.endMsg("l1", "end-l1")
	end.Signer = providerAcct // the provider's own account
	r := e.end(e.fresh(), end)
	e.requireOK(r, "provider end")
	got := e.storedLease("l1")
	if got.GetState() != leasesv1.LeaseState_LEASE_STATE_ENDED || got.GetReservationHeld() {
		t.Fatalf("ended lease %v", got)
	}
	if e.provider("p1").GetReservedBytes() != packageSize {
		t.Fatal("end did not release exactly once")
	}
	for _, key := range [][]byte{
		leases.PairKey("p1", "c1", 1), leases.LeaseDeadlineKey(got.GetDeadlineUnixSeconds(), "l1"),
		leases.ContentLeaseKey("c1", "l1"), leases.ProviderLeaseKey("p1", "l1"),
	} {
		if e.leaseStore().Has(key) {
			t.Fatalf("row %x still present", key)
		}
	}
	parsed, err := msgkit.ParseTyped(r.events[0])
	if err != nil || len(r.events) != 1 || !proto.Equal(parsed, &eventsv1.LeaseEnded{LeaseId: "l1", State: "ENDED", ReleasedBytes: "1000"}) {
		t.Fatalf("event %v %v", parsed, err)
	}
	e.requireIdempotent(e.end(e.fresh(), end), "retry")
	again := e.endMsg("l1", "end-l1-again")
	e.requireReject(e.end(e.fresh(), again), msgkit.Underflow, "second end with a new business id")
	e.requireReject(e.end(e.fresh(), authorityEndMsg("l1", "end-l1-auth")), msgkit.Underflow, "authority end after end")

	// Authority end of a disabled provider's lease of withdrawn content.
	e.editProvider("p1", func(p *providersv1.Provider) { p.State = providersv1.ProviderState_PROVIDER_STATE_DISABLED })
	e.setContentState("c2", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
	e.requireOK(e.end(e.fresh(), authorityEndMsg("l2", "end-l2")), "authority end")
	if e.provider("p1").GetReservedBytes() != 0 {
		t.Fatal("authority end did not release")
	}

	// Expiry releases l3; ending it afterwards is UNDERFLOW.
	if err := e.expire(e.at(e.storedLease("l3").GetDeadlineUnixSeconds())); err != nil {
		t.Fatal(err)
	}
	if e.provider("p2").GetReservedBytes() != 0 || e.storedLease("l3").GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED {
		t.Fatal("expiry did not release l3")
	}
	e.requireReject(e.end(e.fresh(), e.endMsg("l3", "end-l3")), msgkit.Underflow, "end after expiry")
	e.requireInvariants(nil)
}

// TestEndSignedForEarlierTermD12: the end payload binds the lease's term
// version (DR-08-04, deviation D-12). An end the provider signed at term 1
// does not end the lease after the provider renewed it to term 2, even
// when it was already public (here: committed in a transaction the
// handler rejected with REPLAY). Relayed by the broadcaster under a fresh
// business id it is UNAUTHORIZED with the trial state unchanged; the
// renewal stays in force. An end signed for term 2 then ends the lease,
// and its idempotent retry still verifies (an end changes no term
// version).
func TestEndSignedForEarlierTermD12(t *testing.T) {
	e := newEnv(t)
	e.mustLease("l1", "c1", "p1")
	stale := &leasesv1.MsgEndLease{Signer: broadcasterAcct, BusinessId: "acc-l1", LeaseId: "l1"}
	e.signEnd(stale, "p1", "p1", "key-p1", 1)
	e.requireReject(e.end(e.fresh(), stale), msgkit.Replay, "term-1 end under a used business id")
	e.requireOK(e.renew(e.fresh(), e.renewMsg("l1", "renew-l1")), "renew to term 2")
	if got := e.storedLease("l1").GetTermVersion(); got != 2 {
		t.Fatalf("term version %d", got)
	}
	reserved := e.provider("p1").GetReservedBytes()
	stale.BusinessId = "end-stale"
	e.requireReject(e.end(e.fresh(), stale), msgkit.Unauthorized, "term-1 end after renewal to term 2")
	byProvider := proto.Clone(stale).(*leasesv1.MsgEndLease)
	byProvider.Signer = providerAcct
	e.requireReject(e.end(e.fresh(), byProvider), msgkit.Unauthorized, "term-1 end sent by the provider's account")
	if l := e.storedLease("l1"); l.GetState() != leasesv1.LeaseState_LEASE_STATE_RENEWED || !l.GetReservationHeld() ||
		e.provider("p1").GetReservedBytes() != reserved {
		t.Fatalf("renewed lease changed: %v reserved %d", l, e.provider("p1").GetReservedBytes())
	}
	current := e.endMsg("l1", "end-current")
	e.requireOK(e.end(e.fresh(), current), "term-2 end")
	e.requireIdempotent(e.end(e.fresh(), current), "term-2 end retry")
	e.requireInvariants(nil)
}
