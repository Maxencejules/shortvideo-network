package chain

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p"
	cmtversionpb "github.com/cometbft/cometbft/proto/tendermint/version"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	cmtversion "github.com/cometbft/cometbft/version"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

type abciQueryParams struct {
	Path   string            `json:"path"`
	Data   cmtbytes.HexBytes `json:"data"`
	Height int64             `json:"height"`
	Prove  bool              `json:"prove"`
}

// answerQuery returns an abci_query handler that checks the path and
// answers code, value and height.
func answerQuery(t *testing.T, path string, height int64, code uint32, value []byte, check func(data []byte)) func(json.RawMessage) rpcAnswer {
	return func(raw json.RawMessage) rpcAnswer {
		var p abciQueryParams
		decodeParams(t, raw, &p)
		if p.Path != path {
			t.Errorf("query path %q, want %q", p.Path, path)
		}
		if check != nil {
			check(p.Data)
		}
		r := abci.ResponseQuery{Code: code, Value: value, Height: height}
		if code != 0 {
			r.Codespace = sdkerrors.RootCodespace
			r.Log = "refused"
		}
		return rpcAnswer{result: &coretypes.ResultABCIQuery{Response: r}}
	}
}

func notFoundCode() uint32 { return sdkerrors.ErrKeyNotFound.ABCICode() }

// TestStatusChecksChainID: Status returns the latest committed header of a
// node running the configured chain id, and refuses another chain id, a
// catching-up node, and a status without a committed block.
func TestStatusChecksChainID(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	at := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	status := func(network string, height int64, catchingUp bool) {
		n.handle("status", func(json.RawMessage) rpcAnswer {
			return rpcAnswer{result: &coretypes.ResultStatus{NodeInfo: p2p.DefaultNodeInfo{Network: network},
				SyncInfo: coretypes.SyncInfo{LatestBlockHeight: height, LatestBlockTime: at, CatchingUp: catchingUp}}}
		})
	}
	status(testChainID, 42, false)
	st, err := c.Status(context.Background())
	if err != nil || st.Height != 42 || st.ChainID != testChainID || !st.Time.Equal(at) {
		t.Fatalf("status %+v %v", st, err)
	}
	status("shortvideo-dev-ffffffffffffffff", 42, false)
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrChainID) {
		t.Fatalf("another chain id: %v", err)
	}
	status(testChainID, 42, true)
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrCatchingUp) {
		t.Fatalf("catching up: %v", err)
	}
	status(testChainID, 0, false)
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrResponse) {
		t.Fatalf("no committed block: %v", err)
	}
}

// accountValue encodes the Query/Account answer for addr as a node does.
func accountValue(t *testing.T, addr []byte, number, seq uint64) []byte {
	t.Helper()
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	acc := authtypes.NewBaseAccount(addr, nil, number, seq)
	packed, err := codectypes.NewAnyWithValue(acc)
	if err != nil {
		t.Fatal(err)
	}
	value, err := enc.Codec.Marshal(&authtypes.QueryAccountResponse{Account: packed})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestAccountReadHeightBound: Account returns number, sequence and the
// answer's height; an answer below the needed height is a StaleError (the
// node is stale, plan 8.1); an unknown address is ErrNoAccount; an account
// of another address is refused.
func TestAccountReadHeightBound(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	addr := testKey("broadcaster").PubKey().Address()
	value := accountValue(t, addr, 5, 17)
	checkAddr := func(data []byte) {
		var req authtypes.QueryAccountRequest
		if err := gogoproto.Unmarshal(data, &req); err != nil {
			t.Errorf("account request: %v", err)
		}
		if want, _ := c.Bech32(addr); req.Address != want {
			t.Errorf("account request for %q", req.Address)
		}
	}
	n.handle("abci_query", answerQuery(t, pathAccount, 9, 0, value, checkAddr))
	var stale *StaleError
	if _, err := c.Account(context.Background(), addr, 10); !errors.As(err, &stale) || stale.Want != 10 || stale.Got != 9 {
		t.Fatalf("height 9 for needed 10: %v", err)
	}
	acc, err := c.Account(context.Background(), addr, 9)
	if err != nil || acc.Number != 5 || acc.Sequence != 17 || acc.Height != 9 {
		t.Fatalf("account %+v %v", acc, err)
	}
	n.handle("abci_query", answerQuery(t, pathAccount, 9, notFoundCode(), nil, nil))
	if _, err := c.Account(context.Background(), addr, 0); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("unknown address: %v", err)
	}
	n.handle("abci_query", answerQuery(t, pathAccount, 9, 0, accountValue(t, testKey("other").PubKey().Address(), 5, 17), nil))
	if _, err := c.Account(context.Background(), addr, 0); !errors.Is(err, ErrResponse) {
		t.Fatalf("account of another address: %v", err)
	}
	n.handle("abci_query", answerQuery(t, pathAccount, 0, 0, value, nil))
	if _, err := c.Account(context.Background(), addr, 0); !errors.Is(err, ErrResponse) {
		t.Fatalf("answer without a height: %v", err)
	}
	if _, err := c.Account(context.Background(), addr[:19], 0); err == nil {
		t.Fatal("19-byte address accepted")
	}
	if _, err := c.Account(context.Background(), addr, -1); err == nil {
		t.Fatal("negative minimum height accepted")
	}
}

