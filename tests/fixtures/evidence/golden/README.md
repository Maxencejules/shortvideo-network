# Golden producer envelopes

These files are written by the Go audit producer, not by hand:
`go test ./agents/auditor/batch -run TestGoldenEnvelopeFixtures -update` (run inside `agents/auditor`). Without
`-update` that test fails if any file here differs from the producer's output or if a file is added or missing.

- `index.json`: the signed batch: TEST-ONLY key label, public key, signer address, batch id, epoch, the
  SVN-JCS-1 batch payload (`canonical`), the ed25519 signature over
  `UTF-8("SVN-EVIDENCE-ATTESTATION-1") || 0x00 || canonical`, and one entry per item.
- `envelopes/<evidence_id>.json`: each item's `svn-evidence-envelope-v1` envelope, byte-identical to the
  producer's `envelopes/<evidence_id>.json` and to the envelope inside `canonical`.
- `reports/<sha256>.json`: the synthetic audit reports (`svn-audit-report-proposed-v0`) the items attest. The
  evidence id is the signer's 16-hex-character address prefix, `.`, and the first 47 hex characters of the
  report's SHA-256. The FAIL item's `observation_sha256` is the report's SHA-256; the PASS item's is the lease
  manifest digest the report expected.

The key is derived from a public label (`sha256("svn-evidence-test-seed:" + key_label)` is the ed25519 seed); no
private key is stored. The leases, reports, and endpoints are synthetic; nothing was fetched. The files are
attestations, not storage proofs.

`tests/unit/test_evidence_envelopes.py` checks them in Python: every envelope passes the frozen schema and is
canonical SVN-JCS-1, the envelopes are the payload's envelopes in order, the signature verifies with the public
key, the signer is the first 20 bytes of SHA-256 of the public key, every evidence id is derived from its report,
and each item's digest follows its result. The directory is not listed in `cases.json`; `index.json` lists every
file here.
