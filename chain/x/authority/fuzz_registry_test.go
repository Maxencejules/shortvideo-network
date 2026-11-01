package authority

import (
	"bytes"
	"maps"
	"slices"
	"strconv"
	"testing"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// specRegistry is a deterministic unit-test double of the x/providers and
// x/content council hooks. It follows apply_proposal_payload in the frozen
// spec machine and writes only after every check passed.
type specRegistry struct {
	providers map[string]string
	groups    map[string]string
	contents  map[string]uint64
	withdrawn map[string]bool
	effects   int
}

func newSpecRegistry() *specRegistry {
	return &specRegistry{
		providers: map[string]string{"p1": "APPLIED", "p2": "APPLIED", "p3": "APPLIED"},
		groups:    map[string]string{"p1": "devnet-test-only", "p2": "devnet-test-only", "p3": "other-group"},
		contents:  map[string]uint64{"c1": 1, "c2": 1},
		withdrawn: map[string]bool{},
	}
}

func (r *specRegistry) copy() *specRegistry {
	return &specRegistry{
		providers: maps.Clone(r.providers),
		groups:    maps.Clone(r.groups),
		contents:  maps.Clone(r.contents),
		withdrawn: maps.Clone(r.withdrawn),
		effects:   r.effects,
	}
}

func (r *specRegistry) equal(o *specRegistry) bool {
	return maps.Equal(r.providers, o.providers) && maps.Equal(r.contents, o.contents) &&
		maps.Equal(r.withdrawn, o.withdrawn) && r.effects == o.effects
}

func (r *specRegistry) Admit(id string, devnet bool) Outcome {
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
// spec registry. It mirrors Keeper.transition: a failure discards both the
// authority trial and the registry writes; a keep saves the authority trial
// and discards the registry writes.
//
// Invariants: a failure leaves the trial's canonical authority bytes equal
// to the state before the operation and writes nothing to the registry
// (the engine writes nothing before it fails); a keep writes nothing to
// the registry; the registry changed exactly once
// per proposal that ever executed; the policy version is one plus that
// count; the validator set and pending set never change; DISABLED
// providers and withdrawn content stay terminal; a proposal never returns
// to OPEN or QUEUED once it is EXECUTED, REJECTED, or EXPIRED.
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
			st, registry := runCouncilRegistrySequence(t, seed.raw)
			if !maps.Equal(registry.providers, seed.providers) {
				t.Fatalf("providers %v", registry.providers)
			}
			if !maps.Equal(registry.withdrawn, seed.withdrawn) {
				t.Fatalf("withdrawn %v", registry.withdrawn)
			}
			var states []authorityv1.ProposalState
			for _, p := range st.Proposals {
				states = append(states, p.GetState())
			}
			if !slices.Equal(states, seed.states) {
				t.Fatalf("states %v", states)
			}
		})
	}
}

// runCouncilRegistrySequence applies raw as described on
// FuzzCouncilRegistryPayloads and checks the invariants after every op.
func runCouncilRegistrySequence(t *testing.T, raw []byte) (*State, *specRegistry) {
	t.Helper()
	st, seats, authoritySigner := readyState()
	registry := newSpecRegistry()
	if len(raw) == 0 {
		return st, registry
	}
	if raw[0]&1 != 0 {
		st.Params = append(st.Params, devnetParam("DEC-002", "DEVNET_TEST_ONLY"))
	}
	if raw[0]&2 != 0 {
		st.Params = append(st.Params, devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
	}
	validators := canonicalValidators(st)
	var ids []string
	terminal := map[string]authorityv1.ProposalState{}
	executedEver := map[string]bool{}

	for i, b := range raw[1:] {
		before := canonicalBytes(t, st)
		saved := registry.copy()
		trial := st.clone()
		trial.Registry = registry
		var out Outcome
		var created []string
		if b%8 == 5 {
			// A DrainProvider in x/providers, outside authority: ADMITTED
			// to DRAINING. It writes no authority state.
			target := fuzzProviders[int(b>>3)%len(fuzzProviders)]
			if registry.providers[target] == "ADMITTED" {
				registry.providers[target] = "DRAINING"
			}
		} else {
			out, created = applyRegistryOp(trial, seats, authoritySigner, ids, b, i)
		}
		switch {
		case out.Success():
			st = trial
			ids = append(ids, created...)
		case out.Keep:
			if !registry.equal(saved) {
				t.Fatalf("op %d code %s: a keep wrote to the registry", i, out.Code)
			}
			st = trial
		default:
			if !registry.equal(saved) {
				t.Fatalf("op %d code %s: a failure wrote to the registry", i, out.Code)
			}
			registry = saved
			if !bytes.Equal(before, canonicalBytes(t, trial)) {
				t.Fatalf("op %d code %s mutated the authority state before it failed", i, out.Code)
			}
		}
		for _, p := range st.Proposals {
			id, state := p.GetProposalId(), p.GetState()
			if prev, seen := terminal[id]; seen && prev != state && state != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
				t.Fatalf("proposal %s went from %v to %v", id, prev, state)
			}
			switch state {
			case authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED:
				executedEver[id] = true
				terminal[id] = state
			case authorityv1.ProposalState_PROPOSAL_STATE_REJECTED, authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED:
				terminal[id] = state
			}
		}
		if registry.effects != len(executedEver) {
			t.Fatalf("registry effects %d, executed registry proposals %d", registry.effects, len(executedEver))
		}
		if st.PolicyVersion != uint64(1+len(executedEver)) {
			t.Fatalf("policy version %d, executed %d", st.PolicyVersion, len(executedEver))
		}
		if !bytes.Equal(validators, canonicalValidators(st)) || len(st.Pending) != 0 {
			t.Fatal("a registry payload changed the validator set")
		}
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
	return st, registry
}

var (
	fuzzProviders = []string{"p1", "p2", "p3", "p9"}
	fuzzContents  = []string{"c1", "c2", "c9"}
)

func applyRegistryOp(st *State, seats [][]byte, authoritySigner []byte, ids []string, b byte, i int) (Outcome, []string) {
	id := "q" + strconv.Itoa(i)
	pick := func() string {
		if len(ids) == 0 {
			return "q-none"
		}
		return ids[int(b>>3)%len(ids)]
	}
	open := func(msg *authorityv1.MsgProposeChange) (Outcome, []string) {
		msg = propose(seats[int(b>>6)%len(seats)], id, st.PolicyVersion, msg)
		msg.ExpiryUnixSeconds = st.Time + 1 + int64(b%16)
		return st.ProposeChange(msg), []string{id}
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
		return st.ApproveChange(approve(seats[int(b>>5)%len(seats)], "ap-"+target+"-"+strconv.Itoa(int(b>>5)%4), target)), nil
	case 4:
		target := pick()
		return st.ExecuteChange(execute(authoritySigner, "ex-"+target+"-"+strconv.Itoa(i%2), target)), nil
	case 6:
		st.Time += int64(b>>3) + 1
		return ok(), nil
	default:
		target := pick()
		return st.ExecuteChange(execute(authoritySigner, "ex-"+target+"-0", target)), nil
	}
}

func canonicalValidators(st *State) []byte {
	var buf bytes.Buffer
	for _, row := range st.Validators {
		buf.WriteString(row.GetNodeId())
		buf.Write(row.GetConsensusPubkey())
		buf.WriteString(strconv.FormatInt(row.GetPower(), 10))
	}
	return buf.Bytes()
}
