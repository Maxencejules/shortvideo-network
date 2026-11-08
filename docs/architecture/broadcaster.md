# Transaction broadcaster and outbox

Status: PROPOSED. This is the goal 11 design for `services/broadcaster`, its PostgreSQL outbox and its signing boundary.
It is not approved. It is not an audit. It applies to the single-machine devnet only: one operator, one machine, four
validator processes. Public launch is disabled. There is no token, fee, reward, staking module or penalty.

Approval. The goal 11 prerequisite "approved service/provider signing boundaries"
(`docs/goals/11-transaction-broadcaster-and-outbox.md:8`) has no approved document behind it. Gate A accepted "the proposed
signing domains" (`docs/decision-register.md:23`), not a broadcaster boundary. The prerequisite is recorded BLOCKED as
DR-11-01. The owner will be asked to approve or reject this file at a pinned commit and file sha256 when a goal 11 pull
request is ready, as was done for the goal 10 specification (DR-10-00, `docs/evidence/goal-10.md:89`). The owner questions
Q1 to Q4 (section 17) are unanswered. No sentence in this file is an owner decision unless it quotes one (section 2).

How to read this file.

- Line citations (`path:line`) were read on the goal 11 working tree, whose base is main `6d74aca`.
- Framework citations name module-cache copies of the pinned versions: `cosmos-sdk@v0.55.0/...` is
  `$(go env GOMODCACHE)/github.com/cosmos/cosmos-sdk@v0.55.0/...`; `cometbft@v0.40.0/...` and `lib/pq@v1.12.3/...` likewise.
- "Goal spec :N" means `docs/goals/11-transaction-broadcaster-and-outbox.md` line N.
- Sections 4 to 16 describe the design under the recommended answers to Q1 to Q4. They are a proposal. If the owner picks
  another option, the sections that depend on it change; section 17 says which.
- This file does not say which parts are built or tested. `docs/evidence/goal-11.md` records that, with logs.
- Every number is either cited or labelled PROPOSED. A PROPOSED number is a proposal with a decision request (DR-11-10), not
  policy and not a service level.
- Labels. "Interim" marks a rule that answers an open decision request; the goal 11 plan implements and tests it and
  reports the request open. "Fail-closed" on an interim means it refuses the uncertain action instead of allowing it.
  "Derived from source" means read in the pinned source but not yet exercised by a test in this repository.
  `[Educated Guess]` marks an inference.

## Contents

1. Scope and what this file does not claim
2. Owner rulings that bind goal 11
3. Chain facts the design depends on
4. Processes and identities
5. Signing boundary
6. Externally signed submissions
7. Hosting intents and the publication permission
8. Chain id binding and stored authorizations
9. Redaction and secret hygiene
10. Service API
11. Outbox schema
12. Submission engine
13. States and transitions
14. Canonical commitment evidence for the indexer
15. Chain outage behaviour
16. Configuration and PROPOSED numbers
17. Owner questions Q1 to Q4 (unanswered)
18. Security-relevant decision requests with interim rules (open)
19. Reversible implementation choices
20. Protocol additions recorded, not made
21. Residual risks
22. Steps that need a human approval or an independent review

## 1. Scope and what this file does not claim

Goal 11 asks for a broadcaster with typed requests and a PostgreSQL transactional outbox; approved service keys used only
for their permitted messages; per-signer sequences, bounded retries, deadlines and reconciliation; caller-visible states,
authentication and object permissions; a development signing boundary, redacted logs and explicit chain id and gateway
configuration (goal spec :18-32). The handoff is failure traces, the outbox schema, the service API, this boundary
document, and stable identities with canonical commitment evidence for the indexer (goal spec :43-44).

This file does not claim:

- an approved boundary, an audit, production readiness, production key custody, or operator independence;
- a uid boundary that did not run (section 4.2);
- any production path. Content registration off the devnet is `DECISION_BLOCKED` (`chain/x/content/README.md:60`). Offers
  and renewals need the DEC-005 devnet test number (`chain/x/leases/README.md:124-125`);
- application projections. `[Educated Guess]` The blueprint's "then updates the application projection"
  (`docs/blueprint.md:305`) is read as: the broadcaster updates intent status and commitment evidence only. Projections come
  from the goal 12 indexer (`docs/blueprint.md:387`). DR-11-33 records the reading;
- read proofs. Reads trust one configured node (`docs/blueprint.md:391`; DR-11-08).

Out of scope for goal 11: the API service (goal 13), creator UI states (goal 21), the provider agent (goal 16), the indexer
(goal 12), key rotation and backup drills (goal 26), production packaging and certificate issuance (goal 27), load numbers
(goal 28).

## 2. Owner rulings that bind goal 11

No owner ruling about goal 11 itself exists. These earlier rulings constrain it. Each is quoted from the record.

- DR-06-06: "seat-only admission, owner ruling 2026-11-02" (`docs/progress.md:38`). A council seat key signs only
  `MsgProposeChange` and `MsgApproveChange` (`chain/app/ante.go:161-166`). The broadcaster never holds a seat key.
- Goal 09 Q1, "Account key signs (Recommended)": "The submitting account must be an `AUDITOR`. ... No `BROADCASTER` relay,
  no rotation. Devnet beta only." (`docs/evidence/goal-09.md:74`). No broadcaster key signs an audit batch.
- DR-10-00, "Approve as pinned", for `docs/architecture/governance-and-upgrades.md` at `18fa310`, for goal 10 only
  (`docs/evidence/goal-10.md:89`). It is a precedent for how this file could be approved. It approves nothing for goal 11.
- Model substitution: "I acknowledge the model substitution for DR-06-07", then "extend the acknowledgement to goals 07
  through 30" (`docs/progress.md:37`). It is about model allocation only.

Not an owner ruling. `[Educated Guess]` Goal 09 Q1 forbids a broadcaster-signed audit batch. It does not decide whether an
AUDITOR-signed batch may pass through the outbox unchanged. Goal 09 recorded "Outbox submission: Goal 11" as deferred
(`docs/evidence/goal-09.md:39`). That item stays DEFERRED until the owner answers Q2 (DR-11-06).

## 3. Chain facts the design depends on

### 3.1 Transaction path

- One TxConfig with `SIGN_MODE_DIRECT` only and a strict decoder that rejects unknown body fields
  (`chain/app/encoding.go:44-81,110-138`). Each registered message has one 20-byte `signer` field, its only signer
  (`chain/app/encoding.go:144-173`).
- The ante chain runs SetUpContext, ExtensionOptions, ValidateBasic, TxTimeoutHeight, the envelope, memo and bounds
  decorators, tx-size gas, fee reject, SetPubKey, signature count, signature gas, signature verification, admission and
  sequence increment, in that order (`chain/app/ante.go:28-44`). Gas must be from 1 to 20,000,000 (0 only in simulation)
  and a transaction at most 65,536 bytes (`chain/app/ante.go:90-108`; `chain/genesis/bounds.go:29-31`). A fee amount is
  `FEE_REJECTED` (`chain/app/ante.go:112-121`). No memo, fee payer, granter or tip can carry an intent id.
- `NewTxTimeoutHeightDecorator` is installed (`chain/app/ante.go:32`). It rejects a transaction when `timeout_height > 0`
  and the block height exceeds it, or when a `timeout_timestamp` is set and is before the block time
  (`cosmos-sdk@v0.55.0/x/auth/ante/basic.go:205-227`). No repository code sets either field. The goal 11 plan tests the
  semantics through FinalizeBlock before relying on them.
- Unordered transactions are refused. The account keeper is built without the unordered option
  (`chain/app/app.go:199-207`), and the signature decorator rejects an unordered transaction when the option is off
  (`cosmos-sdk@v0.55.0/x/auth/ante/sigverify.go:319-325`). Sequences per signer are strict.
- CheckTx runs only the ante chain. A CheckTx code 0 says nothing about roles, the business gate or capacity: on the devnet
  an acceptance passes CheckTx and commits with svn `CAPACITY` (`chain/devnet/lease_test.go:507-530`).
- Writes and events of a failed transaction. On an ante failure the result keeps no writes and carries no events
  (`cosmos-sdk@v0.55.0/baseapp/baseapp.go:916`). On a handler failure after a passing ante, the ante writes are kept, so the
  sequence is consumed, and the result carries the ante events
  (`cosmos-sdk@v0.55.0/baseapp/baseapp.go:916-920,720-731`). A transaction that finds no block gas left returns before the
  ante with sdk code 11 (`cosmos-sdk@v0.55.0/baseapp/baseapp.go:803-805`). The same code can come from either side,
  so no code tells whether the sequence was consumed.
- `tx.acc_seq`. SetPubKey emits `tx.acc_seq=<address>/<sequence>` (`cosmos-sdk@v0.55.0/x/auth/ante/sigverify.go:119-141`).
  Ante events survive only when the whole ante passed. So a committed result carries `tx.acc_seq=<S>/<N>` if and only if
  the ante passed and sequence N of S was consumed (derived from source).
- After a node starts, CheckTx rejects correctly signed transactions from normal accounts with sdk code 4 until the node
  commits a block (DR-10-41, `docs/architecture/governance-and-upgrades.md:677-680`).
- The node builds its CometBFT configuration from `cmtcfg.DefaultConfig()` and `node.json`, not from `config.toml`
  (`chain/node/start.go:211-234`). The mempool is therefore the v0.40.0 default
  (`cometbft@v0.40.0/config/config.go:1021-1033`). Admitted bytes wait until a block includes them, recheck evicts them, or
  every node holding them restarts.
- A resend of bytes that are in the mempool cache gets the JSON-RPC error "tx already exists in cache" before CheckTx
  (`cometbft@v0.40.0/mempool/clist_mempool.go:257-271`; `cometbft@v0.40.0/mempool/errors.go:11-12`). After a node restart
  the cache is empty, so the bytes of a committed transaction get CheckTx sdk code 32 instead.
- Devnet blocks are produced continuously: `CreateEmptyBlocks` is true, the interval is 0, `TimeoutCommit` is 200 ms and
  `SkipTimeoutCommit` is true (`chain/node/start.go:237-248`). A `timeout_height` passes on an idle chain, and a block can
  follow an acknowledgement faster than one poll.
- Reads. `/tx_search` takes queries up to 512 bytes and at most 100 results per page
  (`cometbft@v0.40.0/rpc/core/events.go:16-18`; `cometbft@v0.40.0/rpc/core/env.go:23-24`). Typed events copy string
  attributes as they are and index every attribute (`chain/msgkit/events.go:60-75`). No event carries a business id.
- `/check_tx` calls the app's CheckTx without adding to the mempool (`cometbft@v0.40.0/rpc/core/mempool.go:186-194`).
  Derived from source, it still advances the check-state sequence, so the broadcaster never uses it.
- Simulation. `abci_query` with path `/app/simulate` runs `app.Simulate` (`cosmos-sdk@v0.55.0/baseapp/abci.go:207-209`),
  then JSON-encodes the answer with the app's interface registry, which does not register the svn `Msg...Response` types.
  So a successful svn message simulation answers a non-zero code ("unable to resolve type URL") on that route
  (`chain/app/tx_deadline_test.go:722`, `TestAppSimulatePathCannotEncodeSvnResults`;
  `services/broadcaster/internal/chain/rpc_test.go`, `TestAppSimulateJSONCannotCarrySvnResponses`). The same simulation
  answers code 0 through `abci_query` on the gRPC-routed `/cosmos.tx.v1beta1.Service/Simulate` with a binary
  `SimulateRequest{tx_bytes}` (`chain/app/tx_deadline_test.go:654`, `TestSimulateWithEmptySignature`); svnd registers that
  service after the node starts (`chain/node/start.go:120`), and the moment right after start, before it is registered, is
  untested. The chain client uses that route (`services/broadcaster/internal/chain/rpc.go:88`). The plan named
  `/app/simulate` (DR-11-27); the route is pending the orchestrator's ruling. In simulate mode signature verification is
  skipped (`cosmos-sdk@v0.55.0/x/auth/ante/sigverify.go:387`) and a missing public key is accepted
  (`cosmos-sdk@v0.55.0/x/auth/ante/sigverify.go:209-213`), so a simulation transaction with an empty signature is accepted
  (`TestSimulateWithEmptySignature`).
