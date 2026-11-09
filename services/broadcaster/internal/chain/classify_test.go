package chain

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/mempool"
	rpccore "github.com/cometbft/cometbft/rpc/core"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestRefusalTextsMatchCometBFT: the JSON-RPC texts the classifier
// recognises are the pinned CometBFT error values.
func TestRefusalTextsMatchCometBFT(t *testing.T) {
	exact := map[string]string{
		textTxInCache:   mempool.ErrTxInCache.Error(),
		textRecheckFull: mempool.ErrRecheckFull.Error(),
		textCatchingUp:  rpccore.ErrEndpointClosedCatchingUp.Error(),
	}
	for ours, theirs := range exact {
		if ours != theirs {
			t.Fatalf("text %q, CometBFT %q", ours, theirs)
		}
	}
	prefixes := map[string]string{
		prefixMempoolFull: mempool.ErrMempoolIsFull{NumTxs: 5000, MaxTxs: 5000}.Error(),
		prefixTxTooLarge:  mempool.ErrTxTooLarge{Max: 1, Actual: 2}.Error(),
		prefixPreCheck:    mempool.ErrPreCheck{Err: errors.New("tx size")}.Error(),
		prefixAppConn:     mempool.ErrAppConnMempool{Err: errors.New("closed")}.Error(),
	}
	for prefix, text := range prefixes {
		if !strings.HasPrefix(text, prefix) {
			t.Fatalf("CometBFT text %q does not start with %q", text, prefix)
		}
	}
}

