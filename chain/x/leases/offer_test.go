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
)

var drainAcct = bytes.Repeat([]byte{0x88}, 20)

type offerFault = fault[*leasesv1.MsgOfferLease]

func offerEdit(name string, want msgkit.Code, edit func(m *leasesv1.MsgOfferLease)) offerFault {
	return offerFault{name: name, want: want, apply: func(_ *env, c *call[*leasesv1.MsgOfferLease]) { edit(c.msg) }}
}

func offerEnv(name string, want msgkit.Code, edit func(e *env)) offerFault {
	return offerFault{name: name, want: want, apply: func(e *env, _ *call[*leasesv1.MsgOfferLease]) { edit(e) }}
}

// offerLadderSetup: withdrawn content c-w, draining provider p-drain, an
// open offer o-open (c2 -> p1), and an ended lease whose offer id o-used
// is spent.
func offerLadderSetup(e *env) {
	e.putContent("c-w", contentv1.ContentState_CONTENT_STATE_WITHDRAWN, packageSize)
	e.putProvider("p-drain", drainAcct, providersv1.ProviderState_PROVIDER_STATE_DRAINING, 1<<30, 0)
	e.seedOffer(e.offerRecord("o-open", "c2", "p1"))
	ended := e.leaseRecord("l-used", "c3", "p2")
	ended.OfferId = "o-used"
	ended.State = leasesv1.LeaseState_LEASE_STATE_ENDED
	ended.ReservationHeld = false
	e.seedLease(ended)
}

