# Goal 11 plan: transaction broadcaster and PostgreSQL outbox (services/broadcaster, signing boundary, reconciliation, devnet fault tests)

Planning lead output, revision 2. Revision 1 is kept unchanged as `scratchpad/goal-11/plan-r1.md` (sha256
`0dff335d9591f1fc4b580b2348eee0b3cbf8d9e90f57b2f2bae77eab81dc7e70`). Three critics reviewed it: spec and security, tests
and evidence, scope and honesty. Each finding was checked against `$WT` at `6d74aca` or against the module cache before it
was applied. Section 21 lists every finding with its outcome. No production code was written. No repository file was
edited, added, committed or pushed. This revision wrote only `scratchpad/goal-11/plan.md`, `scratchpad/goal-11/plan-r1.md`,
`scratchpad/goal-11/owner-questions.md` and its draft parts under `scratchpad/goal-11/r2parts/`.

- Worktree (the only place any agent reads, edits, builds or tests):
  `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/wt-11` (written `$WT`),
  branch `goal-11`. The task text says the branch was created from main `dbf0897` and that PR 15 merges before goal 11
  commits. PR 15 has merged: main is `6d74aca`, and `$WT` was fast-forwarded to it (`scratchpad/notes-08-10.md:272-277`).
  HEAD is `6d74aca5cfc461a9756587bdbec2f78e7111e574`, tree `5062bb44`. `git diff --stat dbf0897 6d74aca` touches only
  `chain/app/app.go`, `chain/cmd/svnd/cli/genesis*.go` and a README. Every line number in this plan is for `6d74aca`.
  Never touch `/home/user/shortvideo-network`, which is an old goal-07 checkout (`scratchpad/brief.md` section 0).
- Netns wrapper for anything that starts svnd or PostgreSQL in a devnet run:
  `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh <cmd...>`
  (written `$NETNS`). It runs `unshare --net` as root with only loopback up (`netns-run.sh:6-13`). There is no outbound
  network inside, so Go modules must be cached.
- Goal spec: `docs/goals/11-transaction-broadcaster-and-outbox.md` (never edit). "goal spec :N" means that file, line N.
  Playbook assignment: GPT-6 Astra, EFFORT HIGH (goal spec :6).
- Short names: `SDK/` = `/root/go/pkg/mod/github.com/cosmos/cosmos-sdk@v0.55.0`, `CMT/` =
  `/root/go/pkg/mod/github.com/cometbft/cometbft@v0.40.0`, `PQ/` = `/root/go/pkg/mod/github.com/lib/pq@v1.12.3`
  (`go env GOMODCACHE` = `/root/go/pkg/mod`). "spec_machine" = `docs/evidence/goal-02/spec_machine.py`, "cases" =
  `docs/evidence/goal-02/cases.py`.
- Labels. A sentence with a `file:line` citation is a fact read on this tree or in the module cache. "Derived from source"
  means read in the pinned source but not exercised by any test in this repository; goal 11 must test it before relying on
  it. `[Educated Guess]` marks an inference. Every design item in sections 3 to 13 is a proposal, not an owner decision.
  Nothing in this plan is an owner decision unless it quotes one.
- Owner rulings that bind goal 11 (quoted from the records; no owner ruling about goal 11 itself exists):
  - DR-06-06, 2026-11-02: "Seat-only admission". A seat key signs only propose and approve; a seat address is never a
    submitter (`scratchpad/notes-08-10.md:86-90`; `chain/app/ante.go:139-180`). The broadcaster never holds a seat key.
  - Goal 09 Q1, 2026-11-02: "Account key signs (Recommended)": "The submitting account must be an `AUDITOR`. ... No
    `BROADCASTER` relay, no rotation. Devnet beta only." (`docs/evidence/goal-09.md:74`).
  - DR-10-00, 2026-11-08: "Approve as pinned", for `docs/architecture/governance-and-upgrades.md` at `18fa310`, for goal 10
    only (`scratchpad/notes-08-10.md:221`). It is a precedent for how a goal 11 boundary document could be approved. It
    approves nothing for goal 11.
  - Model substitution: "I acknowledge the model substitution for DR-06-07", then "extend the acknowledgement to goals 07
    through 30" (`scratchpad/brief.md` section 5).
  - Relayed user request for this run: "I authorize you to merge it and complete the next goals"
    (`scratchpad/notes-08-10.md:272`). "it" is PR 15. This plan reads the sentence as authority to work on goal 11. It
    answers no DR-11 request below. The brief says each PR and each merge is authorized separately (`scratchpad/brief.md`
    section 0). Whether the sentence also pre-authorizes a goal 11 PR or merge is for the orchestrator to confirm with the
    owner, not for this plan to assume.
- Not an owner ruling. `[Educated Guess]` Goal 09 Q1 forbids a broadcaster-signed audit batch on chain. It does not decide
  whether AUDITOR-signed batch bytes may pass through the outbox verbatim, which needs no broadcaster key. Goal 09 recorded
  "Outbox submission: Goal 11" as deferred (`docs/evidence/goal-09.md:39`; `docs/progress.md:39`). That item stays
  DEFERRED until the owner answers Q2 (DR-11-06). Revision 1 listed it under the rulings; that was wrong.
- Owner questions. Four security-critical or policy questions must be answered before Wave 1 (section 17.1). They are in
  `scratchpad/goal-11/owner-questions.md`, each self-contained: Q1 the signing boundary and the broadcaster's key set
  (DR-11-01), Q2 which externally signed submissions the broadcaster may carry (DR-11-02, includes DR-11-06), Q3 the
  publication permission, hosting-intent key custody and object permissions (DR-11-03), Q4 what the outbox may store and
  how it is minimized (DR-11-04). Wave 0 (section 18) does not depend on any answer. Wave 1 starts only after all four are
  answered; there is no interim implementation of an answer-dependent part. Approval of the boundary document itself is
  asked at PR time on a pinned commit and file sha256, as DR-10-00 was.
- Disclosure. The runtime reader of revision 1 ran `initdb` as root once while mapping; it refused ("cannot be run as
  root"), exited 1 and created nothing (`map-runtime.md` section 1 item 7). It ran `go mod why -m github.com/lib/pq` with
  the default `-mod`; `git status --short` in `$WT` stayed empty. Revision 2 ran only reads (`sed`, `grep`, `git log`, `id`,
  `getent`, `df`, `du`). Nothing else was executed.

Section map: 1 findings; 2 scope, non-scope, must not; 3 language and runtime; 4 architecture, process identities and file
layout; 5 typed request API; 6 outbox schema and the one-transaction coupling; 7 signing boundary; 8 submission engine; 9
state machine and commitment evidence; 10 chain outage; 11 configuration and PROPOSED numbers; 12 repository integration
points; 13 test plan; 14 acceptance and requirements matrix; 15 evidence capture list; 16 expected status and residual
risks; 17 decision requests; 18 work packages; 19 orchestrator notes; 20 steps that still need a human; 21 changelog of
revision 2.

---------------------------------------------------------------------------------------------------

## 1. Findings that shape the plan

### 1.1 The chain transaction path a broadcaster faces

- One TxConfig: `app.MakeEncoding` enables only `SIGN_MODE_DIRECT` and installs a strict decoder that rejects any unknown
  field in the body, including non-critical ones (`chain/app/encoding.go:44-81,110-138`). Each registered message has one
  20-byte `signer` field that is its only signer (`chain/app/encoding.go:144-173`).
- The ante chain (`chain/app/ante.go:24-45`) runs, in order: SetUpContext, ExtensionOptions(nil), ValidateBasic,
  TxTimeoutHeight, envelope (non-empty memo `BOUNDS`; fee payer, granter or tip `FEE_REJECTED`, `:66-86`), memo, bounds
  (gas outside `[1, 20,000,000]` except 0 in simulation, tx above 65,536 bytes: `BOUNDS`, `:90-108`), tx-size gas, fee
  reject (`:112-121`), SetPubKey, sig count, sig gas, signature verification, admission (`:41`; its seat refusal is svn
  `FORBIDDEN`, `:164`), sequence increment. No memo, fee payer, granter or tip can carry an intent id.
- `NewTxTimeoutHeightDecorator` is installed (`chain/app/ante.go:32`). In SDK v0.55.0 it rejects a transaction when
  `timeout_height > 0` and the context height exceeds it (sdk code 30), or when `timeout_timestamp` is set and before the
  block time (sdk code 42) (`SDK/x/auth/ante/basic.go:205-227`; `SDK/types/errors/errors.go:100,147`). No repository code
  sets either field (`map-chain.md` 3.5). Goal 11 tests the semantics itself (section 13.3).
- Unordered transactions are refused: the account keeper is built without `WithUnorderedTransactions(true)`
  (`chain/app/app.go:199-207`), so per-signer sequences are strict (`SDK/x/auth/ante/sigverify.go:319-325`).
- CheckTx runs only the ante chain; handlers run only in FinalizeBlock and Simulate (`scratchpad/notes-08-10.md:64-65`).
  A CheckTx code 0 says nothing about roles, the business gate or capacity: on the devnet an acceptance passes CheckTx with
  code 0 and commits with svn `CAPACITY` (`chain/devnet/lease_test.go:507-530`).
- Writes and events of a failed transaction. On an ante failure `runTx` returns no events and keeps no writes
  (`SDK/baseapp/baseapp.go:916`); the block result carries the code only. On a handler failure after a passing ante the
  ante writes are kept, so the sequence is consumed, and the result carries the ante events (`:916-919` keep them;
  `:720-731` returns them with the error). A transaction that finds no block gas left returns before the ante with sdk
  code 11 (`:803-805`). The same code can come from either side: svn `FORBIDDEN` from the admission decorator
  (`chain/app/ante.go:164`) or from a handler; sdk 11 from the block gas meter or from a handler. So no code tells whether
  the sequence was consumed.
- `tx.acc_seq`. SetPubKey emits `tx.acc_seq=<bech32>/<sequence>` with the transaction's own sequence
  (`SDK/x/auth/ante/sigverify.go:119-141`). Ante events survive only when the whole ante passed (previous item). So a
  committed result carries `tx.acc_seq=<S>/<N>` if and only if the ante passed and sequence N of S was consumed (derived
  from source; tested in 13.3). Revision 1 said "every transaction whose ante reaches SetPubKey"; that was wrong.
- After every node start, CheckTx uses account number 0 until that node commits a block, so correctly signed transactions
  from accounts numbered above 0 get sdk code 4 (DR-10-41; `docs/architecture/governance-and-upgrades.md:667-683`;
  `SDK/x/auth/ante/sigverify.go:377-382`). It caused two goal 10 test flakes (`docs/evidence/goal-10.md:124,183`).
- The node ignores `config.toml`; it builds CometBFT config from `cmtcfg.DefaultConfig()` plus `node.json`
  (`chain/node/start.go:211-234`). The mempool is therefore the v0.40.0 default: flood type, recheck on, gossip on, no WAL,
  cache of 10,000 entries (`CMT/config/config.go:1021-1033`), no TTL (`map-chain.md` 2.3). Admitted bytes wait until a
  block includes them, recheck evicts them, or every node holding them restarts.
- The mempool cache. A transaction CheckTx accepted, or one that committed, stays in the cache. A resend of the same bytes
  gets the JSON-RPC error `tx already exists in cache` before CheckTx (`CMT/mempool/clist_mempool.go:257-271`;
  `CMT/mempool/errors.go:11-12`). A CheckTx refusal removes the bytes from the cache (`clist_mempool.go:466,499`,
  `KeepInvalidTxsInCache` is false by default). After a node restart the cache is empty, so the bytes of a committed
  transaction get CheckTx sdk code 32 (sequence already used), not the cache error.
- Devnet blocks are produced continuously: `CreateEmptyBlocks = true`, interval 0, `TimeoutCommit` 200 ms and
  `SkipTimeoutCommit = true` (`chain/node/start.go:237-248`). Heights advance on an idle chain, so a `timeout_height`
  passes without traffic, and a block can follow an acknowledgement faster than one API poll.
- `broadcast_tx_sync` returns the CheckTx answer; a mempool error before CheckTx comes back as a JSON-RPC error
  (`map-chain.md` 4.1). The `/check_tx` route calls the app's CheckTx without adding to the mempool
  (`CMT/rpc/core/mempool.go:186-194`); derived from source, it still advances the check-state sequence, so the broadcaster
  never uses it.
- Reads: `/tx?hash=` returns height, index, result and the raw bytes; `/tx_search` takes queries up to 512 bytes and at
  most 100 results per page (`CMT/rpc/core/events.go:16-18`; `CMT/rpc/core/env.go:23-24`). Typed events copy string
  attributes as they are and index every attribute (`chain/msgkit/events.go:60-75`), so a query such as
  `svn.events.v1.LeaseAccepted.lease_id='l1'` is well formed (derived from source). No event carries a business id
  (`map-chain.md` 5.2).
- Simulation: `abci_query` with path `/app/simulate` runs `app.Simulate` on the given bytes (`SDK/baseapp/abci.go:207-209,
  1136-1145`). In simulate mode the SDK skips signature verification (`SDK/x/auth/ante/sigverify.go:387`) and the sig gas
  decorator accepts a missing public key (`:209-213`), so a simulation transaction with an empty signature is accepted
  (derived from source; tested in 13.3). The node also serves the SDK tx gRPC service (`chain/node/start.go:113-139`;
  `chain/app/app.go:691-694`); goal 11 does not use it.
- Deterministic signatures. SDK v0.55.0 secp256k1 signing uses dcrd `ecdsa.SignCompact`, which is RFC 6979 deterministic
  (`SDK/crypto/keys/secp256k1/secp256k1_nocgo.go:13-21`). Signing the same body at the same sequence gives the same bytes
  and the same hash.

### 1.2 Roles, signatures and business ids

- Roles are set at genesis; no message adds a submitter (`docs/architecture/authority-authorization.md:16-17`). The devnet
  has `authority`, `service`, `service-2`, `provider-1`, `provider-2`, `broadcaster` (secp256k1) and `auditor-1`,
  `auditor-2` (ed25519) (`chain/devnet/init.go:130-144`), plus four seat keys (`map-chain.md` 10). There is one
  `BROADCASTER` account (`:143`) and a second `ADMITTED_SERVICE` account, `service-2` (`:138`).
- Who may sign what, as the code enforces it (`map-signing.md` section 5):
  - `RegisterContent`, `WithdrawContent`: `ADMITTED_SERVICE` only; the signer becomes `submitting_host`; a `BROADCASTER`
    outer signer is `FORBIDDEN` (`chain/x/content/msg_server.go:78,186,348-357`; `chain/x/content/README.md:39`).
  - `OfferLease`: `ADMITTED_SERVICE`; the signer becomes the scheduler (`chain/x/leases/msg_server.go:239`).
  - `AcceptLease`, `RenewLease`, provider `EndLease`: `BROADCASTER` (relay) or the provider's own `ADMITTED_PROVIDER`
    account, with the provider's inner signature by its current key (`chain/x/leases/msg_server.go:405,446,628-630`).
  - `EndLease{authority_end}`: one `AUTHORITY` signature, "outside DEC-009" (`chain/x/leases/msg_server.go:680-681`;
    `docs/protocol.md:239`).
  - `SubmitAuditBatch`: `AUDITOR` only; a `BROADCASTER` relay is `FORBIDDEN` (`chain/x/evidence/README.md:277`).
  - Provider registry messages: the provider account only (`map-signing.md` section 5).
- Withdraw authority. `WithdrawContent` checks the service role, a withdraw intent signed by any key with role
  `DEVNET_TEST_ONLY` (`chain/x/content/intent.go:126-152`), and that the signer is the content's submitting host
  (`chain/x/content/msg_server.go:176-219`, host check `:215`). "The withdraw intent is not bound to the key that signed the
  register intent" (`chain/x/content/README.md:63`). A withdraw is an irreversible tombstone (`docs/protocol.md:234`).
- Offer authority. No offer check binds the scheduler to the content's submitting host (`chain/x/leases/msg_server.go:203-224`,
  the full check list). The offer business fields exclude the scheduler (`chain/x/leases/README.md:136`), yet the
  scheduler owns the offer: an idempotent replay by another scheduler is `FORBIDDEN` (D-11, `README.md:519`;
  `msg_server.go:280-290`), and the scheduler is bound into the provider's ACCEPT payload (`README.md:153`). Another admitted
  service can commit a copy of a pending offer first (the D-11 comment, `msg_server.go:281-286`); an identical offer of ours
  then gets `DUPLICATE_ID` (`:294`).
- The `AcceptLease` business digest excludes the signer; a `BROADCASTER` replay stays idempotent (D-13, `README.md:521`;
  `msg_server.go:426-438`). A relay whose offer has closed gets `NOT_FOUND` (`:441-443`). The inner signature is verified
  before the provider-account check (`:446-452`).
- Effects with no committing transaction of the intent's kind: begin-block expiry emits `LeaseEnded{state "EXPIRED"}`
  (`chain/x/leases/README.md:323-337`); a council force-withdraw emits `ContentWithdrawn` inside `MsgExecuteChange`
  (`chain/x/content/msg_server.go:250-254,300-330`).
- Provider consent is an ed25519 inner signature by the provider's current registered key over the stored offer terms and
  the lease id (ACCEPT), the message (RENEW), or lease id, provider id and current term version (END)
  (`chain/x/leases/README.md:146-162`). Exported preimage builders: `AcceptSignBytes`, `RenewSignBytes`, `EndSignBytes`
  (`chain/x/leases/signdoc.go:92,104,117`). Limits: no chain id in any inner payload, no expiry on RENEW or END, no proof
  that the provider holds its registered key (`chain/x/leases/README.md:148,164-168`; `map-signing.md` section 6).
- The provider never signs the business id: the `AcceptLease` business fields are `lease_id, offer_id,
  provider_acceptance` (`chain/x/leases/README.md:138`). The relayer chooses the business id.
- Hosting intents (devnet interim): the intent key signs `{action, contentId, contentVersion, manifestSha256 |
  reasonSha256, packageBytes, roleName, submittingHost}`, where `submittingHost` is the outer signer, so one intent
  authorizes one host (`chain/x/content/intent.go:14-33`). The key is the one in the message; the signature "does not prove
  a creator authorized the host" (`chain/x/content/README.md:61`). Every non-devnet chain is `DECISION_BLOCKED` (`:60`).
  Exported builders: `RegisterIntentSignBytes` (`intent.go:83`) and `WithdrawIntentSignBytes` (`:98`); the inner
  verifier is `msgkit.VerifyInner` (`chain/msgkit/innersig.go:115`).
- Business ids: one unsigned namespace across five modules (`map-chain.md` 6). Same id and payload is code 0 with
  `<module>_result idempotent=true` and no typed event (`chain/msgkit/events.go:324-329`); another payload is `REPLAY`; a
  failed attempt stores nothing (`docs/protocol.md:185`). Every check before the gate runs again on a retry, so a retry of a
  committed action can return `FORBIDDEN`, `UNAUTHORIZED`, `BOUNDS` or a role failure; the outbox "reads the committed
  result by transaction hash, or the lease state, before it resubmits, and never treats a resubmission's code as the first
  transaction's result" (`chain/x/leases/README.md:607-620`). A committed rotation is reconciled by querying the provider
  (`chain/x/providers/README.md:126-128`). Any admitted account can take a pending business id first; the auditor draws
  retry ids from `crypto/rand` (`chain/x/evidence/README.md:703-712`).
- No gRPC query returns anything by business id, by closed offer id or by ended lease id (`map-chain.md` 7). Raw `/store`
  reads are unmetered and unpaged (`scratchpad/notes-08-10.md:61-63`).
- `Query/Governance` returns the submitters with their roles and the devnet seats
  (`docs/protocol/proto/svn/authority/v1/state.proto:70-78`; `chain/x/authority/README.md:160-166`).

### 1.3 Existing client code and the scaffold

- `agents/auditor/chainclient` is the only chain client outside tests (`map-chain.md` 8.1). Reusable: the hardened HTTP
  client (no proxy, no redirect, timeouts, response cap; `ResponseHeaderTimeout` and `Client.Timeout` both equal the RPC
  timeout, `agents/auditor/chainclient/rpc.go:102-120`), the `RPC` interface (`:73-80`), `Header` (refuses a catching-up
  node, `:197-209`), `Broadcast`, whose answer "is never a commit" (`:340-356`), `CommittedTx` ("not found" is a JSON-RPC
  error, `:359-386`), `SVNCode` (`:388-397`), `BuildTx` with SDK `tx.SignWithPrivKey` (`tx.go:34-82`) and the strict
  re-decode `TxMessage` (`tx.go:86-100`). The submitter stores the bytes, then sends, then stores the record; the record
  defaults to `AckUnknown` and is written only after the send (`submit.go:590-635`, default `:609`).
- Not reusable as is: `BuildTx` and `TxMessage` are typed to `*evidencev1.MsgSubmitAuditBatch` (`tx.go:34,86`); the chain
  id comes from the node and the sequence from committed state before each send (`submit.go:596-603`); no deadline field is
  set; `CommittedTx` does not check that the returned bytes hash to the requested hash (`rpc.go:370-385`); `Account` queries
  the latest state and ignores the answer's height (`rpc.go:305-337`). Its bounds are `MaxSubmissions = 16` per batch and
  `maxRounds = 4` broadcast rounds per `Submit` call (`submit.go:78-82`). The goal 09 mutation runner anchors on
  `chainclient/submit.go` (`docs/evidence/goal-09/mutants.py:341,396`), so refactoring the auditor would move goal 09
  anchors.
- `services/broadcaster` is a Python scaffold: `serve()` raises "does not submit transactions"
  (`services/broadcaster/svn_broadcaster/scaffold.py:1-7`); the README says it "does not hold keys and it does not speak to
  a node" (`services/broadcaster/README.md:3`). Pinned by `tests/unit/test_scaffold.py:24-27` and
  `tests/unit/test_repo_policy.py:17`; listed in `pyproject.toml:31,47` and `README.md:14`.
- `services/api` raises too, and goal 13 is the first API goal (`map-decisions.md` section 1 item 10). No caller of the
  broadcaster exists yet.

### 1.4 Protocol and spec facts about the outbox

- Client operation states: "requested, submitted, committed, failed, and pending while the chain is stalled"
  (`docs/protocol.md:217`). The frozen enum is `OperationStatus` with REQUESTED, SUBMITTED, COMMITTED, FAILED,
  PENDING_STALL (`docs/protocol/proto/svn/offchain/v1/records.proto:45-52`). It has no "uncertain" value.
- "The broadcaster, not a chain handler, records `PENDING_STALL` in its outbox" (`docs/protocol.md:260`). DEC-011's approved
  text: "... the outbox records `PENDING_STALL`." (`docs/decision-register.md:337`). The reference outbox
  `broadcaster_submit` parks a request with its business id when blocks are withheld and returns `ERROR_CODE_QUORUM`
  (`cases:64-77`); `pending` is excluded from the app hash (spec_machine `:2030-2041`). The protocol's error table says
  the fixture `down` set does not produce `ERROR_CODE_QUORUM` (`docs/protocol.md:129`). Goal 11 records a stall and returns
  no chain code (DR-11-19).
- Reconciliation fields the protocol says are retained, "and does not treat as a retention period": business id,
  transaction hash, message type, height, committed time, object id, digest, byte count, result code, reservation delta and
  the idempotent flag (`docs/protocol.md:272`). Goal 11 stores more than that list; Q4 lists every extra column.
