package leases

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Module genesis. The launch genesis keeps offers and leases empty (goal 05
// rule, enforced by the app at InitChain; DR-07-02). The module itself
// validates, imports, and exports any catalogue, so an exported catalogue
// can be checked and imported into fresh stores.
//
// Export carries the open offers and every lease. It does not carry the
// business-id ledger (the v0 genesis schema has no field for it; DR-07-03)
// or the used-offer markers of offers that expired or were voided
// (DR-08-06): a chain started from an export forgets those ids. Index rows
// and counters are derived state that InitGenesis rebuilds.

// exportJSON is the export encoding, the same options the other modules use.
var exportJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

// ParseGenesis decodes a module genesis document. Unknown fields are
// errors. An empty document is an error: the app always writes the key.
func ParseGenesis(raw json.RawMessage) (*leasesv1.GenesisState, error) {
	if len(raw) == 0 {
		return nil, errors.New("leases genesis: empty document")
	}
	var state leasesv1.GenesisState
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("leases genesis: %w", err)
	}
	return &state, nil
}

// MarshalGenesis is the module export encoding: protojson with proto field
// names and unpopulated fields. Compare decoded messages with proto.Equal,
// not these bytes; protojson output is not byte-stable across builds.
func MarshalGenesis(state *leasesv1.GenesisState) (json.RawMessage, error) {
	raw, err := exportJSON.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("leases genesis: %w", err)
	}
	return raw, nil
}

// ValidateGenesis is a pure check of a decoded module genesis.
//
// Offers (open offers):
//   - offer_id, content_id, provider_id in the id pattern; offer_id unique
//   - content_version 1 (v0 content records use version 1)
//   - manifest_digest and terms_digest SHA-256 digests of 32 bytes
//   - package_bytes in 1..268435456 (the protocol package cap)
//   - start, deadline, and expiry in [1, 253402300799]; deadline > start
//   - term_version 1; scheduler 20 bytes
//   - at most 1024 open offers per provider (the max_open_offers ceiling)
//     and at most MaxOpenOffersPerContent per content id
//
// Leases:
//   - lease_id, offer_id, content_id, provider_id in the id pattern;
//     lease_id unique; offer_id unique among leases and not the id of an
//     open offer (offer ids are never reused, D-4)
//   - content_version 1; digests and package_bytes as for offers
//   - start and deadline in [1, 253402300799]; deadline > start
//   - state ACTIVE, RENEWED, ENDED, or EXPIRED (UNSPECIFIED, OFFERED, the
//     reserved 6, and unknown values are invalid; OFFERED is never a lease)
//   - reservation_held exactly when the state is ACTIVE or RENEWED
//   - term_version >= 1; ACTIVE has term_version 1 and RENEWED at least 2
//     (a lease is born at term 1 and each renewal adds one)
//   - policy_version >= 1
//   - at most one held lease per (provider, content, content_version)
//
// No record or digest may carry unknown fields.
func ValidateGenesis(state *leasesv1.GenesisState) error {
	if state == nil {
		return errors.New("leases genesis: nil state")
	}
	if hasUnknown(state) {
		return errors.New("leases genesis: unknown fields")
	}
	offerIDs := make(map[string]bool, len(state.GetOffers()))
	perProvider := map[string]int{}
	perContent := map[string]int{}
	for i, offer := range state.GetOffers() {
		if err := validateOffer(offer); err != nil {
			return fmt.Errorf("leases genesis: offer %d: %w", i, err)
		}
		if offerIDs[offer.GetOfferId()] {
			return fmt.Errorf("leases genesis: offer %d: duplicate offer_id %q", i, offer.GetOfferId())
		}
		offerIDs[offer.GetOfferId()] = true
		perProvider[offer.GetProviderId()]++
		if perProvider[offer.GetProviderId()] > int(genesis.MaxOpenOffersPerProvider) {
			return fmt.Errorf("leases genesis: provider %q has more than %d open offers", offer.GetProviderId(), genesis.MaxOpenOffersPerProvider)
		}
		perContent[offer.GetContentId()]++
		if perContent[offer.GetContentId()] > MaxOpenOffersPerContent {
			return fmt.Errorf("leases genesis: content %q has more than %d open offers", offer.GetContentId(), MaxOpenOffersPerContent)
		}
	}
	leaseIDs := make(map[string]bool, len(state.GetLeases()))
	leaseOffers := make(map[string]bool, len(state.GetLeases()))
	pairs := map[string]string{}
	for i, lease := range state.GetLeases() {
		if err := validateLease(lease); err != nil {
			return fmt.Errorf("leases genesis: lease %d: %w", i, err)
		}
		if leaseIDs[lease.GetLeaseId()] {
			return fmt.Errorf("leases genesis: lease %d: duplicate lease_id %q", i, lease.GetLeaseId())
		}
		leaseIDs[lease.GetLeaseId()] = true
		if leaseOffers[lease.GetOfferId()] || offerIDs[lease.GetOfferId()] {
			return fmt.Errorf("leases genesis: lease %d: offer_id %q is used twice", i, lease.GetOfferId())
		}
		leaseOffers[lease.GetOfferId()] = true
		if lease.GetReservationHeld() {
			pair := string(PairKey(lease.GetProviderId(), lease.GetContentId(), lease.GetContentVersion()))
			if other, taken := pairs[pair]; taken {
				return fmt.Errorf("leases genesis: lease %d: %q and %q hold the same pair", i, other, lease.GetLeaseId())
			}
			pairs[pair] = lease.GetLeaseId()
		}
	}
	return nil
}

