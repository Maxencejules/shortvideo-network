# Shared brief for goal workflows 07-30 (shortvideo-network)

Read this before working. AGENTS.md (auto-injected) is the contract; this brief adds the
conventions and the review standard the owner (Max) applies. Repo: /home/user/shortvideo-network.

## 0. Update for goals 11+ (2026-11-08; supersedes section 1 and the disk line of section 4 where they differ)

- Goals 00-10 are merged into main: goal 08 (PR 11, d33751e), goal 09 (PR 12, 1969b2c), goal 10 (PR 13,
  b7aaf75), auditor run-limit fix (PR 14, dbf0897). PR 15 (svnd genesis validate applies InitChain's
  Version.App rule) is open and owner-authorized to merge. Goals 07-10 stay BLOCKED in progress.md for
  their recorded reasons (owner decisions, independent operators, live keys); merged != VERIFIED.
- Goal 11 branches from main (no more stacking): branch `goal-11`, worktree
  /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/wt-11.
  /home/user/shortvideo-network itself is an old goal-07 checkout: do not use it as the code base.
- PRs: the owner authorizes each PR and each merge separately (AGENTS.md). Agents never open PRs,
  merge, push, or resolve review threads. The orchestrator commits and pushes.
- Disk: on 2026-11-08 ~10:10Z the orchestrator ran `go clean -cache` (13 GB free afterwards; the Go
  build cache refills as you build). Old scratch of goals 06-10 is still there (cleanup not yet
  allowed by the owner). Still: no repository copies or fresh GOCACHEs without the orchestrator's
  instruction; delete what you create; stop and report below 2 GB free.
- Python: worktrees have no .venv. Use /home/user/shortvideo-network/.venv/bin/python (same
  requirements.lock, Python 3.12.3).
- Node-starting tests (devnet, svnd start) run inside the loopback-only namespace:
  <scratchpad>/netns-run.sh <cmd>. Never bind non-loopback ports.
- Never run `git stash`, `checkout` or `reset` in a worktree another process may be using.

## 1. Situation

- Goals 00-06 exist. Goal 06 (x/authority) is MERGED: PR 9 was squash-merged into main as f5916bd (tree ba701bb9, identical to goal-06 head 0936332) on 2026-11-02. Its round 3 redesigned x/authority (per-object store layout 0x01-0x17, D1 provider-message router FORBID until goal 07, export refusal, app.ValidateAuthorityGenesis, 37 full-store reject rows, devnet stopForStall/stallLimit stall check). Later goal branches must build on that merged design, not on round 2 (3853e34).
- Each later goal is a branch `goal-NN` stacked on `goal-(NN-1)`. Do not merge, rebase, force-push,
  tag, open PRs, publish, or deploy. The orchestrator commits and pushes; agents do NOT run
  `git commit`/`git push` unless their prompt explicitly says so.
- Goal specs: docs/goals/NN-*.md (exact playbook slices; NEVER edit them, a checker byte-compares them).
- Approved decisions: docs/decision-register.md (DEC-001..012 APPROVED at Gate A with the PROPOSED
  defaults; those defaults deliberately leave numbers UNSET: lease durations, per-account
  allowances, retention periods, council members, licence file, operator list). Unset numbers must
  stay configurable and labelled as unset/proposed; never invent them as policy.
