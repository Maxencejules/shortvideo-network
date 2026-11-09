package devnet_test

// Chain reads and the accounting check of the broadcaster devnet tests.
// checkAccounting (plan 13.4, phase Z's function) runs after every phase
// and at the end. Every read goes to validator 0 directly, never through
// the fault proxy, and every search runs against the real node.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// serviceAddr and bcastAddr are the broadcaster's SERVICE and BROADCASTER
// account addresses.
func (n *bnet) serviceAddr() []byte { return n.service.key.PubKey().Address() }
func (n *bnet) bcastAddr() []byte   { return n.bcast.key.PubKey().Address() }

// bech is the bech32 form of an account address, as tx.acc_seq names it.
func bech(addr []byte) string { return sdk.AccAddress(addr).String() }

// txSearch returns every committed transaction query matches on validator
// 0, reading pages of 100 until the node's total is read.
func (n *bnet) txSearch(query string) []*coretypes.ResultTx {
	n.t.Helper()
	var out []*coretypes.ResultTx
	for page := 1; page <= 50; page++ {
		p, per := page, 100
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		res, err := n.d.clients[0].TxSearch(ctx, query, false, &p, &per, "asc")
		cancel()
		if err != nil {
			n.t.Fatalf("tx_search %q: %v", query, err)
		}
		out = append(out, res.Txs...)
		if len(out) >= res.TotalCount || len(res.Txs) == 0 {
			if len(out) != res.TotalCount {
				n.t.Fatalf("tx_search %q: read %d of %d", query, len(out), res.TotalCount)
			}
			return out
		}
	}
	n.t.Fatalf("tx_search %q: more than 50 pages", query)
	return nil
}

// accSeqTxs returns every committed transaction whose ante handler
// consumed sequence seq of addr (the tx.acc_seq attribute).
func (n *bnet) accSeqTxs(addr []byte, seq int64) []*coretypes.ResultTx {
	n.t.Helper()
	return n.txSearch(fmt.Sprintf("tx.acc_seq='%s/%d'", bech(addr), seq))
}

// accSeqHashes returns the hashes of accSeqTxs.
func (n *bnet) accSeqHashes(addr []byte, seq int64) [][]byte {
	n.t.Helper()
	var out [][]byte
	for _, tx := range n.accSeqTxs(addr, seq) {
		out = append(out, tx.Hash)
	}
	return out
}

// eventIDKey is the attribute that names an object in its typed event.
func eventIDKey(event string) string {
	switch event {
	case evRegistered, evWithdrawn:
		return "content_id"
	case evOffered:
		return "offer_id"
	}
	return "lease_id"
}

// eventMatches reports whether a parsed typed event is obj's.
func eventMatches(obj bcObject, msg proto.Message) bool {
	switch e := msg.(type) {
	case *eventsv1.ContentRegistered:
		return obj.Event == evRegistered && e.GetContentId() == obj.ID && e.GetContentVersion() == obj.Version
	case *eventsv1.ContentWithdrawn:
		return obj.Event == evWithdrawn && e.GetContentId() == obj.ID && e.GetContentVersion() == obj.Version
	case *eventsv1.LeaseOffered:
		return obj.Event == evOffered && e.GetOfferId() == obj.ID
	case *eventsv1.LeaseAccepted:
		return obj.Event == evAccepted && e.GetLeaseId() == obj.ID
	case *eventsv1.LeaseRenewed:
		return obj.Event == evRenewed && e.GetLeaseId() == obj.ID && e.GetTermVersion() == obj.Version
	case *eventsv1.LeaseEnded:
		return obj.Event == evEnded && e.GetLeaseId() == obj.ID
	}
	return false
}

// bcEffect is one committed transaction carrying an object's typed event:
// its hash, height and the signer of the message that emitted it.
type bcEffect struct {
	Hash   []byte
	Height int64
	Signer []byte
}

