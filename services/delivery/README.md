# Delivery

Status of this note's behavior: PROPOSED. The package format is still DR-14-02. A local playback grant is not publication and not a hosting lease. See `docs/architecture/delivery.md`.

`python -m svn_delivery serve --root <worker-root>` listens on loopback with an ephemeral certificate. It serves a ready-for-review package only after `note-chain --note HELD` and `authorize`. Quarantine bytes are not served. P2P, TURN, WebRTC, and public IPFS are disabled.