// TestClassifyBroadcastAnswers: the send-outcome table of plan 8.4, through
// the real CometBFT RPC client against a fake JSON-RPC node, with the exact
// JSON-RPC texts of CMT/mempool/errors.go and CMT/rpc/core/mempool.go.
// The last column is DefinitiveRefusal, which feeds the dead rule (d2).
func TestClassifyBroadcastAnswers(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	key := testKey("broadcaster")
	raw, hash := signed(t, c.Codec, key, acceptMsg(key.PubKey().Address()), stdInput(1))
	checkTx := func(code uint32, codespace string, resHash []byte) func(json.RawMessage) rpcAnswer {
		return func(json.RawMessage) rpcAnswer {
			return rpcAnswer{result: &coretypes.ResultBroadcastTx{Code: code, Codespace: codespace, Log: "log", Hash: resHash}}
		}
	}
	rpcErr := func(text string) func(json.RawMessage) rpcAnswer {
		return func(json.RawMessage) rpcAnswer { return rpcAnswer{err: internalError(text)} }
	}
	cases := []struct {
		name       string
		answer     func(json.RawMessage) rpcAnswer
		outcome    SendOutcome
		definitive bool
	}{
		{"CheckTx code 0", checkTx(0, "", hash[:]), SendAck, false},
		{"cache", rpcErr(mempool.ErrTxInCache.Error()), SendCache, false},
		{"mempool full", rpcErr(mempool.ErrMempoolIsFull{NumTxs: 5000, MaxTxs: 5000, TxsBytes: 1, MaxTxsBytes: 2}.Error()), SendNodeRefused, true},
		{"recheck full", rpcErr(mempool.ErrRecheckFull.Error()), SendNodeRefused, true},
		{"tx too large", rpcErr(mempool.ErrTxTooLarge{Max: 1048576, Actual: 2000000}.Error()), SendNodeRefused, true},
		{"pre check", rpcErr(mempool.ErrPreCheck{Err: errors.New("tx size is too big")}.Error()), SendNodeRefused, true},
		{"app connection", rpcErr(mempool.ErrAppConnMempool{Err: errors.New("connection refused")}.Error()), SendNodeRefused, true},
		{"catching up", rpcErr(rpccore.ErrEndpointClosedCatchingUp.Error()), SendNodeRefused, true},
		{"confirmation not received", rpcErr(fmt.Errorf("broadcast confirmation not received: %w", context.DeadlineExceeded).Error()), SendUnknown, false},
		{"unknown internal error", rpcErr("something else"), SendUnknown, false},
		{"cache text in another error code", func(json.RawMessage) rpcAnswer {
			return rpcAnswer{err: &rpctypes.RPCError{Code: -32000, Message: "Server error", Data: mempool.ErrTxInCache.Error()}}
		}, SendUnknown, false},
		{"refusal text with a suffix", rpcErr(mempool.ErrTxInCache.Error() + "!"), SendUnknown, false},
		{"sdk 32 sequence mismatch", checkTx(sdkerrors.ErrWrongSequence.ABCICode(), sdkerrors.RootCodespace, hash[:]), SendMempoolRejected, false},
		{"sdk 4 signature", checkTx(sdkerrors.ErrUnauthorized.ABCICode(), sdkerrors.RootCodespace, hash[:]), SendMempoolRejected, true},
		{"sdk 30 timeout height", checkTx(sdkerrors.ErrTxTimeoutHeight.ABCICode(), sdkerrors.RootCodespace, hash[:]), SendMempoolRejected, true},
		{"svn 32 is not sdk 32", checkTx(32, svnerr.ModuleName, hash[:]), SendMempoolRejected, true},
		{"svn FORBIDDEN", checkTx(uint32(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN), svnerr.ModuleName, hash[:]), SendMempoolRejected, true},
		{"answer for another hash", checkTx(0, "", make([]byte, 32)), SendUnknown, false},
	}
	for _, tc := range cases {
		n.handle("broadcast_tx_sync", tc.answer)
		obs, err := c.Broadcast(context.Background(), raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if obs.Outcome != tc.outcome || obs.DefinitiveRefusal() != tc.definitive {
			t.Fatalf("%s: outcome %s definitive %t, want %s %t (%+v)", tc.name, obs.Outcome, obs.DefinitiveRefusal(), tc.outcome, tc.definitive, obs)
		}
	}
	n.handle("broadcast_tx_sync", checkTx(sdkerrors.ErrWrongSequence.ABCICode(), sdkerrors.RootCodespace, hash[:]))
	if obs, _ := c.Broadcast(context.Background(), raw); !obs.IsSDK(32) || obs.IsSDK(4) {
		t.Fatalf("IsSDK on %+v", obs)
	}

	// A transport failure: the node is gone after the request may have
	// reached it.
	n.srv.Close()
	obs, err := c.Broadcast(context.Background(), raw)
	if err != nil || obs.Outcome != SendUnknown || obs.DefinitiveRefusal() {
		t.Fatalf("closed node: %+v %v", obs, err)
	}
	// A client timeout.
	slow := newFakeNode(t)
	slow.handle("broadcast_tx_sync", func(json.RawMessage) rpcAnswer {
		time.Sleep(300 * time.Millisecond)
		return rpcAnswer{result: &coretypes.ResultBroadcastTx{Hash: hash[:]}}
	})
	rpc, err := NewHTTP(slow.srv.URL, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := New(rpc, c.Codec)
	if err != nil {
		t.Fatal(err)
	}
	if obs, err := cs.Broadcast(context.Background(), raw); err != nil || obs.Outcome != SendUnknown {
		t.Fatalf("timeout: %+v %v", obs, err)
	}
	if _, err := c.Broadcast(context.Background(), nil); err == nil {
		t.Fatal("empty transaction sent")
	}
	if obs := ClassifyBroadcast(nil, nil, hash); obs.Outcome != SendUnknown {
		t.Fatalf("empty answer: %+v", obs)
	}
	if obs := ClassifyRPCError(nil); obs.Outcome != SendUnknown {
		t.Fatalf("nil error: %+v", obs)
	}
}

// TestSequenceConsumedOnlyWithAccSeq: a committed failure is
// EXECUTION_FAILED (sequence consumed) only when the result carries this
// attempt's tx.acc_seq, and INCLUDED_ANTE_FAILED otherwise, whatever the
// code: svn FORBIDDEN and sdk 11 can each come from the ante or from a
// handler (plan 1.1). A success needs acc_seq and exactly one of the
// kind's typed event for the object or the idempotent marker. Every
// contradiction is an IntegrityError.
func TestSequenceConsumedOnlyWithAccSeq(t *testing.T) {
	c := testCodec(t)
	key := testKey("broadcaster")
	signer := key.PubKey().Address()
	msg := acceptMsg(signer)
	raw, rawHash := signed(t, c, key, msg, stdInput(7))
	ref := AttemptRef{Hash: rawHash, Signer: signer, Sequence: 7, Msg: msg}
	accSeq := accSeqEvent(t, c, signer, 7)
	event := typedEvent(t, eventFor(t, msg), 0)
	bech, err := c.Bech32(signer)
	if err != nil {
		t.Fatal(err)
	}
	otherLease := acceptMsg(signer)
	otherLease.LeaseId = "l2"
	forbidden := uint32(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN)
	outOfGas := sdkerrors.ErrOutOfGas.ABCICode()

	classes := []struct {
		name      string
		com       Committed
		class     CommitClass
		svn       msgkit.Code
		integrity bool
	}{
		{"handler FORBIDDEN", committed(raw, forbidden, svnerr.ModuleName, accSeq), ExecutionFailed, msgkit.Code("ERROR_CODE_FORBIDDEN"), false},
		{"ante FORBIDDEN", committed(raw, forbidden, svnerr.ModuleName), IncludedAnteFailed, msgkit.Code("ERROR_CODE_FORBIDDEN"), false},
		{"handler out of gas", committed(raw, outOfGas, sdkerrors.RootCodespace, accSeq), ExecutionFailed, msgkit.OK, false},
		{"block gas sdk 11", committed(raw, outOfGas, sdkerrors.RootCodespace), IncludedAnteFailed, msgkit.OK, false},
		{"deadline sdk 30", committed(raw, sdkerrors.ErrTxTimeoutHeight.ABCICode(), sdkerrors.RootCodespace), IncludedAnteFailed, msgkit.OK, false},
		{"success", committed(raw, 0, "", accSeq, event), CommittedOK, msgkit.OK, false},
		{"idempotent", committed(raw, 0, "", accSeq, idempotentEvent(leases.ModuleName)), CommittedIdempotent, msgkit.OK, false},
		{"failure with acc_seq of another signer", committed(raw, forbidden, svnerr.ModuleName, accSeqEvent(t, c, testKey("other").PubKey().Address(), 7)), 0, "", true},
		{"failure with acc_seq of another sequence", committed(raw, forbidden, svnerr.ModuleName, accSeqEvent(t, c, signer, 8)), 0, "", true},
		{"two acc_seq", committed(raw, forbidden, svnerr.ModuleName, accSeq, accSeq), 0, "", true},
		{"success without acc_seq", committed(raw, 0, "", event), 0, "", true},
		{"success with neither event nor marker", committed(raw, 0, "", accSeq), 0, "", true},
		{"success with event and marker", committed(raw, 0, "", accSeq, event, idempotentEvent(leases.ModuleName)), 0, "", true},
		{"success with another module's marker", committed(raw, 0, "", accSeq, idempotentEvent("content")), 0, "", true},
		{"success for another lease", committed(raw, 0, "", accSeq, typedEvent(t, eventFor(t, otherLease), 0)), 0, "", true},
		{"malformed typed event", committed(raw, 0, "", accSeq, abci.Event{Type: "svn.events.v1.LeaseAccepted", Attributes: []abci.EventAttribute{{Key: "lease_id", Value: "l1"}}}), 0, "", true},
		{"acc_seq with a bad address", committed(raw, forbidden, svnerr.ModuleName, abci.Event{Type: "tx", Attributes: []abci.EventAttribute{{Key: "acc_seq", Value: "svn1nothing/7"}}}), 0, "", true},
		{"acc_seq with a non-canonical sequence", committed(raw, forbidden, svnerr.ModuleName, abci.Event{Type: "tx", Attributes: []abci.EventAttribute{{Key: "acc_seq", Value: bech + "/07"}}}), 0, "", true},
		{"acc_seq without a sequence", committed(raw, forbidden, svnerr.ModuleName, abci.Event{Type: "tx", Attributes: []abci.EventAttribute{{Key: "acc_seq", Value: bech}}}), 0, "", true},
	}
	for _, tc := range classes {
		obs, err := c.ClassifyCommitted(tc.com, ref)
		var integrity *IntegrityError
		if tc.integrity {
			if !errors.As(err, &integrity) {
				t.Fatalf("%s: got %+v %v, want an IntegrityError", tc.name, obs, err)
			}
			continue
		}
		if err != nil || obs.Class != tc.class || obs.SVN != tc.svn || obs.Hash != tc.com.Hash || obs.Height != tc.com.Height {
			t.Fatalf("%s: got %s %s %v, want %s %s", tc.name, obs.Class, obs.SVN, err, tc.class, tc.svn)
		}
	}
	other, otherHash := signed(t, c, key, msg, stdInput(8))
	var integrity *IntegrityError
	if _, err := c.ClassifyCommitted(committed(other, 0, "", accSeq, event), AttemptRef{Hash: otherHash, Signer: signer, Sequence: 7, Msg: msg}); !errors.As(err, &integrity) {
		t.Fatalf("bytes of another attempt: %v", err)
	}
	// Another attempt at the same signer, sequence and message, with a
	// later timeout_height: its bytes decode like ref's, but its hash is not
	// ref's, so its result is never classified as ref's.
	in := stdInput(7)
	in.TimeoutHeight++
	sibling, _ := signed(t, c, key, msg, in)
	if _, err := c.ClassifyCommitted(committed(sibling, 0, "", accSeq, event), ref); !errors.As(err, &integrity) {
		t.Fatalf("result of a sibling attempt: %v", err)
	}
	forged := committed(raw, 0, "", accSeq, event)
	forged.Raw = sibling
	if _, err := c.ClassifyCommitted(forged, ref); !errors.As(err, &integrity) {
		t.Fatalf("bytes that do not hash to the result's hash: %v", err)
	}
	authorityEnd := endMsg(signer)
	authorityEnd.AuthorityEnd = true
	if _, err := c.ClassifyCommitted(committed(raw, 0, "", accSeq, event), AttemptRef{Hash: rawHash, Signer: signer, Sequence: 7, Msg: authorityEnd}); !errors.Is(err, ErrNotRouted) {
		t.Fatalf("authority end: %v", err)
	}
}

// FuzzClassifyRPCError: for any JSON-RPC error, wrapped or not, the
// classifier never panics, is deterministic, never answers ACK or
// MEMPOOL_REJECTED, answers CACHE only for the exact cache text, answers
// NODE_REFUSED only for a recognised refusal text in a JSON-RPC internal
// error, and answers UNKNOWN for everything else. A recognised refusal is
// the only definitive refusal.
func FuzzClassifyRPCError(f *testing.F) {
	seeds := []string{
		textTxInCache, textRecheckFull, textCatchingUp, mempool.ErrMempoolIsFull{}.Error(),
		mempool.ErrTxTooLarge{Max: 1, Actual: 2}.Error(), "tx pre check: x", "appConn mempool: x",
		"broadcast confirmation not received: context canceled", "tx (00) not found", "", "Tx too large",
	}
	for _, s := range seeds {
		f.Add(-32603, "Internal error", s, false)
		f.Add(-32000, "Server error", s, true)
	}
	f.Fuzz(func(t *testing.T, code int, message, data string, wrap bool) {
		var err error = &rpctypes.RPCError{Code: code, Message: message, Data: data}
		if wrap {
			err = fmt.Errorf("post failed: %w", err)
		}
		obs := ClassifyRPCError(err)
		if again := ClassifyRPCError(err); again != obs {
			t.Fatalf("not deterministic: %+v then %+v", obs, again)
		}
		if len(obs.Detail) > maxLogBytes {
			t.Fatalf("detail of %d bytes", len(obs.Detail))
		}
		internal := code == -32603 && message == "Internal error"
		refusal := data == textRecheckFull || data == textCatchingUp
		for _, p := range []string{prefixMempoolFull, prefixTxTooLarge, prefixPreCheck, prefixAppConn} {
			refusal = refusal || strings.HasPrefix(data, p)
		}
		var want SendOutcome
		switch {
		case internal && data == textTxInCache:
			want = SendCache
		case internal && refusal:
			want = SendNodeRefused
		default:
			want = SendUnknown
		}
		if obs.Outcome != want {
			t.Fatalf("%d %q %q: %s, want %s", code, message, data, obs.Outcome, want)
		}
		if obs.DefinitiveRefusal() != (want == SendNodeRefused) || obs.Code != 0 || obs.Codespace != "" {
			t.Fatalf("%d %q %q: %+v", code, message, data, obs)
		}
		other := ClassifyBroadcast(nil, err, sha256.Sum256([]byte(data)))
		if other != obs {
			t.Fatalf("ClassifyBroadcast %+v, ClassifyRPCError %+v", other, obs)
		}
	})
}
