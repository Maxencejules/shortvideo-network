# Goal 11 services-runtime map (runtime reader)

Reader output for the goal 11 planning workflow. Nothing in the repository was edited, added, committed, or pushed.
The only file written is this one. Nothing was built and no test suite was run.

- Worktree read: `scratchpad/wt-11` (written `$WT`), branch `goal-11`. Its HEAD is `6d74aca` (PR 15 merged), not
  `dbf0897` as the task text says. The orchestrator fast-forwarded it (`scratchpad/notes-08-10.md`, "2026-11-08 ~05:45Z").
  Nothing cited here changed between the two commits: `git diff --stat dbf0897 6d74aca` touches only
  `chain/app/app.go`, `chain/cmd/svnd/cli/genesis*.go` and a README.
- Module cache: `go env GOMODCACHE` = `/root/go/pkg/mod`. `SDK/` = `github.com/cosmos/cosmos-sdk@v0.55.0`,
  `CMT/` = `github.com/cometbft/cometbft@v0.40.0`, `PQ/` = `github.com/lib/pq@v1.12.3`.
- Labels: a `file:line` citation is a fact read on this tree, in the module cache, or in the installed package.
  **[Educated Guess]** marks an inference. **[Verified here]** marks something I checked by a read-only command in this
  container (the command is given). Every design item in sections 7 to 11 is a proposal, not an owner decision. No owner
  ruling about goal 11 exists. The only owner text that covers this run is "I authorize you to merge it and complete
  the next goals" (`notes-08-10.md`, "2026-11-08 ~05:45Z").

---

## 1. Findings in brief

1. The three `services/*` directories are Python scaffolds that raise on start (`services/broadcaster/svn_broadcaster/scaffold.py:6-7`).
   Two unit tests pin them as scaffolds (`tests/unit/test_scaffold.py:24-27`, `tests/unit/test_repo_policy.py:17`).
2. The blueprint names a **Go** broadcaster: "Python services request bounded operations through an operator-authorized
   Go broadcaster or generated client" (`docs/blueprint.md:373`, p. 13). The Python+FastAPI row covers "Accounts,
   policies, social actions, feed API and moderation" (`docs/blueprint.md:203`, p. 7), not the transaction sender.
   Goal 03 made `services/broadcaster` a Python package as scaffolding only (`docs/evidence/goal-03.md:63`).
3. `requirements.lock` has FastAPI 0.162.0, Starlette 1.10.0, Pydantic 2.14.2, cryptography 50.0.2 and protobuf 7.36.2.
   It has no ASGI server, no PostgreSQL driver, no migration tool, no HTTP client, no gRPC and no Cosmos client
   (`requirements.lock:7-945`, list in 3.1). `docs/dependencies.md:39` says "No Python client is pinned" for PostgreSQL.
4. The Go side already has everything except the broadcaster code. `lib/pq v1.12.3` (MIT, a `database/sql` PostgreSQL
   driver) is in `chain/go.mod:129` and `chain/go.sum:486-487`. svnd already compiles it: CometBFT's node imports it
   (`CMT/node/setup.go:40`; `go mod why -m github.com/lib/pq` prints `chain/node -> cometbft/node -> lib/pq`).
   lib/pq has no dependencies of its own (`PQ/go.mod`: only `go 1.21`).
5. A Go service can reuse the chain's own codec and signer: `app.MakeEncoding` (`chain/app/encoding.go:44-81`),
   the SDK `tx.SignWithPrivKey` path the auditor already uses (`agents/auditor/chainclient/tx.go:34-82`), the hardened
   CometBFT RPC client (`agents/auditor/chainclient/rpc.go:100-167`), typed committed events
   (`chain/msgkit/events.go:186-200`) and the provider sign bytes (`chain/x/leases/signdoc.go:88-99`).
6. Signing from Python is feasible but means hand-building SIGN_MODE_DIRECT. That includes the sign doc, the pubkey
   `Any`, low-S R||S signatures and bech32. Those are new crypto-adjacent code paths that AGENTS.md calls "unreviewed
   cryptography". `proto/gen/python` has no `cosmos.tx` types (section 4.2).
7. PostgreSQL 18.6, the goal 04 pin, is installed at `/usr/lib/postgresql/18/bin`. 16.15 is also present.
   `initdb` refuses to run as root. **[Verified here]**: `initdb -D <scratch>/pgprobe-root` printed "initdb: error:
   cannot be run as root" and exited 1, with no directory created. So `scripts/env/start.sh` cannot initialise a
   cluster as root in this container (`scripts/env/start.sh:48-55`). The scratchpad is not reachable by the
   `postgres` user, because `/tmp/claude-0` is mode 0700 root.
8. RabbitMQ is not needed for goal 11. REQ-TECH-004 puts Celery/RabbitMQ in goal 14 (`docs/requirements.md:429`).
   A second queue next to the outbox would bring back the dual-write problem the outbox removes.
9. `.gitignore:70` ignores `*.sql`. Migration files named `*.sql` would never be committed (section 8.8).
10. lib/pq defaults to `sslmode=require` (`PQ/connector.go:304-309`). A loopback or unix-socket development cluster
    without TLS needs an explicit `sslmode=disable`.
11. The frozen protocol already names the caller-visible states. `svn.offchain.v1.OperationStatus` is
    REQUESTED, SUBMITTED, COMMITTED, FAILED, PENDING_STALL (`docs/protocol/proto/svn/offchain/v1/records.proto:45-52`),
    pinned by `docs/evidence/goal-02/check_protocol.py:182-187`. The reference outbox parks a request as `PENDING_STALL`
    with its business id when quorum is lost (`docs/evidence/goal-02/cases.py:64-77`; `docs/protocol.md:260`).
    "Uncertain" has no enum value. Adding one is a protocol change.
12. CI caps every job at 20 minutes (`scripts/workflow_policy.py:21`) and allows only four pinned actions
    (`scripts/workflow_policy.py:14-19`). Devnet tests must fit a go-race shard whose filters
    `tests/unit/test_ci_devnet_coverage.py` checks. No CI job has PostgreSQL today.

**Recommendation (section 5.5):** implement the broadcaster as a Go module at `services/broadcaster`. Use
`database/sql` with lib/pq for the outbox and the chain's own codec and SDK signer. Serve a stdlib `net/http` internal
API with mTLS from a runtime-generated development CA. Add no Python dependency and no new third-party Go module. Run
the acceptance test in `chain/devnet`, which builds the broadcaster the way `TestDevnetAuditLifecycle` builds the
auditor (`chain/devnet/audit_test.go:218-238`). PostgreSQL 18.6 comes from installed binaries in a disposable cluster.
Faults are injected by a loopback reverse proxy and by process kills. The language choice, the layout change and the
test/CI edits need owner approval (section 12).

---

## 2. The services scaffolding today

| Path | Content | Pinned by |
| --- | --- | --- |
| `services/broadcaster/README.md:3` | "Scaffolding for the authorized transaction sender. A broadcast acknowledgement is not committed execution. Goal 11 implements the outbox. This package does not hold keys and it does not speak to a node." | |
| `services/broadcaster/svn_broadcaster/scaffold.py:1-7` | `SCAFFOLD = True`; `serve()` raises "services/broadcaster is scaffolding and does not submit transactions" | `tests/unit/test_scaffold.py:24-27` (`match="does not submit"`) |
| `services/broadcaster/svn_broadcaster/__init__.py:1-5` | re-exports `SCAFFOLD`, `serve` | `tests/unit/test_scaffold.py:7` (import) |
| `services/api/README.md:3-5` | "The selected family is Python and FastAPI 0.162.0 ... Goal 13 is the first service that accepts uploads, and it still uses development principals until Goal 19." | `tests/unit/test_scaffold.py:12-15` |
| `services/indexer/README.md:3` | "Goal 12 implements projections and reconciliation against a real chain. This package does not connect to PostgreSQL" | `tests/unit/test_scaffold.py:18-21` |
| `tests/unit/test_repo_policy.py:13-17` | `REQUIRED_PATHS` includes `services/broadcaster/svn_broadcaster/scaffold.py` | `test_blueprint_paths_exist` (`:43-45`) |
| `pyproject.toml:28-35,46-47` | pytest `pythonpath` and mypy `mypy_path` list `services/broadcaster` | ruff `src` has `services` (`:39`) |
| `workers/media` | same scaffold pattern; FFmpeg in goal 14 (`workers/media/README.md:3`) | |

No architecture document fixes a language for the broadcaster. `docs/architecture/README.md:1-13` indexes goal 06-10
reviewer maps only. Goal 03 recorded "Entry points raise. FastAPI is version-checked. No HTTP server." for all three
services (`docs/evidence/goal-03.md:63`) and REQ-TECH-002 as "FastAPI 0.162.0" (`:84`).

