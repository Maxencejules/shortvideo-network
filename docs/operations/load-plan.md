# Load plan

Development procedure for one measurement on the machine that runs it. This is not a production runbook. Public launch is disabled. The numbers below are the procedure. A result is not a universal hardware minimum and not an operating budget.

The blueprint tables on pages 23 and 24 are arithmetic. They are not demand, not a measurement, and not a vendor quote. Peer share is not measured. This plan does not fill it with zero and does not subtract it from host traffic.

## What is fixed before a run

| Item | Value |
| --- | --- |
| Revision | `git rev-parse HEAD` and `git rev-parse HEAD^{tree}` at the start of the run |
| Worker concurrency | 1 |
| Playback samples | 8, sequential |
| Processing limit | 120 seconds to ready-for-review |
| Startup limit | 3000 milliseconds |
| Rebuffer limit | 0.01 |
| Downlink | tbf rate 20mbit, burst 256kbit, latency 400ms |
| Delay | netem delay 25ms on that tbf |
| Loss | netem loss is unset |
| Shape body | 5000000 bytes |
| Ping band | average of 10 pings from 40 ms through 80 ms |
| Goodput band | 10 through 30 megabits per second on the shape body |
| Accounts | none; the playback grant is a local delivery grant |
| Spend | none; this machine only |

There is no approved integration runner and no resource budget. Those are DR-28-01 and DR-28-02. The runner does not start 100 viewers and does not start 10 upload sessions. It does not rent a machine.

## Corpus and cache

The input is a synthetic `testsrc` of 1280x720 at 10 frames per second plus a 440 Hz sine, 60 seconds, written by the local FFmpeg. It is not a camera recording. The worker encodes `h480` and `h720` with libx264 `veryfast`. The file is placed in the worker quarantine on this disk. The processing clock starts at `run_once` and does not include generating that file or a network upload.

Each playback sample uses a new Chrome profile. The delivery process drops its ETag memory for the content id before the sample. The first response is a cold response. `Cache-Control` on a segment is the existing `private, max-age=60`. A 304 is not a cold sample.

The product client takes the first master-playlist variant (`apps/web/src/playback.ts`). The capacity page does the same. The delivery probe page takes the last variant. This plan measures the product order.

## Network limit

The limit is a network namespace whose loopback carries the qdisc. The delivery process and the browser run in that namespace, so the segment bytes cross the shaped loopback. The commands are:

```
ip netns add svncap28
ip netns exec svncap28 ip link set lo up
ip netns exec svncap28 tc qdisc add dev lo root handle 1: tbf rate 20mbit burst 256kbit latency 400ms
ip netns exec svncap28 tc qdisc add dev lo parent 1:1 handle 10: netem delay 25ms
```

`tc qdisc show` is part of the result. The shape counts only when that text contains the rate (`20Mbit` or `20mbit`) and the delay (`25.0ms` or `25ms`), the ping average is inside 40 ms through 80 ms, the 5000000-byte goodput is inside 10 through 30 megabits per second, and ping reports 0% loss. A miss is BLOCKED. The playback samples are not started. The bands are not moved after the result.

Burst `256kbit` is above `rate/HZ` for a 100 Hz or 250 Hz kernel so the qdisc can be installed. It is not a result.

## Denominators

Startup is `renderMs`: milliseconds from the start of the page script to the first decoded frame that has a non-zero canvas. Chrome's process start is not included. `fetchMs` is the segment `arrayBuffer` time. The server `handler_ms` is the delivery handler time for that segment response. Those three are different. A sample is a playback success only when the report has `rendered` true, the segment SHA-256 matches the file, the server recorded one `200` for that segment with the file's byte count, and `mediaMs` is at least nine tenths of the probed media duration. A segment download without `rendered` true is not a success.

`mediaMs` is `video.currentTime` at the end of the sample, in milliseconds. `stallMs` is time in the `waiting` state after that first frame and before the sample ends. Rebuffer ratio is `stall_ms / media_ms`. The aggregate ratio is the sum of `stallMs` over the sum of `mediaMs` for the successful samples. It is not the mean of the per-sample ratios.

p95 startup uses nearest-rank on the successful samples' `renderMs`, sorted ascending. The rank is `(95 * n + 99) // 100`. The value is the sample at that rank, counting from 1. A sample that is not a success fails the gate. Its `renderMs` is not inserted as zero.

The processing gate is the `run_once` wall time. Moderation, replication, and publication of that 60-second object are separate. The worker record stays `NOT_PUBLISHED`. This plan does not invent those three times.

Chain bytes are the signed transaction length. Gas is the committed `gas_used`. State is the sum of key and value bytes in the application stores, not IAVL disk bytes. Catch-up is `replayFresh` of that fixture. The scheduler repair fixture is 64 bytes on simulated providers in one process group. It is not a repair of the 60-second package and not a second machine.

CPU busy is `(total - idle) / total` from the first `/proc/stat` line over the processing window. The idle field is the fourth number. Iowait counts as busy. RSS is the peak sum of `VmRSS` for the runner and its descendants.

## Arithmetic, kept apart from the run

Viewer payload for the small-beta row is `100 * 20 * 60 * 2 * 30 / 8000` = 900 GB. Stored bytes for that row are `20 * 25 * 3 * 30 / 1000` = 45 GB. The illustrative coordination example is `200 * 4` = 800 transactions per day, which is `800/86400` transactions per second. None of those figures is a result of this run.

Traffic classes stay in separate columns: viewer payload, replication, repair, prefetch, protocol overhead, and relay. Overhead and relay are unmeasured. An unmeasured class is left blank.
