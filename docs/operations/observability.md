# Observability, quotas and usage controls

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. No person is assigned to deployment, chain security, media, product, or trust and safety. Numeric thresholds in `svn-observe` are placeholders. They are not an allowance and they are not a spend cap. Alerts do not admit or refuse work by themselves.

## What the process records

`svn-observe` listens on a loopback address. The operator token comes from the environment. Health and readiness are public. Metrics, logs, the usage report, and the worksheet require that token. The token is not accepted in a query string.

Correlation ids are random. An address or an account id supplied in that header is replaced. The id is not written to the chain. Watch events are not written to the chain. Playback notes kept by this process are aggregates.

Logs are redacted and the buffer drops the oldest lines when the entry or byte cap is reached. A synthetic canary, a bearer value, an address, and an account id are removed before a line is kept.

## Probes

Readiness is false until the chain, the indexer, and the database have each reported usable. The chain becomes usable only from a commit-age reading inside the placeholder bound and a quorum reading that is held. Marking the chain dependency usable without those readings is refused.

A stale commit and a lost quorum are different readings from index lag. Index lag alone does not mark the chain unusable. A lost quorum does. An unusable critical dependency fails readiness. Liveness can still succeed.

## Admission

Uploads, login, search, reports, discovery, audit batches, and retries use a placeholder per-minute cap. Processing concurrency uses a placeholder running cap and a placeholder queue. Output bytes and storage reservations are hard caps that do not reset when the minute changes. A repeated idempotency key does not consume a second unit. After a grant is released, that same key does not open a new grant.

Slow processing stops new processing admission until the age falls. Low disk stops new storage reservations. A repair backlog stops new storage reservations and does not add delivered bytes. A stale commit or a lost quorum stops new audit batches and retries. Clearing the reading allows admission again. These stops are capacity controls. They are not a budget.

## Alerts and roles

Each alert names a runbook and an unassigned role. Nobody is on call.

| Runbook | When it is raised | Unassigned role |
| --- | --- | --- |
| RB-STALE-COMMIT | Commit age is over the placeholder bound | chain/security |
| RB-QUORUM | Quorum is lost | chain/security |
| RB-INDEX-LAG | The index is behind the commit | deployment/on-call |
| RB-REPLICAS | A replica reading is zero | media/provider agents |
| RB-REPAIR | A repair backlog is open | media/provider agents |
| RB-PROCESSING | Processing age is over the placeholder bound | media/provider agents |
| RB-DISK | Free disk is under the placeholder floor | deployment/on-call |
| RB-PLAYBACK | A playback failure was recorded | product/application |
| RB-REPORTS | Unreviewed reports are above zero | trust/safety |
| RB-ADMISSION | An intake stop refused work | deployment/on-call |

The response is to read the matching section below, confirm the reading, and use the intake control that the process already applied. Do not treat the alert as permission to spend. Do not invent a rate or a monthly bill. The worksheet is [cost-worksheet.md](cost-worksheet.md).

## RB-STALE-COMMIT

Confirm the commit-age reading. New audit batches and retries stay stopped while the chain is unusable. Reads and playback notes can continue. Readiness stays failed. When a later reading is inside the placeholder bound and quorum is held, admission of those chain-bound kinds resumes.

## RB-QUORUM

Confirm this is quorum loss and not only index lag. The diagnosis string names `quorum_loss` on its own when the index is not behind. Keep new audit batches and retries stopped. Do not report a commit that the chain did not make.

## RB-INDEX-LAG

Confirm the indexer process is up and the lag is a height gap. Readiness can stay successful. Do not describe the gap as quorum loss. The response is to let the indexer catch up. This page does not start an indexer.

## RB-REPLICAS

A zero replica reading raises the alert. It does not set an approved replica target. Repair remains a separate reading.

## RB-REPAIR

A failed repair increases the backlog and does not increase delivered bytes. New storage reservations stay stopped until the backlog reading is cleared. Clearing the backlog is an operator reading, not a claim that bytes were copied.

## RB-PROCESSING

New processing admission stops while the age is over the placeholder bound. Work already granted can still be released. When the age falls, new admission resumes. The queue, when used, does not grow past its placeholder depth.

## RB-DISK

New storage reservations stop while free space is under the placeholder floor or under the requested size. An absent disk reading is not treated as zero. When a later reading is above the floor, admission can resume if the repair backlog is clear and the placeholder byte cap has room.

## RB-PLAYBACK

Playback failures are a counter. The retained note has no account and no watch history. Do not copy a watch event into a chain transaction.

## RB-REPORTS

Unreviewed reports are a gauge. The complaint text is not copied into this process. Coverage for trust and safety is unassigned.

## RB-ADMISSION

Look up the refusal reason. `hard_stop` means the placeholder cap is exhausted. `intake_stopped`, `low_disk`, `repair_backlog`, and `chain_unusable` are the fault controls above. `already_released` means the same idempotency key must not be used for a new grant. None of these reasons is a spend cap.

## Unmeasured classes

Protocol overhead, relay traffic, record-layer bytes, chain gossip, interface bandwidth, and the scheduler job queue are not counted. A missing class is not a zero and it is not a saving. Interface bandwidth and the scheduler job queue are the open readings from the operator dashboard.

## Decisions still open

The budget owner, the allowance numbers, the rate card, the currency approval, the on-call assignment, and trust-and-safety coverage are unset. Hostname resolution for the fetch check is unset, so names are refused and IP literals are checked. Local quotas from the operator dashboard remain process configuration.
