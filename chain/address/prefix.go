// Package address sets the chain's bech32 prefixes once.
package address

import (
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	Account      = "svn"
	AccountPub   = "svnpub"
	Validator    = "svnvaloper"
	ValidatorPub = "svnvaloperpub"
	Consensus    = "svnvalcons"
	ConsensusPub = "svnvalconspub"
)

var once sync.Once

// Configure selects the svn bech32 prefixes and seals the SDK config.
// The prefixes are not the Cosmos Hub prefixes. Sealing is process-wide.
func Configure() {
	once.Do(func() {
		cfg := sdk.GetConfig()
		cfg.SetBech32PrefixForAccount(Account, AccountPub)
		cfg.SetBech32PrefixForValidator(Validator, ValidatorPub)
		cfg.SetBech32PrefixForConsensusNode(Consensus, ConsensusPub)
		cfg.Seal()
	})
}
