# Uploads and quarantine

Status of this note: PROPOSED. It is the goal 13 integration record. It is not an approved account contract, not an approved allowance, and not an approved object-storage adapter. DR-13-01 through DR-13-04 are open. Public launch: DISABLED. No tradable token.

`cmd/svn-api` is the process. `svn_api.serve` does not listen.

## Routes

| Method and path | Who | Result |
| --- | --- | --- |
| `POST /upload-intents` | Bearer session, role `creator` | `202` and a `Transfer` token bound to one upload. Reserves input bytes and a separate output budget, or returns the existing row for the same idempotency key and the same declared size. |
| `PUT /uploads/{upload_id}/content` | That upload's `Transfer` token, before it expires | Writes the body under a generated name. A session token cannot call this route. |
| `POST /uploads/{upload_id}/complete` | The owning session | Checks the caller's digest, then a bounded `ffprobe` child. One `QUEUED` job. A repeated call returns that same job. |
| `GET /uploads/{upload_id}` | The owning session | Status. Another account gets `404`. |
| `DELETE /uploads/{upload_id}` | The owning session | Cancels and releases the reservation once. |
| `POST /publish` | A session | `403` `PUBLICATION_BLOCKED`. |

There is no `/signup` or `/accounts` route. Unregistered paths, including playback and the quarantine name, do not return the bytes.

Errors are `{"error":{"code","message"}}`. The message does not include a path, a token, or another account id.

## State

`RESERVED`, `RECEIVING`, `RECEIVED`, `PROBING`, `PROCESSING_QUEUED`, `REJECTED`, `EXPIRED`, `CANCELLED`. The database check rejects `PUBLISHED`.

`PROCESSING_QUEUED` keeps the reservation. Abandoned `RESERVED`, `RECEIVING`, `RECEIVED`, and `PROBING` rows are expired by sweep, the reservation is returned once, and the file is removed. A second sweep does not return it again.

`api.processing_jobs.status` can only be `QUEUED`. `api.chain_outbox.status` can only be `HELD`. The payload is `account_id`, `content_id`, `input_sha256`, `input_bytes`, `output_budget_bytes`, and `schema_version`. Filenames, MIME types, tokens, and the object path are not in it. This process does not submit that row to the chain.

## Later goals

Goal 14 reads `api.processing_jobs` and the quarantine object named by `api.uploads.object_name`. It does not publish. The isolated worker and the output ladder are that goal. The `ffprobe` child here is a deadline-bounded probe, not that sandbox.

Goal 15 must not serve the quarantine directory.

Goal 11 is not called. The held row is not a broadcaster outbox entry.

Goal 19 replaces the development verifier. Until then a production configuration refuses the fixture file and refuses to start without a verifier.
