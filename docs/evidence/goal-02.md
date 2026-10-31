# Goal 02 evidence

Status: IMPLEMENTED

The schema and fixture checks below passed on this tree. Max accepted the launch allocation as reported effort for goals 01 and 02 at Gate A on 2026-10-31 (America/Toronto): grok-4.7, xhigh reasoning. This file does not mark the goal VERIFIED. Protocol v0 freezes once this goal passes review. This revision is the candidate. It does not claim that review has passed.

The checks are not a devnet, a security audit, or an approval of the protocol. The protocol is not an approved protocol. Public launch: DISABLED. No tradable token, coin, sale, or mining.

Pull request: https://github.com/Maxencejules/shortvideo-network/pull/5

## Gate A

Date: 2026-10-31 (America/Toronto).

At 06:58 Max said "I take all your recommendations".

Reviewer's reading: this approves DEC-009, DEC-010, DEC-011, and DEC-012.

At 06:59 Max said "Yes let's do it", in reply to the reviewer's 4-question card.

Reviewer's reading: this accepts the recommended option on every question. DEC-009 is council-only for admit, disable, and force-withdraw, with no single-signature path. DEC-001 through DEC-008 accept every PROPOSED default. The launch allocation (grok-4.7, xhigh reasoning) counts as reported effort for goals 01 and 02. SVN-JCS-1, SHA-256, Ed25519, the proposed signing domains, the safety and governed caps, and the shortvideo-dev- prefix are accepted. Protocol v0 freezes once goal 02 passes review.

## Liveness
Liveness can still halt an accepted schedule. With v4 down, removing v1 at height 30 halts the chain.

The hashed check refuses that removal, so it is not scheduled. The remaining set would be v2, v3, and v4, with v4 down, and live power would not commit. `liveness-halt-example` records that operator fact, records that the removal is `ERROR_CODE_FORBIDDEN` and the proposal is `REJECTED`, and records that `CommitBlock` with v4 down on the original four validators still succeeds. `down` is not an application-hash input. `commit_possible` does not read it.

## Model and effort

- Playbook assignment for goal 02: GPT-6 Astra, effort EXTRA HIGH. Astra was not used.
- Model actually used: `grok-4.7`.
- Effort actually used: xhigh reasoning. Max accepted the launch allocation as the reported effort for goals 01 and 02. cursor-cloud `run-info` reports `originalModelName=grok-4.7` and `bcId=bc-763364ae-2b17-5737-aa7d-39bb2a64a0c0`. That payload has no effort field.
- `docs/evidence/goal-01.md` and goal 01's status are unchanged.
- Extra effort does not certify correctness.

## Approved decisions in this tree

- DEC-009: admit, disable, and force-withdraw are council-only. The proto service no longer has `MsgAdmitProvider`, `MsgDisableProvider`, or `MsgForceWithdraw`. The direct ops return `ERROR_CODE_FORBIDDEN`. `single-sig-authority-rejected` covers all three. Test setup uses `FixtureAdmitProvider`, which is not a chain message.
- DEC-010: `REJECTED` is derived. `observation_digest` present and different from the lease manifest records `REJECTED`. There is no submitter `reject` field. `evidence-reject-flag-unknown` rejects a leftover `reject` flag as `ERROR_CODE_UNKNOWN_FIELD`.
- DEC-011: liveness stays out of the hash. Every activation point keeps at least four validators and `max_power * 3 < total_power`. `power_set_invariant` replaced the empty `quorum_holds(validators, [])` check. `validator-power-invariant` fails if that check is neutered.
- DEC-012: a height change beside a time change is `ERROR_CODE_CONFLICT`. CONFLICT leaves the proposal `QUEUED`. `ERROR_CODE_FORBIDDEN` marks it `REJECTED`. `mixed-activation-kind` and `proposal-rejected` cover those two outcomes.
- Two changes to the same node or the same consensus key at one activation point are `ERROR_CODE_CONFLICT` before either is applied. `same-point-power-change` includes a removal whose proposal id sorts first and an add of that same key. A duplicate key at a different activation point stays `ERROR_CODE_DUPLICATE_ID` and the proposal stays `QUEUED`.

Names that match no forbidden pattern are stopped only by the descriptor allowlist. A diff that adds or renames an entry in that pinned inventory needs a human review. The pattern list does not review it.

The devnet council fixture is still two approvals of three seats and a delay of 10 seconds, labeled `PROPOSED_TEST_PARAMETER`. DEC-008's accepted default is four seats and three approvals. The fixture is not that policy.

## What was regenerated

- Descriptor allowlist: 524 names to 513. Removed `field:reject`, `json_name:reject`, `message:MsgAdmitProvider`, `message:MsgAdmitProviderResponse`, `message:MsgDisableProvider`, `message:MsgDisableProviderResponse`, `message:MsgForceWithdraw`, `message:MsgForceWithdrawResponse`, `rpc:AdmitProvider`, `rpc:DisableProvider`, and `rpc:ForceWithdraw`. No names were added.
- `docs/protocol/fixtures/vectors.json` did not change. Its sha256 is `8729de9714a97462e151a2fd50ead9f5074536117581219036bd08cdd0724477`.
- The replay golden hash did not change: `ffee724a8b7944512db064250664d46b7a519d2062134b5cdda9a03ca202437d`.

