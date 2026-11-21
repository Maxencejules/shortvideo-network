# Packaging and installation

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. The candidate is NOT-RELEASED. DEC-006 is not applied as a licence file.

## Secure defaults

The install writes `etc/svn-install.json` with listen `127.0.0.1`, `default_credentials` false, and `release_label` `NOT-RELEASED`. It does not copy a shared private key. The only secret it creates is `var/dev/local-token`, mode `0600`, labelled `DEVELOPMENT-ONLY-NOT-A-CONSENSUS-KEY`, from bytes generated at install time. That file is not in the bundle and is not a consensus key.

## Smoke scenario

From a clean checkout of the pinned commit, with the virtualenv and Go 1.26.10 on `PATH`, and with FFmpeg `6.1.1-3ubuntu5`:

```bash
test -z "$(git status --porcelain)"
git rev-parse HEAD
git rev-parse 'HEAD^{tree}'
.venv/bin/python scripts/packaging.py candidate --out "$OUT"
```

`OUT` is an empty directory outside the worktree. The command builds each packaged binary twice, refuses the candidate when the hashes differ, writes `OUT/bundle` with `SHA256SUMS`, installs into `OUT/prefix`, runs the smoke, rehearses the prefix switch, and uninstalls. Operator data planted at `OUT/prefix/operator-data` is still there after uninstall. The command does not tag, publish, or open a port.

`SVN_PACKAGING_ALLOW_DIRTY=1` is not part of this scenario. A dirty tree is refused without it.

Verify a bundle that was already built:

```bash
.venv/bin/python scripts/packaging.py verify --bundle "$OUT/bundle"
( cd "$OUT/bundle" && sha256sum -c SHA256SUMS )
```

`make release-check` runs the fast gate: profiles, notices, image text, the workflow policy, the pinned FFmpeg version, the SBOM, and the same install path with fixture binaries. `make release-candidate OUT="$OUT"` runs the scenario above.

## What is non-reproducible on purpose

The dev fixture token changes every install. It is not a build output. `go.sum` pins module downloads. The apt index used to install the pinned FFmpeg package is not a content digest; the package version and the recorded deb checksum are. Two builds of one clean commit use `CGO_ENABLED=0`, `-trimpath`, and `-buildvcs=true`, and unset `SOURCE_DATE_EPOCH`.

## Uninstall

Uninstall deletes only paths listed in `INSTALL-MANIFEST.json` under that prefix. It refuses `/` and a directory that is not an install. It leaves files that the install did not create.
