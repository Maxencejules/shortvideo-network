package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/spf13/cobra"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

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
	if err := genesis.RejectPrivateMaterial(raw); err != nil {
		return err
	}
	genDoc, cometErr := cmttypes.GenesisDocFromJSON(raw)
	if cometErr == nil && len(genDoc.AppState) > 0 {
		// InitChain's consensus params rule, on the params CometBFT's
		// handshake hands InitChain for this document. GenesisDocFromJSON
		// has filled in CometBFT's defaults (Version.App 0) for missing
		// consensus params.
		params := genDoc.ConsensusParams.ToProto()
		if err := app.ValidateGenesisConsensusParams(&params); err != nil {
			return err
		}
		var modules map[string]json.RawMessage
		if err := json.Unmarshal(genDoc.AppState, &modules); err != nil {
			return err
		}
		doc, err := app.DocumentFromModules(modules)
		if err != nil {
			return err
		}
		pubs := make([]string, len(genDoc.Validators))
		powers := make([]int64, len(genDoc.Validators))
		for i, validator := range genDoc.Validators {
			if validator.PubKey == nil {
				return fmt.Errorf("comet validator %d has no public key", i)
			}
			pubs[i] = hex.EncodeToString(validator.PubKey.Bytes())
			powers[i] = validator.Power
		}
		if err := genesis.Validate(doc, genesis.CometIdentity{
			ChainID:            genDoc.ChainID,
			GenesisUnixSeconds: genDoc.GenesisTime.Unix(),
			ValidatorPubkeyHex: pubs,
			ValidatorPowers:    powers,
		}); err != nil {
			return err
		}
		// The authority rules InitChain applies: pending entries, genesis
		// proposals, approved council, quorum loss, the start point.
		return app.ValidateAuthorityGenesis(modules, genDoc.InitialHeight, genDoc.GenesisTime.Unix())
	}
	doc, err := genesis.Parse(raw)
	if err != nil {
		if cometErr != nil {
			return fmt.Errorf("malformed genesis: %w", cometErr)
		}
		return err
	}
	return genesis.Validate(doc, genesis.CometIdentity{})
}