- "A broadcaster may relay a transaction it did not author. It cannot manufacture the inner signature."
  (`docs/protocol.md:183`). The relay column of the transition table lets a broadcaster submit content messages and audit
  batches (`docs/protocol.md:233-234,240`); the code refuses both (recorded deviations, `chain/x/content/README.md:39`;
  `chain/x/evidence/README.md:277`).
- The protocol envelope rule constrains the fee amount and the gas limit (`docs/protocol.md:187`); it says nothing about
  `timeout_height`. `[Educated Guess]` Setting `timeout_height` is not a protocol change (DR-11-05).
- Blueprint: "Python services request bounded operations through an operator-authorized Go broadcaster or generated
  client" (`docs/blueprint.md:373`); "Use a transaction outbox in PostgreSQL ... records attempt IDs and reconciles unknown
  submissions before resending. Use a stable business ID" (`:385`); "A successful broadcast or mempool response is not a
  committed successful transaction. The broadcaster waits for inclusion and execution success, then updates the
  application projection." (`:305`); the broadcaster "must not be able to manufacture provider acceptance or bypass a
  user's publication permissions" (`:242`); "A service submits a creator's authorized hosting intent" (`:242`); "If the
  chain stops, queue requests with explicit expiry and show them as pending" (`:395`); the chosen gateway is trusted for
  reads at beta (`:391`); the idempotency test row (`:766`); "The database projection is a cache of coordination state"
  (`:387`).

### 1.5 Environment, repository rules and CI facts

- PostgreSQL 18.6 (the goal 04 pin, `docs/dependencies.md:39`) is installed at `/usr/lib/postgresql/18/bin`, and 16.15 at
  `/usr/lib/postgresql/16/bin` (`ls /usr/lib/postgresql` shows `16 18`). The brief records both (`scratchpad/brief.md`
  section 4: 16.15 in its PostgreSQL line, and the 18.6 install by `scripts/env/install.sh` a few lines below). The real
  gap: the brief suggests data directories under `.local/` or the scratchpad, and `postgres` cannot reach either, because
  `/tmp/claude-0` is mode 0700 and owned by root.
- `initdb` and the server refuse root (`map-runtime.md` 6.3). The `postgres` user is uid 102, gid 104 (`id postgres`).
  `/tmp` is 1777 and `TMPDIR` is empty here. `scripts/env/start.sh` always starts RabbitMQ and assumes a non-root user
  (`map-runtime.md` 6.1, 6.3).
- Process identity. The workspace runs as uid 0 (`id -u`), and `$NETNS` runs `unshare --net` as root (`netns-run.sh:6`).
  Root bypasses file permission checks. `nobody` (65534) exists (`getent passwd nobody`). `[Educated Guess]` The CI runner
  is one non-root user with passwordless sudo, so a CI run cannot give two processes distinct uids without sudo.
- `github.com/lib/pq v1.12.3` (MIT) is in `chain/go.mod:129` as an indirect dependency and in svnd, because CometBFT's node
  imports it (`CMT/node/setup.go:40`). It has no dependency of its own (`PQ/go.mod`). Its `sslmode` default is `require`
  (`PQ/connector.go:304-306`). `pq.NewConfig` and `pq.NewConnector` merge `os.Environ()` (`PQ/connector.go:193-199,
  511-512,542-555`); the environment tags cover PGHOST, PGPASSWORD, PGPASSFILE, PGOPTIONS, PGSSLMODE, PGSERVICE,
  PGSERVICEFILE and more (`:242-431`), and some PG* variables make it fail (`:667-678`). `pq.NewConnectorConfig(cfg)` takes a
  ready `Config` and reads no environment (`:205-207`). `PQGO_DEBUG=1`, read at package init, prints `CONNECT <config>` and
  protocol traffic to stderr, outside any logger (`PQ/conn.go:68-73,253-264`). `requirements.lock` pins no PostgreSQL
  driver, ASGI server, bech32 or Cosmos client (`map-runtime.md` 4.1).
- `.gitignore:70` ignores `*.sql`; `.local/` (`:11`), `*.pem` and `*.key` (`:24-25`) are ignored.
- Repository contract. The goal 01 checker allows `.go` files only under `docs/protocol/`, `docs/evidence/goal-02/`,
  `apps/web/`, `chain/`, `proto/` and `agents/` (`docs/evidence/goal-01/check_requirements.py:184-194`); any other `.go`
  file is "application source present" (`:636-637`). A Go module under `services/` therefore fails `make contract` until
  `services/broadcaster/` is added to `SOURCE_ALLOW`. Goal 08-10 mutation runners each added one `PY_ALLOW` line to the
  same file (`:163-176`). The brief requires this checker to stay green (`scratchpad/brief.md` section 4).
- Go workspace modules: `agents/auditor`, `agents/provider`, `chain`, `docs/evidence/goal-02/go`, `proto`
  (`go.work:5-11`). Inside a workspace, a module that `go.work` does not list cannot be built without `GOWORK=off`.
  `scripts/run_tests.py:151` lists the Go modules the test runner covers, each run with a 30-minute `go test` timeout
  (`:152-158`); goal 10 measured `chain/devnet` at 922 s without `-race` and 1155 s with it
  (`docs/evidence/logs/goal-10/tests-go.txt:11`; `docs/evidence/logs/goal-10/race.txt:11`). `run_tests.py` runs without
  `-count=1` (`:163-166`); goal 10 captured it as `$NETNS env GOFLAGS=-count=1 .venv/bin/python scripts/run_tests.py --only
  go` (`docs/evidence/logs/goal-10/tests-go.txt:1`). The chain module may contain only `chain/cmd/svnd/main.go`
  (`tests/unit/test_repo_policy.py:48-50`). The SBOM writer reads every `go.sum` (`scripts/write_sbom.py:68`). The
  Makefile `build` target builds chain, `agents/provider` and `agents/auditor` only (`Makefile:67-73`).
- CI: every job at most 20 minutes (`scripts/workflow_policy.py:21`), four pinned actions (`:14-19`). go-race shards
  `app, devnet, devnet-evidence, devnet-upgrade, rest` (`.github/workflows/ci.yml:128`); the devnet shards use 12 or 15
  minute test timeouts (`:146-171`), prebuild their binaries, and the tagged builds `svn_rehearsal` and
  `svn_rehearsal_bad` are the precedent for fault binaries (`:162-171`). The `devnet` shard uses `-skip` with the union of
  the other devnet shards' `-run` filters (`:152`). `tests/unit/test_ci_devnet_coverage.py` fails unless every
  `chain/devnet` test runs exactly once (`:1-20`). Its fixtures assert exact strings: `MATRIX` and `DEVNET_SKIP`
  (`:539-543`, used by `text.count(old) == 1` at `:895`); the go step's env must be exactly `{SVN_SKIP_DEVNET: "1"}`
  (`:519`); the only allowed skip gate in `chain/devnet` is `if os.Getenv("SVN_SKIP_DEVNET") == "1" { t.Skip(` (`:89,
  531-536`). No CI job has PostgreSQL.
- Mutation runners. The goal 10 runner exports the whole tree into a new temp directory and uses a fresh GOCACHE unless
  `--gocache` is given (`docs/evidence/goal-10/mutants.py:7-8,64-68,2180,2219`); it selects killers by top-level test name
  (`:1993`). Goal 10 ran 303 mutants in 2141 s (`docs/evidence/logs/goal-10/mutants-goal10.txt:650`) with no
  validator-starting killer on the unmutated tree (`mutants.py:74-86`).
- `scripts/scan_secrets.py:25-62` flags `privkey`/`private_key` style names in assignments, camelCase `PrivKey =`/`:=`,
  quoted names and `--privkey` flags in any file.
- `Pdeathsig` is used nowhere in `chain` or `agents` (`grep -rn Pdeathsig`).
- Disk: 1.8 GB free on `/` (`df -h /`, 2026-11-08); the Go build cache is 10 GB (`du -sh /root/.cache/go-build`). Four
  CPUs; other work runs.

### 1.6 Prerequisites and inherited status

| Prerequisite (goal spec :8; `docs/progress.md:41`) | State on this tree | Effect on goal 11 |
| --- | --- | --- |
| Goal 04 environment | BLOCKED (`docs/progress.md:34`); local PostgreSQL and RabbitMQ checks were VERIFIED there | PostgreSQL runs here; record the version. RabbitMQ is not used (REQ-TECH-004 is goal 14, `docs/requirements.md:429`). |
| Goals 07, 08, 09, 10 | BLOCKED in their own status, merged into main (`docs/progress.md:37-40`) | Every goal 11 result is devnet-only, single machine. |
| "Signing boundaries from Goal 02" | Goal 02 is IMPLEMENTED (`docs/progress.md:32`); `docs/protocol.md:3` says it is not an approved protocol | Not approved. |
| "approved service/provider signing boundaries" | No such document. Gate A accepted "the proposed signing domains" only (`docs/decision-register.md:23`). | BLOCKED until the owner approves a pinned boundary document (Q1, DR-11-01). |
| "DEC-001 blocks host custody of user keys" | DEC-001 default approved; on chain only `DEVNET_TEST_ONLY` intents pass (`docs/protocol.md:140`) | Production content registration stays `DECISION_BLOCKED`. Goal 11 holds no creator key. |

### 1.7 Discrepancies found while planning (report; do not edit)

- `map-chain.md` 8.1 cites `agents/auditor/chainclient/rpc.go:200-497`. The file has 397 lines; section 1.3 gives the right
  ranges.
- `chain/app/app.go` lines after 478 moved by PR 15. `RegisterTxService` is at `:691-694`.
- `buildAuditor` is at `chain/devnet/audit_test.go:220`, not `:218`.
- `docs/protocol.md:129` says the fixture `down` set does not produce `ERROR_CODE_QUORUM`, while `cases:76` returns it
  from the reference outbox. Goal 11 records the difference as DR-11-19 and tests the reference's observable state.
- `docs/evidence/goal-04.md:286` says goal 12 is the first goal to open PostgreSQL for application queries; goal 11 is.
- `docs/threat-model.md:18` says a host-held key exists "only if DEC-001 is later accepted"; DEC-001 is approved
  (`map-signing.md` section 9).
- `docs/protocol.md:233-234,240` (relay column) versus the code (section 1.4): already recorded deviations.
- Revision 1 errors corrected in this revision: the `tx.acc_seq` rule (1.1); "8 attempts per business id" was called an
  auditor precedent and is not one (8.7); revision 1 said the brief lacks PostgreSQL 18.6 (1.5); revision 1 named goal 27
  as the owner of mTLS (16); revision 1 filed the DR-11-06 inference under the owner rulings (header).

---------------------------------------------------------------------------------------------------

## 2. Scope, non-scope, and what goal 11 must not do

### 2.1 In scope (goal spec :18-32)

- A broadcaster service at `services/broadcaster`: typed requests, a PostgreSQL transactional outbox, a per-signer
  submission engine, reconciliation, a caller API with authentication and object permissions, a development signing
  boundary, explicit chain id and gateway configuration, redacted logs.
- The single-transaction coupling of application intent and outbox insertion, shown with a fixture application schema in
  the same database (section 6.3), and driven to the real chain through the SQL path (13.4 phases H and UA).
- Real devnet acceptance tests for every acceptance check (goal spec :35-40), with real PostgreSQL, real process kills,
  build-tagged fault points and a loopback fault proxy.
- Handoff: failure traces, outbox schema, service API and signing-boundary documentation; stable identities and canonical
  commitment evidence for goal 12 (goal spec :43-44).

### 2.2 Not in scope (not to be claimed)

- Any production path: content registration off the devnet is `DECISION_BLOCKED` (DEC-001, DR-07-01); offers and renewals
  need the DEC-005 devnet test number (`chain/x/leases/README.md:124-125`).
- The API service (goal 13), the creator UI states (goal 21), the provider agent (goal 16), the indexer (goal 12), key
  rotation and backup drills (goal 26), production packaging and certificate issuance (goal 27), load numbers (goal 28).
- Application projections. `[Educated Guess]` The blueprint's "then updates the application projection"
  (`docs/blueprint.md:305`) is read as: the broadcaster updates intent status and commitment evidence only; application
  projections come from the goal 12 indexer (`docs/blueprint.md:387`; `docs/goals/12-committed-state-indexer-and-reconciliation.md:19-21`).
  DR-11-33 records the reading.
- Provider registry messages, council messages and `authority_end`: not routed (DR-11-12). Audit batches: not routed under
  the recommended Q2 answer (DR-11-06 stays open until the owner answers).
- Read proofs: reads trust one configured node, disclosed (DR-11-08).
- Independent operators; every devnet result is one operator, one machine, four validator processes.

### 2.3 What goal 11 must NOT do (any of these is a stop-and-report)

1. Hold or request a user, creator, provider, seat, AUTHORITY, AUDITOR or validator key. The broadcaster loads only the
   keys Q1 allows; start-up refuses any other role (section 7.1).
2. Synthesize provider consent. No broadcaster code path signs in an `SVN-PROVIDER-*` or `SVN-HOSTING-INTENT-1` domain; the
   signer signs only the outer `SIGN_MODE_DIRECT` envelope (section 7.2).
3. Report COMMITTED from a broadcast acknowledgement, a mempool answer, a timer, or the broadcaster's own database. Only a
   committed result read from the chain moves a row to COMMITTED (section 9.4).
4. Create final leases or any projection in the database. The broadcaster has no lease or content state table; goal 12
   builds projections from committed events (goal spec :28-29; `docs/blueprint.md:387`).
5. Require a live credential, an external service or an undocumented network dependency for the devnet. All listeners are
   loopback or Unix sockets; all keys and certificates are generated at run time (goal spec :31-32).
6. Invent a policy number. Retries, deadlines, expiry, caps and stall windows are PROPOSED configuration with hard code
   bounds (DR-11-10, table in section 11); retention stays unset (DEC-004, `docs/decision-register.md:127`).
7. Change `.proto`, `bounds.json`, `docs/protocol.md`, `docs/evidence/goal-02/**`, `docs/goals/**`,
   `docs/decision-register.md`, `docs/threat-model.md`, or any chain production code. Change `docs/evidence/goal-0*/**`
   only by exactly two edits to `docs/evidence/goal-01/check_requirements.py`: the `SOURCE_ALLOW` entry and its comment
   (INT) and the goal 11 `PY_ALLOW` line (B10). Goal 11 adds chain tests only (section 13.3).
8. Use raw `/store` queries, `/check_tx`, unordered transactions, or a multi-message transaction of unrelated intents.
9. Merge, push, open a PR, or change branch protection (orchestrator only, with owner approval).
10. Weaken or delete a test to get green. The scaffold test and the scaffold path pin are replaced by design and recorded,
    and the constant strings of `tests/unit/test_ci_devnet_coverage.py` change with the CI matrix (section 12).
11. Claim an audit, production readiness, custody, operator independence, or a uid boundary that did not run.
12. Create repository copies, a fresh GOCACHE, or run two devnet or PostgreSQL test runs at once, without the
    orchestrator's instruction (`scratchpad/brief.md` section 0).

---------------------------------------------------------------------------------------------------

## 3. Language and runtime choice

Proposal: a Go module at `services/broadcaster` (DR-11-20, reversible; the orchestrator tells the owner in the same message
as Q1-Q4).

| Criterion | Go service | Python service |
| --- | --- | --- |
| Blueprint | "operator-authorized Go broadcaster" (`docs/blueprint.md:373`) | Python row covers accounts, policies, social, feed, moderation (`docs/blueprint.md:203`) |
| Signing | SDK `tx.SignWithPrivKey`, as the auditor does (`agents/auditor/chainclient/tx.go:67`) | hand-built SIGN_MODE_DIRECT, pubkey `Any`, low-S R\|\|S, bech32, RIPEMD-160: new crypto-adjacent code ("unreviewed cryptography", AGENTS.md) |
| Decoder parity | the chain's own `app.MakeEncoding` (`chain/app/encoding.go:44-81`) | must be proven by cross-language vectors |
| Typed results and preimages | `msgkit.ParseCommitted`, `SVNCode`, `leases.*SignBytes`, `content.*IntentSignBytes`, `msgkit.VerifyInner` | reimplemented |
| New dependencies | none: lib/pq is already in `chain/go.sum` and svnd | ASGI server, PostgreSQL driver, migration tool, cosmos protos, bech32 (`map-runtime.md` 4.1-4.2) |
| Devnet harness | built and driven from `chain/devnet` like the auditor (`chain/devnet/audit_test.go:220`) | a new cross-language harness |
| Repository contract | needs `services/broadcaster/` in the goal 01 `SOURCE_ALLOW` (`check_requirements.py:184-194`): a goal 01 contract change this choice causes, disclosed with Q1-Q4 | `services/` is already in `PY_ALLOW` (`:179`) |

Python services (goal 13 onward) never load keys; they reach the broadcaster through its HTTP API or the granted SQL
enqueue function (section 6.3). PostgreSQL 18.6 runs the outbox; the SQL avoids 18-only features such as `uuidv7()` so a
16.x server still works (`map-runtime.md` 6.2). RabbitMQ is not used: the outbox is the queue, and publishing to a broker
as well would reintroduce the dual write the outbox removes (`docs/blueprint.md:385`).

---------------------------------------------------------------------------------------------------

## 4. Architecture, process identities and file layout

### 4.1 Processes (development layout; one binary, three roles)

```
caller (test harness now; goal 13 API later)
   | mTLS loopback HTTP  (or the granted SQL function, in the caller's own DB transaction)
   v
svn-broadcaster serve   (uid U_serve)  -- API, validation, outbox writes, per-signer workers, reconciler, observer
   | Unix socket, SO_PEERCRED uid == U_serve           | lib/pq, Unix socket, explicit pq.Config
   v                                                    v
svn-broadcaster signer  (uid U_signer) -- key dir 0700  PostgreSQL 18.6 (schema broadcaster), run as `postgres`
                          owned by U_signer; allowlist,
                          intent-key check, timeout bound, outer signing only
   |                                                    
   +--> /status of the configured node (loopback), for its own timeout bound only

serve --> CometBFT RPC of ONE configured node (loopback): status, abci_query, broadcast_tx_sync, tx, tx_search, commit

intent-key registry file (public keys only): owned by the operator uid, mode 0644, writable by neither U_serve nor U_signer
```

- Uid rule (DR-11-35, reversible). When the launcher's euid is 0 (this workspace, `$NETNS`), it starts `signer` and `serve`
  under two distinct non-root uids with `SysProcAttr.Credential` and an empty supplementary group list, the mechanism
  `internal/pgdev` uses for `postgres`. The devnet test picks two numeric uids that `getent passwd` does not list and checks
  that no file is owned by them before use; it prints `boundary_uids=signer:<u1>,serve:<u2>`. The key directory is 0700 and
  owned by U_signer. The signer accepts a peer only when its SO_PEERCRED uid equals the configured U_serve, and refuses to
  start when U_serve equals its own uid while distinct uids are configured.
- Same-uid fallback. When the launcher is not root (the CI runner), both processes run as one uid and the run prints
  `boundary_uids=same:<u>`. Then the process split separates key bytes in memory only; a compromised `serve` can read the
  key files or trace the signer. Requirement 5's boundary row is VERIFIED only from a logged root run with distinct uids;
  a same-uid run counts as IMPLEMENTED (section 14). Q1 and `docs/architecture/broadcaster.md` state this limit.
- The signer process exists only under Q1 option (a) (section 7.2). Under Q1 (b) the keyring is a package inside `serve`.
  Under Q1 (c) only the `BROADCASTER` key exists and the service kinds are not built.

### 4.2 Module and packages (names are proposals)

```
services/broadcaster/
  go.mod, go.sum                module github.com/Maxencejules/shortvideo-network/services/broadcaster
                                replace .../chain => ../../chain, .../protocol => ../../proto (as agents/auditor/go.mod);
                                every direct requirement pinned to the exact chain/go.mod version; frozen after
                                Wave 0 step 0 (section 4.5)                                                   [B1]
  doc.go                        package doc only (Wave 0 step 0, so the module builds)                        [B1]
  README.md                     rewritten: service API, states, outbox schema, signing boundary summary      [B5]
  cmd/svn-broadcaster/main.go   subcommands: serve | signer | migrate | reconcile-once | status | signer-clear |
                                dev-ca | dev-db (dev-db requires a `shortvideo-dev-` chain id, docs/protocol.md:136,
                                and a data dir under os.TempDir()/svn-pg-*)                                   [B7]
  internal/config/              typed config, hard bounds on every PROPOSED number, explicit chain id and rpc url,
                                explicit pq.Config, PG*/PQGO_DEBUG refusal                                    [B7]
  internal/store/               database/sql + lib/pq (NewConnectorConfig); migrations runner; every query;
                                roles; compare-and-set helpers                                               [B1]
  internal/store/migrations/    0001_outbox.pgsql ... (not *.sql: .gitignore:70)                              [B1]
  internal/pgdev/               disposable cluster: initdb, postgres as a direct child (Pdeathsig, own process
                                group), privilege drop, fast stop; no crash hooks; refuses data dirs outside
                                os.TempDir()/svn-pg-*                                                         [B1]
  internal/chain/               generalised from agents/auditor/chainclient: BuildTx(sdk.Msg, timeout_height),
                                Decode, RPC (Status, ABCIQuery, Simulate, BroadcastTxSync, Tx, TxSearch, Commit),
                                height-bound Account, Provider, Content, ActiveLeases, Deadlines, Governance queries,
                                answer classifier, acc_seq reader, committed-effect comparison per kind      [B2]
  internal/outbox/              pure state machine: states, transitions, dead-attempt rule, reconcile decision
                                over typed observations; no I/O                                               [B3]
  internal/keys/                key file loader (symlink, mode, owner, format checks, as agents/auditor/keys)  [B6]
  internal/signer/              signer process and its client; per-key type-URL allowlist; intent-key and
                                timeout checks; outer signing only                                            [B6]
  internal/policy/              kind -> signer class -> type URL; caller kinds; object ownership; provider
                                signature pre-check; publication permission; intent-key registry reader      [B6]
  internal/worker/              per-signer loop, advisory-lock fencing, compare-and-set persist, sends_started,
                                deadlines, backoff, reconcile, observer and stall episodes                    [B8]
  internal/faultpoint/          build tag svn_faultpoint: named points that write a marker file and block;
                                the default build compiles them to no-ops with no symbol                      [B8]
  internal/api/                 net/http handlers, mTLS principal, strict JSON, status views; one server
                                constructor used by main and by tests                                        [B7]
  internal/logx/                slog JSON handler with a redacting ReplaceAttr                                [B6]
  internal/pgtest/              test-only: pgdev plus crash hooks (immediate stop, SIGKILL of the postmaster),
                                re-executed helper processes; never linked into the binary                   [B1]
docs/architecture/broadcaster.md   signing boundary (PROPOSED; approval asked at PR time, pinned), states,
                                   schema, API, residual risks                                                [B5]
chain/app/tx_deadline_test.go      timeout_height, acc_seq and simulate semantics through FinalizeBlock (tests only) [B4]
chain/devnet/broadcaster_test.go   TestDevnetBroadcasterLifecycle, TestDevnetBroadcasterQuorumStall, the loopback
                                   fault proxy and the per-phase accounting                                   [B9]
tests/unit/test_chain_writer_policy.py   REQ-INT-001 static policy with planted-violation fixtures            [INT]
tests/unit/test_ci_broadcaster.py        CI guard: PostgreSQL installed in every job that runs the module's tests,
                                         no PostgreSQL skip variable, no phase-selection variable in ci.yml   [INT]
docs/evidence/goal-11/mutants.py   mutation runner (design of goals 08-10, killers listed per mutant)         [B10]
```

