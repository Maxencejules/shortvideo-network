#!/usr/bin/env bash
# Start PostgreSQL and RabbitMQ for one disposable workspace.
# Existing data directories are reused. This command does not run initdb
# again and does not delete chain or devnet files.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-start"

if [[ $# -ne 0 ]]; then
  die "usage: start.sh  (workspace from SVN_WORKSPACE, default dev)" 2
fi

ws="$(svn_workspace_path)" || exit $?
mkdir -p "${ws}/postgres/run" "${ws}/rabbitmq" "${ws}/home" "${ws}/ffmpeg" "${ws}/storage"
cred="$(svn_credential_file "${ws}")"

if [[ -f "${ws}/postgres/data/PG_VERSION" && ! -f "${cred}" ]]; then
  die "credentials file is missing at ${cred}. Refusing to reset existing database state." 2
fi
if [[ -d "${ws}/rabbitmq/mnesia" && -n "$(ls -A "${ws}/rabbitmq/mnesia" 2>/dev/null || true)" && ! -f "${cred}" ]]; then
  die "credentials file is missing at ${cred}. Refusing to reset existing queue state." 2
fi
if [[ ! -f "${cred}" ]]; then
  svn_generate_credentials "${cred}"
  say "wrote new credentials at ${cred} (mode 0600). The password is not printed."
fi
svn_load_credentials "${cred}"
svn_prepare_client_files "${ws}"
svn_ensure_storage "${ws}"

if svn_postgres_running "${ws}" && svn_rabbit_running "${ws}"; then
  say "postgres and rabbitmq are already running for ${ws}. State was not regenerated."
  exit 0
fi

if ! svn_postgres_running "${ws}"; then
  if [[ ! -x "${SVN_PG_BIN}/initdb" ]]; then
    die "PostgreSQL 18 is not installed. Run scripts/env/install.sh"
  fi
  if ! svn_require_port 127.0.0.1 "${SVN_POSTGRES_PORT}"; then
    die "PostgreSQL port ${SVN_POSTGRES_PORT} on 127.0.0.1 is not available. Refusing to start."
  fi
  if [[ ! -f "${ws}/postgres/data/PG_VERSION" ]]; then
    say "initialising a new postgres data directory"
    pwfile="$(mktemp)"
    chmod 600 "${pwfile}"
    printf '%s\n' "${SVN_DB_PASSWORD}" > "${pwfile}"
    "${SVN_PG_BIN}/initdb" \
      -D "${ws}/postgres/data" \
      --username="${SVN_DB_USER}" \
      --pwfile="${pwfile}" \
      --auth-local=scram-sha-256 \
      --auth-host=scram-sha-256 \
      --encoding=UTF8 \
      --locale=C.UTF-8
    rm -f "${pwfile}"
  else
    say "postgres data directory already exists; not running initdb"
  fi
  "${SVN_PG_BIN}/pg_ctl" -D "${ws}/postgres/data" -l "${ws}/postgres/postgres.log" -w -t 60 start \
    -o "-c listen_addresses=127.0.0.1 -c port=${SVN_POSTGRES_PORT} -c unix_socket_directories='${ws}/postgres/run'"
  tries=0
  until "${SVN_PG_BIN}/pg_isready" -h 127.0.0.1 -p "${SVN_POSTGRES_PORT}" -q; do
    tries=$((tries + 1))
    if [[ "${tries}" -ge 30 ]]; then
      tail -n 40 "${ws}/postgres/postgres.log" >&2 || true
      die "postgres was not ready on 127.0.0.1:${SVN_POSTGRES_PORT}"
    fi
    sleep 1
  done
  say "postgres is ready on 127.0.0.1:${SVN_POSTGRES_PORT}"
fi

exists="$("${SVN_PG_BIN}/psql" \
  -h 127.0.0.1 -p "${SVN_POSTGRES_PORT}" -U "${SVN_DB_USER}" -d postgres \
  -v ON_ERROR_STOP=1 -X -tAc "SELECT 1 FROM pg_database WHERE datname='${SVN_DB_NAME}'")"
if [[ "$(echo "${exists}" | tr -d '[:space:]')" != "1" ]]; then
  "${SVN_PG_BIN}/createdb" \
    -h 127.0.0.1 -p "${SVN_POSTGRES_PORT}" -U "${SVN_DB_USER}" "${SVN_DB_NAME}"
  say "created database ${SVN_DB_NAME}"
else
  say "database ${SVN_DB_NAME} already exists"
fi

if ! svn_rabbit_running "${ws}"; then
  if ! svn_require_port 127.0.0.1 "${SVN_RABBITMQ_PORT}"; then
    die "RabbitMQ port ${SVN_RABBITMQ_PORT} on 127.0.0.1 is not available. Refusing to start."
  fi
  dist_port="${SVN_RABBITMQ_DIST_PORT:-25672}"
  if ! svn_require_port 127.0.0.1 "${dist_port}"; then
    die "RabbitMQ distribution port ${dist_port} on 127.0.0.1 is not available. Refusing to start."
  fi
  umask 077
  cat > "${ws}/rabbitmq/rabbitmq.conf" <<EOF
listeners.tcp.local = 127.0.0.1:${SVN_RABBITMQ_PORT}
distribution.listener.interface = 127.0.0.1
distribution.listener.port_range.min = ${dist_port}
distribution.listener.port_range.max = ${dist_port}
default_user = ${SVN_QUEUE_USER}
default_pass = ${SVN_QUEUE_PASSWORD}
loopback_users.${SVN_QUEUE_USER} = true
EOF
  chmod 600 "${ws}/rabbitmq/rabbitmq.conf"
  say "starting rabbitmq"
  svn_with_rabbit_env "${ws}" rabbitmq-server -detached
  tries=0
  until svn_rabbit_running "${ws}" && python3 -c 'import socket,sys; s=socket.create_connection(("127.0.0.1", int(sys.argv[1])), 1); s.close()' "${SVN_RABBITMQ_PORT}"; do
    tries=$((tries + 1))
    if [[ "${tries}" -ge 90 ]]; then
      log_file="$(ls -1 "${ws}/rabbitmq/log"/*.log 2>/dev/null | head -n 1 || true)"
      if [[ -n "${log_file}" ]]; then
        tail -n 60 "${log_file}" >&2 || true
      fi
      die "rabbitmq was not ready on 127.0.0.1:${SVN_RABBITMQ_PORT}"
    fi
    sleep 1
  done
  say "rabbitmq is ready on 127.0.0.1:${SVN_RABBITMQ_PORT}"
fi

tries=0
until svn_amqp amqp-declare-queue --queue=svn-auth-probe --durable >/dev/null 2>&1; do
  tries=$((tries + 1))
  if [[ "${tries}" -ge 20 ]]; then
    svn_amqp amqp-declare-queue --queue=svn-auth-probe --durable >&2 || true
    die "queue login failed. Refusing to pass the password on the rabbitmqctl command line. Reset this workspace if its queue user was created with a different password."
  fi
  sleep 1
done
svn_with_rabbit_env "${ws}" rabbitmqctl set_permissions -p / "${SVN_QUEUE_USER}" ".*" ".*" ".*"
if svn_with_rabbit_env "${ws}" rabbitmqctl list_users | awk 'NR > 1 { print $1 }' | grep -qx guest; then
  svn_with_rabbit_env "${ws}" rabbitmqctl delete_user guest
  say "removed the default broker user"
fi
say "workspace ${ws} is ready. svn_db is the database superuser. Chain files were not changed."
