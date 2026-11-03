// Package chainclient reads chain state and submits audit batches through
// the operator's CometBFT RPC endpoint (plan sections 16.3 and 16.8).
//
// The chain RPC URL is trusted operator configuration, not a provider
// endpoint, so the provider destination policy does not apply to it. The
// HTTP client still uses no proxy (Transport.Proxy is nil), follows no
// redirect, bounds every request in time, and bounds every response body.
//
// Reads go through ABCI query to gRPC-routed query paths and are answered
// by one node: they trust that node. Writes use broadcast_tx_sync, whose
// answer is CheckTx (the ante chain only). An acknowledgement is not a
// commit: a batch is reported COMMITTED only from the committed transaction
// result (the tx RPC), and its items only from the evidence records read
// back afterwards (Submitter).
package chainclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Bounds.
const (
	// MaxResponseBytes bounds one RPC response body.
	MaxResponseBytes = 16 << 20
	// maxLogBytes bounds chain log text kept in an error.
	maxLogBytes = 256
)

// gRPC-routed query paths.
const (
	pathActiveLeases = "/svn.leases.v1.Query/ActiveLeases"
	pathContent      = "/svn.content.v1.Query/Content"
	pathProvider     = "/svn.providers.v1.Query/Provider"
	pathEvidence     = "/svn.evidence.v1.Query/Evidence"
	pathAccount      = "/cosmos.auth.v1beta1.Query/Account"
)

// RPC is the part of the CometBFT RPC client this package uses.
// *rpchttp.HTTP implements it; unit tests use a fake.
type RPC interface {
	Status(ctx context.Context) (*coretypes.ResultStatus, error)
	ABCIQueryWithOptions(ctx context.Context, path string, data cmtbytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error)
	BroadcastTxSync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error)
	Tx(ctx context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error)
}

var _ RPC = (*rpchttp.HTTP)(nil)

// Errors.
var (
	ErrCatchingUp = errors.New("chainclient: the node is catching up; its latest header is not current")
	ErrResponse   = errors.New("chainclient: unexpected response")
)

// QueryError is a rejected ABCI query.
type QueryError struct {
	Path      string
	Codespace string
	Code      uint32
	Log       string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("chainclient: %s: %s/%d %q", e.Path, e.Codespace, e.Code, e.Log)
}

// HTTPClient is the chain RPC HTTP client: no proxy, no redirect, a request
// timeout, and a response body bound.
func HTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 16 << 10,
		DisableCompression:     true,
	}
	return &http.Client{
		Transport: limitedTransport{next: transport},
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// limitedTransport caps every response body at MaxResponseBytes: reading
// past it is an error, not a silent truncation.
type limitedTransport struct{ next http.RoundTripper }

func (t limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &limitedBody{ReadCloser: resp.Body, left: MaxResponseBytes}
	return resp, nil
}

type limitedBody struct {
	io.ReadCloser
	left int64
}

var errBodyTooLarge = errors.New("chainclient: response body above the bound")

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		var one [1]byte
		if n, _ := b.ReadCloser.Read(one[:]); n > 0 {
			return 0, errBodyTooLarge
		}
		return b.ReadCloser.Read(p)
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, err
}

// NewHTTP returns the CometBFT RPC client of the base URL rpcURL (http or
// https, no path) over HTTPClient(timeout). It opens no connection.
func NewHTTP(rpcURL string, timeout time.Duration) (*rpchttp.HTTP, error) {
	u, err := url.Parse(rpcURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("chainclient: rpc URL must be an http or https base URL without userinfo, path, query, or fragment")
	}
	return rpchttp.NewWithClient(strings.TrimSuffix(rpcURL, "/"), "/websocket", HTTPClient(timeout))
}

// Client reads chain state through one RPC endpoint.
type Client struct {
	rpc RPC
	enc app.Encoding
}

// New wraps rpc.
func New(rpc RPC) (*Client, error) {
	if rpc == nil {
		return nil, errors.New("chainclient: nil rpc")
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		return nil, fmt.Errorf("chainclient: encoding: %w", err)
	}
	return &Client{rpc: rpc, enc: enc}, nil
}

// Header is the node's latest committed header.
type Header struct {
	ChainID string
	Height  int64
	// Time is the header time in unix seconds: the chain's clock.
	Time int64
}

