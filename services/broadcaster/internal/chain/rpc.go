// Package chain is the broadcaster's chain client: it builds outer
// transactions for one signature (it holds no key and signs nothing),
// decodes them with the application's strict decoder, reads the chain
// through one configured CometBFT RPC endpoint, classifies broadcast and
// committed answers, and compares a committed transaction with an intended
// message (plan sections 8.2, 8.4, 8.6 and 9.4).
//
// It is copied and generalised from agents/auditor/chainclient, which stays
// unchanged (plan 4.4, DR-11-21): messages are any registered svn message
// instead of the audit batch, every transaction carries a timeout height,
// the chain id is configuration and the node must report the same one,
// committed transactions are checked against the requested hash, and reads
// return the height they were answered at.
//
// The RPC URL is trusted operator configuration. The HTTP client uses no
// proxy, follows no redirect, bounds every request in time, and bounds every
// response body. Every read trusts the one node that answered (the beta's
// trusted gateway, docs/blueprint.md:391). A broadcast answer is CheckTx
// only: it is never a commit.
package chain

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
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
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Bounds.
const (
	// MaxResponseBytes bounds one RPC response body.
	MaxResponseBytes = 16 << 20
	// maxLogBytes bounds chain log text kept in a result or an error.
	maxLogBytes = 256
	// MaxSearchPageBudget bounds the pages of 100 one TxSearch reads (the
	// hard upper bound of the Query/Deadlines page budget, plan section 11,
	// PROPOSED).
	MaxSearchPageBudget = 50
	// searchPerPage is CometBFT's maximum tx_search page size
	// (rpc/core/env.go maxPerPage).
	searchPerPage = 100
	// maxQueryBytes is CometBFT's tx_search query length limit
	// (rpc/core/events.go maxQueryLength).
	maxQueryBytes = 512
)

// ABCI query paths. Object reads use the gRPC-routed module queries. Gas
// is measured through the SDK transaction service's Simulate method, also
// gRPC-routed: the /app/simulate path JSON-encodes its answer with the
// application's interface registry, which does not register the svn
// message responses, so it cannot answer a successful svn message
// (TestAppSimulateJSONCannotCarrySvnResponses).
const (
	pathActiveLeases = "/svn.leases.v1.Query/ActiveLeases"
	pathDeadlines    = "/svn.leases.v1.Query/Deadlines"
	pathContent      = "/svn.content.v1.Query/Content"
	pathProvider     = "/svn.providers.v1.Query/Provider"
	pathGovernance   = "/svn.authority.v1.Query/Governance"
	pathAccount      = "/cosmos.auth.v1beta1.Query/Account"
	pathSimulate     = "/cosmos.tx.v1beta1.Service/Simulate"
)

// RPC is the part of the CometBFT RPC client this package uses.
// *rpchttp.HTTP implements it.
type RPC interface {
	Status(ctx context.Context) (*coretypes.ResultStatus, error)
	ABCIQueryWithOptions(ctx context.Context, path string, data cmtbytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error)
	BroadcastTxSync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error)
	Tx(ctx context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error)
	TxSearch(ctx context.Context, query string, prove bool, page, perPage *int, orderBy string) (*coretypes.ResultTxSearch, error)
	Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error)
}

var _ RPC = (*rpchttp.HTTP)(nil)

// Errors.
var (
	ErrCatchingUp       = errors.New("chain: the node is catching up; its latest header is not current")
	ErrResponse         = errors.New("chain: unexpected response")
	ErrChainID          = errors.New("chain: the node runs another chain id than the configured one")
	ErrSearchIncomplete = errors.New("chain: tx_search has more results than the page budget reads")
	ErrNoAccount        = errors.New("chain: no account at this address")
)

// QueryError is a rejected ABCI query.
type QueryError struct {
	Path      string
	Codespace string
	Code      uint32
	Log       string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("chain: %s: %s/%d %q", e.Path, e.Codespace, e.Code, e.Log)
}

