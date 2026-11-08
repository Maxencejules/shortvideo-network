#!/usr/bin/env python3
"""Goal 11 mutation runner (standard library only).

What it does (the design and CLI of docs/evidence/goal-10/mutants.py, with
the changes listed under "Changes from the goal 10 runner"):
1. Exports a committed revision (default HEAD) with `git archive` into a new
   temporary directory outside the repository. With --worktree it copies the
   working tree instead (see below).
2. Runs, once per selected group, every killer test that the group's
   selected mutants name, on the unmutated copy (control). A control passes
   only when every command exits 0, every named Go test printed a top-level
   `--- PASS` in its own package's output, and no named test was skipped. A
   failing control stops the run before any mutant.
3. For each mutant: applies its edits (each replaces one exact search string
   in one file; the string must occur exactly once in the file as it is when
   the edit is applied), runs only that mutant's named killer tests in the
   copy (`go test -run '^(names)$'` on the killers' packages, one command per
   set of build tags and lifecycle phases), and restores every edited file. The edits are written
   to the copy on disk, so tests that read source files (the static scans of
   internal/policy) and tests that build the svn-broadcaster binary (the
   smoke test) see the mutant too.

Verdicts:
- KILLED: a command exited non-zero, no Go package or test binary failed to
  build, and a NAMED killer failed: a top-level `--- FAIL: <name>` line in
  the output of the killer's own package, or a `panic:` in that package's
  output whose last `=== RUN` line before the panic belongs to the named
  test. A failure of any other test is not a kill.
- SURVIVED: every command exited zero.
- BUILD_FAILED: a Go package or test binary did not build, or, for a mutant
  that edits a migration (.pgsql), PostgreSQL reported a syntax error. This
  is not a kill.
- UNCLEAR: a command exited non-zero and no named killer failed. This is
  not a kill.
- TIMEOUT: a command ran longer than --timeout seconds.

Exit status is 0 only when every control passed and every mutant is KILLED.
It is non-zero when a search string is missing or not unique, or a named
killer is not defined (both checked for all selected mutants before any test
runs), when a control fails, or when any mutant is SURVIVED, BUILD_FAILED,
UNCLEAR or TIMEOUT.

Usage:
  python3 docs/evidence/goal-11/mutants.py [--rev REV | --worktree]
                                            [--group G[,G...]] [--only a,b]
                                            [--check] [--list] [--logdir DIR]
                                            [--keep] [--gocache DIR]
                                            [--timeout SECONDS]

--check validates the search strings and the named killers and runs
nothing: every killer must be a `func Test...(` or `func Fuzz...(` in a
_test.go file of the named package directory that a build with the
killer's tags compiles (a `//go:build tag` or `//go:build !tag` line is
evaluated); a killer's lifecycle phases must be phases of
chain/devnet/broadcaster_test.go; and a chain/devnet killer belongs to a
devnet mutant and only there. --list prints every mutant with its group, files and killers
and runs nothing.

--worktree copies the working tree instead of exporting --rev: every file
`git ls-files --cached --others --exclude-standard` lists that exists on
disk, as it is on disk (uncommitted edits and untracked, not ignored, files
included). It is for dry runs of uncommitted tests and mutants. Its output
is not evidence for a committed revision: the header names HEAD and prints
`git status --porcelain`, so a log shows what the copy held beyond HEAD.
--worktree and --rev are exclusive.

The copy runs Go with GOPROXY=off and `go test -p 2`, so the module cache
must already hold the dependencies and the toolchain that go.work names. By
default the copy uses its own GOCACHE under the temporary directory, which
is removed at the end unless --keep is given. --gocache DIR uses DIR
instead; the Go build cache is content-addressed, so this changes build time
only, never a verdict.

What the commands start. Only the devnet group starts validators: its
killers are the chain/devnet tests TestDevnetBroadcasterLifecycle (with
SVN_BROADCASTER_PHASES set to S plus the named phases, the selection
DR-11-36 allows for mutation killers; CI never sets it) and
TestDevnetBroadcasterQuorumStall. Each starts four svnd validator
processes on one machine, a dev-db PostgreSQL cluster and the
svn-broadcaster binary built in the copy (single machine, four validator
processes, private network namespace; not independent operators, not
production); the go test commands of a run run one at a time, so one devnet
runs at a time. The groups store, worker and smoke start disposable
PostgreSQL 18 clusters: internal/pgtest (store and worker) and the
svn-broadcaster dev-db subcommand (the smoke test) run initdb and postgres
from /usr/lib/postgresql/18/bin in a new directory under the system
temporary directory, listening on a Unix socket only, as the postgres user
when the runner is root; internal/pgtest holds a machine-wide flock so test
binaries of one run start one cluster at a time. The smoke test builds
svn-broadcaster and runs its subcommands (dev-ca, dev-db, migrate, serve,
signer, status, reconcile-once, signer-clear) against a fake node served by
the test; as root it runs signer and serve under one unused uid and then
under two distinct unused uids. The signing group's svn_roottest
killers need root: they copy their test binary into a temporary directory
and run it as unused uids. The api tests and the serve process listen on
loopback addresses only. The runs of this runner recorded for goal 11 ran
as root inside a private network namespace with only loopback up
(scratchpad netns-run.sh), and each run of a PostgreSQL group started only
after `pgrep -f 'svnd|postgres'` showed no other PostgreSQL or devnet run.

Groups (one set of commands per group, run from the root of the copy; the
packages and tests of a group are the union of its mutants' killers). The
first six start no PostgreSQL; store, worker, smoke and devnet do.
- outbox: the state machine of internal/outbox: the commit guard (COMMITTED
  only from a committed /tx result, T5/T11/T12), the dead rule (d1)-(d3)
  with the L margin and the R2 ante-failure rule, reconcile before resend,
  retries and their bounds, expiry, terminal states, and the stall overlay.
- chain-client: internal/chain: the broadcast and committed-result
  classifier, tx.acc_seq handling, hash-checked /tx, complete tx_search,
  height-bound account reads, timeout_height on every transaction, and the
  effect comparison with the signer rule and the R4 and R5 rulings.
- signing: internal/policy, internal/signer and internal/keys: the per-class
  signer allowlists (Q1), no authority_end, chain id and deadline checks,
  the intent-key registry (Q3), the provider pre-check, the verbatim forward
  rules (Q2), object permissions, start-up role refusals, the peer uid and
  key file rules, and the static rules (no signing outside internal/signer,
  no local preimage copy).
- api: internal/api and internal/api/request: mTLS principal binding,
  object permissions and uniform answers, relay authority, idempotency and
  the request digest, and the commitment evidence a COMMITTED view needs.
- config: internal/config (without its PostgreSQL test), strictjson,
  secretfile and internal/logx: redaction, the PG*/PQGO_DEBUG refusal, the
  chain id binding, number bounds and role rules.
- binary: cmd/svn-broadcaster tests that start no process: environment
  refusal first, uid checks, dev-ca and dev-db argument rules.
- store: internal/store over PostgreSQL: persist-before-send and the
  compare-and-set on the intent row, fencing, the (d2) CHECK, canonical
  commitment evidence, terminal immutability and the Q4 clearing triggers,
  object permissions in SQL, idempotency (R6), signer classes, forwarded
  transactions and caps.
- worker: internal/worker over PostgreSQL with a fake node (unit-labelled
  evidence, never end to end): persist before send, sends_started before
  each send, reconcile before resend, revalidation of permissions before
  signing, signer conflict halts, the stall rule, and the R3 simulate route.
- smoke: cmd/svn-broadcaster's TestSmokeProcesses, the built binary's
  subcommands against a dev-db cluster and a fake node.
- devnet: plan 13.7's devnet killers. Each dv-* mutant repeats the edit of
  a unit or PostgreSQL mutant (or, for the stall rule, the edits of two of
  them, because either guard alone keeps the rule) and names only a devnet
  test: OC for the signer rule of the effect comparison, WP2 for (d2) and
  an unrecorded send, the stall test for new attempts while stalled, and H
  for the R3 simulation route.

Devnet. The dv-* mutants are the only mutation evidence for devnet phases
OC, WP2, H and the stall test. Every other devnet phase of
chain/devnet/broadcaster*_test.go is an end-to-end demonstration on a single
machine (four validator processes, private network namespace), not
mutation evidence.

Changes from the goal 10 runner: a killer carries build tags (one go test
command per set of tags; --check evaluates single-tag build constraints)
and, for the devnet lifecycle test, the phases it runs (one go test command
per set of phases, with SVN_BROADCASTER_PHASES in its environment);
the default go test timeout is 10m; a PostgreSQL syntax error in a mutant
that edits a migration is BUILD_FAILED; there are no pytest killers, so the
pytest command, the Python and JSON syntax checks and SVN_SPEC_PYTHON are
gone; the header prints the euid and which control commands start
PostgreSQL.

Where the mutants come from (names start with their source; every source is
a scratch runner of a goal 11 work package or review, not in Git, whose
search strings are reused unchanged unless a line says otherwise; an edit
reported by two sources is listed once):
- w0b1-*, w0b2-*, w0b3-*: Wave 0 B1 (store), B2 (chain client), B3 (state
  machine) spot mutants.
- w0rt-*: the Wave 0 tests review (its five store mutants were not run then
  because of disk; they are run here).
- w0fix-*: the Wave 0 fix round (store S1-S10, migration Q1-Q7, chain C1,
  and its overlay mutant of the R2 ante-failure rule, rebuilt here as
  w0fix-r2-ante-failure-dead-before-deadline).
- w1b1b-*, w1b6-*, w1b7a-*, w1b7b-*, w1b8-*: Wave 1 B1b (store answers),
  B6 (signer and policy), B7a (API and config), B7b (binary), B8 (worker).
- w1rs-*, w1rt-*: the Wave 1 security and tests reviews.
- w1fix-*: the Wave 1 fix round (the review survivors P1, P4, S5/R1, A4,
  R8, W3-W8, R5, and its own F4b, F4c and F9 mutants).
- b10-*: added by this runner for rules no package runner mutated: the
  compare-and-set and expiry refusals of PersistAttempt, the advisory lock
  result, the R3, R4 and R5 rulings, the PROVIDER_MUST_RESIGN reason of the
  Wave 1 fix round, and the two static rules.
- b11-*: the Wave 2 fix round: the second replay check of a create that
  phase DU's defect needed, the "local clock decides death" row (expiry
  ends a live attempt), and the "no final leases" rule of plan 13.6 (a
  lease table added to the schema).
- dv-*: the devnet killers of plan 13.7 (see the devnet group).

Plan 13.7 rows and the mutants that cover them (a killer the plan names that
did not fail on its mutant in this runner's dry runs is not named):
| plan row | mutants |
| --- | --- |
| ack sets COMMITTED | w0b3-ack-sets-committed |
| T11 without the typed-event check | w0b3-t11-without-typed-event |
| T12 without the first-commit lookup | w0b3-t12-without-first-commit, w1b8-replay-hash-not-first-commitment |
| effect comparison ignores the signer | w0b2-effect-ignores-signer, dv-effect-ignores-signer (devnet OC) |
| DEAD-hash match as another account's commit | w0b3-dead-hash-in-settle-not-integrity, w0b3-dead-hash-in-check-effect-not-integrity, w0rt-dead-attempt-hit-becomes-sequence-used |
| drop the L margin | w0b3-drop-the-l-margin, w0rt-d3-boundary-off-by-one |
| (d2) ignores UNKNOWN sends | w0b3-d2-ignores-unknown |
| (d2) ignores sends_started | w0b3-d2-ignores-sends-started, w0b1-d2-check-removed, w1b8-unrecorded-send-not-unknown, w1rt-unrecorded-send-counted-refusal, dv-unrecorded-send-counted-refusal (devnet WP2) |
| sdk 32 or CACHE counted as a refusal | w0b3-sdk32-counted-as-refusal, w0b3-cache-counted-as-refusal, w0b2-sdk32-definitive, w0fix-sdk32-counts |
| local clock decides death | b11-expiry-kills-live-attempt (outbox.Judge reads no clock; the one clock-driven input, expiry on the database clock, is made to end a live attempt) |
| sequence consumed inferred from the code | w0b2-sequence-consumed-from-code |
| skip the effect check before a new attempt | w0b3-skip-effect-check-before-new-attempt, w0b3-persist-without-effect-check, w1fix-no-effect-check-before-retry |
| map a resend's code to the first attempt | w0rt-hash-not-verified-accepted, w0rt-swap-execution-failed-and-ante-failed |
| committed failure goes to FAILED without the effect check | w0b3-committed-failure-without-effect-check |
| state change without a transaction reported COMMITTED | w0b3-state-change-reported-committed |
| rebroadcast after expiry | w0b3-rebroadcast-after-expiry, w0b3-send-after-expiry |
| broadcast before the attempt row commits | w1b8-broadcast-before-persist |
| no sends_started increment before a send | w1b8-send-without-counted-start |
| drop the compare-and-set on the intent row | b10-persist-without-intent-state-cas, b10-persist-after-expiry |
| drop the terminal-row trigger | w0b1-terminal-intent-trigger-off |
| ignore advisory lock loss | w0b1-fence-check-skipped |
| reconcile-once without the lock | b10-advisory-lock-result-ignored, w1fix-lock-held-without-session-name |
| skip the stale-node height check | w0b2-stale-account-height-accepted, w0b3-stale-account-read-accepted |
| skip the signer-conflict halt | w1b8-attempt-above-next-sequence, w0b3-foreign-hit-not-a-conflict, w1b8-foreign-use-always-cleared |
| new attempt reuses an earlier timeout_height | w0b2-new-attempt-reuses-timeout, w0rt-equal-timeout-at-same-sequence |
| remove one type URL check in the signer | w1b6-signer-allowlist-before-decode-removed, w1b6-allowed-always-true |
| allow authority_end | w1b6-authority-end-allowed |
| drop the intent-key registry check | w1b6-intent-registry-check-dropped, w1b6-registry-membership-dropped |
| drop the timeout bound | w1b6-timeout-bound-dropped, w1b6-timeout-zero-allowed |
| accept any peer uid | w1b6-any-peer-uid |
| verify against the message key | w1b6-verify-against-message-key |
| skip the registered-key check | w1b6-registered-key-check-skipped |
| skip the owner check on withdraw | w1b6-withdraw-owner-principal-skipped |
| a PENDING owner row allows an offer | w1b6-pending-owner-allows-offer, w1b8-no-revalidation |
| drop the unique key | none: equivalent (see the list below) |
| digest over raw JSON | w1b7a-context-not-canonical, w1b7a-digest-omits-context |
| allow new attempts while stalled | w0b3-new-attempts-while-stalled, w1b8-stall-not-blocking, w0b1-stall-not-checked, dv-stall-guards-removed (devnet stall test) |
| stale observer shown as a stall | w0b3-stale-observer-shown-as-stall, w1b7a-stale-observer-shown-fresh |
| log the request body | w1b6-composite-values-logged, w1b6-json-like-strings-logged |
| skip the PG*/PQGO_DEBUG refusal | w1b7a-pqgo-debug-allowed, w1b7b-environment-not-checked-first |
| skip the chain id check | w1b7a-db-chain-mismatch-ignored, w1b6-chain-id-unchecked-in-signer |

Rows outside plan 13.7: the R3 simulation route (b10-r3-simulate-through-app-path,
dv-r3-simulate-through-app-path), a concurrent duplicate create refused by
its own PENDING owner row (b11-replay-after-authorize-dropped) and the
schema's lack of lease state (b11-lease-table-added).

Candidate mutants of the package and review runners that are not listed,
and why:
- B1b M03 (the intents guard lets a sent intent be cleared): equivalent, as
  B1b reported: the attempts_bytes_kept CHECK keeps tx_bytes on every
  attempt with a started send, so the guard's tx_bytes clause already
  refuses it.
- B1b M27 (settleFailed's consistency assertion removed): equivalent, as B1b
  reported: the assertion cannot fail while the intent row is locked.
- B7b M13 (status calls Expire with an empty intent id): equivalent: Expire
  refuses an id that is not a UUID before its first statement and rolls its
  transaction back, so status still writes nothing.
- Wave 1 security review P5 (CheckForwardedTx without its authority_end
  check): equivalent, as the review reported: CheckForwardedTx runs
  PrecheckRelay on the same message a few checks later, and PrecheckRelay
  refuses authority_end with the same code.
- The idempotency unique key (caller_principal, idempotency_key) of
  migration 0001 dropped: equivalent on this tree. Both enqueue paths (the
  HTTP insert of internal/store and enqueue_intent) first update the
  caller's caller_limits row, which serialises every enqueue of one caller,
  and then read the key under READ COMMITTED, so no second row with the
  same key is ever inserted (TestIdempotencyUniqueUnderConcurrency passes
  without the constraint).
- Mutants of migration 0001's intents_guard and enqueue_intent: migration
  0002 replaces both functions (CREATE OR REPLACE), so an edit of the 0001
  bodies changes nothing at run time. w0b1-terminal-intent-trigger-off and
  the two w0fix-r6-* mutants are re-anchored on the 0002 bodies.
- Wave 0 fix round S-sql-probe: a deliberate syntax error that showed the
  overlay reached the embedded migration, not a mutant.
- Duplicates listed once: Wave 1 tests review R1, R3, R4, R5, R6, R8 (the
  same edits as w1fix-client-check-signable-dropped,
  w1rs-forward-provider-account-unchecked, w1rs-offer-on-chain-host-unchecked,
  w1fix-no-effect-check-before-retry, w0b1-send-after-expiry and
  w1fix-committed-short-tx-hash); Wave 0 tests review S3 (w0b1-send-after-
  expiry); Wave 1 security review A1, ST1, ST2, ST4 and ST5 (the same
  effects as w1b7a-two-uri-sans-accepted, w1b1b-confirm-ignores-observed-
  host, w1b1b-scheduled-offer-ignores-committing-signer,
  w1b1b-content-owner-of-another-caller and
  w1b7b-idempotency-read-of-any-caller).

Test gaps closed in the Wave 2 fix round. Four mutants survived the first
dry runs on the Wave 2 working tree, each for want of one test case, which
now exists: b10-r3-simulate-through-app-path (TestSimulateUsesTxService
pins the literal route), w1rs-api-replay-ignores-sql-entry and
w0rt-http-key-reuses-sql-intent (TestIdempotencyConflict and
TestEnqueueHTTPRefusals repeat a validated SQL-path intent's key over HTTP
with the same digest), and w0b1-evidence-check-silenced
(TestCommitNeedsEvidence has a COMMITTED row with own_transaction = false,
which only the evidence check refuses).

Every mutant's killers are tests that failed on it in this runner's
own dry runs on the goal 11 working tree; a test first named from a package
report that did not fail on the mutant was taken off its list. This runner
re-runs all of them; its own output is the evidence, not the scratch logs.
"""

from __future__ import annotations

import argparse
import io
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
from dataclasses import dataclass

OUTBOX = "outbox"
CHAIN = "chain-client"
SIGNING = "signing"
API = "api"
CONFIG = "config"
BINARY = "binary"
STORE = "store"
WORKER = "worker"
SMOKE = "smoke"
DEVNET = "devnet"
GROUPS = (OUTBOX, CHAIN, SIGNING, API, CONFIG, BINARY, STORE, WORKER, SMOKE, DEVNET)
# The groups whose named tests start a disposable PostgreSQL 18 cluster
# (internal/pgtest, or the svn-broadcaster dev-db subcommand in the smoke
# and devnet tests). The other groups start no PostgreSQL and no validator.
POSTGRES_GROUPS = frozenset({STORE, WORKER, SMOKE, DEVNET})
# The groups whose named tests start svnd validators (four, on one machine).
VALIDATOR_GROUPS = frozenset({DEVNET})

