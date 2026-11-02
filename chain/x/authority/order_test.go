package authority

import (
	"testing"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// Each row moves a branch to the spec order (spec_machine.py line in the
// comment) and gets the spec code. Every row returned another code on the
// round-2 order, so each fails on the old code. Rows leave the store
// byte-identical.
func TestCheckOrderMatchesSpec(t *testing.T) {
	kv, seats, authority := readyStore(t)
	blocked, _, _ := readyGenesis()
	blocked.DevnetTestParameters = nil
	blockedKV := storeFrom(t, blocked)
	outsider := bytes20(9)
	rows := []struct {
		name string
		kv   KV
		run  func(*State) Outcome
		code string
	}{
		// op_propose require_fields (1405) runs before council_ready (1408).
		// Round 2: DECISION_BLOCKED.
		{"propose: empty business id on a blocked council", blockedKV, func(s *State) Outcome {
			msg := propose(seats[0].Signer, "p-1", 1, governedMsg("max_page_limit", 10))
			msg.BusinessId = ""
			return s.ProposeChange(msg)
		}, CodeMalformed},
		// op_propose require_fields (1405) runs before the seat check (1411).
		// Round 2: UNAUTHORIZED.
		{"propose: missing payload from a non-seat", kv, func(s *State) Outcome {
			msg := propose(outsider, "p-2", 1, governedMsg("max_page_limit", 10))
			msg.Payload = nil
			return s.ProposeChange(msg)
		}, CodeMalformed},
		// validated_payload activation check (1209-1210) runs before
		// check_time on the expiry (1423). Round 2: BOUNDS.
		{"propose: past activation and expiry out of range", kv, func(s *State) Outcome {
			msg := propose(seats[0].Signer, "p-3", 1, powerMsg("v5", pub(9), 1, uint64(testHeight)))
			msg.ExpiryUnixSeconds = 0
			return s.ProposeChange(msg)
		}, CodeOutOfRange},
		// op_approve require_fields (1455) runs before council_ready (1457).
		// Round 2: DECISION_BLOCKED.
		{"approve: empty proposal id on a blocked council", blockedKV, func(s *State) Outcome {
			return s.ApproveChange(approve(seats[0].Signer, "a-1", ""))
		}, CodeMalformed},
		// op_approve require_fields (1455) runs before the seat check (1460).
		// Round 2: UNAUTHORIZED.
		{"approve: empty business id from a non-seat", kv, func(s *State) Outcome {
			return s.ApproveChange(approve(outsider, "", "p-x"))
		}, CodeMalformed},
		// op_approve seat check (1460) runs before the proposal lookup
		// (1465). Round 2: MALFORMED (bad id checked first).
		{"approve: bad proposal id from a non-seat", kv, func(s *State) Outcome {
			return s.ApproveChange(approve(outsider, "a-2", "Bad!"))
		}, CodeUnauthorized},
		// op_approve proposal lookup (1465-1467): no record has a bad id.
		// Round 2: MALFORMED.
		{"approve: bad proposal id from a seat", kv, func(s *State) Outcome {
			return s.ApproveChange(approve(seats[1].Signer, "a-3", "Bad!"))
		}, CodeNotFound},
		// op_execute require_fields (1488) runs before check_role (1490).
		// Round 2: UNAUTHORIZED.
		{"execute: empty business id from a non-submitter", kv, func(s *State) Outcome {
			return s.ExecuteChange(execute(outsider, "", "p-x"))
		}, CodeMalformed},
		// op_execute proposal lookup (1499-1501) after the business gate
		// (1496). Round 2: BOUNDS.
		{"execute: bad proposal id", kv, func(s *State) Outcome {
			return s.ExecuteChange(execute(authority, "ex-1", "Bad!"))
		}, CodeNotFound},
	}
	for _, row := range rows {
		before := dumpKV(row.kv)
		out := row.run(loadAt(t, row.kv, testHeight, testTime))
		if out.Code != row.code || out.Keep {
			t.Fatalf("%s: got %+v, want %s", row.name, out, row.code)
		}
		wantUnchanged(t, row.kv, before, row.name)
	}
	// A nil message is MALFORMED on every path.
	st := loadAt(t, kv, testHeight, testTime)
	for i, out := range []Outcome{
		st.ProposeChange(nil),
		st.ApproveChange(nil),
		st.ExecuteChange((*authorityv1.MsgExecuteChange)(nil)),
	} {
		if out.Code != CodeMalformed {
			t.Fatalf("nil message %d: %+v", i, out)
		}
	}
}
