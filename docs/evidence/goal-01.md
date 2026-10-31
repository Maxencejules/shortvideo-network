# Goal 01 evidence

Status: VERIFIED

The verification is the specification check for this goal. It is not a chain test, a media test, a security audit, or an approval of the protocol. The protocol is not approved. Public launch: DISABLED. No tradable token, coin, sale, or mining.

34 in-scope requirements stay explicitly blocked on OPEN decisions DEC-001 through DEC-008. That record is required. It is not a failed check. Matrix tests assigned to later goals were not run and are not marked passed.

## Model and effort

- Playbook assignment for goal 01: GPT-6 Astra, effort EXTRA HIGH.
- Model actually used: `grok-4.7`.
- Effort actually used: `reasoning_effort=xhigh`, `fast=false`, `context=500k`. The task that launched this run stated those settings.
- Runtime report from cursor-cloud `run-info`: `originalModelName=grok-4.7`, `bcId=bc-95d6fe3d-22bc-5dbe-a7bb-287237e044cb`, run URL `https://cursor.com/agents/bc-95d6fe3d-22bc-5dbe-a7bb-287237e044cb`. That payload has no effort, reasoning, fast, or context field.
- Astra was not used. GPT-6.1 Sol was not used.
- Extra effort does not certify correctness.

## Runtime

Linux 6.12.94+ x86_64. Python 3.12.3. No chain, database, browser, FFmpeg worker, container devnet, or second machine was started.

## Base

Goal 00 is merged at `a821cc520060513c7a00f773e6cff28a1b83a92d` (tree `2eb10a97b7bca68f83a8c2634125c20d0d322e23`). This review fix is on branch `goal-01`. The parent of the review-fix commit is `64a19d75bf0fc27a49873f3270b2ba45e5e4fbc7`.

## Tested revision

This evidence file is part of the head commit that contains the review fixes. The checker does not read this file. It does read `docs/progress.md` and `docs/progress.tsv`, so those hashes below are the bytes in this commit, not the earlier hashes from `ad9f054205469e061b2dbb42a6d834ec11edd45b`.

The head sha cannot be written into this file without changing the commit. The tested tree is this commit's tree. It is the `Tested-tree` trailer. Confirm with `git rev-parse 'HEAD^{tree}'`.

Checker-input sha256 values at the run:

- `docs/requirements.md` `a68677782361bbb02c7709cfef78ebde46dec350d9855209056a684ce66b4f16`
- `docs/threat-model.md` `1ff5afab1bdfa0e5cd5c8a367e6457e38595f05ee891822177ee14cb9282b036`
- `docs/decision-register.md` `f464e653435bc43d88b99027896f5bbd5055c9f18922de6be5156c554536fbf4`
- `docs/progress.md` `bbf3eb3aed0d28303ac8edb7c81a4d695f1199c38031958e7b2bea778f9e6da1`
- `docs/progress.tsv` `30177a84848e1e442e90f920a632834ab6e9af4dbf97d3484672ffde99486ec5`
- `docs/evidence/goal-01/check_requirements.py` `b859cd2cd4776b3ce7894ce5ac3aad3fc9c85617152f307e8a125bcc81f5a022`

## Commands and output

The checker stdout below was captured after the review edits. Exit code 0. The same command was run again after this evidence file existed. That repeat exited 0 and its stdout matched the checker section from `self_test duplicate id` through `public launch disabled`.

