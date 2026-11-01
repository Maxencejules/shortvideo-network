package content

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestRegisterContentChecksInSpecOrder runs one fault per check of
// op_register_content (plus the Enforce and presence prefix), each on a
// fresh store with c-existing already registered, and asserts the code and
// that the store did not change.
func TestRegisterContentChecksInSpecOrder(t *testing.T) {
	type tc struct {
		name  string
		setup func(f *fixture)
		edit  func(f *fixture, m *contentv1.MsgRegisterContent)
		tx    func(ctx sdk.Context) sdk.Context // optional transaction context override
		want  msgkit.Code
	}
	resign := func(f *fixture, m *contentv1.MsgRegisterContent) { f.signRegister(m, DevnetRoleName, f.intent) }
	cases := []tc{
		{name: "signer-19-bytes", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.Signer = m.Signer[:19] }, want: msgkit.Malformed},
		{name: "missing-business-id", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.BusinessId = "" }, want: msgkit.Malformed},
		{name: "missing-content-id", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentId = "" }, want: msgkit.Malformed},
		{name: "missing-manifest-digest", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ManifestDigest = nil }, want: msgkit.Malformed},
		{name: "missing-hosting-intent", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.HostingIntent = nil }, want: msgkit.Malformed},
		{name: "outsider-signer", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.Signer = outsiderAddr; resign(f, m) }, want: msgkit.Unauthorized},
		{name: "provider-signer", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.Signer = providerAddr; resign(f, m) }, want: msgkit.Forbidden},
		{name: "authority-signer", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.Signer = authorityAddr; resign(f, m) }, want: msgkit.Forbidden},
		{name: "content-id-pattern", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentId = "Bad-Id"; resign(f, m) }, want: msgkit.Bounds},
		{name: "boundary-id-65", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentId = strings.Repeat("a", 65); resign(f, m) }, want: msgkit.Bounds},
		{name: "content-version-0", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentVersion = 0; resign(f, m) }, want: msgkit.OutOfRange},
		{name: "content-version-2", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentVersion = 2; resign(f, m) }, want: msgkit.OutOfRange},
		{name: "package-bytes-0", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.PackageBytes = 0; resign(f, m) }, want: msgkit.Bounds},
		{name: "package-bytes-cap-plus-1", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.PackageBytes = genesis.MaxPackageBytes + 1
			resign(f, m)
		}, want: msgkit.Bounds},
		{name: "package-bytes-governed-plus-1", setup: func(f *fixture) { f.limits.values[msgkit.LimitMaxPackageBytes] = 1000 },
			edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.PackageBytes = 1001; resign(f, m) }, want: msgkit.Bounds},
		{name: "manifest-algorithm-unspecified", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.ManifestDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
		}, want: msgkit.Malformed},
		{name: "manifest-algorithm-unknown", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ManifestDigest.Algorithm = 2 }, want: msgkit.Malformed},
		{name: "manifest-31-bytes", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.ManifestDigest.Value = m.ManifestDigest.Value[:31]
		}, want: msgkit.Bounds},
		{name: "manifest-33-bytes", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.ManifestDigest.Value = append(m.ManifestDigest.Value, 0)
		}, want: msgkit.Bounds},
		{name: "intent-domain-unspecified", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.HostingIntent.Domain = 0 }, want: msgkit.Malformed},
		{name: "intent-profile-0", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.HostingIntent.CanonicalProfileVersion = 0 }, want: msgkit.Malformed},
		{name: "intent-profile-2", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.HostingIntent.CanonicalProfileVersion = 2 }, want: msgkit.OutOfRange},
		{name: "intent-key-id-empty", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.HostingIntent.KeyId = "" }, want: msgkit.Malformed},
		{name: "intent-key-id-pattern", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.HostingIntent.KeyId = "Key" }, want: msgkit.Bounds},
		{name: "intent-pubkey-31", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.HostingIntent.PublicKey = m.HostingIntent.PublicKey[:31]
		}, want: msgkit.Bounds},
		{name: "intent-signature-63", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.HostingIntent.Signature = m.HostingIntent.Signature[:63]
		}, want: msgkit.Bounds},
		{name: "host-custody-not-assumed", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { f.signRegister(m, DelegateRoleName, f.intent) }, want: msgkit.Forbidden},
		{name: "intent-wrong-domain", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
		}, want: msgkit.WrongDomain},
		{name: "production-chain-no-dec-001", setup: func(f *fixture) { delete(f.params.roles, HostingDecisionID) }, want: msgkit.DecisionBlocked},
		{name: "dec-001-other-role", setup: func(f *fixture) { f.params.roles[HostingDecisionID] = "CREATOR" }, want: msgkit.DecisionBlocked},
		{name: "intent-bad-signature", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.HostingIntent.Signature[0] ^= 0x01 }, want: msgkit.Unauthorized},
		{name: "intent-other-role", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { f.signRegister(m, "CREATOR", f.intent) }, want: msgkit.Unauthorized},
		{name: "intent-for-another-host", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.Signer = service2Addr
			resign(f, m)
			m.Signer = append([]byte(nil), serviceAddr...)
		}, want: msgkit.Unauthorized},
		{name: "intent-over-other-bytes", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.PackageBytes++ }, want: msgkit.Unauthorized},
		{name: "intent-key-swapped", edit: func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.HostingIntent.PublicKey = append([]byte(nil), seedKey("other").Public().(ed25519.PublicKey)...)
		}, want: msgkit.Unauthorized},
		{name: "business-id-pattern", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.BusinessId = "Bad Business" }, want: msgkit.Bounds},
		{name: "replay-other-payload", setup: func(f *fixture) { f.mustRegister("reg-new", "c-other") }, want: msgkit.Replay},
		{name: "replay-peer-business-id", setup: func(f *fixture) { f.peer["reg-new"] = bytes.Repeat([]byte{7}, 32) }, want: msgkit.Replay},
		{name: "duplicate-id", edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentId = "c-existing"; resign(f, m) }, want: msgkit.DuplicateID},
		{name: "duplicate-id-withdrawn", setup: func(f *fixture) { f.mustWithdraw("wd-existing", "c-existing") },
			edit: func(f *fixture, m *contentv1.MsgRegisterContent) { m.ContentId = "c-existing"; resign(f, m) }, want: msgkit.DuplicateID},
		{name: "gas-limit-0", tx: func(ctx sdk.Context) sdk.Context {
			return ctx.WithGasMeter(storetypes.NewGasMeter(0))
		}, want: msgkit.Bounds},
		{name: "tx-bytes-above-max", tx: func(ctx sdk.Context) sdk.Context {
			return ctx.WithTxBytes(make([]byte, genesis.MaxTxBytes+1))
		}, want: msgkit.Bounds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.mustRegister("reg-existing", "c-existing")
			if c.setup != nil {
				c.setup(f)
			}
			msg := f.registerMsg("reg-new", "c-new")
			if c.edit != nil {
				c.edit(f, msg)
			}
			before := f.snapshot()
			ctx := f.tx()
			if c.tx != nil {
				ctx = c.tx(ctx)
			}
			_, err := f.keeper.RegisterContent(ctx, msg)
			requireCode(t, err, c.want)
			requireSame(t, before, f.snapshot(), c.name)
			noEvents(t, ctx, c.name)
		})
	}
}

