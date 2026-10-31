#!/usr/bin/env bash
# Show that a busy port, a taken service port, a wrong database password,
# and a missing credentials file fail with a non-zero status and a clear message.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-prove-failures"

ws="$(svn_workspace_path)" || exit $?
cred="$(svn_credential_file "${ws}")"
svn_load_credentials "${cred}"
svn_prepare_client_files "${ws}"

hold_port() {
  local port="$1"
  python3 - "${port}" <<'PY' &
import socket
import sys
import time

sock = socket.socket()
sock.bind(("127.0.0.1", int(sys.argv[1])))
sock.listen(1)
time.sleep(60)
PY
}

say "busy port"
hold_port 55991
holder=$!
sleep 0.2
set +e
port_out="$(bash "${_ENV_DIR}/require-port.sh" 127.0.0.1 55991 2>&1)"
port_status=$?
set -e
kill "${holder}" 2>/dev/null || true
wait "${holder}" 2>/dev/null || true
if [[ "${port_status}" -eq 0 ]]; then
  die "busy port was reported as free"
fi
printf '%s\n' "${port_out}" | grep -q "not available" || die "busy port message was not clear: ${port_out}"
say "busy port failed clearly (exit ${port_status})"

say "wrong database password"
bad_pass="$(mktemp)"
chmod 600 "${bad_pass}"
printf '127.0.0.1:%s:*:%s:incorrect\n' "${SVN_POSTGRES_PORT}" "${SVN_DB_USER}" > "${bad_pass}"
set +e
auth_out="$(PGPASSFILE="${bad_pass}" "${SVN_PG_BIN}/psql" \
  -h 127.0.0.1 -p "${SVN_POSTGRES_PORT}" -U "${SVN_DB_USER}" -d "${SVN_DB_NAME}" \
  -v ON_ERROR_STOP=1 -X -c 'SELECT 1' 2>&1)"
auth_status=$?
set -e
rm -f "${bad_pass}"
if [[ "${auth_status}" -eq 0 ]]; then
  die "wrong password was accepted"
fi
printf '%s\n' "${auth_out}" | grep -E -q 'authentication failed|password authentication failed|FATAL' \
  || die "authentication failure was not clear: ${auth_out}"
say "wrong password failed clearly (exit ${auth_status})"

say "missing credentials with existing state"
mv "${cred}" "${cred}.aside"
set +e
missing_out="$(bash "${_ENV_DIR}/start.sh" 2>&1)"
missing_status=$?
set -e
mv "${cred}.aside" "${cred}"
if [[ "${missing_status}" -eq 0 ]]; then
  die "start succeeded without credentials"
fi
printf '%s\n' "${missing_out}" | grep -q "credentials file is missing" \
  || die "missing credentials message was not clear: ${missing_out}"
printf '%s\n' "${missing_out}" | grep -q "Refusing to reset" \
  || die "missing credentials did not refuse to reset state: ${missing_out}"
svn_load_credentials "${cred}"
svn_prepare_client_files "${ws}"
svn_psql "${ws}" -c 'SELECT 1 AS still_up;' >/dev/null
say "missing credentials failed clearly (exit ${missing_status})"

say "start refuses a taken postgres port"
bash "${_ENV_DIR}/stop.sh"
hold_port "${SVN_POSTGRES_PORT}"
holder=$!
sleep 0.2
set +e
pg_out="$(bash "${_ENV_DIR}/start.sh" 2>&1)"
pg_status=$?
set -e
kill "${holder}" 2>/dev/null || true
wait "${holder}" 2>/dev/null || true
if [[ "${pg_status}" -eq 0 ]]; then
  die "start succeeded while postgres port ${SVN_POSTGRES_PORT} was taken"
fi
printf '%s\n' "${pg_out}" | grep -q "PostgreSQL port" || die "postgres port failure was not clear: ${pg_out}"
printf '%s\n' "${pg_out}" | grep -q "Refusing to start" || die "postgres port failure did not refuse: ${pg_out}"
say "taken postgres port failed clearly (exit ${pg_status})"

say "start refuses a taken rabbitmq port"
hold_port "${SVN_RABBITMQ_PORT}"
holder=$!
sleep 0.2
set +e
rq_out="$(bash "${_ENV_DIR}/start.sh" 2>&1)"
rq_status=$?
set -e
kill "${holder}" 2>/dev/null || true
wait "${holder}" 2>/dev/null || true
bash "${_ENV_DIR}/stop.sh"
if [[ "${rq_status}" -eq 0 ]]; then
  die "start succeeded while rabbitmq port ${SVN_RABBITMQ_PORT} was taken"
fi
printf '%s\n' "${rq_out}" | grep -q "RabbitMQ port" || die "rabbitmq port failure was not clear: ${rq_out}"
printf '%s\n' "${rq_out}" | grep -q "Refusing to start" || die "rabbitmq port failure did not refuse: ${rq_out}"
say "taken rabbitmq port failed clearly (exit ${rq_status})"

bash "${_ENV_DIR}/start.sh"
say "failure proofs passed"
