package devnet_test

// Duplicate and race phases of TestDevnetBroadcasterLifecycle: DU
// (duplicate requests), FR1, FR1b and FR2 (front-runs of a held relay or
// register), FC and OC (content and offer front-runs), CS (concurrent
// requests for one signer and a second serve process on the same
// database) and MR (a foreign use of the broadcaster key's sequence).
// Holds are "fault injected by a test proxy on loopback".

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"google.golang.org/protobuf/proto"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// bcRawResult is one concurrent POST's outcome; retries counts the 503
// UNAVAILABLE answers before it.
type bcRawResult struct {
	status  int
	id      string
	code    string
	err     error
	retries int
}

// rawPost posts body to url as client from any goroutine: it reports
// errors instead of failing the test. A 503 UNAVAILABLE answer (a chain or
// database read failed; the API's documented "retry") is retried up to
// bcRetries times, 300 ms apart, and counted.
func rawPost(client *http.Client, url string, body []byte) bcRawResult {
	var r bcRawResult
	for try := 0; try < bcRetries; try++ {
		r = rawPostOnce(client, url, body)
		r.retries = try
		if r.err != nil || r.status != http.StatusServiceUnavailable || r.code != "UNAVAILABLE" {
			return r
		}
		time.Sleep(300 * time.Millisecond)
	}
	return r
}

// retriesOf sums the 503 answers retried across results.
func retriesOf(results []bcRawResult) int {
	sum := 0
	for _, r := range results {
		sum += r.retries
	}
	return sum
}

func rawPostOnce(client *http.Client, url string, body []byte) bcRawResult {
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return bcRawResult{err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return bcRawResult{err: err}
	}
	if resp.StatusCode >= 300 {
		var e bcAPIError
		if err := json.Unmarshal(raw, &e); err != nil {
			return bcRawResult{status: resp.StatusCode, err: err}
		}
		return bcRawResult{status: resp.StatusCode, code: e.Error.Code}
	}
	var v bcView
	if err := json.Unmarshal(raw, &v); err != nil {
		return bcRawResult{status: resp.StatusCode, err: err}
	}
	return bcRawResult{status: resp.StatusCode, id: v.IntentID}
}

// postConcurrently posts every body at once as caller to url and returns
// the results in body order.
func (n *bnet) postConcurrently(caller, url string, bodies [][]byte) []bcRawResult {
	n.t.Helper()
	out := make([]bcRawResult, len(bodies))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, body := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = rawPost(n.clients[caller], url, body)
		}()
	}
	close(start)
	wg.Wait()
	for i, r := range out {
		if r.err != nil {
			n.t.Fatalf("concurrent post %d: %v", i, r.err)
		}
	}
	return out
}