```
COMMAND git rev-parse HEAD
64a19d75bf0fc27a49873f3270b2ba45e5e4fbc7
exit:0

COMMAND git rev-parse HEAD^{tree}
eff7a4b36481e9ee97a4221cb7f424cc90f8dda1
exit:0

COMMAND git status --short --branch
## goal-01...origin/goal-01
 M docs/evidence/goal-01/check_requirements.py
 M docs/requirements.md
 M docs/threat-model.md
exit:0

COMMAND python3 -m py_compile docs/evidence/goal-01/check_requirements.py
exit:0

COMMAND python3 docs/evidence/goal-01/check_requirements.py
self_test duplicate id fail_as_expected
self_test missing role fail_as_expected
self_test bad goal fail_as_expected
self_test deferred with a goal fail_as_expected
self_test missing test or blocker fail_as_expected
self_test missing blueprint page fail_as_expected
self_test scenario list fail_as_expected
requirements 265
in_scope 250
with_test 216
with_blocker 34
deferred 9
non_goal 6
decisions 8
threats 15
scenarios 5
contradictions 13
self_test_cases 7
self_test_problems 0
page_coverage ok
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
threat TM-STAKE-BYPASS recorded
threat TM-PERSONAL recorded
threat TM-INJECTION recorded
threat TM-MINT recorded
threat TM-SECRET recorded
result PASS
note document check only; protocol not approved; public launch disabled
exit:0

COMMAND sha256sum
a68677782361bbb02c7709cfef78ebde46dec350d9855209056a684ce66b4f16  docs/requirements.md
1ff5afab1bdfa0e5cd5c8a367e6457e38595f05ee891822177ee14cb9282b036  docs/threat-model.md
f464e653435bc43d88b99027896f5bbd5055c9f18922de6be5156c554536fbf4  docs/decision-register.md
bbf3eb3aed0d28303ac8edb7c81a4d695f1199c38031958e7b2bea778f9e6da1  docs/progress.md
30177a84848e1e442e90f920a632834ab6e9af4dbf97d3484672ffde99486ec5  docs/progress.tsv
b859cd2cd4776b3ce7894ce5ac3aad3fc9c85617152f307e8a125bcc81f5a022  docs/evidence/goal-01/check_requirements.py
exit:0
```

## Scenario walkthroughs

Specification walks only. The requirements lists now include every requirement id cited in the scenario prose.

- S-PLAYBACK cites and lists REQ-MEDIA-002, REQ-MEDIA-007, REQ-HOST-001, REQ-CHAIN-012, REQ-PROD-004, REQ-MEDIA-005, and REQ-UX-008, plus the rest of the upload path. DEC-001, DEC-003, DEC-004, and DEC-005 stay open.
- S-ORIGIN lists the survivor path, including REQ-UX-010. DEC-002 and DEC-004 stay open.
- S-DEPART lists the handoff. DEC-002 and DEC-005 stay open.
- S-STALL lists the quorum stop. DEC-008 stays open.
- S-REMOVE lists REQ-MOD-009 with the delist limits. DEC-003 and DEC-004 stay open.

## Blockers

DEC-001 custody and recovery, DEC-002 operator admission, DEC-003 moderation and removal, DEC-004 retention, DEC-005 sponsorship, DEC-006 licensing, DEC-007 audience policy, and DEC-008 council quorum remain OPEN. No page 26 measurement was run.


## Gate A addendum (2026-10-31)

The record above is the pre-Gate-A evidence, including the statements that DEC-001 through DEC-008 remain OPEN. Gate A on 2026-10-31 (America/Toronto) approved DEC-001 through DEC-008. This addendum does not rewrite that history.

Max approved the goal 02 fix round at 07:35 on 2026-10-31 (America/Toronto).

Checker command on this revision: `python3 docs/evidence/goal-01/check_requirements.py`. Exit 0. The repeat run was byte-identical.

```
self_test duplicate id fail_as_expected
self_test missing role fail_as_expected
self_test bad goal fail_as_expected
self_test deferred with a goal fail_as_expected
self_test missing test or blocker fail_as_expected
self_test missing blueprint page fail_as_expected
self_test scenario list fail_as_expected
self_test threat heading exact fail_as_expected
self_test progress mismatch fail_as_expected
self_test row goals fail_as_expected
self_test extra column goal fail_as_expected
self_test negation still names a goal fail_as_expected
requirements 265
in_scope 250
with_test 216
with_blocker 34
deferred 9
non_goal 6
decisions 8
threats 15
scenarios 5
contradictions 13
self_test_cases 12
self_test_problems 0
page_coverage ok
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
threat TM-STAKE-BYPASS recorded
threat TM-PERSONAL recorded
threat TM-INJECTION recorded
threat TM-MINT recorded
threat TM-SECRET recorded
result PASS
note document check only; protocol not approved; public launch disabled
EXIT:0
```
