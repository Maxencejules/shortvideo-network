// Command svnd is the shortvideo-network application chain binary. Its
// commands are the cli package; svnd compiles no upgrade handler.
package main

import (
	"fmt"
	"os"

	"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli"
)

func main() {
	if err := cli.Root(cli.Options{}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
