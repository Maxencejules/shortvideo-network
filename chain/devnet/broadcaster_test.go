package devnet_test

// TestDevnetBroadcasterLifecycle and TestDevnetBroadcasterQuorumStall are
// the goal 11 devnet acceptance tests of the transaction broadcaster (plan
// 13.4). Each runs on a single machine, four validator processes, private
// network namespace (the test commands run under the loopback-only
// namespace): not independent operators and not production. They drive the
// real svn-broadcaster binary (dev-db, dev-ca, migrate, signer, serve)
// against real svnd validators and a real PostgreSQL 18.6 cluster. Faults
// are either "fault injected by a test proxy on loopback" (the fault proxy
// between serve and validator 0), "build-tagged fault point" (the
// svn_faultpoint build of the binary), or a process crash (SIGKILL), not a
// host power loss.

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// bcPhase is one phase of a broadcaster devnet test.
type bcPhase struct {
	name string
	run  func(n *bnet, name string)
}

// bcPhasesEnv selects lifecycle phases for local debugging and mutation
// killers only (DR-11-36): a comma list of phase names, run after S. CI
// never sets it (tests/unit/test_ci_broadcaster.py fails if ci.yml names
// it); unset runs every phase.
const bcPhasesEnv = "SVN_BROADCASTER_PHASES"

// bcSelect returns the phases to run: S plus the selected ones, in the
// order of phases.
func bcSelect(t *testing.T, phases []bcPhase) []bcPhase {
	t.Helper()
	list := strings.TrimSpace(os.Getenv(bcPhasesEnv))
	if list == "" {
		fmt.Println("phases=all")
		return phases
	}
	want := map[string]bool{}
	for _, name := range strings.Split(list, ",") {
		want[strings.TrimSpace(name)] = true
	}
	var out []bcPhase
	known := map[string]bool{}
	for _, p := range phases {
		known[p.name] = true
		if p.name == "S" || want[p.name] {
			out = append(out, p)
		}
	}
	for name := range want {
		if !known[name] {
			t.Fatalf("%s names unknown phase %q", bcPhasesEnv, name)
		}
	}
	var names []string
	for _, p := range out {
		names = append(names, p.name)
	}
	fmt.Printf("phases=%s (selected by %s; not a full run)\n", strings.Join(names, ","), bcPhasesEnv)
	return out
}

// runPhase runs one phase, then the accounting check and the trace lines,
// and prints the phase's time, heights and proxy counters.
func (n *bnet) runPhase(p bcPhase) {
	n.t.Helper()
	started := time.Now()
	from := n.d.height()
	fmt.Printf("phase=%s begin height=%d\n", p.name, from)
	p.run(n, p.name)
	n.checkAccounting(p.name, false)
	n.printTraces(p.name)
	fmt.Printf("phase=%s seconds=%.1f heights=%d..%d %s\n", p.name, time.Since(started).Seconds(), from, n.d.height(), n.proxy.summary())
}

// bcLifecyclePhases are the phases of plan 13.4's lifecycle table, in run
// order. Phase L scans the logs written by every earlier phase, so it runs
// last; phase Z (the final full accounting) follows it.
var bcLifecyclePhases = []bcPhase{
	{"S", phaseSetup},
	{"H", phaseHappy},
	{"A", phaseAckCommit},
	{"TB1", phaseTimeoutBefore},
	{"TB2", phaseTimeoutDeadline},
	{"TA", phaseTimeoutAfter},
	{"WD", phaseWorkerDeath},
	{"WP1", phaseFaultPersist},
	{"WP2", phaseFaultSendsStarted},
	{"WP3", phaseFaultAfterBroadcast},
	{"HD", phaseHeldDelivered},
	{"DU", phaseDuplicates},
	{"FR1", phaseFrontRunBusinessID},
	{"FR1b", phaseFrontRunOwnAcceptance},
	{"FR2", phaseBusinessIDTaken},
	{"FC", phaseContentFrontRun},
	{"OC", phaseOfferCopy},
	{"CS", phaseConcurrentSigner},
	{"MR", phaseSignerConflict},
	{"UA", phaseUnauthorized},
	{"PI", phaseImpersonation},
	{"FW", phaseForward},
	{"PB", phasePublication},
	{"PG", phasePostgres},
	{"SG", phaseSignerDeath},
	{"BR", phaseRestarts},
	{"RS", phaseValidatorRestart},
	{"EX", phaseExpired},
	{"L", phaseLogCanary},
}

func TestDevnetBroadcasterLifecycle(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip("SVN_SKIP_DEVNET=1")
	}
	fmt.Println("label=single machine, four validator processes, private network namespace (not independent operators, not production)")
	out := teeStdout(t)
	phases := bcSelect(t, bcLifecyclePhases)
	n := newBnet(t)
	n.stdout = out
	for _, p := range phases {
		n.runPhase(p)
	}
	started := time.Now()
	n.checkAccounting("Z", true)
	n.printTraces("Z")
	fmt.Printf("phase=Z seconds=%.1f\n", time.Since(started).Seconds())
	n.stopAll()
	fmt.Printf("test=TestDevnetBroadcasterLifecycle seconds=%.1f ci_go_test_timeout=15m\n", time.Since(n.start).Seconds())
}

// stopAll stops serve, the signer and dev-db with SIGTERM and requires a
// clean exit and dev-db's cleanup.
func (n *bnet) stopAll() {
	n.t.Helper()
	for _, p := range []*bcProc{n.serve, n.signer} {
		if p == nil || p.exited() {
			continue
		}
		if code := p.stop(n.t); code != 0 {
			n.t.Fatalf("%s exit %d on SIGTERM: %s", p.name, code, p.logTail(10))
		}
	}
	if code := n.devdb.stop(n.t); code != 0 {
		n.t.Fatalf("dev-db exit %d on SIGTERM: %s", code, n.devdb.logTail(10))
	}
	if _, err := os.Lstat(n.db.ClusterDir); !os.IsNotExist(err) {
		n.t.Fatalf("dev-db left %s: %v", n.db.ClusterDir, err)
	}
}
