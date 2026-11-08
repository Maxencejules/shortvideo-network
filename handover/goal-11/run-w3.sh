#!/usr/bin/env bash
# Goal 11 Wave 3: evidence captures on the committed tree (plan section 15,
# as amended by the Wave 1 and Wave 2 gate reports). Sequential on purpose
# (4 CPUs; fixed devnet ports; timings). Continues after a failing step;
# every exit is in the log and in summary.tsv.
set -u
S=/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad
WT=$S/wt-11
W3=$S/goal-11/w3
L=$WT/docs/evidence/logs/goal-11
NETNS=$S/netns-run.sh
BASE=6d74aca5cfc461a9756587bdbec2f78e7111e574   # main after PR 15, the goal 11 base
REV=$(git -C "$WT" rev-parse HEAD)
SUM=$W3/summary.tsv
mkdir -p "$L" "$W3"
cd "$WT" || exit 1
# TMPDIR stays unset (/tmp): pgdev needs os.TempDir()/svn-pg-* with a socket path of at most
# 107 bytes, and the root-only uid tests run children as unused uids that cannot enter
# /tmp/claude-0. W3TMP is for this script's own scratch builds.
unset TMPDIR
W3TMP=$W3/tmp
mkdir -p "$W3TMP"
# START_AT=<step> resumes a cut-off run; ONLY="<step> ..." runs just those
# steps; END_AT=<step> stops after that step (segments of at most two hours).
START_AT=${START_AT:-}
ONLY=${ONLY:-}
[ -n "$ONLY" ] || [ -n "$START_AT" ] || : >"$SUM"

step() {
  local name=$1; shift
  if [ -n "$ONLY" ] && [[ " $ONLY " != *" $name "* ]]; then return 0; fi
  if [ -n "$START_AT" ]; then
    [ "$name" = "$START_AT" ] || return 0
    START_AT=""
    echo "RESUMED at step $name $(date -u +%H:%M:%SZ) (HEAD $REV)" >>"$SUM"
  fi
  local t0 t1 rc
  t0=$(date +%s)
  bash scripts/evidence/capture.sh "$L/$name.txt" -- "$@"
  rc=$?
  t1=$(date +%s)
  local dirty
  dirty=$(git status --porcelain --untracked-files=all | awk '{print $NF}' | grep -v '^docs/evidence/' | tr '\n' ' ')
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$rc" "$((t1 - t0))" "$(date -u +%H:%M:%SZ)" \
    "$(cut -d' ' -f1-3 /proc/loadavg)" "$(df -h / | awk 'NR==2{print $4}')" "${dirty:-clean}" >>"$SUM"
  # A killed devnet or PostgreSQL run can leave processes; never start the next step over them.
  pkill -f '/svn-pg-' 2>/dev/null; rm -rf /tmp/svn-pg-* 2>/dev/null
  if [ -n "${END_AT:-}" ] && [ "$name" = "$END_AT" ]; then
    echo "SEGMENT STOPPED after step $name $(date -u +%H:%M:%SZ) (END_AT)" >>"$SUM"
    exit 0
  fi
}
filter() { printf '%s 2>&1 | grep -a -v -F "<nil> DBG "; exit "${PIPESTATUS[0]}"' "$1"; }

[ -z "$(git status --porcelain --untracked-files=all | grep -v ' docs/evidence/')" ] || { echo "tree dirty"; exit 1; }

