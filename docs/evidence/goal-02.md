# Goal 02 evidence

Status: VERIFIED

The verification is the schema and fixture check for this goal. It is not a devnet, a four-validator process test, a media test, a security audit, or an approval of the protocol. The protocol is not approved. Public launch: DISABLED. No tradable token, coin, sale, or mining.

DEC-001 through DEC-008 stay OPEN. That record is required. It is not a failed check. Page 26 playback, processing, load, recovery, moderation-timing, and operator-independence measurements were not run and are not passes. The contract fixtures below express the page 26 failure predicates that this goal can execute without a chain.

Pull request: https://github.com/Maxencejules/shortvideo-network/pull/5

## Model and effort

- Playbook assignment for goal 02: GPT-6 Astra, effort EXTRA HIGH.
- Model actually used: `grok-4.7`.
- Effort actually used: `reasoning_effort=xhigh`. The task that launched this run stated that setting. `fast=false`. `context=500k`.
- Runtime report from cursor-cloud `run-info`: `originalModelName=grok-4.7`, `bcId=bc-763364ae-2b17-5737-aa7d-39bb2a64a0c0`, run URL `https://cursor.com/agents/bc-763364ae-2b17-5737-aa7d-39bb2a64a0c0`. That payload has no effort, reasoning, fast, or context field.
- Astra was not used. GPT-6.1 Sol was not used.
- Extra effort does not certify correctness.

## Runtime

Linux 6.12.94+ x86_64. Python 3.12.3. Go go1.22.2 linux/amd64. Node v22.23.4. libprotoc 3.21.12. No chain, database, browser, FFmpeg worker, container devnet, or second machine was started.

## Base

Branch `goal-02` was created from `783e15911b3ea35eefd060932e5a6a317b0f9402` (`goal 01: beta requirements, threat model, and decision register`). Goal 01 evidence is `docs/evidence/goal-01.md`. DEC-001 through DEC-008 were OPEN in that revision and are still OPEN.

## Tested revision

This evidence file is part of the head commit. The checkers do not read this file. They do read `docs/progress.md` and `docs/progress.tsv`, so those hashes below are the bytes in this commit.

The head sha cannot be written into this file without changing the commit. The tested tree is this commit's tree. It is the `Tested-tree` trailer. Confirm with `git rev-parse 'HEAD^{tree}'`.

The protocol checker was run again after this evidence file existed. That repeat exited 0 and its stdout matched the protocol checker section below.

## Goal 01 review nits folded into the shared checker

These edits are in `docs/evidence/goal-01/check_requirements.py` and `docs/requirements.md`. They do not change the Goal 01 status.

- `REQ-SCOPE-002` now cites blueprint pages 20 and 28. The summary column carries that citation. Page 29 is the glossary and is no longer the cite.
- The checker compares `docs/progress.md` with `docs/progress.tsv`: a started goal has one TSV row, the status strings match, and a `NOT STARTED` goal is absent from the TSV.
- For every `TEST:` row, the goals column and the goal numbers named in the disposition are the same set. Thirteen rows were aligned so a round-1 mismatch would fail.
- `self_test_cases` is the number of negative fixtures actually executed. This run reports 10.
- Threat headings match the full heading line. `## TM-SECRETS` does not satisfy `## TM-SECRET`.

## Checker-input sha256

The protocol checker prints these hashes. They include the progress files.

## Commands and output