## Mutation self-check

In-process patches. The tree on disk was not edited for them.

- `power_set_invariant` returned `None`: `proposal-rejected`, `stacked-validator-removal`, `validator-power-invariant`, `activation-gap-halt`, and `liveness-halt-example` each returned `ExecuteChange expected ERROR_CODE_FORBIDDEN got ok`.
- `commit_possible` read `down` through `quorum_holds`: `stall-two-validators` returned `CommitBlock expected ok got ERROR_CODE_QUORUM`.
- The same-point node and key check was removed: `same-point-power-change` returned `ExecuteChange expected ERROR_CODE_CONFLICT got ok`.
- `l2_reservation_problems` returned `[]`: `hosting-resilience` returned `flag-only l2 flip was accepted`.
- On the real helper, a flag-only `reservation_held` flip returned `['l2 flag']`. Clearing `reserved_bytes` returned `['l2 bytes']`. The untouched l2 state returned `[]`.

## Runtime

Linux 6.12.94+ x86_64. Python 3.12.3. Go go1.22.2 linux/amd64. Node v22.23.4. libprotoc 3.21.12. Python package `cryptography` 43.0.3. OpenSSL 3.0.13.

## Tested revision

This evidence file is part of the head commit. The checkers do not read it. They do read `docs/progress.md` and `docs/progress.tsv`.

The tested tree is this commit's tree and the `Tested-tree` trailer. Confirm with `git rev-parse 'HEAD^{tree}'`.

Parent of this round: `e59f699b5b294e3d0efdc713d94424323995eed5`.

## Counts

- Transition fixtures: 86, pinned, 0 failures.
- Valid canonical vectors: 15. Invalid: 14. Integers: 6. Arithmetic: 5.
- Descriptor allowlist entries: 513.
- Go exit 0. TypeScript exit 0. `languages MATCH`.
- Goal 01 checker exit 0. protoc exit 0.
- `docs/evidence/goal-01.md` is unchanged. Versus `783e159`, `docs/evidence/goal-01/check_requirements.py` is 190 insertions and 31 deletions, and `docs/requirements.md` is 15 insertions and 15 deletions. The goal 01 checker now requires `Status: APPROVED` for DEC-001 through DEC-008 and fails if `Status: OPEN` remains. Requirements do not cite DEC-009 through DEC-012.

## Commands and output

Run 1 and run 2 used the same checker inputs. Run 2 stdout was byte-identical to run 1 (`cmp` equal). Both commands exited 0.

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
EXIT:0

