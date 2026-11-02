# agents/auditor

Off-chain audit producer for permitted test endpoints (goal 09). An audit result is an attestation by an auditor. It is not a storage proof, not a unique-replica proof, and not independent: every run in this repository is one operator on one machine.

This module is a separate Go module (`github.com/Maxencejules/shortvideo-network/agents/auditor`, listed in `go.work`). It imports `chain/jcs` and `chain/genesis` from the chain module. The chain module never imports it, and the validator binary `svnd` does not carry an HTTP fetcher. Go placement is decision request DR-09-20.

## What exists

Libraries with tests. There is no command yet.

| Package | Does |
| --- | --- |
| `config` | Strict JSON operator configuration: exact keys, no unknown or duplicate key, no null, at most 1 MiB. No field disables a check. |
| `policy` | Destination policy (PROPOSED, DR-09-10). URL rules, the address rule against the pinned IANA table, the allowlist. |
| `fetch` | HTTPS requests to vetted addresses. Typed outcomes. |
| `fetch/fetchtest` | Loopback test endpoints for tests. Only tests import it. |
| `verify` | Manifest parsing and digest, interim package layout, segment checks, sample selection. It returns observations. |
| `report` | Canonical report encoding, format `svn-audit-report-proposed-v0` (DR-09-22). |
| `store` | Content-addressed report store and append-only index. |

Not here yet, because they depend on open owner questions: mapping outcomes to FAIL or INCOMPLETE (Q3), auditor keys (Q1), batch construction and signing (Q2), epochs and evidence lifetime (Q4), the CLI, chain reads, submission, the reviewer `verify` command, evidence labels, and envelope files.

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
- Bounds: connect, TLS handshake, response header, and idle read timeouts; a per-object budget; a run budget setting (the command that applies it is not written yet); 16 KiB of response headers; one connection per session; a body read of at most expected + 1 bytes; 65536-byte manifests; 16 resolved addresses; 64 allowlist entries.

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

Layout: `reports/<sha256>.json`, `envelopes/`, `batches/`, `pending/`, `index.jsonl`. Writes are create-exclusive through a synced temporary file and a hard link; the same bytes again are a no-op; other bytes under an existing name are refused. Directories are 0700 and files 0600. The default root belongs under `.local/`, which Git ignores. `index.jsonl` takes whole lines only: after a torn append (a last line without its newline), appends are refused and scans report the index as corrupt; a line that is not one JSON object is reported as corrupt, never handed on.

Retention: none set (approved DEC-004 default); nothing is deleted.

## Tests

```bash
go test -count=1 -race ./agents/auditor/...
```

No test uses the network beyond loopback. Times in reports come from the auditor host clock and are not verified.
