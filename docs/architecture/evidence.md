# Beta attestations and audit evidence

Reviewer note for Goal 09. This is not an audit. Public launch is disabled. There is no token, fee denomination, reward,
or penalty. `chain/x/evidence/README.md` holds the check order with every negative case, the payloads, the store layout,
events, the query, genesis, invariants, bounds, deviations D-1 to D-19 (D-14 withdrawn), and the keeper-level tests.
`agents/auditor/README.md` holds the producer's commands, classification, network policy, store, and reviewer. This page
covers the app wiring, the reviewer map, the labels and limitations, the producer network policy, retention, the
cryptographic-verification decision gate, and the app-level and devnet tests.

An evidence record says that an admitted auditor signed a statement about one lease. It is an attestation. It is not a
storage proof, not a unique-replica proof, and not independent: every run in this repository is one operator on one
machine. A valid auditor signature shows which admitted key made the statement. It does not make the statement true. The
chain compared two digests and fetched nothing (DEC-010). No evidence record renews, extends, or revives a lease.

Owner rulings that bind this goal (2026-11-02): Q1 "Account key signs" (the attestation key must derive the submitting
`AUDITOR` account's address; no relay; no rotation; devnet beta only), Q2 "Envelopes + id prefix", Q3 "Provider-answered
only", Q4 "Strict, read-time". The numbers 3600 s (round) and 86400 s (lifetime) are PROPOSED constants in the option
text the owner chose, not policy. Every other interim choice is a reversible implementation choice or a decision request
listed in `chain/x/evidence/README.md`; none of them is owner approval.

## Wiring

`app.New` builds the keepers in this order:

1. The `x/authority` read keeper (`Role`, `DevnetRole`, `DevnetNumeric`, `PolicyVersion`), and since goal 10 the
   `x/policy` keeper for the governed limits (`GovernedLimit`).
2. `x/providers`, and the hook-less `x/content` copy, with one shared `msgkit.Peers` set.
3. `leasesRead`: an `x/leases` keeper with no evidence reader. x/evidence reads leases only through it (`Lease`, one
   read of the leases store). It is never registered as a module or on a router.
4. The `x/evidence` keeper: `Roles` is the authority read keeper, `Limits` the `x/policy` keeper (the authority read
   keeper before goal 10), `Leases` is `leasesRead`, `Content` is
   the hook-less content copy, `Peers` is the shared set.
5. The `x/leases` keeper the app runs: the same dependencies as before Goal 09, with `Evidence` set to the x/evidence
   keeper. A renewal that names an `evidence_id` reads the committed record and is always rejected: `NOT_FOUND` (no
   record), `FORBIDDEN` (a live record, `RECORDED` or `REJECTED`), or `EXPIRED` (a record past its expiry).
6. The `x/content` keeper with the leases withdrawal hook, then the authority keeper `WithRegistry(...).WithPeers(peers)`.
7. `peers` lists `authority`, `content`, `evidence`, `leases`, and `providers`: a business id is one namespace across the
   five modules (`TestAuditRetryScenarios` reuses a lease business id in an audit and an audit business id in a lease
   message: `REPLAY` both ways).

Both lease keeper copies share the leases store key, so they read the same committed state. The compile-time checks
`var _ leases.Evidence = evidence.Keeper{}`, `var _ evidence.Leases = leases.Keeper{}`, `var _ evidence.Content =
content.Keeper{}`, `var _ evidence.Roles = authority.Keeper{}`, and `var _ evidence.Limits = policy.Keeper{}` (goal 10;
`authority.Keeper{}` before) are in `chain/app/app.go`.

`evidence.AppModule` replaces the scaffold slot. `RegisterServices` registers `svn.evidence.v1.Msg` on the message router
and `svn.evidence.v1.Query` on the gRPC query router; the closed evidence stub, `register`, `methodSpec`,
`rejectHandler`, and `checkEvidence` are gone from `chain/app`. x/evidence has no BeginBlock and no EndBlock: a
`RECORDED` record reads `EXPIRED` from the first committed time at or after its expiry, computed when it is read; nothing
is swept, and evidence takes no share of the lease expiry budget (owner ruling on Q4; x/evidence deviation D-4). The
evidence store is in the app's layout table (`checkRegistryLayouts` before goal 10, the load gate since): a committed
evidence store without the layout key or with another layout version than the persisted version map stops `app.New`
(`TestAppRefusesScaffoldLayout`). A node reopened from disk loads every store, the evidence
store included (`TestRestartLoadsCommittedState`, with the layout key only; `TestDevnetAuditLifecycle` stops two
validators after evidence records commit, restarts them, and they reach the other nodes' app hash). The svn header
slot stays scaffolding (`chain/x/scaffold`, DR-10-28); x/policy replaced the policy slot in Goal 10.

Genesis and export:

- The launch rule (`emptyCatalog`) refuses evidence records and a `current_epoch` other than 0 in InitChain and in
  `svnd genesis validate` (`TestLaunchGenesisRejectsEvidence`).
- App genesis validation refuses two `AUDITOR` submitters whose addresses share their first 8 bytes, because every
  evidence id and batch id must start with `IDPrefix` of the auditor's address (DR-09-30, owner ruling on Q2). It runs in
  `AppStateFromDocument` (devnet init), `ValidateAuthorityGenesis` (InitChain and `svnd genesis validate`), and so in
  InitChain (`TestGenesisRefusesAuditorPrefixCollision`, with the two synthetic keys of
  `evidencetest.PrefixCollisionKeys`: genesis rows carry addresses, so no private key is needed). The same address under
  another role, and two auditors with distinct prefixes, are accepted.
- `App.Export` refuses, before the module manager exports anything, when the evidence store holds any row besides its
  layout key. The export context has no block time, so the module export of a record would fail with
  `evidence.ErrNoBlockTime` inside the SDK module manager's goroutine, where a panic ends the process
  (`TestExportRefusesEvidenceState`, also with a record row written outside ABCI on a chain with no lease, where only
  this check refuses). `App.Export` of evidence state and an app-level re-import stay BLOCKED (DR-06-01, DR-07-02,
  DR-07-03, DR-08-06, DR-09-18). The module round trip runs at keeper level (`TestEvidenceGenesisRoundTrip` in
  `chain/x/evidence`) and from committed app state with the real lease keeper
  (`TestEvidenceGenesisRoundTripFromCommittedState`): the module export at a given block time carries each record with
  its reported state (a `RECORDED` record past its expiry is exported `EXPIRED`); imported into a discarded branch whose
  evidence store was emptied, it re-exports equal, the invariants hold, and every row comes back except the business-id
  rows, which the v0 genesis schema cannot carry.
- Before a later goal enables evidence export or a non-empty evidence genesis, two app rules are needed that do not
  exist now (the launch rule and the export refusal make them unreachable): `App.Export` must give its context the last
  committed header time, and genesis validation must require each record's `AuditorAddress(signer_pubkey)` to be an
  `AUDITOR` submitter of the same document (the module checks the id prefix and the lease, not the role).

## Who may write

| Action | Outer signer | Inner signature |
| --- | --- | --- |
| `SubmitAuditBatch` | an `AUDITOR` account | SVN-EVIDENCE-ATTESTATION-1 over the batch of envelopes, by an ed25519 key whose address is the outer signer |

- The ante admits only submitter rows and council seats. A council seat is refused with `ERROR_CODE_FORBIDDEN` before
  any handler (DR-06-06); `TestSeatEvidenceRefusedForTheSeatRule` sends a batch that is valid in every field, signed by
  the seat's own ed25519 key (so it even passes the key binding), and the same batch shape from `auditor-1` commits.
- Any other admitted role is `FORBIDDEN` at the handler's role check, a `BROADCASTER` relay included (no relay, owner
  ruling on Q1; `TestSecpAuditorAccountCannotBind`, `TestRejectedAuditMessagesLeaveTheFullState`).
- An `AUDITOR` account with a secp256k1 key cannot make a binding attestation: no ed25519 key derives its address
  (`UNAUTHORIZED` at step 7, `TestSecpAuditorAccountCannotBind`).
- The devnet genesis has two `AUDITOR` accounts, `auditor-1` and `auditor-2`, with ed25519 account keys generated at
  `svnd devnet init` and written under the home (`accounts/auditor-N.hex`, mode 0600), never printed or committed
  (`TestDevnetKeysAreDistinct`). They are one operator on one machine, not independent auditors.

## Reviewer map

| Question | Where |
| --- | --- |
| What does a record mean, what may change after acceptance, which checks run in which order | `chain/x/evidence/README.md` ("What an attestation is", "SubmitAuditBatch: check order", "What changes after acceptance") |
| Which bytes the auditor signs | `chain/x/evidence/signdoc.go`; golden vectors `chain/x/evidence/testdata/sign-vectors.json`; the producer imports the same builders (`TestProducerMatchesChainSignVectors`) |
| How the app wires it | this page, "Wiring"; `chain/app/app.go`, `chain/app/services.go`, `chain/app/state.go` |
| How the producer fetches, classifies, signs, and submits | `agents/auditor/README.md` |
| How a reviewer checks one evidence id | `auditor verify --evidence-id ID --store DIR --node URL` (`agents/auditor/review`); verdicts below |
| What labels a UI may show | `agents/auditor/labels/evidence-labels.json`; `apps/web/src/evidence-labels.ts` (not rendered anywhere yet) |
| What the real-transaction runs show | "App-level tests" and "Devnet tests" below |

Reviewer verdicts (`auditor verify`, first failure wins): `NOT_ON_CHAIN` (node only), `PAYLOAD_UNAVAILABLE`,
`CHECKSUM_MISMATCH`, `REPORT_INVALID`, `SIGNATURE_INVALID`, `ENVELOPE_MISMATCH`, `RECORD_MISMATCH`, `CONSISTENT`. The
output names its record source first: `NODE <url>` (read through an unverified ABCI query; trusts that node) or
`FILE <path>` (not read from the chain). `CONSISTENT` is still an attestation.

Evidence availability and chain inclusion are separate questions. A record on chain whose report the producer's store
lost is `PAYLOAD_UNAVAILABLE`; the record does not change. A record whose observation is a provider's 404 is a
provider-availability result (`REJECTED`, report `FAIL HTTP_STATUS`), and the reviewer finds it `CONSISTENT`.

## Labels and limitations

The label catalog `agents/auditor/labels/evidence-labels.json` (`svn-evidence-labels-proposed-v0`, PROPOSED, DR-09-24)
holds the mode labels (`AUDITOR_ATTESTATION`, `PROVIDER_RECEIPT`), the state labels (`RECORDED`, `REJECTED`, `EXPIRED`),
`claim_limit` `ATTESTATION_NOT_STORAGE_PROOF`, the event sentence, the acceptance sentence, and these limitations,
verbatim:

- Auditors can lie.
- Providers may fetch from another host.
- Node IDs do not prove independent replicas.
- A valid signature does not make an observation true.
- Accounting success is not physical capacity or operator independence.
- A sampled check covers only the sampled segments.
- This auditor is not shown to be independent of the provider.
- No evidence record renews, extends or revives a lease, in any state.
- No cryptographic storage proof exists in this beta. It is a separate decision gate.

`TestDocsContainLimitations` (`agents/auditor/labels`) checks that this page and `chain/x/evidence/README.md` contain
every one of them. "EvidenceAccepted means the record was written to the ledger. It does not mean the check passed."
The `RECORDED` text ends "A recorded attestation describes one past observation. It does not show that the lease is held
or the content is available now." (`TestEvidenceCannotExtendOrReviveLease` reads it from the catalog.)

Limits of this goal:

- Every success needs a held lease, so evidence is recorded only on a chain with the labelled DEC-001 content and DEC-005
  lease devnet parameters. Elsewhere every batch ends at `NOT_FOUND` or earlier (`TestAuditWithoutDevnetParams`).
- Auditors are admitted only in genesis; no message admits, removes, or rotates one (DR-09-08). A dishonest auditor's
  records stay.
- The chain cannot tell a shared operator or a shared key: a provider and an auditor run by one operator look like
  independent parties.
- `PROVIDER_RECEIPT` is refused on chain (`FORBIDDEN`, DR-09-07).
- A `RECORDED` record written while a lease was held keeps reading `RECORDED` after a later `EndLease`,
  `WithdrawContent`, or lease expiry until its own expiry, at most 86400 s (PROPOSED) and never past the lease deadline
  at recording. It describes one past observation.
- The producer's timestamps come from the auditor host clock and are not verified. The chain reads only the header time.

## Producer network policy (PROPOSED, DR-09-10)

The producer (`agents/auditor`) fetches only from provider endpoints that pass its destination policy; the chain never
fetches anything. In short (the full rules are in `agents/auditor/README.md`):

- `https` only; no userinfo, fragment, query, or path in the endpoint URL; the package path
  `/packages/{content_id}/{content_version}` is built from chain values.
- Every resolved address must be globally reachable under the pinned IANA special-purpose tables, or be permitted by an
  exact allowlist entry `(provider_id, host, ip, port)`; no CIDR, wildcard, "allow private" switch, or global disable.
  The session dials only the vetted addresses.
- No redirect is followed; no proxy is used (proxy environment variables are ignored); `Accept-Encoding: identity`;
  TLS 1.2 minimum with system roots or an explicit CA file; bounded headers, bodies, and time.
- Classification follows the owner's Q3 ruling: only an answer the provider's verified server gave (a wrong or missing
  byte, a length mismatch under its own framing, too many bytes, 404 or 410, another 4xx or 5xx after retries) is
  `FAIL` and goes on chain as `REJECTED`. A network, TLS, timeout, DNS, redirect, or policy failure is `INCOMPLETE` and
  stays off chain; its report is kept.
- The endpoint must be one the provider account declared: a capability manifest bound to its committed
  `endpoint_manifest_digest` (interim choice N2, 2026-11-06; approved in general terms, see Residual risks). Every
  report records `endpoint_source`:

  | Endpoint source | How the endpoint is bound | Provider-answered wrong or missing bytes | Correct bytes |
  | --- | --- | --- | --- |
  | `CAPABILITY_MANIFEST` | a capability manifest file that names the provider and its current signing key id and hashes to the provider record's committed `endpoint_manifest_digest`, checked before any request | `FAIL`, signed, `REJECTED` on chain (never expires) | `PASS`, signed, `RECORDED` until its expiry |
  | `OPERATOR_CONFIG` | none: the operator typed it into the configuration | `INCOMPLETE ENDPOINT_UNBOUND`, the provider-answered reasons kept in the report; nothing signed, off chain | `PASS`, signed, `RECORDED` until its expiry |

  The rule is in `classify`, where the result is decided; `batch.NewItem` refuses a `FAIL` report whose source is not
  `CAPABILITY_MANIFEST`, and the reviewer reports such a signed item as `RECORD_MISMATCH`. The chain cannot see the
  source.

The configured chain RPC URL is trusted operator configuration, not a provider endpoint, and may be loopback. Reads
through it trust the one node they ask.

## Retention (DEC-004 approved default)

No retention duration is set; that is the approved DEC-004 default. The producer's store deletes nothing: reports,
envelopes, batches, submissions, pending runs, and the append-only index stay. Chain records are never rewritten or
deleted (x/evidence invariant I3). This goal does not invent a period. The store root belongs under `.local/`, which Git
ignores; no report, key, or media byte is committed.

## Cryptographic-verification decision gate (DR-09-G1): recorded, not implemented

Status: OPEN, with no goal in 01-30 (REQ-SCOPE-011; REQ-EVID-006 DEFERRED). Nothing in Goal 09 implements, approximates,
or prepares it. Evidence kinds 3, 4, and 5 stay reserved and are rejected (`MALFORMED`; `TestEvidenceMessagesDecode`
sends kind 4 through the app). No Merkle code exists in `chain/x/evidence` or `agents/auditor`. A decision must first
choose between an explicit external Filecoin integration and a separately reviewed local scheme, and for a local scheme
specify chunking, the commitment format, the challenge randomness source (validators must not fetch a beacon during
execution), deadlines, replay resistance, proof-size bounds, deterministic verification, gas bounds, adversarial tests,
and external review. Preconditions: independent operators (DEC-002), independent review of the evidence rules before any
reward or penalty, and a new protocol version. Whether to add it to `docs/decision-register.md` is the owner's call.

## App-level tests (`chain/app`, in-process FinalizeBlock, real keepers, real signed transactions)

Every test runs the real application with x/evidence wired to the real x/authority, x/leases, and x/content keepers.
Auditor keys are generated at test time. Leases exist only under the labelled DEC-001 and DEC-005 devnet parameters.

| Test | What it shows |
| --- | --- |
| `TestEvidenceMessagesDecode` | D-19: an item carrying the reserved field 9 (`reject`), another unknown field in an item, the observation digest, the auditor signature, or the message fails strict decoding (sdk code 2) in CheckTx and FinalizeBlock, before the ante; the same message without the field commits; reserved kind 4 decodes and the handler answers `MALFORMED`. |
| `TestRejectedAuditMessagesLeaveTheFullState` | One or more cases for every row of the check order a committed transaction can reach (handler cases from presence to withdrawn content, an expiry-backlog case for T6 and five ante cases: outsider, fee, gas 0, memo, 70 KiB), each with its exact svn code, the handler reached (ante committed), and the full non-account state byte-identical; the base batch is valid under Simulate. |
| `TestAuditRetryScenarios` | The same business id and payload in a second transaction (new hash), in a later block and twice in one block: code 0, `evidence_result idempotent=true`, no record or typed event, full state unchanged; another payload `REPLAY`; a lease business id in an audit and an audit business id in a lease message `REPLAY`; another auditor resending the batch `UNAUTHORIZED`. |
| `TestNoEvidenceRenews` | A provider-signed renewal naming a live `RECORDED` record (of its own lease and of another), a live `REJECTED` record, a record past its expiry, and an absent id: `FORBIDDEN`, `FORBIDDEN`, `FORBIDDEN`, `EXPIRED`, `NOT_FOUND`, each with the full state and the lease query bytes unchanged; the same renewal without an evidence id commits. |
| `TestEvidenceCannotExtendOrReviveLease` | REQ-INV-005: renewal naming live evidence `FORBIDDEN` at expiry-1 and `EXPIRED` at expiry; after `EndLease`, `WithdrawContent`, and lease expiry by header time, new audits (passing or failing) are `CONFLICT`, `WITHDRAWN`, `CONFLICT` with the state unchanged; the earlier records still read `RECORDED` by lease until their own expiry; no lease, provider, or content byte changed because of an evidence message. |
| `TestDishonestAdmittedAuditorInApp` | Plan section 18: auditor-2 records a false PASS (`RECORDED`) and a false FAIL (`REJECTED`) beside auditor-1's opposite records, with no other store changed; the limits still bind it (D-9 `BOUNDS`, D-10 `CONFLICT`, D-17 `FORBIDDEN` for auditor-1's ids, after which auditor-1's batch commits, renewals naming the false records `FORBIDDEN`, and the per-epoch cap D-15 `BOUNDS` at item 1025 and lifted in the next epoch). The README sentence "A valid auditor signature shows which admitted key made the statement. It does not make the statement true." is asserted. |
| `TestSeatEvidenceRefusedForTheSeatRule`, `TestSecpAuditorAccountCannotBind` | See "Who may write". |
| `TestEvidenceQueryTimeThroughABCI` | `Query/Evidence` through ABCI reports `RECORDED` at the heights whose header time is E-2 and E-1 and `EXPIRED` at E and E+1, at the latest height and historically; `REJECTED` at every height; the stored record stays `RECORDED`. |
| `TestAuditFlowReplayAndQueries` | Three nodes (equal app hash and result codes at every height): `RECORDED` and `REJECTED` items, a 32-item batch and a second auditor in one epoch, renewals naming evidence, and the epoch boundary at effective-1 (a batch for epoch E commits, one for E+1 is `STALE_VERSION`) and effective (the reverse); a fresh application replays every block to the same hashes, gas, and full state, and `Query/Evidence` bytes (by id, by lease, page by page) are equal across nodes and the replay. |
| `TestAuditBatchMeasurements` | Plan section 19 fixtures F1, F8, F32, F32max, F32rej, Rcount, Rpayload, and Q100: transaction bytes, committed and simulated gas (asserted equal), result and event bytes, evidence-store key and byte growth (logical KV bytes, not IAVL or disk bytes; asserted 2N+2 keys per batch, +1 for an auditor's first batch), record size, canonical payload bytes, query response bytes and query gas. Per-fixture measurement on one machine; no throughput or network-capacity claim. |
| `TestEvidenceEventSchemas` | Every evidence event of a fixed flow parses and matches `chain/app/testdata/evidence-events.golden.json` (auditor prefixes normalized): `EvidenceAccepted` then `evidence_state` per item, `evidence_result` alone for an idempotent resend, nothing for a rejection; forbidden-name scan. |
| `TestNoPrivateDataInEvidenceStateOrEvents` | Only digests are stored: canary digests as observations, each found in the evidence store (positive control), and no store, transaction, or event holds a canary or a URL scheme. The message has no free-text field, so the canaries never enter a transaction; the devnet event scan checks producer output for URLs. Opaque ids and digests stay linkable by anyone holding the preimage. |
| `TestEvidenceQueryGasAndBounds` | Query rejections with svn codes (no id `MALFORMED`; id outside the pattern, limit above the cap, page key outside the pattern, page key with an evidence id `BOUNDS`); query gas below the node limit. |
| `TestGenesisRefusesAuditorPrefixCollision`, `TestLaunchGenesisRejectsEvidence`, `TestExportRefusesEvidenceState`, `TestEvidenceGenesisRoundTripFromCommittedState`, `TestAppRefusesScaffoldLayout`, `TestRestartLoadsCommittedState` | See "Wiring". |
| `TestAuditWithoutDevnetParams` | Without the DEC-001 and DEC-005 devnet parameters a valid batch ends `NOT_FOUND` with the state unchanged. |
| `TestAcceptanceSignatureCoversDigestBytesTerms` | REQ-EVID-002 with real keys: an acceptance signed over another manifest digest, byte count, or terms digest is `UNAUTHORIZED` with the state unchanged; acceptance is a promise, not possession. |
| `TestSimulateEstimatesGas`, `TestEveryProtocolMessageDecodes` | `SubmitAuditBatch` simulates with gas equal to committed gas; every populated variant from the service account decodes and is `FORBIDDEN` at the role check. |

Rows of the check order that no committed transaction reaches, and where they are tested:

- Step 1 (`msgkit.Enforce`): the ante applies the gas and size bounds first; a signer that is not 20 bytes cannot sign a
  transaction (the ante resolves it to an account address). Keeper ladder enforce rows.
- Step 2, a nil item: an item on the wire always decodes as a message; an empty item fails the `evidence_id` presence
  check. Keeper ladder presence-item-nil.
- Step 3, no role (`UNAUTHORIZED`): the ante admits only submitter rows and council seats, and refuses a seat's evidence
  message. Keeper ladder role-none.
- Step 15, a business payload with no canonical form (`MALFORMED`): every integer of the business payload is also
  signed, so such a payload is `UNAUTHORIZED` at step 8 first (`TestRejectedAuditMessagesLeaveTheFullState`,
  `content_version` 2^53).
- Step 26, content absent (`NOT_FOUND`): needs a lease that names unregistered content, an invariant violation. Keeper
  ladder.
- Step 27 (T9, the per-auditor per-epoch cap) is reached through the app in `TestDishonestAdmittedAuditorInApp`, not in
  the reject table.

## Devnet tests (`chain/devnet`)

Label: single-machine four-process devnet (not independent operators). The test endpoint is a loopback test endpoint,
synthetic package, not a provider agent. auditor-1 and auditor-2 are one operator on one machine; not independent
auditors.

`TestDevnetAuditLifecycle` builds `agents/auditor` with `go build` (a build failure fails the test), starts four svnd
processes, and runs:

1. A loopback HTTPS endpoint with a runtime CA (lease la's package served correctly, lb's with one corrupted byte, lc
   absent: 404) and a closed port for ld. Providers p1 and p2 registered with an `endpoint_manifest_digest` equal to
   the digest of a capability manifest the test writes (p1: the endpoint; p2: the endpoint, then the closed port; each
   naming the provider and its signing key id) and admitted by the council; seven contents whose manifest digest is
   SHA-256 of the SVN-JCS-1 text of a synthetic `svn-media-manifest-v1`; offers and acceptances (DEC-005 3600 s term,
   start in the past).
2. The producer configuration: exact allowlist entries for the endpoint and the closed port; each audit names its
   endpoint by its index in its provider's capability manifest (`capability_manifest_path`).
3. A round wait (polling the header time) so a batch signed now commits in its round.
4. `auditor run`, `sign`, `submit` as auditor-1: la `PASS`, lb `FAIL SEGMENT_DIGEST_MISMATCH`, lc `FAIL HTTP_STATUS`,
   ld `INCOMPLETE CONNECT_FAILED`. Two batches; the acknowledgement (CheckTx code 0) and the committed code and height
   are printed separately; records read back `RECORDED`, `REJECTED`, `REJECTED`; ld has no item and no record (a network
   failure stays off chain); `status` reads the same committed result without broadcasting; the reviewer finds all
   three `CONSISTENT`, each report with `report_endpoint_source` `CAPABILITY_MANIFEST`. Then (N2) the same failing
   endpoint typed into the operator's `endpoints` list for lb and lc: `run` gives
   `INCOMPLETE ENDPOINT_UNBOUND SEGMENT_DIGEST_MISMATCH` and `INCOMPLETE ENDPOINT_UNBOUND HTTP_STATUS`, the test reads
   `endpoint_source` `OPERATOR_CONFIG` from both stored reports, `sign` signs no batch, and no record appears for either lease or for the evidence ids those reports would have.
5. A renewal naming la's live record is `FORBIDDEN`; the lease bytes do not change. The same business payload under a
   new transaction hash is an idempotent success with one set of records.
6. auditor-2 (test code, fetching nothing) records a false FAIL for la and a false PASS for lb; the reviewer finds the
   false PASS `PAYLOAD_UNAVAILABLE` (no report exists); D-9, D-10, D-17 refusals, auditor-1's own batch commits, a
   renewal naming the false PASS is `FORBIDDEN`, and a `BROADCASTER` relay of an auditor batch is `FORBIDDEN`.
7. Availability is not inclusion: la's report removed from the store, `PAYLOAD_UNAVAILABLE` with the record unchanged;
   lb's report altered, `CHECKSUM_MISMATCH`; lc's 404 record `CONSISTENT`. An expired round: a second run's signed
   batch, copied by test code into the previous epoch (re-signed by auditor-1's key) and sent with `auditor submit`:
   warning `EPOCH_LIKELY_STALE`, CheckTx code 0, committed code 5 (`STALE_VERSION`), `FAILED(5)`, the reviewer
   `NOT_ON_CHAIN`. No quorum (v3 and v4 stopped): `auditor submit` is acknowledged and stays `SUBMITTED`; a second
   submit rebuilds the same bytes, the node's mempool refuses them ("tx already exists in cache"), the producer records
   the answer as `UNKNOWN`, and the batch stays `SUBMITTED`. After v3 and v4 restart, `status` reports `COMMITTED(0)`,
   also after the submission records are removed (transaction bytes stored before the broadcast are found by hash).
8. After la's first record expires, a renewal naming it is `EXPIRED`; the record reads `RECORDED` at the last height
   whose header time is below its expiry and `EXPIRED` at the first height at or after it.
9. auditor-1 records le and lw while held; then le ends, cw is withdrawn, and lx passes its deadline: new audits of
   each, passing or failing, are `CONFLICT`, `WITHDRAWN`, `CONFLICT`, with the lease and content query bytes unchanged;
   the earlier records still read `RECORDED`.
10. F1, F8, and F32 test-built batches: committed transaction bytes and `gas_used` from `/tx`.
11. Equal `Query/Evidence` bytes on all four nodes; the event scan (typed events parse with their `msg_index`;
    forbidden names; canaries; no URL); an in-process replay of every block reproduces the header app hash of all four
    nodes and the replay time is logged (one machine; not a catch-up benchmark).

Goal 09 changed two devnet assertions, without weakening them: `TestDevnetKeysAreDistinct` now expects 8 accounts and
12 account and seat keys (6 and 10 before) and checks the auditors' key type, file mode, and distinct prefixes;
`TestDevnetSeatsHaveNoSubmitterRow` now expects 8 submitters and 12 auth accounts (6 and 10 before).

CI runs `TestDevnetAudit` in its own go-race shard (`devnet-evidence`); the `devnet` shard skips it, so every
`chain/devnet` test runs under `-race` in exactly one of the two. The CI `go` job sets `SVN_SKIP_DEVNET=1`: its
non-race run skips the seven tests that start devnet validator processes and runs the other tests of the package.
`tests/unit/test_ci_devnet_coverage.py` fails if that skip is set and the two shards stop covering the package.

## Residual risks

- An admitted auditor can lie, in both directions; a `REJECTED` record never expires; nothing is paid or slashed.
- Endpoint source (N2). An honest producer files `REJECTED` only for an endpoint from a capability manifest bound to
  the provider record's committed `endpoint_manifest_digest`; a failure at an operator-configured endpoint stays off
  chain as `INCOMPLETE ENDPOINT_UNBOUND`. A `PASS` from an operator-configured endpoint is still signed and `RECORDED`
  (until its expiry, at most 86400 s, PROPOSED), although nothing bound that endpoint to the provider account; its report
  says `OPERATOR_CONFIG`, and the chain cannot see that. The binding is a digest the provider account committed, not a
  check of the detached manifest signature (DR-07-09); it shows that the provider account declared the endpoint, not
  that the provider controls the host. Nothing on chain enforces any of this: a dishonest admitted auditor can still
  sign a `REJECTED` record for any endpoint. N2 is an interim choice of 2026-11-06, reversible: the owner
  approved the recommendation in general terms; the option was named in the orchestrator's message after the
  approval.
- Business ids are one unsigned namespace across modules: any admitted account can take a pending batch's business id
  first and make the whole batch `REPLAY` (`chain/x/evidence/README.md`, residual risks; DR-09-28 asks for business ids
  bound to the signer). The producer moves to the next business attempt, whose business id it draws from `crypto/rand`,
  so retry ids cannot be computed and committed in advance; a broadcast already in the mempool can still be front-run.
- The auditor's account key also signs its attestations; the domain-separation argument is unreviewed (devnet beta only).
- A historical `Query/Evidence` uses that height's commit-info time when the node has it, else the latest header time,
  so it can report `EXPIRED` for a record that was `RECORDED` at that height on a node without the commit info.
- Raw `/store/evidence/...` queries bypass the query router: no gas meter, no page cap, and the stored (not reported)
  state. Do not expose the CometBFT RPC port publicly.
- Every run here is one operator on one machine; REQ-RISK-003's independent half stays BLOCKED.