GO_MODULE_PREFIX = "github.com/Maxencejules/shortvideo-network/"
GO_TEST_TIMEOUT = "10m"
# go test -timeout of a devnet mutant: S plus one phase, or the stall test,
# with the binaries built in the copy.
DEVNET_TIMEOUT = "15m"
# The environment variable that selects TestDevnetBroadcasterLifecycle's
# phases (chain/devnet/broadcaster_test.go, DR-11-36).
PHASES_ENV = "SVN_BROADCASTER_PHASES"

B = "services/broadcaster/internal/"
OB = B + "outbox"
CH = B + "chain"
PO = B + "policy"
SI = B + "signer"
KE = B + "keys"
AP = B + "api"
RQ = B + "api/request"
CF = B + "config"
SJ = B + "config/strictjson"
SF = B + "config/secretfile"
LX = B + "logx"
ST = B + "store"
WK = B + "worker"
CMD = "services/broadcaster/cmd/svn-broadcaster"
DV = "chain/devnet"
MIG1 = ST + "/migrations/0001_outbox.pgsql"
MIG2 = ST + "/migrations/0002_answers.pgsql"
ROOT_TAG = "svn_roottest"


@dataclass(frozen=True)
class Killer:
    """A test that must fail on the mutant: a Go test or fuzz target in a
    package directory (repository-relative, e.g. services/broadcaster/
    internal/outbox), built with the given build tags (empty for the
    default build). phases, for TestDevnetBroadcasterLifecycle only, is the
    SVN_BROADCASTER_PHASES value: the test runs phase S plus these."""

    package: str
    name: str
    tags: str = ""
    phases: str = ""

    def label(self) -> str:
        suffix = f"[{self.tags}]" if self.tags else ""
        if self.phases:
            suffix += f"[{PHASES_ENV}={self.phases}]"
        return f"{self.package}:{self.name}{suffix}"


def K(package: str, *names: str, tags: str = "") -> tuple[Killer, ...]:
    return tuple(Killer(package, name, tags) for name in names)


def KL(phases: str) -> tuple[Killer, ...]:
    """TestDevnetBroadcasterLifecycle with phase S plus phases."""
    return (Killer(DV, "TestDevnetBroadcasterLifecycle", phases=phases),)


@dataclass(frozen=True)
class Edit:
    path: str
    old: str
    new: str


@dataclass(frozen=True)
class Mutant:
    name: str
    group: str
    edits: tuple[Edit, ...]
    killers: tuple[Killer, ...]
    reason: str
    # go test -timeout for this mutant's command.
    timeout: str = GO_TEST_TIMEOUT


def one(name: str, group: str, path: str, old: str, new: str, killers: tuple[Killer, ...], reason: str,
        timeout: str = GO_TEST_TIMEOUT) -> Mutant:
    return Mutant(name, group, (Edit(path, old, new),), killers, reason, timeout)


