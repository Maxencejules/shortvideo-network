# Moderation, blocking and removal

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. No person is assigned to trust and safety, product, chain security, media, or deployment. The contact lines are placeholders. This page is not legal approval, not an incident review, and not a rights review.

The behavior is described in [moderation.md](../architecture/moderation.md#local-delist).

## Open a report

A signed-in account sends `POST /v1/reports` on the discovery process with a target type, a target id, a category, and a narrative. The response does not echo the narrative. A repeat from the same account on the same target is a spam report and does not open a second case. Another account cannot read the case.

## Decide, appeal, escalate

An account is a moderator only when the process was given that role. A stranger receives a refusal and no case file. A moderator reads the queue, records a reason code, and the narrative is then dropped. The subject may appeal. The appeal statement is not returned to the subject and is dropped when the appeal is decided. Escalation status moves from requested to escalated. The appeal authority line stays `PLACEHOLDER unassigned`.

## Block

`POST /v1/blocks` stores a block for the signed-in account. Feeds, search, comments, likes, and new follows between those accounts stop. The block is not a chain transaction.

## Remove

`POST /v1/removal` with confirm `withdraw` is the creator request. It hides the id, bumps the discovery cache generation, and asks each configured cooperating endpoint. Upload admission, publication, a queued encode, repair, and playback each refuse the id without waiting for a new committed height. `POST /v1/policy/delist` on the upload process and `POST /v1/delist` on the scheduler and delivery processes are the local admission stops. A provider `POST /admin/delist` refuses a later offer for that id.

## Say what remains

The response says the deletion is not universal. An unreachable controlled host and an unresponsive independent host stay incomplete. Historical ledger records stay. DEC-004 sets no duration, so only the minimum rows above are kept. Restart of these in-memory discovery rows loses them; that persistence is not available in this build.

## Do not

<!-- never-list -->
- Never describe this page as legal approval or as a minor-safety claim.
- Never tell a viewer that an independent host deleted a copy when that host did not answer.
- Never wait for a stalled chain before hiding an item on a service this operator runs.
<!-- /never-list -->
