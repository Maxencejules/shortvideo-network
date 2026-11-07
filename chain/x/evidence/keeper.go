// Package evidence is the x/evidence module: signed beta attestations.
//
// An admitted AUDITOR account submits a batch of items (MsgSubmitAuditBatch).
// Each item states one observation of one held lease: the SHA-256 digest the
// auditor reports for the lease's package, an expiry, and the round (epoch)
// it was made in. One ed25519 signature by the auditor's own account key
// covers every item (signdoc.go). The chain compares the reported digest
// with the lease's manifest digest and stores a constant-size record:
// RECORDED when they are equal, REJECTED otherwise (DEC-010). It fetches
// nothing.
//
// An attestation is a signed statement by an admitted key. It is not a
// storage proof, not proof of independent storage or unique replication,
// not proof of future delivery, and not proof that the statement is true.
// No record extends, renews, revives, or ends a lease; this module writes
// only its own store.
//
// Everything in this directory except the _test.go files (and the
// evidencetest subpackage, which is test support) runs inside chain
// execution. It reads block data only from sdk.Context (the block header
// time is the only clock), iterates stores in key order, and never reads the
// local clock, randomness, the environment, files, or the network.
// TestDeterministicSources runs the syntactic source scan of
// msgkittest.AssertDeterministicSources on it. The module consumes the
// authority, lease, and content read surfaces through the interfaces below;
// it never imports those modules or the app (TestKeeperImports).
package evidence

