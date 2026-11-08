package app_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/encoding/protowire"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Goal 11 plan section 13.3: the chain-level facts the transaction
// broadcaster relies on, shown on the real application (IAVL on memdb,
// the ante chain of ante.go, the message router, Commit):
//
//   - timeout_height through CheckTx and FinalizeBlock, with the boundary
//     height (TestTimeoutHeightThroughFinalizeBlock);
//   - the tx.acc_seq attribute: present on success and on a message
//     handler failure, absent on every ante failure, so it, not the result
//     code, tells whether the signer's sequence was consumed
//     (TestAccSeqPresentOnSuccess, TestAccSeqPresentOnHandlerFailure,
//     TestAccSeqAbsentOnAnyAnteFailure);
//   - the strict transaction decoder accepts timeout_height
//     (TestStrictDecoderAcceptsTimeoutHeight);
//   - simulation accepts a transaction with the signer's public key and an
//     empty signature (TestSimulateWithEmptySignature), through the SDK tx
//     service route of abci_query. The /app/simulate route of abci_query
//     runs the same simulation but cannot JSON-encode its answer for an svn
//     message (TestAppSimulatePathCannotEncodeSvnResults, a record of
//     current behaviour).
//
// CheckTx verifies signatures with the account number only once the check
// state has a non-zero height (DR-10-41), so every test that requires a
// CheckTx code 0 signs with an account whose number is not 0.

// outcome is an ABCI result code: codespace and code, code 0 for success.
type outcome struct {
	space string
	code  uint32
}

var accepted = outcome{}

func sdkOutcome(e *errorsmod.Error) outcome {
	return outcome{space: e.Codespace(), code: e.ABCICode()}
}

func svnOutcome(c msgkit.Code) outcome {
	return outcome{space: svnerr.ModuleName, code: uint32(typesv1.ErrorCode_value[string(c)])}
}

// wantOutcome fails unless (space, code) is want. A code 0 is success
// whatever the codespace.
func wantOutcome(t *testing.T, label, space string, code uint32, log string, want outcome) {
	t.Helper()
	if want.code == 0 {
		if code != 0 {
			t.Fatalf("%s: code %s/%d log %s, want 0", label, space, code, log)
		}
		return
	}
	if space != want.space || code != want.code {
		t.Fatalf("%s: code %s/%d log %s, want %s/%d", label, space, code, log, want.space, want.code)
	}
}

// wantCheckTx runs CheckTx of the given type on raw and requires want.
func wantCheckTx(t *testing.T, label string, chainApp *app.App, raw []byte, kind abci.CheckTxType, want outcome) {
	t.Helper()
	res, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: kind})
	if err != nil {
		t.Fatalf("%s: CheckTx: %v", label, err)
	}
	wantOutcome(t, label, res.Codespace, res.Code, res.Log, want)
}

// nonzeroService returns the ADMITTED_SERVICE account of n whose account
// number is not 0 (DR-10-41). Account numbers follow address order and the
// keys are new on every run; at most one account is numbered 0.
func nonzeroService(t *testing.T, n *regNet) *actor {
	t.Helper()
	for _, who := range []*actor{n.service, n.service2} {
		if n.numbers[who.hexAddr()] != 0 {
			return who
		}
	}
	t.Fatal("both service accounts have account number 0")
	return nil
}

// otherService returns the ADMITTED_SERVICE account of n that is not who.
func otherService(n *regNet, who *actor) *actor {
	if who == n.service {
		return n.service2
	}
	return n.service
}

// accSeqValues returns the value of every acc_seq attribute of every "tx"
// event, in order. The SDK SetPubKey decorator emits one per signature.
func accSeqValues(events []abci.Event) []string {
	var out []string
	for _, ev := range events {
		if ev.Type != sdk.EventTypeTx {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == sdk.AttributeKeyAccountSequence {
				out = append(out, attr.Value)
			}
		}
	}
	return out
}

// wantAccSeq requires exactly one acc_seq attribute, equal to who's bech32
// address (the auth keeper's address codec), a slash, and sequence.
func wantAccSeq(t *testing.T, label string, res *abci.ExecTxResult, who *actor, sequence uint64) {
	t.Helper()
	bech, err := addresscodec.NewBech32Codec(chainaddress.Account).BytesToString(who.addr)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%s/%d", bech, sequence)
	if got := accSeqValues(res.Events); len(got) != 1 || got[0] != want {
		t.Fatalf("%s: acc_seq %q, want [%q]", label, got, want)
	}
}