// TestQueriesValidateAndBindHeight: every object query refuses invalid
// ids and page bounds before sending, returns the answer height, applies
// the height bound to not-found answers too, and refuses a record of
// another object.
func TestQueriesValidateAndBindHeight(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	ctx := context.Background()
	if _, _, _, err := c.Content(ctx, "Bad'id", 1, 0); err == nil {
		t.Fatal("content query with an invalid id")
	}
	if _, _, _, err := c.Content(ctx, "c1", 0, 0); err == nil {
		t.Fatal("content query with version 0")
	}
	if _, _, _, err := c.Provider(ctx, "", 0); err == nil {
		t.Fatal("provider query with an empty id")
	}
	if _, _, _, err := c.HeldLease(ctx, "p1", "", 0); err == nil {
		t.Fatal("held lease query with an empty content id")
	}
	if _, _, err := c.Deadlines(ctx, 0, nil, 10, 0); err == nil {
		t.Fatal("deadlines query with bound 0")
	}
	if _, _, err := c.Deadlines(ctx, 1_800_000_000, nil, 101, 0); err == nil {
		t.Fatal("deadlines page above 100")
	}
	if _, _, err := c.Governance(ctx, make([]byte, 65), 10, 0); err == nil {
		t.Fatal("governance page key above 64 bytes")
	}
	if got := n.count("abci_query"); got != 0 {
		t.Fatalf("refused queries sent %d requests", got)
	}

	marshal := func(m proto.Message) []byte {
		b, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	contentResp := marshal(&contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "c1", ContentVersion: 2}})
	n.handle("abci_query", answerQuery(t, pathContent, 30, 0, contentResp, func(data []byte) {
		var req contentv1.QueryContentRequest
		if err := proto.Unmarshal(data, &req); err != nil || req.GetContentId() != "c1" || req.GetContentVersion() != 2 {
			t.Errorf("content request %v %v", &req, err)
		}
	}))
	if _, found, h, err := c.Content(ctx, "c1", 2, 30); err != nil || !found || h != 30 {
		t.Fatalf("content: found %t height %d %v", found, h, err)
	}
	var stale *StaleError
	if _, _, _, err := c.Content(ctx, "c1", 2, 31); !errors.As(err, &stale) {
		t.Fatalf("content stale: %v", err)
	}
	for name, resp := range map[string]*contentv1.QueryContentResponse{
		"another version":         {Content: &contentv1.Content{ContentId: "c1", ContentVersion: 3}},
		"tombstone of another id": {Tombstone: &contentv1.Tombstone{ContentId: "c2", ContentVersion: 2}},
		"no record, no tombstone": {},
	} {
		n.handle("abci_query", answerQuery(t, pathContent, 30, 0, marshal(resp), nil))
		if _, _, _, err := c.Content(ctx, "c1", 2, 0); !errors.Is(err, ErrResponse) {
			t.Fatalf("content %s: %v", name, err)
		}
	}
	n.handle("abci_query", answerQuery(t, pathContent, 30, notFoundCode(), nil, nil))
	if _, found, h, err := c.Content(ctx, "c1", 2, 30); err != nil || found || h != 30 {
		t.Fatalf("content not found: found %t height %d %v", found, h, err)
	}
	if _, _, _, err := c.Content(ctx, "c1", 2, 31); !errors.As(err, &stale) {
		t.Fatalf("a stale not-found answer was accepted: %v", err)
	}
	n.handle("abci_query", answerQuery(t, pathContent, 30, 18, nil, nil))
	var qerr *QueryError
	if _, _, _, err := c.Content(ctx, "c1", 2, 0); !errors.As(err, &qerr) {
		t.Fatalf("rejected query: %v", err)
	}

	n.handle("abci_query", answerQuery(t, pathProvider, 30, 0, marshal(&providersv1.QueryProviderResponse{Provider: &providersv1.Provider{ProviderId: "p2"}}), nil))
	if _, _, _, err := c.Provider(ctx, "p1", 0); !errors.Is(err, ErrResponse) {
		t.Fatalf("provider record of another id: %v", err)
	}
	n.handle("abci_query", answerQuery(t, pathProvider, 30, 0, marshal(&providersv1.QueryProviderResponse{Provider: &providersv1.Provider{ProviderId: "p1"}}), nil))
	if p, found, _, err := c.Provider(ctx, "p1", 0); err != nil || !found || p.GetProviderId() != "p1" {
		t.Fatalf("provider: %v %t %v", p, found, err)
	}

	lease := func(provider, content string) *leasesv1.Lease {
		return &leasesv1.Lease{LeaseId: "l1", ProviderId: provider, ContentId: content}
	}
	n.handle("abci_query", answerQuery(t, pathActiveLeases, 30, 0, marshal(&leasesv1.QueryActiveLeasesResponse{Leases: []*leasesv1.Lease{lease("p1", "c1"), lease("p1", "c1")}}), nil))
	if _, _, _, err := c.HeldLease(ctx, "p1", "c1", 0); !errors.Is(err, ErrResponse) {
		t.Fatalf("two held leases for one pair: %v", err)
	}
	n.handle("abci_query", answerQuery(t, pathActiveLeases, 30, 0, marshal(&leasesv1.QueryActiveLeasesResponse{Leases: []*leasesv1.Lease{lease("p2", "c1")}}), nil))
	if _, _, _, err := c.HeldLease(ctx, "p1", "c1", 0); !errors.Is(err, ErrResponse) {
		t.Fatalf("lease of another pair: %v", err)
	}
	n.handle("abci_query", answerQuery(t, pathActiveLeases, 30, 0, marshal(&leasesv1.QueryActiveLeasesResponse{}), nil))
	if _, found, _, err := c.HeldLease(ctx, "p1", "c1", 0); err != nil || found {
		t.Fatalf("no held lease: %t %v", found, err)
	}

	n.handle("abci_query", answerQuery(t, pathDeadlines, 30, 0, marshal(&leasesv1.QueryDeadlinesResponse{Leases: []*leasesv1.Lease{lease("p1", "c1"), lease("p1", "c2")}}), nil))
	if _, _, err := c.Deadlines(ctx, 1_800_000_000, nil, 1, 0); !errors.Is(err, ErrResponse) {
		t.Fatalf("deadlines page above its limit: %v", err)
	}
	if resp, h, err := c.Deadlines(ctx, 1_800_000_000, nil, 2, 0); err != nil || len(resp.GetLeases()) != 2 || h != 30 {
		t.Fatalf("deadlines: %v %d %v", resp, h, err)
	}

	n.handle("abci_query", answerQuery(t, pathGovernance, 30, 0, marshal(&authorityv1.QueryGovernanceResponse{}), nil))
	if _, _, err := c.Governance(ctx, nil, 10, 0); !errors.Is(err, ErrResponse) {
		t.Fatalf("governance without a state: %v", err)
	}
	n.handle("abci_query", answerQuery(t, pathGovernance, 30, 0, marshal(&authorityv1.QueryGovernanceResponse{State: &authorityv1.AuthorityState{}}), nil))
	if _, h, err := c.Governance(ctx, nil, 10, 30); err != nil || h != 30 {
		t.Fatalf("governance: %d %v", h, err)
	}
}