func hasUnknown(m protoreflect.ProtoMessage) bool {
	return len(m.ProtoReflect().GetUnknown()) != 0
}

// checkGenesisDigest is a stored SHA-256 digest without unknown fields.
func checkGenesisDigest(name string, d *typesv1.Digest) error {
	if d != nil && hasUnknown(d) {
		return fmt.Errorf("%s has unknown fields", name)
	}
	if _, code := msgkit.CheckDigest(d); code != msgkit.OK {
		return fmt.Errorf("%s: %s", name, code)
	}
	return nil
}

// checkTerms is the part an offer and a lease share.
func checkTerms(contentID, providerID string, version uint64, manifest, terms *typesv1.Digest, packageBytes uint64, start, deadline int64) error {
	for _, id := range []string{contentID, providerID} {
		if !msgkit.ValidID(id) {
			return fmt.Errorf("id %q outside the id pattern", id)
		}
	}
	if version != 1 {
		return fmt.Errorf("content_version %d, want 1", version)
	}
	if err := checkGenesisDigest("manifest_digest", manifest); err != nil {
		return err
	}
	if err := checkGenesisDigest("terms_digest", terms); err != nil {
		return err
	}
	if packageBytes < 1 || packageBytes > genesis.MaxPackageBytes {
		return fmt.Errorf("package_bytes %d outside 1..%d", packageBytes, genesis.MaxPackageBytes)
	}
	for _, t := range []int64{start, deadline} {
		if checkTime(t) != msgkit.OK {
			return fmt.Errorf("time %d outside [%d, %d]", t, genesis.UnixSecondsMin, genesis.UnixSecondsMax)
		}
	}
	if deadline <= start {
		return fmt.Errorf("deadline %d not after start %d", deadline, start)
	}
	return nil
}

func validateOffer(o *leasesv1.Offer) error {
	if o == nil {
		return errors.New("nil record")
	}
	if hasUnknown(o) {
		return errors.New("unknown fields")
	}
	if !msgkit.ValidID(o.GetOfferId()) {
		return fmt.Errorf("offer_id %q outside the id pattern", o.GetOfferId())
	}
	if err := checkTerms(o.GetContentId(), o.GetProviderId(), o.GetContentVersion(), o.GetManifestDigest(), o.GetTermsDigest(),
		o.GetPackageBytes(), o.GetStartUnixSeconds(), o.GetDeadlineUnixSeconds()); err != nil {
		return err
	}
	if checkTime(o.GetExpiryUnixSeconds()) != msgkit.OK {
		return fmt.Errorf("expiry_unix_seconds %d out of range", o.GetExpiryUnixSeconds())
	}
	if o.GetTermVersion() != 1 {
		return fmt.Errorf("term_version %d, want 1", o.GetTermVersion())
	}
	if len(o.GetScheduler()) != msgkit.AccountBytes {
		return fmt.Errorf("scheduler is %d bytes, want %d", len(o.GetScheduler()), msgkit.AccountBytes)
	}
	return nil
}

