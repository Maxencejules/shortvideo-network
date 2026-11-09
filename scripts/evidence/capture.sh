#!/usr/bin/env bash
# Run one command and write an evidence log.
#
# usage: scripts/evidence/capture.sh <log-file> -- <command> [args...]
#
# The log starts with COMMAND, DATE, HEAD, TREE and the dirty state, then
# the command output, then EXIT:<code>. DIRTY is yes when the worktree
# differs from HEAD. Untracked files count, including files under
# docs/evidence/. The log this command is about to write is ignored, because
# it does not exist yet when the status is read and it is not a change to
# the tested tree. The script exits with the command's exit code. It does
# not retry and it does not hide a failure.
set -uo pipefail

if [[ $# -lt 3 || "$2" != "--" ]]; then
  echo "usage: $0 <log-file> -- <command> [args...]" >&2
  exit 2
fi

log="$1"
shift 2
root="$(git rev-parse --show-toplevel)"
mkdir -p "$(dirname "$log")"

# Status is read before the log file is created. Ignore that one path anyway,
# so a reader can see every other untracked or modified file.
log_abs="$(readlink -f "$log" 2>/dev/null || realpath "$log")"
log_rel="$(python3 -c 'import os,sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$log_abs" "$root")"
outside="$(
  git -C "$root" status --porcelain=v1 --untracked-files=all | while IFS= read -r line; do
    path="${line:3}"
    case "$path" in
      *" -> "*) path="${path##* -> }" ;;
    esac
    if [[ "$path" != "$log_rel" ]]; then
      printf '%s\n' "$path"
    fi
  done
)"

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
