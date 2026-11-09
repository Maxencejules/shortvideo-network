package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/faultpoint"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// maxGapCheck bounds the sequences one allocation checks with the acc_seq
// search when the committed account sequence is above the signer's next
// sequence. A larger gap halts the signer (SEQUENCE_GAP) for the operator.
const maxGapCheck = 256

// startAttempt builds, signs, persists and sends a new attempt for a
// validated REQUESTED intent of the key signer (plan 8.1-8.3, T8):
//
//  1. the next sequence: the committed account read at a height of at
//     least the signer's observed height (a lower answer is a stale node:
//     wait). Sequences the chain consumed above the stored next sequence
//     must each be one of our attempts or a foreign use an operator
//     acknowledged; any other foreign use halts the signer
//     (DR-11-15). The stored sequence then follows the chain;
//  2. the timeout height: the latest height plus K, strictly above every
//     earlier attempt at this sequence (any intent), and, after an attempt
//     that died with sdk code 4, at least Sdk4RetryGapBlocks above it
//     (DR-11-18); when the latest height is not high enough yet the intent
//     waits for a block;
//  3. the gas: the kind's declared gas, or for a withdrawal the gas the node
//     measured for the broadcaster's simulation transaction plus the margin
//     (ruling R3: the tx service Simulate route);
//  4. the signer process signs; the returned bytes are decoded again and
//     must carry exactly the requested message, sequence, gas and timeout;
//  5. PersistAttempt stores the attempt under the intent and signer row
//     locks (a refusal discards the bytes, which were never sent); then the
//     first send.
func (s *session) startAttempt(ctx context.Context, work store.Work, atts []store.Attempt, msg sdk.Msg) (bool, error) {
	acct, seq, ok, err := s.allocate(ctx)
	if err != nil || !ok {
		return false, err
	}
	st, err := s.w.cfg.Chain.Status(ctx)
	if err != nil {
		return false, err
	}
	earlier, err := s.earlierTimeouts(ctx, seq, atts)
	if err != nil {
		return false, err
	}
	timeout, ready, err := chain.NextTimeoutHeight(st.Height, s.w.cfg.Numbers.TimeoutOffset, earlier)
	if err != nil || !ready {
		return false, err
	}
	gas, err := s.gasFor(ctx, policy.Kind(work.Kind), msg, seq, timeout)
	if err != nil {
		return false, err
	}
	req := signer.SignRequest{KeyName: s.key.Name, AccountNumber: acct, Sequence: seq, Gas: gas, TimeoutHeight: timeout, Msg: msg}
	signed, err := s.w.cfg.RequestSignature(ctx, req)
	if err != nil {
		return false, s.signRefused(ctx, work.ID, err)
	}
	if err := s.checkSigned(signed, req); err != nil {
		return false, s.integrity(ctx, "SIGNED_TX_MISMATCH", work.ID, err)
	}
	faultpoint.Hit(faultpoint.AfterSignBeforePersist)
	res, err := s.ss.PersistAttempt(ctx, store.AttemptDraft{IntentID: work.ID, AccountNumber: acct, Sequence: seq,
		TimeoutHeight: timeout, Gas: gas, TxBytes: signed.Bytes, TxHash: signed.Hash})
	if err != nil {
		return false, err
	}
	if res.Refusal != store.RefusedNone {
		s.w.log.Info("worker attempt discarded before sending", "intent_id", work.ID, "refusal", string(res.Refusal))
		return false, nil
	}
	faultpoint.Hit(faultpoint.AfterPersistCommit)
	s.w.log.Info("worker attempt persisted", "intent_id", work.ID, "attempt_id", res.AttemptID, "sequence", seq,
		"timeout_height", timeout, "gas", gas)
	return true, s.send(ctx, res.AttemptID, signed.Bytes)
}

// allocate returns the account number and next sequence of the key signer
// for a new attempt (plan 8.1), after storing what the chain shows; ok is
// false when the signer must wait (a stale node, an acc_seq search not
// indexed yet) or is halted.
func (s *session) allocate(ctx context.Context) (acct, seq uint64, ok bool, err error) {
	row, err := s.w.st.GetSigner(ctx, s.key.Address)
	if err != nil || row.HaltedReason != "" {
		return 0, 0, false, err
	}
	if row.ObservedHeight > 1<<62 {
		return 0, 0, false, errors.New("worker: observed height out of range")
	}
	a, err := s.w.cfg.Chain.Account(ctx, s.key.Address, int64(row.ObservedHeight))
	var stale *chain.StaleError
	if errors.As(err, &stale) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if row.AccountNumber != nil && *row.AccountNumber != a.Number {
		return 0, 0, false, s.halted(ctx, "ACCOUNT_NUMBER_CHANGED", "")
	}
	if row.NextSequence != nil && a.Sequence > *row.NextSequence {
		if a.Sequence-*row.NextSequence > maxGapCheck {
			return 0, 0, false, s.halted(ctx, "SEQUENCE_GAP", "")
		}
		for n := *row.NextSequence; n < a.Sequence; n++ {
			clear, err := s.sequenceUsedByUs(ctx, n)
			if err != nil || !clear {
				return 0, 0, false, err
			}
		}
	}
	err = s.ss.SyncSequence(ctx, a.Number, a.Sequence, uint64(a.Height))
	if errors.Is(err, store.ErrStaleObservation) || errors.Is(err, store.ErrSignerBusy) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return a.Number, a.Sequence, true, nil
}

