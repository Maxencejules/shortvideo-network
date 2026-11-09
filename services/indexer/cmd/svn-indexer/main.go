// Command svn-indexer is the goal 12 committed-state indexer.
//
// It reads committed blocks from one configured node and keeps a PostgreSQL
// projection. The projection is a read model. The node is trusted for the
// bytes it returns; this process checks that those bytes agree with each
// other and says so on every read. That is not light-client verification.
//
// Subcommands: migrate, dev-db, serve, backfill, reconcile-once, status,
// rebuild, canonical.
//
// Exit codes: 0 success, 1 runtime failure, 2 usage, 3 refused input
// (configuration, chain id, forged response, event version, malformed event),
// 4 history unavailable, 5 reconciliation found a difference.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Environ(), os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
