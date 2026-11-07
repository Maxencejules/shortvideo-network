package upgrade_test

import (
	"crypto/sha256"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// The fuzz domain: four module names, three upgrade names, small versions
// and heights, so equal maps and due plans are frequent.
var (
	fuzzModules  = []string{"alpha", "beta", "delta", "gamma"}
	fuzzUpgrades = []string{"up-1", "up-2", "up-3"}
)

// fuzzBytes reads the fuzz input one byte at a time; past the end it reads
// zeros.
type fuzzBytes struct {
	data []byte
	at   int
}

func (r *fuzzBytes) next() byte {
	if r.at >= len(r.data) {
		return 0
	}
	b := r.data[r.at]
	r.at++
	return b
}

// decodeGateInput turns fuzz bytes into a stored state and a binary. With
// the first byte's low bit set, the state starts consistent with the
// binary (same module names, layouts equal to the stored map, Version.App
// equal to the stored app version); later bytes then perturb it. Bit 1 of
// the first byte puts L near the uint64 maximum.
func decodeGateInput(data []byte) (upgrade.StoredState, upgrade.Binary) {
	r := &fuzzBytes{data: data}
	flags := r.next()
	coherent := flags&1 == 1
	var s upgrade.StoredState
	s.LastHeight = uint64(r.next() % 6)
	if flags&2 != 0 {
		s.LastHeight = math.MaxUint64 - uint64(r.next()%3)
	}
	b := upgrade.Binary{AppVersion: uint64(r.next() % 5), Modules: module.VersionMap{}}
	s.AppVersion = uint64(r.next() % 5)
	if coherent && r.next()%2 == 0 {
		s.AppVersion = b.AppVersion - uint64(r.next()%2)
	}
	s.ConsensusAppVersion = s.AppVersion
	if c := r.next(); c%5 == 0 {
		s.ConsensusAppVersion = uint64(c % 4)
	}
	s.Modules = module.VersionMap{}
	s.Layouts = map[string]uint64{}
	for _, name := range fuzzModules {
		m := r.next()
		inBinary, inState, hasLayout := m&1 == 0 || coherent, m&2 == 0 || coherent, m&4 == 0
		version := uint64(1 + r.next()%3)
		if inBinary {
			b.Modules[name] = version
		}
		if inState {
			stored := version
			if d := r.next(); d%4 == 0 {
				stored = uint64(d % 4) // 0..3: equal, lower, higher, or 0
			}
			s.Modules[name] = stored
			if hasLayout {
				s.Layouts[name] = stored
				if l := r.next(); l%6 == 0 {
					s.Layouts[name] = uint64(l % 4)
				}
			}
		} else if hasLayout && !coherent {
			s.Layouts[name] = uint64(r.next() % 4)
		}
	}
	if p := r.next(); p%4 != 0 {
		name := fuzzUpgrades[r.next()%3]
		// Height L-1 .. L+3, without wrapping.
		delta := int(r.next()%5) - 1
		height := s.LastHeight
		switch {
		case delta < 0 && height > 0:
			height--
		case delta > 0 && height <= math.MaxUint64-uint64(delta):
			height += uint64(delta)
		}
		s.Plan = &upgrade.Plan{Name: name, Height: height, Digest: digest(r.next())}
	}
	for n := r.next() % 4; n > 0; n-- {
		u := upgrade.Upgrade{Name: fuzzUpgrades[r.next()%3], Handler: migrateThrough}
		u.FromAppVersion, u.ToAppVersion = b.AppVersion-1, b.AppVersion
		if e := r.next(); e%5 == 0 {
			u.FromAppVersion, u.ToAppVersion = uint64(e%4), uint64(e/4%4)
		}
		b.Upgrades = append(b.Upgrades, u)
	}
	// With the third flag bit, a coherent input names the plan's upgrade
	// first in the handler list, so rows 3 and 4 are frequent.
	if coherent && flags&4 != 0 && s.Plan != nil && len(b.Upgrades) > 0 {
		b.Upgrades[0].Name = s.Plan.Name
	}
	return s, b
}

// gateOracle states rows 1 and 3 of the gate table (plan 11.4) directly
// from the definitions, independently of Gate's code order: which row
// holds (1 or 3), or 0 when neither does.
func gateOracle(s upgrade.StoredState, b upgrade.Binary) int {
	if s.LastHeight == 0 {
		return 0 // the gate applies only after a committed block
	}
	consistent := len(s.Modules) <= upgrade.MaxModules && len(s.Layouts) <= upgrade.MaxModules && len(b.Modules) <= upgrade.MaxModules &&
		s.AppVersion >= 1 && s.AppVersion == s.ConsensusAppVersion && len(s.Modules) == len(b.Modules)
	for name, stored := range s.Modules {
		if _, ok := b.Modules[name]; !ok || stored < 1 {
			consistent = false
		}
	}
	for name, layout := range s.Layouts {
		if stored, ok := s.Modules[name]; !ok || layout != stored {
			consistent = false
		}
	}
	if !consistent {
		return 0
	}
	planAt := func(offset uint64) bool { // plan height == L + offset, without wrapping
		return s.Plan != nil && s.Plan.Height > s.LastHeight && s.Plan.Height-s.LastHeight == offset
	}
	noPlanAtOrBelowNext := s.Plan == nil || (s.Plan.Height > s.LastHeight && s.Plan.Height-s.LastHeight > 1)
	if s.AppVersion == b.AppVersion && reflect.DeepEqual(map[string]uint64(s.Modules), map[string]uint64(b.Modules)) && noPlanAtOrBelowNext {
		return 1
	}
	if b.AppVersion >= 1 && s.AppVersion == b.AppVersion-1 && planAt(1) {
		matches, named := 0, 0
		for _, u := range b.Upgrades {
			if u.Name == s.Plan.Name {
				named++
				if u.FromAppVersion == s.AppVersion && u.ToAppVersion == b.AppVersion {
					matches++
				}
			}
		}
		notNewer := true
		for name, stored := range s.Modules {
			if stored > b.Modules[name] {
				notNewer = false
			}
		}
		if named == 1 && matches == 1 && notNewer {
			return 3
		}
	}
	return 0
}

// checkGateAgainstOracle runs Gate on the decoded input and fails unless
// it starts exactly when the oracle says row 1 or row 3 holds, with that
// row; refuses with row 2, 4, 5, or 6 (or ErrNoCommittedBlock at L = 0)
// otherwise; leaves its inputs unchanged; and gives the same answer twice.
// It returns the row Gate reported (0 for ErrNoCommittedBlock).
func checkGateAgainstOracle(t *testing.T, data []byte) int {
	s, b := decodeGateInput(data)
	want := gateOracle(s, b)
	sCopy, bModules, bUpgrades := cloneState(s), vm(b.Modules), append([]upgrade.Upgrade(nil), b.Upgrades...)
	decision, err := upgrade.Gate(s, b)
	again, err2 := upgrade.Gate(s, b)
	if !reflect.DeepEqual(s, sCopy) || !reflect.DeepEqual(b.Modules, bModules) || len(b.Upgrades) != len(bUpgrades) {
		t.Fatalf("Gate changed its input: %+v", s)
	}
	if !reflect.DeepEqual(decision, again) && !(decision.Row == again.Row && decision.Upgrade.Name == again.Upgrade.Name) {
		t.Fatalf("Gate is not deterministic: %+v then %+v", decision, again)
	}
	if (err == nil) != (err2 == nil) || (err != nil && err.Error() != err2.Error()) {
		t.Fatalf("Gate is not deterministic: %v then %v", err, err2)
	}
	if want != 0 {
		if err != nil {
			t.Fatalf("oracle row %d holds, Gate refused: %v\nstate %+v plan %+v\nbinary %+v", want, err, s, s.Plan, b)
		}
		if decision.Row != want || decision.Window != (want == 3) {
			t.Fatalf("oracle row %d, Gate decision %+v", want, decision)
		}
		if want == 3 && (decision.Upgrade.Name != s.Plan.Name || decision.Upgrade.Handler == nil) {
			t.Fatalf("row 3 decision names upgrade %q, plan %q", decision.Upgrade.Name, s.Plan.Name)
		}
		return want
	}
	if err == nil {
		t.Fatalf("neither row 1 nor row 3 holds, Gate started with %+v\nstate %+v plan %+v\nbinary %+v", decision, s, s.Plan, b)
	}
	if s.LastHeight == 0 {
		if !errors.Is(err, upgrade.ErrNoCommittedBlock) {
			t.Fatalf("L = 0: %v, want ErrNoCommittedBlock", err)
		}
		return 0
	}
	var refusal *upgrade.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is %T %v, want *RefusalError", err, err)
	}
	switch refusal.Row {
	case 2, 4, 5, 6:
	default:
		t.Fatalf("refusal row %d", refusal.Row)
	}
	return refusal.Row
}

