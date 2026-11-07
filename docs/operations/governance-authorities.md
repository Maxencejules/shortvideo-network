# Governance authorities and approvals

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. On the devnet one operator holds every council seat, the executor key, and every node, so the roles below are separate in the design and concentrated in practice (REQ-RISK-004).

This page separates local emergency controls, protocol changes, and publication authority, and lists every goal-10 step that still needs a human approval or an independent security review. The specification is [governance-and-upgrades.md](../architecture/governance-and-upgrades.md#17-separation-of-local-emergency-controls-protocol-changes-and-publication-authority) sections 17 and 24.

## Three authorities

| Authority | Who | May | May not |
| --- | --- | --- | --- |
| Local emergency controls | each node operator, for its own node | stop its process; close its own ports; disable its own endpoint locally ("Emergency local disablement of a compromised endpoint is not a committed chain decision.", DEC-008); decide whether to install a scheduled binary; restore its own diverged node from its copy at the halt ([failed-upgrade.md](failed-upgrade.md#restore-a-lone-diverged-node)) | write a committed decision; change another node; change finality; skip an executed upgrade plan; approve anything |
| Protocol changes | the council proposes and approves (three of four seats, the review delay, an expiry); the `AUTHORITY` account executes; operators then install reviewed binaries | validator power, governed limits, provider admit and disable, force-withdraw, upgrade plans and their cancel, seat key replacement ([council-changes.md](council-changes.md)) | through any payload: approve a blocked decision, change a safety cap, act without the delay and expiry rules, or install a binary on a node |
| Publication authority | the project maintainers ("The project's maintainers control what code enters the reference repository.", `docs/blueprint.txt:1033`); release signing and tags are owner actions in goal 27 | decide what code enters the reference repository; prepare release candidates ([release-preparation.md](release-preparation.md)) | change chain state; schedule an upgrade; run a validator by virtue of the role |

None of the three owns users' content (REQ-GOV-002). Community governance (goal 23) is a fourth body, named and not designed here. Goal 10 adds no on-chain pause switch, emergency key, or single-signature path (DEC-009).

An upgrade binary can change any rule. The council's approval schedules it; it does not make it safe. Human review of the source and digest ([source-and-digest-review.md](source-and-digest-review.md)) and each operator's choice to install ([operator-installation.md](operator-installation.md#how-an-operator-chooses)) are the controls, and neither is a test.

## Steps that need a human approval or an independent security review

Goal 10 performs none of these.

1. The owner's approval of `docs/architecture/governance-and-upgrades.md` at a pinned commit and file sha256 (DR-10-00), asked at pull-request time. The owner answered Q1, Q2, and Q3 on 2026-11-07; the two lead constants (100 and 1,000,000 blocks) remain PROPOSED.
2. A recorded decision for any further protocol revision.
3. DEC-008 live values: seat holders, the review delay, a maximum expiry, quorum-loss behaviour; any genesis that names real council keys; the PROPOSED lead bounds (Q3, DR-10-15).
4. Real operators, validators, and auditors (DEC-002).
5. Generation, custody, and rotation of live keys: seat, executor, validator, release signing (DEC-001).
6. Approving any blocked decision parameter. The chain refuses it; the decision stays human.
7. Tagging, publishing, or signing a release, and naming the publisher (goal 27).
8. Human review of release source and digests before any operator installs a binary, including the diff of `chain/upgrade`, the handler, and the self-check list against the previous release ([source-and-digest-review.md](source-and-digest-review.md)).
9. Each operator's decision to install an upgrade. On the devnet one operator decides for all four nodes.
10. An independent operator installing from these runbooks (goal 30).
11. Independent security review of the authority changes, the upgrade gate, the per-block rule and runner, the handler environment, every migration handler, and the release digest procedure (REQ-SEC-011, goal 29). A model-assisted review is not that review.
12. Adding the governance-capture, malicious-upgrade, single-executor, and prior-release threats to `docs/threat-model.md` (DR-10-19).
13. Merging goal 09 and goal 10, and any branch-protection change for new CI checks.
14. Any council-scheduled upgrade on a chain other than a disposable devnet.
15. Recovery from permanent quorum loss (out of scope; a new network needs its own decision, DR-10-11).
16. A maximum planned halt and any floors for governed values (DR-10-39, DR-10-09).
17. The open spec divergences of the authority differential: G-11 as widened by the orchestrator (DR-10-44) and the three parse divergence classes no rule names (`chain/x/authority/README.md`, "Spec differences recorded by the goal 10 differential").