- Signing is deterministic. SDK secp256k1 signing uses `ecdsa.SignCompact`, which is RFC 6979 deterministic
  (`cosmos-sdk@v0.55.0/crypto/keys/secp256k1/secp256k1_nocgo.go:13-21`). The same body at the same sequence gives the same
  bytes and hash.

### 3.2 Who may sign what, as the code enforces it

| Message | Outer signer the chain accepts | Source |
| --- | --- | --- |
| `RegisterContent`, `WithdrawContent` | `ADMITTED_SERVICE` only; the signer becomes `submitting_host`; a `BROADCASTER` is `FORBIDDEN` | `chain/x/content/msg_server.go:78,186`; `chain/x/content/README.md:35-39` |
| `WithdrawContent` host check | the signer must equal the content's `submitting_host` | `chain/x/content/msg_server.go:215` |
| `OfferLease` | `ADMITTED_SERVICE`; the signer becomes the scheduler | `chain/x/leases/msg_server.go:239` |
| `AcceptLease`, `RenewLease`, provider `EndLease` | `BROADCASTER` (relay) or the provider's own `ADMITTED_PROVIDER` account, with the provider's inner signature | `chain/x/leases/msg_server.go:405,628-630` |
| `EndLease{authority_end}` | one `AUTHORITY` signature | `chain/x/leases/msg_server.go:680-681`; `docs/protocol.md:239` |
| `SubmitAuditBatch` | `AUDITOR` only; a `BROADCASTER` relay is `FORBIDDEN` | `chain/x/evidence/README.md:277` |

- Submitter roles are written at genesis and by the engine-only service rotation, which no message reaches
  (`chain/x/authority/README.md:22,38`). The devnet has `authority`, `service`, `service-2`, `provider-1`, `provider-2`, `broadcaster`
  and two auditor accounts (`chain/devnet/init.go:131-144`). A second `ADMITTED_SERVICE` account, `service-2`, exists.
- The relay column of `docs/protocol.md:233-234,240` lets a broadcaster submit content messages and audit batches. The code
  refuses both. These are recorded deviations (`chain/x/content/README.md:39`; `chain/x/evidence/README.md:277`).

### 3.3 Business ids and retries

- Business ids form one unsigned namespace across modules. The same id and payload is code 0 with an idempotent result
  event and no typed event; another payload is `REPLAY` (`chain/x/leases/README.md:105-110`;
  `chain/msgkit/events.go:324-329`).
- Every check before the business gate runs again on a retry. A retry of a committed action can return `FORBIDDEN`,
  `UNAUTHORIZED`, `BOUNDS` or a role failure. The outbox "reads the committed result by transaction hash, or the lease
  state, before it resubmits, and never treats a resubmission's code as the first transaction's result"
  (`chain/x/leases/README.md:607-618`). A committed provider key rotation is reconciled by query, not by resubmission
  (`chain/x/providers/README.md:126-128`).
- Any admitted account can take a pending business id first. The auditor draws retry ids from `crypto/rand`
  (`chain/x/evidence/README.md:704-712`).
- An `OfferLease` replay by another scheduler is `FORBIDDEN` (D-11), and another service can commit a copy of a pending
  offer first; an identical offer of ours then gets `DUPLICATE_ID` (`chain/x/leases/msg_server.go:280-294`).

### 3.4 Provider consent and hosting intents

- Provider consent is an inner signature verified with `msgkit.VerifyInner` against the provider's current registered
  key (`chain/x/leases/README.md:157-162`). There is no chain id in any provider payload (`chain/x/leases/README.md:148`).
  RENEW and END payloads carry no expiry (`chain/x/leases/README.md:154-155`). The provider never signs the business id
  (`chain/x/leases/README.md:138`). An accept relay whose offer has closed gets `NOT_FOUND`
  (`chain/x/leases/msg_server.go:441-443`).
- Exported preimage builders exist for provider payloads (`chain/x/leases/signdoc.go:92,104,117`) and hosting intents
  (`chain/x/content/intent.go:83,98`). The inner verifier is `msgkit.VerifyInner` (`chain/msgkit/innersig.go:115`).
- A devnet hosting intent is signed by the key in the message. "It does not prove a creator authorized the host"
  (`chain/x/content/README.md:61`). "The withdraw intent is not bound to the key that signed the register intent. The
  payload has no chain id." (`chain/x/content/README.md:63`). A withdraw is an irreversible tombstone
  (`docs/protocol.md:234`).

## 4. Processes and identities

### 4.1 Layout (PROPOSED; under Q1 option (a))

```
caller (test harness in goal 11; goal 13 API later)
   | mTLS on loopback (or the granted SQL enqueue function, in the caller's own database transaction)
   v
svn-broadcaster serve   (uid U_serve)   API, validation, outbox writes, per-signer workers, reconciler, observer
   | Unix socket; peer uid must equal U_serve        | lib/pq over a Unix socket, explicit connection config
   v                                                 v
svn-broadcaster signer  (uid U_signer)   PostgreSQL (schema broadcaster), run as `postgres`
   key directory owned by U_signer; allowlist, intent-key check, deadline bound, outer signing only
   +--> /status of the configured node (loopback), for its own deadline bound only

serve --> CometBFT RPC of ONE configured node (loopback): status, abci_query, broadcast_tx_sync, tx, tx_search, commit

intent-key registry file (public keys only): written by the operator; writable by neither U_serve nor U_signer
```

- One binary, several subcommands: `serve`, `signer`, `migrate`, `reconcile-once`, `status`, `signer-clear`, `dev-ca`,
  `dev-db`.
- Python services (goal 13 onward) never load keys. They reach the broadcaster through its HTTP API or the granted SQL
  enqueue function (section 11.3).
- PostgreSQL 18.6 is the goal 04 pin (`docs/dependencies.md:39`). The SQL avoids 18-only features so a 16.x server also
  works. RabbitMQ is not used: the outbox is the queue (`docs/blueprint.md:385`).
- Under Q1 option (b) the signer is a package inside `serve`. Under option (c) only the `BROADCASTER` key exists and the
  service kinds are not built.

### 4.2 The uid rule and the same-uid limit

- Uid rule (DR-11-35, reversible). When the launcher runs as root, it starts `signer` and `serve` under two distinct
  non-root uids with no supplementary groups. The key directory belongs to U_signer and is closed to everyone else. The
  signer accepts a peer only when the socket's peer credentials report the configured U_serve. It refuses to start when
  U_serve equals its own uid while distinct uids are configured. The planned devnet test
  prints `boundary_uids=signer:<u1>,serve:<u2>`.
- Same-uid limit. When the launcher is not root, both processes run as one uid and the run prints
  `boundary_uids=same:<u>`. Then the process split separates key bytes in memory only. A compromised `serve` can read the
  key files or trace the signer. The boundary protects nothing once `serve` is compromised.
- `[Educated Guess]` The CI runner is one non-root user, so a CI run uses the same-uid fallback. The development boundary
  counts as VERIFIED only from a logged root run with distinct uids. A same-uid run counts as IMPLEMENTED.
- The workspace where goal 11 is built runs as uid 0 (`id -u`), and root bypasses file permission checks. A root process on the host can
  read every key file. The uid rule separates `serve` from `signer`; it does not protect against the host's root.

## 5. Signing boundary (PROPOSED; under Q1 option (a))

### 5.1 Key classes and allowlists

| Key | Role read from `Query/Governance` at start-up | May sign (type URL allowlist; checked in policy and again in the signer) |
| --- | --- | --- |
| `service` (devnet) | `ADMITTED_SERVICE` | `/svn.content.v1.MsgRegisterContent`, `/svn.content.v1.MsgWithdrawContent`, `/svn.leases.v1.MsgOfferLease` |
| `broadcaster` (devnet) | `BROADCASTER` | `/svn.leases.v1.MsgAcceptLease`, `/svn.leases.v1.MsgRenewLease`, `/svn.leases.v1.MsgEndLease` with `authority_end = false` |

- `Query/Governance` returns the submitters with their roles and the seats (`chain/x/authority/README.md:160-166`;
  `docs/protocol/proto/svn/authority/v1/state.proto:70-78`).
- Start-up refuses: a key whose address has another role, no role, or is a council seat; two keys with one address; a key
  file that is a symlink, not regular, readable by group or others, not owned by the signer uid, or not the devnet hex
  format (the auditor loader rules, `agents/auditor/keys/keys.go:12-18`, plus an owner check); a chain id outside `shortvideo-dev-` while
  development keys are configured.
- One message per transaction (DR-11-14). A multi-message transaction is atomic, so batching unrelated intents would couple
  their failures.
- The signer of an intent is fixed when the intent is created and never changes. An offer replay by another scheduler is
  `FORBIDDEN` (D-11), and content is owned by its `submitting_host` (`chain/x/leases/README.md:107-108`;
  `chain/x/content/msg_server.go:215`).

### 5.2 Signer checks

- Only `svn-broadcaster signer` reads key files. It listens on a Unix socket in a directory owned by U_signer.
- Request: key name, chain id, account number, sequence, gas, timeout height, and one message.
- Checks, in this order: decode the message with the app interface registry; type URL in the key's allowlist; the
  message's `signer` equals the key's address; `authority_end = false` for `MsgEndLease`; chain id equals the signer's own
  configuration; gas within bounds; `timeout_height` not 0 and not above the signer's own observed height plus K_max
  (PROPOSED, section 16). The signer reads `/status` of the configured node itself and refuses while it has no fresh height.
- For `MsgRegisterContent` and `MsgWithdrawContent` it also checks that the hosting intent's public key is in the
  operator's intent-key registry (section 7), and that the intent signature verifies over the payload rebuilt with
  `content.RegisterIntentSignBytes` or `content.WithdrawIntentSignBytes` (`chain/x/content/intent.go:83,98`), with its own
  address as submitting host and role `DEVNET_TEST_ONLY`.
- It then builds and signs the outer transaction with `SIGN_MODE_DIRECT`, re-decodes it with the strict decoder, and
  returns the bytes and the hash. It signs nothing else: no inner-signature domain and no arbitrary bytes. It keeps no
  state, so its death mid-request leaves no attempt and consumes no sequence.
- No broadcaster package outside the signer calls an ed25519 or SDK signing function, and the module has no
  `SVN-PROVIDER-` or `SVN-HOSTING-INTENT-` string literal. Planned static tests check both.

### 5.3 What a compromised `serve` can still do

With distinct uids, a compromised `serve` can: make the service key send any `MsgOfferLease` the chain accepts; relay any
stored or captured provider signature as `BROADCASTER`; replay a stored hosting intent for its own content and version;
delay, reorder or withhold submissions; read the outbox, including authorization contexts and unbroadcast inner signatures.
It cannot register or withdraw content under an intent key it made up, sign another message type, sign for another chain
id, or set an unbounded deadline. With one uid (section 4.2) it can also read the key files.

### 5.4 Never signed, never held

- Never signed by the broadcaster: provider inner signatures, hosting intents, council messages, `MsgExecuteChange`,
  `authority_end`, audit batches, provider registry messages, arbitrary bytes.
- Never held: council seat, `AUTHORITY`, `AUDITOR` or provider account keys, provider signing keys, validator keys, creator
  or user keys, hosting-intent keys.
- Production custody (KMS, HSM, remote signer) is not available here and is not claimed (DEC-001; goal 26).

## 6. Externally signed submissions (PROPOSED; under Q2 option (a))

- P1, relay. Before enqueue and again before signing, the broadcaster reads the provider (`Query/Provider`), checks that it
  is admitted, rebuilds the payload with `leases.AcceptSignBytes` from the committed open offer, or with
  `RenewSignBytes`/`EndSignBytes` from the message and the held lease, and verifies the inner signature with
  `msgkit.VerifyInner` against the provider's current key. A signature by the service key, the broadcaster key, another
  provider's key, a retired key or any unregistered key is refused with `PROVIDER_SIGNATURE_INVALID` and no row. The chain
  verifies again; the pre-check only fails fast (`docs/protocol.md:183`).
- No query returns an offer by id. `Query/Deadlines` returns full offers but pages by deadline, so the pre-check pages it
  within a PROPOSED page budget. An offer not found within the budget fails the pre-check (`OFFER_NOT_FOUND`); the check
  is never skipped.
