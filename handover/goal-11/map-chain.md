# Goal 11 chain map: transaction path, client code, devnet hooks (main dbf0897)

Reader: chain reader for the goal 11 plan. Code base: worktree `wt-11`, branch `goal-11`, created from main `dbf0897`
(`git log -1` = "auditor: keep every permitted run within the pending-run encoding limit (#14)"). PR 15 (genesis validate
Version.App rule) touches `chain/cmd/svnd/cli/genesis.go` and `chain/app` validation only; nothing below depends on it.

Method: read the repository files, and the pinned framework sources in the Go module cache (`/root/go/pkg/mod`):
`github.com/cosmos/cosmos-sdk@v0.55.0` (below `SDK/`), `github.com/cometbft/cometbft@v0.40.0` (below `CMT/`),
`github.com/decred/dcrd/dcrec/secp256k1/v4@v4.4.1`. Nothing was built, run or edited. Repository paths are relative to
the worktree root. A statement marked **[Educated Guess]** is an inference, not something the code or a test shows.
"Derived from source" means read in the pinned source but not exercised by any test in this repository.

---

## 1. Transaction encoding and signing

### 1.1 Codec and sign mode

- `app.MakeEncoding` builds the only TxConfig: `authtx.NewTxConfigWithOptions` with `EnabledSignModes:
  [SIGN_MODE_DIRECT]` and `ProtoDecoder: strictTxDecoder(cdc)` (`chain/app/encoding.go:44-81`, sign mode at `:68-71`).
  Clients use the same function: the auditor (`agents/auditor/chainclient/rpc.go:276-285`) and the devnet tests
  (`chain/devnet/lifecycle_test.go:267-274`).
- `strictTxDecoder` runs the SDK decoder, then `unknownproto.RejectUnknownFieldsStrict` on the body bytes, so a
  non-critical unknown field (field number with bit 0x400) anywhere in the body or its messages fails with sdk code 2
  (`chain/app/encoding.go:110-138`). TxBody fields 3 `timeout_height`, 4 `unordered`, 5 `timeout_timestamp` are known
  fields (`SDK/types/tx/tx.pb.go:366,380,387`), so they decode. **[Educated Guess]** the strict body check accepts a set
  `timeout_timestamp` (a nested `google.protobuf.Timestamp`); no repository test sets it (section 3.5).
- Signers are custom: each of the 14 registered messages has a 20-byte `signer` field and `signerBytes` returns exactly
  that (`chain/app/encoding.go:51-53,144-154,156-173`). One message type = one signer. A transaction whose messages name
  different signers needs one signature per signer; the ante admits every signer separately
  (`chain/app/ante.go:151-160`).
- Bech32 account prefix `svn` (`chain/address/prefix.go:11`). The `tx.acc_seq` event value is `<svn1...>/<sequence>`
  (section 5.1).

### 1.2 What a signer must supply

The two Go builders in the repository do the same thing (`agents/auditor/chainclient/tx.go:34-82`,
`chain/devnet/lifecycle_test.go:334-364`, `chain/devnet/lease_test.go:139-171`,
`chain/devnet/registry_test.go:1068-1101`):

1. `builder.SetMsgs(msg...)`, `builder.SetGasLimit(gas)`; no fee, no memo, no timeout fields set.
2. A placeholder `SignatureV2{PubKey, SignMode DIRECT, Sequence}`, then
   `tx.SignWithPrivKey(ctx, SIGN_MODE_DIRECT, SignerData{Address(bech32), ChainID, AccountNumber, Sequence, PubKey}, ...)`,
   then `SetSignatures(sig)` and `TxEncoder()(builder.GetTx())`.
3. The auditor refuses a gas limit outside `[1, 20,000,000]`, an empty chain id, a key that is not the message signer,
   and an encoded transaction above 65,536 bytes before it broadcasts (`agents/auditor/chainclient/tx.go:36-45,78-80`).

Inputs and where they come from:

| Input | Source | Notes |
| --- | --- | --- |
| chain id | The node's genesis. `node.StartWith` gives the app the chain id CometBFT runs (`chain/node/start.go:74-77,190-196`); signature verification uses `ctx.ChainID()` (`SDK/x/auth/ante/sigverify.go:379`). | The auditor takes it from `/status` `NodeInfo.Network` (`agents/auditor/chainclient/rpc.go:297-309`, used at `submit.go:536,557`). It is not operator configuration. Devnet chain ids are `shortvideo-dev-<16 hex>`, random per `devnet.Init` unless set (`chain/devnet/init.go:195-201`). A wrong chain id fails signature verification, sdk code 4 (`SDK/x/auth/ante/sigverify.go:403-414`). |
| account number | Fixed at genesis: one auth account per submitter and per seat address, numbered in lowercase-hex address order, no pubkey, sequence 0 (`chain/app/state.go:268-296`). Read with ABCI query `/cosmos.auth.v1beta1.Query/Account` (`agents/auditor/chainclient/rpc.go:405-437`) or from the genesis auth state (`chain/devnet/lifecycle_test.go:239-265`). | No message creates a submitter account. The only runtime account creation is the council seat-key replacement (`chain/app/app.go:284-285`, `chain/x/authority/README.md:20`). |
| sequence | Committed value from the same account query (`rpc.go:436`). | The committed value excludes transactions waiting in a mempool (section 2). |
| gas limit | Declared by the client: `[1, 20,000,000]` outside simulation (`chain/app/ante.go:95-103`, `chain/genesis/bounds.go:29-31`). Devnet single messages use 2,000,000 (`chain/devnet/lifecycle_test.go:340`), multi-message transactions 10,000,000 (`chain/devnet/lease_test.go:50,176`); the auditor default is 2,000,000 (`agents/auditor/config/config.go:100`). | Simulation can estimate gas (section 1.5). |
| pubkey | Set on the account by `SetPubKeyDecorator` on the first transaction (`chain/app/ante.go:38`; `SDK/x/auth/ante/sigverify.go:63-118`). | Genesis accounts carry none (`chain/app/state.go:292`). |

### 1.3 The ante chain actually installed

`NewAnteHandler` (`chain/app/ante.go:24-45`), installed by `app.New` (`chain/app/app.go:361-367`). Order and effect:

| # | Decorator | Rejects | Code | Runs on ReCheck |
| --- | --- | --- | --- | --- |
| 1 | `NewSetUpContextDecorator` | not a GasTx; gas limit above block `MaxGas` | sdk 2; sdk `ErrInvalidGasLimit` (`SDK/x/auth/ante/setup.go:30-50`) | yes |
| 2 | `NewExtensionOptionsDecorator(nil)` | any extension option (nil checker = reject all) | (`SDK/x/auth/ante/ext.go:17-21,35-38`) | yes |
| 3 | `NewValidateBasicDecorator` | `Tx.ValidateBasic`: missing body/auth info/fee, no signatures, signer count mismatch (`SDK/types/tx/types.go:39-95`, `SDK/x/auth/tx/builder.go:105-128`) | sdk | **no**, skipped on recheck (`SDK/x/auth/ante/basic.go:30-34`) |
| 4 | `NewTxTimeoutHeightDecorator` | `timeout_height` > 0 and block height > it; `timeout_timestamp` set and before block header time | sdk 30, sdk 42 (`SDK/x/auth/ante/basic.go:205-227`) | yes |
| 5 | `envelopeDecorator` | non-empty memo; fee payer or granter set; tip set | svn `BOUNDS`; svn `FEE_REJECTED` (`chain/app/ante.go:66-86`) | yes |
| 6 | `NewValidateMemoDecorator` | memo above the auth param (256); unreachable after #5 | sdk | yes |
| 7 | `boundsDecorator` | gas limit outside `[1, 20,000,000]` (0 allowed only in simulation); tx bytes > 65,536 | svn `BOUNDS` (`chain/app/ante.go:90-108`) | yes |
| 8 | `NewConsumeGasForTxSizeDecorator` | out of gas | sdk 11; 10 gas per byte (`SDK/x/auth/types/params.go:11`) | yes |
| 9 | `feeRejectDecorator` | any fee amount | svn `FEE_REJECTED` (`chain/app/ante.go:112-121`) | yes |
| 10 | `NewSetPubKeyDecorator` | bad pubkey; emits `tx.acc_seq` and `tx.signature` events | (`SDK/x/auth/ante/sigverify.go:120-157`) | yes |
| 11 | `NewValidateSigCountDecorator` | more than 7 signatures (`SDK/x/auth/types/params.go:10`) | sdk | yes |
| 12 | `NewSigGasConsumeDecorator` | out of gas; secp256k1 1000, ed25519 590 gas (`SDK/x/auth/types/params.go:12-13`) | sdk 11 | yes |
| 13 | `NewSigVerificationDecorator` | unordered tx (disabled); sequence mismatch; bad signature | sdk 37; sdk 32; sdk 4 (`SDK/x/auth/ante/sigverify.go:313-421`) | sequence yes, signature **no** (`:387`) |
| 14 | `admittedDecorator` | signer with no admission row and not a seat; seat signing anything but propose/approve | svn `UNAUTHORIZED`; svn `FORBIDDEN` (`chain/app/ante.go:139-180`) | yes |
| 15 | `NewIncrementSequenceDecorator` | (writes sequence+1 for every signer) | (`SDK/x/auth/ante/sigverify.go:505-532`) | yes |

