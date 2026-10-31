package devnet_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/node"
)

func TestMain(m *testing.M) {
	if os.Getenv("SVN_HELPER") == "start" {
		if err := node.Start(os.Getenv("SVN_HOME")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestInitRefusesToReplaceGenesis(t *testing.T) {
	home := t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: home, ChainID: "shortvideo-dev-init"})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Topology.GenesisSHA256 == "" || len(layout.Topology.Validators) != 4 {
		t.Fatalf("%+v", layout.Topology)
	}
	if _, err := devnet.Init(devnet.Options{Home: home}); err == nil {
		t.Fatal("second init replaced the home")
	}
	if _, err := os.Stat(filepath.Join(home, "nodes", "v1", "config", "genesis.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFourValidatorsCommitRestartAndQuorum(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	home := t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	stops := make([]func() error, 4)
	start := func(index int) {
		t.Helper()
		stop, err := launch(layout.Topology.Validators[index].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[index] = stop
	}
	for i := range stops {
		start(i)
	}
	t.Cleanup(func() {
		for _, stop := range stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	rpcs := rpcList(layout)
	startHeight, startHash, err := devnet.WaitEqual(rpcs, 3, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	logPhase(t, "start", rpcs)
	beforeStop, err := devnet.Agreed(rpcs)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	fmt.Printf("phase=stop-all threshold=%d app_hash=%s\n", beforeStop.Height, beforeStop.AppHash)
	for _, stop := range stops {
		if err := stop(); err != nil {
			t.Fatal(err)
		}
	}
	stops = make([]func() error, 4)
	for i := range stops {
		start(i)
	}
	resumed, _, err := devnet.WaitAbove(rpcs, beforeStop.Height, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	if resumed <= beforeStop.Height {
		t.Fatalf("restart did not pass height %d (at %d)", beforeStop.Height, resumed)
	}
	logPhase(t, "restart", rpcs)
	preserved, err := devnet.BlockHash(rpcs[0], startHeight)
	if err != nil {
		t.Fatal(err)
	}
	if preserved != startHash {
		t.Fatalf("height %d hash changed from %s to %s", startHeight, startHash, preserved)
	}
	beforeOne, err := devnet.Agreed(rpcs)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	fmt.Printf("phase=stop-one threshold=%d app_hash=%s\n", beforeOne.Height, beforeOne.AppHash)
	if err := stops[3](); err != nil {
		t.Fatal(err)
	}
	stops[3] = nil
	oneDown, _, err := devnet.WaitAbove(rpcs[:3], beforeOne.Height, 30*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	if oneDown <= beforeOne.Height {
		t.Fatalf("one validator down did not pass height %d (at %d)", beforeOne.Height, oneDown)
	}
	observeMoving(t, "one-down", rpcs[:3], beforeOne.Height)
	beforeTwo, err := devnet.Agreed(rpcs[:3])
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	fmt.Printf("phase=stop-two threshold=%d app_hash=%s\n", beforeTwo.Height, beforeTwo.AppHash)
	if err := stops[2](); err != nil {
		t.Fatal(err)
	}
	stops[2] = nil
	stall := observeStall(t, "two-down", rpcs[:2])
	if stall < beforeTwo.Height {
		t.Fatalf("stall height %d is below the height at stop %d", stall, beforeTwo.Height)
	}
	start(2)
	start(3)
	recovered, _, err := devnet.WaitAbove(rpcs, stall, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	if recovered <= stall {
		t.Fatalf("recovery did not pass stall height %d (at %d)", stall, recovered)
	}
	logPhase(t, "recovery", rpcs)
}

func logPhase(t *testing.T, phase string, rpcs []string) {
	t.Helper()
	rows, err := devnet.Snapshot(rpcs)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		fmt.Printf("phase=%s sample=0 node=%d height=%d app_hash=%s\n", phase, i, row.Height, row.AppHash)
	}
}

func observeMoving(t *testing.T, phase string, rpcs []string, threshold int64) {
	t.Helper()
	var first int64
	var last int64
	for sample := 0; sample < 4; sample++ {
		rows, err := devnet.Snapshot(rpcs)
		if err != nil {
			t.Fatal(err)
		}
		low := rows[0].Height
		for i, row := range rows {
			if row.AppHash != rows[0].AppHash {
				t.Fatalf("phase %s app hash disagree", phase)
			}
			if row.Height < low {
				low = row.Height
			}
			fmt.Printf("phase=%s sample=%d node=%d height=%d app_hash=%s\n", phase, sample, i, row.Height, row.AppHash)
		}
		if sample == 0 {
			first = low
		}
		last = low
		if last < first {
			t.Fatalf("phase %s height moved backward", phase)
		}
		time.Sleep(time.Second)
	}
	if last <= threshold || last <= first {
		t.Fatalf("phase %s did not keep committing: threshold %d first %d last %d", phase, threshold, first, last)
	}
}

func observeStall(t *testing.T, phase string, rpcs []string) int64 {
	t.Helper()
	var height int64
	for sample := 0; sample < 6; sample++ {
		rows, err := devnet.Snapshot(rpcs)
		if err != nil {
			t.Fatal(err)
		}
		for i, row := range rows {
			if row.Height != rows[0].Height || row.AppHash != rows[0].AppHash {
				t.Fatalf("phase %s nodes disagree", phase)
			}
			fmt.Printf("phase=%s sample=%d node=%d height=%d app_hash=%s\n", phase, sample, i, row.Height, row.AppHash)
		}
		if sample == 0 {
			height = rows[0].Height
		}
		if rows[0].Height != height {
			t.Fatalf("phase %s height moved from %d to %d", phase, height, rows[0].Height)
		}
		time.Sleep(time.Second)
	}
	return height
}

func launch(home string) (func() error, error) {
	logFile, err := os.Create(filepath.Join(home, "node.log"))
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "SVN_HELPER=start", "SVN_HOME="+home)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	return func() error {
		defer logFile.Close()
		if cmd.Process == nil {
			return nil
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			return fmt.Errorf("validator did not stop")
		case err := <-done:
			if err == nil || isSignal(err) {
				return nil
			}
			return err
		}
	}, nil
}

func isSignal(err error) bool {
	exit, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	return exit.ExitCode() == 143 || exit.ExitCode() == -1
}

func rpcList(layout devnet.Layout) []string {
	out := make([]string, len(layout.Topology.Validators))
	for i, node := range layout.Topology.Validators {
		out[i] = node.RPC
	}
	return out
}

func tail(layout devnet.Layout, err error) error {
	var text string
	for _, node := range layout.Topology.Validators {
		raw, readErr := os.ReadFile(filepath.Join(node.Home, "node.log"))
		if readErr != nil {
			continue
		}
		if len(raw) > 2000 {
			raw = raw[len(raw)-2000:]
		}
		text += "\n--- " + node.Name + " ---\n" + string(raw)
	}
	return fmt.Errorf("%w%s", err, text)
}
