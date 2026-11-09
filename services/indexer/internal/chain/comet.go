package chain

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
)

// HTTP is the CometBFT JSON-RPC endpoint.
type HTTP struct {
	rpc     rpcclient.Client
	timeout time.Duration
}

// Dial returns an HTTP reader for an http or https base URL.
func Dial(rpcURL string, timeout time.Duration) (*HTTP, error) {
	u, err := url.Parse(rpcURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("chain: rpc URL must be an http or https base URL")
	}
	if timeout <= 0 {
		return nil, errors.New("chain: rpc timeout must be positive")
	}
	rpc, err := rpchttp.NewWithClient(strings.TrimSuffix(rpcURL, "/"), "/websocket", &http.Client{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	return &HTTP{rpc: rpc, timeout: timeout}, nil
}

func (h *HTTP) call(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, h.timeout)
}

// Status implements Reader.
func (h *HTTP) Status(ctx context.Context) (Status, error) {
	ctx, cancel := h.call(ctx)
	defer cancel()
	st, err := h.rpc.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if st == nil {
		return Status{}, fmt.Errorf("%w: empty status", ErrHistoryUnavailable)
	}
	return Status{
		ChainID: st.NodeInfo.Network, Height: st.SyncInfo.LatestBlockHeight,
		Time: st.SyncInfo.LatestBlockTime.UTC(), CatchingUp: st.SyncInfo.CatchingUp,
	}, nil
}

// Header implements Reader.
func (h *HTTP) Header(ctx context.Context, height int64) (Header, error) {
	ctx, cancel := h.call(ctx)
	defer cancel()
	block, err := h.rpc.Block(ctx, &height)
	if err != nil || block == nil || block.Block == nil {
		return Header{}, ErrHistoryUnavailable
	}
	commit, err := h.rpc.Commit(ctx, &height)
	if err != nil || commit == nil || commit.SignedHeader.Commit == nil {
		return Header{}, ErrHistoryUnavailable
	}
	return headerFrom(block, commit), nil
}

func headerFrom(block *ctypes.ResultBlock, commit *ctypes.ResultCommit) Header {
	hash := []byte(block.BlockID.Hash)
	if len(hash) == 0 {
		hash = block.Block.Hash()
	}
	return Header{
		ChainID: block.Block.ChainID, Height: block.Block.Height,
		Hash: append([]byte(nil), hash...), AppHash: append([]byte(nil), block.Block.AppHash...),
		Time: block.Block.Time.UTC(), CommitHash: append([]byte(nil), commit.SignedHeader.Commit.BlockID.Hash...),
	}
}

// Results implements Reader.
func (h *HTTP) Results(ctx context.Context, height int64) (Results, error) {
	ctx, cancel := h.call(ctx)
	defer cancel()
	block, err := h.rpc.Block(ctx, &height)
	if err != nil || block == nil || block.Block == nil {
		return Results{}, ErrHistoryUnavailable
	}
	out, err := h.rpc.BlockResults(ctx, &height)
	if err != nil || out == nil {
		return Results{}, ErrHistoryUnavailable
	}
	txs, err := txsFrom(block, out)
	if err != nil {
		return Results{}, err
	}
	return Results{Height: out.Height, AppHash: append([]byte(nil), out.AppHash...), Txs: txs, Finalize: out.FinalizeBlockEvents}, nil
}

func txsFrom(block *ctypes.ResultBlock, out *ctypes.ResultBlockResults) ([]project.Tx, error) {
	raw := block.Block.Data.Txs
	if out.TxsResults != nil && len(raw) != len(out.TxsResults) {
		return nil, fmt.Errorf("%w: %d transactions and %d results", ErrForged, len(raw), len(out.TxsResults))
	}
	txs := make([]project.Tx, len(raw))
	for i, tx := range raw {
		txs[i].Hash = cmttypes.Tx(tx).Hash()
		if out.TxsResults != nil && out.TxsResults[i] != nil {
			txs[i].Code = out.TxsResults[i].Code
			txs[i].Events = out.TxsResults[i].Events
		}
	}
	return txs, nil
}

// Query implements Reader. Prove is false: this client does not check a
// light-client proof.
func (h *HTTP) Query(ctx context.Context, path string, data []byte, height int64) (QueryResult, error) {
	ctx, cancel := h.call(ctx)
	defer cancel()
	res, err := h.rpc.ABCIQueryWithOptions(ctx, path, data, rpcclient.ABCIQueryOptions{Height: height, Prove: false})
	if err != nil || res == nil {
		return QueryResult{}, ErrQuery
	}
	r := res.Response
	return QueryResult{Code: r.Code, Codespace: r.Codespace, Log: clip(r.Log), Value: append([]byte(nil), r.Value...), Height: r.Height, Found: r.Code == 0}, nil
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
