package difftest

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// tailRules are the x/evidence chain-only checks the adapter applies after
// every spec check, in the chain's tail order (README "Check order", T1-T9).
var tailRules = []string{
	"evidence D-17", "evidence D-8", "evidence D-9", "evidence D-10", "evidence D-11", "evidence D-12",
	"evidence D-18", "evidence D-13", "evidence D-15",
}

// allEvidenceRulesButEpoch disables every evidence adapter rule except D-3
// (without D-3 the spec's epoch stays 1 and every audit is STALE_VERSION).
var allEvidenceRulesButEpoch = append(slices.Clone(tailRules), "evidence D-4")

// ruleFixture is one trace for one adapter rule: the step index the rule
// decides and the chain code there.
type ruleFixture struct {
	f     *fixture
	at    int
	chain msgkit.Code
	// aloneOff is the spec code at the rule step with only this rule
	// disabled: "ok", or the code of a later tail rule the item also breaks.
	aloneOff string
}

// long is a lease deadline far enough that no item reaches it.
const long = startTime + 2*86400

// tailFixtures builds one trace per tail rule (tailRules).
func tailFixtures() map[string]func() ruleFixture {
	return map[string]func() ruleFixture{
		"evidence D-17": func() ruleFixture {
			f := newFixture(nil, long)
			it := f.item(auditorA, "sl0", 600)
			it.evidenceID = prefixOf(auditorB) + "taken"
			return ruleFixture{f, f.audit(auditorA, it), msgkit.Forbidden, "ok"}
		},
		"evidence D-8": func() ruleFixture {
			f := newFixture(nil, long)
			it := f.item(auditorA, "sl0", 600)
			it.kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
			return ruleFixture{f, f.audit(auditorA, it), msgkit.Forbidden, "ok"}
		},
		"evidence D-9": func() ruleFixture {
			f := newFixture(nil, long)
			return ruleFixture{f, f.audit(auditorA, f.item(auditorA, "sl0", 86401)), msgkit.Bounds, "ok"}
		},
		"evidence D-10": func() ruleFixture {
			f := newFixture(nil, long)
			it := f.item(auditorA, "sl0", 600)
			it.contentVersion = 2
			return ruleFixture{f, f.audit(auditorA, it), msgkit.Conflict, "ok"}
		},
		"evidence D-11": func() ruleFixture {
			f := newFixture(nil, long)
			f.end("sl0")
			return ruleFixture{f, f.audit(auditorA, f.item(auditorA, "sl0", 600)), msgkit.Conflict, "ok"}
		},
		"evidence D-12": func() ruleFixture {
			// sl0 and sl1 fall due together; a budget of 1 sweeps sl0 and
			// leaves sl1 held past its deadline. Every expiry after now is
			// after sl1's deadline, so without D-12 the item fails D-18.
			due := startTime + 600
			f := newFixture(map[string]uint64{msgkit.LimitMaxExpirySteps: 1}, due, due)
			f.block(due)
			return ruleFixture{f, f.audit(auditorA, f.item(auditorA, "sl1", 60)), msgkit.Expired, string(msgkit.Conflict)}
		},
		"evidence D-18": func() ruleFixture {
			deadline := startTime + 2*termSeconds
			f := newFixture(nil, deadline)
			return ruleFixture{f, f.audit(auditorA, f.item(auditorA, "sl0", deadline+1-startTime)), msgkit.Conflict, "ok"}
		},
		"evidence D-13": func() ruleFixture {
			f := newFixture(nil, long)
			f.withdraw("c0")
			return ruleFixture{f, f.audit(auditorA, f.item(auditorA, "sl0", 600)), msgkit.Withdrawn, "ok"}
		},
		"evidence D-15": func() ruleFixture {
			// MaxEvidencePerAuditorPerEpoch one-item batches by auditor A in
			// the first epoch, then one more.
			f := newFixture(nil, long)
			for i := uint64(0); i < evidence.MaxEvidencePerAuditorPerEpoch; i++ {
				f.audit(auditorA, f.item(auditorA, "sl0", 3000))
			}
			return ruleFixture{f, f.audit(auditorA, f.item(auditorA, "sl0", 3000)), msgkit.Bounds, "ok"}
		},
	}
}

