package app_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// leaseBasics commits p1 and p2 (ADMITTED) and contents c1, c2, c3
// (sizeSmall each).
func leaseBasics(t *testing.T, n *regNet, c *cluster) {
	t.Helper()
	n.registerAdmit(t, c, n.provider1, "p1", capacity1)
	n.registerAdmit(t, c, n.provider2, "p2", capacity1)
	for _, id := range []string{"c1", "c2", "c3"} {
		wantCode(t, "register "+id, n.send(t, c, n.service, n.registerContent(t, n.service, "reg-"+id, id, 1, packageManifest(id), sizeSmall)), msgkit.OK)
	}
}

// TestLeaseMessagesDecode: the four lease messages, with their Digest and
// InnerSignature fields, decode from signed transactions and pass CheckTx
// (which runs only the ante chain). An unknown field, critical or not, at
// the message top level or inside a Digest or an InnerSignature fails
// decoding, and so does a message type the protocol does not define (no
// reachability or AT_RISK message exists: REQ-BOUND-004). State is
// byte-identical after each decode failure.
func TestLeaseMessagesDecode(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	leaseBasics(t, n, c)
	chainApp := c.app()
	now := c.now()
	offer := n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeSmall, now+1800, now+600)
	accept := n.acceptMsg(t, n.broadcaster, "ac-l1", offer, "l1")
	renew := n.renewMsg(t, n.broadcaster, "rn-l1", "l1", "p1", "c1", sizeSmall, 2, now+1800+leaseTerm)
	end := n.endMsg(t, n.broadcaster, "en-l1", "l1", "p1")
	msgs := []struct {
		who *actor
		msg sdk.Msg
	}{{n.service, offer}, {n.broadcaster, accept}, {n.broadcaster, renew}, {n.broadcaster, end}}
	decode := chainApp.EncodingConfig().TxConfig.TxDecoder()
	checkSeq := map[string]uint64{}
	for _, tc := range msgs {
		seq := n.seq[tc.who.hexAddr()] + checkSeq[tc.who.hexAddr()]
		raw := signFull(t, chainApp, n.doc.ChainID, tc.who.key, n.numbers[tc.who.hexAddr()], seq, registryGas, nil, tc.msg)
		decoded, err := decode(raw)
		if err != nil {
			t.Fatalf("%T: %v", tc.msg, err)
		}
		got, ok := decoded.GetMsgs()[0].(proto.Message)
		if !ok || !proto.Equal(got, tc.msg.(proto.Message)) {
			t.Fatalf("%T: decoded message differs", tc.msg)
		}
		checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatal(err)
		}
		if checked.Code != 0 {
			t.Fatalf("%T: check tx %s/%d %s", tc.msg, checked.Codespace, checked.Code, checked.Log)
		}
		checkSeq[tc.who.hexAddr()]++
	}
	// In blocks the four messages reach their handlers and commit.
	for _, tc := range msgs {
		wantCode(t, string(proto.MessageName(tc.msg.(proto.Message))), n.send(t, c, tc.who, tc.msg), msgkit.OK)
	}

	unknown := []struct {
		name  string
		who   *actor
		build func() (sdk.Msg, proto.Message)
		num   protowire.Number
	}{
		{"OfferLease top level, field 99", n.service, func() (sdk.Msg, proto.Message) {
			m := n.offerMsg(n.service, "of-u1", "o-u1", "c2", "p1", sizeSmall, now+1800, now+600)
			return m, m
		}, 99},
		{"OfferLease top level, field 1024", n.service, func() (sdk.Msg, proto.Message) {
			m := n.offerMsg(n.service, "of-u2", "o-u2", "c2", "p1", sizeSmall, now+1800, now+600)
			return m, m
		}, 1024},
		{"OfferLease terms_digest, field 2047", n.service, func() (sdk.Msg, proto.Message) {
			m := n.offerMsg(n.service, "of-u3", "o-u3", "c2", "p1", sizeSmall, now+1800, now+600)
			return m, m.TermsDigest
		}, 2047},
		{"AcceptLease provider_acceptance, field 99", n.broadcaster, func() (sdk.Msg, proto.Message) {
			m := n.acceptMsg(t, n.broadcaster, "ac-u4", offer, "l-u4")
			return m, m.ProviderAcceptance
		}, 99},
		{"AcceptLease provider_acceptance, field 1024", n.broadcaster, func() (sdk.Msg, proto.Message) {
			m := n.acceptMsg(t, n.broadcaster, "ac-u5", offer, "l-u5")
			return m, m.ProviderAcceptance
		}, 1024},
		{"RenewLease manifest_digest, field 1024", n.broadcaster, func() (sdk.Msg, proto.Message) {
			m := n.renewMsg(t, n.broadcaster, "rn-u6", "l1", "p1", "c1", sizeSmall, 3, now+1800+2*leaseTerm)
			return m, m.ManifestDigest
		}, 1024},
		{"RenewLease provider_signature, field 2047", n.broadcaster, func() (sdk.Msg, proto.Message) {
			m := n.renewMsg(t, n.broadcaster, "rn-u7", "l1", "p1", "c1", sizeSmall, 3, now+1800+2*leaseTerm)
			return m, m.ProviderSignature
		}, 2047},
		{"EndLease top level, field 2047", n.authority, func() (sdk.Msg, proto.Message) {
			m := n.authorityEndMsg("en-u8", "l1")
			return m, m
		}, 2047},
		{"EndLease provider_signature, field 99", n.broadcaster, func() (sdk.Msg, proto.Message) {
			m := n.endMsg(t, n.broadcaster, "en-u9", "l1", "p1")
			return m, m.ProviderSignature
		}, 99},
	}
	payload := []byte("canary-unknown-field-7f3a")
	for _, tc := range unknown {
		msg, where := tc.build()
		withUnknown(where, tc.num, payload)
		raw := n.signEnvelope(t, tc.who, msg, nil, nil)
		if !bytes.Contains(raw, payload) {
			t.Fatalf("%s: the unknown field is not in the signed bytes", tc.name)
		}
		before := chainApp.FullSnapshot()
		res := n.deliverRaw(t, c, nil, raw)
		wantDecodeError(t, tc.name, res.Codespace, res.Code)
		unchanged(t, tc.name, chainApp, before, res)
	}
	for _, typeURL := range []string{"/svn.leases.v1.MsgMarkAtRisk", "/svn.leases.v1.MsgCancelOffer", "/svn.leases.v1.MsgReportReachability"} {
		before := chainApp.FullSnapshot()
		res := n.deliverRaw(t, c, nil, directAdminTx(t, typeURL, &leasesv1.MsgEndLease{Signer: n.broadcaster.addr}))
		wantDecodeError(t, typeURL, res.Codespace, res.Code)
		unchanged(t, typeURL, chainApp, before, res)
	}
}

