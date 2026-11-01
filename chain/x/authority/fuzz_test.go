package authority

import (
	"bytes"
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// FuzzStateTransitions applies a bounded sequence of authority operations.
// Each operation runs on a clone (trial), as Keeper.transition does. A
// non-keeping failure must leave the trial's canonical authority bytes
// equal to the state before the operation: the engine writes nothing
// before it fails. The committed set keeps distinct keys and positive
// power.
func FuzzStateTransitions(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 1, 1, 2, 4})
	f.Add([]byte{0, 1, 1, 1, 2, 5, 3, 2})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 48 {
			raw = raw[:48]
		}
		st, seats, authoritySigner := readyState()
		for i, b := range raw {
			before := canonicalBytes(t, st)
			trial := st.clone()
			out := applyFuzzOp(trial, seats, authoritySigner, b, i)
			if out.Success() || out.Keep {
				st = trial
			} else if !bytes.Equal(before, canonicalBytes(t, trial)) {
				t.Fatalf("op %d code %s mutated the authority state before it failed", b%6, out.Code)
			}
			if problem := fuzzInvariant(st); problem != "" {
				t.Fatalf("op %d: %s", i, problem)
			}
		}
	})
}

func applyFuzzOp(st *State, seats [][]byte, authoritySigner []byte, b byte, i int) Outcome {
	id := "p" + string(rune('a'+(i%8)))
	switch b % 6 {
	case 0:
		return st.ProposeChange(propose(seats[int(b)%len(seats)], id, st.PolicyVersion, powerMsg("v5", pub(9), 1, uint64(st.Height)+2)))
	case 1:
		return st.ApproveChange(approve(seats[int(b)%len(seats)], "ap-"+id+"-"+string(rune('a'+int(b)%4)), id))
	case 2:
		return st.ExecuteChange(execute(authoritySigner, "ex-"+id, id))
	case 3:
		st.Height++
		_, _, out := st.ApplyDue()
		return out
	case 4:
		st.Time++
		_, _, out := st.ApplyDue()
		return out
	default:
		return st.ProposeChange(propose(seats[0], id, st.PolicyVersion, powerMsg("v1", pub(1), 0, uint64(st.Height)+3)))
	}
}

func fuzzInvariant(st *State) string {
	seen := map[string]struct{}{}
	for _, row := range st.Validators {
		if row.GetPower() <= 0 {
			return "nonpositive power"
		}
		key := string(row.GetConsensusPubkey())
		if _, ok := seen[key]; ok {
			return "duplicate key"
		}
		seen[key] = struct{}{}
	}
	if len(st.Validators) > 0 && len(st.Validators) < ApprovedSeats {
		return "validator count fell below four"
	}
	if st.PolicyVersion < 1 {
		return "policy version"
	}
	return ""
}

func canonicalBytes(t *testing.T, st *State) []byte {
	t.Helper()
	gen := &authorityv1.GenesisState{Authority: &authorityv1.AuthorityState{
		Validators:            st.Validators,
		Submitters:            st.Submitters,
		DevnetTestSeats:       st.Seats,
		Proposals:             st.Proposals,
		PendingValidatorPower: st.Pending,
	}, DevnetTestParameters: st.Params}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(gen)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(sideFrom(st))
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, encoded...)
}
