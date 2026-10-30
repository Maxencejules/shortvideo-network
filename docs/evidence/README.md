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
- effort or reasoning setting actually used
- the playbook's assigned model and effort for that goal

Do not invent logs, exit codes or test counts. A command that was not run is a blocker, not a pass. `IMPLEMENTED`, `VERIFIED`, `BLOCKED` and `DEFERRED` are distinct. A missing test environment is `BLOCKED`, never a pass.

Record the model that actually ran the goal, and the effort or reasoning setting it actually ran with. Unknown is allowed for the effort setting only when the playbook assigns no setting for that goal, as with goal 00. Goals 01-30 have assigned settings. For those goals, record the setting that was actually used. If it was not reported or could not be selected, log the goal as BLOCKED on model allocation. Do not mark it VERIFIED. The playbook names Codex models (Astra and GPT-6.1 Sol) as a suggested allocation. Do not claim one of those models was used unless it was. Extra effort does not certify correctness.

Do not put secrets, private keys, personal data or live credentials in an evidence file.

This companion pack is recorded in [goal-00.md](goal-00.md). The repeatable checks are [goal-00/build_companion_pack.py](goal-00/build_companion_pack.py) and [goal-00/recheck.py](goal-00/recheck.py).

Goal pages under `docs/goals/` stay exact playbook slices. They are not reflowed into Markdown lists: joining those lines would change the bytes and fail the slice check.
