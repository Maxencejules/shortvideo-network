// Package upgrade holds the chain-upgrade decisions of svnd:
//
//   - Binary: what one binary declares, its app version, its module
//     consensus versions, and the upgrade handlers compiled into it.
//   - Gate: the load-time decision, outside consensus, whether a binary
//     may start on committed state (only after a committed block).
//   - CheckBlock: the per-block rule. At the plan height a binary without
//     the handler fails; at any other height a stored app version that
//     differs from the binary's fails.
//   - Runner: runs one upgrade in a cache context and writes it back only
//     after every check passed. It gives the handler the handler
//     environment (env.go): by-name access to the module stores.
//   - PlanSource: where the scheduled upgrade is read from.
//     CouncilPlans (plans.go) is the production source: the plan row an
//     executed council proposal wrote into the x/policy store, read through
//     PlanRows.
//
// The package imports no app, module keeper, node, or command code. The
// app composes it: it supplies the stores through the interfaces of
// runner.go and plans.go (for the plan row, a small adapter over the
// x/policy keeper), the migrations through Migrate, and its mounted store
// keys through Env.
//
// Every non-test file runs inside chain execution or in the load gate. It
// reads block data only from sdk.Context, iterates maps in sorted key
// order with a bound, and never reads the local clock, randomness, the
// environment, files, or the network (TestDeterministicSources).
//
// What the mechanism does not provide: a skip-upgrade height, a node-local
// halt height, automatic binary download, time-based activation, store
// additions at an upgrade, or a rollback.
package upgrade

