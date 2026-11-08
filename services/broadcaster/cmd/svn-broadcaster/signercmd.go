package main

import (
	"context"
	"log/slog"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/faultpoint"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
)

// runSigner is the signer subcommand: the signing boundary process (plan
// 7.2, owner answer Q1). It refuses to start under any uid but the
// configuration's signer uid, and never as root: the configuration refuses
// uid 0 for both processes, and a root launcher starts the signer under
// its own non-root uid (plan 4.1). There is no flag that lifts this.
func runSigner(ctx context.Context, args []string, env processEnv, log *slog.Logger) error {
	fs := newFlags("signer", env)
	cfgPath := configFlag(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	c, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if err := c.RequireSigner(); err != nil {
		return refusedError(err)
	}
	if err := c.CheckProcessUID(true, env.euid); err != nil {
		return refusedError(err)
	}
	sc, err := c.SignerConfig()
	if err != nil {
		return refusedError(err)
	}
	sc.Logger = log
	if faultpoint.Enabled {
		sc.BeforeReply = func() { faultpoint.Hit(faultpoint.AfterDecodeBeforeReply) }
	}
	srv, err := signer.New(ctx, sc)
	if err != nil {
		return startError(err)
	}
	if err := srv.Serve(ctx); err != nil {
		return err
	}
	log.Info("stopped")
	return nil
}
