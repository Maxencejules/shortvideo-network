package devnet_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/mempool"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The lease demos run four svnd validator processes on one machine
// (loopback peers, one network namespace in the test commands). They are
// not an independent-operator network and not a production test. The
// DEC-005 term (3600 s) is the devnet test number of the devnet genesis,
// not a policy.

const (
	devnetTerm    = int64(3600)
	capacitySmall = uint64(10_000_000)
	bulkGas       = uint64(10_000_000)
	activePath    = "/svn.leases.v1.Query/ActiveLeases"
	deadlinesPath = "/svn.leases.v1.Query/Deadlines"
	providerPath  = "/svn.providers.v1.Query/Provider"
)

func leaseManifest(contentID string) *typesv1.Digest {
	sum := sha256.Sum256(packageManifest(contentID))
	return sha256Digest(sum[:])
}

func leaseTerms(offerID string) *typesv1.Digest {
	sum := sha256.Sum256([]byte("svn-lease-terms:" + offerID))
	return sha256Digest(sum[:])
}

func (d *demo) offerMsg(bid, offerID, contentID, providerID string, size uint64, deadline, expiry int64) *leasesv1.MsgOfferLease {
	return &leasesv1.MsgOfferLease{
		Signer: d.service.key.PubKey().Address(), BusinessId: bid, OfferId: offerID,
		ContentId: contentID, ContentVersion: 1, ProviderId: providerID,
		ManifestDigest: leaseManifest(contentID), PackageBytes: size, TermsDigest: leaseTerms(offerID),
		StartUnixSeconds: deadline - devnetTerm, DeadlineUnixSeconds: deadline, ExpiryUnixSeconds: expiry, TermVersion: 1,
	}
}

func storedOffer(msg *leasesv1.MsgOfferLease) *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId: msg.GetOfferId(), ContentId: msg.GetContentId(), ContentVersion: msg.GetContentVersion(),
		ProviderId: msg.GetProviderId(), ManifestDigest: msg.GetManifestDigest(), PackageBytes: msg.GetPackageBytes(),
		TermsDigest: msg.GetTermsDigest(), StartUnixSeconds: msg.GetStartUnixSeconds(),
		DeadlineUnixSeconds: msg.GetDeadlineUnixSeconds(), ExpiryUnixSeconds: msg.GetExpiryUnixSeconds(),
		TermVersion: msg.GetTermVersion(), Scheduler: msg.GetSigner(),
	}
}

// acceptMsg relays offer's acceptance as leaseID, signed with key (the
// provider's registered signing key unless a test passes another).
func (d *demo) acceptMsg(who loaded, bid string, offer *leasesv1.MsgOfferLease, leaseID string, key ed25519Key) *leasesv1.MsgAcceptLease {
	d.t.Helper()
	signBytes, err := leases.AcceptSignBytes(storedOffer(offer), leaseID)
	if err != nil {
		d.t.Fatal(err)
	}
	return &leasesv1.MsgAcceptLease{
		Signer: who.key.PubKey().Address(), BusinessId: bid, OfferId: offer.GetOfferId(), LeaseId: leaseID,
		ProviderAcceptance: signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, key, signBytes),
	}
}

func (d *demo) renewMsg(who loaded, bid, leaseID, providerID, contentID string, size, termVersion uint64, deadline int64) *leasesv1.MsgRenewLease {
	d.t.Helper()
	manifest := leaseManifest(contentID)
	signBytes, err := leases.RenewSignBytes(leaseID, termVersion, deadline, manifest, size, "")
	if err != nil {
		d.t.Fatal(err)
	}
	return &leasesv1.MsgRenewLease{
		Signer: who.key.PubKey().Address(), BusinessId: bid, LeaseId: leaseID, TermVersion: termVersion,
		DeadlineUnixSeconds: deadline, ManifestDigest: manifest, PackageBytes: size,
		ProviderSignature: signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, d.keys[providerID], signBytes),
	}
}

// endMsg is a provider-signed end; termVersion is the lease's term version
// when the provider signs (the payload binds it, D-12).
func (d *demo) endMsg(who loaded, bid, leaseID, providerID string, termVersion uint64) *leasesv1.MsgEndLease {
	d.t.Helper()
	signBytes, err := leases.EndSignBytes(leaseID, providerID, termVersion)
	if err != nil {
		d.t.Fatal(err)
	}
	return &leasesv1.MsgEndLease{
		Signer: who.key.PubKey().Address(), BusinessId: bid, LeaseId: leaseID,
		ProviderSignature: signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, d.keys[providerID], signBytes),
	}
}

func (d *demo) authorityEndMsg(bid, leaseID string) *leasesv1.MsgEndLease {
	return &leasesv1.MsgEndLease{Signer: d.authority.key.PubKey().Address(), BusinessId: bid, LeaseId: leaseID, AuthorityEnd: true}
}

func governedChange(key string, value uint64) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
		GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value},
	}}
}

// signMulti signs one transaction from who carrying msgs at who's next
// sequence.
func (d *demo) signMulti(who loaded, gas uint64, msgs ...sdk.Msg) []byte {
	d.t.Helper()
	builder := d.enc.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		d.t.Fatal(err)
	}
	builder.SetGasLimit(gas)
	sequence := d.seq[who.addr]
	signer := authsigning.SignerData{
		Address: sdk.AccAddress(who.key.PubKey().Address()).String(), ChainID: d.chainID,
		AccountNumber: d.numbers[who.addr], Sequence: sequence, PubKey: who.key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{
		PubKey: who.key.PubKey(), Sequence: sequence,
		Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		d.t.Fatal(err)
	}
	sig, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, who.key, d.enc.TxConfig, sequence)
	if err != nil {
		d.t.Fatal(err)
	}
	if err := builder.SetSignatures(sig); err != nil {
		d.t.Fatal(err)
	}
	raw, err := d.enc.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		d.t.Fatal(err)
	}
	return raw
}

