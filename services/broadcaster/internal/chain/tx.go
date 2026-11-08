package chain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"

	"cosmossdk.io/core/address"
	"github.com/cosmos/cosmos-sdk/client"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/proto"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// Bounds of the timeout_height offset K (plan section 11, PROPOSED,
// DR-11-10): the hard bounds in code, not the configured value.
const (
	MinTimeoutOffset = 5
	MaxTimeoutOffset = 1000
)

// MaxEarlierAttempts bounds the earlier timeout heights NextTimeoutHeight
// reads: the hard upper bound of attempts per intent (plan section 11,
// PROPOSED).
const MaxEarlierAttempts = 32

// svnTypeURLPrefix starts the type URL of every svn message. The
// application also registers SDK auth messages, which the broadcaster
// never builds.
const svnTypeURLPrefix = "/svn."

// MaxGasMarginPercent is the upper bound of the withdraw simulation margin
// (plan section 11, PROPOSED).
const MaxGasMarginPercent = 100

// Codec builds, signs-for and decodes transactions of one chain with the
// application's own encoding (app.MakeEncoding): SIGN_MODE_DIRECT, the
// strict body decoder, and the custom one-signer rule of every svn message.
// It holds no key and does no network I/O.
type Codec struct {
	enc     app.Encoding
	chainID string
	bech    address.Codec
}

// NewCodec returns the codec of the chain chainID. The chain id is
// operator configuration; it is never read from a node.
func NewCodec(chainID string) (*Codec, error) {
	if chainID == "" || len(chainID) > genesis.MaxChainIDLen {
		return nil, fmt.Errorf("chain: chain id must be 1 to %d bytes", genesis.MaxChainIDLen)
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		return nil, fmt.Errorf("chain: encoding: %w", err)
	}
	return &Codec{enc: enc, chainID: chainID, bech: addresscodec.NewBech32Codec(chainaddress.Account)}, nil
}

// ChainID returns the configured chain id.
func (c *Codec) ChainID() string { return c.chainID }

// Bech32 returns the account address string of a 20-byte address.
func (c *Codec) Bech32(addr []byte) (string, error) {
	if len(addr) != genesis.AccountBytes {
		return "", fmt.Errorf("chain: address must be %d bytes", genesis.AccountBytes)
	}
	return c.bech.BytesToString(addr)
}

// TxInput is what an outer transaction needs besides its message and the
// signer's public key.
type TxInput struct {
	AccountNumber uint64
	Sequence      uint64
	// Gas is the declared gas limit, genesis.MinGasPerTx to
	// genesis.MaxGasPerTx (the ante's bounds outside simulation).
	Gas uint64
	// TimeoutHeight is the last height whose block may include the
	// transaction (SDK TxTimeoutHeightDecorator). It must be at least 1:
	// every attempt carries a deadline (DR-11-05).
	TimeoutHeight uint64
}

// Unsigned is one outer transaction prepared for exactly one signature by
// the key whose public key it names. It is not safe for concurrent use.
type Unsigned struct {
	codec     *Codec
	builder   client.TxBuilder
	msg       sdk.Msg
	pub       cryptotypes.PubKey
	in        TxInput
	signBytes []byte
}

// Prepare builds the outer transaction that carries msg as its only
// message: SIGN_MODE_DIRECT, no fee, no memo, no fee payer or granter, no
// timeout timestamp, ordered, the declared gas and timeout height of in,
// and pub's address as the only signer, which must be msg's signer. It
// computes the SIGN_MODE_DIRECT sign bytes for the configured chain id.
// It signs nothing.
func (c *Codec) Prepare(msg sdk.Msg, pub cryptotypes.PubKey, in TxInput) (*Unsigned, error) {
	if in.Gas < genesis.MinGasPerTx || in.Gas > genesis.MaxGasPerTx {
		return nil, fmt.Errorf("chain: gas limit must be %d to %d", genesis.MinGasPerTx, genesis.MaxGasPerTx)
	}
	if in.TimeoutHeight < 1 || in.TimeoutHeight > math.MaxInt64 {
		return nil, errors.New("chain: timeout height must be 1 to 2^63-1")
	}
	builder, msg, bech, err := c.builder(msg, pub, in.Sequence, in.Gas, in.TimeoutHeight)
	if err != nil {
		return nil, err
	}
	signer := authsigning.SignerData{
		Address: bech, ChainID: c.chainID, AccountNumber: in.AccountNumber, Sequence: in.Sequence, PubKey: pub,
	}
	signBytes, err := authsigning.GetSignBytesAdapter(context.Background(), c.enc.TxConfig.SignModeHandler(),
		signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("chain: sign bytes: %w", err)
	}
	return &Unsigned{codec: c, builder: builder, msg: msg, pub: pub, in: in, signBytes: signBytes}, nil
}

