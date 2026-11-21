//go:build svn_capacity

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/sched"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/stage"
)

// TestCapacityFixture times one publication and one repair on the local
// conformance processes. The package is 64 bytes. Operators are simulated
// labels on this machine. CI does not pass -tags svn_capacity.
func TestCapacityFixture(t *testing.T) {
	bin := buildProvider(t)
	src, err := stage.Listen("source-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	chain := newChain()
	t.Cleanup(chain.close)
	index := httptest.NewServer(http.HandlerFunc(chain.serveIndex))
	t.Cleanup(index.Close)

	ids := []string{"p1", "p2", "p3", "p4"}
	provs := map[string]*provider{}
	var hosts []sched.Host
	for _, id := range ids {
		item := startProvider(t, bin, id, 1<<20, src, chain)
		provs[id] = item
		role := policy.RoleReplica
		if id == "p4" {
			role = policy.RoleSpare
		}
		hosts = append(hosts, sched.Host{
			ProviderID: id, Endpoint: item.url, CAFile: item.ca, DeclaredOperator: "op-" + id,
			DeclaredDomain: "sim-" + id, Simulated: true, PolicyID: "beta", Capacity: 1 << 20,
			Role: role, AdminToken: item.admin,
		})
	}
	var delay atomic.Int64
	var approvals atomic.Int32
	var approveMu sync.Mutex
	coordinator := sched.New(sched.Config{
		PolicyID: "beta", NoticeSeconds: 600, EvidenceMaxAge: time.Minute, CooldownSeconds: 3600,
		MaxRepairAttempts: 3, ProbeAttempts: 2, PhysicalDomain: "vm-1", IndexerURL: index.URL,
		Stage: src, Hosts: hosts,
		Approver: func(id, offer string) error {
			approvals.Add(1)
			if wait := delay.Load(); wait > 0 {
				time.Sleep(time.Duration(wait) * time.Millisecond)
			}
			approveMu.Lock()
			defer approveMu.Unlock()
			return provs[id].approve(offer)
		},
	})
	stopApply := make(chan struct{})
	go applyLoop(chain, coordinator, stopApply)
	t.Cleanup(func() { close(stopApply) })

	deadline := time.Now().Unix() + 3600
	publishStarted := time.Now()
	published := publish(t, coordinator, src, chain, "ccap", deadline, true)
	publishWall := time.Since(publishStarted)
	requirePublished(t, published)
	fmt.Printf(
		"CAPACITY_PUBLISH package_bytes=64 wall_ms=%d publication=%s verified=%d physical_count=%d simulated=%t label=one-machine-fixture\n",
		publishWall.Milliseconds(), published.Publication, published.VerifiedTransfers,
		published.PhysicalFailureDomainCount, published.SimulatedLabels,
	)

	provs["p1"].stop()
	repairStarted := time.Now()
	repaired, err := coordinator.Repair(context.Background(), "ccap", 1)
	repairWall := time.Since(repairStarted)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf(
		"CAPACITY_REPAIR package_bytes=64 wall_ms=%d state=%s source=%s replacement=%s stopped=p1 hosts=4 machine=1 simulated=true label=process-stop-not-multi-host\n",
		repairWall.Milliseconds(), repaired.State, repaired.Source, repaired.Replacement,
	)
	if repaired.State != policy.JobConfirmed || repaired.Replacement != "p4" {
		t.Fatalf("repair %s %s", repaired.State, repaired.Replacement)
	}

	restart(t, coordinator, provs["p1"])
	before := approvals.Load()
	delay.Store(250)
	slowStarted := time.Now()
	slow := publish(t, coordinator, src, chain, "cslow", deadline, true)
	slowWall := time.Since(slowStarted)
	requirePublished(t, slow)
	fmt.Printf(
		"CAPACITY_SLOW package_bytes=64 wall_ms=%d injected_ms=250 approvals=%d label=in-process-sleep-not-a-shaped-network\n",
		slowWall.Milliseconds(), approvals.Load()-before,
	)
}