type txParams struct {
	Hash  []byte `json:"hash"`
	Prove bool   `json:"prove"`
}

// TestCommittedTxChecksHash: CommittedTx accepts a committed result only
// when the returned bytes hash to the requested hash (and the answer names
// that hash); it reports not found only for CometBFT's exact "tx (HASH)
// not found" answer for this hash, and any other error stays an error.
func TestCommittedTxChecksHash(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	key := testKey("broadcaster")
	raw, hash := signed(t, c.Codec, key, acceptMsg(key.PubKey().Address()), stdInput(2))
	otherRaw, otherHash := signed(t, c.Codec, key, acceptMsg(key.PubKey().Address()), stdInput(3))
	answer := func(resHash []byte, tx []byte) {
		n.handle("tx", func(params json.RawMessage) rpcAnswer {
			var p txParams
			decodeParams(t, params, &p)
			if string(p.Hash) != string(hash[:]) {
				t.Errorf("tx request for %X", p.Hash)
			}
			return rpcAnswer{result: &coretypes.ResultTx{Hash: resHash, Height: 12, Index: 1, Tx: tx,
				TxResult: abci.ExecTxResult{Code: 0, GasUsed: 60_000, GasWanted: 2_000_000}}}
		})
	}
	answer(hash[:], raw)
	com, found, err := c.CommittedTx(context.Background(), hash)
	if err != nil || !found || com.Hash != hash || com.Height != 12 || com.Index != 1 || com.GasUsed != 60_000 || string(com.Raw) != string(raw) {
		t.Fatalf("committed %+v %t %v", com, found, err)
	}
	var integrity *IntegrityError
	answer(hash[:], otherRaw)
	if _, _, err := c.CommittedTx(context.Background(), hash); !errors.As(err, &integrity) {
		t.Fatalf("bytes of another transaction: %v", err)
	}
	answer(otherHash[:], otherRaw)
	if _, _, err := c.CommittedTx(context.Background(), hash); !errors.As(err, &integrity) {
		t.Fatalf("answer for another hash: %v", err)
	}
	n.handle("tx", func(json.RawMessage) rpcAnswer {
		return rpcAnswer{err: internalError(fmt.Sprintf("tx (%X) not found", hash[:]))}
	})
	if _, found, err := c.CommittedTx(context.Background(), hash); err != nil || found {
		t.Fatalf("not found: %t %v", found, err)
	}
	n.handle("tx", func(json.RawMessage) rpcAnswer {
		return rpcAnswer{err: internalError(fmt.Sprintf("tx (%X) not found", otherHash[:]))}
	})
	if _, _, err := c.CommittedTx(context.Background(), hash); err == nil {
		t.Fatal("a not-found answer for another hash was taken as not found")
	}
	n.handle("tx", func(json.RawMessage) rpcAnswer {
		return rpcAnswer{err: internalError("transaction indexing is disabled")}
	})
	if _, _, err := c.CommittedTx(context.Background(), hash); err == nil {
		t.Fatal("indexing disabled was taken as not found")
	}
}

