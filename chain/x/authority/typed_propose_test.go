package authority

import (
	"bytes"
	"crypto/sha256"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

var _ PolicyWriter = localPolicy{}

// nonGovernedKeys are limit keys outside the governed_limit_caps table: the
// two safety caps, two compile-time quotas, and an unknown key. All match the
// id pattern, so only D-1 refuses them at propose.
var nonGovernedKeys = []string{"max_tx_bytes", "max_gas_per_tx", "max_open_proposals", "max_pending_changes", "max_unknown_limit"}

// TestGovernedLimitTypedAtProposeD1: a governed limit payload must name a key
// of the msgkit table and a value in 1..ceiling at propose (D-1), or the
// propose is BOUNDS and writes nothing. The spec accepts such payloads at
// propose. Every key is accepted at 1 and at its ceiling and refused at
// ceiling+1; a value of 0 is BOUNDS earlier (parse). The D-1 check comes
// right after the payload checks: it wins over every later check (expiry,
// business id replay and idempotent retry, duplicate proposal id, stale base)
// and loses to every earlier one (council, seat, parameter key binding,
// digest, dec- prefix). Each refusal is also checked through Keeper.Propose
// on a real IAVL store.
func TestGovernedLimitTypedAtProposeD1(t *testing.T) {
	type row struct {
		name  string
		key   string
		value uint64
		want  string
	}
	var rows []row
	for _, key := range msgkit.LimitKeys() {
		ceiling, _ := msgkit.Ceiling(key)
		rows = append(rows,
			row{key + " at 1", key, 1, ""},
			row{key + " at its ceiling", key, ceiling, ""},
			row{key + " at ceiling+1", key, ceiling + 1, CodeBounds},
			row{key + " at 0", key, 0, CodeBounds},
		)
	}
	for _, key := range nonGovernedKeys {
		rows = append(rows, row{key + " (not governed)", key, 1, CodeBounds})
	}
	for i, r := range rows {
		kv, seats, _ := readyStore(t)
		before := dumpKV(kv)
		msg := governedProposal(r.key, r.value)
		id := "d1-" + strconv.Itoa(i)
		got := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, id, 1, msg))
		if got.Code != r.want {
			t.Fatalf("%s: %+v, want %q", r.name, got, r.want)
		}
		if r.want != "" {
			wantUnchanged(t, kv, before, r.name)
			continue
		}
		if rec := recordOf(t, kv, id); rec.Decoded.LimitKey != r.key || rec.Decoded.LimitValue != r.value {
			t.Fatalf("%s: stored %+v", r.name, rec.Decoded)
		}
	}

	// D-1 wins over every later check.
	over := func() *authorityv1.MsgProposeChange { return governedProposal(msgkit.LimitMaxPageLimit, 101) }
	kv, seats, _ := readyStore(t)
	queueAt(t, kv, seats, testHeight, testTime, "taken", governedMsg(msgkit.LimitMaxPageLimit, 10))
	if out := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(bytes20(1), "ex-taken", "taken")); !out.Success() {
		t.Fatal(out)
	}
	idem := propose(seats[1].Signer, "idem", 2, over())
	_, _, canon, out := CanonicalFromProto(idem.GetPayload(), idem.GetPayloadDigest().GetValue())
	if !out.Success() {
		t.Fatal(out)
	}
	// A business row holding the digest this exact propose computes, as an
	// earlier binary would have stored it: without D-1 the retry would be
	// an idempotent success.
	kv.Set(businessKey(idem.GetBusinessId()), businessDigest(map[string]any{
		"base_policy_version": "2",
		"expiry_unix_seconds": strconv.FormatInt(idem.GetExpiryUnixSeconds(), 10),
		"parameter_key":       idem.GetParameterKey(),
		"payload":             canon,
		"proposal_id":         idem.GetProposalId(),
	}))
	later := []struct {
		name string
		msg  *authorityv1.MsgProposeChange
		code string // the code without D-1
	}{
		{"expired expiry", func() *authorityv1.MsgProposeChange {
			m := propose(seats[1].Signer, "late", 2, over())
			m.ExpiryUnixSeconds = testTime
			return m
		}(), CodeExpired},
		{"stale base", propose(seats[1].Signer, "stale", 1, over()), CodeStale},
		{"duplicate proposal id", func() *authorityv1.MsgProposeChange {
			m := propose(seats[1].Signer, "taken", 2, over())
			m.BusinessId = "b-dup"
			return m
		}(), CodeDuplicate},
		{"replayed business id", func() *authorityv1.MsgProposeChange {
			m := propose(seats[1].Signer, "replay", 2, over())
			m.BusinessId = "b-taken"
			return m
		}(), CodeReplay},
		{"idempotent retry", idem, "idempotent"},
	}
	for _, r := range later {
		before := dumpKV(kv)
		if got := loadAt(t, kv, testHeight, testTime).ProposeChange(r.msg); got.Code != CodeBounds || got.Idempotent {
			t.Fatalf("%s: %+v, want BOUNDS (without D-1: %s)", r.name, got, r.code)
		}
		wantUnchanged(t, kv, before, r.name)
		// The same message with an in-range value reaches the later check.
		inRange := copyProposeFields(r.msg)
		setGoverned(inRange, msgkit.LimitMaxPageLimit, 100)
		got := loadAt(t, kv, testHeight, testTime).ProposeChange(inRange)
		if r.code == "idempotent" {
			if got.Code != CodeReplay {
				t.Fatalf("%s in range: %+v (the business row binds the over-ceiling digest)", r.name, got)
			}
			continue
		}
		if got.Code != r.code {
			t.Fatalf("%s in range: %+v, want %s", r.name, got, r.code)
		}
	}

	// Every earlier check wins over D-1.
	earlier := []struct {
		name string
		edit func(*authorityv1.MsgProposeChange)
		load func() *State
		want string
	}{
		{"not a seat", func(m *authorityv1.MsgProposeChange) { m.Signer = bytes20(9) }, nil, CodeUnauthorized},
		{"parameter key bound to another key", func(m *authorityv1.MsgProposeChange) { m.ParameterKey = "max_open_offers" }, nil, CodeMalformed},
		{"digest of another payload", func(m *authorityv1.MsgProposeChange) {
			m.PayloadDigest = sha256Digest(pub(5))
		}, nil, CodeIntegrity},
		{"digest algorithm", func(m *authorityv1.MsgProposeChange) {
			m.PayloadDigest = &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED, Value: m.GetPayloadDigest().GetValue()}
		}, nil, CodeMalformed},
		{"off devnet", func(*authorityv1.MsgProposeChange) {}, func() *State {
			st, err := LoadState(kv, "shortvideo-live", testHeight, testTime)
			if err != nil {
				t.Fatal(err)
			}
			return st
		}, CodeDecisionBlocked},
	}
	for _, r := range earlier {
		msg := propose(seats[1].Signer, "early", 2, over())
		r.edit(msg)
		st := loadAt(t, kv, testHeight, testTime)
		if r.load != nil {
			st = r.load()
		}
		before := dumpKV(kv)
		if got := st.ProposeChange(msg); got.Code != r.want {
			t.Fatalf("%s: %+v, want %s", r.name, got, r.want)
		}
		wantUnchanged(t, kv, before, r.name)
	}
	before := dumpKV(kv)
	if got := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[1].Signer, "dec", 2, governedProposal("dec-008", 1))); got.Code != CodeForbidden {
		t.Fatalf("dec- key: %+v", got)
	}
	wantUnchanged(t, kv, before, "dec- key")

	// Through the keeper handler: BOUNDS, an authority_result event, and
	// the IAVL store unchanged.
	env := readyKeeper(t)
	for _, key := range append([]string{msgkit.LimitMaxEvidenceBatch}, nonGovernedKeys...) {
		value := uint64(1)
		if key == msgkit.LimitMaxEvidenceBatch {
			value = 33
		}
		before := dumpKV(env.kv())
		ctx := env.txCtx()
		_, err := env.keeper.Propose(ctx, propose(env.seats[0].Signer, "k-"+strings.ReplaceAll(key, "_", "-"), 1, governedProposal(key, value)))
		if !svnerr.ErrBounds.Is(err) {
			t.Fatalf("keeper %s=%d: %v", key, value, err)
		}
		if code, _ := resultCode(t, ctx); code != CodeBounds {
			t.Fatalf("keeper %s: authority_result %q", key, code)
		}
		wantUnchanged(t, env.kv(), before, "keeper "+key)
	}
}

