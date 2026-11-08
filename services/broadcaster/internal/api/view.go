package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// intentView is the status of one intent as a caller sees it (plan 5.8).
// It carries digests of the stored request and authorization context,
// never the request, a signature, a hosting intent or the context itself.
type intentView struct {
	IntentID       string `json:"intent_id"`
	Kind           string `json:"kind"`
	IdempotencyKey string `json:"idempotency_key"`
	// Status is the frozen OperationStatus name (DR-11-09: no new value;
	// "uncertain" is SUBMITTED with detail.phase UNCERTAIN).
	Status     string          `json:"status"`
	Detail     detailView      `json:"detail"`
	Commitment *commitmentView `json:"commitment,omitempty"`
	Failure    *failureView    `json:"failure,omitempty"`
	BusinessID string          `json:"business_id,omitempty"`
	// RequestDigest is the HTTP-path digest; SQLDigest the SQL path's.
	RequestDigest     string `json:"request_digest,omitempty"`
	SQLDigest         string `json:"sql_digest,omitempty"`
	AuthContextDigest string `json:"auth_context_digest,omitempty"`
	// AuthorizationsCleared says the outbox removed the stored request,
	// authorization context and signatures of an intent that failed with
	// nothing ever sent (owner answer Q4); their digests stay.
	AuthorizationsCleared bool       `json:"authorizations_cleared"`
	CreatedAt             time.Time  `json:"created_at"`
	ExpiresAt             time.Time  `json:"expires_at"`
	FinalAt               *time.Time `json:"final_at,omitempty"`
}

// detailView is the detail object of plan 5.8.
type detailView struct {
	// Phase is the underlying phase of a non-terminal intent (kept under a
	// PENDING_STALL overlay).
	Phase string `json:"phase,omitempty"`
	// Reason is EXPIRED_AWAITING_CHAIN for an intent whose expiry passed
	// while its live attempt may still commit.
	Reason      string    `json:"reason,omitempty"`
	Attempts    int       `json:"attempts"`
	LastOutcome string    `json:"last_outcome,omitempty"`
	LastAck     *codeView `json:"last_ack,omitempty"`
	// LastLandingHeight is the timeout height of the live attempt: the
	// last height at which it can still commit. expires_at bounds new
	// attempts and sends only.
	LastLandingHeight uint64 `json:"last_landing_height,omitempty"`
	// Observer is FRESH or OBSERVER_UNAVAILABLE; a stale observer is never
	// shown as a stall.
	Observer string     `json:"observer"`
	Stall    *stallView `json:"stall,omitempty"`
}

type codeView struct {
	Code      uint32 `json:"code"`
	Codespace string `json:"codespace"`
}

type stallView struct {
	SinceHeight uint64    `json:"since_height"`
	SinceTime   time.Time `json:"since_time"`
	Reason      string    `json:"reason"`
}

type failureView struct {
	Reason    string `json:"reason"`
	Code      uint32 `json:"code,omitempty"`
	Codespace string `json:"codespace,omitempty"`
}

// commitmentView is the canonical commitment evidence of plan 9.5.
type commitmentView struct {
	ChainID           string          `json:"chain_id"`
	BusinessID        string          `json:"business_id"`
	Height            uint64          `json:"height"`
	BlockHash         string          `json:"block_hash"`
	TxHash            string          `json:"tx_hash"`
	FirstCommitTxHash string          `json:"first_commit_tx_hash"`
	TxIndex           uint32          `json:"tx_index"`
	MsgIndex          uint32          `json:"msg_index"`
	Code              uint32          `json:"code"`
	Codespace         string          `json:"codespace"`
	GasWanted         uint64          `json:"gas_wanted"`
	GasUsed           uint64          `json:"gas_used"`
	Events            json.RawMessage `json:"events"`
	IdempotentReplay  bool            `json:"idempotent_replay"`
	CommittingSigner  string          `json:"committing_signer"`
	OwnTransaction    bool            `json:"own_transaction"`
	RecordedAt        time.Time       `json:"recorded_at"`
}

// overlay is what a read adds to the stored state (plan section 10).
type overlay struct {
	outbox.Overlay
	episode *store.StallEpisode
}

// errIntegrity is a stored intent whose evidence contradicts its state; it
// is answered INTEGRITY and never shown.
var errIntegrity = errors.New("api: stored intent and its commitment evidence disagree")

// presentInput converts a stored intent into the state machine's view for
// outbox.Present, which maps state, phase, reason and code to the caller
// status. Present reads only those fields; Validated, the attempt count,
// the canonical hash and own_transaction come from the row, and the
// business attempt counters, which Present does not read, are set to the
// least values its consistency check accepts for the stored attempt count.
func presentInput(it store.Intent) (outbox.Intent, error) {
	st, err := outbox.ParseState(string(it.State))
	if err != nil {
		return outbox.Intent{}, err
	}
	in := outbox.Intent{State: st, Validated: it.Object != nil, Attempts: it.AttemptCount, Business: 1}
	if it.AttemptCount > 0 {
		in.BusinessTries = 1
	}
	if it.Phase != store.PhaseNone {
		if in.Phase, err = outbox.ParsePhase(string(it.Phase)); err != nil {
			return outbox.Intent{}, err
		}
	}
	switch st {
	case outbox.StateFailed:
		if in.Reason, err = outbox.ParseReason(it.Reason); err != nil {
			return outbox.Intent{}, err
		}
		if it.Failure != nil {
			in.Code = outbox.Code{Codespace: it.Failure.Codespace, Number: it.Failure.Code}
		}
	case outbox.StateCommitted:
		copy(in.Canonical[:], it.CommittedTxHash)
		in.Own = it.OwnTransaction != nil && *it.OwnTransaction
	}
	return in, nil
}

