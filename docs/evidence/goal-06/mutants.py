#!/usr/bin/env python3
"""Goal 06 round 3 mutation runner (standard library only).

What it does:
1. Exports a committed revision (default HEAD) with `git archive` into a new
   temporary directory outside the repository.
2. Runs every named test command once on the unmutated copy (control). A
   failing control stops the run.
3. For each mutant: replaces one exact search string in one file (the string
   must occur exactly once), runs the mutant's named non-devnet go test
   command in the copy, and restores the file.

Verdicts:
- KILLED: the command exited non-zero and the package built.
- SURVIVED: the command exited zero.
- BUILD_FAILED: a package or test binary did not build. This is not a kill.
- TIMEOUT: the command ran longer than --timeout seconds.

Exit status is 0 only when every control passed and every mutant is KILLED.
It is non-zero when a search string is missing or not unique (checked for all
mutants before any test runs), or when any mutant SURVIVED, BUILD_FAILED or
hit TIMEOUT.

Usage:
  python3 docs/evidence/goal-06/mutants.py [--rev REV] [--only a,b] [--check]
                                            [--logdir DIR] [--keep]

--check validates the search strings and runs nothing. The copy runs Go with
GOPROXY=off, so the module cache must already hold the dependencies and the
toolchain that go.work names. The copy uses its own GOCACHE under the
temporary directory, which is removed at the end unless --keep is given.

Where the 119 mutants come from. Each was reported KILLED by a round 3
fix-gate run on the uncommitted worktree (scratch logs, not in Git):
- 22 named mutants of the first fix gate (honesty review and round 3 fixes);
- 13 named mutants of the second fix gate (rotation rejects, ApplyDue halts,
  export checks, open-count reads, validator canonical key);
- 6 mutants of chain/devnet/init.go from the power-vs-count review;
- 75 rows of the re-review code-swap sweep, where one `fail(CodeX)` or
  `genesisError(CodeX` becomes CodeQuorum. The sweep rows that SURVIVED are
  not listed: they are defensive integrity branches, except four rotation
  rejects that the second fix gate later killed under the names seat-bounds,
  seat-notfound, service-bounds and service-duplicate.
- 3 mutants named by the round-2 review (B1 extra 0x02 ante row; B2 execute
  writes before CONFLICT; B2 dropped max*3).
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

CHAIN = "chain"
DEVNET_INIT = "devnet-init"

# Named non-devnet test commands, run from the root of the copy. SVN_SKIP_DEVNET=1
# makes every test that would start validator processes skip itself.
COMMANDS: dict[str, tuple[list[str], dict[str, str]]] = {
    CHAIN: (
        [
            "go", "test", "-count=1", "-timeout", "10m",
            "./chain/x/authority/", "./chain/app/", "./chain/genesis/", "./chain/cmd/svnd/",
        ],
        {"SVN_SKIP_DEVNET": "1"},
    ),
    DEVNET_INIT: (
        [
            "go", "test", "-count=1", "-timeout", "10m",
            "-run", "^(TestInitPower.*|TestQuorumModelVectors)$", "./chain/devnet/",
        ],
        {"SVN_SKIP_DEVNET": "1"},
    ),
}


@dataclass(frozen=True)
class Mutant:
    name: str
    path: str
    old: str
    new: str
    command: str
    reason: str


def M(name: str, path: str, old: str, new: str, command: str, reason: str) -> Mutant:
    return Mutant(name, path, old, new, command, reason)


E = "chain/x/authority/engine.go"
S = "chain/x/authority/store.go"
P = "chain/x/authority/payload.go"
G = "chain/x/authority/genesis.go"
MSG = "chain/x/authority/msg.go"
INIT = "chain/devnet/init.go"

MUTANTS: list[Mutant] = [
    # First fix gate: honesty review mutants.
    M("algo-gone", E,
      "\tif msg.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {\n\t\treturn fail(CodeMalformed)\n\t}\n\tif msg.GetParameterKey()",
      "\tif false && msg.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {\n\t\treturn fail(CodeMalformed)\n\t}\n\tif msg.GetParameterKey()",
      CHAIN, "Propose must reject a payload digest whose algorithm is not SHA-256 (MALFORMED)"),
    M("ceiling-gone", E,
      "if !known || decoded.LimitValue < 1 || decoded.LimitValue > ceiling {",
      "if _ = ceiling; !known || decoded.LimitValue < 1 {",
      CHAIN, "A limit value above its bounds ceiling must be rejected"),
    M("council-open", E,
      "func (s *State) council() Outcome {\n",
      "func (s *State) council() Outcome {\n\tif true {\n\t\treturn ok()\n\t}\n",
      CHAIN, "The DEC-008 council gate must refuse council messages while the decision is open"),
    M("expiry-bounds-gone", E,
      "if msg.GetExpiryUnixSeconds() < unixMin || msg.GetExpiryUnixSeconds() > unixMax { // check_time 1423",
      "if false { // check_time 1423",
      CHAIN, "A proposal expiry outside the unix-time bounds must be rejected"),
    M("halt-writes", MSG,
      "\tbefore, changed, out := st.ApplyDue()\n\tif !out.Success() {\n",
      "\tbefore, changed, out := st.ApplyDue()\n\tif !out.Success() {\n\t\tstore.Set([]byte{0x7f}, []byte(\"halt wrote this\"))\n\t\titer := store.Iterator([]byte{pendingPrefix}, prefixEnd(pendingPrefix))\n\t\tvar keys [][]byte\n\t\tfor ; iter.Valid(); iter.Next() {\n\t\t\tkeys = append(keys, append([]byte(nil), iter.Key()...))\n\t\t}\n\t\titer.Close()\n\t\tfor _, key := range keys {\n\t\t\tstore.Delete(key)\n\t\t}\n",
      CHAIN, "A halting ApplyDue must leave the authority store as it was (no write, no delete)"),
    M("halt-writes2", MSG,
      "\tbefore, changed, out := st.ApplyDue()\n\tif !out.Success() {\n",
      "\tbefore, changed, out := st.ApplyDue()\n\tif !out.Success() {\n\t\tstore.Set([]byte{0x7f}, []byte(\"halt wrote this\"))\n",
      CHAIN, "A halting ApplyDue must not write any key"),
    M("paramkey-gone", E,
      "if msg.GetParameterKey() != parameterKey(",
      "if false && msg.GetParameterKey() != parameterKey(",
      CHAIN, "The proposal parameter key must be bound to the payload"),
    M("samekey-gone", E,
      "if _, dup := keys[string(item.GetConsensusPubkey())]; dup {",
      "if _, dup := keys[string(item.GetConsensusPubkey())]; dup && false {",
      CHAIN, "Two changes with one consensus key in one activation group must CONFLICT"),
    M("samenode-gone", E,
      "if _, dup := nodes[item.GetNodeId()]; dup {",
      "if _, dup := nodes[item.GetNodeId()]; dup && false {",
      CHAIN, "Two changes for one node id in one activation group must CONFLICT"),
    M("sched-future-gone", E,
      "\tpoint := activationPoint(change)\n\tif !activationFuture(point, s.Height, s.Time) {",
      "\tpoint := activationPoint(change)\n\tif false && !activationFuture(point, s.Height, s.Time) {",
      CHAIN, "An activation point that is not in the future must be rejected"),
    M("stale-gone", E,
      "\tif rec.BasePolicyVersion != version {",
      "\tif false && rec.BasePolicyVersion != version {",
      CHAIN, "Execute must reject a proposal built on a stale policy version"),
    M("stale-swap", E,
      "\t\tif prev != nil && !bytes.Equal(prev, rec.PayloadDigest) {\n\t\t\treturn fail(CodeConflict)",
      "\t\tif prev != nil && !bytes.Equal(prev, rec.PayloadDigest) {\n\t\t\treturn fail(CodeStale)",
      CHAIN, "A different digest at the same version must be CONFLICT, not STALE"),
    M("unknown-limit-ok", E,
      "if !known || decoded.LimitValue < 1 || decoded.LimitValue > ceiling {",
      "if known && (decoded.LimitValue < 1 || decoded.LimitValue > ceiling) {",
      CHAIN, "An unknown limit key must be rejected"),
    M("version-overflow-gone", E,
      "if version == math.MaxUint64 {",
      "if version == math.MaxUint64-version {",
      CHAIN, "Executing at policy version 2^64-1 must give OVERFLOW"),
    # First fix gate: mutants of the round 3 fixes.
    M("open-count-per-seat-only", S,
      "\tkey = binary.BigEndian.AppendUint64(key, version)\n\treturn append(key, seatID...)",
      "\tkey = binary.BigEndian.AppendUint64(key, version*0)\n\treturn append(key, seatID...)",
      CHAIN, "Open-proposal counts are keyed by base policy version, so stale proposals stop counting"),
    M("propose-reads-base-1-count", E,
      "open, out := s.openCount(seat.GetSeatId(), version)",
      "open, out := s.openCount(seat.GetSeatId(), 1)",
      CHAIN, "Propose must read the open count of the current policy version"),
    M("seat-rotation-keeps-admission", E,
      "\tif moved != nil {\n\t\tb.del(AdmissionKey(oldAddr))",
      "\tif false {\n\t\tb.del(AdmissionKey(oldAddr))",
      CHAIN, "Council seat rotation must move the admission row off the old address"),
    M("seat-rotation-no-submitter-dup", E,
      "\t\tif bytes.Equal(row.GetAddress(), newAddr) {\n\t\t\treturn fail(CodeDuplicate)\n\t\t}\n\t\tif bytes.Equal(row.GetAddress(), oldAddr) {\n\t\t\tmoved = row",
      "\t\tif false {\n\t\t\treturn fail(CodeDuplicate)\n\t\t}\n\t\tif bytes.Equal(row.GetAddress(), oldAddr) {\n\t\t\tmoved = row",
      CHAIN, "Seat rotation onto an address that is already a submitter must be DUPLICATE_ID"),
    M("height-int64-compare", P,
      "if body.Height < 1 || body.Height > uint64(safeMax) {",
      "if body.Height < 1 || int64(body.Height) > safeMax {",
      CHAIN, "Activation height must be compared as uint64, so 2^63 and above are rejected"),
    M("evidenceitem-unregistered", "chain/app/encoding.go",
      "\tregisterGogo(\"svn.evidence.v1.EvidenceItem\", &evidencev1.EvidenceItem{})\n",
      "",
      CHAIN, "svn.evidence.v1.EvidenceItem must stay registered so every protocol message decodes"),
    M("svnd-skips-authority-rules", "chain/cmd/svnd/main.go",
      "\t\treturn app.ValidateAuthorityGenesis(modules, genDoc.InitialHeight, genDoc.GenesisTime.Unix())",
      "\t\t_ = app.ValidateAuthorityGenesis\n\t\treturn nil",
      CHAIN, "svnd genesis validation must apply the authority genesis rules"),
    M("initchain-skips-authority-rules", "chain/app/app.go",
      "\tif err := ValidateAuthorityGenesis(modules, req.InitialHeight, req.Time.Unix()); err != nil {",
      "\tif err := ValidateAuthorityGenesis(modules, req.InitialHeight, req.Time.Unix()); err != nil && false {",
      CHAIN, "InitChain must reject an authority genesis that fails the authority rules"),
    # Second fix gate.
    M("seat-bounds", E,
      "\tif !idPattern.MatchString(seatID) || len(newPub) != 32 {\n\t\treturn fail(CodeBounds)\n",
      "\tif !idPattern.MatchString(seatID) || len(newPub) != 32 {\n\t\treturn fail(CodeQuorum)\n",
      CHAIN, "RotateCouncilSeat with a bad seat id or key length must be BOUNDS"),
    M("seat-notfound", E,
      "\tif seat == nil {\n\t\treturn fail(CodeNotFound)\n",
      "\tif seat == nil {\n\t\treturn fail(CodeQuorum)\n",
      CHAIN, "RotateCouncilSeat for an unknown seat must be NOT_FOUND"),
    M("service-bounds", E,
      "\tif len(oldAddr) != 20 || len(newAddr) != 20 || bytes.Equal(oldAddr, newAddr) {\n\t\treturn fail(CodeBounds)\n",
      "\tif len(oldAddr) != 20 || len(newAddr) != 20 || bytes.Equal(oldAddr, newAddr) {\n\t\treturn fail(CodeQuorum)\n",
      CHAIN, "RotateServiceSubmitter with a bad or unchanged address must be BOUNDS"),
    M("service-duplicate", E,
      "\tvar found *typesv1.Submitter\n\tfor _, row := range cfg.GetAuthority().GetSubmitters() {\n\t\tif bytes.Equal(row.GetAddress(), newAddr) {\n\t\t\treturn fail(CodeDuplicate)\n",
      "\tvar found *typesv1.Submitter\n\tfor _, row := range cfg.GetAuthority().GetSubmitters() {\n\t\tif bytes.Equal(row.GetAddress(), newAddr) {\n\t\t\treturn fail(CodeQuorum)\n",
      CHAIN, "RotateServiceSubmitter onto an existing submitter must be DUPLICATE_ID"),
    M("applydue-dupkey-gone", E,
      "\t\tif _, dup := keys[string(row.GetConsensusPubkey())]; dup {\n\t\t\treturn before, false, fail(CodeDuplicate)\n\t\t}\n",
      "",
      CHAIN, "ApplyDue must halt when two validator rows share a consensus key"),
    M("applydue-notfound", E,
      "\t\t\t\treturn before, false, fail(CodeNotFound)",
      "\t\t\t\treturn before, false, fail(CodeQuorum)",
      CHAIN, "ApplyDue must halt with NOT_FOUND when a due removal names an unknown validator"),
    M("oldlayout-gone", S,
      "if len(auth.GetProposals()) != 0 || len(auth.GetPendingValidatorPower()) != 0 || len(auth.GetApprovedCouncil()) != 0 {",
      "if auth == nil {",
      CHAIN, "A config that still carries the old in-document layout is an integrity error"),
    M("export-role-presence", S,
      "if !ok || role != byte(row.GetRole()) {",
      "if _ = role; !ok {",
      CHAIN, "Export must check each admission row's role against its submitter"),
    M("readpending-cap-gone", S,
      "\t\tif len(out) == MaxPendingChanges {",
      "\t\tif false {",
      CHAIN, "Reading more than MaxPendingChanges pending entries must be an integrity error"),
    M("export-pending-cap-gone", S,
      "\tif len(pending) > MaxPendingChanges {",
      "\tif false {",
      CHAIN, "Export must refuse more than MaxPendingChanges pending entries"),
    M("approve-reads-open", E,
      "\tif slices.Contains(rec.Approvers, seat.GetSeatId()) { // 1473",
      "\t_, _ = s.openCount(rec.Proposer, rec.BasePolicyVersion)\n\tif slices.Contains(rec.Approvers, seat.GetSeatId()) { // 1473",
      CHAIN, "Approve must not read open-count keys, so its key set stays fixed"),
    M("propose-skips-open-read", E,
      "\topen, out := s.openCount(seat.GetSeatId(), version)",
      "\topen, out := uint64(0), ok()",
      CHAIN, "Propose must read the seat's open-proposal count before it accepts"),
    M("canonical-truncates-key", P,
      "\t\tPubKey:   append([]byte(nil), pub...),",
      "\t\tPubKey:   append([]byte(nil), pub[:31]...),",
      CHAIN, "The validator canonical form must carry the full 32-byte consensus key"),
    # Power-vs-count review: chain/devnet/init.go.
    M("init-power-ceiling-gone", INIT,
      "\tif requested > genesis.SafeIntegerMax/Validators {",
      "\tif false {",
      DEVNET_INIT, "Init must refuse a per-validator power whose total exceeds safe_integer_max"),
    M("init-negative-power-ok", INIT,
      "\tif requested < 0 {",
      "\tif false {",
      DEVNET_INIT, "Init must refuse a negative validator power"),
    M("init-default-power-2", INIT,
      "\tif requested == 0 {\n\t\treturn 1, nil",
      "\tif requested == 0 {\n\t\treturn 2, nil",
      DEVNET_INIT, "Power 0 must mean the default power 1"),
    M("init-document-power-1", INIT,
      "\t\t\tPower:              opts.Power,",
      "\t\t\tPower:              1,",
      DEVNET_INIT, "The genesis document must carry the requested validator power"),
    M("init-comet-power-1", INIT,
      "\t\t\tPower:   power,",
      "\t\t\tPower:   1,",
      DEVNET_INIT, "The CometBFT genesis validators must carry the requested power"),
    M("init-topology-power-1", INIT,
      "\t\tPower:              power,",
      "\t\tPower:              1,",
      DEVNET_INIT, "The topology file must record the requested power"),
    # Re-review code-swap sweep rows reported KILLED.
    M('swap-engine-124', 'chain/x/authority/engine.go',
      '\tif param == nil || param.GetNumericValue() != ApprovedThreshold || param.GetDelaySeconds() < 0 || param.GetDelaySeconds() > unixMax {\n\t\treturn fail(CodeDecisionBlocked)\n',
      '\tif param == nil || param.GetNumericValue() != ApprovedThreshold || param.GetDelaySeconds() < 0 || param.GetDelaySeconds() > unixMax {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'council: the DecisionBlocked reject after `if param == nil || param.GetNumericValue() != ApprovedThreshold || param.GetDelaySecond...` must keep its exact code'),
    M('swap-engine-127', 'chain/x/authority/engine.go',
      '\tif len(s.auth().GetDevnetTestSeats()) != ApprovedSeats {\n\t\treturn fail(CodeDecisionBlocked)\n',
      '\tif len(s.auth().GetDevnetTestSeats()) != ApprovedSeats {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'council: the DecisionBlocked reject after `if len(s.auth().GetDevnetTestSeats()) != ApprovedSeats` must keep its exact code'),
    M('swap-engine-155', 'chain/x/authority/engine.go',
      '\tif !idPattern.MatchString(id) {\n\t\treturn fail(CodeBounds)\n',
      '\tif !idPattern.MatchString(id) {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'business: the Bounds reject after `if !idPattern.MatchString(id)` must keep its exact code'),
    M('swap-engine-165', 'chain/x/authority/engine.go',
      '\tif len(prev) != sha256.Size {\n\t\treturn fail(CodeIntegrity)\n',
      '\tif len(prev) != sha256.Size {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'business: the Integrity reject after `if len(prev) != sha256.Size` must keep its exact code'),
    M('swap-engine-170', 'chain/x/authority/engine.go',
      '\t}\n\treturn fail(CodeReplay)\n',
      '\t}\n\treturn fail(CodeQuorum)\n',
      CHAIN, 'business: the Replay reject after `if bytes.Equal(prev, digest)` must keep its exact code'),
    M('swap-engine-177', 'chain/x/authority/engine.go',
      'func (s *State) record(id string) (*proposalRecord, Outcome) {\n\tif !idPattern.MatchString(id) {\n\t\treturn nil, fail(CodeNotFound)\n',
      'func (s *State) record(id string) (*proposalRecord, Outcome) {\n\tif !idPattern.MatchString(id) {\n\t\treturn nil, fail(CodeQuorum)\n',
      CHAIN, 'record: the NotFound reject after `if !idPattern.MatchString(id)` must keep its exact code'),
    M('swap-engine-181', 'chain/x/authority/engine.go',
      '\traw := s.kv.Get(proposalKey(id))\n\tif raw == nil {\n\t\treturn nil, fail(CodeNotFound)\n',
      '\traw := s.kv.Get(proposalKey(id))\n\tif raw == nil {\n\t\treturn nil, fail(CodeQuorum)\n',
      CHAIN, 'record: the NotFound reject after `if raw == nil` must keep its exact code'),
    M('swap-engine-185', 'chain/x/authority/engine.go',
      '\trec, err := decodeRecord(id, raw)\n\tif err != nil {\n\t\treturn nil, fail(CodeIntegrity)\n',
      '\trec, err := decodeRecord(id, raw)\n\tif err != nil {\n\t\treturn nil, fail(CodeQuorum)\n',
      CHAIN, 'record: the Integrity reject after `if err != nil` must keep its exact code'),
    M('swap-engine-224', 'chain/x/authority/engine.go',
      '\tcase 0:\n\t\treturn fail(CodeIntegrity)\n',
      '\tcase 0:\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'release: the Integrity reject after `case 0:` must keep its exact code'),
    M('swap-engine-258', 'chain/x/authority/engine.go',
      '\t\tmsg.GetParameterKey() == "" || msg.GetPayload() == nil || msg.GetPayloadDigest() == nil {\n\t\treturn fail(CodeMalformed)\n',
      '\t\tmsg.GetParameterKey() == "" || msg.GetPayload() == nil || msg.GetPayloadDigest() == nil {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Malformed reject after `if msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProp...` must keep its exact code'),
    M('swap-engine-265', 'chain/x/authority/engine.go',
      '\tif seat == nil { // 1411\n\t\treturn fail(CodeUnauthorized)\n',
      '\tif seat == nil { // 1411\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Unauthorized reject after `if seat == nil { // 1411` must keep its exact code'),
    M('swap-engine-268', 'chain/x/authority/engine.go',
      '\tif !idPattern.MatchString(msg.GetProposalId()) || !idPattern.MatchString(msg.GetParameterKey()) { // 1413-1416\n\t\treturn fail(CodeBounds)\n',
      '\tif !idPattern.MatchString(msg.GetProposalId()) || !idPattern.MatchString(msg.GetParameterKey()) { // 1413-1416\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Bounds reject after `if !idPattern.MatchString(msg.GetProposalId()) || !idPattern.MatchString(msg.GetParamet...` must keep its exact code'),
    M('swap-engine-271', 'chain/x/authority/engine.go',
      '\tif strings.HasPrefix(msg.GetParameterKey(), "dec-") { // 1417\n\t\treturn fail(CodeForbidden)\n',
      '\tif strings.HasPrefix(msg.GetParameterKey(), "dec-") { // 1417\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Forbidden reject after `if strings.HasPrefix(msg.GetParameterKey(), "dec-") { // 1417` must keep its exact code'),
    M('swap-engine-279', 'chain/x/authority/engine.go',
      '\tif msg.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {\n\t\treturn fail(CodeMalformed)\n',
      '\tif msg.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Malformed reject after `if msg.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SH...` must keep its exact code'),
    M('swap-engine-282', 'chain/x/authority/engine.go',
      '\tif msg.GetParameterKey() != parameterKey(kind, change.NodeID, limitKey(msg), providerID(msg), contentID(msg)) {\n\t\treturn fail(CodeMalformed)\n',
      '\tif msg.GetParameterKey() != parameterKey(kind, change.NodeID, limitKey(msg), providerID(msg), contentID(msg)) {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Malformed reject after `if msg.GetParameterKey() != parameterKey(kind, change.NodeID, limitKey(msg), providerID...` must keep its exact code'),
    M('swap-engine-285', 'chain/x/authority/engine.go',
      '\tif kind == kindValidatorPower && !activationFuture(activationPoint(change), s.Height, s.Time) {\n\t\treturn fail(CodeOutOfRange)\n',
      '\tif kind == kindValidatorPower && !activationFuture(activationPoint(change), s.Height, s.Time) {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the OutOfRange reject after `if kind == kindValidatorPower && !activationFuture(activationPoint(change), s.Height, s...` must keep its exact code'),
    M('swap-engine-288', 'chain/x/authority/engine.go',
      '\tif msg.GetExpiryUnixSeconds() < unixMin || msg.GetExpiryUnixSeconds() > unixMax { // check_time 1423\n\t\treturn fail(CodeBounds)\n',
      '\tif msg.GetExpiryUnixSeconds() < unixMin || msg.GetExpiryUnixSeconds() > unixMax { // check_time 1423\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Bounds reject after `if msg.GetExpiryUnixSeconds() < unixMin || msg.GetExpiryUnixSeconds() > unixMax { // ch...` must keep its exact code'),
    M('swap-engine-301', 'chain/x/authority/engine.go',
      '\tif s.kv.Has(proposalKey(msg.GetProposalId())) { // 1432\n\t\treturn fail(CodeDuplicate)\n',
      '\tif s.kv.Has(proposalKey(msg.GetProposalId())) { // 1432\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Duplicate reject after `if s.kv.Has(proposalKey(msg.GetProposalId())) { // 1432` must keep its exact code'),
    M('swap-engine-304', 'chain/x/authority/engine.go',
      '\t\treturn fail(CodeExpired)\n',
      '\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Expired reject after `if msg.GetExpiryUnixSeconds() <= s.Time { // 1434` must keep its exact code'),
    M('swap-engine-311', 'chain/x/authority/engine.go',
      '\tif msg.GetBasePolicyVersion() != version { // 1436\n\t\treturn fail(CodeStale)\n',
      '\tif msg.GetBasePolicyVersion() != version { // 1436\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Stale reject after `if msg.GetBasePolicyVersion() != version { // 1436` must keep its exact code'),
    M('swap-engine-321', 'chain/x/authority/engine.go',
      '\tif open >= MaxOpenProposalsPerSeat {\n\t\treturn fail(CodeBounds)\n',
      '\tif open >= MaxOpenProposalsPerSeat {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ProposeChange: the Bounds reject after `if open >= MaxOpenProposalsPerSeat` must keep its exact code'),
    M('swap-engine-350', 'chain/x/authority/engine.go',
      '\tif msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProposalId() == "" { // 1455\n\t\treturn fail(CodeMalformed)\n',
      '\tif msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProposalId() == "" { // 1455\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ApproveChange: the Malformed reject after `if msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProp...` must keep its exact code'),
    M('swap-engine-357', 'chain/x/authority/engine.go',
      '\tif seat == nil { // 1460\n\t\treturn fail(CodeUnauthorized)\n',
      '\tif seat == nil { // 1460\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ApproveChange: the Unauthorized reject after `if seat == nil { // 1460` must keep its exact code'),
    M('swap-engine-371', 'chain/x/authority/engine.go',
      '\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN { // 1468\n\t\treturn fail(CodeConflict)\n',
      '\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN { // 1468\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ApproveChange: the Conflict reject after `if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN { // 1468` must keep its exact code'),
    M('swap-engine-377', 'chain/x/authority/engine.go',
      '\t\treturn fail(CodeReplay)\n',
      '\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ApproveChange: the Replay reject after `if slices.Contains(rec.Approvers, seat.GetSeatId()) { // 1473` must keep its exact code'),
    M('swap-engine-382', 'chain/x/authority/engine.go',
      '\tif len(rec.Approvers)+1 >= ApprovedThreshold && s.Time > unixMax-delay {\n\t\treturn fail(CodeOverflow)\n',
      '\tif len(rec.Approvers)+1 >= ApprovedThreshold && s.Time > unixMax-delay {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ApproveChange: the Overflow reject after `if len(rec.Approvers)+1 >= ApprovedThreshold && s.Time > unixMax-delay` must keep its exact code'),
    M('swap-engine-405', 'chain/x/authority/engine.go',
      '\tif msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProposalId() == "" { // 1488\n\t\treturn fail(CodeMalformed)\n',
      '\tif msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProposalId() == "" { // 1488\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the Malformed reject after `if msg == nil || len(msg.GetSigner()) != 20 || msg.GetBusinessId() == "" || msg.GetProp...` must keep its exact code'),
    M('swap-engine-409', 'chain/x/authority/engine.go',
      '\t\t\treturn fail(CodeUnauthorized)\n',
      '\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the Unauthorized reject after `if role == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED` must keep its exact code'),
    M('swap-engine-411', 'chain/x/authority/engine.go',
      '\t\t}\n\t\treturn fail(CodeForbidden)\n',
      '\t\t}\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the Forbidden reject after `if role == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED` must keep its exact code'),
    M('swap-engine-429', 'chain/x/authority/engine.go',
      '\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {\n\t\treturn fail(CodeConflict)\n',
      '\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the Conflict reject after `if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED` must keep its exact code'),
    M('swap-engine-432', 'chain/x/authority/engine.go',
      '\tif s.Time < rec.ExecuteAfterUnixSeconds {\n\t\treturn fail(CodeOutOfRange)\n',
      '\tif s.Time < rec.ExecuteAfterUnixSeconds {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the OutOfRange reject after `if s.Time < rec.ExecuteAfterUnixSeconds` must keep its exact code'),
    M('swap-engine-441', 'chain/x/authority/engine.go',
      '\t\tif prev != nil && !bytes.Equal(prev, rec.PayloadDigest) {\n\t\t\treturn fail(CodeConflict)\n',
      '\t\tif prev != nil && !bytes.Equal(prev, rec.PayloadDigest) {\n\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the Conflict reject after `if prev != nil && !bytes.Equal(prev, rec.PayloadDigest)` must keep its exact code'),
    M('swap-engine-443', 'chain/x/authority/engine.go',
      '\t\t}\n\t\treturn fail(CodeStale)\n',
      '\t\t}\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the Stale reject after `if prev != nil && !bytes.Equal(prev, rec.PayloadDigest)` must keep its exact code'),
    M('swap-engine-453', 'chain/x/authority/engine.go',
      '\tif version == math.MaxUint64 {\n\t\treturn fail(CodeOverflow)\n',
      '\tif version == math.MaxUint64 {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'ExecuteChange: the Overflow reject after `if version == math.MaxUint64` must keep its exact code'),
    M('swap-engine-482', 'chain/x/authority/engine.go',
      '\t\t\treturn fail(CodeBounds)\n',
      '\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'applyPayload: the Bounds reject after `if !known || decoded.LimitValue < 1 || decoded.LimitValue > ceiling` must keep its exact code'),
    M('swap-engine-492', 'chain/x/authority/engine.go',
      '\t\t// No content registry until Goal 07.\n\t\treturn fail(CodeNotFound)\n',
      '\t\t// No content registry until Goal 07.\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'applyPayload: the NotFound reject after `case kindForceWithdraw:` must keep its exact code'),
    M('swap-engine-503', 'chain/x/authority/engine.go',
      '\tif !activationFuture(point, s.Height, s.Time) {\n\t\treturn fail(CodeOutOfRange)\n',
      '\tif !activationFuture(point, s.Height, s.Time) {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'schedule: the OutOfRange reject after `if !activationFuture(point, s.Height, s.Time)` must keep its exact code'),
    M('swap-engine-524', 'chain/x/authority/engine.go',
      '\t\tif item.GetProposalId() == proposalID {\n\t\t\treturn fail(CodeConflict)\n',
      '\t\tif item.GetProposalId() == proposalID {\n\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'schedule: the Conflict reject after `if item.GetProposalId() == proposalID` must keep its exact code'),
    M('swap-engine-532', 'chain/x/authority/engine.go',
      '\tif len(pending) >= MaxPendingChanges {\n\t\treturn fail(CodeBounds)\n',
      '\tif len(pending) >= MaxPendingChanges {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'schedule: the Bounds reject after `if len(pending) >= MaxPendingChanges` must keep its exact code'),
    M('swap-engine-548', 'chain/x/authority/engine.go',
      '\traw := s.kv.Get(providerKey(id))\n\tif raw == nil {\n\t\treturn nil, fail(CodeNotFound)\n',
      '\traw := s.kv.Get(providerKey(id))\n\tif raw == nil {\n\t\treturn nil, fail(CodeQuorum)\n',
      CHAIN, 'provider: the NotFound reject after `if raw == nil` must keep its exact code'),
    M('swap-engine-575', 'chain/x/authority/engine.go',
      '\tif param == nil || param.GetRoleName() != "DEVNET_TEST_ONLY" {\n\t\treturn fail(CodeDecisionBlocked)\n',
      '\tif param == nil || param.GetRoleName() != "DEVNET_TEST_ONLY" {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'admit: the DecisionBlocked reject after `if param == nil || param.GetRoleName() != "DEVNET_TEST_ONLY"` must keep its exact code'),
    M('swap-engine-578', 'chain/x/authority/engine.go',
      '\tif row.OperatorGroup != "devnet-test-only" || row.State != "APPLIED" {\n\t\treturn fail(CodeForbidden)\n',
      '\tif row.OperatorGroup != "devnet-test-only" || row.State != "APPLIED" {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'admit: the Forbidden reject after `if row.OperatorGroup != "devnet-test-only" || row.State != "APPLIED"` must keep its exact code'),
    M('swap-engine-591', 'chain/x/authority/engine.go',
      '\tif row.State != "ADMITTED" && row.State != "DRAINING" {\n\t\treturn fail(CodeConflict)\n',
      '\tif row.State != "ADMITTED" && row.State != "DRAINING" {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'disable: the Conflict reject after `if row.State != "ADMITTED" && row.State != "DRAINING"` must keep its exact code'),
    M('swap-engine-640', 'chain/x/authority/engine.go',
      '\t\tif kind != firstKind { // DEC-012\n\t\t\treturn fail(CodeConflict)\n',
      '\t\tif kind != firstKind { // DEC-012\n\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'projectAll: the Conflict reject after `if kind != firstKind { // DEC-012` must keep its exact code'),
    M('swap-engine-660', 'chain/x/authority/engine.go',
      '\t\t\tif _, dup := nodes[item.GetNodeId()]; dup {\n\t\t\t\treturn fail(CodeConflict)\n',
      '\t\t\tif _, dup := nodes[item.GetNodeId()]; dup {\n\t\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'projectAll: the Conflict reject after `if _, dup := nodes[item.GetNodeId()]; dup` must keep its exact code'),
    M('swap-engine-667', 'chain/x/authority/engine.go',
      '\t\t\t\t\treturn fail(CodeConflict)\n',
      '\t\t\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'projectAll: the Conflict reject after `if !have || !bytes.Equal(current.pub, item.GetConsensusPubkey())` must keep its exact code'),
    M('swap-engine-671', 'chain/x/authority/engine.go',
      '\t\t\tif _, dup := keys[string(item.GetConsensusPubkey())]; dup {\n\t\t\t\treturn fail(CodeConflict)\n',
      '\t\t\tif _, dup := keys[string(item.GetConsensusPubkey())]; dup {\n\t\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'projectAll: the Conflict reject after `if _, dup := keys[string(item.GetConsensusPubkey())]; dup` must keep its exact code'),
    M('swap-engine-682', 'chain/x/authority/engine.go',
      '\t\t\t\t\treturn fail(CodeDuplicate)\n',
      '\t\t\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'projectAll: the Duplicate reject after `if node != item.GetNodeId() && bytes.Equal(row.pub, item.GetConsensusPubkey())` must keep its exact code'),
    M('swap-engine-717', 'chain/x/authority/engine.go',
      '\tif len(vals) < genesis.MinValidators {\n\t\treturn fail(CodeForbidden)\n',
      '\tif len(vals) < genesis.MinValidators {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'powerInvariant: the Forbidden reject after `if len(vals) < genesis.MinValidators` must keep its exact code'),
    M('swap-engine-722', 'chain/x/authority/engine.go',
      '\t\t\treturn fail(CodeForbidden)\n',
      '\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'powerInvariant: the Forbidden reject after `if row.power <= 0 || row.power > safeMax-total` must keep its exact code'),
    M('swap-engine-728', 'chain/x/authority/engine.go',
      '\tif maxPower*3 >= total {\n\t\treturn fail(CodeForbidden)\n',
      '\tif maxPower*3 >= total {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'powerInvariant: the Forbidden reject after `if maxPower*3 >= total` must keep its exact code'),
    M('swap-engine-771', 'chain/x/authority/engine.go',
      '\t\t\t\treturn before, false, fail(CodeConflict)\n',
      '\t\t\t\treturn before, false, fail(CodeQuorum)\n',
      CHAIN, 'ApplyDue: the Conflict reject after `if !bytes.Equal(current.GetConsensusPubkey(), item.GetConsensusPubkey())` must keep its exact code'),
    M('swap-engine-841', 'chain/x/authority/engine.go',
      '\t\tif bytes.Equal(row.GetPublicKey(), newPub) {\n\t\t\treturn fail(CodeDuplicate)\n',
      '\t\tif bytes.Equal(row.GetPublicKey(), newPub) {\n\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'RotateCouncilSeat: the Duplicate reject after `if bytes.Equal(row.GetPublicKey(), newPub)` must keep its exact code'),
    M('swap-engine-848', 'chain/x/authority/engine.go',
      '\tif bytes.Equal(seat.GetPublicKey(), newPub) || s.kv.Has(retiredCouncilKey(newPub)) {\n\t\treturn fail(CodeConflict)\n',
      '\tif bytes.Equal(seat.GetPublicKey(), newPub) || s.kv.Has(retiredCouncilKey(newPub)) {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'RotateCouncilSeat: the Conflict reject after `if bytes.Equal(seat.GetPublicKey(), newPub) || s.kv.Has(retiredCouncilKey(newPub))` must keep its exact code'),
    M('swap-engine-855', 'chain/x/authority/engine.go',
      '\tvar moved *typesv1.Submitter\n\tfor _, row := range cfg.GetAuthority().GetSubmitters() {\n\t\tif bytes.Equal(row.GetAddress(), newAddr) {\n\t\t\treturn fail(CodeDuplicate)\n',
      '\tvar moved *typesv1.Submitter\n\tfor _, row := range cfg.GetAuthority().GetSubmitters() {\n\t\tif bytes.Equal(row.GetAddress(), newAddr) {\n\t\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'RotateCouncilSeat: the Duplicate reject after `if bytes.Equal(row.GetAddress(), newAddr)` must keep its exact code'),
    M('swap-engine-886', 'chain/x/authority/engine.go',
      '\tif len(oldAddr) != 20 || len(newAddr) != 20 || bytes.Equal(oldAddr, newAddr) {\n\t\treturn fail(CodeBounds)\n',
      '\tif len(oldAddr) != 20 || len(newAddr) != 20 || bytes.Equal(oldAddr, newAddr) {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'RotateServiceSubmitter: the Bounds reject after `if len(oldAddr) != 20 || len(newAddr) != 20 || bytes.Equal(oldAddr, newAddr)` must keep its exact code'),
    M('swap-engine-899', 'chain/x/authority/engine.go',
      '\tif found == nil || found.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE {\n\t\treturn fail(CodeNotFound)\n',
      '\tif found == nil || found.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'RotateServiceSubmitter: the NotFound reject after `if found == nil || found.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SER...` must keep its exact code'),
    M('swap-engine-902', 'chain/x/authority/engine.go',
      '\tif s.kv.Has(retiredServiceKey(newAddr)) {\n\t\treturn fail(CodeConflict)\n',
      '\tif s.kv.Has(retiredServiceKey(newAddr)) {\n\t\treturn fail(CodeQuorum)\n',
      CHAIN, 'RotateServiceSubmitter: the Conflict reject after `if s.kv.Has(retiredServiceKey(newAddr))` must keep its exact code'),
    M('swap-genesis-40', 'chain/x/authority/genesis.go',
      '\t\treturn genesisError(CodeMalformed, "authority state is missing")\n',
      '\t\treturn genesisError(CodeQuorum, "authority state is missing")\n',
      CHAIN, 'ValidateGenesisState: the Malformed reject after `if auth == nil` must keep its exact code'),
    M('swap-genesis-43', 'chain/x/authority/genesis.go',
      '\t\treturn genesisError(CodeMalformed, "genesis proposals are not supported: the proto has no payload, so they could never execute")\n',
      '\t\treturn genesisError(CodeQuorum, "genesis proposals are not supported: the proto has no payload, so they could never execute")\n',
      CHAIN, 'ValidateGenesisState: the Malformed reject after `if len(auth.GetProposals()) != 0` must keep its exact code'),
    M('swap-genesis-46', 'chain/x/authority/genesis.go',
      '\t\treturn genesisError(CodeForbidden, "approved_council must be empty while DEC-008 is open")\n',
      '\t\treturn genesisError(CodeQuorum, "approved_council must be empty while DEC-008 is open")\n',
      CHAIN, 'ValidateGenesisState: the Forbidden reject after `if len(auth.GetApprovedCouncil()) != 0` must keep its exact code'),
    M('swap-genesis-49', 'chain/x/authority/genesis.go',
      '\t\treturn genesisError(CodeForbidden, "quorum_loss must be UNSET while DEC-008 is open")\n',
      '\t\treturn genesisError(CodeQuorum, "quorum_loss must be UNSET while DEC-008 is open")\n',
      CHAIN, 'ValidateGenesisState: the Forbidden reject after `if auth.GetQuorumLoss() != authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET` must keep its exact code'),
    M('swap-genesis-62', 'chain/x/authority/genesis.go',
      '\t\treturn genesisError(CodeBounds, "%d pending validator changes, more than %d", len(pending), MaxPendingChanges)\n',
      '\t\treturn genesisError(CodeQuorum, "%d pending validator changes, more than %d", len(pending), MaxPendingChanges)\n',
      CHAIN, 'ValidateGenesisState: the Bounds reject after `if len(pending) > MaxPendingChanges` must keep its exact code'),
    M('swap-genesis-70', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeDuplicate, "pending proposal id %q appears twice", entry.GetProposalId())\n',
      '\t\t\treturn genesisError(CodeQuorum, "pending proposal id %q appears twice", entry.GetProposalId())\n',
      CHAIN, 'ValidateGenesisState: the Duplicate reject after `if _, dup := ids[entry.GetProposalId()]; dup` must keep its exact code'),
    M('swap-genesis-81', 'chain/x/authority/genesis.go',
      '\t\treturn genesisError(CodeMalformed, "initial height %d", at.InitialHeight)\n',
      '\t\treturn genesisError(CodeQuorum, "initial height %d", at.InitialHeight)\n',
      CHAIN, 'ValidateGenesisState: the Malformed reject after `if at.InitialHeight < 1` must keep its exact code'),
    M('swap-genesis-85', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeOutOfRange, "pending entry %q activates before the first block", entry.GetProposalId())\n',
      '\t\t\treturn genesisError(CodeQuorum, "pending entry %q activates before the first block", entry.GetProposalId())\n',
      CHAIN, 'ValidateGenesisState: the OutOfRange reject after `if !activationFuture(entry.GetActivation(), at.InitialHeight-1, at.GenesisUnixSeconds)` must keep its exact code'),
    M('swap-genesis-93', 'chain/x/authority/genesis.go',
      '\t\treturn genesisError(CodeBounds, "%d devnet test seats; the council shape is %d", len(seats), ApprovedSeats)\n',
      '\t\treturn genesisError(CodeQuorum, "%d devnet test seats; the council shape is %d", len(seats), ApprovedSeats)\n',
      CHAIN, 'validateSeats: the Bounds reject after `if len(seats) != 0 && len(seats) != ApprovedSeats` must keep its exact code'),
    M('swap-genesis-102', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeDuplicate, "seat id %q appears twice", seat.GetSeatId())\n',
      '\t\t\treturn genesisError(CodeQuorum, "seat id %q appears twice", seat.GetSeatId())\n',
      CHAIN, 'validateSeats: the Duplicate reject after `if _, dup := ids[seat.GetSeatId()]; dup` must keep its exact code'),
    M('swap-genesis-106', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeDuplicate, "seat key appears twice")\n',
      '\t\t\treturn genesisError(CodeQuorum, "seat key appears twice")\n',
      CHAIN, 'validateSeats: the Duplicate reject after `if _, dup := keys[string(seat.GetPublicKey())]; dup` must keep its exact code'),
    M('swap-genesis-117', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeBounds, "submitter address length %d", len(row.GetAddress()))\n',
      '\t\t\treturn genesisError(CodeQuorum, "submitter address length %d", len(row.GetAddress()))\n',
      CHAIN, 'validateSubmitters: the Bounds reject after `if len(row.GetAddress()) != 20` must keep its exact code'),
    M('swap-genesis-120', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeMalformed, "submitter role %v", row.GetRole())\n',
      '\t\t\treturn genesisError(CodeQuorum, "submitter role %v", row.GetRole())\n',
      CHAIN, 'validateSubmitters: the Malformed reject after `if !roleKnown(row.GetRole())` must keep its exact code'),
    M('swap-genesis-123', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeDuplicate, "submitter %x appears twice", row.GetAddress())\n',
      '\t\t\treturn genesisError(CodeQuorum, "submitter %x appears twice", row.GetAddress())\n',
      CHAIN, 'validateSubmitters: the Duplicate reject after `if _, dup := seen[string(row.GetAddress())]; dup` must keep its exact code'),
    M('swap-genesis-135', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeBounds, "validator %q", row.GetNodeId())\n',
      '\t\t\treturn genesisError(CodeQuorum, "validator %q", row.GetNodeId())\n',
      CHAIN, 'validateGenesisValidators: the Bounds reject after `if !idPattern.MatchString(row.GetNodeId()) || len(row.GetConsensusPubkey()) != 32` must keep its exact code'),
    M('swap-genesis-138', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeDuplicate, "validator node id %q appears twice", row.GetNodeId())\n',
      '\t\t\treturn genesisError(CodeQuorum, "validator node id %q appears twice", row.GetNodeId())\n',
      CHAIN, 'validateGenesisValidators: the Duplicate reject after `if _, dup := nodes[row.GetNodeId()]; dup` must keep its exact code'),
    M('swap-genesis-142', 'chain/x/authority/genesis.go',
      '\t\t\treturn genesisError(CodeDuplicate, "validator key of %q appears twice", row.GetNodeId())\n',
      '\t\t\treturn genesisError(CodeQuorum, "validator key of %q appears twice", row.GetNodeId())\n',
      CHAIN, 'validateGenesisValidators: the Duplicate reject after `if _, dup := keys[string(row.GetConsensusPubkey())]; dup` must keep its exact code'),
    # Named by the round-2 review (B1 and B2).
    M("ante-extra-admission", "chain/app/app.go",
      "func(ctx sdk.Context, addr []byte) bool {\n\t\treturn authorityKeeper.Admitted(ctx, addr)\n",
      "func(ctx sdk.Context, addr []byte) bool {\n\t\tctx.KVStore(keys[authority.ModuleName]).Set(authority.AdmissionKey(append([]byte(nil), addr[:19]...)), []byte{9})\n\t\treturn authorityKeeper.Admitted(ctx, addr)\n",
      CHAIN, "B1 (round-2 review): an extra 0x02 admission row written in the ante of a committed transaction must fail the full authority-store compare"),
    M("exec-conflict-write", E,
      "\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {\n\t\treturn fail(CodeConflict)\n",
      "\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {\n\t\tif version, vout := s.policy(); vout.Success() {\n\t\t\trec.State = int32(authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED)\n\t\t\tvar mb batch\n\t\t\tputRecord(&mb, rec)\n\t\t\tmb.set([]byte{policyKey}, encodeU64(version+1))\n\t\t\ts.flush(&mb)\n\t\t}\n\t\treturn fail(CodeConflict)\n",
      CHAIN, "B2 (round-2 review): Execute of a proposal that is not QUEUED must return CONFLICT without writing State=EXECUTED or a bumped policy version"),
    M("drop-share-cap", E,
      "\tif maxPower*3 >= total {\n\t\treturn fail(CodeForbidden)\n\t}\n",
      "",
      CHAIN, "B2 (round-2 review): powerInvariant must reject a resulting set whose max power times 3 is at least the total (FORBIDDEN)"),
]

BUILD_FAILURE = re.compile(r"\[(build|setup) failed\]|^# \S+$|cannot find package|undefined: ", re.M)
FAIL_LINE = re.compile(r"^\s*--- FAIL: (\S+)", re.M)


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


def validate(copy: pathlib.Path, mutants: list[Mutant]) -> list[str]:
    problems = []
    names = set()
    for m in mutants:
        if m.name in names:
            problems.append(f"DUPLICATE_NAME {m.name}")
        names.add(m.name)
        if m.command not in COMMANDS:
            problems.append(f"UNKNOWN_COMMAND {m.name} {m.command}")
        target = copy / m.path
        if not target.is_file():
            problems.append(f"MISSING_FILE {m.name} {m.path}")
            continue
        n = target.read_text().count(m.old)
        if n != 1:
            problems.append(f"{'NOT_FOUND' if n == 0 else 'NOT_UNIQUE'} {m.name} {m.path} occurrences={n}")
        if m.old == m.new:
            problems.append(f"NO_CHANGE {m.name}")
    return problems


def run(copy: pathlib.Path, command: str, env: dict[str, str], timeout: int) -> tuple[str, int, str, float]:
    argv, extra = COMMANDS[command]
    t0 = time.monotonic()
    try:
        p = subprocess.run(argv, cwd=copy, env={**env, **extra}, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired as exc:
        out = (exc.stdout or b"").decode(errors="replace") if isinstance(exc.stdout, bytes) else (exc.stdout or "")
        return "TIMEOUT", -1, out, time.monotonic() - t0
    out = p.stdout + p.stderr
    if p.returncode == 0:
        verdict = "SURVIVED"
    elif BUILD_FAILURE.search(out):
        verdict = "BUILD_FAILED"
    else:
        verdict = "KILLED"
    return verdict, p.returncode, out, time.monotonic() - t0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--rev", default="HEAD")
    ap.add_argument("--only", default="", help="comma-separated mutant names")
    ap.add_argument("--check", action="store_true", help="validate search strings only")
    ap.add_argument("--logdir", default="", help="write each command's full output here")
    ap.add_argument("--keep", action="store_true", help="keep the temporary copy and GOCACHE")
    ap.add_argument("--timeout", type=int, default=900, help="seconds per command")
    args = ap.parse_args()

    root = repo_root()
    commit = git(root, "rev-parse", f"{args.rev}^{{commit}}")
    tree = git(root, "rev-parse", f"{args.rev}^{{tree}}")
    only = {n for n in args.only.split(",") if n}
    unknown = only - {m.name for m in MUTANTS}
    if unknown:
        print(f"unknown mutant names: {sorted(unknown)}")
        return 2
    selected = [m for m in MUTANTS if not only or m.name in only]

    work = pathlib.Path(tempfile.mkdtemp(prefix="goal06-mutants-"))
    if work.resolve().is_relative_to(root.resolve()):
        print(f"temporary directory {work} is inside the repository")
        return 2
    copy = work / "tree"
    copy.mkdir()
    logdir = pathlib.Path(args.logdir).resolve() if args.logdir else None
    if logdir:
        logdir.mkdir(parents=True, exist_ok=True)
    print(f"revision {args.rev} commit {commit} tree {tree}")
    print(f"copy {copy} (git archive)")
    print(f"mutants {len(selected)} of {len(MUTANTS)}")
    try:
        export(root, commit, copy)
        problems = validate(copy, selected)
        for line in problems:
            print(line)
        if problems:
            print(f"RESULT search-string problems={len(problems)}; no test was run")
            return 1
        print("search strings: every one occurs exactly once")
        if args.check:
            return 0

        env = dict(os.environ, GOPROXY="off", GOCACHE=str(work / "gocache"))
        version = subprocess.run(["go", "version"], cwd=copy, capture_output=True, text=True, env=env)
        print(f"go {version.stdout.strip()} {version.stderr.strip()}".rstrip())
        for name, (argv, extra) in COMMANDS.items():
            print(f"command {name}: {' '.join(f'{k}={v}' for k, v in extra.items())} {' '.join(argv)}")

        for command in sorted({m.command for m in selected}):
            verdict, rc, out, secs = run(copy, command, env, args.timeout)
            if logdir:
                (logdir / f"control-{command}.txt").write_text(out)
            ok = verdict == "SURVIVED"
            print(f"CONTROL {command} {'PASS' if ok else 'FAIL'} exit={rc} secs={secs:.1f}", flush=True)
            if not ok:
                print(out[-4000:])
                print("RESULT control failed; mutants not run")
                return 1

        tally: dict[str, int] = {}
        bad = []
        for m in selected:
            target = copy / m.path
            original = target.read_text()
            target.write_text(original.replace(m.old, m.new, 1))
            try:
                verdict, rc, out, secs = run(copy, m.command, env, args.timeout)
            finally:
                target.write_text(original)
            tally[verdict] = tally.get(verdict, 0) + 1
            if logdir:
                (logdir / f"{m.name}.txt").write_text(out)
            fails = FAIL_LINE.findall(out)
            shown = " ".join(dict.fromkeys(fails))[:300]
            print(f"{verdict} {m.name} exit={rc} secs={secs:.1f} file={m.path} failing={shown or '-'}", flush=True)
            print(f"    reason: {m.reason}", flush=True)
            if verdict != "KILLED":
                bad.append(m.name)
                print("    output tail:\n" + "\n".join("    " + line for line in out.strip().splitlines()[-25:]), flush=True)
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
