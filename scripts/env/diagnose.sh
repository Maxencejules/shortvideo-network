#!/usr/bin/env bash
# Print observed runtime facts. Missing tools are reported. Exit 0 when the
# report itself was produced.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-diagnose"

section() {
  printf '\n## %s\n' "$1"
}

section "identity"
date -u +%Y-%m-%dT%H:%M:%SZ
uname -srmo
if [[ -f /etc/os-release ]]; then
  awk -F= '$1 == "PRETTY_NAME" { print $2 }' /etc/os-release
fi
id
ps -p 1 -o comm= || true
if command -v systemctl >/dev/null 2>&1; then
  systemctl is-system-running || true
fi

section "cpu-memory-disk"
echo "nproc $(nproc)"
awk '/^MemTotal:|^MemAvailable:|^MemFree:/ { print }' /proc/meminfo
free -h
df -hT /
awk '/balloon_inflate|balloon_deflate/ { print }' /proc/vmstat || true

section "tools"
report() {
  local name="$1"
  shift
  if command -v "${name}" >/dev/null 2>&1; then
    echo "${name}: present $(command -v "${name}")"
    "$@" || true
  else
    echo "${name}: MISSING"
  fi
}
report go go version
report python3 python3 --version
report node node --version
report npm npm --version
report ffmpeg ffmpeg -version
report ffprobe ffprobe -version
report psql "${SVN_PG_BIN}/psql" --version
if [[ -x "${SVN_PG_BIN}/postgres" ]]; then
  echo "postgres: present ${SVN_PG_BIN}/postgres"
  "${SVN_PG_BIN}/postgres" -V || true
else
  echo "postgres: MISSING"
fi
report erl erl -noshell -eval 'io:format("otp ~s~n",[erlang:system_info(otp_release)]), halt().'
if [[ -x "${SVN_RABBIT_HOME}/sbin/rabbitmqctl" ]]; then
  echo "rabbitmq: present ${SVN_RABBIT_HOME}"
  env \
    HOME="${TMPDIR:-/tmp}" \
    RABBITMQ_HOME="${SVN_RABBIT_HOME}" \
    PATH="${SVN_RABBIT_HOME}/sbin:${PATH}" \
    "${SVN_RABBIT_HOME}/sbin/rabbitmqctl" version || true
else
  echo "rabbitmq: MISSING"
fi
if command -v amqp-publish >/dev/null 2>&1; then
  echo "amqp-publish: present $(command -v amqp-publish)"
else
  echo "amqp-publish: MISSING"
fi
report docker docker version
report podman podman version
if [[ -n "${DOCKER_HOST:-}" ]]; then
  echo "DOCKER_HOST=set"
  python3 - <<'PY'
import os
from urllib.parse import urlparse

raw = os.environ.get("DOCKER_HOST", "")
parsed = urlparse(raw)
if parsed.scheme and parsed.hostname:
    port = f":{parsed.port}" if parsed.port else ""
    print(f"DOCKER_HOST_TARGET={parsed.scheme}://{parsed.hostname}{port}")
else:
    print("DOCKER_HOST_TARGET=redacted")
PY
else
  echo "DOCKER_HOST=unset"
fi
echo "probe http://127.0.0.1:2375/version"
set +e
probe_out="$(curl -sS -m 5 -D - http://127.0.0.1:2375/version 2>&1)"
probe_status=$?
set -e
printf '%s\n' "${probe_out}"
echo "curl_exit:${probe_status}"
echo "read-only probe. diagnose does not run containers and does not install docker."
if command -v ss >/dev/null 2>&1; then
  echo "ss: present"
else
  echo "ss: MISSING"
fi
if command -v tc >/dev/null 2>&1; then
  echo "tc: present $(command -v tc)"
else
  echo "tc: MISSING"
fi
report google-chrome google-chrome --version
report openssl openssl version

section "bind"
python3 - <<'PY'
import socket

def probe(host, port):
    sock = socket.socket()
    try:
        sock.bind((host, port))
    except OSError as exc:
        print(f"bind {host}:{port} FAIL {exc}")
    else:
        actual = sock.getsockname()
        print(f"bind {host}:{port} ok actual={actual}")
    finally:
        sock.close()

# Port 0 asks the kernel for an ephemeral port. Port 80 is privileged.
probe("127.0.0.1", 0)
probe("0.0.0.0", 0)
probe("127.0.0.1", 80)
probe("0.0.0.0", 80)
PY

section "listeners"
python3 - <<'PY'
from pathlib import Path

def parse(path, label):
    file = Path(path)
    if not file.exists():
        print(f"{label}: absent")
        return
    print(f"{label}:")
    lines = file.read_text(encoding="utf-8", errors="replace").splitlines()[1:]
    for line in lines:
        parts = line.split()
        if len(parts) < 4 or parts[3] != "0A":
            continue
        local = parts[1]
        ip_hex, port_hex = local.split(":")
        port = int(port_hex, 16)
        if label == "tcp":
            ip = ".".join(str(int(ip_hex[i:i + 2], 16)) for i in (6, 4, 2, 0))
        else:
            ip = ip_hex
        print(f"  {ip}:{port}")

parse("/proc/net/tcp", "tcp")
parse("/proc/net/tcp6", "tcp6")
PY

section "git"
git -C "${SVN_ROOT}" rev-parse --abbrev-ref HEAD
git -C "${SVN_ROOT}" rev-parse HEAD
git -C "${SVN_ROOT}" rev-parse 'HEAD^{tree}'
say "report complete"
