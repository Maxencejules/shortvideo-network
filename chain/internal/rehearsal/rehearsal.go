// Package rehearsal is the goal-10 upgrade rehearsal (plan section 12): the
// upgrade "rehearsal-1" from app version 1 to 2, its handler, and the module
// migration content of the in-process rehearsal (decision request DR-10-36).
//
// It is linked only into the rehearsal binaries under
// chain/internal/rehearsal/cmd and into tests. svnd never links it:
// TestLinkedBinaryOmitsTestHooks fails on any chain/internal symbol in svnd.
// The rehearsal binaries are built from the same tree as svnd; they are not
// independently built releases, not signed, and not reviewed source.
//
// What a handler can change. An upgrade handler compiled into a binary
// (app.Config.Upgrades) receives the block context, a copy of the stored
// module version map, and the app's Migrate. It holds no store key, so it
// changes module state only through Migrate: the module migrations that
// module code registered on the app's configurator, or, for a module left
// out of the map it passes, that module's InitGenesis with its default
// genesis (which the svnd modules refuse or panic on when their store holds
// state; auth rewrites its default parameters). Handler runs Migrate with
// the stored map unchanged. The in-process rehearsal registers LeasesStep and EvidenceStep
// through the app's test hook (BumpModuleVersionForTest); the rehearsal
// binary registers none, so its upgrade changes only the rows the upgrade
// runner owns: the app version, ConsensusParams.Version.App, the version
// map, the upgrade record, the plan, and the policy version (DR-10-38).
package rehearsal

import (
	"bytes"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

const (
	// UpgradeName is the name of the rehearsal upgrade. A council
	// software_upgrade payload schedules it.
	UpgradeName = "rehearsal-1"

	// FromAppVersion and ToAppVersion are the app versions the rehearsal
	// upgrade moves between: the genesis app version and the next one.
	FromAppVersion uint64 = 1
	ToAppVersion   uint64 = 2

	// BadRowValue is the value of the one row BadLeasesStep adds to the
	// leases store under key BadRowKey, a prefix x/leases never uses.
	BadRowValue byte = 0x01
)

// BadRowKey is the key of the row BadLeasesStep adds. x/leases uses the
// prefixes 0x00 to 0x0D only.
var BadRowKey = []byte{0xFF}

// Upgrades are the upgrade handlers of the rehearsal binary: one upgrade,
// UpgradeName, from FromAppVersion to ToAppVersion, run by Handler. A
// binary that compiles them declares app version ToAppVersion.
func Upgrades() []upgrade.Upgrade {
	return []upgrade.Upgrade{{
		Name:           UpgradeName,
		FromAppVersion: FromAppVersion,
		ToAppVersion:   ToAppVersion,
		Handler:        Handler,
	}}
}

// Handler is the rehearsal upgrade's handler: it runs the module
// migrations from the stored version map (the app's RunMigrations in its
// migration order) and returns the version map they produce. The upgrade
// runner then checks that map against the binary's, the layout keys, and
// the rows no upgrade may change (I13), and writes the upgrade record.
func Handler(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
	return migrate(ctx, from)
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
		return rebuild(ctx.KVStore(key), leases.ConsensusVersion, leaseRecord, nil)
	}
}

// BadLeasesStep is LeasesStep that also writes one row the honest step
// does not write: BadRowKey -> BadRowValue. It is the wrong migration of the
// rehearsal (incompatible binary): a node that runs it commits an app hash
// that differs from the nodes that run LeasesStep, and the leases
// invariants refuse the row (no row may sit under an unknown prefix). It
// passes the upgrade runner's own checks, which do not cover the leases
// store.
func BadLeasesStep(key storetypes.StoreKey) module.MigrationHandler {
	return func(ctx sdk.Context) error {
		return rebuild(ctx.KVStore(key), leases.ConsensusVersion, leaseRecord, [][2][]byte{{BadRowKey, {BadRowValue}}})
	}
}

// EvidenceStep is the rehearsal migration of the x/evidence store from
// layout version evidence.ConsensusVersion to the next one, with the rules
// of LeasesStep for evidence records.
func EvidenceStep(key storetypes.StoreKey) module.MigrationHandler {
	return func(ctx sdk.Context) error {
		return rebuild(ctx.KVStore(key), evidence.ConsensusVersion, evidenceRecord, nil)
	}
}

// recordDecoder returns, for a row key, the message its record decodes
// into and the id the record must name, or nil when the row is not a
// record.
type recordDecoder func(key []byte) (proto.Message, func() string)

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
// every row, writes every row back (records re-encoded), writes extra, and
// writes layout version from+1. The work is bounded by the size of the
// store; the handler runs once per chain.
func rebuild(store storetypes.KVStore, from uint64, decode recordDecoder, extra [][2][]byte) error {
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
	for _, r := range extra {
		store.Set(r[0], r[1])
	}
	msgkit.WriteLayoutVersion(store, from+1)
	return nil
}

// reencode decodes raw into msg and returns its deterministic encoding,
// which must equal raw.
func reencode(key, raw []byte, msg proto.Message, id func() string) ([]byte, error) {
	if err := (proto.UnmarshalOptions{}).Unmarshal(raw, msg); err != nil {
		return nil, fmt.Errorf("rehearsal migration: record %x does not decode: %w", key, err)
	}
	if len(msg.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("rehearsal migration: record %x has unknown fields", key)
	}
	if id() != string(key[1:]) {
		return nil, fmt.Errorf("rehearsal migration: record under %x names %q", key, id())
	}
	out, err := deterministic.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("rehearsal migration: record %x does not encode: %w", key, err)
	}
	if !bytes.Equal(out, raw) {
		return nil, fmt.Errorf("rehearsal migration: record %x does not re-encode to its stored bytes", key)
	}
	return out, nil
}
