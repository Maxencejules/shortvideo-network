package rehearsal_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sort"
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
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
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

// testEnv is the handler environment over the test context's stores.
type testEnv map[string]*storetypes.KVStoreKey

func (e testEnv) StoreNames() []string {
	names := make([]string, 0, len(e))
	for name := range e {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e testEnv) KVStore(ctx sdk.Context, name string) (storetypes.KVStore, bool) {
	key, ok := e[name]
	if !ok {
		return nil, false
	}
	return ctx.KVStore(key), true
}

// walkedStores are the stores the rehearsal handler reads.
var walkedStores = []string{providers.ModuleName, content.ModuleName, leases.ModuleName, evidence.ModuleName, authority.ModuleName}

// contentKey is an x/content record key: prefix, id, 0x00, version as 8
// bytes big-endian (chain/x/content/store.go).
func contentKey(prefix byte, id string, version uint64) []byte {
	return binary.BigEndian.AppendUint64(append(append([]byte{prefix}, id...), 0x00), version)
}

// handlerCtx is a context on the devnet chain id msgkittest.ChainID over the
// walked stores, each holding valid records and an index row, carrying the
// handler environment over them (upgrade.WithEnv), as the upgrade runner
// gives it to a handler.
func handlerCtx(t *testing.T) (sdk.Context, map[string]*storetypes.KVStoreKey) {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, walkedStores...)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	prov := ctx.KVStore(keys[providers.ModuleName])
	prov.Set(providers.ProviderKey("p1"), marshal(t, &providersv1.Provider{ProviderId: "p1", KeyGeneration: 2}))
	prov.Set(providers.RetiredKeyKey("p1", 1), marshal(t, &providersv1.PreviousSigningKey{PublicKey: []byte{7}, Generation: 1}))
	prov.Set(append([]byte{providers.BusinessKeyPrefix}, "b1"...), bytes.Repeat([]byte{1}, 32))
	cont := ctx.KVStore(keys[content.ModuleName])
	cont.Set(contentKey(content.ContentPrefix, "c1", 1), marshal(t, &contentv1.Content{ContentId: "c1", ContentVersion: 1, PackageBytes: 9}))
	cont.Set(contentKey(content.TombstonePrefix, "c2", 1), marshal(t, &contentv1.Tombstone{ContentId: "c2", ContentVersion: 1}))
	leaseStore(t, ctx, keys[leases.ModuleName])
	ev := ctx.KVStore(keys[evidence.ModuleName])
	ev.Set(evidence.RecordKey("e1"), marshal(t, &evidencev1.EvidenceRecord{EvidenceId: "e1", LeaseId: "l1", ExpiryUnixSeconds: 7}))
	ev.Set(evidence.LeaseIndexKey("l1", "e1"), []byte{0x01})
	auth := ctx.KVStore(keys[authority.ModuleName])
	auth.Set([]byte{0x10, 'p', '1'}, []byte(`{"proposal_id":"p1","parameter_key":"k","state":1}`))
	auth.Set([]byte{0x01}, []byte("config"))
	return upgrade.WithEnv(ctx, testEnv(keys)), keys
}

// allRows is every row of every store of keys, keyed by store name and key.
func allRows(ctx sdk.Context, keys map[string]*storetypes.KVStoreKey) map[string]map[string][]byte {
	out := map[string]map[string][]byte{}
	for name, key := range keys {
		out[name] = rows(ctx, key)
	}
	return out
}

// sameRows fails the test unless a and b hold the same rows.
func sameRows(t *testing.T, label string, a, b map[string]map[string][]byte) {
	t.Helper()
	for name := range a {
		if len(a[name]) != len(b[name]) {
			t.Fatalf("%s: store %s has %d rows, then %d", label, name, len(a[name]), len(b[name]))
		}
		for k, v := range a[name] {
			if !bytes.Equal(b[name][k], v) {
				t.Fatalf("%s: store %s row %x changed", label, name, k)
			}
		}
	}
}

