// Package chaintest is an in-memory fake of the CometBFT RPC methods that
// package chainclient uses, for unit tests only.
//
// Label: "unit-test fake chain RPC; not a node, not end-to-end evidence".
// It answers Status, gRPC-routed ABCI queries, broadcast_tx_sync, and tx
// from maps the test fills. It runs no handler: a test decides what a
// broadcast does (Node.OnBroadcast) and writes the records it expects. Only
// tests import this package (TestChaintestOnlyInTests).
package chaintest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Label names what this package is, for test logs.
const Label = "unit-test fake chain RPC; not a node, not end-to-end evidence"

// Tx is one broadcast transaction.
type Tx struct {
	Raw []byte
	// Committed is set once a block holds the transaction.
	Committed bool
	Height    int64
	Code      uint32
	Codespace string
}

// Account is an auth account.
type Account struct {
	Number, Sequence uint64
}

// Node is the fake. Fields are guarded by mu; tests change them through
// the methods or under Lock.
type Node struct {
	mu         sync.Mutex
	ChainID    string
	Height     int64
	Time       int64
	CatchingUp bool
	// Leases is keyed by provider_id + "/" + content_id.
	Leases map[string]*leasesv1.Lease
	// Contents is keyed by content_id + "/" + version.
	Contents  map[string]*contentv1.QueryContentResponse
	Providers map[string]*providersv1.Provider
	Records   map[string]*evidencev1.EvidenceRecord
	// Accounts is keyed by hex address.
	Accounts map[string]*Account
	// Txs is keyed by lowercase hex hash.
	Txs map[string]*Tx
	// Ack is the CheckTx answer of the next broadcasts (0: accepted).
	Ack          uint32
	AckCodespace string
	// OnBroadcast runs after an accepted broadcast, with the node locked;
	// it may commit the transaction (Commit) and write records.
	OnBroadcast func(n *Node, msg *evidencev1.MsgSubmitAuditBatch, hash string)
	// Calls logs every method call in order.
	Calls []string
	// BroadcastErr makes the next broadcast fail at the transport.
	BroadcastErr error
	// Mempool makes BroadcastTxSync answer as a node's mempool does while
	// a stored transaction waits uncommitted: the same bytes again are the
	// JSON-RPC error "tx already exists in cache", and other bytes with the
	// waiting one's signer and sequence are CheckTx code 32 in codespace
	// sdk (account sequence mismatch). Off by default.
	Mempool bool
	enc     app.Encoding
}

// New returns an empty node at height 1 and the given header time.
func New(chainID string, headerTime int64) *Node {
	enc, err := app.MakeEncoding()
	if err != nil {
		panic(err)
	}
	return &Node{
		ChainID: chainID, Height: 1, Time: headerTime,
		Leases: map[string]*leasesv1.Lease{}, Contents: map[string]*contentv1.QueryContentResponse{},
		Providers: map[string]*providersv1.Provider{}, Records: map[string]*evidencev1.EvidenceRecord{},
		Accounts: map[string]*Account{}, Txs: map[string]*Tx{}, enc: enc,
	}
}

// Lock and Unlock guard direct field changes by tests.
func (n *Node) Lock()   { n.mu.Lock() }
func (n *Node) Unlock() { n.mu.Unlock() }

// Advance moves the header by blocks blocks of seconds each.
func (n *Node) Advance(blocks int64, seconds int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Height += blocks
	n.Time += blocks * seconds
}

// CallCount counts logged calls of a method name.
func (n *Node) CallCount(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, c := range n.Calls {
		if c == method || strings.HasPrefix(c, method+" ") {
			count++
		}
	}
	return count
}

// Commit marks a transaction committed in the next block with code. The
// caller holds the lock (inside OnBroadcast) or calls CommitLocked.
func (n *Node) Commit(hash string, code uint32, codespace string) {
	tx := n.Txs[hash]
	n.Height++
	tx.Committed, tx.Height, tx.Code, tx.Codespace = true, n.Height, code, codespace
	if code == 0 {
		if acc := n.accountOfTx(tx.Raw); acc != nil {
			acc.Sequence++
		}
	}
}

// CommitLocked is Commit for a test outside OnBroadcast.
func (n *Node) CommitLocked(hash string, code uint32, codespace string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Commit(hash, code, codespace)
}

func (n *Node) accountOfTx(raw []byte) *Account {
	msg, err := n.Message(raw)
	if err != nil {
		return nil
	}
	return n.Accounts[hex.EncodeToString(msg.GetSigner())]
}

