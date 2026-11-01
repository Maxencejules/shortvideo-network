package content

import (
	"bytes"
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	fuzzOpBytes = 6
	fuzzMaxOps  = 24
)

var (
	fuzzSigners   = [][]byte{serviceAddr, service2Addr, providerAddr, outsiderAddr}
	fuzzIDs       = []string{"c0", "c1", "c10", "c2", "BAD"}
	fuzzBusiness  = []string{"b0", "b1", "b2", "b3", "b4", "b5"}
	fuzzGoverned  = uint64(5000)
	fuzzPackageSz = []uint64{1, 4096, 5000, 5001, genesis.MaxPackageBytes, 0}
)

// fuzzState is what the fuzz test reads back after every operation.
type fuzzState struct {
	snapshot   []byte
	contents   map[string]*contentv1.Content
	tombstones map[string]*contentv1.Tombstone
	business   map[string][]byte
}

func readFuzzState(f *fixture) fuzzState {
	s := fuzzState{
		snapshot:   f.snapshot(),
		contents:   map[string]*contentv1.Content{},
		tombstones: map[string]*contentv1.Tombstone{},
		business:   map[string][]byte{},
	}
	for _, c := range f.keeper.allContents(f.ctx) {
		s.contents[c.GetContentId()] = c
	}
	for _, tomb := range f.keeper.allTombstones(f.ctx) {
		s.tombstones[tomb.GetContentId()] = tomb
	}
	for _, id := range fuzzBusiness {
		if d, ok := f.keeper.BusinessDigest(f.ctx, id); ok {
			s.business[id] = d
		}
	}
	return s
}

// contentProgramSeeds are byte-coded programs: one config byte (bit 0:
// DEC-001 devnet role present; bit 1: governed max_package_bytes 5000),
// then 6 bytes per operation: kind (0 register, 1 withdraw, 2 council
// force-withdraw, 3 exact replay of the last committed operation), signer,
// content id, business id, variant (0-3 valid, then one corruption each),
// and an extra byte (package size, version, or devnet flag).
var contentProgramSeeds = [][]byte{
	{
		0x01,
		0, 0, 0, 0, 0, 1, // register c0 by service: success
		3, 0, 0, 0, 0, 0, // exact replay: idempotent
		0, 1, 1, 1, 0, 1, // register c1 by service-2: success
		0, 0, 0, 2, 0, 1, // register c0 again: DUPLICATE_ID
		0, 0, 2, 0, 0, 1, // business b0 with another payload: REPLAY
		1, 0, 0, 3, 0, 0, // withdraw c0 by its host: success
		3, 0, 0, 0, 0, 0, // exact replay of the withdrawal: idempotent
		1, 0, 1, 4, 0, 0, // withdraw c1 by service (host is service-2): FORBIDDEN
		2, 0, 1, 0, 0, 1, // council force-withdraw c1: success
		2, 0, 1, 0, 0, 1, // again: WITHDRAWN
		2, 0, 3, 0, 0, 1, // force-withdraw c2 (absent): NOT_FOUND
		0, 2, 3, 5, 0, 1, // register by a provider: FORBIDDEN
		0, 3, 3, 5, 0, 1, // register by an outsider: UNAUTHORIZED
		0, 0, 3, 5, 4, 1, // content_version 2: OUT_OF_RANGE
		0, 0, 3, 5, 5, 1, // digest algorithm 0: MALFORMED
		0, 0, 3, 5, 6, 1, // 31-byte digest: BOUNDS
		0, 0, 3, 5, 7, 1, // ACCOUNT_HOST_DELEGATE intent: FORBIDDEN
		0, 0, 3, 5, 8, 1, // intent domain PROVIDER_ACCEPT: WRONG_DOMAIN
		0, 0, 3, 5, 9, 1, // corrupted intent signature: UNAUTHORIZED
		0, 0, 3, 5, 0, 4, // register c2 at the package cap: success
		1, 0, 3, 4, 4, 2, // withdraw c2 version 2: STALE_VERSION
		2, 0, 3, 0, 0, 0, // force-withdraw without DEC-003: DECISION_BLOCKED
		2, 0, 3, 0, 11, 1, // force-withdraw with a failing hook: MALFORMED
		2, 0, 3, 0, 4, 1, // force-withdraw version 2: STALE_VERSION
	},
	{0x00, 0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0},                   // no DEC-001: DECISION_BLOCKED twice
	{0x03, 0, 0, 0, 0, 0, 3, 0, 0, 0, 1, 0, 2, 1, 0, 4, 2, 0, 0}, // governed 5000: 5001 BOUNDS, 5000 ok, withdraw BAD id NOT_FOUND
	{0x01, 1, 0, 0, 0, 0, 0, 0, 0, 1, 2, 10, 1},                  // withdraw absent; register with no intent: MALFORMED
}

