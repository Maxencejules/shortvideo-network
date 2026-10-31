#!/usr/bin/env bash
# Idempotent install for the development profile.
# Installs pinned packages and language tools. It does not start daemons
# and it does not create or delete workspace state.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-install"

PG_VER="18.6-1.pgdg24.04+2"
ERL_VER="1:27.3.4.18-1rmq1ppa1~ubuntu24.04.1"
FFMPEG_VER="7:6.1.1-3ubuntu5"
AMQP_VER="0.11.0-1ubuntu0.2"
PY_VENV_VER="3.12.3-1ubuntu0.17"
PG_FPR="B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8"
ERL_FPR="B279943D2A549531E144B875F77F1EDA57EBB1CC"
RABBIT_FPR="0A9AF2115F4687BD29803A206B73A36E6026DFCA"
RABBIT_URL="https://github.com/rabbitmq/rabbitmq-server/releases/download/v4.4.0/rabbitmq-server-generic-unix-4.4.0.tar.xz"
RABBIT_ASC_URL="${RABBIT_URL}.asc"
RABBIT_KEY_URL="https://github.com/rabbitmq/signing-keys/releases/download/3.0/rabbitmq-release-signing-key.asc"
RABBIT_NAME="rabbitmq-server-generic-unix-4.4.0.tar.xz"

pin_sha256() {
  local name="$1"
  local file="$2"
  local want
  want="$(awk -v n="${name}" '$2 == n { print $1 }' "${SVN_ROOT}/deployments/pins/checksums.txt")"
  if [[ -z "${want}" ]]; then
    die "checksum file has no entry for ${name}"
  fi
  echo "${want}  ${file}" | sha256sum -c -
}

gpg_fpr() {
  gpg --batch --show-keys --with-colons "$1" | awk -F: '$1 == "fpr" { print $10; exit }'
}

pkg_version() {
  dpkg-query -W -f '${Version}' "$1" 2>/dev/null || true
}

need_packages() {
  [[ "$(pkg_version postgresql-18)" == "${PG_VER}" ]] || return 0
  [[ "$(pkg_version postgresql-client-18)" == "${PG_VER}" ]] || return 0
  [[ "$(pkg_version erlang-base)" == "${ERL_VER}" ]] || return 0
  [[ "$(pkg_version ffmpeg)" == "${FFMPEG_VER}" ]] || return 0
  [[ "$(pkg_version amqp-tools)" == "${AMQP_VER}" ]] || return 0
  [[ "$(pkg_version python3.12-venv)" == "${PY_VENV_VER}" ]] || return 0
  return 1
}

install_packages() {
  sudo -n true || die "passwordless sudo is required to install packages" 2
  local created=0
  if [[ ! -e /usr/sbin/policy-rc.d ]]; then
    created=1
    printf '#!/bin/sh\nexit 101\n' | sudo tee /usr/sbin/policy-rc.d >/dev/null
    sudo chmod 755 /usr/sbin/policy-rc.d
  fi
  cleanup_policy() {
    if [[ "${created}" -eq 1 ]]; then
      sudo rm -f /usr/sbin/policy-rc.d
    fi
  }
  trap cleanup_policy RETURN

  sudo install -d /usr/share/postgresql-common/pgdg
  sudo curl -fsSL -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
    https://www.postgresql.org/media/keys/ACCC4CF8.asc
  local pg_fpr
  pg_fpr="$(gpg_fpr /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc)"
  [[ "${pg_fpr}" == "${PG_FPR}" ]] || die "PostgreSQL apt key fingerprint ${pg_fpr} != ${PG_FPR}"

  sudo tee /etc/apt/sources.list.d/pgdg.sources >/dev/null <<'EOF'
Types: deb
URIs: https://apt.postgresql.org/pub/repos/apt
Suites: noble-pgdg
Architectures: amd64
Components: main
Signed-By: /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc
EOF

  local erl_key="/usr/share/keyrings/net.launchpad.ppa.rabbitmq.erlang.gpg"
  sudo rm -f "${erl_key}"
  curl -fsSL "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0xf77f1eda57ebb1cc" \
    | sudo gpg --batch --yes --dearmor -o "${erl_key}"
  local erl_fpr
  erl_fpr="$(gpg_fpr "${erl_key}")"
  [[ "${erl_fpr}" == "${ERL_FPR}" ]] || die "Erlang apt key fingerprint ${erl_fpr} != ${ERL_FPR}"
  sudo tee /etc/apt/sources.list.d/rabbitmq-erlang.list >/dev/null <<EOF
deb [arch=amd64 signed-by=${erl_key}] http://ppa.launchpad.net/rabbitmq/rabbitmq-erlang/ubuntu noble main
EOF

  sudo DEBIAN_FRONTEND=noninteractive apt-get update -y
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    "postgresql-18=${PG_VER}" \
    "postgresql-client-18=${PG_VER}" \
    "erlang-base=${ERL_VER}" \
    "erlang-asn1=${ERL_VER}" \
    "erlang-crypto=${ERL_VER}" \
    "erlang-eldap=${ERL_VER}" \
    "erlang-ftp=${ERL_VER}" \
    "erlang-inets=${ERL_VER}" \
    "erlang-mnesia=${ERL_VER}" \
    "erlang-os-mon=${ERL_VER}" \
    "erlang-parsetools=${ERL_VER}" \
    "erlang-public-key=${ERL_VER}" \
    "erlang-runtime-tools=${ERL_VER}" \
    "erlang-snmp=${ERL_VER}" \
    "erlang-ssl=${ERL_VER}" \
    "erlang-syntax-tools=${ERL_VER}" \
    "erlang-tftp=${ERL_VER}" \
    "erlang-tools=${ERL_VER}" \
    "erlang-xmerl=${ERL_VER}" \
    "ffmpeg=${FFMPEG_VER}" \
    "amqp-tools=${AMQP_VER}" \
    "python3.12-venv=${PY_VENV_VER}" \
    ca-certificates curl gnupg xz-utils openssl

  if command -v pg_lsclusters >/dev/null 2>&1 && pg_lsclusters 2>/dev/null | awk 'NR > 1 && $4 == "online" { found = 1 } END { exit found ? 0 : 1 }'; then
    say "stopping a package cluster so it does not hold a port"
    sudo pg_ctlcluster 18 main stop || true
  fi
}

