# Goal 11 (transaction broadcaster and outbox): four questions before implementation

Goal 11 builds `services/broadcaster`: a service that takes typed requests from other services, stores them in a PostgreSQL
outbox, signs and submits chain transactions, and reports requested, submitted, committed, failed or pending status only from
what the chain committed. These four choices decide which keys the service holds, whose signatures it may carry, who may ask
it to publish or withdraw content, and what personal or reusable data it keeps. Work that does not depend on them starts
now (the database layer, the chain client, the state machine, chain-level deadline tests, the documentation draft). The
rest waits for your answers; nothing answer-dependent is built as an interim. Each recommendation is my proposal, not a
decision. You may combine options. Paths are in the repository at `goal-11` (`6d74aca`). The full plan is
`scratchpad/goal-11/plan.md` (sections 7 and 17).

Under every option: the service never holds a council seat, AUTHORITY, AUDITOR, provider, validator, creator or user key;
it never signs a provider's consent or a hosting intent; it never reports COMMITTED from a broadcast acknowledgement; and
everything runs on the single-machine devnet only.

---

## Q1. Which keys may the broadcaster hold, for which messages, and behind which development boundary?

Facts: whoever holds a key can send everything its chain role allows, and no message can revoke a service or broadcaster
key on chain today. The chain lets an `ADMITTED_SERVICE` account register and withdraw content and offer leases
(`chain/x/content/msg_server.go:78,186`; `chain/x/leases/msg_server.go:239`), and a `BROADCASTER` account relay a provider's
signed acceptance, renewal or end (`chain/x/leases/msg_server.go:405,628-630`); a `BROADCASTER` may not sign content
messages (`chain/x/content/README.md:39`). A withdraw is an irreversible tombstone (`docs/protocol.md:234`), and the chain
accepts a withdraw intent signed by any key with the devnet test role, not only the key that registered the content
(`chain/x/content/README.md:63`). The goal's prerequisite names "approved service/provider signing boundaries", and no such
document exists (`docs/decision-register.md:23`). This workspace runs as root; two processes get real isolation only if
they run as two different non-root users, which CI's single runner user cannot do.

- **(a) Recommended: one `BROADCASTER` key and one `ADMITTED_SERVICE` key in a separate signer process.**
  The `BROADCASTER` key relays provider-signed accept, renew and provider end; the service key registers and withdraws
  content and makes offers. A separate `signer` process alone reads the key files and runs as its own non-root user,
  different from the API process. Before signing it checks: an allowed message type for that key, the message's signer field,
  no `authority_end`, the chain id, a bounded deadline (it reads the node's height itself), and, for register and withdraw,
  that the hosting intent's public key is in an operator-written registry and its signature verifies. One message per
  transaction. The boundary document goal 11 writes is approved by you at PR time on a pinned commit and sha256, as you did
  for goal 10.
  Why: least privilege per key, and an API or database bug cannot reach key bytes or make the service tombstone content
  with an intent key it made up.
  Limits I must state: a compromised API process can still make the service key send any offer the chain accepts, relay any
  stored provider signature, and withhold or reorder submissions. Whoever steals the service key bytes themselves can still
  register content with a self-made intent key, because the chain checks only the devnet role; so the "publication
  permission" half of REQ-CHAIN-013 holds for the broadcaster's code path, not against a key thief. In CI (one user) the
  process split separates key bytes in memory only; the boundary is verified only from a root run with two users, and the
  evidence says which run showed what.
  Implementation: two key classes with type-URL allowlists, the signer process with the checks above, a uid launch in the
  devnet test, and refusal tests for every other message type, key role and peer.
- **(b) As (a), but the keys live inside the API process.**
  Why consider it: one process fewer. Cost: any bug or compromise of the API process reaches the key bytes and skips the
  signer's checks.
  Implementation: the same checks as a package inside `serve`; no uid separation is tested.
- **(c) `BROADCASTER` key only.**
  Content registration, withdrawal and offers leave goal 11; another service, not built here, would sign them. The chain
  itself then refuses content messages from the broadcaster's key, so the publication permission is enforced by chain role
  for this key.
  Why consider it: the smallest key set and a chain-enforced limit. Cost: goal 11 cannot show content and offer submission;
  those acceptance paths and the service-key tests are not built, and goal 13+ needs a second signer.
  Implementation: only the three relay kinds, the same signer process and checks.

## Q2. Which externally signed submissions may the broadcaster carry (provider consent and audit batches)?

Facts: the goal says "Providers sign their own acceptances; support forwarding their valid signed transactions or the
approved provider-side submission path", and goal 16 also names an "approved provider-signing path" (goal spec :21-23;
`docs/goals/16-storage-provider-agent.md:22-23`); none is approved. A provider's consent is an ed25519 signature by its
current registered key over the exact offer terms; the chain verifies it, and it has no chain id and, for renew and end, no
expiry (`chain/x/leases/README.md:146-168`). Signed transaction bytes without a deadline can commit at any later time. For
audit batches you ruled in goal 09 "No `BROADCASTER` relay" (`docs/evidence/goal-09.md:74`), and goal 09 left "Outbox
submission: Goal 11" open (`docs/evidence/goal-09.md:39`). Your ruling forbids a broadcaster-signed batch; whether an
auditor-signed batch may pass through the outbox unchanged is my reading, not yours, so I ask.

