package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// locators name where a relay's or forward's pre-check reads: the
// provider and content of the held lease of a renew or end (no chain query
// returns a lease by id, DR-08-08), and the provider of a forward. The SQL
// path keeps them in the stored request JSON; the HTTP path stores a
// forward's provider only, so a held lease is otherwise found by paging
// Query/Deadlines.
type locators struct {
	providerID string
	contentID  string
}

// locatorsOf returns the locators of a stored intent.
func locatorsOf(w store.Work) locators {
	var loc locators
	if len(w.RequestJSON) > 0 {
		if dec, err := request.DecodeRequest(policy.Kind(w.Kind), json.RawMessage(w.RequestJSON)); err == nil {
			loc = locators{providerID: dec.ProviderID, contentID: dec.ContentID}
		}
	}
	if w.Forward != nil && loc.providerID == "" {
		loc.providerID = w.Forward.ProviderID
	}
	return loc
}

// authorized is what the checks of one request decided: the account whose
// sequence its transaction uses, its object, and a forward's fields.
type authorized struct {
	signer  []byte
	object  store.ObjectRef
	forward *store.ForwardFields
}

// checkRequest runs the checks of plan 6.3 for one request against the
// latest committed state, as the API does at enqueue: the publication
// permission (7.4), the object permissions (5.7), the provider signature
// pre-check (7.3 P1) or the forward checks (7.3 P3). Every read is
// answered at the read floor of the latest header or later
// (chain.Status.AppFloor: its height minus one, because the node may
// report a block its application is still executing). msg is the request's
// message with the kind's key as signer (nil for a forward, whose bytes
// are raw). ownIntent is the intent being checked again before signing
// (empty at its first validation): its own PENDING owner row does not make
// its content id unavailable. A refusal is a *policy.Refusal; any other
// error is a read that failed.
func (w *Worker) checkRequest(ctx context.Context, caller policy.Caller, auth policy.AuthContext, kind policy.Kind,
	msg sdk.Msg, raw []byte, loc locators, ownIntent string) (authorized, error) {
	st, err := w.cfg.Chain.Status(ctx)
	if err != nil {
		return authorized{}, err
	}
	service := w.keys[policy.ClassService].Address
	relayer := w.keys[policy.ClassBroadcaster].Address
	switch kind {
	case policy.KindRegisterContent, policy.KindWithdrawContent:
		ref, ok := msg.(interface {
			GetContentId() string
			GetContentVersion() uint64
		})
		if !ok {
			return authorized{}, errors.New("worker: content message shape")
		}
		owner, err := w.ownerRow(ctx, caller.Principal, ref.GetContentId(), ownIntent)
		if err != nil {
			return authorized{}, err
		}
		onchain, found, _, err := w.cfg.Chain.Content(ctx, ref.GetContentId(), ref.GetContentVersion(), st.AppFloor())
		if err != nil {
			return authorized{}, err
		}
		if !found {
			onchain = nil
		}
		if err := policy.CheckPublication(policy.PublicationInput{Caller: caller, Auth: auth, Msg: msg, Service: service,
			Registry: w.cfg.Registry, Owner: owner, OnChain: onchain}); err != nil {
			return authorized{}, err
		}
		return authorized{signer: service, object: store.ObjectRef{Type: store.ObjectContent, ID: ref.GetContentId(),
			Version: ref.GetContentVersion()}}, nil
	case policy.KindOfferLease:
		offer, ok := msg.(*leasesv1.MsgOfferLease)
		if !ok {
			return authorized{}, errors.New("worker: offer message shape")
		}
		owner, err := w.ownerRow(ctx, caller.Principal, offer.GetContentId(), ownIntent)
		if err != nil {
			return authorized{}, err
		}
		onchain, found, _, err := w.cfg.Chain.Content(ctx, offer.GetContentId(), offer.GetContentVersion(), st.AppFloor())
		if err != nil {
			return authorized{}, err
		}
		if !found {
			onchain = nil
		}
		if err := policy.CheckOffer(policy.OfferInput{Caller: caller, Auth: auth, Msg: msg, Service: service, Owner: owner,
			OnChain: onchain}); err != nil {
			return authorized{}, err
		}
		return authorized{signer: service, object: store.ObjectRef{Type: store.ObjectOffer, ID: offer.GetOfferId()}}, nil
	case policy.KindRelayAcceptLease, policy.KindRelayRenewLease, policy.KindRelayEndLease:
		if err := policy.CheckSignable(policy.ClassBroadcaster, relayer, msg); err != nil {
			return authorized{}, err
		}
		loc, err := w.leaseLocators(ctx, msg, loc)
		if err != nil {
			return authorized{}, err
		}
		found, err := policy.PrecheckRelay(ctx, w.cfg.Chain, policy.RelayInput{Msg: msg, ProviderID: loc.providerID,
			ContentID: loc.contentID, MinHeight: st.AppFloor(), ChainTime: st.Time, DeadlinePages: w.cfg.Numbers.DeadlinePages})
		if err != nil {
			return authorized{}, err
		}
		if err := w.relayAuthority(caller).CheckRelay(ctx, kind, auth, found); err != nil {
			return authorized{}, err
		}
		object := store.ObjectRef{Type: store.ObjectLease, ID: found.LeaseID}
		if accept, ok := msg.(*leasesv1.MsgAcceptLease); ok {
			object = store.ObjectRef{Type: store.ObjectOffer, ID: accept.GetOfferId()}
		}
		return authorized{signer: relayer, object: object}, nil
	case policy.KindForwardProviderTx:
		return w.checkForward(ctx, caller, auth, raw, loc, st)
	}
	return authorized{}, &policy.Refusal{Code: policy.CodeUnknownKind, Detail: "kind is not one of the routed kinds"}
}

