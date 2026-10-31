#!/usr/bin/env bash
# Install the pinned Go and protoc toolchains into .toolchains/.
# Checksums come from go.dev and the protobuf v36.2 release assets.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
dest="${root}/.toolchains"
mkdir -p "${dest}"

go_version="1.26.10"
go_tarball="go${go_version}.linux-amd64.tar.gz"
go_sha="ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5"
protoc_zip="protoc-36.2-linux-x86_64.zip"
protoc_sha="121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278"
plugin_version="v1.36.12"

protoc_only=0
if [[ "${1:-}" == "--protoc-only" ]]; then
  protoc_only=1
fi

if [[ "${protoc_only}" -eq 0 ]]; then
  if ! "${dest}/go/bin/go" version 2>/dev/null | grep -q "go${go_version} "; then
    tmp="$(mktemp)"
    curl -fsSL -o "${tmp}" "https://dl.google.com/go/${go_tarball}"
    echo "${go_sha}  ${tmp}" | sha256sum -c -
    rm -rf "${dest}/go"
    tar -C "${dest}" -xzf "${tmp}"
    rm -f "${tmp}"
  fi
  export PATH="${dest}/go/bin:${PATH}"
fi

if ! "${dest}/bin/protoc" --version 2>/dev/null | grep -q "libprotoc 36.2"; then
  tmp="$(mktemp)"
  curl -fsSL -o "${tmp}" "https://github.com/protocolbuffers/protobuf/releases/download/v36.2/${protoc_zip}"
  echo "${protoc_sha}  ${tmp}" | sha256sum -c -
  mkdir -p "${dest}"
  unzip -q -o "${tmp}" -d "${dest}"
  rm -f "${tmp}"
fi

export PATH="${dest}/bin:${PATH}"
if [[ "${protoc_only}" -eq 0 ]]; then
  export PATH="${dest}/go/bin:${PATH}"
fi
export GOBIN="${dest}/bin"
if ! "${dest}/bin/protoc-gen-go" --version 2>/dev/null | grep -q "${plugin_version}"; then
  go install "google.golang.org/protobuf/cmd/protoc-gen-go@${plugin_version}"
fi

echo "go: $(go version)"
echo "protoc: $(protoc --version)"
echo "protoc-gen-go: $(protoc-gen-go --version)"
echo "PATH prefix: ${dest}/go/bin:${dest}/bin"
