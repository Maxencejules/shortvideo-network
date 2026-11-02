package authority

import (
	"bytes"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/store/v2/cachekv"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// specRegistry is a deterministic unit-test double of the x/providers and
// x/content council hooks. It follows apply_proposal_payload in the frozen
// spec machine and writes only after every check passed. It records every
// call, including the ones that fail.
type specRegistry struct {
	providers map[string]string
	groups    map[string]string
	contents  map[string]uint64
	withdrawn map[string]bool
	effects   int
	calls     []registryCall
}

// newSpecRegistry has providers p1, p2 (devnet-test-only) and p3
// (other-group), all APPLIED, and content c1 and c2 at version 1.
func newSpecRegistry() *specRegistry {
	return &specRegistry{
		providers: map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"},
		groups:    map[string]string{"p1": "devnet-test-only", "p2": "devnet-test-only", "p3": "other-group"},
		contents:  map[string]uint64{"c1": 1, "c2": 1},
		withdrawn: map[string]bool{},
	}
}

// emptySpecRegistry has no provider and no content: every payload is
// NOT_FOUND.
func emptySpecRegistry() *specRegistry {
	return &specRegistry{
		providers: map[string]string{},
		groups:    map[string]string{},
		contents:  map[string]uint64{},
		withdrawn: map[string]bool{},
	}
}

// clone is an independent copy, the registry side of one cache branch.
func (r *specRegistry) clone() *specRegistry {
	return &specRegistry{
		providers: maps.Clone(r.providers),
		groups:    maps.Clone(r.groups),
		contents:  maps.Clone(r.contents),
		withdrawn: maps.Clone(r.withdrawn),
		effects:   r.effects,
		calls:     slices.Clone(r.calls),
	}
}

// equal compares the registry state. Calls are not state.
func (r *specRegistry) equal(o *specRegistry) bool {
	return maps.Equal(r.providers, o.providers) && maps.Equal(r.groups, o.groups) &&
		maps.Equal(r.contents, o.contents) && maps.Equal(r.withdrawn, o.withdrawn) && r.effects == o.effects
}

func (r *specRegistry) Admit(id string, devnet bool) Outcome {
	r.calls = append(r.calls, registryCall{Method: "admit", ID: id, Devnet: devnet})
	state, found := r.providers[id]
	if !found {
		return fail(CodeNotFound)
	}
	if !devnet {
		return fail(CodeDecisionBlocked)
	}
	if r.groups[id] != "devnet-test-only" || state != "APPLIED" {
		return fail(CodeForbidden)
	}
	r.providers[id] = "ADMITTED"
	r.effects++
	return ok()
}

func (r *specRegistry) Disable(id string) Outcome {
	r.calls = append(r.calls, registryCall{Method: "disable", ID: id})
	state, found := r.providers[id]
	if !found {
		return fail(CodeNotFound)
	}
	if state != "ADMITTED" && state != "DRAINING" {
		return fail(CodeConflict)
	}
	r.providers[id] = "DISABLED"
	r.effects++
	return ok()
}

func (r *specRegistry) ForceWithdraw(id string, version uint64, reason []byte, devnet bool) Outcome {
	r.calls = append(r.calls, registryCall{
		Method: "force_withdraw", ID: id, Version: version, Reason: bytes.Clone(reason), Devnet: devnet,
	})
	have, found := r.contents[id]
	if !found {
		return fail(CodeNotFound)
	}
	if !devnet {
		return fail(CodeDecisionBlocked)
	}
	if r.withdrawn[id] {
		return fail(CodeWithdrawn)
	}
	if version != have || len(reason) != 32 {
		return fail(CodeStale)
	}
	r.withdrawn[id] = true
	r.effects++
	return ok()
}

// FuzzCouncilRegistryPayloads runs bounded sequences of admit, disable, and
// force-withdraw proposals through propose, approve, and execute against a
// spec registry, on the per-object store. It mirrors Keeper.transition: each
// operation runs on a cache branch (cachekv) of the committed store and on a
// clone of the registry; a success writes both back; a keep writes the
// authority branch and drops the registry clone; any other outcome drops
// both.
//
// Invariants after every operation: a failure or an idempotent retry leaves
// the branch byte-identical to the committed store (dumpKV) and the registry
// clone equal to the registry before the operation (the engine and the
// registry write nothing before they fail); a keep writes nothing to the
// registry; the registry changed exactly once per proposal that ever
// executed; the policy version is one plus that count; the validator set
// never changes and no pending entry (0x03) exists; DISABLED providers and
// withdrawn content stay terminal; a proposal never returns to OPEN or QUEUED
// once it is EXECUTED, REJECTED, or EXPIRED; and the round 3 store invariants
// of fuzzHarness.checkInvariants hold (open counts per seat and base equal
// the OPEN and QUEUED records, records well formed, admission rows).
//
// An EXECUTED or REJECTED proposal can later read EXPIRED: op_execute in
// the frozen spec machine tests expiry before QUEUED and marks the
// proposal. That order is a recorded Goal 06 follow-up and is kept here.
func FuzzCouncilRegistryPayloads(f *testing.F) {
	for _, seed := range councilRegistrySeeds {
		f.Add(seed.raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 64 {
			raw = raw[:64]
		}
		runCouncilRegistrySequence(t, raw)
	})
}

// councilRegistrySeeds reach every registry path. Byte 0 sets DEC-002 (bit
// 1) and DEC-003 (bit 2). Approval bytes 3, 35, 67 are seats 0, 1, 2.
var councilRegistrySeeds = []struct {
	name      string
	raw       []byte
	providers map[string]string
	withdrawn map[string]bool
	states    []authorityv1.ProposalState
}{
	{
		name:      "admit then disable",
		raw:       []byte{3, 0, 3, 35, 67, 4, 1, 11, 43, 75, 12},
		providers: map[string]string{"p1": "DISABLED", "p2": "APPLIED", "p3": "APPLIED"},
		states:    []authorityv1.ProposalState{authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED, authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED},
	},
	{
		name:      "force withdraw then withdrawn",
		raw:       []byte{2, 2, 3, 35, 67, 4, 2, 11, 43, 75, 12},
		providers: map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"},
		withdrawn: map[string]bool{"c1": true},
		states:    []authorityv1.ProposalState{authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED},
	},
	{
		name:      "admit without DEC-002 stays queued",
		raw:       []byte{0, 0, 3, 35, 67, 4, 4},
		providers: map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"},
		states:    []authorityv1.ProposalState{authorityv1.ProposalState_PROPOSAL_STATE_QUEUED},
	},
	{
		name:      "admit of another group is rejected",
		raw:       []byte{1, 16, 3, 35, 67, 4},
		providers: map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"},
		states:    []authorityv1.ProposalState{authorityv1.ProposalState_PROPOSAL_STATE_REJECTED},
	},
	{
		name:      "expired before execute",
		raw:       []byte{3, 0, 3, 35, 67, 6, 4},
		providers: map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"},
		states:    []authorityv1.ProposalState{authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED},
	},
	{
		// Found by this fuzz: an execute after expiry re-marks a REJECTED
		// proposal EXPIRED (op_execute order, Goal 06 follow-up).
		name:      "rejected then expired",
		raw:       []byte("10c#C7&70"),
		providers: map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"},
		states:    []authorityv1.ProposalState{authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED, authorityv1.ProposalState_PROPOSAL_STATE_OPEN},
	},
	{
		name:      "disable of a draining provider",
		raw:       []byte{1, 0, 3, 35, 67, 4, 5, 1, 11, 43, 75, 12},
		providers: map[string]string{"p1": "DISABLED", "p2": "APPLIED", "p3": "APPLIED"},
		states:    []authorityv1.ProposalState{authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED, authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED},
	},
}

func TestCouncilRegistrySeedsReachEveryPath(t *testing.T) {
	for _, seed := range councilRegistrySeeds {
		t.Run(seed.name, func(t *testing.T) {
			kv, registry, ids := runCouncilRegistrySequence(t, seed.raw)
			if !maps.Equal(registry.providers, seed.providers) {
				t.Fatalf("providers %v", registry.providers)
			}
			if !maps.Equal(registry.withdrawn, seed.withdrawn) {
				t.Fatalf("withdrawn %v", registry.withdrawn)
			}
			var states []authorityv1.ProposalState
			for _, id := range ids {
				states = append(states, recordOf(t, kv, id).state())
			}
			if !slices.Equal(states, seed.states) {
				t.Fatalf("states %v", states)
			}
		})
	}
}

// runCouncilRegistrySequence applies raw as described on
// FuzzCouncilRegistryPayloads and checks the invariants after every op. It
// returns the committed store, the registry and the accepted proposal ids in
// order.
func runCouncilRegistrySequence(t testing.TB, raw []byte) (KV, *specRegistry, []string) {
	t.Helper()
	gen, seats, authoritySigner := readyGenesis()
	registry := newSpecRegistry()
	if len(raw) == 0 {
		return storeFrom(t, gen), registry, nil
	}
	if raw[0]&1 != 0 {
		gen.DevnetTestParameters = append(gen.DevnetTestParameters, devnetParam("DEC-002", "DEVNET_TEST_ONLY"))
	}
	if raw[0]&2 != 0 {
		gen.DevnetTestParameters = append(gen.DevnetTestParameters, devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
	}
	base := storeFrom(t, gen)
	// h reuses the round 3 store invariants (checkInvariants). The registry
	// payloads never schedule a validator change, so h.executes is the
	// number of proposals that ever executed.
	h := &fuzzHarness{
		t: t, base: base, seats: slices.Clone(seats), authority: authoritySigner, service: bytes20(2),
		height: testHeight, unix: testTime, executed: map[string]bool{}, records: map[string]fuzzRecord{},
	}
	h.vals = h.validatorBytes(h.load(base, h.height, h.unix).Validators())
	validators := h.vals
	var ids []string
	terminal := map[string]authorityv1.ProposalState{}
	executedEver := map[string]bool{}

	for i, b := range raw[1:] {
		h.c.Ops++
		before := dumpKV(base)
		saved := registry.clone()
		switch b % 8 {
		case 5:
			// A DrainProvider in x/providers, outside authority: ADMITTED
			// to DRAINING. It writes no authority state.
			target := fuzzProviders[int(b>>3)%len(fuzzProviders)]
			if registry.providers[target] == "ADMITTED" {
				registry.providers[target] = "DRAINING"
			}
		case 6:
			// Time passes; no message.
			h.unix += int64(b>>3) + 1
		default:
			trial := cachekv.NewStore(base)
			reg := registry.clone()
			st := loadAt(t, trial, h.height, h.unix)
			st.Registry = reg
			out, created, executed := applyRegistryOp(st, seats, authoritySigner, ids, b, i)
			switch {
			case out.Idempotent || (!out.Success() && !out.Keep):
				if !reg.equal(saved) {
					t.Fatalf("op %d %+v wrote to the registry", i, out)
				}
				if got := dumpKV(trial); !bytes.Equal(got, before) {
					t.Fatalf("op %d %+v wrote to the authority store before it failed", i, out)
				}
			case out.Keep:
				if !reg.equal(saved) {
					t.Fatalf("op %d code %s: a keep wrote to the registry", i, out.Code)
				}
				trial.Write()
			default:
				trial.Write()
				registry = reg
				ids = append(ids, created...)
				if executed != "" {
					executedEver[executed] = true
					h.executed[executed] = true
				}
			}
		}
		for _, id := range ids {
			state := recordOf(t, base, id).state()
			if prev, seen := terminal[id]; seen && prev != state && state != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
				t.Fatalf("proposal %s went from %v to %v", id, prev, state)
			}
			switch state {
			case authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED, authorityv1.ProposalState_PROPOSAL_STATE_REJECTED,
				authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED:
				terminal[id] = state
			}
		}
		h.executes = uint64(len(executedEver))
		if registry.effects != len(executedEver) {
			t.Fatalf("registry effects %d, executed registry proposals %d", registry.effects, len(executedEver))
		}
		if got := loadAt(t, base, h.height, h.unix).PolicyVersion(); got != uint64(1+len(executedEver)) {
			t.Fatalf("policy version %d, executed %d", got, len(executedEver))
		}
		if countPrefix(base, pendingPrefix) != 0 {
			t.Fatal("a registry payload scheduled a validator change")
		}
		h.checkInvariants(i, validators, false)
		for id, state := range saved.providers {
			if state == "DISABLED" && registry.providers[id] != "DISABLED" {
				t.Fatalf("provider %s left DISABLED", id)
			}
		}
		for id := range saved.withdrawn {
			if !registry.withdrawn[id] {
				t.Fatalf("content %s was revived", id)
			}
		}
	}
	return base, registry, ids
}

var (
	fuzzProviders = []string{"p1", "p2", "p3", "p9"}
	fuzzContents  = []string{"c1", "c2", "c9"}
)

// applyRegistryOp decodes one byte: 0 admit, 1 disable, 2 force-withdraw
// proposals (seat b>>6, near expiry), 3 an approval, 4 and 7 an execute by
// the authority account. It returns the outcome, the proposal id a propose
// opened, and the proposal an execute named (empty for other ops). Bytes 5
// and 6 are handled by the caller.
func applyRegistryOp(st *State, seats []testSeat, authoritySigner []byte, ids []string, b byte, i int) (Outcome, []string, string) {
	id := "q" + strconv.Itoa(i)
	pick := func() string {
		if len(ids) == 0 {
			return "q-none"
		}
		return ids[int(b>>3)%len(ids)]
	}
	open := func(msg *authorityv1.MsgProposeChange) (Outcome, []string, string) {
		msg = propose(seats[int(b>>6)%len(seats)].Signer, id, st.PolicyVersion(), msg)
		msg.ExpiryUnixSeconds = st.Time + 1 + int64(b%16)
		return st.ProposeChange(msg), []string{id}, ""
	}
	switch b % 8 {
	case 0:
		return open(admitProposal(fuzzProviders[int(b>>3)%len(fuzzProviders)]))
	case 1:
		return open(disableProposal(fuzzProviders[int(b>>3)%len(fuzzProviders)]))
	case 2:
		return open(withdrawProposal(fuzzContents[int(b>>3)%len(fuzzContents)], 1+uint64(b>>5)%2, reasonValue))
	case 3:
		target := pick()
		return st.ApproveChange(approve(seats[int(b>>5)%len(seats)].Signer, "ap-"+target+"-"+strconv.Itoa(int(b>>5)%4), target)), nil, ""
	case 4:
		target := pick()
		return st.ExecuteChange(execute(authoritySigner, "ex-"+target+"-"+strconv.Itoa(i%2), target)), nil, target
	default:
		target := pick()
		return st.ExecuteChange(execute(authoritySigner, "ex-"+target+"-0", target)), nil, target
	}
}

// devnetParam is a labelled devnet test parameter that names role.
func devnetParam(decision, role string) *typesv1.DevnetTestParameter {
	return &typesv1.DevnetTestParameter{Label: "PROPOSED_TEST_PARAMETER", DecisionId: decision, RoleName: role}
}
