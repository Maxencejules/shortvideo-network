package app

import (
	"bytes"
	"encoding/binary"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// Goal 10 INT-b test hooks. This is a _test.go file, so none of these
// symbols is linked into svnd (TestLinkedBinaryOmitsTestHooks).

// AccountNumberForTest is the committed auth account number of addr, and
// whether the account exists. A council seat payload creates the new seat
// key's account inside its execute (DR-10-35); the governance tests read
// its number here to sign that key's first transaction.
func (app *App) AccountNumberForTest(addr []byte) (uint64, bool) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	account := app.accounts.GetAccount(ctx, sdk.AccAddress(addr))
	if account == nil {
		return 0, false
	}
	return account.GetAccountNumber(), true
}

// SignerBlindAuthSnapshotForTest is every row of the committed auth store
// in key order, each as the key length (8 bytes big-endian), the key, the
// value length, and the value, where an account row (prefix 0x01 followed
// by the 20-byte address) is replaced by that account with sequence 0 and
// no public key, encoded by the app codec. Those two fields are what the
// ante handler writes for a signer whose transaction it admits (the
// sequence on every one, the public key on the first), so the snapshot
// stays equal across a transaction a message handler refuses, and still
// shows an account that was created or removed, a changed account number,
// the global account number, and every other auth row.
func (app *App) SignerBlindAuthSnapshotForTest() ([]byte, error) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	accountPrefix := authtypes.AddressStoreKeyPrefix.Bytes()
	var out []byte
	it := ctx.KVStore(app.keys[authtypes.StoreKey]).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		key, value := it.Key(), it.Value()
		if bytes.HasPrefix(key, accountPrefix) {
			account := app.accounts.GetAccount(ctx, sdk.AccAddress(key[len(accountPrefix):]))
			if account == nil {
				return nil, fmt.Errorf("auth row %x does not decode as an account", key)
			}
			if err := account.SetSequence(0); err != nil {
				return nil, err
			}
			if err := account.SetPubKey(nil); err != nil {
				return nil, err
			}
			raw, err := app.encoding.Codec.MarshalInterface(account)
			if err != nil {
				return nil, err
			}
			value = raw
		}
		out = binary.BigEndian.AppendUint64(out, uint64(len(key)))
		out = append(out, key...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(value)))
		out = append(out, value...)
	}
	return out, nil
}

// AccountAddressesForTest is the address of every committed auth account,
// in key order. Account numbers are derived from the block header's app
// hash and the transaction index (SDK auth GenerateID), so two chains that
// create the same account in different states give it different numbers;
// tests that compare two chains compare these addresses instead.
func (app *App) AccountAddressesForTest() [][]byte {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	accountPrefix := authtypes.AddressStoreKeyPrefix.Bytes()
	var out [][]byte
	it := ctx.KVStore(app.keys[authtypes.StoreKey]).Iterator(accountPrefix, storetypes.PrefixEndBytes(accountPrefix))
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out = append(out, append([]byte(nil), it.Key()[len(accountPrefix):]...))
	}
	return out
}

// StoreAtForTest is msgkit.StoreSnapshot of store name as committed at
// height: every key and value of that version, read from the multistore's
// versioned trees. (The raw ABCI /store/<name>/subspace query reads the
// latest version whatever height it is given.)
func (app *App) StoreAtForTest(name string, height int64) ([]byte, error) {
	key, ok := app.keys[name]
	if !ok {
		return nil, fmt.Errorf("no store %s", name)
	}
	versioned, err := app.CommitMultiStore().CacheMultiStoreWithVersion(height)
	if err != nil {
		return nil, err
	}
	return msgkit.StoreSnapshot(versioned.GetKVStore(key)), nil
}
