package upgrade

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/types/module"
)

// StoredState is the committed upgrade state the load gate reads, after the
// last committed block.
type StoredState struct {
	// LastHeight is the last committed block height (L).
	LastHeight uint64
	// AppVersion is the stored app version (x/policy 0x04); 0 means the row
	// is absent, which is a state written before goal 10.
	AppVersion uint64
	// ConsensusAppVersion is the stored ConsensusParams.Version.App; 0 when
	// the params hold no version.
	ConsensusAppVersion uint64
	// Modules is the persisted module version map (x/policy 0x05).
	Modules module.VersionMap
	// Layouts holds the layout version key of every module whose store
	// keeps one; 0 means the key is absent.
	Layouts map[string]uint64
	// Plan is the scheduled upgrade, nil when none is scheduled.
	Plan *Plan
}

// Decision is the outcome of a gate that lets the binary start.
type Decision struct {
	// Row is the gate row that holds: 1 (normal start) or 3 (start in the
	// upgrade window).
	Row int
	// Window is true for row 3: the next block applies Upgrade, and until
	// it commits, CheckTx and queries run this binary's code on state of
	// the previous app version.
	Window bool
	// Upgrade is the handler the next block runs (row 3 only).
	Upgrade Upgrade
}

// RefusalError is a gate refusal: the binary must not start on this state.
type RefusalError struct {
	// Row is the gate row that refused: 2, 4, 5, or 6.
	Row int
	Msg string
}

func (e *RefusalError) Error() string { return e.Msg }

func refuse(row int, format string, args ...any) (Decision, error) {
	return Decision{}, &RefusalError{Row: row, Msg: fmt.Sprintf(format, args...)}
}

