# Evidence envelope fixtures

Synthetic envelopes for `docs/protocol/schemas/evidence-envelope.schema.json` (`svn-evidence-envelope-v1`).
`tests/unit/test_evidence_envelopes.py` validates them with `svn_protocol.schemas.validator_for`.

The files in `valid/` and `invalid/` are hand-made. No producer wrote them and nothing signed them. No key exists for
any of their `signer_key_id` values. The envelope schema has no signature field.

`golden/` is different: the Go audit producer wrote it, with a TEST-ONLY key derived from a public label, and the
same test checks the signature and the evidence ids in Python. `golden/README.md` describes it. It is listed by
`golden/index.json`, not by `cases.json`.

- `valid/`: one envelope per kind (`PROVIDER_RECEIPT`, `AUDITOR_ATTESTATION`), and two envelopes at the schema's
  lower and upper bounds. The two per-kind envelopes carry `observation_sha256` = SHA-256 of `svn-manifest-v1`, the
  manifest digest of the goal-02 spec machine fixtures (`docs/evidence/goal-02/spec_machine.py:30`). No manifest with
  that digest exists.
- `invalid/`: each file is a `valid/` envelope with stated changes. The file name starts with its base: `receipt-`
  or `audit-`.
- `cases.json`: the index. Each invalid entry gives `base`, `group`, `requirements`, `reason`, and `errors`, the
  exact schema errors expected: a JSON Pointer `path`, the schema `keyword`, and, for `additionalProperties` and
  `required` at the root, the property `names`.

The test checks that:

- every valid file passes, and both kinds have one;
- every invalid file fails with exactly the stated errors, and differs from its base only at the keys those errors
  name;
- every JSON file here outside `golden/` is indexed once;
- each label group has a receipt-based and an audit-based fixture;
- each Merkle fixture carries `merkle_root` and `inclusion_proof` fields whose leaf is its `observation_sha256`, and
  is rejected for those fields and, where present, for `kind` or `claim_limit` `UNIQUE_REPLICA_PROOF`. No test
  computes or checks the inclusion;
- the `EvidenceKind` names in `docs/protocol/proto/svn/evidence/v1/state.proto` match the schema's kinds, and the
  zero value, the proto numbers, and the reserved numbers and names are rejected;
- each envelope property has a negative fixture;
- each range or length fixture is one step past a value that a valid fixture uses.

The test loader refuses duplicate keys, fractions, exponents, NaN, and Infinity. The standard loader keeps the last
duplicate key, and the schema's `integer` type accepts `1.0`.

| Group | Requirements | What the fixture claims or carries |
| --- | --- | --- |
| `storage-proof-label` | REQ-EVID-001 | `kind` or `claim_limit` `STORAGE_PROOF`, or a `storage_proof` field |
| `unique-replica-label` | REQ-EVID-001 | `kind` or `claim_limit` `UNIQUE_REPLICA_PROOF` |
| `independence-label` | REQ-EVID-001 | an `independent` field |
| `legality-label` | REQ-EVID-001 | a `lawful` field |
| `future-quality-label` | REQ-EVID-001 | `kind` `FUTURE_DELIVERY`, or a `quality_guarantee` field |
| `merkle-inclusion-as-unique-replica` | REQ-EVID-001, REQ-EVID-007 | `merkle_root` and `inclusion_proof`, alone or with `kind` or `claim_limit` `UNIQUE_REPLICA_PROOF` |
| `attestation-label-removed` | REQ-EVID-001 | `attestation` false, a string, or missing; `claim_limit` missing |
| `wrong-kind` | REQ-EVID-001 | proto enum names and numbers, the zero value, lower case, empty, null, missing |
| `reserved-kind` | REQ-EVID-001 | the reserved numbers 3, 4, 5 and the reserved proto names |
| `unknown-field` | none | `signature`, `batch_id`, `result`, `reject`, `host_observed_unix` |
| `bounds` | none | `const`, `pattern`, length, type, and range violations |

The schema checks shape only. It accepts a Merkle root in `observation_sha256`, because both are 64 lower-case hex
characters. It does not pattern-check `lease_id`, `content_id`, or `signer_key_id`.
