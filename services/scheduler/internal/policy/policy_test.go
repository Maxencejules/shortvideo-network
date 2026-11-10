package policy

import (
	"testing"
	"time"
)

func host(id, operator, role string, capacity uint64, reachable bool) Host {
	return Host{
		ProviderID: id, DeclaredOperator: operator, DeclaredDomain: "sim-" + id,
		Simulated: true, PolicyID: "beta", Capacity: capacity, Reachable: reachable,
		Admitted: true, Role: role,
	}
}

func TestSelectSkipsOriginPolicyCapacityAndDuplicateOperators(t *testing.T) {
	origin := host("origin", "op-o", RoleOriginBackup, 100, true)
	low := host("pcap", "op-low", RoleReplica, 10, true)
	other := host("px", "op-x", RoleReplica, 100, true)
	other.PolicyID = "other"
	down := host("pd", "op-d", RoleReplica, 100, false)
	spare := host("p4", "op-4", RoleSpare, 100, true)
	a := host("p1", "op-1", RoleReplica, 100, true)
	b := host("p2", "op-1", RoleReplica, 100, true)
	c := host("p3", "op-3", RoleReplica, 100, true)
	draining := host("pz", "op-z", RoleReplica, 100, true)
	draining.Draining = true
	out := Select([]Host{origin, low, other, down, spare, b, a, c, draining}, 64, "beta", false, 3)
	if len(out.Chosen) != 2 {
		t.Fatalf("chosen %d", len(out.Chosen))
	}
	if out.Chosen[0].ProviderID != "p1" || out.Chosen[1].ProviderID != "p3" {
		t.Fatalf("order %+v", out.Chosen)
	}
	reasons := map[string]string{}
	for _, skip := range out.Skipped {
		reasons[skip.ProviderID] = skip.Reason
	}
	for id, reason := range map[string]string{
		"origin": "origin_backup", "pcap": "capacity", "px": "policy",
		"pd": "unreachable", "p4": "spare_held_back", "p2": "operator", "pz": "draining",
	} {
		if reasons[id] != reason {
			t.Fatalf("%s reason %q", id, reasons[id])
		}
	}
}

func TestSpareIsEligibleForRepair(t *testing.T) {
	holder := host("p1", "op-1", RoleReplica, 100, true)
	holder.Holding = true
	spare := host("p4", "op-4", RoleSpare, 100, true)
	out := Select([]Host{holder, spare}, 64, "beta", true, 1)
	if len(out.Chosen) != 1 || out.Chosen[0].ProviderID != "p4" {
		t.Fatalf("%+v", out)
	}
}

func TestGateAndEvidenceAndRenewal(t *testing.T) {
	if status, reason := Gate(false, 3, 3, 3); status != PublicationWithheld || reason != "community_approval" {
		t.Fatalf("%s %s", status, reason)
	}
	if status, reason := Gate(true, 2, 3, 3); status != PublicationWithheld || reason != "verified_transfer" {
		t.Fatalf("%s %s", status, reason)
	}
	if status, reason := Gate(true, 3, 2, 3); status != PublicationWithheld || reason != "operator_distinct_commitments" {
		t.Fatalf("%s %s", status, reason)
	}
	if status, reason := Gate(true, 3, 3, 3); status != PublicationPublished || reason != "" {
		t.Fatalf("%s %s", status, reason)
	}
	mode, limit := Evidence("abc", "abd", 0, time.Minute)
	if mode != EvidenceDishonest || limit != ClaimLimit {
		t.Fatalf("dishonest %s %s", mode, limit)
	}
	mode, limit = Evidence("abc", "abc", 2*time.Minute, time.Minute)
	if mode != EvidenceStale || limit != ClaimLimit {
		t.Fatalf("stale %s %s", mode, limit)
	}
	mode, limit = Evidence("abc", "abc", time.Second, time.Minute)
	if mode != EvidenceProviderAcceptance || limit != ClaimLimit {
		t.Fatalf("acceptance %s %s", mode, limit)
	}
	if Renewal(100, 90, 20) != RenewalNeeded || Renewal(100, 100, 20) != RenewalExpired || Renewal(100, 10, 20) != RenewalNone {
		t.Fatal("renewal")
	}
	if MayDepart(2, 3) || !MayDepart(3, 3) {
		t.Fatal("depart")
	}
}

func TestJobDoesNotStartTwiceOrDuringCooldown(t *testing.T) {
	job, run := TouchJob(nil, 10, 100, 3)
	if !run || job.Attempts != 1 || job.State != JobRunning {
		t.Fatalf("start %+v %v", job, run)
	}
	again, run := TouchJob(&job, 11, 100, 3)
	if run || again.State != JobRunning || again.Attempts != 1 {
		t.Fatalf("duplicate %+v %v", again, run)
	}
	job.State = JobCooling
	job.NextUnix = 50
	held, run := TouchJob(&job, 20, 100, 3)
	if run || held.State != JobCooling {
		t.Fatalf("cooldown %+v %v", held, run)
	}
	job.Attempts = 3
	job.NextUnix = 20
	done, run := TouchJob(&job, 20, 100, 3)
	if run || done.State != JobExhausted {
		t.Fatalf("exhausted %+v %v", done, run)
	}
}

func TestManifestOrdersKeys(t *testing.T) {
	text := ManifestText("cpub", 1, 64, []Segment{{ID: "s0", SHA256: "aa", ByteCount: 32}, {ID: "s1", SHA256: "bb", ByteCount: 32}})
	want := `{"content_id":"cpub","content_version":1,"manifest_id":"mcpub","package_bytes":"64","profile":"SVN-JCS-1","renditions":[{"codec":"raw","rendition_id":"r0","segments":[{"byte_count":"32","segment_id":"s0","sha256":"aa"},{"byte_count":"32","segment_id":"s1","sha256":"bb"}]}],"schema_version":1}`
	if text != want {
		t.Fatalf("%s", text)
	}
}