// objectEffects returns every committed transaction that carries obj's
// typed event (typed event search, msg_index aware), with the signer of
// the emitting message.
func (n *bnet) objectEffects(obj bcObject) []bcEffect {
	n.t.Helper()
	var out []bcEffect
	query := fmt.Sprintf("%s.%s='%s'", obj.Event, eventIDKey(obj.Event), obj.ID)
	for _, tx := range n.txSearch(query) {
		if tx.TxResult.Code != 0 {
			continue
		}
		var index int64 = -1
		for _, ev := range tx.TxResult.Events {
			if ev.Type != obj.Event {
				continue
			}
			msg, msgIndex, err := msgkit.ParseCommitted(ev)
			if err != nil {
				n.t.Fatalf("typed event of %X: %v", tx.Hash, err)
			}
			if eventMatches(obj, msg) {
				if index >= 0 {
					n.t.Fatalf("%X emits %s twice", tx.Hash, obj)
				}
				index = int64(msgIndex)
			}
		}
		if index < 0 {
			continue
		}
		out = append(out, bcEffect{Hash: tx.Hash, Height: tx.Height, Signer: n.msgSigner(tx.Tx, int(index))})
	}
	return out
}

// msgSigner decodes raw and returns the signer field of message index.
func (n *bnet) msgSigner(raw []byte, index int) []byte {
	n.t.Helper()
	decoded, err := n.d.enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		n.t.Fatalf("decode committed transaction: %v", err)
	}
	msgs := decoded.GetMsgs()
	if index >= len(msgs) {
		n.t.Fatalf("msg_index %d of %d messages", index, len(msgs))
	}
	s, ok := msgs[index].(interface{ GetSigner() []byte })
	if !ok {
		n.t.Fatalf("message %T has no signer field", msgs[index])
	}
	return s.GetSigner()
}

// committedTx reads one committed transaction by hash from validator 0,
// waiting for the node's index.
func (n *bnet) committedTx(hash []byte, timeout time.Duration) *coretypes.ResultTx {
	n.t.Helper()
	return n.d.waitTx(hash, timeout)
}

// txOnChain reports whether validator 0 holds a committed transaction
// with hash (a missing transaction is not an error).
func (n *bnet) txOnChain(hash []byte) (*coretypes.ResultTx, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := n.d.clients[0].Tx(ctx, hash, false)
	if err != nil {
		return nil, false
	}
	return res, true
}

// quiesce waits until every intent is terminal and every running node's
// mempool is empty (plan 13.4: a phase starts only from that state).
func (n *bnet) quiesce(phase string) {
	n.t.Helper()
	bcWait(n.t, "phase "+phase+": every intent terminal", 4*time.Minute, func() bool {
		return len(n.openIntentsByCaller()) == 0
	}, n.serve)
	bcWait(n.t, "phase "+phase+": empty mempools", 2*time.Minute, func() bool {
		for i, client := range n.d.clients {
			if n.stops[i] == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			res, err := client.NumUnconfirmedTxs(ctx)
			cancel()
			if err != nil || res.Total != 0 {
				return false
			}
		}
		return true
	})
}

// checkAccounting is plan 13.4's accounting check. full re-checks every
// intent; otherwise intents verified by an earlier call are skipped in (b)
// and (c) (their rows are immutable once terminal; the final call is full).
func (n *bnet) checkAccounting(phase string, full bool) {
	n.t.Helper()
	n.quiesce(phase)
	n.checkSequences(phase)
	rows := n.allIntents()
	committed, failed := 0, 0
	for _, r := range rows {
		if !full && n.verified(r.ID) {
			continue
		}
		n.checkIntent(phase, r)
		if r.State == "COMMITTED" {
			committed++
		} else {
			failed++
		}
		n.markVerified(r.ID)
	}
	// (d) no caller has an open intent after the phase. The schema keeps
	// no stored open-intent counter that could leak: the per-caller cap is
	// checked against a count of open rows (store test
	// TestPerCallerCapNoGlobalLock shows the cap and the slot a terminal
	// intent frees). quiesce already waited for this state, so (d) restates
	// it; it is not a separate leak check.
	if open := n.openIntentsByCaller(); len(open) != 0 {
		n.t.Fatalf("phase %s: open intents after the phase: %v", phase, open)
	}
	// (e) chain invariant, not broadcaster mutation evidence: x/leases
	// keeps reserved_bytes equal to the held leases whatever the
	// broadcaster does.
	height := n.d.height()
	var reserved []string
	for _, provider := range n.providers {
		got := n.d.reservedAt(n.d.clients[0], provider, height)
		var sum uint64
		for _, lease := range n.leasesOf(provider, height) {
			sum += lease.GetPackageBytes()
		}
		if got != sum {
			n.t.Fatalf("phase %s: provider %s reserved %d, held leases sum %d (height %d)", phase, provider, got, sum, height)
		}
		reserved = append(reserved, fmt.Sprintf("%s=%d", provider, got))
	}
	fmt.Printf("accounting phase=%s intents=%d checked_committed=%d checked_failed=%d open=0 (no stored counter exists) reserved=%s (chain invariant, not broadcaster mutation evidence) height=%d full=%t\n",
		phase, len(rows), committed, failed, strings.Join(reserved, ","), height, full)
}

