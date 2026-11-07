package rehearsal_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"cosmossdk.io/log/v2"

	store "github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// storeCtx is a context over one real IAVL store (rootmulti over an
// in-memory cosmos-db).
func storeCtx(t *testing.T) (sdk.Context, *storetypes.KVStoreKey) {
	t.Helper()
	cms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger())
	key := storetypes.NewKVStoreKey("rehearsal-test")
	cms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
	if err := cms.LoadLatestVersion(); err != nil {
		t.Fatal(err)
	}
	return sdk.NewContext(cms, cmtproto.Header{Height: 1}, false, log.NewNopLogger()), key
}

func marshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rows is every row of the store in key order, as "hex(key)=hex(value)".
func rows(ctx sdk.Context, key storetypes.StoreKey) map[string][]byte {
	out := map[string][]byte{}
	it := ctx.KVStore(key).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = bytes.Clone(it.Value())
	}
	return out
}

// leaseStore writes a layout-2 leases store: one offer, one lease, and one
// deadline index row.
func leaseStore(t *testing.T, ctx sdk.Context, key storetypes.StoreKey) {
	t.Helper()
	kv := ctx.KVStore(key)
	msgkit.WriteLayoutVersion(kv, leases.ConsensusVersion)
	kv.Set(leases.OfferKey("o1"), marshal(t, &leasesv1.Offer{OfferId: "o1", ContentId: "c1", ProviderId: "p1", ContentVersion: 1}))
	kv.Set(leases.LeaseKey("l1"), marshal(t, &leasesv1.Lease{LeaseId: "l1", ContentId: "c1", ProviderId: "p1", ContentVersion: 1, DeadlineUnixSeconds: 99}))
	kv.Set(leases.LeaseDeadlineKey(99, "l1"), []byte{0x01})
}

