# Evidence

Each goal writes `docs/evidence/goal-NN.md` for the revision that was tested.

Record all of the following:

- tested commit
- runtime (operating system and the toolchain versions that were actually observed)
- commands actually run
- exit codes
- test counts (passed, failed and skipped)
- logs, or the locations of raw artifacts
- blockers
- model used

Do not invent logs, exit codes or test counts. A command that was not run is a blocker, not a pass. `IMPLEMENTED`, `VERIFIED`, `BLOCKED` and `DEFERRED` are distinct. A missing test environment is `BLOCKED`, never a pass.

Record the model that actually ran the goal. The playbook names Codex models (Astra and GPT-6.1 Sol) as a suggested allocation. Do not claim one of those models was used unless it was.

Do not put secrets, private keys, personal data or live credentials in an evidence file.

This companion pack is recorded in [goal-00.md](goal-00.md).
