# Goal 00 evidence

Docs and scaffolding only. No application code.

## Model used

`grok-4.7-high-fast`

That is the model name on this Cursor cloud agent run (`bc-1596bc25-19f9-57b8-bf0a-2fb414e326d7`). The playbook's Codex models (Astra and GPT-6.1 Sol) were not used.

## Runtime

- Linux 6.12.94+ x86_64
- Python 3.12.3

There is no application test suite in this repository. The checks below are file-split and copy checks.

## Tested commit

`64f288a72d2f529dc6e3b2115cf3607d406eda3d`

That commit adds the companion pack. This evidence file is a following commit and only records that revision. It does not change the goal pages, blueprint, or other pack files.

## Commands

Split and transcription, from `/tmp/build_companion_pack.py`. The script exits non-zero unless there are 30 non-empty goal files, each an exact slice of the playbook from its `GOAL NN / 30` line up to the next goal header (goal 30 through the end of its page), and the blueprint wording matches the extracted text.

```
python3 /tmp/build_companion_pack.py
```

Exit code: 0

```
GOAL SPLIT
files 30
01 01-requirements-scope-and-threat-model.md bytes=3133 sha256=bcc0736a330d9d41bbd396ba0b41ab232ee28ef5b0ec0774a3643e5d595ec1f1
02 02-protocol-schemas-and-invariants.md bytes=3113 sha256=d71f0d283415b0984fbc6744de2353af7b8185c9d08bc0f42a3f07b1d2019ffd
03 03-monorepo-toolchains-and-ci.md bytes=3031 sha256=c395c35957c111d8371a8500025b33e5a6d5b4e478ea42036d8ecae2017e6e12
04 04-codex-cloud-development-environment.md bytes=3175 sha256=90b4fc90b4978980371194f2c5404f5284efff6eb847fba0fb6361dd62acb248
05 05-chain-application-genesis-and-devnet.md bytes=3122 sha256=86d78b6bb3b6286e3260ec6e95329e76d1f381421007bcbccd88c839d0f315c0
06 06-authority-and-validator-lifecycle.md bytes=2912 sha256=fa746f4f8397e8795786e126a8d5625415921f2474ae9a758875f405c144f81e
07 07-provider-and-content-registries.md bytes=2934 sha256=dc45bfdd58cc3473a4e8bbf6932f8e73c75c749ad8ce93ad28dcfa9c38afd591
08 08-lease-lifecycle-and-capacity-accounting.md bytes=3062 sha256=ffe7ab54dcb566fbc8bf3192b079b7ddb8595776f507d8b2200803e2d4edadce
09 09-beta-attestations-and-audit-evidence.md bytes=2977 sha256=8a4c239df18cbbed28db3d10f3180e4b052d2daed5096cdf32b5f661c53b5b2b
10 10-policy-governance-and-chain-upgrades.md bytes=2919 sha256=7f2fe6cc47460050211f795c48c800acb85849709d4056179c4d95a73c229471
11 11-transaction-broadcaster-and-outbox.md bytes=2984 sha256=dc3778d860ffe4944fb7654ecb39fd7fb501944c6958721845aea449a6c8a059
12 12-committed-state-indexer-and-reconciliation.md bytes=2891 sha256=8c35f458a44817757e1cd3c1685da786be4d86b8d217c9b8785346c77a943082
13 13-authenticated-uploads-and-quarantine.md bytes=2999 sha256=61a774103130673d1f8551c5e6fcc35d3635286f59af744691ade7a09d0843c8
14 14-isolated-video-processing-workers.md bytes=2942 sha256=7499ca9be27d94cba1a51880864b2acd6dc9913e082f0e26741a864de1d3ab73
15 15-media-manifests-and-https-playback.md bytes=2914 sha256=f75d25ae6527e21d45cec401a70db05801e5b58d165a4c28650657b762765f30
16 16-storage-provider-agent.md bytes=2985 sha256=eeef4e9c3bce7fa05440ffaf96140f15090e097a75340e6926e9707585b0bfef
17 17-replication-monitoring-and-repair.md bytes=3021 sha256=bcd663c3997f67a635dbb7a2dd2efc0abc9ddd99f4dca14cb2832d61e6b23167
18 18-end-to-end-network-milestone.md bytes=2901 sha256=3ae2c72fa688dd163d76ebea85a7a7ad736db82507a69547f673acbc4a7f3d9c
19 19-accounts-recovery-and-data-portability.md bytes=3024 sha256=838faee9fb1529f1f3f8cccc7394e7aa106fb669fb330ae00f3533c4221af511
20 20-viewer-web-application.md bytes=2839 sha256=e2a30e6cb87179ddd7a83f25fc6e7dc7713d35b9da2380643eb5168ddc7262a7
21 21-creator-publishing-interface.md bytes=2788 sha256=31cb30290c9dc0ce2cde13d57ae75cc4c70591add069e43c80bc5230eba1a42c
22 22-social-features-search-and-feeds.md bytes=2815 sha256=4ba70a3eb17d102ad3b6be7602a528fd9ad04b3426661343e769164551dd0734
23 23-moderation-blocking-and-removal.md bytes=2995 sha256=d4d5a105d9f80c6d2c608c18f20600fc9f7c1cbf5735ab88d432767f9464939e
24 24-operator-dashboard-and-node-onboarding.md bytes=2806 sha256=98689fd08e9ae603ccac23aba866f6c03da2d8f81562be62fdffa90ddc720731
25 25-observability-quotas-and-usage-controls.md bytes=2882 sha256=1cd1aea7c999160f8fbee42ccb2bb9788bb2381fd53ec56c8890c1c170aceb20
26 26-backups-restoration-and-signer-safety.md bytes=2903 sha256=6d8ebf1ba4d3dfdd6e3a797b9f05ca335fb4244406f2a4f05336d5b4a69cf926
27 27-packaging-installation-and-release-preparation.md bytes=2984 sha256=547d159092d4f1e59cb0b4516092e5b68f20b948a557eae07f0d6dec17728db1
28 28-performance-load-and-capacity-evidence.md bytes=2954 sha256=ad8c99603f1ef4facd6ffd5fed567098d7d3e20c25e2ecf8a1a7f227d7033284
29 29-adversarial-review-and-fault-testing.md bytes=3003 sha256=011cf9fbadb66e5f11cd20b17940bd0bb76cf0bdbcebdf83ff87d50878a06db9
30 30-final-acceptance-and-independent-installation.md bytes=3027 sha256=e30ea2e467cc478f4c2e1773dcd1c6b36f550674c269df3c55489ab260255d6d
SLICE_OK joined goals equal playbook span from GOAL 01 through the end of GOAL 30
REQUIRED_SECTIONS_OK prerequisites prompt implementation acceptance handoff
BLUEPRINT_OK token and symbol counts match the extracted text, minus repeated running headers
blueprint.md bytes=73768
source_urls 31
COPIES_OK pdf txt tsv byte-identical
blueprint.pdf sha256=e66210c66ef5755f1b262d89a0d9af6bea9da25a56c5a7012e41425ebed0e6de
prompt-playbook.pdf sha256=a8136b9f632bdd60767bd0ad72a89465da6219d01dac6454fc77a7b845b15213
prompt-playbook.txt sha256=2dd95abd20e5b385e59683932609d3eb27fb291964cb369ebc0e2cfec8f1b5a8
progress.tsv sha256=92ec16ce0ccb7bc8da496eaae44829d2ecb30bc26583d3084e62b7fea8b90db5
```