Facts that follow from the table:

- No fee, memo, fee payer, fee granter or tip can carry an intent id or anything else (`chain/app/ante.go:47-58`). The
  rejection of memo, payer, granter and tip is tested in `TestTransactionsRejectVideoBytes`
  (`chain/app/registry_decode_test.go:218-225,321`). Zero-fee transactions are still metered
  (`TestZeroFeeTransactionIsMetered`, `chain/app/authority_limits_test.go:156`).
- CheckTx runs only the ante chain; message handlers run only in FinalizeBlock and Simulate (notes-08-10.md, goal 07 round
  2; `SDK/baseapp/baseapp.go:920-927` inserts into the app mempool and `runMsgs` executes handlers only in finalize and
  simulate). Role, business-gate, capacity and every other handler check is not part of a CheckTx answer. Example: an
  acceptance with CheckTx code 0 commits with svn `CAPACITY` (`chain/devnet/lease_test.go:507-530`).
- An ante failure writes nothing: `RunTx` returns before `msCache.Write()` of the ante branch
  (`SDK/baseapp/baseapp.go:897-919`). A handler failure after a passing ante keeps the ante writes (sequence +1, pubkey)
  and drops the message branch (`SDK/baseapp/baseapp.go:919,975-983`).
- Every svn handler starts with `msgkit.Enforce`: gas meter limit in `[1, 20,000,000]` (skipped in simulation), tx bytes
  <= 65,536, `FixedMessageGas` 50,000, signer 20 bytes (`chain/msgkit/enforce.go:9-49`).

### 1.4 DR-10-41: CheckTx uses account number 0 before the first commit after a load

- `BaseApp.Init` sets the check state with an empty header, height 0 (`SDK/baseapp/baseapp.go:437-440`). `Commit` resets
  the check state with the header of the block just committed (`SDK/baseapp/abci.go:1059,1099-1100`).
- `SigVerificationDecorator` uses account number 0 when `ctx.BlockHeight() == 0` (`SDK/x/auth/ante/sigverify.go:377-382`).
- So after every start of a node, until that node commits a block, `CheckTx` refuses a correctly signed transaction from
  an account whose number is not 0 with sdk code 4. Documented in `docs/architecture/governance-and-upgrades.md:667-683`
  (section 14.5) and `docs/operations/council-changes.md:25`; tested by `TestUpgradeWindowCheckTxAndQueries`
  (`chain/app/upgrade_rehearsal_test.go:1739-1749`) and the devnet probe (`chain/devnet/upgrade_test.go:1711,1866`). The
  devnet tests wait for height >= 3 after a start before broadcasting (`chain/devnet/upgrade_test.go:185-205`,
  `chain/devnet/lease_test.go:440-473`). Status: open decision request DR-10-41 (interim: documented, not refused).
- Consequence for goal 11: a CheckTx sdk/4 from a node that just restarted is not a permanent rejection of correctly
  signed bytes; the account with number 0 (whichever address sorts first, `chain/app/state.go:286-292`) is not affected.
  Two goal-10 test flakes came from this (`docs/evidence/goal-10.md:124,183`).

### 1.5 Size, gas and block capacity

- Tx bytes cap 65,536 and gas cap 20,000,000 (`chain/genesis/bounds.go:29-31`), enforced in the ante (`ante.go:101-106`)
  and again in every handler (`msgkit/enforce.go:34-42`). CometBFT's own mempool cap is 4 MiB and is not reached
  (`CMT/config/config.go:1033`).
- Genesis consensus params: CometBFT defaults with `Block.MaxGas = 20,000,000` and `Version.App = 1`
  (`chain/devnet/init.go:576-581`). The app enables the block gas meter (`chain/app/app.go:165`).
- CometBFT reaps the mempool in FIFO order and stops when the sum of declared `gasWanted` would exceed `MaxGas`
  (`CMT/mempool/clist_mempool.go:525-563`); with the default no-op app mempool the SDK `PrepareProposal` passes the reaped
  transactions through, selecting by declared gas and bytes, without re-running the ante
  (`SDK/baseapp/baseapp.go:195-197`, `SDK/baseapp/abci_utils.go:282-305`), and `ProcessProposal` accepts
  (`abci_utils.go:429-432`). Arithmetic: at 2,000,000 declared gas per transaction at most 10 transactions fit in a
  block; at 20,000,000 one. A failed transaction still consumes block gas (`SDK/baseapp/baseapp.go:820-840`).
- Gas estimation: `Simulate` runs on a cache branch of the check state (`SDK/baseapp/baseapp.go:638-641`), reachable as
  ABCI query `/app/simulate` (`SDK/baseapp/abci.go:1136-1150`) or gRPC `/cosmos.tx.v1beta1.Service/Simulate`
  (`SDK/x/auth/tx/service.go:76`). A declared gas of 0 is accepted only in simulation (`chain/app/ante.go:96-103`).
  Simulated gas equals committed gas for the lease and registry messages (`TestSimulateEstimatesGas`,
  `chain/app/registry_simulate_test.go:20-117`, `docs/architecture/leases.md:64`). Derived from source: simulation
  still checks the sequence (`sigverify.go:368-375` has no simulate exception), against the check state, which includes
  waiting transactions.

### 1.6 Transaction hash and signature determinism

- CometBFT hash = SHA-256 of the raw transaction bytes (`CMT/types/tx.go:29-35`, `CMT/crypto/tmhash/hash.go:19-22`). The
  auditor computes it before broadcasting (`agents/auditor/chainclient/tx.go:81`) and stores the bytes first
  (`submit.go:596-614`).
