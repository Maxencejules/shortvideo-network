# Goal 02 evidence

Status: IMPLEMENTED

The schema and fixture checks below passed on this tree. The goal is not VERIFIED. The effort setting cannot be confirmed from inside this VM, and that point is waiting for Max's ruling at Gate A. AGENTS.md says an unreported or unverifiable effort setting for goals 01-30 is BLOCKED rather than VERIFIED. This goal is therefore IMPLEMENTED.

The checks are not a devnet, a four-validator process test, a media test, a security audit, or an approval of the protocol. The protocol is not approved. Public launch: DISABLED. No tradable token, coin, sale, or mining.

DEC-001 through DEC-008 stay OPEN. Page 26 playback, processing, load, recovery, moderation-timing, and operator-independence measurements were not run and are not passes. `replay-app-hash` and `hosting-resilience` are contract simulations of those page 26 rows. They are not the later process or retrieval measurements.

Pull request: https://github.com/Maxencejules/shortvideo-network/pull/5

## Model and effort

- Playbook assignment for goal 02: GPT-6 Astra, effort EXTRA HIGH. Astra was not used.
- Launch parameter, not an observation: the task that started this run requested model `grok-4.7` with `reasoning_effort=xhigh`.
- What this VM can see: cursor-cloud `run-info` reports `originalModelName=grok-4.7` and `bcId=bc-763364ae-2b17-5737-aa7d-39bb2a64a0c0`. That payload has no effort, reasoning, fast, or context field. The effort setting cannot be independently verified from inside the VM.
- Ruling: this point is awaiting Max at Gate A. Until that ruling, goal 02 stays IMPLEMENTED and is not marked VERIFIED.
- Goal 01's status and evidence are unchanged.
- Extra effort would not certify correctness.

## Runtime

Linux 6.12.94+ x86_64. Python 3.12.3. Go go1.22.2 linux/amd64. Node v22.23.4. libprotoc 3.21.12. Python package `cryptography` 43.0.3 signed the Ed25519 comparison. No chain, database, browser, FFmpeg worker, container devnet, or second machine was started.

## Base

Branch `goal-02` was created from `783e15911b3ea35eefd060932e5a6a317b0f9402`. Goal 01 evidence is `docs/evidence/goal-01.md`. DEC-001 through DEC-008 were OPEN in that revision and are still OPEN.

## Tested revision

This evidence file is part of the head commit. The checkers do not read this file. They do read `docs/progress.md` and `docs/progress.tsv`, so those hashes below are the bytes in this commit.

The head sha cannot be written into this file without changing the commit. The tested tree is this commit's tree. It is the `Tested-tree` trailer. Confirm with `git rev-parse 'HEAD^{tree}'`.

## Goal 01 review nits kept, and the extra spec edits reverted

Kept in `docs/evidence/goal-01/check_requirements.py`:

- `REQ-SCOPE-002` cites blueprint pages 20 and 28.
- `docs/progress.md` and `docs/progress.tsv` must agree. A started goal has one TSV row. A `NOT STARTED` goal is absent.
- Every goal number assigned in a `TEST:` disposition is in that row's goals column. A goal mentioned only to say it did not do the work is not an assignment.
- `self_test_cases` is the number of negative fixtures executed.
- Threat headings match the full heading line.

Reverted, because they were outside those nits: the requirements inventory sentences about Goal 02 fixtures and about which progress rows exist, and the four dispositions that had been rewritten away from `Goal 01` (`REQ-API-001`, `REQ-SEC-011`, `REQ-REPO-003`, `REQ-LAUNCH-001`).

## Checker-input sha256

