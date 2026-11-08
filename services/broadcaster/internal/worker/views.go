package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// Conversions between the store's rows, the chain client's observations
// and the state machine's typed views.

// intentView is the state machine's view of a non-terminal intent: its row
// and its attempts (at most 32). Business is at least 1, the business
// attempt of an intent the worker has not validated yet.
func intentView(w store.Work, atts []store.Attempt) (outbox.Intent, error) {
	state, err := outbox.ParseState(string(w.State))
	if err != nil {
		return outbox.Intent{}, err
	}
	in := outbox.Intent{State: state, Validated: w.Validated, Attempts: w.AttemptCount, Business: max(1, w.BusinessAttempt)}
	if w.Phase != store.PhaseNone {
		if in.Phase, err = outbox.ParsePhase(string(w.Phase)); err != nil {
			return outbox.Intent{}, err
		}
	}
	for _, a := range atts {
		if a.BusinessN == w.BusinessAttempt {
			in.BusinessTries++
		}
		if sdk4Death(a) {
			in.Sdk4Deaths++
		}
	}
	return in, nil
}

// sdk4Death reports whether a died by (d2) with a last CheckTx refusal of
// sdk code 4 (DR-11-18).
func sdk4Death(a store.Attempt) bool {
	return a.DeadReason == string(store.DeadD2Refused) && a.LastOutcome == string(store.SendMempoolRejected) &&
		a.Ack != nil && a.Ack.Codespace == outbox.CodeSDKUnauthorized.Codespace && a.Ack.Code == outbox.CodeSDKUnauthorized.Number
}

// attemptView is the state machine's view of a stored attempt.
func attemptView(a store.Attempt) (outbox.Attempt, error) {
	if len(a.TxHash) != 32 {
		return outbox.Attempt{}, errors.New("worker: attempt without a 32-byte hash")
	}
	phase, err := outbox.ParseAttemptPhase(string(a.Phase))
	if err != nil {
		return outbox.Attempt{}, err
	}
	v := outbox.Attempt{Sequence: a.Sequence, TimeoutHeight: a.TimeoutHeight, Phase: phase, Sends: outbox.Sends{
		Started: a.SendsStarted, Recorded: a.SendsRecorded, Unknown: a.UnknownSends, Cache: a.CacheAnswers,
		Refusals: a.CountedRefusals, Acked: a.Acked,
	}}
	copy(v.Hash[:], a.TxHash)
	if phase == outbox.AttemptDead {
		if v.Dead, err = outbox.ParseDeadReason(a.DeadReason); err != nil {
			return outbox.Attempt{}, err
		}
	}
	if a.SendsRecorded > 0 {
		if v.Sends.Last.Outcome, err = outbox.ParseSendOutcome(a.LastOutcome); err != nil {
			return outbox.Attempt{}, err
		}
		if v.Sends.Last.Outcome == outbox.SendMempoolRejected {
			if a.Ack == nil {
				return outbox.Attempt{}, errors.New("worker: a CheckTx rejection without its code")
			}
			v.Sends.Last.Code = outbox.Code{Codespace: a.Ack.Codespace, Number: a.Ack.Code}
		}
	}
	return v, nil
}

// txResult is the state machine's view of an attempt's committed result,
// which ClassifyCommitted read from /tx with the hash check.
func txResult(o chain.CommitObservation) outbox.TxResult {
	r := outbox.TxResult{Found: true, HashVerified: true, Height: uint64(max(o.Height, 1)),
		Code: outbox.Code{Codespace: o.Codespace, Number: o.Code}, AccSeq: outbox.AccSeqThis}
	switch o.Class {
	case chain.CommittedOK:
		r.TypedEvent = true
	case chain.CommittedIdempotent:
		r.Idempotent = true
	case chain.IncludedAnteFailed:
		r.AccSeq = outbox.AccSeqAbsent
	}
	return r
}

// committedResult is a committed observation as the store records it on
// its attempt.
func committedResult(o chain.CommitObservation) store.CommittedResult {
	return store.CommittedResult{
		TxHash: o.Hash, Height: uint64(max(o.Height, 0)), Index: o.Index, Code: o.Code, Codespace: o.Codespace,
		AccSeqPresent: o.Class != chain.IncludedAnteFailed, GasUsed: uint64(max(o.GasUsed, 0)),
		Idempotent: o.Class == chain.CommittedIdempotent,
	}
}

// storeOutcome is a classified broadcast answer as the store records it.
// CountsAsRefusal is the chain client's (d2) rule; the store refuses it
// unless it equals its own copy of the rule and the state machine reads
// the stored outcome with its own (TestRefusalRuleCopiesAgree). A CheckTx
// rejection without a codespace is recorded as UNKNOWN, which never counts
// for (d2).
func storeOutcome(o chain.SendObservation) store.SendOutcome {
	out := store.SendOutcome{CountsAsRefusal: o.DefinitiveRefusal(), Log: o.Detail}
	switch o.Outcome {
	case chain.SendCache:
		out.Class = store.SendCache
	case chain.SendNodeRefused:
		out.Class = store.SendNodeRefused
	case chain.SendAck:
		out.Class = store.SendAck
	case chain.SendMempoolRejected:
		if o.Codespace == "" || o.Code == 0 {
			return store.SendOutcome{Class: store.SendUnknown, Log: o.Detail}
		}
		out.Class, out.Code, out.Codespace = store.SendMempoolRejected, o.Code, o.Codespace
	default:
		return store.SendOutcome{Class: store.SendUnknown, Log: o.Detail}
	}
	return out
}

