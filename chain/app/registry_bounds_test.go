package app_test

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// nodeOptions is a servertypes.AppOptions map for tests.
type nodeOptions map[string]any

func (o nodeOptions) Get(key string) any { return o[key] }

// queryGas runs one query through the node's query router on the query
// context the node builds for ABCI and gRPC queries, and returns the gas it
// used, the meter limit, and the response bytes.
func queryGas(t *testing.T, chainApp *app.App, path string, req proto.Message) (used, limit uint64, value []byte) {
	t.Helper()
	ctx, err := chainApp.CreateQueryContext(0, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	handler := chainApp.GRPCQueryRouter().Route(path)
	if handler == nil {
		t.Fatalf("no route %s", path)
	}
	res, err := handler(ctx, &abci.RequestQuery{Path: path, Data: data})
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return ctx.GasMeter().GasConsumed(), ctx.GasMeter().Limit(), res.Value
}

// TestRotationCapInApp drives one provider to MaxRetiredKeysPerProvider
// retired keys with committed RotateProviderKey transactions. The next
// rotation is ERROR_CODE_BOUNDS and leaves the full state byte-identical.
// Query/Provider at the bound stays within the stated 64 KiB and well
// within the node's query gas limit; before the bound existed, each
// rotation (no fee, any provider state) grew that response without limit.
func TestRotationCapInApp(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)

	const perBlock = 100
	generation := uint64(1)
	for generation <= providers.MaxRetiredKeysPerProvider {
		var txs [][]byte
		for len(txs) < perBlock && generation <= providers.MaxRetiredKeysPerProvider {
			next := newSigningKey(fmt.Sprintf("p1-k%04d", generation))
			msg := n.rotate(t, n.provider1, fmt.Sprintf("rot-%04d", generation), "p1", generation, next)
			txs = append(txs, n.signed(t, c, n.provider1, registryGas, nil, msg))
			n.seq[n.provider1.hexAddr()]++
			n.signing["p1"] = next
			generation++
		}
		res := c.block(t, txs...)
		for i, r := range res.TxResults {
			wantCode(t, fmt.Sprintf("rotation %d of block %d", i, c.height()), r, msgkit.OK)
		}
	}

	record, raw := queryProvider(t, chainApp, "p1", 0)
	if record.GetKeyGeneration() != providers.MaxRetiredKeysPerProvider+1 || len(record.GetPreviousSigningKeys()) != providers.MaxRetiredKeysPerProvider {
		t.Fatalf("at the bound: generation %d, %d retired keys", record.GetKeyGeneration(), len(record.GetPreviousSigningKeys()))
	}
	if len(raw) > 64<<10 {
		t.Fatalf("Query/Provider at the bound is %d bytes", len(raw))
	}
	used, limit, _ := queryGas(t, chainApp, "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: "p1"})
	if limit != app.DefaultQueryGasLimit || used >= limit {
		t.Fatalf("Query/Provider at the bound used %d gas of %d", used, limit)
	}
	pageUsed, _, _ := queryGas(t, chainApp, "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{})
	t.Logf("at the bound: Query/Provider %d bytes, %d gas; Providers page %d gas; query gas limit %d", len(raw), used, pageUsed, limit)

	before := chainApp.FullSnapshot()
	res := n.send(t, c, n.provider1, n.rotate(t, n.provider1, "rot-over", "p1", generation, newSigningKey("p1-over")))
	wantCode(t, "rotation past the bound", res, msgkit.Bounds)
	unchanged(t, "rotation past the bound", chainApp, before, res)
	if _, again := queryProvider(t, chainApp, "p1", 0); !bytes.Equal(raw, again) {
		t.Fatal("Query/Provider changed after the rejected rotation")
	}
	// Updates still work at the bound.
	wantCode(t, "update at the bound", n.send(t, c, n.provider1, n.updateProvider(t, n.provider1, "upd-bound", "p1", capacity1+1, "bound")), msgkit.OK)
}

// TestQueryGasLimit: every query runs on a finite gas meter. With no node
// option the limit is DefaultQueryGasLimit; a node option lowers it, and a
// query above it fails (ABCI query: the SDK's recovered-panic result;
// query router: an out-of-gas panic) while the same query passes on a node
// with the default.
func TestQueryGasLimit(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	used, limit, _ := queryGas(t, c.app(), "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: "p1"})
	if limit != app.DefaultQueryGasLimit {
		t.Fatalf("default query gas limit %d", limit)
	}
	if used == 0 || used >= 1_000_000 {
		t.Fatalf("Query/Provider used %d gas", used)
	}

	low := newRegNet()
	lc := low.startWith(t, 1, nodeOptions{app.QueryGasLimitOption: uint64(1000)})
	wantCode(t, "register p1 on the low-limit node", low.send(t, lc, low.provider1, low.registerProvider(t, low.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	ctx, err := lc.app().CreateQueryContext(0, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ctx.GasMeter().Limit(); got != 1000 {
		t.Fatalf("configured query gas limit %d", got)
	}
	if used <= 1000 {
		t.Fatalf("the probe query uses %d gas, not above the low limit", used)
	}
	_, res := query(t, lc.app(), "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: "p1"}, 0)
	if res.Codespace != errorsmod.UndefinedCodespace || res.Code != errorsmod.ErrPanic.ABCICode() {
		t.Fatalf("ABCI query above the limit: %s/%d %s", res.Codespace, res.Code, res.Log)
	}
	func() {
		defer func() {
			if _, ok := recover().(storetypes.ErrorOutOfGas); !ok {
				t.Fatal("the query router did not run out of gas")
			}
		}()
		queryGas(t, lc.app(), "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: "p1"})
	}()
	// The same query on the default node passes.
	if _, res := query(t, c.app(), "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: "p1"}, 0); res.Code != 0 {
		t.Fatalf("default node: %s/%d", res.Codespace, res.Code)
	}
}

// TestQueryGasLimitOption: the option key is the SDK's, absent or 0 is the
// default (never unlimited), and a value that is not a non-negative
// integer stops the node.
func TestQueryGasLimitOption(t *testing.T) {
	if app.QueryGasLimitOption != server.FlagQueryGasLimit {
		t.Fatalf("option %q, SDK flag %q", app.QueryGasLimitOption, server.FlagQueryGasLimit)
	}
	if app.DefaultQueryGasLimit != genesis.MaxGasPerTx {
		t.Fatalf("default query gas limit %d, transaction gas cap %d", app.DefaultQueryGasLimit, genesis.MaxGasPerTx)
	}
	key := app.QueryGasLimitOption
	for _, c := range []struct {
		name string
		opts nodeOptions
		want uint64
		bad  bool
	}{
		{"no option", nodeOptions{}, app.DefaultQueryGasLimit, false},
		{"zero", nodeOptions{key: uint64(0)}, app.DefaultQueryGasLimit, false},
		{"empty string", nodeOptions{key: ""}, app.DefaultQueryGasLimit, false},
		{"uint64", nodeOptions{key: uint64(5)}, 5, false},
		{"int", nodeOptions{key: 7}, 7, false},
		{"int64", nodeOptions{key: int64(8)}, 8, false},
		{"decimal string", nodeOptions{key: " 12345 "}, 12345, false},
		{"max", nodeOptions{key: uint64(math.MaxUint64)}, math.MaxUint64, false},
		{"negative int", nodeOptions{key: -1}, 0, true},
		{"negative int64", nodeOptions{key: int64(-1)}, 0, true},
		{"negative string", nodeOptions{key: "-5"}, 0, true},
		{"text", nodeOptions{key: "lots"}, 0, true},
		{"float", nodeOptions{key: 1.5}, 0, true},
	} {
		got, err := app.QueryGasLimit(c.opts)
		if c.bad {
			if err == nil {
				t.Errorf("%s: accepted, limit %d", c.name, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: %d %v, want %d", c.name, got, err, c.want)
		}
	}
	if got, err := app.QueryGasLimit(nil); err != nil || got != app.DefaultQueryGasLimit {
		t.Fatalf("nil options: %d %v", got, err)
	}
	if _, err := app.New(app.Config{DB: dbm.NewMemDB(), ChainID: "shortvideo-dev-q", AppOptions: nodeOptions{key: "lots"}}); err == nil {
		t.Fatal("New accepted an invalid query gas limit")
	}
}