// wantNoAccSeq requires that the result carries no event at all: the SDK
// keeps no ante event of a transaction whose ante chain failed, so no
// acc_seq attribute either.
func wantNoAccSeq(t *testing.T, label string, res *abci.ExecTxResult) {
	t.Helper()
	if got := accSeqValues(res.Events); len(got) != 0 {
		t.Fatalf("%s: acc_seq %q on an ante failure", label, got)
	}
	if len(res.Events) != 0 {
		t.Fatalf("%s: %d events on an ante failure", label, len(res.Events))
	}
}

// wantSequence requires who's committed auth sequence to be want.
func wantSequence(t *testing.T, label string, chainApp *app.App, who *actor, want uint64) {
	t.Helper()
	got, known := chainApp.AccountSequenceForTest(who.addr)
	if !known {
		t.Fatalf("%s: %s has no auth account", label, who.name)
	}
	if got != want {
		t.Fatalf("%s: %s sequence %d, want %d", label, who.name, got, want)
	}
}

// storesState is every committed store of one application: FullSnapshot
// (every store but the auth account store) and the auth account store,
// whose rows hold account sequences and public keys.
type storesState struct {
	full, auth []byte
}

func takeStores(t *testing.T, chainApp *app.App) storesState {
	t.Helper()
	auth := chainApp.ModuleSnapshot(authtypes.StoreKey)
	if len(auth) == 0 {
		t.Fatalf("auth store %q has no rows", authtypes.StoreKey)
	}
	return storesState{full: chainApp.FullSnapshot(), auth: auth}
}

// sameStores requires every committed store to equal before.
func sameStores(t *testing.T, label string, chainApp *app.App, before storesState) {
	t.Helper()
	after := takeStores(t, chainApp)
	if !bytes.Equal(before.full, after.full) {
		t.Fatalf("%s: non-account state changed (%d -> %d bytes)", label, len(before.full), len(after.full))
	}
	if !bytes.Equal(before.auth, after.auth) {
		t.Fatalf("%s: auth account store changed (%d -> %d bytes)", label, len(before.auth), len(after.auth))
	}
}

// signedWithTimeout returns msg signed by who at who's next sequence, with
// the gas limit registryGas and the given timeout_height.
func (n *regNet) signedWithTimeout(t *testing.T, c *cluster, who *actor, timeoutHeight uint64, msg sdk.Msg) []byte {
	t.Helper()
	builder := c.app().EncodingConfig().TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		t.Fatal(err)
	}
	builder.SetTimeoutHeight(timeoutHeight)
	return n.signBuilt(t, c, who, registryGas, builder)
}

// emptySignatureTx returns msg from who at who's next sequence with the
// given gas limit, SIGN_MODE_DIRECT, who's public key in the signer info,
// and an empty signature: the simulation transaction a client builds
// without the private key. It checks that the encoded bytes carry exactly
// one signature, of length 0, and who's public key.
func (n *regNet) emptySignatureTx(t *testing.T, c *cluster, who *actor, gas uint64, msg sdk.Msg) []byte {
	t.Helper()
	cfg := c.app().EncodingConfig().TxConfig
	builder := cfg.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		t.Fatal(err)
	}
	builder.SetGasLimit(gas)
	if err := builder.SetSignatures(signingtypes.SignatureV2{
		PubKey:   who.key.PubKey(),
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: n.seq[who.hexAddr()],
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := cfg.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	var envelope txtypes.TxRaw
	if err := gogoproto.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Signatures) != 1 || len(envelope.Signatures[0]) != 0 {
		t.Fatalf("simulation transaction carries %d signatures, want one empty signature", len(envelope.Signatures))
	}
	decoded, err := cfg.TxDecoder()(raw)
	if err != nil {
		t.Fatal(err)
	}
	sigTx, ok := decoded.(authsigning.SigVerifiableTx)
	if !ok {
		t.Fatalf("decoded %T has no signer infos", decoded)
	}
	pubs, err := sigTx.GetPubKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(pubs) != 1 || pubs[0] == nil || !pubs[0].Equals(who.key.PubKey()) {
		t.Fatalf("simulation transaction public keys %v, want %s's key", pubs, who.name)
	}
	return raw
}

// txSimulatePath is the abci_query path of the SDK tx service Simulate
// method. svnd registers the tx service on the gRPC query router at start
// (node/start.go, App.RegisterTxService), and app.Query serves every
// gRPC-routed path, so a node answers it over abci_query as well as gRPC.
const txSimulatePath = "/cosmos.tx.v1beta1.Service/Simulate"

