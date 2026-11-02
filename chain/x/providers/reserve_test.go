package providers

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// The capacity accounting x/leases calls: ReserveProbe, Reserve, and
// Release (spec_machine.py reserve_probe :677, reserve :286, release :296).

var allProviderStates = []providersv1.ProviderState{
	providersv1.ProviderState_PROVIDER_STATE_APPLIED,
	providersv1.ProviderState_PROVIDER_STATE_ADMITTED,
	providersv1.ProviderState_PROVIDER_STATE_DRAINING,
	providersv1.ProviderState_PROVIDER_STATE_DISABLED,
}

// storeRows is every key/value pair of the providers store.
func storeRows(e *env) map[string][]byte {
	out := map[string][]byte{}
	it := e.store(ModuleName).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = append([]byte(nil), it.Value()...)
	}
	return out
}

// onlyRecordChanged returns an error unless after differs from before in
// exactly the record of id, and that record differs only in reserved_bytes,
// which is now want. Retired-key rows, business ids, the layout key, and
// every other provider stay byte-identical; no key is added or removed.
func onlyRecordChanged(before, after map[string][]byte, id string, want uint64) error {
	key := string(ProviderKey(id))
	if len(before) != len(after) {
		return fmt.Errorf("store had %d keys, now %d", len(before), len(after))
	}
	for k, v := range before {
		w, ok := after[k]
		if !ok {
			return fmt.Errorf("key %x removed", k)
		}
		if k != key && !bytes.Equal(v, w) {
			return fmt.Errorf("key %x changed", k)
		}
	}
	old, err := decodeProvider([]byte(key), before[key])
	if err != nil {
		return err
	}
	now, err := decodeProvider([]byte(key), after[key])
	if err != nil {
		return err
	}
	if now.GetReservedBytes() != want {
		return fmt.Errorf("reserved_bytes %d, want %d", now.GetReservedBytes(), want)
	}
	now.ReservedBytes = old.GetReservedBytes()
	if !proto.Equal(old, now) {
		return fmt.Errorf("a field other than reserved_bytes changed: %v -> %v", old, now)
	}
	return nil
}

// accountingProvider is a seeded record in state with two retired keys, so
// a test sees that accounting never touches the retired-key rows.
func accountingProvider(id string, state providersv1.ProviderState, capacity, reserved uint64) *providersv1.Provider {
	p := rotatedProvider(id)
	p.State = state
	p.AdmittedCapacityBytes = capacity
	p.ReservedBytes = reserved
	return p
}