// TestUpgradesDeclareAppVersionTwo: the handlers of B2 (Upgrades) and of
// B2-bad (BadUpgrades) are each one upgrade, rehearsal-1, from app version
// 1 to 2, the same plan name; a binary at app version 2 with them passes
// upgrade.Binary.Validate, and one at app version 1 does not. On a devnet
// chain with the handler environment and valid stores, Handler and
// BadHandler pass their context and a copy of the stored map to Migrate and
// return Migrate's map and error unchanged.
func TestUpgradesDeclareAppVersionTwo(t *testing.T) {
	for name, ups := range map[string][]upgrade.Upgrade{"B2": rehearsal.Upgrades(), "B2-bad": rehearsal.BadUpgrades()} {
		if len(ups) != 1 || ups[0].Name != "rehearsal-1" || ups[0].FromAppVersion != 1 || ups[0].ToAppVersion != 2 || ups[0].Handler == nil {
			t.Fatalf("%s upgrades %+v", name, ups)
		}
		if err := (upgrade.Binary{AppVersion: 2, Modules: module.VersionMap{"leases": 3}, Upgrades: ups}).Validate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := (upgrade.Binary{AppVersion: 1, Modules: module.VersionMap{"leases": 2}, Upgrades: ups}).Validate(); !errors.Is(err, upgrade.ErrInvalidBinary) {
			t.Fatalf("%s: app version 1 binary with the rehearsal upgrade: %v", name, err)
		}
	}
	for name, handler := range map[string]upgrade.Handler{"Handler": rehearsal.Handler, "BadHandler": rehearsal.BadHandler} {
		ctx, _ := handlerCtx(t)
		from := module.VersionMap{"leases": 2}
		var seen module.VersionMap
		migrated, err := handler(ctx, from, func(_ sdk.Context, vm module.VersionMap) (module.VersionMap, error) {
			seen = vm
			return module.VersionMap{"leases": 3}, nil
		})
		if err != nil || migrated["leases"] != 3 || seen["leases"] != 2 {
			t.Fatalf("%s returned %v %v; migrate saw %v", name, migrated, err, seen)
		}
		fail := errors.New("migration failed")
		if _, err := handler(ctx, from, func(sdk.Context, module.VersionMap) (module.VersionMap, error) { return nil, fail }); !errors.Is(err, fail) {
			t.Fatalf("%s error %v", name, err)
		}
	}
}