- P3, verbatim forward. The broadcaster strictly decodes the raw bytes, requires exactly one message of an allowed relay
  type, an outer signer that is an `ADMITTED_PROVIDER` whose provider record names that account, a valid inner signature
  as in P1, the configured chain id (checked by verifying the outer signature with the account's public key and account
  number), and a `timeout_height` within a PROPOSED forward window. Bytes without a deadline are refused, because they
  could commit at any later time. Forwarded bytes are never edited. The outbox tracks them by hash and allocates no
  sequence. When they can no longer commit, the intent fails with `PROVIDER_MUST_RESIGN`.
- P2, the provider submits its own transaction: goal 16. Goal 11 reconciles by object state when a provider's own
  submission commits the effect first.
- Audit batches stay on the auditor's own path (DR-11-06). The broadcaster carries no audit batch and holds no `AUDITOR`
  key.
- Residual: a broadcaster can withhold a signed acceptance until the offer expires. P2 is the provider's remedy.

## 7. Hosting intents and the publication permission (PROPOSED; under Q3 option (a))

- The broadcaster never holds an intent key. A separate devnet fixture signer stands in for the account host and signs
  intents that name the service signer's address as `submittingHost`. It is a test stand-in, not DEC-001 custody.
- Intent-key registry: a file `intent-keys.json` that maps a `principal_id` to one ed25519 public key and a label. It holds
  public keys only. The operator writes it, as a uid that is neither U_serve nor U_signer, in a directory neither of them
  can write. `serve` and `signer` read it at start and refuse to start if the file or its directory is writable by their own
  uid. A change takes a restart. No broadcaster command and no caller writes it.
- For `REGISTER_CONTENT` and `WITHDRAW_CONTENT`, `serve` checks that the authenticated caller may send the kind; that the
  authorization context grants `PUBLISH_CONTENT` (or `WITHDRAW_CONTENT`) to `principal_id`; that the intent's public key
  equals the key the registry lists for that principal; that the intent signature verifies over the payload rebuilt with
  the exported builders and the pinned service address; and, for a withdraw, that the principal is the recorded owner
  (section 10.7). The signer checks the registry and the signature again (section 5.2).
- The configuration schema has no intent-key field.
- Limits:
  - The chain cannot enforce creator permission (`chain/x/content/README.md:61`).
  - Under Q1 (a) the publication half of REQ-CHAIN-013 (`docs/requirements.md:452`) holds for the broadcaster's code path
    only. Whoever holds the `ADMITTED_SERVICE` key bytes can register content with an intent key they make up, because the
    chain checks only the `DEVNET_TEST_ONLY` role (`chain/x/content/intent.go:126-152`).
  - Under Q1 (c) the chain role enforces it for the broadcaster's key: a `BROADCASTER` outer signer is `FORBIDDEN` for
    content messages (`chain/x/content/README.md:39`).
  - The truth of a caller's permission claim belongs to the API (goals 13, 19, 21). Community publication approval
    (`docs/protocol.md:195`) is not this permission under option (a).

## 8. Chain id binding and stored authorizations (PROPOSED; under Q4 option (a))

- Every intent row stores the configured chain id. `serve` refuses to start when `/status` reports another network, and
  refuses to relay an intent created under another chain id.
- Inner signatures carry no chain id, and RENEW and END never expire on chain (section 3.4). Unbroadcast inner signatures
  are sensitive data: never logged, and returned only as a digest. RENEW and END relays carry a local expiry (PROPOSED) and
  are refused after it. ACCEPT relays are refused after the offer's expiry.
- Local expiry revokes nothing on chain. Only a renewal (D-12, `chain/x/leases/README.md:164-168`) or a provider key
  rotation does.
- When an intent ends FAILED with nothing ever sent, its stored inner signatures, hosting intent and full authorization
  context are cleared and their digests kept. This is data minimization, not a retention period. Every other row is kept;
  retention is unset (DEC-004, `docs/decision-register.md:127`).

## 9. Redaction and secret hygiene

- Logs are JSON through `log/slog` with a replace hook that drops attributes named or typed as key material, transaction
  bytes, signature bytes, connection strings, passwords, certificates, authorization contexts or request bodies. Values
  are logged only as hashes or ids. API errors never echo internal errors.
- Database connection. The configuration builds a `pq.Config` field by field and opens it with `pq.NewConnectorConfig`,
  which reads no environment (`lib/pq@v1.12.3/connector.go:205-207`). `pq.NewConfig` merges the process environment
  (`lib/pq@v1.12.3/connector.go:511-512`), and `pq.NewConnector` calls it (`lib/pq@v1.12.3/connector.go:193-199`). The
  environment tags include `PGPASSWORD`, `PGPASSFILE` and `PGSERVICEFILE` (`lib/pq@v1.12.3/connector.go:286,291,431`), and
  `PQGO_DEBUG=1` prints the connection config and protocol traffic to stderr outside any logger
  (`lib/pq@v1.12.3/conn.go:68-73`). So every subcommand refuses to start, before any connection, if any `PG*` variable or
  `PQGO_DEBUG` is set. The password comes only from a passfile owned by the reading uid.
