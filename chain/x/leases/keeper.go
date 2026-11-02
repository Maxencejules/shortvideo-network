// Package leases is the x/leases module: storage obligations between a
// scheduler, a provider, and one registered content package.
//
// An offer (OfferLease, sent by an admitted service) names the exact content
// version, manifest digest, package bytes, terms digest, and real-time start,
// deadline, and offer expiry. It reserves nothing. AcceptLease turns the offer
// into an ACTIVE lease only with the provider's own inner signature over those
// terms, and reserves the package bytes against the provider's admitted
// capacity. RenewLease extends the deadline by one DEC-005 term without
// changing the committed bytes or digest. EndLease and BeginBlock expiry
// release the reservation exactly once.
//
// ACTIVE (and RENEWED) means one thing: a committed obligation that holds a
// reservation. It is not evidence of retrieval, of physical independence, of
// a region, or of publication approval. Observed reachability (AT_RISK) stays
// off chain; no message, field, or state of this module records it.
//
// Everything in this directory except the _test.go files (and the
// leasestest subpackage, which is test support) runs inside chain
// execution. It reads block data only from sdk.Context (the block header time
// is the only clock), iterates stores in key order, and never reads the local
// clock, randomness, the environment, files, or the network.
// TestDeterministicSources runs the syntactic source scan of
// msgkittest.AssertDeterministicSources on it. The module consumes the
// provider, content, authority, and evidence read surfaces through the
// interfaces below; it never imports those modules or the app
// (TestKeeperImports).
package leases

import (
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	// ModuleName is the module name, the store key name, and this module's
	// name in the shared business-id namespace (msgkit.Peers).
	ModuleName = "leases"

	// ConsensusVersion is the store layout version, written at genesis under
	// msgkit.LayoutVersionKey. Version 1 (msgkit.ScaffoldLayoutVersion) was
	// the goal-05/06 scaffold slot: one key [0x01] holding the module genesis
	// JSON, which sits inside this layout's offer prefix. Version 2 is the
	// first lease layout (store.go). No migration from version 1 is
	// registered.
	ConsensusVersion uint64 = 2

	// TermDecisionID is the decision that owns sponsorship durations. DEC-005
	// leaves them unset; only its labelled devnet test number is read, and
	// without it every offer and renewal is DECISION_BLOCKED.
	TermDecisionID = "DEC-005"

	// MaxOpenOffersPerContent bounds the open offers of one content id,
	// whoever sent them (deviation D-6). It is a chain safety constant, not
	// a rule of the v0 spec machine: PROPOSED, decision request DR-08-02. It
	// bounds the work of the withdrawal hook, which voids every open offer
	// of the content inside the withdrawal transaction (WithdrawContent or a
	// council force-withdraw). It counts OPEN offers: a slot frees when its
	// offer is accepted, expires, or is voided.
	MaxOpenOffersPerContent = 256

	// MaxOpenOffersPerContentPerScheduler bounds the open offers one
	// scheduler (the outer signer of OfferLease) holds for one content id
	// (deviation D-10): half of MaxOpenOffersPerContent, so one admitted
	// service alone can never fill a content's D-6 slots. Two admitted
	// services together can. It does not stop one service from filling
	// every provider's shared max_open_offers slots, which blocks other
	// schedulers' offers of every content (DR-08-13). PROPOSED, decision
	// request DR-08-02. It counts OPEN offers.
	MaxOpenOffersPerContentPerScheduler = MaxOpenOffersPerContent / 2

	// MaxOfferLifetimeSeconds bounds expiry_unix_seconds - committed time of
	// a new offer (deviation D-5). It is a chain safety constant, not a
	// DEC-005 duration: PROPOSED, decision request DR-08-03. It bounds how
	// long ONE offer holds a slot. It does not bound how long one scheduler
	// keeps the shared caps occupied: v0 has no decline or cancel message,
	// and an admitted service can send a new offer each time a slot frees.
	// What it does bound: once a scheduler sends no new offers (its role
	// moved by the engine-only service rotation, or the chain upgraded), its
	// open offers are gone within this many seconds of committed time.
	MaxOfferLifetimeSeconds int64 = 86400
)

// Roles reads the submitter role of an outer signer. x/authority implements
// it. An account without a role returns SUBMITTER_ROLE_UNSPECIFIED.
type Roles interface {
	Role(ctx sdk.Context, addr []byte) typesv1.SubmitterRole
}

// Limits reads a governed limit set by a council GovernedLimitChange.
// found is false when no value was set; msgkit.Effective caps the value.
type Limits interface {
	GovernedLimit(ctx sdk.Context, key string) (uint64, bool)
}

// DevnetParams reads the numeric value of a devnet test parameter. found is
// false on every chain where the parameter is not a labelled, unapproved
// devnet stand-in (x/authority applies the chain id prefix and label rule).
type DevnetParams interface {
	DevnetNumeric(ctx sdk.Context, decisionID string) (uint64, bool)
}

// Policy reads the committed policy version. A new lease records it.
type Policy interface {
	PolicyVersion(ctx sdk.Context) uint64
}

