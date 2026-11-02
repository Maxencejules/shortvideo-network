package leases

import (
	"errors"
	"strconv"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Provider inner-signature payloads (interim choice, decision request
// DR-08-04; no proto field or API of protocol v0 defines them). The signed
// bytes are UTF-8(domain) || 0x00 || UTF-8(SVN-JCS-1 canonical JSON)
// (msgkit.SignBytes). Keys are lowerCamelCase. Every integer is a decimal
// string (a negative int64 keeps its "-"), so building a payload never
// fails on a value; an out-of-range value still yields a payload, its
// signature can verify, and the value is then rejected by its own range
// check. Digests are {"algorithm": <enum name>, "value": <lowercase hex>};
// the scheduler account is lowercase hex. There is no chain id (consistent
// with goal 07, DR-07-01).
//
//	SVN-PROVIDER-ACCEPT-1  {"contentId","contentVersion","deadlineUnixSeconds","expiryUnixSeconds","leaseId",
//	                        "manifestDigest","offerId","packageBytes","providerId","scheduler",
//	                        "startUnixSeconds","termVersion","termsDigest"}
//	                       built from the STORED offer and the message's lease id
//	SVN-PROVIDER-RENEW-1   {"deadlineUnixSeconds","evidenceId","leaseId","manifestDigest","packageBytes","termVersion"}
//	                       built from the message; evidenceId is "" when absent
//	SVN-PROVIDER-END-1     {"leaseId","providerId"}
//
// The acceptance binds the provider's consent to the exact content version,
// manifest digest, package bytes, terms digest, term version, real-time
// start and deadline, offer expiry, scheduler, and lease id. Lease ids are
// never reused, so an end signature cannot end another lease, and a second
// end of the same lease is UNDERFLOW.

func decInt(v int64) string { return strconv.FormatInt(v, 10) }

// acceptCanonical is the SVN-JCS-1 text of an acceptance payload.
func acceptCanonical(offer *leasesv1.Offer, leaseID string) (string, error) {
	if offer == nil {
		return "", errors.New("leases: nil offer")
	}
	return msgkit.Canonical(map[string]any{
		"contentId":           offer.GetContentId(),
		"contentVersion":      msgkit.Dec(offer.GetContentVersion()),
		"deadlineUnixSeconds": decInt(offer.GetDeadlineUnixSeconds()),
		"expiryUnixSeconds":   decInt(offer.GetExpiryUnixSeconds()),
		"leaseId":             leaseID,
		"manifestDigest":      msgkit.DigestObject(offer.GetManifestDigest()),
		"offerId":             offer.GetOfferId(),
		"packageBytes":        msgkit.Dec(offer.GetPackageBytes()),
		"providerId":          offer.GetProviderId(),
		"scheduler":           msgkit.Hex(offer.GetScheduler()),
		"startUnixSeconds":    decInt(offer.GetStartUnixSeconds()),
		"termVersion":         msgkit.Dec(offer.GetTermVersion()),
		"termsDigest":         msgkit.DigestObject(offer.GetTermsDigest()),
	})
}

// renewCanonical is the SVN-JCS-1 text of a renewal payload.
func renewCanonical(leaseID string, termVersion uint64, deadline int64, manifest *typesv1.Digest, packageBytes uint64, evidenceID string) (string, error) {
	return msgkit.Canonical(map[string]any{
		"deadlineUnixSeconds": decInt(deadline),
		"evidenceId":          evidenceID,
		"leaseId":             leaseID,
		"manifestDigest":      msgkit.DigestObject(manifest),
		"packageBytes":        msgkit.Dec(packageBytes),
		"termVersion":         msgkit.Dec(termVersion),
	})
}

// endCanonical is the SVN-JCS-1 text of an end payload.
func endCanonical(leaseID, providerID string) (string, error) {
	return msgkit.Canonical(map[string]any{
		"leaseId":    leaseID,
		"providerId": providerID,
	})
}

// AcceptSignBytes returns the bytes the provider's current key signs for
// MsgAcceptLease: the SVN-PROVIDER-ACCEPT-1 domain, 0x00, and the canonical
// acceptance payload of the stored offer and leaseID. Schedulers, provider
// agents, and tests use it. A nil offer, a string that is not UTF-8, or a
// payload above the SVN-JCS-1 size cap is an error.
func AcceptSignBytes(offer *leasesv1.Offer, leaseID string) ([]byte, error) {
	canonical, err := acceptCanonical(offer, leaseID)
	if err != nil {
		return nil, err
	}
	return msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, canonical), nil
}

// RenewSignBytes returns the bytes the provider's current key signs for
// MsgRenewLease: the SVN-PROVIDER-RENEW-1 domain, 0x00, and the canonical
// renewal payload of the message fields. evidenceID is "" when the message
// names no evidence.
func RenewSignBytes(leaseID string, termVersion uint64, deadline int64, manifest *typesv1.Digest, packageBytes uint64, evidenceID string) ([]byte, error) {
	canonical, err := renewCanonical(leaseID, termVersion, deadline, manifest, packageBytes, evidenceID)
	if err != nil {
		return nil, err
	}
	return msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, canonical), nil
}

// EndSignBytes returns the bytes the provider's current key signs for a
// provider-signed MsgEndLease: the SVN-PROVIDER-END-1 domain, 0x00, and the
// canonical end payload.
func EndSignBytes(leaseID, providerID string) ([]byte, error) {
	canonical, err := endCanonical(leaseID, providerID)
	if err != nil {
		return nil, err
	}
	return msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, canonical), nil
}