MUTANTS: list[Mutant] = [
    # ---------------------------------------------------------------- outbox (unit)
    # Commit guard: COMMITTED only from a committed /tx result (T5, T11, T12).
    one("w0b3-ack-sets-committed", OUTBOX, OB + "/machine.go",
        "step.ID, step.After.Phase = T9, PhaseAcknowledged",
        "step.ID, step.After = T11, committed(in, a.Hash, true)",
        K(OB, "TestAckNeverCommits", "TestNoCommittedWithoutCommittedResult", "TestStateMachineProperty"),
        "a CheckTx acknowledgement is ACKNOWLEDGED, never COMMITTED (goal spec: a broadcast ack is not committed success)"),
    one("w0b3-t11-without-typed-event", OUTBOX, OB + "/judge.go",
        "case tx.AccSeq != AccSeqThis || tx.TypedEvent == tx.Idempotent:",
        "case tx.AccSeq != AccSeqThis:",
        K(OB, "TestCommitNeedsTypedEvent", "TestNoCommittedWithoutCommittedResult"),
        "code 0 commits only with this attempt's acc_seq and exactly one of the typed event or the idempotent marker"),
    one("w0b3-t12-without-first-commit", OUTBOX, OB + "/machine.go",
        "case EffectNone:\n\t\t\treturn hold(AlertFirstCommitNotFound, 0)",
        "case EffectNone:\n\t\t\tstep.ID, step.After = T12, committed(in, v.attempt.Hash, true)\n\t\t\treturn step, nil",
        K(OB, "TestIdempotentReplayNeedsFirstCommit", "TestNoCommittedWithoutCommittedResult"),
        "an idempotent replay commits (T12) only once the first commitment of the business id is found"),
    one("w0b3-dead-hash-in-settle-not-integrity", OUTBOX, OB + "/machine.go",
        "\t\tif eff.DeadAttempt {\n\t\t\treturn hold(AlertDeadAttemptCommitted, HaltIntegrity)\n\t\t}\n",
        "",
        K(OB, "TestDeadAttemptHashMatchIsIntegrityAlert", "TestIdempotentReplayNeedsFirstCommit",
          "TestNoCommittedWithoutCommittedResult"),
        "an effect committed by an attempt the outbox marked DEAD is an integrity hold, never another account's commit"),
    one("w0b3-dead-hash-in-check-effect-not-integrity", OUTBOX, OB + "/machine.go",
        "\t\tif eff.DeadAttempt {\n\t\t\tstep.Alert, step.Halt = AlertDeadAttemptCommitted, HaltIntegrity\n\t\t\treturn step, nil\n\t\t}\n",
        "",
        K(OB, "TestDeadAttemptHashMatchIsIntegrityAlert", "TestNoCommittedWithoutCommittedResult"),
        "the effect check before a new attempt holds on a DEAD attempt's hash as well"),
    one("w0rt-settle-own-failed-attempt-canonical", OUTBOX, OB + "/machine.go",
        "\t\tif eff.Canonical == v.attempt.Hash {",
        "\t\tif false && eff.Canonical == v.attempt.Hash {",
        K(OB, "TestIdempotentReplayNeedsFirstCommit", "TestNoCommittedWithoutCommittedResult"),
        "a failed attempt of this intent is never reported as the canonical commitment"),
    one("w0rt-hash-not-verified-accepted", OUTBOX, OB + "/judge.go",
        "\tif !tx.HashVerified {",
        "\tif false && !tx.HashVerified {",
        K(OB, "TestCommitNeedsTypedEvent", "TestNoCommittedWithoutCommittedResult", "TestResendCodeNeverMapsToFirstAttempt"),
        "a /tx result whose bytes do not hash to the attempt's hash holds and halts; a resend's code never maps to the first attempt"),
    # Dead rule (plan 8.5) and the R2 ante-failure rule.
    one("w0b3-drop-the-l-margin", OUTBOX, OB + "/judge.go",
        "deadline := a.TimeoutHeight + limits.IndexingMargin",
        "deadline := a.TimeoutHeight",
        K(OB, "TestDeadRuleD1D2D3"),
        "(d3) waits until the height reaches timeout_height + L, not timeout_height"),
    one("w0rt-d3-boundary-off-by-one", OUTBOX, OB + "/judge.go",
        "\tif view.Height < deadline {\n\t\treturn live, nil",
        "\tif view.Height+1 < deadline {\n\t\treturn live, nil",
        K(OB, "TestDeadRuleD1D2D3"),
        "height T+L-1 keeps the attempt live"),
    one("w0b3-d2-ignores-unknown", OUTBOX, OB + "/send.go",
        "s.Started >= 1 && s.Recorded == s.Started && s.Refusals == s.Recorded",
        "s.Started >= 1 && s.Recorded == s.Started && s.Refusals+s.Unknown == s.Recorded",
        K(OB, "TestDeadRuleD1D2D3", "TestNoCommittedWithoutCommittedResult", "TestStateMachineProperty"),
        "(d2) needs every started send recorded as a counted refusal; an UNKNOWN send keeps the attempt live"),
    one("b11-expiry-kills-live-attempt", OUTBOX, OB + "/machine.go",
        "\tafter := in\n\tafter.Phase = PhaseAwaitingDeadline\n\treturn Step{ID: T15, Before: in, After: after}",
        "\treturn Step{ID: T4, Before: in, After: failed(in, ReasonExpiredDead, Code{})}",
        K(OB, "TestExpiryNeverAbandonsLiveAttempt"),
        "local clock decides death: expiry (the database clock) never ends an IN_FLIGHT intent; it waits for the chain "
        "(T15, AWAITING_DEADLINE) until its attempt commits or dies by chain proof"),
    one("w0b3-d2-ignores-sends-started", OUTBOX, OB + "/send.go",
        "s.Started >= 1 && s.Recorded == s.Started && s.Refusals == s.Recorded",
        "s.Recorded >= 1 && s.Refusals == s.Recorded",
        K(OB, "TestStateMachineProperty", "TestUnrecordedSendCountsUnknown"),
        "(d2) counts sends_started: a send started but not recorded is UNKNOWN, so the attempt is not dead"),
    one("w0b3-sdk32-counted-as-refusal", OUTBOX, OB + "/send.go",
        "return s.Code != CodeSDKWrongSequence",
        "return true",
        K(OB, "TestSdk32AndCacheNeverDefinitive"),
        "a CheckTx sdk 32 answer is never a definitive refusal for (d2)"),
    one("w0b3-cache-counted-as-refusal", OUTBOX, OB + "/send.go",
        "\tcase SendNodeRefused:\n\t\treturn true\n",
        "\tcase SendNodeRefused, SendCache:\n\t\treturn true\n",
        K(OB, "TestSdk32AndCacheNeverDefinitive", "TestDeadRuleD1D2D3", "TestStateMachineProperty"),
        "a tx-in-cache answer is never a definitive refusal for (d2)"),
    one("w0b3-unrecorded-send-not-uncertain", OUTBOX, OB + "/judge.go",
        "if a.Phase == AttemptSigned && a.Sends.Unrecorded() > 0 {",
        "if false {",
        K(OB, "TestUnrecordedSendCountsUnknown"),
        "a SIGNED attempt with an unrecorded send is reported UNCERTAIN"),
    one("w0b3-stale-account-read-accepted", OUTBOX, OB + "/judge.go",
        "if view.Account == nil || view.Account.Height < deadline {",
        "if view.Account == nil {",
        K(OB, "TestDeadRuleD1D2D3"),
        "(d3) deadline-passed needs an account read at a height of at least T+L (a stale node is not believed)"),
    one("w0rt-account-seq-equal-kept-live", OUTBOX, OB + "/judge.go",
        "if view.Account.Sequence <= a.Sequence {",
        "if view.Account.Sequence < a.Sequence {",
        K(OB, "TestDeadRuleD1D2D3", "TestDeadAttemptHashMatchIsIntegrityAlert", "TestReconcileBeforeResend"),
        "an account sequence equal to the attempt's sequence at T+L means the attempt can no longer commit"),
    one("w0b3-foreign-hit-not-a-conflict", OUTBOX, OB + "/judge.go",
        "\tcase OwnerForeign:\n\t\treturn Verdict{kind: VerdictHold, attempt: a, alert: AlertSignerConflict, halt: HaltSignerConflict}\n",
        "",
        K(OB, "TestDeadRuleD1D2D3", "TestExpiryNeverAbandonsLiveAttempt"),
        "a foreign transaction at our sequence that nobody acknowledged is a signer conflict that halts the signer"),
    one("w0rt-dead-attempt-hit-becomes-sequence-used", OUTBOX, OB + "/judge.go",
        "\tcase OwnerOursDead:\n\t\treturn Verdict{kind: VerdictHold, attempt: a, alert: AlertDeadAttemptCommitted, halt: HaltIntegrity}\n",
        "",
        K(OB, "TestDeadAttemptHashMatchIsIntegrityAlert"),
        "a search hit by an attempt the outbox marked DEAD is a fatal integrity hold"),
    one("w0rt-search-hit-own-hash-treated-as-other", OUTBOX, OB + "/judge.go",
        "\tif hit.Hash == a.Hash {",
        "\tif false && hit.Hash == a.Hash {",
        K(OB, "TestDeadRuleD1D2D3"),
        "a search hit naming this attempt while /tx does not find it is an inconsistency, not a death"),
    one("w0b3-d3-hit-by-our-attempt-ignored", OUTBOX, OB + "/judge.go",
        "\t\tif len(view.Search) == 1 {\n\t\t\treturn judgeHit(a, view.Search[0], live), nil\n\t\t}\n",
        "",
        K(OB, "TestDeadRuleD1D2D3", "TestDeadAttemptHashMatchIsIntegrityAlert", "TestExpiryNeverAbandonsLiveAttempt"),
        "(d3) consults the acc_seq search hit before the account sequence"),
    one("w0rt-multiple-search-hits-picks-first", OUTBOX, OB + "/judge.go",
        "\t\tif len(view.Search) > 1 {",
        "\t\tif len(view.Search) > 100 {",
        K(OB, "TestDeadRuleD1D2D3"),
        "two transactions at one sequence are an inconsistency alert, not a verdict"),
    one("w0rt-swap-execution-failed-and-ante-failed", OUTBOX, OB + "/judge.go",
        "\tif tx.AccSeq == AccSeqThis {\n\t\treturn resolved(AttemptExecutionFailed)",
        "\tif tx.AccSeq != AccSeqThis {\n\t\treturn resolved(AttemptExecutionFailed)",
        K(OB, "TestCommittedFailureRunsEffectCheck", "TestDeadRuleD1D2D3", "TestResendCodeNeverMapsToFirstAttempt",
          "TestRetryRules"),
        "only a committed failure with this attempt's acc_seq consumed the sequence (EXECUTION_FAILED)"),
    one("w0fix-r2-ante-failure-dead-before-deadline", OUTBOX, OB + "/judge.go",
        "\tif !pastDeadline {\n\t\treturn live\n\t}\n\treturn resolved(AttemptIncludedAnteFailed)",
        "\treturn resolved(AttemptIncludedAnteFailed)",
        K(OB, "TestDeadRuleD1D2D3", "TestStateMachineProperty"),
        "R2: a committed ante failure (no acc_seq) keeps the attempt live until the height reaches T+L, because CometBFT "
        "lets the same bytes be resubmitted and the indexer lets a later success replace the failure"),
    # Reconcile before any resend (plan 8.6).
    one("w0b3-skip-effect-check-before-new-attempt", OUTBOX, OB + "/reconcile.go",
        "if p.Effect.Verdict == 0 {\n\t\treturn Decision{Need: NeedEffect}, nil\n\t}",
        "if p.Effect.Verdict == 0 {\n\t\tp.Effect.Verdict = EffectNone\n\t}",
        K(OB, "TestReconcileBeforeResend", "TestStateMachineProperty"),
        "a new attempt waits for the effect check of plan 8.6 step 3"),
    one("w0b3-persist-without-effect-check", OUTBOX, OB + "/machine.go",
        "\tcase eff.Verdict != EffectNone:\n\t\treturn guardFailed(T8, \"the effect check did not find the effect absent\")\n",
        "",
        K(OB, "TestReconcileBeforeResend"),
        "T8 (persist a new attempt) needs an effect check that found the effect absent"),
    one("w0b3-committed-failure-without-effect-check", OUTBOX, OB + "/machine.go",
        "\tcase 0:\n\t\treturn Step{}, ErrNeedEffect\n\tcase EffectSignerConflict:",
        "\tcase 0:\n\t\teff.Verdict = EffectNone\n\tcase EffectSignerConflict:",
        K(OB, "TestIdempotentReplayNeedsFirstCommit", "TestReconcileBeforeResend"),
        "a committed failure runs the effect check before the intent can fail"),
    one("w0b3-state-change-reported-committed", OUTBOX, OB + "/machine.go",
        "\tcase EffectStateChanged:\n\t\tstep.ID, step.After = T7, failed(in, ReasonObjectStateChanged, Code{})\n\t\treturn step, nil",
        "\tcase EffectStateChanged:\n\t\tstep.ID, step.After = T5, committed(in, v.attempt.Hash, true)\n\t\treturn step, nil",
        K(OB, "TestStateChangeWithoutTxIsObjectStateChanged", "TestCommittedFailureRunsEffectCheck",
          "TestNoCommittedWithoutCommittedResult"),
        "an object changed by another message type is FAILED(OBJECT_STATE_CHANGED), never COMMITTED"),
    one("w0b3-rebroadcast-after-expiry", OUTBOX, OB + "/reconcile.go",
        "d.Rebroadcast = v.kind == VerdictLive && !p.Guard.Expired && cur.Phase != PhaseAwaitingDeadline",
        "d.Rebroadcast = v.kind == VerdictLive",
        K(OB, "TestNoSendAfterExpiry", "TestExpiryNeverAbandonsLiveAttempt"),
        "no rebroadcast after the intent's expires_at"),
    one("w0b3-send-after-expiry", OUTBOX, OB + "/machine.go",
        "if e.Expired || in.Phase == PhaseAwaitingDeadline {",
        "if false {",
        K(OB, "TestNoSendAfterExpiry", "TestStateMachineProperty"),
        "StartSend is refused after expiry and in AWAITING_DEADLINE"),
    one("w0rt-awaiting-deadline-allows-start-send", OUTBOX, OB + "/machine.go",
        "if e.Expired || in.Phase == PhaseAwaitingDeadline {",
        "if e.Expired {",
        K(OB, "TestNoSendAfterExpiry"),
        "an intent in AWAITING_DEADLINE sends nothing more"),
    one("w0b3-terminal-states-do-not-absorb", OUTBOX, OB + "/machine.go",
        "\tif in.State.Terminal() {\n\t\treturn Step{ID: T17, Before: in, After: in}, nil\n\t}\n",
        "",
        K(OB, "TestTerminalStatesAbsorb", "TestStateMachineProperty"),
        "COMMITTED and FAILED absorb every later event (T17)"),
    one("w0b3-no-per-signer-window", OUTBOX, OB + "/machine.go",
        "\tcase !g.SignerIdle:\n\t\treturn guardFailed(T8, \"the signer has a live attempt\")\n",
        "",
        K(OB, "TestPersistGuards", "TestStateMachineProperty"),
        "one live attempt per signer: no new attempt while the signer has one"),
    one("w0b3-cancel-after-signing", OUTBOX, OB + "/machine.go",
        "\t\tif in.Attempts > 0 {\n\t\t\treturn Step{}, guardFailed(T3, \"an attempt was persisted\")\n\t\t}\n",
        "",
        K(OB, "TestCancelOnlyBeforeSigning"),
        "cancel is allowed only before an attempt was persisted"),
    one("w0b3-phase-loses-knowledge", OUTBOX, OB + "/send.go",
        "return max(p, AttemptUncertain)",
        "return AttemptUncertain",
        K(OB, "TestPhaseNeverLosesKnowledge", "TestStateMachineProperty"),
        "an attempt phase never falls back below what is known (ACKNOWLEDGED stays ACKNOWLEDGED)"),
    one("w0b3-replay-unbounded", OUTBOX, OB + "/retry.go",
        "\t\tcase newBusiness && in.Business >= limits.BusinessAttempts:\n\t\t\tstep.After = failed(step.After, ReasonBusinessIDTaken, Code{})\n",
        "",
        K(OB, "TestReplayOpensBoundedBusinessAttempt"),
        "business attempts after REPLAY are bounded (BUSINESS_ID_TAKEN)"),
    one("w0b3-sdk4-unbounded", OUTBOX, OB + "/retry.go",
        "if step.After.Sdk4Deaths > limits.Sdk4Retries {",
        "if false {",
        K(OB, "TestRetryRules"),
        "sdk 4 (unauthorized at CheckTx) retries are bounded"),
    one("w0rt-sdk4-retry-without-two-block-gap", OUTBOX, OB + "/retry.go",
        "\t\t\tstep.WaitBlocks = Sdk4RetryGapBlocks",
        "\t\t\tstep.WaitBlocks = 1",
        K(OB, "TestRetryRules"),
        "an sdk 4 retry waits two blocks (DR-10-41)"),
    one("w0b3-unauthorized-refusal-does-not-halt", OUTBOX, OB + "/retry.go",
        "\t\tstep.Halt = HaltUnauthorized\n",
        "",
        K(OB, "TestRetryRules"),
        "a committed UNAUTHORIZED failure halts the signer"),
    one("w0b3-retryable-ante-failure-fails", OUTBOX, OB + "/retry.go",
        "case CodeSDKTimeoutHeight, CodeSDKTimeout, CodeSDKWrongSequence, CodeSDKOutOfGas:",
        "case CodeSDKTimeout:",
        K(OB, "TestRetryRules", "TestCommittedFailureRunsEffectCheck"),
        "timeout height, wrong sequence and out-of-gas ante failures are retried, not failed"),
    one("w0b3-caps-ignored-on-retry", OUTBOX, OB + "/retry.go",
        "\t\tcase in.Attempts >= limits.AttemptsPerIntent,\n\t\t\t!newBusiness && in.BusinessTries >= limits.AttemptsPerBusinessID:\n\t\t\tstep.After = failed(step.After, ReasonAttemptsExhausted, Code{})\n",
        "",
        K(OB, "TestRetryRules", "TestReplayOpensBoundedBusinessAttempt"),
        "attempts per intent and per business id are bounded"),
    one("w0b3-expiry-ignored-on-retry", OUTBOX, OB + "/retry.go",
        "\t\tcase expired:\n\t\t\tstep.After = failed(step.After, ReasonExpiredDead, Code{})\n",
        "",
        K(OB, "TestRetryRules", "TestExpiryNeverAbandonsLiveAttempt", "TestReplayOpensBoundedBusinessAttempt"),
        "an expired intent whose attempt died is FAILED(EXPIRED_DEAD), not retried"),
    # Outage: stall episodes block new attempts; a stale observer is not a stall.
    one("w0b3-new-attempts-while-stalled", OUTBOX, OB + "/machine.go",
        "\tcase g.Stalled:\n\t\treturn guardFailed(T8, \"a stall episode is open\")\n",
        "",
        K(OB, "TestStallBlocksNewAttempts"),
        "no new attempt while a stall episode is open"),
    one("w0b3-stale-observer-shown-as-stall", OUTBOX, OB + "/present.go",
        "if o.StallOpen && o.ObserverFresh {",
        "if o.StallOpen {",
        K(OB, "TestObserverStaleNotStall", "TestStallOverlayNeverCommits"),
        "a dead observer is OBSERVER_UNAVAILABLE, never PENDING_STALL"),
    one("b10-provider-must-resign-not-a-failure-reason", OUTBOX, OB + "/types.go",
        "\treturn (r >= ReasonExecutionFailed && r <= ReasonCancelled) || r == ReasonProviderMustResign",
        "\treturn (r >= ReasonExecutionFailed && r <= ReasonCancelled)",
        K(OB, "TestNamesRoundTrip") + K(AP, "TestStatusViews"),
        "PROVIDER_MUST_RESIGN is a failure reason a FAILED intent can carry (Wave 1 fix round)"),

    # ---------------------------------------------------------------- chain-client (unit)
    # Classifier (plan 8.4).
    one("w0b2-sdk32-definitive", CHAIN, CH + "/classify.go",
        "return !o.IsSDK(sdkerrors.ErrWrongSequence.ABCICode())",
        "return true",
        K(CH, "TestClassifyBroadcastAnswers"),
        "a CheckTx sdk 32 answer is not a definitive refusal"),
    one("w0rt-unrecognised-rpc-data-node-refused", CHAIN, CH + "/classify.go",
        "\treturn SendObservation{Outcome: SendUnknown, Detail: clip(data)}\n}",
        "\treturn SendObservation{Outcome: SendNodeRefused, Detail: clip(data)}\n}",
        K(CH, "TestClassifyBroadcastAnswers", "FuzzClassifyRPCError"),
        "an unrecognised JSON-RPC error text is UNKNOWN (it may have been delivered), not a node refusal"),
    one("w0rt-any-jsonrpc-error-code-trusted", CHAIN, CH + "/classify.go",
        "if !errors.As(err, &rpcErr) || rpcErr.Code != rpcInternalCode || rpcErr.Message != rpcInternalMessage {",
        "if !errors.As(err, &rpcErr) {",
        K(CH, "FuzzClassifyRPCError"),
        "only CometBFT's internal-error code and message carry a refusal text"),
    one("w0b2-tx-in-cache-substring", CHAIN, CH + "/classify.go",
        "\tcase data == textTxInCache:",
        "\tcase strings.Contains(data, textTxInCache):",
        K(CH, "TestClassifyBroadcastAnswers"),
        "the tx-in-cache answer is recognised by its exact text"),
    one("w0b2-sequence-consumed-from-code", CHAIN, CH + "/classify.go",
        "\t\tif len(seqs) == 1 {\n\t\t\tobs.Class = ExecutionFailed",
        "\t\tif com.Codespace == \"svn\" {\n\t\t\tobs.Class = ExecutionFailed",
        K(CH, "TestSequenceConsumedOnlyWithAccSeq"),
        "a committed failure consumed the sequence only when it carries tx.acc_seq, never inferred from the code"),
    one("w0rt-acc-seq-of-another-sequence", CHAIN, CH + "/classify.go",
        "(string(seqs[0].Address) != string(ref.Signer) || seqs[0].Sequence != ref.Sequence)",
        "(string(seqs[0].Address) != string(ref.Signer))",
        K(CH, "TestSequenceConsumedOnlyWithAccSeq"),
        "an acc_seq of another sequence is an integrity error"),
    one("w0rt-success-without-acc-seq", CHAIN, CH + "/classify.go",
        "\tif len(seqs) == 0 {\n\t\treturn CommitObservation{}, &IntegrityError{",
        "\tif false && len(seqs) == 0 {\n\t\treturn CommitObservation{}, &IntegrityError{",
        K(CH, "TestSequenceConsumedOnlyWithAccSeq"),
        "a successful result without tx.acc_seq is an integrity error"),
    one("w0rt-typed-event-of-another-object", CHAIN, CH + "/classify.go",
        "if ev.MsgIndex != 0 || !key.matches(ev.Msg) {",
        "if ev.MsgIndex != 0 {",
        K(CH, "TestSequenceConsumedOnlyWithAccSeq"),
        "the typed event must name the intent's object"),
    one("w0fix-classify-ignores-ref-hash", CHAIN, CH + "/classify.go",
        "\tif com.Hash != ref.Hash || sha256.Sum256(com.Raw) != com.Hash {",
        "\tif sha256.Sum256(com.Raw) != com.Hash {",
        K(CH, "TestSequenceConsumedOnlyWithAccSeq"),
        "a committed result is classified only for the attempt whose hash it carries (Wave 0 fix 7)"),
    one("w0b2-result-hash-unchecked", CHAIN, CH + "/classify.go",
        "\tif string(res.Hash) != string(hash[:]) {",
        "\tif false && string(res.Hash) != string(hash[:]) {",
        K(CH, "TestClassifyBroadcastAnswers"),
        "a broadcast answer that names another hash is UNKNOWN, never an acknowledgement of these bytes"),
    # Reads: hash-checked /tx, complete search, height-bound account reads.
    one("w0b2-tx-bytes-hash-unchecked", CHAIN, CH + "/rpc.go",
        "\tif want != nil && string(want) != string(sum[:]) {",
        "\tif false && want != nil && string(want) != string(sum[:]) {",
        K(CH, "TestCommittedTxChecksHash"),
        "committed bytes that do not hash to the asked hash are refused"),
    one("w0b2-incomplete-search-accepted", CHAIN, CH + "/rpc.go",
        "\treturn nil, ErrSearchIncomplete",
        "\treturn out, nil",
        K(CH, "TestTxSearchQueries"),
        "a tx_search answer with more hits than returned is refused (no silent truncation)"),
    one("w0b2-stale-account-height-accepted", CHAIN, CH + "/rpc.go",
        "\tif r.Height < minHeight {",
        "\tif false && r.Height < minHeight {",
        K(CH, "TestAccountReadHeightBound"),
        "an account read answered below the needed height is stale"),
    one("b10-r3-simulate-through-app-path", CHAIN, CH + "/rpc.go",
        "\tpathSimulate     = \"/cosmos.tx.v1beta1.Service/Simulate\"",
        "\tpathSimulate     = \"/app/simulate\"",
        K(CH, "TestSimulateUsesTxService"),
        "R3: gas is measured through the SDK transaction service's Simulate method, not /app/simulate"),
    # Transaction building: timeout_height.
    one("w0b2-timeout-height-dropped", CHAIN, CH + "/tx.go",
        "\tbuilder.SetTimeoutHeight(timeoutHeight)",
        "\tbuilder.SetTimeoutHeight(0)",
        K(CH, "TestBuildTxMatchesChainDecoder"),
        "every signed transaction carries its timeout_height"),
    one("w0b2-new-attempt-reuses-timeout", CHAIN, CH + "/tx.go",
        "\t\tif t >= timeout {",
        "\t\tif t > timeout {",
        K(CH, "TestNewAttemptTimeoutAboveEarlier"),
        "a new attempt's timeout_height is above every earlier attempt's"),
    # Effect comparison (plan 8.6) with the signer rule, R4 and R5.
    one("w0b2-effect-ignores-signer", CHAIN, CH + "/effect.go",
        "\tif key.SignerBound && !ourKey {",
        "\tif false && key.SignerBound && !ourKey {",
        K(CH, "TestEffectComparison"),
        "an offer, register or withdraw committed by another account is OBJECT_TAKEN, not our effect"),
    one("w0b2-r4-our-key-foreign-hash-not-conflict", CHAIN, CH + "/effect.go",
        "\tif ourKey && owner == HashForeign {",
        "\tif false && ourKey && owner == HashForeign {",
        K(CH, "TestEffectComparison"),
        "R4: a commit by our key with a hash this outbox does not hold is SIGNER_CONFLICT, for every kind"),
    one("b10-r4-relay-signer-bound", CHAIN, CH + "/effect.go",
        "\t\tkey = ObjectKey{EventType: \"svn.events.v1.LeaseAccepted\", Module: leases.ModuleName, IDKey: \"lease_id\", ID: m.GetLeaseId()}",
        "\t\tkey = ObjectKey{EventType: \"svn.events.v1.LeaseAccepted\", Module: leases.ModuleName, IDKey: \"lease_id\", ID: m.GetLeaseId(), SignerBound: true}",
        K(CH, "TestEffectComparison"),
        "a relayed acceptance committed by the provider's own account is still the intended effect (signer not bound)"),
    one("w0b2-effect-skips-package-bytes", CHAIN, CH + "/effect.go",
        "\t\tif name := string(fd.Name()); name == fieldSigner || name == fieldBusinessID {",
        "\t\tif name := string(fd.Name()); name == fieldSigner || name == fieldBusinessID || name == \"package_bytes\" {",
        K(CH, "TestEffectComparison"),
        "every business field other than signer and business_id changes the effect comparison"),
    one("b10-r5-effect-compares-business-id", CHAIN, CH + "/effect.go",
        "\t\tif name := string(fd.Name()); name == fieldSigner || name == fieldBusinessID {",
        "\t\tif name := string(fd.Name()); name == fieldSigner {",
        K(CH, "TestEffectComparison"),
        "R5: business_id is excluded from the effect comparison (a provider's own acceptance under its own id is the effect)"),
    one("w0rt-own-attempt-other-key-not-integrity", CHAIN, CH + "/effect.go",
        "\tif owner == HashThisIntent && !ourKey {",
        "\tif false && owner == HashThisIntent && !ourKey {",
        K(CH, "TestEffectComparison"),
        "our own attempt signed by another key than the pinned signer is an integrity error"),
    one("w0rt-other-intent-attempt-own-transaction", CHAIN, CH + "/effect.go",
        "out.OwnTransaction = EffectCommitted, owner == HashThisIntent",
        "out.OwnTransaction = EffectCommitted, owner != HashForeign",
        K(CH, "TestEffectComparison"),
        "own_transaction is true only for this intent's own attempt"),
    one("w0rt-two-typed-events-for-one-object", CHAIN, CH + "/effect.go",
        "\t\t\tif found {\n\t\t\t\treturn Effect{}, &IntegrityError{",
        "\t\t\tif false && found {\n\t\t\t\treturn Effect{}, &IntegrityError{",
        K(CH, "TestEffectStateChangedAndRefusals"),
        "two typed events for one object in one transaction are an integrity error"),
    one("w0rt-other-message-type-not-state-changed", CHAIN, CH + "/effect.go",
        "\tif committed.TypeURL != key.TypeURL {",
        "\tif false && committed.TypeURL != key.TypeURL {",
        K(CH, "TestEffectStateChangedAndRefusals"),
        "an object changed by another message type is OBJECT_STATE_CHANGED"),

    # ---------------------------------------------------------------- signing (unit; policy, signer, keys)
    # Signer allowlists per key class (Q1), no authority_end.
    one("w1b6-signer-allowlist-before-decode-removed", SIGNING, SI + "/signer.go",
        "\tif !policy.Allowed(class, typeURL) {",
        "\tif false {",
        K(SI, "TestSignerRefusesOtherTypeURLs"),
        "the signer refuses every type URL outside its key class's allowlist before decoding"),
    one("w1b6-allowed-always-true", SIGNING, PO + "/kinds.go",
        "func Allowed(class Class, typeURL string) bool { return contains(AllowedTypeURLs(class), typeURL) }",
        "func Allowed(class Class, typeURL string) bool { return true }",
        K(SI, "TestSignerRefusesOtherTypeURLs"),
        "the per-class allowlist (SERVICE: register, withdraw, offer; BROADCASTER: accept, renew, end) is enforced"),
    one("w1rt-end-lease-moved-to-service-class", SIGNING, PO + "/kinds.go",
        "\t{KindRelayEndLease, ClassBroadcaster, TypeEndLease, PermRelayProviderSigned},",
        "\t{KindRelayEndLease, ClassService, TypeEndLease, PermRelayProviderSigned},",
        K(PO, "TestKindSignerAllowlist") + K(SI, "TestSignerRefusesAuthorityEnd"),
        "a provider-signed EndLease is relayed by the BROADCASTER key only"),
    one("w1rs-check-signable-signer-unchecked", SIGNING, PO + "/kinds.go",
        "if !ok || len(signer) != genesis.AccountBytes || !bytes.Equal(got, signer) {",
        "if !ok || len(signer) != genesis.AccountBytes || (len(got) == 0 && bytes.Equal(got, signer)) {",
        K(PO, "TestKindSignerAllowlist", "TestOfferAndOwnership", "TestPublicationPermission"),
        "a message is signable only by the key whose address is its signer field"),
    one("w1b6-authority-end-allowed", SIGNING, PO + "/kinds.go",
        "ok && end.GetAuthorityEnd() {\n\t\treturn refuse(CodeAuthorityEnd, \"EndLease with authority_end is never signed",
        "ok && end.GetAuthorityEnd() && false {\n\t\treturn refuse(CodeAuthorityEnd, \"EndLease with authority_end is never signed",
        K(SI, "TestSignerRefusesAuthorityEnd"),
        "an EndLease with authority_end is never signed (the broadcaster holds no AUTHORITY key)"),
    one("w1rs-signer-check-signable-not-run", SIGNING, SI + "/signer.go",
        "\tif err := policy.CheckSignable(k.class, k.address, msg); err != nil {",
        "\tif err := error(nil); err != nil {",
        K(SI, "TestSignerRefusesAuthorityEnd", "TestSignerRefusesMalformedRequests"),
        "the signer re-checks the decoded message against its key's allowlist"),
    one("w1fix-client-check-signable-dropped", SIGNING, SI + "/client.go",
        "\tif err := policy.CheckSignable(info.Class, info.Address, req.Msg); err != nil {\n\t\treturn Signed{}, err\n\t}\n\tm, ok",
        "\tif err := error(nil); err != nil {\n\t\treturn Signed{}, err\n\t}\n\tm, ok",
        K(SI, "TestClientChecksBeforeAsking"),
        "the serve-side client checks the allowlist before it asks the signer (review S5/R1)"),
    one("w1b6-unknown-fields-accepted", SIGNING, SI + "/signer.go",
        "\tif hasUnknown(m.ProtoReflect()) {",
        "\tif false {",
        K(SI, "TestSignerRefusesMalformedRequests"),
        "a message with unknown fields is refused"),
    one("w1rs-non-deterministic-encoding-accepted", SIGNING, SI + "/signer.go",
        "\tif err != nil || !bytes.Equal(canonical, value) {",
        "\tif err != nil || canonical == nil {",
        K(SI, "TestSignerRefusesMalformedRequests"),
        "a message whose bytes are not its deterministic encoding is refused"),
    one("w1rs-signer-gas-bounds-dropped", SIGNING, SI + "/signer.go",
        "\tif req.Gas < genesis.MinGasPerTx || req.Gas > genesis.MaxGasPerTx {",
        "\tif false {",
        K(SI, "TestSignerRefusesMalformedRequests"),
        "the signer refuses gas outside the chain's per-transaction bounds"),
    # Chain id and deadline checks.
    one("w1b6-chain-id-unchecked-in-signer", SIGNING, SI + "/signer.go",
        "\tif req.ChainID != s.cfg.ChainID {",
        "\tif false {",
        K(SI, "TestSignerRefusesForeignChainID"),
        "the signer refuses a request for another chain id"),
    one("w1b6-non-dev-chain-id-allowed", SIGNING, SI + "/signer.go",
        "\tcase !strings.HasPrefix(c.ChainID, genesis.DevnetChainIDPrefix):",
        "\tcase !strings.HasPrefix(c.ChainID, genesis.DevnetChainIDPrefix) && false:",
        K(SI, "TestSignerStartupRefusals"),
        "the development signer starts only for a devnet chain id"),
    one("w1b6-timeout-bound-dropped", SIGNING, SI + "/signer.go",
        "\tif req.TimeoutHeight > uint64(height)+s.cfg.MaxTimeoutOffset {",
        "\tif req.TimeoutHeight > uint64(height)+s.cfg.MaxTimeoutOffset && false {",
        K(SI, "TestSignerRefusesUnboundedTimeout"),
        "timeout_height is at most the signer's fresh height plus K_max"),
    one("w1b6-timeout-zero-allowed", SIGNING, SI + "/signer.go",
        "\tif req.TimeoutHeight == 0 {",
        "\tif false {",
        K(SI, "TestSignerRefusesUnboundedTimeout"),
        "timeout_height 0 (no deadline) is refused"),
    one("w1b6-stale-height-accepted", SIGNING, SI + "/height.go",
        "\tif h.height < 1 || h.now().Sub(h.readAt) > h.freshness {",
        "\tif h.height < 1 {",
        K(SI, "TestSignerRefusesUnboundedTimeout", "TestHeightSourceFreshness"),
        "the signer's timeout bound uses a fresh height only"),
    # Intent-key registry (Q3).
    one("w1b6-intent-registry-check-dropped", SIGNING, SI + "/signer.go",
        "\t\tif err := policy.CheckIntentRegistered(msg, k.address, s.registry); err != nil {",
        "\t\tif err := error(nil); err != nil {",
        K(SI, "TestSignerRefusesUnregisteredIntentKey"),
        "the signer signs a register or withdraw only with an intent key in the operator's registry"),
    one("w1b6-registry-membership-dropped", SIGNING, PO + "/intent.go",
        "\tif !reg.Contains(pub) {",
        "\tif !reg.Contains(pub) && false {",
        K(SI, "TestSignerRefusesUnregisteredIntentKey"),
        "an intent key not in the registry is refused"),
    one("w1rs-withdraw-intent-registry-dropped", SIGNING, SI + "/signer.go",
        "\tcase *contentv1.MsgRegisterContent, *contentv1.MsgWithdrawContent:\n\t\tif err := policy.CheckIntentRegistered",
        "\tcase *contentv1.MsgRegisterContent:\n\t\tif err := policy.CheckIntentRegistered",
        K(SI, "TestSignerRefusesUnregisteredIntentKey", "TestSignerVerifiesIntentSignature"),
        "a withdraw's hosting intent is checked against the registry too"),
    one("w1b6-owner-may-write-registry", SIGNING, PO + "/registry.go",
        "\tif uid == 0 || st.Uid == uid || perm&0o002 != 0 {",
        "\tif uid == 0 || perm&0o002 != 0 {",
        K(PO, "TestIntentKeyRegistryRefusesWritableFile"),
        "a registry the reading process could write is refused"),
    one("w1rs-registry-same-uid-write-bit-allowed", SIGNING, PO + "/registry.go",
        "\t\tif file.Mode().Perm()&0o222 != 0 {",
        "\t\tif file.Mode().Perm()&0o222 == 0o777 {",
        K(PO, "TestIntentKeyRegistryRefusesWritableFile"),
        "with one uid, a registry file with any write bit is refused"),
    one("w1fix-registry-owned-by-serve-accepted", SIGNING, PO + "/registry.go",
        "\t\tcase st.Uid == serveUID:",
        "\t\tcase st.Uid == serveUID && false:",
        K(PO, "TestRegistryForServe"),
        "under distinct uids the signer refuses a registry owned by the serve uid (Wave 1 fix F4)"),
    one("w1fix-registry-group-write-accepted", SIGNING, PO + "/registry.go",
        "\t\tcase info.Mode().Perm()&0o022 != 0:\n\t\t\treturn fmt.Errorf(\"%w: %s mode %04o has a group or other write bit\", ErrRegistryServeWritable",
        "\t\tcase false:\n\t\t\treturn fmt.Errorf(\"%w: %s mode %04o has a group or other write bit\", ErrRegistryServeWritable",
        K(PO, "TestRegistryForServe"),
        "under distinct uids the signer refuses a registry with a group or other write bit"),
    # Provider pre-check (provider signatures are not service signatures).
    one("w1b6-verify-against-message-key", SIGNING, PO + "/relay.go",
        "msgkit.VerifyInner(sig, domain, rec.GetSigningKeyId(), rec.GetSigningPubkey(), canonical)",
        "msgkit.VerifyInner(sig, domain, sig.GetKeyId(), sig.GetPublicKey(), canonical)",
        K(PO, "TestProviderPrecheck"),
        "a relayed provider signature is verified against the provider's registered current key, never the message's key"),
    one("w1b6-offer-expiry-unchecked", SIGNING, PO + "/relay.go",
        "\tif in.ChainTime.IsZero() || in.ChainTime.Unix() >= offer.GetExpiryUnixSeconds() {",
        "\tif false {",
        K(PO, "TestProviderPrecheck"),
        "an acceptance of an expired offer is refused before signing"),
    one("w1b6-page-budget-ignored", SIGNING, PO + "/relay.go",
        "\tfor page := 0; page < pages; page++ {",
        "\tfor page := 0; page < 1000; page++ {",
        K(PO, "TestProviderPrecheck"),
        "the held-lease lookup reads a bounded number of pages"),
    one("w1rs-relay-provider-content-must-match-offer", SIGNING, PO + "/relay.go",
        "if (in.ProviderID != \"\" && in.ProviderID != offer.GetProviderId()) || (in.ContentID != \"\" && in.ContentID != offer.GetContentId()) {",
        "if false {",
        K(PO, "TestProviderPrecheck"),
        "the named provider and content must match the offer"),
    one("w1rs-relay-provider-admitted-dropped", SIGNING, PO + "/relay.go",
        "if requireAdmitted && rec.GetState() != providersv1.ProviderState_PROVIDER_STATE_ADMITTED {",
        "if requireAdmitted && rec.GetState() == providersv1.ProviderState(-1) {",
        K(PO, "TestProviderPrecheck"),
        "an acceptance needs an ADMITTED provider"),
    one("w1rs-relay-held-lease-id-unchecked", SIGNING, PO + "/relay.go",
        "if !found || lease.GetLeaseId() != leaseID {",
        "if !found {",
        K(PO, "TestProviderPrecheck"),
        "a renew or end names the held lease's id"),
    # Forward rules (Q2 P3).
    one("w1fix-forward-provider-role-dropped", SIGNING, PO + "/forward.go",
        "if role := in.Roles.RoleOf(signer); role != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER {",
        "if role := in.Roles.RoleOf(signer); role == typesv1.SubmitterRole(-1) {",
        K(PO, "TestForwardedTxChecks"),
        "a forwarded transaction's signer has the ADMITTED_PROVIDER role (review P1)"),
    one("w1rs-forward-provider-account-unchecked", SIGNING, PO + "/forward.go",
        "if !found || !bytes.Equal(rec.GetAccount(), signer) {",
        "if !found || rec == nil {",
        K(PO, "TestForwardedTxChecks"),
        "a forwarded transaction is signed by the named provider's own account"),
    one("w1rs-forward-used-sequence-unchecked", SIGNING, PO + "/forward.go",
        "if own.Signer.Sequence < account.Sequence {",
        "if false {",
        K(PO, "TestForwardedTxChecks"),
        "a forwarded transaction at an already used sequence is refused"),
    one("w1fix-forward-outer-pubkey-address-unchecked", SIGNING, PO + "/forward.go",
        "if sig.PubKey == nil || !bytes.Equal(sig.PubKey.Address(), signer) || sig.Sequence != sequence {",
        "if sig.PubKey == nil || sig.Sequence != sequence {",
        K(PO, "TestForwardedTxChecks"),
        "the outer public key's address is the signer (review P4)"),
    one("w1b6-forward-window-unchecked", SIGNING, PO + "/forward.go",
        "\tif decoded.TimeoutHeight <= uint64(in.ObservedHeight) || decoded.TimeoutHeight > uint64(in.ObservedHeight)+in.Window {",
        "\tif false {",
        K(PO, "TestForwardedTxChecks"),
        "a forwarded transaction's timeout_height is inside the window"),
    one("w1b6-forward-outer-signature-unchecked", SIGNING, PO + "/forward.go",
        "\tif !sig.PubKey.VerifySignature(signBytes, data.Signature) {",
        "\tif !sig.PubKey.VerifySignature(signBytes, data.Signature) && false {",
        K(PO, "TestForwardedTxChecks"),
        "a forwarded transaction's outer signature verifies for this chain id"),
    # Object permissions in policy (Q3).
    one("w1b6-registered-key-check-skipped", SIGNING, PO + "/permissions.go",
        "\tif !bytes.Equal(pub, registered) {",
        "\tif !bytes.Equal(pub, registered) && false {",
        K(PO, "TestPublicationPermission"),
        "a hosting intent must be signed by the principal's registered intent key"),
    one("w1b6-withdraw-owner-principal-skipped", SIGNING, PO + "/permissions.go",
        "in.Owner.PrincipalID != in.Auth.PrincipalID ||",
        "false ||",
        K(PO, "TestPublicationPermission"),
        "only the owner principal may withdraw its content"),
    one("w1rs-register-on-chain-record-unchecked", SIGNING, PO + "/permissions.go",
        "if in.OnChain != nil && (in.OnChain.GetContent() != nil || in.OnChain.GetTombstone() != nil) {",
        "if false {",
        K(PO, "TestPublicationPermission"),
        "a register of a content id that already has a record or tombstone is refused"),
    one("w1rs-withdraw-on-chain-host-unchecked", SIGNING, PO + "/permissions.go",
        "in.Owner.PrincipalID != in.Auth.PrincipalID || !hostedBy(in.OnChain, in.Service) {",
        "in.Owner.PrincipalID != in.Auth.PrincipalID {",
        K(PO, "TestPublicationPermission"),
        "a withdraw needs our service as the content's on-chain host"),
    one("w1b6-pending-owner-allows-offer", SIGNING, PO + "/permissions.go",
        "\treturn owner != nil && owner.Status == OwnerEffective && owner.ContentID == contentID &&",
        "\treturn owner != nil && owner.Status != OwnerVoid && owner.ContentID == contentID &&",
        K(PO, "TestOfferAndOwnership"),
        "an offer needs an EFFECTIVE owner row; PENDING is not enough"),
    one("w1rs-offer-on-chain-host-unchecked", SIGNING, PO + "/permissions.go",
        "\tif !ownsEffective(in.Owner, offer.GetContentId(), offer.GetContentVersion(), in.Caller.Principal, in.Service) ||\n\t\t!hostedBy(in.OnChain, in.Service) {",
        "\tif !ownsEffective(in.Owner, offer.GetContentId(), offer.GetContentVersion(), in.Caller.Principal, in.Service) {",
        K(PO, "TestOfferAndOwnership"),
        "an offer needs our service as the content's on-chain host"),
    one("w1rs-owns-effective-caller-unchecked", SIGNING, PO + "/permissions.go",
        "owner.ContentVersion == version && owner.CallerPrincipal == caller && bytes.Equal(owner.SignerAddress, service)",
        "owner.ContentVersion == version && len(caller) >= 0 && bytes.Equal(owner.SignerAddress, service)",
        K(PO, "TestOfferAndOwnership", "TestPublicationPermission"),
        "ownership is per caller: another caller's EFFECTIVE content is not ours"),
    one("w1rs-relay-any-bound-caller", SIGNING, PO + "/permissions.go",
        "if a.Caller.BoundProviderID != \"\" && a.Caller.BoundProviderID == found.ProviderID {",
        "if a.Caller.BoundProviderID != \"\" {",
        K(PO, "TestRelayAuthority"),
        "a bound caller relays only its own provider's signatures"),
    one("w1rs-grant-not-required", SIGNING, PO + "/permissions.go",
        "\tif !ctx.Grants(spec.Permission) {",
        "\tif !ctx.Grants(spec.Permission) && false {",
        K(PO, "TestOfferAndOwnership", "TestPublicationPermission", "TestRelayAuthority"),
        "each kind needs its permission in the authorized request context"),
    one("w1rs-any-routed-kind-allowed", SIGNING, PO + "/permissions.go",
        "\tfor _, k := range c.Kinds {\n\t\tif k == kind {",
        "\tfor _, k := range append(c.Kinds, kind) {\n\t\tif k == kind {",
        K(PO, "TestOfferAndOwnership", "TestPublicationPermission", "TestRelayAuthority"),
        "a caller may submit only the kinds configured for it"),
    # Start-up refusals: no seat, authority, auditor or provider key.
    one("w1b6-seat-key-accepted", SIGNING, PO + "/governance.go",
        "\t\tif seat, isSeat := view.SeatOf(k.Address); isSeat {",
        "\t\tif seat, isSeat := view.SeatOf(k.Address); isSeat && false {",
        K(PO, "TestStartupRefusesRole"),
        "a council seat key is never loaded (DR-06-06)"),
    one("w1b6-any-role-accepted", SIGNING, PO + "/governance.go",
        "\t\tcase got != want:",
        "\t\tcase false:",
        K(PO, "TestStartupRefusesRole"),
        "each key's on-chain role must be its class's role (ADMITTED_SERVICE or BROADCASTER)"),
    # Peer uid, key files, socket directory.
    one("w1b6-any-peer-uid", SIGNING, SI + "/signer.go",
        "\tif err != nil || uid != s.peerUID.Load() {",
        "\tif err != nil {",
        K(SI, "TestSignerPeerUID") + K(SI, "TestSignerRefusesOtherUIDPeer", tags=ROOT_TAG),
        "the signer serves only the configured serve uid (SO_PEERCRED)"),
    one("w1rs-serve-uid-equals-signer-uid", SIGNING, SI + "/signer.go",
        "\t\tif c.ServeUID == 0 || c.ServeUID == euid {",
        "\t\tif c.ServeUID == 0 {",
        K(SI, "TestSignerStartupRefusals"),
        "under distinct uids the serve uid differs from the signer's"),
    one("w1b6-distinct-uids-as-root", SIGNING, SI + "/signer.go",
        "\t\tif euid == 0 {\n\t\t\treturn errors.New(\"signer: distinct uids are configured and the signer runs as root\")",
        "\t\tif false {\n\t\t\treturn errors.New(\"signer: distinct uids are configured and the signer runs as root\")",
        K(SI, "TestSignerStartupRefusals"),
        "under distinct uids the signer does not run as root"),
    one("w1rs-socket-dir-group-write", SIGNING, SI + "/signer.go",
        "\tcase info.Mode().Perm()&0o022 != 0:\n\t\treturn fmt.Errorf(\"signer: socket directory mode",
        "\tcase false:\n\t\treturn fmt.Errorf(\"signer: socket directory mode",
        K(SI, "TestSignerStartupRefusals"),
        "the signer socket directory has no group or other write bit"),
    one("w1b6-client-accepts-another-hash", SIGNING, SI + "/client.go",
        "\tif !bytes.Equal(raw, resp.TxBytes) || !bytes.Equal(hash[:], resp.TxHash) {",
        "\tif !bytes.Equal(raw, resp.TxBytes) {",
        K(SI, "TestClientRejectsAlteredTransaction"),
        "the client refuses a signed answer whose hash is not the bytes' hash"),
    one("w1b6-key-file-mode-unchecked", SIGNING, KE + "/keys.go",
        "\tcase info.Mode().Perm()&0o077 != 0:\n\t\treturn fmt.Errorf(\"%w: key file mode",
        "\tcase false:\n\t\treturn fmt.Errorf(\"%w: key file mode",
        K(KE, "TestKeyFileRules"),
        "a key file readable by group or other is refused"),
    one("w1b6-key-owner-unchecked", SIGNING, KE + "/keys.go",
        "\tif !ok || uid != owner {\n\t\treturn fmt.Errorf(\"%w: key file owner",
        "\tif !ok {\n\t\treturn fmt.Errorf(\"%w: key file owner",
        K(KE, "TestLoadRefusesOtherOwner", tags=ROOT_TAG),
        "a key file owned by another uid is refused (only the root test chowns a file to another uid; without root "
        "the directory owner check refuses first)"),
    one("w1rs-key-directory-checks-dropped", SIGNING, KE + "/keys.go",
        "\tif err := checkDir(filepath.Dir(path), owner); err != nil {",
        "\tif err := checkDir(filepath.Dir(path), owner); err != nil && false {",
        K(KE, "TestKeyFileRules") + K(KE, "TestLoadRefusesOtherOwner", tags=ROOT_TAG),
        "a key directory owned by another uid or writable by others is refused"),
    # Only the signer signs; no local preimage copy.
    Mutant("b10-signing-outside-the-signer", SIGNING,
           (Edit(WK + "/housekeeping.go", "import (\n\t\"context\"\n\t\"errors\"\n",
                 "import (\n\t\"context\"\n\t\"crypto/ed25519\"\n\t\"errors\"\n"),
            Edit(WK + "/housekeeping.go", "// housekeepingPass runs, each over at most one batch",
                 "var _ = ed25519.Sign\n\n// housekeepingPass runs, each over at most one batch")),
           K(PO, "TestNoSigningOutsideSigner"),
           "no signing function is used outside internal/signer (plan 13.6)"),
    one("b10-preimage-literal-outside-chain", SIGNING, WK + "/housekeeping.go",
        "// housekeepingPass runs, each over at most one batch",
        "var _ = \"SVN-HOSTING-INTENT-v1\"\n\n// housekeepingPass runs, each over at most one batch",
        K(PO, "TestNoLocalPreimageCopy"),
        "the module holds no copy of the chain's signing preimage labels (plan 13.6)"),

    # ---------------------------------------------------------------- api (unit)
    one("b11-replay-after-authorize-dropped", API, AP + "/handlers.go",
        "\t\tif status, v, replayErr, done := s.replay(ctx, caller.Principal, env.IdempotencyKey, digest); done {\n"
        "\t\t\treturn status, v, replayErr\n\t\t}\n",
        "",
        K(AP, "TestDuplicateCreateAfterMissedReplay"),
        "phase DU's defect: a duplicate create whose replay lookup ran before the first create committed answers "
        "the existing intent, not the content id refusal its PENDING owner row causes"),
    # mTLS principal binding (plan 5.6).
    one("w1b7a-two-uri-sans-accepted", API, AP + "/principal.go",
        "if len(leaf.URIs) != 1 {",
        "if len(leaf.URIs) < 1 {",
        K(AP, "TestClientCertRequired"),
        "a client certificate carries exactly one URI SAN, the principal"),
    one("w1b7a-client-cert-optional", API, AP + "/server.go",
        "ClientAuth:   tls.RequireAndVerifyClientCert,",
        "ClientAuth:   tls.VerifyClientCertIfGiven,",
        K(AP, "TestClientCertRequired", "TestLoopbackOnly"),
        "the API requires a verified client certificate"),
    one("w1rs-client-cert-not-verified", API, AP + "/server.go",
        "\t\tClientAuth:   tls.RequireAndVerifyClientCert,",
        "\t\tClientAuth:   tls.RequireAnyClientCert,",
        K(AP, "TestClientCertRequired", "TestCreateRegisterContent", "TestCrossCallerNotFound"),
        "the client certificate must chain to the development CA"),
    one("w1rs-tls12-allowed", API, AP + "/server.go",
        "\t\tMinVersion:   tls.VersionTLS13,",
        "\t\tMinVersion:   tls.VersionTLS12,",
        K(AP, "TestClientCertRequired", "TestLoopbackOnly"),
        "the API speaks TLS 1.3 only"),
    one("w1b7a-serve-on-any-address", API, AP + "/server.go",
        "if !ok || !ip.Unmap().IsLoopback() {",
        "if !ok || !ip.Unmap().IsLoopback() && false {",
        K(AP, "TestLoopbackOnly"),
        "the API listens on a loopback address only"),
    one("w1rs-unknown-caller-let-through", API, AP + "/handlers.go",
        "\t\tcaller, ok := s.callers[principal]\n\t\tif !ok {",
        "\t\tcaller, ok := s.callers[principal]\n\t\tif !ok && false {\n\t\t\t_ = ok",
        K(AP, "TestClientCertRequired", "TestKindAndPermissionChecks"),
        "a verified principal that is not a configured caller is refused"),
    # Object permissions and uniform answers at the API.
    one("w1b7a-grant-check-dropped", API, AP + "/handlers.go",
        "\tif err := policy.CheckGrant(spec.Kind, auth); err != nil {\n\t\treturn 0, intentView{}, policyError(err)\n\t}",
        "",
        K(AP, "TestKindAndPermissionChecks"),
        "the API checks the kind's permission before any chain read"),
    one("w1b7a-cancel-answers-another-caller", API, AP + "/handlers.go",
        "\tif err := s.deps.Store.Cancel(r.Context(), caller, id); err != nil {",
        "\tif err := s.deps.Store.Cancel(r.Context(), \"app-fixture\", id); err != nil {",
        K(AP, "TestCrossCallerNotFound"),
        "a caller cancels only its own intents; another caller's intent is 404"),
    one("w1rs-relay-authority-unchecked", API, AP + "/authorize.go",
        "\tif err := s.relayAuthority(caller).CheckRelay(ctx, dec.Kind, auth, found); err != nil {",
        "\tif err := s.relayAuthority(caller).CheckRelay(ctx, dec.Kind, auth, found); err != nil && false {",
        K(AP, "TestRelayPrecheckAndAuthority"),
        "a relay needs a bound caller or the caller's own committed offer"),
    one("w1fix-forward-relay-authority-unchecked", API, AP + "/authorize.go",
        "\tif err := s.relayAuthority(caller).CheckRelay(ctx, dec.Kind, auth, fwd.Relay); err != nil {",
        "\tif err := error(nil); err != nil {",
        K(AP, "TestForwardProviderTx"),
        "a forward needs the same relay authority (review A4, fix F1)"),
    one("w1b7a-relay-expiry-not-applied", API, AP + "/handlers.go",
        "&& d > s.cfg.RelayLocalExpiry {",
        "&& d > 1000*s.cfg.RelayLocalExpiry {",
        K(AP, "TestRelayPrecheckAndAuthority"),
        "a relayed renew or end has a bounded local expiry"),
    one("w1b7a-forward-gas-not-signed-one", API, AP + "/authorize.go",
        "Gas: decoded.Gas,",
        "Gas: decoded.Gas + 1,",
        K(AP, "TestForwardProviderTx"),
        "a forward stores the provider's signed bytes as they are"),
    one("w1b7a-forward-pinned-to-broadcaster", API, AP + "/authorize.go",
        "\t\tsigner: fwd.Signer,\n",
        "\t\tsigner: s.signers[policy.ClassBroadcaster].Address,\n",
        K(AP, "TestForwardProviderTx"),
        "a forward's signer is the provider's own account, never our key"),
    one("w1b7a-relay-signed-by-service-key", API, AP + "/authorize.go",
        "\trelayer := s.signers[policy.ClassBroadcaster].Address\n",
        "\trelayer := s.signers[policy.ClassService].Address\n",
        K(AP, "TestRelayPrecheckAndAuthority"),
        "a relay is signed by the BROADCASTER key, never the service key"),
    # Idempotency and the request digest.
    one("w1rs-api-replay-ignores-sql-entry", API, AP + "/handlers.go",
        "\tif it.Entry != \"HTTP\" || !bytes.Equal(it.RequestDigest, digest[:]) {",
        "\tif !bytes.Equal(it.RequestDigest, digest[:]) {",
        K(AP, "TestIdempotencyConflict"),
        "an HTTP create never answers from an SQL-path intent with the same key (its validated digest has the HTTP form)"),
    one("w1b7a-idempotency-precheck-off", API, AP + "/handlers.go",
        "\tif errors.Is(err, store.ErrNotFound) {\n\t\treturn 0, intentView{}, nil, false\n\t}",
        "\tif err != nil || true {\n\t\treturn 0, intentView{}, nil, false\n\t}",
        K(AP, "TestIdempotencyConflict"),
        "a repeated key answers from the stored intent; another body is 409"),
    Mutant("w1b7a-context-not-canonical", API,
           (Edit(RQ + "/envelope.go", "\tcanonical := a.Canonical()\n", "\tcanonical := a\n"),
            Edit(RQ + "/envelope.go", "json.Marshal(a.Canonical())", "json.Marshal(a)")),
           K(RQ, "TestAuthContextRefusesPersonalData", "TestRequestDigestCanonical") + K(AP, "TestRequestDigestCanonical"),
           "the authorized context is canonicalised (fields in order, permissions sorted) before it is stored and digested"),
    one("w1b7a-digest-omits-context", API, RQ + "/digest.go",
        "[][]byte{[]byte(digestLabel), []byte(kind), stored, authJSON}",
        "[][]byte{[]byte(digestLabel), []byte(kind), stored, authJSON[:0]}",
        K(RQ, "TestRequestDigestCanonical") + K(AP, "TestIdempotencyConflict"),
        "the request digest covers the authorized context"),
    one("w1b7a-ip-id-accepted", API, RQ + "/envelope.go",
        "if _, err := netip.ParseAddr(id); err == nil || dottedQuadRE.MatchString(id) {",
        "if _, err := netip.ParseAddr(id); false && (err == nil || dottedQuadRE.MatchString(id)) {",
        K(RQ, "TestAuthContextRefusesPersonalData"),
        "an IP address is personal data and is refused as an id"),
    one("w1b7a-renew-term-version-1-accepted", API, RQ + "/kinds.go",
        "uintField(\"request.term_version\", r.TermVersion, 2, safeMax)",
        "uintField(\"request.term_version\", r.TermVersion, 1, safeMax)",
        K(RQ, "TestRequestRefusals"),
        "a renew names a term version of at least 2"),
    # Canonical commitment evidence in the status view.
    one("w1b7a-evidence-check-skipped", API, AP + "/view.go",
        "\tif err := checkEvidence(it, chainID); err != nil {\n\t\treturn intentView{}, err\n\t}",
        "\t_ = checkEvidence",
        K(AP, "TestNeverCommittedWithoutEvidence"),
        "the API shows COMMITTED only with complete commitment evidence"),
    one("w1b7a-committed-without-code-0", API, AP + "/view.go",
        "c.Code != 0, ",
        "",
        K(AP, "TestNeverCommittedWithoutEvidence"),
        "COMMITTED evidence carries code 0"),
    one("w1fix-committed-short-tx-hash", API, AP + "/view.go",
        "len(c.TxHash) != 32, ",
        "",
        K(AP, "TestNeverCommittedWithoutEvidence"),
        "COMMITTED evidence carries a 32-byte transaction hash (review R8)"),
    one("w1b7a-status-filter-ignores-stall", API, AP + "/handlers.go",
        "\tstall := o.StallOpen && o.ObserverFresh\n",
        "\tstall := false\n",
        K(AP, "TestListPagingAndFilters"),
        "list filters show PENDING_STALL while a stall episode is open"),
    one("w1b7a-stale-observer-shown-fresh", API, AP + "/handlers.go",
        "\t\to.ObserverFresh = h.Age >= 0 && h.Age <= s.cfg.ObserverFreshness\n",
        "\t\to.ObserverFresh = h.Age >= 0 || true\n",
        K(AP, "TestStatusViews"),
        "a stale observer is shown as OBSERVER_UNAVAILABLE"),

    # ---------------------------------------------------------------- config (unit; config, strictjson, secretfile, logx)
    # Redaction.
    one("w1b6-composite-values-logged", CONFIG, LX + "/logx.go",
        "\treturn slog.String(a.Key, Redacted)\n}\n\n// jsonLike",
        "\treturn a\n}\n\n// jsonLike",
        K(LX, "TestRedactorDropsRequestBody"),
        "composite log values (request bodies) are redacted"),
    one("w1b6-json-like-strings-logged", CONFIG, LX + "/logx.go",
        "\t\tif len(v.String()) > MaxStringBytes || jsonLike(v.String()) {",
        "\t\tif len(v.String()) > MaxStringBytes {",
        K(LX, "TestRedactorDropsRequestBody"),
        "JSON-like string values are redacted"),
    # PostgreSQL environment refusal and config rules.
    one("w1b7a-pqgo-debug-allowed", CONFIG, CF + "/env.go",
        "|| name == \"PQGO_DEBUG\"",
        "",
        K(CF, "TestStartRefusesPGEnv"),
        "PQGO_DEBUG (which logs every query) is refused at start"),
    one("w1b7a-env-value-echoed", CONFIG, CF + "/env.go",
        "\t\tname, _, _ := strings.Cut(kv, \"=\")\n",
        "\t\tname, _, _ := strings.Cut(kv, \"=\")\n\t\tif strings.HasPrefix(name, \"PG\") {\n\t\t\tname = kv\n\t\t}\n",
        K(CF, "TestStartRefusesPGEnv"),
        "the refusal names the variable, never its value"),
    one("w1b7a-db-chain-mismatch-ignored", CONFIG, CF + "/convert.go",
        "\tif err := checkDB(ctx, c.ChainID); err != nil {",
        "\tif err := checkDB(ctx, c.ChainID); err != nil && false {",
        K(CF, "TestRefusesChainIDMismatch"),
        "serve refuses a database bound to another chain id"),
    one("w1b7a-callers-on-any-chain", CONFIG, CF + "/config.go",
        "\tif !c.devChain() {\n\t\treturn fmt.Errorf(\"config: development principals",
        "\tif false {\n\t\treturn fmt.Errorf(\"config: development principals",
        K(CF, "TestDevPrincipalsRefuseNonDevChain"),
        "development principals are refused off a devnet chain id"),
    one("w1b7a-number-bound-off-by-one", CONFIG, CF + "/numbers.go",
        "if *f.v < b.lo || *f.v > b.hi {\n\t\t\treturn fmt.Errorf(\"config: number %s outside its hard bounds [%d, %d]\", f.name, b.lo, b.hi)\n\t\t}\n\t}\n\treturn nil",
        "if *f.v < b.lo-1 || *f.v > b.hi {\n\t\t\treturn fmt.Errorf(\"config: number %s outside its hard bounds [%d, %d]\", f.name, b.lo, b.hi)\n\t\t}\n\t}\n\treturn nil",
        K(CF, "TestNumbersHaveHardBounds"),
        "every PROPOSED number has hard bounds"),
    one("w1b7a-caps-sum-unchecked", CONFIG, CF + "/config.go",
        "\tif sum > c.Numbers.CallerCapsSumMax {",
        "\tif sum > 1<<40 {",
        K(CF, "TestCapsSumBounded"),
        "the sum of per-caller caps is bounded"),
    one("w1b7b-runtime-database-not-required", CONFIG, CF + "/config.go",
        "\treturn c.RequireRuntimeDatabase()\n}",
        "\treturn nil\n}",
        K(CF, "TestDatabaseRoleRequirements"),
        "serve connects as the runtime role only"),
    one("w1b7b-caller-may-use-runtime-role", CONFIG, CF + "/config.go",
        "case cl.SQLRole == store.OwnerRole || cl.SQLRole == store.RuntimeRole:",
        "case cl.SQLRole == store.OwnerRole:",
        K(CF, "TestCallerSQLRole"),
        "a caller's SQL role is neither the owner nor the runtime role"),
    one("w1b7b-timeout-offset-from-signer-bound", CONFIG, CF + "/convert.go",
        "TimeoutOffset:         uint64(n.TimeoutOffsetBlocks),",
        "TimeoutOffset:         uint64(n.SignerMaxTimeoutOffsetBlocks),",
        K(CF, "TestWorkerNumbers"),
        "the worker's timeout offset is its own number, below the signer's bound"),
    one("w1b7a-strict-decode-allows-unknown", CONFIG, SJ + "/strictjson.go",
        "\tdec.DisallowUnknownFields()\n",
        "",
        K(SJ, "TestDecode") + K(CF, "TestUnknownFieldsRefused") + K(AP, "TestStrictDecode"),
        "configuration and request JSON with unknown fields is refused"),
    one("w1b7a-duplicate-keys-allowed", CONFIG, SJ + "/strictjson.go",
        "\t\tif seen[key] {\n\t\t\treturn ErrDuplicate\n\t\t}\n",
        "",
        K(SJ, "TestCheck") + K(AP, "TestStrictDecode"),
        "JSON with a duplicate key is refused"),
    one("w1b7a-secret-file-mode-unchecked", CONFIG, SF + "/secretfile.go",
        "if info.Mode().Perm()&0o077 != 0 {",
        "if info.Mode().Perm()&0o000 != 0 {",
        K(SF, "TestRead") + K(CF, "TestConnConfigPasswordFile") + K(AP, "TestNewRefusesBadDeps"),
        "a password or key file readable by group or other is refused"),

    # ---------------------------------------------------------------- binary (unit; cmd/svn-broadcaster without PostgreSQL)
    one("w1b7b-environment-not-checked-first", BINARY, CMD + "/run.go",
        "err := config.CheckEnvironment(env.environ)",
        "err := error(nil)",
        K(CMD, "TestEnvironmentRefusedFirst"),
        "every subcommand refuses PG* and PQGO_DEBUG before it reads anything"),
    one("w1b7b-signer-uid-unchecked", BINARY, CMD + "/signercmd.go",
        "if err := c.CheckProcessUID(true, env.euid); err != nil {",
        "if err := error(nil); err != nil {",
        K(CMD, "TestSectionAndRoleRefusals"),
        "the signer subcommand checks its process uid"),
    one("w1b7b-serve-uid-unchecked", BINARY, CMD + "/serve.go",
        "if err := c.CheckProcessUID(false, env.euid); err != nil {",
        "if err := error(nil); err != nil {",
        K(CMD, "TestSectionAndRoleRefusals"),
        "the serve subcommand checks its process uid"),
    one("w1b7b-serve-stop-error-hidden", BINARY, CMD + "/serve.go",
        "if !stopped && len(errs) == 0 {",
        "if false {",
        K(CMD, "TestServeUntilDone"),
        "serve reports a component that stopped by itself"),
    one("w1b7b-signer-clear-hash-format", BINARY, CMD + "/signerclear.go",
        "if !hashRE.MatchString(*foreign) {",
        "if len(*foreign) != 64 {",
        K(CMD, "TestSignerClearArguments"),
        "signer-clear takes a lowercase hex hash only"),
    one("w1b7b-dev-ca-two-uri-sans", BINARY, CMD + "/devca.go",
        "URIs:        []*url.URL{u},",
        "URIs:        []*url.URL{u, u},",
        K(CMD, "TestDevCAWritesCertificates"),
        "dev-ca issues each caller certificate with one URI SAN"),
    one("w1b7b-dev-db-any-chain-id", BINARY, CMD + "/devdb.go",
        "case !strings.HasPrefix(*chainID, genesis.DevnetChainIDPrefix) || len(*chainID) > genesis.MaxChainIDLen:",
        "case len(*chainID) > genesis.MaxChainIDLen:",
        K(CMD, "TestDevDBArguments"),
        "dev-db creates a database only for a devnet chain id"),
    one("w1b7b-dev-db-writable-dir", BINARY, CMD + "/devdb.go",
        "case info.Mode().Perm()&0o022 != 0:",
        "case false:",
        K(CMD, "TestDevDBArguments"),
        "dev-db refuses an output directory with a group or other write bit"),

    # ---------------------------------------------------------------- store (PostgreSQL)
    one("b11-lease-table-added", STORE, MIG2,
        "CREATE UNIQUE INDEX content_owners_one_live ON broadcaster.content_owners (content_id) WHERE status <> 'VOID';",
        "CREATE UNIQUE INDEX content_owners_one_live ON broadcaster.content_owners (content_id) WHERE status <> 'VOID';\n"
        "CREATE TABLE broadcaster.leases (lease_id text PRIMARY KEY, provider_id text NOT NULL, package_bytes bigint NOT NULL);",
        K(ST, "TestNoLeaseOrProjectionTable", "TestSQLMetacharactersAreData"),
        "Req. 4 no final leases: the outbox schema holds no lease, offer or provider state"),
    # Persist before send, compare-and-set, fencing.
    one("b10-persist-without-intent-state-cas", STORE, ST + "/signer.go",
        "\tcase cur.state != StateRequested || (cur.phase != PhaseQueued && cur.phase != PhaseRetryWait):\n\t\treturn refuse(RefusedNotRequested)\n",
        "",
        K(ST, "TestCancelRacesPersist", "TestPersistAttemptConditions"),
        "an attempt is persisted only while the intent row is still REQUESTED/QUEUED or RETRY_WAIT (compare-and-set)"),
    one("b10-persist-after-expiry", STORE, ST + "/signer.go",
        "\tcase !cur.beforeExpiry:\n\t\treturn refuse(RefusedExpired)\n",
        "",
        K(ST, "TestExpiryRacesPersist"),
        "no attempt is persisted after the intent's expires_at (database clock)"),
    one("w0b1-send-after-expiry", STORE, ST + "/signer.go",
        "cur.phase == PhaseAwaitingDeadline || !cur.beforeExpiry {",
        "cur.phase == PhaseAwaitingDeadline {",
        K(ST, "TestExpiryRacesPersist"),
        "BeginSend refuses a send after expires_at (also review-test R6 and Wave 0 review S3)"),
    one("w0b1-stall-not-checked", STORE, ST + "/signer.go",
        "\tif stalled {\n\t\treturn refuse(RefusedStalled)",
        "\tif false {\n\t\treturn refuse(RefusedStalled)",
        K(ST, "TestPersistAttemptConditions"),
        "the store refuses a new attempt while a stall episode is open"),
    one("w0rt-equal-timeout-at-same-sequence", STORE, ST + "/signer.go",
        "if maxTimeout.Valid && timeout <= maxTimeout.Int64 {",
        "if maxTimeout.Valid && timeout < maxTimeout.Int64 {",
        K(ST, "TestOneLiveAttemptPerSequence", "TestPersistAttemptConditions"),
        "a new attempt at the same sequence needs a timeout_height above every earlier one"),
    one("w0rt-per-signer-window-dropped", STORE, ST + "/signer.go",
        "\tif busy {\n\t\treturn refuse(RefusedSignerBusy)\n\t}\n",
        "",
        K(ST, "TestPersistAttemptConditions"),
        "one live attempt per signer (SIGNER_BUSY)"),
    one("w0rt-one-live-attempt-index-not-unique", STORE, MIG1,
        "CREATE UNIQUE INDEX attempts_one_live_per_sequence ON broadcaster.attempts (signer_address, sequence)",
        "CREATE INDEX attempts_one_live_per_sequence ON broadcaster.attempts (signer_address, sequence)",
        K(ST, "TestOneLiveAttemptPerSequence"),
        "the database allows at most one live attempt per (signer, sequence)"),
    one("w0b1-fence-check-skipped", STORE, ST + "/signer.go",
        "\tif !held {\n\t\treturn nil, rollback(tx, ErrFenceLost)",
        "\tif false {\n\t\treturn nil, rollback(tx, ErrFenceLost)",
        K(ST, "TestAdvisoryLockFencesSecondWorker"),
        "every signer transaction checks that the session still holds its advisory lock"),
    one("b10-advisory-lock-result-ignored", STORE, ST + "/signer.go",
        "\tif !got {\n\t\treturn nil, errors.Join(ErrSignerLockHeld, conn.Close())\n\t}",
        "\tif false && !got {\n\t\treturn nil, errors.Join(ErrSignerLockHeld, conn.Close())\n\t}",
        K(ST, "TestAdvisoryLockFencesSecondWorker"),
        "a second worker does not get a signer session while another holds the lock"),
    # Dead rule (d2) and send counting in the database.
    one("w0b1-d2-check-removed", STORE, MIG1,
        "    CHECK (dead_reason IS DISTINCT FROM 'D2_REFUSED'\n        OR (sends_started >= 1 AND sends_recorded = sends_started AND counted_refusals = sends_recorded))",
        "    CHECK (true)",
        K(ST, "TestDeadRuleD2NeedsCountedRefusals"),
        "the database refuses a (d2) death unless every started send was recorded as a counted refusal"),
    one("w0fix-sdk32-counts", STORE, ST + "/signer.go",
        "return o.Codespace != \"sdk\" || o.Code != 32",
        "return true",
        K(ST, "TestSendCountingAndPhases"),
        "a recorded sdk 32 answer never counts as a refusal (the three (d2) copies agree)"),
    one("w0fix-refusal-flag-not-checked", STORE, ST + "/signer.go",
        "\tif o.CountsAsRefusal != countsAsRefusal(o) {",
        "\tif false && o.CountsAsRefusal != countsAsRefusal(o) {",
        K(ST, "TestSendCountingAndPhases"),
        "RecordSendOutcome refuses a refusal flag that differs from the (d2) rule"),
    one("w0fix-rejection-code-0", STORE, ST + "/signer.go",
        "if o.Code == 0 || !printableRE.MatchString(o.Codespace) {",
        "if !codespaceRE.MatchString(o.Codespace) {",
        K(ST, "TestSendCountingAndPhases"),
        "a mempool rejection carries a non-zero code"),
    # Canonical commitment evidence.
    one("w0b1-evidence-check-silenced", STORE, MIG1,
        "        RAISE EXCEPTION 'SVN_COMMITTED_WITHOUT_EVIDENCE' USING ERRCODE = 'SV008';",
        "        NULL;",
        K(ST, "TestCommitNeedsEvidence"),
        "the database refuses COMMITTED without a commitments row"),
    one("w0b1-canonical-hash-from-replay", STORE, ST + "/settle.go",
        "\t\tintentID, c.FirstCommitTxHash[:], ownTransaction, string(from.State), string(from.Phase))",
        "\t\tintentID, c.TxHash[:], ownTransaction, string(from.State), string(from.Phase))",
        K(ST, "TestCommitNeedsEvidence"),
        "the canonical hash of an idempotent replay is the first commitment's hash"),
    one("w0fix-open-intent-holds-committed-attempt", STORE, MIG1,
        "        RAISE EXCEPTION 'SVN_COMMITTED_ATTEMPT_OF_OPEN_INTENT' USING ERRCODE = 'SV008';",
        "        NULL;",
        K(ST, "TestEvidenceAgreesWithAttempts"),
        "an intent that is not COMMITTED may not hold a committed attempt"),
    one("w0fix-ownership-unchecked", STORE, MIG1,
        "        IF v_phase IS DISTINCT FROM (CASE WHEN v_own THEN 'COMMITTED_OK' END) THEN",
        "        IF false THEN",
        K(ST, "TestEvidenceAgreesWithAttempts"),
        "own_transaction equals 'the canonical hash is this intent's COMMITTED_OK attempt'"),
    one("w0fix-replay-unchecked", STORE, MIG1,
        "        IF NEW.idempotent_replay AND NOT EXISTS (",
        "        IF false AND NOT EXISTS (",
        K(ST, "TestEvidenceAgreesWithAttempts"),
        "an idempotent replay is read from a COMMITTED_IDEMPOTENT attempt"),
    one("w0fix-committed-reason-check-off", STORE, MIG1,
        "    CHECK (state <> 'COMMITTED' OR reason IS NULL),\n",
        "",
        K(ST, "TestEvidenceAgreesWithAttempts"),
        "a COMMITTED intent carries no failure reason"),
    one("w0fix-reason-kept-at-commit", STORE, ST + "/settle.go",
        "phase = NULL, reason = NULL,\n",
        "phase = NULL,\n",
        K(ST, "TestEvidenceAgreesWithAttempts"),
        "CommitIntent clears the reason"),
    one("w0fix-no-hash-binding", STORE, ST + "/settle.go",
        "\tif !bytes.Equal(a.txHash, r.TxHash[:]) {",
        "\tif false && !bytes.Equal(a.txHash, r.TxHash[:]) {",
        K(ST, "TestResolveBindsHashAndObservedHeight"),
        "a committed result resolves only the attempt whose hash it carries"),
    one("w0fix-no-observed-height", STORE, ST + "/settle.go",
        "SET observed_height = GREATEST(observed_height, $2)",
        "SET observed_height = GREATEST(observed_height, 0 * $2)",
        K(ST, "TestResolveBindsHashAndObservedHeight"),
        "resolving a commit raises the signer's observed height"),
    one("w0b1-live-attempt-check-silenced", STORE, MIG1,
        "        RAISE EXCEPTION 'SVN_LIVE_ATTEMPT_MISMATCH' USING ERRCODE = 'SV009';",
        "        NULL;",
        K(ST, "TestLiveAttemptInvariant"),
        "the intent's live attempt pointer agrees with its attempts"),
    # Signer conflict halts.
    one("w0fix-halt-for-cleared-event", STORE, ST + "/signer.go",
        "\tif cleared {\n\t\treturn rollback(tx, ErrConflictCleared)",
        "\tif false && cleared {\n\t\treturn rollback(tx, ErrConflictCleared)",
        K(ST, "TestSignerConflictClearedIsNotHaltedAgain"),
        "a conflict an operator already cleared does not halt the signer again"),
    one("w0fix-halt-accepts-signer-conflict", STORE, ST + "/signer.go",
        "|| reason == haltSignerConflict {",
        "|| reason == \"NEVER\" {",
        K(ST, "TestSignerConflictClearedIsNotHaltedAgain"),
        "a SIGNER_CONFLICT halt needs a recorded foreign use"),
    # Terminal immutability and the Q4 clearing trigger.
    # Re-anchored on migration 0002, whose intents_guard replaces 0001's.
    one("w0b1-terminal-intent-trigger-off", STORE, MIG2,
        "    IF OLD.state IN ('COMMITTED', 'FAILED') THEN\n        IF TG_OP = 'UPDATE' AND OLD.state = 'FAILED'",
        "    IF false THEN\n        IF TG_OP = 'UPDATE' AND OLD.state = 'FAILED'",
        K(ST, "TestTerminalRowsImmutable"),
        "a COMMITTED or FAILED intent row never changes, except the Q4 clearing"),
    one("w1b1b-intents-guard-ignores-other-columns", STORE, MIG2,
        "AND (to_jsonb(NEW) - v_cleared) = (to_jsonb(OLD) - v_cleared)",
        "AND true",
        K(ST, "TestTerminalTriggersAllowOnlyClearing"),
        "Q4: clearing a terminal intent changes only the cleared columns"),
    one("w1b1b-intents-guard-clears-before-attempt-bytes", STORE, MIG2,
        "WHERE a.intent_id = OLD.intent_id AND (a.sends_started > 0 OR a.tx_bytes IS NOT NULL)) THEN",
        "WHERE a.intent_id = OLD.intent_id AND (a.sends_started > 0)) THEN",
        K(ST, "TestTerminalTriggersAllowOnlyClearing"),
        "Q4: an intent is cleared only once no attempt keeps its bytes"),
    one("w1b1b-attempts-guard-clears-non-failed", STORE, MIG2,
        "WHERE i.intent_id = OLD.intent_id AND i.state = 'FAILED' AND i.cleared_at IS NULL)",
        "WHERE i.intent_id = OLD.intent_id)",
        K(ST, "TestTerminalTriggersAllowOnlyClearing"),
        "Q4: only the unsent attempts of a FAILED intent lose their bytes"),
    one("w1b1b-no-sv011-commit-check", STORE, MIG2,
        "RAISE EXCEPTION 'SVN_UNSENT_NOT_CLEARED' USING ERRCODE = 'SV011';",
        "NULL;",
        K(ST, "TestTerminalTriggersAllowOnlyClearing"),
        "Q4: an unsent FAILED intent left uncleared is refused at commit"),
    one("w1b1b-clear-unsent-never-clears", STORE, MIG2,
        "OR EXISTS (SELECT 1 FROM broadcaster.attempts a WHERE a.intent_id = p_intent AND a.sends_started > 0) THEN",
        "OR true THEN",
        K(ST, "TestClearUnsent", "TestMigration0002"),
        "Q4: an unsent intent is cleared when it fails"),
    one("w1rt-request-json-kept-after-clearing", STORE, MIG2,
        "    UPDATE broadcaster.intents i SET request = NULL, request_json = NULL, auth_context = NULL,",
        "    UPDATE broadcaster.intents i SET request = NULL, auth_context = NULL,",
        K(ST, "TestClearUnsent", "TestMigration0002"),
        "Q4: clearing removes the SQL-path request JSON too"),
    one("w1b1b-cleared-replay-compares-gone-context", STORE, MIG2,
        "OR (v_cleared IS NOT NULL AND v_auth_digest IS DISTINCT FROM sha256(convert_to(p_auth_context::text, 'UTF8'))) THEN",
        "OR (v_cleared IS NOT NULL AND v_auth IS DISTINCT FROM p_auth_context) THEN",
        K(ST, "TestClearUnsent"),
        "Q4: a replay of a cleared intent is compared with the kept digest"),
    one("w1b1b-owner-row-not-voided-on-failed", STORE, ST + "/clear.go",
        "status_at = clock_timestamp() WHERE intent_id = $1 AND status = 'PENDING'`",
        "status_at = clock_timestamp() WHERE intent_id = $1 AND false`",
        K(ST, "TestContentOwners", "TestClearUnsent"),
        "a FAILED register voids its PENDING owner row"),
    one("w1b1b-status-digest-from-request", STORE, ST + "/read.go",
        "COALESCE(i.auth_context_digest, sha256(convert_to(i.auth_context::text, 'UTF8'))),",
        "COALESCE(i.auth_context_digest, i.request_digest),",
        K(ST, "TestClearUnsent"),
        "the status view's context digest is the digest of the context"),
    # Object permissions in SQL and ownership.
    one("w1b1b-content-owner-principal-unchecked", STORE, MIG2,
        "AND i.auth_context ->> 'principal_id' = NEW.principal_id) THEN",
        ") THEN",
        K(ST, "TestContentOwners"),
        "an owner row's principal is the registering context's principal"),
    one("w1b1b-content-owner-of-another-caller", STORE, ST + "/owners.go",
        "status <> 'VOID' AND caller_principal = $2`",
        "status <> 'VOID' AND $2::text IS NOT NULL`",
        K(ST, "TestContentOwners"),
        "a caller sees only its own content owner rows (also review ST4)"),
    one("w1b1b-list-accepts-another-callers-token", STORE, ST + "/read.go",
        "WHERE intent_id = $1 AND caller_principal = $2`, f.After",
        "WHERE intent_id = $1 AND $2::text IS NOT NULL`, f.After",
        K(ST, "TestListIntentsPages"),
        "a list page token of another caller is refused"),
    one("w1b1b-scheduled-offer-ignores-committing-signer", STORE, ST + "/owners.go",
        "AND i.state = 'COMMITTED' AND c.committing_signer = i.signer_address)`",
        "AND i.state = 'COMMITTED')`",
        K(ST, "TestCallerScheduledOffer"),
        "a caller's scheduled offer is one our key committed (also review ST2)"),
    one("w1rs-scheduled-offer-of-any-caller", STORE, ST + "/owners.go",
        "AND i.caller_principal = $1 AND i.state = 'COMMITTED' AND c.committing_signer = i.signer_address)`",
        "AND $1::text IS NOT NULL AND i.state = 'COMMITTED' AND c.committing_signer = i.signer_address)`",
        K(ST, "TestCallerScheduledOffer"),
        "a caller's scheduled offer is one of its own intents"),
    one("w1b1b-confirm-ignores-observed-host", STORE, ST + "/owners.go",
        "case !bytes.Equal(observedHost, signer) || !bytes.Equal(committer, signer):",
        "case !bytes.Equal(committer, signer):",
        K(ST, "TestContentOwners"),
        "an owner row becomes EFFECTIVE only when the chain's submitting host is our signer (also review ST1)"),
    one("w1b1b-effective-without-commitment", STORE, MIG2,
        "OR (v_status = 'EFFECTIVE' AND NOT EXISTS (",
        "OR (false AND NOT EXISTS (",
        K(ST, "TestContentOwners"),
        "EFFECTIVE needs the register's commitment"),
    one("w1b1b-content-owners-truncate-allowed", STORE, MIG2,
        "CREATE TRIGGER content_owners_no_truncate BEFORE TRUNCATE ON broadcaster.content_owners\n    FOR EACH STATEMENT EXECUTE FUNCTION broadcaster.refuse_statement();",
        "",
        K(ST, "TestContentOwners"),
        "content owner rows cannot be truncated"),
    one("w1b7b-idempotency-read-of-any-caller", STORE, ST + "/idempotency.go",
        "WHERE i.caller_principal = $1 AND i.idempotency_key = $2",
        "WHERE $1::text <> '' AND i.idempotency_key = $2",
        K(ST, "TestIntentByIdempotencyKey"),
        "an idempotency key is looked up within the caller only (also review ST5)"),
    one("w0rt-http-key-reuses-sql-intent", STORE, ST + "/intent.go",
        "if existingEntry != \"HTTP\" || !bytes.Equal(existingDigest, r.RequestDigest[:]) {",
        "if !bytes.Equal(existingDigest, r.RequestDigest[:]) {",
        K(ST, "TestEnqueueHTTPRefusals"),
        "an HTTP create never answers from an SQL-path intent with the same key (its validated digest has the HTTP form)"),
    # R6 re-anchored on migration 0002, whose enqueue_intent replaces 0001's.
    one("w0fix-r6-replay-ignores-kind", STORE, MIG2,
        "IF v_existing IS DISTINCT FROM v_digest OR v_kind IS DISTINCT FROM p_kind\n",
        "IF v_existing IS DISTINCT FROM v_digest\n",
        K(ST, "TestSQLReplayNeedsSameKindAndContext"),
        "R6: an SQL-path replay repeats the kind"),
    one("w0fix-r6-replay-ignores-context", STORE, MIG2,
        "           OR (v_cleared IS NULL AND v_auth IS DISTINCT FROM p_auth_context)\n",
        "",
        K(ST, "TestSQLReplayNeedsSameKindAndContext"),
        "R6: an SQL-path replay repeats the authorized context"),
    one("w1b1b-sql-path-kind-list-unchecked", STORE, MIG2,
        "IF p_kind IS NULL OR NOT EXISTS (SELECT 1 FROM broadcaster.kinds k WHERE k.kind = p_kind) THEN",
        "IF p_kind IS NULL THEN",
        K(ST, "TestSQLMetacharactersAreData"),
        "the SQL path accepts only the fixed kind list"),
    one("w1b1b-kinds-table-writable", STORE, MIG2,
        "CREATE TRIGGER kinds_fixed BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON broadcaster.kinds\n    FOR EACH STATEMENT EXECUTE FUNCTION broadcaster.refuse_statement();",
        "",
        K(ST, "TestKindsMatchDatabase"),
        "the kinds table is fixed"),
    # Signer class binding and the forwarded transactions (Q2 P3).
    one("w1b1b-signer-class-not-checked", STORE, MIG2,
        "IF v_want IS NOT NULL AND v_have IS NOT NULL AND v_have <> v_want THEN",
        "IF false THEN",
        K(ST, "TestSignerClassBinding", "TestMarkValidatedAnswers"),
        "each kind is signed by its key class's signer"),
    one("w1b1b-forward-attempt-not-checked", STORE, MIG2,
        "IF v_class = 'FORWARD_SOURCE' AND (NEW.n <> 1 OR NOT EXISTS (",
        "IF false AND (NEW.n <> 1 OR NOT EXISTS (",
        K(ST, "TestForwardedTransactions"),
        "a forward source account has exactly one attempt, the stored provider bytes"),
    one("w1b1b-forward-may-retry", STORE, ST + "/settle.go",
        "\tif cur.class == ClassForwardSource {\n\t\treturn errForwardOnce\n\t}\n\tif err := t.ss.s.recordTransition(ctx, t.tx, cur, transition{\n\t\tintentID: intentID, from: &from, to: Expect{StateRequested, PhaseRetryWait}",
        "\tif err := t.ss.s.recordTransition(ctx, t.tx, cur, transition{\n\t\tintentID: intentID, from: &from, to: Expect{StateRequested, PhaseRetryWait}",
        K(ST, "TestForwardedTransactions"),
        "a forwarded intent is never retried with new bytes"),
    one("w1b1b-forwarded-txs-hash-unchecked", STORE, MIG2,
        "AND sha256(i.request) = NEW.tx_hash",
        "",
        K(ST, "TestForwardedTransactions"),
        "a forwarded_txs row binds the stored bytes' hash"),
    one("w1b1b-forwarded-business-ids-unique", STORE, MIG2,
        "(business_id) WHERE source = 'DRAWN';",
        "(business_id);",
        K(ST, "TestForwardedTransactions"),
        "a forwarded transaction's business id is the provider's, not drawn by us"),
    one("w1b1b-lock-signer-locks-forward-source", STORE, ST + "/signer.go",
        "\tif !key.Valid {\n\t\treturn nil, errors.Join(fmt.Errorf(\"%w: a forward source has no signer session\", ErrInvalid), conn.Close())\n\t}\n",
        "",
        K(ST, "TestSignerClassBinding"),
        "a forward source account has no signer session"),
    one("w1b1b-persist-forward-second-attempt", STORE, ST + "/forward.go",
        "\tcase cur.attemptCount != 0:\n\t\treturn refuse(RefusedAttemptCap)\n",
        "",
        K(ST, "TestForwardedTransactions"),
        "PersistForward persists one attempt only"),
    one("w1b1b-forward-source-halted-or-synced", STORE, MIG2,
        "AND next_sequence IS NULL AND halted_reason IS NULL));",
        "));",
        K(ST, "TestSignerClassBinding"),
        "a forward source row has no sequence and no halt"),
    # Caps and bounds.
    one("w0b1-cap-check-off-by-one", STORE, ST + "/intent.go",
        "\tif open >= openCap {",
        "\tif open > openCap {",
        K(ST, "TestPerCallerCapNoGlobalLock", "TestEnqueueRollbackLeavesNothing"),
        "a caller's open intents are capped"),
    one("w0fix-stall-bound-off", STORE, ST + "/observer.go",
        "\tif len(ids) > maxStallIntents {",
        "\tif len(ids) > 2*maxStallIntents {",
        K(ST, "TestCapSumAndStallBound"),
        "a stall episode marks a bounded number of open intents"),
    one("w0fix-cap-sum-off", STORE, MIG1,
        "       + p_cap > 16384 THEN",
        "       + p_cap > 163840 THEN",
        K(ST, "TestCapSumAndStallBound"),
        "register_caller bounds the sum of caps"),
    one("w0fix-insert-errors-unmapped", STORE, ST + "/intent.go",
        "\tif invalidRowStates[SQLState(err)] {",
        "\tif false && invalidRowStates[SQLState(err)] {",
        K(ST, "TestHTTPAuthContextDatabaseChecks"),
        "an HTTP insert refused by a CHECK is ErrInvalid, not an internal error"),

    # ---------------------------------------------------------------- worker (PostgreSQL)
    # Persist before send; sends_started before each send.
    one("w1b8-broadcast-before-persist", WORKER, WK + "/attempt.go",
        "\tfaultpoint.Hit(faultpoint.AfterSignBeforePersist)\n",
        "\tfaultpoint.Hit(faultpoint.AfterSignBeforePersist)\n\tif _, err := s.w.cfg.Chain.Broadcast(ctx, signed.Bytes); err != nil {\n\t\treturn false, err\n\t}\n",
        K(WK, "TestFakeRPCSeesPersistedAttempt"),
        "the attempt row commits before any broadcast of its bytes"),
    one("w1b8-send-without-counted-start", WORKER, WK + "/session.go",
        "\tok, err := s.ss.BeginSend(ctx, attemptID)\n\tif err != nil || !ok {\n\t\treturn err\n\t}",
        "\tok, err := s.ss.BeginSend(ctx, attemptID)\n\t_ = ok\n\tif err != nil {\n\t\treturn err\n\t}",
        K(WK, "TestSendNeedsCountedStart"),
        "a send happens only after sends_started was incremented"),
    one("w1b8-unrecorded-send-not-unknown", WORKER, WK + "/settle.go",
        "\tif att.SendsStarted > att.SendsRecorded {",
        "\tif false && att.SendsStarted > att.SendsRecorded {",
        K(WK, "TestCrashBetweenSendsStartedAndRecord"),
        "after a crash between sends_started and the record, the send is recorded UNKNOWN, so (d2) never applies"),
    one("w1rt-unrecorded-send-counted-refusal", WORKER, WK + "/settle.go",
        "\t\t\tif _, err := s.ss.RecordSendOutcome(ctx, att.ID, store.SendOutcome{Class: store.SendUnknown,",
        "\t\t\tif _, err := s.ss.RecordSendOutcome(ctx, att.ID, store.SendOutcome{Class: store.SendNodeRefused, CountsAsRefusal: true,",
        K(WK, "TestCrashBetweenSendsStartedAndRecord"),
        "an unrecorded started send is never recorded as a counted node refusal"),
    one("w1b8-mempool-rejection-counted", WORKER, WK + "/views.go",
        "\tout := store.SendOutcome{CountsAsRefusal: o.DefinitiveRefusal(), Log: o.Detail}",
        "\tout := store.SendOutcome{CountsAsRefusal: o.DefinitiveRefusal() || o.Outcome == chain.SendMempoolRejected, Log: o.Detail}",
        K(WK, "TestRefusalRuleCopiesAgree"),
        "the worker's refusal flag follows the classifier's (d2) rule"),
    # Reconcile before any resend.
    one("w1fix-no-effect-check-before-retry", WORKER, WK + "/session.go",
        "\tif !eff.decided {\n\t\treturn false, nil\n\t}\n\tguard, err := s.guard(ctx)",
        "\tif len(atts) > 0 {\n\t\teff = effectResult{decided: true, effect: outbox.Effect{Verdict: outbox.EffectNone}}\n\t}\n\tif !eff.decided {\n\t\treturn false, nil\n\t}\n\tguard, err := s.guard(ctx)",
        K(WK, "TestEffectCheckBeforeRetry"),
        "a retry attempt waits for the effect check (review R5)"),
    one("w1rt-no-effect-check-before-first-attempt", WORKER, WK + "/session.go",
        "\tif !eff.decided {\n\t\treturn false, nil\n\t}\n\tguard, err := s.guard(ctx)",
        "\tif len(atts) == 0 {\n\t\teff = effectResult{decided: true, effect: outbox.Effect{Verdict: outbox.EffectNone}}\n\t}\n\tif !eff.decided {\n\t\treturn false, nil\n\t}\n\tguard, err := s.guard(ctx)",
        K(WK, "TestEffectSkipsOtherVersions", "TestObjectStateChangedWaitsForIndex", "TestContentOwnerVoidOnFailedRegister"),
        "the first attempt waits for the effect check too (review-test R10)"),
    one("w1b8-own-intent-hash-not-recognised", WORKER, WK + "/validate.go",
        "\tif ownIntent != \"\" && o.IntentID == ownIntent {",
        "\tif false && ownIntent != \"\" && o.IntentID == ownIntent {",
        K(WK, "TestRegisterCommitsWithEvidence"),
        "the effect check recognises this intent's own attempts"),
    one("w1b8-own-transaction-inverted", WORKER, WK + "/effect.go",
        "Canonical: c.Hash, Own: eff.OwnTransaction, DeadAttempt: dead}",
        "Canonical: c.Hash, Own: !eff.OwnTransaction, DeadAttempt: dead}",
        K(WK, "TestProviderCommitsAcceptanceFirst"),
        "own_transaction is reported as the chain shows it"),
    one("w1b8-object-state-changed-before-index", WORKER, WK + "/effect.go",
        "objHeight < first+int64(w.cfg.Outbox.IndexingMargin)",
        "objHeight < first",
        K(WK, "TestObjectStateChangedWaitsForIndex"),
        "OBJECT_STATE_CHANGED waits the indexing margin"),
    one("w1b8-unrelated-effect-ends-search", WORKER, WK + "/effect.go",
        "\t\tcase chain.EffectUnrelated:\n\t\t\tcontinue",
        "\t\tcase chain.EffectUnrelated:\n\t\t\treturn effectResult{decided: true, effect: outbox.Effect{Verdict: outbox.EffectNone}}, nil",
        K(WK, "TestEffectSkipsOtherVersions"),
        "an unrelated typed event does not end the effect search"),
    one("w1b8-replay-hash-not-first-commitment", WORKER, WK + "/settle.go",
        "\tif id == outbox.T12 {\n\t\tc.TxHash = dc.result.Hash\n\t}",
        "",
        K(WK, "TestIdempotentReplayNeedsFirstCommitment"),
        "an idempotent replay records both its own and the first commitment's hash"),
    one("w1b8-forward-retried", WORKER, WK + "/settle.go",
        "\tif s.forward {\n\t\treturn tx.FailIntent(ctx, id, from, reasonProviderMustResign, nil)\n\t}",
        "",
        K(WK, "TestForwardThatCannotCommitFailsMustResign"),
        "a forward that can no longer commit fails PROVIDER_MUST_RESIGN instead of a retry"),
    # Signer conflict and halts.
    one("w1b8-attempt-above-next-sequence", WORKER, WK + "/attempt.go",
        "\tif row.NextSequence != nil && a.Sequence > *row.NextSequence {",
        "\tif false && row.NextSequence != nil && a.Sequence > *row.NextSequence {",
        K(WK, "TestSignerConflictHalts"),
        "an attempt above the signer's next sequence halts the signer"),
    one("w1b8-foreign-use-always-cleared", WORKER, WK + "/settle.go",
        "\tif status == store.ConflictCleared {\n\t\treturn outbox.OwnerForeignCleared, nil\n\t}\n\treturn outbox.OwnerForeign, nil",
        "\t_ = status\n\treturn outbox.OwnerForeignCleared, nil",
        K(WK, "TestForeignUseOfLiveSequenceHalts"),
        "a foreign use nobody cleared halts the signer"),
    one("w1b8-foreign-use-wrong-sequence", WORKER, WK + "/settle.go",
        "\t\treturn dc.hit.Hash, dc.live.Sequence, true",
        "\t\treturn dc.hit.Hash, dc.live.Sequence + 1, true",
        K(WK, "TestForeignUseOfLiveSequenceHalts"),
        "the recorded foreign use names the live attempt's sequence"),
    one("w1b8-halt-dropped", WORKER, WK + "/settle.go",
        "\treturn s.ss.Halt(ctx, reason)",
        "\treturn nil",
        K(WK, "TestUnauthorizedRefusalHaltsSigner", "TestCommittedBytesMismatchHaltsSigner"),
        "UNAUTHORIZED and integrity alerts halt the signer"),
    one("w1b8-sdk4-retry-gap", WORKER, WK + "/attempt.go",
        "\t\tout = append(out, atts[n-1].TimeoutHeight+outbox.Sdk4RetryGapBlocks-1)",
        "\t\tout = append(out, atts[n-1].TimeoutHeight+outbox.Sdk4RetryGapBlocks-2)",
        K(WK, "TestSdk4RetryWaitsTwoBlocks"),
        "an sdk 4 retry waits two blocks (DR-10-41)"),
    one("w1b8-signed-tx-signer-unchecked", WORKER, WK + "/attempt.go",
        "\tcase !bytes.Equal(own.Signer.Address, s.key.Address) || own.Signer.Sequence != req.Sequence:",
        "\tcase !bytes.Equal(own.Signer.Address, own.Signer.Address):",
        K(WK, "TestSignedTransactionMismatchIsRefused"),
        "the signed transaction's signer and sequence are the requested ones"),
    one("w1rs-signed-message-unchecked", WORKER, WK + "/attempt.go",
        "\tcase !ok1 || !ok2 || !proto.Equal(want, got):",
        "\tcase !ok1 || !ok2 || (want == nil && got == nil):",
        K(WK, "TestSignedTransactionMismatchIsRefused"),
        "the signed transaction's message is the intended one"),
    # Revalidation before signing (object permissions on the SQL path).
    one("w1b8-no-revalidation", WORKER, WK + "/session.go",
        "\treason, err := s.w.revalidate(ctx, work, msg)",
        "\treason, err := \"\", error(nil)",
        K(WK, "TestOfferNeedsEffectiveOwner"),
        "every attempt revalidates its intent's permissions before signing"),
    one("w1rs-revalidate-returns-early", WORKER, WK + "/validate.go",
        "func (w *Worker) revalidate(ctx context.Context, work store.Work, msg sdk.Msg) (string, error) {\n",
        "func (w *Worker) revalidate(ctx context.Context, work store.Work, msg sdk.Msg) (string, error) {\n\tif work.ID != \"\" {\n\t\treturn \"\", nil\n\t}\n",
        K(WK, "TestOfferNeedsEffectiveOwner", "TestRelayRevalidatedBeforeSigning"),
        "revalidation runs for every attempt"),
    one("w1fix-relay-authority-not-checked", WORKER, WK + "/validate.go",
        "\t\tif err := w.relayAuthority(caller).CheckRelay(ctx, kind, auth, found); err != nil {",
        "\t\tif err := error(nil); err != nil {",
        K(WK, "TestSQLPathRelayPermission"),
        "an SQL-path relay needs a bound caller or the caller's own committed offer (review W4)"),
    one("w1fix-relay-authority-result-ignored", WORKER, WK + "/validate.go",
        "\t\tif err := w.relayAuthority(caller).CheckRelay(ctx, kind, auth, found); err != nil {",
        "\t\tif err := w.relayAuthority(caller).CheckRelay(ctx, kind, auth, found); err != nil && false {",
        K(WK, "TestSQLPathRelayPermission"),
        "the relay authority's refusal is applied (review W6)"),
    one("w1fix-forward-relay-authority-not-checked", WORKER, WK + "/validate.go",
        "\tif err := w.relayAuthority(caller).CheckRelay(ctx, policy.KindForwardProviderTx, auth, fwd.Relay); err != nil {",
        "\tif err := w.relayAuthority(caller).CheckRelay(ctx, policy.KindForwardProviderTx, auth, fwd.Relay); err != nil && false {",
        K(WK, "TestSQLPathRelayPermission"),
        "an SQL-path forward needs relay authority (review W5)"),
    one("w1fix-provider-precheck-refusal-ignored", WORKER, WK + "/validate.go",
        "ContentID: loc.contentID, MinHeight: st.Height, ChainTime: st.Time, DeadlinePages: w.cfg.Numbers.DeadlinePages})\n\t\tif err != nil {",
        "ContentID: loc.contentID, MinHeight: st.Height, ChainTime: st.Time, DeadlinePages: w.cfg.Numbers.DeadlinePages})\n\t\tif err != nil && policy.CodeOf(err) == \"\" {",
        K(WK, "TestRelayRevalidatedBeforeSigning"),
        "a provider pre-check refusal before signing fails the intent (review W7)"),
    one("w1fix-publication-refusal-ignored", WORKER, WK + "/validate.go",
        "\t\t\tRegistry: w.cfg.Registry, Owner: owner, OnChain: onchain}); err != nil {\n\t\t\treturn authorized{}, err",
        "\t\t\tRegistry: w.cfg.Registry, Owner: owner, OnChain: onchain}); err != nil && policy.CodeOf(err) == \"\" {\n\t\t\treturn authorized{}, err",
        K(WK, "TestSQLPathPublicationPermission"),
        "an SQL-path register or withdraw needs the publication permission (review W8)"),
    one("w1fix-owner-effective-from-stored-signer", WORKER, WK + "/housekeeping.go",
        "\terr = w.st.ConfirmContentOwner(ctx, intentID, resp.GetContent().GetSubmittingHost())",
        "\terr = w.st.ConfirmContentOwner(ctx, intentID, work.SignerAddress)",
        K(WK, "TestOwnerHostMismatchStaysPending"),
        "an owner row becomes EFFECTIVE from the chain's submitting host, not the stored signer (review W3)"),
    one("w1b8-sql-path-unavailable-not-failed", WORKER, WK + "/housekeeping.go",
        "\t\tif errors.Is(err, store.ErrContentUnavailable) || errors.Is(err, store.ErrTxTracked) {",
        "\t\tif false {",
        K(WK, "TestSQLPathValidation"),
        "an SQL-path intent for content another caller owns, or for tracked bytes, fails"),
    one("w1b8-sql-path-lifetime-unbounded", WORKER, WK + "/validate.go",
        "\tif lifetime < w.cfg.Numbers.MinLifetime || lifetime > w.cfg.Numbers.MaxLifetime ||",
        "\tif lifetime < w.cfg.Numbers.MinLifetime || lifetime > 100*w.cfg.Numbers.MaxLifetime ||",
        K(WK, "TestSQLPathValidation"),
        "an SQL-path intent's lifetime is bounded"),
    # Outage.
    one("w1b8-stall-not-blocking", WORKER, WK + "/session.go",
        "\t\tg.Stalled = true",
        "\t\tg.Stalled = false",
        K(WK, "TestStallBlocksNewAttempts"),
        "the worker starts no new attempt while a stall episode is open"),
    one("w1b8-observer-never-opens-stall", WORKER, WK + "/observer.go",
        "\tif now.Sub(o.failSince) >= o.w.cfg.Numbers.StallWindow {",
        "\tif false && now.Sub(o.failSince) >= o.w.cfg.Numbers.StallWindow {",
        K(WK, "TestObserverStallEpisodes"),
        "an unreachable node for the stall window opens a stall episode"),
    one("w1b8-observer-height-shifted", WORKER, WK + "/observer.go",
        "\tif err := o.w.st.RecordChainHealth(ctx, uint64(st.Height), st.Time); err != nil {",
        "\tif err := o.w.st.RecordChainHealth(ctx, uint64(st.Height)+1, st.Time); err != nil {",
        K(WK, "TestObserverStallEpisodes"),
        "the observer records the height the node reported"),
    one("w1fix-lock-held-without-session-name", WORKER, WK + "/worker.go",
        "\t\t\t\terr = fmt.Errorf(\"%w (%s)\", ErrLockHeld, s.name())",
        "\t\t\t\terr = ErrLockHeld",
        K(WK, "TestReconcileOnceTakesLock"),
        "reconcile-once names the session whose lock another process holds (fix F9)"),

    # ---------------------------------------------------------------- smoke (PostgreSQL; svn-broadcaster processes)
    one("w1b7b-schema-behind-not-refused", SMOKE, CMD + "/serve.go",
        "case errors.Is(err, store.ErrSchemaBehind), errors.Is(err, store.ErrDatabaseNewer),",
        "case errors.Is(err, store.ErrDatabaseNewer),",
        K(CMD, "TestSmokeProcesses"),
        "serve refuses a database whose schema is behind"),
    one("w1b7b-lock-held-not-reported", SMOKE, CMD + "/serve.go",
        "if errors.Is(err, worker.ErrLockHeld) {",
        "if false {",
        K(CMD, "TestSmokeProcesses"),
        "reconcile-once exits with its lock-held code when another process drives the signer"),
    one("w1b7b-signer-clear-not-found-code", SMOKE, CMD + "/signerclear.go",
        "return notFoundError(errors.New(\"no uncleared signer event matches this signer, sequence and hash\"))",
        "return errors.New(\"no uncleared signer event matches this signer, sequence and hash\")",
        K(CMD, "TestSmokeProcesses"),
        "signer-clear of an unknown event exits with the not-found code"),
    one("w1b7b-migrate-registers-no-callers", SMOKE, CMD + "/migrate.go",
        "for _, r := range regs {",
        "for _, r := range regs[:0] {",
        K(CMD, "TestSmokeProcesses"),
        "migrate registers the configured callers"),
    one("w1b7b-runtime-role-owned-by-root", SMOKE, CMD + "/devdb.go",
        "{store.RuntimeRole, \"runtime\", runtimeOwner}",
        "{store.RuntimeRole, \"runtime\", env.euid + 0*runtimeOwner}",
        K(CMD, "TestSmokeProcesses"),
        "the runtime role's password file is owned by the serve uid"),
    one("w1b7b-server-cert-owned-by-root", SMOKE, CMD + "/devca.go",
        "files.ServerCert, files.ServerKey, serveUID, euid); err != nil {",
        "files.ServerCert, files.ServerKey, euid+0*serveUID, euid); err != nil {",
        K(CMD, "TestSmokeProcesses"),
        "the server key file is owned by the serve uid"),
    # ---------------------------------------------------------------- devnet (validators, PostgreSQL, processes)
    # Plan 13.7's devnet killers. Each mutant repeats the edit of a unit or
    # PostgreSQL mutant listed above and names only the devnet test, so the
    # devnet phase alone must fail on it.
    one("dv-effect-ignores-signer", DEVNET, CH + "/effect.go",
        "\tif key.SignerBound && !ourKey {",
        "\tif false && key.SignerBound && !ourKey {",
        KL("OC"),
        "devnet OC: a copy of our offer committed by service-2 is OBJECT_TAKEN, not our effect (edit of w0b2-effect-ignores-signer)",
        DEVNET_TIMEOUT),
    one("dv-unrecorded-send-counted-refusal", DEVNET, WK + "/settle.go",
        "\t\t\tif _, err := s.ss.RecordSendOutcome(ctx, att.ID, store.SendOutcome{Class: store.SendUnknown,",
        "\t\t\tif _, err := s.ss.RecordSendOutcome(ctx, att.ID, store.SendOutcome{Class: store.SendNodeRefused, CountsAsRefusal: true,",
        KL("WP2"),
        "devnet WP2: after a death between sends_started and the send, the restart never marks the attempt dead by (d2) "
        "(edit of w1rt-unrecorded-send-counted-refusal)",
        DEVNET_TIMEOUT),
    Mutant("dv-stall-guards-removed", DEVNET, (
        Edit(WK + "/session.go", "\t\tg.Stalled = true", "\t\tg.Stalled = false"),
        Edit(ST + "/signer.go", "\tif stalled {\n\t\treturn refuse(RefusedStalled)", "\tif false {\n\t\treturn refuse(RefusedStalled)"),
    ), K(DV, "TestDevnetBroadcasterQuorumStall"),
        "devnet stall test: no new attempt while a stall episode is open; the worker's stall flag and the store's "
        "refusal are both removed (the edits of w1b8-stall-not-blocking and w0b1-stall-not-checked), since either one "
        "alone keeps the rule",
        DEVNET_TIMEOUT),
    one("dv-r3-simulate-through-app-path", DEVNET, CH + "/rpc.go",
        "\tpathSimulate     = \"/cosmos.tx.v1beta1.Service/Simulate\"",
        "\tpathSimulate     = \"/app/simulate\"",
        KL("H"),
        "devnet H: the WITHDRAW_CONTENT, the kind whose gas is simulated, commits only through the transaction service's "
        "Simulate route (edit of b10-r3-simulate-through-app-path)",
        DEVNET_TIMEOUT),
]