type searchParams struct {
	Query   string `json:"query"`
	Prove   bool   `json:"prove"`
	Page    *int   `json:"page"`
	PerPage *int   `json:"per_page"`
	OrderBy string `json:"order_by"`
}

// searchEntry wraps raw as one tx_search entry.
func searchEntry(raw []byte, events ...abci.Event) *coretypes.ResultTx {
	sum := sha256.Sum256(raw)
	return &coretypes.ResultTx{Hash: sum[:], Height: 20, Tx: raw, TxResult: abci.ExecTxResult{Events: events}}
}

// TestTxSearchQueries: the acc_seq and typed event query strings are
// exact, single-condition, and below CometBFT's 512-byte limit; an object
// id outside the protocol pattern (a quote) is refused. TxSearch asks for
// pages of 100 in ascending order, reads every page up to the total, never
// returns a partial list (ErrSearchIncomplete), and checks every entry's
// bytes against its hash.
func TestTxSearchQueries(t *testing.T) {
	c := testCodec(t)
	signer := testKey("broadcaster").PubKey().Address()
	bech, err := c.Bech32(signer)
	if err != nil {
		t.Fatal(err)
	}
	q, err := c.AccSeqQuery(signer, 7)
	if err != nil || q != "tx.acc_seq='"+bech+"/7'" {
		t.Fatalf("acc_seq query %q %v", q, err)
	}
	if _, err := c.AccSeqQuery(signer[:19], 7); err == nil {
		t.Fatal("acc_seq query for a 19-byte address")
	}
	want := map[string]string{
		"register": "svn.events.v1.ContentRegistered.content_id='c1'",
		"withdraw": "svn.events.v1.ContentWithdrawn.content_id='c1'",
		"offer":    "svn.events.v1.LeaseOffered.offer_id='o1'",
		"accept":   "svn.events.v1.LeaseAccepted.lease_id='l1'",
		"renew":    "svn.events.v1.LeaseRenewed.lease_id='l1'",
		"end":      "svn.events.v1.LeaseEnded.lease_id='l1'",
	}
	for _, tc := range sixMsgs(signer) {
		key, err := ObjectOf(tc.msg)
		if err != nil {
			t.Fatal(err)
		}
		if got := key.Query(); got != want[tc.name] || len(got) > maxQueryBytes {
			t.Fatalf("%s query %q", tc.name, got)
		}
	}
	long := offerMsg(signer)
	long.OfferId = strings.Repeat("o", 64)
	if key, err := ObjectOf(long); err != nil || len(key.Query()) > maxQueryBytes {
		t.Fatalf("longest id query: %v", err)
	}
	quoted := offerMsg(signer)
	quoted.OfferId = "o1' OR tx.height>0 AND x='"
	if _, err := ObjectOf(quoted); err == nil {
		t.Fatal("object id with a quote accepted")
	}

	n := newFakeNode(t)
	cl := n.client(t)
	key := testKey("service")
	var entries []*coretypes.ResultTx
	for i := 0; i < 150; i++ {
		raw, _ := signed(t, cl.Codec, key, offerMsg(key.PubKey().Address()), stdInput(uint64(i)))
		entries = append(entries, searchEntry(raw))
	}
	var pages []int
	n.handle("tx_search", func(params json.RawMessage) rpcAnswer {
		var p searchParams
		decodeParams(t, params, &p)
		if p.Query != want["offer"] || p.PerPage == nil || *p.PerPage != 100 || p.OrderBy != "asc" || p.Prove || p.Page == nil {
			t.Errorf("tx_search params %+v", p)
			return rpcAnswer{err: internalError("bad params")}
		}
		pages = append(pages, *p.Page)
		start := (*p.Page - 1) * 100
		end := min(start+100, len(entries))
		return rpcAnswer{result: &coretypes.ResultTxSearch{Txs: entries[start:end], TotalCount: len(entries)}}
	})
	got, err := cl.EffectCandidates(context.Background(), offerMsg(key.PubKey().Address()), 2)
	if err != nil || len(got) != 150 || fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("search: %d results, pages %v, %v", len(got), pages, err)
	}
	for i, com := range got {
		if com.Hash != sha256.Sum256(entries[i].Tx) {
			t.Fatalf("result %d out of order", i)
		}
	}
	if _, err := cl.TxSearch(context.Background(), want["offer"], 1); !errors.Is(err, ErrSearchIncomplete) {
		t.Fatalf("budget below the total: %v", err)
	}
	if _, err := cl.TxSearch(context.Background(), "", 1); err == nil {
		t.Fatal("empty query accepted")
	}
	if _, err := cl.TxSearch(context.Background(), strings.Repeat("q", maxQueryBytes+1), 1); err == nil {
		t.Fatal("oversize query accepted")
	}
	if _, err := cl.TxSearch(context.Background(), want["offer"], MaxSearchPageBudget+1); err == nil {
		t.Fatal("page budget above the bound accepted")
	}
	bad := *entries[0]
	bad.Tx = entries[1].Tx
	entries = []*coretypes.ResultTx{&bad}
	var integrity *IntegrityError
	if _, err := cl.TxSearch(context.Background(), want["offer"], 1); !errors.As(err, &integrity) {
		t.Fatalf("entry whose bytes do not hash to its hash: %v", err)
	}
	entries = []*coretypes.ResultTx{entries[0], entries[0]}
	entries[0] = searchEntry(entries[0].Tx)
	entries[1] = entries[0]
	if _, err := cl.TxSearch(context.Background(), want["offer"], 1); !errors.Is(err, ErrResponse) {
		t.Fatalf("repeated entry: %v", err)
	}
}