install_rabbitmq() {
  if [[ -x "${SVN_RABBIT_HOME}/sbin/rabbitmq-server" ]]; then
    say "RabbitMQ 4.4.0 already extracted"
    return 0
  fi
  local tmp key asc
  tmp="$(mktemp)"
  key="$(mktemp)"
  asc="$(mktemp)"
  curl -fsSL -o "${tmp}" "${RABBIT_URL}"
  curl -fsSL -o "${asc}" "${RABBIT_ASC_URL}"
  curl -fsSL -o "${key}" "${RABBIT_KEY_URL}"
  pin_sha256 "${RABBIT_NAME}" "${tmp}"
  local fpr
  fpr="$(gpg_fpr "${key}")"
  [[ "${fpr}" == "${RABBIT_FPR}" ]] || die "RabbitMQ signing key fingerprint ${fpr} != ${RABBIT_FPR}"
  gpg --batch --import "${key}"
  gpg --batch --verify "${asc}" "${tmp}"
  mkdir -p "${SVN_ROOT}/.toolchains"
  tar -C "${SVN_ROOT}/.toolchains" -xJf "${tmp}"
  rm -f "${tmp}" "${key}" "${asc}"
  [[ -x "${SVN_RABBIT_HOME}/sbin/rabbitmq-server" ]] || die "RabbitMQ extract did not produce sbin/rabbitmq-server"
  say "RabbitMQ 4.4.0 extracted"
}

install_languages() {
  local want_node
  want_node="v$(tr -d '[:space:]' < "${SVN_ROOT}/.nvmrc")"
  command -v node >/dev/null || die "node is not on PATH; the pin is ${want_node}"
  [[ "$(node --version)" == "${want_node}" ]] || die "node is $(node --version); the pin is ${want_node}"
  command -v npm >/dev/null || die "npm is not on PATH"
  python3 --version | grep -q 'Python 3.12.3' || die "python3 is not 3.12.3"
  bash "${SVN_ROOT}/scripts/install_toolchains.sh"
  "${SVN_ROOT}/.toolchains/go/bin/go" version | grep -q 'go1.26.10 ' || die "Go toolchain is not 1.26.10"
  python3 -m venv "${SVN_ROOT}/.venv"
  "${SVN_ROOT}/.venv/bin/pip" install --require-hashes -r "${SVN_ROOT}/requirements.lock"
  npm ci --prefix "${SVN_ROOT}"
}

if need_packages; then
  say "installing pinned packages"
  install_packages
else
  say "pinned packages already installed"
fi
install_rabbitmq
install_languages

"${SVN_PG_BIN}/postgres" -V | grep -q '18.6' || die "postgres binary is not 18.6"
erl -noshell -eval 'io:format("~s~n",[erlang:system_info(otp_release)]), halt().' | grep -qx '27' \
  || die "Erlang/OTP release is not 27"
say "install finished. Services are not started. Run scripts/env/start.sh"
say "Celery 5.6.3 is pinned in deployments/pins/checksums.txt and is not installed"
