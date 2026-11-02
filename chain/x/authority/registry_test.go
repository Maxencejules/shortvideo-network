package authority

import (
	"bytes"
	"encoding/hex"
	"slices"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

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

func admitProposal(id string) *authorityv1.MsgProposeChange {
	return payloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{
		AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: id},
	}}, "admit-"+id)
}

func disableProposal(id string) *authorityv1.MsgProposeChange {
	return payloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_DisableProvider{
		DisableProvider: &authorityv1.DisableProviderChange{ProviderId: id},
	}}, "disable-"+id)
}

func withdrawProposal(id string, version uint64, reason []byte) *authorityv1.MsgProposeChange {
	return payloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ForceWithdraw{
		ForceWithdraw: &authorityv1.ForceWithdrawChange{ContentId: id, ContentVersion: version, ReasonDigest: sha256Digest(reason)},
	}}, "withdraw-"+id)
}

var reasonValue = func() []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = byte(0xa0 + i)
	}
	return out
}()

// registryKV is readyGenesis plus params, written by InitStore.
func registryKV(t testing.TB, params ...*typesv1.DevnetTestParameter) (storetypes.KVStore, []testSeat, []byte) {
	t.Helper()
	gen, seats, authority := readyGenesis()
	gen.DevnetTestParameters = append(gen.DevnetTestParameters, params...)
	return storeFrom(t, gen), seats, authority
}

// executeWith runs ExecuteChange on kv at the test height and the given unix
// time, with reg bound as Keeper.transition binds it (nil: no registry). The
// engine writes kv only on a success or keep, so a test of "a failure writes
// nothing" compares dumpKV(kv) before and after.
func executeWith(t testing.TB, kv KV, reg Registry, unix int64, signer []byte, business, proposal string) Outcome {
	t.Helper()
	st := loadAt(t, kv, testHeight, unix)
	st.Registry = reg
	return st.ExecuteChange(execute(signer, business, proposal))
}

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
			kv, seats, authoritySigner := registryKV(t, tc.params...)
			registry := &fakeRegistry{}
			if !tc.devnet {
				// x/providers answers DECISION_BLOCKED when the flag is false.
				registry.out = map[string]Outcome{"admit": fail(CodeDecisionBlocked)}
			}
			queueAt(t, kv, seats, testHeight, testTime, "admit-p1", admitProposal("p1"))
			before := dumpKV(kv)
			out := executeWith(t, kv, registry, testTime, authoritySigner, "ex-admit", "admit-p1")
			if !sameCalls(registry.calls, []registryCall{{Method: "admit", ID: "p1", Devnet: tc.devnet}}) {
				t.Fatalf("calls %+v", registry.calls)
			}
			if tc.devnet {
				if !out.Success() || recordOf(t, kv, "admit-p1").state() != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
					t.Fatalf("%+v %v", out, recordOf(t, kv, "admit-p1").state())
				}
				return
			}
			if out.Code != CodeDecisionBlocked || out.Keep {
				t.Fatalf("%+v", out)
			}
			wantUnchanged(t, kv, before, "blocked admission")
			if got := recordOf(t, kv, "admit-p1").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				t.Fatal(got)
			}
		})
	}
}

