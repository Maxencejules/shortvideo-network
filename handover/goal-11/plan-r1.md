# Goal 11 plan: transaction broadcaster and PostgreSQL outbox (services/broadcaster, signing boundary, reconciliation, devnet fault tests)

Planning lead output, revision 1. It is built from the four reader maps in `scratchpad/goal-11/` (`map-signing.md`,
`map-chain.md`, `map-decisions.md`, `map-runtime.md`). Every load-bearing fact was re-read on the tree below or in the
module cache before it went into this plan; section 1.7 lists the reader citations that turned out wrong and the values
used instead. No production code was written. No repository file was edited, added, committed or pushed. This revision
wrote only `scratchpad/goal-11/plan.md`.

- Worktree (the only place any agent reads, edits, builds or tests):
  `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/wt-11` (written `$WT`),
  branch `goal-11`. The task text says the branch was created from main `dbf0897` and that PR 15 merges before goal 11
  commits. PR 15 has merged: main is `6d74aca` and `$WT` was fast-forwarded to it
  (`scratchpad/notes-08-10.md:272-277`). HEAD is `6d74aca5cfc461a9756587bdbec2f78e7111e574`, tree `5062bb44`. `git diff
  --stat dbf0897 6d74aca` touches only `chain/app/app.go`, `chain/cmd/svnd/cli/genesis*.go` and a README
  (`map-runtime.md` header). Every line number in this plan is for `6d74aca`. Never touch `/home/user/shortvideo-network`
  (an old goal-07 checkout; `scratchpad/brief.md` section 0).
- Netns wrapper for anything that starts svnd or PostgreSQL in a devnet run:
  `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh <cmd...>`
  (written `$NETNS`). It runs `unshare --net` with only loopback up; there is no outbound network, so Go modules must be
  cached (`netns-run.sh:2-4`).
- Goal spec: `docs/goals/11-transaction-broadcaster-and-outbox.md` (never edit). "goal spec :N" means that file, line N.
  Playbook assignment: GPT-6 Astra, EFFORT HIGH (goal spec :6).
- Short names: `SDK/` = `/root/go/pkg/mod/github.com/cosmos/cosmos-sdk@v0.55.0`, `CMT/` =
  `/root/go/pkg/mod/github.com/cometbft/cometbft@v0.40.0`, `PQ/` = `/root/go/pkg/mod/github.com/lib/pq@v1.12.3`
  (`go env GOMODCACHE` = `/root/go/pkg/mod`). "spec_machine" = `docs/evidence/goal-02/spec_machine.py`, "cases" =
  `docs/evidence/goal-02/cases.py`.
- Labels. A sentence with a `file:line` citation is a fact read on this tree or in the module cache. "Derived from
  source" means read in the pinned source but not exercised by any test in this repository; goal 11 must test it before
  relying on it. `[Educated Guess]` marks an inference. Every design item in sections 3 to 13 is a proposal, not an owner
  decision. Nothing in this plan is an owner decision unless it quotes one.
- Owner rulings that bind goal 11 (quoted from the records; no owner ruling about goal 11 itself exists):
  - DR-06-06, 2026-11-02: "Seat-only admission". A seat key signs only propose and approve; a seat address is never a
    submitter (`scratchpad/notes-08-10.md:86-90`; `chain/app/ante.go:139-180`). The broadcaster never holds a seat key.
  - Goal 09 Q1, 2026-11-02: "Account key signs (Recommended)": "The submitting account must be an `AUDITOR`. ... No
    `BROADCASTER` relay, no rotation. Devnet beta only." (`docs/evidence/goal-09.md:74`). The broadcaster never submits
    audit batches (DR-11-06).
  - DR-10-00, 2026-11-08: "Approve as pinned", for `docs/architecture/governance-and-upgrades.md` at `18fa310`, for goal 10
    only (`scratchpad/notes-08-10.md:221`). It is a precedent for how a goal 11 boundary document could be approved; it
    approves nothing for goal 11.
  - Model substitution: "I acknowledge the model substitution for DR-06-07", then "extend the acknowledgement to goals 07
    through 30" (`scratchpad/brief.md` section 5).
  - Relayed user request for this run: "I authorize you to merge it and complete the next goals"
    (`scratchpad/notes-08-10.md:272`). "it" is PR 15. This plan reads the sentence as authority to work on goal 11. It
    does not answer any DR-11 request below, and the brief says each PR and each merge is authorized separately
    (`scratchpad/brief.md` section 0). Whether it also pre-authorizes a goal 11 PR or merge is for the orchestrator to
    confirm with the owner, not for this plan to assume.
- Owner questions. Four security-critical questions must be answered before Wave 1 (section 17.1): Q1 signing boundary
  and the broadcaster's key set (DR-11-01), Q2 the provider-side submission path (DR-11-02), Q3 the publication
  permission, hosting-intent custody, caller authentication and object permissions (DR-11-03), Q4 what the outbox may
  store and for how long (DR-11-04). Approval of the boundary document itself is asked at PR time on a pinned commit and
  file sha256, as DR-10-00 was. Wave 0 (section 18) does not depend on any answer.
- Disclosure. The runtime reader ran `initdb` as root once while mapping; it refused ("cannot be run as root"), exited 1
  and created nothing (`map-runtime.md` section 1 item 7). It ran `go mod why -m github.com/lib/pq` with the default
  `-mod`; `git status --short` in `$WT` stayed empty. Nothing else was executed.

Section map: 1 findings; 2 scope, non-scope, must not; 3 language and runtime; 4 architecture and file layout; 5 typed
request API; 6 outbox schema and the one-transaction coupling; 7 signing boundary; 8 submission engine; 9 state machine
and commitment evidence; 10 chain outage; 11 configuration; 12 repository integration points; 13 test plan; 14 acceptance
and requirements matrix; 15 evidence capture list; 16 expected status and residual risks; 17 decision requests; 18 work
packages; 19 orchestrator notes; 20 steps that still need a human.

---------------------------------------------------------------------------------------------------

## 1. Findings that shape the plan

### 1.1 The chain transaction path a broadcaster faces

- One TxConfig: `app.MakeEncoding` enables only `SIGN_MODE_DIRECT` and installs a strict decoder that rejects any unknown
  field in the body, including non-critical ones (`chain/app/encoding.go:44-81,110-138`). Each registered message has one
  20-byte `signer` field that is its only signer (`chain/app/encoding.go:144-173`).
- The ante chain (`chain/app/ante.go:24-45`) runs, in order: SetUpContext, ExtensionOptions(nil), ValidateBasic,
  TxTimeoutHeight, envelope (non-empty memo `BOUNDS`; fee payer, granter or tip `FEE_REJECTED`, `:66-86`), memo, bounds
  (gas outside `[1, 20,000,000]` except 0 in simulation, tx above 65,536 bytes: `BOUNDS`, `:90-108`), tx-size gas, fee
  reject (`:112-121`), SetPubKey, sig count, sig gas, signature verification, admission (`:139-180`), sequence increment.
  No memo, fee payer, granter or tip can carry an intent id.
- `NewTxTimeoutHeightDecorator` is installed (`chain/app/ante.go:32`). In SDK v0.55.0 it rejects a transaction when
  `timeout_height > 0` and the context height exceeds it (sdk code 30), or when `timeout_timestamp` is set and before the
  block time (sdk code 42) (`SDK/x/auth/ante/basic.go:205-227`; `SDK/types/errors/errors.go:100,147`). No repository code
  sets either field (`map-chain.md` 3.5). Goal 11 must test the semantics itself (section 13.3).
- Unordered transactions are refused: the account keeper is built without `WithUnorderedTransactions(true)`
  (`chain/app/app.go:199-207`), so per-signer sequences are strict (`map-chain.md` 3.3, `SDK/x/auth/ante/sigverify.go:319-325`).
- CheckTx runs only the ante chain; handlers run only in FinalizeBlock and Simulate (`scratchpad/notes-08-10.md:64-65`).
  A CheckTx code 0 says nothing about roles, the business gate or capacity: on the devnet an acceptance passes CheckTx
  with code 0 and commits with svn `CAPACITY` (`chain/devnet/lease_test.go:507-530`).
- An ante failure writes nothing; a handler failure after a passing ante keeps the ante writes, so the sequence is
  consumed (`SDK/baseapp/baseapp.go:897-919,975-983`, `map-chain.md` 1.3).
- After every node start, CheckTx uses account number 0 until that node commits a block, so correctly signed transactions
  from accounts numbered above 0 get sdk code 4 (DR-10-41; `docs/architecture/governance-and-upgrades.md:667-683`;
  `SDK/x/auth/ante/sigverify.go:377-382`). It caused two goal 10 test flakes (`docs/evidence/goal-10.md:124,183`).
- The node ignores `config.toml`; it builds CometBFT config from `cmtcfg.DefaultConfig()` plus `node.json`
  (`chain/node/start.go:211-234`). The mempool is therefore the v0.40.0 default: flood type, recheck on, gossip on, no
  WAL, cache of 10,000 entries (`CMT/config/config.go:1021-1033`), and no TTL (`map-chain.md` 2.3). Admitted bytes wait
  until a block includes them, recheck evicts them, or every node holding them restarts.
- Devnet blocks are produced continuously: `CreateEmptyBlocks = true`, interval 0, `TimeoutCommit` 200 ms
  (`chain/node/start.go:237-248`). Heights keep advancing on an idle chain, so a `timeout_height` passes without traffic.
- `broadcast_tx_sync` returns the CheckTx answer; a mempool error before CheckTx (for example `tx already exists in
  cache`, `CMT/mempool/errors.go:11-12`) comes back as a JSON-RPC error (`map-chain.md` 4.1). The `/check_tx` route calls
  the app's CheckTx without adding to the mempool (`CMT/rpc/core/mempool.go:186-194`); derived from source, it still
  advances the check-state sequence, so the broadcaster never uses it.
- Reads: `/tx?hash=` returns height, index, result and the raw bytes; `/tx_search` takes queries up to 512 bytes and at
  most 100 results per page (`CMT/rpc/core/events.go:16-18`; `CMT/rpc/core/env.go:23-24`). The ante emits
  `tx.acc_seq=<bech32>/<seq>` for every transaction whose ante reaches SetPubKey, failed handlers included
  (`map-chain.md` 4.3, derived from source). Typed events copy string attributes as they are and index every attribute
  (`chain/msgkit/events.go:60-75`), so a query such as `svn.events.v1.LeaseAccepted.lease_id='l1'` is well formed
  (derived from source). No event carries a business id (`map-chain.md` 5.2).
- The node also serves the SDK tx service on its gRPC listener (`chain/node/start.go:113-139`;
  `chain/app/app.go:691-694`). No repository test calls it; this plan does not use it.

### 1.2 Roles, signatures and business ids

- Roles are set at genesis; no message adds a submitter (`docs/architecture/authority-authorization.md:16-17`). The
  devnet has `authority`, `service`, `service-2`, `provider-1`, `provider-2`, `broadcaster` (secp256k1) and `auditor-1`,
  `auditor-2` (ed25519) (`chain/devnet/init.go:119-144`), plus four seat keys (`map-chain.md` 10).
- Who may sign what, as the code enforces it (`map-signing.md` section 5):
  - `RegisterContent`, `WithdrawContent`: `ADMITTED_SERVICE` only; the signer becomes `submitting_host`; a `BROADCASTER`
    outer signer is `FORBIDDEN` (`chain/x/content/msg_server.go:78,186,348-357`; `chain/x/content/README.md:39`).
  - `OfferLease`: `ADMITTED_SERVICE`; the signer becomes the scheduler (`chain/x/leases/msg_server.go:239`).
  - `AcceptLease`, `RenewLease`, provider `EndLease`: `BROADCASTER` (relay) or the provider's own `ADMITTED_PROVIDER`
    account, with the provider's inner signature by its current key (`chain/x/leases/msg_server.go:405,449,628-630`).
  - `EndLease{authority_end}`: one `AUTHORITY` signature, "outside DEC-009" (`chain/x/leases/msg_server.go:680-681`;
    `docs/protocol.md:239`).
  - `SubmitAuditBatch`: `AUDITOR` only; a `BROADCASTER` relay is `FORBIDDEN` (`chain/x/evidence/README.md:277`).
  - Provider registry messages: the provider account only (`map-signing.md` section 5).
- Provider consent is an ed25519 inner signature by the provider's current registered key over the stored offer terms and
  the lease id (ACCEPT), the message (RENEW), or lease id, provider id and current term version (END)
  (`chain/x/leases/README.md:146-162`). Exported preimage builders exist for broadcasters: `AcceptSignBytes`,
  `RenewSignBytes`, `EndSignBytes` (`chain/x/leases/signdoc.go:92,104,117`; README `:170-173`). Limits: no chain id in
  any inner payload, no expiry on RENEW or END, no proof that the provider holds its registered key
  (`chain/x/leases/README.md:148,164-168`; `map-signing.md` section 6).
- The provider never signs the business id: the `AcceptLease` business fields are `lease_id, offer_id,
  provider_acceptance` (`chain/x/leases/README.md:138`). The relayer chooses the business id.
- Hosting intents (devnet interim): the intent key signs `{action, contentId, contentVersion, manifestSha256 |
  reasonSha256, packageBytes, roleName, submittingHost}`, where `submittingHost` is the outer signer, so one intent
  authorizes one host (`chain/x/content/intent.go:14-33`). The key is the one in the message; the signature "does not
  prove a creator authorized the host" (`chain/x/content/README.md:61`). Every non-devnet chain is `DECISION_BLOCKED`
  (`:60`).
- Business ids: one unsigned namespace across five modules (`map-chain.md` 6). Same id and payload is code 0 with
  `<module>_result idempotent=true` and no typed event (`chain/msgkit/events.go:324-329`); another payload is `REPLAY`; a
  failed attempt stores nothing (`docs/protocol.md:185`). Every check before the gate runs again on a retry, so a retry of
  a committed action can return `FORBIDDEN`, `UNAUTHORIZED`, `BOUNDS` or a role failure; "the outbox reads the committed
  result by transaction hash, or the lease state, before it resubmits, and never treats a resubmission's code as the
  first transaction's result" (`chain/x/leases/README.md:607-618`). A committed rotation is reconciled by querying the
  provider (`chain/x/providers/README.md:126-128`). Any admitted account can take a pending business id first; the
  auditor draws retry ids from `crypto/rand` (`chain/x/evidence/README.md:703-712`).
- No gRPC query returns anything by business id, by closed offer id or by ended lease id (`map-chain.md` 7). Raw `/store`
  reads are unmetered and unpaged (`scratchpad/notes-08-10.md:61-63`).
- `Query/Governance` returns the submitters with their roles and the devnet seats
  (`docs/protocol/proto/svn/authority/v1/state.proto:70-78`; `chain/x/authority/README.md:160-166`). The broadcaster can
  check each configured key's role from committed state at start-up.

### 1.3 Existing client code and the scaffold

