#!/usr/bin/env bash
# Exit 0 when HOST:PORT can be bound. Exit 1 with a clear message when it cannot.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-port"

if [[ $# -ne 2 ]]; then
  die "usage: require-port.sh HOST PORT" 2
fi
svn_require_port "$1" "$2"
