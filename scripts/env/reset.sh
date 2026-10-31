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
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        die "--workspace requires a workspace name. Exit 2." 2
      fi
      workspace="$2"
      shift 2
      ;;
    --confirm)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        die "--confirm requires the workspace name. Exit 2." 2
      fi
      confirm="$2"
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
  keys_only=0
  if [[ -f "${ws}/devnet/validator.key" && -f "${ws}/devnet/validator.pub" ]]; then
    keys_only=1
    while IFS= read -r entry; do
      base="$(basename "${entry}")"
      case "${base}" in
        devnet|validator.key|validator.pub) ;;
        *) keys_only=0 ;;
      esac
    done < <(find "${ws}" -mindepth 1)
  fi
  if [[ "${keys_only}" -ne 1 ]]; then
    die "refusing to delete ${ws}: missing .svn-workspace. reset removes a workspace created by start.sh, or a workspace that contains only a devnet identity." 2
  fi
  say "deleting a keys-only workspace. It has a devnet identity and no service state."
else
  marker="$(tr -d '[:space:]' < "${ws}/.svn-workspace")"
  if [[ "${marker}" != "svn-dev-workspace" ]]; then
    die "refusing to delete ${ws}: .svn-workspace is not the start.sh marker" 2
  fi
fi

SVN_WORKSPACE="${workspace}" bash "${_ENV_DIR}/stop.sh"
rm -rf "${ws}"
if [[ -e "${ws}" ]]; then
  die "failed to delete ${ws}"
fi
say "deleted disposable workspace ${workspace} at ${ws}"