The protocol checker prints these hashes. They include the progress files.

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
transition_cases 61
transition_case_pin 61
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 13
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
hashes
f464e653435bc43d88b99027896f5bbd5055c9f18922de6be5156c554536fbf4  docs/decision-register.md
1ecd54ba35562f55bdf791da247c1460b4ab85d09cc0b02b355fbeaee62606a6  docs/evidence/goal-01/check_requirements.py
8f7593e7d17729586ebadc8fd4493d98944a43a094e49397bd92f1d1afaadcd8  docs/evidence/goal-02/cases.py
be9efc435573b3b4edf9c753acb6978e9943186b5ce61b9e9690fca19001b775  docs/evidence/goal-02/check_protocol.py
e0b344008a1ebdec3fba9b1cb22a74d4f251c23cac23d5092c67fda5387bd6ef  docs/evidence/goal-02/go/go.mod
0e576d67efbc316dcd43980170516d1dfa6d0c694b68ddb681547007f535bcfe  docs/evidence/goal-02/go/vectorcheck.go
44c72f0c839896bd9b559f33851a512e0a8db9cbc087df2a745aefd909a765b0  docs/evidence/goal-02/jcs.py
ed959570306e4cadeb744a37299add6d64351aab476fd88a550039a795bac1dd  docs/evidence/goal-02/negative/media.proto
0a1da67c3881ae73b1924a5120cd088eefce711c2a79354bbc8cd5a7f7e6851e  docs/evidence/goal-02/negative/mint.proto
e1b1e71b30fc6a4098e3152f25a6830cfa4a52aa7ec5f3ea16ec989f6adcf2fc  docs/evidence/goal-02/negative/slash.proto
85da4ddcb323c76695d62da03477d93a562f458ec4024c8a397c2cc2e702655f  docs/evidence/goal-02/negative/storage_proof.proto
502e774d6bd2ba90752f8ca39d8fa10259b595f97e7978f91a1e01a193858309  docs/evidence/goal-02/negative/timestamp.proto
80af39dead9ac6fd66c76b0068d4733c4c9af8690a254ce48a0b2c71dc0d55c2  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
f9e758f0daf562e02e742138e9df01ad6d120558fa846d90002365d32982780f  docs/progress.md
1b1279fc9b389d7dd9fbe93f265de39b6c92e9966ef418647a563576c07e04b5  docs/progress.tsv
d52993f4fd2368c2bf25a9e5131b7dd4b4b2622f9c62cc6e090a5d925e34e25f  docs/protocol/api/client-api.json
94a2f35aaf83f234a3fb84e344cf562d6c3c391e0467ff77c7a5590f16324c9f  docs/protocol/api/provider-api.json
586a211cd073d29f30a9bcb8620de58117cb9e73ae25f36482af22aa93ec0917  docs/protocol/bounds.json
eec164c9beb92a879de4b885eeceed4069c7991896097a1864c0b437026996bc  docs/protocol/fixtures/genesis-valid.json
de0735b82b34a84e7d95d8e8bc720f346d1a705582ab6fc0a222324f0794539d  docs/protocol/fixtures/manifest-invalid-boundary.json
f4b0a345b494f00b4a9bab599be25d80d9ff31bf28b017979f28c91beafb512b  docs/protocol/fixtures/manifest-invalid-duplicate-id.json
be1b53f8674503c5086af4a23468cd4fb205cf296faf7845c4d383c3a3a9f2a9  docs/protocol/fixtures/manifest-invalid-unknown.json
c6e8246ddb86788efa30f6e36eb887bc2e286fd7e2226baa5522a13790bef238  docs/protocol/fixtures/manifest-invalid-wrong-domain.json
703a4abce900576fbd08f21fd3d1a611fb49eb01fc7882d694ddd8106f4477f1  docs/protocol/fixtures/manifest-valid.json
9f5ffbcd0a501bc03574e3322363873d9d436dcbd956c4eb0f230f8daf529f4a  docs/protocol/fixtures/vectors.json
39f250f2338b12284fa76d427ee7294f00f4412d77e6fb9d83ac517f11ac9e23  docs/protocol/proto/svn/app/v1/genesis.proto
1f1bdc75b15f0634c704ecb930d7df09df8aaeadde8a2082fc7436c827d57274  docs/protocol/proto/svn/authority/v1/genesis.proto
826d47717c514115a706000d35f3cc6a53ea1ebec48e8e3c8f1cc241218c5299  docs/protocol/proto/svn/authority/v1/query.proto
57ee9ab0ecfc4859e35c7a2d701c0b5875542acf1ef9d66e6ac14b082d244b9f  docs/protocol/proto/svn/authority/v1/state.proto
f2b1941b783bc4d0d81d958aa02cf210e5665e9503bd003a8916b0f507b2b779  docs/protocol/proto/svn/authority/v1/tx.proto
2cd0a0748f54b16763d453f9d42dfda8e57d9f8124d464a0f24d53bd8db760da  docs/protocol/proto/svn/content/v1/genesis.proto
ece88f408b5cdb10e17ecfa7224661a9f2d3e2296b29c1b25c029c530ce14769  docs/protocol/proto/svn/content/v1/query.proto
e2cbb92ef171857e87806a0ec5a262578f8fa280a87303d193ae34b09acafb50  docs/protocol/proto/svn/content/v1/state.proto
5f2a0f3d6281df3dc02340c86bbabd591188004ee28f153778831dec718dbf27  docs/protocol/proto/svn/content/v1/tx.proto
482e78f5690b3478c67c5abddecec580e22327456f3ed5e6ab5f4a3b53cb03c4  docs/protocol/proto/svn/events/v1/events.proto
9e9316b3542141a78e7f5cde214422ed84a1214726786baa5f69ece832c17cd6  docs/protocol/proto/svn/evidence/v1/genesis.proto
7d11286f4c3f95a407644129fc97f6f0e856db8ecb16ec50610ad1612d411175  docs/protocol/proto/svn/evidence/v1/query.proto
9a411c384f7bee53b87a55c8ce2a95ae8efc334e3af02f37bc31436c1c77e4d5  docs/protocol/proto/svn/evidence/v1/state.proto
0936eea985e3c8b9fb9dbfcbd2c77bd180a0b3a8dcf9d253331e4643bcc2b293  docs/protocol/proto/svn/evidence/v1/tx.proto
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
95d73d0b5a6913f3601bfd39195b8c06d248a0241da481f96aab2d8db772b3bf  docs/protocol.md
a1f08fb7dd66d3937be22bc7ca49ef2e7c2cd7e92d04483b6680aba0c41f97c2  docs/requirements.md
errors 0
languages MATCH
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

