# Lease lifecycle and capacity accounting

Reviewer note for Goal 08. This is not an audit. Public launch is disabled. There is no token, fee denomination, or staking module. `chain/x/leases/README.md` holds the full check orders, store layout and indexes, events, invariants, deviations from the spec machine, and the keeper-level tests. This page covers the app wiring, the cross-module flows, and the app-level and devnet tests.

A lease is a committed storage obligation between a scheduler (an `ADMITTED_SERVICE` account), one provider, and one registered content package. `ACTIVE` and `RENEWED` mean that the obligation is committed and holds a capacity reservation. They do not mean that the package is retrievable, that two providers are independent operators, that a provider is in a region, or that the content is published. Observed reachability (`AT_RISK`) stays off chain; no message, field, or state of x/leases records it (REQ-BOUND-004).

## Wiring

`app.New` builds the keepers in this order:

1. An `x/authority` keeper. Its read surface (`Role`, `GovernedLimit`, `DevnetRole`, `DevnetNumeric`, `PolicyVersion`) is what the registries and x/leases consume.
2. The `x/providers` keeper with that read surface and one shared `msgkit.Peers` set.
3. A copy of the `x/content` keeper without the withdrawal hook. x/leases reads content only through it (`CheckOfferable`, `CheckNotWithdrawn`).
4. The `x/leases` keeper: `Roles`, `Limits`, `Params` (the DEC-005 devnet number), and `Policy` are the authority read keeper; `Providers` is the providers keeper; `Content` is the hook-less content copy; `Evidence` is nil until Goal 09 wires x/evidence, so every renewal that names an `evidence_id` is `ERROR_CODE_NOT_FOUND`.
5. The `x/content` keeper with `Hook` set to the leases keeper. Both content keepers share the content store key. This is the copy given to the content module, the message router, and `authority.WithRegistry`, so a `WithdrawContent` message and a council force-withdraw both void the content's open offers inside the withdrawal transaction.
6. The authority keeper used by the app: `WithRegistry(providers, content).WithPeers(peers)`.
7. `peers` lists `authority`, `content`, `leases`, and `providers`. A business id is one namespace across the four modules (`TestLeaseRetryScenarios` checks lease messages that reuse a content, provider, or authority business id).

`leases.AppModule.RegisterServices` registers `svn.leases.v1.Msg` on the message router and `svn.leases.v1.Query` on the gRPC query router. The closed lease stubs that returned `ERROR_CODE_FORBIDDEN` since Goal 05 are removed; the evidence message stays a closed stub until Goal 09. x/leases has a BeginBlock (`ExpireDue`) and no EndBlock, and returns no validator updates: x/authority stays the only validator-set writer. The BeginBlock order is the genesis order (`app.InitOrder`): leases run after providers and content and before authority.

The leases store holds its layout version (consensus version 2) at key `0x00` from InitGenesis on. Version 1 was the goal-05/06 scaffold slot (the module genesis JSON at key `[0x01]`, which lies inside the offer prefix). `app.New` refuses committed state whose leases store has no layout key or another version, as it does for providers and content (`TestAppRefusesScaffoldLayout`); no migration from version 1 is registered. A goal-07 home must be initialized again. No deployed chain exists.

## Who may write

