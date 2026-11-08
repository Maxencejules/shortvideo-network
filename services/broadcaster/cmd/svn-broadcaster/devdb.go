package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgdev"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// dev-db defaults and bounds.
const (
	// defaultPGBinDir is the PostgreSQL 18 install of scripts/env (goal 04
	// pin).
	defaultPGBinDir       = "/usr/lib/postgresql/18/bin"
	defaultPGStartTimeout = 2 * time.Minute
	maxLoginRoles         = 16
	// devDatabase is the outbox database dev-db creates, owned by the
	// owner role.
	devDatabase = "broadcaster"
)

var pgRoleRE = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// roleList is the repeatable --login-role flag.
type roleList []string

func (r *roleList) String() string { return fmt.Sprint([]string(*r)) }

func (r *roleList) Set(v string) error {
	switch {
	case len(*r) == maxLoginRoles:
		return fmt.Errorf("at most %d login roles", maxLoginRoles)
	case !pgRoleRE.MatchString(v):
		return errors.New("a role must match ^[a-z_][a-z0-9_]{0,62}$")
	case strings.HasPrefix(v, "pg_") || v == "postgres" || v == pgdev.Superuser || v == store.OwnerRole || v == store.RuntimeRole:
		return errors.New("a login role may not be reserved, the superuser, the owner or the runtime role")
	}
	for _, have := range *r {
		if have == v {
			return errors.New("a login role is named twice")
		}
	}
	*r = append(*r, v)
	return nil
}

// devDBInfo is dev-db's one output line: where the cluster is and which
// file holds each role's password. It holds no password.
type devDBInfo struct {
	ChainID         string      `json:"chain_id"`
	ClusterDir      string      `json:"cluster_dir"`
	SocketDir       string      `json:"socket_dir"`
	Port            int         `json:"port"`
	Database        string      `json:"database"`
	PostgresVersion string      `json:"postgres_version"`
	Roles           []devDBRole `json:"roles"`
}

type devDBRole struct {
	Role string `json:"role"`
	// Use is "owner" (migrate), "runtime" (serve, reconcile-once,
	// signer-clear) or "login" (a role with CONNECT only: a caller's SQL
	// path once migrate --callers maps it, or a test's reader role once
	// the owner grants it SELECT).
	Use          string `json:"use"`
	PasswordFile string `json:"password_file"`
}

