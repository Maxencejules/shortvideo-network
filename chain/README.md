# chain

`svnd` is the Goal 05 application binary. It uses Cosmos SDK v0.55.0 and CometBFT v0.40.0.

The binary wires auth accounts, signatures, sequences, transaction size, and gas limits. It registers the six module slots from the blueprint. `x/authority` is the only module that returns the initial validator set. That set stays static until Goal 06. Provider, content, lease, evidence, and policy messages are registered and rejected. Those machines are Goals 06-10.

Staking, bank, mint, distribution, slashing, governance-by-token, EVM, and IBC are not imported. There is no coin denomination and no fee amount.

`make devnet-up` starts four processes. `make devnet-down` stops them without deleting genesis. `make devnet-resume` starts the same homes again.
