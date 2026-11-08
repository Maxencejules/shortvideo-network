package upgrade

import (
	"bytes"
	"context"
	"fmt"
	"strconv"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// PlanSource reads the scheduled upgrade. found is false when no upgrade
// is scheduled. A source never reports an upgrade that has applied: once
// the runner records an upgrade (VersionStore.RecordUpgrade), the source
// stops reporting it, and the runner checks that it did. CouncilPlans
// (plans.go) is the production source.
type PlanSource interface {
	Plan(ctx sdk.Context) (plan Plan, found bool, err error)
}

// VersionStore holds the stored app version and module version map and
// records an applied upgrade. The x/policy keeper has this method set.
type VersionStore interface {
	// AppVersion returns the stored app version, 0 when none is stored.
	AppVersion(ctx sdk.Context) uint64
	// VersionMap returns the persisted module version map.
	VersionMap(ctx sdk.Context) module.VersionMap
	// RecordUpgrade records upgrade name as applied at height, with the
	// new version map and app version, and deletes a stored plan of that
	// name. It checks before it writes.
	RecordUpgrade(ctx sdk.Context, name string, height uint64, vm module.VersionMap, appVersion uint64) error
}

// ConsensusParamStore reads and writes the stored consensus params. The
// app's baseapp.ParamStore has this method set.
type ConsensusParamStore interface {
	Get(ctx context.Context) (cmtproto.ConsensusParams, error)
	Set(ctx context.Context, params cmtproto.ConsensusParams) error
}

// LayoutSource reads the layout version key of every module whose store
// keeps one, keyed by module name; 0 means the key is absent.
type LayoutSource interface {
	LayoutVersions(ctx sdk.Context) (map[string]uint64, error)
}

// PolicyCounter reads and bumps the council policy version. An applied
// upgrade bumps it by one (DR-10-38, now the owner's Q2 ruling "Council
// plan, fail closed (Recommended)"), so a proposal approved on the old
// version cannot execute after the upgrade. The x/authority keeper has
// this method set.
type PolicyCounter interface {
	PolicyVersion(ctx sdk.Context) uint64
	BumpPolicyVersionForUpgrade(ctx sdk.Context) error
}

// Unchanged is one class of rows an upgrade must leave as they were (the
// I13 self-check). Snapshot returns a canonical encoding of those rows in
// the state at version map vm; the runner takes it before the handler with
// the stored map and after the handler with the map the handler returned,
// and refuses the upgrade unless the two are byte-equal. A snapshot that
// decodes rows by their module's version can therefore cover a module the
// upgrade migrates. The check runs before the runner writes its own rows
// (the version rows, the plan, Version.App, the policy version).
//
// This protects only against an honest binary's mistakes: an upgrade
// binary can change or drop its own checks. The control against a wrong
// or malicious binary is human review of its source and digest before an
// operator installs it.
type Unchanged struct {
	Name     string
	Snapshot func(ctx sdk.Context, vm module.VersionMap) ([]byte, error)
}

// Deps are the stores and code the runner uses. Every field is required;
// Unchanged must hold at least one check (the I13 self-check). Env is the
// handler environment the runner gives every handler it calls (env.go).
type Deps struct {
	Plans     PlanSource
	Versions  VersionStore
	Params    ConsensusParamStore
	Layouts   LayoutSource
	Counter   PolicyCounter
	Migrate   Migrate
	Env       Env
	Unchanged []Unchanged
}

// EventUpgradeApplied is emitted, with the three attributes below, when an
// upgrade applies. It is untyped, outside the protocol event list
// (decision request DR-10-40).
const (
	EventUpgradeApplied = "upgrade_applied"
	AttributeName       = "name"
	AttributeHeight     = "height"
	AttributeAppVersion = "app_version"
)

// maxUnchangedNameBytes bounds the name of one post-condition check.
const maxUnchangedNameBytes = 64

// Runner applies the upgrades of one binary.
type Runner struct {
	binary Binary
	deps   Deps
}

// NewRunner returns the runner for binary b. It refuses a declaration that
// Binary.Validate refuses (ErrInvalidBinary), a missing dependency, no
// post-condition check or more than MaxUnchanged, a check without a name or
// a snapshot, and two checks of one name (ErrInvalidDeps).
func NewRunner(b Binary, deps Deps) (*Runner, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	switch {
	case deps.Plans == nil, deps.Versions == nil, deps.Params == nil, deps.Layouts == nil, deps.Counter == nil, deps.Migrate == nil, deps.Env == nil:
		return nil, errorf(ErrInvalidDeps, nil, "runner dependencies missing (plans %t, versions %t, params %t, layouts %t, counter %t, migrate %t, env %t)",
			deps.Plans != nil, deps.Versions != nil, deps.Params != nil, deps.Layouts != nil, deps.Counter != nil, deps.Migrate != nil, deps.Env != nil)
	case len(deps.Unchanged) == 0:
		return nil, errorf(ErrInvalidDeps, nil, "no post-condition checks (I13)")
	case len(deps.Unchanged) > MaxUnchanged:
		return nil, errorf(ErrInvalidDeps, nil, "%d post-condition checks, at most %d", len(deps.Unchanged), MaxUnchanged)
	}
	seen := make(map[string]bool, len(deps.Unchanged))
	for i, check := range deps.Unchanged {
		switch {
		case check.Name == "" || len(check.Name) > maxUnchangedNameBytes:
			return nil, errorf(ErrInvalidDeps, nil, "post-condition check %d has a name of %d bytes, want 1 to %d", i, len(check.Name), maxUnchangedNameBytes)
		case check.Snapshot == nil:
			return nil, errorf(ErrInvalidDeps, nil, "post-condition check %s has no snapshot", check.Name)
		case seen[check.Name]:
			return nil, errorf(ErrInvalidDeps, nil, "post-condition check %s is declared twice", check.Name)
		}
		seen[check.Name] = true
	}
	b.Modules = cloneVM(b.Modules)
	b.Upgrades = append([]Upgrade(nil), b.Upgrades...)
	deps.Unchanged = append([]Unchanged(nil), deps.Unchanged...)
	return &Runner{binary: b, deps: deps}, nil
}

// Binary returns a copy of the runner's binary declaration.
func (r *Runner) Binary() Binary {
	b := r.binary
	b.Modules = cloneVM(b.Modules)
	b.Upgrades = append([]Upgrade(nil), b.Upgrades...)
	return b
}

// StoredState reads the state the load gate decides on through the
// runner's stores, with lastHeight as L. ctx must read committed state.
func (r *Runner) StoredState(ctx sdk.Context, lastHeight uint64) (StoredState, error) {
	s := StoredState{
		LastHeight: lastHeight,
		AppVersion: r.deps.Versions.AppVersion(ctx),
		Modules:    r.deps.Versions.VersionMap(ctx),
	}
	params, err := r.deps.Params.Get(ctx)
	if err != nil {
		return StoredState{}, errorf(ErrIntegrity, err, "read consensus params")
	}
	if params.Version != nil {
		s.ConsensusAppVersion = params.Version.App
	}
	if s.Layouts, err = r.deps.Layouts.LayoutVersions(ctx); err != nil {
		return StoredState{}, errorf(ErrIntegrity, err, "read layout versions")
	}
	plan, found, err := r.deps.Plans.Plan(ctx)
	if err != nil {
		return StoredState{}, errorf(ErrIntegrity, err, "read upgrade plan")
	}
	if found {
		s.Plan = &plan
	}
	return s, nil
}

// Gate reads the committed state through the runner's stores and returns
// Gate(state, binary). ctx must read committed state.
func (r *Runner) Gate(ctx sdk.Context, lastHeight uint64) (Decision, error) {
	s, err := r.StoredState(ctx, lastHeight)
	if err != nil {
		return Decision{}, err
	}
	return Gate(s, r.binary)
}

// PreBlock applies the per-block rule (CheckBlock) to the block of ctx and,
// at the plan height of an upgrade this binary handles, runs it. It
// returns true only when an upgrade applied, which changed the consensus
// params (Version.App). Any error returns before anything is written to
// ctx: the block fails on this node, and the same binary started again
// repeats the attempt.
func (r *Runner) PreBlock(ctx sdk.Context) (bool, error) {
	if ctx.BlockHeight() < 1 {
		return false, errorf(ErrIntegrity, nil, "block height %d", ctx.BlockHeight())
	}
	height := uint64(ctx.BlockHeight())
	plan, found, err := r.deps.Plans.Plan(ctx)
	if err != nil {
		return false, errorf(ErrIntegrity, err, "read upgrade plan at height %d", height)
	}
	var scheduled *Plan
	if found {
		scheduled = &plan
	}
	up, apply, err := CheckBlock(height, r.deps.Versions.AppVersion(ctx), scheduled, r.binary)
	if err != nil || !apply {
		return false, err
	}
	if err := r.apply(ctx, height, up, plan); err != nil {
		return false, err
	}
	return true, nil
}

// before is what the runner read before the handler.
type before struct {
	appVersion    uint64
	modules       module.VersionMap
	params        cmtproto.ConsensusParams
	paramBytes    []byte
	policyVersion uint64
	snapshots     [][]byte
}

// apply runs upgrade up at height inside a cache context of ctx and writes
// it back only after every step succeeded:
//
//  1. preconditions (preconditions)
//  2. the handler, with a copy of the stored version map, Migrate, and the
//     handler environment (runHandler)
//  3. post-conditions (postconditions)
//  4. the record: RecordUpgrade, Version.App, the policy version bump, and
//     a read-back of each (record), then the upgrade_applied event
//
// Any error returns before the cache is written, so ctx holds nothing of
// the attempt.
func (r *Runner) apply(ctx sdk.Context, height uint64, up Upgrade, plan Plan) error {
	cc, write := ctx.CacheContext()
	pre, err := r.preconditions(cc, up)
	if err != nil {
		return err
	}
	migrated, err := r.runHandler(cc, height, up, pre.modules)
	if err != nil {
		return err
	}
	if err := r.postconditions(cc, up, plan, pre, migrated); err != nil {
		return err
	}
	if err := r.record(cc, height, up, pre, migrated); err != nil {
		return err
	}
	cc.EventManager().EmitEvent(sdk.NewEvent(EventUpgradeApplied,
		sdk.NewAttribute(AttributeName, up.Name),
		sdk.NewAttribute(AttributeHeight, strconv.FormatUint(height, 10)),
		sdk.NewAttribute(AttributeAppVersion, strconv.FormatUint(up.ToAppVersion, 10)),
	))
	write()
	return nil
}

// runHandler calls the handler of up with a copy of the stored version map
// and Migrate, on the cache context cc carrying the handler environment
// (WithEnv), so the handler's store writes go to cc. A handler error, or a
// panic in the handler or in a migration it runs, is returned as an
// ErrHandler error, so PreBlock fails the block through its error return
// and the cache context is never written. Every node runs the same code on
// the same state and fails the same way.
func (r *Runner) runHandler(cc sdk.Context, height uint64, up Upgrade, stored module.VersionMap) (migrated module.VersionMap, err error) {
	defer func() {
		if p := recover(); p != nil {
			migrated, err = nil, errorf(ErrHandler, fmt.Errorf("panic: %v", p), "upgrade %s at height %d", up.Name, height)
		}
	}()
	migrated, err = up.Handler(WithEnv(cc, r.deps.Env), cloneVM(stored), r.deps.Migrate)
	if err != nil {
		return nil, errorf(ErrHandler, err, "upgrade %s at height %d", up.Name, height)
	}
	return migrated, nil
}

// preconditions checks the state the upgrade starts from and takes the
// snapshots. CheckBlock has already matched the handler's app versions to
// the stored app version and the binary's. The module manager's
// RunMigrations initializes a module missing from the stored map, drops a
// stored module the binary lacks, and runs nothing for a stored version
// above the binary's (when the module registered a migration, or its
// binary version is 1), all without an error; the runner refuses each of
// those itself (TestRunMigrationsGapsAreClosedByTheRunner):
//
//   - the stored map has at most MaxModules modules, names exactly the
//     binary's modules, and every entry is in 1..binary version
//   - every layout key equals its stored map entry
//   - the stored consensus params hold Version.App equal to the stored app
//     version
func (r *Runner) preconditions(cc sdk.Context, up Upgrade) (before, error) {
	var pre before
	pre.appVersion = r.deps.Versions.AppVersion(cc)
	pre.modules = r.deps.Versions.VersionMap(cc)
	if len(pre.modules) > MaxModules {
		return before{}, errorf(ErrPrecondition, nil, "stored version map has %d modules, more than %d", len(pre.modules), MaxModules)
	}
	for _, name := range sortedNames(pre.modules) {
		want, ok := r.binary.Modules[name]
		switch stored := pre.modules[name]; {
		case !ok:
			return before{}, errorf(ErrPrecondition, nil, "stored module %s is not in this binary", name)
		case stored < 1 || stored > want:
			return before{}, errorf(ErrPrecondition, nil, "module %s stored version %d is outside 1..%d", name, stored, want)
		}
	}
	for _, name := range sortedNames(r.binary.Modules) {
		if _, ok := pre.modules[name]; !ok {
			return before{}, errorf(ErrPrecondition, nil, "module %s is in this binary but not in the stored version map", name)
		}
	}
	if err := r.checkLayouts(cc, pre.modules, ErrPrecondition); err != nil {
		return before{}, err
	}
	params, err := r.deps.Params.Get(cc)
	if err != nil {
		return before{}, errorf(ErrPrecondition, err, "read consensus params")
	}
	if params.Version == nil || params.Version.App != pre.appVersion {
		return before{}, errorf(ErrPrecondition, nil, "stored consensus Version.App %s differs from the stored app version %d",
			describeVersion(params.Version), pre.appVersion)
	}
	if pre.paramBytes, err = params.Marshal(); err != nil {
		return before{}, errorf(ErrPrecondition, err, "encode consensus params")
	}
	pre.params = params
	pre.policyVersion = r.deps.Counter.PolicyVersion(cc)
	pre.snapshots = make([][]byte, len(r.deps.Unchanged))
	for i, check := range r.deps.Unchanged {
		if pre.snapshots[i], err = check.Snapshot(cc, cloneVM(pre.modules)); err != nil {
			return before{}, errorf(ErrPrecondition, err, "snapshot %s before upgrade %s", check.Name, up.Name)
		}
	}
	return pre, nil
}

// postconditions checks the state the handler left:
//
//   - the returned map is exactly the binary's module map
//   - every layout key equals its entry in the returned map
//   - the handler left the rows the runner owns: the stored app version,
//     the stored version map, the plan, the consensus params, and the
//     policy version
//   - every Unchanged snapshot is byte-equal to the one taken before
func (r *Runner) postconditions(cc sdk.Context, up Upgrade, plan Plan, pre before, migrated module.VersionMap) error {
	if len(migrated) > MaxModules {
		return errorf(ErrPostcondition, nil, "upgrade %s returned %d modules, more than %d", up.Name, len(migrated), MaxModules)
	}
	if name, differ := firstDifference(migrated, r.binary.Modules); differ {
		return errorf(ErrPostcondition, nil, "upgrade %s returned module %s at version %s; this binary declares %s",
			up.Name, name, describeEntry(migrated, name), describeEntry(r.binary.Modules, name))
	}
	if err := r.checkLayouts(cc, migrated, ErrPostcondition); err != nil {
		return err
	}
	if got := r.deps.Versions.AppVersion(cc); got != pre.appVersion {
		return errorf(ErrPostcondition, nil, "upgrade %s handler changed the stored app version from %d to %d", up.Name, pre.appVersion, got)
	}
	if name, differ := firstDifference(r.deps.Versions.VersionMap(cc), pre.modules); differ {
		return errorf(ErrPostcondition, nil, "upgrade %s handler changed the stored version map entry %s", up.Name, name)
	}
	after, found, err := r.deps.Plans.Plan(cc)
	if err != nil {
		return errorf(ErrPostcondition, err, "read upgrade plan after upgrade %s handler", up.Name)
	}
	if !found || after.Name != plan.Name || after.Height != plan.Height || !bytes.Equal(after.Digest, plan.Digest) {
		return errorf(ErrPostcondition, nil, "upgrade %s handler changed or removed the upgrade plan", up.Name)
	}
	params, err := r.deps.Params.Get(cc)
	if err != nil {
		return errorf(ErrPostcondition, err, "read consensus params after upgrade %s handler", up.Name)
	}
	paramBytes, err := params.Marshal()
	if err != nil {
		return errorf(ErrPostcondition, err, "encode consensus params after upgrade %s handler", up.Name)
	}
	if !bytes.Equal(paramBytes, pre.paramBytes) {
		return errorf(ErrPostcondition, nil, "upgrade %s handler changed the consensus params", up.Name)
	}
	if got := r.deps.Counter.PolicyVersion(cc); got != pre.policyVersion {
		return errorf(ErrPostcondition, nil, "upgrade %s handler changed the policy version from %d to %d", up.Name, pre.policyVersion, got)
	}
	for i, check := range r.deps.Unchanged {
		snapshot, err := check.Snapshot(cc, cloneVM(migrated))
		if err != nil {
			return errorf(ErrPostcondition, err, "snapshot %s after upgrade %s handler", check.Name, up.Name)
		}
		if !bytes.Equal(snapshot, pre.snapshots[i]) {
			return errorf(ErrPostcondition, nil, "upgrade %s changed %s", up.Name, check.Name)
		}
	}
	return nil
}

// record writes the upgrade record and reads each row back:
//
//   - RecordUpgrade: the applied row at height, the new version map, the
//     new app version, and the plan deleted
//   - ConsensusParams.Version.App set to the new app version, every other
//     param unchanged
//   - the policy version bumped by one
//   - the plan source no longer reports the upgrade
func (r *Runner) record(cc sdk.Context, height uint64, up Upgrade, pre before, migrated module.VersionMap) error {
	if err := r.deps.Versions.RecordUpgrade(cc, up.Name, height, cloneVM(migrated), up.ToAppVersion); err != nil {
		return errorf(ErrRecord, err, "record upgrade %s", up.Name)
	}
	params := pre.params
	params.Version = &cmtproto.VersionParams{App: up.ToAppVersion}
	if err := r.deps.Params.Set(cc, params); err != nil {
		return errorf(ErrRecord, err, "set consensus Version.App to %d", up.ToAppVersion)
	}
	if err := r.deps.Counter.BumpPolicyVersionForUpgrade(cc); err != nil {
		return errorf(ErrRecord, err, "bump the policy version for upgrade %s", up.Name)
	}

	if got := r.deps.Versions.AppVersion(cc); got != up.ToAppVersion {
		return errorf(ErrRecord, nil, "stored app version is %d after upgrade %s, want %d", got, up.Name, up.ToAppVersion)
	}
	if name, differ := firstDifference(r.deps.Versions.VersionMap(cc), migrated); differ {
		return errorf(ErrRecord, nil, "stored version map entry %s after upgrade %s is not the migrated version", name, up.Name)
	}
	stored, err := r.deps.Params.Get(cc)
	if err != nil {
		return errorf(ErrRecord, err, "read consensus params after upgrade %s", up.Name)
	}
	if stored.Version == nil || stored.Version.App != up.ToAppVersion {
		return errorf(ErrRecord, nil, "stored consensus Version.App is %s after upgrade %s, want %d",
			describeVersion(stored.Version), up.Name, up.ToAppVersion)
	}
	stored.Version = &cmtproto.VersionParams{App: pre.appVersion}
	storedBytes, err := stored.Marshal()
	if err != nil {
		return errorf(ErrRecord, err, "encode consensus params after upgrade %s", up.Name)
	}
	if !bytes.Equal(storedBytes, pre.paramBytes) {
		return errorf(ErrRecord, nil, "consensus params other than Version.App changed at upgrade %s", up.Name)
	}
	if got := r.deps.Counter.PolicyVersion(cc); pre.policyVersion == ^uint64(0) || got != pre.policyVersion+1 {
		return errorf(ErrRecord, nil, "policy version is %d after upgrade %s, want %d + 1", got, up.Name, pre.policyVersion)
	}
	after, found, err := r.deps.Plans.Plan(cc)
	if err != nil {
		return errorf(ErrRecord, err, "read upgrade plan after upgrade %s", up.Name)
	}
	if found && after.Name == up.Name {
		return errorf(ErrRecord, nil, "the plan source still reports upgrade %s after it was recorded", up.Name)
	}
	return nil
}

// checkLayouts returns an error of kind when a layout key differs from its
// entry in vm, or names a module vm lacks.
func (r *Runner) checkLayouts(cc sdk.Context, vm module.VersionMap, kind error) error {
	layouts, err := r.deps.Layouts.LayoutVersions(cc)
	if err != nil {
		return errorf(kind, err, "read layout versions")
	}
	if len(layouts) > MaxModules {
		return errorf(kind, nil, "%d layout keys, more than %d", len(layouts), MaxModules)
	}
	for _, name := range sortedNames(layouts) {
		want, ok := vm[name]
		switch layout := layouts[name]; {
		case !ok:
			return errorf(kind, nil, "%s store has a layout key, but the version map has no %s entry", name, name)
		case layout != want:
			return errorf(kind, nil, "%s store has layout version %d; the version map says %d", name, layout, want)
		}
	}
	return nil
}

func describeVersion(v *cmtproto.VersionParams) string {
	if v == nil {
		return "(absent)"
	}
	return strconv.FormatUint(v.App, 10)
}

func describeEntry(vm module.VersionMap, name string) string {
	v, ok := vm[name]
	if !ok {
		return "(absent)"
	}
	return strconv.FormatUint(v, 10)
}
