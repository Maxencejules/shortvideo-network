package policy_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
)

// TestStoreLayout: genesis and InitVersions write exactly the layout key
// (version 2), the safety caps, the app version 1, and one version map row
// per module; the key builders and value sizes match the README table.
func TestStoreLayout(t *testing.T) {
	e := newEnv(t)
	want := map[string][]byte{
		string(msgkit.LayoutVersionKey): policy.EncodeU64(2),
		string([]byte{0x01}):            policy.EncodeSafetyCaps(policy.DefaultSafetyCaps()),
		string([]byte{0x04}):            policy.EncodeU64(1),
	}
	for name, v := range testVersionMap() {
		want[string(append([]byte{0x05}, name...))] = policy.EncodeU64(v)
	}
	it := e.store().Iterator(nil, nil)
	got := map[string][]byte{}
	for ; it.Valid(); it.Next() {
		got[string(it.Key())] = append([]byte(nil), it.Value()...)
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("store holds %d rows, want %d", len(got), len(want))
	}
	for key, value := range want {
		if !bytes.Equal(got[key], value) {
			t.Fatalf("row %x = %x, want %x", key, got[key], value)
		}
	}
	keys := map[string][]byte{
		"governed": policy.GovernedKey("max_page_limit"),
		"pending":  policy.PendingKey("max_page_limit"),
		"module":   policy.VersionMapKey("leases"),
		"applied":  policy.AppliedKey("rehearsal-1"),
		"caps":     policy.SafetyCapsKey(),
		"app":      policy.AppVersionKey(),
		"plan":     policy.PlanKey(),
	}
	wantKeys := map[string][]byte{
		"governed": append([]byte{0x02}, "max_page_limit"...),
		"pending":  append([]byte{0x03}, "max_page_limit"...),
		"module":   append([]byte{0x05}, "leases"...),
		"applied":  append([]byte{0x07}, "rehearsal-1"...),
		"caps":     {0x01},
		"app":      {0x04},
		"plan":     {0x06},
	}
	for name, key := range keys {
		if !bytes.Equal(key, wantKeys[name]) {
			t.Fatalf("%s key %x, want %x", name, key, wantKeys[name])
		}
	}
	if policy.PendingValueBytes != 88 || policy.PlanValueBytes != 168 {
		t.Fatalf("value sizes %d %d", policy.PendingValueBytes, policy.PlanValueBytes)
	}
	if policy.ConsensusVersion != 2 || policy.ModuleName != "policy" {
		t.Fatalf("module %s version %d", policy.ModuleName, policy.ConsensusVersion)
	}
	e.mustCheck()
}

