// Package rehearsal is the goal-10 upgrade rehearsal (plan section 12): the
// upgrade "rehearsal-1" from app version 1 to 2, its handler (Handler, the
// rehearsal binary B2), the wrong handler of the minority run (BadHandler,
// B2-bad), and the module migration content of the in-process rehearsal
// (decision request DR-10-36).
//
// It is linked only into svnd built with the build tag svn_rehearsal (B2)
// or svn_rehearsal_bad (B2-bad), chain/cmd/svnd/rehearsal_tag.go and
// rehearsal_bad_tag.go, and into tests. The default svnd build never links
// it: TestLinkedBinaryOmitsTestHooks fails on any chain/internal symbol in
// svnd. The rehearsal builds come from the same tree as svnd; they are not
// independently built releases, not signed, and not reviewed source.
//
// What a handler can change. An upgrade handler compiled into a binary
// (app.Config.Upgrades) receives the block context, a copy of the stored
// module version map, and the app's Migrate; its context carries the
// handler environment (upgrade.EnvOf), by-name access to every mounted
// module store in the upgrade runner's cache branch (orchestrator choice
// ORCH-7, a reversible implementation choice, not an owner decision). The
// runner's checks
// after the handler refuse any change to the rows it owns and to the I13
// classes (the authority rows except the policy version key, and the
// x/policy safety caps, governed values, and pending changes); a write to
// any other row is kept.
//
// Handler reads every provider, content, lease, evidence, and proposal
// record through the environment and writes nothing (the read-only
// precondition walk of plan 12.2), then runs Migrate with the stored map
// unchanged. The rehearsal binary registers no module step, so its upgrade
// changes only the rows the upgrade runner owns: the app version,
// ConsensusParams.Version.App, the version map, the upgrade record, the
// plan, and the policy version (DR-10-38). The in-process rehearsal
// registers LeasesStep and EvidenceStep through the app's test hook
// (BumpModuleVersionForTest). BadHandler is Handler plus one documented
// write outside every runner check (BadRowKey -> BadRowValue in the x/leases
// store), so a node that runs it commits another app hash at the plan
// height than the nodes that run Handler.
//
// Both handlers refuse a chain whose id does not start with
// genesis.DevnetChainIDPrefix, and the rehearsal builds refuse to start on
// one (defence in depth: no council plan can be written off a devnet chain).
package rehearsal

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

const (
	// UpgradeName is the name of the rehearsal upgrade. A council
	// software_upgrade payload schedules it.
	UpgradeName = "rehearsal-1"

	// FromAppVersion and ToAppVersion are the app versions the rehearsal
	// upgrade moves between: the genesis app version and the next one.
	FromAppVersion uint64 = 1
	ToAppVersion   uint64 = 2

	// BadRowValue is the value of the one row BadHandler writes into the
	// x/leases store under key BadRowKey, a prefix x/leases never uses.
	BadRowValue byte = 0x01
)

// BadRowKey is the key of the row BadHandler writes. x/leases uses the
// prefixes 0x00 to 0x0D only.
var BadRowKey = []byte{0xFF}

// Upgrades are the upgrade handlers of the rehearsal binary B2: one
// upgrade, UpgradeName, from FromAppVersion to ToAppVersion, run by
// Handler. A binary that compiles them declares app version ToAppVersion.
func Upgrades() []upgrade.Upgrade {
	return upgrades(Handler)
}

// BadUpgrades are the upgrade handlers of the rehearsal binary B2-bad: the
// same upgrade name and app versions as Upgrades, run by BadHandler.
func BadUpgrades() []upgrade.Upgrade {
	return upgrades(BadHandler)
}

func upgrades(handler upgrade.Handler) []upgrade.Upgrade {
	return []upgrade.Upgrade{{
		Name:           UpgradeName,
		FromAppVersion: FromAppVersion,
		ToAppVersion:   ToAppVersion,
		Handler:        handler,
	}}
}

