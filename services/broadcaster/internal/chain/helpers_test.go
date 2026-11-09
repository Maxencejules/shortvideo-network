package chain

// Test helpers. fakeNode is a JSON-RPC server for unit tests only
// (httptest, loopback): it answers the CometBFT methods this package calls
// with canned results or errors, so the real rpchttp client and its error
// decoding run. It is a fake; it is never end-to-end evidence.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const testChainID = "shortvideo-dev-0123456789abcdef"

// rpcAnswer is one canned JSON-RPC answer: a result, or an error.
type rpcAnswer struct {
	result any
	err    *rpctypes.RPCError
}

// fakeNode answers JSON-RPC calls with the handler registered per method.
type fakeNode struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	handlers map[string]func(params json.RawMessage) rpcAnswer
	calls    map[string]int
}

func newFakeNode(t *testing.T) *fakeNode {
	t.Helper()
	n := &fakeNode{t: t, handlers: map[string]func(json.RawMessage) rpcAnswer{}, calls: map[string]int{}}
	n.srv = httptest.NewServer(http.HandlerFunc(n.serve))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *fakeNode) handle(method string, h func(params json.RawMessage) rpcAnswer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handlers[method] = h
}

func (n *fakeNode) count(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[method]
}

func (n *fakeNode) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req rpctypes.RPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n.mu.Lock()
	h := n.handlers[req.Method]
	n.calls[req.Method]++
	n.mu.Unlock()
	var resp rpctypes.RPCResponse
	switch {
	case h == nil:
		resp = rpctypes.NewRPCErrorResponse(req.ID, -32601, "Method not found", req.Method)
	default:
		a := h(req.Params)
		if a.err != nil {
			resp = rpctypes.NewRPCErrorResponse(req.ID, a.err.Code, a.err.Message, a.err.Data)
		} else {
			resp = rpctypes.NewRPCSuccessResponse(req.ID, a.result)
		}
	}
	out, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// internalError is the JSON-RPC error CometBFT returns for a handler error.
func internalError(data string) *rpctypes.RPCError {
	return &rpctypes.RPCError{Code: -32603, Message: "Internal error", Data: data}
}

// client returns a Client of the fake node for testChainID.
func (n *fakeNode) client(t *testing.T) *Client {
	t.Helper()
	rpc, err := NewHTTP(n.srv.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	codec := testCodec(t)
	c, err := New(rpc, codec)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testCodec(t testing.TB) *Codec {
	t.Helper()
	c, err := NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// decodeParams decodes JSON-RPC params as the CometBFT server does
// (amino-compatible JSON).
func decodeParams(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	if err := cmtjson.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode params %s: %v", raw, err)
	}
}

// testKey returns a deterministic development-only secp256k1 key.
func testKey(name string) *secp256k1.PrivKey {
	return secp256k1.GenPrivKeyFromSecret([]byte("svn broadcaster chain test " + name))
}

func digest(seed string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(seed))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

func innerSig(domain typesv1.SignatureDomain, seed string) *typesv1.InnerSignature {
	pub := sha256.Sum256([]byte("pub " + seed))
	sig := make([]byte, 0, 64)
	for i := 0; i < 2; i++ {
		part := sha256.Sum256([]byte(fmt.Sprintf("sig %s %d", seed, i)))
		sig = append(sig, part[:]...)
	}
	return &typesv1.InnerSignature{Domain: domain, PublicKey: pub[:], Signature: sig, KeyId: "k-" + seed, CanonicalProfileVersion: 1}
}

// The six reconciled messages with every field set, signed by signer.
func registerMsg(signer []byte) *contentv1.MsgRegisterContent {
	return &contentv1.MsgRegisterContent{Signer: signer, BusinessId: "reg-c1", ContentId: "c1", ContentVersion: 1,
		ManifestDigest: digest("manifest c1"), PackageBytes: 4096, HostingIntent: innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, "intent c1")}
}

func withdrawMsg(signer []byte) *contentv1.MsgWithdrawContent {
	return &contentv1.MsgWithdrawContent{Signer: signer, BusinessId: "wd-c1", ContentId: "c1", ContentVersion: 1,
		ReasonDigest: digest("reason c1"), HostingIntent: innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, "withdraw c1")}
}

func offerMsg(signer []byte) *leasesv1.MsgOfferLease {
	return &leasesv1.MsgOfferLease{Signer: signer, BusinessId: "of-o1", OfferId: "o1", ContentId: "c1", ContentVersion: 1,
		ProviderId: "p1", ManifestDigest: digest("manifest c1"), PackageBytes: 4096, TermsDigest: digest("terms"),
		StartUnixSeconds: 1_800_000_000, DeadlineUnixSeconds: 1_800_003_600, ExpiryUnixSeconds: 1_800_000_600, TermVersion: 1}
}

func acceptMsg(signer []byte) *leasesv1.MsgAcceptLease {
	return &leasesv1.MsgAcceptLease{Signer: signer, BusinessId: "ac-l1", OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, "accept l1")}
}

func renewMsg(signer []byte) *leasesv1.MsgRenewLease {
	return &leasesv1.MsgRenewLease{Signer: signer, BusinessId: "rn-l1", LeaseId: "l1", TermVersion: 2,
		DeadlineUnixSeconds: 1_800_007_200, ManifestDigest: digest("manifest c1"), PackageBytes: 4096,
		ProviderSignature: innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, "renew l1"), EvidenceId: "e1"}
}

func endMsg(signer []byte) *leasesv1.MsgEndLease {
	return &leasesv1.MsgEndLease{Signer: signer, BusinessId: "en-l1", LeaseId: "l1",
		ProviderSignature: innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, "end l1")}
}