func validateLease(l *leasesv1.Lease) error {
	if l == nil {
		return errors.New("nil record")
	}
	if hasUnknown(l) {
		return errors.New("unknown fields")
	}
	for _, id := range []string{l.GetLeaseId(), l.GetOfferId()} {
		if !msgkit.ValidID(id) {
			return fmt.Errorf("id %q outside the id pattern", id)
		}
	}
	if err := checkTerms(l.GetContentId(), l.GetProviderId(), l.GetContentVersion(), l.GetManifestDigest(), l.GetTermsDigest(),
		l.GetPackageBytes(), l.GetStartUnixSeconds(), l.GetDeadlineUnixSeconds()); err != nil {
		return err
	}
	switch l.GetState() {
	case leasesv1.LeaseState_LEASE_STATE_ACTIVE:
		if l.GetTermVersion() != 1 {
			return fmt.Errorf("ACTIVE lease has term_version %d, want 1", l.GetTermVersion())
		}
	case leasesv1.LeaseState_LEASE_STATE_RENEWED:
		if l.GetTermVersion() < 2 {
			return fmt.Errorf("RENEWED lease has term_version %d, want at least 2", l.GetTermVersion())
		}
	case leasesv1.LeaseState_LEASE_STATE_ENDED, leasesv1.LeaseState_LEASE_STATE_EXPIRED:
		if l.GetTermVersion() < 1 {
			return errors.New("term_version is 0")
		}
	default:
		return fmt.Errorf("state %d is not a lease state", int32(l.GetState()))
	}
	if l.GetReservationHeld() != held(l.GetState()) {
		return fmt.Errorf("reservation_held %t in state %s", l.GetReservationHeld(), l.GetState())
	}
	if l.GetPolicyVersion() < 1 {
		return errors.New("policy_version is 0")
	}
	return nil
}

// InitGenesis validates state, checks it against the provider and content
// registries, and writes it into an empty store.
//
// The store must be empty: a store holding any key (the goal-05/06 scaffold
// key [0x01], a layout key, or a record) is an error. Cross-module checks
// run through Deps before any write (x/providers and x/content import
// their genesis first):
//
//   - every open offer: Content.CheckOfferable(content, version, manifest,
//     bytes) is OK (a PENDING record with the same version, digest, and
//     bytes) and its provider exists
//   - every lease: its provider exists, and its content exists
//     (Content.CheckNotWithdrawn is OK or WITHDRAWN)
//   - every provider named by an offer or a lease: reserved_bytes equals
//     the sum of package_bytes of its held leases
//
// The Providers surface lists no providers, so a provider that no offer or
// lease names is not checked here.
//
// Writes, in id byte order: the layout version (msgkit.LayoutVersionKey),
// the open offers with their expiry and content rows, counters, and
// used-offer markers, then the leases with the pair, deadline, content, and
// provider rows of every held lease and a used-offer marker for every
// lease's offer_id.
func (k Keeper) InitGenesis(ctx sdk.Context, state *leasesv1.GenesisState) error {
	if err := ValidateGenesis(state); err != nil {
		return err
	}
	it := k.kv(ctx).Iterator(nil, nil)
	occupied := it.Valid()
	if err := it.Close(); err != nil {
		return fmt.Errorf("leases genesis: close iterator: %w", err)
	}
	if occupied {
		return errors.New("leases genesis: store is not empty")
	}
	offers := append([]*leasesv1.Offer(nil), state.GetOffers()...)
	sort.Slice(offers, func(i, j int) bool { return offers[i].GetOfferId() < offers[j].GetOfferId() })
	leases := append([]*leasesv1.Lease(nil), state.GetLeases()...)
	sort.Slice(leases, func(i, j int) bool { return leases[i].GetLeaseId() < leases[j].GetLeaseId() })
	if err := k.checkGenesisRegistries(ctx, offers, leases); err != nil {
		return err
	}

	msgkit.WriteLayoutVersion(k.kv(ctx), ConsensusVersion)
	for _, offer := range offers {
		if err := k.addOffer(ctx, normalizeOffer(offer)); err != nil {
			return fmt.Errorf("leases genesis: %w", err)
		}
	}
	for _, lease := range leases {
		stored := normalizeLease(lease)
		k.saveLease(ctx, stored)
		if stored.GetReservationHeld() {
			k.holdIndexes(ctx, stored)
		}
		k.kv(ctx).Set(UsedOfferKey(stored.GetOfferId()), marker())
	}
	return nil
}

