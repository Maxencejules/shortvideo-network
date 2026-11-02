package app_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	sizeSmall = uint64(4_000_000)
	// capacitySmall holds one 9 MB package but not a second package.
	capacitySmall = uint64(10_000_000)
)

// leaseRejectState is the committed starting state of the lease reject
// test.
type leaseRejectState struct {
	setupNow int64
	// Open offers by role.
	open, pairB, capacity, pastDeadline, drained *leasesv1.MsgOfferLease
	// accepted is o1, accepted as l1 by the broadcaster under "ac-l1".
	accepted *leasesv1.MsgOfferLease
}

// leaseRejectSetup commits:
//
//   - p1 ADMITTED (1 GiB), p2 DRAINING, p3 APPLIED, p4 ADMITTED (10 MB)
//   - c1 c2 c3 c6 c7 c8 cr cw (4 MB), c5 (9 MB) PENDING; c9 and cw WITHDRAWN
//   - l1 (c1 on p1) and lw (cw on p1) held, l2 (c2 on p1) held, l3 (c3 on
//     p1) ended, l4 (c5 on p4, 9 MB of 10 MB) held, lr (cr on p1) held and
//     renewed to term 2
//   - open offers: o7 (c7 on p1), o2b (c2 on p1, its pair held by l2),
//     ocap (c1 on p4, 4 MB, does not fit next to l4), od3 (c6 on p1,
//     deadline setupNow+5), od (c8 on p2, accepted before the drain)
//
// The header time then moves 10 s past setupNow, so od3's deadline has
// passed while every other offer and lease is far from due.
func leaseRejectSetup(t *testing.T, n *regNet, c *cluster) leaseRejectState {
	t.Helper()
	var st leaseRejectState
	n.registerAdmit(t, c, n.provider1, "p1", capacity1)
	n.registerAdmit(t, c, n.provider2, "p2", capacity1)
	n.registerAdmit(t, c, n.provider2, "p4", capacitySmall)
	wantCode(t, "register p3", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p3", "p3", groupDevnet, capacity1)), msgkit.OK)
	for _, id := range []string{"c1", "c2", "c3", "c6", "c7", "c8", "c9", "cr", "cw"} {
		wantCode(t, "register "+id, n.send(t, c, n.service, n.registerContent(t, n.service, "reg-"+id, id, 1, packageManifest(id), sizeSmall)), msgkit.OK)
	}
	wantCode(t, "register c5", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c5", "c5", 1, packageManifest("c5"), sizeC2)), msgkit.OK)
	wantCode(t, "withdraw c9", n.send(t, c, n.service, n.withdrawContent(t, n.service, "wd-c9", "c9", 1, reasonDigest("c9"))), msgkit.OK)

	now := c.now()
	st.setupNow = now
	deadline, expiry := now+1800, now+600
	offerAccept := func(offerID, contentID, providerID string, size uint64, leaseID string) *leasesv1.MsgOfferLease {
		t.Helper()
		offer := n.offerMsg(n.service, "of-"+offerID, offerID, contentID, providerID, size, deadline, expiry)
		wantCode(t, "offer "+offerID, n.send(t, c, n.service, offer), msgkit.OK)
		if leaseID != "" {
			wantCode(t, "accept "+leaseID, n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-"+leaseID, offer, leaseID)), msgkit.OK)
		}
		return offer
	}
	st.accepted = offerAccept("o1", "c1", "p1", sizeSmall, "l1")
	offerAccept("or", "cr", "p1", sizeSmall, "lr")
	wantCode(t, "renew lr", n.send(t, c, n.broadcaster, n.renewMsg(t, n.broadcaster, "rn-lr", "lr", "p1", "cr", sizeSmall, 2, deadline+leaseTerm)), msgkit.OK)
	// Two open offers for one pair; the first acceptance holds the pair.
	o2 := offerAccept("o2", "c2", "p1", sizeSmall, "")
	st.pairB = offerAccept("o2b", "c2", "p1", sizeSmall, "")
	wantCode(t, "accept l2", n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l2", o2, "l2")), msgkit.OK)
	offerAccept("o3", "c3", "p1", sizeSmall, "l3")
	offerAccept("ow", "cw", "p1", sizeSmall, "lw")
	// Both p4 offers fit alone; once l4 holds 9 MB the 4 MB one does not.
	o4 := offerAccept("o4", "c5", "p4", sizeC2, "")
	st.capacity = offerAccept("ocap", "c1", "p4", sizeSmall, "")
	wantCode(t, "accept l4", n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l4", o4, "l4")), msgkit.OK)
	wantCode(t, "authority end l3", n.send(t, c, n.authority, n.authorityEndMsg("en-l3", "l3")), msgkit.OK)
	wantCode(t, "withdraw cw", n.send(t, c, n.service, n.withdrawContent(t, n.service, "wd-cw", "cw", 1, reasonDigest("cw"))), msgkit.OK)
	st.open = offerAccept("o7", "c7", "p1", sizeSmall, "")
	st.drained = offerAccept("od", "c8", "p2", sizeSmall, "")
	st.pastDeadline = n.offerMsg(n.service, "of-od3", "od3", "c6", "p1", sizeSmall, now+5, expiry)
	wantCode(t, "offer od3", n.send(t, c, n.service, st.pastDeadline), msgkit.OK)
	wantCode(t, "drain p2", n.send(t, c, n.provider2, n.drain(t, n.provider2, "drain-p2", "p2", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)), msgkit.OK)
	c.setTime(c.nodes[0].when.Add(10 * time.Second))
	c.checkInvariants(t, "reject setup")
	return st
}

// TestRejectedLeaseMessagesLeaveTheFullState: through FinalizeBlock, at
// least one case for every row of the four check tables of
// chain/x/leases/README.md ("Messages: check order and negative cases")
// that a committed transaction can reach in this app, in check order,
// except the rows listed below. Each case asserts its exact svn code, that
// the ante handler committed (the handler ran), that the full non-account
// state is byte-identical, and that no lease event was emitted. Ante
// rejections leave the account store unchanged too.
//
// Rows no case here reaches, and where they are tested:
//   - Enforce (gas limit, transaction size, signer size): the ante handler
//     applies the same bounds first (the ante cases below); the keeper
//     ladders reach the handler rows.
//   - signer without a role (UNAUTHORIZED, and FORBIDDEN for a provider
//     end): the ante admits only submitter rows and council seats, and it
//     refuses a lease message from a seat (DR-06-06,
//     TestSeatKeysSignOnlyCouncilMessages); keeper ladders role-none.
//   - offer DECISION_BLOCKED: this chain has the DEC-005 devnet number
//     (TestDevnetTermDecisionBlocked).
//   - offer and accept OVERFLOW: need reserved bytes near 2^64 (keeper
//     ladders reserve-overflow, the differential near-max family).
//   - offer D-6 and D-10: TestWithdrawalAtCapGasInApp.
//   - accept WITHDRAWN: withdrawal voids the content's open offers first
//     (keeper ladder content-withdrawn, with fakes).
//   - accept EXPIRED of an unswept expired offer and renew D-2 EXPIRED:
//     need an expiry backlog (TestLargeExpiringCatalogue).
//   - renew evidence EXPIRED and FORBIDDEN: Evidence is nil until Goal 09,
//     so every evidence_id is NOT_FOUND (keeper TestEvidenceCannotExtendLease).
//   - end Release UNDERFLOW: needs reserved bytes below a held lease's
//     bytes, an invariant violation no transaction produces (keeper ladder
//     release-underflow; TestExpiryInvariantViolationHalts for expiry).
func TestRejectedLeaseMessagesLeaveTheFullState(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	st := leaseRejectSetup(t, n, c)
	chainApp := c.app()
	now := c.now()
	d1 := st.setupNow + 1800
	serviceKey := signingKey{id: n.signing["p1"].id, priv: n.intent.priv}

	baseOffer := func(bid, offerID string) *leasesv1.MsgOfferLease {
		return n.offerMsg(n.service, bid, offerID, "c7", "p1", sizeSmall, now+1800, now+600)
	}
	baseAccept := func(who *actor, bid string) *leasesv1.MsgAcceptLease {
		return n.acceptMsg(t, who, bid, st.open, "l-new")
	}
	baseRenew := func(who *actor, bid string) *leasesv1.MsgRenewLease {
		return n.renewMsg(t, who, bid, "l1", "p1", "c1", sizeSmall, 2, d1+leaseTerm)
	}
	baseEnd := func(who *actor, bid string) *leasesv1.MsgEndLease {
		return n.endMsg(t, who, bid, "l1", "p1", 1)
	}
	// The base messages are valid: a dry run on a cached branch of the
	// committed state (Simulate) must succeed for each.
	for _, base := range []struct {
		who *actor
		msg sdk.Msg
	}{
		{n.service, baseOffer("dry-o", "o-dry")},
		{n.broadcaster, baseAccept(n.broadcaster, "dry-a")},
		{n.broadcaster, baseRenew(n.broadcaster, "dry-r")},
		{n.broadcaster, baseEnd(n.broadcaster, "dry-e")},
	} {
		if _, _, err := chainApp.Simulate(n.signed(t, c, base.who, registryGas, nil, base.msg)); err != nil {
			t.Fatalf("base %T does not succeed: %v", base.msg, err)
		}
	}
	renewSig := func(leaseID string, termVersion uint64, deadline int64, manifest *typesv1.Digest, size uint64, evidenceID string, domain typesv1.SignatureDomain, key signingKey) *typesv1.InnerSignature {
		signBytes, err := leases.RenewSignBytes(leaseID, termVersion, deadline, manifest, size, evidenceID)
		if err != nil {
			t.Fatal(err)
		}
		if domain != domainRenew {
			name, _ := msgkit.DomainString(domain)
			signBytes = append([]byte(name), signBytes[bytes.IndexByte(signBytes, 0):]...)
		}
		return innerSig(domain, key, signBytes)
	}
	endSig := func(leaseID, providerID string, termVersion uint64, key signingKey) *typesv1.InnerSignature {
		signBytes, err := leases.EndSignBytes(leaseID, providerID, termVersion)
		if err != nil {
			t.Fatal(err)
		}
		return innerSig(domainEnd, key, signBytes)
	}

	type rejectCase struct {
		name string
		who  *actor
		msg  func() sdk.Msg
		want msgkit.Code
	}
	cases := []rejectCase{
		// OfferLease, in op_offer order (chain/x/leases/README.md).
		{"offer: empty business_id", n.service, func() sdk.Msg { return baseOffer("", "o-x1") }, msgkit.Malformed},
		{"offer: nil terms_digest", n.service, func() sdk.Msg { m := baseOffer("x-of-2", "o-x2"); m.TermsDigest = nil; return m }, msgkit.Malformed},
		{"offer: provider account as scheduler", n.provider1, func() sdk.Msg { m := baseOffer("x-of-3", "o-x3"); m.Signer = n.provider1.addr; return m }, msgkit.Forbidden},
		{"offer: offer_id outside the id pattern", n.service, func() sdk.Msg { return baseOffer("x-of-4", "O-X4") }, msgkit.Bounds},
		{"offer: 65-byte content_id", n.service, func() sdk.Msg { m := baseOffer("x-of-5", "o-x5"); m.ContentId = strings.Repeat("c", 65); return m }, msgkit.Bounds},
		{"offer: manifest digest algorithm 0", n.service, func() sdk.Msg {
			m := baseOffer("x-of-6", "o-x6")
			m.ManifestDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
			return m
		}, msgkit.Malformed},
		{"offer: 31-byte terms digest", n.service, func() sdk.Msg {
			m := baseOffer("x-of-7", "o-x7")
			m.TermsDigest.Value = m.TermsDigest.Value[:31]
			return m
		}, msgkit.Bounds},
		{"offer: start 0", n.service, func() sdk.Msg { m := baseOffer("x-of-8", "o-x8"); m.StartUnixSeconds = 0; return m }, msgkit.Bounds},
		{"offer: deadline after 9999-12-31", n.service, func() sdk.Msg { m := baseOffer("x-of-9", "o-x9"); m.DeadlineUnixSeconds = maxUnix + 1; return m }, msgkit.Bounds},
		{"offer: package_bytes 0", n.service, func() sdk.Msg { m := baseOffer("x-of-10", "o-x10"); m.PackageBytes = 0; return m }, msgkit.Bounds},
		{"offer: package_bytes above the cap", n.service, func() sdk.Msg {
			m := baseOffer("x-of-11", "o-x11")
			m.PackageBytes = genesis.MaxPackageBytes + 1
			return m
		}, msgkit.Bounds},
		{"offer: business_id outside the id pattern", n.service, func() sdk.Msg { return baseOffer("X-OF-BP", "o-x-bp") }, msgkit.Bounds},
		{"offer: business id of a provider registration", n.service, func() sdk.Msg { return baseOffer("reg-p1", "o-x12") }, msgkit.Replay},
		{"offer: copy of another scheduler's committed offer (D-11)", n.service2, func() sdk.Msg {
			m := proto.Clone(st.open).(*leasesv1.MsgOfferLease)
			m.Signer = n.service2.addr
			return m
		}, msgkit.Forbidden},
		{"offer: content_version above 2^53-1", n.service, func() sdk.Msg { m := baseOffer("x-of-13", "o-x13"); m.ContentVersion = 1 << 60; return m }, msgkit.Malformed},
		{"offer: id of an open offer", n.service, func() sdk.Msg { return baseOffer("x-of-14", "o7") }, msgkit.DuplicateID},
		{"offer: unknown content", n.service, func() sdk.Msg {
			m := baseOffer("x-of-15", "o-x15")
			m.ContentId = "c404"
			m.ManifestDigest = contentManifest("c404")
			return m
		}, msgkit.NotFound},
		{"offer: withdrawn content", n.service, func() sdk.Msg {
			m := baseOffer("x-of-16", "o-x16")
			m.ContentId = "c9"
			m.ManifestDigest = contentManifest("c9")
			return m
		}, msgkit.Withdrawn},
		{"offer: content version 2", n.service, func() sdk.Msg { m := baseOffer("x-of-17", "o-x17"); m.ContentVersion = 2; return m }, msgkit.Conflict},
		{"offer: package bytes differ from the content record", n.service, func() sdk.Msg { m := baseOffer("x-of-18", "o-x18"); m.PackageBytes = sizeSmall + 1; return m }, msgkit.Integrity},
		{"offer: manifest digest differs from the content record", n.service, func() sdk.Msg {
			m := baseOffer("x-of-19", "o-x19")
			m.ManifestDigest = contentManifest("c1")
			return m
		}, msgkit.Integrity},
		{"offer: unknown provider", n.service, func() sdk.Msg { m := baseOffer("x-of-20", "o-x20"); m.ProviderId = "p404"; return m }, msgkit.NotFound},
		{"offer: provider not admitted", n.service, func() sdk.Msg { m := baseOffer("x-of-21", "o-x21"); m.ProviderId = "p3"; return m }, msgkit.Forbidden},
		{"offer: draining provider", n.service, func() sdk.Msg { m := baseOffer("x-of-21b", "o-x21b"); m.ProviderId = "p2"; return m }, msgkit.Forbidden},
		{"offer: expiry at the committed time", n.service, func() sdk.Msg { m := baseOffer("x-of-22", "o-x22"); m.ExpiryUnixSeconds = now; return m }, msgkit.Expired},
		{"offer: deadline equal to start", n.service, func() sdk.Msg {
			m := baseOffer("x-of-23", "o-x23")
			m.StartUnixSeconds = m.DeadlineUnixSeconds
			return m
		}, msgkit.OutOfRange},
		{"offer: term is not the DEC-005 number", n.service, func() sdk.Msg { m := baseOffer("x-of-24", "o-x24"); m.StartUnixSeconds--; return m }, msgkit.OutOfRange},
		{"offer: term_version 2", n.service, func() sdk.Msg { m := baseOffer("x-of-25", "o-x25"); m.TermVersion = 2; return m }, msgkit.StaleVersion},
		{"offer: pair held by a lease", n.service, func() sdk.Msg {
			m := baseOffer("x-of-26", "o-x26")
			m.ContentId = "c1"
			m.ManifestDigest = contentManifest("c1")
			return m
		}, msgkit.DuplicateID},
		{"offer: capacity", n.service, func() sdk.Msg {
			// c7 is 4 MB; p4 holds 9 MB of 10 MB.
			m := baseOffer("x-of-28", "o-x28")
			m.ProviderId = "p4"
			return m
		}, msgkit.Capacity},
		{"offer: id of an accepted offer (D-4)", n.service, func() sdk.Msg { return baseOffer("x-of-29", "o1") }, msgkit.DuplicateID},
		{"offer: lifetime above one day (D-5)", n.service, func() sdk.Msg {
			m := baseOffer("x-of-30", "o-x30")
			m.ExpiryUnixSeconds = now + leases.MaxOfferLifetimeSeconds + 1
			return m
		}, msgkit.Bounds},

		// AcceptLease, in op_accept order.
		{"accept: nil provider_acceptance", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-1")
			m.ProviderAcceptance = nil
			return m
		}, msgkit.Malformed},
		{"accept: service account as relay", n.service, func() sdk.Msg { return baseAccept(n.service, "x-ac-2") }, msgkit.Forbidden},
		{"accept: lease_id outside the id pattern", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-3")
			m.LeaseId = "L-NEW"
			return m
		}, msgkit.Bounds},
		{"accept: signature domain unspecified", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-3b")
			m.ProviderAcceptance.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_UNSPECIFIED
			return m
		}, msgkit.Malformed},
		{"accept: signature in the renew domain", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-4")
			m.ProviderAcceptance.Domain = domainRenew
			return m
		}, msgkit.WrongDomain},
		{"accept: canonical profile version 2", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-5")
			m.ProviderAcceptance.CanonicalProfileVersion = 2
			return m
		}, msgkit.OutOfRange},
		{"accept: empty key_id", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-5b")
			m.ProviderAcceptance.KeyId = ""
			return m
		}, msgkit.Malformed},
		{"accept: 31-byte public key", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-6")
			m.ProviderAcceptance.PublicKey = m.ProviderAcceptance.PublicKey[:31]
			return m
		}, msgkit.Bounds},
		{"accept: business_id outside the id pattern", n.broadcaster, func() sdk.Msg { return baseAccept(n.broadcaster, "X-AC-BP") }, msgkit.Bounds},
		{"accept: business id of a content registration", n.broadcaster, func() sdk.Msg { return baseAccept(n.broadcaster, "reg-c1") }, msgkit.Replay},
		{"accept: copy of the broadcaster's committed acceptance of a p1 offer, sent by p2's account (D-13)", n.provider2, func() sdk.Msg {
			return n.acceptMsg(t, n.provider2, "ac-l1", st.accepted, "l1")
		}, msgkit.Forbidden},
		{"accept: unknown offer", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-8")
			m.OfferId = "o404"
			return m
		}, msgkit.NotFound},
		{"accept: service key under the provider's key id (REQ-CHAIN-012)", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-9")
			m.ProviderAcceptance = acceptanceBy(t, st.open, "l-new", serviceKey)
			return m
		}, msgkit.Unauthorized},
		{"accept: acceptance of another lease id", n.broadcaster, func() sdk.Msg {
			m := baseAccept(n.broadcaster, "x-ac-10")
			m.ProviderAcceptance = acceptanceBy(t, st.open, "l-other", n.signing["p1"])
			return m
		}, msgkit.Unauthorized},
		{"accept: another provider's account as relay", n.provider2, func() sdk.Msg { return baseAccept(n.provider2, "x-ac-11") }, msgkit.Forbidden},
		{"accept: lease id in use", n.broadcaster, func() sdk.Msg { return n.acceptMsg(t, n.broadcaster, "x-ac-12", st.open, "l3") }, msgkit.DuplicateID},
		{"accept: draining provider", n.broadcaster, func() sdk.Msg { return n.acceptMsg(t, n.broadcaster, "x-ac-13", st.drained, "l-new") }, msgkit.Forbidden},
		{"accept: pair held by a lease", n.broadcaster, func() sdk.Msg { return n.acceptMsg(t, n.broadcaster, "x-ac-14", st.pairB, "l-new") }, msgkit.DuplicateID},
		{"accept: capacity consumed by another lease", n.broadcaster, func() sdk.Msg { return n.acceptMsg(t, n.broadcaster, "x-ac-15", st.capacity, "l-new") }, msgkit.Capacity},
		{"accept: offer deadline passed (D-3)", n.broadcaster, func() sdk.Msg { return n.acceptMsg(t, n.broadcaster, "x-ac-16", st.pastDeadline, "l-new") }, msgkit.Expired},

		// RenewLease, in op_renew order.
		{"renew: nil manifest_digest", n.broadcaster, func() sdk.Msg { m := baseRenew(n.broadcaster, "x-rn-1"); m.ManifestDigest = nil; return m }, msgkit.Malformed},
		{"renew: unknown lease", n.broadcaster, func() sdk.Msg {
			m := baseRenew(n.broadcaster, "x-rn-2")
			m.LeaseId = "l404"
			return m
		}, msgkit.NotFound},
		{"renew: signature in the end domain", n.broadcaster, func() sdk.Msg {
			m := baseRenew(n.broadcaster, "x-rn-3")
			m.ProviderSignature = renewSig("l1", 2, d1+leaseTerm, contentManifest("c1"), sizeSmall, "", domainEnd, n.signing["p1"])
			return m
		}, msgkit.WrongDomain},
		{"renew: service key", n.broadcaster, func() sdk.Msg {
			m := baseRenew(n.broadcaster, "x-rn-4")
			m.ProviderSignature = renewSig("l1", 2, d1+leaseTerm, contentManifest("c1"), sizeSmall, "", domainRenew, serviceKey)
			return m
		}, msgkit.Unauthorized},
		{"renew: service account as relay", n.service, func() sdk.Msg { return baseRenew(n.service, "x-rn-5") }, msgkit.Forbidden},
		{"renew: another provider's account as relay", n.provider2, func() sdk.Msg { return baseRenew(n.provider2, "x-rn-6") }, msgkit.Forbidden},
		{"renew: 31-byte manifest digest", n.broadcaster, func() sdk.Msg {
			m := baseRenew(n.broadcaster, "x-rn-7")
			m.ManifestDigest = digestOf(m.ManifestDigest.Value[:31])
			m.ProviderSignature = renewSig("l1", 2, d1+leaseTerm, m.ManifestDigest, sizeSmall, "", domainRenew, n.signing["p1"])
			return m
		}, msgkit.Bounds},
		{"renew: business_id outside the id pattern", n.broadcaster, func() sdk.Msg { return baseRenew(n.broadcaster, "X-RN-BP") }, msgkit.Bounds},
		{"renew: business id of a lease offer", n.broadcaster, func() sdk.Msg { return baseRenew(n.broadcaster, "of-o7") }, msgkit.Replay},
		{"renew: deadline above 2^53-1", n.broadcaster, func() sdk.Msg {
			m := baseRenew(n.broadcaster, "x-rn-9")
			m.DeadlineUnixSeconds = 1 << 60
			m.ProviderSignature = renewSig("l1", 2, 1<<60, contentManifest("c1"), sizeSmall, "", domainRenew, n.signing["p1"])
			return m
		}, msgkit.Malformed},
		{"renew: ended lease", n.broadcaster, func() sdk.Msg {
			return n.renewMsg(t, n.broadcaster, "x-rn-10", "l3", "p1", "c3", sizeSmall, 2, d1+leaseTerm)
		}, msgkit.Conflict},
		{"renew: withdrawn content", n.broadcaster, func() sdk.Msg {
			return n.renewMsg(t, n.broadcaster, "x-rn-11", "lw", "p1", "cw", sizeSmall, 2, d1+leaseTerm)
		}, msgkit.Withdrawn},
		{"renew: term_version 3", n.broadcaster, func() sdk.Msg {
			return n.renewMsg(t, n.broadcaster, "x-rn-12", "l1", "p1", "c1", sizeSmall, 3, d1+leaseTerm)
		}, msgkit.StaleVersion},
		{"renew: other package bytes", n.broadcaster, func() sdk.Msg {
			return n.renewMsg(t, n.broadcaster, "x-rn-13", "l1", "p1", "c1", sizeSmall+1, 2, d1+leaseTerm)
		}, msgkit.Conflict},
		{"renew: deadline after 9999-12-31", n.broadcaster, func() sdk.Msg {
			return n.renewMsg(t, n.broadcaster, "x-rn-14", "l1", "p1", "c1", sizeSmall, 2, maxUnix+1)
		}, msgkit.Bounds},
		{"renew: term is not the DEC-005 number", n.broadcaster, func() sdk.Msg {
			return n.renewMsg(t, n.broadcaster, "x-rn-15", "l1", "p1", "c1", sizeSmall, 2, d1+leaseTerm+1)
		}, msgkit.OutOfRange},
		{"renew: names evidence (no x/evidence before Goal 09)", n.broadcaster, func() sdk.Msg {
			m := baseRenew(n.broadcaster, "x-rn-16")
			m.EvidenceId = "ev-1"
			m.ProviderSignature = renewSig("l1", 2, d1+leaseTerm, contentManifest("c1"), sizeSmall, "ev-1", domainRenew, n.signing["p1"])
			return m
		}, msgkit.NotFound},

		// EndLease, in op_end order.
		{"end: empty lease_id", n.broadcaster, func() sdk.Msg { m := baseEnd(n.broadcaster, "x-en-1"); m.LeaseId = ""; return m }, msgkit.Malformed},
		{"end: unknown lease", n.broadcaster, func() sdk.Msg { return n.endMsg(t, n.broadcaster, "x-en-2", "l404", "p1", 1) }, msgkit.NotFound},
		{"end: authority_end from the service account", n.service, func() sdk.Msg {
			m := n.authorityEndMsg("x-en-3", "l1")
			m.Signer = n.service.addr
			return m
		}, msgkit.Forbidden},
		{"end: authority_end with a provider signature", n.authority, func() sdk.Msg {
			m := n.authorityEndMsg("x-en-4", "l1")
			m.ProviderSignature = endSig("l1", "p1", 1, n.signing["p1"])
			return m
		}, msgkit.UnknownField},
		{"end: no provider signature", n.broadcaster, func() sdk.Msg { m := baseEnd(n.broadcaster, "x-en-5"); m.ProviderSignature = nil; return m }, msgkit.Malformed},
		{"end: signature in the renew domain", n.broadcaster, func() sdk.Msg {
			m := baseEnd(n.broadcaster, "x-en-5b")
			m.ProviderSignature.Domain = domainRenew
			return m
		}, msgkit.WrongDomain},
		{"end: service key", n.broadcaster, func() sdk.Msg {
			m := baseEnd(n.broadcaster, "x-en-6")
			m.ProviderSignature = endSig("l1", "p1", 1, serviceKey)
			return m
		}, msgkit.Unauthorized},
		{"end: signed for term 1, relayed after the renewal to term 2 (D-12)", n.broadcaster, func() sdk.Msg {
			return n.endMsg(t, n.broadcaster, "x-en-6b", "lr", "p1", 1)
		}, msgkit.Unauthorized},
		{"end: service account as relay", n.service, func() sdk.Msg { return baseEnd(n.service, "x-en-7") }, msgkit.Forbidden},
		{"end: another provider's account as relay", n.provider2, func() sdk.Msg { return baseEnd(n.provider2, "x-en-8") }, msgkit.Forbidden},
		{"end: business_id outside the id pattern", n.broadcaster, func() sdk.Msg { return baseEnd(n.broadcaster, "X-EN-BP") }, msgkit.Bounds},
		{"end: business id of a lease acceptance", n.broadcaster, func() sdk.Msg { return baseEnd(n.broadcaster, "ac-l1") }, msgkit.Replay},
		{"end: ended lease, new business id", n.broadcaster, func() sdk.Msg { return n.endMsg(t, n.broadcaster, "x-en-10", "l3", "p1", 1) }, msgkit.Underflow},
	}
	run := func(tc rejectCase) {
		t.Helper()
		before := chainApp.FullSnapshot()
		res := n.send(t, c, tc.who, tc.msg())
		wantCode(t, tc.name, res, tc.want)
		if !anteCommitted(res) {
			t.Fatalf("%s: rejected in the ante handler, not by x/leases", tc.name)
		}
		unchanged(t, tc.name, chainApp, before, res)
		wantTxEvents(t, tc.name, res)
	}
	for _, tc := range cases {
		run(tc)
	}

	// The provider open-offer cap: a council GovernedLimitChange sets
	// max_open_offers to the number of p1's open offers (o7, o2b, od3).
	n.council(t, c, "prop-open-3", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 3))
	run(rejectCase{"offer: provider open-offer cap", n.service, func() sdk.Msg { return baseOffer("x-of-31", "o-x31") }, msgkit.Bounds})

	// Ante rejections: nothing reaches a handler, and the account store
	// does not change either.
	ante := []struct {
		name   string
		raw    func() []byte
		svn    msgkit.Code
		sdkErr *errorsmod.Error
	}{
		{"outsider", func() []byte {
			outsider := newSecp("outsider")
			msg := baseOffer("x-out", "o-out")
			msg.Signer = outsider.addr
			return signFull(t, chainApp, n.doc.ChainID, outsider.key, 0, 0, registryGas, nil, msg)
		}, "", sdkerrors.ErrUnknownAddress},
		{"fee", func() []byte {
			return n.signed(t, c, n.service, registryGas, sdk.NewCoins(sdk.NewInt64Coin("reject", 1)), baseOffer("x-fee", "o-fee"))
		}, msgkit.FeeRejected, nil},
		{"gas limit 0", func() []byte { return n.signed(t, c, n.service, 0, nil, baseOffer("x-gas", "o-gas")) }, msgkit.Bounds, nil},
		{"70 KiB transaction", func() []byte {
			return n.signed(t, c, n.service, registryGas, nil, baseOffer(strings.Repeat("b", 70*1024), "o-big"))
		}, msgkit.Bounds, nil},
	}
	for _, tc := range ante {
		before := chainApp.FullSnapshot()
		accBefore := chainApp.ModuleSnapshot("acc")
		res := n.deliverRaw(t, c, n.service, tc.raw())
		if res.Code == 0 || anteCommitted(res) {
			t.Fatalf("%s: code %d ante committed %t", tc.name, res.Code, anteCommitted(res))
		}
		if tc.sdkErr != nil {
			if res.Codespace != sdkerrors.RootCodespace || res.Code != tc.sdkErr.ABCICode() {
				t.Fatalf("%s: got %s/%d want sdk/%d (%s)", tc.name, res.Codespace, res.Code, tc.sdkErr.ABCICode(), tc.sdkErr)
			}
		} else {
			wantCode(t, tc.name, res, tc.svn)
		}
		unchanged(t, tc.name, chainApp, before, res)
		if !bytes.Equal(accBefore, chainApp.ModuleSnapshot("acc")) {
			t.Fatalf("%s: account store changed", tc.name)
		}
	}
	c.checkInvariants(t, "after every rejection")
}