// txHash is the CometBFT transaction hash (SHA-256 of the bytes).
func txHash(raw []byte) [32]byte { return sha256.Sum256(raw) }

// TestLeaseRetryScenarios (REQ-INV-003, lease part): a client that times
// out and resubmits the same business id and payload in a new transaction
// (a distinct tx hash) gets one effect. Two relays (the broadcaster and the
// provider's own account) submit the same acceptance in one block, in both
// orders, and one account resubmits in a later block: exactly one
// LeaseAccepted, the other result is code 0 with leases_result
// idempotent=true and no write, and the reservation is taken once. The
// same holds for renewal and end. A changed payload under the business id
// is REPLAY, and a lease message reusing a business id of the content,
// providers, or authority module is REPLAY.
func TestLeaseRetryScenarios(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	leaseBasics(t, n, c)
	chainApp := c.app()
	now := c.now()
	deadline, expiry := now+1800, now+600

	pair := func(label string, first, second *actor, offer *leasesv1.MsgOfferLease, leaseID string) {
		t.Helper()
		wantCode(t, "offer for "+label, n.send(t, c, n.service, offer), msgkit.OK)
		before := reserved(t, chainApp, "p1", 0)
		bid := "ac-" + leaseID
		rawA := n.signed(t, c, first, registryGas, nil, n.acceptMsg(t, first, bid, offer, leaseID))
		rawB := n.signed(t, c, second, registryGas, nil, n.acceptMsg(t, second, bid, offer, leaseID))
		if txHash(rawA) == txHash(rawB) {
			t.Fatalf("%s: the two submissions share a tx hash", label)
		}
		res := c.block(t, rawA, rawB)
		n.seq[first.hexAddr()]++
		n.seq[second.hexAddr()]++
		wantCode(t, label+" first", res.TxResults[0], msgkit.OK)
		wantCode(t, label+" second", res.TxResults[1], msgkit.OK)
		wantTxEvents(t, label+" first", res.TxResults[0], evAccepted)
		wantTxEvents(t, label+" second", res.TxResults[1], evResult)
		if got := reserved(t, chainApp, "p1", 0) - before; got != sizeSmall {
			t.Fatalf("%s: reserved grew by %d, want %d once", label, got, sizeSmall)
		}
		if heldLease(t, chainApp, "p1", leaseID, 0) == nil {
			t.Fatalf("%s: lease %s is not held", label, leaseID)
		}
	}
	pair("broadcaster then provider, one block", n.broadcaster, n.provider1, n.offerMsg(n.service, "of-oa", "oa", "c1", "p1", sizeSmall, deadline, expiry), "la")
	pair("provider then broadcaster, one block", n.provider1, n.broadcaster, n.offerMsg(n.service, "of-ob", "ob", "c2", "p1", sizeSmall, deadline, expiry), "lb")

	// One account resubmits in a later block (next sequence, new tx hash).
	oc := n.offerMsg(n.service, "of-oc", "oc", "c3", "p1", sizeSmall, deadline, expiry)
	wantCode(t, "offer oc", n.send(t, c, n.service, oc), msgkit.OK)
	accept := n.acceptMsg(t, n.broadcaster, "ac-lc", oc, "lc")
	first := n.signed(t, c, n.broadcaster, registryGas, nil, accept)
	res := n.deliverRaw(t, c, n.broadcaster, first)
	wantCode(t, "accept lc", res, msgkit.OK)
	wantTxEvents(t, "accept lc", res, evAccepted)
	retry := func(label string, who *actor, msg sdk.Msg) {
		t.Helper()
		raw := n.signed(t, c, who, registryGas, nil, msg)
		before := chainApp.FullSnapshot()
		res := n.deliverRaw(t, c, who, raw)
		wantCode(t, label, res, msgkit.OK)
		wantTxEvents(t, label, res, evResult)
		if !bytes.Equal(before, chainApp.FullSnapshot()) {
			t.Fatalf("%s: the idempotent retry changed the full state", label)
		}
	}
	retry("accept lc resubmitted in a later block", n.broadcaster, accept)
	if raw := n.signed(t, c, n.broadcaster, registryGas, nil, accept); txHash(raw) == txHash(first) {
		t.Fatal("the resubmission reused the tx hash")
	}

	reject := func(label string, who *actor, msg sdk.Msg, want msgkit.Code) {
		t.Helper()
		before := chainApp.FullSnapshot()
		res := n.send(t, c, who, msg)
		wantCode(t, label, res, want)
		unchanged(t, label, chainApp, before, res)
	}
	reject("accept business id with another lease id", n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-lc", oc, "lc2"), msgkit.Replay)
	reject("lease message with a content business id", n.broadcaster, n.acceptMsg(t, n.broadcaster, "reg-c1", oc, "lc3"), msgkit.Replay)
	reject("lease message with a provider business id", n.service, n.offerMsg(n.service, "reg-p1", "o-x", "c3", "p2", sizeSmall, deadline, expiry), msgkit.Replay)
	reject("lease message with an authority business id", n.broadcaster, n.renewMsg(t, n.broadcaster, "pb-prop-admit-p1", "la", "p1", "c1", sizeSmall, 2, deadline+leaseTerm), msgkit.Replay)

	// Renewal and end retries.
	renew := n.renewMsg(t, n.broadcaster, "rn-la-2", "la", "p1", "c1", sizeSmall, 2, deadline+leaseTerm)
	wantCode(t, "renew la", n.send(t, c, n.broadcaster, renew), msgkit.OK)
	retry("renew la resubmitted", n.broadcaster, renew)
	end := n.endMsg(t, n.provider1, "en-lb", "lb", "p1")
	before := reserved(t, chainApp, "p1", 0)
	wantCode(t, "end lb", n.send(t, c, n.provider1, end), msgkit.OK)
	retry("end lb resubmitted by the broadcaster", n.broadcaster, n.endMsg(t, n.broadcaster, "en-lb", "lb", "p1"))
	if got := before - reserved(t, chainApp, "p1", 0); got != sizeSmall {
		t.Fatalf("end lb released %d, want %d once", got, sizeSmall)
	}
	reject("end lb with a new business id", n.provider1, n.endMsg(t, n.provider1, "en-lb-2", "lb", "p1"), msgkit.Underflow)
	c.checkInvariants(t, "after the retries")
}

// TestServiceKeyCannotSignAcceptanceInApp (REQ-CHAIN-012): through
// FinalizeBlock, the service's ed25519 key (the hosting-intent key the
// service signs content registrations with) cannot produce a provider
// acceptance, under the provider's key id or its own; the service account
// cannot relay the provider's valid acceptance; a provider signature in
// the renew domain and one by the provider's retired key are refused. A
// broadcaster relaying the provider's current-key acceptance succeeds.
func TestServiceKeyCannotSignAcceptanceInApp(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	leaseBasics(t, n, c)
	chainApp := c.app()
	now := c.now()
	offer := n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeSmall, now+1800, now+600)
	wantCode(t, "offer o1", n.send(t, c, n.service, offer), msgkit.OK)
	retired := n.signing["p1"]
	next := newSigningKey("p1-key-2")
	wantCode(t, "rotate p1", n.send(t, c, n.provider1, n.rotate(t, n.provider1, "rot-p1", "p1", 1, next)), msgkit.OK)
	n.signing["p1"] = next

	with := func(bid string, sig *typesv1.InnerSignature) *leasesv1.MsgAcceptLease {
		m := n.acceptMsg(t, n.broadcaster, bid, offer, "l1")
		m.ProviderAcceptance = sig
		return m
	}
	renewDomain := func(key signingKey) *typesv1.InnerSignature {
		sig := acceptanceBy(t, offer, "l1", key)
		sig.Domain = domainRenew
		return sig
	}
	for _, tc := range []struct {
		name string
		who  *actor
		msg  sdk.Msg
		want msgkit.Code
	}{
		{"service key under the provider's key id", n.broadcaster, with("x-1", acceptanceBy(t, offer, "l1", signingKey{id: next.id, priv: n.intent.priv})), msgkit.Unauthorized},
		{"service key under its own key id", n.broadcaster, with("x-2", acceptanceBy(t, offer, "l1", n.intent)), msgkit.Unauthorized},
		{"service account relays the provider's acceptance", n.service, n.acceptMsg(t, n.service, "x-3", offer, "l1"), msgkit.Forbidden},
		{"provider signature in the renew domain", n.broadcaster, with("x-4", renewDomain(next)), msgkit.WrongDomain},
		{"provider's retired key", n.broadcaster, with("x-5", acceptanceBy(t, offer, "l1", retired)), msgkit.Unauthorized},
	} {
		before := chainApp.FullSnapshot()
		res := n.send(t, c, tc.who, tc.msg)
		wantCode(t, tc.name, res, tc.want)
		unchanged(t, tc.name, chainApp, before, res)
	}
	res := n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l1", offer, "l1"))
	wantCode(t, "broadcaster relays the provider's acceptance", res, msgkit.OK)
	wantTxEvents(t, "broadcaster relays the provider's acceptance", res, evAccepted)
}

