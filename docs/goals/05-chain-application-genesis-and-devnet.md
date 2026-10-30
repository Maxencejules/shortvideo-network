GOAL 05 / 30



Chain application, genesis and devnet
  GPT-6 Astra                                                                       EFFORT: HIGH

Prerequisites: Goals 02-04; compatible framework/toolchain confirmed.
Blueprint reference: pp. 8, 11-12, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 05. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Create the real Go application-chain foundation and demonstrate four validator processes committing the
same chain state.
IMPLEMENTATION REQUIREMENTS

1. Inspect the official scaffold for the pinned SDK and CometBFT versions. Create the chain binary, application
    wiring, address configuration, service routes and module registration. Remove unused
    staking/EVM/bridge/token functionality deliberately; do not hide a default economic model behind a
    renamed chain.
2. Implement a validated genesis schema for chain ID, timestamp, protocol version, initial authorities, public
    validator keys, equal voting powers, admitted application accounts and resource bounds. Keep the initial
    validator set static until Goal 06 adds its complete update controls. State that this is scaffolding, not all
    project modules.
3. Use existing signature/account-sequence mechanisms with size and gas limits. Reject unknown or
    unauthorized application messages. Test initialization ordering and genesis import/export. Prevent two
    independent module paths from setting validator power.
4. Add a devnet launcher for four distinct data directories, peer identities and consensus keys. Use a unique
    disposable chain identity per independent test run, distribute one validated genesis, and keep keys out of
    version control and shared environment templates.
5. Provide status, logs and safe stop/resume commands. Include ordinary process-based execution where
    permitted and a full Linux/container profile. Do not reset genesis or manufacture block history to recover
    from an error.
ACCEPTANCE CHECKS

From a clean workspace, start all four validators and compare committed application hashes at the same
height. Restart and confirm continued progression with preserved state.
Stop one validator, then two, and observe continued commits versus stalled new commits under the declared
test assumptions. Validate recovery and reject malformed genesis, duplicate keys and inconsistent chain IDs.
If the current runtime cannot start the real network, run available unit tests and leave devnet verification
blocked for the named runner.
REQUIRED HANDOFF

Record binary/tool versions, public genesis digest, test topology and logs. Leave application-module
completion for Goals 06-10; never present a mock ledger as this milestone.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                11 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



