package worker

// fakeNode is a unit-test fake (never end-to-end evidence): it stands in for
// one CometBFT node and the svn application behind it, in process, as the
// chain.RPC interface, so the real internal/chain client (hash checks,
// classification, height binding) runs on top of it. It keeps accounts with
// sequences, a mempool, committed blocks and their results with the ante's
// tx.acc_seq event and the typed svn.events.v1 events, a tx_search index,
// and a minimal model of content records, providers, open offers and held
// leases. Its rules are simplified: CheckTx checks the sequence and the
// timeout height; a block includes every mempool transaction whose sequence
// and deadline still fit; the default execution succeeds once per business
// id (the same payload again is idempotent, another payload is REPLAY).
// Tests change any behaviour through the hooks.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p"
	cmtversionpb "github.com/cometbft/cometbft/proto/tendermint/version"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	cmtversion "github.com/cometbft/cometbft/version"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
)

type fakeAccount struct {
	number, seq uint64
}

// outcome is a committed transaction's result as the fake application
// decides it.
type outcome struct {
	code      uint32
	codespace string
	events    []abci.Event
	// consumed: the ante passed, so the sequence is consumed and the
	// result carries tx.acc_seq.
	consumed bool
}

type fakeNode struct {
	t     *testing.T
	codec *chain.Codec
	enc   app.Encoding

	mu     sync.Mutex
	height int64
	// queryLag is how many blocks below the /status height an ABCI query
	// answers: 1 is CometBFT while the reported block executes
	// (chain.Status.AppFloor).
	queryLag   int64
	when       time.Time
	statusErr  error
	catchingUp bool
	accounts   map[string]*fakeAccount
	mempool    [][]byte
	txs        map[[32]byte]*coretypes.ResultTx
	order      [][32]byte
	contents   map[string]*contentv1.QueryContentResponse
	hidden     map[string]bool
	providers  map[string]*providersv1.Provider
	offers     map[string]*leasesv1.Offer
	leases     map[string]*leasesv1.Lease
	business   map[string][]byte
	governance *authorityv1.AuthorityState
	simGas     uint64
	broadcasts [][32]byte

	// beforeBroadcast runs, outside the lock, before a broadcast is
	// answered.
	beforeBroadcast func(raw []byte)
	// onBroadcast answers a broadcast instead of the CheckTx model when it
	// returns handled.
	onBroadcast func(raw []byte) (res *coretypes.ResultBroadcastTx, err error, handled bool)
	// execute decides a transaction in a block instead of the default
	// model when it returns handled.
	execute func(d chain.DecodedTx, raw []byte) (out outcome, handled bool)
}

func newFakeNode(t *testing.T, codec *chain.Codec) *fakeNode {
	t.Helper()
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	return &fakeNode{t: t, codec: codec, enc: enc, height: 10, when: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC),
		accounts: map[string]*fakeAccount{}, txs: map[[32]byte]*coretypes.ResultTx{}, contents: map[string]*contentv1.QueryContentResponse{},
		hidden: map[string]bool{}, providers: map[string]*providersv1.Provider{}, offers: map[string]*leasesv1.Offer{},
		leases: map[string]*leasesv1.Lease{}, business: map[string][]byte{}, governance: &authorityv1.AuthorityState{},
		simGas: 90_000}
}

var _ chain.RPC = (*fakeNode)(nil)

func (n *fakeNode) bech(addr []byte) string {
	s, err := n.codec.Bech32(addr)
	if err != nil {
		n.t.Fatal(err)
	}
	return s
}

// addAccount creates an account with number and sequence 0.
func (n *fakeNode) addAccount(addr []byte, number uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.accounts[n.bech(addr)] = &fakeAccount{number: number}
}

// accountNumber returns an account's number.
func (n *fakeNode) accountNumber(addr []byte) uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.accounts[n.bech(addr)].number
}

// sequence returns an account's committed sequence.
func (n *fakeNode) sequence(addr []byte) uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.accounts[n.bech(addr)].seq
}

func (n *fakeNode) setStatusErr(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.statusErr = err
}

func (n *fakeNode) heightNow() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.height
}

func (n *fakeNode) broadcastCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.broadcasts)
}

