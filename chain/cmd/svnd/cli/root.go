// Package cli is the svnd command tree. A main package calls Root with the
// upgrade handlers compiled into its binary: svnd passes none, so a second
// binary (for example an upgrade rehearsal binary) reuses the same
// commands with its own handlers.
package cli

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/node"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// Options are what one binary compiles into the command tree.
type Options struct {
	// Upgrades are the upgrade handlers compiled into the binary. start
	// passes them to the node; version and the help text list them.
	Upgrades []upgrade.Upgrade
}

// Root returns the svnd command tree for a binary with opts.
func Root(opts Options) *cobra.Command {
	upgrades := append([]upgrade.Upgrade(nil), opts.Upgrades...)
	cmd := &cobra.Command{
		Use:   "svnd",
		Short: "shortvideo-network development chain",
		Long:  longHelp(upgrades),
	}
	cmd.AddCommand(versionCmd(upgrades, debug.ReadBuildInfo), startCmd(upgrades), genesisCmd(), devnetCmd(), statusCmd())
	return cmd
}

func longHelp(upgrades []upgrade.Upgrade) string {
	compiled := "none"
	if len(upgrades) > 0 {
		names := make([]string, len(upgrades))
		for i, up := range upgrades {
			names[i] = fmt.Sprintf("%s (app version %d to %d)", up.Name, up.FromAppVersion, up.ToAppVersion)
		}
		compiled = strings.Join(names, ", ")
	}
	return `svnd is the shortvideo-network application chain.

x/authority runs the devnet council and is the only validator-set writer.
x/providers and x/content are the provider and content registries. x/leases
holds lease offers, leases, and their capacity reservations. x/evidence
records signed audit attestations of held leases; a record is an
attestation, not a storage proof. Policy has no message service: a governed
limit changes only through an executed council proposal. There is no token,
mint, distribution, slashing, or staking module.

Compiled upgrade handlers: ` + compiled + `.

Public launch is disabled. This binary is not a release.`
}

func startCmd(upgrades []upgrade.Upgrade) *cobra.Command {
	var home string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start one validator from an existing home",
		Long: `Start one validator. A missing home is an error.

start does not create genesis, delete data, or invent block history.
Stop it with SIGINT or SIGTERM. Start it again to resume.`,
		RunE: func(*cobra.Command, []string) error {
			return node.StartWith(node.Config{Home: home, Upgrades: upgrades})
		},
	}
	cmd.Flags().StringVar(&home, "home", "", "validator home directory")
	_ = cmd.MarkFlagRequired("home")
	return cmd
}

func devnetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devnet",
		Short: "Create a disposable four-validator devnet",
	}
	var home, peers, chainID string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write four homes and one validated genesis",
		Long: `Create a new home. Refuses to replace an existing one.

Keys stay in the home directory. Nothing is written to the repository.`,
		RunE: func(*cobra.Command, []string) error {
			layout, err := devnet.Init(devnet.Options{
				Home:    home,
				ChainID: chainID,
				Peers:   devnet.PeerMode(peers),
			})
			if err != nil {
				return err
			}
			raw, err := json.MarshalIndent(layout.Topology, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		},
	}
	initCmd.Flags().StringVar(&home, "home", "", "devnet home directory")
	initCmd.Flags().StringVar(&peers, "peers", string(devnet.PeerLoopback), "loopback or compose")
	initCmd.Flags().StringVar(&chainID, "chain-id", "", "chain id; generated when empty")
	_ = initCmd.MarkFlagRequired("home")
	cmd.AddCommand(initCmd)
	return cmd
}

func statusCmd() *cobra.Command {
	var rpc string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print one validator's committed height and application hash",
		RunE: func(*cobra.Command, []string) error {
			status, err := devnet.ReadStatus(rpc)
			if err != nil {
				return err
			}
			fmt.Printf("height %d\napp_hash %s\n", status.Height, status.AppHash)
			return nil
		},
	}
	cmd.Flags().StringVar(&rpc, "rpc", "", "CometBFT RPC base URL")
	_ = cmd.MarkFlagRequired("rpc")
	return cmd
}
