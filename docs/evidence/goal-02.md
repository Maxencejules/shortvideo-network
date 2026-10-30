# Goal 02 evidence

Status: IMPLEMENTED

The schema and fixture checks below passed on this tree. The goal is not VERIFIED. The effort setting cannot be confirmed from inside this VM, and that point is waiting for Max's ruling at Gate A. AGENTS.md says an unreported or unverifiable effort setting for goals 01-30 is BLOCKED rather than VERIFIED. This goal is therefore IMPLEMENTED.

The checks are not a devnet, a four-validator process test, a media test, a security audit, or an approval of the protocol. The protocol is not approved. Public launch: DISABLED. No tradable token, coin, sale, or mining.

DEC-001 through DEC-010 stay OPEN. Page 26 playback, processing, load, recovery, moderation-timing, and operator-independence measurements were not run and are not passes. `replay-app-hash` pins a golden hash of the chain-state projection. It is not Goal 05's process replay.

Pull request: https://github.com/Maxencejules/shortvideo-network/pull/5

## Model and effort

- Playbook assignment for goal 02: GPT-6 Astra, effort EXTRA HIGH. Astra was not used.
- Launch parameter, not an observation: the task that started this run requested model `grok-4.7` with `reasoning_effort=xhigh`.
- What this VM can see: cursor-cloud `run-info` reports `originalModelName=grok-4.7` and `bcId=bc-763364ae-2b17-5737-aa7d-39bb2a64a0c0`. That payload has no effort, reasoning, fast, or context field. The effort setting cannot be independently verified from inside the VM.
- Ruling: this point is awaiting Max at Gate A. Until that ruling, goal 02 stays IMPLEMENTED and is not marked VERIFIED.
- `docs/evidence/goal-01.md` was not edited. Goal 01's status in `docs/progress.md` and `docs/progress.tsv` stays VERIFIED.
- Extra effort would not certify correctness.

## Goal 01 files that differ from main

`docs/evidence/goal-01.md` and goal 01's status are unchanged. Two other goal 01 files differ from `783e159` (`main` at the goal 02 base). `git diff 783e159 --numstat` reports `186` insertions and `27` deletions in `docs/evidence/goal-01/check_requirements.py`, and `13` insertions and `13` deletions in `docs/requirements.md` (13 matrix rows).

Checker changes, and why:

- Approved nit: threat headings match the full heading line, so `## TM-SECRETS` does not match `## TM-SECRET`.
- Approved nit: `docs/progress.md` and `docs/progress.tsv` must carry the same status for every started goal.
- Approved nit: `self_test_cases` is the number of negative fixtures executed, not a hardcoded 7.
- Approved nit, then restored after round 2: a `TEST:` disposition and its goals column are the same set of goal numbers. The check is two-way. Negation text is not stripped. Self-tests fail an extra column goal (`01,07` on a Goal 01 sentence) and fail a sentence that says `Goal 01 does not`.
- The checker allows `docs/protocol/` and `docs/evidence/goal-02/` as specification artifacts. Without that, the goal 01 checker would report the protocol drafts as application source.

Requirement rows, and why:

- `REQ-SCOPE-002`: blueprint cite is pages 20 and 28, not 20 and 29. That was the approved nit.
- `REQ-PROD-013`, `REQ-UX-013`, `REQ-SCOPE-009`, `REQ-OFF-002`, `REQ-INV-003`, `REQ-GEN-004`, `REQ-GOV-002`, `REQ-CAP-005`: the goals column was updated to the goals the existing TEST sentence already names, so the two-way check can pass.
- `REQ-API-001`: the TEST sentence now names goals 13, 19, 21, 22, and 23. The column already listed those goals. "Later goals" named none.
- `REQ-SEC-011`, `REQ-REPO-003`, `REQ-LAUNCH-001`: the TEST sentence names only the goal in the column. The statements that goal 01 does not do that later work are in the summary, because the exact checker counts every `Goal N` token in the TEST cell, including a negation.

