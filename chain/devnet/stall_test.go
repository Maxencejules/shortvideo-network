package devnet_test

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
)

// TestStallLimit drives stallLimit, the per-sample decision of observeStall,
// without processes. It shows nothing about CometBFT; the devnet evidence is
// the stall phases of the process tests.
func TestStallLimit(t *testing.T) {
	const threshold = 12
	errUnsigned := errors.New("commit 13 signers [v1 v2] do not include v3")
	cases := []struct {
		name      string
		limit     int64
		height    int64
		verifier  error // returned by the in-flight check
		wantLimit int64
		wantErr   bool
		wantCalls []int64
	}{
		{name: "catch-up below threshold", limit: threshold, height: threshold - 1, wantLimit: threshold},
		{name: "far behind the threshold", limit: threshold, height: 1, wantLimit: threshold},
		{name: "equal to threshold", limit: threshold, height: threshold, wantLimit: threshold},
		{name: "threshold+1 with a passing verifier moves the limit", limit: threshold, height: threshold + 1, wantLimit: threshold + 1, wantCalls: []int64{threshold + 1}},
		{name: "threshold+1 with a failing verifier", limit: threshold, height: threshold + 1, verifier: errUnsigned, wantLimit: threshold, wantErr: true, wantCalls: []int64{threshold + 1}},
		{name: "threshold+2", limit: threshold, height: threshold + 2, wantLimit: threshold, wantErr: true},
		{name: "threshold+1 again after the limit moved is the same block", limit: threshold + 1, height: threshold + 1, verifier: errUnsigned, wantLimit: threshold + 1},
		{name: "below threshold after the limit moved", limit: threshold + 1, height: threshold - 1, wantLimit: threshold + 1},
		{name: "second in-flight attempt after the limit moved", limit: threshold + 1, height: threshold + 2, wantLimit: threshold + 1, wantErr: true},
		{name: "height far above the limit when the limit is threshold+1", limit: threshold + 1, height: threshold + 5, wantLimit: threshold + 1, wantErr: true},
		{name: "limit below threshold is refused", limit: threshold - 1, height: threshold - 1, wantLimit: threshold - 1, wantErr: true},
		{name: "limit above threshold+1 is refused", limit: threshold + 2, height: threshold + 2, wantLimit: threshold + 2, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []int64
			verify := func(height int64) error {
				calls = append(calls, height)
				return tc.verifier
			}
			got, err := stallLimit("two-down", 1, threshold, tc.limit, tc.height, verify)
			if got != tc.wantLimit {
				t.Errorf("limit %d, want %d", got, tc.wantLimit)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("error %v, want error %t", err, tc.wantErr)
			}
			if fmt.Sprint(calls) != fmt.Sprint(tc.wantCalls) {
				t.Errorf("verifier calls %v, want %v", calls, tc.wantCalls)
			}
			if err == nil {
				return
			}
			for _, part := range []string{
				"phase two-down",
				"node 1",
				fmt.Sprintf("height %d", tc.height),
				fmt.Sprintf("threshold %d", threshold),
				fmt.Sprintf("limit %d", tc.limit),
			} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("error %q does not name %q", err, part)
				}
			}
			if tc.verifier != nil && len(tc.wantCalls) > 0 && !errors.Is(err, tc.verifier) {
				t.Errorf("error %q does not wrap the verifier error", err)
			}
		})
	}
}

// TestStallLimitWithoutVerifier refuses the in-flight rule when the phase
// gives no check.
func TestStallLimitWithoutVerifier(t *testing.T) {
	got, err := stallLimit("two-down", 0, 12, 12, 13, nil)
	if err == nil || got != 12 {
		t.Fatalf("limit %d error %v; want limit 12 and an error", got, err)
	}
}

