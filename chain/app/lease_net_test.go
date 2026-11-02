package app_test

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The lease tests run the real application (x/leases wired with the real
// x/providers, x/content, and x/authority keepers) and deliver signed
// transactions block by block. Provider acceptance, renewal, and end
// signatures are ed25519 signatures by the provider's registered signing
// key over the sign bytes x/leases exports. Nothing here is a mock.

// leaseTerm is the DEC-005 devnet test number newRegNet puts in genesis:
// the only offer and renewal term the chain accepts. It is a devnet test
// value, not a policy.
const leaseTerm = int64(3600)

const (
	domainAccept = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT
	domainRenew  = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW
	domainEnd    = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END
)

// now is the header time, in whole seconds, of the next block c commits.
func (c *cluster) now() int64 { return c.nodes[0].when.Unix() }

// setTime sets the header time of every node's next block.
func (c *cluster) setTime(when time.Time) {
	for _, node := range c.nodes {
		node.when = when
	}
}

// unixTime is a header time of whole unix seconds.
func unixTime(seconds int64) time.Time { return time.Unix(seconds, 0).UTC() }

// setUnix sets the header time of every node's next block to unix seconds.
func (c *cluster) setUnix(seconds int64) { c.setTime(unixTime(seconds)) }

// blockAt sets the header time of every node to when and commits one block
// with txs. Later blocks keep that time until it changes again.
func (c *cluster) blockAt(t *testing.T, when time.Time, txs ...[]byte) *abci.ResponseFinalizeBlock {
	t.Helper()
	c.setTime(when)
	return c.block(t, txs...)
}

// replayFresh boots a new application from doc and delivers the recorded
// block inputs of c (same height, header time, and transactions) up to
// height to (0: every block). After each block the replayed app hash and
// every result code and gas must equal what the cluster committed. It
// returns the replayed node.
func (c *cluster) replayFresh(t *testing.T, doc genesis.Document, to int64) *running {
	t.Helper()
	fresh := bootWith(t, doc, nil)
	for i, in := range c.inputs {
		if to != 0 && in.height > to {
			break
		}
		res, err := fresh.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: in.height,
			Time:   in.time,
			Hash:   bytes32(byte(in.height)),
			Txs:    in.txs,
		})
		if err != nil {
			t.Fatalf("replay height %d: %v", in.height, err)
		}
		if _, err := fresh.app.Commit(); err != nil {
			t.Fatal(err)
		}
		if got := fresh.app.LastCommitID().Hash; !bytes.Equal(got, c.hashes[i]) {
			t.Fatalf("replay height %d: app hash %X, cluster %X", in.height, got, c.hashes[i])
		}
		if len(res.TxResults) != len(c.blocks[i].TxResults) {
			t.Fatalf("replay height %d: %d results, cluster %d", in.height, len(res.TxResults), len(c.blocks[i].TxResults))
		}
		for j, r := range res.TxResults {
			want := c.blocks[i].TxResults[j]
			if r.Code != want.Code || r.Codespace != want.Codespace || r.GasUsed != want.GasUsed {
				t.Fatalf("replay height %d tx %d: %s/%d gas %d, cluster %s/%d gas %d", in.height, j, r.Codespace, r.Code, r.GasUsed, want.Codespace, want.Code, want.GasUsed)
			}
		}
		fresh.height = in.height
		fresh.when = in.time
		fresh.last = res
	}
	return fresh
}

// contentManifest is the digest of the synthetic package manifest the
// registry tests register for contentID.
func contentManifest(contentID string) *typesv1.Digest {
	return digestOf(sha(packageManifest(contentID)))
}

// termsDigest is the digest of a synthetic off-chain terms document.
func termsDigest(offerID string) *typesv1.Digest {
	return digestOf(sha([]byte("svn-lease-terms:" + offerID)))
}

// offerMsg builds an OfferLease for content version 1 with a DEC-005 term
// ending at deadline.
func (n *regNet) offerMsg(who *actor, bid, offerID, contentID, providerID string, size uint64, deadline, expiry int64) *leasesv1.MsgOfferLease {
	return &leasesv1.MsgOfferLease{
		Signer:              who.addr,
		BusinessId:          bid,
		OfferId:             offerID,
		ContentId:           contentID,
		ContentVersion:      1,
		ProviderId:          providerID,
		ManifestDigest:      contentManifest(contentID),
		PackageBytes:        size,
		TermsDigest:         termsDigest(offerID),
		StartUnixSeconds:    deadline - leaseTerm,
		DeadlineUnixSeconds: deadline,
		ExpiryUnixSeconds:   expiry,
		TermVersion:         1,
	}
}

