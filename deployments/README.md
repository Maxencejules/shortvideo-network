# deployments

Goal 04 adds the cloud profile under `scripts/env/` and the compose profile in `deployments/compose/`. Checksums are in `deployments/pins/`. `deployments/profiles/` is the Goal 27 reference. `deployments/chain/Dockerfile` and `deployments/images/worker.Dockerfile` are local images. This repository does not publish an image. The compose file pins upstream image digests. `deployments/devnet/` is the devnet home layout.
