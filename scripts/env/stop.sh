#!/usr/bin/env bash
# Stop the workspace services. Missing processes are not an error.
# Data directories are left in place.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-stop"

if [[ $# -ne 0 ]]; then
  die "usage: stop.sh  (workspace from SVN_WORKSPACE, default dev)" 2
fi

ws="$(svn_workspace_path)" || exit $?
if [[ ! -d "${ws}" ]]; then
  say "workspace ${ws} is absent"
  exit 0
fi

if svn_postgres_running "${ws}"; then
  "${SVN_PG_BIN}/pg_ctl" -D "${ws}/postgres/data" -w -t 60 -m fast stop
  say "postgres stopped"
else
  say "postgres is not running"
fi

if [[ -x "${SVN_RABBIT_HOME}/sbin/rabbitmqctl" ]] && svn_rabbit_running "${ws}"; then
  svn_with_rabbit_env "${ws}" rabbitmqctl shutdown
  say "rabbitmq stopped"
else
  say "rabbitmq is not running"
fi