- `agents/auditor/chainclient` is the only chain client outside tests (`map-chain.md` 8.1). Reusable: the hardened HTTP
  client (no proxy, no redirect, timeouts, response cap, `agents/auditor/chainclient/rpc.go:102-167`), the `RPC`
  interface (`:73-80`), `Header` (refuses a catching-up node, `:188-209`), the account query with an address check
  (`:305-337`), `Broadcast` whose answer "is never a commit" (`:340-356`), `CommittedTx` ("not found" is a JSON-RPC
  error, `:359-386`), `SVNCode` (`:388-397`), `BuildTx` with SDK `tx.SignWithPrivKey` (`tx.go:34-82`) and the strict
  re-decode `TxMessage` (`tx.go:86-100`); the submitter's store-bytes-then-send order and its states
  (`submit.go:28-98,596-636`).
- Not reusable as is: `BuildTx` and `TxMessage` are typed to `*evidencev1.MsgSubmitAuditBatch` (`tx.go:34,86`); the chain
  id comes from the node and the sequence from committed state before each send (`submit.go:596-603`); no deadline field
  is set; `CommittedTx` does not check that the returned bytes hash to the requested hash (`rpc.go:370-385`). The goal
  09 mutation runner anchors on `chainclient/submit.go` (`docs/evidence/goal-09/mutants.py:341,396`), so refactoring the
  auditor would move goal 09 anchors.
- `services/broadcaster` is a Python scaffold: `serve()` raises "does not submit transactions"
  (`services/broadcaster/svn_broadcaster/scaffold.py:1-7`); the README says it "does not hold keys and it does not speak
  to a node" (`services/broadcaster/README.md:3`). Pinned by `tests/unit/test_scaffold.py:24-27` and
  `tests/unit/test_repo_policy.py:17`; listed in `pyproject.toml:31,47` and `README.md:14`.
- `services/api` raises too, and goal 13 is the first API goal (`map-decisions.md` section 1 item 10). No caller of the
  broadcaster exists yet.

### 1.4 Protocol and spec facts about the outbox

- Client operation states: "requested, submitted, committed, failed, and pending while the chain is stalled"
  (`docs/protocol.md:217`). The frozen enum is `OperationStatus` with REQUESTED, SUBMITTED, COMMITTED, FAILED,
  PENDING_STALL (`docs/protocol/proto/svn/offchain/v1/records.proto:45-52`). It has no "uncertain" value.
- "The broadcaster, not a chain handler, records `PENDING_STALL` in its outbox" (`docs/protocol.md:260`). DEC-011's
  approved text: "Losing more than one third halts the real network because the environment produces no block and the
  outbox records `PENDING_STALL`." (`docs/decision-register.md:337`). The reference outbox `broadcaster_submit` parks a
  request with its business id when blocks are withheld (`cases:64-77`); `pending` is excluded from the app hash
  (spec_machine `:2030-2041`).
- Reconciliation fields the protocol says are retained, "and does not treat as a retention period": business id,
  transaction hash, message type, height, committed time, object id, digest, byte count, result code, reservation delta
  and the idempotent flag (`docs/protocol.md:272`).
- "A broadcaster may relay a transaction it did not author. It cannot manufacture the inner signature."
  (`docs/protocol.md:183`). The relay column of the transition table lets a broadcaster submit content messages and audit
  batches (`docs/protocol.md:233-234,240`); the code refuses both (recorded deviations, `chain/x/content/README.md:39`;
  `chain/x/evidence/README.md:277`).
- The protocol envelope rule constrains the fee amount and the gas limit (`docs/protocol.md:187`); it says nothing about
  `timeout_height`. `[Educated Guess]` Setting `timeout_height` is not a protocol change (DR-11-05).
- Blueprint: "Python services request bounded operations through an operator-authorized Go broadcaster or generated
  client" (`docs/blueprint.md:373`); "Use a transaction outbox in PostgreSQL ... records attempt IDs and reconciles
  unknown submissions before resending. Use a stable business ID" (`:385`); "A successful broadcast or mempool response
  is not a committed successful transaction ... reconcile after timeouts before retrying" (`:305`); the broadcaster "must
  not be able to manufacture provider acceptance or bypass a user's publication permissions" (`:242`); "If the chain
  stops, queue requests with explicit expiry and show them as pending" (`:395`); the chosen gateway is trusted for reads
  at beta (`:391`); the idempotency test row (`:766`).

### 1.5 Environment and CI facts

- PostgreSQL 18.6 (the goal 04 pin, `docs/dependencies.md:39`) is installed at `/usr/lib/postgresql/18/bin`, and 16.15
  at `/usr/lib/postgresql/16/bin` (`map-runtime.md` 6.2; `ls /usr/lib/postgresql` shows `16 18`). The brief's
  environment line still says only 16.15 exists (`scratchpad/brief.md` section 4); use 18.6 and record the version.
- `initdb` and the server refuse root (`map-runtime.md` 6.3). The `postgres` user is uid 102, gid 104 (`id postgres`).
  `/tmp/claude-0` is mode 0700 root, so `postgres` cannot reach the worktree or the scratchpad; `/tmp` is 1777 and
  `TMPDIR` is empty here. `scripts/env/start.sh` always starts RabbitMQ (`map-runtime.md` 6.1, 6.3) and assumes a
  non-root user.
- `github.com/lib/pq v1.12.3` (MIT) is already in `chain/go.mod:129` as an indirect dependency and in svnd, because
  CometBFT's node imports it (`CMT/node/setup.go:40`). It has no dependency of its own (`PQ/go.mod`). Its `sslmode`
  default is `require` (`PQ/connector.go:304-306`). `requirements.lock` pins no PostgreSQL driver, ASGI server, bech32
  or Cosmos client (`map-runtime.md` 4.1).
- `.gitignore:70` ignores `*.sql`; `.local/` (`:11`), `*.pem` and `*.key` (`:24-25`) are ignored.
- Go workspace modules: `agents/auditor`, `agents/provider`, `chain`, `docs/evidence/goal-02/go`, `proto` (`go.work:5-11`).
  `scripts/run_tests.py:151` lists the Go modules the test runner covers. The chain module may contain only
  `chain/cmd/svnd/main.go` (`tests/unit/test_repo_policy.py:48-50`). The SBOM writer reads every `go.sum`
  (`scripts/write_sbom.py:68`).
- CI: every job at most 20 minutes (`scripts/workflow_policy.py:21`), four pinned actions (`:14-19`); go-race shards
  `app, devnet, devnet-evidence, devnet-upgrade, rest` (`.github/workflows/ci.yml:128`). The `devnet` shard uses `-skip`
  with the union of the other devnet shards' `-run` filters (`:152`), and `tests/unit/test_ci_devnet_coverage.py:1-20`
  fails unless every `chain/devnet` test runs exactly once. No CI job has PostgreSQL.
- Disk: 1.8 GB free on `/` (`df -h /`, 2026-11-08). Four CPUs; other work runs.

### 1.6 Prerequisites and inherited status

| Prerequisite (goal spec :8; `docs/progress.md:41`) | State on this tree | Effect on goal 11 |
| --- | --- | --- |
| Goal 04 environment | BLOCKED (`docs/progress.md:34`); local PostgreSQL and RabbitMQ checks were VERIFIED there | PostgreSQL runs here; record the version. RabbitMQ is not used (REQ-TECH-004 is goal 14, `docs/requirements.md:429`). |
| Goals 07, 08, 09, 10 | BLOCKED in their own status, merged into main (`docs/progress.md:37-40`) | Every goal 11 result is devnet-only, single machine. |
| "Signing boundaries from Goal 02" | Goal 02 is IMPLEMENTED (`docs/progress.md:32`); `docs/protocol.md:3` says it is not an approved protocol | Not approved. |
| "approved service/provider signing boundaries" | No such document. Gate A accepted "the proposed signing domains" only (`docs/decision-register.md:23`). | BLOCKED until the owner approves a pinned boundary document (Q1, DR-11-01). |
| "DEC-001 blocks host custody of user keys" | DEC-001 default approved; on chain only `DEVNET_TEST_ONLY` intents pass (`docs/protocol.md:140`) | Production content registration stays `DECISION_BLOCKED`. Goal 11 holds no creator key. |

### 1.7 Discrepancies found while planning (report; do not edit)

- `map-chain.md` 8.1 cites `agents/auditor/chainclient/rpc.go:200-497`. The file has 397 lines. Correct ranges:
  `RPC` `:73-80`, `HTTPClient`/`NewHTTP` `:102-167`, `Header` `:188-209`, queries `:221-337`, `Broadcast` `:340-356`,
  `CommittedTx` `:359-386`, `SVNCode` `:388-397` (read on this tree).
- `chain/app/app.go` lines after 478 moved by PR 15. `RegisterTxService` is at `:691-694`, not `:669-685`.
- `buildAuditor` is at `chain/devnet/audit_test.go:220`, not `:218`.
- `docs/protocol.md:129` says the fixture `down` set does not produce `ERROR_CODE_QUORUM`, while `cases:76` returns it
  from the reference outbox. The real outbox must not report a chain code for a stall: it reports `PENDING_STALL`.
- `docs/evidence/goal-04.md:286` says goal 12 is the first goal to open PostgreSQL for application queries; goal 11 is.
- `docs/threat-model.md:18` says a host-held key exists "only if DEC-001 is later accepted"; DEC-001 is approved
  (`map-signing.md` section 9).
- `docs/protocol.md:233-234,240` (relay column) versus the code (section 1.4): already recorded deviations.

---------------------------------------------------------------------------------------------------

## 2. Scope, non-scope, and what goal 11 must not do

### 2.1 In scope (goal spec :18-32)

- A broadcaster service at `services/broadcaster`: typed requests, a PostgreSQL transactional outbox, a per-signer
  submission engine, reconciliation, a caller API with authentication and object permissions, a development signing
  boundary, explicit chain id and gateway configuration, redacted logs.
- The single-transaction coupling of application intent and outbox insertion, shown with a fixture application schema
  in the same database (section 6.3).
- Real devnet acceptance tests for every acceptance check (goal spec :35-40), with real PostgreSQL, real process kills
  and a loopback fault proxy.
- Handoff: failure traces, outbox schema, service API and signing-boundary documentation; stable identities and
  canonical commitment evidence for goal 12 (goal spec :43-44).

### 2.2 Not in scope (not to be claimed)

- Any production path: content registration off the devnet is `DECISION_BLOCKED` (DEC-001, DR-07-01); offers and
  renewals need the DEC-005 devnet test number (`chain/x/leases/README.md:124-125`).
- The API service (goal 13), the creator UI states (goal 21), the provider agent (goal 16), the indexer and projections
  (goal 12), key rotation and backup drills (goal 26), packaging (goal 27), load numbers (goal 28).
- Provider registry messages, council messages, `authority_end`, and audit batches: not routed (DR-11-12, DR-11-06).
- Read proofs: reads trust one configured node, disclosed (DR-11-08).
- Independent operators; every devnet result is one operator, one machine, four processes.

### 2.3 What goal 11 must NOT do (any of these is a stop-and-report)

1. Hold or request a user, creator, provider, seat, AUTHORITY, AUDITOR or validator key. The broadcaster loads only the
   keys Q1 allows; start-up refuses any other role (section 7.1).
2. Synthesize provider consent. No broadcaster code path signs in an `SVN-PROVIDER-*` or `SVN-HOSTING-INTENT-1` domain;
   the signer signs only the outer `SIGN_MODE_DIRECT` envelope (section 7.2).
3. Report COMMITTED from a broadcast acknowledgement, a mempool answer, a timer, or the broadcaster's own database. Only a
   committed result read from the chain moves a row to COMMITTED (section 9.4).
4. Create final leases or any projection in the database. The broadcaster has no lease or content table; goal 12 builds
   projections from committed events (goal spec :28-29; `docs/blueprint.md:387`).
5. Require a live credential, an external service or an undocumented network dependency for the devnet. All listeners are
   loopback or Unix sockets; all keys and certificates are generated at run time (goal spec :31-32).
6. Invent a policy number. Retries, deadlines, expiry, caps and stall windows are PROPOSED configuration with bounds
   (DR-11-10); retention stays unset (DEC-004, `docs/decision-register.md:127`).
7. Change `.proto`, `bounds.json`, `docs/protocol.md`, `docs/evidence/goal-02/**`, `docs/goals/**`,
   `docs/decision-register.md` or `docs/threat-model.md`. Change any chain production code. Goal 11 adds chain tests only
   (section 13.3).
8. Use raw `/store` queries, `/check_tx`, unordered transactions, or a multi-message transaction of unrelated intents.
9. Merge, push, open a PR, or change branch protection (orchestrator only, with owner approval).
10. Weaken or delete a test to get green. The two scaffold tests are replaced by design and recorded (section 12).
11. Claim an audit, production readiness, custody, or operator independence.

---------------------------------------------------------------------------------------------------

## 3. Language and runtime choice

Proposal: a Go module at `services/broadcaster` (DR-11-20, reversible; the orchestrator tells the owner in the same
message as Q1-Q4).

| Criterion | Go service | Python service |
| --- | --- | --- |
| Blueprint | "operator-authorized Go broadcaster" (`docs/blueprint.md:373`) | Python row covers accounts, policies, social, feed, moderation (`docs/blueprint.md:203`) |
| Signing | SDK `tx.SignWithPrivKey`, as the auditor does (`agents/auditor/chainclient/tx.go:67`) | hand-built SIGN_MODE_DIRECT, pubkey `Any`, low-S R||S, bech32, RIPEMD-160: new crypto-adjacent code ("unreviewed cryptography", AGENTS.md) |
| Decoder parity | the chain's own `app.MakeEncoding` (`chain/app/encoding.go:44-81`) | must be proven by cross-language vectors |
| Typed results | `msgkit.ParseCommitted`, `SVNCode`, `leases.*SignBytes`, `msgkit.VerifyInner` | reimplemented |
| New dependencies | none: lib/pq is already in `chain/go.sum` and svnd | ASGI server, PostgreSQL driver, migration tool, cosmos protos, bech32 (`map-runtime.md` 4.1-4.2) |
| Devnet harness | built and driven from `chain/devnet` like the auditor (`chain/devnet/audit_test.go:220`) | a new cross-language harness |

Python services (goal 13 onward) never load keys; they reach the broadcaster through its HTTP API or the granted SQL
enqueue function (section 6.3). PostgreSQL 18.6 runs the outbox; the SQL avoids 18-only features such as `uuidv7()` so a
16.x server still works (`map-runtime.md` 6.2). RabbitMQ is not used: the outbox is the queue, and publishing to a broker
as well would reintroduce the dual write the outbox removes (`docs/blueprint.md:385`).

---------------------------------------------------------------------------------------------------

## 4. Architecture and file layout

### 4.1 Processes (development layout; one binary, three roles)