BUILD_FAILURE = re.compile(r"\[(build|setup) failed\]|^# \S+$|cannot find package|undefined: ", re.M)
# A PostgreSQL syntax error in the output of a mutant that edits a
# migration means the mutated migration does not parse; that is not a kill.
SQL_SYNTAX = re.compile(r"syntax error at or near|\(42601\)")
TOP_FAIL = re.compile(r"^--- FAIL: (\S+) \(", re.M)
TOP_PASS = re.compile(r"^--- PASS: (\S+) \(", re.M)
TOP_SKIP = re.compile(r"^--- SKIP: (\S+) \(", re.M)
PANIC = re.compile(r"^panic: ", re.M)
RUN_LINE = re.compile(r"^=== (?:RUN|CONT)\s+(\S+)", re.M)
GO_RESULT = re.compile(r"^(ok|FAIL)\s+(" + re.escape(GO_MODULE_PREFIX) + r"\S+)")
TEST_FUNC = re.compile(r"^func ((?:Test|Fuzz)\w+)\(", re.M)
BUILD_LINE = re.compile(r"^//go:build (!?)(\w+)\s*$", re.M)
# A lifecycle phase entry of chain/devnet/broadcaster_test.go:
# {"<name>", phase<Func>}.
PHASE_ENTRY = re.compile(r'^\t\{"(\w+)", phase\w+\},$', re.M)


