package node

import (
	"bytes"
	"errors"
	"reflect"
	"sync"
	"testing"

	cmtdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/store"
)

func openLevel(t *testing.T) cmtdb.DB {
	t.Helper()
	db, err := cmtdb.NewDB("test", cmtdb.GoLevelDBBackend, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// TestStartUsesCloseSafeDBProvider: Start opens the CometBFT databases
// through closeSafeDBProvider, not cmtcfg.DefaultDBProvider.
func TestStartUsesCloseSafeDBProvider(t *testing.T) {
	if reflect.ValueOf(dbProvider).Pointer() != reflect.ValueOf(closeSafeDBProvider).Pointer() {
		t.Fatal("dbProvider is not closeSafeDBProvider")
	}
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot(t.TempDir())
	cfg.DBBackend = string(cmtdb.GoLevelDBBackend)
	db, err := dbProvider(&cmtcfg.DBContext{ID: "blockstore", Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := db.(*closeSafeDB); !ok {
		t.Fatalf("provider returned %T, want *closeSafeDB", db)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCloseSafeDBBeforeClose: before Close every call reaches the wrapped
// database unchanged.
func TestCloseSafeDBBeforeClose(t *testing.T) {
	db := &closeSafeDB{DB: openLevel(t)}
	defer db.Close()
	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSync([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Get([]byte("a")); err != nil || !bytes.Equal(v, []byte("1")) {
		t.Fatalf("Get a = %q, %v", v, err)
	}
	if ok, err := db.Has([]byte("b")); err != nil || !ok {
		t.Fatalf("Has b = %v, %v", ok, err)
	}
	keys := func(it cmtdb.Iterator, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer it.Close()
		var out []string
		for ; it.Valid(); it.Next() {
			out = append(out, string(it.Key()))
		}
		return out
	}
	if got := keys(db.Iterator(nil, nil)); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Iterator = %v", got)
	}
	if got := keys(db.ReverseIterator(nil, nil)); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("ReverseIterator = %v", got)
	}
	if err := db.Delete([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSync([]byte("b")); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Get([]byte("a")); err != nil || v != nil {
		t.Fatalf("Get after Delete = %q, %v", v, err)
	}
}

// TestCloseSafeDBAfterClose: after Close, reads report not found or an empty
// range with no error, and every write fails with errDBClosed.
func TestCloseSafeDBAfterClose(t *testing.T) {
	db := &closeSafeDB{DB: openLevel(t)}
	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Get([]byte("a")); err != nil || v != nil {
		t.Fatalf("Get after Close = %q, %v; want nil, nil", v, err)
	}
	if ok, err := db.Has([]byte("a")); err != nil || ok {
		t.Fatalf("Has after Close = %v, %v; want false, nil", ok, err)
	}
	for name, open := range map[string]func([]byte, []byte) (cmtdb.Iterator, error){
		"Iterator":        db.Iterator,
		"ReverseIterator": db.ReverseIterator,
	} {
		it, err := open([]byte("a"), []byte("z"))
		if err != nil || it.Valid() || it.Error() != nil {
			t.Fatalf("%s after Close: valid=%v err=%v iterErr=%v", name, it != nil && it.Valid(), err, it.Error())
		}
		if s, e := it.Domain(); string(s) != "a" || string(e) != "z" {
			t.Fatalf("%s domain = %q..%q", name, s, e)
		}
		if err := it.Close(); err != nil {
			t.Fatal(err)
		}
	}
	writes := map[string]func() error{
		"Set":        func() error { return db.Set([]byte("b"), []byte("2")) },
		"SetSync":    func() error { return db.SetSync([]byte("b"), []byte("2")) },
		"Delete":     func() error { return db.Delete([]byte("a")) },
		"DeleteSync": func() error { return db.DeleteSync([]byte("a")) },
	}
	for name, write := range writes {
		if err := write(); !errors.Is(err, errDBClosed) {
			t.Fatalf("%s after Close = %v, want errDBClosed", name, err)
		}
	}
}

// TestBlockStoreReadAfterClose reproduces the shutdown panic and shows the
// wrapper removes it. CometBFT's BlockStore.LoadSeenCommit panics on any
// database error; consensus.(*Reactor).queryMaj23Routine can call it after
// node.OnStop closed the block store. With the raw database that read panics
// ("leveldb: closed"); through closeSafeDB it returns no commit.
func TestBlockStoreReadAfterClose(t *testing.T) {
	raw := store.NewBlockStore(openLevel(t))
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("LoadSeenCommit on a closed raw block store did not panic; the reproduction no longer holds")
			}
		}()
		_ = raw.LoadSeenCommit(5)
	}()

	safe := store.NewBlockStore(&closeSafeDB{DB: openLevel(t)})
	if err := safe.Close(); err != nil {
		t.Fatal(err)
	}
	if c := safe.LoadSeenCommit(5); c != nil {
		t.Fatalf("LoadSeenCommit after Close = %v, want nil", c)
	}
	if c := safe.LoadBlockCommit(5); c != nil {
		t.Fatalf("LoadBlockCommit after Close = %v, want nil", c)
	}
}

// TestCloseSafeDBConcurrentClose: readers that race Close never panic and
// never see an error. Run with -race.
func TestCloseSafeDBConcurrentClose(t *testing.T) {
	for round := 0; round < 20; round++ {
		db := &closeSafeDB{DB: openLevel(t)}
		if err := db.Set([]byte("k"), []byte("v")); err != nil {
			t.Fatal(err)
		}
		bs := store.NewBlockStore(db)
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, 64)
		for r := 0; r < 4; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < 200; i++ {
					if v, err := db.Get([]byte("k")); err != nil || (v != nil && string(v) != "v") {
						errs <- errors.New("Get returned an error or a wrong value")
						return
					}
					if _, err := db.Has([]byte("k")); err != nil {
						errs <- err
						return
					}
					it, err := db.Iterator(nil, nil)
					if err != nil {
						errs <- err
						return
					}
					for ; it.Valid(); it.Next() {
					}
					_ = it.Close()
					_ = bs.LoadSeenCommit(int64(i + 1))
				}
			}()
		}
		close(start)
		if err := bs.Close(); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
	}
}

// closingDuringRead is a database whose reads close the wrapping
// closeSafeDB first and then fail as a closed leveldb does. It makes the
// race between a read and Close deterministic: the read passed the closed
// check before Close ran.
type closingDuringRead struct {
	cmtdb.DB
	outer *closeSafeDB
}

var errLevelClosed = errors.New("leveldb: closed")

func (c *closingDuringRead) Get([]byte) ([]byte, error) {
	_ = c.outer.Close()
	return nil, errLevelClosed
}

func (c *closingDuringRead) Has([]byte) (bool, error) {
	_ = c.outer.Close()
	return false, errLevelClosed
}

func (c *closingDuringRead) Iterator([]byte, []byte) (cmtdb.Iterator, error) {
	_ = c.outer.Close()
	return nil, errLevelClosed
}

func (c *closingDuringRead) ReverseIterator([]byte, []byte) (cmtdb.Iterator, error) {
	_ = c.outer.Close()
	return nil, errLevelClosed
}

func racingDB() *closeSafeDB {
	outer := &closeSafeDB{}
	outer.DB = &closingDuringRead{DB: cmtdb.NewMemDB(), outer: outer}
	return outer
}

// TestCloseSafeDBReadRacingClose: a read that started before Close and fails
// because Close ran meanwhile is answered as not found, with no error. A read
// error while the database is still open is returned unchanged.
func TestCloseSafeDBReadRacingClose(t *testing.T) {
	if v, err := racingDB().Get([]byte("k")); err != nil || v != nil {
		t.Fatalf("Get racing Close = %q, %v; want nil, nil", v, err)
	}
	if ok, err := racingDB().Has([]byte("k")); err != nil || ok {
		t.Fatalf("Has racing Close = %v, %v; want false, nil", ok, err)
	}
	if it, err := racingDB().Iterator(nil, nil); err != nil || it.Valid() {
		t.Fatalf("Iterator racing Close: err=%v", err)
	}
	if it, err := racingDB().ReverseIterator(nil, nil); err != nil || it.Valid() {
		t.Fatalf("ReverseIterator racing Close: err=%v", err)
	}
	open := &closeSafeDB{DB: &failingRead{DB: cmtdb.NewMemDB()}}
	if _, err := open.Get([]byte("k")); !errors.Is(err, errReadFailed) {
		t.Fatalf("Get error while open = %v, want errReadFailed", err)
	}
	if _, err := open.Has([]byte("k")); !errors.Is(err, errReadFailed) {
		t.Fatalf("Has error while open = %v, want errReadFailed", err)
	}
}

// failingRead fails every read while open, to show closeSafeDB does not hide
// errors before Close.
type failingRead struct{ cmtdb.DB }

var errReadFailed = errors.New("read failed")

func (failingRead) Get([]byte) ([]byte, error) { return nil, errReadFailed }
func (failingRead) Has([]byte) (bool, error)   { return false, errReadFailed }

// TestCloseSafeDBAfterCloseMemDB: the after-Close answers do not depend on
// the backend failing. cmtdb.MemDB keeps serving reads after its Close, yet
// through closeSafeDB a read after Close still reports not found.
func TestCloseSafeDBAfterCloseMemDB(t *testing.T) {
	db := &closeSafeDB{DB: cmtdb.NewMemDB()}
	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Get([]byte("a")); err != nil || v != nil {
		t.Fatalf("Get after Close = %q, %v; want nil, nil", v, err)
	}
	if ok, err := db.Has([]byte("a")); err != nil || ok {
		t.Fatalf("Has after Close = %v, %v; want false, nil", ok, err)
	}
	if it, err := db.Iterator(nil, nil); err != nil || it.Valid() {
		t.Fatalf("Iterator after Close: valid or err=%v", err)
	}
	if it, err := db.ReverseIterator(nil, nil); err != nil || it.Valid() {
		t.Fatalf("ReverseIterator after Close: valid or err=%v", err)
	}
}