## Round 4 review fixes

Scheduling a validator power change folds every change already waiting. The projected set must stay non-empty and must still meet the two-thirds voting-power predicate. `stacked-validator-removal` schedules removals of v1, v2, and v3 at height 30, rejects removal of v4 with `ERROR_CODE_FORBIDDEN`, then commits through height 30 and one block after that. `validator-removal-breaks-quorum` rejects a removal that would drop the available power to two thirds or below while one validator is down.

`wrong-payload-digest` expects `ERROR_CODE_INTEGRITY`. `duplicate-scheduled-validator-key` expects `ERROR_CODE_DUPLICATE_ID` for a key shared by two scheduled changes, not only a key already in the active set.

Proto3 JSON omits default values. `power` 0 is omitted from the canonical payload. A present `"power": "0"` is `ERROR_CODE_MALFORMED`. Vector `proposal-payload-validator-power-zero` is the omitted form. Its digest is `7eeb7c6d8fadfb2b28fea5d2cd42f1a85eaf2b773e79b24399d350b9b9be675a`.

The descriptor allowlist is the set of message, field, enum, enum value, oneof, service, rpc, and json names in the compiled protocol. It has 524 entries. A new or renamed name fails until it is added to that set. The pattern blocklist still applies, including `ipv4`, `ipv6`, `ipAddress`, `remote_addr`, `device_id`, `geo_location`, `viewer_history`, `payout_amount`, `bond_amount`, `fee_amount`, `coin_denom`, `inflation_rate`, `issuance`, `media_blob`, and `chunk_data`. Negative `leaks.proto` requires a hit on each of those fields. A file name or package line is not a hit. `ERROR_CODE_FEE_REJECTED`, `SIGNATURE_DOMAIN_MEDIA_MANIFEST`, `mintModuleEnabled`, and `slashingModuleEnabled` are pinned exemptions so the fee-rejection code and the media-manifest domain stay legal.

`hosting-resilience` checks l1, l2, l3, and l4 separately. The checker requires the l2 reservation assertion in `cases.py`. Deleting that line fails the checker even if l3 and l4 are still checked.

Well-known protobuf files are resolved by `protoc` itself (`protoc --descriptor_set_out=/dev/null google/protobuf/descriptor.proto`). This checker does not pass an include directory.

DEC-009 cites `council_ready` at `spec_machine.py:334`, `op_admit_provider` at `:409`, `op_disable_provider` at `:1751`, and `op_force_withdraw` at `:1777`.

## Mutation self-check

These runs were in-process patches of the final functions. The tree on disk was not edited for them.

- Ignore pending changes and project only the change being scheduled: `stacked-validator-removal` returned `ExecuteChange expected ERROR_CODE_FORBIDDEN got ok`. `duplicate-scheduled-validator-key` returned `ExecuteChange expected ERROR_CODE_DUPLICATE_ID got ok`.
- `quorum_holds` always true: `validator-removal-breaks-quorum` returned `ExecuteChange expected ERROR_CODE_QUORUM got ok`. `stacked-validator-removal` still passed.
- Delete the scheduled duplicate-key comparison: `duplicate-scheduled-validator-key` returned `ExecuteChange expected ERROR_CODE_DUPLICATE_ID got ok`.
- Replace the payload-digest comparison with `if False`: `wrong-payload-digest` returned `ProposeChange expected ERROR_CODE_INTEGRITY got ok`.
- A descriptor that is only `file { name: "negative/mint.proto" package: "negative.mint" }` produces no pattern hit. The same parser on `message_type { name: "MsgMint" }` hits `mint`.
- Allowlist: `field:ipv4_address` is reported as an extra name. An empty pin returns `descriptor allowlist is empty`.
- The l2 assertion string is present in `cases.py`. Removing it makes the pin fail.

## Runtime