// commitMulti commits one multi-message transaction (code 0).
func (d *demo) commitMulti(phase string, who loaded, msgs ...sdk.Msg) int64 {
	d.t.Helper()
	res, err := d.broadcast(d.signMulti(who, bulkGas, msgs...))
	if err != nil {
		d.t.Fatalf("phase %s: %v", phase, err)
	}
	if res.check.Code != 0 || res.deliver.Code != 0 {
		d.t.Fatalf("phase %s: check %s/%d deliver %s/%d %s", phase, res.check.Codespace, res.check.Code, res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
	}
	d.seq[who.addr]++
	d.included = append(d.included, hex.EncodeToString(res.hash))
	fmt.Printf("phase=%s height=%d code=0 messages=%d gas_used=%d txhash=%X\n", phase, res.height, len(msgs), res.deliver.GasUsed, res.hash)
	return res.height
}

// broadcastSync sends raw with broadcast_tx_sync to client: the reply is
// CheckTx (the ante chain only), not the committed result.
func (d *demo) broadcastSync(client *rpchttp.HTTP, raw []byte) (*coretypes.ResultBroadcastTx, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.BroadcastTxSync(ctx, cmttypes.Tx(raw))
}

// waitTx polls node v1 until the transaction hash is in a committed block
// and returns its committed result.
func (d *demo) waitTx(hash []byte, timeout time.Duration) *coretypes.ResultTx {
	d.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := d.clients[0].Tx(context.Background(), hash, false)
		if err == nil {
			return res
		}
		time.Sleep(200 * time.Millisecond)
	}
	d.t.Fatalf("tx %X not committed within %s", hash, timeout)
	return nil
}

// blockTime is the header time of height on node v1, in unix seconds.
func (d *demo) blockTime(height int64) int64 {
	d.t.Helper()
	h := height
	block, err := d.clients[0].Block(context.Background(), &h)
	if err != nil {
		d.t.Fatal(err)
	}
	return block.Block.Time.Unix()
}

// chainTime is the header time of the latest block on node v1.
func (d *demo) chainTime() int64 {
	d.t.Helper()
	return d.blockTime(d.height())
}

// waitChainTime waits until node v1's latest header time is at least
// when, and returns that height.
func (d *demo) waitChainTime(when int64, timeout time.Duration) int64 {
	d.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h := d.height()
		if d.blockTime(h) >= when {
			return h
		}
		time.Sleep(300 * time.Millisecond)
	}
	d.t.Fatalf("header time did not reach %d within %s", when, timeout)
	return 0
}

// expiryItem is one item BeginBlock expiry closed.
type expiryItem struct {
	kind, id string
}

func (e expiryItem) String() string { return e.kind + ":" + e.id }

