// Package auditor is the root of the off-chain audit producer for
// permitted test endpoints (goal 09). An audit result is an attestation by
// an auditor, not a storage proof and not a unique-replica proof.
//
// This module holds libraries with tests; it has no command yet:
//
//   - config: strict operator configuration with no bypass field
//   - policy: destination policy (PROPOSED, DR-09-10): URL rules, deny
//     unless globally reachable against the pinned IANA special-purpose
//     table, exact (provider_id, host, ip, port) allowlist entries
//   - fetch: pinned-address HTTPS requests with no proxy, no redirect,
//     identity encoding, and size and time bounds; typed outcomes
//   - verify: manifest parsing and digest, interim package layout, segment
//     checks, sample selection; observations, not verdicts
//   - report: canonical SVN-JCS-1 report encoding
//   - store: content-addressed report store and append-only index
//
// The mapping of outcomes to FAIL or INCOMPLETE (owner question Q3),
// auditor keys (Q1), batch construction and signing (Q2), epochs (Q4), and
// the CLI are not in this module yet.
package auditor