func TestDisablePassesTheProviderID(t *testing.T) {
	kv, seats, authoritySigner := registryKV(t)
	registry := &fakeRegistry{}
	queueAt(t, kv, seats, testHeight, testTime, "disable-p7", disableProposal("p7"))
	st := loadAt(t, kv, testHeight, testTime)
	validators, version := st.Validators(), st.PolicyVersion()
	if out := executeWith(t, kv, registry, testTime, authoritySigner, "ex-disable", "disable-p7"); !out.Success() {
		t.Fatal(out)
	}
	if !sameCalls(registry.calls, []registryCall{{Method: "disable", ID: "p7"}}) {
		t.Fatalf("calls %+v", registry.calls)
	}
	after := loadAt(t, kv, testHeight, testTime)
	if after.PolicyVersion() != version+1 || !sameValidators(after.Validators(), validators) || countPrefix(kv, pendingPrefix) != 0 {
		t.Fatal("disable changed the validator set or missed the policy version")
	}
	if got := recordOf(t, kv, "disable-p7").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(got)
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
			kv, seats, authoritySigner := registryKV(t, tc.params...)
			registry := &fakeRegistry{}
			queueAt(t, kv, seats, testHeight, testTime, "withdraw-c1", withdrawProposal("c1", 1, reasonValue))
			stored := recordOf(t, kv, "withdraw-c1").Decoded
			if stored.Kind != kindForceWithdraw || stored.ContentID != "c1" || stored.ContentVersion != 1 || !bytes.Equal(stored.Reason, reasonValue) {
				t.Fatalf("stored %+v", stored)
			}
			if out := executeWith(t, kv, registry, testTime, authoritySigner, "ex-withdraw", "withdraw-c1"); !out.Success() {
				t.Fatal(out)
			}
			want := []registryCall{{Method: "force_withdraw", ID: "c1", Version: 1, Reason: reasonValue, Devnet: tc.devnet}}
			if !sameCalls(registry.calls, want) {
				t.Fatalf("calls %+v want %+v", registry.calls, want)
			}
			if !bytes.Equal(recordOf(t, kv, "withdraw-c1").Decoded.Reason, reasonValue) {
				t.Fatal("the registry reached the stored reason digest")
			}
		})
	}
}