// checkSequences is check (a): for each broadcaster signer and every
// sequence up to the chain's, tx.acc_seq names exactly one committed
// transaction, and it is an attempt of this outbox or a recorded foreign
// use. An attempt it names is not dead and holds the chain's result: the
// committed height, code and codespace of node 0's answer. Sequences
// checked by an earlier call are not searched again: a committed
// transaction and a terminal intent's attempt rows never change.
func (n *bnet) checkSequences(phase string) {
	n.t.Helper()
	events := n.signerEvents()
	for _, who := range []loaded{n.service, n.bcast} {
		addr := who.key.PubKey().Address()
		n.d.syncSequence(who)
		chainSeq := int64(n.d.seq[who.addr])
		ours := map[string]bcAttemptRow{}
		for _, a := range n.attemptsWhere(`signer_address = $1`, addr) {
			ours[string(a.Hash)] = a
		}
		foreign := map[string]bool{}
		for _, e := range events {
			if bytes.Equal(e.Signer, addr) {
				foreign[string(e.Foreign)] = true
			}
		}
		from := n.verifiedSeq[who.addr]
		for seq := from; seq < chainSeq; seq++ {
			txs := n.accSeqTxs(addr, seq)
			if len(txs) != 1 {
				n.t.Fatalf("phase %s: %s sequence %d is named by %d committed transactions", phase, who.name, seq, len(txs))
			}
			tx := txs[0]
			a, own := ours[string(tx.Hash)]
			if !own && !foreign[string(tx.Hash)] {
				n.t.Fatalf("phase %s: %s sequence %d was used by %X, neither an attempt nor a recorded foreign use (ghost or unrecorded submission)",
					phase, who.name, seq, tx.Hash)
			}
			if own && (a.DeadReason != "" || !a.CommittedHeight.Valid || a.CommittedHeight.Int64 != tx.Height ||
				a.CommittedCode.Int64 != int64(tx.TxResult.Code) || a.CommittedSpace != tx.TxResult.Codespace || a.Sequence != seq) {
				n.t.Fatalf("phase %s: %s sequence %d: attempt %s, but the chain committed %X at %d with %s/%d",
					phase, who.name, seq, a, tx.Hash, tx.Height, tx.TxResult.Codespace, tx.TxResult.Code)
			}
		}
		n.verifiedSeq[who.addr] = chainSeq
		fmt.Printf("accounting phase=%s signer=%s chain_sequence=%d acc_seq_searched=%d..%d attempts=%d foreign=%d\n",
			phase, who.name, chainSeq, from, chainSeq-1, len(ours), len(foreign))
	}
}

