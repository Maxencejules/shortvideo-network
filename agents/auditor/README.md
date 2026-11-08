# agents/auditor

Off-chain audit producer for permitted test endpoints (goal 09). An audit result is an attestation by an auditor. It is not a storage proof, not a unique-replica proof, and not independent: every run in this repository is one operator on one machine.

This module is a separate Go module (`github.com/Maxencejules/shortvideo-network/agents/auditor`, listed in `go.work`). It imports from the chain module `chain/jcs`, `chain/genesis`, `chain/msgkit`, `chain/x/evidence` (every signed byte), `chain/x/providers/capability`, and `chain/app` (only `MakeEncoding`, for the outer transaction), plus the CometBFT RPC client and the Cosmos SDK transaction builder at the chain's versions. The chain module never imports it, and the validator binary `svnd` does not carry an HTTP fetcher. Go placement is decision request DR-09-20.

## What exists

A command, `auditor` (`main.go`, built with `go build -C agents/auditor .`), and its libraries with tests. The commands separate network use from key use (owner rulings on Q1 to Q4, 2026-11-02):

| Command | Network | Key | Does |
| --- | --- | --- | --- |
| `keygen --key PATH` | none | writes | a fresh dev-only ed25519 key, mode 0600, never over an existing file; prints the address and public key only |
| `run --config FILE` | the chain RPC (reads) and provider endpoints (policy below) | never opens it | reads the latest header, each held lease, its content, and (with a capability manifest) the provider; fetches and checks; stores every report, INCOMPLETE ones included, and a pending run |
| `sign --config FILE --run RUN_ID` | none | reads | builds one batch per provider from the run's PASS and FAIL audits, signs it, checks it with `evidence.VerifyAuditSignature`, and stores the batch and its envelopes |
| `submit --config FILE --batch ID ...` | the chain RPC only | reads | broadcasts, waits for a committed result, reads the records back |
| `status --config FILE --batch ID ...` | the chain RPC only | none | reports the submission state from committed results and records; broadcasts nothing |
| `verify --evidence-id ID --store DIR (--node URL \| --record FILE)` | the given node only | none | the reviewer's check |

Output is one JSON object per line on stdout. Exit codes: 0 done; 1 an input, store, chain-read, or key error; 2 usage; 3 finished but not final (a batch not recorded, or a review verdict other than CONSISTENT). No command prints key bytes; error text is printed quoted.

