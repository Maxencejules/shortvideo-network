package authority

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// Propose, Approve, and Execute are the committed handlers.
// Ante checks the signature. These handlers check it again against the
// message signer so a permissive mempool cannot skip that binding.
func (k Keeper) Propose(ctx context.Context, msg *authorityv1.MsgProposeChange) (*authorityv1.MsgProposeChangeResponse, error) {
	if err := k.transition(ctx, msg.GetSigner(), func(st *State) Outcome { return st.ProposeChange(msg) }); err != nil {
		return nil, err
	}
	return &authorityv1.MsgProposeChangeResponse{}, nil
}

func (k Keeper) Approve(ctx context.Context, msg *authorityv1.MsgApproveChange) (*authorityv1.MsgApproveChangeResponse, error) {
	if err := k.transition(ctx, msg.GetSigner(), func(st *State) Outcome { return st.ApproveChange(msg) }); err != nil {
		return nil, err
	}
	return &authorityv1.MsgApproveChangeResponse{}, nil
}

func (k Keeper) Execute(ctx context.Context, msg *authorityv1.MsgExecuteChange) (*authorityv1.MsgExecuteChangeResponse, error) {
	if err := k.transition(ctx, msg.GetSigner(), func(st *State) Outcome { return st.ExecuteChange(msg) }); err != nil {
		return nil, err
	}
	return &authorityv1.MsgExecuteChangeResponse{}, nil
}

func (k Keeper) transition(ctx context.Context, signer []byte, fn func(*State) Outcome) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if code := enforceCommitted(sdkCtx, signer); code != "" {
		return sdkError(code)
	}
	st, err := k.load(sdkCtx)
	if err != nil {
		return err
	}
	st.ChainID = sdkCtx.ChainID()
	st.Height = sdkCtx.BlockHeight()
	st.Time = sdkCtx.BlockTime().Unix()
	trial := st.clone()
	out := fn(trial)
	if out.Idempotent {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("authority_result", sdk.NewAttribute("idempotent", "true")))
		return nil
	}
	if out.Success() || out.Keep {
		if err := k.save(sdkCtx, trial); err != nil {
			return err
		}
	}
	if out.Code == "" {
		return nil
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("authority_result", sdk.NewAttribute("code", out.Code)))
	if out.Keep {
		// The SDK discards this branch when the handler returns an error.
		// The spec keeps the EXPIRED or REJECTED mark, so the code is an
		// event and the handler returns success. The payload was not applied.
		return nil
	}
	return sdkError(out.Code)
}

func enforceCommitted(ctx sdk.Context, signer []byte) string {
	limit := ctx.GasMeter().Limit()
	if limit < genesis.MinGasPerTx || limit > genesis.MaxGasPerTx {
		return CodeBounds
	}
	if uint64(len(ctx.TxBytes())) > genesis.MaxTxBytes {
		return CodeBounds
	}
	// The ante decorator already verified the signature against this signer
	// during FinalizeBlock. That check is committed execution, not only
	// CheckTx. The handler still rejects a signer the message does not carry.
	ctx.GasMeter().ConsumeGas(50_000, "authority")
	if len(signer) != 20 {
		return CodeMalformed
	}
	return ""
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// EndBlock applies due validator changes. A removal-bind failure halts:
// nothing is saved, and the error stops FinalizeBlock. CometBFT v0.40 uses
// the returned set two heights later. See CometEffectiveDelay.
func (k Keeper) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	st, err := k.load(sdkCtx)
	if err != nil {
		return nil, err
	}
	st.ChainID = sdkCtx.ChainID()
	st.Height = sdkCtx.BlockHeight()
	st.Time = sdkCtx.BlockTime().Unix()
	trial := st.clone()
	before, changed, out := trial.ApplyDue()
	if !out.Success() {
		return nil, sdkError(out.Code)
	}
	if !changed {
		return nil, nil
	}
	if err := k.save(sdkCtx, trial); err != nil {
		return nil, err
	}
	return Changeset(before, trial.Validators)
}
