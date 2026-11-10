# Goal 17 evidence

Status: BLOCKED

`services/scheduler` is an off-chain coordinator. It proposes hosts from admitted indexer rows, declared capacity, a compatible policy, and a bounded reachability probe. Providers sign their own acceptances. The scheduler does not sign and does not broadcast. Publication is withheld until community approval, a verified byte transfer, and three active operator-distinct commitments are all present. Reachability, evidence mode, and committed lease state are separate fields. The goal stays BLOCKED. DR-17-01 through DR-17-08 are open. DEC-001 and DR-07-01 stay open. Every provider process in the conformance log is on this host. That is one physical failure domain. Declared operator labels are simulation. Public launch: DISABLED. No tradable token. This is not an audit and not a production network. No video bytes and no personal data were submitted to a chain. `chain/` and `proto/` are not in the commit file list.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/17-replication-monitoring-and-repair.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-01294149-87eb-5109-ad61-528a07f96615`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `b7c1ed19891c297c9af9bea4afe646c64ac870c7` |
| Tree | `f5621f62d5dedbf2fa8d44e306fdf76d0aa3dbf2` |
| Base | `25654220f70e99f7dcc7cc16f9cde5474668c9c8` |
| Branch | `goal-17` |

[trailer.txt](logs/goal-17/trailer.txt) line 16 is `commit b7c1ed19891c297c9af9bea4afe646c64ac870c7`. Lines 17 and 18 name `Maxence Jules <Maxencejules@users.noreply.github.com>` as author and committer. Line 24 is `Tested-tree: f5621f62d5dedbf2fa8d44e306fdf76d0aa3dbf2`. The same file's `TREE` line is that tree. This evidence commit changes only `docs/evidence/` relative to that tested commit, including the goal 01 source allowlist line for `services/scheduler/`.

[names.txt](logs/goal-17/names.txt) is `git diff-tree --no-commit-id --name-only -r HEAD` at that commit. Line 17 is `.github/workflows/ci.yml`. The list continues through line 36, `tests/unit/test_ci_scheduler.py`. `chain/` and `proto/` are not in that list.

## Inherited blockers

BLOCKED, carried from goals 04, 07, 12, and 16:

1. **DEC-001 and DR-07-01, production custody.** The scheduler does not hold a production signing key. The hosting-intent role remains unset.
2. **One machine.** [conformance.txt](logs/goal-17/conformance.txt) line 11 prints `physical=vm-1 physical_count=1 simulated=true independent=false`. Five provider processes on this host are one physical failure domain.
3. **Goal 16 fixture chain view.** The providers talk to an in-process HTTPS fixture, the same shape as goal 16. A broadcast `{"code":0}` is counted in `txs`. It is not a four-validator commit. DR-16-04 stays open.
4. **Goal 06 and goal 07 mutation runners.** Anchor checks only. [anchors06.txt](logs/goal-17/anchors06.txt) line 27 is `RESULT search-string problems=13; no test was run` and line 28 is `EXIT:1`. [anchors07.txt](logs/goal-17/anchors07.txt) line 23 is `RESULT validation problems=3; no test was run` and line 24 is `EXIT:1`.
5. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-17/golangci-lint.txt) line 13 is `scripts/evidence/capture.sh: line 58: golangci-lint: command not found` and line 14 is `EXIT:127`.
6. **PostgreSQL 16.** [pg16.txt](logs/goal-17/pg16.txt) line 15 is `EXIT:1`. [runtime.txt](logs/goal-17/runtime.txt) line 14 is `pg16_test:1`. The scheduler does not open PostgreSQL. The indexer read in this run is the fixture JSON, not a PostgreSQL projection.
7. **Goal 04 compose bridge.** [docker.txt](logs/goal-17/docker.txt) line 14 is `scripts/evidence/capture.sh: line 58: docker: command not found` and line 15 is `EXIT:127`. Compose was not run. The unauthenticated :2375 daemon was not used.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-17-01 | OPEN | Indexer lease rows have no package-byte column. `ACCOUNTING` reserved totals are the fixture, not an indexer field. |
| DR-17-02 | OPEN | The chain endpoint is the goal 16 fixture. `txs=4` on the accounting line is that fixture's broadcast count. |
| DR-17-03 | OPEN | Declared operator and failure-domain labels in this run are simulation. `physical_count=1`. `independent=false`. |
| DR-17-04 | OPEN | Cooperating depart and removal use the admin token the test read from each provider's data directory. The scheduler does not hold the provider signing key. A provider that does not grant that token is not deleted. |
| DR-17-05 | OPEN | The repair buffer is discarded. It is not a counted replica. `origin_counted=false` and `origin_reserved=0`. |
| DR-17-06 | OPEN | Three copies stay the proposed publication parameter. They are not a durability guarantee. DEC-005 allowances stay unset. |
| DR-17-07 | OPEN | `DISHONEST` and `STALE` are the classifier given a mutated claim and an hour-old observation. The live receipt on the `PUBLISH` line is `PROVIDER_ACCEPTANCE`. No provider process returned a forged receipt. |
| DR-17-08 | OPEN | `RENEWAL_EXPIRED` leaves `lease_state=ACTIVE`. The notice is not a renewal transaction. The fixture did not expire the row. |
| DR-16-01 | OPEN | Carried. Receipt fields remain an attestation. |
| DR-16-02 | OPEN | Carried. |
| DR-16-03 | OPEN | Carried. Providers listen on loopback. |
| DR-16-04 | OPEN | Carried. Fixture chain view. |
| DR-16-05 | OPEN | Carried. |
| DR-16-06 | OPEN | Carried. One machine. |
| DR-07-01 | OPEN | Carried. Production custody is unset. |
| DR-13-01 | OPEN | Carried. |
| DR-13-02 | OPEN | Carried. |
| DR-13-03 | OPEN | Carried. |
| DR-14-01 | OPEN | Carried. |
| DR-14-02 | OPEN | Carried. The bytes are synthetic 64-byte segments, not an approved video package. |
| DR-14-03 | OPEN | Carried. |
| DR-14-04 | OPEN | Carried. |
| DR-15-01 | OPEN | Carried. |
| DR-15-02 | OPEN | Carried. |
| DR-15-03 | OPEN | Carried. |
| DR-15-04 | OPEN | Carried. |
| DR-15-05 | OPEN | Carried. |
| DR-15-06 | OPEN | Carried. |

## Acceptance

The acceptance log is [conformance.txt](logs/goal-17/conformance.txt). `HEAD` is `b7c1ed19891c297c9af9bea4afe646c64ac870c7` and `TREE` is `f5621f62d5dedbf2fa8d44e306fdf76d0aa3dbf2`. `DIRTY no` is line 5. The other logs in this directory were captured after that file existed, so they print `DIRTY yes` and list the earlier log paths. No scheduler source file was modified between those captures.

The providers are `agents/provider` `svn-provider` processes. The test builds that binary and starts five of them: `p1`, `p2`, `p3` as replicas, `p4` as the spare, and `pcap` with a 10-byte quota. The package is 64 synthetic bytes. The origin is a loopback HTTPS source and is not a counted replica.

| Check | Status | Where |
| --- | --- | --- |
| Approval withheld, no offer posted | VERIFIED on this host | Line 10: `APPROVAL publication=WITHHELD reason=community_approval posts=0`. |
| Publish after approval, verified transfer, three commitments | VERIFIED on this host | Line 11: `publication=PUBLISHED` `evidence=PROVIDER_ACCEPTANCE` `claim=ATTESTATION_NOT_STORAGE_PROOF` `verified=3` `distinct=3` `counted=3` `origin_counted=false` `scheduler_signed=false` `storage_proof=false` `independent=false` `physical_count=1`. |
| Low capacity not selected, and the provider refuses | VERIFIED | Line 12: `LOW_CAPACITY selected=false provider=pcap status=409 error=insufficient quota`. |
| Origin and one provider disconnected, survivors still serve | VERIFIED as process fault injection | Line 13: `at_risk=true` `reachability=REACHABILITY_DEGRADED` `counted=2` `reachable=p2,p3` `degraded=p1`. `active=3` is the fixture lease count, which still includes `p1`. |
| Repair from a survivor onto the spare, bytes and accounting | VERIFIED against the fixture | Line 14: `REPAIR state=CONFIRMED source=p2 replacement=p4`. Line 15: `counted=3` `reachable=p2,p3,p4` `at_risk=false`. Line 16: `ACCOUNTING active=4 reserved_p1=64 reserved_p2=64 reserved_p3=64 reserved_p4=64 origin_reserved=0 txs=4 duplicate_refused=true`. The 64 is the package length the test used. `active=4` keeps the dead provider's reservation. `duplicate_refused=true` is the fixture refusing a second live pair. This is not a devnet height. |
| Two providers down, one spare, still short of three | VERIFIED as process fault injection | Line 17: `SIMULTANEOUS state=CONFIRMED counted=2 at_risk=true reachability=REACHABILITY_DEGRADED`. |
| Duplicate repair does not post again | VERIFIED | Line 18: `DUPLICATE state=DUPLICATE attempts=1 posts_while_blocked=0 final=CONFIRMED`. |
| Renewal notice and expiry do not submit a renewal | VERIFIED against the fixture row | Line 19: `RENEWAL_NOTICE renewal=RENEWAL_NEEDED posts=0`. Line 20: `RENEWAL_EXPIRED renewal=EXPIRED lease_state=ACTIVE renewal_submitted=false`. |
| Dishonest and stale evidence | VERIFIED as the classifier | Line 21: `DISHONEST mode=DISHONEST claim_limit=ATTESTATION_NOT_STORAGE_PROOF storage_proof=false`. Line 22: `STALE mode=STALE claim_limit=ATTESTATION_NOT_STORAGE_PROOF`. See DR-17-07. The live receipt is the `PUBLISH` line's `PROVIDER_ACCEPTANCE`. |
| Chain stall queues and does not post | VERIFIED against a stale indexer flag | Line 23: `STALL publication=QUEUED_NOT_COMMITTED service=TEMPORARY_OFF_CHAIN posts=0`. |
| Removal during repair stops cooperating distribution | VERIFIED on the cooperating providers | Line 24: `REMOVAL state=STOPPED_WITHDRAWN posts=0 distribution=410 provider=410`. |
| Drain does not delete the last needed copy first | VERIFIED | Line 25: `DRAIN depart_before_repair=false last_copy_kept=true after_depart_p1=410 p2=200 p4=200`. |
| No recovery when reachable copies are gone | VERIFIED | Line 26: `UNRECOVERABLE state=UNRECOVERABLE posts=0 origin_used=false`. |
| Four-validator commit of these transactions | BLOCKED | Not run. DR-17-02 and DR-16-04. |
| Independent operator or a second machine | BLOCKED | Line 11 `independent=false` `physical_count=1`. |

[conformance.txt](logs/goal-17/conformance.txt) line 39 is `--- PASS: TestConformance (47.49s)`. Line 41 is `ok github.com/Maxencejules/shortvideo-network/services/scheduler/cmd/svn-scheduler 47.497s`. Line 42 is `EXIT:0`.

Repair traces in the same file: line 28 `repair confirmed content=cpub source=p2 replacement=p4 buffer=discarded`, line 32 `repair duplicate key=cdup/1 attempts=1`, line 33 `queue content=cstall label=TEMPORARY_OFF_CHAIN posts=0`, line 35 `distribution content=crem stopped=true`, line 36 `repair withdrawn-before-offer content=crem posts=0`, line 38 `repair unrecoverable content=clost reachable_copies=0 origin_used=false`.

The scheduler policy is [docs/architecture/scheduler.md](../architecture/scheduler.md). It is PROPOSED.

## Other commands

| Step | Exit | Log |
| --- | --- | --- |
| runtime | 0 | [runtime.txt](logs/goal-17/runtime.txt). Line 10 `Linux 6.12.113+ x86_64`. Line 11 `go version go1.26.10 linux/amd64`. Line 12 `4`. Line 13 `Python 3.12.3`. |
| policy | 0 | [policy.txt](logs/goal-17/policy.txt). Line 18 `ok github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy 0.002s`. |
| vet | 0 | [vet.txt](logs/goal-17/vet.txt). Line 8 `EXIT:0`. |
| gofmt | 0 | [gofmt.txt](logs/goal-17/gofmt.txt). No file names were printed. Line 9 `EXIT:0`. |
| race | 0 | [race.txt](logs/goal-17/race.txt). Line 19 `ok .../cmd/svn-scheduler 50.309s`. Line 20 `ok .../internal/policy 1.009s`. Line 24 `EXIT:0`. A search of that file for `DATA RACE` and `FAIL` returned no line. |
| requirements | 0 | [requirements.txt](logs/goal-17/requirements.txt). Line 45 `errors 0`. Line 79 `result PASS`. Line 81 `EXIT:0`. This capture includes the uncommitted allowlist edit described above. |
| pytest | 0 | [pytest.txt](logs/goal-17/pytest.txt). Line 23 ends with `[100%]`. Line 24 `EXIT:0`. The two files are `tests/unit/test_ci_scheduler.py` and `tests/unit/test_chain_writer_policy.py`. |
| secrets | 0 | [secrets.txt](logs/goal-17/secrets.txt). Line 21 `secret scan ok`. Line 22 `EXIT:0`. |
| sbom | 0 | [sbom.txt](logs/goal-17/sbom.txt). Line 22 `sbom ok components 1322`. Line 23 `EXIT:0`. The scheduler module has no `go.sum`. |
| anchors06 | 1 | BLOCKED. Not a pass. |
| anchors07 | 1 | BLOCKED. Not a pass. |
| golangci-lint | 127 | BLOCKED. Not a pass. |
| docker | 127 | BLOCKED. Not a pass. |
| pg16 | 1 | BLOCKED. Not a pass. |

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-SCOPE-015 | VERIFIED as the proposed parameter on this host. Not a durability result and not independent operators. | [conformance.txt](logs/goal-17/conformance.txt) line 11 `distinct=3` `physical_count=1` `independent=false`. |
| REQ-HOST-005 | VERIFIED for the approval gate and the separate evidence mode on this host. | Lines 10 and 11. |
| REQ-HOST-006 | VERIFIED for repair-before-delete on the cooperating provider. | Line 25. |
| REQ-HOST-008 | BLOCKED | DEC-004 retention is unset. The origin backup is tracked separately (`origin_counted=false`). IPFS is not used. |
| REQ-QA-003 | Not this goal's measurement | The same scenario ran here as process fault injection (lines 13-16). Goal 18's hosting gate is not claimed. |

## Handoff

- Failure and repair traces: [conformance.txt](logs/goal-17/conformance.txt) lines 13-16, 17, 24-26, and 28-38.
- Scheduler policy: [docs/architecture/scheduler.md](../architecture/scheduler.md) and `services/scheduler/internal/policy`.
- Availability: line 11 is the published view. Line 13 is `AT_RISK` while the fixture still has three active leases. Line 15 is restored reachability with the dead lease still reserved.
- Independent-operator verification is the `independent=false` field. The single-host topology test is `physical=vm-1 physical_count=1`.

Install, from [services/scheduler/README.md](../services/scheduler/README.md):

```text
svn-scheduler serve config.json
```

## Independence tests that still need another person or another machine

- A second person installing a provider on infrastructure they control.
- A claim that `op-p1` through `op-p4` are different operators or different failure domains. Line 11 records `simulated=true`, `independent=false`, and `physical_count=1`.
- Physical redundancy, a site failure, or a network partition between operators.
- Goal 30's independent installation. Not this goal's result.

## Head CI

Run [42801666285](https://github.com/Maxencejules/shortvideo-network/actions/runs/42801666285) on `75a708cfca79057c0f81ec49af31ed0efef041c1` completed **success** at 2026-11-10T10:57:18Z. It was created at 2026-11-10T10:44:18Z. All 16 jobs succeeded: `node`, `python`, `go`, `contract`, `devnet`, and the go-race shards `app`, `devnet`, `devnet-evidence`, `devnet-upgrade`, `devnet-broadcaster`, `devnet-broadcaster-stall`, `devnet-indexer`, `broadcaster`, `indexer`, `api`, and `rest`. `75a708cf` differs from the tested commit `b7c1ed19891c297c9af9bea4afe646c64ac870c7` only under `docs/evidence/`. No run on this branch was failed or cancelled when this section was written. This docs-only commit starts a later run; that run is not 42801666285.

## Log stamps

[conformance.txt](logs/goal-17/conformance.txt) was written while `HEAD` was `b7c1ed19891c297c9af9bea4afe646c64ac870c7`, `TREE` was `f5621f62d5dedbf2fa8d44e306fdf76d0aa3dbf2`, and `DIRTY no`. The other logs in `docs/evidence/logs/goal-17/` use that same `HEAD` and `TREE` and print `DIRTY yes` because earlier logs in that directory were untracked. The requirements log also saw the uncommitted allowlist edit in `docs/evidence/goal-01/check_requirements.py`.

## Residual risk

The chain endpoint is an in-process fixture. Reserved bytes are that fixture's counter. A four-validator commit was not run. The dishonest and stale results are the classifier, not a provider that lied on the wire. Removal stops providers that granted the admin token. A provider that does not grant it can keep serving. Renewal does not change the fixture lease row. The package is 64 synthetic bytes, not a video.

## Next safe action

Open a draft pull request from `goal-17`. Do not merge it. Do not start goal 18 from an unreviewed branch. Public launch stays disabled.

## Proposed pull-request title and summary

Title: Add the off-chain replication scheduler

Summary: The scheduler proposes hosts and does not sign for them. Publication waits for approval, a verified transfer, and three operator-distinct commitments. Repair copies from a survivor, checks the bytes, and confirms the new lease before a planned departure deletes a needed copy. Withdrawn content is not repaired. One machine is one physical failure domain. The goal stays BLOCKED on custody, that single machine, the fixture chain view, the mutation anchors, golangci-lint, PostgreSQL 16, and compose. DR-17-01 through DR-17-08 are open.