// TestSequenceUser: the tx_search hit of tx.acc_seq='<signer>/<seq>' is
// returned only if its acc_seq attribute names exactly that signer and
// sequence; no hit is not found; two hits are an IntegrityError.
func TestSequenceUser(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	key := testKey("broadcaster")
	signer := key.PubKey().Address()
	raw, hash := signed(t, c.Codec, key, acceptMsg(signer), stdInput(7))
	otherRaw, _ := signed(t, c.Codec, key, renewMsg(signer), stdInput(7))
	query, err := c.AccSeqQuery(signer, 7)
	if err != nil {
		t.Fatal(err)
	}
	var hits []*coretypes.ResultTx
	n.handle("tx_search", func(params json.RawMessage) rpcAnswer {
		var p searchParams
		decodeParams(t, params, &p)
		if p.Query != query {
			t.Errorf("query %q", p.Query)
		}
		return rpcAnswer{result: &coretypes.ResultTxSearch{Txs: hits, TotalCount: len(hits)}}
	})
	if _, found, err := c.SequenceUser(context.Background(), signer, 7); err != nil || found {
		t.Fatalf("no hit: %t %v", found, err)
	}
	hits = []*coretypes.ResultTx{searchEntry(raw, accSeqEvent(t, c.Codec, signer, 7))}
	com, found, err := c.SequenceUser(context.Background(), signer, 7)
	if err != nil || !found || com.Hash != hash {
		t.Fatalf("one hit: %t %v", found, err)
	}
	var integrity *IntegrityError
	hits = []*coretypes.ResultTx{searchEntry(raw, accSeqEvent(t, c.Codec, signer, 8))}
	if _, _, err := c.SequenceUser(context.Background(), signer, 7); !errors.As(err, &integrity) {
		t.Fatalf("hit with another sequence: %v", err)
	}
	hits = []*coretypes.ResultTx{searchEntry(raw)}
	if _, _, err := c.SequenceUser(context.Background(), signer, 7); !errors.As(err, &integrity) {
		t.Fatalf("hit without acc_seq: %v", err)
	}
	hits = []*coretypes.ResultTx{searchEntry(raw, accSeqEvent(t, c.Codec, signer, 7)), searchEntry(otherRaw, accSeqEvent(t, c.Codec, signer, 7))}
	if _, _, err := c.SequenceUser(context.Background(), signer, 7); !errors.As(err, &integrity) {
		t.Fatalf("two hits: %v", err)
	}
}

