# deployments/devnet

`scripts/devnet/up.sh` starts four validator processes. `scripts/devnet/down.sh` stops them and keeps the data. `scripts/devnet/resume.sh` starts them again. `scripts/devnet/status.sh` prints the committed height and application hash. `scripts/devnet/logs.sh` shows the logs.

Homes live under `.local/devnet/`, which is gitignored. Keys are generated when the home is created and are not copied from another home.

`deployments/chain/compose.yaml` is the container profile. `scripts/devnet/compose-up.sh` builds a local image and refuses a Docker host on port 2375. It does not delete a home after an error.