// TestReserveReleaseAccounting: exact boundaries of the three calls, in
// every provider state. Every reject leaves every mounted store
// byte-identical and emits nothing; ReserveProbe never writes and returns
// the code Reserve returns on the same state; a successful Reserve or
// Release changes only reserved_bytes of the one record.
func TestReserveReleaseAccounting(t *testing.T) {
	const top = uint64(math.MaxUint64)
	type row struct {
		name               string
		id                 string // "p1" is the seeded provider
		capacity, reserved uint64
		amount             uint64
		want               msgkit.Code
		after              uint64 // reserved_bytes after a success
	}
	reserves := []row{
		{"amount 0", "p1", 1000, 400, 0, msgkit.Malformed, 0},
		{"amount 0 before lookup", "p9", 1000, 400, 0, msgkit.Malformed, 0},
		{"absent provider", "p9", 1000, 400, 1, msgkit.NotFound, 0},
		{"id outside the pattern", "P1", 1000, 400, 1, msgkit.NotFound, 0},
		{"empty id", "", 1000, 400, 1, msgkit.NotFound, 0},
		{"overflow by one", "p1", top, top - 5, 6, msgkit.Overflow, 0},
		{"up to MaxUint64", "p1", top, top - 5, 5, msgkit.OK, top},
		{"overflow when full", "p1", top, top, 1, msgkit.Overflow, 0},
		{"whole range at once", "p1", top, 0, top, msgkit.OK, top},
		{"whole range plus one reserved", "p1", top, 1, top, msgkit.Overflow, 0},
		{"overflow before capacity", "p1", top - 5, top - 5, 6, msgkit.Overflow, 0},
		{"capacity at the boundary of overflow", "p1", top - 5, top - 5, 1, msgkit.Capacity, 0},
		{"capacity by one", "p1", 1000, 400, 601, msgkit.Capacity, 0},
		{"up to capacity", "p1", 1000, 400, 600, msgkit.OK, 1000},
		{"capacity when full", "p1", 1000, 1000, 1, msgkit.Capacity, 0},
		{"one byte", "p1", 1000, 0, 1, msgkit.OK, 1},
	}
	releases := []row{
		{"amount 0", "p1", 1000, 400, 0, msgkit.Malformed, 0},
		{"amount 0 with nothing reserved", "p1", 1000, 0, 0, msgkit.Malformed, 0},
		{"amount 0 before lookup", "p9", 1000, 400, 0, msgkit.Malformed, 0},
		{"absent provider", "p9", 1000, 400, 1, msgkit.NotFound, 0},
		{"id outside the pattern", "P1", 1000, 400, 1, msgkit.NotFound, 0},
		{"underflow by one", "p1", 1000, 400, 401, msgkit.Underflow, 0},
		{"down to zero", "p1", 1000, 400, 400, msgkit.OK, 0},
		{"part", "p1", 1000, 400, 1, msgkit.OK, 399},
		{"underflow at zero", "p1", 1000, 0, 1, msgkit.Underflow, 0},
		{"MaxUint64", "p1", top, top, top, msgkit.OK, 0},
		{"MaxUint64 with one byte less reserved", "p1", top, top - 1, top, msgkit.Underflow, 0},
	}
	check := func(t *testing.T, e *env, op string, call func(ctx sdk.Context) msgkit.Code, c row) {
		t.Helper()
		before, rows := e.snapshot(), storeRows(e)
		ctx := e.fresh()
		got := call(ctx)
		if got != c.want {
			t.Fatalf("%s: code %q, want %q", op, got, c.want)
		}
		if n := len(ctx.EventManager().Events()); n != 0 {
			t.Fatalf("%s: emitted %d events", op, n)
		}
		if op == "ReserveProbe" || got != msgkit.OK {
			e.requireSameSnapshot(before, op)
			return
		}
		for name, snap := range before {
			if name != ModuleName && !bytes.Equal(snap, e.snapshot()[name]) {
				t.Fatalf("%s changed store %q", op, name)
			}
		}
		if err := onlyRecordChanged(rows, storeRows(e), c.id, c.after); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	for _, state := range allProviderStates {
		for _, c := range reserves {
			t.Run(fmt.Sprintf("reserve/%s/%s", state, c.name), func(t *testing.T) {
				e := newEnv(t)
				e.seed(accountingProvider("p1", state, c.capacity, c.reserved))
				check(t, e, "ReserveProbe", func(ctx sdk.Context) msgkit.Code { return e.k.ReserveProbe(ctx, c.id, c.amount) }, c)
				check(t, e, "Reserve", func(ctx sdk.Context) msgkit.Code { return e.k.Reserve(ctx, c.id, c.amount) }, c)
			})
		}
		for _, c := range releases {
			t.Run(fmt.Sprintf("release/%s/%s", state, c.name), func(t *testing.T) {
				e := newEnv(t)
				e.seed(accountingProvider("p1", state, c.capacity, c.reserved))
				check(t, e, "Release", func(ctx sdk.Context) msgkit.Code { return e.k.Release(ctx, c.id, c.amount) }, c)
			})
		}
	}

	t.Run("reserve then release restores the store", func(t *testing.T) {
		e := newEnv(t)
		e.seed(accountingProvider("p1", providersv1.ProviderState_PROVIDER_STATE_DRAINING, 1000, 250),
			accountingProvider("p2", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 0))
		before := e.snapshot()
		for _, step := range []struct {
			call func(sdk.Context, string, uint64) msgkit.Code
			id   string
			n    uint64
		}{
			{e.k.Reserve, "p1", 700}, {e.k.Reserve, "p2", 1000}, {e.k.Release, "p1", 300},
			{e.k.Reserve, "p1", 50}, {e.k.Release, "p2", 1000}, {e.k.Release, "p1", 450},
		} {
			if code := step.call(e.fresh(), step.id, step.n); code != msgkit.OK {
				t.Fatalf("%s %d: %s", step.id, step.n, code)
			}
		}
		e.requireSameSnapshot(before, "reserve/release round trip")
	})

	t.Run("the read surface and queries see the new value", func(t *testing.T) {
		e := newEnv(t)
		e.seed(accountingProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 0))
		if code := e.k.Reserve(e.fresh(), "p1", 640); code != msgkit.OK {
			t.Fatal(code)
		}
		if got := e.record("p1").GetReservedBytes(); got != 640 {
			t.Fatalf("Provider reserved %d", got)
		}
		res, err := e.k.QueryProvider(e.unmetered(), &providersv1.QueryProviderRequest{ProviderId: "p1"})
		if err != nil {
			t.Fatal(err)
		}
		want := accountingProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 640)
		if !proto.Equal(res.GetProvider(), want) {
			t.Fatalf("Query/Provider %v", res.GetProvider())
		}
		state, err := e.k.ExportGenesis(e.unmetered())
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateGenesis(state); err != nil || len(state.GetProviders()) != 1 || !proto.Equal(state.GetProviders()[0], want) {
			t.Fatalf("export %v: %v", state, err)
		}
		// UpdateProvider keeps refusing a capacity below what is reserved.
		if r := e.update(e.fresh(), updMsg("p1", "upd-below", 639)); r.code != msgkit.Capacity {
			t.Fatalf("update below reserved: %s", r.code)
		}
		e.mustOK(e.update(e.fresh(), updMsg("p1", "upd-at", 640)), "update at reserved")
		if code := e.k.ReserveProbe(e.fresh(), "p1", 1); code != msgkit.Capacity {
			t.Fatalf("probe after shrinking to reserved: %s", code)
		}
	})
}

