package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// NewAnteHandler chains the SDK signature and sequence decorators with the
// beta bounds. Fee amounts are rejected. There is no coin denomination.
func NewAnteHandler(ak authante.AccountKeeper, signatures *txsigning.HandlerMap, admitted func(ctx sdk.Context, addr []byte) bool) (sdk.AnteHandler, error) {
	if ak == nil || signatures == nil || admitted == nil {
		return nil, svnerr.ErrMalformed.Wrap("ante dependencies")
	}
	return sdk.ChainAnteDecorators(
		authante.NewSetUpContextDecorator(),
		authante.NewExtensionOptionsDecorator(nil),
		authante.NewValidateBasicDecorator(),
		authante.NewTxTimeoutHeightDecorator(),
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
