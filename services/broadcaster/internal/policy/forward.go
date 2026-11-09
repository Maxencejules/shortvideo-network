package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
)

// Forward window bounds (plan section 11, PROPOSED: the forward window in
// blocks).
const (
	MinForwardWindow = 5
	MaxForwardWindow = 1000
)

// ForwardReader is the chain state the forward check reads
// (*chain.Client): the relay reads plus the signer's committed account.
type ForwardReader interface {
	Reader
	Account(ctx context.Context, addr []byte, minHeight int64) (chain.Account, error)
}

// ForwardInput is one provider-signed transaction to forward verbatim
// (plan 7.3 P3).
type ForwardInput struct {
	// Raw is the transaction exactly as the provider signed it.
	Raw []byte
	// ProviderID is the provider the transaction acts for; ContentID
	// locates the held lease of a renew or end (see RelayInput).
	ProviderID string
	ContentID  string
	// ObservedHeight is the height of the latest header the caller saw
	// (chain.Status.Height): the forward window is counted from it.
	ObservedHeight int64
	// MinHeight is the read floor (chain.Status.AppFloor of that header,
	// 1 to ObservedHeight): every read must be answered at it or later.
	MinHeight int64
	// ChainTime is the time of the header at ObservedHeight (see
	// RelayInput.ChainTime).
	ChainTime time.Time
	// Window is the forward window (MinForwardWindow to MaxForwardWindow):
	// the timeout height must be above ObservedHeight and at most
	// ObservedHeight + Window.
	Window uint64
	// DeadlinePages is the Query/Deadlines page budget for an accept.
	DeadlinePages int
	// Roles is the role view the outer signer's ADMITTED_PROVIDER role is
	// read from.
	Roles RoleView
}

// Forwarded is a transaction that passed CheckForwardedTx. The outbox
// tracks it by Hash and allocates no sequence for it.
type Forwarded struct {
	Hash          [32]byte
	TypeURL       string
	Msg           sdk.Msg
	Signer        []byte
	Sequence      uint64
	AccountNumber uint64
	TimeoutHeight uint64
	Relay         RelayResult
}

var encoding = sync.OnceValues(app.MakeEncoding)