func TestForceWithdrawPassesTheProposedVersion(t *testing.T) {
	// The payload version reaches the registry unchanged. x/content
	// compares it with the record and answers STALE_VERSION.
	kv, seats, authoritySigner := registryKV(t, devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
	registry := &fakeRegistry{out: map[string]Outcome{"force_withdraw": fail(CodeStale)}}
	queueAt(t, kv, seats, testHeight, testTime, "withdraw-c9", withdrawProposal("c9", 2, reasonValue))
	before := dumpKV(kv)
	out := executeWith(t, kv, registry, testTime, authoritySigner, "ex-c9", "withdraw-c9")
	if out.Code != CodeStale || out.Keep {
		t.Fatalf("%+v", out)
	}
	if !sameCalls(registry.calls, []registryCall{{Method: "force_withdraw", ID: "c9", Version: 2, Reason: reasonValue, Devnet: true}}) {
		t.Fatalf("calls %+v", registry.calls)
	}
	wantUnchanged(t, kv, before, "stale force-withdraw")
}

func TestRegistryForbiddenRejectsAndKeeps(t *testing.T) {
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		t.Run(kind, func(t *testing.T) {
			kv, seats, authoritySigner := registryKV(t, devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
			registry := &fakeRegistry{out: map[string]Outcome{kind: fail(CodeForbidden)}}
			id, msg := registryProposal(kind)
			queueAt(t, kv, seats, testHeight, testTime, id, msg)
			version := loadAt(t, kv, testHeight, testTime).PolicyVersion()
			out := executeWith(t, kv, registry, testTime, authoritySigner, "ex-"+id, id)
			if out.Code != CodeForbidden || !out.Keep {
				t.Fatalf("%+v", out)
			}
			rec := recordOf(t, kv, id)
			if rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED {
				t.Fatal(rec.state())
			}
			if loadAt(t, kv, testHeight, testTime).PolicyVersion() != version || kv.Has(executedKey(rec.ParameterKey)) {
				t.Fatal("a rejected proposal counted as executed")
			}
			if kv.Has(businessKey("ex-" + id)) {
				t.Fatal("a rejected execution committed its business id")
			}
			if len(registry.calls) != 1 {
				t.Fatalf("calls %+v", registry.calls)
			}
			// A second execute of a REJECTED proposal is a conflict and does
			// not reach the registry.
			before := dumpKV(kv)
			again := executeWith(t, kv, registry, testTime, authoritySigner, "ex2-"+id, id)
			if again.Code != CodeConflict || len(registry.calls) != 1 {
				t.Fatalf("again %+v calls %d", again, len(registry.calls))
			}
			wantUnchanged(t, kv, before, "execute of a REJECTED proposal")
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
				kv, seats, authoritySigner := registryKV(t, devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
				registry := &fakeRegistry{out: map[string]Outcome{kind: fail(code)}}
				id, msg := registryProposal(kind)
				queueAt(t, kv, seats, testHeight, testTime, id, msg)
				before := dumpKV(kv)
				out := executeWith(t, kv, registry, testTime, authoritySigner, "ex-"+id, id)
				if out.Code != code || out.Keep || out.Idempotent {
					t.Fatalf("%+v", out)
				}
				wantUnchanged(t, kv, before, "a registry failure")
				if got := recordOf(t, kv, id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
					t.Fatal(got)
				}
				// The same proposal can execute once the registry accepts.
				registry.out = nil
				if retry := executeWith(t, kv, registry, testTime, authoritySigner, "ex-retry-"+id, id); !retry.Success() {
					t.Fatalf("retry %+v", retry)
				}
				if got := recordOf(t, kv, id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
					t.Fatal(got)
				}
				if len(registry.calls) != 2 {
					t.Fatalf("calls %+v", registry.calls)
				}
			})
		}
	}
}

func TestRegistryCannotAskForKeepOrIdempotent(t *testing.T) {
	kv, seats, authoritySigner := registryKV(t, devnetParam("DEC-002", "DEVNET_TEST_ONLY"))
	registry := &fakeRegistry{out: map[string]Outcome{"admit": {Code: CodeConflict, Keep: true}}}
	queueAt(t, kv, seats, testHeight, testTime, "admit-p1", admitProposal("p1"))
	before := dumpKV(kv)
	out := executeWith(t, kv, registry, testTime, authoritySigner, "ex-1", "admit-p1")
	if out.Code != CodeConflict || out.Keep {
		t.Fatalf("a registry keep leaked: %+v", out)
	}
	wantUnchanged(t, kv, before, "a registry keep")
	registry.out = map[string]Outcome{"admit": {Idempotent: true}}
	out = executeWith(t, kv, registry, testTime, authoritySigner, "ex-2", "admit-p1")
	if !out.Success() || out.Idempotent {
		t.Fatalf("a registry idempotent leaked: %+v", out)
	}
	if got := recordOf(t, kv, "admit-p1").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(got)
	}
}

