package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/api"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/config"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/engine"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/status"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

func run(ctx context.Context, args, environ []string, stdout, stderr io.Writer) int {
	if err := config.CheckEnvironment(environ); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 3
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: svn-indexer <migrate|dev-db|serve|backfill|reconcile-once|status|rebuild|canonical> [flags]")
		return 2
	}
	switch args[0] {
	case "dev-db":
		return runDevDB(ctx, args[1:], stdout, stderr)
	case "migrate", "serve", "backfill", "reconcile-once", "status", "rebuild", "canonical":
		return runConfig(ctx, args[0], args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		return 2
	}
}

func runConfig(ctx context.Context, cmd string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("config", "", "configuration file (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: --config FILE")
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 3
	}
	role := cfg.Database.Role
	if cmd == "migrate" || cmd == "rebuild" {
		role = cfg.Database.OwnerRole
	}
	passPath := cfg.Database.PasswordFile
	if role == cfg.Database.OwnerRole {
		passPath = cfg.Database.OwnerPasswordFile
	}
	pass, err := config.Password(passPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	db, err := store.Open(store.Conn{
		SocketDir: cfg.Database.SocketDir, Port: cfg.Database.Port, Database: cfg.Database.Name,
		User: role, Password: pass, ApplicationName: cfg.Database.ApplicationName,
		ConnectTimeout: time.Duration(cfg.Database.ConnectTimeoutSeconds) * time.Second,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer db.Close()
	switch cmd {
	case "migrate":
		if err := store.Migrate(ctx, db); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := store.Bind(ctx, db, cfg.ChainID); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 3
		}
		fmt.Fprintln(stdout, "migrated")
		return 0
	case "rebuild":
		st := store.New(db, cfg.ChainID)
		if err := st.Rebuild(ctx, cfg.ChainID); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return exitCode(err)
		}
		fmt.Fprintln(stdout, "rebuilt")
		return 0
	case "canonical":
		text, err := store.New(db, cfg.ChainID).Canonical(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprint(stdout, text)
		return 0
	case "status":
		view, err := readView(ctx, store.New(db, cfg.ChainID), cfg)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(view); err != nil {
			return 1
		}
		return 0
	}
	raw, err := chain.Dial(cfg.RPCURL, cfg.Numbers.RPCTimeout())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 3
	}
	client, err := chain.New(cfg.ChainID, raw)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 3
	}
	eng := &engine.Engine{Chain: client, Store: store.New(db, cfg.ChainID), ChainID: cfg.ChainID}
	switch cmd {
	case "backfill":
		if _, err := eng.CatchUp(ctx); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return exitCode(err)
		}
		fmt.Fprintln(stdout, "backfill")
		return 0
	case "reconcile-once":
		result, err := eng.CatchUp(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return exitCode(err)
		}
		state, err := eng.Reconcile(ctx, result.Floor)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, state)
		if state == "divergent" {
			return 5
		}
		if state == "blocked" {
			return 4
		}
		return 0
	case "serve":
		return serve(ctx, cfg, eng, stdout, stderr)
	default:
		return 2
	}
}

func serve(ctx context.Context, cfg config.File, eng *engine.Engine, stdout, stderr io.Writer) int {
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer ln.Close()
	fmt.Fprintf(stdout, "{\"listen\":%q}\n", ln.Addr().String())
	reader := dbReader{db: eng.Store, cfg: cfg}
	srv := &http.Server{Handler: api.Server{Reader: reader}.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	var last time.Time
	tick := time.NewTicker(cfg.Numbers.Poll())
	defer tick.Stop()
	for {
		result, err := eng.CatchUp(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			if fatal(err) {
				return exitCode(err)
			}
		} else if time.Since(last) >= cfg.Numbers.ReconcileEvery() {
			if _, recErr := eng.Reconcile(ctx, result.Floor); recErr != nil {
				fmt.Fprintln(stderr, recErr.Error())
			}
			last = time.Now()
		}
		select {
		case <-ctx.Done():
			return 0
		case <-tick.C:
		}
	}
}

type dbReader struct {
	db  *store.Store
	cfg config.File
}

func (r dbReader) View(ctx context.Context) (status.View, error) {
	return readView(ctx, r.db, r.cfg)
}

func (r dbReader) Load(ctx context.Context) (store.Projection, error) {
	return r.db.Load(ctx)
}

func readView(ctx context.Context, st *store.Store, cfg config.File) (status.View, error) {
	cp, err := st.Checkpoint(ctx)
	if err != nil {
		return status.View{}, err
	}
	obs, err := st.Observation(ctx)
	if err != nil {
		return status.View{}, err
	}
	run, err := st.LatestRun(ctx)
	if err != nil {
		return status.View{}, err
	}
	height := int64(0)
	if cp.Found {
		height = cp.Height
	}
	return status.Evaluate(status.Input{
		ChainID: cfg.ChainID, Gateway: cfg.RPCURL, IndexedHeight: height, HaveIndex: cp.Found,
		Obs: obs, Run: run, StaleAfter: cfg.Numbers.StaleAfter(), ObserverFresh: cfg.Numbers.ObserverFresh(),
	}), nil
}

func fatal(err error) bool {
	return errors.Is(err, chain.ErrChainID) || errors.Is(err, chain.ErrForged) ||
		errors.Is(err, project.ErrEventVersion) || errors.Is(err, project.ErrMalformed) ||
		errors.Is(err, store.ErrInconsistent) || errors.Is(err, store.ErrConflict) ||
		errors.Is(err, store.ErrGap) || errors.Is(err, store.ErrChain)
}

func exitCode(err error) int {
	switch {
	case errors.Is(err, chain.ErrHistoryUnavailable):
		return 4
	case fatal(err):
		return 3
	default:
		return 1
	}
}