// FuzzContentTransitions drives bounded byte-coded sequences of
// RegisterContent, WithdrawContent, council force-withdraw, and exact
// replays with valid or corrupted fields, on a real store. After every
// operation it checks:
//
//   - a failed operation leaves the store byte-identical and emits nothing
//   - an idempotent replay leaves the store byte-identical
//   - a success changes exactly one record, as the operation says, and only
//     when its preconditions held (service role, devnet role, owner, state)
//   - records are never deleted; id, version, digest, bytes, and host never
//     change; WITHDRAWN never returns to PENDING
//   - a tombstone exists exactly for WITHDRAWN records and never changes
//   - a committed business digest never changes
//   - the export validates; at the end it re-imports to the same records
func FuzzContentTransitions(f *testing.F) {
	for _, seed := range contentProgramSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, program []byte) {
		runContentProgram(t, program)
	})
}

// programStats counts the outcomes of one program, so a unit test can
// prove the seed corpus reaches every transition.
type programStats struct {
	registered, withdrawn, forced, idempotent int
	rejected                                  map[msgkit.Code]int
}

// runContentProgram runs one byte-coded program and checks every
// invariant listed on FuzzContentTransitions.
func runContentProgram(t *testing.T, program []byte) programStats {
	stats := programStats{rejected: map[msgkit.Code]int{}}
	if len(program) == 0 {
		return stats
	}
	fx := newFixture(t)
	config := program[0]
	if config&0x01 == 0 {
		delete(fx.params.roles, HostingDecisionID)
	}
	if config&0x02 != 0 {
		fx.limits.values[msgkit.LimitMaxPackageBytes] = fuzzGoverned
	}
	program = program[1:]
	var last func() (sdk.Context, msgkit.Code)
	for op := 0; op < fuzzMaxOps && len(program) >= fuzzOpBytes; op++ {
		b := program[:fuzzOpBytes]
		program = program[fuzzOpBytes:]
		before := readFuzzState(fx)
		kind := b[0] % 4
		var (
			run   func() (sdk.Context, msgkit.Code)
			check func(after fuzzState)
		)
		switch kind {
		case 0:
			msg := fuzzRegister(fx, b)
			run = func() (sdk.Context, msgkit.Code) {
				ctx, err := fx.register(proto.Clone(msg).(*contentv1.MsgRegisterContent))
				return ctx, fuzzCode(t, err)
			}
			check = func(after fuzzState) {
				checkRegistered(t, fx, before, after, msg)
				stats.registered++
			}
		case 1:
			msg := fuzzWithdraw(fx, b)
			run = func() (sdk.Context, msgkit.Code) {
				ctx, err := fx.withdraw(proto.Clone(msg).(*contentv1.MsgWithdrawContent))
				return ctx, fuzzCode(t, err)
			}
			check = func(after fuzzState) {
				old := before.contents[msg.GetContentId()]
				if old == nil || !bytes.Equal(old.GetSubmittingHost(), msg.GetSigner()) || fx.params.roles[HostingDecisionID] != DevnetRoleName {
					t.Fatalf("withdraw succeeded without its preconditions: %v", msg)
				}
				checkWithdrawn(t, fx, before, after, msg.GetContentId(), msg.GetReasonDigest().GetValue())
				stats.withdrawn++
			}
		case 2:
			id := fuzzIDs[int(b[2])%len(fuzzIDs)]
			version := uint64(1)
			if b[4]%5 == 4 {
				version = 2
			}
			devnet := b[5]%3 != 0
			reason := reasonDigest("force " + id)
			failHook := b[4]%12 == 11
			run = func() (sdk.Context, msgkit.Code) {
				fx.hook.err = nil
				if failHook {
					fx.hook.err = errPlainHook
				}
				defer func() { fx.hook.err = nil }()
				return fx.force(id, version, reason, devnet)
			}
			check = func(after fuzzState) {
				if !devnet || failHook || version != 1 {
					t.Fatalf("force-withdraw succeeded with devnet=%t failHook=%t version=%d", devnet, failHook, version)
				}
				checkWithdrawn(t, fx, before, after, id, reason)
				stats.forced++
			}
		default:
			if last == nil {
				continue
			}
			run = last
			check = func(fuzzState) {
				t.Fatal("an exact replay of a committed operation changed the store")
			}
		}
		ctx, code := run()
		after := readFuzzState(fx)
		idempotent := idempotentEvent(ctx)
		switch {
		case code != msgkit.OK:
			if !bytes.Equal(before.snapshot, after.snapshot) {
				t.Fatalf("op %d kind %d failed with %s but changed the store", op, kind, code)
			}
			if n := len(ctx.EventManager().ABCIEvents()); n != 0 {
				t.Fatalf("op %d failed with %s but emitted %d events", op, code, n)
			}
			stats.rejected[code]++
		case idempotent || bytes.Equal(before.snapshot, after.snapshot):
			if !bytes.Equal(before.snapshot, after.snapshot) {
				t.Fatalf("op %d idempotent but changed the store", op)
			}
			if !idempotent || len(typedEvents(t, ctx)) != 0 {
				t.Fatalf("op %d: success without a write must be an idempotent replay", op)
			}
			stats.idempotent++
		default:
			check(after)
			if kind != 3 {
				last = run
			}
		}
		checkInvariants(t, fx, before, after)
	}
	// The committed catalogue re-imports to the same records.
	exported := fx.keeper.ExportGenesis(fx.ctx)
	fresh := newFixture(t)
	if err := fresh.keeper.InitGenesis(fresh.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), exported); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if !bytes.Equal(recordPrefixes(fx), recordPrefixes(fresh)) {
		t.Fatal("re-imported records differ")
	}
	return stats
}

