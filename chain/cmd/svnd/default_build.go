//go:build !svn_rehearsal && !svn_rehearsal_bad

package main

import "github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"

// buildOptions of the default build: no upgrade handler and no chain id
// restriction. This is the release build.
func buildOptions() cli.Options {
	return cli.Options{}
}
