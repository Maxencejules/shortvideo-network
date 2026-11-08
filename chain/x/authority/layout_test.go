package authority

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// TestLayoutKeyAndConsensusVersion (plan 7.4, 8.4): ConsensusVersion is 2
// and the module reports it; InitStore writes the layout key
// (msgkit.LayoutVersionKey) with that version before anything else, so
// msgkit.ReadLayoutVersion reads it; export accepts the layout key and does
// not carry it, refuses a layout key that does not hold ConsensusVersion,
// and refuses a key under the retired governed-value prefix 0x13 with
// ErrExportRefused.
func TestLayoutKeyAndConsensusVersion(t *testing.T) {
	if ConsensusVersion != 2 || (AppModule{}).ConsensusVersion() != ConsensusVersion {
		t.Fatalf("ConsensusVersion %d, module %d", ConsensusVersion, (AppModule{}).ConsensusVersion())
	}
	if !bytes.Equal([]byte{layoutKey}, msgkit.LayoutVersionKey) {
		t.Fatalf("layout key %x, msgkit %x", layoutKey, msgkit.LayoutVersionKey)
	}
	gen, _, _ := readyGenesis()
	recorder := &orderKV{KV: newMemKV()}
	if err := InitStore(recorder, gen); err != nil {
		t.Fatal(err)
	}
	if len(recorder.order) == 0 || !bytes.Equal(recorder.order[0], msgkit.LayoutVersionKey) {
		t.Fatalf("writes in order %x", recorder.order)
	}
	kv := storeFrom(t, gen)
	version, found, err := msgkit.ReadLayoutVersion(kv)
	if err != nil || !found || version != ConsensusVersion {
		t.Fatalf("layout version %d %t %v", version, found, err)
	}
	exported, err := ExportState(kv)
	if err != nil {
		t.Fatal(err)
	}
	again := storeFrom(t, exported)
	if !bytes.Equal(dumpKV(again), dumpKV(kv)) {
		t.Fatal("export and import do not round-trip the store")
	}

	for name, value := range map[string][]byte{"layout 1": encodeU64(1), "layout 3": encodeU64(3), "7 bytes": make([]byte, 7)} {
		bad := storeFrom(t, gen)
		bad.Set([]byte{layoutKey}, value)
		if _, err := ExportState(bad); err == nil || errors.Is(err, ErrExportRefused) || !strings.Contains(err.Error(), "layout version") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	retired := storeFrom(t, gen)
	retired.Set(append([]byte{governedPrefix}, msgkit.LimitMaxPageLimit...), encodeU64(10))
	if _, err := ExportState(retired); !errors.Is(err, ErrExportRefused) || !strings.Contains(err.Error(), "0x13") {
		t.Fatalf("retired 0x13 row: %v", err)
	}
}

// orderKV records the keys written through it, in order.
type orderKV struct {
	KV
	order [][]byte
}

func (k *orderKV) Set(key, value []byte) {
	k.order = append(k.order, bytes.Clone(key))
	k.KV.Set(key, value)
}

// TestOldLayoutRecordIsIntegrity (plan 7.4): a proposal record written by a
// layout version 1 binary (decodedWire without the schema version 2
// fields) does not decode, so an approve or execute that reads it is
// ERROR_CODE_INTEGRITY and writes nothing. Homes of layout version 1 are
// re-initialized (DR-10-04); the app refuses them at load (layout key).
func TestOldLayoutRecordIsIntegrity(t *testing.T) {
	kv, seats, authority := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "p1", governedMsg(msgkit.LimitMaxPageLimit, 10))
	var fields map[string]any
	if err := json.Unmarshal(kv.Get(proposalKey("p1")), &fields); err != nil {
		t.Fatal(err)
	}
	decoded := fields["decoded"].(map[string]any)
	for _, name := range []string{"activation", "upgrade_name", "upgrade_height", "upgrade_digest", "seat_id", "seat_key"} {
		if _, ok := decoded[name]; !ok {
			t.Fatalf("record has no %s field", name)
		}
		delete(decoded, name)
	}
	old, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	kv.Set(proposalKey("p1"), old)
	if _, err := decodeRecord("p1", old); err == nil {
		t.Fatal("a layout version 1 record decoded")
	}
	wantRefused(t, kv, "execute of a layout version 1 record", CodeIntegrity, func() Outcome {
		return loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-p1", "p1"))
	})
	wantRefused(t, kv, "approve of a layout version 1 record", CodeIntegrity, func() Outcome {
		return loadAt(t, kv, testHeight, testTime).ApproveChange(approve(seats[3].Signer, "ap-p1-3", "p1"))
	})
}

// TestRegisterServicesAndMigrations (plan 8.4): RegisterServices registers
// svn.authority.v1.Query on the gRPC query router (an ABCI query of
// Governance answers from the store) and the migration table, which covers
// exactly 2 .. ConsensusVersion-1 (empty while ConsensusVersion is 2); a
// step added to the table is registered and run by the module manager.
func TestRegisterServicesAndMigrations(t *testing.T) {
	e := readyKeeper(t)
	registry := codectypes.NewInterfaceRegistry()
	msgRouter := baseapp.NewMsgServiceRouter()
	msgRouter.SetInterfaceRegistry(registry)
	queryRouter := baseapp.NewGRPCQueryRouter()
	queryRouter.SetInterfaceRegistry(registry)
	cfg := module.NewConfigurator(codec.NewProtoCodec(registry), msgRouter, queryRouter)
	am := NewAppModule(e.keeper, nil)
	am.RegisterServices(cfg)
	if err := cfg.Error(); err != nil {
		t.Fatal(err)
	}
	route := queryRouter.Route("/svn.authority.v1.Query/Governance")
	if route == nil {
		t.Fatal("Query/Governance is not routed")
	}
	if msgRouter.HandlerByTypeURL("/svn.authority.v1.MsgProposeChange") != nil {
		t.Fatal("RegisterServices registered the Msg service; the app owns that route")
	}

	first := uint64(2)
	if uint64(len(migrations)) != ConsensusVersion-first {
		t.Fatalf("%d migrations for ConsensusVersion %d", len(migrations), ConsensusVersion)
	}
	for from := first; from < ConsensusVersion; from++ {
		if migrations[from] == nil {
			t.Fatalf("no migration from %d", from)
		}
	}
	saved := migrations
	defer func() { migrations = saved }()
	ran := 0
	migrations = map[uint64]module.MigrationHandler{ConsensusVersion: func(sdk.Context) error { ran++; return nil }}
	cfg2 := module.NewConfigurator(codec.NewProtoCodec(registry), baseapp.NewMsgServiceRouter(), baseapp.NewGRPCQueryRouter())
	if err := registerMigrations(cfg2); err != nil {
		t.Fatal(err)
	}
	if err := cfg2.RegisterMigration(ModuleName, ConsensusVersion, func(sdk.Context) error { return nil }); err == nil {
		t.Fatal("the table step was not registered")
	}
	mm := module.NewManager(versionedAuthority{AppModule: am, version: ConsensusVersion + 1})
	vm, err := mm.RunMigrations(e.blockCtx(), cfg2, module.VersionMap{ModuleName: ConsensusVersion})
	if err != nil || ran != 1 || vm[ModuleName] != ConsensusVersion+1 {
		t.Fatalf("migration ran %d times, map %v, %v", ran, vm, err)
	}
}

// versionedAuthority reports another ConsensusVersion, as a later binary
// would, so RunMigrations runs the step from ConsensusVersion.
type versionedAuthority struct {
	AppModule
	version uint64
}

func (v versionedAuthority) ConsensusVersion() uint64 { return v.version }