// accountingGas is what the accounting calls cost after a number of
// rotations, and how many keys Reserve wrote.
type accountingGas struct {
	recordBytes                   int
	probeGas, reserveGas, release uint64
	written                       int
}

// accountingAfterRotations admits p1, rotates it rotations times, and then
// measures the gas of ReserveProbe, Reserve, and Release of 1000 bytes.
// Every key id has the same length, so only the rotation count differs
// between two runs.
func accountingAfterRotations(t *testing.T, rotations int) accountingGas {
	t.Helper()
	e := newEnv(t)
	e.admitted("p1")
	current := "key-p1"
	for i := 1; i <= rotations; i++ {
		next := fmt.Sprintf("k%05d", i)
		e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", current, next, uint64(i))), "rotate")
		current = next
	}
	var g accountingGas
	g.recordBytes = len(e.store(ModuleName).Get(ProviderKey("p1")))

	ctx := e.fresh()
	if code := e.k.ReserveProbe(ctx, "p1", 1000); code != msgkit.OK {
		t.Fatal(code)
	}
	g.probeGas = ctx.GasMeter().GasConsumed()

	before := storeRows(e)
	ctx = e.fresh()
	if code := e.k.Reserve(ctx, "p1", 1000); code != msgkit.OK {
		t.Fatal(code)
	}
	g.reserveGas = ctx.GasMeter().GasConsumed()
	after := storeRows(e)
	if err := onlyRecordChanged(before, after, "p1", 1000); err != nil {
		t.Fatal(err)
	}
	for k, v := range after {
		if !bytes.Equal(before[k], v) {
			g.written++
		}
	}

	ctx = e.fresh()
	if code := e.k.Release(ctx, "p1", 1000); code != msgkit.OK {
		t.Fatal(code)
	}
	g.release = ctx.GasMeter().GasConsumed()
	return g
}

