# Release readiness

Public launch: DISABLED

No tradable token, coin, sale, or mining. This file is not an audit, not a production-security claim, and not an approved protocol. Completing goals 00 through 30 is not approval.

## Decision

**Implemented prototype.**

The three categories are implemented prototype, beta candidate, and beta approved after required human sign-offs. No human sign-off exists, so the category cannot be beta approved. It is also not a beta candidate. A beta candidate would mean the prototype is complete enough to ask a named operator to install, with only those signatures outstanding. The open items include missing independence, an unmet human audit, an unapplied licence, unset quotas, a contract gap on published client paths, an open grant-accounting finding, and load gates that were not met. Those are release blockers, not a short signature list.

The candidate is the commit that adds this file and carries `Tested-tree` in its message. Later commits may change only `docs/evidence/`. The evidence file records the commit, the tree, the commands, and the exits. A command that was not run is not a pass.

## What is usable

A developer on one machine can build the devnet prototype, run the unit and contract checks the evidence file lists, and rehearse the NOT-RELEASED packaging flow. The viewer, the operator dashboard, the provider agent, and the chain are in the tree. Invited accounts, upload quarantine, processing, HTTPS playback, leases, and local removal exist as development behavior. Several of those paths still stop on an open decision. `POST /publish` is not a public launch.

`scripts/acceptance/second_client.py` speaks only the paths in `docs/protocol/api/client-api.json` and `docs/protocol/api/provider-api.json`. It uses the Python standard library, does not open a database, and does not follow a redirect. It does not claim ActivityPub, AT Protocol, IPFS, or Filecoin compatibility. `svn-feed-client` is a different program: it links the discovery package, so it is not this second client.

## What remains unverified

- No separate operator and no separate machine. The install on the agent VM is a rehearsal. DR-30-01.
- Separate-host success demonstration. A multi-process run on this VM, if the evidence log has one, is one failure domain. DR-30-07.
- Owner sign-off for moderation coverage, privacy and rights, funding and quotas, operator participation, and security review. DR-30-02 through DR-30-06.
- Human security audit. DR-29-01. Finding F-29-05. DR-30-08.
- Published paths `GET /feeds`, `POST /reports`, and `GET /exports` are not the routes the handlers register. DR-30-11.
- Page 26 design point and shaped playback. DR-30-10. Inherited DR-28-01 through DR-28-11.
- Container devnet on this VM. The docker command is absent. Port 2375 is not used. DR-30-09.
- Project licence file. DEC-006 is not applied. Inherited DR-27-01.
- Chain-state rollback. Inherited DR-27-06.
- golangci-lint on this VM, unless the evidence log shows it was installed and run.
- PostgreSQL 16. The pin exercised here, when the restore and milestone logs exist, is 18.6.
- Custody and account-recovery completion. DEC-001 and DR-26-05.
- Named operators, council seats, and on-call coverage. DEC-002.

Prior evidence from goals 01 through 29 was produced on earlier commits. A rerun on this candidate is the only result that belongs to this candidate. The evidence file is that list. A missing rerun stays blocked for this revision.

## Sign-off placeholders

No name below was supplied. Blank is the recorded state.

| Gate | Name | Date | Decision | Record |
| --- | --- | --- | --- | --- |
| Moderation coverage |  |  | not given | DR-30-02 |
| Privacy and rights policy |  |  | not given | DR-30-03 |
| Funding and quotas |  |  | not given | DR-30-04 |
| Operator participation |  |  | not given | DR-30-05 |
| Security review |  |  | not given | DR-30-06 |

## Public-release checklist

Each item from the checklist in `docs/requirements.md` is still blocked.

| Item | State |
| --- | --- |
| Independent installation | Blocked. DR-30-01. Rehearsal only. |
| Recovery drills | The goal 26 drill exists. Live recovery is blocked. DR-26-01 through DR-26-05. Chain-state rollback was not rehearsed. |
| Signed builds | The packaging flow can make a test signature and then delete the test key. An owner signature was not made. DR-27-05. |
| Reviewed authorization audit | Blocked. DR-29-01 and DR-30-06. |
| Privacy and rights policy | Blocked. DR-30-03. |
| Measured service targets | Blocked. DR-30-10. No service RTO or RPO is set. |
| Budget owner | Blocked. DR-30-04. No person is named. |
| Named operational coverage | Blocked. DR-30-05. The five roles have no person. |

## Goal 30 requirement rows