- PostgreSQL server logs (DR-11-37). The development cluster runs with five settings
  (`services/broadcaster/internal/pgdev/pgdev.go:115-119`): `log_min_error_statement=panic`, `log_statement=none` and
  `log_parameter_max_length_on_error=0`, so no statement text or bind parameter is logged (PostgreSQL's default
  `log_min_error_statement` would log the text of a failing statement, including a caller's literal enqueue arguments);
  and `log_error_verbosity=terse` and `log_min_messages=fatal`, because PostgreSQL error messages can quote input values
  themselves (for example `invalid input syntax for type interval: "<value>"`, the DETAIL of a unique or CHECK violation
  that shows the failing row, and jsonb CONTEXT lines). With the first three alone those values reach the server log. The
  server log then keeps FATAL and above only; `TestSQLPathErrorsNotLogged` checks this on the development cluster.
  Operators must apply all five settings; the goal 11 plan checks them only on the development cluster.
- Key files, certificates and data directories live in ignored or temporary paths (`.gitignore:12,24-25`). SQL files are
  ignored by `.gitignore:70`, so migrations use the `.pgsql` suffix.
- Names avoid the patterns of `scripts/scan_secrets.py`, and test canary strings are generated at run time.
- A planned log canary test plants unique run-time strings in a key file, a database password, an authorization context
  id, an inner signature and a request body. It first checks that each was processed, then searches the broadcaster,
  signer, PostgreSQL and node logs and the test output. Zero hits is the pass condition.

## 10. Service API (PROPOSED)

### 10.1 Kinds and the chain message each produces

| Kind | Chain message | Outer signer (fixed by kind) | Caller supplies | Broadcaster supplies |
| --- | --- | --- | --- | --- |
| `REGISTER_CONTENT` | `MsgRegisterContent` | `ADMITTED_SERVICE` key | content id, version, manifest digest, package bytes, hosting intent signed by a key the broadcaster does not hold | signer, business id |
| `WITHDRAW_CONTENT` | `MsgWithdrawContent` | the service key that registered the content | content id, version, reason digest, withdraw intent | signer, business id |
| `OFFER_LEASE` | `MsgOfferLease` | `ADMITTED_SERVICE` key, pinned per offer | offer id, provider id, content id and version, manifest digest, bytes, start, deadline, expiry, terms digest | signer, business id, term version 1 |
| `RELAY_ACCEPT_LEASE` | `MsgAcceptLease` | `BROADCASTER` key | offer id, lease id, provider acceptance | signer, business id |
| `RELAY_RENEW_LEASE` | `MsgRenewLease` | `BROADCASTER` key | lease id, term version, deadline, manifest digest, bytes, evidence id (empty), provider signature | signer, business id |
| `RELAY_END_LEASE` | `MsgEndLease{authority_end=false}` | `BROADCASTER` key | lease id, provider signature | signer, business id |
| `FORWARD_PROVIDER_TX` (Q2 (a) or (c)) | the provider's own signed bytes, unchanged | the provider's `ADMITTED_PROVIDER` account | raw transaction bytes | nothing; no sequence allocated |
| `FORWARD_AUDIT_BATCH` (Q2 (c) only; not recommended) | an `AUDITOR`-signed `MsgSubmitAuditBatch` transaction, unchanged | the auditor's `AUDITOR` account | raw transaction bytes | nothing; no sequence allocated |

No kind exists for provider registry messages, council messages or `authority_end` (DR-11-12). An unknown kind is 422 and
writes nothing.

### 10.2 Endpoints (HTTP/1.1 JSON over mTLS on loopback)

| Method and path | Effect | Errors |
| --- | --- | --- |
| `POST /v1/intents` | validate, then insert intent and outbox rows in one database transaction; 201 new, 200 existing (same idempotency key and request digest) | 400 malformed, 401 no client certificate, 403 kind not allowed or permission missing, 404 object not visible to the caller, 409 same key with another digest or `CONTENT_ID_UNAVAILABLE`, 413 body above the cap, 422 invalid request or provider signature, 429 open-intent cap, 503 database unavailable |
| `GET /v1/intents/{intent_id}` | status view (section 10.8) | 404 unknown or another caller's |
| `GET /v1/intents?status=&page_token=&limit=` | the caller's own intents, at most 100 per page (PROPOSED) | 400 |
| `POST /v1/intents/{intent_id}/cancel` | only while REQUESTED with no attempt ever persisted: FAILED(`CANCELLED`) (T3) | 409 once an attempt exists (signed bytes cannot be unsent) |
| `GET /v1/signers` | public signer addresses and public keys per class, so an account host can name `submittingHost` in an intent (`chain/x/content/intent.go:16-22`) | none |
| `GET /v1/health` | chain id, latest height and time seen, observer freshness, stall flag, database reachability; no secrets | none |

`CONTENT_ID_UNAVAILABLE` is one answer for a content id that is pending or owned by another caller, held by another host on
chain, or void after another caller's failed register. It never says which, so it reveals nothing about another caller's
object (DR-11-34).

### 10.3 Request decoding and limits

- One Go struct per kind, decoded with unknown fields refused and a body cap of 65,536 bytes (PROPOSED; equal to
  `chain/genesis/bounds.go:29`). Ids are checked against the chain pattern `^[a-z0-9][a-z0-9._-]{0,63}$`
  (`chain/msgkit/ids.go:29-44`). Digests are 32 bytes. Integers are range-checked against the chain bounds before any
  insert.
- The typed request is converted to the protocol message and encoded with the app codec. The stored `request` column is
  that protobuf, so the worker never re-parses caller JSON.
- No new `.proto` file (DR-11-30). The bodies are JSON. The status field reuses the frozen `OperationStatus` names
  (`docs/protocol/proto/svn/offchain/v1/records.proto:45-52`).

### 10.4 Idempotency keys and request digests

- Every create carries a caller-chosen `idempotency_key` (same id pattern). `UNIQUE(caller_principal, idempotency_key)`
  decides in PostgreSQL. The same key and digest returns the existing intent; another digest is 409.
- HTTP digest: SHA-256 over a length-prefixed framing of the kind name, the deterministic protobuf encoding of the request
  message with `signer` and `business_id` empty, and the authorization context re-serialized from its typed struct with
  permissions sorted and deduplicated. JSON key order, whitespace and number spelling do not change it.
- SQL digest: the request is stored as `jsonb`, whose text form has a fixed key order and no insignificant whitespace, so
  `sha256(convert_to(request_json::text, 'UTF8'))` ignores key order. The worker later computes the HTTP digest from the
  decoded request and stores it next to the SQL digest.
- The idempotency key is the caller's dedupe; the business id is the chain's (section 12.2). Two keys with the same chain
  effect are two intents. The chain keeps one effect, and the second intent reports it honestly (DR-11-28).

### 10.5 Authorized request context (under Q4 option (a))

- Allowed fields only: `principal_kind` (`DEV_FIXTURE` in goal 11), `principal_id` (opaque, id pattern), `permissions`
  (`PUBLISH_CONTENT`, `WITHDRAW_CONTENT`, `SCHEDULE_HOSTING`, `RELAY_PROVIDER_SIGNED`) and `policy_ref` (id pattern). The
  server adds `caller_principal` and `request_digest`.
- Refused with 400 and no row: any other key, any email, IP address, user agent, token, cookie, header copy or free text.
- Stored as bounded `jsonb` (PROPOSED cap 2,048 bytes); never logged; returned to its own caller only as a digest.
- Opaque ids and digests are still linkable (REQ-OFF-004).

### 10.6 Caller authentication (DR-11-32, reversible)

- HTTP: TLS 1.3 with a required, verified client certificate. A development CA is generated at run time by
  `svn-broadcaster dev-ca` into an ignored directory (`.gitignore:12`) or a test temp directory. The principal is the
  certificate's single URI SAN `svn://principal/<id>`; a certificate without exactly one such SAN is refused. The listener
  binds loopback only.
- SQL path: each caller service connects with its own PostgreSQL role. `broadcaster.callers` maps the role name to the
  principal. Only the owner role writes that table, through `svn-broadcaster migrate --callers`.
- Development principals refuse to load when the chain id lacks the `shortvideo-dev-` prefix (`docs/protocol.md:136`), so a
  fixture cannot enable access on another chain (goal 13 rule,
  `docs/goals/13-authenticated-uploads-and-quarantine.md:19`).
- Alternatives kept open: a Unix socket with peer credentials, or per-caller bearer tokens. Production certificate issuance
  and rotation are goal 27.

### 10.7 Object permissions (DR-11-34; fail-closed; under Q3 option (a))

- The `callers` configuration maps a principal to its allowed kinds, an open-intent cap and optional bindings (a
  `provider_id` for a provider-agent principal). Start-up refuses caps that sum above the global bound (section 16).
- Read, list and cancel only intents whose `caller_principal` equals the requester. Another caller's intent answers 404, so
  ids cannot be probed.
- `REGISTER_CONTENT` needs `PUBLISH_CONTENT` and the check of section 7. The enqueue transaction inserts a
  `content_owners` row with status `PENDING`. One non-`VOID` row may exist per content id and version; a conflict is
  `CONTENT_ID_UNAVAILABLE`. The row becomes `EFFECTIVE` only when the register intent is COMMITTED with the pinned service
  address as signer and `Query/Content` shows that address as `submitting_host`. A FAILED register turns the row `VOID`.
- `WITHDRAW_CONTENT` needs `WITHDRAW_CONTENT`, an `EFFECTIVE` row with the same caller and principal, and `Query/Content`
  showing our service address as host, at enqueue and again before signing. Otherwise the answer is a uniform 404.
- `OFFER_LEASE` needs `SCHEDULE_HOSTING`, an `EFFECTIVE` row of this caller, the on-chain host match and content not
  withdrawn, at enqueue and again before signing.
- Relay kinds need `RELAY_PROVIDER_SIGNED`, and either a caller bound to that provider id or a COMMITTED `OFFER_LEASE`
  intent of this caller for that offer, committed by our service address. The provider signature pre-check always runs.
- Per-caller open-intent counters are updated in the enqueue transaction under local lock and statement timeouts
  (PROPOSED 2 s and 5 s). A timeout is 503 and writes nothing. No global row is locked.

### 10.8 Status shown to callers (DR-11-09; no protocol change)

```
{ "intent_id", "kind", "status": "OPERATION_STATUS_REQUESTED|SUBMITTED|COMMITTED|FAILED|PENDING_STALL",
  "detail": { "phase": "QUEUED|RETRY_WAIT|SIGNED|ACKNOWLEDGED|UNCERTAIN|AWAITING_DEADLINE",
              "reason", "attempts", "last_ack": {"code","codespace"},
              "last_landing_height",                          // timeout_height of the live attempt, if any
              "observer": "FRESH|OBSERVER_UNAVAILABLE",
              "stall": {"since_height","since_time"} },       // only while a stall episode is open
  "commitment": { ... section 14 ... },                       // only when COMMITTED
  "failure": { "reason", "code", "codespace" },               // only when FAILED
  "expires_at", "created_at" }
```

- The protocol's client states are "requested, submitted, committed, failed, and pending while the chain is stalled"
  (`docs/protocol.md:217`). The frozen enum has no "uncertain" value
  (`docs/protocol/proto/svn/offchain/v1/records.proto:45-52`). "Uncertain" is `SUBMITTED` with
  `detail.phase = UNCERTAIN`. "Pending" is `PENDING_STALL` while a stall episode is open and the observer is fresh
  (section 15).
- `last_landing_height` is the last height at which a live attempt can still commit. `expires_at` bounds new attempts and
  sends only; it does not bound when an effect can land.

## 11. Outbox schema (PROPOSED)

### 11.1 Roles

- Schema `broadcaster`.
- `svn_broadcaster_owner` owns the schema, runs migrations and writes `callers`.
- `svn_broadcaster` (runtime) has DML on the outbox tables, SELECT only on `callers` and `schema_migrations`, and no DDL.
- One role per caller service for the SQL path: EXECUTE on the enqueue function and SELECT on a per-caller view only.
- The cluster superuser exists only in the development bootstrap and is not used at run time.
- The devnet test's own database role is SELECT-only on every table.
- Every query is parameterized (REQ-SEC-009); every function has a fixed `search_path`.
- There is no intent-key table (section 7) and no lease, offer or content state table. The broadcaster cannot create a final
  lease or any projection in the database (goal spec :28-29).

### 11.2 Tables (every text, bytea and jsonb column has a length `CHECK`)

| Table | Main columns | Constraints and notes |
| --- | --- | --- |
| `schema_migrations` | version PK, name, sha256, applied_at | the runner refuses a changed checksum or a gap |
| `chain_binding` | singleton PK, chain_id UNIQUE, bound_at | the one chain the database serves; every intent, commitment, stall episode and health row references it; written by the owner role |
| `caller_limits` | principal PK, open_intent_cap (1 to 4,096), enqueue_count | per-caller open-intent cap on both paths; the caller's row lock serializes only that caller's enqueues; `register_caller` refuses a cap that takes the sum of all caps above 16,384 |
| `callers` | role_name PK, principal UNIQUE | SQL-path mapping; written by the owner role only |
| `signers` | address bytea(20) PK, account_number, next_sequence, observed_height, lock_key UNIQUE, halted_reason, halted_at | the row lock serializes sequence allocation; `halted_reason` is set on a signer conflict (DR-11-15); `observed_height` also rises to every stored committed height. The key class (`SERVICE`, `BROADCASTER`) and key name depend on Q1 and are not in migration 0001 |
| `signer_events` | id, signer_address, sequence, foreign_tx_hash, detected_at, cleared_at, cleared_by_command | one row per foreign use of a signer sequence; `signer-clear` sets the `cleared_*` columns |
| `intents` | intent_id uuid PK, caller_principal, idempotency_key, request_digest bytea(32), sql_digest bytea(32) NULL, kind, request bytea, request_json jsonb NULL, auth_context jsonb, validated bool, chain_id, signer_address FK, business_id, business_attempt, state, phase, reason, created_at, expires_at, next_attempt_at, attempt_count, final_at, committed_tx_hash, own_transaction bool, transitions_suppressed | `UNIQUE(caller_principal, idempotency_key)`; `CHECK(expires_at > created_at)`; partial index on non-final state and `next_attempt_at`; `chain_id` must equal the configured chain id; a trigger refuses any UPDATE or DELETE of a COMMITTED or FAILED row |
| `business_attempts` | intent_id, n, business_id UNIQUE, created_at, outcome | a new business id only after reconciliation shows no effect; n bounded (PROPOSED) |
| `attempts` | attempt_id uuid PK, intent_id FK, n, signer_address, account_number, sequence, timeout_height, gas, tx_bytes, tx_hash bytea(32) UNIQUE, created_at, phase, sends_started, sends_recorded, unknown_sends, cache_answers, counted_refusals, acked, ack_code, ack_codespace, ack_log (clipped), committed_height, committed_index, committed_code, committed_codespace, acc_seq_present, gas_used, dead_reason, dead_at, worker_instance | partial `UNIQUE(signer_address, sequence) WHERE dead_at IS NULL AND committed_height IS NULL` (at most one live transaction per signer sequence); `CHECK(timeout_height > 0)`; `CHECK(sends_recorded <= sends_started)`; n from 1 to 32 (the hard bound of section 16; the PROPOSED cap is 16) |
| `commitments` | intent_id PK, chain_id, height, block_hash, tx_hash, tx_index, msg_index, code, codespace, gas_wanted, gas_used, events jsonb, idempotent_replay, first_commit_tx_hash, committing_signer bytea(20) | written once, only from a committed result (section 14). At commit the database checks the evidence against the intent's attempts: an intent that is not COMMITTED has no attempt resolved `COMMITTED_OK` or `COMMITTED_IDEMPOTENT`; `own_transaction` is true exactly when `first_commit_tx_hash` is an attempt of this intent resolved `COMMITTED_OK`, and no attempt of this intent in another phase carries it; an idempotent replay is read from an attempt of this intent resolved `COMMITTED_IDEMPOTENT`. A COMMITTED intent has no `reason` |
| `content_owners` | id PK, content_id, content_version, caller_principal, principal_id, signer_address, intent_id, status (`PENDING`, `EFFECTIVE`, `VOID`), status_at | depends on Q3; not in migration 0001, added by a later migration. Partial `UNIQUE(content_id, content_version) WHERE status <> 'VOID'` |
| `transitions` | id, intent_id, attempt_id, at, from_state, to_state, phase, reason, observation jsonb (bounded), worker_instance | append-only (a trigger refuses UPDATE and DELETE); a row only on a change of state, phase or observation class; at most 256 rows per intent (PROPOSED), then only `intents.transitions_suppressed` grows |
| `stall_episodes` | id, chain_id, reason (`HEIGHT_STALLED`, `NODE_UNREACHABLE`, `CATCHING_UP`), started_height, started_at, ended_height, ended_at | one open episode at a time; written by the observer |
| `chain_health` | chain_id PK, latest_height, latest_time, seen_at, observer_instance | updated every poll; its freshness decides between the stall overlay and `OBSERVER_UNAVAILABLE` |

- `worker_instance` is a boot nonce plus the process id, written on every attempt and transition, so a test can tell which
  process wrote a row.
- Every state UPDATE is a compare-and-set on the expected state and phase, and its row count is checked. A zero count is a
  lost race and is handled.
- Retention: no column is deleted automatically (DEC-004). The protocol's retained reconciliation fields are listed at
  `docs/protocol.md:272`; this schema stores more than that list, and Q4 names every extra item.

### 11.3 Enqueue in one database transaction

- HTTP path: validation (sections 6, 7, 10.7), then one transaction: insert into `intents` with `ON CONFLICT
  (caller_principal, idempotency_key) DO NOTHING RETURNING`; on no row, read the existing digest (same: 200; other: 409);
  update the per-caller counter under the local timeouts; insert `business_attempts` n=1 and any `content_owners` row;
  insert the first `transitions` row; `NOTIFY broadcaster_work` (delivered at commit only); commit.
- SQL path, for goal 13 and later services that write their own application row in the same transaction:
  `broadcaster.enqueue_intent(kind, request_json, auth_context, idempotency_key, expires_in)`. It is `SECURITY DEFINER`,
  has a fixed `search_path` and `REVOKE ALL FROM PUBLIC`, and takes the caller principal from `session_user` through
  `callers`. It checks size, kind and the per-caller cap, and inserts the row with `phase = QUEUED, validated = false`.
  Its error messages carry codes only, never argument values. The worker runs the full validation before any signing
  and fails the intent with `UNAUTHORIZED` or `INVALID` and no attempt if it does not pass (T2).
- The worker re-runs validation immediately before signing on both paths, because chain state can change after enqueue.
- Coupling proof: a test-only fixture schema writes its own row and calls the enqueue function in one transaction. Commit
  leaves both rows; rollback leaves neither; killing the client process in the middle leaves neither.

### 11.4 Migrations

Embedded `NNNN_name.pgsql` files, applied in order, each in its own transaction under a constant advisory transaction lock,
recorded with sha256, forward-only (DR-11-23). `migrate` runs as the owner role. `serve` refuses to start unless the schema
version equals the binary's.

## 12. Submission engine (PROPOSED)

### 12.1 Per-signer serialization

- One worker loop per signer. It takes a session advisory lock on a pinned connection, and every allocation and attempt
  write for that signer uses that connection. If the connection dies, the lock is gone and the next write fails, so nothing
  is signed or sent until the lock is held again. A second broadcaster process does not drive that signer.
- `reconcile-once` and `signer-clear` try the same lock and exit with an error if it is held. `status` only reads.
- One live transaction per signer (DR-11-26). A later sequence becomes invalid when an earlier one dies, and there is no gap
  queue. Concurrency comes from several signers.
- Next sequence: with no live attempt, read the committed account through the node. The answer's height must be at least
  the signer's last observed height and the height the caller needs; otherwise the node is stale and the loop waits. The
  auditor's account read ignores that height (`agents/auditor/chainclient/rpc.go:305-337`).
- Signer conflict (DR-11-15). If the chain sequence is above `next_sequence` and a `tx.acc_seq` lookup names a hash that is
  not one of our attempts, the signer halts: `halted_reason = SIGNER_CONFLICT`, a `signer_events` row and an operator
  alert. It signs nothing until an operator runs `signer-clear` for that signer and hash.

### 12.2 Building an attempt

- Business id: drawn from `crypto/rand` when the intent or a new business attempt is created; a short prefix plus 128 bits
  in lowercase base32, inside the chain id pattern; reused for every attempt of that business attempt (DR-11-07).
- Deadline: `timeout_height = latest committed height + K` (K PROPOSED), strictly above the `timeout_height` of every
  earlier attempt with the same signer and sequence. Because signing is deterministic (section 3.1), this keeps two attempts
  from having the same bytes. No `timeout_timestamp`.
- Gas: a fixed declared gas per kind (PROPOSED 2,000,000; the devnet and auditor use the same value,
  `chain/devnet/lifecycle_test.go:340`, `agents/auditor/config/config.go:100`). `WithdrawContent` is sized by simulation,
  because open offers raise its gas (`docs/architecture/leases.md:140`): `serve` builds the simulation transaction with the
  signer's public key and an empty signature, without calling the signer, and sends it to the tx service Simulate route of
  section 3.1, not to `/app/simulate`, which cannot encode an svn message response (route pending the orchestrator's
  ruling). The declared gas is the simulated gas plus a PROPOSED margin; a simulated gas above 20,000,000 is refused rather
  than capped, because such a transaction could only fail out of gas (DR-11-27). A wrong answer from the node can only make
  the transaction fail with out-of-gas, which reconciliation handles.
- The worker re-decodes the signed bytes and checks that the single message equals the stored request with signer and
  business id filled in.

### 12.3 Persist, then broadcast

1. Persist, in one transaction on the pinned connection: lock the intent row and continue only if it is
   `REQUESTED` in phase `QUEUED` or `RETRY_WAIT`, validated, before `expires_at` (database clock), below the attempt caps,
   with no live attempt and no open stall episode; lock the signer row and continue only if it is not halted; insert the
   attempt (bytes, hash, sequence, timeout height, phase `SIGNED`, `sends_started = 0`); advance `next_sequence`; move the
   intent to `IN_FLIGHT/SIGNED` by compare-and-set; add a `transitions` row; commit. If any condition fails, roll back and
   discard the signed bytes. They were never sent, so the sequence is not used on chain. Lock order is always intent row,
   then signer row.
2. Before every send of these bytes (the first and every rebroadcast), increment `sends_started` for the still-live attempt
   before `expires_at`, and commit. If that fails or matches no row, nothing is sent.
3. `broadcast_tx_sync` to the configured node.
4. Record the send outcome (section 12.4) in a second transaction. If this write fails, the outcome counts as UNKNOWN.

A crash between steps 2 and 4 leaves `sends_started > sends_recorded`, and each such send counts as UNKNOWN. This is the
auditor's order (bytes stored, then sent, then the record, which defaults to unknown;
`agents/auditor/chainclient/submit.go:590-635`) moved into PostgreSQL, with the send counted first.

