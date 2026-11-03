package chaintest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// Serve answers CometBFT JSON-RPC over HTTP from n on a loopback
// httptest server, so a test can drive the real CometBFT HTTP client (and
// the command line) against the fake: status, abci_query,
// broadcast_tx_sync, and tx. Any other method is a JSON-RPC error. It is
// still the unit-test fake, not a node.
func Serve(t testing.TB, n *Node) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpctypes.RPCRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		result, err := n.call(r.Context(), req)
		resp := rpctypes.NewRPCSuccessResponse(req.ID, result)
		var rpcErr *rpctypes.RPCError
		switch {
		case errors.As(err, &rpcErr):
			resp = rpctypes.NewRPCErrorResponse(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		case err != nil:
			resp = rpctypes.NewRPCErrorResponse(req.ID, -32603, "Internal error", err.Error())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (n *Node) call(ctx context.Context, req rpctypes.RPCRequest) (any, error) {
	switch req.Method {
	case "status":
		return n.Status(ctx)
	case "abci_query":
		var p struct {
			Path   string            `json:"path"`
			Data   cmtbytes.HexBytes `json:"data"`
			Height int64             `json:"height"`
			Prove  bool              `json:"prove"`
		}
		if err := cmtjson.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		return n.ABCIQueryWithOptions(ctx, p.Path, p.Data, rpcclient.ABCIQueryOptions{Height: p.Height, Prove: p.Prove})
	case "broadcast_tx_sync":
		var p struct {
			Tx cmttypes.Tx `json:"tx"`
		}
		if err := cmtjson.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		return n.BroadcastTxSync(ctx, p.Tx)
	case "tx":
		var p struct {
			Hash  []byte `json:"hash"`
			Prove bool   `json:"prove"`
		}
		if err := cmtjson.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		return n.Tx(ctx, p.Hash, p.Prove)
	}
	return nil, &rpctypes.RPCError{Code: -32601, Message: "Method not found", Data: req.Method}
}
