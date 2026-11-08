package chain

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// IntegrityError is an answer that contradicts what the broadcaster stored
// or what the chain can produce: bytes that do not hash to their hash, an
// acc_seq naming another account or sequence, a committed success without
// its event. The caller alerts and halts the signer; nothing is rewritten
// (plan 8.4, 8.5).
type IntegrityError struct {
	Reason string
}

func (e *IntegrityError) Error() string { return "chain: integrity: " + e.Reason }

// SendOutcome is the class of one started send (plan 8.4).
type SendOutcome uint8

// Send outcomes.
const (
	// SendUnknown: a transport error, a timeout, a cancelled request
	// ("broadcast confirmation not received"), or any answer this package
	// does not recognise. The bytes may be in a mempool and may commit.
	SendUnknown SendOutcome = iota + 1
	// SendCache: JSON-RPC "tx already exists in cache". The node holds
	// these bytes in its mempool or has committed them.
	SendCache
	// SendNodeRefused: a JSON-RPC refusal returned before the node added
	// the bytes to its mempool (mempool full or rechecking, transaction
	// too large, pre-check failure, mempool application connection error,
	// endpoint closed while catching up).
	SendNodeRefused
	// SendMempoolRejected: CheckTx answered a non-zero code. The node
	// removed the bytes from its cache and did not gossip them.
	SendMempoolRejected
	// SendAck: CheckTx answered code 0. The bytes wait in this node's
	// mempool; nothing has executed. It is never a commit.
	SendAck
)

func (o SendOutcome) String() string {
	switch o {
	case SendUnknown:
		return "UNKNOWN"
	case SendCache:
		return "CACHE"
	case SendNodeRefused:
		return "NODE_REFUSED"
	case SendMempoolRejected:
		return "MEMPOOL_REJECTED"
	case SendAck:
		return "ACK"
	}
	return "SendOutcome(" + strconv.Itoa(int(o)) + ")"
}

// SendObservation is the classified answer of one broadcast_tx_sync.
type SendObservation struct {
	Outcome SendOutcome
	// Codespace and Code are the CheckTx answer (SendMempoolRejected).
	Codespace string
	Code      uint32
	// Detail is the clipped CheckTx log or error text.
	Detail string
}

// IsSDK reports whether the observation is the CheckTx rejection code of
// codespace sdk.
func (o SendObservation) IsSDK(code uint32) bool {
	return o.Outcome == SendMempoolRejected && o.Codespace == sdkerrors.RootCodespace && o.Code == code
}

// DefinitiveRefusal reports whether the observation counts as a refusal for
// the dead rule (d2) of plan 8.5: a node refusal, or a CheckTx rejection
// other than sdk 32. Sdk 32 (sequence mismatch) does not count: after a
// node restart cleared its cache, the bytes of a committed transaction get
// sdk 32. UNKNOWN, CACHE and ACK never count.
func (o SendObservation) DefinitiveRefusal() bool {
	switch o.Outcome {
	case SendNodeRefused:
		return true
	case SendMempoolRejected:
		return !o.IsSDK(sdkerrors.ErrWrongSequence.ABCICode())
	}
	return false
}

// JSON-RPC answers of CometBFT v0.40.0 that this package recognises: the
// error texts of mempool and rpc/core, which CometBFT returns as the data
// of a JSON-RPC internal error. TestRefusalTextsMatchCometBFT compares
// each with the pinned CometBFT value.
const (
	rpcInternalCode    = -32603
	rpcInternalMessage = "Internal error"

	textTxInCache     = "tx already exists in cache"
	textRecheckFull   = "mempool is still rechecking after a new committed block, so it is considered as full"
	textCatchingUp    = "endpoint is closed while node is catching up"
	prefixMempoolFull = "mempool is full: "
	prefixTxTooLarge  = "Tx too large. Max size is "
	prefixPreCheck    = "tx pre check: "
	prefixAppConn     = "appConn mempool: "
)

// ClassifyRPCError classifies a broadcast_tx_sync error. Only a JSON-RPC
// internal error whose data is one of the recognised CometBFT texts is
// CACHE or NODE_REFUSED; every other error is UNKNOWN, because the bytes
// may have reached a mempool.
func ClassifyRPCError(err error) SendObservation {
	if err == nil {
		return SendObservation{Outcome: SendUnknown, Detail: "no answer"}
	}
	var rpcErr *rpctypes.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != rpcInternalCode || rpcErr.Message != rpcInternalMessage {
		return SendObservation{Outcome: SendUnknown, Detail: clip(err.Error())}
	}
	data := rpcErr.Data
	switch {
	case data == textTxInCache:
		return SendObservation{Outcome: SendCache, Detail: data}
	case data == textRecheckFull, data == textCatchingUp,
		strings.HasPrefix(data, prefixMempoolFull), strings.HasPrefix(data, prefixTxTooLarge),
		strings.HasPrefix(data, prefixPreCheck), strings.HasPrefix(data, prefixAppConn):
		return SendObservation{Outcome: SendNodeRefused, Detail: clip(data)}
	}
	return SendObservation{Outcome: SendUnknown, Detail: clip(data)}
}