// TestDeviationRulesAreLoadBearing runs one trace per x/evidence adapter
// rule. With every rule on, every step agrees and carries exactly the
// deviation the Go side derives. Then:
//
//   - each tail rule (D-17, D-8, D-9, D-10, D-11, D-12, D-18, D-13, D-15):
//     with only that rule off, the first divergence is the rule step; with
//     every evidence rule but D-3 off, spec_machine.py alone says ok there;
//   - D-3: with it off the spec's epoch stays 1 and the first audit
//     diverges (STALE_VERSION in the spec, ok on the chain);
//   - D-4: with it off the spec sweeps the falling-due record inside the
//     expiry budget at the block (stored EXPIRED, and the lease due at the
//     same time stays held), so the first divergence is that block;
//   - D-6 (the request mapping, never turned off): sending the spec only
//     the first item of a 33-item batch, without item_count, makes it say
//     ok where the chain says BOUNDS.
//
// So each rule is needed, and no adapter rule hides another difference.
// The lease rules of the shared oracle (D-2 to D-6, D-10 to D-13) have
// their own fixtures in chain/x/leases/difftest.
func TestDeviationRulesAreLoadBearing(t *testing.T) {
	o := getOracle(t)
	fixtures := tailFixtures()
	for _, rule := range tailRules {
		t.Run(rule, func(t *testing.T) {
			rf := fixtures[rule]()
			on := mustAgree(t, rf.f, o)
			if got := on.specs[rf.at].deviation(); got != rule || on.outs[rf.at].code != rf.chain {
				t.Fatalf("rule step %d: chain %s, oracle deviation %q; want %s under %s", rf.at, on.outs[rf.at].code, got, rf.chain, rule)
			}
			for i, spec := range on.specs {
				if i != rf.at && spec.Deviation != nil && rf.f.steps[i].op != fxBlock {
					t.Errorf("step %d (%s) carries deviation %s", i, rf.f.steps[i].op, *spec.Deviation)
				}
			}
			// The runs with rules off only change the oracle: the keepers
			// replay the run above step for step, so its trial and
			// invariant checks are not repeated, and the steps before the
			// rule step are compared without projections (runFixtureFrom;
			// the D-15 trace is 1036 steps long).
			quiet := *rf.f
			quiet.cfg = &config{governed: rf.f.cfg.governed}
			for _, off := range []struct {
				name    string
				disable []string
				want    string
			}{
				{"only " + rule + " off", []string{rule}, rf.aloneOff},
				{"every evidence rule but D-3 off", allEvidenceRulesButEpoch, "ok"},
			} {
				rec := &recorder{TB: t}
				run := runFixtureFrom(t, rec, &quiet, o, off.disable, nil, on, rf.at)
				if run.firstMismatch != rf.at || len(rec.errors) == 0 {
					t.Fatalf("%s: the first divergence is step %d, want %d", off.name, run.firstMismatch, rf.at)
				}
				if got := run.specs[rf.at].Code; got != off.want {
					t.Fatalf("%s: the spec says %s at step %d, want %s", off.name, got, rf.at, off.want)
				}
			}
			t.Logf("%s: chain %s at step %d; with the rule off the spec says %s; spec_machine.py alone: ok",
				rule, on.outs[rf.at].code, rf.at, rf.aloneOff)
		})
	}

	t.Run("evidence D-3", func(t *testing.T) {
		// The first record expires after the boundary block, so that block
		// crosses the epoch and nothing else (no evidence D-4).
		f := newFixture(nil, long)
		first := f.audit(auditorA, f.item(auditorA, "sl0", 2*epochSeconds))
		next := (startTime/epochSeconds + 1) * epochSeconds
		crossing := f.block(next)
		stale := f.auditWith(auditParams{
			signer: auditorA, business: f.fresh("bf"), batchID: prefixOf(auditorA) + f.fresh("batch"),
			epoch: epochOf(startTime), items: []auditItem{f.item(auditorA, "sl0", 600)}, variant: sigValid,
		})
		current := f.audit(auditorA, f.item(auditorA, "sl0", 600))
		on := mustAgree(t, f, o)
		on.requireCodes(t, map[int]msgkit.Code{first: msgkit.OK, stale: msgkit.StaleVersion, current: msgkit.OK})
		if got := on.specs[crossing].deviation(); got != "evidence D-3" {
			t.Fatalf("the block crossing the epoch boundary reports %q", got)
		}
		rec := &recorder{TB: t}
		off := runFixture(t, rec, f, o, []string{"evidence D-3"}, nil)
		if off.firstMismatch != first || off.specs[first].Code != string(msgkit.StaleVersion) {
			t.Fatalf("with D-3 off: first divergence at step %d (spec %s), want step %d with STALE_VERSION",
				off.firstMismatch, off.specs[off.firstMismatch].Code, first)
		}
		t.Logf("evidence D-3: chain ok at step %d (epoch %d); spec_machine.py alone (epoch 1): STALE_VERSION", first, epochOf(startTime))
	})

	t.Run("evidence D-4", func(t *testing.T) {
		f, block, recorded := noBudgetFixture()
		on := mustAgree(t, f, o)
		if got := on.specs[block].deviation(); got != "evidence D-4" {
			t.Fatalf("the block where %s falls due reports %q", recorded, got)
		}
		rec := &recorder{TB: t}
		off := runFixture(t, rec, f, o, []string{"evidence D-4"}, nil)
		if off.firstMismatch != block || off.specs[block].Code != "ok" {
			t.Fatalf("with D-4 off: first divergence at step %d, want the block at step %d", off.firstMismatch, block)
		}
		_, stored := evidenceState(off.specs[block].Projection, recorded)
		t.Logf("evidence D-4: with the rule off the spec stores %s for %s and keeps a due lease held: %s", stored, recorded, joinErrors(rec.errors))
	})

	t.Run("evidence D-6", func(t *testing.T) {
		f := newFixture(nil, long)
		var items []auditItem
		for i := 0; i < 33; i++ {
			items = append(items, f.item(auditorA, "sl0", 600))
		}
		at := f.audit(auditorA, items...)
		on := mustAgree(t, f, o)
		if on.outs[at].code != msgkit.Bounds || on.specs[at].deviation() != "evidence D-6" {
			t.Fatalf("33-item batch: chain %s, oracle deviation %q", on.outs[at].code, on.specs[at].deviation())
		}
		firstOnly := func(i int, s *step) *step {
			if i != at {
				return s
			}
			cut := *s
			cut.spec = maps.Clone(s.spec)
			cut.spec["items"] = s.spec["items"].([]any)[:1]
			return &cut
		}
		rec := &recorder{TB: t}
		off := runFixture(t, rec, f, o, false, firstOnly)
		if off.firstMismatch != at || off.specs[at].Code != "ok" {
			t.Fatalf("without the mapping: first divergence at step %d (spec %s), want step %d with ok",
				off.firstMismatch, off.specs[off.firstMismatch].Code, at)
		}
		t.Logf("evidence D-6: chain BOUNDS for 33 items; spec_machine.py given only the first item: ok")
	})
}

