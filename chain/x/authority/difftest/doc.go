// Package difftest is the keeper-level differential and state-transition
// fuzz harness of the x/authority council flow (goal 10). It has no
// non-test code.
//
// The tests run the real x/authority keeper (propose, approve, execute, and
// the EndBlock validator application) with the real x/providers and
// x/content keepers wired as its council registry hooks and as business-id
// peers, over real IAVL stores (msgkittest.NewContext). They compare every
// step with docs/evidence/goal-02/spec_machine.py, driven through
// tests/difftest/spec_oracle.py in a Python subprocess with its "council"
// reset config. Each divergence the oracle maps is a named rule (G-1 to
// G-10, authority D-1, D-2 and D-7), listed in chain/x/authority/README.md.
// TestUnnamedParseDivergences pins the eight open divergences that no rule
// names; the README lists them too. x/policy is not wired into x/authority
// before Wave 1, so governed values are authority's own 0x13 rows here.
//
// The harness calls the message servers and the EndBlock body directly,
// one cached branch per transaction, as the SDK does in FinalizeBlock; it
// does not run CometBFT, the ante handler, or a node. Its results are
// keeper-level evidence, not end-to-end evidence.
package difftest