// Gate decides whether binary b may start on committed state s. It runs
// at load, outside consensus, and only after a committed block (s.LastHeight
// of 0 returns an error wrapping ErrNoCommittedBlock: InitChain rules apply
// there instead). It is a pure function.
//
// s is consistent when the stored app version is present (not 0), equals
// the stored ConsensusParams.Version.App, every layout key equals its
// stored version map entry, every stored entry is at least 1, the stored
// map and the binary's map name the same modules, and neither map holds
// more than MaxModules modules. With P = b.AppVersion, Ps = s.AppVersion,
// VMs = s.Modules, VM = b.Modules, L = s.LastHeight:
//
//	row 1  consistent, Ps == P, VMs == VM, no plan or plan height > L+1     start
//	row 2  consistent, Ps == P, plan height == L+1                          refuse: install the binary for it
//	row 3  consistent, Ps == P-1, plan height == L+1, exactly one handler
//	       of the plan's name, from Ps to P, every VMs[m] <= VM[m]          start in the upgrade window
//	row 4  as row 3 but plan height > L+1                                   refuse: too early
//	row 5  Ps > P, a VMs[m] > VM[m], or a stored module the binary lacks    refuse: state is newer
//	row 6  everything else                                                  refuse: named mismatch
//
// The map sizes are checked first (row 6), then row 5, then consistency
// (row 6), then a plan at or below L (row 6), then rows 1 to 4. Gate
// starts the binary if and only if row 1 or row 3 holds
// (FuzzGateStartsOnlyOnRows1And3).
func Gate(s StoredState, b Binary) (Decision, error) {
	if s.LastHeight == 0 {
		return Decision{}, errorf(ErrNoCommittedBlock, nil, "the load gate applies only after a committed block; InitChain rules apply at height 0")
	}
	switch {
	case len(s.Modules) > MaxModules:
		return refuse(6, "the stored version map has %d modules, more than %d", len(s.Modules), MaxModules)
	case len(s.Layouts) > MaxModules:
		return refuse(6, "the state reports %d layout keys, more than %d", len(s.Layouts), MaxModules)
	case len(b.Modules) > MaxModules:
		return refuse(6, "the binary declares %d modules, more than %d", len(b.Modules), MaxModules)
	}

	// Row 5: the state is newer than this binary.
	if s.AppVersion > b.AppVersion {
		return refuse(5, "state is newer than this binary (app version %d, binary %d)", s.AppVersion, b.AppVersion)
	}
	for _, name := range sortedNames(s.Modules) {
		want, ok := b.Modules[name]
		if !ok {
			return refuse(5, "state is newer than this binary (app version %d, binary %d): stored module %s is not in this binary",
				s.AppVersion, b.AppVersion, name)
		}
		if s.Modules[name] > want {
			return refuse(5, "state is newer than this binary (app version %d, binary %d): module %s stored version %d, binary version %d",
				s.AppVersion, b.AppVersion, name, s.Modules[name], want)
		}
	}

	if msg, bad := inconsistent(s, b); bad {
		return Decision{}, &RefusalError{Row: 6, Msg: msg}
	}

	if s.Plan != nil && s.Plan.Height <= s.LastHeight {
		return refuse(6, "upgrade %s at height %d is not above the last committed height %d", s.Plan.Name, s.Plan.Height, s.LastHeight)
	}
	// The plan height is above L here, so Height-L cannot wrap.
	due := s.Plan != nil && s.Plan.Height-s.LastHeight == 1

	switch {
	case s.AppVersion == b.AppVersion:
		if due {
			return refuse(2, "upgrade %s is due at height %d; install the binary for it (release digest %x)",
				s.Plan.Name, s.Plan.Height, s.Plan.Digest)
		}
		if name, differ := firstDifference(s.Modules, b.Modules); differ {
			return refuse(6, "app version %d matches this binary, but module %s stored version %d differs from binary version %d",
				s.AppVersion, name, s.Modules[name], b.Modules[name])
		}
		return Decision{Row: 1}, nil
	case b.AppVersion >= 1 && s.AppVersion == b.AppVersion-1:
		if s.Plan == nil {
			return refuse(6, "stored app version %d is one behind this binary (%d) and no upgrade plan is stored", s.AppVersion, b.AppVersion)
		}
		up, n := lookup(b, s.Plan.Name)
		switch {
		case n == 0:
			return refuse(6, "stored app version %d is one behind this binary (%d), and this binary has no handler for the planned upgrade %s",
				s.AppVersion, b.AppVersion, s.Plan.Name)
		case n > 1:
			return refuse(6, "this binary declares %d handlers named %s", n, s.Plan.Name)
		case up.FromAppVersion != s.AppVersion || up.ToAppVersion != b.AppVersion:
			return refuse(6, "the handler for upgrade %s goes from app version %d to %d; stored app version %d, binary %d",
				up.Name, up.FromAppVersion, up.ToAppVersion, s.AppVersion, b.AppVersion)
		case !due:
			return refuse(4, "too early: run the previous binary until height %d (upgrade %s applies at height %d)",
				s.Plan.Height-1, s.Plan.Name, s.Plan.Height)
		}
		return Decision{Row: 3, Window: true, Upgrade: up}, nil
	default:
		return refuse(6, "stored app version %d is more than one behind this binary (%d)", s.AppVersion, b.AppVersion)
	}
}

// inconsistent returns a message naming the mismatch, and true, when s is
// not consistent with itself or does not name the binary's modules. Row 5
// has already refused every stored module the binary lacks.
func inconsistent(s StoredState, b Binary) (string, bool) {
	fail := func(format string, args ...any) (string, bool) {
		return fmt.Sprintf(format, args...), true
	}
	if s.AppVersion == 0 {
		return fail("no stored app version: the state was written by a binary from before goal 10 (goals 05 to 09), which this binary cannot read or migrate; initialize a new home")
	}
	if s.AppVersion != s.ConsensusAppVersion {
		return fail("stored app version %d differs from the stored consensus Version.App %d", s.AppVersion, s.ConsensusAppVersion)
	}
	for _, name := range sortedNames(s.Modules) {
		if s.Modules[name] < 1 {
			return fail("stored version map entry %s is 0", name)
		}
	}
	for _, name := range sortedNames(b.Modules) {
		if _, ok := s.Modules[name]; !ok {
			return fail("module %s is in this binary but not in the stored version map", name)
		}
	}
	for _, name := range sortedNames(s.Layouts) {
		stored, ok := s.Modules[name]
		switch layout := s.Layouts[name]; {
		case !ok:
			return fail("%s store has a layout key, but the stored version map has no %s entry", name, name)
		case layout == 0:
			return fail("%s store has no layout version key; the stored version map says %d", name, stored)
		case layout != stored:
			return fail("%s store has layout version %d; the stored version map says %d", name, layout, stored)
		}
	}
	return "", false
}