```
caller (test harness now; goal 13 API later)
   | mTLS loopback HTTP  (or the granted SQL function, in the caller's own DB transaction)
   v
svn-broadcaster serve    -- API, validation, outbox writes, per-signer workers, reconciler
   | Unix socket 0600 + peer-uid check                  | lib/pq (unix socket, sslmode explicit)
   v                                                    v
svn-broadcaster signer   -- holds the key files,        PostgreSQL 18.6 (schema broadcaster)
                            allowlist, outer signing only
   |
svn-broadcaster serve --> CometBFT RPC of ONE configured node (loopback): status, abci_query, broadcast_tx_sync, tx, tx_search, commit
```

The signer process exists only under Q1 option (a) (section 7.2). Under Q1 (b) the keyring is a package inside `serve`.

### 4.2 Module and packages (names are proposals)

```
services/broadcaster/
  go.mod, go.sum                module github.com/Maxencejules/shortvideo-network/services/broadcaster
                                replace .../chain => ../../chain, .../protocol => ../../proto (as agents/auditor/go.mod);
                                direct requires: chain, protocol, cometbft v0.40.0, cosmos-sdk v0.55.0, gogoproto,
                                protobuf, lib/pq v1.12.3; every go.sum line also in chain/go.sum            [B1/INT]
  README.md                     rewritten: service API, states, outbox schema, signing boundary summary      [B5]
  cmd/svn-broadcaster/main.go   subcommands: serve | signer | migrate | reconcile-once | status | dev-ca | dev-db [B7]
  internal/config/              typed config, bounds on every PROPOSED number, explicit chain id and rpc url  [B7]
  internal/store/               database/sql + lib/pq; migrations runner; every outbox query; roles            [B1]
  internal/store/migrations/    0001_outbox.pgsql ... (not *.sql: .gitignore:70)                              [B1]
  internal/pgdev/               disposable PostgreSQL cluster (initdb, pg_ctl, crash hooks, privilege drop)  [B1]
  internal/chain/               generalised from agents/auditor/chainclient: BuildTx(sdk.Msg, timeout_height),
                                Decode, RPC (Status, ABCIQuery, BroadcastTxSync, Tx, TxSearch, Commit), Account,
                                Provider, Content, ActiveLeases, Deadlines, Governance queries, answer classifier,
                                committed-effect comparison per kind                                          [B2]
  internal/outbox/              pure state machine: states, transitions, dead-attempt rule, reconcile decision
                                over typed observations; no I/O                                               [B3]
  internal/keys/                key file loader (symlink, mode, format checks, as agents/auditor/keys)        [B6]
  internal/signer/              signer process and its client; per-key type-URL allowlist; outer signing only [B6]
  internal/policy/              kind -> signer class -> type URL; caller kinds; object ownership; provider
                                signature pre-check; publication permission                                   [B6]
  internal/worker/              per-signer loop, advisory-lock fencing, persist-then-send, deadlines, backoff,
                                reconcile, stall detection                                                    [B8]
  internal/api/                 net/http handlers, mTLS principal, strict JSON, status views                  [B7]
  internal/logx/                slog JSON handler with a redacting ReplaceAttr                                [B6]
  internal/pgtest/              test helper over pgdev (never linked into the binary's non-test code)         [B1]
docs/architecture/broadcaster.md   signing boundary (PROPOSED; approval asked at PR time, pinned), states,
                                   schema, API, residual risks                                                [B5]
chain/app/tx_deadline_test.go      timeout_height and acc_seq semantics through FinalizeBlock (tests only)    [B4]
chain/devnet/broadcaster_test.go   real devnet acceptance tests and the loopback fault proxy                  [B9]
tests/unit/test_chain_writer_policy.py   REQ-INT-001 static policy with planted-violation fixtures            [INT]
tests/unit/test_ci_postgres_coverage.py  guard for the SVN_SKIP_POSTGRES skip (mirror of the devnet guard)    [INT]
docs/evidence/goal-11/mutants.py   mutation runner (same design as goals 08-10)                               [B10]
```

### 4.3 Dependency direction

- `services/broadcaster -> {chain/app (MakeEncoding), chain/msgkit, chain/genesis, chain/address, chain/x/leases
  (SignBytes builders), chain/x/content (intent canonical form, if exported; else a local copy checked by vectors),
  protocol types, SDK, CometBFT RPC client, lib/pq}`. The chain module never imports the services module.
- `chain/devnet/broadcaster_test.go` builds the broadcaster binary into a temp directory, as `buildAuditor` does
  (`chain/devnet/audit_test.go:220`), and starts PostgreSQL through `svn-broadcaster dev-db`, so the chain module does
  not import broadcaster packages and the PostgreSQL helper has one implementation (DR-11-24, DR-11-29).
- `internal/outbox` imports nothing but the standard library and typed observation structs, so its property tests need
  no database or chain.

### 4.4 Shared client package

No shared package now (DR-11-21). `internal/chain` is copied and generalised from the auditor client; the auditor stays
untouched because goal 09 mutants anchor on it (section 1.3). When goal 16 needs provider-side signing (path P2), extract
a shared package and record it then.

---------------------------------------------------------------------------------------------------

## 5. Typed request API

### 5.1 Kinds and the chain message each produces (under the recommended Q1/Q2)

| Kind | Chain message | Outer signer (fixed by kind, never by the caller) | Caller supplies | Broadcaster supplies |
| --- | --- | --- | --- | --- |
| `REGISTER_CONTENT` | `MsgRegisterContent` | `ADMITTED_SERVICE` key | content id, version, manifest digest, package bytes, hosting intent (signed by a key the broadcaster does not hold) | signer, business id |
| `WITHDRAW_CONTENT` | `MsgWithdrawContent` | the same service key that registered the content (`submitting_host`) | content id, version, reason digest, withdraw intent | signer, business id |
| `OFFER_LEASE` | `MsgOfferLease` | `ADMITTED_SERVICE` key, pinned per offer (D-11) | offer id, provider id, content id/version, manifest digest, bytes, start, deadline, expiry, terms digest | signer, business id, term version 1 |
| `RELAY_ACCEPT_LEASE` | `MsgAcceptLease` | `BROADCASTER` key | offer id, lease id, provider acceptance (InnerSignature) | signer, business id |
| `RELAY_RENEW_LEASE` | `MsgRenewLease` | `BROADCASTER` key | lease id, term version, deadline, manifest digest, bytes, evidence id (empty), provider signature | signer, business id |
| `RELAY_END_LEASE` | `MsgEndLease{authority_end=false}` | `BROADCASTER` key | lease id, provider signature | signer, business id |
| `FORWARD_PROVIDER_TX` (only if Q2 includes P3) | the provider's own signed tx bytes, verbatim | the provider's `ADMITTED_PROVIDER` account (not a broadcaster key) | raw tx bytes | nothing; no sequence allocated |

No kind exists for provider registry messages, council messages, `authority_end`, or audit batches (DR-11-12, DR-11-06).
An unknown kind is 422 and writes nothing.

### 5.2 Endpoints (HTTP/1.1 JSON over mTLS on loopback)

| Method and path | Effect | Errors |
| --- | --- | --- |
| `POST /v1/intents` | validate, then insert intent and outbox rows in one DB transaction; 201 new, 200 existing (same idempotency key, same request digest) | 400 malformed, 401 no client certificate, 403 kind not allowed or permission missing, 404 object of another caller, 409 same key with another digest, 413 body above the cap, 422 invalid request or provider signature, 429 open-intent cap, 503 database unavailable |
| `GET /v1/intents/{intent_id}` | status view (5.8) | 404 unknown or another caller's |
| `GET /v1/intents?status=&page_token=&limit=` | the caller's own intents, at most 100 per page (PROPOSED) | 400 |
| `POST /v1/intents/{intent_id}/cancel` | only while REQUESTED with no attempt ever signed: FAILED(`CANCELLED`) | 409 once an attempt exists (signed bytes cannot be unsent) |
| `GET /v1/signers` | public signer addresses per class, so an account host can name `submittingHost` in an intent (`chain/x/content/intent.go:20-22`) | none |
| `GET /v1/health` | chain id, latest height and time seen, stall flag, database reachability; no secrets | none |

### 5.3 Request and response types

- Go structs per kind in `internal/api`, decoded with `json.Decoder.DisallowUnknownFields` and a body cap (PROPOSED 65,536
  bytes, equal to `genesis.MaxTxBytes`, `chain/genesis/bounds.go:29-31`). Ids are checked against the chain id pattern
  `^[a-z0-9][a-z0-9._-]{0,63}$` (`chain/msgkit/ids.go:29-44`); digests are 32 bytes; integers are range-checked against
  the chain bounds before any insert. The typed request is converted to the protocol message and re-encoded with the
  app codec; the stored `request` column is that protobuf, so the worker never re-parses caller JSON.
- No new `.proto` file (DR-11-30): the request and response bodies are JSON documented in the README; the status field
  reuses the frozen `OperationStatus` names.

### 5.4 Idempotency keys

- Every create carries `idempotency_key` (caller-chosen, same id pattern). `UNIQUE(caller_principal, idempotency_key)`
  in PostgreSQL decides; the server computes `request_digest` = SHA-256 of the canonical typed request (kind plus
  protobuf bytes plus the auth context digest). Same key and digest returns the existing intent; another digest is 409.
- The idempotency key is the caller's dedupe; the business id is the chain's (8.2). Two different keys with the same
  chain effect are two intents; the chain keeps one effect and the second intent reports it honestly (DR-11-28).

### 5.5 Authorized request context (under Q4 option (a))

- Allowed fields only: `principal_kind` (`DEV_FIXTURE` in goal 11), `principal_id` (opaque, id pattern), `permissions`
  (enum list: `PUBLISH_CONTENT`, `WITHDRAW_CONTENT`, `SCHEDULE_HOSTING`, `RELAY_PROVIDER_SIGNED`), `policy_ref` (id
  pattern). The server adds `caller_principal` (from the certificate) and `request_digest`.
- Refused: any other key, any email, IP address, user agent, token, cookie, header copy or free text. A test sends each
  and expects 400 with no row. Opaque ids and digests are still linkable (REQ-OFF-004); the README says so.
- Stored as a bounded `jsonb` (PROPOSED cap 2,048 bytes); never logged; returned to its own caller only as a digest.

### 5.6 Authentication of internal endpoints (under Q3 option (a))

- HTTP: TLS 1.3, `ClientAuth: tls.RequireAndVerifyClientCert`, a development CA generated at run time by
  `svn-broadcaster dev-ca` into a 0700 directory under `.local/` (ignored, `.gitignore:11`) or a test temp dir. The
  principal is the client certificate's single URI SAN `svn://principal/<id>`; a certificate without exactly one such
  SAN is refused. Listener binds loopback only; a non-loopback address is a config error.
- SQL path: each caller service connects with its own PostgreSQL role (scram-sha-256); `broadcaster.callers` maps the
  role name to the principal (section 6.3).
- Development principals refuse to load when the configured chain id lacks the `shortvideo-dev-` prefix
  (`docs/protocol.md:136`), so a fixture cannot enable access on another chain (goal 13 rule,
  `docs/goals/13-authenticated-uploads-and-quarantine.md:17-19`).

### 5.7 Object permissions

- `callers` config: principal -> allowed kinds, open-intent cap, and optional bindings (`provider_id` for a provider-agent
  principal). Loaded at start; changing it is a restart.
- Read, list, cancel: only intents whose `caller_principal` equals the requester; another caller's intent answers 404 so
  ids cannot be probed.
- `REGISTER_CONTENT`: the auth context carries `PUBLISH_CONTENT` for `principal_id`, and the publication permission check
  of 7.4 passes. The broadcaster records `content_owners(content_id, version) -> (caller, principal_id)` in the same DB
  transaction as the intent.
- `WITHDRAW_CONTENT`: `WITHDRAW_CONTENT` permission and the same caller and principal as the recorded owner; otherwise
  404. A withdraw is an irreversible tombstone (`docs/protocol.md:234`).
- `OFFER_LEASE`: `SCHEDULE_HOSTING`; the content must be owned through this outbox and, at validation and again before
  signing, not withdrawn on chain (`Query/Content`).
- Relay kinds: `RELAY_PROVIDER_SIGNED`, plus either the caller is bound to that provider id, or the caller owns the
  `OFFER_LEASE` intent of that offer. The provider signature pre-check of 7.3 always runs.
- Open-intent caps per caller and globally are checked in the insert transaction (PROPOSED numbers, DR-11-10).

### 5.8 Status exposed to callers (DR-11-09: no protocol change)

```
{ "intent_id", "kind", "status": "OPERATION_STATUS_REQUESTED|SUBMITTED|COMMITTED|FAILED|PENDING_STALL",
  "detail": { "phase": "QUEUED|RETRY_WAIT|SIGNED|ACKNOWLEDGED|UNCERTAIN|AWAITING_DEADLINE",
              "reason", "attempts", "last_ack": {"code","codespace"} },
  "commitment": { ... section 9.5 ... },           // only when COMMITTED
  "failure": { "reason", "code", "codespace" },    // only when FAILED
  "expires_at", "created_at" }
```

"Uncertain" is `SUBMITTED` with `detail.phase = UNCERTAIN`; "pending" is `PENDING_STALL` while the chain is stalled
(section 10). Adding an `UNCERTAIN` enum value would change the frozen `records.proto`, so it is recorded as a protocol
addition (DR-11-40), not made.

---------------------------------------------------------------------------------------------------

## 6. Outbox schema and the one-transaction coupling

### 6.1 Roles and schema

- Schema `broadcaster`. Roles: `svn_broadcaster_owner` (owns the schema, runs migrations), `svn_broadcaster` (runtime:
  DML on its tables, no DDL), one role per caller service for the SQL path (EXECUTE on the enqueue function, SELECT on a
  per-caller view only). The cluster superuser exists only in the dev cluster bootstrap and is never used at run time.
  Goal 04 created only a superuser (`docs/evidence/goal-04.md:286`); goal 11 adds the non-superuser roles.
- Every query is parameterized (TM-INJECTION, REQ-SEC-009); `search_path` is fixed on every function.

### 6.2 Tables (every text and bytea column has a length `CHECK`)

