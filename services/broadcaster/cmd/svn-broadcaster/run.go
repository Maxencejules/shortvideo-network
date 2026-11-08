package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/logx"
)

// Exit codes (see the package comment).
const (
	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	exitRefused  = 3
	exitLockHeld = 4
	exitNotFound = 5
)

// exitError is an error with the exit code it maps to.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// usageError, refusedError, lockHeldError and notFoundError tag err with
// its exit code; any other error exits with exitFailure.
func usageError(err error) error    { return &exitError{code: exitUsage, err: err} }
func refusedError(err error) error  { return &exitError{code: exitRefused, err: err} }
func lockHeldError(err error) error { return &exitError{code: exitLockHeld, err: err} }
func notFoundError(err error) error { return &exitError{code: exitNotFound, err: err} }

// exitCode returns the exit code of err: exitOK for nil, the tagged code,
// or exitFailure.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	return exitFailure
}

// command is one subcommand: it parses its own arguments and runs.
type command func(ctx context.Context, args []string, env processEnv, log *slog.Logger) error

// commands maps each subcommand name to its implementation.
var commands = map[string]command{
	"serve":          runServe,
	"signer":         runSigner,
	"migrate":        runMigrate,
	"reconcile-once": runReconcileOnce,
	"status":         runStatus,
	"signer-clear":   runSignerClear,
	"dev-ca":         runDevCA,
	"dev-db":         runDevDB,
}

// commandNames returns the subcommand names in sorted order.
func commandNames() []string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// run dispatches args to a subcommand and returns the process exit code.
// The environment check runs before the subcommand parses anything.
func run(ctx context.Context, args []string, env processEnv) int {
	log := logx.New(env.stderr, slog.LevelInfo)
	if len(args) == 0 {
		usage(env.stderr)
		return exitUsage
	}
	name := args[0]
	if name == "help" || name == "-h" || name == "--help" {
		usage(env.stdout)
		return exitOK
	}
	cmd, ok := commands[name]
	if !ok {
		log.Error("unknown subcommand", "command", clipName(name))
		usage(env.stderr)
		return exitUsage
	}
	err := config.CheckEnvironment(env.environ)
	if err != nil {
		err = refusedError(err)
	} else {
		err = cmd(ctx, args[1:], env, log.With("command", name))
	}
	code := exitCode(err)
	if err != nil {
		log.Error("svn-broadcaster stopped with an error", "command", name, "exit_code", code, "error", err)
	}
	return code
}

// clipName bounds an unknown subcommand name before it is logged.
func clipName(s string) string {
	if len(s) > 32 {
		return s[:32]
	}
	return s
}

// usage writes the subcommand list.
func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: svn-broadcaster <subcommand> [flags]")
	fmt.Fprintln(w, "subcommands: "+strings.Join(commandNames(), ", "))
	fmt.Fprintln(w, "run svn-broadcaster <subcommand> -h for the flags of one subcommand")
}

// newFlags returns a flag set for a subcommand whose errors and help go to
// env.stderr.
func newFlags(name string, env processEnv) *flag.FlagSet {
	fs := flag.NewFlagSet("svn-broadcaster "+name, flag.ContinueOnError)
	fs.SetOutput(env.stderr)
	return fs
}

// parseFlags parses args and refuses positional arguments. Help (-h) is a
// usage error too: the subcommand did not run.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 0 {
		return usageError(fmt.Errorf("unexpected argument %q", clipName(fs.Arg(0))))
	}
	return nil
}

// configFlag registers the --config flag.
func configFlag(fs *flag.FlagSet) *string {
	return fs.String("config", "", "absolute path of the JSON configuration file (required)")
}

// loadConfig reads the configuration at path; a missing flag is a usage
// error and an invalid file a refusal.
func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		return nil, usageError(errors.New("--config is required"))
	}
	if !filepath.IsAbs(path) {
		return nil, usageError(errors.New("--config must be an absolute path"))
	}
	c, err := config.Load(path)
	if err != nil {
		return nil, refusedError(err)
	}
	return c, nil
}