- ed25519 signatures are deterministic. secp256k1 in the default build (`!libsecp256k1_sdk`,
  `SDK/crypto/keys/secp256k1/secp256k1_nocgo.go:1`) uses decred `ecdsa.SignCompact` (`:15-21`), which calls
  `signRFC6979` (`decred .../ecdsa/signature.go:775-778`): deterministic. So re-signing the same messages, gas, chain id,
  account number and sequence reproduces the same bytes and hash; any change (gas, timeout, sequence, business id)
  gives a new hash. The devnet shows it for ed25519 (auditor re-submission "rebuilds the same bytes at the same account
  sequence", `chain/devnet/audit_test.go:1085-1125`); no test shows it for secp256k1 (derived from source).

---

## 2. Sequence handling, CheckTx state and the mempool

### 2.1 Check state

- CheckTx (`SDK/baseapp/abci.go:361-399`) runs `RunTx(execModeCheck)`; a passing ante branch is written into the check
  state (`SDK/baseapp/baseapp.go:919`), so the check-state sequence of the signer is +1 after each accepted CheckTx.
  The SDK app mempool is the no-op default (`SDK/baseapp/baseapp.go:195-197`; `chain/` never calls `SetMempool`), so
  CometBFT's mempool is the only mempool.
- After each Commit the check state is reset to committed state (`SDK/baseapp/abci.go:1100`) and CometBFT rechecks every
  remaining mempool transaction (`CMT/mempool/clist_mempool.go:629-632,646-690`) with `CheckTxType_Recheck`; recheck skips
  ValidateBasic and the signature, but checks sequence, admission, bounds and timeouts (table 1.3). A recheck failure
  evicts the transaction and removes it from the cache (`clist_mempool.go:481-504`).
- Derived from source: the CometBFT `/check_tx` route calls the app's CheckTx directly and does not add the transaction to
  the mempool (`CMT/rpc/core/mempool.go:186-194`), but the app still runs it in `execModeCheck`, which writes the
  sequence into the check state. A `/check_tx` of a valid transaction at sequence N therefore makes a later broadcast of
  any transaction at N fail CheckTx with sdk 32 until the next commit. The devnet calls `/check_tx` only on transactions
  that already committed (`chain/devnet/registry_test.go:401-428`, `lease_test.go:399-417`), where the ante fails and
  nothing is written. Do not use `/check_tx` as a dry run; use Simulate.

### 2.2 A second transaction with the same sequence

| Case | CheckTx / broadcast answer | Outcome |
| --- | --- | --- |
| Same bytes while the first waits in this node's mempool | JSON-RPC error `tx already exists in cache` (`CMT/mempool/clist_mempool.go:258-270`; `CMT/mempool/errors.go:12`) | Not a rejection. Devnet: `checkMempoolCache` (`chain/devnet/audit_test.go:1327-1340`); the auditor records the answer UNKNOWN and keeps the batch SUBMITTED (`audit_test.go:1113-1125`). |
| Same bytes after the first committed with code 0 | `ErrTxInCache` while the bytes are still in the LRU cache (committed OK transactions are pushed into it, `clist_mempool.go:603-606`; cache size 10,000, `CMT/config/config.go:1032`), otherwise CheckTx sdk 32 | Not included again; the original inclusion does not move (`chain/devnet/registry_test.go:401-444`, `lease_test.go:399-438`). |
| Same bytes after the first committed with a non-zero code | Removed from the cache (`clist_mempool.go:607-609`); CheckTx then answers sdk 32 when the ante passed in the block (sequence consumed), or the same ante error again when the ante failed in the block (sequence not consumed, e.g. a passed timeout) | Derived from source. |
| Different bytes, same sequence, first waiting in this node's mempool | CheckTx sdk 32 (check state already at N+1) | Not admitted here. Auditor warning `REBROADCAST_NOT_ACCEPTED` (`agents/auditor/chainclient/submit.go:87-93,562-569`; unit test with a fake RPC, `chainclient_test.go:492-534`). |
| Different bytes, same sequence, first waiting only on another node | This node can admit it | **[Educated Guess]** both can sit in different mempools; whichever a proposer includes first consumes N, the other is evicted by recheck or included and fails its ante with sdk 32 (no state change). A re-signed transaction at the same sequence is not a reliable cancel or replacement. |
| Sequence ahead (N+1 before N) | CheckTx sdk 32 (`sigverify.go:368-375`) | No gap queue: transactions of one signer must reach a node in sequence order. |

### 2.3 CometBFT mempool configuration actually used

`chain/node/start.go:211-228` builds the CometBFT config from `cmtcfg.DefaultConfig()` plus `node.json` values (moniker,
P2P, RPC listen, peers, pex) and devnet consensus timers (`:237-248`). It does not read `config.toml` (`:165-166`; devnet
`Init` writes one for operators, `chain/devnet/init.go:428-444`). The mempool and RPC sections are therefore the CometBFT
v0.40.0 defaults, and changing them needs a code change:

| Setting | Value | Source |
| --- | --- | --- |
| mempool type | `flood` (clist, gossip) | `CMT/config/config.go:46-48,1023` |
| recheck | true, recheck timeout 1 s | `:1024-1025` |
| broadcast (gossip to peers) | true | `:1026` |
| WAL | disabled (`wal_dir` empty): the mempool is in memory only | `:1027` |
| size / max txs bytes | 5,000 txs / 1 GiB | `:1030-1031` |
| cache size | 10,000 | `:1032` |
| keep invalid txs in cache | false (field `:982-985`, not set in the defaults) | |
| TTL | none: CometBFT v0.40.0 `MempoolConfig` has no TTL fields (`:932-1019`); only the unused `app` mempool type mentions TTL (`CMT/mempool/app_mempool.go:30`) | |
| RPC `timeout_broadcast_tx_commit` | 10 s | `:460` |
| RPC max body | 1 MB; unsafe routes off | `:454,464` |
| tx indexer | `kv` | `:1460-1464` |
| ABCI responses kept (`/block_results`) | yes (`discard_abci_responses` false) | `:1417-1421` |

Consequences (from the rows above):

- A transaction that passed CheckTx waits in every mempool that accepted it until a block includes it, a recheck evicts
  it, or that node restarts. Nothing expires it by time. With quorum lost it waits and commits when quorum returns
  (`chain/devnet/audit_test.go:1085-1153`).
- Gossip copies an accepted transaction to the other validators. A restart of the node the client used empties only that
  node's mempool; copies elsewhere can still commit (**[Educated Guess]** about timing, the mechanism is
  `Broadcast: true`).
- Unconfirmed transactions are visible with `/num_unconfirmed_txs` and `/unconfirmed_txs`
  (`CMT/rpc/core/mempool.go:160-184`); the upgrade test uses `NumUnconfirmedTxs` (`chain/devnet/upgrade_test.go:410-430`).
  A per-node view only.
- A node that reports it is catching up refuses broadcasts with `endpoint is closed while node is catching up`
  (`CMT/rpc/core/mempool.go:17,25-27,39-41`); the auditor refuses a catching-up node's header (`rpc.go:305-307`).

### 2.4 What a broadcaster can rely on for per-signer sequences (facts, then guesses)

- Facts: the committed account query returns the committed sequence, not the check-state one (`rpc.go:405-437`);
  check state advances on every accepted CheckTx; there is no nonce-gap queue; a node restart drops its mempool and
  resets check state to committed state at height 0 (DR-10-41).
- **[Educated Guess]** a correct design keeps the next sequence per signer in its own durable state, submits one
  signer's transactions to one node in order, and, after any uncertain or failed submission, re-derives the next
  sequence from (committed sequence, own persisted attempts, `tx.acc_seq` search, section 4.3) before signing again.
- Unordered transactions would remove the sequence coupling but are disabled (section 3.3).

---

## 3. Transaction deadlines

### 3.1 `timeout_height`

- Works in this ante chain: `NewTxTimeoutHeightDecorator` is installed (`chain/app/ante.go:32`) and rejects when the
  context height > `timeout_height` (`SDK/x/auth/ante/basic.go:211-216`), sdk code 30
  (`SDK/types/errors/errors.go:100`).
- In CheckTx and recheck the context height is the last committed height h (check state header from Commit,
  `SDK/baseapp/abci.go:1100`); in FinalizeBlock it is the new block's height. So the last block that can include a
  transaction with `timeout_height = T` is T, and CheckTx at committed height T still admits it (derived from source).

### 3.2 `timeout_timestamp`

