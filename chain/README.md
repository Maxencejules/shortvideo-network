# chain

The chain is a Go application on Cosmos SDK v0.55.0 and CometBFT v0.40.0. Those modules are pinned from `chain/tools`, which carries a `tools` build tag. `chain/app` does not import them.

Goal 05 creates the binary, genesis, and devnet. There is no `main` package here and `make build` does not produce a chain binary.

`chain/x/*` names the six module slots from the blueprint. The keepers are not written.
