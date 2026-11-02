package authority

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The read surface below is what x/providers, x/content, and x/leases
// consume through their own small interfaces. Each method reads committed authority state
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
	row := k.devnetRow(ctx, decisionID)
	if row == nil {
		return "", false
	}
	return row.GetRoleName(), true
}

// DevnetNumeric returns numeric_value of the devnet test parameter for
// decisionID, under the same rule as DevnetRole: the chain id of ctx starts
// with "shortvideo-dev-", the label is PROPOSED_TEST_PARAMETER, and
// approved_policy is false. found is false on every other chain, for any
// other row, and when the state does not decode, so a caller fails closed
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
// State.devnet rule with the chain id of ctx, or nil. A missing or
// undecodable state record is nil.
func (k Keeper) devnetRow(ctx sdk.Context, decisionID string) *typesv1.DevnetTestParameter {
	raw := ctx.KVStore(k.storeKey).Get([]byte{stateKey})
	if raw == nil {
		return nil
	}
	var stored authorityv1.GenesisState
	if err := proto.Unmarshal(raw, &stored); err != nil {
		return nil
	}
	view := State{ChainID: ctx.ChainID(), Params: stored.GetDevnetTestParameters()}
	return view.devnet(decisionID)
}

// PolicyVersion returns the committed policy version: policy_version of the
// side document, which starts at 1 and grows by one with each executed
// proposal (op_execute, spec_machine.py:1522). It is the value the engine
// sees when it loads state: 1 when the side document is absent (a store
// before the first save) and when the document holds 0. x/leases stamps it
// on each accepted lease.
//
// It fails closed, like GovernedLimit: a side document that does not decode
// as the document load reads panics instead of reporting version 1. The
// side document is written only by save, so this is an invariant
// violation; the SDK turns a handler panic into a failed transaction on
// every node alike.
func (k Keeper) PolicyVersion(ctx sdk.Context) uint64 {
	raw := ctx.KVStore(k.storeKey).Get([]byte{sideKey})
	if raw == nil {
		return 1
	}
	var side sideDocument
	if err := json.Unmarshal(raw, &side); err != nil {
		panic(fmt.Sprintf("authority: side document does not decode: %v", err))
	}
	if side.PolicyVersion == 0 {
		return 1
	}
	return side.PolicyVersion
}

// GovernedLimit returns the value an executed GovernedLimitChange set for
// key. authority accepted it only in 1..ceiling. found is false when no
// value was set. The caller applies msgkit.Effective, which uses the
// protocol ceiling when found is false.
//
// It fails closed, like BusinessDigest: a side document that does not
// decode panics instead of reporting no limit, so a transaction never runs
// against the protocol ceiling when a lower governed value may be set. The
// side document is written only by save, so this is an invariant
// violation. A transaction handler panic becomes a failed transaction on
// every node alike. Query handlers also call it: ABCI Query recovers a
// panic into an error result, and the node gRPC server
// (RegisterGRPCServerWithSkipCheckHeader) wraps every query in a recovery
// interceptor, so a query fails and the node keeps running.
func (k Keeper) GovernedLimit(ctx sdk.Context, key string) (uint64, bool) {
	raw := ctx.KVStore(k.storeKey).Get([]byte{sideKey})
	if raw == nil {
		return 0, false
	}
	var side struct {
		Governed []namedUint `json:"governed"`
	}
	if err := json.Unmarshal(raw, &side); err != nil {
		panic(fmt.Sprintf("authority: side document does not decode: %v", err))
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
