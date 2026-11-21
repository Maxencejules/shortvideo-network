//go:build svn_capacity

package app_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
)

// TestCapacityFixture records one committed registration on the in-process
// application. It is not a CometBFT network and not a throughput claim.
// CI does not pass -tags svn_capacity.
func TestCapacityFixture(t *testing.T) {
	net := newRegNet()
	cluster := net.start(t, 2)
	before := storeBytes(t, cluster.app())
	msg := net.registerContent(t, net.service, "reg-c1", "c1", 1, packageManifest("c1"), 4_000_000)
	raw := net.signed(t, cluster, net.service, registryGas, nil, msg)
	started := time.Now()
	res := net.deliverRaw(t, cluster, net.service, raw)
	commitWall := time.Since(started)
	if res.Code != 0 {
		t.Fatalf("register code %s/%d %s", res.Codespace, res.Code, res.Log)
	}
	after := storeBytes(t, cluster.app())
	dup := cluster.block(t, raw)
	if len(dup.TxResults) != 1 || dup.TxResults[0].Code == 0 {
		t.Fatal("replayed transaction bytes were committed again")
	}
	dupAfter := storeBytes(t, cluster.app())
	if dupAfter != after {
		t.Fatalf("replayed bytes changed logical state from %d to %d", after, dupAfter)
	}
	hashBefore := append([]byte(nil), cluster.hashes[len(cluster.hashes)-1]...)
	emptyStarted := time.Now()
	cluster.block(t)
	emptyWall := time.Since(emptyStarted)
	hashAfter := cluster.hashes[len(cluster.hashes)-1]
	replayStarted := time.Now()
	cluster.replayFresh(t, net.doc, 0)
	replayWall := time.Since(replayStarted)
	fmt.Printf(
		"CAPACITY_CHAIN nodes=2 tx_bytes=%d gas_used=%d code=0 payload_bytes=%d state_before=%d state_after=%d state_delta=%d dup_code=%d dup_state_delta=%d empty_hash_changed=%t commit_wall_ms=%d empty_wall_ms=%d replay_wall_ms=%d heights=%d label=in-process-abci\n",
		len(raw),
		res.GasUsed,
		proto.Size(msg),
		before,
		after,
		after-before,
		dup.TxResults[0].Code,
		dupAfter-after,
		!bytes.Equal(hashBefore, hashAfter),
		commitWall.Milliseconds(),
		emptyWall.Milliseconds(),
		replayWall.Milliseconds(),
		cluster.height(),
	)
}

func storeBytes(t *testing.T, application *app.App) int {
	t.Helper()
	names := []string{
		authtypes.StoreKey,
		app.ModuleSVN,
		app.ModulePolicy,
		app.ModuleProviders,
		app.ModuleContent,
		app.ModuleLeases,
		app.ModuleEvidence,
		authority.ModuleName,
	}
	total := 0
	for _, name := range names {
		_, size := application.StoreRowsForTest(name)
		total += size
	}
	return total
}
