// Package worker is the broadcaster's submission engine (goal 11 plan
// sections 8 and 10): it turns validated intents of the PostgreSQL outbox
// into signed transactions, sends them to the one configured node, and
// reconciles every attempt with the chain until the intent is COMMITTED or
// FAILED.
//
// It runs these loops, each over its own database work:
//
//   - one loop per key signer (the service key and the broadcaster key,
//     owner answer Q1 "Both keys, separate signer (Recommended)"). The loop
//     holds the signer's advisory lock on its own connection
//     (store.SignerSession), so a second broadcaster process with the same
//     configuration does not drive the signer, and every attempt write
//     fails once the lock is lost (8.1). It allocates the signer's next
//     sequence from the committed account read at a height of at least the
//     signer's observed height, halts the signer when a transaction it never
//     stored used one of its sequences (DR-11-15), builds an attempt with a
//     timeout height K blocks ahead and above every earlier attempt at that
//     sequence (8.2), asks the signer process for the signature, persists
//     the attempt, counts the send before it happens and records its outcome
//     after (8.3);
//   - one forwarder loop for provider-signed transactions carried verbatim
//     (owner answer Q2 "Relay + verbatim forward (Recommended)"): nothing is
//     signed or allocated, the stored bytes are the attempt;
//   - a housekeeping loop: the worker's full validation of SQL-path intents
//     (plan 6.3, T2), expiry of REQUESTED intents (T4) and the EFFECTIVE
//     promotion of content owner rows (owner answer Q3, plan 5.7);
//   - the observer (section 10): it records the node's latest height in
//     chain_health and opens or closes stall episodes on the database clock.
//
// Every decision is internal/outbox's: the worker reads the intent, its
// live attempt and the chain, passes them to outbox.Reconcile, and stores
// the steps it returns with internal/store, in one database transaction per
// decision. The worker never reports COMMITTED from a broadcast answer: only
// a committed result read from the chain (/tx of an attempt's hash, or the
// first commitment found by the typed event search) moves an intent to
// COMMITTED. Before any new attempt the effect check of plan 8.6 step 3
// runs, and the intent's request is validated again against current chain
// state.
//
// The worker holds no key. It reaches the signer process only through
// Config.RequestSignature, the signer client's request method, which checks
// the returned transaction against the request byte for byte.
//
// Every number it uses is a PROPOSED value of plan section 11 (DR-11-10)
// that the configuration supplies within its hard bounds. All reads trust
// the one configured node (the beta's trusted gateway).
package worker
