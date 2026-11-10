# Goal 19 evidence

Status: BLOCKED

Invited accounts, passkey login, sessions, export, import, deletion, and an encrypted backup are implemented. Delegated host-signing custody and account recovery are not. Public launch stays disabled. There is no token, coin, sale, or mining. No video bytes and no personal data were written to the chain. This is not an audit and not a production network.

The account service is `services/api`. Passkeys are verified by `github.com/go-webauthn/webauthn` v0.18.2. Tests drive a software authenticator from `github.com/descope/virtualwebauthn` v1.0.5. That package is not imported by `cmd/svn-api`. Session tokens are random identifiers stored as SHA-256 hashes. They are not a signature scheme.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/19-accounts-recovery-and-data-portability.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-9adb61ab-2c7c-5e13-b577-1995badf9f52`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run.
- On 2026-10-31 the owner waived the model requirement as a blocker. On 2026-11-01 the owner extended that acknowledgement through goal 30. That covers model allocation only.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `fec7ab32dc24be11065327ccd8b0d996e53798cd` |
| Tree | `4c10752fdc54f028b996d50d1038fb9004512e0e` |
| Base | `b5b05b37c6885ca1c63176cf7c240bea59bd955c` |
| Branch | `goal-19` |

[trailer.txt](logs/goal-19/trailer.txt) line 17 is `commit fec7ab32dc24be11065327ccd8b0d996e53798cd`. Line 18 is `Author: Maxence Jules <Maxencejules@users.noreply.github.com>`. Line 19 is `Commit: Maxence Jules <Maxencejules@users.noreply.github.com>`. Line 28 is `Tested-tree: 4c10752fdc54f028b996d50d1038fb9004512e0e`. The same file's `TREE` line is that tree. This evidence commit changes only `docs/evidence/` relative to that tested commit.

[scenario.txt](logs/goal-19/scenario.txt) line 3 is `HEAD fec7ab32dc24be11065327ccd8b0d996e53798cd` and line 4 is `TREE 4c10752fdc54f028b996d50d1038fb9004512e0e`. Line 5 is `DIRTY no`. Later logs print `DIRTY yes` because this log already existed. No source file was modified between those captures.

## What ran

| Command | Log | Result |
| --- | --- | --- |
| `go test -count=1 -timeout 15m -run TestMilestone -v ./services/milestone` | [scenario.txt](logs/goal-19/scenario.txt) | line 101 `ok`, line 102 `EXIT:0` |
| `go test -count=1 -timeout 8m -run 'TestAccount\|TestRateLimitRoleAndProductionFixtures' ./services/api/internal/service/` | [accounts.txt](logs/goal-19/accounts.txt) | line 7 `ok`, line 8 `EXIT:0` |
| `go test -count=1 -timeout 2m -v ./services/api/internal/records/` | [records.txt](logs/goal-19/records.txt) | line 9 `--- PASS: TestKeyHistoryAndReplay`, line 12 `EXIT:0` |
| Python fixture check of `services/api/internal/records/testdata/history.json` | [python-records.txt](logs/goal-19/python-records.txt) | line 10 `ok`, line 11 `EXIT:0` |
| `go test -count=1 -timeout 3m -run TestControlRoutesRequireOperator -v ./services/scheduler/internal/sched/` | [scheduler-auth.txt](logs/goal-19/scheduler-auth.txt) | line 12 `ok`, line 13 `EXIT:0` |
| `go vet ./services/api/...` | [vet.txt](logs/goal-19/vet.txt) | line 11 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | [secrets.txt](logs/goal-19/secrets.txt) | line 16 `secret scan ok`, line 17 `EXIT:0` |

[runtime.txt](logs/goal-19/runtime.txt) line 12 is `go version go1.26.10 linux/amd64`. Line 13 is `postgres (PostgreSQL) 18.6 (Ubuntu 18.6-1.pgdg24.04+2)`. Line 14 is `Python 3.12.3`. Line 15 is `pg16_test:1`.

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Invited enrollment, stable account ids, no public sign-up | VERIFIED | `TestAccountSessionsAndAuthorization`. [accounts.txt](logs/goal-19/accounts.txt) line 7. |
| Duplicate enrollment | VERIFIED | same test. A spent invite returns 409. |
| Stolen and revoked sessions, rotation, expiry | VERIFIED | same test. |
| CSRF and protected cookies | VERIFIED | same test. The session cookie is `HttpOnly`, `Secure`, and `SameSite=Strict`. A cookie write without `X-CSRF-Token` is 403. Bearer calls are not cookie submissions. |
| Privilege escalation | VERIFIED | a viewer cannot upload or create invites. A creator cannot create invites. A preference body cannot change the role. |
| Cross-account access | VERIFIED | another creator's upload read is 404. An export does not contain another account's preference or a moderation note. |
| Rate limits on enrollment and login | VERIFIED | `TestAccountRateLimit`. |
| Passkeys | VERIFIED | enrollment, login, and step-up confirmation use WebAuthn ceremonies checked by go-webauthn. |
| Strong confirmation | VERIFIED for session revocation, export, backup, deletion, and import. Recovery-key changes are BLOCKED with recovery. |
| Key rotation, old and new signed records, replay | VERIFIED | `TestKeyHistoryAndReplay` and the Python fixture check. Retired keys still verify old records. A new sequence needs the active key. |
| Migrate a test account between two hosts | VERIFIED | `TestAccountMigration`. Host B verifies host A's application export signature and the same passkey logs in. |
| Delete a test account | VERIFIED | `TestAccountDeletionAndBackup`. Future login fails. Preferences are removed. Moderation notes and public-record refs remain. `history_erased` is false. The retention decision id is `DEC-004` and the value version is 0. |
| Encrypted backup | VERIFIED | `SVN-BACKUP-1`, AES-256-GCM. The key is returned once and is not stored. Losing it is not recoverable. Recovery of that key is BLOCKED on DEC-001. |
| Goal 18 scenario under the new identities | VERIFIED on this host, one machine | [scenario.txt](logs/goal-19/scenario.txt) line 14 `fixture=false`. Line 16 `anonymous=401 creator=401`. Line 47 is the reserved upload. Line 75 is `CLIENT ok`. Line 98 is `phase=done`. |
| Recovery links and challenges | BLOCKED | no route and no stub. `POST /v1/recovery` is 404. |
| Delegated host-signing custody | BLOCKED | DEC-001. Public-record code stores and checks public key references. It does not generate or hold a host signing key. |

The milestone is still one machine. [scenario.txt](logs/goal-19/scenario.txt) line 7 is `physical_count=1 independent=false`. That does not become an independent-operator result.

## Endpoint audit

Application routes on `svn-api` require an account session, except enroll and login, which require an invite or a passkey and are rate limited. There is no public sign-up route.

| Surface | Identity | Why |
| --- | --- | --- |
| `svn-api` upload routes | account session, role `creator`, and the row owner | production no longer accepts `dev-fixture-` tokens or an injected verifier |
| `svn-api` `POST /v1/invites` and `POST /v1/account/import` | operator session, and import also needs a fresh passkey confirmation | a creator or viewer is 403 |
| `svn-scheduler` `POST /v1/publications`, `/v1/repairs`, `/v1/origin`, `/v1/hosts` | active operator session, checked with `GET /v1/session` | DR-18-07. A creator session is 401. [scenario.txt](logs/goal-19/scenario.txt) line 16. |
| `svn-scheduler` `GET /health` | none | process liveness, no account data |
| `svn-scheduler` `GET /v1/availability` | none | public coordination read, no account data |
| `svn-scheduler` `GET /v1/distribution/...` | none | package bytes this loopback process already accepted. It is not an application session. Providers fetch it on the loopback listener. |
| `svn-indexer` `GET /v1/*` | none | committed chain read model. Public coordination, not account data. |
| `svn-broadcaster` | operator mTLS caller | chain submission. Not an application session and not a viewer passkey. |
| `svn-provider` `/admin/*` | provider machine credential | not an application session and not a chain key |
| `svn-delivery` playback | local grant on loopback | the grant is not an application session and not publication |
| `svn-chainview` | milestone chain gateway | `/account` is a chain account query, not an application login |
| `agents/auditor` | no HTTP listener in this tree | unchanged |

Development fixtures still run only when `environment` is `development` and `enable_dev_fixtures` is true. Production refuses fixtures, a probe delay, and an injected verifier. The milestone config is `environment=production`.

Application credentials stay separate from operator chain signing. The export key is ed25519 and signs `SVN-ACCOUNT-EXPORT-1`. It is not a Cosmos key. Viewers and creators do not receive chain-submission keys. The broadcaster still uses its own mTLS callers.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-19-01 | OPEN | The bootstrap invite is inserted only when `SVN_API_BOOTSTRAP_TOKEN` is set and the account table is empty. It is one operator invite, not a session, and it is ignored after the first account exists. It is not an approved operator list (DEC-002). |
| DR-19-02 | OPEN, BLOCKED | Delegated host-signing custody is not implemented. DEC-001. No host-held user signing key. |
| DR-19-03 | OPEN, BLOCKED | Account recovery is not implemented. DEC-001. No recovery link, challenge, or recovery-key change. |
| DR-19-04 | OPEN, BLOCKED | DEC-004 does not set a retention period. Deletion records `decision_id=DEC-004`, `value_version=0`, and `history_erased=false`. It does not delete moderation notes, public-record refs, or chain history. |
| DR-19-05 | OPEN | The application export key signs export bundles so another host can verify them. The owner has not approved a key ceremony. The key is not a hosting-intent key. |
| DR-19-06 | OPEN | The relying party id in tests and in the milestone is `localhost`, origin `http://localhost`. A public hostname is not chosen. |
| DR-19-07 | OPEN | DEC-007 audience, age, and privacy review stay unset. Public sign-up and public launch stay off. |
| DR-19-08 | OPEN | Development fixtures remain behind `environment=development`. That mode is not the milestone path and is refused in production. |
| DR-18-07 | addressed for the four POST routes | Those routes now require an operator session. See the audit table for the routes that stay unauthenticated, and why. |
| DR-13-01 | OPEN | The production listener now uses account sessions. The owner has not approved the session boundary as the account contract. Development fixtures remain for the development environment. |
| DR-07-01 | OPEN | Carried. Production hosting-intent custody is unset. |
| DR-18-01 .. DR-18-06 | OPEN | Carried. The milestone did not change those chain and publication readings. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody and recovery stay unimplemented. DR-19-02 and DR-19-03.
2. **DEC-004.** The retention period is unset. DR-19-04.
3. **DEC-007.** Audience and age stay unset. DR-19-07.
4. **One machine.** [scenario.txt](logs/goal-19/scenario.txt) line 7 `physical_count=1 independent=false`.
5. **Goal 06 and goal 07 mutation runners.** Anchor checks only. [anchors06.txt](logs/goal-19/anchors06.txt) line 34 is `RESULT search-string problems=13; no test was run` and line 35 is `EXIT:1`. [anchors07.txt](logs/goal-19/anchors07.txt) line 30 is `RESULT validation problems=3; no test was run` and line 31 is `EXIT:1`.
6. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-19/golangci-lint.txt) line 15 is `scripts/evidence/capture.sh: line 58: golangci-lint: command not found` and line 16 is `EXIT:127`.
7. **PostgreSQL 16.** Not installed. [pg16.txt](logs/goal-19/pg16.txt) line 13 is `EXIT:1`. The server in use is 18.6. [runtime.txt](logs/goal-19/runtime.txt) line 13.
8. **Goal 04 compose bridge.** [docker.txt](logs/goal-19/docker.txt) line 14 is `scripts/evidence/capture.sh: line 58: docker: command not found` and line 15 is `EXIT:127`. Compose was not run. The unauthenticated :2375 daemon was not used.

## Requirements

The `requirements-matrix` block already names goal 19. It was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-SCOPE-001 | VERIFIED for this host. Uninvited enrollment is rejected. `POST /signup` is 404. |
| REQ-BOUND-001 | VERIFIED. Login creates an application session. It does not produce a Cosmos signature. |
| REQ-ID-001 | VERIFIED. A passkey is a login factor. Viewers do not receive chain-submission keys. |
| REQ-ID-002 | The envelope fields are checked in `services/api/internal/records`. Canonicalization stays SVN-JCS-1 from goal 02. This goal does not put the envelope on chain. |
| REQ-ID-004 | BLOCKED. DEC-001. DR-19-02. |
| REQ-ID-007 | BLOCKED for recovery-key changes and host-signing migration. Session revocation and the tested host migration require a fresh passkey confirmation. DR-19-03. |
| REQ-UX-006 | Export and deletion ran. Recovery controls are BLOCKED. DR-19-03 and DR-19-04. |
| REQ-OFF-002 | Email, recovery secrets, watch events, and complaint text are not accepted on the public-record envelope and are not chain fields of this goal. |

Saved videos in this goal are the account's list of content ids. The bytes stay in the media store. Preferences, the backup key, and moderation notes stay off chain. This is not full federation and not follower migration.

## Next safe action

The owner decides DEC-001 before any host-signing custody or recovery flow. DEC-004 still has to set a retention period before deletion can claim a duration. DEC-007 still blocks an audience claim. Do not bind the scheduler or the API off loopback until that review. Do not treat this single machine as independent operators.
