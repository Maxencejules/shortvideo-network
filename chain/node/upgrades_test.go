package node_test

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/node"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// TestStartWithPassesUpgradeHandlers: StartWith hands the binary's upgrade
// handlers to the application. A binary with a handler from app version 1
// to 2 runs app version 2, and a chain starts at app version 1, so on a new
// devnet home the CometBFT handshake's InitChain refuses it and StartWith
// returns that error before the node starts (no port is bound). The same
// home without handlers passes the handshake (TestDevnet*, which start
// svnd for real).
func TestStartWithPassesUpgradeHandlers(t *testing.T) {
	layout, err := devnet.Init(devnet.Options{Home: t.TempDir(), ChainID: "shortvideo-dev-node-upgrades"})
	if err != nil {
		t.Fatal(err)
	}
	handler := func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
		return migrate(ctx, from)
	}
	err = node.StartWith(node.Config{
		Home:     layout.Topology.Validators[0].Home,
		Upgrades: []upgrade.Upgrade{{Name: "v2", FromAppVersion: 1, ToAppVersion: 2, Handler: handler}},
	})
	if err == nil || !strings.Contains(err.Error(), "this binary runs app version 2, and a chain starts at app version 1") {
		t.Fatalf("StartWith with an app version 2 handler on a new home: %v", err)
	}
}