// CheckForwardedTx runs the checks of plan 7.3 P3 on bytes the broadcaster
// would forward without editing them:
//
//  1. strict decode with the application's decoder; exactly one message
//     and one signer, the message's signer is the transaction's signer, no
//     memo, fee, fee payer, granter, timeout timestamp or unordered flag
//     (FORWARD_SHAPE);
//  2. the message is the provider's own MsgAcceptLease, MsgRenewLease or
//     MsgEndLease without authority_end (TYPE_NOT_ALLOWED, AUTHORITY_END);
//  3. a timeout height above ObservedHeight and at most ObservedHeight +
//     Window: bytes without a deadline could commit at any later time and
//     are refused (FORWARD_DEADLINE);
//  4. the outer signer has the ADMITTED_PROVIDER role and is the account
//     the provider record of ProviderID names (FORWARD_SIGNER);
//  5. the inner provider signature passes PrecheckRelay, and it is for
//     ProviderID;
//  6. the outer SIGN_MODE_DIRECT signature verifies for the configured
//     chain id (the codec's) and the signer's committed account number,
//     with a public key whose address is the signer, so bytes signed for
//     another chain or account are refused (FORWARD_OUTER_SIGNATURE);
//  7. the transaction's sequence is not below the committed account
//     sequence (FORWARD_SEQUENCE_USED).
//
// The bytes are never edited and nothing is signed. A read failure is an
// error that is not a Refusal.
func CheckForwardedTx(ctx context.Context, codec *chain.Codec, r ForwardReader, in ForwardInput) (Forwarded, error) {
	if codec == nil || r == nil || in.Roles == nil {
		return Forwarded{}, errors.New("policy: forward check needs a codec, a reader and a role view")
	}
	if in.Window < MinForwardWindow || in.Window > MaxForwardWindow {
		return Forwarded{}, fmt.Errorf("policy: forward window must be %d to %d blocks", MinForwardWindow, MaxForwardWindow)
	}
	if in.ObservedHeight < 1 || uint64(in.ObservedHeight) > math.MaxInt64-in.Window {
		return Forwarded{}, errors.New("policy: observed height out of range")
	}
	if in.MinHeight < 1 || in.MinHeight > in.ObservedHeight {
		return Forwarded{}, errors.New("policy: read floor must be 1 to the observed height")
	}
	if !msgkit.ValidID(in.ProviderID) {
		return Forwarded{}, refuse(CodeInvalid, "provider id outside the id pattern")
	}
	decoded, err := codec.Decode(in.Raw)
	if err != nil {
		return Forwarded{}, refuse(CodeForwardShape, "not a transaction the application decodes strictly")
	}
	own, err := decoded.Own()
	if err != nil {
		if len(decoded.Msgs) == 1 && len(decoded.Signers) == 1 && decoded.TimeoutHeight == 0 {
			return Forwarded{}, refuse(CodeForwardDeadline, "transaction has no timeout height")
		}
		return Forwarded{}, refuse(CodeForwardShape, "transaction is not one message from one signer with a bare envelope")
	}
	if !contains(RelayTypeURLs(), own.Msg.TypeURL) {
		return Forwarded{}, refuse(CodeTypeNotAllowed, "forwarded message %s is not a provider lease message", own.Msg.TypeURL)
	}
	if end, ok := own.Msg.Msg.(*leasesv1.MsgEndLease); ok && end.GetAuthorityEnd() {
		return Forwarded{}, refuse(CodeAuthorityEnd, "EndLease with authority_end is not forwarded")
	}
	if decoded.TimeoutHeight <= uint64(in.ObservedHeight) || decoded.TimeoutHeight > uint64(in.ObservedHeight)+in.Window {
		return Forwarded{}, refuse(CodeForwardDeadline, "timeout height is not within the forward window of %d blocks", in.Window)
	}
	signer := own.Signer.Address
	if role := in.Roles.RoleOf(signer); role != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER {
		return Forwarded{}, refuse(CodeForwardSigner, "outer signer has role %s, not ADMITTED_PROVIDER", role)
	}
	rec, found, height, err := r.Provider(ctx, in.ProviderID, in.MinHeight)
	if err != nil {
		return Forwarded{}, fmt.Errorf("policy: read provider: %w", err)
	}
	if !found || !bytes.Equal(rec.GetAccount(), signer) {
		return Forwarded{}, refuse(CodeForwardSigner, "outer signer is not the account of the named provider")
	}
	relay, err := PrecheckRelay(ctx, r, RelayInput{
		Msg: own.Msg.Msg, ProviderID: in.ProviderID, ContentID: in.ContentID, MinHeight: height,
		ChainTime: in.ChainTime, DeadlinePages: in.DeadlinePages,
	})
	if err != nil {
		return Forwarded{}, err
	}
	if relay.ProviderID != in.ProviderID {
		return Forwarded{}, refuse(CodeForwardSigner, "the message acts for another provider")
	}
	account, err := r.Account(ctx, signer, height)
	if err != nil {
		return Forwarded{}, fmt.Errorf("policy: read signer account: %w", err)
	}
	if err := verifyOuter(codec, in.Raw, signer, own.Signer.Sequence, account.Number); err != nil {
		return Forwarded{}, err
	}
	if own.Signer.Sequence < account.Sequence {
		return Forwarded{}, refuse(CodeForwardSequence, "transaction sequence is below the committed account sequence")
	}
	return Forwarded{
		Hash: sha256.Sum256(in.Raw), TypeURL: own.Msg.TypeURL, Msg: own.Msg.Msg, Signer: bytes.Clone(signer),
		Sequence: own.Signer.Sequence, AccountNumber: account.Number, TimeoutHeight: decoded.TimeoutHeight, Relay: relay,
	}, nil
}

// verifyOuter checks the transaction's only signature: SIGN_MODE_DIRECT,
// by a public key whose address is signer, over the sign bytes for the
// codec's chain id, accountNumber and sequence.
func verifyOuter(codec *chain.Codec, raw, signer []byte, sequence, accountNumber uint64) error {
	enc, err := encoding()
	if err != nil {
		return fmt.Errorf("policy: encoding: %w", err)
	}
	tx, err := enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		return refuse(CodeForwardShape, "transaction does not decode")
	}
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return refuse(CodeForwardShape, "transaction carries no signatures")
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil || len(sigs) != 1 {
		return refuse(CodeForwardShape, "transaction does not carry exactly one signature")
	}
	sig := sigs[0]
	data, ok := sig.Data.(*signingtypes.SingleSignatureData)
	if !ok || data.SignMode != signingtypes.SignMode_SIGN_MODE_DIRECT || len(data.Signature) == 0 {
		return refuse(CodeForwardSignature, "signature is not one SIGN_MODE_DIRECT signature")
	}
	if sig.PubKey == nil || !bytes.Equal(sig.PubKey.Address(), signer) || sig.Sequence != sequence {
		return refuse(CodeForwardSignature, "signer public key is missing or is not the signer's")
	}
	bech, err := codec.Bech32(signer)
	if err != nil || len(signer) != genesis.AccountBytes {
		return refuse(CodeForwardSignature, "signer address does not encode")
	}
	signBytes, err := authsigning.GetSignBytesAdapter(context.Background(), enc.TxConfig.SignModeHandler(),
		signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{Address: bech, ChainID: codec.ChainID(), AccountNumber: accountNumber, Sequence: sequence, PubKey: sig.PubKey}, tx)
	if err != nil {
		return refuse(CodeForwardSignature, "sign bytes do not build")
	}
	if !sig.PubKey.VerifySignature(signBytes, data.Signature) {
		return refuse(CodeForwardSignature, "outer signature does not verify for the configured chain id and the signer's account number")
	}
	return nil
}
