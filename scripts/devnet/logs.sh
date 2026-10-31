#!/usr/bin/env bash
# Show the validator logs. This does not rotate or delete them.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
home="${SVN_HOME:-${root}/.local/devnet/current}"

for name in v1 v2 v3 v4; do
  log="${home}/nodes/${name}/node.log"
  echo "--- ${name} ${log} ---"
  if [[ -f "${log}" ]]; then
    tail -n 40 "${log}"
  else
    echo "no log"
  fi
done