// TestLeasesStepRebuildsTheStore: LeasesStep keeps every row byte for byte
// and moves the layout version from 2 to 3.
func TestLeasesStepRebuildsTheStore(t *testing.T) {
	ctx, key := storeCtx(t)
	leaseStore(t, ctx, key)
	before := rows(ctx, key)
	if err := rehearsal.LeasesStep(key)(ctx); err != nil {
		t.Fatal(err)
	}
	after := rows(ctx, key)
	version, found, err := msgkit.ReadLayoutVersion(ctx.KVStore(key))
	if err != nil || !found || version != leases.ConsensusVersion+1 {
		t.Fatalf("layout %d found %t err %v", version, found, err)
	}
	delete(before, string(msgkit.LayoutVersionKey))
	delete(after, string(msgkit.LayoutVersionKey))
	if len(after) != len(before) {
		t.Fatalf("%d rows, want %d", len(after), len(before))
	}
	for k, v := range before {
		if !bytes.Equal(after[k], v) {
			t.Fatalf("row %x changed", k)
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

// TestHandlerWalksEveryRecordReadOnly: Handler reads every provider,
// retired key, content, tombstone, offer, lease, evidence, and proposal
// record of valid stores, writes nothing, and then runs Migrate. A record it
// cannot read is refused before Migrate runs, with every store left byte for
// byte as it was: a record that does not decode, has an unknown field, sits
// under a key its fields do not name, or is not its deterministic encoding;
// a proposal record that is not one JSON object, names another id, or has an
// unknown or unspecified state; and a walked store the environment does not
// mount. BadHandler refuses the same states before its own write.
func TestHandlerWalksEveryRecordReadOnly(t *testing.T) {
	ctx, keys := handlerCtx(t)
	before := allRows(ctx, keys)
	calls := 0
	migrate := func(_ sdk.Context, from module.VersionMap) (module.VersionMap, error) {
		calls++
		return from, nil
	}
	if _, err := rehearsal.Handler(ctx, module.VersionMap{"leases": 2}, migrate); err != nil || calls != 1 {
		t.Fatalf("valid stores: err %v, migrate ran %d times", err, calls)
	}
	sameRows(t, "valid stores", before, allRows(ctx, keys))

	unknownField := func(t *testing.T, m proto.Message) []byte {
		return protowire.AppendVarint(protowire.AppendTag(marshal(t, m), 99, protowire.VarintType), 1)
	}
	type damage struct {
		name  string
		store string
		key   []byte
		value func(t *testing.T) []byte
		want  string
	}
	cases := []damage{
		{"provider does not decode", providers.ModuleName, providers.ProviderKey("p1"), func(*testing.T) []byte { return []byte{0xff, 0xff} }, "does not decode"},
		{"provider under another id", providers.ModuleName, providers.ProviderKey("p2"), func(t *testing.T) []byte {
			return marshal(t, &providersv1.Provider{ProviderId: "p1"})
		}, `names "p1"`},
		{"provider unknown field", providers.ModuleName, providers.ProviderKey("p1"), func(t *testing.T) []byte {
			return unknownField(t, &providersv1.Provider{ProviderId: "p1"})
		}, "unknown fields"},
		{"retired key under another generation", providers.ModuleName, providers.RetiredKeyKey("p1", 3), func(t *testing.T) []byte {
			return marshal(t, &providersv1.PreviousSigningKey{Generation: 1})
		}, "names"},
		{"retired key under a short key", providers.ModuleName, []byte{providers.RetiredKeyPrefix, 'p'}, func(t *testing.T) []byte {
			return marshal(t, &providersv1.PreviousSigningKey{Generation: 1})
		}, "names"},
		{"content under another version", content.ModuleName, contentKey(content.ContentPrefix, "c1", 2), func(t *testing.T) []byte {
			return marshal(t, &contentv1.Content{ContentId: "c1", ContentVersion: 1})
		}, "names"},
		{"tombstone unknown field", content.ModuleName, contentKey(content.TombstonePrefix, "c2", 1), func(t *testing.T) []byte {
			return unknownField(t, &contentv1.Tombstone{ContentId: "c2", ContentVersion: 1})
		}, "unknown fields"},
		{"lease not deterministic", leases.ModuleName, leases.LeaseKey("l1"), func(*testing.T) []byte {
			raw := protowire.AppendTag(nil, 2, protowire.BytesType)
			raw = protowire.AppendString(raw, "c1")
			raw = protowire.AppendTag(raw, 1, protowire.BytesType)
			return protowire.AppendString(raw, "l1")
		}, "does not re-encode"},
		{"evidence under another id", evidence.ModuleName, evidence.RecordKey("e2"), func(t *testing.T) []byte {
			return marshal(t, &evidencev1.EvidenceRecord{EvidenceId: "e1"})
		}, `names "e1"`},
		{"proposal is not JSON", authority.ModuleName, []byte{0x10, 'p', '1'}, func(*testing.T) []byte { return []byte("{") }, "does not decode"},
		{"proposal has trailing data", authority.ModuleName, []byte{0x10, 'p', '1'}, func(*testing.T) []byte {
			return []byte(`{"proposal_id":"p1","state":1}{}`)
		}, "data after its JSON object"},
		{"proposal under another id", authority.ModuleName, []byte{0x10, 'p', '2'}, func(*testing.T) []byte {
			return []byte(`{"proposal_id":"p1","state":1}`)
		}, `names "p1"`},
		{"proposal state unspecified", authority.ModuleName, []byte{0x10, 'p', '1'}, func(*testing.T) []byte {
			return []byte(`{"proposal_id":"p1","state":0}`)
		}, "has state 0"},
		{"proposal state unknown", authority.ModuleName, []byte{0x10, 'p', '1'}, func(*testing.T) []byte {
			return []byte(`{"proposal_id":"p1","state":99}`)
		}, "has state 99"},
	}
	for _, c := range cases {
		for name, handler := range map[string]upgrade.Handler{"Handler": rehearsal.Handler, "BadHandler": rehearsal.BadHandler} {
			t.Run(c.name+"/"+name, func(t *testing.T) {
				ctx, keys := handlerCtx(t)
				ctx.KVStore(keys[c.store]).Set(c.key, c.value(t))
				before := allRows(ctx, keys)
				calls := 0
				_, err := handler(ctx, module.VersionMap{"leases": 2}, func(_ sdk.Context, from module.VersionMap) (module.VersionMap, error) {
					calls++
					return from, nil
				})
				if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), c.store+" store") {
					t.Fatalf("error %v, want %q in the %s store", err, c.want, c.store)
				}
				if calls != 0 {
					t.Fatal("migrate ran after a refused record")
				}
				sameRows(t, c.name, before, allRows(ctx, keys))
			})
		}
	}
	for _, missing := range walkedStores {
		env := testEnv{}
		for name, key := range keys {
			if name != missing {
				env[name] = key
			}
		}
		_, err := rehearsal.Handler(upgrade.WithEnv(ctx, env), module.VersionMap{}, migrate)
		if err == nil || !strings.Contains(err.Error(), "store "+missing+" is not mounted") {
			t.Fatalf("environment without %s: %v", missing, err)
		}
	}
}

// TestHandlersRefuseOutsideTheirEnvironment: Handler and BadHandler refuse a
// chain whose id does not start with shortvideo-dev- (the rehearsal builds
// run on devnet chains only) and a context without the handler environment,
// before Migrate runs and before any write.
func TestHandlersRefuseOutsideTheirEnvironment(t *testing.T) {
	for name, handler := range map[string]upgrade.Handler{"Handler": rehearsal.Handler, "BadHandler": rehearsal.BadHandler} {
		ctx, keys := handlerCtx(t)
		before := allRows(ctx, keys)
		calls := 0
		migrate := func(_ sdk.Context, from module.VersionMap) (module.VersionMap, error) {
			calls++
			return from, nil
		}
		for label, c := range map[string]struct {
			ctx  sdk.Context
			want string
		}{
			"production chain id":      {ctx.WithChainID("svn-beta-1"), `runs only on a chain whose id starts with "shortvideo-dev-", not on "svn-beta-1"`},
			"empty chain id":           {ctx.WithChainID(""), `not on ""`},
			"prefix inside another id": {ctx.WithChainID("svn-shortvideo-dev-1"), `not on "svn-shortvideo-dev-1"`},
			"no environment":           {ctx.WithContext(context.Background()), "carries no handler environment"},
		} {
			_, err := handler(c.ctx, module.VersionMap{}, migrate)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%s, %s: %v, want %q", name, label, err, c.want)
			}
		}
		if calls != 0 {
			t.Fatalf("%s: migrate ran %d times", name, calls)
		}
		sameRows(t, name, before, allRows(ctx, keys))
	}
}

