# Goal 11 signing-boundary map (signing reader)

Base: worktree `wt-11`, branch `goal-11`, HEAD `dbf0897` (main; goals 00-10 merged). All `file:line` citations are
repository paths in that worktree unless prefixed `SDK:` (`/root/go/pkg/mod/github.com/cosmos/cosmos-sdk@v0.55.0/`) or
`CMT:` (`/root/go/pkg/mod/github.com/cometbft/cometbft@v0.40.0/`). Read-only work: nothing was built, run or edited.
`[Educated Guess]` marks an inference or a recommendation that no code, test or owner text states. Nothing here is an
owner decision unless it is quoted as one with its source.

Naming note: the task text says "DEC-006-06 seat rules". No identifier `DEC-006-06` exists in the repository (grep
returns nothing). The seat ruling is owner ruling **DR-06-06** (`docs/architecture/authority-authorization.md:94`).
DEC-006 is the creator licence decision and is unrelated.

---

## 1. Findings in brief

1. **The prerequisite "approved service/provider signing boundaries" does not exist as an approved document.** The
   signer/relay tables live in `docs/protocol.md`, which says it is "draft contract ... not an approved protocol"
   (`docs/protocol.md:3`); goal 02 is IMPLEMENTED, not VERIFIED (`docs/progress.md:32`). What IS approved: the signing
   *domains*, SVN-JCS-1/SHA-256/Ed25519 (Gate A reviewer reading, `docs/decision-register.md:23`), DEC-001, DEC-009,
   DEC-010, and owner rulings DR-06-06 (seats), goal-09 Q1/Q2 (auditor), goal-10 Q1-Q3 + DR-10-00 (governance, goal 10
   only). Section 2 quotes each. Goal 11 should treat the prerequisite as unmet unless the owner approves a pinned
   boundary document (the DR-10-00 precedent) `[Educated Guess]`.
2. **One address = one role; roles are genesis-only.** Five submitter roles (`docs/protocol/proto/svn/types/v1/types.proto:131-138`)
   plus council seats (no role). No message creates or changes a submitter row; `RotateServiceSubmitter` is engine-only
   and only for `ADMITTED_SERVICE` (`chain/x/authority/engine.go:1101-1117`; `docs/architecture/authority-authorization.md:18`).
3. **Outer signer = the message's `signer` field** (custom GetSigners, `chain/app/encoding.go:51-53,144-154`). The ante
   admits it from the authority store or the seat list and lets seats sign only propose/approve
   (`chain/app/ante.go:139-180`). Each handler then checks the role (section 5). No router FORBID list remains (goal 06's
   D1 ended in goal 07, `chain/x/authority/README.md:22`).
4. **Provider consent = an ed25519 InnerSignature by the provider's CURRENT registered signing key**, verified by
   `msgkit.VerifyInner` (`chain/msgkit/innersig.go:115-129`) via `Providers.VerifyProviderSignature`
   (`chain/x/providers/handoff.go:61-67`). It is embedded in `MsgAcceptLease.provider_acceptance`,
   `MsgRenewLease.provider_signature`, `MsgEndLease.provider_signature` (provider path),
   `MsgRotateProviderKey.current_key_signature`, `MsgDrainProvider.provider_signature`. The transaction signer may be a
   `BROADCASTER` (relay) or the provider's own `ADMITTED_PROVIDER` account, never a service account.
5. **Business ids are one unsigned namespace across five modules.** Same id + same payload = ABCI 0 + `<module>_result
   idempotent=true` and no module write; same id + other payload = `ERROR_CODE_REPLAY` (svn code 3); a failed attempt
   stores no id. But every check *before* the business gate re-runs on a retry, so a committed action can answer
   `FORBIDDEN`, `UNAUTHORIZED`, `STALE_VERSION` or `BOUNDS` on resubmission (section 8.3). There is **no gRPC query by
   business id**; reconciliation is by tx hash, by object id, or by raw `/store` read.
6. **DEC-001 is approved, but on chain the host-custody role is rejected and production content registration is
   `DECISION_BLOCKED`**; the devnet hosting intent proves only that "the key in the message" signed, not creator consent
   (`chain/x/content/intent.go:29-33`). REQ-CHAIN-013's "publication permission bit" cannot be enforced on chain.

---

## 2. Is there an APPROVED signing-boundary document? (quotes)

| Source | Quoted text | Status |
| --- | --- | --- |
| Goal 11 spec | "Prerequisites: Goals 04, 07-10; approved service/provider signing boundaries." (`docs/goals/11-transaction-broadcaster-and-outbox.md:8`) | Requirement |
| Progress row 11 | "Goals 04, 07-10. Signing boundaries from Goal 02. DEC-001 blocks host custody of user keys." (`docs/progress.md:41`) | Points at goal 02 |
| Goal 02 status | row 02 `IMPLEMENTED` (`docs/progress.md:32`); "This file does not mark the goal VERIFIED. Protocol v0 freezes once this goal passes review. This revision is the candidate. It does not claim that review has passed." (`docs/evidence/goal-02.md:7`) | Not approved |
| Protocol doc | "Status of this document: draft contract for the permissioned community beta. It is not an approved protocol." (`docs/protocol.md:3`) | Not approved |
| Protocol signer tables | Transition table with "Signer" and "Who may relay" columns (`docs/protocol.md:225-244`); goal-10 revision table (`docs/protocol.md:369-377`) | Candidate text |
| Gate A record | Reviewer's reading: "SVN-JCS-1, SHA-256, Ed25519, the proposed signing domains, the safety and governed caps, and the shortvideo-dev- prefix are accepted." (`docs/decision-register.md:23`) | Domains APPROVED; who-signs-what is not named |
| DEC-001 | "The account host holds a delegated signing key for beta public records, and the product discloses that custody. A web passkey is a login authenticator and is not a Cosmos transaction key. ... Operators, not viewers, hold chain-submission credentials." (`docs/decision-register.md:37`); "Status: APPROVED" (`:43`); Blocks: "Goal 02 must not freeze the account host as the owner of hosting-intent signatures." (`:53`) | APPROVED default |
| DEC-009 | "The approved choice is council only. ... The emergency path was not selected." (`docs/decision-register.md:281`) | APPROVED |
| DEC-010 | "`observation_digest` is required, stored, and bound by the auditor signature." (`docs/protocol.md:149`) | APPROVED |
| DR-06-06 | "RESOLVED by the owner on 2026-11-02: 'Seat-only admission (Recommended)': 'Admit seat accounts from the council seat list in authority's config, not from a submitter row, and refuse a seat address in the submitter list entirely ... Seat keys can then only send council messages.'" (`docs/architecture/authority-authorization.md:94`) | Owner ruling |
| Goal 09 Q1 | "Q1 'Account key signs (Recommended)': the submitting (outer) account must be an `AUDITOR`; the inner attestation ed25519 key must derive that account's address; no `BROADCASTER` relay; no rotation; devnet beta only (DR-09-01)." (`chain/x/evidence/README.md:49-50`) | Owner ruling |
| Goal 10 revision | "Seats still sign only `ProposeChange` and `ApproveChange`; the AUTHORITY submitter still signs `ExecuteChange`." (`docs/protocol.md:367`), under the owner's Q1-Q3 answers (`docs/protocol.md:342`) | Owner ruling (goal 10) |
| DR-10-00 | "On 2026-11-08 the owner approved it as pinned (commit `18fa310`, file sha256), for goal 10 only (DR-10-00 ...)" (`docs/evidence/goal-10.md:17`) | Goal 10 only |
| Requirements | "State who signs each transition and who may relay it. Provider signatures are not service signatures (REQ-CHAIN-012). A broadcaster must not sign provider acceptance (REQ-CHAIN-013)." (`docs/requirements.md:629`, goal 02 checklist) | Requirement text, not approval |
| Search | `grep -ri "signing boundar"` finds only the goal 11 spec and the playbook (`docs/prompt-playbook.txt:888,910,923`) | No such document |

