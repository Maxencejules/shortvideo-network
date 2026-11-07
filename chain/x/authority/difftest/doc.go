// Package difftest is the keeper-level differential and state-transition
// fuzz harness of the x/authority council flow (goal 10). It has no
// non-test code.
//
// The tests run the real x/authority keeper (propose, approve, execute, and
// the EndBlock validator application) with the real x/providers and
// x/content keepers wired as its council registry hooks and as business-id
// peers, and the real x/policy keeper as its policy writer and as the
// governed limit source of the registries (as chain/app wires them), over
// real IAVL stores (msgkittest.NewContext). Governed values are x/policy's
// rows; a block runs x/policy's BeginBlock body (ApplyDue). They compare
// every step with docs/evidence/goal-02/spec_machine.py, driven through
// tests/difftest/spec_oracle.py in a Python subprocess with its "council"
// reset config. Each divergence the oracle maps is a named rule (G-1 to
// G-11, authority D-1, D-2 and D-7), listed in chain/x/authority/README.md.
// TestUnnamedParseDivergences pins the open divergences that no rule names
// and sends one payload of every v0 field shape class combination to show
// that no other parse divergence is unnamed.
// The generator sends v0 payloads only (and the G-11 shapes); the schema
// version 2 payloads (D-4, D-5) are sent only by TestSchemaTwoPayloadsD5.
//
// The harness calls the message servers, the BeginBlock and EndBlock bodies
// directly, one cached branch per transaction, as the SDK does in
// FinalizeBlock; it does not run CometBFT, the ante handler, the upgrade
// runner, or a node, and it has no consensus parameters. Its results are
// keeper-level evidence, not end-to-end evidence.
package difftest