- **(a) Recommended: relay (P1) and verbatim forwarding (P3) now; provider self-submission (P2) in goal 16; audit batches
  stay on the auditor's own path.**
  P1: the provider returns its signature and the broadcaster relays it as `BROADCASTER` after checking it against the
  provider's current registered key. P3: the broadcaster forwards a provider-signed transaction byte for byte, only if it
  carries a deadline within a proposed window. P2: the provider sends its own transaction; goal 11 only recognises such an
  effect when it lands first. The auditor keeps submitting its own batches; goal 09's open item is closed as "not through the
  outbox".
  Why: both paths the goal names are covered; the broadcaster can never alter consent, only delay it, and P2 is the
  provider's remedy against delay.
  Implementation: relay kinds with the signature pre-check, a forward kind with strict decoding and the deadline rule, tests
  that every forged or retired-key signature is refused before and by the chain.
- **(b) Relay (P1) only; P2 in goal 16; audit batches on the auditor's path.**
  Why consider it: one path fewer to secure. Cost: providers cannot hand over a ready signed transaction.
  Implementation: as (a) without the forward kind.
- **(c) As (a), plus verbatim forwarding of AUDITOR-signed audit batch transactions.**
  The broadcaster holds no auditor key; the chain still requires the auditor as signer.
  Why consider it: one submission and status path for operators. Cost: the outbox then stores audit batches and their
  retries, and goal 09's auditor status path and the outbox can disagree.
  Implementation: a forward kind for batches with the P3 rules.
- **(d) P2 only: the broadcaster never carries provider consent.**
  Why consider it: no consent passes through a service at all. Cost: no relay; every provider must submit from its own admitted
  account; goal 11's relay tests become reconciliation-only.
  Implementation: no relay or forward kinds; reconciliation of provider-submitted effects only.

## Q3. What is "the publication permission bit", who holds the devnet hosting-intent keys, and which object permissions apply?

Facts: REQ-CHAIN-013 tests that the broadcaster "cannot ... skip the publication permission bit"
(`docs/requirements.md:452`), and nothing defines that bit. On chain, content registration needs a hosting intent, but the
chain cannot tell whether a creator authorized it: "It does not prove a creator authorized the host"
(`chain/x/content/README.md:61`); off the devnet it is blocked by DEC-001. The broadcaster acts with its own chain authority
for many callers, so it must check per caller what each may do (a confused-deputy risk). A second service account on the
devnet can register a content id or copy a pending offer first (`chain/devnet/init.go:138`;
`chain/x/leases/msg_server.go:280-294`).

- **(a) Recommended: an operator-registered intent key per principal; the broadcaster holds none; ownership counts only after
  the chain confirms it.**
  The caller's authenticated request names an opaque principal and grants it `PUBLISH_CONTENT` or `WITHDRAW_CONTENT`. The
  hosting intent must be signed by the key an operator registered for that principal in a public-key file the broadcaster
  cannot write. A separate devnet fixture stands in for the account host and holds the intent keys; this is a test stand-in,
  not DEC-001 custody. A content id belongs to a caller only after its registration committed under our service account and
  the chain shows us as host; only that caller and principal can withdraw it or schedule offers for it; relays need a
  provider bound to the caller or an offer the caller scheduled and that committed under our account. Another caller's object
  answers "not found", and a taken content id gets one uniform answer.
  Why: the strongest check available without a chain change; a broadcaster bug or a hostile caller cannot publish or
  tombstone for someone else.
  Implementation: the registry file, the ownership states, the signer's registry check (Q1 (a)), and devnet tests for
  missing permission, foreign intent keys, another principal's withdraw, a front-run content id and a copied offer.