---

## 3. What fixes languages and frameworks

| Source | Text | Applies to broadcaster? |
| --- | --- | --- |
| Blueprint p. 7, `docs/blueprint.md:203-205` | "App services: Python + FastAPI: Accounts, policies, social actions, feed API and moderation." "Database: PostgreSQL ... job state and read models." "Jobs: Celery + RabbitMQ" | API yes; broadcaster not named |
| Blueprint p. 7, `:212` | "These are recommendations rather than confirmed dependencies ... Pin exact versions and container digests, generate a software bill of materials, and record a licence decision for each dependency." | all |
| Blueprint p. 13, `:373` | "Python services request bounded operations through an operator-authorized Go broadcaster or generated client." | **yes: Go broadcaster** |
| Blueprint p. 13, `:385` | "Use a transaction outbox in PostgreSQL for intended work. The broadcaster records attempt IDs and reconciles unknown submissions before resending. Use a stable business ID ..." | yes |
| Blueprint p. 10, `:305` | "The broadcaster waits for inclusion and execution success, then updates the application projection. Persist the transaction hash, message ID and result; reconcile after timeouts before retrying the business action." | yes |
| Blueprint p. 27, `:790-792` | monorepo tree: `services/api/  # FastAPI and social data`, `services/broadcaster/  # authorized transaction sender` (no language) | layout only |
| `docs/requirements.md:427` REQ-TECH-002 | "PROPOSED application-service family: Python and FastAPI." | API |
| `docs/requirements.md:428` REQ-TECH-003 | "PROPOSED database: PostgreSQL for private account data, social records, job state, and read models." | outbox store |
| `docs/requirements.md:429` REQ-TECH-004 | "PROPOSED jobs: Celery and RabbitMQ" with goal 14 | not goal 11 |
| `docs/requirements.md:431` REQ-TECH-006 | "PROPOSED provider agent: Go and HTTPS" | precedent for Go services |
| `docs/dependencies.md:38-39` | FastAPI 0.162.0 "No route is implemented"; PostgreSQL 18.6, "No Python client is pinned. Smoke tests use `psql`." | |
| `docs/dependencies.md:53` | `agents/auditor` is a separate Go module that "adds no third-party module: every line of `agents/auditor/go.sum` is also in `chain/go.sum`" | precedent for a Go service module |