// StaleError is a read answered below the height the caller needs: the
// node has not yet committed (or indexed) what the caller already saw.
type StaleError struct {
	Path string
	Want int64
	Got  int64
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("chain: %s answered at height %d, below the needed %d", e.Path, e.Got, e.Want)
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

var errBodyTooLarge = errors.New("chain: response body above the bound")

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
		return nil, errors.New("chain: rpc URL must be an http or https base URL without userinfo, path, query, or fragment")
	}
	if timeout <= 0 {
		return nil, errors.New("chain: rpc timeout must be positive")
	}
	return rpchttp.NewWithClient(strings.TrimSuffix(rpcURL, "/"), "/websocket", HTTPClient(timeout))
}

// Client reads the chain through one RPC endpoint and builds transactions
// with its Codec.
type Client struct {
	*Codec
	rpc RPC
}

// New wraps rpc for the chain of codec.
func New(rpc RPC, codec *Codec) (*Client, error) {
	if rpc == nil || codec == nil {
		return nil, errors.New("chain: nil rpc or codec")
	}
	return &Client{Codec: codec, rpc: rpc}, nil
}

// Status is the node's latest committed header.
type Status struct {
	ChainID string
	Height  int64
	// Time is the header time: the chain's clock, not a local one.
	Time time.Time
}

// Status reads the latest committed header. A node that reports another
// chain id than the configured one is refused (ErrChainID), and so is a
// node that reports it is catching up (ErrCatchingUp).
func (c *Client) Status(ctx context.Context) (Status, error) {
	st, err := c.rpc.Status(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("chain: status: %w", err)
	}
	if st == nil || st.NodeInfo.Network == "" || st.SyncInfo.LatestBlockHeight < 1 {
		return Status{}, fmt.Errorf("%w: status without a chain id or a committed block", ErrResponse)
	}
	if st.NodeInfo.Network != c.chainID {
		return Status{}, fmt.Errorf("%w: node %q, configured %q", ErrChainID, clip(st.NodeInfo.Network), c.chainID)
	}
	if st.SyncInfo.CatchingUp {
		return Status{}, ErrCatchingUp
	}
	return Status{ChainID: st.NodeInfo.Network, Height: st.SyncInfo.LatestBlockHeight, Time: st.SyncInfo.LatestBlockTime.UTC()}, nil
}

func clip(s string) string {
	if len(s) > maxLogBytes {
		return s[:maxLogBytes]
	}
	return s
}

// abciQuery runs one ABCI query at the latest height and checks the answer
// height: below 1 is a malformed answer, below minHeight is a StaleError.
// found is false when the node answered not found (codespace sdk,
// ErrKeyNotFound, which is how a NotFound query status crosses ABCI). Any
// other non-zero code is a QueryError.
func (c *Client) abciQuery(ctx context.Context, path string, data []byte, minHeight int64) (value []byte, height int64, found bool, err error) {
	if minHeight < 0 {
		return nil, 0, false, errors.New("chain: minimum height must not be negative")
	}
	res, err := c.rpc.ABCIQueryWithOptions(ctx, path, data, rpcclient.ABCIQueryOptions{})
	if err != nil {
		return nil, 0, false, fmt.Errorf("chain: %s: %w", path, err)
	}
	if res == nil {
		return nil, 0, false, fmt.Errorf("%w: empty %s answer", ErrResponse, path)
	}
	r := res.Response
	if r.Height < 1 {
		return nil, 0, false, fmt.Errorf("%w: %s answer without a height", ErrResponse, path)
	}
	if r.Height < minHeight {
		return nil, r.Height, false, &StaleError{Path: path, Want: minHeight, Got: r.Height}
	}
	if r.Code != 0 {
		if r.Codespace == sdkerrors.ErrKeyNotFound.Codespace() && r.Code == sdkerrors.ErrKeyNotFound.ABCICode() {
			return nil, r.Height, false, nil
		}
		return nil, r.Height, false, &QueryError{Path: path, Codespace: r.Codespace, Code: r.Code, Log: clip(r.Log)}
	}
	return r.Value, r.Height, true, nil
}

