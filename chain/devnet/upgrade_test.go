package devnet_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/privval"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/protobuf/proto"

	"cosmossdk.io/log/v2"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal/scenario"
	"github.com/Maxencejules/shortvideo-network/chain/internal/statecheck"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/node"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The goal-10 upgrade demos (plan 12.5) run four validator processes of
// svnd binaries the test builds from this tree (go build), on one machine,
// with loopback peers, in one private network namespace in the test
// commands: a single-machine four-process devnet, not independent
// operators. The binaries are not independently built releases, not
// signed, and not reviewed source:
//
//   - B1: svnd, the release build (no build tag): app version 1, no upgrade
//     handler.
//   - B2: svnd built with -tags svn_rehearsal: app version 2 with the
//     rehearsal upgrade rehearsal-1 (rehearsal.Handler). It starts only on a
//     chain whose id starts with shortvideo-dev-.
//   - B2-bad: svnd built with -tags svn_rehearsal_bad: the same upgrade name
//     and app versions, run by rehearsal.BadHandler, which writes one
//     documented extra row (rehearsal.BadRowKey -> rehearsal.BadRowValue in
//     the x/leases store).
//
// Plan heights keep at least msgkit.MinActivationLeadBlocks (100 blocks, a
// PROPOSED constant, owner option text Q3 (a)) between the execute block
// and the plan height. Each test builds the binaries it runs once, before
// its first validator starts.
const (
	upgradeLabel     = "single-machine four-process devnet (not independent operators)"
	buildLabel       = "binaries built from one tree by go build; not independently built releases; not signed; not reviewed source"
	rehearsalRelease = "svnd -tags svn_rehearsal built from the tested tree (not a release)"
	// nodeStartLine is the line CometBFT logs when it starts the node
	// service. A binary the load gate or InitChain refuses never logs it.
	nodeStartLine = "Starting Node service"
)

// svndBuild is one build of chain/cmd/svnd: an output file name and its
// build tags (empty for the release build).
type svndBuild struct {
	name string
	tags string
}

var (
	buildB1    = svndBuild{name: "svnd-b1"}
	buildB2    = svndBuild{name: "svnd-b2", tags: "svn_rehearsal"}
	buildB2Bad = svndBuild{name: "svnd-b2-bad", tags: "svn_rehearsal_bad"}
)

