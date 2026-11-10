# Storage-provider agent

`svn-provider` is the development storage agent. It is one process. Two processes on one machine are not independent operators and are not physical redundancy. Public launch is disabled. There is no token. The agent does not put video bytes or personal data on chain.

This note is PROPOSED. DR-16-01 through DR-16-06 are open. Production provider keys are not used. The process binds to a loopback address only (DR-16-03).

## Install

The config file has no private key and no admin token. `hosting_consent` must be true or `init` and `run` refuse to start.

```text
svn-provider init config.json
svn-provider run config.json
```

`init` creates, under `data_dir`, mode `0700`:

- `keys/1.hex`, the development ed25519 key, mode `0600`. This is not a production key.
- `admin.token`, mode `0600`.
- `tls.crt` and `tls.key`, a one-day loopback certificate.
- `enrollment.json`, the public key, the provider id, and `production_keys: false`.

The operator writes `source.token` (the bearer sent when fetching a package) and `approvals.json`:

```json
{"offer_ids":["oca"]}
```

An offer id that is not in that file is refused. The agent re-reads the file on each offer.

`run` listens on the loopback address in `listen` and writes `listen.json` with the bound `https://` URL. `GET /health` is the process check. `GET /status` is the readiness, quota, disk, and chain-observation report.

## What it signs

Signatures use the chain helpers. A service key is not an input.

| Record | Helper |
| --- | --- |
| Capability manifest | `chain/x/providers/capability` |
| Lease acceptance | `leases.AcceptSignBytes` |
| End and local withdrawal | `leases.EndSignBytes` |
| Key rotation | `providers.RotateSignBytes` |
| Drain | `providers.DrainSignBytes` |
| Outer transaction | `SIGN_MODE_DIRECT`, the provider key, no fee |

The acceptance transaction is built by this process (the provider self-submission path). It is posted to the operator's chain URL. The HTTP acknowledgement is not a commit. Readiness stays `ACCEPTANCE_PENDING` until that endpoint reports the lease `ACTIVE` or `RENEWED`.

Receipt and inventory canonical fields are DR-16-01 and DR-16-02. Both say `ATTESTATION_NOT_STORAGE_PROOF`.

## Readiness

`TRANSFERRING`, then `STAGED` after the digest and the byte count match, then `ACCEPTANCE_PENDING` after the provider signs, then `COMMITTED` when the chain endpoint reports the lease and the deadline is inside the clock-uncertainty window, then `SERVING` when the deadline is outside that window. `GET /packages/...` requires `SERVING` and the bearer returned with the acceptance.

A partial transfer is deleted on the next start. Used bytes are counted again from the leases whose files are still present.

A stalled chain (the endpoint's height does not advance for `stall_after_seconds`, or the endpoint errors, or `catching_up` is true) sets `chain_uncertain`. A new acceptance is not promoted and is not broadcast. A lease that is already `SERVING` stays available until its real-time deadline. The deadline is compared with the process clock. An expired lease is removed locally. That removal is not a chain end.

`POST /admin/drain`, `POST /admin/withdraw`, and `POST /admin/rotate` require the admin bearer. Drain refuses new offers and keeps serving. Rotate refuses while an acceptance is still pending, signs with the current key, and does not keep the old private key. Withdraw deletes the local bytes and signs an end. `chain_end` in that response is false.

## Fetches

Package and chain URLs are `https` only. Redirects are refused. The body is limited to the segment's byte count. A private or loopback address is dialed only when `destinations` or `chain_destinations` names that host, IP, and port. There is no switch that allows every private address. Link-local addresses, including `169.254.0.0/16`, are refused even if named. The dial uses the vetted IP. `HTTP_PROXY` is not used.