// TestDevnetTermDecisionBlocked: DEC-005 leaves lease durations unset. On
// a devnet genesis without the DEC-005 test parameter every OfferLease is
// DECISION_BLOCKED (through FinalizeBlock, state unchanged). The devnet
// number is read only under a shortvideo-dev- chain id: the app's own
// registered OfferLease handler, run on the committed state of a devnet
// with DEC-005 = 3600 but with a non-devnet chain id in the context, is
// DECISION_BLOCKED, while the same message with the devnet chain id
// succeeds. A non-devnet genesis cannot carry devnet test parameters at
// all.
func TestDevnetTermDecisionBlocked(t *testing.T) {
	t.Run("devnet without DEC-005", func(t *testing.T) {
		n := newRegNet("DEC-005")
		c := n.start(t, 1)
		leaseBasics(t, n, c)
		if _, found := c.app().DevnetNumericForTest(n.doc.ChainID, "DEC-005"); found {
			t.Fatal("DEC-005 found without the devnet parameter")
		}
		now := c.now()
		before := c.app().FullSnapshot()
		res := n.send(t, c, n.service, n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeSmall, now+1800, now+600))
		wantCode(t, "offer without DEC-005", res, msgkit.DecisionBlocked)
		unchanged(t, "offer without DEC-005", c.app(), before, res)
	})
	t.Run("non-devnet chain id", func(t *testing.T) {
		n := newRegNet()
		c := n.start(t, 1)
		leaseBasics(t, n, c)
		chainApp := c.app()
		if value, found := chainApp.DevnetNumericForTest(n.doc.ChainID, "DEC-005"); !found || value != uint64(leaseTerm) {
			t.Fatalf("devnet DEC-005: %d %t", value, found)
		}
		const beta = "shortvideo-beta-1"
		if _, found := chainApp.DevnetNumericForTest(beta, "DEC-005"); found {
			t.Fatal("DEC-005 found under a non-devnet chain id")
		}
		now := c.now()
		msg := n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeSmall, now+1800, now+600)
		handler := chainApp.MsgServiceRouter().Handler(msg)
		if handler == nil {
			t.Fatal("no route for OfferLease")
		}
		run := func(chainID string) error {
			ctx := chainApp.NewUncachedContext(false, cmtproto.Header{ChainID: chainID, Height: c.height() + 1, Time: c.nodes[0].when})
			cache, _ := ctx.CacheContext()
			cache = cache.WithGasMeter(storetypes.NewGasMeter(registryGas)).WithEventManager(sdk.NewEventManager())
			_, err := handler(cache, msg)
			return err
		}
		codespace, code, _ := errorsmod.ABCIInfo(run(beta), false)
		if codespace != svnerr.ModuleName || code != uint32(typesv1.ErrorCode_ERROR_CODE_DECISION_BLOCKED) {
			t.Fatalf("OfferLease under %s: %s/%d", beta, codespace, code)
		}
		if err := run(n.doc.ChainID); err != nil {
			t.Fatalf("OfferLease under the devnet chain id: %v", err)
		}
		doc := n.doc
		doc.ChainID = beta
		if err := genesis.Validate(doc, genesis.CometIdentity{}); err == nil {
			t.Fatal("a non-devnet genesis accepted devnet test parameters")
		}
	})
}

