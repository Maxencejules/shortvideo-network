# Committed-state indexer

This note is the goal 12 handoff for hosting services. It is PROPOSED where it names a timing number. It is not an audit and not a light client. Public launch is disabled. There is no token, coin, sale, or mining.

## What the indexer is

`services/indexer` builds a PostgreSQL read model from committed blocks. The read floor is the node's status height minus one (`AppFloor`). CometBFT can save a block before the application has committed it, so the newest saved height is not indexed. A broadcast acknowledgement is not read, and a failed transaction's events are not applied.

The database is not a substitute ledger. Hosting services must treat a stale or divergent read as not confirmed. The indexer never writes a publication-approval column, and a lease term changes only from a committed `LeaseRenewed` event.

## Trust

Every status and read response says:

- `trust.model` is `configured_read_gateway`
- `trust.light_client` is false
- the gateway URL is the one configured node

The process checks that a response agrees with itself: the chain id matches the configuration, the height matches the request, and the block hash matches the commit hash. A chain-id mismatch, a hash mismatch, a malformed event, or an event whose version is not `svn.events.v1` stops the indexer with a diagnostic. That is not a proof against a gateway that returns a consistent lie. Comparing two unauthenticated responses would not be one either.

Decoder version is 1. The checkpoint and the projection rows of one block commit in one database transaction. The deduplication key is the chain id, the height, the transaction hash (32 zero bytes for a block event), the message index (`-1` for a block event), and the event index. Each applied change keeps that source reference.

## Read API

`svn-indexer serve` listens on a loopback address and serves JSON:

| Method and path | Body |
| --- | --- |
| `GET /v1/status` | Indexed height, canonical height, app floor, commit time, commit age, last reconciliation, stale flag and reasons, trust |
| `GET /v1/providers` | Provider id, state, key generation, and the status object |
| `GET /v1/content` | Content id, version, manifest hash, package byte count, withdrawn, history-erased, and the status object |
| `GET /v1/leases` | Lease id, parties, state, term version, deadline, active flag, and the status object |
| `GET /v1/deadlines` | Active-lease deadlines, and open offers with `expiry_known: false` when the event did not carry an expiry |
| `GET /v1/policy` | Policy version and parameter key, and the status object |

`svn-indexer status` prints the same status object from the database. Commit age is the database clock minus the chain header time. It is a disclosure. It does not renew a lease.

Stale reasons include `behind`, `commit_age`, `catching_up`, `history_unavailable`, `observer_stale`, `divergent`, and `reconciliation_blocked`. A stopped chain becomes stale because the commit age grows. The projection is left as it was.

## Reconciliation

`reconcile-once` and the serve loop compare the projection to chain queries at the floor: provider pages, content by known id, active leases by known provider, deadlines, and the policy version. A difference is stored as a finding and the run is `divergent`. The projection row is not rewritten. A query error is `blocked`. It is not an empty chain and it does not clear the projection.

Content has no list query. Reconciliation of content uses the ids the events produced. A gap in the event history is not treated as "no content".

## Rebuild and private data

`rebuild` connects as `svn_indexer_owner`, checks that every table in the `indexer` schema is one of the projection tables, and runs `DROP SCHEMA IF EXISTS indexer CASCADE`. It then migrates and binds the chain id again. `app_private` and `broadcaster` are not named by that statement. A table such as `indexer.social_records` makes rebuild refuse and delete nothing. The runtime role has no DELETE.

Private social records do not belong in this schema. They are not rebuildable coordination data.

## Archive and pruning

Backfill asks for each height from the checkpoint through the floor, in order. If the node does not return that height, the indexer stops with `history unavailable` and does not apply a later height in its place. Operators who want a rebuild from genesis need a node that still has those block results (`discard_abci_responses` off, and no pruning of heights the indexer has not checkpointed). A full-history test against a node configured to prune was not run. The missing-height behaviour was tested by a proxy in front of a devnet node.

## PROPOSED timings

| Name | Default | Bounds |
| --- | --- | --- |
| `stale_after_seconds` | 8 | 3 to 600 |
| `poll_ms` | 500 | 100 to 10000 |
| `reconcile_ms` | 5000 | 1000 to 3600000 |
| `rpc_timeout_seconds` | 10 | 1 to 60 |

These are not a service level.

## Commands

Development only, on a `shortvideo-dev-` chain id, over a Unix socket to PostgreSQL:

`dev-db`, then `migrate`, then `serve`. `backfill` is one catch-up pass. `canonical` prints the normalized projection for a rebuild comparison. Exit 3 is a refused input (configuration, chain id, forged response, event version, malformed event). Exit 4 is history unavailable or a blocked reconciliation. Exit 5 is a divergent reconciliation.