// Handler is the rehearsal upgrade's handler. In order, before it runs any
// migration:
//
//  1. it refuses a chain whose id does not start with
//     genesis.DevnetChainIDPrefix;
//  2. it reads every record of the walked stores through the handler
//     environment (checkRecords) and writes nothing;
//
// then it runs the module migrations from the stored version map (the
// app's RunMigrations in its migration order) and returns the version map
// they produce. The upgrade runner then checks that map against the
// binary's, the layout keys, and the rows no upgrade may change (I13), and
// writes the upgrade record. Any error fails the upgrade, and nothing of it
// is written.
func Handler(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
	if !strings.HasPrefix(ctx.ChainID(), genesis.DevnetChainIDPrefix) {
		return nil, fmt.Errorf("rehearsal upgrade %s runs only on a chain whose id starts with %q, not on %q",
			UpgradeName, genesis.DevnetChainIDPrefix, ctx.ChainID())
	}
	env, found := upgrade.EnvOf(ctx)
	if !found {
		return nil, fmt.Errorf("rehearsal upgrade %s: the context carries no handler environment", UpgradeName)
	}
	if err := checkRecords(ctx, env); err != nil {
		return nil, err
	}
	return migrate(ctx, from)
}

// BadHandler is the wrong handler of the rehearsal (B2-bad, plan 12.1, case
// I6): Handler, then one write Handler does not make, through the handler
// environment: BadRowKey -> BadRowValue in the x/leases store. The row is
// outside every check of the upgrade runner (x/leases is not an I13 class),
// so the upgrade applies; a node that runs BadHandler commits an app hash
// that differs from the nodes that run Handler, and the leases invariants
// refuse the row (no row may sit under an unknown prefix).
func BadHandler(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
	migrated, err := Handler(ctx, from, migrate)
	if err != nil {
		return nil, err
	}
	env, _ := upgrade.EnvOf(ctx)
	store, found := env.KVStore(ctx, leases.ModuleName)
	if !found {
		return nil, fmt.Errorf("rehearsal upgrade %s: store %s is not mounted", UpgradeName, leases.ModuleName)
	}
	store.Set(BadRowKey, []byte{BadRowValue})
	return migrated, nil
}

// walked are the stores checkRecords decodes with the record decoder of
// each, in walk order.
var walked = []struct {
	store  string
	decode recordDecoder
}{
	{providers.ModuleName, providerRecord},
	{content.ModuleName, contentRecord},
	{leases.ModuleName, leaseRecord},
	{evidence.ModuleName, evidenceRecord},
}

// checkRecords is the read-only precondition walk of plan 12.2. For each
// store of walked, in key order, every record decodes with no unknown
// field, sits under the key its fields name, and re-encodes to its stored
// bytes (reencode); then every x/authority proposal record decodes
// (checkProposals). It writes nothing. A store the environment does not
// mount is an error. The work is bounded by the size of the state; the
// handler runs once per chain.
func checkRecords(ctx sdk.Context, env upgrade.Env) error {
	for _, w := range walked {
		store, found := env.KVStore(ctx, w.store)
		if !found {
			return fmt.Errorf("rehearsal upgrade %s: store %s is not mounted", UpgradeName, w.store)
		}
		if err := walk(store, w.decode); err != nil {
			return fmt.Errorf("rehearsal upgrade %s: %s store: %w", UpgradeName, w.store, err)
		}
	}
	store, found := env.KVStore(ctx, authority.ModuleName)
	if !found {
		return fmt.Errorf("rehearsal upgrade %s: store %s is not mounted", UpgradeName, authority.ModuleName)
	}
	if err := checkProposals(store); err != nil {
		return fmt.Errorf("rehearsal upgrade %s: %s store: %w", UpgradeName, authority.ModuleName, err)
	}
	return nil
}

