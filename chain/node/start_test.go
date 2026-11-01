package node

import (
	"os"
	"path/filepath"
	"testing"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"

	"github.com/Maxencejules/shortvideo-network/chain/app"
)

// TestNodeFileQueryGasLimit: query_gas_limit in node.json reaches the app
// as app.QueryGasLimitOption; an absent value selects the app default.
func TestNodeFileQueryGasLimit(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) NodeFile {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "config", "node.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		saved, err := loadNodeFile(home)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	for _, c := range []struct {
		body string
		want uint64
	}{
		{`{"moniker":"v1","query_gas_limit":777}`, 777},
		{`{"moniker":"v1"}`, app.DefaultQueryGasLimit},
		{`{"moniker":"v1","query_gas_limit":0}`, app.DefaultQueryGasLimit},
	} {
		saved := write(c.body)
		cfg := AppConfig(log.NewNopLogger(), dbm.NewMemDB(), "shortvideo-dev-node", saved)
		got, err := app.QueryGasLimit(cfg.AppOptions)
		if err != nil || got != c.want {
			t.Fatalf("%s: limit %d %v, want %d", c.body, got, err, c.want)
		}
	}
	if _, err := loadNodeFile(t.TempDir()); err == nil {
		t.Fatal("a home without node.json loaded")
	}
}
