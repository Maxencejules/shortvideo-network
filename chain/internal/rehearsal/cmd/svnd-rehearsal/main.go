// Command svnd-rehearsal is the rehearsal binary B2 of goal 10 (plan 12.1):
// the svnd command tree (chain/cmd/svnd/cli) with the rehearsal upgrade
// handlers compiled in (rehearsal.Upgrades: upgrade "rehearsal-1", app
// version 1 to 2). It declares app version 2, so it refuses to initialize a
// chain, refuses every block below the plan height, and applies the upgrade
// in the block at the plan height. It runs no module migration step: its
// upgrade changes only the rows the upgrade runner owns.
//
// It is built from the same tree as svnd for disposable devnet rehearsals.
// It is not a release, not signed, and not reviewed source.
package main

import (
	"fmt"
	"os"

	"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
)

func main() {
	if err := cli.Root(cli.Options{Upgrades: rehearsal.Upgrades()}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
