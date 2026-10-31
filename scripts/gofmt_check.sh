#!/usr/bin/env bash
# Fail when any tracked Go file in the repository is not gofmt-clean.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "${root}"

mapfile -t files < <(
  find . -name '*.go' \
    -not -path './.git/*' \
    -not -path './.toolchains/*' \
    -not -path './.venv/*' \
    -not -path './node_modules/*' \
    | sort
)
if [[ "${#files[@]}" -eq 0 ]]; then
  echo "no go files" >&2
  exit 1
fi
dirty="$(gofmt -l "${files[@]}")"
if [[ -n "${dirty}" ]]; then
  echo "${dirty}"
  exit 1
fi
echo "gofmt ok files ${#files[@]}"
