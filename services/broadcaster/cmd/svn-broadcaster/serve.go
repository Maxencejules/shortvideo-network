package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/worker"
)

// signerClientSlack is added to the configured RPC timeout to bound one
// request to the signer process, which may read its node's /status before
// it signs.
const signerClientSlack = 10 * time.Second

// serveDeps are the collaborators serve and reconcile-once build from the
// serve configuration.
type serveDeps struct {
	cfg      *config.Config
	db       *sql.DB
	store    *store.Store
	codec    *chain.Codec
	chain    *chain.Client
	registry *policy.Registry
	signer   *signer.Client
	keys     []signer.KeyInfo
}

// close releases the database pool.
func (d *serveDeps) close() error { return d.db.Close() }

// openServe loads the serve configuration and runs the start-up checks of
// serve and reconcile-once, in this order: the configuration's sections
// and database role, the process uid, the database schema, the chain id
// of the node and of the database binding, the intent-key registry, and
// the signer process's keys.
func openServe(ctx context.Context, cfgPath string, env processEnv, log *slog.Logger, role string) (*serveDeps, error) {
	c, err := loadConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	if err := c.RequireServe(); err != nil {
		return nil, refusedError(err)
	}
	if err := c.CheckProcessUID(false, env.euid); err != nil {
		return nil, refusedError(err)
	}
	log.Info("configuration loaded", "chain_id", c.ChainID, "boundary", c.Signer.Boundary().String())
	logNumbers(log, c)
	conn, err := c.ConnConfig()
	if err != nil {
		return nil, refusedError(err)
	}
	db, err := store.OpenDB(conn)
	if err != nil {
		return nil, refusedError(err)
	}
	d := &serveDeps{cfg: c, db: db}
	if err := d.check(ctx, role); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return d, nil
}

// logNumbers logs every PROPOSED number in use, one record each (plan
// section 11: the values are labelled PROPOSED, none is an owner
// decision). The name is a value, not an attribute key, so the redacting
// handler does not drop numbers whose names contain a denied word.
func logNumbers(log *slog.Logger, c *config.Config) {
	for _, entry := range c.Numbers.Describe() {
		name, rest, _ := strings.Cut(entry, "=")
		value, label, _ := strings.Cut(rest, " ")
		log.Info("number in use", "number", name, "value", value, "label", strings.Trim(label, "()"))
	}
}

// check runs the checks of openServe that need the database pool.
func (d *serveDeps) check(ctx context.Context, role string) error {
	c := d.cfg
	if err := checkSchema(ctx, d.db); err != nil {
		return err
	}
	codec, err := chain.NewCodec(c.ChainID)
	if err != nil {
		return refusedError(err)
	}
	rpc, err := chain.NewHTTP(c.RPCURL, c.Numbers.RPCTimeout())
	if err != nil {
		return refusedError(err)
	}
	client, err := chain.New(rpc, codec)
	if err != nil {
		return refusedError(err)
	}
	if err := c.VerifyChainID(ctx, client, func(ctx context.Context, id string) error {
		return store.CheckChain(ctx, d.db, id)
	}); err != nil {
		if errors.Is(err, config.ErrChainIDMismatch) || errors.Is(err, chain.ErrChainID) || errors.Is(err, store.ErrChainUnbound) {
			return refusedError(err)
		}
		return err
	}
	instance, err := instanceID(role)
	if err != nil {
		return err
	}
	st, err := store.New(d.db, c.StoreLimits(), instance)
	if err != nil {
		return refusedError(err)
	}
	registry, err := policy.LoadRegistry(c.IntentKeyRegistry, c.Signer.Boundary())
	if err != nil {
		return refusedError(err)
	}
	timeout := min(c.Numbers.RPCTimeout()+signerClientSlack, signer.MaxClientTimeout)
	sc, err := signer.NewClient(c.Signer.SocketPath, codec, timeout)
	if err != nil {
		return refusedError(err)
	}
	keyInfos, err := sc.Keys(ctx)
	if err != nil {
		return startError(fmt.Errorf("signer keys: %w", err))
	}
	if err := checkKeyClasses(keyInfos); err != nil {
		return refusedError(err)
	}
	d.store, d.codec, d.chain, d.registry, d.signer, d.keys = st, codec, client, registry, sc, keyInfos
	return nil
}

// checkSchema refuses a database whose applied migrations are not the
// binary's (behind, newer, changed or with a gap); any other error is a
// run-time failure.
func checkSchema(ctx context.Context, db *sql.DB) error {
	err := store.CheckSchema(ctx, db)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrSchemaBehind), errors.Is(err, store.ErrDatabaseNewer),
		errors.Is(err, store.ErrMigrationChecksum), errors.Is(err, store.ErrMigrationGap):
		return refusedError(err)
	}
	return fmt.Errorf("check schema: %w", err)
}

// startError maps an error of a start-up check that reads another
// process: a network error (the node or the signer did not answer) is a
// run-time failure; any other error is a refused start.
func startError(err error) error {
	var ne net.Error
	if errors.As(err, &ne) {
		return err
	}
	return refusedError(err)
}

