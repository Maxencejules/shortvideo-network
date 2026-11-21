# Signing procedure

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. A signature made by this procedure is not a release signature.

## Digests

The candidate writes `SHA256SUMS` in `sha256sum` format. Check it with either command:

```bash
.venv/bin/python scripts/packaging.py verify --bundle "$OUT/bundle"
( cd "$OUT/bundle" && sha256sum -c SHA256SUMS )
```

A mismatch refuses the install.

## Test signature

`candidate` generates an Ed25519 key under `$OUT/test-signing` after the bundle is checksummed. The public file starts with `SVN-TEST-SIGNING-KEY-NOT-FOR-A-RELEASE`. The private file is mode `0600` and is deleted before the command returns. The key directory is not inside the bundle. The key is generated in that run. It is not committed, and it is not a consensus key.

That signature only shows that the checksum file can be signed. It does not authorise a release.

## Owner signature

Release signing is an owner action. The owner signs `SHA256SUMS` with a key this repository does not hold, keeps that key outside the tree, and does not commit it. The release-candidate workflow in `.github/workflows/release-candidate.yml` starts only when someone dispatches it. It has `contents: read` and cannot push a tag or a package. Each pull request runs the packaging check in the unit tests. That check does not build the candidate binaries. A repository environment that requires a second reviewer is not named in that workflow. This goal does not name a publisher and does not create a tag.