T2 (validation failure), T3 (cancel) and T4 (expiry) take the same intent row lock and update only a `REQUESTED` row with
no live attempt (for T3, no attempt ever persisted), checked by row count. A race with the persist step therefore cannot
leave an on-chain effect under a FAILED row.

Fault points for tests exist only in a build with the tag `svn_faultpoint`: `after-sign-before-persist`,
`after-persist-commit`, `after-sends-started`, `after-broadcast-before-record`, and in the signer
`after-decode-before-reply`. The default build has none.

### 12.4 Classifying answers

Send outcomes, one per started send:

| Observation | Recorded outcome | Counts as a definitive refusal for (d2)? |
| --- | --- | --- |
| transport error, timeout, reset, or no record after a crash | UNKNOWN | no; blocks (d2) |
| JSON-RPC "tx already exists in cache" | CACHE (the node holds or committed these bytes) | no; blocks (d2) |
| JSON-RPC mempool full, tx too large, app connection error | NODE_REFUSED | yes |
| CheckTx sdk code 32 (sequence mismatch) | MEMPOOL_REJECTED{sdk, 32} | no; decided by (d3), because the bytes may have committed before a restart cleared the cache |
| CheckTx sdk code 4 inside the DR-10-41 window | MEMPOOL_REJECTED{sdk, 4} | yes; retryable |
| CheckTx any other non-zero code | MEMPOOL_REJECTED{codespace, code} | yes |
| CheckTx code 0 | ACK; shown as SUBMITTED, never COMMITTED | no; blocks (d2) |

Committed results, read from `/tx` by hash H; the returned bytes must hash to H:

| Observation | Recorded as | Attempt |
| --- | --- | --- |
| code 0 with the kind's typed `svn.events.v1.*` event | `COMMITTED_OK` | resolved |
| code 0 with only the idempotent result event | `COMMITTED_IDEMPOTENT` | resolved |
| code != 0 and `tx.acc_seq=<S>/<N>` of this attempt | `EXECUTION_FAILED` with code (sequence consumed) | dead |
| code != 0 and no `tx.acc_seq` attribute | `INCLUDED_ANTE_FAILED` with code (sequence not consumed) | live until the committed height is at least T + L, then dead (section 12.5) |
| code != 0 and `tx.acc_seq` names another account or sequence | integrity error: alert, signer halt | unchanged |
| not found | nothing yet | decided by section 12.5 |

The sequence-consumed rule reads the `tx.acc_seq` attribute, never the code (section 3.1).

An ante failure in a block does not end the attempt at once. CometBFT removes a transaction that failed in a block from
the mempool cache so that it can be resubmitted (`cometbft@v0.40.0/mempool/clist_mempool.go:604-609`, with
`KeepInvalidTxsInCache` off), and its indexer lets a later success of the same bytes replace the indexed failure
(`cometbft@v0.40.0/state/txindex/kv/kv.go:122-142`). The sequence was not consumed, so anyone holding the bytes (they are
public in the block) can have them included again until their `timeout_height`, for example after a role is restored or
when the block had no gas left (sdk 11). So the attempt stays live, the same stored bytes may be rebroadcast, and it is
resolved `INCLUDED_ANTE_FAILED` only once the node's committed height, read before `/tx`, is at least T + L and `/tx(H)`
still shows the failure. A retry or a FAILED intent therefore never leaves bytes behind that can still commit. A failure
with this attempt's `tx.acc_seq` consumed the sequence, so those bytes can never pass the ante again.

### 12.5 When an attempt can no longer commit (the dead rule)

Attempt A = (signer S, sequence N, timeout T, hash H). A is DEAD when one of these holds:

- (d1) `/tx(H)` is found with a non-zero code: with this attempt's `tx.acc_seq` at once; without it (the ante failed)
  only once the committed height is at least T + L and `/tx(H)` still shows the failure (section 12.4).
- (d2) At least one send started; every started send has a recorded outcome; every outcome is a counted refusal of the
  table above; none is UNKNOWN, CACHE, ACK or sdk 32. A refused transaction is not added to the node's mempool, so it is
  not gossiped. With zero started sends, (d2) never holds.
- (d3) The node's committed height is at least T + L (L PROPOSED, for asynchronous indexing; the devnet tests poll for the
  same reason, `chain/devnet/lease_test.go:406-408`), `/tx(H)` is not found, and either the committed sequence of S, read at
  a height of at least T + L, is at most N, or a search for `tx.acc_seq='<S>/N'` returns a hash other than H. If that hash
  is one of our attempts, that attempt is resolved by its own `/tx`; if not, the signer halts. If the sequence is above N
  and the search finds nothing, A stays UNCERTAIN and an alert is raised.

A is never declared dead from the local clock, from the intent's expiry, or while the chain height does not advance. If a
committed transaction's hash equals an attempt already marked DEAD, that is a fatal integrity alert: the signer halts,
nothing is rewritten, and the intent is never reported as committed by someone else.

### 12.6 Reconciliation before any resend

It runs at start-up for every non-final intent, after every UNCERTAIN, periodically, and always immediately before signing a
new attempt.

1. Re-decode every stored attempt's bytes with the app decoder. A decode failure is a fatal integrity error, reported and
   not repaired.
2. For each live attempt, read `/tx(H)`. Found: classify. Not found: apply the dead rule. If still live and before
   `expires_at`, rebroadcast the same stored bytes, never re-signed bytes, at most once per backoff step. After
   `expires_at`, observe only and send nothing.