- Also checked by the same decorator for ordered transactions: rejected when set and before the block header time
  (`SDK/x/auth/ante/basic.go:218-224`), sdk code 42 (`errors.go:147`). The header time is CometBFT BFT time, not a
  local clock. Setting it: `SetTimeoutTimestamp` ignores a zero time (`SDK/x/auth/tx/builder.go:89-96`).
- CheckTx compares with the last committed block's time; FinalizeBlock with the new block's time.

### 3.3 Unordered transactions: refused

- The account keeper is built without `WithUnorderedTransactions(true)` (`chain/app/app.go:199-207`;
  `SDK/x/auth/keeper/keeper.go:115-121,168-170`). `SigVerificationDecorator` refuses `unordered=true` with sdk 37
  "unordered transactions are not enabled" (`SDK/x/auth/ante/sigverify.go:319-325`); `IncrementSequenceDecorator` would
  refuse too (`:506-511`); the auth PreBlock nonce sweep is gated on the same flag (`chain/app/authmodule.go:78-81`).
  Enabling them changes consensus behaviour and is a decision, not an implementation detail.

### 3.4 What a deadline guarantees

- A transaction whose deadline passed either (a) is evicted by recheck and never enters a block (`/tx` stays not found),
  or (b) is included by a proposer that reaped it before recheck evicted it (the no-op `PrepareProposal` does not re-run
  the ante, `abci_utils.go:286-305`) and fails its ante in FinalizeBlock with sdk 30 or 42: committed failure, no state
  written, sequence not consumed (`SDK/baseapp/baseapp.go:897-917`; the result carries only ante events,
  `baseapp.go:722-732`).
- Derived from source: once a committed block has height > `timeout_height` (or header time > `timeout_timestamp`), no
  later block can execute that transaction successfully on any honest node. That is the only chain-enforced way to turn
  "unknown" into "will never execute" for bytes that may sit in some mempool; without a deadline, bytes can commit at any
  later time (section 2.3).
- A deadline that expires leaves the signer's sequence N unused, so later transactions signed for N+1.. fail until N is
  used again (section 2.2).

### 3.5 Test status

No repository code sets `timeout_height`, `timeout_timestamp` or `unordered` (grep of `chain/` and `agents/` for
`SetTimeoutHeight`, `SetTimeoutTimestamp`, `SetUnordered`, `ErrTxTimeout`: only `chain/app/ante.go:32`). Goal 11 must
test deadline behaviour itself (CheckTx, recheck eviction, included-after-deadline code, sequence not consumed).

---

## 4. Telling the outcomes apart

### 4.1 CometBFT broadcast routes (`CMT/rpc/core/mempool.go`)

| Route | Returns | Lines |
| --- | --- | --- |
| `broadcast_tx_async` | hash only, after the mempool's pre-checks; no CheckTx answer | `:24-34` |
| `broadcast_tx_sync` | CheckTx code, codespace, log, hash; a pre-CheckTx mempool error is a JSON-RPC error; a cancelled request is `broadcast confirmation not received` | `:38-66` |
| `broadcast_tx_commit` | CheckTx and the DeliverTx result; subscribes to the tx event; after 10 s returns the CheckTx result together with the error `timed out waiting for tx to be included in a block`; limited by `max_subscription_clients` | `:70-158` |

The auditor and the devnet use `broadcast_tx_sync` then poll `/tx` (`agents/auditor/chainclient/rpc.go:446-456`,
`submit.go:573-580,636-660`; `chain/devnet/lease_test.go:190-211`). The devnet registry demo uses `broadcast_tx_commit`
with a 30 s client timeout (`chain/devnet/registry_test.go:321-330`); that is a test convenience, and the commit route
can return an error after CheckTx admitted the transaction.

### 4.2 Classification table

| Observation | Meaning | Can the business effect still happen from these bytes? |
| --- | --- | --- |
| Transport error (dial refused, HTTP timeout, reset) before a JSON-RPC answer | Unknown: CheckTx may have admitted the bytes. Devnet: broadcast to a stopped node fails (`chain/devnet/lease_test.go:587-599`) | Yes, until a deadline passes (3.4) or the bytes are seen committed. The auditor stores the bytes first and records `UNKNOWN` (`submit.go:596-634`). |
| JSON-RPC error `tx already exists in cache` | The node holds these bytes (waiting or committed OK) | Yes / already did. Not a rejection. |
| JSON-RPC error mempool full, tx too large, app connection error, catching up | Not admitted by this node now (`CMT/mempool/clist_mempool.go:234-255`, `mempool.go:25-27`) | Only through another node that holds them. |
| `broadcast_tx_sync` code != 0 (CheckTx rejection) | Ante refused: svn `BOUNDS`, `FEE_REJECTED`, `UNAUTHORIZED`, `FORBIDDEN`; sdk 2, 4, 11, 30, 32, 37, 42 (table 1.3) | Not from this admission. Removed from the node's cache (`clist_mempool.go:466-469`). sdk 4 right after a node start can be DR-10-41 (1.4); sdk 32 can mean the sequence is held by a waiting transaction (2.2). |
| `broadcast_tx_sync` code 0 | Admitted to this node's mempool; not executed | Yes; it can still fail in its block (`lease_test.go:507-530`). |
| `/tx` found, `tx_result.code` != 0, codespace `svn` | Committed failure of a handler; message branch dropped, ante writes (sequence) kept | No (for these bytes). Protocol code name: `chainclient.SVNCode` (`rpc.go:486-497`). |
| `/tx` found, code != 0, codespace `sdk` (e.g. 11 out of gas, 30/42 deadline, 32 sequence) | Committed failure; ante failures write nothing, out-of-gas in a handler keeps the ante writes | No. |
| `/tx` found, code 0, `authority_result` event with `code=...` | Council "keep" outcome (EXPIRED/REJECTED mark): the payload was not applied (`chain/x/authority/msg.go:93-98,129-132`; README G-7 `chain/x/authority/README.md:140`) | No. Code 0 is not success for council messages. |
| `/tx` found, code 0, `<module>_result idempotent=true` and no typed event | Idempotent success: the business id already committed with the same digest, in this or another transaction (`chain/msgkit/business.go:87-115`, `msgkit/events.go:324-329`) | The effect exists once; this transaction added nothing. |
| `/tx` found, code 0, typed `svn.events.v1.*` events | Committed success of this transaction | Effect applied in this block. |
| `/tx` not found | Not indexed on this node: waiting somewhere, dropped, never sent, or committed but not yet indexed | Unknown. Indexing is asynchronous after commit (`chain/devnet/lease_test.go:406-408`); "not found" is a JSON-RPC error `tx (HASH) not found` (`CMT/rpc/core/tx.go:31-33`; `rpc.go:467-477`). |

Committed failures are final: CometBFT has instant finality. Every read trusts the one node that answered
(`agents/auditor/chainclient/rpc.go:109-114`); `CommittedTx` does not compare the returned bytes with the requested hash
(`rpc.go:470-484`).

### 4.3 Read paths for reconciliation

