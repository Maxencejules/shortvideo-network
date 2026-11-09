# services/indexer

The committed-state indexer. `svn-indexer` reads committed blocks from one configured CometBFT node and keeps a PostgreSQL projection of providers, content, offers, leases, policy versions and evidence records.

The database is a read model. It is not a chain. A row is not a publication approval and not a lease renewal. The configured gateway is trusted for the bytes it returns. Checks that a block's hash matches its commit are not light-client verification.

Subcommands: `dev-db`, `migrate`, `serve`, `backfill`, `reconcile-once`, `status`, `rebuild`, `canonical`.

`rebuild` drops the `indexer` schema only. It refuses another role, and it refuses when that schema holds a table that is not a projection. Private social records stay in another schema.

Archive: the node must still have block results from the checkpoint through the read floor (status height minus one). A missing height stops the indexer. It is not an empty history.

See `docs/architecture/indexer.md`.
