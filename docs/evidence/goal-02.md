# Goal 02 evidence

Status: IMPLEMENTED

The schema and fixture checks below passed on this tree. The goal is not VERIFIED. The effort setting cannot be confirmed from inside this VM, and that point is waiting for Max's ruling at Gate A. AGENTS.md says an unreported or unverifiable effort setting for goals 01-30 is BLOCKED rather than VERIFIED. This goal is therefore IMPLEMENTED.

The checks are not a devnet, a security audit, or an approval of the protocol. The protocol is not approved. Public launch: DISABLED. No tradable token, coin, sale, or mining.

DEC-001 through DEC-012 stay OPEN. Page 26 playback, processing, load, recovery, moderation-timing, and operator-independence measurements were not run and are not passes.

Pull request: https://github.com/Maxencejules/shortvideo-network/pull/5

## Model and effort

- Playbook assignment for goal 02: GPT-6 Astra, effort EXTRA HIGH. Astra was not used.
- Launch parameter, not an observation: the task that started this run requested model `grok-4.7` with `reasoning_effort=xhigh`.
- What this VM can see: cursor-cloud `run-info` reports `originalModelName=grok-4.7` and `bcId=bc-763364ae-2b17-5737-aa7d-39bb2a64a0c0`. That payload has no effort field. The effort setting cannot be independently verified from inside the VM.
- Until Max rules at Gate A, goal 02 stays IMPLEMENTED and is not marked VERIFIED.
- `docs/evidence/goal-01.md` and goal 01's status are unchanged.
- Extra effort would not certify correctness.

## Round 5

Scheduling simulates every waiting power change in due order and checks the validator set after each activation point. A later add does not excuse an earlier point that would leave no commit. `activation-gap-halt` adds v5-v8 at height 60 and removes v1-v4 at height 40. The removal of v4 is `ERROR_CODE_FORBIDDEN`. Commits through height 40 and one block after that still succeed. `activation-gap-keeps-one` removes only v1-v3 at height 40 and adds v5 at height 60. Both points stay committable, and v5 is applied at height 60.

`down` is not read by that schedule check, and it stays outside the application hash. `down-not-consensus` runs one removal with and without `SetDown` and requires the same success, the same proposal state, and the same app hash. DEC-011 is OPEN on whether liveness belongs in consensus state.

Pending power changes must share one activation kind. A height change beside a time change is `ERROR_CODE_CONFLICT`. DEC-012 is OPEN on how to order a height against a chain time. `mixed-activation-kind` is an add at a chain time followed by a removal at a height.

The apply-time duplicate-key branch in `commit_validator_power` is gone. The scheduled duplicate-key case still covers the check inside the projection.

`hosting-resilience` asks p2 to lower capacity below the reserved lease. That step fails with `ERROR_CODE_CAPACITY` only while l2's reservation is held. The checker no longer pins the `if` line.

Names that match no forbidden pattern are stopped only by the descriptor allowlist. A diff that adds or renames an entry in that pinned inventory needs a human review. The pattern list does not review it.

## Mutation self-check

In-process patches of the final functions. The tree on disk was not edited for them.

- Checkpoint only the final set: `activation-gap-halt` returned `ExecuteChange expected ERROR_CODE_FORBIDDEN got ok`. `activation-gap-keeps-one` and `stacked-validator-removal` still passed.
- Scheduling quorum reads `state["down"]`: `down-not-consensus` returned `ExecuteChange expected ok got ERROR_CODE_QUORUM`.
- Mixed activation kinds allowed: `mixed-activation-kind` returned `ExecuteChange expected ERROR_CODE_CONFLICT got ERROR_CODE_NOT_FOUND`.
- l2 reservation cleared before the capacity step: `UpdateProvider expected ERROR_CODE_CAPACITY got ok`.

## Runtime

Linux 6.12.94+ x86_64. Python 3.12.3. Go go1.22.2 linux/amd64. Node v22.23.4. libprotoc 3.21.12. Python package `cryptography` 43.0.3. OpenSSL 3.0.13.

## Tested revision

This evidence file is part of the head commit. The checkers do not read it. They do read `docs/progress.md` and `docs/progress.tsv`.

The tested tree is this commit's tree and the `Tested-tree` trailer. Confirm with `git rev-parse 'HEAD^{tree}'`.

## Counts

- Transition fixtures: 81, pinned, 0 failures.
- Valid canonical vectors: 15. Invalid: 14. Integers: 6. Arithmetic: 5.
- Descriptor allowlist entries: 524.
- Go exit 0. TypeScript exit 0. `languages MATCH` only after the reports match.
- Goal 01 checker exit 0. protoc exit 0.
- New open decisions: DEC-011, DEC-012.

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
transition_cases 81
transition_case_pin 81
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 15
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
hashes
7a5ad93e9edea5d47a40e816b1937ef2772cb5a623225cd08543bc13758e32c9  docs/decision-register.md
e8d7ab87e25f4b770f9fcae641cc1bfe8663d0dce042bd818e65c26998ad7359  docs/evidence/goal-01/check_requirements.py
c9253406a684ba0f3eb189f1bab28aa2ddfee2c2b82557d4e5b930e5caa20003  docs/evidence/goal-02/cases.py
556863b78873fbde49f7387a2364c0a9d4ba45532a61d49acc2e56b0336a544f  docs/evidence/goal-02/check_protocol.py
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
35e8d18bb7508093524f0df8455879ac19caed006b6ace8127d2c7f83315f1ec  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
99e01ad086e6db551f342ce33f362e45616582c15a8e020fb73f6ef746368d26  docs/progress.md
3b9371ae7b65e61648121e299967aca73de48887d6d1a4cfd5265adf21ec1e3d  docs/progress.tsv
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
8363de52ae2a470fc908ad7c86807bb6ecdd2fb0856e51e327e8543fa54cfc29  docs/protocol.md
197fded5c09c712f10ebf74459e46868ab570afb44895c1ea582b5b64a527318  docs/requirements.md
errors 0
languages MATCH
result PASS
note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled
exit:0
```

## Repeat run

Run 2 used the same two commands after this file existed and before this paragraph was filled in. The checkers do not read this file. Stdout was byte-identical to run 1.

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
transition_cases 81
transition_case_pin 81
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 15
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
hashes
7a5ad93e9edea5d47a40e816b1937ef2772cb5a623225cd08543bc13758e32c9  docs/decision-register.md
e8d7ab87e25f4b770f9fcae641cc1bfe8663d0dce042bd818e65c26998ad7359  docs/evidence/goal-01/check_requirements.py
c9253406a684ba0f3eb189f1bab28aa2ddfee2c2b82557d4e5b930e5caa20003  docs/evidence/goal-02/cases.py
556863b78873fbde49f7387a2364c0a9d4ba45532a61d49acc2e56b0336a544f  docs/evidence/goal-02/check_protocol.py
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
35e8d18bb7508093524f0df8455879ac19caed006b6ace8127d2c7f83315f1ec  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
99e01ad086e6db551f342ce33f362e45616582c15a8e020fb73f6ef746368d26  docs/progress.md
3b9371ae7b65e61648121e299967aca73de48887d6d1a4cfd5265adf21ec1e3d  docs/progress.tsv
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
8363de52ae2a470fc908ad7c86807bb6ecdd2fb0856e51e327e8543fa54cfc29  docs/protocol.md
197fded5c09c712f10ebf74459e46868ab570afb44895c1ea582b5b64a527318  docs/requirements.md
errors 0
languages MATCH
result PASS
note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled
exit:0
```