## Repeat run

The same two checkers were run again after this evidence file existed. The protocol checker does not read this file. The repeat stdout is below. Exit codes are 0.

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
transition_cases 61
transition_case_pin 61
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 13
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
hashes
f464e653435bc43d88b99027896f5bbd5055c9f18922de6be5156c554536fbf4  docs/decision-register.md
1ecd54ba35562f55bdf791da247c1460b4ab85d09cc0b02b355fbeaee62606a6  docs/evidence/goal-01/check_requirements.py
8f7593e7d17729586ebadc8fd4493d98944a43a094e49397bd92f1d1afaadcd8  docs/evidence/goal-02/cases.py
be9efc435573b3b4edf9c753acb6978e9943186b5ce61b9e9690fca19001b775  docs/evidence/goal-02/check_protocol.py
e0b344008a1ebdec3fba9b1cb22a74d4f251c23cac23d5092c67fda5387bd6ef  docs/evidence/goal-02/go/go.mod
0e576d67efbc316dcd43980170516d1dfa6d0c694b68ddb681547007f535bcfe  docs/evidence/goal-02/go/vectorcheck.go
44c72f0c839896bd9b559f33851a512e0a8db9cbc087df2a745aefd909a765b0  docs/evidence/goal-02/jcs.py
ed959570306e4cadeb744a37299add6d64351aab476fd88a550039a795bac1dd  docs/evidence/goal-02/negative/media.proto
0a1da67c3881ae73b1924a5120cd088eefce711c2a79354bbc8cd5a7f7e6851e  docs/evidence/goal-02/negative/mint.proto
e1b1e71b30fc6a4098e3152f25a6830cfa4a52aa7ec5f3ea16ec989f6adcf2fc  docs/evidence/goal-02/negative/slash.proto
85da4ddcb323c76695d62da03477d93a562f458ec4024c8a397c2cc2e702655f  docs/evidence/goal-02/negative/storage_proof.proto
502e774d6bd2ba90752f8ca39d8fa10259b595f97e7978f91a1e01a193858309  docs/evidence/goal-02/negative/timestamp.proto
80af39dead9ac6fd66c76b0068d4733c4c9af8690a254ce48a0b2c71dc0d55c2  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
f9e758f0daf562e02e742138e9df01ad6d120558fa846d90002365d32982780f  docs/progress.md
1b1279fc9b389d7dd9fbe93f265de39b6c92e9966ef418647a563576c07e04b5  docs/progress.tsv
d52993f4fd2368c2bf25a9e5131b7dd4b4b2622f9c62cc6e090a5d925e34e25f  docs/protocol/api/client-api.json
94a2f35aaf83f234a3fb84e344cf562d6c3c391e0467ff77c7a5590f16324c9f  docs/protocol/api/provider-api.json
586a211cd073d29f30a9bcb8620de58117cb9e73ae25f36482af22aa93ec0917  docs/protocol/bounds.json
eec164c9beb92a879de4b885eeceed4069c7991896097a1864c0b437026996bc  docs/protocol/fixtures/genesis-valid.json
de0735b82b34a84e7d95d8e8bc720f346d1a705582ab6fc0a222324f0794539d  docs/protocol/fixtures/manifest-invalid-boundary.json
f4b0a345b494f00b4a9bab599be25d80d9ff31bf28b017979f28c91beafb512b  docs/protocol/fixtures/manifest-invalid-duplicate-id.json
be1b53f8674503c5086af4a23468cd4fb205cf296faf7845c4d383c3a3a9f2a9  docs/protocol/fixtures/manifest-invalid-unknown.json
c6e8246ddb86788efa30f6e36eb887bc2e286fd7e2226baa5522a13790bef238  docs/protocol/fixtures/manifest-invalid-wrong-domain.json
703a4abce900576fbd08f21fd3d1a611fb49eb01fc7882d694ddd8106f4477f1  docs/protocol/fixtures/manifest-valid.json
9f5ffbcd0a501bc03574e3322363873d9d436dcbd956c4eb0f230f8daf529f4a  docs/protocol/fixtures/vectors.json
39f250f2338b12284fa76d427ee7294f00f4412d77e6fb9d83ac517f11ac9e23  docs/protocol/proto/svn/app/v1/genesis.proto
1f1bdc75b15f0634c704ecb930d7df09df8aaeadde8a2082fc7436c827d57274  docs/protocol/proto/svn/authority/v1/genesis.proto
826d47717c514115a706000d35f3cc6a53ea1ebec48e8e3c8f1cc241218c5299  docs/protocol/proto/svn/authority/v1/query.proto
57ee9ab0ecfc4859e35c7a2d701c0b5875542acf1ef9d66e6ac14b082d244b9f  docs/protocol/proto/svn/authority/v1/state.proto
f2b1941b783bc4d0d81d958aa02cf210e5665e9503bd003a8916b0f507b2b779  docs/protocol/proto/svn/authority/v1/tx.proto
2cd0a0748f54b16763d453f9d42dfda8e57d9f8124d464a0f24d53bd8db760da  docs/protocol/proto/svn/content/v1/genesis.proto
ece88f408b5cdb10e17ecfa7224661a9f2d3e2296b29c1b25c029c530ce14769  docs/protocol/proto/svn/content/v1/query.proto
e2cbb92ef171857e87806a0ec5a262578f8fa280a87303d193ae34b09acafb50  docs/protocol/proto/svn/content/v1/state.proto
5f2a0f3d6281df3dc02340c86bbabd591188004ee28f153778831dec718dbf27  docs/protocol/proto/svn/content/v1/tx.proto
482e78f5690b3478c67c5abddecec580e22327456f3ed5e6ab5f4a3b53cb03c4  docs/protocol/proto/svn/events/v1/events.proto
9e9316b3542141a78e7f5cde214422ed84a1214726786baa5f69ece832c17cd6  docs/protocol/proto/svn/evidence/v1/genesis.proto
7d11286f4c3f95a407644129fc97f6f0e856db8ecb16ec50610ad1612d411175  docs/protocol/proto/svn/evidence/v1/query.proto
9a411c384f7bee53b87a55c8ce2a95ae8efc334e3af02f37bc31436c1c77e4d5  docs/protocol/proto/svn/evidence/v1/state.proto
0936eea985e3c8b9fb9dbfcbd2c77bd180a0b3a8dcf9d253331e4643bcc2b293  docs/protocol/proto/svn/evidence/v1/tx.proto
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
95d73d0b5a6913f3601bfd39195b8c06d248a0241da481f96aab2d8db772b3bf  docs/protocol.md
a1f08fb7dd66d3937be22bc7ca49ef2e7c2cd7e92d04483b6680aba0c41f97c2  docs/requirements.md
errors 0
languages MATCH
result PASS
note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled
exit:0
```

## Language vectors

Go, Python, and TypeScript each parsed `docs/protocol/fixtures/vectors.json` with a separate SVN-JCS-1 implementation. The checker asserts 13 valid ids, 14 invalid ids, 6 integers, and 5 uint64 add/sub results. The invalid set includes a lone surrogate, a UTF-8 BOM, invalid UTF-8, and `9223372036854775808`, which is the Go `unsafe_integer` path past `ParseInt`. Python signed the RFC 8032 empty message and the domain-separated payload with `cryptography` and the three reports were equal, including signatures. The seed is the public RFC 8032 test vector, labeled `RFC8032_TEST_VECTOR_NOT_A_SECRET`. It is not a project key.

| Language | Result | Exit |
| --- | --- | --- |
| Python 3.12.3 | canonical values, integers, and Ed25519 signatures | 0 |
| Go 1.22.2 | same report, including `above-int64` | 0 |
| TypeScript on Node v22.23.4 | same report | 0 |
| protoc 3.21.12 | protocol descriptors compiled; negative descriptors were rejected by the scanner | 0 |

## Transition fixtures

61 pinned contract simulations passed, including the transitions named in the round-1 review: `UpdateProvider`, a successful `RenewLease`, changed-bytes conflict, DEC-002 admission block, non-authority admit, a second withdraw, same-seat approval replay, stale and DEC-named proposals, execute-before-delay, authority end, key rotation then an old key, disablement, expired and rejected proposals, rejected evidence, force-withdraw, validator-power application, `replay-app-hash`, and `hosting-resilience`. These are not process-fault or devnet results.

## DEC dependencies

No decision was marked approved.

| Decision | Contract fields that stay blocked |
| --- | --- |
| DEC-001 | `HostingIntent.role_name`, domain `SVN-HOSTING-INTENT-1`. `ACCOUNT_HOST_DELEGATE` is rejected. |
| DEC-002 | `MsgAdmitProvider`, `AdmitProviderChange`, `operator_group_id`. |
| DEC-003 | `MsgForceWithdraw`, `ForceWithdrawChange`, and `removal_promise_approved`, which stays false. |
| DEC-004 | `Tombstone.retention_value_version` stays 0. No retention duration. `history_erased` is false. |
| DEC-005 | Lease term length and any allowance. A governed limit cannot raise a safety cap. |
| DEC-006 | Creator upload licence on `POST /publish`. |
| DEC-007 | Audience and age fields. No public sign-up route. |
| DEC-008 | Council seats, threshold, delay, expiry, and `QuorumLossBehavior` `UNSET`. `approved_council` is empty. `MsgDisableProvider` uses the devnet test parameter only. |

## Freeze

Protocol v0 interfaces for Goals 03-18 are frozen-pending-Gate-A. An incompatible change requires a recorded decision. This freeze does not approve DEC-001 through DEC-008 and does not approve the protocol.

## Blockers

The effort setting is not independently verifiable here. That blocks VERIFIED until Max rules at Gate A. DEC-001 through DEC-008 remain OPEN. No page 26 playback, processing, load, recovery, moderation-timing, or operator-independence measurement was run. No four-validator devnet was started.