// TestReserveWritesConstantSize: ReserveProbe reads, and Reserve and
// Release read and rewrite, one constant-size record. After 300 rotations
// the record is at most 2 bytes longer than after 2 (the varint of
// key_generation passes 127), Reserve writes exactly one key, and each
// call's gas differs only by the store's per-byte cost of those extra
// bytes (one read for ReserveProbe, one read and one write for Reserve and
// Release). With the retired keys inline, 300 rotations would add about
// 13,500 bytes to that record (TestWritesDoNotGrowWithRotations).
func TestReserveWritesConstantSize(t *testing.T) {
	few := accountingAfterRotations(t, 2)
	many := accountingAfterRotations(t, 300)
	extra := many.recordBytes - few.recordBytes
	if extra < 0 || extra > 2 {
		t.Fatalf("record is %d bytes after 2 rotations and %d after 300", few.recordBytes, many.recordBytes)
	}
	if few.written != 1 || many.written != 1 {
		t.Fatalf("Reserve wrote %d and %d keys", few.written, many.written)
	}
	cost := storetypes.KVGasConfig()
	read := uint64(extra) * cost.ReadCostPerByte
	write := uint64(extra) * cost.WriteCostPerByte
	for _, c := range []struct {
		name      string
		few, many uint64
		perByte   uint64
	}{
		{"ReserveProbe", few.probeGas, many.probeGas, read},
		{"Reserve", few.reserveGas, many.reserveGas, read + write},
		{"Release", few.release, many.release, read + write},
	} {
		if c.few == 0 || c.many != c.few+c.perByte {
			t.Fatalf("%s gas %d after 300 rotations, %d after 2, record %d bytes longer", c.name, c.many, c.few, extra)
		}
	}
}

// --- state-transition fuzz with an independent accounting model ---------

// acctModel is the test's own record of one provider: capacity, reserved
// bytes, and state. It is updated from the spec rules below, not from the
// keeper.
type acctModel struct {
	capacity, reserved uint64
	state              providersv1.ProviderState
}

// acctCalls are the three accounting calls under test. The fuzz runs the
// keeper's; TestAccountingHarnessCatchesMutants runs broken ones.
type acctCalls struct {
	probe, reserve, release func(sdk.Context, string, uint64) msgkit.Code
}

func keeperCalls(k Keeper) acctCalls {
	return acctCalls{probe: k.ReserveProbe, reserve: k.Reserve, release: k.Release}
}

// acctHarness runs bounded accounting sequences on a real IAVL store and
// checks each step against the model.
type acctHarness struct {
	e     *env
	calls acctCalls
	model map[string]*acctModel
	steps int
	// reached records "<op>/<code>" and "<op>/OK/<state>" outcomes.
	reached map[string]bool
}

var acctIDs = []string{"pa", "pb", "pc", "pd", "px", "P1"}

func acctSeed() []*providersv1.Provider {
	return []*providersv1.Provider{
		accountingProvider("pa", providersv1.ProviderState_PROVIDER_STATE_APPLIED, 1000, 0),
		accountingProvider("pb", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1<<40, 500),
		accountingProvider("pc", providersv1.ProviderState_PROVIDER_STATE_DRAINING, math.MaxUint64, math.MaxUint64-10),
		accountingProvider("pd", providersv1.ProviderState_PROVIDER_STATE_DISABLED, 10, 10),
	}
}

func newAcctHarness(t testing.TB, calls func(Keeper) acctCalls) *acctHarness {
	e := newEnv(t)
	seed := acctSeed()
	e.seed(seed...)
	h := &acctHarness{e: e, calls: calls(e.k), model: map[string]*acctModel{}, reached: map[string]bool{}}
	for _, p := range seed {
		h.model[p.GetProviderId()] = &acctModel{capacity: p.GetAdmittedCapacityBytes(), reserved: p.GetReservedBytes(), state: p.GetState()}
	}
	return h
}