// ClassifyBroadcast classifies a broadcast_tx_sync answer for the
// transaction whose hash is hash (plan 8.4). An answer naming another hash
// is UNKNOWN: it does not say what happened to these bytes.
func ClassifyBroadcast(res *coretypes.ResultBroadcastTx, err error, hash [32]byte) SendObservation {
	if err != nil {
		return ClassifyRPCError(err)
	}
	if res == nil {
		return SendObservation{Outcome: SendUnknown, Detail: "empty broadcast answer"}
	}
	if string(res.Hash) != string(hash[:]) {
		return SendObservation{Outcome: SendUnknown, Detail: fmt.Sprintf("broadcast answer for hash %X", []byte(res.Hash))}
	}
	if res.Code == 0 {
		return SendObservation{Outcome: SendAck}
	}
	return SendObservation{Outcome: SendMempoolRejected, Codespace: clip(res.Codespace), Code: res.Code, Detail: clip(res.Log)}
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

// Committed is one committed transaction as the node indexed it (the tx
// route or one tx_search entry). Its bytes hash to Hash.
type Committed struct {
	Hash      [32]byte
	Height    int64
	Index     uint32
	Code      uint32
	Codespace string
	Log       string
	GasWanted int64
	GasUsed   int64
	Events    []abci.Event
	Raw       []byte
}

// AccSeq is one tx.acc_seq attribute: the signer and the sequence its
// transaction used.
type AccSeq struct {
	Address  []byte
	Sequence uint64
}

// AccSeqs returns every tx.acc_seq attribute of a committed result (SDK
// SetPubKey decorator: "<bech32>/<sequence>"). The ante keeps its events
// only when the whole ante passed (SDK baseapp runTx), so a result carries
// the attribute of signer S and sequence N exactly when sequence N of S was
// consumed. A malformed attribute is an IntegrityError.
func (c *Codec) AccSeqs(events []abci.Event) ([]AccSeq, error) {
	var out []AccSeq
	for _, ev := range events {
		if ev.Type != sdk.EventTypeTx {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key != sdk.AttributeKeyAccountSequence {
				continue
			}
			cut := strings.LastIndexByte(attr.Value, '/')
			if cut < 1 {
				return nil, &IntegrityError{Reason: fmt.Sprintf("malformed acc_seq %q", clip(attr.Value))}
			}
			addr, err := c.bech.StringToBytes(attr.Value[:cut])
			if err != nil || len(addr) != genesis.AccountBytes {
				return nil, &IntegrityError{Reason: fmt.Sprintf("acc_seq address %q", clip(attr.Value[:cut]))}
			}
			digits := attr.Value[cut+1:]
			seq, err := strconv.ParseUint(digits, 10, 64)
			if err != nil || strconv.FormatUint(seq, 10) != digits {
				return nil, &IntegrityError{Reason: fmt.Sprintf("acc_seq sequence %q", clip(digits))}
			}
			out = append(out, AccSeq{Address: addr, Sequence: seq})
		}
	}
	return out, nil
}

// TypedEvent is one svn.events.v1 event of a committed transaction.
type TypedEvent struct {
	Type     string
	Msg      proto.Message
	MsgIndex uint32
}

// typedPrefix starts the type of every svn.events.v1 event.
const typedPrefix = "svn.events.v1."

// TypedEvents decodes every svn.events.v1 event of a committed result with
// msgkit.ParseCommitted. A malformed typed event is an IntegrityError.
func TypedEvents(events []abci.Event) ([]TypedEvent, error) {
	var out []TypedEvent
	for _, ev := range events {
		if !strings.HasPrefix(ev.Type, typedPrefix) {
			continue
		}
		msg, index, err := msgkit.ParseCommitted(ev)
		if err != nil {
			return nil, &IntegrityError{Reason: fmt.Sprintf("typed event: %v", err)}
		}
		out = append(out, TypedEvent{Type: ev.Type, Msg: msg, MsgIndex: index})
	}
	return out, nil
}

// idempotentMarker reports whether events hold "<module>_result" with
// idempotent=true (msgkit.EmitIdempotent).
func idempotentMarker(events []abci.Event, module string) bool {
	for _, ev := range events {
		if ev.Type != module+"_result" {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == "idempotent" && attr.Value == "true" {
				return true
			}
		}
	}
	return false
}

// CommitClass is the class of one attempt's committed result (plan 8.4).
type CommitClass uint8

// Commit classes.
const (
	// CommittedOK: code 0 and the typed event of the message's kind for
	// the intended object. The attempt is resolved; its hash is the
	// canonical commitment.
	CommittedOK CommitClass = iota + 1
	// CommittedIdempotent: code 0 and only "<module>_result
	// idempotent=true": the business id had already committed with the
	// same digest. It is never itself the canonical commitment (plan 9.4).
	CommittedIdempotent
	// ExecutionFailed: code != 0 and the result carries this attempt's
	// tx.acc_seq: the ante passed and the sequence is consumed.
	ExecutionFailed
	// IncludedAnteFailed: code != 0 and no tx.acc_seq: the ante failed in
	// the block and the sequence is not consumed.
	IncludedAnteFailed
)

func (k CommitClass) String() string {
	switch k {
	case CommittedOK:
		return "COMMITTED_OK"
	case CommittedIdempotent:
		return "COMMITTED_IDEMPOTENT"
	case ExecutionFailed:
		return "EXECUTION_FAILED"
	case IncludedAnteFailed:
		return "INCLUDED_ANTE_FAILED"
	}
	return "CommitClass(" + strconv.Itoa(int(k)) + ")"
}

// AttemptRef identifies one stored attempt: its transaction hash, its
// signer, its sequence, and the message it carries.
type AttemptRef struct {
	Hash     [32]byte
	Signer   []byte
	Sequence uint64
	Msg      sdk.Msg
}

// CommitObservation is one attempt's classified committed result.
type CommitObservation struct {
	Class     CommitClass
	Hash      [32]byte
	Height    int64
	Index     uint32
	Code      uint32
	Codespace string
	Log       string
	GasWanted int64
	GasUsed   int64
	// SVN is the protocol code name of a codespace svn failure (msgkit.OK
	// otherwise).
	SVN msgkit.Code
	// Events are the typed events of a CommittedOK result.
	Events []TypedEvent
}

// ClassifyCommitted classifies the committed result of one stored attempt
// (plan 8.4). The result must be for ref's transaction hash (another
// attempt at the same signer, sequence and message, with another
// timeout_height, decodes the same way but has another hash), and the
// transaction's bytes must decode to ref's signer, sequence and message.
// Whether the sequence was consumed is read from the
// tx.acc_seq attribute, never from the code: the same code can come from
// the ante or from a handler (plan 1.1). An acc_seq of another account or
// sequence, two acc_seq attributes, a success without acc_seq, a success
// whose typed event names another object, or a success with neither the
// typed event nor the idempotent marker is an IntegrityError.
func (c *Codec) ClassifyCommitted(com Committed, ref AttemptRef) (CommitObservation, error) {
	key, err := ObjectOf(ref.Msg)
	if err != nil {
		return CommitObservation{}, err
	}
	if com.Hash != ref.Hash || sha256.Sum256(com.Raw) != com.Hash {
		return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed %X is not the referenced attempt's transaction", com.Hash)}
	}
	decoded, err := c.Decode(com.Raw)
	if err != nil {
		return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed %X does not decode: %v", com.Hash, err)}
	}
	own, err := decoded.Own()
	if err != nil {
		return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed %X: %v", com.Hash, err)}
	}
	if string(own.Signer.Address) != string(ref.Signer) || own.Signer.Sequence != ref.Sequence || !equalMsg(own.Msg.Msg, ref.Msg) {
		return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed %X is not the referenced attempt", com.Hash)}
	}
	seqs, err := c.AccSeqs(com.Events)
	if err != nil {
		return CommitObservation{}, err
	}
	switch {
	case len(seqs) > 1:
		return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed %X carries %d acc_seq attributes", com.Hash, len(seqs))}
	case len(seqs) == 1 && (string(seqs[0].Address) != string(ref.Signer) || seqs[0].Sequence != ref.Sequence):
		return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed %X names acc_seq %X/%d, not this attempt", com.Hash, seqs[0].Address, seqs[0].Sequence)}
	}
	obs := CommitObservation{
		Hash: com.Hash, Height: com.Height, Index: com.Index, Code: com.Code, Codespace: com.Codespace, Log: com.Log,
		GasWanted: com.GasWanted, GasUsed: com.GasUsed, SVN: SVNCode(com.Codespace, com.Code),
	}
	if com.Code != 0 {
		obs.Class = IncludedAnteFailed
		if len(seqs) == 1 {
			obs.Class = ExecutionFailed
		}
		return obs, nil
	}
	if len(seqs) == 0 {
		return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed success %X without acc_seq", com.Hash)}
	}
	events, err := TypedEvents(com.Events)
	if err != nil {
		return CommitObservation{}, err
	}
	var kind []TypedEvent
	for _, ev := range events {
		if ev.Type != key.EventType {
			continue
		}
		if ev.MsgIndex != 0 || !key.matches(ev.Msg) {
			return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed %X emits %s for another object", com.Hash, ev.Type)}
		}
		kind = append(kind, ev)
	}
	idempotent := idempotentMarker(com.Events, key.Module)
	switch {
	case len(kind) == 1 && !idempotent:
		obs.Class, obs.Events = CommittedOK, events
		return obs, nil
	case len(kind) == 0 && idempotent:
		obs.Class = CommittedIdempotent
		return obs, nil
	}
	return CommitObservation{}, &IntegrityError{Reason: fmt.Sprintf("committed success %X has %d %s events and idempotent=%t", com.Hash, len(kind), key.EventType, idempotent)}
}
