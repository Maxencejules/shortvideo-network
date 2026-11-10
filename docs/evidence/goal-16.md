# Goal 16 evidence

Status: BLOCKED

`agents/provider/cmd/svn-provider` is a loopback development storage agent. It enrolls a per-workspace key, checks disk and quota before staging, signs the exact acceptance with the chain helpers, and serves bytes only after the configured chain endpoint reports the lease. The goal stays BLOCKED. DR-16-01 through DR-16-06 are open. DEC-001 and DR-07-01 stay open. Two processes on this host are not independent operators. Public launch: DISABLED. No tradable token. This is not an audit and not a production network. No video bytes and no personal data were submitted to a chain.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/16-storage-provider-agent.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-c0957f5d-f7e2-5e4b-9768-e52c3704abef`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `0ae57fcf1896cf863da1e867c1f9081007313c57` |
| Tree | `4cd758ce0806103e86d63dc0162e2ff3aa2edde7` |
| Base | `cd89c1908a045b28af7d8e429f38767eb923bb5d` |
| Branch | `goal-16` |

[trailer.txt](logs/goal-16/trailer.txt) prints `commit 0ae57fcf1896cf863da1e867c1f9081007313c57`, `Author: Maxence Jules <Maxencejules@users.noreply.github.com>`, and `Tested-tree: 4cd758ce0806103e86d63dc0162e2ff3aa2edde7`. The same file's `TREE` line is `4cd758ce0806103e86d63dc0162e2ff3aa2edde7`. This evidence commit changes only `docs/evidence/` relative to that tested commit.

[names.txt](logs/goal-16/names.txt) is `git diff --name-only cd89c1908a045b28af7d8e429f38767eb923bb5d..HEAD`. The names are under `agents/provider/` and `docs/`. `chain/` and `proto/` are not in that list. The agent calls the existing sign-bytes helpers. It does not change them.

## Inherited blockers

BLOCKED, carried from goals 04 and 07 through 15:

1. **DEC-001 and DR-07-01, production custody.** The agent uses a key it generates under its data directory. That key is not a production provider key. The hosting-intent role remains unset.
2. **One machine.** Every process in these logs is on this host. A second agent is another process. It is not an independent operator and it is not physical redundancy.
3. **Goal 06 and goal 07 mutation runners.** Anchor checks only. [anchors06.txt](logs/goal-16/anchors06.txt) exit 1, `RESULT search-string problems=13; no test was run`. [anchors07.txt](logs/goal-16/anchors07.txt) exit 1, `RESULT validation problems=3; no test was run`.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-16/golangci-lint.txt) prints `golangci-lint: command not found` and `EXIT:127`.
5. **PostgreSQL 16.** [pg16.txt](logs/goal-16/pg16.txt) prints `pg16_test:1` and `EXIT:1`. [runtime.txt](logs/goal-16/runtime.txt) also prints `pg16_test:1`.
6. **Goal 04 compose bridge.** `docker` is not installed. [docker.txt](logs/goal-16/docker.txt) prints `docker: command not found` and `EXIT:127`. Compose was not run. The unauthenticated :2375 daemon was not used.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-16-01 | OPEN | Transfer-receipt canonical fields. Not a storage proof. |
| DR-16-02 | OPEN | Inventory canonical fields. `independentOperator` and `physicalRedundancy` are false. |
| DR-16-03 | OPEN | The development agent listens on loopback only. |
| DR-16-04 | OPEN | Chain observation is the operator HTTPS endpoint. A broadcast response is not a commit. The endpoint in this run was a test fixture, not a four-validator devnet. |
| DR-16-05 | OPEN | Quota, free-space margin, bandwidth, clock uncertainty, and stall interval are operator config. Not a DEC-005 allowance. |
| DR-16-06 | OPEN | Two identities on one machine are not independent ownership or physical redundancy. |
| DR-07-01 | OPEN | Carried. Production custody is unset. |
| DR-08-04 | OPEN | Carried. The acceptance payload signed here is the interim `leases.AcceptSignBytes` payload. |
| DR-13-01 | OPEN | Carried. |
| DR-13-02 | OPEN | Carried. |
| DR-13-03 | OPEN | Carried. Allowances stay unset. |
| DR-14-01 | OPEN | Carried. |
| DR-14-02 | OPEN | Carried. The bytes transferred here are synthetic segment files, not an approved video package. |
| DR-14-03 | OPEN | Carried. |
| DR-14-04 | OPEN | Carried. |
| DR-15-01 | OPEN | Carried. |
| DR-15-02 | OPEN | Carried. |
| DR-15-03 | OPEN | Carried. |
| DR-15-04 | OPEN | Carried. |
| DR-15-05 | OPEN | Carried. |
| DR-15-06 | OPEN | Carried. |

## Acceptance

The acceptance log is [conformance.txt](logs/goal-16/conformance.txt). `HEAD` is `0ae57fcf1896cf863da1e867c1f9081007313c57` and `TREE` is `4cd758ce0806103e86d63dc0162e2ff3aa2edde7`. `DIRTY yes` is the untracked log directory. [diffstat.txt](logs/goal-16/diffstat.txt) has an empty diff and `EXIT:0`.

| Check | Status | Where |
| --- | --- | --- |
| Hosting consent refused | VERIFIED on this host | [conformance.txt](logs/goal-16/conformance.txt) line 8: `CONSENT_REFUSED exit=1`. The assertion is `cmd/svn-provider/conformance_test.go` line 57. |
| Link-local destination cannot be configured | VERIFIED on this host | Line 9: `LINK_LOCAL_CONFIG refused=true`. Assertion at `conformance_test.go` line 72. |
| Two agents installed | VERIFIED as two processes | Line 9: `INSTALL alpha=https://127.0.0.1:36119 beta=https://127.0.0.1:44959`. Those ports are the ephemeral ports that log printed. |
| Not independent | VERIFIED as a negative claim | Line 10: `INDEPENDENCE alpha=false beta=false machine=vm-1` and `disk_available_bytes=261396000768`. |
| Real package transfer and HTTPS serve | VERIFIED on loopback | Line 11: `TRANSFER alpha status=200 bytes=64 sha256=5d57ec6c48c08c20543c1ff21b946d6eea681f3c1544ccab9253152898a16472 readiness=SERVING`. The hash compare is `conformance_test.go` lines 116-117. |
| Acceptance bound to that key and the exact terms | VERIFIED | Line 12: `ACCEPT alpha verifies=true altered_signature=false service_key=false terms_bound=true`. Assertions at `conformance_test.go` lines 128, 133, 139, 144, and 152. |
| Transaction has no segment bytes | VERIFIED | Line 13: `TX_SEGMENT_BYTES=false signer_is_alpha=true broadcasts=1`. Assertion at `conformance_test.go` lines 164-167. |
| Repeated identical offer | VERIFIED | Line 14: `REPEAT same_signature=true used_bytes=64`. |
| Conflict, unapproved offer, altered bytes | VERIFIED | Line 15: `REJECT conflict=409 unapproved=403 altered_bytes=422 used_bytes=64`. |
| Malicious URL, redirect, unbounded body | VERIFIED inside the test process | Line 16: `MALICIOUS_URL http=destination_refused metadata=destination_refused dotdot=destination_refused redirect=redirect_refused unbounded=unbounded_response`. |
| Encoded dot-dot path | VERIFIED | Line 17: `PATH_REFUSED contains_secret=false`. Assertion at `conformance_test.go` line 236. |
| Restart after a committed acceptance | VERIFIED | Line 18: `RESTART_COMMITTED status=200 used_bytes=64 readiness=SERVING`. |
| Real-time deadline | VERIFIED | Line 19: `DEADLINE status=410 readiness=EXPIRED`. |
| Clock uncertainty keeps the lease off the wire | VERIFIED | Line 20: `DEADLINE_UNCERTAIN readiness=COMMITTED advertised=false certainty=uncertain`. |
| Key rotation | VERIFIED for the local hook | Line 21: `ROTATE old_key_rejected=true new_key=ak.r2`. The old public key failing is `conformance_test.go` line 307. The rotation was not committed on a devnet. |
| Unauthorized admin | VERIFIED | Line 22: `ADMIN_UNAUTHORIZED status=401 draining=false`. |
| Chain stall | VERIFIED against the fixture endpoint | Line 23: `STALL chain_uncertain=true new_readiness=ACCEPTANCE_PENDING broadcasts_added=0 serving_kept=true`. This is not a four-validator halt. |
| Drain | VERIFIED | Line 24: `DRAIN new_offer=draining serving_status=200`. |
| Local withdrawal | VERIFIED | Line 25: `WITHDRAW chain_end=false local_readiness=REMOVED inventory_attestation=true`. |
| Capability and receipt | VERIFIED as local signatures | Line 26: `CAPABILITY digest=ee3bc7b57b6d25ae69450720783d797619e69411d2f1ceb7a1ec9d324f2fda2c key=ak.r2`. Line 27: `RECEIPT status=201 key=ak.r2`. |
| Second agent's key | VERIFIED | Line 28: `TWO_KEYS alpha_rejects_beta=true beta_verifies=true`. |
| Package source requires a bearer | VERIFIED | Line 29: `SOURCE_UNAUTHENTICATED status=401`. |
| Restart after a partial transfer | VERIFIED | Line 30: `PARTIAL_RESTART used_bytes=0 leases=0 staging_removed=true`. |
| Insufficient disk | VERIFIED with `statfs` | Line 31: `DISK_REJECT available=261395943424 min_free=261395943424 error=insufficient_disk`. |
| Insufficient quota | VERIFIED | Line 32: `QUOTA_REJECT quota_bytes=10 error=insufficient_quota`. |
| Bandwidth limit | VERIFIED | Line 33: `BANDWIDTH_REJECT bytes_per_second=1 budget_seconds=1 error=bandwidth_limit`. |
| Four-validator commit of this agent's transaction | BLOCKED | Not run. DR-16-04. The fixture's broadcast response was not treated as a commit (`conformance_test.go` lines 105-108 require `ACCEPTANCE_PENDING` and package status 404 before the fixture reports the lease). |
| Independent operator or a second machine | BLOCKED | Needs another person and another machine. Listed below. |