func TestNilRegistryIsMalformed(t *testing.T) {
	for _, kind := range []string{"admit", "disable", "force_withdraw"} {
		t.Run(kind, func(t *testing.T) {
			kv, seats, authoritySigner := registryKV(t, devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
			id, msg := registryProposal(kind)
			queueAt(t, kv, seats, testHeight, testTime, id, msg)
			before := dumpKV(kv)
			out := executeWith(t, kv, nil, testTime, authoritySigner, "ex-"+id, id)
			if out.Code != CodeMalformed || out.Keep {
				t.Fatalf("%+v", out)
			}
			wantUnchanged(t, kv, before, "a missing registry")
			if got := recordOf(t, kv, id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				t.Fatal(got)
			}
		})
	}
	// Validator and governed-limit payloads do not need a registry.
	kv, seats, authoritySigner := registryKV(t)
	queueAt(t, kv, seats, testHeight, testTime, "p-add", powerMsg("v5", pub(9), 1, 20))
	if out := executeWith(t, kv, nil, testTime, authoritySigner, "ex-add", "p-add"); !out.Success() {
		t.Fatal(out)
	}
}

func TestExpiredRegistryProposalDoesNotCallTheRegistry(t *testing.T) {
	kv, seats, authoritySigner := registryKV(t, devnetParam("DEC-002", "DEVNET_TEST_ONLY"))
	registry := &fakeRegistry{}
	msg := propose(seats[0].Signer, "admit-p1", 1, admitProposal("p1"))
	msg.ExpiryUnixSeconds = testTime + 5
	if out := loadAt(t, kv, testHeight, testTime).ProposeChange(msg); !out.Success() {
		t.Fatal(out)
	}
	for i := 0; i < ApprovedThreshold; i++ {
		if out := loadAt(t, kv, testHeight, testTime).ApproveChange(approve(seats[i].Signer, "ap-"+string(rune('a'+i)), "admit-p1")); !out.Success() {
			t.Fatal(out)
		}
	}
	out := executeWith(t, kv, registry, testTime+5, authoritySigner, "ex-late", "admit-p1")
	if out.Code != CodeExpired || !out.Keep {
		t.Fatalf("%+v", out)
	}
	if got := recordOf(t, kv, "admit-p1").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED || len(registry.calls) != 0 {
		t.Fatalf("state %v calls %+v", got, registry.calls)
	}
}

func TestExternalBusinessIDIsReplay(t *testing.T) {
	foreign := bytes.Repeat([]byte{0x5a}, 32)
	external := func(lookups *[]string) func(string) ([]byte, bool) {
		return func(id string) ([]byte, bool) {
			*lookups = append(*lookups, id)
			switch id {
			case "shared":
				return append([]byte(nil), foreign...), true
			case "short":
				return bytes.Repeat([]byte{0x5a}, 31), true
			case "empty":
				return []byte{}, true
			}
			return nil, false
		}
	}
	// at loads the engine on kv with the peer lookup bound.
	at := func(t *testing.T, kv KV, lookups *[]string) *State {
		st := loadAt(t, kv, testHeight, testTime)
		st.External = external(lookups)
		return st
	}

	t.Run("propose", func(t *testing.T) {
		kv, seats, _ := registryKV(t)
		var lookups []string
		before := dumpKV(kv)
		msg := propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))
		msg.BusinessId = "shared"
		if out := at(t, kv, &lookups).ProposeChange(msg); out.Code != CodeReplay {
			t.Fatalf("%+v", out)
		}
		wantUnchanged(t, kv, before, "a replayed propose")
		if !slices.Equal(lookups, []string{"shared"}) {
			t.Fatalf("lookups %v", lookups)
		}
	})

	t.Run("approve", func(t *testing.T) {
		kv, seats, _ := registryKV(t)
		var lookups []string
		if out := at(t, kv, &lookups).ProposeChange(propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))); !out.Success() {
			t.Fatal(out)
		}
		before := dumpKV(kv)
		if out := at(t, kv, &lookups).ApproveChange(approve(seats[1].Signer, "shared", "p-add")); out.Code != CodeReplay {
			t.Fatalf("%+v", out)
		}
		wantUnchanged(t, kv, before, "a replayed approval")
		if len(recordOf(t, kv, "p-add").ApproverKeys) != 0 {
			t.Fatal("a replayed approval was recorded")
		}
	})

	t.Run("execute", func(t *testing.T) {
		kv, seats, authoritySigner := registryKV(t)
		var lookups []string
		queueAt(t, kv, seats, testHeight, testTime, "p-add", powerMsg("v5", pub(9), 1, 20))
		before := dumpKV(kv)
		if out := at(t, kv, &lookups).ExecuteChange(execute(authoritySigner, "shared", "p-add")); out.Code != CodeReplay || out.Keep {
			t.Fatalf("%+v", out)
		}
		wantUnchanged(t, kv, before, "a replayed execute")
		if got := recordOf(t, kv, "p-add").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatal(got)
		}
	})

	t.Run("own ledger first", func(t *testing.T) {
		kv, seats, _ := registryKV(t)
		var lookups []string
		if out := at(t, kv, &lookups).ProposeChange(propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))); !out.Success() {
			t.Fatal(out)
		}
		if out := at(t, kv, &lookups).ProposeChange(propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))); !out.Idempotent {
			t.Fatalf("%+v", out)
		}
		// The first propose consulted peers once; the repeat found its own
		// row (0x11) and did not.
		if !slices.Equal(lookups, []string{"b-p-add"}) {
			t.Fatalf("lookups %v", lookups)
		}
	})

	t.Run("same digest is idempotent", func(t *testing.T) {
		// One namespace: a peer row with the exact digest of this request
		// is the same request already committed.
		kv, seats, _ := registryKV(t)
		if out := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))); !out.Success() {
			t.Fatal(out)
		}
		want := businessDigest(map[string]any{"proposal_id": "p-add", "signer": hex.EncodeToString(seats[1].Signer)})
		st := loadAt(t, kv, testHeight, testTime)
		st.External = func(id string) ([]byte, bool) {
			if id == "elsewhere" {
				return append([]byte(nil), want...), true
			}
			return nil, false
		}
		before := dumpKV(kv)
		out := st.ApproveChange(approve(seats[1].Signer, "elsewhere", "p-add"))
		if !out.Idempotent || out.Code != "" {
			t.Fatalf("%+v", out)
		}
		wantUnchanged(t, kv, before, "an idempotent approval")
	})

	t.Run("malformed peer value is integrity", func(t *testing.T) {
		// A peer value that is not a 32-byte digest is damaged state, like
		// a damaged 0x11 value: INTEGRITY, never a free id.
		for _, id := range []string{"short", "empty"} {
			kv, seats, _ := registryKV(t)
			var lookups []string
			before := dumpKV(kv)
			msg := propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))
			msg.BusinessId = id
			if out := at(t, kv, &lookups).ProposeChange(msg); out.Code != CodeIntegrity || out.Keep {
				t.Fatalf("%s: %+v", id, out)
			}
			wantUnchanged(t, kv, before, "a malformed peer value")
		}
	})

	t.Run("malformed id never reaches peers", func(t *testing.T) {
		kv, seats, _ := registryKV(t)
		var lookups []string
		msg := propose(seats[0].Signer, "p-add", 1, powerMsg("v5", pub(9), 1, 20))
		msg.BusinessId = "Shared"
		if out := at(t, kv, &lookups).ProposeChange(msg); out.Code != CodeBounds {
			t.Fatalf("%+v", out)
		}
		msg.BusinessId = ""
		if out := at(t, kv, &lookups).ProposeChange(msg); out.Code != CodeMalformed {
			t.Fatalf("%+v", out)
		}
		if len(lookups) != 0 {
			t.Fatalf("lookups %v", lookups)
		}
	})
}