// TestStallLimitSequences replays the node heights of stall phases through
// stallLimit with one limit shared by the nodes, as observeStall does. The
// first four are the traces of the failures the old first-sample ceiling
// reported; each is a correct stall.
func TestStallLimitSequences(t *testing.T) {
	cases := []struct {
		name      string
		threshold int64
		samples   [][]int64 // per sample, per node
		signed    bool      // the in-flight commit carries the stopped validator
		wantLimit int64
		wantErr   bool
		wantCalls int
	}{
		{name: "CI 41362122347: node 0 catches up 11 to 12", threshold: 12, samples: [][]int64{{11, 12}, {12, 12}, {12, 12}}, signed: true, wantLimit: 12},
		{name: "local: node 0 catches up 15 to 16", threshold: 16, samples: [][]int64{{15, 16}, {16, 16}, {16, 16}}, signed: true, wantLimit: 16},
		{name: "local: node 1 catches up 12 to 13", threshold: 13, samples: [][]int64{{13, 12}, {13, 13}, {13, 13}}, signed: true, wantLimit: 13},
		{name: "local: in-flight block 14 after the stop at 13", threshold: 13, samples: [][]int64{{13, 13}, {14, 13}, {14, 14}, {14, 14}}, signed: true, wantLimit: 14, wantCalls: 1},
		{name: "in-flight block without the stopped validator", threshold: 13, samples: [][]int64{{13, 13}, {14, 13}}, signed: false, wantLimit: 13, wantErr: true, wantCalls: 1},
		{name: "a second block after the in-flight one", threshold: 13, samples: [][]int64{{14, 14}, {15, 14}}, signed: true, wantLimit: 14, wantErr: true, wantCalls: 1},
		{name: "chain still committing", threshold: 13, samples: [][]int64{{13, 13}, {15, 15}}, signed: true, wantLimit: 13, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			verify := func(height int64) error {
				calls++
				if height != tc.threshold+1 {
					return fmt.Errorf("checked height %d", height)
				}
				if !tc.signed {
					return errors.New("stopped validator did not sign")
				}
				return nil
			}
			limit := tc.threshold
			var err error
		replay:
			for _, sample := range tc.samples {
				for node, height := range sample {
					limit, err = stallLimit("replay", node, tc.threshold, limit, height, verify)
					if err != nil {
						break replay
					}
				}
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("error %v, want error %t", err, tc.wantErr)
			}
			if limit != tc.wantLimit || calls != tc.wantCalls {
				t.Fatalf("limit %d after %d checks, want %d after %d", limit, calls, tc.wantLimit, tc.wantCalls)
			}
		})
	}
}

// The tests below drive stopForStall, watchStall, observeStall and
// stoppedSigned against fakeChain, an httptest stand-in for the CometBFT RPC
// endpoints they read (/status, /validators, /commit). They show the stall
// code decides correctly on given RPC answers. They show nothing about
// CometBFT; the devnet evidence is the stall phases of the process tests.

// fakeValidator is one member of a fakeChain validator set.
type fakeValidator struct {
	name  string
	power int64
}

// fakeVote is one signature of a fake commit: a CometBFT BlockIDFlag (1
// absent, 2 commit, 3 nil) and the vote time.
type fakeVote struct {
	flag int
	at   time.Time
}

const (
	flagAbsent = 1
	flagCommit = 2
	flagNil    = 3
)

// fakeChain is what every fake node serves: one validator set at every
// height and the commits by height. Keys come from fixed seeds.
type fakeChain struct {
	set     []fakeValidator
	keys    map[string][]byte
	commits map[int64]map[string]fakeVote
}

func newFakeChain(set ...fakeValidator) *fakeChain {
	c := &fakeChain{set: set, keys: map[string][]byte{}, commits: map[int64]map[string]fakeVote{}}
	for _, v := range set {
		c.keys[v.name] = cmted25519.GenPrivKeyFromSecret([]byte("svn-stall-test-" + v.name)).PubKey().Bytes()
	}
	return c
}

// equalPower is the two-down set: v1..v4, power 1 each.
func equalPower() *fakeChain {
	return newFakeChain(fakeValidator{"v1", 1}, fakeValidator{"v2", 1}, fakeValidator{"v3", 1}, fakeValidator{"v4", 1})
}

// caseY is the power case Y set: v1:10 v2:10 v3:2 v4:10 v5:1, total 33.
func caseY() *fakeChain {
	return newFakeChain(fakeValidator{"v1", 10}, fakeValidator{"v2", 10}, fakeValidator{"v3", 2}, fakeValidator{"v4", 10}, fakeValidator{"v5", 1})
}

// commit records the commit for height: every named validator signed the
// block at, the others are absent.
func (c *fakeChain) commit(height int64, at time.Time, signers ...string) {
	votes := map[string]fakeVote{}
	for _, who := range signers {
		votes[who] = fakeVote{flag: flagCommit, at: at}
	}
	c.commits[height] = votes
}

