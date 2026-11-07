// Command svnd is the shortvideo-network application chain binary. Its
// commands are the cli package.
//
// One main package builds the release svnd and the tagged test builds
// (orchestrator choice ORCH-6, a reversible implementation choice, not an
// owner decision). buildOptions, defined by exactly one file for each
// build, says what the build compiles into the command tree:
//
//   - no build tag (default_build.go): svnd, the only release build. It
//     compiles no upgrade handler and links no chain/internal code.
//   - -tags svn_rehearsal (rehearsal_tag.go): the rehearsal binary B2 of
//     goal 10, with the rehearsal upgrade handler.
//   - -tags svn_rehearsal_bad (rehearsal_bad_tag.go): the rehearsal binary
//     B2-bad, with the wrong rehearsal handler of the same upgrade name.
//
// The two rehearsal tags together define buildOptions twice, so that build
// does not compile. `svnd version` names the compiled upgrade handlers: the
// default build prints "upgrades none".
//
// statecheckBuild, defined by statecheck_tag.go under the build tag
// svn_statecheck and by no_statecheck.go otherwise, adds the statecheck
// subcommand and the devnet chain id restriction on start to a test build
// (svn_statecheck alone, or with a rehearsal tag); the default build gets
// neither.
package main

import (
	"fmt"
	"os"

	"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"
)

func main() {
	opts, commands := statecheckBuild(buildOptions())
	root := cli.Root(opts)
	root.AddCommand(commands...)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
