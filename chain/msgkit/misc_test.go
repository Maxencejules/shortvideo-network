package msgkit

import (
	"bytes"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
)

func TestEffectiveLimit(t *testing.T) {
	cases := []struct {
		key      string
		governed uint64
		found    bool
		want     uint64
	}{
		{LimitMaxPackageBytes, 0, false, 268435456},
		{LimitMaxPackageBytes, 1024, false, 268435456},
		{LimitMaxPackageBytes, 1024, true, 1024},
		{LimitMaxPackageBytes, 1, true, 1},
		{LimitMaxPackageBytes, 0, true, 268435456},
		{LimitMaxPackageBytes, 268435456, true, 268435456},
		{LimitMaxPackageBytes, 268435457, true, 268435456},
		{LimitMaxPageLimit, 10, true, 10},
		{LimitMaxPageLimit, 101, true, 100},
		{LimitMaxPageLimit, 0, false, 100},
		{LimitMaxEvidenceBatch, 0, false, 32},
		{LimitMaxOpenOffers, 0, false, 1024},
		{LimitMaxExpirySteps, 0, false, 128},
		{"max_unknown", 5, true, 0},
		{"", 0, false, 0},
	}
	for _, tc := range cases {
		if got := Effective(tc.key, tc.governed, tc.found); got != tc.want {
			t.Errorf("Effective(%q, %d, %v) = %d, want %d", tc.key, tc.governed, tc.found, got, tc.want)
		}
	}
	if _, ok := Ceiling("max_unknown"); ok {
		t.Fatal("unknown key has a ceiling")
	}
	keys := LimitKeys()
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("LimitKeys not sorted: %v", keys)
		}
	}
}

func TestEnforce(t *testing.T) {
	signer := bytes.Repeat([]byte{1}, 20)
	base, _ := msgkittest.NewContext(t, "s")
	cases := []struct {
		name      string
		gasLimit  uint64
		infinite  bool
		txBytes   int
		signer    []byte
		want      Code
		wantSpent uint64
		outOfGas  bool
		mode      sdk.ExecMode
	}{
		{"ok", 200_000, false, 256, signer, OK, FixedMessageGas, false, sdk.ExecModeFinalize},
		{"gas limit 0", 0, false, 256, signer, Bounds, 0, false, sdk.ExecModeFinalize},
		{"gas limit 1 passes bounds, then runs out of gas", 1, false, 256, signer, OK, 0, true, sdk.ExecModeFinalize},
		{"gas limit max", genesis.MaxGasPerTx, false, 256, signer, OK, FixedMessageGas, false, sdk.ExecModeFinalize},
		{"gas limit max+1", genesis.MaxGasPerTx + 1, false, 256, signer, Bounds, 0, false, sdk.ExecModeFinalize},
		{"infinite gas meter", 0, true, 256, signer, Bounds, 0, false, sdk.ExecModeFinalize},
		{"tx at the cap", 200_000, false, int(genesis.MaxTxBytes), signer, OK, FixedMessageGas, false, sdk.ExecModeFinalize},
		{"tx above the cap", 200_000, false, int(genesis.MaxTxBytes) + 1, signer, Bounds, 0, false, sdk.ExecModeFinalize},
		{"signer 19 bytes", 200_000, false, 256, signer[:19], Malformed, FixedMessageGas, false, sdk.ExecModeFinalize},
		{"signer 21 bytes", 200_000, false, 256, append(append([]byte{}, signer...), 1), Malformed, FixedMessageGas, false, sdk.ExecModeFinalize},
		{"signer empty", 200_000, false, 256, nil, Malformed, FixedMessageGas, false, sdk.ExecModeFinalize},
		{"bounds before signer", 0, false, 256, nil, Bounds, 0, false, sdk.ExecModeFinalize},
		// Simulation runs on the SDK's infinite meter (limit 2^64-1). Only
		// the meter check is skipped there; FinalizeBlock keeps it.
		{"simulate on the infinite meter", 0, true, 256, signer, OK, FixedMessageGas, false, sdk.ExecModeSimulate},
		{"finalize on the infinite meter", 0, true, 256, signer, Bounds, 0, false, sdk.ExecModeFinalize},
		{"simulate tx above the cap", 0, true, int(genesis.MaxTxBytes) + 1, signer, Bounds, 0, false, sdk.ExecModeSimulate},
		{"simulate signer 19 bytes", 0, true, 256, signer[:19], Malformed, FixedMessageGas, false, sdk.ExecModeSimulate},
	}
	for _, tc := range cases {
		meter := storetypes.NewGasMeter(tc.gasLimit)
		if tc.infinite {
			meter = storetypes.NewInfiniteGasMeter()
		}
		ctx := base.WithGasMeter(meter).WithTxBytes(make([]byte, tc.txBytes)).WithExecMode(tc.mode)
		if tc.outOfGas {
			func() {
				defer func() {
					if _, ok := recover().(storetypes.ErrorOutOfGas); !ok {
						t.Errorf("%s: expected an out-of-gas panic", tc.name)
					}
				}()
				Enforce(ctx, tc.signer)
			}()
			continue
		}
		if got := Enforce(ctx, tc.signer); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		if spent := ctx.GasMeter().GasConsumed(); spent != tc.wantSpent {
			t.Errorf("%s: spent %d gas, want %d", tc.name, spent, tc.wantSpent)
		}
	}
}

func TestStoreSnapshot(t *testing.T) {
	ctx, keys := msgkittest.NewContext(t, "a", "b")
	a := ctx.MultiStore().GetKVStore(keys["a"])
	b := ctx.MultiStore().GetKVStore(keys["b"])
	if len(StoreSnapshot(a)) != 0 {
		t.Fatal("empty store has a snapshot")
	}
	a.Set([]byte("ab"), []byte("c"))
	b.Set([]byte("a"), []byte("bc"))
	// Length prefixes keep different pairs with the same concatenation apart.
	if bytes.Equal(StoreSnapshot(a), StoreSnapshot(b)) {
		t.Fatal("ambiguous snapshot encoding")
	}
	a.Set([]byte{0x01}, []byte{})
	first := StoreSnapshot(a)
	if !bytes.Equal(first, StoreSnapshot(a)) {
		t.Fatal("snapshot not stable")
	}
	want := []byte{0, 0, 0, 0, 0, 0, 0, 1, 0x01, 0, 0, 0, 0, 0, 0, 0, 0}
	want = append(want, 0, 0, 0, 0, 0, 0, 0, 2, 'a', 'b', 0, 0, 0, 0, 0, 0, 0, 1, 'c')
	if !bytes.Equal(first, want) {
		t.Fatalf("snapshot %x, want %x", first, want)
	}
	a.Set([]byte("ab"), []byte("d"))
	if bytes.Equal(first, StoreSnapshot(a)) {
		t.Fatal("value change not visible")
	}
	a.Delete([]byte("ab"))
	a.Set([]byte("ab"), []byte("c"))
	if !bytes.Equal(first, StoreSnapshot(a)) {
		t.Fatal("same pairs give a different snapshot")
	}
}

func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}