// buildSvnd builds every build of builds into one temporary directory of
// t, concurrently, with GOPROXY=off (the network namespace has no network,
// so the module cache must hold every module). It prints each binary's
// sha256 and requires its version output to name app version 1 and no
// upgrade for the release build, and app version 2 and the rehearsal
// upgrade for a tagged build. The directory is removed with t.
func buildSvnd(t *testing.T, builds ...svndBuild) map[svndBuild]string {
	t.Helper()
	dir := t.TempDir()
	started := time.Now()
	errs := make([]error, len(builds))
	var wg sync.WaitGroup
	for i, b := range builds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := []string{"build", "-o", filepath.Join(dir, b.name)}
			if b.tags != "" {
				args = append(args, "-tags", b.tags)
			}
			cmd := exec.Command("go", append(args, "../cmd/svnd")...)
			cmd.Env = append(os.Environ(), "GOPROXY=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				errs[i] = fmt.Errorf("go build %s (tags %q): %v\n%s", b.name, b.tags, err, out)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	fmt.Printf("phase=build binaries=%d wall_seconds=%.1f label=%q\n", len(builds), time.Since(started).Seconds(), buildLabel)
	out := map[svndBuild]string{}
	for _, b := range builds {
		path := filepath.Join(dir, b.name)
		version, err := exec.Command(path, "version").CombinedOutput()
		if err != nil {
			t.Fatalf("%s version: %v\n%s", b.name, err, version)
		}
		wantApp, wantUpgrade := app.GenesisAppVersion, "upgrades none"
		if b.tags != "" {
			wantApp = rehearsal.ToAppVersion
			wantUpgrade = fmt.Sprintf("upgrade %s from_app_version %d to_app_version %d", rehearsal.UpgradeName, rehearsal.FromAppVersion, rehearsal.ToAppVersion)
		}
		text := string(version)
		if !strings.Contains(text, fmt.Sprintf("\napp_version %d\n", wantApp)) || !strings.Contains(text, "\n"+wantUpgrade+"\n") {
			t.Fatalf("%s version does not name app_version %d and %q:\n%s", b.name, wantApp, wantUpgrade, text)
		}
		fmt.Printf("phase=binary name=%s tags=%q sha256=%s app_version=%d version_line=%q\n", b.name, b.tags, fileSHA256(t, path), wantApp, wantUpgrade)
		out[b] = path
	}
	return out
}

// fileSHA256 is the hex SHA-256 of one file.
func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// upgradeNet is one devnet of an upgrade demo: the layout, the RPC
// addresses, the stop function of every running validator (nil when
// stopped), and the demo client that signs and submits through node v1.
type upgradeNet struct {
	t      *testing.T
	layout devnet.Layout
	home   string
	rpcs   []string
	stops  []func() error
	d      *demo
}

// startUpgradeNet initializes a devnet with opts (Home is set here),
// starts B1 (bin) on all four validators, and waits for an equal height of
// at least 3: the first commits after the load, so CheckTx accepts the
// transactions of normal accounts (DR-10-41). The test's cleanup stops
// every validator still running.
func startUpgradeNet(t *testing.T, opts devnet.Options, bin string) *upgradeNet {
	t.Helper()
	opts.Home = t.TempDir()
	layout, err := devnet.Init(opts)
	if err != nil {
		t.Fatal(err)
	}
	n := &upgradeNet{t: t, layout: layout, home: opts.Home, rpcs: rpcList(layout), stops: make([]func() error, len(layout.Topology.Validators))}
	t.Cleanup(func() {
		for _, stop := range n.stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	for i := range n.stops {
		n.start(i, bin)
	}
	if _, _, err := devnet.WaitEqual(n.rpcs, 3, 60*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	n.d = newDemo(t, layout, opts.Home)
	fmt.Printf("phase=devnet-start chain_id=%s genesis_sha256=%s council_delay_seconds=%d label=%q\n",
		layout.Topology.ChainID, layout.Topology.GenesisSHA256, opts.CouncilDelaySeconds, upgradeLabel)
	return n
}

// start launches bin on validator i with launchBinary.
func (n *upgradeNet) start(i int, bin string) {
	n.t.Helper()
	if n.stops[i] != nil {
		n.t.Fatalf("validator %d is already running", i)
	}
	stop, err := launchBinary(bin, n.layout.Topology.Validators[i].Home)
	if err != nil {
		n.t.Fatal(err)
	}
	n.stops[i] = stop
}

// stop stops the validators of indexes concurrently with their stop
// functions (SIGTERM, a kill after 20 s; exit 0, 143, or -1 is a clean
// stop) and prints how long each stop took.
func (n *upgradeNet) stop(label string, indexes ...int) {
	n.t.Helper()
	errs := make([]error, len(indexes))
	took := make([]time.Duration, len(indexes))
	var wg sync.WaitGroup
	for k, i := range indexes {
		stop := n.stops[i]
		if stop == nil {
			n.t.Fatalf("%s: validator %d is not running", label, i)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			errs[k] = stop()
			took[k] = time.Since(started)
		}()
	}
	wg.Wait()
	for k, i := range indexes {
		n.stops[i] = nil
		if errs[k] != nil {
			n.t.Fatalf("%s: stop validator %d: %v", label, i, errs[k])
		}
		fmt.Printf("phase=%s-stop node=%d seconds=%.1f\n", label, i, took[k].Seconds())
	}
}

// all is every validator index.
func (n *upgradeNet) all() []int {
	out := make([]int, len(n.stops))
	for i := range out {
		out[i] = i
	}
	return out
}

// nodeHome is validator i's home directory.
func (n *upgradeNet) nodeHome(i int) string { return n.layout.Topology.Validators[i].Home }

// checkHistory requires, on every running validator, that the home's
// genesis.json still has the topology's sha256 and that /status reports
// earliest_block_height 1: no step replaced genesis or pruned history.
func (n *upgradeNet) checkHistory(label string) {
	n.t.Helper()
	for i := range n.stops {
		if got := fileSHA256(n.t, filepath.Join(n.nodeHome(i), "config", "genesis.json")); got != n.layout.Topology.GenesisSHA256 {
			n.t.Fatalf("%s: node %d genesis.json sha256 %s, topology %s", label, i, got, n.layout.Topology.GenesisSHA256)
		}
		if n.stops[i] == nil {
			continue
		}
		status, err := n.d.clients[i].Status(context.Background())
		if err != nil {
			n.t.Fatalf("%s: node %d status: %v", label, i, err)
		}
		if status.SyncInfo.EarliestBlockHeight != 1 {
			n.t.Fatalf("%s: node %d earliest_block_height %d", label, i, status.SyncInfo.EarliestBlockHeight)
		}
	}
	fmt.Printf("phase=%s-history genesis_sha256=%s earliest_block_height=1\n", label, n.layout.Topology.GenesisSHA256)
}

// abciInfo is one node's /abci_info: the application's last committed
// height, its app hash, and its app version.
func abciInfo(t *testing.T, client *rpchttp.HTTP) (int64, []byte, uint64) {
	t.Helper()
	info, err := client.ABCIInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return info.Response.LastBlockHeight, info.Response.LastBlockAppHash, info.Response.AppVersion
}

// heightRow is one row of the per-height table: the block id hash, the
// header app hash (the state after the block before), the header
// Version.App, the header time, and the number of transactions.
type heightRow struct {
	blockHash  string
	appHash    string
	versionApp uint64
	unix       int64
	txs        int
}

func readHeight(t *testing.T, client *rpchttp.HTTP, height int64) heightRow {
	t.Helper()
	h := height
	res, err := client.Block(context.Background(), &h)
	if err != nil {
		t.Fatalf("block %d: %v", height, err)
	}
	return heightRow{
		blockHash:  res.BlockID.Hash.String(),
		appHash:    res.Block.AppHash.String(),
		versionApp: res.Block.Version.App,
		unix:       res.Block.Time.Unix(),
		txs:        len(res.Block.Txs),
	}
}

// printHeights prints the per-height table of every running node for
// heights and requires the nodes to agree on every row.
func (n *upgradeNet) printHeights(label string, heights ...int64) map[int64]heightRow {
	n.t.Helper()
	out := map[int64]heightRow{}
	for _, h := range heights {
		for i, client := range n.d.clients {
			if n.stops[i] == nil {
				continue
			}
			row := readHeight(n.t, client, h)
			if first, ok := out[h]; ok && first != row {
				n.t.Fatalf("%s: node %d height %d %+v, another node %+v", label, i, h, row, first)
			}
			out[h] = row
			fmt.Printf("phase=%s-height node=%d height=%d block_hash=%s header_app_hash=%s header_version_app=%d block_time=%d txs=%d\n",
				label, i, h, row.blockHash, row.appHash, row.versionApp, row.unix, row.txs)
		}
	}
	return out
}

// waitLogs waits until node.log of every validator in indexes contains
// every one of wants and prints the first matching line of each as a
// phase line (node.log is replaced at the next start).
func (n *upgradeNet) waitLogs(label string, timeout time.Duration, indexes []int, wants ...string) {
	n.t.Helper()
	deadline := time.Now().Add(timeout)
	for _, i := range indexes {
		path := filepath.Join(n.nodeHome(i), "node.log")
		for {
			raw, _ := os.ReadFile(path)
			missing := ""
			for _, want := range wants {
				if !bytes.Contains(raw, []byte(want)) {
					missing = want
					break
				}
			}
			if missing == "" {
				for _, want := range wants {
					fmt.Printf("phase=%s-log node=%d line=%q\n", label, i, matchingLine(raw, want))
				}
				break
			}
			if time.Now().After(deadline) {
				n.t.Fatal(tail(n.layout, fmt.Errorf("%s: node %d log has no %q within %s", label, i, missing, timeout)))
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
}

// matchingLine is the first line of raw that contains want, without
// terminal color sequences, cut to 400 bytes.
func matchingLine(raw []byte, want string) string {
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, want) {
			line = colorSequence.ReplaceAllString(line, "")
			if len(line) > 400 {
				line = line[:400]
			}
			return line
		}
	}
	return ""
}

// colorSequence matches the terminal color sequences of the node log.
var colorSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

// neededLine is the node log text of the I1 halt at height h: the
// PreBlock error UPGRADE "rehearsal-1" NEEDED at height h, as the log
// writes it (quotes escaped inside the err field).
func neededLine(h int64) string {
	return fmt.Sprintf(`UPGRADE \"%s\" NEEDED at height %d release_digest=%s`, rehearsal.UpgradeName, h, releaseHex())
}

// waitMempoolEmpty waits until every running node's mempool is empty.
func (n *upgradeNet) waitMempoolEmpty(timeout time.Duration) {
	n.t.Helper()
	deadline := time.Now().Add(timeout)
	for i, client := range n.d.clients {
		if n.stops[i] == nil {
			continue
		}
		for {
			res, err := client.NumUnconfirmedTxs(context.Background())
			if err == nil && res.Total == 0 {
				break
			}
			if time.Now().After(deadline) {
				n.t.Fatalf("node %d mempool not empty within %s (%v %v)", i, timeout, res, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	fmt.Println("phase=mempool-empty nodes=running num_unconfirmed_txs=0")
}

// copyTree copies the regular files and directories under src to dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// copyHome copies a stopped validator home into a new directory of t.
func copyHome(t *testing.T, home, name string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), name)
	copyTree(t, home, dst)
	return dst
}

// isolate applies the copy rules to a home copy before any binary starts
// on it (plan 12.5): a fresh throwaway validator key and a reset signing
// state (privval.GenFilePV), no persistent peers, and listen addresses of
// slot, distinct from the four validators'. A start on the copy can then
// neither sign as a validator of the devnet nor dial its peers.
func isolate(t *testing.T, home string, slot int) {
	t.Helper()
	cfgDir := filepath.Join(home, "config")
	privval.GenFilePV(filepath.Join(cfgDir, "priv_validator_key.json"), filepath.Join(home, "data", "priv_validator_state.json")).Save()
	p2p := fmt.Sprintf("tcp://127.0.0.1:%d", 27656+slot*10)
	rpc := fmt.Sprintf("tcp://127.0.0.1:%d", 27657+slot*10)
	grpc := fmt.Sprintf("127.0.0.1:%d", 27201+slot)
	var saved node.NodeFile
	readJSONFile(t, filepath.Join(cfgDir, "node.json"), &saved)
	saved.P2P, saved.RPC, saved.PersistentPeers = p2p, rpc, ""
	writeJSONFile(t, filepath.Join(cfgDir, "node.json"), saved)
	writeJSONFile(t, filepath.Join(cfgDir, "svn-listen.json"), node.Listen{GRPC: grpc, RPC: rpc, P2P: p2p})
}

func readJSONFile(t *testing.T, path string, out any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// refusedStart runs `bin start --home home` to its end (at most 90 s)
// and requires exit code 1, output that contains want, and no CometBFT
// node start line: the binary stopped before consensus started. It prints
// the refusal and how long the run took.
func refusedStart(t *testing.T, label, bin, home, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	started := time.Now()
	out, err := exec.CommandContext(ctx, bin, "start", "--home", home).CombinedOutput()
	took := time.Since(started)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("%s: start exited with %v, want exit 1\n%s", label, err, out)
	}
	if !bytes.Contains(out, []byte(want)) {
		t.Fatalf("%s: output does not contain %q:\n%s", label, want, out)
	}
	if bytes.Contains(out, []byte(nodeStartLine)) {
		t.Fatalf("%s: the refused binary logged %q:\n%s", label, nodeStartLine, out)
	}
	fmt.Printf("phase=%s exit=1 seconds=%.1f node_started=false refusal=%q\n", label, took.Seconds(), matchingLine(out, want))
}

// runReport runs the state check library (chain/internal/statecheck) on a
// stopped home or a copy at height (0: the latest) for the binary declared
// by upgrades and returns the report and its text.
func runReport(t *testing.T, label, home string, height int64, upgrades []upgrade.Upgrade) (*statecheck.Report, string) {
	t.Helper()
	binary, err := app.DeclaredBinary(upgrades)
	if err != nil {
		t.Fatal(err)
	}
	report, err := statecheck.Run(statecheck.Options{Home: home, Height: height, Binary: binary})
	if err != nil {
		t.Fatalf("%s: statecheck: %v", label, err)
	}
	var b strings.Builder
	if err := report.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	return report, b.String()
}

// printReport prints a report's text, one phase line per report line.
func printReport(label, text string) {
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		fmt.Printf("phase=%s statecheck=%q\n", label, line)
	}
}

// checkReport is runReport on one home, with the report printed.
func checkReport(t *testing.T, label, home string, height int64, upgrades []upgrade.Upgrade) *statecheck.Report {
	t.Helper()
	report, text := runReport(t, label, home, height, upgrades)
	printReport(label, text)
	return report
}

// sameReports runs the state check on every home at height for the binary
// declared by upgrades, requires byte-identical report texts (a report
// does not print the home path), prints the text once, and returns the
// first report.
func sameReports(t *testing.T, label string, homes []string, height int64, upgrades []upgrade.Upgrade) *statecheck.Report {
	t.Helper()
	first, text := runReport(t, label, homes[0], height, upgrades)
	for i, home := range homes[1:] {
		if _, other := runReport(t, label, home, height, upgrades); other != text {
			t.Fatalf("%s: the report of home %d differs from home 0:\n%s\n%s", label, i+1, other, text)
		}
	}
	printReport(label, text)
	fmt.Printf("phase=%s homes=%d reports_byte_identical=true\n", label, len(homes))
	return first
}

// requirePassed requires a report that passed with gate row and every
// checker passing.
func requirePassed(t *testing.T, label string, report *statecheck.Report, row int) {
	t.Helper()
	if !report.Passed() || report.Gate.Row != row {
		t.Fatalf("%s: report passed %t gate row %d (%s), want PASS with row %d", label, report.Passed(), report.Gate.Row, report.Gate.Refusal, row)
	}
	for _, check := range report.Checks {
		if check.Err != nil {
			t.Fatalf("%s: check %s: %v", label, check.Name, check.Err)
		}
	}
}

// storeText is the store digest lines of a report.
func storeText(report *statecheck.Report) string {
	var b strings.Builder
	for _, store := range report.Stores {
		fmt.Fprintf(&b, "%s=%x ", store.Name, store.SHA256)
	}
	return b.String()
}

// planPayload is the council software_upgrade payload of rehearsal-1 at
// height; its release digest is SHA-256 of rehearsalRelease.
func planPayload(height int64) *authorityv1.ProposalPayload {
	sum := sha256.Sum256([]byte(rehearsalRelease))
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_SoftwareUpgrade{
		SoftwareUpgrade: &authorityv1.SoftwareUpgradeChange{Name: rehearsal.UpgradeName, Height: uint64(height), ReleaseDigest: sha256Digest(sum[:])},
	}}
}

// releaseHex is the hex release digest the plan names.
func releaseHex() string {
	sum := sha256.Sum256([]byte(rehearsalRelease))
	return hex.EncodeToString(sum[:])
}

// propose commits seat-1's proposal id of payload on the current policy
// version (business id pb-<id>) with the expiry, and returns its height.
func (d *demo) propose(phase, id, parameterKey string, payload *authorityv1.ProposalPayload, expiry int64) int64 {
	d.t.Helper()
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		d.t.Fatal(out.Code)
	}
	return d.commit(phase+"-propose", d.seats[0], &authorityv1.MsgProposeChange{
		Signer: d.seats[0].key.PubKey().Address(), BusinessId: "pb-" + id, ProposalId: id,
		ParameterKey: parameterKey, BasePolicyVersion: d.policy, Payload: payload,
		PayloadDigest: sha256Digest(digest), ExpiryUnixSeconds: expiry,
	})
}

// approveMsg is seat n's approval of id with business id <id>-ap-<n>.
func (d *demo) approveMsg(id string, n int) *authorityv1.MsgApproveChange {
	seat := d.seats[n-1]
	return &authorityv1.MsgApproveChange{Signer: seat.key.PubKey().Address(), BusinessId: fmt.Sprintf("%s-ap-%d", id, n), ProposalId: id}
}

// approve commits seats 1..count approving id and returns the height of
// the last approval.
func (d *demo) approve(phase, id string, count int) int64 {
	d.t.Helper()
	var height int64
	for n := 1; n <= count; n++ {
		height = d.commit(fmt.Sprintf("%s-approve-%d", phase, n), d.seats[n-1], d.approveMsg(id, n))
	}
	return height
}

// executeMsg is the authority's execute of id (business id ex-<id>).
func (d *demo) executeMsg(id string) *authorityv1.MsgExecuteChange {
	return &authorityv1.MsgExecuteChange{Signer: d.authority.key.PubKey().Address(), BusinessId: "ex-" + id, ProposalId: id}
}

// execute commits the authority's execute of id, requires that the
// payload applied (code 0 and no kept authority_result), moves the demo's
// policy version on by one, and returns the committed result.
func (d *demo) execute(phase, id string) *cmtResult {
	d.t.Helper()
	res := d.send(d.authority, d.executeMsg(id))
	if res.deliver.Code != 0 || authorityResult(res.deliver.Events) != "" {
		d.t.Fatalf("phase %s execute %s: %s/%d result %q %s", phase, id, res.deliver.Codespace, res.deliver.Code, authorityResult(res.deliver.Events), res.deliver.Log)
	}
	d.policy++
	fmt.Printf("phase=%s-execute height=%d code=0 txhash=%X policy_version=%d\n", phase, res.height, res.hash, d.policy)
	return res
}

// authorityResult is the code of a kept authority_result event, or "".
func authorityResult(events []abci.Event) string {
	for _, ev := range events {
		if ev.Type != "authority_result" {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == "code" {
				return attr.Value
			}
		}
	}
	return ""
}

// idempotentResult reports whether events carry authority_result
// idempotent=true.
func idempotentResult(events []abci.Event) bool {
	for _, ev := range events {
		if ev.Type != "authority_result" {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == "idempotent" && attr.Value == "true" {
				return true
			}
		}
	}
	return false
}

// failCode is the protocol code of a committed DeliverTx failure in the
// svn codespace, or "" for another result.
func failCode(res abci.ExecTxResult) msgkit.Code {
	if res.Codespace != "svn" || res.Code == 0 {
		return ""
	}
	return msgkit.Code(typesv1.ErrorCode_name[int32(res.Code)])
}

// queryInto runs one ABCI query on client at height (the node's
// application must have committed it) and decodes the answer into out.
func (d *demo) queryInto(client *rpchttp.HTTP, path string, req, out proto.Message, height int64) {
	d.t.Helper()
	if err := proto.Unmarshal(d.leaseQuery(client, path, req, height), out); err != nil {
		d.t.Fatalf("%s at %d: %v", path, height, err)
	}
}

// policyAt is Query/Policy on client at height.
func (d *demo) policyAt(client *rpchttp.HTTP, height int64) *policyv1.PolicyState {
	d.t.Helper()
	var res policyv1.QueryPolicyResponse
	d.queryInto(client, "/svn.policy.v1.Query/Policy", &policyv1.QueryPolicyRequest{}, &res, height)
	return res.GetState()
}

// governanceAt is every page of Query/Governance on client at height,
// merged into one state (40 rows per page, the scenario's governed page
// limit).
func (d *demo) governanceAt(client *rpchttp.HTTP, height int64) *authorityv1.AuthorityState {
	d.t.Helper()
	merged := &authorityv1.AuthorityState{}
	var key []byte
	for page := 0; ; page++ {
		if page > 20 {
			d.t.Fatal("Query/Governance has more than 20 pages")
		}
		var res authorityv1.QueryGovernanceResponse
		d.queryInto(client, "/svn.authority.v1.Query/Governance", &authorityv1.QueryGovernanceRequest{Page: &typesv1.PageRequest{Limit: 40, PageKey: key}}, &res, height)
		proto.Merge(merged, res.GetState())
		key = res.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return merged
		}
	}
}

// proposalAt is the state of proposal id in Query/Governance at height.
func (d *demo) proposalAt(client *rpchttp.HTTP, id string, height int64) *authorityv1.Proposal {
	d.t.Helper()
	for _, p := range d.governanceAt(client, height).GetProposals() {
		if p.GetProposalId() == id {
			return p
		}
	}
	d.t.Fatalf("no proposal %s at height %d", id, height)
	return nil
}

// rawStore is the value of key in store name on client at height (empty
// when absent), read with a raw /store query.
func (d *demo) rawStore(client *rpchttp.HTTP, name string, key []byte, height int64) []byte {
	d.t.Helper()
	res, err := client.ABCIQueryWithOptions(context.Background(), "/store/"+name+"/key", key, rpcclient.ABCIQueryOptions{Height: height})
	if err != nil {
		d.t.Fatal(err)
	}
	if res.Response.Code != 0 || res.Response.Height != height {
		d.t.Fatalf("/store/%s/key %x at %d: code %d height %d %s", name, key, height, res.Response.Code, res.Response.Height, res.Response.Log)
	}
	return res.Response.Value
}

// leaseAt is the stored lease record id (any state) at height.
func (d *demo) leaseAt(client *rpchttp.HTTP, id string, height int64) *leasesv1.Lease {
	d.t.Helper()
	raw := d.rawStore(client, app.ModuleLeases, leases.LeaseKey(id), height)
	if len(raw) == 0 {
		d.t.Fatalf("no lease %s at height %d", id, height)
	}
	var lease leasesv1.Lease
	if err := proto.Unmarshal(raw, &lease); err != nil {
		d.t.Fatal(err)
	}
	return &lease
}

// openOffers is the ids of the open offers in Query/Deadlines at height.
func (d *demo) openOffers(client *rpchttp.HTTP, height int64) map[string]bool {
	d.t.Helper()
	out := map[string]bool{}
	var key []byte
	for page := 0; ; page++ {
		if page > 20 {
			d.t.Fatal("Query/Deadlines has more than 20 pages")
		}
		var res leasesv1.QueryDeadlinesResponse
		d.queryInto(client, deadlinesPath, &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: genesis.UnixSecondsMax, Page: &typesv1.PageRequest{Limit: 40, PageKey: key}}, &res, height)
		for _, offer := range res.GetOffers() {
			out[offer.GetOfferId()] = true
		}
		key = res.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return out
		}
	}
}

// waitHeight waits until every running node's application has committed
// height.
func (n *upgradeNet) waitHeight(height int64, timeout time.Duration) {
	n.t.Helper()
	deadline := time.Now().Add(timeout)
	for i, client := range n.d.clients {
		if n.stops[i] == nil {
			continue
		}
		for {
			last, _, _ := abciInfoQuiet(client)
			if last >= height {
				break
			}
			if time.Now().After(deadline) {
				n.t.Fatal(tail(n.layout, fmt.Errorf("node %d did not commit height %d within %s (at %d)", i, height, timeout, last)))
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// abciInfoQuiet is abciInfo with the error returned (a node that is still
// starting answers with an error).
func abciInfoQuiet(client *rpchttp.HTTP) (int64, []byte, error) {
	info, err := client.ABCIInfo(context.Background())
	if err != nil {
		return 0, nil, err
	}
	return info.Response.LastBlockHeight, info.Response.LastBlockAppHash, nil
}

// firstAtOrAfter is the first height in (lo, hi] whose header time is at
// least when (header times do not decrease), read from node v1.
func (d *demo) firstAtOrAfter(when, lo, hi int64) int64 {
	d.t.Helper()
	if d.blockTime(hi) < when {
		d.t.Fatalf("no height up to %d has a header time at or after %d", hi, when)
	}
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		if d.blockTime(mid) >= when {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// rehearsalRun is the representative scenario (plan 12.3) delivered on a
// devnet with the devnet helpers: the level that maps the scenario's
// symbolic times to header times and its symbolic heights to real ones.
type rehearsalRun struct {
	n *upgradeNet
	d *demo
	// auditor signs the scenario's audit batches (auditor-1).
	auditor loaded
	steps   []scenario.Step
	index   int
	// times maps each instant to unix seconds of header time. Every
	// instant is fixed at the first step that needs one (fixTimes).
	times map[scenario.Instant]int64
	// far is the expiry of every council proposal without an Expiry.
	far int64
	// h is the plan height and exec the plan's execute height.
	h, exec int64
	// offers are the scenario's offer messages by id, leaseOffer maps a
	// lease id to the offer it accepted.
	offers     map[string]*leasesv1.MsgOfferLease
	leaseOffer map[string]string
	// validatorKey is the consensus key of the scenario's new validator
	// (no process runs it).
	validatorKey []byte
	// started, startHeight, startUnix, txs, lastWall, and lastHeight
	// measure the setup for fixTimes and the plan height: the wall time,
	// height, and header time at the start, the committed transactions,
	// and the wall time and height of the last one.
	started     time.Time
	startHeight int64
	startUnix   int64
	txs         int
	lastWall    time.Time
	lastHeight  int64
}

// resumeAllowance is the time fixTimes allows from block h-1 to block
// h+2: the halt, the stops, the copies, the B1 refusal, and the start of
// B2. crossMargin is the margin M after that estimate before the first
// crossing time (plan 12.3). Both are PROPOSED test parameters in seconds,
// not approved values. A run whose h+2 comes later than the first crossing
// time fails with "halt exceeded the margin".
const (
	resumeAllowance = 40
	crossMargin     = 60
)

func newRehearsalRun(n *upgradeNet) *rehearsalRun {
	n.t.Helper()
	d := n.d
	r := &rehearsalRun{
		n: n, d: d, auditor: loadEd(n.t, n.home, "auditor-1"), steps: scenario.Steps(),
		offers: map[string]*leasesv1.MsgOfferLease{}, leaseOffer: map[string]string{},
		validatorKey: cmted25519.GenPrivKey().PubKey().Bytes(),
		far:          d.chainTime() + 24*3600,
	}
	r.started, r.startHeight = time.Now(), d.height()
	r.startUnix = d.blockTime(r.startHeight)
	return r
}

// messages is the number of transactions a step sends.
func messages(step scenario.Step) int {
	switch s := step.(type) {
	case scenario.SetTime:
		return 0
	case scenario.Council:
		n := 1 + s.Approvals
		if s.Execute {
			n++
		}
		return n
	}
	return 1
}

// messagesUntil counts the transactions of steps up to and including the
// first step for which stop is true.
func messagesUntil(steps []scenario.Step, stop func(scenario.Step) bool) int {
	total := 0
	for _, step := range steps {
		total += messages(step)
		if stop(step) {
			return total
		}
	}
	return total
}

func isPlanTime(step scenario.Step) bool {
	s, ok := step.(scenario.SetTime)
	return ok && s.At == scenario.AtPlan
}

func isUpgrade(step scenario.Step) bool {
	s, ok := step.(scenario.Council)
	if !ok {
		return false
	}
	_, upgrade := s.Change.(scenario.SoftwareUpgrade)
	return upgrade
}

// rates are the setup's measured seconds of wall time per committed
// transaction and blocks per transaction, both up to the last committed
// transaction (a wait for a header time is not counted), and header
// seconds per block up to the latest block.
func (r *rehearsalRun) rates() (perTx, perBlock, blocksPerTx float64) {
	r.n.t.Helper()
	height := r.d.height()
	if r.txs == 0 || r.lastHeight <= r.startHeight || height <= r.startHeight {
		r.n.t.Fatal("no setup transaction to measure")
	}
	perTx = r.lastWall.Sub(r.started).Seconds() / float64(r.txs)
	perBlock = float64(r.d.blockTime(height)-r.startUnix) / float64(height-r.startHeight)
	blocksPerTx = float64(r.lastHeight-r.startHeight) / float64(r.txs)
	return perTx, perBlock, blocksPerTx
}

// counted records one committed scenario transaction at height.
func (r *rehearsalRun) counted(height int64) {
	r.txs++
	r.lastWall, r.lastHeight = time.Now(), height
}

// planSlack is the number of blocks the plan height keeps beyond the
// minimum lead for txs transactions before the plan's execute.
func planSlack(txs int, blocksPerTx float64) int64 {
	return int64(math.Ceil(float64(txs)*blocksPerTx*1.5)) + 10
}

// fixTimes maps every instant to header time, at the first step that
// needs one, from the setup measured so far:
//
//   - EvidenceExpiresEarly after the estimated commit of the last setup
//     step that needs it (twice the measured time per transaction, plus 30
//     s for an audit round boundary wait), then LeaseExpiresEarly and
//     AtPlan 5 s apart.
//   - AtResume: the estimated header time of h+2: the plan phase starts
//     at AtPlan or when the setup ends, h-1 comes MinActivationLeadBlocks
//     plus the plan slack blocks later at the measured block interval,
//     and resumeAllowance seconds cover the halt and the swap.
//   - The crossing times from AtResume plus crossMargin, 8 s apart, in
//     scenario order; HeldDeadline 3000 s after now (one DEC-005 term,
//     3600 s, bounds an offer's term, not its deadline).
func (r *rehearsalRun) fixTimes() {
	t := r.n.t
	t.Helper()
	now := r.d.chainTime()
	perTx, perBlock, blocksPerTx := r.rates()
	rest := r.steps[r.index:]
	early := messagesUntil(rest, func(s scenario.Step) bool {
		a, ok := s.(scenario.Audit)
		return ok && a.Expiry == scenario.EvidenceExpiresEarly
	})
	setup := messagesUntil(rest, isPlanTime)
	planIndex := 0
	for i, step := range r.steps {
		if isPlanTime(step) {
			planIndex = i
		}
	}
	toExecute := messagesUntil(r.steps[planIndex:], isUpgrade)
	evidenceEarly := now + int64(math.Ceil(float64(early)*perTx*2)) + 30
	atPlan := evidenceEarly + 10
	planStart := max(atPlan, now+int64(math.Ceil(float64(setup)*perTx)))
	lead := int64(msgkit.MinActivationLeadBlocks) + planSlack(toExecute, blocksPerTx)
	resume := planStart + int64(math.Ceil(float64(lead)*perBlock)) + resumeAllowance
	cross := resume + crossMargin
	r.times = map[scenario.Instant]int64{
		scenario.AtSetup:              now,
		scenario.EvidenceExpiresEarly: evidenceEarly,
		scenario.LeaseExpiresEarly:    evidenceEarly + 5,
		scenario.AtPlan:               atPlan,
		scenario.AtResume:             resume,
		scenario.EvidenceCrossExpiry:  cross,
		scenario.OfferCrossExpiry:     cross + 8,
		scenario.LeaseCrossDeadline:   cross + 16,
		scenario.ProposalCrossExpiry:  cross + 24,
		scenario.HeldDeadline:         now + 3000,
	}
	var last int64
	for _, instant := range scenario.Instants() {
		if r.times[instant] <= last {
			t.Fatalf("instant %d at %d is not after %d", instant, r.times[instant], last)
		}
		last = r.times[instant]
	}
	fmt.Printf("phase=scenario-times now=%d seconds_per_tx=%.2f seconds_per_block=%.2f blocks_per_tx=%.2f evidence_expires_early=%d lease_expires_early=%d at_plan=%d estimated_h_plus_2=%d evidence_cross=%d offer_cross=%d lease_cross=%d proposal_cross=%d held_deadline=%d\n",
		now, perTx, perBlock, blocksPerTx, r.times[scenario.EvidenceExpiresEarly], r.times[scenario.LeaseExpiresEarly], atPlan, resume,
		r.times[scenario.EvidenceCrossExpiry], r.times[scenario.OfferCrossExpiry], r.times[scenario.LeaseCrossDeadline],
		r.times[scenario.ProposalCrossExpiry], r.times[scenario.HeldDeadline])
}

// at is the header time of instant i.
func (r *rehearsalRun) at(i scenario.Instant) int64 {
	if r.times == nil {
		r.fixTimes()
	}
	return r.times[i]
}

// role is the demo account of a scenario role.
func (r *rehearsalRun) role(role scenario.Role) loaded {
	switch role {
	case scenario.Authority:
		return r.d.authority
	case scenario.Service:
		return r.d.service
	case scenario.Provider1:
		return r.d.provider1
	case scenario.Provider2:
		return r.d.provider2
	case scenario.Broadcaster:
		return r.d.broadcaster
	case scenario.Auditor:
		return r.auditor
	case scenario.Seat1:
		return r.d.seats[0]
	case scenario.Seat2:
		return r.d.seats[1]
	case scenario.Seat3:
		return r.d.seats[2]
	}
	r.n.t.Fatalf("unknown role %s", role)
	return loaded{}
}

// evidenceID is the chain id of a scenario evidence id: the auditor's
// 8-byte id prefix (deviation D-17) and the scenario id.
func (r *rehearsalRun) evidenceID(id string) string { return idPrefix(r.auditor) + id }

// commit commits one scenario message (code 0) and counts it.
func (r *rehearsalRun) commit(label string, who loaded, msg sdk.Msg) {
	r.n.t.Helper()
	r.counted(r.d.commit(label, who, msg))
}

// auditEpoch is the audit round of the next block: the round of the
// latest header time, after waiting past a round boundary that is less
// than 15 s away (a batch must name the round of its committed time).
func (r *rehearsalRun) auditEpoch() uint64 {
	r.n.t.Helper()
	now := r.d.chainTime()
	next := (int64(evidence.EpochAt(now)) + 1) * evidence.EpochSeconds
	if next-now < 15 {
		fmt.Printf("phase=audit-round-wait header_time=%d boundary=%d\n", now, next)
		now = r.d.blockTime(r.d.waitChainTime(next+1, 60*time.Second))
	}
	return evidence.EpochAt(now)
}

// run delivers every scenario step in order with the devnet helpers;
// every message must commit with code 0.
func (r *rehearsalRun) run() {
	t, d := r.n.t, r.d
	t.Helper()
	for i, step := range r.steps {
		r.index = i
		bid := "bid-" + step.Label()
		switch s := step.(type) {
		case scenario.SetTime:
			if s.At == scenario.AtPlan {
				r.enterPlan()
			}
		case scenario.RegisterProvider:
			who := r.role(s.Signer)
			r.commit(s.Name, who, d.registerProvider(who, bid, s.ProviderID, capacityOne))
		case scenario.DrainProvider:
			who := r.role(s.Signer)
			r.commit(s.Name, who, d.drain(who, bid, s.ProviderID, 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN))
		case scenario.RegisterContent:
			r.commit(s.Name, d.service, d.registerContent(d.service, bid, s.ContentID, 1, sizeOne))
		case scenario.Offer:
			msg := d.offerMsg(bid, s.OfferID, s.ContentID, s.ProviderID, sizeOne, r.at(s.Deadline), r.at(s.Expiry))
			r.offers[s.OfferID] = msg
			r.commit(s.Name, d.service, msg)
		case scenario.Accept:
			offer := r.offers[s.OfferID]
			r.leaseOffer[s.LeaseID] = s.OfferID
			r.commit(s.Name, d.broadcaster, d.acceptMsg(d.broadcaster, bid, offer, s.LeaseID, d.keys[offer.GetProviderId()]))
		case scenario.Renew:
			offer := r.offers[r.leaseOffer[s.LeaseID]]
			r.commit(s.Name, d.broadcaster, d.renewMsg(d.broadcaster, bid, s.LeaseID, offer.GetProviderId(), offer.GetContentId(), sizeOne, 2,
				offer.GetDeadlineUnixSeconds()+devnetTerm))
		case scenario.End:
			offer := r.offers[r.leaseOffer[s.LeaseID]]
			r.commit(s.Name, d.broadcaster, d.endMsg(d.broadcaster, bid, s.LeaseID, offer.GetProviderId(), 1))
		case scenario.Audit:
			observation := leaseManifest(s.ContentID).GetValue()
			if !s.Matches {
				sum := sha256.Sum256([]byte("svn-audit-report-synthetic-fail:" + s.Name))
				observation = sum[:]
			}
			item := auditItem(r.evidenceID(s.EvidenceID), s.LeaseID, s.ContentID, observation, r.at(s.Expiry))
			r.commit(s.Name, r.auditor, signedBatch(t, r.auditor, bid, idPrefix(r.auditor)+"b-"+s.Name, r.auditEpoch(), item))
		case scenario.Council:
			r.council(s)
		default:
			t.Fatalf("step %s: unknown kind %T", step.Label(), step)
		}
	}
}

// enterPlan starts the plan phase: it waits until the header time passed
// AtPlan (so LeaseExpiresEarly and EvidenceExpiresEarly are behind) and
// fixes the plan height h: the current height plus
// MinActivationLeadBlocks plus the plan slack for the transactions up to
// the plan's execute, at the measured blocks per transaction.
func (r *rehearsalRun) enterPlan() {
	t := r.n.t
	t.Helper()
	r.d.waitChainTime(r.at(scenario.AtPlan)+1, 300*time.Second)
	_, _, blocksPerTx := r.rates()
	toExecute := messagesUntil(r.steps[r.index:], isUpgrade)
	height := r.d.height()
	r.h = height + int64(msgkit.MinActivationLeadBlocks) + planSlack(toExecute, blocksPerTx)
	fmt.Printf("phase=plan-height current=%d header_time=%d h=%d transactions_to_execute=%d blocks_per_tx=%.2f min_lead=%d (PROPOSED)\n",
		height, r.d.chainTime(), r.h, toExecute, blocksPerTx, msgkit.MinActivationLeadBlocks)
}

// payload builds the council payload of a scenario change.
func (r *rehearsalRun) payload(change scenario.Change) *authorityv1.ProposalPayload {
	switch c := change.(type) {
	case scenario.AdmitProvider:
		return admitChange(c.ProviderID)
	case scenario.GovernedLimit:
		return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{GovernedLimit: &authorityv1.GovernedLimitChange{
			LimitKey: c.Key, Value: c.Value, ActivationHeight: r.afterPlan(c.Activation),
		}}}
	case scenario.ValidatorPower:
		return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: &authorityv1.ValidatorPowerChange{
			ConsensusPubkey: r.validatorKey,
			Power:           c.Power,
			NodeId:          c.NodeID,
			ReasonDigest:    sha256Digest(reason("rehearsal validator change")),
			Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: r.afterPlan(c.Activation)}},
		}}}
	case scenario.SoftwareUpgrade:
		if c.Name != rehearsal.UpgradeName {
			r.n.t.Fatalf("the scenario plans %s, B2 carries %s", c.Name, rehearsal.UpgradeName)
		}
		return planPayload(r.h)
	}
	r.n.t.Fatalf("unknown change %T", change)
	return nil
}

// afterPlan is the height h+a (0 stays 0: an immediate change).
func (r *rehearsalRun) afterPlan(a scenario.AfterPlan) uint64 {
	if a == 0 {
		return 0
	}
	if r.h == 0 {
		r.n.t.Fatal("a scheduled change before the plan height is fixed")
	}
	return uint64(r.h + int64(a))
}

// council delivers one scenario council step: seat-1's propose, the
// approvals of seats 1, 2, 3 in order, and the authority's execute, one
// transaction each.
func (r *rehearsalRun) council(s scenario.Council) {
	t, d := r.n.t, r.d
	t.Helper()
	expiry := r.far
	if s.Expiry != 0 {
		expiry = r.at(s.Expiry)
	}
	r.counted(d.propose(s.Name, s.ProposalID, s.Change.ParameterKey(), r.payload(s.Change), expiry))
	for n := 1; n <= s.Approvals; n++ {
		r.commit(fmt.Sprintf("%s-approve-%d", s.Name, n), d.seats[n-1], d.approveMsg(s.ProposalID, n))
	}
	if !s.Execute {
		return
	}
	res := d.execute(s.Name, s.ProposalID)
	r.counted(res.height)
	if _, ok := s.Change.(scenario.SoftwareUpgrade); ok {
		r.exec = res.height
		if r.h-r.exec < int64(msgkit.MinActivationLeadBlocks) {
			t.Fatalf("plan height %d is %d blocks after the execute at %d, below the minimum lead %d", r.h, r.h-r.exec, r.exec, msgkit.MinActivationLeadBlocks)
		}
		fmt.Printf("phase=plan-executed proposal=%s execute_height=%d h=%d lead=%d min_lead=%d (PROPOSED)\n", s.ProposalID, r.exec, r.h, r.h-r.exec, msgkit.MinActivationLeadBlocks)
	}
}

// census reads the scenario census at height from node v1: providers
// through Query/Provider, leases from their stored records (a raw store
// read; no query lists ended or expired leases), open offers through
// Query/Deadlines, evidence through Query/Evidence (the state reported at
// that height's header time), proposals and pending validator changes
// through Query/Governance, pending limit changes through Query/Policy,
// and governed values from the stored x/policy rows (Query/Policy reports
// the value in force of every key, a ceiling included, so it does not show
// whether the council set one).
func (r *rehearsalRun) census(height int64) scenario.Census {
	d := r.d
	r.n.t.Helper()
	client := d.clients[0]
	got := scenario.Census{}
	for _, id := range []string{scenario.ProviderAdmitted, scenario.ProviderDraining} {
		var res providersv1.QueryProviderResponse
		d.queryInto(client, providerPath, &providersv1.QueryProviderRequest{ProviderId: id}, &res, height)
		switch res.GetProvider().GetState() {
		case providersv1.ProviderState_PROVIDER_STATE_ADMITTED:
			got[scenario.ProvidersAdmitted]++
		case providersv1.ProviderState_PROVIDER_STATE_DRAINING:
			got[scenario.ProvidersDraining]++
		}
	}
	for _, id := range []string{scenario.LeaseRenewed, scenario.LeaseCross, scenario.LeaseExpired, scenario.LeaseEnded, scenario.LeaseHeld} {
		switch d.leaseAt(client, id, height).GetState() {
		case leasesv1.LeaseState_LEASE_STATE_ACTIVE:
			got[scenario.LeasesActive]++
		case leasesv1.LeaseState_LEASE_STATE_RENEWED:
			got[scenario.LeasesRenewed]++
		case leasesv1.LeaseState_LEASE_STATE_EXPIRED:
			got[scenario.LeasesExpired]++
		case leasesv1.LeaseState_LEASE_STATE_ENDED:
			got[scenario.LeasesEnded]++
		}
	}
	open := d.openOffers(client, height)
	for _, id := range []string{scenario.OfferOpen, scenario.OfferCross} {
		if open[id] {
			got[scenario.OffersOpen]++
		}
	}
	for _, id := range []string{scenario.EvidenceRecorded, scenario.EvidenceRejected, scenario.EvidenceExpired, scenario.EvidenceCross} {
		switch d.reportedState(r.evidenceID(id), height) {
		case evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED:
			got[scenario.EvidenceRecordedClass]++
		case evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED:
			got[scenario.EvidenceRejectedClass]++
		case evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED:
			got[scenario.EvidenceExpiredClass]++
		}
	}
	gov := d.governanceAt(client, height)
	states := map[string]authorityv1.ProposalState{}
	for _, p := range gov.GetProposals() {
		states[p.GetProposalId()] = p.GetState()
	}
	for _, id := range []string{"admit-p1", "admit-p2", scenario.ProposalGoverned, scenario.ProposalPending, scenario.ProposalValidator,
		scenario.ProposalPlan, scenario.ProposalQueued, scenario.ProposalOpen} {
		switch states[id] {
		case authorityv1.ProposalState_PROPOSAL_STATE_OPEN:
			got[scenario.ProposalsOpen]++
		case authorityv1.ProposalState_PROPOSAL_STATE_QUEUED:
			got[scenario.ProposalsQueued]++
		case authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED:
			got[scenario.ProposalsExecuted]++
		}
	}
	got[scenario.PendingValidatorChanges] = len(gov.GetPendingValidatorPower())
	pol := d.policyAt(client, height)
	got[scenario.PendingLimitChanges] = len(pol.GetPendingLimitChanges())
	for _, key := range msgkit.LimitKeys() {
		if _, set := d.governedRow(client, key, height); set {
			got[scenario.GovernedValues]++
		}
	}
	return got
}

// checkCensus reads the census at height, prints it, and requires the
// scenario's census for phase.
func (r *rehearsalRun) checkCensus(label string, height int64, phase scenario.Phase) {
	r.n.t.Helper()
	got := r.census(height)
	fmt.Printf("phase=census-%s height=%d %s\n", label, height, got)
	if err := got.Check(scenario.Want(phase), phase); err != nil {
		r.n.t.Fatalf("%s at height %d: %v", label, height, err)
	}
}

// objectAnswers are the query answers for every representative object at
// height on node v1 (plan 12.7), by name, in deterministic protobuf:
// providers, stored lease records, evidence records, open offers and held
// leases (Query/Deadlines), proposals and validators (Query/Governance),
// and the CometBFT validator set. Query/Policy is compared field by field
// (comparePolicy).
func (r *rehearsalRun) objectAnswers(height int64) map[string][]byte {
	t, d := r.n.t, r.d
	t.Helper()
	client := d.clients[0]
	out := map[string][]byte{}
	for _, id := range []string{scenario.ProviderAdmitted, scenario.ProviderDraining} {
		out["provider "+id] = d.leaseQuery(client, providerPath, &providersv1.QueryProviderRequest{ProviderId: id}, height)
	}
	for _, id := range []string{scenario.LeaseRenewed, scenario.LeaseCross, scenario.LeaseExpired, scenario.LeaseEnded, scenario.LeaseHeld} {
		out["lease "+id] = d.rawStore(client, app.ModuleLeases, leases.LeaseKey(id), height)
	}
	for _, id := range []string{scenario.EvidenceRecorded, scenario.EvidenceRejected, scenario.EvidenceExpired, scenario.EvidenceCross} {
		out["evidence "+id] = d.leaseQuery(client, evidencePath, &evidencev1.QueryEvidenceRequest{EvidenceId: r.evidenceID(id)}, height)
	}
	out["deadlines"] = d.leaseQuery(client, deadlinesPath, &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: genesis.UnixSecondsMax, Page: &typesv1.PageRequest{Limit: 40}}, height)
	gov, err := proto.MarshalOptions{Deterministic: true}.Marshal(d.governanceAt(client, height))
	if err != nil {
		t.Fatal(err)
	}
	out["governance"] = gov
	set, err := json.Marshal(validatorSet(t, r.n.rpcs[0], height))
	if err != nil {
		t.Fatal(err)
	}
	out["validators"] = set
	return out
}

// comparePolicy requires Query/Policy at after to equal the answer at
// before except for exactly the upgrade's fields: app version 1 -> 2,
// the plan gone, and the policy version one higher (DR-10-38).
func (r *rehearsalRun) comparePolicy(before, after int64) {
	t, d := r.n.t, r.d
	t.Helper()
	pre, post := d.policyAt(d.clients[0], before), d.policyAt(d.clients[0], after)
	if pre.GetAppVersion() != rehearsal.FromAppVersion || post.GetAppVersion() != rehearsal.ToAppVersion {
		t.Fatalf("Query/Policy app version %d at %d, %d at %d", pre.GetAppVersion(), before, post.GetAppVersion(), after)
	}
	if plan := pre.GetUpgradePlan(); plan.GetName() != rehearsal.UpgradeName || plan.GetHeight() != uint64(r.h) || hex.EncodeToString(plan.GetReleaseDigest().GetValue()) != releaseHex() {
		t.Fatalf("Query/Policy plan at %d: %v", before, plan)
	}
	if post.GetUpgradePlan() != nil || post.GetPolicyVersion() != pre.GetPolicyVersion()+1 {
		t.Fatalf("Query/Policy at %d: plan %v policy version %d (before %d)", after, post.GetUpgradePlan(), post.GetPolicyVersion(), pre.GetPolicyVersion())
	}
	a, b := proto.Clone(pre).(*policyv1.PolicyState), proto.Clone(post).(*policyv1.PolicyState)
	a.AppVersion, a.UpgradePlan, a.PolicyVersion = 0, nil, 0
	b.AppVersion, b.UpgradePlan, b.PolicyVersion = 0, nil, 0
	if !proto.Equal(a, b) {
		t.Fatalf("Query/Policy differs beyond the upgrade fields:\n%v\n%v", a, b)
	}
	fmt.Printf("phase=compare-policy before=%d after=%d app_version=%d->%d plan=%s->none policy_version=%d->%d other_fields=equal\n",
		before, after, pre.GetAppVersion(), post.GetAppVersion(), rehearsal.UpgradeName, pre.GetPolicyVersion(), post.GetPolicyVersion())
}

// transitions asserts every scenario transition (scenario.Transitions)
// under B2 at its effective-1 and effective points, by committed heights
// and header times: the governed activation at h+3, the validator change
// at h+4, and each crossing time at the first height whose header time
// reaches it. approveHeight is the height of the OPEN proposal's third
// approval, committed after the upgrade and before its expiry.
func (r *rehearsalRun) transitions(approveHeight int64) {
	t, d := r.n.t, r.d
	t.Helper()
	client := d.clients[0]
	for _, tr := range scenario.Transitions() {
		var before, after int64
		switch tr.Object {
		case scenario.PendingKey:
			after = r.h + int64(tr.At)
			before = after - 1
			r.n.waitHeight(after, 60*time.Second)
			pre, post := d.policyAt(client, before), d.policyAt(client, after)
			if _, set := d.governedRow(client, tr.Object, before); set || !pendingFor(pre, tr.Object, uint64(after)) {
				t.Fatalf("%s at a-1=%d: governed row set %t pending %v", tr.Object, before, set, pre.GetPendingLimitChanges())
			}
			value, set := d.governedRow(client, tr.Object, after)
			if !set || strconv.FormatUint(value, 10) != tr.After || governedValue(post, tr.Object) != tr.After || len(post.GetPendingLimitChanges()) != 0 {
				t.Fatalf("%s at a=%d: governed row %d set %t in force %q pending %v", tr.Object, after, value, set, governedValue(post, tr.Object), post.GetPendingLimitChanges())
			}
		case scenario.ValidatorNode:
			after = r.h + int64(tr.At)
			before = after - 1
			r.n.waitHeight(after+2, 60*time.Second)
			if n := len(d.governanceAt(client, before).GetPendingValidatorPower()); n != 1 || len(r.validatorUpdates(before)) != 0 {
				t.Fatalf("%s at %d: %d pending, updates %v", tr.Object, before, n, r.validatorUpdates(before))
			}
			updates := r.validatorUpdates(after)
			if len(d.governanceAt(client, after).GetPendingValidatorPower()) != 0 || len(updates) != 1 ||
				!bytes.Equal(updates[0].PubKey.GetEd25519(), r.validatorKey) || updates[0].Power != scenario.ValidatorPowerN {
				t.Fatalf("%s at %d: updates %v", tr.Object, after, updates)
			}
			// CometBFT applies a validator update of block H from H+2.
			if inSet(validatorSet(t, r.n.rpcs[0], after+1), r.validatorKey) || !inSet(validatorSet(t, r.n.rpcs[0], after+2), r.validatorKey) {
				t.Fatalf("%s: validator set at %d/%d", tr.Object, after+1, after+2)
			}
		case scenario.EvidenceCross, scenario.OfferCross, scenario.LeaseCross:
			when := r.at(tr.Effective)
			top := d.waitChainTime(when+1, 300*time.Second)
			r.n.waitHeight(top, 60*time.Second)
			after = d.firstAtOrAfter(when, r.h+2, top)
			before = after - 1
			if d.blockTime(before) >= when || d.blockTime(after) < when {
				t.Fatalf("%s: header times %d/%d around %d", tr.Object, d.blockTime(before), d.blockTime(after), when)
			}
			r.crossState(tr, before, after)
		case scenario.ProposalOpen:
			when := r.at(tr.Effective)
			if d.blockTime(approveHeight) >= when {
				t.Fatalf("%s: the approval at %d (header time %d) is not before the expiry %d", tr.Object, approveHeight, d.blockTime(approveHeight), when)
			}
			d.waitChainTime(when, 300*time.Second)
			res := d.send(d.authority, d.executeMsg(tr.Object))
			if res.deliver.Code != 0 || authorityResult(res.deliver.Events) != string(msgkit.Expired) {
				t.Fatalf("%s execute at its expiry: %s/%d result %q", tr.Object, res.deliver.Codespace, res.deliver.Code, authorityResult(res.deliver.Events))
			}
			after = res.height
			before = after - 1
			if d.blockTime(after) < when {
				t.Fatalf("%s: the execute's header time %d is before the expiry %d", tr.Object, d.blockTime(after), when)
			}
			r.n.waitHeight(after, 60*time.Second)
			if got := d.proposalAt(client, tr.Object, before).GetState().String(); got != "PROPOSAL_STATE_"+tr.Before {
				t.Fatalf("%s at %d: %s", tr.Object, before, got)
			}
			if got := d.proposalAt(client, tr.Object, after).GetState().String(); got != "PROPOSAL_STATE_"+tr.After {
				t.Fatalf("%s at %d: %s", tr.Object, after, got)
			}
		default:
			t.Fatalf("transition %s has no check", tr.Object)
		}
		fmt.Printf("phase=transition object=%s class=%q before=%s at_height=%d header_time=%d after=%s at_height=%d header_time=%d\n",
			tr.Object, tr.Class, tr.Before, before, d.blockTime(before), tr.After, after, d.blockTime(after))
	}
}

// crossState asserts one crossing time's object at effective-1 and
// effective, and for an offer or a lease the BeginBlock expiry event at
// effective.
func (r *rehearsalRun) crossState(tr scenario.Transition, before, after int64) {
	t, d := r.n.t, r.d
	t.Helper()
	client := d.clients[0]
	switch tr.Object {
	case scenario.EvidenceCross:
		d.wantReported("evidence-cross-before", r.evidenceID(tr.Object), before, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED)
		d.wantReported("evidence-cross-after", r.evidenceID(tr.Object), after, evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED)
	case scenario.OfferCross:
		if !d.openOffers(client, before)[tr.Object] || d.openOffers(client, after)[tr.Object] {
			t.Fatalf("%s open at %d/%d", tr.Object, before, after)
		}
		if !slices.Contains(d.blockExpiry(after), expiryItem{"offer", tr.Object}) {
			t.Fatalf("%s: block %d expired %v", tr.Object, after, d.blockExpiry(after))
		}
	case scenario.LeaseCross:
		if d.leaseAt(client, tr.Object, before).GetState() != leasesv1.LeaseState_LEASE_STATE_ACTIVE ||
			d.leaseAt(client, tr.Object, after).GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED {
			t.Fatalf("%s at %d/%d: %s/%s", tr.Object, before, after, d.leaseAt(client, tr.Object, before).GetState(), d.leaseAt(client, tr.Object, after).GetState())
		}
		if !slices.Contains(d.blockExpiry(after), expiryItem{"lease", tr.Object}) {
			t.Fatalf("%s: block %d expired %v", tr.Object, after, d.blockExpiry(after))
		}
	}
}

// validatorUpdates are the validator updates FinalizeBlock returned at
// height.
func (r *rehearsalRun) validatorUpdates(height int64) []abci.ValidatorUpdate {
	r.n.t.Helper()
	h := height
	res, err := r.d.clients[0].BlockResults(context.Background(), &h)
	if err != nil {
		r.n.t.Fatal(err)
	}
	return res.ValidatorUpdates
}

// governedRow is the governed value the council set for key, read from
// the stored x/policy row at height, and whether one is set.
func (d *demo) governedRow(client *rpchttp.HTTP, key string, height int64) (uint64, bool) {
	d.t.Helper()
	raw := d.rawStore(client, app.ModulePolicy, policy.GovernedKey(key), height)
	if len(raw) == 0 {
		return 0, false
	}
	value, err := policy.DecodeU64(raw)
	if err != nil {
		d.t.Fatalf("governed row %s at %d: %v", key, height, err)
	}
	return value, true
}

// governedValue is the value in force of key in state (the governed value
// or the key's ceiling), or "" when state does not list the key.
func governedValue(state *policyv1.PolicyState, key string) string {
	for _, row := range state.GetGovernedLimits() {
		if row.GetLimitKey() == key {
			return strconv.FormatUint(row.GetValue(), 10)
		}
	}
	return ""
}

// pendingFor reports whether state holds a pending change of key at
// activation.
func pendingFor(state *policyv1.PolicyState, key string, activation uint64) bool {
	for _, row := range state.GetPendingLimitChanges() {
		if row.GetLimitKey() == key && row.GetActivationHeight() == activation {
			return true
		}
	}
	return false
}

// eventHeights are the heights in [from, to] whose FinalizeBlock events
// include an event of kind, read from node v1.
func (d *demo) eventHeights(kind string, from, to int64) []int64 {
	d.t.Helper()
	var out []int64
	for h := from; h <= to; h++ {
		height := h
		res, err := d.clients[0].BlockResults(context.Background(), &height)
		if err != nil {
			d.t.Fatalf("block results %d: %v", h, err)
		}
		if hasEvent(res.FinalizeBlockEvents, kind) {
			out = append(out, h)
		}
	}
	return out
}

// replayTwoPhase rebuilds the chain in process into a fresh goleveldb
// database (plan 12.5 step 14c): InitChain from the on-disk genesis, the
// v1 application (svnd's) for blocks 1..h-1, a close, then the v2
// application (B2's upgrade handlers; load gate row 3) for blocks h..to,
// every block as fetched from node v1. After each block the replayed app
// hash must equal the header app hash of the next block.
func replayTwoPhase(t *testing.T, d *demo, genesisPath string, h, to int64) {
	t.Helper()
	genDoc, err := cmttypes.GenesisDocFromFile(genesisPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	open := func(upgrades []upgrade.Upgrade) *app.App {
		db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, dir)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: genDoc.ChainID, Upgrades: upgrades})
		if err != nil {
			_ = db.Close()
			t.Fatalf("replay: open at %d: %v", to, err)
		}
		return replayed
	}
	replayed := open(nil)
	validators := make([]*cmttypes.Validator, len(genDoc.Validators))
	for i, val := range genDoc.Validators {
		validators[i] = cmttypes.NewValidator(val.PubKey, val.Power)
	}
	params := genDoc.ConsensusParams.ToProto()
	if _, err := replayed.InitChain(&abci.RequestInitChain{
		Time: genDoc.GenesisTime, ChainId: genDoc.ChainID, InitialHeight: genDoc.InitialHeight, ConsensusParams: &params,
		Validators: cmttypes.TM2PB.ValidatorUpdates(cmttypes.NewValidatorSet(validators)), AppStateBytes: genDoc.AppState,
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for height := int64(1); height <= to; height++ {
		if height == h {
			if err := replayed.Close(); err != nil {
				t.Fatal(err)
			}
			replayed = open(rehearsal.Upgrades())
		}
		hh := height
		block, err := d.clients[0].Block(ctx, &hh)
		if err != nil {
			t.Fatal(err)
		}
		b := block.Block
		if _, err := replayed.FinalizeBlock(&abci.RequestFinalizeBlock{
			Hash: b.Hash(), NextValidatorsHash: b.NextValidatorsHash, ProposerAddress: b.ProposerAddress,
			Height: b.Height, Time: b.Time, Misbehavior: b.Evidence.Evidence.ToABCI(), Txs: b.Txs.ToSliceOfBytes(),
		}); err != nil {
			t.Fatalf("replay height %d: %v", height, err)
		}
		if _, err := replayed.Commit(); err != nil {
			t.Fatal(err)
		}
		next := readHeight(t, d.clients[0], height+1)
		if got := strings.ToUpper(hex.EncodeToString(replayed.LastCommitID().Hash)); got != next.appHash {
			t.Fatalf("replay height %d: app hash %s, node %s", height, got, next.appHash)
		}
	}
	if err := replayed.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("phase=replay-two-phase v1_heights=1..%d v2_heights=%d..%d app_hashes_equal=true\n", h-1, h, to)
}

// checkTxCode runs CheckTx (the /check_tx route: the ante handler only, no
// mempool) of raw on client and returns its codespace and code.
func checkTxCode(t *testing.T, client *rpchttp.HTTP, raw []byte) (string, uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := client.CheckTx(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	return res.Codespace, res.Code
}

// waitRPC waits until client answers /status.
func waitRPC(t *testing.T, client *rpchttp.HTTP, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := client.Status(context.Background()); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("RPC did not answer within %s", timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestDevnetUpgradeRehearsal is the goal-10 upgrade rehearsal (plan 12.5)
// on a single-machine four-process devnet (not independent operators):
//
//  1. B1 and B2 are built from this tree; four validators run B1.
//  2. The representative scenario (chain/internal/rehearsal/scenario, plan
//     12.3) runs with real signed transactions; the council schedules
//     rehearsal-1 at h with h at least MinActivationLeadBlocks (PROPOSED)
//     after the execute block. Before h-1 validator v3 stops once, its home
//     is copied (copy B, the plan committed), and it restarts with B1.
//  3. Every node halts at h before writing: each log names UPGRADE
//     "rehearsal-1" NEEDED at height h, the application stays at h-1 while
//     the block store holds h, and the census and the representative
//     objects are read at h-1 from B1. All four stop; each home is copied
//     (copy A, its own copy at the halt).
//  4. B1 restarted on v1 is refused (load gate row 2) and changes nothing.
//     B2 starts on all four: the handshake replays h through B2,
//     the upgrade applies, the chain resumes with equal app hashes, header
//     Version.App 1 at h and 2 from h+1, ABCI app version 2, Query/Policy
//     app version 2 and no plan. Blocks h-1, h, h+1 hold no transaction.
//  5. Every representative object answers the same at h+1 as at h-1;
//     Query/Policy differs only in the upgrade fields. The census at h+1
//     is unchanged. The QUEUED proposal's execute is STALE_VERSION (owner
//     ruling Q2, "Council plan, fail closed (Recommended)"), and a
//     pre-upgrade business id replays idempotently. Every crossing
//     transition holds at effective-1 and effective; then the census is
//     scenario.Want(AfterTransitions).
//  6. B1 on v2 after the upgrade is refused (row 5), and B2 restarts there;
//     B2 on copy B is refused (row 4, too early); B2 on a fresh home is
//     refused at InitChain. Copies run under the copy rules (isolate).
//     Phase lines number the validators from 0 (node=0 is v1).
//  7. Restart of all four with B2: CheckTx refuses a normal account's
//     transaction until the first commit after the load (DR-10-41) and
//     accepts the same bytes after it. A two-phase in-process replay
//     reproduces every app hash; every committed event parses;
//     upgrade_applied appears once, at h; every node has the same block
//     hashes; the per-height table holds h+5, with Version.App 2.
//  8. The state check library on the stopped homes and copies, with
//     byte-identical reports on all four: copy A at h-1 under the v1
//     description (row 2, upgrade due) and the v2 description (row 3); the
//     nodes at h-1 under v1 equal to copy A (nothing rewrote history); the
//     nodes at h, at h+5, and at the tip under v2 (row 1); every invariant
//     passing;
//     the I13 digest equal at h-1 and h; the v1 description refuses the
//     upgraded state (row 5).
func TestDevnetUpgradeRehearsal(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	fmt.Printf("label=%s\n", upgradeLabel)
	testStarted := time.Now()
	bins := buildSvnd(t, buildB1, buildB2)
	b1, b2 := bins[buildB1], bins[buildB2]
	n := startUpgradeNet(t, devnet.Options{}, b1)
	d := n.d
	n.checkHistory("start")

	// 1-2. The scenario, the plan, and copy B.
	r := newRehearsalRun(n)
	r.run()
	h := r.h
	n.waitMempoolEmpty(30 * time.Second)
	if now := d.height(); now >= h-20 {
		t.Fatalf("the scenario ended at height %d, less than 20 blocks before h-1=%d", now, h-1)
	}
	n.stop("copy-b", 2)
	copyB := copyHome(t, n.nodeHome(2), "copy-b")
	copyBHeight := committedHeight(t, copyB)
	n.start(2, b1)
	fmt.Printf("phase=copy-b node=2 committed_height=%d h=%d restarted=B1\n", copyBHeight, h)

	// 3. The halt at h.
	n.waitLogs("halt", time.Duration(h-d.height()+60)*2*time.Second, n.all(), neededLine(h), "CONSENSUS FAILURE")
	if _, err := devnet.Stable(n.rpcs, 3*time.Second); err != nil {
		t.Fatal(tail(n.layout, err))
	}
	for i, client := range d.clients {
		last, _, version := abciInfo(t, client)
		status, err := client.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if last != h-1 || version != app.GenesisAppVersion || status.SyncInfo.LatestBlockHeight != h {
			t.Fatalf("node %d at the halt: application height %d app version %d, block store height %d; want %d, %d, %d",
				i, last, version, status.SyncInfo.LatestBlockHeight, h-1, app.GenesisAppVersion, h)
		}
		fmt.Printf("phase=halt node=%d application_height=%d block_store_height=%d abci_app_version=%d\n", i, last, status.SyncInfo.LatestBlockHeight, version)
	}
	haltWall := time.Now()
	r.checkCensus("h-1", h-1, scenario.BeforeUpgrade)
	beforeAnswers := r.objectAnswers(h - 1)
	n.checkHistory("halt")
	n.stop("halt", n.all()...)
	copies := make([]string, len(n.stops))
	for i := range copies {
		copies[i] = copyHome(t, n.nodeHome(i), fmt.Sprintf("copy-a-%d", i))
	}

	// 4. B1 refused on node 1; B2 on all four.
	refusedStart(t, "b1-after-halt", b1, n.nodeHome(0), fmt.Sprintf("load gate at height %d refused this binary (row 2): upgrade %s is due at height %d; install the binary for it (release digest %s)",
		h-1, rehearsal.UpgradeName, h, releaseHex()))
	for i := range n.stops {
		n.start(i, b2)
	}
	resumed, _, err := devnet.WaitEqual(n.rpcs, h+2, 120*time.Second)
	if err != nil {
		t.Fatal(tail(n.layout, err))
	}
	fmt.Printf("phase=resumed height=%d seconds_from_halt_to_h_plus_2=%.1f\n", resumed, time.Since(haltWall).Seconds())
	rows := n.printHeights("upgrade", h-1, h, h+1, h+2)
	if rows[h].versionApp != rehearsal.FromAppVersion || rows[h+1].versionApp != rehearsal.ToAppVersion || rows[h+2].versionApp != rehearsal.ToAppVersion {
		t.Fatalf("header Version.App at h, h+1, h+2: %d %d %d", rows[h].versionApp, rows[h+1].versionApp, rows[h+2].versionApp)
	}
	for _, height := range []int64{h - 1, h, h + 1} {
		if rows[height].txs != 0 {
			t.Fatalf("block %d holds %d transactions", height, rows[height].txs)
		}
	}
	if first := r.at(scenario.EvidenceCrossExpiry); rows[h+2].unix >= first {
		t.Fatalf("halt exceeded the margin: header time of h+2 is %d, the first crossing time is %d", rows[h+2].unix, first)
	}
	for i, client := range d.clients {
		if _, _, version := abciInfo(t, client); version != rehearsal.ToAppVersion {
			t.Fatalf("node %d ABCI app version %d after the upgrade", i, version)
		}
	}
	n.checkHistory("resumed")

	// 5. Pre/post comparison, census, STALE, business id replay.
	afterAnswers := r.objectAnswers(h + 1)
	for _, name := range slices.Sorted(maps.Keys(beforeAnswers)) {
		if !bytes.Equal(beforeAnswers[name], afterAnswers[name]) {
			t.Fatalf("%s answers differently at h+1 than at h-1", name)
		}
	}
	fmt.Printf("phase=compare-objects before=%d after=%d objects=%d equal=true\n", h-1, h+1, len(beforeAnswers))
	r.comparePolicy(h-1, h+1)
	r.checkCensus("h+1", h+1, scenario.AfterUpgrade)
	stale := d.send(d.authority, d.executeMsg(scenario.ProposalQueued))
	if failCode(stale.deliver) != msgkit.StaleVersion {
		t.Fatalf("the QUEUED proposal's execute after the upgrade: %s/%d %s", stale.deliver.Codespace, stale.deliver.Code, stale.deliver.Log)
	}
	n.waitHeight(stale.height, 30*time.Second)
	if got := d.proposalAt(d.clients[0], scenario.ProposalQueued, stale.height).GetState(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatalf("the QUEUED proposal is %s after its stale execute", got)
	}
	fmt.Printf("phase=queued-stale proposal=%s height=%d code=%s state=QUEUED\n", scenario.ProposalQueued, stale.height, msgkit.StaleVersion)
	replayed := d.send(d.seats[0], d.approveMsg(scenario.ProposalQueued, 1))
	if replayed.deliver.Code != 0 || !idempotentResult(replayed.deliver.Events) {
		t.Fatalf("pre-upgrade business id %s-ap-1: %s/%d events %v", scenario.ProposalQueued, replayed.deliver.Codespace, replayed.deliver.Code, replayed.deliver.Events)
	}
	fmt.Printf("phase=business-id-replay business_id=%s-ap-1 height=%d code=0 idempotent=true\n", scenario.ProposalQueued, replayed.height)
	approveHeight := d.commit("open-approve-3", d.seats[2], d.approveMsg(scenario.ProposalOpen, 3))

	// 6. Refusals while the crossing times approach: B1 on node 2 after the
	// upgrade (row 5), B2 on copy B (row 4), B2 on a fresh home (InitChain).
	n.stop("b1-after-upgrade", 1)
	refusedStart(t, "b1-after-upgrade", b1, n.nodeHome(1), fmt.Sprintf("refused this binary (row 5): state is newer than this binary (app version %d, binary %d)",
		rehearsal.ToAppVersion, app.GenesisAppVersion))
	n.start(1, b2)
	isolate(t, copyB, 0)
	refusedStart(t, "b2-too-early", b2, copyB, fmt.Sprintf("refused this binary (row 4): too early: run the previous binary until height %d (upgrade %s applies at height %d)",
		h-1, rehearsal.UpgradeName, h))
	fresh := filepath.Join(t.TempDir(), "fresh")
	for _, dir := range []string{"config", "data"} {
		if err := os.MkdirAll(filepath.Join(fresh, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"genesis.json", "node.json", "svn-listen.json"} {
		raw, err := os.ReadFile(filepath.Join(n.nodeHome(0), "config", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fresh, "config", name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	isolate(t, fresh, 1)
	refusedStart(t, "b2-fresh-home", b2, fresh, fmt.Sprintf("this binary runs app version %d, and a chain starts at app version %d", rehearsal.ToAppVersion, app.GenesisAppVersion))

	// 5 (continued). The crossing transitions and the final census.
	r.transitions(approveHeight)
	tip := d.height()
	n.waitHeight(tip, 60*time.Second)
	r.checkCensus("after-transitions", tip, scenario.AfterTransitions)

	// 7. Restart all four; DR-10-41.
	n.stop("restart", n.all()...)
	n.start(0, b2)
	waitRPC(t, d.clients[0], 60*time.Second)
	last, _, _ := abciInfo(t, d.clients[0])
	// The probe needs a signer whose account number is not 0: before the
	// first commit the check state verifies signatures with account number
	// 0, so a signer numbered 0 would pass. Account numbers follow address
	// order and devnet keys are new on every run, so either service account
	// may be numbered 0; at most one of them is.
	who := d.service
	if d.numbers[who.addr] == 0 {
		who = d.service2
	}
	if d.numbers[who.addr] == 0 {
		t.Fatal("both service accounts have account number 0")
	}
	probe := signDevnet(t, d.enc, d.chainID, who.key, d.numbers[who.addr], d.seq[who.addr],
		d.registerContent(who, "bid-dr-10-41", "c-dr-10-41", 1, sizeOne))
	space, code := checkTxCode(t, d.clients[0], probe)
	if space != sdkerrors.RootCodespace || code != sdkerrors.ErrUnauthorized.ABCICode() {
		t.Fatalf("CheckTx before the first commit after the load: %s/%d, want %s/%d", space, code, sdkerrors.RootCodespace, sdkerrors.ErrUnauthorized.ABCICode())
	}
	if again, _, _ := abciInfo(t, d.clients[0]); again != last {
		t.Fatalf("node 0 alone committed height %d", again)
	}
	fmt.Printf("phase=dr-10-41-before-commit node=0 application_height=%d check_tx=%s/%d (refused; account number %d)\n", last, space, code, d.numbers[who.addr])
	for i := 1; i < len(n.stops); i++ {
		n.start(i, b2)
	}
	up, _, err := devnet.WaitEqual(n.rpcs, last+1, 90*time.Second)
	if err != nil {
		t.Fatal(tail(n.layout, err))
	}
	if space, code := checkTxCode(t, d.clients[0], probe); code != 0 {
		t.Fatalf("CheckTx after the first commit: %s/%d", space, code)
	}
	fmt.Printf("phase=dr-10-41-after-commit node=0 height=%d check_tx=0\n", up)
	if _, _, err := devnet.WaitEqual(n.rpcs, up+2, 60*time.Second); err != nil {
		t.Fatal(tail(n.layout, err))
	}
	probeRes, err := d.broadcast(probe)
	if err != nil || probeRes.check.Code != 0 || probeRes.deliver.Code != 0 {
		t.Fatalf("the probe transaction after the commit: %v %+v", err, probeRes)
	}
	d.seq[who.addr]++
	fmt.Printf("phase=dr-10-41-committed height=%d code=0 txhash=%X\n", probeRes.height, probeRes.hash)
	n.checkHistory("restart")
	if row := n.printHeights("h-plus-5", h+5)[h+5]; row.versionApp != rehearsal.ToAppVersion {
		t.Fatalf("header Version.App at h+5: %d", row.versionApp)
	}

	// 7 (continued). Replay, events, block hashes.
	final := d.height()
	if _, _, err := devnet.WaitEqual(n.rpcs, final+1, 60*time.Second); err != nil {
		t.Fatal(tail(n.layout, err))
	}
	replayTwoPhase(t, d, n.layout.Genesis, h, final)
	d.scanEvents(final)
	if got := d.eventHeights(upgrade.EventUpgradeApplied, 1, final); len(got) != 1 || got[0] != h {
		t.Fatalf("upgrade_applied at heights %v, want only %d", got, h)
	}
	for height := int64(1); height <= final; height++ {
		want := readHeight(t, d.clients[0], height).blockHash
		for i := 1; i < len(d.clients); i++ {
			if got := readHeight(t, d.clients[i], height).blockHash; got != want {
				t.Fatalf("node %d block %d hash %s, node 0 %s", i, height, got, want)
			}
		}
	}
	fmt.Printf("phase=block-hashes heights=1..%d nodes=4 equal=true upgrade_applied_height=%d\n", final, h)

	// 8. The state check library on the stopped homes and copies.
	n.stop("final", n.all()...)
	v2 := rehearsal.Upgrades()
	homes := make([]string, len(n.stops))
	for i := range homes {
		homes[i] = n.nodeHome(i)
	}
	haltV1 := sameReports(t, "statecheck-copy-a-v1", copies, 0, nil)
	requirePassed(t, "copy A under v1", haltV1, 2)
	if haltV1.Height != h-1 {
		t.Fatalf("copy A holds height %d, want h-1=%d", haltV1.Height, h-1)
	}
	requirePassed(t, "copy A under v2", sameReports(t, "statecheck-copy-a-v2", copies, 0, v2), 3)
	historyV1 := sameReports(t, "statecheck-nodes-h-1-v1", homes, h-1, nil)
	if storeText(historyV1) != storeText(haltV1) || !bytes.Equal(historyV1.AppHash, haltV1.AppHash) {
		t.Fatal("the nodes' committed state at h-1 differs from copy A")
	}
	atH := sameReports(t, "statecheck-nodes-h-v2", homes, h, v2)
	requirePassed(t, "the nodes at h under v2", atH, 1)
	if atH.I13 != haltV1.I13 {
		t.Fatalf("I13 digest at h-1 %x, at h %x", haltV1.I13, atH.I13)
	}
	requirePassed(t, "the nodes at h+5 under v2", sameReports(t, "statecheck-nodes-h-plus-5-v2", homes, h+5, v2), 1)
	requirePassed(t, "the nodes at the tip under v2", sameReports(t, "statecheck-nodes-tip-v2", homes, final, v2), 1)
	refused := sameReports(t, "statecheck-nodes-tip-v1", homes, final, nil)
	if refused.Gate.Row != 5 || refused.Passed() {
		t.Fatalf("the v1 description on the upgraded state: row %d passed %t", refused.Gate.Row, refused.Passed())
	}
	fmt.Printf("phase=statecheck homes=4 copy_a_h_minus_1=row2(v1),row3(v2) nodes_h_minus_1_equal_copy_a=true h=row1(v2) h_plus_5=row1(v2) tip=%d row1(v2),row5(v1) i13_h_minus_1_equals_h=%x\n", final, atH.I13)
	fmt.Printf("phase=rehearsal-done h=%d tip=%d wall_seconds=%.1f label=%q\n", h, final, time.Since(testStarted).Seconds(), upgradeLabel)
}

// committedHeight is the last committed application height of a stopped
// home or a copy (rootmulti.GetLatestVersion of data/application.db).
func committedHeight(t *testing.T, home string) int64 {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(home, "data"))
	if err != nil {
		t.Fatal(err)
	}
	height := rootmulti.GetLatestVersion(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return height
}

// logHas waits up to timeout until node.log of validator i contains want
// and reports whether it does; it prints the matching line.
func (n *upgradeNet) logHas(label string, timeout time.Duration, i int, want string) bool {
	n.t.Helper()
	return n.logAny(label, timeout, i, want) != ""
}

// logAny waits up to timeout until node.log of validator i contains one
// of alternatives, prints the matching line, and returns the alternative
// found, or "" when none appeared.
func (n *upgradeNet) logAny(label string, timeout time.Duration, i int, alternatives ...string) string {
	n.t.Helper()
	path := filepath.Join(n.nodeHome(i), "node.log")
	deadline := time.Now().Add(timeout)
	for {
		raw, _ := os.ReadFile(path)
		for _, want := range alternatives {
			if bytes.Contains(raw, []byte(want)) {
				fmt.Printf("phase=%s-log node=%d line=%q\n", label, i, matchingLine(raw, want))
				return want
			}
		}
		if time.Now().After(deadline) {
			fmt.Printf("phase=%s-log node=%d absent=%q within_seconds=%.0f\n", label, i, alternatives, timeout.Seconds())
			return ""
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// signingState is the height and round of a priv_validator_state.json.
func signingState(t *testing.T, path string) (int64, int32) {
	t.Helper()
	var state struct {
		Height string `json:"height"`
		Round  int32  `json:"round"`
	}
	readJSONFile(t, path, &state)
	height, err := strconv.ParseInt(state.Height, 10, 64)
	if err != nil {
		t.Fatalf("%s: height %q", path, state.Height)
	}
	return height, state.Round
}

// TestDevnetMinorityWrongMigration is case I6 of plan 11.6 on a
// single-machine four-process devnet (not independent operators): four
// validators run B1 to a council plan for rehearsal-1 and halt at h; each
// home is copied at the halt (its own copy). Three validators restart with
// B2 and one with B2-bad. B2-bad's handler writes one documented extra row
// (rehearsal.BadRowKey in the x/leases store, outside every check of the
// upgrade runner), so that node commits another app hash at h. The three
// B2 nodes hold more than two thirds of the power and continue; the B2-bad
// node refuses their block h+1 (wrong Block.Header.AppHash), then halts
// with a CometBFT consensus failure, "+2/3 prevoted for an invalid block"
// or "+2/3 committed an invalid block" (which one depends on whether it
// sees the block's prevotes or its precommits first), and stays at h; its
// state at h fails the state check (the leases invariants refuse the
// extra row). Recovery as row I6 allows it, after that log line: stop the
// node, restore data/{application.db,blockstore.db,state.db,evidence.db,
// cs.wal} from its own copy taken at the halt, keep its newest
// priv_validator_state.json (byte-equal to the file before the restore,
// height not lower than the copy's), and start B2: the handshake replays h
// with B2 and the node catches up with equal app hashes. There is never a
// chain-wide rollback: block hashes 1..h are unchanged, genesis is
// unchanged, and history starts at height 1.
func TestDevnetMinorityWrongMigration(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	fmt.Printf("label=%s\n", upgradeLabel)
	testStarted := time.Now()
	bins := buildSvnd(t, buildB1, buildB2, buildB2Bad)
	b2, bad := bins[buildB2], bins[buildB2Bad]
	n := startUpgradeNet(t, devnet.Options{}, bins[buildB1])
	d := n.d
	client := d.clients[0]

	// The plan: h keeps the minimum lead plus 30 blocks for the propose,
	// the three approvals, and the execute.
	h := d.height() + int64(msgkit.MinActivationLeadBlocks) + 30
	d.propose("minority-plan", "plan-minority", "upgrade", planPayload(h), d.chainTime()+24*3600)
	d.approve("minority-plan", "plan-minority", 3)
	exec := d.execute("minority-plan", "plan-minority").height
	if h-exec < int64(msgkit.MinActivationLeadBlocks) {
		t.Fatalf("plan height %d is %d blocks after the execute at %d", h, h-exec, exec)
	}
	if plan := d.policyAt(client, exec).GetUpgradePlan(); plan.GetName() != rehearsal.UpgradeName || plan.GetHeight() != uint64(h) {
		t.Fatalf("Query/Policy plan at %d: %v", exec, plan)
	}
	fmt.Printf("phase=minority-plan execute_height=%d h=%d lead=%d min_lead=%d (PROPOSED)\n", exec, h, h-exec, msgkit.MinActivationLeadBlocks)
	n.waitMempoolEmpty(30 * time.Second)
	n.waitLogs("minority-halt", time.Duration(h-d.height()+60)*2*time.Second, n.all(), neededLine(h), "CONSENSUS FAILURE")
	for i, c := range d.clients {
		if last, _, _ := abciInfo(t, c); last != h-1 {
			t.Fatalf("node %d application height %d at the halt, want %d", i, last, h-1)
		}
	}
	hashes := map[int64]string{}
	for height := int64(1); height <= h; height++ {
		hashes[height] = readHeight(t, client, height).blockHash
	}
	n.stop("minority-halt", n.all()...)
	copies := make([]string, len(n.stops))
	for i := range copies {
		copies[i] = copyHome(t, n.nodeHome(i), fmt.Sprintf("copy-a-%d", i))
	}

	// B2-bad on node 3 and B2 on nodes 0-2. B2-bad starts first, so no
	// peer is ahead of it when its consensus begins at h+1.
	const minority = 3
	n.start(minority, bad)
	for i := 0; i < minority; i++ {
		n.start(i, b2)
	}
	majority := n.rpcs[:minority]
	if _, _, err := devnet.WaitEqual(majority, h+3, 120*time.Second); err != nil {
		t.Fatal(tail(n.layout, err))
	}
	// The B2-bad node refuses the majority's block h+1 (its header app
	// hash is not the node's own state after h) and prevotes nil. When it
	// sees more than two thirds of the power vote for that block, CometBFT
	// stops its consensus: "+2/3 prevoted for an invalid block" in the
	// precommit step if the prevotes come first, "+2/3 committed an invalid
	// block" when it finalizes the commit if the precommits come first.
	if !n.logHas("minority-prevote", 60*time.Second, minority, "wrong Block.Header.AppHash") {
		t.Fatal(tail(n.layout, errors.New("the B2-bad node did not refuse the majority's block")))
	}
	matched := n.logAny("minority-failure", 60*time.Second, minority, "+2/3 prevoted for an invalid block", "+2/3 committed an invalid block")
	if matched == "" {
		t.Fatal(tail(n.layout, errors.New("the B2-bad node logged no CometBFT invalid-block consensus failure")))
	}
	majorityHash := readHeight(t, client, h+1).appHash
	last, badHash, _ := abciInfo(t, d.clients[minority])
	badHex := strings.ToUpper(hex.EncodeToString(badHash))
	status, err := d.clients[minority].Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if last != h || badHex == majorityHash || status.SyncInfo.LatestBlockHeight != h {
		t.Fatalf("B2-bad node: application height %d app hash %s block store height %d; want height %d, an app hash other than %s",
			last, badHex, status.SyncInfo.LatestBlockHeight, h, majorityHash)
	}
	if got := d.rawStore(d.clients[minority], app.ModuleLeases, rehearsal.BadRowKey, h); !bytes.Equal(got, []byte{rehearsal.BadRowValue}) {
		t.Fatalf("B2-bad node: leases row %x at h is %x", rehearsal.BadRowKey, got)
	}
	if got := d.rawStore(client, app.ModuleLeases, rehearsal.BadRowKey, h); len(got) != 0 {
		t.Fatalf("B2 node: leases row %x at h is %x", rehearsal.BadRowKey, got)
	}
	ahead, _, err := devnet.WaitEqual(majority, h+5, 60*time.Second)
	if err != nil {
		t.Fatal(tail(n.layout, err))
	}
	if again, _, _ := abciInfo(t, d.clients[minority]); again != h {
		t.Fatalf("B2-bad node moved to height %d", again)
	}
	fmt.Printf("phase=minority-diverged node=%d outcome=%q application_height=%d app_hash=%s majority_app_hash=%s majority_height=%d bad_row=%x->%x\n",
		minority, matched, last, badHex, majorityHash, ahead, rehearsal.BadRowKey, rehearsal.BadRowValue)

	// Recovery of the minority node from its own copy at the halt.
	n.stop("minority", minority)
	home := n.nodeHome(minority)
	report := checkReport(t, "statecheck-minority-before-restore", home, h, rehearsal.Upgrades())
	leasesFailed := false
	for _, check := range report.Checks {
		if check.Name == statecheck.CheckLeases && check.Err != nil {
			leasesFailed = true
		}
	}
	if report.Passed() || !leasesFailed {
		t.Fatalf("the B2-bad state at h passed the state check (leases failed %t)", leasesFailed)
	}
	statePath := filepath.Join(home, "data", "priv_validator_state.json")
	signedBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"application.db", "blockstore.db", "state.db", "evidence.db", "cs.wal"} {
		target := filepath.Join(home, "data", name)
		if err := os.RemoveAll(target); err != nil {
			t.Fatal(err)
		}
		copyTree(t, filepath.Join(copies[minority], "data", name), target)
	}
	signedAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	keptHeight, keptRound := signingState(t, statePath)
	copyHeight, copyRound := signingState(t, filepath.Join(copies[minority], "data", "priv_validator_state.json"))
	if !bytes.Equal(signedBefore, signedAfter) || keptHeight < copyHeight || (keptHeight == copyHeight && keptRound < copyRound) {
		t.Fatalf("signing state after the restore: height %d round %d (copy at the halt %d/%d), unchanged %t",
			keptHeight, keptRound, copyHeight, copyRound, bytes.Equal(signedBefore, signedAfter))
	}
	fmt.Printf("phase=minority-restore node=%d restored=application.db,blockstore.db,state.db,evidence.db,cs.wal from=own-copy-at-halt kept_signing_state_height=%d round=%d copy_signing_state_height=%d round=%d\n",
		minority, keptHeight, keptRound, copyHeight, copyRound)
	n.start(minority, b2)
	caught, hash, err := devnet.WaitEqual(n.rpcs, ahead+2, 120*time.Second)
	if err != nil {
		t.Fatal(tail(n.layout, err))
	}
	if _, _, version := abciInfo(t, d.clients[minority]); version != rehearsal.ToAppVersion {
		t.Fatalf("recovered node app version %d", version)
	}
	if got := d.rawStore(d.clients[minority], app.ModuleLeases, rehearsal.BadRowKey, h); len(got) != 0 {
		t.Fatalf("recovered node: leases row %x at h is %x", rehearsal.BadRowKey, got)
	}
	if got := readHeight(t, d.clients[minority], h+1).appHash; got != majorityHash {
		t.Fatalf("recovered node: app hash after h %s, majority %s", got, majorityHash)
	}
	for height := int64(1); height <= h; height++ {
		for i, c := range d.clients {
			if got := readHeight(t, c, height).blockHash; got != hashes[height] {
				t.Fatalf("node %d block %d hash %s, before the swap %s", i, height, got, hashes[height])
			}
		}
	}
	n.checkHistory("minority-recovered")
	fmt.Printf("phase=minority-recovered node=%d height=%d app_hash=%s nodes=4 equal=true block_hashes_1_to_h_unchanged=true\n", minority, caught, hash)
	n.stop("minority-final", n.all()...)
	homes := make([]string, len(n.stops))
	for i := range homes {
		homes[i] = n.nodeHome(i)
	}
	requirePassed(t, "after recovery", sameReports(t, "statecheck-minority-recovered", homes, caught, rehearsal.Upgrades()), 1)
	fmt.Printf("phase=minority-done h=%d recovered_height=%d reports_byte_identical=true wall_seconds=%.1f label=%q\n", h, caught, time.Since(testStarted).Seconds(), upgradeLabel)
}

// timingDelay is the council review delay of TestDevnetGovernedChangeTiming
// in seconds: devnet.Options.CouncilDelaySeconds, the DEC-008 devnet test
// parameter labelled PROPOSED_TEST_PARAMETER, a test value and not an
// approved DEC-008 value.
const timingDelay = 6

// TestDevnetGovernedChangeTiming shows the council review delay and a
// scheduled governed change on a single-machine four-process devnet (not
// independent operators) with a nonzero DEC-008 devnet delay. A live
// devnet cannot place a transaction at an exact time or height, so the
// test submits executes one after another and asserts by each committed
// height and header time:
//
//   - a proposal for a scheduled max_open_offers change reaches three
//     approvals; Query/Governance shows it QUEUED with execute_after = the
//     header time of the third approval plus the delay;
//   - every execute committed at a header time below execute_after is
//     OUT_OF_RANGE and changes nothing; the first one at or after it
//     applies. An attempt counts when one rejected execute committed at
//     exactly execute_after-1 and the applied one at exactly
//     execute_after; otherwise the attempt is logged and a new proposal
//     tries again (at most three attempts);
//   - the scheduled change of the counted attempt applies at its
//     activation height a and not at a-1: at a-1 Query/Policy shows the
//     change pending and another value in force, and the stored governed
//     row does not hold the value; at a the value is in force and stored,
//     and no longer pending; block a, and not a-1, emits policy_activated.
func TestDevnetGovernedChangeTiming(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	fmt.Printf("label=%s\n", upgradeLabel)
	testStarted := time.Now()
	bins := buildSvnd(t, buildB1)
	n := startUpgradeNet(t, devnet.Options{CouncilDelaySeconds: timingDelay}, bins[buildB1])
	d := n.d
	client := d.clients[0]
	from := d.height()
	measured, _, err := devnet.WaitEqual(n.rpcs, from+10, 60*time.Second)
	if err != nil {
		t.Fatal(tail(n.layout, err))
	}
	perBlock := float64(d.blockTime(measured)-d.blockTime(from)) / float64(measured-from)
	key := msgkit.LimitMaxOpenOffers
	var activation int64
	var value uint64
	for attempt := 1; ; attempt++ {
		id := fmt.Sprintf("timing-%d", attempt)
		value = uint64(10 + attempt)
		proposed := d.height()
		activation = proposed + int64(math.Ceil(float64(timingDelay+30)/math.Max(perBlock, 0.05))) + 20
		payload := &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{GovernedLimit: &authorityv1.GovernedLimitChange{
			LimitKey: key, Value: value, ActivationHeight: uint64(activation),
		}}}
		d.propose(id, id, key, payload, d.chainTime()+24*3600)
		threshold := d.approve(id, id, 3)
		n.waitHeight(threshold, 30*time.Second)
		queued := d.proposalAt(client, id, threshold)
		after := queued.GetExecuteAfterUnixSeconds()
		if queued.GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED || after != d.blockTime(threshold)+timingDelay {
			t.Fatalf("%s at %d: %s execute_after %d, threshold header time %d + delay %d", id, threshold, queued.GetState(), after, d.blockTime(threshold), timingDelay)
		}
		fmt.Printf("phase=timing-queued attempt=%d proposal=%s threshold_height=%d header_time=%d delay=%d execute_after=%d activation=%d\n",
			attempt, id, threshold, d.blockTime(threshold), timingDelay, after, activation)
		d.waitChainTime(after-3, 60*time.Second)
		var rejectedAt, appliedAt int64 = math.MinInt64, 0
		for appliedAt == 0 {
			res := d.send(d.authority, d.executeMsg(id))
			at := d.blockTime(res.height)
			switch {
			case failCode(res.deliver) == msgkit.OutOfRange && at < after:
				rejectedAt = at
				fmt.Printf("phase=timing-execute attempt=%d height=%d header_time=%d execute_after=%d code=%s\n", attempt, res.height, at, after, msgkit.OutOfRange)
			case res.deliver.Code == 0 && authorityResult(res.deliver.Events) == "" && at >= after:
				appliedAt = at
				d.policy++
				fmt.Printf("phase=timing-execute attempt=%d height=%d header_time=%d execute_after=%d code=0 applied=true\n", attempt, res.height, at, after)
			default:
				t.Fatalf("execute of %s at header time %d (execute_after %d): %s/%d result %q", id, at, after, res.deliver.Codespace, res.deliver.Code, authorityResult(res.deliver.Events))
			}
			if appliedAt == 0 && at > after+5 {
				t.Fatalf("no execute of %s applied by header time %d", id, at)
			}
		}
		exact := rejectedAt == after-1 && appliedAt == after
		fmt.Printf("phase=timing-attempt attempt=%d last_rejected_header_time=%d applied_header_time=%d execute_after=%d exact=%t\n", attempt, rejectedAt, appliedAt, after, exact)
		if exact {
			break
		}
		if attempt == 3 {
			t.Fatal("three attempts did not observe execute_after-1 (OUT_OF_RANGE) and execute_after (applied) at exact header times")
		}
	}
	n.waitHeight(activation, time.Duration(float64(activation-d.height()+60)*math.Max(perBlock, 0.05)*3)*time.Second+60*time.Second)
	pre, post := d.policyAt(client, activation-1), d.policyAt(client, activation)
	want := strconv.FormatUint(value, 10)
	preRow, preSet := d.governedRow(client, key, activation-1)
	postRow, postSet := d.governedRow(client, key, activation)
	if (preSet && preRow == value) || governedValue(pre, key) == want || !pendingFor(pre, key, uint64(activation)) {
		t.Fatalf("%s at a-1=%d: governed row %d set %t, in force %q, pending %v", key, activation-1, preRow, preSet, governedValue(pre, key), pre.GetPendingLimitChanges())
	}
	if !postSet || postRow != value || governedValue(post, key) != want || pendingFor(post, key, uint64(activation)) {
		t.Fatalf("%s at a=%d: governed row %d set %t, in force %q, pending %v", key, activation, postRow, postSet, governedValue(post, key), post.GetPendingLimitChanges())
	}
	if got := d.eventHeights(policy.EventPolicyActivated, activation-1, activation); len(got) != 1 || got[0] != activation {
		t.Fatalf("%s at heights %v, want only %d", policy.EventPolicyActivated, got, activation)
	}
	fmt.Printf("phase=timing-activation key=%s value=%s a_minus_1=%d governed_row_set=%t in_force=%q pending=true a=%d governed_row=%d in_force=%q pending=false policy_activated_height=%d\n",
		key, want, activation-1, preSet, governedValue(pre, key), activation, postRow, governedValue(post, key), activation)
	n.checkHistory("timing")
	fmt.Printf("phase=timing-done wall_seconds=%.1f label=%q\n", time.Since(testStarted).Seconds(), upgradeLabel)
}
