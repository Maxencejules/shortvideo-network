package authority

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// authorityGas is the fixed charge of every authority message. The store then
// charges for each key the message reads and writes (SDK KV gas config). A
// message reads and writes a fixed set of keys: the config key (0x01), the
// keys its own fields name, and, for a propose or an execute, the policy
// version (0x04). A propose reads and writes its seat's open count (0x14) at
// the current version. A keep from OPEN or QUEUED, or a successful execute,
// reads and writes the proposer's open count at the record's base version.
// None of these grows with past proposals. An execute of a validator payload
// also scans the pending prefix (0x03), at most MaxPendingChanges entries, so
// its gas is bounded but depends on how many changes are scheduled.
const authorityGas = 50_000

// Propose, Approve, and Execute are the committed handlers.
// The ante decorator verifies the signature during FinalizeBlock and during
// CheckTx. This handler checks gas, transaction size, and that the message
// carries a 20-byte signer. It does not verify the signature bytes again.
//
// RegisterProvider and RotateProviderKey are FORBIDDEN at the router until
// Goal 07 owns the provider registry. This package has no handler for them,
// so no message can grow provider rows or retired provider keys (review
// round 2, non-blocking 9).
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

// transition runs one engine call on a cache branch of the store, with the
// same gas meter. Reads and writes are charged once by the gas store; write()
// flushes without more gas.
//
//   - idempotent retry: an authority_result event with idempotent=true; the
//     branch is dropped (the engine wrote nothing).
//   - success: the branch is written.
//   - keep (EXPIRED or REJECTED mark, DR-06-05): the branch is written and the
//     code goes on an authority_result event. Cosmos SDK v0.55 discards the
//     message branch when the handler returns an error, so the handler
//     returns success. The payload was not applied.
//   - failure: an authority_result event and the svn error; the branch is
//     dropped, so the authority store is byte-identical.
func (k Keeper) transition(ctx context.Context, signer []byte, fn func(*State) Outcome) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if code := enforceCommitted(sdkCtx, signer); code != "" {
		return sdkError(code)
	}
	cacheCtx, write := sdkCtx.CacheContext()
	st, err := LoadState(cacheCtx.KVStore(k.storeKey), sdkCtx.ChainID(), sdkCtx.BlockHeight(), sdkCtx.BlockTime().Unix())
	if err != nil {
		return svnerr.ErrIntegrity.Wrapf("%s: %v", CodeIntegrity, err)
	}
	out := fn(st)
	switch {
	case out.Idempotent:
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("authority_result", sdk.NewAttribute("idempotent", "true")))
		return nil
	case out.Success():
		write()
		return nil
	case out.Keep:
		write()
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("authority_result", sdk.NewAttribute("code", out.Code)))
		return nil
	default:
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("authority_result", sdk.NewAttribute("code", out.Code)))
		return sdkError(out.Code)
	}
}

// enforceCommitted bounds the transaction and charges the fixed gas before
// any store read.
func enforceCommitted(ctx sdk.Context, signer []byte) string {
	limit := ctx.GasMeter().Limit()
	if limit < genesis.MinGasPerTx || limit > genesis.MaxGasPerTx {
		return CodeBounds
	}
	if uint64(len(ctx.TxBytes())) > genesis.MaxTxBytes {
		return CodeBounds
	}
	ctx.GasMeter().ConsumeGas(authorityGas, "authority")
	if len(signer) != 20 {
		return CodeMalformed
	}
	return ""
}

// EndBlock applies due validator changes on a cache branch and writes it only
// when the set changed. A removal-bind failure halts: nothing is written,
// and the error stops FinalizeBlock. A store with the round-2 side document
// halts too. Per-block work is at most MaxPendingChanges entries. CometBFT
// v0.40 uses the returned set two heights later. See CometEffectiveDelay.
func (k Keeper) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := sdkCtx.KVStore(k.storeKey)
	if store.Has([]byte{legacySideKey}) {
		return nil, errLegacySide
	}
	if !hasPending(store) {
		return nil, nil
	}
	cacheCtx, write := sdkCtx.CacheContext()
	st, err := LoadState(cacheCtx.KVStore(k.storeKey), sdkCtx.ChainID(), sdkCtx.BlockHeight(), sdkCtx.BlockTime().Unix())
	if err != nil {
		return nil, svnerr.ErrIntegrity.Wrapf("%s: %v", CodeIntegrity, err)
	}
	before, changed, out := st.ApplyDue()
	if !out.Success() {
		return nil, sdkError(out.Code)
	}
	if !changed {
		return nil, nil
	}
	write()
	return Changeset(before, st.Validators())
}
