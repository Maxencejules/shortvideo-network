#!/usr/bin/env bash
# Generate one fresh devnet identity inside a disposable workspace.
# Refuses to replace an existing identity and refuses to copy one.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-devnet-keys"

workspace=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --workspace)
      workspace="${2:-}"
      shift 2
      ;;
    --from|--clone|--clone-from)
      die "refusing to clone a validator identity into this workspace. Exit 2. Pass only --workspace. --from and --clone are rejected so one task cannot copy another task's key." 2
      ;;
    *)
      die "unknown argument: $1" 2
      ;;
  esac
done

if [[ -n "${workspace}" ]]; then
  SVN_WORKSPACE="${workspace}"
fi
ws="$(svn_workspace_path)" || exit $?
mkdir -p "${ws}/devnet"
key="${ws}/devnet/validator.key"
pub="${ws}/devnet/validator.pub"
if [[ -e "${key}" || -e "${pub}" ]]; then
  die "refusing to replace the devnet identity at ${ws}/devnet. Exit 2. An existing identity is kept and is not regenerated." 2
fi
command -v openssl >/dev/null || die "openssl is not installed, so an ed25519 identity cannot be generated. Exit 2." 2
umask 077
openssl genpkey -algorithm ED25519 -out "${key}"
chmod 600 "${key}"
openssl pkey -in "${key}" -pubout -out "${pub}"
chmod 644 "${pub}"
fingerprint="$(openssl pkey -pubin -in "${pub}" -outform DER | sha256sum | awk '{ print $1 }')"
say "wrote a new devnet identity for workspace $(basename "${ws}")"
say "public key ${pub}"
say "public fingerprint ${fingerprint}"
