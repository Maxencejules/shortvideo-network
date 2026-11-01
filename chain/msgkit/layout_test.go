package msgkit_test

import (
	"bytes"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
)

func TestWriteLayoutVersion(t *testing.T) {
	ctx, keys := msgkittest.NewContext(t, "m")
	store := ctx.KVStore(keys["m"])
	if len(msgkit.StoreSnapshot(store)) != 0 {
		t.Fatal("fresh store is not empty")
	}
	msgkit.WriteLayoutVersion(store, 1)
	got := store.Get([]byte{0x00})
	if !bytes.Equal(got, []byte{0, 0, 0, 0, 0, 0, 0, 1}) {
		t.Fatalf("layout version value %x", got)
	}
	if !bytes.Equal(msgkit.LayoutVersionKey, []byte{0x00}) {
		t.Fatalf("layout version key %x", msgkit.LayoutVersionKey)
	}
}