// Providers is the provider registry surface x/leases consumes. x/providers
// implements it. Reserve and Release are the only writers of a provider's
// reserved bytes, and x/leases is their only caller.
//
//   - Provider: a copy of the record, or false when absent
//   - CheckNewObligation: NOT_FOUND, or FORBIDDEN unless ADMITTED
//   - VerifyProviderSignature: NOT_FOUND, then msgkit.VerifyInner against
//     the provider's current key only
//   - ReserveProbe: amount 0 MALFORMED, absent NOT_FOUND, OVERFLOW, CAPACITY;
//     never writes
//   - Reserve: the ReserveProbe checks, then reserved += amount
//   - Release: amount 0 MALFORMED, absent NOT_FOUND, amount > reserved
//     UNDERFLOW, then reserved -= amount
type Providers interface {
	Provider(ctx sdk.Context, providerID string) (*providersv1.Provider, bool)
	CheckNewObligation(ctx sdk.Context, providerID string) (*providersv1.Provider, msgkit.Code)
	VerifyProviderSignature(ctx sdk.Context, providerID string, domain typesv1.SignatureDomain, canonical string, sig *typesv1.InnerSignature) msgkit.Code
	ReserveProbe(ctx sdk.Context, providerID string, amount uint64) msgkit.Code
	Reserve(ctx sdk.Context, providerID string, amount uint64) msgkit.Code
	Release(ctx sdk.Context, providerID string, amount uint64) msgkit.Code
}

// Content is the content registry surface x/leases consumes. x/content
// implements it.
//
//   - CheckOfferable: NOT_FOUND, WITHDRAWN, CONFLICT (not PENDING or another
//     version), INTEGRITY (digest or bytes differ), in op_offer order
//   - CheckNotWithdrawn: NOT_FOUND, WITHDRAWN
//   - CheckPackage: the record of (content, version) in any state: absent
//     NOT_FOUND, digest or bytes differ INTEGRITY (InitGenesis, leases)
type Content interface {
	CheckOfferable(ctx sdk.Context, contentID string, version uint64, manifestSHA256 []byte, packageBytes uint64) msgkit.Code
	CheckNotWithdrawn(ctx sdk.Context, contentID string) msgkit.Code
	CheckPackage(ctx sdk.Context, contentID string, version uint64, manifestSHA256 []byte, packageBytes uint64) msgkit.Code
}

// Evidence reads one evidence record for the renewal check of op_renew.
// x/evidence (goal 09) implements it. found is false when no record exists.
type Evidence interface {
	RenewalEvidence(ctx sdk.Context, evidenceID string) (expiry int64, expired bool, found bool)
}

// Deps are the read surfaces this module consumes. They are declared here
// and satisfied structurally. Roles, Limits, Params, Policy, Providers, and
// Content are required. Evidence may be nil until goal 09 wires x/evidence;
// every evidence_id is then NOT_FOUND. Peers joins the business-id ledgers
// of the other modules and may be nil (no peers).
type Deps struct {
	Roles     Roles
	Limits    Limits
	Params    DevnetParams
	Policy    Policy
	Providers Providers
	Content   Content
	Evidence  Evidence
	Peers     *msgkit.Peers
}

// Keeper owns the leases store. It writes only its own store; reserved
// bytes change only through Providers.Reserve and Providers.Release. It is a
// value type; copies share the store key and the Peers set.
type Keeper struct {
	key    *storetypes.KVStoreKey
	deps   Deps
	ledger msgkit.Ledger
}

// NewKeeper returns the keeper for key. A nil key or a missing required
// dependency is a wiring error and panics before the chain starts.
func NewKeeper(key *storetypes.KVStoreKey, deps Deps) Keeper {
	if key == nil {
		panic("leases: nil store key")
	}
	if deps.Roles == nil || deps.Limits == nil || deps.Params == nil || deps.Policy == nil ||
		deps.Providers == nil || deps.Content == nil {
		panic(fmt.Sprintf("leases: missing dependency (roles %t, limits %t, params %t, policy %t, providers %t, content %t)",
			deps.Roles != nil, deps.Limits != nil, deps.Params != nil, deps.Policy != nil,
			deps.Providers != nil, deps.Content != nil))
	}
	return Keeper{
		key:  key,
		deps: deps,
		ledger: msgkit.Ledger{
			Key:    key,
			Prefix: BusinessPrefix,
			Self:   ModuleName,
			Peers:  deps.Peers,
		},
	}
}

// StoreKey returns the store key of this module.
func (k Keeper) StoreKey() *storetypes.KVStoreKey { return k.key }

// BusinessDigest returns the business digest this module committed for id.
// It makes the keeper a msgkit.BusinessSource, so the other modules see
// lease business ids in the one protocol namespace.
func (k Keeper) BusinessDigest(ctx sdk.Context, id string) ([]byte, bool) {
	return k.ledger.BusinessDigest(ctx, id)
}

// Lease returns a copy of one lease in any state, or false when no lease
// has that id. It is the handoff read for goal 09 (DEC-010 compares the
// lease manifest digest). A stored record that does not decode panics
// (invariant violation, as every message path treats it).
func (k Keeper) Lease(ctx sdk.Context, leaseID string) (*leasesv1.Lease, bool) {
	return k.mustLoadLease(ctx, leaseID)
}

// limit is the effective value of one governed limit key.
func (k Keeper) limit(ctx sdk.Context, key string) uint64 {
	governed, found := k.deps.Limits.GovernedLimit(ctx, key)
	return msgkit.Effective(key, governed, found)
}

// pageLimit is the effective page size cap of the queries. Effective never
// returns more than the protocol ceiling (100), so the conversion cannot
// truncate.
func (k Keeper) pageLimit(ctx sdk.Context) uint32 {
	return uint32(k.limit(ctx, msgkit.LimitMaxPageLimit))
}

var _ msgkit.BusinessSource = Keeper{}