// messageOf rebuilds the chain message of a validated intent: the stored
// request (the message with signer and business id empty) with the
// intent's pinned signer and business id; for a forward, the one message
// of the stored provider-signed transaction.
func messageOf(c *chain.Codec, w store.Work) (sdk.Msg, error) {
	if !w.Validated || len(w.Request) == 0 {
		return nil, errors.New("worker: intent has no validated request")
	}
	if w.Kind == store.KindForwardProvider {
		decoded, err := c.Decode(w.Request)
		if err != nil {
			return nil, err
		}
		own, err := decoded.Own()
		if err != nil {
			return nil, err
		}
		return own.Msg.Msg, nil
	}
	var msg sdk.Msg
	switch w.Kind {
	case store.KindRegisterContent:
		m := &contentv1.MsgRegisterContent{}
		if err := proto.Unmarshal(w.Request, m); err != nil {
			return nil, err
		}
		m.Signer, m.BusinessId = w.SignerAddress, w.BusinessID
		msg = m
	case store.KindWithdrawContent:
		m := &contentv1.MsgWithdrawContent{}
		if err := proto.Unmarshal(w.Request, m); err != nil {
			return nil, err
		}
		m.Signer, m.BusinessId = w.SignerAddress, w.BusinessID
		msg = m
	case store.KindOfferLease:
		m := &leasesv1.MsgOfferLease{}
		if err := proto.Unmarshal(w.Request, m); err != nil {
			return nil, err
		}
		m.Signer, m.BusinessId = w.SignerAddress, w.BusinessID
		msg = m
	case store.KindRelayAcceptLease:
		m := &leasesv1.MsgAcceptLease{}
		if err := proto.Unmarshal(w.Request, m); err != nil {
			return nil, err
		}
		m.Signer, m.BusinessId = w.SignerAddress, w.BusinessID
		msg = m
	case store.KindRelayRenewLease:
		m := &leasesv1.MsgRenewLease{}
		if err := proto.Unmarshal(w.Request, m); err != nil {
			return nil, err
		}
		m.Signer, m.BusinessId = w.SignerAddress, w.BusinessID
		msg = m
	case store.KindRelayEndLease:
		m := &leasesv1.MsgEndLease{}
		if err := proto.Unmarshal(w.Request, m); err != nil {
			return nil, err
		}
		m.Signer, m.BusinessId = w.SignerAddress, w.BusinessID
		msg = m
	default:
		return nil, fmt.Errorf("worker: kind %q has no message", w.Kind)
	}
	if _, err := chain.ObjectOf(msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// T2 reasons (plan 9.3): what a failed validation stores.
const (
	reasonUnauthorized      = "UNAUTHORIZED"
	reasonInvalid           = "INVALID"
	reasonProviderSignature = "PROVIDER_SIGNATURE_INVALID"
)

// reasonOf maps a validation refusal to its T2 reason: a provider
// signature that does not verify is PROVIDER_SIGNATURE_INVALID; a missing
// caller kind, permission, relay authority, hosting-intent registration or
// verification, or an object the caller may not act on is UNAUTHORIZED;
// every other refusal (a malformed request, an offer, lease or provider not
// found, a content id unavailable, a forward that breaks a P3 rule) is
// INVALID. It returns "" when err is not a refusal (a read that failed:
// retry later).
func reasonOf(err error) string {
	if request.ClassOf(err) != 0 {
		return reasonInvalid
	}
	switch policy.CodeOf(err) {
	case "":
		return ""
	case policy.CodeProviderSignature:
		return reasonProviderSignature
	case policy.CodePermission, policy.CodeKindNotAllowed, policy.CodeRelayNotAllowed, policy.CodeNotVisible,
		policy.CodeIntentKeyUnregistered, policy.CodeIntentSignature, policy.CodeTypeNotAllowed,
		policy.CodeSignerMismatch, policy.CodeAuthorityEnd, policy.CodeUnknownKind:
		return reasonUnauthorized
	}
	return reasonInvalid
}

// typedEventJSON is one svn.events.v1 event of a commitment: its type, its
// position in the transaction result's events, its msg_index, and its
// fields.
type typedEventJSON struct {
	Type     string          `json:"type"`
	Position int             `json:"position"`
	MsgIndex uint32          `json:"msg_index"`
	Fields   json.RawMessage `json:"fields"`
}

// eventsJSON returns the typed events of a committed result as the JSON
// array the commitments row stores (plan 9.5), each decoded with
// msgkit.ParseCommitted.
func eventsJSON(events []abci.Event) (json.RawMessage, error) {
	out := []typedEventJSON{}
	for i, ev := range events {
		if !strings.HasPrefix(ev.Type, "svn.events.v1.") {
			continue
		}
		msg, index, err := msgkit.ParseCommitted(ev)
		if err != nil {
			return nil, &chain.IntegrityError{Reason: fmt.Sprintf("typed event: %v", err)}
		}
		fields, err := protojson.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("worker: encode typed event: %w", err)
		}
		out = append(out, typedEventJSON{Type: ev.Type, Position: i, MsgIndex: index, Fields: fields})
	}
	return json.Marshal(out)
}