Independent re-check of the files on disk, after the split. Each goal file had to equal the playbook slice byte for byte.

Exit code: 0

```
goal_file_count 30
playbook_goal_headers 30
OK 01-requirements-scope-and-threat-model.md bytes=3133 sha256=bcc0736a330d9d41bbd396ba0b41ab232ee28ef5b0ec0774a3643e5d595ec1f1
OK 02-protocol-schemas-and-invariants.md bytes=3113 sha256=d71f0d283415b0984fbc6744de2353af7b8185c9d08bc0f42a3f07b1d2019ffd
OK 03-monorepo-toolchains-and-ci.md bytes=3031 sha256=c395c35957c111d8371a8500025b33e5a6d5b4e478ea42036d8ecae2017e6e12
OK 04-codex-cloud-development-environment.md bytes=3175 sha256=90b4fc90b4978980371194f2c5404f5284efff6eb847fba0fb6361dd62acb248
OK 05-chain-application-genesis-and-devnet.md bytes=3122 sha256=86d78b6bb3b6286e3260ec6e95329e76d1f381421007bcbccd88c839d0f315c0
OK 06-authority-and-validator-lifecycle.md bytes=2912 sha256=fa746f4f8397e8795786e126a8d5625415921f2474ae9a758875f405c144f81e
OK 07-provider-and-content-registries.md bytes=2934 sha256=dc45bfdd58cc3473a4e8bbf6932f8e73c75c749ad8ce93ad28dcfa9c38afd591
OK 08-lease-lifecycle-and-capacity-accounting.md bytes=3062 sha256=ffe7ab54dcb566fbc8bf3192b079b7ddb8595776f507d8b2200803e2d4edadce
OK 09-beta-attestations-and-audit-evidence.md bytes=2977 sha256=8a4c239df18cbbed28db3d10f3180e4b052d2daed5096cdf32b5f661c53b5b2b
OK 10-policy-governance-and-chain-upgrades.md bytes=2919 sha256=7f2fe6cc47460050211f795c48c800acb85849709d4056179c4d95a73c229471
OK 11-transaction-broadcaster-and-outbox.md bytes=2984 sha256=dc3778d860ffe4944fb7654ecb39fd7fb501944c6958721845aea449a6c8a059
OK 12-committed-state-indexer-and-reconciliation.md bytes=2891 sha256=8c35f458a44817757e1cd3c1685da786be4d86b8d217c9b8785346c77a943082
OK 13-authenticated-uploads-and-quarantine.md bytes=2999 sha256=61a774103130673d1f8551c5e6fcc35d3635286f59af744691ade7a09d0843c8
OK 14-isolated-video-processing-workers.md bytes=2942 sha256=7499ca9be27d94cba1a51880864b2acd6dc9913e082f0e26741a864de1d3ab73
OK 15-media-manifests-and-https-playback.md bytes=2914 sha256=f75d25ae6527e21d45cec401a70db05801e5b58d165a4c28650657b762765f30
OK 16-storage-provider-agent.md bytes=2985 sha256=eeef4e9c3bce7fa05440ffaf96140f15090e097a75340e6926e9707585b0bfef
OK 17-replication-monitoring-and-repair.md bytes=3021 sha256=bcd663c3997f67a635dbb7a2dd2efc0abc9ddd99f4dca14cb2832d61e6b23167
OK 18-end-to-end-network-milestone.md bytes=2901 sha256=3ae2c72fa688dd163d76ebea85a7a7ad736db82507a69547f673acbc4a7f3d9c
OK 19-accounts-recovery-and-data-portability.md bytes=3024 sha256=838faee9fb1529f1f3f8cccc7394e7aa106fb669fb330ae00f3533c4221af511
OK 20-viewer-web-application.md bytes=2839 sha256=e2a30e6cb87179ddd7a83f25fc6e7dc7713d35b9da2380643eb5168ddc7262a7
OK 21-creator-publishing-interface.md bytes=2788 sha256=31cb30290c9dc0ce2cde13d57ae75cc4c70591add069e43c80bc5230eba1a42c
OK 22-social-features-search-and-feeds.md bytes=2815 sha256=4ba70a3eb17d102ad3b6be7602a528fd9ad04b3426661343e769164551dd0734
OK 23-moderation-blocking-and-removal.md bytes=2995 sha256=d4d5a105d9f80c6d2c608c18f20600fc9f7c1cbf5735ab88d432767f9464939e
OK 24-operator-dashboard-and-node-onboarding.md bytes=2806 sha256=98689fd08e9ae603ccac23aba866f6c03da2d8f81562be62fdffa90ddc720731
OK 25-observability-quotas-and-usage-controls.md bytes=2882 sha256=1cd1aea7c999160f8fbee42ccb2bb9788bb2381fd53ec56c8890c1c170aceb20
OK 26-backups-restoration-and-signer-safety.md bytes=2903 sha256=6d8ebf1ba4d3dfdd6e3a797b9f05ca335fb4244406f2a4f05336d5b4a69cf926
OK 27-packaging-installation-and-release-preparation.md bytes=2984 sha256=547d159092d4f1e59cb0b4516092e5b68f20b948a557eae07f0d6dec17728db1
OK 28-performance-load-and-capacity-evidence.md bytes=2954 sha256=ad8c99603f1ef4facd6ffd5fed567098d7d3e20c25e2ecf8a1a7f227d7033284
OK 29-adversarial-review-and-fault-testing.md bytes=3003 sha256=011cf9fbadb66e5f11cd20b17940bd0bb76cf0bdbcebdf83ff87d50878a06db9
OK 30-final-acceptance-and-independent-installation.md bytes=3027 sha256=e30ea2e467cc478f4c2e1773dcd1c6b36f550674c269df3c55489ab260255d6d
joined_equals_playbook_span True
joined_bytes 89035
verify_exit 0
```