// amountChoice picks an amount relative to the model record of id (zero
// values for an absent id), so short inputs reach every boundary.
func amountChoice(m *acctModel, choice int) uint64 {
	var capacity, reserved uint64
	if m != nil {
		capacity, reserved = m.capacity, m.reserved
	}
	room := uint64(0)
	if capacity > reserved {
		room = capacity - reserved
	}
	inc := func(n uint64) uint64 {
		if n == math.MaxUint64 {
			return n
		}
		return n + 1
	}
	switch choice {
	case 0:
		return 0
	case 1:
		return 1
	case 2:
		return room
	case 3:
		return inc(room)
	case 4:
		return reserved
	case 5:
		return inc(reserved)
	case 6:
		return math.MaxUint64 - reserved
	case 7:
		return inc(math.MaxUint64 - reserved)
	case 8:
		return math.MaxUint64
	default:
		return 500
	}
}

// capacityChoice picks a declared capacity for an UpdateProvider step.
func capacityChoice(m *acctModel, choice int) uint64 {
	var reserved uint64
	if m != nil {
		reserved = m.reserved
	}
	switch choice {
	case 0:
		return 0
	case 1:
		return reserved
	case 2:
		if reserved == 0 {
			return 1
		}
		return reserved - 1
	case 3:
		return math.MaxUint64
	case 4:
		return 10
	default:
		return 1 << 40
	}
}

// modelReserve is spec reserve/reserve_probe, written independently of the
// keeper: the overflow test is a carry out of a 64-bit add.
func modelReserve(m *acctModel, amount uint64) (msgkit.Code, uint64) {
	if amount == 0 {
		return msgkit.Malformed, 0
	}
	if m == nil {
		return msgkit.NotFound, 0
	}
	sum, carry := bits.Add64(m.reserved, amount, 0)
	if carry != 0 {
		return msgkit.Overflow, 0
	}
	if sum > m.capacity {
		return msgkit.Capacity, 0
	}
	return msgkit.OK, sum
}

// modelRelease is spec release, written as a borrow out of a 64-bit sub.
func modelRelease(m *acctModel, amount uint64) (msgkit.Code, uint64) {
	if amount == 0 {
		return msgkit.Malformed, 0
	}
	if m == nil {
		return msgkit.NotFound, 0
	}
	diff, borrow := bits.Sub64(m.reserved, amount, 0)
	if borrow != 0 {
		return msgkit.Underflow, 0
	}
	return msgkit.OK, diff
}

var errAcct = errors.New("accounting invariant violated")