// TestLeaseQueryGasAndBounds: ActiveLeases and Deadlines through the ABCI
// query path and the query router. Pages walk with inclusive keys and
// stop at the limit; a limit above the cap, an oversized or unknown page
// key, a missing filter, and an out-of-range not_after are rejected with
// svn codes; each query runs on the node's finite query gas meter, and a
// node with a low query gas limit fails the same query.
func TestLeaseQueryGasAndBounds(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	leaseBasics(t, n, c)
	chainApp := c.app()
	now := c.now()
	for i, id := range []string{"c1", "c2", "c3"} {
		offer := n.offerMsg(n.service, "of-"+id, "o-"+id, id, "p1", sizeSmall, now+1800+int64(i), now+600)
		wantCode(t, "offer "+id, n.send(t, c, n.service, offer), msgkit.OK)
		wantCode(t, "accept "+id, n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-"+id, offer, "l-"+id)), msgkit.OK)
		wantCode(t, "offer p2 "+id, n.send(t, c, n.service, n.offerMsg(n.service, "of2-"+id, "o2-"+id, id, "p2", sizeSmall, now+1800, now+600+int64(i))), msgkit.OK)
	}
	pages, held := activePages(t, chainApp, "", "p1", 2, 0)
	if len(pages) != 2 || len(held) != 3 {
		t.Fatalf("ActiveLeases(p1) limit 2: %d pages, %d leases", len(pages), len(held))
	}
	pages, held, open := deadlinePages(t, chainApp, maxUnix, 2, 0)
	if len(pages) != 3 || len(held) != 3 || len(open) != 3 {
		t.Fatalf("Deadlines limit 2: %d pages, %d leases, %d offers", len(pages), len(held), len(open))
	}
	// The phase boundary falls inside page 2: one lease, then one offer.
	var page2 leasesv1.QueryDeadlinesResponse
	if err := proto.Unmarshal(pages[1], &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.GetLeases()) != 1 || len(page2.GetOffers()) != 1 {
		t.Fatalf("Deadlines page 2: %d leases, %d offers", len(page2.GetLeases()), len(page2.GetOffers()))
	}
	if _, onlyOffers, _ := deadlinePages(t, chainApp, now+600, 100, 0); len(onlyOffers) != 0 {
		t.Fatalf("Deadlines not_after %d returned %d leases", now+600, len(onlyOffers))
	}

	activePath, deadlinesPath := "/svn.leases.v1.Query/ActiveLeases", "/svn.leases.v1.Query/Deadlines"
	for _, tc := range []struct {
		path string
		req  proto.Message
		want msgkit.Code
	}{
		{activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: "p1", Page: &typesv1.PageRequest{Limit: 101}}, msgkit.Bounds},
		{activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: "p1", Page: &typesv1.PageRequest{Limit: 1, PageKey: bytes.Repeat([]byte("a"), 65)}}, msgkit.Bounds},
		{activePath, &leasesv1.QueryActiveLeasesRequest{}, msgkit.Malformed},
		{activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: "P1"}, msgkit.Bounds},
		{activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: "p1", ContentId: "c1", Page: &typesv1.PageRequest{Limit: 1, PageKey: []byte("l-c1")}}, msgkit.Bounds},
		{deadlinesPath, &leasesv1.QueryDeadlinesRequest{}, msgkit.Malformed},
		{deadlinesPath, &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: maxUnix + 1}, msgkit.Bounds},
		{deadlinesPath, &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: maxUnix, Page: &typesv1.PageRequest{Limit: 101}}, msgkit.Bounds},
		{deadlinesPath, &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: maxUnix, Page: &typesv1.PageRequest{Limit: 1, PageKey: []byte("l404")}}, msgkit.Bounds},
	} {
		assertQueryCode(t, chainApp, tc.path, tc.req, tc.want)
	}

	full := &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: maxUnix, Page: &typesv1.PageRequest{Limit: 100}}
	for _, q := range []struct {
		path string
		req  proto.Message
	}{
		{activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: "p1", Page: &typesv1.PageRequest{Limit: 100}}},
		{deadlinesPath, full},
	} {
		used, limit, _ := queryGas(t, chainApp, q.path, q.req)
		if limit != app.DefaultQueryGasLimit || used == 0 || used >= limit {
			t.Fatalf("%s used %d of %d gas", q.path, used, limit)
		}
		t.Logf("%s: query gas used %d (limit %d)", q.path, used, limit)
	}
	used, _, _ := queryGas(t, chainApp, deadlinesPath, full)

	// A node whose query gas limit is below what the query uses fails it.
	low := newRegNet()
	lc := low.startWith(t, 1, nodeOptions{app.QueryGasLimitOption: used - 1})
	leaseBasics(t, low, lc)
	lnow := lc.now()
	for i, id := range []string{"c1", "c2", "c3"} {
		offer := low.offerMsg(low.service, "of-"+id, "o-"+id, id, "p1", sizeSmall, lnow+1800+int64(i), lnow+600)
		wantCode(t, "low: offer "+id, low.send(t, lc, low.service, offer), msgkit.OK)
		wantCode(t, "low: accept "+id, low.send(t, lc, low.broadcaster, low.acceptMsg(t, low.broadcaster, "ac-"+id, offer, "l-"+id)), msgkit.OK)
		wantCode(t, "low: offer p2 "+id, low.send(t, lc, low.service, low.offerMsg(low.service, "of2-"+id, "o2-"+id, id, "p2", sizeSmall, lnow+1800, lnow+600+int64(i))), msgkit.OK)
	}
	if _, res := query(t, lc.app(), deadlinesPath, full, 0); res.Codespace != errorsmod.UndefinedCodespace || res.Code != errorsmod.ErrPanic.ABCICode() {
		t.Fatalf("Deadlines above the low query gas limit: %s/%d %s", res.Codespace, res.Code, res.Log)
	}
}