Linux 6.12.94+ x86_64. Python 3.12.3. Go go1.22.2 linux/amd64. Node v22.23.4. libprotoc 3.21.12. Python package `cryptography` 43.0.3. OpenSSL 3.0.13. No chain, database, browser, FFmpeg worker, container devnet, or second machine was started.

## Tested revision

This evidence file is part of the head commit. The checkers do not read this file. They do read `docs/progress.md` and `docs/progress.tsv`.

The head sha cannot be written into this file without changing the commit. The tested tree is this commit's tree and the `Tested-tree` trailer. Confirm with `git rev-parse 'HEAD^{tree}'`.

## Counts

- Transition fixtures: 78, pinned, 0 failures.
- Valid canonical vectors: 15. Invalid: 14. Integers: 6. Arithmetic: 5.
- Descriptor allowlist entries: 524.
- Go exit 0. TypeScript exit 0. Reports match Python, including Ed25519 signatures. `languages MATCH` is printed only after that.
- Goal 01 checker: 265 requirements, 12 self-test cases, 0 errors, exit 0.
- protoc exit 0.

## Commands and output

Run 1, before this evidence file contained these logs:

```
COMMAND python3 docs/evidence/goal-01/check_requirements.py
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
exit:0

COMMAND python3 docs/evidence/goal-02/check_protocol.py
protoc exit:0
transition_cases 78
transition_case_pin 78
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 15
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
hashes
907da1f7c835652cf870c1d48c6ae55637d15a5378a608d0a8f7eb3912d50692  docs/decision-register.md
e8d7ab87e25f4b770f9fcae641cc1bfe8663d0dce042bd818e65c26998ad7359  docs/evidence/goal-01/check_requirements.py
37db5db4ba60c241cab9eb48b668ed4d0f7afdcc678ced06c886e6ace0eded77  docs/evidence/goal-02/cases.py
ded724880d232a3d4f508de42a43ad1661720cc5425b3c2cfbd5c10033250ebf  docs/evidence/goal-02/check_protocol.py
e0b344008a1ebdec3fba9b1cb22a74d4f251c23cac23d5092c67fda5387bd6ef  docs/evidence/goal-02/go/go.mod
0167772bf599c2779e16a708c6e7cb1e4f0dcacc9eedf1195cae60cf4c679c9b  docs/evidence/goal-02/go/vectorcheck.go
44c72f0c839896bd9b559f33851a512e0a8db9cbc087df2a745aefd909a765b0  docs/evidence/goal-02/jcs.py
7caaa3bf47979383afac9c8468fa65cc63ee2ebe2fe67b8e8bec218bd3a0d3a7  docs/evidence/goal-02/negative/email.proto
6afb552fd968083003159a79cf12846a3480e34063ddda78ad2730ce75fce46c  docs/evidence/goal-02/negative/ip_address.proto
d5938cbd382771a2bb0384b74ddb6425251b8ad97331df01cb1f6e264a599f77  docs/evidence/goal-02/negative/leaks.proto
ed959570306e4cadeb744a37299add6d64351aab476fd88a550039a795bac1dd  docs/evidence/goal-02/negative/media.proto
0a1da67c3881ae73b1924a5120cd088eefce711c2a79354bbc8cd5a7f7e6851e  docs/evidence/goal-02/negative/mint.proto
961101c3d3ab2aca71dc00ffc4ad980366a225d4970a87f934e1fdbfc6818500  docs/evidence/goal-02/negative/mint_tokens.proto
417cd0f5551289bbc20a27a5ddac1313e53e3bb2670ebfd36722e5e404165b5d  docs/evidence/goal-02/negative/penalize.proto
6e1e1e70b0a6cf3952eb4ae0ecabd0f2b4ffae20971380b2cf9ccd0bb6795c4a  docs/evidence/goal-02/negative/reward.proto
e1b1e71b30fc6a4098e3152f25a6830cfa4a52aa7ec5f3ea16ec989f6adcf2fc  docs/evidence/goal-02/negative/slash.proto
85da4ddcb323c76695d62da03477d93a562f458ec4024c8a397c2cc2e702655f  docs/evidence/goal-02/negative/storage_proof.proto
502e774d6bd2ba90752f8ca39d8fa10259b595f97e7978f91a1e01a193858309  docs/evidence/goal-02/negative/timestamp.proto
92c9bcb8172148f4ea65802def2a54c69d280e2c4a8b21a66a6472910f83c98c  docs/evidence/goal-02/negative/token_balance.proto
6d2816506bad14e04b00348f0a395f39968832450bca1253492792e5ed788b4e  docs/evidence/goal-02/negative/video_payload.proto
2a5a1459b33a883604bd0606cdb662b8ce52037b131dc8583aec1978f8ff6b6a  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
78261299f04647a8bb1da6a34eef7cc7260a3867fc0fc7ca136e42abe6298896  docs/progress.md
16bce2d3e9e9ab93b84999d59e6de98a3a050ca64c5025fdea73dc30b6c49828  docs/progress.tsv
d52993f4fd2368c2bf25a9e5131b7dd4b4b2622f9c62cc6e090a5d925e34e25f  docs/protocol/api/client-api.json
94a2f35aaf83f234a3fb84e344cf562d6c3c391e0467ff77c7a5590f16324c9f  docs/protocol/api/provider-api.json
15fc8fa73ef39a5b867e74e6ca1d3f41d9e1eb82a3364903ad40d2849648c81b  docs/protocol/bounds.json
eec164c9beb92a879de4b885eeceed4069c7991896097a1864c0b437026996bc  docs/protocol/fixtures/genesis-valid.json
de0735b82b34a84e7d95d8e8bc720f346d1a705582ab6fc0a222324f0794539d  docs/protocol/fixtures/manifest-invalid-boundary.json
f4b0a345b494f00b4a9bab599be25d80d9ff31bf28b017979f28c91beafb512b  docs/protocol/fixtures/manifest-invalid-duplicate-id.json
be1b53f8674503c5086af4a23468cd4fb205cf296faf7845c4d383c3a3a9f2a9  docs/protocol/fixtures/manifest-invalid-unknown.json
c6e8246ddb86788efa30f6e36eb887bc2e286fd7e2226baa5522a13790bef238  docs/protocol/fixtures/manifest-invalid-wrong-domain.json
703a4abce900576fbd08f21fd3d1a611fb49eb01fc7882d694ddd8106f4477f1  docs/protocol/fixtures/manifest-valid.json
8729de9714a97462e151a2fd50ead9f5074536117581219036bd08cdd0724477  docs/protocol/fixtures/vectors.json
39f250f2338b12284fa76d427ee7294f00f4412d77e6fb9d83ac517f11ac9e23  docs/protocol/proto/svn/app/v1/genesis.proto
1f1bdc75b15f0634c704ecb930d7df09df8aaeadde8a2082fc7436c827d57274  docs/protocol/proto/svn/authority/v1/genesis.proto
826d47717c514115a706000d35f3cc6a53ea1ebec48e8e3c8f1cc241218c5299  docs/protocol/proto/svn/authority/v1/query.proto
ec9a8cafedb4608dd8b614d9df61ac04e380cf9f85b5e7f148189c0a2b7df0ad  docs/protocol/proto/svn/authority/v1/state.proto
78a88ede813c41b6b6e43ad92fc84c487d4f74d7e9a0a0ac0b3faef422a97550  docs/protocol/proto/svn/authority/v1/tx.proto
2cd0a0748f54b16763d453f9d42dfda8e57d9f8124d464a0f24d53bd8db760da  docs/protocol/proto/svn/content/v1/genesis.proto
ece88f408b5cdb10e17ecfa7224661a9f2d3e2296b29c1b25c029c530ce14769  docs/protocol/proto/svn/content/v1/query.proto
e2cbb92ef171857e87806a0ec5a262578f8fa280a87303d193ae34b09acafb50  docs/protocol/proto/svn/content/v1/state.proto
5f2a0f3d6281df3dc02340c86bbabd591188004ee28f153778831dec718dbf27  docs/protocol/proto/svn/content/v1/tx.proto
482e78f5690b3478c67c5abddecec580e22327456f3ed5e6ab5f4a3b53cb03c4  docs/protocol/proto/svn/events/v1/events.proto
9e9316b3542141a78e7f5cde214422ed84a1214726786baa5f69ece832c17cd6  docs/protocol/proto/svn/evidence/v1/genesis.proto
7d11286f4c3f95a407644129fc97f6f0e856db8ecb16ec50610ad1612d411175  docs/protocol/proto/svn/evidence/v1/query.proto
9a411c384f7bee53b87a55c8ce2a95ae8efc334e3af02f37bc31436c1c77e4d5  docs/protocol/proto/svn/evidence/v1/state.proto
132d998783bcc73f2a4d60aeba9c425627d82f01d1ab6b0e3a5fc940042b985b  docs/protocol/proto/svn/evidence/v1/tx.proto
ba34a46e28683f9f42ce28043fd5a50dae134ed1a3dff6b29392fc6d03a0bae1  docs/protocol/proto/svn/leases/v1/genesis.proto
8ed45a73375c8b7b489325d41ece40e9bed2351990e3945136a48e6647fb010e  docs/protocol/proto/svn/leases/v1/query.proto
b404428b32d76aa264e6f8e9b6b3e5f896d40836558ed9f9fbd03c9e2e64ab51  docs/protocol/proto/svn/leases/v1/state.proto
f15e176cfa14efd84c6b6fced77c46d1e70cb9744c893b3473a363d28440ab38  docs/protocol/proto/svn/leases/v1/tx.proto
17c0758486bb5b03a732a65a2a38b1ba9bf0fd1d2087d1e0d624ffc82d67643b  docs/protocol/proto/svn/offchain/v1/records.proto
e36be8a769caef1a269231a1151f96bde6cc4c360f1732da04556f991d953c19  docs/protocol/proto/svn/policy/v1/genesis.proto
d1c6edc4b63f49a8767589a1a050f0a1c95234320f73716d949c423ccb80be62  docs/protocol/proto/svn/policy/v1/query.proto
75a34fb9d6bcc1ab03b921e255eaa7c6a35af59819797d2d0e14e0d605254a10  docs/protocol/proto/svn/policy/v1/state.proto
b791a4cc6d2d6614aef9e836bac9de9b698f29816e4b7aa80d0d45e73d2710db  docs/protocol/proto/svn/policy/v1/tx.proto
f93ca1361d8549169b65784f37e23be22624b31212dd1bb8355a02768ba6d6aa  docs/protocol/proto/svn/providers/v1/genesis.proto
d94a0458d70001c7b938abd35af1e89c49c19a7f6d8893adc5c17ee4253ce30e  docs/protocol/proto/svn/providers/v1/query.proto
a8d7866b9c3df4ae99fe6c4325dace93255c2db6c4c8e899a8af7e2b8563c16b  docs/protocol/proto/svn/providers/v1/state.proto
586c4fb43e28ddcd2234edbc05886c8d10e43e38b33e3ff45c89ff8ebdacdf1e  docs/protocol/proto/svn/providers/v1/tx.proto
e54a4045b05602d94fe0aea456a314f4e6db0390fe45b93dd10a04ca5c97a507  docs/protocol/proto/svn/types/v1/types.proto
2f2e4ec9486495fa49c7159729cab455cdeed85a2451100b99057e9128f5279d  docs/protocol/schemas/evidence-envelope.schema.json
0093a1de6721fc8275bfb48821839606a8baa83accb9f0c5e34e9aafe2f74e4e  docs/protocol/schemas/media-manifest.schema.json
d679f577326d4f796b2d74a6df7ad5b0d2d61bc573aecfdd55af1112aa1770ea  docs/protocol/schemas/provider-capability.schema.json
cb33617cc2a2c0f55da607817826f22d2c9e32ce699d69805f11dc02dcb3304e  docs/protocol/schemas/public-record.schema.json
bae60cfc97922eefbde42daf818c427e81bf6cf04f6c03a2f51d4e90669be31e  docs/protocol.md
197fded5c09c712f10ebf74459e46868ab570afb44895c1ea582b5b64a527318  docs/requirements.md
errors 0
languages MATCH
result PASS
note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled
exit:0
```

