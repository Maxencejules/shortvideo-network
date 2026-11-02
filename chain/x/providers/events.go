package providers

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Typed events (svn.events.v1), emitted through msgkit.EmitTyped:
//
//	RegisterProvider                         ProviderRegistered{provider_id, state="APPLIED"}
//	RotateProviderKey, DrainProvider,
//	council admit, council disable           ProviderStateChanged{provider_id, state, key_generation}
//	UpdateProvider                           none (the spec emits none; events.proto has no update event)
//	idempotent replay                        none; one providers_result idempotent=true event instead
//
// State values are the spec machine's short names: APPLIED, ADMITTED,
// DRAINING, DISABLED. Events are indexer input; they are not part of the
// application hash.

// StateName is the short state name used in events: the enum name without
// the PROVIDER_STATE_ prefix.
func StateName(state providersv1.ProviderState) string {
	return strings.TrimPrefix(state.String(), "PROVIDER_STATE_")
}

func emitRegistered(ctx sdk.Context, record *providersv1.Provider) {
	emit(ctx, &eventsv1.ProviderRegistered{
		ProviderId: record.GetProviderId(),
		State:      StateName(record.GetState()),
	})
}

func emitStateChanged(ctx sdk.Context, record *providersv1.Provider) {
	emit(ctx, &eventsv1.ProviderStateChanged{
		ProviderId:    record.GetProviderId(),
		State:         StateName(record.GetState()),
		KeyGeneration: record.GetKeyGeneration(),
	})
}

// emit fails only for a message outside svn.events.v1 or a string that is
// not UTF-8. Both event types are allowlisted and every value is a
// validated ASCII id or a state name, so a failure is an invariant
// violation. It panics; the SDK fails the transaction and discards its
// writes on every node alike.
func emit(ctx sdk.Context, ev proto.Message) {
	if err := msgkit.EmitTyped(ctx, ev); err != nil {
		panic(fmt.Sprintf("providers: emit %s: %v", proto.MessageName(ev), err))
	}
}
