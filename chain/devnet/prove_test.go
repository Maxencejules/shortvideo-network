package devnet_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"

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
	// The threshold is not read before the stops. A block committed while
	// the processes are shutting down used to satisfy a threshold+1 check.
	for _, stop := range stops {
		if err := stop(); err != nil {
			t.Fatal(err)
		}
	}
	stops = make([]func() error, 4)
	for i := range stops {
		start(i)
	}
	up, _, err := devnet.WaitEqual(rpcs, 1, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	fmt.Printf("phase=restart-up height=%d\n", up)
	// Two committed blocks after the validators are observed up.
	resumed, _, err := devnet.WaitAbove(rpcs, up+1, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	if resumed < up+2 {
		t.Fatalf("restart did not produce two blocks after height %d (at %d)", up, resumed)
	}
	logPhase(t, "restart", rpcs)
	preserved, err := devnet.BlockHash(rpcs[0], startHeight)
	if err != nil {
		t.Fatal(err)
	}
	if preserved != startHash {
		t.Fatalf("height %d hash changed from %s to %s", startHeight, startHash, preserved)
	}
	beforeOne, err := devnet.HighWater(rpcs)
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
	// stopForStall reads the threshold over v1, v2 and v3, the processes
	// still running, and then stops v3.
	two, err := stopForStall("stop-two", layout, stops, 2)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	// Four validators of equal power: v1 and v2 alone are not a quorum, so a
	// block above the threshold commits only with v3's precommit, cast before
	// its process stopped. v4 was stopped at stop-one and signs nothing.
	stall := observeStall(t, "two-down", two, stoppedSigned(t, consensusKeys(t, layout), two))
	start(2)
	start(3)
	// stall+1 is not enough: a block already in flight can satisfy it.
	// Wait until the restarted validators are up past the stall, then
	// require two further blocks.
	up, _, err = devnet.WaitEqual(rpcs, stall+1, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	fmt.Printf("phase=recovery-up height=%d stall=%d\n", up, stall)
	recovered, _, err := devnet.WaitAbove(rpcs, up+1, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	if recovered < up+2 {
		t.Fatalf("recovery did not produce two blocks after height %d (at %d)", up, recovered)
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

// stallLimit decides one sample of one node in a stall phase. It does no I/O
// apart from calling inFlight. threshold is the high-water mark of the nodes
// read immediately before the stop. limit is the highest height the phase
// allows so far: threshold at the start, threshold+1 once the in-flight block
// is verified. It returns the limit for the next sample.
//
//   - height <= limit is allowed. A node below threshold is catching up to a
//     block that was committed before the stop; that makes no new block.
//   - height == threshold+1 while limit == threshold is the one block decided
//     before the stop and committed after it. The running validators alone
//     are not a quorum, so its commit must carry the precommit of the
//     validator just stopped; inFlight(threshold+1) checks that. When it
//     returns nil the limit becomes threshold+1.
//   - Anything else is an error that names phase, node, height, threshold and
//     limit. A second block above threshold+1 is never allowed, and neither
//     is an in-flight block whose check fails or that has no check.
func stallLimit(phase string, node int, threshold, limit, height int64, inFlight func(int64) error) (int64, error) {
	if limit != threshold && limit != threshold+1 {
		return limit, fmt.Errorf("phase %s node %d height %d: limit %d is neither threshold %d nor threshold+1", phase, node, height, limit, threshold)
	}
	if height <= limit {
		return limit, nil
	}
	if height == threshold+1 && limit == threshold {
		if inFlight == nil {
			return limit, fmt.Errorf("phase %s node %d height %d is above threshold %d (limit %d) and the phase has no in-flight check", phase, node, height, threshold, limit)
		}
		if err := inFlight(height); err != nil {
			return limit, fmt.Errorf("phase %s node %d height %d is above threshold %d (limit %d) and is not the block in flight at the stop: %w", phase, node, height, threshold, limit, err)
		}
		return height, nil
	}
	return limit, fmt.Errorf("phase %s node %d height %d is above limit %d (threshold %d): at most one block, decided before the stop, may commit after it", phase, node, height, limit, threshold)
}

// stallStart is what a stall phase starts from. stopForStall makes it, so the
// threshold, the stopped validator and the running nodes have one source.
type stallStart struct {
	// threshold is the high-water mark of every process that was running,
	// the one then stopped included, read immediately before the stop.
	threshold devnet.Status
	// stopped is the name of the validator whose process was stopped.
	stopped string
	// running and names are the RPCs and the validator names of the
	// processes still running after the stop, in topology order.
	running []string
	names   []string
	// stoppedAt is the wall time at which the stop returned. The process had
	// exited by then (launch waits for it), so it cast no vote after it.
	stoppedAt time.Time
}

// stopForStall starts a stall phase. It reads the high-water mark of every
// validator whose stops entry is set, index included, prints it as
// phase=<label> threshold=, and only then stops index and clears its entry.
// It does nothing else between the read and the stop. If the stop fails the
// entry is kept, so the test's cleanup still stops the process.
func stopForStall(label string, layout devnet.Layout, stops []func() error, index int) (stallStart, error) {
	nodes := layout.Topology.Validators
	if len(stops) != len(nodes) {
		return stallStart{}, fmt.Errorf("%s: %d stop functions for %d validators", label, len(stops), len(nodes))
	}
	if index < 0 || index >= len(nodes) || stops[index] == nil {
		return stallStart{}, fmt.Errorf("%s: validator index %d has no running process to stop", label, index)
	}
	var before []string
	for i, stop := range stops {
		if stop != nil {
			before = append(before, nodes[i].RPC)
		}
	}
	mark, err := devnet.HighWater(before)
	if err != nil {
		return stallStart{}, fmt.Errorf("%s: %w", label, err)
	}
	fmt.Printf("phase=%s threshold=%d app_hash=%s\n", label, mark.Height, mark.AppHash)
	if err := stops[index](); err != nil {
		return stallStart{}, fmt.Errorf("%s: stop %s: %w", label, nodes[index].Name, err)
	}
	out := stallStart{threshold: mark, stopped: nodes[index].Name, stoppedAt: time.Now()}
	stops[index] = nil
	for i, stop := range stops {
		if stop != nil {
			out.running = append(out.running, nodes[i].RPC)
			out.names = append(out.names, nodes[i].Name)
		}
	}
	return out, nil
}

// inFlightCheck verifies the commit at height, read from rpc, the node that
// reported that height. It returns the commit's signers and an error unless
// the commit is the block in flight at the stop (stoppedSigned).
type inFlightCheck func(rpc string, height int64) ([]string, error)

// stallSamples and stallInterval are observeStall's sampling: six samples
// about a second apart.
const (
	stallSamples  = 6
	stallInterval = time.Second
)

// observeStall samples the nodes still running after start's stop six times,
// about a second apart, and fails the test unless every sample satisfies
// stallLimit for start's threshold; check verifies the one block allowed
// above it. It returns the greatest height observed. watchStall does the work.
func observeStall(t *testing.T, phase string, start stallStart, check inFlightCheck) int64 {
	t.Helper()
	stalled, err := watchStall(phase, start, check, stallSamples, stallInterval)
	if err != nil {
		t.Fatal(err)
	}
	return stalled
}

// watchStall is observeStall with the sampling as parameters and the failure
// as an error, so a unit test can drive it against a fake RPC.
func watchStall(phase string, start stallStart, check inFlightCheck, samples int, interval time.Duration) (int64, error) {
	threshold := start.threshold.Height
	limit := threshold
	var stalled int64
	for sample := 0; sample < samples; sample++ {
		rows, err := devnet.Snapshot(start.running)
		if err != nil {
			return stalled, fmt.Errorf("phase %s sample %d: %w", phase, sample, err)
		}
		for i, row := range rows {
			// Nodes at different heights are compared too, and that is
			// correct here: no transaction is sent and no authority change
			// is due in a stall window. The authority EndBlock writes only
			// when a change is due, the auth PreBlock only removes expired
			// unordered nonces (these tests create none), and no other
			// module has a block hook. So the header app hash is the same at
			// every height of the window; a difference is a divergence.
			// HighWater, which reads the threshold, relies on the same fact.
			if row.AppHash != rows[0].AppHash {
				return stalled, fmt.Errorf("phase %s app hash disagree", phase)
			}
			fmt.Printf("phase=%s sample=%d node=%d height=%d app_hash=%s\n", phase, sample, i, row.Height, row.AppHash)
			var signers []string
			var inFlight func(int64) error
			if check != nil {
				rpc := start.running[i]
				inFlight = func(height int64) error {
					var err error
					signers, err = check(rpc, height)
					return err
				}
			}
			next, err := stallLimit(phase, i, threshold, limit, row.Height, inFlight)
			if err != nil {
				return stalled, err
			}
			if next != limit {
				fmt.Printf("phase=%s-in-flight sample=%d node=%d height=%d threshold=%d signers=%s\n", phase, sample, i, next, threshold, strings.Join(signers, ","))
				limit = next
			}
			if row.Height > stalled {
				stalled = row.Height
			}
		}
		time.Sleep(interval)
	}
	return stalled, nil
}

// stoppedSigned is the in-flight check of the stall phase that start began.
// The commit at the height must carry a BlockIDFlagCommit signature of
// start.stopped, timestamped no later than start.stoppedAt; every signer must
// be start.stopped or a validator whose process still runs (start.names), so
// a validator with no process fails it; and the signers must hold more than
// two thirds of the power of that height's validator set. keys maps validator
// names to consensus keys; a signer missing from keys fails the check.
//
// What it shows, and what it does not. In every stall phase the running
// validators alone are not a quorum, so once the signer rule holds the power
// rule already forces start.stopped into the commit: requiring it by name
// adds no separate condition, and naming the wrong stopped validator is
// caught by the unit tests of stopForStall, not by a devnet run. The
// signature shows the commit is consistent with the stopped validator's
// vote. The timestamp shows that vote was cast before the stop returned. A
// commit made after the stop by a validator wrongly believed stopped fails
// the timestamp rule. None of this shows the block was proposed before the
// threshold was read. stallLimit allows one block above the threshold either
// way, and never a second one.
func stoppedSigned(t *testing.T, keys map[string][]byte, start stallStart) inFlightCheck {
	return func(rpc string, height int64) ([]string, error) {
		t.Helper()
		names, signed, total := commitSigners(t, rpc, height, keys)
		if !slices.Contains(names, start.stopped) {
			return names, fmt.Errorf("commit %d signers %v do not include %s, the validator just stopped", height, names, start.stopped)
		}
		for _, who := range names {
			if who != start.stopped && !slices.Contains(start.names, who) {
				return names, fmt.Errorf("commit %d is signed by %s, which has no running process and is not %s, the validator just stopped", height, who, start.stopped)
			}
		}
		if signed*3 <= total*2 {
			return names, fmt.Errorf("commit %d signed power %d of %d", height, signed, total)
		}
		at, err := precommitTime(t, rpc, height, keys[start.stopped])
		if err != nil {
			return names, err
		}
		if at.After(start.stoppedAt) {
			return names, fmt.Errorf("commit %d: %s precommitted at %s, after its process had stopped at %s", height, start.stopped, at.UTC().Format(time.RFC3339Nano), start.stoppedAt.UTC().Format(time.RFC3339Nano))
		}
		return names, nil
	}
}

// precommitTime returns the timestamp of the BlockIDFlagCommit signature of
// the validator with consensus key pub in the commit for height. CometBFT
// v0.40 sets it to the later of the voter's clock and the block time plus
// 1ms (consensus State.voteTime). The block time is the median of the
// previous height's precommit times, all earlier, so the vote time is the
// voter's clock when it signed. Every devnet process and the test share one
// host clock.
func precommitTime(t *testing.T, rpc string, height int64, pub []byte) (time.Time, error) {
	t.Helper()
	address := hex.EncodeToString(cmted25519.PubKey(pub).Address())
	for _, sig := range readCommit(t, rpc, height) {
		if sig.Flag == blockIDFlagCommit && strings.EqualFold(sig.Address, address) {
			at, err := time.Parse(time.RFC3339Nano, sig.Timestamp)
			if err != nil {
				return time.Time{}, fmt.Errorf("commit %d signature of %s has timestamp %q: %w", height, address, sig.Timestamp, err)
			}
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("commit %d has no signature for the block from %s", height, address)
}

// consensusKeys maps each topology validator name to its consensus public key.
func consensusKeys(t *testing.T, layout devnet.Layout) map[string][]byte {
	t.Helper()
	keys := map[string][]byte{}
	for _, node := range layout.Topology.Validators {
		raw, err := hex.DecodeString(node.ConsensusPubkeyHex)
		if err != nil {
			t.Fatal(err)
		}
		keys[node.Name] = raw
	}
	return keys
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