// TestStoreCodecs: the pending and plan codecs round-trip, have a constant
// size whatever the id length, and refuse every malformed value.
func TestStoreCodecs(t *testing.T) {
	long := strings.Repeat("a", msgkit.IDMaxBytes)
	for _, id := range []string{"p", "proposal-1", long} {
		p := policy.Pending{Activation: 9, Value: 8, PolicyVersion: 3, ProposalID: id}
		raw := policy.EncodePending(p)
		if len(raw) != policy.PendingValueBytes {
			t.Fatalf("pending value %d bytes", len(raw))
		}
		back, err := policy.DecodePending(raw)
		if err != nil || back != p {
			t.Fatalf("pending round trip %+v %v", back, err)
		}
		plan := policy.Plan{Name: id, Height: 300, Digest: digestOf(id), ProposalID: "u1"}
		rawPlan := policy.EncodePlan(plan)
		if len(rawPlan) != policy.PlanValueBytes {
			t.Fatalf("plan value %d bytes", len(rawPlan))
		}
		backPlan, err := policy.DecodePlan(rawPlan)
		if err != nil || !equalPlan(backPlan, plan) {
			t.Fatalf("plan round trip %+v %v", backPlan, err)
		}
	}
	good := policy.EncodePending(policy.Pending{Activation: 9, Value: 8, PolicyVersion: 3, ProposalID: "p1"})
	badPending := map[string][]byte{
		"short":            good[:len(good)-1],
		"long":             append(append([]byte(nil), good...), 0),
		"activation 0":     overwrite(good, 0, policy.EncodeU64(0)),
		"value 0":          overwrite(good, 8, policy.EncodeU64(0)),
		"policy version 1": overwrite(good, 16, policy.EncodeU64(1)),
		"empty id":         overwrite(good, 24, make([]byte, msgkit.IDMaxBytes)),
		"bytes after pad":  overwrite(good, len(good)-1, []byte{'x'}),
		"id pattern":       overwrite(good, 24, []byte("P1")),
	}
	for name, raw := range badPending {
		if _, err := policy.DecodePending(raw); err == nil {
			t.Fatalf("pending %s decoded", name)
		}
	}
	plan := policy.EncodePlan(policy.Plan{Name: "up", Height: 5, Digest: digestOf("d"), ProposalID: "p1"})
	nameAt := 8 + msgkit.DigestBytes
	badPlan := map[string][]byte{
		"short":           plan[:len(plan)-1],
		"height 0":        overwrite(plan, 0, policy.EncodeU64(0)),
		"empty name":      overwrite(plan, nameAt, make([]byte, msgkit.IDMaxBytes)),
		"name pattern":    overwrite(plan, nameAt, []byte("_x")),
		"empty proposal":  overwrite(plan, nameAt+msgkit.IDMaxBytes, make([]byte, msgkit.IDMaxBytes)),
		"bytes after pad": overwrite(plan, nameAt+msgkit.IDMaxBytes-1, []byte{'z'}),
	}
	for name, raw := range badPlan {
		if _, err := policy.DecodePlan(raw); err == nil {
			t.Fatalf("plan %s decoded", name)
		}
	}
	for _, raw := range [][]byte{nil, {1}, make([]byte, 9)} {
		if _, err := policy.DecodeU64(raw); err == nil {
			t.Fatalf("scalar of %d bytes decoded", len(raw))
		}
	}
	mustPanic(t, "outside the id pattern", func() {
		policy.EncodePending(policy.Pending{Activation: 1, Value: 1, PolicyVersion: 2, ProposalID: long + "a"})
	})
	mustPanic(t, "digest of 31 bytes", func() {
		policy.EncodePlan(policy.Plan{Name: "up", Height: 1, Digest: make([]byte, 31), ProposalID: "p"})
	})
	caps := policy.EncodeSafetyCaps(policy.DefaultSafetyCaps())
	if _, err := policy.DecodeSafetyCaps(caps); err != nil {
		t.Fatal(err)
	}
	other := policy.DefaultSafetyCaps()
	other.SafetyMaxTxBytes--
	unknown := append(append([]byte(nil), caps...), 0xf8, 0x01, 0x01) // field 31, varint 1
	for name, raw := range map[string][]byte{
		"changed":  policy.EncodeSafetyCaps(other),
		"garbage":  {0xff},
		"unknown":  unknown,
		"no field": {},
	} {
		if _, err := policy.DecodeSafetyCaps(raw); err == nil {
			t.Fatalf("safety caps %s decoded", name)
		}
	}
}

func overwrite(raw []byte, at int, with []byte) []byte {
	out := append([]byte(nil), raw...)
	copy(out[at:], with)
	return out
}

// TestVersionMapCodec: VersionMap reads the rows InitVersions wrote in
// module order; an absent map is empty; a damaged row or too many rows
// panics.
func TestVersionMapCodec(t *testing.T) {
	e := newEnv(t)
	vm := e.k.VersionMap(e.ctx)
	if len(vm) != len(testVersionMap()) {
		t.Fatalf("version map %v", vm)
	}
	for name, v := range testVersionMap() {
		if vm[name] != v {
			t.Fatalf("version map %v", vm)
		}
	}
	if b := newBareEnv(t); len(b.k.VersionMap(b.ctx)) != 0 {
		t.Fatal("empty store has a version map")
	}
	damage := map[string]func(*env){
		"3-byte value":  func(d *env) { d.store().Set(policy.VersionMapKey("leases"), []byte{1, 2, 3}) },
		"version 0":     func(d *env) { d.store().Set(policy.VersionMapKey("leases"), policy.EncodeU64(0)) },
		"name pattern":  func(d *env) { d.store().Set(policy.VersionMapKey("Leases"), policy.EncodeU64(2)) },
		"too many rows": func(d *env) { fillModules(d, policy.MaxVersionMapModules+1) },
	}
	for name, apply := range damage {
		t.Run(name, func(t *testing.T) {
			d := newEnv(t)
			apply(d)
			mustPanic(t, "policy:", func() { d.k.VersionMap(d.ctx) })
		})
	}
	t.Run("at the bound", func(t *testing.T) {
		d := newEnv(t)
		fillModules(d, policy.MaxVersionMapModules)
		if got := d.k.VersionMap(d.ctx); len(got) != policy.MaxVersionMapModules {
			t.Fatalf("%d modules", len(got))
		}
	})
}

