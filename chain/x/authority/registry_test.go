package authority

import (
	"bytes"
	"encoding/hex"
	"slices"
	"testing"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// registryCall is one call authority made into the registry.
type registryCall struct {
	Method  string
	ID      string
	Version uint64
	Reason  []byte
	Devnet  bool
}

// fakeRegistry records calls and returns a fixed outcome per method. It is
// a unit-test double for the x/providers and x/content hooks.
type fakeRegistry struct {
	calls []registryCall
	out   map[string]Outcome
}

func (f *fakeRegistry) result(method string) Outcome {
	return f.out[method]
}

func (f *fakeRegistry) Admit(id string, devnet bool) Outcome {
	f.calls = append(f.calls, registryCall{Method: "admit", ID: id, Devnet: devnet})
	return f.result("admit")
}

func (f *fakeRegistry) Disable(id string) Outcome {
	f.calls = append(f.calls, registryCall{Method: "disable", ID: id})
	return f.result("disable")
}

func (f *fakeRegistry) ForceWithdraw(id string, version uint64, reason []byte, devnet bool) Outcome {
	f.calls = append(f.calls, registryCall{
		Method: "force_withdraw", ID: id, Version: version, Reason: append([]byte(nil), reason...), Devnet: devnet,
	})
	// The engine hands over a copy. Scribbling on it must not reach the
	// stored decoded payload.
	for i := range reason {
		reason[i] ^= 0xff
	}
	return f.result("force_withdraw")
}

func sameCalls(got, want []registryCall) bool {
	return slices.EqualFunc(got, want, func(a, b registryCall) bool {
		return a.Method == b.Method && a.ID == b.ID && a.Version == b.Version &&
			bytes.Equal(a.Reason, b.Reason) && a.Devnet == b.Devnet
	})
}

func sameValidators(a, b []*authorityv1.ValidatorPower) bool {
	return slices.EqualFunc(a, b, func(x, y *authorityv1.ValidatorPower) bool {
		return x.GetNodeId() == y.GetNodeId() && x.GetPower() == y.GetPower() &&
			bytes.Equal(x.GetConsensusPubkey(), y.GetConsensusPubkey())
	})
}

func sha256Digest(value []byte) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: value}
}

func payloadProposal(payload *authorityv1.ProposalPayload, key string) *authorityv1.MsgProposeChange {
	digest, out := PayloadDigest(payload)
	if !out.Success() {
		panic(out.Code)
	}
	return &authorityv1.MsgProposeChange{ParameterKey: key, Payload: payload, PayloadDigest: sha256Digest(digest)}
}

func admitProposal(id string) *authorityv1.MsgProposeChange {
	return payloadProposal(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{
		AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: id},
	}}, "admit-"+id)
}

func disableProposal(id string) *authorityv1.MsgProposeChange {
	return payloadProposal(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_DisableProvider{
		DisableProvider: &authorityv1.DisableProviderChange{ProviderId: id},
	}}, "disable-"+id)
}

func withdrawProposal(id string, version uint64, reason []byte) *authorityv1.MsgProposeChange {
	return payloadProposal(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ForceWithdraw{
		ForceWithdraw: &authorityv1.ForceWithdrawChange{ContentId: id, ContentVersion: version, ReasonDigest: sha256Digest(reason)},
	}}, "withdraw-"+id)
}

func devnetParam(decision, role string) *typesv1.DevnetTestParameter {
	return &typesv1.DevnetTestParameter{Label: "PROPOSED_TEST_PARAMETER", DecisionId: decision, RoleName: role}
}

// queueProposal opens id and collects three seat approvals.
func queueProposal(t *testing.T, st *State, seats [][]byte, id string, msg *authorityv1.MsgProposeChange) {
	t.Helper()
	if out := st.ProposeChange(propose(seats[0], id, st.PolicyVersion, msg)); !out.Success() {
		t.Fatalf("propose %s: %+v", id, out)
	}
	for i := 0; i < ApprovedThreshold; i++ {
		if out := st.ApproveChange(approve(seats[i], "ap-"+id+"-"+string(rune('a'+i)), id)); !out.Success() {
			t.Fatalf("approve %s: %+v", id, out)
		}
	}
	if st.proposal(id).GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatalf("%s is %v", id, st.proposal(id).GetState())
	}
}

// executeTrial runs ExecuteChange the way Keeper.transition does: on a
// clone, committing the clone only on success or keep. It returns the
// committed state, the clone the engine ran on, and the outcome. On a
// failure the committed state is st itself, untouched by construction, so
// a test of "a failure changes nothing" must look at the clone: it proves
// the engine wrote nothing before it failed, not only that the keeper
// discards the clone.
func executeTrial(st *State, signer []byte, businessID, proposalID string) (committed, trial *State, out Outcome) {
	trial = st.clone()
	out = trial.ExecuteChange(execute(signer, businessID, proposalID))
	if out.Success() || out.Keep {
		return trial, trial, out
	}
	return st, trial, out
}