def repo_root() -> pathlib.Path:
    here = pathlib.Path(__file__).resolve().parent
    out = subprocess.run(["git", "-C", str(here), "rev-parse", "--show-toplevel"],
                         capture_output=True, text=True, check=True)
    return pathlib.Path(out.stdout.strip())


def git(root: pathlib.Path, *args: str) -> str:
    return subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True, check=True).stdout.strip()


def export(root: pathlib.Path, rev: str, dest: pathlib.Path) -> None:
    raw = subprocess.run(["git", "-C", str(root), "archive", "--format=tar", rev],
                         capture_output=True, check=True).stdout
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as tar:
        tar.extractall(dest, filter="data")


def copy_worktree(root: pathlib.Path, dest: pathlib.Path) -> int:
    """Copy every tracked or untracked, not ignored, file of the working tree
    that exists on disk into dest, as it is on disk. Returns the file count.
    A symbolic link is copied as a link."""
    argv = ["git", "-C", str(root), "ls-files", "-z", "--cached", "--others", "--exclude-standard"]
    raw = subprocess.run(argv, capture_output=True, check=True).stdout
    count = 0
    for name in sorted({n for n in raw.decode().split("\0") if n}):
        src = root / name
        if not src.is_symlink() and not src.is_file():
            continue  # deleted in the working tree, or a submodule directory
        target = dest / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, target, follow_symlinks=False)
        count += 1
    return count


