package policy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
)

// Store layout (store key "policy"). Every value is non-empty and has a
// constant size: the goleveldb backend reports a key with an empty value
// as absent (msgkit.LayoutVersionKey).
//
//	0x00                 -> layout version 2, 8 bytes BE (msgkit.LayoutVersionKey); InitGenesis
//	0x01                 -> svn.policy.v1.SafetyCaps, deterministic protobuf; InitGenesis only, never changed
//	0x02 | limit_key     -> governed value, 8 bytes BE, in 1..ceiling; SetLimit
//	0x03 | limit_key     -> pending change (PendingValueBytes); SchedulePending; deleted by SetLimit of the key
//	0x04                 -> app version, 8 bytes BE, at least 1; InitVersions, RecordUpgrade
//	0x05 | module name   -> consensus version, 8 bytes BE, at least 1; InitVersions, RecordUpgrade
//	0x06                 -> upgrade plan (PlanValueBytes); ScheduleUpgrade; deleted by CancelUpgrade, RecordUpgrade
//	0x07 | upgrade name  -> height at which the upgrade applied, 8 bytes BE, at least 1; RecordUpgrade
//
// Limit keys, module names, upgrade names, and proposal ids match the id
// pattern ^[a-z0-9][a-z0-9._-]{0,63}$, so a key never contains 0x00 and key
// byte order is name byte order. Bounds: 0x02 and 0x03 hold at most one row
// per governed limit key (five); 0x04 and 0x06 are single keys; 0x05 holds
// at most MaxVersionMapModules rows; 0x07 grows by one row per applied
// upgrade.
const (
	GovernedPrefix   byte = 0x02
	PendingPrefix    byte = 0x03
	VersionMapPrefix byte = 0x05
	AppliedPrefix    byte = 0x07

	safetyCapsByte byte = 0x01
	appVersionByte byte = 0x04
	planByte       byte = 0x06
)

// idFieldBytes is the width of an id field inside a value: the longest id
// the id pattern allows. A shorter id is followed by 0x00 bytes; an id never
// contains 0x00, so the padding is unambiguous.
const idFieldBytes = msgkit.IDMaxBytes

// PendingValueBytes is the size of a pending change value: activation
// height, value, and policy version (8 bytes BE each), then the proposal id
// in a 64-byte field.
const PendingValueBytes = 8 + 8 + 8 + idFieldBytes

// PlanValueBytes is the size of an upgrade plan value: height (8 bytes BE),
// the 32-byte release digest, then the name and the proposal id in 64-byte
// fields.
const PlanValueBytes = 8 + msgkit.DigestBytes + idFieldBytes + idFieldBytes

// SafetyCapsKey returns the key of the safety caps row.
func SafetyCapsKey() []byte { return []byte{safetyCapsByte} }

// AppVersionKey returns the key of the app version row.
func AppVersionKey() []byte { return []byte{appVersionByte} }

// PlanKey returns the key of the upgrade plan row.
func PlanKey() []byte { return []byte{planByte} }

func nameKey(prefix byte, name string) []byte {
	out := make([]byte, 0, 1+len(name))
	out = append(out, prefix)
	return append(out, name...)
}

// GovernedKey is the governed value row of one limit key.
func GovernedKey(limit string) []byte { return nameKey(GovernedPrefix, limit) }

// PendingKey is the pending change row of one limit key.
func PendingKey(limit string) []byte { return nameKey(PendingPrefix, limit) }

// VersionMapKey is the version map row of one module.
func VersionMapKey(module string) []byte { return nameKey(VersionMapPrefix, module) }

// AppliedKey is the applied-upgrade row of one upgrade name.
func AppliedKey(name string) []byte { return nameKey(AppliedPrefix, name) }

// Pending is a governed limit change that applies at a later height.
// PolicyVersion is the policy version the executing proposal produced.
type Pending struct {
	Activation    uint64
	Value         uint64
	PolicyVersion uint64
	ProposalID    string
}

// Plan is the scheduled upgrade. Digest is the 32-byte SHA-256 release
// digest the proposal named; the chain cannot check which binary a node
// runs, so it is information for operators.
type Plan struct {
	Name       string
	Height     uint64
	Digest     []byte
	ProposalID string
}

// EncodeU64 is the 8-byte big-endian value of every scalar row.
func EncodeU64(v uint64) []byte { return binary.BigEndian.AppendUint64(make([]byte, 0, 8), v) }

// DecodeU64 reads an 8-byte big-endian value. Any other length is an error.
func DecodeU64(raw []byte) (uint64, error) {
	if len(raw) != 8 {
		return 0, fmt.Errorf("policy: scalar value is %d bytes, want 8", len(raw))
	}
	return binary.BigEndian.Uint64(raw), nil
}

// putID writes id into a 64-byte zero-padded field. Writers check ids
// before encoding, so an id that is not valid is a wiring error and panics.
func putID(dst []byte, id string) []byte {
	if !msgkit.ValidID(id) {
		panic(fmt.Sprintf("policy: encode id %q outside the id pattern", id))
	}
	field := make([]byte, idFieldBytes)
	copy(field, id)
	return append(dst, field...)
}

