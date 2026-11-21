package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/sched"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/stage"
)

// confirmNet is four local provider processes and one coordinator.
// p4 is the spare. The labels are simulated. This is one machine.
func confirmNet(t *testing.T, bin string, confirmFor time.Duration) (*sched.Sched, map[string]*provider, *stage.Stage, *chainFix) {
	t.Helper()
	src, err := stage.Listen("source-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	chain := newChain()
	t.Cleanup(chain.close)
	indexSrv := httptest.NewServer(http.HandlerFunc(chain.serveIndex))
	t.Cleanup(indexSrv.Close)
	index := indexSrv.URL

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
	coordinator := sched.New(sched.Config{
		PolicyID: "beta", NoticeSeconds: 600, EvidenceMaxAge: time.Minute, CooldownSeconds: 3600,
		MaxRepairAttempts: 3, ProbeAttempts: 2, PhysicalDomain: "vm-1", IndexerURL: index,
		Stage: src, Hosts: hosts, ConfirmFor: confirmFor,
		Approver: func(id, offer string) error {
			return provs[id].approve(offer)
		},
	})
	stopApply := make(chan struct{})
	go applyLoop(chain, coordinator, stopApply)
	t.Cleanup(func() { close(stopApply) })
	return coordinator, provs, src, chain
}

func corruptSegments(t *testing.T, item *provider) int {
	t.Helper()
	root := filepath.Join(item.dir, "packages")
	n := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		buf, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(buf) == 0 {
			buf = []byte{0}
		}
		buf[0] ^= 0xff
		if err := os.WriteFile(path, buf, info.Mode()); err != nil {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no stored segment")
	}
	return n
}

func TestUnreachableHolderDoesNotExtendConfirm(t *testing.T) {
	bin := buildProvider(t)
	limit := 5 * time.Second
	coordinator, provs, src, chain := confirmNet(t, bin, limit)
	deadline := time.Now().Unix() + 3600
	view := publish(t, coordinator, src, chain, "cskip", deadline, true)
	requirePublished(t, view)
	provs["p1"].stop()
	started := time.Now()
	job, err := coordinator.Repair(context.Background(), "cskip", 1)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CONFIRM_UNREACHABLE wall_ms=%d limit_ms=%d state=%s replacement=%s\n",
		elapsed.Milliseconds(), limit.Milliseconds(), job.State, job.Replacement)
	if job.State != policy.JobConfirmed || job.Replacement != "p4" {
		t.Fatalf("repair %s %s", job.State, job.Replacement)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("unreachable holder extended confirm to %s (limit %s)", elapsed, limit)
	}
}

func TestReachableByteMismatchStillWaits(t *testing.T) {
	bin := buildProvider(t)
	limit := 1200 * time.Millisecond
	coordinator, provs, src, chain := confirmNet(t, bin, limit)
	deadline := time.Now().Unix() + 3600
	view := publish(t, coordinator, src, chain, "cwait", deadline, true)
	requirePublished(t, view)
	res, err := provs["p2"].client().Get(provs["p2"].url + "/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("holder health %d", res.StatusCode)
	}
	corruptSegments(t, provs["p2"])
	started := time.Now()
	job, err := coordinator.Repair(context.Background(), "cwait", 1)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CONFIRM_MISMATCH wall_ms=%d limit_ms=%d state=%s replacement=%s\n",
		elapsed.Milliseconds(), limit.Milliseconds(), job.State, job.Replacement)
	if job.State != policy.JobConfirmed || job.Replacement != "p4" {
		t.Fatalf("repair %s %s", job.State, job.Replacement)
	}
	if elapsed < limit {
		t.Fatalf("reachable mismatch returned in %s, under the %s confirm window", elapsed, limit)
	}
}

func TestRepairReverifyFailureIsCooling(t *testing.T) {
	bin := buildProvider(t)
	coordinator, provs, src, chain := confirmNet(t, bin, 2*time.Second)
	deadline := time.Now().Unix() + 3600
	view := publish(t, coordinator, src, chain, "ccool", deadline, true)
	requirePublished(t, view)
	provs["p1"].stop()
	coordinator.SetAfterConfirm(func() { corruptSegments(t, provs["p4"]) })
	job, err := coordinator.Repair(context.Background(), "ccool", 1)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CONFIRM_REVERIFY state=%s replacement=%s\n", job.State, job.Replacement)
	if job.State == policy.JobConfirmed {
		t.Fatal("confirmed without a second successful verify")
	}
	if job.State != policy.JobCooling {
		t.Fatalf("state %s", job.State)
	}
}
