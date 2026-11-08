package node_test

import (
	"strings"
	"testing"
	"time"

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
// svnd for real). A StartWith that does not return within 60 s has started
// the node (the handlers did not reach the application): the test fails
// then, and that node runs until the test binary exits.
func TestStartWithPassesUpgradeHandlers(t *testing.T) {
	layout, err := devnet.Init(devnet.Options{Home: t.TempDir(), ChainID: "shortvideo-dev-node-upgrades"})
	if err != nil {
		t.Fatal(err)
	}
	handler := func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
		return migrate(ctx, from)
	}
	done := make(chan error, 1)
	go func() {
		done <- node.StartWith(node.Config{
			Home:     layout.Topology.Validators[0].Home,
			Upgrades: []upgrade.Upgrade{{Name: "v2", FromAppVersion: 1, ToAppVersion: 2, Handler: handler}},
		})
	}()
	const wait = 60 * time.Second
	select {
	case err = <-done:
	case <-time.After(wait):
		t.Fatalf("StartWith did not return within %s: it started the node instead of refusing the app version 2 handler", wait)
	}
	if err == nil || !strings.Contains(err.Error(), "this binary runs app version 2, and a chain starts at app version 1") {
		t.Fatalf("StartWith with an app version 2 handler on a new home: %v", err)
	}
}