// Header reads the latest committed header. A node that reports it is
// catching up is refused: its header time is not the chain's current time.
func (c *Client) Header(ctx context.Context) (Header, error) {
	st, err := c.rpc.Status(ctx)
	if err != nil {
		return Header{}, fmt.Errorf("chainclient: status: %w", err)
	}
	if st == nil || st.NodeInfo.Network == "" || st.SyncInfo.LatestBlockHeight < 1 {
		return Header{}, fmt.Errorf("%w: status without a chain id or a committed block", ErrResponse)
	}
	if st.SyncInfo.CatchingUp {
		return Header{}, ErrCatchingUp
	}
	return Header{ChainID: st.NodeInfo.Network, Height: st.SyncInfo.LatestBlockHeight, Time: st.SyncInfo.LatestBlockTime.Unix()}, nil
}

func clip(s string) string {
	if len(s) > maxLogBytes {
		return s[:maxLogBytes]
	}
	return s
}

// query runs one gRPC-routed ABCI query at the latest height. found is
// false when the node answered not found (codespace sdk, ErrKeyNotFound,
// which is how a NotFound query status crosses ABCI).
func (c *Client) query(ctx context.Context, path string, req proto.Message, resp proto.Message) (bool, error) {
	data, err := proto.Marshal(req)
	if err != nil {
		return false, fmt.Errorf("chainclient: encode %s: %w", path, err)
	}
	res, err := c.rpc.ABCIQueryWithOptions(ctx, path, data, rpcclient.ABCIQueryOptions{})
	if err != nil {
		return false, fmt.Errorf("chainclient: %s: %w", path, err)
	}
	if res == nil {
		return false, fmt.Errorf("%w: empty %s answer", ErrResponse, path)
	}
	r := res.Response
	if r.Code != 0 {
		if r.Codespace == sdkerrors.ErrKeyNotFound.Codespace() && r.Code == sdkerrors.ErrKeyNotFound.ABCICode() {
			return false, nil
		}
		return false, &QueryError{Path: path, Codespace: r.Codespace, Code: r.Code, Log: clip(r.Log)}
	}
	if err := proto.Unmarshal(r.Value, resp); err != nil {
		return false, fmt.Errorf("%w: decode %s: %v", ErrResponse, path, err)
	}
	return true, nil
}

// HeldLease returns the held lease of the (provider, content) pair, if
// any: svn.leases.v1.Query/ActiveLeases with both filters, which answers at
// most one lease (DR-08-08: no get-by-id query exists).
func (c *Client) HeldLease(ctx context.Context, providerID, contentID string) (*leasesv1.Lease, bool, error) {
	resp := &leasesv1.QueryActiveLeasesResponse{}
	found, err := c.query(ctx, pathActiveLeases, &leasesv1.QueryActiveLeasesRequest{ProviderId: providerID, ContentId: contentID}, resp)
	if err != nil || !found {
		return nil, false, err
	}
	switch n := len(resp.GetLeases()); {
	case n == 0:
		return nil, false, nil
	case n > 1:
		return nil, false, fmt.Errorf("%w: %d leases for one (provider, content) pair", ErrResponse, n)
	}
	return resp.GetLeases()[0], true, nil
}

// Content returns svn.content.v1.Query/Content for one version.
func (c *Client) Content(ctx context.Context, contentID string, version uint64) (*contentv1.QueryContentResponse, bool, error) {
	resp := &contentv1.QueryContentResponse{}
	found, err := c.query(ctx, pathContent, &contentv1.QueryContentRequest{ContentId: contentID, ContentVersion: version}, resp)
	if err != nil || !found {
		return nil, false, err
	}
	return resp, true, nil
}

// Provider returns the provider record.
func (c *Client) Provider(ctx context.Context, providerID string) (*providersv1.Provider, bool, error) {
	resp := &providersv1.QueryProviderResponse{}
	found, err := c.query(ctx, pathProvider, &providersv1.QueryProviderRequest{ProviderId: providerID}, resp)
	if err != nil || !found || resp.GetProvider() == nil {
		return nil, false, err
	}
	return resp.GetProvider(), true, nil
}

// Evidence returns the evidence record of an id with its reported state at
// the node's latest committed time (read-time expiry, x/evidence D-4).
func (c *Client) Evidence(ctx context.Context, evidenceID string) (*evidencev1.EvidenceRecord, bool, error) {
	resp := &evidencev1.QueryEvidenceResponse{}
	found, err := c.query(ctx, pathEvidence, &evidencev1.QueryEvidenceRequest{EvidenceId: evidenceID}, resp)
	if err != nil || !found {
		return nil, false, err
	}
	switch n := len(resp.GetRecords()); {
	case n == 0:
		return nil, false, nil
	case n > 1:
		return nil, false, fmt.Errorf("%w: %d records for one evidence id", ErrResponse, n)
	}
	rec := resp.GetRecords()[0]
	if rec.GetEvidenceId() != evidenceID {
		return nil, false, fmt.Errorf("%w: record of another evidence id", ErrResponse)
	}
	return rec, true, nil
}

