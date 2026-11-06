#!/usr/bin/env python3
"""Goal 09 mutation runner (standard library only).

What it does:
1. Exports a committed revision (default HEAD) with `git archive` into a new
   temporary directory outside the repository. With --worktree it copies the
   working tree instead (see below).
2. Runs the test command of every selected group once on the unmutated copy
   (control). A control passes only when every command exits 0, every Go
   test the group names ran at top level and printed `--- PASS`, no test was
   skipped, and the group's Node test file (labels only) reported
   `# fail 0`, at least one pass, no skip or todo, and every executed test
   `ok`. A failing control stops the run before any mutant.
3. For each mutant: applies its edits (each replaces one exact search string
   in one file; the string must occur exactly once in the file as it is when
   the edit is applied), runs the group's commands in the copy, and restores
   every edited file. A mutated .py file must still compile and a mutated
   .json file must still parse, so a syntax error is never counted as a kill.

Verdicts:
- KILLED: a command exited non-zero, the Go packages built, and its output
  has a top-level `--- FAIL:` line or a `panic:` line (go test), or a
  top-level `not ok N - <test>` line for a test, not for the file (node).
- SURVIVED: every command exited zero.
- BUILD_FAILED: a Go package or test binary did not build. This is not a
  kill.
- UNCLEAR: a command exited non-zero with no build failure, no failing test
  and no panic. This is not a kill.
- TIMEOUT: a command ran longer than --timeout seconds.

Exit status is 0 only when every control passed and every mutant is KILLED.
It is non-zero when a search string is missing or not unique (checked for
all selected mutants before any test runs), when a control fails, or when
any mutant is SURVIVED, BUILD_FAILED, UNCLEAR or TIMEOUT.

Usage:
  python3 docs/evidence/goal-09/mutants.py [--rev REV | --worktree]
                                            [--group G[,G...]] [--only a,b]
                                            [--check] [--list] [--logdir DIR]
                                            [--keep] [--gocache DIR]
                                            [--timeout SECONDS]

--check validates the search strings and runs nothing. --list prints every
mutant with its group and files and runs nothing.

--worktree copies the working tree instead of exporting --rev: every file
`git ls-files --cached --others --exclude-standard` lists that exists on
disk, as it is on disk (uncommitted edits and untracked, not ignored, files
included; ignored files such as .venv are not copied). It is for dry runs of
uncommitted tests and mutants. Its output is not evidence for a committed
revision: the header names HEAD and prints `git status --porcelain`, so a
log shows what the copy held beyond HEAD. --worktree and --rev are
exclusive.

The copy runs Go with GOPROXY=off, so the module cache must already hold
the dependencies and the toolchain that go.work names. By default the copy
uses its own GOCACHE under the temporary directory, which is removed at the
end unless --keep is given. --gocache DIR uses DIR instead; the Go build
cache is content-addressed, so this changes build time only, never a
verdict. No command starts a validator: the devnet package is never tested
here, and no group uses the network.

Groups (one set of commands each, run from the root of the copy; the -run
list of each group names its intended killer tests):
- evidence: x/evidence keeper and evidencetest tests (fakes over real
  stores). Every keeper test except TestSignBytesVectors, so no mutant of
  this runner is killed by a golden byte comparison alone.
- signature: TestAlteredObservationD1, TestAuditorKeyBindingD2 and
  TestSpecIntegrityCaseIsUnauthorizedD1 only (plan 21.8: the -run list
  excludes TestSignBytesVectors). Each envelope-field mutant changes the
  signer and the verifier alike, so only a behavioural test can kill it.
- query: keeper query tests and the app query tests through ABCI.
- app: chain/app (committed transactions through FinalizeBlock, InitChain,
  App.Export) and the x/leases renewal tests.
- difftest: TestDeviationRulesAreLoadBearing and two P4 tests of
  chain/x/evidence/difftest (real keepers, spec_machine.py through
  tests/difftest/spec_oracle.py).
- auditor-net, auditor-files, auditor-cli: agents/auditor packages
  (loopback HTTPS endpoints and a fake chain RPC in unit tests only).
- labels: agents/auditor/labels and apps/web/test/evidence-labels.test.ts.

Where the mutants come from (names start with their source):
- p1-review-*: the twelve mutants M01-M12 of the P1 test-strength review
  (scratch runner, not in Git); search strings unchanged, paths from the
  repository root.
- p1-fix-*: the fourteen mutants N01-N14 of the P1 fix round.
- w0-*: the Wave 0 producer mutants: the fifteen of P2a's scratch runner
  (its "store overwrites" is the fix round's M13, the same edit, listed
  once), the review's M4 and M5, and the fix round's M1, M2, M3, M3b, M6
  and M7-M12. The review's M1, M2, M3 and M6 are the fix round's (M3 with
  the fix round's search string).
- w1b-*: the 24 Wave 1b fix-round mutants: classify 3, lazy session 3,
  sign/status 2, submission 5 (whole-file overlays in the scratch runner;
  here the edit each overlay made), labels 11.
- plan-*: plan section 21.8's groups: check order, signature, batch size,
  ids, epoch, derivation, lease, ledger, query, tail, renewal, genesis,
  producer and labels mutants not already covered by a ported mutant.
- int-*: the app wiring of goal 09 (export refusal, AUDITOR prefix genesis
  rule, the x/leases evidence reader, the DR-06-06 seat gate, the
  broadcaster relay refusal, the layout check, the business-id namespace).
- p4-*: the x/evidence adapter rules of tests/difftest/spec_oracle.py.
This runner re-runs all of them; its own output is the evidence, not the
scratch logs.
"""

from __future__ import annotations

import argparse
import io
import json
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

EVIDENCE = "evidence"
SIGNATURE = "signature"
QUERY = "query"
APP = "app"
DIFFTEST = "difftest"
AUD_NET = "auditor-net"
AUD_FILES = "auditor-files"
AUD_CLI = "auditor-cli"
LABELS = "labels"
GROUPS = (EVIDENCE, SIGNATURE, QUERY, APP, DIFFTEST, AUD_NET, AUD_FILES, AUD_CLI, LABELS)


@dataclass(frozen=True)
class Command:
    packages: tuple[str, ...]
    tests: tuple[str, ...]
    timeout: str = "15m"
    node: tuple[str, ...] = ()

    def argvs(self) -> list[list[str]]:
        run = "^(" + "|".join(self.tests) + ")$"
        out = [["go", "test", "-count=1", "-v", "-timeout", self.timeout, "-run", run, *self.packages]]
        for path in self.node:
            out.append(["node", "--experimental-strip-types", "--test", "--test-reporter", "tap", path])
        return out


EVIDENCE_TESTS = (
    # chain/x/evidence (keeper with fake read surfaces over real stores)
    "TestSubmitAuditBatchCheckOrder", "TestItemFaultsOnLastItem", "TestLadderCoversEveryCode",
    "TestTrialSnapshotDetectsEarlyWrite", "TestBatchCheckMajorOrderD6", "TestBatchAtomicD6",
    "TestDuplicateInBatchD6", "TestBatchSizeAndGovernedLimit", "TestAlteredObservationD1",
    "TestSpecIntegrityCaseIsUnauthorizedD1", "TestAuditorKeyBindingD2", "TestBroadcasterRelayForbidden",
    "TestForeignAuditorCannotTakeEvidenceIDsD17", "TestEpochRuleD3", "TestSchemaVersionD5",
    "TestDuplicateIDs", "TestBusinessIDTakenFirst", "TestExpiryRangeEdges", "TestIdempotentReplay",
    "TestSameObservationNewIDIsAccepted", "TestEvidenceKindHasNoProofValues",
    "TestEvidenceItemHasNoRejectFlag", "TestProviderReceiptNotOnChainD8",
    "TestEvidenceLifetimeBoundD9", "TestLeaseContentBindingD10", "TestLeaseNotHeldD11",
    "TestLeaseDeadlinePassedD12", "TestEvidenceExpiryWithinLeaseD18", "TestContentWithdrawnD13",
    "TestAuditorEpochCapD15", "TestReportedStateD4", "TestNoEvidenceRevivesAnything",
    "TestRecordedEvidenceAfterEndWithdrawExpiry", "TestRecordSizeBound",
    "TestAuditLeavesOtherStoresUntouched", "TestStoreInvariantViolationsFailClosed",
    "TestRecordFieldsAndDerivedState", "TestEvidenceEvents", "TestGenesisValidateImportExport",
    "TestGenesisBatchConsistency", "TestEvidenceGenesisRoundTrip", "TestModuleGenesis",
    "TestModule", "TestServicesOnRealRouters", "TestServiceDescriptorsMatchTheProtocol",
    "TestUnaryHandlerInterceptor", "TestRunMigrationsAtCurrentVersionIsANoOp",
    "TestNoMigrationFromScaffoldLayout", "TestMigrationTable", "TestNewKeeperRejectsMissingDependencies",
    "TestDeterministicSources", "TestKeeperImports", "TestNoMerkleCode", "TestNoForbiddenNames",
    "TestReadsWithoutBlockTimeFailClosed", "TestEvidenceQuery", "TestEvidenceQueryResponseSizeBound",
    "TestSignPayloadNeverFails", "TestAttestationPayloadRejectsInvalidUTF8", "TestSignPayloadSizeBound",
    "TestAttestationPreimageIsNotASignDoc", "TestAuditorAddressMatchesSDK", "TestKindName",
    "TestEpochAt", "TestEnvelopeMatchesSchema",
    "FuzzSubmitAuditBatchDecode", "FuzzAuditStateSequence", "FuzzEvidenceGenesisJSON",
    "FuzzEvidencePageKey",
    # chain/x/evidence/evidencetest
    "TestCheckInvariantsDetectsEachViolation", "TestPrefixCollisionKeys", "TestStoreSnapshot",
)

