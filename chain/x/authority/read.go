package authority

import (
	"crypto/sha256"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The read surface below is what x/providers and x/content consume through
// their own small interfaces. Each method reads committed authority state
// through ctx and writes nothing. Each reads at most one key of the
// per-object layout (store.go), so a registry message's gas does not depend
// on authority history.

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
	cfg, err := readConfig(ctx.KVStore(k.storeKey))
	if err != nil {
		return "", false
	}
	view := State{ChainID: ctx.ChainID(), config: cfg}
	row := view.devnet(decisionID)
	if row == nil {
		return "", false
	}
	return row.GetRoleName(), true
}

// GovernedLimit returns the value an executed GovernedLimitChange set for
// key (0x13). authority writes only values in 1..ceiling. found is false
// when no value was set. The caller applies msgkit.Effective, which uses the
// protocol ceiling when found is false.
//
// It fails closed, like BusinessDigest: a stored value that is not 8 bytes,
// or is outside 1..ceiling of a known key, panics instead of reporting no
// limit, so a transaction never runs against the protocol ceiling when a
// lower governed value may be set. Only ExecuteChange writes 0x13, so this
// is integrity damage. A transaction handler panic becomes a failed
// transaction on every node alike. Query handlers also call it: ABCI Query
// recovers a panic into an error result, and the node gRPC server
// (RegisterGRPCServerWithSkipCheckHeader) wraps every query in a recovery
// interceptor, so a query fails and the node keeps running.
func (k Keeper) GovernedLimit(ctx sdk.Context, key string) (uint64, bool) {
	raw := ctx.KVStore(k.storeKey).Get(governedKey(key))
	if raw == nil {
		return 0, false
	}
	value, good := decodeU64(raw)
	ceiling, known := governedCeiling(key)
	if !good || !known || value < 1 || value > ceiling {
		panic(fmt.Sprintf("authority: governed limit %q is not an 8-byte value in 1..ceiling (%d bytes stored)", key, len(raw)))
	}
	return value, true
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
