# Goal 11 owner questions as asked (AskUserQuestion), answered 2026-11-08T07:44Z

Asked through the Claude Code AskUserQuestion dialog. Selections are the owner's; the option text is what the owner saw.

## Q1 header "Keys"
Question: "Goal 11 broadcaster (a new service that turns other services' requests into chain transactions): which chain keys may it hold? On chain, ADMITTED_SERVICE may register/withdraw content and make lease offers; BROADCASTER may only relay a provider's own signed accept/renew/end. No council-seat, AUTHORITY, auditor, provider or user key is held under any option, and no on-chain message can revoke these keys today."
Options: "Both keys, separate signer (Recommended)" | "Both keys, in API process" | "BROADCASTER key only"
SELECTED: "Both keys, separate signer (Recommended)"
Option text: "One BROADCASTER key and one ADMITTED_SERVICE key, read only by a separate signer process running as its own non-root user. The signer checks message type, chain id, deadline and (for content) an operator-registered intent key. An API/database bug can't reach key bytes. CI can only show the uid split in-memory; the real split is verified in a root run."

## Q2 header "Provider sig"
Question: "Goal 11: which externally signed submissions may the broadcaster carry? A provider's consent is its ed25519 signature over the exact lease terms (checked on chain; no chain id, and renew/end signatures never expire). You ruled in goal 09 'No BROADCASTER relay' for audit batches."
Options: "Relay + verbatim forward (Recommended)" | "Relay only" | "Also forward auditor batches" | "Never carry provider consent"
SELECTED: "Relay + verbatim forward (Recommended)"
Option text: "Relay: provider returns its signature, broadcaster relays it after checking it against the provider's registered key. Forward: broadcaster forwards a provider-signed transaction byte-for-byte only if it carries a short deadline. Provider self-submission arrives in goal 16. Audit batches stay on the auditor's own path (not through the outbox)."

## Q3 header "Publish bit"
Question: "Goal 11: REQ-CHAIN-013 says the broadcaster must not 'skip the publication permission bit', but nothing defines that bit, and the chain cannot tell whether a creator authorized a hosting intent. How should the broadcaster decide who may publish or withdraw content (confused-deputy protection)?"
Options: "Operator-registered intent keys (Recommended)" | "Trust caller claim" | "Community approval flag"
SELECTED: "Operator-registered intent keys (Recommended)"
Option text: "Each caller principal gets PUBLISH/WITHDRAW permissions; the hosting intent must be signed by the key an operator registered for that principal in a file the broadcaster can't write (a devnet fixture holds those keys, as a test stand-in, not DEC-001 custody). Ownership counts only after the chain confirms our registration; other callers' objects answer 'not found'."

## Q4 header "Outbox data"
Question: "Goal 11: the outbox must persist an 'authorized request context'. Stored rows would include an opaque caller principal id (links a creator to content), authorization context, and raw provider signatures/hosting intents (reusable: renew/end signatures never expire on chain). Emails, IPs, tokens and free text are refused at the API. DEC-004 sets no retention period. How minimal should it be?"
Options: "Minimal + clear unsent (Recommended)" | "Minimal, keep everything" | "Minimal + delete after period"
SELECTED: "Minimal + clear unsent (Recommended)"
Option text: "Only the minimal fields; answers return digests, not raw signatures. Relayed renew/end signatures get a proposed local expiry. When an intent fails without anything ever being sent, its stored signatures, intent and full auth context are cleared (digests kept). Other rows kept; retention stays unset."

Not asked as questions (reversible implementation choices stated in the plan, 17.1 notes): Go service at services/broadcaster replacing the Python scaffold; CI PostgreSQL install and new shards; PROPOSED numbers. The boundary document approval (DR-11-01) and any PR/merge are asked separately at PR time. The owner's standing instruction of 2026-11-08: "I authorize you to merge it and complete the next goals" ("it" = PR 15).