// fakeNode is one fake RPC endpoint. /status answers heights in turn, the
// last one repeating, with appHash. It counts the reads of each path.
type fakeNode struct {
	url     string
	close   func()
	mu      sync.Mutex
	reads   map[string]int
	heights []int64
	appHash string
}

func (c *fakeChain) node(t *testing.T, appHash string, heights ...int64) *fakeNode {
	t.Helper()
	n := &fakeNode{reads: map[string]int{}, heights: heights, appHash: appHash}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		count := n.reads[r.URL.Path]
		n.reads[r.URL.Path]++
		n.mu.Unlock()
		switch r.URL.Path {
		case "/status":
			height := n.heights[min(count, len(n.heights)-1)]
			fmt.Fprintf(w, `{"result":{"sync_info":{"latest_block_height":"%d","latest_app_hash":"%s"}}}`, height, n.appHash)
		case "/validators":
			rows := make([]string, 0, len(c.set))
			for _, v := range c.set {
				rows = append(rows, fmt.Sprintf(`{"pub_key":{"type":"tendermint/PubKeyEd25519","value":"%s"},"voting_power":"%d"}`, base64.StdEncoding.EncodeToString(c.keys[v.name]), v.power))
			}
			fmt.Fprintf(w, `{"result":{"block_height":"%s","validators":[%s],"count":"%d","total":"%d"}}`, r.URL.Query().Get("height"), strings.Join(rows, ","), len(rows), len(rows))
		case "/commit":
			height, err := strconv.ParseInt(r.URL.Query().Get("height"), 10, 64)
			votes, ok := c.commits[height]
			if err != nil || !ok {
				http.Error(w, "no commit", http.StatusNotFound)
				return
			}
			sigs := make([]string, 0, len(c.set))
			for _, v := range c.set {
				vote, ok := votes[v.name]
				if !ok {
					vote = fakeVote{flag: flagAbsent}
				}
				address := strings.ToUpper(hex.EncodeToString(cmted25519.PubKey(c.keys[v.name]).Address()))
				sigs = append(sigs, fmt.Sprintf(`{"block_id_flag":%d,"validator_address":"%s","timestamp":"%s","signature":null}`, vote.flag, address, vote.at.UTC().Format(time.RFC3339Nano)))
			}
			fmt.Fprintf(w, `{"result":{"signed_header":{"commit":{"height":"%d","round":0,"signatures":[%s]}},"canonical":false}}`, height, strings.Join(sigs, ","))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	n.url = srv.URL
	n.close = srv.Close
	return n
}

// count is the number of reads of path so far.
func (n *fakeNode) count(path string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.reads[path]
}

// fakeLayout is a layout whose validators are the given fake nodes, named
// v1.. in order.
func fakeLayout(nodes []*fakeNode) devnet.Layout {
	var layout devnet.Layout
	for i, n := range nodes {
		layout.Topology.Validators = append(layout.Topology.Validators, devnet.Node{Name: fmt.Sprintf("v%d", i+1), RPC: n.url})
	}
	return layout
}

func statusReads(nodes []*fakeNode) []int {
	out := make([]int, len(nodes))
	for i, n := range nodes {
		out[i] = n.count("/status")
	}
	return out
}

// TestStopForStall checks the start of a stall phase: the threshold is read
// over every running process, the one stopped included, before the stop and
// never after it; the stopped validator, the running nodes and the stop time
// come from the same call.
func TestStopForStall(t *testing.T) {
	chain := equalPower()
	// v3, the one to stop, alone holds the high-water mark. v4 has no
	// process, so its endpoint must not be read.
	nodes := []*fakeNode{chain.node(t, "AA", 12), chain.node(t, "AA", 12), chain.node(t, "AA", 13), chain.node(t, "AA", 99)}
	var atStop []int
	var calledAt time.Time
	stops := []func() error{
		func() error { t.Error("v1 was stopped"); return nil },
		func() error { t.Error("v2 was stopped"); return nil },
		func() error {
			atStop = statusReads(nodes)
			calledAt = time.Now()
			return nil
		},
		nil,
	}
	got, err := stopForStall("stop-two", fakeLayout(nodes), stops, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.threshold != (devnet.Status{Height: 13, AppHash: "AA"}) {
		t.Errorf("threshold %+v, want height 13 app hash AA", got.threshold)
	}
	if got.stopped != "v3" {
		t.Errorf("stopped %q, want v3", got.stopped)
	}
	if !slices.Equal(got.running, []string{nodes[0].url, nodes[1].url}) || !slices.Equal(got.names, []string{"v1", "v2"}) {
		t.Errorf("running %v names %v, want v1 and v2", got.running, got.names)
	}
	if want := []int{1, 1, 1, 0}; !slices.Equal(atStop, want) {
		t.Errorf("status reads when the stop was called %v, want %v", atStop, want)
	}
	if want := []int{1, 1, 1, 0}; !slices.Equal(statusReads(nodes), want) {
		t.Errorf("status reads after stopForStall %v, want %v: no read after the stop", statusReads(nodes), want)
	}
	if stops[2] != nil || stops[0] == nil || stops[1] == nil || stops[3] != nil {
		t.Errorf("stops after stopForStall: only index 2 must be cleared")
	}
	if got.stoppedAt.Before(calledAt) {
		t.Errorf("stoppedAt %s is before the stop ran at %s", got.stoppedAt, calledAt)
	}
}

// TestStopForStallRefuses covers the calls that must not stop anything.
func TestStopForStallRefuses(t *testing.T) {
	chain := equalPower()
	errStop := errors.New("validator did not stop")
	cases := []struct {
		name      string
		hashes    []string
		index     int
		stopErr   error
		nilStops  []int
		short     bool
		wantErr   string
		wantStop  bool // the stop function ran
		wantReads bool // a status endpoint was read
		keepEntry bool // stops[index] is still set afterwards
	}{
		{name: "index without a process", hashes: []string{"AA", "AA", "AA", "AA"}, index: 3, nilStops: []int{3}, wantErr: "no running process to stop"},
		{name: "index below range", hashes: []string{"AA", "AA", "AA", "AA"}, index: -1, wantErr: "no running process to stop"},
		{name: "index above range", hashes: []string{"AA", "AA", "AA", "AA"}, index: 4, wantErr: "no running process to stop"},
		{name: "stops shorter than the validators", hashes: []string{"AA", "AA", "AA", "AA"}, index: 0, short: true, wantErr: "3 stop functions for 4 validators"},
		{name: "app hash disagreement before the stop", hashes: []string{"AA", "BB", "AA", "AA"}, index: 2, wantErr: "app hash disagreement", wantReads: true, keepEntry: true},
		{name: "stop fails", hashes: []string{"AA", "AA", "AA", "AA"}, index: 2, stopErr: errStop, wantErr: "stop v3: validator did not stop", wantStop: true, wantReads: true, keepEntry: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes := make([]*fakeNode, 4)
			for i := range nodes {
				nodes[i] = chain.node(t, tc.hashes[i], 12)
			}
			stopped := false
			stops := make([]func() error, 4)
			for i := range stops {
				stops[i] = func() error { stopped = true; return tc.stopErr }
			}
			for _, i := range tc.nilStops {
				stops[i] = nil
			}
			if tc.short {
				stops = stops[:3]
			}
			_, err := stopForStall("stop-two", fakeLayout(nodes), stops, tc.index)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %v, want one containing %q", err, tc.wantErr)
			}
			if stopped != tc.wantStop {
				t.Errorf("stop ran %t, want %t", stopped, tc.wantStop)
			}
			reads := 0
			for _, n := range statusReads(nodes) {
				reads += n
			}
			if (reads > 0) != tc.wantReads {
				t.Errorf("status reads %v, want reads %t", statusReads(nodes), tc.wantReads)
			}
			if tc.keepEntry && stops[tc.index] == nil {
				t.Errorf("stops[%d] was cleared although the stop did not succeed", tc.index)
			}
		})
	}
}

