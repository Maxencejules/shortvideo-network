#!/usr/bin/env bash
# Four-container devnet proof on the host network.
# The Docker bridge on this VM does not deliver peer packets. This script
# does not claim that bridge works. It refuses a context that is not a local
# unix socket, and it refuses port 2375.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${root}"
compose="${root}/deployments/chain/compose.yaml"
stamp="$(date -Is)"
home="${SVN_HOME:-${root}/.local/devnet/prove-containers-${stamp//:/-}}"

echo "COMMAND bash scripts/devnet/prove-containers.sh"
echo "DATE ${stamp}"
echo "TREE $(git rev-parse 'HEAD^{tree}')"
echo "DOCKER_HOST=${DOCKER_HOST-<unset>}"
echo "NOTE empty blocks only. No application transaction is submitted, so the hash is the genesis application state."
echo "NOTE bridge networking is BLOCKED. This run uses host networking."

if [[ -n "${DOCKER_HOST:-}" ]]; then
  echo "refusing DOCKER_HOST" >&2
  echo "EXIT:2"
  exit 2
fi
unset DOCKER_HOST
context="$(docker context show)"
endpoint="$(docker context inspect "${context}" --format '{{.Endpoints.docker.Host}}')"
echo "docker context ${context} endpoint ${endpoint}"
case "${endpoint}" in
  *2375*)
    echo "refusing port 2375" >&2
    echo "EXIT:2"
    exit 2
    ;;
  unix://*) ;;
  *)
    echo "refusing non-local docker endpoint" >&2
    echo "EXIT:2"
    exit 2
    ;;
esac

mkdir -p "${home}"
if [[ -f "${home}/genesis.json" ]]; then
  echo "refusing to replace ${home}" >&2
  echo "EXIT:2"
  exit 2
fi

cleanup() {
  HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" \
    docker compose -f "${compose}" stop >/dev/null 2>&1 || true
}
finish() {
  status=$?
  cleanup
  echo "EXIT:${status}"
}
trap finish EXIT

docker build -t svn-devnet:goal05 -f "${root}/deployments/chain/Dockerfile" "${root}"
docker run --rm --user "$(id -u):$(id -g)" -v "${home}:/work" \
  svn-devnet:goal05 devnet init --home /work --peers loopback >/dev/null
HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" \
  docker compose -f "${compose}" up -d

sample() {
  local phase="$1"
  local count="$2"
  python3 - "${home}/topology.json" "${phase}" "${count}" <<'PY'
import json
import sys
import time
import urllib.request

topology = json.load(open(sys.argv[1]))
phase = sys.argv[2]
count = int(sys.argv[3])
nodes = topology["validators"][:count]

def read(url):
    with urllib.request.urlopen(url + "/status", timeout=2) as response:
        body = json.load(response)
    info = body["result"]["sync_info"]
    height = int(info["latest_block_height"])
    return height, info["latest_app_hash"].upper()

rows = [read(node["rpc"]) for node in nodes]
height, digest = rows[0]
for item in rows[1:]:
    if item != (height, digest):
        raise SystemExit(f"disagreement {rows}")
for index, (height, digest) in enumerate(rows):
    print(f"phase={phase} sample=0 node={index} height={height} app_hash={digest}", flush=True)
print(f"phase={phase} threshold={height} app_hash={digest}", flush=True)
PY
}

wait_above() {
  local phase="$1"
  local count="$2"
  local threshold="$3"
  python3 - "${home}/topology.json" "${phase}" "${count}" "${threshold}" <<'PY'
import json
import sys
import time
import urllib.request

topology = json.load(open(sys.argv[1]))
phase = sys.argv[2]
count = int(sys.argv[3])
threshold = int(sys.argv[4])
nodes = topology["validators"][:count]
deadline = time.time() + 45

def read(url):
    with urllib.request.urlopen(url + "/status", timeout=2) as response:
        body = json.load(response)
    info = body["result"]["sync_info"]
    return int(info["latest_block_height"]), info["latest_app_hash"].upper()

last = None
while time.time() < deadline:
    try:
        rows = [read(node["rpc"]) for node in nodes]
    except Exception:
        time.sleep(0.3)
        continue
    if all(row == rows[0] and row[0] > threshold for row in rows):
        for index, (height, digest) in enumerate(rows):
            print(f"phase={phase} sample=0 node={index} height={height} app_hash={digest}", file=sys.stderr, flush=True)
        print(height)
        raise SystemExit(0)
    last = rows
    time.sleep(0.3)
raise SystemExit(f"phase {phase} did not pass {threshold}: {last}")
PY
}