// TestRegisterContentEnforceBounds covers the Enforce prefix: gas limit
// above the protocol maximum and an oversized transaction are BOUNDS.
func TestRegisterContentEnforceBounds(t *testing.T) {
	f := newFixture(t)
	before := f.snapshot()
	ctx := f.tx().WithGasMeter(storetypes.NewGasMeter(genesis.MaxGasPerTx + 1))
	_, err := f.keeper.RegisterContent(ctx, f.registerMsg("b1", "c1"))
	requireCode(t, err, msgkit.Bounds)
	ctx = f.tx().WithTxBytes(make([]byte, genesis.MaxTxBytes+1))
	_, err = f.keeper.RegisterContent(ctx, f.registerMsg("b1", "c1"))
	requireCode(t, err, msgkit.Bounds)
	_, err = f.keeper.RegisterContent(f.tx(), nil)
	requireCode(t, err, msgkit.Malformed)
	requireSame(t, before, f.snapshot(), "enforce")
}

// TestRegisterContentMultiFaultOrder pairs two faults and asserts the
// earlier check in the spec order wins.
func TestRegisterContentMultiFaultOrder(t *testing.T) {
	cases := []struct {
		name string
		edit func(f *fixture, m *contentv1.MsgRegisterContent)
		want msgkit.Code
	}{
		{"wrong-role-before-bad-digest", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.Signer = providerAddr
			m.ManifestDigest.Value = m.ManifestDigest.Value[:31]
		}, msgkit.Forbidden},
		{"outsider-before-bad-id", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.Signer = outsiderAddr
			m.ContentId = "BAD"
		}, msgkit.Unauthorized},
		{"bad-id-before-version", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.ContentId = "BAD"
			m.ContentVersion = 2
		}, msgkit.Bounds},
		{"version-before-package-bytes", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.ContentVersion = 2
			m.PackageBytes = 0
		}, msgkit.OutOfRange},
		{"package-bytes-before-digest-algorithm", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.PackageBytes = 0
			m.ManifestDigest.Algorithm = 0
		}, msgkit.Bounds},
		{"digest-before-intent-domain", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.ManifestDigest.Value = m.ManifestDigest.Value[:31]
			m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
		}, msgkit.Bounds},
		{"delegate-before-wrong-domain", func(f *fixture, m *contentv1.MsgRegisterContent) {
			f.signRegister(m, DelegateRoleName, f.intent)
			m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
		}, msgkit.Forbidden},
		{"delegate-before-decision-blocked", func(f *fixture, m *contentv1.MsgRegisterContent) {
			delete(f.params.roles, HostingDecisionID)
			f.signRegister(m, DelegateRoleName, f.intent)
		}, msgkit.Forbidden},
		{"wrong-domain-before-decision-blocked", func(f *fixture, m *contentv1.MsgRegisterContent) {
			delete(f.params.roles, HostingDecisionID)
			m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE
		}, msgkit.WrongDomain},
		{"decision-blocked-before-bad-signature", func(f *fixture, m *contentv1.MsgRegisterContent) {
			delete(f.params.roles, HostingDecisionID)
			m.HostingIntent.Signature[5] ^= 0xff
		}, msgkit.DecisionBlocked},
		{"bad-signature-before-business-bounds", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.HostingIntent.Signature[5] ^= 0xff
			m.BusinessId = "BAD"
		}, msgkit.Unauthorized},
		{"business-bounds-before-duplicate", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.BusinessId = "BAD"
			m.ContentId = "c-existing"
			f.signRegister(m, DevnetRoleName, f.intent)
		}, msgkit.Bounds},
		{"replay-before-duplicate", func(f *fixture, m *contentv1.MsgRegisterContent) {
			m.BusinessId = "reg-existing"
			m.ContentId = "c-existing"
			m.PackageBytes = 1
			f.signRegister(m, DevnetRoleName, f.intent)
		}, msgkit.Replay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.mustRegister("reg-existing", "c-existing")
			msg := f.registerMsg("reg-new", "c-new")
			c.edit(f, msg)
			before := f.snapshot()
			_, err := f.register(msg)
			requireCode(t, err, c.want)
			requireSame(t, before, f.snapshot(), c.name)
		})
	}
}