// step decodes and runs one operation, then checks it. It returns an error
// (wrapping errAcct) on the first violation instead of failing the test,
// so the mutant test can assert that a broken call is caught.
func (h *acctHarness) step(b *fuzzBytes) error {
	e := h.e
	h.steps++
	op := b.pick(6)
	id := acctIDs[b.pick(len(acctIDs))]
	choice := b.pick(10)
	m := h.model[id]
	before, rows := e.snapshot(), storeRows(e)
	ctx := e.fresh()

	var name string
	var got, want msgkit.Code
	var reserved uint64 // the model's reserved bytes after a success
	switch op {
	case 0, 1, 2:
		amount := amountChoice(m, choice)
		switch op {
		case 0:
			name = "probe"
			got = h.calls.probe(ctx, id, amount)
			want, reserved = modelReserve(m, amount)
		case 1:
			name = "reserve"
			got = h.calls.reserve(ctx, id, amount)
			want, reserved = modelReserve(m, amount)
		default:
			name = "release"
			got = h.calls.release(ctx, id, amount)
			want, reserved = modelRelease(m, amount)
		}
		if got != want {
			return fmt.Errorf("%w: %s %s %d: code %q, model %q", errAcct, name, id, amount, got, want)
		}
		if n := len(ctx.EventManager().Events()); n != 0 {
			return fmt.Errorf("%w: %s emitted %d events", errAcct, name, n)
		}
		if name == "probe" || got != msgkit.OK {
			if err := sameSnapshot(before, e.snapshot()); err != nil {
				return fmt.Errorf("%w: %s %s %d (%q): %v", errAcct, name, id, amount, got, err)
			}
		} else {
			if err := onlyRecordChanged(rows, storeRows(e), id, reserved); err != nil {
				return fmt.Errorf("%w: %s %s %d: %v", errAcct, name, id, amount, err)
			}
			m.reserved = reserved
		}
	case 3:
		name = "update"
		capacity := capacityChoice(m, choice)
		got = h.e.update(ctx, updMsg(id, fmt.Sprintf("u-%d", h.steps), capacity)).code
		switch {
		case m == nil:
			want = msgkit.NotFound
		case capacity == 0:
			want = msgkit.Bounds
		case capacity < m.reserved:
			want = msgkit.Capacity
		default:
			want = msgkit.OK
		}
		if got != want {
			return fmt.Errorf("%w: update %s to %d: code %q, model %q", errAcct, id, capacity, got, want)
		}
		if got == msgkit.OK {
			m.capacity = capacity
		}
	case 4:
		name = "disable"
		got = e.k.DisableByCouncil(ctx, id)
		switch {
		case m == nil:
			want = msgkit.NotFound
		case m.state != providersv1.ProviderState_PROVIDER_STATE_ADMITTED && m.state != providersv1.ProviderState_PROVIDER_STATE_DRAINING:
			want = msgkit.Conflict
		default:
			want = msgkit.OK
		}
		if got != want {
			return fmt.Errorf("%w: disable %s: code %q, model %q", errAcct, id, got, want)
		}
		if got == msgkit.OK {
			m.state = providersv1.ProviderState_PROVIDER_STATE_DISABLED
		}
	default:
		name = "admit"
		got = e.k.AdmitByCouncil(ctx, id, true)
		switch {
		case m == nil:
			want = msgkit.NotFound
		case m.state != providersv1.ProviderState_PROVIDER_STATE_APPLIED:
			want = msgkit.Forbidden
		default:
			want = msgkit.OK
		}
		if got != want {
			return fmt.Errorf("%w: admit %s: code %q, model %q", errAcct, id, got, want)
		}
		if got == msgkit.OK {
			m.state = providersv1.ProviderState_PROVIDER_STATE_ADMITTED
		}
	}
	if got != msgkit.OK {
		if err := sameSnapshot(before, e.snapshot()); err != nil {
			return fmt.Errorf("%w: rejected %s (%q): %v", errAcct, name, got, err)
		}
	}
	if got == msgkit.OK {
		h.reached[name+"/OK"] = true
		if m != nil {
			h.reached[fmt.Sprintf("%s/OK/%s", name, m.state)] = true
		}
	} else {
		h.reached[name+"/"+string(got)] = true
	}
	return h.checkModel()
}

// checkModel compares every stored record with the model: reserved bytes,
// capacity, and state match, reserved never exceeds capacity, and no
// record appears for an id the model does not hold.
func (h *acctHarness) checkModel() error {
	for _, id := range acctIDs {
		m := h.model[id]
		record, found := h.e.k.Provider(h.e.unmetered(), id)
		if found != (m != nil) {
			return fmt.Errorf("%w: %s found %v", errAcct, id, found)
		}
		if !found {
			continue
		}
		if record.GetReservedBytes() != m.reserved || record.GetAdmittedCapacityBytes() != m.capacity || record.GetState() != m.state {
			return fmt.Errorf("%w: %s stored %d/%d %s, model %d/%d %s", errAcct, id,
				record.GetReservedBytes(), record.GetAdmittedCapacityBytes(), record.GetState(), m.reserved, m.capacity, m.state)
		}
		if record.GetReservedBytes() > record.GetAdmittedCapacityBytes() {
			return fmt.Errorf("%w: %s reserved %d above capacity %d", errAcct, id, record.GetReservedBytes(), record.GetAdmittedCapacityBytes())
		}
	}
	state, err := h.e.k.ExportGenesis(h.e.unmetered())
	if err != nil {
		return fmt.Errorf("%w: export: %v", errAcct, err)
	}
	if err := ValidateGenesis(state); err != nil {
		return fmt.Errorf("%w: export invalid: %v", errAcct, err)
	}
	return nil
}

