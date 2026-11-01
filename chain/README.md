# chain

`svnd` is the application binary. It uses Cosmos SDK v0.55.0 and CometBFT v0.40.0.

The binary wires auth accounts, signatures, sequences, transaction size, and gas limits. It registers the six module slots from the blueprint. `x/authority` is the only module that returns validator updates. It runs the council proposal flow of Goal 06: four devnet seats propose and approve, the authority submitter executes, and the validator set changes in EndBlock at the activation point. See `x/authority/README.md`. Provider, content, lease, and evidence messages are registered and rejected with `ERROR_CODE_FORBIDDEN`. Those machines are Goals 07-09. Policy has no message service: a governed limit changes only through a council governed-limit payload that `MsgExecuteChange` executes.

Staking, bank, mint, distribution, slashing, governance-by-token, EVM, and IBC are not imported. There is no coin denomination and no fee amount.

`make devnet-up` starts four processes. `make devnet-down` stops them without deleting genesis. `make devnet-resume` starts the same homes again.
