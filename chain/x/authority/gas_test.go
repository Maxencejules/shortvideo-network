package authority

import (
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// councilGas is the gas of one measured round: propose, three approvals and
// the execute of the same governed-limit payload from the same seats.
type councilGas struct {
	propose  uint64
	approves [ApprovedThreshold]uint64
	execute  uint64
}

func measureRound(t *testing.T, env *keeperEnv, id string) councilGas {
	t.Helper()
	var out councilGas
	used := func(run func(sdk.Context) error) uint64 {
		t.Helper()
		ctx := env.txCtx()
		if err := run(ctx); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return ctx.GasMeter().GasConsumed()
	}
	base := env.policy(t)
	out.propose = used(func(ctx sdk.Context) error {
		_, err := env.keeper.Propose(ctx, propose(env.seats[0].Signer, id, base, governedMsg("max_page_limit", 50)))
		return err
	})
	for i := 0; i < ApprovedThreshold; i++ {
		out.approves[i] = used(func(ctx sdk.Context) error {
			_, err := env.keeper.Approve(ctx, approve(env.seats[i].Signer, "ap-"+id+"-"+strconv.Itoa(i), id))
			return err
		})
	}
	out.execute = used(func(ctx sdk.Context) error {
		_, err := env.keeper.Execute(ctx, execute(env.authority, "ex-"+id, id))
		return err
	})
	return out
}

// Gas of a council message does not depend on history (plan 2.9). The
// measured messages read and write the same keys with the same value
// lengths before and after more than 300 terminal proposals; the only
// history-correlated value, base_policy_version in the record, stays one
// decimal digit on both sides. So the bound is 0: gas is equal.
//
// Fails on the old code: it loaded and rewrote one side document with every
// proposal (about 22.7k gas more per retained proposal) and returned BOUNDS
// at the 129th proposal.
func TestCouncilGasDoesNotGrowWithHistory(t *testing.T) {
	env := readyKeeper(t)
	// Warm-up: one executed governed limit, so the policy key (0x04) and the
	// governed key (0x13||max_page_limit) exist on both sides.
	env.queue(t, "warm-up", governedMsg("max_page_limit", 40))
	if _, err := env.keeper.Execute(env.txCtx(), execute(env.authority, "ex-warm-up", "warm-up")); err != nil {
		t.Fatal(err)
	}
	baseline := measureRound(t, env, "m-001")

	const expired, rejected, executed = 150, 150, 5
	for i := 0; i < expired; i++ {
		id := "x-" + strconv.Itoa(i)
		msg := propose(env.seats[i%4].Signer, id, env.policy(t), governedMsg("max_page_limit", 10))
		msg.ExpiryUnixSeconds = env.unix + 5
		if _, err := env.keeper.Propose(env.txCtx(), msg); err != nil {
			t.Fatal(err)
		}
		env.unix += 10
		if _, err := env.keeper.Approve(env.txCtx(), approve(env.seats[(i+1)%4].Signer, "ax-"+strconv.Itoa(i), id)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < rejected; i++ {
		id := "r-" + strconv.Itoa(i)
		env.queue(t, id, removalMsg("v1", pub(1), 100))
		ctx := env.txCtx()
		if _, err := env.keeper.Execute(ctx, execute(env.authority, "er-"+strconv.Itoa(i), id)); err != nil {
			t.Fatal(err)
		}
		if code, _ := resultCode(t, ctx); code != CodeForbidden {
			t.Fatalf("%s was not REJECTED: %q", id, code)
		}
	}
	for i := 0; i < executed; i++ {
		id := "e-" + strconv.Itoa(i)
		env.queue(t, id, governedMsg("max_evidence_batch", uint64(1+i)))
		if _, err := env.keeper.Execute(env.txCtx(), execute(env.authority, "ee-"+strconv.Itoa(i), id)); err != nil {
			t.Fatal(err)
		}
	}
	terminal := countPrefix(env.kv(), proposalPrefix)
	if terminal < 300 || countPrefix(env.kv(), openPrefix) != 0 {
		t.Fatalf("%d proposal records, %d open counts", terminal, countPrefix(env.kv(), openPrefix))
	}
	if version := env.policy(t); version > 9 {
		t.Fatalf("policy version %d is not one digit", version)
	}
	after := measureRound(t, env, "m-002")
	t.Logf("history %d proposal records; baseline %+v; after %+v", terminal, baseline, after)
	if after != baseline {
		t.Fatalf("gas grew with history: baseline %+v after %+v", baseline, after)
	}
	if got := recordOf(t, env.kv(), "m-002").state(); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
		t.Fatal(got)
	}
}

// traceKV records the keys a transition reads (Get, Has, and the start key
// of each iterator, as "scan:" plus the start) and writes (Set, Delete).
type traceKV struct {
	KV
	reads, writes map[string]bool
}

func (k *traceKV) Get(key []byte) []byte { k.reads[string(key)] = true; return k.KV.Get(key) }
func (k *traceKV) Has(key []byte) bool   { k.reads[string(key)] = true; return k.KV.Has(key) }
func (k *traceKV) Set(key, value []byte) { k.writes[string(key)] = true; k.KV.Set(key, value) }
func (k *traceKV) Delete(key []byte)     { k.writes[string(key)] = true; k.KV.Delete(key) }
func (k *traceKV) Iterator(start, end []byte) storetypes.Iterator {
	k.reads["scan:"+string(start)] = true
	return k.KV.Iterator(start, end)
}

func keySet(keys ...[]byte) map[string]bool {
	out := map[string]bool{}
	for _, key := range keys {
		out[string(key)] = true
	}
	return out
}

func hexKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, hex.EncodeToString([]byte(key)))
	}
	slices.Sort(out)
	return out
}

