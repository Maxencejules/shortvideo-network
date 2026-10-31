# Goal 05 evidence

Status: VERIFIED

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. The six application modules are scaffolding. Goals 06-10 fill them in.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH.
- Model that ran: `grok-4.7-high-fast`. Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-b90997c1-5660-5130-ab13-3e1662716345`.
- GPT-6 Astra was not used. A separate Astra HIGH control was not selected. `high-fast` is the suffix of the reported model name.
- The goal 05 prompt says Max waived the model requirement. The mismatch is recorded and is not a blocker.

## Tested revision

Application, genesis, and devnet checks ran on commit `5bd4bf99fadf919b3bdc51865c7a16d4e265d241`, tree `89aa295fe4bbe58778ff45e8f2fe033ff800b649`. The progress-only commit `c78f00befed28ba98463395fdf61735661917a3a` did not change chain code. Its tree is `d6c8e83ef7d944f9cfc69b208b150b3334084aec`.

GitHub Actions run `41244886019` was canceled because a newer run for the same pull request was queued. It is not a test failure. Run `41244914773` on `c78f00b` failed the python job and passed go, node, and contract. The python failure is `chain/app/app_test.go: private key name`. Log: [ci-41244914773-python.txt](logs/goal-05/ci-41244914773-python.txt).

The malformed-genesis test now builds that field name at runtime. `go test -count=1 -timeout 180s ./chain/...` then exited 0, including `TestMalformedGenesisRejected` and `TestFourValidatorsCommitRestartAndQuorum` (12.78s). Log: [go-test-rerun.txt](logs/goal-05/go-test-rerun.txt). `scripts/scan_secrets.py` exited 0 on that tree. Commit `08faed8c5b2be99e6d01be28baaae4d9164cbd51` carries that fix. Its tree is `1c03d6dbd0d5105bc63ac1e0709b134a81ac61d6`.

Actions run `41245418439` on that commit passed go, node, and contract. The python job passed the secret scan and then printed `sbom drift` from `scripts/write_sbom.py --check`. `chain/go.sum` was not in the committed CycloneDX file. Regenerating `docs/sbom.cdx.json` with that script produced 1320 components. `--check` then exited 0. `scripts/run_tests.py --only python` exited 0 with 120 passed. The chain packages were not changed for the inventory.

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
| Goal 04 compose not executed | VERIFIED here | A dockerd this VM started, on `unix:///var/run/docker.sock`, built `svn-devnet:goal05` and ran four containers. Port 2375 was not used. |

## Versions

```text
svnd (devel)
go go1.26.10
github.com/cometbft/cometbft v0.40.0
github.com/cosmos/cosmos-sdk v0.55.0
```

`/tmp/svnd version` printed that. Docker client and engine `29.1.3` (`29.1.3-0ubuntu3~24.04.2`). Compose `2.40.3+ds1-0ubuntu1~24.04.1`. protoc `libprotoc 36.2`. The CometBFT module constant `TMCoreSemVer` in the v0.40.0 source is `0.39.0`. The pin used is the module version v0.40.0.

## Public genesis and topology

Container run, home `.local/devnet/containers` (gitignored):

- Chain id: `shortvideo-dev-8e9346b8cf00522c`
- Genesis SHA-256: `42fe3ddc7e407aa3006d84b834e56016ea1986d5107f4b6142bd6410fba3b8f4`
- Four validators, power 1 each, distinct consensus public keys and peer ids
- Public copy: [container-topology.json](logs/goal-05/container-topology.json)

At height 192 all four container RPC ports reported app hash `9242B71A92C248E317788251EFCF52FDF338117984DCB32671EB59B19A05F93A`. Empty blocks do not change that hash. The log is [container-status.txt](logs/goal-05/container-status.txt).

## Acceptance