// checkIntent is checks (b) and (c) for one terminal intent: the number of
// committed transactions carrying its object's typed event and the signer
// of the one that does, the commitment evidence against the chain, and the
// API view against both.
func (n *bnet) checkIntent(phase string, r bcIntentRow) {
	n.t.Helper()
	obj, ok := n.objects[r.ID]
	if !ok {
		n.t.Fatalf("phase %s: intent %s was not created through the test's request helpers", phase, r.ID)
	}
	effects := n.objectEffects(obj)
	v := n.view(r.ID)
	switch r.State {
	case "COMMITTED":
		c, ok := n.commitment(r.ID)
		if !ok {
			n.t.Fatalf("phase %s: COMMITTED %s without a commitment row", phase, r.ID)
		}
		if len(effects) != 1 || !bytes.Equal(effects[0].Hash, c.FirstCommit) || effects[0].Height != c.Height {
			n.t.Fatalf("phase %s: %s %s: %d committed transactions carry %s; commitment first commit %X at %d",
				phase, r.ID, r.Kind, len(effects), obj, c.FirstCommit, c.Height)
		}
		// The stored committing signer is the emitting message's signer.
		// A register, withdraw or offer counts only when this intent's own
		// key signed it (rulings R4 and R5); a relay's effect may be
		// committed by the provider's own account (FR1, FR1b).
		if !bytes.Equal(effects[0].Signer, c.CommitSigner) {
			n.t.Fatalf("phase %s: %s committing signer %X, the chain's message signer %X", phase, r.ID, c.CommitSigner, effects[0].Signer)
		}
		switch obj.Event {
		case evRegistered, evWithdrawn, evOffered:
			if !bytes.Equal(effects[0].Signer, r.Signer) {
				n.t.Fatalf("phase %s: COMMITTED %s %s signed by %X, not by the intent's signer %X", phase, r.ID, r.Kind, effects[0].Signer, r.Signer)
			}
		}
		// An idempotent replay's own transaction is on chain with the
		// stored code and is one of this intent's COMMITTED_IDEMPOTENT
		// attempts.
		if !bytes.Equal(c.TxHash, c.FirstCommit) {
			tx, onChain := n.txOnChain(c.TxHash)
			replay := false
			for _, a := range n.attempts(r.ID) {
				replay = replay || (bytes.Equal(a.Hash, c.TxHash) && a.Phase == "COMMITTED_IDEMPOTENT")
			}
			if !c.Idempotent || !onChain || int64(tx.TxResult.Code) != c.Code || !replay {
				n.t.Fatalf("phase %s: %s replay transaction %X: idempotent %t, on chain %t, attempt %t", phase, r.ID, c.TxHash, c.Idempotent, onChain, replay)
			}
		}
		if v.Status != stCommitted || v.Commitment == nil || !bytes.Equal(n.hashOf(v.Commitment.FirstCommitTxHash), c.FirstCommit) ||
			v.Commitment.Height != c.Height || v.Commitment.OwnTransaction != r.Own.Bool {
			n.t.Fatalf("phase %s: API view %s disagrees with the row %s and commitment", phase, v, r)
		}
		h := c.Height
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		res, err := n.d.clients[0].Block(ctx, &h)
		cancel()
		if err != nil || !bytes.Equal(res.BlockID.Hash, c.BlockHash) {
			n.t.Fatalf("phase %s: %s block hash at %d: %v (stored %X)", phase, r.ID, h, err, c.BlockHash)
		}
		ownHash := false
		for _, a := range n.attempts(r.ID) {
			if a.Phase == "COMMITTED_OK" && bytes.Equal(a.Hash, c.FirstCommit) {
				ownHash = true
			}
		}
		if ownHash != r.Own.Bool {
			n.t.Fatalf("phase %s: %s own_transaction %t but its COMMITTED_OK attempt match is %t", phase, r.ID, r.Own.Bool, ownHash)
		}
	case "FAILED":
		if v.Status != stFailed || v.failureReason() != r.Reason {
			n.t.Fatalf("phase %s: API view %s disagrees with the row %s", phase, v, r)
		}
		if r.Reason == "OBJECT_TAKEN" {
			if len(effects) != 1 || bytes.Equal(effects[0].Signer, r.Signer) {
				n.t.Fatalf("phase %s: OBJECT_TAKEN %s: %d effects of %s, want one by another signer", phase, r.ID, len(effects), obj)
			}
		} else if len(effects) != 0 {
			n.t.Fatalf("phase %s: FAILED(%s) %s %s, but %d committed transactions carry %s (first %X by %X)",
				phase, r.Reason, r.ID, r.Kind, len(effects), obj, effects[0].Hash, effects[0].Signer)
		}
		// EXECUTION_FAILED always names the code its committed attempt
		// carries on chain. Other reasons with a code (REJECTED) name a
		// CheckTx refusal, which leaves nothing on chain.
		if r.Reason == "EXECUTION_FAILED" && (!r.FailureCode.Valid || r.FailureCode.Int64 == 0) {
			n.t.Fatalf("phase %s: %s FAILED(EXECUTION_FAILED) without a failure code", phase, r.ID)
		}
		if r.FailureCode.Valid && r.FailureCode.Int64 != 0 {
			found := false
			for _, a := range n.attempts(r.ID) {
				if a.CommittedHeight.Valid && a.CommittedCode.Int64 == r.FailureCode.Int64 {
					tx, ok := n.txOnChain(a.Hash)
					if ok && int64(tx.TxResult.Code) == r.FailureCode.Int64 && tx.TxResult.Codespace == r.FailureSpace {
						found = true
					}
				}
			}
			if !found && r.Reason == "EXECUTION_FAILED" {
				n.t.Fatalf("phase %s: %s FAILED with %s/%d but no committed attempt shows it on chain", phase, r.ID, r.FailureSpace, r.FailureCode.Int64)
			}
		}
	default:
		n.t.Fatalf("phase %s: intent %s is %s after quiesce", phase, r.ID, r.State)
	}
}