| Need | Route | Notes |
| --- | --- | --- |
| Result by hash | `/tx?hash=` (`CMT/rpc/core/tx.go:20-51`): height, index, `tx_result` (code, codespace, log, gas, events), raw tx, optional Merkle proof (`prove=true`, `:35-40`) | Needs the kv indexer (default). |
| Which transaction used signer sequence N | `/tx_search?query="tx.acc_seq='svn1.../N'"` | `tx.acc_seq` is emitted by `SetPubKeyDecorator` for every transaction whose ante reaches it, failed ones included (`sigverify.go:120-157`; failed results keep ante events, `SDK/baseapp/baseapp.go:722-732`); every attribute is indexed because the app sets no index-events filter (`SDK/types/events.go:311-331`; no `SetIndexEvents` in `chain/`). Query length <= 512, 30 per page default, 100 max (`CMT/rpc/core/events.go:18`, `env.go:23-24`). Not used by any repository test (derived from source). |
| Typed event search | `/tx_search?query="svn.events.v1.LeaseAccepted.lease_id='la'"`, `message.action='/svn.leases.v1.MsgAcceptLease'`, `message.sender='svn1...'` | Event names in section 5. Not used by any repository test. |
| Block results | `/block_results?height=` (`CMT/rpc/core/blocks.go:184-205`): per-tx results in block order, `finalize_block_events` (BeginBlock/EndBlock events with `mode`), app hash | The devnet scans raw `/block_results` events (`chain/devnet/registry_test.go:933`). |
| Header, commit, app hash | `/block`, `/commit`, `/status` | `devnet.BlockHash`, `ReadStatus` (`chain/devnet/status.go:68-90,163-187`). |
| Chain health | `/status` latest height/time, `catching_up` | A stalled `latest_block_height` means no quorum. |

### 4.4 Node gRPC: SDK tx service is wired but untested

- After CometBFT starts, `node.StartWith` registers the SDK tx service, the CometBFT service and the node service on the
  query router with a client context over the in-process CometBFT RPC (`chain/node/start.go:113-122`;
  `chain/app/app.go:669-685`), then serves every router service on a plain `grpc.NewServer()` at the `grpc` address of
  `svn-listen.json` (`chain/node/start.go:124-139`; `SDK/baseapp/grpcserver.go:113-143`). No TLS, no authentication.
- So `cosmos.tx.v1beta1.Service` (`GetTx`, `GetTxsEvent`, `BroadcastTx` sync/async, `Simulate`, `GetBlockWithTxs`) is
  served (`SDK/x/auth/tx/service.go:46,76,110,149,225-227,313-323`; `SDK/client/broadcast.go:120-147`). The REST gateway
  is off (`chain/app/app.go:337-339`).
- No repository test calls the tx service. The devnet uses node gRPC only for `svn.providers.v1.Query/Provider` with the
  block-height header (`chain/devnet/registry_test.go:759-805`). Treat gRPC tx calls as unverified until goal 11 tests
  them.

---

## 5. Events emitted per business operation

### 5.1 SDK events on every transaction

- Ante: `tx` events with `acc_seq=<bech32>/<seq>` and `signature=<base64>` (`SDK/x/auth/ante/sigverify.go:135-157`);
  kept on committed failures (4.3).
- Per message on success: `message` with `action=<type URL>`, `sender=<first signer bech32>`, `module`
  (`SDK/baseapp/baseapp.go:1083-1107`), and `msg_index` appended to every event of the message
  (`chain/msgkit/events.go:180-206`).
- A rejected message emits no module event (module READMEs below).

### 5.2 Module events

Typed events are `svn.events.v1` messages emitted by `msgkit.EmitTyped`: type = full proto name, attributes in field
order, integers decimal, all indexed (`chain/msgkit/events.go:60-106`; schema
`docs/protocol/proto/svn/events/v1/events.proto:15-81`). Parse committed ones with `msgkit.ParseCommitted`
(`events.go:191-206`) and block ones with `msgkit.ParseBlockEvent` (`events.go:227-240`).

| Operation | Success events | Idempotent replay | Source |
| --- | --- | --- | --- |
| `RegisterProvider` | `ProviderRegistered{provider_id, state=APPLIED}` | `providers_result{idempotent=true}` only | `chain/x/providers/README.md:168-175` |
| `UpdateProvider` | none (only the SDK `message` event) | same | `:174` |
| `RotateProviderKey`, `DrainProvider`, council admit/disable | `ProviderStateChanged{provider_id, state, key_generation}` | same | `:173` |
| `RegisterContent` | `ContentRegistered{content_id, content_version, manifest_sha256, package_bytes}` | `content_result{idempotent=true}` | `chain/x/content/README.md:77-83` |
| `WithdrawContent`, council force-withdraw | `ContentWithdrawn{content_id, content_version, history_erased=false}`, plus `leases_offer_closed{offer_id, reason=WITHDRAWN}` per voided offer | same | `content/README.md:81`; `chain/x/leases/README.md:385` |
| `OfferLease` | `LeaseOffered{offer_id, provider_id, content_id}` | `leases_result{idempotent=true}` | `leases/README.md:379,386` |
| `AcceptLease` | `LeaseAccepted{lease_id, provider_id, content_id, content_version, package_bytes, deadline_unix_seconds}` | same | `:380` |
| `RenewLease` | `LeaseRenewed{lease_id, term_version, deadline_unix_seconds}` | same | `:381` |
| `EndLease` | `LeaseEnded{lease_id, state=ENDED, released_bytes}` | same | `:382` |
| BeginBlock expiry | `LeaseEnded{state=EXPIRED}`, `leases_offer_closed{reason=EXPIRED}` with `mode=BeginBlock` | n/a | `:383-384` |
| `SubmitAuditBatch` | per item: `EvidenceAccepted{evidence_id, batch_id, kind, attestation=true}` then untyped `evidence_state{evidence_id, lease_id, state RECORDED|REJECTED}` | `evidence_result{idempotent=true}` only | `chain/x/evidence/README.md:383-389` |
| `MsgExecuteChange` success | hook events, then `PolicyChanged{policy_version, parameter_key}`, `ProposalExecuted{proposal_id, policy_version}` | `authority_result{idempotent=true}` | `chain/x/authority/README.md:20,119`; `msg.go:87-135` |
| council keep outcome | `authority_result{code}`, ABCI code 0 | | `authority/msg.go:129-132` |

No event carries the business id. The signer appears only as `message.sender` and in `tx.acc_seq`. A broadcaster that
needs "which transaction carried business id X" must keep that mapping itself (intent id -> tx hashes) or read the
ledger (section 7).

---

## 6. Business-id idempotency (what makes retries safe)

- `msgkit.Ledger.Gate`: id empty `MALFORMED`, outside `^[a-z0-9][a-z0-9._-]{0,63}$` `BOUNDS`; own ledger, then the other
  modules in name order (authority, content, evidence, leases, providers); absent proceeds, same 32-byte digest is an
  idempotent success, another digest `REPLAY`. The id is stored only when the message succeeds
  (`chain/msgkit/business.go:55-70,87-126`; `chain/genesis/bounds.go:41`). One namespace across five modules
  (`chain/app/app.go:218-220,286-290`).
- Digest = SHA-256 of SVN-JCS-1 of the spec's field set (`chain/msgkit/canon.go:120-129`). The signer is not in any set:
  providers (`chain/x/providers/README.md:104-109`), content (`chain/x/content/msg_server.go:100-106,199-204`), leases
  (`chain/x/leases/README.md:135-140`), evidence (`chain/x/evidence/msg_server.go:96-117`).
- Signer-sensitive exceptions on an idempotent hit: `OfferLease` by another scheduler `FORBIDDEN` (D-11,
  `leases/README.md:209`); `AcceptLease` by another provider's account `FORBIDDEN`, any `BROADCASTER` stays idempotent
  (D-13, `chain/x/leases/msg_server.go:427-439`); `RegisterProvider` copy by another account `FORBIDDEN` (DR-07-08,
  `providers/README.md:111-115`).
- Every check before the gate runs again on a retry, so an identical retry can become a rejection after intervening
  commits: another scheduler (D-11), another provider account (D-13), provider key rotation (`UNAUTHORIZED`, DR-08-12),
  end after a renewal (`UNAUTHORIZED`, D-12), lowered `max_package_bytes` (`BOUNDS`), role change
  (`docs/architecture/leases.md:140`, `chain/x/leases/README.md:607-615`). A retried committed rotation is
  `UNAUTHORIZED`, not idempotent: "A broadcaster must reconcile a rotation by querying the provider, not by resubmitting
  it" (`chain/x/providers/README.md:126-128`).
