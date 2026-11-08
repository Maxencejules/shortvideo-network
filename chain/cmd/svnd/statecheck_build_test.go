package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/gogoproto/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
	"github.com/Maxencejules/shortvideo-network/chain/internal/statecheck"
	"github.com/Maxencejules/shortvideo-network/chain/internal/statecheck/testhome"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// The statecheck builds: the build tag svn_statecheck alone (svnd's binary
// description, no upgrade handler) and with svn_rehearsal (the rehearsal
// binary B2's description).
const (
	tagStatecheck          = "svn_statecheck"
	tagStatecheckRehearsal = "svn_statecheck,svn_rehearsal"
)

// runSplit runs bin with args and returns stdout, stderr, and the exit code.
func runSplit(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), stderr.String(), 0
	case errors.As(err, &exit):
		return stdout.String(), stderr.String(), exit.ExitCode()
	}
	t.Fatalf("%s %v: %v", bin, args, err)
	return "", "", 0
}

// reportText is the state check report of home for a binary with upgrades,
// as the library writes it.
func reportText(t *testing.T, home string, upgrades []upgrade.Upgrade) string {
	t.Helper()
	binary, err := app.DeclaredBinary(upgrades)
	if err != nil {
		t.Fatal(err)
	}
	report, err := statecheck.Run(statecheck.Options{Home: home, Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := report.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestStatecheckBuildsReportOnAHome builds svnd with the build tag
// svn_statecheck, alone and with svn_rehearsal, and runs `svnd statecheck`
// on a stopped test home whose rehearsal-1 plan is due in the next block:
//
//   - the help of both builds lists statecheck, and go version -m records
//     the tags;
//   - the svn_statecheck build reports for svnd (no upgrade handler): gate
//     row 2, result PASS, exit 0; the rehearsal build reports for B2
//     (rehearsal-1, app version 1 to 2): gate row 3, result PASS, exit 0;
//     each output equals the library's report for that binary;
//   - while the home's node holds the database, both exit 1 with the lock
//     message and print no report;
//   - on a home whose consensus Version.App differs from the stored app
//     version, both exit 1 after a report that ends "result FAIL".
//
// The default build has no statecheck command
// (TestDefaultBuildCompilesNoRehearsal).
func TestStatecheckBuildsReportOnAHome(t *testing.T) {
	h := testhome.New(t, filepath.Join(t.TempDir(), "devnet"))
	h.Block(t)
	plan := h.Height() + 5 + int64(msgkit.MinActivationLeadBlocks) + 2
	h.Council(t, "p-upgrade", testhome.UpgradeParameterKey, testhome.SoftwareUpgrade(rehearsal.UpgradeName, uint64(plan)))
	h.BlocksTo(t, plan-1)
	h.Stop(t)

	builds := []struct {
		tag      string
		upgrades []upgrade.Upgrade
		lines    []string
	}{
		{tagStatecheck, nil, []string{"binary upgrades none\n", "gate row 2 refused: upgrade rehearsal-1 is due at height"}},
		{tagStatecheckRehearsal, rehearsal.Upgrades(), []string{"binary upgrade rehearsal-1 1 2\n", "gate row 3 start in the upgrade window"}},
	}
	for _, b := range builds {
		bin := svndBuild(t, b.tag)
		if help, _, code := runSplit(t, bin, "--help"); code != 0 || !strings.Contains(help, "statecheck") {
			t.Fatalf("%s help (exit %d):\n%s", b.tag, code, help)
		}
		if tags := readBuildInfoLines(t, bin).settings["-tags"]; tags != b.tag {
			t.Fatalf("%s: go version -m records -tags=%q", b.tag, tags)
		}
		out, stderr, code := runSplit(t, bin, "statecheck", "--home", h.Dir)
		if code != 0 || stderr != "" || out != reportText(t, h.Dir, b.upgrades) || !strings.HasSuffix(out, "result PASS\n") {
			t.Fatalf("%s statecheck (exit %d, stderr %q):\n%s", b.tag, code, stderr, out)
		}
		for _, line := range b.lines {
			if !strings.Contains(out, line) {
				t.Fatalf("%s statecheck has no %q:\n%s", b.tag, line, out)
			}
		}
	}

	// B2 starts in the upgrade window (row 3) and holds the database.
	h.StartWith(t, rehearsal.Upgrades())
	for _, b := range builds {
		out, stderr, code := runSplit(t, svndBuild(t, b.tag), "statecheck", "--home", h.Dir)
		if code != 1 || out != "" || !strings.Contains(stderr, "locked by another process (a running node holds it)") {
			t.Fatalf("%s statecheck of a held home (exit %d, stderr %q):\n%s", b.tag, code, stderr, out)
		}
	}
	h.Stop(t)

	var params cmtproto.ConsensusParams
	stored := h.Row(t, app.ModuleSVN, []byte{0x03})
	if err := proto.Unmarshal(stored, &params); err != nil {
		t.Fatal(err)
	}
	params.Version.App = 2
	damaged, err := proto.Marshal(&params)
	if err != nil {
		t.Fatal(err)
	}
	h.Damage(t, app.ModuleSVN, []byte{0x03}, damaged)
	for _, b := range builds {
		out, stderr, code := runSplit(t, svndBuild(t, b.tag), "statecheck", "--home", h.Dir)
		if code != 1 || !strings.HasSuffix(out, "check versions FAIL I5: stored app version 1, stored consensus Version.App 2\nresult FAIL\n") || stderr != "state check failed\n" {
			t.Fatalf("%s statecheck of a damaged home (exit %d, stderr %q):\n%s", b.tag, code, stderr, out)
		}
	}

}
