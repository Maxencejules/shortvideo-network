# Storage-provider agent

Status of this note: PROPOSED. It is the goal 16 install and state record. It is not an approved allowance, not an approved chain-observation protocol, and not a claim of operator independence. DR-16-01 through DR-16-06 are open. Public launch: DISABLED. No tradable token.

The program is `agents/provider/cmd/svn-provider`. Install steps are in [agents/provider/README.md](../../agents/provider/README.md).

## Consent and bounds

`hosting_consent` must be true. Each offer id must be listed in the operator's `approvals.json`. Before any byte is staged the agent checks the operator quota, the declared capacity, the bandwidth budget (`bandwidth_bytes_per_second` times `transfer_budget_seconds`), and the free space `statfs` reports for the data directory. The free space must cover the package plus `min_free_bytes`. These numbers are operator configuration. They are not DEC-005 allowances (DR-16-05).

## Signatures

The provider key signs the capability manifest, the acceptance, the end, the rotation, and the drain, through the helpers in `chain/x/providers/capability`, `chain/x/leases`, and `chain/x/providers`. The same key signs the outer `SIGN_MODE_DIRECT` transaction. No service key is accepted as that signature. A field named like a supplied acceptance signature is rejected as an unknown field.

Video bytes stay in the data directory. The transaction carries the acceptance, not the segment bytes.

## Chain observation

The chain URL is operator configuration (DR-16-04). The agent reads `/status` (a CometBFT status subset: height and `catching_up`), `/account`, and `/lease/{id}`, and posts the signed transaction to `/broadcast`. A broadcast response is not a commit. `SERVING` requires the lease endpoint to say `ACTIVE` or `RENEWED`, a height that is still advancing, and a deadline outside the configured clock-uncertainty window. This is not a light client. `light_client` in the status document is false.

## One machine

`independent_operator` and `physical_redundancy` are false. The status text says that several identities on one machine are not independent ownership or physical redundancy. A second process with its own key does not change that (DR-16-06).