def mutate(copy: pathlib.Path, m: Mutant) -> tuple[dict[str, str], list[str]]:
    """Return the mutated text of every file m edits, and the problems.

    The edits apply in order. Each search string must occur exactly once in
    its file as it is when that edit is applied. Nothing is written.
    """
    texts: dict[str, str] = {}
    problems: list[str] = []
    for i, e in enumerate(m.edits):
        target = copy / e.path
        if e.path not in texts:
            if not target.is_file():
                problems.append(f"MISSING_FILE {m.name} edit={i} {e.path}")
                continue
            texts[e.path] = target.read_text()
        n = texts[e.path].count(e.old)
        if n != 1:
            problems.append(f"{'NOT_FOUND' if n == 0 else 'NOT_UNIQUE'} {m.name} edit={i} {e.path} occurrences={n}")
            continue
        if e.old == e.new:
            problems.append(f"NO_CHANGE {m.name} edit={i}")
        texts[e.path] = texts[e.path].replace(e.old, e.new, 1)
    return texts, problems


def builds_with(text: str, tags: str) -> bool:
    """Whether a _test.go file is part of a build with the given tags. Only
    the single-tag constraints this module uses (`//go:build tag` and
    `//go:build !tag`) are evaluated; a file without one is always built."""
    m = BUILD_LINE.search(text)
    if m is None:
        return True
    present = m.group(2) in set(tags.split(",")) if tags else False
    return present != (m.group(1) == "!")