import (
	"errors"
	"fmt"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

const (
	// MaxModules bounds every module version map this package reads or
	// accepts. It equals the bound of the persisted version map in
	// x/policy (MaxVersionMapModules), above the app's eight modules.
	MaxModules = 32

	// MaxUpgrades bounds the upgrade handlers one binary declares.
	MaxUpgrades = 8

	// MaxUnchanged bounds the post-condition checks one runner holds.
	MaxUnchanged = 32

	// DigestBytes is the size of a plan's release digest (SHA-256).
	DigestBytes = msgkit.DigestBytes
)

// Plan is a scheduled upgrade: the upgrade name and the height of the
// first block the new binary executes. Digest is the 32-byte SHA-256
// release digest the plan names. The chain cannot check which binary a
// node runs, so the digest is information for operators.
type Plan struct {
	Name   string
	Height uint64
	Digest []byte
}

// Migrate runs the module migrations from the version map from and
// returns the new map. The app supplies it as the module manager's
// RunMigrations with the configurator every module registered its
// migrations on, and with an explicit migration order.
type Migrate func(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error)

// Handler is the code of one upgrade. It receives a copy of the stored
// version map and the app's Migrate, and returns the version map after the
// upgrade. Its context carries the handler environment (EnvOf), through
// which it reads and writes module stores by name. It runs inside the
// runner's cache context: an error, or any failed check after it, discards
// everything it wrote.
type Handler func(ctx sdk.Context, from module.VersionMap, migrate Migrate) (module.VersionMap, error)

// Upgrade is one upgrade handler compiled into a binary. It moves the app
// version from FromAppVersion to ToAppVersion.
type Upgrade struct {
	Name           string
	FromAppVersion uint64
	ToAppVersion   uint64
	Handler        Handler
}

// Binary is what one binary declares: its app version, the consensus
// version of every module it registers, and the upgrades it can apply.
type Binary struct {
	AppVersion uint64
	Modules    module.VersionMap
	Upgrades   []Upgrade
}

// Validate refuses a declaration the gate and the runner cannot rely on:
//
//   - app version 0
//   - no module, more than MaxModules modules, a module name outside the
//     protocol id pattern, or a module version 0
//   - more than MaxUpgrades upgrades
//   - an upgrade name outside the protocol id pattern, a name declared
//     twice, or a nil handler
//   - an upgrade whose FromAppVersion is not AppVersion-1 or whose
//     ToAppVersion is not AppVersion (so an app version 1 binary declares
//     no upgrade: app versions start at 1)
func (b Binary) Validate() error {
	if b.AppVersion < 1 {
		return errorf(ErrInvalidBinary, nil, "binary app version is 0")
	}
	if len(b.Modules) == 0 || len(b.Modules) > MaxModules {
		return errorf(ErrInvalidBinary, nil, "binary declares %d modules, want 1 to %d", len(b.Modules), MaxModules)
	}
	for _, name := range sortedNames(b.Modules) {
		if !msgkit.ValidID(name) {
			return errorf(ErrInvalidBinary, nil, "binary module name %q is outside the id pattern", name)
		}
		if b.Modules[name] < 1 {
			return errorf(ErrInvalidBinary, nil, "binary module %s has consensus version 0", name)
		}
	}
	if len(b.Upgrades) > MaxUpgrades {
		return errorf(ErrInvalidBinary, nil, "binary declares %d upgrades, at most %d", len(b.Upgrades), MaxUpgrades)
	}
	seen := make(map[string]bool, len(b.Upgrades))
	for i, up := range b.Upgrades {
		switch {
		case !msgkit.ValidID(up.Name):
			return errorf(ErrInvalidBinary, nil, "upgrade %d name %q is outside the id pattern", i, up.Name)
		case seen[up.Name]:
			return errorf(ErrInvalidBinary, nil, "upgrade %s is declared twice", up.Name)
		case up.Handler == nil:
			return errorf(ErrInvalidBinary, nil, "upgrade %s has no handler", up.Name)
		case b.AppVersion == 1:
			return errorf(ErrInvalidBinary, nil, "upgrade %s goes from app version %d to %d; an app version 1 binary declares no upgrade (app versions start at 1)",
				up.Name, up.FromAppVersion, up.ToAppVersion)
		case up.FromAppVersion+1 != b.AppVersion || up.ToAppVersion != b.AppVersion:
			return errorf(ErrInvalidBinary, nil, "upgrade %s goes from app version %d to %d; a binary at app version %d declares only %d to %d",
				up.Name, up.FromAppVersion, up.ToAppVersion, b.AppVersion, b.AppVersion-1, b.AppVersion)
		}
		seen[up.Name] = true
	}
	return nil
}

// Error kinds. Every error this package returns wraps exactly one of them
// (errors.Is), and the cause when there is one.
var (
	// ErrInvalidBinary: a Binary declaration that Validate refuses.
	ErrInvalidBinary = errors.New("invalid binary declaration")
	// ErrInvalidDeps: a runner built without a required dependency.
	ErrInvalidDeps = errors.New("invalid runner dependencies")
	// ErrNoCommittedBlock: the load gate was asked about a state with no
	// committed block; InitChain rules apply there instead.
	ErrNoCommittedBlock = errors.New("no committed block")
	// ErrUpgradeNeeded: the block at the plan height reached a binary
	// without the plan's handler.
	ErrUpgradeNeeded = errors.New("upgrade needed")
	// ErrWrongBinary: a block reached a binary built for another app
	// version, or a handler whose versions do not fit the state.
	ErrWrongBinary = errors.New("wrong binary")
	// ErrIntegrity: committed upgrade state that cannot occur while the
	// rules hold (a plan below the block height), or a store read failed.
	ErrIntegrity = errors.New("upgrade state integrity")
	// ErrPrecondition: the state the upgrade starts from is not the one
	// the binary declares it upgrades from.
	ErrPrecondition = errors.New("upgrade precondition failed")
	// ErrHandler: the upgrade handler returned an error.
	ErrHandler = errors.New("upgrade handler failed")
	// ErrPostcondition: the state after the handler is not the state the
	// binary declares, or the handler changed a row it must not change.
	ErrPostcondition = errors.New("upgrade post-condition failed")
	// ErrRecord: writing or reading back the upgrade record failed.
	ErrRecord = errors.New("upgrade record failed")
)

// kindError is an error of one kind with its own message and an optional
// cause.
type kindError struct {
	kind  error
	cause error
	msg   string
}

func (e *kindError) Error() string { return e.msg }

func (e *kindError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

func errorf(kind, cause error, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if cause != nil {
		msg += ": " + cause.Error()
	}
	return &kindError{kind: kind, cause: cause, msg: msg}
}

// sortedNames returns the keys of a version map in byte order. Callers
// bound the map's size first.
func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// cloneVM returns a copy of vm, so a callee cannot change the caller's map.
func cloneVM(vm module.VersionMap) module.VersionMap {
	out := make(module.VersionMap, len(vm))
	for name, v := range vm {
		out[name] = v
	}
	return out
}

// firstDifference returns the first module name, in byte order over both
// maps, whose entry differs between a and b (absent counts as different),
// and false when the maps are equal.
func firstDifference(a, b module.VersionMap) (string, bool) {
	names := sortedNames(a)
	for name := range b {
		if _, ok := a[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		va, okA := a[name]
		vb, okB := b[name]
		if okA != okB || va != vb {
			return name, true
		}
	}
	return "", false
}

// lookup returns the upgrades of b named name and how many there are.
func lookup(b Binary, name string) (Upgrade, int) {
	var found Upgrade
	n := 0
	for _, up := range b.Upgrades {
		if up.Name == name {
			found = up
			n++
		}
	}
	return found, n
}
