package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// NewAnteHandler chains the SDK signature and sequence decorators with the
// beta bounds. Fee amounts are rejected. There is no coin denomination.
// envelopeDecorator rejects the envelope fields protocol v0 does not use
// (memo, fee payer, fee granter, tip), so no submitter-chosen text rides in
// the transaction envelope.
func NewAnteHandler(ak authante.AccountKeeper, signatures *txsigning.HandlerMap, admitted func(ctx sdk.Context, addr []byte) bool) (sdk.AnteHandler, error) {
	if ak == nil || signatures == nil || admitted == nil {
		return nil, svnerr.ErrMalformed.Wrap("ante dependencies")
	}
	return sdk.ChainAnteDecorators(
		authante.NewSetUpContextDecorator(),
		authante.NewExtensionOptionsDecorator(nil),
		authante.NewValidateBasicDecorator(),
		authante.NewTxTimeoutHeightDecorator(),
		envelopeDecorator{},
		authante.NewValidateMemoDecorator(ak),
		boundsDecorator{},
		authante.NewConsumeGasForTxSizeDecorator(ak),
		feeRejectDecorator{},
		authante.NewSetPubKeyDecorator(ak),
		authante.NewValidateSigCountDecorator(ak),
		authante.NewSigGasConsumeDecorator(ak, authante.DefaultSigVerificationGasConsumer),
		authante.NewSigVerificationDecorator(ak, signatures),
		admittedDecorator{admitted: admitted},
		authante.NewIncrementSequenceDecorator(ak),
	), nil
}

// envelopeDecorator rejects the SDK envelope fields that protocol v0 does
// not use. Each one is free text or an address the chain would otherwise
// carry into a committed block without reading it:
//
//   - a non-empty memo: ERROR_CODE_BOUNDS (the memo bound is 0 bytes; the
//     SDK auth default of 256 characters would let a memo carry an email
//     address or a URL)
//   - a fee payer, a fee granter, or a tip: ERROR_CODE_FEE_REJECTED (there
//     are no fees)
//
// Unknown fields and extension options never reach the ante handler: the
// transaction decoder (strictTxDecoder) rejects them.
type envelopeDecorator struct{}

// protoTx is the SDK transaction wrapper's raw view.
type protoTx interface {
	GetProtoTx() *txtypes.Tx
}

func (envelopeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	wrapped, ok := tx.(protoTx)
	if !ok {
		return ctx, svnerr.ErrMalformed.Wrap("transaction has no protobuf envelope")
	}
	raw := wrapped.GetProtoTx()
	if raw == nil || raw.GetBody() == nil || raw.GetAuthInfo() == nil {
		return ctx, svnerr.ErrMalformed.Wrap("transaction envelope is incomplete")
	}
	if len(raw.GetBody().GetMemo()) != 0 {
		return ctx, svnerr.ErrBounds.Wrapf("memo is %d bytes, the bound is 0", len(raw.GetBody().GetMemo()))
	}
	fee := raw.GetAuthInfo().GetFee()
	if fee.GetPayer() != "" || fee.GetGranter() != "" {
		return ctx, svnerr.ErrFeeRejected.Wrap("fee payer and fee granter must be empty")
	}
	if raw.GetAuthInfo().GetTip() != nil { //nolint:staticcheck // the deprecated tip is rejected, not used
		return ctx, svnerr.ErrFeeRejected.Wrap("tip must be empty")
	}
	return next(ctx, tx, simulate)
}

type boundsDecorator struct{}

func (boundsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, svnerr.ErrMalformed.Wrap("transaction has no gas limit")
	}
	limit := feeTx.GetGas()
	if limit < genesis.MinGasPerTx || limit > genesis.MaxGasPerTx {
		return ctx, svnerr.ErrBounds.Wrapf("gas_limit %d", limit)
	}
	if uint64(len(ctx.TxBytes())) > genesis.MaxTxBytes {
		return ctx, svnerr.ErrBounds.Wrapf("tx bytes %d", len(ctx.TxBytes()))
	}
	return next(ctx, tx, simulate)
}

type feeRejectDecorator struct{}

func (feeRejectDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, svnerr.ErrMalformed.Wrap("transaction has no fee field")
	}
	if len(feeTx.GetFee()) != 0 {
		return ctx, svnerr.ErrFeeRejected.Wrap("fee amount must be empty")
	}
	return next(ctx, tx, simulate)
}

type admittedDecorator struct {
	admitted func(ctx sdk.Context, addr []byte) bool
}

func (d admittedDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, svnerr.ErrMalformed.Wrap("transaction has no signer")
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, svnerr.ErrMalformed.Wrap(err.Error())
	}
	if len(signers) == 0 {
		return ctx, svnerr.ErrUnauthorized.Wrap("missing signer")
	}
	for _, signer := range signers {
		if !d.admitted(ctx, signer) {
			return ctx, svnerr.ErrUnauthorized.Wrap("signer is not an admitted application account")
		}
	}
	return next(ctx, tx, simulate)
}