// fillModules makes the version map hold exactly n rows.
func fillModules(e *env, n int) {
	for _, name := range keysOf(e.k.VersionMap(e.ctx)) {
		e.store().Delete(policy.VersionMapKey(name))
	}
	for i := 0; i < n; i++ {
		e.store().Set(policy.VersionMapKey(fmt.Sprintf("m%02d", i)), policy.EncodeU64(1))
	}
}

func keysOf(vm module.VersionMap) []string {
	out := make([]string, 0, len(vm))
	for name := range vm {
		out = append(out, name)
	}
	return out
}

// FuzzVersionMapDecode: arbitrary version map rows either make VersionMap
// panic with a policy message, or read back as exactly the rows written.
func FuzzVersionMapDecode(f *testing.F) {
	f.Add("leases", policy.EncodeU64(2), "svn", policy.EncodeU64(1))
	f.Add("", []byte{}, "x", []byte{1})
	f.Add("a.b-c_d", policy.EncodeU64(0), "Z", policy.EncodeU64(1<<63))
	f.Fuzz(func(t *testing.T, n1 string, v1 []byte, n2 string, v2 []byte) {
		e := newBareEnv(t)
		rows := map[string][]byte{}
		for _, row := range []struct {
			name  string
			value []byte
		}{{n1, v1}, {n2, v2}} {
			if row.name == "" || len(row.value) == 0 {
				continue
			}
			e.store().Set(policy.VersionMapKey(row.name), row.value)
			rows[row.name] = row.value
		}
		var vm module.VersionMap
		panicked := ""
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = fmt.Sprint(r)
				}
			}()
			vm = e.k.VersionMap(e.ctx)
		}()
		if panicked != "" {
			if !strings.HasPrefix(panicked, "policy: ") {
				t.Fatalf("panic %q", panicked)
			}
			return
		}
		if len(vm) != len(rows) {
			t.Fatalf("read %d rows, wrote %d", len(vm), len(rows))
		}
		for name, raw := range rows {
			if !msgkit.ValidID(name) || len(raw) != 8 || binary.BigEndian.Uint64(raw) != vm[name] || vm[name] < 1 {
				t.Fatalf("row %q=%x read as %d", name, raw, vm[name])
			}
		}
	})
}

// FuzzStoreCodecs: DecodePending and DecodePlan never panic, and a value
// they accept re-encodes to the same bytes.
func FuzzStoreCodecs(f *testing.F) {
	f.Add(policy.EncodePending(policy.Pending{Activation: 9, Value: 8, PolicyVersion: 3, ProposalID: "p1"}))
	f.Add(policy.EncodePlan(policy.Plan{Name: "up", Height: 5, Digest: digestOf("d"), ProposalID: "p1"}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if p, err := policy.DecodePending(raw); err == nil && !bytes.Equal(policy.EncodePending(p), raw) {
			t.Fatalf("pending %x re-encodes differently", raw)
		}
		if p, err := policy.DecodePlan(raw); err == nil && !bytes.Equal(policy.EncodePlan(p), raw) {
			t.Fatalf("plan %x re-encodes differently", raw)
		}
		if caps, err := policy.DecodeSafetyCaps(raw); err == nil && !proto.Equal(caps, policy.DefaultSafetyCaps()) {
			t.Fatalf("caps %x decoded to other caps", raw)
		}
	})
}

// TestDefaultSafetyCapsMatchGenesis: the protocol message carries the
// genesis package caps field for field.
func TestDefaultSafetyCapsMatchGenesis(t *testing.T) {
	caps := policy.DefaultSafetyCaps()
	g := genesis.DefaultSafetyCaps()
	got := genesis.SafetyCaps{
		SafetyMaxPackageBytes:          caps.GetSafetyMaxPackageBytes(),
		SafetyMaxEvidenceBatch:         caps.GetSafetyMaxEvidenceBatch(),
		SafetyMaxExpiryStepsPerBlock:   caps.GetSafetyMaxExpiryStepsPerBlock(),
		SafetyMaxPageLimit:             caps.GetSafetyMaxPageLimit(),
		SafetyMaxTxBytes:               caps.GetSafetyMaxTxBytes(),
		SafetyMaxGasPerTx:              caps.GetSafetyMaxGasPerTx(),
		SafetyMaxOpenOffersPerProvider: caps.GetSafetyMaxOpenOffersPerProvider(),
	}
	if got != g {
		t.Fatalf("caps %+v, want %+v", got, g)
	}
	if fields := (&policyv1.SafetyCaps{}).ProtoReflect().Descriptor().Fields().Len(); fields != 7 {
		t.Fatalf("SafetyCaps has %d fields", fields)
	}
}
