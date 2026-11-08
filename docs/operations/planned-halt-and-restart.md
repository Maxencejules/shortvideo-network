# Planned halt and restart

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. On the devnet one operator runs all four nodes on one machine; these steps are not evidence of independent operators.

An upgrade is a planned halt: at the plan height h every node that runs the old binary stops before it writes block h, and each node resumes when its operator starts the binary for the plan. The behaviour is specified in [governance-and-upgrades.md](../architecture/governance-and-upgrades.md#164-planned-halt) sections 14 and 16.4. Install the binary first ([operator-installation.md](operator-installation.md)).

The walkthrough (`scripts/devnet/walk-runbooks.py`) runs the tagged blocks of this page on a devnet with no upgrade plan scheduled, so it shows no halt: it shows that the commands stop, copy, check, start, and compare a node. The four-process halt and binary swap is `TestDevnetUpgradeRehearsal` (single-machine four-process devnet, not independent operators).

## Shell variables

The variables of [operator-installation.md](operator-installation.md#shell-variables), and:

| Variable | Meaning |
| --- | --- |
| `PEER_RPC` | the CometBFT RPC URL of another node, to compare history |
| `NODE_COPY` | a new directory for this node's copy taken at the halt; it must not exist yet |
| `NOTES` | a directory for the operator's records of this upgrade |
| `CHECK_BIN` | a state check build of the binary the nodes run now (`go build -tags svn_statecheck ./cmd/svnd`, run from `chain/`): a test tool that links test-support checkers, never a release and never used to run a node |

## Before the plan height

- Providers renew every lease whose deadline falls in the expected halt. `RenewLease` cannot commit while the chain is halted, and expiry runs again as soon as blocks commit (`docs/protocol.md:258`).
- No maximum planned halt is set (decision request DR-10-39). Lease terms are unset (DEC-005), so the risk is open.
- Record the state the halt must not change:

```bash walk:record-before
mkdir -p "$NOTES"
sha256sum "$SVN_HOME/genesis.json" | tee "$NOTES/genesis.sha256"
curl -fsS "$PEER_RPC/block?height=2" | jq -r '.result.block_id.hash' | tee "$NOTES/block-2.hash"
bash scripts/devnet/status.sh
```

## How a node shows the halt

At h a node without the upgrade fails the block in PreBlock with `UPGRADE "<name>" NEEDED at height <h> release_digest=<hex>`, before anything of block h is written. CometBFT logs `CONSENSUS FAILURE!!!` and stops consensus; the latest committed height stays at h-1. Block h stays in the block store; the app stays at h-1. Restarting the old binary is refused: `load gate at height <h-1> refused this binary (row 2): upgrade <name> is due at height <h>; install the binary for it (release digest <hex>)` (`TestUpgradeRehearsalInProcess`).

```bash walk:look-for-halt
"$SVN_BIN" status --rpc "$NODE_RPC" || echo "the RPC of $NODE does not answer"
grep -E 'UPGRADE ".*" NEEDED|CONSENSUS FAILURE' "$NODE/node.log" | tail -n 3 || echo "no halt line in $NODE/node.log"
```

## Stop, copy, swap, start

Do steps 1 to 4 for each node in turn, then wait once for two blocks ([after the last node started](#after-the-last-node-started)). On the devnet, repeat steps 1 to 4 with `NODE` set to each of `nodes/v1` to `nodes/v4`.

1. Stop the node with its own pidfile and wait until it exits:

```bash walk:stop-node
pid="$(cat "$NODE/svnd.pid")"
kill -TERM "$pid"
for _ in $(seq 1 60); do
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.5
done
if kill -0 "$pid" 2>/dev/null; then
  echo "svnd pid $pid did not stop" >&2
  exit 1
fi
```

2. Copy the whole home while the node is stopped. This copy, taken at the halt, is the only input of the one local restore that is allowed ([failed-upgrade.md](failed-upgrade.md#restore-a-lone-diverged-node)). It holds the validator key: keep it private on the same machine and never start a node from it.

```bash walk:copy-at-halt
test ! -e "$NODE_COPY"
cp -a "$NODE" "$NODE_COPY"
du -sh "$NODE_COPY"
```

   Check the copy with the state check. It opens the copy's databases read-only (a copy without a database's `LOCK` file is refused) and prints the chain id, the height, the app hash, the load gate row for the binary `CHECK_BIN` describes, one SHA-256 per store, an `i13` digest, and every invariant check; it exits 1 when a check fails or the gate refuses with row 5 or 6. At the halt the copy is at h-1 and the gate row is 2 (the upgrade is due). Keep the report: the reports of all nodes at the same height must be byte-identical.

```bash walk:check-copy
mkdir -p "$NOTES"
"$CHECK_BIN" statecheck --home "$NODE_COPY" | tee "$NOTES/statecheck-$(basename "$NODE").txt"
```

3. Swap and start: start the node from its home with the installed binary.

```bash walk:start-new
nohup "$INSTALL_DIR/svnd-next" start --home "$NODE" >>"$NODE/node.log" 2>&1 &
echo $! >"$NODE/svnd.pid"
```

4. Wait until the node runs and its RPC answers. At the plan height it commits no block until nodes with more than two thirds of the voting power run the binary for the plan, so do not wait for blocks here. A start the load gate refuses exits 1 within seconds, and the last line of `node.log` names the gate row.

```bash walk:wait-up
height() { "$SVN_BIN" status --rpc "$NODE_RPC" 2>/dev/null | awk '$1 == "height" {print $2}'; }
up=""
for _ in $(seq 1 60); do
  if ! kill -0 "$(cat "$NODE/svnd.pid")" 2>/dev/null; then
    tail -n 5 "$NODE/node.log" >&2
    exit 1
  fi
  up="$(height || true)"
  [ -n "$up" ] && break
  sleep 1
done
echo "node is up at height $up"
test -n "$up"
```

## After the last node started

When steps 1 to 4 are done for every node, wait until the node has committed two blocks after this point.

```bash walk:wait-two-blocks
height() { "$SVN_BIN" status --rpc "$NODE_RPC" 2>/dev/null | awk '$1 == "height" {print $2}'; }
up="$(height || true)"
test -n "$up"
now="$up"
for _ in $(seq 1 120); do
  now="$(height || true)"
  [ -n "$now" ] && [ "$now" -ge "$((up + 2))" ] && break
  sleep 1
done
echo "node was at height $up and reached height $now"
test "$now" -ge "$((up + 2))"
```

## After the restart

Check that history was not rewritten: the genesis file is unchanged, an earlier block hash equals the one recorded before the halt, and the node still serves its history from height 1.

```bash walk:compare-history
sha256sum -c "$NOTES/genesis.sha256"
test "$(curl -fsS "$NODE_RPC/block?height=2" | jq -r '.result.block_id.hash')" = "$(cat "$NOTES/block-2.hash")"
test "$(curl -fsS "$NODE_RPC/status" | jq -r '.result.sync_info.earliest_block_height')" = "1"
bash scripts/devnet/status.sh
```

After an upgrade, also read `Query/Policy` ([council-changes.md](council-changes.md#reading-the-committed-result)): `state.appVersion` is the new app version and `state.upgradePlan` is absent.

## Times and due work after the halt

- Block h+1's header time is the median time of block h's precommits, which were cast before the halt. So h+1 carries a time from before the halt, and the jump appears at h+2.
- Everything that fell due during the halt becomes due at h+2, at most 128 items per block (`docs/protocol.md:256`).
- Proposals approved but not executed before the upgrade fail at execute with `ERROR_CODE_STALE_VERSION` (or `ERROR_CODE_CONFLICT`): the applied upgrade bumps the policy version (owner ruling Q2, "Council plan, fail closed (Recommended)"). Propose them again.

## The upgrade window (DR-10-41)

A node whose block store does not yet hold block h, started with the new binary at h-1, opens the upgrade window: until block h commits, `CheckTx` and queries run the new code on app-version-1 state. `Query/Policy` then reports the old app version and the due plan. After any start, with or without an upgrade, `CheckTx` rejects a correctly signed transaction from a normal account with sdk code 4 until the node commits its first block (`TestUpgradeWindowCheckTxAndQueries`). Do not broadcast to a node until it has committed a block after its start. The window is documented, not refused; refusing it is a reversible option (decision request DR-10-41, interim).

A node that halted at h already holds block h in its block store. CometBFT replays block h during the start, before the node serves `CheckTx` or queries, so for that node the window closes before it opens.

## What an operator must not expect

- The halt is not a pause switch. No message and no node setting makes the chain halt or resume; the plan height and the operators' binaries do.
- A halted chain commits no council message, including a cancel of the plan.
- After an upgrade that changes a store layout, queries and state checks for heights below h must use the old binary.