def defined_tests(copy: pathlib.Path, killer: Killer, cache: dict[tuple[str, str], set[str]]) -> set[str]:
    """The Go test and fuzz functions of a package directory's _test.go
    files that a build with the killer's tags compiles, in the copy."""
    key = (killer.package, killer.tags)
    if key not in cache:
        names: set[str] = set()
        target = copy / killer.package
        if target.is_dir():
            for path in sorted(target.glob("*_test.go")):
                text = path.read_text()
                if builds_with(text, killer.tags):
                    names |= set(TEST_FUNC.findall(text))
        cache[key] = names
    return cache[key]


def lifecycle_phases(copy: pathlib.Path) -> set[str]:
    """The phase names of TestDevnetBroadcasterLifecycle in the copy."""
    path = copy / DV / "broadcaster_test.go"
    return set(PHASE_ENTRY.findall(path.read_text())) if path.is_file() else set()


def validate(copy: pathlib.Path, mutants: list[Mutant]) -> list[str]:
    problems: list[str] = []
    names: set[str] = set()
    tests: dict[tuple[str, str], set[str]] = {}
    phases = lifecycle_phases(copy)
    for m in mutants:
        if m.name in names:
            problems.append(f"DUPLICATE_NAME {m.name}")
        names.add(m.name)
        if m.group not in GROUPS:
            problems.append(f"UNKNOWN_GROUP {m.name} {m.group}")
        if not m.edits:
            problems.append(f"NO_EDIT {m.name}")
        if not m.killers:
            problems.append(f"NO_KILLER {m.name}")
        for k in m.killers:
            if k.name == "TestMain" or k.name not in defined_tests(copy, k, tests):
                problems.append(f"KILLER_NOT_DEFINED {m.name} {k.label()}")
            if k.phases and (k.package != DV or k.name != "TestDevnetBroadcasterLifecycle"
                             or not set(k.phases.split(",")) <= phases):
                problems.append(f"PHASES_NOT_DEFINED {m.name} {k.label()}")
            if (k.package == DV) != (m.group == DEVNET):
                problems.append(f"DEVNET_KILLER_GROUP {m.name} {k.label()}")
        problems.extend(mutate(copy, m)[1])
    return problems