| Table | Columns (main) | Constraints and notes |
| --- | --- | --- |
| `schema_migrations` | version PK, name, sha256, applied_at | runner refuses a changed checksum or a gap |
| `callers` | role_name PK, principal UNIQUE | SQL-path mapping; HTTP principals come from config |
| `signers` | address bytea(20) PK, class (`SERVICE`, `BROADCASTER`), key_name, account_number, next_sequence, observed_height, lock_key UNIQUE, halted_reason | row lock serialises sequence allocation; `halted_reason` set on a signer conflict (DR-11-15) |
| `intents` | intent_id uuid PK, caller_principal, idempotency_key, request_digest bytea(32), kind, request bytea, auth_context jsonb, chain_id, signer_address FK, business_id, business_attempt smallint, state, phase, reason, created_at, expires_at, next_attempt_at, attempt_count, final_at, committed_tx_hash, own_transaction bool | `UNIQUE(caller_principal, idempotency_key)`; `CHECK(expires_at > created_at)`; partial index on non-final state and `next_attempt_at`; `chain_id` must equal the configured chain id |
| `business_attempts` | intent_id, n, business_id UNIQUE, created_at, outcome | a new business id only after reconciliation shows no effect (8.6); n bounded |
| `attempts` | attempt_id uuid PK, intent_id FK, n smallint, signer_address, account_number, sequence, timeout_height, gas, tx_bytes bytea, tx_hash bytea(32) UNIQUE, created_at, phase, sends smallint, unknown_sends smallint, ack_code, ack_codespace, ack_log (clipped), committed_height, committed_index, committed_code, committed_codespace, gas_used, dead_reason, dead_at | partial `UNIQUE(signer_address, sequence) WHERE dead_at IS NULL AND committed_height IS NULL` (at most one live transaction per signer sequence); `n < 16` (auditor precedent `submit.go:78-80`) |
| `commitments` | intent_id PK, chain_id, height, block_hash, tx_hash, tx_index, msg_index, code, codespace, gas_wanted, gas_used, events jsonb, idempotent_replay bool, first_commit_tx_hash | written once, only from a committed result (9.5) |
| `content_owners` | (content_id, content_version) PK, caller_principal, principal_id, signer_address, intent_id | written with the `REGISTER_CONTENT` intent; read by withdraw and offer checks |
| `creator_intent_keys` (only under Q3 (a)) | principal_id, public_key bytea(32), registered_at | dev fixture registry, written only by an operator command, never by a caller |
| `transitions` | id bigserial, intent_id, attempt_id, at, from_state, to_state, phase, reason, observation jsonb (bounded) | append-only; the "exact failure traces" deliverable comes from this table |
| `chain_health` | chain_id PK, latest_height, latest_time, seen_at | updated by the worker; drives the stall overlay (section 10) |

Retention: no column is deleted automatically; retention is unset (DEC-004; `docs/protocol.md:272`).

### 6.3 Enqueue in one database transaction

- HTTP path: `POST /v1/intents` runs validation (7.3, 7.4, 5.7), then one transaction: insert into `intents`
  (`ON CONFLICT (caller_principal, idempotency_key) DO NOTHING RETURNING`), on no row read the existing digest (same ->
  200, other -> 409), check caps with `FOR UPDATE` on a per-caller counter row, insert `business_attempts` n=1 and any
  `content_owners` row, insert the first `transitions` row, `NOTIFY broadcaster_work` (delivered at commit only), commit.
- SQL path (for goal 13+ services, which write their own application row in the same transaction): `SELECT
  broadcaster.enqueue_intent(kind, request_json, auth_context, idempotency_key, expires_in)` — `SECURITY DEFINER`, fixed
  `search_path`, `REVOKE ALL FROM PUBLIC`, caller principal from `session_user` via `callers`. It performs the size, kind
  and cap checks in SQL and inserts the row with `phase = QUEUED, validated = false`; the worker runs the full
  validation before any signing and fails the intent with `UNAUTHORIZED` or `INVALID` and no attempt if it does not pass.
- The worker re-runs validation immediately before signing on both paths, because chain state can change after enqueue.
- Coupling proof (13.2): a test-only fixture schema `app_fixture` writes its own row and calls the enqueue function in one
  transaction; commit leaves both, rollback leaves neither, and killing the client process mid-transaction leaves neither.

### 6.4 Migrations

- Embedded `internal/store/migrations/NNNN_name.pgsql`, applied in order, each in its own transaction under
  `pg_advisory_xact_lock(<constant>)`, recorded with sha256; forward-only (DR-11-23). `svn-broadcaster migrate` runs with
  the owner role; `serve` refuses to start unless the schema version equals the binary's.

---------------------------------------------------------------------------------------------------

## 7. Signing boundary (documented in `docs/architecture/broadcaster.md`, PROPOSED)

### 7.1 Key classes and per-key allowlists (Q1 recommended option (a))

| Key | Role read from `Query/Governance` at start-up | May sign (type URL allowlist, checked in policy and again in the signer) |
| --- | --- | --- |
| `service` (devnet) | `ADMITTED_SERVICE` | `/svn.content.v1.MsgRegisterContent`, `/svn.content.v1.MsgWithdrawContent`, `/svn.leases.v1.MsgOfferLease` |
| `broadcaster` (devnet) | `BROADCASTER` | `/svn.leases.v1.MsgAcceptLease`, `/svn.leases.v1.MsgRenewLease`, `/svn.leases.v1.MsgEndLease` with `authority_end = false` |

- Start-up refuses: a key whose address has any other role, no role, or is a council seat; two configured keys with one
  address; a key file that is a symlink, not regular, group- or world-accessible, or not the devnet hex format (the
  auditor loader rules, `agents/auditor/keys/keys.go:1-21`); a chain id outside `shortvideo-dev-` while dev keys are
  configured.
- One message per transaction (DR-11-14): multi-message transactions are atomic, so batching unrelated intents couples
  their failures (`map-chain.md` 6).
- The signer for an intent is fixed when the intent is created and never changes, because an offer replay by another
  scheduler is `FORBIDDEN` (D-11) and content is owned by its `submitting_host` (`chain/x/leases/README.md:105-110`;
  `chain/x/content/README.md:35-37`).

### 7.2 Development signing boundary

- Q1 option (a), recommended: `svn-broadcaster signer` is a separate process that alone reads the key files. It listens
  on a Unix socket (directory 0700, socket 0600) and accepts a connection only from the configured uid
  (`SO_PEERCRED`, `syscall.GetsockoptUcred`). Request: key name, chain id, account number, sequence, gas, timeout height,
  one message as `Any`. The signer decodes the message with the app interface registry, checks the type URL against the
  key's allowlist, checks the message's `signer` equals the key address, checks `authority_end = false` for
  `MsgEndLease`, checks the chain id against its own config, and checks gas and timeout bounds; then it builds and signs
  the outer transaction with `tx.SignWithPrivKey` (`SIGN_MODE_DIRECT`), re-decodes it with the strict decoder, and
  returns bytes and hash. It never signs anything else: no inner-signature domain, no arbitrary bytes.
- Q1 option (b): the same keyring and checks as a package inside `serve`. Fewer processes; an API or SQL bug in the same
  process can reach key bytes.
- Production custody (KMS, HSM, remote signer) is not available here and is not claimed (DEC-001, goal 26).

### 7.3 Provider-signed messages (Q2)

- P1 (relay): before enqueue and again before signing, the broadcaster reads the provider (`Query/Provider`), checks the
  provider is admitted, rebuilds the preimage with `leases.AcceptSignBytes` from the committed open offer or
  `RenewSignBytes`/`EndSignBytes` from the message and the held lease (`Query/ActiveLeases`), and verifies the InnerSignature with `msgkit.VerifyInner` against the provider's current key id
  and public key (`chain/x/leases/README.md:158-162`). A signature by the service key, the broadcaster key, another
  provider's key, a retired key or any unregistered key is refused with `PROVIDER_SIGNATURE_INVALID` and no row. The
  chain verifies again; the pre-check is fail-fast only (`docs/protocol.md:183`). There is no offer-by-id query
  (DR-08-08): `Query/Deadlines` returns full `Offer` messages but pages by deadline
  (`docs/protocol/proto/svn/leases/v1/query.proto`, `QueryDeadlinesResponse`), so the pre-check pages it within a PROPOSED
  page budget; an offer not found within the budget fails the pre-check (`OFFER_NOT_FOUND`), it never skips it.
