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
	height, hash, err := devnet.WaitEqual(rpcs, 3, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	for _, stop := range stops {
		if err := stop(); err != nil {
			t.Fatal(err)
		}
	}
	stops = make([]func() error, 4)
	for i := range stops {
		start(i)
	}
	resumed, _, err := devnet.WaitAbove(rpcs, height, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	preserved, err := devnet.BlockHash(rpcs[0], height)
	if err != nil {
		t.Fatal(err)
	}
	if preserved != hash {
		t.Fatalf("height %d hash changed from %s to %s", height, hash, preserved)
	}
	if err := stops[3](); err != nil {
		t.Fatal(err)
	}
	stops[3] = nil
	if _, _, err := devnet.WaitAbove(rpcs[:3], resumed, 30*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	if err := stops[2](); err != nil {
		t.Fatal(err)
	}
	stops[2] = nil
	if _, err := devnet.Stable(rpcs[:2], 6*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	start(2)
	start(3)
	if _, _, err := devnet.WaitAbove(rpcs, height, 45*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
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
