package leases

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// Events. Typed events (svn.events.v1) go through msgkit.EmitTyped:
//
//	OfferLease                LeaseOffered{offer_id, provider_id, content_id}
//	AcceptLease               LeaseAccepted{lease_id, provider_id, content_id, content_version,
//	                                        package_bytes (decimal string), deadline_unix_seconds}
//	RenewLease                LeaseRenewed{lease_id, term_version (new), deadline_unix_seconds (new)}
//	EndLease                  LeaseEnded{lease_id, state "ENDED", released_bytes (decimal string)}
//	BeginBlock lease expiry   LeaseEnded{lease_id, state "EXPIRED", released_bytes}
//
// svn.events.v1 has no event for an offer that closes without acceptance,
// so the module emits one untyped event (deviation D-9, decision request
// DR-08-08):
//
//	BeginBlock offer expiry   leases_offer_closed{offer_id, reason "EXPIRED"}
//	withdrawal hook           leases_offer_closed{offer_id, reason "WITHDRAWN"}
//
// An idempotent replay emits only leases_result{idempotent "true"}
// (msgkit.EmitIdempotent). A rejected message emits nothing. Committed
// transaction events end with the SDK's msg_index attribute
// (msgkit.ParseCommitted); BeginBlock events end with mode=BeginBlock.
// Events are indexer input, not part of the application hash.
const (
	EventOfferClosed = "leases_offer_closed"
	AttrOfferID      = "offer_id"
	AttrReason       = "reason"
	ReasonExpired    = "EXPIRED"
	ReasonWithdrawn  = "WITHDRAWN"
)

// StateName is the short lease state name used in events: the enum name
// without the LEASE_STATE_ prefix (ACTIVE, RENEWED, ENDED, EXPIRED).
func StateName(state leasesv1.LeaseState) string {
	return strings.TrimPrefix(state.String(), "LEASE_STATE_")
}

func emitOffered(ctx sdk.Context, offer *leasesv1.Offer) {
	emit(ctx, &eventsv1.LeaseOffered{
		OfferId:    offer.GetOfferId(),
		ProviderId: offer.GetProviderId(),
		ContentId:  offer.GetContentId(),
	})
}

func emitAccepted(ctx sdk.Context, lease *leasesv1.Lease) {
	emit(ctx, &eventsv1.LeaseAccepted{
		LeaseId:             lease.GetLeaseId(),
		ProviderId:          lease.GetProviderId(),
		ContentId:           lease.GetContentId(),
		ContentVersion:      lease.GetContentVersion(),
		PackageBytes:        msgkit.Dec(lease.GetPackageBytes()),
		DeadlineUnixSeconds: lease.GetDeadlineUnixSeconds(),
	})
}

func emitRenewed(ctx sdk.Context, lease *leasesv1.Lease) {
	emit(ctx, &eventsv1.LeaseRenewed{
		LeaseId:             lease.GetLeaseId(),
		TermVersion:         lease.GetTermVersion(),
		DeadlineUnixSeconds: lease.GetDeadlineUnixSeconds(),
	})
}

// endedEvent reports a released lease. lease.state is ENDED or EXPIRED.
func endedEvent(lease *leasesv1.Lease) *eventsv1.LeaseEnded {
	return &eventsv1.LeaseEnded{
		LeaseId:       lease.GetLeaseId(),
		State:         StateName(lease.GetState()),
		ReleasedBytes: msgkit.Dec(lease.GetPackageBytes()),
	}
}

func emitOfferClosed(ctx sdk.Context, offerID, reason string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventOfferClosed,
		sdk.NewAttribute(AttrOfferID, offerID),
		sdk.NewAttribute(AttrReason, reason),
	))
}

// emit fails only for a message outside svn.events.v1 or a string that is
// not UTF-8. Every event type here is allowlisted and every value is a
// validated ASCII id, a state name, or a number, so a failure is an
// invariant violation. It panics; in a message handler the SDK fails the
// transaction and discards its writes on every node alike.
func emit(ctx sdk.Context, ev proto.Message) {
	if err := msgkit.EmitTyped(ctx, ev); err != nil {
		panic(fmt.Sprintf("leases: emit %s: %v", proto.MessageName(ev), err))
	}
}
