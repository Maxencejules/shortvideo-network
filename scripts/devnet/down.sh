#!/usr/bin/env bash
# Stop validator processes. Data and genesis stay on disk.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
home="${SVN_HOME:-${root}/.local/devnet/current}"

if [[ ! -d "${home}/nodes" ]]; then
  echo "no devnet home at ${home}" >&2
  exit 2
fi

for name in v1 v2 v3 v4; do
  pidfile="${home}/nodes/${name}/svnd.pid"
  if [[ ! -f "${pidfile}" ]]; then
    continue
  fi
  pid="$(cat "${pidfile}")"
  if kill -0 "${pid}" 2>/dev/null; then
    kill -TERM "${pid}"
  fi
done

for name in v1 v2 v3 v4; do
  pidfile="${home}/nodes/${name}/svnd.pid"
  if [[ ! -f "${pidfile}" ]]; then
    continue
  fi
  pid="$(cat "${pidfile}")"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if ! kill -0 "${pid}" 2>/dev/null; then
      break
    fi
    sleep 0.5
  done
  if kill -0 "${pid}" 2>/dev/null; then
    echo "validator ${name} pid ${pid} did not stop" >&2
    exit 1
  fi
done
echo "stopped validators in ${home}. Genesis was kept."