// TestOfferCheckOrder is the OfferLease ladder: one row per check of
// op_offer (spec_machine.py:583), then the chain-only checks D-4, D-5, D-6.
func TestOfferCheckOrder(t *testing.T) {
	ladder[*leasesv1.MsgOfferLease]{
		setup: offerLadderSetup,
		base:  func(e *env) *leasesv1.MsgOfferLease { return e.offerMsg("o1", "c1", "p1") },
		exec:  func(e *env, ctx sdk.Context, m *leasesv1.MsgOfferLease) result { return e.offer(ctx, m) },
		faults: []offerFault{
			gasFault[*leasesv1.MsgOfferLease](),
			txBytesFault[*leasesv1.MsgOfferLease](),
			signerLenFault(func(m *leasesv1.MsgOfferLease, s []byte) { m.Signer = s }),
			offerEdit("presence-business-id", msgkit.Malformed, func(m *leasesv1.MsgOfferLease) { m.BusinessId = "" }),
			offerEdit("role-none", msgkit.Unauthorized, func(m *leasesv1.MsgOfferLease) { m.Signer = outsiderAcct }),
			offerEdit("role-broadcaster", msgkit.Forbidden, func(m *leasesv1.MsgOfferLease) { m.Signer = broadcasterAcct }),
			offerEdit("offer-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.OfferId = "O1" }),
			offerEdit("content-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.ContentId = "-c1" }),
			offerEdit("provider-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.ProviderId = "p1/x" }),
			offerEdit("manifest-algorithm-0", msgkit.Malformed, func(m *leasesv1.MsgOfferLease) { m.ManifestDigest.Algorithm = 0 }),
			offerEdit("manifest-31-bytes", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.ManifestDigest.Value = m.ManifestDigest.Value[:31] }),
			offerEdit("terms-algorithm-7", msgkit.Malformed, func(m *leasesv1.MsgOfferLease) { m.TermsDigest.Algorithm = 7 }),
			offerEdit("terms-33-bytes", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.TermsDigest.Value = append(m.TermsDigest.Value, 1) }),
			offerEdit("start-0", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.StartUnixSeconds = 0 }),
			offerEdit("deadline-above-9999", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.DeadlineUnixSeconds = 253402300800 }),
			offerEdit("expiry-negative", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.ExpiryUnixSeconds = -1 }),
			offerEdit("package-bytes-0", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.PackageBytes = 0 }),
			offerEnv("package-above-governed-limit", msgkit.Bounds, func(e *env) { e.auth.limits[msgkit.LimitMaxPackageBytes] = 998 }),
			offerEdit("gate-business-id-pattern", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) { m.BusinessId = "B1" }),
			offerEdit("gate-content-version-2^60", msgkit.Malformed, func(m *leasesv1.MsgOfferLease) { m.ContentVersion = 1 << 60 }),
			offerEdit("gate-term-version-2^60", msgkit.Malformed, func(m *leasesv1.MsgOfferLease) { m.TermVersion = 1 << 60 }),
			offerEdit("gate-term-version-max-uint64", msgkit.Malformed, func(m *leasesv1.MsgOfferLease) { m.TermVersion = math.MaxUint64 }),
			replayFault(func(m *leasesv1.MsgOfferLease) string { return m.GetBusinessId() }),
			offerEdit("open-offer-id", msgkit.DuplicateID, func(m *leasesv1.MsgOfferLease) { m.OfferId = "o-open" }),
			offerEdit("content-not-found", msgkit.NotFound, func(m *leasesv1.MsgOfferLease) { m.ContentId = "c-none" }),
			offerEdit("content-withdrawn", msgkit.Withdrawn, func(m *leasesv1.MsgOfferLease) { m.ContentId = "c-w" }),
			offerEdit("content-version-2", msgkit.Conflict, func(m *leasesv1.MsgOfferLease) { m.ContentVersion = 2 }),
			offerEdit("manifest-differs", msgkit.Integrity, func(m *leasesv1.MsgOfferLease) { m.ManifestDigest = manifestOf("c2") }),
			offerEdit("package-bytes-differ", msgkit.Integrity, func(m *leasesv1.MsgOfferLease) { m.PackageBytes = 999 }),
			offerEdit("provider-not-found", msgkit.NotFound, func(m *leasesv1.MsgOfferLease) { m.ProviderId = "p-none" }),
			offerEdit("provider-draining", msgkit.Forbidden, func(m *leasesv1.MsgOfferLease) { m.ProviderId = "p-drain" }),
			offerEdit("expiry-at-committed-time", msgkit.Expired, func(m *leasesv1.MsgOfferLease) { m.ExpiryUnixSeconds = t0 }),
			offerEdit("deadline-at-start", msgkit.OutOfRange, func(m *leasesv1.MsgOfferLease) { m.DeadlineUnixSeconds = m.StartUnixSeconds }),
			offerEnv("dec005-absent", msgkit.DecisionBlocked, func(e *env) { e.auth.termFound = false }),
			offerEnv("dec005-zero", msgkit.DecisionBlocked, func(e *env) { e.auth.term = 0 }),
			offerEdit("term-plus-one-second", msgkit.OutOfRange, func(m *leasesv1.MsgOfferLease) {
				m.DeadlineUnixSeconds = m.StartUnixSeconds + term + 1
			}),
			offerEdit("term-version-2", msgkit.StaleVersion, func(m *leasesv1.MsgOfferLease) { m.TermVersion = 2 }),
			offerEnv("live-pair", msgkit.DuplicateID, func(e *env) {
				// The pair rows only: reserved bytes stay as the later
				// accounting faults set them.
				e.k.SeedLeaseForTest(e.unmetered(), e.leaseRecord("l-live", "c1", "p1"))
			}),
			offerEnv("provider-open-offer-cap", msgkit.Bounds, func(e *env) { e.auth.limits[msgkit.LimitMaxOpenOffers] = 1 }),
			offerEnv("reserve-overflow", msgkit.Overflow, func(e *env) {
				e.editProvider("p1", func(p *providersv1.Provider) {
					p.ReservedBytes, p.AdmittedCapacityBytes = math.MaxUint64-10, math.MaxUint64
				})
			}),
			offerEnv("reserve-capacity", msgkit.Capacity, func(e *env) {
				e.editProvider("p1", func(p *providersv1.Provider) { p.AdmittedCapacityBytes = p.ReservedBytes + packageSize - 1 })
			}),
			offerEdit("d4-offer-id-used", msgkit.DuplicateID, func(m *leasesv1.MsgOfferLease) { m.OfferId = "o-used" }),
			offerEdit("d5-lifetime-86401", msgkit.Bounds, func(m *leasesv1.MsgOfferLease) {
				m.ExpiryUnixSeconds = t0 + leases.MaxOfferLifetimeSeconds + 1
			}),
			offerEnv("d6-content-open-offer-cap", msgkit.Bounds, func(e *env) {
				for i := 0; i < leases.MaxOpenOffersPerContent; i++ {
					e.seedOffer(e.offerRecord(fmt.Sprintf("o-cap-%03d", i), "c1", "p2"))
				}
			}),
		},
	}.run(t)
}

