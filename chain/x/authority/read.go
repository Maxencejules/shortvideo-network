package authority

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The read surface below is what x/providers and x/content consume through
// their own small interfaces. Each method reads committed authority state
// through ctx and writes nothing.

// Role returns the genesis submitter role of addr from the admission index.
// An address that is not 20 bytes, not admitted, or indexed with a value
// outside the protocol enum is SUBMITTER_ROLE_UNSPECIFIED.
func (k Keeper) Role(ctx sdk.Context, addr []byte) typesv1.SubmitterRole {
	if len(addr) != 20 {
		return typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
	}
	value := ctx.KVStore(k.storeKey).Get(admitKeyBytes(addr))
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
// decisionID. It applies the State.devnet rule with the chain id of ctx: the
// chain id starts with "shortvideo-dev-", the label is
// PROPOSED_TEST_PARAMETER, and approved_policy is false. found is false on
// every other chain, for any other row, and when the state does not decode,
// so a caller fails closed with DECISION_BLOCKED.
func (k Keeper) DevnetRole(ctx sdk.Context, decisionID string) (string, bool) {
	raw := ctx.KVStore(k.storeKey).Get([]byte{stateKey})
	if raw == nil {
		return "", false
	}
	var stored authorityv1.GenesisState
	if err := proto.Unmarshal(raw, &stored); err != nil {
		return "", false
	}
	view := State{ChainID: ctx.ChainID(), Params: stored.GetDevnetTestParameters()}
	row := view.devnet(decisionID)
	if row == nil {
		return "", false
	}
	return row.GetRoleName(), true
}

// GovernedLimit returns the value an executed GovernedLimitChange set for
// key. authority accepted it only in 1..ceiling. found is false when no
// value was set. The caller applies msgkit.Effective, which uses the
// protocol ceiling when found is false.
//
// The side document is written only by save. If it ever failed to decode,
// every authority message would already fail on load; this method then
// reports not found rather than panic, because query handlers call it and a
// panic there is not recovered on the node's gRPC server.
func (k Keeper) GovernedLimit(ctx sdk.Context, key string) (uint64, bool) {
	raw := ctx.KVStore(k.storeKey).Get([]byte{sideKey})
	if raw == nil {
		return 0, false
	}
	var side struct {
		Governed []namedUint `json:"governed"`
	}
	if err := json.Unmarshal(raw, &side); err != nil {
		return 0, false
	}
	for _, row := range side.Governed {
		if row.ID == key {
			return row.Value, true
		}
	}
	return 0, false
}

// BusinessDigest returns the business digest authority committed for id.
// It implements msgkit.BusinessSource, so x/providers and x/content see
// authority's ids in the one protocol namespace. It does not consult peers.
//
// It fails closed: a side document that does not decode panics instead of
// reporting the id as free. Only transaction handlers call it (through
// msgkit.Ledger.Gate); the SDK turns a handler panic into a failed
// transaction on every node alike.
func (k Keeper) BusinessDigest(ctx sdk.Context, id string) ([]byte, bool) {
	if !idPattern.MatchString(id) {
		return nil, false
	}
	raw := ctx.KVStore(k.storeKey).Get([]byte{sideKey})
	if raw == nil {
		return nil, false
	}
	var side struct {
		Business []namedBytes `json:"business"`
	}
	if err := json.Unmarshal(raw, &side); err != nil {
		panic(fmt.Sprintf("authority: side document does not decode: %v", err))
	}
	for _, row := range side.Business {
		if row.ID == id {
			return append([]byte(nil), row.Value...), true
		}
	}
	return nil, false
}
