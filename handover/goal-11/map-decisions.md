# Goal 11 decisions, requirements and blueprint map (decisions reader)

Reading map for planning goal 11 (transaction broadcaster and outbox). It is not an owner decision. Anything inferred is
marked [Educated Guess]. Recommended defaults in section 10 are proposals to put to the owner, not approvals.

Tree read: worktree `scratchpad/wt-11`, branch `goal-11`, HEAD `6d74aca` ("svnd: genesis validate applies InitChain's
consensus app version rule (#15)"), clean (`git status --short` empty). The task text says the branch was created from
`dbf0897`; the orchestrator notes record the fast-forward to `6d74aca` after PR 15 merged (notes-08-10.md:272-277). No
repository file was edited. Every `file:line` below is on that tree unless it names a scratchpad file or the SDK module
cache.

---

## 1. Findings in brief

1. Five matrix rows name goal 11, all with a `TEST:` assignment and no `BLOCKER:`: REQ-CHAIN-013, REQ-ACK-001,
   REQ-INT-001, REQ-IDEM-001, REQ-QA-008 (docs/requirements.md:452, :483, :496, :500, :594).
2. The goal's prerequisite "approved service/provider signing boundaries" (docs/goals/11-transaction-broadcaster-and-outbox.md:8)
   has no approved document behind it. Gate A accepted "the proposed signing domains" (docs/decision-register.md:23), but
   docs/protocol.md is "not an approved protocol" (docs/protocol.md:3), and the chain already departs from the protocol's
   relay column for content, provider and audit messages (docs/architecture/registries.md:30; chain/x/content/README.md:39;
   chain/x/evidence/README.md:578-580). No file defines "the approved development signing boundary" of item 5 (grep for
   "signing boundar", "keyring", "remote signer" finds only docs/progress.md:41 outside goal specs and goal-00 ignore
   fixtures). DR-11-01.
3. No `### Blocks` section of the decision register names goal 11 (grep "Goal 11|broadcaster|outbox" in
   docs/decision-register.md hits only :337). DEC-011's approved text names the outbox: "the outbox records
   `PENDING_STALL`" (docs/decision-register.md:337). Progress row 11 adds "DEC-001 blocks host custody of user keys"
   (docs/progress.md:41).
4. REQ-CHAIN-013's "publication permission bit" is defined nowhere. The phrase appears only in docs/requirements.md:452,
   docs/blueprint.md:242 ("a user's publication permissions") and docs/threat-model.md:75. The chain cannot enforce a
   creator's permission today: the hosting-intent public key is the one carried in the message, and the signature
   "does not prove a creator authorized the host" (chain/x/content/README.md:61). On a devnet with the DEC-001 test
   parameter, any holder of an `ADMITTED_SERVICE` key can sign an intent the chain accepts. Enforcement must be off-chain
   in the broadcaster, and its definition needs the owner. DR-11-04, DR-11-05.
5. What goal 11 asks for needs two chain roles, not one. `RegisterContent`, `WithdrawContent` and `OfferLease` require an
   `ADMITTED_SERVICE` signer (chain/x/content/msg_server.go:350; chain/x/leases/msg_server.go:239). Relaying a
   provider-signed `AcceptLease`, `RenewLease` or provider `EndLease` needs `BROADCASTER` or the provider's own account
   (chain/x/leases/msg_server.go:405, :625-640). A `BROADCASTER` outer signer on content messages is `FORBIDDEN`
   (chain/x/content/README.md:39). Which keys the broadcaster may hold, and for which messages, is not decided. DR-11-02.
6. Goal 09 deferred "Outbox submission: Goal 11" (docs/evidence/goal-09.md:39), but the owner's goal 09 Q1 ruling forbids a
   `BROADCASTER` relay of audit batches (docs/evidence/goal-09.md:74). The deferral cannot be met by a relay. DR-11-06.
7. Business ids are one unsigned namespace across modules. Any admitted account can commit a pending id first and turn the
   legitimate submission into `REPLAY` (chain/x/evidence/README.md:704-713; DR-09-28 docs/evidence/goal-09.md:467; DR-10-16
   docs/evidence/goal-10.md:585). Goal 11's "stable business ID" choice has to live with that. DR-11-08.
8. Provider inner-signature payloads carry no chain id, and the END payload has no expiry (chain/x/leases/README.md:148,
   :155; DR-08-04 at :644-649). Hosting intents carry no chain id either (chain/x/content/README.md:63). An outbox that
   stores them stores reusable authorizations. DR-11-09.
9. "Authorized request context" (goal spec :19) is new off-chain storage that can hold personal data. DEC-004 sets no
   retention period (docs/decision-register.md:127) and DEC-007 keeps a privacy assessment as a launch gate (:217).
   DR-11-10.
10. No client API exists yet. `services/api` refuses to serve (services/api/svn_api/scaffold.py: `serve()` raises), and
    goal 13 is the first API goal (services/api/README.md:5). REQ-INT-001's "used by the API" can only be shown in goal 11
    as a static policy check; the runtime half waits for goals 13 and 21.
11. Every prerequisite goal is BLOCKED in its own status: 04 (docs/progress.md:34), 07 (:37), 08 (:38), 09 (:39), 10
    (:40). Merged is not VERIFIED (brief.md section 0). Content registration and offers work only on a devnet with
    labelled test parameters (chain/x/content/README.md:60; chain/x/leases/README.md:124-125), so every goal 11 devnet
    result is devnet-only.
12. No threat-model item covers confused-deputy use of the broadcaster, log leakage, or business-id squatting by name
    (grep "confused|deputy|log|redact|impersonat|front-run|squat" in docs/threat-model.md finds none of these as items).
    Section 7 maps them onto the nearest items and lists the gaps.

---

## 2. Requirement rows assigned to goal 11

Columns are tab separated in `docs/requirements.md`; text below is copied exactly.

### 2.1 REQ-CHAIN-013 (docs/requirements.md:452)

- Scope / pages / role / goals: `IN_SCOPE` / 8 / `chain/security` / 11.
- TEST: "Goal 11 broadcaster fixture cannot mint a provider signature or skip the publication permission bit."
- Summary: "The broadcaster is not a relayer that can manufacture provider acceptance or bypass publication permissions."
- Source passage: blueprint p. 8 (docs/blueprint.md:242), quoted in section 8.

What exists already (chain level, not through a broadcaster):
- `TestServiceKeyCannotSignAcceptance`: a service key, another provider's key or a retired key as acceptance is
  `UNAUTHORIZED`; a service account as outer signer of an acceptance is `FORBIDDEN` (chain/x/leases/README.md:158-162).
- Devnet phase 5 of `TestDevnetLeaseLifecycle`: "the service key as acceptance `UNAUTHORIZED`, the service as outer signer
  `FORBIDDEN`" (docs/architecture/leases.md:88).
- Hosting intent: devnet-only role `DEVNET_TEST_ONLY`, verified against the key in the message
  (chain/x/content/README.md:47-63).

An honest VERIFIED needs, on the real devnet and through the goal 11 broadcaster:
1. Provider half. The broadcaster process holds no provider key and has no code path that signs in an `SVN-PROVIDER-*`
   domain. Evidence: (a) a unit test that the signer refuses any inner-signature domain (it signs only the outer
   `SIGN_MODE_DIRECT` envelope); (b) a configuration test that a key whose address is an `ADMITTED_PROVIDER`,
   `AUTHORITY`, `AUDITOR` or seat account is refused at start-up (read the role from the chain); (c) relay requests whose
   `provider_acceptance` (or renew/end signature) is signed by the service key, the broadcaster key, another provider's
   key, or a retired provider key are refused by the broadcaster before any broadcast, with a typed reason; (d) the same
   requests forced through a test-only bypass reach the chain and come back `UNAUTHORIZED` in their block with provider
   reserved bytes and the lease set unchanged. (c) without (d) shows only the broadcaster's own check; (d) without (c)
   repeats goal 08.
2. Publication half. Requires the definition asked in DR-11-04. Under the recommended default: a `RegisterContent` or
   `WithdrawContent` request without an authorized creator context, or with a context for another creator's content, is
   refused with no outbox row and no transaction; the broadcaster cannot produce a hosting intent because it never loads
   the intent key (DR-11-05). Tested by a request that carries an intent the broadcaster would have to sign itself, and
   by a check that the intent-key path is absent from its configuration.
3. Logs captured with `scripts/evidence/capture.sh` on the tested commit, `-v` Go output naming the tests, DIRTY `no`.

Leaves it BLOCKED or partial:
- DR-11-04 unanswered: the publication half stays IMPLEMENTED (interim) at best, because "the publication permission bit"
  has no definition to test against.
- Chain-level enforcement of creator permission is impossible on this protocol: production content registration is
  `DECISION_BLOCKED` (chain/x/content/README.md:60) and the intent proves integrity only (:61). That part stays BLOCKED on
  DR-07-01 and DEC-001 (docs/evidence/goal-07.md:244, :595), whatever goal 11 does. A compromised `ADMITTED_SERVICE` key
  can still mint devnet intents (residual risk, not a goal 11 failure).
- DR-11-01 unanswered: the row depends on a boundary that is not approved; record the dependency.

### 2.2 REQ-ACK-001 (docs/requirements.md:483)

- Scope / pages / role / goals: `IN_SCOPE` / 10 / `chain/security` / 11.
- TEST: "Goal 11 keeps a broadcast acknowledgement in the submitted state until inclusion and execution succeed, and
  reconciles a timeout before retry."
- Summary: "A broadcast or mempool acknowledgement is not committed success. Persist the transaction hash, message id, and
  result."
- Source passage: blueprint p. 10 "Commit acknowledgement" (docs/blueprint.md:305).

What exists already: goal 08 devnet phase "ack-vs-commit": an acceptance passes CheckTx with code 0 and fails in its block
with `svn/13` (docs/evidence/goal-08.md:185; docs/architecture/leases.md:85). The auditor's own states
`NOT_SUBMITTED`, `NOT_ACCEPTED`, `SUBMITTED`, `COMMITTED(0)`, `FAILED(code)` (agents/auditor/README.md:97-108).

An honest VERIFIED needs:
1. A devnet transaction that passes CheckTx and fails in its block (for example `CAPACITY`) goes requested, submitted,
   failed with the committed code, and is never shown as committed. The API read between the acknowledgement and the
   block shows submitted.
2. An acknowledgement followed by a quorum stop (two of four validators down) stays submitted or pending for as long as no
   block commits; after restart it becomes committed or failed from the committed result.
3. Timeout before submission (the node never received the bytes) and timeout after submission (the node received and the
   block committed, the client lost the answer): the log shows the reconciliation read (transaction hash, then object
   state) before any resend, and the run ends with one effect.
4. The outbox schema persists, per attempt: transaction hash, message type and index, signer, sequence, height, code,
   codespace, gas, result; per intent: business id and reconciliation state. The schema is documented (goal handoff
   :43).
5. A state-machine unit test (plus a mutant) shows there is no transition to committed except from a committed result read
   from the chain.

Leaves it BLOCKED: a devnet that cannot run here (netns, disk), fault injection replaced by mocks, or a "submitted" state
inferred only from the client's own timer. A mock is unit-test evidence only (AGENTS.md "Implementation discipline").

### 2.3 REQ-INT-001 (docs/requirements.md:496)

- Scope / pages / role / goals: `IN_SCOPE` / 13 / `chain/security` / 11.
- TEST: "Goal 11 is the only chain writer used by the API, and the web client does not subscribe to every block."
- Summary: "Chain access sits behind the broadcaster or generated client. Read models come from committed events and
  periodic queries."
- Source passage: blueprint p. 13 (docs/blueprint.md:373).

Facts: `services/api` is a scaffold that raises (services/api/svn_api/scaffold.py; services/api/README.md:5). `apps/web`
implements no login, playback or uploads (apps/web/README.md:5). The blueprint names "an operator-authorized Go
broadcaster or generated client" (docs/blueprint.md:373); `services/broadcaster` is a Python scaffold
(services/broadcaster/svn_broadcaster/scaffold.py:1-7).

An honest VERIFIED in goal 11 needs a repository policy test that fails on any chain write outside the broadcaster: no
signing key loader, no `broadcast_tx_*` call, no SDK transaction builder in `services/api`, `services/indexer`,
`workers/`, `apps/web`; no CometBFT websocket or `subscribe` use in `apps/web`. The test must be shown to fail on a planted
violation (a mutant), or it proves nothing.

Leaves it partial: the runtime claim "used by the API" cannot be shown while the API does not exist. Honest status:
VERIFIED for the static policy, DEFERRED for the runtime path to goals 13 and 21 (the first API routes and the publish
flow). "Read models come from committed events" is goal 12 (REQ-IDEM-002, docs/requirements.md:501).

### 2.4 REQ-IDEM-001 (docs/requirements.md:500)

- Scope / pages / role / goals: `IN_SCOPE` / 13 / `chain/security` / 11.
- TEST: "Goal 11 retries one business id after a timeout and shows a single committed effect."
- Summary: "A transactional outbox records attempts, reconciles unknown submissions, and uses a stable business id."
- Source passage: blueprint p. 13 "Idempotency across systems" (docs/blueprint.md:385).

What exists already: goal 08 A10 devnet "timeout-retry": two transactions, one `LeaseAccepted`, one `leases_result`, one
held lease (docs/evidence/goal-08.md:193). That test drives the chain directly; there is no outbox.

An honest VERIFIED needs, on the devnet with the PostgreSQL outbox:
1. Application intent and outbox row written in one database transaction (goal spec :20); a crash between them leaves
   neither.
2. Timeout after submission; the retry worker reconciles, then either finds the commit or resends under the same business
   id and gets code 0 with `<module>_result idempotent=true` (docs/architecture/registries.md:46).
3. Assertions read from committed chain data: exactly one typed event for the business effect across all blocks (from
   block results, `msg_index` aware), one held lease, reserved bytes equal to one package; the outbox shows at least two
   attempts for one business id and records the canonical first commitment, not the idempotent replay.
4. Duplicate caller requests with the same idempotency key produce one intent row (unique constraint), tested
   concurrently.

Leaves it BLOCKED: no PostgreSQL here; no devnet; assertions on the broadcaster's own database instead of chain reads.

### 2.5 REQ-QA-008 (docs/requirements.md:594)

- Scope / pages / role / goals: `IN_SCOPE` / 26 / `chain/security` / 11.
- TEST: "Goal 11 repeats one business operation across timeout and restart and shows one transition and no leaked
  reservation."
- Summary: "Idempotency gate: one final transition and no leaked reservation after retries and restarts."
- Source passage: blueprint p. 26 "Idempotency" row (docs/blueprint.md:766).

An honest VERIFIED needs REQ-IDEM-001's evidence plus:
1. Broadcaster killed (SIGKILL) after the block commits and before the outbox records it ("worker death after commit",
   goal spec :35); restart reconciles from persisted transaction bytes or hash, without manual edits (:40).
2. PostgreSQL stopped and restarted mid-flight; same outcome.
3. "No leaked reservation": for every provider touched, `reserved_bytes` from `Query/Provider` equals the sum of
   `package_bytes` of its held leases from `Query/ActiveLeases`, before and after; a failed acceptance (`CAPACITY`) leaves
   reserved bytes unchanged.
4. Label: single machine, four processes, private network namespace (brief.md section 0; docs/architecture/leases.md:80).

Leaves it BLOCKED: as REQ-IDEM-001; also if restart is simulated in-process instead of killing the real process.

### 2.6 Acceptance checks and handoff of the goal spec, mapped

| Item (goal spec line) | Rows | Honest VERIFIED needs | BLOCKED when |
| --- | --- | --- | --- |
| AC1 timeouts before/after submission, worker death after commit, duplicate requests, concurrent same-signer requests; one business effect, no leaked capacity, eventual accurate status (:35-37) | REQ-IDEM-001, REQ-QA-008, REQ-ACK-001 | real devnet, real PostgreSQL, real process kills; chain-read assertions (section 2.4/2.5); concurrent same-signer run shows every intent committed once with no lost sequence | devnet, netns or PostgreSQL unavailable; mocks |
| AC2 reject unauthorized messages and attempted provider impersonation (:38) | REQ-CHAIN-013 | broadcaster refusal before broadcast plus forced chain rejection with unchanged state (section 2.1); unauthorized = a message outside the per-key allowlist of DR-11-02 | DR-11-02 unanswered (no allowlist to test against) |
| AC3 stop quorum; API pending or honest failure, never committed success (:38-39) | REQ-ACK-001 | two of four validators stopped on the devnet; API reads during the stall; resume; final status equals committed chain result | stall test not run on the real devnet |
| AC4 restart PostgreSQL/broadcaster, reconcile from persisted state, no manual history edits (:40) | REQ-QA-008 | section 2.5 items 1-2 | as above |
| H1 failure traces, outbox schema, service API, signing-boundary documentation (:43) | all | committed logs for each injected fault; schema and API docs; boundary doc (DR-11-01) | boundary not approved: IMPLEMENTED only |
| H2 indexer stable identities and canonical commitment evidence (:43-44) | handoff to goal 12 | intent id, business id, attempt id, chain id, tx hash, height, `msg_index`, event identity of the first commitment (goal 12 needs "chain identity, block height and stable transaction/message/event identity", docs/goals/12-committed-state-indexer-and-reconciliation.md:19-21) | none, documentation |

Implementation items I1-I5 (goal spec :18-32) feed these checks; their open choices are section 10.

---

## 3. Rows, evidence and handoffs from other goals that defer to goal 11

| Source | Exact text (cited) | What goal 11 must do |
| --- | --- | --- |
| docs/evidence/goal-08.md:38 | "IMPLEMENTED: the handoff documentation and the evidence for scheduler and broadcaster integration. Goals 11 and 16 are the consumers." | Consume the lease handoff below. |
| docs/evidence/goal-08.md:231 | "Evidence before scheduler (goal 11) and broadcaster integration \| IMPLEMENTED; consumers are goals 11 and 16" | Same. |
| chain/x/leases/README.md:607-618 | "Goal 11 broadcaster: a broadcast acknowledgement (CheckTx runs only the ante chain) is not success; poll the committed result. Resubmitting the same business id and payload under a new transaction hash returns code 0 and `leases_result` (idempotent) only if every check before the business gate still passes ... So the outbox reads the committed result by transaction hash, or the lease state, before it resubmits, and never treats a resubmission's code as the first transaction's result." | Reconcile before resend; never map a resend's `FORBIDDEN` (D-11, D-13), `UNAUTHORIZED` (DR-08-12, D-12), `BOUNDS` or role failure to the first attempt's result. |
| docs/architecture/leases.md:140 | Same handoff, plus "Size the gas of a withdrawal with many open offers by simulation." | Use `Simulate` for gas on `WithdrawContent`. |
| chain/x/leases/README.md:165-168 and :170-173 | END binds the term version because "a pending end can sit in a broadcaster outbox"; exported builders `AcceptSignBytes`, `RenewSignBytes`, `EndSignBytes` "for schedulers, broadcasters, and the provider agent" | Use the exported builders to pre-verify relayed signatures; expect an END queued before a renewal to fail `UNAUTHORIZED` after it. |
| chain/x/providers/README.md:126-128 | "A broadcaster must reconcile a rotation by querying the provider, not by resubmitting it." | Rotation is not a broadcaster message under DR-11-02's default; keep the rule if it ever is. |
| docs/architecture/registries.md:46 | "a broadcaster confirms a rotation by querying the provider." | Same. |
| docs/evidence/goal-09.md:39 | "Outbox submission: Goal 11." | Conflicts with Q1 (no relay). DR-11-06. |
| docs/progress.md:39 (row 09) | "DEFERRED: ... outbox submission (Goal 11)" | Same; the progress row 09 wording must be resolved by the goal 11 evidence. |
| docs/evidence/goal-10.md:813 | "Goal 11 (broadcaster and outbox) builds on this revision once goal 10 is reviewed." | Base on merged goal 10 (main). |
| docs/protocol.md:260 | "The broadcaster, not a chain handler, records `PENDING_STALL` in its outbox. ... That outbox is not a lease and is not in the application hash. A broadcast acknowledgement is not success." | Implement a stall state in the outbox; nothing chain-side. |
| docs/decision-register.md:337 (DEC-011, APPROVED) | "Losing more than one third halts the real network because the environment produces no block and the outbox records `PENDING_STALL`." | Same; this is the only approved decision text that names the outbox. |
| docs/evidence/goal-02/cases.py:64-77 | `broadcaster_submit`: "Broadcaster outbox. Not a chain handler and not in TX_HANDLERS." Parks `PENDING_STALL` with the business id. | Reference model of the stall behaviour. |
| docs/evidence/goal-02/spec_machine.py:2030-2040 | "`pending` is that outbox." `pending` is in `APP_HASH_EXCLUDED`. | The outbox never enters consensus state. |
| services/broadcaster/README.md:3 | "A broadcast acknowledgement is not committed execution. Goal 11 implements the outbox. This package does not hold keys and it does not speak to a node." | README must be rewritten; the last sentence stops being true. |
| tests/unit/test_scaffold.py:24-27; tests/unit/test_repo_policy.py:17 | `test_broadcaster_refuses_to_serve` asserts `svn_broadcaster.SCAFFOLD is True` and "does not submit"; `scaffold.py` is a required path. | Replacing these is a test replacement by design; record it (brief.md section 2: never weaken or delete a test silently). |
| docs/threat-model.md:83 | "Goal 05, Goal 06, Goal 08, and Goal 11 negative tests for forged, unauthorized, replayed, and oversized messages, including a service signature presented as a provider acceptance." | Section 2.1. |
| docs/requirements.md:629-630 (goal 02 checklist) | "A broadcaster must not sign provider acceptance (REQ-CHAIN-013)." "Specify replay protection and idempotency with a stable business identifier (REQ-IDEM-001, REQ-INV-003)." | Same rows. |

No `chain/x/*/README.md` other than leases, providers, content and evidence, no `docs/architecture/*.md` other than
leases.md and registries.md, and no `services/*/README.md` other than broadcaster mention goal 11 or an outbox (grep over
those paths; governance-and-upgrades.md mentions only goal 12).

---

## 4. Adjacent rows goal 11 must respect (assigned elsewhere)

| Row | Goals | Text (exact summary) | Bearing on goal 11 |
| --- | --- | --- | --- |
| REQ-CHAIN-012 (:451) | 08 | "A service submits the creator's authorized hosting intent. The provider signs acceptance. Those signatures are not interchangeable." | The broadcaster pre-check must keep this. |
| REQ-CHAIN-008 (:447) | 06 | "Beta chain writes come only from admitted operator and service accounts." | The broadcaster's accounts must be admitted submitters. |
| REQ-SEC-002 (:543) | 06 | "Service, validator, and governance keys are separate and least-privilege." | No governance or validator key in the broadcaster (DR-11-02). |
| REQ-ID-001 (:529) | 19 | "Operators hold beta chain-submission credentials." | The broadcaster's keys are operator keys, never a viewer's. |
| REQ-ID-004 (:532) | 19 | BLOCKER:DEC-001 "PROPOSED beta custody is a disclosed host-held delegated signing key. It is not approved, and portability claims wait." | Goal 11 must not hold creator keys or claim custody. |
| REQ-TX-002 (:470) | 07 | RegisterContent checks "the user intent". Production intent BLOCKED (docs/evidence/goal-07.md:244). | Publication half of REQ-CHAIN-013. |
| REQ-INV-003 (:477) | 08,10 | "Lease and governance transitions cannot be replayed." | Chain guarantee the outbox relies on. |
| REQ-PEND-001 (:503) | 21 | "The interface separates requested, submitted, committed, and failed. A local database write does not activate a new storage promise." | Goal 11 supplies the states the UI will show. |
| REQ-ARCH-011 (:424) | 18 | "A chain outage pauses new final commitments and does not require existing videos to disappear." | "cannot create final leases in the database" (goal spec :28-29). |
| REQ-OPS-006 (:573) | 18 | "During a stall, approved reads and in-term playback may continue and new final commitments freeze." | Same. |
| REQ-REC-001 (:494) | 10 | "A stall is recovered by an operator procedure. An application does not override finality." | No outbox action marks a row final during a stall. |
| REQ-IDEM-002 (:501) | 12 | "The indexer checkpoint is atomic with its projection. The projection is a cache, not a ledger." | Handoff H2. |
| REQ-SEC-006 (:547) | 12 | forged events and stale projections | Commitment evidence must be canonical (first commitment). |
| REQ-ARCH-009 (:422), REQ-GW-001 (:502) | 20 | Beta read trust in the chosen gateway is disclosed. | DR-11-12. |
| REQ-OFF-002 (:463), REQ-OFF-004 (:465) | 02,19 / 02 | personal data off-chain; "Hashing is not anonymization." | DR-11-10. |
| REQ-SEC-009 (:550) | 23 | untrusted text not interpolated into SQL | Parameterized outbox queries. |
| REQ-SEC-004 (:545), REQ-CHAIN-010 (:449) | 06 | spam, quotas, metered zero fees | Per-caller bounds in the outbox (DR-11-13). |
| REQ-UX-019 (:380) | 16,25,26 | "Goal 26 rotates a service key." | The outbox pins a signer per intent; rotation is goal 26. |
| REQ-REC-002 (:495), REQ-SEC-003 (:544), REQ-QA-009 (:595) | 26 | BLOCKER:DEC-001 signer backup, compromise drill, recovery gate | Out of scope; residual risk for the broadcaster's keys. |
| REQ-GEN-003 (:486) | 05 | "PROPOSED example chain id shortvideo-dev-1 is an example, not an approved network name." | Chain id is configuration, never a constant (goal spec :30-31). |

---

## 5. Decision register entries that bear on goal 11

All twelve decisions are APPROVED at Gate A with their PROPOSED defaults (docs/decision-register.md:13-23). None of their
`### Blocks` sections names goal 11.

| Decision | Approved text that binds goal 11 (cited) | Blocks section | Goal 11 consequence |
| --- | --- | --- | --- |
| DEC-001 custody | "The account host holds a delegated signing key for beta public records, and the product discloses that custody. A web passkey is a login authenticator and is not a Cosmos transaction key. ... Operators, not viewers, hold chain-submission credentials." (:37) | "Goal 02 must not freeze the account host as the owner of hosting-intent signatures. Goal 19 must not claim portability ... Goal 26 backup and signer-recovery drills wait on this decision. Devnet keys stay fresh development-only keys and are not a stand-in for this policy." (:53) | Broadcaster keys are operator submission keys. No creator key in the broadcaster. Intent role `ACCOUNT_HOST_DELEGATE` is `FORBIDDEN` on chain and only `DEVNET_TEST_ONLY` passes, with the devnet flag (docs/protocol.md:140; chain/x/content/README.md:56). Progress row 11: "DEC-001 blocks host custody of user keys" (docs/progress.md:41). |
| DEC-002 operators | "Admission stays permissioned. ... Multiple node identities are not treated as proof of independent operators" (:67) | "The real operator list, any public claim of operator independence ..." (:83) | The broadcaster's operator is one operator. No independence claim. |
| DEC-003 removal | Publication means moderation and hosting conditions in force; removal ends participating commitments (:97) | "The rule text, the appeal authority, the removal promise ... Goal 23" (:113) | If "publication permission" means community publication approval, it is DEC-003 territory (DR-11-04 option b). `WithdrawContent` through the broadcaster is irreversible (a tombstone; docs/protocol.md:234). |
| DEC-004 retention | "No retention duration is set. ... Chain coordination records are not rewritten to imitate erasure." (:127) | "Goal 23 deletion behavior, Goal 26 backup scope, and the privacy assessment required before any public launch. ... must not invent a retention period." (:143) | Outbox retention of request context and reconciliation rows: no period may be invented. Reconciliation fields are retained "and does not treat as a retention period" (docs/protocol.md:272). DR-11-10. |
| DEC-005 sponsorship | "The per-account number and the lease length are unset." (:157) | "Numeric account allowances, lease durations ..." (:173) | `OfferLease` and `RenewLease` are `DECISION_BLOCKED` without the devnet test number (chain/x/leases/README.md:124-125; docs/protocol.md:144). The outbox maps that code to a final failure, not a retry. Queue bounds are not allowances. |
| DEC-006 licence | publish route returns 403 while no licence file is applied (docs/protocol.md:145) | Goal 27 (:203) | Not a goal 11 path unless publish is routed through it. |
| DEC-007 audience | "Accounts are invited. Public sign-up stays off. Public launch stays disabled. ... A privacy assessment ... remain[s an] unmet gate[] for public launch." (:217) | "Public launch, community-rule tone, any age gate ..." (:233) | The outbox is a development store; any personal data in it is covered by no privacy review yet. |
| DEC-008 council | Four seats, three approvals; "Council voting is application governance" (:247) | Production council parameters, real council keys (:263) | Seat keys never go to the broadcaster (also DR-06-06). |
| DEC-009 council-only | "The approved choice is council only." (:281) | Goals 06 and 10 use the payload path (:295) | No single-signature admit/disable/force-withdraw exists to route. |
| DEC-011 liveness | "Losing more than one third halts the real network because the environment produces no block and the outbox records `PENDING_STALL`." (:337) | "Goal 05 and Goal 06 must not put the process failure list in the application hash." (:351) | Approved basis for the stall state. The outbox reads liveness off-chain only. |
| Gate A reading | "SVN-JCS-1, SHA-256, Ed25519, the proposed signing domains, the safety and governed caps, and the shortvideo-dev- prefix are accepted." (:23) | none | Closest thing to an approved signing boundary: domains, not the signer/relay matrix. DR-11-01. |

Requirements-document resolutions that restate these: CR-005 "Host custody is the PROPOSED default under DEC-001 and remains
OPEN ... Viewer login stays separate from chain signing." (docs/requirements.md:163; note the APPROVED status in the
register supersedes "remains OPEN"); CR-008 "New final commitments pause when commits stop. ... Applications do not
override finality" (:187); CR-011 "Operators hold chain-submission credentials. ... Provider signatures are not service
signatures. Recovery of each class sits in DEC-001 and stays open." (:211).

---

## 6. Earlier owner rulings and open decision requests that constrain goal 11

Owner rulings (quoted from the records; the quotes are the owner's option labels):
- DR-06-06 "Seat-only admission (Recommended)", 2026-11-02 (docs/evidence/goal-08.md:235). A seat key "may sign only
  propose and approve" (docs/architecture/registries.md:28); "A council seat key is not a `BROADCASTER`"
  (docs/architecture/leases.md:33). The broadcaster may never hold a seat key.
- Goal 09 Q1 "Account key signs (Recommended)": "The submitting account must be an `AUDITOR`. ... No `BROADCASTER` relay"
  (docs/evidence/goal-09.md:74); chain row "signer BROADCASTER relaying a valid auditor-signed batch (Q1: no relay) |
  `FORBIDDEN`" (chain/x/evidence/README.md:277).
- Goal 10 Q2 "Council plan, fail closed (Recommended)": approved-but-unexecuted proposals become stale at an upgrade
  (notes-08-10.md:192). [Educated Guess] Outbox work pending across an upgrade height should be re-validated after the
  upgrade, not resent blindly; the upgrade halt looks like a stall to the broadcaster.
- Model substitution for goals 07-30: "I acknowledge the model substitution for DR-06-07", then "extend the
  acknowledgement to goals 07 through 30" (brief.md section 5). Goal 11's playbook assignment is GPT-6 Astra, effort HIGH
  (docs/goals/11-transaction-broadcaster-and-outbox.md:6). The evidence must record the model and effort that actually ran
  and quote both sentences.

Open decision requests with interims that the broadcaster must handle (none answered by the owner):

| DR | Interim on chain (cited) | Broadcaster consequence |
| --- | --- | --- |
| DR-06-05 keep outcomes | Authority keep outcomes commit with ABCI 0 and an `authority_result{code}` event (docs/architecture/authority-authorization.md:63, :93) | If authority messages were ever routed, code 0 would not mean success. Default: do not route them (DR-11-02). |
| DR-07-01 hosting intent | Devnet only; integrity, not authorization; no chain id; withdraw intent not bound to the register key (docs/evidence/goal-07.md:595; chain/x/content/README.md:60-63) | Publication half of REQ-CHAIN-013 stays off-chain (DR-11-04, DR-11-05). |
| DR-07-06 key separation | "Should a provider signing key equal to a consensus key, a council key or another provider's key be rejected? Interim: not checked." (docs/evidence/goal-07.md:602) | The broadcaster should refuse its own keys colliding with known role keys at start-up [Educated Guess]. |
| DR-07-08 registration copy | Business digest does not cover the signer; copy by another account `FORBIDDEN` (docs/architecture/registries.md:46) | Same squatting class as DR-11-08. |
| DR-08-04 provider payloads | No chain id; END binds the term version (D-12) (chain/x/leases/README.md:644-649) | DR-11-09. |
| DR-08-08 protocol additions | No Lease/Offer by-id query; `ActiveLeases` needs a content or provider id (chain/x/leases/README.md:400, :664-666) | Reconciliation reads must go through transaction hash plus `ActiveLeases`/`Deadlines`/`Content`/`Provider` (DR-11-12). |
| DR-08-11 / D-11 | `OfferLease` replay by another scheduler is `FORBIDDEN` (chain/x/leases/README.md:672-675) | Pin one service signer per offer intent; a retry under `service-2` fails. |
| DR-08-12 | Renew/end retry after a key rotation is `UNAUTHORIZED` (chain/x/leases/README.md:676-679) | A resend's code is not the first attempt's result. |
| DR-08-13 | One admitted service can keep a provider's offer slots full; "no message that removes a service role" (chain/x/leases/README.md:680-684) | Per-caller offer bounds in the broadcaster; a compromised service key has no on-chain revocation (see DR-11-02). |
| DR-08-14 / D-13 | `AcceptLease` replay by another provider's account `FORBIDDEN`; a `BROADCASTER` replay stays idempotent (chain/x/leases/README.md:685-689) | Relay retries by the broadcaster stay idempotent. |
| DR-09-28 / DR-10-16 squatting | Business ids unsigned and global; auditor draws retry ids from `crypto/rand` (docs/evidence/goal-09.md:467; agents/auditor/README.md:93; docs/evidence/goal-10.md:585, :633) | DR-11-08. |

Service-key rotation: `RotateServiceSubmitter` is engine-only (docs/architecture/authority-authorization.md:18). A content
record keeps `submitting_host` and an offer keeps its scheduler, so work started under an old service key cannot be
continued by the new one [Educated Guess from the D-11 rule and `submitting_host` ownership in
chain/x/content/README.md:37]. The governance spec adds seat-key replacement, not submitter removal (grep of
docs/architecture/governance-and-upgrades.md finds no submitter removal payload). [Educated Guess] So a compromised
`ADMITTED_SERVICE` or `BROADCASTER` key cannot be revoked by any committed transaction in the current code; only an
engine-level change does it.

---

## 7. Threat-model items, by the categories asked

| Category | Item and cited text | Gap or note |
| --- | --- | --- |
| Service-key misuse | TM-KEYS: keys are distinct, "a signature from one class must not be accepted as another" (docs/threat-model.md:93); mitigation "Rotate service keys. Least privilege for the broadcaster: it cannot manufacture provider acceptance." (:97); residual "A stolen key is authentic until a revocation commits." (:105). TM-UNAUTH-TX residual: "An admitted key can still submit a transaction the rules allow." (:87). Actor: "Broadcaster service. Submits admitted transactions and cannot sign a provider acceptance." (:24). Assets include "service broadcaster keys" (:11). | No on-chain revocation of a service key exists (section 6). Goal 26 owns rotation drills (:101). |
| Provider impersonation | TM-UNAUTH-TX: "The broadcaster submits a provider acceptance the provider did not sign" (:75); "Keep provider acceptance signatures distinct from service signatures." (:79). | Covered; goal 11 adds the broadcaster pre-check. |
| Replay | TM-UNAUTH-TX: "replays a transition" (:75); "Use a stable business identifier so a retry with a new transaction hash cannot duplicate a successful action." (:79). Protocol: "Replay uses two layers." (docs/protocol.md:185). | Cross-chain replay of inner signatures (no chain id) is not named in the threat model (DR-11-09). |
| Double submission | Same TM-UNAUTH-TX mitigation (:79); TM-STALE-GW "Keep submitted and committed distinct." (:169). | Business-id squatting (a third party takes the id first) is not a threat-model item; it is recorded only in module READMEs and DRs (section 6). |
| Confused deputy | No item. Nearest: TM-UNAUTH-TX "bypasses the creator's publication permission" (:75) and "object authorization" (:79). | Gap. The broadcaster signs with its own role on behalf of callers. Without object permissions a caller could make it register, withdraw (irreversible tombstone) or offer for content it does not own, or flood a provider's offer slots (DR-08-13). DR-11-11. |
| Log leakage | No item. TM-SECRET covers a secret committed to git (:309-321); TM-PERSONAL covers personal data on chain (:253-267). | Gap. Goal spec requires "redact logs" (:30). Default in DR-11-10/DR-11-11: no key bytes, bearer tokens, raw request context or unbroadcast inner signatures in logs; canary-based log test. |
| Stall honesty | TM-STALL: "Show requested, submitted, committed, and failed as distinct states. Queue chain requests with an explicit expiry and show them as pending. Do not activate a new storage promise from a database write alone." (:151); test names goals 05 and 18 (:155). | Goal 11 provides the states. |
| Personal data | TM-PERSONAL: "Treat an opaque id or a hash as still linkable." (:259); "Retention of the off-chain copies is DEC-004 and is unset." (:267). | DR-11-10. |
| SQL injection | TM-INJECTION: "Do not interpolate it into shell commands, SQL, or model prompts." (:277). | Parameterized outbox queries; a test with SQL metacharacters in request text fields. |

Trust boundaries: "A broadcast acknowledgement is not committed execution. A provider signature is not a service
signature. ... A database projection is not the ledger." (docs/threat-model.md:43; also docs/protocol.md:55).

---

## 8. Blueprint passages (docs/blueprint.md, page footers at :250, :307, :397, :779, :825)

Page 8, "Resource control without a tradable token" (docs/blueprint.md:240, :242):

> For beta chain writes, accept only approved operator and service accounts. Require signatures, account sequences,
> explicit message authorization, bounded transaction size, execution gas metering and deterministic write quotas. Zero
> monetary fees do not mean unlimited computation. The custom admission/ante path must enforce the same
> consensus-critical rules during execution, not only in each node's mempool.

> Viewers authenticate with the application. A service submits a creator's authorized hosting intent; providers sign
> their own acceptance. A transaction broadcaster is not an IBC relayer. It must not be able to manufacture provider
> acceptance or bypass a user's publication permissions.

Page 10, "Commit acknowledgement" (docs/blueprint.md:305), and the transaction rule (:285):

> A successful broadcast or mempool response is not a committed successful transaction. The broadcaster waits for
> inclusion and execution success, then updates the application projection. Persist the transaction hash, message ID and
> result; reconcile after timeouts before retrying the business action.

> Every state-changing message needs a named signer, authorization rule, replay protection, input bounds and
> deterministic state transition. Treat all transaction data as hostile.

Page 13, "Connecting the blockchain to the app" (docs/blueprint.md:373, :385, :387, :391, :395):

> Keep chain access behind a well-defined adapter. Python services request bounded operations through an
> operator-authorized Go broadcaster or generated client. Read models come from committed events plus periodic queries.
> The web client does not need to track every block.

> Use a transaction outbox in PostgreSQL for intended work. The broadcaster records attempt IDs and reconciles unknown
> submissions before resending. Use a stable business ID so one successful action cannot be duplicated by a retry with a
> new transaction hash. Providers enforce the same offer/lease idempotency locally.

> The indexer persists a last-processed block height and event identity in the same database transaction as its
> projection updates. ... The database projection is a cache of coordination state, not a substitute ledger.

> Allow users and operators to configure more than one service endpoint. At beta, disclose that the chosen gateway is
> trusted for read responses. ... comparing two unauthenticated HTTP responses is not an equivalent security guarantee.

> The UI separates requested, submitted, committed and failed operations. If the chain stops, queue requests with
> explicit expiry and show them as pending. Do not activate a new storage promise based only on a local database write.
> Existing approved playback may continue from reachable hosts under the last accepted terms.

Page 26, test table and layers (docs/blueprint.md:759, :760, :766, :773):

> | Consensus correctness | Equal application hashes after deterministic replay; reject forged, unauthorized and
> replayed messages. |
> | Quorum behaviour | Four equal validators: stop one and sustain commits; stop two and show stalled writes without
> false success. |
> | Idempotency | Repeat the same business operation, timeout and restart components: one final transition and no leaked
> reservation. |

> Use unit and property tests for state machines, fuzzing for parsers/proofs, protocol contract tests across languages,
> integration tests with real stores and queues, end-to-end browser tests, load tests and controlled fault injection.

Page 27, repository and contracts (docs/blueprint.md:792, :817):

> services/broadcaster/      # authorized transaction sender

> Reuse the SDK transaction-signing envelope rather than inventing signature serialization for chain messages.

Reading notes. "Operator-authorized Go broadcaster" (:373) is a blueprint suggestion; the scaffold is Python
(services/broadcaster/svn_broadcaster/scaffold.py). The language is a reversible choice (section 11). "The broadcaster ...
then updates the application projection" (:305) overlaps goal 12's indexer; [Educated Guess] the broadcaster updates only
its own outbox state, and lease/content projections stay goal 12's, so "cannot create final leases in the database"
(goal spec :28-29) holds by construction.

---

## 9. Progress row 11 and the prerequisite check

Row: "| 11 | [Transaction broadcaster and outbox](goals/11-transaction-broadcaster-and-outbox.md) | NOT STARTED | | Goals 04,
07-10. Signing boundaries from Goal 02. DEC-001 blocks host custody of user keys. |" (docs/progress.md:41).

| Prerequisite (goal spec :8; progress :41) | State on this tree | Effect |
| --- | --- | --- |
| Goal 04 dev environment | BLOCKED (docs/progress.md:34). Local PostgreSQL 18.6 and RabbitMQ 4.4.0 checks VERIFIED (docs/evidence/goal-04.md:48); Compose, full stack, network fault, load BLOCKED. This container has PostgreSQL binaries 16 and 18 (`/usr/lib/postgresql/{16,18}/bin`) and `scripts/env/start.sh` (brief.md section 4). | PostgreSQL outbox tests can run locally; record the version used. |
| Goals 07, 08, 09, 10 | BLOCKED each (docs/progress.md:37-40), merged into main. | Goal 11 inherits devnet-only results and the open DRs of section 6. |
| "Signing boundaries from Goal 02" | Goal 02 is IMPLEMENTED, not VERIFIED (docs/progress.md:32). docs/protocol.md:183: "A broadcaster may relay a transaction it did not author. It cannot manufacture the inner signature. Relaying a transaction does not make the broadcaster the provider." The relay column (:227-240) is a candidate the chain departs from (section 1 item 2). | DR-11-01. |
| "approved service/provider signing boundaries" | No approved document. Gate A accepted the signing domains only (docs/decision-register.md:23). | Goal-level BLOCKED until the owner approves a boundary, unless DR-11-01 is answered otherwise. |
| "DEC-001 blocks host custody of user keys" | DEC-001 default approved; on-chain only `DEVNET_TEST_ONLY` intents pass (docs/protocol.md:140). | Production content registration stays `DECISION_BLOCKED`. Goal 11 holds no creator key. |

Disk is about 1.8 GB free on `/` at the time of reading (`df -h /tmp`); brief.md section 0 warns against repository copies
and fresh caches.

---

## 10. Security-critical ambiguities to put to the owner BEFORE implementation

Each item: the question, why it is security-critical, the evidence, options, and a recommended default. The defaults
are proposals. Numbers in them are PROPOSED and stay configurable and labelled unset, as DEC-005 numbers do.

### DR-11-01 Which signing boundary is "approved" for goal 11?

- Why: the goal's prerequisite (spec :8) and item 5 ("the approved development signing boundary", :30) assume an approved
  boundary. Without one, REQ-CHAIN-013 tests a boundary nobody approved.
- Evidence: section 9; docs/protocol.md:3; docs/decision-register.md:23; relay-column deviations
  (docs/architecture/registries.md:30; chain/x/content/README.md:39; chain/x/evidence/README.md:578-580).
- Options: (a) goal 11 writes a boundary document (signer matrix as the code enforces it, the broadcaster's keys and
  per-key message allowlist, key storage, what the broadcaster never signs) and the owner approves it as pinned by commit
  and sha256 at PR time, the way DR-10-00 was approved for goal 10 (docs/progress.md:40, "Approve as pinned"); (b) the
  owner approves the code-level matrix in the module READMEs now; (c) goal 11 runs with the prerequisite BLOCKED.
- Recommended default: (a). Until approved, the document is labelled PROPOSED and the goal status stays BLOCKED on this
  prerequisite only.

### DR-11-02 Which keys may the broadcaster hold, and for which messages?

- Why: whoever holds a key can send every message its role allows. A broadcaster with the `AUTHORITY` key could execute any
  `QUEUED` proposal at a time it picks and end any lease with one signature (`authority_end`, "outside DEC-009",
  docs/protocol.md:239; chain/x/leases/msg_server.go:681). Seat keys would collapse DEC-008 and break DR-06-06. An auditor
  key breaks Q1. A provider key breaks REQ-CHAIN-012. A compromised service or broadcaster key has no on-chain revocation
  (section 6), so least privilege is the main control.
- Evidence: section 1 item 5; docs/architecture/leases.md:28-30; docs/architecture/registries.md:24-28.
- Options: (a) `BROADCASTER` only (relay of provider-signed accept, renew, end); content and offers then need another
  sender; (b) `BROADCASTER` plus one `ADMITTED_SERVICE` key, each with its own allowlist; (c) (b) plus `AUTHORITY`;
  (d) more.
- Recommended default: (b), devnet only. Allowlist: the `ADMITTED_SERVICE` key signs only `MsgRegisterContent`,
  `MsgWithdrawContent`, `MsgOfferLease`; the `BROADCASTER` key signs only `MsgAcceptLease`, `MsgRenewLease`, and
  `MsgEndLease` with `authority_end` false. One message per transaction. Anything else is refused before signing, and a
  test-only bypass shows the chain refuses it too. Never `AUTHORITY`, seat, `AUDITOR` or provider keys. The signer for an
  intent is fixed when the intent is created (D-11). [Educated Guess] One signer process with two key files is enough for
  the devnet; production custody stays BLOCKED (DEC-001, DEC-002).

### DR-11-03 What is "the approved provider-side submission path"?

- Why: goal spec :21-23 and goal 16 (docs/goals/16-storage-provider-agent.md:22-23, "Submit or forward acceptance through
  the approved provider-signing path") both name a path nobody approved. The choice decides whether the broadcaster can
  censor or alter provider consent.
- Evidence: candidate provider API `POST /offers/{offer_id}/acceptance` returns "provider signature over the offer; not
  yet a chain commit" (docs/protocol/api/provider-api.json:10); the provider signs the lease id and offer terms
  (chain/x/leases/README.md:153); the chain accepts a `BROADCASTER` relay or the provider's own account
  (chain/x/leases/msg_server.go:405).
- Options: P1 the provider returns an inner signature and the broadcaster relays it as `BROADCASTER`; P2 the provider
  submits its own transaction from its `ADMITTED_PROVIDER` account and the broadcaster only reconciles; P3 the broadcaster
  forwards provider-signed transaction bytes verbatim (outer signature by the provider's account).
- Recommended default: P1 and P3 in goal 11; P2 is the provider's fallback in goal 16. The broadcaster verifies a relayed
  signature against the provider's current key from `Query/Provider` and the stored offer before enqueueing, never edits
  a forwarded transaction, and refuses an acceptance after the offer's expiry. Residual: a broadcaster can withhold a
  signed acceptance until the offer expires; P2 is the remedy.

### DR-11-04 What is "the publication permission bit" and where is it enforced?

- Why: REQ-CHAIN-013's TEST names it; nothing defines it (section 1 item 4). The chain cannot enforce creator permission.
- Options: (a) it is the creator's hosting intent for `RegisterContent`/`WithdrawContent`: the broadcaster accepts these
  only with an authorized creator context from the caller and an intent signed by a key it does not hold; (b) it is the
  off-chain community publication approval (`PublicationStatus`, docs/protocol.md:195; DEC-003), and the broadcaster
  refuses `OfferLease` for unapproved content; (c) both.
- Recommended default: (a) for content messages, and for `OfferLease` a rule that the content was registered from an
  authorized intent in this outbox and is not withdrawn. Publication approval (b) stays with goals 21 and 23, because
  docs/protocol.md:193-195 keeps publication separate from chain content. Chain-level enforcement stays BLOCKED
  (DR-07-01, DEC-001). [Educated Guess] that (a) is what the blueprint's "authorized hosting intent" (docs/blueprint.md:242)
  means.

### DR-11-05 Who holds the devnet hosting-intent key in goal 11?

- Why: if the broadcaster holds it, it can mint the intents REQ-CHAIN-013 says it must not bypass.
- Evidence: chain/x/content/README.md:49-61; docs/protocol.md:140; DEC-001 Blocks (docs/decision-register.md:53: "Devnet
  keys stay fresh development-only keys and are not a stand-in for this policy").
- Recommended default: a separate devnet fixture signer that stands in for the account host, key generated at run time,
  role `DEVNET_TEST_ONLY`, never readable by the broadcaster process; labelled "not DEC-001 custody". Non-devnet chains
  stay `DECISION_BLOCKED`.

### DR-11-06 Audit batches and goal 09's "Outbox submission: Goal 11"

- Why: closing a deferral by giving the broadcaster an `AUDITOR` key would undo Q1; leaving it open leaves a goal 09
  DEFERRED item without an owner.
- Evidence: docs/evidence/goal-09.md:39, :74; chain/x/evidence/README.md:277; agents/auditor/README.md:16-17, :97-114 (the
  auditor already stores transaction bytes before broadcast and reconciles by hash).
- Recommended default: the broadcaster never submits `SubmitAuditBatch` and holds no `AUDITOR` key. Goal 11 records the
  goal 09 deferral as answered by Q1: the auditor keeps its own submit and status path. Optionally the auditor may later
  reuse the outbox library inside its own process with its own key (DEFERRED). Needs the owner because it closes a
  recorded deferral.

### DR-11-07 When is an operation committed, failed, or uncertain?

- Why: false success breaks REQ-ACK-001 and AC3; false failure invites a duplicate business action.
- Evidence: docs/protocol.md:217 ("requested, submitted, committed, failed, and pending while the chain is stalled");
  agents/auditor/README.md:105 (a rebroadcast refused by CheckTx with `sdk/32` while an earlier submission waits stays
  `SUBMITTED`); DR-06-05 (code 0 is not always success for authority messages). The ante chain runs
  `authante.NewTxTimeoutHeightDecorator()` (chain/app/ante.go:32); in SDK v0.55.0 it rejects a transaction when the block
  height exceeds `timeout_height` or the block time passes `timeout_timestamp`
  (`$GOMODCACHE/github.com/cosmos/cosmos-sdk@v0.55.0/x/auth/ante/basic.go:205-227`).
- Recommended default: committed = a committed transaction with code 0 whose typed event or idempotent result is read
  back (for an idempotent result, the canonical evidence is the earlier first commitment); failed = a committed non-zero
  code, or proof the transaction can no longer commit (a committed block above its `timeout_height` with no inclusion);
  uncertain = no committed result visible and the transaction may still commit; pending-stall = no new block within a
  PROPOSED window. Never mark failed from a wall clock alone. Each broadcast carries a `timeout_height` so uncertainty
  resolves. [Educated Guess] `timeout_height` needs no protocol decision: docs/protocol.md:187 constrains only the fee and
  gas fields, and the ante does not reject it (chain/app/ante.go:59-85). Authority messages are not routed (DR-11-02).
  Failed is terminal only when the chain proves it.

### DR-11-08 How are business ids generated, and what does REPLAY on a retry mean?

- Why: the "stable business ID" (docs/blueprint.md:385) must survive retries, but a predictable id can be taken first by
  any admitted account (section 6), and a resend's `REPLAY`, `DUPLICATE_ID` or `FORBIDDEN` can mean "someone else did it"
  or "we did it earlier".
- Evidence: chain/x/evidence/README.md:704-713; agents/auditor/README.md:93; chain/x/leases/README.md:105-116, :607-618;
  business payloads omit the signer and the tx hash (docs/protocol.md:185; chain/x/leases/README.md:135-140).
- Options: ids derived from the intent id (stable, predictable); random per business attempt, persisted before the first
  broadcast (stable for that attempt, unpredictable); signer-bound ids (protocol change, DR-09-28).
- Recommended default: random per business attempt (a prefix plus 128 bits or more from a CSPRNG), written in the same
  database transaction as the intent and reused for every resend of that attempt. On `REPLAY`, `DUPLICATE_ID` or a role
  failure after a resend, reconcile by transaction hash and object state (content `submitting_host` and digest, the lease
  for the offer) before deciding; a new attempt id only after reconciliation shows no effect. Residual: a broadcast
  already in the mempool can still be front-run (DR-09-28, DR-10-16).

### DR-11-09 Stored inner signatures, chain-id binding and local expiry

- Why: provider ACCEPT/RENEW/END payloads and hosting intents have no chain id (chain/x/leases/README.md:148;
  chain/x/content/README.md:63), and END has no expiry (:155). A leaked outbox row can be submitted by any `BROADCASTER`
  or by the provider's account (chain/x/leases/msg_server.go:625-640), on this chain or on another devnet with the same
  keys and ids. Only a renewal (D-12) or a key rotation (DR-08-12) revokes an END.
- Recommended default: every intent is bound to the configured chain id; the broadcaster refuses to start when the node's
  chain id differs, and refuses to relay an intent created under another chain id. Unbroadcast inner signatures are
  sensitive data: never logged, access-restricted. RENEW/END relay requests get a local expiry (PROPOSED number), with
  the documented residual that local expiry does not revoke anything on chain. ACCEPT relays stop at the offer's expiry.

### DR-11-10 What may "authorized request context" contain, and how long is it kept?

- Why: it is personal-data storage outside any privacy review (DEC-007) with no retention rule (DEC-004).
- Evidence: docs/decision-register.md:127, :143, :217; docs/protocol.md:272; docs/threat-model.md:259, :267.
- Recommended default: store only the caller service id, an opaque principal id and kind (development fixture), object
  ids, the permission rule id and policy version, a SHA-256 of the typed request, and creation time. Never an email, IP
  address, user agent, session or bearer token, cookie, raw header or free text. No retention period is invented: goal 11
  deletes nothing automatically and labels retention unset (DEC-004). Opaque ids and digests are still linkable
  (REQ-OFF-004). Database dumps never enter Git or logs (AGENTS.md "Cloud and credentials").

### DR-11-11 How are internal endpoints authenticated, and what are the object permissions?

- Why: the broadcaster acts with its own chain authority for callers (confused deputy, section 7). `WithdrawContent` is an
  irreversible tombstone (docs/protocol.md:234). Offers fill shared provider slots (DR-08-13).
- Evidence: goal spec :27-28; no API principals exist before goal 13 ("Development principals must be explicit fixtures
  that cannot enable anonymous production access", docs/goals/13-authenticated-uploads-and-quarantine.md:19).
- Recommended default: loopback or Unix-socket listeners only; per-caller credentials generated at run time, stored mode
  0600 outside Git, compared in constant time, never logged; a per-caller allowlist of request kinds; a caller reads and
  acts only on intents it created and on objects its principal owns; a provider relay is accepted only when its signature
  verifies against that provider's current key and names that provider's offer or lease; development principals refuse
  to start on a chain id outside the `shortvideo-dev-` prefix (docs/protocol.md:136); per-caller bounds on open intents
  and open offers. [Educated Guess] mTLS waits for goal 27 packaging. Tests: cross-caller denial, withdraw of another
  creator's content denied, unauthenticated request denied, all with no outbox row and no transaction.

### DR-11-12 Which node is trusted for commitment evidence?

- Why: a lying or stale node can make the broadcaster report committed. Raw `/store` queries are unmetered and unpaged
  (chain/x/leases/README.md:416, :692).
- Evidence: blueprint p. 13 read-trust disclosure (docs/blueprint.md:391); REQ-ARCH-009, REQ-GW-001; the auditor "Reads
  trust the one node they ask" (agents/auditor/README.md:114).
- Recommended default: one configured node per chain id, disclosed as trusted for reads; no light-client claim; a second
  node, if configured, is labelled a comparison and not a proof; reconciliation uses transaction lookup by hash and
  gRPC-routed queries, never raw `/store`; node RPC stays on loopback.

### DR-11-13 Bounds that need numbers

- Why: "bounded retries, backoff, transaction deadlines" (goal spec :24) and "bounded expiring work" (:28) need numbers;
  none is approved, and inventing them as policy is forbidden (brief.md section 1, unset numbers).
- Recommended default: maximum attempts per business attempt, backoff base and cap, `timeout_height` distance, outbox
  expiry, stall window, open intents per caller and per signer, open offers per caller: PROPOSED constants in one
  configuration file, each labelled PROPOSED, each with an upper and a lower bound, none presented as a service level.
  The owner is asked to accept the labels, not the values.

---

## 11. Reversible implementation choices (record; not owner questions)

- Language and process split. Blueprint suggests a Go broadcaster (docs/blueprint.md:373); the scaffold is Python
  (services/broadcaster/svn_broadcaster/scaffold.py). [Educated Guess] A Go signer reuses the chain's encoding,
  `msgkit` codes and the auditor's chain client (agents/auditor/chainclient/rpc.go:367-384 reads a committed result by
  hash). Python services never load keys either way.
- Dependencies. `requirements.lock` pins no PostgreSQL driver (grep finds `fastapi==0.162.0`, `pydantic==2.14.2`, no
  psycopg/asyncpg/SQLAlchemy); the Go chain module has `github.com/lib/pq v1.12.3 // indirect` only (chain/go.mod:129).
  Any addition goes through the lock and SBOM procedure (brief.md section 4).
- One business intent per transaction; no batching of unrelated intents (a multi-message transaction is atomic).
- A single active sender per signer, enforced in PostgreSQL (row lock or advisory lock), for sequence allocation.
- Store the signed transaction bytes before broadcasting (agents/auditor/README.md:112 precedent), so a crash between
  broadcast and record is reconcilable.
- Outbox state names beyond docs/protocol.md:217; API shape; schema names.
- PostgreSQL version actually used (goal 04 pinned 18.6; 16 is also installed): record it.
- Replacing `test_broadcaster_refuses_to_serve` and the `scaffold.py` required path: record as replaced by design.

---

## 12. Status forecast and evidence obligations

[Educated Guess] Honest goal 11 status: BLOCKED, for (i) DR-11-01 unless the owner approves a pinned boundary,
(ii) production paths `DECISION_BLOCKED` on DEC-001 and DEC-005 values, (iii) prerequisite goals BLOCKED in their own
status. Rows that can be VERIFIED on the devnet (single machine, four processes, private namespace) with real PostgreSQL
and real fault injection: REQ-ACK-001, REQ-IDEM-001, REQ-QA-008, and the provider half of REQ-CHAIN-013. The publication
half of REQ-CHAIN-013 is IMPLEMENTED (interim) until DR-11-04 is answered, and its chain-level part is BLOCKED on DR-07-01.
REQ-INT-001: VERIFIED for the static policy, DEFERRED for the runtime API path (goals 13, 21).

Evidence must record: tested commit and tree; captured logs with COMMAND, HEAD, TREE, DIRTY, EXIT (brief.md section 3);
the model that ran, the effort setting, the playbook assignment (GPT-6 Astra, HIGH; docs/goals/11-...md:6) and both
owner sentences of the 2026-11-01 ruling; the PostgreSQL version; a single-machine label on every devnet claim; the
DR-11-xx answers as quoted owner text, or as open interims if unanswered.

---

## 13. What was read and how

Read in full or in the cited ranges: AGENTS.md (injected); brief.md; notes-08-10.md; docs/goals/11, 12 (:10-40), 13
(:15-32), 16 (:15-30), 21 (:16-26); docs/requirements.md (matrix rows by awk on the goals column, plus :1-60, :150-215,
:600-643); docs/decision-register.md (whole); docs/threat-model.md (:1-52, :71-124, :143-196, :251-325); docs/progress.md
(:1-42); docs/blueprint.md pp. 8, 10, 13, 26, 27; docs/protocol.md (:1-29, :42-152, :179-300, :381-387);
docs/protocol/api/{client,provider}-api.json; docs/evidence/goal-02/spec_machine.py (:255-282, :1590-1625, :2028-2042) and
cases.py (:60-82, :250-275); module READMEs of leases, content, providers, evidence, authority; docs/architecture
leases.md, registries.md, authority-authorization.md; goal-04, -07, -08, -09, -10 evidence (grep hits); agents/auditor
README; services/* READMEs and scaffolds; chain/app/ante.go; chain/x/{content,leases}/msg_server.go role checks.

Commands: `git log`, `git status`, `grep -n`, `awk`, `sed -n`, `ls`, `df`, `go env GOMODCACHE`. The SDK claim in DR-11-07
was checked in the module cache (`cosmos-sdk@v0.55.0/x/auth/ante/basic.go:205-227`). Nothing was built or run.

Not verified here: whether unordered transactions are enabled in the app (chain/app/authmodule.go:79 calls
`UnorderedTransactionsEnabled()`; the setting was not traced); the node's transaction indexer configuration (the auditor
looks up committed transactions by hash on the devnet, agents/auditor/chainclient/rpc.go:367-384, so it works there).
