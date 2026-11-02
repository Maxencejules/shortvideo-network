package devnet_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
)

// devnetPower is the genesis power of each of the four processes.
const devnetPower = 10

// TestDevnetQuorumFollowsVotingPower runs on a single-machine, multi-process
// devnet: four validator processes on loopback, not independent operators.
//
// Genesis gives v1..v4 power 10. Two council proposals, executed in this
// order and sharing one activation height, add v5 with power 1 and no
// process, then lower v3 to power 2. The set is v1:10 v2:10 v3:2 v4:10 v5:1,
// total 33. Each execute passes the DEC-011 projection: 10,10,10,10,1 (41)
// and then the group 10,10,2,10,1 (33); 30 < 33.
//
// Two cases stop one process each. Both leave three of five validators
// online:
//
//	X: v3 (2) and v5 (1) offline; 30 of 33 online; 30*3 > 66.
//	Y: v1 (10) and v5 (1) offline; 22 of 33 online; 22*3 = 66.
//
// CometBFT commits a block only with precommits from more than two thirds of
// the voting power (types/vote_set.go: quorum is total*2/3+1, 23 here). A
// rule that counts validators predicts a stall in both cases. The test
// observes each case from committed blocks, in the same way, and asserts that
// the outcome follows the power rule: X commits, Y stalls.
//
// The online validators of a case are observed, not taken from the processes
// the test runs: in X they are the signers of the commits after the stop; in
// Y they are the validators whose votes the running nodes hold at the height
// that does not commit. Each must equal the validators whose process runs.
// Before the next case, every restarted process must sign a commit.
func TestDevnetQuorumFollowsVotingPower(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	home := t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: home, Power: devnetPower})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string][]byte{}
	for _, node := range layout.Topology.Validators {
		if node.Power != devnetPower {
			t.Fatalf("%s topology power %d", node.Name, node.Power)
		}
		raw, err := hex.DecodeString(node.ConsensusPubkeyHex)
		if err != nil {
			t.Fatal(err)
		}
		keys[node.Name] = raw
	}
	keys["v5"] = sdked.GenPrivKey().PubKey().Bytes()

	stops := make([]func() error, len(layout.Topology.Validators))
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
	if _, _, err := devnet.WaitEqual(rpcs, 3, 45*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	rpc := rpcs[0]
	genesisWant := map[string]int64{"v1": 10, "v2": 10, "v3": 10, "v4": 10}
	requireSet(t, "genesis", validatorSet(t, rpc, 1), keys, genesisWant)

	enc := mustEncoding(t)
	signers := loadSigners(t, home)
	numbers := signerNumbers(signers)
	seq := map[string]uint64{}
	heightNow := mustHeight(t, rpc)
	activation := uint64(heightNow + 100)
	effective := int64(activation) + authority.CometEffectiveDelay
	fmt.Printf("phase=power-plan height=%d activation=%d effective=%d\n", heightNow, activation, effective)
	// Policy version 1 at genesis; each executed proposal adds one.
	submitPower(t, enc, layout.Topology.ChainID, rpc, signers, numbers, seq, "pq-add-v5", 1, "v5", keys["v5"], 1, activation)
	submitPower(t, enc, layout.Topology.ChainID, rpc, signers, numbers, seq, "pq-low-v3", 2, "v3", keys["v3"], 2, activation)
	executed := mustHeight(t, rpc)
	fmt.Printf("phase=power-executed height=%d activation=%d\n", executed, activation)
	if _, _, err := devnet.WaitEqual(rpcs, effective, 120*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	finalWant := map[string]int64{"v1": 10, "v2": 10, "v3": 2, "v4": 10, "v5": 1}
	oldSet := validatorSet(t, rpc, effective-1)
	newSet := validatorSet(t, rpc, effective)
	requireSet(t, "effective-1", oldSet, keys, genesisWant)
	requireSet(t, "effective", newSet, keys, finalWant)
	fmt.Printf("phase=power-set effective-1=%d powers=%s effective=%d powers=%s\n", effective-1, describeSet(oldSet, keys), effective, describeSet(newSet, keys))

	x := runPowerCase(t, layout, rpcs, stops, keys, finalWant, "X", 2)
	start(2)
	recoverAll(t, layout, rpcs, keys, "X", x.to)

	y := runPowerCase(t, layout, rpcs, stops, keys, finalWant, "Y", 0)
	start(0)
	recoverAll(t, layout, rpcs, keys, "Y", y.to)

	if x.observed != x.powerRule || x.observed == x.countRule || x.observed != "progress" {
		t.Fatalf("case X observed %s, power rule %s, count rule %s", x.observed, x.powerRule, x.countRule)
	}
	if y.observed != y.powerRule || y.observed != "stall" {
		t.Fatalf("case Y observed %s, power rule %s", y.observed, y.powerRule)
	}
	// The online counts are observed: commit signers in X, votes at the
	// height that does not commit in Y. Each case already required its
	// observed voters to be exactly its running processes, so this check
	// restates that comparison for the two cases together; it adds no
	// separate observation.
	if x.validators != y.validators || x.onlineValidators != y.onlineValidators {
		t.Fatalf("X has %d of %d validators online, Y has %d of %d: the counts must be equal",
			x.onlineValidators, x.validators, y.onlineValidators, y.validators)
	}
}

// powerCase is one stop-and-observe case. Powers come from the RPC validator
// set. The online validators come from observed votes: commit signatures when
// blocks commit, consensus votes at the stuck height when they do not.
// observed comes from committed blocks.
type powerCase struct {
	name             string
	offline          []string
	onlinePower      int64
	totalPower       int64
	onlineValidators int
	validators       int
	countRule        string
	powerRule        string
	observed         string
	from             int64
	to               int64
	setHeight        int64
}

// runPowerCase stops validator process index and observes the running nodes.
// The observation is the same in every case: progress is every running node
// agreeing at a height at least from+3 within the window, with the commits of
// from+2 and later signed without the validators that have no running
// process. Stall is no such height, then six samples (observeStall) at or
// below the high-water mark read immediately before the stop, except one
// block above it that was in flight at the stop, whose commit must carry the
// stopped validator's signature; and at most one block past from.
//
// The online set is what was observed voting during the case: the signers of
// those commits for progress, the votes held at the stuck height for a stall
// (observeVoters). It must equal the validators whose process runs.
func runPowerCase(t *testing.T, layout devnet.Layout, rpcs []string, stops []func() error, keys map[string][]byte, want map[string]int64, name string, index int) powerCase {
	t.Helper()
	if _, _, err := devnet.WaitEqual(rpcs, 1, 45*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	phase := "power-case-" + name
	// The stall threshold: the high-water mark of every process, the one
	// about to stop included, read immediately before the stop.
	at, err := stopForStall(phase+"-stop", layout, stops, index)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	stopped, running, names := at.stopped, at.running, at.names
	from, err := devnet.HighWater(running)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	// observeStall numbers the running nodes 0.. in this order.
	fmt.Printf("phase=%s stopped=%s running=%s height_from=%d\n", phase, stopped, strings.Join(names, ","), from.Height)
	out := powerCase{name: name, from: from.Height}
	const window = 30 * time.Second
	if reached, _, err := devnet.WaitAbove(running, from.Height+2, window); err == nil {
		out.observed = "progress"
		out.to = reached
		out.setHeight = reached
	} else {
		fmt.Printf("phase=%s probe=no-agreement-above from=%d target=%d window=%s\n", phase, from.Height, from.Height+3, window)
		out.observed = "stall"
		// observeStall bounds every sample by the threshold T, plus the one
		// block at T+1 in flight at the stop, whose commit it requires to
		// carry the stopped validator's signature (stoppedSigned). The bound
		// on from and the commit check below stay and agree with it. from is
		// read after the stop. In case Y the stopped v1 can precommit T+1
		// only after a polka for it, 23 of 33 power. v5 has no process, so
		// without v2 or without v4 at most 22 can prevote: both prevoted at
		// height T+1, so both had committed T, and from >= T. Every commit
		// in case Y needs v1 (22 of 33 without it), so a running node that
		// catches up to a height at or below T passes the commit check too.
		out.to = observeStall(t, phase, at, stoppedSigned(t, keys, at))
		agreed, err := devnet.Agreed(running)
		if err != nil || agreed.Height != out.to {
			t.Fatalf("case %s stall: running nodes not agreed at %d: %+v %v", name, out.to, agreed, err)
		}
		if out.to > from.Height+1 {
			t.Fatalf("case %s committed %d blocks after height %d and then stopped", name, out.to-from.Height, from.Height)
		}
		// The set that must sign the height that does not commit.
		out.setHeight = out.to + 1
	}
	requireSet(t, fmt.Sprintf("case %s at %d", name, out.from), validatorSet(t, running[0], out.from), keys, want)
	set := validatorSet(t, running[0], out.setHeight)
	requireSet(t, fmt.Sprintf("case %s at %d", name, out.setHeight), set, keys, want)
	// Validators without a running process: the stopped one, and v5.
	var noProcess []string
	for _, row := range set {
		if who := nameOf(keys, row.pub); !slices.Contains(names, who) {
			noProcess = append(noProcess, who)
		}
	}
	var voters []string
	if out.observed == "progress" {
		seen := map[string]bool{}
		for height := out.from + 2; height <= out.to; height++ {
			for _, who := range requireCommit(t, running[0], height, keys, noProcess, nil) {
				seen[who] = true
			}
		}
		for who := range seen {
			voters = append(voters, who)
		}
		sort.Strings(voters)
	} else {
		// A block after from was already in flight at the stop: without the
		// stopped validator the online power is not enough to commit it.
		for height := out.from + 1; height <= out.to; height++ {
			requireCommit(t, running[0], height, keys, nil, []string{stopped})
		}
		voters = observeVoters(t, phase, running, keys, set, out.setHeight, names, stopped)
	}
	processes := slices.Sorted(slices.Values(names))
	if !slices.Equal(voters, processes) {
		t.Fatalf("case %s: validators observed voting %v, validators with a running process %v", name, voters, processes)
	}
	for _, row := range set {
		who := nameOf(keys, row.pub)
		out.validators++
		out.totalPower += row.power
		if slices.Contains(voters, who) {
			out.onlineValidators++
			out.onlinePower += row.power
		} else {
			out.offline = append(out.offline, who)
		}
	}
	sort.Strings(out.offline)
	out.countRule = ruleOutcome(int64(out.onlineValidators), int64(out.validators))
	out.powerRule = ruleOutcome(out.onlinePower, out.totalPower)
	fmt.Printf("phase=power-vs-count case=%s offline=%s online_power=%d total_power=%d online_validators=%d validators=%d count_rule=%s power_rule=%s observed=%s height_from=%d height_to=%d set_height=%d online_from=%s\n",
		name, strings.Join(out.offline, ","), out.onlinePower, out.totalPower, out.onlineValidators, out.validators, out.countRule, out.powerRule, out.observed, out.from, out.to, out.setHeight, onlineSource(out.observed))
	return out
}

// onlineSource names the observation the online set of a case comes from.
func onlineSource(observed string) string {
	if observed == "progress" {
		return "commit-signers"
	}
	return "stuck-height-votes"
}

// ruleOutcome is the more-than-two-thirds rule over online of total.
func ruleOutcome(online, total int64) string {
	if total > 0 && online*3 > total*2 {
		return "progress"
	}
	return "stall"
}

// recoverAll waits until every process agrees above the case's last height
// and then for two more blocks. Then every validator process, including the
// restarted one, must sign at least one commit after that height.
func recoverAll(t *testing.T, layout devnet.Layout, rpcs []string, keys map[string][]byte, name string, last int64) {
	t.Helper()
	up, _, err := devnet.WaitEqual(rpcs, last+1, 60*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	reached, _, err := devnet.WaitAbove(rpcs, up+1, 45*time.Second)
	if err != nil || reached < up+2 {
		t.Fatalf("case %s recovery up %d reached %d: %v", name, up, reached, err)
	}
	fmt.Printf("phase=power-vs-count-recovery case=%s nodes=%d up=%d reached=%d\n", name, len(rpcs), up, reached)
	all := make([]string, 0, len(layout.Topology.Validators))
	for _, node := range layout.Topology.Validators {
		all = append(all, node.Name)
	}
	last = waitSigners(t, "case "+name+" recovery", rpcs[0], keys, all, up, 45*time.Second)
	fmt.Printf("phase=power-vs-count-live case=%s heights=%d..%d signed=%s\n", name, up+1, last, strings.Join(all, ","))
}

// waitSigners reads the commits after height after on rpc until each
// validator in want has signed at least one of them, for at most timeout. It
// returns the last height read. With SkipTimeoutCommit a commit can lack a
// late precommit, so one commit is not enough.
func waitSigners(t *testing.T, label, rpc string, keys map[string][]byte, want []string, after int64, timeout time.Duration) int64 {
	t.Helper()
	seen := map[string]bool{}
	next := after + 1
	deadline := time.Now().Add(timeout)
	for {
		status, err := devnet.ReadStatus(rpc)
		if err != nil {
			t.Fatal(err)
		}
		for ; next <= status.Height; next++ {
			signers, _, _ := commitSigners(t, rpc, next, keys)
			for _, who := range signers {
				seen[who] = true
			}
		}
		var missing []string
		for _, who := range want {
			if !seen[who] {
				missing = append(missing, who)
			}
		}
		if len(missing) == 0 {
			return next - 1
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v signed no commit in %d..%d", label, missing, after+1, next-1)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// requireSet fails unless set has exactly the named keys with these powers.
func requireSet(t *testing.T, label string, set []voted, keys map[string][]byte, want map[string]int64) {
	t.Helper()
	if len(set) != len(want) {
		t.Fatalf("%s: %d validators, want %d (%s)", label, len(set), len(want), describeSet(set, keys))
	}
	for who, power := range want {
		if !hasPower(set, keys[who], power) {
			t.Fatalf("%s: %s does not have power %d (%s)", label, who, power, describeSet(set, keys))
		}
	}
}

// describeSet prints a set as name:power, sorted by name.
func describeSet(set []voted, keys map[string][]byte) string {
	parts := make([]string, 0, len(set))
	for _, row := range set {
		parts = append(parts, fmt.Sprintf("%s:%d", nameOf(keys, row.pub), row.power))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func nameOf(keys map[string][]byte, pub []byte) string {
	for who, key := range keys {
		if bytes.Equal(key, pub) {
			return who
		}
	}
	return "unknown-" + hex.EncodeToString(pub)
}

// commitSigners reads the commit for height and that height's validator set.
// It returns the validators whose signature in the commit is for the
// committed block, sorted, with their power and the set's total power.
func commitSigners(t *testing.T, rpc string, height int64, keys map[string][]byte) ([]string, int64, int64) {
	t.Helper()
	set := validatorSet(t, rpc, height)
	sigs := readCommit(t, rpc, height)
	if len(sigs) != len(set) {
		t.Fatalf("commit %d has %d signatures for %d validators", height, len(sigs), len(set))
	}
	var signed, total int64
	var names []string
	for _, row := range set {
		total += row.power
		address := hex.EncodeToString(cmted25519.PubKey(row.pub).Address())
		for _, sig := range sigs {
			if sig.Flag == blockIDFlagCommit && strings.EqualFold(sig.Address, address) {
				signed += row.power
				names = append(names, nameOf(keys, row.pub))
			}
		}
	}
	sort.Strings(names)
	return names, signed, total
}

// requireCommit checks the commit for height: no validator in absent signed
// it, every validator in present did, and the signers hold more than two
// thirds of the power of that height's validator set. It returns the signers.
func requireCommit(t *testing.T, rpc string, height int64, keys map[string][]byte, absent, present []string) []string {
	t.Helper()
	names, signed, total := commitSigners(t, rpc, height, keys)
	for _, who := range names {
		if slices.Contains(absent, who) {
			t.Fatalf("commit %d is signed by %s, which has no running process", height, who)
		}
	}
	for _, want := range present {
		if !slices.Contains(names, want) {
			t.Fatalf("commit %d signers %v do not include %s", height, names, want)
		}
	}
	if signed*3 <= total*2 {
		t.Fatalf("commit %d signed power %d of %d", height, signed, total)
	}
	fmt.Printf("phase=power-commit height=%d signers=%s signed_power=%d total_power=%d\n", height, strings.Join(names, ","), signed, total)
	return names
}

// observeVoters reads /dump_consensus_state on every running node until each
// one is at height and holds a prevote or precommit at that height from every
// validator in expect, for at most 20 seconds. Votes are matched to
// validators by their index in the node's own validator set, which must equal
// the RPC set. It returns the validators every node saw vote, without
// stopped: a vote of stopped, if a node holds one, was cast before its
// process exited.
func observeVoters(t *testing.T, phase string, rpcs []string, keys map[string][]byte, set []voted, height int64, expect []string, stopped string) []string {
	t.Helper()
	const window = 20 * time.Second
	deadline := time.Now().Add(window)
	for {
		var lines []string
		var common map[string]bool
		ready := true
		for i, rpc := range rpcs {
			state := dumpRoundState(t, rpc)
			atHeight, err := strconv.ParseInt(state.Height, 10, 64)
			if err != nil {
				t.Fatalf("node %d round state height %q: %v", i, state.Height, err)
			}
			if atHeight != height {
				ready = false
				lines = append(lines, fmt.Sprintf("node=%d height=%d", i, atHeight))
				continue
			}
			nodeSet := make([]voted, 0, len(state.Validators.Validators))
			for _, row := range state.Validators.Validators {
				power, err := strconv.ParseInt(row.VotingPower, 10, 64)
				if err != nil {
					t.Fatalf("node %d voting power %q: %v", i, row.VotingPower, err)
				}
				nodeSet = append(nodeSet, voted{pub: row.PubKey.Value, power: power})
			}
			if describeSet(nodeSet, keys) != describeSet(set, keys) {
				t.Fatalf("node %d consensus set at %d is %s, RPC set is %s", i, height, describeSet(nodeSet, keys), describeSet(set, keys))
			}
			votedNow := map[string]bool{}
			for _, round := range state.Votes {
				if len(round.Prevotes) != len(nodeSet) || len(round.Precommits) != len(nodeSet) {
					t.Fatalf("node %d round %d has %d prevotes and %d precommits for %d validators", i, round.Round, len(round.Prevotes), len(round.Precommits), len(nodeSet))
				}
				for index, row := range nodeSet {
					if round.Prevotes[index] != nilVote || round.Precommits[index] != nilVote {
						votedNow[nameOf(keys, row.pub)] = true
					}
				}
			}
			var voters []string
			var power, total int64
			for _, row := range nodeSet {
				who := nameOf(keys, row.pub)
				total += row.power
				if votedNow[who] && who != stopped {
					voters = append(voters, who)
					power += row.power
				}
			}
			sort.Strings(voters)
			lines = append(lines, fmt.Sprintf("node=%d height=%d round=%d voters=%s stopped=%s stopped_vote=%t voter_power=%d total_power=%d",
				i, atHeight, state.Round, strings.Join(voters, ","), stopped, votedNow[stopped], power, total))
			for _, who := range expect {
				if !votedNow[who] {
					ready = false
				}
			}
			if common == nil {
				common = map[string]bool{}
				for _, who := range voters {
					common[who] = true
				}
			} else {
				for who := range common {
					if !slices.Contains(voters, who) {
						delete(common, who)
					}
				}
			}
		}
		if ready {
			for _, line := range lines {
				fmt.Printf("phase=%s-voters %s\n", phase, line)
			}
			out := make([]string, 0, len(common))
			for who := range common {
				out = append(out, who)
			}
			sort.Strings(out)
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: within %s the running nodes do not all hold votes at %d from %v: %s", phase, window, height, expect, strings.Join(lines, "; "))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// nilVote is the entry CometBFT's vote set JSON gives a validator whose vote
// it does not hold (types/vote.go nilVoteStr).
const nilVote = "nil-Vote"

// roundState is the part of CometBFT's /dump_consensus_state round state
// that observeVoters reads. Votes are per round, indexed like the validators.
type roundState struct {
	Height     string `json:"height"`
	Round      int32  `json:"round"`
	Validators struct {
		Validators []struct {
			PubKey struct {
				Value []byte `json:"value"`
			} `json:"pub_key"`
			VotingPower string `json:"voting_power"`
		} `json:"validators"`
	} `json:"validators"`
	Votes []struct {
		Round      int32    `json:"round"`
		Prevotes   []string `json:"prevotes"`
		Precommits []string `json:"precommits"`
	} `json:"votes"`
}

// dumpRoundState reads one node's consensus round state.
func dumpRoundState(t *testing.T, rpc string) roundState {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(rpc, "/") + "/dump_consensus_state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed struct {
		Result struct {
			RoundState roundState `json:"round_state"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Result.RoundState
}

// blockIDFlagCommit is CometBFT BlockIDFlagCommit: the validator precommitted
// the committed block.
const blockIDFlagCommit = 2

// commitSig is one signature of /commit. Timestamp stays text; only
// precommitTime parses it.
type commitSig struct {
	Flag      int    `json:"block_id_flag"`
	Address   string `json:"validator_address"`
	Timestamp string `json:"timestamp"`
}

// readCommit returns the commit signatures for one height from /commit.
func readCommit(t *testing.T, rpc string, height int64) []commitSig {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/commit?height=%d", strings.TrimRight(rpc, "/"), height))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed struct {
		Result struct {
			SignedHeader struct {
				Commit struct {
					Height     string      `json:"height"`
					Signatures []commitSig `json:"signatures"`
				} `json:"commit"`
			} `json:"signed_header"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	got, err := strconv.ParseInt(parsed.Result.SignedHeader.Commit.Height, 10, 64)
	if err != nil || got != height {
		t.Fatalf("commit for %d reports height %q: %v", height, parsed.Result.SignedHeader.Commit.Height, err)
	}
	return parsed.Result.SignedHeader.Commit.Signatures
}
