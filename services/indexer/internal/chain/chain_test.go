package chain

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
)

func TestAppFloor(t *testing.T) {
	if (Status{Height: 5}).AppFloor() != 4 {
		t.Fatalf("floor of 5 = %d, want 4", (Status{Height: 5}).AppFloor())
	}
	if (Status{Height: 1}).AppFloor() != 1 {
		t.Fatalf("floor of 1 = %d, want 1", (Status{Height: 1}).AppFloor())
	}
	if floorMinus != 1 {
		t.Fatalf("floorMinus = %d, want 1", floorMinus)
	}
}

func TestVerifyRejectsMismatch(t *testing.T) {
	hash := bytes.Repeat([]byte{1}, 32)
	other := bytes.Repeat([]byte{2}, 32)
	ok := Header{ChainID: "shortvideo-dev-t", Height: 3, Hash: hash, AppHash: hash, CommitHash: hash}
	res := Results{Height: 3, AppHash: hash}
	if err := verify("shortvideo-dev-t", 3, ok, res); err != nil {
		t.Fatal(err)
	}
	// The header app hash is the previous state. A different results app
	// hash is the state after the block, and it is accepted.
	if err := verify("shortvideo-dev-t", 3, ok, Results{Height: 3, AppHash: other}); err != nil {
		t.Fatal(err)
	}
	if err := verify("shortvideo-dev-t", 3, Header{ChainID: "other", Height: 3, Hash: hash, AppHash: hash, CommitHash: hash}, res); !errors.Is(err, ErrChainID) {
		t.Fatalf("chain id: %v", err)
	}
	if err := verify("shortvideo-dev-t", 3, Header{ChainID: "shortvideo-dev-t", Height: 3, Hash: hash, AppHash: hash, CommitHash: other}, res); !errors.Is(err, ErrForged) {
		t.Fatalf("commit hash: %v", err)
	}
	if err := verify("shortvideo-dev-t", 3, ok, Results{Height: 4, AppHash: hash}); !errors.Is(err, ErrForged) {
		t.Fatalf("results height: %v", err)
	}
	if err := verify("shortvideo-dev-t", 3, ok, Results{Height: 3, AppHash: []byte{1}}); !errors.Is(err, ErrForged) {
		t.Fatalf("short app hash: %v", err)
	}
}

type fakeReader struct {
	status  Status
	headers map[int64]Header
	results map[int64]Results
	missing map[int64]bool
}

func (f fakeReader) Status(context.Context) (Status, error) { return f.status, nil }

func (f fakeReader) Header(_ context.Context, height int64) (Header, error) {
	if f.missing[height] {
		return Header{}, ErrHistoryUnavailable
	}
	h, ok := f.headers[height]
	if !ok {
		return Header{}, ErrHistoryUnavailable
	}
	return h, nil
}

func (f fakeReader) Results(_ context.Context, height int64) (Results, error) {
	if f.missing[height] {
		return Results{}, ErrHistoryUnavailable
	}
	r, ok := f.results[height]
	if !ok {
		return Results{}, ErrHistoryUnavailable
	}
	return r, nil
}

func (f fakeReader) Query(context.Context, string, []byte, int64) (QueryResult, error) {
	return QueryResult{}, ErrQuery
}

func TestBlockMissingIsNotEmpty(t *testing.T) {
	hash := bytes.Repeat([]byte{7}, 32)
	raw := fakeReader{
		status:  Status{ChainID: "shortvideo-dev-t", Height: 4, Time: time.Unix(10, 0)},
		missing: map[int64]bool{2: true},
		headers: map[int64]Header{1: {ChainID: "shortvideo-dev-t", Height: 1, Hash: hash, AppHash: hash, CommitHash: hash}},
		results: map[int64]Results{1: {Height: 1, AppHash: hash}},
	}
	c, err := New("shortvideo-dev-t", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Block(context.Background(), 2); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("missing block: %v", err)
	}
	block, err := c.Block(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != 1 || len(block.Txs) != 0 {
		t.Fatalf("empty committed block %+v", block)
	}
}

func TestStatusChainID(t *testing.T) {
	c, err := New("shortvideo-dev-t", fakeReader{status: Status{ChainID: "other", Height: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrChainID) {
		t.Fatalf("status: %v", err)
	}
}

func TestForgedEventShapeStaysOnTheBlock(t *testing.T) {
	// A results payload that parses as events is still a block. Version checks
	// happen in project.Decode. This test keeps a forged hash out of that path.
	hash := bytes.Repeat([]byte{3}, 32)
	raw := fakeReader{
		headers: map[int64]Header{1: {ChainID: "shortvideo-dev-t", Height: 1, Hash: hash, AppHash: hash, CommitHash: bytes.Repeat([]byte{9}, 32)}},
		results: map[int64]Results{1: {Height: 1, AppHash: hash, Finalize: []abci.Event{{Type: "svn.events.v2.ContentRegistered"}}}},
	}
	c, _ := New("shortvideo-dev-t", raw)
	_, err := c.Block(context.Background(), 1)
	if !errors.Is(err, ErrForged) {
		t.Fatalf("forged commit: %v", err)
	}
	_ = project.DecoderVersion
}