// storedOffer is the Offer record x/leases stores for msg: the message
// terms and the outer signer as scheduler.
func storedOffer(msg *leasesv1.MsgOfferLease) *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId:             msg.GetOfferId(),
		ContentId:           msg.GetContentId(),
		ContentVersion:      msg.GetContentVersion(),
		ProviderId:          msg.GetProviderId(),
		ManifestDigest:      msg.GetManifestDigest(),
		PackageBytes:        msg.GetPackageBytes(),
		TermsDigest:         msg.GetTermsDigest(),
		StartUnixSeconds:    msg.GetStartUnixSeconds(),
		DeadlineUnixSeconds: msg.GetDeadlineUnixSeconds(),
		ExpiryUnixSeconds:   msg.GetExpiryUnixSeconds(),
		TermVersion:         msg.GetTermVersion(),
		Scheduler:           msg.GetSigner(),
	}
}

// acceptanceBy signs the acceptance of offer as leaseID with key.
func acceptanceBy(t testing.TB, offer *leasesv1.MsgOfferLease, leaseID string, key signingKey) *typesv1.InnerSignature {
	t.Helper()
	signBytes, err := leases.AcceptSignBytes(storedOffer(offer), leaseID)
	if err != nil {
		t.Fatal(err)
	}
	return innerSig(domainAccept, key, signBytes)
}

// acceptMsg relays the provider's acceptance of offer, signed with the
// provider's current registered key, from who.
func (n *regNet) acceptMsg(t testing.TB, who *actor, bid string, offer *leasesv1.MsgOfferLease, leaseID string) *leasesv1.MsgAcceptLease {
	t.Helper()
	return &leasesv1.MsgAcceptLease{
		Signer:             who.addr,
		BusinessId:         bid,
		OfferId:            offer.GetOfferId(),
		LeaseId:            leaseID,
		ProviderAcceptance: acceptanceBy(t, offer, leaseID, n.signing[offer.GetProviderId()]),
	}
}

// renewMsg builds a renewal of leaseID to termVersion and deadline, signed
// with the provider's current key.
func (n *regNet) renewMsg(t testing.TB, who *actor, bid, leaseID, providerID, contentID string, size, termVersion uint64, deadline int64) *leasesv1.MsgRenewLease {
	t.Helper()
	manifest := contentManifest(contentID)
	signBytes, err := leases.RenewSignBytes(leaseID, termVersion, deadline, manifest, size, "")
	if err != nil {
		t.Fatal(err)
	}
	return &leasesv1.MsgRenewLease{
		Signer:              who.addr,
		BusinessId:          bid,
		LeaseId:             leaseID,
		TermVersion:         termVersion,
		DeadlineUnixSeconds: deadline,
		ManifestDigest:      manifest,
		PackageBytes:        size,
		ProviderSignature:   innerSig(domainRenew, n.signing[providerID], signBytes),
	}
}

// endMsg builds a provider-signed EndLease relayed by who.
func (n *regNet) endMsg(t testing.TB, who *actor, bid, leaseID, providerID string) *leasesv1.MsgEndLease {
	t.Helper()
	signBytes, err := leases.EndSignBytes(leaseID, providerID)
	if err != nil {
		t.Fatal(err)
	}
	return &leasesv1.MsgEndLease{
		Signer:            who.addr,
		BusinessId:        bid,
		LeaseId:           leaseID,
		ProviderSignature: innerSig(domainEnd, n.signing[providerID], signBytes),
	}
}

// authorityEndMsg is an EndLease with authority_end from the AUTHORITY
// account.
func (n *regNet) authorityEndMsg(bid, leaseID string) *leasesv1.MsgEndLease {
	return &leasesv1.MsgEndLease{Signer: n.authority.addr, BusinessId: bid, LeaseId: leaseID, AuthorityEnd: true}
}

// registerAdmit registers providerID from who and runs the council admit.
func (n *regNet) registerAdmit(t *testing.T, c *cluster, who *actor, providerID string, capacity uint64) {
	t.Helper()
	wantCode(t, "register "+providerID, n.send(t, c, who, n.registerProvider(t, who, "reg-"+providerID, providerID, groupDevnet, capacity)), msgkit.OK)
	n.council(t, c, "prop-admit-"+providerID, "admit-"+providerID, admitPayload(providerID))
}

// signedMulti signs one transaction from who carrying msgs, at who's next
// sequence, with the given gas limit.
func (n *regNet) signedMulti(t *testing.T, c *cluster, who *actor, gas uint64, msgs []sdk.Msg) []byte {
	t.Helper()
	builder := c.app().EncodingConfig().TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		t.Fatal(err)
	}
	return n.signBuilt(t, c, who, gas, builder)
}