### 4.3 Dependency direction

- `services/broadcaster -> {chain/app (MakeEncoding), chain/msgkit (ParseCommitted, VerifyInner, ids), chain/genesis,
  chain/address, chain/x/leases (AcceptSignBytes, RenewSignBytes, EndSignBytes), chain/x/content (RegisterIntentSignBytes,
  WithdrawIntentSignBytes), protocol types, SDK, CometBFT RPC client, lib/pq}`. The chain module never imports the
  services module.
- Exported preimage builders and verifiers only. A local copy of a signed preimage is unreviewed crypto-adjacent code and
  is forbidden; a static test fails on any `SVN-PROVIDER-` or `SVN-HOSTING-INTENT-` string literal in the module (13.6).
- `chain/devnet/broadcaster_test.go` builds the broadcaster binary into a temp directory, as `buildAuditor` does
  (`chain/devnet/audit_test.go:220-236`), with `-race` when the test binary is race-enabled (a race-tagged constant), and
  with `-tags svn_faultpoint` for the fault phases. It starts PostgreSQL through `svn-broadcaster dev-db`. Crash hooks
  (`pg_ctl -m immediate`, SIGKILL of the postmaster from its pid file) are test code in `chain/devnet` and
  `internal/pgtest`, never in the shipped binary (DR-11-24).
- `internal/outbox` imports nothing but the standard library and typed observation structs, so its property tests need no
  database or chain.

### 4.4 Shared client package

No shared package now (DR-11-21). `internal/chain` is copied and generalised from the auditor client; the auditor stays
untouched because goal 09 mutants anchor on it (section 1.3). When goal 16 needs provider-side signing (path P2), extract a
shared package and record it then.

### 4.5 Module pinning (Wave 0 step 0, serial)

1. INT adds `./services/broadcaster` to `go.work` and `services/broadcaster/` to `SOURCE_ALLOW` (with its comment).
2. B1 creates `go.mod` with every direct requirement pinned to the exact `chain/go.mod` version (cometbft v0.40.0,
   cosmos-sdk v0.55.0, lib/pq v1.12.3, gogoproto and protobuf as in chain), the two replace directives, a `doc.go`, and a
   complete `go.sum` resolved with the module cache only (`GOPROXY=off`).
3. Evidence for the step: `go list -m all` in `chain` before and after is byte-identical; `go version -m` of an svnd build
   before and after lists the same dependencies; `check_requirements.py` passes; `go build ./services/broadcaster/...`
   passes. If any check differs, stop and report.
4. Then `go.mod` and `go.sum` are frozen. Another package that needs an import outside the pinned set asks the
   orchestrator, and B1 makes the change. `GOWORK` stays on; nobody builds with `GOWORK=off`.

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
| `FORWARD_AUDIT_BATCH` (only under Q2 (c), not recommended) | an `AUDITOR`-signed `MsgSubmitAuditBatch` transaction, verbatim | the auditor's `AUDITOR` account | raw tx bytes | nothing; no sequence allocated |

No kind exists for provider registry messages, council messages or `authority_end` (DR-11-12). An unknown kind is 422 and
writes nothing.

### 5.2 Endpoints (HTTP/1.1 JSON over mTLS on loopback)

| Method and path | Effect | Errors |
| --- | --- | --- |
| `POST /v1/intents` | validate, then insert intent and outbox rows in one DB transaction; 201 new, 200 existing (same idempotency key, same request digest) | 400 malformed, 401 no client certificate, 403 kind not allowed or permission missing, 404 object not visible to the caller, 409 same key with another digest or `CONTENT_ID_UNAVAILABLE`, 413 body above the cap, 422 invalid request or provider signature, 429 open-intent cap, 503 database unavailable |
| `GET /v1/intents/{intent_id}` | status view (5.8) | 404 unknown or another caller's |
| `GET /v1/intents?status=&page_token=&limit=` | the caller's own intents, at most 100 per page (PROPOSED) | 400 |
| `POST /v1/intents/{intent_id}/cancel` | compare-and-set, only while REQUESTED with no attempt ever persisted: FAILED(`CANCELLED`) (T3) | 409 once an attempt exists (signed bytes cannot be unsent) |
| `GET /v1/signers` | public signer addresses and public keys per class, so an account host can name `submittingHost` in an intent (`chain/x/content/intent.go:20-22`) | none |
| `GET /v1/health` | chain id, latest height and time seen, observer freshness, stall flag, database reachability; no secrets | none |

`CONTENT_ID_UNAVAILABLE` is one uniform answer for a content id that is pending or effective for another caller, held by
another host on chain, or void after a failed register by another caller. It never says which, so it does not reveal
another caller's object (DR-11-34).

### 5.3 Request and response types

- Go structs per kind in `internal/api`, decoded with `json.Decoder.DisallowUnknownFields` and a body cap (PROPOSED 65,536
  bytes, equal to `genesis.MaxTxBytes`, `chain/genesis/bounds.go:29-31`). Ids are checked against the chain id pattern
  `^[a-z0-9][a-z0-9._-]{0,63}$` (`chain/msgkit/ids.go:29-44`); digests are 32 bytes; integers are range-checked against the
  chain bounds before any insert. The typed request is converted to the protocol message and re-encoded with the app codec;
  the stored `request` column is that protobuf, so the worker never re-parses caller JSON.
- No new `.proto` file (DR-11-30): the request and response bodies are JSON documented in the README; the status field
  reuses the frozen `OperationStatus` names.

### 5.4 Idempotency keys and the request digest

- Every create carries `idempotency_key` (caller-chosen, same id pattern). `UNIQUE(caller_principal, idempotency_key)` in
  PostgreSQL decides.
- HTTP path digest: SHA-256 over a length-prefixed framing of (kind name; the deterministic protobuf encoding of the request
  message with `signer` and `business_id` empty, from the app codec; the auth context re-serialized from its typed struct
  with `permissions` sorted and deduplicated). JSON key order, whitespace and number spelling do not change it.
- SQL path digest: the request is stored as `jsonb`, whose text form has a deterministic key order and no insignificant
  whitespace, so the SQL-level digest `sha256(convert_to(request_json::text, 'UTF8'))` ignores key order. The worker later
  decodes the stored JSON into the typed struct, computes the HTTP-path digest, and stores it next to the SQL digest.
- Same key and digest returns the existing intent; another digest is 409. A test sends the same request with reordered keys
  on both paths and expects 200 (m9).
- The idempotency key is the caller's dedupe; the business id is the chain's (8.2). Two different keys with the same chain
  effect are two intents; the chain keeps one effect and the second intent reports it honestly (DR-11-28).

### 5.5 Authorized request context (under Q4 option (a))

- Allowed fields only: `principal_kind` (`DEV_FIXTURE` in goal 11), `principal_id` (opaque, id pattern), `permissions` (enum
  list: `PUBLISH_CONTENT`, `WITHDRAW_CONTENT`, `SCHEDULE_HOSTING`, `RELAY_PROVIDER_SIGNED`), `policy_ref` (id pattern). The
  server adds `caller_principal` (from the certificate or the SQL role) and `request_digest`.
- Refused: any other key, any email, IP address, user agent, token, cookie, header copy or free text. A test sends each and
  expects 400 with no row. Opaque ids and digests are still linkable (REQ-OFF-004); the README says so.
- Stored as a bounded `jsonb` (PROPOSED cap 2,048 bytes); never logged; returned to its own caller only as a digest.
- Every stored column that is person-linked or a reusable authorization is listed in Q4 with its purpose, readers and
  returned form.

### 5.6 Authentication of internal endpoints (DR-11-32, reversible; mTLS proposed)

- HTTP: TLS 1.3, `ClientAuth: tls.RequireAndVerifyClientCert`, a development CA generated at run time by
  `svn-broadcaster dev-ca` into a 0700 directory under `.local/` (ignored, `.gitignore:11`) or a test temp dir. The
  principal is the client certificate's single URI SAN `svn://principal/<id>`; a certificate without exactly one such SAN
  is refused. The listener binds loopback only; a non-loopback address is a config error. Unit tests build the server with
  the same constructor `main` uses, and the devnet test drives the running binary (13.4 phase UA).
- SQL path: each caller service connects with its own PostgreSQL role (scram-sha-256); `broadcaster.callers` maps the role
  name to the principal (section 6.3). The table is written only with the owner role by `svn-broadcaster migrate --callers`.
- Development principals refuse to load when the configured chain id lacks the `shortvideo-dev-` prefix
  (`docs/protocol.md:136`), so a fixture cannot enable access on another chain (goal 13 rule,
  `docs/goals/13-authenticated-uploads-and-quarantine.md:17-19`).
- Alternatives kept open in DR-11-32: a Unix socket with `SO_PEERCRED`, or per-caller bearer tokens. Production
  certificate issuance and rotation are goal 27.

### 5.7 Object permissions (DR-11-34, fail-closed)

- `callers` config: principal -> allowed kinds, open-intent cap, and optional bindings (`provider_id` for a provider-agent
  principal). Loaded at start; changing it is a restart. Start-up refuses a configuration whose per-caller caps sum above
  the hard global limit (section 11), so the global number of open intents is bounded without a global lock.
- Read, list, cancel: only intents whose `caller_principal` equals the requester; another caller's intent answers 404 so
  ids cannot be probed.
- `REGISTER_CONTENT`: the auth context grants `PUBLISH_CONTENT` to `principal_id`, and the publication permission check of
  7.4 passes. The enqueue transaction inserts `content_owners(content_id, version, caller, principal_id, intent_id)` with
  status `PENDING`. A partial unique index allows one non-`VOID` row per (content_id, version); a conflict is
  `CONTENT_ID_UNAVAILABLE`.
  - The row becomes `EFFECTIVE` only when the REGISTER intent is COMMITTED (9.4, signer equal to the pinned service
    address) and `Query/Content` shows `submitting_host` equal to that address.
  - A FAILED register turns its row `VOID`. A `VOID` row is kept as history and may be replaced by a new `PENDING` row.
- `WITHDRAW_CONTENT`: permission `WITHDRAW_CONTENT`, an `EFFECTIVE` row with the same caller and principal, and
  `Query/Content` showing our service address as `submitting_host`, at enqueue and again before signing; otherwise a uniform
  404.
- `OFFER_LEASE`: `SCHEDULE_HOSTING`; an `EFFECTIVE` row of this caller, the on-chain host match, and content not withdrawn
  (`Query/Content`), at enqueue and again before signing.
- Relay kinds: `RELAY_PROVIDER_SIGNED`, plus either the caller is bound to that provider id, or the caller owns an
  `OFFER_LEASE` intent for that offer that is COMMITTED with our service address as the committing signer (9.4). The
  provider signature pre-check of 7.3 always runs.
- Per-caller open-intent counter rows are updated in the enqueue transaction under `SET LOCAL lock_timeout` and `SET LOCAL
  statement_timeout` (PROPOSED 2 s and 5 s); a timeout is 503 and writes nothing. No global row is locked, so one slow
  caller transaction cannot block other callers or the worker (m8).

### 5.8 Status exposed to callers (DR-11-09: no protocol change)

```
{ "intent_id", "kind", "status": "OPERATION_STATUS_REQUESTED|SUBMITTED|COMMITTED|FAILED|PENDING_STALL",
  "detail": { "phase": "QUEUED|RETRY_WAIT|SIGNED|ACKNOWLEDGED|UNCERTAIN|AWAITING_DEADLINE",
              "reason", "attempts", "last_ack": {"code","codespace"},
              "last_landing_height",         // timeout_height of the live attempt, if any
              "observer": "FRESH|OBSERVER_UNAVAILABLE",
              "stall": {"since_height","since_time"} },   // only while a stall episode is open
  "commitment": { ... section 9.5 ... },           // only when COMMITTED
  "failure": { "reason", "code", "codespace" },    // only when FAILED
  "expires_at", "created_at" }
```

- "Uncertain" is `SUBMITTED` with `detail.phase = UNCERTAIN`. "Pending" is `PENDING_STALL` while a stall episode is open
  and the observer is fresh (section 10). Adding an `UNCERTAIN` enum value would change the frozen `records.proto`, so it is
  recorded as a protocol addition (DR-11-40), not made.
- `last_landing_height` is the last height at which a live attempt can still commit. `expires_at` bounds new attempts and
  sends only; it does not bound when an effect can land (m5).

---------------------------------------------------------------------------------------------------

## 6. Outbox schema and the one-transaction coupling

### 6.1 Roles and schema

- Schema `broadcaster`. Roles:
  - `svn_broadcaster_owner`: owns the schema, runs migrations and the operator commands that write `callers`.
  - `svn_broadcaster` (runtime): DML on the outbox tables, SELECT only on `callers` and `schema_migrations`, no DDL.
  - One role per caller service for the SQL path: EXECUTE on the enqueue function, SELECT on a per-caller view only.
  - The cluster superuser exists only in the dev cluster bootstrap and is never used at run time. Goal 04 created only a
    superuser (`docs/evidence/goal-04.md:286`); goal 11 adds the non-superuser roles.
- The devnet test's own database role is SELECT-only on every table; it never writes history (13.4).
- No intent-key table: the intent-key registry is an operator file (7.4).
- Every query is parameterized (TM-INJECTION, REQ-SEC-009); `search_path` is fixed on every function.

### 6.2 Tables (every text, bytea and jsonb column has a length `CHECK`)

| Table | Columns (main) | Constraints and notes |
| --- | --- | --- |
| `schema_migrations` | version PK, name, sha256, applied_at | runner refuses a changed checksum or a gap |
| `callers` | role_name PK, principal UNIQUE | SQL-path mapping; written by the owner role only |
| `signers` | address bytea(20) PK, class (`SERVICE`, `BROADCASTER`), key_name, account_number, next_sequence, observed_height, lock_key UNIQUE, halted_reason | row lock serialises sequence allocation; `halted_reason` set on a signer conflict (DR-11-15) |
| `signer_events` | id, signer_address, sequence, foreign_tx_hash, detected_at, cleared_at, cleared_by_command | one row per foreign use of a signer sequence; `signer-clear` sets `cleared_*`; phase Z subtracts recorded foreign uses |
| `intents` | intent_id uuid PK, caller_principal, idempotency_key, request_digest bytea(32), sql_digest bytea(32) NULL, kind, request bytea, request_json jsonb NULL (SQL path), auth_context jsonb, validated bool, chain_id, signer_address FK, business_id, business_attempt smallint, state, phase, reason, created_at, expires_at, next_attempt_at, attempt_count, final_at, committed_tx_hash, own_transaction bool, transitions_suppressed int | `UNIQUE(caller_principal, idempotency_key)`; `CHECK(expires_at > created_at)`; partial index on non-final state and `next_attempt_at`; `chain_id` must equal the configured chain id; trigger `intents_terminal_immutable` refuses any UPDATE or DELETE of a row whose stored state is COMMITTED or FAILED |
| `business_attempts` | intent_id, n, business_id UNIQUE, created_at, outcome | a new business id only after reconciliation shows no effect (8.6); n bounded |
| `attempts` | attempt_id uuid PK, intent_id FK, n smallint, signer_address, account_number, sequence, timeout_height, gas, tx_bytes bytea, tx_hash bytea(32) UNIQUE, created_at, phase, sends_started smallint, sends_recorded smallint, unknown_sends smallint, cache_answers smallint, counted_refusals smallint, acked bool, ack_code, ack_codespace, ack_log (clipped), committed_height, committed_index, committed_code, committed_codespace, acc_seq_present bool, gas_used, dead_reason, dead_at, worker_instance | partial `UNIQUE(signer_address, sequence) WHERE dead_at IS NULL AND committed_height IS NULL` (at most one live transaction per signer sequence); `CHECK(timeout_height > 0)`; `CHECK(sends_recorded <= sends_started)`; `n < 16` |
| `commitments` | intent_id PK, chain_id, height, block_hash, tx_hash, tx_index, msg_index, code, codespace, gas_wanted, gas_used, events jsonb, idempotent_replay bool, first_commit_tx_hash, committing_signer bytea(20) | written once, only from a committed result (9.5) |
| `content_owners` | id PK, content_id, content_version, caller_principal, principal_id, signer_address, intent_id, status (`PENDING`, `EFFECTIVE`, `VOID`), status_at | partial `UNIQUE(content_id, content_version) WHERE status <> 'VOID'`; read by withdraw, offer and relay checks |
| `transitions` | id bigserial, intent_id, attempt_id, at, from_state, to_state, phase, reason, observation jsonb (bounded), worker_instance | append-only (a trigger refuses UPDATE and DELETE); a row is appended only on a change of state, phase or observation class; at most a PROPOSED 256 rows per intent, after which only `intents.transitions_suppressed` grows (m11) |
| `stall_episodes` | id, chain_id, reason (`HEIGHT_STALLED`, `NODE_UNREACHABLE`, `CATCHING_UP`), started_height, started_at, ended_height, ended_at | one open episode at a time; written by the observer (section 10) |
| `chain_health` | chain_id PK, latest_height, latest_time, seen_at, observer_instance | updated by the observer every poll; freshness decides between the stall overlay and `OBSERVER_UNAVAILABLE` |

- `worker_instance` is a boot nonce plus pid, written on every attempt and transition, so a test can tell which process
  wrote a row (13.4 phase CS).
- Every state UPDATE is a compare-and-set on the expected (state, phase) and checks the row count; a zero count is a lost
  race and is handled, never ignored.
- Retention: no column is deleted automatically; retention is unset (DEC-004; `docs/protocol.md:272`). The Q4 minimization
  option, if chosen, clears named columns of named terminal rows and keeps their digests.

### 6.3 Enqueue in one database transaction

- HTTP path: `POST /v1/intents` runs validation (7.3, 7.4, 5.7), then one transaction: insert into `intents` (`ON CONFLICT
  (caller_principal, idempotency_key) DO NOTHING RETURNING`); on no row, read the existing digest (same -> 200, other ->
  409); update the per-caller counter row under the local timeouts of 5.7; insert `business_attempts` n=1 and any
  `content_owners` row; insert the first `transitions` row; `NOTIFY broadcaster_work` (delivered at commit only); commit.
- SQL path (goal 13+ services, which write their own application row in the same transaction): `SELECT
  broadcaster.enqueue_intent(kind, request_json, auth_context, idempotency_key, expires_in)` is `SECURITY DEFINER`, has a
  fixed `search_path`, `REVOKE ALL FROM PUBLIC`, and takes the caller principal from `session_user` via `callers`. It performs
  the size, kind and per-caller cap checks in SQL and inserts the row with `phase = QUEUED, validated = false`. Its
  `RAISE` messages carry error codes only, never argument values (m7). The worker runs the full validation before any
  signing and fails the intent with `UNAUTHORIZED` or `INVALID` and no attempt if it does not pass (T2).
- The worker re-runs validation immediately before signing on both paths, because chain state can change after enqueue.
- Coupling proof (13.2): a test-only fixture schema `app_fixture` writes its own row and calls the enqueue function in one
  transaction; commit leaves both, rollback leaves neither, and SIGKILL of a re-executed client process mid-transaction
  leaves neither. Phase H drives the same path to the real chain.

### 6.4 Migrations

- Embedded `internal/store/migrations/NNNN_name.pgsql`, applied in order, each in its own transaction under
  `pg_advisory_xact_lock(<constant>)`, recorded with sha256; forward-only (DR-11-23). `svn-broadcaster migrate` runs with the
  owner role; `serve` refuses to start unless the schema version equals the binary's.

### 6.5 PostgreSQL settings that keep secrets out of server logs (DR-11-37)

- `internal/pgdev` starts the dev cluster with `log_min_error_statement=panic`, `log_statement=none` and
  `log_parameter_max_length_on_error=0`. PostgreSQL's default `log_min_error_statement=error` would log the text of a
  failing statement, including a caller's literal `enqueue_intent(...)` arguments (m7). The README asks operators to use the
  same settings; goal 11 checks them only on the dev cluster.
- The log canary (13.4 phase L) also covers SQL-path errors raised with literal SQL: cap reached, idempotency conflict, bad
  JSON.

---------------------------------------------------------------------------------------------------

## 7. Signing boundary (documented in `docs/architecture/broadcaster.md`, PROPOSED)

### 7.1 Key classes and per-key allowlists (Q1 recommended option (a))

| Key | Role read from `Query/Governance` at start-up | May sign (type URL allowlist, checked in policy and again in the signer) |
| --- | --- | --- |
| `service` (devnet) | `ADMITTED_SERVICE` | `/svn.content.v1.MsgRegisterContent`, `/svn.content.v1.MsgWithdrawContent`, `/svn.leases.v1.MsgOfferLease` |
| `broadcaster` (devnet) | `BROADCASTER` | `/svn.leases.v1.MsgAcceptLease`, `/svn.leases.v1.MsgRenewLease`, `/svn.leases.v1.MsgEndLease` with `authority_end = false` |

- Start-up refuses: a key whose address has any other role, no role, or is a council seat; two configured keys with one
  address; a key file that is a symlink, not regular, group- or world-accessible, not owned by the signer uid, or not the
  devnet hex format (the auditor loader rules, `agents/auditor/keys/keys.go:1-21`); a chain id outside `shortvideo-dev-`
  while dev keys are configured.
- One message per transaction (DR-11-14): multi-message transactions are atomic, so batching unrelated intents couples
  their failures (`map-chain.md` 6).
- The signer for an intent is fixed when the intent is created and never changes, because an offer replay by another
  scheduler is `FORBIDDEN` (D-11) and content is owned by its `submitting_host` (`chain/x/leases/README.md:105-110`;
  `chain/x/content/README.md:35-37`).

### 7.2 Development signing boundary

Q1 option (a), recommended:

- Process and identity. `svn-broadcaster signer` alone reads the key files. It runs as U_signer, distinct from U_serve when
  the launcher is root (4.1). It listens on a Unix socket in a directory owned by U_signer (mode 0711; socket mode 0666) and
  accepts a connection only when `SO_PEERCRED` (`syscall.GetsockoptUcred`) reports the configured U_serve.
- Request: key name, chain id, account number, sequence, gas, timeout height, one message as `Any`.
- Checks, all in the signer, in this order: decode the message with the app interface registry; type URL in the key's
  allowlist; the message's `signer` equals the key address; `authority_end = false` for `MsgEndLease`; chain id equals its
  own config; gas within bounds; `timeout_height` not 0 and not above the signer's own observed height plus K_max (the
  signer reads `/status` of the configured node over loopback with the hardened client, at most once per poll interval,
  and refuses while it has no fresh height) (M3).
- Extra checks for `MsgRegisterContent` and `MsgWithdrawContent`: the `hosting_intent` public key is in the operator
  intent-key registry (7.4), and the intent signature verifies over `content.RegisterIntentSignBytes` or
  `content.WithdrawIntentSignBytes` (`chain/x/content/intent.go:83,98`) built from the message, with the signer's own
  address as submitting host and role `DEVNET_TEST_ONLY`. A key the registry does not list is refused (M3).