// Message decodes the audit batch carried by a transaction with the app's
// own decoder.
func (n *Node) Message(raw []byte) (*evidencev1.MsgSubmitAuditBatch, error) {
	decoded, err := n.enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		return nil, err
	}
	msgs := decoded.GetMsgs()
	if len(msgs) != 1 {
		return nil, fmt.Errorf("chaintest: %d messages", len(msgs))
	}
	msg, ok := msgs[0].(*evidencev1.MsgSubmitAuditBatch)
	if !ok {
		return nil, fmt.Errorf("chaintest: message %T", msgs[0])
	}
	return msg, nil
}

// Record writes the records a committed batch would write (DEC-010: the
// state is RECORDED iff the digest equals the lease's manifest digest).
// It runs no check of the handler; tests use it to model a commit.
func (n *Node) Record(msg *evidencev1.MsgSubmitAuditBatch) {
	for _, it := range msg.GetItems() {
		state := evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED
		for _, l := range n.Leases {
			if l.GetLeaseId() == it.GetLeaseId() && string(l.GetManifestDigest().GetValue()) == string(it.GetObservationDigest().GetValue()) {
				state = evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED
			}
		}
		n.Records[it.GetEvidenceId()] = &evidencev1.EvidenceRecord{
			EvidenceId: it.GetEvidenceId(), BatchId: msg.GetBatchId(), Kind: it.GetKind(), State: state,
			SignerPubkey: msg.GetAuditorSignature().GetPublicKey(), LeaseId: it.GetLeaseId(),
			ContentId: it.GetContentId(), ContentVersion: it.GetContentVersion(),
			ObservationDigest: it.GetObservationDigest(), Epoch: msg.GetEpoch(),
			ExpiryUnixSeconds: it.GetExpiryUnixSeconds(), SchemaVersion: msg.GetSchemaVersion(), Attestation: it.GetAttestation(),
		}
	}
}

func (n *Node) log(call string) { n.Calls = append(n.Calls, call) }

// Status implements chainclient.RPC.
func (n *Node) Status(context.Context) (*coretypes.ResultStatus, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.log("status")
	return &coretypes.ResultStatus{
		NodeInfo: p2p.DefaultNodeInfo{Network: n.ChainID},
		SyncInfo: coretypes.SyncInfo{LatestBlockHeight: n.Height, LatestBlockTime: time.Unix(n.Time, 0).UTC(), CatchingUp: n.CatchingUp},
	}, nil
}

func notFound() *coretypes.ResultABCIQuery {
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{
		Code: sdkerrors.ErrKeyNotFound.ABCICode(), Codespace: sdkerrors.ErrKeyNotFound.Codespace(), Log: "not found",
	}}
}

func value(m proto.Message) (*coretypes.ResultABCIQuery, error) {
	raw, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: raw}}, nil
}

// ABCIQueryWithOptions implements chainclient.RPC for the five query paths
// chainclient uses.
func (n *Node) ABCIQueryWithOptions(_ context.Context, path string, data cmtbytes.HexBytes, _ rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.log("abci_query " + path)
	switch path {
	case "/svn.leases.v1.Query/ActiveLeases":
		var req leasesv1.QueryActiveLeasesRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		out := &leasesv1.QueryActiveLeasesResponse{}
		if l, ok := n.Leases[req.GetProviderId()+"/"+req.GetContentId()]; ok {
			out.Leases = append(out.Leases, proto.Clone(l).(*leasesv1.Lease))
		}
		return value(out)
	case "/svn.content.v1.Query/Content":
		var req contentv1.QueryContentRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		c, ok := n.Contents[fmt.Sprintf("%s/%d", req.GetContentId(), req.GetContentVersion())]
		if !ok {
			return notFound(), nil
		}
		return value(c)
	case "/svn.providers.v1.Query/Provider":
		var req providersv1.QueryProviderRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		p, ok := n.Providers[req.GetProviderId()]
		if !ok {
			return notFound(), nil
		}
		return value(&providersv1.QueryProviderResponse{Provider: p})
	case "/svn.evidence.v1.Query/Evidence":
		var req evidencev1.QueryEvidenceRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		out := &evidencev1.QueryEvidenceResponse{}
		if r, ok := n.Records[req.GetEvidenceId()]; ok {
			rec := proto.Clone(r).(*evidencev1.EvidenceRecord)
			rec.State = evidence.ReportedState(rec, n.Time)
			out.Records = append(out.Records, rec)
		}
		return value(out)
	case "/cosmos.auth.v1beta1.Query/Account":
		var req authtypes.QueryAccountRequest
		if err := gogoproto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		addr, err := addresscodec.NewBech32Codec(chainaddress.Account).StringToBytes(req.Address)
		if err != nil {
			return nil, err
		}
		acc, ok := n.Accounts[hex.EncodeToString(addr)]
		if !ok {
			return notFound(), nil
		}
		packed, err := codectypes.NewAnyWithValue(authtypes.NewBaseAccount(sdk.AccAddress(addr), nil, acc.Number, acc.Sequence))
		if err != nil {
			return nil, err
		}
		raw, err := n.enc.Codec.Marshal(&authtypes.QueryAccountResponse{Account: packed})
		if err != nil {
			return nil, err
		}
		return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: raw}}, nil
	}
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Code: 6, Codespace: "sdk", Log: "unknown query path"}}, nil
}

