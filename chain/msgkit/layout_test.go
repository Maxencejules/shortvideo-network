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

func TestReadLayoutVersion(t *testing.T) {
	ctx, keys := msgkittest.NewContext(t, "m")
	store := ctx.KVStore(keys["m"])
	if v, found, err := msgkit.ReadLayoutVersion(store); found || err != nil || v != 0 {
		t.Fatalf("empty store: %d %v %v", v, found, err)
	}
	// The scaffold slot's only key is [0x01]; it has no layout version.
	store.Set([]byte{0x01}, []byte(`{"providers":[]}`))
	if _, found, err := msgkit.ReadLayoutVersion(store); found || err != nil {
		t.Fatalf("scaffold layout: %v %v", found, err)
	}
	msgkit.WriteLayoutVersion(store, 2)
	if v, found, err := msgkit.ReadLayoutVersion(store); !found || err != nil || v != 2 {
		t.Fatalf("version 2: %d %v %v", v, found, err)
	}
	store.Set(msgkit.LayoutVersionKey, []byte{2})
	if _, found, err := msgkit.ReadLayoutVersion(store); !found || err == nil {
		t.Fatalf("short value: %v %v", found, err)
	}
	if msgkit.ScaffoldLayoutVersion != 1 {
		t.Fatalf("scaffold layout version %d", msgkit.ScaffoldLayoutVersion)
	}
}
