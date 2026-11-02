package providers

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// fuzzOps concatenates encoded operations into one fuzz input.
func fuzzOps(ops ...[]byte) []byte {
	var out []byte
	for _, op := range ops {
		out = append(out, op...)
	}
	return out
}

// fuzzBytes reads choices from the fuzz input; past the end it returns 0.
type fuzzBytes struct {
	data []byte
	pos  int
}

func (b *fuzzBytes) more() bool { return b.pos < len(b.data) }

func (b *fuzzBytes) pick(n int) int {
	if b.pos >= len(b.data) {
		return 0
	}
	v := b.data[b.pos]
	b.pos++
	return int(v) % n
}

const fuzzMaxSteps = 40

var (
	fuzzIDs      = []string{"p0", "p1", "p2", "ps", "a" + strings.Repeat("b", 64)}
	fuzzSigners  = [][]byte{providerAcct, provider2Acct, serviceAcct, outsiderAcct, broadcasterAcct}
	fuzzKeys     = []string{"k0", "k1", "k2", "k3"}
	fuzzBusiness = []string{"b0", "b1", "b2", "b3", "b4", "b5", "b6"}
	fuzzCapacity = []uint64{0, 1, 499, 500, 1000, 1 << 40}
	fuzzGroups   = []string{DevnetOperatorGroup, "operator-a"}
)

// legalNext is the provider state machine, plus staying put.
var legalNext = map[providersv1.ProviderState][]providersv1.ProviderState{
	providersv1.ProviderState_PROVIDER_STATE_APPLIED:  {providersv1.ProviderState_PROVIDER_STATE_APPLIED, providersv1.ProviderState_PROVIDER_STATE_ADMITTED},
	providersv1.ProviderState_PROVIDER_STATE_ADMITTED: {providersv1.ProviderState_PROVIDER_STATE_ADMITTED, providersv1.ProviderState_PROVIDER_STATE_DRAINING, providersv1.ProviderState_PROVIDER_STATE_DISABLED},
	providersv1.ProviderState_PROVIDER_STATE_DRAINING: {providersv1.ProviderState_PROVIDER_STATE_DRAINING, providersv1.ProviderState_PROVIDER_STATE_DISABLED},
	providersv1.ProviderState_PROVIDER_STATE_DISABLED: {providersv1.ProviderState_PROVIDER_STATE_DISABLED},
}

// fuzzModel is what the invariants remember across steps.
type fuzzModel struct {
	providers map[string]*providersv1.Provider
	business  map[string][]byte
}

func fuzzSeedProvider() *providersv1.Provider {
	return &providersv1.Provider{
		ProviderId:             "ps",
		Account:                append([]byte(nil), providerAcct...),
		SigningPubkey:          testPub("k0"),
		SigningKeyId:           "k0",
		KeyGeneration:          1,
		EndpointManifestDigest: digestOf("endpoint/ps"),
		State:                  providersv1.ProviderState_PROVIDER_STATE_ADMITTED,
		AdmittedCapacityBytes:  1000,
		ReservedBytes:          500,
		OperatorGroupId:        DevnetOperatorGroup,
	}
}