// noBudgetFixture is the deviation D-4 trace: with max_expiry_steps 1, a
// RECORDED record of the long lease sl2 expires at the same time T as the
// deadline of sl0; a block at T. It returns the fixture, the block's index,
// and the record's id.
func noBudgetFixture() (*fixture, int, string) {
	due := startTime + 900
	f := newFixture(map[string]uint64{msgkit.LimitMaxExpirySteps: 1}, due, long, long)
	it := f.item(auditorA, "sl2", due-startTime)
	f.audit(auditorA, it)
	return f, f.block(due), it.evidenceID
}

// TestComparatorDetectsMismatch perturbs one field of a real oracle answer
// (or of the Go outcome) at a time and requires compareStep to report it.
func TestComparatorDetectsMismatch(t *testing.T) {
	o := getOracle(t)
	f := newFixture(nil, long)
	f.audit(auditorA, f.item(auditorA, "sl0", 600))
	run := mustAgree(t, f, o)
	at := len(f.steps) - 1
	s, out, proj, spec := f.steps[at], run.outs[at], run.projections[at], run.specs[at]
	if !compareStep(t, "unperturbed", s, out, proj, spec) {
		t.Fatalf("the unperturbed step must agree")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	mutants := []struct {
		name   string
		mutate func(*specResult)
	}{
		{"code", func(r *specResult) { r.Code = string(msgkit.Unauthorized) }},
		{"idempotent flag", func(r *specResult) { r.Idempotent = !r.Idempotent }},
		{"event id", func(r *specResult) { r.Events[0].ID = "e-other" }},
		{"event type", func(r *specResult) { r.Events[0].Type = "LeaseRenewed" }},
		{"event dropped", func(r *specResult) { r.Events = nil }},
		{"chain time", func(r *specResult) { r.Projection.ChainTime = "1" }},
		{"evidence reported state", func(r *specResult) { r.Projection.Evidence[0].State = "EXPIRED" }},
		{"evidence stored state", func(r *specResult) { r.Projection.Evidence[0].StoredState = "REJECTED" }},
		{"evidence batch id", func(r *specResult) { r.Projection.Evidence[0].BatchID = "b-other" }},
		{"evidence kind", func(r *specResult) { r.Projection.Evidence[0].Kind = "PROVIDER_RECEIPT" }},
		{"evidence lease", func(r *specResult) { r.Projection.Evidence[0].LeaseID = "l-other" }},
		{"evidence content", func(r *specResult) { r.Projection.Evidence[0].ContentID = "c-other" }},
		{"evidence expiry", func(r *specResult) { r.Projection.Evidence[0].ExpiryUnixSeconds = "2" }},
		{"evidence digest", func(r *specResult) { r.Projection.Evidence[0].ObservationDigest = strings.Repeat("ab", 32) }},
		{"evidence attestation", func(r *specResult) { r.Projection.Evidence[0].Attestation = false }},
		{"evidence dropped", func(r *specResult) { r.Projection.Evidence = nil }},
		{"batch id dropped", func(r *specResult) { r.Projection.BatchIDs = nil }},
		{"business id dropped", func(r *specResult) { r.Projection.BusinessIDs = r.Projection.BusinessIDs[1:] }},
		{"lease state", func(r *specResult) { r.Projection.Leases[0].State = "ENDED" }},
		{"reserved bytes", func(r *specResult) { r.Projection.Providers[0].ReservedBytes = "0" }},
	}
	for _, m := range mutants {
		var mutated specResult
		if err := json.Unmarshal(raw, &mutated); err != nil {
			t.Fatal(err)
		}
		m.mutate(&mutated)
		rec := &recorder{TB: t}
		if compareStep(rec, m.name, s, out, proj, mutated) || len(rec.errors) == 0 {
			t.Errorf("mutant %q was not detected", m.name)
		}
	}
	goMutant := out
	goMutant.code = msgkit.Unauthorized
	rec := &recorder{TB: t}
	if compareStep(rec, "go code", s, goMutant, proj, spec) || len(rec.errors) == 0 {
		t.Errorf("a changed Go code was not detected")
	}
	t.Logf("%d spec mutants and 1 Go mutant detected", len(mutants))
}

// TestTrialSnapshotDetectsEarlyWrite shows the harness's store checks fire:
// a handler wrapper that writes a key and then returns the real handler's
// result is reported for a rejected audit and for an idempotent replay, and
// a wrapper that writes into the leases store around an accepted audit is
// reported as an I9 violation, while the unwrapped handler is not.
func TestTrialSnapshotDetectsEarlyWrite(t *testing.T) {
	f := newFixture(nil, long)
	accepted := f.audit(auditorA, f.item(auditorA, "sl0", 600))
	h := newHarness(t, f.cfg)
	for _, s := range f.steps {
		if out := h.apply(s); out.code != msgkit.OK {
			t.Fatalf("setup %s: %s", s.op, out.code)
		}
	}
	replay := f.steps[accepted]
	rejected := auditParams{
		signer: auditorA, business: "b-rejected", batchID: prefixOf(auditorA) + "b-rejected", epoch: epochOf(startTime),
		items: []auditItem{f.item(auditorA, "sl0", 600)}, variant: sigOther + "observation_sha256",
	}.build("")
	fresh := func() *step {
		return auditParams{
			signer: auditorA, business: f.fresh("bf"), batchID: prefixOf(auditorA) + f.fresh("batch"), epoch: epochOf(startTime),
			items: []auditItem{f.item(auditorA, "sl0", 600)}, variant: sigValid,
		}.build("")
	}

	rec := &recorder{TB: t}
	h.tb = rec
	if out := h.apply(rejected); out.code != msgkit.Unauthorized || len(rec.errors) != 0 {
		t.Fatalf("control: code %s, reports %v", out.code, rec.errors)
	}
	if out := h.apply(replay); !out.idempotent || len(rec.errors) != 0 {
		t.Fatalf("control replay: idempotent %t, reports %v", out.idempotent, rec.errors)
	}
	if out := h.apply(fresh()); out.code != msgkit.OK || len(rec.errors) != 0 {
		t.Fatalf("control accepted: code %s, reports %v", out.code, rec.errors)
	}

	// The first mutant writes into the authority store, which every trial
	// snapshot covers and no invariant reads.
	h.intercept = func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error {
		ctx.KVStore(h.keys[keyAuthority]).Set([]byte("early-write"), []byte{1})
		return next(ctx, msg)
	}
	if out := h.apply(rejected); out.code != msgkit.Unauthorized {
		t.Fatalf("mutant reject: code %s", out.code)
	}
	if len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "changed the trial state") {
		t.Fatalf("the early write of a rejected audit was not reported: %v", rec.errors)
	}
	if out := h.apply(replay); !out.idempotent {
		t.Fatalf("mutant replay is not idempotent")
	}
	if len(rec.errors) != 2 || !strings.Contains(rec.errors[1], "idempotent") {
		t.Fatalf("the write of an idempotent replay was not reported: %v", rec.errors)
	}

	// The second mutant writes into the leases store after an accepted audit.
	h.intercept = func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error {
		err := next(ctx, msg)
		ctx.KVStore(h.keys[keyLeases]).Set([]byte("\xffi9-write"), []byte{1})
		return err
	}
	h.cfg.invariants = false // the stray row is not a lease row; I9 is the check under test
	if out := h.apply(fresh()); out.code != msgkit.OK {
		t.Fatalf("mutant accepted: code %s", out.code)
	}
	if len(rec.errors) != 3 || !strings.Contains(rec.errors[2], "I9") {
		t.Fatalf("a write into the leases store by an accepted audit was not reported: %v", rec.errors)
	}
}