AUDITOR_NET_TESTS = (
    # agents/auditor/policy
    "TestParseEndpointRules", "TestURLLengthBoundary", "TestParseEndpointValues",
    "TestAllowlistEntryRules", "TestVet", "TestRefusalTextHasNoAddress", "TestBoundsMatchProtocolFile",
    "TestSpecialPurposeTable", "TestUnmappingCases", "TestGoBuiltinsAreNotEnough",
    "TestClassifyRejectsInvalidAndZoned", "TestTableMatchesRegistrySnapshot",
    # agents/auditor/fetch
    "TestFetchWholePackageOverLoopbackEntry", "TestLoopbackURLNeedsItsExactEntry",
    "TestEntryNameResolvingElsewhereRefused", "TestMixedResolutionRefusedAsAWhole",
    "TestNoSecondLookup", "TestGlobalDestinationThroughSeam", "TestRedirectNotFollowed",
    "TestHTTPStatusOutcomes", "TestCompressedResponseRefused", "TestOversizedBody",
    "TestShortBodyVersusReset", "TestSlowBodyTimesOutInsideTheBudget", "TestTLSFailures",
    "TestRanges", "TestOversizedHeadersAreAProtocolError", "TestConnectFailed", "TestDNSFailures",
    "TestURLRulesRefuseBeforeResolving", "TestRequestValidation", "TestSinkErrorIsReturned",
    "TestPackagePath", "TestTransportSettings", "TestControlHookRefusesOtherAddresses",
    "TestNewValidatesOptions", "TestSeamNotReachableThroughOptions", "TestLoadCAFile",
    "TestFetchIgnoresProxyEnvironment",
    # agents/auditor/config
    "TestExampleConfig", "TestDefaults", "TestAuditorDefaults", "TestRejects",
    "TestConfigHasNoBypass", "TestLoadBoundsTheFile",
)

AUDITOR_FILES_TESTS = (
    # agents/auditor/verify
    "TestParseManifestValidFixture", "TestParseManifestRejectsGoal02InvalidFixtures",
    "TestParseManifestStrict", "TestManifestDigestIsCanonical", "TestReadManifestFile",
    "TestNewLayout", "TestSplitterZeroLengthSegments", "TestSelectIsDeterministicPerSeed",
    "TestNewSeed", "TestInputProblemsFetchNothing", "TestWholePackageMatches",
    "TestWholePackageCorruptedSegment", "TestWholePackageLengthMismatch", "TestSampledSegments",
    "TestFreshSeedPerAudit", "TestAuditRequestValidation", "TestProducerBytesRead",
    # agents/auditor/store
    "TestContentAddressedReports", "TestNamedObjects", "TestConcurrentPutsCreateOnce",
    "TestLinkAfterAnotherWriterNeverReplaces", "TestNoPartialFileAfterCrash", "TestModes",
    "TestOpenRefusesUnsafeRoots", "TestSymlinkedObjectRefused", "TestIndexAppendOnly",
    "TestScanIndexRefusesInvalidLines", "TestScanIndexWithoutIndex", "TestNoDeleteOperation",
    # agents/auditor/batch
    "TestEvidenceIDDerivation", "TestBatchAndBusinessIDs", "TestNewItemDigests", "TestExpiry",
    "TestRoundOpen", "TestCheckLease", "TestCheckContent", "TestBuildOneBatchPerProvider",
    "TestSignVerifiesWithTheChainCheck", "TestEnvelopesAreTheSignedEnvelopes", "TestPersistAndLoad",
    "TestRebuild", "TestRunFormat", "TestSummaryFormat", "TestProducerMatchesChainSignVectors",
    "TestGoldenEnvelopeFixtures",
)

AUDITOR_CLI_TESTS = (
    # agents/auditor (the CLI; loopback endpoints, fake chain RPC)
    "TestRunNeverOpensKeyPath", "TestRunClassifiesLoopbackOutcomes",
    "TestRunTLSAndTimeoutAreIncomplete", "TestRunBudgetEndsRetriesIncomplete",
    "TestContentWithdrawnFetchesNothing", "TestManifestCheckedBeforeResolution",
    "TestEmptySampleStillVetsEndpoint", "TestCapabilityManifestEndpoint", "TestReportLimitations",
    "TestRunSignSubmitReviewUnit", "TestSignRefusals", "TestCLIUsageAndKeygen",
    "TestSignAndStatusKeepToTheirResources", "TestCommandsAgainstTheFakeNode",
    "TestNoInsecureTLSOrProxyInSource", "TestFetchtestOnlyInTests", "TestTestSeamOnlySetInTests",
    # agents/auditor/classify
    "TestEveryFetchKindIsMapped", "TestEveryPolicyRuleIsMapped", "TestEveryVerifyStateIsMapped",
    "TestEveryReasonHasAResult", "TestFailReasonsAreTheRuling", "TestOutcomeTable",
    "TestRangeOutcomeTable", "TestObservation", "TestFailedSegmentsAreBounded", "TestReasons",
    # agents/auditor/chainclient
    "TestAcknowledgementIsNotCommit", "TestCommittedResultIsReadBack", "TestDifferentRecordIsIDTaken",
    "TestRetryQueriesBeforeResign", "TestReplayLeadsToANewBusinessID", "TestStaleEpochIsWarnedAndFails",
    "TestSubmittedBatchInAPassedEpochIsNotRebroadcast", "TestCheckTxRejectionIsNotAccepted",
    "TestRebroadcastWhileWaitingStaysSubmitted", "TestUnrecordedSubmissionIsQueried",
    "TestStoredSubmissionMustCarryItsBatch", "TestLostBroadcastAnswerIsSubmitted",
    "TestBatchFailureIsolation", "TestBuildTx", "TestReads", "TestSVNCode", "TestHTTPClient",
    "TestChaintestOnlyInTests",
    # agents/auditor/review
    "TestConsistentPassAndFail", "TestNotOnChainOnlyFromANode", "TestPayloadUnavailable",
    "TestChecksumMismatch", "TestReportInvalid", "TestSignatureInvalid", "TestEnvelopeMismatch",
    "TestRecordMismatch", "TestSelfConsistentFalseReportIsConsistent", "TestIndexFallback",
)

LABEL_TESTS = (
    "TestCatalogFileIsCanonical", "TestPinnedSentences", "TestSentenceShape", "TestModesAreExplicit",
    "TestStatesMatchChainNames", "TestRequiredLimitations", "TestAcceptanceIsAPromise",
    "TestStateTextsLimitTheClaim", "TestRenewalSentenceIsCatalogWide", "TestClaimLimitMatchesEnvelope",
    "TestLimitationsFitReportBound", "TestDenylistRulesAreWellFormed", "TestNoDeniedClaims",
    "TestDenylistScannerDetects", "TestEmbeddedReturnsIndependentValues", "TestMissingFromChecker",
    "TestDocsContainLimitations", "TestEmbeddedCatalogParses", "TestParseRejects", "TestParseTextBound",
)