- Protocol v0 (goal 02) is the frozen candidate: docs/protocol.md, docs/protocol/proto/svn/**.proto
  (sources) generated into proto/svn/**.pb.go (Go, module github.com/Maxencejules/shortvideo-network/protocol)
  and proto/gen/python. The executable reference semantics are docs/evidence/goal-02/spec_machine.py
  (op_register_provider, op_register_content, op_withdraw_content, op_offer, op_accept, op_renew,
  op_end, op_audit, op_drain, op_rotate, op_update_capacity, ...) and cases.py. Implement the chain to
  match those semantics. Do NOT change .proto files or bounds.json without flagging it as a protocol
  change needing a recorded decision; if a change is unavoidable, keep it minimal, regenerate with
  `bash scripts/gen_proto.sh` (protoc is in .toolchains/bin, add it to PATH), and list it as a
  decision request in the evidence file.
- Requirements matrix: docs/requirements.md block `requirements-matrix`. Each row names the goal(s)
  and the TEST assigned. Evidence must map every row assigned to the goal to an executed test or
  an explicit BLOCKED/DEFERRED reason.

## 2. Hard rules (any violation is a review FIX)

- Real Cosmos SDK v0.55.0 / CometBFT v0.40.0 chain; no simulated ledger; no new consensus.
- No tradable token, coin, mint, distribution, staking, slashing, fees paid to holders, rewards.
- No video bytes, personal data, emails, private social records, or moderation narratives on chain or in
  chain events. Opaque hashes can still be linkable: say so.
- Chain execution deterministic: no time.Now, rand, floats, os.Getenv, exec, network or file IO in
  chain non-test code (devnet harness excepted). Block time/height from the header. Sort map keys
  before iteration that affects state or output.
- Rejected messages must leave the full module state byte-identical (all KV keys of the module,
  including indexes/side documents), tested through the keeper/app path with bytes.Equal, for every
  reject class.
- Bound every input: string lengths, byte totals, page limits, batch sizes, durations (upper AND lower
  bounds), counters (overflow checks), per-block work.
- Provider signatures are not service signatures. A service credential never manufactures provider
  consent. Broadcast ack != committed success.
- Mocks only in unit tests. Never present a mock, stub, simulated or single-host result as
  end-to-end, independent-operator, or production evidence. Label single-machine multi-process
  demos as such.
- No secrets, keys, media uploads or DB dumps in Git or logs. Fresh dev-only keys generated at runtime.
  `scripts/scan_secrets.py` must stay green.
- Do not weaken, skip or delete an existing test/requirement/check to get green.

## 3. Review standard learned from the owner's PR 9 review (apply proactively)

1. Every factual claim in docs/evidence/goal-NN.md must be backed by a committed log file under
   docs/evidence/logs/goal-NN/ that was produced by the command on the cited tree. No numbers from
   memory, no numbers from a different run. If a claim has no log, drop it or mark it unverified.
2. Logs are produced with `scripts/evidence/capture.sh <log> -- <cmd...>`, which writes COMMAND, DATE,
   HEAD, TREE, DIRTY (+DIRTY_PATH lines for anything outside docs/evidence/) and EXIT:<code>. The
   tested code must be committed first so DIRTY is `no` (or only evidence paths).
3. Use `-v` (verbose) for Go tests whose individual test names are cited, so the log shows them.
4. Fuzzing: a state-transition fuzz (random bounded op sequences checking invariants and
   reject-equals-same-bytes), plus parser fuzz, and a real `go test -fuzz=... -fuzztime=Ns` run logged.
   A plain `go test` that only runs fuzz seeds is NOT a fuzz run.
5. Height/time semantics: assert both effective-1 and effective, not just "eventually".
6. Docs and code comments must describe what the code actually does. Never call something a
   committed handler/transaction unless it is wired in app/services.go and exercised by a test.
   Fix stale comments when behaviour changes.
7. Use parsed typed structs; no string scraping of JSON. No dead code (unused helpers, no-op deletes).
8. Halt/consensus-failure behaviour must be tested at app (FinalizeBlock) level, not only engine level.
9. docs/progress.md row and docs/progress.tsv row must agree (checker enforces), status one of
   IMPLEMENTED|VERIFIED|BLOCKED|DEFERRED; rows only for started goals.
10. Tables in evidence: per acceptance check -> status (VERIFIED/BLOCKED/DEFERRED/IMPLEMENTED) and the
    log that proves it. A check that did not run is BLOCKED with the exact command preserved.
11. State what is NOT done (follow-ups) explicitly. Overclaiming is the most common FIX reason.
12. Writing style of existing docs: short declarative sentences, no marketing, no hedging filler.

## 4. Environment of THIS container (Claude Code remote session, not the Cursor VM of goals 00-06)

- Linux 6.18, root, 4 CPUs, 15 GiB RAM, ~29 GB free disk. Outbound HTTPS via proxy (apt, pypi, npm,
  proxy.golang.org work). No inbound ports from outside.
- Go 1.26.10 (`go`), Python 3.12.3 venv at .venv (use `.venv/bin/python`, `.venv/bin/pytest`,
  `.venv/bin/ruff`, `.venv/bin/mypy`), Node 22.23.4 / npm (node_modules installed via `npm ci`),
  protoc 36.2 at .toolchains/bin (export PATH="$PWD/.toolchains/bin:$PATH").
- PostgreSQL 16.15 binaries at /usr/lib/postgresql/16/bin (system cluster is down; use a disposable
  data dir under .local/ or the scratchpad, run as user `postgres` or with initdb as non-root via
  `su postgres`/`runuser`; never commit data dirs). Note: goal 04 pinned PostgreSQL 18.6 on the Cursor
  VM; here 16.15 is what exists -- record the version actually used.
- The orchestrator ran `bash scripts/env/install.sh` (goal 04 installer): it installed PostgreSQL
  18.6 (PGDG, /usr/lib/postgresql/18/bin -- the version scripts/env uses), Erlang OTP 27.3.4.18 and
  extracted RabbitMQ 4.4.0 to .toolchains/rabbitmq_server-4.4.0 (signature verified), then exited 1 at
  its final check "python3 is not 3.12.3" because system python3 is 3.13.18 (the repo venv .venv is
  built from /usr/bin/python3.12 = 3.12.3). scripts/env/start.sh/stop.sh/reset.sh manage a disposable
  workspace under .local/workspaces (PostgreSQL on 127.0.0.1 + RabbitMQ); prefer them over ad-hoc
  servers, and if a script assumes python3==3.12.3 or a non-root user, record the workaround.
- Redis 7.0.15 server also present. Celery is not in requirements.lock (goal 04 pinned the 5.6.3 sdist hash).
- FFmpeg 6.1.1-3ubuntu5 (GPL build). Chromium + Playwright preinstalled (require('playwright') works,
  chromium.launch() needs no options; executable /opt/pw-browsers/chromium if a pinned
  @playwright/test needs it). Do not run `playwright install`.
- Docker: CLI 29.9.2 + compose v5.6.1 + buildx. The orchestrator started a LOCAL dockerd 29.9.2
  (unix:///var/run/docker.sock, cgroupfs, cgroup v1, overlayfs) on 2026-11-01T01:40Z; it is running.
  Pinned Docker Hub image pulls work (debian:bookworm-slim@sha256:a3bba7dc... pulled). Containers run
  with --network host and on the default bridge (bridge peer-to-peer delivery between containers is
  untested; prove-containers.sh uses host networking). The daemon does NOT survive a container
  restart (seen 2026-11-01 ~06:31Z; restarted at ~08:00Z). If `docker info` fails, start it with
  `(nohup dockerd > <scratchpad>/logs/dockerd.log 2>&1 &)` and record that in evidence; never
  record a missing daemon as a pass. Never use a remote daemon or port 2375. `docker build` steps that download (go mod, pip, npm, apt) fail TLS inside the build container because it does not trust this session's egress-proxy CA (/root/.ccr/ca-bundle.crt; see /root/.ccr/README.md). The unchanged repo build (e.g. scripts/devnet/prove-containers.sh) therefore exits 1 here: record that run as BLOCKED (environment), and if useful run a labelled copy whose only change is the build step (`DOCKER_BUILDKIT=1 docker build --network host --build-arg HTTPS_PROXY=... --secret id=proxyca,src=/root/.ccr/ca-bundle.crt` with a Dockerfile copy that mounts the secret into /usr/local/share/ca-certificates and runs update-ca-certificates in the discarded build stage). Goal 07 did this: docs/evidence/goal-07/containers/. Never bake the CA into a committed image or disable TLS verification. Record
  any container use in evidence. Containers are useful for isolation (--network none, --read-only,
  --user, --memory, --cpus, --pids-limit, --cap-drop ALL, --security-opt no-new-privileges).
- golangci-lint at /usr/local/bin.
- Existing test entry points: `make fmt lint typecheck test contract sbom secrets policy build`,
  `python3 scripts/run_tests.py --only {python,node,go}`, CI = .github/workflows/ci.yml (jobs python,
  node, go, devnet, contract). Contract checkers: docs/evidence/goal-01/check_requirements.py and
  docs/evidence/goal-02/check_protocol.py must stay green.
- Python packages are hash-locked (requirements.lock, pip-compile --generate-hashes --allow-unsafe).
  Adding a dependency means updating pyproject.toml, regenerating requirements.lock with hashes
  (exact original command from the lock header:
  `.venv/bin/pip-compile --allow-unsafe --extra=dev --generate-hashes --no-strip-extras --output-file=requirements.lock pyproject.toml`), updating
  docs/dependencies.md and `python scripts/write_sbom.py` (sbom check must pass). Same discipline for
  npm (package-lock.json) and Go (go.mod/go.sum). Pin exact versions; prefer what is already locked.
- Python version pin is 3.12 (pyproject requires >=3.12.3,<3.13).

## 5. Model and effort recording (every docs/evidence/goal-NN.md)

- Model that ran: `claude-opus-5-5` (Claude Code remote session `session_014mwakeb27YrugDfwAx8vXL`;
  source: claude-code-remote get_session -> session_context.model and
  external_metadata.last_served_model). Workflow subagents inherit the session model.
- Effort setting actually used: get_session reports session_context.effort_level = `ultracode`.
  Goals 29-30 subagents additionally run with per-agent effort `max` (highest available), as AGENTS.md
  requires for Extra High goals. Record exactly that; do not claim any other setting.
- Playbook assignment: 05-12 GPT-6 Astra HIGH; 13-28 GPT-6.1 Sol HIGH; 29-30 GPT-6 Astra EXTRA HIGH.
  The playbook model was not used. AGENTS.md's model-substitution note names Cursor cloud agents; this
  run is Claude Code, a further substitution -- record it. Max waived model mismatch for goals 04-06
  ("Don't mind the ai model requirement", 2026-10-31).
- Owner ruling for goals 07-30. On 2026-11-01, received by this session at about 16:34 UTC, the owner wrote in
  the Claude Code session:
  - "I acknowledge the model substitution for DR-06-07", then
  - "extend the acknowledgement to goals 07 through 30".

  So the substitution of Claude Code `claude-opus-5-5` with effort `ultracode` (plus per-agent `max` for
  goals 29-30) for the playbook model is owner-acknowledged for goals 06 (fix round) through 30, and a goal
  is not BLOCKED on model allocation for it.
  - Quote both sentences verbatim in the evidence file.
  - Still record the model and effort that actually ran. If get_session shows a different serving model or
    a fallback for any part of a goal, the ruling does not cover it: record it and flag it for the owner.
  - The ruling is about model allocation only. It does not certify correctness and does not replace any
    test.

## 6. Evidence file shape (follow docs/evidence/goal-06.md)

Status line; public-launch/no-token line; Model and effort; Tested revision (commit + tree, base,
branch); Prerequisites table; Acceptance table (check | status | log); Requirements rows for this goal
(id | status | evidence); Commands blocks (COMMAND/TREE/EXIT copied from logs); Blockers; Follow-ups;
Residual risks; Next safe action; Proposed PR title + review summary.