// checkEvidence refuses to show an intent whose stored evidence does not
// back its state: a COMMITTED intent needs its commitment row for the
// configured chain, with a 32-byte canonical hash equal to the row's first
// commitment, a 32-byte block hash, a height and code 0 (the canonical
// commitment is a success with the kind's typed event); any other intent
// has no commitment.
func checkEvidence(it store.Intent, chainID string) error {
	if it.State != store.StateCommitted {
		if it.Commitment != nil || len(it.CommittedTxHash) != 0 {
			return errIntegrity
		}
		return nil
	}
	c := it.Commitment
	switch {
	case c == nil, len(it.CommittedTxHash) != 32, c.ChainID != chainID, c.Height == 0, len(c.BlockHash) != 32,
		len(c.TxHash) != 32, c.Code != 0, !bytes.Equal(c.FirstCommitTxHash, it.CommittedTxHash), it.OwnTransaction == nil:
		return errIntegrity
	}
	return nil
}

// view builds the caller view of a stored intent under the read-time
// overlay. bech32 renders the committing signer.
func view(it store.Intent, o overlay, chainID string, bech32 func([]byte) (string, error)) (intentView, error) {
	if err := checkEvidence(it, chainID); err != nil {
		return intentView{}, err
	}
	in, err := presentInput(it)
	if err != nil {
		return intentView{}, errors.Join(errIntegrity, err)
	}
	v, err := outbox.Present(in, o.Overlay)
	if err != nil {
		return intentView{}, errors.Join(errIntegrity, err)
	}
	out := intentView{
		IntentID: it.ID, Kind: it.Kind, IdempotencyKey: it.IdempotencyKey, Status: v.Status.String(),
		BusinessID: it.BusinessID, RequestDigest: hexOrEmpty(it.RequestDigest), SQLDigest: hexOrEmpty(it.SQLDigest),
		AuthContextDigest: hexOrEmpty(it.AuthContextDigest), AuthorizationsCleared: it.ClearedAt != nil,
		CreatedAt: it.CreatedAt, ExpiresAt: it.ExpiresAt, FinalAt: it.FinalAt,
		Detail: detailView{
			Attempts: it.AttemptCount, LastOutcome: it.LastOutcome, LastLandingHeight: it.LandingHeight,
			Observer: v.Observer.String(),
		},
	}
	if v.Phase != 0 {
		out.Detail.Phase = v.Phase.String()
	}
	if it.LastAck != nil {
		out.Detail.LastAck = &codeView{Code: it.LastAck.Code, Codespace: it.LastAck.Codespace}
	}
	switch v.Status {
	case outbox.StatusFailed:
		out.Failure = &failureView{Reason: v.Reason.String(), Code: v.Code.Number, Codespace: v.Code.Codespace}
	case outbox.StatusCommitted:
		cv, err := commitment(it, bech32)
		if err != nil {
			return intentView{}, err
		}
		out.Commitment = cv
	default:
		if v.Reason != 0 {
			out.Detail.Reason = v.Reason.String()
		}
	}
	if v.Stall && o.episode != nil {
		out.Detail.Stall = &stallView{
			SinceHeight: o.episode.StartedHeight, SinceTime: o.episode.StartedAt, Reason: string(o.episode.Reason),
		}
	}
	return out, nil
}

func commitment(it store.Intent, bech32 func([]byte) (string, error)) (*commitmentView, error) {
	c := it.Commitment
	signer, err := bech32(c.CommittingSigner)
	if err != nil {
		return nil, errors.Join(errIntegrity, err)
	}
	events := json.RawMessage(c.Events)
	if !json.Valid(events) {
		return nil, errIntegrity
	}
	return &commitmentView{
		ChainID: c.ChainID, BusinessID: c.BusinessID, Height: c.Height, BlockHash: hex.EncodeToString(c.BlockHash),
		TxHash: hex.EncodeToString(c.TxHash), FirstCommitTxHash: hex.EncodeToString(c.FirstCommitTxHash),
		TxIndex: c.TxIndex, MsgIndex: c.MsgIndex, Code: c.Code, Codespace: c.Codespace, GasWanted: c.GasWanted,
		GasUsed: c.GasUsed, Events: events, IdempotentReplay: c.IdempotentReplay, CommittingSigner: signer,
		OwnTransaction: *it.OwnTransaction, RecordedAt: c.RecordedAt,
	}, nil
}

func hexOrEmpty(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return hex.EncodeToString(b)
}