- P3 (forward, if allowed): the broadcaster strictly decodes the raw bytes with the app decoder, requires exactly one
  message of an allowed relay type, an outer signer that is an `ADMITTED_PROVIDER` whose provider record names that
  account, a valid inner signature as in P1, the configured chain id (checked by verifying the outer signature against
  the account's pubkey and account number), and a `timeout_height` no further than the PROPOSED forward window. Bytes
  without a deadline are refused, because they could commit at any later time (section 8.5). Forwarded bytes are never
  edited; the outbox tracks them by hash and allocates no sequence. When they die, the intent fails with `PROVIDER_MUST_RESIGN`.
- P2 (provider submits its own transaction): goal 16. Goal 11 only reconciles by object state when a provider's own
  submission commits the effect first (8.6).
- Residual: a broadcaster can withhold a signed acceptance until the offer expires; P2 is the provider's remedy.

### 7.4 Hosting intents and the publication permission (Q3)

- Recommended (a): the broadcaster never holds an intent key. A separate devnet fixture signer stands in for the account
  host and signs intents naming the service signer's address as `submittingHost`. For `REGISTER_CONTENT` and
  `WITHDRAW_CONTENT` the broadcaster checks: the authenticated caller may send the kind; the auth context grants
  `PUBLISH_CONTENT` (or `WITHDRAW_CONTENT`) to `principal_id`; the intent public key equals the key registered for that
  principal in `creator_intent_keys`; the intent signature verifies over the canonical payload rebuilt from the message
  and the pinned service address (`chain/x/content/intent.go:42-63`); for a withdraw, the principal is the recorded owner.
- The broadcaster's config schema has no intent-key field, and a static test fails if any broadcaster package calls an
  ed25519 or SDK signing function outside `internal/signer`.
- Limits, stated in the README and evidence: the chain cannot enforce creator permission (`chain/x/content/README.md:61`);
  a compromised `ADMITTED_SERVICE` key can still mint devnet intents with its own key; the truth of the caller's
  permission claim is the API's (goals 13, 19, 21). Community publication approval (`docs/protocol.md:195`, DEC-003) is
  not this bit under option (a).

### 7.5 Never signed, never held

Provider inner signatures, hosting intents, council messages, `MsgExecuteChange`, `authority_end`, audit batches,
provider registry messages, arbitrary bytes. Never held: seat, AUTHORITY, AUDITOR, provider account or provider signing
keys, validator keys, creator or user keys.

### 7.6 Chain id binding and stored authorizations (Q4)

- Every intent row stores the configured chain id; `serve` refuses to start when `/status` reports another network
  (`agents/auditor/chainclient/rpc.go:188-209` pattern) and refuses to relay an intent created under another chain id.
- Inner signatures carry no chain id and RENEW/END never expire on chain (section 1.2). Under Q4 (a): unbroadcast inner
  signatures are sensitive data (never logged, never returned except as a digest); RENEW and END relays carry a local
  expiry (PROPOSED) and are refused after it; ACCEPT relays are refused after the offer's expiry. Local expiry revokes
  nothing on chain; only a renewal (D-12) or a key rotation (DR-08-12) does. The README says so.

### 7.7 Redaction

- `log/slog` JSON handler with a `ReplaceAttr` that drops attributes named or typed as key material, tx bytes, signature
  bytes, DSN, password, certificate, auth context, request body; values are logged only as hashes or ids. API errors
  never echo internal errors. PostgreSQL keeps its default of not logging bound parameters on error.
- A log canary test (13.4 phase L) plants unique strings in a key file, a DSN password, an auth context id, an inner
  signature and a request body, runs the devnet scenarios, and greps the broadcaster, signer, PostgreSQL and node logs:
  zero hits is the pass condition; a mutant that logs the request body must fail it.

---------------------------------------------------------------------------------------------------

## 8. Submission engine

### 8.1 Per-signer serialization and sequence allocation

- One worker loop per configured signer. On start it takes `pg_try_advisory_lock(lock_key)` on a pinned connection
  (`db.Conn`); every allocation and attempt write for that signer runs on that connection. If the connection dies (process
  death, PostgreSQL restart), the lock is gone and the next write fails, so nothing is signed or sent until the lock is
  held again. A second broadcaster process with the same config does not drive that signer.
- Window of one live transaction per signer (DR-11-26): a later sequence becomes invalid when an earlier one dies, and
  there is no gap queue (`map-chain.md` 2.2). Concurrency comes from several signers.
- Next sequence: when the signer has no live attempt, read the committed account (number, sequence) through the node.
  The node's latest height must be at least `signers.observed_height`, else the node is stale and the loop waits. If the
  chain sequence is above `next_sequence` and no attempt of ours explains it (8.5's `tx.acc_seq` lookup names a foreign
  hash), the signer halts with `SIGNER_CONFLICT` and an operator alert (DR-11-15). Otherwise `next_sequence` := chain
  sequence.

### 8.2 Building an attempt

- Business id: drawn from `crypto/rand` when the intent (or a new business attempt) is created: a short prefix plus 128
  bits in lowercase base32, inside the chain pattern; reused for every attempt of that business attempt (DR-11-07).
- Deadline: `timeout_height = latest committed height + K` (K PROPOSED, DR-11-05). No `timeout_timestamp`.
- Gas: a fixed PROPOSED declared gas per kind (2,000,000, the devnet and auditor precedent,
  `chain/devnet/lifecycle_test.go:340`; `agents/auditor/config/config.go:100`); `WithdrawContent` is sized by Simulate
  because many open offers raise its gas (`docs/architecture/leases.md:140`), capped at 20,000,000 (DR-11-27).
- The signer returns bytes and hash; the worker re-decodes and checks the single message equals the stored request with
  signer and business id filled in.

### 8.3 Persist, then broadcast

1. In one transaction on the pinned connection: `SELECT ... FROM signers WHERE address=$1 FOR UPDATE`; insert the
   attempt (bytes, hash, sequence, timeout height, phase `SIGNED`); advance `next_sequence`; set the intent phase; add a
   `transitions` row; commit. If the commit fails, nothing is sent.
2. `broadcast_tx_sync` to the configured node.
3. Record the answer (8.4) in a second transaction. If that write fails, the attempt stays `SIGNED` and reconciliation
   decides it later from the chain. This is the auditor's order (`agents/auditor/chainclient/submit.go:612-634`) moved
   into PostgreSQL.

### 8.4 Classifying answers (goal spec :25-26)

| Observation | Recorded as | Attempt live? |
| --- | --- | --- |
| Transport error, timeout, reset, "broadcast confirmation not received" | send UNKNOWN; phase `UNCERTAIN` | yes |
| JSON-RPC `tx already exists in cache` | send UNKNOWN (the node holds these bytes or committed them OK); phase `UNCERTAIN` | yes |
| JSON-RPC mempool full, tx too large, catching up, app connection error | `NODE_REFUSED` | only if an earlier send was UNKNOWN or accepted |
| CheckTx code != 0 (mempool rejection) | `MEMPOOL_REJECTED` with code and codespace | only if an earlier send of the same bytes was UNKNOWN or accepted |
| CheckTx code 0 | `ACKNOWLEDGED`; status SUBMITTED; never COMMITTED | yes |
| `/tx` found, code 0, typed `svn.events.v1.*` events | `COMMITTED_OK` | resolved |
| `/tx` found, code 0, `<module>_result idempotent=true` only | `COMMITTED_IDEMPOTENT` | resolved |
| `/tx` found, code != 0, ante passed in the block (sequence consumed) | `EXECUTION_FAILED` with code | dead |
| `/tx` found, code sdk 30, 42 or 32 (ante failed in the block; sequence not consumed) | `INCLUDED_ANTE_FAILED` | dead |
| `/tx` not found | nothing yet | decided by 8.5 |

`CommittedTx` is extended to check that SHA-256 of the returned bytes equals the requested hash (section 1.3).

### 8.5 When an attempt can no longer commit (the dead rule)

Attempt A = (signer S, sequence N, timeout T, hash H). A is DEAD when one of:

- (d1) `/tx(H)` is found with a non-zero code (committed failure).
- (d2) every send of A got a definitive refusal (`MEMPOOL_REJECTED` or `NODE_REFUSED`) and no send of A was ever UNKNOWN
  or ACKNOWLEDGED. A refused transaction is not added to the node's mempool, so it is not gossiped.
- (d3) the node's committed height is at least T + L (L PROPOSED, for asynchronous indexing,
  `chain/devnet/lease_test.go:406-408`), `/tx(H)` is not found, and either the committed account sequence of S is at most
  N (no later block can pass A's deadline check; derived from source, section 3.4 of `map-chain.md`), or
  `tx_search "tx.acc_seq='<S>/N'"` returns a hash other than H (sequence N was used by another transaction). If the
  account sequence is above N and the search finds nothing, A stays UNCERTAIN and an alert is raised.

A is never declared dead from the local clock, from the intent's expiry, or while the chain height does not advance.

### 8.6 Reconciliation before any resend (goal spec :25)

Runs at start-up for every non-final intent, after every UNCERTAIN, periodically, and always immediately before signing a
new attempt:

1. Re-decode every stored attempt's bytes with the app decoder; a decode failure is a fatal integrity error, reported, not
   repaired.
2. For each live attempt: `/tx(H)`. Found: classify (8.4). Not found: apply 8.5. If still live, rebroadcast the same
   stored bytes (never re-signed bytes) at most once per backoff step; same bytes cannot double-apply.
3. If no attempt is live: check the effect on chain by object state and typed event search, before any new attempt:
   content (`Query/Content`), held lease (`Query/ActiveLeases`), open offer (`Query/Deadlines`), and the typed event of
   the kind (`ContentRegistered.content_id`, `ContentWithdrawn.content_id`, `LeaseOffered.offer_id`,
   `LeaseAccepted.lease_id`, `LeaseRenewed.lease_id` with term version, `LeaseEnded.lease_id`) through `/tx_search`.
   If the effect exists, decode the committing transaction from `/tx` and compare its business fields with ours
   (`chain/x/leases/README.md:134-140` field sets; content fields `chain/x/content/msg_server.go:100-106,199-204`).
   Equal: COMMITTED with `own_transaction = false` and that transaction as canonical evidence. Different: FAILED
   (`OBJECT_TAKEN`).
4. Only if no effect exists and the intent may still start an attempt (8.7) is a new attempt signed: same business id,
   sequence from 8.1.
5. A resend's committed `REPLAY`: our business id was taken by another payload. If step 3 finds no effect, open a new
   business attempt (new random id), bounded; else as step 3. A resend's `DUPLICATE_ID`, `FORBIDDEN`, `UNAUTHORIZED`,
   `BOUNDS` or role failure is reconciled by step 3 and never mapped onto an earlier attempt
   (`chain/x/leases/README.md:607-618`).

No step edits history by hand; every decision appends a `transitions` row with the observation that justified it.

### 8.7 Retries, backoff, bounds (all PROPOSED, DR-11-10)

- Retryable: sdk 32 (resync the sequence), sdk 4 within the DR-10-41 window (at most a PROPOSED number of tries, at least
  two blocks apart, DR-11-18), sdk 30 or 42 (expired deadline), node refusals, transport errors.
- Not retryable (FAILED): svn `UNAUTHORIZED`, `FORBIDDEN`, `BOUNDS`, `FEE_REJECTED` at CheckTx (configuration or request
  error; an `UNAUTHORIZED` signer also halts the signer), sdk 2 decode, and every committed execution failure except as
  8.6 step 5 says.
- Backoff with full jitter, base 1 s, cap 60 s; `next_attempt_at` is stored, so restarts keep the schedule.
- At most 8 attempts per business id, 4 business attempts, 16 attempts per intent (auditor precedent
  `agents/auditor/chainclient/submit.go:78-80`).
- A new attempt starts only before `expires_at` (database clock); a live attempt is never abandoned because of expiry.

---------------------------------------------------------------------------------------------------

## 9. State machine (exact)

### 9.1 Intent states and the caller mapping

| Internal state / phase | Meaning | `OperationStatus` shown |
| --- | --- | --- |
| `REQUESTED/QUEUED` | rows committed; nothing signed | REQUESTED |
| `REQUESTED/RETRY_WAIT` | every earlier attempt is dead; next attempt at `next_attempt_at` | REQUESTED (detail says attempts so far) |
| `IN_FLIGHT/SIGNED` | one attempt persisted; no answer recorded | SUBMITTED (phase SIGNED) |
| `IN_FLIGHT/ACKNOWLEDGED` | CheckTx code 0 on the node | SUBMITTED |
| `IN_FLIGHT/UNCERTAIN` | an unknown send; may commit | SUBMITTED (phase UNCERTAIN) |
| `IN_FLIGHT/AWAITING_DEADLINE` | expiry passed; a live attempt still may commit | SUBMITTED (reason EXPIRED_AWAITING_CHAIN) |
| `COMMITTED` (terminal) | 9.4 | COMMITTED |
| `FAILED` (terminal) | reasons: `EXECUTION_FAILED{code}`, `REJECTED{code}`, `EXPIRED_NOT_SENT`, `EXPIRED_DEAD`, `ATTEMPTS_EXHAUSTED`, `OBJECT_TAKEN`, `BUSINESS_ID_TAKEN`, `UNAUTHORIZED`, `INVALID`, `PROVIDER_SIGNATURE_INVALID`, `PROVIDER_MUST_RESIGN`, `CANCELLED` | FAILED |
| any non-terminal while the chain is stalled | overlay computed at read time (section 10) | PENDING_STALL (detail keeps the phase) |

### 9.2 Attempt phases

`SIGNED -> {ACKNOWLEDGED, UNCERTAIN, REFUSED}`; `ACKNOWLEDGED -> UNCERTAIN` never (an acknowledgement stays known);
`{SIGNED, ACKNOWLEDGED, UNCERTAIN} -> {COMMITTED_OK, COMMITTED_IDEMPOTENT, EXECUTION_FAILED, INCLUDED_ANTE_FAILED, DEAD}`;
`REFUSED` is dead only under (d2). Terminal phases never change.

### 9.3 Transition table (every transition writes one `transitions` row)

| # | From | Event (observation) | Guard | To |
| --- | --- | --- | --- | --- |
| T1 | none | create (HTTP or SQL) | validation passed (HTTP) or size/kind/cap passed (SQL) | REQUESTED/QUEUED |
| T2 | REQUESTED | worker validation fails | | FAILED(UNAUTHORIZED, INVALID, PROVIDER_SIGNATURE_INVALID) |
| T3 | REQUESTED | cancel | no attempt ever signed | FAILED(CANCELLED) |
| T4 | REQUESTED | expiry passed (DB clock) | no live attempt | FAILED(EXPIRED_NOT_SENT if never sent, else EXPIRED_DEAD) |
| T5 | REQUESTED | reconcile finds equal effect on chain | 8.6 step 3 | COMMITTED (own_transaction false) |
| T6 | REQUESTED | reconcile finds different effect | | FAILED(OBJECT_TAKEN) |
| T7 | REQUESTED | attempt persisted | signer lock held, not stalled, attempts below caps, before expiry | IN_FLIGHT/SIGNED |
| T8 | IN_FLIGHT | CheckTx code 0 | | IN_FLIGHT/ACKNOWLEDGED |
| T9 | IN_FLIGHT | unknown send | | IN_FLIGHT/UNCERTAIN |
| T10 | IN_FLIGHT | `/tx(H)` code 0 with typed event | event matches the kind and object | COMMITTED (canonical = H) |
| T11 | IN_FLIGHT | `/tx(H)` code 0 idempotent | first commitment found and equal (9.4) | COMMITTED (canonical = first commitment) |
| T12 | IN_FLIGHT | `/tx(H)` permanent code | not REPLAY/DUPLICATE_ID/FORBIDDEN/UNAUTHORIZED/role | FAILED(EXECUTION_FAILED{code}) |
| T13 | IN_FLIGHT | `/tx(H)` REPLAY, DUPLICATE_ID, FORBIDDEN, UNAUTHORIZED, role failure | run 8.6 step 3 | COMMITTED (other), FAILED(OBJECT_TAKEN), or REQUESTED/RETRY_WAIT with a new business attempt (REPLAY, no effect, below cap), else FAILED(BUSINESS_ID_TAKEN) |
| T14 | IN_FLIGHT | attempt dead (d2, d3, or INCLUDED_ANTE_FAILED), retryable | below caps and before expiry | REQUESTED/RETRY_WAIT |
| T15 | IN_FLIGHT | attempt dead, retryable | caps reached or after expiry | FAILED(ATTEMPTS_EXHAUSTED or EXPIRED_DEAD) |
| T16 | IN_FLIGHT | attempt refused, not retryable | (d2) holds | FAILED(REJECTED{code}) |
| T17 | IN_FLIGHT | expiry passed | attempt still live | IN_FLIGHT/AWAITING_DEADLINE (no other change) |
| T18 | COMMITTED, FAILED | anything | | unchanged (absorbing) |

There is no transition into COMMITTED except T5, T10, T11 and T13, each guarded by a committed result read from the
chain. A property test and a mutant prove it (13.5, 13.7).

### 9.4 What moves a row to COMMITTED

- T10: `/tx(H)` for one of this intent's attempts shows code 0 and the typed event of the kind with this intent's object id
  (`ParseCommitted`, `chain/msgkit/events.go:186-206`), and the returned bytes hash to H.
- T11 and T13/T5: code 0 with `<module>_result idempotent=true` (or an effect found by object state) is accepted only after
  the first commitment of the same effect is found by typed event search and its business fields equal ours; that first
  commitment becomes the canonical evidence. An idempotent replay is never itself the canonical commitment.
- Authority messages are not routed, so the DR-06-05 case (code 0 with `authority_result{code}`,
  `chain/x/authority/msg.go:129-132`) cannot reach this rule; if it ever does, it is FAILED.

### 9.5 Canonical commitment evidence for the indexer (goal 12 handoff)

For every COMMITTED intent the `commitments` row holds: `chain_id`; `height`; `block_hash` (from `/commit` at that height,
`CMT/rpc/client/http/http.go:475`); `tx_hash` (SHA-256 of the bytes, `CMT/types/tx.go:29-35`); `tx_index` (from `/tx`);
`msg_index` (0, one message per transaction); `code`, `codespace`, `gas_wanted`, `gas_used`; the typed events decoded with
`ParseCommitted`, each with its position in the transaction result; `intent_id`, `business_id`; `idempotent_replay` and
`first_commit_tx_hash`. Goal 12 needs "chain identity, block height and stable transaction/message/event identity"
(`docs/goals/12-committed-state-indexer-and-reconciliation.md:19-21`). The evidence is relative to one configured node;
the README discloses that gateway trust (`docs/blueprint.md:391`).

---------------------------------------------------------------------------------------------------

## 10. Chain outage behaviour (goal spec :28-29, :38-39)

- Stall detection: the worker records `/status` latest height and block time in `chain_health`. Stalled = the height has
  not advanced for T seconds of database clock (T PROPOSED), or the node is unreachable or catching up. The overlay
  PENDING_STALL is computed at read time for every non-terminal intent (DR-11-11).
- While stalled: new intents are still accepted within the caps, with a required, bounded `expires_at`; no new attempt is
  started (the reference outbox parks requests, `cases:64-77`); live attempts stay live and are polled.
- An intent that expires with nothing sent becomes FAILED(EXPIRED_NOT_SENT): an honest failure. An intent with a live
  attempt stays SUBMITTED/AWAITING_DEADLINE with the PENDING_STALL overlay until the chain moves past its
  `timeout_height`; then it becomes COMMITTED (if included) or FAILED(EXPIRED_DEAD). It never becomes COMMITTED during
  the stall unless a committed block containing it is read.
- The broadcaster has no lease, offer or content state of its own beyond ownership rows; it cannot create a final lease in
  the database. COMMITTED for `OFFER_LEASE` means an offer committed, not a lease.

---------------------------------------------------------------------------------------------------

## 11. Configuration (`internal/config`; typed JSON file plus flags; no defaults for chain id or gateway)

| Key | Rule |
| --- | --- |
| `chain_id` | required; must equal `/status` network at start and before each signing round; dev keys refuse a chain id without `shortvideo-dev-` |
| `rpc_url` | required; `http://127.0.0.1:...` or `http://[::1]:...` by default; a non-loopback URL needs `https` and `allow_remote_rpc: true`; one node, disclosed as trusted for reads |
| `database` | unix socket directory or 127.0.0.1, database, role; password only from a passfile (mode 0600); `sslmode` must be set explicitly (`PQ/connector.go:304-306`); PG* environment overrides are refused at start `[Educated Guess: lib/pq reads them, verify which]` |
| `listen` | loopback address; TLS server cert, key and client CA paths |
| `signer` | socket path and expected uid (Q1 (a)); or key names and paths (Q1 (b)) |
| `keys` (signer only) | name -> file, class; no intent key field exists |
| `callers` | principal -> kinds, caps, bindings |
| PROPOSED numbers | K, L, T, attempt caps, backoff, intent lifetime bounds, open-intent caps, RENEW/END local expiry, forward window, gas per kind, RPC timeout, poll interval; each with hard lower and upper bounds in code; labelled PROPOSED in the README |

---------------------------------------------------------------------------------------------------

## 12. Repository integration points (outside `services/broadcaster`; INT owns them unless noted)

| File | Change | Note |
| --- | --- | --- |
| `go.work:5-11` | add `./services/broadcaster` | |
| `scripts/run_tests.py:151` | add `services/broadcaster` to `GO_MODULES` | |
| `services/broadcaster/svn_broadcaster/*` | delete the Python scaffold | its `serve()` would say "does not submit transactions" next to a service that does |
| `tests/unit/test_scaffold.py:7,24-27` | remove the broadcaster scaffold test | replaced by design, recorded with its replacement tests (`tests-replaced.txt`) |
| `tests/unit/test_repo_policy.py:17` | require `services/broadcaster/go.mod` and `services/broadcaster/cmd/svn-broadcaster/main.go` | |
| `pyproject.toml:31,47` | drop `services/broadcaster` from `pythonpath` and `mypy_path` | |
| `README.md:14` | describe the service | |
| `.github/workflows/ci.yml` | `go vet` and build of the module in the `go` job; new go-race shards `broadcaster` (module tests with PostgreSQL) and `devnet-broadcaster` (`-run TestDevnetBroadcaster`); add `TestDevnetBroadcaster` to the `devnet` shard's `-skip` union; a pinned PGDG install step in those two shards (DR-11-25) | |
| `tests/unit/test_ci_devnet_coverage.py` | no code change if the new shard follows its rule (`:1-20`) | verify |
| `tests/unit/test_ci_postgres_coverage.py` (new) | fails when the `go` job sets `SVN_SKIP_POSTGRES=1` while no shard runs the module's PostgreSQL tests | mirror of the devnet guard |
| `tests/unit/test_chain_writer_policy.py` (new) | REQ-INT-001 static policy (13.6) | |
| `docs/dependencies.md` | lib/pq v1.12.3 (MIT) as a direct dependency of `services/broadcaster`, already in svnd | |
| `docs/sbom.cdx.json` | regenerate (`scripts/write_sbom.py:68` reads every `go.sum`) | |
| `docs/architecture/README.md` | index `broadcaster.md` | B5 |
| `chain/x/leases/README.md`, `docs/architecture/leases.md` | none (their goal 11 handoff text stays true) | report only |
| `docs/progress.md` row 09 wording "outbox submission (Goal 11)" | evidence phase, per the DR-11-06 answer | orchestrator |

Disk: one more binary and its build cache entries. Delete temp binaries and PostgreSQL clusters after each run.

---------------------------------------------------------------------------------------------------

## 13. Test plan

Mocks and fakes appear only in 13.1 and are never cited as end-to-end evidence. 13.2 to 13.4 use real PostgreSQL 18.6, a
real chain, real processes.

### 13.1 Unit (services/broadcaster; fake RPC and fake signer allowed)

- outbox: `TestNoCommittedWithoutCommittedResult`, `TestAckNeverCommits`, `TestDeadRuleD1D2D3` (each condition and each
  near miss: refusal after an unknown send stays live; height T+L-1 stays live; account sequence above N with no search hit
  stays live), `TestExpiryNeverAbandonsLiveAttempt`, `TestReconcileBeforeResend` (no new attempt without an effect check),
  `TestResendCodeNeverMapsToFirstAttempt`, `TestIdempotentReplayNeedsFirstCommit`, `TestReplayOpensBoundedBusinessAttempt`,
  `TestCancelOnlyBeforeSigning`, `TestTerminalStatesAbsorb`, `TestStallOverlayNeverCommits`.
- chain: `TestBuildTxMatchesChainDecoder` (strict decode with `app.MakeEncoding`, one message, signer, gas,
  timeout_height, empty memo and fee), `TestBuildTxRefusesForeignSigner`, `TestClassifyBroadcastAnswers` (table of 8.4,
  including the exact JSON-RPC texts taken from `CMT/mempool/errors.go` and `CMT/rpc/core/mempool.go`),
  `TestCommittedTxChecksHash`, `TestTxSearchQueries` (acc_seq and typed event query strings, length below 512),
  `TestEffectComparison` (each business field changes the result; signer and non-business fields do not).
- policy and signer: `TestKindSignerAllowlist`, `TestSignerRefusesOtherTypeURLs` (every registered message type URL except
  the allowed ones), `TestSignerRefusesAuthorityEnd`, `TestSignerRefusesForeignChainID`, `TestProviderPrecheck`
  (service key, broadcaster key, other provider's key, retired key, random key, wrong domain, wrong lease id: each
  refused), `TestForwardedTxChecks` (multi-message, wrong type, no deadline, deadline too far, wrong chain, foreign
  signer), `TestPublicationPermission` (missing permission, unregistered intent key, another principal's content,
  `submittingHost` not the pinned signer), `TestStartupRefusesRole` (AUTHORITY, AUDITOR, provider, seat, no role,
  duplicate address), `TestKeyFileRules`, `TestSignerPeerUID`.
- api: `TestStrictDecode` (unknown field, oversize, bad id), `TestClientCertRequired`, `TestCrossCallerNotFound`,
  `TestIdempotencyConflict`, `TestAuthContextRefusesPersonalData`, `TestDevPrincipalsRefuseNonDevChain`.
- logx: `TestRedactorDropsSecrets`.

### 13.2 PostgreSQL integration (real disposable cluster via `internal/pgtest`)

`internal/pgdev`: `os.MkdirTemp("", "svn-pg-")` under `/tmp` (short path, under the 107-byte Unix socket limit); when
`euid == 0`, `chown` to `postgres` and run `initdb`, `pg_ctl` and `postgres` with `SysProcAttr.Credential` (uid 102, gid
104 here, looked up by name); otherwise run as self (CI runner). `initdb --auth-local=scram-sha-256
--auth-host=scram-sha-256 --encoding=UTF8 --locale=C.UTF-8` (the `scripts/env/start.sh` flags), Unix socket only
(`listen_addresses=''`), `fsync=on`. Hooks: `pg_ctl -m immediate stop`, `-m fast stop`, `kill -9` of the postmaster,
restart on the same data directory; cleanup removes the directory. It refuses to start if an ancestor directory is not
traversable by the server user. Missing binaries fail the test unless `SVN_SKIP_POSTGRES=1`, which the CI guard allows
only while a shard runs these tests. The version (`postgres --version`) is logged.

Tests: `TestEnqueueAtomicWithApplicationRow`, `TestEnqueueRollbackLeavesNothing`, `TestClientDeathMidTransaction`,
`TestIdempotencyUniqueUnderConcurrency` (50 goroutines, one key), `TestOneLiveAttemptPerSequence` (unique index rejects
a second live row), `TestAdvisoryLockFencesSecondWorker`, `TestLockLossStopsSending` (terminate the backend; the next
allocation fails; nothing is sent), `TestMigrationsChecksumGapForwardOnly`, `TestRuntimeRoleHasNoDDL`,
`TestEnqueueFunctionSearchPathAndGrants`, `TestCallerSeesOnlyOwnRows`, `TestSQLMetacharactersAreData`,
`TestImmediateStopKeepsCommittedAttempts`, `TestTransitionsAppendOnly`.

### 13.3 Chain-level semantics (chain/app, deterministic, tests only; B4)

- `TestTimeoutHeightThroughFinalizeBlock`: CheckTx at committed height T admits a transaction with `timeout_height = T`;
  FinalizeBlock at T+1 returns sdk 30; the signer's account sequence and every store are unchanged
  (`FinalizeFullSnapshotForTest`-style snapshot); FinalizeBlock at T commits it.
- `TestAccSeqEventOnHandlerFailure`: a handler failure (for example `CAPACITY`) still emits `tx.acc_seq`; an ante timeout
  failure does not (SetPubKey runs after the timeout decorator, `chain/app/ante.go:32,38`).
- `TestStrictDecoderAcceptsTimeoutHeight`.

### 13.4 Devnet acceptance (`chain/devnet/broadcaster_test.go`; `$NETNS`; label "single machine, four processes, private network namespace")

Harness: `startDevnet` (`chain/devnet/lease_test.go:442`), the council helper and the lease setup builders
(`map-chain.md` 8.2), `reservedAt` (`lease_test.go:353-360`), `stopForStall`/`observeStall`
(`chain/devnet/prove_test.go:263,312`); the broadcaster binary built into a temp dir; PostgreSQL through
`svn-broadcaster dev-db`; a dev CA and two caller certificates generated in the test; the devnet fixture account-host
signer and the provider inner keys generated in the test (as `registry_test.go:240-245` does). Fault proxy: an
`httputil.ReverseProxy` on loopback between the broadcaster and validator 0's RPC; it reads the bounded JSON-RPC body,
applies a per-method rule (pass, drop request, forward then drop response, hold, delay, reset), and counts forwarded
`broadcast_tx_*` calls. Test reads go directly to the nodes, never through the proxy.

`TestDevnetBroadcasterLifecycle` phases (one devnet, sequential; each phase logs `phase=` lines with heights and hashes):

| Phase | Injection | Assertions (all read from the chain unless stated) |
| --- | --- | --- |
| S setup | provider-1 and provider-2 register and are admitted by the council; account-host fixture keys registered in `creator_intent_keys` | providers ADMITTED |
| H happy path | one intent per kind through the API | each COMMITTED with T10 evidence equal to `/tx`; `own_transaction` true |
| A ack versus commit | an acceptance that passes CheckTx and fails `CAPACITY` (as `lease_test.go:507-530`) | API shows SUBMITTED between ack and block, then FAILED(EXECUTION_FAILED CAPACITY); `reservedAt` unchanged |
| TB1 timeout before submission, same bytes | proxy drops the first `broadcast_tx_sync` | UNCERTAIN; `/tx` checked before the resend; the resend is the same bytes (hash equal); one commit; forward count 1 |
| TB2 timeout before submission, deadline | proxy drops every broadcast until height > T+L | attempt DEAD by (d3); new attempt with the SAME sequence and business id; one effect; `reservedAt` equals one package |
| TA timeout after submission | proxy forwards and drops the response | `/tx` finds the commit; no further `broadcast_tx_*` forwarded; COMMITTED with that hash |
| WD worker death after commit | proxy holds the broadcast response; test sees the commit on a node; SIGKILL the broadcaster; restart with pass-through | COMMITTED from the persisted hash; zero new broadcasts; signer `next_sequence` consistent with the chain |
| WP death between persist and send | proxy holds the request; SIGKILL; (a) proxy forwards later, (b) proxy discards | (a) reconcile finds the commit; (b) (d3) then one resend; both one effect |
| DU duplicate requests | 20 concurrent creates with one key; same key other body; two keys with the same relay payload | one intent id; 409; both relay intents COMMITTED, the second with `own_transaction=false` and the first's tx as canonical; one `LeaseAccepted` across all blocks (block results scan, `msg_index` aware) |
| CS concurrent same signer | 20 concurrent `REGISTER_CONTENT` for `service`; a second `serve` process on the same database | attempts' sequences unique and dense; all COMMITTED; the chain sequence advanced by exactly the number of included transactions; the second process drove nothing for that signer |
| UA unauthorized | caller without the kind; another caller's intent read; withdraw of another principal's content; signer asked (directly, test-only client) to sign `MsgOfferLease` with the BROADCASTER key; start-up with the authority, auditor-1, provider-1 and seat-1 keys | 403/404 with no row; signer refusal; start-up refusals; forced chain check: the test signs that `MsgOfferLease` with the broadcaster key itself and broadcasts it: `FORBIDDEN`, state unchanged |
| PI provider impersonation | relay acceptance signed by the service key, the broadcaster key, provider-2's key, a retired provider-1 key, a random key; under P3 a forwarded transaction with a forged inner signature | pre-check refusal, no attempt; forced chain check by the test: `UNAUTHORIZED`, `reservedAt` and held leases unchanged |
| PB publication permission | register without `PUBLISH_CONTENT`; with an intent key not registered for the principal; withdraw by another principal | refused, no row, no transaction |
| PG PostgreSQL restart | `pg_ctl -m immediate stop` with intents in QUEUED, SIGNED and ACKNOWLEDGED; restart | the broadcaster sends nothing while it cannot persist; after restart every intent reaches the chain's truth; no manual SQL |
| BR broadcaster restart | SIGTERM and SIGKILL at random points of a 50-intent batch across both signers | every intent final or honestly pending; one effect per business id |
| RS DR-10-41 | restart validator 0 and broadcast before it commits a block | sdk 4 treated as retryable; COMMITTED after the node commits |
| L log canary | 7.7 canaries | zero hits in broadcaster, signer, PostgreSQL and node logs |
| Z final accounting | none | for each provider `reserved_bytes` (`Query/Provider`) equals the sum of `package_bytes` of its held leases (`Query/ActiveLeases`); every COMMITTED intent's canonical hash is on chain with code 0 and its typed event; no FAILED intent's object carries its effect; the account sequence of each signer equals the count of its attempts that consumed a sequence |

`TestDevnetBroadcasterQuorumStall` (separate devnet): stop validators 3 and 4 with `stopForStall`; submit intents (one
queued, one in flight acknowledged just before the stop); sample the API at least six times over the stall: never
COMMITTED, PENDING_STALL overlay present; an intent with a short expiry and nothing sent becomes FAILED(EXPIRED_NOT_SENT)
during the stall; restart the validators: the in-flight intent becomes COMMITTED or FAILED(EXPIRED_DEAD) from chain data
only; the queued one commits; the one-block-in-flight allowance of `observeStall` is respected and logged.

`TestDevnetBroadcasterForwardedProviderTx` (only if Q2 includes P3; may be a phase of the lifecycle test to save a
devnet start): a provider-1 account-signed acceptance forwarded verbatim commits by its own hash; one without
`timeout_height` is refused.

`tx.acc_seq` search and typed event search are exercised against the real node in phases TB2, DU and Z; their results
are logged, which turns the "derived from source" items of 1.1 into tested ones.

### 13.5 Fuzz and property tests (real `-fuzz` runs logged)

- `TestStateMachineProperty` and `FuzzTransitions` (internal/outbox): random bounded sequences of observations (acks,
  unknown sends, refusals, `/tx` results, heights, account sequences, search results, restarts, expiry, stall) against
  the transition table; invariants: COMMITTED only via T5/T10/T11/T13 with a committed observation; at most one live
  attempt per signer; terminal states absorb; no new attempt while one is live; sequences dense. A reachability check
  shows each transition is hit.
- `FuzzDecodeIntentRequest` (api), `FuzzForwardedTx` (policy: never panics, refuses anything but one allowed message),
  `FuzzClassifyRPCError` (chain).

### 13.6 Static and binary checks

- `tests/unit/test_chain_writer_policy.py` (REQ-INT-001): fails if `services/api`, `services/indexer`, `workers/`,
  `apps/web` or `packages/` contain a signing-key loader, `broadcast_tx`, `BroadcastTx`, `SignWithPrivKey`, a
  `cosmos.tx.v1beta1` builder, or (in `apps/web`) a CometBFT websocket or `subscribe` use; Go chain writers are allowed only
  in `services/broadcaster`, `agents/auditor` (its own AUDITOR path, Q1 of goal 09) and test code. It carries planted
  violation fixtures and must fail on each (as `test_ci_devnet_coverage.py` does with mutated documents).
- Go: `TestNoSigningOutsideSigner` (go/ast scan of the module: no `ed25519.Sign`, `PrivKey.Sign`, `SignWithPrivKey` outside
  `internal/signer`), `TestConfigHasNoIntentKey`, `TestNoLeaseOrProjectionTable` (schema has no lease, offer or content
  state table besides ownership).

### 13.7 Mutation runner (`docs/evidence/goal-11/mutants.py`, B10; same design as goals 08-10)

Groups, each mutant killed by a named test: commit guard (T10 without the typed-event check; T11 without the first-commit
lookup; ack sets COMMITTED); dead rule (drop the L margin; drop the unknown-send condition of d2; use local clock);
reconcile (skip the effect check before a resend; map a resend's code to the first attempt); persist order (broadcast
before the commit of the attempt row); fencing (ignore advisory lock loss); sequence (skip the stale-node guard; skip the
conflict halt); allowlist (remove one type URL check in the signer; allow `authority_end`); provider pre-check (verify
against the message key instead of the registered key); publication (skip the registered-key check; skip the owner
check); idempotency (drop the unique key; compare digests wrongly); stall (allow T7 while stalled); redaction (log the
request body); config (skip the chain id check). Devnet-only mutants run inside `$NETNS`.

### 13.8 Regression on the same revision

Goal 08, 09, 10 runners with `--check` and full runs; the goal 06 and 07 anchor checks (known BLOCKED); every existing Go,
Python and Node suite; both contract checkers; the secret scan; the SBOM and workflow policy checks.

---------------------------------------------------------------------------------------------------

## 14. Acceptance and requirements-to-tests matrix (forecasts under the recommended answers; not results)

| Item (goal spec) | Tests | Mutant group | Expected status |
| --- | --- | --- | --- |
| Timeouts before and after submission (:35) | TB1, TB2, TA; `TestDeadRuleD1D2D3`; `TestTimeoutHeightThroughFinalizeBlock` | dead rule, reconcile | VERIFIED (devnet) |
| Worker death after commit (:35) | WD, WP | persist order, reconcile | VERIFIED (devnet) |
| Duplicate requests (:35) | DU; `TestIdempotencyUniqueUnderConcurrency` | idempotency | VERIFIED |
| Concurrent same-signer requests (:36) | CS; `TestAdvisoryLockFencesSecondWorker`, `TestOneLiveAttemptPerSequence` | fencing, sequence | VERIFIED |
| One business effect, no leaked capacity, eventual accurate status (:36-37) | Z after every phase; DU block scan | commit guard | VERIFIED (devnet) |
| Reject unauthorized messages (:38) | UA; 13.1 allowlist tests | allowlist | VERIFIED for the Q1 allowlist; without a Q1 answer IMPLEMENTED (interim allowlist) |
| Reject provider impersonation (:38) | PI; `TestProviderPrecheck` | provider pre-check | VERIFIED (both the broadcaster refusal and the forced chain refusal) |
| Stop quorum: pending or honest failure (:38-39) | `TestDevnetBroadcasterQuorumStall` | stall, commit guard | VERIFIED (devnet) |
| Restart PostgreSQL and broadcaster, reconcile from persisted state (:40) | PG, BR, WD; `TestImmediateStopKeepsCommittedAttempts` | fencing, persist order | VERIFIED (devnet, real kills) |
| Req. 1 typed requests, outbox fields, one-transaction coupling (:18-20) | 6.2 schema tests; `TestEnqueueAtomicWithApplicationRow` | idempotency | VERIFIED for the fixture application schema; the API's own use DEFERRED (goal 13) |
| Req. 2 approved service keys only; provider signs; forward path (:21-23) | 7.1-7.3 tests; UA; PI; forwarded test | allowlist, pre-check | VERIFIED under Q1/Q2 answers; "approved" BLOCKED until the boundary document is approved (DR-11-01) |
| Req. 3 sequence, retries, backoff, deadlines, uncertainty, classification (:24-26) | 13.1 chain and outbox tests; TB/TA/RS | dead rule, sequence | VERIFIED |
| Req. 4 states, authentication, object permissions, outage queue, no final leases (:27-29) | 5.8 view tests; UA; stall test; `TestNoLeaseOrProjectionTable` | stall, ownership | VERIFIED; "uncertain" shown as SUBMITTED/UNCERTAIN (DR-11-09) |
| Req. 5 signing boundary, redaction, explicit chain id and gateway, no live credential (:30-32) | L; `TestSignerRefusesForeignChainID`; config tests; netns run | redaction, config | VERIFIED for the development boundary; "approved" BLOCKED (DR-11-01) |
| Handoff: traces, schema, API, boundary doc (:43) | `transitions` dumps in logs; README; `docs/architecture/broadcaster.md` | | IMPLEMENTED (doc); boundary approval BLOCKED until asked at PR time |
| Handoff: stable identities, canonical commitment evidence (:43-44) | H, DU, Z compare `commitments` with `/tx` and `/commit` | commit guard | VERIFIED as recorded; goal 12 consumption DEFERRED |
| Head CI on the tested commit | ci.yml shards | | BLOCKED until the owner authorizes a draft PR |

| Requirement | Assigned TEST (short) | Goal 11 tests | Expected |
| --- | --- | --- | --- |
| REQ-CHAIN-013 (`docs/requirements.md:452`) | broadcaster fixture cannot mint a provider signature or skip the publication permission bit | PI, PB, UA, `TestNoSigningOutsideSigner`, `TestConfigHasNoIntentKey` | provider half VERIFIED (devnet); publication half VERIFIED for the broadcaster check under Q3 (a), with the caller's claim DEFERRED to goals 13/19/21 and chain-level creator binding BLOCKED (DR-07-01, DEC-001); IMPLEMENTED only if Q3 is unanswered |
| REQ-ACK-001 (`:483`) | ack stays submitted until inclusion and execution; reconcile a timeout before retry | A, TB1, TB2, TA, stall test, `TestAckNeverCommits` | VERIFIED (devnet) |
| REQ-INT-001 (`:496`) | goal 11 is the only chain writer used by the API; web client does not subscribe to every block | `test_chain_writer_policy.py` with planted fixtures | static policy VERIFIED; runtime "used by the API" DEFERRED (goals 13, 21) |
| REQ-IDEM-001 (`:500`) | retry one business id after a timeout, single committed effect | TB2, WD, DU, Z | VERIFIED (devnet, real PostgreSQL) |
| REQ-QA-008 (`:594`) | repeat one operation across timeout and restart, one transition, no leaked reservation | WD, WP, PG, BR, Z | VERIFIED (devnet, real kills) |
| Regression rows (goal 08 REQ-CHAIN-012, goal 06 linked-binary tests) | must stay green | 13.8 | regression only |

---------------------------------------------------------------------------------------------------

## 15. Evidence capture list (Wave 3, after the orchestrator commits; `scripts/evidence/capture.sh <log> -- <cmd>`; logs under `docs/evidence/logs/goal-11/`; run from `$WT`)

Filtered captures keep go test's exit status: `bash -c '<cmd> 2>&1 | grep -a -v -F "<nil> DBG "; exit "${PIPESTATUS[0]}"'`
(written `FILTER(<cmd>)`), as goals 08-10 did.

```
runtime.txt / revision.txt / diff-stat.txt   go version, postgres --version (18.6), HEAD, tree, base 6d74aca
go-mod-tidy-broadcaster.txt   go -C services/broadcaster mod tidy -diff (no change; no new third-party module)
go-sum-subset.txt             every services/broadcaster/go.sum line is in chain/go.sum
unit-broadcaster.txt          SVN_SKIP_POSTGRES= go test -count=1 -v ./services/broadcaster/...   (unit + PostgreSQL integration)
race-broadcaster.txt          go test -race -count=1 ./services/broadcaster/...
app-deadline.txt              FILTER(go test -count=1 -v ./chain/app -run 'TestTimeoutHeight|TestAccSeq|TestStrictDecoderAcceptsTimeoutHeight')
devnet-broadcaster.txt        $NETNS go test -count=1 -timeout 1800s -v -run TestDevnetBroadcaster ./chain/devnet
devnet.txt                    $NETNS go test -count=1 -timeout 1800s -v -skip 'TestDevnetBroadcaster|TestDevnetAudit|TestDevnetUpgrade|TestDevnetMinority|TestDevnetGovernedChange' ./chain/devnet   (regression)
fuzz-<Name>.txt               go test -run='^$' -fuzz=^Name$ -fuzztime=60s -count=1 <pkg>  for each 13.5 target
traces.txt                    derived from devnet-broadcaster.txt: the transitions rows printed per phase
chain-writer-policy.txt       .venv/bin/pytest -v tests/unit/test_chain_writer_policy.py tests/unit/test_ci_postgres_coverage.py tests/unit/test_ci_devnet_coverage.py
tests-go.txt / tests-python.txt / tests-node.txt    python3 scripts/run_tests.py --only {go,python,node}
ci-shards.txt                 the go-race shard commands of ci.yml, including broadcaster and devnet-broadcaster
gofmt-vet.txt                 bash scripts/gofmt_check.sh && go vet ./chain/... ./agents/... ./proto/... ./services/broadcaster/...
check-protocol.txt / check-requirements.txt / scan-secrets.txt / sbom-check.txt / workflow-policy.txt
protocol-diff.txt             git diff --stat 6d74aca..HEAD -- docs/protocol proto docs/evidence/goal-02   (must be empty)
chain-prod-diff.txt           git diff --stat 6d74aca..HEAD -- chain ':!chain/**/*_test.go' ':!chain/**/README.md'   (must be empty)
tests-not-removed.txt / tests-replaced.txt   the two scaffold assertions and their replacements
mutants-goal11.txt            docs/evidence/goal-11/mutants.py --rev <tested commit>   ($NETNS for devnet mutants)
mutants-goal10.txt / -goal09.txt / -goal08.txt and their --check logs; goal06-anchors.txt / goal07-anchors.txt
golangci-lint.txt             recorded BLOCKED with its command if it still cannot run (goals 07-10)
disk.txt                      df -h / before and after (temp clusters and binaries removed)
head CI                       BLOCKED until the owner authorizes a draft PR; then run id and per-job result
```

Every number in `docs/evidence/goal-11.md` comes from these logs on the committed tree (DIRTY `no`). The evidence file
records: model `claude-opus-5-5`, effort `ultracode`, the playbook assignment GPT-6 Astra HIGH (goal spec :6), the
owner's two quoted sentences (`scratchpad/brief.md` section 5), the PostgreSQL version, the single-machine label on every
devnet claim, the fault-proxy label ("fault injected by a test proxy on loopback"), and each DR-11 answer as quoted owner
text or as an open interim.

---------------------------------------------------------------------------------------------------

## 16. Expected status, BLOCKED, DEFERRED, residual risks (forecast)

Expected goal status: BLOCKED. Reasons, each recorded with its command or decision:

- Prerequisite "approved service/provider signing boundaries" (goal spec :8): no approved document today. Recorded BLOCKED
  (DR-11-01) until the owner approves `docs/architecture/broadcaster.md` at a pinned commit and sha256, asked at PR time.
- Prerequisite goals 04, 07, 08, 09, 10 are BLOCKED in their own status (`docs/progress.md:34-40`).
- Production paths: content registration `DECISION_BLOCKED` off the devnet (DEC-001, DR-07-01); offers and renewals need
  the DEC-005 devnet number.
- Head CI until a draft PR is authorized; golangci-lint (built with go1.25); goal 06 and 07 mutation anchors.
- Any owner question left unanswered (its rows stay IMPLEMENTED as interim).

DEFERRED: the API's runtime use of the broadcaster (goals 13, 21), creator UI states (goal 21), provider-side submission
P2 (goal 16), indexer consumption of commitment evidence (goal 12), service-key rotation and signer recovery drills (goal
26), mTLS and service packaging (goal 27), throughput numbers (goal 28), the shared client package (goal 16).

Residual risks (each written into the evidence file):

- Reads trust one configured node; a lying node can make the broadcaster report COMMITTED. Goal 12 reconciles; no light
  client is claimed.
- No on-chain revocation of a compromised `ADMITTED_SERVICE` or `BROADCASTER` key; only an engine-level change can remove
  it (`map-decisions.md` section 6). Least privilege is the main control.
- A compromised `ADMITTED_SERVICE` key can mint devnet hosting intents with its own key; creator permission is not
  enforced on chain (`chain/x/content/README.md:61`).
- Provider inner signatures have no chain id and RENEW/END have no expiry; a leaked outbox row can be submitted by any
  `BROADCASTER` or the provider account until a renewal or rotation. Local expiry revokes nothing on chain.
- Business-id squatting: an admitted account can take a pending business id; the broadcaster spends a bounded number of
  business attempts and then fails (DR-09-28, DR-10-16). Denial of service, never a wrong transition.
- A broadcaster can delay or withhold a provider's signed acceptance until the offer expires (P2 is the remedy).
- Window of one per signer limits throughput to about one transaction per block per signer; block capacity is about
  `20,000,000 / declared gas` transactions (`map-chain.md` 1.5).
- `LISTEN/NOTIFY` is a hint only; correctness rests on polling with `FOR UPDATE SKIP LOCKED`.
- Mempool gossip timing and same-sequence transactions in different mempools are outside the broadcaster's control
  (`map-chain.md` 2.2); the tests prove outcomes, not mempool internals.
- PostgreSQL crash safety depends on `fsync=on`; the outbox is not backed up in goal 11 (goal 26).
- The raw `/store` path stays unmetered on the node; the node RPC must stay private (`scratchpad/notes-08-10.md:61-63`).
- The fault proxy proves the broadcaster's handling of lost requests and answers on one loopback link, not real network
  partitions.

---------------------------------------------------------------------------------------------------

## 17. Decision requests (interim choices are reversible and are not owner approval)

### 17.1 Owner questions: answer BEFORE Wave 1 (security-critical)

- **Q1 (DR-11-01) [SEC] Signing boundary: which keys may the broadcaster hold, for which messages, and behind which
  development boundary?** Why: whoever holds a key can send everything its role allows. The `AUTHORITY` key would let a
  compromised broadcaster execute any queued proposal at a time it picks and end any lease alone (`authority_end`,
  `docs/protocol.md:239`); seat keys would collapse the 3-of-4 council (DEC-008, DR-06-06); an auditor key would undo goal
  09 Q1; a provider key would let it consent for a provider (REQ-CHAIN-012). No on-chain revocation exists for service or
  broadcaster keys. The goal's prerequisite names an "approved" boundary that does not exist (section 1.6).
  Options: (a) one `BROADCASTER` key (relay accept, renew, provider end) and one `ADMITTED_SERVICE` key (register and
  withdraw content, offer), each with a type-URL allowlist, one message per transaction, held by a separate signer process
  on a 0600 Unix socket with a peer-uid check, devnet only; never AUTHORITY, seat, AUDITOR or provider keys; the boundary
  document is written by goal 11 and approved at PR time on a pinned commit and sha256, as DR-10-00 was.
  (b) as (a), but the keyring lives inside the `serve` process. (c) `BROADCASTER` key only: content and offers leave goal
  11 (another service signs them). (d) as (a) plus `AUTHORITY`. **Recommended: (a).**
- **Q2 (DR-11-02) [SEC] Which provider-side submission path is approved?** Why: goal spec :21-23 and goal 16
  (`docs/goals/16-storage-provider-agent.md:22-23`) both name "the approved provider-signing path", and none is approved.
  The choice decides whether the broadcaster can alter, delay or censor provider consent. Options: P1 the provider returns
  an inner signature and the broadcaster relays it as `BROADCASTER`, after verifying it against the provider's current
  registered key; P2 the provider submits its own transaction from its `ADMITTED_PROVIDER` account and the broadcaster only
  reconciles; P3 the broadcaster forwards provider-signed transaction bytes verbatim, which must carry a `timeout_height`.
  **Recommended: P1 and P3 in goal 11, P2 as the provider's fallback in goal 16.**
- **Q3 (DR-11-03) [SEC] What is "the publication permission bit", who holds the devnet hosting-intent key, how are internal
  callers authenticated, and what object permissions apply?** Why: REQ-CHAIN-013 tests a bit nothing defines
  (`docs/requirements.md:452`); the chain cannot enforce creator permission (`chain/x/content/README.md:61`); the
  broadcaster acts with its own chain authority for callers (confused deputy), and a withdraw is an irreversible
  tombstone. Options: (a) the bit is a creator-authorized hosting intent: the caller's authenticated context grants
  `PUBLISH_CONTENT`/`WITHDRAW_CONTENT` to an opaque principal, the intent is signed by a key registered for that principal
  in a broadcaster dev registry (written only by an operator command), the broadcaster never holds an intent key (a
  separate devnet fixture signer stands in for the account host, "not DEC-001 custody"), withdraws only by the recorded
  owner, offers only for content registered through this outbox and not withdrawn; callers authenticate with mTLS client
  certificates from a run-time development CA on loopback, plus a PostgreSQL role per caller for the SQL enqueue path;
  another caller's object answers 404. (b) as (a) without the key registry: the broadcaster trusts the caller's permission
  claim plus a valid intent signature. (c) the bit is the off-chain community publication approval (`docs/protocol.md:195`,
  DEC-003): the broadcaster refuses `OFFER_LEASE` for unapproved content; intents as in (b). Authentication alternatives
  inside any option: Unix socket with `SO_PEERCRED`, or per-caller bearer tokens. **Recommended: (a) with mTLS.**
- **Q4 (DR-11-04) [SEC] What may the outbox store, and for how long?** Why: "authorized request context" is new off-chain
  storage that can hold personal data outside any privacy assessment (DEC-007, `docs/decision-register.md:217`) with no
  retention rule (DEC-004, `:127`); stored inner signatures and transaction bytes are reusable authorizations (no chain id;
  RENEW/END never expire). Options: (a) store only the minimal context of 5.5; refuse emails, IP addresses, tokens, headers
  and free text; treat unbroadcast inner signatures as sensitive (never logged, returned only as digests); give RENEW and
  END relays a PROPOSED local expiry and refuse ACCEPT relays after the offer expiry; keep transaction bytes and
  reconciliation rows with retention unset and no automatic deletion. (b) as (a) plus automatic deletion after a period
  (needs a number the owner sets; goal 11 will not invent one). (c) store the full caller request. **Recommended: (a).**

The orchestrator sends Q1-Q4 in one message and says in it: the boundary document's approval is asked at PR time on a
pinned commit and sha256; the language choice (DR-11-20) and the replaced scaffold tests; the interim rules of 17.2, each
labelled fail-closed or not.

### 17.2 Security-relevant requests with interim rules (implemented, tested, reported open)

- DR-11-05 Commit, failure, uncertainty and deadlines (fail-closed): COMMITTED only from a committed result (9.4); FAILED
  only from a committed code, a definitive refusal (d2), or chain proof that the attempt cannot commit (d3); every attempt
  carries `timeout_height`; `[Educated Guess]` not a protocol change (`docs/protocol.md:187` constrains only fee and gas),
  and goal 11 tests the ante semantics (13.3).
- DR-11-06 Audit batches (fail-closed): the broadcaster never submits `SubmitAuditBatch` and holds no `AUDITOR` key; goal
  09's "Outbox submission: Goal 11" (`docs/evidence/goal-09.md:39`) is recorded as answered by goal 09 Q1 ("No
  `BROADCASTER` relay", `:74`); the auditor keeps its own submit and status path. Closing a recorded deferral needs the
  owner's confirmation at PR time.
- DR-11-07 Business ids (fail-closed against prediction, not against front-running): random per business attempt, stable
  across its resends; a new id only after reconciliation shows no effect; bounded business attempts. Signer-bound ids are a
  protocol addition (DR-09-28).
- DR-11-08 Reconciliation read path (fail-closed): `/tx` by hash with a hash check, `/tx_search` by `tx.acc_seq` and by
  typed event, gRPC-routed object queries, `/commit` for block hashes; never raw `/store` or `/check_tx`; one configured
  node, disclosed as trusted for reads (`docs/blueprint.md:391`).
- DR-11-09 Caller-visible status: the five frozen `OperationStatus` values plus `detail.phase` (UNCERTAIN, AWAITING_DEADLINE,
  RETRY_WAIT). Adding an enum value is a protocol change (DR-11-40).
- DR-11-10 PROPOSED numbers (not fail-closed: new numbers): K = 40 blocks, L = 2 blocks, T = 10 s, 8/4/16 attempt caps,
  backoff 1 s to 60 s, intent lifetime 30 s to 24 h, open intents 256 per caller and 4,096 in all, RENEW/END local expiry
  3,600 s, forward window 100 blocks, gas 2,000,000 per kind, RPC timeout 10 s, poll 500 ms, list page 100, sdk 4 retries
  3. Each has code bounds; none is a service level. The owner is asked to accept the labels, not the values.
- DR-11-11 Stall rule (fail-closed): no new attempt while stalled; live attempts are never abandoned; an intent with nothing
  sent fails at its expiry.
- DR-11-12 Not routed (fail-closed): council messages, `MsgExecuteChange`, `authority_end`, provider registry messages
  (`RotateProviderKey` must be reconciled by query, `chain/x/providers/README.md:126-128`).
- DR-11-13 Service-key rotation (`RotateServiceSubmitter`, engine-only) leaves content `submitting_host` and offer
  schedulers on the old address; intents pin their signer; rotation is goal 26. Open.
- DR-11-14 One message per transaction (fail-closed).
- DR-11-15 Signer conflict (fail-closed): a foreign use of a broadcaster key halts that signer until an operator clears it.
- DR-11-16 Threat-model additions (confused deputy, log leakage, business-id squatting, stored authorizations): asked,
  not edited (`docs/threat-model.md` is a goal 01 document).
- DR-11-17 The publication half of REQ-CHAIN-013 is recorded in parts (broadcaster check, caller claim, chain binding), as
  section 14 shows; the owner confirms the reading.
- DR-11-18 DR-10-41 handling: sdk 4 is retryable a PROPOSED number of times, at least two blocks apart; then FAILED and a
  signer alert. Not fail-closed for liveness.

### 17.3 Reversible implementation choices (documented as interim; no owner input needed)

DR-11-20 Go module at `services/broadcaster`, replacing the Python scaffold and its two pinning tests by design (section
3, section 12). DR-11-21 copy-and-generalise the auditor client; no auditor refactor (4.4). DR-11-22 lib/pq v1.12.3 as
the only new direct dependency, already in `chain/go.sum` and svnd. DR-11-23 embedded `.pgsql` migrations and a small
forward-only runner (`.gitignore:70` ignores `*.sql`; no negation, which would weaken the dump rule). DR-11-24 one
PostgreSQL helper (`internal/pgdev`) used by tests and by `svn-broadcaster dev-db`; PostgreSQL 18.6; SQL compatible with 16.
DR-11-25 CI: shards `broadcaster` and `devnet-broadcaster`, a pinned PGDG install step (`postgresql-18=18.6-1.pgdg24.04+2`,
key fingerprint as `docs/dependencies.md:39`), and the `SVN_SKIP_POSTGRES` guard; new required check names need the owner's
branch-protection update. DR-11-26 one live transaction per signer. DR-11-27 fixed per-kind gas, Simulate for withdraws.
DR-11-28 a second intent with an identical effect reports COMMITTED with `own_transaction=false`. DR-11-29 devnet tests in
`chain/devnet` driving the built binary through a loopback fault proxy. DR-11-30 JSON bodies, no new `.proto`; status
names reuse `OperationStatus`. DR-11-31 internal phase names beyond `docs/protocol.md:217`.

### 17.4 Protocol additions recorded, not implemented (DR-11-40)

An `UNCERTAIN` `OperationStatus` value; business ids bound to the signer (DR-09-28); chain id and expiry inside provider
inner payloads and hosting intents (DR-07-01, DR-08-04); get-by-id queries for leases, offers and business ids (DR-08-08);
a registered hosting-intent key per creator (DR-07-01); an on-chain revocation of service and broadcaster keys.

---------------------------------------------------------------------------------------------------

## 18. Work packages

Rules for every package: work only in `$WT`; one owner per file (paths listed); run only the package's own commands
(4 CPUs, other work runs; no full suites outside the gate); no `git commit`; never edit `docs/goals/**`,
`docs/decision-register.md`, `docs/threat-model.md`, `docs/protocol*`, `proto/**`, `docs/evidence/goal-0*/**`,
`requirements.lock`, `chain/go.mod`, `chain/go.sum`, or any chain production file (stop and report if a change seems
needed); delete temp clusters and binaries; mocks only in unit tests; comments describe what the code does; no dead code;
every input bounded; every error handled; parameterized SQL only.

### Preconditions (orchestrator)

- Send Q1-Q4 (17.1) with the notes listed there.
- Confirm disk headroom before Wave 1 (about 1.8 GB free now; a cluster is tens of MB, a binary and its cache entries a few
  hundred MB `[Educated Guess]`).

### Wave 0 (no answer needed)

- B1 store and PostgreSQL — owns `services/broadcaster/{go.mod,go.sum}`, `internal/store/**`, `internal/pgdev/**`,
  `internal/pgtest/**`: schema 6.2 (Q-dependent tables behind a later migration), roles, migrations runner, every query,
  the enqueue function, the 13.2 tests. Uses `/usr/lib/postgresql/18/bin`.
- B2 chain client — owns `internal/chain/**`: 1.3 generalisation, `timeout_height`, hash-checked `/tx`, `TxSearch`,
  `Commit`, queries, the 8.4 classifier, effect comparison; 13.1 chain tests and `FuzzClassifyRPCError`.
- B3 state machine — owns `internal/outbox/**`: 9.1-9.3, 8.5, 8.6 as pure functions over typed observations; 13.1 outbox
  tests, `TestStateMachineProperty`, `FuzzTransitions` with reachability.
- B4 chain-level checks — owns `chain/app/tx_deadline_test.go`: 13.3. Test-only.
- B5 documentation draft — owns `docs/architecture/broadcaster.md`, `services/broadcaster/README.md`,
  `docs/architecture/README.md` (index line): the boundary document (PROPOSED, every open question with its interim), the
  API, the schema, the states.

### Wave 1 (after the answers)

- B6 signing and policy — owns `internal/keys/**`, `internal/signer/**`, `internal/policy/**`, `internal/logx/**`: 7.1-7.7
  under the answers; 13.1 policy, signer and redaction tests; `TestNoSigningOutsideSigner`, `TestConfigHasNoIntentKey`;
  `FuzzForwardedTx`.
- B7 API, config and binary — owns `internal/api/**`, `internal/config/**`, `cmd/svn-broadcaster/**`: section 5, section
  11, `dev-ca`, `dev-db`, `migrate`, `status`; 13.1 API tests; `FuzzDecodeIntentRequest`.
- B8 worker — owns `internal/worker/**`: section 8 and section 10 over B1-B3 and B6; PostgreSQL-backed worker tests with a
  fake RPC (unit-labelled) plus one real-chain smoke run deferred to B9.
- INT — owns the section 12 files: workspace, test runner, scaffold removal and replacement record, policy tests, CI
  shards and guards, dependencies, SBOM.

### Wave 2

- B9 devnet — owns `chain/devnet/broadcaster_test.go`: 13.4 including the fault proxy; measures the shard time; splits
  into two `-run` filters if it nears 12 minutes under `-race`.
- B10 mutation runner — owns `docs/evidence/goal-11/mutants.py` and its `PY_ALLOW` line in
  `docs/evidence/goal-01/check_requirements.py` (as goals 08-10 did): 13.7; `--check` and `--worktree`.
- Gate: section 15 commands on the uncommitted tree; three review lenses (security and signing boundary, evidence honesty,
  tests); fixes by the owning package; rerun.

### Wave 3 (evidence; orchestrator)

Commit the tested code (message without the model id, `scratchpad/notes-08-10.md:126-129`); section 15 captures on the
committed tree; `docs/evidence/goal-11.md` in the shape of `docs/evidence/goal-06.md`; progress rows (expected BLOCKED);
evidence commit; ask the owner about a draft PR and, in the same message, for approval of
`docs/architecture/broadcaster.md` at the committed sha256 (DR-11-01) and confirmation of DR-11-06.

---------------------------------------------------------------------------------------------------

## 19. Notes for the orchestrator

- Disk is tight. Each devnet test run writes four homes and a PostgreSQL cluster under `/tmp`; the helpers clean up, but a
  killed run can leave `/tmp/svn-pg-*` and devnet temp dirs: check and remove them after aborted runs.
- `postgres` cannot traverse `/tmp/claude-0`; never point `pgdev` at the scratchpad or the worktree. `TMPDIR` is empty
  here; if a harness sets it under `/tmp/claude-0`, `pgdev` refuses with a clear error.
- Run long captures as harness-tracked background tasks of at most two hours (`scratchpad/notes-08-10.md:209`).
- DR-10-41 caused flakes before; any devnet phase that restarts a node waits for height to advance before it asserts, and
  any test that needs a nonzero account number picks the signer that has one (`scratchpad/notes-08-10.md:213`).
- New CI check names (`go-race (broadcaster)`, `go-race (devnet-broadcaster)`) may need a branch-protection update by the
  owner. The PGDG install step is a network fetch in CI; it is documented in `docs/dependencies.md`.
- Report, do not edit: the 1.7 discrepancies; `docs/threat-model.md` gaps (DR-11-16); the progress row 09 deferral wording
  (DR-11-06).

---------------------------------------------------------------------------------------------------

## 20. Steps that still need a human approval or an independent review (handoff list)

1. Answers to Q1-Q4, and approval of `docs/architecture/broadcaster.md` at a pinned commit and sha256 (DR-11-01).
2. Confirmation that goal 09's outbox deferral is closed by its Q1 ruling (DR-11-06).
3. Acceptance of the PROPOSED number labels (DR-11-10) and of the language and layout change (DR-11-20).
4. Production key custody for service and broadcaster keys (DEC-001, goal 26); any non-devnet chain id.
5. A retention period for outbox rows and transaction bytes (DEC-004), and the privacy assessment (DEC-007).
6. Threat-model additions (DR-11-16) in the goal 01 document.
7. Branch-protection updates for new CI checks; the goal 11 draft PR and its merge.
8. Independent security review of the signer, the policy pre-checks, the reconciliation rules and the SQL privilege
   model (REQ-SEC-011, goal 29). Model-assisted review is not that review.