// leaseLocators completes the locators of a renew or end whose request did
// not name its (provider, content) pair: the held lease with the message's
// lease id, found by paging Query/Deadlines. A lease not found within the
// page budget is LEASE_NOT_FOUND.
func (w *Worker) leaseLocators(ctx context.Context, msg sdk.Msg, loc locators) (locators, error) {
	var leaseID string
	switch m := msg.(type) {
	case *leasesv1.MsgRenewLease:
		leaseID = m.GetLeaseId()
	case *leasesv1.MsgEndLease:
		leaseID = m.GetLeaseId()
	default:
		return loc, nil
	}
	if loc.providerID != "" && loc.contentID != "" {
		return loc, nil
	}
	_, lease, _, _, err := w.deadlineEntry(ctx, "", leaseID)
	if err != nil {
		return locators{}, err
	}
	if lease == nil {
		return locators{}, &policy.Refusal{Code: policy.CodeLeaseNotFound, Detail: "no held lease with this id within the page budget"}
	}
	return locators{providerID: lease.GetProviderId(), contentID: lease.GetContentId()}, nil
}

// checkForward runs the forward checks of plan 7.3 P3 on a provider-signed
// transaction, then the relay authority of the caller. st is the latest
// header: its read floor bounds the reads, its height the forward window.
func (w *Worker) checkForward(ctx context.Context, caller policy.Caller, auth policy.AuthContext, raw []byte, loc locators,
	st chain.Status) (authorized, error) {
	decoded, err := w.cfg.Chain.Decode(raw)
	if err != nil {
		return authorized{}, &policy.Refusal{Code: policy.CodeForwardShape, Detail: "not a transaction the application decodes strictly"}
	}
	if len(decoded.Msgs) == 1 {
		if loc, err = w.leaseLocators(ctx, decoded.Msgs[0].Msg, loc); err != nil {
			return authorized{}, err
		}
	}
	roles, err := policy.ReadGovernance(ctx, w.cfg.Chain, st.AppFloor())
	if err != nil {
		return authorized{}, err
	}
	fwd, err := policy.CheckForwardedTx(ctx, w.cfg.Chain.Codec, w.cfg.Chain, policy.ForwardInput{
		Raw: raw, ProviderID: loc.providerID, ContentID: loc.contentID, ObservedHeight: st.Height, MinHeight: st.AppFloor(), ChainTime: st.Time,
		Window: w.cfg.Numbers.ForwardWindow, DeadlinePages: w.cfg.Numbers.DeadlinePages, Roles: roles,
	})
	if err != nil {
		return authorized{}, err
	}
	if err := w.relayAuthority(caller).CheckRelay(ctx, policy.KindForwardProviderTx, auth, fwd.Relay); err != nil {
		return authorized{}, err
	}
	object, businessID, err := request.ForwardObject(fwd.Msg)
	if err != nil {
		return authorized{}, err
	}
	return authorized{signer: fwd.Signer, object: object, forward: &store.ForwardFields{
		MessageType: fwd.TypeURL, ProviderID: fwd.Relay.ProviderID, BusinessID: businessID,
		AccountNumber: fwd.AccountNumber, Sequence: fwd.Sequence, TimeoutHeight: fwd.TimeoutHeight, Gas: decoded.Gas,
	}}, nil
}

// relayAuthority is the relay object permission of caller (plan 5.7).
func (w *Worker) relayAuthority(caller policy.Caller) policy.RelayAuthority {
	return policy.RelayAuthority{Caller: caller, OwnsCommittedOffer: func(ctx context.Context, offerID string) (bool, error) {
		return w.st.CallerScheduledOffer(ctx, caller.Principal, offerID)
	}}
}