func sameSnapshot(before, after map[string][]byte) error {
	names := make([]string, 0, len(before))
	for name := range before {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !bytes.Equal(before[name], after[name]) {
			return fmt.Errorf("store %q changed", name)
		}
	}
	return nil
}

// run executes up to fuzzMaxSteps operations of data.
func (h *acctHarness) run(data []byte) error {
	b := &fuzzBytes{data: data}
	for i := 0; i < fuzzMaxSteps && b.more(); i++ {
		if err := h.step(b); err != nil {
			return err
		}
	}
	return nil
}

// acctSeeds are the seed inputs: for each accounting call and each id, the
// ten amount choices in turn; then capacity updates around the reserved
// bytes; then state changes followed by accounting in the new state.
func acctSeeds() [][]byte {
	var seeds [][]byte
	for op := byte(0); op < 3; op++ {
		for id := range acctIDs {
			var in []byte
			for choice := byte(0); choice < 10; choice++ {
				in = append(in, op, byte(id), choice)
			}
			seeds = append(seeds, in)
		}
	}
	for id := range acctIDs {
		var in []byte
		for choice := byte(0); choice < 6; choice++ {
			in = append(in, 3, byte(id), choice, 1, byte(id), 3, 0, byte(id), 2, 2, byte(id), 2)
		}
		seeds = append(seeds, in)
	}
	seeds = append(seeds,
		// pa: reserve and release while applied, admit, reserve, disable,
		// reserve and release while disabled, admit again (FORBIDDEN).
		[]byte{1, 0, 9, 2, 0, 1, 5, 0, 0, 1, 0, 1, 4, 0, 0, 1, 0, 9, 2, 0, 4, 1, 0, 3, 5, 0, 0},
		// pb: disable, release everything, reserve to capacity, overflow.
		[]byte{4, 1, 0, 2, 1, 4, 1, 1, 2, 1, 1, 1, 0, 1, 7, 2, 1, 5},
		// pd (disabled, full): release, then reserve back, then update.
		[]byte{2, 3, 1, 1, 3, 1, 1, 3, 3, 3, 3, 1, 3, 3, 2},
	)
	return seeds
}

// FuzzReserveRelease runs bounded sequences of ReserveProbe, Reserve,
// Release, UpdateProvider, council disable, and council admit on four
// seeded providers (one per state) and two ids without a record. After
// every step: the code equals the independent model's (spec reserve,
// reserve_probe, release, op_update_capacity, and the council hooks); a
// reject or a probe leaves every mounted store byte-identical and the
// accounting calls emit nothing; a successful Reserve or Release changes
// only reserved_bytes of one record; every record equals the model, never
// holds more reserved bytes than capacity, and the export is valid genesis.
func FuzzReserveRelease(f *testing.F) {
	for _, seed := range acctSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		h := newAcctHarness(t, keeperCalls)
		if err := h.run(data); err != nil {
			t.Fatal(err)
		}
	})
}