Verdict: the boundaries are **partly approved** (domains, DEC-001/009/010, DR-06-06, Q1, goal-10 seat/authority split)
and **partly proposed** (the per-message signer and relay columns, all interim inner-payload schemas DR-07-01,
DR-08-04, and the chain deviations D-11/D-13/DR-07-08). Two registry/evidence relay rows of the candidate table are
already overridden by code (section 5, notes R1-R2).

---

## 3. Roles, key classes and what each may sign

Roles: `SubmitterRole` enum (`docs/protocol/proto/svn/types/v1/types.proto:131-138`); admission row `0x02||addr` holds one
role byte (`chain/x/authority/README.md:38`); genesis refuses a duplicate address (`chain/x/authority/genesis.go:158`) and
a seat address with any role (`chain/x/authority/genesis.go:174-179`; `chain/genesis/document.go:486-499`).

| Identity | How admitted | Devnet account(s), key type (`chain/devnet/init.go:136-143,228-236`) | Outer signer of | Inner signatures it produces |
| --- | --- | --- | --- | --- |
| `AUTHORITY` submitter | genesis row | `authority`, secp256k1 | `MsgExecuteChange` (`chain/x/authority/engine.go:526-531`); `MsgEndLease{authority_end:true}` (`chain/x/leases/msg_server.go:680-686`) | none |
| `ADMITTED_SERVICE` ("service", scheduler, "submitting host") | genesis row; engine-only rotation | `service`, `service-2`, secp256k1 | `MsgOfferLease` (`chain/x/leases/msg_server.go:239`); `MsgRegisterContent`, `MsgWithdrawContent` (`chain/x/content/msg_server.go:78,186,348-357`) | none (the hosting intent is a different key, section 9) |
| `ADMITTED_PROVIDER` (provider operator account) | genesis row | `provider-1`, `provider-2`, secp256k1 | `MsgRegisterProvider` (`chain/x/providers/msg_server.go:128`); Update/Rotate/Drain when signer == record account (`:202,248,314`); Accept/Renew/End for its own provider only (`chain/x/leases/msg_server.go:449-453,630-634`) | none from the account key |
| Provider signing key (ed25519, `Provider.signing_pubkey`/`signing_key_id`) | registered by `MsgRegisterProvider`, rotated by `MsgRotateProviderKey` | not generated by devnet init; tests create it with `ed25519.GenerateKey` (`chain/devnet/registry_test.go:240-241`) | never an outer signer | ACCEPT, RENEW, END, DRAIN, ROTATE domains (sections 5-6) |
| `BROADCASTER` | genesis row | `broadcaster`, secp256k1 ("relays provider-signed lease messages", `chain/devnet/init.go:122-123`) | Accept, Renew, provider-path End only (`chain/x/leases/msg_server.go:405,628-629`) | none |
| `AUDITOR` | genesis row; ids prefixed by address | `auditor-1`, `auditor-2`, **ed25519** account key | `MsgSubmitAuditBatch` only (`chain/x/evidence/msg_server.go:203`) | `SVN-EVIDENCE-ATTESTATION-1` with the SAME account key (`chain/x/evidence/signdoc.go:51-59`) |
| Council seat (no role) | seat list in authority config (`AdmissionOf`) | `seat-1..4` keys in `<home>/accounts/seat-N.hex` (`chain/devnet/init.go:228-236`) | `MsgProposeChange`, `MsgApproveChange` only (`chain/app/ante.go:161-167,173-180`; engine `seatOf` `chain/x/authority/engine.go:360-363,474-476`) | none |
| Hosting-intent key | not registered on chain | any ed25519 key in the message (`chain/x/content/intent.go:29-33`) | never | `SVN-HOSTING-INTENT-1` (devnet role `DEVNET_TEST_ONLY` only) |
| Users / viewers | off-chain session | none | nothing | none ("Viewers authenticate to the application. Operators hold chain-submission credentials. A web passkey is not a Cosmos key.", `docs/requirements.md:211`) |
| Validators | consensus keys | node keys | no application messages | CometBFT votes only |

All dev private keys of a devnet live together under `<home>/accounts/*.hex`, mode 0600 (`chain/devnet/init.go:104,223,236,587`),
seat keys included: one operator, one machine.