// sixMsgs returns the six reconciled messages, signed by signer, by name.
func sixMsgs(signer []byte) []struct {
	name string
	msg  sdk.Msg
} {
	return []struct {
		name string
		msg  sdk.Msg
	}{
		{"register", registerMsg(signer)}, {"withdraw", withdrawMsg(signer)}, {"offer", offerMsg(signer)},
		{"accept", acceptMsg(signer)}, {"renew", renewMsg(signer)}, {"end", endMsg(signer)},
	}
}

// eventFor returns the typed event a first commitment of msg emits, as the
// chain modules fill it from the message (x/content and x/leases README
// "Events"); fields the message does not carry get fixed test values.
func eventFor(t testing.TB, msg sdk.Msg) proto.Message {
	t.Helper()
	switch m := msg.(type) {
	case *contentv1.MsgRegisterContent:
		return &eventsv1.ContentRegistered{ContentId: m.GetContentId(), ContentVersion: m.GetContentVersion(),
			ManifestSha256: msgkit.Hex(m.GetManifestDigest().GetValue()), PackageBytes: strconv.FormatUint(m.GetPackageBytes(), 10)}
	case *contentv1.MsgWithdrawContent:
		return &eventsv1.ContentWithdrawn{ContentId: m.GetContentId(), ContentVersion: m.GetContentVersion()}
	case *leasesv1.MsgOfferLease:
		return &eventsv1.LeaseOffered{OfferId: m.GetOfferId(), ProviderId: m.GetProviderId(), ContentId: m.GetContentId()}
	case *leasesv1.MsgAcceptLease:
		return &eventsv1.LeaseAccepted{LeaseId: m.GetLeaseId(), ProviderId: "p1", ContentId: "c1", ContentVersion: 1,
			PackageBytes: "4096", DeadlineUnixSeconds: 1_800_003_600}
	case *leasesv1.MsgRenewLease:
		return &eventsv1.LeaseRenewed{LeaseId: m.GetLeaseId(), TermVersion: m.GetTermVersion(), DeadlineUnixSeconds: m.GetDeadlineUnixSeconds()}
	case *leasesv1.MsgEndLease:
		return &eventsv1.LeaseEnded{LeaseId: m.GetLeaseId(), State: "ENDED", ReleasedBytes: "4096"}
	}
	t.Fatalf("no event for %T", msg)
	return nil
}

// typedEvent emits ev with the chain's own emitter (msgkit.EmitTyped) and
// appends the msg_index attribute baseapp adds to a committed message
// event.
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

// accSeqEvent is the ante's tx event for signer and sequence.
func accSeqEvent(t testing.TB, c *Codec, signer []byte, seq uint64) abci.Event {
	t.Helper()
	q, err := c.Bech32(signer)
	if err != nil {
		t.Fatal(err)
	}
	return abci.Event{Type: sdk.EventTypeTx, Attributes: []abci.EventAttribute{{Key: sdk.AttributeKeyAccountSequence, Value: fmt.Sprintf("%s/%d", q, seq), Index: true}}}
}

func idempotentEvent(module string) abci.Event {
	return abci.Event{Type: module + "_result", Attributes: []abci.EventAttribute{
		{Key: "idempotent", Value: "true", Index: true}, {Key: msgkit.MsgIndexKey, Value: "0", Index: true}}}
}

// signed builds msg with Prepare, signs it with key (test code; the
// broadcaster's own signing is not in this package), and assembles it.
func signed(t testing.TB, c *Codec, key *secp256k1.PrivKey, msg sdk.Msg, in TxInput) ([]byte, [32]byte) {
	t.Helper()
	u, err := c.Prepare(msg, key.PubKey(), in)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := key.Sign(u.SignBytes())
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := u.Assemble(sig)
	if err != nil {
		t.Fatal(err)
	}
	return raw, hash
}

// signedMulti builds a transaction of several messages of one signer, the
// shape another admitted account may commit (test code only).
func signedMulti(t testing.TB, c *Codec, key *secp256k1.PrivKey, seq uint64, msgs ...sdk.Msg) []byte {
	t.Helper()
	cfg := c.enc.TxConfig
	b := cfg.NewTxBuilder()
	if err := b.SetMsgs(msgs...); err != nil {
		t.Fatal(err)
	}
	b.SetGasLimit(10_000_000)
	info := signingtypes.SignatureV2{PubKey: key.PubKey(), Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT}, Sequence: seq}
	if err := b.SetSignatures(info); err != nil {
		t.Fatal(err)
	}
	bech, err := c.Bech32(key.PubKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	signBytes, err := authsigning.GetSignBytesAdapter(context.Background(), cfg.SignModeHandler(), signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{Address: bech, ChainID: c.chainID, AccountNumber: 7, Sequence: seq, PubKey: key.PubKey()}, b.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	sig, err := key.Sign(signBytes)
	if err != nil {
		t.Fatal(err)
	}
	info.Data = &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT, Signature: sig}
	if err := b.SetSignatures(info); err != nil {
		t.Fatal(err)
	}
	raw, err := cfg.TxEncoder()(b.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// committed wraps raw as a committed result with code, codespace and
// events, at height 12, index 0.
func committed(raw []byte, code uint32, codespace string, events ...abci.Event) Committed {
	return Committed{Hash: sha256.Sum256(raw), Height: 12, Code: code, Codespace: codespace, GasWanted: 2_000_000, GasUsed: 60_000, Events: events, Raw: raw}
}

func stdInput(seq uint64) TxInput {
	return TxInput{AccountNumber: 7, Sequence: seq, Gas: 2_000_000, TimeoutHeight: 140}
}
