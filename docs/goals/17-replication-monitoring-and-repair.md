GOAL 17 / 30



Replication, monitoring and repair
  GPT-6.1 Sol                                                                       EFFORT: HIGH

Prerequisites: Goal 16 and canonical lease read models from Goal 12.
Blueprint reference: pp. 5-6, 13-15, 22, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 17. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Coordinate publication and repair across designated hosts without treating promises or node counts as
durable storage proof.
IMPLEMENTATION REQUIREMENTS

1. Implement an off-chain scheduler using admitted operator/failure-domain records, compatible policies,
    declared capacity and measured reachability. Require explicit provider acceptance; do not let the scheduler
    sign for providers or perform external work inside consensus.
2. For the proposed beta, require community approval, verified package transfer and three active
    operator-distinct commitments before publication. Keep observed reachability, evidence mode and
    committed lease state separate. Local simulated operator labels must be disclosed as simulation.
3. Build bounded retrieval monitoring and an AT_RISK availability view. Select replacements, transfer from a
    surviving replica, verify bytes and confirm the new commitment before a planned departure removes
    needed data. Use idempotent repair jobs and avoid retry storms.
4. Implement renewal notices, deadlines, provider draining and orderly handoff. Respect withdrawal and
    removal policy; repair must not resurrect withdrawn content. During chain stalls, queue new final
    commitments and label any temporary off-chain service accurately.
5. Prepare three active test providers plus one spare or a safely provisioned replacement for the repair test.
    Track origin backups separately from counted replicas and report the actual physical failure domains.
ACCEPTANCE CHECKS

Publish a permitted package, disconnect creator/origin and one provider, retrieve from surviving hosts, and
restore the replica target with the spare. Verify repaired bytes and chain accounting.
Test simultaneous failures, a dishonest receipt, low capacity, stale evidence, renewal expiry and duplicate
repair work. Document inability to recover when all reachable copies are lost.
Drain a provider without prematurely deleting the last needed copy; remove content during repair and ensure
distribution stops on cooperating services.
REQUIRED HANDOFF

Produce failure/repair traces, scheduler policy and honest availability status. Mark independent-operator
verification separately from single-host topology tests.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                23 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                               CODEX CLOUD + GITHUB