// builder copies msg, then sets the copy, the gas limit, the timeout
// height, and a signer info with pub, SIGN_MODE_DIRECT and sequence but no
// signature. It returns the builder, the copy (so a later change to the
// caller's message cannot reach the prepared transaction), and pub's
// bech32 address.
func (c *Codec) builder(msg sdk.Msg, pub cryptotypes.PubKey, sequence, gas, timeoutHeight uint64) (client.TxBuilder, sdk.Msg, string, error) {
	if msg == nil || pub == nil {
		return nil, nil, "", errors.New("chain: nil message or public key")
	}
	typeURL := sdk.MsgTypeURL(msg)
	if !strings.HasPrefix(typeURL, svnTypeURLPrefix) {
		return nil, nil, "", fmt.Errorf("chain: message type %s is not an svn message", typeURL)
	}
	v2, ok := msg.(proto.Message)
	if !ok {
		return nil, nil, "", fmt.Errorf("chain: message %T is not a protobuf message", msg)
	}
	msg, ok = proto.Clone(v2).(sdk.Msg)
	if !ok {
		return nil, nil, "", fmt.Errorf("chain: message %T does not copy", v2)
	}
	if _, err := c.enc.InterfaceRegistry.Resolve(typeURL); err != nil {
		return nil, nil, "", fmt.Errorf("chain: message type %s is not registered by the application", typeURL)
	}
	signers, _, err := c.enc.Codec.GetMsgV1Signers(msg)
	if err != nil {
		return nil, nil, "", fmt.Errorf("chain: message signer: %w", err)
	}
	if len(signers) != 1 || len(signers[0]) != genesis.AccountBytes {
		return nil, nil, "", errors.New("chain: message must name exactly one 20-byte signer")
	}
	if !bytes.Equal(pub.Address(), signers[0]) {
		return nil, nil, "", errors.New("chain: public key address is not the message signer")
	}
	bech, err := c.Bech32(signers[0])
	if err != nil {
		return nil, nil, "", err
	}
	builder := c.enc.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		return nil, nil, "", fmt.Errorf("chain: set message: %w", err)
	}
	builder.SetGasLimit(gas)
	builder.SetTimeoutHeight(timeoutHeight)
	info := signingtypes.SignatureV2{
		PubKey:   pub,
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: sequence,
	}
	if err := builder.SetSignatures(info); err != nil {
		return nil, nil, "", fmt.Errorf("chain: signer info: %w", err)
	}
	return builder, msg, bech, nil
}

// SignBytes returns a copy of the SIGN_MODE_DIRECT bytes the signer signs.
func (u *Unsigned) SignBytes() []byte { return append([]byte(nil), u.signBytes...) }

// Assemble attaches signature and returns the encoded transaction and its
// CometBFT hash (SHA-256 of the bytes). The signature must verify against
// the named public key over SignBytes. The encoded bytes must be at most
// genesis.MaxTxBytes, and decoding them with the application's strict
// decoder must give back the same message, signer, sequence, gas and
// timeout height.
func (u *Unsigned) Assemble(signature []byte) ([]byte, [32]byte, error) {
	var none [32]byte
	if !u.pub.VerifySignature(u.signBytes, signature) {
		return nil, none, errors.New("chain: signature does not verify against the transaction's public key")
	}
	sig := signingtypes.SignatureV2{
		PubKey:   u.pub,
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT, Signature: signature},
		Sequence: u.in.Sequence,
	}
	if err := u.builder.SetSignatures(sig); err != nil {
		return nil, none, fmt.Errorf("chain: set signature: %w", err)
	}
	raw, err := u.codec.enc.TxConfig.TxEncoder()(u.builder.GetTx())
	if err != nil {
		return nil, none, fmt.Errorf("chain: encode: %w", err)
	}
	if uint64(len(raw)) > genesis.MaxTxBytes {
		return nil, none, fmt.Errorf("chain: transaction of %d bytes above %d", len(raw), genesis.MaxTxBytes)
	}
	decoded, err := u.codec.Decode(raw)
	if err != nil {
		return nil, none, err
	}
	own, err := decoded.Own()
	if err != nil {
		return nil, none, err
	}
	if !equalMsg(own.Msg.Msg, u.msg) || !bytes.Equal(own.Signer.Address, u.pub.Address()) ||
		own.Signer.Sequence != u.in.Sequence || decoded.Gas != u.in.Gas || decoded.TimeoutHeight != u.in.TimeoutHeight {
		return nil, none, errors.New("chain: decoded transaction differs from the prepared one")
	}
	return raw, sha256.Sum256(raw), nil
}