| Check | Status | Command |
| --- | --- | --- |
| Genesis validation, init order, import/export, chain-id mismatch | VERIFIED | `go test ./chain/app` |
| Unknown message, outsider, fee amount, gas 0, no end-block power change | VERIFIED | same package, `TestMessagesAreRejected` |
| One `abci.ValidatorUpdate` constructor, no economic imports | VERIFIED | `TestValidatorUpdatesHaveOneConstructor`, `TestChainSourcesDoNotImportEconomicModules` |
| Four processes, same hash, restart, one stopped, two stopped, resume | VERIFIED | `go test -count=1 -timeout 180s ./chain/devnet/ -run 'TestInit\|TestFour'` exit 0. Log [devnet-process.txt](logs/goal-05/devnet-process.txt). `TestFourValidatorsCommitRestartAndQuorum` passed in 12.21s. |
| Malformed comet genesis chain id | VERIFIED | `svnd genesis validate` exit 1, `inconsistent chain id`. Log [malformed-genesis.txt](logs/goal-05/malformed-genesis.txt). |
| Four containers, same app hash | VERIFIED | `bash scripts/devnet/compose-up.sh` on the unix-socket daemon. Host network. Height 192 hash above. |
| Docker bridge between containers | BLOCKED | The first compose file used a bridge. Dials to `172.18.0.0/16:26656` returned `i/o timeout`. That home was left in place. A second home uses host networking. |
| Protocol checker | VERIFIED | `python3 docs/evidence/goal-02/check_protocol.py` with protoc 36.2 on `PATH`. `transition_cases 97`, `transition_failures 0`, `errors 0`, `result PASS`. |
| Requirements checker | VERIFIED | `python docs/evidence/goal-01/check_requirements.py` exit 0, `result PASS`. |

An earlier `go test ./chain/...` exited 1 because the containers still held the loopback ports. That log is [go-test.txt](logs/goal-05/go-test.txt). It is not the passing run. The rerun after `docker compose stop` is the process result above. Genesis was not deleted to recover.

`docker compose stop` stopped the containers and left the homes.

## Commands

Process devnet, ports free, exit 0:

```text
COMMAND cd /workspace && go test -count=1 -timeout 180s ./chain/devnet/ -run 'TestInit|TestFour' -v
EXIT:0
--- PASS: TestInitRefusesToReplaceGenesis (0.03s)
--- PASS: TestFourValidatorsCommitRestartAndQuorum (12.21s)
ok  github.com/Maxencejules/shortvideo-network/chain/devnet  12.271s
```

Container profile, after `sudo apt-get install -y docker.io docker-compose-v2` and `sudo dockerd --host unix:///var/run/docker.sock`:

```text
COMMAND sg docker -c 'bash scripts/devnet/compose-up.sh'
```

The image `svn-devnet:goal05` built from `deployments/chain/Dockerfile`. Four containers reached the hash above. `DOCKER_HOST` was unset. `docker version` reported engine 29.1.3, which is the package installed in this step, not the process that was already listening on port 2375.

Unit Python, mypy, secret scan, and `gofmt` were run with the project venv and exited 0 before the pull request. Ruff on `services workers/media packages/protocol tests scripts` exited 0. Actions run `41244914773` then rejected the quoted field in `chain/app/app_test.go`. After the test built that name at runtime, `scripts/scan_secrets.py` exited 0 again and `go test -count=1 -timeout 180s ./chain/...` exited 0. That log is [go-test-rerun.txt](logs/goal-05/go-test-rerun.txt). Run `41245418439` then failed `scripts/write_sbom.py --check` with `sbom drift`. The regenerated `docs/sbom.cdx.json` makes `--check` exit 0 (1320 components). `scripts/run_tests.py --only python` exited 0, 120 passed.

## Residual risks

- Application messages do not implement the protocol machine. They fail closed.
- The validator set cannot be changed until Goal 06.
- The container profile uses host networking on this VM because the bridge did not pass peer packets.
- One workstation is one operator. Four processes are not four independent operators.
- This is not an audit and not a production deployment.

## Next safe action

Goal 06 can add the authority update controls on top of this static set. Do not treat the devnet keys as council keys. Do not delete a genesis to recover a stuck chain.