Copy check, same run, exit code 0:

```
blueprint.pdf identical e66210c66ef5755f1b262d89a0d9af6bea9da25a56c5a7012e41425ebed0e6de bytes 218370
prompt-playbook.pdf identical a8136b9f632bdd60767bd0ad72a89465da6219d01dac6454fc77a7b845b15213 bytes 169822
prompt-playbook.txt identical 2dd95abd20e5b385e59683932609d3eb27fb291964cb369ebc0e2cfec8f1b5a8 bytes 114502
progress.tsv identical 92ec16ce0ccb7bc8da496eaae44829d2ecb30bc26583d3084e62b7fea8b90db5 bytes 61
```

`find docs/goals -name '*.md' | wc -l` printed `30`. `find docs/goals -name '*.md' -empty | wc -l` printed `0`.

## Result

30 goal files, none empty, none truncated. Joined text equals the playbook from `GOAL 01 / 30` through the end of goal 30. Each file contains Prerequisites, the copy-ready prompt, implementation requirements, acceptance checks and the required handoff. Goals 01-29 include the playbook footer and the next page's running header, because the slice stops at the next goal header. Goal 30 stops at its own page footer.

Goals 01-30 in `docs/progress.md` are NOT STARTED. No application tests were run. A missing product test suite is not recorded as a pass.

## Blockers

None for this documentation task.
