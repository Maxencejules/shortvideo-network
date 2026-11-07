package upgrade

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Env is the handler environment: by-name access to the module stores of
// the app that runs an upgrade, as an SDK upgrade handler reaches them
// through the keepers it captures. The app builds it from its mounted store
// keys and passes it to NewRunner (Deps.Env); the runner gives it to every
// handler it calls through the handler's context (WithEnv), and the handler
// reads it with EnvOf.
//
// A handler may read and write any store through it. The runner's checks
// after the handler are the same for these writes as for any other: a
// change to a row the runner owns or to an Unchanged class (the I13
// self-check) refuses the upgrade, and nothing of the attempt is written.
// A write to any other row is kept when every check passes.
type Env interface {
	// StoreNames returns the names of the mounted stores in byte order.
	StoreNames() []string
	// KVStore returns the store named name in the multistore of ctx, so
	// reads see and writes go to the branch ctx holds. found is false when
	// no store of that name is mounted.
	KVStore(ctx sdk.Context, name string) (store storetypes.KVStore, found bool)
}

// envKey is the context key of the handler environment.
type envKey struct{}

// WithEnv returns ctx carrying env as the handler environment. The runner
// calls a handler with the context WithEnv returns; the context keeps the
// multistore of ctx, so the handler's writes go to the same branch.
func WithEnv(ctx sdk.Context, env Env) sdk.Context {
	return ctx.WithValue(envKey{}, env)
}

// EnvOf returns the handler environment ctx carries (WithEnv). found is
// false for a context the runner did not give to a handler.
func EnvOf(ctx sdk.Context) (env Env, found bool) {
	env, found = ctx.Value(envKey{}).(Env)
	return env, found && env != nil
}