- Then it builds and signs the outer transaction with `tx.SignWithPrivKey` (`SIGN_MODE_DIRECT`), re-decodes it with the
  strict decoder, and returns bytes and hash. It never signs anything else: no inner-signature domain, no arbitrary bytes.
  It keeps no state, so its death mid-request leaves no attempt and consumes no sequence (13.4 phase SG).
- What a compromised `serve` can still do under (a) with distinct uids: make the service key send any `MsgOfferLease` the
  chain accepts; relay any stored or captured provider signature as `BROADCASTER`; replay a stored hosting intent for its
  own content and version; delay, reorder or withhold submissions; read the outbox, including auth contexts and
  unbroadcast inner signatures. It cannot register or withdraw content under an intent key it made up, sign another
  message type, sign for another chain id, or set an unbounded deadline.
- Same-uid limit: without distinct uids (4.1), the boundary protects nothing once `serve` is compromised.

Q1 option (b): the same keyring and checks as a package inside `serve`. Fewer processes; any `serve` bug or compromise can
reach key bytes and skip the checks.

Q1 option (c): only the `BROADCASTER` key; the service kinds are not built in goal 11.

Production custody (KMS, HSM, remote signer) is not available here and is not claimed (DEC-001, goal 26).

### 7.3 Externally signed submissions (Q2)

- P1 (relay): before enqueue and again before signing, the broadcaster reads the provider (`Query/Provider`), checks the
  provider is admitted, rebuilds the preimage with `leases.AcceptSignBytes` from the committed open offer or
  `RenewSignBytes`/`EndSignBytes` from the message and the held lease (`Query/ActiveLeases`), and verifies the
  InnerSignature with `msgkit.VerifyInner` against the provider's current key id and public key
  (`chain/x/leases/README.md:158-162`). A signature by the service key, the broadcaster key, another provider's key, a
  retired key or any unregistered key is refused with `PROVIDER_SIGNATURE_INVALID` and no row. The chain verifies again;
  the pre-check is fail-fast only (`docs/protocol.md:183`). There is no offer-by-id query (DR-08-08): `Query/Deadlines`
  returns full `Offer` messages but pages by deadline (`docs/protocol/proto/svn/leases/v1/query.proto`,
  `QueryDeadlinesResponse`), so the pre-check pages it within the PROPOSED page budget; an offer not found within the
  budget fails the pre-check (`OFFER_NOT_FOUND`), it never skips it.
- P3 (forward, if allowed): the broadcaster strictly decodes the raw bytes with the app decoder, requires exactly one message
  of an allowed relay type, an outer signer that is an `ADMITTED_PROVIDER` whose provider record names that account, a valid
  inner signature as in P1, the configured chain id (checked by verifying the outer signature against the account's pubkey
  and account number), and a `timeout_height` no further than the PROPOSED forward window. Bytes without a deadline are
  refused, because they could commit at any later time (section 8.5). Forwarded bytes are never edited; the outbox tracks
  them by hash and allocates no sequence. When they die, the intent fails with `PROVIDER_MUST_RESIGN`.
- P2 (provider submits its own transaction): goal 16. Goal 11 reconciles by object state when a provider's own submission
  commits the effect first (8.6; 13.4 phase FR1b).
- Audit batches (DR-11-06). Under the recommended Q2 answer the auditor keeps its own submit path and the broadcaster
  carries no audit batch. Under Q2 (c) the broadcaster forwards AUDITOR-signed batch transactions verbatim with the P3
  rules (strict decode, one `MsgSubmitAuditBatch`, `AUDITOR` outer signer, deadline window); it holds no `AUDITOR` key.
- Residual: a broadcaster can withhold a signed acceptance until the offer expires; P2 is the provider's remedy.

### 7.4 Hosting intents and the publication permission (Q3)

- Recommended (a): the broadcaster never holds an intent key. A separate devnet fixture signer stands in for the account
  host and signs intents naming the service signer's address as `submittingHost`.
- Intent-key registry: a file `intent-keys.json` mapping `principal_id` to one ed25519 public key (hex) and a label. It holds
  public keys only. It is written by the operator (a uid that is neither U_serve nor U_signer), mode 0644, in a directory
  neither of them can write. `serve` and `signer` read it at start and refuse to start if the file or its directory is
  writable by their own uid. A change is a restart. No broadcaster command and no caller writes it (M3).
- For `REGISTER_CONTENT` and `WITHDRAW_CONTENT`, `serve` checks: the authenticated caller may send the kind; the auth
  context grants `PUBLISH_CONTENT` (or `WITHDRAW_CONTENT`) to `principal_id`; the intent public key equals the key the
  registry lists for that principal; the intent signature verifies over the canonical payload rebuilt with the exported
  builders and the pinned service address; for a withdraw, the principal is the recorded `EFFECTIVE` owner (5.7). The
  signer checks the registry and the signature again (7.2).
- The broadcaster's config schema has no intent-key field, and a static test fails if any broadcaster package calls an
  ed25519 or SDK signing function outside `internal/signer` (13.6).
- Limits, stated in the README, the boundary document and the evidence:
  - The chain cannot enforce creator permission (`chain/x/content/README.md:61`).
  - Under Q1 (a) the publication half of REQ-CHAIN-013 holds for the broadcaster's code path (`serve` and `signer`) only.
    Whoever holds the `ADMITTED_SERVICE` key bytes can register content with an intent key they make up, because the chain
    checks only the `DEVNET_TEST_ONLY` role (`chain/x/content/intent.go:126-152`).
  - Under Q1 (c) the chain role enforces it for the broadcaster's key: a `BROADCASTER` outer signer is `FORBIDDEN` for
    `RegisterContent` (`chain/x/content/README.md:39`), and content submission leaves goal 11.
  - The truth of the caller's permission claim is the API's (goals 13, 19, 21). Community publication approval
    (`docs/protocol.md:195`, DEC-003) is not this bit under option (a).

### 7.5 Never signed, never held

Provider inner signatures, hosting intents, council messages, `MsgExecuteChange`, `authority_end`, audit batches,
provider registry messages, arbitrary bytes. Never held: seat, AUTHORITY, AUDITOR or provider account keys, provider
signing keys, validator keys, creator or user keys, hosting-intent keys.

### 7.6 Chain id binding and stored authorizations (Q4)

- Every intent row stores the configured chain id; `serve` refuses to start when `/status` reports another network (the
  `Header` pattern, `agents/auditor/chainclient/rpc.go:197-209`) and refuses to relay an intent created under another chain
  id.
- Inner signatures carry no chain id and RENEW/END never expire on chain (section 1.2). Under Q4 (a): unbroadcast inner
  signatures are sensitive data (never logged, never returned except as a digest); RENEW and END relays carry a local
  expiry (PROPOSED) and are refused after it; ACCEPT relays are refused after the offer's expiry. Local expiry revokes
  nothing on chain; only a renewal (D-12) or a key rotation (DR-08-12) does. The README says so.

### 7.7 Redaction and secret hygiene

- `log/slog` JSON handler with a `ReplaceAttr` that drops attributes named or typed as key material, tx bytes, signature
  bytes, DSN, password, certificate, auth context or request body; values are logged only as hashes or ids. API errors never
  echo internal errors.
- Database connection: `internal/config` builds `pq.Config` field by field and opens it with `pq.NewConnectorConfig`, which
  reads no environment (`PQ/connector.go:205-207`). As a second guard, every subcommand refuses to start, before any
  connection, if any `PG*` variable or `PQGO_DEBUG` is set (`PQ/connector.go:242-431,511-512`; `PQ/conn.go:68-73`). A test
  proves the hand-built config connects and that each variable is refused.
- PostgreSQL server logging: section 6.5.
- Names: variables and fields avoid the `scripts/scan_secrets.py` patterns without exemptions (for example `signingKey`,
  `keyMaterial`), and canary strings are generated at run time (m15).
- The log canary (13.4 phase L) plants unique run-time strings in a key file, a DSN password, an auth context id, an inner
  signature and a request body. It first asserts that each canary was processed (the intent committed, the auth context was
  stored, the inner signature was relayed), then greps the broadcaster, signer, PostgreSQL and node logs and the go test
  output. Zero hits is the pass condition. A slog-level unit test also kills the "log the request body" mutant (13.1).

---------------------------------------------------------------------------------------------------

## 8. Submission engine

### 8.1 Per-signer serialization and sequence allocation

- One worker loop per configured signer. On start it takes `pg_try_advisory_lock(lock_key)` on a pinned connection
  (`db.Conn`); every allocation and attempt write for that signer runs on that connection. If the connection dies (process
  death, PostgreSQL restart), the lock is gone and the next write fails, so nothing is signed or sent until the lock is
  held again. A second broadcaster process with the same config does not drive that signer.
- `reconcile-once` and `signer-clear` take the same lock with `pg_try_advisory_lock` and exit with an error if it is held
  (m10). `status` is read-only: it opens a read-only transaction and never broadcasts.
- Window of one live transaction per signer (DR-11-26): a later sequence becomes invalid when an earlier one dies, and there
  is no gap queue (`map-chain.md` 2.2). Concurrency comes from several signers.
- Next sequence: when the signer has no live attempt, read the committed account (number, sequence) through the node. The
  generalised `Account` returns the answer's `Response.Height`; it must be at least `max(signers.observed_height, height
  needed by the caller)`, else the node is stale and the loop waits (m2). The auditor's `Account` ignores that height
  (`agents/auditor/chainclient/rpc.go:305-337`).
- Signer conflict (DR-11-15). If the chain sequence is above `next_sequence` and an `acc_seq` lookup (8.5) names a hash that
  is not one of our attempts, the signer halts: `halted_reason = SIGNER_CONFLICT`, a `signer_events` row, an operator alert.
  It signs nothing until `svn-broadcaster signer-clear --signer <address> --foreign-hash <hash>` records the operator's
  acknowledgement. Otherwise `next_sequence` := chain sequence.

### 8.2 Building an attempt

- Business id: drawn from `crypto/rand` when the intent (or a new business attempt) is created: a short prefix plus 128
  bits in lowercase base32, inside the chain pattern; reused for every attempt of that business attempt (DR-11-07).
- Deadline: `timeout_height = latest committed height + K` (K PROPOSED, DR-11-05), and strictly above the `timeout_height`
  of every earlier attempt with the same signer and sequence; the worker waits for a block if needed (m6). Because signing is
  deterministic (1.1), this is what keeps two attempts from having the same bytes and colliding on `UNIQUE(tx_hash)`. No
  `timeout_timestamp`.
- Gas: a fixed PROPOSED declared gas per kind (2,000,000, the devnet and auditor precedent,
  `chain/devnet/lifecycle_test.go:340`; `agents/auditor/config/config.go:100`). `WithdrawContent` is sized by simulation
  because many open offers raise its gas (`docs/architecture/leases.md:140`): `serve` builds the simulation transaction
  itself with the signer's public key and an empty signature, without calling the signer, and sends it to `abci_query
  /app/simulate` (1.1). The declared gas is the simulated gas plus a PROPOSED margin, capped at 20,000,000 (DR-11-27). The
  result comes from one trusted node; a wrong answer can only make the transaction fail with out-of-gas, which 8.6 handles.
- The signer returns bytes and hash; the worker re-decodes and checks the single message equals the stored request with
  signer and business id filled in.

### 8.3 Persist, then broadcast (compare-and-set; sends counted before they happen)

1. Persist, in one transaction on the pinned connection:
   - `SELECT ... FROM intents WHERE intent_id = $1 FOR UPDATE`. Continue only if: `state = 'REQUESTED'`, `phase IN
     ('QUEUED', 'RETRY_WAIT')`, `validated`, `clock_timestamp() < expires_at`, counts below the attempt caps, no live
     attempt, no open stall episode.
   - `SELECT ... FROM signers WHERE address = $1 FOR UPDATE`; continue only if `halted_reason IS NULL`.
   - Insert the attempt (bytes, hash, sequence, timeout height, phase `SIGNED`, `sends_started = 0`); advance
     `next_sequence`; set the intent to `IN_FLIGHT/SIGNED` by compare-and-set; add a `transitions` row; commit.
   - If any condition fails, roll back and discard the signed bytes. They were never sent, so the sequence is not used on
     chain and the next allocation reuses it. If the commit fails, nothing is sent.
   - Lock order is always intent row, then signer row.
2. Before every send of these bytes (the first send and every rebroadcast): `UPDATE attempts SET sends_started =
   sends_started + 1 WHERE attempt_id = $1 AND <attempt still live> AND <before expires_at>`, committed on the pinned
   connection. If it fails or matches no row, nothing is sent.
3. `broadcast_tx_sync` to the configured node.
4. Record the send outcome (8.4) in a second transaction: increments `sends_recorded` and the matching counter, raises the
   attempt phase (9.2), adds a `transitions` row on a class change. If this write fails, the outcome stays unrecorded and
   counts as UNKNOWN (8.5).

A crash between steps 2 and 4 leaves `sends_started > sends_recorded`, and every such send counts as UNKNOWN. This is the
auditor's order (`agents/auditor/chainclient/submit.go:590-635`: bytes stored, then sent, then the record, which defaults to
`AckUnknown`) moved into PostgreSQL, with the send counted first (M1).

T2 (validation failure), T3 (cancel) and T4 (expiry) take the same intent row lock and update only `WHERE state =
'REQUESTED'` with no live attempt (and, for T3, no attempt ever persisted), checked by row count. So a cancel, an expiry
or a validation failure that races the persist step either wins (the persist step then sees a non-REQUESTED row and
discards its bytes) or loses (it sees the attempt and does nothing). Neither can leave an on-chain effect under a FAILED
row (M2).

Build-tagged fault points (`-tags svn_faultpoint`, `internal/faultpoint`): `after-sign-before-persist`,
`after-persist-commit`, `after-sends-started`, `after-broadcast-before-record`, and in the signer `after-decode-before-reply`.
When armed by a file in the test's temp directory, a point writes a marker file and blocks; the test waits for the marker,
checks the database state, then sends SIGKILL (13.4). The default build has no fault-point symbol (13.6).

### 8.4 Classifying answers (goal spec :25-26)

Send outcomes (one per started send):

| Observation | Recorded send outcome | Counts as a definitive refusal for (d2)? |
| --- | --- | --- |
| Transport error, timeout, reset, "broadcast confirmation not received", or no record after a crash | UNKNOWN | no; blocks (d2) |
| JSON-RPC `tx already exists in cache` | CACHE (the node holds these bytes or committed them) | no; blocks (d2) |
| JSON-RPC mempool full, tx too large, app connection error | NODE_REFUSED | yes |
| CheckTx sdk code 32 (sequence mismatch) | MEMPOOL_REJECTED{sdk, 32} | no; decided by (d3), because the bytes may have committed before a node restart cleared the cache (1.1) |
| CheckTx sdk code 4 inside the DR-10-41 window | MEMPOOL_REJECTED{sdk, 4} | yes; retryable (8.7) |
| CheckTx any other code != 0 | MEMPOOL_REJECTED{codespace, code} | yes |
| CheckTx code 0 | ACK; status SUBMITTED; never COMMITTED | no; blocks (d2) |

Committed results (from `/tx(H)`, whose returned bytes must hash to H):

| Observation | Recorded as | Attempt |
| --- | --- | --- |
| code 0, typed `svn.events.v1.*` event of the kind | `COMMITTED_OK` | resolved |
| code 0, `<module>_result idempotent=true` only | `COMMITTED_IDEMPOTENT` | resolved |
| code != 0, result carries `tx.acc_seq=<S>/<N>` of this attempt | `EXECUTION_FAILED` with code (sequence consumed) | dead |
| code != 0, no `tx.acc_seq` attribute | `INCLUDED_ANTE_FAILED` with code (sequence not consumed) | dead |
| code != 0, `tx.acc_seq` names another account or sequence | integrity error: alert, signer halt | unchanged |
| not found | nothing yet | decided by 8.5 |

The sequence-consumed rule reads the `tx.acc_seq` attribute, never the code (1.1, m1). `CommittedTx` is extended to check
that SHA-256 of the returned bytes equals the requested hash (section 1.3).

### 8.5 When an attempt can no longer commit (the dead rule)

Attempt A = (signer S, sequence N, timeout T, hash H). A is DEAD when one of:

- (d1) `/tx(H)` is found with a non-zero code (committed failure).
- (d2) `sends_started >= 1`; every started send has a recorded outcome (`sends_recorded = sends_started`); every recorded
  outcome is a counted refusal of the 8.4 table; and none is UNKNOWN, CACHE, ACK or sdk 32. A refused transaction is not
  added to the node's mempool, so it is not gossiped. With zero started sends, (d2) never holds (M1).
- (d3) the node's committed height is at least T + L (L PROPOSED, for asynchronous indexing,
  `chain/devnet/lease_test.go:406-408`), `/tx(H)` is not found, and either the committed account sequence of S, read at a
  height of at least T + L (8.1), is at most N (no later block can pass A's deadline check; derived from source,
  `map-chain.md` 3.4), or `tx_search "tx.acc_seq='<S>/N'"` returns a hash other than H (sequence N was used by another
  transaction). If that hash is one of our stored attempts, that attempt is resolved by its own `/tx`; if it is not ours,
  the signer halts (8.1). If the account sequence is above N and the search finds nothing, A stays UNCERTAIN and an alert is
  raised.

A is never declared dead from the local clock, from the intent's expiry, or while the chain height does not advance.

Integrity rule: if a committed transaction's hash equals a stored attempt already marked DEAD, it is a fatal integrity
alert; the signer halts, nothing is rewritten, and the intent is never reported as committed by someone else (M1).

### 8.6 Reconciliation before any resend (goal spec :25)

Runs at start-up for every non-final intent, after every UNCERTAIN, periodically, and always immediately before signing a
new attempt:

1. Re-decode every stored attempt's bytes with the app decoder; a decode failure is a fatal integrity error, reported, not
   repaired.
2. For each live attempt: `/tx(H)`. Found: classify (8.4). Not found: apply 8.5. If still live and before `expires_at`,
   rebroadcast the same stored bytes (never re-signed bytes) at most once per backoff step, whether the attempt is SIGNED,
   UNCERTAIN or ACKNOWLEDGED, because a node restart empties its mempool and cache (1.1); the same bytes cannot double-apply.
   After `expires_at`, observe only and send nothing (m5).
3. Effect check. It runs after every committed non-zero code, every `INCLUDED_ANTE_FAILED`, every dead attempt, and before
   any new attempt (M6):
   - Look up the object (`Query/Content`, `Query/ActiveLeases`, `Query/Deadlines`) and the kind's typed event through
     `/tx_search` (`ContentRegistered.content_id`, `ContentWithdrawn.content_id`, `LeaseOffered.offer_id`,
     `LeaseAccepted.lease_id`, `LeaseRenewed.lease_id` with term version, `LeaseEnded.lease_id`).
   - "Effect exists" means: a committing transaction was found by that search, decoded from `/tx` with the hash check, its
     single message has the intent's type URL, and its business fields equal ours (`chain/x/leases/README.md:134-140`;
     content fields `chain/x/content/msg_server.go:100-106,199-204`).
   - Signer rule (M7). For `OFFER_LEASE`, `REGISTER_CONTENT` and `WITHDRAW_CONTENT`, the committing signer must equal the
     intent's pinned service address. If it does and the hash is one of our attempts: COMMITTED, `own_transaction = true`.
     If it does and the hash is not ours: SIGNER_CONFLICT (8.1), no transition. If it does not: FAILED(`OBJECT_TAKEN`).
     For the relay kinds the signer is ignored (a `BROADCASTER` or the provider's own account may commit it):
     COMMITTED, with `own_transaction` true only when the hash is one of our attempts.
   - Business fields differ: FAILED(`OBJECT_TAKEN`).
   - The object changed state with no such transaction (begin-block expiry `LeaseEnded{EXPIRED}`, a council force-withdraw
     inside `MsgExecuteChange`, or a transaction of another kind): FAILED(`OBJECT_STATE_CHANGED`) with the observation.
     Never COMMITTED.
4. Only if no effect exists and the intent may still start an attempt (8.7, before expiry, not stalled, signer not halted)
   is a new attempt signed: same business id, sequence from 8.1, deadline from 8.2.
5. A committed `REPLAY` with no effect: our business id was taken by another payload; open a new business attempt (new
   random id), bounded; at the bound, FAILED(`BUSINESS_ID_TAKEN`). Any other committed code with no effect: retryable codes
   (8.7) go to RETRY_WAIT, the rest to FAILED(`EXECUTION_FAILED{code}`). A resend's code is never mapped onto an earlier
   attempt (`chain/x/leases/README.md:607-620`).

No step edits history by hand. A `transitions` row records the observation behind each change of state, phase or
observation class; repeated identical polls only raise `transitions_suppressed` (m11).

### 8.7 Retries, backoff, bounds (all PROPOSED, DR-11-10)

- Retryable: `INCLUDED_ANTE_FAILED` with sdk 30, 42 or 32 (expired deadline or a sequence race; resync the sequence), sdk 4
  within the DR-10-41 window (at most a PROPOSED number of tries, at least two blocks apart, DR-11-18), node refusals,
  transport errors, sdk 11 out of block gas.
- Not retryable (FAILED, after the effect check of 8.6 step 3): svn `UNAUTHORIZED`, `FORBIDDEN`, `BOUNDS`, `FEE_REJECTED` at
  CheckTx (configuration or request error; an `UNAUTHORIZED` signer also halts the signer), sdk 2 decode, and every committed
  execution failure except `REPLAY` (8.6 step 5).
- Backoff with full jitter, base 1 s, cap 60 s; `next_attempt_at` is stored, so restarts keep the schedule.
- Caps: 8 attempts per business id, 4 business attempts, 16 attempts per intent. These are new PROPOSED values. The auditor
  bounds different things: `MaxSubmissions = 16` per batch and `maxRounds = 4` broadcast rounds per `Submit` call
  (`agents/auditor/chainclient/submit.go:78-82`).
- A new attempt starts only before `expires_at` (database clock); a live attempt is never abandoned because of expiry, and
  it is not resent after expiry (8.6 step 2).

---------------------------------------------------------------------------------------------------

## 9. State machine (exact)

### 9.1 Intent states and the caller mapping

| Internal state / phase | Meaning | `OperationStatus` shown |
| --- | --- | --- |
| `REQUESTED/QUEUED` | rows committed; nothing signed | REQUESTED |
| `REQUESTED/RETRY_WAIT` | every earlier attempt is dead and has no effect; next attempt at `next_attempt_at` | REQUESTED (detail says attempts so far) |
| `IN_FLIGHT/SIGNED` | one attempt persisted; no send outcome recorded | SUBMITTED (phase SIGNED) |
| `IN_FLIGHT/ACKNOWLEDGED` | CheckTx code 0 on the node | SUBMITTED |
| `IN_FLIGHT/UNCERTAIN` | an UNKNOWN or CACHE send; may commit | SUBMITTED (phase UNCERTAIN) |
| `IN_FLIGHT/AWAITING_DEADLINE` | expiry passed; a live attempt still may commit; no more sends | SUBMITTED (reason EXPIRED_AWAITING_CHAIN, `last_landing_height`) |
| `COMMITTED` (terminal) | 9.4 | COMMITTED |
| `FAILED` (terminal) | reasons: `EXECUTION_FAILED{code}`, `REJECTED{code}`, `EXPIRED_NOT_SENT`, `EXPIRED_DEAD`, `ATTEMPTS_EXHAUSTED`, `OBJECT_TAKEN`, `OBJECT_STATE_CHANGED`, `BUSINESS_ID_TAKEN`, `UNAUTHORIZED`, `INVALID`, `PROVIDER_SIGNATURE_INVALID`, `PROVIDER_MUST_RESIGN`, `CANCELLED` | FAILED |
| any non-terminal while a stall episode is open and the observer is fresh | the episode is recorded (`stall_episodes`, plus a `transitions` row per non-terminal intent on entry and exit); the overlay is shown at read time (section 10) | PENDING_STALL (detail keeps the phase) |
| any non-terminal while the observer is stale | no stall claim | the underlying status with `detail.observer = OBSERVER_UNAVAILABLE` |

### 9.2 Attempt phases

- Non-terminal phases are ordered by knowledge: `SIGNED < UNCERTAIN < ACKNOWLEDGED`. A send outcome raises the phase to the
  highest knowledge seen and never lowers it: a CACHE or UNKNOWN answer on a rebroadcast of an ACKNOWLEDGED attempt keeps
  ACKNOWLEDGED (m12).
- Terminal phases: `COMMITTED_OK`, `COMMITTED_IDEMPOTENT`, `EXECUTION_FAILED`, `INCLUDED_ANTE_FAILED`, `DEAD`. They never
  change.
- A refusal is a send outcome, not a phase. An attempt whose (d2) holds becomes `DEAD`.

### 9.3 Transition table (a `transitions` row on every change)

| # | From | Event (observation) | Guard | To |
| --- | --- | --- | --- | --- |
| T1 | none | create (HTTP or SQL) | validation passed (HTTP) or size/kind/cap passed (SQL) | REQUESTED/QUEUED |
| T2 | REQUESTED | worker validation fails | row lock, `state = REQUESTED`, no attempt | FAILED(UNAUTHORIZED, INVALID, PROVIDER_SIGNATURE_INVALID) |
| T3 | REQUESTED | cancel | row lock, `state = REQUESTED`, no attempt ever persisted | FAILED(CANCELLED) |
| T4 | REQUESTED | expiry passed (DB clock) | row lock, `state = REQUESTED`, no live attempt | FAILED(EXPIRED_NOT_SENT if no attempt was ever persisted, else EXPIRED_DEAD) |
| T5 | REQUESTED or IN_FLIGHT | effect check finds our effect | 8.6 step 3 (signer rule) | COMMITTED (`own_transaction` per 8.6) |
| T6 | REQUESTED or IN_FLIGHT | effect check finds another account's effect or different fields | 8.6 step 3 | FAILED(OBJECT_TAKEN) |
| T7 | REQUESTED or IN_FLIGHT | effect check finds a state change with no committing transaction of the kind | 8.6 step 3 | FAILED(OBJECT_STATE_CHANGED) |
| T8 | REQUESTED | attempt persisted | 8.3 step 1 conditions; signer lock held; not stalled; signer not halted | IN_FLIGHT/SIGNED |
| T9 | IN_FLIGHT/SIGNED or UNCERTAIN | ACK | | IN_FLIGHT/ACKNOWLEDGED |
| T10 | IN_FLIGHT/SIGNED | UNKNOWN or CACHE | | IN_FLIGHT/UNCERTAIN |
| T11 | IN_FLIGHT | `/tx(H)` code 0 with typed event | event matches the kind and the object; bytes hash to H | COMMITTED (canonical = H) |
| T12 | IN_FLIGHT | `/tx(H)` code 0 idempotent | first commitment found and equal (9.4) | COMMITTED (canonical = first commitment) |
| T13 | IN_FLIGHT | `/tx(H)` code != 0 (EXECUTION_FAILED or INCLUDED_ANTE_FAILED) | run 8.6 step 3 first: T5, T6 or T7 if it decides | else REQUESTED/RETRY_WAIT (retryable, or REPLAY with a new business attempt, below caps, before expiry), else FAILED(EXECUTION_FAILED{code}, BUSINESS_ID_TAKEN, ATTEMPTS_EXHAUSTED or EXPIRED_DEAD) |
| T14 | IN_FLIGHT | attempt dead by (d2) or (d3) | run 8.6 step 3 first | else REQUESTED/RETRY_WAIT (retryable, below caps, before expiry), FAILED(REJECTED{code}) for a non-retryable refusal, else FAILED(ATTEMPTS_EXHAUSTED or EXPIRED_DEAD) |
| T15 | IN_FLIGHT | expiry passed | attempt still live | IN_FLIGHT/AWAITING_DEADLINE (no sends) |
| T16 | any non-terminal | signer conflict detected | 8.1 | unchanged; signer halted; observation row only |
| T17 | COMMITTED, FAILED | anything | | unchanged (absorbing; the database trigger refuses the update) |

There is no transition into COMMITTED except T5, T11 and T12, each guarded by a committed result read from the chain. A
property test and mutants prove it (13.5, 13.7).

### 9.4 What moves a row to COMMITTED

- T11: `/tx(H)` for one of this intent's attempts shows code 0 and the typed event of the kind with this intent's object id
  (`ParseCommitted`, `chain/msgkit/events.go:186-206`), and the returned bytes hash to H.
- T12 and T5: code 0 with `<module>_result idempotent=true`, or an effect found by object state, is accepted only after the
  first commitment of the same effect is found by typed event search, its business fields equal ours, and the signer rule of
  8.6 step 3 holds; that first commitment becomes the canonical evidence. An idempotent replay is never itself the canonical
  commitment.
- A committed hash that equals an attempt already marked DEAD is an integrity alert, never a commitment by another account
  (8.5).
- Authority messages are not routed, so the DR-06-05 case (code 0 with `authority_result{code}`,
  `chain/x/authority/msg.go:129-132`) cannot reach this rule; if it ever does, it is FAILED.

### 9.5 Canonical commitment evidence for the indexer (goal 12 handoff)

For every COMMITTED intent the `commitments` row holds: `chain_id`; `height`; `block_hash` (from `/commit` at that height,
`CMT/rpc/client/http/http.go:475`); `tx_hash` (SHA-256 of the bytes, `CMT/types/tx.go:29-35`); `tx_index` (from `/tx`);
`msg_index` (0, one message per transaction); `code`, `codespace`, `gas_wanted`, `gas_used`; `committing_signer`; the typed
events decoded with `ParseCommitted`, each with its position in the transaction result; `intent_id`, `business_id`;
`idempotent_replay` and `first_commit_tx_hash`. Goal 12 needs "chain identity, block height and stable
transaction/message/event identity" (`docs/goals/12-committed-state-indexer-and-reconciliation.md:19-21`). The evidence is
relative to one configured node; the README discloses that gateway trust (`docs/blueprint.md:391`).

---------------------------------------------------------------------------------------------------

## 10. Chain outage behaviour (goal spec :28-29, :38-39)

- Observer. The worker's observer reads `/status` every poll and writes `chain_health` (latest height, block time,
  `seen_at`). The observer is fresh when `seen_at` is within a PROPOSED number of poll intervals of the database clock.
- Stall episodes (DR-11-11, m3). A stall episode opens when, with a fresh observer, the height has not advanced for T
  seconds of database clock, or the node is unreachable or catching up; it closes when the height advances again. Each
  opening and closing writes a `stall_episodes` row change and one `transitions` row per non-terminal intent (bounded by the
  open-intent caps). Reads show PENDING_STALL for every non-terminal intent while an episode is open.
- A dead observer is not a stall (m4). If the worker or its RPC link dies, `seen_at` goes stale; reads then show the
  underlying status with `detail.observer = OBSERVER_UNAVAILABLE`, never PENDING_STALL and never COMMITTED.
- While a stall episode is open: new intents are still accepted within the caps, each with a required, bounded
  `expires_at`; no new attempt is started; live attempts stay live, are polled, and are rebroadcast only before their
  intent's `expires_at`.
- Outcomes. An intent that expires with nothing sent becomes FAILED(`EXPIRED_NOT_SENT`): an honest failure. An intent with a
  live attempt stays SUBMITTED/AWAITING_DEADLINE with the PENDING_STALL overlay until the chain moves past its
  `timeout_height` or commits it; then it becomes COMMITTED (if included) or FAILED(`EXPIRED_DEAD`). It never becomes
  COMMITTED unless a committed block containing it is read.
- Reference difference (DR-11-19, m9). The reference outbox parks the request with its business id and returns
  `ERROR_CODE_QUORUM` (`cases:64-77`). Goal 11 parks it the same way (REQUESTED/QUEUED, business id assigned, no attempt, no
  chain write) and returns no chain code, because the protocol says the fixture `down` set does not produce that code
  (`docs/protocol.md:129`) and a stall is not a chain result. The stall test asserts the reference's observable state: a
  pending row with its business id and no transaction for it on chain.
- The broadcaster has no lease, offer or content state of its own beyond ownership rows; it cannot create a final lease in
  the database. COMMITTED for `OFFER_LEASE` means an offer committed, not a lease.

---------------------------------------------------------------------------------------------------

## 11. Configuration and PROPOSED numbers (`internal/config`; typed JSON file plus flags; no defaults for chain id or gateway)

| Key | Rule |
| --- | --- |
| `chain_id` | required; must equal `/status` network at start and before each signing round; dev keys refuse a chain id without `shortvideo-dev-` |
| `rpc_url` | required; `http://127.0.0.1:...` or `http://[::1]:...` by default; a non-loopback URL needs `https` and `allow_remote_rpc: true`; one node, disclosed as trusted for reads |
| `database` | Unix socket directory or 127.0.0.1, database, role, `sslmode` (required, `PQ/connector.go:304-306`); password only from a passfile (mode 0600, owned by the reading uid); built into `pq.Config` field by field and opened with `pq.NewConnectorConfig` (7.7); any `PG*` variable or `PQGO_DEBUG` in the environment refuses start |
| `listen` | loopback address; TLS server cert, key and client CA paths |
| `signer` | socket path, expected serve uid and signer uid (Q1 (a)); or key names and paths (Q1 (b)) |
| `keys` (signer only) | name -> file, class; no intent key field exists |
| `intent_key_registry` | path of the operator-written public-key file (7.4) |
| `callers` | principal -> kinds, per-caller cap, bindings |
| PROPOSED numbers | the table below; each has hard lower and upper bounds in code; labelled PROPOSED in the README |