// Account returns the account number and sequence of a 20-byte address.
func (c *Client) Account(ctx context.Context, addr []byte) (number, sequence uint64, err error) {
	bech, err := addresscodec.NewBech32Codec(chainaddress.Account).BytesToString(addr)
	if err != nil {
		return 0, 0, fmt.Errorf("chainclient: address: %w", err)
	}
	data, err := gogoproto.Marshal(&authtypes.QueryAccountRequest{Address: bech})
	if err != nil {
		return 0, 0, fmt.Errorf("chainclient: encode account query: %w", err)
	}
	res, err := c.rpc.ABCIQueryWithOptions(ctx, pathAccount, data, rpcclient.ABCIQueryOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("chainclient: account: %w", err)
	}
	if res == nil {
		return 0, 0, fmt.Errorf("%w: empty account answer", ErrResponse)
	}
	if r := res.Response; r.Code != 0 {
		return 0, 0, &QueryError{Path: pathAccount, Codespace: r.Codespace, Code: r.Code, Log: clip(r.Log)}
	}
	var resp authtypes.QueryAccountResponse
	if err := c.enc.Codec.Unmarshal(res.Response.Value, &resp); err != nil {
		return 0, 0, fmt.Errorf("%w: decode account: %v", ErrResponse, err)
	}
	var account sdk.AccountI
	if err := c.enc.InterfaceRegistry.UnpackAny(resp.Account, &account); err != nil || account == nil {
		return 0, 0, fmt.Errorf("%w: account type", ErrResponse)
	}
	if string(account.GetAddress()) != string(addr) {
		return 0, 0, fmt.Errorf("%w: account of another address", ErrResponse)
	}
	return account.GetAccountNumber(), account.GetSequence(), nil
}

// Ack is a broadcast_tx_sync answer: CheckTx only. It is never a commit.
type Ack struct {
	Code      uint32
	Codespace string
	Log       string
}

// Broadcast sends raw with broadcast_tx_sync.
func (c *Client) Broadcast(ctx context.Context, raw []byte) (Ack, error) {
	res, err := c.rpc.BroadcastTxSync(ctx, cmttypes.Tx(raw))
	if err != nil {
		return Ack{}, fmt.Errorf("chainclient: broadcast: %w", err)
	}
	if res == nil {
		return Ack{}, fmt.Errorf("%w: empty broadcast answer", ErrResponse)
	}
	return Ack{Code: res.Code, Codespace: res.Codespace, Log: clip(res.Log)}, nil
}

// TxResult is a committed transaction result.
type TxResult struct {
	Height    int64
	Code      uint32
	Codespace string
	Log       string
	GasUsed   int64
}

// CommittedTx returns the committed result of a transaction hash. found is
// false while no block holds it: the node answers "tx (...) not found" as
// a JSON-RPC error, the only form that answer has.
func (c *Client) CommittedTx(ctx context.Context, hash []byte) (TxResult, bool, error) {
	res, err := c.rpc.Tx(ctx, hash, false)
	if err != nil {
		var rpcErr *rpctypes.RPCError
		if errors.As(err, &rpcErr) && strings.Contains(rpcErr.Data, "not found") {
			return TxResult{}, false, nil
		}
		return TxResult{}, false, fmt.Errorf("chainclient: tx: %w", err)
	}
	if res == nil || res.Height < 1 {
		return TxResult{}, false, fmt.Errorf("%w: tx answer without a height", ErrResponse)
	}
	r := res.TxResult
	return TxResult{Height: res.Height, Code: r.Code, Codespace: r.Codespace, Log: clip(r.Log), GasUsed: r.GasUsed}, true, nil
}

// SVNCode returns the protocol code name of a result in codespace svn, or
// OK for code 0, any other codespace, and any number outside the enum.
func SVNCode(codespace string, code uint32) msgkit.Code {
	if codespace != svnerr.ModuleName || code == 0 || code > math.MaxInt32 {
		return msgkit.OK
	}
	name, ok := typesv1.ErrorCode_name[int32(code)]
	if !ok {
		return msgkit.OK
	}
	return msgkit.Code(name)
}
