# services/broadcaster

Status: goal 11 work in progress. The design is PROPOSED in
[docs/architecture/broadcaster.md](../../docs/architecture/broadcaster.md): it is not approved, it is not an audit, and it
applies to the single-machine devnet only. This README summarizes that design. It does not say which parts are built or
tested; `docs/evidence/goal-11.md` will record that, with logs. The owner questions Q1 to Q4 (section 17 of the design)
are unanswered, and the parts that depend on them are not built.

A broadcast acknowledgement is not committed execution. The broadcaster reports COMMITTED only from a committed result it
read from the chain.

## What is in this directory

- `go.mod`: the Go module `github.com/Maxencejules/shortvideo-network/services/broadcaster`, listed in the root `go.work`.
  Every third-party requirement is pinned to the version `chain/go.mod` requires; the chain and protocol modules are
  replaced by the local `../../chain` and `../../proto`.
- `doc.go`: the module's root package; it declares no identifiers.
- `internal/`: the service packages, written during goal 11.
- `svn_broadcaster/`: the goal 03 Python scaffold. Its `serve()` raises "services/broadcaster is scaffolding and does not
  submit transactions" (`svn_broadcaster/scaffold.py`). The goal 11 plan replaces it with the Go service (DR-11-20).

## What the service is designed to do

- Take typed requests from other services over mTLS on loopback, or through a granted SQL function inside the caller's own
  database transaction.
- Store each intent and its outbox rows in PostgreSQL in one database transaction.
- Sign one message per transaction with an allowlisted development key, in a separate signer process, and broadcast it to
  one configured node.
- Reconcile by transaction hash, account sequence and object state before any resend.
- Report requested, submitted, committed, failed or pending status from committed chain results only.

It holds no council seat, `AUTHORITY`, `AUDITOR`, provider, validator, creator, user or hosting-intent key. It never signs a
provider's consent, a hosting intent, a council message, `authority_end` or arbitrary bytes. Under the recommended Q1
answer it holds one `ADMITTED_SERVICE` key (register and withdraw content, offer leases) and one `BROADCASTER` key (relay
provider-signed accept, renew and end). Design section 5 lists the checks the signer runs.

## Signing boundary and the same-uid limit

When the launcher runs as root, `svn-broadcaster signer` and `svn-broadcaster serve` run under two distinct non-root uids,
and only the signer can read the key files. When the launcher is not root, both run as one uid: the split then separates
key bytes in memory only, and a compromised `serve` can read the key files. Only a logged root run with distinct uids can
verify the boundary. Design section 4.2 states the limits, and section 5.3 lists what a compromised `serve` can still do
with distinct uids.

## Service API (PROPOSED)

HTTP/1.1 JSON over mTLS on loopback. The caller's principal is the client certificate's single URI SAN
`svn://principal/<id>`.

| Method and path | Effect |
| --- | --- |
| `POST /v1/intents` | validate, then insert the intent and its outbox rows in one database transaction; 201 new, 200 for the same idempotency key and request digest, 409 for the same key with another digest |
| `GET /v1/intents/{intent_id}` | the status view; another caller's intent answers 404 |
| `GET /v1/intents?status=&page_token=&limit=` | the caller's own intents, at most 100 per page (PROPOSED) |
| `POST /v1/intents/{intent_id}/cancel` | only while REQUESTED with no attempt ever persisted |
| `GET /v1/signers` | public signer addresses and keys per class |
| `GET /v1/health` | chain id, latest height seen, observer freshness, stall flag, database reachability |

- A create carries the kind, a caller-chosen idempotency key, an expiry, an authorization context and the request. The
  request holds the fields of the kind's chain message; the broadcaster fills `signer` and `business_id`.
- Kinds: `REGISTER_CONTENT`, `WITHDRAW_CONTENT`, `OFFER_LEASE`, `RELAY_ACCEPT_LEASE`, `RELAY_RENEW_LEASE`,
  `RELAY_END_LEASE`, and `FORWARD_PROVIDER_TX` if Q2 allows it. No kind exists for provider registry messages, council
  messages or `authority_end`.
- The authorization context allows only `principal_kind`, `principal_id`, `permissions` and `policy_ref`. Emails, IP
  addresses, tokens, headers and free text are refused. Opaque ids and digests are still linkable.
- The SQL path is `broadcaster.enqueue_intent(kind, request_json, auth_context, idempotency_key, expires_in)`, granted to
  one PostgreSQL role per caller service.
- Design section 10 has the error codes, the request digests and the object permissions.

## States callers see

| Status | When |
| --- | --- |
| `OPERATION_STATUS_REQUESTED` | stored; nothing signed yet, or waiting to retry after an attempt that can no longer commit |
| `OPERATION_STATUS_SUBMITTED` | an attempt is persisted and may commit; `detail.phase` is `SIGNED`, `ACKNOWLEDGED`, `UNCERTAIN` or `AWAITING_DEADLINE` |
| `OPERATION_STATUS_COMMITTED` | a committed result was read from the chain; the commitment evidence is attached |
| `OPERATION_STATUS_FAILED` | a committed failure after the effect check, a definitive refusal, chain proof that no attempt can commit, or a refusal before any send |
| `OPERATION_STATUS_PENDING_STALL` | a stall episode is open and the observer is fresh; the underlying phase is kept in `detail` |

"Uncertain" is `SUBMITTED` with `detail.phase = UNCERTAIN`; the frozen enum has no uncertain value. A stale observer is
shown as `detail.observer = OBSERVER_UNAVAILABLE`, never as a stall. Design section 13 has the full transition table.

## Outbox schema (PROPOSED)

Schema `broadcaster`. Migration 0001 has the tables `schema_migrations`, `chain_binding`, `caller_limits`, `callers`,
`signers`, `signer_events`, `intents`, `business_attempts`, `attempts`, `commitments`, `transitions`, `stall_episodes` and
`chain_health`. The answer-dependent parts come in a later migration: signer key classes and names (Q1) and
`content_owners` (Q3). Every text, bytea and jsonb column has a length check. COMMITTED and FAILED rows cannot be updated,
and `transitions` and `commitments` are append-only. At commit the database checks that a COMMITTED intent's evidence
agrees with its own attempts. There is no lease, offer or content state table, so the broadcaster cannot create a final lease. Migrations are
embedded `.pgsql` files, because `.gitignore` ignores `*.sql`. Retention is unset (DEC-004). Design section 11 has the
columns and constraints.

## Configuration and operation notes

- The chain id and the node RPC URL are required and have no defaults. The node is trusted for reads.
- Every PROPOSED number has hard bounds in code and is listed in design section 16.
- The database connection is built field by field. Every subcommand refuses to start if any `PG*` variable or `PQGO_DEBUG`
  is set, because `lib/pq` reads them and `PQGO_DEBUG=1` prints the connection config to stderr.
- Run PostgreSQL with all five settings the development cluster uses: `log_min_error_statement=panic`,
  `log_statement=none` and `log_parameter_max_length_on_error=0`, so failing statements and bind parameters are not
  logged; and `log_error_verbosity=terse` and `log_min_messages=fatal`, because PostgreSQL error messages can quote input
  values themselves (an invalid input value, the failing row in a constraint violation's DETAIL, jsonb CONTEXT lines). With
  the first three alone, caller values still reach the server log.
- Local expiry of a relayed provider signature revokes nothing on chain. Only a renewal or a provider key rotation does.
- Keys, certificates and database directories are generated at run time in ignored or temporary paths. None belongs in
  Git or in a log.