// TestAccountingFuzzSeedsReachEveryOutcome: the seed corpus alone drives
// every accounting call to every code it can return, a successful Reserve
// and Release in each of the four provider states, and both outcomes of a
// capacity update around the reserved bytes. So FuzzReserveRelease starts
// from inputs that reach each branch it checks.
func TestAccountingFuzzSeedsReachEveryOutcome(t *testing.T) {
	reached := map[string]bool{}
	for i, seed := range acctSeeds() {
		h := newAcctHarness(t, keeperCalls)
		if err := h.run(seed); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
		for k := range h.reached {
			reached[k] = true
		}
	}
	var want []string
	for _, op := range []string{"probe", "reserve"} {
		want = append(want, op+"/OK")
		for _, code := range []msgkit.Code{msgkit.Malformed, msgkit.NotFound, msgkit.Overflow, msgkit.Capacity} {
			want = append(want, op+"/"+string(code))
		}
	}
	want = append(want, "release/OK", "update/OK")
	for _, code := range []msgkit.Code{msgkit.Malformed, msgkit.NotFound, msgkit.Underflow} {
		want = append(want, "release/"+string(code))
	}
	for _, code := range []msgkit.Code{msgkit.Capacity, msgkit.Bounds, msgkit.NotFound} {
		want = append(want, "update/"+string(code))
	}
	for _, state := range allProviderStates {
		want = append(want, fmt.Sprintf("reserve/OK/%s", state), fmt.Sprintf("release/OK/%s", state))
	}
	want = append(want, "disable/OK", "admit/OK")
	for _, w := range want {
		if !reached[w] {
			t.Errorf("seeds never reach %s", w)
		}
	}
}

// TestAccountingHarnessCatchesMutants: the checks of FuzzReserveRelease
// catch each broken accounting call on the seed corpus. Each mutant runs
// the real keeper except for one fault; at least one seed must fail with
// an accounting violation.
func TestAccountingHarnessCatchesMutants(t *testing.T) {
	mutants := map[string]func(Keeper) acctCalls{
		"reject writes before it fails": func(k Keeper) acctCalls {
			c := keeperCalls(k)
			c.reserve = func(ctx sdk.Context, id string, amount uint64) msgkit.Code {
				code := k.Reserve(ctx, id, amount)
				if code == msgkit.Capacity {
					record, _ := k.mustLoad(ctx, id)
					record.ReservedBytes = record.GetAdmittedCapacityBytes()
					k.save(ctx, record)
				}
				return code
			}
			return c
		},
		"probe reserves": func(k Keeper) acctCalls {
			c := keeperCalls(k)
			c.probe = k.Reserve
			return c
		},
		"reserve wraps instead of OVERFLOW": func(k Keeper) acctCalls {
			c := keeperCalls(k)
			c.reserve = func(ctx sdk.Context, id string, amount uint64) msgkit.Code {
				if amount == 0 {
					return msgkit.Malformed
				}
				record, found := k.mustLoad(ctx, id)
				if !found {
					return msgkit.NotFound
				}
				if record.GetReservedBytes()+amount > record.GetAdmittedCapacityBytes() {
					return msgkit.Capacity
				}
				record.ReservedBytes += amount
				k.save(ctx, record)
				return msgkit.OK
			}
			return c
		},
		"release wraps instead of UNDERFLOW": func(k Keeper) acctCalls {
			c := keeperCalls(k)
			c.release = func(ctx sdk.Context, id string, amount uint64) msgkit.Code {
				if amount == 0 {
					return msgkit.Malformed
				}
				record, found := k.mustLoad(ctx, id)
				if !found {
					return msgkit.NotFound
				}
				record.ReservedBytes -= amount
				k.save(ctx, record)
				return msgkit.OK
			}
			return c
		},
		"reserve also changes capacity": func(k Keeper) acctCalls {
			c := keeperCalls(k)
			c.reserve = func(ctx sdk.Context, id string, amount uint64) msgkit.Code {
				code := k.Reserve(ctx, id, amount)
				if code == msgkit.OK {
					record, _ := k.mustLoad(ctx, id)
					if record.GetAdmittedCapacityBytes() < math.MaxUint64 {
						record.AdmittedCapacityBytes++
					}
					k.save(ctx, record)
				}
				return code
			}
			return c
		},
	}
	names := make([]string, 0, len(mutants))
	for name := range mutants {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		caught := false
		for _, seed := range acctSeeds() {
			h := newAcctHarness(t, mutants[name])
			if err := h.run(seed); err != nil {
				if !errors.Is(err, errAcct) {
					t.Fatalf("%s: unexpected error %v", name, err)
				}
				caught = true
				break
			}
		}
		if !caught {
			t.Errorf("mutant %q passed every seed", name)
		}
	}
}
