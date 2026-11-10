# Scheduler

`svn-scheduler` is the off-chain publication and repair coordinator. It proposes hosts. It does not sign for them, and it does not write chain state. A provider signature is that provider's acceptance. A broadcast response is not a commit. A count of processes is not durable storage.

This note is PROPOSED. DR-17-01 through DR-17-08 are open. Public launch is disabled. There is no token.

## Run

The config has no private key.

```text
svn-scheduler serve config.json
```

`listen` is the loopback read API. `indexer_url` is the Goal 12 read model (`GET /v1/status`, `/v1/leases`, `/v1/content`, `/v1/providers`). `physical_failure_domain` is the machine this process is actually on. Declared operator and failure-domain strings in the host records are claims. When `simulated_labels` is true, the availability view says so.

`GET /health` is the process check. `GET /v1/availability?content=` is the publication, reachability, and lease view. Those three are separate fields. `GET /v1/distribution/{content}/{version}` serves bytes only from a reachable verified replica. It does not serve the origin backup. Withdrawn content is refused.

## Publication

The proposed beta parameter is three active operator-distinct commitments, plus community approval, plus a verified byte transfer. The origin backup is tracked and is not one of the three. Evidence mode is `PROVIDER_ACCEPTANCE`, `STALE`, `DISHONEST`, or `NONE`. The claim limit stays `ATTESTATION_NOT_STORAGE_PROOF`. `storage_proof` is false.

A stale indexer read queues the content as `TEMPORARY_OFF_CHAIN`. That queue is not a committed lease and not a renewal.

## Repair

A repair job is one key per content version. A second call while the job is running or cooling down does not post another offer. The coordinator copies from a reachable replica, checks the bytes, and counts the replacement only after the read model shows the new lease active. A planned departure is refused while the other verified copies are below the target. Withdrawn content cancels the job and stops cooperating distribution. If every reachable replica is gone, the result is unrecoverable. The origin backup is not used as a substitute replica.
