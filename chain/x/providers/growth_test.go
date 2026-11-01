package providers

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// growth is what one provider costs after a number of rotations.
type growth struct {
	recordBytes int
	rotateGas   uint64
	updateGas   uint64
	drainGas    uint64
	disableGas  uint64
}

// measureAfterRotations admits p1, rotates it rotations times, and then
// measures one more rotation, an update, a drain, and a council disable.
// Every key id has the same length, so only the rotation count differs
// between two runs.
func measureAfterRotations(t *testing.T, rotations int) growth {
	t.Helper()
	e := newEnv(t)
	e.admitted("p1")
	keyName := func(i int) string { return fmt.Sprintf("k%05d", i) }
	current := "key-p1"
	for i := 1; i <= rotations; i++ {
		e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", current, keyName(i), uint64(i))), "rotate")
		current = keyName(i)
	}
	var g growth
	g.recordBytes = len(e.store(ModuleName).Get(ProviderKey("p1")))

	ctx := e.fresh()
	e.mustOK(e.rotate(ctx, rotMsg(t, "p1", current, keyName(rotations+1), uint64(rotations)+1)), "measured rotate")
	g.rotateGas = ctx.GasMeter().GasConsumed()
	current = keyName(rotations + 1)

	ctx = e.fresh()
	e.mustOK(e.update(ctx, updMsg("p1", "upd-measure", 2_000_000)), "measured update")
	g.updateGas = ctx.GasMeter().GasConsumed()

	ctx = e.fresh()
	drain := drainMsg(t, "p1", current, uint64(rotations)+2)
	e.mustOK(e.drain(ctx, drain), "measured drain")
	g.drainGas = ctx.GasMeter().GasConsumed()

	ctx = e.fresh()
	if code := e.k.DisableByCouncil(ctx, "p1"); code != msgkit.OK {
		t.Fatalf("disable after %d rotations: %s", rotations, code)
	}
	g.disableGas = ctx.GasMeter().GasConsumed()

	// The history is complete and in order where it is read back.
	res, err := e.k.QueryProvider(e.unmetered(), &providersv1.QueryProviderRequest{ProviderId: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	history := res.GetProvider().GetPreviousSigningKeys()
	if len(history) != rotations+1 {
		t.Fatalf("%d retired keys after %d rotations", len(history), rotations+1)
	}
	if !proto.Equal(history[0], &providersv1.PreviousSigningKey{PublicKey: testPub("key-p1"), Generation: 1, RetiredUnixSeconds: e.ctx.BlockTime().Unix()}) {
		t.Fatalf("first retired key %v", history[0])
	}
	for i, key := range history[1:] {
		if key.GetGeneration() != uint64(i)+2 || string(key.GetPublicKey()) != string(testPub(keyName(i+1))) {
			t.Fatalf("retired key %d: %v", i+1, key)
		}
	}
	state, err := e.k.ExportGenesis(e.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGenesis(state); err != nil {
		t.Fatalf("export after %d rotations: %v", rotations+1, err)
	}
	return g
}

// TestWritesDoNotGrowWithRotations is the regression for a provider that
// rotates until the council can no longer disable it. With retired keys
// inside the record, every rotation added about 45 bytes to the one value
// that a rotation, an update, a drain, and a council disable all read and
// rewrite, at about 33 gas per byte. The retired keys are now rows of
// their own, so the record size and the gas of each of those writes do not
// depend on the rotation count. The slack covers the varint of
// key_generation (one more byte past 127) and nothing else: 300 inline
// retired keys would add about 13,500 bytes and 440,000 gas.
func TestWritesDoNotGrowWithRotations(t *testing.T) {
	few := measureAfterRotations(t, 2)
	many := measureAfterRotations(t, 300)
	if diff := many.recordBytes - few.recordBytes; diff < 0 || diff > 2 {
		t.Fatalf("record is %d bytes after 2 rotations and %d after 300", few.recordBytes, many.recordBytes)
	}
	const slack = 100
	for _, c := range []struct {
		name      string
		few, many uint64
	}{
		{"rotate", few.rotateGas, many.rotateGas},
		{"update", few.updateGas, many.updateGas},
		{"drain", few.drainGas, many.drainGas},
		{"council disable", few.disableGas, many.disableGas},
	} {
		if c.many > c.few+slack {
			t.Fatalf("%s gas %d after 300 rotations, %d after 2", c.name, c.many, c.few)
		}
	}
}

// TestRetiredKeyRowsAreTheHistory: a stored retired-key row that is
// missing, out of order, or does not decode makes the history read fail,
// so Query/Provider and export never return a gap.
func TestRetiredKeyRowsAreTheHistory(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(e *env)
	}{
		{"missing row", func(e *env) { e.store(ModuleName).Delete(RetiredKeyKey("p1", 1)) }},
		{"undecodable row", func(e *env) { e.store(ModuleName).Set(RetiredKeyKey("p1", 1), []byte{0xff, 0xff}) }},
		{"row under another generation", func(e *env) {
			raw := e.store(ModuleName).Get(RetiredKeyKey("p1", 1))
			e.store(ModuleName).Delete(RetiredKeyKey("p1", 1))
			e.store(ModuleName).Set(RetiredKeyKey("p1", 7), raw)
		}},
		{"extra row", func(e *env) {
			e.store(ModuleName).Set(RetiredKeyKey("p1", 2), e.store(ModuleName).Get(RetiredKeyKey("p1", 1)))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.admitted("p1")
			e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate")
			c.mutate(e)
			if _, err := e.k.QueryProvider(e.unmetered(), &providersv1.QueryProviderRequest{ProviderId: "p1"}); err == nil {
				t.Fatal("Query/Provider returned a broken history")
			}
			if _, err := e.k.QueryProviders(e.unmetered(), &providersv1.QueryProvidersRequest{}); err == nil {
				t.Fatal("Query/Providers returned a broken history")
			}
			if _, err := e.k.ExportGenesis(e.unmetered()); err == nil {
				t.Fatal("export returned a broken history")
			}
		})
	}
}

// withRetired is a seeded provider that has rotated n times.
func withRetired(id string, n int) *providersv1.Provider {
	p := seededProvider(id, providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 0)
	p.KeyGeneration = uint64(n) + 1
	for i := 1; i <= n; i++ {
		p.PreviousSigningKeys = append(p.PreviousSigningKeys, &providersv1.PreviousSigningKey{
			PublicKey: testPub(fmt.Sprintf("%s-old-%d", id, i)), Generation: uint64(i), RetiredUnixSeconds: 1_700_000_000,
		})
	}
	return p
}

// TestProvidersPageRetiredKeyBudget: a Providers page stops before the
// provider that would take it above MaxRetiredKeysPerPage retired keys, and
// next_page_key names that provider. The first provider of a page is
// always returned, so a walk advances and returns every provider and every
// retired key exactly once; a provider at MaxRetiredKeysPerProvider fills
// a page on its own.
func TestProvidersPageRetiredKeyBudget(t *testing.T) {
	e := newEnv(t)
	e.seed(
		withRetired("p0", MaxRetiredKeysPerProvider),
		withRetired("p1", 1000),
		withRetired("p2", 30),
		withRetired("p3", 0),
	)
	type page struct {
		ids  []string
		next string
	}
	want := []page{
		{ids: []string{"p0"}, next: "p1"},
		{ids: []string{"p1"}, next: "p2"},
		{ids: []string{"p2", "p3"}, next: ""},
	}
	var key []byte
	total := 0
	for i, w := range want {
		res, err := e.k.QueryProviders(e.unmetered(), &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{PageKey: key}})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, p := range res.GetProviders() {
			ids = append(ids, p.GetProviderId())
			if uint64(len(p.GetPreviousSigningKeys())) != p.GetKeyGeneration()-1 {
				t.Fatalf("page %d: %s carries %d of %d retired keys", i, p.GetProviderId(), len(p.GetPreviousSigningKeys()), p.GetKeyGeneration()-1)
			}
			total += len(p.GetPreviousSigningKeys())
		}
		if fmt.Sprint(ids) != fmt.Sprint(w.ids) || string(res.GetPage().GetNextPageKey()) != w.next {
			t.Fatalf("page %d: %v next %q, want %v next %q", i, ids, res.GetPage().GetNextPageKey(), w.ids, w.next)
		}
		key = res.GetPage().GetNextPageKey()
	}
	if total != MaxRetiredKeysPerProvider+1000+30 {
		t.Fatalf("walk returned %d retired keys", total)
	}
}

// TestCorruptStoredRecord: a stored provider value that does not decode,
// that names another id than its key, or that carries retired keys inline
// is an invariant violation. Every message path and council hook that
// loads it panics (the SDK turns that into a failed transaction), and
// Query/Provider, Query/Providers, and ExportGenesis return an error.
func TestCorruptStoredRecord(t *testing.T) {
	for _, c := range []struct {
		name  string
		value func(e *env) []byte
	}{
		{"undecodable", func(*env) []byte { return []byte{0xff, 0xff, 0xff} }},
		{"names another id", func(e *env) []byte {
			p := proto.Clone(e.record("p1")).(*providersv1.Provider)
			p.ProviderId = "p2"
			raw, err := proto.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}},
		{"retired keys inline", func(e *env) []byte {
			p := proto.Clone(e.record("p1")).(*providersv1.Provider)
			p.PreviousSigningKeys = []*providersv1.PreviousSigningKey{{PublicKey: testPub("x"), Generation: 1, RetiredUnixSeconds: 1}}
			raw, err := proto.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.admitted("p1")
			e.store(ModuleName).Set(ProviderKey("p1"), c.value(e))
			paths := map[string]func(){
				"UpdateProvider": func() { _, _ = e.k.UpdateProvider(e.fresh(), updMsg("p1", "upd-x", 2_000_000)) },
				"RotateProviderKey": func() {
					_, _ = e.k.RotateProviderKey(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1))
				},
				"DrainProvider":    func() { _, _ = e.k.DrainProvider(e.fresh(), drainMsg(t, "p1", "key-p1", 1)) },
				"RegisterProvider": func() { _, _ = e.k.RegisterProvider(e.fresh(), regMsg("p1")) },
				"DisableByCouncil": func() { e.k.DisableByCouncil(e.fresh(), "p1") },
				"AdmitByCouncil":   func() { e.k.AdmitByCouncil(e.fresh(), "p1", true) },
				"Provider":         func() { e.k.Provider(e.fresh(), "p1") },
			}
			for name, call := range paths {
				func() {
					defer func() {
						if recover() == nil {
							t.Errorf("%s did not panic on the corrupt record", name)
						}
					}()
					call()
				}()
			}
			if _, err := e.k.QueryProvider(e.unmetered(), &providersv1.QueryProviderRequest{ProviderId: "p1"}); err == nil {
				t.Error("Query/Provider returned the corrupt record")
			}
			if _, err := e.k.QueryProviders(e.unmetered(), &providersv1.QueryProvidersRequest{}); err == nil {
				t.Error("Query/Providers returned the corrupt record")
			}
			if _, err := e.k.ExportGenesis(e.unmetered()); err == nil {
				t.Error("export returned the corrupt record")
			}
		})
	}
}