// TestSafetyCapsNotGovernable: no council payload can change the transaction
// size or gas caps. A limit key naming either is BOUNDS at propose (D-1)
// and, written as a QUEUED record (no message can store one), BOUNDS at
// execute with the record left QUEUED; no governed row is ever written.
func TestSafetyCapsNotGovernable(t *testing.T) {
	for _, key := range []string{"max_tx_bytes", "max_gas_per_tx"} {
		for _, value := range []uint64{1, 65536, 20_000_000} {
			kv, seats, authority := readyStore(t)
			before := dumpKV(kv)
			if got := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, "cap", 1, governedProposal(key, value))); got.Code != CodeBounds {
				t.Fatalf("propose %s=%d: %+v", key, value, got)
			}
			wantUnchanged(t, kv, before, "propose "+key)
			putQueuedGoverned(t, kv, seats, "cap", key, value)
			before = dumpKV(kv)
			if got := loadAt(t, kv, testHeight, testTime).ExecuteChange(execute(authority, "ex-cap", "cap")); got.Code != CodeBounds {
				t.Fatalf("execute %s=%d: %+v", key, value, got)
			}
			wantUnchanged(t, kv, before, "execute "+key)
			if kv.Has(governedKey(key)) {
				t.Fatalf("%s has a governed row", key)
			}
		}
	}
}