// query runs one gRPC-routed svn query (see abciQuery) and decodes the
// answer into resp.
func (c *Client) query(ctx context.Context, path string, req, resp proto.Message, minHeight int64) (bool, int64, error) {
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	if err != nil {
		return false, 0, fmt.Errorf("chain: encode %s: %w", path, err)
	}
	value, height, found, err := c.abciQuery(ctx, path, data, minHeight)
	if err != nil || !found {
		return false, height, err
	}
	if err := proto.Unmarshal(value, resp); err != nil {
		return false, height, fmt.Errorf("%w: decode %s: %v", ErrResponse, path, err)
	}
	return true, height, nil
}

func checkPage(pageKey []byte, limit uint32) error {
	if limit < 1 || limit > genesis.MaxPageLimit {
		return fmt.Errorf("chain: page limit must be 1 to %d", genesis.MaxPageLimit)
	}
	if len(pageKey) > msgkit.PageKeyMaxBytes {
		return fmt.Errorf("chain: page key above %d bytes", msgkit.PageKeyMaxBytes)
	}
	return nil
}

// Content returns svn.content.v1.Query/Content for one content version
// (record or tombstone) and the height it was read at. A record or
// tombstone of another id or version is refused.
func (c *Client) Content(ctx context.Context, contentID string, version uint64, minHeight int64) (*contentv1.QueryContentResponse, bool, int64, error) {
	if !msgkit.ValidID(contentID) || version < 1 {
		return nil, false, 0, errors.New("chain: content query needs a valid id and a version of at least 1")
	}
	resp := &contentv1.QueryContentResponse{}
	found, height, err := c.query(ctx, pathContent, &contentv1.QueryContentRequest{ContentId: contentID, ContentVersion: version}, resp, minHeight)
	if err != nil || !found {
		return nil, false, height, err
	}
	for _, rec := range []interface {
		GetContentId() string
		GetContentVersion() uint64
	}{resp.GetContent(), resp.GetTombstone()} {
		if (rec.GetContentId() != "" || rec.GetContentVersion() != 0) && (rec.GetContentId() != contentID || rec.GetContentVersion() != version) {
			return nil, false, height, fmt.Errorf("%w: content record of another id or version", ErrResponse)
		}
	}
	if resp.GetContent() == nil && resp.GetTombstone() == nil {
		return nil, false, height, fmt.Errorf("%w: content answer without a record or tombstone", ErrResponse)
	}
	return resp, true, height, nil
}

// Provider returns the provider record and the height it was read at.
func (c *Client) Provider(ctx context.Context, providerID string, minHeight int64) (*providersv1.Provider, bool, int64, error) {
	if !msgkit.ValidID(providerID) {
		return nil, false, 0, errors.New("chain: provider query needs a valid id")
	}
	resp := &providersv1.QueryProviderResponse{}
	found, height, err := c.query(ctx, pathProvider, &providersv1.QueryProviderRequest{ProviderId: providerID}, resp, minHeight)
	if err != nil || !found || resp.GetProvider() == nil {
		return nil, false, height, err
	}
	if resp.GetProvider().GetProviderId() != providerID {
		return nil, false, height, fmt.Errorf("%w: record of another provider", ErrResponse)
	}
	return resp.GetProvider(), true, height, nil
}

