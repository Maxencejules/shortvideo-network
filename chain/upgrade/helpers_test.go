package upgrade_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// migrateThrough is a handler that only calls Migrate, as a production
// handler without extra steps does.
func migrateThrough(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
	return migrate(ctx, from)
}

// up returns an upgrade from app version from to to with migrateThrough.
func up(name string, from, to uint64) upgrade.Upgrade {
	return upgrade.Upgrade{Name: name, FromAppVersion: from, ToAppVersion: to, Handler: migrateThrough}
}

// digest is a synthetic 32-byte release digest.
func digest(b byte) []byte { return bytes.Repeat([]byte{b}, upgrade.DigestBytes) }

// vm copies a version map literal, so a row can edit its own copy.
func vm(m module.VersionMap) module.VersionMap {
	out := module.VersionMap{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// wantKind fails unless err wraps kind and its message contains every part.
func wantKind(t *testing.T, name string, err, kind error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: no error, want %v", name, kind)
	}
	if !errors.Is(err, kind) {
		t.Fatalf("%s: error %q does not wrap %v", name, err, kind)
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("%s: error %q does not contain %q", name, err, part)
		}
	}
}