// verified and markVerified remember intents whose terminal state was
// checked.
func (n *bnet) verified(id string) bool { return n.objects[id].checked }

func (n *bnet) markVerified(id string) {
	obj := n.objects[id]
	obj.checked = true
	n.objects[id] = obj
}

// leasesOf returns providerID's held leases at height.
func (n *bnet) leasesOf(providerID string, height int64) []*leasesv1.Lease {
	n.t.Helper()
	var res leasesv1.QueryActiveLeasesResponse
	raw := n.d.leaseQuery(n.d.clients[0], activePath, &leasesv1.QueryActiveLeasesRequest{ProviderId: providerID, Page: &typesv1.PageRequest{Limit: 100}}, height)
	if err := proto.Unmarshal(raw, &res); err != nil {
		n.t.Fatal(err)
	}
	if len(res.GetPage().GetNextPageKey()) != 0 {
		n.t.Fatal("more than one page of held leases")
	}
	return res.GetLeases()
}

// printTraces prints one trace line per transitions row written since the
// last call.
func (n *bnet) printTraces(phase string) {
	n.t.Helper()
	rows := n.transitionsAfter(n.lastTransition)
	for _, tr := range rows {
		obs := tr.Observation
		if len(obs) > 240 {
			obs = obs[:240] + "..."
		}
		fmt.Printf("trace phase=%s id=%d intent=%s attempt=%s from=%s to=%s reason=%s worker=%s observation=%s\n",
			phase, tr.ID, tr.Intent, tr.Attempt.String, tr.From, tr.To, tr.Reason, tr.Worker, obs)
		n.lastTransition = tr.ID
	}
}

// forwardedAllHash requires every broadcast the proxy forwarded since
// index from to hash to want.
func (n *bnet) forwardedAllHash(phase string, from int, want []byte) int {
	n.t.Helper()
	txs := n.proxy.forwardedTxs()
	for _, h := range txs[from:] {
		if !bytes.Equal(h[:], want) {
			n.t.Fatalf("phase %s: the proxy forwarded %X, not the attempt's %X", phase, h, want)
		}
	}
	return len(txs) - from
}

// sortedHex lists hashes as sorted hex strings for the log.
func sortedHex(hashes [][]byte) []string {
	var out []string
	for _, h := range hashes {
		out = append(out, hexOf(h))
	}
	sort.Strings(out)
	return out
}

// sha256Of is the transaction hash of raw.
func sha256Of(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}
