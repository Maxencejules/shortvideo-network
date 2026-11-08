//go:build svn_statecheck

package main

import (
	"github.com/spf13/cobra"

	"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/internal/statecheck"
)

// statecheckBuild of a build with the build tag svn_statecheck: the
// statecheck subcommand (chain/internal/statecheck), which reports on a
// stopped node home for this build's binary description (its upgrade
// handlers), and the devnet chain id restriction on start
// (genesis.DevnetChainIDPrefix), so a build that links the test-support
// invariant checkers (leasestest, evidencetest, policytest), which the
// default svnd build must not link, starts a validator only on a devnet.
// The tag combines with svn_rehearsal or svn_rehearsal_bad: that build
// describes the rehearsal binary. A build with this tag is a test tool, not
// a release.
func statecheckBuild(opts cli.Options) (cli.Options, []*cobra.Command) {
	opts.ChainIDPrefix = genesis.DevnetChainIDPrefix
	return opts, []*cobra.Command{statecheck.Command(opts.Upgrades)}
}
