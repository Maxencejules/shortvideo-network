package node

import (
	"errors"
	"sync/atomic"

	cmtdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
)

// dbProvider opens the CometBFT databases (block store, state store,
// evidence and tx index) for Start. It is a variable only so a test can
// check that Start uses closeSafeDBProvider.
var dbProvider cmtcfg.DBProvider = closeSafeDBProvider

// errDBClosed is returned by a write to a database after Close.
var errDBClosed = errors.New("node: database written after close")

// closeSafeDBProvider opens a database exactly as cmtcfg.DefaultDBProvider
// does and wraps it in closeSafeDB.
//
// Why: CometBFT v0.40.0 node.OnStop stops the p2p switch, then closes the
// block store. The switch does not wait for the per-peer consensus
// goroutine consensus.(*Reactor).queryMaj23Routine. That goroutine sleeps
// PeerQueryMaj23SleepDuration between sends and then calls
// State.LoadCommit without checking whether the node is still running.
// BlockStore.LoadSeenCommit panics on any database error, so a read that
// lands after the close panics with "leveldb: closed" and the process
// exits with status 2 instead of 0. No later CometBFT release exists to
// take a fix from.
func closeSafeDBProvider(ctx *cmtcfg.DBContext) (cmtdb.DB, error) {
	db, err := cmtcfg.DefaultDBProvider(ctx)
	if err != nil {
		return nil, err
	}
	return &closeSafeDB{DB: db}, nil
}

// closeSafeDB passes every call to the wrapped database until Close is
// called. After Close it changes only what a late caller sees:
//
//   - reads (Get, Has, Iterator, ReverseIterator) report "not found" or an
//     empty range instead of an error, so a goroutine that outlived the
//     shutdown gets nil and exits at its next running check;
//   - writes (Set, SetSync, Delete, DeleteSync) return errDBClosed, so a
//     write after close still fails loudly.
//
// Close happens only during node shutdown, so a read that returns "not
// found" for a key that exists can only be seen by a goroutine the node is
// already stopping. Before Close nothing changes.
type closeSafeDB struct {
	cmtdb.DB
	closed atomic.Bool
}

// Close marks the database closed before closing it, so a read racing the
// close sees the flag or the wrapped error, and is answered as not found.
func (d *closeSafeDB) Close() error {
	d.closed.Store(true)
	return d.DB.Close()
}

func (d *closeSafeDB) Get(key []byte) ([]byte, error) {
	if d.closed.Load() {
		return nil, nil
	}
	value, err := d.DB.Get(key)
	if err != nil && d.closed.Load() {
		return nil, nil
	}
	return value, err
}

func (d *closeSafeDB) Has(key []byte) (bool, error) {
	if d.closed.Load() {
		return false, nil
	}
	ok, err := d.DB.Has(key)
	if err != nil && d.closed.Load() {
		return false, nil
	}
	return ok, err
}

func (d *closeSafeDB) Iterator(start, end []byte) (cmtdb.Iterator, error) {
	if d.closed.Load() {
		return emptyIterator{start: start, end: end}, nil
	}
	it, err := d.DB.Iterator(start, end)
	if err != nil && d.closed.Load() {
		return emptyIterator{start: start, end: end}, nil
	}
	return it, err
}

func (d *closeSafeDB) ReverseIterator(start, end []byte) (cmtdb.Iterator, error) {
	if d.closed.Load() {
		return emptyIterator{start: start, end: end}, nil
	}
	it, err := d.DB.ReverseIterator(start, end)
	if err != nil && d.closed.Load() {
		return emptyIterator{start: start, end: end}, nil
	}
	return it, err
}

func (d *closeSafeDB) Set(key, value []byte) error {
	if d.closed.Load() {
		return errDBClosed
	}
	return d.DB.Set(key, value)
}

func (d *closeSafeDB) SetSync(key, value []byte) error {
	if d.closed.Load() {
		return errDBClosed
	}
	return d.DB.SetSync(key, value)
}

func (d *closeSafeDB) Delete(key []byte) error {
	if d.closed.Load() {
		return errDBClosed
	}
	return d.DB.Delete(key)
}

func (d *closeSafeDB) DeleteSync(key []byte) error {
	if d.closed.Load() {
		return errDBClosed
	}
	return d.DB.DeleteSync(key)
}

// emptyIterator is the iterator a closed closeSafeDB returns: an empty
// range. Next, Key and Value panic, as the cmtdb.Iterator contract allows
// for an iterator that is not Valid.
type emptyIterator struct {
	start, end []byte
}

func (it emptyIterator) Domain() ([]byte, []byte) { return it.start, it.end }
func (emptyIterator) Valid() bool                 { return false }
func (emptyIterator) Next()                       { panic("node: Next on an empty iterator") }
func (emptyIterator) Key() []byte                 { panic("node: Key on an empty iterator") }
func (emptyIterator) Value() []byte               { panic("node: Value on an empty iterator") }
func (emptyIterator) Error() error                { return nil }
func (emptyIterator) Close() error                { return nil }