// BroadcastTxSync implements chainclient.RPC. It stores the transaction,
// answers n.Ack, and then runs OnBroadcast for an accepted transaction.
func (n *Node) BroadcastTxSync(_ context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	sum := sha256.Sum256(tx)
	hash := hex.EncodeToString(sum[:])
	n.log("broadcast " + hash)
	if err := n.BroadcastErr; err != nil {
		n.BroadcastErr = nil
		n.Txs[hash] = &Tx{Raw: append([]byte(nil), tx...)}
		return nil, err
	}
	if n.Mempool {
		if waiting, ok := n.Txs[hash]; ok && !waiting.Committed {
			return nil, &rpctypes.RPCError{Code: -32603, Message: "Internal error", Data: "tx already exists in cache"}
		}
		if n.sequenceWaiting(tx) {
			return &coretypes.ResultBroadcastTx{Code: sdkerrors.ErrWrongSequence.ABCICode(), Codespace: sdkerrors.ErrWrongSequence.Codespace(), Hash: sum[:]}, nil
		}
	}
	if n.Ack != 0 {
		return &coretypes.ResultBroadcastTx{Code: n.Ack, Codespace: n.AckCodespace, Hash: sum[:]}, nil
	}
	if _, ok := n.Txs[hash]; !ok {
		n.Txs[hash] = &Tx{Raw: append([]byte(nil), tx...)}
	}
	if n.OnBroadcast != nil {
		msg, err := n.Message(tx)
		if err != nil {
			return nil, err
		}
		n.OnBroadcast(n, msg, hash)
	}
	return &coretypes.ResultBroadcastTx{Hash: sum[:]}, nil
}

// sequenceWaiting reports whether a stored uncommitted transaction has the
// signer and sequence of raw.
func (n *Node) sequenceWaiting(raw []byte) bool {
	signer, sequence, ok := n.signerSequence(raw)
	if !ok {
		return false
	}
	for _, tx := range n.Txs {
		if tx.Committed {
			continue
		}
		if s, q, ok := n.signerSequence(tx.Raw); ok && s == signer && q == sequence {
			return true
		}
	}
	return false
}

// signerSequence returns the only signer of a transaction and its
// sequence.
func (n *Node) signerSequence(raw []byte) (string, uint64, bool) {
	decoded, err := n.enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		return "", 0, false
	}
	tx, ok := decoded.(authsigning.Tx)
	if !ok {
		return "", 0, false
	}
	sigs, err := tx.GetSignaturesV2()
	if err != nil || len(sigs) != 1 {
		return "", 0, false
	}
	return hex.EncodeToString(sigs[0].PubKey.Address()), sigs[0].Sequence, true
}

// Tx implements chainclient.RPC: an uncommitted or unknown hash is the
// node's JSON-RPC "not found" error.
func (n *Node) Tx(_ context.Context, hash []byte, _ bool) (*coretypes.ResultTx, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := hex.EncodeToString(hash)
	n.log("tx " + key)
	tx, ok := n.Txs[key]
	if !ok || !tx.Committed {
		return nil, &rpctypes.RPCError{Code: -32603, Message: "Internal error", Data: fmt.Sprintf("tx (%X) not found", hash)}
	}
	return &coretypes.ResultTx{Hash: hash, Height: tx.Height, TxResult: abci.ExecTxResult{Code: tx.Code, Codespace: tx.Codespace}, Tx: tx.Raw}, nil
}

// Broadcasts returns the hashes of every broadcast, in order.
func (n *Node) Broadcasts() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for _, c := range n.Calls {
		if hash, ok := strings.CutPrefix(c, "broadcast "); ok {
			out = append(out, hash)
		}
	}
	return out
}