COMMAND python3 docs/evidence/goal-02/check_protocol.py
protoc exit:0
transition_cases 86
transition_case_pin 86
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 15
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
hashes
4ab75dcca4553166dbd2bad2227e4f66308d676fe8090557f3ca440213184004  docs/decision-register.md
77dbb119c755e847bb5a51159ae3f75edcfcc2c07b0076eabd8a0b1a2cc10249  docs/evidence/goal-01/check_requirements.py
213c09f82c6c60aa696ed4f273503fcc8a1067c9066d5cb87cbeca2be524c3c5  docs/evidence/goal-02/cases.py
43c2d0f9622332cbf5227c9a593002ed441e5c83800ba73d4fe91a6a75f41592  docs/evidence/goal-02/check_protocol.py
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
af8e5c589f926d9b9c49b51ecec24c0c62c88ca8e49b8305fa16fe4eab3071b4  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
236d8faf8980ffabbf985dead0072092d33450d61ba8dd198e94ace6b5bfcf4d  docs/progress.md
afdfca95fae1a23a50914131172d58d67d5dd6de706b07af9fd4cceb30b84ed4  docs/progress.tsv
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
48b8b96b539a245412fa4e007397ab0c7bc0bf7374e8bc5f97474528662b93a0  docs/protocol/proto/svn/app/v1/genesis.proto
9bbe1a6314a911288473641861459722a001a707b999f1df95da27407a5aed5d  docs/protocol/proto/svn/authority/v1/genesis.proto
9aa5f64ed65b6f8179a9222ead115cdedd768132fed1ab59087c94b53ae0f68a  docs/protocol/proto/svn/authority/v1/query.proto
d3f6a4491d4f0da001002ed5530d25b917bac99440deda8a78c4fdf99030c7fc  docs/protocol/proto/svn/authority/v1/state.proto
19bf544c9b4ac1fe65386cce239fe3b4c394c1b95d854aa1b44b52dacccf7fb8  docs/protocol/proto/svn/authority/v1/tx.proto
23a18541dc700a8edee4b0198dfebd0ea3a6df1f9915b9dffbeaec218556efbb  docs/protocol/proto/svn/content/v1/genesis.proto
887ea9ef25e21068c7a81d4d45b00317239e208db15227cc0737767c11c91a09  docs/protocol/proto/svn/content/v1/query.proto
0d8f97c4cc156270bf8c6fe453a628cfa74e919769a4ef4027ddbc651e0cc5f2  docs/protocol/proto/svn/content/v1/state.proto
9c04368a00baac4aa9bb092b2031d6566efa6a196a5b377c04518f4e0ce46300  docs/protocol/proto/svn/content/v1/tx.proto
3de8ebd33d133f74eafb5128cdd09b1cf625cc1c8faeade2ebc7521272e59f98  docs/protocol/proto/svn/events/v1/events.proto
f9c7f291e50f9224b4d152c6b5a4d8a9f6ee5f81950bbcde2c1ee0c3540fc50f  docs/protocol/proto/svn/evidence/v1/genesis.proto
688f7d58b308ecf3417bab33e32b2594ed2c262ef78f302b32c0bb1115a73932  docs/protocol/proto/svn/evidence/v1/query.proto
8a461e6936bdb171584a677c156e63f3b4b5329053e572cc87159d9e770957bf  docs/protocol/proto/svn/evidence/v1/state.proto
b9405205ee7fa304b5418e0febf05886b76d22a9122292b87829cc729230a135  docs/protocol/proto/svn/evidence/v1/tx.proto
1a818e14a4094247795868d6536d110c37c5d0e63150749f12515d1c1ef94224  docs/protocol/proto/svn/leases/v1/genesis.proto
38694e568ac9f97ef43aef94510469fc7401488047a46f0496a786d0319097aa  docs/protocol/proto/svn/leases/v1/query.proto
0c1131c955b2bd2c7d7d1e5b36e789b3f58e313d0a413a1844bf38db30a6f8fc  docs/protocol/proto/svn/leases/v1/state.proto
f3acba5925a8ed3b8b1ae686934fd17f85f38efb2cc107fc492ff6cb9ef78611  docs/protocol/proto/svn/leases/v1/tx.proto
0723269bacc329df94fd0c7dbbc7b85dc60fbe9c6be9c3fd57d305d5cf2015f1  docs/protocol/proto/svn/offchain/v1/records.proto
9d8bcc798c5c8735fdfccc55f060f67f49f0d0080baa77ca0ce29f94ac9bc488  docs/protocol/proto/svn/policy/v1/genesis.proto
81c0059a2a497a142b26e18a3cda51ed87c759331338d6dc93062c45ebb17167  docs/protocol/proto/svn/policy/v1/query.proto
db1a9b9d273d2fa47a99e7865c9bbb99525ead8d8e820e7a20ed72ef5b966724  docs/protocol/proto/svn/policy/v1/state.proto
4fc33686daf1c23c1a6d19de60246b37247f0bc15989ee6d095cae3f767aa328  docs/protocol/proto/svn/policy/v1/tx.proto
9aef3a7196eb54c5edb0f4d0c26bb4ec2d11f2c3643a6725989c8c639f64f451  docs/protocol/proto/svn/providers/v1/genesis.proto
1540131227dd5b9cf418395eb1e14c011b23d0f370002ca0511287342bd725e8  docs/protocol/proto/svn/providers/v1/query.proto
2e026ae7d279774b75d711a7be89b22f7b029e1d7c3cd04e1fa8087f0c45acdf  docs/protocol/proto/svn/providers/v1/state.proto
9ab96f5cd4b108e103a1ce9fd984dbc6d30b80f1bf4715988e2bd4d94e1e1313  docs/protocol/proto/svn/providers/v1/tx.proto
e60f3b2ce334b2ab5d59e686ca78c3804509abc5b441ca089e6fba41ab847524  docs/protocol/proto/svn/types/v1/types.proto
2f2e4ec9486495fa49c7159729cab455cdeed85a2451100b99057e9128f5279d  docs/protocol/schemas/evidence-envelope.schema.json
0093a1de6721fc8275bfb48821839606a8baa83accb9f0c5e34e9aafe2f74e4e  docs/protocol/schemas/media-manifest.schema.json
d679f577326d4f796b2d74a6df7ad5b0d2d61bc573aecfdd55af1112aa1770ea  docs/protocol/schemas/provider-capability.schema.json
cb33617cc2a2c0f55da607817826f22d2c9e32ce699d69805f11dc02dcb3304e  docs/protocol/schemas/public-record.schema.json
7e23ecc9346c8f8135c5d74c3af2eae5dfab1db4439fcf9a69c7e7b3b86b4dfb  docs/protocol.md
5e50de124248b97fce75d51d01b27d1375bacb5c9832ea75421a10f597bc9d96  docs/requirements.md
errors 0
languages MATCH
result PASS
note schema and fixture check; protocol not approved; Gate A approved DEC-001 through DEC-012; v0 freezes once review passes; public launch disabled
EXIT:0
```

Page 26 playback, processing, load, recovery, moderation-timing, and operator-independence measurements were not run and are not passes.

## Residual

The requirements matrix still describes several later-goal rows as waiting on an answer. Those rows were not rewritten in this round. The decision register is the Gate A record. Production council keys are not named. The on-chain `approved` flag of a blocked-decision parameter stays false.
