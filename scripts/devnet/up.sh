#!/usr/bin/env bash
# Start four validator processes from one new genesis.
# A home that already has files is refused. This script does not delete one.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
home="${SVN_HOME:-${root}/.local/devnet/current}"
binary="${SVN_BIN:-${root}/bin/svnd}"

if [[ ! -x "${binary}" ]]; then
  mkdir -p "$(dirname "${binary}")"
  go build -C "${root}/chain" -o "${binary}" ./cmd/svnd
fi

if [[ -d "${home}" ]] && [[ -n "$(ls -A "${home}" 2>/dev/null || true)" ]]; then
  echo "devnet home ${home} already exists. Refusing to replace it. Stop it or choose a new SVN_HOME." >&2
  exit 2
fi

mkdir -p "$(dirname "${home}")"
"${binary}" devnet init --home "${home}" --peers loopback
for name in v1 v2 v3 v4; do
  log="${home}/nodes/${name}/node.log"
  pid="${home}/nodes/${name}/svnd.pid"
  nohup "${binary}" start --home "${home}/nodes/${name}" >"${log}" 2>&1 &
  echo $! >"${pid}"
done
echo "started four validators in ${home}"
echo "status: bash scripts/devnet/status.sh"
echo "stop: bash scripts/devnet/down.sh"