// sequenceUsedByUs checks the transaction that consumed sequence n of the
// key signer: one of our attempts, or a foreign use an operator
// acknowledged, is clear; an unacknowledged foreign use halts the signer
// with SIGNER_CONFLICT and a signer_events row (DR-11-15). It is not clear,
// and nothing changes, while the search does not find the transaction yet.
func (s *session) sequenceUsedByUs(ctx context.Context, n uint64) (bool, error) {
	hit, found, err := s.w.cfg.Chain.SequenceUser(ctx, s.key.Address, n)
	if err != nil {
		return false, s.integrityOr(ctx, err, "")
	}
	if !found {
		return false, nil
	}
	_, err = s.w.st.AttemptByHash(ctx, hit.Hash)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	err = s.ss.HaltForConflict(ctx, n, hit.Hash)
	if errors.Is(err, store.ErrConflictCleared) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	s.w.alert("SIGNER_HALTED", "signer", s.key.Name, "reason", "SIGNER_CONFLICT", "sequence", n)
	return false, nil
}

// earlierTimeouts are the timeout heights a new attempt at seq must exceed:
// the highest of every stored attempt at that sequence, and, when the
// intent's latest attempt died by (d2) with sdk code 4, that attempt's
// timeout height plus Sdk4RetryGapBlocks - 1, so the new attempt is built
// at least that many blocks after it (DR-11-18).
func (s *session) earlierTimeouts(ctx context.Context, seq uint64, atts []store.Attempt) ([]uint64, error) {
	var out []uint64
	top, found, err := s.w.st.MaxTimeoutAt(ctx, s.key.Address, seq)
	if err != nil {
		return nil, err
	}
	if found {
		out = append(out, top)
	}
	if n := len(atts); n > 0 && sdk4Death(atts[n-1]) {
		out = append(out, atts[n-1].TimeoutHeight+outbox.Sdk4RetryGapBlocks-1)
	}
	return out, nil
}

// gasFor returns the declared gas of a new attempt: the kind's configured
// gas, or for a withdrawal, whose gas grows with the content's open offers,
// the gas the node measured for the broadcaster's simulation transaction
// (no key, an empty signature) plus the configured margin. When the node
// rejects the simulation the transaction would fail anyway; the declared
// gas is used and the chain's committed result decides.
func (s *session) gasFor(ctx context.Context, kind policy.Kind, msg sdk.Msg, seq, timeout uint64) (uint64, error) {
	declared := s.w.cfg.Numbers.DeclaredGas[kind]
	if kind != policy.KindWithdrawContent {
		return declared, nil
	}
	raw, err := s.w.cfg.Chain.SimulationTx(msg, s.key.PubKey, seq, timeout)
	if err != nil {
		return 0, err
	}
	sim, err := s.w.cfg.Chain.Simulate(ctx, raw)
	var rejected *chain.QueryError
	if errors.As(err, &rejected) {
		s.w.log.Info("worker simulation rejected; using the declared gas", "kind", string(kind), "code", rejected.Code)
		return declared, nil
	}
	if err != nil {
		return 0, err
	}
	gas, err := chain.GasWithMargin(sim.GasUsed, s.w.cfg.Numbers.WithdrawMarginPercent)
	if err != nil {
		return declared, nil
	}
	return gas, nil
}

// checkSigned decodes the signer's transaction again (plan 8.2) and checks
// it carries exactly the requested message, the key as signer, and the
// requested sequence, gas and timeout height.
func (s *session) checkSigned(signed signer.Signed, req signer.SignRequest) error {
	decoded, err := s.w.cfg.Chain.Decode(signed.Bytes)
	if err != nil {
		return err
	}
	own, err := decoded.Own()
	if err != nil {
		return err
	}
	want, ok1 := req.Msg.(proto.Message)
	got, ok2 := own.Msg.Msg.(proto.Message)
	switch {
	case !ok1 || !ok2 || !proto.Equal(want, got):
		return errors.New("worker: signed message differs from the request")
	case !bytes.Equal(own.Signer.Address, s.key.Address) || own.Signer.Sequence != req.Sequence:
		return errors.New("worker: signed transaction has another signer or sequence")
	case decoded.Gas != req.Gas || decoded.TimeoutHeight != req.TimeoutHeight:
		return errors.New("worker: signed transaction has another gas or timeout height")
	}
	return nil
}

// signRefused handles a failed signature request. A refusal of the message
// itself (its type, signer, authority_end, hosting intent or registry
// entry) fails the intent's validation (T2). Any other refusal (no fresh
// height, a chain id, gas or timeout bound, an unknown key, an overloaded
// signer) and an unreachable signer leave the intent REQUESTED: nothing was
// signed or stored, and the next pass asks again (plan 13.4 phase SG).
func (s *session) signRefused(ctx context.Context, intentID string, err error) error {
	switch code := policy.CodeOf(err); code {
	case policy.CodeTypeNotAllowed, policy.CodeSignerMismatch, policy.CodeAuthorityEnd, policy.CodeIntentKeyUnregistered,
		policy.CodeIntentSignature, policy.CodeInvalid:
		reason := reasonOf(err)
		s.w.log.Info("worker signer refused the message", "intent_id", intentID, "code", string(code), "reason", reason)
		return ignoreStateChanged(s.w.st.FailValidation(ctx, intentID, reason))
	case "":
		return fmt.Errorf("worker: signer request: %w", err)
	default:
		s.w.alert("SIGNER_REFUSED", "intent_id", intentID, "code", string(code))
		return nil
	}
}
