#!/usr/bin/env bash
# Run one command and write an evidence log.
#
# usage: scripts/evidence/capture.sh <log-file> -- <command> [args...]
#
# The log starts with COMMAND, DATE, HEAD, TREE and the dirty state, then
# the command output, then EXIT:<code>. DIRTY lists paths changed outside
# docs/evidence/ so a reader can see whether the tested code was committed.
# The script exits with the command's exit code. It does not retry and it
# does not hide a failure.
set -uo pipefail

if [[ $# -lt 3 || "$2" != "--" ]]; then
  echo "usage: $0 <log-file> -- <command> [args...]" >&2
  exit 2
fi

log="$1"
shift 2
root="$(git rev-parse --show-toplevel)"
mkdir -p "$(dirname "$log")"

outside="$(git -C "$root" status --porcelain --untracked-files=all | awk '{print $NF}' | grep -v '^docs/evidence/' || true)"

{
  printf 'COMMAND'
  printf ' %q' "$@"
  printf '\n'
  echo "DATE $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "HEAD $(git -C "$root" rev-parse HEAD)"
  echo "TREE $(git -C "$root" rev-parse 'HEAD^{tree}')"
  if [[ -n "$outside" ]]; then
    echo "DIRTY yes"
    while IFS= read -r path; do
      echo "DIRTY_PATH $path"
    done <<<"$outside"
  else
    echo "DIRTY no"
  fi
} >"$log"

"$@" >>"$log" 2>&1
status=$?
echo "EXIT:${status}" >>"$log"
exit "$status"