PROPOSED numbers (DR-11-10). The values and the hard bounds are new; none is a service level. The owner is asked to accept
the labels, not the values. The devnet test runs at the lower bound wherever a phase does not depend on the value, and
prints the values it used (F11).

| Number | Value | Hard bounds in code |
| --- | --- | --- |
| K, `timeout_height` offset (blocks) | 40 | 5 to 1,000 |
| K_max, signer's timeout bound (blocks) | 1,000 | equal to K's upper bound |
| L, indexing margin (blocks) | 2 | 1 to 20 |
| T, stall window (s) | 10 | 3 to 600 |
| Observer freshness (poll intervals) | 4 | 2 to 20 |
| Attempts per business id | 8 | 1 to 16 |
| Business attempts per intent | 4 | 1 to 8 |
| Attempts per intent | 16 | 1 to 32 |
| Backoff base / cap (s) | 1 / 60 | base 0.1 to 10; cap 1 to 600 |
| Intent lifetime (`expires_in`) | 30 s to 24 h | 5 s to 7 d |
| Open intents per caller | 256 | 1 to 4,096 |
| Sum of per-caller caps (global bound) | 4,096 | 1 to 16,384 |
| RENEW/END local expiry (s) | 3,600 | 60 to 86,400 |
| Forward window (blocks) | 100 | 5 to 1,000 |
| Declared gas per kind | 2,000,000 | 1 to 20,000,000 |
| Withdraw simulation margin | 30 % | 0 to 100 %; result capped at 20,000,000 |
| RPC timeout (s) | 10 | 1 to 60 |
| Poll interval (ms) | 500 | 100 to 10,000 |
| List page size | 100 | 1 to 100 |
| sdk 4 retries (at least 2 blocks apart) | 3 | 1 to 10 |
| Request body cap (bytes) | 65,536 | 1,024 to 65,536 |
| Auth context cap (bytes) | 2,048 | 256 to 8,192 |
| `Query/Deadlines` page budget (pages of 100) | 10 | 1 to 50 |
| `ack_log` clip (bytes) | 512 | 0 to 4,096 |
| `transitions.observation` cap (bytes) | 2,048 | 256 to 8,192 |
| `transitions` rows per intent | 256 | 16 to 4,096 |
| Enqueue `lock_timeout` / `statement_timeout` (s) | 2 / 5 | 0.1 to 30 / 1 to 60 |

---------------------------------------------------------------------------------------------------

## 12. Repository integration points (outside `services/broadcaster`; INT owns them unless noted)

| File | Change | Note |
| --- | --- | --- |
| `go.work:5-11` | add `./services/broadcaster` | Wave 0 step 0 (4.5) |
| `docs/evidence/goal-01/check_requirements.py:184-194` | add `"services/broadcaster/"` to `SOURCE_ALLOW` and update the `:184-185` comment ("Scaffold Go lives under chain/ and agents/") | Wave 0 step 0. A goal 01 contract change caused by DR-11-20; disclosed to the owner with Q1-Q4 and recorded in the evidence. B10 adds the goal 11 `PY_ALLOW` line in Wave 2. No other edit to the file. |
| `scripts/run_tests.py:151` | add `services/broadcaster` to `GO_MODULES` | the module's tests include PostgreSQL integration tests and fail without PostgreSQL. If `chain/devnet` with the new tests nears the 30-minute limit (`:152-158`), report it; do not raise the limit without recording it |
| `Makefile:67-73` | add `go build -C services/broadcaster ./...` to `build` | |
| `services/broadcaster/svn_broadcaster/*` | delete the Python scaffold | its `serve()` would say "does not submit transactions" next to a service that does |
| `tests/unit/test_scaffold.py:7,24-27` | remove the broadcaster scaffold test | replaced by design, recorded with its replacement tests (`tests-replaced.txt`) |
| `tests/unit/test_repo_policy.py:17` | require `services/broadcaster/go.mod` and `services/broadcaster/cmd/svn-broadcaster/main.go` | |
| `pyproject.toml:31,47` | drop `services/broadcaster` from `pythonpath` and `mypy_path` | |
| `README.md:14` | describe the service | |
| `.github/workflows/ci.yml` | `go` job: a pinned PGDG install step before `run_tests.py`, plus `go vet` of the module; `go-race`: new shards `broadcaster` (module tests under `-race`, PostgreSQL installed), `devnet-broadcaster` (`-run TestDevnetBroadcasterLifecycle`) and `devnet-broadcaster-stall` (`-run TestDevnetBroadcasterQuorumStall`), each prebuilding svnd and the race-built, fault-tagged broadcaster with the flags the tests use; `TestDevnetBroadcaster` added to the `devnet` shard's `-skip` union; the `go` job's echo line names the new shards | DR-11-25 |
| `tests/unit/test_ci_devnet_coverage.py:519,539-543` and its echo assertion | update the `MATRIX` and `DEVNET_SKIP` constants and the echo assertion only; keep every mutation fixture | recorded as "constants only" in `tests-replaced.txt` (F9) |
| PostgreSQL skip | none. There is no `SVN_SKIP_POSTGRES`; every job that runs the module's tests installs PostgreSQL; `chain/devnet` gets no new skip gate, so the devnet tests fail without PostgreSQL | replaces revision 1's skip variable and its guard test |
| `tests/unit/test_ci_broadcaster.py` (new) | fails if a job runs the module's tests without the pinned PostgreSQL install, if any PostgreSQL skip variable appears, or if ci.yml sets `SVN_BROADCASTER_PHASES` (13.4) | planted-mutation fixtures, as `test_ci_devnet_coverage.py` |
| `tests/unit/test_chain_writer_policy.py` (new) | REQ-INT-001 static policy (13.6) | |
| `docs/dependencies.md` | lib/pq v1.12.3 (MIT) as a direct dependency of `services/broadcaster`, already in svnd; the PGDG install in CI, with the apt archive fallback `[Educated Guess]` (F19) | |
| `docs/sbom.cdx.json` | regenerate (`scripts/write_sbom.py:68` reads every `go.sum`) | |
| `docs/architecture/README.md` | index `broadcaster.md` | B5 |
| `chain/x/leases/README.md`, `docs/architecture/leases.md` | none (their goal 11 handoff text stays true) | report only |
| `docs/progress.md` row 09 ("Outbox submission: Goal 11") | not touched until the owner answers Q2 (DR-11-06) | orchestrator |

CI pinning (DR-11-25, F19). The step installs `postgresql-18=18.6-1.pgdg24.04+2` with the key fingerprint of
`docs/dependencies.md:39` (`scripts/env/install.sh:9,14`). `[Educated Guess]` apt.postgresql.org's main suite keeps only the
newest build of each major, so the pin may stop installing; the step then tries apt-archive.postgresql.org for the same
version, and otherwise fails with a clear message. It logs `postgres --version`.

Disk: one more binary per build flavour and its build cache entries. Temp binaries and PostgreSQL clusters are deleted after
each run.

---------------------------------------------------------------------------------------------------

## 13. Test plan

Mocks and fakes appear only in 13.1 and in the worker tests marked "fake RPC" in 13.2; they are never cited as end-to-end
evidence. 13.3 and 13.4 use a real chain and real processes; 13.2 and 13.4 use real PostgreSQL 18.6.

### 13.1 Unit (services/broadcaster; fake RPC and fake signer allowed)

- outbox:
  - `TestNoCommittedWithoutCommittedResult`, `TestAckNeverCommits`, `TestCommitNeedsTypedEvent`,
    `TestIdempotentReplayNeedsFirstCommit`, `TestTerminalStatesAbsorb`, `TestStallOverlayNeverCommits`,
    `TestObserverStaleNotStall`.
  - `TestDeadRuleD1D2D3`: each condition and each near miss (a refusal after an UNKNOWN send stays live; height T+L-1 stays
    live; account sequence above N with no search hit stays live).
  - `TestZeroRecordedSendsNeverDeadByD2`, `TestUnrecordedSendCountsUnknown`, `TestSdk32AndCacheNeverDefinitive`,
    `TestDeadAttemptHashMatchIsIntegrityAlert`.
  - `TestExpiryNeverAbandonsLiveAttempt`, `TestNoSendAfterExpiry`, `TestReconcileBeforeResend`,
    `TestResendCodeNeverMapsToFirstAttempt`, `TestCommittedFailureRunsEffectCheck`,
    `TestStateChangeWithoutTxIsObjectStateChanged`, `TestReplayOpensBoundedBusinessAttempt`, `TestCancelOnlyBeforeSigning`,
    `TestPhaseNeverLosesKnowledge`, `TestStallBlocksNewAttempts`.
- chain:
  - `TestBuildTxMatchesChainDecoder` (strict decode with `app.MakeEncoding`, one message, signer, gas, `timeout_height`,
    empty memo and fee), `TestBuildTxRefusesForeignSigner`.
  - `TestClassifyBroadcastAnswers` (the 8.4 table, with the exact JSON-RPC texts from `CMT/mempool/errors.go` and
    `CMT/rpc/core/mempool.go`), `TestSequenceConsumedOnlyWithAccSeq`, `TestCommittedTxChecksHash`, `TestTxSearchQueries`
    (acc_seq and typed event query strings, below 512 bytes).
  - `TestAccountReadHeightBound` (a node answering below the needed height is stale), `TestNewAttemptTimeoutAboveEarlier`.
  - `TestEffectComparison`, per kind: each business field changes the result; for `OFFER_LEASE`, `REGISTER_CONTENT` and
    `WITHDRAW_CONTENT` the committing signer changes it (another service: OBJECT_TAKEN; ours with a foreign hash:
    SIGNER_CONFLICT); for relay kinds the signer does not.
- policy and signer:
  - `TestKindSignerAllowlist`, `TestSignerRefusesOtherTypeURLs` (every registered message type URL except the allowed ones),
    `TestSignerRefusesAuthorityEnd`, `TestSignerRefusesForeignChainID`, `TestSignerRefusesUnboundedTimeout` (0, above
    observed + K_max, no fresh height), `TestSignerRefusesUnregisteredIntentKey`, `TestSignerVerifiesIntentSignature`.
  - `TestProviderPrecheck` (service key, broadcaster key, other provider's key, retired key, random key, wrong domain, wrong
    lease id: each refused), `TestForwardedTxChecks` (multi-message, wrong type, no deadline, deadline too far, wrong chain,
    foreign signer), `TestPublicationPermission` (missing permission, unregistered intent key, another principal's content,
    `submittingHost` not the pinned signer).
  - `TestIntentKeyRegistryRefusesWritableFile`, `TestStartupRefusesRole` (AUTHORITY, AUDITOR, provider, seat, no role,
    duplicate address), `TestKeyFileRules`, `TestSignerPeerUID`.
- api: `TestStrictDecode` (unknown field, oversize, bad id), `TestServerConstructorSharedWithMain`,
  `TestClientCertRequired`, `TestCrossCallerNotFound`, `TestIdempotencyConflict`, `TestRequestDigestCanonical` (reordered
  keys give the same digest), `TestAuthContextRefusesPersonalData`, `TestDevPrincipalsRefuseNonDevChain`,
  `TestContentIDUnavailableIsUniform`.
- config: `TestStartRefusesPGEnv` (each `PG*` variable and `PQGO_DEBUG`), `TestExplicitPQConfigReadsNoEnv`,
  `TestNumbersHaveHardBounds`, `TestRefusesChainIDMismatch`, `TestCapsSumBounded`.
- logx: `TestRedactorDropsSecrets`, `TestRedactorDropsRequestBody` (slog level; kills the "log the request body" mutant
  without a devnet).

### 13.2 PostgreSQL integration (real disposable cluster via `internal/pgtest`)

`internal/pgdev`: `os.MkdirTemp("", "svn-pg-")` under `/tmp` (short path, under the 107-byte Unix socket limit). When
`euid == 0`, it `chown`s the directory to `postgres` and runs `initdb` and `postgres` with `SysProcAttr.Credential` (uid
102, gid 104 here, looked up by name); otherwise it runs as self (the CI runner). `initdb --auth-local=scram-sha-256
--auth-host=scram-sha-256 --encoding=UTF8 --locale=C.UTF-8` (the `scripts/env/start.sh` flags), Unix socket only
(`listen_addresses=''`), socket directory traversable by the serve uid, `fsync=on`, the logging settings of 6.5.
`postgres` runs as a direct child, not daemonized by `pg_ctl start`, with `Pdeathsig = SIGKILL` set from a goroutine
locked to its OS thread, and in its own process group, so a timed-out test does not leave a server holding disk (F18). It
refuses to start if an ancestor directory is not traversable by the server user. Missing binaries fail the test; there is
no skip. The version (`postgres --version`) is logged. Crash hooks are in `internal/pgtest` only: `pg_ctl -m immediate
stop`, SIGKILL of the postmaster, restart on the same data directory. These are process crashes, not host power loss.

