# services/broadcaster

Status: goal 11 work in progress. The design is PROPOSED in
[docs/architecture/broadcaster.md](../../docs/architecture/broadcaster.md): it is not approved, it is not an audit, and it
applies to the single-machine devnet only. This README summarizes that design as the code in this directory implements
it. It does not say which parts are tested or verified; `docs/evidence/goal-11.md` will record that, with logs. The owner answered Q1 to Q4 on 2026-11-08 (design section
17), each with option (a): Q1 "Both keys, separate signer (Recommended)", Q2 "Relay + verbatim forward (Recommended)", Q3
"Operator-registered intent keys (Recommended)", Q4 "Minimal + clear unsent (Recommended)". On 2026-11-08 at 21:39 America/Toronto the owner approved that design file only, at sha256
`9779fdb1e7fe5a7577565c64a8066b6bff7e7bace1174b085df81e7b17ae905b` (DR-11-01). The file still reads PROPOSED and is not
edited. The approval does not approve any PROPOSED number, and the Q1 to Q4 answers do not approve a pull request or a
merge. The orchestrator's reversible choices after the Wave 0 review (rulings R1 to R6) are in design
section 19.1; they are not owner decisions.

A broadcast acknowledgement is not committed execution. The broadcaster reports COMMITTED only from a committed result it
read from the chain.

## What is in this directory

- `go.mod`: the Go module `github.com/Maxencejules/shortvideo-network/services/broadcaster`, listed in the root `go.work`.
  Every third-party requirement is pinned to the version `chain/go.mod` requires; the chain and protocol modules are
  replaced by the local `../../chain` and `../../proto`.
- `doc.go`: the module's root package; it declares no identifiers.
- `cmd/svn-broadcaster/`: the one binary, with the subcommands below.
- `internal/`: the service packages: `api` (mTLS API) and `api/request` (typed requests and digests), `config`, `store`
  (PostgreSQL outbox and its migrations), `outbox` (state machine), `chain` (node client), `policy` (allowlists,
  permissions, provider and forward checks, intent-key registry), `signer` (signer process and its client), `keys` (key
  file loader, imported only by `signer`), `worker` (submission engine), `logx` (redacting logs), `faultpoint`
  (build-tagged fault points), `pgdev` and `pgtest` (disposable development and test clusters).
- The goal 03 Python scaffold `svn_broadcaster/` is removed (DR-11-20). Its `serve()` raised "services/broadcaster is
  scaffolding and does not submit transactions", which is not true of the Go service here. Replaced with it:
  - the scaffold test `test_broadcaster_refuses_to_serve` of `tests/unit/test_scaffold.py`, by
    `test_scaffold_is_replaced_by_the_go_module` and the CI checks of `tests/unit/test_ci_broadcaster.py`, and by the
    module's own Go tests, which `scripts/run_tests.py` and the `go-race (broadcaster)` CI shard run;
  - the path pin `services/broadcaster/svn_broadcaster/scaffold.py` of `tests/unit/test_repo_policy.py`, by
    `services/broadcaster/go.mod` and `services/broadcaster/cmd/svn-broadcaster/main.go`;
  - the `services/broadcaster` entries of `pythonpath` and `mypy_path` in `pyproject.toml`, removed.

## What the service is designed to do

- Take typed requests from other services over mTLS on loopback, or through a granted SQL function inside the caller's own
  database transaction.
- Store each intent and its outbox rows in PostgreSQL in one database transaction.
- Sign one message per transaction with an allowlisted development key, in a separate signer process, and broadcast it to
  one configured node.
- Reconcile by transaction hash, account sequence and object state before any resend.
- Report requested, submitted, committed, failed or pending status from committed chain results only.

It holds no council seat, `AUTHORITY`, `AUDITOR`, provider, validator, creator, user or hosting-intent key. It never signs a
provider's consent, a hosting intent, a council message, `authority_end` or arbitrary bytes. Under the Q1 answer
(option (a)) it holds one `ADMITTED_SERVICE` key (register and withdraw content, offer leases) and one `BROADCASTER` key
(relay provider-signed accept, renew and end). It also forwards a provider's own signed transaction byte for byte, with
no key of its own (Q2). Design section 5 lists the checks the signer runs.