// HeldLease returns the held lease of the (provider, content) pair, if
// any: svn.leases.v1.Query/ActiveLeases with both filters, which answers at
// most one lease (DR-08-08: no get-by-id query exists). Ended and expired
// leases are not listed.
func (c *Client) HeldLease(ctx context.Context, providerID, contentID string, minHeight int64) (*leasesv1.Lease, bool, int64, error) {
	if !msgkit.ValidID(providerID) || !msgkit.ValidID(contentID) {
		return nil, false, 0, errors.New("chain: held lease query needs a valid provider id and content id")
	}
	resp := &leasesv1.QueryActiveLeasesResponse{}
	found, height, err := c.query(ctx, pathActiveLeases, &leasesv1.QueryActiveLeasesRequest{ProviderId: providerID, ContentId: contentID}, resp, minHeight)
	if err != nil || !found {
		return nil, false, height, err
	}
	switch n := len(resp.GetLeases()); {
	case n == 0:
		return nil, false, height, nil
	case n > 1:
		return nil, false, height, fmt.Errorf("%w: %d leases for one (provider, content) pair", ErrResponse, n)
	}
	lease := resp.GetLeases()[0]
	if lease.GetProviderId() != providerID || lease.GetContentId() != contentID {
		return nil, false, height, fmt.Errorf("%w: lease of another (provider, content) pair", ErrResponse)
	}
	return lease, true, height, nil
}

// Deadlines returns one page of svn.leases.v1.Query/Deadlines (open offers
// and held leases whose deadline is at most notAfter) and the height it
// was read at.
func (c *Client) Deadlines(ctx context.Context, notAfter int64, pageKey []byte, limit uint32, minHeight int64) (*leasesv1.QueryDeadlinesResponse, int64, error) {
	if notAfter < genesis.UnixSecondsMin || notAfter > genesis.UnixSecondsMax {
		return nil, 0, fmt.Errorf("chain: deadline bound must be %d to %d", genesis.UnixSecondsMin, genesis.UnixSecondsMax)
	}
	if err := checkPage(pageKey, limit); err != nil {
		return nil, 0, err
	}
	req := &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: notAfter, Page: &typesv1.PageRequest{Limit: limit, PageKey: pageKey}}
	resp := &leasesv1.QueryDeadlinesResponse{}
	found, height, err := c.query(ctx, pathDeadlines, req, resp, minHeight)
	if err != nil {
		return nil, height, err
	}
	if !found {
		return nil, height, fmt.Errorf("%w: %s answered not found", ErrResponse, pathDeadlines)
	}
	if n := len(resp.GetLeases()) + len(resp.GetOffers()); n > int(limit) {
		return nil, height, fmt.Errorf("%w: %d entries for a page of %d", ErrResponse, n, limit)
	}
	return resp, height, nil
}

// Governance returns one page of svn.authority.v1.Query/Governance (the
// submitters with their roles, and the devnet seats) and the height it was
// read at.
func (c *Client) Governance(ctx context.Context, pageKey []byte, limit uint32, minHeight int64) (*authorityv1.QueryGovernanceResponse, int64, error) {
	if err := checkPage(pageKey, limit); err != nil {
		return nil, 0, err
	}
	resp := &authorityv1.QueryGovernanceResponse{}
	found, height, err := c.query(ctx, pathGovernance, &authorityv1.QueryGovernanceRequest{Page: &typesv1.PageRequest{Limit: limit, PageKey: pageKey}}, resp, minHeight)
	if err != nil {
		return nil, height, err
	}
	if !found || resp.GetState() == nil {
		return nil, height, fmt.Errorf("%w: %s answered without a state", ErrResponse, pathGovernance)
	}
	return resp, height, nil
}

// Account is an account's number and sequence at the height they were
// read.
type Account struct {
	Number   uint64
	Sequence uint64
	Height   int64
}