func (n *fakeNode) broadcastHashes() [][32]byte {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([][32]byte(nil), n.broadcasts...)
}

func (n *fakeNode) mempoolSize() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.mempool)
}

// Status implements chain.RPC.
func (n *fakeNode) Status(context.Context) (*coretypes.ResultStatus, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.statusErr != nil {
		return nil, n.statusErr
	}
	return &coretypes.ResultStatus{NodeInfo: p2p.DefaultNodeInfo{Network: n.codec.ChainID()},
		SyncInfo: coretypes.SyncInfo{LatestBlockHeight: n.height, LatestBlockTime: n.when, CatchingUp: n.catchingUp}}, nil
}

// ABCIQueryWithOptions implements chain.RPC for the query paths the chain
// client uses.
func (n *fakeNode) ABCIQueryWithOptions(_ context.Context, path string, data cmtbytes.HexBytes, _ rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	value, found, err := n.query(path, data)
	if err != nil {
		return nil, err
	}
	resp := abci.ResponseQuery{Height: n.height - n.queryLag, Value: value}
	if !found {
		resp.Code, resp.Codespace, resp.Value = sdkerrors.ErrKeyNotFound.ABCICode(), sdkerrors.ErrKeyNotFound.Codespace(), nil
	}
	return &coretypes.ResultABCIQuery{Response: resp}, nil
}

func (n *fakeNode) query(path string, data []byte) ([]byte, bool, error) {
	marshal := func(m proto.Message) ([]byte, bool, error) {
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		return b, true, err
	}
	switch path {
	case "/cosmos.auth.v1beta1.Query/Account":
		var req authtypes.QueryAccountRequest
		if err := gogoproto.Unmarshal(data, &req); err != nil {
			return nil, false, err
		}
		acct, ok := n.accounts[req.Address]
		if !ok {
			return nil, false, nil
		}
		addr, err := sdk.GetFromBech32(req.Address, strings.SplitN(req.Address, "1", 2)[0])
		if err != nil {
			return nil, false, err
		}
		packed, err := codectypes.NewAnyWithValue(authtypes.NewBaseAccount(addr, nil, acct.number, acct.seq))
		if err != nil {
			return nil, false, err
		}
		b, err := n.enc.Codec.Marshal(&authtypes.QueryAccountResponse{Account: packed})
		return b, true, err
	case "/svn.content.v1.Query/Content":
		var req contentv1.QueryContentRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, false, err
		}
		key := contentKey(req.GetContentId(), req.GetContentVersion())
		resp, ok := n.contents[key]
		if !ok || n.hidden[key] {
			return nil, false, nil
		}
		return marshal(resp)
	case "/svn.providers.v1.Query/Provider":
		var req providersv1.QueryProviderRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, false, err
		}
		p, ok := n.providers[req.GetProviderId()]
		if !ok {
			return nil, false, nil
		}
		return marshal(&providersv1.QueryProviderResponse{Provider: p})
	case "/svn.leases.v1.Query/ActiveLeases":
		var req leasesv1.QueryActiveLeasesRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, false, err
		}
		resp := &leasesv1.QueryActiveLeasesResponse{}
		for _, id := range sortedKeys(n.leases) {
			l := n.leases[id]
			if l.GetProviderId() == req.GetProviderId() && l.GetContentId() == req.GetContentId() {
				resp.Leases = append(resp.Leases, l)
			}
		}
		return marshal(resp)
	case "/svn.leases.v1.Query/Deadlines":
		resp := &leasesv1.QueryDeadlinesResponse{Page: &typesv1.PageResponse{}}
		for _, id := range sortedKeys(n.offers) {
			resp.Offers = append(resp.Offers, n.offers[id])
		}
		for _, id := range sortedKeys(n.leases) {
			resp.Leases = append(resp.Leases, n.leases[id])
		}
		return marshal(resp)
	case "/svn.authority.v1.Query/Governance":
		return marshal(&authorityv1.QueryGovernanceResponse{State: n.governance, Page: &typesv1.PageResponse{}})
	case "/cosmos.tx.v1beta1.Service/Simulate":
		b, err := gogoproto.Marshal(&txtypes.SimulateResponse{GasInfo: &sdk.GasInfo{GasUsed: n.simGas}, Result: &sdk.Result{}})
		return b, true, err
	}
	return nil, false, fmt.Errorf("fake node: query path %s not served", path)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contentKey(id string, version uint64) string { return id + "/" + strconv.FormatUint(version, 10) }