## Signing boundary and the same-uid limit

When the launcher runs as root, `svn-broadcaster signer` and `svn-broadcaster serve` run under two distinct non-root uids,
and only the signer can read the key files. When the launcher is not root, both run as one uid: the split then separates
key bytes in memory only, and a compromised `serve` can read the key files. Only a logged root run with distinct uids can
verify the boundary. The operator's intent-key registry (public keys only) must be writable by neither process; under
distinct uids the signer also refuses a registry the serve uid owns or that has a group or other write bit. Design
section 4.2 states the limits, section 5.3 lists what a compromised `serve` can still do with distinct uids, and section
7 has the registry rules.

## Commands

One binary, `svn-broadcaster`; design section 4.3 has every flag and the JSON that `dev-db` and `dev-ca` print.

| Subcommand | Role |
| --- | --- |
| `serve --config FILE` | caller API and submission engine; never reads a key file |
| `signer --config FILE` | the only process that reads key files; never root |
| `migrate --config FILE [--callers]` | migrations as the owner role, chain binding, caller registration |
| `reconcile-once --config FILE` | one pass of every worker loop; exit 4 while another process holds a lock |
| `status --config FILE` | read-only JSON summary |
| `signer-clear --config FILE --signer ADDRESS --sequence N --foreign-hash HEX` | acknowledges a foreign use of a signer's sequence and lifts the `SIGNER_CONFLICT` halt |
| `dev-ca --out DIR --principal ID...` | development CA, loopback server certificate, one client certificate per principal |
| `dev-db --chain-id ID --secrets-dir DIR` | disposable PostgreSQL cluster under the system temp directory, removed at SIGINT or SIGTERM |

Exit codes: 0 success or a clean stop; 1 run-time failure; 2 usage error; 3 refused configuration or environment; 4 lock
held; 5 not found. Start order: `dev-db`, `migrate`, `signer`, `serve`. Only a `SIGNER_CONFLICT` halt can be lifted; a
signer halted for `INTEGRITY`, `UNAUTHORIZED`, `SEQUENCE_GAP` or `ACCOUNT_NUMBER_CHANGED` stays halted for the life of
the database (on the devnet: recreate it with `dev-db` and `migrate`).

## Service API (PROPOSED)

HTTP/1.1 JSON over mTLS on loopback. The caller's principal is the client certificate's single URI SAN
`svn://principal/<id>`.

| Method and path | Effect |
| --- | --- |
| `POST /v1/intents` | validate, then insert the intent and its outbox rows in one database transaction; 201 new, 200 for the same idempotency key and request digest (also for concurrent creates), 409 `IDEMPOTENCY_CONFLICT` for the same key with another digest |
| `GET /v1/intents/{intent_id}` | the status view; another caller's intent answers 404 |
| `GET /v1/intents?status=&page_token=&limit=` | the caller's own intents, at most 100 per page (PROPOSED) |
| `POST /v1/intents/{intent_id}/cancel` | only while REQUESTED with no attempt ever persisted; otherwise 409 `NOT_CANCELLABLE` |
| `GET /v1/signers` | public signer addresses and keys per class |
| `GET /v1/health` | chain id, latest height seen, observer freshness, stall flag, database reachability |

- A create carries the kind, a caller-chosen idempotency key, an expiry, an authorization context and the request. The
  request holds the fields of the kind's chain message; the broadcaster fills `signer` and `business_id`.
- Kinds: `REGISTER_CONTENT`, `WITHDRAW_CONTENT`, `OFFER_LEASE`, `RELAY_ACCEPT_LEASE`, `RELAY_RENEW_LEASE`,
  `RELAY_END_LEASE` and `FORWARD_PROVIDER_TX` (Q2). No kind exists for provider registry messages, council messages,
  `authority_end` or audit batches. Renew, end and forward requests name the lease's `provider_id` (and `content_id` for
  renew and end), because no chain query returns a lease by id.
