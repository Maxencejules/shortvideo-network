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
//   - gas meter limit outside [genesis.MinGasPerTx, genesis.MaxGasPerTx]:
//     BOUNDS, except in simulation (see below)
//   - transaction bytes above genesis.MaxTxBytes: BOUNDS
//   - consume FixedMessageGas (an out-of-gas panic aborts the transaction)
//   - signer not 20 bytes: MALFORMED
//
// Simulation (sdk.ExecModeSimulate, the /cosmos.tx.v1beta1.Service/Simulate
// path) runs on the SDK's infinite gas meter, whose limit is 2^64-1, so the
// meter check would reject every simulated message and no client could
// estimate gas. In that mode only the meter check is skipped; the ante
// handler still checks the declared gas limit of the simulated
// transaction (a declared 0 is accepted only in simulation), and the size, gas charge, and signer checks still run. The
// SDK runs message handlers only in simulation and in FinalizeBlock, so the
// meter check runs for every message that can commit.
//
// The ante handler has already verified the outer signature; Enforce does
// not verify it again.
func Enforce(ctx sdk.Context, signer []byte) Code {
	if ctx.ExecMode() != sdk.ExecModeSimulate {
		limit := ctx.GasMeter().Limit()
		if limit < genesis.MinGasPerTx || limit > genesis.MaxGasPerTx {
			return Bounds
		}
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