// TestRegisterContentSuccessBoundaries accepts the edges: a 64-byte id
// (boundary-id-64), package bytes 1, the protocol cap, and a governed
// limit exactly; an invalid governed value falls back to the cap.
func TestRegisterContentSuccessBoundaries(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		id    string
		bytes uint64
		limit uint64
		set   bool
	}{
		{id: strings.Repeat("a", 64), bytes: testPackageBytes},
		{id: "edge-1", bytes: 1},
		{id: "edge-cap", bytes: genesis.MaxPackageBytes},
		{id: "edge-governed", bytes: 1000, limit: 1000, set: true},
		{id: "edge-governed-zero", bytes: genesis.MaxPackageBytes, limit: 0, set: true},
		{id: "edge-governed-above-cap", bytes: genesis.MaxPackageBytes, limit: genesis.MaxPackageBytes + 5, set: true},
	}
	for i, c := range cases {
		delete(f.limits.values, msgkit.LimitMaxPackageBytes)
		if c.set {
			f.limits.values[msgkit.LimitMaxPackageBytes] = c.limit
		}
		msg := f.registerMsg("edge-"+string(rune('a'+i)), c.id)
		msg.PackageBytes = c.bytes
		f.signRegister(msg, DevnetRoleName, f.intent)
		if _, err := f.register(msg); err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if got := f.record(c.id).GetPackageBytes(); got != c.bytes {
			t.Fatalf("%s: package bytes %d", c.id, got)
		}
	}
}

// TestContentRecordFields is REQ-MODU-003: the stored record holds the
// opaque id, version, manifest digest, byte count, submitting host, and
// coordination status, and the event carries the same identity.
func TestContentRecordFields(t *testing.T) {
	f := newFixture(t)
	msg := f.registerMsg("reg-c1", "c1")
	ctx, err := f.register(msg)
	if err != nil {
		t.Fatal(err)
	}
	want := &contentv1.Content{
		ContentId:      "c1",
		ContentVersion: 1,
		ManifestDigest: digestOf(manifestDigest("c1")),
		PackageBytes:   testPackageBytes,
		SubmittingHost: serviceAddr,
		State:          contentv1.ContentState_CONTENT_STATE_PENDING,
	}
	if got := f.record("c1"); !proto.Equal(got, want) {
		t.Fatalf("record = %v, want %v", got, want)
	}
	if _, ok := f.keeper.tombstone(f.ctx, "c1", 1); ok {
		t.Fatal("PENDING content has a tombstone")
	}
	events := typedEvents(t, ctx)
	wantEvent := &eventsv1.ContentRegistered{
		ContentId:      "c1",
		ContentVersion: 1,
		ManifestSha256: msgkit.Hex(manifestDigest("c1")),
		PackageBytes:   "4096",
	}
	if len(events) != 1 || !proto.Equal(events[0], wantEvent) {
		t.Fatalf("events = %v, want [%v]", events, wantEvent)
	}
	if idempotentEvent(ctx) {
		t.Fatal("first registration emitted an idempotent result")
	}
	digest, ok := f.keeper.BusinessDigest(f.ctx, "reg-c1")
	if !ok || len(digest) != 32 {
		t.Fatalf("business id not committed: %x %t", digest, ok)
	}
}