func fuzzCode(t *testing.T, err error) msgkit.Code {
	t.Helper()
	if err == nil {
		return msgkit.OK
	}
	return codeFromErr(t, err)
}

func fuzzRegister(fx *fixture, b []byte) *contentv1.MsgRegisterContent {
	id := fuzzIDs[int(b[2])%len(fuzzIDs)]
	msg := fx.registerMsg(fuzzBusiness[int(b[3])%len(fuzzBusiness)], id)
	msg.Signer = append([]byte(nil), fuzzSigners[int(b[1])%len(fuzzSigners)]...)
	msg.PackageBytes = fuzzPackageSz[int(b[5])%len(fuzzPackageSz)]
	fx.signRegister(msg, DevnetRoleName, fx.intent)
	switch b[4] % 12 {
	case 4:
		msg.ContentVersion = 2
		fx.signRegister(msg, DevnetRoleName, fx.intent)
	case 5:
		msg.ManifestDigest.Algorithm = 0
	case 6:
		msg.ManifestDigest.Value = msg.ManifestDigest.Value[:31]
	case 7:
		fx.signRegister(msg, DelegateRoleName, fx.intent)
	case 8:
		msg.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT
	case 9:
		msg.HostingIntent.Signature[b[5]%64] ^= 0x80
	case 10:
		msg.HostingIntent = nil
	case 11:
		msg.ManifestDigest.Value = manifestDigest("other " + id)
		fx.signRegister(msg, DevnetRoleName, fx.intent)
	}
	return msg
}

func fuzzWithdraw(fx *fixture, b []byte) *contentv1.MsgWithdrawContent {
	id := fuzzIDs[int(b[2])%len(fuzzIDs)]
	msg := fx.withdrawMsg(fuzzBusiness[int(b[3])%len(fuzzBusiness)], id)
	msg.Signer = append([]byte(nil), fuzzSigners[int(b[1])%len(fuzzSigners)]...)
	fx.signWithdraw(msg, DevnetRoleName, fx.intent)
	switch b[4] % 12 {
	case 4:
		msg.ContentVersion = uint64(b[5] % 3)
		fx.signWithdraw(msg, DevnetRoleName, fx.intent)
	case 5:
		msg.ReasonDigest = nil
	case 6:
		msg.ReasonDigest.Value = append(msg.ReasonDigest.Value, 1)
	case 7:
		fx.signWithdraw(msg, DelegateRoleName, fx.intent)
	case 8:
		msg.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_INVENTORY
	case 9:
		msg.HostingIntent.Signature[b[5]%64] ^= 0x01
	case 10:
		msg.HostingIntent.CanonicalProfileVersion = 0
	case 11:
		msg.ReasonDigest.Value = reasonDigest("alt " + id)
		fx.signWithdraw(msg, DevnetRoleName, fx.intent)
	}
	return msg
}

