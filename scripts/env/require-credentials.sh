#!/usr/bin/env bash
# Load a credentials file or fail. Does not print the password.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-credentials"

if [[ $# -ne 1 ]]; then
  die "usage: require-credentials.sh FILE" 2
fi
svn_load_credentials "$1"
say "loaded credentials for ${SVN_DB_USER} and ${SVN_QUEUE_USER}"
