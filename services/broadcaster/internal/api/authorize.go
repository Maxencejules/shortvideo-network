package api

import (
	"context"
	"errors"
	"net/http"

	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// authorized is what the checks of one request decided: the account whose
// sequence the transaction uses, the object, and the forward fields of a
// forwarded transaction.
type authorized struct {
	signer  []byte
	object  store.ObjectRef
	forward *store.ForwardFields
}

// authorize runs the enqueue-time checks of plan 6.3 for one decoded
// request, after the caller's kind and the context's permission were
// checked: the publication permission (7.4), the object permissions
// (5.7), the provider signature pre-check (7.3 P1) or the forward checks
// (7.3 P3). It reads the latest committed header first, so every chain
// read is answered at that height or later. The worker runs the same
// checks again before it signs.
func (s *Server) authorize(ctx context.Context, caller policy.Caller, auth policy.AuthContext, dec request.Decoded) (authorized, *apiError) {
	st, err := s.deps.Chain.Status(ctx)
	if err != nil {
		return authorized{}, fail(http.StatusServiceUnavailable, codeUnavailable, err)
	}
	switch dec.Kind {
	case policy.KindRegisterContent, policy.KindWithdrawContent:
		return s.authorizeContent(ctx, caller, auth, dec, st.Height)
	case policy.KindOfferLease:
		return s.authorizeOffer(ctx, caller, auth, dec, st.Height)
	case policy.KindRelayAcceptLease, policy.KindRelayRenewLease, policy.KindRelayEndLease:
		return s.authorizeRelay(ctx, caller, auth, dec, st)
	case policy.KindForwardProviderTx:
		return s.authorizeForward(ctx, caller, auth, dec, st)
	}
	return authorized{}, fail(http.StatusUnprocessableEntity, string(policy.CodeUnknownKind), nil)
}

// owner returns the caller's own non-VOID ownership row of contentID, or
// nil; another caller's row is invisible to the store read itself.
func (s *Server) owner(ctx context.Context, caller, contentID string) (*policy.ContentOwner, *apiError) {
	o, err := s.deps.Store.ContentOwnerFor(ctx, caller, contentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fail(http.StatusServiceUnavailable, codeUnavailable, err)
	}
	return &policy.ContentOwner{
		ContentID: o.ContentID, ContentVersion: o.ContentVersion, CallerPrincipal: o.CallerPrincipal,
		PrincipalID: o.PrincipalID, SignerAddress: o.SignerAddress, Status: policy.OwnerStatus(o.Status),
	}, nil
}

// onChain reads Query/Content for a content version; no record and no
// tombstone is nil.
func (s *Server) onChain(ctx context.Context, contentID string, version uint64, height int64) (*contentv1.QueryContentResponse, *apiError) {
	resp, found, _, err := s.deps.Chain.Content(ctx, contentID, version, height)
	if err != nil {
		return nil, fail(http.StatusServiceUnavailable, codeUnavailable, err)
	}
	if !found {
		return nil, nil
	}
	return resp, nil
}

func (s *Server) authorizeContent(ctx context.Context, caller policy.Caller, auth policy.AuthContext, dec request.Decoded, height int64) (authorized, *apiError) {
	service := s.signers[policy.ClassService].Address
	owner, apiErr := s.owner(ctx, caller.Principal, dec.Object.ID)
	if apiErr != nil {
		return authorized{}, apiErr
	}
	onchain, apiErr := s.onChain(ctx, dec.Object.ID, dec.Object.Version, height)
	if apiErr != nil {
		return authorized{}, apiErr
	}
	if err := policy.CheckPublication(policy.PublicationInput{
		Caller: caller, Auth: auth, Msg: dec.WithSigner(service), Service: service, Registry: s.deps.Registry,
		Owner: owner, OnChain: onchain,
	}); err != nil {
		return authorized{}, policyError(err)
	}
	return authorized{signer: service, object: dec.Object}, nil
}

func (s *Server) authorizeOffer(ctx context.Context, caller policy.Caller, auth policy.AuthContext, dec request.Decoded, height int64) (authorized, *apiError) {
	service := s.signers[policy.ClassService].Address
	msg := dec.WithSigner(service)
	offer, ok := msg.(interface {
		GetContentId() string
		GetContentVersion() uint64
	})
	if !ok {
		return authorized{}, fail(http.StatusInternalServerError, codeInternal, errors.New("api: offer message shape"))
	}
	owner, apiErr := s.owner(ctx, caller.Principal, offer.GetContentId())
	if apiErr != nil {
		return authorized{}, apiErr
	}
	onchain, apiErr := s.onChain(ctx, offer.GetContentId(), offer.GetContentVersion(), height)
	if apiErr != nil {
		return authorized{}, apiErr
	}
	if err := policy.CheckOffer(policy.OfferInput{
		Caller: caller, Auth: auth, Msg: msg, Service: service, Owner: owner, OnChain: onchain,
	}); err != nil {
		return authorized{}, policyError(err)
	}
	return authorized{signer: service, object: dec.Object}, nil
}

// relayAuthority is the relay object permission for caller.
func (s *Server) relayAuthority(caller policy.Caller) policy.RelayAuthority {
	return policy.RelayAuthority{
		Caller: caller,
		OwnsCommittedOffer: func(ctx context.Context, offerID string) (bool, error) {
			return s.deps.Store.CallerScheduledOffer(ctx, caller.Principal, offerID)
		},
	}
}

func (s *Server) authorizeRelay(ctx context.Context, caller policy.Caller, auth policy.AuthContext, dec request.Decoded, st chain.Status) (authorized, *apiError) {
	relayer := s.signers[policy.ClassBroadcaster].Address
	msg := dec.WithSigner(relayer)
	if err := policy.CheckSignable(policy.ClassBroadcaster, relayer, msg); err != nil {
		return authorized{}, policyError(err)
	}
	found, err := policy.PrecheckRelay(ctx, s.deps.Chain, policy.RelayInput{
		Msg: msg, ProviderID: dec.ProviderID, ContentID: dec.ContentID, MinHeight: st.Height, ChainTime: st.Time,
		DeadlinePages: s.cfg.DeadlinePages,
	})
	if err != nil {
		return authorized{}, policyError(err)
	}
	if err := s.relayAuthority(caller).CheckRelay(ctx, dec.Kind, auth, found); err != nil {
		return authorized{}, policyError(err)
	}
	return authorized{signer: relayer, object: dec.Object}, nil
}

func (s *Server) authorizeForward(ctx context.Context, caller policy.Caller, auth policy.AuthContext, dec request.Decoded, st chain.Status) (authorized, *apiError) {
	roles, err := policy.ReadGovernance(ctx, s.deps.Chain, st.Height)
	if err != nil {
		return authorized{}, fail(http.StatusServiceUnavailable, codeUnavailable, err)
	}
	fwd, err := policy.CheckForwardedTx(ctx, s.deps.Codec, s.deps.Chain, policy.ForwardInput{
		Raw: dec.Raw, ProviderID: dec.ProviderID, ContentID: dec.ContentID, ObservedHeight: st.Height,
		ChainTime: st.Time, Window: s.cfg.ForwardWindow, DeadlinePages: s.cfg.DeadlinePages, Roles: roles,
	})
	if err != nil {
		return authorized{}, policyError(err)
	}
	if err := s.relayAuthority(caller).CheckRelay(ctx, dec.Kind, auth, fwd.Relay); err != nil {
		return authorized{}, policyError(err)
	}
	object, businessID, err := request.ForwardObject(fwd.Msg)
	if err != nil {
		return authorized{}, requestError(err)
	}
	decoded, err := s.deps.Codec.Decode(dec.Raw)
	if err != nil {
		return authorized{}, fail(http.StatusUnprocessableEntity, string(policy.CodeForwardShape), err)
	}
	return authorized{
		signer: fwd.Signer,
		object: object,
		forward: &store.ForwardFields{
			MessageType: fwd.TypeURL, ProviderID: fwd.Relay.ProviderID, BusinessID: businessID,
			AccountNumber: fwd.AccountNumber, Sequence: fwd.Sequence, TimeoutHeight: fwd.TimeoutHeight, Gas: decoded.Gas,
		},
	}, nil
}
