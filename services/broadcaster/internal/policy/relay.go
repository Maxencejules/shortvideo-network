package policy

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Deadline page budget bounds (plan section 11, PROPOSED: the
// Query/Deadlines page budget, pages of genesis.MaxPageLimit).
const (
	MinDeadlinePages = 1
	MaxDeadlinePages = 50
)

// Reader is the chain state the provider pre-check reads (*chain.Client).
// Every read is answered at a height of at least minHeight or refused as
// stale by the client.
type Reader interface {
	Provider(ctx context.Context, providerID string, minHeight int64) (*providersv1.Provider, bool, int64, error)
	HeldLease(ctx context.Context, providerID, contentID string, minHeight int64) (*leasesv1.Lease, bool, int64, error)
	Deadlines(ctx context.Context, notAfter int64, pageKey []byte, limit uint32, minHeight int64) (*leasesv1.QueryDeadlinesResponse, int64, error)
}

// RelayInput is one relayed lease message to pre-check (plan 7.3 P1).
type RelayInput struct {
	// Msg is a *MsgAcceptLease, *MsgRenewLease or *MsgEndLease.
	Msg sdk.Msg
	// ProviderID and ContentID locate the held lease of a renew or end:
	// no query returns a lease by id (DR-08-08), so the caller names the
	// (provider, content) pair and the pre-check reads the pair's held
	// lease and requires its lease id. For an accept they are optional;
	// when set they must match the stored offer.
	ProviderID string
	ContentID  string
	// MinHeight is the lowest height a read may be answered at: the read
	// floor of the latest header (chain.Status.AppFloor).
	MinHeight int64
	// ChainTime is the time of the latest header the node reported
	// (chain.Status.Time, the chain's clock), for the offer expiry check
	// of an accept. It is not the time of the MinHeight block: any block
	// that can include the message is above that header, and CometBFT
	// block times strictly increase (cometbft@v0.40.0/state/validation.go:
	// 132), so it is a lower bound of the inclusion block's time and the
	// check refuses an offer the chain would find expired. The offer's
	// expiry is a stored field that the reads do not change.
	ChainTime time.Time
	// DeadlinePages is the Query/Deadlines page budget (MinDeadlinePages
	// to MaxDeadlinePages) for finding an accept's open offer.
	DeadlinePages int
}

// RelayResult names what a relayed message acts on, as read from the
// chain.
type RelayResult struct {
	ProviderID string
	// OfferID is the offer an accept takes, or the offer a renewed or
	// ended lease came from.
	OfferID       string
	LeaseID       string
	KeyGeneration uint64
	// Height is the lowest height the reads were answered at.
	Height int64
}

// PrecheckRelay is the provider signature pre-check of plan 7.3 P1, run
// before enqueue and again before signing (owner answer Q2 "Relay +
// verbatim forward (Recommended)"). It rebuilds the provider's signed
// payload with the chain's exported builders from committed state and
// verifies the inner signature with msgkit.VerifyInner against the
// provider's CURRENT key id and public key, so a signature by the service
// key, the broadcaster key, another provider's key, a retired key or any
// unregistered key is refused (PROVIDER_SIGNATURE_INVALID):
//
//   - accept: the open offer is found by paging Query/Deadlines within
//     the page budget (OFFER_NOT_FOUND when not found within it, never
//     skipped); the provider must be ADMITTED (the chain's own row 12);
//     the offer must not have expired at ChainTime; the payload is
//     leases.AcceptSignBytes(stored offer, lease id).
//   - renew: the pair's held lease must have the message's lease id; the
//     payload is leases.RenewSignBytes(message fields). The chain allows a
//     renewal in every provider state (DR-08-01), so no state is required.
//   - end: authority_end must be false; the pair's held lease must have
//     the message's lease id; the payload is leases.EndSignBytes(lease id,
//     the lease's provider id, its current term version). The chain allows
//     an end in every provider state.
//
// A read failure is returned as an error that is not a Refusal (retry
// later). The chain verifies the signature again when the message
// executes; this check only fails fast (docs/protocol.md:183).
func PrecheckRelay(ctx context.Context, r Reader, in RelayInput) (RelayResult, error) {
	if r == nil {
		return RelayResult{}, errors.New("policy: no chain reader")
	}
	if in.MinHeight < 0 {
		return RelayResult{}, errors.New("policy: negative minimum height")
	}
	switch m := in.Msg.(type) {
	case *leasesv1.MsgAcceptLease:
		return precheckAccept(ctx, r, m, in)
	case *leasesv1.MsgRenewLease:
		lease, res, err := heldLease(ctx, r, m.GetLeaseId(), in)
		if err != nil {
			return RelayResult{}, err
		}
		sb, err := leases.RenewSignBytes(m.GetLeaseId(), m.GetTermVersion(), m.GetDeadlineUnixSeconds(), m.GetManifestDigest(), m.GetPackageBytes(), m.GetEvidenceId())
		if err != nil {
			return RelayResult{}, refuse(CodeProviderSignature, "renew payload does not build")
		}
		return verifyProvider(ctx, r, lease.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, sb, m.GetProviderSignature(), false, res)
	case *leasesv1.MsgEndLease:
		if m.GetAuthorityEnd() {
			return RelayResult{}, refuse(CodeAuthorityEnd, "EndLease with authority_end is not relayed")
		}
		lease, res, err := heldLease(ctx, r, m.GetLeaseId(), in)
		if err != nil {
			return RelayResult{}, err
		}
		sb, err := leases.EndSignBytes(m.GetLeaseId(), lease.GetProviderId(), lease.GetTermVersion())
		if err != nil {
			return RelayResult{}, refuse(CodeProviderSignature, "end payload does not build")
		}
		return verifyProvider(ctx, r, lease.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, sb, m.GetProviderSignature(), false, res)
	}
	return RelayResult{}, refuse(CodeInvalid, "not a relayed lease message")
}

func precheckAccept(ctx context.Context, r Reader, m *leasesv1.MsgAcceptLease, in RelayInput) (RelayResult, error) {
	if !msgkit.ValidID(m.GetOfferId()) || !msgkit.ValidID(m.GetLeaseId()) {
		return RelayResult{}, refuse(CodeInvalid, "offer id or lease id outside the id pattern")
	}
	if in.DeadlinePages < MinDeadlinePages || in.DeadlinePages > MaxDeadlinePages {
		return RelayResult{}, fmt.Errorf("policy: deadline page budget must be %d to %d", MinDeadlinePages, MaxDeadlinePages)
	}
	offer, height, err := findOffer(ctx, r, m.GetOfferId(), in.MinHeight, in.DeadlinePages)
	if err != nil {
		return RelayResult{}, err
	}
	if (in.ProviderID != "" && in.ProviderID != offer.GetProviderId()) || (in.ContentID != "" && in.ContentID != offer.GetContentId()) {
		return RelayResult{}, refuse(CodeOfferNotFound, "the open offer is for another provider or content")
	}
	if in.ChainTime.IsZero() || in.ChainTime.Unix() >= offer.GetExpiryUnixSeconds() {
		return RelayResult{}, refuse(CodeOfferExpired, "the offer has expired at the latest committed block time")
	}
	sb, err := leases.AcceptSignBytes(offer, m.GetLeaseId())
	if err != nil {
		return RelayResult{}, refuse(CodeProviderSignature, "accept payload does not build")
	}
	res := RelayResult{OfferID: offer.GetOfferId(), LeaseID: m.GetLeaseId(), Height: height}
	return verifyProvider(ctx, r, offer.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, sb, m.GetProviderAcceptance(), true, res)
}

// findOffer pages Query/Deadlines (every open offer and held lease, by
// deadline) for the open offer offerID, reading at most pages pages, every
// page at least at the first page's height.
func findOffer(ctx context.Context, r Reader, offerID string, minHeight int64, pages int) (*leasesv1.Offer, int64, error) {
	var pageKey []byte
	first := int64(0)
	for page := 0; page < pages; page++ {
		resp, height, err := r.Deadlines(ctx, genesis.UnixSecondsMax, pageKey, genesis.MaxPageLimit, minHeight)
		if err != nil {
			return nil, 0, fmt.Errorf("policy: read deadlines: %w", err)
		}
		if page == 0 {
			first, minHeight = height, height
		}
		for _, o := range resp.GetOffers() {
			if o.GetOfferId() == offerID {
				return o, first, nil
			}
		}
		pageKey = resp.GetPage().GetNextPageKey()
		if len(pageKey) == 0 {
			return nil, 0, refuse(CodeOfferNotFound, "no open offer with this id")
		}
	}
	return nil, 0, refuse(CodeOfferNotFound, "no open offer with this id within the page budget of %d pages", pages)
}

// heldLease reads the held lease of the (provider, content) pair the
// caller named and requires the message's lease id.
func heldLease(ctx context.Context, r Reader, leaseID string, in RelayInput) (*leasesv1.Lease, RelayResult, error) {
	if !msgkit.ValidID(leaseID) || !msgkit.ValidID(in.ProviderID) || !msgkit.ValidID(in.ContentID) {
		return nil, RelayResult{}, refuse(CodeInvalid, "lease id, provider id or content id outside the id pattern")
	}
	lease, found, height, err := r.HeldLease(ctx, in.ProviderID, in.ContentID, in.MinHeight)
	if err != nil {
		return nil, RelayResult{}, fmt.Errorf("policy: read held lease: %w", err)
	}
	if !found || lease.GetLeaseId() != leaseID {
		return nil, RelayResult{}, refuse(CodeLeaseNotFound, "the pair holds no lease with this id")
	}
	return lease, RelayResult{OfferID: lease.GetOfferId(), LeaseID: leaseID, Height: height}, nil
}

// verifyProvider reads the provider and verifies sig over signBytes
// against its current key (msgkit.VerifyInner). requireAdmitted is the
// accept's provider-state rule.
func verifyProvider(ctx context.Context, r Reader, providerID string, domain typesv1.SignatureDomain, signBytes []byte, sig *typesv1.InnerSignature, requireAdmitted bool, res RelayResult) (RelayResult, error) {
	rec, found, height, err := r.Provider(ctx, providerID, res.Height)
	if err != nil {
		return RelayResult{}, fmt.Errorf("policy: read provider: %w", err)
	}
	if !found {
		return RelayResult{}, refuse(CodeProviderNotFound, "no provider record")
	}
	if requireAdmitted && rec.GetState() != providersv1.ProviderState_PROVIDER_STATE_ADMITTED {
		return RelayResult{}, refuse(CodeProviderNotAdmitted, "provider is %s", rec.GetState())
	}
	canonical, err := canonicalOf(domain, signBytes)
	if err != nil {
		return RelayResult{}, refuse(CodeProviderSignature, "%v", err)
	}
	if code := msgkit.VerifyInner(sig, domain, rec.GetSigningKeyId(), rec.GetSigningPubkey(), canonical); code != msgkit.OK {
		return RelayResult{}, refuse(CodeProviderSignature, "provider signature does not verify against the provider's current key: %s", code)
	}
	res.ProviderID = providerID
	res.KeyGeneration = rec.GetKeyGeneration()
	if height < res.Height || res.Height == 0 {
		res.Height = height
	}
	return res, nil
}
