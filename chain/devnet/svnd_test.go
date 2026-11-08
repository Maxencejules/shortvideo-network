package devnet_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
)

// TestDevnetRunsBuiltSvnd builds the production svnd from this tree
// (`go build ./cmd/svnd`) and starts the four validators of a new devnet
// with it through launchBinary; the other devnet tests run the test binary.
// The four nodes reach an equal height of at least 3, ABCI Info reports
// app version 1 (app.GenesisAppVersion, set at load before InitChain and
// after every Commit) on every node, the header of blocks 1 to 3 carries
// Version.App 1, `svnd version` prints app_version 1, and every node stops
// cleanly on SIGTERM.
func TestDevnetRunsBuiltSvnd(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	bin := filepath.Join(t.TempDir(), "svnd")
	build := exec.Command("go", "build", "-o", bin, "../cmd/svnd")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build svnd: %v\n%s", err, out)
	}
	version, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), fmt.Sprintf("\napp_version %d\n", app.GenesisAppVersion)) {
		t.Fatalf("svnd version: %v\n%s", err, version)
	}
	layout, err := devnet.Init(devnet.Options{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	stops := make([]func() error, len(layout.Topology.Validators))
	t.Cleanup(func() {
		for _, stop := range stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	for i, node := range layout.Topology.Validators {
		stop, err := launchBinary(bin, node.Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[i] = stop
	}
	rpcs := rpcList(layout)
	height, _, err := devnet.WaitEqual(rpcs, 3, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	logPhase(t, "svnd-binary", rpcs)
	for i, rpc := range rpcs {
		var info struct {
			Result struct {
				Response struct {
					AppVersion string `json:"app_version"`
				} `json:"response"`
			} `json:"result"`
		}
		if err := rpcJSON(rpc+"/abci_info", &info); err != nil {
			t.Fatal(err)
		}
		if info.Result.Response.AppVersion != fmt.Sprint(app.GenesisAppVersion) {
			t.Fatalf("node %d abci_info app_version %q", i, info.Result.Response.AppVersion)
		}
		for h := int64(1); h <= 3; h++ {
			var block struct {
				Result struct {
					Block struct {
						Header struct {
							Version struct {
								App string `json:"app"`
							} `json:"version"`
						} `json:"header"`
					} `json:"block"`
				} `json:"result"`
			}
			if err := rpcJSON(fmt.Sprintf("%s/block?height=%d", rpc, h), &block); err != nil {
				t.Fatal(err)
			}
			if got := block.Result.Block.Header.Version.App; got != fmt.Sprint(app.GenesisAppVersion) {
				t.Fatalf("node %d block %d header Version.App %q", i, h, got)
			}
		}
	}
	for i, stop := range stops {
		if err := stop(); err != nil {
			t.Fatalf("stop node %d: %v", i, err)
		}
		stops[i] = nil
	}
	fmt.Printf("phase=svnd-binary-stopped height=%d\n", height)
}

// rpcJSON decodes the JSON body of one CometBFT RPC GET into out.
func rpcJSON(url string, out any) error {
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d: %s", url, response.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}