# One set of commands per group, run from the root of the copy.
COMMANDS: dict[str, Command] = {
    EVIDENCE: Command(("./chain/x/evidence/", "./chain/x/evidence/evidencetest/"), EVIDENCE_TESTS),
    SIGNATURE: Command(
        ("./chain/x/evidence/",),
        ("TestAlteredObservationD1", "TestAuditorKeyBindingD2", "TestSpecIntegrityCaseIsUnauthorizedD1"),
    ),
    QUERY: Command(
        ("./chain/x/evidence/", "./chain/app/"),
        (
            "TestEvidenceQuery", "TestReportedStateD4", "TestReadsWithoutBlockTimeFailClosed",
            "TestEvidenceQueryResponseSizeBound", "TestEvidenceQueryTimeThroughABCI",
            "TestEvidenceQueryGasAndBounds", "TestAuditFlowReplayAndQueries",
        ),
    ),
    APP: Command(
        ("./chain/x/leases/", "./chain/app/"),
        (
            # chain/x/leases (keeper, fake evidence reader)
            "TestRenewCheckOrder", "TestEvidenceCannotExtendLease",
            # chain/app (real keepers, signed transactions, FinalizeBlock)
            "TestExportRefusesEvidenceState", "TestGenesisRefusesAuditorPrefixCollision",
            "TestLaunchGenesisRejectsEvidence", "TestEvidenceGenesisRoundTripFromCommittedState",
            "TestNoEvidenceRenews", "TestEvidenceCannotExtendOrReviveLease",
            "TestSeatEvidenceRefusedForTheSeatRule", "TestSecpAuditorAccountCannotBind",
            "TestRejectedAuditMessagesLeaveTheFullState", "TestAuditRetryScenarios",
            "TestDishonestAdmittedAuditorInApp", "TestAuditFlowReplayAndQueries",
            "TestAppRefusesScaffoldLayout", "TestEvidenceMessagesDecode", "TestEvidenceEventSchemas",
            "TestNoPrivateDataInEvidenceStateOrEvents", "TestAuditWithoutDevnetParams",
        ),
    ),
    DIFFTEST: Command(
        ("./chain/x/evidence/difftest/",),
        ("TestDeviationRulesAreLoadBearing", "TestEvidenceUsesNoExpiryBudget", "TestNothingRevives"),
        timeout="20m",
    ),
    AUD_NET: Command(
        ("./agents/auditor/policy/", "./agents/auditor/fetch/", "./agents/auditor/config/"),
        AUDITOR_NET_TESTS,
    ),
    AUD_FILES: Command(
        ("./agents/auditor/verify/", "./agents/auditor/store/", "./agents/auditor/batch/"),
        AUDITOR_FILES_TESTS,
    ),
    AUD_CLI: Command(
        (
            "./agents/auditor/", "./agents/auditor/classify/", "./agents/auditor/chainclient/",
            "./agents/auditor/review/",
        ),
        AUDITOR_CLI_TESTS,
    ),
    LABELS: Command(
        ("./agents/auditor/labels/",), LABEL_TESTS, node=("apps/web/test/evidence-labels.test.ts",)
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


EMSG = "chain/x/evidence/msg_server.go"
ESIGN = "chain/x/evidence/signdoc.go"
EGEN = "chain/x/evidence/genesis.go"
EKEEP = "chain/x/evidence/keeper.go"
EQUERY = "chain/x/evidence/query.go"
ESTORE = "chain/x/evidence/store.go"
ETEST = "chain/x/evidence/evidencetest/evidencetest.go"
LMSG = "chain/x/leases/msg_server.go"
APPGO = "chain/app/app.go"
ANTE = "chain/app/ante.go"
APPSTATE = "chain/app/state.go"
ORACLE = "tests/difftest/spec_oracle.py"
AUD = "agents/auditor/"
FETCH = AUD + "fetch/fetch.go"
DIAL = AUD + "fetch/dial.go"
POLICY = AUD + "policy/policy.go"
PCLASSIFY = AUD + "policy/classify.go"
PTABLE = AUD + "policy/table.go"
CONFIG = AUD + "config/config.go"
STORE = AUD + "store/store.go"
VAUDIT = AUD + "verify/audit.go"
VLAYOUT = AUD + "verify/layout.go"
BATCH = AUD + "batch/batch.go"
CLASSIFY = AUD + "classify/classify.go"
MAIN = AUD + "main.go"
SUBMIT = AUD + "chainclient/submit.go"
REVIEW = AUD + "review/review.go"
LJSON = AUD + "labels/evidence-labels.json"
LRULES = AUD + "labels/testdata/label-rules.json"
LGO = AUD + "labels/labels.go"
LGOTEST = AUD + "labels/labels_test.go"
LTS = "apps/web/src/evidence-labels.ts"
LNODETEST = "apps/web/test/evidence-labels.test.ts"

FIRST = "items[:min(1, len(items))]"
LAST_RECORDED = "It does not show that the lease is held or the content is available now."


def recorded_sentence(name: str, extra: str, reason: str) -> Mutant:
    """W1b label mutant: append a denied claim to the RECORDED state text in
    both catalogs and in both tests' pinned lists, so that only the denylist
    rules can kill it."""
    q = json.dumps(extra)
    return Mutant(
        name, LABELS,
        (
            Edit(LJSON, LAST_RECORDED + '"', LAST_RECORDED + " " + extra + '"'),
            Edit(LTS, LAST_RECORDED + '"', LAST_RECORDED + " " + extra + '"'),
            Edit(LGOTEST, '\t\t"' + LAST_RECORDED + '",\n\t}},', '\t\t"' + LAST_RECORDED + '",\n\t\t' + q + ",\n\t}},"),
            Edit(LNODETEST, '"' + LAST_RECORDED + '"]],', '"' + LAST_RECORDED + '", ' + q + "]],"),
        ),
        reason,
    )


MUTANTS: list[Mutant] = [
    # ---------------------------------------------------------------- P1 review (M01-M12)
    one("p1-review-m01-step2-presence-first-item-only", EVIDENCE, EMSG,
        '\tfor _, item := range items {\n\t\tif item == nil || item.GetEvidenceId() == ""',
        "\tfor _, item := range " + FIRST + ' {\n\t\tif item == nil || item.GetEvidenceId() == ""',
        "step 2 presence must cover every item, not the first only (MALFORMED)"),
    one("p1-review-m02-step9-id-pattern-first-item-only", EVIDENCE, EMSG,
        "\tfor _, item := range items {\n"
        "\t\tfor _, id := range []string{item.GetEvidenceId(), item.GetLeaseId(), item.GetContentId()} {",
        "\tfor _, item := range " + FIRST + " {\n"
        "\t\tfor _, id := range []string{item.GetEvidenceId(), item.GetLeaseId(), item.GetContentId()} {",
        "step 9 id pattern must cover every item (BOUNDS)"),
    one("p1-review-m03-step16-stored-evidence-id-first-item-only", EVIDENCE, EMSG,
        "if seen[id] || k.has(ctx, RecordKey(id)) {",
        "if seen[id] || (len(seen) == 0 && k.has(ctx, RecordKey(id))) {",
        "step 16 must refuse a stored evidence id at any item position (DUPLICATE_ID)"),
    one("p1-review-m04-step17-expired-first-item-only", EVIDENCE, EMSG,
        "\tfor _, item := range items {\n\t\tif item.GetExpiryUnixSeconds() <= now {",
        "\tfor _, item := range " + FIRST + " {\n\t\tif item.GetExpiryUnixSeconds() <= now {",
        "step 17 expiry <= committed time must cover every item (EXPIRED)"),
    one("p1-review-m05-step14-upper-bound-off-by-one", EVIDENCE, EMSG,
        "t < genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {\n\t\t\treturn fail(msgkit.Bounds)",
        "t < genesis.UnixSecondsMin || t >= genesis.UnixSecondsMax {\n\t\t\treturn fail(msgkit.Bounds)",
        "step 14 accepts expiry 253402300799 (BOUNDS only above it)"),
    one("p1-review-m06-step14-lower-bound-off-by-one", EVIDENCE, EMSG,
        "t < genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {\n\t\t\treturn fail(msgkit.Bounds)",
        "t <= genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {\n\t\t\treturn fail(msgkit.Bounds)",
        "step 14 accepts expiry 1 (BOUNDS only below it)"),
    one("p1-review-m07-genesis-expiry-window-upper-tightened", EVIDENCE, EGEN,
        "expiry >= epochStart+EpochSeconds+MaxEvidenceLifetimeSeconds {",
        "expiry >= epochStart+EpochSeconds+MaxEvidenceLifetimeSeconds-1 {",
        "ValidateGenesis must accept a record at the last second of its epoch's expiry window"),
    one("p1-review-m08-genesis-expiry-window-lower-tightened", EVIDENCE, EGEN,
        "if expiry <= epochStart || expiry",
        "if expiry <= epochStart+1 || expiry",
        "ValidateGenesis must accept a record whose expiry is the second after its epoch start"),
    one("p1-review-m09-invariant-I6-drop-content-version", EVIDENCE, ETEST,
        "if lease.GetContentId() != record.GetContentId() || "
        "lease.GetContentVersion() != record.GetContentVersion() {",
        "if lease.GetContentId() != record.GetContentId() {",
        "CheckInvariants I6 must compare the content version with the lease's"),
    one("p1-review-m10-invariant-I2-drop-32-record-cap", EVIDENCE, ETEST,
        "if batch.count > int(genesis.MaxEvidenceBatch) {",
        "if false && batch.count > int(genesis.MaxEvidenceBatch) {",
        "CheckInvariants I2 must refuse a batch of more than 32 records"),
    one("p1-review-m11-invariant-I7-drop-expiry-upper", EVIDENCE, ETEST,
        "if t := r.GetExpiryUnixSeconds(); t < genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {",
        "if t := r.GetExpiryUnixSeconds(); t < genesis.UnixSecondsMin {",
        "CheckInvariants I7 must refuse an expiry above 253402300799"),
    one("p1-review-m12-window-count-not-accumulated-per-item", EVIDENCE, EMSG,
        "k.saveWindow(ctx, msg.GetSigner(), current, windowCount+uint64(len(items)))",
        "k.saveWindow(ctx, msg.GetSigner(), current, windowCount+1)",
        "the auditor window must count items, not batches (D-15)"),
    # ---------------------------------------------------------------- P1 fix round (N01-N14)
    one("p1-fix-n01-genesis-batch-signer-key-rule-dropped", EVIDENCE, EGEN,
        "if !bytes.Equal(batch.pub, record.GetSignerPubkey()) || batch.epoch != record.GetEpoch() {",
        "if (false && !bytes.Equal(batch.pub, record.GetSignerPubkey())) || batch.epoch != record.GetEpoch() {",
        "ValidateGenesis must refuse a batch whose records carry two signer keys"),
    one("p1-fix-n02-invariant-I2-signer-key-dropped", EVIDENCE, ETEST,
        "if !bytes.Equal(batch.pub, record.GetSignerPubkey()) || batch.epoch != record.GetEpoch() {",
        "if batch.epoch != record.GetEpoch() {",
        "CheckInvariants I2 must refuse a batch whose records carry two signer keys"),
    one("p1-fix-n03-envelope-utf8-check-dropped", EVIDENCE, ESIGN,
        "\t\tif !utf8.ValidString(field.value) {\n\t\t\treturn nil,",
        "\t\tif false && !utf8.ValidString(field.value) {\n\t\t\treturn nil,",
        "AuditEnvelope must refuse an id or key_id that is not UTF-8 (no SVN-JCS-1 form)"),
    one("p1-fix-n04-batch-id-utf8-check-dropped", EVIDENCE, ESIGN,
        "if !utf8.ValidString(msg.GetBatchId()) {",
        "if false && !utf8.ValidString(msg.GetBatchId()) {",
        "AuditCanonical must refuse a batch_id that is not UTF-8"),
    one("p1-fix-n05-zero-block-time-fail-open", EVIDENCE, EQUERY,
        "if now < genesis.UnixSecondsMin {\n\t\treturn nil, ErrNoBlockTime",
        "if false && now < genesis.UnixSecondsMin {\n\t\treturn nil, ErrNoBlockTime",
        "a read without a block time must fail closed (ErrNoBlockTime), never report the stored state"),
    one("p1-fix-n06-step16-stored-id-last-item-skipped", EVIDENCE, EMSG,
        "if seen[id] || k.has(ctx, RecordKey(id)) {",
        "if seen[id] || (len(seen) < len(items)-1 && k.has(ctx, RecordKey(id))) {",
        "step 16 must refuse a stored evidence id in the last item"),
    one("p1-fix-n07-step2-presence-last-item-skipped", EVIDENCE, EMSG,
        '\tfor _, item := range items {\n\t\tif item == nil || item.GetEvidenceId() == ""',
        '\tfor _, item := range items[:max(0, len(items)-1)] {\n\t\tif item == nil || item.GetEvidenceId() == ""',
        "step 2 presence must cover the last item"),
    one("p1-fix-n08-step10-kind-first-item-only", EVIDENCE, EMSG,
        "\tfor _, item := range items {\n\t\tswitch item.GetKind() {",
        "\tfor _, item := range " + FIRST + " {\n\t\tswitch item.GetKind() {",
        "step 10 kind must cover every item (MALFORMED)"),
    one("p1-fix-n09-step12-attestation-first-item-only", EVIDENCE, EMSG,
        "\tfor _, item := range items {\n\t\tif !item.GetAttestation() {",
        "\tfor _, item := range " + FIRST + " {\n\t\tif !item.GetAttestation() {",
        "step 12 attestation must cover every item (MALFORMED)"),
    one("p1-fix-n10-step18-lease-first-item-only", EVIDENCE, EMSG,
        "\t\tlease, found := k.deps.Leases.Lease(ctx, item.GetLeaseId())\n\t\tif !found {",
        "\t\tlease, found := k.deps.Leases.Lease(ctx, item.GetLeaseId())\n\t\tif !found && i == 0 {",
        "step 18 lease NOT_FOUND must cover every item"),
    one("p1-fix-n11-invariant-I8-cap-clause-dropped", EVIDENCE, ETEST,
        "if epoch != want.epoch || count != want.count || count > evidence.MaxEvidencePerAuditorPerEpoch {",
        "if epoch != want.epoch || count != want.count {",
        "CheckInvariants I8 must refuse a window count above MaxEvidencePerAuditorPerEpoch"),
    one("p1-fix-n12-business-row-id-check-dropped", EVIDENCE, ETEST,
        "if !msgkit.ValidID(key[1:]) || len(rows[key]) != msgkit.DigestBytes {",
        "if len(rows[key]) != msgkit.DigestBytes {",
        "CheckInvariants must refuse a business-ledger row whose id is outside the id pattern"),
    one("p1-fix-n13-export-zero-time-fail-open", EVIDENCE, EGEN,
        "report, err := reported(record, now)",
        "report, err := reported(record, max(now, 1))",
        "ExportGenesis without a block time must fail closed, never export a state read at time 1"),
    one("p1-fix-n14-ladder-t4-first-item-only", EVIDENCE, EMSG,
        "if item.GetContentId() != leases[i].GetContentId() || "
        "item.GetContentVersion() != leases[i].GetContentVersion() {",
        "if i == 0 && (item.GetContentId() != leases[i].GetContentId() || "
        "item.GetContentVersion() != leases[i].GetContentVersion()) {",
        "T4 (D-10) lease/content binding must cover every item"),
    # ---------------------------------------------------------------- plan 21.8: check order
    one("plan-check-order-role-dropped", EVIDENCE, EMSG,
        "typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR); code != msgkit.OK {\n\t\treturn fail(code)",
        "typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR); code != msgkit.OK && false {\n\t\treturn fail(code)",
        "step 3: a signer with no role is UNAUTHORIZED and another role FORBIDDEN"),
    one("plan-check-order-schema-dropped", EVIDENCE, EMSG,
        "\tswitch msg.GetSchemaVersion() {\n\tcase 0:\n\t\treturn fail(msgkit.Malformed)\n"
        "\tcase SchemaVersion:\n\tdefault:\n\t\treturn fail(msgkit.OutOfRange)\n\t}\n",
        "",
        "step 4: schema_version 0 is MALFORMED and any value but 1 OUT_OF_RANGE (D-5)"),
    one("plan-check-order-business-id-pattern-dropped", EVIDENCE, EMSG,
        "\tif code := msgkit.CheckID(msg.GetBusinessId()); code != msgkit.OK {\n\t\treturn fail(code), nil\n\t}\n",
        "",
        "step 15: a business_id outside the id pattern is BOUNDS"),
    # ---------------------------------------------------------------- plan 21.8: signature
    one("plan-sig-verify-skipped", SIGNATURE, EMSG,
        "\tif code := VerifyAuditSignature(msg); code != msgkit.OK {",
        "\tif code := VerifyAuditSignature(msg); code != msgkit.OK && false {",
        "steps 6-8: a batch altered after signing is UNAUTHORIZED (D-1)"),
    one("plan-sig-key-binding-dropped", SIGNATURE, ESIGN,
        "\tif address == nil || !bytes.Equal(address, msg.GetSigner()) {",
        "\tif address == nil || !bytes.Equal(address, address) {",
        "step 7: the signature key must derive the outer signer's address (D-2, Q1)"),
    one("plan-sig-envelope-omits-observation", SIGNATURE, ESIGN,
        '\t\t"observation_sha256":  msgkit.Hex(observation),\n',
        '\t\t"observation_sha256":  msgkit.Hex(observation[:0]),\n',
        "the signed envelope must carry the observation digest"),
    one("plan-sig-envelope-omits-epoch", SIGNATURE, ESIGN,
        '\t\t"epoch":               epoch,\n',
        '\t\t"epoch":               epoch * 0,\n',
        "the signed envelope must carry the epoch"),
    one("plan-sig-batch-omits-signer", SIGNATURE, ESIGN,
        '\t\t"signer":    msgkit.Hex(msg.GetSigner()),\n',
        "",
        "the signed batch object must carry the outer signer"),
    one("plan-sig-batch-omits-batch-id", SIGNATURE, ESIGN,
        '\t\t"batch_id":  msg.GetBatchId(),\n',
        "",
        "the signed batch object must carry the batch_id"),
    one("plan-sig-batch-omits-item-order", SIGNATURE, ESIGN,
        "\t\tenvelopes = append(envelopes, envelope)\n",
        "\t\tenvelopes = append(envelopes, envelope)\n"
        "\t\tif n := len(envelopes); n > 1 && "
        'envelopes[n-2].(map[string]any)["evidence_id"].(string) > envelope["evidence_id"].(string) {\n'
        "\t\t\tenvelopes[n-2], envelopes[n-1] = envelopes[n-1], envelopes[n-2]\n\t\t}\n",
        "the signed batch object must keep the message's item order"),
    # ---------------------------------------------------------------- plan 21.8: batch size
    one("plan-batch-count-check-dropped", EVIDENCE, EMSG,
        "\tif len(items) < 1 || uint64(len(items)) > k.limit(ctx, msgkit.LimitMaxEvidenceBatch) {",
        "\tif false && (len(items) < 1 || uint64(len(items)) > k.limit(ctx, msgkit.LimitMaxEvidenceBatch)) {",
        "step 11: an empty batch or one above max_evidence_batch is BOUNDS"),
    one("plan-batch-cap-ge", EVIDENCE, EMSG,
        "uint64(len(items)) > k.limit(ctx, msgkit.LimitMaxEvidenceBatch) {",
        "uint64(len(items)) >= k.limit(ctx, msgkit.LimitMaxEvidenceBatch) {",
        "step 11: a batch of exactly max_evidence_batch items is accepted"),
    one("plan-batch-governed-limit-ignored", EVIDENCE, EMSG,
        "uint64(len(items)) > k.limit(ctx, msgkit.LimitMaxEvidenceBatch) {",
        "uint64(len(items)) > msgkit.Effective(msgkit.LimitMaxEvidenceBatch, 0, false) {",
        "step 11 uses the governed max_evidence_batch, not the protocol ceiling only"),
    # ---------------------------------------------------------------- plan 21.8: ids
    one("plan-ids-in-batch-duplicate-dropped", EVIDENCE, EMSG,
        "if seen[id] || k.has(ctx, RecordKey(id)) {",
        "if k.has(ctx, RecordKey(id)) {",
        "step 16: an evidence id repeated in one batch is DUPLICATE_ID (D-6)"),
    one("plan-ids-stored-id-dropped", EVIDENCE, EMSG,
        "if seen[id] || k.has(ctx, RecordKey(id)) {",
        "if seen[id] {",
        "step 16: a stored evidence id is DUPLICATE_ID"),
    one("plan-ids-batch-marker-not-written", EVIDENCE, EMSG,
        "\tk.markBatch(ctx, msg.GetBatchId())\n",
        "",
        "an accepted batch must mark its batch_id used, so a second batch with it is DUPLICATE_ID"),
    one("plan-ids-d17-batch-prefix-dropped", EVIDENCE, EMSG,
        "\tif !strings.HasPrefix(msg.GetBatchId(), prefix) {",
        "\tif false && !strings.HasPrefix(msg.GetBatchId(), prefix) {",
        "T1 (D-17): a batch_id without the signer's prefix is FORBIDDEN"),
    one("plan-ids-d17-item-prefix-dropped", EVIDENCE, EMSG,
        "\t\tif !strings.HasPrefix(item.GetEvidenceId(), prefix) {",
        "\t\tif false && !strings.HasPrefix(item.GetEvidenceId(), prefix) {",
        "T1 (D-17): an evidence id without the signer's prefix is FORBIDDEN"),
    # ---------------------------------------------------------------- plan 21.8: epoch, derivation
    one("plan-epoch-previous-round", EVIDENCE, EMSG,
        "\tif msg.GetEpoch() != current {",
        "\tif msg.GetEpoch() != current-1 {",
        "step 13: the epoch must be EpochAt(committed time) (D-3)"),
    one("plan-derivation-inverted", EVIDENCE, EMSG,
        "\t\tif matchesManifest(observations[i], leases[i]) {",
        "\t\tif !matchesManifest(observations[i], leases[i]) {",
        "DEC-010: the manifest digest records RECORDED, any other digest REJECTED"),
    one("plan-derivation-ignores-algorithm", EVIDENCE, EMSG,
        "\treturn manifest.GetAlgorithm() == typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 &&\n"
        "\t\tbytes.Equal(observation, manifest.GetValue())",
        "\treturn bytes.Equal(observation, manifest.GetValue())",
        "DEC-010 compares the digest algorithm as well as the value"),
    # ---------------------------------------------------------------- plan 21.8: lease, ledger
    one("plan-lease-not-found-dropped", EVIDENCE, EMSG,
        "\t\tif !found {\n\t\t\treturn fail(msgkit.NotFound)\n\t\t}\n\t\tleases[i] = lease",
        "\t\tif false && !found {\n\t\t\treturn fail(msgkit.NotFound)\n\t\t}\n\t\tleases[i] = lease",
        "step 18: an item naming no lease is NOT_FOUND"),
    one("plan-lease-write-before-last-check", EVIDENCE, EMSG,
        "\twindowEpoch, windowCount := k.mustLoadWindow(ctx, msg.GetSigner())\n",
        "\tk.markBatch(ctx, msg.GetBatchId())\n"
        "\twindowEpoch, windowCount := k.mustLoadWindow(ctx, msg.GetSigner())\n",
        "no write before the last check (T9): a batch refused at T9 leaves the store untouched"),
    one("plan-ledger-commit-skipped", EVIDENCE, EMSG,
        "\tk.ledger.Commit(ctx, msg.GetBusinessId(), digest)\n",
        "\t_ = digest\n",
        "an accepted batch commits its business id, so a replay is idempotent and another payload REPLAY"),
    # ---------------------------------------------------------------- plan 21.8: query
    one("plan-query-lease-index-not-written", QUERY, ESTORE,
        "\tstore.Set(LeaseIndexKey(record.GetLeaseId(), record.GetEvidenceId()), marker())\n",
        "",
        "every record has its lease index row, so the lease_id query lists it"),
    one("plan-query-page-budget-off-by-one", QUERY, EQUERY,
        "msgkit.Paginate(k.kv(ctx), prefix, key, limit, func(",
        "msgkit.Paginate(k.kv(ctx), prefix, key, limit+1, func(",
        "a lease_id page holds at most limit records"),
    one("plan-query-reported-state-is-stored", QUERY, EKEEP,
        "\tif state == evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED && record.GetExpiryUnixSeconds() <= now {",
        "\tif false && state == evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED && "
        "record.GetExpiryUnixSeconds() <= now {",
        "a RECORDED record past its expiry reads EXPIRED (D-4)"),
    one("plan-query-reported-state-expires-rejected", QUERY, EKEEP,
        "\tif state == evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED && record.GetExpiryUnixSeconds() <= now {",
        "\tif state != evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED && record.GetExpiryUnixSeconds() <= now {",
        "a REJECTED record never expires"),
    one("plan-query-expiry-boundary-strict", QUERY, EKEEP,
        "\tif state == evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED && record.GetExpiryUnixSeconds() <= now {",
        "\tif state == evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED && record.GetExpiryUnixSeconds() < now {",
        "a RECORDED record reads EXPIRED at its expiry second (expiry <= now)"),
    # ---------------------------------------------------------------- plan 21.8: tail checks
    one("plan-tail-d8-dropped", EVIDENCE, EMSG,
        "\t\tif item.GetKind() == evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT {",
        "\t\tif false && item.GetKind() == evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT {",
        "T2 (D-8): a PROVIDER_RECEIPT item is FORBIDDEN"),
    one("plan-tail-d9-dropped", EVIDENCE, EMSG,
        "\t\tif item.GetExpiryUnixSeconds()-MaxEvidenceLifetimeSeconds > now {",
        "\t\tif false && item.GetExpiryUnixSeconds()-MaxEvidenceLifetimeSeconds > now {",
        "T3 (D-9): an expiry more than MaxEvidenceLifetimeSeconds ahead is BOUNDS"),
    one("plan-tail-d10-dropped", EVIDENCE, EMSG,
        "\t\tif item.GetContentId() != leases[i].GetContentId() || "
        "item.GetContentVersion() != leases[i].GetContentVersion() {",
        "\t\tif false && (item.GetContentId() != leases[i].GetContentId() || "
        "item.GetContentVersion() != leases[i].GetContentVersion()) {",
        "T4 (D-10): an item whose content differs from its lease is CONFLICT"),
    one("plan-tail-d11-dropped", EVIDENCE, EMSG,
        "\t\tif !held(leases[i]) {",
        "\t\tif false && !held(leases[i]) {",
        "T5 (D-11): an item whose lease is not held is CONFLICT"),
    one("plan-tail-d12-dropped", EVIDENCE, EMSG,
        "\t\tif leases[i].GetDeadlineUnixSeconds() <= now {",
        "\t\tif false && leases[i].GetDeadlineUnixSeconds() <= now {",
        "T6 (D-12): an item whose held lease is past its deadline is EXPIRED"),
    one("plan-tail-d18-dropped", EVIDENCE, EMSG,
        "\t\tif item.GetExpiryUnixSeconds() > leases[i].GetDeadlineUnixSeconds() {",
        "\t\tif false && item.GetExpiryUnixSeconds() > leases[i].GetDeadlineUnixSeconds() {",
        "T7 (D-18): an item expiring after its lease's deadline is CONFLICT"),
    one("plan-tail-d13-dropped", EVIDENCE, EMSG,
        "\t\tif code := k.deps.Content.CheckNotWithdrawn(ctx, leases[i].GetContentId()); code != msgkit.OK {",
        "\t\tif code := k.deps.Content.CheckNotWithdrawn(ctx, leases[i].GetContentId()); "
        "code != msgkit.OK && false {",
        "T8 (D-13): an item of withdrawn content is WITHDRAWN"),
    one("plan-tail-d15-dropped", EVIDENCE, EMSG,
        "\tif windowCount > MaxEvidencePerAuditorPerEpoch || "
        "uint64(len(items)) > MaxEvidencePerAuditorPerEpoch-windowCount {",
        "\tif false && (windowCount > MaxEvidencePerAuditorPerEpoch || "
        "uint64(len(items)) > MaxEvidencePerAuditorPerEpoch-windowCount) {",
        "T9 (D-15): more than MaxEvidencePerAuditorPerEpoch items of one auditor in one epoch is BOUNDS"),
    one("plan-tail-window-reset-wrong-epoch", EVIDENCE, EMSG,
        "\tif windowEpoch != current {\n\t\twindowCount = 0\n\t}",
        "\tif windowEpoch != current-1 {\n\t\twindowCount = 0\n\t}",
        "the auditor window resets only when the stored epoch is not the current one"),
    # ---------------------------------------------------------------- plan 21.8: genesis
    one("plan-genesis-cross-module-check-dropped", EVIDENCE, EGEN,
        "\t\tif err := k.checkGenesisLease(ctx, record, current); err != nil {",
        "\t\tif err := k.checkGenesisLease(ctx, record, current); err != nil && false {",
        "InitGenesis must check every record against its lease (I5, I6, I13, epoch)"),
    one("plan-genesis-export-writes-stored-state", EVIDENCE, EGEN,
        "\t\tstate.Records = append(state.Records, report)\n",
        "\t\t_ = report\n\t\tstate.Records = append(state.Records, record)\n",
        "ExportGenesis must export the reported state (a RECORDED record past expiry as EXPIRED)"),
    # ---------------------------------------------------------------- plan 21.8: renewal
    one("plan-renewal-evidence-falls-through", APP, LMSG,
        "\t\t\treturn fail(msgkit.Expired)\n\t\t}\n\t\treturn fail(msgkit.Forbidden)\n\t}\n",
        "\t\t\treturn fail(msgkit.Expired)\n\t\t}\n\t}\n",
        "a renewal that names live evidence is FORBIDDEN; evidence never renews or extends a lease"),
    one("plan-renewal-evidence-expiry-strict", APP, LMSG,
        "\t\tif expired || evidenceExpiry <= now {",
        "\t\tif expired || evidenceExpiry < now {",
        "a renewal naming evidence at its expiry second is EXPIRED"),
    one("plan-renewal-evidence-not-found", APP, EKEEP,
        "record.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED, true\n",
        "record.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED, false\n",
        "RenewalEvidence must report a stored record as found (the renewal is FORBIDDEN or EXPIRED, "
        "not NOT_FOUND)"),
    # ---------------------------------------------------------------- INT app wiring
    one("int-export-evidence-refusal-off", APP, APPGO,
        "\tif err := app.evidenceStoreEmpty(ctx); err != nil {",
        "\tif err := app.evidenceStoreEmpty(ctx); err != nil && false {",
        "App.Export must refuse evidence state before the module manager export (DR-09-18)"),
    one("int-genesis-auditor-prefix-rule-off", APP, APPSTATE,
        "\t\tif prev, taken := owners[prefix]; taken {",
        "\t\tif prev, taken := owners[prefix]; taken && false {",
        "genesis must refuse two AUDITOR submitters that share an evidence id prefix (DR-09-30)"),
    one("int-leases-evidence-reader-nil", APP, APPGO,
        "\t\tEvidence:  evidenceKeeper,\n",
        "",
        "the leases keeper the app runs must read evidence (a renewal naming evidence is FORBIDDEN or "
        "EXPIRED, not NOT_FOUND)"),
    one("int-ante-seat-may-sign-audits", APP, ANTE,
        "\t\t\tif !councilMessage(msg) {",
        '\t\t\tif !councilMessage(msg) && sdk.MsgTypeURL(msg) != "/svn.evidence.v1.MsgSubmitAuditBatch" {',
        "DR-06-06: a council seat key must not sign MsgSubmitAuditBatch (ante FORBIDDEN)"),
    one("int-evidence-role-accepts-broadcaster", APP, EMSG,
        "typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR); code != msgkit.OK {\n\t\treturn fail(code)",
        "typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR); code != msgkit.OK &&\n"
        "\t\tk.deps.Roles.Role(ctx, msg.GetSigner()) != typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER {\n"
        "\t\treturn fail(code)",
        "a BROADCASTER relaying an auditor batch is FORBIDDEN at step 3 (owner ruling Q1: no relay)"),
    one("int-layout-check-omits-evidence", APP, APPGO,
        "\t\t{ModuleEvidence, evidence.ConsensusVersion},\n",
        "",
        "the app must refuse an evidence store whose layout is not this binary's"),
    one("int-evidence-outside-business-namespace", APP, APPGO,
        "\tpeers.Add(evidence.ModuleName, evidenceKeeper)\n",
        "",
        "evidence business ids share the one protocol namespace with the other modules"),
    # ---------------------------------------------------------------- P4 oracle adapter rules
    one("p4-oracle-d17-off", DIFFTEST, ORACLE,
        '        if self.on("evidence D-17") and not (',
        '        if False and self.on("evidence D-17") and not (',
        "the adapter applies evidence D-17 (T1) after every spec check"),
    one("p4-oracle-d13-withdrawn-ignored", DIFFTEST, ORACLE,
        '            if content["state"] == "WITHDRAWN":',
        "            if False:",
        "the adapter applies evidence D-13 (T8) to withdrawn content"),
    one("p4-oracle-d15-window-not-counted", DIFFTEST, ORACLE,
        '            self.windows[spec["signer"]] = (epoch, held + 1)',
        '            self.windows[spec["signer"]] = (epoch, held)',
        "the adapter counts each accepted item in the auditor's window (evidence D-15)"),
    one("p4-oracle-d3-epoch-not-advanced", DIFFTEST, ORACLE,
        '                trial["epoch"] = epoch',
        "                pass",
        "at a Block the adapter sets the spec epoch from header time (evidence D-3)"),
    one("p4-oracle-d4-evidence-not-swapped", DIFFTEST, ORACLE,
        '                trial["evidence"] = {}',
        "                pass",
        "at a Block the adapter hides evidence from ExpireDue (evidence D-4: no evidence sweep)"),
    one("p4-oracle-d6-first-item-only", DIFFTEST, ORACLE,
        '            spec["item_count"] = len(items)',
        "            pass",
        "a batch of N != 1 items maps to one spec step that carries item_count (evidence D-6)"),
    # ---------------------------------------------------------------- Wave 0: P2a scratch runner
    one("w0-p2a-proxy-from-env", AUD_NET, FETCH,
        "\t\tProxy:       nil,", "\t\tProxy:       http.ProxyFromEnvironment,",
        "the fetch transport must ignore proxy environment variables"),
    one("w0-p2a-follow-redirects", AUD_NET, FETCH,
        "\t\t\treturn http.ErrUseLastResponse\n", "\t\t\treturn nil\n",
        "the fetch client must not follow redirects"),
    one("w0-p2a-no-encoding-check", AUD_NET, FETCH,
        '\tif !identityOnly(resp.Header.Values("Content-Encoding")) {',
        '\tif false && !identityOnly(resp.Header.Values("Content-Encoding")) {',
        "a compressed response is refused"),
    one("w0-p2a-no-extra-byte-check", AUD_NET, FETCH,
        '\t\t\tout.Kind, out.Detail = TooLarge, "body continues past the expected size"\n\t\t\treturn out, nil',
        "\t\t\tout.Kind = OK\n\t\t\treturn out, nil",
        "a body longer than the expected size is TOO_LARGE (size cap)"),
    one("w0-p2a-skip-denied-addresses", AUD_NET, POLICY,
        "\t\t\treturn nil, &Refusal{Rule: RuleAddress, Row: v.Deny}", "\t\t\tcontinue",
        "a denied address refuses the whole resolved set"),
    one("w0-p2a-no-unmapping", AUD_NET, PCLASSIFY,
        "\tcase a.Is4In6():", "\tcase false && a.Is4In6():",
        "an IPv4-mapped address is unmapped before the table lookup"),
    one("w0-p2a-no-embedded-check", AUD_NET, PCLASSIFY,
        "\t\t\tv.Deny = denyRow(IPv4Rows, v.Embedded)", "\t\t\t_ = v.Embedded",
        "an embedded IPv4 address (NAT64, 6to4) is checked against the table"),
    one("w0-p2a-no-2000-3-rule", AUD_NET, PCLASSIFY,
        "\t\tif v.Deny == nil && !GlobalUnicast6.Contains(a) {",
        "\t\tif false && v.Deny == nil && !GlobalUnicast6.Contains(a) {",
        "IPv6 outside 2000::/3 is not global"),
    one("w0-p2a-control-hook-permits-all", AUD_NET, DIAL,
        "\t\t\treturn errSocketMismatch\n", "\t\t\treturn nil\n",
        "the dial control hook refuses an address other than the vetted one"),
    one("w0-p2a-entry-ignores-provider", AUD_NET, POLICY,
        "\twant := Entry{ProviderID: providerID, Host: ep.Host, IP: a, Port: ep.Port}\n"
        "\tfor _, e := range p.entries {\n\t\tif e == want {",
        "\twant := Entry{ProviderID: providerID, Host: ep.Host, IP: a, Port: ep.Port}\n"
        "\tfor _, e := range p.entries {\n\t\te.ProviderID = want.ProviderID\n\t\tif e == want {",
        "an allowlist entry matches its provider_id exactly"),
    one("w0-p2a-link-local-entry-allowed", AUD_NET, POLICY,
        "\treturn a.IsLinkLocalUnicast() || ", "\treturn ",
        "an allowlist entry may not name a link-local address"),
    one("w0-p2a-second-lookup-per-dial", AUD_NET, FETCH,
        "\tfor _, target := range s.targets {",
        "\tif res, ok := s.client.opts.Resolver.(interface {\n"
        "\t\tLookupNetIP(context.Context, string, string) ([]netip.Addr, error)\n"
        "\t}); ok && !s.endpoint.Literal.IsValid() {\n"
        '\t\t_, _ = res.LookupNetIP(ctx, "ip", s.endpoint.Host)\n\t}\n'
        "\tfor _, target := range s.targets {",
        "a fetch resolves the host once and dials only the vetted addresses (no second lookup)"),
    one("w0-store-publish-renames", AUD_FILES, STORE,
        "\tif err := os.Link(tmp, final); err != nil {", "\tif err := os.Rename(tmp, final); err != nil {",
        "a stored object is created once and never replaced (P2a 'store overwrites' = fix round M13)"),
    one("w0-p2a-manifest-mismatch-ignored", AUD_FILES, VAUDIT,
        "\tcase m.SHA256 != l.ManifestSHA256:", "\tcase false:",
        "a manifest whose digest is not the lease's is an input problem; nothing is fetched"),
    one("w0-p2a-config-unknown-keys", AUD_NET, CONFIG,
        '\t\t\t\tif !ok {\n\t\t\t\t\treturn invalid("%s: unknown key %q", path, key)\n\t\t\t\t}',
        "\t\t\t\tif !ok {\n\t\t\t\t\tcontinue\n\t\t\t\t}",
        "the config refuses unknown keys (no bypass field can be added silently)"),
    # ---------------------------------------------------------------- Wave 0: review M4, M5
    one("w0-review-m4-global-any-port", AUD_NET, POLICY,
        "\t\tcase v.Global && ep.Port == DefaultPort:", "\t\tcase v.Global:",
        "a global destination without an allowlist entry is permitted on port 443 only"),
    one("w0-review-m5-package-bytes-not-compared", AUD_FILES, VAUDIT,
        "\tcase m.PackageBytes != l.PackageBytes:\n\t\treturn \"manifest package_bytes is not the lease's\"",
        '\tcase false:\n\t\treturn ""',
        "a manifest whose package_bytes is not the lease's is an input problem"),
    # ---------------------------------------------------------------- Wave 0: fix round
    one("w0-fix-m1-content-range-total-ignored", AUD_NET, FETCH,
        'if got := resp.Header.Values("Content-Range"); len(got) != 1 || got[0] != want {',
        'if got := resp.Header.Values("Content-Range"); len(got) != 1 || '
        '!strings.HasPrefix(got[0], want[:strings.LastIndex(want, "/")+1]) {',
        "a range response must carry the exact Content-Range, total included"),
    one("w0-fix-m2-first-content-encoding-line-only", AUD_NET, FETCH,
        'if !identityOnly(resp.Header.Values("Content-Encoding")) {',
        'if !identityOnly(resp.Header.Values("Content-Encoding")'
        '[:min(1, len(resp.Header.Values("Content-Encoding")))]) {',
        "every Content-Encoding header line is checked"),
    one("w0-fix-m3-unexpected-eof-is-length-mismatch", AUD_NET, FETCH,
        "case errors.Is(err, io.EOF) && chunked(resp):",
        "case (errors.Is(err, io.EOF) && chunked(resp)) || errors.Is(err, io.ErrUnexpectedEOF):",
        "a connection closed inside a declared length is CONNECTION_RESET, not LENGTH_MISMATCH"),
    one("w0-fix-m3b-close-delimited-eof-is-length-mismatch", AUD_NET, FETCH,
        "case errors.Is(err, io.EOF) && chunked(resp):",
        "case errors.Is(err, io.EOF):",
        "a close-delimited body that ends early is CONNECTION_RESET (authenticated framing only)"),
    one("w0-fix-m6-only-301-302-redirect", AUD_NET, FETCH,
        "case code >= 300 && code <= 399:", "case code == 301 || code == 302:",
        "every 3xx is a redirect outcome"),
    one("w0-fix-m7-zero-length-segment-digest-unchecked", AUD_FILES, VLAYOUT,
        "if s.ByteCount == 0 && s.SHA256 != emptySHA256 {", "if false {",
        "a zero-length segment must carry the SHA-256 of empty input (DR-09-21)"),
    one("w0-fix-m8-append-after-torn-index-line", AUD_FILES, STORE,
        "if last[0] != '\\n' {", "if false {",
        "the index refuses an append after a torn last line"),
    one("w0-fix-m9-index-lines-not-validated", AUD_FILES, STORE,
        "\t\tif !validIndexLine(line) {\n"
        '\t\t\treturn fmt.Errorf("%w: index line is not one JSON object", ErrCorrupt)',
        "\t\tif false {\n"
        '\t\t\treturn fmt.Errorf("%w: index line is not one JSON object", ErrCorrupt)',
        "every scanned index line must be one JSON object"),
    one("w0-fix-m10-ipv6-entry-space-unrestricted", AUD_NET, POLICY,
        "if e.IP.Is6() && !inEntryIPv6(e.IP) {", "if false {",
        "an IPv6 allowlist ip must be ::1, unique-local, in 64:ff9b::/96, or in 2000::/3"),
    one("w0-fix-m11-only-all-digit-last-label-refused", AUD_NET, POLICY,
        "if c := labels[len(labels)-1][0]; c >= '0' && c <= '9' {",
        'if strings.Trim(labels[len(labels)-1], "0123456789") == "" {',
        "a host whose last label starts with a digit is refused"),
    one("w0-fix-m12-production-code-sets-seed-source", AUD_CLI, VAUDIT,
        "func mismatch(m *Manifest, l Lease) string {",
        "func withSeed(r Request, src io.Reader) Request {\n\tr.seedSource = src\n\treturn r\n}\n\n"
        "func mismatch(m *Manifest, l Lease) string {",
        "only test files may set the sample seed source (source scan)"),
    # ---------------------------------------------------------------- Wave 1b fix round: classify
    one("w1b-classify-m1-no-range-416", AUD_CLI, CLASSIFY,
        "\tcase ranged && (code == http.StatusRequestedRangeNotSatisfiable || "
        "code == http.StatusNotImplemented):\n\t\treturn classOf(RangeUnsupported)\n",
        "",
        "a sampled request answered 416 or 501 is INCOMPLETE RANGE_UNSUPPORTED (DR-09-09)"),
    one("w1b-classify-m2-no-408", AUD_CLI, CLASSIFY,
        "\tcase code == http.StatusRequestTimeout:\n\t\treturn classOf(Timeout)\n",
        "",
        "a 408 is TIMEOUT, not an HTTP status FAIL"),
    one("w1b-classify-m3-whole-package-ranged", AUD_CLI, CLASSIFY,
        "\tclassOfFetch := Outcome\n", "\tclassOfFetch := RangeOutcome\n",
        "a whole-package request answered 416 is not a range outcome"),
    # ---------------------------------------------------------------- Wave 1b fix round: lazy session
    one("w1b-session-m4-no-open-without-requests", AUD_CLI, MAIN,
        "\t\t\t// empty) still needs an endpoint that passes the policy.\n\t\t\tsession.open(ctx)\n",
        "\t\t\t// empty) still needs an endpoint that passes the policy.\n",
        "an audit that made no request still vets its endpoint"),
    one("w1b-session-m5-eager-open", AUD_CLI, MAIN,
        "\tdefer session.Close()\n\treq := verify.Request{",
        "\tdefer session.Close()\n\tsession.open(ctx)\n\treq := verify.Request{",
        "the manifest is checked before the endpoint is resolved (no lookup for an input problem)"),
    one("w1b-session-m6-refusal-ignored", AUD_CLI, MAIN,
        "if out := session.refused; out != nil {", "if out := session.refused; out != nil && false {",
        "a policy refusal of the endpoint decides the audit"),
    # ---------------------------------------------------------------- Wave 1b fix round: sign/status
    one("w1b-sign-m7-sign-reads-header", AUD_CLI, MAIN,
        "\tout, err := sign(cfg, st, key, *runID, time.Now().Unix())\n",
        "\tif chain, err := newChain(cfg); err == nil {\n\t\t_, _ = chain.Header(context.Background())\n\t}\n"
        "\tout, err := sign(cfg, st, key, *runID, time.Now().Unix())\n",
        "sign uses no network"),
    one("w1b-status-m8-status-loads-key", AUD_CLI, MAIN,
        "\ts := &chainclient.Submitter{Client: chain, Store: st}\n"
        "\treturn eachBatch(st, ids, stdout, func(b *batch.Batch, _ batch.Summary) "
        "(chainclient.Result, error) {\n\t\treturn s.Status(",
        "\tif _, err := loadKey(cfg); err != nil {\n\t\treturn 0, err\n\t}\n"
        "\ts := &chainclient.Submitter{Client: chain, Store: st}\n"
        "\treturn eachBatch(st, ids, stdout, func(b *batch.Batch, _ batch.Summary) "
        "(chainclient.Result, error) {\n\t\treturn s.Status(",
        "status never opens the key"),
    # ---------------------------------------------------------------- Wave 1b fix round: submission
    one("w1b-submit-m9-rebroadcast-not-accepted", AUD_CLI, SUBMIT,
        "\t\t\tif res.State == Submitted {\n\t\t\t\t// An earlier submission is still waiting",
        "\t\t\tif res.State == Submitted && false {\n\t\t\t\t// An earlier submission is still waiting",
        "a refused rebroadcast while an earlier submission waits stays SUBMITTED"),
    one("w1b-submit-m10-bytes-after-broadcast", AUD_CLI, SUBMIT,
        "\tname := SubmissionName(b.ID(), n)\n"
        '\tif err := s.Store.Put(store.Batches, name, ".pb", raw); err != nil {\n'
        '\t\treturn Submission{}, nil, fmt.Errorf("chainclient: store transaction: %w", err)\n\t}\n'
        "\ta, berr := s.Client.Broadcast(ctx, raw)\n",
        "\tname := SubmissionName(b.ID(), n)\n"
        "\ta, berr := s.Client.Broadcast(ctx, raw)\n"
        '\tif err := s.Store.Put(store.Batches, name, ".pb", raw); err != nil {\n'
        '\t\treturn Submission{}, nil, fmt.Errorf("chainclient: store transaction: %w", err)\n\t}\n',
        "the transaction bytes are stored before the broadcast"),
    one("w1b-submit-m11-unrecorded-submission-ignored", AUD_CLI, SUBMIT,
        '\t\traw, err := s.Store.Get(store.Batches, name, ".json")\n\t\tif errors.Is(err, store.ErrNotFound) {\n',
        '\t\traw, err := s.Store.Get(store.Batches, name, ".json")\n\t\tif errors.Is(err, store.ErrNotFound) {\n'
        "\t\t\tbreak\n",
        "a submission with bytes but no record is queried, not ignored"),
    one("w1b-submit-m12-unrecorded-any-message", AUD_CLI, SUBMIT,
        "if want := batch.WithBusinessAttempt(b.Msg, attempt); proto.Equal(msg, want) {",
        "if want := batch.WithBusinessAttempt(b.Msg, attempt); proto.Equal(msg, want) || msg != nil {",
        "an unrecorded submission must carry this batch's message"),
    one("w1b-submit-m13-record-without-bytes-ok", AUD_CLI, SUBMIT,
        '\t\t\t\treturn nil, fmt.Errorf("chainclient: submission %d has a record without transaction bytes", n)',
        "\t\t\t\tbreak",
        "a submission record without transaction bytes is an error"),
    # ---------------------------------------------------------------- Wave 1b fix round: labels
    recorded_sentence("w1b-labels-verifies-stores",
                      "The chain verifies that the provider stores the content.",
                      "no catalog string may claim the chain verifies storage"),
    recorded_sentence("w1b-labels-enum-storage-proof", "This is a STORAGE_PROOF.",
                      "no catalog string may name a storage proof (enum spelling)"),
    recorded_sentence("w1b-labels-camel-storage-proof", "A storageProof was recorded.",
                      "no catalog string may name a storage proof (camel case)"),
    recorded_sentence("w1b-labels-extends-revives",
                      "A recorded attestation extends the lease and revives expired hosting.",
                      "no catalog string may claim evidence extends or revives a lease"),
    recorded_sentence("w1b-labels-honest-accurate",
                      "The auditor is honest and this attestation is accurate.",
                      "no catalog string may claim the auditor is honest or the attestation accurate"),
    one("w1b-labels-drop-audited-network", LABELS, LRULES,
        '    "audited network",\n', "",
        "the shared denylist keeps 'audited network'"),
    one("w1b-labels-drop-unlawful", LABELS, LRULES,
        '    "unlawful",\n', "",
        "the shared denylist keeps 'unlawful'"),
    one("w1b-labels-ts-extra-export", LABELS, LTS,
        "export type EvidenceStateKey = keyof typeof EVIDENCE_LABELS.states;\n",
        "export type EvidenceStateKey = keyof typeof EVIDENCE_LABELS.states;\n"
        '\nexport const EXTRA_COPY = "Verified storage.";\n',
        "the TypeScript module exports only the catalog"),
    one("w1b-labels-parse-no-exact-keys", LABELS, LGO,
        "\tif err := exactKeys(json.NewDecoder(bytes.NewReader(data)), reflect.TypeOf(c)); err != nil {",
        "\tif err := exactKeys(json.NewDecoder(bytes.NewReader(data)), reflect.TypeOf(c)); err != nil && false {",
        "Parse refuses unknown or missing keys"),
    one("w1b-labels-go-pinned-only", LABELS, LGOTEST,
        '{"limitations.0", []string{"Auditors can lie."}}',
        '{"limitations.0", []string{"Auditors may lie."}}',
        "the Go test's pinned list is checked against the catalog"),
    one("w1b-labels-node-pinned-only", LABELS, LNODETEST,
        '["limitations.0", ["Auditors can lie."]]',
        '["limitations.0", ["Auditors may lie."]]',
        "the Node test's pinned list is checked against the catalog"),
    # ---------------------------------------------------------------- plan 21.8: producer
    one("plan-producer-cgnat-row-dropped", AUD_NET, PTABLE,
        '\trow("100.64.0.0/10", "Shared Address Space", ReachFalse, "IANA"),\n',
        "",
        "the pinned special-purpose table keeps 100.64.0.0/10 (CGNAT shared address space)"),
    one("plan-producer-allowlist-accepts-cidr", AUD_NET, CONFIG,
        "\t\tip, err := netip.ParseAddr(e.IP)\n",
        '\t\tip, err := netip.ParseAddr(strings.SplitN(e.IP, "/", 2)[0])\n',
        "an allowlist ip is one address; a CIDR is refused"),
    one("plan-producer-sample-seed-constant", AUD_FILES, VLAYOUT,
        "\tif r == nil {\n\t\tr = rand.Reader\n\t}\n",
        "\tif r == nil {\n\t\t_ = rand.Reader\n\t\treturn [SeedBytes]byte{7}, nil\n\t}\n",
        "every audit draws a fresh sample seed"),
    one("plan-producer-pass-fail-digest-swapped", AUD_FILES, BATCH,
        "\t\tit.Observation = a.ManifestSHA256\n\tcase report.ResultFail:\n\t\tit.Observation = sum\n",
        "\t\tit.Observation = sum\n\tcase report.ResultFail:\n\t\tit.Observation = a.ManifestSHA256\n",
        "PASS reports the manifest digest and FAIL the report's SHA-256"),
    one("plan-producer-epoch-guard-off", AUD_FILES, BATCH,
        "\tif headerTime >= end || now >= end {",
        "\tif false && (headerTime >= end || now >= end) {",
        "no signing within the epoch guard before a round boundary"),
    one("plan-producer-run-opens-key", AUD_CLI, MAIN,
        "\tctx, cancel := context.WithTimeout(ctx, p.cfg.RunBudget)\n\tdefer cancel()\n"
        "\theader, err := p.chain.Header(ctx)\n",
        "\tctx, cancel := context.WithTimeout(ctx, p.cfg.RunBudget)\n\tdefer cancel()\n"
        "\tif _, err := loadKey(p.cfg); err != nil {\n\t\treturn runOutput{}, err\n\t}\n"
        "\theader, err := p.chain.Header(ctx)\n",
        "run never opens the key path"),
    one("plan-producer-reviewer-skips-checksum", AUD_CLI, REVIEW,
        '\tif v, d := checksum(in.EvidenceID, rec, p); v != "" {',
        '\tif v, d := checksum(in.EvidenceID, rec, p); v != "" && false {',
        "the reviewer reports CHECKSUM_MISMATCH for a report whose bytes do not hash to its name"),
    one("plan-producer-reviewer-skips-envelope", AUD_CLI, REVIEW,
        '\tif v, d := envelopes(msg, index, rec, p.envelope); v != "" {',
        '\tif v, d := envelopes(msg, index, rec, p.envelope); v != "" && false {',
        "the reviewer reports ENVELOPE_MISMATCH for an envelope other than the signed one"),
    one("plan-producer-status-recorded-on-ack", AUD_CLI, SUBMIT,
        "\tcase waiting:\n\t\tres.State = Submitted\n",
        "\tcase waiting:\n\t\tres.State = Committed\n",
        "an acknowledgement is not a commit: a waiting submission is SUBMITTED"),
    one("plan-producer-id-taken-comparison-dropped", AUD_CLI, SUBMIT,
        "\t\t\tif matches(rec, b.Msg, it) {",
        "\t\t\tif true || matches(rec, b.Msg, it) {",
        "a record that differs from the batch's item is ID_TAKEN"),
    # ---------------------------------------------------------------- plan 21.8: labels
    Mutant(
        "plan-labels-sentence-removed", LABELS,
        (
            Edit(LJSON, "about one lease. It is an attestation. It is not proof of storage.\"",
                 "about one lease. It is an attestation.\""),
            Edit(LTS, "about one lease. It is an attestation. It is not proof of storage.\"",
                 "about one lease. It is an attestation.\""),
        ),
        "the auditor attestation mode text keeps 'It is not proof of storage.' in both catalogs",
    ),
]

BUILD_FAILURE = re.compile(r"\[(build|setup) failed\]|^# \S+$|cannot find package|undefined: ", re.M)
TOP_FAIL = re.compile(r"^--- FAIL: (\S+)", re.M)
TOP_PASS = re.compile(r"^--- PASS: (\S+)", re.M)
TOP_SKIP = re.compile(r"^--- SKIP: (\S+)", re.M)
PANIC = re.compile(r"^panic: ", re.M)
NODE_RESULT = re.compile(r"^(not ok|ok) \d+ - (.+?)(?: # (SKIP|TODO)\b.*)?$", re.M)
NODE_FILE_SUFFIXES = (".js", ".mjs", ".cjs", ".ts", ".mts", ".cts")


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


def syntax_problem(path: str, text: str) -> str:
    """A mutated Python or JSON file must still compile or parse, so that a
    syntax error is never counted as a kill. Returns "" when it does."""
    try:
        if path.endswith(".py"):
            compile(text, path, "exec")
        elif path.endswith(".json"):
            json.loads(text)
    except (SyntaxError, ValueError) as exc:
        return f"{type(exc).__name__}: {exc}"
    return ""


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
    for path, text in texts.items():
        if detail := syntax_problem(path, text):
            problems.append(f"SYNTAX {m.name} {path} {detail}")
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


def node_results(out: str) -> list[tuple[bool, str, str]]:
    """Top-level TAP results of executed tests: (ok, name, directive). The
    file-level result node prints for the test file itself is left out."""
    results = []
    for status, name, directive in NODE_RESULT.findall(out):
        if name.strip().endswith(NODE_FILE_SUFFIXES):
            continue
        results.append((status == "ok", name.strip(), directive))
    return results


def classify_go(rc: int, out: str) -> str:
    if rc == 0:
        return "SURVIVED"
    if BUILD_FAILURE.search(out):
        return "BUILD_FAILED"
    if TOP_FAIL.search(out) or PANIC.search(out):
        return "KILLED"
    return "UNCLEAR"


def classify_node(rc: int, out: str) -> str:
    if rc == 0:
        return "SURVIVED"
    if any(not ok for ok, _, _ in node_results(out)):
        return "KILLED"
    return "UNCLEAR"


def combine(verdicts: list[str]) -> str:
    for verdict in ("BUILD_FAILED", "KILLED", "TIMEOUT", "UNCLEAR"):
        if verdict in verdicts:
            return verdict
    return "SURVIVED"


@dataclass
class Outcome:
    verdict: str
    codes: list[int]
    out: str
    secs: float


def run(copy: pathlib.Path, group: str, env: dict[str, str], timeout: int) -> Outcome:
    t0 = time.monotonic()
    verdicts: list[str] = []
    codes: list[int] = []
    parts: list[str] = []
    for argv in COMMANDS[group].argvs():
        parts.append(f"=== COMMAND {' '.join(argv)}\n")
        try:
            p = subprocess.run(argv, cwd=copy, env=env, capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired as exc:
            raw = exc.stdout or b""
            parts.append(raw.decode(errors="replace") if isinstance(raw, bytes) else raw)
            verdicts.append("TIMEOUT")
            codes.append(-1)
            continue
        out = p.stdout + p.stderr
        parts.append(out)
        codes.append(p.returncode)
        verdicts.append(classify_node(p.returncode, out) if argv[0] == "node" else classify_go(p.returncode, out))
    return Outcome(combine(verdicts), codes, "".join(parts), time.monotonic() - t0)


def control_problems(group: str, o: Outcome) -> list[str]:
    problems = [f"command {i} exit={rc}" for i, rc in enumerate(o.codes) if rc != 0]
    if o.verdict == "TIMEOUT":
        problems.append("timeout")
    passed = set(TOP_PASS.findall(o.out))
    for name in COMMANDS[group].tests:
        if name not in passed:
            problems.append(f"{name} did not run and pass at top level")
    for name in TOP_SKIP.findall(o.out):
        problems.append(f"{name} skipped")
    if COMMANDS[group].node:
        results = node_results(o.out)
        if not results:
            problems.append("node: no executed test")
        for ok, name, directive in results:
            if not ok or directive:
                problems.append(f"node: {name} {'not ok' if not ok else directive}")
        if not re.search(r"^# fail 0$", o.out, re.M) or not re.search(r"^# pass [1-9]", o.out, re.M):
            problems.append("node: summary is not '# fail 0' with at least one pass")
    return problems


def failing(out: str) -> str:
    names = list(dict.fromkeys(TOP_FAIL.findall(out)))
    names += [f"node:{name!r}" for ok, name, _ in node_results(out) if not ok]
    return " ".join(names)


def main() -> int:
    ap = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    ap.add_argument("--rev", default=None, help="revision to git archive (default HEAD)")
    ap.add_argument("--worktree", action="store_true",
                    help="copy the working tree (uncommitted and untracked, not ignored, files) "
                    "instead of --rev")
    ap.add_argument("--group", default="", help="comma-separated groups to run (default: all)")
    ap.add_argument("--only", default="", help="comma-separated mutant names")
    ap.add_argument("--check", action="store_true", help="validate search strings only")
    ap.add_argument("--list", action="store_true", help="list the mutants only")
    ap.add_argument("--logdir", default="", help="write each command's full output here")
    ap.add_argument("--keep", action="store_true", help="keep the temporary copy and GOCACHE")
    ap.add_argument("--gocache", default="", help="GOCACHE to use instead of a fresh one in the copy")
    ap.add_argument("--timeout", type=int, default=1200, help="seconds per command")
    args = ap.parse_args()

    if args.list:
        for m in MUTANTS:
            files = ",".join(dict.fromkeys(e.path for e in m.edits))
            print(f"{m.group:13} {m.name} {files}")
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

    work = pathlib.Path(tempfile.mkdtemp(prefix="goal09-mutants-"))
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
        print("search strings: every one occurs exactly once")
        if args.check:
            return 0

        gocache = pathlib.Path(args.gocache).resolve() if args.gocache else work / "gocache"
        env = dict(os.environ, GOPROXY="off", GOCACHE=str(gocache))
        # The difftest oracle runs spec_machine.py under $SVN_SPEC_PYTHON,
        # else <repo>/.venv/bin/python, else python3. The copy has no .venv,
        # so point it at the repository's interpreter, as in the worktree.
        venv = root / ".venv" / "bin" / "python"
        if not env.get("SVN_SPEC_PYTHON") and venv.is_file():
            env["SVN_SPEC_PYTHON"] = str(venv)
        version = subprocess.run(["go", "version"], cwd=copy, capture_output=True, text=True, env=env)
        print(f"go {version.stdout.strip()} {version.stderr.strip()}".rstrip())
        used = [g for g in GROUPS if any(m.group == g for m in selected)]
        if any(COMMANDS[g].node for g in used):
            nv = subprocess.run(["node", "--version"], capture_output=True, text=True, env=env)
            print(f"node {nv.stdout.strip()} {nv.stderr.strip()}".rstrip())
        print(f"SVN_SPEC_PYTHON={env.get('SVN_SPEC_PYTHON', '')}")
        print(f"GOCACHE={gocache}{' (given)' if args.gocache else ' (fresh, in the copy)'}")
        for group in used:
            for argv in COMMANDS[group].argvs():
                print(f"command {group}: {' '.join(argv)}")

        group_secs: dict[str, float] = {}
        for group in used:
            o = run(copy, group, env, args.timeout)
            group_secs[group] = group_secs.get(group, 0.0) + o.secs
            if logdir:
                (logdir / f"control-{group}.txt").write_text(o.out)
            bad_control = control_problems(group, o)
            ran = sorted(set(TOP_PASS.findall(o.out)) & set(COMMANDS[group].tests))
            node = f" node_tests_ok={sum(1 for ok, _, _ in node_results(o.out) if ok)}" if COMMANDS[group].node else ""
            print(f"CONTROL {group} {'FAIL' if bad_control else 'PASS'} exit={o.codes} secs={o.secs:.1f} "
                  f"named_tests_passed={len(ran)}/{len(COMMANDS[group].tests)}{node}", flush=True)
            if bad_control:
                for line in bad_control:
                    print(f"    {line}")
                print(o.out[-4000:])
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
                o = run(copy, m.group, env, args.timeout)
            finally:
                for path, text in originals.items():
                    (copy / path).write_text(text)
            group_secs[m.group] = group_secs.get(m.group, 0.0) + o.secs
            tally[o.verdict] = tally.get(o.verdict, 0) + 1
            if logdir:
                (logdir / f"{m.name}.txt").write_text(o.out)
            panic = " panic=yes" if PANIC.search(o.out) else ""
            files = ",".join(dict.fromkeys(e.path for e in m.edits))
            print(f"{o.verdict} {m.name} group={m.group} exit={o.codes} secs={o.secs:.1f} file={files}{panic} "
                  f"failing={failing(o.out) or '-'}", flush=True)
            print(f"    reason: {m.reason}", flush=True)
            if o.verdict != "KILLED":
                bad.append(m.name)
                tail = "\n".join("    " + line for line in o.out.strip().splitlines()[-25:])
                print("    output tail:\n" + tail, flush=True)
        for group in used:
            print(f"GROUP_SECS {group} {group_secs.get(group, 0.0):.1f}")
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
