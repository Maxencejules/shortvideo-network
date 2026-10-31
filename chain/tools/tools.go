//go:build tools

// Package tools pins the Cosmos SDK and CometBFT modules recorded in
// docs/dependencies.md. The build tag keeps this package out of `go test`
// and out of any chain binary. Enterprise PoA and Enterprise Groups are
// not imported.
package tools

import (
	_ "github.com/cometbft/cometbft/version"
	_ "github.com/cosmos/cosmos-sdk/version"
)
