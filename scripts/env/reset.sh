#!/usr/bin/env bash
# Delete one named disposable workspace. Requires --confirm equal to the name.
# Package installs, other workspaces, and the git checkout are not removed.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-reset"

workspace=""
confirm=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --workspace)
      workspace="${2:-}"
      shift 2
      ;;
    --confirm)
      confirm="${2:-}"
      shift 2
      ;;
    --from|--clone|--clone-from)
      die "refusing to clone a workspace or a validator identity" 2
      ;;
    *)
      die "unknown argument: $1" 2
      ;;
  esac
done

if [[ -z "${workspace}" || "${confirm}" != "${workspace}" ]]; then
  die "refusing to reset. Pass --workspace NAME --confirm NAME. NAME is one disposable workspace." 2
fi

# Do not apply the dev default. The name was supplied on purpose.
SVN_WORKSPACE="${workspace}"
ws="$(svn_workspace_path "${workspace}")" || exit $?
root="$(svn_state_root)" || exit $?
case "${ws}" in
  "${root}"|"${root}/")
    die "refusing to delete the state root" 2
    ;;
  "${root}/"*) ;;
  *)
    die "refusing to delete a path outside the state root" 2
    ;;
esac
if [[ ! -d "${ws}" ]]; then
  say "workspace ${ws} is already absent"
  exit 0
fi
if [[ ! -f "${ws}/.svn-workspace" ]]; then
  die "refusing to delete ${ws}: missing .svn-workspace. reset only removes a workspace created by start.sh" 2
fi
marker="$(tr -d '[:space:]' < "${ws}/.svn-workspace")"
if [[ "${marker}" != "svn-dev-workspace" ]]; then
  die "refusing to delete ${ws}: .svn-workspace is not the start.sh marker" 2
fi

SVN_WORKSPACE="${workspace}" bash "${_ENV_DIR}/stop.sh"
rm -rf "${ws}"
if [[ -e "${ws}" ]]; then
  die "failed to delete ${ws}"
fi
say "deleted disposable workspace ${workspace} at ${ws}"
