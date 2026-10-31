#!/usr/bin/env bash
# Start validators again from the existing homes. Genesis is not rewritten.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
home="${SVN_HOME:-${root}/.local/devnet/current}"
binary="${SVN_BIN:-${root}/bin/svnd}"

if [[ ! -f "${home}/genesis.json" ]]; then
  echo "no genesis at ${home}. Refusing to create one from resume." >&2
  exit 2
fi
if [[ ! -x "${binary}" ]]; then
  echo "missing ${binary}. Run scripts/devnet/up.sh once to build it." >&2
  exit 2
fi

for name in v1 v2 v3 v4; do
  pidfile="${home}/nodes/${name}/svnd.pid"
  if [[ -f "${pidfile}" ]] && kill -0 "$(cat "${pidfile}")" 2>/dev/null; then
    echo "${name} is already running" >&2
    exit 2
  fi
  log="${home}/nodes/${name}/node.log"
  nohup "${binary}" start --home "${home}/nodes/${name}" >>"${log}" 2>&1 &
  echo $! >"${pidfile}"
done
echo "resumed four validators in ${home}"
