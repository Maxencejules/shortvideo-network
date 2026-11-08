//go:build svn_rehearsal_bad

package main

import (
	"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
)

// buildOptions of the rehearsal binary B2-bad (plan 12.1, case I6): the
// same upgrade name and app versions as B2, run by rehearsal.BadHandler,
// which writes one documented row outside every check of the upgrade runner
// (rehearsal.BadRowKey -> rehearsal.BadRowValue in the x/leases store). A
// node that runs it commits another app hash at the plan height than the
// nodes that run B2. It starts only on a chain whose id starts with
// genesis.DevnetChainIDPrefix.
//
// It exists only to show the wrong-migration failure on a disposable
// devnet. It is not a release, not signed, and not reviewed source.
func buildOptions() cli.Options {
	return cli.Options{Upgrades: rehearsal.BadUpgrades(), ChainIDPrefix: genesis.DevnetChainIDPrefix}
}
