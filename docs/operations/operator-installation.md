# Operator installation

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. On the devnet one operator runs all four nodes on one machine; these steps are not evidence of independent operators.

This runbook covers what an operator does between an executed upgrade plan and the plan height: verify the reviewed binary, read the plan, decide, and install the binary beside the running one. The swap itself is in [planned-halt-and-restart.md](planned-halt-and-restart.md). The rules are in [governance-and-upgrades.md](../architecture/governance-and-upgrades.md#12-upgrade-plans-who-schedules-when-and-notice) sections 12 to 14 and 16.

## Shell variables

The commands run from the repository checkout and use these variables:

| Variable | Meaning |
| --- | --- |
| `SVN_HOME` | the devnet home that `svnd devnet init` wrote (`genesis.json`, `topology.json`, `nodes/v1` to `nodes/v4`) |
| `SVN_BIN` | the binary the nodes run now; `scripts/devnet/*.sh` read `SVN_HOME` and `SVN_BIN` |
| `NODE` | the home of the node being handled, for example `$SVN_HOME/nodes/v4` |
| `NODE_RPC` | that node's CometBFT RPC URL (`topology.json`, `validators[].rpc`) |
| `NEW_BIN` | the release binary as received |
| `MANIFEST` | the reviewed release manifest's digest line, in `sha256sum` format |
| `INSTALL_DIR` | where this node's next binary is installed, outside the node home |

`scripts/devnet/walk-runbooks.py` runs every block tagged `walk:<id>` in this file, [planned-halt-and-restart.md](planned-halt-and-restart.md), and [failed-upgrade.md](failed-upgrade.md), in that order, against a disposable single-machine devnet.

## How an operator chooses

- The council schedules a plan; it cannot install a binary on any node. Each operator decides whether to install the reviewed release before the plan height.
- A node that does not install stops at the plan height. If more than one third of the voting power does not install, no block is made at the plan height (DEC-011, `docs/protocol.md:150`), and the chain resumes when enough power runs the binary for the plan.
- An operator who finds the release wrong does not install it and raises it with the council before the plan height. Only the council's executed cancel removes the plan ([council-changes.md](council-changes.md#cancelling-an-upgrade)); a node cannot skip it.

## 1. Verify the binary

The digest must equal the reviewed manifest's ([source-and-digest-review.md](source-and-digest-review.md)). The binary must be a release build: no `-tags` setting in its build information, and no line "This build starts only on a chain whose id starts with ..." in `svnd --help` (the rehearsal builds print it).

```bash walk:verify-binary
echo "$(cut -d ' ' -f 1 "$MANIFEST")  $NEW_BIN" | sha256sum -c -
"$NEW_BIN" version
info="$(go version -m "$NEW_BIN")"
help="$("$NEW_BIN" --help)"
case "$info" in
  *-tags=*) echo "refusing $NEW_BIN: it was built with a build tag, so it is not a release build" >&2; exit 1 ;;
esac
case "$help" in
  *"This build starts only"*) echo "refusing $NEW_BIN: it is a rehearsal build" >&2; exit 1 ;;
esac
```

In the `version` output check:

- `vcs.revision` is the reviewed commit and `vcs.modified` is `false`;
- `upgrades none`, or exactly one line `upgrade <name> from_app_version <A> to_app_version <A+1>` whose name is the plan name and whose `A` is the chain's current app version.

## 2. Read the plan

`svnd` has no query subcommand. Read `Query/Policy` from the node:

```bash walk:read-plan
.venv/bin/python scripts/devnet/walk-runbooks.py query policy --rpc "$NODE_RPC"
```

Compare `state.upgradePlan.name` with the binary's `upgrade` line, `state.upgradePlan.releaseDigest` with the manifest's digest, and `state.appVersion` with `from_app_version`. Note `state.upgradePlan.height`: it is the plan height h. With no plan, `state.upgradePlan` is absent and there is nothing to install.

The release digest in the plan is information. The chain never compares it with the running binary.

## 3. Install beside the running binary

Install the new binary under its own path. Do not overwrite the binary the node runs: the node keeps running the old binary until block h-1 commits, and the old binary is what reads heights below h.

```bash walk:install-beside
install -D -m 0755 "$NEW_BIN" "$INSTALL_DIR/svnd-next"
test "$(sha256sum "$INSTALL_DIR/svnd-next" | cut -d ' ' -f 1)" = "$(cut -d ' ' -f 1 "$MANIFEST")"
"$INSTALL_DIR/svnd-next" version
```

## Starting the new binary early

A binary built for the plan refuses to start before block h-1 has committed: `svnd start` exits 1 with `load gate at height L refused this binary (row 4): too early: run the previous binary until height <h-1>`. It exits before CometBFT starts and before the validator key is loaded. The gate only reads; after the refusal the old binary still starts on the same home with gate row 1 (`TestLoadGateTable`, `TestOnChainGateTable`). Started on a home with no plan, it exits 1 with row 6 ("stored app version ... is one behind this binary ... and no upgrade plan is stored"; `TestCancelBeforeHeightRemovesPlan`). Run the old binary until it stops at h, then swap ([planned-halt-and-restart.md](planned-halt-and-restart.md)).

## One operator, four nodes

On the devnet the operator swaps each node in turn with its own pidfile, as in [planned-halt-and-restart.md](planned-halt-and-restart.md). `scripts/devnet/resume.sh` starts the four nodes of `SVN_HOME` with `SVN_BIN` in order and stops at the first node that is already running, after starting the nodes before it; use it only on the one-operator devnet, after every node has stopped, with `SVN_BIN` set to the new binary.

## A node that joins after an upgrade

A new node starts from the genesis binary. An upgrade binary refuses to initialize a chain (`TestNewerBinaryRefusesInitChain`): InitChain requires app version 1. The node syncs with the genesis binary until the per-block rule stops it at the first plan height, then switches to the binary of that plan, and so on for each applied upgrade (specification section 14.2). The release manifests name each upgrade; `Query/Policy` read from a synced node at a height below a plan height shows that plan's name and height.

## Keys

- Never copy a validator key to a second live node. A node home copied as a backup holds the validator key: keep it on the same machine, private, and never start a node from it while the original node exists.
- Seat keys and the executor key never go on a validator machine for an upgrade. No step here signs a council message.

## What an operator must not expect

- The chain does not know which binary a node runs; the per-block app-version rule refuses a binary whose app version does not match the committed one, but a wrong binary that declares the right app version and upgrade name is caught only by review.
- Installing the binary early does not move the plan. The plan height is the council's.
