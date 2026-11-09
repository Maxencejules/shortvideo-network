package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/pgdev"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

func runDevDB(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dev-db", flag.ContinueOnError)
	fs.SetOutput(stderr)
	chainID := fs.String("chain-id", "", "development chain id (required)")
	secrets := fs.String("secrets-dir", "", "directory for password files (required)")
	binDir := fs.String("bin-dir", "/usr/lib/postgresql/18/bin", "directory of initdb and postgres")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *chainID == "" || *secrets == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: dev-db --chain-id ID --secrets-dir DIR")
		return 2
	}
	if !strings.HasPrefix(*chainID, genesis.DevnetChainIDPrefix) || len(*chainID) > genesis.MaxChainIDLen {
		fmt.Fprintln(stderr, "dev-db needs a shortvideo-dev- chain id")
		return 3
	}
	cluster, err := pgdev.Start(ctx, pgdev.Options{BinDir: *binDir, StartTimeout: 2 * time.Minute})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer cluster.Remove()
	type role struct {
		Role         string `json:"role"`
		Use          string `json:"use"`
		PasswordFile string `json:"password_file"`
	}
	roles := []struct {
		name string
		use  string
	}{
		{store.OwnerRole, "owner"},
		{store.RuntimeRole, "runtime"},
	}
	var out []role
	for _, item := range roles {
		pass, err := cluster.CreateLoginRole(ctx, item.name)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		path := filepath.Join(*secrets, item.name+".password")
		if err := os.WriteFile(path, []byte(pass+"\n"), 0o600); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		out = append(out, role{Role: item.name, Use: item.use, PasswordFile: path})
	}
	if err := cluster.CreateDatabase(ctx, store.Database, store.OwnerRole); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := cluster.GrantConnect(ctx, store.Database, store.RuntimeRole); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	info := map[string]any{
		"chain_id": *chainID, "socket_dir": cluster.SocketDir(), "port": pgdev.Port,
		"database": store.Database, "postgres_version": cluster.Version(), "roles": out,
	}
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(info); err != nil {
		return 1
	}
	<-ctx.Done()
	return 0
}