// TestBadHandlerWritesOneDocumentedRow: BadHandler (B2-bad) leaves every
// store as Handler (B2) does except exactly one row: BadRowKey ->
// BadRowValue in the x/leases store, a prefix x/leases never uses.
func TestBadHandlerWritesOneDocumentedRow(t *testing.T) {
	migrate := func(_ sdk.Context, from module.VersionMap) (module.VersionMap, error) { return from, nil }
	good, goodKeys := handlerCtx(t)
	if _, err := rehearsal.Handler(good, module.VersionMap{}, migrate); err != nil {
		t.Fatal(err)
	}
	bad, badKeys := handlerCtx(t)
	if _, err := rehearsal.BadHandler(bad, module.VersionMap{}, migrate); err != nil {
		t.Fatal(err)
	}
	want, got := allRows(good, goodKeys), allRows(bad, badKeys)
	if v := got[leases.ModuleName][string(rehearsal.BadRowKey)]; !bytes.Equal(v, []byte{rehearsal.BadRowValue}) || len(rehearsal.BadRowKey) != 1 || rehearsal.BadRowKey[0] != 0xFF {
		t.Fatalf("bad row %x -> %x", rehearsal.BadRowKey, v)
	}
	if _, used := want[leases.ModuleName][string(rehearsal.BadRowKey)]; used {
		t.Fatal("the honest handler wrote the bad row")
	}
	delete(got[leases.ModuleName], string(rehearsal.BadRowKey))
	sameRows(t, "B2 and B2-bad", want, got)
}

// TestDeterministicSources: the rehearsal handler and migration steps run
// inside chain execution, so the package's non-test code (and the
// scenario package's) reads no clock, randomness, environment, file,
// process, or network source.
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
	msgkittest.AssertDeterministicSources(t, "scenario")
}
