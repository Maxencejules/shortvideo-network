# Operator quickstart

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. This quickstart installs a local candidate. It does not make the candidate a release, and it does not show independent operation.

## Limits

There is no audit, no interoperability result, and no second operator in this goal. Goals 28-30 and the human launch gates have not passed. The licence file is not applied. Video bytes stay off the chain. There is no token.

## Install the tools

The pins are [../dependencies.md](../dependencies.md).

```bash
bash scripts/install_toolchains.sh
export PATH="$(pwd)/.toolchains/go/bin:$(pwd)/.toolchains/bin:$PATH"
python3 -m venv .venv
.venv/bin/pip install --require-hashes -r requirements.lock
```

FFmpeg must be Ubuntu `7:6.1.1-3ubuntu5`. The obligations are [ffmpeg-obligations.md](ffmpeg-obligations.md).

## Run the candidate

```bash
test -z "$(git status --porcelain)"
OUT="${TMPDIR:-/tmp}/svn-candidate"
rmdir "$OUT" 2>/dev/null || true
mkdir "$OUT"
.venv/bin/python scripts/packaging.py candidate --out "$OUT"
```

Read [deployment-profiles.md](deployment-profiles.md) before choosing a host. Keep the media worker off the validator host. The profile document is `deployments/profiles/profiles.json`.

## Read next

- Protocol and API: [../api-reference.md](../api-reference.md)
- Signing: [signing-procedure.md](signing-procedure.md)
- Migration and recovery: [migration-and-recovery.md](migration-and-recovery.md)
- Backups: [backup-and-restore.md](backup-and-restore.md)
- Governance: [governance-notes.md](governance-notes.md)
