package authority

import (
	"crypto/sha256"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The read surface below is what x/providers, x/content, x/leases,
// x/evidence and x/policy consume through their own small interfaces. Each
// method reads committed authority state through ctx and writes nothing.
// Each reads at most one key of the per-object layout (store.go), so a
// registry or lease message's gas does not depend on authority history.
// Governed limit values are x/policy state (policy Keeper.GovernedLimit);
// this module no longer stores or serves them.

// Role returns the genesis submitter role of addr from its admission row
// (0x02). An address that is not 20 bytes, has no row, or whose row is not
// one byte holding a value of the protocol enum is SUBMITTER_ROLE_UNSPECIFIED.
func (k Keeper) Role(ctx sdk.Context, addr []byte) typesv1.SubmitterRole {
	if len(addr) != 20 {
		return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
	}
	value := ctx.KVStore(k.storeKey).Get(AdmissionKey(addr))
	if len(value) != 1 {
		return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
	}
	role := typesv1.SubmitterRole(value[0])
	if _, known := typesv1.SubmitterRole_name[int32(role)]; !known {
		return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
	}
	return role
}

// DevnetRole returns the role name of the devnet test parameter for
// decisionID, read from the config key (0x01). It applies the State.devnet
// rule with the chain id of ctx: the chain id starts with "shortvideo-dev-",
// the label is PROPOSED_TEST_PARAMETER, and approved_policy is false. found
// is false on every other chain, for any other row, and when the config key
// is missing, does not decode, or has the old layout, so a caller fails
// closed with DECISION_BLOCKED.
func (k Keeper) DevnetRole(ctx sdk.Context, decisionID string) (string, bool) {
	row := k.devnetRow(ctx, decisionID)
	if row == nil {
		return "", false
	}
	return row.GetRoleName(), true
}

// DevnetNumeric returns numeric_value of the devnet test parameter for
// decisionID, read from the config key (0x01), under the same rule as
// DevnetRole: the chain id of ctx starts with "shortvideo-dev-", the label
// is PROPOSED_TEST_PARAMETER, and approved_policy is false. found is false
// on every other chain, for any other row, and when the config key is
// missing, does not decode, or has the old layout, so a caller fails closed
// with DECISION_BLOCKED. A matching row without a numeric value returns
// (0, true); the caller decides what 0 means (x/leases treats a DEC-005
// value of 0 as DECISION_BLOCKED, as sponsorship_seconds does in
// spec_machine.py:384). The value is a devnet test number, not policy.
func (k Keeper) DevnetNumeric(ctx sdk.Context, decisionID string) (uint64, bool) {
	row := k.devnetRow(ctx, decisionID)
	if row == nil {
		return 0, false
	}
	return row.GetNumericValue(), true
}

// devnetRow is the devnet test parameter row for decisionID under the
// State.devnet rule with the chain id of ctx, or nil. A missing,
// undecodable, or old-layout config key is nil.
func (k Keeper) devnetRow(ctx sdk.Context, decisionID string) *typesv1.DevnetTestParameter {
	cfg, err := readConfig(ctx.KVStore(k.storeKey))
	if err != nil {
		return nil
	}
	view := State{ChainID: ctx.ChainID(), config: cfg}
	return view.devnet(decisionID)
}

// PolicyVersion returns the committed policy version (0x04): 1 while the key
// is absent, which is before the first executed proposal, then the stored
// value, which grows by one with each executed proposal (op_execute,
// spec_machine.py:1522) and with each BumpPolicyVersionForUpgrade call. It
// is the value the engine reads (State.policy). x/leases stamps it on each
// accepted lease.
//
// It fails closed: a stored value that is not 8 bytes, or is below 2,
// panics instead of reporting a version, exactly where the engine reports
// ERROR_CODE_INTEGRITY. Only ExecuteChange and BumpPolicyVersionForUpgrade
// write 0x04, so this is integrity damage. The SDK turns a handler panic
// into a failed transaction on every node alike. It implements x/policy's
// VersionSource.
func (k Keeper) PolicyVersion(ctx sdk.Context) uint64 {
	raw := ctx.KVStore(k.storeKey).Get([]byte{policyKey})
	if raw == nil {
		return 1
	}
	version, good := decodeU64(raw)
	if !good || version < 2 {
		panic(fmt.Sprintf("authority: policy version is not an 8-byte value of at least 2 (%d bytes stored)", len(raw)))
	}
	return version
}

// BlockedDecisions returns the blocked decision rows of the config key
// (0x01), in the order the config stores them, decoded for this call (a
// caller may keep or change them). It implements x/policy's DecisionSource:
// Query/Policy and the policy export read the rows here, and x/policy
// stores no copy.
//
// It fails closed: a missing or undecodable config key panics instead of
// reporting no blocked decision. InitGenesis writes the config key and no
// message deletes it, so this is integrity damage; ABCI Query and the node
// gRPC server recover a query panic into an error.
func (k Keeper) BlockedDecisions(ctx sdk.Context) []*typesv1.BlockedDecisionParameter {
	cfg, err := readConfig(ctx.KVStore(k.storeKey))
	if err != nil {
		panic(fmt.Sprintf("authority: blocked decisions: %v", err))
	}
	return cfg.GetBlockedDecisions()
}

// BusinessDigest returns the business digest authority committed for id
// (0x11). It implements msgkit.BusinessSource, so x/providers and x/content
// see authority's ids in the one protocol namespace. It does not consult
// peers. An id outside the protocol pattern cannot be stored, so it is not
// found without a read.
//
// It fails closed: a stored value that is not 32 bytes panics instead of
// reporting the id as free. Only transaction handlers call it (through
// msgkit.Ledger.Gate); the SDK turns a handler panic into a failed
// transaction on every node alike.
func (k Keeper) BusinessDigest(ctx sdk.Context, id string) ([]byte, bool) {
	if !idPattern.MatchString(id) {
		return nil, false
	}
	raw := ctx.KVStore(k.storeKey).Get(businessKey(id))
	if raw == nil {
		return nil, false
	}
	if len(raw) != sha256.Size {
		panic(fmt.Sprintf("authority: business id %q holds %d bytes, not a 32-byte digest", id, len(raw)))
	}
	return append([]byte(nil), raw...), true
}
