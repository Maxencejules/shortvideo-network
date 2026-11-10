#!/usr/bin/env bash
# Goal 18 demonstration. One fresh process on this machine.
#
# It builds the services, starts a four-validator devnet, and runs the
# upload, processing, approval, transfer, lease, publish, loss, repair,
# quorum and withdrawal scenario in services/milestone. The independent
# client is scripts/milestone/client.py. Keys stay in the test's temporary
# directory. This is a working prototype on one physical failure domain.
# It is not a reviewed public beta.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"
if [[ "${SVN_SKIP_DEVNET:-}" == "1" ]]; then
  echo "SVN_SKIP_DEVNET=1 skips the scenario. That skip is not a pass." >&2
  exit 2
fi
exec go test -count=1 -timeout 20m -v -run TestMilestone ./services/milestone/