// TestExecuteRechecksCeiling: after D-1 no message can store a governed
// limit outside the table, so the execute-side check (PolicyWriter.SetLimit)
// is reached only through a record written directly. An over-ceiling value
// and an unknown key are BOUNDS at execute, the proposal stays QUEUED, and
// the store is byte-identical; the same record shape with an in-range value
// executes, so the BOUNDS comes from the recheck and not an earlier check.
func TestExecuteRechecksCeiling(t *testing.T) {
	rows := []struct {
		key   string
		value uint64
		want  string
	}{
		{msgkit.LimitMaxPageLimit, 101, CodeBounds},
		{msgkit.LimitMaxPackageBytes, 268435457, CodeBounds},
		{"max_unknown_limit", 1, CodeBounds},
		{msgkit.LimitMaxPageLimit, 100, ""},
		{msgkit.LimitMaxEvidenceBatch, 1, ""},
	}
	for _, r := range rows {
		kv, seats, authority := readyStore(t)
		putQueuedGoverned(t, kv, seats, "rq", r.key, r.value)
		before := dumpKV(kv)
		st := loadAt(t, kv, testHeight, testTime)
		got := st.ExecuteChange(execute(authority, "ex-rq", "rq"))
		if got.Code != r.want {
			t.Fatalf("%s=%d: %+v, want %q", r.key, r.value, got, r.want)
		}
		if r.want != "" {
			wantUnchanged(t, kv, before, r.key)
			if st.executed != nil || recordOf(t, kv, "rq").state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				t.Fatalf("%s: executed %+v", r.key, st.executed)
			}
			continue
		}
		raw := kv.Get(governedKey(r.key))
		if value, good := decodeU64(raw); !good || value != r.value {
			t.Fatalf("%s: stored %x", r.key, raw)
		}
	}
}

// TestLocalPolicyWriter: SetLimit checks the key and the ceiling before it
// adds one write to the execute's batch and writes nothing to the store
// itself; the methods the layout cannot serve refuse with MALFORMED and add
// nothing.
func TestLocalPolicyWriter(t *testing.T) {
	kv, _, _ := readyStore(t)
	before := dumpKV(kv)
	st := loadAt(t, kv, testHeight, testTime)
	var b batch
	w := st.policyWriter(&b)
	if out := w.SetLimit(msgkit.LimitMaxOpenOffers, 7, "p"); !out.Success() {
		t.Fatal(out)
	}
	if len(b.keys) != 1 || !bytes.Equal(b.keys[0], governedKey(msgkit.LimitMaxOpenOffers)) || !bytes.Equal(b.values[0], encodeU64(7)) {
		t.Fatalf("batch %x %x", b.keys, b.values)
	}
	wantUnchanged(t, kv, before, "SetLimit before the flush")
	for _, r := range []struct {
		key   string
		value uint64
	}{{msgkit.LimitMaxOpenOffers, 0}, {msgkit.LimitMaxOpenOffers, 1025}, {"max_tx_bytes", 1}, {"", 1}} {
		if out := w.SetLimit(r.key, r.value, "p"); out.Code != CodeBounds {
			t.Fatalf("SetLimit(%q, %d): %+v", r.key, r.value, out)
		}
	}
	if out := w.SchedulePending(msgkit.LimitMaxOpenOffers, 7, 100, "p", 2); out.Code != CodeMalformed {
		t.Fatalf("SchedulePending: %+v", out)
	}
	if out := w.ScheduleUpgrade("rehearsal-1", 300, pub(1), "p"); out.Code != CodeMalformed {
		t.Fatalf("ScheduleUpgrade: %+v", out)
	}
	if out := w.CancelUpgrade("rehearsal-1"); out.Code != CodeMalformed {
		t.Fatalf("CancelUpgrade: %+v", out)
	}
	if w.UpgradeApplied("rehearsal-1") {
		t.Fatal("UpgradeApplied")
	}
	if len(b.keys) != 1 {
		t.Fatalf("refusals added writes: %x", b.keys)
	}
	wantUnchanged(t, kv, before, "writer refusals")
}

