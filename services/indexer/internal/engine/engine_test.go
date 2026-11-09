package engine

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/pgtest"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

func newRuntime(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	cluster, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cluster.Close() })
	ownerPass, err := cluster.Cluster.CreateLoginRole(ctx, store.OwnerRole)
	if err != nil {
		t.Fatal(err)
	}
	runtimePass, err := cluster.Cluster.CreateLoginRole(ctx, store.RuntimeRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := cluster.NewDatabase(ctx, store.Database, store.OwnerRole, store.RuntimeRole); err != nil {
		t.Fatal(err)
	}
	owner, err := store.Open(store.Conn{
		SocketDir: cluster.Cluster.SocketDir(), Port: 5432, Database: store.Database,
		User: store.OwnerRole, Password: ownerPass, ApplicationName: "owner", ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if err := store.Migrate(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if err := store.Bind(ctx, owner, chainID); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(store.Conn{
		SocketDir: cluster.Cluster.SocketDir(), Port: 5432, Database: store.Database,
		User: store.RuntimeRole, Password: runtimePass, ApplicationName: "runtime", ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// The engine tests use the store harness by constructing a database in this
// package. A fake chain records which heights were fetched.

type fakeChain struct {
	height  int64
	blocks  map[int64]project.Block
	missing map[int64]bool
	got     []int64
}

func (f *fakeChain) Status(context.Context) (chain.Status, error) {
	return chain.Status{ChainID: chainID, Height: f.height, Time: time.Unix(1_700_000_100, 0).UTC()}, nil
}

func (f *fakeChain) Block(_ context.Context, height int64) (project.Block, error) {
	f.got = append(f.got, height)
	if f.missing[height] {
		return project.Block{}, chain.ErrHistoryUnavailable
	}
	block, ok := f.blocks[height]
	if !ok {
		return project.Block{}, chain.ErrHistoryUnavailable
	}
	return block, nil
}

func (f *fakeChain) Providers(context.Context, int64) ([]chain.ProviderView, int64, error) {
	return nil, f.height, nil
}

func (f *fakeChain) Content(context.Context, string, uint64, int64) (chain.ContentView, bool, int64, error) {
	return chain.ContentView{}, false, f.height, nil
}

func (f *fakeChain) ActiveLeases(context.Context, string, int64) ([]chain.LeaseView, int64, error) {
	return nil, f.height, nil
}

func (f *fakeChain) Deadlines(context.Context, int64) ([]chain.DeadlineView, int64, error) {
	return nil, f.height, nil
}

func (f *fakeChain) PolicyVersion(context.Context, int64) (uint64, int64, error) {
	return 1, f.height, nil
}

const chainID = "shortvideo-dev-engine"

func TestDoesNotIndexAboveTheFloor(t *testing.T) {
	db := newRuntime(t)
	f := &fakeChain{height: 5, blocks: map[int64]project.Block{
		1: empty(1), 2: empty(2), 3: empty(3), 4: withContent(4), 5: withContent(5),
	}}
	eng := &Engine{Chain: f, Store: store.New(db, chainID), ChainID: chainID}
	result, err := eng.CatchUp(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Floor != 4 || result.Indexed != 4 {
		t.Fatalf("result %+v fetched %v", result, f.got)
	}
	for _, h := range f.got {
		if h > 4 {
			t.Fatalf("indexed uncommitted height %d (fetched %v)", h, f.got)
		}
	}
	n, err := store.New(db, chainID).ContentCount(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("content %d %v", n, err)
	}
}

func TestMissingHeightStopsThePass(t *testing.T) {
	db := newRuntime(t)
	f := &fakeChain{height: 5, missing: map[int64]bool{2: true}, blocks: map[int64]project.Block{
		1: empty(1), 3: withContent(3),
	}}
	eng := &Engine{Chain: f, Store: store.New(db, chainID), ChainID: chainID}
	_, err := eng.CatchUp(context.Background())
	if !errors.Is(err, chain.ErrHistoryUnavailable) {
		t.Fatalf("missing: %v", err)
	}
	n, err := store.New(db, chainID).ContentCount(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("missing range was treated as history that contains content: %d %v", n, err)
	}
	cp, err := store.New(db, chainID).Checkpoint(context.Background())
	if err != nil || !cp.Found || cp.Height != 1 {
		t.Fatalf("checkpoint %+v %v", cp, err)
	}
}

func TestBadVersionDoesNotAdvance(t *testing.T) {
	db := newRuntime(t)
	block := empty(1)
	block.Finalize = []abci.Event{{Type: "svn.events.v9.ContentRegistered"}}
	f := &fakeChain{height: 3, blocks: map[int64]project.Block{1: block}}
	eng := &Engine{Chain: f, Store: store.New(db, chainID), ChainID: chainID}
	_, err := eng.CatchUp(context.Background())
	if !errors.Is(err, project.ErrEventVersion) {
		t.Fatalf("version: %v", err)
	}
	cp, err := store.New(db, chainID).Checkpoint(context.Background())
	if err != nil || cp.Found {
		t.Fatalf("checkpoint advanced on a bad version: %+v %v", cp, err)
	}
}

func empty(h int64) project.Block {
	hash := bytes.Repeat([]byte{byte(h)}, 32)
	app := bytes.Repeat([]byte{9}, 32)
	return project.Block{ChainID: chainID, Height: h, Hash: hash, AppHash: app, TimeUnix: 1_700_000_000}
}

func withContent(h int64) project.Block {
	block := empty(h)
	block.Txs = []project.Tx{{
		Hash: block.Hash, Code: 0,
		Events: []abci.Event{{Type: "svn.events.v1.ContentRegistered", Attributes: []abci.EventAttribute{
			{Key: "content_id", Value: "c1"},
			{Key: "content_version", Value: "1"},
			{Key: "manifest_sha256", Value: bytesHex()},
			{Key: "package_bytes", Value: "4"},
			{Key: "msg_index", Value: "0"},
		}}},
	}}
	return block
}

func bytesHex() string {
	return "abababababababababababababababababababababababababababababababab"
}