// forceVerifyOK is a test-only stand-in for a chain whose attestation check
// always passes: before routing, it re-signs every audit batch of an
// auditor with that auditor's key over the chain's own sign bytes
// (evidence.AuditSignBytes), keeping the domain. Every other check runs.
func forceVerifyOK(h *harness) {
	h.intercept = func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error {
		batch, ok := msg.(*evidencev1.MsgSubmitAuditBatch)
		key := auditorKeyOf(batch.GetSigner())
		if !ok || key == "" || batch.GetAuditorSignature() == nil {
			return next(ctx, msg)
		}
		forged := proto.Clone(batch).(*evidencev1.MsgSubmitAuditBatch)
		forged.AuditorSignature.PublicKey = pubFor(key)
		payload, err := evidence.AuditSignBytes(forged)
		if err != nil {
			return next(ctx, msg)
		}
		forged.AuditorSignature.Signature = ed25519.Sign(keyFor(key), payload)
		return next(ctx, forged)
	}
}

// TestChainVerifyForcedOKDiverges shows the differential detects a chain
// that accepts every attestation signature: with forceVerifyOK, a fixture
// diverges exactly at its first altered-signature audit, and seeded
// sequences diverge first at an audit whose signature is not valid.
func TestChainVerifyForcedOKDiverges(t *testing.T) {
	o := getOracle(t)
	f := newFixture(nil, long)
	f.audit(auditorA, f.item(auditorA, "sl0", 600))
	altered := f.auditWith(auditParams{
		signer: auditorA, business: f.fresh("bf"), batchID: prefixOf(auditorA) + f.fresh("batch"), epoch: epochOf(startTime),
		items: []auditItem{f.item(auditorA, "sl0", 600)}, variant: sigOther + "expiry_unix_seconds",
	})
	mustAgree(t, f, o)
	h := newHarness(t, f.cfg)
	forceVerifyOK(h)
	o.reset(t, f.cfg, false)
	for i, s := range f.steps {
		out := h.apply(s)
		spec := o.step(t, s)
		rec := &recorder{TB: t}
		agree := compareStep(rec, "forced", s, out, h.view().projection(), spec)
		if i < altered && !agree {
			t.Fatalf("step %d diverged before the altered signature: %v", i, rec.errors)
		}
		if i == altered {
			if agree || out.code != msgkit.OK || spec.Code != string(msgkit.Unauthorized) {
				t.Fatalf("altered step: chain %s, spec %s, agree %t", out.code, spec.Code, agree)
			}
			break
		}
	}

	for seed := 0; seed < 8; seed++ {
		rec := &recorder{TB: t}
		_, ok := runSequence(rec, newRandChooser(famDefault, uint64(seed)), seqOptions{
			family: famDefault, ops: diffOps, oracle: o, intercept: forceVerifyOK, label: "forced",
		})
		if ok {
			continue
		}
		first := rec.errors[0]
		if !strings.Contains(first, "SubmitAuditBatch") || strings.Contains(first, "["+sigValid+"]") ||
			!strings.Contains(first, "spec "+string(msgkit.Unauthorized)) {
			t.Fatalf("seed %d: the first divergence is not an altered signature: %s", seed, first)
		}
		t.Logf("seed %d diverged first at an altered signature: %s", seed, first[:strings.Index(first, "\n")])
		return
	}
	t.Fatal("no seeded sequence detected a chain that accepts every signature")
}