// checkRegistered: one new PENDING record equal to the message, and only
// when every precondition held.
func checkRegistered(t *testing.T, fx *fixture, before, after fuzzState, msg *contentv1.MsgRegisterContent) {
	t.Helper()
	limit := msgkit.Effective(msgkit.LimitMaxPackageBytes, fx.limits.values[msgkit.LimitMaxPackageBytes], len(fx.limits.values) > 0)
	if fx.roles.Role(fx.ctx, msg.GetSigner()) != typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE ||
		fx.params.roles[HostingDecisionID] != DevnetRoleName || !msgkit.ValidID(msg.GetContentId()) ||
		msg.GetContentVersion() != 1 || msg.GetPackageBytes() < 1 || msg.GetPackageBytes() > limit ||
		before.contents[msg.GetContentId()] != nil {
		t.Fatalf("register succeeded without its preconditions: %v", msg)
	}
	if len(after.contents) != len(before.contents)+1 {
		t.Fatalf("register changed %d records", len(after.contents)-len(before.contents))
	}
	want := &contentv1.Content{
		ContentId:      msg.GetContentId(),
		ContentVersion: 1,
		ManifestDigest: digestOf(msg.GetManifestDigest().GetValue()),
		PackageBytes:   msg.GetPackageBytes(),
		SubmittingHost: msg.GetSigner(),
		State:          contentv1.ContentState_CONTENT_STATE_PENDING,
	}
	if !proto.Equal(after.contents[msg.GetContentId()], want) {
		t.Fatalf("registered %v, want %v", after.contents[msg.GetContentId()], want)
	}
	if after.business[msg.GetBusinessId()] == nil || before.business[msg.GetBusinessId()] != nil {
		t.Fatal("register did not commit a new business id")
	}
}

// checkWithdrawn: exactly id moved PENDING -> WITHDRAWN with a tombstone
// carrying reason.
func checkWithdrawn(t *testing.T, fx *fixture, before, after fuzzState, id string, reason []byte) {
	t.Helper()
	old := before.contents[id]
	if old == nil || old.GetState() != contentv1.ContentState_CONTENT_STATE_PENDING {
		t.Fatalf("withdrawal of %q succeeded from %v", id, old)
	}
	now := after.contents[id]
	if now.GetState() != contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		t.Fatalf("withdrawal left %q in %s", id, now.GetState())
	}
	tomb := after.tombstones[id]
	if tomb == nil || !bytes.Equal(tomb.GetReasonDigest().GetValue(), reason) ||
		tomb.GetCommittedUnixSeconds() != fx.ctx.BlockTime().Unix() || tomb.GetHistoryErased() {
		t.Fatalf("tombstone %v", tomb)
	}
	for other, c := range before.contents {
		if other != id && !proto.Equal(after.contents[other], c) {
			t.Fatalf("withdrawal of %q changed %q", id, other)
		}
	}
}