// registerTxService registers the SDK tx service on chainApp's gRPC query
// router with the client context fields svnd sets that the service reads
// (codec, interface registry, tx config). The Simulate method calls
// App.Simulate and needs no node client.
func registerTxService(chainApp *app.App) {
	enc := chainApp.EncodingConfig()
	chainApp.RegisterTxService(client.Context{}.
		WithCodec(enc.Codec).
		WithInterfaceRegistry(enc.InterfaceRegistry).
		WithTxConfig(enc.TxConfig))
}

// txServiceSimulate sends raw to txSimulatePath through app.Query and
// decodes the binary SimulateResponse of a code 0 answer.
func txServiceSimulate(t *testing.T, chainApp *app.App, raw []byte) (*abci.ResponseQuery, *txtypes.SimulateResponse) {
	t.Helper()
	data, err := gogoproto.Marshal(&txtypes.SimulateRequest{TxBytes: raw})
	if err != nil {
		t.Fatal(err)
	}
	res, err := chainApp.Query(context.Background(), &abci.RequestQuery{Path: txSimulatePath, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 {
		return res, nil
	}
	var sim txtypes.SimulateResponse
	if err := gogoproto.Unmarshal(res.Value, &sim); err != nil {
		t.Fatalf("decode simulate response: %v", err)
	}
	return res, &sim
}

// TestTimeoutHeightThroughFinalizeBlock: the SDK TxTimeoutHeightDecorator
// (ante.go) refuses a transaction whose timeout_height is below the height
// of the context it runs in, with sdk code 30; a timeout_height equal to
// that height passes. Three nodes boot from one document and commit the
// same blocks up to height T-1. Then:
//
//   - node B finalizes block T carrying a transaction with
//     timeout_height = T: it commits with code 0, carries tx.acc_seq, and
//     advances the signer's sequence (the boundary height is valid);
//   - node A and the control node commit an empty block T. On node A,
//     CheckTx (check state at committed height T) refuses a transaction
//     with timeout_height = T-1 with sdk code 30 and admits the
//     transaction with timeout_height = T with code 0, although no later
//     block can include it. FinalizeBlock T+1 on node A refuses those same
//     bytes with sdk code 30: no event, so no tx.acc_seq; the signer's
//     sequence and every committed store, the auth store included,
//     unchanged; and node A's app hash equal to the control node's after
//     an empty block T+1. At committed height T+1, CheckTx and a recheck
//     (the mempool's re-validation after a block) refuse the bytes with
//     sdk code 30;
//   - a new transaction with the same message, the same sequence and
//     timeout_height = T+2 has other bytes and commits on node A in block
//     T+2.
func TestTimeoutHeightThroughFinalizeBlock(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 3)
	signer := nonzeroService(t, n)
	for c.height() < 3 {
		c.block(t)
	}
	nodeA, nodeB, control := c.nodes[0], c.nodes[1], c.nodes[2]
	deadline := uint64(c.height()) + 1 // T, the height of the next block
	seq := n.seq[signer.hexAddr()]
	msg := n.registerContent(t, signer, "reg-deadline", "c-deadline", 1, packageManifest("c-deadline"), sizeC1)
	atBoundary := n.signedWithTimeout(t, c, signer, deadline, msg)
	passed := n.signedWithTimeout(t, c, signer, deadline-1, msg)
	timedOut := sdkOutcome(sdkerrors.ErrTxTimeoutHeight)

	res := nodeB.deliver(t, atBoundary)
	if nodeB.height != int64(deadline) {
		t.Fatalf("node B finalized height %d, want %d", nodeB.height, deadline)
	}
	wantOutcome(t, "block T = timeout_height on node B", res.Codespace, res.Code, res.Log, accepted)
	wantAccSeq(t, "block T = timeout_height on node B", res, signer, seq)
	if len(typedEvents(res.Events)) == 0 {
		t.Fatal("block T on node B: the registration emitted no typed event")
	}
	wantSequence(t, "node B after block T", nodeB.app, signer, seq+1)

	nodeA.deliver(t, nil)
	control.deliver(t, nil)
	if nodeA.height != int64(deadline) || !bytes.Equal(nodeA.app.LastCommitID().Hash, control.app.LastCommitID().Hash) {
		t.Fatalf("node A and the control node differ after the empty block T (heights %d, %d)", nodeA.height, control.height)
	}
	wantCheckTx(t, "CheckTx at committed height T, timeout_height T-1", nodeA.app, passed, abci.CheckTxType_New, timedOut)
	wantCheckTx(t, "CheckTx at committed height T, timeout_height T", nodeA.app, atBoundary, abci.CheckTxType_New, accepted)

	before := takeStores(t, nodeA.app)
	res = nodeA.deliver(t, atBoundary)
	label := "block T+1 with timeout_height T on node A"
	wantOutcome(t, label, res.Codespace, res.Code, res.Log, timedOut)
	wantNoAccSeq(t, label, res)
	sameStores(t, label, nodeA.app, before)
	wantSequence(t, label, nodeA.app, signer, seq)
	control.deliver(t, nil)
	if !bytes.Equal(nodeA.app.LastCommitID().Hash, control.app.LastCommitID().Hash) {
		t.Fatalf("%s: app hash %X, control node after an empty block %X", label, nodeA.app.LastCommitID().Hash, control.app.LastCommitID().Hash)
	}
	wantCheckTx(t, "CheckTx at committed height T+1, timeout_height T", nodeA.app, atBoundary, abci.CheckTxType_New, timedOut)
	wantCheckTx(t, "recheck at committed height T+1, timeout_height T", nodeA.app, atBoundary, abci.CheckTxType_Recheck, timedOut)

	renewed := n.signedWithTimeout(t, c, signer, deadline+2, msg)
	if bytes.Equal(renewed, atBoundary) {
		t.Fatal("a higher timeout_height gave the same bytes")
	}
	res = nodeA.deliver(t, renewed)
	label = "block T+2 with timeout_height T+2 on node A"
	if nodeA.height != int64(deadline)+2 {
		t.Fatalf("%s: finalized height %d", label, nodeA.height)
	}
	wantOutcome(t, label, res.Codespace, res.Code, res.Log, accepted)
	wantAccSeq(t, label, res, signer, seq)
	wantSequence(t, label, nodeA.app, signer, seq+1)
}

// TestAccSeqPresentOnSuccess: a committed transaction with code 0 carries
// exactly one tx.acc_seq attribute, the signer's bech32 address, a slash,
// and the sequence it was signed at; the signer's committed sequence moves
// to the next value. Two transactions in a row show the value follows the
// sequence.
func TestAccSeqPresentOnSuccess(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	signer := nonzeroService(t, n)
	for _, id := range []string{"c-ok-1", "c-ok-2"} {
		label := "register " + id
		seq := n.seq[signer.hexAddr()]
		res := n.send(t, c, signer, n.registerContent(t, signer, "reg-"+id, id, 1, packageManifest(id), sizeC1))
		wantCode(t, label, res, msgkit.OK)
		wantAccSeq(t, label, res, signer, seq)
		if len(typedEvents(res.Events)) == 0 {
			t.Fatalf("%s: no typed event", label)
		}
		wantSequence(t, label, c.app(), signer, seq+1)
	}
}

// TestAccSeqPresentOnHandlerFailure: a transaction that passes the ante
// chain and fails in its message handler keeps the ante writes and the
// ante events. p1 is admitted with capacity sizeSmall and the acceptance
// of c1 reserves all of it; an offer of the one-byte c2 then passes
// CheckTx with code 0 (CheckTx runs only the ante chain) and commits with
// ERROR_CODE_CAPACITY. Its result carries tx.acc_seq=<signer>/<N>, the
// signer's sequence moves from N to N+1, the non-account state is
// byte-identical and there is no typed event, and the auth store equals
// its earlier rows once account sequences and public keys are blinded.
// The sequence is consumed: the same bytes again get sdk code 32 in
// CheckTx and in a block, with no event and every store unchanged, and the
// signer's next transaction at N+1 commits.
func TestAccSeqPresentOnHandlerFailure(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	signer := nonzeroService(t, n)
	n.registerAdmit(t, c, n.provider1, "p1", sizeSmall)
	sizes := map[string]uint64{"c1": sizeSmall, "c2": 1}
	for _, id := range []string{"c1", "c2"} {
		wantCode(t, "register "+id, n.send(t, c, signer, n.registerContent(t, signer, "reg-"+id, id, 1, packageManifest(id), sizes[id])), msgkit.OK)
	}
	now := c.now()
	deadline, expiry := now+1800, now+600
	offer := n.offerMsg(signer, "of-c1", "o-c1", "c1", "p1", sizeSmall, deadline, expiry)
	wantCode(t, "offer c1", n.send(t, c, signer, offer), msgkit.OK)
	wantCode(t, "accept l1", n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l1", offer, "l1")), msgkit.OK)

	seq := n.seq[signer.hexAddr()]
	over := n.signed(t, c, signer, registryGas, nil, n.offerMsg(signer, "of-c2", "o-c2", "c2", "p1", 1, deadline, expiry))
	wantCheckTx(t, "CheckTx of the offer over the capacity", chainApp, over, abci.CheckTxType_New, accepted)
	before := chainApp.FullSnapshot()
	blindBefore, err := chainApp.SignerBlindAuthSnapshotForTest()
	if err != nil {
		t.Fatal(err)
	}
	label := "offer over the capacity"
	res := n.deliverRaw(t, c, signer, over)
	wantCode(t, label, res, msgkit.Capacity)
	wantAccSeq(t, label, res, signer, seq)
	wantSequence(t, label, chainApp, signer, seq+1)
	unchanged(t, label, chainApp, before, res)
	blindAfter, err := chainApp.SignerBlindAuthSnapshotForTest()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blindBefore, blindAfter) {
		t.Fatalf("%s: the auth store changed beyond sequences and public keys", label)
	}

	wrongSequence := sdkOutcome(sdkerrors.ErrWrongSequence)
	wantCheckTx(t, "CheckTx of the failed offer again", chainApp, over, abci.CheckTxType_New, wrongSequence)
	stores := takeStores(t, chainApp)
	label = "the failed offer again in a block"
	res = n.deliverRaw(t, c, signer, over)
	wantOutcome(t, label, res.Codespace, res.Code, res.Log, wrongSequence)
	wantNoAccSeq(t, label, res)
	sameStores(t, label, chainApp, stores)
	wantSequence(t, label, chainApp, signer, seq+1)

	label = "the next transaction at N+1"
	res = n.send(t, c, signer, n.registerContent(t, signer, "reg-c3", "c3", 1, packageManifest("c3"), 1))
	wantCode(t, label, res, msgkit.OK)
	wantAccSeq(t, label, res, signer, seq+1)
	wantSequence(t, label, chainApp, signer, seq+2)
}

// TestAccSeqAbsentOnAnyAnteFailure: the SDK drops every write and every
// event of a transaction whose ante chain fails, whichever decorator
// refuses it, so the result carries no tx.acc_seq and the signer's
// sequence does not move. Each row is refused by CheckTx and by
// FinalizeBlock with the same code, with no event and every committed
// store, the auth store included, byte-identical:
//
//   - two admission refusals, which run after SetPubKey: a submitter whose
//     admission row was removed (ERROR_CODE_UNAUTHORIZED, its first
//     transaction, so the public key SetPubKey would have stored is not
//     kept either) and a council seat key signing an authority_end
//     MsgEndLease (ERROR_CODE_FORBIDDEN);
//   - a passed timeout_height (sdk code 30), before SetPubKey;
//   - a signature over the wrong account number (sdk code 4) and the bytes
//     of a committed transaction again (sdk code 32), after SetPubKey.
//
// Afterwards the signer's and the seat's next transactions, at the
// unchanged sequences, commit with tx.acc_seq naming those sequences.
func TestAccSeqAbsentOnAnyAnteFailure(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	signer := nonzeroService(t, n)
	revoked := otherService(n, signer)
	seat := n.seats[0]

	committed := n.signed(t, c, signer, registryGas, nil, n.registerContent(t, signer, "reg-a1", "c-a1", 1, packageManifest("c-a1"), sizeC1))
	wantCode(t, "first registration", n.deliverRaw(t, c, signer, committed), msgkit.OK)
	chainApp.RevokeAdmissionForTest(revoked.addr)
	c.block(t)

	rows := []struct {
		name string
		who  *actor
		raw  func() []byte
		want outcome
	}{
		{"admission refusal: submitter with no admission row", revoked, func() []byte {
			return n.signed(t, c, revoked, registryGas, nil, n.registerContent(t, revoked, "reg-r1", "c-r1", 1, packageManifest("c-r1"), sizeC1))
		}, svnOutcome(msgkit.Unauthorized)},
		{"admission refusal: council seat key signing a lease message", seat, func() []byte {
			return n.signed(t, c, seat, registryGas, nil, &leasesv1.MsgEndLease{Signer: seat.addr, BusinessId: "seat-ena", LeaseId: "l1", AuthorityEnd: true})
		}, svnOutcome(msgkit.Forbidden)},
		{"timeout_height below the block height", signer, func() []byte {
			return n.signedWithTimeout(t, c, signer, uint64(c.height())-1, n.registerContent(t, signer, "reg-a2", "c-a2", 1, packageManifest("c-a2"), sizeC1))
		}, sdkOutcome(sdkerrors.ErrTxTimeoutHeight)},
		{"signature over the wrong account number", signer, func() []byte {
			return signFull(t, chainApp, n.doc.ChainID, signer.key, n.numbers[signer.hexAddr()]+1, n.seq[signer.hexAddr()], registryGas, nil,
				n.registerContent(t, signer, "reg-a3", "c-a3", 1, packageManifest("c-a3"), sizeC1))
		}, sdkOutcome(sdkerrors.ErrUnauthorized)},
		{"the committed transaction again", signer, func() []byte { return committed }, sdkOutcome(sdkerrors.ErrWrongSequence)},
	}
	for _, row := range rows {
		raw := row.raw()
		wantCheckTx(t, row.name+": CheckTx", chainApp, raw, abci.CheckTxType_New, row.want)
		seq, known := chainApp.AccountSequenceForTest(row.who.addr)
		if !known {
			t.Fatalf("%s: %s has no auth account", row.name, row.who.name)
		}
		before := takeStores(t, chainApp)
		res := n.deliverRaw(t, c, row.who, raw)
		wantOutcome(t, row.name, res.Codespace, res.Code, res.Log, row.want)
		wantNoAccSeq(t, row.name, res)
		sameStores(t, row.name, chainApp, before)
		wantSequence(t, row.name, chainApp, row.who, seq)
	}

	seq := n.seq[signer.hexAddr()]
	label := "the signer's next transaction"
	res := n.send(t, c, signer, n.registerContent(t, signer, "reg-a4", "c-a4", 1, packageManifest("c-a4"), sizeC1))
	wantCode(t, label, res, msgkit.OK)
	wantAccSeq(t, label, res, signer, seq)
	label = "the seat's first transaction"
	res = n.send(t, c, seat, n.proposal(t, "prop-after", "admit-p9", admitPayload("p9")))
	wantCode(t, label, res, msgkit.OK)
	wantAccSeq(t, label, res, seat, 0)
}

// TestStrictDecoderAcceptsTimeoutHeight: timeout_height is TxBody field 3,
// a known field, so the strict decoder (encoding.go strictTxDecoder)
// accepts a body that sets it, whether the TxBuilder or a field-by-field
// envelope encodes it, and the decoded transaction reports it to the
// ante's timeout decorator. The strict body check still runs on such a
// body: the same envelope with a non-critical unknown field 1024 appended
// to the body fails decode (sdk code 2) in CheckTx and in a block, with no
// event and every store unchanged. The envelope without that field passes
// CheckTx and commits in a block below its timeout_height.
func TestStrictDecoderAcceptsTimeoutHeight(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	signer := nonzeroService(t, n)
	deadline := uint64(c.height()) + 5
	msg := n.registerContent(t, signer, "reg-strict", "c-strict", 1, packageManifest("c-strict"), sizeC1)
	setDeadline := func(body *txtypes.TxBody, _ *txtypes.AuthInfo) { body.TimeoutHeight = deadline }
	tail := protowire.AppendTag(nil, 1024, protowire.VarintType)
	tail = protowire.AppendVarint(tail, 1)
	envelope := n.signEnvelope(t, signer, msg, setDeadline, nil)
	withUnknown := n.signEnvelope(t, signer, msg, setDeadline, tail)
	built := n.signedWithTimeout(t, c, signer, deadline, msg)

	decode := chainApp.EncodingConfig().TxConfig.TxDecoder()
	for _, tc := range []struct {
		name string
		raw  []byte
	}{{"field-by-field envelope", envelope}, {"TxBuilder", built}} {
		var raw txtypes.TxRaw
		if err := gogoproto.Unmarshal(tc.raw, &raw); err != nil {
			t.Fatal(err)
		}
		var body txtypes.TxBody
		if err := gogoproto.Unmarshal(raw.BodyBytes, &body); err != nil {
			t.Fatal(err)
		}
		if body.TimeoutHeight != deadline {
			t.Fatalf("%s: body timeout_height %d, want %d", tc.name, body.TimeoutHeight, deadline)
		}
		decoded, err := decode(tc.raw)
		if err != nil {
			t.Fatalf("%s: strict decoder: %v", tc.name, err)
		}
		timed, ok := decoded.(authante.TxWithTimeoutHeight)
		if !ok {
			t.Fatalf("%s: decoded %T does not report a timeout height", tc.name, decoded)
		}
		if timed.GetTimeoutHeight() != deadline {
			t.Fatalf("%s: decoded timeout_height %d, want %d", tc.name, timed.GetTimeoutHeight(), deadline)
		}
	}

	_, err := decode(withUnknown)
	if space, code, _ := errorsmod.ABCIInfo(err, false); space != sdkerrors.ErrTxDecode.Codespace() || code != sdkerrors.ErrTxDecode.ABCICode() {
		t.Fatalf("unknown body field with timeout_height: decode %s/%d %v", space, code, err)
	}
	decodeFailed := sdkOutcome(sdkerrors.ErrTxDecode)
	wantCheckTx(t, "CheckTx of the unknown body field", chainApp, withUnknown, abci.CheckTxType_New, decodeFailed)
	before := takeStores(t, chainApp)
	label := "the unknown body field in a block"
	res := n.deliverRaw(t, c, signer, withUnknown)
	wantOutcome(t, label, res.Codespace, res.Code, res.Log, decodeFailed)
	wantNoAccSeq(t, label, res)
	sameStores(t, label, chainApp, before)

	wantCheckTx(t, "CheckTx of the envelope", chainApp, envelope, abci.CheckTxType_New, accepted)
	seq := n.seq[signer.hexAddr()]
	label = "the envelope in a block"
	res = n.deliverRaw(t, c, signer, envelope)
	if uint64(c.height()) > deadline {
		t.Fatalf("%s: height %d is above timeout_height %d", label, c.height(), deadline)
	}
	wantCode(t, label, res, msgkit.OK)
	wantAccSeq(t, label, res, signer, seq)
}

// TestSimulateWithEmptySignature: the SDK tx service Simulate method,
// sent through app.Query on txSimulatePath (the abci_query route a node
// serves), runs App.Simulate: the ante chain in simulate mode (no
// signature verification) and the message handler, on a branch of the
// check state. A MsgWithdrawContent transaction from the signer, carrying
// the signer's public key and an empty signature, is simulated with a
// declared gas limit of 0 (the SDK client's gas=auto request) and of
// registryGas: both answer code 0 with gas used of at least
// msgkit.FixedMessageGas and the handler's typed event, and change no
// committed store and no sequence. The same empty-signature bytes outside
// simulation are refused by the signature check, sdk code 4, in CheckTx
// and in a block, with no event and every store unchanged. The signed
// transaction that declares the registryGas estimate as its gas limit
// commits, with gas used not above the estimate; the test logs both. (For
// an empty signature the SDK's simulate mode charges transaction-size gas
// for a placeholder amino signature with its public key, which is longer
// than the 64-byte signature of the signed bytes.)
//
// Goal 11 plan 13.3 names the /app/simulate route; that route cannot
// encode its answer for an svn message
// (TestAppSimulatePathCannotEncodeSvnResults), so this test uses the tx
// service route, which returns the same App.Simulate result in binary.
func TestSimulateWithEmptySignature(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	registerTxService(chainApp)
	signer := nonzeroService(t, n)
	wantCode(t, "register c1", n.send(t, c, signer, n.registerContent(t, signer, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)), msgkit.OK)
	withdraw := n.withdrawContent(t, signer, "wd-c1", "c1", 1, reasonDigest("c1"))
	seq := n.seq[signer.hexAddr()]
	before := takeStores(t, chainApp)

	estimates := map[uint64]uint64{}
	for _, gas := range []uint64{0, registryGas} {
		label := fmt.Sprintf("simulate with declared gas %d", gas)
		res, sim := txServiceSimulate(t, chainApp, n.emptySignatureTx(t, c, signer, gas, withdraw))
		if res.Code != 0 || sim == nil {
			t.Fatalf("%s: code %s/%d log %s", label, res.Codespace, res.Code, res.Log)
		}
		if sim.GasInfo == nil || sim.GasInfo.GasUsed < msgkit.FixedMessageGas {
			t.Fatalf("%s: gas info %v, want gas used of at least %d", label, sim.GasInfo, msgkit.FixedMessageGas)
		}
		if sim.Result == nil || len(typedEvents(sim.Result.Events)) == 0 {
			t.Fatalf("%s: the simulation result has no typed event of the handler", label)
		}
		estimates[gas] = sim.GasInfo.GasUsed
		t.Logf("%s: gas used %d", label, sim.GasInfo.GasUsed)
	}
	sameStores(t, "after the simulations", chainApp, before)
	wantSequence(t, "after the simulations", chainApp, signer, seq)

	unsigned := n.emptySignatureTx(t, c, signer, registryGas, withdraw)
	sigFailed := sdkOutcome(sdkerrors.ErrUnauthorized)
	wantCheckTx(t, "CheckTx of the empty signature", chainApp, unsigned, abci.CheckTxType_New, sigFailed)
	label := "the empty signature in a block"
	res := n.deliverRaw(t, c, signer, unsigned)
	wantOutcome(t, label, res.Codespace, res.Code, res.Log, sigFailed)
	wantNoAccSeq(t, label, res)
	sameStores(t, label, chainApp, before)
	wantSequence(t, label, chainApp, signer, seq)

	declared := estimates[registryGas]
	label = "the signed withdrawal with the estimate as its gas limit"
	res = n.deliverRaw(t, c, signer, n.signed(t, c, signer, declared, nil, withdraw))
	wantCode(t, label, res, msgkit.OK)
	wantAccSeq(t, label, res, signer, seq)
	if res.GasUsed < 0 || uint64(res.GasUsed) > declared {
		t.Fatalf("%s: gas used %d, declared %d", label, res.GasUsed, declared)
	}
	t.Logf("%s: declared %d, gas used %d", label, declared, res.GasUsed)
}

// TestAppSimulatePathCannotEncodeSvnResults records current behaviour (a
// goal 11 finding, not a requirement): the abci_query route /app/simulate
// runs App.Simulate and then JSON-encodes the SimulationResponse with the
// app's interface registry (SDK baseapp handleQueryApp). The result's
// msg_responses hold one Any per message, typed
// /svn.<module>.v1.Msg<Name>Response, and the app's interface registry
// does not register those response types, so the route answers a non-zero
// code with "failed to JSON encode simulation response" and "unable to
// resolve type URL", for a MsgWithdrawContent with an empty signature and
// for a signed MsgRegisterContent alike. The same bytes simulate with code
// 0 through txSimulatePath, whose binary answer carries the one response
// Any with that type URL, and neither route changes a committed store. If
// the response types are registered, this test fails and is to be replaced
// by an /app/simulate form of TestSimulateWithEmptySignature. Pending the
// orchestrator's ruling on goal 11 Wave 0 finding B4 F1 (plan 1.1, 8.2,
// DR-11-27 name /app/simulate): evidence lists this test as a recorded
// deviation, not as a plan-conformant check.
func TestAppSimulatePathCannotEncodeSvnResults(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	registerTxService(chainApp)
	signer := nonzeroService(t, n)
	wantCode(t, "register c1", n.send(t, c, signer, n.registerContent(t, signer, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)), msgkit.OK)
	rows := []struct {
		name     string
		response string
		raw      []byte
	}{
		{"withdrawal with an empty signature", "/svn.content.v1.MsgWithdrawContentResponse",
			n.emptySignatureTx(t, c, signer, registryGas, n.withdrawContent(t, signer, "wd-c1", "c1", 1, reasonDigest("c1")))},
		{"signed registration", "/svn.content.v1.MsgRegisterContentResponse",
			n.signed(t, c, signer, registryGas, nil, n.registerContent(t, signer, "reg-c2", "c2", 1, packageManifest("c2"), sizeC1))},
	}
	before := takeStores(t, chainApp)
	for _, row := range rows {
		res, err := chainApp.Query(context.Background(), &abci.RequestQuery{Path: "/app/simulate", Data: row.raw})
		if err != nil {
			t.Fatalf("%s: /app/simulate: %v", row.name, err)
		}
		if res.Code == 0 || !strings.Contains(res.Log, "failed to JSON encode simulation response") ||
			!strings.Contains(res.Log, "unable to resolve type URL "+row.response) {
			t.Fatalf("%s: /app/simulate code %s/%d log %s", row.name, res.Codespace, res.Code, res.Log)
		}
		t.Logf("%s: /app/simulate code %s/%d log %s", row.name, res.Codespace, res.Code, res.Log)
		served, sim := txServiceSimulate(t, chainApp, row.raw)
		if served.Code != 0 || sim == nil || sim.GasInfo == nil || sim.GasInfo.GasUsed == 0 {
			t.Fatalf("%s: %s code %s/%d log %s", row.name, txSimulatePath, served.Codespace, served.Code, served.Log)
		}
		if sim.Result == nil || len(sim.Result.MsgResponses) != 1 || sim.Result.MsgResponses[0].TypeUrl != row.response {
			t.Fatalf("%s: %s result %v, want one %s", row.name, txSimulatePath, sim.Result, row.response)
		}
	}
	sameStores(t, "after the simulations", chainApp, before)
}
