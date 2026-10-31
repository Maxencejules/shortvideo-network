#!/usr/bin/env bash
# Write a database row and a durable queue message, restart, and read them back.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-prove-restart"

ws="$(svn_workspace_path)" || exit $?
cred="$(svn_credential_file "${ws}")"
svn_load_credentials "${cred}"
svn_prepare_client_files "${ws}"
export SVN_WORKSPACE

bash "${_ENV_DIR}/start.sh"
svn_psql "${ws}" <<'SQL'
CREATE TABLE IF NOT EXISTS env_marker (
  id integer PRIMARY KEY,
  note text NOT NULL
);
INSERT INTO env_marker (id, note) VALUES (1, 'goal04')
ON CONFLICT (id) DO UPDATE SET note = EXCLUDED.note;
SQL

body="goal04-persist"
svn_amqp amqp-declare-queue --queue=svn-persist --durable >/dev/null
svn_amqp amqp-publish --routing-key=svn-persist --persistent --body="${body}"
say "stored the marker, a durable message, and the storage directory"

bash "${_ENV_DIR}/stop.sh"
bash "${_ENV_DIR}/start.sh"

note="$(svn_psql "${ws}" -tAc "SELECT note FROM env_marker WHERE id = 1")"
note="$(echo "${note}" | tr -d '[:space:]')"
[[ "${note}" == "goal04" ]] || die "database marker did not survive restart (got ${note})"
got="$(timeout 20 amqp-consume --server=127.0.0.1 --port="${SVN_RABBITMQ_PORT}" --vhost=/ --authfile="${SVN_AUTHFILE}" --queue=svn-persist --count=1 cat)"
got="${got//$'\n'/}"
[[ "${got}" == "${body}" ]] || die "durable message did not survive restart (got ${got})"
storage_marker="$(tr -d '[:space:]' < "${ws}/storage/marker")"
[[ "${storage_marker}" == "goal04-storage" ]] || die "storage marker did not survive restart (got ${storage_marker})"
say "restart kept the database marker, the durable message, and the storage directory"