- **(b) As (a) without the registry: the broadcaster trusts the caller's permission claim plus any valid devnet intent
  signature.**
  Why consider it: no operator file to manage. Cost: any caller with the kind can publish with an intent key it made up; the
  bit becomes the caller's word.
  Implementation: as (a) minus the registry checks.
- **(c) The bit is the off-chain community publication approval (DEC-003): the broadcaster refuses offers for content
  without that approval; intents as in (b).**
  Why consider it: it matches the protocol's "publication approval" status (`docs/protocol.md:195`). Cost: no approval
  service exists until a later goal, so goal 11 would test against a fixture flag; registration itself stays unguarded.
  Implementation: an approval-status input on offers, fixture-backed in goal 11.

## Q4. What may the outbox store, and how is it minimized?

Facts: the goal asks the outbox to persist an "authorized request context". The protocol lists the reconciliation fields it
retains: business id, transaction hash, message type, height, committed time, object id, digest, byte count, result code,
reservation delta and the idempotent flag (`docs/protocol.md:272`). Goal 11 needs more. These stored items are person-linked
or reusable authorizations:
- the caller principal and the opaque `principal_id` (in each request and in content ownership rows): link a creator to
  content; read by the broadcaster and the owning caller; returned to that caller only;
- the authorization context (principal kind, permissions, policy reference): read by the broadcaster; returned only as a
  digest;
- the intent-key registry (principal to public key): an operator file; public keys only;
- the stored request and transaction bytes: contain provider signatures and hosting intents, which carry no chain id, and
  renew/end signatures never expire on chain, so a leaked row can be submitted until the provider renews or rotates;
  returned only as digests;
- clipped node answers and transition observations: operational, no personal fields by construction.
Emails, IP addresses, tokens, headers and free text are refused at the API. DEC-004 sets no retention period
(`docs/decision-register.md:127`), and DEC-007's privacy assessment is not done (`:217`). Opaque ids are still linkable.

- **(a) Recommended: the minimal fields above, digests in answers, local expiry for relayed signatures, and clearing of
  reusable authorizations from rows that ended without any send.**
  Renew and end relays get a proposed local expiry and accept relays are refused after the offer expires (this revokes
  nothing on chain; the README says so). When an intent ends FAILED with nothing ever sent (cancelled, invalid,
  unauthorized, expired unsent, bad provider signature), its stored inner signatures, hosting intent and full authorization
  context are cleared and their digests kept. This is data minimization, not a retention period; it is my proposal. Every
  other row is kept with retention unset.
  Implementation: the field allowlist, the local expiries, a clearing step in the terminal transition, tests that refused
  fields write nothing and that cleared rows keep only digests.
- **(b) As (a) without the clearing step.**
  Why consider it: full history for debugging. Cost: unsent provider signatures and intents stay in the database until a
  retention period exists.
  Implementation: as (a) minus the clearing step.
- **(c) As (a) or (b), plus automatic deletion after a period you set.**
  Goal 11 will not invent the number. Cost: deletion of reconciliation rows can conflict with the protocol's retained-fields
  list, so the period would apply only to the extra items above.
  Implementation: a deletion job for the named columns after your period, with a test at the boundary.

---

Notes, not questions (decided as reversible implementation choices unless you object):
- The service is written in Go at `services/broadcaster`, replacing the Python scaffold and its one scaffold test and path
  pin. This needs one line in goal 01's repository checker (`services/broadcaster/` added to the allowed Go source paths),
  a goal 01 contract change caused by the language choice.
- CI gains a PostgreSQL install in the Go job and three new race-test shards; new required check names need your branch
  protection update.
- Retries, deadlines, caps and stall windows are proposed numbers with hard bounds in code; you are asked to accept the
  labels, not the values.
- Approval of the boundary document and any goal 11 PR or merge are asked separately, at PR time.
