# Goal 01 evidence

Status: VERIFIED

The verification is the specification check for this goal. It is not a chain test, a media test, a security audit, or an approval of the protocol. The protocol is not approved. Public launch: DISABLED. No tradable token, coin, sale, or mining.

34 in-scope requirements are explicitly blocked on OPEN decisions DEC-001 through DEC-008. That is the required record of missing decisions, not a failed check. Assigned tests in the matrix belong to later goals. They were not run here and are not marked passed.

## Model and effort

- Playbook assignment for goal 01: GPT-6 Astra, effort EXTRA HIGH.
- Model actually used: `grok-4.7`.
- Effort actually used: `reasoning_effort=xhigh`, `fast=false`, `context=500k`. The task that launched this run stated those settings and described xhigh as the highest reasoning setting offered for that model.
- Runtime report from cursor-cloud `run-info`: `originalModelName=grok-4.7`, `bcId=bc-95d6fe3d-22bc-5dbe-a7bb-287237e044cb`, run URL `https://cursor.com/agents/bc-95d6fe3d-22bc-5dbe-a7bb-287237e044cb`. That payload has no effort, reasoning, fast, or context field. It does confirm the model name.
- Astra was not used. GPT-6.1 Sol was not used. Do not read the playbook assignment as the model that ran.
- Extra effort does not certify correctness.

The effort value is taken from the launch instruction because `run-info` does not echo it. The goal is not marked BLOCKED on model allocation: the setting was stated for this run, and the runtime model name matches. A reader who counts only the `run-info` payload will see the effort field absent there.

## Runtime

Linux 6.12.94+ x86_64. Python 3.12.3. No application toolchain was required. No chain, database, browser, FFmpeg worker, container devnet, or second machine was started. Those absences are expected for this goal. They are not passes of later goals' tests.

## Base

Verified before editing. `origin/main` and local `HEAD` were `a821cc520060513c7a00f773e6cff28a1b83a92d`, commit subject `goal 00: companion pack (#3)`. Tree at that commit: `2eb10a97b7bca68f83a8c2634125c20d0d322e23`. Goal 00 evidence is `docs/evidence/goal-00.md`, status VERIFIED. Branch for this goal: `goal-01`, created from that commit.

## Tested revision

The command log below ran on the worktree before this evidence file existed. `git rev-parse HEAD` was still the base commit. The specification bytes under test are the sha256 values in that log:

- `docs/requirements.md` `13b38667fa0db2023467ea0e502256a12b054cbb83efbab6deffb34aa7c35136`
- `docs/threat-model.md` `74f09e3fdafe011501ae8c6622d69840e36841789b46700d6e7b41177d46e09e`
- `docs/decision-register.md` `f464e653435bc43d88b99027896f5bbd5055c9f18922de6be5156c554536fbf4`
- `docs/progress.md` `6c41067b213eb1df01c8d9e14ec0fcfdb4818fee36e973d1e855e78e8854fd9f`
- `docs/progress.tsv` `47abeac5deefa0410cd6fe993c13c552298800d994b6b3f227961a9a21465fbf`
- `docs/evidence/goal-01/check_requirements.py` `f39f1e502c6f5310a885286149e8e575923d089a794b0814687295382876a2b5`

The checker does not read this evidence file. Adding this file does not change those hashes. This commit's own sha and tree are not written here, because writing them would change the tree. The commit message carries a `Tested-tree` trailer for the commit that introduces this file. A later evidence note may name that commit after `git rev-parse` can see it.

## What the check covers

`docs/evidence/goal-01/check_requirements.py` checks all of the following:

- 265 requirements, 250 in scope, each with a role, a goal in 01-30, and either `TEST:` or `BLOCKER:DEC-00N`
- 216 test assignments and 34 decision blockers
- 9 deferred rows and 6 non-goals kept on `goals=none`
- Every blueprint page 3 through 28 appears on an in-scope row
- DEC-001 through DEC-008 exist, are `Status: OPEN`, and each blocks at least one in-scope row
- Five scenarios and 13 contradiction resolutions are present
- Ten threat entries each have a mitigation, a test, and a residual limitation
- The independent-process and independent-operator distinction is present
- Public launch is disabled in the specification, the threat model, the register, and the progress tracker
- The token, coin, sale, and mining exclusion is present
- Video bytes stay off-chain
- Goal pages 01-30 are byte-identical to the Goal 00 hashes
- No application directories or application source files were added
- A planted bad matrix fails inside `--` the script's self-test. `self_test_problems 0` means that negative check found the planted failures and returned no checker defect

`TEST:` in the matrix names a later goal's check. It does not mean that check ran.

## Commands and output