- Business ids are unsigned and global: any admitted account can commit another message under a pending business id
  seen in a mempool, so the victim gets `REPLAY` (front-running at no cost; `chain/x/evidence/README.md:703-712`).
  `REPLAY` alone does not tell whether our payload committed; compare the committed digest (section 7) with our own.
- Devnet proof: the same acceptance under two transaction hashes gives one `LeaseAccepted`, one `leases_result`, one
  reservation (`chain/devnet/lease_test.go:532-570`); another payload under the same id is `REPLAY` (`:571`); renewal and
  end retries are idempotent (`:631-653`); the same signed bytes through another node commit once and a rebroadcast is
  refused (`:587-607`). App level: `TestLeaseRetryScenarios` (`chain/app/lease_misc_test.go:189`, `docs/architecture/leases.md:60`).
- Multi-message transactions are atomic: one failing message fails all (`SDK/baseapp/baseapp.go:1002-1040`, branch written only when every message passed, `:975-983`); a REPLAY of one
  evidence batch fails every item (`evidence/README.md:703-705`).

---

## 7. Querying committed state by object id and by business id

gRPC-routed ABCI queries (`/svn.<module>.v1.Query/<Method>`, protobuf request in `data`, optional height) run on the
query gas meter (`chain/app/app.go:167-173`, `chain/app/querygas.go:20-36`); not found crosses ABCI as sdk
`ErrKeyNotFound` (`rpc.go:318-344`).

| Operation | Read-back | Notes |
| --- | --- | --- |
| provider register/update/rotate/drain | `/svn.providers.v1.Query/Provider{provider_id}` (every state) | `docs/architecture/registries.md:70`; `rpc.go:374-382` |
| content register/withdraw | `/svn.content.v1.Query/Content{content_id, content_version}` (record and tombstone) | `registries.md:72`; `rpc.go:364-372` |
| offer | no get-by-id: `/svn.leases.v1.Query/Deadlines{not_after_unix_seconds, page}` lists open offers and held leases by time | `docs/protocol/proto/svn/leases/v1/query.proto:26-35`; page cap 100 (`chain/genesis/bounds.go:36`) |
| accept/renew/end | `/svn.leases.v1.Query/ActiveLeases{provider_id, content_id}`: held leases only, at most one per pair; ended or expired leases are not listed | `query.proto:15-24`; "DR-08-08: no get-by-id query exists" (`rpc.go:346-362`) |
| audit batch | `/svn.evidence.v1.Query/Evidence{evidence_id | lease_id}` | `rpc.go:384-403` |
| council | `/svn.authority.v1.Query/Governance{page}` | `docs/protocol/proto/svn/authority/v1/query.proto:15-30` |
| policy | `/svn.policy.v1.Query/Policy` | `chain/x/policy/service.go:29` |
| account | `/cosmos.auth.v1beta1.Query/Account` | `chain/app/authmodule.go:73-76`; `rpc.go:405-437` |

There is no gRPC query by business id. The ledgers are readable only as raw store keys, ABCI query path
`/store/<store>/key` with `data = prefix || business_id`, value = the 32-byte digest:

| Store | Prefix | Source |
| --- | --- | --- |
| `authority` | `0x11` | `chain/x/authority/store.go:59-60` |
| `content` | `0x03` | `chain/x/content/store.go:20,29` |
| `evidence` | `0x03` | `chain/x/evidence/store.go:22,42` |
| `leases` | `0x03` | `chain/x/leases/store.go:23,51` |
| `providers` | `0x02` | `chain/x/providers/store.go:19,40` |

Other raw keys useful for reconciliation: lease record `leases 0x02|lease_id` (every state, never deleted), open offer
`0x01|offer_id`, used-offer marker `0x0C|offer_id` -> scheduler (`chain/x/leases/store.go:18-33`). The devnet reads raw
keys this way (`chain/devnet/upgrade_test.go:772-777`). Raw store queries bypass the query router: no gas meter, no
page cap, so the CometBFT RPC must not be public (`chain/app/app.go:171-172`, `docs/architecture/leases.md` residual
risks, notes-08-10.md goal 07 round 2).

---

## 8. Existing Go client code

### 8.1 `agents/auditor/chainclient` (the only non-test chain client)

| Piece | What it does | Lines |
| --- | --- | --- |
| `NewHTTP`, `HTTPClient` | CometBFT RPC client: http/https base URL only, no proxy, no redirect, per-request timeout, 16 MiB response cap | `rpc.go:200-267` |
| `RPC` interface | `Status`, `ABCIQueryWithOptions`, `BroadcastTxSync`, `Tx`; `*rpchttp.HTTP` implements it, unit tests use a fake (`chaintest`, test-only, `chainclient_test.go:1111`) | `rpc.go:171-180` |
| `Header` | latest height/time/chain id; refuses catching up | `rpc.go:295-309` |
| `query`, `Account`, `HeldLease`, `Content`, `Provider`, `Evidence` | gRPC-routed ABCI queries at latest height | `rpc.go:318-437` |
| `Broadcast` | `broadcast_tx_sync`; returns CheckTx code only (`Ack`) | `rpc.go:439-456` |
| `CommittedTx` | `/tx`; "not found" -> not committed | `rpc.go:458-484` |
| `SVNCode` | svn codespace code -> protocol code name | `rpc.go:486-497` |
| `BuildTx`, `TxMessage` | sign/decode, **typed to `*evidencev1.MsgSubmitAuditBatch`** | `tx.go:20-100` |
| `Submitter` | outbox-like file store per batch: bytes stored before the send, record after (`ACCEPTED`/`NOT_ACCEPTED`/`UNKNOWN`), bytes without record = UNKNOWN; states NOT_SUBMITTED, NOT_ACCEPTED, SUBMITTED, COMMITTED, FAILED, RECORDS_FOUND; reconcile by stored hashes and records read back before any re-sign; REPLAY -> new random business id; bounded 16 submissions and 4 rounds | `submit.go:28-98,158-311,391-660` |

Reusable for goal 11: the RPC hardening, the store-bytes-then-send order, the "ack is never success" state machine, the
reconcile-before-resend loop. Not reusable as is:

- `BuildTx`/`TxMessage` accept only the audit batch message (`tx.go:34,86-100`); a broadcaster needs `sdk.Msg`.
- Chain id comes from the node (`submit.go:536,557`), not from operator configuration; goal 11 asks for explicit chain id
  configuration.
- The next sequence is read from committed state before each broadcast (`submit.go:596-603`), so a second transaction of
  the same signer while one waits gets sdk 32; the auditor handles one batch at a time per key. Concurrent same-signer
  requests need local sequence allocation (2.4).
- `CommittedTx` trusts one node and does not check the returned bytes (`rpc.go:470-484`).
- No deadline fields; uncertainty is bounded by an epoch rule specific to evidence (`submit.go:540-548`).
- Unit tests run against a fake RPC (`chainclient_test.go:217-830`); they are not end-to-end evidence. The real path is
  covered by `TestDevnetAuditLifecycle` (`chain/devnet/audit_test.go:636`, phases 1085-1153).

### 8.2 `chain/devnet` helpers (package `devnet_test`, test files only, not importable)