# --- revision, runtime, scope
step runtime bash -c 'uname -srm; grep PRETTY_NAME /etc/os-release; id -u; go version; go env GOOS GOARCH GOFLAGS GOWORK; /usr/lib/postgresql/18/bin/postgres --version; /usr/lib/postgresql/16/bin/postgres --version; .venv/bin/python --version; .venv/bin/ruff --version; .venv/bin/mypy --version; node --version; npm --version; .toolchains/bin/protoc --version; golangci-lint version; echo "cpus $(nproc)"; free -m | head -2; df -h / | tail -1; echo "worktree links (ignored by git):"; ls -ld .venv node_modules .toolchains'
step revision bash -c 'git rev-parse HEAD "HEAD^{tree}"; echo "status (empty means clean):"; git status --porcelain --untracked-files=all; echo "(end of status)"; git log --oneline -6'
step stack bash -c "git ls-remote origin refs/heads/main refs/heads/goal-11; if git merge-base --is-ancestor $BASE HEAD; then echo \"$BASE is an ancestor of HEAD\"; else echo \"$BASE is NOT an ancestor of HEAD\"; exit 1; fi; git log --oneline $BASE..HEAD"
step diff-stat git diff --stat "$BASE..HEAD"
step protocol-diff bash -c "out=\$(git diff --stat $BASE..HEAD -- docs/protocol proto docs/evidence/goal-02); echo \"git diff --stat $BASE..HEAD -- docs/protocol proto docs/evidence/goal-02:\"; echo \"\${out:-<empty>}\"; test -z \"\$out\""
CHAINPROD=$(cat <<EOF
echo "non-test, non-README files under chain/ changed since $BASE:"
files=\$(git diff --name-only $BASE..HEAD -- chain ':!chain/**/*_test.go' ':!chain/**/README.md' ':!chain/README.md')
echo "\${files:-<none>}"
echo
echo "their diff:"
git diff $BASE..HEAD -- chain ':!chain/**/*_test.go' ':!chain/**/README.md' ':!chain/README.md'
echo
echo "expected: only chain/go.mod, with github.com/lib/pq v1.12.3 moved from an indirect to a direct requirement"
want="chain/go.mod"
[ "\$files" = "\$want" ] || { echo "SCOPE DIFFERENT"; exit 1; }
added=\$(git diff -U0 $BASE..HEAD -- chain/go.mod | grep -E '^[+-][^+-]' )
echo "changed lines:"; echo "\$added"
exp=\$(printf '%s\n%s' "+	github.com/lib/pq v1.12.3" "-	github.com/lib/pq v1.12.3 // indirect")
[ "\$added" = "\$exp" ] && echo "SCOPE OK" || { echo "SCOPE DIFFERENT"; exit 1; }
git diff --exit-code --stat $BASE..HEAD -- chain/go.sum && echo "chain/go.sum unchanged"
EOF
)
step chain-prod-diff bash -c "$CHAINPROD"
step goal01-diff bash -c "git diff $BASE..HEAD -- docs/evidence/goal-01; echo; echo 'expected: the SOURCE_ALLOW entry services/broadcaster/ with its comment and the goal 11 PY_ALLOW line'"
DEPS=$(cat <<EOF
d=\$(mktemp -d "$W3TMP/deps.XXXXXX")
git worktree add -q --detach "\$d/base" $BASE || exit 90
echo "base worktree \$d/base at \$(git -C "\$d/base" rev-parse HEAD)"
(cd "\$d/base/chain" && GOPROXY=off go list -m all) > "\$d/list-base.txt" || exit 91
(cd chain && GOPROXY=off go list -m all) > "\$d/list-head.txt" || exit 92
echo "=== go -C chain list -m all, base vs HEAD (workspace mode; HEAD adds the services/broadcaster main module line):"
diff "\$d/list-base.txt" "\$d/list-head.txt"; echo "diff exit \$?"
dl=\$(diff <(grep ' ' "\$d/list-base.txt") <(grep ' ' "\$d/list-head.txt")); echo "dependency lines (module and version) differ: \${dl:-no}"
(cd "\$d/base/chain" && GOWORK=off GOPROXY=off go list -m all) > "\$d/off-base.txt" || exit 93
(cd chain && GOWORK=off GOPROXY=off go list -m all) > "\$d/off-head.txt" || exit 94
cmp "\$d/off-base.txt" "\$d/off-head.txt" && echo "GOWORK=off chain listing: byte-identical (\$(wc -l < "\$d/off-head.txt") lines)"
go build -C "\$d/base/chain" -o "\$d/svnd-base" ./cmd/svnd || exit 95
go build -C chain -o "\$d/svnd-head" ./cmd/svnd || exit 96
go version -m "\$d/svnd-base" | tail -n +2 > "\$d/m-base.txt"; go version -m "\$d/svnd-head" | tail -n +2 > "\$d/m-head.txt"
echo "=== go version -m svnd (line 1, the file name, dropped), base vs HEAD:"
diff "\$d/m-base.txt" "\$d/m-head.txt" | grep -v -E 'vcs\.(revision|time|modified)' ; echo "(lines above: differences other than vcs stamps, if any)"
diff <(grep -E '^\s*(dep|=>)' "\$d/m-base.txt") <(grep -E '^\s*(dep|=>)' "\$d/m-head.txt") && echo "svnd dependency lines: identical (\$(grep -c -E '^\s*dep' "\$d/m-head.txt") deps)"
s=\$?
git worktree remove --force "\$d/base"; rm -rf "\$d"
exit \$s
EOF
)
step dependency-identity bash -c "$DEPS"
step go-mod-tidy-broadcaster env -C services/broadcaster GOPROXY=off go mod tidy -diff
step go-mod-tidy-chain env -C chain GOPROXY=off go mod tidy -diff
step go-mod-tidy-proto env -C proto GOPROXY=off go mod tidy -diff
step go-mod-tidy-agents env -C agents/auditor GOPROXY=off go mod tidy -diff
step go-sum-subset bash -c 'n=0; m=0; while IFS= read -r l; do n=$((n+1)); grep -qxF -- "$l" chain/go.sum || { echo "not in chain/go.sum: $l"; m=$((m+1)); }; done < services/broadcaster/go.sum; echo "services/broadcaster/go.sum lines: $n; missing from chain/go.sum: $m"; test "$m" = 0'
step gofmt-vet bash -c 'bash scripts/gofmt_check.sh; a=$?; l="$(gofmt -l chain proto agents services/broadcaster)"; echo "gofmt -l: ${l:-<empty>}"; test -z "$l"; b=$?; go vet ./chain/... ./proto/... ./agents/provider/... ./agents/auditor/... ./services/broadcaster/...; c=$?; go vet -tags svn_faultpoint ./services/broadcaster/...; d=$?; go vet -tags svn_roottest ./services/broadcaster/internal/signer/... ./services/broadcaster/internal/keys/...; e=$?; go vet -tags race ./chain/devnet/; f=$?; echo "gofmt_check=$a gofmt_l=$b vet=$c vet_faultpoint=$d vet_roottest=$e vet_devnet_race=$f"; test "$a$b$c$d$e$f" = 000000'
BUILDS=$(cat <<'EOF'
d=$(mktemp -d "$W3TMP/builds.XXXXXX")
rc=0
go build -C services/broadcaster -o "$d/svn-broadcaster" ./cmd/svn-broadcaster || rc=1
go build -C services/broadcaster -tags svn_faultpoint -o "$d/svn-broadcaster-fp" ./cmd/svn-broadcaster || rc=1
sha256sum "$d"/* | sed "s#$d/##"
"$d/svn-broadcaster" --help 2>&1 | head -30
echo "fault point symbols in the default build (expected none):"; go tool nm "$d/svn-broadcaster" | grep -i faultpoint | grep -v -i 'go:\|runtime' | head -5; echo "(end)"
echo "fault point symbols in the svn_faultpoint build:"; go tool nm "$d/svn-broadcaster-fp" | grep -c -i 'faultpoint'
rm -rf "$d"
exit $rc
EOF
)
step builds bash -c "$BUILDS"

# --- Python, Node, policy and contract checks
step ruff-format .venv/bin/ruff format --check services workers/media packages/protocol tests scripts
step ruff-check .venv/bin/ruff check services workers/media packages/protocol tests scripts
step mypy .venv/bin/mypy
step scan-secrets .venv/bin/python scripts/scan_secrets.py
step sbom-check .venv/bin/python scripts/write_sbom.py --check
step workflow-policy .venv/bin/python scripts/workflow_policy.py
step check-requirements .venv/bin/python docs/evidence/goal-01/check_requirements.py
step check-protocol bash -c 'export PATH="$PWD/.toolchains/bin:$PATH"; .venv/bin/python docs/evidence/goal-02/check_protocol.py'
step tests-node .venv/bin/python scripts/run_tests.py --only node
step tests-python .venv/bin/python scripts/run_tests.py --only python
PYCLONE=$(cat <<PYEOF
echo "wt-11 layout: .venv, node_modules and .toolchains are symlinks to the main checkout:"
ls -ld .venv node_modules .toolchains
for p in node_modules/react/index.js .venv/bin/python .toolchains/bin/protoc; do
  out=\$(git check-ignore -- "\$p" 2>&1); rc=\$?; echo "wt-11: git check-ignore -- \$p -> exit \$rc \${out}"
done
d=\$(mktemp -d $W3/pyclone.XXXXXX)
git clone -q --no-checkout "\$PWD" "\$d/repo" || exit 90
git -C "\$d/repo" checkout -q --detach $REV || exit 91
cd "\$d/repo" || exit 92
echo "scratch clone \$d/repo: HEAD \$(git rev-parse HEAD) tree \$(git rev-parse 'HEAD^{tree}')"
echo "clone status (empty means clean):"; git status --porcelain --untracked-files=all; echo "(end of status)"
for p in node_modules/react/index.js .venv/bin/python .toolchains/bin/protoc; do
  git check-ignore -q -- "\$p"; echo "clone: git check-ignore -q -- \$p -> exit \$?"
done
echo "full python suite in the clone (run_tests.py falls back to sys.executable, the same venv):"
/home/user/shortvideo-network/.venv/bin/python scripts/run_tests.py --only python; s=\$?
cd / && rm -rf "\$d"
exit \$s
PYEOF
)
step tests-python-clean-clone bash -c "$PYCLONE"
step chain-writer-policy .venv/bin/python -m pytest -v -p no:cacheprovider tests/unit/test_chain_writer_policy.py tests/unit/test_ci_broadcaster.py tests/unit/test_ci_devnet_coverage.py

# --- Go: broadcaster unit and PostgreSQL, uid boundary, chain/app, regression
step unit-broadcaster "$NETNS" go test -count=1 -v -timeout 30m ./services/broadcaster/...
step race-broadcaster "$NETNS" go test -race -count=1 -timeout 30m ./services/broadcaster/...
step uid-boundary "$NETNS" go test -count=1 -v -tags svn_roottest -run 'TestServeUIDCannotReadKeyFiles|TestSignerRefusesOtherUIDPeer|TestLoadRefusesOtherOwner' ./services/broadcaster/internal/signer/ ./services/broadcaster/internal/keys/
step faultpoint-tag go test -count=1 -v -tags svn_faultpoint ./services/broadcaster/internal/faultpoint/
step app-deadline bash -c "$(filter "go test -count=1 -v ./chain/app -run '^(TestTimeoutHeightThroughFinalizeBlock|TestAccSeqPresentOnSuccess|TestAccSeqPresentOnHandlerFailure|TestAccSeqAbsentOnAnyAnteFailure|TestStrictDecoderAcceptsTimeoutHeight|TestSimulateWithEmptySignature|TestAppSimulatePathCannotEncodeSvnResults|TestSimulateRouteGasForBroadcasterKinds)\$'")"
step unit bash -c "$(filter 'env SVN_SKIP_DEVNET=1 go test -count=1 -v ./chain/...')"
step auditor env -C agents/auditor go test -count=1 -v ./...

# --- devnet (single machine, four validator processes, private network namespace)
step devnet-broadcaster "$NETNS" go test -count=1 -timeout 1800s -v -run '^TestDevnetBroadcasterLifecycle$' ./chain/devnet
step devnet-broadcaster-stall "$NETNS" go test -count=1 -timeout 1200s -v -run '^TestDevnetBroadcasterQuorumStall$' ./chain/devnet
step traces bash -c "grep -a '^trace ' $L/devnet-broadcaster.txt; exit 0"
step canary bash -c "grep -a -E '^(canary|phase=L|logs_kept|boundary_uids)' $L/devnet-broadcaster.txt; grep -a -c -E '^canary' $L/devnet-broadcaster.txt"
step devnet "$NETNS" go test -count=1 -timeout 1800s -v -skip 'TestDevnetBroadcaster|TestDevnetAudit|TestDevnetUpgrade|TestDevnetMinority|TestDevnetGovernedChange' ./chain/devnet

# --- run_tests.py --only go, CI go job, CI shards
step tests-go "$NETNS" env GOFLAGS=-count=1 .venv/bin/python scripts/run_tests.py --only go
step ci-go-job "$NETNS" env SVN_SKIP_DEVNET=1 GOFLAGS=-count=1 .venv/bin/python scripts/run_tests.py --only go
CISHARDS=$(cat <<'EOF'
# The go-race shard bodies of .github/workflows/ci.yml at this revision, run
# one after another on this machine (CI runs them as parallel jobs).
mod="github.com/Maxencejules/shortvideo-network"
rc_all=0
for SHARD in app devnet devnet-evidence devnet-upgrade devnet-broadcaster devnet-broadcaster-stall broadcaster rest; do
  t0=$(date +%s)
  echo "=== shard ${SHARD} start $(date -u +%H:%M:%SZ)"
  (
    set -e
    case "${SHARD}" in
      app)
        go test -race -count=1 -timeout 12m ./chain/app
        ;;
      devnet)
        go build -C chain -o /dev/null ./cmd/svnd
        go test -race -count=1 -timeout 12m -skip 'TestDevnetAudit|TestDevnetUpgrade|TestDevnetMinority|TestDevnetGovernedChange|TestDevnetBroadcasterLifecycle|TestDevnetBroadcasterQuorumStall' ./chain/devnet
        ;;
      devnet-evidence)
        go build -C agents/auditor -o /dev/null .
        go test -race -count=1 -timeout 15m -run TestDevnetAudit ./chain/devnet
        ;;
      devnet-upgrade)
        go build -C chain -o /dev/null ./cmd/svnd
        go build -C chain -tags svn_rehearsal -o /dev/null ./cmd/svnd
        go build -C chain -tags svn_rehearsal_bad -o /dev/null ./cmd/svnd
        go build -C chain ./internal/...
        go test -race -count=1 -timeout 15m -run 'TestDevnetUpgrade|TestDevnetMinority|TestDevnetGovernedChange' ./chain/devnet
        ;;
      devnet-broadcaster)
        go build -C chain -o /dev/null ./cmd/svnd
        go build -C services/broadcaster -race -tags svn_faultpoint -o /dev/null ./cmd/svn-broadcaster
        go test -race -count=1 -timeout 15m -run TestDevnetBroadcasterLifecycle ./chain/devnet
        ;;
      devnet-broadcaster-stall)
        go build -C chain -o /dev/null ./cmd/svnd
        go build -C services/broadcaster -race -tags svn_faultpoint -o /dev/null ./cmd/svn-broadcaster
        go test -race -count=1 -timeout 12m -run TestDevnetBroadcasterQuorumStall ./chain/devnet
        ;;
      broadcaster)
        go test -race -count=1 -timeout 12m ./services/broadcaster/...
        ;;
      rest)
        pkgs="$(go list ./chain/... | grep -v -x -e "${mod}/chain/app" -e "${mod}/chain/devnet")"
        if [ -z "${pkgs}" ]; then echo "no packages in the rest shard"; exit 1; fi
        echo "${pkgs}"
        go test -race -count=1 -timeout 12m ${pkgs}
        go test -race -count=1 -timeout 2m ./proto/...
        go test -race -count=1 -timeout 2m ./agents/provider/...
        go test -race -count=1 -timeout 2m ./agents/auditor/...
        ;;
    esac
  )
  rc=$?
  echo "=== shard ${SHARD} exit ${rc} seconds $(( $(date +%s) - t0 ))"
  [ "$rc" = 0 ] || rc_all=1
  pkill -f '/svn-pg-' 2>/dev/null; rm -rf /tmp/svn-pg-* 2>/dev/null
done
exit $rc_all
EOF
)
step ci-shards "$NETNS" bash -c "$CISHARDS"

# --- fuzz (after the suites: a crasher file would make later logs DIRTY)
fuzz() {
  local name=$1 pkg=$2 t=$3
  step "fuzz-$name" bash -c "echo \"loadavg before: \$(cat /proc/loadavg)\"; go test -run='^\$' -fuzz='^$name\$' -fuzztime=$t -count=1 $pkg; s=\$?; echo \"loadavg after: \$(cat /proc/loadavg)\"; echo 'new or changed files outside docs/evidence after the run:'; git status --porcelain --untracked-files=all | grep -v ' docs/evidence/'; echo '(end of list)'; exit \$s"
}
fuzz FuzzTransitions ./services/broadcaster/internal/outbox/ 60s
fuzz FuzzClassifyRPCError ./services/broadcaster/internal/chain/ 60s
fuzz FuzzForwardedTx ./services/broadcaster/internal/policy/ 60s
fuzz FuzzDecodeIntentRequest ./services/broadcaster/internal/api/request/ 60s

# --- lint, test-name bookkeeping
step golangci-lint bash -c 'golangci-lint version; cd chain && golangci-lint run ./...; a=$?; echo "chain=$a"; test "$a" = 0'
NAMES=$(cat <<EOF
names() { git grep -h -E '^func (Test|Fuzz)' "\$1" -- chain agents services | sed 's/(.*//; s/^func //' | sort -u; }
h=\$(names HEAD); b=\$(names $BASE)
echo "HEAD \$(git rev-parse HEAD): \$(echo "\$h" | grep -c .) Go test/fuzz function names under chain/, agents/, services/"
echo "$BASE (base): \$(echo "\$b" | grep -c .) names"
gone=\$(comm -23 <(echo "\$b") <(echo "\$h")); echo "base names missing from HEAD: \$(echo "\$gone" | grep -c .)"; echo "\$gone" | grep . || true
new=\$(comm -13 <(echo "\$b") <(echo "\$h")); echo "HEAD names not in base: \$(echo "\$new" | grep -c .)"
pyn() { git grep -h -E '^\s*def test_' "\$1" -- tests services | sed 's/(.*//; s/^\s*def //' | sort -u; }
ph=\$(pyn HEAD); pb=\$(pyn $BASE)
pgone=\$(comm -23 <(echo "\$pb") <(echo "\$ph")); echo "Python test names: HEAD \$(echo "\$ph" | grep -c .), base \$(echo "\$pb" | grep -c .), base names missing from HEAD: \$(echo "\$pgone" | grep -c .)"; echo "\$pgone" | grep . || true
test -z "\$gone"
EOF
)
step tests-not-removed bash -c "$NAMES"
step tests-replaced bash -c "echo 'pre-existing test files changed since $BASE:'; git diff --stat --diff-filter=M $BASE..HEAD -- '*_test.go' 'tests/*.py' 'tests/**/*.py' 'services/**/test*.py'; echo; echo 'deleted files since $BASE:'; git diff --stat --diff-filter=D $BASE..HEAD; echo; git diff --diff-filter=D $BASE..HEAD; echo; git diff -U0 --diff-filter=M $BASE..HEAD -- '*_test.go' 'tests/unit/*.py'"

