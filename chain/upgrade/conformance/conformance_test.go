// Package conformance_test checks that the chain types the app will hand
// to chain/upgrade have the method sets its interfaces name, and that the
// version-map bounds of chain/upgrade and x/policy agree. It is separate
// from chain/upgrade's own tests so that package builds and tests without
// the module keepers.
package conformance_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
)

// Compile-time checks: the x/policy keeper is a VersionStore, the
// x/authority keeper is a PolicyCounter, and the baseapp consensus param
// store interface (what the app's param store returns) is a
// ConsensusParamStore.
var (
	_ upgrade.VersionStore        = policy.Keeper{}
	_ upgrade.PolicyCounter       = authority.Keeper{}
	_ upgrade.ConsensusParamStore = baseapp.ParamStore(nil)
)

// TestVersionMapBoundsAgree: the runner and the gate accept at most as many
// modules as x/policy persists, and no fewer.
func TestVersionMapBoundsAgree(t *testing.T) {
	if upgrade.MaxModules != policy.MaxVersionMapModules {
		t.Fatalf("upgrade.MaxModules %d, policy.MaxVersionMapModules %d", upgrade.MaxModules, policy.MaxVersionMapModules)
	}
}