// TestOfferPresence: every required string, bytes, or message field that
// is empty or nil is MALFORMED before the role check.
func TestOfferPresence(t *testing.T) {
	cases := map[string]func(m *leasesv1.MsgOfferLease){
		"business-id":     func(m *leasesv1.MsgOfferLease) { m.BusinessId = "" },
		"offer-id":        func(m *leasesv1.MsgOfferLease) { m.OfferId = "" },
		"content-id":      func(m *leasesv1.MsgOfferLease) { m.ContentId = "" },
		"provider-id":     func(m *leasesv1.MsgOfferLease) { m.ProviderId = "" },
		"manifest-digest": func(m *leasesv1.MsgOfferLease) { m.ManifestDigest = nil },
		"terms-digest":    func(m *leasesv1.MsgOfferLease) { m.TermsDigest = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			msg := e.offerMsg("o1", "c1", "p1")
			edit(msg)
			msg.Signer = outsiderAcct // presence comes before the role check
			e.requireReject(e.offer(e.fresh(), msg), msgkit.Malformed, name)
		})
	}
}

// TestOfferWritesRecordAndEvent: a successful offer stores the offer with
// the scheduler, writes the expiry, content, used-id, and counter rows,
// reserves nothing, commits the business id, and emits LeaseOffered. A
// replay of the same payload is idempotent; another payload is REPLAY.
func TestOfferWritesRecordAndEvent(t *testing.T) {
	e := newEnv(t)
	msg := e.offerMsg("o1", "c1", "p1")
	r := e.offer(e.fresh(), msg)
	e.requireOK(r, "offer")
	stored := e.storedOffer("o1")
	if !proto.Equal(stored, e.offerRecord("o1", "c1", "p1")) {
		t.Fatalf("stored offer %v", stored)
	}
	if e.provider("p1").GetReservedBytes() != 0 {
		t.Fatal("an offer reserved bytes")
	}
	for _, key := range [][]byte{
		leases.OfferExpiryKey(msg.GetExpiryUnixSeconds(), "o1"), leases.ContentOfferKey("c1", "o1"), leases.UsedOfferKey("o1"),
	} {
		if !bytes.Equal(e.leaseStore().Get(key), leases.Marker) {
			t.Fatalf("row %x missing", key)
		}
	}
	for _, key := range [][]byte{leases.ProviderOfferCountKey("p1"), leases.ContentOfferCountKey("c1")} {
		if !bytes.Equal(e.leaseStore().Get(key), []byte{0, 0, 0, 0, 0, 0, 0, 1}) {
			t.Fatalf("counter %x = %x", key, e.leaseStore().Get(key))
		}
	}
	if len(r.events) != 1 {
		t.Fatalf("events %v", r.events)
	}
	parsed, err := msgkit.ParseTyped(r.events[0])
	if err != nil || !proto.Equal(parsed, &eventsv1.LeaseOffered{OfferId: "o1", ProviderId: "p1", ContentId: "c1"}) {
		t.Fatalf("event %v %v", parsed, err)
	}
	if _, found := e.k.BusinessDigest(e.unmetered(), "off-o1"); !found {
		t.Fatal("business id not committed")
	}
	e.requireIdempotent(e.offer(e.fresh(), msg), "replay")
	changed := proto.Clone(msg).(*leasesv1.MsgOfferLease)
	changed.TermsDigest = digestOf("other terms")
	e.requireReject(e.offer(e.fresh(), changed), msgkit.Replay, "changed payload")
	e.requireInvariants(nil)
}