// checkKeyClasses requires exactly one SERVICE and one BROADCASTER key
// from the signer (owner answer Q1).
func checkKeyClasses(keyInfos []signer.KeyInfo) error {
	seen := map[policy.Class]bool{}
	for _, k := range keyInfos {
		if k.Class != policy.ClassService && k.Class != policy.ClassBroadcaster {
			return fmt.Errorf("signer offers a key of class %q", k.Class)
		}
		if seen[k.Class] {
			return fmt.Errorf("signer offers two keys of class %s", k.Class)
		}
		seen[k.Class] = true
	}
	if len(seen) != 2 {
		return errors.New("signer must offer one SERVICE and one BROADCASTER key")
	}
	return nil
}

// instanceID is the writing process's identity in attempts and
// transitions rows: the role, the pid and a random boot nonce.
func instanceID(role string) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("boot nonce: %w", err)
	}
	return fmt.Sprintf("%s-%d-%s", role, os.Getpid(), hex.EncodeToString(nonce[:])), nil
}

// workerConfig is the submission engine's configuration over d.
func (d *serveDeps) workerConfig(log *slog.Logger) (worker.Config, error) {
	n, err := d.cfg.WorkerNumbers()
	if err != nil {
		return worker.Config{}, refusedError(err)
	}
	return worker.Config{
		Store:            d.store,
		Chain:            d.chain,
		RequestSignature: d.signer.RequestSignature,
		Keys:             d.keys,
		Registry:         d.registry,
		Callers:          d.cfg.PolicyCallers(),
		Outbox:           d.cfg.OutboxLimits(),
		Numbers:          n,
		Logger:           log,
	}, nil
}

// apiSigners are the signer keys as the API publishes and pins them.
func (d *serveDeps) apiSigners() []api.SignerKey {
	out := make([]api.SignerKey, 0, len(d.keys))
	for _, k := range d.keys {
		out = append(out, api.SignerKey{Class: k.Class, Address: k.Address, PublicKey: k.PubKey.Bytes(), Algorithm: k.Algorithm})
	}
	return out
}

// runServe is the serve subcommand: the caller API and the submission
// engine until SIGINT or SIGTERM.
func runServe(ctx context.Context, args []string, env processEnv, log *slog.Logger) error {
	fs := newFlags("serve", env)
	cfgPath := configFlag(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	d, err := openServe(ctx, *cfgPath, env, log, "serve")
	if err != nil {
		return err
	}
	defer d.close()
	apiCfg, err := d.cfg.APIConfig()
	if err != nil {
		return refusedError(err)
	}
	srv, err := api.New(apiCfg, api.Deps{Store: d.store, Chain: d.chain, Codec: d.codec, Registry: d.registry,
		Signers: d.apiSigners(), Logger: log})
	if err != nil {
		return refusedError(err)
	}
	wcfg, err := d.workerConfig(log)
	if err != nil {
		return err
	}
	w, err := worker.New(wcfg)
	if err != nil {
		return refusedError(err)
	}
	return serveUntilDone(ctx, log, srv.ListenAndServe, w.Run)
}

// serveUntilDone runs the API and the worker until ctx ends or either
// stops on its own, then stops the other and waits for it. A stop because
// ctx ended is clean; any other stop is the error it returned.
func serveUntilDone(ctx context.Context, log *slog.Logger, serveAPI, runWorker func(context.Context) error) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		name string
		err  error
	}
	done := make(chan result, 2)
	go func() { done <- result{"api", serveAPI(runCtx)} }()
	go func() { done <- result{"worker", runWorker(runCtx)} }()
	log.Info("serving")
	results := [2]result{<-done}
	cancel()
	results[1] = <-done
	// stopped: a signal ended ctx, so both were asked to stop.
	stopped := ctx.Err() != nil
	var errs []error
	for i, r := range results {
		switch {
		case r.err == nil:
		case errors.Is(r.err, context.Canceled) && (stopped || i == 1):
			// It stopped because this function cancelled its context.
		default:
			errs = append(errs, fmt.Errorf("%s: %w", r.name, r.err))
		}
	}
	if !stopped && len(errs) == 0 {
		errs = append(errs, fmt.Errorf("%s stopped on its own", results[0].name))
	}
	if len(errs) == 0 {
		log.Info("stopped")
	}
	return errors.Join(errs...)
}

// runReconcileOnce is the reconcile-once subcommand: one pass of every
// worker loop under the serve configuration.
func runReconcileOnce(ctx context.Context, args []string, env processEnv, log *slog.Logger) error {
	fs := newFlags("reconcile-once", env)
	cfgPath := configFlag(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	d, err := openServe(ctx, *cfgPath, env, log, "reconcile-once")
	if err != nil {
		return err
	}
	defer d.close()
	wcfg, err := d.workerConfig(log)
	if err != nil {
		return err
	}
	w, err := worker.New(wcfg)
	if err != nil {
		return refusedError(err)
	}
	if err := w.ReconcileOnce(ctx); err != nil {
		if errors.Is(err, worker.ErrLockHeld) {
			return lockHeldError(err)
		}
		return err
	}
	log.Info("reconcile pass complete")
	return nil
}
