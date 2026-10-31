package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/node"
)

func main() {
	if err := root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "svnd",
		Short: "shortvideo-network development chain",
		Long: `svnd is the Goal 05 application chain.

It is scaffolding. Provider, content, lease, evidence, and policy execution
are Goals 06-10. There is no token, mint, distribution, slashing, or staking
module. The initial validator set stays static until Goal 06.

Public launch is disabled. This binary is not a release.`,
	}
	cmd.AddCommand(versionCmd(), startCmd(), genesisCmd(), devnetCmd(), statusCmd())
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the binary and pinned module versions",
		RunE: func(*cobra.Command, []string) error {
			info, ok := debug.ReadBuildInfo()
			if !ok {
				return fmt.Errorf("build info is unavailable")
			}
			fmt.Printf("svnd %s\n", info.Main.Version)
			fmt.Printf("go %s\n", info.GoVersion)
			for _, dep := range info.Deps {
				switch dep.Path {
				case "github.com/cosmos/cosmos-sdk", "github.com/cometbft/cometbft":
					fmt.Printf("%s %s\n", dep.Path, dep.Version)
				}
			}
			return nil
		},
	}
}

func startCmd() *cobra.Command {
	var home string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start one validator from an existing home",
		Long: `Start one validator. A missing home is an error.

start does not create genesis, delete data, or invent block history.
Stop it with SIGINT or SIGTERM. Start it again to resume.`,
		RunE: func(*cobra.Command, []string) error {
			return node.Start(home)
		},
	}
	cmd.Flags().StringVar(&home, "home", "", "validator home directory")
	_ = cmd.MarkFlagRequired("home")
	return cmd
}

func genesisCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "genesis", Short: "Inspect a public genesis document"}
	var file string
	validate := &cobra.Command{
		Use:   "validate",
		Short: "Reject a malformed, duplicate-key, or inconsistent genesis",
		RunE: func(*cobra.Command, []string) error {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			return validateGenesisFile(raw)
		},
	}
	validate.Flags().StringVar(&file, "file", "", "genesis document")
	_ = validate.MarkFlagRequired("file")
	cmd.AddCommand(validate)
	return cmd
}

func validateGenesisFile(raw []byte) error {
	var envelope struct {
		ChainID     string                     `json:"chain_id"`
		GenesisTime string                     `json:"genesis_time"`
		AppState    map[string]json.RawMessage `json:"app_state"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if envelope.AppState != nil {
		doc, err := app.DocumentFromModules(envelope.AppState)
		if err != nil {
			return err
		}
		identity := genesis.CometIdentity{ChainID: envelope.ChainID}
		if envelope.GenesisTime != "" {
			parsed, err := time.Parse(time.RFC3339Nano, envelope.GenesisTime)
			if err != nil {
				return fmt.Errorf("genesis_time: %w", err)
			}
			identity.GenesisUnixSeconds = parsed.Unix()
		}
		return genesis.Validate(doc, identity)
	}
	doc, err := genesis.Parse(raw)
	if err != nil {
		return err
	}
	return genesis.Validate(doc, genesis.CometIdentity{})
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