var reasonValue = func() []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = byte(0xa0 + i)
	}
	return out
}()

func TestAdmitPassesTheDEC002Flag(t *testing.T) {
	cases := []struct {
		name   string
		params []*typesv1.DevnetTestParameter
		devnet bool
	}{
		{"devnet test role", []*typesv1.DevnetTestParameter{devnetParam("DEC-002", "DEVNET_TEST_ONLY")}, true},
		{"missing", nil, false},
		{"other role", []*typesv1.DevnetTestParameter{devnetParam("DEC-002", "OPERATOR")}, false},
		{"empty role", []*typesv1.DevnetTestParameter{devnetParam("DEC-002", "")}, false},
		{"wrong label", []*typesv1.DevnetTestParameter{{Label: "APPROVED", DecisionId: "DEC-002", RoleName: "DEVNET_TEST_ONLY"}}, false},
		{"approved policy", []*typesv1.DevnetTestParameter{{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-002", RoleName: "DEVNET_TEST_ONLY", ApprovedPolicy: true}}, false},
		{"only DEC-003", []*typesv1.DevnetTestParameter{devnetParam("DEC-003", "DEVNET_TEST_ONLY")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, seats, authoritySigner := readyState()
			st.Params = append(st.Params, tc.params...)
			registry := &fakeRegistry{}
			if !tc.devnet {
				// x/providers answers DECISION_BLOCKED when the flag is false.
				registry.out = map[string]Outcome{"admit": fail(CodeDecisionBlocked)}
			}
			st.Registry = registry
			queueProposal(t, st, seats, "admit-p1", admitProposal("p1"))
			before := canonicalBytes(t, st)
			after, trial, out := executeTrial(st, authoritySigner, "ex-admit", "admit-p1")
			if !sameCalls(registry.calls, []registryCall{{Method: "admit", ID: "p1", Devnet: tc.devnet}}) {
				t.Fatalf("calls %+v", registry.calls)
			}
			if tc.devnet {
				if !out.Success() || after.proposal("admit-p1").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
					t.Fatalf("%+v %v", out, after.proposal("admit-p1").GetState())
				}
				return
			}
			if out.Code != CodeDecisionBlocked || out.Keep {
				t.Fatalf("%+v", out)
			}
			if !bytes.Equal(before, canonicalBytes(t, trial)) {
				t.Fatal("blocked admission changed authority state")
			}
			if trial.proposal("admit-p1").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				t.Fatal(trial.proposal("admit-p1").GetState())
			}
		})
	}
}

func TestDisablePassesTheProviderID(t *testing.T) {
	st, seats, authoritySigner := readyState()
	registry := &fakeRegistry{}
	st.Registry = registry
	queueProposal(t, st, seats, "disable-p7", disableProposal("p7"))
	validators := cloneValidators(st.Validators)
	version := st.PolicyVersion
	after, _, out := executeTrial(st, authoritySigner, "ex-disable", "disable-p7")
	if !out.Success() {
		t.Fatal(out)
	}
	if !sameCalls(registry.calls, []registryCall{{Method: "disable", ID: "p7"}}) {
		t.Fatalf("calls %+v", registry.calls)
	}
	if after.PolicyVersion != version+1 || !sameValidators(after.Validators, validators) || len(after.Pending) != 0 {
		t.Fatal("disable changed the validator set or missed the policy version")
	}
	if after.proposal("disable-p7").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(after.proposal("disable-p7").GetState())
	}
}