| Package | Does |
| --- | --- |
| `config` | Strict JSON operator configuration: exact keys, no unknown or duplicate key, no null, at most 1 MiB. No field disables a check. Auditor fields: `auditor_address`, `key_path`, `key_id`, `evidence_lifetime_seconds` (60 to 86400, default 86400), `epoch_guard_seconds` (30 to 1800, default 120), `inclusion_timeout_seconds` (5 to 600, default 60), `gas_limit` (default 2,000,000), `max_batch_items` (1 to 32), `http_status_retries` (1 to 5, default 3), `chain_rpc_timeout_ms`. The lifetime must exceed guard + inclusion. Every default is PROPOSED. |
| `policy` | Destination policy (PROPOSED, DR-09-10). URL rules, the address rule against the pinned IANA table, the allowlist. |
| `fetch` | HTTPS requests to vetted addresses. Typed outcomes. |
| `fetch/fetchtest` | Loopback test endpoints for tests. Only tests import it. |
| `verify` | Manifest parsing and digest, interim package layout, segment checks, sample selection. It returns observations. |
| `classify` | PASS, FAIL, or INCOMPLETE under the owner's Q3 ruling and the endpoint-source rule (N2) (table below). |
| `report` | Canonical report encoding, format `svn-audit-report-proposed-v0` (DR-09-22). |
| `store` | Content-addressed report store and append-only index. |
| `keys` | Dev-only ed25519 key files (owner's Q1 ruling). |
| `batch` | Eligibility, evidence ids, batches, envelopes, signing, and the stored run and batch formats, through `chain/x/evidence` (owner's Q2 and Q4 rulings). |
| `chainclient` | Chain reads, the outer transaction, and submission states. |
| `chainclient/chaintest` | An in-memory fake of the RPC methods for unit tests only ("unit-test fake chain RPC; not a node, not end-to-end evidence"). Only tests import it. |
| `review` | The reviewer's verdicts. |
| `labels` | The evidence label catalog (P3b). Reports carry its limitations; the reviewer's output carries its labels. |

### Keys (owner ruling on Q1, DR-09-25)

The auditor account key signs both the SDK transactions and the attestations: the chain requires the attestation public key to derive the outer signer's address. A key file is the `svnd devnet init` format: the lowercase hex of the 64-byte SDK ed25519 key (seed, then public key) and a newline. `keys.Load` refuses a symlink, a FIFO or other non-regular file, a file readable or writable by group or others, any other format, and a public half that is not the seed's. A `Key` formats as its address only. There is no rotation: the address is the key. These are development keys; no live validator, governance, or production key is handled.

`run` writes the configured `auditor_address` into reports and never opens `key_path` (`TestRunNeverOpensKeyPath` makes the key path a FIFO). `sign` and `submit` refuse a key whose address is not `auditor_address`.

### Classification (owner ruling on Q3, "Provider-answered only")

| Observation | Result | Reason |
| --- | --- | --- |
| every checked segment received in full with the manifest digest | PASS | none |
| a segment received in full with another digest | FAIL | `SEGMENT_DIGEST_MISMATCH` |
| fewer bytes than expected under the provider's own framing (`LENGTH_MISMATCH`: a declared `Content-Length` below the size, or a chunked body that ends early) | FAIL | `PACKAGE_LENGTH_MISMATCH` |
| a declared length above the size, or bytes past it | FAIL | `TOO_LARGE` |
| 404 or 410 | FAIL at once | `HTTP_STATUS` |
| any other 4xx or 5xx, except the three statuses of the `TIMEOUT` and `RANGE_UNSUPPORTED` rows | FAIL only if it is still the answer after `http_status_retries` retries (backoff 1 s, doubling); INCOMPLETE `RUN_BUDGET_EXHAUSTED` if the run budget ends first | `HTTP_STATUS` |
| another status (for example 204) | INCOMPLETE | `HTTP_STATUS_UNEXPECTED` |
| `CONNECT_FAILED`, `TLS_FAILED`, `TIMEOUT` (including a 408 answer), `CONNECTION_RESET` (a cut inside a declared length or a chunk, or a close-delimited body that ends early), `PROTOCOL_ERROR`, `DNS_FAILURE`, `REDIRECT_REFUSED`, `ENCODING_REFUSED`, `RANGE_UNSUPPORTED` (a 200, 416, or 501 answer to a range request, or a `Content-Range` that is not the requested range of the expected total) | INCOMPLETE | the kind |
| a policy refusal | INCOMPLETE | `POLICY_SCHEME`, `POLICY_PORT`, `POLICY_URL` (length, syntax, userinfo, fragment, path, host), `ENDPOINT_NOT_PERMITTED` (address, no address, too many addresses) |
| an input or eligibility problem, before any request | INCOMPLETE | `INPUT_MANIFEST_INVALID`, `INPUT_MANIFEST_MISMATCH`, `INPUT_CAPABILITY_INVALID`, `INPUT_CONTENT_WITHDRAWN`, `INPUT_CONTENT_NOT_FOUND`, `LEASE_NOT_HELD`, `LEASE_NEAR_DEADLINE`, `CHAIN_READ_FAILED`, `CONFIG_INVALID` |
| an outcome kind, rule, or state `classify` does not know | INCOMPLETE | `UNCLASSIFIED` (never FAIL) |
| any row above that is FAIL, when the endpoint came from the operator's `endpoints` list (report `endpoint_source` `OPERATOR_CONFIG`) | INCOMPLETE | `ENDPOINT_UNBOUND`, beside the row's own reason |

Every request of a `SAMPLED_SEGMENTS` audit is a range request, a goal-09 extension: protocol v0 defines only the 200 answer to a whole-package request. A 416 or 501 refuses the range and says nothing about the bytes the provider holds, so it is `RANGE_UNSUPPORTED`. The trade-off: a truncated package fails a `WHOLE_PACKAGE` audit (`PACKAGE_LENGTH_MISMATCH`), while a sampled audit of it is INCOMPLETE. A 408 is the server saying the request did not arrive in time; the ruling keeps timeouts off chain, so it is `TIMEOUT`. Both are interim choices under DR-09-09 and DR-09-22.

A FAIL reason wins over INCOMPLETE reasons in the same audit: a segment received in full over the verified connection with the wrong digest is the provider's answer even if the connection was cut later. `TestEveryFetchKindIsMapped`, `TestEveryPolicyRuleIsMapped`, and `TestEveryVerifyStateIsMapped` read the constants from the source of `fetch`, `policy`, and `verify` and fail if one is not mapped; `TestFailReasonsAreTheRuling` pins the four FAIL reasons. INCOMPLETE audits stay off chain; their reports are kept.

#### Endpoint source (interim choice N2, 2026-11-06)

Q3 names "the provider's verified server". A certificate shows only that the server holds the host name, not that the host is the provider's. An audit takes its endpoint from one of two sources, and every report records which in `endpoint_source`:

- `CAPABILITY_MANIFEST`: `capability_manifest_path` names a capability manifest file. Before any request, `run` requires it to parse, to name the audit's provider and the provider record's current signing key id, and to hash to the record's committed `endpoint_manifest_digest`; otherwise the audit is INCOMPLETE `INPUT_CAPABILITY_INVALID`. The detached manifest signature is not checked (protocol v0 defines no wire envelope for it, DR-07-09); the committed digest binds the body.
- `OPERATOR_CONFIG`: the operator typed the endpoint into `endpoints`. Nothing binds it to the provider.

The field names the configured source: an audit whose capability manifest fails the check is INCOMPLETE `INPUT_CAPABILITY_INVALID`, fetches nothing, and its report still says `CAPABILITY_MANIFEST`.

Only `CAPABILITY_MANIFEST` allows FAIL. With `OPERATOR_CONFIG`, a result that would be FAIL is INCOMPLETE with the added reason `ENDPOINT_UNBOUND`: the endpoint was not declared by the provider account (no capability manifest bound to its committed `endpoint_manifest_digest`). Even a `CAPABILITY_MANIFEST` endpoint shows only that the provider account declared it, not that the provider controls the host. The provider-answered reasons and the failed segments stay in the report, so the operator sees what the server answered; the audit is retried the same way (a 5xx is still retried before `HTTP_STATUS` is listed); nothing is signed and no `REJECTED` record follows. The rule lives where `classify` decides the result (`Observation` and `Reasons` take the source), so no caller gets a FAIL without it. `batch.NewItem` refuses a FAIL report whose `endpoint_source` is not `CAPABILITY_MANIFEST` (`ErrIneligible`). The paths that re-sign or broadcast stored items without `NewItem` read each FAIL item's stored report again (`batch.CheckFailSources`): the CONFLICT, EXPIRED, or WITHDRAWN rebuild drops such an item, and `submit` refuses a stored batch that holds one before any broadcast, naming the evidence id (for example a batch signed before reports recorded the field). The reviewer reports such a signed item as `RECORD_MISMATCH`.

PASS does not depend on the source: a PASS from an operator-configured endpoint is PASS, is signed, and is `RECORDED` on chain until its expiry (at most 86400 s, PROPOSED); its report says `OPERATOR_CONFIG`, and the reviewer's output carries `report_endpoint_source`. The chain cannot see the source. This asymmetry is a residual risk: a PASS can describe a server that is not the provider's. N2 is an interim choice, reversible; the owner approved the recommendation in general terms ("go with your recommendations for N1 and N2"), and the option was named in the orchestrator's message after the approval.

### Eligibility and rounds (owner ruling on Q4)

Before any request, `run` reads the latest header (a node that is catching up is refused), the held lease of the (provider, content) pair through `Query/ActiveLeases`, and `Query/Content`. Only the configured lease, ACTIVE or RENEWED with its reservation held, with a deadline after header time + guard + inclusion, of registered content that is not withdrawn and has no tombstone, is fetched. Without a held lease no report is written (nothing to describe); the pending run records `LEASE_NOT_HELD`.

`sign` uses the run's header time: epoch = `EpochAt(header time)` (3600 s rounds, PROPOSED), expiry = min(header time + lifetime, lease deadline), never above min(header time + 86400, lease deadline) and never inside the inclusion window. It refuses (`ErrRoundClosing`) when the run's header time or the signing host's clock is within the guard of the end of the run's epoch or past it. The host clock is not verified; it can only add a refusal.

### Batches and envelopes (owner ruling on Q2)

Every signed byte comes from `chain/x/evidence` (`AuditEnvelope`, `AuditSignBytes`, `VerifyAuditSignature`, `IDPrefix`, `EpochAt`); nothing is re-implemented. `TestProducerMatchesChainSignVectors` rebuilds the chain's golden sign vectors through the producer's own path and requires equal canonical text, sign bytes, and signatures.

- evidence id: `IDPrefix(address)` (16 hex characters of the address, then `.`) and the first 47 hex characters of the report's SHA-256
- observation digest: the lease manifest digest for PASS (the chain stores RECORDED), the full SHA-256 of the report for FAIL (REJECTED, DEC-010); INCOMPLETE produces no item, and a FAIL report whose `endpoint_source` is not `CAPABILITY_MANIFEST` is refused (`ErrIneligible`, N2)
- one batch per provider, at most `max_batch_items` items, ordered by evidence id
- batch id: prefix and 47 hex characters of SHA-256 of `{"auditor", "epoch", "evidence_ids"}` (SVN-JCS-1)
- business id: attempt 0 (the one the signed batch carries) is the prefix and 47 hex characters of SHA-256 of `"svn-audit-business:" + batch_id + ":0"`. The next attempt, made only after the chain answered REPLAY, is the prefix and 47 hex characters of 32 bytes from `crypto/rand`, so another account that sees attempt 0 cannot compute and commit the retry ids first (`TestSquattedBusinessIDsDoNotBlockTheBatch`). The business id is not signed, so a new attempt keeps the auditor signature.

`tests/fixtures/evidence/golden` holds a golden batch the producer writes (`TestGoldenEnvelopeFixtures`, `-update` rewrites it); `tests/unit/test_evidence_envelopes.py` checks it in Python.

### Submission: an acknowledgement is not a commit

`broadcast_tx_sync` answers CheckTx only. States, from the chain:

| State | Meaning |
| --- | --- |
| `NOT_SUBMITTED` | no stored submission, no record |
| `NOT_ACCEPTED(codespace/code)` | every stored submission was refused by CheckTx |
| `SUBMITTED` | acknowledged, or the answer was lost, and no committed result is visible; never recorded. A rebroadcast that CheckTx refuses while an earlier submission waits (on a node, `sdk/32`: the waiting transaction holds the sequence) keeps it `SUBMITTED`, with the warning `REBROADCAST_NOT_ACCEPTED` |
| `COMMITTED(0)` | a stored submission is in a block with code 0; items are read back with `Query/Evidence` |
| `FAILED(code)` | the latest committed submission has a non-zero code |
| `RECORDS_FOUND` | no committed submission of this store is visible, but records hold the evidence ids |

Items read back: `RECORDED`, `REJECTED`, or `EXPIRED` when the record equals the batch item (signer key, batch id, lease, content, version, kind, digest, epoch, expiry); `ID_TAKEN` when it differs; `NOT_ON_CHAIN` when absent. A retry first queries every stored transaction hash and the evidence ids; only then does it broadcast again with the current account sequence. REPLAY moves to the next business attempt. CONFLICT, EXPIRED, or WITHDRAWN re-reads each item's lease and content, drops the items that changed (reported in `dropped`), and submits the rest as a new batch in the same epoch (`next`; its summary names the old batch in `supersedes`). Another epoch than the batch's adds the warning `EPOCH_LIKELY_STALE`; a batch never submitted is still broadcast (the chain answers STALE_VERSION and nothing changes), a stale SUBMITTED batch is not broadcast again, and any other failure is reported as it is. An index line is appended only for a committed result.

Each submission stores the transaction bytes (`batches/<name>.pb`) before the broadcast and the record with the CheckTx answer (`batches/<name>.json`) after it. Bytes without a record (the process stopped, or the record write failed, in between) are a submission whose answer is unknown: `status` and `submit` query its hash like any other, so a transaction that commits is still found and indexed. The bytes must carry the batch's own message: submission 0 under the batch's own business id, a later submission under the business id of the one before it (the same attempt) or a new id with the auditor's prefix (the next attempt, read back from the bytes, so a retry after a stop keeps its drawn id). A record without bytes, or bytes that carry anything else, is an error.

The outer transaction is SIGN_MODE_DIRECT with the app's own encoding, no fee, no memo, and the auditor account's number and sequence from the auth query. The chain RPC client uses no proxy, follows no redirect, bounds each request in time, and bounds each response body at 16 MiB. Reads trust the one node they ask.

### Reviewer (`verify`)

It prints `record_source` first: `NODE <url>` ("read through an unverified ABCI query; trusts that node") or `FILE <path>` ("not read from the chain"). Verdicts, first failure wins: `NOT_ON_CHAIN` (node only), `PAYLOAD_UNAVAILABLE`, `CHECKSUM_MISMATCH`, `REPORT_INVALID`, `SIGNATURE_INVALID`, `ENVELOPE_MISMATCH`, `RECORD_MISMATCH`, `CONSISTENT`. `RECORD_MISMATCH` also covers a report the producer never attests: an INCOMPLETE report, or a FAIL report whose `endpoint_source` is not `CAPABILITY_MANIFEST` (N2). CONSISTENT is still an attestation: a valid signature and a consistent payload do not make an observation true. The output carries `claim_limit`, the report's result and `endpoint_source` (`report_endpoint_source`), the mode and state labels, and the catalog's limitations.

## Network policy (PROPOSED, DR-09-10)

- URL: `https` only, at most 2048 bytes, no userinfo, no fragment, no query, no path other than `/`, ASCII host. A host whose last label starts with a digit (`127.1`, `2130706433`, `0x7f000001`) is refused: no top-level domain starts with a digit, and some resolvers read such names as IPv4 addresses.
- Path: `/packages/{content_id}/{content_version}`, built from chain values and percent-encoded. Never taken from a response.
- Resolution: once per session, through an injectable resolver. Every returned address must pass. One denied address refuses the whole name. The session dials only the vetted addresses by literal, and the dialer's `Control` hook refuses any other socket address. Later requests reuse the pinned addresses; there is no second lookup.
- Address rule: deny unless globally reachable. An address is global only when no deny row of the pinned table contains it and, for IPv6, it is inside `2000::/3`. A `True` row inside a deny row stays denied (for example `192.0.0.9`, `2001:1::1`).
- Unmapping: `::ffff:a.b.c.d` is judged and dialed as `a.b.c.d`. NAT64 `64:ff9b::/96` and 6to4 `2002::/16` carry an IPv4 address that is judged first; the IPv6 address is then judged too, and both prefixes are denied under this table (`64:ff9b::/96` is outside `2000::/3`; `2002::/16` is a Globally Reachable N/A row). Local-use NAT64 `64:ff9b:1::/48` and `2001::/23` (Teredo included) are deny rows. Network-specific NAT64 prefixes cannot be recognised; that is a residual risk.
- Allowlist: a non-global destination, or a global one on a port other than 443, is permitted only by an entry `(provider_id, host, ip, port)` that matches all four exactly. Entries are scoped to one provider. There is no CIDR, wildcard, "allow private" switch, or global disable. An entry may not name a link-local, unspecified, multicast, or broadcast address, nor one inside a NAT64 or 6to4 address. An IPv6 entry address must be `::1`, unique-local (`fc00::/7`), NAT64 `64:ff9b::/96`, or inside `2000::/3`; other IPv6 forms, such as IPv4-compatible `::/96` and IPv4-translated `::ffff:0:0:0/96` addresses, are refused.
- Redirects: never followed. A 3xx keeps only the SHA-256 of its `Location`.
- TLS: system roots, or an explicit CA file that replaces them. TLS 1.2 minimum. SNI and verification use the original host name.
- Proxies: none. `Transport.Proxy` is nil, so `HTTPS_PROXY`, `HTTP_PROXY`, and `NO_PROXY` are ignored.
- Encoding: `Accept-Encoding: identity`; transparent decompression is off; any other `Content-Encoding`, on any header line, is refused.
- Short bodies: `LENGTH_MISMATCH` only when the provider's framing says so inside TLS (a declared `Content-Length` below the expected size, or a chunked body whose last chunk comes early). A body cut inside a declared length, or a body with neither a `Content-Length` nor chunked framing that ends early, is `CONNECTION_RESET`: a TCP close at a TLS record boundary reads as a clean end, and the client cannot tell it from `close_notify`.
- Bounds: connect, TLS handshake, response header, and idle read timeouts; a per-object budget; a run budget (`run`); 16 KiB of response headers; one connection per session; a body read of at most expected + 1 bytes; 65536-byte manifests; 16 resolved addresses; 64 allowlist entries.

The configured chain RPC URL is trusted configuration, not a provider endpoint. The destination policy does not apply to it.

### Pinned special-purpose table

`policy/table.go` holds the IANA IPv4 and IPv6 Special-Purpose Address registries (now titled "IPv4/IPv6 Special-Purpose Address Space"), registry Last Updated 2025-10-09, fetched on 2026-11-02 from:

- https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv
- https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv

Unchanged copies are in `policy/testdata`. `TestTableMatchesRegistrySnapshot` checks their SHA-256 and compares every registry row with the table. Rows not in the registry are marked with their RFC: `224.0.0.0/4`, `::/96`, `fec0::/10`, `ff00::/8`. A registry change after 2025-10-09 is not reflected; refreshing the table is a code change.

## Test endpoints and the test seam

Tests use `fetch/fetchtest`: an `httptest` TLS server on `127.0.0.1` with a CA generated at run time, serving synthetic bytes generated at test time. Label: "loopback test endpoint, synthetic package, not a provider agent". Nothing is committed media.

Loopback is denied by default. Tests reach it in two ways:

1. The production path: an exact allowlist entry for `127.0.0.1:<ephemeral port>` (or a test host name that an injected resolver maps to `127.0.0.1`). The same URL without its entry is refused.
2. A test seam for destinations that cannot exist on loopback (a global address on port 443). `fetch.Client` has an unexported field `connect`; when set, the session hands it the vetted address instead of dialing. Tests set it to record the vetted address and connect to the loopback server. It cannot be enabled from outside the package or from configuration: `New` leaves it nil (`TestSeamNotReachableThroughOptions`), `fetch.Options` has no function field, the configuration holds only data and refuses unknown keys (`TestConfigHasNoBypass`), and a source scan finds the assignment only in `fetch` test files (`TestTestSeamOnlySetInTests`). The seam still receives only addresses that passed the policy.

`TestFetchIgnoresProxyEnvironment` runs in a re-executed child process with `HTTPS_PROXY` and `HTTP_PROXY` pointing at a counting listener and `NO_PROXY` empty, fetches a non-loopback host name, and requires success with zero proxy connections.

## Interim package rules (DR-09-21)

The manifest is an operator file of at most 65536 bytes. Its digest is SHA-256 over its SVN-JCS-1 text and must equal the lease's manifest digest; its content id, version, and package bytes must equal the lease's. Otherwise nothing is fetched. The test-endpoint package is the segments concatenated in manifest order. A segment with `byte_count` 0 must carry the SHA-256 of empty input, or the manifest is invalid and nothing is fetched. A sampled audit draws a fresh 32-byte seed from the OS and checks the first k segments ordered by SHA-256(seed || segment_id), one `Range` request each. Range answers are a goal-09 test-endpoint extension; v0 defines only 200. "A sampled check covers only the sampled segments."

## Report store

Layout: `reports/<sha256>.json`, `envelopes/<evidence_id>.json`, `batches/<batch_id>.pb` (the signed message, business attempt 0) and `batches/<batch_id>.json` (the batch summary: evidence id to full report SHA-256), `batches/<submission name>.json` and `.pb` (one record and the transaction bytes per broadcast), `pending/<run_id>.json` (the pending run, at most 48 audits: it is one SVN-JCS-1 text, capped at 65536 bytes, and `TestWorstCaseRunFitsTheEncodingLimit` checks that 48 audits fit with every field at its largest; `run` refuses a longer audits list before it reads the chain or fetches anything), `index.jsonl` (one line per item of a committed result). Writes are create-exclusive through a synced temporary file and a hard link; the same bytes again are a no-op; other bytes under an existing name are refused. Directories are 0700 and files 0600. The default root belongs under `.local/`, which Git ignores. `index.jsonl` takes whole lines only: after a torn append (a last line without its newline), appends are refused and scans report the index as corrupt; a line that is not one JSON object is reported as corrupt, never handed on.

Every report records `endpoint_source` (`CAPABILITY_MANIFEST` or `OPERATOR_CONFIG`; required). The field was added on 2026-11-06 under N2 without a new format string: the format is interim and unreleased (DR-09-22), and a report written before the change does not parse (the reviewer reports `REPORT_INVALID`).

Reports carry the label catalog's limitations that apply to them: all but "Accounting success is not physical capacity or operator independence." (it is about lease accounting), and "A sampled check covers only the sampled segments." only in sampled reports. More than the report's 8 is an error, never a silent cut. An audit with a capability manifest that ended before the manifest named the endpoint records `endpoint_name_sha256` of the empty name. A sampled audit that ended before any selection (an input or eligibility reason) records a fresh seed that selected nothing, because the report format requires one.

Retention: none set (approved DEC-004 default); nothing is deleted.

## Tests

```bash
go test -count=1 -race ./agents/auditor/...
```

Parser fuzz targets (plan section 21.7), each with a seed-corpus test that its seeds reach the accepting path and the refusals: `FuzzURLPolicy` (`policy.ParseEndpoint`; `TestURLPolicySeeds`), `FuzzManifestParse` (`verify.ParseManifest` and `NewLayout`; `TestManifestSeeds`), `FuzzReportParse` (`report.Parse`, re-encoding to the same bytes; `TestReportSeeds`), and `FuzzConfigParse` (`config.Parse`; `TestConfigSeeds`). A plain `go test` runs only their seeds; a fuzz run is `go test -run '^$' -fuzz '^FuzzURLPolicy$' -fuzztime=15s ./agents/auditor/policy/`.

No test uses the network beyond loopback. Times in reports come from the auditor host clock and are not verified. The chain in the command and `chainclient` tests is `chaintest`, a unit-test fake: those tests are unit tests, not end-to-end evidence. The devnet test runs the built binary against real nodes.
