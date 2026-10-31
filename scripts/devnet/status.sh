#!/usr/bin/env bash
# Print each validator's committed height and application hash.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
home="${SVN_HOME:-${root}/.local/devnet/current}"
binary="${SVN_BIN:-${root}/bin/svnd}"

if [[ ! -f "${home}/topology.json" ]]; then
  echo "no topology at ${home}" >&2
  exit 2
fi
if [[ ! -x "${binary}" ]]; then
  echo "missing ${binary}" >&2
  exit 2
fi

python3 - "${home}/topology.json" "${binary}" <<'PY'
import json, subprocess, sys
topology = json.load(open(sys.argv[1], encoding="utf-8"))
binary = sys.argv[2]
print("chain_id", topology["chain_id"])
print("genesis_sha256", topology["genesis_sha256"])
for node in topology["validators"]:
    completed = subprocess.run([binary, "status", "--rpc", node["rpc"]], text=True, capture_output=True)
    print(node["name"], "rpc", node["rpc"])
    sys.stdout.write(completed.stdout)
    if completed.returncode != 0:
        sys.stderr.write(completed.stderr)
        raise SystemExit(completed.returncode)
PY