// TestMessageKeySets pins the per-message read and write sets of the
// x/authority README table and the msg.go gas comment: the exact keys each
// transition reads and writes, through LoadState on a recording store. A
// propose reads and writes its seat's open count (0x14) at the current
// version; an approve that is not a keep never touches 0x14; an EXPIRED keep
// from approve, a REJECTED keep from execute, and a successful execute read
// and write the proposer's 0x14 at the record's base; the stale branch reads
// the parameter's 0x12; a rejected execute writes nothing. The second
// fix-round review found the comment and the table left out the 0x14 reads of
// a propose, an approve keep and an execute. An execute of an admit, disable
// or force-withdraw payload reads and writes the keys of a governed execute
// except 0x13: the registry hook writes x/providers or x/content, and no
// authority key under the reserved 0x17 prefix is read or written.
func TestMessageKeySets(t *testing.T) {
	kv, seats, authority := readyStore(t)
	cfg := []byte{configKey}
	policy := []byte{policyKey}
	unix := testTime
	run := func(name string, out string, wantReads, wantWrites map[string]bool, fn func(*State) Outcome) {
		t.Helper()
		trace := &traceKV{KV: kv, reads: map[string]bool{}, writes: map[string]bool{}}
		st, err := LoadState(trace, testChainID, testHeight, unix)
		if err != nil {
			t.Fatal(err)
		}
		if got := fn(st); got.Code != out {
			t.Fatalf("%s: %+v, want %q", name, got, out)
		}
		wantReads[string(cfg)] = true
		if !maps.Equal(trace.reads, wantReads) {
			t.Fatalf("%s reads\n got %v\nwant %v", name, hexKeys(trace.reads), hexKeys(wantReads))
		}
		if !maps.Equal(trace.writes, wantWrites) {
			t.Fatalf("%s writes\n got %v\nwant %v", name, hexKeys(trace.writes), hexKeys(wantWrites))
		}
	}
	seat1, seat2 := seats[0].ID, seats[1].ID

	// Propose, three approvals (the third queues), execute: version 1 -> 2.
	run("propose", "", keySet(businessKey("b-p1"), proposalKey("p1"), policy, openKey(seat1, 1)),
		keySet(proposalKey("p1"), businessKey("b-p1"), openKey(seat1, 1)),
		func(st *State) Outcome {
			return st.ProposeChange(propose(seats[0].Signer, "p1", 1, governedMsg("max_page_limit", 10)))
		})
	for i := 0; i < ApprovedThreshold; i++ {
		id := "a1-" + strconv.Itoa(i)
		run("approve "+id, "", keySet(businessKey(id), proposalKey("p1")), keySet(proposalKey("p1"), businessKey(id)),
			func(st *State) Outcome { return st.ApproveChange(approve(seats[i].Signer, id, "p1")) })
	}
	run("execute", "", keySet(businessKey("e1"), proposalKey("p1"), policy, openKey(seat1, 1)),
		keySet(governedKey("max_page_limit"), openKey(seat1, 1), proposalKey("p1"), executedKey("max_page_limit"), policy, businessKey("e1")),
		func(st *State) Outcome { return st.ExecuteChange(execute(authority, "e1", "p1")) })

	// Stale branch: p5 executes (version 2 -> 3), then p4 on base 2 reads
	// the executed digest of its parameter and is CONFLICT, writing nothing.
	queueAt(t, kv, seats, testHeight, unix, "p4", governedMsg("max_evidence_batch", 5))
	queueAt(t, kv, seats, testHeight, unix, "p5", governedMsg("max_evidence_batch", 6))
	if out := loadAt(t, kv, testHeight, unix).ExecuteChange(execute(authority, "e5", "p5")); !out.Success() {
		t.Fatal(out)
	}
	run("stale execute", CodeConflict, keySet(businessKey("e4"), proposalKey("p4"), policy, executedKey("max_evidence_batch")),
		keySet(), func(st *State) Outcome { return st.ExecuteChange(execute(authority, "e4", "p4")) })

	// EXPIRED keep from approve: the proposer's 0x14 at the record's base.
	early := propose(seats[1].Signer, "p2", 3, governedMsg("max_page_limit", 20))
	early.ExpiryUnixSeconds = unix + 5
	run("propose p2", "", keySet(businessKey("b-p2"), proposalKey("p2"), policy, openKey(seat2, 3)),
		keySet(proposalKey("p2"), businessKey("b-p2"), openKey(seat2, 3)),
		func(st *State) Outcome { return st.ProposeChange(early) })
	unix += 10
	run("approve keep", CodeExpired, keySet(businessKey("late"), proposalKey("p2"), openKey(seat2, 3)),
		keySet(openKey(seat2, 3), proposalKey("p2")),
		func(st *State) Outcome { return st.ApproveChange(approve(seats[2].Signer, "late", "p2")) })

	// REJECTED keep from execute: a removal that leaves three validators.
	queueAt(t, kv, seats, testHeight, unix, "p3", removalMsg("v1", pub(1), 100))
	run("execute keep", CodeForbidden,
		keySet(businessKey("e3"), proposalKey("p3"), policy, []byte("scan:\x03"), openKey(seat1, 3)),
		keySet(openKey(seat1, 3), proposalKey("p3")),
		func(st *State) Outcome { return st.ExecuteChange(execute(authority, "e3", "p3")) })

	// Registry payloads: the hook (a fake here) is the only other effect.
	// Each executes at the current version, so each one bumps it.
	registry := &fakeRegistry{}
	for i, row := range []struct {
		id  string
		msg *authorityv1.MsgProposeChange
	}{
		{"p6", admitProposal("p1")},
		{"p7", disableProposal("p1")},
		{"p8", withdrawProposal("c1", 1, reasonValue)},
	} {
		version := uint64(3 + i)
		queueAt(t, kv, seats, testHeight, unix, row.id, row.msg)
		param := recordOf(t, kv, row.id).ParameterKey
		business := "e" + row.id[1:]
		run("execute "+row.id, "", keySet(businessKey(business), proposalKey(row.id), policy, openKey(seat1, version)),
			keySet(openKey(seat1, version), proposalKey(row.id), executedKey(param), policy, businessKey(business)),
			func(st *State) Outcome {
				st.Registry = registry
				return st.ExecuteChange(execute(authority, business, row.id))
			})
	}
	if len(registry.calls) != 3 {
		t.Fatalf("registry calls %+v", registry.calls)
	}
}