// signBuilt signs builder (its messages already set) from who at who's
// next sequence, SIGN_MODE_DIRECT, with the given gas limit.
func (n *regNet) signBuilt(t *testing.T, c *cluster, who *actor, gas uint64, builder client.TxBuilder) []byte {
	t.Helper()
	cfg := c.app().EncodingConfig().TxConfig
	sequence := n.seq[who.hexAddr()]
	builder.SetGasLimit(gas)
	signer := authsigning.SignerData{
		Address: sdk.AccAddress(who.addr).String(), ChainID: n.doc.ChainID,
		AccountNumber: n.numbers[who.hexAddr()], Sequence: sequence, PubKey: who.key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{
		PubKey: who.key.PubKey(), Sequence: sequence,
		Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		t.Fatal(err)
	}
	signature, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, who.key, cfg, sequence)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.SetSignatures(signature); err != nil {
		t.Fatal(err)
	}
	raw, err := cfg.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// sendMulti delivers one multi-message transaction in its own block and
// requires code 0.
func (n *regNet) sendMulti(t *testing.T, c *cluster, who *actor, label string, msgs []sdk.Msg) *abci.ExecTxResult {
	t.Helper()
	res := n.deliverRaw(t, c, who, n.signedMulti(t, c, who, genesis.MaxGasPerTx, msgs))
	wantCode(t, label, res, msgkit.OK)
	return res
}

// queryActive runs svn.leases.v1.Query/ActiveLeases at height (0 = latest)
// and requires code 0.
func queryActive(t *testing.T, chainApp *app.App, contentID, providerID string, limit uint32, key []byte, height int64) (*leasesv1.QueryActiveLeasesResponse, []byte) {
	t.Helper()
	raw, res := query(t, chainApp, "/svn.leases.v1.Query/ActiveLeases", &leasesv1.QueryActiveLeasesRequest{
		ContentId: contentID, ProviderId: providerID, Page: &typesv1.PageRequest{Limit: limit, PageKey: key},
	}, height)
	if res.Code != 0 {
		t.Fatalf("ActiveLeases %q/%q at %d: %s/%d %s", contentID, providerID, height, res.Codespace, res.Code, res.Log)
	}
	var out leasesv1.QueryActiveLeasesResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return &out, raw
}

// activePages walks ActiveLeases with limit and returns every page's bytes
// and the leases in order.
func activePages(t *testing.T, chainApp *app.App, contentID, providerID string, limit uint32, height int64) ([][]byte, []*leasesv1.Lease) {
	t.Helper()
	var pages [][]byte
	var all []*leasesv1.Lease
	var key []byte
	for i := 0; i < 10000; i++ {
		page, raw := queryActive(t, chainApp, contentID, providerID, limit, key, height)
		if uint32(len(page.GetLeases())) > limit {
			t.Fatalf("ActiveLeases page %d has %d leases above limit %d", i, len(page.GetLeases()), limit)
		}
		pages = append(pages, raw)
		all = append(all, page.GetLeases()...)
		key = page.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return pages, all
		}
	}
	t.Fatal("ActiveLeases pagination did not end")
	return nil, nil
}

// deadlinePages walks Deadlines with limit and returns every page's bytes,
// the held leases, and the open offers in order.
func deadlinePages(t *testing.T, chainApp *app.App, notAfter int64, limit uint32, height int64) ([][]byte, []*leasesv1.Lease, []*leasesv1.Offer) {
	t.Helper()
	var pages [][]byte
	var held []*leasesv1.Lease
	var open []*leasesv1.Offer
	var key []byte
	for i := 0; i < 10000; i++ {
		raw, res := query(t, chainApp, "/svn.leases.v1.Query/Deadlines", &leasesv1.QueryDeadlinesRequest{
			NotAfterUnixSeconds: notAfter, Page: &typesv1.PageRequest{Limit: limit, PageKey: key},
		}, height)
		if res.Code != 0 {
			t.Fatalf("Deadlines at %d: %s/%d %s", height, res.Codespace, res.Code, res.Log)
		}
		var page leasesv1.QueryDeadlinesResponse
		if err := proto.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if uint32(len(page.GetLeases())+len(page.GetOffers())) > limit {
			t.Fatalf("Deadlines page %d holds %d items above limit %d", i, len(page.GetLeases())+len(page.GetOffers()), limit)
		}
		pages = append(pages, raw)
		held = append(held, page.GetLeases()...)
		open = append(open, page.GetOffers()...)
		key = page.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return pages, held, open
		}
	}
	t.Fatal("Deadlines pagination did not end")
	return nil, nil, nil
}

