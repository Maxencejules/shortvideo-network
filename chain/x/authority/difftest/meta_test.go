package difftest

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// TestOracleSelfTest runs spec_oracle.py --self-test: the lease, evidence,
// and council traces, each council rule with the rule on and off.
func TestOracleSelfTest(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command(pythonPath(t, root), "-I", "-B", filepath.Join(root, oracleScript), "--self-test")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("spec_oracle.py --self-test: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "spec_oracle self-test: PASS") {
		t.Fatalf("unexpected self-test output: %s", out)
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
}

// passedFixture is one governed limit executed through the whole flow.
func passedFixture() *sequence {
	q := newSequence()
	q.pass("e1", governedPayload(msgkit.LimitMaxPageLimit, 50), 1, 10)
	return q
}

// TestComparatorDetectsMismatch perturbs one field of a real oracle answer
// (or of the Go outcome) at a time and requires compareStep to report it.
func TestComparatorDetectsMismatch(t *testing.T) {
	q := passedFixture()
	run := runFixture(t, t, defaultConfig(), q.steps, false, 1)
	if run.firstMismatch != -1 {
		t.Fatalf("fixture diverged at step %d", run.firstMismatch)
	}
	at := len(q.steps) - 1
	s, out, spec := q.steps[at], run.outs[at], run.specs[at]
	h := newHarness(t, defaultConfig())
	for _, step := range q.steps {
		h.apply(step)
	}
	proj := h.projection()
	if !compareStep(t, "unperturbed", s, out, &proj, spec) {
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
		{"code", func(r *specResult) { r.Code = string(msgkit.Conflict) }},
		{"idempotent flag", func(r *specResult) { r.Idempotent = true }},
		{"keep flag", func(r *specResult) { r.Keep = true }},
		{"event version", func(r *specResult) { r.Events[0].PolicyVersion = "1" }},
		{"event id", func(r *specResult) { r.Events[1].ID = "e-other" }},
		{"event dropped", func(r *specResult) { r.Events = r.Events[:1] }},
		{"policy version", func(r *specResult) { r.Projection.Council.PolicyVersion = "3" }},
		{"height", func(r *specResult) { r.Projection.Council.Height = "9" }},
		{"proposal state", func(r *specResult) { r.Projection.Council.Proposals[0].State = "QUEUED" }},
		{"approver", func(r *specResult) { r.Projection.Council.Proposals[0].Approvers[0] = "seat-4" }},
		{"execute after", func(r *specResult) { r.Projection.Council.Proposals[0].ExecuteAfterUnixSeconds = "1" }},
		{"executed digest", func(r *specResult) { r.Projection.Council.ExecutedParams[0].PayloadDigest = "00" }},
		{"governed value", func(r *specResult) { r.Projection.Council.GovernedLimits[0].Value = "51" }},
		{"validator power", func(r *specResult) { r.Projection.Council.Validators[0].Power = "11" }},
		{"pending added", func(r *specResult) {
			r.Projection.Council.PendingPower = append(r.Projection.Council.PendingPower, pendingView{ProposalID: "x"})
		}},
		{"business id dropped", func(r *specResult) { r.Projection.BusinessIDs = r.Projection.BusinessIDs[1:] }},
		{"provider state", func(r *specResult) { r.Projection.Providers[0].State = "DISABLED" }},
	}
	for _, m := range mutants {
		var mutated specResult
		if err := json.Unmarshal(raw, &mutated); err != nil {
			t.Fatal(err)
		}
		m.mutate(&mutated)
		rec := &recorder{TB: t}
		if compareStep(rec, m.name, s, out, &proj, mutated) || len(rec.errors) == 0 {
			t.Errorf("mutant %q was not detected", m.name)
		}
	}
	goMutant := out
	goMutant.keep = true
	rec := &recorder{TB: t}
	if compareStep(rec, "go keep", s, goMutant, &proj, spec) || len(rec.errors) == 0 {
		t.Errorf("a changed Go keep flag was not detected")
	}
	t.Logf("%d spec mutants and 1 Go mutant detected", len(mutants))
}

// TestTrialSnapshotDetectsEarlyWrite shows the reject check compares the
// trial branch of every store the council can touch: a handler wrapper
// that writes a key and then returns the real handler's result is reported
// for a rejected propose and for an idempotent execute replay, while the
// unwrapped handler is not. It is the mutant that proves the bytes check of
// FuzzGovernanceTransitions can fail. The idempotent replay commits its
// early write, so the mutant replay runs through deliver (the trial check)
// and the test then requires the I12 step check to report the committed
// write exactly when it went to the x/policy store.
func TestTrialSnapshotDetectsEarlyWrite(t *testing.T) {
	q := passedFixture()
	h := newHarness(t, defaultConfig())
	for _, s := range q.steps {
		if out := h.apply(s); out.code != msgkit.OK {
			t.Fatalf("setup %s: %s", s.op, out.code)
		}
	}
	rejected := buildPropose(proposeParams{
		signer: outsiderAcct, business: "bp-outsider", proposalID: "x1", base: 2, expiry: h.now + 100,
		payload: governedPayload(msgkit.LimitMaxPageLimit, 60),
	}, "")
	replay := q.steps[len(q.steps)-1]
	rec := &recorder{TB: t}
	h.tb = rec
	if out := h.apply(rejected); out.code != msgkit.Unauthorized || len(rec.errors) != 0 {
		t.Fatalf("control: code %s, reports %v", out.code, rec.errors)
	}
	if out := h.apply(replay); !out.idempotent || len(rec.errors) != 0 {
		t.Fatalf("control replay: idempotent %t, reports %v", out.idempotent, rec.errors)
	}
	for i, key := range h.keys {
		h.intercept = func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error {
			ctx.KVStore(key).Set([]byte("early-write"), []byte{1})
			return next(ctx, msg)
		}
		before := len(rec.errors)
		if out := h.apply(rejected); out.code != msgkit.Unauthorized {
			t.Fatalf("store %d mutant reject: code %s", i, out.code)
		}
		if len(rec.errors) != before+1 || !strings.Contains(rec.errors[before], "changed the trial state") {
			t.Fatalf("store %s: the early write of a rejected message was not reported: %v", key.Name(), rec.errors[before:])
		}
		policyBefore := policyRows(h.unmetered().KVStore(h.keys[storePolicy]))
		out := h.deliver(replay)
		if !out.idempotent {
			t.Fatalf("store %d mutant replay is not idempotent", i)
		}
		if len(rec.errors) != before+2 || !strings.Contains(rec.errors[before+1], "idempotent") {
			t.Fatalf("store %s: the write of an idempotent replay was not reported: %v", key.Name(), rec.errors[before:])
		}
		err := h.checkPolicyWrites(replay, out, nil, policyBefore, policyRows(h.unmetered().KVStore(h.keys[storePolicy])))
		if got := err != nil && strings.HasPrefix(err.Error(), "I12:"); got != (i == storePolicy) {
			t.Fatalf("store %s: the I12 step check returned %v", key.Name(), err)
		}
	}
}

// TestStoreLayout ties the prefixes the projection and the invariant
// checker read to the keepers' own surfaces: the business row is what
// BusinessDigest returns, the x/policy governed row what policy
// GovernedLimit returns, the policy row what PolicyVersion returns, the
// open count and the executed digest have the documented shapes, and the
// pending key is authority.PendingKey. The authority layout key holds
// layout version 2 and no authority row uses the retired governed prefix
// 0x13.
func TestStoreLayout(t *testing.T) {
	q := newSequence()
	q.pass("e1", governedPayload(msgkit.LimitMaxPageLimit, 50), 1, 10)
	q.pass("v9", validatorPayload("v9", consensusKey("v9"), 10, digest(sha("r")), atHeight(100)), 2, 10)
	q.propose(2, "o1", governedPayload(msgkit.LimitMaxOpenOffers, 7), 3)
	h := newHarness(t, defaultConfig())
	for _, s := range q.steps {
		if out := h.apply(s); out.code != msgkit.OK {
			t.Fatalf("setup %s: %s", s.op, out.code)
		}
	}
	ctx := h.unmetered()
	store := ctx.KVStore(h.keys[storeAuthority])
	for _, id := range []string{q.steps[0].spec["business_id"].(string), q.steps[len(q.steps)-1].spec["business_id"].(string)} {
		want, found := h.auth.BusinessDigest(ctx, id)
		if !found || !bytes.Equal(store.Get(append([]byte{prefixBusiness}, id...)), want) {
			t.Errorf("business row of %s does not match BusinessDigest", id)
		}
	}
	value, found := h.pol.GovernedLimit(ctx, msgkit.LimitMaxPageLimit)
	raw := ctx.KVStore(h.keys[storePolicy]).Get(append([]byte{prefixGoverned}, msgkit.LimitMaxPageLimit...))
	if !found || value != 50 || len(raw) != 8 || binary.BigEndian.Uint64(raw) != value {
		t.Errorf("policy governed row %x does not match GovernedLimit %d, %t", raw, value, found)
	}
	if layout, found, err := msgkit.ReadLayoutVersion(store); err != nil || !found || layout != authority.ConsensusVersion {
		t.Errorf("authority layout key %d (found %t, %v), want %d", layout, found, err, authority.ConsensusVersion)
	}
	eachRow(store, prefixRetiredGoverned, func(key, _ []byte) {
		t.Errorf("authority row %x under the retired governed prefix", key)
	})
	if raw := store.Get([]byte{prefixPolicy}); len(raw) != 8 || binary.BigEndian.Uint64(raw) != h.auth.PolicyVersion(ctx) {
		t.Errorf("policy row does not match PolicyVersion")
	}
	if raw := store.Get([]byte(openKey(seatID(2), 3))); len(raw) != 8 || binary.BigEndian.Uint64(raw) != 1 {
		t.Errorf("open count of seat-2 on base 3 is %x, want 1", raw)
	}
	digests := map[string][]byte{}
	for _, rec := range h.records(ctx) {
		digests[rec.ProposalID] = rec.PayloadDigest
	}
	if raw := store.Get(append([]byte{prefixExecuted}, msgkit.LimitMaxPageLimit...)); !bytes.Equal(raw, digests["e1"]) {
		t.Errorf("executed row of max_page_limit is %x, want e1's digest", raw)
	}
	pending := h.governance(ctx).GetPendingValidatorPower()
	if len(pending) != 1 || !store.Has(authority.PendingKey(pending[0])) || authority.PendingKey(pending[0])[0] != prefixPending {
		t.Errorf("pending entry is not stored under authority.PendingKey with prefix %#x", prefixPending)
	}
}

// TestCouncilInvariantsSelfTest injects one violation per invariant the
// checker covers, by writing the authority or x/policy store directly
// after a valid sequence (or by changing the harness's own record), and
// requires checkInvariants to name that invariant. The I3, I4 and I5 rows
// of x/policy's own checker (policytest) are injected here too, so a
// harness that stopped calling it fails.
func TestCouncilInvariantsSelfTest(t *testing.T) {
	setup := func(t *testing.T) *harness {
		q := newSequence()
		q.pass("e1", governedPayload(msgkit.LimitMaxPageLimit, 50), 1, 10)
		q.propose(2, "o1", governedPayload(msgkit.LimitMaxOpenOffers, 7), 2)
		q.approve(1, "o1")
		h := newHarness(t, defaultConfig())
		for _, s := range q.steps {
			if out := h.apply(s); out.code != msgkit.OK {
				t.Fatalf("setup %s: %s", s.op, out.code)
			}
		}
		if err := h.checkInvariants(h.unmetered()); err != nil {
			t.Fatalf("control: %v", err)
		}
		return h
	}
	rewriteConfig := func(h *harness, edit func(*authorityv1.GenesisState)) {
		ctx := h.unmetered()
		cfg := h.config(ctx)
		edit(cfg)
		raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(cfg)
		if err != nil {
			h.tb.Fatal(err)
		}
		ctx.KVStore(h.keys[storeAuthority]).Set([]byte{0x01}, raw)
	}
	rewriteRecord := func(h *harness, id, from, to string) {
		store := h.unmetered().KVStore(h.keys[storeAuthority])
		key := append([]byte{prefixProposal}, id...)
		raw := string(store.Get(key))
		if !strings.Contains(raw, from) {
			h.tb.Fatalf("record %s has no %s: %s", id, from, raw)
		}
		store.Set(key, []byte(strings.Replace(raw, from, to, 1)))
	}
	be := func(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
	cases := []struct {
		rule   string
		inject func(*harness)
	}{
		{"I1", func(h *harness) { h.unmetered().KVStore(h.keys[storeAuthority]).Set([]byte{prefixPolicy}, be(5)) }},
		{"I2", func(h *harness) {
			h.unmetered().KVStore(h.keys[storePolicy]).Set(append([]byte{prefixGoverned}, "foo"...), be(5))
		}},
		{"I7", func(h *harness) {
			rewriteConfig(h, func(c *authorityv1.GenesisState) { c.BlockedDecisions[0].Approved = true })
		}},
		{"I7", func(h *harness) {
			rewriteConfig(h, func(c *authorityv1.GenesisState) { c.DevnetTestParameters[3].DelaySeconds = 99 })
		}},
		{"I8", func(h *harness) {
			h.unmetered().KVStore(h.keys[storeAuthority]).Set(append([]byte{prefixRetired}, seatPub(1)...), []byte(seatID(1)))
		}},
		{"I8", func(h *harness) {
			rewriteConfig(h, func(c *authorityv1.GenesisState) {
				c.Authority.DevnetTestSeats[0].PublicKey = consensusKey(nodeID(1))
			})
		}},
		{"I9", func(h *harness) { rewriteRecord(h, "o1", `"approvers":["seat-1"]`, `"approvers":["seat-1","seat-1"]`) }},
		{"I10", func(h *harness) {
			h.unmetered().KVStore(h.keys[storeAuthority]).Set([]byte(openKey(seatID(2), 2)), be(2))
		}},
		{"I10", func(h *harness) { h.proposers["o1"] = seatID(3) }},
		{"I11", func(h *harness) {
			rewriteConfig(h, func(c *authorityv1.GenesisState) { c.Authority.Validators[0].Power = 1000 })
		}},
		// I3, state form: a pending row that did not activate at its height.
		{"I3", func(h *harness) {
			policyStore(h).Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), policy.EncodePending(policy.Pending{
				Activation: uint64(h.height), Value: 5, PolicyVersion: 2, ProposalID: "e1",
			}))
		}},
		// I3, exact form: a governed value no executed decision set.
		{"I3", func(h *harness) { policyStore(h).Set(policy.GovernedKey(msgkit.LimitMaxPageLimit), be(60)) }},
		{"I3", func(h *harness) { delete(h.decisions, msgkit.LimitMaxPageLimit) }},
		// I4: a plan at or below the current height.
		{"I4", func(h *harness) {
			policyStore(h).Set(policy.PlanKey(), policy.EncodePlan(policy.Plan{
				Name: "up-1", Height: uint64(h.height), Digest: sha("release"), ProposalID: "e1",
			}))
		}},
		// I5 (policytest): the app version is not 1 + the applied upgrades.
		{"I5", func(h *harness) { policyStore(h).Set(policy.AppVersionKey(), be(2)) }},
		// I5 (keeper part): a consistent applied upgrade, so the stored app
		// version (2) is not the binary's (1).
		{"I5", func(h *harness) {
			store := policyStore(h)
			store.Set(policy.AppliedKey("up-1"), be(1))
			store.Set(policy.PlanNameKey("up-1"), sha("release"))
			store.Set(policy.AppVersionKey(), be(2))
		}},
		// I6: the persisted version map is not the binary's.
		{"I6", func(h *harness) { policyStore(h).Set(policy.VersionMapKey(providers.ModuleName), be(3)) }},
		// I6: a module's layout key is not its version map entry.
		{"I6", func(h *harness) {
			msgkit.WriteLayoutVersion(h.unmetered().KVStore(h.keys[storeContent]), 3)
		}},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			h := setup(t)
			c.inject(h)
			err := h.checkInvariants(h.unmetered())
			if err == nil || !strings.HasPrefix(err.Error(), c.rule+":") {
				t.Fatalf("injected %s violation: checker returned %v", c.rule, err)
			}
		})
	}
}