def commands(killers: tuple[Killer, ...],
             timeout: str = GO_TEST_TIMEOUT) -> list[tuple[list[str], list[Killer], dict[str, str]]]:
    """One go test command per set of build tags and lifecycle phases among
    the killers, with the killers it runs and the environment it adds (the
    phases, for a lifecycle killer)."""
    out: list[tuple[list[str], list[Killer], dict[str, str]]] = []
    for tags, phases in sorted({(k.tags, k.phases) for k in killers}):
        mine = [k for k in killers if k.tags == tags and k.phases == phases]
        run = "^(" + "|".join(sorted({k.name for k in mine})) + ")$"
        packages = ["./" + p + "/" for p in sorted({k.package for k in mine})]
        tag_args = ["-tags", tags] if tags else []
        extra = {PHASES_ENV: phases} if phases else {}
        out.append((["go", "test", "-count=1", "-p", "2", "-v", *tag_args, "-timeout", timeout, "-run", run,
                     *packages], mine, extra))
    return out


def go_segments(out: str) -> dict[str, str]:
    """Split go test output into the output of each package, keyed by the
    repository-relative package directory. go test buffers the output of
    each package when it tests more than one, so a package's output ends
    with its `ok` or `FAIL` line."""
    segments: dict[str, str] = {}
    buf: list[str] = []
    for line in out.splitlines(keepends=True):
        m = GO_RESULT.match(line)
        if m:
            pkg = m.group(2).removeprefix(GO_MODULE_PREFIX)
            segments[pkg] = segments.get(pkg, "") + "".join(buf) + line
            buf = []
        else:
            buf.append(line)
    if buf:
        segments[""] = segments.get("", "") + "".join(buf)
    return segments


def go_failed(out: str, killers: list[Killer]) -> list[str]:
    """The named Go killers that failed in their own package's output."""
    segments = go_segments(out)
    failed: list[str] = []
    for k in killers:
        seg = segments.get(k.package, "")
        if k.name in TOP_FAIL.findall(seg):
            failed.append(k.label())
            continue
        panic = PANIC.search(seg)
        if panic:
            runs = RUN_LINE.findall(seg[:panic.start()])
            if runs and runs[-1].split("/")[0] == k.name:
                failed.append(k.label() + "(panic)")
    return failed


@dataclass
class Outcome:
    verdict: str
    codes: list[int]
    out: str
    secs: float
    failed: list[str]


def run(copy: pathlib.Path, killers: tuple[Killer, ...], env: dict[str, str], timeout: int,
        go_timeout: str = GO_TEST_TIMEOUT, edits_sql: bool = False) -> Outcome:
    t0 = time.monotonic()
    verdicts: list[str] = []
    codes: list[int] = []
    parts: list[str] = []
    failed: list[str] = []
    for argv, mine, extra in commands(killers, go_timeout):
        prefix = "".join(f"{k}={v} " for k, v in extra.items())
        parts.append(f"=== COMMAND {prefix}{' '.join(argv)}\n")
        try:
            p = subprocess.run(argv, cwd=copy, env={**env, **extra}, capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired as exc:
            raw = exc.stdout or b""
            parts.append(raw.decode(errors="replace") if isinstance(raw, bytes) else raw)
            verdicts.append("TIMEOUT")
            codes.append(-1)
            continue
        out = p.stdout + p.stderr
        parts.append(out)
        codes.append(p.returncode)
        hit = go_failed(out, mine)
        failed.extend(hit)
        if p.returncode == 0:
            verdicts.append("SURVIVED")
        elif BUILD_FAILURE.search(out) or (edits_sql and SQL_SYNTAX.search(out)):
            verdicts.append("BUILD_FAILED")
        elif hit:
            verdicts.append("KILLED")
        else:
            verdicts.append("UNCLEAR")
    return Outcome(combine(verdicts), codes, "".join(parts), time.monotonic() - t0, failed)


def combine(verdicts: list[str]) -> str:
    for verdict in ("BUILD_FAILED", "KILLED", "TIMEOUT", "UNCLEAR"):
        if verdict in verdicts:
            return verdict
    return "SURVIVED"


def group_killers(mutants: list[Mutant], group: str) -> tuple[Killer, ...]:
    seen: dict[Killer, None] = {}
    for m in mutants:
        if m.group == group:
            for k in m.killers:
                seen.setdefault(k, None)
    return tuple(seen)


def duration_seconds(d: str) -> int:
    """Seconds of a go test -timeout this runner writes: a whole number of
    s, m or h."""
    return int(d[:-1]) * {"s": 1, "m": 60, "h": 3600}[d[-1]]


def control_timeout(mutants: list[Mutant], group: str) -> str:
    """The go test -timeout of a group's control: the longest of its
    mutants'."""
    return max((m.timeout for m in mutants if m.group == group), key=duration_seconds, default=GO_TEST_TIMEOUT)


def control_problems(killers: tuple[Killer, ...], o: Outcome) -> list[str]:
    problems = [f"command {i} exit={rc}" for i, rc in enumerate(o.codes) if rc != 0]
    if o.verdict == "TIMEOUT":
        problems.append("timeout")
    segments = go_segments(o.out)
    for k in killers:
        seg = segments.get(k.package, "")
        if k.name not in TOP_PASS.findall(seg):
            problems.append(f"{k.label()} did not run and pass at top level")
        if k.name in TOP_SKIP.findall(seg):
            problems.append(f"{k.label()} skipped")
    return problems


def main() -> int:
    ap = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    ap.add_argument("--rev", default=None, help="revision to git archive (default HEAD)")
    ap.add_argument("--worktree", action="store_true",
                    help="copy the working tree (uncommitted and untracked, not ignored, files) "
                    "instead of --rev")
    ap.add_argument("--group", default="", help="comma-separated groups to run (default: all)")
    ap.add_argument("--only", default="", help="comma-separated mutant names")
    ap.add_argument("--check", action="store_true", help="validate search strings and killers only")
    ap.add_argument("--list", action="store_true", help="list the mutants only")
    ap.add_argument("--logdir", default="", help="write each command's full output here")
    ap.add_argument("--keep", action="store_true", help="keep the temporary copy and GOCACHE")
    ap.add_argument("--gocache", default="", help="GOCACHE to use instead of a fresh one in the copy")
    ap.add_argument("--timeout", type=int, default=1200, help="seconds per command")
    args = ap.parse_args()

    if args.list:
        for m in MUTANTS:
            files = ",".join(dict.fromkeys(e.path for e in m.edits))
            killers = " ".join(k.label() for k in m.killers)
            limit = f" go_timeout={m.timeout}" if m.timeout != GO_TEST_TIMEOUT else ""
            print(f"{m.group:12} {m.name} {files} killers={killers}{limit}")
        return 0
    root = repo_root()
    if args.worktree and args.rev is not None:
        print("--worktree and --rev are exclusive")
        return 2
    groups = [g for g in args.group.split(",") if g]
    unknown_groups = set(groups) - set(GROUPS)
    if unknown_groups:
        print(f"unknown groups: {sorted(unknown_groups)}")
        return 2
    rev = args.rev or "HEAD"
    commit = git(root, "rev-parse", f"{rev}^{{commit}}")
    tree = git(root, "rev-parse", f"{rev}^{{tree}}")
    only = {n for n in args.only.split(",") if n}
    unknown = only - {m.name for m in MUTANTS}
    if unknown:
        print(f"unknown mutant names: {sorted(unknown)}")
        return 2
    selected = [m for m in MUTANTS if (not only or m.name in only) and (not groups or m.group in groups)]
    if not selected:
        print("no mutant selected")
        return 2

    work = pathlib.Path(tempfile.mkdtemp(prefix="goal11-mutants-"))
    if work.resolve().is_relative_to(root.resolve()):
        print(f"temporary directory {work} is inside the repository")
        return 2
    copy = work / "tree"
    copy.mkdir()
    logdir = pathlib.Path(args.logdir).resolve() if args.logdir else None
    if logdir:
        logdir.mkdir(parents=True, exist_ok=True)
    print(f"revision {rev} commit {commit} tree {tree}")
    if args.worktree:
        argv = ["git", "-C", str(root), "status", "--porcelain", "--untracked-files=all"]
        out = subprocess.run(argv, capture_output=True, text=True, check=True).stdout
        status = out.splitlines()
        print(f"copy {copy} (working tree of {root}, not a committed revision; "
              f"git status --porcelain: {len(status)} entries)")
        for line in status:
            print(f"    {line}")
    else:
        print(f"copy {copy} (git archive)")
    counts = ", ".join(f"{g}={sum(1 for m in selected if m.group == g)}" for g in GROUPS)
    print(f"mutants {len(selected)} of {len(MUTANTS)} ({counts})")
    t_start = time.monotonic()
    try:
        if args.worktree:
            print(f"copied {copy_worktree(root, copy)} files")
        else:
            export(root, commit, copy)
        problems = validate(copy, selected)
        for line in problems:
            print(line)
        if problems:
            print(f"RESULT validation problems={len(problems)}; no test was run")
            return 1
        print("search strings: every one occurs exactly once; every named killer is defined")
        if args.check:
            return 0

        gocache = pathlib.Path(args.gocache).resolve() if args.gocache else work / "gocache"
        env = dict(os.environ, GOPROXY="off", GOCACHE=str(gocache))
        version = subprocess.run(["go", "version"], cwd=copy, capture_output=True, text=True, env=env)
        print(f"go {version.stdout.strip()} {version.stderr.strip()}".rstrip())
        print(f"euid {os.geteuid()}")
        print(f"GOCACHE={gocache}{' (given)' if args.gocache else ' (fresh, in the copy)'}")
        used = [g for g in GROUPS if any(m.group == g for m in selected)]
        controls = {g: group_killers(selected, g) for g in used}
        for group in used:
            starts = "starts PostgreSQL" if group in POSTGRES_GROUPS else "starts no PostgreSQL"
            if group in VALIDATOR_GROUPS:
                starts += " and four svnd validators"
            for argv, _, extra in commands(controls[group], control_timeout(selected, group)):
                prefix = "".join(f"{k}={v} " for k, v in extra.items())
                print(f"control command {group} ({starts}): {prefix}{' '.join(argv)}")

        group_secs: dict[str, float] = {}
        for group in used:
            o = run(copy, controls[group], env, args.timeout, control_timeout(selected, group))
            group_secs[group] = group_secs.get(group, 0.0) + o.secs
            if logdir:
                (logdir / f"control-{group}.txt").write_text(o.out)
            bad_control = control_problems(controls[group], o)
            print(f"CONTROL {group} {'FAIL' if bad_control else 'PASS'} exit={o.codes} secs={o.secs:.1f} "
                  f"named_tests={len(controls[group])}", flush=True)
            if bad_control:
                for line in bad_control:
                    print(f"    {line}")
                print(o.out[-4000:])
                print("RESULT control failed; mutants not run")
                return 1

        tally: dict[str, int] = {}
        per_group: dict[str, dict[str, int]] = {}
        bad = []
        for m in selected:
            texts, problems = mutate(copy, m)
            if problems:  # validated above; a second miss means the copy changed
                print("\n".join(problems))
                return 1
            originals = {path: (copy / path).read_text() for path in texts}
            edits_sql = any(path.endswith(".pgsql") for path in texts)
            try:
                for path, text in texts.items():
                    (copy / path).write_text(text)
                o = run(copy, m.killers, env, args.timeout, m.timeout, edits_sql)
            finally:
                for path, text in originals.items():
                    (copy / path).write_text(text)
            group_secs[m.group] = group_secs.get(m.group, 0.0) + o.secs
            tally[o.verdict] = tally.get(o.verdict, 0) + 1
            row = per_group.setdefault(m.group, {})
            row[o.verdict] = row.get(o.verdict, 0) + 1
            if logdir:
                (logdir / f"{m.name}.txt").write_text(o.out)
            panic = " panic=yes" if PANIC.search(o.out) else ""
            files = ",".join(dict.fromkeys(e.path for e in m.edits))
            print(f"{o.verdict} {m.name} group={m.group} exit={o.codes} secs={o.secs:.1f} file={files}{panic} "
                  f"failing={' '.join(o.failed) or '-'}", flush=True)
            print(f"    reason: {m.reason}", flush=True)
            if o.verdict != "KILLED":
                bad.append(m.name)
                others = sorted(set(TOP_FAIL.findall(o.out)) - {k.name for k in m.killers})
                if others:
                    print(f"    other failing tests (not named killers): {' '.join(others)}", flush=True)
                tail = "\n".join("    " + line for line in o.out.strip().splitlines()[-25:])
                print("    output tail:\n" + tail, flush=True)
        for group in used:
            row = per_group.get(group, {})
            summary = " ".join(f"{k.lower()}={v}" for k, v in sorted(row.items()))
            print(f"GROUP {group} secs={group_secs.get(group, 0.0):.1f} {summary}")
        print(f"TOTAL_SECS {time.monotonic() - t_start:.1f}")
        summary = " ".join(f"{k.lower()}={v}" for k, v in sorted(tally.items()))
        print(f"RESULT mutants={len(selected)} {summary}")
        if bad:
            print(f"NOT KILLED: {', '.join(bad)}")
            return 1
        return 0
    finally:
        if args.keep:
            print(f"kept {work}")
        else:
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