| Row | State on this candidate |
| --- | --- |
| REQ-PROD-001 | The viewer, the operator software, and the chain are in the tree. A joint run on one VM is not three deployments. |
| REQ-PROD-011 | Blocked. No second operator. DR-30-01 and DR-30-07. |
| REQ-UX-015 | The second client uses the published documents and refuses the four interoperability claims. |
| REQ-UX-020 | The second client exercises the published paths. Live services do not implement three of the client paths. DR-30-11. |
| REQ-UX-021 | Blocked. DR-30-01. A node count is not independence. |
| REQ-DEV-002 | Blocked. DR-30-01. |
| REQ-MOD-006 | Blocked on DEC-007. DR-30-03. |
| REQ-QA-011 | Blocked. The installer was not given to a second operator. The agent rehearsal is not that gate. |
| REQ-LAUNCH-001 | This file lists every public-checklist item as blocked. |
| REQ-SUCCESS-001 | Separate hosts are blocked. A local multi-process log, if present, is labeled as one machine. |

## Inherited blockers

These stay open. This goal does not mark them complete.

- DR-25-01 through DR-25-12
- DR-26-01 through DR-26-12
- DR-27-01 through DR-27-13
- DR-28-01 through DR-28-11
- DR-29-01 through DR-29-06
- F-29-05

Goals 04 and 07 through 29 are BLOCKED in `docs/progress.md` except goal 06, which is VERIFIED on its own earlier revision, and goals 02 and 03, which are IMPLEMENTED. Those statuses are unchanged. Goal 06's verified revision is not this candidate.

## Deferred extensions

Stage 6 stays deferred: mobile clients, peer assistance, feed plugins, deeper portability, and reviewed proofs. Public sign-up, monetary rewards, a tradable token, open validator admission, live video, and a commercial music library stay out of this beta. Matrix rows whose scope is not `IN_SCOPE` are out of this release. `scripts/acceptance/audit_matrix.py` prints the row counts. The evidence log is the count for this candidate. A count is not a pass.

## Governance and ownership

`docs/ownership.md` names five functions and no person: product/application, chain/security, media/provider agents, deployment/on-call, and trust/safety. There is no `CODEOWNERS` file. Council seats are not named. DEC-002 names no operator. The three authorities in `docs/operations/governance-authorities.md` are separate in the design. On the devnet one operator holds every seat and every node, so the separation is not demonstrated. This file does not appoint anyone to those roles. The GitHub account that owns the repository is not recorded here as a moderator, a validator operator, or a budget owner.

Publication of a tag or a release remains an owner action that this goal does not take. The release-candidate workflow is `workflow_dispatch` only. It was not dispatched.

## Operator responsibilities

The handoff is `docs/operations/operator-handoff.md`. An operator who appears later decides whether to install, keeps a consensus key off the dashboard, and does not treat a service signature as a provider signature. No such operator participated.

## Recovery and incident coverage

Rollback and restore are constrained as follows.

- Chain-state rollback was not rehearsed. `docs/operations/failed-upgrade.md` records when an old binary is not a recovery. Do not present a new genesis as recovery of this chain.
- Permanent loss of more than one third of voting power stops new commits. `QuorumLossBehavior` is unset. There is no on-chain return path (DR-10-11).
- `svn-restore` restores onto a clean directory when the origin path is absent. The backup encryption key is not stored in the backup. Completing account recovery is refused.
- A second live process with the same consensus public key is refused.
- Uninstall deletes only paths listed in the install manifest and leaves operator-data.
- No retention period, custody procedure, or service recovery-time target is set (DR-26-01 through DR-26-03).
- No person is on call. Observe alerts are not an incident rota and not a spend cap. Moderation contact lines are placeholders. No incident review was signed.

## First community, plan only

Nothing was launched. No DNS name was published. No account was invited outside this VM. No catalogue was served to another person.

If a later human approval names a first community, the bound is the one machine that produced the evidence, one media worker, and invited accounts only. Do not start the page 26 design point. Do not describe several local processes as several operators. Do not enable public sign-up. Do not create a token. Stop if a processing run does not finish inside the 120 second procedure limit in `docs/operations/load-plan.md`, or if the evidence measure for this candidate is blocked. Use synthetic media or media the owner supplies for a test. The creator upload licence text is still unwritten, so do not accept material from outside that test. This paragraph does not authorize that community. DR-30-01 through DR-30-11 would still have to be closed, along with the inherited blockers, before the category could move.

## Requirement audit

`scripts/acceptance/audit_matrix.py` reads the `requirements-matrix` fence in `docs/requirements.md`. Every in-scope row is `TEST:` or `BLOCKER:`. Goal 30 rows are the ten ids in the table above. The contract checker `docs/evidence/goal-01/check_requirements.py` remains the document gate. Neither script approves the protocol.
