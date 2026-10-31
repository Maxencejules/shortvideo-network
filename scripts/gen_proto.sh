#!/usr/bin/env bash
# Generate Go and Python stubs from the frozen protocol sources.
# Handwritten logic stays out of proto/svn and *_pb2.py files.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "${root}"

if [[ -x "${root}/.toolchains/go/bin/go" ]]; then
  export PATH="${root}/.toolchains/go/bin:${root}/.toolchains/bin:${PATH}"
elif [[ -x "${root}/.toolchains/bin/protoc" ]]; then
  export PATH="${root}/.toolchains/bin:${PATH}"
fi

protoc_version="$(protoc --version)"
plugin_version="$(protoc-gen-go --version)"
[[ "${protoc_version}" == "libprotoc 36.2" ]] || {
  echo "protoc version ${protoc_version}; want libprotoc 36.2" >&2
  exit 1
}
[[ "${plugin_version}" == "protoc-gen-go v1.36.12" ]] || {
  echo "protoc-gen-go version ${plugin_version}; want v1.36.12" >&2
  exit 1
}

mapfile -t files < <(find docs/protocol/proto -name '*.proto' | sort)
if [[ "${#files[@]}" -eq 0 ]]; then
  echo "no proto sources" >&2
  exit 1
fi

find proto/svn -name '*.pb.go' -delete 2>/dev/null || true
find proto/gen/python -name '*_pb2.py' -delete 2>/dev/null || true
mkdir -p proto/gen/python

protoc \
  -I docs/protocol/proto \
  --go_out=proto \
  --go_opt=module=github.com/Maxencejules/shortvideo-network/protocol \
  --python_out=proto/gen/python \
  "${files[@]}"

while IFS= read -r dir; do
  init="${dir}/__init__.py"
  if [[ ! -f "${init}" ]]; then
    printf '%s\n' '"""Package marker for generated protobuf modules."""' > "${init}"
  fi
done < <(find proto/gen/python -type d | sort)

echo "generated ${#files[@]} proto files"
