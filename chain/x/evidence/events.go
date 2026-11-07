package evidence

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// Events. For every item of an accepted batch, in item order:
//
//	svn.events.v1.EvidenceAccepted{evidence_id, batch_id, kind (KindName), attestation "true"}  (msgkit.EmitTyped)
//	evidence_state{evidence_id, lease_id, state "RECORDED" or "REJECTED"}                       (untyped)
//
// EvidenceAccepted means the record was written to the ledger. It does not
// mean the check passed: svn.events.v1 has no state field, so the module
// emits the stored state in the untyped evidence_state event right after it
// (deviation D-16, decision request DR-09-16). An idempotent replay emits
// only evidence_result{idempotent "true"} (msgkit.EmitIdempotent). A
// rejected message emits nothing. Nothing is swept, so no event reports a
// record turning EXPIRED (deviation D-4). Committed transaction events end
// with the SDK's msg_index attribute (msgkit.ParseCommitted). Events are
// indexer input, not part of the application hash.
const (
	EventState     = "evidence_state"
	AttrEvidenceID = "evidence_id"
	AttrLeaseID    = "lease_id"
	AttrState      = "state"
)

// StateName is the short evidence state name used in events and labels: the
// enum name without the EVIDENCE_STATE_ prefix (RECORDED, EXPIRED, REJECTED).
func StateName(state evidencev1.EvidenceState) string {
	return strings.TrimPrefix(state.String(), "EVIDENCE_STATE_")
}

// emitAccepted emits the two events of one written record. EmitTyped fails
// only for a message outside svn.events.v1 or a string that is not UTF-8;
// every value here is a validated ASCII id, a kind name, or a bool, so a
// failure is an invariant violation. It panics; in a message handler the
// SDK fails the transaction and discards its writes on every node alike.
func emitAccepted(ctx sdk.Context, record *evidencev1.EvidenceRecord) {
	ev := &eventsv1.EvidenceAccepted{
		EvidenceId:  record.GetEvidenceId(),
		BatchId:     record.GetBatchId(),
		Kind:        KindName(record.GetKind()),
		Attestation: record.GetAttestation(),
	}
	if err := msgkit.EmitTyped(ctx, ev); err != nil {
		panic(fmt.Sprintf("evidence: emit EvidenceAccepted: %v", err))
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventState,
		sdk.NewAttribute(AttrEvidenceID, record.GetEvidenceId()),
		sdk.NewAttribute(AttrLeaseID, record.GetLeaseId()),
		sdk.NewAttribute(AttrState, StateName(record.GetState())),
	))
}