// heldLease returns the held lease leaseID of providerID, or nil.
func heldLease(t *testing.T, chainApp *app.App, providerID, leaseID string, height int64) *leasesv1.Lease {
	t.Helper()
	_, all := activePages(t, chainApp, "", providerID, 100, height)
	for _, lease := range all {
		if lease.GetLeaseId() == leaseID {
			return lease
		}
	}
	return nil
}

// reserved is the committed reserved_bytes of providerID at height.
func reserved(t *testing.T, chainApp *app.App, providerID string, height int64) uint64 {
	t.Helper()
	record, _ := queryProvider(t, chainApp, providerID, height)
	return record.GetReservedBytes()
}

// expiryItem is one item BeginBlock expiry closed: a lease (LeaseEnded
// with state EXPIRED) or an offer (leases_offer_closed reason EXPIRED).
type expiryItem struct {
	kind string // "lease" or "offer"
	id   string
}

func (e expiryItem) String() string { return e.kind + ":" + e.id }

// beginBlockExpiry parses the BeginBlock events of one finalized block
// (attributes end with mode=BeginBlock) and returns the expiry items in
// event order, plus the released bytes of the expired leases. Every
// BeginBlock event must be one of the two expiry events; any other
// BeginBlock event fails the test.
func beginBlockExpiry(t *testing.T, res *abci.ResponseFinalizeBlock) ([]expiryItem, uint64) {
	t.Helper()
	var items []expiryItem
	var released uint64
	for _, ev := range res.Events {
		n := len(ev.Attributes)
		if n == 0 || ev.Attributes[n-1].Key != msgkit.ModeKey {
			continue
		}
		mode := ev.Attributes[n-1].Value
		if mode != "BeginBlock" {
			t.Fatalf("event %s in mode %s", ev.Type, mode)
		}
		switch ev.Type {
		case leases.EventOfferClosed:
			if n != 3 || ev.Attributes[0].Key != leases.AttrOfferID || ev.Attributes[1].Key != leases.AttrReason || ev.Attributes[1].Value != leases.ReasonExpired {
				t.Fatalf("BeginBlock %s attributes %v", ev.Type, ev.Attributes)
			}
			items = append(items, expiryItem{kind: "offer", id: ev.Attributes[0].Value})
		default:
			msg, parsedMode, err := msgkit.ParseBlockEvent(ev)
			if err != nil {
				t.Fatalf("BeginBlock event %s: %v", ev.Type, err)
			}
			ended, ok := msg.(*eventsv1.LeaseEnded)
			if !ok || parsedMode != "BeginBlock" || ended.GetState() != "EXPIRED" {
				t.Fatalf("BeginBlock event %s (%s): %v", ev.Type, parsedMode, msg)
			}
			bytesOut, err := strconv.ParseUint(ended.GetReleasedBytes(), 10, 64)
			if err != nil {
				t.Fatalf("released_bytes %q: %v", ended.GetReleasedBytes(), err)
			}
			released += bytesOut
			items = append(items, expiryItem{kind: "lease", id: ended.GetLeaseId()})
		}
	}
	return items, released
}

// txEventTypes returns the event types of one tx result that x/leases
// emits: typed svn.events.v1 lease events, leases_offer_closed, and
// leases_result.
func txEventTypes(res *abci.ExecTxResult) []string {
	var out []string
	for _, ev := range res.Events {
		switch {
		case ev.Type == leases.EventOfferClosed || ev.Type == "leases_result":
			out = append(out, ev.Type)
		case bytes.HasPrefix([]byte(ev.Type), []byte("svn.events.v1.Lease")):
			out = append(out, ev.Type)
		}
	}
	return out
}

// wantTxEvents asserts the lease events of one result, in order.
func wantTxEvents(t *testing.T, label string, res *abci.ExecTxResult, want ...string) {
	t.Helper()
	got := txEventTypes(res)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s: lease events %v, want %v", label, got, want)
	}
}

const (
	evOffered  = "svn.events.v1.LeaseOffered"
	evAccepted = "svn.events.v1.LeaseAccepted"
	evRenewed  = "svn.events.v1.LeaseRenewed"
	evEnded    = "svn.events.v1.LeaseEnded"
	evClosed   = leases.EventOfferClosed
	evResult   = "leases_result"
)

// checkInvariants runs the lease invariants on every node of c.
func (c *cluster) checkInvariants(t *testing.T, label string) {
	t.Helper()
	for i, node := range c.nodes {
		if err := node.app.CheckLeaseInvariantsForTest(); err != nil {
			t.Fatalf("%s: node %d: %v", label, i, err)
		}
	}
}
