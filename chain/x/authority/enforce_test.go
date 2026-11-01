package authority

import (
	"bytes"
	"testing"

	errorsmod "cosmossdk.io/errors"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// TestEnforceCommitted covers the first check of every authority handler in
// each execution mode. FinalizeBlock (and CheckTx) keep the gas-meter
// check: an infinite meter or a limit outside [MinGasPerTx, MaxGasPerTx] is
// BOUNDS before any gas is charged. Simulation skips only that check; the
// size, gas charge, and signer checks still run. The ante handler hides
// this check at app level (it rejects the same limits first), so this
// table is its evidence.
func TestEnforceCommitted(t *testing.T) {
	signer := bytes.Repeat([]byte{1}, 20)
	base, _ := msgkittest.NewContext(t, ModuleName)
	cases := []struct {
		name      string
		mode      sdk.ExecMode
		gasLimit  uint64
		infinite  bool
		txBytes   int
		signer    []byte
		want      string
		wantSpent uint64
	}{
		{"finalize ok", sdk.ExecModeFinalize, 200_000, false, 256, signer, "", 50_000},
		{"finalize on the infinite meter", sdk.ExecModeFinalize, 0, true, 256, signer, CodeBounds, 0},
		{"finalize gas limit 0", sdk.ExecModeFinalize, 0, false, 256, signer, CodeBounds, 0},
		{"finalize gas limit max", sdk.ExecModeFinalize, genesis.MaxGasPerTx, false, 256, signer, "", 50_000},
		{"finalize gas limit max+1", sdk.ExecModeFinalize, genesis.MaxGasPerTx + 1, false, 256, signer, CodeBounds, 0},
		{"finalize tx above the cap", sdk.ExecModeFinalize, 200_000, false, int(genesis.MaxTxBytes) + 1, signer, CodeBounds, 0},
		{"finalize signer 19 bytes", sdk.ExecModeFinalize, 200_000, false, 256, signer[:19], CodeMalformed, 50_000},
		{"finalize bounds before signer", sdk.ExecModeFinalize, 0, true, 256, nil, CodeBounds, 0},
		{"check on the infinite meter", sdk.ExecModeCheck, 0, true, 256, signer, CodeBounds, 0},
		{"simulate on the infinite meter", sdk.ExecModeSimulate, 0, true, 256, signer, "", 50_000},
		{"simulate tx at the cap", sdk.ExecModeSimulate, 0, true, int(genesis.MaxTxBytes), signer, "", 50_000},
		{"simulate tx above the cap", sdk.ExecModeSimulate, 0, true, int(genesis.MaxTxBytes) + 1, signer, CodeBounds, 0},
		{"simulate signer 19 bytes", sdk.ExecModeSimulate, 0, true, 256, signer[:19], CodeMalformed, 50_000},
	}
	for _, tc := range cases {
		meter := storetypes.NewGasMeter(tc.gasLimit)
		if tc.infinite {
			meter = storetypes.NewInfiniteGasMeter()
		}
		ctx := base.WithGasMeter(meter).WithTxBytes(make([]byte, tc.txBytes)).WithExecMode(tc.mode)
		got, panicked := runEnforceCommitted(ctx, tc.signer)
		if panicked != nil {
			t.Errorf("%s: panic %v, want %q", tc.name, panicked, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		if spent := ctx.GasMeter().GasConsumed(); spent != tc.wantSpent {
			t.Errorf("%s: spent %d gas, want %d", tc.name, spent, tc.wantSpent)
		}
	}
}

// runEnforceCommitted returns the code, or the panic value when the gas
// charge runs out of gas (a limit the meter check should have rejected).
func runEnforceCommitted(ctx sdk.Context, signer []byte) (code string, panicked any) {
	defer func() { panicked = recover() }()
	return enforceCommitted(ctx, signer), nil
}

// TestHandlerMeterCheckByMode drives one handler through the keeper: on the
// infinite meter, FinalizeBlock stops at the meter check (BOUNDS), while
// simulation passes it and reaches the engine, which reports the unknown
// proposal (NOT_FOUND). Neither writes to the store.
func TestHandlerMeterCheckByMode(t *testing.T) {
	e := newKeeperEnv(t)
	before := e.snapshot(ModuleName)
	for _, c := range []struct {
		mode sdk.ExecMode
		want *errorsmod.Error
	}{
		{sdk.ExecModeFinalize, svnerr.ErrBounds},
		{sdk.ExecModeSimulate, svnerr.ErrNotFound},
	} {
		ctx := e.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager()).
			WithTxBytes(make([]byte, 256)).WithExecMode(c.mode)
		_, err := e.keeper.Approve(ctx, approve(e.seats[0], "ap-missing", "no-such-proposal"))
		if !c.want.Is(err) {
			t.Errorf("mode %d: %v, want %v", c.mode, err, c.want)
		}
		if !bytes.Equal(before, e.snapshot(ModuleName)) {
			t.Fatalf("mode %d: a rejected approval changed the store", c.mode)
		}
	}
}