// fuzzStep builds and runs one operation from the input. It returns the
// operation name, its code, whether it was an idempotent success, and the
// events it emitted.
func fuzzStep(t *testing.T, e *env, b *fuzzBytes) (string, result) {
	op := b.pick(6)
	id := fuzzIDs[b.pick(len(fuzzIDs))]
	signer := fuzzSigners[b.pick(len(fuzzSigners))]
	business := fuzzBusiness[b.pick(len(fuzzBusiness))]
	corrupt := b.pick(12)
	current, found := e.k.Provider(e.unmetered(), id)
	keyName, generation := "k0", uint64(1)
	if found {
		keyName, generation = current.GetSigningKeyId(), current.GetKeyGeneration()
	}
	ctx := e.fresh()
	switch op {
	case 0:
		key := fuzzKeys[b.pick(len(fuzzKeys))]
		msg := &providersv1.MsgRegisterProvider{
			Signer: signer, BusinessId: business, ProviderId: id,
			SigningPubkey: testPub(key), SigningKeyId: key,
			EndpointManifestDigest: digestOf(fmt.Sprintf("endpoint/%s/%d", id, b.pick(3))),
			DeclaredCapacityBytes:  fuzzCapacity[b.pick(len(fuzzCapacity))],
			OperatorGroupId:        fuzzGroups[b.pick(len(fuzzGroups))],
		}
		switch corrupt {
		case 9:
			msg.EndpointManifestDigest.Value = msg.EndpointManifestDigest.Value[:31]
		case 10:
			msg.BusinessId = ""
		case 11:
			msg.SigningPubkey = msg.SigningPubkey[:31]
		}
		return "register", e.register(ctx, msg)
	case 1:
		msg := &providersv1.MsgUpdateProvider{
			Signer: signer, BusinessId: business, ProviderId: id,
			EndpointManifestDigest: digestOf(fmt.Sprintf("endpoint/%s/u%d", id, b.pick(3))),
			DeclaredCapacityBytes:  fuzzCapacity[b.pick(len(fuzzCapacity))],
		}
		switch corrupt {
		case 9:
			msg.EndpointManifestDigest.Algorithm = 0
		case 10:
			msg.BusinessId = ""
		}
		return "update", e.update(ctx, msg)
	case 2:
		next := fuzzKeys[b.pick(len(fuzzKeys))]
		expected := generation + uint64(b.pick(2))
		msg := &providersv1.MsgRotateProviderKey{
			Signer: signer, BusinessId: business, ProviderId: id,
			NewSigningPubkey: testPub(next), NewSigningKeyId: next, ExpectedGeneration: expected,
		}
		signer := keyName
		if corrupt == 8 {
			signer = fuzzKeys[(b.pick(len(fuzzKeys)))]
		}
		signRotate(t, msg, signer)
		switch corrupt {
		case 9:
			msg.CurrentKeySignature.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
		case 10:
			msg.CurrentKeySignature = nil
		case 11:
			msg.CurrentKeySignature.Signature[b.pick(64)] ^= 0x01
		}
		return "rotate", e.rotate(ctx, msg)
	case 3:
		signerKey, gen := keyName, generation
		if corrupt == 8 {
			signerKey = fuzzKeys[b.pick(len(fuzzKeys))]
		}
		if corrupt == 7 {
			gen++
		}
		msg := drainMsg(t, id, signerKey, gen)
		msg.Signer, msg.BusinessId = signer, business
		switch corrupt {
		case 9:
			msg.ProviderSignature.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE
		case 10:
			msg.ProviderSignature = nil
		case 11:
			msg.ProviderSignature.CanonicalProfileVersion = 2
		}
		return "drain", e.drain(ctx, msg)
	case 4:
		devnet := b.pick(4) != 0
		code := e.k.AdmitByCouncil(ctx, id, devnet)
		return "admit", result{code: code, events: ctx.EventManager().ABCIEvents()}
	default:
		code := e.k.DisableByCouncil(ctx, id)
		return "disable", result{code: code, events: ctx.EventManager().ABCIEvents()}
	}
}