// Account reads the committed account of a 20-byte address. The answer must
// be at least minHeight: a node answering below the height the caller
// needs is stale (StaleError, plan 8.1). The committed sequence does not
// count transactions waiting in a mempool. An unknown address is
// ErrNoAccount.
func (c *Client) Account(ctx context.Context, addr []byte, minHeight int64) (Account, error) {
	bech, err := c.Bech32(addr)
	if err != nil {
		return Account{}, err
	}
	data, err := gogoproto.Marshal(&authtypes.QueryAccountRequest{Address: bech})
	if err != nil {
		return Account{}, fmt.Errorf("chain: encode account query: %w", err)
	}
	value, height, found, err := c.abciQuery(ctx, pathAccount, data, minHeight)
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, fmt.Errorf("%w: %s at height %d", ErrNoAccount, bech, height)
	}
	var resp authtypes.QueryAccountResponse
	if err := c.enc.Codec.Unmarshal(value, &resp); err != nil {
		return Account{}, fmt.Errorf("%w: decode account: %v", ErrResponse, err)
	}
	var account sdk.AccountI
	if err := c.enc.InterfaceRegistry.UnpackAny(resp.Account, &account); err != nil || account == nil {
		return Account{}, fmt.Errorf("%w: account type", ErrResponse)
	}
	if string(account.GetAddress()) != string(addr) {
		return Account{}, fmt.Errorf("%w: account of another address", ErrResponse)
	}
	return Account{Number: account.GetAccountNumber(), Sequence: account.GetSequence(), Height: height}, nil
}

// SimulateResult is the gas a node measured for a simulated transaction.
type SimulateResult struct {
	GasWanted uint64
	GasUsed   uint64
	// Height is the committed height the simulation was answered at.
	Height int64
}

// Simulate asks the node to run raw (a SimulationTx) on its check state
// through the SDK transaction service's Simulate method, as an ABCI query.
// A rejected simulation is a QueryError; the node's gRPC error text
// carries the reason, not the svn code. The result trusts the one node; a
// wrong answer can only make the signed transaction fail out of gas.
func (c *Client) Simulate(ctx context.Context, raw []byte) (SimulateResult, error) {
	if len(raw) == 0 || uint64(len(raw)) > genesis.MaxTxBytes {
		return SimulateResult{}, fmt.Errorf("chain: transaction must be 1 to %d bytes", genesis.MaxTxBytes)
	}
	data, err := gogoproto.Marshal(&txtypes.SimulateRequest{TxBytes: raw})
	if err != nil {
		return SimulateResult{}, fmt.Errorf("chain: encode simulate request: %w", err)
	}
	value, height, found, err := c.abciQuery(ctx, pathSimulate, data, 0)
	if err != nil {
		return SimulateResult{}, err
	}
	if !found {
		return SimulateResult{}, fmt.Errorf("%w: %s answered not found", ErrResponse, pathSimulate)
	}
	// gogoproto.Unmarshal does not unpack the result's message-response
	// Anys, whose svn types the interface registry does not know.
	var resp txtypes.SimulateResponse
	if err := gogoproto.Unmarshal(value, &resp); err != nil {
		return SimulateResult{}, fmt.Errorf("%w: decode %s: %v", ErrResponse, pathSimulate, err)
	}
	if resp.GasInfo == nil || resp.GasInfo.GasUsed < 1 {
		return SimulateResult{}, fmt.Errorf("%w: %s answered without gas used", ErrResponse, pathSimulate)
	}
	return SimulateResult{GasWanted: resp.GasInfo.GasWanted, GasUsed: resp.GasInfo.GasUsed, Height: height}, nil
}

// Broadcast sends raw with broadcast_tx_sync and classifies the answer
// (ClassifyBroadcast). The error is non-nil only when raw is refused before
// anything is sent.
func (c *Client) Broadcast(ctx context.Context, raw []byte) (SendObservation, error) {
	if len(raw) == 0 || uint64(len(raw)) > genesis.MaxTxBytes {
		return SendObservation{}, fmt.Errorf("chain: transaction must be 1 to %d bytes", genesis.MaxTxBytes)
	}
	res, err := c.rpc.BroadcastTxSync(ctx, cmttypes.Tx(raw))
	return ClassifyBroadcast(res, err, sha256.Sum256(raw)), nil
}

