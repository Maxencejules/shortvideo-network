package node

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
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

// TestStartWithReadsTheHomeWithUpgradeHandlers: StartWith no longer refuses
// a binary that declares upgrade handlers; it reads the home first with or
// without handlers (the home here has no node.json, which StartWith reports
// as a file error). TestStartWithPassesUpgradeHandlers shows the handlers
// reach the application.
func TestStartWithReadsTheHomeWithUpgradeHandlers(t *testing.T) {
	home := t.TempDir()
	handler := func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
		return migrate(ctx, from)
	}
	for name, start := range map[string]func() error{
		"Start":     func() error { return Start(home) },
		"StartWith": func() error { return StartWith(Config{Home: home}) },
		"StartWith and handler": func() error {
			return StartWith(Config{Home: home, Upgrades: []upgrade.Upgrade{{Name: "v2", FromAppVersion: 1, ToAppVersion: 2, Handler: handler}}})
		},
	} {
		err := start()
		if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "node.json") {
			t.Fatalf("%s on a home without node.json: %v", name, err)
		}
	}
}
