package project

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
)

func manifest() string { return strings.Repeat("ab", 32) }

func registered() abci.Event {
	return abci.Event{Type: "svn.events.v1.ContentRegistered", Attributes: []abci.EventAttribute{
		{Key: "content_id", Value: "c1"},
		{Key: "content_version", Value: "1"},
		{Key: "manifest_sha256", Value: manifest()},
		{Key: "package_bytes", Value: "4000000"},
		{Key: "msg_index", Value: "0"},
	}}
}

func TestDecodeVersionAndFailure(t *testing.T) {
	txHash := bytes.Repeat([]byte{4}, 32)
	block := Block{ChainID: "shortvideo-dev-t", Height: 4, Hash: txHash, AppHash: txHash, Txs: []Tx{{
		Hash: txHash, Code: 0, Events: []abci.Event{registered()},
	}}}
	effects, err := Decode(block)
	if err != nil || len(effects) != 1 || effects[0].Content == nil || effects[0].Content.ID != "c1" {
		t.Fatalf("decode: %v %+v", err, effects)
	}
	if effects[0].Content.Withdrawn {
		t.Fatal("registration is not a publication approval and not a withdrawal")
	}

	bad := block
	bad.Txs[0].Events = []abci.Event{{Type: "svn.events.v2.ContentRegistered", Attributes: registered().Attributes}}
	if _, err := Decode(bad); !errors.Is(err, ErrEventVersion) {
		t.Fatalf("v2: %v", err)
	}
	bad.Txs[0].Events = []abci.Event{{Type: "svn.events.v10.ContentRegistered"}}
	if _, err := Decode(bad); !errors.Is(err, ErrEventVersion) {
		t.Fatalf("v10: %v", err)
	}
	broken := registered()
	broken.Attributes = broken.Attributes[:2]
	bad.Txs[0].Events = []abci.Event{broken}
	if _, err := Decode(bad); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed: %v", err)
	}

	failed := block
	failed.Txs[0].Code = 8
	effects, err = Decode(failed)
	if err != nil || len(effects) != 0 {
		t.Fatalf("failed tx must not project: %v %d", err, len(effects))
	}
}

func TestRenewRequiresACommittedEvent(t *testing.T) {
	ev := abci.Event{Type: "svn.events.v1.LeaseRenewed", Attributes: []abci.EventAttribute{
		{Key: "lease_id", Value: "l1"},
		{Key: "term_version", Value: "2"},
		{Key: "deadline_unix_seconds", Value: "100"},
		{Key: "msg_index", Value: "0"},
	}}
	effects, err := Decode(Block{Height: 2, Txs: []Tx{{Hash: bytes.Repeat([]byte{1}, 32), Events: []abci.Event{ev}}}})
	if err != nil || len(effects) != 1 || effects[0].Lease == nil || !effects[0].Lease.RenewOnly || effects[0].Lease.TermVersion != 2 {
		t.Fatalf("renew: %v %+v", err, effects)
	}
}

func TestOfferClosed(t *testing.T) {
	ev := abci.Event{Type: "leases_offer_closed", Attributes: []abci.EventAttribute{
		{Key: "offer_id", Value: "o1"},
		{Key: "reason", Value: "EXPIRED"},
		{Key: "mode", Value: "BeginBlock"},
	}}
	effects, err := Decode(Block{Height: 2, Finalize: []abci.Event{ev}})
	if err != nil || len(effects) != 1 || effects[0].Offer == nil || !effects[0].Offer.CloseOnly {
		t.Fatalf("offer closed: %v %+v", err, effects)
	}
	ev.Attributes[2].Value = "nope"
	if _, err := Decode(Block{Height: 2, Finalize: []abci.Event{ev}}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("bad mode: %v", err)
	}
}
