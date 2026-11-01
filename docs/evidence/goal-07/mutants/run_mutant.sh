#!/usr/bin/env bash
# Run one goal-07 mutant against the tests that should kill it.
#
# usage: docs/evidence/goal-07/mutants/run_mutant.sh <mutant>
#
# Each mutant is a full copy of one committed chain source file with one
# small change, stored as <mutant>.go.txt next to this script. The script
# prints the diff from the file in the worktree to the mutant, writes a
# go -overlay JSON that maps that repository file to the mutant copy, and
# runs the named tests from chain/. It exits with the go test exit code.
# Non-zero means the tests failed on the mutant: the mutant was killed.
# Exit 0 means the mutant survived. The repository files are not modified.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(git -C "$here" rev-parse --show-toplevel)"
name="${1:-}"

# mutant -> repository-relative target file, package (relative to chain/),
# and the -run pattern of the tests expected to kill it.
case "${name}" in
  m01-simulate-extra-gas)
    target=chain/msgkit/enforce.go pkg=./app/ run='^TestSimulateEstimatesGas$' ;;
  m02-ante-gas0-always)
    target=chain/app/ante.go pkg=./app/ run='^TestSimulateEstimatesGas$' ;;
  m03-query-gas-zero-unlimited)
    target=chain/app/querygas.go pkg=./app/ run='^(TestQueryGasLimit|TestQueryGasLimitOption)$' ;;
  m04-rotation-cap-off-by-one)
    target=chain/x/providers/msg_server.go pkg=./x/providers/ run='^TestRotationCap$' ;;
  m05-authority-meter-finalize-only)
    target=chain/x/authority/msg.go pkg=./x/authority/ run='^(TestEnforceCommitted|TestHandlerMeterCheckByMode)$' ;;
  *)
    echo "unknown mutant: ${name}" >&2
    exit 2 ;;
esac

mutant="${here}/${name}.go.txt"
if [[ ! -f "${mutant}" ]]; then
  echo "missing ${mutant}" >&2
  exit 2
fi

echo "mutant ${name}: target ${target}, package ${pkg}, tests ${run}"
if diff -u "${root}/${target}" "${mutant}"; then
  echo "mutant ${name} is identical to ${target}; not a mutant" >&2
  exit 2
fi

overlay="${TMPDIR:-/tmp}/svn-goal07-${name}.overlay.json"
printf '{"Replace":{"%s":"%s"}}\n' "${root}/${target}" "${mutant}" >"${overlay}"
echo "overlay ${overlay}: $(cat "${overlay}")"

cd "${root}/chain" || exit 2
go test -count=1 -overlay "${overlay}" -run "${run}" "${pkg}"
status=$?
echo "mutant ${name}: go test exit ${status} (non-zero = killed, 0 = survived)"
exit "${status}"
