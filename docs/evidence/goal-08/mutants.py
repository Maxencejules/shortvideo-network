#!/usr/bin/env python3
"""Goal 08 mutation runner (standard library only).

What it does:
1. Exports a committed revision (default HEAD) with `git archive` into a new
   temporary directory outside the repository. With --worktree it copies the
   working tree instead (see below).
2. Runs the test command of every selected group once on the unmutated copy
   (control). A control passes only when the command exits 0 and every test
   the command names ran at top level and printed `--- PASS`. A failing
   control stops the run before any mutant.
3. For each mutant: applies its edits (each replaces one exact search string
   in one file; the string must occur exactly once in the file as it is when
   the edit is applied), runs the group's named non-devnet go test command in
   the copy, and restores every edited file.

Verdicts:
- KILLED: the command exited non-zero, the packages built, and the output
  has a top-level `--- FAIL:` line or a `panic:` line.
- SURVIVED: the command exited zero.
- BUILD_FAILED: a package or test binary did not build. This is not a kill.
- UNCLEAR: the command exited non-zero with no build failure, no failing
  test and no panic. This is not a kill.
- TIMEOUT: the command ran longer than --timeout seconds.

Exit status is 0 only when every control passed and every mutant is KILLED.
It is non-zero when a search string is missing or not unique (checked for
all selected mutants before any test runs), when a control fails, or when
any mutant is SURVIVED, BUILD_FAILED, UNCLEAR or TIMEOUT.

Usage:
  python3 docs/evidence/goal-08/mutants.py [--rev REV | --worktree]
                                            [--group seat|review|leases]
                                            [--only a,b] [--check]
                                            [--logdir DIR] [--keep]

--check validates the search strings and runs nothing.

--worktree copies the working tree instead of exporting --rev: every file
`git ls-files --cached --others --exclude-standard` lists that exists on
disk, as it is on disk (uncommitted edits and untracked, not ignored, files
included; ignored files such as .venv are not copied). It is for dry runs of
uncommitted tests and mutants. Its output is not evidence for a committed
revision: the header names HEAD and prints `git status --porcelain`, so a
log shows what the copy held beyond HEAD. --worktree and --rev are
exclusive. Without --worktree the runner exports --rev (default HEAD) with
`git archive`, as before.

The copy runs Go with GOPROXY=off, so the module cache must already hold
the dependencies and the toolchain that go.work names. The copy uses its own
GOCACHE under the temporary directory, which is removed at the end unless
--keep is given. No command starts a validator: the devnet package is never
tested here.

Where the mutants come from.
- Group seat: DR-06-06 seat-only admission (owner ruling 2026-11-02). The
  eleven mutants of the seat-admission scratch runner (not in Git) that
  reported 11/11 KILLED on the uncommitted merge worktree. Their search
  strings are that runner's, unchanged except that paths start at the
  repository root. Two more carry forward earlier runners' mutants whose
  search strings DR-06-06 changed, so their `--check` misses them on this
  tree: carried-ante-extra-admission (goal 06 ante-extra-admission, round-2
  review B1, on the new ante admission callback) and
  carried-doc-seat-match-case-sensitive (goal 07 doc-seat-rule-case-sensitive,
  on the new refuse-every-role check).
- Group review: the goal 08 review-round mutants M1, M2, M4, M5 and M6. M1,
  M2, M5 and M6 use the mutated line their scratch logs print; M4 (D-10
  disabled) is described in the review notes only, so its search string is a
  reconstruction of the same `if false &&` form. The review's M3 (export
  catalogue lease check) is not listed.
- Group leases: capacity and lease-lifecycle mutants that the goal 08
  acceptance checks depend on: over-capacity reservation, a reservation
  released twice at end or at expiry, repeated ending, a duplicate
  (provider, content, version) copy (REQ-INV-001), a service key accepted as
  the provider acceptance signature (REQ-CHAIN-012), and an expiry sweep that
  ignores the per-block budget, and an expiry that treats a damaged
  governed max_expiry_steps as the protocol ceiling instead of failing
  closed. They are new in this runner. The capacity mutant changes the real
  provider keeper (chain/x/providers/handoff.go); the x/leases unit tests
  use a fake provider registry and do not reach it (TestCapacityBoundaryThroughAccept
  pins what x/leases asks of the registry, on the fake), while difftest
  TestCapacityBoundaryAgainstSpecMachine and app TestCapacityBoundaryInApp
  run the one-byte boundary on the real keeper.
This runner re-runs all of them on the committed tree; its own output is the
evidence, not the scratch logs.
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

SEAT = "seat"
REVIEW = "review"
LEASES = "leases"
GROUPS = (SEAT, REVIEW, LEASES)


@dataclass(frozen=True)
class Command:
    packages: tuple[str, ...]
    tests: tuple[str, ...]
    timeout: str = "15m"

    def argv(self) -> list[str]:
        run = "^(" + "|".join(self.tests) + ")$"
        return ["go", "test", "-count=1", "-v", "-timeout", self.timeout, "-run", run, *self.packages]


# One named non-devnet test command per group, run from the root of the copy.
# Each names the tests expected to kill the group's mutants.
COMMANDS: dict[str, Command] = {
    SEAT: Command(
        ("./chain/x/authority/", "./chain/genesis/", "./chain/app/"),
        (
            "TestAdmissionOf",
            "TestValidateGenesisState",
            "TestSeatRotationMovesSeatAdmission",
            "TestEngineOnlyRotationRejects",
            "TestSeatAddressIsNotASubmitter",
            "TestSeatKeysSignOnlyCouncilMessages",
            "TestSeatRotationMovesAnteAdmission",
            "TestRegistryMessagesRejectOtherRoles",
            "TestRejectedMessagesLeaveTheFullAuthorityState",
            "TestAnteRejectsNonAdmittedAccount",
            "TestCommittedAuthorityFlowAndReplay",
        ),
    ),
    REVIEW: Command(
        ("./chain/x/leases/", "./chain/x/leases/difftest/", "./chain/app/"),
        (
            "TestGenesisValidateImportExport",
            "TestOfferCheckOrder",
            "TestOpenOffersPerContentCapD6",
            "TestOpenOffersPerSchedulerCapD10",
            "TestEndCheckOrder",
            "TestEndSignedForEarlierTermD12",
            "TestAcceptReplayByAnotherProviderD13",
            "TestDeviationRulesAreLoadBearing",
            "TestWithdrawalAtContentCapGas",
            "TestWithdrawalAtCapGasInApp",
            "TestRejectedLeaseMessagesLeaveTheFullState",
        ),
    ),
    LEASES: Command(
        ("./chain/x/leases/", "./chain/x/leases/difftest/", "./chain/x/providers/", "./chain/app/"),
        (
            # chain/x/providers: the real Reserve/Release accounting. The
            # x/leases unit tests use a fake provider registry, so the
            # capacity mutant of the real keeper is killed here.
            "TestReserveReleaseAccounting",
            "TestAccountingFuzzSeedsReachEveryOutcome",
            # chain/x/leases (keeper, fake registries)
            "TestAcceptCheckOrder",
            "TestOfferCheckOrder",
            "TestEndCheckOrder",
            "TestAuthorityEndCheckOrder",
            "TestDuplicateReplicaRejected",
            "TestServiceKeyCannotSignAcceptance",
            "TestEndReleasesOnce",
            "TestExpiryOrderAndBudget",
            "TestExpiryWorkIsBounded",
            "TestCapacityBoundaryThroughAccept",
            # chain/x/leases/difftest (real keepers, spec_machine.py oracle)
            "TestDifferentialAgainstSpecMachine",
            "TestExpiryWorkIndependentOfBacklog",
            "TestCapacityBoundaryAgainstSpecMachine",
            # chain/app (committed transactions through FinalizeBlock)
            "TestServiceKeyCannotSignAcceptanceInApp",
            "TestRejectedLeaseMessagesLeaveTheFullState",
            "TestLeaseRetryScenarios",
            "TestLargeExpiringCatalogue",
            "TestRestartContinuesExpiry",
            "TestCapacityBoundaryInApp",
            "TestDamagedExpiryLimitHalts",
        ),
    ),
}


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
    reason: str


def one(name: str, group: str, path: str, old: str, new: str, reason: str) -> Mutant:
    return Mutant(name, group, (Edit(path, old, new),), reason)


ANTE = "chain/app/ante.go"
APP = "chain/app/app.go"
APPSTATE = "chain/app/state.go"
DOC = "chain/genesis/document.go"
AGEN = "chain/x/authority/genesis.go"
ASTORE = "chain/x/authority/store.go"
ENGINE = "chain/x/authority/engine.go"
LMSG = "chain/x/leases/msg_server.go"
LGEN = "chain/x/leases/genesis.go"
LEXP = "chain/x/leases/expiry.go"
PHANDOFF = "chain/x/providers/handoff.go"

MUTANTS: list[Mutant] = [
    # Group seat: DR-06-06 seat-only admission.
    one("ante-seat-admission-removed", SEAT, ASTORE,
        "\tif seatAt(cfg.GetAuthority().GetDevnetTestSeats(), addr) != nil {\n\t\treturn AdmittedSeat\n\t}",
        "\tif seatAt(cfg.GetAuthority().GetDevnetTestSeats(), addr) != nil {\n\t\treturn NotAdmitted\n\t}",
        "AdmissionOf must admit a council seat account from the seat list in the authority config"),
    one("ante-seat-gate-removed", SEAT, ANTE,
        "\tif seat {\n\t\tfor _, msg := range tx.GetMsgs() {",
        "\tif seat && false {\n\t\tfor _, msg := range tx.GetMsgs() {",
        "the ante must refuse a seat-signed transaction that carries a non-council message"),
    one("auth-genesis-allows-seat-row", SEAT, AGEN,
        "\t\t\tif bytes.Equal(row.GetAddress(), addr) {\n\t\t\t\treturn genesisError(CodeForbidden,",
        "\t\t\tif bytes.Equal(row.GetAddress(), addr) && false {\n\t\t\t\treturn genesisError(CodeForbidden,",
        "authority ValidateGenesisState must refuse a seat address in the submitter list"),
    one("doc-genesis-allows-seat-row", SEAT, DOC,
        "\t\t\tif strings.ToLower(row.AddressHex) == addr {",
        "\t\t\tif strings.ToLower(row.AddressHex) == addr && false {",
        "genesis.Validate must refuse a seat address in the submitter list"),
    Mutant(
        "doc-and-auth-allow-broadcaster-seat-row", SEAT,
        (
            Edit(AGEN,
                 "\t\t\tif bytes.Equal(row.GetAddress(), addr) {\n\t\t\t\treturn genesisError(CodeForbidden,",
                 "\t\t\tif bytes.Equal(row.GetAddress(), addr)"
                 " && row.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER {\n"
                 "\t\t\t\treturn genesisError(CodeForbidden,"),
            Edit(DOC,
                 "\t\t\tif strings.ToLower(row.AddressHex) == addr {",
                 "\t\t\tif strings.ToLower(row.AddressHex) == addr && row.Role != RoleBroadcaster {"),
        ),
        "both genesis validators must refuse a seat address listed as BROADCASTER (DR-06-06 supersedes DR-07-11)",
    ),
    Mutant(
        "seat-may-sign-lease-messages", SEAT,
        (
            Edit(ANTE,
                 "\tcase *authorityv1.MsgProposeChange, *authorityv1.MsgApproveChange:\n"
                 "\t\treturn true\n\tdefault:\n\t\treturn false",
                 "\tcase *authorityv1.MsgProposeChange, *authorityv1.MsgApproveChange:\n"
                 "\t\treturn true\n\tdefault:\n\t\treturn strings.HasPrefix(sdk.MsgTypeURL(msg), \"/svn.leases.v1.\")"),
            Edit(ANTE, "import (\n", "import (\n\t\"strings\"\n"),
        ),
        "a seat key must not sign any svn.leases.v1 message",
    ),
    one("seat-gate-checks-first-message-only", SEAT, ANTE,
        "\t\tfor _, msg := range tx.GetMsgs() {\n\t\t\tif !councilMessage(msg) {",
        "\t\tfor _, msg := range tx.GetMsgs()[:1] {\n\t\t\tif !councilMessage(msg) {",
        "the seat check must cover every message of the transaction, not the first only"),
    one("rotation-keeps-old-key-admitted", SEAT, ENGINE,
        "\tb.set(retiredCouncilKey(old), []byte(seatID))\n",
        "\tb.set(retiredCouncilKey(old), []byte(seatID))\n"
        "\tb.set(AdmissionKey(SeatAddress(old)), []byte{byte(typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER)})\n",
        "RotateCouncilSeat must write no admission row; the retired key must not stay admitted"),
    one("rotation-onto-submitter-allowed", SEAT, ENGINE,
        "\tif s.role(SeatAddress(newPub)) != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {\n"
        "\t\treturn fail(CodeDuplicate)\n\t}",
        "\tif s.role(SeatAddress(newPub)) == 99 {\n\t\treturn fail(CodeDuplicate)\n\t}",
        "a seat rotation onto an address that is a submitter must be DUPLICATE_ID"),
    one("service-rotation-onto-seat-allowed", SEAT, ENGINE,
        "\tif s.seatOf(newAddr) != nil {\n\t\treturn fail(CodeDuplicate)\n\t}",
        "\tif s.seatOf(newAddr) != nil && false {\n\t\treturn fail(CodeDuplicate)\n\t}",
        "RotateServiceSubmitter onto a seat address must be DUPLICATE_ID"),
    one("auth-genesis-omits-seat-accounts", SEAT, APPSTATE,
        "\t\taddrs = append(addrs, hex.EncodeToString(genesis.SeatAddress(pub)))",
        "\t\t_ = pub",
        "genesis must create an auth account for every seat address"),
    one("carried-ante-extra-admission", SEAT, APP,
        "func(ctx sdk.Context, addr []byte) authority.Admission {\n\t\treturn authorityKeeper.Admission(ctx, addr)\n",
        "func(ctx sdk.Context, addr []byte) authority.Admission {\n"
        "\t\tctx.KVStore(keys[authority.ModuleName]).Set("
        "authority.AdmissionKey(append([]byte(nil), addr[:19]...)), []byte{9})\n"
        "\t\treturn authorityKeeper.Admission(ctx, addr)\n",
        "goal 06 ante-extra-admission (B1): an extra 0x02 admission row written in the ante must fail "
        "the full authority-store compare"),
    one("carried-doc-seat-match-case-sensitive", SEAT, DOC,
        "\t\t\tif strings.ToLower(row.AddressHex) == addr {",
        "\t\t\tif row.AddressHex == addr {",
        "goal 07 doc-seat-rule-case-sensitive: genesis.Validate must match a seat address case-insensitively"),
    # Group review: goal 08 review-round mutants.
    one("m1-genesis-content-cap-off", REVIEW, LGEN,
        "\t\tif perContent[offer.GetContentId()] > MaxOpenOffersPerContent {\n",
        "\t\tif false && perContent[offer.GetContentId()] > MaxOpenOffersPerContent {\n",
        "leases ValidateGenesis must refuse more than MaxOpenOffersPerContent open offers of one content"),
    one("m2-offer-d6-off", REVIEW, LMSG,
        "\tif contentOffers >= MaxOpenOffersPerContent {\n",
        "\tif false && contentOffers >= MaxOpenOffersPerContent {\n",
        "OfferLease past MaxOpenOffersPerContent open offers of one content must be BOUNDS (D-6)"),
    one("m4-offer-d10-off", REVIEW, LMSG,
        "\tif schedulerOffers >= MaxOpenOffersPerContentPerScheduler {\n",
        "\tif false && schedulerOffers >= MaxOpenOffersPerContentPerScheduler {\n",
        "OfferLease past MaxOpenOffersPerContentPerScheduler open offers of one content by one scheduler "
        "must be BOUNDS (D-10)"),
    one("m5-end-payload-drops-term-version", REVIEW, LMSG,
        "\t\tcanonical, canonErr := endCanonical(msg.GetLeaseId(), lease.GetProviderId(), lease.GetTermVersion())\n",
        "\t\tcanonical, canonErr := endCanonical(msg.GetLeaseId(), lease.GetProviderId(), 1)\n",
        "an END signed for an earlier term version must be UNAUTHORIZED after a renewal (D-12)"),
    one("m6-accept-replay-owner-check-off", REVIEW, LMSG,
        "\t\t\tif !found || !bytes.Equal(msg.GetSigner(), k.providerAccount(ctx, lease.GetProviderId())) {\n",
        "\t\t\tif false && (!found || !bytes.Equal(msg.GetSigner(), k.providerAccount(ctx, lease.GetProviderId()))) {\n",
        "an AcceptLease replay by an ADMITTED_PROVIDER account that is not the lease provider's must be "
        "FORBIDDEN (D-13)"),
    # Group leases: capacity accounting and lease lifecycle.
    one("capacity-off-by-one", LEASES, PHANDOFF,
        "\tif reserved+amount > record.GetAdmittedCapacityBytes() {\n\t\treturn nil, msgkit.Capacity\n\t}\n",
        "\tif reserved+amount-1 > record.GetAdmittedCapacityBytes() {\n\t\treturn nil, msgkit.Capacity\n\t}\n",
        "a reservation that ends one byte above the admitted capacity must be CAPACITY at offer and accept"),
    one("end-releases-twice", LEASES, LMSG,
        "\tk.dropIndexes(ctx, lease)\n\tlease.State = leasesv1.LeaseState_LEASE_STATE_ENDED\n",
        "\t_ = k.deps.Providers.Release(ctx, lease.GetProviderId(), lease.GetPackageBytes())\n"
        "\tk.dropIndexes(ctx, lease)\n\tlease.State = leasesv1.LeaseState_LEASE_STATE_ENDED\n",
        "EndLease must release the lease's reservation exactly once"),
    one("end-held-guard-removed", LEASES, LMSG,
        "\tif !lease.GetReservationHeld() {\n\t\treturn fail(msgkit.Underflow)\n\t}\n",
        "",
        "a second EndLease (repeated ending) must be UNDERFLOW and release nothing"),
    one("expiry-releases-twice", LEASES, LEXP,
        "\tk.dropIndexes(ctx, lease)\n\tlease.State = leasesv1.LeaseState_LEASE_STATE_EXPIRED\n",
        "\t_ = k.deps.Providers.Release(ctx, lease.GetProviderId(), lease.GetPackageBytes())\n"
        "\tk.dropIndexes(ctx, lease)\n\tlease.State = leasesv1.LeaseState_LEASE_STATE_EXPIRED\n",
        "expiry must release an expiring lease's reservation exactly once"),
    one("accept-duplicate-copy-allowed", LEASES, LMSG,
        "\tif k.has(ctx, PairKey(offer.GetProviderId(), offer.GetContentId(), offer.GetContentVersion())) {\n",
        "\tif false && k.has(ctx, PairKey(offer.GetProviderId(), offer.GetContentId(), offer.GetContentVersion())) {\n",
        "AcceptLease of a second offer for a held (provider, content, version) pair must be DUPLICATE_ID "
        "(REQ-INV-001)"),
    one("offer-duplicate-copy-allowed", LEASES, LMSG,
        "\tif k.has(ctx, PairKey(msg.GetProviderId(), msg.GetContentId(), msg.GetContentVersion())) {\n",
        "\tif false && k.has(ctx, PairKey(msg.GetProviderId(), msg.GetContentId(), msg.GetContentVersion())) {\n",
        "OfferLease for a (provider, content, version) pair that a lease holds must be DUPLICATE_ID (REQ-INV-001)"),
    one("service-key-signs-acceptance", LEASES, LMSG,
        "\tif code := k.verify(ctx, offer.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,"
        " canonical, canonErr, acceptance); code != msgkit.OK {\n",
        "\tif code := k.verify(ctx, offer.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,"
        " canonical, canonErr, acceptance); code != msgkit.OK &&\n"
        "\t\t!msgkit.VerifyWithKey(acceptance.GetPublicKey(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,"
        " canonical, acceptance.GetSignature()) {\n",
        "an acceptance signed by any key it carries (a service key) must be UNAUTHORIZED; only the provider's "
        "current registered key counts (REQ-CHAIN-012)"),
    one("expiry-budget-ignored", LEASES, LEXP,
        "\titems, err := k.dueItems(ctx, now, k.limit(ctx, msgkit.LimitMaxExpirySteps))\n",
        "\titems, err := k.dueItems(ctx, now, 1<<40)\n",
        "BeginBlock expiry must process at most the effective max_expiry_steps items per block"),
    one("expiry-limit-damage-ignored", LEASES, LEXP,
        "\titems, err := k.dueItems(ctx, now, k.limit(ctx, msgkit.LimitMaxExpirySteps))\n",
        "\tbudget := msgkit.Effective(msgkit.LimitMaxExpirySteps, 0, false)\n"
        "\tfunc() {\n\t\tdefer func() { _ = recover() }()\n"
        "\t\tbudget = k.limit(ctx, msgkit.LimitMaxExpirySteps)\n\t}()\n"
        "\titems, err := k.dueItems(ctx, now, budget)\n",
        "BeginBlock expiry must fail closed on a governed max_expiry_steps that is not an 8-byte "
        "value in 1..ceiling (x/authority GovernedLimit panics), not fall back to the protocol "
        "ceiling"),
    one("expiry-merge-ignores-budget", LEASES, LEXP,
        "\tfor uint64(len(out)) < budget && (i < len(leases) || j < len(offers)) {\n",
        "\tfor i < len(leases) || j < len(offers) {\n",
        "the merge of due leases and due offers must stop at the budget, not at budget per index"),
]

BUILD_FAILURE = re.compile(r"\[(build|setup) failed\]|^# \S+$|cannot find package|undefined: ", re.M)
TOP_FAIL = re.compile(r"^--- FAIL: (\S+)", re.M)
TOP_PASS = re.compile(r"^--- PASS: (\S+)", re.M)
TOP_SKIP = re.compile(r"^--- SKIP: (\S+)", re.M)
PANIC = re.compile(r"^panic: ", re.M)


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


def validate(copy: pathlib.Path, mutants: list[Mutant]) -> list[str]:
    problems: list[str] = []
    names: set[str] = set()
    for m in mutants:
        if m.name in names:
            problems.append(f"DUPLICATE_NAME {m.name}")
        names.add(m.name)
        if m.group not in GROUPS:
            problems.append(f"UNKNOWN_GROUP {m.name} {m.group}")
        if not m.edits:
            problems.append(f"NO_EDIT {m.name}")
        problems.extend(mutate(copy, m)[1])
    return problems


def classify(rc: int, out: str) -> str:
    if rc == 0:
        return "SURVIVED"
    if BUILD_FAILURE.search(out):
        return "BUILD_FAILED"
    if TOP_FAIL.search(out) or PANIC.search(out):
        return "KILLED"
    return "UNCLEAR"


def run(copy: pathlib.Path, group: str, env: dict[str, str], timeout: int) -> tuple[str, int, str, float]:
    t0 = time.monotonic()
    try:
        p = subprocess.run(COMMANDS[group].argv(), cwd=copy, env=env, capture_output=True, text=True,
                           timeout=timeout)
    except subprocess.TimeoutExpired as exc:
        raw = exc.stdout or b""
        out = raw.decode(errors="replace") if isinstance(raw, bytes) else raw
        return "TIMEOUT", -1, out, time.monotonic() - t0
    out = p.stdout + p.stderr
    return classify(p.returncode, out), p.returncode, out, time.monotonic() - t0


def control_problems(group: str, rc: int, out: str) -> list[str]:
    problems = []
    if rc != 0:
        problems.append(f"exit={rc}")
    passed = set(TOP_PASS.findall(out))
    for name in COMMANDS[group].tests:
        if name not in passed:
            problems.append(f"{name} did not run and pass at top level")
    for name in TOP_SKIP.findall(out):
        problems.append(f"{name} skipped")
    return problems


def main() -> int:
    ap = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    ap.add_argument("--rev", default=None, help="revision to git archive (default HEAD)")
    ap.add_argument("--worktree", action="store_true",
                    help="copy the working tree (uncommitted and untracked, not ignored, files) "
                    "instead of --rev")
    ap.add_argument("--group", default="", choices=("",) + GROUPS, help="run one group only")
    ap.add_argument("--only", default="", help="comma-separated mutant names")
    ap.add_argument("--check", action="store_true", help="validate search strings only")
    ap.add_argument("--logdir", default="", help="write each command's full output here")
    ap.add_argument("--keep", action="store_true", help="keep the temporary copy and GOCACHE")
    ap.add_argument("--timeout", type=int, default=1200, help="seconds per command")
    args = ap.parse_args()

    root = repo_root()
    if args.worktree and args.rev is not None:
        print("--worktree and --rev are exclusive")
        return 2
    rev = args.rev or "HEAD"
    commit = git(root, "rev-parse", f"{rev}^{{commit}}")
    tree = git(root, "rev-parse", f"{rev}^{{tree}}")
    only = {n for n in args.only.split(",") if n}
    unknown = only - {m.name for m in MUTANTS}
    if unknown:
        print(f"unknown mutant names: {sorted(unknown)}")
        return 2
    selected = [m for m in MUTANTS if (not only or m.name in only) and (not args.group or m.group == args.group)]
    if not selected:
        print("no mutant selected")
        return 2

    work = pathlib.Path(tempfile.mkdtemp(prefix="goal08-mutants-"))
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
    groups = ", ".join(f"{g}={sum(1 for m in selected if m.group == g)}" for g in GROUPS)
    print(f"mutants {len(selected)} of {len(MUTANTS)} ({groups})")
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
        print("search strings: every one occurs exactly once")
        if args.check:
            return 0

        env = dict(os.environ, GOPROXY="off", GOCACHE=str(work / "gocache"))
        # The difftest oracle runs spec_machine.py under $SVN_SPEC_PYTHON,
        # else <repo>/.venv/bin/python, else python3. The copy has no .venv,
        # so point it at the repository's interpreter, as in the worktree.
        venv = root / ".venv" / "bin" / "python"
        if not env.get("SVN_SPEC_PYTHON") and venv.is_file():
            env["SVN_SPEC_PYTHON"] = str(venv)
        version = subprocess.run(["go", "version"], cwd=copy, capture_output=True, text=True, env=env)
        print(f"go {version.stdout.strip()} {version.stderr.strip()}".rstrip())
        print(f"SVN_SPEC_PYTHON={env.get('SVN_SPEC_PYTHON', '')}")
        used = [g for g in GROUPS if any(m.group == g for m in selected)]
        for group in used:
            print(f"command {group}: {' '.join(COMMANDS[group].argv())}")

        group_secs: dict[str, float] = {}
        for group in used:
            verdict, rc, out, secs = run(copy, group, env, args.timeout)
            group_secs[group] = group_secs.get(group, 0.0) + secs
            if logdir:
                (logdir / f"control-{group}.txt").write_text(out)
            bad_control = control_problems(group, rc, out) if verdict != "TIMEOUT" else ["timeout"]
            ran = sorted(set(TOP_PASS.findall(out)) & set(COMMANDS[group].tests))
            print(f"CONTROL {group} {'FAIL' if bad_control else 'PASS'} exit={rc} secs={secs:.1f} "
                  f"named_tests_passed={len(ran)}/{len(COMMANDS[group].tests)}", flush=True)
            if bad_control:
                for line in bad_control:
                    print(f"    {line}")
                print(out[-4000:])
                print("RESULT control failed; mutants not run")
                return 1

        tally: dict[str, int] = {}
        bad = []
        for m in selected:
            texts, problems = mutate(copy, m)
            if problems:  # validated above; a second miss means the copy changed
                print("\n".join(problems))
                return 1
            originals = {path: (copy / path).read_text() for path in texts}
            try:
                for path, text in texts.items():
                    (copy / path).write_text(text)
                verdict, rc, out, secs = run(copy, m.group, env, args.timeout)
            finally:
                for path, text in originals.items():
                    (copy / path).write_text(text)
            group_secs[m.group] = group_secs.get(m.group, 0.0) + secs
            tally[verdict] = tally.get(verdict, 0) + 1
            if logdir:
                (logdir / f"{m.name}.txt").write_text(out)
            fails = " ".join(dict.fromkeys(TOP_FAIL.findall(out)))
            panic = " panic=yes" if PANIC.search(out) else ""
            files = ",".join(dict.fromkeys(e.path for e in m.edits))
            print(f"{verdict} {m.name} group={m.group} exit={rc} secs={secs:.1f} file={files}{panic} "
                  f"failing={fails or '-'}", flush=True)
            print(f"    reason: {m.reason}", flush=True)
            if verdict != "KILLED":
                bad.append(m.name)
                print("    output tail:\n" + "\n".join("    " + line for line in out.strip().splitlines()[-25:]),
                      flush=True)
        for group in used:
            print(f"GROUP_SECS {group} {group_secs.get(group, 0.0):.1f}")
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