// TestGeneratorRestrictionsHideKnownDivergences shows that generator
// restrictions hide real chain/spec differences that have no adapter rule
// (they are deviations D-1 and D-2 of chain/x/evidence/README.md, which the
// spec's abstract signature cannot express): with every adapter rule on,
// the first divergence of each trace is its last step.
//
//   - D-2: auditor B resends auditor A's business id and spec-visible
//     fields under B's own valid signature. The chain's business payload
//     carries the real signature (REPLAY); spec_machine.py has one auditor
//     key, so the payload is the same there (idempotent ok). gen.retry
//     keeps an audit's signer for a changed payload.
//   - D-1: a content_version, epoch, or expiry above 2^53-1 has no
//     SVN-JCS-1 form, so the chain's signature check fails (UNAUTHORIZED);
//     spec_machine.py reaches the field's own step (MALFORMED at the
//     business gate, STALE_VERSION, BOUNDS). The generator keeps signed
//     integers within the safe range.
func TestGeneratorRestrictionsHideKnownDivergences(t *testing.T) {
	o := getOracle(t)
	const huge = int64(1) << 53
	for _, c := range []struct {
		name       string
		last       func(f *fixture) *step
		chain      msgkit.Code
		spec       string
		idempotent bool
	}{
		{"D-2 business id replayed by another auditor", func(f *fixture) *step {
			first := f.steps[len(f.steps)-1]
			m := auditBatch(first)
			return auditParams{
				signer: auditorB, business: m.GetBusinessId(), batchID: m.GetBatchId(), epoch: m.GetEpoch(),
				items: []auditItem{itemOf(m.GetItems()[0])}, variant: sigValid,
			}.build("")
		}, msgkit.Replay, "ok", true},
		{"D-1 content_version 2^53", func(f *fixture) *step {
			it := f.item(auditorA, "sl0", 600)
			it.contentVersion = uint64(huge)
			return f.params(auditorA, it).build("")
		}, msgkit.Unauthorized, string(msgkit.Malformed), false},
		{"D-1 epoch 2^53", func(f *fixture) *step {
			p := f.params(auditorA, f.item(auditorA, "sl0", 600))
			p.epoch = uint64(huge)
			return p.build("")
		}, msgkit.Unauthorized, string(msgkit.StaleVersion), false},
		{"D-1 expiry 2^53", func(f *fixture) *step {
			it := f.item(auditorA, "sl0", 600)
			it.expiry = huge
			return f.params(auditorA, it).build("")
		}, msgkit.Unauthorized, string(msgkit.Bounds), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(nil, long)
			f.audit(auditorA, f.item(auditorA, "sl0", 600))
			f.add(c.last(f))
			at := len(f.steps) - 1
			rec := &recorder{TB: t}
			run := runFixture(t, rec, f, o, false, nil)
			if run.firstMismatch != at || len(rec.errors) == 0 {
				t.Fatalf("first divergence at step %d, want %d (reports %v)", run.firstMismatch, at, rec.errors)
			}
			out, spec := run.outs[at], run.specs[at]
			if out.code != c.chain || spec.Code != c.spec || spec.Idempotent != c.idempotent {
				t.Fatalf("chain %s, spec %s idempotent %t; want chain %s, spec %s idempotent %t",
					out.code, spec.Code, spec.Idempotent, c.chain, c.spec, c.idempotent)
			}
			t.Logf("%s: chain %s, spec_machine.py %s (idempotent %t)", c.name, out.codeName(), spec.Code, spec.Idempotent)
		})
	}
}