// blockExpiry reads /block_results of height on node v1 and returns the
// BeginBlock expiry items in event order: LeaseEnded with state EXPIRED
// (parsed with msgkit.ParseBlockEvent) and leases_offer_closed with
// reason EXPIRED, both ending with mode=BeginBlock.
func (d *demo) blockExpiry(height int64) []expiryItem {
	d.t.Helper()
	h := height
	// CometBFT stores a block (and reports it as the latest) before it
	// executes it and saves its results, so the results of the latest
	// height can be briefly missing: poll.
	var res *coretypes.ResultBlockResults
	deadline := time.Now().Add(30 * time.Second)
	for {
		var err error
		res, err = d.clients[0].BlockResults(context.Background(), &h)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("block results at %d: %v", height, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	var items []expiryItem
	for _, ev := range res.FinalizeBlockEvents {
		n := len(ev.Attributes)
		if n == 0 || ev.Attributes[n-1].Key != msgkit.ModeKey || ev.Attributes[n-1].Value != "BeginBlock" {
			continue
		}
		switch {
		case ev.Type == leases.EventOfferClosed:
			if n != 3 || ev.Attributes[0].Key != leases.AttrOfferID || ev.Attributes[1].Value != leases.ReasonExpired {
				d.t.Fatalf("height %d: %s %v", height, ev.Type, ev.Attributes)
			}
			items = append(items, expiryItem{"offer", ev.Attributes[0].Value})
		case strings.HasPrefix(ev.Type, "svn.events.v1."):
			msg, _, err := msgkit.ParseBlockEvent(ev)
			if err != nil {
				d.t.Fatalf("height %d: %v", height, err)
			}
			ended, ok := msg.(*eventsv1.LeaseEnded)
			if !ok || ended.GetState() != "EXPIRED" {
				d.t.Fatalf("height %d: BeginBlock event %v", height, msg)
			}
			items = append(items, expiryItem{"lease", ended.GetLeaseId()})
		}
	}
	return items
}

// leaseQuery runs one ABCI query on client at height (0: latest) and
// returns the bytes. CometBFT reports a block as the latest once it is in
// the block store, which can be before the application has committed it
// (and a restarted node's application can trail its block store while it
// catches up), so a query at a fixed height first waits until the node's
// application (abci_info last_block_height) has committed that height.
func (d *demo) leaseQuery(client *rpchttp.HTTP, path string, req proto.Message, height int64) []byte {
	d.t.Helper()
	if height > 0 {
		deadline := time.Now().Add(60 * time.Second)
		for {
			info, err := client.ABCIInfo(context.Background())
			if err == nil && info.Response.LastBlockHeight >= height {
				break
			}
			if time.Now().After(deadline) {
				d.t.Fatalf("the application did not commit height %d within 60s (last error %v)", height, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	return d.abciQuery(client, path, req, height)
}

// heldOn returns the ids of the held leases of providerID at height.
func (d *demo) heldOn(client *rpchttp.HTTP, providerID string, height int64) []string {
	d.t.Helper()
	var res leasesv1.QueryActiveLeasesResponse
	raw := d.leaseQuery(client, activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: providerID, Page: &typesv1.PageRequest{Limit: 100}}, height)
	if err := proto.Unmarshal(raw, &res); err != nil {
		d.t.Fatal(err)
	}
	if len(res.GetPage().GetNextPageKey()) != 0 {
		d.t.Fatal("more than one page of held leases")
	}
	var ids []string
	for _, lease := range res.GetLeases() {
		ids = append(ids, lease.GetLeaseId())
	}
	return ids
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// reservedAt is providerID's committed reserved bytes at height on client.
func (d *demo) reservedAt(client *rpchttp.HTTP, providerID string, height int64) uint64 {
	d.t.Helper()
	var res providersv1.QueryProviderResponse
	if err := proto.Unmarshal(d.leaseQuery(client, providerPath, &providersv1.QueryProviderRequest{ProviderId: providerID}, height), &res); err != nil {
		d.t.Fatal(err)
	}
	return res.GetProvider().GetReservedBytes()
}

// eventTypes lists the x/leases event types of one committed result.
func eventTypes(res abci.ExecTxResult) []string {
	var out []string
	for _, ev := range res.Events {
		if strings.HasPrefix(ev.Type, "svn.events.v1.Lease") || ev.Type == leases.EventOfferClosed || ev.Type == "leases_result" {
			out = append(out, ev.Type)
		}
	}
	return out
}

// effectiveExpiry finds the first height after from whose header time is
// at or after when, and checks it is the height where BeginBlock expired
// want: no block from from+1 to effective-1 expired anything, the block
// at effective-1 has a header time before when, and the block at
// effective expired exactly want.
func (d *demo) effectiveExpiry(from, when int64, want []expiryItem) int64 {
	d.t.Helper()
	latest := d.height()
	for h := from + 1; h <= latest; h++ {
		items := d.blockExpiry(h)
		if d.blockTime(h) < when {
			if len(items) != 0 {
				d.t.Fatalf("height %d (header time %d < %d) expired %v", h, d.blockTime(h), when, items)
			}
			continue
		}
		if fmt.Sprint(items) != fmt.Sprint(want) {
			d.t.Fatalf("height %d (header time %d >= %d) expired %v, want %v", h, d.blockTime(h), when, items, want)
		}
		return h
	}
	d.t.Fatalf("no block at or after header time %d up to height %d", when, latest)
	return 0
}

// rebroadcastRaw sends signed bytes that already committed again: the
// application's CheckTx refuses them with the SDK sequence error, and a
// broadcast is refused by the mempool cache or by the same sequence
// error. The original inclusion does not move.
func (d *demo) rebroadcastRaw(phase string, raw []byte) {
	d.t.Helper()
	sum := sha256.Sum256(raw)
	// The node indexes a committed transaction asynchronously, so the
	// lookup of the original inclusion polls.
	original := d.waitTx(sum[:], 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	checked, err := d.clients[0].CheckTx(ctx, raw)
	cancel()
	if err != nil {
		d.t.Fatalf("phase %s: check_tx: %v", phase, err)
	}
	wrongSequence := sdkerrors.ErrWrongSequence.ABCICode()
	if checked.Codespace != sdkerrors.RootCodespace || checked.Code != wrongSequence {
		d.t.Fatalf("phase %s: check_tx %s/%d, want sdk/%d", phase, checked.Codespace, checked.Code, wrongSequence)
	}
	res, err := d.broadcast(raw)
	outcome := ""
	switch {
	case err != nil:
		var rpcErr *rpctypes.RPCError
		if !errors.As(err, &rpcErr) || rpcErr.Data != "error on broadcastTxCommit: "+mempool.ErrTxInCache.Error() {
			d.t.Fatalf("phase %s: broadcast error %v is not the mempool cache refusal", phase, err)
		}
		outcome = "mempool ErrTxInCache"
	case res.check.Codespace == sdkerrors.RootCodespace && res.check.Code == wrongSequence:
		outcome = fmt.Sprintf("checktx sdk/%d (wrong sequence)", wrongSequence)
	default:
		d.t.Fatalf("phase %s: broadcast check tx %s/%d", phase, res.check.Codespace, res.check.Code)
	}
	again, err := d.clients[0].Tx(context.Background(), sum[:], false)
	if err != nil || again.Height != original.Height {
		d.t.Fatalf("phase %s: inclusion moved from %d", phase, original.Height)
	}
	fmt.Printf("phase=%s check_tx=sdk/%d broadcast=%q original_height=%d included_again=false\n", phase, checked.Code, outcome, original.Height)
}

// startDevnet initializes and launches four validators and waits for
// height 3. stops[i] stops validator i; a nil entry is already stopped.
func startDevnet(t *testing.T) (devnet.Layout, string, []func() error) {
	t.Helper()
	home := t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	stops := make([]func() error, 4)
	for i := range stops {
		stop, err := launch(layout.Topology.Validators[i].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[i] = stop
	}
	t.Cleanup(func() {
		for _, stop := range stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	if _, _, err := devnet.WaitEqual(rpcList(layout), 3, 45*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	return layout, home, stops
}

// TestDevnetLeaseLifecycle is the Goal 08 real-transaction lease demo on
// four svnd processes on one machine. It shows that a broadcast
// acknowledgement (CheckTx) is not a committed result, that a client
// resubmitting the same business payload under new tx hashes (after a
// timeout, or after its node went down) gets one effect and one
// reservation, that rejected transactions leave the full state unchanged
// (replay), that expiry follows the block header time (effective-1 and
// effective heights), that a governed expiry budget of 4 drains a shared
// deadline in bounded blocks, and that withdrawal, disablement, and repair
// behave as specified. Every block is then replayed into an in-process
// application and must reproduce the header app_hash of all four nodes.
func TestDevnetLeaseLifecycle(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	fmt.Println("label=single-machine four-process devnet (not independent operators)")
	layout, home, stops := startDevnet(t)
	rpcs := rpcList(layout)
	d := newDemo(t, layout, home)
	fmt.Printf("phase=start height=%d chain_id=%s accounts=%d\n", d.height(), layout.Topology.ChainID, len(layout.Topology.Accounts))

	// Phase 1: providers and content.
	d.commit("register-p1", d.provider1, d.registerProvider(d.provider1, "reg-p1", "p1", capacityOne))
	d.commit("register-p2", d.provider2, d.registerProvider(d.provider2, "reg-p2", "p2", capacityOne))
	d.commit("register-p3", d.provider2, d.registerProvider(d.provider2, "reg-p3", "p3", capacitySmall))
	for _, id := range []string{"p1", "p2", "p3"} {
		d.council("admit-"+id, "prop-admit-"+id, "admit-"+id, admitChange(id))
	}
	contents := []sdk.Msg{}
	for _, id := range []string{"c1", "c3", "c4", "c5", "c6"} {
		contents = append(contents, d.registerContent(d.service, "reg-"+id, id, 1, sizeOne))
	}
	contents = append(contents, d.registerContent(d.service, "reg-c2", "c2", 1, sizeTwo))
	d.commitMulti("register-contents", d.service, contents...)
	now := d.chainTime()
	far, offerExpiry := now+devnetTerm, now+3000

	// Phase 2: a broadcast acknowledgement is not a committed result. The
	// acceptance passes CheckTx (ante only) and fails in its block with
	// CAPACITY: p3 (10 MB) already holds the 9 MB lease of c2.
	big := d.offerMsg("of-big", "o-big", "c2", "p3", sizeTwo, far, offerExpiry)
	small := d.offerMsg("of-small", "o-small", "c1", "p3", sizeOne, far, offerExpiry)
	d.commitMulti("offers-p3", d.service, big, small)
	d.commit("accept-big", d.broadcaster, d.acceptMsg(d.broadcaster, "ac-big", big, "l-big", d.keys["p3"]))
	raw := d.signMulti(d.broadcaster, 2_000_000, d.acceptMsg(d.broadcaster, "ac-small", small, "l-small", d.keys["p3"]))
	ack, err := d.broadcastSync(d.clients[0], raw)
	if err != nil {
		t.Fatal(err)
	}
	d.seq[d.broadcaster.addr]++
	if ack.Code != 0 {
		t.Fatalf("ack: CheckTx %s/%d %s", ack.Codespace, ack.Code, ack.Log)
	}
	committed := d.waitTx(ack.Hash, 60*time.Second)
	if committed.TxResult.Codespace != "svn" || committed.TxResult.Code != uint32(typesv1.ErrorCode_ERROR_CODE_CAPACITY) {
		t.Fatalf("committed result %s/%d, want svn/13", committed.TxResult.Codespace, committed.TxResult.Code)
	}
	if got := d.reservedAt(d.clients[0], "p3", committed.Height); got != sizeTwo {
		t.Fatalf("p3 reserved %d after the failed acceptance", got)
	}
	fmt.Printf("phase=ack-vs-commit txhash=%X ack=%d committed=%s/%d height=%d p3_reserved=%d\n", ack.Hash, ack.Code, committed.TxResult.Codespace, committed.TxResult.Code, committed.Height, sizeTwo)

	// Phase 3: timeout and retry. The client broadcasts the acceptance of
	// la, does not wait (its timeout), and re-signs the same business id
	// and payload at the next sequence: a distinct tx hash.
	oa := d.offerMsg("of-oa", "oa", "c1", "p1", sizeOne, far, offerExpiry)
	d.commit("offer-oa", d.service, oa)
	acceptA := d.acceptMsg(d.broadcaster, "ac-la", oa, "la", d.keys["p1"])
	raw1 := d.signMulti(d.broadcaster, 2_000_000, acceptA)
	ack1, err := d.broadcastSync(d.clients[0], raw1)
	if err != nil || ack1.Code != 0 {
		t.Fatalf("first submission: %v %v", err, ack1)
	}
	d.seq[d.broadcaster.addr]++
	raw2 := d.signMulti(d.broadcaster, 2_000_000, acceptA)
	ack2, err := d.broadcastSync(d.clients[0], raw2)
	if err != nil || ack2.Code != 0 {
		t.Fatalf("retry: %v %v", err, ack2)
	}
	d.seq[d.broadcaster.addr]++
	if bytes.Equal(ack1.Hash, ack2.Hash) {
		t.Fatal("the retry has the same tx hash")
	}
	r1, r2 := d.waitTx(ack1.Hash, 60*time.Second), d.waitTx(ack2.Hash, 60*time.Second)
	if r1.TxResult.Code != 0 || r2.TxResult.Code != 0 {
		t.Fatalf("submissions: %d %d", r1.TxResult.Code, r2.TxResult.Code)
	}
	ev1, ev2 := fmt.Sprint(eventTypes(r1.TxResult)), fmt.Sprint(eventTypes(r2.TxResult))
	accepted, idempotent := "[svn.events.v1.LeaseAccepted]", "[leases_result]"
	if !(ev1 == accepted && ev2 == idempotent) && !(ev1 == idempotent && ev2 == accepted) {
		t.Fatalf("one acceptance and one idempotent result expected: %s, %s", ev1, ev2)
	}
	retryHeight := max(r1.Height, r2.Height)
	if held := d.heldOn(d.clients[0], "p1", retryHeight); fmt.Sprint(held) != "[la]" {
		t.Fatalf("held on p1 after the retry: %v", held)
	}
	if got := d.reservedAt(d.clients[0], "p1", retryHeight); got != sizeOne {
		t.Fatalf("p1 reserved %d after the retry, want %d once", got, sizeOne)
	}
	fmt.Printf("phase=timeout-retry first=%X height=%d events=%s retry=%X height=%d events=%s p1_reserved=%d leases=1\n",
		ack1.Hash, r1.Height, ev1, ack2.Hash, r2.Height, ev2, sizeOne)
	d.reject("changed-payload-same-business-id", d.broadcaster, d.acceptMsg(d.broadcaster, "ac-la", oa, "la-2", d.keys["p1"]), msgkit.Replay)

	// A council seat key is not a relay (DR-06-06). Its acceptance, with a
	// valid provider signature, is refused by CheckTx on the node with
	// ERROR_CODE_FORBIDDEN, so it never enters a block; the seat's sequence
	// does not move.
	seatRaw := d.signMulti(d.seats[0], 2_000_000, d.acceptMsg(d.seats[0], "ac-seat", oa, "l-seat", d.keys["p1"]))
	seatAck, err := d.broadcastSync(d.clients[0], seatRaw)
	if err != nil {
		t.Fatal(err)
	}
	if seatAck.Codespace != "svn" || seatAck.Code != uint32(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN) || !strings.Contains(seatAck.Log, "a council seat key may sign only") {
		t.Fatalf("seat-signed acceptance: CheckTx %s/%d %s", seatAck.Codespace, seatAck.Code, seatAck.Log)
	}
	fmt.Printf("phase=seat-relay-refused seat=seat-1 checktx=%s/%d\n", seatAck.Codespace, seatAck.Code)

	// Phase 4: the client's node is down. The broadcast to v4 fails, the
	// same signed bytes commit once through v1, a later rebroadcast is
	// refused, and v4 catches up after its restart.
	if err := stops[3](); err != nil {
		t.Fatal(tail(layout, err))
	}
	stops[3] = nil
	renew := d.renewMsg(d.broadcaster, "rn-la-2", "la", "p1", "c1", sizeOne, 2, far+devnetTerm)
	rawRenew := d.signMulti(d.broadcaster, 2_000_000, renew)
	if _, err := d.broadcastSync(d.clients[3], rawRenew); err == nil {
		t.Fatal("broadcast to the stopped node v4 succeeded")
	} else {
		fmt.Printf("phase=node-down node=v4 broadcast_error=%q\n", firstLine(err.Error()))
	}
	res, err := d.broadcast(rawRenew)
	if err != nil || res.check.Code != 0 || res.deliver.Code != 0 {
		t.Fatalf("renewal through v1: %v %+v", err, res)
	}
	d.seq[d.broadcaster.addr]++
	fmt.Printf("phase=node-down-retry node=v1 height=%d code=0 events=%v txhash=%X\n", res.height, eventTypes(res.deliver), res.hash)
	d.rebroadcastRaw("node-down-rebroadcast", rawRenew)
	stop, err := launch(layout.Topology.Validators[3].Home)
	if err != nil {
		t.Fatal(err)
	}
	stops[3] = stop
	common, _, err := devnet.WaitEqual(rpcs, d.height()+2, 90*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	for _, q := range []struct {
		path string
		req  proto.Message
	}{
		{activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: "p1"}},
		{providerPath, &providersv1.QueryProviderRequest{ProviderId: "p1"}},
	} {
		if !bytes.Equal(d.leaseQuery(d.clients[0], q.path, q.req, common), d.leaseQuery(d.clients[3], q.path, q.req, common)) {
			t.Fatalf("v4 %s at %d differs from v1 after its restart", q.path, common)
		}
	}
	fmt.Printf("phase=node-restart node=v4 common_height=%d queries_equal_v1=true\n", common)

	// Phase 5: renewal, end, and the rejections around them.
	retryRenew := d.send(d.broadcaster, renew)
	if retryRenew.deliver.Code != 0 || fmt.Sprint(eventTypes(retryRenew.deliver)) != "[leases_result]" {
		t.Fatalf("renewal retry: %d %v", retryRenew.deliver.Code, eventTypes(retryRenew.deliver))
	}
	fmt.Printf("phase=renew-retry height=%d code=0 events=[leases_result]\n", retryRenew.height)
	d.reject("stale-renewal", d.broadcaster, d.renewMsg(d.broadcaster, "rn-la-2b", "la", "p1", "c1", sizeOne, 2, far+2*devnetTerm), msgkit.StaleVersion)
	d.reject("renewal-other-bytes", d.broadcaster, d.renewMsg(d.broadcaster, "rn-la-3x", "la", "p1", "c1", sizeOne+1, 3, far+2*devnetTerm), msgkit.Conflict)
	ob := d.offerMsg("of-ob", "ob", "c3", "p1", sizeOne, far, offerExpiry)
	oc := d.offerMsg("of-oc", "oc", "c5", "p1", sizeOne, far, offerExpiry)
	ow := d.offerMsg("of-ow", "ow", "c4", "p1", sizeOne, far, offerExpiry)
	osvc := d.offerMsg("of-osvc", "osvc", "c4", "p2", sizeOne, far, offerExpiry)
	d.commitMulti("offers-phase-5", d.service, ob, oc, ow, osvc)
	d.commitMulti("accepts-phase-5", d.broadcaster,
		d.acceptMsg(d.broadcaster, "ac-lb", ob, "lb", d.keys["p1"]),
		d.acceptMsg(d.broadcaster, "ac-lc", oc, "lc", d.keys["p1"]),
		d.acceptMsg(d.broadcaster, "ac-lw", ow, "lw", d.keys["p1"]))
	end := d.endMsg(d.provider1, "en-lb", "lb", "p1", 1)
	d.commit("provider-end-lb", d.provider1, end)
	endRetry := d.send(d.provider1, end)
	if endRetry.deliver.Code != 0 || fmt.Sprint(eventTypes(endRetry.deliver)) != "[leases_result]" {
		t.Fatalf("end retry: %d %v", endRetry.deliver.Code, eventTypes(endRetry.deliver))
	}
	fmt.Printf("phase=end-retry height=%d code=0 events=[leases_result]\n", endRetry.height)
	d.reject("end-again-new-business-id", d.provider1, d.endMsg(d.provider1, "en-lb-2", "lb", "p1", 1), msgkit.Underflow)
	d.commit("authority-end-lc", d.authority, d.authorityEndMsg("en-lc", "lc"))
	d.reject("duplicate-replica", d.service, d.offerMsg("of-dup", "o-dup", "c1", "p1", sizeOne, far, offerExpiry), msgkit.DuplicateID)
	serviceKey := ed25519Key{id: d.keys["p2"].id, priv: d.intent.priv}
	d.reject("service-key-acceptance", d.broadcaster, d.acceptMsg(d.broadcaster, "x-svc-1", osvc, "l-svc", serviceKey), msgkit.Unauthorized)
	d.reject("service-outer-signer", d.service, d.acceptMsg(d.service, "x-svc-2", osvc, "l-svc", d.keys["p2"]), msgkit.Forbidden)

	// Phase 6: expiry by header time.
	now = d.chainTime()
	deadline6 := now + 25
	oe := d.offerMsg("of-oe", "oe", "c3", "p2", sizeOne, deadline6, now+20)
	d.commit("offer-oe", d.service, oe)
	acceptHeight := d.commit("accept-le", d.provider2, d.acceptMsg(d.provider2, "ac-le", oe, "le", d.keys["p2"]))
	d.waitChainTime(deadline6+1, 90*time.Second)
	effective := d.effectiveExpiry(acceptHeight, deadline6, []expiryItem{{"lease", "le"}})
	before, after := d.heldOn(d.clients[0], "p2", effective-1), d.heldOn(d.clients[0], "p2", effective)
	if !contains(before, "le") || contains(after, "le") {
		t.Fatalf("le held at %d: %v; at %d: %v", effective-1, before, effective, after)
	}
	released := d.reservedAt(d.clients[0], "p2", effective-1) - d.reservedAt(d.clients[0], "p2", effective)
	if released != sizeOne {
		t.Fatalf("expiry released %d", released)
	}
	fmt.Printf("phase=expiry deadline=%d effective_minus_1=%d header_time=%d held=true effective=%d header_time=%d held=false released=%d\n",
		deadline6, effective-1, d.blockTime(effective-1), effective, d.blockTime(effective), released)

	// Phase 7: a governed expiry budget of 4 drains 24 leases and 6 offers
	// that share one deadline and expiry in bounded blocks.
	d.council("expiry-steps-4", "prop-expiry-4", msgkit.LimitMaxExpirySteps, governedChange(msgkit.LimitMaxExpirySteps, 4))
	var drainContents []sdk.Msg
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("d%02d", i)
		drainContents = append(drainContents, d.registerContent(d.service, "reg-"+id, id, 1, 1000))
	}
	d.commitMulti("register-drain-contents", d.service, drainContents...)
	due := d.chainTime() + 45
	var offers, accepts []sdk.Msg
	var want []expiryItem
	for _, provider := range []string{"p1", "p2"} {
		for i := 1; i <= 12; i++ {
			content := fmt.Sprintf("d%02d", i)
			offer := d.offerMsg("of-q"+provider+content, "q"+provider+"-"+content, content, provider, 1000, due, due)
			offers = append(offers, offer)
			leaseID := "m" + provider + "-" + content
			accepts = append(accepts, d.acceptMsg(d.broadcaster, "ac-"+leaseID, offer, leaseID, d.keys[provider]))
			want = append(want, expiryItem{"lease", leaseID})
		}
	}
	for i := 1; i <= 6; i++ {
		content := fmt.Sprintf("d%02d", i)
		id := "z-" + content
		offers = append(offers, d.offerMsg("of-"+id, id, content, "p1", 1000, due+1800, due))
		want = append(want, expiryItem{"offer", id})
	}
	d.commitMulti("drain-offers", d.service, offers...)
	acceptsHeight := d.commitMulti("drain-accepts", d.broadcaster, accepts...)
	if now := d.chainTime(); now >= due {
		t.Fatalf("setup ended at header time %d, after the shared deadline %d", now, due)
	}
	d.waitChainTime(due, 120*time.Second)
	first := d.effectiveExpiry(acceptsHeight, due, want[:4])
	pos, counts := 0, []int{}
	for h := first; pos < len(want); h++ {
		for d.height() < h {
			time.Sleep(200 * time.Millisecond)
		}
		items := d.blockExpiry(h)
		n := min(4, len(want)-pos)
		if fmt.Sprint(items) != fmt.Sprint(want[pos:pos+n]) {
			t.Fatalf("drain height %d: %v, want %v", h, items, want[pos:pos+n])
		}
		counts = append(counts, len(items))
		pos += n
	}
	if fmt.Sprint(counts) != "[4 4 4 4 4 4 4 2]" {
		t.Fatalf("per-block counts %v", counts)
	}
	last := first + int64(len(counts)) - 1
	for d.height() < last+1 {
		time.Sleep(200 * time.Millisecond)
	}
	if items := d.blockExpiry(last + 1); len(items) != 0 {
		t.Fatalf("height %d after the drain: %v", last+1, items)
	}
	fmt.Printf("phase=bounded-drain max_expiry_steps=4 shared_deadline=%d heights=%d..%d effective_minus_1=%d header_time=%d counts=%v order=(time,kind,id)\n",
		due, first, last, first-1, d.blockTime(first-1), counts)

	// Phase 8: withdrawal voids open offers; the active lease on the
	// withdrawn content keeps its reservation until it ends.
	reservedBefore := d.reservedAt(d.clients[0], "p1", 0)
	wd := d.send(d.service, d.withdrawContent(d.service, "wd-c4", "c4", 1, reason("c4")))
	if wd.deliver.Code != 0 || fmt.Sprint(eventTypes(wd.deliver)) != "[leases_offer_closed]" {
		t.Fatalf("withdraw c4: %d %v", wd.deliver.Code, eventTypes(wd.deliver))
	}
	fmt.Printf("phase=withdraw-c4 height=%d code=0 voided_offers=1 txhash=%X\n", wd.height, wd.hash)
	if got := d.reservedAt(d.clients[0], "p1", wd.height); got != reservedBefore || !contains(d.heldOn(d.clients[0], "p1", wd.height), "lw") {
		t.Fatalf("withdrawal changed p1: reserved %d (was %d)", got, reservedBefore)
	}
	d.reject("accept-voided-offer", d.broadcaster, d.acceptMsg(d.broadcaster, "ac-l-svc", osvc, "l-svc", d.keys["p2"]), msgkit.NotFound)
	d.reject("offer-withdrawn-content", d.service, d.offerMsg("of-w2", "o-w2", "c4", "p1", sizeOne, far, offerExpiry), msgkit.Withdrawn)
	d.reject("renew-withdrawn-content", d.broadcaster, d.renewMsg(d.broadcaster, "rn-lw-2", "lw", "p1", "c4", sizeOne, 2, far+devnetTerm), msgkit.Withdrawn)
	d.commit("authority-end-lw", d.authority, d.authorityEndMsg("en-lw", "lw"))

	// Council disable of p2: new obligations are FORBIDDEN, renewal is
	// allowed (DR-08-01), authority end releases; c6 moves to p1.
	o6 := d.offerMsg("of-o6", "o6", "c6", "p2", sizeOne, far, offerExpiry)
	open2 := d.offerMsg("of-open2", "open2", "c5", "p2", sizeOne, far, offerExpiry)
	d.commitMulti("offers-p2", d.service, o6, open2)
	d.commit("accept-l6", d.provider2, d.acceptMsg(d.provider2, "ac-l6", o6, "l6", d.keys["p2"]))
	d.council("disable-p2", "prop-disable-p2", "disable-p2", disableChange("p2"))
	d.reject("offer-disabled-provider", d.service, d.offerMsg("of-dis", "o-dis", "c5", "p2", sizeOne, far, offerExpiry), msgkit.Forbidden)
	d.reject("accept-disabled-provider", d.broadcaster, d.acceptMsg(d.broadcaster, "ac-open2", open2, "l-open2", d.keys["p2"]), msgkit.Forbidden)
	d.commit("renew-on-disabled-provider", d.provider2, d.renewMsg(d.provider2, "rn-l6-2", "l6", "p2", "c6", sizeOne, 2, far+devnetTerm))
	repair := d.offerMsg("of-repair", "o-repair", "c6", "p1", sizeOne, far, offerExpiry)
	d.commit("repair-offer", d.service, repair)
	d.commit("repair-accept", d.broadcaster, d.acceptMsg(d.broadcaster, "ac-l6r", repair, "l6r", d.keys["p1"]))
	endHeight := d.commit("authority-end-l6", d.authority, d.authorityEndMsg("en-l6", "l6"))
	if got := d.reservedAt(d.clients[0], "p2", endHeight); got != 0 {
		t.Fatalf("p2 reserved %d after its last lease ended", got)
	}

	// Phase 9: queries on all four nodes and an in-process replay of every
	// block.
	fixed := d.height()
	if _, _, err := devnet.WaitEqual(rpcs, fixed+1, 30*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	queries := map[string]namedQuery{
		"active-p1":     {path: activePath, msg: &leasesv1.QueryActiveLeasesRequest{ProviderId: "p1"}},
		"active-c1":     {path: activePath, msg: &leasesv1.QueryActiveLeasesRequest{ContentId: "c1"}},
		"deadlines-all": {path: deadlinesPath, msg: &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: 253402300799}},
		"provider-p1":   {path: providerPath, msg: &providersv1.QueryProviderRequest{ProviderId: "p1"}},
		"provider-p2":   {path: providerPath, msg: &providersv1.QueryProviderRequest{ProviderId: "p2"}},
		"provider-p3":   {path: providerPath, msg: &providersv1.QueryProviderRequest{ProviderId: "p3"}},
	}
	for i, client := range d.clients {
		for name, q := range queries {
			raw := d.leaseQuery(client, q.path, q.msg, fixed)
			if i == 0 {
				q.want = raw
				queries[name] = q
				continue
			}
			if !bytes.Equal(q.want, raw) {
				t.Fatalf("query %s at %d differs on node %d", name, fixed, i+1)
			}
		}
	}
	held := d.heldOn(d.clients[0], "p1", fixed)
	if fmt.Sprint(held) != "[l6r la]" {
		t.Fatalf("held on p1 at %d: %v", fixed, held)
	}
	for _, name := range sortedKeys(queries) {
		fmt.Printf("phase=query height=%d name=%s nodes=4 equal=true bytes=%d sha256=%x\n", fixed, name, len(queries[name].want), sha256.Sum256(queries[name].want))
	}
	replayed := d.replay(layout.Genesis, fixed)
	for name, q := range queries {
		if !bytes.Equal(replayQuery(t, replayed, q.path, q.msg, fixed), q.want) {
			t.Fatalf("replayed query %s differs from the node", name)
		}
	}
	fmt.Printf("phase=replay-queries height=%d count=%d equal=true\n", fixed, len(queries))

	// Phase 10: every committed event.
	d.scanEvents(fixed)
}

// lastCommittedHeight is the highest committed application version in the
// stores of the stopped validators (rootmulti.GetLatestVersion of each
// application.db; svnd node.Start opens it under <home>/data). Every
// validator must be stopped.
func lastCommittedHeight(t *testing.T, layout devnet.Layout) int64 {
	t.Helper()
	var highest int64
	for _, v := range layout.Topology.Validators {
		db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(v.Home, "data"))
		if err != nil {
			t.Fatalf("open %s application store: %v", v.Name, err)
		}
		height := rootmulti.GetLatestVersion(db)
		if err := db.Close(); err != nil {
			t.Fatalf("close %s application store: %v", v.Name, err)
		}
		fmt.Printf("phase=halt-stored validator=%s committed_height=%d\n", v.Name, height)
		highest = max(highest, height)
	}
	return highest
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestDevnetLeaseHaltRestart (REQ-HOST-007): a lease's deadline is a
// real-time obligation in the signed terms. The provider signs a renewal
// but does not broadcast it (an offline note). All four validators stop
// before the deadline, so no block commits and expiry cannot run; they
// restart after the deadline has passed in real time. The first block
// whose header time is at or after the deadline expires the lease
// (effective), the block before it still holds it (effective-1), and the
// offline renewal submitted after the restart is CONFLICT: a halted chain
// does not turn the offline note into a renewal. The reservation is
// released once, and a replay of every block reproduces the header
// app_hash of all four nodes.
func TestDevnetLeaseHaltRestart(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	fmt.Println("label=single-machine four-process devnet (not independent operators)")
	layout, home, stops := startDevnet(t)
	rpcs := rpcList(layout)
	d := newDemo(t, layout, home)
	d.commit("register-p1", d.provider1, d.registerProvider(d.provider1, "reg-p1", "p1", capacityOne))
	d.council("admit-p1", "prop-admit-p1", "admit-p1", admitChange("p1"))
	d.commit("register-c1", d.service, d.registerContent(d.service, "reg-c1", "c1", 1, sizeOne))
	now := d.chainTime()
	deadline := now + 20
	offer := d.offerMsg("of-o1", "o1", "c1", "p1", sizeOne, deadline, now+15)
	d.commit("offer-o1", d.service, offer)
	acceptHeight := d.commit("accept-l1", d.broadcaster, d.acceptMsg(d.broadcaster, "ac-l1", offer, "l1", d.keys["p1"]))
	offline := d.renewMsg(d.broadcaster, "rn-l1-offline", "l1", "p1", "c1", sizeOne, 2, deadline+devnetTerm)
	fmt.Printf("phase=offline-renewal-signed lease=l1 deadline=%d broadcast=false\n", deadline)

	stopBegan := d.height()
	if d.blockTime(stopBegan) >= deadline || !contains(d.heldOn(d.clients[0], "p1", stopBegan), "l1") {
		t.Fatalf("at height %d, when the stops begin, l1 must be held before its deadline %d", stopBegan, deadline)
	}
	for i, stop := range stops {
		if err := stop(); err != nil {
			t.Fatal(tail(layout, err))
		}
		stops[i] = nil
	}
	// Blocks can still commit while the first validators stop (three of
	// four keep a quorum), so the last pre-halt height is measured after
	// all four stopped: the highest committed application version in the
	// validators' stores. Its header time is read after the restart.
	haltHeight := lastCommittedHeight(t, layout)
	if haltHeight < stopBegan {
		t.Fatalf("stored height %d is below the height %d seen before the stops", haltHeight, stopBegan)
	}
	fmt.Printf("phase=halt stopped=4 height_before_stop_began=%d last_committed_height=%d deadline=%d\n", stopBegan, haltHeight, deadline)
	for time.Now().Unix() <= deadline+5 {
		time.Sleep(500 * time.Millisecond)
	}
	for i := range stops {
		stop, err := launch(layout.Topology.Validators[i].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[i] = stop
	}
	if _, _, err := devnet.WaitEqual(rpcs, haltHeight+3, 90*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	haltTime := d.blockTime(haltHeight)
	if haltTime >= deadline || !contains(d.heldOn(d.clients[0], "p1", haltHeight), "l1") {
		t.Fatalf("at the last pre-halt height %d (header time %d) l1 must be held before its deadline %d", haltHeight, haltTime, deadline)
	}
	d.waitChainTime(deadline, 60*time.Second)
	effective := d.effectiveExpiry(acceptHeight, deadline, []expiryItem{{"lease", "l1"}})
	if effective <= haltHeight {
		t.Fatalf("l1 expired at %d, before the halt at %d", effective, haltHeight)
	}
	if !contains(d.heldOn(d.clients[0], "p1", effective-1), "l1") || contains(d.heldOn(d.clients[0], "p1", effective), "l1") {
		t.Fatalf("l1 at %d and %d", effective-1, effective)
	}
	if got := d.reservedAt(d.clients[0], "p1", effective-1) - d.reservedAt(d.clients[0], "p1", effective); got != sizeOne {
		t.Fatalf("expiry released %d", got)
	}
	fmt.Printf("phase=restart-expiry last_pre_halt_height=%d header_time=%d effective_minus_1=%d header_time=%d held=true effective=%d header_time=%d held=false released=%d\n",
		haltHeight, haltTime, effective-1, d.blockTime(effective-1), effective, d.blockTime(effective), sizeOne)
	d.reject("offline-renewal-after-restart", d.broadcaster, offline, msgkit.Conflict)
	if got := d.reservedAt(d.clients[0], "p1", 0); got != 0 {
		t.Fatalf("p1 reserved %d after the expiry", got)
	}
	fixed := d.height()
	if _, _, err := devnet.WaitEqual(rpcs, fixed+1, 30*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	d.replay(layout.Genesis, fixed)
	d.scanEvents(fixed)
}
