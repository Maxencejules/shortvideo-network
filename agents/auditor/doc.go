// Command auditor is the off-chain audit producer for permitted test
// endpoints (goal 09). An audit result is an attestation by an auditor, not
// a storage proof and not a unique-replica proof.
//
// The commands separate network use from key use:
//
//   - keygen: writes a fresh dev-only ed25519 key (no network)
//   - run: reads the chain (header, held lease, content, provider) and
//     fetches from provider endpoints; writes reports and a pending run; it
//     never opens the key path
//   - sign: builds one batch per provider from a pending run, signs it, and
//     stores the batch and its envelopes; no network
//   - submit: broadcasts stored batches to the chain RPC only and waits for
//     committed results; an acknowledgement is not a commit
//   - status: reads committed results and records; broadcasts nothing
//   - verify: the reviewer's check of one evidence id against a store
//
// Packages:
//
//   - config: strict operator configuration with no bypass field
//   - policy: destination policy (PROPOSED, DR-09-10): URL rules, deny
//     unless globally reachable against the pinned IANA special-purpose
//     table, exact (provider_id, host, ip, port) allowlist entries
//   - fetch: pinned-address HTTPS requests with no proxy, no redirect,
//     identity encoding, and size and time bounds; typed outcomes
//   - verify: manifest parsing and digest, interim package layout, segment
//     checks, sample selection; observations, not verdicts
//   - classify: PASS, FAIL, or INCOMPLETE under the owner's Q3 ruling
//   - report: canonical SVN-JCS-1 report encoding
//   - store: content-addressed report store and append-only index
//   - keys: dev-only ed25519 key files (owner's Q1 ruling)
//   - batch: evidence ids, batches, envelopes, and signing through
//     chain/x/evidence (owner's Q2 and Q4 rulings)
//   - chainclient: chain reads, outer transactions, submission states
//   - review: the reviewer's verdicts
//   - labels: the evidence label catalog
package main