- The authorization context allows only `principal_kind`, `principal_id`, `permissions` and `policy_ref`. Emails, IP
  addresses, tokens, headers and free text are refused. Opaque ids and digests are still linkable.
- The SQL path is `broadcaster.enqueue_intent(kind, request_json, auth_context, idempotency_key, expires_in)`, granted to
  one PostgreSQL role per caller service.
- Another caller's intent or content answers 404 `NOT_FOUND`; a content id that is pending, owned by another caller or
  taken on chain answers one uniform 409 `CONTENT_ID_UNAVAILABLE`. COMMITTED is shown only with valid commitment evidence;
  otherwise the answer is 500 `INTEGRITY`. Answers carry digests, never signatures, hosting intents or the authorization
  context.
- Design section 10 has every error code, the request digests and the object permissions.

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

Schema `broadcaster`. Migration `0001_outbox.pgsql` has the tables `schema_migrations`, `chain_binding`, `caller_limits`,
`callers`, `signers`, `signer_events`, `intents`, `business_attempts`, `attempts`, `commitments`, `transitions`,
`stall_episodes` and `chain_health`. Migration `0002_answers.pgsql` adds what the Q1 to Q4 answers decide:

- `kinds`, the seven request kinds and the signer class each needs; signer classes `SERVICE`, `BROADCASTER` and
  `FORWARD_SOURCE` (a provider account of a verbatim forward, with no key) and key names (Q1, Q2);
- `forwarded_txs`, the decoded fields of a verbatim forward, whose bytes are the intent's request (Q2);
- `content_owners`, one live (non-`VOID`) row per content id, `PENDING` until the chain shows our registration (Q3);
- the Q4 clearing: when an intent ends FAILED and none of its attempts ever started a send, its request, SQL-path request
  JSON, authorization context and attempt bytes are cleared, with `request_digest`, `sql_digest`, `tx_hash` and
  `auth_context_digest` kept. A FAILED never-sent intent that is not cleared is refused at commit.

Every text, bytea and jsonb column has a length check. COMMITTED and FAILED rows cannot be updated, except for that
clearing, and `transitions` and `commitments` are append-only. At commit the database checks that a COMMITTED intent's
evidence agrees with its own attempts. Refusals carry SQLSTATEs SV001 to SV014. There is no lease, offer or content state
table, so the broadcaster cannot create a final lease (`TestNoLeaseOrProjectionTable`). Migrations are embedded `.pgsql` files, because `.gitignore`
ignores `*.sql`. Retention is unset (DEC-004). Design section 11 has the columns and constraints.

## Configuration and operation notes

- Each process has its own strict JSON configuration file (unknown fields refused): the `signer` file holds the keys and
  no database, listener or callers; the `serve` file holds no keys. Callers have `principal`, `kinds`,
  `open_intent_cap`, `bound_provider_id` and `sql_role`. Design section 16 has every key.
- The chain id and the node RPC URL are required and have no defaults. The node is trusted for reads.
- The database connection is a Unix socket with SSL off, with the password read from a `password_file` owned by the
  process's uid with no group or other bit.
- Every PROPOSED number has hard bounds in code and is listed in design section 16. `indexing_margin_blocks` is at least
  2: at a reported height S, block S−1 may not be in the transaction index yet.
- Chain reads that gate a signature or an authorization use a read floor of the `/status` height minus one (at least 1).
  CometBFT can report a block the application is still executing, so a query answered one block below that height is
  current, and a query answered further below is stale. The relay pre-check still compares offer expiry with the time of
  that same `/status` header: block times increase, so the header time is a lower bound of any later block that could
  include the relay. The signer start-up governance read uses the floor and, if the answer is still below it, retries for
  at most four poll intervals, then exits with an error that names the floor and the bound. Design section 3.1 states the
  guarantee.
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
