// Package cometwrap adapts the SDK application to CometBFT's ABCI service.
//
// The body matches cosmos-sdk/server.NewCometABCIWrapper. svnd keeps the copy
// here so the binary does not import cosmos-sdk/server, which pulls
// x/genutil and x/staking.
package cometwrap

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

type wrapper struct {
	app servertypes.ABCI
}

// New returns the CometBFT application wrapper.
func New(app servertypes.ABCI) abci.Application {
	return wrapper{app: app}
}

func (w wrapper) Info(_ context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	return w.app.Info(req)
}

func (w wrapper) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	return w.app.Query(ctx, req)
}

func (w wrapper) CheckTx(_ context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	return w.app.CheckTx(req)
}

func (w wrapper) InsertTx(_ context.Context, req *abci.RequestInsertTx) (*abci.ResponseInsertTx, error) {
	return w.app.InsertTx(req)
}

func (w wrapper) ReapTxs(_ context.Context, req *abci.RequestReapTxs) (*abci.ResponseReapTxs, error) {
	return w.app.ReapTxs(req)
}

func (w wrapper) InitChain(_ context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return w.app.InitChain(req)
}

func (w wrapper) PrepareProposal(_ context.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	return w.app.PrepareProposal(req)
}

func (w wrapper) ProcessProposal(_ context.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return w.app.ProcessProposal(req)
}

func (w wrapper) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return w.app.FinalizeBlock(req)
}

func (w wrapper) ExtendVote(ctx context.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
	return w.app.ExtendVote(ctx, req)
}

func (w wrapper) VerifyVoteExtension(_ context.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
	return w.app.VerifyVoteExtension(req)
}

func (w wrapper) Commit(_ context.Context, _ *abci.RequestCommit) (*abci.ResponseCommit, error) {
	return w.app.Commit()
}

func (w wrapper) ListSnapshots(_ context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	return w.app.ListSnapshots(req)
}

func (w wrapper) OfferSnapshot(_ context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	return w.app.OfferSnapshot(req)
}

func (w wrapper) LoadSnapshotChunk(_ context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	return w.app.LoadSnapshotChunk(req)
}

func (w wrapper) ApplySnapshotChunk(_ context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	return w.app.ApplySnapshotChunk(req)
}