// SimulationTx builds the transaction a node simulates to measure gas:
// msg with pub, sequence and timeoutHeight as Prepare would set them, a
// declared gas of 0 (accepted only in simulation, chain/app/ante.go
// boundsDecorator), and an empty signature (simulation skips signature
// verification, SDK x/auth/ante sigverify.go). It needs no key. In
// simulation the SDK's tx-size decorator charges an estimated signature
// for the empty one (SDK x/auth/ante basic.go ConsumeTxSizeGasDecorator);
// the bytes of the declared gas field, omitted at 0, are not charged, and
// the caller's margin covers them.
func (c *Codec) SimulationTx(msg sdk.Msg, pub cryptotypes.PubKey, sequence, timeoutHeight uint64) ([]byte, error) {
	if timeoutHeight < 1 || timeoutHeight > math.MaxInt64 {
		return nil, errors.New("chain: timeout height must be 1 to 2^63-1")
	}
	builder, _, _, err := c.builder(msg, pub, sequence, 0, timeoutHeight)
	if err != nil {
		return nil, err
	}
	raw, err := c.enc.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("chain: encode: %w", err)
	}
	if uint64(len(raw)) > genesis.MaxTxBytes {
		return nil, fmt.Errorf("chain: transaction of %d bytes above %d", len(raw), genesis.MaxTxBytes)
	}
	return raw, nil
}

// DecodedMsg is one message of a decoded transaction.
type DecodedMsg struct {
	Msg     sdk.Msg
	TypeURL string
	// Signer is the message's only signer (the app's custom signer rule).
	Signer []byte
}

// SignerInfo is one signer of a decoded transaction.
type SignerInfo struct {
	Address  []byte
	PubKey   cryptotypes.PubKey
	Sequence uint64
}

// DecodedTx is a transaction decoded with the application's strict
// decoder. It reports the envelope as encoded; it does not judge it.
type DecodedTx struct {
	Msgs             []DecodedMsg
	Signers          []SignerInfo
	Gas              uint64
	TimeoutHeight    uint64
	TimeoutTimestamp bool
	Unordered        bool
	Memo             string
	FeeZero          bool
	FeePayer         string
	FeeGranter       string
}

// protoTxProvider is the SDK transaction wrapper's access to its decoded
// protobuf form, whose getters are nil-safe.
type protoTxProvider interface {
	GetProtoTx() *txtypes.Tx
}

// Decode decodes raw with the application's strict decoder (sdk code 2
// on any unknown field). raw must be 1 to genesis.MaxTxBytes bytes. Bytes
// read from a node are untrusted: the SDK accessors assume a well-formed
// envelope, so a panic inside them is returned as a decode error.
func (c *Codec) Decode(raw []byte) (out DecodedTx, err error) {
	if len(raw) == 0 || uint64(len(raw)) > genesis.MaxTxBytes {
		return DecodedTx{}, fmt.Errorf("chain: transaction must be 1 to %d bytes", genesis.MaxTxBytes)
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = DecodedTx{}, fmt.Errorf("chain: decode transaction: %v", r)
		}
	}()
	tx, err := c.enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		return DecodedTx{}, fmt.Errorf("chain: decode transaction: %w", err)
	}
	full, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return DecodedTx{}, fmt.Errorf("chain: decoded transaction %T lacks the SDK signer interface", tx)
	}
	provider, ok := tx.(protoTxProvider)
	if !ok {
		return DecodedTx{}, fmt.Errorf("chain: decoded transaction %T lacks its protobuf form", tx)
	}
	body, info := provider.GetProtoTx().GetBody(), provider.GetProtoTx().GetAuthInfo()
	fee := info.GetFee()
	out = DecodedTx{
		Gas:              fee.GetGasLimit(),
		TimeoutHeight:    body.GetTimeoutHeight(),
		TimeoutTimestamp: body.GetTimeoutTimestamp() != nil,
		Unordered:        body.GetUnordered(),
		Memo:             body.GetMemo(),
		FeeZero:          fee.GetAmount().IsZero(),
		FeePayer:         fee.GetPayer(),
		FeeGranter:       fee.GetGranter(),
	}
	for _, msg := range full.GetMsgs() {
		signers, _, err := c.enc.Codec.GetMsgV1Signers(msg)
		if err != nil || len(signers) != 1 {
			return DecodedTx{}, fmt.Errorf("chain: message %s does not name exactly one signer", sdk.MsgTypeURL(msg))
		}
		out.Msgs = append(out.Msgs, DecodedMsg{Msg: msg, TypeURL: sdk.MsgTypeURL(msg), Signer: append([]byte(nil), signers[0]...)})
	}
	addrs, err := full.GetSigners()
	if err != nil {
		return DecodedTx{}, fmt.Errorf("chain: transaction signers: %w", err)
	}
	sigs, err := full.GetSignaturesV2()
	if err != nil {
		return DecodedTx{}, fmt.Errorf("chain: transaction signatures: %w", err)
	}
	if len(sigs) != len(addrs) {
		return DecodedTx{}, fmt.Errorf("chain: %d signatures for %d signers", len(sigs), len(addrs))
	}
	for i, sig := range sigs {
		out.Signers = append(out.Signers, SignerInfo{Address: append([]byte(nil), addrs[i]...), PubKey: sig.PubKey, Sequence: sig.Sequence})
	}
	return out, nil
}