// readID reads a 64-byte zero-padded id field. The id ends at the first 0x00
// byte; every byte after it must be 0x00, and the id must match the id
// pattern.
func readID(field []byte) (string, error) {
	n := bytes.IndexByte(field, 0x00)
	if n < 0 {
		n = len(field)
	}
	for _, b := range field[n:] {
		if b != 0x00 {
			return "", errors.New("policy: id field has bytes after its padding starts")
		}
	}
	id := string(field[:n])
	if !msgkit.ValidID(id) {
		return "", fmt.Errorf("policy: id field %q outside the id pattern", id)
	}
	return id, nil
}

// EncodePending is the value of a pending change row. The proposal id must
// match the id pattern.
func EncodePending(p Pending) []byte {
	out := make([]byte, 0, PendingValueBytes)
	out = binary.BigEndian.AppendUint64(out, p.Activation)
	out = binary.BigEndian.AppendUint64(out, p.Value)
	out = binary.BigEndian.AppendUint64(out, p.PolicyVersion)
	return putID(out, p.ProposalID)
}

// DecodePending reads a pending change value. It checks the size, an
// activation height and a value of at least 1, a policy version of at least
// 2 (the first execute produces version 2), and the proposal id. The value's
// range for its key is the caller's check.
func DecodePending(raw []byte) (Pending, error) {
	if len(raw) != PendingValueBytes {
		return Pending{}, fmt.Errorf("policy: pending value is %d bytes, want %d", len(raw), PendingValueBytes)
	}
	p := Pending{
		Activation:    binary.BigEndian.Uint64(raw[0:8]),
		Value:         binary.BigEndian.Uint64(raw[8:16]),
		PolicyVersion: binary.BigEndian.Uint64(raw[16:24]),
	}
	if p.Activation < 1 || p.Value < 1 || p.PolicyVersion < 2 {
		return Pending{}, fmt.Errorf("policy: pending value has activation %d, value %d, policy version %d", p.Activation, p.Value, p.PolicyVersion)
	}
	id, err := readID(raw[24:])
	if err != nil {
		return Pending{}, err
	}
	p.ProposalID = id
	return p, nil
}

// EncodePlan is the value of the plan row. The digest must be 32 bytes and
// the name and the proposal id must match the id pattern.
func EncodePlan(p Plan) []byte {
	if len(p.Digest) != msgkit.DigestBytes {
		panic(fmt.Sprintf("policy: encode plan digest of %d bytes", len(p.Digest)))
	}
	out := make([]byte, 0, PlanValueBytes)
	out = binary.BigEndian.AppendUint64(out, p.Height)
	out = append(out, p.Digest...)
	out = putID(out, p.Name)
	return putID(out, p.ProposalID)
}

// DecodePlan reads a plan value. It checks the size, a height of at least 1,
// the name, and the proposal id.
func DecodePlan(raw []byte) (Plan, error) {
	if len(raw) != PlanValueBytes {
		return Plan{}, fmt.Errorf("policy: plan value is %d bytes, want %d", len(raw), PlanValueBytes)
	}
	p := Plan{
		Height: binary.BigEndian.Uint64(raw[0:8]),
		Digest: append([]byte(nil), raw[8:8+msgkit.DigestBytes]...),
	}
	if p.Height < 1 {
		return Plan{}, errors.New("policy: plan value has height 0")
	}
	off := 8 + msgkit.DigestBytes
	name, err := readID(raw[off : off+idFieldBytes])
	if err != nil {
		return Plan{}, err
	}
	proposal, err := readID(raw[off+idFieldBytes:])
	if err != nil {
		return Plan{}, err
	}
	p.Name, p.ProposalID = name, proposal
	return p, nil
}

var deterministic = proto.MarshalOptions{Deterministic: true}

// EncodeSafetyCaps is the deterministic protobuf of caps.
func EncodeSafetyCaps(caps *policyv1.SafetyCaps) []byte {
	raw, err := deterministic.Marshal(caps)
	if err != nil {
		panic(fmt.Sprintf("policy: marshal safety caps: %v", err))
	}
	return raw
}

// DecodeSafetyCaps reads the safety caps row. A value that does not decode,
// carries unknown fields, or differs from the protocol caps is an error:
// the caps never change (DefaultSafetyCaps).
func DecodeSafetyCaps(raw []byte) (*policyv1.SafetyCaps, error) {
	var caps policyv1.SafetyCaps
	if err := proto.Unmarshal(raw, &caps); err != nil {
		return nil, fmt.Errorf("policy: safety caps do not decode: %w", err)
	}
	if len(caps.ProtoReflect().GetUnknown()) != 0 {
		return nil, errors.New("policy: safety caps carry unknown fields")
	}
	if !proto.Equal(&caps, DefaultSafetyCaps()) {
		return nil, errors.New("policy: stored safety caps differ from the protocol caps")
	}
	return &caps, nil
}