func checkInvariants(t *testing.T, fx *fixture, before, after fuzzState) {
	t.Helper()
	for id, old := range before.contents {
		now := after.contents[id]
		if now == nil {
			t.Fatalf("record %q was deleted", id)
		}
		if now.GetContentVersion() != old.GetContentVersion() || !proto.Equal(now.GetManifestDigest(), old.GetManifestDigest()) ||
			now.GetPackageBytes() != old.GetPackageBytes() || !bytes.Equal(now.GetSubmittingHost(), old.GetSubmittingHost()) {
			t.Fatalf("immutable fields of %q changed: %v -> %v", id, old, now)
		}
		if old.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN && now.GetState() != old.GetState() {
			t.Fatalf("%q revived from WITHDRAWN to %s", id, now.GetState())
		}
	}
	for id, tomb := range before.tombstones {
		if !proto.Equal(after.tombstones[id], tomb) {
			t.Fatalf("tombstone of %q changed", id)
		}
	}
	for id, c := range after.contents {
		withdrawn := c.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN
		if withdrawn != (after.tombstones[id] != nil) {
			t.Fatalf("%q state %s, tombstone %t", id, c.GetState(), after.tombstones[id] != nil)
		}
	}
	for id := range after.tombstones {
		if after.contents[id] == nil {
			t.Fatalf("tombstone without record %q", id)
		}
	}
	for id, d := range before.business {
		if !bytes.Equal(after.business[id], d) {
			t.Fatalf("business digest of %q changed", id)
		}
	}
	if err := ValidateGenesis(fx.keeper.ExportGenesis(fx.ctx)); err != nil {
		t.Fatalf("export invalid: %v", err)
	}
}

// FuzzContentGenesisJSON parses arbitrary module genesis JSON. It never
// panics; a catalogue that validates imports into a real store, exports
// proto-equal in key order, and re-imports to the same bytes. A catalogue
// that does not validate writes nothing.
func FuzzContentGenesisJSON(f *testing.F) {
	valid, err := MarshalGenesis(sampleGenesis())
	if err != nil {
		f.Fatal(err)
	}
	empty, _ := MarshalGenesis(&contentv1.GenesisState{})
	f.Add([]byte(valid))
	f.Add([]byte(empty))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"contents":[{"content_id":"c1"}]}`))
	f.Add([]byte(`{"contents":[],"tombstones":[{"content_id":"c1","history_erased":true}]}`))
	f.Add([]byte(`{"contents":[],"unknown":1}`))
	f.Add([]byte(`[`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<16 {
			return
		}
		state, err := ParseGenesis(json.RawMessage(raw))
		if err != nil {
			return
		}
		fx := newFixture(t)
		ctx := fx.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		before := fx.snapshot()
		if verr := ValidateGenesis(state); verr != nil {
			if fx.keeper.InitGenesis(ctx, state) == nil {
				t.Fatal("InitGenesis accepted what ValidateGenesis rejected")
			}
			requireSame(t, before, fx.snapshot(), "rejected genesis")
			return
		}
		if err := fx.keeper.InitGenesis(ctx, state); err != nil {
			t.Fatalf("InitGenesis: %v", err)
		}
		exported := fx.keeper.ExportGenesis(fx.ctx)
		if err := ValidateGenesis(exported); err != nil {
			t.Fatalf("export invalid: %v", err)
		}
		if len(exported.GetContents()) != len(state.GetContents()) || len(exported.GetTombstones()) != len(state.GetTombstones()) {
			t.Fatal("export changed the row count")
		}
		for i := 1; i < len(exported.GetContents()); i++ {
			if exported.GetContents()[i-1].GetContentId() >= exported.GetContents()[i].GetContentId() {
				t.Fatal("export not in key order")
			}
		}
		again := newFixture(t)
		if err := again.keeper.InitGenesis(again.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), exported); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fx.snapshot(), again.snapshot()) {
			t.Fatal("re-import differs")
		}
		if !proto.Equal(again.keeper.ExportGenesis(again.ctx), exported) {
			t.Fatal("second export differs")
		}
	})
}

// TestContentProgramSeedsReachEveryTransition: the seed corpus reaches
// every success path, idempotent replays, and every reject class, so the
// fuzz run starts from programs that exercise the whole module.
func TestContentProgramSeedsReachEveryTransition(t *testing.T) {
	total := programStats{rejected: map[msgkit.Code]int{}}
	for _, seed := range contentProgramSeeds {
		stats := runContentProgram(t, seed)
		total.registered += stats.registered
		total.withdrawn += stats.withdrawn
		total.forced += stats.forced
		total.idempotent += stats.idempotent
		for code, n := range stats.rejected {
			total.rejected[code] += n
		}
	}
	if total.registered < 3 || total.withdrawn < 1 || total.forced < 1 || total.idempotent < 2 {
		t.Fatalf("seed outcomes %+v", total)
	}
	for _, class := range contentRejectClasses {
		if total.rejected[class] == 0 {
			t.Fatalf("no seed reaches %s (outcomes %+v)", class, total.rejected)
		}
	}
}