Genesis-only consequence: a new broadcaster or service key needs a new genesis or an upgrade handler; there is no
admission message (`chain/x/authority/README.md:22`: "Service account rotation is engine-only (`RotateServiceSubmitter`);
no message reaches it."). A goal-11 devnet with more than one broadcaster key needs a devnet-init change
`[Educated Guess]`.

---

## 4. Admission layers a submitted transaction crosses

1. Decode: strict decoder; unknown or non-critical unknown fields fail with codespace `sdk` code 2 (`chain/app/encoding.go:110-138`).
   Unregistered message types (a direct admit/disable/force-withdraw) fail the same way (`docs/architecture/registries.md:32`).
   Registered messages: 14 svn messages (`chain/app/encoding.go:156-173`) plus SDK `cosmos.auth.v1beta1.MsgUpdateParams`
   (registered by `authtypes.RegisterInterfaces`, `chain/app/encoding.go:62`; Msg service `chain/app/authmodule.go:74`),
   whose signer is the module address `authtypes.NewModuleAddress("svn")` (`chain/app/app.go:206`), which has no key.
2. Ante chain (`chain/app/ante.go:28-44`): set-up, extension options, validate-basic, **timeout height/timestamp**
   (`:32`; SDK `x/auth/ante/basic.go:205-222`: sdk code 30 `ErrTxTimeoutHeight`, sdk code 42 `ErrTxTimeout`, SDK
   `types/errors/errors.go:100,147`), envelope (memo must be empty `BOUNDS`; fee payer/granter/tip `FEE_REJECTED`,
   `:66-86`), memo, bounds (gas 1..MaxGasPerTx except simulate 0; tx bytes, `:90-108`), fee amount empty (`:112-121`),
   pubkey, sig count, sig gas, **SIG_MODE_DIRECT signature verification** (only mode enabled, `chain/app/encoding.go:69`),
   **admittedDecorator** (`:139-169`), increment sequence.
3. admittedDecorator: signer with neither admission row nor seat key: `ERROR_CODE_UNAUTHORIZED`; a transaction any seat
   signs that carries a non-council message: `ERROR_CODE_FORBIDDEN`; runs in CheckTx and FinalizeBlock, before the
   sequence moves (`chain/app/ante.go:123-134`).
4. Unordered transactions are **disabled** (`NewAccountKeeper` without `WithUnorderedTransactions`,
   `chain/app/app.go:199-207`; SDK `x/auth/keeper/keeper.go:118-121,168-169`; rejection SDK `x/auth/ante/sigverify.go:321-323`).
   Every signer has one strict account sequence.
5. Handlers (FinalizeBlock and Simulate only): `msgkit.Enforce` then the role/owner checks in section 5.

Sequence consumption (needed for reconciliation):
- Ante failure (CheckTx or FinalizeBlock): no store write, sequence NOT consumed, result carries no ante events
  (SDK `baseapp/baseapp.go:903-917`).
- Handler failure after a passing ante: ante branch written, **sequence consumed**, result keeps ante events
  (`tx.acc_seq`) (SDK `baseapp/baseapp.go:919-920,722-729`); module state unchanged
  (`docs/architecture/registries.md:40`: "The account sequence still advances when the ante handler succeeded; that is
  not a registry decision.").

---

## 5. Message-by-message signer matrix (code, not the candidate table)

Codes are svn codespace, number = `svn.types.v1.ErrorCode` (`chain/msgkit/codes.go:190-205`; table `docs/protocol.md:108-131`).

| Message | Outer signer rule (violation code) | Inner signature: domain, key, preimage builder | Business digest fields | Object ids | Time bound on the authorization |
| --- | --- | --- | --- | --- | --- |
| `MsgRegisterProvider` | `ADMITTED_PROVIDER` (none UNAUTHORIZED, other FORBIDDEN) (`chain/x/providers/msg_server.go:128`); record.account := signer (`:175`) | none; no proof of possession of `signing_pubkey` (`chain/x/providers/README.md:117-118`) | business_id, capacity, endpoint digest, operator group, provider_id, key id, pubkey; not the signer (`chain/x/providers/README.md:107`) | provider_id (DUPLICATE_ID, `msg_server.go:170-172`) | none |
| `MsgUpdateProvider` | signer == record.account, else FORBIDDEN (`msg_server.go:202-204`) | none | capacity, endpoint digest, provider_id (`README.md:108`) | provider_id | none |
| `MsgRotateProviderKey` | signer == record.account, FORBIDDEN (`msg_server.go:248-250`) | `SVN-PROVIDER-ROTATE-1`, current key, `{"expectedGeneration","newSigningKeyId","newSigningPubkey","providerId"}` (`chain/x/providers/signdoc.go:14,22-30,43-49`) | business_id, signature, generation, new key id/pubkey, provider_id (`README.md:109`) | provider_id + generation | until the generation moves |
| `MsgDrainProvider` | signer == record.account, FORBIDDEN (`msg_server.go:314-316`) | `SVN-PROVIDER-DRAIN-1`, current key, `{"keyGeneration","providerId"}` (`signdoc.go:15,33-38,54-60`) | provider_id, signature (`README.md:110`) | provider_id | until rotation |
| `MsgRegisterContent` | `ADMITTED_SERVICE` (`chain/x/content/msg_server.go:78,348-357`); record.submitting_host := signer (`:122`) | `SVN-HOSTING-INTENT-1`, unregistered key in the message, payload binds `submittingHost` = outer signer and role `DEVNET_TEST_ONLY` (`chain/x/content/intent.go:24-27,42-52,126-152`) | content_id, version, hosting_intent, manifest digest, bytes (`msg_server.go:100-106`) | content_id+version (DUPLICATE_ID, `:113-115`) | **none** |
| `MsgWithdrawContent` | `ADMITTED_SERVICE` (`:186`) AND signer == submitting_host, else FORBIDDEN (`:215-217`) | hosting intent, withdraw payload (`intent.go:26-27,54-63`); not bound to the register intent key (`intent.go:96-97`) | content_id, version, intent, reason digest (`:199-204`) | content_id+version | **none** |
| `MsgOfferLease` | `ADMITTED_SERVICE` (`chain/x/leases/msg_server.go:239`); offer.scheduler := signer (`:362`) | none | 12 fields incl. business_id (`chain/x/leases/README.md:137`) | offer_id (open DUPLICATE_ID `:293-295`; ever used D-4 `:329-331`) | `expiry_unix_seconds` (offer) |
| `MsgAcceptLease` | `BROADCASTER` or `ADMITTED_PROVIDER` (other FORBIDDEN, none UNAUTHORIZED) (`:401-408`); an `ADMITTED_PROVIDER` must be the offer's provider account, FORBIDDEN (`:449-453`) | `SVN-PROVIDER-ACCEPT-1`, provider's current key, over the STORED offer + lease_id (`chain/x/leases/signdoc.go:23-25,44-64,92-98`) | lease_id, offer_id, provider_acceptance; not business_id, not signer (`msg_server.go:418-422`) | offer_id (NOT_FOUND once closed), lease_id (DUPLICATE_ID `:454-456`) | offer expiry (`:458-460`) and deadline D-3 (`:473-475`) |
| `MsgRenewLease` | `BROADCASTER`, or own `ADMITTED_PROVIDER` (other provider FORBIDDEN; no role UNAUTHORIZED; other role FORBIDDEN) (`:549,625-643`) | `SVN-PROVIDER-RENEW-1`, current key, built from the message (`signdoc.go:27,67-76,104-110`) | deadline, evidence_id (if set), lease_id, manifest digest, bytes, signature, term_version (`README.md:139`) | lease_id + term_version | **none** until term moves (STALE_VERSION) or key rotates |
| `MsgEndLease` provider path | `BROADCASTER` or own `ADMITTED_PROVIDER`; any other signer FORBIDDEN (`:695,625-643`) | `SVN-PROVIDER-END-1`, current key, `{"leaseId","providerId","termVersion"}` with STORED provider and current term (`signdoc.go:29-31,79-85,117-123`; D-12) | authority_end, lease_id, signature (`README.md:140`) | lease_id | **none** until renewal (D-12) or rotation |
| `MsgEndLease{authority_end}` | `AUTHORITY` (none UNAUTHORIZED, other FORBIDDEN); a provider_signature is UNKNOWN_FIELD (`:680-686`) | none: "one AUTHORITY signature. It is outside DEC-009" (`docs/protocol.md:239`) | authority_end, lease_id | lease_id | none |
| `MsgSubmitAuditBatch` | `AUDITOR` only; `BROADCASTER` relay FORBIDDEN (`chain/x/evidence/msg_server.go:156-157,203`); inner key must derive the signer, UNAUTHORIZED (D-2, `:162,221`); ids must carry the signer's 8-byte prefix, FORBIDDEN (`:289-296`) | `SVN-EVIDENCE-ATTESTATION-1`, auditor account key, `{"batch_id","envelopes","signer"}` (`chain/x/evidence/signdoc.go:19-49`) | all fields except signer (`msg_server.go:93-118`) | batch_id, evidence_ids (DUPLICATE_ID `:263-273`) | epoch = current 3600 s round, else STALE_VERSION before the gate (`:249-253`); item expiry |
| `MsgProposeChange` | council seat (non-seat UNAUTHORIZED) (`chain/x/authority/engine.go:360-363`); ante FORBIDDEN for seats on other messages | none (the seat signs the outer tx; the record keeps approver keys) | per engine (`engine.go:409-416`) | proposal_id (DUPLICATE_ID `:419-420`) | proposal expiry |
| `MsgApproveChange` | council seat, once (REPLAY on second, `engine.go:474-476,495-496`) | none | per engine (`:478-482`) | proposal_id | proposal expiry |
| `MsgExecuteChange` | `AUTHORITY` (none UNAUTHORIZED, other FORBIDDEN) (`engine.go:526-531`); seat-signed FORBIDDEN at ante (`docs/architecture/authority-authorization.md:13`) | none: "Council signatures are not replaced by this signature." (`:13`) | `{"proposal_id"}` only (`engine.go:535`) | proposal_id | proposal expiry and delay |

Notes on deviations from the candidate relay column:
- **R1** `docs/protocol.md:227,233-234` lets a broadcaster submit the outer tx of RegisterProvider, RegisterContent,
  WithdrawContent. Code refuses: "a broadcaster outer signer is `ERROR_CODE_FORBIDDEN`. Adopting the relay column would
  give every `BROADCASTER` key registry reach." (`docs/architecture/registries.md:30`; `chain/x/providers/README.md:85-88`;
  `TestRegistryMessagesRejectOtherRoles`, `chain/app/registry_roles_test.go:31`).
- **R2** `docs/protocol.md:240` lists "Broadcaster" as relay of SubmitAuditBatch; code and owner Q1 refuse
  (`chain/x/evidence/README.md:277,578-579`; `TestBroadcasterRelayForbidden`, `chain/x/evidence/submit_test.go:363`;
  devnet `broadcaster-relays-auditor-batch`, `chain/devnet/audit_test.go:1005`).

Multi-message transactions are accepted (devnet `commitMulti("accepts", d.broadcaster, accepts...)`,
`chain/devnet/audit_test.go:713-715`); SDK execution is atomic, so one failing message fails all of them
`[Educated Guess: standard SDK runMsgs semantics, not re-read here]`.

---

## 6. Provider consent: where it is embedded and what proves it

Embedded (field of the Msg, separate from the tx signer):

| Message field | Domain | Preimage (UTF-8(domain) `0x00` SVN-JCS-1) | Built from |
| --- | --- | --- | --- |
| `MsgAcceptLease.provider_acceptance` (field 5, `docs/protocol/proto/svn/leases/v1/tx.proto:35`) | ACCEPT | 13 keys incl. `scheduler` (hex of the offering service) and `leaseId` | stored offer + message lease id (`chain/x/leases/signdoc.go:44-64`) |
| `MsgRenewLease.provider_signature` (field 8) | RENEW | 6 keys, `evidenceId` "" when absent | message (`signdoc.go:67-76`) |
| `MsgEndLease.provider_signature` (field 4) | END | leaseId, providerId, termVersion | message lease id + stored provider + stored term (`signdoc.go:79-85`) |
| `MsgRotateProviderKey.current_key_signature` (field 7) | ROTATE | 4 keys | message (`chain/x/providers/signdoc.go:22-30`) |
| `MsgDrainProvider.provider_signature` (field 4) | DRAIN | keyGeneration, providerId | message + stored generation (`signdoc.go:33-38`) |

Exported preimage builders for the broadcaster/agent: `leases.AcceptSignBytes`, `RenewSignBytes`, `EndSignBytes`
(`chain/x/leases/signdoc.go:87-123`), `providers.RotateSignBytes`, `DrainSignBytes` (`chain/x/providers/signdoc.go:40-60`);
golden vectors `chain/x/leases/testdata/sign-vectors.json` (`chain/x/leases/README.md:170-173`).

What proves consent on chain (`chain/msgkit/innersig.go:110-143`):
1. structure: domain known and equal to the expected domain (else `WRONG_DOMAIN`), profile version 1, key id on the
   id pattern, 32-byte key, 64-byte signature (`:74-108`);
2. `key_id` == stored `signing_key_id` and `public_key` == stored `signing_pubkey`, else `UNAUTHORIZED` (`:119-124`);
3. ed25519 verify over `SignBytes(domain, canonical)` (`:125-127,134-143`).
Only the CURRENT key verifies; a retired key, another provider's key, or a service key is `UNAUTHORIZED`
(`chain/x/leases/README.md:159-162`).

What the acceptance binds: "the exact content version, manifest digest, bytes, terms digest, term version, real-time
start and deadline, offer expiry, scheduler, and lease id" (`chain/x/leases/signdoc.go:33-36`).

Limits of that proof (all code-documented):
- **No chain id** in any inner payload (`chain/x/leases/signdoc.go:20-21`; `chain/x/providers/signdoc.go:11-12`;
  DR-07-01, DR-08-04). The outer SIGN_MODE_DIRECT SignDoc binds chain id and account number (`docs/protocol.md:102`).
- **No proof of possession at registration**: "a front-runner can register another provider's key and manifest digest
  under its own account" (`chain/x/providers/README.md:117-118`; DR-07-08). A provider record whose `signing_pubkey`
  is a key the broadcaster holds would let the broadcaster "consent" for that provider.
- **RENEW and END carry no expiry**: an END stays valid until a renewal commits (D-12) or the key rotates; "a pending
  end can sit in a broadcaster outbox" (`chain/x/leases/README.md:164-168`; DR-08-04 security note `:644-649`).
- Signatures become public once in a block, even in a rejected transaction (`chain/x/leases/README.md:164-165`).
- ed25519 (Go `crypto/ed25519`) signatures are deterministic, so re-signing the same preimage with the same key yields
  the same InnerSignature and the same business digest `[Educated Guess: RFC 8032 property; not tested in repo]`.

Provider-side submission paths the chain accepts (none designated "approved"):
- P1 BROADCASTER relays the provider's InnerSignature as outer signer (`chain/x/leases/msg_server.go:405,628-629`).
  The provider API's acceptance route returns "provider signature over the offer; not yet a chain commit"
  (`docs/protocol/api/provider-api.json`, path `/offers/{offer_id}/acceptance`).
- P2 the provider's own `ADMITTED_PROVIDER` account is the outer signer (`:405,449-453,630-634`).
- P3 the broadcaster forwards raw tx bytes the provider account signed (outer signer and sequence stay the provider's;
  the broadcaster only transports bytes) `[Educated Guess: follows from the SDK envelope; no repo test names it]`.
Goal 16 says "Submit or forward acceptance through the approved provider-signing path; a service cannot manufacture it."
(`docs/goals/16-storage-provider-agent.md:22-23`). Which path is "approved" is undecided (ambiguity S3).

---

## 7. "Service signatures are not provider signatures": concrete enforcement points

1. Verification uses only the provider record's key (`chain/x/providers/handoff.go:61-67`): a service key as acceptance
   is `UNAUTHORIZED` (`TestServiceKeyCannotSignAcceptance`, `chain/x/leases/accept_test.go:183`;
   `TestServiceKeyCannotSignAcceptanceInApp`, `chain/app/lease_misc_test.go:282`; devnet `service-key-acceptance`
   UNAUTHORIZED, `chain/devnet/lease_test.go:658`). REQ-CHAIN-012 VERIFIED in goal 08 (`docs/evidence/goal-08.md:205`).
2. A service account as outer signer of an acceptance is `FORBIDDEN` at the role check (`chain/x/leases/msg_server.go:401-408`;
   devnet `service-outer-signer`, `chain/devnet/lease_test.go:659`).
3. One role per address (`chain/x/authority/genesis.go:158`); a BROADCASTER key cannot offer, a service key cannot relay.
4. Domain separation: a signature in another known domain is `WRONG_DOMAIN` (`chain/msgkit/innersig.go:84-86`); the
   hosting intent is its own domain.
5. Provider rotation/drain need the provider's current key; "a service account that sends `UpdateProvider` is
   `ERROR_CODE_FORBIDDEN`" (`docs/architecture/registries.md:34`).
6. `PROVIDER_RECEIPT` evidence is `FORBIDDEN` on chain: "no provider signature exists on chain" (`chain/x/evidence/msg_server.go:297-301`).
7. Opposite rule for auditors: the inner key must BE the outer account key (D-2), so an auditor attestation cannot be
   produced by a relay (`chain/x/evidence/signdoc.go:51-59`).
8. Statement of principle: "A provider signature is not a service signature. A broadcast acknowledgement is not
   committed execution." (`docs/protocol.md:55`); "A broadcaster may relay a transaction it did not author. It cannot
   manufacture the inner signature." (`docs/protocol.md:183`).

What it does NOT cover: the chain cannot tell whether a hosting-intent key belongs to a creator (section 9), and a
provider may register any key, including one a service holds (section 6). Goal 11's REQ-CHAIN-013 fixture
("broadcaster fixture cannot mint a provider signature or skip the publication permission bit",
`docs/requirements.md:452`) therefore needs key custody separation off chain.

---

## 8. Business ids, duplicates and reconciliation

### 8.1 Namespace and gate
- One protocol namespace across `authority`, `content`, `evidence`, `leases`, `providers` (`chain/app/app.go:286-290`;
  `chain/msgkit/business.go:17-70`).
- `Ledger.Gate` (`chain/msgkit/business.go:87-115`): empty id MALFORMED, off-pattern BOUNDS; own ledger then peers;
  absent proceeds; same digest idempotent; other digest `REPLAY`. Never writes. `Commit` only after every check
  (`:117-126`). Authority mirrors it (`chain/x/authority/engine.go:245-268`).
- Id pattern `^[a-z0-9][a-z0-9._-]{0,63}$` (`chain/msgkit/ids.go:29-44`). Business ids are unsigned by any signer
  binding: DR-09-28 asks for signer-bound ids (`docs/architecture/evidence.md:320-323`); DR-10-16 records squatting
  as "a denial of service, never a wrong transition" (`docs/architecture/governance-and-upgrades.md:1025`).
- "A failed attempt does not store the business id." (`docs/protocol.md:185`).

### 8.2 Outcomes and state

| Case | ABCI result | Module state | Other |
| --- | --- | --- | --- |
| Same id, same payload, all pre-gate checks pass | code 0 | no write, no typed event (`chain/x/leases/README.md:105-106`) | one `<module>_result{idempotent="true"}` event (`chain/msgkit/events.go:324-329`; authority `authority_result`, `chain/x/authority/msg.go:119-121`); sequence advances (auth store) |
| Same id, other payload | svn code 3 `ERROR_CODE_REPLAY`, also across modules (`docs/architecture/registries.md:46`) | byte-identical (every key of every mounted store except auth: `docs/architecture/registries.md:40`) | sequence consumed (handler failure) |
| New id, object id already used | svn code 4 `DUPLICATE_ID` (provider_id, content_id, open/used offer_id, lease_id, batch/evidence id, proposal_id; section 5) | byte-identical | sequence consumed |
| Authority keep (EXPIRED / execute FORBIDDEN -> REJECTED) | **code 0** | proposal record and open count written; business id NOT stored (`chain/x/authority/README.md:56-57`) | `authority_result{code=...}` (`chain/x/authority/msg.go:129-132`); DR-06-05 (`docs/architecture/authority-authorization.md:63`) |
| Ante refusal (unknown signer, seat on non-council msg, timeout, wrong sequence) | svn 1/2 or sdk 30/32/42 | nothing written | sequence NOT consumed (SDK `baseapp/baseapp.go:903-917`) |

Tests: `TestLeaseRetryScenarios` (`chain/app/lease_misc_test.go:189`; same id under distinct hashes, one block both
orders and a later block, one reservation, `docs/architecture/leases.md:60`); `TestAuditRetryScenarios`
(`chain/app/evidence_flow_test.go:361`; "full state unchanged", `docs/architecture/evidence.md:217`); devnet phase 3
timeout-and-retry (`chain/devnet/lease_test.go:532-571`).

### 8.3 Retry hazards: an identical resubmission is NOT a reliable status probe
Owner-facing text: "Every check before the business gate runs again on a retry ... Read the committed result by
transaction hash or lease state instead of resubmitting." (`chain/x/leases/README.md:111-119`); goal 11 handoff
(`chain/x/leases/README.md:607-618`; `docs/architecture/leases.md:140`).

| Retry of a COMMITTED action | Answer | Source |
| --- | --- | --- |
| OfferLease by another scheduler | FORBIDDEN (D-11) | `chain/x/leases/msg_server.go:281-292` |
| OfferLease after governed `max_package_bytes` lowered | BOUNDS | `chain/x/leases/README.md:614-615` |
| AcceptLease by another provider's account | FORBIDDEN (D-13); a BROADCASTER replay stays idempotent | `msg_server.go:426-440` |
| AcceptLease under a NEW business id | NOT_FOUND (offer closed) or DUPLICATE_ID (lease id) | `:441-456` |
| RenewLease / EndLease after provider key rotation | UNAUTHORIZED (DR-08-12) | `chain/x/leases/README.md:676-679`; `TestRetryAfterKeyRotation` `chain/x/leases/renew_test.go:292` |
| EndLease after a renewal committed | UNAUTHORIZED (D-12) | `TestEndSignedForEarlierTermD12` `chain/x/leases/end_test.go:198` |
| EndLease with a new business id after release | UNDERFLOW | `msg_server.go:704-706` |
| RegisterProvider copy by another account | FORBIDDEN (DR-07-08) | `chain/x/providers/msg_server.go:158-169` |
| RotateProviderKey retry | UNAUTHORIZED (signed by the retired key); "a broadcaster confirms a rotation by querying the provider" | `docs/architecture/registries.md:46` |
| DrainProvider retry after rotation | UNAUTHORIZED (drain binds key generation) | `chain/x/providers/signdoc.go:17-20`, verify before gate `msg_server.go:310-316` |
| RegisterContent copy by another service | UNAUTHORIZED (intent binds `submittingHost`) | `chain/x/content/intent.go:20-22` |
| SubmitAuditBatch in a later epoch | STALE_VERSION (epoch check before gate) | `chain/x/evidence/msg_server.go:249-262` |
| SubmitAuditBatch by another auditor | UNAUTHORIZED (D-2) | `docs/architecture/evidence.md:217` |
| Any message after the signer's role changed | role failure | `chain/x/leases/README.md:615-616` |

### 8.4 Business ids and object ids available for reconciliation

| Message | Ids the broadcaster holds before submitting | Committed read path (gRPC unless noted) |
| --- | --- | --- |
| Register/Update/Rotate/Drain provider | business_id, provider_id, generation | `/svn.providers.v1.Query/Provider{provider_id}` (`docs/protocol/proto/svn/providers/v1/query.proto`) returns record, account, key id/generation, state |
| Register/Withdraw content | business_id, content_id, version | `/svn.content.v1.Query/Content{content_id, content_version}`: record + tombstone (`chain/x/content/README.md:91`) |
| OfferLease | business_id, offer_id | open offers only, via `Deadlines{not_after}` (includes `scheduler`); closed offers: no gRPC query; used-offer marker `0x0C\|offer_id` raw only (`chain/x/leases/README.md:81,88-92,405-411`) |
| Accept/Renew/End | business_id, lease_id, term_version | held leases only: `ActiveLeases{content_id, provider_id}` (`chain/x/leases/README.md:400-404`); ENDED/EXPIRED lease: raw `0x02\|lease_id` only; DR-08-08 asks for by-id queries (`:664-666`) |
| SubmitAuditBatch | business_id, batch_id, evidence_ids | `/svn.evidence.v1.Query/Evidence{evidence_id}` or `{lease_id}`; batch id: raw `0x02\|batch_id` only (`chain/x/evidence/store.go:40-44`) |
| Propose/Approve/Execute | business_id, proposal_id | `/svn.authority.v1.Query/Governance` with `page_key` = bare proposal id (`chain/x/authority/README.md:164-166`) |
| any | tx hash | CometBFT `/tx?hash=` (default kv indexer, CMT `config/config.go:1460-1464`; not overridden: `chain/node/start.go:211-222`); `cosmos.tx.v1beta1.Service/GetTx` (`chain/app/app.go:669-671`); used by the auditor client `CommittedTx` (`agents/auditor/chainclient/rpc.go:367-384`) |
| any | signer + sequence | `tx_search "tx.acc_seq='<bech32>/<seq>'"`: ante emits `tx.acc_seq` (SDK `x/auth/ante/sigverify.go:139-142`), kept on failed txs (SDK `baseapp/baseapp.go:722-729`), all attributes indexed (no `IndexEvents` set in `chain/`; SDK `types/events.go:311-327`) `[Educated Guess: not exercised by any repo test]` |
| any | business_id | **no gRPC query.** Raw `abci_query /store/<module>/key` on prefix\|\|id returns the 32-byte digest: authority `0x11` (`chain/x/authority/store.go:59-60`), content `0x03` (`chain/x/content/store.go:29`), evidence `0x03` (`chain/x/evidence/store.go:42`), leases `0x03` (`chain/x/leases/store.go:51`), providers `0x02` (`chain/x/providers/store.go:40`); store names `chain/app/state.go:31-38`. Compare with `msgkit.BusinessDigest` (`chain/msgkit/canon.go:120-122`) over the documented field set. Raw store reads are unmetered and unpaged; "CometBFT RPC must not be public" (`chain/x/leases/README.md:692`) |

No typed event carries a business id (`docs/protocol/proto/svn/events/v1/events.proto`); events carry object ids;
committed tx events carry `msg_index` (`chain/msgkit/events.go:191`, `ParseCommitted`). The memo must be empty
(`chain/app/ante.go:75-77`), so no intent id can ride in the envelope.

### 8.5 Tools that make "uncertain" resolvable
- `timeout_height` / `timeout_timestamp` are enforced in CheckTx and FinalizeBlock (`chain/app/ante.go:32`; SDK
  `x/auth/ante/basic.go:205-222`), so an attempt can be made provably dead after a committed height/time.
- Strict sequences: once the signer's committed sequence passes an attempt's sequence and the attempt's hash is in no
  block, that attempt can never commit `[Educated Guess: follows from section 4; confirm with an acc_seq search]`.
- Existing pattern to reuse or mirror: `agents/auditor/chainclient` states NOT_SUBMITTED / NOT_ACCEPTED / SUBMITTED /
  COMMITTED / FAILED / RECORDS_FOUND, "Only COMMITTED says that a transaction carrying the batch is in a block with code
  0; an acknowledgement is never reported as anything but SUBMITTED" (`agents/auditor/chainclient/submit.go:28-56`),
  `broadcast_tx_sync` (`rpc.go:346-356`), rebroadcast refusal `sdk/32` while an earlier tx holds the sequence
  (`submit.go:88-93`).
- Spec stall rule: "The broadcaster, not a chain handler, records `PENDING_STALL` in its outbox." (`docs/protocol.md:260`);
  client operation states "requested, submitted, committed, failed, and pending while the chain is stalled"
  (`docs/protocol.md:217`). The spec outbox returns `ERROR_CODE_QUORUM` (`docs/evidence/goal-02/cases.py:64-77`); the
  real chain returns nothing for a stall, so QUORUM is a broadcaster label, never a chain result `[Educated Guess]`.
- Reconciliation fields the protocol says to retain: "business id, transaction hash, message type, height, committed
  time, object id, digest, byte count, result code, reservation delta, and the idempotent flag" (`docs/protocol.md:272`).

---

## 9. DEC-001 (host custody) implications

- Approved default: the account host holds a delegated signing key for beta public records, with disclosure;
  operators hold chain credentials; passkey is not a Cosmos key (`docs/decision-register.md:37,43`).
- Protocol v0 encoding: "Gate A accepted disclosed host custody. `ACCOUNT_HOST_DELEGATE` is rejected. Only the labeled
  devnet role `DEVNET_TEST_ONLY` can pass, and only with the devnet flag." (`docs/protocol.md:140`).
- Chain behaviour: a valid intent signature with role `ACCOUNT_HOST_DELEGATE` is `FORBIDDEN`; without the DEC-001 devnet
  parameter every RegisterContent/WithdrawContent is `DECISION_BLOCKED` (`chain/x/content/intent.go:113-118,134-143`);
  "Every non-devnet chain is `DECISION_BLOCKED`. Production content registration waits for DEC-001."
  (`chain/x/content/README.md:60`).
- What the intent proves: "A valid signature proves the payload was signed by that key; it does not prove a creator
  authorized the host, because no intent key is registered on chain and DEC-001 key custody is unset."
  (`chain/x/content/intent.go:29-33`).
- Goal 07 carries this as BLOCKED, security-critical DR-07-01: production needs "a hosting-intent record ... its schema,
  the key that speaks for a creator, a chain binding, and a binding of the withdraw intent to the register intent ...
  [and] an approved on-chain DEC-001 value" (`docs/evidence/goal-07.md:10`).
- Stale text: the threat model still says "A host-held signing key exists only if DEC-001 is later accepted"
  (`docs/threat-model.md:18`), although DEC-001 is APPROVED.
- Consequences for goal 11:
  1. On devnet the service account plus any locally generated ed25519 key can register content; the broadcaster could
     therefore "manufacture creator intent" if it holds an intent key. The publication-permission check
     (REQ-CHAIN-013, `docs/requirements.md:452`; blueprint p. 8 "must not be able to manufacture provider acceptance or
     bypass a user's publication permissions", `docs/blueprint.txt:385`) can only be an off-chain object-permission
     check before the outbox insert `[Educated Guess]`.
  2. Viewers never hold chain keys; goal 11's authenticated internal endpoints carry a user/session principal as
     "authorized request context", never a chain key (`docs/requirements.md:211`; goal spec item 1).
  3. Production RegisterContent cannot commit; a goal-11 production path for content would be BLOCKED by DEC-001/DR-07-01.

---

## 10. Implications for the broadcaster's key custody ("approved development signing boundary")

No document defines a "development signing boundary" (search in section 2). Facts that constrain it:
- Dev keys are fresh, generated at runtime, 0600, never in Git or logs (AGENTS.md; `docs/environment.md:60,84,96`;
  goal 04 evidence `docs/evidence/goal-04.md:52,184`). The auditor key loader is a working pattern: refuses symlinks,
  group/other-readable files, wrong format; never prints key bytes (`agents/auditor/keys/keys.go:1-21,41-52,77-80`).
- Key classes must stay distinct: "a provider key, a service broadcaster key, a validator consensus key, a peer identity
  key, and a governance key. Theft of one class is not theft of the others" (`docs/threat-model.md:93`); REQ-SEC-002
  (`docs/requirements.md:543`).
- Devnet puts every account and seat key in one directory (`chain/devnet/init.go:104,223,236`).

Recommendations, all `[Educated Guess]`, for the owner to confirm (see S2):
- The broadcaster loads only `BROADCASTER` and `ADMITTED_SERVICE` keys, by explicit configured name, and refuses seat,
  AUTHORITY, AUDITOR and any ed25519 provider signing key.
- Provider signing keys live in a separate process (goal 16 agent or a goal-11 provider stub) that returns
  InnerSignatures; the broadcaster verifies them locally with the same preimage builders before queuing.
- Per signer one sequence allocator; one key = one role is already enforced on chain.
- Chain id is explicit configuration checked against the node's genesis chain id before any signing (the SignDoc binds it).

---

## 11. Security-critical ambiguities (decision candidates)

- **S1 Prerequisite unmet.** No approved service/provider signing-boundary document (section 2). Options: owner approves
  a pinned boundary document for goal 11 (DR-10-00 precedent); or goal 11 runs as BLOCKED on that prerequisite.
- **S2 Which keys the broadcaster may hold.** Candidate text gives the broadcaster relay rights only (BROADCASTER), but
  goal 11 also needs scheduler/content messages (ADMITTED_SERVICE). Holding the AUTHORITY key would let a compromised
  broadcaster execute any QUEUED proposal at a time it picks and end ANY lease with one signature
  (`authority_end`, "outside DEC-009", `docs/protocol.md:239`). Holding 3 of 4 seat keys collapses DEC-008. Holding an
  auditor key breaks the Q1 "no relay" intent. Decide: BROADCASTER only, BROADCASTER+SERVICE, or more.
- **S3 Approved provider-side submission path** (section 6, P1/P2/P3). Goal 11 item 2 and goal 16
  (`docs/goals/16-storage-provider-agent.md:22-23`) both name "the approved provider-signing path"; none is approved.
  Also: who chooses `lease_id` (the provider signs it) and whether the broadcaster may withhold or delay a signed
  acceptance (censorship until offer expiry; provider fallback is P2).
- **S4 Hosting intent / publication permission (DEC-001, DR-07-01).** Who holds the devnet intent key in goal 11, and
  whether the API must prove a creator principal authorized each RegisterContent before outbox insert. On chain any key
  passes on devnet.
- **S5 Outbox retention of unbounded authorizations.** RENEW/END signatures and content intents have no chain expiry
  and no chain id (sections 5-6). Needs an outbox expiry rule, a revocation story (only key rotation or renewal revokes),
  and a rule that signatures for another chain id are refused. DR-08-04 already names the END risk.
- **S6 Business-id generation and squatting.** Unsigned cross-module namespace; any admitted account can take a pending
  id (DR-09-28, DR-10-16, DR-07-08, D-11). Decide: random ids per business attempt (as the auditor does,
  `docs/architecture/evidence.md:322-323`) vs ids derived from the intent id; and how REPLAY / FORBIDDEN on retry map to
  outbox states (they can mean "someone else committed it").
- **S7 Reconciliation read path.** No gRPC query by business id, lease id, closed offer id or batch id (DR-08-08). Raw
  `/store` reads are unmetered and need private node RPC. Decide whether the broadcaster may use raw store reads, or
  must rely on tx hash + acc_seq search + object queries, or whether a protocol addition is requested.
- **S8 False success on authority messages.** Keep outcomes return ABCI 0 with `authority_result{code}` (DR-06-05). If
  the broadcaster submits authority messages at all, "committed" must require no `authority_result` code.
- **S9 Service key rotation orphans ownership.** `RotateServiceSubmitter` (engine-only) moves the admission row but
  content `submitting_host` and offer `scheduler` keep the old address (`chain/x/content/msg_server.go:215-217`;
  `chain/x/leases/msg_server.go:189-192`), so the new key cannot withdraw that content or get the D-11 idempotent replay.
  Goal 26 rehearses service-key rotation (`docs/goals/26-backups-restoration-and-signer-safety.md:27`). The outbox must
  pin the signer per intent.
- **S10 Batching.** Multi-message transactions are accepted and atomic; batching unrelated business intents couples
  their failure. Decide whether the broadcaster may batch.
- **S11 Stall honesty.** `broadcast_tx_sync` returns a local CheckTx answer even when no block can commit; the API must
  stay "submitted/pending" (REQ-ACK-001, `docs/requirements.md:483`). Not ambiguous in principle, but which state name
  (`PENDING_STALL` vs pending) and how long before `failed` is unset.

Non-security reversible choices (record, do not ask): outbox state names beyond protocol.md:217; per-module business
field-set replication in the broadcaster; whether to mirror the auditor chainclient state names.

---

## 12. Goal-11 requirement rows touching signing (`docs/requirements.md`)

- REQ-CHAIN-013 (`:452`): broadcaster fixture cannot mint a provider signature or skip the publication permission bit.
- REQ-ACK-001 (`:483`): ack stays submitted until inclusion and execution succeed; reconcile a timeout before retry.
- REQ-INT-001 (`:496`): goal 11 is the only chain writer the API uses (the auditor producer remains a separate writer by
  Q1 `[Educated Guess: REQ-INT-001 scopes "the API", not agents]`).
- REQ-IDEM-001 (`:500`), REQ-QA-008 (`:594`): one business effect and no leaked reservation across timeout and restart.

---

## 13. What was verified and how

- Repository facts: read at `dbf0897` in `wt-11` (files cited above). No test or build was run.
- Framework facts checked in the module cache: SDK v0.55.0 error codes (`types/errors/errors.go:15,66,100,107,147`),
  ante timeout checks (`x/auth/ante/basic.go:205-222`), unordered-tx gate (`x/auth/keeper/keeper.go:118-121,168-169`;
  `x/auth/ante/sigverify.go:321-323`), runTx ante/handler branches (`baseapp/baseapp.go:722-729,903-920`), event indexing
  default (`types/events.go:311-327`), auth `MsgUpdateParams` signer option (`proto/cosmos/auth/v1beta1/tx.proto:18,26`);
  CometBFT v0.40.0 default kv tx indexer (`config/config.go:1460-1464`) and mempool duplicate cache
  (`mempool/clist_mempool.go:269`).
- Not verified: the exact RPC error text a duplicate broadcast returns; `tx_search` on `tx.acc_seq` against this app;
  atomicity of multi-message txs (standard SDK, not re-read).
