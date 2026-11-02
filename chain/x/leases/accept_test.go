package leases_test

import (
	"bytes"
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

type acceptFault = fault[*leasesv1.MsgAcceptLease]

func acceptEdit(name string, want msgkit.Code, edit func(m *leasesv1.MsgAcceptLease)) acceptFault {
	return acceptFault{name: name, want: want, apply: func(_ *env, c *call[*leasesv1.MsgAcceptLease]) { edit(c.msg) }}
}

func acceptEnv(name string, want msgkit.Code, edit func(e *env, c *call[*leasesv1.MsgAcceptLease])) acceptFault {
	return acceptFault{name: name, want: want, apply: edit}
}

// lateOffer is an open offer of the same pair as o1 whose real-time
// deadline (t0-100) has passed while the offer itself (expiry t0+600) has
// not: the D-3 case.
func (e *env) lateOffer() *leasesv1.Offer {
	o := e.offerRecord("o-late", "c1", "p1")
	o.StartUnixSeconds = t0 - 100 - term
	o.DeadlineUnixSeconds = t0 - 100
	return o
}

func acceptLadderSetup(e *env) {
	e.seedOffer(e.offerRecord("o1", "c1", "p1"))
	e.seedOffer(e.lateOffer())
	ended := e.leaseRecord("l-exists", "c3", "p2")
	ended.State = leasesv1.LeaseState_LEASE_STATE_EXPIRED
	ended.ReservationHeld = false
	e.seedLease(ended)
}

// signAccept signs the acceptance of the offer the message names (o1 when
// it names no open offer) and the message's lease id with p1's key.
func signAccept(e *env, c *call[*leasesv1.MsgAcceptLease]) {
	offer, found, err := e.k.OfferForTest(e.unmetered(), c.msg.GetOfferId())
	if err != nil || !found {
		offer = e.offerRecord("o1", "c1", "p1")
	}
	sig := acceptSig(e.t, offer, c.msg.GetLeaseId(), "p1", "key-p1")
	c.editSignature(sig)
	c.msg.ProviderAcceptance = sig
}

// TestAcceptCheckOrder is the AcceptLease ladder: one row per check of
// op_accept (spec_machine.py:685) with deviation D-1 (ed25519 after the
// offer lookup), then the chain-only check D-3.
func TestAcceptCheckOrder(t *testing.T) {
	faults := []acceptFault{
		gasFault[*leasesv1.MsgAcceptLease](),
		txBytesFault[*leasesv1.MsgAcceptLease](),
		signerLenFault(func(m *leasesv1.MsgAcceptLease, s []byte) { m.Signer = s }),
		acceptEdit("presence-business-id", msgkit.Malformed, func(m *leasesv1.MsgAcceptLease) { m.BusinessId = "" }),
		acceptEdit("role-none", msgkit.Unauthorized, func(m *leasesv1.MsgAcceptLease) { m.Signer = outsiderAcct }),
		acceptEdit("role-service", msgkit.Forbidden, func(m *leasesv1.MsgAcceptLease) { m.Signer = serviceAcct }),
		acceptEdit("offer-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgAcceptLease) { m.OfferId = "o1 " }),
		acceptEdit("lease-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgAcceptLease) { m.LeaseId = "L1" }),
	}
	faults = append(faults, shapeFaults[*leasesv1.MsgAcceptLease](typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW)...)
	faults = append(faults,
		acceptEdit("gate-business-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgAcceptLease) { m.BusinessId = "acc l1" }),
		replayFault(func(m *leasesv1.MsgAcceptLease) string { return m.GetBusinessId() }),
		acceptEdit("offer-not-open", msgkit.NotFound, func(m *leasesv1.MsgAcceptLease) { m.OfferId = "o-none" }),
		sigFault[*leasesv1.MsgAcceptLease]("d1-ed25519-mismatch", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.Signature[0] ^= 1 }),
		acceptEdit("provider-signer-not-the-provider", msgkit.Forbidden, func(m *leasesv1.MsgAcceptLease) { m.Signer = provider2Acct }),
		acceptEdit("lease-id-exists", msgkit.DuplicateID, func(m *leasesv1.MsgAcceptLease) { m.LeaseId = "l-exists" }),
		acceptEnv("offer-expired", msgkit.Expired, func(_ *env, c *call[*leasesv1.MsgAcceptLease]) { c.ctx = withTime(c.ctx, t0+600) }),
		acceptEnv("content-withdrawn", msgkit.Withdrawn, func(e *env, _ *call[*leasesv1.MsgAcceptLease]) {
			e.setContentState("c1", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
		}),
		acceptEnv("provider-draining", msgkit.Forbidden, func(e *env, _ *call[*leasesv1.MsgAcceptLease]) {
			e.editProvider("p1", func(p *providersv1.Provider) { p.State = providersv1.ProviderState_PROVIDER_STATE_DRAINING })
		}),
		acceptEnv("live-pair", msgkit.DuplicateID, func(e *env, _ *call[*leasesv1.MsgAcceptLease]) {
			e.k.SeedLeaseForTest(e.unmetered(), e.leaseRecord("l-live", "c1", "p1"))
		}),
		acceptEnv("reserve-overflow", msgkit.Overflow, func(e *env, _ *call[*leasesv1.MsgAcceptLease]) {
			e.editProvider("p1", func(p *providersv1.Provider) {
				p.ReservedBytes, p.AdmittedCapacityBytes = math.MaxUint64-10, math.MaxUint64
			})
		}),
		acceptEnv("reserve-capacity", msgkit.Capacity, func(e *env, _ *call[*leasesv1.MsgAcceptLease]) {
			e.editProvider("p1", func(p *providersv1.Provider) { p.AdmittedCapacityBytes = p.ReservedBytes + packageSize - 1 })
		}),
		acceptEdit("d3-offer-deadline-passed", msgkit.Expired, func(m *leasesv1.MsgAcceptLease) { m.OfferId = "o-late" }),
	)
	ladder[*leasesv1.MsgAcceptLease]{
		setup: acceptLadderSetup,
		base: func(e *env) *leasesv1.MsgAcceptLease {
			return &leasesv1.MsgAcceptLease{Signer: broadcasterAcct, BusinessId: "acc-l1", OfferId: "o1", LeaseId: "l1"}
		},
		sign:   signAccept,
		faults: faults,
		exec:   func(e *env, ctx sdk.Context, m *leasesv1.MsgAcceptLease) result { return e.accept(ctx, m) },
	}.run(t)
}

// TestAcceptPresence: an empty business_id, offer_id, or lease_id, or a
// nil provider_acceptance, is MALFORMED before the role check.
func TestAcceptPresence(t *testing.T) {
	cases := map[string]func(m *leasesv1.MsgAcceptLease){
		"business-id":         func(m *leasesv1.MsgAcceptLease) { m.BusinessId = "" },
		"offer-id":            func(m *leasesv1.MsgAcceptLease) { m.OfferId = "" },
		"lease-id":            func(m *leasesv1.MsgAcceptLease) { m.LeaseId = "" },
		"provider-acceptance": func(m *leasesv1.MsgAcceptLease) { m.ProviderAcceptance = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.mustOffer(e.offerMsg("o1", "c1", "p1"))
			msg := e.acceptMsg("o1", "l1")
			edit(msg)
			msg.Signer = outsiderAcct
			e.requireReject(e.accept(e.fresh(), msg), msgkit.Malformed, name)
		})
	}
}

// TestAcceptCheckOrderDeviationD1: the spec checks the abstract signature
// flag before the business gate and the offer lookup; the chain verifies
// ed25519 after both, because the signed terms are the stored offer's. So
// an invalid acceptance of an absent offer is NOT_FOUND (spec:
// UNAUTHORIZED), and an invalid acceptance under a business id committed
// with another payload is REPLAY (spec: UNAUTHORIZED). With a fresh
// business id and an open offer both say UNAUTHORIZED.
func TestAcceptCheckOrderDeviationD1(t *testing.T) {
	e := newEnv(t)
	e.mustOffer(e.offerMsg("o1", "c1", "p1"))
	bad := e.acceptMsg("o1", "l1")
	bad.ProviderAcceptance.Signature[5] ^= 0x40
	e.requireReject(e.accept(e.fresh(), bad), msgkit.Unauthorized, "open offer, fresh business id")

	absent := proto.Clone(bad).(*leasesv1.MsgAcceptLease)
	absent.OfferId = "o-none"
	e.requireReject(e.accept(e.fresh(), absent), msgkit.NotFound, "absent offer")

	e.peer.digests["acc-l1"] = bytes.Repeat([]byte{9}, 32)
	e.requireReject(e.accept(e.fresh(), bad), msgkit.Replay, "reused business id")
}

// TestAcceptPastOfferDeadlineIsExpiredD3: an open offer whose real-time
// deadline is not after committed time cannot become a lease (EXPIRED,
// after every spec check). One second before the deadline it can.
func TestAcceptPastOfferDeadlineIsExpiredD3(t *testing.T) {
	e := newEnv(t)
	o := e.offerRecord("o1", "c1", "p1")
	o.StartUnixSeconds, o.DeadlineUnixSeconds, o.ExpiryUnixSeconds = t0-term+50, t0+50, t0+600
	e.seedOffer(o)
	// Effective-1: one second before the deadline the offer is accepted.
	e.requireOK(e.accept(e.at(t0+49), e.acceptMsg("o1", "l1")), "deadline-1")
	// Effective: at and after the deadline it is EXPIRED (fresh stores).
	e2 := newEnv(t)
	e2.seedOffer(o)
	e2.requireReject(e2.accept(e2.at(t0+50), e2.acceptMsg("o1", "l1")), msgkit.Expired, "at the deadline")
	e2.requireReject(e2.accept(e2.at(t0+51), e2.acceptMsg("o1", "l1")), msgkit.Expired, "after the deadline")
}

// TestServiceKeyCannotSignAcceptance (REQ-CHAIN-012): a provider
// acceptance verifies only under the provider's current registered key in
// the PROVIDER_ACCEPT domain. A service key presented with the provider's
// key id or with its own key id, another provider's key, a retired
// provider key, and a RENEW-domain signature are rejected; a service
// account as the outer signer is FORBIDDEN even with the provider's valid
// acceptance; a broadcaster relaying the provider's valid acceptance
// succeeds.
func TestServiceKeyCannotSignAcceptance(t *testing.T) {
	e := newEnv(t)
	e.mustOffer(e.offerMsg("o1", "c1", "p1"))
	offer := e.storedOffer("o1")
	base := func() *leasesv1.MsgAcceptLease {
		return &leasesv1.MsgAcceptLease{Signer: broadcasterAcct, BusinessId: "acc-l1", OfferId: "o1", LeaseId: "l1"}
	}
	signBytes, err := leases.AcceptSignBytes(offer, "l1")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		sig  *typesv1.InnerSignature
		want msgkit.Code
	}{
		{"service-key-with-provider-key-id", acceptSig(t, offer, "l1", "svc", "key-p1"), msgkit.Unauthorized},
		{"service-key-with-own-key-id", acceptSig(t, offer, "l1", "svc", "key-svc"), msgkit.Unauthorized},
		{"other-provider-key", acceptSig(t, offer, "l1", "p2", "key-p2"), msgkit.Unauthorized},
		{"provider-key-id-service-public-key", func() *typesv1.InnerSignature {
			s := acceptSig(t, offer, "l1", "p1", "key-p1")
			s.PublicKey = pub("svc")
			return s
		}(), msgkit.Unauthorized},
		{"renew-domain", func() *typesv1.InnerSignature {
			s := sign(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, signBytes, "p1", "key-p1")
			s.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW
			return s
		}(), msgkit.WrongDomain},
		{"accept-domain-label-renew-bytes", func() *typesv1.InnerSignature {
			// The accept domain with a signature over the same payload in
			// the RENEW domain string: the preimage differs.
			canonical, err := leases.AcceptCanonicalForTest(offer, "l1")
			if err != nil {
				t.Fatal(err)
			}
			return sign(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,
				msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, canonical), "p1", "key-p1")
		}(), msgkit.Unauthorized},
	}
	for _, tc := range cases {
		msg := base()
		msg.ProviderAcceptance = tc.sig
		e.requireReject(e.accept(e.fresh(), msg), tc.want, tc.name)
	}
	// A service account cannot relay the provider's valid acceptance.
	valid := e.acceptMsg("o1", "l1")
	valid.Signer = serviceAcct
	e.requireReject(e.accept(e.fresh(), valid), msgkit.Forbidden, "service outer signer")

	// A retired key: rotate p1 in the registry; the old key no longer
	// verifies and the new key does.
	e.editProvider("p1", func(p *providersv1.Provider) {
		p.SigningPubkey, p.SigningKeyId, p.KeyGeneration = pub("p1-new"), "key-p1b", 2
	})
	old := base()
	old.ProviderAcceptance = acceptSig(t, offer, "l1", "p1", "key-p1")
	e.requireReject(e.accept(e.fresh(), old), msgkit.Unauthorized, "retired key")
	current := base()
	current.ProviderAcceptance = acceptSig(t, offer, "l1", "p1-new", "key-p1b")
	e.requireOK(e.accept(e.fresh(), current), "broadcaster relays the current key")
}

// TestAcceptReservesOnceAndBindsTerms (REQ-TX-003, REQ-MODU-004): the lease
// carries the offer's pair, terms digest, deadline, reservation, and
// status; the provider's reserved bytes grow by the package bytes once;
// the offer and its rows are gone, the used-id marker stays; an acceptance
// signed over any other term is UNAUTHORIZED.
func TestAcceptReservesOnceAndBindsTerms(t *testing.T) {
	e := newEnv(t)
	e.auth.policy = 7
	e.mustOffer(e.offerMsg("o1", "c1", "p1"))
	offer := e.storedOffer("o1")

	mutations := map[string]func(o *leasesv1.Offer){
		"deadline":        func(o *leasesv1.Offer) { o.DeadlineUnixSeconds++ },
		"start":           func(o *leasesv1.Offer) { o.StartUnixSeconds-- },
		"expiry":          func(o *leasesv1.Offer) { o.ExpiryUnixSeconds++ },
		"manifest":        func(o *leasesv1.Offer) { o.ManifestDigest = manifestOf("c2") },
		"terms":           func(o *leasesv1.Offer) { o.TermsDigest = digestOf("other terms") },
		"bytes":           func(o *leasesv1.Offer) { o.PackageBytes++ },
		"content":         func(o *leasesv1.Offer) { o.ContentId = "c2" },
		"content-version": func(o *leasesv1.Offer) { o.ContentVersion = 2 },
		"provider":        func(o *leasesv1.Offer) { o.ProviderId = "p2" },
		"offer-id":        func(o *leasesv1.Offer) { o.OfferId = "o2" },
		"term-version":    func(o *leasesv1.Offer) { o.TermVersion = 2 },
		"scheduler":       func(o *leasesv1.Offer) { o.Scheduler = service2Acct },
	}
	for name, mutate := range mutations {
		signed := proto.Clone(offer).(*leasesv1.Offer)
		mutate(signed)
		msg := &leasesv1.MsgAcceptLease{Signer: broadcasterAcct, BusinessId: "acc-" + name, OfferId: "o1", LeaseId: "l1",
			ProviderAcceptance: acceptSig(t, signed, "l1", "p1", "key-p1")}
		e.requireReject(e.accept(e.fresh(), msg), msgkit.Unauthorized, "signed "+name)
	}
	otherLease := &leasesv1.MsgAcceptLease{Signer: broadcasterAcct, BusinessId: "acc-other", OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: acceptSig(t, offer, "l2", "p1", "key-p1")}
	e.requireReject(e.accept(e.fresh(), otherLease), msgkit.Unauthorized, "signed another lease id")

	msg := e.acceptMsg("o1", "l1")
	msg.Signer = providerAcct // the provider's own account may submit
	r := e.accept(e.fresh(), msg)
	e.requireOK(r, "accept")
	want := &leasesv1.Lease{
		LeaseId: "l1", OfferId: "o1", ContentId: "c1", ContentVersion: 1, ProviderId: "p1",
		ManifestDigest: offer.GetManifestDigest(), PackageBytes: offer.GetPackageBytes(), TermsDigest: offer.GetTermsDigest(),
		StartUnixSeconds: offer.GetStartUnixSeconds(), DeadlineUnixSeconds: offer.GetDeadlineUnixSeconds(),
		State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, TermVersion: 1, PolicyVersion: 7, ReservationHeld: true,
	}
	if got := e.storedLease("l1"); !proto.Equal(got, want) {
		t.Fatalf("lease %v\nwant %v", got, want)
	}
	if got := e.provider("p1").GetReservedBytes(); got != packageSize {
		t.Fatalf("reserved %d, want %d", got, packageSize)
	}
	store := e.leaseStore()
	for _, gone := range [][]byte{
		leases.OfferKey("o1"), leases.OfferExpiryKey(offer.GetExpiryUnixSeconds(), "o1"), leases.ContentOfferKey("c1", "o1"),
		leases.ProviderOfferCountKey("p1"), leases.ContentOfferCountKey("c1"),
	} {
		if store.Has(gone) {
			t.Fatalf("row %x still present", gone)
		}
	}
	if !bytes.Equal(store.Get(leases.PairKey("p1", "c1", 1)), []byte("l1")) {
		t.Fatal("pair row")
	}
	for _, present := range [][]byte{
		leases.LeaseDeadlineKey(offer.GetDeadlineUnixSeconds(), "l1"), leases.ContentLeaseKey("c1", "l1"),
		leases.ProviderLeaseKey("p1", "l1"),
	} {
		if !bytes.Equal(store.Get(present), leases.Marker) {
			t.Fatalf("row %x missing", present)
		}
	}
	// The used-offer marker stays and keeps the offer's scheduler: the
	// lease's sponsor is in committed state after the offer is gone.
	if !bytes.Equal(store.Get(leases.UsedOfferKey("o1")), serviceAcct) {
		t.Fatalf("used-offer marker %x, want the scheduler", store.Get(leases.UsedOfferKey("o1")))
	}
	if store.Has(leases.SchedulerOfferCountKey("c1", serviceAcct)) {
		t.Fatal("the scheduler counter of the accepted offer remains")
	}
	if len(r.events) != 1 {
		t.Fatalf("events %v", r.events)
	}
	parsed, err := msgkit.ParseTyped(r.events[0])
	if err != nil || !proto.Equal(parsed, &eventsv1.LeaseAccepted{LeaseId: "l1", ProviderId: "p1", ContentId: "c1",
		ContentVersion: 1, PackageBytes: "1000", DeadlineUnixSeconds: offer.GetDeadlineUnixSeconds()}) {
		t.Fatalf("event %v %v", parsed, err)
	}
	// A retry of the same acceptance (same business id and payload) is
	// idempotent and reserves nothing more.
	e.requireIdempotent(e.accept(e.fresh(), msg), "retry")
	if got := e.provider("p1").GetReservedBytes(); got != packageSize {
		t.Fatalf("reserved after retry %d", got)
	}
	e.requireInvariants(nil)
}

// TestDuplicateReplicaRejected (REQ-INV-001): a provider holds at most one
// live lease per (content, version). A second offer for a leased pair is
// DUPLICATE_ID; of two open offers for one pair only the first accept
// succeeds; another provider is a second replica; after end or expiry the
// pair is free again.
func TestDuplicateReplicaRejected(t *testing.T) {
	e := newEnv(t)
	e.mustLease("l1", "c1", "p1")
	again := e.offerMsg("o-again", "c1", "p1")
	e.requireReject(e.offer(e.fresh(), again), msgkit.DuplicateID, "offer for a leased pair")

	e.mustOffer(e.offerMsg("o-a", "c2", "p1"))
	e.mustOffer(e.offerMsg("o-b", "c2", "p1"))
	e.requireOK(e.accept(e.fresh(), e.acceptMsg("o-a", "l-a")), "first accept")
	e.requireReject(e.accept(e.fresh(), e.acceptMsg("o-b", "l-b")), msgkit.DuplicateID, "second accept of the pair")

	e.mustLease("l1-p2", "c1", "p2") // a second replica on another provider
	if got := e.provider("p2").GetReservedBytes(); got != packageSize {
		t.Fatalf("p2 reserved %d", got)
	}

	e.requireOK(e.end(e.fresh(), e.endMsg("l1", "end-l1")), "end")
	e.requireOK(e.offer(e.fresh(), again), "offer after end")

	// Expiry frees the pair too.
	lease := e.storedLease("l-a")
	if err := e.expire(e.at(lease.GetDeadlineUnixSeconds())); err != nil {
		t.Fatal(err)
	}
	if e.storedLease("l-a").GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED {
		t.Fatal("l-a did not expire")
	}
	later := lease.GetDeadlineUnixSeconds()
	e.requireOK(e.offer(e.at(later), e.offerMsgAt("o-c", "c2", "p1", later)), "offer after expiry")
	e.requireOK(e.accept(e.at(later), e.acceptMsg("o-c", "l-c")), "accept after expiry")
	e.requireInvariants(nil)
}

// TestCapacityAndOverflow (REQ-INV-002, REQ-INV-004): the offer probes the
// reservation (CAPACITY / OVERFLOW, nothing reserved) and the accept
// repeats it against the reserved bytes at accept time. reserved +
// package_bytes == capacity is accepted.
func TestCapacityAndOverflow(t *testing.T) {
	e := newEnv(t)
	e.editProvider("p1", func(p *providersv1.Provider) { p.AdmittedCapacityBytes = 1500 })
	e.mustOffer(e.offerMsg("o-a", "c1", "p1"))
	e.mustOffer(e.offerMsg("o-b", "c2", "p1"))
	e.requireOK(e.accept(e.fresh(), e.acceptMsg("o-a", "l-a")), "first accept")
	e.requireReject(e.accept(e.fresh(), e.acceptMsg("o-b", "l-b")), msgkit.Capacity, "accept over capacity")
	e.requireReject(e.offer(e.fresh(), e.offerMsg("o-c", "c3", "p1")), msgkit.Capacity, "offer over capacity")
	if got := e.provider("p1").GetReservedBytes(); got != packageSize {
		t.Fatalf("reserved %d", got)
	}
	// Exactly at capacity.
	e.editProvider("p1", func(p *providersv1.Provider) { p.AdmittedCapacityBytes = 2000 })
	e.requireOK(e.accept(e.fresh(), e.acceptMsg("o-b", "l-b")), "accept at capacity")
	if got := e.provider("p1").GetReservedBytes(); got != 2000 {
		t.Fatalf("reserved %d", got)
	}
	e.requireInvariants(nil)

	// OVERFLOW: a provider near 2^64 (counter fixture, baseline outside
	// any lease).
	e.editProvider("p2", func(p *providersv1.Provider) {
		p.AdmittedCapacityBytes, p.ReservedBytes = math.MaxUint64, math.MaxUint64-5000
	})
	e.mustOffer(e.offerMsg("o-x", "c3", "p2"))
	e.mustOffer(e.offerMsg("o-y", "c1", "p2"))
	e.editProvider("p2", func(p *providersv1.Provider) { p.ReservedBytes = math.MaxUint64 - 500 })
	e.requireReject(e.accept(e.fresh(), e.acceptMsg("o-x", "l-x")), msgkit.Overflow, "accept overflow")
	e.requireReject(e.offer(e.fresh(), e.offerMsg("o-z", "c2", "p2")), msgkit.Overflow, "offer overflow")
	e.editProvider("p2", func(p *providersv1.Provider) { p.ReservedBytes = math.MaxUint64 - packageSize })
	e.requireOK(e.accept(e.fresh(), e.acceptMsg("o-x", "l-x")), "accept up to 2^64-1")
	if got := e.provider("p2").GetReservedBytes(); got != math.MaxUint64 {
		t.Fatalf("reserved %d", got)
	}
	e.requireInvariants(map[string]uint64{"p2": math.MaxUint64 - packageSize})
}

// TestAcceptReplayByAnotherProviderD13: the AcceptLease business digest
// (the spec's field set) does not cover the signer. A replay of a committed
// acceptance (same business id and payload) by an ADMITTED_PROVIDER account
// that is not the lease's provider account is FORBIDDEN with the state
// unchanged, as a fresh acceptance by that account is; the spec reports an
// idempotent success to it (deviation D-13, DR-08-14). A BROADCASTER relays
// for every provider, so its replay stays idempotent, and so does the
// provider's own account.
func TestAcceptReplayByAnotherProviderD13(t *testing.T) {
	e := newEnv(t)
	e.mustOffer(e.offerMsg("o1", "c1", "p1"))
	relayed := e.acceptMsg("o1", "l1")
	e.requireOK(e.accept(e.fresh(), relayed), "accept relayed by the broadcaster")
	copied := proto.Clone(relayed).(*leasesv1.MsgAcceptLease)
	copied.Signer = provider2Acct
	e.requireReject(e.accept(e.fresh(), copied), msgkit.Forbidden, "broadcaster's acceptance replayed by p2's account")
	own := proto.Clone(relayed).(*leasesv1.MsgAcceptLease)
	own.Signer = providerAcct
	e.requireIdempotent(e.accept(e.fresh(), own), "replay by p1's own account")
	e.requireIdempotent(e.accept(e.fresh(), relayed), "broadcaster replay")

	e.mustOffer(e.offerMsg("o2", "c2", "p1"))
	byOwner := e.acceptMsg("o2", "l2")
	byOwner.Signer = providerAcct
	e.requireOK(e.accept(e.fresh(), byOwner), "accept sent by p1's account")
	copied = proto.Clone(byOwner).(*leasesv1.MsgAcceptLease)
	copied.Signer = provider2Acct
	e.requireReject(e.accept(e.fresh(), copied), msgkit.Forbidden, "p1's acceptance replayed by p2's account")
	e.requireIdempotent(e.accept(e.fresh(), byOwner), "p1's own replay")

	// A fresh acceptance by p2's account of a p1 offer is FORBIDDEN too
	// (row 8), so the replay rule matches it.
	e.mustOffer(e.offerMsg("o3", "c3", "p1"))
	fresh := e.acceptMsg("o3", "l3")
	fresh.Signer = provider2Acct
	e.requireReject(e.accept(e.fresh(), fresh), msgkit.Forbidden, "fresh acceptance by p2's account")
	e.requireInvariants(nil)
}