```
COMMAND python3 -m py_compile docs/evidence/goal-01/check_requirements.py docs/evidence/goal-02/check_protocol.py docs/evidence/goal-02/jcs.py docs/evidence/goal-02/spec_machine.py docs/evidence/goal-02/cases.py
exit:0

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
self_test_cases 10
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
transition_cases 39
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
languages MATCH
valid_vectors 13
invalid_vectors 10
integer_vectors 6
arithmetic_vectors 5
hashes
f464e653435bc43d88b99027896f5bbd5055c9f18922de6be5156c554536fbf4  docs/decision-register.md
de49397b5fa040e47bd626cb629fc4ee9dbf89a4211f7c8391f9d200adccc4b1  docs/evidence/goal-01/check_requirements.py
7a987da8ec4ae94c1c5ee34ba8ee802f411d5fb1a18638d1cfae16644bf720a8  docs/evidence/goal-02/cases.py
8ef9c6780c6f3894d28d60f0f2804c76c2c945b27b5798f1389474892913539b  docs/evidence/goal-02/check_protocol.py
e0b344008a1ebdec3fba9b1cb22a74d4f251c23cac23d5092c67fda5387bd6ef  docs/evidence/goal-02/go/go.mod
1e32c528175be56ff94fb5eadb389104bcbc682f5680a2c149025e2238ce692e  docs/evidence/goal-02/go/vectorcheck.go
47808fdca46918e249da1636f0ee3b9c87785480705e29c994d16c7d1d5ea193  docs/evidence/goal-02/jcs.py
e5ed3598ae7cbdb0a14ade8b2a8bb9681bcf30d80bb347446b0d5de3b94a264c  docs/evidence/goal-02/spec_machine.py
b652e17ef9c6fc230ed3d6fff98e6deecdac2d5db04baedba5d3b818fec67713  docs/evidence/goal-02/ts/vectorcheck.ts
96da5ace644810f760ecdb74d1735eb383c017b3f695995208610c5710b3574a  docs/progress.md
21fdad601742688dfdf073b608d422738ad5922dac3d5c6a1198784152409947  docs/progress.tsv
d52993f4fd2368c2bf25a9e5131b7dd4b4b2622f9c62cc6e090a5d925e34e25f  docs/protocol/api/client-api.json
94a2f35aaf83f234a3fb84e344cf562d6c3c391e0467ff77c7a5590f16324c9f  docs/protocol/api/provider-api.json
586a211cd073d29f30a9bcb8620de58117cb9e73ae25f36482af22aa93ec0917  docs/protocol/bounds.json
eec164c9beb92a879de4b885eeceed4069c7991896097a1864c0b437026996bc  docs/protocol/fixtures/genesis-valid.json
7b1d7b332e560968f2f4035e982462975a1e8b37f8681d9a33785218aed819c4  docs/protocol/fixtures/manifest-invalid.json
703a4abce900576fbd08f21fd3d1a611fb49eb01fc7882d694ddd8106f4477f1  docs/protocol/fixtures/manifest-valid.json
60d558bf69cf103621b338df349b04aff7cb7d7a6b31c6630bcfff0afd0bb579  docs/protocol/fixtures/vectors.json
39f250f2338b12284fa76d427ee7294f00f4412d77e6fb9d83ac517f11ac9e23  docs/protocol/proto/svn/app/v1/genesis.proto
1f1bdc75b15f0634c704ecb930d7df09df8aaeadde8a2082fc7436c827d57274  docs/protocol/proto/svn/authority/v1/genesis.proto
826d47717c514115a706000d35f3cc6a53ea1ebec48e8e3c8f1cc241218c5299  docs/protocol/proto/svn/authority/v1/query.proto
57ee9ab0ecfc4859e35c7a2d701c0b5875542acf1ef9d66e6ac14b082d244b9f  docs/protocol/proto/svn/authority/v1/state.proto
ea57073ed014e8e0a675127e8f0c758dff5ee13f90b7ad7aadc130536faf92ae  docs/protocol/proto/svn/authority/v1/tx.proto
2cd0a0748f54b16763d453f9d42dfda8e57d9f8124d464a0f24d53bd8db760da  docs/protocol/proto/svn/content/v1/genesis.proto
ece88f408b5cdb10e17ecfa7224661a9f2d3e2296b29c1b25c029c530ce14769  docs/protocol/proto/svn/content/v1/query.proto
e2cbb92ef171857e87806a0ec5a262578f8fa280a87303d193ae34b09acafb50  docs/protocol/proto/svn/content/v1/state.proto
136df10676fb6181a0fd904d532176a48920d154d72441ec90c9a66a0c9969b4  docs/protocol/proto/svn/content/v1/tx.proto
482e78f5690b3478c67c5abddecec580e22327456f3ed5e6ab5f4a3b53cb03c4  docs/protocol/proto/svn/events/v1/events.proto
9e9316b3542141a78e7f5cde214422ed84a1214726786baa5f69ece832c17cd6  docs/protocol/proto/svn/evidence/v1/genesis.proto
7d11286f4c3f95a407644129fc97f6f0e856db8ecb16ec50610ad1612d411175  docs/protocol/proto/svn/evidence/v1/query.proto
9a411c384f7bee53b87a55c8ce2a95ae8efc334e3af02f37bc31436c1c77e4d5  docs/protocol/proto/svn/evidence/v1/state.proto
b02ddd6640c27607de8f15e3a6bc7c0d2e37ee2c9c381481d3b72b945bac54ad  docs/protocol/proto/svn/evidence/v1/tx.proto
ba34a46e28683f9f42ce28043fd5a50dae134ed1a3dff6b29392fc6d03a0bae1  docs/protocol/proto/svn/leases/v1/genesis.proto
8ed45a73375c8b7b489325d41ece40e9bed2351990e3945136a48e6647fb010e  docs/protocol/proto/svn/leases/v1/query.proto
b404428b32d76aa264e6f8e9b6b3e5f896d40836558ed9f9fbd03c9e2e64ab51  docs/protocol/proto/svn/leases/v1/state.proto
b426270e67f0272e8486cc969c8da521b8dc8681b287ab71d23c17dc0373a15f  docs/protocol/proto/svn/leases/v1/tx.proto
17c0758486bb5b03a732a65a2a38b1ba9bf0fd1d2087d1e0d624ffc82d67643b  docs/protocol/proto/svn/offchain/v1/records.proto
e36be8a769caef1a269231a1151f96bde6cc4c360f1732da04556f991d953c19  docs/protocol/proto/svn/policy/v1/genesis.proto
d1c6edc4b63f49a8767589a1a050f0a1c95234320f73716d949c423ccb80be62  docs/protocol/proto/svn/policy/v1/query.proto
75a34fb9d6bcc1ab03b921e255eaa7c6a35af59819797d2d0e14e0d605254a10  docs/protocol/proto/svn/policy/v1/state.proto
b791a4cc6d2d6614aef9e836bac9de9b698f29816e4b7aa80d0d45e73d2710db  docs/protocol/proto/svn/policy/v1/tx.proto
f93ca1361d8549169b65784f37e23be22624b31212dd1bb8355a02768ba6d6aa  docs/protocol/proto/svn/providers/v1/genesis.proto
d94a0458d70001c7b938abd35af1e89c49c19a7f6d8893adc5c17ee4253ce30e  docs/protocol/proto/svn/providers/v1/query.proto
a8d7866b9c3df4ae99fe6c4325dace93255c2db6c4c8e899a8af7e2b8563c16b  docs/protocol/proto/svn/providers/v1/state.proto
51c1e64f7e158c6b00be471a49f2d0e8e3dbf8e928b6f9281fe6c279031d793a  docs/protocol/proto/svn/providers/v1/tx.proto
e54a4045b05602d94fe0aea456a314f4e6db0390fe45b93dd10a04ca5c97a507  docs/protocol/proto/svn/types/v1/types.proto
29487c6ccd442fd80d745965f694ba859195c409395d3c1887f6487b06f739e8  docs/protocol/schemas/evidence-envelope.schema.json
eaf22eea498ca9aa40e9f405312b5b51dc992ae11da61b3dcc0475e55e85815c  docs/protocol/schemas/media-manifest.schema.json
8a3308664aef21b1ba88b2b704627e6ccf8fc04304302e36636453091adc4f9f  docs/protocol/schemas/provider-capability.schema.json
9aa75183701303b8bb9178bd07dae1eb6e7dbdc625e4a8996b2d3e7ff562f6a2  docs/protocol/schemas/public-record.schema.json
dc7d640a758d620abe799b594f9a4b1699aaa9920d733dc6134534fb0a7498dd  docs/protocol.md
1dd730063e262e1dd38be846e84abaac8f2b1a99eb7c155150eaf0efb3451ae9  docs/requirements.md
errors 0
result PASS
note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled
exit:0

COMMAND python3 --version
Python 3.12.3
exit:0

COMMAND go version
go version go1.22.2 linux/amd64
exit:0

COMMAND node --version
v22.23.4
exit:0

COMMAND protoc --version
libprotoc 3.21.12
exit:0
```