func cloneState(s upgrade.StoredState) upgrade.StoredState {
	out := s
	out.Modules = vm(s.Modules)
	out.Layouts = layouts(s.Layouts)
	if s.Plan != nil {
		p := *s.Plan
		p.Digest = append([]byte(nil), s.Plan.Digest...)
		out.Plan = &p
	}
	return out
}

// gateSeeds is a deterministic corpus: SHA-256 chains of a counter, cut to
// a length that covers every byte the decoder reads.
func gateSeeds(n int) [][]byte {
	seeds := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		var buf []byte
		h := sha256.Sum256([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		for len(buf) < 48 {
			buf = append(buf, h[:]...)
			h = sha256.Sum256(h[:])
		}
		seeds = append(seeds, buf[:48])
	}
	return seeds
}

// FuzzGateStartsOnlyOnRows1And3: the load gate starts a binary if and only
// if row 1 or row 3 of the gate table holds (gateOracle), never changes its
// input, and is deterministic.
func FuzzGateStartsOnlyOnRows1And3(f *testing.F) {
	for _, seed := range gateSeeds(64) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkGateAgainstOracle(t, data)
	})
}

// TestGateFuzzDomainReachesEveryRow: the fuzz decoder reaches every gate
// row, the L = 0 case, and both start rows, on a deterministic corpus, so
// the fuzz target compares the gate with the oracle on interesting inputs
// and not only on refusals.
func TestGateFuzzDomainReachesEveryRow(t *testing.T) {
	counts := map[int]int{}
	for _, seed := range gateSeeds(20000) {
		counts[checkGateAgainstOracle(t, seed)]++
	}
	for row := 0; row <= 6; row++ {
		if counts[row] == 0 {
			t.Fatalf("no input reached row %d (0 is L = 0): %v", row, counts)
		}
	}
	t.Logf("rows reached over 20000 inputs (0 is L = 0): %v", counts)
}