// ownerRow returns the caller's own non-VOID owner row of contentID, or nil
// when there is none or it is ownIntent's own row.
func (w *Worker) ownerRow(ctx context.Context, caller, contentID, ownIntent string) (*policy.ContentOwner, error) {
	o, err := w.st.ContentOwnerFor(ctx, caller, contentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ownIntent != "" && o.IntentID == ownIntent {
		return nil, nil
	}
	return &policy.ContentOwner{ContentID: o.ContentID, ContentVersion: o.ContentVersion, CallerPrincipal: o.CallerPrincipal,
		PrincipalID: o.PrincipalID, SignerAddress: o.SignerAddress, Status: policy.OwnerStatus(o.Status)}, nil
}

// callerAndContext returns the configured caller of a stored intent and
// its decoded authorization context, or the T2 reason that refuses them.
func (w *Worker) callerAndContext(principal string, raw []byte) (policy.Caller, policy.AuthContext, string) {
	caller, ok := w.callers[principal]
	if !ok {
		return policy.Caller{}, policy.AuthContext{}, reasonUnauthorized
	}
	auth, _, err := request.DecodeAuthContext(raw, w.cfg.Numbers.AuthContextBytes)
	if err != nil {
		return policy.Caller{}, policy.AuthContext{}, reasonInvalid
	}
	return caller, auth, ""
}

// revalidate checks a validated intent again immediately before a new
// attempt is signed or a forward is persisted (plan 6.3: chain state can
// change after enqueue): the caller is still configured, the stored context
// still decodes, and checkRequest passes for the intent's message. It
// returns the T2 reason of a refusal, or an error for a read that failed.
func (w *Worker) revalidate(ctx context.Context, work store.Work, msg sdk.Msg) (string, error) {
	caller, auth, reason := w.callerAndContext(work.CallerPrincipal, work.AuthContext)
	if reason != "" {
		return reason, nil
	}
	kind := policy.Kind(work.Kind)
	var raw []byte
	if kind == policy.KindForwardProviderTx {
		raw, msg = work.Request, nil
	}
	if _, err := w.checkRequest(ctx, caller, auth, kind, msg, raw, locatorsOf(work), work.ID); err != nil {
		if r := reasonOf(err); r != "" {
			return r, nil
		}
		return "", err
	}
	return "", nil
}

// validateSQL is the worker's full validation of a SQL-path intent (plan
// 6.3, T2): the caller and its kinds, the kind, the authorization context
// under the configured cap, the intent's lifetime under the configured
// bounds (a RENEW or END relay at most the relay local expiry, owner answer
// Q4), the typed request, and checkRequest. It returns what MarkValidated
// stores, or the T2 reason of a refusal, or an error for a read that
// failed.
func (w *Worker) validateSQL(ctx context.Context, u store.Unvalidated, work store.Work) (store.Validation, string, error) {
	caller, auth, reason := w.callerAndContext(u.CallerPrincipal, u.AuthContext)
	if reason != "" {
		return store.Validation{}, reason, nil
	}
	spec, err := request.LookupKind(u.Kind)
	if err != nil {
		return store.Validation{}, reasonInvalid, nil
	}
	kind := spec.Kind
	if err := caller.CheckKind(kind); err != nil {
		return store.Validation{}, reasonOf(err), nil
	}
	if err := policy.CheckGrant(kind, auth); err != nil {
		return store.Validation{}, reasonOf(err), nil
	}
	lifetime := work.ExpiresAt.Sub(work.CreatedAt)
	if lifetime < w.cfg.Numbers.MinLifetime || lifetime > w.cfg.Numbers.MaxLifetime ||
		((kind == policy.KindRelayRenewLease || kind == policy.KindRelayEndLease) && lifetime > w.cfg.Numbers.RelayLocalExpiry) {
		return store.Validation{}, reasonInvalid, nil
	}
	dec, err := request.DecodeRequest(kind, json.RawMessage(u.RequestJSON))
	if err != nil {
		return store.Validation{}, reasonInvalid, nil
	}
	var msg sdk.Msg
	if kind != policy.KindForwardProviderTx {
		msg = dec.WithSigner(w.keys[spec.Class].Address)
	}
	a, err := w.checkRequest(ctx, caller, auth, kind, msg, dec.Raw, locators{providerID: dec.ProviderID, contentID: dec.ContentID}, "")
	if err != nil {
		if r := reasonOf(err); r != "" {
			return store.Validation{}, r, nil
		}
		return store.Validation{}, "", err
	}
	stored, err := dec.Stored()
	if err != nil {
		return store.Validation{}, reasonInvalid, nil
	}
	digest, err := request.Digest(kind, stored, auth)
	if err != nil {
		return store.Validation{}, "", fmt.Errorf("worker: request digest: %w", err)
	}
	return store.Validation{Request: stored, RequestDigest: digest, SignerAddress: a.signer, Object: a.object, Forward: a.forward}, "", nil
}