func TestForceWithdrawPassesTheDecodedPayload(t *testing.T) {
	cases := []struct {
		name   string
		params []*typesv1.DevnetTestParameter
		devnet bool
	}{
		{"devnet test role", []*typesv1.DevnetTestParameter{devnetParam("DEC-003", "DEVNET_TEST_ONLY")}, true},
		{"missing", nil, false},
		{"only DEC-002", []*typesv1.DevnetTestParameter{devnetParam("DEC-002", "DEVNET_TEST_ONLY")}, false},
		{"other role", []*typesv1.DevnetTestParameter{devnetParam("DEC-003", "MODERATOR")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, seats, authoritySigner := readyState()
			st.Params = append(st.Params, tc.params...)
			registry := &fakeRegistry{}
			st.Registry = registry
			queueProposal(t, st, seats, "withdraw-c1", withdrawProposal("c1", 1, reasonValue))
			stored := st.Decoded["withdraw-c1"]
			if stored.Kind != "force_withdraw" || stored.ContentID != "c1" || stored.ContentVersion != 1 || !bytes.Equal(stored.Reason, reasonValue) {
				t.Fatalf("stored %+v", stored)
			}
			after, _, out := executeTrial(st, authoritySigner, "ex-withdraw", "withdraw-c1")
			if !out.Success() {
				t.Fatal(out)
			}
			want := []registryCall{{Method: "force_withdraw", ID: "c1", Version: 1, Reason: reasonValue, Devnet: tc.devnet}}
			if !sameCalls(registry.calls, want) {
				t.Fatalf("calls %+v want %+v", registry.calls, want)
			}
			if !bytes.Equal(after.Decoded["withdraw-c1"].Reason, reasonValue) {
				t.Fatal("the registry reached the stored reason digest")
			}
		})
	}
}

func TestForceWithdrawPassesTheProposedVersion(t *testing.T) {
	// The payload version reaches the registry unchanged. x/content
	// compares it with the record and answers STALE_VERSION.
	st, seats, authoritySigner := readyState()
	st.Params = append(st.Params, devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
	registry := &fakeRegistry{out: map[string]Outcome{"force_withdraw": fail(CodeStale)}}
	st.Registry = registry
	queueProposal(t, st, seats, "withdraw-c9", withdrawProposal("c9", 2, reasonValue))
	before := canonicalBytes(t, st)
	_, trial, out := executeTrial(st, authoritySigner, "ex-c9", "withdraw-c9")
	if out.Code != CodeStale || out.Keep {
		t.Fatalf("%+v", out)
	}
	if !sameCalls(registry.calls, []registryCall{{Method: "force_withdraw", ID: "c9", Version: 2, Reason: reasonValue, Devnet: true}}) {
		t.Fatalf("calls %+v", registry.calls)
	}
	if !bytes.Equal(before, canonicalBytes(t, trial)) {
		t.Fatal("stale force-withdraw changed authority state")
	}
}

func TestRegistryForbiddenRejectsAndKeeps(t *testing.T) {
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		t.Run(kind, func(t *testing.T) {
			st, seats, authoritySigner := readyState()
			st.Params = append(st.Params, devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
			registry := &fakeRegistry{out: map[string]Outcome{kind: fail(CodeForbidden)}}
			st.Registry = registry
			id, msg := registryProposal(kind)
			queueProposal(t, st, seats, id, msg)
			version := st.PolicyVersion
			after, _, out := executeTrial(st, authoritySigner, "ex-"+id, id)
			if out.Code != CodeForbidden || !out.Keep {
				t.Fatalf("%+v", out)
			}
			if after.proposal(id).GetState() != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED {
				t.Fatal(after.proposal(id).GetState())
			}
			if after.PolicyVersion != version || after.Executed[after.proposal(id).GetParameterKey()] != nil {
				t.Fatal("a rejected proposal counted as executed")
			}
			if _, committed := after.Business["ex-"+id]; committed {
				t.Fatal("a rejected execution committed its business id")
			}
			if len(registry.calls) != 1 {
				t.Fatalf("calls %+v", registry.calls)
			}
			// A second execute of a REJECTED proposal is a conflict and does
			// not reach the registry.
			_, _, again := executeTrial(after, authoritySigner, "ex2-"+id, id)
			if again.Code != CodeConflict || len(registry.calls) != 1 {
				t.Fatalf("again %+v calls %d", again, len(registry.calls))
			}
		})
	}
}

func registryProposal(kind string) (string, *authorityv1.MsgProposeChange) {
	switch kind {
	case "admit":
		return "admit-p1", admitProposal("p1")
	case "disable":
		return "disable-p1", disableProposal("p1")
	default:
		return "withdraw-c1", withdrawProposal("c1", 1, reasonValue)
	}
}

func TestRegistryFailureLeavesTheProposalQueued(t *testing.T) {
	codes := []string{CodeNotFound, CodeDecisionBlocked, CodeConflict, CodeWithdrawn, CodeStale, "ERROR_CODE_WRONG_DOMAIN"}
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		for _, code := range codes {
			t.Run(kind+"/"+code, func(t *testing.T) {
				st, seats, authoritySigner := readyState()
				st.Params = append(st.Params, devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
				registry := &fakeRegistry{out: map[string]Outcome{kind: fail(code)}}
				st.Registry = registry
				id, msg := registryProposal(kind)
				queueProposal(t, st, seats, id, msg)
				before := canonicalBytes(t, st)
				after, trial, out := executeTrial(st, authoritySigner, "ex-"+id, id)
				if out.Code != code || out.Keep || out.Idempotent {
					t.Fatalf("%+v", out)
				}
				if !bytes.Equal(before, canonicalBytes(t, trial)) {
					t.Fatal("a registry failure changed authority state")
				}
				if trial.proposal(id).GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
					t.Fatal(trial.proposal(id).GetState())
				}
				// The same proposal can execute once the registry accepts.
				registry.out = nil
				final, _, retry := executeTrial(after, authoritySigner, "ex-retry-"+id, id)
				if !retry.Success() || final.proposal(id).GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
					t.Fatalf("retry %+v", retry)
				}
				if len(registry.calls) != 2 {
					t.Fatalf("calls %+v", registry.calls)
				}
			})
		}
	}
}