[conformance.txt](logs/goal-16/conformance.txt) ends with `--- PASS: TestConformance (7.36s)` and `ok github.com/Maxencejules/shortvideo-network/agents/provider/cmd/svn-provider 7.400s`.

## Other commands

| Step | Exit | Log |
| --- | --- | --- |
| runtime | 0 | [runtime.txt](logs/goal-16/runtime.txt). Linux `6.12.113+`, `go version go1.26.10 linux/amd64`, `nproc` printed `4`, `pg16_test:1`. |
| scaffold | 0 | [scaffold.txt](logs/goal-16/scaffold.txt). `ok github.com/Maxencejules/shortvideo-network/agents/provider 0.001s`. |
| vet | 0 | [vet.txt](logs/goal-16/vet.txt). |
| gofmt | 0 | [gofmt.txt](logs/goal-16/gofmt.txt). No file names were printed. |
| race | 0 | [race.txt](logs/goal-16/race.txt). `ok` for the root package in `1.010s` and for `cmd/svn-provider` in `16.971s`. |
| anchors06 | 1 | BLOCKED. Not a pass. |
| anchors07 | 1 | BLOCKED. Not a pass. |
| golangci-lint | 127 | BLOCKED. Not a pass. |
| docker | 127 | BLOCKED. Not a pass. |
| pg16 | 1 | BLOCKED. Not a pass. |

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-PROD-006 | VERIFIED for a designated loopback agent that does not advertise a package before acceptance. Not an independent host. | `TestConformance` package fetch before and after the fixture reports the lease, `conformance_test.go` lines 105-113. |
| REQ-UX-013 | VERIFIED for the local quota refusal and the drain. The leave-network handoff to another operator is goal 24 and was not run. | `QUOTA_REJECT` and `DRAIN` lines in [conformance.txt](logs/goal-16/conformance.txt). |
| REQ-UX-019 | VERIFIED for a bounded lease and the rotation hook. Cost counters are goal 25. A production key rotation is goal 26. | `ROTATE` line. |
| REQ-ARCH-004 | VERIFIED | A service key does not verify the acceptance (`conformance_test.go` line 152). Unauthorized admin is line 22 of the log. |
| REQ-ARCH-010 | VERIFIED for signed manifests over HTTPS on loopback. No IPFS path was added. | `INDEPENDENCE` line includes the status note; `ipfs` is asserted false at `conformance_test.go` line 93. |
| REQ-TECH-006 | VERIFIED for the Go agent on toolchain `go1.26.10` from [runtime.txt](logs/goal-16/runtime.txt). | `go.mod` says `go 1.26.5` and `toolchain go1.26.10`. |
| REQ-OFF-003 | VERIFIED for the signed capability document. The chain was not given the endpoint URL. | `CAPABILITY` line. |
| REQ-API-002 | VERIFIED | Capability, acceptance, package fetch, receipt, and inventory are the lines `CAPABILITY`, `ACCEPT`, `TRANSFER`, `RECEIPT`, and `WITHDRAW` in [conformance.txt](logs/goal-16/conformance.txt). |
| REQ-HOST-007 host clock display | VERIFIED for this process | `DEADLINE` and `DEADLINE_UNCERTAIN` lines. The chain half of the row remains the goal 08 result. |

