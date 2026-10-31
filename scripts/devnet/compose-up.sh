#!/usr/bin/env bash
# Build and start the four-validator container profile.
# Refuses a Docker host on port 2375. Does not delete an existing home.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
home="${SVN_HOME:-${root}/.local/devnet/containers}"
binary="${SVN_BIN:-${root}/bin/svnd}"

if [[ -n "${DOCKER_HOST:-}" ]]; then
  echo "refusing DOCKER_HOST ${DOCKER_HOST}. The container profile uses the local docker context." >&2
  exit 2
fi
unset DOCKER_HOST
if ! command -v docker >/dev/null; then
  echo "docker is not installed. The container profile is blocked." >&2
  exit 2
fi
context="$(docker context show)"
endpoint="$(docker context inspect "${context}" --format '{{.Endpoints.docker.Host}}')"
case "${endpoint}" in
  *2375*)
    echo "refusing docker context ${context} endpoint ${endpoint}. Port 2375 is not this VM's daemon." >&2
    exit 2
    ;;
  unix://*)
    ;;
  *)
    echo "refusing docker context ${context} endpoint ${endpoint}. Expected a local unix socket." >&2
    exit 2
    ;;
esac
echo "docker context ${context} endpoint ${endpoint}"
if [[ ! -x "${binary}" ]]; then
  mkdir -p "$(dirname "${binary}")"
  go build -C "${root}/chain" -o "${binary}" ./cmd/svnd
fi
if [[ ! -f "${home}/genesis.json" ]]; then
  if [[ -d "${home}" ]] && [[ -n "$(ls -A "${home}" 2>/dev/null || true)" ]]; then
    echo "refusing to replace ${home}" >&2
    exit 2
  fi
  "${binary}" devnet init --home "${home}" --peers loopback
fi
docker build -t svn-devnet:goal05 -f "${root}/deployments/chain/Dockerfile" "${root}"
HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" docker compose -f "${root}/deployments/chain/compose.yaml" up -d
echo "container devnet home ${home}"