| Action | Outer signer | Inner signature |
| --- | --- | --- |
| `OfferLease` | `ADMITTED_SERVICE` account (stored as the offer's `scheduler`) | None |
| `AcceptLease` | A `BROADCASTER` account, or the provider's own `ADMITTED_PROVIDER` account | Provider acceptance, domain `SVN-PROVIDER-ACCEPT-1`, by the provider's current registered key, over the stored offer terms and the lease id |
| `RenewLease` | A `BROADCASTER` account, or the provider's own account | Domain `SVN-PROVIDER-RENEW-1`, current provider key |
| `EndLease` (provider) | A `BROADCASTER` account, or the provider's own account | Domain `SVN-PROVIDER-END-1`, current provider key |
| `EndLease` (`authority_end`) | The `AUTHORITY` account | None (a signature is `ERROR_CODE_UNKNOWN_FIELD`); outside DEC-009 |

A service credential never produces provider consent. The service's ed25519 hosting-intent key presented as the provider acceptance, under the provider's key id or its own, is `ERROR_CODE_UNAUTHORIZED`; the service account relaying the provider's valid acceptance is `ERROR_CODE_FORBIDDEN`; a retired provider key is `ERROR_CODE_UNAUTHORIZED`; a signature in another known domain is `ERROR_CODE_WRONG_DOMAIN` (`TestServiceKeyCannotSignAcceptanceInApp`, REQ-CHAIN-012). The sign payloads are interim (decision request DR-08-04); `chain/x/leases/testdata/sign-vectors.json` holds golden vectors.

Offers and renewals need the DEC-005 devnet test number (3600 seconds in the devnet genesis). It is a devnet test value, not a policy: DEC-005 leaves lease durations and allowances unset. The number is read only under a `shortvideo-dev-` chain id. Without it every offer is `ERROR_CODE_DECISION_BLOCKED`; the app's own `OfferLease` handler run on devnet state under a non-devnet chain id is `ERROR_CODE_DECISION_BLOCKED` while the same message under the devnet chain id succeeds; and a non-devnet genesis cannot carry devnet test parameters at all (`TestDevnetTermDecisionBlocked`). REQ-CAP-008 (sponsor and budget) stays BLOCKED on DEC-005.

## Cross-module flows

- Offer: x/leases checks the message, then `content.CheckOfferable` (content `PENDING`, same version, manifest digest, and package bytes, else `NOT_FOUND`, `WITHDRAWN`, `CONFLICT`, `INTEGRITY`), `providers.CheckNewObligation` (`ADMITTED`, else `FORBIDDEN`), the DEC-005 term, the live pair, the provider's open-offer count, and `providers.ReserveProbe` (`CAPACITY`, `OVERFLOW`). An offer reserves nothing.
- Accept: the offer lookup, `providers.VerifyProviderSignature` over the stored offer terms, the relay rule, the lease id, the offer expiry, `content.CheckNotWithdrawn`, `CheckNewObligation`, the live pair, `ReserveProbe`, and the offer deadline (D-3). Then `providers.Reserve` takes the bytes and the lease records the authority policy version at acceptance (`TestLeaseFlowReplayAndQueries` decodes it).
- Renew: the provider's renewal signature, the relay rule, a held lease, content not withdrawn, `term_version` exactly one more, the same bytes and manifest digest, one DEC-005 term past the current deadline, and a deadline still in the future (D-2). The reservation does not change.
- End: a provider-signed end (relayed) or `authority_end`; a lease whose reservation is not held is `ERROR_CODE_UNDERFLOW`. `providers.Release` returns the bytes exactly once.
- Expiry: BeginBlock of every block reads the block header time and closes at most the effective `max_expiry_steps` due items (protocol ceiling 128): held leases by (deadline, id) and open offers by (expiry, id), merged in (time, kind, id) order, leases before offers at equal times. A lease's bytes are released once. An invariant violation (for example a release that would underflow) returns an error from BeginBlock: FinalizeBlock fails and the block is not committed (`TestExpiryInvariantViolationHalts`; fail closed, DR-08-07).
- Withdrawal: `WithdrawContent` or a council force-withdraw calls the hook, which voids every open offer of the content (at most `MaxOpenOffersPerContent` = 256, PROPOSED, DR-08-02) and emits `leases_offer_closed` with reason `WITHDRAWN` before the content's `ContentWithdrawn`. Leases of withdrawn content keep their reservation until they end or expire; their renewal is `ERROR_CODE_WITHDRAWN`. A voided offer cannot be accepted (`NOT_FOUND`) and no new offer can name the content (`WITHDRAWN`).
- Drain and council disable: new offers and acceptances for that provider are `ERROR_CODE_FORBIDDEN`. Existing leases keep their reservations and can still be renewed (the v0 spec rule, decision request DR-08-01, the successor of DR-07-05); the remedy is `authority_end`, one lease per message.
- Repair handoff: a replica on another provider is a new offer and acceptance for the same content id; the pair index is per provider, so it is allowed while the old provider's lease is held, and each provider's reservation is accounted separately. Two leases on two providers are two replicas; the chain does not claim the providers are independent operators.

The withdrawal transaction voids each open offer of the content (at most 256), so its gas depends on that number. `TestWithdrawalAtCapGasInApp` asserts that at 256 open offers both the `WithdrawContent` message and the council `ExecuteChange` commit below 10,000,000 gas, half the transaction gas cap; the client declares a gas limit that covers it (simulation reports the gas a committed transaction of the same bytes uses).

## App-level tests (`chain/app`, in-process FinalizeBlock, real keepers, signed transactions)

| Test | What it asserts |
| --- | --- |
| `TestLeaseMessagesDecode` | The four lease messages decode and pass CheckTx (ante only) and then commit; an unknown field at the top level or inside a Digest or InnerSignature fails decoding with the state unchanged; message types such as `MsgMarkAtRisk` do not exist. |
| `TestLeaseFlowReplayAndQueries` | The lease flow (`runLeaseFlow`: two replicas, a new encoding as its own content id, renewal, provider end, authority end, offer and lease expiry by header time, withdrawal voiding offers, council disable, repair handoff, a governed budget of one) on three nodes with equal app hash and result codes at every height; a fresh application replaying the recorded (height, time, transactions) reaches the same app hash, codes, and gas at every height and the same final `FullSnapshot`; `ActiveLeases` and `Deadlines` page bytes are equal on every node and the replay; decoded leases carry the offer terms, the reservation, and the policy version; effective-1 and effective heights of both expiries on every node. |
| `TestRejectedLeaseMessagesLeaveTheFullState` | Every reject class of the four handlers that a committed transaction can reach, in check order, through FinalizeBlock: exact svn code, the ante handler committed, `FullSnapshot` byte-identical, no lease event; ante rejections (outsider, fee, gas 0, 70 KiB) leave the account store unchanged too. The classes it cannot reach are listed in the test comment. |
| `TestLeaseRetryScenarios` | REQ-INV-003 (lease part): the same business id and payload under distinct tx hashes, in one block in both relay orders and in a later block, gives one `LeaseAccepted`, one `leases_result idempotent=true`, and one reservation; renewal and end retries likewise; another payload is `REPLAY`, and so is a business id of another module. |
| `TestServiceKeyCannotSignAcceptanceInApp` | REQ-CHAIN-012 through FinalizeBlock (see "Who may write"). |
| `TestLeaseEventSchemas` | Every x/leases event of the flow parses (`msgkit.ParseCommitted` for transactions, `msgkit.ParseBlockEvent` for BeginBlock), matches its proto fields and value rules and the forbidden-name patterns, and equals `chain/app/testdata/lease-events.golden.json`. |
| `TestNoPrivateDataInLeaseStateOrEvents` | Canary and URL scans of every store, committed transaction, export, and event after the flow, and forbidden-name scans of the leases export keys and event keys. The flow hashes its canaries before building a transaction, so the canary part checks this client; the chain-side part is the name scan. |
| `TestSimulateEstimatesGas` | Offer, accept, renew, and end: simulated gas equals committed gas (asserted), and simulation changes no committed state. |
| `TestLargeExpiringCatalogue` | 2,048 leases and 256 open offers due at one header time T: the block at T-1 closes none; each block from T closes exactly min(128, remaining) items in (time, kind, id) order, read from the FinalizeBlock events with `mode=BeginBlock`; the backlog drains in ceil(N/128) blocks; during the backlog a renewal of an unswept due lease and an acceptance of an unswept expired offer are `EXPIRED`, and a provider end of an unswept lease releases once; reserved bytes drop by exactly the released bytes and the invariants hold after every block. |
| `TestExpiryInvariantViolationHalts` | A due lease whose provider's reserved bytes were lowered by a test-only writer makes FinalizeBlock fail on two nodes with the same error; the commit does not move. |
| `TestRestartContinuesExpiry` | A goleveldb node closed and reopened mid-backlog (governed budget 2) continues the drain with the same app hash as a node that never stopped. |
| `TestDevnetTermDecisionBlocked` | See "Who may write". |
| `TestWithdrawalAtCapGasInApp` | 256 open offers on one content; the 257th is `BOUNDS` (D-6) with the state unchanged; withdrawal by message and by council each void 256 offers below the gas ceiling. |
| `TestLeasesGenesisRoundTripFromCommittedState` | Module-level export, `ValidateGenesis`, `InitGenesis` into fresh stores, export again, at a height with held leases and at the end of the flow: the imported leases store equals the committed one except exactly the business ids (DR-07-03) and the used-offer markers of the expired and voided offers (DR-08-06). |
| `TestLaunchGenesisRejectsNonEmptyCatalog` | InitChain refuses an export with leases or offers (the launch rule); app-level re-import stays BLOCKED on DR-07-02, DR-07-03, and DR-08-06. |
| `TestAppRefusesScaffoldLayout`, `TestMigrationsNoOpInApp`, `TestRegistryMigrationPathInApp` | The leases store layout check at load, consensus version 2, and the migration path through the app's configurator. |
| `TestLeaseQueryGasAndBounds` | `ActiveLeases` and `Deadlines` page walks (including the lease/offer phase boundary inside a page), rejections with svn codes over gRPC detail and ABCI query, query gas below the default limit, and a node with a lower query gas limit failing the same query. |

The keeper-level ladders, the differential test against `docs/evidence/goal-02/spec_machine.py` (through `tests/difftest/spec_oracle.py`), the fuzzers, and the scale test of `chain/x/leases/difftest` are listed in `chain/x/leases/README.md`. They run real keepers on IAVL stores with a fake authority read surface; they are keeper-level evidence, not end to end.

## Devnet tests (`chain/devnet`, four svnd processes)

Label: single-machine four-process devnet (not independent operators). The four validators run on one machine with loopback peers; the test commands run them inside one network namespace. Command: `go test -count=1 -timeout 1200s ./chain/devnet/ -run 'TestDevnetLease' -v` (through the netns wrapper in this workspace). The devnet genesis has a `broadcaster` account (`BROADCASTER`) since Goal 08; its key is generated at init and stays under the home directory.

`TestDevnetLeaseLifecycle` phases:

1. Register and council-admit p1, p2, and a 10 MB provider p3; register contents in one multi-message transaction.
2. Acknowledgement is not commitment: an acceptance broadcast with `broadcast_tx_sync` passes CheckTx (code 0) and fails in its block with `ERROR_CODE_CAPACITY`; p3's reserved bytes are unchanged.
3. Timeout and retry: the client sends an acceptance, does not wait, and re-signs the same business payload at the next sequence (a distinct tx hash). Both commit with code 0: one `LeaseAccepted`, one `leases_result`; one held lease and one reservation. A changed payload under the business id is `REPLAY`.
4. Node down: v4 stops; the broadcast to v4 fails; the same signed bytes commit once through v1; a rebroadcast is refused (CheckTx sequence error, or the mempool cache); v4 restarts, catches up, and answers the same query bytes as v1 at a common height.
5. Renewal and end retries are idempotent; stale renewal `STALE_VERSION`, other bytes `CONFLICT`, end again with a new business id `UNDERFLOW`, a second replica on the same provider `DUPLICATE_ID`, the service key as acceptance `UNAUTHORIZED`, the service as outer signer `FORBIDDEN`; authority end.
6. Expiry by header time: the last height whose header time is before the deadline holds the lease (effective-1) and the first height at or after it expires it (effective), releasing its bytes once; both heights and header times are printed.
7. Bounded drain: a council sets `max_expiry_steps` to 4; 24 leases and 6 offers share one deadline and expiry; the per-block counts from `/block_results` (`mode=BeginBlock`) are `[4 4 4 4 4 4 4 2]` in (time, kind, id) order, and the block before closes none.
8. Withdrawal voids the open offer of the content and keeps the active lease's reservation; accept of the voided offer `NOT_FOUND`, a new offer `WITHDRAWN`, renewal `WITHDRAWN`; council disable of p2: offer and accept `FORBIDDEN`, renewal allowed, authority end releases; repair handoff of c6 to p1.
9. Query bytes are equal on all four nodes at one height; every block is replayed into an in-process application, which must reproduce the header `app_hash` of the next block on all four nodes, keep the full state unchanged at every committed rejection, and answer the same query bytes.
10. Every committed event is parsed (`ParseCommitted` for transaction events, `ParseBlockEvent` for BeginBlock events) and scanned for forbidden names, canaries, and URLs.

`TestDevnetLeaseHaltRestart` (REQ-HOST-007): a lease's deadline is a real-time obligation in the signed terms. The provider signs a renewal and does not broadcast it. All four validators stop before the deadline and restart after it has passed in real time. The first height whose header time is at or after the deadline expires the lease, the height before still holds it, and the offline renewal submitted after the restart is `ERROR_CODE_CONFLICT`: a halted chain does not turn an offline note into a renewal. The reservation is released once and a replay reproduces every node's header `app_hash`. CometBFT block time after a restart comes from the votes of the previous commit, so the first block after the restart can still carry a header time from before the halt; the test finds the effective height by header time, not by assuming it is the first new block.

Measured numbers (gas, heights, counts, hashes) are cited only from the evidence logs of the committed tree (`docs/evidence/goal-08.md`), not from this page.

## Requirements

| Requirement | Tests |
| --- | --- |
| REQ-BOUND-004 | `TestLeaseStateHasNoAtRisk` (keeper), `TestLeaseMessagesDecode` (no reachability message type) |
| REQ-CHAIN-012 | `TestServiceKeyCannotSignAcceptance` (keeper), `TestServiceKeyCannotSignAcceptanceInApp`, devnet phase 5, differential service-key variant |
| REQ-MODU-004 | `TestAcceptReservesOnceAndBindsTerms` (keeper), `TestLeaseFlowReplayAndQueries` |
| REQ-IDX-001 | `TestActiveLeasesPagination`, `TestDeadlinesPagination`, `TestExpiryOrderAndBudget` (keeper), `TestExpiryWorkIndependentOfBacklog` (difftest), `TestLargeExpiringCatalogue`, `TestLeaseQueryGasAndBounds`, devnet phase 7 |
| REQ-SEP-001 | `TestRenewKeepsCommittedBytes` (keeper), `TestLeaseFlowReplayAndQueries` (a new encoding is a new content id with its own lease; v0, DR-07-04) |
| REQ-TX-003 | Offer and accept ladders (keeper), the differential test, `TestRejectedLeaseMessagesLeaveTheFullState`, `TestLeaseFlowReplayAndQueries` |
| REQ-TX-005 | Renew and end ladders and `TestEndReleasesOnce` (keeper), the differential test, `TestRejectedLeaseMessagesLeaveTheFullState` |
| REQ-INV-001 | `TestDuplicateReplicaRejected` (keeper), differential `DUPLICATE_ID` classes, `TestRejectedLeaseMessagesLeaveTheFullState`, devnet phase 5 |
| REQ-INV-002 | Differential `CAPACITY` classes and `FuzzLeaseTransitions` (I2), `TestRejectedLeaseMessagesLeaveTheFullState`, devnet phase 2 |
| REQ-INV-003 | `TestLeaseRetryScenarios`, devnet phase 3 (lease part; the governance part is Goal 10) |
| REQ-INV-004 | `TestReserveReleaseAccounting` (providers), differential `OVERFLOW` and `UNDERFLOW` classes |
| REQ-HOST-003 | Accept ladder and `TestNoForbiddenNames` (keeper: no region field) |
| REQ-HOST-007 | `TestDevnetLeaseHaltRestart`, `TestRenewPastDeadlineIsExpiredD2` (keeper), the acceptance payload binds the real-time deadline |
| REQ-CAP-008 | BLOCKED on DEC-005 (sponsor and budget unset) |

## Decision requests

Interim choices are reversible and are not owner approval. Details are in `chain/x/leases/README.md`.

- DR-08-01 renewal after drain or council disable (successor of DR-07-05): v0 allows it; the remedy is `authority_end`, one lease per message. A disabled, possibly compromised provider can keep extending its own leases.
- DR-08-02 `MaxOpenOffersPerContent` = 256 (PROPOSED), bounding the withdrawal hook's work. One scheduler can occupy a content's slots for up to the offer lifetime.
- DR-08-03 `MaxOfferLifetimeSeconds` = 86400 (PROPOSED, not a DEC-005 duration). Without it one scheduler could hold a provider's open-offer slots indefinitely.
- DR-08-04 provider ACCEPT, RENEW, and END payloads (field sets, decimal-string integers, lease id bound, no chain id).
- DR-08-05 real-time deadline guards D-2 and D-3 (stricter than v0).
- DR-08-06 offer ids are never reused (D-4); an export drops the closed-offer markers.
- DR-08-07 expiry: 128 per block, BeginBlock placement, (time, kind, id) order, a budget shared with evidence in Goal 09, and halting on an invariant violation.
- DR-08-08 protocol additions: a typed offer-closed event, Lease and Offer by-id queries, a structured Deadlines cursor, an unfiltered ActiveLeases.
- DR-08-09 renewal window: a provider can renew any time while the reservation is held and stack terms.
- DR-08-10 one integer encoding rule for business payloads across modules.

## Handoff

- Goal 09: wire `Deps.Evidence` (`RenewalEvidence`); decide the expiry budget shared with evidence (DR-08-07); use `leases.Keeper.Lease` for the DEC-010 manifest digest comparison. Evidence never extends a lease.
- Goal 11 broadcaster: a broadcast acknowledgement (CheckTx runs only the ante chain) is not success; poll the committed result. Resubmitting the same business id and payload under a new tx hash is idempotent (code 0, `leases_result`); another payload is `REPLAY`; a rejected transaction is in its block with its svn code. Declare enough gas for a withdrawal with many open offers.
- Goal 12 indexer: the typed lease events plus `leases_offer_closed`; `msgkit.ParseCommitted` for transaction events and `msgkit.ParseBlockEvent` for BeginBlock events; reconcile offers through `Deadlines` and leases through `ActiveLeases` and the events.
- Goal 16 provider agent: the sign-bytes builders and `chain/x/leases/testdata/sign-vectors.json`; deadlines are real-time obligations even while the chain commits no blocks; the agent compares deadlines with its own clock and shows the uncertainty.

## Residual risks

- Raw `/store/leases/*` ABCI queries are not metered or paged (the SDK serves them from the IAVL store); the CometBFT RPC port must not be public. Only gRPC-routed queries run on the query gas meter.
- Lease history grows without limit; there is no per-account lease limit while DEC-005 leaves allowances unset. Gas meters every write.
- A scheduler can occupy a content's 256 offer slots or a provider's 1,024 for up to one day.
- A disabled provider can keep renewing its leases (DR-08-01).
- An expiry invariant violation halts the chain (fail closed, DR-08-07).
- An export drops business ids and closed-offer markers; app-level re-import is BLOCKED.
- The devnet runs four processes on one machine; it is not an independent-operator network.
