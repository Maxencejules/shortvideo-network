package devnet_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// TestInitCouncilDelayAndAppVersion: the genesis Init writes carries
// consensus Version.App app.GenesisAppVersion and the DEC-008 devnet test
// parameter (label PROPOSED_TEST_PARAMETER) with the review delay of
// Options.CouncilDelaySeconds: 0 by default, the requested value when set,
// and up to genesis.UnixSecondsMax. A negative delay or one above that bound
// is refused before any file is written.
func TestInitCouncilDelayAndAppVersion(t *testing.T) {
	for _, c := range []struct {
		name  string
		delay int64
	}{
		{"default", 0},
		{"set", 30},
		{"maximum", genesis.UnixSecondsMax},
	} {
		t.Run(c.name, func(t *testing.T) {
			layout, err := devnet.Init(devnet.Options{Home: t.TempDir(), ChainID: "shortvideo-dev-delay", CouncilDelaySeconds: c.delay})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(layout.Genesis)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := cmttypes.GenesisDocFromJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			if doc.ConsensusParams.Version.App != app.GenesisAppVersion {
				t.Fatalf("genesis Version.App %d, want %d", doc.ConsensusParams.Version.App, app.GenesisAppVersion)
			}
			var modules map[string]json.RawMessage
			if err := json.Unmarshal(doc.AppState, &modules); err != nil {
				t.Fatal(err)
			}
			state, err := app.DocumentFromModules(modules)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, row := range state.DevnetTestParameters {
				if row.DecisionID != "DEC-008" {
					continue
				}
				found++
				if row.Label != genesis.DevnetTestLabel || row.NumericValue != 3 || row.DelaySeconds != c.delay {
					t.Fatalf("DEC-008 row %+v, want label %s, threshold 3, delay %d", row, genesis.DevnetTestLabel, c.delay)
				}
			}
			if found != 1 {
				t.Fatalf("%d DEC-008 rows", found)
			}
		})
	}
	for _, delay := range []int64{-1, genesis.UnixSecondsMax + 1} {
		home := filepath.Join(t.TempDir(), "home")
		_, err := devnet.Init(devnet.Options{Home: home, ChainID: "shortvideo-dev-delay", CouncilDelaySeconds: delay})
		if err == nil || !strings.Contains(err.Error(), "council delay") {
			t.Fatalf("delay %d: %v", delay, err)
		}
		if _, statErr := os.Stat(home); !os.IsNotExist(statErr) {
			t.Fatalf("delay %d: Init wrote %s (%v)", delay, home, statErr)
		}
	}
}