// checkGenesisRegistries runs the cross-module checks of InitGenesis. It
// writes nothing.
func (k Keeper) checkGenesisRegistries(ctx sdk.Context, offers []*leasesv1.Offer, leases []*leasesv1.Lease) error {
	reserved := map[string]uint64{}
	for _, offer := range offers {
		if code := k.deps.Content.CheckOfferable(ctx, offer.GetContentId(), offer.GetContentVersion(),
			offer.GetManifestDigest().GetValue(), offer.GetPackageBytes()); code != msgkit.OK {
			return fmt.Errorf("leases genesis: offer %q: content check %s", offer.GetOfferId(), code)
		}
		if _, found := k.deps.Providers.Provider(ctx, offer.GetProviderId()); !found {
			return fmt.Errorf("leases genesis: offer %q: provider %q not found", offer.GetOfferId(), offer.GetProviderId())
		}
		if _, seen := reserved[offer.GetProviderId()]; !seen {
			reserved[offer.GetProviderId()] = 0
		}
	}
	for _, lease := range leases {
		if code := k.deps.Content.CheckNotWithdrawn(ctx, lease.GetContentId()); code != msgkit.OK && code != msgkit.Withdrawn {
			return fmt.Errorf("leases genesis: lease %q: content check %s", lease.GetLeaseId(), code)
		}
		if _, found := k.deps.Providers.Provider(ctx, lease.GetProviderId()); !found {
			return fmt.Errorf("leases genesis: lease %q: provider %q not found", lease.GetLeaseId(), lease.GetProviderId())
		}
		sum := reserved[lease.GetProviderId()]
		if lease.GetReservationHeld() {
			if lease.GetPackageBytes() > math.MaxUint64-sum {
				return fmt.Errorf("leases genesis: held bytes of provider %q overflow", lease.GetProviderId())
			}
			sum += lease.GetPackageBytes()
		}
		reserved[lease.GetProviderId()] = sum
	}
	providers := make([]string, 0, len(reserved))
	for id := range reserved {
		providers = append(providers, id)
	}
	sort.Strings(providers)
	for _, id := range providers {
		record, _ := k.deps.Providers.Provider(ctx, id)
		if record.GetReservedBytes() != reserved[id] {
			return fmt.Errorf("leases genesis: provider %q reserves %d bytes, its held leases hold %d", id, record.GetReservedBytes(), reserved[id])
		}
	}
	return nil
}

// normalizeOffer is the stored form of a validated offer: digests rebuilt
// from their values.
func normalizeOffer(o *leasesv1.Offer) *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId:             o.GetOfferId(),
		ContentId:           o.GetContentId(),
		ContentVersion:      o.GetContentVersion(),
		ProviderId:          o.GetProviderId(),
		ManifestDigest:      sha256Digest(o.GetManifestDigest().GetValue()),
		PackageBytes:        o.GetPackageBytes(),
		TermsDigest:         sha256Digest(o.GetTermsDigest().GetValue()),
		StartUnixSeconds:    o.GetStartUnixSeconds(),
		DeadlineUnixSeconds: o.GetDeadlineUnixSeconds(),
		ExpiryUnixSeconds:   o.GetExpiryUnixSeconds(),
		TermVersion:         o.GetTermVersion(),
		Scheduler:           append([]byte(nil), o.GetScheduler()...),
	}
}

// normalizeLease is the stored form of a validated lease.
func normalizeLease(l *leasesv1.Lease) *leasesv1.Lease {
	return &leasesv1.Lease{
		LeaseId:             l.GetLeaseId(),
		OfferId:             l.GetOfferId(),
		ContentId:           l.GetContentId(),
		ContentVersion:      l.GetContentVersion(),
		ProviderId:          l.GetProviderId(),
		ManifestDigest:      sha256Digest(l.GetManifestDigest().GetValue()),
		PackageBytes:        l.GetPackageBytes(),
		TermsDigest:         sha256Digest(l.GetTermsDigest().GetValue()),
		StartUnixSeconds:    l.GetStartUnixSeconds(),
		DeadlineUnixSeconds: l.GetDeadlineUnixSeconds(),
		State:               l.GetState(),
		TermVersion:         l.GetTermVersion(),
		PolicyVersion:       l.GetPolicyVersion(),
		ReservationHeld:     l.GetReservationHeld(),
	}
}

// ExportGenesis reads the open offers and every lease, each in id byte
// order. A stored record that does not decode is an error.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*leasesv1.GenesisState, error) {
	state := &leasesv1.GenesisState{}
	if err := k.iterate(ctx, OfferPrefix, func(key, value []byte) error {
		offer, err := decodeOffer(key, value)
		if err != nil {
			return err
		}
		state.Offers = append(state.Offers, offer)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := k.iterate(ctx, LeasePrefix, func(key, value []byte) error {
		lease, err := decodeLease(key, value)
		if err != nil {
			return err
		}
		state.Leases = append(state.Leases, lease)
		return nil
	}); err != nil {
		return nil, err
	}
	return state, nil
}
