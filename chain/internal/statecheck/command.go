package statecheck

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// ErrFailed is the error the statecheck command returns after it printed a
// report that did not pass, so svnd exits 1.
var ErrFailed = errors.New("state check failed")

// Command is the svnd statecheck subcommand of a binary that compiles the
// upgrade handlers upgrades: it reports on a home for that binary's
// description (app.DeclaredBinary(upgrades)).
func Command(upgrades []upgrade.Upgrade) *cobra.Command {
	var home string
	var height int64
	cmd := &cobra.Command{
		Use:   "statecheck",
		Short: "Check the committed state of a stopped node home (test tool, not a release command)",
		Long: `Read the committed application state of a stopped node home, or of a copy
of one, and print a report: the chain id, height, and application hash; the
load gate decision for this binary; one SHA-256 per mounted store; one
SHA-256 over the rows the upgrade I13 self-check protects; and the result of
every invariant checker. It exits 1 when a checker fails or the gate refuses
with row 5 or 6.

The database is opened read-only: nothing in the home is written, and a home
whose node is running is refused. statecheck starts no node, reads no key,
and opens no network connection. It exists only in svnd built with the build
tag svn_statecheck, which links test-support checkers and is not a release.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			binary, err := app.DeclaredBinary(upgrades)
			if err != nil {
				return err
			}
			report, err := Run(Options{Home: home, Height: height, Binary: binary})
			if err != nil {
				return err
			}
			if err := report.WriteText(cmd.OutOrStdout()); err != nil {
				return err
			}
			if !report.Passed() {
				return ErrFailed
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&home, "home", "", "node home directory (a stopped node, or a copy)")
	cmd.Flags().Int64Var(&height, "height", 0, "committed height to read; 0 reads the latest")
	_ = cmd.MarkFlagRequired("home")
	return cmd
}