## Repeat run

Run 2 used the same two commands after this evidence file existed, before this paragraph was filled in. The checkers do not read this file. Their stdout was byte-identical to run 1. The logged stdout is:

```
COMMAND python3 docs/evidence/goal-01/check_requirements.py
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
exit:0

COMMAND python3 docs/evidence/goal-02/check_protocol.py
protoc exit:0
transition_cases 78
transition_case_pin 78
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 15
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
hashes
907da1f7c835652cf870c1d48c6ae55637d15a5378a608d0a8f7eb3912d50692  docs/decision-register.md
e8d7ab87e25f4b770f9fcae641cc1bfe8663d0dce042bd818e65c26998ad7359  docs/evidence/goal-01/check_requirements.py
37db5db4ba60c241cab9eb48b668ed4d0f7afdcc678ced06c886e6ace0eded77  docs/evidence/goal-02/cases.py
ded724880d232a3d4f508de42a43ad1661720cc5425b3c2cfbd5c10033250ebf  docs/evidence/goal-02/check_protocol.py
e0b344008a1ebdec3fba9b1cb22a74d4f251c23cac23d5092c67fda5387bd6ef  docs/evidence/goal-02/go/go.mod
0167772bf599c2779e16a708c6e7cb1e4f0dcacc9eedf1195cae60cf4c679c9b  docs/evidence/goal-02/go/vectorcheck.go
44c72f0c839896bd9b559f33851a512e0a8db9cbc087df2a745aefd909a765b0  docs/evidence/goal-02/jcs.py
7caaa3bf47979383afac9c8468fa65cc63ee2ebe2fe67b8e8bec218bd3a0d3a7  docs/evidence/goal-02/negative/email.proto
6afb552fd968083003159a79cf12846a3480e34063ddda78ad2730ce75fce46c  docs/evidence/goal-02/negative/ip_address.proto
d5938cbd382771a2bb0384b74ddb6425251b8ad97331df01cb1f6e264a599f77  docs/evidence/goal-02/negative/leaks.proto
ed959570306e4cadeb744a37299add6d64351aab476fd88a550039a795bac1dd  docs/evidence/goal-02/negative/media.proto
0a1da67c3881ae73b1924a5120cd088eefce711c2a79354bbc8cd5a7f7e6851e  docs/evidence/goal-02/negative/mint.proto
961101c3d3ab2aca71dc00ffc4ad980366a225d4970a87f934e1fdbfc6818500  docs/evidence/goal-02/negative/mint_tokens.proto
417cd0f5551289bbc20a27a5ddac1313e53e3bb2670ebfd36722e5e404165b5d  docs/evidence/goal-02/negative/penalize.proto
6e1e1e70b0a6cf3952eb4ae0ecabd0f2b4ffae20971380b2cf9ccd0bb6795c4a  docs/evidence/goal-02/negative/reward.proto
e1b1e71b30fc6a4098e3152f25a6830cfa4a52aa7ec5f3ea16ec989f6adcf2fc  docs/evidence/goal-02/negative/slash.proto
85da4ddcb323c76695d62da03477d93a562f458ec4024c8a397c2cc2e702655f  docs/evidence/goal-02/negative/storage_proof.proto
502e774d6bd2ba90752f8ca39d8fa10259b595f97e7978f91a1e01a193858309  docs/evidence/goal-02/negative/timestamp.proto
92c9bcb8172148f4ea65802def2a54c69d280e2c4a8b21a66a6472910f83c98c  docs/evidence/goal-02/negative/token_balance.proto
6d2816506bad14e04b00348f0a395f39968832450bca1253492792e5ed788b4e  docs/evidence/goal-02/negative/video_payload.proto
2a5a1459b33a883604bd0606cdb662b8ce52037b131dc8583aec1978f8ff6b6a  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
78261299f04647a8bb1da6a34eef7cc7260a3867fc0fc7ca136e42abe6298896  docs/progress.md
16bce2d3e9e9ab93b84999d59e6de98a3a050ca64c5025fdea73dc30b6c49828  docs/progress.tsv
d52993f4fd2368c2bf25a9e5131b7dd4b4b2622f9c62cc6e090a5d925e34e25f  docs/protocol/api/client-api.json
94a2f35aaf83f234a3fb84e344cf562d6c3c391e0467ff77c7a5590f16324c9f  docs/protocol/api/provider-api.json
15fc8fa73ef39a5b867e74e6ca1d3f41d9e1eb82a3364903ad40d2849648c81b  docs/protocol/bounds.json
eec164c9beb92a879de4b885eeceed4069c7991896097a1864c0b437026996bc  docs/protocol/fixtures/genesis-valid.json
de0735b82b34a84e7d95d8e8bc720f346d1a705582ab6fc0a222324f0794539d  docs/protocol/fixtures/manifest-invalid-boundary.json
f4b0a345b494f00b4a9bab599be25d80d9ff31bf28b017979f28c91beafb512b  docs/protocol/fixtures/manifest-invalid-duplicate-id.json
be1b53f8674503c5086af4a23468cd4fb205cf296faf7845c4d383c3a3a9f2a9  docs/protocol/fixtures/manifest-invalid-unknown.json
c6e8246ddb86788efa30f6e36eb887bc2e286fd7e2226baa5522a13790bef238  docs/protocol/fixtures/manifest-invalid-wrong-domain.json
703a4abce900576fbd08f21fd3d1a611fb49eb01fc7882d694ddd8106f4477f1  docs/protocol/fixtures/manifest-valid.json
8729de9714a97462e151a2fd50ead9f5074536117581219036bd08cdd0724477  docs/protocol/fixtures/vectors.json
39f250f2338b12284fa76d427ee7294f00f4412d77e6fb9d83ac517f11ac9e23  docs/protocol/proto/svn/app/v1/genesis.proto
1f1bdc75b15f0634c704ecb930d7df09df8aaeadde8a2082fc7436c827d57274  docs/protocol/proto/svn/authority/v1/genesis.proto
826d47717c514115a706000d35f3cc6a53ea1ebec48e8e3c8f1cc241218c5299  docs/protocol/proto/svn/authority/v1/query.proto
ec9a8cafedb4608dd8b614d9df61ac04e380cf9f85b5e7f148189c0a2b7df0ad  docs/protocol/proto/svn/authority/v1/state.proto
78a88ede813c41b6b6e43ad92fc84c487d4f74d7e9a0a0ac0b3faef422a97550  docs/protocol/proto/svn/authority/v1/tx.proto
2cd0a0748f54b16763d453f9d42dfda8e57d9f8124d464a0f24d53bd8db760da  docs/protocol/proto/svn/content/v1/genesis.proto
ece88f408b5cdb10e17ecfa7224661a9f2d3e2296b29c1b25c029c530ce14769  docs/protocol/proto/svn/content/v1/query.proto
e2cbb92ef171857e87806a0ec5a262578f8fa280a87303d193ae34b09acafb50  docs/protocol/proto/svn/content/v1/state.proto
5f2a0f3d6281df3dc02340c86bbabd591188004ee28f153778831dec718dbf27  docs/protocol/proto/svn/content/v1/tx.proto
482e78f5690b3478c67c5abddecec580e22327456f3ed5e6ab5f4a3b53cb03c4  docs/protocol/proto/svn/events/v1/events.proto
9e9316b3542141a78e7f5cde214422ed84a1214726786baa5f69ece832c17cd6  docs/protocol/proto/svn/evidence/v1/genesis.proto
7d11286f4c3f95a407644129fc97f6f0e856db8ecb16ec50610ad1612d411175  docs/protocol/proto/svn/evidence/v1/query.proto
9a411c384f7bee53b87a55c8ce2a95ae8efc334e3af02f37bc31436c1c77e4d5  docs/protocol/proto/svn/evidence/v1/state.proto
132d998783bcc73f2a4d60aeba9c425627d82f01d1ab6b0e3a5fc940042b985b  docs/protocol/proto/svn/evidence/v1/tx.proto
ba34a46e28683f9f42ce28043fd5a50dae134ed1a3dff6b29392fc6d03a0bae1  docs/protocol/proto/svn/leases/v1/genesis.proto
8ed45a73375c8b7b489325d41ece40e9bed2351990e3945136a48e6647fb010e  docs/protocol/proto/svn/leases/v1/query.proto
b404428b32d76aa264e6f8e9b6b3e5f896d40836558ed9f9fbd03c9e2e64ab51  docs/protocol/proto/svn/leases/v1/state.proto
f15e176cfa14efd84c6b6fced77c46d1e70cb9744c893b3473a363d28440ab38  docs/protocol/proto/svn/leases/v1/tx.proto
17c0758486bb5b03a732a65a2a38b1ba9bf0fd1d2087d1e0d624ffc82d67643b  docs/protocol/proto/svn/offchain/v1/records.proto
e36be8a769caef1a269231a1151f96bde6cc4c360f1732da04556f991d953c19  docs/protocol/proto/svn/policy/v1/genesis.proto
d1c6edc4b63f49a8767589a1a050f0a1c95234320f73716d949c423ccb80be62  docs/protocol/proto/svn/policy/v1/query.proto
75a34fb9d6bcc1ab03b921e255eaa7c6a35af59819797d2d0e14e0d605254a10  docs/protocol/proto/svn/policy/v1/state.proto
b791a4cc6d2d6614aef9e836bac9de9b698f29816e4b7aa80d0d45e73d2710db  docs/protocol/proto/svn/policy/v1/tx.proto
f93ca1361d8549169b65784f37e23be22624b31212dd1bb8355a02768ba6d6aa  docs/protocol/proto/svn/providers/v1/genesis.proto
d94a0458d70001c7b938abd35af1e89c49c19a7f6d8893adc5c17ee4253ce30e  docs/protocol/proto/svn/providers/v1/query.proto
a8d7866b9c3df4ae99fe6c4325dace93255c2db6c4c8e899a8af7e2b8563c16b  docs/protocol/proto/svn/providers/v1/state.proto
586c4fb43e28ddcd2234edbc05886c8d10e43e38b33e3ff45c89ff8ebdacdf1e  docs/protocol/proto/svn/providers/v1/tx.proto
e54a4045b05602d94fe0aea456a314f4e6db0390fe45b93dd10a04ca5c97a507  docs/protocol/proto/svn/types/v1/types.proto
2f2e4ec9486495fa49c7159729cab455cdeed85a2451100b99057e9128f5279d  docs/protocol/schemas/evidence-envelope.schema.json
0093a1de6721fc8275bfb48821839606a8baa83accb9f0c5e34e9aafe2f74e4e  docs/protocol/schemas/media-manifest.schema.json
d679f577326d4f796b2d74a6df7ad5b0d2d61bc573aecfdd55af1112aa1770ea  docs/protocol/schemas/provider-capability.schema.json
cb33617cc2a2c0f55da607817826f22d2c9e32ce699d69805f11dc02dcb3304e  docs/protocol/schemas/public-record.schema.json
bae60cfc97922eefbde42daf818c427e81bf6cf04f6c03a2f51d4e90669be31e  docs/protocol.md
197fded5c09c712f10ebf74459e46868ab570afb44895c1ea582b5b64a527318  docs/requirements.md
errors 0
languages MATCH
result PASS
note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled
exit:0
```
