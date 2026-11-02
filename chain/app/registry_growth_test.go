package app_test

import (
	"fmt"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// disableAfterRotations runs one chain: register and admit p1, rotate its
// key rotations times, then disable it through the council. It returns the
// gas the committed ExecuteChange used. Key ids, business ids, and
// proposal ids have the same length in every run, and every height stays
// below 128, so only the rotation count differs between two runs.
func disableAfterRotations(t *testing.T, rotations int) int64 {
	t.Helper()
	n := newRegNet()
	c := n.start(t, 1)
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	n.council(t, c, "prop-admit-p1", "admit-p1", admitPayload("p1"))
	for i := 1; i <= rotations; i++ {
		next := newSigningKey(fmt.Sprintf("p1-k%04d", i))
		wantCode(t, fmt.Sprintf("rotate %d", i), n.send(t, c, n.provider1, n.rotate(t, n.provider1, fmt.Sprintf("rot-%04d", i), "p1", uint64(i), next)), msgkit.OK)
		n.signing["p1"] = next
	}
	res := n.council(t, c, "prop-disable-p1", "disable-p1", disablePayload("p1"))
	if c.height() >= 128 {
		t.Fatalf("height %d: a longer height varint would change the gas", c.height())
	}
	p1, _ := queryProvider(t, c.app(), "p1", 0)
	if p1.GetState() != providersv1.ProviderState_PROVIDER_STATE_DISABLED || len(p1.GetPreviousSigningKeys()) != rotations {
		t.Fatalf("p1 after disable: state %v, %d retired keys", p1.GetState(), len(p1.GetPreviousSigningKeys()))
	}
	return res.GasUsed
}

// TestCouncilDisableGasDoesNotGrowWithRotations is the app-level regression
// for a provider that rotates until the council cannot disable it. The
// council's committed ExecuteChange(disable_provider) uses the same gas
// after 60 rotations as after 2. With the retired keys inside the record,
// 58 more rotations would have added about 2,600 bytes to the record that
// the disable reads and rewrites, about 86,000 gas.
func TestCouncilDisableGasDoesNotGrowWithRotations(t *testing.T) {
	few := disableAfterRotations(t, 2)
	many := disableAfterRotations(t, 60)
	t.Logf("ExecuteChange(disable) gas: %d after 2 rotations, %d after 60", few, many)
	if many > few+100 || few > many+100 {
		t.Fatalf("ExecuteChange(disable) used %d gas after 2 rotations and %d after 60", few, many)
	}
}