// walk decodes every record of store, in key order, and checks it
// (reencode) without writing.
func walk(store storetypes.KVStore, decode recordDecoder) error {
	it := store.Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		key := it.Key()
		if len(key) == 0 {
			return fmt.Errorf("empty key")
		}
		if msg, id := decode(key); msg != nil {
			if _, err := reencode(key, it.Value(), msg, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// authorityProposalPrefix is the x/authority key prefix of proposal
// records (proposalPrefix in chain/x/authority/store.go): the prefix, then
// the proposal id, holding the record as JSON.
const authorityProposalPrefix byte = 0x10

// proposalHead is the part of an x/authority proposal record the walk
// reads. The full record type and its canonical-encoding check are
// x/authority's own (decodeRecord), which every council message and
// Query/Governance run on the records they read.
type proposalHead struct {
	ProposalID string `json:"proposal_id"`
	State      int32  `json:"state"`
}

// checkProposals checks every proposal record of the x/authority store, in
// key order: the value is one JSON object with nothing after it, its
// proposal_id is the id of its key, and its state is a known state other
// than UNSPECIFIED. It writes nothing.
func checkProposals(store storetypes.KVStore) error {
	it := storetypes.KVStorePrefixIterator(store, []byte{authorityProposalPrefix})
	defer it.Close()
	for ; it.Valid(); it.Next() {
		key := it.Key()
		var head proposalHead
		dec := json.NewDecoder(bytes.NewReader(it.Value()))
		if err := dec.Decode(&head); err != nil {
			return fmt.Errorf("proposal record %x does not decode: %w", key, err)
		}
		if dec.More() {
			return fmt.Errorf("proposal record %x has data after its JSON object", key)
		}
		if head.ProposalID != string(key[1:]) {
			return fmt.Errorf("proposal record under %x names %q", key, head.ProposalID)
		}
		if _, known := authorityv1.ProposalState_name[head.State]; !known || head.State == 0 {
			return fmt.Errorf("proposal record %x has state %d", key, head.State)
		}
	}
	return nil
}

// LeasesStep is the rehearsal migration of the x/leases store from layout
// version leases.ConsensusVersion to the next one (plan 12.2): it reads
// every row, decodes every offer and lease record and checks that it
// re-encodes to its stored bytes, deletes every row, writes every row back
// (each record re-encoded), and writes the next layout version. The record
// layout does not change, so the code of the current version reads the
// result. Any row that does not decode, a layout version other than
// leases.ConsensusVersion, or a record that does not re-encode to its stored
// bytes is an error, and the migration (and with it the upgrade) fails.
func LeasesStep(key storetypes.StoreKey) module.MigrationHandler {
	return func(ctx sdk.Context) error {
		return rebuild(ctx.KVStore(key), leases.ConsensusVersion, leaseRecord)
	}
}

// EvidenceStep is the rehearsal migration of the x/evidence store from
// layout version evidence.ConsensusVersion to the next one, with the rules
// of LeasesStep for evidence records.
func EvidenceStep(key storetypes.StoreKey) module.MigrationHandler {
	return func(ctx sdk.Context) error {
		return rebuild(ctx.KVStore(key), evidence.ConsensusVersion, evidenceRecord)
	}
}

// recordDecoder returns, for a row key, the message its record decodes
// into and a function of the decoded record that returns the key after the
// prefix byte the record must sit under (its id, for a record keyed by one
// id), or nil when the row is not a record.
type recordDecoder func(key []byte) (proto.Message, func() string)

func providerRecord(key []byte) (proto.Message, func() string) {
	switch key[0] {
	case providers.ProviderKeyPrefix:
		provider := &providersv1.Provider{}
		return provider, provider.GetProviderId
	case providers.RetiredKeyPrefix:
		retired := &providersv1.PreviousSigningKey{}
		return retired, func() string {
			return string(providers.RetiredKeyKey(retiredOwner(key), retired.GetGeneration())[1:])
		}
	default:
		return nil, nil
	}
}

// retiredOwner is the provider id of a retired-key row key (prefix, id,
// 0x00, 8-byte generation), or "" when the key is shorter than that.
func retiredOwner(key []byte) string {
	if len(key) < 10 {
		return ""
	}
	return string(key[1 : len(key)-9])
}

func contentRecord(key []byte) (proto.Message, func() string) {
	switch key[0] {
	case content.ContentPrefix:
		record := &contentv1.Content{}
		return record, func() string { return contentKey(record.GetContentId(), record.GetContentVersion()) }
	case content.TombstonePrefix:
		record := &contentv1.Tombstone{}
		return record, func() string { return contentKey(record.GetContentId(), record.GetContentVersion()) }
	default:
		return nil, nil
	}
}

// contentKey is the key after the prefix byte of an x/content record (the
// layout of chain/x/content/store.go): the content id, 0x00, and the
// version as 8 bytes big-endian.
func contentKey(id string, version uint64) string {
	out := append([]byte(id), 0x00)
	return string(binary.BigEndian.AppendUint64(out, version))
}

func leaseRecord(key []byte) (proto.Message, func() string) {
	switch key[0] {
	case leases.OfferPrefix:
		offer := &leasesv1.Offer{}
		return offer, offer.GetOfferId
	case leases.LeasePrefix:
		lease := &leasesv1.Lease{}
		return lease, lease.GetLeaseId
	default:
		return nil, nil
	}
}

func evidenceRecord(key []byte) (proto.Message, func() string) {
	if key[0] != evidence.RecordPrefix {
		return nil, nil
	}
	record := &evidencev1.EvidenceRecord{}
	return record, record.GetEvidenceId
}

var deterministic = proto.MarshalOptions{Deterministic: true}

// rebuild checks every row of store before it writes: the layout version is
// from, no key is empty, every record decodes with no unknown field, names
// the id of its key, and re-encodes to its stored bytes. It then deletes
// every row, writes every row back (records re-encoded), and writes layout
// version from+1. The work is bounded by the size of the store; the
// migration runs once per chain.
func rebuild(store storetypes.KVStore, from uint64, decode recordDecoder) error {
	version, found, err := msgkit.ReadLayoutVersion(store)
	switch {
	case err != nil:
		return err
	case !found || version != from:
		return fmt.Errorf("rehearsal migration: layout version %d (found %t), want %d", version, found, from)
	}
	type row struct{ key, value []byte }
	var rows []row
	it := store.Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		key, value := bytes.Clone(it.Key()), bytes.Clone(it.Value())
		if len(key) == 0 {
			it.Close()
			return fmt.Errorf("rehearsal migration: empty key")
		}
		if msg, id := decode(key); msg != nil {
			if value, err = reencode(key, value, msg, id); err != nil {
				it.Close()
				return err
			}
		}
		rows = append(rows, row{key, value})
	}
	if err := it.Close(); err != nil {
		return err
	}
	for _, r := range rows {
		store.Delete(r.key)
	}
	for _, r := range rows {
		store.Set(r.key, r.value)
	}
	msgkit.WriteLayoutVersion(store, from+1)
	return nil
}

// reencode decodes raw into msg and returns its deterministic encoding,
// which must equal raw.
func reencode(key, raw []byte, msg proto.Message, id func() string) ([]byte, error) {
	if err := (proto.UnmarshalOptions{}).Unmarshal(raw, msg); err != nil {
		return nil, fmt.Errorf("rehearsal record check: record %x does not decode: %w", key, err)
	}
	if len(msg.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("rehearsal record check: record %x has unknown fields", key)
	}
	if id() != string(key[1:]) {
		return nil, fmt.Errorf("rehearsal record check: record under %x names %q", key, id())
	}
	out, err := deterministic.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("rehearsal record check: record %x does not encode: %w", key, err)
	}
	if !bytes.Equal(out, raw) {
		return nil, fmt.Errorf("rehearsal record check: record %x does not re-encode to its stored bytes", key)
	}
	return out, nil
}