// devnetRole applies the State.devnet rule: a devnet chain id, the
// PROPOSED_TEST_PARAMETER label, approved_policy false, and the role
// DEVNET_TEST_ONLY. The State comes from LoadState on a stored config.
func TestDevnetRoleRule(t *testing.T) {
	kv, _, _ := registryKV(t,
		devnetParam("DEC-002", "DEVNET_TEST_ONLY"),
		devnetParam("DEC-003", "OTHER"),
		&typesv1.DevnetTestParameter{Label: "APPROVED", DecisionId: "DEC-004", RoleName: "DEVNET_TEST_ONLY"},
		&typesv1.DevnetTestParameter{Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-005", RoleName: "DEVNET_TEST_ONLY", ApprovedPolicy: true},
	)
	st := loadAt(t, kv, testHeight, testTime)
	if !st.devnetRole("DEC-002") {
		t.Fatal("DEC-002 devnet test role not found")
	}
	for _, id := range []string{"DEC-003", "DEC-004", "DEC-005", "DEC-001", ""} {
		if st.devnetRole(id) {
			t.Fatalf("%q counted as the devnet test role", id)
		}
	}
	beta, err := LoadState(kv, "shortvideo-beta-1", testHeight, testTime)
	if err != nil {
		t.Fatal(err)
	}
	if beta.devnetRole("DEC-002") {
		t.Fatal("a non-devnet chain id used a devnet test parameter")
	}
}