// kv is the module store: the store bound by WithStore, else ctx's store.
func (k Keeper) kv(ctx sdk.Context) storetypes.KVStore {
	if k.store != nil {
		return k.store
	}
	return ctx.KVStore(k.key)
}

// anyUnder reports whether the store holds a row under prefix. It reads at
// most one key.
func anyUnder(store storetypes.KVStore, prefix []byte) (found bool, err error) {
	it := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			found, err = false, fmt.Errorf("policy: close iterator: %w", closeErr)
		}
	}()
	return it.Valid(), nil
}

// iterate visits every row under prefix in key byte order and stops after
// limit rows with an error, so the work is at most limit+1 iterator steps.
// The first visit error stops the walk. visit must not keep the slices.
func iterate(store storetypes.KVStore, prefix byte, limit int, visit func(key, value []byte) error) (err error) {
	start := []byte{prefix}
	it := store.Iterator(start, storetypes.PrefixEndBytes(start))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("policy: close iterator: %w", closeErr)
		}
	}()
	seen := 0
	for ; it.Valid(); it.Next() {
		if seen == limit {
			return fmt.Errorf("policy: more than %d rows under prefix 0x%02x", limit, prefix)
		}
		if visitErr := visit(it.Key(), it.Value()); visitErr != nil {
			return visitErr
		}
		seen++
	}
	// Iterator.Error is not consulted: a cachekv merge iterator reports an
	// exhausted range as an error ("invalid cacheMergeIterator").
	return nil
}

// loadGoverned reads the governed value of key. A value that is not 8 bytes
// or is outside 1..ceiling of a known key is an error.
func loadGoverned(store storetypes.KVStore, key string) (uint64, bool, error) {
	raw := store.Get(GovernedKey(key))
	if raw == nil {
		return 0, false, nil
	}
	value, err := DecodeU64(raw)
	ceiling, known := msgkit.Ceiling(key)
	if err != nil || !known || value < 1 || value > ceiling {
		return 0, false, fmt.Errorf("policy: governed limit %q is not an 8-byte value in 1..ceiling (%d bytes stored)", key, len(raw))
	}
	return value, true, nil
}

// loadPending reads the pending change of key. A value that does not decode
// or whose value is outside 1..ceiling of a known key is an error.
func loadPending(store storetypes.KVStore, key string) (Pending, bool, error) {
	raw := store.Get(PendingKey(key))
	if raw == nil {
		return Pending{}, false, nil
	}
	p, err := DecodePending(raw)
	if err != nil {
		return Pending{}, false, fmt.Errorf("policy: pending change of %q: %w", key, err)
	}
	if ceiling, known := msgkit.Ceiling(key); !known || p.Value > ceiling {
		return Pending{}, false, fmt.Errorf("policy: pending change of %q holds value %d outside 1..ceiling", key, p.Value)
	}
	return p, true, nil
}

// loadPlan reads the plan row.
func loadPlan(store storetypes.KVStore) (Plan, bool, error) {
	raw := store.Get(PlanKey())
	if raw == nil {
		return Plan{}, false, nil
	}
	p, err := DecodePlan(raw)
	if err != nil {
		return Plan{}, false, err
	}
	return p, true, nil
}

// loadAppVersion reads the app version row. Absent is 0; a value that is not
// 8 bytes or is 0 is an error.
func loadAppVersion(store storetypes.KVStore) (uint64, error) {
	raw := store.Get(AppVersionKey())
	if raw == nil {
		return 0, nil
	}
	v, err := DecodeU64(raw)
	if err != nil || v < 1 {
		return 0, fmt.Errorf("policy: app version is not an 8-byte value of at least 1 (%d bytes stored)", len(raw))
	}
	return v, nil
}

// loadApplied reads the applied height of one upgrade name. A value that is
// not 8 bytes or is 0 is an error.
func loadApplied(store storetypes.KVStore, name string) (uint64, bool, error) {
	raw := store.Get(AppliedKey(name))
	if raw == nil {
		return 0, false, nil
	}
	h, err := DecodeU64(raw)
	if err != nil || h < 1 {
		return 0, false, fmt.Errorf("policy: applied upgrade %q is not an 8-byte height of at least 1 (%d bytes stored)", name, len(raw))
	}
	return h, true, nil
}

// loadSafetyCaps reads the safety caps row. Absent is an error: InitGenesis
// writes it.
func loadSafetyCaps(store storetypes.KVStore) (*policyv1.SafetyCaps, error) {
	raw := store.Get(SafetyCapsKey())
	if raw == nil {
		return nil, errors.New("policy: safety caps row missing")
	}
	return DecodeSafetyCaps(raw)
}

// must turns an integrity error of committed state into a panic. The module
// writes only rows it checked, so such an error is damage. A transaction
// handler panic becomes a failed transaction on every node alike, and
// nothing of that transaction is written.
func must[T any](value T, found bool, err error) (T, bool) {
	if err != nil {
		panic(err.Error())
	}
	return value, found
}
