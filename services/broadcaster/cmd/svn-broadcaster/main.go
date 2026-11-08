// Command svn-broadcaster is the goal 11 transaction broadcaster: one
// binary with one subcommand per process role (plan section 4.1). It runs
// on a devnet chain only (development keys and principals refuse a chain
// id without the shortvideo-dev- prefix).
//
// Subcommands:
//
//	serve           --config FILE
//	    The caller API (mTLS on loopback) and the submission engine: the
//	    per-signer workers, the forwarder, housekeeping and the chain
//	    observer. It reaches the signer process through its Unix socket and
//	    never reads a key file. It runs under the configuration's serve uid
//	    and refuses to start unless the database schema is the binary's and
//	    the node and the database serve the configured chain id.
//	signer          --config FILE
//	    The signing boundary (owner answer Q1): the only process that reads
//	    key files. It runs under the configuration's signer uid, never as
//	    root, and serves only the serve uid on its socket.
//	migrate         --config FILE [--callers]
//	    Applies the embedded migrations as the owner role, binds the
//	    database to the configured chain id, and with --callers registers
//	    every configured caller (its open-intent cap and SQL-path role).
//	reconcile-once  --config FILE
//	    One pass of every worker loop with the serve configuration, under the
//	    serve uid and with the signer process running (a pass may sign a new
//	    attempt). It takes each signer's advisory lock with
//	    pg_try_advisory_lock and fails while serve (or another pass) holds
//	    one.
//	status          --config FILE
//	    Prints the schema, chain binding, signers, observer record and open
//	    stall episode as one JSON object. It calls only read functions of the
//	    outbox store and never reaches the node or the signer.
//	signer-clear    --config FILE --signer ADDRESS --sequence N --foreign-hash HEX
//	    Records the operator's acknowledgement of one foreign use of a
//	    signer's sequence (DR-11-15) and lifts the SIGNER_CONFLICT halt when
//	    no uncleared event remains. It connects as the runtime role (the
//	    serve configuration) and takes the signer's advisory lock, so it
//	    fails while serve holds it.
//	dev-ca          --out DIR --principal ID [--principal ID]... [--days N] [--serve-uid UID]
//	    Writes a development CA certificate, a loopback server certificate
//	    and one client certificate per caller principal into a new 0700
//	    directory. The CA key is never written.
//	dev-db          --chain-id ID --secrets-dir DIR [--serve-uid UID] [--login-role NAME]...
//	                [--bin-dir DIR] [--start-timeout DURATION]
//	    Runs a disposable PostgreSQL cluster in os.TempDir()/svn-pg-* for a
//	    shortvideo-dev- chain id, creates the outbox roles and database,
//	    writes each role's password to its own file, prints one JSON line
//	    describing the cluster, and removes everything at SIGINT or SIGTERM.
//
// Every subcommand refuses to start, before it reads any other input, when
// a PG* variable or PQGO_DEBUG is set (plan 7.7). Logs go to standard error
// as redacting JSON (internal/logx); results go to standard output.
//
// Exit codes:
//
//	0  success, or a clean stop on SIGINT or SIGTERM
//	1  failure at run time: database, node, signer, file system
//	2  usage error: unknown subcommand, bad flag, missing or extra argument
//	3  refused configuration or environment: an invalid configuration file,
//	   a PG* or PQGO_DEBUG variable, a uid the configuration does not name
//	   for the process, root where it is refused, a node or database that
//	   serves another chain id, a schema that is not the binary's
//	4  lock held: another process holds a signer's or the forwarder's
//	   advisory lock (reconcile-once, signer-clear)
//	5  not found: signer-clear names no open signer event
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], processEnv{
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		environ: os.Environ(),
		euid:    uint32(os.Geteuid()),
	})
	stop()
	os.Exit(code)
}

// processEnv is what a subcommand takes from its process: the output
// streams, the environment and the effective uid.
type processEnv struct {
	stdout  io.Writer
	stderr  io.Writer
	environ []string
	euid    uint32
}