func auditBatch(s *step) *evidencev1.MsgSubmitAuditBatch {
	return s.msg.(*evidencev1.MsgSubmitAuditBatch)
}

func itemOf(it *evidencev1.EvidenceItem) auditItem {
	return auditItem{
		evidenceID: it.GetEvidenceId(), kind: it.GetKind(), leaseID: it.GetLeaseId(), contentID: it.GetContentId(),
		contentVersion: it.GetContentVersion(), digest: it.GetObservationDigest().GetValue(),
		expiry: it.GetExpiryUnixSeconds(), attestation: it.GetAttestation(),
	}
}

// TestFrozenJCSAcceptsNonASCIIDigits records a divergence between the frozen
// Python reference docs/evidence/goal-02/jcs.py and chain/jcs: the Python
// parser reads digits with str.isdigit(), so it accepts non-ASCII decimal
// digits (U+0661 ARABIC-INDIC DIGIT ONE, U+FF11 FULLWIDTH DIGIT ONE) as a
// JSON number, and the Go parser refuses them. The oracle never calls the
// Python parser (it reads requests with the json module, whose numbers are
// ASCII), and the generator writes only ASCII, so the differential cannot
// reach the difference. This test keeps it visible; it fails if either side
// changes.
func TestFrozenJCSAcceptsNonASCIIDigits(t *testing.T) {
	root := repoRoot(t)
	python := pythonPath(t, root)
	for _, text := range []string{`{"n":` + "١" + `}`, `{"n":` + "１" + `}`} {
		cmd := exec.Command(python, "-I", "-B", "-c",
			"import sys; sys.path.insert(0, sys.argv[1]); import jcs; print(jcs.parse_and_canonicalize(sys.argv[2]))",
			filepath.Join(root, "docs", "evidence", "goal-02"), text)
		raw, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("jcs.py on %q: %v: %s", text, err, raw)
		}
		pyOut := strings.TrimSpace(string(raw))
		goOut, goErr := jcs.ParseAndCanonicalize(text)
		if pyOut != `{"n":1}` || goErr == nil {
			t.Fatalf("%q: jcs.py %q, chain/jcs %q, %v; the recorded divergence changed", text, pyOut, goOut, goErr)
		}
		t.Logf("%q: frozen jcs.py accepts it as %s; chain/jcs refuses it (%v)", text, pyOut, goErr)
	}
}