// TestUpgradesDeclareAppVersionTwo: the rehearsal binary's handlers are one
// upgrade, rehearsal-1, from app version 1 to 2; a binary at app version 2
// with them passes upgrade.Binary.Validate, and one at app version 1 does
// not. Handler passes its context and a copy of the stored map to Migrate
// and returns Migrate's map and error unchanged.
func TestUpgradesDeclareAppVersionTwo(t *testing.T) {
	ups := rehearsal.Upgrades()
	if len(ups) != 1 || ups[0].Name != "rehearsal-1" || ups[0].FromAppVersion != 1 || ups[0].ToAppVersion != 2 || ups[0].Handler == nil {
		t.Fatalf("upgrades %+v", ups)
	}
	if err := (upgrade.Binary{AppVersion: 2, Modules: module.VersionMap{"leases": 3}, Upgrades: ups}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (upgrade.Binary{AppVersion: 1, Modules: module.VersionMap{"leases": 2}, Upgrades: ups}).Validate(); !errors.Is(err, upgrade.ErrInvalidBinary) {
		t.Fatalf("app version 1 binary with the rehearsal upgrade: %v", err)
	}
	ctx, _ := storeCtx(t)
	from := module.VersionMap{"leases": 2}
	var seen module.VersionMap
	migrated, err := rehearsal.Handler(ctx, from, func(_ sdk.Context, vm module.VersionMap) (module.VersionMap, error) {
		seen = vm
		return module.VersionMap{"leases": 3}, nil
	})
	if err != nil || migrated["leases"] != 3 || seen["leases"] != 2 {
		t.Fatalf("handler returned %v %v; migrate saw %v", migrated, err, seen)
	}
	fail := errors.New("migration failed")
	if _, err := rehearsal.Handler(ctx, from, func(sdk.Context, module.VersionMap) (module.VersionMap, error) { return nil, fail }); !errors.Is(err, fail) {
		t.Fatalf("handler error %v", err)
	}
}

// TestLeasesStepRebuildsTheStore: LeasesStep keeps every row byte for byte
// and moves the layout version from 2 to 3. BadLeasesStep does the same and
// adds exactly the documented row 0xff -> 0x01.
func TestLeasesStepRebuildsTheStore(t *testing.T) {
	for _, bad := range []bool{false, true} {
		ctx, key := storeCtx(t)
		leaseStore(t, ctx, key)
		before := rows(ctx, key)
		step := rehearsal.LeasesStep(key)
		if bad {
			step = rehearsal.BadLeasesStep(key)
		}
		if err := step(ctx); err != nil {
			t.Fatal(err)
		}
		after := rows(ctx, key)
		version, found, err := msgkit.ReadLayoutVersion(ctx.KVStore(key))
		if err != nil || !found || version != leases.ConsensusVersion+1 {
			t.Fatalf("bad %t: layout %d found %t err %v", bad, version, found, err)
		}
		delete(before, string(msgkit.LayoutVersionKey))
		delete(after, string(msgkit.LayoutVersionKey))
		if bad {
			if !bytes.Equal(after[string(rehearsal.BadRowKey)], []byte{rehearsal.BadRowValue}) {
				t.Fatalf("bad step row %x", after[string(rehearsal.BadRowKey)])
			}
			delete(after, string(rehearsal.BadRowKey))
		}
		if len(after) != len(before) {
			t.Fatalf("bad %t: %d rows, want %d", bad, len(after), len(before))
		}
		for k, v := range before {
			if !bytes.Equal(after[k], v) {
				t.Fatalf("bad %t: row %x changed", bad, k)
			}
		}
	}
}

// TestEvidenceStepRebuildsTheStore: EvidenceStep keeps an evidence record
// and an index row and moves the layout version from 2 to 3.
func TestEvidenceStepRebuildsTheStore(t *testing.T) {
	ctx, key := storeCtx(t)
	kv := ctx.KVStore(key)
	msgkit.WriteLayoutVersion(kv, evidence.ConsensusVersion)
	record := marshal(t, &evidencev1.EvidenceRecord{EvidenceId: "e1", LeaseId: "l1", ExpiryUnixSeconds: 7})
	kv.Set(evidence.RecordKey("e1"), record)
	kv.Set(evidence.LeaseIndexKey("l1", "e1"), []byte{0x01})
	if err := rehearsal.EvidenceStep(key)(ctx); err != nil {
		t.Fatal(err)
	}
	if version, _, _ := msgkit.ReadLayoutVersion(kv); version != evidence.ConsensusVersion+1 {
		t.Fatalf("layout %d", version)
	}
	if !bytes.Equal(kv.Get(evidence.RecordKey("e1")), record) || !kv.Has(evidence.LeaseIndexKey("l1", "e1")) {
		t.Fatal("the evidence rows changed")
	}
}

// TestStepsRefuseBeforeWriting: a store the step cannot read is refused
// with an error and left byte for byte as it was: a layout version other
// than 2, no layout version, a record that does not decode, a record with
// an unknown field, a record under another id, and a record whose stored
// bytes are not its deterministic encoding.
func TestStepsRefuseBeforeWriting(t *testing.T) {
	lease := &leasesv1.Lease{LeaseId: "l1", ContentId: "c1", ProviderId: "p1", ContentVersion: 1, DeadlineUnixSeconds: 99}
	reordered := protowire.AppendTag(nil, 2, protowire.BytesType)
	reordered = protowire.AppendString(reordered, "c1")
	reordered = protowire.AppendTag(reordered, 1, protowire.BytesType)
	reordered = protowire.AppendString(reordered, "l1")
	for _, c := range []struct {
		name string
		edit func(t *testing.T, kv storetypes.KVStore)
		want string
	}{
		{"layout 3", func(_ *testing.T, kv storetypes.KVStore) { msgkit.WriteLayoutVersion(kv, 3) }, "layout version 3"},
		{"no layout", func(_ *testing.T, kv storetypes.KVStore) { kv.Delete(msgkit.LayoutVersionKey) }, "found false"},
		{"undecodable", func(_ *testing.T, kv storetypes.KVStore) { kv.Set(leases.LeaseKey("l1"), []byte{0xff, 0xff}) }, "does not decode"},
		{"unknown field", func(t *testing.T, kv storetypes.KVStore) {
			raw := protowire.AppendVarint(protowire.AppendTag(marshal(t, lease), 99, protowire.VarintType), 1)
			kv.Set(leases.LeaseKey("l1"), raw)
		}, "unknown fields"},
		{"other id", func(t *testing.T, kv storetypes.KVStore) {
			kv.Set(leases.LeaseKey("l2"), marshal(t, lease))
		}, `names "l1"`},
		{"not deterministic", func(_ *testing.T, kv storetypes.KVStore) { kv.Set(leases.LeaseKey("l1"), reordered) }, "does not re-encode"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, key := storeCtx(t)
			leaseStore(t, ctx, key)
			c.edit(t, ctx.KVStore(key))
			before := rows(ctx, key)
			err := rehearsal.LeasesStep(key)(ctx)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}
			after := rows(ctx, key)
			if len(after) != len(before) {
				t.Fatalf("%d rows after the refusal, %d before", len(after), len(before))
			}
			for k, v := range before {
				if !bytes.Equal(after[k], v) {
					t.Fatalf("row %x changed after the refusal", k)
				}
			}
		})
	}
}

// TestDeterministicSources: the rehearsal handler and migration steps run
// inside chain execution, so the package's non-test code (and the
// scenario package's) reads no clock, randomness, environment, file,
// process, or network source. The command under cmd/ is a main and is not
// scanned.
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
	msgkittest.AssertDeterministicSources(t, "scenario")
}