import (
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	// ModuleName is the module name, the store key name, and this module's
	// name in the shared business-id namespace (msgkit.Peers).
	ModuleName = "evidence"

	// ConsensusVersion is the store layout version, written at genesis under
	// msgkit.LayoutVersionKey. Version 1 (msgkit.ScaffoldLayoutVersion) was
	// the goal-05 scaffold slot: one key [0x01] holding the module genesis
	// JSON, which sits inside this layout's record prefix. Version 2 is the
	// first evidence layout (store.go). No migration from version 1 is
	// registered.
	ConsensusVersion uint64 = 2

	// SchemaVersion is the only accepted evidence message schema version
	// (svn.evidence.v1 schema_version, separate from the bounds.json document
	// schema version). Every record stores it.
	SchemaVersion uint32 = 1

	// EpochSeconds is the length of one audit round in seconds of block
	// header time: the current epoch is EpochAt(committed time) and a batch
	// must name it (deviation D-3). It is a chain-wide PROPOSED constant
	// chosen by the owner's ruling on Q4 ("Strict, read-time (Recommended)",
	// DR-09-03). It is compiled into every chain; changing it is a consensus
	// change that changes the meaning of every stored epoch.
	EpochSeconds int64 = 3600

	// MaxEvidenceLifetimeSeconds bounds expiry_unix_seconds - committed time
	// of a new item (deviation D-9). Chain-wide PROPOSED constant chosen by
	// the owner's ruling on Q4 (DR-09-06, lifetime half). With the lease
	// deadline bound (D-18) an item's expiry is at most min(committed time +
	// MaxEvidenceLifetimeSeconds, lease deadline).
	MaxEvidenceLifetimeSeconds int64 = 86400

	// MaxEvidencePerAuditorPerEpoch bounds the items one auditor records in
	// one epoch (deviation D-15). It counts the open window only and resets
	// each epoch; it is not a lifetime cap. Chain-wide PROPOSED constant,
	// fail-closed interim rule of decision request DR-09-06 (numbers); not
	// an owner decision.
	MaxEvidencePerAuditorPerEpoch uint64 = 1024

	// IDPrefixBytes is the number of leading signer-address bytes whose
	// lowercase hex, followed by ".", starts every evidence id and batch id
	// of that signer (deviation D-17; owner ruling on Q2, DR-09-29).
	IDPrefixBytes = 8
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

// Leases reads one lease in any state. x/leases implements it (Keeper.Lease,
// its goal-09 handoff read). found is false when no lease has the id.
type Leases interface {
	Lease(ctx sdk.Context, leaseID string) (*leasesv1.Lease, bool)
}

// Content reads the withdrawal state of a content id. x/content implements
// it: an absent id is NOT_FOUND, a WITHDRAWN record is WITHDRAWN, any other
// record is OK.
type Content interface {
	CheckNotWithdrawn(ctx sdk.Context, contentID string) msgkit.Code
}

// Deps are the read surfaces this module consumes, declared here and
// satisfied structurally. Roles, Limits, Leases, and Content are required.
// Peers joins the business-id ledgers of the other modules and may be nil
// (no peers).
type Deps struct {
	Roles   Roles
	Limits  Limits
	Leases  Leases
	Content Content
	Peers   *msgkit.Peers
}

// Keeper owns the evidence store. It writes only its own store. It is a
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
		panic("evidence: nil store key")
	}
	if deps.Roles == nil || deps.Limits == nil || deps.Leases == nil || deps.Content == nil {
		panic(fmt.Sprintf("evidence: missing dependency (roles %t, limits %t, leases %t, content %t)",
			deps.Roles != nil, deps.Limits != nil, deps.Leases != nil, deps.Content != nil))
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
// evidence business ids in the one protocol namespace.
func (k Keeper) BusinessDigest(ctx sdk.Context, id string) ([]byte, bool) {
	return k.ledger.BusinessDigest(ctx, id)
}

// RenewalEvidence is the read x/leases uses for the evidence check of a
// renewal (leases.Evidence). An id outside the id pattern is not found
// without a store read. A present record returns its expiry and whether its
// STORED state is EXPIRED (only a genesis-imported record is stored
// EXPIRED; deviation D-4). x/leases then rejects the renewal with EXPIRED
// when expired or expiry <= committed time, and with FORBIDDEN otherwise:
// no evidence state renews or extends a lease. A stored record that does
// not decode panics (invariant violation).
func (k Keeper) RenewalEvidence(ctx sdk.Context, evidenceID string) (expiry int64, expired bool, found bool) {
	record, found := k.mustLoadRecord(ctx, evidenceID)
	if !found {
		return 0, false, false
	}
	return record.GetExpiryUnixSeconds(), record.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED, true
}

// StoredRecord returns a copy of one record as stored (its stored state,
// not the reported state), or false when absent. A stored record that does
// not decode panics (invariant violation).
func (k Keeper) StoredRecord(ctx sdk.Context, evidenceID string) (*evidencev1.EvidenceRecord, bool) {
	return k.mustLoadRecord(ctx, evidenceID)
}

// EpochAt is the audit round of a committed time: floor(t / EpochSeconds).
// A time below 1 (outside the protocol time range) is epoch 0.
func EpochAt(unixSeconds int64) uint64 {
	if unixSeconds < 1 {
		return 0
	}
	return uint64(unixSeconds / EpochSeconds)
}

// ReportedState is the state a query, an export, and every label report for
// a record at committed time now (deviation D-4, read-time expiry): a
// RECORDED record whose expiry <= now reports EXPIRED; every other record
// reports its stored state. Nothing is swept; stored bytes never change.
func ReportedState(record *evidencev1.EvidenceRecord, now int64) evidencev1.EvidenceState {
	state := record.GetState()
	if state == evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED && record.GetExpiryUnixSeconds() <= now {
		return evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED
	}
	return state
}

// limit is the effective value of one governed limit key.
func (k Keeper) limit(ctx sdk.Context, key string) uint64 {
	governed, found := k.deps.Limits.GovernedLimit(ctx, key)
	return msgkit.Effective(key, governed, found)
}

// pageLimit is the effective page size cap of the query. Effective never
// returns more than the protocol ceiling (100), so the conversion cannot
// truncate.
func (k Keeper) pageLimit(ctx sdk.Context) uint32 {
	return uint32(k.limit(ctx, msgkit.LimitMaxPageLimit))
}

var _ msgkit.BusinessSource = Keeper{}