```
COMMAND git rev-parse HEAD
a821cc520060513c7a00f773e6cff28a1b83a92d
exit:0

COMMAND git rev-parse HEAD^{tree}
2eb10a97b7bca68f83a8c2634125c20d0d322e23
exit:0

COMMAND git status --short --branch
## goal-01
 M README.md
 M docs/progress.md
 M docs/progress.tsv
?? docs/decision-register.md
?? docs/evidence/goal-01/
?? docs/requirements.md
?? docs/threat-model.md
exit:0

COMMAND git merge-base --is-ancestor a821cc520060513c7a00f773e6cff28a1b83a92d HEAD
exit:0

COMMAND git log -1 --format=%H
a821cc520060513c7a00f773e6cff28a1b83a92d
Maxence Jules <142577996+Maxencejules@users.noreply.github.com>
goal 00: companion pack (#3)
exit:0

COMMAND uname -srm
Linux 6.12.94+ x86_64
exit:0

COMMAND python3 --version
Python 3.12.3
exit:0

COMMAND python3 -m py_compile docs/evidence/goal-01/check_requirements.py
exit:0

COMMAND python3 docs/evidence/goal-01/check_requirements.py
requirements 265
in_scope 250
with_test 216
with_blocker 34
deferred 9
non_goal 6
decisions 8
threats 10
scenarios 5
contradictions 13
self_test_problems 0
errors 0
scenario S-PLAYBACK recorded
scenario S-ORIGIN recorded
scenario S-DEPART recorded
scenario S-STALL recorded
scenario S-REMOVE recorded
contradiction CR-001 recorded
contradiction CR-002 recorded
contradiction CR-003 recorded
contradiction CR-004 recorded
contradiction CR-005 recorded
contradiction CR-006 recorded
contradiction CR-007 recorded
contradiction CR-008 recorded
contradiction CR-009 recorded
contradiction CR-010 recorded
contradiction CR-011 recorded
contradiction CR-012 recorded
contradiction CR-013 recorded
threat TM-UPLOAD recorded
threat TM-UNAUTH-TX recorded
threat TM-KEYS recorded
threat TM-DISHONEST recorded
threat TM-REPLICAS recorded
threat TM-STALL recorded
threat TM-STALE-GW recorded
threat TM-ABUSE recorded
threat TM-SUPPLY recorded
threat TM-VALIDATOR recorded
result PASS
note document check only; protocol not approved; public launch disabled
exit:0

COMMAND sha256sum specification files
f54368118a32d47459a6bca6fc675b576e22ee1caeb4c9533ae7793fff27af5e  AGENTS.md
13b38667fa0db2023467ea0e502256a12b054cbb83efbab6deffb34aa7c35136  docs/requirements.md
74f09e3fdafe011501ae8c6622d69840e36841789b46700d6e7b41177d46e09e  docs/threat-model.md
f464e653435bc43d88b99027896f5bbd5055c9f18922de6be5156c554536fbf4  docs/decision-register.md
6c41067b213eb1df01c8d9e14ec0fcfdb4818fee36e973d1e855e78e8854fd9f  docs/progress.md
47abeac5deefa0410cd6fe993c13c552298800d994b6b3f227961a9a21465fbf  docs/progress.tsv
f39f1e502c6f5310a885286149e8e575923d089a794b0814687295382876a2b5  docs/evidence/goal-01/check_requirements.py
8a84b33df759510c4537d7727739fe7545a2ff6018a721e3cb7a4aec4baa424b  README.md
exit:0
```

The checker was run again after this file was added. Its stdout matched the checker section quoted above, from `requirements 265` through `public launch disabled`, and the exit code was 0. That re-run is not a second protocol test. It is the same document check on the same specification bytes.

## Scenario walkthroughs

These are specification walks, not executed network runs. The full text is in `docs/requirements.md`.

- S-PLAYBACK. An invited creator logs in at the account service, uploads into quarantine, and an isolated worker produces a manifest. The chain receives a digest and a byte count, not video bytes. Provider acceptance is a provider signature. The app waits for commit before it treats a lease as active. Published is a moderation and hosting condition, not a legal finding. DEC-001, DEC-003, DEC-004, and DEC-005 stay open.
- S-ORIGIN. After publication conditions hold, the creator and origin copy go away. A surviving host can serve bytes that match the manifest. Repair must not delete the last reachable copy first. This does not prove a second operator or a backup policy. DEC-002 and DEC-004 stay open.
- S-DEPART. A provider stops new offers and hands off or reaches the real-time deadline. Committed bytes do not change on renewal. An offline note during a stall is not an on-chain renewal. DEC-002 and DEC-005 stay open.
- S-STALL. Two of four equal-power validators stop new commits. New leases stay pending. In-term playback can continue with a stale-chain disclosure. Finality is not overridden and genesis is not reset. DEC-008 leaves quorum loss unspecified.
- S-REMOVE. Cooperating services delist, end participating leases, purge caches they control, and revoke credentials. Independent copies and ledger history remain. Universal deletion is a non-goal. DEC-003 and DEC-004 stay open.

## Blockers and unrun tests

- DEC-001 custody and recovery. OPEN.
- DEC-002 operator admission and any independence claim. OPEN.
- DEC-003 moderation rules and the removal promise. OPEN.
- DEC-004 retention. OPEN.
- DEC-005 sponsorship, allowances, and lease terms. OPEN.
- DEC-006 software licence and creator upload licence. OPEN.
- DEC-007 audience, age rule, and jurisdiction-specific privacy advice. OPEN.
- DEC-008 council seats, threshold, delay, and quorum loss. OPEN.
- Page 26 numeric gates were not measured. No devnet, browser, load runner, or independent machine was available, and this goal does not require those runs.
- No human security review was performed.

## Handoff

Changed files are the specification, the progress row, this evidence, and the checker. No application code. Next safe action is Goal 02, using the input checklist in `docs/requirements.md`, with DEC-001 through DEC-008 explicitly blocked. Public launch stays disabled.