// TestStoppedSigned checks the in-flight verifier on fake commits: the
// stopped validator's commit signature, no signer without a process, more
// than two thirds of the power, and the stopped validator's vote time.
func TestStoppedSigned(t *testing.T) {
	stoppedAt := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	before := stoppedAt.Add(-200 * time.Millisecond)
	twoDown := stallStart{stopped: "v3", names: []string{"v1", "v2"}, stoppedAt: stoppedAt}
	powerY := stallStart{stopped: "v1", names: []string{"v2", "v3", "v4"}, stoppedAt: stoppedAt}
	type vote struct {
		who  string
		flag int
		at   time.Time
	}
	cases := []struct {
		name      string
		chain     func() *fakeChain
		start     stallStart
		votes     []vote
		dropKey   string // removed from the keys given to stoppedSigned
		wantNames []string
		wantErr   string
	}{
		{name: "v1 v2 v3 before the stop", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v3", flagCommit, before}}, wantNames: []string{"v1", "v2", "v3"}},
		{name: "v3 voted exactly at the stop time", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v3", flagCommit, stoppedAt}}, wantNames: []string{"v1", "v2", "v3"}},
		{name: "v3 voted after the stop", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v3", flagCommit, stoppedAt.Add(time.Millisecond)}}, wantErr: "v3 precommitted at 2026-11-02T12:00:00.001Z, after its process had stopped"},
		{name: "v3 absent and v4 signed", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v4", flagCommit, before}}, wantErr: "signers [v1 v2 v4] do not include v3, the validator just stopped"},
		{name: "v3 voted nil", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v3", flagNil, before}}, wantErr: "signers [v1 v2] do not include v3"},
		{name: "v3 absent", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}}, wantErr: "signers [v1 v2] do not include v3"},
		{name: "v4 with no process signed too", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v3", flagCommit, before}, {"v4", flagCommit, before}}, wantErr: "signed by v4, which has no running process"},
		{name: "a signer missing from the keys", chain: equalPower, start: twoDown, dropKey: "v4",
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v3", flagCommit, before}, {"v4", flagCommit, before}}, wantErr: "signed by unknown-"},
		{name: "two of four power", chain: equalPower, start: twoDown,
			votes: []vote{{"v1", flagCommit, before}, {"v3", flagCommit, before}}, wantErr: "signed power 2 of 4"},
		{name: "case Y: v1 v2 v4 hold 30 of 33", chain: caseY, start: powerY,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v4", flagCommit, before}}, wantNames: []string{"v1", "v2", "v4"}},
		{name: "case Y: v1 v2 v3 hold 22 of 33, two thirds exactly", chain: caseY, start: powerY,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v3", flagCommit, before}}, wantErr: "signed power 22 of 33"},
		{name: "case Y: v5 with no process signed", chain: caseY, start: powerY,
			votes: []vote{{"v1", flagCommit, before}, {"v2", flagCommit, before}, {"v4", flagCommit, before}, {"v5", flagCommit, before}}, wantErr: "signed by v5, which has no running process"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chain := tc.chain()
			votes := map[string]fakeVote{}
			for _, v := range tc.votes {
				votes[v.who] = fakeVote{flag: v.flag, at: v.at}
			}
			chain.commits[13] = votes
			keys := map[string][]byte{}
			for who, key := range chain.keys {
				if who != tc.dropKey {
					keys[who] = key
				}
			}
			node := chain.node(t, "AA", 13)
			names, err := stoppedSigned(t, keys, tc.start)(node.url, 13)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("error %v, want none", err)
				}
				if !slices.Equal(names, tc.wantNames) {
					t.Fatalf("signers %v, want %v", names, tc.wantNames)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("signers %v error %v, want an error containing %q", names, err, tc.wantErr)
			}
		})
	}
}

// TestWatchStall drives the stall loop, stallLimit and stoppedSigned together
// against fake nodes. The two-down shape: v1..v4 of power 1, v4 without a
// process, v3 just stopped, v1 and v2 running, threshold 12.
func TestWatchStall(t *testing.T) {
	stoppedAt := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	before := stoppedAt.Add(-200 * time.Millisecond)
	const samples = 6
	cases := []struct {
		name    string
		heights [2][]int64 // per running node, per sample
		hashes  [2]string
		commits map[int64][]string
		voteAt  time.Time // vote time of every signature
		noCheck bool
		want    int64
		wantErr []string
		// commit reads expected on each node: an in-flight block is
		// verified on the node that reported it, once.
		wantCommitReads [2]bool
	}{
		{name: "every node at the threshold", heights: [2][]int64{{12}, {12}}, want: 12},
		{name: "CI 41362122347: node 0 catches up 11 to 12", heights: [2][]int64{{11, 12}, {12}}, want: 12},
		{name: "node 1 far behind catches up", heights: [2][]int64{{12}, {3, 7, 12}}, want: 12},
		{name: "in-flight block with v3's precommit", heights: [2][]int64{{12, 13}, {12, 12, 13}},
			commits: map[int64][]string{13: {"v1", "v2", "v3"}}, want: 13, wantCommitReads: [2]bool{true, false}},
		{name: "in-flight block first seen on node 1", heights: [2][]int64{{12, 12, 13}, {13}},
			commits: map[int64][]string{13: {"v1", "v2", "v3"}}, want: 13, wantCommitReads: [2]bool{false, true}},
		{name: "in-flight block without v3", heights: [2][]int64{{12, 13}, {12}},
			commits: map[int64][]string{13: {"v1", "v2", "v4"}},
			wantErr: []string{"phase fake node 0 height 13 is above threshold 12 (limit 12) and is not the block in flight at the stop", "do not include v3, the validator just stopped"}},
		{name: "in-flight block signed by v4, which has no process", heights: [2][]int64{{12}, {13}},
			commits: map[int64][]string{13: {"v1", "v2", "v3", "v4"}},
			wantErr: []string{"node 1 height 13", "signed by v4, which has no running process"}},
		{name: "in-flight block with v3's precommit after the stop", heights: [2][]int64{{13}, {12}},
			commits: map[int64][]string{13: {"v1", "v2", "v3"}}, voteAt: stoppedAt.Add(time.Second),
			wantErr: []string{"node 0 height 13", "after its process had stopped"}},
		{name: "a second block after the in-flight one", heights: [2][]int64{{13, 14}, {13}},
			commits: map[int64][]string{13: {"v1", "v2", "v3"}, 14: {"v1", "v2", "v3"}},
			wantErr: []string{"node 0 height 14 is above limit 13 (threshold 12)"}},
		{name: "two blocks at once", heights: [2][]int64{{12}, {14}},
			commits: map[int64][]string{14: {"v1", "v2", "v3"}},
			wantErr: []string{"node 1 height 14 is above limit 12 (threshold 12)"}},
		{name: "in-flight block without a check", heights: [2][]int64{{13}, {12}}, noCheck: true,
			wantErr: []string{"node 0 height 13 is above threshold 12 (limit 12) and the phase has no in-flight check"}},
		{name: "app hash disagreement", heights: [2][]int64{{12}, {12}}, hashes: [2]string{"AA", "BB"},
			wantErr: []string{"phase fake app hash disagree"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chain := equalPower()
			at := tc.voteAt
			if at.IsZero() {
				at = before
			}
			for height, signers := range tc.commits {
				chain.commit(height, at, signers...)
			}
			hashes := tc.hashes
			if hashes[0] == "" {
				hashes = [2]string{"AA", "AA"}
			}
			nodes := []*fakeNode{chain.node(t, hashes[0], tc.heights[0]...), chain.node(t, hashes[1], tc.heights[1]...)}
			start := stallStart{
				threshold: devnet.Status{Height: 12, AppHash: "AA"},
				stopped:   "v3",
				running:   []string{nodes[0].url, nodes[1].url},
				names:     []string{"v1", "v2"},
				stoppedAt: stoppedAt,
			}
			check := stoppedSigned(t, chain.keys, start)
			if tc.noCheck {
				check = nil
			}
			got, err := watchStall("fake", start, check, samples, 0)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("watchStall returned %d and no error; want %q", got, tc.wantErr)
				}
				for _, part := range tc.wantErr {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("error %q does not contain %q", err, part)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("watchStall returned %d, want %d", got, tc.want)
			}
			for i, n := range nodes {
				if reads := n.count("/status"); reads != samples {
					t.Errorf("node %d status read %d times, want %d", i, reads, samples)
				}
				if read := n.count("/commit") > 0; read != tc.wantCommitReads[i] {
					t.Errorf("node %d commit read %t, want %t", i, read, tc.wantCommitReads[i])
				}
			}
		})
	}
}

// TestWatchStallStatusError reports a node that cannot be read.
func TestWatchStallStatusError(t *testing.T) {
	chain := equalPower()
	nodes := []*fakeNode{chain.node(t, "AA", 12), chain.node(t, "AA", 12)}
	nodes[1].close()
	start := stallStart{threshold: devnet.Status{Height: 12, AppHash: "AA"}, stopped: "v3", running: []string{nodes[0].url, nodes[1].url}, names: []string{"v1", "v2"}}
	if got, err := watchStall("fake", start, stoppedSigned(t, chain.keys, start), 6, 0); err == nil || !strings.Contains(err.Error(), "phase fake sample 0") {
		t.Fatalf("watchStall returned %d, error %v; want a sample 0 error", got, err)
	}
}

// TestStopForStallThenObserveStall runs a stall phase from the stop to the
// last sample on fake nodes, with observeStall's own sampling (six samples a
// second apart): the threshold, the running nodes and the stopped validator
// reach the in-flight check from stopForStall alone.
func TestStopForStallThenObserveStall(t *testing.T) {
	chain := equalPower()
	stopTime := make(chan time.Time, 1)
	// Each node's first status read is stopForStall's. v3 is at 12, so the
	// threshold is 12; v1 then commits 13, in flight at the stop.
	nodes := []*fakeNode{chain.node(t, "AA", 11, 12, 13), chain.node(t, "AA", 12), chain.node(t, "AA", 12), chain.node(t, "AA", 12)}
	stops := []func() error{
		func() error { return nil },
		func() error { return nil },
		func() error { stopTime <- time.Now(); return nil },
		nil,
	}
	began := time.Now()
	start, err := stopForStall("fake-stop", fakeLayout(nodes), stops, 2)
	if err != nil {
		t.Fatal(err)
	}
	// v3's precommit for 13 was cast before its stop returned.
	var stopped time.Time
	select {
	case stopped = <-stopTime:
	default:
		t.Fatal("stopForStall returned without calling v3's stop")
	}
	chain.commit(13, stopped.Add(-time.Millisecond), "v1", "v2", "v3")
	got := observeStall(t, "fake", start, stoppedSigned(t, chain.keys, start))
	if got != 13 {
		t.Fatalf("observeStall returned %d, want 13", got)
	}
	if reads := statusReads(nodes); !slices.Equal(reads, []int{1 + stallSamples, 1 + stallSamples, 1, 0}) {
		t.Errorf("status reads %v, want stopForStall's read plus %d samples on v1 and v2, one read of v3, none of v4", reads, stallSamples)
	}
	if stallSamples != 6 || stallInterval != time.Second {
		t.Errorf("observeStall samples %d times every %s, want 6 times every second", stallSamples, stallInterval)
	}
	if elapsed := time.Since(began); elapsed < 5*time.Second {
		t.Errorf("six samples a second apart took %s", elapsed)
	}
	if nodes[1].count("/commit") != 0 || nodes[0].count("/commit") == 0 {
		t.Errorf("commit reads v1 %d v2 %d: the in-flight block must be checked on v1, which reported it", nodes[0].count("/commit"), nodes[1].count("/commit"))
	}
}

// TestObserveStallFailsTheTest checks that observeStall fails the test when
// the in-flight check fails. The failing call runs in a child process of this
// test binary, so the failure is the child's exit status, not this test's.
func TestObserveStallFailsTheTest(t *testing.T) {
	if os.Getenv("SVN_STALL_CHILD") == "1" {
		chain := equalPower()
		chain.commit(13, time.Now().Add(-time.Second), "v1", "v2", "v4")
		nodes := []*fakeNode{chain.node(t, "AA", 13), chain.node(t, "AA", 12)}
		start := stallStart{threshold: devnet.Status{Height: 12, AppHash: "AA"}, stopped: "v3", running: []string{nodes[0].url, nodes[1].url}, names: []string{"v1", "v2"}, stoppedAt: time.Now()}
		got := observeStall(t, "child", start, stoppedSigned(t, chain.keys, start))
		fmt.Printf("child: observeStall returned %d\n", got)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestObserveStallFailsTheTest$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), "SVN_STALL_CHILD=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("child error %v, want exit status 1; output:\n%s", err, out)
	}
	text := string(out)
	for _, part := range []string{"--- FAIL: TestObserveStallFailsTheTest", "phase child node 0 height 13 is above threshold 12", "signers [v1 v2 v4] do not include v3, the validator just stopped"} {
		if !strings.Contains(text, part) {
			t.Errorf("child output does not contain %q:\n%s", part, text)
		}
	}
	if strings.Contains(text, "child: observeStall returned") {
		t.Errorf("observeStall returned in the child:\n%s", text)
	}
}
