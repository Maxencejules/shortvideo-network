# apps/web

Invited development viewer. React 19.3.1 and TypeScript 7.0.2 are pinned in `package.json`. esbuild 0.28.2 bundles the browser script. The bundle is not committed.

The viewer plays HLS from the delivery adapter by fetching a bounded segment window and handing one MP4 to a video element. Chromium on Linux has no HLS demuxer, so a playlist URL is not assigned to the video. Candidate hosts, retries, and the prefetch window match the goal 15 limits. Those limits are proposed.

Home lists content from the indexer and from ids selected in this client. Discover is that list. Search, activity, follow, block, report, and comments stay unavailable until later goals. Profile talks to the account service for a session, passkey enrollment, export, backup, and deletion. Recovery is not available. There is no wallet and no peer upload.

Public launch is disabled. A playback grant is not publication. A development fixture is not a live creator.