// notFoundText is the error the tx route returns for a hash no block holds
// (CometBFT rpc/core/tx.go), the only form that answer has.
func notFoundText(hash []byte) string {
	return fmt.Sprintf("tx (%X) not found", hash)
}

// committedFrom checks one /tx or /tx_search entry and copies it. The
// entry's bytes must hash to its hash, which must equal want when want is
// set; a mismatch is an IntegrityError.
func committedFrom(res *coretypes.ResultTx, want []byte) (Committed, error) {
	if res == nil || res.Height < 1 {
		return Committed{}, fmt.Errorf("%w: committed transaction without a height", ErrResponse)
	}
	if len(res.Tx) == 0 || uint64(len(res.Tx)) > genesis.MaxTxBytes {
		return Committed{}, fmt.Errorf("%w: committed transaction of %d bytes", ErrResponse, len(res.Tx))
	}
	sum := sha256.Sum256(res.Tx)
	if len(res.Hash) != sha256.Size || string(res.Hash) != string(sum[:]) {
		return Committed{}, &IntegrityError{Reason: fmt.Sprintf("node returned bytes hashing to %X for hash %X", sum, []byte(res.Hash))}
	}
	if want != nil && string(want) != string(sum[:]) {
		return Committed{}, &IntegrityError{Reason: fmt.Sprintf("node returned transaction %X for requested %X", sum, want)}
	}
	r := res.TxResult
	return Committed{
		Hash: sum, Height: res.Height, Index: res.Index, Code: r.Code, Codespace: clip(r.Codespace), Log: clip(r.Log),
		GasWanted: r.GasWanted, GasUsed: r.GasUsed, Events: r.Events, Raw: append([]byte(nil), res.Tx...),
	}, nil
}

// CommittedTx returns the committed result of hash (the tx route). found
// is false while no block holds it, or while the node has not indexed it
// yet (indexing follows the commit). The returned bytes must hash to hash.
func (c *Client) CommittedTx(ctx context.Context, hash [32]byte) (Committed, bool, error) {
	res, err := c.rpc.Tx(ctx, hash[:], false)
	if err != nil {
		var rpcErr *rpctypes.RPCError
		if errors.As(err, &rpcErr) && rpcErr.Data == notFoundText(hash[:]) {
			return Committed{}, false, nil
		}
		return Committed{}, false, fmt.Errorf("chain: tx: %w", err)
	}
	com, err := committedFrom(res, hash[:])
	if err != nil {
		return Committed{}, false, err
	}
	return com, true, nil
}

// TxSearch returns every committed transaction the query matches, in
// height order, reading at most pageBudget pages of 100. A query is at
// most 512 bytes (CometBFT's limit). If the node reports more matches than
// the budget reads, the result is ErrSearchIncomplete: a partial list is
// never returned, because a missing match could hide an effect.
func (c *Client) TxSearch(ctx context.Context, query string, pageBudget int) ([]Committed, error) {
	if query == "" || len(query) > maxQueryBytes {
		return nil, fmt.Errorf("chain: tx_search query must be 1 to %d bytes", maxQueryBytes)
	}
	if pageBudget < 1 || pageBudget > MaxSearchPageBudget {
		return nil, fmt.Errorf("chain: tx_search page budget must be 1 to %d", MaxSearchPageBudget)
	}
	var out []Committed
	seen := map[[32]byte]bool{}
	for page := 1; page <= pageBudget; page++ {
		p, per := page, searchPerPage
		res, err := c.rpc.TxSearch(ctx, query, false, &p, &per, "asc")
		if err != nil {
			return nil, fmt.Errorf("chain: tx_search: %w", err)
		}
		if res == nil || res.TotalCount < 0 || len(res.Txs) > searchPerPage {
			return nil, fmt.Errorf("%w: malformed tx_search page", ErrResponse)
		}
		for _, entry := range res.Txs {
			com, err := committedFrom(entry, nil)
			if err != nil {
				return nil, err
			}
			if seen[com.Hash] {
				return nil, fmt.Errorf("%w: tx_search repeats %X", ErrResponse, com.Hash)
			}
			seen[com.Hash] = true
			out = append(out, com)
		}
		if len(out) >= res.TotalCount {
			return out, nil
		}
		if len(res.Txs) == 0 {
			return nil, fmt.Errorf("%w: tx_search page %d is empty below its total %d", ErrResponse, page, res.TotalCount)
		}
	}
	return nil, ErrSearchIncomplete
}