// TestRegisterContentCreatesNoHostingClaim: a registration changes only the
// content store, and in it only the record key and the business id. No
// providers, leases, authority, or evidence key appears; no tombstone.
func TestRegisterContentCreatesNoHostingClaim(t *testing.T) {
	f := newFixtureWith(t, fixtureOptions{extra: []string{"authority", "evidence", "leases", "providers"}})
	others := map[string][]byte{}
	for _, name := range []string{"authority", "evidence", "leases", "providers"} {
		others[name] = msgkit.StoreSnapshot(f.ctx.MultiStore().GetKVStore(f.keys[name]))
	}
	if _, err := f.register(f.registerMsg("reg-c1", "c1")); err != nil {
		t.Fatal(err)
	}
	for name, before := range others {
		requireSame(t, before, msgkit.StoreSnapshot(f.ctx.MultiStore().GetKVStore(f.keys[name])), name)
	}
	var keys [][]byte
	it := f.ctx.MultiStore().GetKVStore(f.keys[ModuleName]).Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		keys = append(keys, append([]byte(nil), it.Key()...))
	}
	it.Close()
	want := [][]byte{recordKey(ContentPrefix, "c1", 1), append([]byte{BusinessPrefix}, "reg-c1"...)}
	if len(keys) != len(want) {
		t.Fatalf("content store keys = %q, want %q", keys, want)
	}
	for i := range want {
		if !bytes.Equal(keys[i], want[i]) {
			t.Fatalf("content store key %d = %q, want %q", i, keys[i], want[i])
		}
	}
	if got := f.record("c1").GetState(); got != contentv1.ContentState_CONTENT_STATE_PENDING {
		t.Fatalf("state %s, want PENDING (PENDING is not PUBLISHED)", got)
	}
}

// TestRegisterContentIdempotent: the same business id with the same payload
// is ABCI success with no write and only the content_result event, even
// though the id now exists.
func TestRegisterContentIdempotent(t *testing.T) {
	f := newFixture(t)
	msg := f.registerMsg("reg-c1", "c1")
	if _, err := f.register(msg); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot()
	ctx, err := f.register(proto.Clone(msg).(*contentv1.MsgRegisterContent))
	requireCode(t, err, msgkit.OK)
	requireSame(t, before, f.snapshot(), "idempotent register")
	if !idempotentEvent(ctx) {
		t.Fatal("no content_result idempotent=true event")
	}
	if n := len(typedEvents(t, ctx)); n != 0 {
		t.Fatalf("idempotent replay emitted %d typed events", n)
	}
	if n := len(ctx.EventManager().ABCIEvents()); n != 1 {
		t.Fatalf("idempotent replay emitted %d events, want 1", n)
	}
	// The same payload under a business id another module committed with
	// the same digest is idempotent too: one namespace.
	digest, _ := f.keeper.BusinessDigest(f.ctx, "reg-c1")
	f.peer["peer-same"] = digest
	msg.BusinessId = "peer-same"
	ctx, err = f.register(msg)
	requireCode(t, err, msgkit.OK)
	if !idempotentEvent(ctx) {
		t.Fatal("peer digest match was not idempotent")
	}
	requireSame(t, before, f.snapshot(), "peer idempotent register")
}

// TestWithdrawalWithoutHook: goal 07 wires no hook; withdrawal works.
func TestWithdrawalWithoutHook(t *testing.T) {
	f := newFixtureWith(t, fixtureOptions{noHook: true})
	f.mustRegister("reg-c1", "c1")
	f.mustWithdraw("wd-c1", "c1")
	if got := f.record("c1").GetState(); got != contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		t.Fatalf("state %s", got)
	}
	if code := f.keeper.ForceWithdrawByCouncil(f.tx(), "c1", 1, reasonDigest("x"), true); code != msgkit.Withdrawn {
		t.Fatalf("force on withdrawn = %s", code)
	}
}

func TestNewKeeperRejectsMissingDependencies(t *testing.T) {
	f := newFixture(t)
	key := f.keys[ModuleName]
	for name, deps := range map[string]Deps{
		"roles":  {Params: f.params, Limits: f.limits},
		"params": {Roles: f.roles, Limits: f.limits},
		"limits": {Roles: f.roles, Params: f.params},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("missing %s did not panic", name)
				}
			}()
			NewKeeper(key, deps)
		}()
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("nil store key did not panic")
			}
		}()
		NewKeeper(nil, Deps{Roles: f.roles, Params: f.params, Limits: f.limits})
	}()
}
