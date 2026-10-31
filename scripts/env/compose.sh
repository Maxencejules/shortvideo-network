#!/usr/bin/env bash
# Full profile entry point. This machine's cloud profile uses local processes.
# Compose runs only when a real docker daemon answers.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-compose"

if ! command -v docker >/dev/null 2>&1; then
  die "docker is not on PATH. The compose profile is BLOCKED. This script does not install docker and does not talk to a daemon on 127.0.0.1:2375. Installing a container runtime needs Max's approval. Use scripts/env/start.sh for local PostgreSQL and RabbitMQ." 2
fi
if ! docker info >/dev/null 2>&1; then
  die "docker is installed but the daemon is not usable. The compose profile is BLOCKED on this machine." 2
fi

ws="$(svn_workspace_path)" || exit $?
svn_load_credentials "$(svn_credential_file "${ws}")"
export POSTGRES_USER="${SVN_DB_USER}"
export POSTGRES_PASSWORD="${SVN_DB_PASSWORD}"
export POSTGRES_DB="${SVN_DB_NAME}"
export RABBITMQ_DEFAULT_USER="${SVN_QUEUE_USER}"
export RABBITMQ_DEFAULT_PASS="${SVN_QUEUE_PASSWORD}"
say "starting the compose profile"
exec docker compose -f "${SVN_ROOT}/deployments/compose/docker-compose.yml" up