// signedHeader returns a signed header and commit for height that pass
// SignedHeader.ValidateBasic for chainID. The commit signature is filler:
// ValidateBasic checks shape, not validator signatures.
func signedHeader(chainID string, height int64) cmttypes.SignedHeader {
	h32 := func(s string) []byte { sum := sha256.Sum256([]byte(s)); return sum[:] }
	header := &cmttypes.Header{
		Version: cmtversionpb.Consensus{Block: cmtversion.BlockProtocol, App: 1}, ChainID: chainID, Height: height,
		Time:           time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
		LastBlockID:    cmttypes.BlockID{Hash: h32("last"), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: h32("last parts")}},
		LastCommitHash: h32("lc"), DataHash: h32("data"), ValidatorsHash: h32("vals"), NextValidatorsHash: h32("vals"),
		ConsensusHash: h32("cons"), AppHash: h32("app"), LastResultsHash: h32("res"), EvidenceHash: h32("ev"),
		ProposerAddress: h32("proposer")[:20],
	}
	commit := &cmttypes.Commit{Height: height, BlockID: cmttypes.BlockID{Hash: header.Hash(), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: h32("parts")}},
		Signatures: []cmttypes.CommitSig{{BlockIDFlag: cmttypes.BlockIDFlagCommit, ValidatorAddress: h32("proposer")[:20], Timestamp: header.Time, Signature: h32("sig")}}}
	return cmttypes.SignedHeader{Header: header, Commit: commit}
}

