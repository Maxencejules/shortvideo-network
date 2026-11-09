// Package outbox is the broadcaster's state machine: the intent states and
// attempt phases, the transition table, the rule that decides when an attempt
// can no longer commit, and the reconciliation decision that runs before any
// resend. It performs no I/O. Every function takes typed observations (what
// the database row says, what the node answered, what the effect check
// found) and returns the next state and the actions the caller may take.
// The package imports only the standard library.
//
// Plan sections (goal 11 plan revision 2):
//
//   - 9.1 intent states, phases and the caller-visible status (Present);
//   - 9.2 attempt phases ordered by knowledge (RaisePhase);
//   - 9.3 the transition table T1-T17 (Apply);
//   - 8.4 the committed-result table and 8.5 the dead rule (Judge);
//   - 8.6 reconciliation before any resend (Reconcile);
//   - 8.7 the retry classification the T13 and T14 guards use;
//   - 10 the stall overlay and the stale-observer rule (Present, and the
//     stall guard of T8).
//
// What the package guarantees, independent of its callers:
//
//   - an intent becomes COMMITTED only through T5, T11 or T12. T11 and T12
//     need a Verdict, which only Judge creates and only from a committed
//     /tx result whose bytes hash to the attempt's hash. T5 and T12 need an
//     effect check that found the first commitment;
//   - a broadcast acknowledgement (ACK) never commits anything;
//   - COMMITTED and FAILED absorb every event (T17);
//   - an attempt is dead only from a committed failure (d1; a failure whose
//     ante did not consume the sequence only once the height reaches
//     timeout_height + L, because those bytes can still commit until then),
//     from refusals of every started send with every send recorded (d2), or
//     from chain heights and sequences (d3); never from a clock or an
//     expiry;
//   - no send is started after expiry, and no new attempt starts while an
//     attempt is live, while a stall episode is open, while the signer is
//     halted or before the effect check found no effect.
//
// Times are never read here. "Expired", "due" and "stalled" are facts the
// caller reads from the database clock and the chain observer.
//
// Scope (Wave 0). No owner answer to goal 11 Q1-Q4 is assumed. The package
// has no message kinds, keys, permissions or forwarded submissions; the
// effect check per kind and the signer rule run in internal/chain, and this
// package consumes their verdict as an Effect.
package outbox
