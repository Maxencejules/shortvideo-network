// Package content is the x/content module: the coordination registry of
// content packages.
//
// A record holds an opaque content id, the package version, the SHA-256
// digest of the off-chain package manifest, the package byte count, the
// submitting host account, and a coordination state (PENDING or WITHDRAWN).
// Video bytes, manifests, endpoint URLs, and personal data never reach this
// module. A record is not copyright proof, not a hosting receipt, and not
// publication approval.
//
// Everything in this package except the _test.go files runs inside chain
// execution. It reads block data only from sdk.Context, iterates stores in
// key order, and never reads the clock, randomness, the environment, files,
// or the network.
package content

import (
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	// ModuleName is the module name, the store key name, and the business-id
	// peer name.
	ModuleName = "content"

	// ConsensusVersion is the module state version. v1 is the first version.
	ConsensusVersion uint64 = 1

	// HostingDecisionID is the decision that owns the hosting-intent signer
	// role. It is unset; only the labelled devnet role passes.
	HostingDecisionID = "DEC-001"

	// RetentionDecisionID is written into every tombstone. DEC-004 leaves
	// the retention value unset, so the value version stays 0.
	RetentionDecisionID = "DEC-004"

	// DevnetRoleName is the only hosting-intent role that can pass, and only
	// on a devnet whose DEC-001 test parameter names it.
	DevnetRoleName = "DEVNET_TEST_ONLY"

	// DelegateRoleName is the host-custody role DEC-001 rejects.
	DelegateRoleName = "ACCOUNT_HOST_DELEGATE"
)

// Roles reads the submitter role of an outer signer. x/authority implements
// it. An account without a role returns SUBMITTER_ROLE_UNSPECIFIED.
type Roles interface {
	Role(ctx sdk.Context, addr []byte) typesv1.SubmitterRole
}

// Limits reads a governed limit set by a council GovernedLimitChange.
// found is false when no value was set. msgkit.Effective caps the value.
type Limits interface {
	GovernedLimit(ctx sdk.Context, key string) (uint64, bool)
}

// DevnetParams reads the role name of a devnet test parameter. found is
// false on every chain where the parameter is not a labelled, unapproved
// devnet stand-in (x/authority applies the chain id prefix and label rule).
type DevnetParams interface {
	DevnetRole(ctx sdk.Context, decisionID string) (string, bool)
}

// WithdrawalHook runs inside a withdrawal, after the tombstone is written
// and before the withdrawal commits. Goal 08 implements it to void open
// lease offers for the content. An error aborts the withdrawal: nothing of
// it is written. Goal 07 wires no hook because no offer store exists yet.
type WithdrawalHook interface {
	ContentWithdrawn(ctx sdk.Context, contentID string, version uint64) error
}

// InitRecorder records InitGenesis calls for tests. *authority.Order
// satisfies it. A nil recorder records nothing.
type InitRecorder interface {
	Record(name string)
}

// Deps are the read surfaces this module consumes. Roles, Params, and
// Limits are required. Peers joins the global business-id namespace and may
// be nil (no other module). Hook may be nil.
type Deps struct {
	Roles  Roles
	Params DevnetParams
	Limits Limits
	Peers  *msgkit.Peers
	Hook   WithdrawalHook
}

// Keeper owns the content store. It never writes another module's store
// and never returns validator updates.
type Keeper struct {
	storeKey *storetypes.KVStoreKey
	deps     Deps
	ledger   msgkit.Ledger
}

// NewKeeper builds the keeper. A nil store key or a missing required
// dependency is a wiring error and panics before the chain starts.
func NewKeeper(storeKey *storetypes.KVStoreKey, deps Deps) Keeper {
	if storeKey == nil {
		panic("content: nil store key")
	}
	if deps.Roles == nil || deps.Params == nil || deps.Limits == nil {
		panic(fmt.Sprintf("content: missing dependency (roles %t, params %t, limits %t)",
			deps.Roles != nil, deps.Params != nil, deps.Limits != nil))
	}
	return Keeper{
		storeKey: storeKey,
		deps:     deps,
		ledger: msgkit.Ledger{
			Key:    storeKey,
			Prefix: BusinessPrefix,
			Self:   ModuleName,
			Peers:  deps.Peers,
		},
	}
}

// BusinessDigest returns the business digest this module committed for id.
// It implements msgkit.BusinessSource, so other modules see content
// business ids in the single protocol namespace.
func (k Keeper) BusinessDigest(ctx sdk.Context, id string) ([]byte, bool) {
	return k.ledger.BusinessDigest(ctx, id)
}

var _ msgkit.BusinessSource = Keeper{}
