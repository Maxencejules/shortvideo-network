//go:build svn_rehearsal

package main

import (
	"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
)

// buildOptions of the rehearsal binary B2 (plan 12.1): the rehearsal
// upgrade "rehearsal-1", app version 1 to 2, run by rehearsal.Handler. The
// binary declares app version 2, so it refuses to initialize a chain,
// refuses every block below the plan height, and applies the upgrade in the
// block at the plan height. It runs no module migration step: its upgrade
// changes only the rows the upgrade runner owns. It starts only on a chain
// whose id starts with genesis.DevnetChainIDPrefix.
//
// It is built from the same tree as svnd for disposable devnet rehearsals.
// It is not a release, not signed, and not reviewed source.
func buildOptions() cli.Options {
	return cli.Options{Upgrades: rehearsal.Upgrades(), ChainIDPrefix: genesis.DevnetChainIDPrefix}
}