func TestRegistryCannotAskForKeepOrIdempotent(t *testing.T) {
	st, seats, authoritySigner := readyState()
	st.Params = append(st.Params, devnetParam("DEC-002", "DEVNET_TEST_ONLY"))
	registry := &fakeRegistry{out: map[string]Outcome{"admit": {Code: CodeConflict, Keep: true}}}
	st.Registry = registry
	queueProposal(t, st, seats, "admit-p1", admitProposal("p1"))
	before := canonicalBytes(t, st)
	_, trial, out := executeTrial(st, authoritySigner, "ex-1", "admit-p1")
	if out.Code != CodeConflict || out.Keep {
		t.Fatalf("a registry keep leaked: %+v", out)
	}
	if !bytes.Equal(before, canonicalBytes(t, trial)) {
		t.Fatal("state changed")
	}
	registry.out = map[string]Outcome{"admit": {Idempotent: true}}
	final, _, out := executeTrial(st, authoritySigner, "ex-2", "admit-p1")
	if !out.Success() || out.Idempotent {
		t.Fatalf("a registry idempotent leaked: %+v", out)
	}
	if final.proposal("admit-p1").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(final.proposal("admit-p1").GetState())
	}
}

func TestNilRegistryIsMalformed(t *testing.T) {
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		t.Run(kind, func(t *testing.T) {
			st, seats, authoritySigner := readyState()
			st.Params = append(st.Params, devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
			id, msg := registryProposal(kind)
			queueProposal(t, st, seats, id, msg)
			before := canonicalBytes(t, st)
			_, trial, out := executeTrial(st, authoritySigner, "ex-"+id, id)
			if out.Code != CodeMalformed || out.Keep {
				t.Fatalf("%+v", out)
			}
			if !bytes.Equal(before, canonicalBytes(t, trial)) || trial.proposal(id).GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				t.Fatal("a missing registry changed authority state")
			}
		})
	}
	// Validator and governed-limit payloads do not need a registry.
	st, seats, authoritySigner := readyState()
	queueProposal(t, st, seats, "p-add", powerMsg("v5", pub(9), 1, 20))
	if _, _, out := executeTrial(st, authoritySigner, "ex-add", "p-add"); !out.Success() {
		t.Fatal(out)
	}
}

func TestExpiredRegistryProposalDoesNotCallTheRegistry(t *testing.T) {
	st, seats, authoritySigner := readyState()
	st.Params = append(st.Params, devnetParam("DEC-002", "DEVNET_TEST_ONLY"))
	registry := &fakeRegistry{}
	st.Registry = registry
	msg := admitProposal("p1")
	if out := st.ProposeChange(propose(seats[0], "admit-p1", 1, msg)); !out.Success() {
		t.Fatal(out)
	}
	st.proposal("admit-p1").ExpiryUnixSeconds = st.Time + 5
	for i := 0; i < ApprovedThreshold; i++ {
		if out := st.ApproveChange(approve(seats[i], "ap-"+string(rune('a'+i)), "admit-p1")); !out.Success() {
			t.Fatal(out)
		}
	}
	st.Time += 5
	after, _, out := executeTrial(st, authoritySigner, "ex-late", "admit-p1")
	if out.Code != CodeExpired || !out.Keep {
		t.Fatalf("%+v", out)
	}
	if after.proposal("admit-p1").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED || len(registry.calls) != 0 {
		t.Fatalf("state %v calls %+v", after.proposal("admit-p1").GetState(), registry.calls)
	}
}