// checkFuzzInvariants compares the store with the model after one step.
func checkFuzzInvariants(t *testing.T, e *env, m *fuzzModel, op string, r result, before map[string][]byte) {
	t.Helper()
	after := e.snapshot()
	changed := false
	for name := range before {
		if !bytes.Equal(before[name], after[name]) {
			if name != ModuleName {
				t.Fatalf("%s changed store %q", op, name)
			}
			changed = true
		}
	}
	switch {
	case r.code != msgkit.OK:
		if changed {
			t.Fatalf("%s failed with %s and changed the store", op, r.code)
		}
		if len(r.events) != 0 {
			t.Fatalf("%s failed with %s and emitted events", op, r.code)
		}
	case r.idempotent:
		if changed {
			t.Fatalf("idempotent %s changed the store", op)
		}
		if len(r.events) != 1 || len(r.typed()) != 0 {
			t.Fatalf("idempotent %s events %v", op, r.events)
		}
	default:
		for _, ev := range r.typed() {
			if _, err := msgkit.ParseTyped(ev); err != nil {
				t.Fatalf("%s event %s: %v", op, ev.Type, err)
			}
		}
		if (op == "update") != (len(r.typed()) == 0) {
			t.Fatalf("%s success emitted %d typed events", op, len(r.typed()))
		}
	}

	state, err := e.k.ExportGenesis(e.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGenesis(state); err != nil {
		t.Fatalf("after %s the export is invalid: %v", op, err)
	}
	seen := map[string]bool{}
	for _, p := range state.GetProviders() {
		id := p.GetProviderId()
		seen[id] = true
		previous, known := m.providers[id]
		if !known {
			if op != "register" || r.code != msgkit.OK {
				t.Fatalf("provider %s appeared after %s %s", id, op, r.code)
			}
			if p.GetState() != providersv1.ProviderState_PROVIDER_STATE_APPLIED || p.GetKeyGeneration() != 1 || p.GetReservedBytes() != 0 {
				t.Fatalf("new provider %v", p)
			}
			m.providers[id] = p
			continue
		}
		if !bytes.Equal(previous.GetAccount(), p.GetAccount()) {
			t.Fatalf("%s changed the account of %s", op, id)
		}
		if previous.GetOperatorGroupId() != p.GetOperatorGroupId() {
			t.Fatalf("%s changed the operator group of %s", op, id)
		}
		if previous.GetReservedBytes() != p.GetReservedBytes() {
			t.Fatalf("%s changed the reserved bytes of %s", op, id)
		}
		legal := false
		for _, next := range legalNext[previous.GetState()] {
			legal = legal || next == p.GetState()
		}
		if !legal {
			t.Fatalf("%s moved %s from %s to %s", op, id, previous.GetState(), p.GetState())
		}
		if p.GetKeyGeneration() < previous.GetKeyGeneration() || p.GetKeyGeneration() > previous.GetKeyGeneration()+1 {
			t.Fatalf("%s moved the generation of %s from %d to %d", op, id, previous.GetKeyGeneration(), p.GetKeyGeneration())
		}
		for i, key := range previous.GetPreviousSigningKeys() {
			if !proto.Equal(key, p.GetPreviousSigningKeys()[i]) {
				t.Fatalf("%s rewrote retired key %d of %s", op, i, id)
			}
		}
		m.providers[id] = p
	}
	for id := range m.providers {
		if !seen[id] {
			t.Fatalf("%s deleted provider %s", op, id)
		}
	}

	ledger := map[string][]byte{}
	it := e.store(ModuleName).Iterator([]byte{BusinessKeyPrefix}, []byte{BusinessKeyPrefix + 1})
	for ; it.Valid(); it.Next() {
		ledger[string(it.Key()[1:])] = append([]byte(nil), it.Value()...)
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	added := 0
	for id, digest := range ledger {
		previous, known := m.business[id]
		if !known {
			added++
			m.business[id] = digest
			continue
		}
		if !bytes.Equal(previous, digest) {
			t.Fatalf("%s changed the digest of business id %s", op, id)
		}
	}
	if len(ledger) < len(m.business) {
		t.Fatalf("%s deleted a business id", op)
	}
	councilOp := op == "admit" || op == "disable"
	wantAdded := 0
	if r.code == msgkit.OK && !r.idempotent && !councilOp {
		wantAdded = 1
	}
	if added != wantAdded {
		t.Fatalf("%s %s added %d business ids, want %d", op, r.code, added, wantAdded)
	}
}

// FuzzProviderTransitions runs bounded byte-driven sequences of register,
// update, rotate, drain, council admit, and council disable, with valid and
// corrupted fields, and checks after every step: a failed step leaves every
// store byte-identical and emits nothing; an idempotent step writes nothing
// and emits only the result event; only the providers store changes;
// generation == 1 + retired keys and retired keys are never rewritten;
// states move only along the state machine; account, group, and reserved
// bytes never change; no provider and no business id is deleted or
// rewritten; and the export is valid genesis. At the end the export
// re-imports into a fresh store with byte-identical provider records.
func FuzzProviderTransitions(f *testing.F) {
	// Each operation reads: op, id, signer, business id, corruption, then
	// its own choices (see fuzzStep).
	f.Add([]byte{})
	// p0: register, admit, rotate k0->k1, drain, update, disable, then an
	// idempotent register retry.
	f.Add(fuzzOps(
		[]byte{0, 0, 0, 0, 0, 0, 0, 4, 0},
		[]byte{4, 0, 0, 1, 0, 1},
		[]byte{2, 0, 0, 2, 0, 1, 0},
		[]byte{3, 0, 0, 3, 0},
		[]byte{1, 0, 0, 4, 0, 0, 3},
		[]byte{5, 0, 0, 0, 0},
		[]byte{0, 0, 0, 0, 0, 0, 0, 4, 0},
	))
	// ps (reserved 500): update below, at, cross-owner, replay; rotate,
	// stale retry; drain; admit a draining provider; disable.
	f.Add(fuzzOps(
		[]byte{1, 3, 0, 0, 0, 0, 2},
		[]byte{1, 3, 0, 1, 0, 0, 3},
		[]byte{1, 3, 1, 2, 0, 0, 4},
		[]byte{1, 3, 0, 1, 0, 1, 4},
		[]byte{2, 3, 0, 3, 0, 2, 0},
		[]byte{2, 3, 0, 3, 0, 2, 0},
		[]byte{3, 3, 0, 4, 0},
		[]byte{4, 3, 0, 5, 0, 1},
		[]byte{5, 3, 0, 6, 0},
	))
	// Corruptions and the role, owner, decision, and signature rejects.
	f.Add(fuzzOps(
		[]byte{0, 1, 0, 0, 9, 0, 0, 4, 0},
		[]byte{0, 1, 0, 0, 0, 1, 0, 4, 0},
		[]byte{4, 1, 0, 1, 0, 0},
		[]byte{4, 1, 0, 1, 0, 1},
		[]byte{2, 1, 0, 2, 8, 2, 0, 3},
		[]byte{2, 1, 0, 2, 9, 2, 0},
		[]byte{3, 1, 0, 3, 7},
		[]byte{3, 1, 4, 3, 0},
		[]byte{3, 4, 0, 3, 0},
		[]byte{0, 4, 0, 5, 0, 0, 0, 4, 0},
		[]byte{0, 2, 2, 6, 0, 0, 0, 4, 0},
		[]byte{0, 2, 3, 6, 0, 0, 0, 4, 0},
	))
	f.Add(bytes.Repeat([]byte{2, 3, 0, 0, 0, 1, 1}, 6))
	f.Fuzz(func(t *testing.T, data []byte) {
		e := newEnv(t)
		e.seed(fuzzSeedProvider())
		m := &fuzzModel{
			providers: map[string]*providersv1.Provider{"ps": fuzzSeedProvider()},
			business:  map[string][]byte{},
		}
		b := &fuzzBytes{data: data}
		for step := 0; step < fuzzMaxSteps && b.more(); step++ {
			before := e.snapshot()
			op, r := fuzzStep(t, e, b)
			checkFuzzInvariants(t, e, m, op, r, before)
		}
		state, err := e.k.ExportGenesis(e.unmetered())
		if err != nil {
			t.Fatal(err)
		}
		fresh := newEnv(t)
		if err := fresh.k.InitGenesis(fresh.unmetered(), state); err != nil {
			t.Fatalf("re-import: %v", err)
		}
		if !bytes.Equal(prefixSnapshot(e, ProviderKeyPrefix), prefixSnapshot(fresh, ProviderKeyPrefix)) {
			t.Fatal("re-imported provider records differ")
		}
	})
}

// FuzzProvidersGenesisJSON parses arbitrary module genesis JSON. It never
// panics. A document that fails ValidateGenesis is refused by InitGenesis
// without a write. A valid one imports, exports proto.Equal to the
// id-sorted input, survives the JSON encoding, and re-imports into a
// byte-identical store.
func FuzzProvidersGenesisJSON(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"providers":[]}`))
	f.Add(genesisJSON(f, fuzzSeedProvider()))
	f.Add(genesisJSON(f, rotatedProvider("pz"), seededProvider("pa", providersv1.ProviderState_PROVIDER_STATE_APPLIED, 5, 0)))
	f.Add([]byte(`{"providers":[{"provider_id":"p1","account":"ERERERERERERERERERERERERERE=","key_generation":"1"}]}`))
	f.Add([]byte(`{"providers":[{"provider_id":"P1"}],"extra":true}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		state, err := ParseGenesis(raw)
		if err != nil {
			return
		}
		if err := ValidateGenesis(state); err != nil {
			e := newEnv(t)
			if e.k.InitGenesis(e.unmetered(), state) == nil {
				t.Fatal("InitGenesis accepted a document ValidateGenesis rejects")
			}
			if len(msgkit.StoreSnapshot(e.store(ModuleName))) != 0 {
				t.Fatal("refused InitGenesis wrote")
			}
			return
		}
		e := newEnv(t)
		if err := e.k.InitGenesis(e.unmetered(), state); err != nil {
			t.Fatalf("valid document refused: %v", err)
		}
		exported, err := e.k.ExportGenesis(e.unmetered())
		if err != nil {
			t.Fatal(err)
		}
		sorted := &providersv1.GenesisState{Providers: append([]*providersv1.Provider(nil), state.GetProviders()...)}
		sort.Slice(sorted.Providers, func(i, j int) bool {
			return sorted.Providers[i].GetProviderId() < sorted.Providers[j].GetProviderId()
		})
		if !proto.Equal(exported, sorted) {
			t.Fatal("export differs from the sorted input")
		}
		encoded, err := MarshalGenesis(exported)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := ParseGenesis(encoded)
		if err != nil || !proto.Equal(decoded, exported) {
			t.Fatalf("JSON round trip: %v", err)
		}
		again := newEnv(t)
		if err := again.k.InitGenesis(again.unmetered(), decoded); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(msgkit.StoreSnapshot(e.store(ModuleName)), msgkit.StoreSnapshot(again.store(ModuleName))) {
			t.Fatal("re-import is not byte-identical")
		}
	})
}