// TestOfferIDNeverReusedD4: an offer id stays used after its offer was
// accepted, expired, or voided by a withdrawal. The spec machine would
// accept each reuse once the offer is gone; the chain returns DUPLICATE_ID
// after every spec check (deviation D-4).
func TestOfferIDNeverReusedD4(t *testing.T) {
	e := newEnv(t)
	e.mustLease("l1", "c1", "p1") // offer o-l1, accepted
	e.mustOffer(e.offerMsg("o-exp", "c2", "p1"))
	e.mustOffer(e.offerMsg("o-void", "c3", "p2"))
	if err := e.expire(e.at(t0 + 600)); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := e.k.OfferForTest(e.unmetered(), "o-exp"); found {
		t.Fatal("o-exp did not expire")
	}
	// o-void expired at the same time; void a fresh one by withdrawal.
	e.mustOffer(withExpiry(e.offerMsg("o-void2", "c3", "p2"), t0+700))
	e.setContentState("c3", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
	if err := e.k.ContentWithdrawn(e.fresh(), "c3", 1); err != nil {
		t.Fatal(err)
	}
	for _, reuse := range []string{"o-l1", "o-exp", "o-void2"} {
		msg := e.offerMsg(reuse, "c2", "p2")
		msg.BusinessId = "reuse-" + reuse
		e.requireReject(e.offer(e.fresh(), msg), msgkit.DuplicateID, reuse)
	}
	fresh := e.offerMsg("o-new", "c2", "p2")
	e.requireOK(e.offer(e.fresh(), fresh), "fresh id")
	e.requireInvariants(nil)
}

func withExpiry(m *leasesv1.MsgOfferLease, expiry int64) *leasesv1.MsgOfferLease {
	m.ExpiryUnixSeconds = expiry
	return m
}

// TestOfferLifetimeBoundD5: expiry - committed time may be at most
// MaxOfferLifetimeSeconds (86400, PROPOSED): 86400 is accepted, 86401 is
// BOUNDS after every spec check (deviation D-5).
func TestOfferLifetimeBoundD5(t *testing.T) {
	e := newEnv(t)
	ok := withExpiry(e.offerMsg("o1", "c1", "p1"), t0+leases.MaxOfferLifetimeSeconds)
	e.requireOK(e.offer(e.fresh(), ok), "86400")
	over := withExpiry(e.offerMsg("o2", "c2", "p1"), t0+leases.MaxOfferLifetimeSeconds+1)
	e.requireReject(e.offer(e.fresh(), over), msgkit.Bounds, "86401")
	// The bound is relative to committed time: one second later the same
	// expiry is within it.
	e.requireOK(e.offer(e.at(t0+1), over), "86401 one second later")
}

// TestOpenOffersPerContentCapD6: a content id holds at most
// MaxOpenOffersPerContent (256, PROPOSED) open offers. The 257th is BOUNDS;
// when one closes (accepted or expired) a new offer fits again: the cap
// counts open offers, it is not a lifetime cap.
func TestOpenOffersPerContentCapD6(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < leases.MaxOpenOffersPerContent; i++ {
		provider := []string{"p1", "p2"}[i%2]
		e.mustOffer(e.offerMsg(fmt.Sprintf("o%03d", i), "c1", provider))
	}
	// p2: accepting o000 below makes the pair (p1, c1) live.
	over := e.offerMsg("o-over", "c1", "p2")
	e.requireReject(e.offer(e.fresh(), over), msgkit.Bounds, "257th")
	// Another content id is unaffected.
	e.requireOK(e.offer(e.fresh(), e.offerMsg("o-c2", "c2", "p1")), "other content")
	// Accepting one offer frees one slot.
	e.requireOK(e.accept(e.fresh(), e.acceptMsg("o000", "l1")), "accept")
	e.requireOK(e.offer(e.fresh(), over), "after accept")
	over2 := e.offerMsg("o-over2", "c1", "p2")
	e.requireReject(e.offer(e.fresh(), over2), msgkit.Bounds, "full again")
	e.requireInvariants(nil)
}

// TestOfferProviderCapCountsOpenOffers: the per-provider cap
// (effective max_open_offers) counts open offers only; an offer that
// closes frees its slot.
func TestOfferProviderCapCountsOpenOffers(t *testing.T) {
	e := newEnv(t)
	e.auth.limits[msgkit.LimitMaxOpenOffers] = 2
	e.mustOffer(e.offerMsg("o1", "c1", "p1"))
	e.mustOffer(e.offerMsg("o2", "c2", "p1"))
	third := e.offerMsg("o3", "c3", "p1")
	e.requireReject(e.offer(e.fresh(), third), msgkit.Bounds, "third open offer")
	e.requireOK(e.offer(e.fresh(), e.offerMsg("o-p2", "c3", "p2")), "other provider")
	e.requireOK(e.accept(e.fresh(), e.acceptMsg("o1", "l1")), "accept frees a slot")
	e.requireOK(e.offer(e.fresh(), third), "after accept")
	e.requireInvariants(nil)
}
