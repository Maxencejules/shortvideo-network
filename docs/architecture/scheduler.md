# Replication scheduler

Status of this note: PROPOSED. It is the goal 17 selection and repair record. It is not an approved allowance, not a durability guarantee, and not a claim that several processes on one machine are independent operators. DR-17-01 through DR-17-08 are open. Public launch: DISABLED. No tradable token.

The program is `services/scheduler/cmd/svn-scheduler`. The rules it applies are in `services/scheduler/internal/policy`.

## What the scheduler will not do

- It does not sign an acceptance, a renewal, or an end. The provider process signs those with its own key.
- It does not broadcast a transaction and it does not treat a broadcast response as a commit.
- It does not run inside consensus and it does not fetch bytes from a chain handler.
- It does not count an origin backup as a replica.
- It does not treat a declared operator string or a declared failure domain as a physical fact.
- It does not call a receipt a storage proof. The claim limit is `ATTESTATION_NOT_STORAGE_PROOF`.

## Selection

A host is proposed only when all of these hold:

- the indexer provider row says `ADMITTED`
- the host is not draining
- the role is `replica`, or `spare` when the caller is repairing
- the role is not `origin_backup`
- the policy id matches
- declared capacity, minus bytes the provider reports as used, covers the package
- a bounded health probe succeeded
- the declared operator label is not already chosen for this package

Publication asks for three such replicas and does not spend the spare. Repair asks for one host that does not already hold the package. The target three is the proposed beta parameter. It is not a measured durability result.

## Separate records

| Record | Where it lives | What it is not |
| --- | --- | --- |
| Lease state | Indexer rows `ACTIVE` or `RENEWED` | Not reachability and not approval |
| Reachability | A fresh probe and a byte fetch | Not a lease transition. `REACHABILITY_DEGRADED` is the off-chain encoding of the proposed label `AT_RISK` |
| Evidence mode | Compared receipt and measurement time | Not a lease extension. A stale or mismatched receipt does not clear `AT_RISK` |
| Publication | Community approval plus verified transfers plus the target commitments | Not a chain content state |

Once those publication conditions have held, a later missed probe stays a reachability failure. It does not by itself withdraw the approval. Withdrawal does stop distribution.

## Repair, drain, stall

The job key is the content id and version. A running job and a job inside its cooldown do not start another attempt. The attempt cap is three. The copy is staged only after a survivor fetch matches the package hash, then the spare pulls that buffer, then the buffer is discarded. The buffer is not a counted replica.

Departure of a cooperating provider is allowed only when the other verified reachable copies already meet the target. The scheduler then asks that provider to withdraw the one lease. It does not delete the last needed copy first.

A renewal is a notice (`RENEWAL_NEEDED` or `EXPIRED`). No renewal transaction is built here. An expired notice does not change the lease row.

If the indexer status is stale, or the indexer cannot be read, new final offers are queued and labeled `TEMPORARY_OFF_CHAIN`. Existing serving copies are not rewritten into a success.

## This workspace

Every provider process started by the goal 17 conformance test is on one machine. The physical failure domain count is one. Declared labels such as `sim-p1` are simulation. `independent_operator_verification` is false. A second person on another machine is not this test.
