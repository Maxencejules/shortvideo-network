# Goal 05 evidence

Status: VERIFIED

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. The six application modules are scaffolding. Goals 06-10 fill them in.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH.
- Model that ran: `grok-4.7-high-fast`. Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-b90997c1-5660-5130-ab13-3e1662716345`.
- GPT-6 Astra was not used. A separate Astra HIGH control was not selected. `high-fast` is the suffix of the reported model name.
- The goal 05 prompt says Max waived the model requirement. The mismatch is recorded and is not a blocker.

## Tested revision

Round 3 corrects the citations. It does not change chain code, `prove_test.go`, or the prove scripts. Head before this documentation commit is `406bb2e25a3d1c30d81ad033530efc4f9c945cbc`, tree `10bdc0def6e1c974501d1c1242eb924bcfaa31e9`. CI on that head is Actions run `41249938901`, success.

The proofs that support the code at that head are not one tree:

| Proof | Log | Tree the command recorded | Files with an empty diff to `406bb2e` |
| --- | --- | --- | --- |
| Four processes | [devnet-process-r2d.txt](logs/goal-05/devnet-process-r2d.txt) | `1c48fc02d3a4004c98cdc53f1dd8251da1b3753b` | `chain/devnet/prove_test.go`, `chain/devnet/status.go` |
| Four containers | [devnet-containers-r2b.txt](logs/goal-05/devnet-containers-r2b.txt) | `f9ad8430a3372d1d3de7608cc201587dc63adf4b` | `scripts/devnet/prove-containers.sh`. The image's `svnd` was built from `chain/` before `HighWater`. The reviewer rebuilt svnd from f9ad843 and from 406bb2e (go1.26.10, -trimpath -buildvcs=false -ldflags=-buildid=): identical symbol tables and identical .text, .rodata, .data and .noptrdata; the files differ by 8 bytes in pc-line and DWARF tables only. |
| CLI genesis rejection | [genesis-cli-r2.txt](logs/goal-05/genesis-cli-r2.txt) | `9bdf99eba90ecb5f87c8e2d0eb4f4ee6fe4c52a5` | `scripts/devnet/prove-genesis.sh`, `chain/cmd/svnd/main.go` (`validateGenesisFile` is in that file) |
| Goal 04 compose | [env-compose-r2.txt](logs/goal-05/env-compose-r2.txt) | `9bdf99eba90ecb5f87c8e2d0eb4f4ee6fe4c52a5` | `scripts/env/compose.sh` |

`git diff` from each proof tree to `406bb2e` is empty for the files named in its row of this table. It is not empty for every proof file on every tree. `f9ad843` differs from `406bb2e` in `chain/devnet/prove_test.go` and `chain/devnet/status.go`. `9bdf99e` differs in those two files and in `scripts/devnet/prove-containers.sh`. Only `1c48fc0` is empty for `prove_test.go` and `status.go`.

Round 1 commit `5bd4bf99fadf919b3bdc51865c7a16d4e265d241`, tree `89aa295fe4bbe58778ff45e8f2fe033ff800b649`, is not the tested revision for this head. The round 1 history is in the superseded section below.

## Base and prerequisites

Base: `aa205ea9aa6ea49f4e9d33112b1629a42cee330c`, subject `goal 04: cloud dev environment (#7)` on `origin/main`. Branch `goal-05` was cut from that commit.

| Prerequisite | Status read before this work | Evidence |
| --- | --- | --- |
| Goal 02 | IMPLEMENTED | [goal-02.md](goal-02.md), PR 5. Protocol v0 is the frozen candidate. DEC-001..012 are approved. |
| Goal 03 | IMPLEMENTED | [goal-03.md](goal-03.md), PR 6. |
| Goal 04 | BLOCKED | [goal-04.md](goal-04.md), PR 7. Compose had not run. This goal started a local dockerd. It did not use port 2375. |

## What this goal is

`svnd` is a Cosmos SDK v0.55.0 application on CometBFT v0.40.0. Auth accounts, signatures, sequences, transaction size, and gas limits are the SDK mechanisms. Fee amounts are rejected. There is no coin denomination.

`x/authority` is the only constructor of `abci.ValidatorUpdate`. InitChain returns the equal-power set once. EndBlock returns no updates. The set stays static until Goal 06. Staking, bank, mint, distribution, slashing, token governance, EVM, and IBC are not imported.

Provider, content, lease, evidence, and policy messages are registered and rejected. That is scaffolding, not those modules.

## Carried items

| Item | Status | What happened |
| --- | --- | --- |
| `handler_down_errors` dynamic scan | VERIFIED | Goal 03 already used an AST. This goal follows `TX_HANDLERS.get`, `getattr`, `itemgetter`, `join`, `state.values()`, and `**state`. `docs/evidence/logs` are not required for that check. `pytest tests/unit/test_down_scan.py` is in the unit run. |
| Harness operations out of the tx router | VERIFIED | `TX_HANDLERS` is the transaction router. `HARNESS_OPS` holds `SetDown`, `CommitBlock`, `Submit`, and the other test drivers. `dispatch_tx` cannot call them. |
| DEC-011 liveness is not consensus | VERIFIED | The chain has no `down` field. The scan still rejects a transaction handler that reads it. |
| DEC-009 council only | VERIFIED | There is no single-signer admit, disable, or force-withdraw message. `MsgCreateValidator` is not registered. |
| Goal 04 compose not executed | VERIFIED in round 2 | Round 1 ran only `deployments/chain/compose.yaml`. Round 2 ran `bash scripts/env/compose.sh`. Postgres and RabbitMQ both became healthy, then the script was stopped. Log: [env-compose-r2.txt](logs/goal-05/env-compose-r2.txt). |

## Versions

```text
svnd (devel)
go go1.26.10
github.com/cometbft/cometbft v0.40.0
github.com/cosmos/cosmos-sdk v0.55.0
```

`/tmp/svnd version` printed that. Docker client and engine `29.1.3` (`29.1.3-0ubuntu3~24.04.2`). Compose `2.40.3+ds1-0ubuntu1~24.04.1`. protoc `libprotoc 36.2`. The CometBFT module constant `TMCoreSemVer` in the v0.40.0 source is `0.39.0`. The pin used is the module version v0.40.0.

## Public genesis and topology

Round 1 container snapshot, superseded. It is not the container proof for this head. Home `.local/devnet/containers` (gitignored):

- Chain id: `shortvideo-dev-8e9346b8cf00522c`
- Genesis SHA-256: `42fe3ddc7e407aa3006d84b834e56016ea1986d5107f4b6142bd6410fba3b8f4`
- Four validators, power 1 each, distinct consensus public keys and peer ids
- Public copy: [container-topology.json](logs/goal-05/container-topology.json)

At height 192 that run reported app hash `9242B71A92C248E317788251EFCF52FDF338117984DCB32671EB59B19A05F93A`. The log [container-status.txt](logs/goal-05/container-status.txt) is a single snapshot. It has no command, timestamp, tree, or exit code. The current container proof is [devnet-containers-r2b.txt](logs/goal-05/devnet-containers-r2b.txt).

## Acceptance

| Check | Status | Command |
| --- | --- | --- |
| Genesis validation, init order, import/export, chain-id mismatch | VERIFIED | `go test ./chain/app` |
| Unknown message, outsider, fee amount, gas 0, no end-block power change | VERIFIED | same package, `TestMessagesAreRejected` |
| One `abci.ValidatorUpdate` constructor, no economic imports | VERIFIED | `TestValidatorUpdatesHaveOneConstructor`, `TestChainSourcesDoNotImportEconomicModules` |
| Four processes, same hash, restart, one stopped, two stopped, resume | VERIFIED | [devnet-process-r2d.txt](logs/goal-05/devnet-process-r2d.txt) on tree `1c48fc02d3a4004c98cdc53f1dd8251da1b3753b`. Exit 0. Stall held at 12. Recovery reached 14. App hash `7AFE302F17F9E46DA33684CE3B5C21477B1FA349E6C7F41F4E1E3419162FE7E5`. No transaction was submitted, so the hash is the genesis application state. [devnet-process-r2.txt](logs/goal-05/devnet-process-r2.txt) and [devnet-process-r2c.txt](logs/goal-05/devnet-process-r2c.txt) are earlier trees. They are not this row. |
| Malformed comet genesis chain id | VERIFIED | [genesis-cli-r2.txt](logs/goal-05/genesis-cli-r2.txt) on tree `9bdf99eba90ecb5f87c8e2d0eb4f4ee6fe4c52a5`. `svnd genesis validate --file` exit 1, `inconsistent chain id`. [malformed-genesis.txt](logs/goal-05/malformed-genesis.txt) is round 1 and is superseded. |
| Four containers, same app hash, restart, one down, two down, recovery | VERIFIED | [devnet-containers-r2b.txt](logs/goal-05/devnet-containers-r2b.txt) on tree `f9ad8430a3372d1d3de7608cc201587dc63adf4b`. Exit 0. Host network. Start 3, restart 6, one-down through 12, stall 14, recovery 16. App hash `D2516ED06E4A3DE1408C5A9A64FB8710BC6EEF49A485750A98BD451CFA8D131F`. The hash is the genesis application state. [devnet-containers-r2.txt](logs/goal-05/devnet-containers-r2.txt) is an earlier sampler. It is not this row. |
| Docker bridge between containers | BLOCKED | The first compose file used a bridge. Dials to `172.18.0.0/16:26656` returned `i/o timeout`. Host networking is what ran. It needs Max's approval at merge. This round does not claim the bridge works. |
| CLI genesis rejection | VERIFIED | [genesis-cli-r2.txt](logs/goal-05/genesis-cli-r2.txt) on tree `9bdf99eba90ecb5f87c8e2d0eb4f4ee6fe4c52a5`. `svnd genesis validate --file` accepts one genesis and rejects a duplicate key, a chain-id mismatch, malformed JSON, and private-key material. The CLI code is unchanged since that tree. |
| Protocol checker | VERIFIED | `python3 docs/evidence/goal-02/check_protocol.py` with protoc 36.2 on `PATH`. `transition_cases 97`, `transition_failures 0`, `errors 0`, `result PASS`. |
| Requirements checker | VERIFIED | `python docs/evidence/goal-01/check_requirements.py` exit 0, `result PASS`. |

Round 1, superseded: `go test ./chain/...` exited 1 because containers still held the loopback ports. That log is [go-test.txt](logs/goal-05/go-test.txt). The rerun after `docker compose stop` is [go-test-rerun.txt](logs/goal-05/go-test-rerun.txt). Neither file is the process proof for this head. That proof is [devnet-process-r2d.txt](logs/goal-05/devnet-process-r2d.txt). Genesis was not deleted to recover.

`docker compose stop` stopped the containers and left the homes.

## Commands

Round 1, superseded. These commands are not the proof for this head.

```text
COMMAND cd /workspace && go test -count=1 -timeout 180s ./chain/devnet/ -run 'TestInit|TestFour' -v
EXIT:0
--- PASS: TestInitRefusesToReplaceGenesis (0.03s)
--- PASS: TestFourValidatorsCommitRestartAndQuorum (12.21s)
ok  github.com/Maxencejules/shortvideo-network/chain/devnet  12.271s
```

That run has no `date -Is` line and no tree. The current process proof is [devnet-process-r2d.txt](logs/goal-05/devnet-process-r2d.txt).

Round 1 container start, superseded:

```text
COMMAND sg docker -c 'bash scripts/devnet/compose-up.sh'
```

That image build used `svn-devnet:goal05`. `docker version` reported engine 29.1.3, the package installed in this VM, not a daemon on port 2375. The current container proof, including `DOCKER_HOST=<unset>` and `unix:///var/run/docker.sock`, is [devnet-containers-r2b.txt](logs/goal-05/devnet-containers-r2b.txt).

Round 1 CI, superseded as the tested revision. Unit Python, mypy, secret scan, and `gofmt` exited 0 before the first pull request. Ruff on `services workers/media packages/protocol tests scripts` exited 0. Run `41244886019` was canceled because a newer run was queued. Run `41244914773` failed the python job on `chain/app/app_test.go: private key name` and passed go, node, and contract. Log: [ci-41244914773-python.txt](logs/goal-05/ci-41244914773-python.txt). The test then built that field name at runtime. `go test -count=1 -timeout 180s ./chain/...` exited 0. Log: [go-test-rerun.txt](logs/goal-05/go-test-rerun.txt). Commit `08faed8c5b2be99e6d01be28baaae4d9164cbd51`, tree `1c03d6dbd0d5105bc63ac1e0709b134a81ac61d6`. Run `41245418439` then failed `scripts/write_sbom.py --check` with `sbom drift`. The regenerated `docs/sbom.cdx.json` has 1320 components. `scripts/run_tests.py --only python` exited 0, 120 passed. Those runs are not the devnet proof for this head.

## Residual risks

- Application messages do not implement the protocol machine. They fail closed.
- The validator set cannot be changed until Goal 06.
- The container profile uses host networking on this VM because the bridge did not pass peer packets. Host networking needs Max's approval at merge. The bridge stays BLOCKED.
- One workstation is one operator. Four processes are not four independent operators.
- This is not an audit and not a production deployment.

## Round 2

Review [5725171267](https://github.com/Maxencejules/shortvideo-network/pull/8#pullrequestreview-5725171267) on `e699f383c83647e98c2ff86ebeb6adcf3f898d33` returned FIX. The round 1 VERIFIED status did not hold while those items stood.

Round 2 proofs did not all run on one tree. Each log below names the tree `git rev-parse HEAD^{tree}` printed at the start of that command.

| Item | Status | What changed |
| --- | --- | --- |
| Authority consensus bytes | VERIFIED | `chain/x/authority/keeper.go` stores `proto.MarshalOptions{Deterministic: true}`. Export stays protojson. `TestAuthorityConsensusBytesArePinned` pins the stored hex for `sampleDocument`. |
| Liveness thresholds | VERIFIED | Current process proof is [devnet-process-r2d.txt](logs/goal-05/devnet-process-r2d.txt), tree `1c48fc02d3a4004c98cdc53f1dd8251da1b3753b`. The test records a high-water height before each stop and requires `WaitAbove` of the stall ceiling after recovery. |
| Raw phase logs | VERIFIED | Current logs are [devnet-process-r2d.txt](logs/goal-05/devnet-process-r2d.txt) and [devnet-containers-r2b.txt](logs/goal-05/devnet-containers-r2b.txt). CLI log [genesis-cli-r2.txt](logs/goal-05/genesis-cli-r2.txt) is still current for the validator file check. Earlier logs [devnet-process-r2.txt](logs/goal-05/devnet-process-r2.txt), [devnet-process-r2c.txt](logs/goal-05/devnet-process-r2c.txt), [devnet-containers-r2.txt](logs/goal-05/devnet-containers-r2.txt), and the failed [devnet-containers-r2-skew.txt](logs/goal-05/devnet-containers-r2-skew.txt) are kept. They are not the proof for the code at head `406bb2e`. |
| Goal 04 compose | VERIFIED | `scripts/env/compose.sh` ran. Both services were healthy. Log [env-compose-r2.txt](logs/goal-05/env-compose-r2.txt). Passwords are not in the log. |
| Empty-block hash | VERIFIED as genesis state | No application transaction was submitted. Every registered application message still fails closed. The equal hashes are the genesis application state, not a hosting transaction. Blueprint step 7's hosting transaction stays with Goals 07-08. |
| Image digests | IMPLEMENTED | `deployments/chain/Dockerfile` pins the linux/amd64 manifests `golang:1.26.10-bookworm@sha256:2bba1f6b063d0e09c5297fbd0a7b9664c4931afc4476a779d7c9a3062704f949` and `debian:bookworm-slim@sha256:a3bba7dc7dbcc1f37c2856c51fc9a7fac96608f50cb87b1b4e4d8b8c85ae988e`. |
| Linked binary | VERIFIED | `auth.NewAppModule` is not used. `TestLinkedBinaryOmitsEconomicModules` builds `svnd` and rejects staking, bank, mint, distribution, slashing, gov, and authz symbols. |
| Docker context | IMPLEMENTED | `scripts/devnet/compose-up.sh` and the container proof refuse a set `DOCKER_HOST`, a non-unix endpoint, and port 2375. The container log records `DOCKER_HOST=<unset>` and `unix:///var/run/docker.sock`. |
| Docker bridge | BLOCKED | Unchanged. Host networking needs Max's approval at merge. |

Process log [devnet-process-r2.txt](logs/goal-05/devnet-process-r2.txt) on tree `9bdf99eba90ecb5f87c8e2d0eb4f4ee6fe4c52a5` is superseded. `prove_test.go` changed after it. Heights in that log: stop at 3, restart at 4, one-down through 11, two-down held at 11, recovery at 15. App hash `8B4D86E7FA70D5808129193EA913B4DF9544CE83F4776AB0ADD9B6EC98B99B6E`.

Actions run `41249097464` passed the devnet job and failed the go job: `Agreed` rejected height 4 against 5 when the app hash matched. The stop threshold is now the greatest of those heights. [devnet-process-r2c.txt](logs/goal-05/devnet-process-r2c.txt) on tree `5131629633ec1c5b86dd7243754020e5829bdd33` exited 0. Restart reached 4 from a stop at 3, and recovery reached 15. App hash `5CBE3D352BF07AAA9CB1D28F68D6FBC9F45159290308B0A520AE15121C3CAE8F`.

Actions run `41249637172` passed devnet again and failed the go job during the two-validator stall: the survivors were one block apart. A stall now rejects a later increase on each node, and recovery must pass the highest height in that window. [devnet-process-r2d.txt](logs/goal-05/devnet-process-r2d.txt) on tree `1c48fc02d3a4004c98cdc53f1dd8251da1b3753b` exited 0. The stall held at 12 and recovery reached 14. App hash `7AFE302F17F9E46DA33684CE3B5C21477B1FA349E6C7F41F4E1E3419162FE7E5`.

Container log [devnet-containers-r2.txt](logs/goal-05/devnet-containers-r2.txt) on tree `9bdf99eba90ecb5f87c8e2d0eb4f4ee6fe4c52a5` is superseded. `prove-containers.sh` changed after it. Heights in that log: start 4, restart 5, one-down through 11, stall 12, recovery 13. App hash `9A8DEE600967031F7D61817CBDD8A3F46BA1805C8D308E67F48111BE844D8C68`.

CI on this round adds `go vet` and `go test -race`, and a `devnet` job that runs `scripts/devnet/prove-containers.sh`. The job timeout stays at 20 minutes. Actions run `41248540896` on `931216d140a68838327494c241078680f579244d` passed python, contract, node, and go, including `go test -race`. The devnet job reached recovery and then failed one snapshot while one validator was a block behind. The sampler now waits until the heights match. The rerun is [devnet-containers-r2b.txt](logs/goal-05/devnet-containers-r2b.txt) on tree `f9ad8430a3372d1d3de7608cc201587dc63adf4b`: start 3, restart 6, one-down through 12, stall 14, recovery 16. App hash `D2516ED06E4A3DE1408C5A9A64FB8710BC6EEF49A485750A98BD451CFA8D131F`. Exit 0.

## Round 3

Review [5725373923](https://github.com/Maxencejules/shortvideo-network/pull/8#pullrequestreview-5725373923) on `406bb2e25a3d1c30d81ad033530efc4f9c945cbc` returned FIX. The code was accepted. This section corrects the evidence. No chain file and no prove script changed, so the proofs were not rerun. `git diff` from each proof tree to `406bb2e` is empty for the files named in its row of the Tested revision table.

### Follow-ups

These are not fixed in this round. They are not VERIFIED claims.

1. Restart also passes at threshold+1: the threshold is recorded before the sequential stops, so a block committed during shutdown can satisfy the restart check. Liveness after restart is shown indirectly by one-down progress.
2. Recovery passes at stall+1. A head CI run reached 13 against a stall of 12. The fix is to wait for two blocks, or one block interval, after the restarted validators are up, then rerun both proofs.
3. `chain/app/authmodule.go` registers the auth query and message servers and sets consensus version 7. It does not register the x/auth migrations from version 5 to 6 and from 6 to 7. A fresh chain starts at 7. Goal 10 has to register those migrations before an upgrade from an older auth version.
4. Done in Goal 06. `scripts/devnet/prove-process.sh`, `prove-containers.sh`, and `prove-genesis.sh` print `DIRTY yes` or `DIRTY no` after `TREE`.
5. [env-compose-r2.txt](logs/goal-05/env-compose-r2.txt) records `COMMAND bash scripts/env/compose.sh` and, later, `COMMAND docker compose -f deployments/compose/docker-compose.yml stop`. The healthy `ps` table has no COMMAND line of its own. The stop command is the line above the container shutdown. A rerun should print COMMAND before `ps`.
6. The Dockerfile digests are linux/amd64 manifests. Another architecture cannot use them as written. That limit is already in the Dockerfile comment.
7. Exact-height checks can still flake when empty blocks leave validators one height apart. Runs `41248540896`, `41249097464`, `41249637172`, and `41252788163` failed that way. Run `41252788163` reported `disagreement [14, 14, 13]` after stop v3: that sampler read the three live heights once and exited. Goal 06 makes that read wait until the heights match, and the restart and recovery proofs wait for two blocks after the restarted validators are up.

## Round 4

Review [5725410146](https://github.com/Maxencejules/shortvideo-network/pull/8#pullrequestreview-5725410146) on `b6398b9c7301716f5043c3a6a2809032ca3ec168` returned FIX. The false sentence in Round 3 is replaced. The empty-diff claim is only the files named in each row of the Tested revision table. This commit does not change chain code or prove scripts.

## Next safe action

Goal 06 can add the authority update controls on top of this static set. Do not treat the devnet keys as council keys. Do not delete a genesis to recover a stuck chain.
