// Package difftest is the keeper-level differential, fuzz, and benchmark
// harness of x/evidence (goal 09). It has no non-test code.
//
// The tests run the real x/providers, x/content, x/leases, and x/evidence
// keepers over real IAVL stores (msgkittest.NewContext), with x/leases wired
// as the content withdrawal hook and x/evidence as the lease renewal's
// evidence read, and compare every step with
// docs/evidence/goal-02/spec_machine.py, driven through
// tests/difftest/spec_oracle.py in a Python subprocess. The x/authority read
// surface (roles, governed limits, devnet test parameters, policy version)
// is a unit-test fake. The harness calls the message servers and the
// x/leases BeginBlock body directly, one cached branch per transaction, as
// the SDK does in FinalizeBlock; it does not run CometBFT, the ante
// handler, or a node. Its results are keeper-level evidence, not end-to-end
// evidence: the app and devnet tests in chain/app and chain/devnet are.
package difftest
