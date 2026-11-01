// Package providers is the x/providers module: the provider registry of the
// permissioned beta.
//
// A provider record holds an opaque provider id, the 20-byte account that
// registered it, the current ed25519 signing key and its retired
// predecessors, the SHA-256 digest of the off-chain capability manifest, the
// declared capacity, the reserved bytes, the admission state, and an opaque
// operator group id. Endpoint URLs and every other manifest field stay off
// chain.
//
// Everything in this directory runs inside chain execution. It reads block
// data only from sdk.Context, iterates stores in key order, and never reads
// the local clock, randomness, the environment, files, or the network. A
// source scan test enforces that. The capability subpackage is off-chain
// tooling; the keeper never imports it.
package providers

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// ModuleName is the module and store name. It is also this module's name in
// the shared business-id namespace (msgkit.Peers).
const ModuleName = "providers"

// ConsensusVersion is the store layout version. Version 1 is the first one;
// there is no migration into it.
const ConsensusVersion uint64 = 1

// DevnetOperatorGroup is the only operator group the council may admit while
// DEC-002 stays a devnet test parameter. It is a label, not evidence of an
// independent operator.
const DevnetOperatorGroup = "devnet-test-only"

// Roles reads the submitter role of an outer signer. x/authority implements
// it. An account without a role returns SUBMITTER_ROLE_UNSPECIFIED.
type Roles interface {
	Role(ctx sdk.Context, addr []byte) typesv1.SubmitterRole
}

// Limits reads a governed limit set by a council GovernedLimitChange.
// x/authority implements it. found is false when no value was set.
type Limits interface {
	GovernedLimit(ctx sdk.Context, key string) (uint64, bool)
}

// Deps are the read surfaces this module consumes. They are declared here
// and satisfied structurally, so this package never imports x/authority or
// the app. Peers joins the business-id ledgers of the other modules; it may
// be nil (no peers).
type Deps struct {
	Roles  Roles
	Limits Limits
	Peers  *msgkit.Peers
}

// Keeper owns the providers store. It is a value type; copies share the
// store key and the Peers set.
type Keeper struct {
	key    *storetypes.KVStoreKey
	deps   Deps
	ledger msgkit.Ledger
}

// NewKeeper returns the keeper for key. A nil key, Roles, or Limits is a
// wiring error and panics before the chain starts.
func NewKeeper(key *storetypes.KVStoreKey, deps Deps) Keeper {
	if key == nil {
		panic("providers: nil store key")
	}
	if deps.Roles == nil {
		panic("providers: nil Roles dependency")
	}
	if deps.Limits == nil {
		panic("providers: nil Limits dependency")
	}
	return Keeper{
		key:  key,
		deps: deps,
		ledger: msgkit.Ledger{
			Key:    key,
			Prefix: BusinessKeyPrefix,
			Self:   ModuleName,
			Peers:  deps.Peers,
		},
	}
}

// StoreKey returns the store key of this module.
func (k Keeper) StoreKey() *storetypes.KVStoreKey { return k.key }

// BusinessDigest returns the business digest this module committed for id.
// It makes the keeper a msgkit.BusinessSource, so the other modules see
// provider business ids in the one protocol namespace.
func (k Keeper) BusinessDigest(ctx sdk.Context, id string) ([]byte, bool) {
	return k.ledger.BusinessDigest(ctx, id)
}

// pageLimit is the effective page size cap of the Providers query.
func (k Keeper) pageLimit(ctx sdk.Context) uint32 {
	governed, found := k.deps.Limits.GovernedLimit(ctx, msgkit.LimitMaxPageLimit)
	// Effective never returns more than the protocol ceiling (100), so the
	// conversion cannot truncate.
	return uint32(msgkit.Effective(msgkit.LimitMaxPageLimit, governed, found))
}

var _ msgkit.BusinessSource = Keeper{}