// TestHarnessConstants checks that the harness's own copies of protocol
// constants (epoch length, address and id-prefix rule, domain strings, id
// pattern) equal the chain's. The harness keeps its own copies so a changed
// chain rule shows up as a divergence; this test names the copy that drifted.
func TestHarnessConstants(t *testing.T) {
	if epochSeconds != evidence.EpochSeconds {
		t.Errorf("epochSeconds %d, evidence.EpochSeconds %d", epochSeconds, evidence.EpochSeconds)
	}
	for _, label := range []string{auditorKeyA, auditorKeyB} {
		pub := pubFor(label)
		if !bytes.Equal(addressOf(pub), evidence.AuditorAddress(pub)) {
			t.Errorf("%s: addressOf differs from evidence.AuditorAddress", label)
		}
		if prefixOf(addressOf(pub)) != evidence.IDPrefix(addressOf(pub)) {
			t.Errorf("%s: prefixOf differs from evidence.IDPrefix", label)
		}
	}
	if prefixOf(auditorA) == prefixOf(auditorB) {
		t.Errorf("the two auditor accounts share an id prefix")
	}
	for domain, name := range domainStrings {
		if chain, ok := msgkit.DomainString(domain); !ok || chain != name {
			t.Errorf("domain %v: harness %q, chain %q", domain, name, chain)
		}
	}
	if idPattern.String() != `^[a-z0-9][a-z0-9._-]{0,63}$` {
		t.Errorf("id pattern %s", idPattern)
	}
	for _, id := range []string{"a", strings.Repeat("a", 64), "A", "-a", strings.Repeat("a", 65)} {
		if idPattern.MatchString(id) != msgkit.ValidID(id) {
			t.Errorf("id %q: harness %t, chain %t", id, idPattern.MatchString(id), msgkit.ValidID(id))
		}
	}
	for k := evidencev1.EvidenceKind(0); k < 7; k++ {
		if kindName(k) != evidence.KindName(k) {
			t.Errorf("kind %d: harness %q, chain %q", k, kindName(k), evidence.KindName(k))
		}
	}
}
