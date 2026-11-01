# chain/x/authority

x/authority holds validator membership, the genesis submitter roles, the devnet test council, council proposals,
and governed limits. It is the only module that returns validator updates: `InitGenesis` returns the genesis set
and `EndBlock` returns a changeset when a scheduled power or key change is due. `validatorUpdate` in `updates.go`
is the only `abci.ValidatorUpdate` literal.

Goal 06 built the module. Goal 07 moved provider records out of it (to `x/providers`) and connected the council
payloads to `x/providers` and `x/content`.

## Store

| Key | Value |
| --- | --- |
| `0x01` | `svn.authority.v1.GenesisState`, deterministic protobuf: validators, submitters, devnet test seats, proposals, pending validator power, devnet test parameters |
| `0x02 \| address` (20 bytes) | one byte, the genesis `SubmitterRole` of that address (admission index) |
| `0x20` | side document: JSON with sorted rows for state the frozen proto has no field for |

The side document holds the policy version, each proposal's canonical payload and decoded payload, authority's
business ids, executed parameter digests, governed limits, and retired council and service keys. A decoded
force-withdraw payload keeps `content_id`, `content_version`, and the 32-byte reason digest value in `reason`.

The side document no longer has `providers` or `prev_provider`. A document written by the Goal 06 code still
decodes (unknown JSON keys are ignored) and the next save drops those keys. No chain with that layout was
deployed, so there is no migration and `ConsensusVersion` stays 1.

## Messages

`MsgProposeChange`, `MsgApproveChange`, and `MsgExecuteChange` are the committed handlers. Each runs
`enforceCommitted` (gas limit within `[MinGasPerTx, MaxGasPerTx]`, transaction bytes at most `MaxTxBytes`, a fixed
50 000 gas charge, a 20-byte signer) and then the pure engine in `engine.go` on a clone of the stored state. The
check order is `op_propose`, `op_approve`, and `op_execute` of the frozen spec machine
(`docs/evidence/goal-02/spec_machine.py`). Execute tests expiry before `QUEUED`; that is the spec order and a
recorded Goal 06 follow-up.

A rejected message saves nothing. A keep result saves the proposal mark and returns handler success with the code
on an `authority_result` event: `EXPIRED`, and `REJECTED` when the payload is `FORBIDDEN`. An idempotent repeat
writes nothing and emits `authority_result idempotent=true`.

There is no single-signature admit, disable, or force-withdraw message (DEC-009). Provider messages
(`RegisterProvider`, `UpdateProvider`, `RotateProviderKey`, `DrainProvider`) belong to `x/providers`; content
messages belong to `x/content`.

## Council payloads and registry hooks

| Payload | Authority decides | Registry call |
| --- | --- | --- |
| `validator_power` | schedules a pending power change | none |
| `governed_limit` | stores the value (1..protocol ceiling) | none |
| `admit_provider` | `devnet` = DEC-002 is the labelled devnet test parameter with role `DEVNET_TEST_ONLY` on a `shortvideo-dev-` chain id | `ProviderAdmission.AdmitByCouncil(ctx, id, devnet)` |
| `disable_provider` | none | `ProviderAdmission.DisableByCouncil(ctx, id)` |
| `force_withdraw` | `devnet` = the same rule for DEC-003 | `ContentRemoval.ForceWithdrawByCouncil(ctx, id, version, reason, devnet)` |

- The engine stays pure. `State.Registry` is an interface; `Keeper.transition` binds it to the transaction
  context and the keepers set with `Keeper.WithRegistry`. Without a registry the three payloads are `MALFORMED`.
- The registry runs every check before its first write. `FORBIDDEN` marks the proposal `REJECTED` (keep path).
  Any other code (`NOT_FOUND`, `DECISION_BLOCKED`, `CONFLICT`, `WITHDRAWN`, `STALE_VERSION`, ...) fails the message
  and the proposal stays `QUEUED`; it can execute later.
- The hooks write through a cache of the message context. The cache is written back only when the message
  succeeds, after authority saves its state. A keep result or a failure discards the hook's writes and events
  even before the SDK discards a failed message branch.
- A registry entry is a coordination record. Admission is not evidence of an independent operator; the DEC-002
  membership policy stays blocked and `operator_group_id` is a label.

## Business ids

Business ids are one protocol namespace across authority, providers, and content (`business_gate` in the spec
machine). Authority checks its own side document first. When peers are wired (`Keeper.WithPeers`), it then asks
`msgkit.Peers` for every other module in name order: absent proceeds, the same digest is an idempotent success,
another digest is `REPLAY`. `Keeper.BusinessDigest` serves authority's ids to the other modules.

Business ids are not in the v0 genesis schema and are not exported (DR-07-03).

## Read surface

Other modules consume these through their own small interfaces. They read committed state and write nothing.

| Method | Returns |
| --- | --- |
| `Role(ctx, addr)` | the genesis `SubmitterRole` from the admission index; `UNSPECIFIED` for any other address |
| `DevnetRole(ctx, decisionID)` | the role name of a devnet test parameter under the `State.devnet` rule with the chain id of `ctx`; not found on any other chain |
| `GovernedLimit(ctx, key)` | the value an executed `GovernedLimitChange` set; callers apply `msgkit.Effective` |
| `BusinessDigest(ctx, id)` | the business digest authority committed for `id` (implements `msgkit.BusinessSource`) |
| `Admitted(ctx, addr)` | whether `addr` is in the admission index (ante decorator) |

`BusinessDigest` panics if the side document does not decode, so no id is reported free on unreadable state;
only transaction handlers call it, and the SDK turns a handler panic into a failed transaction on every node.
`GovernedLimit` reports not found in that case, because query handlers call it; the caller then uses the
protocol ceiling.

## Wiring

```go
authorityKeeper := authority.NewKeeper(keys[authority.ModuleName])
peers := msgkit.NewPeers()
// providers and content keepers take authorityKeeper for its read surface.
authorityKeeper = authorityKeeper.WithRegistry(providersKeeper, contentKeeper).WithPeers(peers)
peers.Add(authority.ModuleName, authorityKeeper)
```

`WithRegistry` panics on a nil hook. A nil peers set consults no other module.

## Tests

- `engine_test.go`: council flow, delay, expiry keep, forbidden removal keep, halt on a mismatched removal,
  council and service rotation, admission through a fake registry, payload digest vector.
- `registry_test.go`: DEC-002 and DEC-003 flags, decoded force-withdraw arguments, `FORBIDDEN` keeps `REJECTED`,
  other registry codes leave the proposal `QUEUED` with unchanged canonical bytes, nil registry, external
  business-id `REPLAY` on propose, approve, and execute.
- `keeper_test.go` (real IAVL multistore): hook writes committed only on success, discarded on keep and failure;
  side-document round trip of force-withdraw arguments; one business-id namespace through `msgkit.Peers`; read
  surface; older side documents; deterministic sources.
- `fuzz_test.go` `FuzzStateTransitions` and `fuzz_registry_test.go` `FuzzCouncilRegistryPayloads`: bounded
  operation sequences; a failure leaves the canonical bytes unchanged.