// reorderJSON re-encodes a JSON document with every object's keys in
// reverse order: the same document for a JSON reader, another byte string.
func reorderJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeReversed(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeReversed(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
		buf.WriteString("{ ")
		for i, k := range keys {
			if i > 0 {
				buf.WriteString(" , ")
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteString(" : ")
			if err := writeReversed(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteString(" }")
	case []any:
		buf.WriteString("[")
		for i, e := range x {
			if i > 0 {
				buf.WriteString(",")
			}
			if err := writeReversed(buf, e); err != nil {
				return err
			}
		}
		buf.WriteString("]")
	case float64:
		// Integers are written without a fraction, as the strict decoder
		// reads them.
		buf.WriteString(strconv.FormatFloat(x, 'f', -1, 64))
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

// duRounds bounds phase DU's rounds of 20 concurrent creates: a round in
// which some create was answered 503 (the documented retry) is checked but
// not judged, because the retry could hide a same-key race the store
// reported as UNAVAILABLE; DU is judged on the first round without one.
const duRounds = 3

// duRound posts 20 creates with one key and one body at once and checks
// that exactly one is 201 and every 201 or 200 names the same intent. It
// returns the request, its body, the answers by status:code, the intent id
// and the number of 503 answers retried.
func (n *bnet) duRound(name, contentID string) (bcIntentReq, []byte, map[string]int, string, int) {
	n.t.Helper()
	req := n.registerReq(creatorOne, contentID, sizeOne)
	body, err := json.Marshal(req)
	if err != nil {
		n.t.Fatal(err)
	}
	bodies := make([][]byte, 20)
	for i := range bodies {
		bodies[i] = body
	}
	results := n.postConcurrently(callerApp, n.apiURL()+"/v1/intents", bodies)
	answers := map[string]int{}
	intentID := ""
	for _, r := range results {
		answers[fmt.Sprintf("%d:%s", r.status, r.code)]++
		if r.status == http.StatusCreated {
			intentID = r.id
		}
	}
	if answers["201:"] != 1 {
		n.t.Fatalf("phase %s: 20 concurrent creates with one key answered %v, want exactly one 201", name, answers)
	}
	for _, r := range results {
		if (r.status == http.StatusCreated || r.status == http.StatusOK) && r.id != intentID {
			n.t.Fatalf("phase %s: concurrent creates with one key returned intents %s and %s", name, intentID, r.id)
		}
	}
	// Plan 13.4 DU: every concurrent create with one key answers the one
	// intent with 201 or 200.
	if answers["201:"]+answers["200:"] != len(results) {
		n.t.Fatalf("phase %s: of 20 concurrent creates with one key and one body, %v (status:code); want 1 x 201 and 19 x 200 naming intent %s",
			name, answers, intentID)
	}
	obj := req.object
	obj.Caller = callerApp
	n.objects[intentID] = obj
	return req, body, answers, intentID, retriesOf(results)
}

// phaseDuplicates is phase DU: 20 concurrent creates with one key make one
// intent and every one of them answers it; the same key with another body
// is 409; the same key and body with the JSON keys reordered is 200; two
// keys with the same relay payload both end COMMITTED, the first by its own
// transaction and the second with own_transaction false and the first's
// transaction as canonical, and one LeaseAccepted exists.
func phaseDuplicates(n *bnet, name string) {
	var (
		req      bcIntentReq
		body     []byte
		answers  map[string]int
		intentID string
		retried  int
		rounds   int
	)
	contentID := ""
	for rounds = 1; ; rounds++ {
		contentID = fmt.Sprintf("cdu%d", rounds)
		req, body, answers, intentID, retried = n.duRound(name, contentID)
		fmt.Printf("phase=%s round=%d content=%s answers=%v retried_503=%d\n", name, rounds, contentID, answers, retried)
		if retried == 0 {
			break
		}
		if rounds == duRounds {
			n.t.Fatalf("phase %s: every one of %d rounds of concurrent creates had a 503 answer", name, duRounds)
		}
	}
	other := req
	other.Request = n.registerReq(creatorOne, contentID, sizeTwo).Request
	if status, _, code := n.post(callerApp, other); status != http.StatusConflict || code != "IDEMPOTENCY_CONFLICT" {
		n.t.Fatalf("phase %s: same key, other body: %d %s", name, status, code)
	}
	reordered, err := reorderJSON(body)
	if err != nil {
		n.t.Fatal(err)
	}
	if bytes.Equal(reordered, body) {
		n.t.Fatalf("phase %s: the reordered body equals the original", name)
	}
	if status, v, code := n.call(n.clients[callerApp], http.MethodPost, n.apiURL()+"/v1/intents", reordered); status != http.StatusOK || v.IntentID != intentID {
		n.t.Fatalf("phase %s: same key and body, reordered keys: %d %s %s", name, status, v.IntentID, code)
	}
	n.committed(name, intentID)

	deadline, expiry := n.terms()
	offer := n.offerOf("odu", contentID, "p2", sizeOne, deadline, expiry)
	n.offerCommitted(name, callerApp, offer)
	sig := n.acceptSig(offer, "ldu", n.d.keys["p2"])
	first := n.create(callerApp, n.acceptReq(creatorOne, offer, "ldu", sig))
	second := n.create(callerApp, n.acceptReq(creatorOne, offer, "ldu", sig))
	a := n.waitTerminal(first.IntentID, 90*time.Second)
	b := n.waitTerminal(second.IntentID, 90*time.Second)
	if a.Status != stCommitted || b.Status != stCommitted || a.Commitment.FirstCommitTxHash != b.Commitment.FirstCommitTxHash ||
		!a.Commitment.OwnTransaction || b.Commitment.OwnTransaction {
		n.t.Fatalf("phase %s: two keys with one relay payload: %s and %s, want both COMMITTED, the first by its own transaction", name, a, b)
	}
	effects := n.objectEffects(bcObject{Event: evAccepted, ID: "ldu"})
	if len(effects) != 1 || !bytes.Equal(effects[0].Hash, n.hashOf(a.Commitment.FirstCommitTxHash)) {
		n.t.Fatalf("phase %s: %d committed LeaseAccepted for ldu, want one: the first relay's %s", name, len(effects), a.Commitment.FirstCommitTxHash)
	}
	fmt.Printf("phase=%s concurrent_creates=20 rounds=%d judged_round_retried_503=0 intents=1 answers=%v other_body=409 reordered_keys=200 relay_keys=2 canonical=%s own=[%t,%t] lease_accepted=1\n",
		name, rounds, answers, a.Commitment.FirstCommitTxHash, a.Commitment.OwnTransaction, b.Commitment.OwnTransaction)
}

// holdBroadcasts makes the proxy hold every broadcast and waits until one
// is held for intent id; it returns the intent's row.
func (n *bnet) holdFor(id string) bcIntentRow {
	n.t.Helper()
	bcWait(n.t, "a held broadcast", 60*time.Second, func() bool { return len(n.proxy.heldTx(broadcastMethod)) >= 1 }, n.serve)
	row := n.intentRow(id)
	if row.BusinessID == "" {
		n.t.Fatalf("intent %s has no business id while its broadcast is held", id)
	}
	return row
}

// releaseBroadcasts clears the rules and forwards every held broadcast.
func (n *bnet) releaseBroadcasts() int {
	n.proxy.clearRules()
	return n.proxy.release(broadcastMethod, true)
}

// sendFrom signs msg from who (at who's synced sequence) and commits it
// with broadcast_tx_commit; it returns the committed result.
func (n *bnet) sendFrom(phase string, who loaded, msg sdk.Msg) *cmtResult {
	n.t.Helper()
	n.d.syncSequence(who)
	res := n.d.send(who, msg)
	if res.deliver.Code != 0 {
		n.t.Fatalf("phase %s: %s: deliver %s/%d %s", phase, who.name, res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
	}
	fmt.Printf("phase=%s competing_tx signer=%s height=%d tx=%X\n", phase, who.name, res.height, res.hash)
	return res
}

// offerWith returns offer with another signer and business id.
func offerWith(offer *leasesv1.MsgOfferLease, signer []byte, businessID string) *leasesv1.MsgOfferLease {
	c := proto.CloneOf(offer)
	c.Signer, c.BusinessId = signer, businessID
	return c
}

// relaySetup registers contentID, offers it to providerID and returns the
// offer.
func (n *bnet) relaySetup(phase, contentID, offerID, providerID string) *leasesv1.MsgOfferLease {
	n.t.Helper()
	n.ownContent(phase, callerApp, sizeOne, contentID)
	deadline, expiry := n.terms()
	offer := n.offerOf(offerID, contentID, providerID, sizeOne, deadline, expiry)
	n.offerCommitted(phase, callerApp, offer)
	return offer
}

// phaseFrontRunBusinessID is phase FR1: while the relay is held, p1's own
// account commits the identical acceptance under the relay's business id.
// The released relay commits code 0 as an idempotent replay; the intent is
// COMMITTED with idempotent_replay and the provider's transaction as
// canonical.
func phaseFrontRunBusinessID(n *bnet, name string) {
	offer := n.relaySetup(name, "cfr1", "ofr1", "p1")
	sig := n.acceptSig(offer, "lfr1", n.d.keys["p1"])
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	v := n.create(callerApp, n.acceptReq(creatorOne, offer, "lfr1", sig))
	row := n.holdFor(v.IntentID)
	provider := n.sendFrom(name, n.d.provider1, &leasesv1.MsgAcceptLease{Signer: n.d.provider1.key.PubKey().Address(),
		BusinessId: row.BusinessID, OfferId: offer.GetOfferId(), LeaseId: "lfr1", ProviderAcceptance: sig})
	released := n.releaseBroadcasts()
	v = n.waitTerminal(v.IntentID, 90*time.Second)
	if v.Status != stCommitted || !v.Commitment.IdempotentReplay || v.Commitment.OwnTransaction ||
		!bytes.Equal(n.hashOf(v.Commitment.FirstCommitTxHash), provider.hash) {
		n.t.Fatalf("phase %s: %s, want COMMITTED, idempotent replay, canonical %X", name, v, provider.hash)
	}
	ours := n.lastCommitted(v.IntentID)
	res := n.committedTx(ours.Hash, 30*time.Second)
	if res.TxResult.Code != 0 || !hasIdempotent(res.TxResult.Events, "leases") || ours.Phase != "COMMITTED_IDEMPOTENT" {
		n.t.Fatalf("phase %s: our relay %s committed code %d without leases_result idempotent=true", name, ours, res.TxResult.Code)
	}
	fmt.Printf("phase=%s held_relay_released=%d business_id=%s provider_tx=%X our_tx=%X code=0 leases_result=idempotent canonical=provider\n",
		name, released, row.BusinessID, provider.hash, ours.Hash)
}

// chainResult requires attempt a's committed result as the outbox stored it
// to be the chain's: node 0's /tx answer for a's hash, at the same height,
// with codespace space and code.
func (n *bnet) chainResult(phase string, a bcAttemptRow, space string, code int64) {
	n.t.Helper()
	res := n.committedTx(a.Hash, 30*time.Second)
	if res.TxResult.Codespace != space || int64(res.TxResult.Code) != code || res.Height != a.CommittedHeight.Int64 ||
		a.CommittedSpace != space || a.CommittedCode.Int64 != code {
		n.t.Fatalf("phase %s: attempt %s; the chain shows %s/%d at %d, want %s/%d", phase, a, res.TxResult.Codespace, res.TxResult.Code,
			res.Height, space, code)
	}
}

// lastCommitted returns the intent's attempt that has a committed result.
func (n *bnet) lastCommitted(id string) bcAttemptRow {
	n.t.Helper()
	rows := n.attempts(id)
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].CommittedHeight.Valid {
			return rows[i]
		}
	}
	n.t.Fatalf("intent %s has no committed attempt: %v", id, rows)
	return bcAttemptRow{}
}

// phaseFrontRunOwnAcceptance is phase FR1b: while the relay is held, p1's
// own account commits the same acceptance under its own business id. The
// released relay commits NOT_FOUND (the offer closed); the effect check
// finds the provider's transaction; COMMITTED with own_transaction false.
func phaseFrontRunOwnAcceptance(n *bnet, name string) {
	offer := n.relaySetup(name, "cfr1b", "ofr1b", "p1")
	sig := n.acceptSig(offer, "lfr1b", n.d.keys["p1"])
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	v := n.create(callerApp, n.acceptReq(creatorOne, offer, "lfr1b", sig))
	n.holdFor(v.IntentID)
	provider := n.sendFrom(name, n.d.provider1, &leasesv1.MsgAcceptLease{Signer: n.d.provider1.key.PubKey().Address(),
		BusinessId: "p1-own-fr1b", OfferId: offer.GetOfferId(), LeaseId: "lfr1b", ProviderAcceptance: sig})
	n.releaseBroadcasts()
	v = n.waitTerminal(v.IntentID, 90*time.Second)
	if v.Status != stCommitted || v.Commitment.OwnTransaction || !bytes.Equal(n.hashOf(v.Commitment.FirstCommitTxHash), provider.hash) {
		n.t.Fatalf("phase %s: %s, want COMMITTED by the provider's transaction %X", name, v, provider.hash)
	}
	ours := n.lastCommitted(v.IntentID)
	notFound := int64(typesv1.ErrorCode_ERROR_CODE_NOT_FOUND)
	n.chainResult(name, ours, "svn", notFound)
	fmt.Printf("phase=%s provider_tx=%X our_tx=%X our_code=svn/%d result=COMMITTED own_transaction=false\n", name, provider.hash, ours.Hash, notFound)
}

// phaseBusinessIDTaken is phase FR2: while a register is held, service-2
// commits another register under its business id. The released register
// commits REPLAY; the broadcaster draws one new business id and commits.
// With the business-attempt bound at 1, the same race ends
// FAILED(BUSINESS_ID_TAKEN).
func phaseBusinessIDTaken(n *bnet, name string) {
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	v := n.create(callerApp, n.registerReq(creatorOne, "cfr2", sizeOne))
	row := n.holdFor(v.IntentID)
	n.sendFrom(name, n.d.service2, n.d.registerContent(n.d.service2, row.BusinessID, "cfr2x", 1, sizeOne))
	n.releaseBroadcasts()
	v = n.committed(name, v.IntentID)
	after := n.intentRow(v.IntentID)
	rows := n.attempts(v.IntentID)
	replay := int64(typesv1.ErrorCode_ERROR_CODE_REPLAY)
	if after.BusinessAttempt != 2 || after.BusinessID == row.BusinessID || rows[0].CommittedCode.Int64 != replay ||
		rows[len(rows)-1].BusinessN != 2 {
		n.t.Fatalf("phase %s: row %s attempts %v, want REPLAY then one new business id", name, after, rows)
	}
	n.chainResult(name, rows[0], "svn", replay)
	fmt.Printf("phase=%s taken_business_id=%s first=svn/%d new_business_id=%s business_attempt=2 result=COMMITTED\n",
		name, row.BusinessID, replay, after.BusinessID)

	n.serial++
	bound := n.writeServeConfig(fmt.Sprintf("serve-business-1-%d.json", n.serial), n.listen, n.proxy.url(), map[string]any{"business_attempts": 1})
	n.swapServe(bound)
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	v = n.create(callerApp, n.registerReq(creatorOne, "cfr2b", sizeOne))
	row = n.holdFor(v.IntentID)
	n.sendFrom(name, n.d.service2, n.d.registerContent(n.d.service2, row.BusinessID, "cfr2y", 1, sizeOne))
	n.releaseBroadcasts()
	n.failed(name, v.IntentID, "BUSINESS_ID_TAKEN")
	n.swapServe(n.serveCfg)
	fmt.Printf("phase=%s rerun business_attempts=1 result=FAILED(BUSINESS_ID_TAKEN)\n", name)
}

// phaseContentFrontRun is phase FC: while our register of a content id is
// held, service-2 registers the same id. Ours commits DUPLICATE_ID; the
// effect check names service-2; FAILED(OBJECT_TAKEN); the ownership row is
// VOID; a later offer for the id by our caller is refused at enqueue.
func phaseContentFrontRun(n *bnet, name string) {
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	v := n.create(callerApp, n.registerReq(creatorOne, "cfc", sizeOne))
	n.holdFor(v.IntentID)
	rival := n.sendFrom(name, n.d.service2, n.d.registerContent(n.d.service2, "s2-fc", "cfc", 1, sizeOne))
	n.releaseBroadcasts()
	n.failed(name, v.IntentID, "OBJECT_TAKEN")
	ours := n.lastCommitted(v.IntentID)
	dup := int64(typesv1.ErrorCode_ERROR_CODE_DUPLICATE_ID)
	effects := n.objectEffects(bcObject{Event: evRegistered, ID: "cfc", Version: 1, HasVersion: true})
	if len(effects) != 1 || !bytes.Equal(effects[0].Signer, n.d.service2.key.PubKey().Address()) {
		n.t.Fatalf("phase %s: our register committed %d; effects %v", name, ours.CommittedCode.Int64, effects)
	}
	n.chainResult(name, ours, "svn", dup)
	if owners := n.contentOwners("cfc"); len(owners) != 1 || owners[0] != "VOID:"+callerApp {
		n.t.Fatalf("phase %s: ownership rows %v, want one VOID", name, owners)
	}
	deadline, expiry := n.terms()
	n.refused(name, callerApp, n.offerReq(creatorOne, n.offerOf("ofc", "cfc", "p2", sizeOne, deadline, expiry)), http.StatusNotFound, "NOT_FOUND")
	fmt.Printf("phase=%s rival_tx=%X our_code=svn/%d result=FAILED(OBJECT_TAKEN) committing_signer=service-2 owner=VOID later_offer=refused\n", name, rival.hash, dup)
}

// phaseOfferCopy is phase OC: while our offer is held, service-2 commits a
// copy of it (same fields, its own business id). Ours commits
// DUPLICATE_ID; the effect check names service-2; FAILED(OBJECT_TAKEN); a
// relay of an acceptance for that offer by our caller is refused.
func phaseOfferCopy(n *bnet, name string) {
	n.ownContent(name, callerApp, sizeOne, "coc")
	deadline, expiry := n.terms()
	offer := n.offerOf("ooc", "coc", "p2", sizeOne, deadline, expiry)
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	v := n.create(callerApp, n.offerReq(creatorOne, offer))
	n.holdFor(v.IntentID)
	rival := n.sendFrom(name, n.d.service2, offerWith(offer, n.d.service2.key.PubKey().Address(), "s2-oc"))
	n.releaseBroadcasts()
	n.failed(name, v.IntentID, "OBJECT_TAKEN")
	ours := n.lastCommitted(v.IntentID)
	dup := int64(typesv1.ErrorCode_ERROR_CODE_DUPLICATE_ID)
	n.chainResult(name, ours, "svn", dup)
	copied := offerWith(offer, n.d.service2.key.PubKey().Address(), "")
	n.refused(name, callerApp, n.acceptReq(creatorOne, copied, "loc", n.acceptSig(copied, "loc", n.d.keys["p2"])), http.StatusForbidden, "RELAY_NOT_ALLOWED")
	fmt.Printf("phase=%s rival_tx=%X our_code=svn/%d result=FAILED(OBJECT_TAKEN) relay_for_copy=refused\n", name, rival.hash, dup)
}

// phaseConcurrentSigner is phase CS: a second serve process runs on the
// same database with its own proxy port; 20 concurrent REGISTER_CONTENT
// requests for the service signer all commit with unique, dense
// sequences; the chain sequence advances by the number of included
// transactions; the second process writes no attempt or transition and
// forwards nothing while the first holds the signer's lock; after the
// first is killed, the second takes over.
func phaseConcurrentSigner(n *bnet, name string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		n.t.Fatal(err)
	}
	listen2 := ln.Addr().String()
	if err := ln.Close(); err != nil {
		n.t.Fatal(err)
	}
	proxy2 := startProxy(n.t, "serve-2", n.rpcs[0])
	serve2 := n.launchServeAt("serve-2", n.writeServeConfig("serve-2.json", listen2, proxy2.url(), nil), listen2)
	worker2 := fmt.Sprintf("serve-%d-", serve2.cmd.Process.Pid)
	n.d.syncSequence(n.service)
	seqBefore := n.d.seq[n.service.addr]
	var bodies [][]byte
	var reqs []bcIntentReq
	for i := 0; i < 20; i++ {
		req := n.registerReq(creatorOne, fmt.Sprintf("ccs-%02d", i), sizeOne)
		body, err := json.Marshal(req)
		if err != nil {
			n.t.Fatal(err)
		}
		bodies, reqs = append(bodies, body), append(reqs, req)
	}
	results := n.postConcurrently(callerApp, n.apiURL()+"/v1/intents", bodies)
	var ids []string
	for i, r := range results {
		if r.status != http.StatusCreated {
			n.t.Fatalf("phase %s: concurrent create %d: %+v", name, i, r)
		}
		obj := reqs[i].object
		obj.Caller = callerApp
		n.objects[r.id] = obj
		ids = append(ids, r.id)
	}
	var seqs []int64
	included := 0
	for _, id := range ids {
		n.committed(name, id)
		for _, a := range n.attempts(id) {
			seqs = append(seqs, a.Sequence)
			if a.CommittedHeight.Valid {
				included++
			}
		}
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for i := 1; i < len(seqs); i++ {
		if seqs[i] != seqs[i-1]+1 {
			n.t.Fatalf("phase %s: attempt sequences %v are not unique and dense", name, seqs)
		}
	}
	n.d.syncSequence(n.service)
	if advanced := int(n.d.seq[n.service.addr] - seqBefore); advanced != included {
		n.t.Fatalf("phase %s: the chain sequence advanced by %d for %d included transactions", name, advanced, included)
	}
	if rows := n.rowsByWorker(worker2); rows != 0 || proxy2.count(broadcastMethod).Forwarded != 0 {
		n.t.Fatalf("phase %s: the second serve wrote %d rows and forwarded %d broadcasts while the first held the lock",
			name, rows, proxy2.count(broadcastMethod).Forwarded)
	}
	fmt.Printf("phase=%s concurrent=20 retried_503=%d sequences=%d..%d dense=true chain_advanced=%d included=%d second_worker=%s rows=0 forwards=0\n",
		name, retriesOf(results), seqs[0], seqs[len(seqs)-1], included, included, worker2)

	n.serve.kill(n.t)
	takeover := n.registerReq(creatorOne, "ccs-takeover", sizeOne)
	body, err := json.Marshal(takeover)
	if err != nil {
		n.t.Fatal(err)
	}
	status, v, code := n.call(n.clients[callerApp], http.MethodPost, "https://"+listen2+"/v1/intents", body)
	if status != http.StatusCreated {
		n.t.Fatalf("phase %s: create through the second serve: %d %s", name, status, code)
	}
	obj := takeover.object
	obj.Caller = callerApp
	n.objects[v.IntentID] = obj
	n.serve, n.apiAddr = serve2, listen2
	n.committed(name, v.IntentID)
	if rows := n.rowsByWorker(worker2); rows == 0 || proxy2.count(broadcastMethod).Forwarded == 0 {
		n.t.Fatalf("phase %s: the second serve did not take over after the first was killed", name)
	}
	if code := serve2.stop(n.t); code != 0 {
		n.t.Fatalf("phase %s: serve-2 exit %d", name, code)
	}
	n.apiAddr = n.listen
	n.serve = n.startServe("serve", n.serveCfg)
	fmt.Printf("phase=%s killed=serve-1 takeover=serve-2 rows_after=%d forwards_after=%d\n", name, n.rowsByWorker(worker2), proxy2.count(broadcastMethod).Forwarded)
}

// launchServeAt starts a serve process listening on listen and waits for
// its health answer.
func (n *bnet) launchServeAt(name, cfg, listen string) *bcProc {
	n.t.Helper()
	p := n.launch.start(name, n.serveID, []string{"SVN_FAULTPOINT_DIR=" + n.faultServe}, "serve", "--config", cfg)
	bcWait(n.t, name+" health", 2*time.Minute, func() bool {
		resp, err := n.clients[callerApp].Get("https://" + listen + "/v1/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, p)
	return p
}

// rowsByWorker counts the attempt and transition rows a worker instance
// (by its "serve-<pid>-" prefix) wrote.
func (n *bnet) rowsByWorker(prefix string) int {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	var count int
	if err := n.reader.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM broadcaster.attempts WHERE starts_with(worker_instance, $1))
		+ (SELECT count(*) FROM broadcaster.transitions WHERE starts_with(worker_instance, $1))`, prefix).Scan(&count); err != nil {
		n.t.Fatal(err)
	}
	return count
}

// hasIdempotent reports whether events hold <module>_result idempotent=true.
func hasIdempotent(events []abci.Event, module string) bool {
	for _, ev := range events {
		if ev.Type != module+"_result" {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == "idempotent" && a.Value == "true" {
				return true
			}
		}
	}
	return false
}

// phaseSignerConflict is phase MR (DR-11-15): while our relay is held at
// broadcaster sequence N, the test signs a MsgOfferLease with the
// broadcaster key at N and broadcasts it directly; it commits FORBIDDEN
// with tx.acc_seq present. Our released send is refused by CheckTx (sdk
// 32) and not counted for (d2); the acc_seq search names the foreign
// hash; the signer halts with SIGNER_CONFLICT and forwards nothing more;
// a second relay queued on the halted key gets no attempt and is then
// cancelled; after signer-clear the relay commits at N+1.
func phaseSignerConflict(n *bnet, name string) {
	offer := n.relaySetup(name, "cmr", "omr", "p2")
	offer2 := n.relaySetup(name, "cmr2", "omr2", "p2")
	n.proxy.setRule(broadcastMethod, proxyHold, -1)
	v := n.create(callerApp, n.acceptReq(creatorOne, offer, "lmr", n.acceptSig(offer, "lmr", n.d.keys["p2"])))
	n.holdFor(v.IntentID)
	seq := n.attempts(v.IntentID)[0].Sequence
	d := n.d
	deadline, expiry := n.terms()
	foreignMsg := offerWith(n.offerOf("omr-foreign", "cmr", "p2", sizeOne, deadline, expiry), n.bcastAddr(), "mr-foreign")
	d.seq[n.bcast.addr] = uint64(seq)
	raw := d.signMulti(n.bcast, 2_000_000, foreignMsg)
	res, err := d.broadcast(raw)
	if err != nil {
		n.t.Fatal(err)
	}
	forbidden := uint32(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN)
	if res.check.Code != 0 || res.deliver.Codespace != "svn" || res.deliver.Code != forbidden {
		n.t.Fatalf("phase %s: the foreign transaction: check %s/%d deliver %s/%d %s", name, res.check.Codespace, res.check.Code,
			res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
	}
	if named := n.accSeqHashes(n.bcastAddr(), seq); len(named) != 1 || !bytes.Equal(named[0], res.hash) {
		n.t.Fatalf("phase %s: acc_seq %d names %v, want the foreign %X", name, seq, sortedHex(named), res.hash)
	}
	n.releaseBroadcasts()
	bcWait(n.t, "the SIGNER_CONFLICT halt", 120*time.Second, func() bool {
		return n.signerRow(n.bcastAddr()).Halted == "SIGNER_CONFLICT"
	}, n.serve)
	// Our released sends were refused by CheckTx with sdk 32 (the
	// sequence is used): recorded MEMPOOL_REJECTED and not counted for
	// (d2). Once the chain passed the attempt's timeout height, a resend
	// is refused with sdk 30, which counts; the attempt is never dead by
	// (d2) because not every recorded send is a counted refusal.
	first := n.attempts(v.IntentID)[0]
	sdk32 := 0
	for _, tr := range n.transitionsOf(v.IntentID) {
		var obs struct {
			Class     string `json:"class"`
			Code      int64  `json:"code"`
			Codespace string `json:"codespace"`
		}
		if tr.Attempt.String == first.ID && tr.Observation != "" && json.Unmarshal([]byte(tr.Observation), &obs) == nil &&
			obs.Class == "MEMPOOL_REJECTED" && obs.Codespace == "sdk" && obs.Code == 32 {
			sdk32++
		}
	}
	if sdk32 == 0 || first.CountedRefusals >= first.SendsRecorded || first.DeadReason == "D2_REFUSED" {
		n.t.Fatalf("phase %s: our refused sends: %s with %d sdk 32 transitions, want sdk 32 recorded and not counted for (d2)", name, first, sdk32)
	}
	events := n.signerEvents()
	if len(events) == 0 || !bytes.Equal(events[len(events)-1].Foreign, res.hash) || events[len(events)-1].Sequence != seq {
		n.t.Fatalf("phase %s: signer events %v, want the foreign use of %d", name, events, seq)
	}
	// Work queued for the halted key stays queued for longer than the
	// backoff cap plus two polls: no attempt, no forward. The queued relay
	// is then cancelled, which the API allows only while nothing was ever
	// signed for it.
	queued := n.create(callerApp, n.acceptReq(creatorOne, offer2, "lmr2", n.acceptSig(offer2, "lmr2", n.d.keys["p2"])))
	forwarded := n.proxy.count(broadcastMethod).Forwarded
	window := time.Duration(n.numbers["backoff_cap_seconds"].(int))*time.Second +
		2*time.Duration(n.numbers["poll_interval_ms"].(int))*time.Millisecond + time.Second
	time.Sleep(window)
	if got := n.proxy.count(broadcastMethod).Forwarded; got != forwarded {
		n.t.Fatalf("phase %s: the halted signer forwarded %d broadcasts", name, got-forwarded)
	}
	if rows := n.attempts(queued.IntentID); len(rows) != 0 {
		n.t.Fatalf("phase %s: the halted signer started %d attempts for a queued relay: %v", name, len(rows), rows)
	}
	status, cancelled, cancelCode := n.call(n.clients[callerApp], http.MethodPost, n.apiURL()+"/v1/intents/"+queued.IntentID+"/cancel", nil)
	if status != http.StatusOK || cancelled.Status != stFailed || cancelled.failureReason() != "CANCELLED" {
		n.t.Fatalf("phase %s: cancel of the queued relay: %d %s %s", name, status, cancelCode, cancelled)
	}
	if code := n.serve.stop(n.t); code != 0 {
		n.t.Fatalf("phase %s: serve exit %d", name, code)
	}
	code, p := n.launch.run("signer-clear", n.serveID, "signer-clear", "--config", n.serveCfg, "--signer", bech(n.bcastAddr()),
		"--sequence", strconv.FormatInt(seq, 10), "--foreign-hash", hex.EncodeToString(res.hash))
	if code != 0 {
		n.t.Fatalf("phase %s: signer-clear exit %d: %s", name, code, p.logTail(5))
	}
	n.restartServe()
	v = n.committed(name, v.IntentID)
	last := n.lastCommitted(v.IntentID)
	if last.Sequence != seq+1 {
		n.t.Fatalf("phase %s: the relay committed at sequence %d, want %d", name, last.Sequence, seq+1)
	}
	fmt.Printf("phase=%s sequence=%d foreign_tx=%X foreign_result=svn/%d acc_seq_present=true our_sends=%d sdk32_transitions=%d counted_refusals=%d (sdk 32 not counted) halt=SIGNER_CONFLICT forwarded_while_halted=0 queued_while_halted=attempts:0,window:%s,then:CANCELLED signer_clear=exit0 relay_sequence=%d\n",
		name, seq, res.hash, forbidden, first.SendsRecorded, sdk32, first.CountedRefusals, window, last.Sequence)
}