// AccSeqQuery is the tx_search query for the transaction that used
// sequence seq of signer: tx.acc_seq='<bech32>/<seq>' (SDK SetPubKey
// decorator).
func (c *Codec) AccSeqQuery(signer []byte, seq uint64) (string, error) {
	bech, err := c.Bech32(signer)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tx.acc_seq='%s/%d'", bech, seq), nil
}

// SequenceUser returns the committed transaction that consumed sequence
// seq of signer, if the node indexed one. Its tx.acc_seq attribute must
// name exactly that signer and sequence. Only a transaction whose whole
// ante passed keeps its acc_seq event (plan 1.1), so at most one committed
// transaction can carry it; two are an IntegrityError.
func (c *Client) SequenceUser(ctx context.Context, signer []byte, seq uint64) (Committed, bool, error) {
	query, err := c.AccSeqQuery(signer, seq)
	if err != nil {
		return Committed{}, false, err
	}
	hits, err := c.TxSearch(ctx, query, 1)
	if err != nil {
		return Committed{}, false, err
	}
	switch len(hits) {
	case 0:
		return Committed{}, false, nil
	case 1:
	default:
		return Committed{}, false, &IntegrityError{Reason: fmt.Sprintf("%d committed transactions carry %s", len(hits), query)}
	}
	seqs, err := c.AccSeqs(hits[0].Events)
	if err != nil {
		return Committed{}, false, err
	}
	if len(seqs) != 1 || string(seqs[0].Address) != string(signer) || seqs[0].Sequence != seq {
		return Committed{}, false, &IntegrityError{Reason: fmt.Sprintf("tx_search hit %X does not carry %s", hits[0].Hash, query)}
	}
	return hits[0], true, nil
}

// BlockID is a committed block's identity at one height.
type BlockID struct {
	Height  int64
	Hash    []byte
	Time    time.Time
	AppHash []byte
}

// Commit returns the identity of the block at height (the commit route).
// The signed header must validate for the configured chain id
// (SignedHeader.ValidateBasic: header and commit agree on height and block
// hash). Validator signatures are not verified: the answer trusts the one
// node.
func (c *Client) Commit(ctx context.Context, height int64) (BlockID, error) {
	if height < 1 {
		return BlockID{}, errors.New("chain: block height must be at least 1")
	}
	h := height
	res, err := c.rpc.Commit(ctx, &h)
	if err != nil {
		return BlockID{}, fmt.Errorf("chain: commit: %w", err)
	}
	if res == nil {
		return BlockID{}, fmt.Errorf("%w: empty commit answer", ErrResponse)
	}
	if err := res.SignedHeader.ValidateBasic(c.chainID); err != nil {
		return BlockID{}, fmt.Errorf("%w: commit: %v", ErrResponse, err)
	}
	if res.Header.Height != height {
		return BlockID{}, fmt.Errorf("%w: commit of height %d for %d", ErrResponse, res.Header.Height, height)
	}
	return BlockID{
		Height:  height,
		Hash:    append([]byte(nil), res.Commit.BlockID.Hash...),
		Time:    res.Header.Time.UTC(),
		AppHash: append([]byte(nil), res.Header.AppHash...),
	}, nil
}