func TestExternalBusinessIDIsReplay(t *testing.T) {
	foreign := bytes.Repeat([]byte{0x5a}, 32)
	external := func(lookups *[]string) func(string) ([]byte, bool) {
		return func(id string) ([]byte, bool) {
			*lookups = append(*lookups, id)
			if id == "shared" {
				return append([]byte(nil), foreign...), true
			}
			return nil, false
		}
	}

	t.Run("propose", func(t *testing.T) {
		st, seats, _ := readyState()
		var lookups []string
		st.External = external(&lookups)
		before := canonicalBytes(t, st)
		msg := propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))
		msg.BusinessId = "shared"
		trial := st.clone()
		if out := trial.ProposeChange(msg); out.Code != CodeReplay {
			t.Fatalf("%+v", out)
		}
		if !bytes.Equal(before, canonicalBytes(t, trial)) || !slices.Equal(lookups, []string{"shared"}) {
			t.Fatalf("lookups %v", lookups)
		}
	})

	t.Run("approve", func(t *testing.T) {
		st, seats, _ := readyState()
		var lookups []string
		st.External = external(&lookups)
		if out := st.ProposeChange(propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))); !out.Success() {
			t.Fatal(out)
		}
		before := canonicalBytes(t, st)
		trial := st.clone()
		if out := trial.ApproveChange(approve(seats[1], "shared", "p-add")); out.Code != CodeReplay {
			t.Fatalf("%+v", out)
		}
		if len(trial.proposal("p-add").GetApproverKeys()) != 0 || !bytes.Equal(before, canonicalBytes(t, trial)) {
			t.Fatal("a replayed approval was recorded")
		}
	})

	t.Run("execute", func(t *testing.T) {
		st, seats, authoritySigner := readyState()
		var lookups []string
		st.External = external(&lookups)
		queueProposal(t, st, seats, "p-add", powerMsg("v5", pub(9), 1, 20))
		before := canonicalBytes(t, st)
		_, trial, out := executeTrial(st, authoritySigner, "shared", "p-add")
		if out.Code != CodeReplay || out.Keep {
			t.Fatalf("%+v", out)
		}
		if !bytes.Equal(before, canonicalBytes(t, trial)) || trial.proposal("p-add").GetState() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatal("a replayed execute changed authority state")
		}
	})

	t.Run("own ledger first", func(t *testing.T) {
		st, seats, _ := readyState()
		var lookups []string
		st.External = external(&lookups)
		msg := propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))
		if out := st.ProposeChange(msg); !out.Success() {
			t.Fatal(out)
		}
		again := propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))
		if out := st.ProposeChange(again); !out.Idempotent {
			t.Fatalf("%+v", out)
		}
		// The first propose consulted peers once; the repeat found its own
		// row and did not.
		if !slices.Equal(lookups, []string{"b-p-add"}) {
			t.Fatalf("lookups %v", lookups)
		}
	})

	t.Run("same digest is idempotent", func(t *testing.T) {
		// One namespace: a peer row with the exact digest of this request
		// is the same request already committed.
		st, seats, _ := readyState()
		if out := st.ProposeChange(propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))); !out.Success() {
			t.Fatal(out)
		}
		want := businessDigest(map[string]any{"proposal_id": "p-add", "signer": hex.EncodeToString(seats[1])})
		st.External = func(id string) ([]byte, bool) {
			if id == "elsewhere" {
				return append([]byte(nil), want...), true
			}
			return nil, false
		}
		before := canonicalBytes(t, st)
		out := st.ApproveChange(approve(seats[1], "elsewhere", "p-add"))
		if !out.Idempotent || out.Code != "" {
			t.Fatalf("%+v", out)
		}
		if !bytes.Equal(before, canonicalBytes(t, st)) {
			t.Fatal("an idempotent approval wrote")
		}
	})

	t.Run("malformed id never reaches peers", func(t *testing.T) {
		st, seats, _ := readyState()
		var lookups []string
		st.External = external(&lookups)
		msg := propose(seats[0], "p-add", 1, powerMsg("v5", pub(9), 1, 20))
		msg.BusinessId = "Shared"
		if out := st.ProposeChange(msg); out.Code != CodeBounds {
			t.Fatalf("%+v", out)
		}
		msg.BusinessId = ""
		if out := st.ProposeChange(msg); out.Code != CodeMalformed {
			t.Fatalf("%+v", out)
		}
		if len(lookups) != 0 {
			t.Fatalf("lookups %v", lookups)
		}
	})
}

func TestDevnetRoleRule(t *testing.T) {
	st, _, _ := readyState()
	st.Params = append(st.Params, devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "OTHER"))
	if !st.devnetRole("DEC-002") || st.devnetRole("DEC-003") || st.devnetRole("DEC-001") {
		t.Fatal("devnet role rule")
	}
	st.ChainID = "shortvideo-beta-1"
	if st.devnetRole("DEC-002") {
		t.Fatal("a non-devnet chain id used a devnet test parameter")
	}
}