Tests:
- Coupling and roles: `TestEnqueueAtomicWithApplicationRow`, `TestEnqueueRollbackLeavesNothing`,
  `TestClientDeathMidTransaction` (a re-executed helper process killed with SIGKILL mid-transaction; a goroutine is not a
  process), `TestMigrationsChecksumGapForwardOnly`, `TestRuntimeRoleHasNoDDL`, `TestRuntimeRoleCannotWriteCallers`,
  `TestEnqueueFunctionSearchPathAndGrants`, `TestCallerSeesOnlyOwnRows`, `TestSQLMetacharactersAreData`,
  `TestSQLDigestIgnoresKeyOrder`.
- Concurrency and races: `TestIdempotencyUniqueUnderConcurrency` (50 goroutines, one key), `TestOneLiveAttemptPerSequence`,
  `TestCancelRacesPersist` and `TestExpiryRacesPersist` (a barrier between signing and the persist transaction; the loser
  changes nothing), `TestTerminalRowsImmutable` (the trigger), `TestTransitionsAppendOnly`, `TestTransitionsBounded`,
  `TestPerCallerCapNoGlobalLock` (an open transaction of one caller does not block another caller's enqueue or the worker).
- Fencing: `TestAdvisoryLockFencesSecondWorker`, `TestLockLossStopsSending` (terminate the backend; the next allocation
  fails; nothing is sent), `TestReconcileOnceTakesLock`, `TestImmediateStopKeepsCommittedAttempts`.
- Worker over real PostgreSQL with a fake RPC (unit-labelled; the killers of the persist-order mutants):
  `TestFakeRPCSeesPersistedAttempt` (on every broadcast the fake RPC reads the attempt row over a separate connection and
  fails if the row is absent or `sends_started` was not incremented), `TestCrashBetweenSendsStartedAndRecord` (a helper
  process killed after `sends_started`; the restarted worker never applies (d2)), `TestSignerConflictHalts`,
  `TestOfferNeedsEffectiveOwner`, `TestContentOwnerVoidOnFailedRegister`.
- Logging: `TestSQLPathErrorsNotLogged` (literal-SQL errors for cap reached, idempotency conflict and bad JSON leave no
  canary in the PostgreSQL log).

### 13.3 Chain-level semantics (chain/app, deterministic, tests only; B4)

- `TestTimeoutHeightThroughFinalizeBlock`: CheckTx at committed height T admits a transaction with `timeout_height = T`;
  FinalizeBlock at T+1 returns sdk 30; the signer's account sequence and every store are unchanged
  (`FinalizeFullSnapshotForTest`-style snapshot); FinalizeBlock at T commits it.
- `TestAccSeqPresentOnHandlerFailure`: a handler failure (for example `CAPACITY`) keeps the ante events, so the result
  carries `tx.acc_seq=<S>/<N>` and the sequence advanced.
- `TestAccSeqAbsentOnAnyAnteFailure`: an ante failure after SetPubKey (admission refusal of an unadmitted signer) and an ante
  timeout failure both carry no `tx.acc_seq`, and the sequence did not advance. The reason is that `runTx` drops all ante
  events on an ante failure (`SDK/baseapp/baseapp.go:916`), not the decorator order; revision 1 gave the wrong reason.
- `TestStrictDecoderAcceptsTimeoutHeight`.
- `TestSimulateWithEmptySignature`: `/app/simulate` through `app.Query` accepts a `MsgWithdrawContent` transaction with the
  signer's public key and an empty signature and returns gas used.

### 13.4 Devnet acceptance (`chain/devnet/broadcaster_test.go`; `$NETNS`; label "single machine, four validator processes, private network namespace")

Harness. `startDevnet` (`chain/devnet/lease_test.go:442`), the council helper and the lease setup builders (`map-chain.md`
8.2), `reservedAt` (`lease_test.go:354`), the provider rotation pattern (`chain/devnet/registry_test.go:506-512`); the
broadcaster binary built into a temp dir (race-built when the test binary is, fault-tagged); PostgreSQL through
`svn-broadcaster dev-db`; a dev CA and two caller certificates generated in the test; the devnet account-host fixture
signer and the provider inner keys generated in the test (as `registry_test.go:240-245` does); an intent-key registry
file written by the test as the operator. When euid is 0, `signer` and `serve` run under two distinct unused uids (4.1).

Fault proxy. An `httputil.ReverseProxy` on loopback between each `serve` process and validator 0's RPC (one proxy port per
`serve` process). It reads the bounded JSON-RPC body and applies a per-method rule: pass, drop request, forward then drop
response, hold, delay, reset. It counts per method: forwarded, dropped, held, and the SHA-256 of every forwarded
`broadcast_tx_*` body. Test reads go directly to the nodes, never through the proxy. Every injected fault is labelled
"fault injected by a test proxy on loopback" or "build-tagged fault point".

Rules for every phase (F4, F5, F11):
- Precondition. Before each fault the test reads the exact precondition with its SELECT-only database role, logs it, and
  fails with "precondition not reached" if it is missing (for example WD: attempt phase SIGNED, no recorded outcome).
- Quiesce. A phase starts only after the previous phase's intents are terminal and every node reports
  `num_unconfirmed_txs = 0`.