## Language vectors

Go, Python, and TypeScript each parsed the same `docs/protocol/fixtures/vectors.json` with a separate SVN-JCS-1 implementation. All three accepted 13 valid documents, rejected 10 invalid documents for the same reason, classified 6 decimal integers, and computed 5 uint64 add/sub results. Go and TypeScript reports were byte-identical, including the RFC 8032 empty-message Ed25519 signature and the domain-separated signature over the `sort-keys` vector. Python checked the canonical SHA-256 values and the signature input SHA-256. The Ed25519 seed is the public RFC 8032 test vector, labeled `RFC8032_TEST_VECTOR_NOT_A_SECRET`. It is not a project key.

| Language | Result | Exit |
| --- | --- | --- |
| Python 3.12.3 | canonical and integer vectors match | 0 |
| Go 1.22.2 | vectors, RFC 8032, and domain signature | 0 |
| TypeScript on Node v22.23.4 | same report as Go | 0 |
| protoc 3.21.12 | descriptor set for every draft proto | 0 |

## Transition fixtures

39 contract simulations passed, including boundary ids, malformed and unknown fields, duplicate reservations, wrong domains, expired offers, forged and unauthorized signers, replay and idempotent retry, a one-validator commit and a two-validator stall without a false success, a corrupt segment, withdrawal, underflow, overflow, stale versions, and a conflicting governance update. These are not process-fault or devnet results.