func policyStore(h *harness) storetypes.KVStore { return h.unmetered().KVStore(h.keys[storePolicy]) }

// TestPolicyWriteCheckSelfTest shows the I12 step check (checkPolicyWrites)
// fails where it should: a registry transaction or a governed execute
// whose handler also writes another policy row, a block that changes a
// governed value with nothing due, a block whose due change of one key
// also changes another key, and a block that applies a change not yet
// due. The control rows pass: a plain governed execute (the setup's e1,
// which apply checks), and a block that applies exactly the due key's
// rows. The live rows run through deliver, after the last apply, because
// they commit a write the harness's bookkeeping does not know.
func TestPolicyWriteCheckSelfTest(t *testing.T) {
	q := newSequence()
	q.pass("e1", governedPayload(msgkit.LimitMaxPageLimit, 50), 1, 10)
	q.pass("e2", governedPayload(msgkit.LimitMaxPageLimit, 60), 2, 10)
	h := newHarness(t, defaultConfig())
	for _, s := range q.steps[:len(q.steps)-1] {
		if out := h.apply(s); out.code != msgkit.OK {
			t.Fatalf("setup %s: %s", s.op, out.code)
		}
	}
	be := func(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
	execute := q.steps[len(q.steps)-1]
	register := buildRegisterProvider(providerAccount(0), "bpr-1", "pr-1")
	extra := string(policy.GovernedKey(msgkit.LimitMaxOpenOffers))
	h.intercept = func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error {
		ctx.KVStore(h.keys[storePolicy]).Set([]byte(extra), be(7))
		return next(ctx, msg)
	}
	for _, live := range []struct {
		s       *step
		payload *authorityv1.ProposalPayload
	}{{register, nil}, {execute, h.payloads["e2"]}} {
		before := policyRows(policyStore(h))
		out := h.deliver(live.s)
		if out.code != msgkit.OK {
			t.Fatalf("%s: %s", live.s.op, out.code)
		}
		err := h.checkPolicyWrites(live.s, out, live.payload, before, policyRows(policyStore(h)))
		if err == nil || !strings.HasPrefix(err.Error(), "I12:") || !strings.Contains(err.Error(), hexOf([]byte(extra))) {
			t.Fatalf("%s with an extra policy write: I12 check returned %v", live.s.op, err)
		}
		policyStore(h).Delete([]byte(extra))
	}
	h.intercept = nil
	pending := func(activation uint64) []byte {
		return policy.EncodePending(policy.Pending{Activation: activation, Value: 5, PolicyVersion: 2, ProposalID: "e1"})
	}
	due, other := msgkit.LimitMaxPageLimit, msgkit.LimitMaxOpenOffers
	block := blockStep(h.now + 1)
	ok := outcome{code: msgkit.OK}
	rows := func(pairs ...string) map[string][]byte {
		m := map[string][]byte{}
		for i := 0; i < len(pairs); i += 2 {
			m[pairs[i]] = []byte(pairs[i+1])
		}
		return m
	}
	duePending := string(pending(uint64(h.height)))
	cases := []struct {
		name          string
		before, after map[string][]byte
		fails         bool
	}{
		{"nothing due, a governed value changes", rows(), rows(string(policy.GovernedKey(other)), "x"), true},
		{"a due change applies", rows(string(policy.PendingKey(due)), duePending),
			rows(string(policy.GovernedKey(due)), string(be(5))), false},
		{"a due change applies and another key changes", rows(string(policy.PendingKey(due)), duePending),
			rows(string(policy.GovernedKey(due)), string(be(5)), string(policy.GovernedKey(other)), "x"), true},
		{"a change due later applies", rows(string(policy.PendingKey(due)), string(pending(uint64(h.height)+1))),
			rows(string(policy.GovernedKey(due)), string(be(5))), true},
	}
	for _, c := range cases {
		err := h.checkPolicyWrites(block, ok, nil, c.before, c.after)
		if fails := err != nil && strings.HasPrefix(err.Error(), "I12:"); fails != c.fails {
			t.Errorf("%s: I12 check returned %v, want a failure %t", c.name, err, c.fails)
		}
	}
}