# --- mutation runners on the committed revision
step mutants-goal11-check .venv/bin/python docs/evidence/goal-11/mutants.py --rev "$REV" --check
step mutants-goal11 "$NETNS" .venv/bin/python docs/evidence/goal-11/mutants.py --rev "$REV" --gocache "$(go env GOCACHE)" --logdir "$W3/mutlogs-goal11"
step mutants-goal10-check .venv/bin/python docs/evidence/goal-10/mutants.py --rev "$REV" --check
step mutants-goal10 "$NETNS" .venv/bin/python docs/evidence/goal-10/mutants.py --rev "$REV" --gocache "$(go env GOCACHE)" --logdir "$W3/mutlogs-goal10"
step mutants-goal09-check .venv/bin/python docs/evidence/goal-09/mutants.py --rev "$REV" --check
step mutants-goal09 "$NETNS" .venv/bin/python docs/evidence/goal-09/mutants.py --rev "$REV" --gocache "$(go env GOCACHE)" --logdir "$W3/mutlogs-goal09"
step mutants-goal08-check .venv/bin/python docs/evidence/goal-08/mutants.py --rev "$REV" --check
step mutants-goal08 "$NETNS" .venv/bin/python docs/evidence/goal-08/mutants.py --rev "$REV" --logdir "$W3/mutlogs-goal08"
step goal06-anchors .venv/bin/python docs/evidence/goal-06/mutants.py --check
step goal07-anchors .venv/bin/python docs/evidence/goal-07/mutants.py --check
step disk bash -c 'df -h /; du -sh "$(go env GOCACHE)"; ls -d /tmp/svn-pg-* 2>/dev/null || echo "no /tmp/svn-pg-* left"; pgrep -a postgres || echo "no postgres process"'
echo "WAVE3 CAPTURES DONE $(date -u +%H:%M:%SZ)" >>"$SUM"