// rpcInternal is the JSON-RPC error CometBFT returns for a handler error.
func rpcInternal(data string) *rpctypes.RPCError {
	return &rpctypes.RPCError{Code: -32603, Message: "Internal error", Data: data}
}

// BroadcastTxSync implements chain.RPC.
func (n *fakeNode) BroadcastTxSync(_ context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	raw := []byte(tx)
	if n.beforeBroadcast != nil {
		n.beforeBroadcast(raw)
	}
	n.mu.Lock()
	n.broadcasts = append(n.broadcasts, sha256.Sum256(raw))
	hook := n.onBroadcast
	n.mu.Unlock()
	if hook != nil {
		if res, err, handled := hook(raw); handled {
			return res, err
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.checkTx(raw)
}

// checkTx is the CheckTx model: a transaction in the mempool or committed
// is the cache error; a wrong sequence is sdk 32; a passed timeout height is
// sdk 30; otherwise code 0 and the mempool takes it.
func (n *fakeNode) checkTx(raw []byte) (*coretypes.ResultBroadcastTx, error) {
	hash := sha256.Sum256(raw)
	if _, ok := n.txs[hash]; ok {
		return nil, rpcInternal("tx already exists in cache")
	}
	for _, m := range n.mempool {
		if bytes.Equal(m, raw) {
			return nil, rpcInternal("tx already exists in cache")
		}
	}
	res := &coretypes.ResultBroadcastTx{Hash: hash[:]}
	d, err := n.codec.Decode(raw)
	if err != nil {
		res.Code, res.Codespace = sdkerrors.ErrTxDecode.ABCICode(), "sdk"
		return res, nil
	}
	signerInfo := d.Signers[0]
	acct, ok := n.accounts[n.bech(signerInfo.Address)]
	if !ok {
		res.Code, res.Codespace = sdkerrors.ErrUnauthorized.ABCICode(), "sdk"
		return res, nil
	}
	pending := uint64(0)
	for _, m := range n.mempool {
		if md, err := n.codec.Decode(m); err == nil && bytes.Equal(md.Signers[0].Address, signerInfo.Address) {
			pending++
		}
	}
	switch {
	case signerInfo.Sequence != acct.seq+pending:
		res.Code, res.Codespace = sdkerrors.ErrWrongSequence.ABCICode(), "sdk"
	case d.TimeoutHeight > 0 && d.TimeoutHeight <= uint64(n.height):
		res.Code, res.Codespace = sdkerrors.ErrTxTimeoutHeight.ABCICode(), "sdk"
	default:
		n.mempool = append(n.mempool, append([]byte(nil), raw...))
	}
	return res, nil
}

// block commits one block at the next height: every mempool transaction
// whose sequence is the signer's next and whose timeout height is not
// passed is executed; the others leave the mempool (recheck).
func (n *fakeNode) block() {
	n.mu.Lock()
	n.height++
	n.when = n.when.Add(time.Second)
	pool := n.mempool
	n.mempool = nil
	exec := n.execute
	n.mu.Unlock()
	var index uint32
	for _, raw := range pool {
		d, err := n.codec.Decode(raw)
		if err != nil {
			continue
		}
		n.mu.Lock()
		acct := n.accounts[n.bech(d.Signers[0].Address)]
		fits := acct != nil && d.Signers[0].Sequence == acct.seq && (d.TimeoutHeight == 0 || d.TimeoutHeight >= uint64(n.height))
		n.mu.Unlock()
		if !fits {
			continue
		}
		var out outcome
		handled := false
		if exec != nil {
			out, handled = exec(d, raw)
		}
		n.mu.Lock()
		if !handled {
			out = n.defaultExecute(d)
		}
		n.commitLocked(raw, d, out, index)
		n.mu.Unlock()
		index++
	}
}

// blocks commits k blocks.
func (n *fakeNode) blocks(k int) {
	for i := 0; i < k; i++ {
		n.block()
	}
}

// commitLocked records a committed transaction at the current height.
func (n *fakeNode) commitLocked(raw []byte, d chain.DecodedTx, out outcome, index uint32) {
	hash := sha256.Sum256(raw)
	events := out.events
	if out.consumed {
		acct := n.accounts[n.bech(d.Signers[0].Address)]
		events = append([]abci.Event{{Type: sdk.EventTypeTx, Attributes: []abci.EventAttribute{{Key: sdk.AttributeKeyAccountSequence,
			Value: fmt.Sprintf("%s/%d", n.bech(d.Signers[0].Address), d.Signers[0].Sequence), Index: true}}}}, events...)
		acct.seq++
	}
	n.txs[hash] = &coretypes.ResultTx{Hash: hash[:], Height: n.height, Index: index, Tx: raw,
		TxResult: abci.ExecTxResult{Code: out.code, Codespace: out.codespace, Events: events, GasWanted: int64(d.Gas), GasUsed: 60_000}}
	n.order = append(n.order, hash)
}

// commitDirect commits raw in a new block without the mempool: another
// account's or a foreign use of a key, outside the broadcaster.
func (n *fakeNode) commitDirect(raw []byte) [32]byte {
	n.mu.Lock()
	n.mempool = append(n.mempool, append([]byte(nil), raw...))
	n.mu.Unlock()
	n.block()
	return sha256.Sum256(raw)
}

// committedCode returns the committed code of hash, and whether it is
// committed.
func (n *fakeNode) committedCode(hash [32]byte) (uint32, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	r, ok := n.txs[hash]
	if !ok {
		return 0, false
	}
	return r.TxResult.Code, true
}

// svnFailure is a handler failure with the svn code c: the ante passed.
func svnFailure(c typesv1.ErrorCode) outcome {
	return outcome{code: uint32(c), codespace: "svn", consumed: true}
}

// defaultExecute is the default application model.
func (n *fakeNode) defaultExecute(d chain.DecodedTx) outcome {
	msg := d.Msgs[0].Msg
	bid, module := businessOf(msg)
	payload := payloadOf(msg)
	if prev, ok := n.business[bid]; ok {
		if bytes.Equal(prev, payload) {
			return outcome{consumed: true, events: []abci.Event{{Type: module + "_result", Attributes: []abci.EventAttribute{
				{Key: "idempotent", Value: "true", Index: true}, {Key: msgkit.MsgIndexKey, Value: "0", Index: true}}}}}
		}
		return svnFailure(typesv1.ErrorCode_ERROR_CODE_REPLAY)
	}
	var ev proto.Message
	switch m := msg.(type) {
	case *contentv1.MsgRegisterContent:
		key := contentKey(m.GetContentId(), m.GetContentVersion())
		if _, ok := n.contents[key]; ok {
			return svnFailure(typesv1.ErrorCode_ERROR_CODE_DUPLICATE_ID)
		}
		n.contents[key] = &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: m.GetContentId(),
			ContentVersion: m.GetContentVersion(), ManifestDigest: m.GetManifestDigest(), PackageBytes: m.GetPackageBytes(),
			SubmittingHost: m.GetSigner()}}
		ev = &eventsv1.ContentRegistered{ContentId: m.GetContentId(), ContentVersion: m.GetContentVersion(),
			ManifestSha256: msgkit.Hex(m.GetManifestDigest().GetValue()), PackageBytes: strconv.FormatUint(m.GetPackageBytes(), 10)}
	case *contentv1.MsgWithdrawContent:
		key := contentKey(m.GetContentId(), m.GetContentVersion())
		if c, ok := n.contents[key]; !ok || c.GetContent() == nil {
			return svnFailure(typesv1.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		n.contents[key] = &contentv1.QueryContentResponse{Tombstone: &contentv1.Tombstone{ContentId: m.GetContentId(),
			ContentVersion: m.GetContentVersion()}}
		ev = &eventsv1.ContentWithdrawn{ContentId: m.GetContentId(), ContentVersion: m.GetContentVersion()}
	case *leasesv1.MsgOfferLease:
		if _, ok := n.offers[m.GetOfferId()]; ok {
			return svnFailure(typesv1.ErrorCode_ERROR_CODE_DUPLICATE_ID)
		}
		n.offers[m.GetOfferId()] = &leasesv1.Offer{OfferId: m.GetOfferId(), ContentId: m.GetContentId(),
			ContentVersion: m.GetContentVersion(), ProviderId: m.GetProviderId(), ManifestDigest: m.GetManifestDigest(),
			PackageBytes: m.GetPackageBytes(), TermsDigest: m.GetTermsDigest(), StartUnixSeconds: m.GetStartUnixSeconds(),
			DeadlineUnixSeconds: m.GetDeadlineUnixSeconds(), ExpiryUnixSeconds: m.GetExpiryUnixSeconds(),
			TermVersion: m.GetTermVersion(), Scheduler: m.GetSigner()}
		ev = &eventsv1.LeaseOffered{OfferId: m.GetOfferId(), ProviderId: m.GetProviderId(), ContentId: m.GetContentId()}
	case *leasesv1.MsgAcceptLease:
		o, ok := n.offers[m.GetOfferId()]
		if !ok {
			return svnFailure(typesv1.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		delete(n.offers, m.GetOfferId())
		n.leases[m.GetLeaseId()] = &leasesv1.Lease{LeaseId: m.GetLeaseId(), OfferId: o.GetOfferId(), ProviderId: o.GetProviderId(),
			ContentId: o.GetContentId(), ContentVersion: o.GetContentVersion(), PackageBytes: o.GetPackageBytes(),
			DeadlineUnixSeconds: o.GetDeadlineUnixSeconds(), TermVersion: 1}
		ev = &eventsv1.LeaseAccepted{LeaseId: m.GetLeaseId(), ProviderId: o.GetProviderId(), ContentId: o.GetContentId(),
			ContentVersion: o.GetContentVersion(), PackageBytes: strconv.FormatUint(o.GetPackageBytes(), 10),
			DeadlineUnixSeconds: o.GetDeadlineUnixSeconds()}
	case *leasesv1.MsgRenewLease:
		l, ok := n.leases[m.GetLeaseId()]
		if !ok {
			return svnFailure(typesv1.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		l.TermVersion, l.DeadlineUnixSeconds = m.GetTermVersion(), m.GetDeadlineUnixSeconds()
		ev = &eventsv1.LeaseRenewed{LeaseId: m.GetLeaseId(), TermVersion: m.GetTermVersion(), DeadlineUnixSeconds: m.GetDeadlineUnixSeconds()}
	case *leasesv1.MsgEndLease:
		if _, ok := n.leases[m.GetLeaseId()]; !ok {
			return svnFailure(typesv1.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		delete(n.leases, m.GetLeaseId())
		ev = &eventsv1.LeaseEnded{LeaseId: m.GetLeaseId(), State: "ENDED", ReleasedBytes: "0"}
	default:
		return svnFailure(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN)
	}
	n.business[bid] = payload
	return outcome{consumed: true, events: []abci.Event{typedEvent(n.t, ev, 0)}}
}

// businessOf returns a message's business id and module.
func businessOf(msg sdk.Msg) (string, string) {
	switch m := msg.(type) {
	case *contentv1.MsgRegisterContent:
		return m.GetBusinessId(), content.ModuleName
	case *contentv1.MsgWithdrawContent:
		return m.GetBusinessId(), content.ModuleName
	case *leasesv1.MsgOfferLease:
		return m.GetBusinessId(), leases.ModuleName
	case *leasesv1.MsgAcceptLease:
		return m.GetBusinessId(), leases.ModuleName
	case *leasesv1.MsgRenewLease:
		return m.GetBusinessId(), leases.ModuleName
	case *leasesv1.MsgEndLease:
		return m.GetBusinessId(), leases.ModuleName
	}
	return "", ""
}

// payloadOf is a message without its signer: the fake's business digest
// (relayed lease messages stay idempotent across relayers, D-13).
func payloadOf(msg sdk.Msg) []byte {
	m, ok := msg.(proto.Message)
	if !ok {
		return nil
	}
	c := proto.Clone(m)
	r := c.ProtoReflect()
	if fd := r.Descriptor().Fields().ByName("signer"); fd != nil {
		r.Clear(fd)
	}
	b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(c)
	return b
}

// typedEvent emits ev with the chain's own emitter and appends the
// msg_index attribute baseapp adds to a committed message event.
func typedEvent(t testing.TB, ev proto.Message, msgIndex int) abci.Event {
	t.Helper()
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	if err := msgkit.EmitTyped(ctx, ev); err != nil {
		t.Fatal(err)
	}
	events := ctx.EventManager().ABCIEvents()
	if len(events) != 1 {
		t.Fatalf("EmitTyped wrote %d events", len(events))
	}
	out := events[0]
	out.Attributes = append(out.Attributes, abci.EventAttribute{Key: msgkit.MsgIndexKey, Value: strconv.Itoa(msgIndex), Index: true})
	return out
}

// Tx implements chain.RPC.
func (n *fakeNode) Tx(_ context.Context, hash []byte, _ bool) (*coretypes.ResultTx, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var h [32]byte
	copy(h[:], hash)
	r, ok := n.txs[h]
	if !ok || len(hash) != 32 {
		return nil, rpcInternal(fmt.Sprintf("tx (%X) not found", hash))
	}
	out := *r
	return &out, nil
}

// TxSearch implements chain.RPC for one condition key='value', where key
// is <event type>.<attribute>.
func (n *fakeNode) TxSearch(_ context.Context, query string, _ bool, page, perPage *int, _ string) (*coretypes.ResultTxSearch, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	cut := strings.Index(query, "='")
	if cut < 0 || !strings.HasSuffix(query, "'") {
		return nil, rpcInternal("fake node: unsupported query")
	}
	key, value := query[:cut], query[cut+2:len(query)-1]
	dot := strings.LastIndexByte(key, '.')
	typ, attr := key[:dot], key[dot+1:]
	var hits []*coretypes.ResultTx
	for _, h := range n.order {
		r := n.txs[h]
		for _, ev := range r.TxResult.Events {
			matched := false
			for _, a := range ev.Attributes {
				if ev.Type == typ && a.Key == attr && a.Value == value {
					matched = true
				}
			}
			if matched {
				out := *r
				hits = append(hits, &out)
				break
			}
		}
	}
	p, per := *page, *perPage
	start := (p - 1) * per
	if start > len(hits) {
		start = len(hits)
	}
	end := min(start+per, len(hits))
	return &coretypes.ResultTxSearch{Txs: hits[start:end], TotalCount: len(hits)}, nil
}

// Commit implements chain.RPC with a signed header that passes
// SignedHeader.ValidateBasic; its hashes are filler.
func (n *fakeNode) Commit(_ context.Context, height *int64) (*coretypes.ResultCommit, error) {
	h32 := func(s string) []byte { sum := sha256.Sum256([]byte(s)); return sum[:] }
	header := &cmttypes.Header{
		Version: cmtversionpb.Consensus{Block: cmtversion.BlockProtocol, App: 1}, ChainID: n.codec.ChainID(), Height: *height,
		Time:           time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC),
		LastBlockID:    cmttypes.BlockID{Hash: h32("last"), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: h32("last parts")}},
		LastCommitHash: h32("lc"), DataHash: h32("data"), ValidatorsHash: h32("vals"), NextValidatorsHash: h32("vals"),
		ConsensusHash: h32("cons"), AppHash: h32(fmt.Sprintf("app %d", *height)), LastResultsHash: h32("res"),
		EvidenceHash: h32("ev"), ProposerAddress: h32("proposer")[:20],
	}
	commit := &cmttypes.Commit{Height: *height, BlockID: cmttypes.BlockID{Hash: header.Hash(), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: h32("parts")}},
		Signatures: []cmttypes.CommitSig{{BlockIDFlag: cmttypes.BlockIDFlagCommit, ValidatorAddress: h32("proposer")[:20], Timestamp: header.Time, Signature: h32("sig")}}}
	return &coretypes.ResultCommit{SignedHeader: cmttypes.SignedHeader{Header: header, Commit: commit}, CanonicalCommit: true}, nil
}

// blockHash is the block hash Commit reports for height.
func (n *fakeNode) blockHash(t *testing.T, height int64) []byte {
	t.Helper()
	res, err := n.Commit(context.Background(), &height)
	if err != nil {
		t.Fatal(err)
	}
	return res.Commit.BlockID.Hash
}