// TestCommitBlockHash: Commit returns the block hash, time and app hash
// of the requested height when the signed header validates for the
// configured chain id, and refuses another chain id, another height, and a
// commit for another block.
func TestCommitBlockHash(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	answer := func(sh cmttypes.SignedHeader) {
		n.handle("commit", func(json.RawMessage) rpcAnswer {
			return rpcAnswer{result: &coretypes.ResultCommit{SignedHeader: sh, CanonicalCommit: true}}
		})
	}
	good := signedHeader(testChainID, 12)
	answer(good)
	id, err := c.Commit(context.Background(), 12)
	if err != nil || id.Height != 12 || string(id.Hash) != string(good.Header.Hash()) || string(id.AppHash) != string(good.Header.AppHash) {
		t.Fatalf("commit %+v %v", id, err)
	}
	answer(signedHeader("shortvideo-dev-ffffffffffffffff", 12))
	if _, err := c.Commit(context.Background(), 12); !errors.Is(err, ErrResponse) {
		t.Fatalf("another chain id: %v", err)
	}
	answer(signedHeader(testChainID, 13))
	if _, err := c.Commit(context.Background(), 12); !errors.Is(err, ErrResponse) {
		t.Fatalf("another height: %v", err)
	}
	other := signedHeader(testChainID, 12)
	other.Commit.BlockID.Hash = signedHeader(testChainID, 11).Header.Hash()
	answer(other)
	if _, err := c.Commit(context.Background(), 12); !errors.Is(err, ErrResponse) {
		t.Fatalf("commit for another block: %v", err)
	}
	if _, err := c.Commit(context.Background(), 0); err == nil {
		t.Fatal("height 0 accepted")
	}
}

// txServiceSimulate is the route ruling R3 fixes for gas simulation: the
// SDK transaction service's Simulate method, never /app/simulate.
const txServiceSimulate = "/cosmos.tx.v1beta1.Service/Simulate"

