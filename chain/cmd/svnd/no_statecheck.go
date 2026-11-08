//go:build !svn_statecheck

package main

import (
	"github.com/spf13/cobra"

	"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"
)

// statecheckBuild of a build without the build tag svn_statecheck, the
// release build among them: the options unchanged and no command, so the
// build links no state-check code.
func statecheckBuild(opts cli.Options) (cli.Options, []*cobra.Command) {
	return opts, nil
}