## DEC dependencies

No decision was marked approved.

| Decision | Contract fields that stay blocked |
| --- | --- |
| DEC-001 | `HostingIntent.role_name`, domain `SVN-HOSTING-INTENT-1`. `ACCOUNT_HOST_DELEGATE` is rejected. |
| DEC-002 | `AdmitProvider`, `operator_group_id`. Registration stays `APPLIED` until a labeled devnet test parameter admits it. |
| DEC-003 | Off-chain publication status and the removal promise. No approved moderator force-withdraw. |
| DEC-004 | `Tombstone.retention_value_version` stays 0. No retention duration. `history_erased` is false. |
| DEC-005 | Lease term length and any allowance. Outside the devnet test number the term is `ERROR_CODE_DECISION_BLOCKED`. |
| DEC-006 | Creator upload licence on `POST /publish`. The route stays 403 while the text is unset. |
| DEC-007 | Audience and age fields. No public sign-up route and no age-gate value. |
| DEC-008 | Council seats, threshold, delay, expiry, and `QuorumLossBehavior` `UNSET`. `approved_council` is empty. |

Devnet fixtures may set `PROPOSED_TEST_PARAMETER` with `approved_policy` false only on a chain id with the PROPOSED example prefix `shortvideo-dev-`. That prefix is not an approved network name.

## Freeze

Protocol v0 interfaces for Goals 03-18 are frozen-pending-Gate-A. An incompatible change requires a recorded decision. This freeze does not approve DEC-001 through DEC-008 and does not approve the protocol.

## Blockers

DEC-001 through DEC-008 remain OPEN. No page 26 playback, processing, load, recovery, moderation-timing, or operator-independence measurement was run. No four-validator devnet was started. Those later tests are not passes.
