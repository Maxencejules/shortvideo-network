package msgkit

// Activation lead bounds of goal 10. A lead is the number of blocks between
// the block that executes a council decision and the height at which the
// decision takes effect. x/authority checks them when a proposal is
// proposed and again when it is executed; x/policy checks the maximum when
// it stores a pending change, and both bounds when it stores a plan. Both
// modules import them from here, so each number is defined once and
// x/authority does not import x/policy.
//
// On 2026-11-07 the owner answered goal 10 question Q3 (DR-10-03) with the
// option "3 of 4 + min notice (Recommended)", whose text named both numbers.
// The numbers themselves remain PROPOSED: they are not approved policy and
// not a live governed value, no payload changes them, and changing one is a
// binary upgrade.
const (
	// MinActivationLeadBlocks is PROPOSED (100 blocks; option text of Q3 (a),
	// DR-10-03). An upgrade plan height must be at least this many blocks
	// above the height of the block that proposes it and of the block that
	// executes it: below is ERROR_CODE_OUT_OF_RANGE, and a refused execute
	// leaves the proposal QUEUED.
	MinActivationLeadBlocks uint64 = 100
	// MaxActivationLeadBlocks is PROPOSED (1,000,000 blocks; option text of
	// Q3 (a), DR-10-03; DR-10-15). An upgrade plan height, and the
	// activation height of a scheduled governed limit change, must be at
	// most this many blocks above the height of the block that proposes or
	// executes it: above is ERROR_CODE_BOUNDS. Applying the same maximum to
	// governed limit activations is an interim implementation choice
	// (DR-10-15), not an owner decision.
	MaxActivationLeadBlocks uint64 = 1_000_000
)
