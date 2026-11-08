package main

// fakeNode is a unit-test fake (never end-to-end evidence): an HTTP
// JSON-RPC server on loopback that answers the CometBFT RPC methods the
// broadcaster processes call, so the smoke test can run them as real
// processes without a chain. It models only what the smoke test needs:
// a height that advances every 500 ms, the governance roles of the two
// signer keys, their accounts at sequence 0, no content record, a mempool
// that accepts every broadcast with code 0 and keeps its bytes, and no
// committed transaction (tx answers "not found", tx_search is empty). The
// real chain is exercised by the devnet test (Wave 2, B9).

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/p2p"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
)

// fakeAccount is one account the fake node knows.
type fakeAccount struct {
	address []byte
	number  uint64
	role    typesv1.SubmitterRole
}

type fakeNode struct {
	t        *testing.T
	codec    *chain.Codec
	enc      app.Encoding
	start    time.Time
	accounts []fakeAccount
	srv      *http.Server
	url      string

	mu         sync.Mutex
	broadcasts [][]byte
	unserved   []string
}

// newFakeNode starts the fake node on a loopback port.
func newFakeNode(t *testing.T, codec *chain.Codec, accounts []fakeAccount) *fakeNode {
	t.Helper()
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	n := &fakeNode{t: t, codec: codec, enc: enc, start: time.Now(), accounts: accounts, url: "http://" + ln.Addr().String()}
	n.srv = &http.Server{Handler: http.HandlerFunc(n.serveHTTP), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := n.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("fake node: %v", err)
		}
	}()
	t.Cleanup(func() { _ = n.srv.Close() })
	return n
}

// height advances by one every 500 ms from 10.
func (n *fakeNode) height() int64 { return 10 + int64(time.Since(n.start)/(500*time.Millisecond)) }

// broadcastTxs returns the bytes of every broadcast received.
func (n *fakeNode) broadcastTxs() [][]byte {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([][]byte(nil), n.broadcasts...)
}

// unservedPaths returns the methods and query paths the fake does not
// model, which the processes asked for.
func (n *fakeNode) unservedPaths() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.unserved...)
}

func (n *fakeNode) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	var req rpctypes.RPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "decode", http.StatusBadRequest)
		return
	}
	result, rpcErr := n.call(req.Method, req.Params)
	resp := rpctypes.NewRPCSuccessResponse(req.ID, result)
	if rpcErr != nil {
		resp = rpctypes.RPCInternalError(req.ID, rpcErr)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// call answers one JSON-RPC method.
func (n *fakeNode) call(method string, params json.RawMessage) (any, error) {
	h := n.height()
	switch method {
	case "status":
		return &coretypes.ResultStatus{NodeInfo: p2p.DefaultNodeInfo{Network: n.codec.ChainID()},
			SyncInfo: coretypes.SyncInfo{LatestBlockHeight: h, LatestBlockTime: n.start.Add(time.Duration(h-10) * time.Second).UTC()}}, nil
	case "abci_query":
		var p struct {
			Path string            `json:"path"`
			Data cmtbytes.HexBytes `json:"data"`
		}
		if err := cmtjson.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		value, found, err := n.query(p.Path, p.Data)
		if err != nil {
			return nil, err
		}
		resp := abci.ResponseQuery{Height: h, Value: value}
		if !found {
			resp.Code, resp.Codespace, resp.Value = sdkerrors.ErrKeyNotFound.ABCICode(), sdkerrors.ErrKeyNotFound.Codespace(), nil
		}
		return &coretypes.ResultABCIQuery{Response: resp}, nil
	case "broadcast_tx_sync":
		var p struct {
			Tx []byte `json:"tx"`
		}
		if err := cmtjson.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if _, err := n.codec.Decode(p.Tx); err != nil {
			return &coretypes.ResultBroadcastTx{Code: sdkerrors.ErrTxDecode.ABCICode(), Codespace: "sdk"}, nil
		}
		n.mu.Lock()
		n.broadcasts = append(n.broadcasts, append([]byte(nil), p.Tx...))
		n.mu.Unlock()
		sum := sha256.Sum256(p.Tx)
		return &coretypes.ResultBroadcastTx{Hash: sum[:]}, nil
	case "tx":
		var p struct {
			Hash []byte `json:"hash"`
		}
		if err := cmtjson.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("tx (%X) not found", p.Hash)
	case "tx_search":
		return &coretypes.ResultTxSearch{Txs: []*coretypes.ResultTx{}}, nil
	}
	n.mu.Lock()
	n.unserved = append(n.unserved, method)
	n.mu.Unlock()
	return nil, fmt.Errorf("fake node: method %s not served", method)
}

// query answers the ABCI query paths the processes use.
func (n *fakeNode) query(path string, data []byte) ([]byte, bool, error) {
	switch path {
	case "/svn.authority.v1.Query/Governance":
		state := &authorityv1.AuthorityState{}
		for _, a := range n.accounts {
			state.Submitters = append(state.Submitters, &typesv1.Submitter{Address: a.address, Role: a.role})
		}
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(&authorityv1.QueryGovernanceResponse{State: state,
			Page: &typesv1.PageResponse{}})
		return b, true, err
	case "/cosmos.auth.v1beta1.Query/Account":
		var req authtypes.QueryAccountRequest
		if err := gogoproto.Unmarshal(data, &req); err != nil {
			return nil, false, err
		}
		for _, a := range n.accounts {
			bech, err := n.codec.Bech32(a.address)
			if err != nil {
				return nil, false, err
			}
			if bech != req.Address {
				continue
			}
			packed, err := codectypes.NewAnyWithValue(authtypes.NewBaseAccount(a.address, nil, a.number, 0))
			if err != nil {
				return nil, false, err
			}
			b, err := n.enc.Codec.Marshal(&authtypes.QueryAccountResponse{Account: packed})
			return b, true, err
		}
		return nil, false, nil
	case "/svn.content.v1.Query/Content":
		return nil, false, nil
	}
	n.mu.Lock()
	n.unserved = append(n.unserved, path)
	n.mu.Unlock()
	return nil, false, fmt.Errorf("fake node: query path %s not served", path)
}