// OwnTx is the shape of every transaction the broadcaster builds: one
// message, one signer.
type OwnTx struct {
	Msg    DecodedMsg
	Signer SignerInfo
}

// Own returns the only message and signer of d. It refuses any other shape
// the broadcaster never builds: more than one message or signer, a signer
// that is not the message signer, a memo, a fee, a fee payer or granter, a timeout
// timestamp, an unordered transaction, or no timeout height.
func (d DecodedTx) Own() (OwnTx, error) {
	switch {
	case len(d.Msgs) != 1 || len(d.Signers) != 1:
		return OwnTx{}, fmt.Errorf("chain: transaction has %d messages and %d signers, want one of each", len(d.Msgs), len(d.Signers))
	case !bytes.Equal(d.Msgs[0].Signer, d.Signers[0].Address):
		return OwnTx{}, errors.New("chain: transaction signer is not the message signer")
	case d.Memo != "" || !d.FeeZero || d.FeePayer != "" || d.FeeGranter != "":
		return OwnTx{}, errors.New("chain: transaction carries a memo, a fee, a fee payer, or a fee granter")
	case d.TimeoutTimestamp || d.Unordered:
		return OwnTx{}, errors.New("chain: transaction sets a timeout timestamp or is unordered")
	case d.TimeoutHeight < 1:
		return OwnTx{}, errors.New("chain: transaction has no timeout height")
	}
	return OwnTx{Msg: d.Msgs[0], Signer: d.Signers[0]}, nil
}

// equalMsg compares two svn messages field by field (protobuf equality).
// Both must be generated with google.golang.org/protobuf, as every
// registered svn message is.
func equalMsg(a, b sdk.Msg) bool {
	av, aok := a.(proto.Message)
	bv, bok := b.(proto.Message)
	return aok && bok && proto.Equal(av, bv)
}

// NextTimeoutHeight returns the timeout height of a new attempt: latest
// committed height plus offset, which must also be strictly above the
// timeout height of every earlier attempt with the same signer and
// sequence (plan 8.2). Signing is deterministic, so an equal timeout
// height would give the same bytes as an earlier attempt. When latest plus
// offset is not above every earlier timeout height, ready is false and the
// caller waits for a later block rather than reaching further ahead.
func NextTimeoutHeight(latest int64, offset uint64, earlier []uint64) (timeout uint64, ready bool, err error) {
	if latest < 1 {
		return 0, false, errors.New("chain: latest height must be at least 1")
	}
	if offset < MinTimeoutOffset || offset > MaxTimeoutOffset {
		return 0, false, fmt.Errorf("chain: timeout offset must be %d to %d", MinTimeoutOffset, MaxTimeoutOffset)
	}
	if len(earlier) > MaxEarlierAttempts {
		return 0, false, fmt.Errorf("chain: more than %d earlier attempts", MaxEarlierAttempts)
	}
	if uint64(latest) > math.MaxInt64-offset {
		return 0, false, errors.New("chain: timeout height overflows")
	}
	timeout = uint64(latest) + offset
	for _, t := range earlier {
		if t >= timeout {
			return 0, false, nil
		}
	}
	return timeout, true, nil
}

// GasWithMargin returns the declared gas for a simulated gas use: the
// simulated gas plus marginPercent of it (0 to MaxGasMarginPercent),
// capped at genesis.MaxGasPerTx. A simulated gas of 0 is refused, and so
// is a simulated gas above genesis.MaxGasPerTx: no declared limit can cover
// it, so the transaction could only fail out of gas.
func GasWithMargin(simulated, marginPercent uint64) (uint64, error) {
	if simulated < 1 || simulated > genesis.MaxGasPerTx {
		return 0, fmt.Errorf("chain: simulated gas must be 1 to %d", genesis.MaxGasPerTx)
	}
	if marginPercent > MaxGasMarginPercent {
		return 0, fmt.Errorf("chain: gas margin must be 0 to %d percent", MaxGasMarginPercent)
	}
	gas := simulated + simulated*marginPercent/100
	if gas > genesis.MaxGasPerTx {
		gas = genesis.MaxGasPerTx
	}
	return gas, nil
}