- Accounting. `checkAccounting` (phase Z's function) runs after every phase and at the end.
- Counters and timing. Each phase prints `phase=<name> seconds=<s>`, the proxy counters, and the heights and hashes it
  used; the test prints a per-test budget.
- Phase selection. `SVN_BROADCASTER_PHASES` (a comma list) runs S plus the named phases; unset means all phases. The test
  prints `phases=all` or the list. It exists only so a mutation killer can run one phase; `tests/unit/test_ci_broadcaster.py`
  fails if ci.yml sets it (DR-11-36).
- Randomness. Phase BR uses a seeded RNG and prints the seed.
- Signers with nonzero account numbers. Any phase that needs one picks a signer whose account number is not 0 and logs it
  (`scratchpad/notes-08-10.md:213`).

`TestDevnetBroadcasterLifecycle` phases (one devnet, sequential):

| Phase | Injection | Assertions (all read from the chain unless stated) |
| --- | --- | --- |
| S setup | provider-1 and provider-2 register and are admitted by the council; provider-1 signs one RENEW and one END with its first key, then rotates its key (`RotateProviderKey`); account-host fixture keys written to the registry; callers configured; caller SQL role created | providers ADMITTED; provider-1's key generation advanced; the pre-rotation signatures are kept for PI |
| H happy path | one intent per kind through the API; `REGISTER_CONTENT` and `OFFER_LEASE` also through `app_fixture` + `broadcaster.enqueue_intent` under the caller's SQL role, in one transaction with an application row | each COMMITTED with T11 evidence equal to `/tx` and `/commit`; `own_transaction` true; `content_owners` EFFECTIVE; the SQL-path intents COMMITTED with `commitments` equal to `/tx` |
| A ack versus commit | an acceptance that passes CheckTx and fails `CAPACITY` (as `lease_test.go:507-530`); after the ack the proxy holds the broadcaster's `tx` and `tx_search` calls | the test reads the committed `CAPACITY` result directly from a node while the API still shows SUBMITTED/ACKNOWLEDGED; after release, FAILED(EXECUTION_FAILED CAPACITY); `reservedAt` unchanged |
| TB1 timeout before submission, same bytes | proxy drops the first `broadcast_tx_sync` request | UNCERTAIN; `/tx` checked before the resend; every forwarded broadcast body hashes to H; exactly one attempt row; exactly one transaction at (signer, N) on chain |
| TB2 timeout before submission, deadline | proxy drops every broadcast until height > T+L | attempt DEAD by (d3); new attempt with the same sequence and business id and a higher `timeout_height`; one effect; `reservedAt` equals one package |
| TA timeout after submission | proxy forwards and drops the response | `/tx` finds the commit; every forwarded body hashes to H; COMMITTED with that hash |
| WD worker death after commit | proxy forwards the request, holds the response, and holds the broadcaster's `tx` and `tx_search` calls; the test sees the commit on a node; precondition: attempt SIGNED, no recorded outcome; SIGKILL the broadcaster; restart with pass-through | COMMITTED from the persisted hash; zero new attempt rows; every forwarded body hashes to H; signer `next_sequence` consistent with the chain |
| WP1 death after persist | fault point `after-persist-commit`; precondition: attempt SIGNED, `sends_started = 0`, forward count unchanged; SIGKILL | restart sends the stored bytes; one effect |
| WP2 death after the send was counted | fault point `after-sends-started`; precondition: `sends_started = 1`, no recorded outcome, forward count unchanged; SIGKILL | restart never marks the attempt dead by (d2); it resends or waits for (d3); one effect |
| WP3 death after the send | fault point `after-broadcast-before-record`; precondition: forward count +1, no recorded outcome; SIGKILL | restart finds the commit by `/tx`; COMMITTED; zero new attempts |
| HD held request delivered after death | proxy holds the request; SIGKILL; (a) proxy forwards it later, (b) proxy discards it | (a) reconcile finds the commit; (b) (d3) then one resend; both one effect |
| DU duplicate requests | 20 concurrent creates with one key; same key other body; same key, same body, reordered JSON keys; two keys with the same relay payload | one intent id; 409; 200; both relay intents COMMITTED, the second with `own_transaction=false` and the first's tx as canonical; one `LeaseAccepted` across all blocks (block results scan, `msg_index` aware) |
| FR1 provider commits our business id | proxy holds our relay broadcast; the test reads the business id from the database and commits the identical `AcceptLease` from provider-1's own account; release | our relay commits code 0 with `leases_result idempotent=true`; COMMITTED with `idempotent_replay=true`; canonical = the provider's transaction, found by typed event search |
| FR1b provider commits its own acceptance | proxy holds our relay; provider-1 commits the same acceptance under its own business id; release | our relay commits `NOT_FOUND` (offer closed, `msg_server.go:441-443`); the effect check finds the provider's transaction; COMMITTED with `own_transaction=false` (M6) |
| FR2 business id taken | proxy holds our broadcast; `service-2` commits a different successful message under our pending business id; release | `REPLAY`; one new business attempt with a fresh id; COMMITTED. A rerun of `serve` with the business-attempt bound at 1: FAILED(BUSINESS_ID_TAKEN) |
| FC content front-run | `service-2` registers content id X while our `REGISTER_CONTENT` for X is held; release | our register gets `DUPLICATE_ID`; the effect check finds `service-2` as committing signer; FAILED(OBJECT_TAKEN); `content_owners` VOID; a later `OFFER_LEASE` for X by our caller is refused at enqueue with no transaction (M5) |
| OC offer copy | `service-2` commits a copy of our pending offer (same fields, its own business id) while ours is held; release | our offer gets `DUPLICATE_ID`; the effect check finds `service-2` as signer; FAILED(OBJECT_TAKEN); a relay of an acceptance for that offer by our caller is refused (M7) |
| CS concurrent same signer | 20 concurrent `REGISTER_CONTENT` for `service`; a second `serve` process on the same database with its own proxy port | attempts' sequences unique and dense; all COMMITTED; the chain sequence advanced by exactly the number of included transactions; zero attempt or transition rows and zero forwards from the second `worker_instance`; after SIGKILL of the first, the second takes over only after the lock is released |
| MR foreign use of our sequence (DR-11-15) | proxy holds our relay broadcast at broadcaster sequence N; the test signs a `MsgOfferLease` with the broadcaster key at N and broadcasts it directly; release | the test's transaction commits `FORBIDDEN` with `tx.acc_seq` present (handler failure, sequence consumed); our held send gets CheckTx sdk 32, recorded as MEMPOOL_REJECTED{sdk,32} and not counted for (d2); at T+L the `acc_seq` search names the foreign hash; the signer halts with SIGNER_CONFLICT and forwards nothing more; after `signer-clear` the relay commits at N+1; phase Z subtracts the recorded foreign use (M8, F7) |
| UA unauthorized | caller without the kind (HTTP and SQL path); another caller's intent read; withdraw of another principal's content; SQL-path intent without `PUBLISH_CONTENT`; a role not in `callers`; signer asked directly (test-only client from the serve uid) to sign `MsgOfferLease` with the broadcaster key, a register with an unregistered intent key, `timeout_height` 0 and above the bound; start-up with the authority, auditor-1, provider-1 and seat-1 keys; mTLS against the running binary: no client certificate, a certificate from another CA, zero or two URI SANs, an expired certificate | 403/404 with no row; SQL-path intent FAILED(UNAUTHORIZED) with zero attempt rows and zero forwards; SQL refusal for the unknown role; each signer refusal; start-up refusals; each TLS refusal with no row (F8, F16) |
| PI provider impersonation | relay acceptance signed by the service key, the broadcaster key, provider-2's key, provider-1's retired key (from S), a random key; RENEW and END signed before the rotation | pre-check refusal, no attempt; forced chain check: the test signs the forged acceptance with provider-1's own account as outer signer and broadcasts it: `UNAUTHORIZED` (the inner signature is checked before the account, `msg_server.go:446-452`); `reservedAt` and held leases unchanged (M8) |
| FW forwarded provider transaction (only if Q2 includes P3) | a provider-1 account-signed acceptance forwarded verbatim; one without `timeout_height`; one with a forged inner signature | the first commits by its own hash; the others are refused with no row |
| PB publication permission | register without `PUBLISH_CONTENT`; with an intent key not registered for the principal; withdraw by another principal | refused, no row, no transaction |
| PG PostgreSQL restart | precondition: at least one intent in each of QUEUED, SIGNED and ACKNOWLEDGED; `pg_ctl -m immediate stop`; restart | the broadcaster forwards nothing while it cannot persist (proxy counter flat); after restart every intent reaches the chain's truth; the harness role is SELECT-only, so no manual history edit is possible |
| SG signer death | fault point `after-decode-before-reply` in the signer; SIGKILL the signer | no attempt row, no consumed sequence, no broadcast; intents stay REQUESTED; after the signer restarts they commit (F21) |
| BR broadcaster restart | a seeded, fixed number of SIGTERM and SIGKILL at random points of a 50-intent batch across both signers | after the faults stop, every intent is terminal within a bounded wait and its API status equals a recomputation from chain reads |
| RS DR-10-41 | stop two validators, restart validator 0 (it cannot commit), send from the signer with a nonzero account number (logged) | CheckTx sdk 4 recorded in `transitions`; after quorum returns, COMMITTED (F13) |
| EX expired and dead | proxy drops broadcasts past T+L and past `expires_at` | FAILED(EXPIRED_DEAD); no forwarded broadcast after `expires_at` |
| L log canary | 7.7 canaries generated at run time | each canary processed (asserted first); zero hits in broadcaster, signer, PostgreSQL and node logs and the go test output; bounded copies of the logs kept for the evidence |
| Z final accounting | none; `checkAccounting` also runs after every phase | see below |

`checkAccounting` (F5):
- (a) For each signer and every sequence 0 to chain_seq-1, `tx_search "tx.acc_seq='<S>/n'"` returns exactly one hash, and it
  equals an attempt row of this outbox or a recorded `signer_events` foreign use. This catches ghost or unrecorded
  submissions.
- (b) For each intent's object, the number of committed transactions carrying its typed event is exactly 1 for COMMITTED
  and 0 for FAILED (except `OBJECT_TAKEN`, where it is 1 and the signer is not ours), and `commitments.tx_hash` or
  `first_commit_tx_hash` equals that transaction.
- (c) Every intent is terminal within a bounded wait after faults stop, and its API status equals a recomputation from chain
  reads.
- (d) The per-caller open-intent counter rows equal the number of non-terminal intents (0 at the end): the outbox's own
  capacity leak check.
- (e) For each provider, `reserved_bytes` equals the sum of `package_bytes` of its held leases. Labelled "chain invariant,
  not broadcaster mutation evidence": x/leases maintains it whatever the broadcaster does.

`TestDevnetBroadcasterQuorumStall` (separate devnet; F1):
1. Quiesce: the proxy holds every broadcast from the broadcaster; wait until every running node reports
   `num_unconfirmed_txs = 0` and no intent is IN_FLIGHT; no offer or lease has a deadline inside the window.
2. Stop validator 4 by hand, then call `stopForStall(..., 2)` (`chain/devnet/prove_test.go:263`), then `observeStall`
   (`:312`), as the existing two-down pattern does (`prove_test.go:115-141`). These helpers require that no transaction
   commits in the window (`prove_test.go:333-342`; `chain/devnet/status.go:115-131`), so nothing is released before this
   point.
3. Create the in-flight attempt: release one held broadcast to node 0 (CheckTx runs without quorum) and assert ACKNOWLEDGED
   in the database. Create one queued intent and one short-expiry intent.
4. Keep the stall for at least the minimum intent lifetime + T + 2 poll intervals, with a test-owned loop that re-reads
   heights at every sample.
5. Assert during the stall: zero attempt rows and zero proxy-forwarded broadcasts for the queued intent (kills the "allow
   new attempts while stalled" mutant); a `stall_episodes` row is open; the API shows COMMITTED for an intent if and only if
   a direct node `/tx` read shows it committed (never, here); the short-expiry intent becomes FAILED(EXPIRED_NOT_SENT); the
   queued intent is REQUESTED with its business id and no transaction on chain (the reference's observable state, DR-11-19).
6. Restart the validators: the in-flight intent ends COMMITTED (pinned: the mempool has no TTL and heights did not advance
   during the stall); the queued intent commits; the episode closes.
7. Observer check: kill only the worker's RPC link (proxy reset on `status`) while the chain runs; the API shows
   `OBSERVER_UNAVAILABLE`, not PENDING_STALL (m4).
EXPIRED_DEAD is tested in the non-stall phase EX of the lifecycle test.

`tx.acc_seq` search and typed event search run against the real node in phases TB2, DU, MR and every `checkAccounting`;
their results are logged, which turns the "derived from source" items of 1.1 into tested ones.

### 13.5 Fuzz and property tests (real `-fuzz` runs logged)

- `TestStateMachineProperty` and `FuzzTransitions` (internal/outbox): random bounded sequences of observations (acks,
  unknown sends, cache answers, refusals, crashes between `sends_started` and the record, `/tx` results with and without
  `acc_seq`, heights, account sequences, search results, restarts, expiry, stall episodes, stale observer, cancel races)
  against the transition table. Invariants: COMMITTED only via T5/T11/T12 with a committed observation; at most one live
  attempt per signer; terminal states absorb; no new attempt while one is live; no send after expiry; (d2) never with an
  unrecorded send; sequences dense. A reachability check shows each transition is hit.
- `FuzzDecodeIntentRequest` (api), `FuzzForwardedTx` (policy: never panics, refuses anything but one allowed message),
  `FuzzClassifyRPCError` (chain).

### 13.6 Static and binary checks

- `tests/unit/test_chain_writer_policy.py` (REQ-INT-001). It fails if `services/api`, `services/indexer`, `workers/`,
  `apps/web` or `packages/` contain a signing-key loader, `broadcast_tx`, `BroadcastTx`, `SignWithPrivKey`, a
  `cosmos.tx.v1beta1` builder, or (in `apps/web`) a CometBFT websocket or `subscribe` use. Go chain writers are allowed only
  at exact paths: `services/broadcaster/internal/signer/`, `services/broadcaster/internal/chain/`,
  `agents/auditor/chainclient/{tx.go,rpc.go}` (its own AUDITOR path, goal 09 Q1),
  `agents/auditor/chainclient/chaintest/{chaintest.go,serve.go}`, `chain/internal/statecheck/testhome/testhome.go` (test-only
  package; `SignWithPrivKey` at `:265`), and `_test.go` files. A planted new file anywhere else must fail, as must each other
  planted fixture (as `test_ci_devnet_coverage.py` does with mutated documents).
- Go: `TestNoSigningOutsideSigner` (go/ast scan of the module: no `ed25519.Sign`, `PrivKey.Sign`, `SignWithPrivKey` outside
  `internal/signer`), `TestNoLocalPreimageCopy` (no `SVN-PROVIDER-` or `SVN-HOSTING-INTENT-` literal in the module),
  `TestConfigHasNoIntentKey`, `TestNoLeaseOrProjectionTable` (schema has no lease, offer or content state table besides
  ownership), `TestDefaultBuildHasNoFaultPoints` (`go tool nm` of the default build lists no `faultpoint` symbol).

### 13.7 Mutation runner (`docs/evidence/goal-11/mutants.py`, B10; design of goals 08-10)

Each mutant names its killers. Every group has a unit or PostgreSQL-level killer; a devnet killer is added only where noted
and runs S plus its phase (`SVN_BROADCASTER_PHASES`) with an explicit `-timeout` (F10).

| Group | Mutant | Killer(s) |
| --- | --- | --- |
| commit guard | ack sets COMMITTED | `TestAckNeverCommits`, `TestNoCommittedWithoutCommittedResult` |
| commit guard | T11 without the typed-event check | `TestCommitNeedsTypedEvent` |
| commit guard | T12 without the first-commit lookup | `TestIdempotentReplayNeedsFirstCommit` |
| commit guard | effect comparison ignores the signer | `TestEffectComparison`; devnet OC |
| commit guard | DEAD-hash match reported as another account's commit | `TestDeadAttemptHashMatchIsIntegrityAlert` |
| dead rule | drop the L margin | `TestDeadRuleD1D2D3` |
| dead rule | (d2) ignores UNKNOWN sends | `TestDeadRuleD1D2D3` |
| dead rule | (d2) ignores `sends_started` | `TestZeroRecordedSendsNeverDeadByD2`, `TestCrashBetweenSendsStartedAndRecord`; devnet WP2 |
| dead rule | sdk 32 or CACHE counted as a refusal | `TestSdk32AndCacheNeverDefinitive` |
| dead rule | local clock decides death | `TestDeadRuleD1D2D3` (clock-free fake) |
| dead rule | sequence consumed inferred from the code | `TestSequenceConsumedOnlyWithAccSeq` |
| reconcile | skip the effect check before a new attempt | `TestReconcileBeforeResend` |
| reconcile | map a resend's code to the first attempt | `TestResendCodeNeverMapsToFirstAttempt` |
| reconcile | committed failure goes to FAILED without the effect check | `TestCommittedFailureRunsEffectCheck` |
| reconcile | state change without a transaction reported COMMITTED | `TestStateChangeWithoutTxIsObjectStateChanged` |
| reconcile | rebroadcast after expiry | `TestNoSendAfterExpiry` |
| persist order | broadcast before the attempt row commits | `TestFakeRPCSeesPersistedAttempt` |
| persist order | no `sends_started` increment before a send | `TestFakeRPCSeesPersistedAttempt` |
| persist order | drop the compare-and-set on the intent row | `TestCancelRacesPersist`, `TestExpiryRacesPersist` |
| persist order | drop the terminal-row trigger | `TestTerminalRowsImmutable` |
| fencing | ignore advisory lock loss | `TestLockLossStopsSending` |
| fencing | `reconcile-once` without the lock | `TestReconcileOnceTakesLock` |
| sequence | skip the stale-node height check | `TestAccountReadHeightBound` |
| sequence | skip the signer-conflict halt | `TestSignerConflictHalts` |
| sequence | new attempt reuses an earlier `timeout_height` | `TestNewAttemptTimeoutAboveEarlier` |
| allowlist | remove one type URL check in the signer | `TestSignerRefusesOtherTypeURLs` |
| allowlist | allow `authority_end` | `TestSignerRefusesAuthorityEnd` |
| allowlist | drop the intent-key registry check | `TestSignerRefusesUnregisteredIntentKey` |
| allowlist | drop the timeout bound | `TestSignerRefusesUnboundedTimeout` |
| allowlist | accept any peer uid | `TestSignerPeerUID` |
| provider pre-check | verify against the message key instead of the registered key | `TestProviderPrecheck` |
| publication | skip the registered-key check | `TestPublicationPermission` |
| publication | skip the owner check on withdraw | `TestPublicationPermission` |
| ownership | a PENDING owner row allows an offer | `TestOfferNeedsEffectiveOwner` |
| idempotency | drop the unique key | `TestIdempotencyUniqueUnderConcurrency` |
| idempotency | digest over raw JSON | `TestRequestDigestCanonical`, `TestIdempotencyConflict` |
| stall | allow new attempts while stalled | `TestStallBlocksNewAttempts`; devnet stall test |
| stall | stale observer shown as a stall | `TestObserverStaleNotStall` |
| redaction | log the request body | `TestRedactorDropsRequestBody` |
| redaction | skip the PG*/PQGO_DEBUG refusal | `TestStartRefusesPGEnv` |
| config | skip the chain id check | `TestRefusesChainIDMismatch` |

Run constraints: the runner copies the tree like goals 08-10 (`docs/evidence/goal-10/mutants.py:7-8,2180`), so it runs
only after the orchestrator approves the temp copy, with `--gocache` pointing at the existing GOCACHE, after a `df` check
(section 18), and inside `$NETNS` for any devnet killer. Devnet phases that no mutant targets are end-to-end
demonstrations, not mutation evidence, and the evidence says so.

### 13.8 Regression on the same revision

Goal 08, 09, 10 runners with `--check` and full runs (each under the same disk rule); the goal 06 and 07 anchor checks
(known BLOCKED); every existing Go, Python and Node suite; both contract checkers; the secret scan; the SBOM and workflow
policy checks.

---------------------------------------------------------------------------------------------------

## 14. Acceptance and requirements-to-tests matrix (forecasts under the recommended answers; not results)

| Item (goal spec) | Tests | Mutant group | Expected status |
| --- | --- | --- | --- |
| Timeouts before and after submission (:35) | TB1, TB2, TA, HD; `TestDeadRuleD1D2D3`; `TestTimeoutHeightThroughFinalizeBlock` | dead rule, reconcile | VERIFIED (devnet) |
| Worker death after commit (:35) | WD, WP1-WP3, SG | persist order, reconcile | VERIFIED (devnet, real kills and build-tagged fault points) |
| Duplicate requests (:35) | DU, FR1, FR1b, FR2; `TestIdempotencyUniqueUnderConcurrency` | idempotency, commit guard | VERIFIED |
| Concurrent same-signer requests (:36) | CS, MR; `TestAdvisoryLockFencesSecondWorker`, `TestOneLiveAttemptPerSequence` | fencing, sequence | VERIFIED |
| One business effect, no leaked capacity, eventual accurate status (:36-37) | `checkAccounting` (a)-(d) after every phase; DU block scan; FC, OC | commit guard, reconcile | VERIFIED (devnet); (e) reported as a chain invariant only |
| Reject unauthorized messages (:38) | UA, MR; 13.1 allowlist tests | allowlist | VERIFIED for the Q1 allowlist |
| Reject provider impersonation (:38) | PI; `TestProviderPrecheck` | provider pre-check | VERIFIED (the broadcaster refusal and the forced chain refusal) |
| Stop quorum: pending or honest failure (:38-39) | `TestDevnetBroadcasterQuorumStall`; EX | stall, commit guard | VERIFIED (devnet) |
| Restart PostgreSQL and broadcaster, reconcile from persisted state (:40) | PG, BR, WD; `TestImmediateStopKeepsCommittedAttempts` | fencing, persist order | VERIFIED (devnet, process crashes, not power loss) |
| Req. 1 typed requests, outbox fields, one-transaction coupling (:18-20) | 6.2 schema tests; `TestEnqueueAtomicWithApplicationRow`; phase H SQL path | idempotency | VERIFIED for the fixture application schema; the API's own use DEFERRED (goal 13) |
| Req. 2 approved service keys only; provider signs; forward path (:21-23) | 7.1-7.3 tests; UA; PI; FW | allowlist, pre-check | BLOCKED: "approved" needs the boundary document approval (DR-11-01). Sub-results under it: key allowlist and provider path VERIFIED (devnet) |
| Req. 3 sequence, retries, backoff, deadlines, uncertainty, classification (:24-26) | 13.1 chain and outbox tests; TB, TA, RS, MR | dead rule, sequence | VERIFIED (devnet) for sequence, deadlines, uncertainty and classification; backoff and attempt caps unit-verified with fakes, labelled so |
| Req. 4 states, authentication, object permissions, outage queue, no final leases (:27-29) | 5.8 view tests; UA; stall test; `TestNoLeaseOrProjectionTable` | stall, ownership | VERIFIED; "uncertain" shown as SUBMITTED/UNCERTAIN (DR-11-09) |
| Req. 5 signing boundary, redaction, explicit chain id and gateway, no live credential (:30-32) | L; `TestSignerRefusesForeignChainID`; config tests; netns run; uid boundary run | redaction, config, allowlist | BLOCKED: "approved" (DR-11-01). Sub-results: the development boundary VERIFIED only from a logged root run with distinct uids, else IMPLEMENTED (process split, same uid); redaction and configuration VERIFIED |
| Handoff: traces, schema, API, boundary doc (:43) | `traces.txt`; README; `docs/architecture/broadcaster.md` | | IMPLEMENTED (doc); boundary approval BLOCKED until asked at PR time |
| Handoff: stable identities, canonical commitment evidence (:43-44) | H, DU, FR1, `checkAccounting` compare `commitments` with `/tx` and `/commit` | commit guard | VERIFIED as recorded; goal 12 consumption DEFERRED |
| Head CI on the tested commit | ci.yml shards | | BLOCKED until the owner authorizes a draft PR |

| Requirement | Assigned TEST (short) | Goal 11 tests | Expected |
| --- | --- | --- | --- |
| REQ-CHAIN-013 (`docs/requirements.md:452`) | broadcaster fixture cannot mint a provider signature or skip the publication permission bit | PI, PB, UA, `TestNoSigningOutsideSigner`, `TestConfigHasNoIntentKey`, `TestSignerRefusesUnregisteredIntentKey` | BLOCKED (DEC-001, DR-07-01: no chain-level creator binding; DR-11-01: boundary not approved). Sub-results listed under the one status: provider half VERIFIED (devnet); publication check of the broadcaster's code path VERIFIED (devnet) under Q3 (a), not against a holder of the service key bytes (7.4); the caller's permission claim DEFERRED to goals 13/19/21 |
| REQ-ACK-001 (`:483`) | ack stays submitted until inclusion and execution; reconcile a timeout before retry | A, TB1, TB2, TA, stall test, `TestAckNeverCommits` | VERIFIED (devnet) |
| REQ-INT-001 (`:496`) | goal 11 is the only chain writer used by the API; web client does not subscribe to every block | `test_chain_writer_policy.py` with planted fixtures | DEFERRED (goals 13, 21): the API and the web client are scaffolds today. Goal 11 result: guard IMPLEMENTED; planted fixtures fail |
| REQ-IDEM-001 (`:500`) | retry one business id after a timeout, single committed effect | TB2, WD, DU, FR1, FR2, `checkAccounting` | VERIFIED (devnet, real PostgreSQL) |
| REQ-QA-008 (`:594`) | repeat one operation across timeout and restart, one transition, no leaked reservation | WD, WP1-WP3, PG, BR, `checkAccounting` | VERIFIED (devnet, process crashes) |
| Regression rows (goal 08 REQ-CHAIN-012, goal 06 linked-binary tests) | must stay green | 13.8 | regression only |

A row whose owner question is unanswered is not forecast: Wave 1 does not start without the answers (17.1).

---------------------------------------------------------------------------------------------------

## 15. Evidence capture list (Wave 3, after the orchestrator commits; `scripts/evidence/capture.sh <log> -- <cmd>`; logs under `docs/evidence/logs/goal-11/`; run from `$WT`)

Filtered captures keep go test's exit status: `bash -c '<cmd> 2>&1 | grep -a -v -F "<nil> DBG "; exit "${PIPESTATUS[0]}"'`
(written `FILTER(<cmd>)`), as goals 08-10 did. Every command that starts a validator or PostgreSQL runs under `$NETNS`
(`scratchpad/brief.md` section 0; `scratchpad/notes-08-10.md:207`). One devnet or PostgreSQL run at a time; `df -h /` is
checked before each (section 18).

```
model.txt                     the orchestrator saves the get_session output (model, last served model, effort) at the
                              goal's start and end; capture.sh runs a committed extraction script over the saved files;
                              the evidence labels it as tool output saved by the orchestrator
runtime.txt / revision.txt / diff-stat.txt   go version, postgres --version (18.6), HEAD, tree, base 6d74aca
go-list-chain-before.txt / go-list-chain-after.txt   go -C chain list -m all on 6d74aca and on the tested commit (identical)
svnd-deps-before.txt / svnd-deps-after.txt           go version -m of an svnd build on both revisions (same dependencies)
go-mod-tidy-broadcaster.txt   go -C services/broadcaster mod tidy -diff (no change; no new third-party module)
go-sum-subset.txt             every services/broadcaster/go.sum line is in chain/go.sum
check-requirements.txt        python3 docs/evidence/goal-01/check_requirements.py (SOURCE_ALLOW and PY_ALLOW edits only)
unit-broadcaster.txt          $NETNS go test -count=1 -v ./services/broadcaster/...   (unit + PostgreSQL integration)
race-broadcaster.txt          $NETNS go test -race -count=1 ./services/broadcaster/...
uid-boundary.txt              $NETNS go test -count=1 -v -tags svn_roottest -run 'TestServeUIDCannotReadKeyFiles|TestSignerRefusesOtherUIDPeer' ./services/broadcaster/internal/signer   (root only; not run in CI)
app-deadline.txt              FILTER(go test -count=1 -v ./chain/app -run 'TestTimeoutHeight|TestAccSeq|TestStrictDecoderAcceptsTimeoutHeight|TestSimulateWithEmptySignature')
devnet-broadcaster.txt        $NETNS go test -count=1 -timeout 1800s -v -run '^TestDevnetBroadcasterLifecycle$' ./chain/devnet
devnet-broadcaster-stall.txt  $NETNS go test -count=1 -timeout 1200s -v -run '^TestDevnetBroadcasterQuorumStall$' ./chain/devnet
devnet.txt                    $NETNS go test -count=1 -timeout 1800s -v -skip 'TestDevnetBroadcaster|TestDevnetAudit|TestDevnetUpgrade|TestDevnetMinority|TestDevnetGovernedChange' ./chain/devnet   (regression)
fuzz-<Name>.txt               go test -run='^$' -fuzz=^Name$ -fuzztime=60s -count=1 <pkg>  for each 13.5 target
traces.txt                    capture.sh traces.txt -- grep -a '^trace ' docs/evidence/logs/goal-11/devnet-broadcaster.txt
                              (the test prints one `trace` line per transitions row at the end of each phase)
canary.txt                    the phase L lines of devnet-broadcaster.txt: per log scanned, its size and sha256 and the
                              hit count (copies of the dev logs are not kept: they live in deleted temp dirs, and copying
                              them into Git would commit a secret if the redactor failed)
chain-writer-policy.txt       .venv/bin/pytest -v tests/unit/test_chain_writer_policy.py tests/unit/test_ci_broadcaster.py tests/unit/test_ci_devnet_coverage.py
tests-go.txt                  $NETNS env GOFLAGS=-count=1 .venv/bin/python scripts/run_tests.py --only go   (exactly as goal 10)
tests-python.txt / tests-node.txt   .venv/bin/python scripts/run_tests.py --only {python,node}
ci-go-job.txt                 the go job's commands, with the PostgreSQL install already present
ci-shards.txt                 $NETNS the go-race shard commands of ci.yml, including broadcaster, devnet-broadcaster and devnet-broadcaster-stall; per-shard seconds
gofmt-vet.txt                 bash scripts/gofmt_check.sh && go vet ./chain/... ./agents/... ./proto/... ./services/broadcaster/...
check-protocol.txt / scan-secrets.txt / sbom-check.txt / workflow-policy.txt
protocol-diff.txt             git diff --stat 6d74aca..HEAD -- docs/protocol proto docs/evidence/goal-02   (must be empty)
chain-prod-diff.txt           git diff --stat 6d74aca..HEAD -- chain ':!chain/**/*_test.go' ':!chain/**/README.md'   (must be empty)
goal01-diff.txt               git diff 6d74aca..HEAD -- docs/evidence/goal-01   (exactly the SOURCE_ALLOW and PY_ALLOW edits)
tests-not-removed.txt / tests-replaced.txt   the scaffold assertion and path pin and their replacements; the
                              test_ci_devnet_coverage.py constants ("constants only")
mutants-goal11.txt            $NETNS docs/evidence/goal-11/mutants.py --rev <tested commit> --gocache <existing GOCACHE>
mutants-goal10/09/08.txt and their --check logs; goal06-anchors.txt / goal07-anchors.txt   (all under $NETNS)
golangci-lint.txt             recorded BLOCKED with its command if it still cannot run (goals 07-10)
pgdev-nonroot                 BLOCKED until head CI: the non-root path (the CI runner) cannot run here, because the
                              workspace is root and `postgres` cannot enter /tmp/claude-0
disk.txt                      df -h / and du -sh of the Go build cache before and after; temp clusters and binaries removed
head CI                       BLOCKED until the owner authorizes a draft PR; then run id and per-job result
```

Every number in `docs/evidence/goal-11.md` comes from these logs on the committed tree (DIRTY `no`). The evidence file
records: model `claude-opus-5-5`, effort `ultracode`, the playbook assignment GPT-6 Astra HIGH (goal spec :6), the owner's
two quoted sentences (`scratchpad/brief.md` section 5), any fallback the model capture shows (flagged for the owner), the
PostgreSQL version, the single-machine label on every devnet claim, the fault labels ("fault injected by a test proxy on
loopback", "build-tagged fault point", "process crash, not host power loss"), the uid boundary actually used
(`boundary_uids=` line), and each DR-11 answer as quoted owner text.

---------------------------------------------------------------------------------------------------

## 16. Expected status, BLOCKED, DEFERRED, residual risks (forecast)

Expected goal status: BLOCKED. Reasons, each recorded with its command or decision:

- Prerequisite "approved service/provider signing boundaries" (goal spec :8): no approved document today. Recorded BLOCKED
  (DR-11-01) until the owner approves `docs/architecture/broadcaster.md` at a pinned commit and sha256, asked at PR time.
- Prerequisite goals 04, 07, 08, 09, 10 are BLOCKED in their own status (`docs/progress.md:34-40`).
- REQ-CHAIN-013 stays BLOCKED (DEC-001, DR-07-01): no chain-level creator binding.
- Production paths: content registration `DECISION_BLOCKED` off the devnet (DEC-001, DR-07-01); offers and renewals need the
  DEC-005 devnet number.
- Head CI until a draft PR is authorized; the non-root pgdev path until head CI; golangci-lint (built with go1.25); goal 06
  and 07 mutation anchors.

DEFERRED: the API's runtime use of the broadcaster and REQ-INT-001 (goals 13, 21), creator UI states (goal 21),
provider-side submission P2 (goal 16), indexer consumption of commitment evidence (goal 12), service-key rotation and
signer recovery drills (goal 26), production certificate issuance, rotation and packaging (goal 27; development mTLS is
goal 11's), throughput numbers (goal 28), the shared client package (goal 16), goal 09's "Outbox submission" item until
the Q2 answer (DR-11-06).

Residual risks (each written into the evidence file):

- Reads trust one configured node; a lying node can make the broadcaster report COMMITTED. Goal 12 reconciles; no light
  client is claimed.
- No on-chain revocation of a compromised `ADMITTED_SERVICE` or `BROADCASTER` key; only an engine-level change can remove it
  (`map-decisions.md` section 6). Least privilege is the main control.
- Whoever holds the `ADMITTED_SERVICE` key bytes can mint devnet hosting intents with a key they make up; creator permission
  is not enforced on chain (`chain/x/content/README.md:61`). The broadcaster's registry check binds only its own code path.
- A compromised `serve` (with distinct uids) can still send any `MsgOfferLease` the chain accepts, relay any stored provider
  signature, replay a stored hosting intent for its own object, withhold or reorder submissions, and read the outbox (7.2).
  With one uid, it can also read the key files.
- Provider inner signatures have no chain id and RENEW/END have no expiry; a leaked outbox row can be submitted by any
  `BROADCASTER` or the provider account until a renewal or rotation. Local expiry revokes nothing on chain.
- Business-id squatting: an admitted account can take a pending business id; the broadcaster spends a bounded number of
  business attempts and then fails (DR-09-28, DR-10-16). Denial of service, never a wrong transition.
- A broadcaster can delay or withhold a provider's signed acceptance until the offer expires (P2 is the remedy).
- Window of one per signer limits throughput to about one transaction per block per signer; block capacity is about
  `20,000,000 / declared gas` transactions (`map-chain.md` 1.5).
- `LISTEN/NOTIFY` is a hint only; correctness rests on polling under per-signer advisory locks and compare-and-set updates.
- Mempool gossip timing and same-sequence transactions in different mempools are outside the broadcaster's control
  (`map-chain.md` 2.2); the tests prove outcomes, not mempool internals.
- An effect can land after the caller's `expires_at`, up to the live attempt's `timeout_height` (`last_landing_height`).
- PostgreSQL crash safety depends on `fsync=on`; the outbox is not backed up in goal 11 (goal 26). The log settings of 6.5
  are checked only on the dev cluster; an operator must apply them.
- The stall report differs from the reference outbox: no `ERROR_CODE_QUORUM` is returned (DR-11-19).
- The raw `/store` path stays unmetered on the node; the node RPC must stay private (`scratchpad/notes-08-10.md:61-63`).
- The fault proxy proves the broadcaster's handling of lost requests and answers on one loopback link, not real network
  partitions. PostgreSQL faults are process crashes, not host power loss.

---------------------------------------------------------------------------------------------------

## 17. Decision requests (interim choices are reversible and are not owner approval)

### 17.1 Owner questions: answer BEFORE Wave 1 (`scratchpad/goal-11/owner-questions.md` holds the same four, self-contained)

- Q1 (DR-11-01) [SEC] Which keys may the broadcaster hold, for which messages, behind which development boundary.
  Recommended (a): one `BROADCASTER` key and one `ADMITTED_SERVICE` key, in a separate signer process under its own uid,
  with the allowlist, intent-key and deadline checks of 7.2; never AUTHORITY, seat, AUDITOR or provider keys.
- Q2 (DR-11-02, DR-11-06) [SEC] Which externally signed submissions the broadcaster may carry: P1 relay, P3 verbatim forward,
  P2 later, and audit batches. Recommended (a): P1 and P3 now, P2 in goal 16, audit batches stay on the auditor's path.
- Q3 (DR-11-03) [SEC] What the publication permission bit is, who holds devnet hosting-intent keys, and the object
  permissions. Recommended (a): an operator-registered intent key per principal, the broadcaster holds none, ownership counts
  only after the chain shows our registration.
- Q4 (DR-11-04) [privacy] What the outbox stores and how it is minimized. Recommended (a): the minimal fields of 5.5, digests
  in answers, and clearing of reusable authorizations from rows that ended without any send.

Rule (one rule, replacing revision 1's two): Wave 1 starts only after all four answers. No answer-dependent part is
implemented as an interim, and no matrix row is forecast for an unanswered question.

The orchestrator sends Q1-Q4 in one message and says in it, as notes, not questions: the boundary document's approval is
asked at PR time on a pinned commit and sha256; the language choice (DR-11-20) and the goal 01 `SOURCE_ALLOW` edit it
causes; the replaced scaffold test and path pin; the CI changes (DR-11-25, new check names); the interim rules of 17.2, each
labelled fail-closed or not.

### 17.2 Security-relevant requests with interim rules (implemented, tested, reported open)

- DR-11-05 Commit, failure, uncertainty and deadlines (fail-closed): COMMITTED only from a committed result (9.4); FAILED only
  from a committed code after the effect check, a definitive refusal (d2), or chain proof that the attempt cannot commit
  (d3); every attempt carries `timeout_height`; `[Educated Guess]` not a protocol change (`docs/protocol.md:187` constrains
  only fee and gas), and goal 11 tests the ante semantics (13.3).
- DR-11-06 Audit batches: open, part of Q2. Goal 09's "Outbox submission: Goal 11" (`docs/evidence/goal-09.md:39`) stays
  DEFERRED. `[Educated Guess]` Goal 09 Q1 ("No `BROADCASTER` relay", `:74`) settles that no broadcaster key signs a batch;
  it does not settle whether AUDITOR-signed bytes may be forwarded verbatim. Until the answer, no audit-batch kind exists.
- DR-11-07 Business ids (fail-closed against prediction, not against front-running): random per business attempt, stable
  across its resends; a new id only after reconciliation shows no effect; bounded business attempts. Signer-bound ids are a
  protocol addition (DR-09-28).
- DR-11-08 Reconciliation read path (fail-closed): `/tx` by hash with a hash check, `/tx_search` by `tx.acc_seq` and by typed
  event, gRPC-routed object queries, height-bound account reads, `/commit` for block hashes, `/app/simulate` for withdraw
  gas only; never raw `/store` or `/check_tx`; one configured node, disclosed as trusted for reads (`docs/blueprint.md:391`).
- DR-11-09 Caller-visible status: the five frozen `OperationStatus` values plus `detail.phase` (UNCERTAIN, AWAITING_DEADLINE,
  RETRY_WAIT), `detail.observer` and `last_landing_height`. Adding an enum value is a protocol change (DR-11-40).
- DR-11-10 PROPOSED numbers (not fail-closed: new numbers): the table in section 11, each with hard code bounds. The owner is
  asked to accept the labels, not the values.
- DR-11-11 Stall rule (fail-closed): stall episodes are recorded; no new attempt while stalled; live attempts are never
  abandoned and are not resent after expiry; an intent with nothing sent fails at its expiry; a stale observer is reported
  as such, not as a stall.
- DR-11-12 Not routed (fail-closed): council messages, `MsgExecuteChange`, `authority_end`, provider registry messages
  (`RotateProviderKey` must be reconciled by query, `chain/x/providers/README.md:126-128`).
- DR-11-13 Service-key rotation (`RotateServiceSubmitter`, engine-only) leaves content `submitting_host` and offer schedulers
  on the old address; intents pin their signer; rotation is goal 26. Open.
- DR-11-14 One message per transaction (fail-closed).
- DR-11-15 Signer conflict (fail-closed): a foreign use of a broadcaster key's sequence halts that signer until an operator
  runs `signer-clear`, which records the acknowledgement in `signer_events`.
- DR-11-16 Threat-model additions (confused deputy, log leakage, business-id squatting, stored authorizations, same-uid
  boundary): asked, not edited (`docs/threat-model.md` is a goal 01 document).
- DR-11-17 REQ-CHAIN-013 is recorded as one status, BLOCKED, with its sub-results listed under it (section 14); the owner
  confirms the reading.
- DR-11-18 DR-10-41 handling: sdk 4 is retryable a PROPOSED number of times, at least two blocks apart; then FAILED and a
  signer alert. Not fail-closed for liveness.
- DR-11-19 Stall report versus the reference outbox: the request is parked with its business id, as in `cases:64-77`, but no
  `ERROR_CODE_QUORUM` is returned (`docs/protocol.md:129`). A recorded spec difference, like DR-10-44; tested by the
  reference's observable state (13.4 stall test step 5).
- DR-11-34 Object ownership (fail-closed): a content owner row counts only after the register committed under our service
  address and the chain shows that host; a failed register voids it; conflicts get one uniform answer; offers and relays
  require that ownership (5.7).

### 17.3 Reversible implementation choices (documented as interim; no owner input needed)

- DR-11-20 Go module at `services/broadcaster`, replacing the Python scaffold, its test and its path pin by design (section
  3, section 12). It requires adding `services/broadcaster/` to the goal 01 checker's `SOURCE_ALLOW`, a goal 01 contract
  change disclosed with Q1-Q4.
- DR-11-21 Copy and generalise the auditor client; no auditor refactor (4.4).
- DR-11-22 lib/pq v1.12.3 as the only new direct dependency, already in `chain/go.sum` and svnd; opened with an explicit
  `pq.Config` and `NewConnectorConfig`.
- DR-11-23 Embedded `.pgsql` migrations and a small forward-only runner (`.gitignore:70` ignores `*.sql`; no negation,
  which would weaken the dump rule).
- DR-11-24 One cluster helper (`internal/pgdev`) used by tests and by `svn-broadcaster dev-db`; crash hooks only in test code
  (`internal/pgtest`, `chain/devnet`); `dev-db` refuses data directories outside `os.TempDir()/svn-pg-*` and a chain id
  without the `shortvideo-dev-` prefix; PostgreSQL 18.6; SQL compatible with 16.
- DR-11-25 CI: PostgreSQL installed in the `go` job and in the `broadcaster` shard; shards `broadcaster`,
  `devnet-broadcaster` and `devnet-broadcaster-stall`; the pinned PGDG step with an archive fallback; no PostgreSQL skip
  variable; new required check names need the owner's branch-protection update.
- DR-11-26 One live transaction per signer.
- DR-11-27 Fixed per-kind gas; withdraw gas from `/app/simulate` with a PROPOSED margin, capped at 20,000,000.
- DR-11-28 A second intent with an identical effect reports COMMITTED with `own_transaction=false`.
- DR-11-29 Devnet tests in `chain/devnet` driving the built binary through a loopback fault proxy and build-tagged fault
  points.
- DR-11-30 JSON bodies, no new `.proto`; status names reuse `OperationStatus`.
- DR-11-31 Internal phase names beyond `docs/protocol.md:217`.
- DR-11-32 Caller authentication: mTLS with a run-time development CA on loopback, plus one PostgreSQL role per caller for
  the SQL path. Alternatives: Unix socket with `SO_PEERCRED`, or per-caller bearer tokens.
- DR-11-33 `[Educated Guess]` "Updates the application projection" (`docs/blueprint.md:305`) means intent status and
  commitment evidence only; projections are goal 12's (section 2.2).
- DR-11-35 Distinct uids for `signer` and `serve` when the launcher is root; same-uid runs are labelled and do not verify the
  boundary (4.1).
- DR-11-36 `SVN_BROADCASTER_PHASES` selects devnet phases for mutation killers only; CI never sets it (guarded).
- DR-11-37 Dev cluster logging settings of 6.5.

### 17.4 Protocol additions recorded, not implemented (DR-11-40)

An `UNCERTAIN` `OperationStatus` value; business ids bound to the signer (DR-09-28); chain id and expiry inside provider
inner payloads and hosting intents (DR-07-01, DR-08-04); get-by-id queries for leases, offers and business ids (DR-08-08);
a registered hosting-intent key per creator on chain (DR-07-01); an on-chain revocation of service and broadcaster keys.

---------------------------------------------------------------------------------------------------

## 18. Work packages

Rules for every package:
- Work only in `$WT`; one owner per file (paths listed); no `git commit`.
- Run only the package's own commands (4 CPUs, other work runs; no full suites outside the gate). One devnet or PostgreSQL run
  at a time across all packages.
- Disk gate: at least 1.2 GB free (PROPOSED) before each wave and before each devnet, PostgreSQL or mutation run; below it,
  stop and report. Record the Go build cache size before and after each wave. No repository copy or fresh GOCACHE without
  the orchestrator's instruction.
- Never edit `docs/goals/**`, `docs/decision-register.md`, `docs/threat-model.md`, `docs/protocol*`, `proto/**`,
  `docs/evidence/goal-0*/**` (except the two `check_requirements.py` edits named in section 12), `requirements.lock`,
  `chain/go.mod`, `chain/go.sum`, or any chain production file (stop and report if a change seems needed).
- After Wave 0 step 0, `services/broadcaster/go.mod` and `go.sum` are frozen; import changes go through the orchestrator to B1.
- Delete temp clusters and binaries; mocks only in unit tests; comments describe what the code does; no dead code; every
  input bounded; every error handled; parameterized SQL only; names avoid the secret-scan patterns.

### Preconditions (orchestrator)

- Send Q1-Q4 (`scratchpad/goal-11/owner-questions.md`) with the notes of 17.1.
- Check disk (1.8 GB free on 2026-11-08) and the disk gate before Wave 0.

### Wave 0 step 0 (serial; no answer needed)

- INT: `go.work` line; `SOURCE_ALLOW` entry and comment in `check_requirements.py`.
- B1: `services/broadcaster/go.mod` (pins of 4.5), `go.sum`, `doc.go`.
- Gate: `go -C chain list -m all` identical before and after; `go version -m` of svnd unchanged; `check_requirements.py`
  passes; `go build ./services/broadcaster/...` passes. Then freeze `go.mod`/`go.sum`.

### Wave 0 (parallel; no answer needed)

- B1 store and PostgreSQL: owns `internal/store/**`, `internal/pgdev/**`, `internal/pgtest/**`. Schema 6.2 (answer-dependent
  tables behind a later migration), roles, triggers, migrations runner, every query, compare-and-set helpers, the enqueue
  function, the 13.2 tests that need no worker. Uses `/usr/lib/postgresql/18/bin`. Gate includes `check_requirements.py`.
- B2 chain client: owns `internal/chain/**`. The 1.3 generalisation, `timeout_height`, hash-checked `/tx`, `TxSearch`,
  `Commit`, `Simulate`, height-bound `Account`, queries, the 8.4 classifier and acc_seq reader, the per-kind effect comparison
  with the signer rule; 13.1 chain tests and `FuzzClassifyRPCError`.
- B3 state machine: owns `internal/outbox/**`. 9.1-9.3, 8.5, 8.6 as pure functions over typed observations; 13.1 outbox
  tests, `TestStateMachineProperty`, `FuzzTransitions` with reachability.
- B4 chain-level checks: owns `chain/app/tx_deadline_test.go`. 13.3. Test-only.
- B5 documentation draft: owns `docs/architecture/broadcaster.md`, `services/broadcaster/README.md`,
  `docs/architecture/README.md` (index line). The boundary document (PROPOSED, every open question with its options), the
  API, the schema, the states, the residual risks of section 16, the same-uid limit.

### Wave 1 (only after the Q1-Q4 answers)

- B6 signing and policy: owns `internal/keys/**`, `internal/signer/**`, `internal/policy/**`, `internal/logx/**`. 7.1-7.7
  under the answers (allowlist, intent-key registry, timeout bound, uid checks, provider pre-check, forward checks); 13.1
  policy, signer and redaction tests; the `svn_roottest` uid tests; `TestNoSigningOutsideSigner`, `TestNoLocalPreimageCopy`,
  `TestConfigHasNoIntentKey`; `FuzzForwardedTx`. Gate includes `scripts/scan_secrets.py`.
- B7 API, config and binary: owns `internal/api/**`, `internal/config/**`, `cmd/svn-broadcaster/**`. Section 5, section 11,
  `dev-ca`, `dev-db`, `migrate`, `status`, `reconcile-once`, `signer-clear`; 13.1 API and config tests;
  `FuzzDecodeIntentRequest`.
- B8 worker: owns `internal/worker/**`, `internal/faultpoint/**`. Sections 8 and 10 over B1-B3 and B6; the 13.2 worker tests
  with a fake RPC (unit-labelled); `TestDefaultBuildHasNoFaultPoints`.
- INT: owns the section 12 files not owned above: test runner, Makefile, scaffold removal and replacement record, the two
  new Python guards, CI jobs and shards, `test_ci_devnet_coverage.py` constants, dependencies, SBOM.

### Wave 2

- B9 devnet: owns `chain/devnet/broadcaster_test.go`. 13.4 including the fault proxy, the uid launch, `checkAccounting` and
  phase selection; measures each phase and each test; reports if a shard nears its CI timeout or `chain/devnet` nears the
  30-minute `run_tests.py` limit.
- B10 mutation runner: owns `docs/evidence/goal-11/mutants.py` and its `PY_ALLOW` line in
  `docs/evidence/goal-01/check_requirements.py`. 13.7 with `--check` and `--worktree`; full runs only under the run
  constraints of 13.7.
- Gate: section 15 commands on the uncommitted tree (one devnet run at a time); three review lenses (security and signing
  boundary, evidence honesty, tests); fixes by the owning package; rerun.

### Wave 3 (evidence; orchestrator)

Commit the tested code (message without the model id, `scratchpad/notes-08-10.md:126-129`); section 15 captures on the
committed tree; `docs/evidence/goal-11.md` in the shape of `docs/evidence/goal-06.md`; progress rows (expected BLOCKED);
evidence commit; ask the owner about a draft PR and, in the same message, for approval of
`docs/architecture/broadcaster.md` at the committed sha256 (DR-11-01).

---------------------------------------------------------------------------------------------------

## 19. Notes for the orchestrator

- Disk is tight. Each devnet test run writes four homes and a PostgreSQL cluster under `/tmp`; the helpers clean up and
  PostgreSQL dies with its parent (`Pdeathsig`), but a killed run can leave `/tmp/svn-pg-*` and devnet temp dirs: check and
  remove them after aborted runs.
- `postgres` cannot traverse `/tmp/claude-0`; never point `pgdev` at the scratchpad or the worktree. `TMPDIR` is empty here;
  if a harness sets it under `/tmp/claude-0`, `pgdev` refuses with a clear error.
- The uid boundary needs root. Captures run as root inside `$NETNS`; CI runs it as one uid, and the evidence says which run
  verified what.
- Run long captures as harness-tracked background tasks of at most two hours (`scratchpad/notes-08-10.md:209`).
- DR-10-41 caused flakes before; any devnet phase that restarts a node waits for height to advance before it asserts, and any
  test that needs a nonzero account number picks the signer that has one (`scratchpad/notes-08-10.md:213`).
- New CI check names (`go-race (broadcaster)`, `go-race (devnet-broadcaster)`, `go-race (devnet-broadcaster-stall)`) may need
  a branch-protection update by the owner. The PGDG install step is a network fetch in CI; it is documented in
  `docs/dependencies.md`.
- Report, do not edit: the 1.7 discrepancies; `docs/threat-model.md` gaps (DR-11-16); the progress row 09 deferral wording
  (DR-11-06).

---------------------------------------------------------------------------------------------------

## 20. Steps that still need a human approval or an independent review (handoff list)

1. Answers to Q1-Q4, and approval of `docs/architecture/broadcaster.md` at a pinned commit and sha256 (DR-11-01).
2. The fate of goal 09's outbox deferral, through the Q2 answer (DR-11-06).
3. Acceptance of the PROPOSED number labels (DR-11-10), of the language and layout change, and of the goal 01 `SOURCE_ALLOW`
   edit it causes (DR-11-20).
4. Production key custody for service and broadcaster keys (DEC-001, goal 26); any non-devnet chain id.
5. A retention period for outbox rows and transaction bytes (DEC-004), and the privacy assessment (DEC-007).
6. Threat-model additions (DR-11-16) in the goal 01 document.
7. Branch-protection updates for new CI checks; the goal 11 draft PR and its merge.
8. Independent security review of the signer, the policy pre-checks, the reconciliation rules and the SQL privilege model
   (REQ-SEC-011, goal 29). Model-assisted review is not that review.

---------------------------------------------------------------------------------------------------

## 21. Changelog of revision 2

Every finding was verified against `$WT` at `6d74aca` or the module cache before the outcome below. "Applied" means the
plan now does what the finding asks; "modified" means applied with the stated change; no finding was rejected outright.

### 21.1 Spec and security critique

| Finding | Outcome | Where | Verification and reason |
| --- | --- | --- | --- |
| M1 (d2) vacuous after a crash | applied | 6.2 `attempts`, 8.3 step 2, 8.4, 8.5 (d2) and integrity rule, 13.1, 13.2, WP2, 13.7 | auditor default `AckUnknown` before the send (`submit.go:609`); committed bytes stay in the CometBFT cache and a restart clears it (`clist_mempool.go:257-271`) |
| M2 cancel/expiry race persist | applied | 8.3 step 1 and the T2-T4 paragraph, 6.2 trigger, 9.3, 13.2, 13.7 | revision 1's persist locked only the signer row |
| M3 signer bounds bytes, not authority | modified | 7.2, 7.4, 6.1, 11, 13.1, UA | withdraw checks only host and any `DEVNET_TEST_ONLY` intent (`content/msg_server.go:176-219`; `README.md:63`). Change: the registry is an operator-owned public-key file read by both `serve` and `signer` (public keys are not secret); the protected property is that neither can write it. The signer reads `/status` itself for K_max. `callers` is SELECT-only for the runtime role; the intent-key table is gone |
| M4 same uid gives no boundary | applied (first fix) | 4.1, 7.2, 14 Req. 5, 15 `uid-boundary.txt`, DR-11-35 | `id -u` is 0; `netns-run.sh:6` runs `unshare` as root; distinct uids when root, labelled same-uid fallback otherwise |
| M5 `content_owners` before commit | applied | 5.2, 5.7, 6.2, 13.2, FC, DR-11-34 | no scheduler-host binding in the offer checks (`leases/msg_server.go:203-224`); `service-2` exists (`init.go:138`) |
| M6 T12 maps codes to FAILED without the effect check | applied | 8.6 step 3, 9.3 T7/T13/T14, 13.1, FR1b | `NOT_FOUND` for a closed offer (`:441-443`); begin-block expiry and council force-withdraw emit events without a transaction of our kind (`leases/README.md:323-337`; `content/msg_server.go:250-254,300-330`) |
| M7 effect comparison ignores the signer | applied | 8.6 step 3 signer rule, 9.4, 13.1 `TestEffectComparison`, OC | D-11 (`leases/README.md:519`; `msg_server.go:280-294`) |
| M8 forced checks with the broadcaster key break the signer | applied | PI (provider-1 outer signer), MR (UA forced check merged), `checkAccounting` (a) | handler failure keeps ante writes (`baseapp.go:916-919,720-731`); one BROADCASTER account (`init.go:143`); inner signature checked before the account (`msg_server.go:446-452`) |
| m1 ante versus handler by code list | applied | 1.1, 8.4, 13.3 | `baseapp.go:916`; `sigverify.go:119-141`; FORBIDDEN from `ante.go:164` or a handler; sdk 11 at `baseapp.go:804` |
| m2 account reads not height-bound | applied | 8.1, 8.5 (d3), 13.1 | `rpc.go:305-337` ignores the response height |
| m3 PENDING_STALL must be recorded | applied | 6.2 `stall_episodes`, 9.1, 10 | `docs/protocol.md:260`; `decision-register.md:337` |
| m4 dead watcher looks like a stall | applied | 5.8, 6.2 `chain_health`, 9.1, 10, stall test step 7 | |
| m5 rebroadcasts after expiry | applied | 5.8 `last_landing_height`, 8.3 step 2, 8.6 step 2, 8.7, EX | |
| m6 deterministic signatures collide | applied | 1.1, 8.2, 13.1 | `secp256k1_nocgo.go:13-21` (RFC 6979 via dcrd `SignCompact`) |
| m7 lib/pq env, debug output, PG error logging | applied | 1.5, 6.3, 6.5, 7.7, 11, 13.1, 13.2 | `PQ/connector.go:205-207,242-431,511-512`; `PQ/conn.go:68-73,253-264` |
| m8 global lock in the caller's transaction | modified | 5.7, 11 | no global lock at all: the global bound is the configured sum of per-caller caps, refused at start above a hard limit; per-caller rows under local timeouts |
| m9 canonical `request_digest` | applied | 5.4, 13.1, 13.2, DU | SQL path uses `jsonb` text, whose key order is deterministic; the worker also computes the typed digest |
| m10 subcommands bypass fencing | applied | 8.1, 13.2 | |
| m11 unbounded `transitions` | applied | 6.2, 8.6, 11 | |
| m12 text inconsistencies | applied | 9.2 (phase order), 16 (locks, not SKIP LOCKED), 1.1 and 8.2 (`/app/simulate`, built by `serve`, one trusted node, capped) | `abci.go:207-209,1136-1145`; `sigverify.go:387` |

### 21.2 Tests and evidence critique

| Finding | Outcome | Where | Verification and reason |
| --- | --- | --- | --- |
| F1 stall test conflicts with the helpers | applied | 13.4 stall test steps 1-7 | `status.go:115-131`; `prove_test.go:115-141,263-293,302-342`; leases begin-block (`module.go:118-126`) |
| F2 WP does not reach its window | applied | 8.3 fault points, WP1-WP3, HD, `TestFakeRPCSeesPersistedAttempt`, `TestDefaultBuildHasNoFaultPoints` | build-tag precedent `ci.yml:162-171` |
| F3 counts contradict rebroadcast; vacuous phase A | applied | 8.6 step 2 (ACKNOWLEDGED attempts are rebroadcast), TB1/TA/WD byte identity, phase A holds | `SkipTimeoutCommit = true` (`start.go:245`) |
| F4 faults not proven to happen; shared state | applied | 13.4 phase rules, WD holds, BR seed, SELECT-only role | `rpc.go:109,115` |
| F5 Z checks chain guarantees | applied | `checkAccounting` (a)-(e) after every phase; (e) labelled | `lease_test.go:520-523` |
| F6 idempotent replay and REPLAY never reach the chain | applied | FR1, FR2 (and FR1b from M6) | `leases/README.md:521`; `evidence/README.md:703-712` |
| F7 mempool rejection and halt only with fakes | applied | MR; 14 Req. 3 label for backoff and caps | |
| F8 SQL path never reaches the chain | applied | H, UA | |
| F9 `test_ci_devnet_coverage.py` will fail | applied | 12 (constants only; no PostgreSQL skip variable; new guard `test_ci_broadcaster.py`) | `:89,519,531-543,895` |
| F10 mutation runner not feasible as designed | applied | 13.7 killer table, run constraints, 18 disk gate | `mutants.py:7-8,64-68,1993,2180,2219`; 1.8 GB free |
| F11 no time budget | applied | 12 (two devnet shards, prebuilds), 11 (lower bounds), 13.4 timing, 18 B9 | `tests-go.txt:11`; `race.txt:11`; `run_tests.py:151-158`; `ci.yml:146-171` |
| F12 capture gaps | applied | 15 (`tests-go.txt` as goal 10, `traces.txt` by command, `canary.txt`, pgdev non-root BLOCKED) | `tests-go.txt:1`; `run_tests.py:163-166` |
| F13 RS timing-dependent | applied | RS | `goal-10.md:124,183` |
| F14 CS second process not observable | applied | 6.2 `worker_instance`, CS, per-process proxy | |
| F15 retired key needs a rotation in S | applied | S, PI | `registry_test.go:506-512` |
| F16 binary's mTLS untested | applied | 5.6, UA, `TestServerConstructorSharedWithMain` | |
| F17 log canary vacuous | applied | 7.7, L, `TestRedactorDropsRequestBody` | |
| F18 orphaned servers | applied | 13.2 (`postgres` direct child, `Pdeathsig`, process group), `TestClientDeathMidTransaction` re-exec | no `Pdeathsig` in the repository |
| F19 PGDG pin may stop installing | applied, `[Educated Guess]` kept | 12 CI pinning | `docs/dependencies.md:39`; `scripts/env/install.sh:9,14` |
| F20 broadcaster built without `-race` | applied | 4.3, 12 | `audit_test.go:220-236` |
| F21 signer death untested | applied | SG | |

### 21.3 Scope and honesty critique

| Finding | Outcome | Where | Verification and reason |
| --- | --- | --- | --- |
| M1 contract checker rejects Go under `services/` | applied | 1.5, 2.3 item 7, 3, 4.5, 12, 15 `goal01-diff.txt`, 17.3 DR-11-20, 18 | `check_requirements.py:184-194,636-637` |
| M2 Wave 0 packages cannot build in parallel | applied | 4.5, 18 Wave 0 step 0 | `go.work:5-11`; `chain/go.mod:129`; 10 GB build cache |
| M3 same-uid boundary not shown | applied | as spec M4 | `netns-run.sh:6`; brief section 4 says root |
| M4 Q1 hides the publication consequence | applied | 7.4 limits, 14 REQ-CHAIN-013 (one BLOCKED status with sub-results), owner Q1 | `content/README.md:39,61`; `intent.go:126-152`; `blueprint.md:242` |
| M5 person-linked data with no decision | applied | 5.5, owner Q4 (column list, minimization option `[Educated Guess]`) | `protocol.md:272`; `decision-register.md:127,217` |
| M6 inference filed as a ruling | applied | header, 2.2, 16, 17.2 DR-11-06, owner Q2 | `goal-09.md:39,74`; `progress.md:39` |
| m1 REQ-INT-001 over scaffolds | applied | 14 (DEFERRED) | |
| m2 allowlist misses existing writers | applied | 13.6 exact paths | `testhome.go:265`; `chaintest/{chaintest,serve}.go` |
| m3 contradictory interim rule; Q3 overloaded | applied | 17.1 (Wave 1 waits); caller authentication moved to DR-11-32 | |
| m4 wrong provenance; incomplete numbers table | applied | 8.7, 11 table | `submit.go:78-82` |
| m5 lib/pq guess resolvable | applied | 1.5, 7.7 | `PQ/connector.go:205-207,511-555,667-678` |
| m6 local-copy fallback unnecessary | applied | 4.3, 13.6 `TestNoLocalPreimageCopy` | `intent.go:83,98`; `innersig.go:115` |
| m7 capture list: netns, model, traces | applied | 15 | |
| m8 test machinery in the binary | applied | 4.2, 4.3, DR-11-24 | `docs/protocol.md:136` |
| m9 reference divergence only reported | applied | 10, DR-11-19, stall test step 5 | `cases.py:64-77`; `protocol.md:129` |
| m10 wrong owner for mTLS | applied | 5.6, 16 | no goal file mentions mTLS; goal 27 spec :19 names TLS |
| m11 wrong brief discrepancy | applied | 1.5, 1.7 | brief section 4 records 18.6 |
| m12 blueprint projection sentence | applied | 1.4, 2.2, DR-11-33 | `blueprint.md:305,387`; goal 12 spec :19-21 |
| m13 no disk threshold | applied | 18 rules | |
| m14 Makefile missing | applied | 12 | `Makefile:67-73` |
| m15 secret scanner patterns | applied | 7.7, 18 B6 gate | `scan_secrets.py:25-62` |
