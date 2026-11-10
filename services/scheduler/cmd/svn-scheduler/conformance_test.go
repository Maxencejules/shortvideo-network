package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/sched"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/stage"
)

func TestUsage(t *testing.T) {
	if command(nil) != 2 {
		t.Fatal("usage")
	}
}

func TestConformance(t *testing.T) {
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

	ids := []string{"p1", "p2", "p3", "p4", "pcap"}
	quotas := map[string]uint64{"p1": 1 << 20, "p2": 1 << 20, "p3": 1 << 20, "p4": 1 << 20, "pcap": 10}
	roles := map[string]string{
		"p1": policy.RoleReplica, "p2": policy.RoleReplica, "p3": policy.RoleReplica,
		"p4": policy.RoleSpare, "pcap": policy.RoleReplica,
	}
	provs := map[string]*provider{}
	var hosts []sched.Host
	for _, id := range ids {
		item := startProvider(t, bin, id, quotas[id], src, chain)
		provs[id] = item
		hosts = append(hosts, sched.Host{
			ProviderID: id, Endpoint: item.url, CAFile: item.ca, DeclaredOperator: "op-" + id,
			DeclaredDomain: "sim-" + id, Simulated: true, PolicyID: "beta", Capacity: quotas[id],
			Role: roles[id], AdminToken: item.admin,
		})
	}
	var approveMu sync.Mutex
	coordinator := sched.New(sched.Config{
		PolicyID: "beta", NoticeSeconds: 600, EvidenceMaxAge: time.Minute, CooldownSeconds: 3600,
		MaxRepairAttempts: 3, ProbeAttempts: 2, PhysicalDomain: "vm-1", IndexerURL: index.URL,
		Stage: src, Hosts: hosts,
		Approver: func(id, offer string) error {
			approveMu.Lock()
			defer approveMu.Unlock()
			return provs[id].approve(offer)
		},
	})
	stopApply := make(chan struct{})
	go applyLoop(chain, coordinator, stopApply)
	t.Cleanup(func() { close(stopApply) })

	api := httptest.NewServer(coordinator.Handler())
	t.Cleanup(api.Close)
	health, err := http.Get(api.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	health.Body.Close()
	if health.StatusCode != 200 {
		t.Fatal(health.StatusCode)
	}

	deadline := time.Now().Unix() + 3600
	before := len(coordinator.Posted())
	withheld := publish(t, coordinator, src, chain, "cno", deadline, false)
	fmt.Printf("APPROVAL publication=%s reason=%s posts=%d\n", withheld.Publication, withheld.PublicationReason, len(coordinator.Posted())-before)
	if withheld.Publication != policy.PublicationWithheld || withheld.PublicationReason != "community_approval" || len(coordinator.Posted()) != before {
		t.Fatalf("approval %s %s", withheld.Publication, withheld.PublicationReason)
	}
	pub := publish(t, coordinator, src, chain, "cpub", deadline, true)
	show("PUBLISH", pub)
	requirePublished(t, pub)
	if !skipped(coordinator, "pcap", "capacity") {
		t.Fatal("low capacity was selected")
	}
	code, errText, err := coordinator.Offer(context.Background(), "cpub", "pcap")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("LOW_CAPACITY selected=false provider=pcap status=%d error=%s\n", code, errText)
	if code != 409 || errText != "insufficient quota" {
		t.Fatalf("pcap %d %s", code, errText)
	}

	src.DisconnectOrigin("cpub")
	provs["p1"].stop()
	atRisk, err := coordinator.Availability(context.Background(), "cpub")
	if err != nil {
		t.Fatal(err)
	}
	show("AT_RISK", atRisk)
	if !atRisk.AtRisk || atRisk.Reachability != policy.Degraded || atRisk.CountedReplicas != 2 {
		t.Fatalf("at risk counted=%d at_risk=%t", atRisk.CountedReplicas, atRisk.AtRisk)
	}
	status, body := coordinator.Distribute(context.Background(), "cpub", 1)
	if status != 200 || hexSum(body) != pubSHA(t, "cpub") {
		t.Fatalf("distribute %d", status)
	}
	if !matches(t, provs["p2"], coordinator, "cpub") || !matches(t, provs["p3"], coordinator, "cpub") {
		t.Fatal("survivor bytes")
	}
	if provs["p1"].packageStatus(t, "cpub", "") == 200 {
		t.Fatal("disconnected provider served")
	}
	repaired, err := coordinator.Repair(context.Background(), "cpub", 1)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("REPAIR state=%s source=%s replacement=%s\n", repaired.State, repaired.Source, repaired.Replacement)
	if repaired.State != policy.JobConfirmed || repaired.Replacement != "p4" {
		t.Fatalf("repair %s %s", repaired.State, repaired.Replacement)
	}
	restored, err := coordinator.Availability(context.Background(), "cpub")
	if err != nil {
		t.Fatal(err)
	}
	show("RESTORED", restored)
	if restored.AtRisk || restored.CountedReplicas != 3 || !matches(t, provs["p4"], coordinator, "cpub") {
		t.Fatalf("restored counted=%d at_risk=%t", restored.CountedReplicas, restored.AtRisk)
	}
	chain.mu.Lock()
	active := chain.activeCount("cpub")
	r1, r2, r3, r4 := chain.reserved["p1"], chain.reserved["p2"], chain.reserved["p3"], chain.reserved["p4"]
	originReserved := chain.reserved["origin"]
	txs := chain.txs
	dup := chain.commit(sched.Posted{LeaseID: "again", ProviderID: "p2", ContentID: "cpub", Version: 1, PackageBytes: 64})
	chain.mu.Unlock()
	fmt.Printf("ACCOUNTING active=%d reserved_p1=%d reserved_p2=%d reserved_p3=%d reserved_p4=%d origin_reserved=%d txs=%d duplicate_refused=%t\n",
		active, r1, r2, r3, r4, originReserved, txs, !dup)
	if active != 4 || r4 != 64 || originReserved != 0 || dup {
		t.Fatal("accounting")
	}
	restart(t, coordinator, provs["p1"])

	simultaneous(t, coordinator, src, chain, provs, deadline)
	duplicate(t, coordinator, src, chain, deadline)
	renewal(t, coordinator, "cpub", deadline)
	evidence(t, coordinator)
	stall(t, coordinator, src, chain, deadline)
	removal(t, coordinator, src, chain, provs, deadline)
	lostView := publish(t, coordinator, src, chain, "clost", deadline, true)
	requirePublished(t, lostView)
	drain(t, coordinator, src, chain, provs, deadline)
	lost(t, coordinator, src, provs)

	fmt.Printf("TRACES %d\n", len(coordinator.Traces()))
	for _, line := range coordinator.Traces() {
		if strings.Contains(line, "repair") || strings.Contains(line, "depart") || strings.Contains(line, "distribution") || strings.Contains(line, "queue") || strings.Contains(line, "unreachable") {
			fmt.Printf("TRACE %s\n", line)
		}
	}
}

func requirePublished(t *testing.T, view sched.View) {
	t.Helper()
	if view.Publication != policy.PublicationPublished || view.VerifiedTransfers != 3 || view.DistinctOperators != 3 {
		t.Fatalf("publish %s verified=%d distinct=%d", view.Publication, view.VerifiedTransfers, view.DistinctOperators)
	}
	if view.OriginCounted || !view.OriginBackupTracked || view.IndependentOperatorVerification || view.StorageProof || view.SchedulerSigned {
		t.Fatal("claims")
	}
	if view.PhysicalFailureDomainCount != 1 || len(view.PhysicalFailureDomains) != 1 || view.PhysicalFailureDomains[0] != "vm-1" || !view.SimulatedLabels {
		t.Fatal("domains")
	}
	if view.EvidenceMode != policy.EvidenceProviderAcceptance || view.ClaimLimit != policy.ClaimLimit {
		t.Fatalf("evidence %s %s", view.EvidenceMode, view.ClaimLimit)
	}
}

func simultaneous(t *testing.T, coordinator *sched.Sched, src *stage.Stage, chain *chainFix, provs map[string]*provider, deadline int64) {
	t.Helper()
	view := publish(t, coordinator, src, chain, "csim", deadline, true)
	requirePublished(t, view)
	provs["p1"].stop()
	provs["p2"].stop()
	job, err := coordinator.Repair(context.Background(), "csim", 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := coordinator.Availability(context.Background(), "csim")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("SIMULTANEOUS state=%s counted=%d at_risk=%t reachability=%s\n", job.State, got.CountedReplicas, got.AtRisk, got.Reachability)
	if job.State != policy.JobConfirmed || got.CountedReplicas != 2 || !got.AtRisk || got.Reachability != policy.Degraded {
		t.Fatalf("simultaneous %s counted=%d", job.State, got.CountedReplicas)
	}
	restart(t, coordinator, provs["p1"])
	restart(t, coordinator, provs["p2"])
}

func duplicate(t *testing.T, coordinator *sched.Sched, src *stage.Stage, chain *chainFix, deadline int64) {
	t.Helper()
	view := publish(t, coordinator, src, chain, "cdup", deadline, true)
	requirePublished(t, view)
	coordinator.SetDown("p1", true)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	coordinator.SetBeforeOffer(func() {
		once.Do(func() { close(entered) })
		<-release
	})
	before := len(coordinator.Posted())
	done := make(chan policy.Job, 1)
	go func() {
		job, err := coordinator.Repair(context.Background(), "cdup", 1)
		if err != nil {
			t.Errorf("duplicate repair: %v", err)
		}
		done <- job
	}()
	select {
	case <-entered:
	case <-time.After(20 * time.Second):
		t.Fatal("repair did not reach the offer")
	}
	second, err := coordinator.Repair(context.Background(), "cdup", 1)
	if err != nil {
		t.Fatal(err)
	}
	mid := len(coordinator.Posted())
	close(release)
	first := <-done
	fmt.Printf("DUPLICATE state=%s attempts=%d posts_while_blocked=%d final=%s\n", second.State, second.Attempts, mid-before, first.State)
	if second.State != policy.JobDuplicate || second.Attempts != 1 || mid != before || first.State != policy.JobConfirmed {
		t.Fatalf("duplicate %s attempts=%d posts=%d final=%s", second.State, second.Attempts, mid-before, first.State)
	}
	coordinator.SetBeforeOffer(nil)
	coordinator.SetDown("p1", false)
}

func renewal(t *testing.T, coordinator *sched.Sched, content string, deadline int64) {
	t.Helper()
	before := len(coordinator.Posted())
	coordinator.SetClock(func() time.Time { return time.Unix(deadline-30, 0) })
	view, err := coordinator.Availability(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("RENEWAL_NOTICE renewal=%s posts=%d\n", view.Renewal, len(coordinator.Posted())-before)
	if view.Renewal != policy.RenewalNeeded || len(coordinator.Posted()) != before {
		t.Fatal(view.Renewal)
	}
	coordinator.SetClock(func() time.Time { return time.Unix(deadline+10, 0) })
	view, err = coordinator.Availability(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	state := ""
	if len(view.LeaseState) > 0 {
		state = view.LeaseState[0].State
	}
	fmt.Printf("RENEWAL_EXPIRED renewal=%s lease_state=%s renewal_submitted=%t\n", view.Renewal, state, len(coordinator.Posted()) != before)
	if view.Renewal != policy.RenewalExpired || state != "ACTIVE" || len(coordinator.Posted()) != before {
		t.Fatal(view.Renewal)
	}
	coordinator.SetClock(time.Now)
}

func evidence(t *testing.T, coordinator *sched.Sched) {
	t.Helper()
	measured := strings.Repeat("ab", 32)
	claimed := "bb" + measured[2:]
	mode, limit := coordinator.Observe("cpub", measured, claimed, time.Now())
	fmt.Printf("DISHONEST mode=%s claim_limit=%s storage_proof=false\n", mode, limit)
	if mode != policy.EvidenceDishonest || limit != policy.ClaimLimit {
		t.Fatalf("dishonest %s %s", mode, limit)
	}
	mode, limit = coordinator.Observe("cpub", measured, measured, time.Now().Add(-time.Hour))
	fmt.Printf("STALE mode=%s claim_limit=%s\n", mode, limit)
	if mode != policy.EvidenceStale || limit != policy.ClaimLimit {
		t.Fatalf("stale %s %s", mode, limit)
	}
}

func stall(t *testing.T, coordinator *sched.Sched, src *stage.Stage, chain *chainFix, deadline int64) {
	t.Helper()
	before := len(coordinator.Posted())
	chain.mu.Lock()
	chain.stale = true
	chain.mu.Unlock()
	view := publish(t, coordinator, src, chain, "cstall", deadline, true)
	fmt.Printf("STALL publication=%s service=%s posts=%d\n", view.Publication, view.ServiceLabel, len(coordinator.Posted())-before)
	if view.Publication != policy.QueuedNotCommitted || view.ServiceLabel != policy.ServiceTemporary || len(coordinator.Posted()) != before {
		t.Fatalf("stall %s %s", view.Publication, view.ServiceLabel)
	}
	chain.mu.Lock()
	chain.stale = false
	chain.mu.Unlock()
}

func removal(t *testing.T, coordinator *sched.Sched, src *stage.Stage, chain *chainFix, provs map[string]*provider, deadline int64) {
	t.Helper()
	view := publish(t, coordinator, src, chain, "crem", deadline, true)
	requirePublished(t, view)
	before := len(coordinator.Posted())
	coordinator.SetDown("p1", true)
	coordinator.SetBeforeOffer(func() { chain.setWithdrawn("crem") })
	job, err := coordinator.Repair(context.Background(), "crem", 1)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := coordinator.Distribute(context.Background(), "crem", 1)
	p2 := provs["p2"].packageStatus(t, "crem", leaseToken(coordinator, "crem", "p2"))
	fmt.Printf("REMOVAL state=%s posts=%d distribution=%d provider=%d\n", job.State, len(coordinator.Posted())-before, code, p2)
	if job.State != policy.JobWithdrawn || len(coordinator.Posted()) != before || code != 410 || p2 != 410 {
		t.Fatalf("removal %s dist=%d provider=%d posts=%d", job.State, code, p2, len(coordinator.Posted())-before)
	}
	coordinator.SetBeforeOffer(nil)
	coordinator.SetDown("p1", false)
}

func drain(t *testing.T, coordinator *sched.Sched, src *stage.Stage, chain *chainFix, provs map[string]*provider, deadline int64) {
	t.Helper()
	view := publish(t, coordinator, src, chain, "cdrain", deadline, true)
	requirePublished(t, view)
	if err := coordinator.Drain(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	token := leaseToken(coordinator, "cdrain", "p1")
	if coordinator.MayDepart("cdrain", "p1") {
		t.Fatal("departed before repair")
	}
	if provs["p1"].packageStatus(t, "cdrain", token) != 200 {
		t.Fatal("drained provider deleted early")
	}
	job, err := coordinator.Repair(context.Background(), "cdrain", 1)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != policy.JobConfirmed || !coordinator.MayDepart("cdrain", "p1") {
		t.Fatalf("handoff %s", job.State)
	}
	if provs["p1"].packageStatus(t, "cdrain", token) != 200 {
		t.Fatal("deleted before depart")
	}
	if err := coordinator.Depart(context.Background(), "cdrain", "p1", "lcdrainp1"); err != nil {
		t.Fatal(err)
	}
	p1 := provs["p1"].packageStatus(t, "cdrain", token)
	p2 := provs["p2"].packageStatus(t, "cdrain", leaseToken(coordinator, "cdrain", "p2"))
	p4 := provs["p4"].packageStatus(t, "cdrain", leaseToken(coordinator, "cdrain", "p4"))
	fmt.Printf("DRAIN depart_before_repair=false last_copy_kept=true after_depart_p1=%d p2=%d p4=%d\n", p1, p2, p4)
	if p1 != 410 || p2 != 200 || p4 != 200 {
		t.Fatal("handoff bytes")
	}
}

func lost(t *testing.T, coordinator *sched.Sched, src *stage.Stage, provs map[string]*provider) {
	t.Helper()
	src.DisconnectOrigin("clost")
	for _, id := range []string{"p1", "p2", "p3"} {
		provs[id].stop()
	}
	before := len(coordinator.Posted())
	job, err := coordinator.Repair(context.Background(), "clost", 1)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("UNRECOVERABLE state=%s posts=%d origin_used=false\n", job.State, len(coordinator.Posted())-before)
	if job.State != policy.JobUnrecoverable || len(coordinator.Posted()) != before {
		t.Fatalf("lost %s posts=%d", job.State, len(coordinator.Posted())-before)
	}
}

func publish(t *testing.T, coordinator *sched.Sched, src *stage.Stage, chain *chainFix, content string, deadline int64, approved bool) sched.View {
	t.Helper()
	putOrigin(src, content)
	chain.addContent(content, deadline)
	approval := ""
	if approved {
		approval = "mod-ok"
	}
	view, err := coordinator.Publish(context.Background(), packageFor(content, deadline), approved, approval)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func restart(t *testing.T, coordinator *sched.Sched, item *provider) {
	t.Helper()
	item.start(t)
	coordinator.SetEndpoint(item.id, item.url)
	coordinator.SetDown(item.id, false)
}

func matches(t *testing.T, item *provider, coordinator *sched.Sched, content string) bool {
	t.Helper()
	buf := item.packageBody(t, content, leaseToken(coordinator, content, item.id))
	return hexSum(buf) == pubSHA(t, content)
}

func show(name string, view sched.View) {
	fmt.Printf("%s publication=%s reason=%s evidence=%s claim=%s reachability=%s at_risk=%t active=%d distinct=%d verified=%d counted=%d origin_tracked=%t origin_counted=%t physical=%s physical_count=%d simulated=%t independent=%t service=%s renewal=%s scheduler_signed=%t storage_proof=%t reachable=%s degraded=%s\n",
		name, view.Publication, view.PublicationReason, view.EvidenceMode, view.ClaimLimit, view.Reachability, view.AtRisk,
		view.ActiveCommitments, view.DistinctOperators, view.VerifiedTransfers, view.CountedReplicas, view.OriginBackupTracked, view.OriginCounted,
		strings.Join(view.PhysicalFailureDomains, ","), view.PhysicalFailureDomainCount, view.SimulatedLabels, view.IndependentOperatorVerification,
		view.ServiceLabel, view.Renewal, view.SchedulerSigned, view.StorageProof, strings.Join(view.ReachableProviders, ","), strings.Join(view.DegradedProviders, ","))
}
