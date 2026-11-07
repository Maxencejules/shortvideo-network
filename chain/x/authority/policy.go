package authority

import (
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// PolicyWriter is the policy store as an executed council proposal sees it.
// applyPayload is its only caller (TestPolicyWriterCallSites). Every method
// runs its checks before its first write, so a refusal writes nothing.
// FORBIDDEN would mark the proposal REJECTED; any other refusal leaves it
// QUEUED, as for a Registry hook.
type PolicyWriter interface {
	// SetLimit sets the governed limit key to value at once (activation
	// height 0) and deletes any pending change of key. A key outside the
	// governed_limit_caps table, or a value outside 1..ceiling, is BOUNDS.
	SetLimit(key string, value uint64, proposalID string) Outcome
	// SchedulePending stores a change of key to value that takes effect at
	// height activation, with the policy version the execute produced. It
	// replaces any pending change of key.
	SchedulePending(key string, value, activation uint64, proposalID string, policyVersion uint64) Outcome
	// ScheduleUpgrade stores the upgrade plan name at height with the
	// 32-byte release digest. A pending plan of the same name with another
	// digest is CONFLICT.
	ScheduleUpgrade(name string, height uint64, digest []byte, proposalID string) Outcome
	// CancelUpgrade deletes the pending plan name.
	CancelUpgrade(name string) Outcome
	// UpgradeApplied reports whether the upgrade name has applied.
	UpgradeApplied(name string) bool
}

// Accounts creates the auth account of an address that is about to sign,
// such as a new council seat key's account (DR-10-35). No authority code
// calls it: no message replaces a council seat key in this binary
// (RotateCouncilSeat is engine-only), and the protocol has no seat payload.
type Accounts interface {
	EnsureAccount(addr []byte) Outcome
}

// localPolicy is the PolicyWriter of this binary. It keeps governed values
// in the authority store (0x13) and adds its writes to the batch of the
// execute that calls it, so they are flushed only with the rest of a
// successful execute and a refused or failed execute writes nothing.
//
// The authority layout has no row for a pending limit change or an upgrade
// plan, and no v0 payload carries an activation height or a plan, so
// applyPayload calls only SetLimit. SchedulePending, ScheduleUpgrade and
// CancelUpgrade refuse with ERROR_CODE_MALFORMED, the code the spec gives a
// payload alternative it does not know, and write nothing; UpgradeApplied is
// false because no upgrade record exists.
type localPolicy struct {
	b *batch
}

// policyWriter returns the PolicyWriter of one execute, bound to its batch.
func (s *State) policyWriter(b *batch) PolicyWriter {
	return localPolicy{b: b}
}

// SetLimit adds the 0x13 write of key to the batch. The layout has no
// pending rows, so there is no pending change to delete.
func (w localPolicy) SetLimit(key string, value uint64, _ string) Outcome {
	if !governedInRange(key, value) {
		return fail(CodeBounds)
	}
	w.b.set(governedKey(key), encodeU64(value))
	return ok()
}

// SchedulePending refuses: the layout has no pending limit rows.
func (localPolicy) SchedulePending(string, uint64, uint64, string, uint64) Outcome {
	return fail(CodeMalformed)
}

// ScheduleUpgrade refuses: the layout has no upgrade plan row.
func (localPolicy) ScheduleUpgrade(string, uint64, []byte, string) Outcome {
	return fail(CodeMalformed)
}

// CancelUpgrade refuses: the layout has no upgrade plan row.
func (localPolicy) CancelUpgrade(string) Outcome {
	return fail(CodeMalformed)
}

// UpgradeApplied is false: the layout has no upgrade record.
func (localPolicy) UpgradeApplied(string) bool {
	return false
}

// governedInRange reports whether key is a governed limit key of the
// msgkit table (bounds.json governed_limit_caps) and value is in
// 1..its ceiling. ProposeChange (D-1) and SetLimit both apply it.
func governedInRange(key string, value uint64) bool {
	ceiling, known := msgkit.Ceiling(key)
	return known && value >= 1 && value <= ceiling
}