Goal-11 requirement rows (`docs/requirements.md`): REQ-CHAIN-013 (`:452`, "broadcaster fixture cannot mint a provider
signature or skip the publication permission bit"), REQ-ACK-001 (`:483`), REQ-INT-001 (`:496`, "Goal 11 is the only
chain writer used by the API"), REQ-IDEM-001 (`:500`), REQ-QA-008 (`:594`, "one transition and no leaked reservation").
None fixes a language.

---

## 4. What is already locked or available

### 4.1 Python (`requirements.lock`, `pip-compile --generate-hashes --allow-unsafe`, header `:1-6`)

| Need | Locked? | Line / note |
| --- | --- | --- |
| Web framework | FastAPI 0.162.0, Starlette 1.10.0, Pydantic 2.14.2, anyio 4.15.1 | `:255`, `:904`, `:509`, `:15` |
| ASGI server (uvicorn, hypercorn) | **no** | FastAPI cannot serve without one |
| PostgreSQL driver (psycopg, asyncpg) | **no** | `docs/dependencies.md:39` |
| Migrations (alembic, SQLAlchemy) | **no** | |
| Protobuf runtime | protobuf 7.36.2 | `:495`; `google.protobuf.any_pb2` imports **[Verified here]** |
| Cryptography / secp256k1 | cryptography 50.0.2 with OpenSSL 4.0.3; ECDSA on SECP256K1 supported; `ECDSA(..., deterministic_signing=True)` available; Ed25519 supported | `:194`; **[Verified here]** with `.venv/bin/python -I -c` (backend checks) |
| RIPEMD-160 (Cosmos secp256k1 address) | not in `cryptography.hazmat.primitives.hashes`; `hashlib.new("ripemd160")` works here, but it depends on the OpenSSL build | **[Verified here]**; on CI's setup-python build it is unverified **[Educated Guess]** |
| bech32 | **no** | |
| HTTP client (httpx), gRPC (grpcio) | **no** | stdlib `http.client`, `urllib`, `ssl` exist |
| opentelemetry-api 1.45.2 | locked only as a FastAPI dependency | `:472-475` |
| Celery | not installed, not in the lock (sdist hash recorded only) | `docs/dependencies.md:42` |

Adding a Python package means editing `pyproject.toml` and regenerating the lock with the exact header command. It also
means updating `docs/dependencies.md` and the SBOM (`scripts/write_sbom.py --check` runs in CI, `ci.yml:43`).

### 4.2 Generated Python protobuf

`proto/gen/python/svn/**` holds only the `svn.*` packages (types, offchain, providers, content, leases, evidence,
authority, policy, events, app) (`find proto/gen/python`). It has no `cosmos.tx.v1beta1` (TxRaw, TxBody, AuthInfo,
SignerInfo, ModeInfo, Fee, SignDoc) and no `cosmos.crypto.secp256k1.PubKey`. Generating them would mean compiling
`SDK/proto/cosmos/tx/v1beta1/tx.proto`, which imports `amino/amino.proto`, `gogoproto/gogo.proto`,
`cosmos/crypto/multisig/...`, `cosmos/base/v1beta1/coin.proto`, `cosmos/tx/signing/v1beta1/signing.proto`,
`google/protobuf/any.proto`, `cosmos_proto/cosmos.proto` and `google/protobuf/timestamp.proto` (`SDK/proto/cosmos/tx/v1beta1/tx.proto:4-11`).
It would also mean extending `scripts/gen_proto.sh`, whose output CI compares (`ci.yml:93-99`).

### 4.3 Go workspace and modules

- `go.work:5-11` uses `./agents/auditor`, `./agents/provider`, `./chain`, `./docs/evidence/goal-02/go`, `./proto`.
  Go 1.26.10 toolchain, language 1.26.5 (`go.work:1-3`; `chain/go.mod:3-5`).
- `scripts/run_tests.py:151` `GO_MODULES = ("chain", "proto", "agents/provider", "agents/auditor")`. An empty suite fails
  (`:161-185`).
- Chain client building blocks (all already in `chain/go.mod` requires):
  - CometBFT RPC HTTP client `CMT/rpc/client/http` (JSON-RPC over POST: `CMT/rpc/jsonrpc/client/http_json_client.go:221,280`;
    `BroadcastTxSync` calls route `broadcast_tx_sync`: `CMT/rpc/client/http/http.go:270-283`).
  - `coretypes.ResultTx{Hash, Height, Index, TxResult, Tx, Proof}` (`CMT/rpc/core/types/responses.go:198-205`).
  - SDK `client/tx`, `x/auth/signing`, `crypto/keys/secp256k1`, `codec/address` (used by `agents/auditor/chainclient/tx.go:9-13`).
- PostgreSQL in Go:
  - `github.com/lib/pq v1.12.3`, MIT (`PQ/LICENSE`), indirect in `chain/go.mod:129` and hashed in `chain/go.sum:486-487`.
    It is compiled into svnd through `CMT/node/setup.go:40` (`_ "github.com/lib/pq" // provide the psql db driver`).
    Its source is in the module cache and download cache (`/root/go/pkg/mod/cache/download/github.com/lib/pq/@v/v1.12.3.zip`).
    It supports unix sockets (`PQ/connector.go:624-662`), `PGPASSFILE` (`:291`) and `LISTEN/NOTIFY` (`PQ/notify.go:485,527`).
    The `sslmode` default is `require` (`:304-309`).
  - `github.com/jackc/pgx/v5 v5.7.5` appears only as a go.mod hash in `go.work.sum:501-502`. Only `.info` and `.mod`
    are in the download cache, not the source, so it is not available offline.
  - `github.com/adlio/schema v1.4.0` (MIT migration library) is in `chain/go.sum:49-50` and in the module cache, but only
    as a test dependency of CometBFT's psql sink (`CMT/go.mod:8`, `CMT/state/indexer/sink/psql/psql_test.go`). Its own
    `go.mod` requires mysql, mssql, sqlite3 and dockertest. Importing it would pull those go.mod hashes into the new
    module's `go.sum`. **[Educated Guess]**: not worth it.
- Go stdlib covers the rest: `net/http`, `crypto/tls` (`tls.RequireAndVerifyClientCert`), `crypto/x509`, `log/slog`
  (`HandlerOptions.ReplaceAttr` for redaction), `database/sql` (`DB.Conn` to pin a connection for session advisory locks),
  `syscall.GetsockoptUcred` (SO_PEERCRED), `syscall.SysProcAttr.Credential` (run initdb as `postgres`). Checked with
  `go doc`.

---

## 5. Signing from Python vs a Go service

### 5.1 What any signer must reproduce exactly

| Rule | Source |
| --- | --- |
| Only `SIGN_MODE_DIRECT` is enabled | `chain/app/encoding.go:68-71` |
| Strict decoder: no unknown field anywhere in the body, including non-critical ones | `chain/app/encoding.go:110-138` |
| TxRaw must follow ADR-027 field order | `SDK/x/auth/tx/decoder.go:18-24,97-104` |
| Signer comes from each message's 20-byte `signer` bytes field (custom GetSigners) | `chain/app/encoding.go:44-53,144-154` |
| Every protocol message type registered, including the v2 payload kinds | `chain/app/encoding.go:83-108,156-173` |
| Empty memo, no fee payer, granter or tip; empty fee amount; gas in [MinGasPerTx, MaxGasPerTx]; tx bytes <= MaxTxBytes | `chain/app/ante.go:59-86,90-108,110-121` |
| `timeout_height` / `timeout_timestamp` checked in the ante | `chain/app/ante.go:32`; `SDK/x/auth/ante/basic.go:205-227` |
| secp256k1 signature = 64-byte R‖S over SHA-256(sign bytes), low-S required; RFC 6979 via `ecdsa.SignCompact` | `SDK/crypto/keys/secp256k1/secp256k1_nocgo.go:13-21,23-53` |
| secp256k1 address = RIPEMD-160(SHA-256(compressed pubkey)) | `SDK/crypto/keys/secp256k1/secp256k1.go:157-163` |
| Service and broadcaster devnet keys are secp256k1; auditors and seats are ed25519; keys are hex in `accounts/<name>.hex` | `chain/devnet/init.go:119-144,213-239` |
| Provider inner signatures: ed25519 over `domain ‖ 0x00 ‖ SVN-JCS-1 canonical` | `chain/msgkit/innersig.go:38-47,115-146`; `chain/x/leases/signdoc.go:88-120` |

### 5.2 Python path: the work and the risks

The work would be:
1. Generate the cosmos tx and crypto protos into `proto/gen/python`, with their gogoproto, cosmos_proto and amino
   imports (4.2).
2. Hand-assemble TxBody, AuthInfo (SignerInfo with an `Any` pubkey of type `/cosmos.crypto.secp256k1.PubKey` and
   ModeInfo DIRECT), SignDoc (body bytes, auth info bytes, chain id, account number) and TxRaw.
3. Sign with `cryptography`'s ECDSA (deterministic), decode the DER, normalise to low-S and emit 32-byte R‖S.
4. Implement bech32 and the RIPEMD-160 address check.
5. Parse CometBFT JSON-RPC by hand, including "tx not found" as an RPC error (`agents/auditor/chainclient/rpc.go:367-377`).
6. Reimplement typed committed-event decoding (`chain/msgkit/events.go:186-200`) and SVN-JCS-1 canonicalisation. The
   Python reference `jcs.py` has a known divergence: it accepts non-ASCII digits (`notes-08-10.md`, goal 09 Wave 1b).
7. Add a PostgreSQL driver and an ASGI server, both new locked dependencies.
8. Keep golden cross-language vectors against Go. Regenerate on every protocol revision; goal 10 already added schema
   version 2 payload kinds (`chain/app/encoding.go:92-97`).

Risks:
- New hand-written signing code is "unreviewed cryptography" (AGENTS.md, Scope).
- A second encoder can drift from the chain's strict decoder. Rejections then surface as `sdk/2` decode errors.
- RIPEMD-160 depends on the OpenSSL build.
- Licences of new drivers need checking. psycopg 3 is LGPL-3.0 **[Educated Guess, verify on PyPI]** and interacts with
  the open DEC-006 licence split (`docs/dependencies.md:7`).

### 5.3 Go path: code that would be reused

| Reused piece | Where | Use in the broadcaster |
| --- | --- | --- |
| `app.MakeEncoding()`: interface registry, strict decoder, DIRECT-only TxConfig | `chain/app/encoding.go:44-81` | build, sign, decode, validate typed requests with the chain's own rules |
| `BuildTx` (SDK `tx.SignWithPrivKey`, DIRECT, no fee, no memo, gas and size bounds from `genesis`) | `agents/auditor/chainclient/tx.go:29-82` | generalise from `*evidencev1.MsgSubmitAuditBatch` to `sdk.Msg`; add `timeout_height` / `timeout_timestamp` |
| `TxMessage` (strict decode, exactly one message) | `tx.go:84-100` | re-decode stored attempt bytes on restart |
| `HTTPClient`/`NewHTTP`: no proxy, no redirect, timeouts, 16 MiB body cap | `rpc.go:100-167` | chain RPC transport (rpc URL is explicit configuration) |
| `RPC` interface (Status, ABCIQueryWithOptions, BroadcastTxSync, Tx) | `rpc.go:71-80` | seam for unit-test fakes and for the e2e proxy |
| `Header` (chain id, height and time; refuses catching up) | `rpc.go:195-209` | check configured chain id against the node; stall detection |
| `Account` (number and sequence, address check) | `rpc.go:305-337` | sequence resync |
| `Broadcast` → `Ack` ("It is never a commit") | `rpc.go:339-356` | |
| `CommittedTx` (/tx; not found means not committed) | `rpc.go:367-384` | reconcile by hash |
| `SVNCode` | `rpc.go:386-397` | classify committed codes |
| Store-bytes-before-send, ACCEPTED/NOT_ACCEPTED/UNKNOWN record, reconcile before re-sign, bounded submissions | `agents/auditor/chainclient/submit.go:28-98,158-177,403-454` | the same state machine moved into PostgreSQL |
| `msgkit.ParseCommitted` (typed event and msg_index) | `chain/msgkit/events.go:186-200` | canonical commitment evidence for goal 12 |
| `leases.AcceptSignBytes`, `msgkit.VerifyInner` | `chain/x/leases/signdoc.go:88-99`; `chain/msgkit/innersig.go:115-128` | fail-fast check that a forwarded acceptance is signed by the provider's current key (the chain checks again) |
| Devnet key files, topology and accounts | `chain/devnet/init.go:100-144` | development keyring input |
| Test CA helpers (P-256 CA plus leaf) | `chain/devnet/audit_test.go:116-160`; `agents/auditor/fetch/fetchtest/fetchtest.go:59-131` | pattern for the mTLS development CA |

Two caveats:
- The auditor's chain client reads the chain id from the node and the next sequence from committed state before each
  broadcast. That suits its one-batch-at-a-time use. Goal 11 needs an explicit chain id and local sequence allocation
  (`scratchpad/goal-11/map-chain.md` 8.1 and 2.4).
- Copying versus extracting a shared client: extracting a shared package would refactor goal 09 code whose mutants
  anchor on it (`docs/evidence/goal-09/mutants.py`). **[Educated Guess]**: copy and generalise into the broadcaster's
  `internal/chain`, leave the auditor untouched, and record deduplication as a follow-up.

### 5.4 Comparison

| Criterion | Python service | Go service |
| --- | --- | --- |
| Blueprint fit | Python "App services" row, p. 7 | "operator-authorized Go broadcaster", p. 13 (`docs/blueprint.md:373`) |
| New dependencies | ASGI server, PostgreSQL driver, likely migration tool, cosmos protos, bech32 | none new: lib/pq already in `chain/go.sum`; everything else stdlib or already required |
| Signing code | new, hand-assembled DIRECT signer | SDK `tx.SignWithPrivKey` as used in `tx.go:67` |
| Decoder parity with the chain | must be proven by vectors | same `app.MakeEncoding` |
| Typed results and events | reimplemented | `msgkit.ParseCommitted`, `SVNCode` |
| Protocol revisions | regenerate Python and Go, re-verify vectors | recompile |
| Devnet e2e harness | spawn Python from Go tests, or a separate harness | built and driven from `chain/devnet` exactly like the auditor (`audit_test.go:218-260`) |
| CI | python job has no devnet or PostgreSQL; go-race jobs have no Python setup | fits a go-race devnet shard plus a PostgreSQL install step |
| Goal 13 FastAPI integration | same process family | Python calls the broadcaster over mTLS HTTP (stdlib `ssl`/`http.client`) or enqueues by SQL in its own transaction (8.3) |

### 5.5 Recommendation and what it changes in the monorepo

Recommend a Go broadcaster. **[Educated Guess]**; owner approval needed for the language and layout.

Proposed layout. Names are proposals.

```
services/broadcaster/
  go.mod                 module github.com/Maxencejules/shortvideo-network/services/broadcaster
                         replace .../chain => ../../chain, .../protocol => ../../proto   (as agents/auditor/go.mod:1-14)
                         direct requires: chain, protocol, cometbft, cosmos-sdk, gogoproto, protobuf, lib/pq v1.12.3
  go.sum                 every line also in chain/go.sum (the docs/dependencies.md:53 rule)
  README.md              rewritten: service API, outbox schema, signing boundary, states
  cmd/svn-broadcaster/main.go      subcommands: migrate | serve | reconcile | status | dev-ca
  internal/config        explicit chain_id, rpc_url, db (unix socket or 127.0.0.1, sslmode explicit), listen addr,
                         TLS files, key files; no defaults for chain id or rpc url; refuses a node whose chain id differs
  internal/store         database/sql + lib/pq; migrations runner; outbox queries
  internal/store/migrations/0001_outbox.pgsql ...   (not *.sql: .gitignore:70)
  internal/chain         generalised from agents/auditor/chainclient (BuildTx(sdk.Msg), RPC, Tx, Account, Header)
  internal/keys          development keyring: loads accounts/<name>.hex (mode 0600); per-key allow-list of msg type URLs
  internal/policy        intent kind -> signer class -> msg type; caller principal -> allowed kinds and pending caps
  internal/worker        per-signer loop, reconcile, backoff, deadlines, stall detection
  internal/api           net/http handlers, mTLS principal extraction, typed JSON, object permissions
  internal/pgtest        test helper: disposable PostgreSQL cluster (section 6.4)
```

Repository edits that follow. Each one needs justification in the evidence; the test edits need owner approval.

| File | Change |
| --- | --- |
| `go.work:5-11` | add `./services/broadcaster` |
| `scripts/run_tests.py:151` | add `"services/broadcaster"` to `GO_MODULES` |
| `services/broadcaster/svn_broadcaster/*` | delete the Python scaffold. Keeping it would leave `serve()` documenting "does not submit transactions" next to a service that does. |
| `tests/unit/test_scaffold.py:7,24-27` | remove the broadcaster scaffold test. This replaces a scaffold check with real service tests; it is not a weakening. |
| `tests/unit/test_repo_policy.py:17` | require `services/broadcaster/go.mod` instead of the Python scaffold path |
| `pyproject.toml:31,47` | drop `services/broadcaster` from `pythonpath` and `mypy_path` |
| `.github/workflows/ci.yml` | `go vet ./services/broadcaster/...`; race run of the module; PostgreSQL install; a devnet shard (section 9) |
| `tests/unit/test_ci_devnet_coverage.py` | no code change if the new shard follows the documented `-run`/`-skip` rule (`:1-20`) |
| `docs/dependencies.md` | lib/pq v1.12.3 as a direct dependency of `services/broadcaster` (MIT), noting it is already in svnd |
| `docs/sbom.cdx.json` | regenerate (`scripts/write_sbom.py:69` reads every `go.sum`) |

Alternative if the owner keeps Python: put the service in Python with FastAPI. Add uvicorn, a PostgreSQL driver and
generated cosmos protos. Add a Go-generated vector file that the Python signer must reproduce byte for byte, and a
devnet e2e harness that starts the Python service. Expect more work and the risks in 5.2.

---

## 6. Goal 04 environment and a disposable PostgreSQL here

### 6.1 What `scripts/env` does

| Script | Behaviour | Lines |
| --- | --- | --- |
| `common.sh` | `SVN_PG_BIN=/usr/lib/postgresql/18/bin`; RabbitMQ under `.toolchains/rabbitmq_server-4.4.0`; state root `SVN_STATE_ROOT` (default `<repo>/.local/workspaces`, must be absolute and under `.local` if inside the repo); credentials parsed as KEY=VALUE, never sourced; `pgpass` and `amqp.auth` written mode 0600; `PGPASSFILE` exported | `common.sh:11-12,25-42,112-161,166-177` |
| `common.sh` `svn_generate_credentials` | `svn_db` superuser with a 24-byte hex password; default port 5432 | `:190-216` |
| `start.sh` | refuses to reset state when credentials are missing; `initdb --auth-local=scram-sha-256 --auth-host=scram-sha-256 --encoding=UTF8 --locale=C.UTF-8`; `pg_ctl ... -o "-c listen_addresses=127.0.0.1 -c port=... -c unix_socket_directories=<ws>/postgres/run"`; `createdb`; **then always starts RabbitMQ** and checks AMQP login | `start.sh:17-30,36-61,74-83,85-135` |
| `stop.sh` | `pg_ctl -m fast stop`; `rabbitmqctl shutdown` | `stop.sh:18-30` |
| `reset.sh` | deletes one named workspace with `--workspace X --confirm X`, only if it carries the start.sh marker | `reset.sh:8-84` |
| `prove-restart.sh` | writes a row and a durable message, stop/start, reads them back | `prove-restart.sh:13-39` |
| `install.sh` | needs passwordless sudo; installs `postgresql-18=18.6-1.pgdg24.04+2` and Erlang; final check needs `python3` 3.12.3 | `install.sh:52,94-95,156` |

Goal 04 evidence says `svn_db` is the superuser created by `initdb --username`. It also says "Goal 12 is the first
goal that opens PostgreSQL for application queries, and that goal needs a separate application role"
(`docs/evidence/goal-04.md:286`). In fact goal 11 opens it first, so goal 11 needs non-superuser application roles.

### 6.2 What exists in this container

- `postgresql-18 18.6-1.pgdg24.04+2` and `postgresql-16 16.15-0ubuntu0.24.04.1`, both server and client
  (`dpkg -l`). `postgres --version` prints 18.6 and 16.15. The `postgres` user is uid 102, gid 104 (`id postgres`).
  `/var/run/postgresql` is group-writable by `postgres`.
- RabbitMQ 4.4.0 extracted in the old checkout's `.toolchains`. Erlang 27.3.4.18 installed.
- PG 18 has `uuidv7()`; PG 16 does not. Both have `gen_random_uuid`, `pg_try_advisory_lock`, `pg_advisory_xact_lock`,
  `pg_notify` and `pg_current_xact_id` (`grep -o ... /usr/share/postgresql/{18,16}/postgres.bki`). The PG 18 server
  binary contains `SKIP LOCKED`. The installed man page documents it as fit "to avoid lock contention with multiple
  consumers accessing a queue-like table" (`/usr/share/postgresql/18/man/man7/SELECT.7.gz`, line 1890).
- Disk: 1.8 GB free (`df -h /`).

### 6.3 Why `start.sh` does not work here as is

1. Root: `initdb` exits 1 as root ("cannot be run as root"; **[Verified here]**, section 1 item 7). The postgres server
   also refuses root (string in the binary: `"root" execution of the PostgreSQL server is not permitted.`).
2. Paths: the worktree and scratchpad sit under `/tmp/claude-0` (mode 0700 root). The `postgres` user cannot traverse
   them, so the scripts cannot run as `postgres` from the worktree, and a data directory cannot live in the scratchpad.
   `/home/user/shortvideo-network/.local` is reachable, but it belongs to the old goal-07 checkout the brief says not to
   use (brief section 0).
3. Coupling: `start.sh` always starts RabbitMQ (`start.sh:85-135`), which goal 11 does not use.
4. In the netns wrapper only loopback exists. TCP 127.0.0.1 works; a unix socket works too.

### 6.4 Recommended disposable cluster (test helper `internal/pgtest`)

**[Educated Guess]**, a design proposal. It uses no new script in `scripts/env`.

1. `dir := os.MkdirTemp("", "svn-pg-")` creates the directory under `/tmp`, outside `/tmp/claude-0`, mode 0700.
   When `os.Geteuid()==0`, `chown` it to `postgres` and run every `initdb`, `pg_ctl` and `postgres` child with
   `SysProcAttr.Credential{Uid: postgres, Gid: postgres}`. When not root (CI runner user), run them as self.
2. Write a pwfile (0600, removed after use). Run
   `initdb -D dir/data --username=svn_super --pwfile=... --auth-local=scram-sha-256 --auth-host=scram-sha-256 --encoding=UTF8 --locale=C.UTF-8`
   (the `start.sh:48-55` flags).
3. Start with
   `pg_ctl -D dir/data -l dir/postgres.log -w -t 60 start -o "-c listen_addresses='' -c unix_socket_directories='dir/run' -c fsync=on"`.
   That is unix socket only, no TCP port, no collision inside or outside the netns. Use
   `listen_addresses=127.0.0.1 -c port=<free>` instead when a TCP path must be exercised.
4. Create roles: an owner role `svn_broadcaster_owner` (runs migrations) and a runtime role `svn_broadcaster`
   (DML only), plus test caller roles if the SQL enqueue path (8.3) is used. Never use the superuser at runtime.
5. Connect with `host=dir/run dbname=svn sslmode=disable user=svn_broadcaster passfile=dir/pgpass` (lib/pq
   `PQ/connector.go:291,624-662`).
6. Fault hooks: `pg_ctl -m immediate stop` (crash; WAL recovery on restart), `pg_ctl -m fast stop`, `kill -9 <postmaster>`,
   and `pg_ctl start` again on the same data directory.
7. Cleanup: `pg_ctl -m fast stop`, then `os.RemoveAll(dir)`, in `t.Cleanup`.
8. Binary location: `SVN_PG_BIN`, default `/usr/lib/postgresql/18/bin` (matches `common.sh:11`). Record
   `postgres --version` in every evidence log. If the binary is missing the test **fails**, unless an explicit skip
   variable is set that CI forbids by a coverage guard (9.4). Skipped is not passed (AGENTS.md).

Keep SQL compatible with PG 16: no `uuidv7()`; generate ids in Go or use `gen_random_uuid()`. Then a runner with
another major still runs it. Record the version actually used.

### 6.5 RabbitMQ is not needed for goal 11

- REQ-TECH-004 assigns Celery and RabbitMQ to goal 14 (`docs/requirements.md:429`).
- The outbox is the queue. Workers poll `FOR UPDATE SKIP LOCKED` and may be woken by `LISTEN/NOTIFY`, which is
  delivered at commit (LISTEN man page lines 77-88). Publishing to a broker in addition would need its own outbox relay
  to stay atomic. That is the dual write the blueprint's outbox exists to avoid (`docs/blueprint.md:385`).

---

## 7. Authenticating internal endpoints and enforcing object permissions

The goal says "Authenticate internal endpoints and enforce object permissions" (`docs/goals/11-...:27-28`). No
commercial service is allowed (AGENTS.md). The options:

| Option | Mechanism (no new dependency) | Strengths | Weaknesses |
| --- | --- | --- | --- |
| A. mTLS with a development CA | Go `crypto/tls` server with `ClientAuth: tls.RequireAndVerifyClientCert` and a CA pool. The caller principal is a URI SAN such as `svn://principal/api`. The CA and leaves are generated at runtime by `svn-broadcaster dev-ca` into `.local/` (`.gitignore:12`; `*.pem`/`*.key` ignored at `:24-25`). Python callers use stdlib `ssl.SSLContext.load_cert_chain`. | reviewed TLS stack; works across hosts and containers; identity bound to a key; precedent CA code in tests (`audit_test.go:116-160`) | certificate lifecycle (expiry, revocation by redeploying the CA); slightly more setup |
| B. Unix socket plus SO_PEERCRED | listen on `unix:<dir>/broadcaster.sock` mode 0660; read the peer uid with `syscall.GetsockoptUcred`; map uid to principal | no secrets at all; trivially local | single host; uid-based identity breaks across containers unless sockets are shared; devnet tests run as root, so uids do not separate callers |
| C. HMAC request tokens | per-caller secret in a 0600 file; HMAC-SHA256 over method, path, timestamp, nonce and body hash; nonce table with TTL | stdlib only (Go `crypto/hmac`, Python `hmac`) | custom canonicalisation and a replay store are a home-made protocol; shared secrets must be distributed |
| D. PostgreSQL roles (for the SQL enqueue path) | each service connects with its own role (scram); `GRANT EXECUTE` on `broadcaster.enqueue_*` per role; row-level security on status reads keyed by `current_user` | the database already authenticates; makes "same transaction" enqueue possible across languages | only covers callers that write SQL; RLS mistakes are easy; still needs A or B for the HTTP API |

**Recommendation** (**[Educated Guess]**, owner decision):
- Use A for the HTTP API, with B as an optional local-only listener.
- Use D for the in-transaction enqueue path that goal 13+ Python services need. A function with fixed `search_path`
  and `SECURITY DEFINER`, granted per role.
- Do not use C.

The object permission model is the same whichever transport is used:
- `callers(principal PK, allowed_kinds text[], max_pending int, enabled bool)` is configuration, loaded at start and
  versioned. The pending caps are PROPOSED numbers and stay labelled unset or proposed (DEC-005 pattern, brief section 1).
- Every intent row stores `caller_principal` and an `on_behalf_of` subject. That is an opaque application id, never
  personal data; it is linkable, so say so.
- Create needs `kind ∈ allowed_kinds` and a pending count below the cap, checked in the same transaction as the insert.
- Read status, list and cancel are allowed only when `caller_principal = principal` (or an operator principal). Another
  caller's intent answers 404, not 403, so ids cannot be probed.
- The signer is never chosen by the caller. The kind maps to the signer class, and the signer class to a configured
  key whose allow-list of msg type URLs is checked again just before signing. That covers TM-KEYS "Least privilege for
  the broadcaster" (`docs/threat-model.md:89-97`) and DEC-001 "Operators, not viewers, hold chain-submission
  credentials" (`docs/decision-register.md:37`).
- Provider-signed messages (AcceptLease, RenewLease, provider EndLease) are forwarded with the BROADCASTER key as
  outer signer only. Before signing, the broadcaster strictly decodes the message with the app codec. It checks the
  kind is in the relay set. It checks the inner signature against the provider's current key from chain using
  `leases.AcceptSignBytes` and `msgkit.VerifyInner` (5.3). That is a fail-fast check; the chain is the authority
  (`docs/protocol.md:183,237`). Raw provider-signed transactions (the provider's own outer signature) are forwarded
  byte for byte and tracked by hash. The broadcaster allocates no sequence for them.
- The "publication permission bit" in REQ-CHAIN-013 (`docs/requirements.md:452`) needs a defined source. No
  publication record exists before goal 21/23. This is for the signing and decisions readers; flagged in section 12.

---

## 8. PostgreSQL transactional outbox

### 8.1 Proposed schema

Schema `broadcaster`. All lengths are bounded by `CHECK`.

| Table | Key columns | Notes |
| --- | --- | --- |
| `schema_migrations` | `version int PK`, `name`, `sha256 bytea`, `applied_at` | the runner refuses a changed checksum |
| `callers` | see section 7 | |
| `signers` | `address bytea(20) PK`, `name`, `key_class` (SERVICE, BROADCASTER), `account_number bigint`, `next_sequence bigint`, `lock_key int UNIQUE` | the row lock serialises sequence allocation |
| `intents` | `intent_id uuid PK`, `caller_principal`, `idempotency_key text`, `request_digest bytea(32)`, `kind`, `request bytea` (typed protobuf of the chain message or of the forwarded message), `business_id`, `signer_address`, `auth_context jsonb` (bounded), `state`, `status_reason`, `created_at`, `expires_at`, `attempt_count`, `next_attempt_at`, `final_at`, result columns (`tx_hash`, `height`, `tx_index`, `msg_index`, `code`, `codespace`, `gas_used`, `block_hash`, `events_digest`) | `UNIQUE(caller_principal, idempotency_key)`; `CHECK(expires_at > created_at)`; partial index on non-final states and `next_attempt_at` |
| `attempts` | `attempt_id uuid PK`, `intent_id FK`, `n smallint`, `signer_address`, `account_number`, `sequence`, `timeout_height`, `timeout_timestamp`, `tx_bytes bytea`, `tx_hash bytea(32) UNIQUE`, `created_at`, `broadcast_outcome` (NOT_SENT, ACK_ACCEPTED, ACK_REJECTED, TRANSPORT_ERROR, UNKNOWN), `ack_code`, `ack_codespace`, `ack_log` (clipped), `committed_*`, `dead_at` | `UNIQUE(signer_address, sequence) WHERE broadcast_outcome <> 'ACK_REJECTED' AND dead_at IS NULL`: at most one live transaction per (signer, sequence); `n < 16` (the `MaxSubmissions` precedent, `submit.go:79-80`) |
| `events_log` (optional) | append-only state transitions `(intent_id, at, from, to, reason)` | gives the "exact failure traces" deliverable without parsing logs |

Retention for `tx_bytes` and logs is unset. Mark it PROPOSED.

### 8.2 State machine (internal) mapped to the protocol enum

| Internal state | Meaning | `OperationStatus` exposed |
| --- | --- | --- |
| REQUESTED | intent and outbox committed; nothing signed | REQUESTED |
| SIGNED | attempt with bytes committed to the DB; broadcast not yet answered | SUBMITTED, with `submission: UNKNOWN` |
| SUBMITTED | CheckTx accepted (code 0) on one node; not committed | SUBMITTED |
| UNCERTAIN | transport error or timeout after the request may have reached a node | SUBMITTED, with `submission: UNKNOWN` |
| RETRY_WAIT | CheckTx refused with a retryable code (for example sdk 32 wrong sequence; sdk 4 right after a node start, DR-10-41 per `map-chain.md` 1.4); the sequence was not consumed | REQUESTED or SUBMITTED, with a reason |
| PENDING_STALL overlay | chain height not advancing for T seconds (PROPOSED T) or quorum loss observed | PENDING_STALL (`records.proto:51`; `cases.py:64-77`) |
| COMMITTED | `/tx` code 0 for one of this intent's hashes, or the business-id ledger or object state shows this intent's effect (8.6) | COMMITTED |
| FAILED | final: committed non-zero code (EXECUTION_FAILED); permanent CheckTx reject (REJECTED); expired with nothing sent (EXPIRED_NOT_SENT); expired with every attempt provably dead (EXPIRED_DEAD); refused by policy (UNAUTHORIZED) | FAILED, with a reason |

Rules:
- COMMITTED is set only from a committed result, never from an ack (REQ-ACK-001, `docs/requirements.md:483`).
- A committed non-zero code consumes the sequence, because ante writes are committed before messages run
  (`SDK/baseapp/baseapp.go:879,902-919`). A CheckTx or ante failure does not.
- "Uncertain" stays an internal state. Exposing it as its own enum value would change the frozen `records.proto`.

### 8.3 Enqueue: intent and outbox in one transaction, idempotent

```sql
BEGIN;
INSERT INTO broadcaster.intents (intent_id, caller_principal, idempotency_key, request_digest, kind, request,
                                 business_id, signer_address, auth_context, state, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'REQUESTED', now(), now() + $10::interval)
ON CONFLICT (caller_principal, idempotency_key) DO NOTHING
RETURNING intent_id;
-- no row: SELECT intent_id, request_digest FROM broadcaster.intents WHERE caller_principal=$2 AND idempotency_key=$3;
--         same digest -> return the existing intent (200); other digest -> 409 conflict
-- pending-cap check for $2 in the same transaction (count of non-final intents FOR UPDATE on the callers row)
NOTIFY broadcaster_work;   -- delivered at COMMIT only
COMMIT;
```

- The HTTP `POST /v1/intents` runs exactly this. A future Python service calls the same statement through the
  `broadcaster.enqueue_*` SQL function inside its own transaction (option D). That transaction also writes its own
  application row, which is what "Couple application intent and outbox insertion in one database transaction" asks for
  (`docs/goals/11-...:19-20`).
- `business_id` is derived deterministically from the intent. Retries reuse it, so the chain business gate turns a
  second committed copy into an idempotent success or a REPLAY (`chain/msgkit/business.go:88-119`).
- Wall clock: `now()` from PostgreSQL is the single service clock. Chain decisions never use it.

### 8.4 Claiming work and serialising per signer

- One worker loop per configured signer. On start it takes `pg_try_advisory_lock(<lock_key>)` on a pinned
  connection (`db.Conn(ctx)`). A session lock taken through the pool can be released on another connection. If the
  lock is not acquired, the process does not drive that signer (fencing between broadcaster replicas). PostgreSQL drops
  the lock when the connection ends: process death or PostgreSQL restart. The loop must then stop sending until it
  re-acquires the lock. **[Educated Guess]**: also set TCP keepalives so a half-open connection is noticed.
- Claim: `SELECT intent_id FROM broadcaster.intents WHERE signer_address=$1 AND state IN ('REQUESTED','RETRY_WAIT') AND next_attempt_at <= now() ORDER BY created_at, intent_id LIMIT $n FOR UPDATE SKIP LOCKED`.
  SKIP LOCKED also lets a separate reconcile loop and the signer loops work without blocking each other.
- Allocate and persist before sending, all in one transaction: `SELECT next_sequence, account_number FROM broadcaster.signers WHERE address=$1 FOR UPDATE`,
  then sign in process, then `INSERT INTO attempts (... tx_bytes, tx_hash, sequence, timeout_height ...)`, then
  `UPDATE signers SET next_sequence = next_sequence + 1`, then `UPDATE intents SET state='SIGNED'`, then COMMIT.
  Only then call `broadcast_tx_sync`. Record the answer in a second transaction. This is the auditor's
  store-bytes-then-send order (`submit.go:158-163`) moved into the database.
- Window: start with one in-flight transaction per signer. A later transaction with a higher sequence becomes invalid
  if an earlier one is dropped. Concurrency comes from several signers; the devnet has `service` and `service-2`
  (`chain/devnet/init.go:137-138`). A window above 1 is a later optimisation and needs its own failure tests.
  **[Educated Guess]**
- Sequence resync: when nothing is in flight for a signer, set `next_sequence` from the chain account query
  (`rpc.go:305-337`). A chain sequence above every known attempt means someone else used the key. Alert and stop that
  signer; do not guess.

### 8.5 Retries, backoff and deadlines (all numbers PROPOSED)

- Exponential backoff with full jitter, base 1 s, cap 60 s; at most 16 attempts per intent. `next_attempt_at` is stored,
  so restarts keep the schedule.
- Every attempt carries `timeout_height = H_now + K` (K PROPOSED). With `timeout_timestamp` the intent deadline maps to
  chain time. The ante refuses a transaction at height > `timeout_height` or block time after `timeout_timestamp`
  (`SDK/x/auth/ante/basic.go:205-227`). `map-chain.md` section 3 notes this is untested in this app. Goal 11 must test it.
- A deadline is what makes an uncertain attempt decidable. Once committed height > `timeout_height` and `/tx` does
  not show the hash with code 0, the attempt can never succeed. Mark it dead and re-sign; the same sequence is reusable
  if the chain sequence did not advance.
- During a halt, height does not advance, so no attempt can be declared dead. The intent stays PENDING_STALL, even past
  `expires_at`, until the chain moves past the attempt's `timeout_height`. Only then may it become FAILED
  (EXPIRED_DEAD) or COMMITTED. This is how "remains pending or fails honestly" is met
  (`docs/goals/11-...:38-39`).
- Bounded queue during an outage: `expires_at` is required and capped (PROPOSED). Per-caller and global pending caps
  are checked at insert. An intent that expires before any attempt was sent becomes FAILED (EXPIRED_NOT_SENT). The
  broadcaster never writes lease or projection rows. Final leases come from goal 12's indexer reading committed events
  (`docs/goals/11-...:28-29`; `docs/blueprint.md:387`).

### 8.6 Reconciliation (on start, after any UNCERTAIN, and periodically)

For each non-final intent, in order:
1. Re-decode every stored attempt's bytes with the app decoder (`tx.go:84-100`). Bytes that fail to decode are a
   fatal integrity error; report it, do not repair it.
2. For each attempt hash, call `/tx` (`rpc.go:367-384`). Code 0 means COMMITTED: record height, index, code, gas, the
   events digest via `ParseCommitted` and the block hash. A non-zero code means the attempt failed in execution and its
   sequence is consumed. If no other live attempt exists, the intent becomes FAILED (EXECUTION_FAILED) or, for an
   idempotent outcome, COMMITTED.
3. No hash found: read the business-id effect or object state (lease, offer and content queries; `map-chain.md`
   sections 6-7). This catches a commit through a transaction this broadcaster did not record. Examples: a
   provider's own submission, or a crash between broadcast and DB write in a window-1 design with a lost row (which
   should not happen).
4. Still nothing: if chain height > every live attempt's `timeout_height`, mark them dead. Then retry if before
   `expires_at`, otherwise FAILED (EXPIRED_DEAD).
5. Otherwise keep SUBMITTED or UNCERTAIN, or PENDING_STALL if the chain is not advancing. Never resend while a live
   attempt could still commit.

No step edits history by hand. Every transition is a new row in `events_log` plus an update of the current state.

### 8.7 Canonical commitment evidence handed to goal 12

Per committed intent, record:
- stable identities: `chain_id`, `height`, `tx_hash` (SHA-256 of bytes), `tx_index` (`ResultTx.Index`,
  `responses.go:201`), `msg_index` 0 (one message per transaction), `intent_id` and `business_id`;
- the result: `code`, `codespace`, `gas_used` and the typed events with their `msg_index` (`events.go:186-200`);
- the block hash at that height. Optionally keep a `/tx?prove=true` inclusion proof against the block's data hash.

That proof only proves inclusion relative to a header from the one configured node. Disclose the gateway trust; goal 12
handles reconciliation against canonical queries.

### 8.8 Migration tooling

- Nothing is locked for migrations in Python. In Go, only `adlio/schema` is present, as a test-only transitive (4.3).
- **[Educated Guess]**: write a small runner in `internal/store`:
  - embed ordered migration files;
  - apply each in its own transaction under `pg_advisory_xact_lock(<constant>)`;
  - record version, name and SHA-256 in `schema_migrations`;
  - refuse a checksum mismatch or a gap;
  - forward-only. Down migrations are not needed for a disposable beta store; restore comes from backups in goal 26.
- File extension: `.gitignore:70` ignores `*.sql` (and `:69-74` dumps and DBs). Name migrations `*.pgsql`, or keep the
  SQL as Go string constants. Do not add a `!*.sql` negation: it would weaken the database-dump rule that
  `tests/unit/test_repo_policy.py` checks with `cache-dump.sql`.
- Run migrations with the owner role. The runtime role gets DML only.

---

## 9. CI: PostgreSQL, devnet and broadcaster within the limits

### 9.1 Current constraints

- Jobs `python`, `node`, `go` (with `SVN_SKIP_DEVNET=1`), `go-race` shards `app`, `devnet`, `devnet-evidence`,
  `devnet-upgrade`, `rest`, plus `devnet` (containers) and `contract` (`.github/workflows/ci.yml:20-233`).
- Every job: `timeout-minutes` 1..20, permissions `contents: read`, only the four pinned actions, setup-go `cache: false`
  (`scripts/workflow_policy.py:14-21,82-145`).
- `workflow_policy.py` does not inspect `services:` (job service containers) or `container:`. Using them is not refused
  by the checker. Pinning their image digests would be a new rule.
- Devnet shard rule: `devnet` uses `-skip` with the union of every other shard's `-run` prefixes. Each `-run` shard
  selects at least one top-level test. The guard is `tests/unit/test_ci_devnet_coverage.py:1-20`.
- Existing timings (local, not CI): race shards rest 327 s, app 210 s, devnet 474 s (`notes-08-10.md`, 2026-11-02
  20:38Z); CI devnet-evidence ran 309.5 s (`notes-08-10.md`, 2026-11-07 01:20Z).
- Block cadence in the devnet: `TimeoutCommit` 200 ms with `SkipTimeoutCommit` (`chain/node/start.go:244-245`).

### 9.2 PostgreSQL in CI: options

| Option | How | For | Against |
| --- | --- | --- | --- |
| P1. Install the pinned package | step `sudo apt-get install postgresql-18=18.6-1.pgdg24.04+2` after adding the PGDG key with the fingerprint in `docs/dependencies.md:39` (as `install.sh:66-95` does); the test helper runs initdb as the runner user | exact pin; same code path as local (installed binaries, disposable cluster); the test can crash and restart the server | network fetch from apt.postgresql.org during CI, a documented dependency; adds time **[Educated Guess: under a minute]** |
| P2. Service container | `services: postgres: image: postgres:18.6@sha256:f4b419e3...` (digest from `deployments/compose/docker-compose.yml:9`, `docs/dependencies.md:46`) | pinned digest already recorded; no apt step | the test cannot restart or crash the server without driving Docker; a different code path from local; Docker Hub pull |
| P3. Runner's preinstalled PostgreSQL | whatever the ubuntu-24.04 image ships **[Educated Guess: a 16.x]** | no install | not the pinned 18.6; image drift |

**Recommendation:** P1. The acceptance check "Restart PostgreSQL/broadcaster" needs the test to own the server
process. **[Educated Guess]**

### 9.3 Where the end-to-end test lives

Put `TestDevnetBroadcaster*` in `chain/devnet`. It builds `services/broadcaster` into a temp dir as
`buildAuditor` does (`audit_test.go:218-238`). It starts a disposable PostgreSQL (6.4) and a devnet with
`startDevnet` (`lease_test.go:440-473`). It reuses the lease setup builders and the council helpers
(`map-chain.md` 8.2), and drives the broadcaster binary through its mTLS API.

Why:
- It reuses the proven start, stop and stall helpers (`prove_test.go:455-509`; `stopForStall`/`observeStall`,
  `map-chain.md` 9.3) and `reservedAt` (`lease_test.go:353-360`).
- It keeps the chain module free of a services import; the binary is built, as for the auditor.
- The alternative, an e2e test inside `services/broadcaster`, would have to copy those test-only helpers, because
  `chain/devnet` helpers are `_test.go` files and not importable.

Unit and PostgreSQL-integration tests of the broadcaster live in its own module. Chain RPC fakes are allowed there,
labelled as unit tests (AGENTS.md).

### 9.4 Proposed CI edits (each a proposal)

1. A new go-race shard `devnet-broadcaster`:
   `go build -C services/broadcaster -o /dev/null ./cmd/svn-broadcaster`, then the PostgreSQL install step, then
   `go test -race -count=1 -timeout 15m -run TestDevnetBroadcaster ./chain/devnet`.
   Add `TestDevnetBroadcaster` to the `devnet` shard's `-skip` union. The coverage guard accepts "any later shard"
   (`test_ci_devnet_coverage.py:6-8`).
2. Module tests of `services/broadcaster`:
   - The `go` job runs them through `run_tests.py` (`GO_MODULES`). It needs PostgreSQL installed, or a skip variable
     `SVN_SKIP_POSTGRES=1` allowed only while another job runs them. A guard test like `test_ci_devnet_coverage.py`
     enforces that.
   - Add `go test -race ./services/broadcaster/...` to the `rest` shard (needs PostgreSQL), or give it its own job.
3. `go vet ./services/broadcaster/...` in the `go` job (`ci.yml:102-105` pattern).
4. Time budget **[Educated Guess, to be measured]**: one devnet start to height 3 plus one council admission and a
   content/offer setup, then about 8 fault scenarios sharing that devnet, should take a few minutes under `-race`.
   Split scenarios across two `-run` shards if a measured run gets near 12 minutes.

---

## 10. Fault injection for the acceptance checks (`docs/goals/11-...:35-40`)

The general technique:
- A **loopback reverse proxy** in the test process sits between the broadcaster and validator 0's RPC.
- It is built with `net/http/httputil.ReverseProxy`. It reads the bounded JSON-RPC body to find `method`
  (CometBFT's client POSTs JSON-RPC, `http_json_client.go:221,280`).
- It applies a per-method fault: pass, drop-request (never forwarded), forward-then-drop-response, delay(d), or
  connection reset.
- It counts forwarded `broadcast_tx_*` requests, which proves "sent once".
- The broadcaster's `rpc_url` is the proxy.

The node, the chain and PostgreSQL are real. Evidence must say "fault injected by a test proxy on loopback; one machine,
four processes". Unit tests may use a fake `RPC` (`rpc.go:71-80`); that is never e2e evidence.

| Acceptance item | Injection | Assertions |
| --- | --- | --- |
| Timeout **before** submission | proxy drop-request on `broadcast_tx_sync`; the broadcaster's request times out | proxy forward count 0; state UNCERTAIN then, after `timeout_height` passes, attempt dead and re-sign with the **same** sequence; one commit; one lease or offer; `reservedAt` equals the package bytes once |
| Timeout **after** submission | forward-then-drop-response on `broadcast_tx_sync` | forward count 1; the broadcaster finds the hash by `/tx` and does not resend while the attempt is live; COMMITTED with that hash; zero extra forwards |
| Worker death **after commit** | proxy blocks `tx` and `abci_query` for the broadcaster only; the test's own RPC client sees the commit on a node; `SIGKILL` the broadcaster; restart with pass-through | after restart: COMMITTED, no new `broadcast_tx_*`, one business effect, sequence allocation unchanged |
| Worker death between persisting an attempt and the broadcast | proxy holds the broadcast request; `SIGKILL` the broadcaster; proxy then forwards (variant a) or discards (variant b) | (a) reconcile finds the commit; (b) reconcile marks the attempt dead after `timeout_height` and re-sends once; both give one effect |
| Duplicate requests | same `(principal, idempotency_key)` 20 times concurrently; same key with a different body; two keys with the same business payload | one intent id; 409 for the different body; the chain business gate gives one effect (second transaction idempotent or REPLAY) and the API reports one COMMITTED effect per intent honestly |
| Concurrent same-signer | N intents for `service` concurrently | sequences in `attempts` unique and dense; all COMMITTED; chain account sequence advanced by exactly the number of included transactions (committed and execution-failed) |
| Unauthorized messages | principal without the kind; a kind whose signer class does not allow the msg type (for example asking the BROADCASTER key for OfferLease); a seat or auditor message | refused before signing (403, or 404 for another caller's intent); no attempt row; chain state unchanged |
| Provider impersonation | AcceptLease with an inner signature from a service key or a wrong provider key; raw-forward of a transaction whose inner signature is forged | pre-check refuses; the forced raw path gives a chain UNAUTHORIZED (`map-chain.md` section 10; `TestServiceKeyCannotSignAcceptanceInApp`); FAILED with the code; `reservedAt` unchanged |
| Stop quorum | stop validators 3 and 4 (`stopForStall`); submit | for the stall period the API never returns COMMITTED; state SUBMITTED or PENDING_STALL. Restart before the deadline: COMMITTED once. Variant: the intent expires during the stall; after restart the chain passes `timeout_height`, so FAILED (EXPIRED_DEAD), no lease, `reservedAt` unchanged |
| Restart PostgreSQL | `pg_ctl -m immediate stop` while intents are in SIGNED, SUBMITTED and REQUESTED; then `pg_ctl start` | the broadcaster stops sending while it cannot persist; after the restart it reconciles every intent to the chain's truth; no manual SQL |
| Restart broadcaster | `SIGTERM` and `SIGKILL` at random points of a 50-intent batch | after restart every intent is final or honestly pending; one effect per business id; no orphan reservation |
| No leaked capacity, accurate status | after all scenarios | for each provider, `reservedAt` = sum of `package_bytes` of its held leases (chain query); every COMMITTED intent's hash is on chain with code 0; every FAILED intent has no effect on chain |

Also: after node restarts, CheckTx returns sdk 4 until that node commits a block (DR-10-41; `map-chain.md` 1.4).
The broadcaster must classify that as retryable. A test should cover it.

---

## 11. Recommended design summary

1. Go module `services/broadcaster` (5.5). Codec and signer from the chain (`encoding.go:44-81`; `tx.go:29-82`).
   No new third-party module; lib/pq is already in `chain/go.sum:486-487` and in svnd (`CMT/node/setup.go:40`).
2. Explicit configuration, with no default for `chain_id` or `rpc_url`. Refuse a node whose `Header().ChainID` differs
   (`rpc.go:195-209`). Database DSN without a password; password through `PGPASSFILE` (`common.sh:166-177`;
   `PQ/connector.go:291`). Explicit `sslmode`.
3. Outbox schema and state machine as in 8.1 and 8.2. External status uses the frozen `OperationStatus` names plus a
   reason field.
4. Same-transaction enqueue through `POST /v1/intents` and a granted SQL function for later Python services (8.3).
5. One worker per signer with a pinned-connection advisory lock. Persist bytes before sending. Window 1. Deadlines on
   every attempt. Reconcile before every resend (8.4-8.6).
6. mTLS with a runtime development CA in `.local/`. Principal from the certificate SAN. Per-principal kinds and pending
   caps. 404 on another caller's object (section 7).
7. Development keyring loads `accounts/<name>.hex` from the devnet home. Each key carries an allow-list of msg type
   URLs. Keys are never logged, returned or stored in PostgreSQL. `slog` JSON logs with a `ReplaceAttr` redactor that
   drops key material, tx bytes, DSN passwords and `auth_context` values; hashes only. The broader signing boundary is
   in `map-signing.md` section 10.
8. Disposable PostgreSQL 18.6 helper for tests (6.4). No RabbitMQ (6.5).
9. E2E test in `chain/devnet` driving the built binary with the loopback fault proxy (9.3, 10). CI: PGDG install step
   plus a `devnet-broadcaster` race shard (9.4).
10. Deliverables named by the goal: exact failure traces from `events_log` plus captured logs, the outbox schema
    (migrations plus README), the service API (README plus typed request/response structs), signing-boundary
    documentation, and commitment evidence for goal 12 (8.7).

---

## 12. Needs owner approval (decision requests)

| # | Item | Why it needs the owner | Proposed default |
| --- | --- | --- | --- |
| R-1 | Broadcaster language: Go instead of the Python scaffold | changes a goal 03 layout choice; the blueprint says Go (`:373`) but REQ-TECH-002 frames services as Python | Go |
| R-2 | Remove `services/broadcaster/svn_broadcaster` and edit `test_scaffold.py:24-27`, `test_repo_policy.py:17`, `pyproject.toml:31,47` | edits existing tests and layout pins | replace with a `go.mod` pin and real tests |
| R-3 | lib/pq v1.12.3 as a direct dependency | new direct dependency, though already compiled into svnd | accept |
| R-4 | CI: PGDG apt install step in two jobs, new race shard, `SVN_SKIP_POSTGRES` guard | new network fetch in CI; job-time budget | P1 (9.2) |
| R-5 | Internal auth: mTLS with a development CA (and the unix-socket local option) | security boundary choice | A, plus D for SQL enqueue |
| R-6 | Same-transaction enqueue through `SECURITY DEFINER` SQL functions and per-service PostgreSQL roles | DB privilege model that goal 13+ inherit | yes |
| R-7 | External status keeps the 5 frozen `OperationStatus` values; "uncertain" stays internal with a reason field | the alternative is a protocol change to `records.proto` | no protocol change |
| R-8 | PROPOSED numbers: attempts (16), backoff (1 s to 60 s), `timeout_height` margin K, stall threshold T, max `expires_at`, per-caller and global pending caps, retention of `tx_bytes` and logs | policy numbers must not be invented (brief section 1) | configurable, labelled PROPOSED |
| R-9 | Window of one in-flight transaction per signer | throughput versus failure complexity | 1 |
| R-10 | Copy the auditor chain client into the broadcaster rather than extract a shared package | duplication versus refactoring goal 09 code under mutation anchors | copy now, deduplicate later |
| R-11 | Source of the "publication permission bit" (REQ-CHAIN-013) before goals 21/23 exist | security-critical, undefined today | for the signing and decisions readers |
| R-12 | Development signing boundary: in-process keyring versus a separate signer process on a unix socket | "approved development signing boundary" is a goal prerequisite (`docs/goals/11-...:8,30`) | see `map-signing.md` sections 10-11 |

---

## 13. Residual risks seen from the runtime side

- Single node trust: `/tx`, `/status` and queries are answered by one configured node (`rpc.go:9-14`). A lying node
  can make the broadcaster mark something committed. Disclose it; goal 12 reconciles.
- Two processes holding the same key outside the advisory lock (misconfiguration) can still sign. The chain business
  gate keeps one effect, but sequences will collide. Detect and stop (8.4).
- `LISTEN/NOTIFY` is a wake-up hint only. Correctness rests on polling with SKIP LOCKED, because notifications are lost
  while disconnected.
- PostgreSQL crash recovery depends on `fsync=on`. A test must not turn it off for speed.
- Mempool behaviour across nodes, such as gossip timing and two same-sequence transactions in different mempools, is
  not under the broadcaster's control (`map-chain.md` section 12). The tests prove the outcomes listed in section 10,
  not mempool internals.
- Runtime cost on this machine: about 1.8 GB free disk. Each disposable cluster is tens of MB **[Educated Guess]**;
  clean it up per test.

---

## 14. Not verified here, and discrepancies found

- Nothing was built or run except these read-only probes: `initdb` as root (refused, nothing created), the Python
  `cryptography` and `hashlib` capability checks, `go doc`, `go mod why` (with the default `-mod`; `git status`
  clean afterwards), and `grep`/`strings` on installed files.
- I did not run PostgreSQL as `postgres` with a data directory under `/tmp`, because the task limits writes to this
  directory. Section 6.4's recipe is unexecuted.
- I did not check CI runner facts: preinstalled PostgreSQL, sudo, Docker Hub limits, apt time. They are marked
  **[Educated Guess]**.
- psycopg, asyncpg and uvicorn licences were not checked on PyPI.
- **Discrepancy:** `scratchpad/goal-11/map-chain.md` section 8.1 cites `agents/auditor/chainclient/rpc.go:200-267`,
  `:295-309`, `:318-437`, `:439-456`, `:458-484` and `:486-497`. The file has 397 lines at both `dbf0897` and `6d74aca`
  (`git show dbf0897:agents/auditor/chainclient/rpc.go | wc -l`). The actual ranges are `HTTPClient`/`NewHTTP` at
  `:100-167`, `Header` at `:195-209`, `query` and the record readers at `:218-337`, `Broadcast` at `:339-356`,
  `CommittedTx` at `:367-384` and `SVNCode` at `:386-397`. The plan should use these.
- The brief's environment note says PostgreSQL 16.15 is what exists. 18.6 is now installed too (6.2). Use 18.6 and
  record the version.