// runDevDB is the dev-db subcommand (DR-11-24): a disposable PostgreSQL
// cluster for a development chain. The cluster lives in
// os.TempDir()/svn-pg-* (internal/pgdev refuses any other place), listens
// on a Unix socket only, and dies with this process. Each role's password
// is written to its own new file in --secrets-dir (mode 0600; the runtime
// role's file owned by --serve-uid); the files are removed at exit.
func runDevDB(ctx context.Context, args []string, env processEnv, log *slog.Logger) error {
	flags := newFlags("dev-db", env)
	chainID := flags.String("chain-id", "", "the development chain id, starting with "+genesis.DevnetChainIDPrefix+" (required)")
	secrets := flags.String("secrets-dir", "", "absolute path of an existing directory of this uid, without group or other write bits, for the password files (required)")
	serveUID := flags.Int64("serve-uid", -1, "uid that owns the runtime role's password file (default: this process's uid; another uid needs root)")
	var logins roleList
	flags.Var(&logins, "login-role", "an extra login role with CONNECT on the database (repeatable)")
	binDir := flags.String("bin-dir", defaultPGBinDir, "absolute directory of initdb and postgres")
	startTimeout := flags.Duration("start-timeout", defaultPGStartTimeout, "bound of initdb and of the wait for the server")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	switch {
	case !strings.HasPrefix(*chainID, genesis.DevnetChainIDPrefix) || len(*chainID) > genesis.MaxChainIDLen:
		return refusedError(fmt.Errorf("dev-db needs a chain id of at most %d bytes starting with %s", genesis.MaxChainIDLen, genesis.DevnetChainIDPrefix))
	case *startTimeout < pgdev.MinStartTimeout || *startTimeout > pgdev.MaxStartTimeout:
		return usageError(fmt.Errorf("--start-timeout must be %s to %s", pgdev.MinStartTimeout, pgdev.MaxStartTimeout))
	case *binDir == "" || !filepath.IsAbs(*binDir) || filepath.Clean(*binDir) != *binDir:
		return usageError(errors.New("--bin-dir must be a clean absolute path"))
	}
	runtimeOwner, err := fileOwner(*serveUID, env.euid)
	if err != nil {
		return err
	}
	if err := checkSecretsDir(*secrets, env.euid); err != nil {
		return err
	}
	for _, name := range append([]string{store.OwnerRole, store.RuntimeRole}, logins...) {
		if _, err := os.Lstat(filepath.Join(*secrets, name+".password")); !errors.Is(err, fs.ErrNotExist) {
			return refusedError(fmt.Errorf("--secrets-dir already holds %s.password", name))
		}
	}
	cluster, err := pgdev.Start(ctx, pgdev.Options{BinDir: *binDir, StartTimeout: *startTimeout})
	if err != nil {
		if errors.Is(err, pgdev.ErrOptions) || errors.Is(err, pgdev.ErrDirectory) || errors.Is(err, pgdev.ErrNotTraversable) {
			return refusedError(err)
		}
		return fmt.Errorf("start PostgreSQL: %w", err)
	}
	var written []string
	defer func() {
		for _, p := range written {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Error("remove password file", "path", p, "error", err)
			}
		}
		if err := cluster.Remove(); err != nil {
			log.Error("remove cluster", "error", err)
		}
	}()
	info := devDBInfo{ChainID: *chainID, ClusterDir: cluster.Dir(), SocketDir: cluster.SocketDir(), Port: pgdev.Port,
		Database: devDatabase, PostgresVersion: cluster.Version()}
	type roleSpec struct {
		name, use string
		owner     uint32
	}
	specs := []roleSpec{{store.OwnerRole, "owner", env.euid}, {store.RuntimeRole, "runtime", runtimeOwner}}
	for _, r := range logins {
		specs = append(specs, roleSpec{r, "login", env.euid})
	}
	for i, r := range specs {
		pass, err := cluster.CreateLoginRole(ctx, r.name)
		if err != nil {
			return fmt.Errorf("create role %s: %w", r.name, err)
		}
		if i == 0 {
			if err := cluster.CreateDatabase(ctx, devDatabase, store.OwnerRole); err != nil {
				return fmt.Errorf("create database: %w", err)
			}
		} else if err := cluster.GrantConnect(ctx, devDatabase, r.name); err != nil {
			return fmt.Errorf("grant connect to %s: %w", r.name, err)
		}
		path := filepath.Join(*secrets, r.name+".password")
		if err := writeNew(path, []byte(pass+"\n"), 0o600, r.owner, env.euid); err != nil {
			return err
		}
		written = append(written, path)
		info.Roles = append(info.Roles, devDBRole{Role: r.name, Use: r.use, PasswordFile: path})
	}
	if err := json.NewEncoder(env.stdout).Encode(info); err != nil {
		return fmt.Errorf("write cluster description: %w", err)
	}
	log.Info("development database running", "chain_id", *chainID, "cluster_dir", info.ClusterDir,
		"postgres_version", info.PostgresVersion)
	_, exited, ok := cluster.Postmaster()
	if !ok {
		return errors.New("PostgreSQL is not running")
	}
	select {
	case <-ctx.Done():
		log.Info("stopping the development database")
		return nil
	case <-exited:
		return errors.New("PostgreSQL exited on its own")
	}
}

// checkSecretsDir refuses a password-file directory that is not a real
// directory owned by euid, or that has a group or other write bit.
func checkSecretsDir(dir string, euid uint32) error {
	if dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return usageError(errors.New("--secrets-dir must be a clean absolute path"))
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return refusedError(fmt.Errorf("--secrets-dir: %w", err))
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.IsDir() || info.Mode()&fs.ModeSymlink != 0:
		return refusedError(errors.New("--secrets-dir is not a real directory"))
	case !ok || st.Uid != euid:
		return refusedError(errors.New("--secrets-dir is not owned by this process's uid"))
	case info.Mode().Perm()&0o022 != 0:
		return refusedError(fmt.Errorf("--secrets-dir mode %04o has a group or other write bit", info.Mode().Perm()))
	}
	return nil
}
