#!/usr/bin/env bash
# Four-process devnet proof. Logs height and app hash for each phase.
# Does not delete a genesis after a failure. The test home is a temp directory.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${root}"

echo "COMMAND bash scripts/devnet/prove-process.sh"
echo "DATE $(date -Is)"
echo "TREE $(git rev-parse 'HEAD^{tree}')"
if [[ -n "$(git status --porcelain)" ]]; then echo "DIRTY yes"; else echo "DIRTY no"; fi
echo "NOTE empty blocks only. No application transaction is submitted, so the hash is the genesis application state."

set +e
go test -count=1 -timeout 180s -v ./chain/devnet/ -run 'TestInitRefusesToReplaceGenesis|TestFourValidatorsCommitRestartAndQuorum'
status=$?
set -e
echo "EXIT:${status}"
exit "${status}"
