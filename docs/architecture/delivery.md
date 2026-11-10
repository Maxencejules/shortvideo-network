# HTTPS playback

Status of this note: PROPOSED. It is the goal 15 integration record. It is not an approved package format, not an approved publication state, and not an approved allowance. DR-15-01 through DR-15-06 are open. DR-14-02 stays open. Public launch: DISABLED. No tradable token. P2P, TURN, WebRTC, and public IPFS are disabled.

`python -m svn_delivery` is the process. It is not the upload API and it is not the chain.

## What it serves

The worker root is the goal 14 root. A package is eligible when `jobs/ready/*.json` says `READY_FOR_REVIEW` and `publication` is `NOT_PUBLISHED`, and a playback grant names that content version and manifest SHA-256.

| Route | Bytes |
| --- | --- |
| `GET /v1/playback/{content_id}` | Publication, removal, grant, chain note, and candidate hosts. `hosting_promise` is false. |
| `GET /v1/media/{content_id}/v{version}/master.m3u8` | HLS master. URIs are relative. |
| `GET /v1/media/{content_id}/v{version}/{rendition}.m3u8` | One rendition playlist. |
| `GET /v1/media/{content_id}/v{version}/segments/{file}` | One manifest segment, after its hash matches. |
| `GET /v1/player/{content_id}` | The test page. It is not the product client. |

Quarantine paths, `..`, percent-encoding, and the disabled names `ipfs`, `p2p`, `turn`, and `webrtc` are not served. The listener refuses any bind address that is not loopback. TLS is required. The certificate is generated when the process starts, names only `localhost` and `127.0.0.1`, and is not a public CA certificate. The private key stays in a temp directory.

The cross-origin policy echoes `Access-Control-Allow-Origin` only for `https://playback.test.local`. It does not send `*`. Methods are GET, HEAD, and OPTIONS. The allowed request headers are `Range` and `Content-Type`.

Segment responses accept one `bytes` range and send `206`. Playlists and JSON ignore a range and send the whole small body. Content types are `application/vnd.apple.mpegurl`, `video/mp4`, `image/jpeg`, and `text/vtt`.

## Grant, removal, and a chain note

`authorize` writes `playback/{content_id}.json`. It refuses unless the operator chain note is `HELD`. The note is a file this process stores. This process does not query the chain and does not submit a transaction.

A note of `STALE` or `UNAVAILABLE` leaves an existing unexpired grant playable. The playback document then says `chain_status: STALE`, `stale: true`, and `hosting_promise: false`. A new grant, and a host that was not already on the grant, are refused. The term is `authorized_until` on the process clock, at most 24 hours after the grant. That clock is not chain time.

`remove` sets the grant to `REMOVED`, drops issued ETags, and later byte requests return 404 with `Cache-Control: private, no-store`. A conditional request with the old ETag is not a 304. Copies outside this process are not deleted.

While a segment is advertised, its cache header is `private, max-age=60` and its ETag is the quoted SHA-256. Playlists use `private, max-age=0, must-revalidate`. Those lifetimes are the DR-15-03 proposal.

## Hosts

`origin-test` reads `packages/<content_id>/v<version>/`. A test replica is a directory accepted only after every segment hash and the manifest digest match that origin package. The harness tries at most four hosts and at most two attempts on each host, then at most one fallback rendition. It does not follow redirects. It refuses a URL that is not `https` on `127.0.0.1` or `localhost`.

## Integrity coverage

This is the claim boundary. It is not a statement that a particular run already proved it. The evidence file records the run.

| Layer | What is checked | What a failure does |
| --- | --- | --- |
| Origin, before a host is listed | Schema, canonical manifest bytes, ready-record digest, every segment hash and length, ffprobe of each video segment | The host is omitted. `package_verified` is false. |
| Origin, on the bytes it sends | The buffer's SHA-256 and length match the manifest segment | `409` and a JSON error. The media bytes are not the body. |
| Replica acceptance | The same checks, and the manifest digest equals the origin package | The replica is not stored and not advertised. |
| Harness | SHA-256 of each video segment and caption file it fetched. Segments past the prefetch window are not fetched. | The play result is not ok. `rendered` stays 0. |
| Test page in a headless browser | The page fetches one rendition's first media segment, hashes it, and assigns those bytes to a `video` element. A passing run has matching width and height, `readyState` at least 2, and a non-zero canvas sample. | A missing browser, or a report that fails those checks, is not a render. |

The page does not use native HLS. Chromium on Linux has no HLS demuxer. Segments the page did not fetch are not client-verified. An HTTP response increments `http_responses` and does not increment `rendered`.

Aggregates are sums: startup milliseconds, rebuffer milliseconds, playback errors, delivered video bytes, playlist bytes, other bytes, HTTP responses, renders, and plays. The export has no content id and no watch history.

## What this goal does not do

It does not publish. It does not write a hosting lease. It does not start a provider, a TURN server, a peer connection, or a public IPFS node. Goal 16 is the independent host. The FFmpeg `file` protocol whitelist in the worker is unchanged.
