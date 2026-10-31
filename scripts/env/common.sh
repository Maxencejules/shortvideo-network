# Shared helpers for the development environment scripts.
# Source this file. Do not execute it.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  echo "env: source scripts/env/common.sh; do not execute it" >&2
  exit 2
fi

_ENV_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SVN_ROOT="$(cd "${_ENV_DIR}/../.." && pwd)"
SVN_PG_BIN="/usr/lib/postgresql/18/bin"
SVN_RABBIT_HOME="${SVN_ROOT}/.toolchains/rabbitmq_server-4.4.0"
SVN_LOG_PREFIX="${SVN_LOG_PREFIX:-env}"

say() {
  printf '%s\n' "${SVN_LOG_PREFIX}: $*"
}

die() {
  local status="${2:-1}"
  printf '%s\n' "${SVN_LOG_PREFIX}: $1" >&2
  exit "${status}"
}

svn_state_root() {
  local root="${SVN_STATE_ROOT:-${SVN_ROOT}/.local/workspaces}"
  case "${root}" in
    /*) ;;
    *)
      echo "env: SVN_STATE_ROOT must be an absolute path" >&2
      return 2
      ;;
  esac
  case "${root}" in
    "${SVN_ROOT}"/.local|"${SVN_ROOT}"/.local/*) ;;
    "${SVN_ROOT}"|"${SVN_ROOT}"/*)
      echo "env: SVN_STATE_ROOT inside the repository must stay under .local" >&2
      return 2
      ;;
  esac
  printf '%s\n' "${root}"
}

svn_workspace_name() {
  local name="${1:-}"
  if [[ -z "${name}" ]]; then
    name="${SVN_WORKSPACE:-dev}"
  fi
  if [[ ! "${name}" =~ ^[a-z][a-z0-9-]{0,31}$ ]]; then
    echo "env: workspace name must match ^[a-z][a-z0-9-]{0,31}\$ (got ${name})" >&2
    return 2
  fi
  printf '%s\n' "${name}"
}

svn_workspace_path() {
  local name root path
  name="$(svn_workspace_name "${1:-}")" || return $?
  root="$(svn_state_root)" || return $?
  if [[ "${name}" == "." || "${name}" == ".." || "${name}" == *"/"* ]]; then
    echo "env: refusing workspace name ${name}" >&2
    return 2
  fi
  path="$(python3 - "${root}" "${name}" <<'PY'
import os
import sys

root, name = sys.argv[1], sys.argv[2]
root = os.path.abspath(root)
path = os.path.abspath(os.path.join(root, name))
if os.path.dirname(path) != root:
    sys.exit(1)
print(path)
PY
)" || {
    echo "env: workspace path escapes the state root" >&2
    return 2
  }
  if [[ "${path}" == "${root}" ]]; then
    echo "env: refusing to use the state root as a workspace" >&2
    return 2
  fi
  printf '%s\n' "${path}"
}

svn_require_port() {
  local host="$1"
  local port="$2"
  python3 - "${host}" "${port}" <<'PY'
import socket
import sys

host, port = sys.argv[1], int(sys.argv[2])
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
# SO_REUSEADDR ignores TIME_WAIT. A socket that is still listening still fails.
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    sock.bind((host, port))
except OSError as exc:
    print(f"port {port} on {host} is not available: {exc}", file=sys.stderr)
    sys.exit(1)
finally:
    sock.close()
PY
}

svn_credential_file() {
  local ws="$1"
  printf '%s\n' "${ws}/credentials.env"
}

svn_load_credentials() {
  local file="$1"
  local line key value
  if [[ ! -f "${file}" ]]; then
    echo "env: credentials file is missing: ${file}" >&2
    echo "env: refusing to use a default or shared password" >&2
    return 2
  fi
  # Parse KEY=VALUE. Do not source the file, so a value cannot run a command.
  SVN_DB_USER=""
  SVN_DB_PASSWORD=""
  SVN_DB_NAME=""
  SVN_QUEUE_USER=""
  SVN_QUEUE_PASSWORD=""
  SVN_POSTGRES_PORT=""
  SVN_RABBITMQ_PORT=""
  SVN_RABBITMQ_DIST_PORT=""
  SVN_EPMD_PORT=""
  while IFS= read -r line || [[ -n "${line}" ]]; do
    [[ -z "${line}" || "${line}" == \#* ]] && continue
    if [[ ! "${line}" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
      echo "env: credentials line is not KEY=VALUE" >&2
      return 2
    fi
    key="${BASH_REMATCH[1]}"
    value="${BASH_REMATCH[2]}"
    case "${key}" in
      SVN_DB_USER|SVN_DB_PASSWORD|SVN_DB_NAME|SVN_QUEUE_USER|SVN_QUEUE_PASSWORD|SVN_POSTGRES_PORT|SVN_RABBITMQ_PORT|SVN_RABBITMQ_DIST_PORT|SVN_EPMD_PORT)
        printf -v "${key}" '%s' "${value}"
        export "${key}"
        ;;
      *)
        echo "env: unexpected credentials key ${key}" >&2
        return 2
        ;;
    esac
  done < "${file}"
  [[ -n "${SVN_DB_USER}" ]] || die "credentials file has no SVN_DB_USER" 2
  [[ -n "${SVN_DB_PASSWORD}" ]] || die "credentials file has no SVN_DB_PASSWORD" 2
  [[ -n "${SVN_DB_NAME}" ]] || die "credentials file has no SVN_DB_NAME" 2
  [[ -n "${SVN_QUEUE_USER}" ]] || die "credentials file has no SVN_QUEUE_USER" 2
  [[ -n "${SVN_QUEUE_PASSWORD}" ]] || die "credentials file has no SVN_QUEUE_PASSWORD" 2
  [[ -n "${SVN_POSTGRES_PORT}" ]] || die "credentials file has no SVN_POSTGRES_PORT" 2
  [[ -n "${SVN_RABBITMQ_PORT}" ]] || die "credentials file has no SVN_RABBITMQ_PORT" 2
  [[ "${SVN_DB_USER}" =~ ^[a-z][a-z0-9_]{0,30}$ ]] || die "database user is not a safe identifier" 2
  [[ "${SVN_DB_NAME}" =~ ^[a-z][a-z0-9_]{0,30}$ ]] || die "database name is not a safe identifier" 2
  [[ "${SVN_QUEUE_USER}" =~ ^[a-z][a-z0-9_]{0,30}$ ]] || die "queue user is not a safe identifier" 2
  [[ "${SVN_POSTGRES_PORT}" =~ ^[0-9]+$ ]] || die "postgres port is not numeric" 2
  [[ "${SVN_RABBITMQ_PORT}" =~ ^[0-9]+$ ]] || die "rabbitmq port is not numeric" 2
}

# svn_db is the database superuser created by initdb. Later goals need a
# separate application role. Generated passwords are hex, so they contain
# no colon or backslash and need no pgpass escaping.
svn_prepare_client_files() {
  local ws="$1"
  local pgpass="${ws}/pgpass"
  local auth="${ws}/amqp.auth"
  umask 077
  printf '%s\n' "127.0.0.1:${SVN_POSTGRES_PORT}:*:${SVN_DB_USER}:${SVN_DB_PASSWORD}" > "${pgpass}"
  printf 'username:%s\npassword:%s\n' "${SVN_QUEUE_USER}" "${SVN_QUEUE_PASSWORD}" > "${auth}"
  chmod 600 "${pgpass}" "${auth}"
  export PGPASSFILE="${pgpass}"
  export SVN_AUTHFILE="${auth}"
  unset PGPASSWORD
}

svn_ensure_storage() {
  local ws="$1"
  mkdir -p "${ws}/storage"
  if [[ ! -f "${ws}/.svn-workspace" ]]; then
    printf 'svn-dev-workspace\n' > "${ws}/.svn-workspace"
  fi
  if [[ ! -f "${ws}/storage/marker" ]]; then
    printf 'goal04-storage\n' > "${ws}/storage/marker"
  fi
}

svn_generate_credentials() {
  local file="$1"
  local pg_port="${2:-5432}"
  local amqp_port="${3:-5672}"
  umask 077
  python3 - "${file}" "${pg_port}" "${amqp_port}" <<'PY'
import secrets
import sys

path, pg_port, amqp_port = sys.argv[1:]
lines = [
    "SVN_DB_USER=svn_db",
    f"SVN_DB_PASSWORD={secrets.token_hex(24)}",
    "SVN_DB_NAME=svn_dev",
    "SVN_QUEUE_USER=svn_queue",
    f"SVN_QUEUE_PASSWORD={secrets.token_hex(24)}",
    f"SVN_POSTGRES_PORT={pg_port}",
    f"SVN_RABBITMQ_PORT={amqp_port}",
    "SVN_RABBITMQ_DIST_PORT=25672",
    "SVN_EPMD_PORT=4369",
    "",
]
with open(path, "w", encoding="utf-8") as handle:
    handle.write("\n".join(lines))
PY
  chmod 600 "${file}"
}

svn_with_rabbit_env() {
  local ws="$1"
  shift
  if [[ ! -x "${SVN_RABBIT_HOME}/sbin/rabbitmq-server" ]]; then
    die "RabbitMQ is missing at ${SVN_RABBIT_HOME}. Run scripts/env/install.sh"
  fi
  mkdir -p "${ws}/rabbitmq/mnesia" "${ws}/rabbitmq/log" "${ws}/home"
  env \
    HOME="${ws}/home" \
    RABBITMQ_HOME="${SVN_RABBIT_HOME}" \
    PATH="${SVN_RABBIT_HOME}/sbin:${PATH}" \
    RABBITMQ_MNESIA_BASE="${ws}/rabbitmq/mnesia" \
    RABBITMQ_LOG_BASE="${ws}/rabbitmq/log" \
    RABBITMQ_PID_FILE="${ws}/rabbitmq/rabbitmq.pid" \
    RABBITMQ_NODENAME="svnqueue@localhost" \
    RABBITMQ_CONFIG_FILE="${ws}/rabbitmq/rabbitmq" \
    ERL_EPMD_ADDRESS="127.0.0.1" \
    ERL_EPMD_PORT="${SVN_EPMD_PORT:-4369}" \
    RABBITMQ_DIST_PORT="${SVN_RABBITMQ_DIST_PORT:-25672}" \
    RABBITMQ_CTL_DIST_PORT_MIN="35672" \
    RABBITMQ_CTL_DIST_PORT_MAX="35682" \
    RABBITMQ_SERVER_ADDITIONAL_ERL_ARGS="-kernel inet_dist_use_interface {127,0,0,1}" \
    RABBITMQ_CTL_ERL_ARGS="-kernel inet_dist_use_interface {127,0,0,1}" \
    "$@"
}

svn_postgres_running() {
  local ws="$1"
  [[ -x "${SVN_PG_BIN}/pg_ctl" ]] || return 1
  [[ -d "${ws}/postgres/data" ]] || return 1
  "${SVN_PG_BIN}/pg_ctl" -D "${ws}/postgres/data" status >/dev/null 2>&1
}

svn_rabbit_running() {
  local ws="$1"
  svn_with_rabbit_env "${ws}" rabbitmq-diagnostics -q ping >/dev/null 2>&1
}

svn_psql() {
  local ws="$1"
  shift
  "${SVN_PG_BIN}/psql" \
    -h 127.0.0.1 \
    -p "${SVN_POSTGRES_PORT}" \
    -U "${SVN_DB_USER}" \
    -d "${SVN_DB_NAME}" \
    -v ON_ERROR_STOP=1 \
    -X \
    "$@"
}

svn_amqp() {
  local tool="$1"
  shift
  "${tool}" \
    --server=127.0.0.1 \
    --port="${SVN_RABBITMQ_PORT}" \
    --vhost=/ \
    --authfile="${SVN_AUTHFILE}" \
    "$@"
}
