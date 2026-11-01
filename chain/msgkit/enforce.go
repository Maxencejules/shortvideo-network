package msgkit

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// FixedMessageGas is charged by Enforce for every registry message, on top
// of the metered store gas.
const FixedMessageGas uint64 = 50_000

// Enforce is the first check of every registry handler. It mirrors the
// authority handlers:
//
//   - gas limit outside [genesis.MinGasPerTx, genesis.MaxGasPerTx]: BOUNDS
//   - transaction bytes above genesis.MaxTxBytes: BOUNDS
//   - consume FixedMessageGas (an out-of-gas panic aborts the transaction)
//   - signer not 20 bytes: MALFORMED
//
// The ante handler has already verified the outer signature; Enforce does
// not verify it again.
func Enforce(ctx sdk.Context, signer []byte) Code {
	limit := ctx.GasMeter().Limit()
	if limit < genesis.MinGasPerTx || limit > genesis.MaxGasPerTx {
		return Bounds
	}
	if uint64(len(ctx.TxBytes())) > genesis.MaxTxBytes {
		return Bounds
	}
	ctx.GasMeter().ConsumeGas(FixedMessageGas, "svn registry message")
	if len(signer) != AccountBytes {
		return Malformed
	}
	return OK
}