// TestPolicyWriterCallSites: in the non-test files of this package, every
// call of a PolicyWriter method, and of policyWriter, is inside
// applyPayload, so no other path writes governed values.
func TestPolicyWriterCallSites(t *testing.T) {
	methods := []string{"SetLimit", "SchedulePending", "ScheduleUpgrade", "CancelUpgrade", "UpgradeApplied", "policyWriter"}
	calls := 0
	for path, file := range parsePackage(t, ".") {
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				var name string
				switch callee := call.Fun.(type) {
				case *ast.SelectorExpr:
					name = callee.Sel.Name
				case *ast.Ident:
					name = callee.Name
				}
				if !slices.Contains(methods, name) {
					return true
				}
				if fn.Name.Name != "applyPayload" {
					t.Errorf("%s: %s calls %s; only applyPayload may", path, fn.Name.Name, name)
				}
				calls++
				return true
			})
		}
	}
	if calls == 0 {
		t.Fatal("no PolicyWriter call found: the scan is broken")
	}
}

// parsePackage parses the non-test Go files of dir.
func parsePackage(t *testing.T, dir string) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ast.File{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		out[path] = file
	}
	if len(out) == 0 {
		t.Fatalf("no non-test Go files in %s", dir)
	}
	return out
}

// governedProposal is governedMsg for any key and value, including those the
// chain refuses: the payload digest is computed from the canonical text,
// which payloadMsg cannot do for a value of 0.
func governedProposal(key string, value uint64) *authorityv1.MsgProposeChange {
	payload := &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
		GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value},
	}}
	canon := `{"governedLimit":{"limitKey":"` + key + `","value":"` + strconv.FormatUint(value, 10) + `"}}`
	sum := sha256.Sum256([]byte(canon))
	return &authorityv1.MsgProposeChange{
		ParameterKey:  key,
		Payload:       payload,
		PayloadDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]},
	}
}

// copyProposeFields copies the fields of a propose message that the tests above edit.
func copyProposeFields(msg *authorityv1.MsgProposeChange) *authorityv1.MsgProposeChange {
	return &authorityv1.MsgProposeChange{
		Signer: msg.GetSigner(), BusinessId: msg.GetBusinessId(), ProposalId: msg.GetProposalId(),
		ParameterKey: msg.GetParameterKey(), BasePolicyVersion: msg.GetBasePolicyVersion(),
		ExpiryUnixSeconds: msg.GetExpiryUnixSeconds(),
	}
}

// setGoverned gives msg a governed limit payload and its digest.
func setGoverned(msg *authorityv1.MsgProposeChange, key string, value uint64) {
	body := governedProposal(key, value)
	msg.Payload, msg.PayloadDigest = body.GetPayload(), body.GetPayloadDigest()
}

// putQueuedGoverned writes a QUEUED governed limit proposal on base version 1
// straight into the store, approved by seats 1..3 with execute_after at
// testTime, and the proposer's open count. No message can store a key or
// value outside the table after D-1; this is the test hook that reaches the
// execute-side check.
func putQueuedGoverned(t testing.TB, kv KV, seats []testSeat, id, key string, value uint64) {
	t.Helper()
	canon, out := governedCanonical(&authorityv1.GovernedLimitChange{LimitKey: key, Value: value})
	if !out.Success() {
		t.Fatal(out)
	}
	sum := sha256.Sum256([]byte(canon))
	approvers := []string{seats[0].ID, seats[1].ID, seats[2].ID}
	keys := [][]byte{bytes.Clone(seats[0].Pub), bytes.Clone(seats[1].Pub), bytes.Clone(seats[2].Pub)}
	slices.Sort(approvers)
	slices.SortFunc(keys, bytes.Compare)
	rec := &proposalRecord{
		ProposalID: id, ParameterKey: key, BasePolicyVersion: 1, PayloadDigest: sum[:],
		State:             int32(authorityv1.ProposalState_PROPOSAL_STATE_QUEUED),
		ExpiryUnixSeconds: testExpiry, ExecuteAfterUnixSeconds: testTime,
		ApproverKeys: keys, BusinessID: "b-" + id, Proposer: seats[0].ID, Approvers: approvers,
		Payload: canon, Decoded: decodedWire{Kind: kindGovernedLimit, LimitKey: key, LimitValue: value},
	}
	raw, err := encodeRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	kv.Set(proposalKey(id), raw)
	kv.Set(openKey(seats[0].ID, 1), encodeU64(1))
}
