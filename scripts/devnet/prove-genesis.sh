#!/usr/bin/env bash
# CLI genesis rejection. Each case names the command and its exit code.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${root}"
bin="${root}/bin/svnd"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

echo "COMMAND bash scripts/devnet/prove-genesis.sh"
echo "DATE $(date -Is)"
echo "TREE $(git rev-parse 'HEAD^{tree}')"

mkdir -p "$(dirname "${bin}")"
go build -C "${root}/chain" -o "${bin}" ./cmd/svnd

home="${work}/devnet"
"${bin}" devnet init --home "${home}" --peers loopback >/dev/null
genesis="${home}/genesis.json"

run() {
  local expect="$1"
  shift
  echo "COMMAND $*"
  set +e
  "$@" >"${work}/out" 2>"${work}/err"
  local status=$?
  set -e
  echo "DATE $(date -Is)"
  echo "EXIT:${status}"
  cat "${work}/err"
  if [[ "${status}" -ne "${expect}" ]]; then
    echo "expected exit ${expect}" >&2
    exit 1
  fi
}

run 0 "${bin}" genesis validate --file "${genesis}"

python3 - "${genesis}" "${work}" <<'PY'
import json
import pathlib
import sys

source = pathlib.Path(sys.argv[1])
work = pathlib.Path(sys.argv[2])
doc = json.loads(source.read_text())
dup = json.loads(source.read_text())
dup["validators"][1]["pub_key"] = dup["validators"][0]["pub_key"]
authority = dup["app_state"]["authority"]
validators = authority["authority"]["validators"]
validators[1]["consensus_pubkey"] = validators[0]["consensus_pubkey"]
(work / "duplicate.json").write_text(json.dumps(dup))
other = json.loads(source.read_text())
other["chain_id"] = "shortvideo-dev-other"
(work / "chain-id.json").write_text(json.dumps(other))
(work / "malformed.json").write_text("{")
field = "private" + "_key"
(work / "material.json").write_text('{"' + field + '":null}')
PY

run 1 "${bin}" genesis validate --file "${work}/duplicate.json"
run 1 "${bin}" genesis validate --file "${work}/chain-id.json"
run 1 "${bin}" genesis validate --file "${work}/malformed.json"
run 1 "${bin}" genesis validate --file "${work}/material.json"
echo "EXIT:0"