## Handoff

Install and run, from [agents/provider/README.md](../agents/provider/README.md):

```text
svn-provider init config.json
svn-provider run config.json
```

`hosting_consent` must be true. The process binds to loopback, writes `listen.json`, and does not start without that consent. `GET /health` and `GET /status` are the process and readiness endpoints. Package bytes are served only in `SERVING`.

## Independence tests that still need another person or another machine

- A second person installing this agent on infrastructure they control. Two processes here do not do that.
- A claim that two provider ids are different operators or different failure domains. [conformance.txt](logs/goal-16/conformance.txt) line 10 records both as `false` on `machine=vm-1`.
- Physical redundancy, a site failure, or a network partition between operators.
- Goal 30's independent installation. Not this goal's result.

## Head CI

Not observed yet. This section is filled after the branch is pushed. A missing run is not a pass.

## Log stamps

Every log under `docs/evidence/logs/goal-16/` starts with `HEAD 0ae57fcf1896cf863da1e867c1f9081007313c57` and `TREE 4cd758ce0806103e86d63dc0162e2ff3aa2edde7`. `DIRTY yes` is the untracked log directory. No tracked source file changed during a command. [diffstat.txt](logs/goal-16/diffstat.txt) is empty.

## Residual risk

The chain endpoint in the conformance test is an in-process fixture. A broadcast `{"code":0}` did not make the package serve. A four-validator commit of the built transaction was not run. The stall, deadline, and withdrawal results are against that fixture and the process clock. Receipt and inventory field sets are DR-16-01 and DR-16-02. The development key stays in the data directory and is not a production key.

## Next safe action

Open a draft pull request from `goal-16`. Do not merge it. Do not start goal 17 from an unreviewed branch. Public launch stays disabled.

## Proposed pull-request title and summary

Title: Add the loopback storage-provider agent

Summary: `svn-provider` enrolls a development key, refuses work without hosting consent, checks disk and quota, signs the exact lease terms with the chain helpers, and serves the package over loopback HTTPS only after the configured chain endpoint reports the lease. Two processes on one machine are not independent operators. The goal stays BLOCKED on custody, that single machine, the mutation anchors, golangci-lint, PostgreSQL 16, and compose. DR-16-01 through DR-16-06 are open.