| Helper | Lines |
| --- | --- |
| `loaded{name, addr, key}`, `loadSecp`, `loadEd`, `loadAccount`, `readHexFile` | `lifecycle_test.go:178-237` |
| `genesisAccountNumbers` (typed auth genesis) | `lifecycle_test.go:239-265` |
| `signDevnet` (gas 2,000,000), `signDevnetWith` (gas, fee), `demo.signMulti` | `lifecycle_test.go:334-364`; `registry_test.go:1068-1101`; `lease_test.go:139-171` |
| `broadcast` (raw JSON `broadcast_tx_commit`), `demo.broadcast` (commit, 30 s), `demo.broadcastSync`, `demo.waitTx` (poll `/tx` 200 ms) | `lifecycle_test.go:366-402`; `registry_test.go:321-337`; `lease_test.go:190-211` |
| `demo.send/commit/reject/notIncluded/rebroadcast/rebroadcastRaw` (sequence bookkeeping in `d.seq`, exact-code asserts, CheckTx-reject not included, mempool cache refusal) | `registry_test.go:339-444`; `lease_test.go:399-438` |
| `demo.abciQuery` (height-checked), `grpcProvider`, `grpcRejection` | `registry_test.go:738-805` |
| `demo.council` (propose by seat-1, approve seats 1-3, execute by authority, refuses a keep outcome) | `registry_test.go:614-649` |
| message builders `registerProvider`, `registerContent`, `offerMsg`, `acceptMsg`, `renewMsg`, `endMsg`, `authorityEndMsg` | `registry_test.go:446-597`; `lease_test.go:56-129` |
| `demo.replay` (in-process replay to the nodes' app hashes), `scanEvents` | `registry_test.go:825,933` |

Provider inner-signing keys (ed25519) and the hosting-intent key are generated inside the test process
(`registry_test.go:240-245,287,446-466`), not in the devnet home.

### 8.3 Shared client package and language

- No shared client package exists. Signing helpers live in the auditor (evidence only) and in test files.
- The broadcaster slot is Python scaffolding that holds no keys and speaks to no node
  (`services/broadcaster/README.md:3`, `services/broadcaster/svn_broadcaster/scaffold.py:1-7`). `requirements.lock` pins
  `protobuf==7.36.2`, `cryptography==50.0.2`, `fastapi==0.162.0` and no Cosmos client and no PostgreSQL driver
  (`requirements.lock:194,255,495`). A Python signer would re-implement SIGN_MODE_DIRECT and secp256k1 signing.
- The blueprint says: "Python services request bounded operations through an operator-authorized Go broadcaster or
  generated client" (`docs/blueprint.md:373`); "The broadcaster records attempt IDs and reconciles unknown submissions
  before resending. Use a stable business ID ..." (`:385`); "A successful broadcast or mempool response is not a
  committed successful transaction ... reconcile after timeouts before retrying" (`:305`); the broadcaster "must not be
  able to manufacture provider acceptance" (`:242`).
- Go workspace modules: `agents/auditor`, `agents/provider`, `chain`, `docs/evidence/goal-02/go`, `proto` (`go.work`).
  The chain module may contain only one `main.go`, `chain/cmd/svnd/main.go` (`tests/unit/test_repo_policy.py:48-50`).
  `github.com/lib/pq v1.12.3` is an indirect dependency of `chain` (`chain/go.mod:129`); no direct PostgreSQL driver is
  pinned in any module.

---

## 9. Devnet harness hooks for the acceptance checks

### 9.1 Initialization (`chain/devnet/init.go`)

- `devnet.Init(Options{Home, ChainID, GenesisTime, Peers, Power, CouncilDelaySeconds})` writes four validator homes and
  one genesis; refuses a non-empty home (`:162-192`). Defaults: random chain id `shortvideo-dev-<16 hex>` (`:195-201`),
  genesis time now (`:202-204`), power 1 each (`:64-75`), council delay 0 (`:169-171,552`).
- Keys: `<home>/accounts/<name>.hex`, lowercase hex of the private key bytes, mode 0600 (`:213-226,583-588`); seats
  `accounts/seat-N.hex` (ed25519, `:228-239`). `topology.json` lists validators (RPC, gRPC, P2P, consensus key) and the
  named accounts with role, address and key type (`:77-111,271-288`).
- Ports, loopback mode: P2P `127.0.0.1:26656+100i`, RPC `127.0.0.1:26657+100i` (exposed as `http://...`), gRPC
  `127.0.0.1:27101+i` (`:377-402`). Compose mode binds `0.0.0.0` (`:378-380`). Each home gets `config.toml`, `node.json`,
  `svn-listen.json` (`:428-467`).
- `svnd devnet init --home --peers --chain-id` prints the topology (`chain/cmd/svnd/cli/root.go:114-149`);
  `svnd start --home` runs one validator (`:75-96`); `svnd status --rpc` (`:151-168`).

### 9.2 Start, stop, restart

| Hook | Behaviour | Lines |
| --- | --- | --- |
| `launch(home)` | re-executes the test binary as a validator (`SVN_HELPER=start`, `TestMain` calls `node.Start`) | `prove_test.go:21-30,455-462` |
| `launchBinary(bin, home)` | runs a built `svnd start --home` | `prove_test.go:464-469`; `svnd_test.go:31-35` builds `svnd` |
| `startLogged` | output to `<home>/node.log` (new file per start); stop = SIGTERM, kill after 20 s; exit 0/143/-1 is clean | `prove_test.go:471-509` |
| `startDevnet(t)` | Init + four `launch` + wait height >= 3; `stops[i]` per validator, cleanup stops all | `lease_test.go:440-473` |
| `upgradeNet.start(i, bin)`, `.stop(label, idx...)` (concurrent stops, timings) | | `upgrade_test.go:187-266` |
| `WaitEqual`, `WaitAbove`, `Stable`, `HighWater`, `Agreed`, `Snapshot`, `ReadStatus`, `BlockHash` | status polling over `/status` and `/block` | `chain/devnet/status.go:19-187` |
| `rpcList`, `tail(layout, err)` (last 2,000 bytes of each node log) | | `prove_test.go:511-532` |
| `lastCommittedHeight` | opens each stopped node's `application.db` for its latest version | `lease_test.go:820-841` |
| `waitMempoolEmpty` | `NumUnconfirmedTxs` == 0 on running nodes | `upgrade_test.go:410-430` |

Node shutdown is safe against the CometBFT v0.40.0 "leveldb: closed" race: the node wraps every CometBFT DB in
`closeSafeDB` (`chain/node/db.go:11-74`; notes-08-10.md 2026-11-02).

### 9.3 Stopping quorum (2 of 4)

- Four validators of equal power; more than two thirds is needed, so one stopped keeps committing and two stopped halt
  (`chain/devnet/prove_test.go:115-150`; vectors `lifecycle_test.go:478-552`).
- `stopForStall` reads the high-water mark and then stops one validator; `observeStall`/`watchStall` sample six times and
  allow at most one block above the threshold, the block in flight at the stop, verified by `stoppedSigned` (signers,
  power, precommit timestamp) (`prove_test.go:222-422`; unit tests `stall_test.go:27-675`). A transaction broadcast just
  before the second stop can still commit in that one block.
- Existing "work while no quorum" patterns: the auditor submit during a v3+v4 stop stays SUBMITTED with CheckTx 0, a
  rebroadcast gets the cache refusal, and after the restart the original hash commits and `status` reports COMMITTED from
  stored bytes alone (`chain/devnet/audit_test.go:1085-1153`). All four stopped across a deadline and restarted:
  `TestDevnetLeaseHaltRestart` (`lease_test.go:861-939`).

### 9.4 Namespace runner, skip flag, CI

- Fixed loopback ports mean concurrent devnets collide; the repository's guidance is to run inside a private network
  namespace with loopback up (`scripts/devnet/walk-runbooks.py:19-22`, `docs/operations/README.md:19`). The orchestrator's
  runner is `<scratchpad>/netns-run.sh` (`unshare --net`, loopback up; no outbound network, Go modules must be cached),
  outside the repository (brief.md section 0).
- Every multi-process devnet test starts with `if os.Getenv("SVN_SKIP_DEVNET") == "1" { t.Skip() }`
  (e.g. `lease_test.go:482-484`). The CI `go` job sets it; the `go-race` shards `devnet`, `devnet-evidence`,
  `devnet-upgrade` run every devnet test under `-race` with 12-15 minute timeouts (`.github/workflows/ci.yml:79-100,
  124-171`); `tests/unit/test_ci_devnet_coverage.py` fails unless the shards select every top-level devnet test exactly
  once (`:1-20`). A new goal 11 devnet test must fit a shard's filter.
- Shell entry points (built `bin/svnd`, `.local/devnet/current`): `scripts/devnet/up.sh` (init + four `nohup` starts,
  pid files), `down.sh` (SIGTERM, 5 s), `resume.sh` (restart from existing homes), `status.sh`
  (`scripts/devnet/up.sh:1-30`, `down.sh:1-40`, `resume.sh:1-28`).
- PostgreSQL/RabbitMQ restart hooks are the goal 04 environment scripts (`scripts/env/start.sh`, `stop.sh` with
  `pg_ctl ... -m fast stop` at `stop.sh:18-22`, `prove-restart.sh`); not chain code.

---

## 10. Devnet accounts and what each may submit

Accounts are generated by `devnet.Init` in this order (`chain/devnet/init.go:119-144`); roles map to submitter rows and
the ante admission (`chain/app/state.go:251-265`, `chain/app/ante.go:139-169`). The submitter set is fixed at genesis:
no message adds a submitter; `RotateServiceSubmitter` and seat rotation are engine-only
(`docs/architecture/authority-authorization.md:16-17`).

| Account | Key | Role | May submit (outer signer) | Refused | Sources |
| --- | --- | --- | --- | --- | --- |
| `authority` | secp256k1 | AUTHORITY | `MsgExecuteChange`; `EndLease` with `authority_end` | anything needing another role (FORBIDDEN) | `chain/x/authority/engine.go:526`; `chain/x/leases/msg_server.go:680-686`; `docs/architecture/leases.md:31` |
| `service`, `service-2` | secp256k1 | ADMITTED_SERVICE | `RegisterContent` (becomes `submitting_host`, needs a devnet hosting intent), `WithdrawContent` (only the submitting host), `OfferLease` (becomes the offer's scheduler) | `AcceptLease` as outer signer FORBIDDEN; a service key as provider acceptance UNAUTHORIZED | `chain/x/content/msg_server.go:348-357`; `chain/x/content/README.md:35-37`; `chain/x/leases/msg_server.go:239`; devnet `lease_test.go:657-659` |
| `provider-1`, `provider-2` | secp256k1 | ADMITTED_PROVIDER | `RegisterProvider`; `UpdateProvider`/`RotateProviderKey`/`DrainProvider` for records whose account is the signer; `AcceptLease`, `RenewLease`, provider `EndLease` for its own provider's offers and leases | another provider's lease FORBIDDEN | `chain/x/providers/msg_server.go:128`; `docs/architecture/registries.md:20-23`; `chain/x/leases/msg_server.go:405-407,427-439,449-453,620-644` |
| `broadcaster` | secp256k1 | BROADCASTER | relay of `AcceptLease`, `RenewLease`, provider-signed `EndLease` for any provider (the provider's inner signature by its current registered key is required) | `RegisterProvider` and the other provider messages, `RegisterContent`, `WithdrawContent`, `OfferLease`, `MsgSubmitAuditBatch`: FORBIDDEN | `chain/x/leases/msg_server.go:405,628-629`; `chain/x/providers/README.md:86-88`; `chain/x/content/README.md:39`; `chain/x/evidence/README.md:218,277` |
| `auditor-1`, `auditor-2` | ed25519 | AUDITOR | `MsgSubmitAuditBatch`; the account key is also the attestation key; evidence and batch ids must start with the auditor's 8-byte address prefix | relay by anyone else | `chain/x/evidence/msg_server.go:203`; `chain/devnet/init.go:123-130`; notes-08-10.md owner rulings goal 09 |
| `seat-1`..`seat-4` | ed25519 | none (admitted from the seat list) | `MsgProposeChange`, `MsgApproveChange` only | every other message: CheckTx FORBIDDEN, sequence unchanged | `chain/app/ante.go:161-180`; `docs/architecture/authority-authorization.md:26`; devnet `lease_test.go:573-585` |

Nobody can send a direct admin message (admit/disable/force-withdraw are council payloads only, DEC-009,
`docs/architecture/registries.md:26`), and x/policy has no message service (`chain/app/app.go:76-78`).

Provider consent boundary on chain: an acceptance needs an `SVN-PROVIDER-ACCEPT-1` inner signature by the provider's
current registered key over the stored offer terms and the lease id (`docs/architecture/leases.md:28`;
`chain/x/leases/README.md:151-155`); a service key cannot produce it (`TestServiceKeyCannotSignAcceptanceInApp`,
`chain/app/lease_misc_test.go:282`; devnet "service-key-acceptance" UNAUTHORIZED, `lease_test.go:657-658`). The acceptance payload names the
scheduler but not the relayer, so the same signed acceptance can be carried by any BROADCASTER or by the provider's own
account (`leases/README.md:153`; business field set `:138`).

Lease setup on the devnet takes, in order: provider registers (provider account), council admit (seat-1 propose, seats
1-3 approve, authority execute), service registers content with a hosting intent, service offers with
`deadline - start = 3600` (DEC-005 devnet test number), then the acceptance (`chain/devnet/lease_test.go:491-505`,
`registry_test.go:614-649`, `chain/x/leases/README.md:219-220`). The hosting intent is not bound to any registered creator
key on chain: "no intent key is registered on chain and DEC-001 key custody is unset" (`chain/x/content/README.md:58-63`).

---

## 11. Constraints this puts on the goal 11 design (summary)

1. CheckTx code 0 means "in one node's mempool", never success (1.3, 4.2). Success = `/tx` code 0 with typed events;
   code 0 with `authority_result{code}` or `<module>_result{idempotent}` needs special handling (4.2).
2. Bytes that passed CheckTx can commit at any later time unless they carry `timeout_height` or `timeout_timestamp`;
   the mempool has no TTL and gossips (2.3, 3.4). Deadlines work in this ante chain but are untested (3.5).
3. Unordered transactions are disabled (3.3); per-signer sequences must be allocated locally and in order; the committed
   account query lags waiting transactions; `/check_tx` mutates check state (2.1, 2.4).
4. After any node start, CheckTx returns sdk 4 for normal accounts until that node commits a block (DR-10-41, 1.4).
5. No memo or fee field can carry an intent id (1.3); events carry no business id (5.2); the broadcaster must persist
   intent id -> business id -> tx hashes itself.
6. Business-id idempotency gives "one business effect" across distinct tx hashes, but every pre-gate check reruns, some
   retries become rejections, rotations must be reconciled by query, and unsigned business ids can be squatted (6).
7. Reconciliation reads: `/tx` by hash, `tx_search` by `tx.acc_seq`, object queries, and raw-store business-ledger keys
   (unmetered; RPC must stay private) (4.3, 7). Offers and ended leases have no gRPC get-by-id (7).
8. One block holds at most `20,000,000 / declared gas` transactions (1.5).
9. A broadcaster key may only relay acceptances, renewals and provider-signed ends; content registration and offers need
   a service key; providers sign their own acceptances (10).
10. No reusable typed client exists; the auditor's chainclient is the template; the blueprint points at a Go broadcaster
    (8.3).

## 12. Not verified here

- Nothing was executed. Every behaviour marked "derived from source" (check-state mutation by `/check_tx`, deadline
  inclusion paths, gRPC tx service, `tx.acc_seq` search, secp256k1 byte determinism, strict decoding of
  `timeout_timestamp`) needs a goal 11 test before it is relied on.
- Mempool gossip ordering and timing between nodes, and what a proposer does with two same-sequence transactions from
  different mempools, are **[Educated Guess]**.
- I did not read the blueprint pages or the decision register beyond the lines cited; signing-boundary approvals for goal
  11 (service vs provider keys, development signing boundary) are for the decisions reader.