3. Effect check, after every committed non-zero code, every dead attempt, and before any new attempt:
   - Look up the object (`Query/Content`, `Query/ActiveLeases`, `Query/Deadlines`) and the kind's typed event through
     `/tx_search` (`ContentRegistered.content_id`, `ContentWithdrawn.content_id`, `LeaseOffered.offer_id`,
     `LeaseAccepted.lease_id`, `LeaseRenewed.lease_id` with term version, `LeaseEnded.lease_id`).
   - "Effect exists" means a committing transaction was found, decoded from `/tx` with the hash check, its single message
     has the intent's type URL, and its business fields equal ours (`chain/x/leases/README.md:134-140`).
   - Signer rule. For `OFFER_LEASE`, `REGISTER_CONTENT` and `WITHDRAW_CONTENT` the committing signer must equal the
     intent's pinned service address. Same signer and our hash: COMMITTED, `own_transaction = true`. Same signer, not our
     hash: signer conflict, no transition. Another signer: FAILED(`OBJECT_TAKEN`). For relay kinds the signer is ignored
     (a `BROADCASTER` or the provider's own account may commit it): COMMITTED, `own_transaction` true only for our hash.
     The Wave 0 chain client is stricter for relay kinds: a committing transaction signed by our pinned key whose hash we
     never stored is a signer conflict there too (`services/broadcaster/internal/chain/effect.go:263-266`). This deviation
     is fail-closed and pending the orchestrator's ruling.
   - Business fields differ: FAILED(`OBJECT_TAKEN`). The Wave 0 chain client leaves `business_id` out of this comparison,
     because each business attempt and each second intent draws its own id; also pending the orchestrator's ruling.
   - The object changed state with no such transaction (begin-block expiry `LeaseEnded{EXPIRED}`,
     `chain/x/leases/README.md:323-337`; a council force-withdraw inside `MsgExecuteChange`,
     `chain/x/content/msg_server.go:250-254`; or a transaction of another kind): FAILED(`OBJECT_STATE_CHANGED`). Never
     COMMITTED.
4. Only if no effect exists and the intent may still start an attempt (below caps, before expiry, not stalled, signer not
   halted) is a new attempt signed: same business id, sequence from section 12.1, deadline from section 12.2.
5. A committed `REPLAY` with no effect means another payload took our business id: open a new business attempt with a new
   random id, bounded; at the bound, FAILED(`BUSINESS_ID_TAKEN`). Any other committed code with no effect: retryable codes go
   to RETRY_WAIT, the rest to FAILED(`EXECUTION_FAILED{code}`). A resend's code is never mapped onto an earlier attempt.

No step edits history by hand. A `transitions` row records the observation behind each change.

### 12.7 Retries, backoff and caps (all PROPOSED, DR-11-10)

- Retryable: `INCLUDED_ANTE_FAILED` with sdk 30 (`timeout_height` passed), 42 (`timeout_timestamp` passed) or 32 (sequence
  race; resync), sdk 4 within the DR-10-41 window (a PROPOSED number of tries, at least two blocks apart; DR-11-18), node
  refusals, transport errors, sdk 11 (no block gas left).
- Not retryable (FAILED, after the effect check): svn `UNAUTHORIZED`, `FORBIDDEN`, `BOUNDS`, `FEE_REJECTED` at CheckTx (a
  configuration or request error; an `UNAUTHORIZED` signer also halts the signer), sdk 2 (decode), and every committed
  execution failure except `REPLAY`.
- Backoff with full jitter; the next attempt time is stored, so restarts keep the schedule.
- Caps: attempts per business id, business attempts per intent, attempts per intent (section 16). The auditor bounds
  different things: `MaxSubmissions = 16` per batch and `maxRounds = 4` per call
  (`agents/auditor/chainclient/submit.go:78-82`).
- A new attempt starts only before `expires_at` (database clock). A live attempt is never abandoned because of expiry, and
  it is not resent after expiry.

## 13. States and transitions (PROPOSED)

### 13.1 Intent states and what callers see

| Internal state / phase | Meaning | `OperationStatus` shown |
| --- | --- | --- |
| `REQUESTED/QUEUED` | rows committed; nothing signed | REQUESTED |
| `REQUESTED/RETRY_WAIT` | every earlier attempt is dead and has no effect; next attempt at `next_attempt_at` | REQUESTED |
| `IN_FLIGHT/SIGNED` | one attempt persisted; no send outcome recorded | SUBMITTED (phase SIGNED) |
| `IN_FLIGHT/ACKNOWLEDGED` | CheckTx code 0 on the node | SUBMITTED |
| `IN_FLIGHT/UNCERTAIN` | an UNKNOWN or CACHE send; may commit | SUBMITTED (phase UNCERTAIN) |
| `IN_FLIGHT/AWAITING_DEADLINE` | expiry passed; a live attempt may still commit; no more sends | SUBMITTED (reason `EXPIRED_AWAITING_CHAIN`, `last_landing_height`) |
| `COMMITTED` (terminal) | section 13.4 | COMMITTED |
| `FAILED` (terminal) | `EXECUTION_FAILED{code}`, `REJECTED{code}`, `EXPIRED_NOT_SENT`, `EXPIRED_DEAD`, `ATTEMPTS_EXHAUSTED`, `OBJECT_TAKEN`, `OBJECT_STATE_CHANGED`, `BUSINESS_ID_TAKEN`, `UNAUTHORIZED`, `INVALID`, `PROVIDER_SIGNATURE_INVALID`, `PROVIDER_MUST_RESIGN`, `CANCELLED` | FAILED |
| any non-terminal, stall episode open, observer fresh | the episode is recorded (section 15); the overlay is applied at read time | PENDING_STALL (detail keeps the phase) |
| any non-terminal, observer stale | no stall claim | the underlying status with `detail.observer = OBSERVER_UNAVAILABLE` |

### 13.2 Attempt phases

- Non-terminal phases are ordered by knowledge: `SIGNED < UNCERTAIN < ACKNOWLEDGED`. A send outcome raises the phase to the
  highest knowledge seen and never lowers it. A CACHE or UNKNOWN answer on a rebroadcast of an ACKNOWLEDGED attempt keeps
  ACKNOWLEDGED.
- Terminal phases: `COMMITTED_OK`, `COMMITTED_IDEMPOTENT`, `EXECUTION_FAILED`, `INCLUDED_ANTE_FAILED`, `DEAD`. They never
  change.
- A refusal is a send outcome, not a phase. An attempt for which (d2) holds becomes `DEAD`.

### 13.3 Transition table (a `transitions` row on every change)

| # | From | Event (observation) | Guard | To |
| --- | --- | --- | --- | --- |
| T1 | none | create (HTTP or SQL) | validation passed (HTTP) or size, kind and cap passed (SQL) | REQUESTED/QUEUED |
| T2 | REQUESTED | worker validation fails | row lock, `state = REQUESTED`, no attempt | FAILED(`UNAUTHORIZED`, `INVALID`, `PROVIDER_SIGNATURE_INVALID`) |
| T3 | REQUESTED | cancel | row lock, `state = REQUESTED`, no attempt ever persisted | FAILED(`CANCELLED`) |
| T4 | REQUESTED | expiry passed (database clock) | row lock, `state = REQUESTED`, no live attempt | FAILED(`EXPIRED_NOT_SENT` if no attempt was ever persisted, else `EXPIRED_DEAD`) |
| T5 | REQUESTED or IN_FLIGHT | effect check finds our effect | section 12.6 step 3, signer rule | COMMITTED (`own_transaction` per section 12.6) |
| T6 | REQUESTED or IN_FLIGHT | effect check finds another account's effect or different fields | section 12.6 step 3 | FAILED(`OBJECT_TAKEN`) |
| T7 | REQUESTED or IN_FLIGHT | effect check finds a state change with no committing transaction of the kind | section 12.6 step 3 | FAILED(`OBJECT_STATE_CHANGED`) |
| T8 | REQUESTED | attempt persisted | section 12.3 step 1; signer lock held; not stalled; signer not halted | IN_FLIGHT/SIGNED |
| T9 | IN_FLIGHT/SIGNED or UNCERTAIN | ACK | | IN_FLIGHT/ACKNOWLEDGED |
| T10 | IN_FLIGHT/SIGNED | UNKNOWN or CACHE | | IN_FLIGHT/UNCERTAIN |
| T11 | IN_FLIGHT | `/tx(H)` code 0 with typed event | event matches the kind and the object; bytes hash to H | COMMITTED (canonical = H) |
| T12 | IN_FLIGHT | `/tx(H)` code 0, idempotent | first commitment found and equal (section 13.4) | COMMITTED (canonical = first commitment) |
| T13 | IN_FLIGHT | `/tx(H)` code != 0 | run the effect check first: T5, T6 or T7 if it decides | else REQUESTED/RETRY_WAIT (retryable, or `REPLAY` with a new business attempt, below caps, before expiry), else FAILED(`EXECUTION_FAILED{code}`, `BUSINESS_ID_TAKEN`, `ATTEMPTS_EXHAUSTED` or `EXPIRED_DEAD`) |
| T14 | IN_FLIGHT | attempt dead by (d2) or (d3) | run the effect check first | else REQUESTED/RETRY_WAIT (retryable, below caps, before expiry), FAILED(`REJECTED{code}`) for a non-retryable refusal, else FAILED(`ATTEMPTS_EXHAUSTED` or `EXPIRED_DEAD`) |
| T15 | IN_FLIGHT | expiry passed | attempt still live | IN_FLIGHT/AWAITING_DEADLINE (no sends) |
| T16 | any non-terminal | signer conflict detected | section 12.1 | unchanged; signer halted; observation row only |
| T17 | COMMITTED, FAILED | anything | | unchanged (absorbing; the database trigger refuses the update) |

The only transitions into COMMITTED are T5, T11 and T12, and each needs a committed result read from the chain.

### 13.4 What moves a row to COMMITTED

- T11: `/tx(H)` for one of this intent's attempts shows code 0 and the kind's typed event with this intent's object id
  (`msgkit.ParseCommitted`, `chain/msgkit/events.go:186-206`), and the returned bytes hash to H.
- T12 and T5: an idempotent result, or an effect found by object state, is accepted only after the first commitment of the
  same effect is found by typed event search, its business fields equal ours, and the signer rule holds. That first
  commitment becomes the canonical evidence. An idempotent replay is never itself the canonical commitment.
- A broadcast acknowledgement, a mempool answer, a timer or the broadcaster's own database never moves a row to COMMITTED
  (`docs/blueprint.md:305`; REQ-ACK-001, `docs/requirements.md:483`).
- Authority messages are not routed, so the DR-06-05 case (code 0 with an `authority_result` code) cannot reach this rule;
  if it ever does, it is FAILED.

## 14. Canonical commitment evidence for the indexer

For every COMMITTED intent the `commitments` row holds:

- `chain_id`;
- `height`, and `block_hash` from `/commit` at that height;
- `tx_hash`, the SHA-256 of the transaction bytes (`cometbft@v0.40.0/types/tx.go:29-35`), and `tx_index` from `/tx`;
- `msg_index` (0; one message per transaction);
- `code`, `codespace`, `gas_wanted`, `gas_used`;
- `committing_signer`;
- the typed events decoded with `ParseCommitted`, each with its position in the transaction result;
- `intent_id`, `business_id`, `idempotent_replay` and `first_commit_tx_hash`.

Goal 12 needs "chain identity, block height and stable transaction/message/event identity"
(`docs/goals/12-committed-state-indexer-and-reconciliation.md:19`). The evidence is relative to one configured node, which
is trusted for reads at beta (`docs/blueprint.md:391`). The indexer builds projections from committed events, not from this
table.

## 15. Chain outage behaviour (DR-11-11, DR-11-19)

- Observer. The worker reads `/status` every poll and writes `chain_health`. The observer is fresh when `seen_at` is within a
  PROPOSED number of poll intervals of the database clock.
- A stall episode opens when, with a fresh observer, the height has not advanced for T seconds of database clock
  (PROPOSED), or the node is unreachable or catching up. It closes when the height advances. Each opening and closing writes
  the `stall_episodes` row and one `transitions` row per non-terminal intent, in one transaction. The caps of all callers
  sum to at most 16,384; if more intents are open than that (caps lowered below the intents already open), the store
  refuses the opening or closing and writes nothing, rather than leave an intent without its row.
- A dead observer is not a stall. If the worker or its RPC link dies, `seen_at` goes stale, and reads show the underlying
  status with `OBSERVER_UNAVAILABLE`, never PENDING_STALL and never COMMITTED.
- While a stall episode is open: new intents are still accepted within the caps, each with a required, bounded
  `expires_at` (`docs/blueprint.md:395`); no new attempt starts; live attempts stay live, are polled, and are rebroadcast
  only before their intent's `expires_at`.
- Outcomes. An intent that expires with nothing sent becomes FAILED(`EXPIRED_NOT_SENT`). An intent with a live attempt stays
  SUBMITTED/AWAITING_DEADLINE, with the PENDING_STALL overlay, until the chain passes its `timeout_height` or commits it. It
  then becomes COMMITTED (if included) or FAILED(`EXPIRED_DEAD`). It never becomes COMMITTED unless a committed block that
  contains it is read.
- The protocol says the broadcaster, not a chain handler, records `PENDING_STALL` (`docs/protocol.md:260`), and DEC-011's
  approved text says "the outbox records `PENDING_STALL`" (`docs/decision-register.md:337`).
- Reference difference (DR-11-19). The reference outbox parks the request with its business id and returns
  `ERROR_CODE_QUORUM` (`docs/evidence/goal-02/cases.py:64-77`). The protocol's error table says the fixture `down` set does
  not produce that code (`docs/protocol.md:129`). Goal 11 parks the request the same way (REQUESTED/QUEUED, business id
  assigned, no attempt, no chain write) and returns no chain code, because a stall is not a chain result.
- COMMITTED for `OFFER_LEASE` means an offer committed, not a lease. A chain outage cannot create a final lease in the
  database, because the outbox has no lease state (section 11.1).

## 16. Configuration and PROPOSED numbers (DR-11-10)

Configuration is a typed JSON file plus flags. There are no defaults for the chain id or the gateway.

| Key | Rule |
| --- | --- |
| `chain_id` | required; must equal the `/status` network at start and before each signing round; development keys refuse a chain id without `shortvideo-dev-` |
| `rpc_url` | required; loopback `http` by default; a non-loopback URL needs `https` and `allow_remote_rpc: true`; one node, disclosed as trusted for reads |
| `database` | Unix socket directory or loopback host, database, role, `sslmode` (required; lib/pq defaults to `require`, `lib/pq@v1.12.3/connector.go:304-306`); password only from a passfile owned by the reading uid; any `PG*` variable or `PQGO_DEBUG` refuses start |
| `listen` | loopback address; TLS server certificate, key and client CA paths |
| `signer` | socket path, expected serve uid and signer uid (Q1 (a)); or key names and paths (Q1 (b)) |
| `keys` (signer only) | name to file and class; no intent-key field exists |
| `intent_key_registry` | path of the operator-written public-key file (section 7) |
| `callers` | principal to kinds, per-caller cap and bindings |

Every number below is PROPOSED: the value and the hard bounds are new, none is a service level, and the owner is asked to
accept the labels, not the values. Each has hard lower and upper bounds in code.

| Number | Value (PROPOSED) | Hard bounds in code (PROPOSED) |
| --- | --- | --- |
| K, `timeout_height` offset (blocks) | 40 | 5 to 1,000 |
| K_max, the signer's deadline bound (blocks) | 1,000 | equal to K's upper bound |
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
| Declared gas per kind | 2,000,000 | 1 to 20,000,000 (the chain range, `chain/genesis/bounds.go:30-31`) |
| Withdraw simulation margin | 30 % | 0 to 100 %; result capped at 20,000,000 |
| RPC timeout (s) | 10 | 1 to 60 |
| Poll interval (ms) | 500 | 100 to 10,000 |
| List page size | 100 | 1 to 100 |
| sdk 4 retries (at least 2 blocks apart) | 3 | 1 to 10 |
| Request body cap (bytes) | 65,536 | 1,024 to 65,536 (the chain's `MaxTxBytes`, `chain/genesis/bounds.go:29`) |
| Authorization context cap (bytes) | 2,048 | 256 to 8,192 |
| `Query/Deadlines` page budget (pages of 100) | 10 | 1 to 50 |
| `ack_log` clip (bytes) | 512 | 0 to 4,096 |
| `transitions.observation` cap (bytes) | 2,048 | 256 to 8,192 |
| `transitions` rows per intent | 256 | 16 to 4,096 |
| Enqueue lock / statement timeout (s) | 2 / 5 | 0.1 to 30 / 1 to 60 |

The planned devnet tests run at the lower bound wherever a phase does not depend on the value, and print the values they
used.

## 17. Owner questions Q1 to Q4 (unanswered)

These four questions are security-critical or privacy questions. Each recommendation below is a proposal, not a decision.
The owner may combine options. None of them is answered. The parts of goal 11 that depend on an answer are not built until
the answer exists; nothing answer-dependent is built as an interim.

Under every option: the broadcaster never holds a council seat, `AUTHORITY`, `AUDITOR`, provider, validator, creator or user
key; it never signs a provider's consent or a hosting intent; it never reports COMMITTED from a broadcast acknowledgement;
and everything runs on the single-machine devnet only.

### 17.1 Q1 (DR-11-01): which keys may the broadcaster hold, for which messages, behind which development boundary?

Facts. Whoever holds a key can send everything its chain role allows. Submitter roles are written at genesis and by the
engine-only service rotation, which no message reaches (`chain/x/authority/README.md:22,38`), so no transaction revokes a
service or broadcaster key today. An `ADMITTED_SERVICE` account registers and withdraws content and offers leases
(`chain/x/content/msg_server.go:78,186`; `chain/x/leases/msg_server.go:239`). A `BROADCASTER` account relays a provider's
signed acceptance, renewal or end (`chain/x/leases/msg_server.go:405,628-630`) and may not sign content messages
(`chain/x/content/README.md:39`). A withdraw is an irreversible tombstone (`docs/protocol.md:234`), and the chain accepts a
withdraw intent signed by any key with the devnet test role (`chain/x/content/README.md:63`;
`chain/x/content/intent.go:126-152`). The goal's prerequisite names
"approved service/provider signing boundaries", and no such document exists (`docs/decision-register.md:23`). Two processes
get real isolation only when they run as two different non-root users (section 4.2).

Options:

- (a) Recommended. One `BROADCASTER` key and one `ADMITTED_SERVICE` key in a separate signer process under its own uid,
  with the allowlist, signer-field, `authority_end`, chain id, deadline and intent-key checks of section 5.2. One message
  per transaction. The owner would approve or reject this file at PR time on a pinned commit and sha256.
  Why: least privilege per key; an API or database bug cannot reach key bytes or make the service tombstone content with
  an intent key it made up.
  Limits: section 5.3 (what a compromised `serve` can still do); a holder of the service key bytes can register content
  with a self-made intent key, so the publication half of REQ-CHAIN-013 holds for the broadcaster's code path only; in CI
  (one uid) the split separates key bytes in memory only (section 4.2).
- (b) As (a), with the keys inside the API process (`serve`). One process fewer. Any bug or compromise of `serve` reaches the
  key bytes and skips the signer's checks. No uid separation is tested.
- (c) `BROADCASTER` key only. Content registration, withdrawal and offers leave goal 11; another service, not built here,
  would sign them. The chain refuses content messages from the broadcaster's key, so the publication permission is enforced
  by chain role for this key. Goal 11 cannot show content and offer submission, and goal 13 and later need a second signer.

Status: unanswered. Recommended option: (a). Sections 4, 5, 7, 10.1 and 16 (`signer`, `keys`) assume (a). Under (b) the
signer process, the uid rule and the socket checks disappear and the checks run inside `serve`. Under (c) the service kinds
and the service key disappear.

### 17.2 Q2 (DR-11-02, includes DR-11-06): which externally signed submissions may the broadcaster carry?

Facts. The goal asks to "support forwarding their valid signed transactions or the approved provider-side submission path"
(goal spec :21-23), and goal 16 names an "approved provider-signing path"
(`docs/goals/16-storage-provider-agent.md:22-23`); none is approved. A provider's consent is an ed25519 signature by its
current registered key over the exact offer terms; the chain verifies it; it has no chain id and, for renew and end, no
expiry (`chain/x/providers/README.md:6`; `chain/x/leases/README.md:146-168`). Signed transaction bytes without a
deadline can commit at any later time. For audit batches the owner ruled "No `BROADCASTER` relay"
(`docs/evidence/goal-09.md:74`), and goal 09 left "Outbox submission: Goal 11" open (`docs/evidence/goal-09.md:39`).

Options:

- (a) Recommended. Relay (P1) and verbatim forwarding (P3) now; provider self-submission (P2) in goal 16; audit batches stay
  on the auditor's own path, and goal 09's open item is closed as "not through the outbox".
  Why: both paths the goal names are covered; the broadcaster can delay consent but never alter it, and P2 is the
  provider's remedy against delay.
- (b) Relay (P1) only; P2 in goal 16; audit batches on the auditor's path. One path fewer to secure. Providers cannot hand
  over a ready signed transaction.
- (c) As (a), plus verbatim forwarding of `AUDITOR`-signed audit batch transactions. The broadcaster holds no auditor key;
  the chain still requires the auditor as signer. The outbox then stores audit batches and their retries, and goal 09's
  auditor status path and the outbox can disagree.
- (d) P2 only: the broadcaster never carries provider consent. No relay; every provider submits from its own admitted
  account; goal 11's relay tests become reconciliation-only.

Status: unanswered. Recommended option: (a). Sections 6 and 10.1 assume (a). Goal 09's "Outbox submission" item stays
DEFERRED until the answer.

### 17.3 Q3 (DR-11-03): the publication permission, devnet hosting-intent keys and object permissions

Facts. REQ-CHAIN-013 tests that the broadcaster "cannot ... skip the publication permission bit"
(`docs/requirements.md:452`), and nothing defines that bit. Content registration needs a hosting intent, but the chain
cannot tell whether a creator authorized it (`chain/x/content/README.md:61`); off the devnet it is blocked by DEC-001
(`chain/x/content/README.md:60`). The broadcaster acts with its own chain authority for many callers, so it must check per
caller what each may do (a confused-deputy risk). A second service account on the devnet can register a content id or copy
a pending offer first (`chain/devnet/init.go:138`; `chain/x/leases/msg_server.go:280-294`).

Options:

- (a) Recommended. An operator-registered intent key per principal; the broadcaster holds none; ownership counts only after
  the chain confirms it (sections 7 and 10.7). A separate devnet fixture holds the intent keys; it is a test stand-in, not
  DEC-001 custody.
  Why: the strongest check available without a chain change; a broadcaster bug or a hostile caller cannot publish or
  tombstone for someone else.
- (b) As (a) without the registry: the broadcaster trusts the caller's permission claim plus any valid devnet intent
  signature. No operator file to manage. Any caller with the kind can publish with an intent key it made up.
- (c) The bit is the off-chain community publication approval (DEC-003, `docs/protocol.md:195`): the broadcaster refuses
  offers for content without that approval; intents as in (b). No approval service exists until a later goal, so goal 11
  would test against a fixture flag, and registration itself stays unguarded.

Status: unanswered. Recommended option: (a). Sections 7, 10.5 and 10.7 assume (a).

### 17.4 Q4 (DR-11-04): what may the outbox store, and how is it minimized?

Facts. The goal asks the outbox to persist an "authorized request context" (goal spec :19). The protocol lists the
reconciliation fields it retains (`docs/protocol.md:272`). Goal 11 needs more. These stored items are person-linked or
reusable authorizations:

- the caller principal and the opaque `principal_id` (in each request and in ownership rows): they link a creator to
  content; read by the broadcaster and the owning caller; returned to that caller only;
- the authorization context (principal kind, permissions, policy reference): read by the broadcaster; returned only as a
  digest;
- the intent-key registry (principal to public key): an operator file; public keys only;
- the stored request and transaction bytes: they contain provider signatures and hosting intents, which carry no chain id,
  and renew and end signatures never expire on chain, so a leaked row can be submitted until the provider renews or
  rotates; returned only as digests;
- clipped node answers and transition observations: operational, with no personal fields by construction.

Emails, IP addresses, tokens, headers and free text are refused at the API. DEC-004 sets no retention period
(`docs/decision-register.md:127`). The privacy assessment is an unmet gate for public launch
(`docs/decision-register.md:217`). Opaque ids are still linkable.

Options:

- (a) Recommended. The minimal fields above, digests in answers, a local expiry for relayed signatures, and clearing of
  reusable authorizations from rows that ended without any send (section 8). This is data minimization, not a retention
  period.
- (b) As (a) without the clearing step. Full history for debugging. Unsent provider signatures and intents stay in the
  database until a retention period exists.
- (c) As (a) or (b), plus automatic deletion after a period the owner sets. Goal 11 will not invent the number. Deleting
  reconciliation rows can conflict with the protocol's retained-fields list, so the period would apply only to the extra
  items above.

Status: unanswered. Recommended option: (a). Sections 8, 10.5 and 11.2 assume (a).

## 18. Security-relevant decision requests with interim rules (open)

Each request below has an interim rule. The interim is the recommended option. It is not an owner decision. The goal 11
plan implements and tests it and reports the request open. None of these requests is answered.

### 18.1 DR-11-05: commit, failure, uncertainty and deadlines

- Options: (a) COMMITTED only from a committed result (section 13.4); FAILED only from a committed code after the effect
  check, a definitive refusal (d2), or chain proof that the attempt cannot commit (d3); every attempt carries
  `timeout_height`. (b) No deadline field: uncertainty then resolves only through the account sequence, and admitted bytes
  can wait in a mempool with no time limit (section 3.1). (c) A `timeout_timestamp` deadline, which the ante checks against
  block time (`cosmos-sdk@v0.55.0/x/auth/ante/basic.go:218-224`); the dead rule (d3) is stated in heights, so (a) uses
  heights.
- Recommended: (a). Fail-closed. `[Educated Guess]` Setting `timeout_height` is not a protocol change:
  `docs/protocol.md:187` constrains only the fee amount and the gas limit. The goal 11 plan tests the ante semantics.
- Status: open; unanswered.

### 18.2 DR-11-06: audit batches and goal 09's "Outbox submission: Goal 11"

- Options: (a) audit batches stay on the auditor's own submit path (Q2 (a), (b) or (d)); (b) the broadcaster forwards
  `AUDITOR`-signed batch transactions unchanged with the P3 rules, holding no `AUDITOR` key (Q2 (c)). A broadcaster-signed
  batch is not an option: the goal 09 ruling forbids it (`docs/evidence/goal-09.md:74`).
- Recommended: (a). Until the answer, no audit-batch kind exists, and goal 09's item (`docs/evidence/goal-09.md:39`) stays
  DEFERRED.
- Status: open; part of Q2; unanswered.

### 18.3 DR-11-07: business ids

- Options: (a) random per business attempt, drawn from `crypto/rand`, stable across its resends; a new id only after
  reconciliation shows no effect; bounded business attempts. (b) Derived from the intent id: stable but predictable, so any
  admitted account can take it first (section 3.3). (c) Business ids bound to the signer: a protocol change (DR-09-28).
- Recommended: (a). Fail-closed against prediction, not against front-running: a broadcast already in a mempool can still be
  taken first (`chain/x/evidence/README.md:704-712`).
- Status: open; unanswered.

### 18.4 DR-11-08: the reconciliation read path

- Options: (a) one configured node, disclosed as trusted for reads (`docs/blueprint.md:391`): `/tx` by hash with a hash
  check, `/tx_search` by `tx.acc_seq` and by typed event, gRPC-routed object queries, height-bound account reads, `/commit`
  for block hashes, simulation for withdraw gas only (through the tx service Simulate route, because `/app/simulate`
  cannot encode an svn message response; section 3.1); never raw `/store` or `/check_tx`. (b) Several nodes compared:
  a second node is a comparison, not a proof. (c) Verified state proofs (a light client): not available here, and not
  claimed.
- Recommended: (a). Fail-closed.
- Status: open; unanswered.

### 18.5 DR-11-09: caller-visible status

- Options: (a) the five frozen `OperationStatus` values plus `detail.phase` (UNCERTAIN, AWAITING_DEADLINE, RETRY_WAIT),
  `detail.observer` and `last_landing_height` (section 10.8). (b) A new `UNCERTAIN` enum value: a change of the frozen
  `records.proto`, recorded as a protocol addition (section 20), not made. (c) Show an uncertain submission as
  `PENDING_STALL`: this would merge a stalled chain with an unresolved submission on a live chain.
- Recommended: (a). No protocol change.
- Status: open; unanswered.

### 18.6 DR-11-10: PROPOSED numbers

- Options: (a) the values of section 16, each with hard code bounds; the owner accepts the labels, not the values. (b) The
  owner sets the values now. (c) No built-in values: every number must be configured before start.
- Recommended: (a). Not fail-closed: these are new numbers.
- Status: open; unanswered.

### 18.7 DR-11-11: the stall rule

- Options: (a) stall episodes are recorded; no new attempt while stalled; live attempts are never abandoned and are not
  resent after expiry; an intent with nothing sent fails at its expiry; a stale observer is reported as such, not as a
  stall (section 15). (b) Keep rebroadcasting live attempts after the caller's expiry until their `timeout_height`: sends
  would continue after the caller's deadline. (c) Fail live attempts at the caller's expiry: an effect could then land under
  a FAILED row.
- Recommended: (a). Fail-closed.
- Status: open; unanswered.

### 18.8 DR-11-12: messages that are not routed

- Options: (a) council messages, `MsgExecuteChange`, `authority_end` and provider registry messages are not routed; a
  committed provider key rotation is reconciled by query (`chain/x/providers/README.md:126-128`). (b) Route provider registry
  messages as verbatim forwards. (c) Route `authority_end` with an `AUTHORITY` key: one signature then ends any lease
  (`docs/protocol.md:239`).
- Recommended: (a). Fail-closed.
- Status: open; unanswered.

### 18.9 DR-11-13: service-key rotation

- Facts: content keeps its `submitting_host` and an offer keeps its scheduler, and a withdraw must come from the content's
  host (`chain/x/content/msg_server.go:215`).
- Options: (a) intents pin their signer; a rotated service key leaves content hosts and offer schedulers on the old address;
  rotation is goal 26. (b) Re-pin open intents to the new address in goal 11: withdraws and replays signed by the new
  address would be refused by the host and scheduler checks.
- Recommended: (a).
- Status: open; unanswered.

### 18.10 DR-11-14: one message per transaction

- Options: (a) one message per transaction. (b) Several intents per transaction: a multi-message transaction is atomic, so
  one failing intent fails the others.
- Recommended: (a). Fail-closed.
- Status: open; unanswered.

### 18.11 DR-11-15: signer conflict

- Options: (a) a foreign use of a broadcaster key's sequence halts that signer until an operator runs `signer-clear`, which
  records the acknowledgement in `signer_events`. (b) Resync the sequence and continue: a leaked key's use would pass
  without an operator seeing it. (c) Halt the whole service: every signer stops for one key's conflict.
- Recommended: (a). Fail-closed.
- Status: open; unanswered.

### 18.12 DR-11-16: threat-model additions

- The additions: confused deputy, log leakage, business-id squatting, stored authorizations, the same-uid boundary.
  `docs/threat-model.md:18` also says a host-held key exists "only if DEC-001 is later accepted", while DEC-001 is approved
  (`docs/decision-register.md:23`).
- Options: (a) the owner adds them to `docs/threat-model.md` in a goal 01 change. (b) They stay in this file only.
- Recommended: (a). Goal 11 does not edit `docs/threat-model.md`; it is a goal 01 document.
- Status: asked; unanswered.

### 18.13 DR-11-17: the reading of REQ-CHAIN-013

- Options: (a) one status, BLOCKED (DEC-001 and DR-07-01: no chain-level creator binding; DR-11-01: boundary not approved),
  with the sub-results listed under it: the provider half, the publication check of the broadcaster's code path, and the
  caller's permission claim (goals 13, 19, 21). (b) Split the row: a change of the requirements matrix, a goal 01 document.
  (c) Record the devnet halves as VERIFIED for the row.
- Recommended: (a). The owner confirms the reading.
- Status: open; unanswered.

### 18.14 DR-11-18: the DR-10-41 window

- Options: (a) sdk 4 is retryable a PROPOSED number of times, at least two blocks apart; then FAILED and a signer alert.
  (b) Treat sdk 4 as a definitive refusal: a correctly signed transaction sent after a node restart fails. (c) Retry without
  a limit: a wrongly configured key would loop.
- Recommended: (a). Not fail-closed for liveness.
- Status: open; unanswered.

### 18.15 DR-11-19: stall report versus the reference outbox

- Options: (a) park the request with its business id, as the reference does (`docs/evidence/goal-02/cases.py:64-77`), and
  return no chain code. (b) Return `ERROR_CODE_QUORUM` as the reference does, which `docs/protocol.md:129` says the fixture
  `down` set does not produce.
- Recommended: (a). A recorded spec difference, tested by the reference's observable state: a pending row with its business
  id and no transaction for it on chain.
- Status: open; unanswered.

### 18.16 DR-11-34: object ownership

- Options: (a) a content owner row counts only after the register committed under our service address and the chain shows
  that host; a failed register voids it; conflicts get one uniform answer; offers and relays require that ownership
  (section 10.7). (b) Ownership from the first enqueue alone: a pending claim would count before the chain confirms it. (c)
  No ownership rows: any caller with the kind could act on any content our service hosts (a confused deputy).
- Recommended: (a). Fail-closed. It overlaps Q3; the Q3 answer can change it.
- Status: open; unanswered.

## 19. Reversible implementation choices

These are reversible choices made while planning goal 11. They need no owner input to proceed. They are not owner
decisions.

| Id | Choice |
| --- | --- |
| DR-11-20 | A Go module at `services/broadcaster` replaces the Python scaffold, its scaffold test and its path pin. It needs `services/broadcaster/` in `SOURCE_ALLOW` of `docs/evidence/goal-01/check_requirements.py`, a goal 01 contract change disclosed to the owner. The blueprint names an "operator-authorized Go broadcaster" (`docs/blueprint.md:373`). |
| DR-11-21 | Copy and generalize the auditor's chain client; the auditor stays unchanged. |
| DR-11-22 | `github.com/lib/pq v1.12.3` is the only new direct dependency. It is already in `chain/go.mod` as an indirect dependency (`chain/go.mod:129`). It is opened with an explicit `pq.Config` and `NewConnectorConfig`. |
| DR-11-23 | Embedded `.pgsql` migrations and a small forward-only runner (`.gitignore:70` ignores `*.sql`). |
| DR-11-24 | One development cluster helper used by tests and `dev-db`; crash hooks only in test code; `dev-db` refuses data directories outside the system temp directory and chain ids without `shortvideo-dev-`. |
| DR-11-25 | CI installs PostgreSQL in the jobs that run the module's tests; new race shards; no PostgreSQL skip variable. New required check names need the owner's branch-protection update. |
| DR-11-26 | One live transaction per signer. |
| DR-11-27 | Fixed gas per kind; withdraw gas from simulation with a PROPOSED margin; a simulated gas above 20,000,000 is refused. The plan named `/app/simulate`; the Wave 0 client uses the tx service Simulate route (section 3.1), pending the orchestrator's ruling. |
| DR-11-28 | A second intent with an identical effect reports COMMITTED with `own_transaction = false`. |
| DR-11-29 | Devnet tests in `chain/devnet` drive the built binary through a loopback fault proxy and build-tagged fault points. |
| DR-11-30 | JSON bodies, no new `.proto`; status names reuse `OperationStatus`. |
| DR-11-31 | Internal phase names beyond `docs/protocol.md:217`. |
| DR-11-32 | Caller authentication: development mTLS on loopback, plus one PostgreSQL role per caller for the SQL path (section 10.6). |
| DR-11-33 | `[Educated Guess]` "Updates the application projection" (`docs/blueprint.md:305`) means intent status and commitment evidence only. |
| DR-11-35 | Distinct uids for `signer` and `serve` when the launcher is root; same-uid runs are labelled and do not verify the boundary (section 4.2). |
| DR-11-36 | A phase-selection variable for devnet mutation killers only; CI never sets it. |
| DR-11-37 | Development cluster log settings (section 9). |

## 20. Protocol additions recorded, not made (DR-11-40)

- An `UNCERTAIN` `OperationStatus` value.
- Business ids bound to the signer (DR-09-28).
- A chain id and an expiry inside provider inner payloads and hosting intents (DR-07-01, DR-08-04).
- Get-by-id queries for leases, offers and business ids (DR-08-08).
- A registered hosting-intent key per creator on chain (DR-07-01).
- An on-chain revocation of service and broadcaster keys.

The goal 11 plan changes no `.proto` file, `bounds.json`, `docs/protocol.md` or chain production code.

## 21. Residual risks

- Reads trust one configured node. A lying node can make the broadcaster report COMMITTED. Goal 12 reconciles; no light
  client is claimed.
- No transaction revokes a compromised `ADMITTED_SERVICE` or `BROADCASTER` key; service rotation is engine-only
  (`chain/x/authority/README.md:22`). Least privilege is the main control.
- Whoever holds the `ADMITTED_SERVICE` key bytes can mint devnet hosting intents with a key they make up. Creator permission
  is not enforced on chain (`chain/x/content/README.md:61`). The registry check binds only the broadcaster's own code path.
- A compromised `serve` with distinct uids can still send any `MsgOfferLease` the chain accepts, relay any stored provider
  signature, replay a stored hosting intent for its own object, withhold or reorder submissions, and read the outbox. With
  one uid it can also read the key files (section 4.2).
- A root process on the host can read every key file.
- Provider inner signatures have no chain id, and RENEW and END have no expiry. A leaked outbox row can be submitted by any
  `BROADCASTER` or the provider's account until a renewal or rotation. Local expiry revokes nothing on chain.
- Business-id squatting: an admitted account can take a pending business id. The broadcaster spends a bounded number of
  business attempts and then fails. This is denial of service, never a wrong transition.
- A broadcaster can delay or withhold a provider's signed acceptance until the offer expires. P2 is the remedy.
- One live transaction per signer limits throughput to about one transaction per block per signer.
- `LISTEN/NOTIFY` is a hint only. Correctness rests on polling under per-signer advisory locks and compare-and-set updates.
- Mempool gossip timing and same-sequence transactions in different mempools are outside the broadcaster's control. The
  planned tests check outcomes, not mempool internals.
- An effect can land after the caller's `expires_at`, up to the live attempt's `timeout_height` (`last_landing_height`).
- PostgreSQL crash safety depends on `fsync=on`. The outbox is not backed up in goal 11 (goal 26). The log settings of
  section 9 are checked only on the development cluster; an operator must apply them.
- The stall report differs from the reference outbox: no `ERROR_CODE_QUORUM` is returned (DR-11-19).
- Raw `/store` queries are unmetered and unpaged on the node, so the node RPC must stay private
  (`chain/x/leases/README.md:692`).
- The planned fault proxy covers lost requests and answers on one loopback link, not real network partitions. The planned
  PostgreSQL faults are process crashes, not host power loss.

## 22. Steps that need a human approval or an independent review

1. Answers to Q1 to Q4, and approval of this file at a pinned commit and sha256 (DR-11-01).
2. The fate of goal 09's outbox deferral, through the Q2 answer (DR-11-06).
3. Acceptance of the PROPOSED number labels (DR-11-10), of the language and layout change, and of the goal 01
   `SOURCE_ALLOW` edit it causes (DR-11-20).
4. Production key custody for service and broadcaster keys (DEC-001; goal 26), and any non-devnet chain id.
5. A retention period for outbox rows and transaction bytes (DEC-004), and the privacy assessment
   (`docs/decision-register.md:217`).
6. Threat-model additions (DR-11-16) in the goal 01 document.
7. Branch-protection updates for new CI checks; a goal 11 draft pull request and its merge.
8. An independent security review of the signer, the policy pre-checks, the reconciliation rules and the SQL privilege
   model (REQ-SEC-011; goal 29). A model-assisted review is not that review.
