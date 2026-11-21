#!/usr/bin/env bash
# Stamp one command for goal 30 evidence.
#
# usage: scripts/acceptance/stamp.sh <log-file> -- <command> [args...]
#
# Line 1 is HEAD, line 2 is TREE, line 3 is CMD, line 4 is DIRTY.
# The command's output follows. EXIT is the command status. AFTER is the
# worktree status once the command returns. The log path itself is ignored.
# The script exits with the command's status. It does not retry.
set -uo pipefail

if [[ $# -lt 3 || "$2" != "--" ]]; then
  echo "usage: $0 <log-file> -- <command> [args...]" >&2
  exit 2
fi

log="$1"
shift 2
root="$(git rev-parse --show-toplevel)"
mkdir -p "$(dirname "$log")"

log_abs="$(readlink -f "$log" 2>/dev/null || realpath "$log")"
log_rel="$(python3 -c 'import os,sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$log_abs" "$root")"

dirty_paths() {
  git -C "$root" status --porcelain=v1 --untracked-files=all | while IFS= read -r line; do
    path="${line:3}"
    case "$path" in
      *" -> "*) path="${path##* -> }" ;;
    esac
    if [[ "$path" != "$log_rel" ]]; then
      printf '%s\n' "$path"
    fi
  done
}

{
  echo "HEAD $(git -C "$root" rev-parse HEAD)"
  echo "TREE $(git -C "$root" rev-parse 'HEAD^{tree}')"
  printf 'CMD'
  printf ' %q' "$@"
  printf '\n'
  before="$(dirty_paths)"
  if [[ -n "$before" ]]; then
    echo "DIRTY yes"
    while IFS= read -r path; do
      echo "DIRTY_PATH $path"
    done <<<"$before"
  else
    echo "DIRTY no"
  fi
} >"$log"

"$@" >>"$log" 2>&1
status=$?
{
  echo "EXIT:${status}"
  after="$(dirty_paths)"
  if [[ -n "$after" ]]; then
    echo "AFTER dirty"
    while IFS= read -r path; do
      echo "AFTER_PATH $path"
    done <<<"$after"
  else
    echo "AFTER clean"
  fi
} >>"$log"
exit "$status"