watch() {
  local phase="$1"
  local count="$2"
  local mode="$3"
  python3 - "${home}/topology.json" "${phase}" "${count}" "${mode}" <<'PY'
import json
import sys
import time
import urllib.request

topology = json.load(open(sys.argv[1]))
phase = sys.argv[2]
count = int(sys.argv[3])
mode = sys.argv[4]
samples = 4 if mode == "moving" else 6
nodes = topology["validators"][:count]

def read(url):
    with urllib.request.urlopen(url + "/status", timeout=2) as response:
        body = json.load(response)
    info = body["result"]["sync_info"]
    return int(info["latest_block_height"]), info["latest_app_hash"].upper()

first = None
ceiling = None
for sample in range(samples):
    rows = [read(node["rpc"]) for node in nodes]
    digest = rows[0][1]
    if any(row[1] != digest for row in rows):
        raise SystemExit(f"phase {phase} app hash disagreement {rows}")
    heights = [row[0] for row in rows]
    for index, row in enumerate(rows):
        print(f"phase={phase} sample={sample} node={index} height={row[0]} app_hash={row[1]}", file=sys.stderr, flush=True)
    if ceiling is None:
        ceiling = heights
        first = min(heights)
    elif mode == "stall":
        for index, height in enumerate(heights):
            if height > ceiling[index]:
                raise SystemExit(f"phase {phase} node {index} moved from {ceiling[index]} to {height}")
    elif min(heights) < first:
        raise SystemExit(f"phase {phase} height moved backward")
    time.sleep(1)
if mode == "moving" and not (min(heights) > first):
    raise SystemExit(f"phase {phase} did not keep committing {first} -> {min(heights)}")
if mode == "stall":
    print(max(ceiling))
PY
}

echo "COMMAND docker compose -f deployments/chain/compose.yaml up -d"
python3 - "${home}/topology.json" <<'PY'
import json, sys, time, urllib.request
nodes = json.load(open(sys.argv[1]))["validators"]
deadline = time.time() + 45

def read(url):
    with urllib.request.urlopen(url + "/status", timeout=2) as response:
        info = json.load(response)["result"]["sync_info"]
        return int(info["latest_block_height"]), info["latest_app_hash"].upper()

while time.time() < deadline:
    try:
        rows = [read(node["rpc"]) for node in nodes]
    except Exception:
        time.sleep(0.3)
        continue
    if all(row == rows[0] and row[0] >= 3 for row in rows):
        for index, (height, digest) in enumerate(rows):
            print(f"phase=start sample=0 node={index} height={height} app_hash={digest}", flush=True)
        raise SystemExit(0)
    time.sleep(0.3)
raise SystemExit("start did not reach height 3")
PY
start_height="$(python3 - "${home}/topology.json" <<'PY'
import json, sys, urllib.request
node = json.load(open(sys.argv[1]))["validators"][0]
with urllib.request.urlopen(node["rpc"] + "/status", timeout=2) as response:
    print(int(json.load(response)["result"]["sync_info"]["latest_block_height"]))
PY
)"
if [[ "${start_height}" -lt 3 ]]; then
  echo "start height ${start_height} is below 3" >&2
  echo "EXIT:1"
  exit 1
fi

echo "COMMAND docker compose -f deployments/chain/compose.yaml stop"
HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" docker compose -f "${compose}" stop
echo "COMMAND docker compose -f deployments/chain/compose.yaml start"
HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" docker compose -f "${compose}" start
restart_height="$(wait_above restart 4 "${start_height}")"
echo "phase=restart threshold=${start_height} reached=${restart_height}"

echo "COMMAND docker compose -f deployments/chain/compose.yaml stop v4"
one_threshold="$(python3 - "${home}/topology.json" <<'PY'
import json, sys, urllib.request
nodes = json.load(open(sys.argv[1]))["validators"]
heights = []
for node in nodes:
    with urllib.request.urlopen(node["rpc"] + "/status", timeout=2) as response:
        heights.append(int(json.load(response)["result"]["sync_info"]["latest_block_height"]))
if len(set(heights)) != 1:
    raise SystemExit(f"disagreement {heights}")
print(heights[0])
PY
)"
echo "phase=stop-one threshold=${one_threshold}"
HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" docker compose -f "${compose}" stop v4
one_height="$(wait_above one-down 3 "${one_threshold}")"
echo "phase=one-down threshold=${one_threshold} reached=${one_height}"
watch one-down 3 moving

echo "COMMAND docker compose -f deployments/chain/compose.yaml stop v3"
two_threshold="$(python3 - "${home}/topology.json" <<'PY'
import json, sys, urllib.request
nodes = json.load(open(sys.argv[1]))["validators"][:3]
heights = []
for node in nodes:
    with urllib.request.urlopen(node["rpc"] + "/status", timeout=2) as response:
        heights.append(int(json.load(response)["result"]["sync_info"]["latest_block_height"]))
if len(set(heights)) != 1:
    raise SystemExit(f"disagreement {heights}")
print(heights[0])
PY
)"
echo "phase=stop-two threshold=${two_threshold}"
HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" docker compose -f "${compose}" stop v3
stall="$(watch two-down 2 stall)"
echo "phase=two-down stall=${stall}"

echo "COMMAND docker compose -f deployments/chain/compose.yaml start v3 v4"
HOST_UID="$(id -u)" HOST_GID="$(id -g)" SVN_HOME="${home}" docker compose -f "${compose}" start v3 v4
recovered="$(wait_above recovery 4 "${stall}")"
echo "phase=recovery threshold=${stall} reached=${recovered}"
sample recovery 4
