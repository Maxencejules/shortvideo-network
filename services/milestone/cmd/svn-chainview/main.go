// Command svn-chainview serves the provider agent's HTTPS chain view
// from one CometBFT node. It does not sign.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Maxencejules/shortvideo-network/services/milestone/internal/chainview"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("svn-chainview", flag.ContinueOnError)
	rpc := fs.String("rpc", "", "CometBFT RPC base URL")
	listen := fs.String("listen", "127.0.0.1:0", "loopback listen address")
	account := fs.String("account", "", "bech32 account this view reports")
	var providers stringList
	fs.Var(&providers, "provider", "provider id to query (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *rpc == "" || *account == "" || len(providers) == 0 {
		fmt.Fprintln(os.Stderr, "usage: svn-chainview --rpc URL --account BECH32 --provider ID")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	url, ca, err := chainview.Listen(ctx, *listen, chainview.Server{
		RPC: *rpc, Account: *account, Providers: providers,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"url": url, "ca_file": ca}); err != nil {
		return 1
	}
	<-ctx.Done()
	return 0
}

type stringList []string

func (s *stringList) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