// TestSimulateUsesTxService: Simulate sends the transaction bytes to the
// SDK transaction service's Simulate method as a gRPC-routed ABCI query
// and reads gas used from the protobuf answer, encoded as the node's query
// router encodes it (with an svn message response inside). A rejected
// simulation is a QueryError; an answer without gas used is refused.
func TestSimulateUsesTxService(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	key := testKey("service")
	raw, err := c.SimulationTx(withdrawMsg(key.PubKey().Address()), key.PubKey(), 4, 150)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	response, err := codectypes.NewAnyWithValue(&contentv1.MsgWithdrawContentResponse{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := enc.Codec.Marshal(&txtypes.SimulateResponse{GasInfo: &sdk.GasInfo{GasUsed: 71_234},
		Result: &sdk.Result{MsgResponses: []*codectypes.Any{response}}})
	if err != nil {
		t.Fatal(err)
	}
	checkReq := func(data []byte) {
		var req txtypes.SimulateRequest
		if err := gogoproto.Unmarshal(data, &req); err != nil || string(req.TxBytes) != string(raw) {
			t.Errorf("simulate request: %v", err)
		}
	}
	n.handle("abci_query", answerQuery(t, txServiceSimulate, 40, 0, value, checkReq))
	res, err := c.Simulate(context.Background(), raw)
	if err != nil || res.GasUsed != 71_234 || res.Height != 40 {
		t.Fatalf("simulate %+v %v", res, err)
	}
	n.handle("abci_query", answerQuery(t, txServiceSimulate, 40, 111222, nil, nil))
	var qerr *QueryError
	if _, err := c.Simulate(context.Background(), raw); !errors.As(err, &qerr) {
		t.Fatalf("rejected simulation: %v", err)
	}
	empty, err := enc.Codec.Marshal(&txtypes.SimulateResponse{})
	if err != nil {
		t.Fatal(err)
	}
	n.handle("abci_query", answerQuery(t, txServiceSimulate, 40, 0, empty, nil))
	if _, err := c.Simulate(context.Background(), raw); !errors.Is(err, ErrResponse) {
		t.Fatalf("answer without gas: %v", err)
	}
	if _, err := c.Simulate(context.Background(), nil); err == nil {
		t.Fatal("empty simulation accepted")
	}
}

// TestAppSimulateJSONCannotCarrySvnResponses records why Simulate does not
// use the /app/simulate path: that path JSON-encodes its answer with
// codec.ProtoMarshalJSON and the application's interface registry (SDK
// baseapp handleQueryApp), and the registry built by app.MakeEncoding does
// not resolve an svn message response, so a successful svn message
// simulation cannot be encoded there. If the application starts
// registering its message responses, this test fails and the choice can
// be revisited. Pending the orchestrator's ruling on goal 11 Wave 0 B2
// deviation 1 and B4 F1 (plan 8.2 and DR-11-27 name /app/simulate):
// evidence lists this test as a recorded deviation, not as a
// plan-conformant check.
func TestAppSimulateJSONCannotCarrySvnResponses(t *testing.T) {
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	response, err := codectypes.NewAnyWithValue(&contentv1.MsgWithdrawContentResponse{})
	if err != nil {
		t.Fatal(err)
	}
	sim := &sdk.SimulationResponse{GasInfo: sdk.GasInfo{GasUsed: 1}, Result: &sdk.Result{MsgResponses: []*codectypes.Any{response}}}
	if _, err := codec.ProtoMarshalJSON(sim, enc.InterfaceRegistry); err == nil || !strings.Contains(err.Error(), "unable to resolve type URL") {
		t.Fatalf("ProtoMarshalJSON with the app registry: %v", err)
	}
}

// TestHTTPClientBounds: NewHTTP accepts only an http or https base URL; the
// response body bound is an error, not a truncation; redirects are not
// followed.
func TestHTTPClientBounds(t *testing.T) {
	for _, bad := range []string{"", "ftp://127.0.0.1:26657", "http://", "http://u:p@127.0.0.1:26657", "http://127.0.0.1:26657/rpc",
		"http://127.0.0.1:26657?x=1", "http://127.0.0.1:26657#f"} {
		if _, err := NewHTTP(bad, time.Second); err == nil {
			t.Fatalf("NewHTTP accepted %q", bad)
		}
	}
	if _, err := NewHTTP("http://127.0.0.1:26657", 0); err == nil {
		t.Fatal("NewHTTP accepted a zero timeout")
	}
	read := func(body string, left int64) error {
		b := &limitedBody{ReadCloser: io.NopCloser(strings.NewReader(body)), left: left}
		_, err := io.ReadAll(b)
		return err
	}
	if err := read("0123456789", 10); err != nil {
		t.Fatalf("body at the bound: %v", err)
	}
	if err := read("0123456789x", 10); !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("body above the bound: %v", err)
	}
	target := newFakeNode(t)
	target.handle("status", func(json.RawMessage) rpcAnswer {
		t.Error("the redirect was followed")
		return rpcAnswer{err: internalError("followed")}
	})
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.srv.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	rpc, err := NewHTTP(redirect.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(rpc, testCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("status through a redirect succeeded")
	}
	if target.count("status") != 0 {
		t.Fatal("the redirect target was called")
	}
	if _, err := New(nil, testCodec(t)); err == nil {
		t.Fatal("New accepted a nil rpc")
	}
}
