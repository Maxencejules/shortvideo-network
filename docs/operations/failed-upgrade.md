# Failed upgrade

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. On the devnet one operator runs all four nodes on one machine; these steps are not evidence of independent operators.

A failed upgrade is recovered without resetting history: the block store is never rewritten, genesis is never replaced, and no committed block is discarded. The goal says it plainly: "A governance stall cannot be fixed by an unaudited finality bypass." (`docs/goals/10-policy-governance-and-chain-upgrades.md:28-29`). The failure cases are specified in [governance-and-upgrades.md](../architecture/governance-and-upgrades.md#15-failure-behaviour-without-resetting-history) sections 15 and 16. Shell variables are those of [planned-halt-and-restart.md](planned-halt-and-restart.md#shell-variables).

## What the owner allowed

On 2026-11-07 the owner answered goal 10 question Q2 (DR-10-02) with the option "Council plan, fail closed (Recommended)". Its text says: "After a failure an operator may: restart with a corrected binary; restore a lone diverged node from its own copy taken at the halt; or wait for a fix-forward upgrade."

How this page reads those three actions (an implementation reading, not part of the ruling):

1. a corrected binary restarts at the same height, for the same plan name;
2. the lone diverged node's own copy is the copy taken at the halt, and the restore keeps its newest signing state;
3. the fix-forward upgrade is a council plan scheduled once the chain runs again.

Everything else in the list at the end of this page is forbidden.

## Failure cases

| Case | What the operator sees | Action | Rehearsal step |
| --- | --- | --- | --- |
| I1 old binary reaches the plan height | PreBlock fails block h with `UPGRADE "<name>" NEEDED at height <h> release_digest=<hex>`; CometBFT logs `CONSENSUS FAILURE!!!`; the latest height stays at h-1 | install the binary for the plan and swap ([planned-halt-and-restart.md](planned-halt-and-restart.md)) | `TestUpgradeRehearsalInProcess` (FinalizeBlock(h) fails and the finalize branch equals the committed state), `TestOnChainHaltAndSwitch`; DEVNET: `TestDevnetUpgradeRehearsal`, all four nodes log `UPGRADE "rehearsal-1" NEEDED` and `CONSENSUS FAILURE` |
| I2 old binary restarted after the halt | `svnd start` exits 1: `load gate at height <h-1> refused this binary (row 2): upgrade <name> is due at height <h>; install the binary for it (release digest <hex>)` | as I1 | `TestUpgradeRehearsalInProcess`, `TestLoadGateTable`; DEVNET: `TestDevnetUpgradeRehearsal`, B1 restarted after the halt is refused with row 2 |
| I3 new binary started before h-1 committed | exit 1: `... (row 4): too early: run the previous binary until height <h-1> ...` | run the old binary until it stops at h | `TestLoadGateTable`, `TestOnChainGateTable`; DEVNET: `TestDevnetUpgradeRehearsal`, B2 on a copy taken before h-1 is refused with row 4 |
| I4 old binary on state after the upgrade | exit 1: `... (row 5): state is newer than this binary (app version <A>, binary <P>)` | run the binary of the applied upgrade | `TestUpgradeRehearsalInProcess`, `TestLoadGateTable`; DEVNET: `TestDevnetUpgradeRehearsal`, B1 on a node after the upgrade is refused with row 5 |
| I5 binary that mounts a store the committed root does not hold | exit 1 at load, before the gate | do not run it; adding a store needs a store upgrade, which goal 10 defers (DR-10-33) | `TestStoreAbsentFromRootRefused` |
| I6 wrong migration on one node of four | that node commits a different app hash at h, refuses the other nodes' block h+1 with `wrong Block.Header.AppHash`, then stops consensus with `+2/3 prevoted for an invalid block` or `+2/3 committed an invalid block` (CometBFT logs whichever it sees first); the other three keep committing | restore that node from its copy at the halt (below) | `TestWrongMigrationDiverges` (two nodes commit different app hashes at h); DEVNET: `TestDevnetMinorityWrongMigration`, the halt and the restore below (it restored `application.db`, `blockstore.db`, `state.db`, `evidence.db`, and `cs.wal` from the node's own copy and kept the newest signing state, not the whole `data/` directory) |
| I7 binary of another app version replaying history | PreBlock fails with `stored app version <A>, binary <P>: wrong binary for height <h>`; on a fresh home InitChain refuses an upgrade binary | start a fresh node with the genesis binary ([operator-installation.md](operator-installation.md#a-node-that-joins-after-an-upgrade)) | `TestPreBlockRefusesAppVersionMismatch`, `TestNewerBinaryRefusesInitChain`; DEVNET: `TestDevnetUpgradeRehearsal`, B2 on a fresh home is refused at InitChain |
| I8 the handler fails on every node | every node fails block h with `upgrade <name> at height <h>: ...` from the handler; nothing of h commits; the plan stays due | restart with a corrected binary for the same plan name (below) | `TestUpgradeHandlerErrorHalts` (fails the same way twice, then the corrected binary applies) |

DEVNET marks a test on a single-machine four-process devnet (not independent operators); the goal-10 evidence file reports its result on the tested revision.

The goal-09 release (`ff951fc`) has no app-version rule and checks only its registry layouts. Never run it on a goal-10 home; what it does there is not claimed (decision request DR-10-43).

## Restart with a corrected binary

For I8, or for a release whose handler is wrong on every node:

1. The maintainers prepare a corrected release with the same upgrade name and the same app versions ([release-preparation.md](release-preparation.md)), and reviewers review it ([source-and-digest-review.md](source-and-digest-review.md)).
2. Its digest differs from the plan's release digest. A halted council cannot change the plan, so operators rely on the reviewed manifest, not on the plan's digest.
3. Each operator installs it and swaps as in [planned-halt-and-restart.md](planned-halt-and-restart.md). The node repeats block h with the corrected handler.

## Restore a lone diverged node

For I6 only. Continue only when the node's log has `wrong Block.Header.AppHash` and then `+2/3 prevoted for an invalid block` or `+2/3 committed an invalid block`, and the other nodes kept committing. Any other failure is not this case.

The node's own copy taken at the halt ([planned-halt-and-restart.md](planned-halt-and-restart.md#stop-copy-swap-start)) holds the block store at h and the state and app at h-1. The restore puts that `data/` back, keeps the node's newest validator signing state, and starts the correct binary. CometBFT replays block h with the correct handler and the node then syncs from its peers. Restoring `application.db` alone should not work: CometBFT's handshake checks the app hash against its own state, so the start should fail (read from the CometBFT source, `cometbft@v0.40.0/consensus/replay.go:404-409`; no test exercises it). The diverged `data/` is moved aside for analysis, not deleted.

The walkthrough runs these blocks on a healthy devnet node, so it shows that the commands restore a node that rejoins committed history; it does not produce a divergence.

```bash walk:diverged-evidence
grep -E 'invalid block|wrong Block.Header.AppHash|CONSENSUS FAILURE|UPGRADE ".*" NEEDED|load gate at height' "$NODE/node.log" | tail -n 5 || echo "no failure line in $NODE/node.log"
"$SVN_BIN" status --rpc "$PEER_RPC"
```

1. Stop the node:

```bash walk:restore-stop
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

2. Keep the newest signing state, move the diverged `data/` aside, put the copy's `data/` in place, and put the newest signing state back:

```bash walk:restore-from-copy
test -d "$NODE_COPY/data"
mkdir -p "$NOTES"
test ! -e "$NOTES/data.diverged"
cp -p "$NODE/data/priv_validator_state.json" "$NOTES/priv_validator_state.newest.json"
mv "$NODE/data" "$NOTES/data.diverged"
cp -a "$NODE_COPY/data" "$NODE/data"
cp -p "$NOTES/priv_validator_state.newest.json" "$NODE/data/priv_validator_state.json"
jq -r '.height' "$NODE/data/priv_validator_state.json"
```

3. Start the correct binary and wait for two blocks after it came up:

```bash walk:restore-start
nohup "$INSTALL_DIR/svnd-next" start --home "$NODE" >>"$NODE/node.log" 2>&1 &
echo $! >"$NODE/svnd.pid"
```

```bash walk:restore-wait
height() { "$SVN_BIN" status --rpc "$NODE_RPC" 2>/dev/null | awk '$1 == "height" {print $2}'; }
peer="$("$SVN_BIN" status --rpc "$PEER_RPC" | awk '$1 == "height" {print $2}')"
now=""
for _ in $(seq 1 120); do
  if ! kill -0 "$(cat "$NODE/svnd.pid")" 2>/dev/null; then
    tail -n 5 "$NODE/node.log" >&2
    exit 1
  fi
  now="$(height || true)"
  [ -n "$now" ] && [ "$now" -ge "$((peer + 2))" ] && break
  sleep 1
done
echo "peer was at height $peer; the restored node reached height $now"
test -n "$now"
test "$now" -ge "$((peer + 2))"
```

4. Check history:

```bash walk:restore-compare
sha256sum -c "$NOTES/genesis.sha256"
test "$(curl -fsS "$NODE_RPC/block?height=2" | jq -r '.result.block_id.hash')" = "$(cat "$NOTES/block-2.hash")"
test "$(curl -fsS "$NODE_RPC/status" | jq -r '.result.sync_info.earliest_block_height')" = "1"
bash scripts/devnet/status.sh
```

## Fix forward

When the chain runs again and a wrong rule is in force, the council schedules a new upgrade with a new name ([council-changes.md](council-changes.md#scheduling-an-upgrade)). That is the only way to change committed behaviour after more than two thirds of the voting power committed block h.

## When rollback is unsafe

- Before block h commits anywhere (I1, I8): there is nothing to roll back. The plan stays due. The forward paths are a corrected binary for the same plan name, or a cancel executed before h, which is impossible once the chain has halted.
- After more than two thirds of the voting power committed block h: running the old binary on migrated state is refused by design (I4). Restoring pre-h state on every node and executing h again discards a committed block. That is a finality bypass and the goal forbids it. A fix-forward upgrade is the path.
- One node diverged alone (I6): only the restore above, after the log line.

## Permanent loss of quorum

Losing more than one third of the voting power for good stops the chain, and no block commits, so no council message executes (`docs/protocol.md:150`; [validator-lifecycle.md](validator-lifecycle.md#loss-of-quorum)). There is no on-chain path back: `QuorumLossBehavior` is `UNSET` (DEC-008), and recovery from permanent quorum loss is out of scope for the beta (decision request DR-10-11). A new network would be a new chain id that keeps this chain's history and publishes it, and it needs its own owner decision. It is never a reset of this chain.

## Never

<!-- never-list -->
- Never run `unsafe-reset-all`, or `rollback --hard`, on any node.
- Never reset validator signing state, delete `priv_validator_state.json`, or restore an older one.
- Never present a new genesis as recovery of this chain.
- Never run two live copies of one validator key.
- Never skip a scheduled upgrade on a minority of nodes, and never use a node-local halt height (`--halt-height`) as an upgrade trigger.
- Never run any message, command, or step that marks a local row final or writes a commit (REQ-REC-001).
<!-- /never-list -->
