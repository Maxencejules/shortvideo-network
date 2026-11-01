package authority

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/store/v2/cachekv"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// FuzzStateTransitions runs bounded sequences of authority operations
// against the per-object store, the way the keeper runs messages: each
// operation runs on a cache branch (cachekv) of the committed store; a
// success or keep outcome is written back; any other outcome (a refusal, an
// idempotent retry, an ApplyDue halt) must leave the branch byte-identical
// to the committed store (dumpKV: every key and value). After every
// committed operation the harness checks the invariants listed on
// checkInvariants. An ApplyDue halt also fails the run: council messages
// must never schedule a change that stops the chain. TestFuzzCorpusReach
// shows that the seeds and the decoder reach QUEUED, EXECUTED, REJECTED,
// EXPIRED and an ApplyDue that changes the set.
func FuzzStateTransitions(f *testing.F) {
	for _, seed := range fuzzStateSeeds() {
		f.Add(seed.encode())
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		runFuzzSequence(t, raw)
	})
}

// Sequence encoding. A sequence is pairs of bytes (b, v), at most
// fuzzMaxOps pairs; later bytes are ignored and a final odd byte gets v = 0.
//
//	b bits 0-2  the operation, b % 8 (op* constants)
//	b bits 3-4  the seat index and business-id letter, int(b>>3) % 4. They do
//	            not depend on the operation bits, so every seat can propose,
//	            approve and act in every role.
//	b bits 5-7  flags x = b >> 5 (flag* constants; meaning per operation)
//	v           the payload variant of the operation
//
// Key pool for node n (fuzzKey): 0 the key n holds now (a fresh key for a
// new node: power-only change or add), 1 a current validator key picked by n
// (often another node's: duplicate key), 2 and 3 fresh keys (key change).
const (
	// v: node (bits 0-2), key pool (3-4), power 0..3 (5-6), far activation (7).
	// flagExtra with power bits 0: power safeMax/4.
	opProposeValidator byte = iota
	// v: node (0-2), unbound key (3), key pool when unbound (4-5), far
	// activation (7). Power 0. flagExtra reuses the last proposal id.
	opProposeRemoval
	// v: payload (0-2: governed limits 0-4, unknown limit 5, admit 6,
	// force-withdraw 7), value 1..16 (3-6), one above the ceiling (7).
	// flagExtra reuses the last proposal id.
	opProposeGoverned
	// v bit 7 clear: the OPEN proposal with the most approvals (pick); set:
	// proposal v&0x7f modulo the count. flagTime: a fresh business id.
	// x == 7: a missing proposal.
	opApprove
	// v as for approve, with a QUEUED proposal. x == 6: signed by the service
	// account; x == 7: signed by the seat.
	opExecute
	// v: 1 + v%4 blocks of fuzzBlockTime seconds, then ApplyDue.
	opHeightStep
	// v: fuzzTimeSteps[v%8] seconds and one block, then ApplyDue.
	opTimeStep
	// v: v&7 picks a wrong-role action or an engine-only rotation; v>>3 picks
	// the new key or address.
	opWrongRole
)

// Flag bits of b >> 5.
const (
	// flagTime: propose with a unix-time activation instead of a height;
	// approve with a fresh business id.
	flagTime byte = 1 << iota
	// flagFar: propose with a far expiry instead of a near one.
	flagFar
	// flagExtra: see opProposeValidator, opProposeRemoval, opProposeGoverned.
	flagExtra
)

// Whole-flag values for approve and execute.
const (
	approveMissing   byte = 7
	executeByService byte = 6
	executeBySeat    byte = 7
)

const (
	fuzzMaxOps                = 64
	fuzzBigPower              = safeMax / 4
	fuzzNearExpiry      int64 = 900
	fuzzFarExpiry       int64 = 1_000_000
	fuzzExpiryJump      int64 = 2_000_000
	fuzzNearTimeAhead   int64 = 60
	fuzzFarTimeAhead    int64 = 1800
	fuzzNearHeightAhead       = 2
	fuzzFarHeightAhead        = 12
	fuzzBlockTime       int64 = 5
)

// fuzzTimeSteps are the time steps. 1800 and 3600 pass a near expiry; the
// last one passes every expiry.
var fuzzTimeSteps = [8]int64{15, 30, 60, 120, 600, 1800, 3600, fuzzExpiryJump}

// fuzzLimits are the governed limit keys; the last one is unknown and fails
// at execute with BOUNDS.
var fuzzLimits = [6]string{"max_evidence_batch", "max_page_limit", "max_open_offers", "max_expiry_steps", "max_package_bytes", "max_unknown_limit"}

// fuzzCounters counts what one or more sequences reached.
type fuzzCounters struct {
	Ops             int // operations decoded
	Committed       int // success or keep outcomes written to the store
	Queued          int // approvals that moved a proposal to QUEUED
	Executed        int // successful executes
	Rejected        int // executes kept as REJECTED (FORBIDDEN)
	Expired         int // approvals or executes kept as EXPIRED
	ApplyDueChanged int // ApplyDue calls that returned changed == true
	Bounds          int // refusals with ERROR_CODE_BOUNDS
	Idempotent      int // idempotent retries
	Refused         int // all refusals; Bounds is part of it
}

func (c *fuzzCounters) add(o fuzzCounters) {
	c.Ops += o.Ops
	c.Committed += o.Committed
	c.Queued += o.Queued
	c.Executed += o.Executed
	c.Rejected += o.Rejected
	c.Expired += o.Expired
	c.ApplyDueChanged += o.ApplyDueChanged
	c.Bounds += o.Bounds
	c.Idempotent += o.Idempotent
	c.Refused += o.Refused
}

// fuzzHarness is the committed store plus what the harness knows about it.
type fuzzHarness struct {
	t         testing.TB
	base      storetypes.KVStore
	seats     []testSeat
	retired   [][]byte
	authority []byte
	service   []byte
	height    int64
	unix      int64
	proposals []string        // ids of accepted proposals, in order
	executed  map[string]bool // ids whose execute succeeded
	executes  uint64
	records   map[string]fuzzRecord // decode cache, by proposal id
	dump      []byte                // dumpKV(base) after the last commit
	vals      []byte                // validatorBytes of the committed set
	c         fuzzCounters
	trace     []string // one token per operation (traceToken)
}

// fuzzRecord is the part of a proposal record the harness uses.
type fuzzRecord struct {
	raw       []byte
	state     authorityv1.ProposalState
	proposer  string
	base      uint64
	approvers int
}

// fuzzResult is one operation's outcome and what to do if it commits.
type fuzzResult struct {
	out      Outcome
	target   string // proposal id the operation names, if any
	want     string // exact refusal code a wrong-role action must get
	due      bool   // the operation was an ApplyDue
	changed  bool   // ApplyDue changed the set
	before   []*authorityv1.ValidatorPower
	onCommit func()
}

// runFuzzSequence decodes raw and runs it on a fresh readyStore. It fails t
// on the first broken property and returns the counters.
func runFuzzSequence(t testing.TB, raw []byte) fuzzCounters {
	t.Helper()
	c, _ := runFuzzTrace(t, raw)
	return c
}

// traceToken names one operation's outcome: "ok" for a success, "apply" for
// an ApplyDue that changed the set, "idle" for one that did not, "idem" for
// an idempotent retry, "keep:" and the code for a keep outcome, and the code
// for a refusal. Codes drop the ERROR_CODE_ prefix.
func traceToken(r fuzzResult) string {
	short := strings.TrimPrefix(r.out.Code, "ERROR_CODE_")
	switch {
	case r.out.Idempotent:
		return "idem"
	case r.out.Keep:
		return "keep:" + short
	case !r.out.Success():
		return short
	case r.due && r.changed:
		return "apply"
	case r.due:
		return "idle"
	default:
		return "ok"
	}
}

// runFuzzTrace is runFuzzSequence that also returns one traceToken per
// operation, so a named seed can pin the exact outcome of each step.
func runFuzzTrace(t testing.TB, raw []byte) (fuzzCounters, []string) {
	t.Helper()
	kv, seats, authority := readyStore(t)
	h := &fuzzHarness{
		t:         t,
		base:      kv,
		seats:     slices.Clone(seats),
		authority: authority,
		service:   bytes20(2),
		height:    testHeight,
		unix:      testTime,
		executed:  map[string]bool{},
		records:   map[string]fuzzRecord{},
		dump:      dumpKV(kv),
	}
	h.vals = h.validatorBytes(h.load(kv, h.height, h.unix).Validators())
	for i := 0; i < len(raw) && h.c.Ops < fuzzMaxOps; i += 2 {
		var v byte
		if i+1 < len(raw) {
			v = raw[i+1]
		}
		h.step(raw[i], v)
	}
	return h.c, h.trace
}

func (h *fuzzHarness) load(kv KV, height, unix int64) *State {
	h.t.Helper()
	st, err := LoadState(kv, testChainID, height, unix)
	if err != nil {
		h.t.Fatalf("op %d: LoadState: %v", h.c.Ops-1, err)
	}
	return st
}

// step runs one operation on a cache branch and commits or discards it.
func (h *fuzzHarness) step(b, v byte) {
	t := h.t
	t.Helper()
	index := h.c.Ops
	h.c.Ops++
	op := b % 8
	before, valsBefore := h.dump, h.vals
	height, unix := h.height, h.unix
	switch op {
	case opHeightStep:
		blocks := 1 + int64(v%4)
		height += blocks
		unix += blocks * fuzzBlockTime
	case opTimeStep:
		height++
		unix += fuzzTimeSteps[v%8]
	}
	trial := cachekv.NewStore(h.base)
	r := h.run(h.load(trial, height, unix), op, int(b>>3)%4, b>>5, v)
	h.trace = append(h.trace, traceToken(r))
	if r.want != "" && r.out.Code != r.want {
		t.Fatalf("op %d (%d/%d): wrong-role action got %+v, want %s", index, op, v, r.out, r.want)
	}
	switch {
	case r.out.Idempotent:
		h.c.Idempotent++
		h.requireUnchanged(index, op, r.out, trial, before)
		return
	case !r.out.Success() && !r.out.Keep:
		h.requireUnchanged(index, op, r.out, trial, before)
		if r.due {
			t.Fatalf("op %d: ApplyDue halted with %s on a store built only by messages", index, r.out.Code)
		}
		h.c.Refused++
		if r.out.Code == CodeBounds {
			h.c.Bounds++
		}
		return
	}
	trial.Write()
	h.c.Committed++
	h.height, h.unix = height, unix
	if r.onCommit != nil {
		r.onCommit()
	}
	if r.out.Keep {
		h.checkKeep(index, r)
	}
	if r.due && r.changed {
		h.c.ApplyDueChanged++
		if !bytes.Equal(h.validatorBytes(r.before), valsBefore) {
			t.Fatalf("op %d: ApplyDue reported a previous set that is not the committed one", index)
		}
	}
	h.checkInvariants(index, valsBefore, r.due && r.changed)
}

func (h *fuzzHarness) requireUnchanged(index int, op byte, out Outcome, trial KV, before []byte) {
	h.t.Helper()
	if got := dumpKV(trial); !bytes.Equal(got, before) {
		h.t.Fatalf("op %d (%d) outcome %+v wrote to the store (%d bytes before, %d after)", index, op, out, len(before), len(got))
	}
}

// checkKeep checks that a keep outcome left its proposal with the matching
// terminal mark, and counts it.
func (h *fuzzHarness) checkKeep(index int, r fuzzResult) {
	h.t.Helper()
	want := authorityv1.ProposalState_PROPOSAL_STATE_UNSPECIFIED
	switch r.out.Code {
	case CodeExpired:
		h.c.Expired++
		want = authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED
	case CodeForbidden:
		h.c.Rejected++
		want = authorityv1.ProposalState_PROPOSAL_STATE_REJECTED
	default:
		h.t.Fatalf("op %d: keep outcome with code %s", index, r.out.Code)
	}
	if got := h.stateOf(r.target); got != want {
		h.t.Fatalf("op %d: keep %s left %s as %v", index, r.out.Code, r.target, got)
	}
}

// stateOf is the committed state of one accepted proposal.
func (h *fuzzHarness) stateOf(id string) authorityv1.ProposalState {
	h.t.Helper()
	raw := h.base.Get(proposalKey(id))
	if raw == nil {
		h.t.Fatalf("op %d: proposal %s has no record", h.c.Ops-1, id)
	}
	return h.summary(id, raw).state
}

// summary decodes one record, reusing the last decode while its bytes are
// unchanged, and checks its proposer and approvers.
func (h *fuzzHarness) summary(id string, raw []byte) fuzzRecord {
	h.t.Helper()
	if cached, ok := h.records[id]; ok && bytes.Equal(cached.raw, raw) {
		return cached
	}
	rec, err := decodeRecord(id, raw)
	if err != nil {
		h.t.Fatalf("op %d: %v", h.c.Ops-1, err)
	}
	if !h.isSeat(rec.Proposer) {
		h.t.Fatalf("op %d: proposal %s has proposer %q", h.c.Ops-1, id, rec.Proposer)
	}
	if len(rec.Approvers) != len(rec.ApproverKeys) || len(rec.Approvers) > ApprovedSeats {
		h.t.Fatalf("op %d: proposal %s has approvers %v and %d keys", h.c.Ops-1, id, rec.Approvers, len(rec.ApproverKeys))
	}
	for i, seat := range rec.Approvers {
		if !h.isSeat(seat) || (i > 0 && rec.Approvers[i-1] >= seat) {
			h.t.Fatalf("op %d: proposal %s approvers %v are not distinct sorted seat ids", h.c.Ops-1, id, rec.Approvers)
		}
	}
	quorum := len(rec.Approvers) >= ApprovedThreshold
	switch rec.state() {
	case authorityv1.ProposalState_PROPOSAL_STATE_OPEN:
		if quorum {
			h.t.Fatalf("op %d: proposal %s is OPEN with %d approvers", h.c.Ops-1, id, len(rec.Approvers))
		}
	case authorityv1.ProposalState_PROPOSAL_STATE_QUEUED, authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED,
		authorityv1.ProposalState_PROPOSAL_STATE_REJECTED:
		if !quorum {
			h.t.Fatalf("op %d: proposal %s is %v with %d approvers", h.c.Ops-1, id, rec.state(), len(rec.Approvers))
		}
	}
	out := fuzzRecord{raw: bytes.Clone(raw), state: rec.state(), proposer: rec.Proposer, base: rec.BasePolicyVersion, approvers: len(rec.Approvers)}
	h.records[id] = out
	return out
}

func (h *fuzzHarness) isSeat(id string) bool {
	for _, seat := range h.seats {
		if seat.ID == id {
			return true
		}
	}
	return false
}

func (h *fuzzHarness) validatorBytes(vals []*authorityv1.ValidatorPower) []byte {
	h.t.Helper()
	raw, err := marshalState(&authorityv1.AuthorityState{Validators: vals})
	if err != nil {
		h.t.Fatal(err)
	}
	return raw
}

// checkInvariants reads the committed store back (one dumpKV pass, kept as
// the next operation's "before") and checks:
//   - the set has at least four validators (no exemption for an empty set),
//     positive powers, distinct consensus keys, max*3 < total and total <=
//     safeMax;
//   - the set bytes are unchanged unless this operation was an ApplyDue that
//     returned changed == true;
//   - the policy version is 1 + the successful executes;
//   - each record has a seat proposer, distinct sorted seat approvers, and
//     an approver count that matches its state;
//   - for each seat and base version, the open count (0x14) equals the
//     seat's OPEN and QUEUED records on that base and is at most
//     MaxOpenProposalsPerSeat; no count key is zero or orphaned;
//   - an EXECUTED record was executed by the harness, and an executed id is
//     EXECUTED or EXPIRED (the spec relabel);
//   - pending entries are at most MaxPendingChanges, of one activation kind,
//     not yet due, and each one comes from a successful execute;
//   - the ante admission rows (0x02) are exactly the config submitters.
func (h *fuzzHarness) checkInvariants(index int, valsBefore []byte, changed bool) {
	t := h.t
	t.Helper()
	st := h.load(h.base, h.height, h.unix)
	vals := st.Validators()
	if len(vals) < genesis.MinValidators {
		t.Fatalf("op %d: %d validators", index, len(vals))
	}
	keys := map[string]bool{}
	var total, maxPower int64
	for _, row := range vals {
		power := row.GetPower()
		if power <= 0 {
			t.Fatalf("op %d: validator %s has power %d", index, row.GetNodeId(), power)
		}
		if keys[string(row.GetConsensusPubkey())] {
			t.Fatalf("op %d: consensus key %x is held twice", index, row.GetConsensusPubkey())
		}
		keys[string(row.GetConsensusPubkey())] = true
		if power > safeMax-total {
			t.Fatalf("op %d: total power passes the safe integer maximum", index)
		}
		total += power
		maxPower = max(maxPower, power)
	}
	if maxPower*3 >= total {
		t.Fatalf("op %d: max power %d is not below a third of total %d", index, maxPower, total)
	}
	h.vals = h.validatorBytes(vals)
	if !changed && !bytes.Equal(valsBefore, h.vals) {
		t.Fatalf("op %d: the validator set changed outside an ApplyDue", index)
	}
	if got := st.PolicyVersion(); got != 1+h.executes {
		t.Fatalf("op %d: policy version %d after %d executes", index, got, h.executes)
	}

	h.dump = dumpKV(h.base)
	// open and counts are keyed by the 0x14 key suffix: base version (8
	// bytes, big-endian) then seat id.
	open := map[string]uint64{}
	counts := map[string]uint64{}
	admission := map[string][]byte{}
	var pending []*authorityv1.PendingValidatorPower
	walkDump(t, h.dump, func(key, value []byte) {
		switch key[0] {
		case admitPrefix:
			admission[string(key[1:])] = value
		case pendingPrefix:
			entry, err := decodePending(key, value)
			if err != nil {
				t.Fatalf("op %d: %v", index, err)
			}
			pending = append(pending, entry)
		case proposalPrefix:
			id := string(key[1:])
			rec := h.summary(id, value)
			switch rec.state {
			case authorityv1.ProposalState_PROPOSAL_STATE_OPEN, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED:
				open[string(openKey(rec.proposer, rec.base)[1:])]++
			case authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED:
				if !h.executed[id] {
					t.Fatalf("op %d: %s is EXECUTED without a successful execute", index, id)
				}
			}
			if h.executed[id] && rec.state != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED &&
				rec.state != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
				t.Fatalf("op %d: executed proposal %s is %v", index, id, rec.state)
			}
		case openPrefix:
			count, good := decodeU64(value)
			if len(key) < 10 || !good || count == 0 || count > MaxOpenProposalsPerSeat {
				t.Fatalf("op %d: open count key %x holds %x", index, key, value)
			}
			counts[string(key[1:])] = count
		}
	})

	for suffix, want := range open {
		if counts[suffix] != want {
			t.Fatalf("op %d: open count of seat %q at base %d is %d, records say %d", index,
				suffix[8:], binary.BigEndian.Uint64([]byte(suffix[:8])), counts[suffix], want)
		}
		delete(counts, suffix)
	}
	if len(counts) != 0 {
		t.Fatalf("op %d: open count keys without OPEN or QUEUED records %x", index, counts)
	}

	if len(pending) > MaxPendingChanges {
		t.Fatalf("op %d: %d pending entries", index, len(pending))
	}
	for _, entry := range pending {
		kind, _, _ := activationSlot(entry.GetActivation())
		first, _, _ := activationSlot(pending[0].GetActivation())
		if kind != first {
			t.Fatalf("op %d: pending entries mix activation kinds", index)
		}
		if activationDue(entry.GetActivation(), st.Height, st.Time) {
			t.Fatalf("op %d: pending entry %s is due and was not applied", index, entry.GetProposalId())
		}
		if !h.executed[entry.GetProposalId()] {
			t.Fatalf("op %d: pending entry %s does not come from an execute", index, entry.GetProposalId())
		}
	}

	submitters := st.auth().GetSubmitters()
	if len(admission) != len(submitters) {
		t.Fatalf("op %d: %d admission rows for %d submitters", index, len(admission), len(submitters))
	}
	for _, row := range submitters {
		if role, ok := admission[string(row.GetAddress())]; !ok || !bytes.Equal(role, []byte{byte(row.GetRole())}) {
			t.Fatalf("op %d: submitter %x has admission row %x", index, row.GetAddress(), role)
		}
	}
}

// walkDump calls fn for each key and value of a dumpKV image, in order.
func walkDump(t testing.TB, dump []byte, fn func(key, value []byte)) {
	t.Helper()
	field := func() []byte {
		if len(dump) < 8 {
			t.Fatalf("dump is truncated")
		}
		n := binary.BigEndian.Uint64(dump)
		if n > uint64(len(dump)-8) {
			t.Fatalf("dump field of %d bytes passes the end", n)
		}
		out := dump[8 : 8+n]
		dump = dump[8+n:]
		return out
	}
	for len(dump) > 0 {
		key := field()
		value := field()
		if len(key) == 0 {
			t.Fatalf("dump has an empty key")
		}
		fn(key, value)
	}
}

// run decodes and applies one operation on st.
func (h *fuzzHarness) run(st *State, op byte, seat int, x, v byte) fuzzResult {
	switch op {
	case opProposeValidator, opProposeRemoval, opProposeGoverned:
		return h.propose(st, op, seat, x, v)
	case opApprove:
		return h.approve(st, seat, x, v)
	case opExecute:
		return h.execute(st, seat, x, v)
	case opHeightStep, opTimeStep:
		before, changed, out := st.ApplyDue()
		return fuzzResult{out: out, due: true, changed: changed, before: before}
	default:
		return h.wrongRole(st, seat, v)
	}
}

func fuzzLetter(seat int) string {
	return string(rune('a' + seat))
}

// fuzzNode is the node the node bits name: v1..v6, with v5 and v6 (adds)
// twice as often.
func fuzzNode(nodeBits byte) (string, byte) {
	number := [8]byte{1, 2, 3, 4, 5, 6, 5, 6}[nodeBits&7]
	return "v" + strconv.Itoa(int(number)), number
}

// fuzzKey is the key pool (see the encoding comment): 0 the key the node
// holds now, or pub(49+n) for a new node vn; 1 the key of the validator after
// the node's index in key order; 2 and 3 the fresh keys pub(58) and pub(59).
func fuzzKey(vals []*authorityv1.ValidatorPower, nodeBits, pool byte) []byte {
	node, number := fuzzNode(nodeBits)
	switch pool & 3 {
	case 0:
		if current := currentKey(vals, node); current != nil {
			return current
		}
		return pub(49 + number)
	case 1:
		return bytes.Clone(vals[int(number)%len(vals)].GetConsensusPubkey())
	default:
		return pub(56 + pool&3)
	}
}

func currentKey(vals []*authorityv1.ValidatorPower, node string) []byte {
	for _, row := range vals {
		if row.GetNodeId() == node {
			return bytes.Clone(row.GetConsensusPubkey())
		}
	}
	return nil
}

// pick names the proposal an approve or execute acts on. With bit 7 of v
// set it is proposal number v&0x7f modulo the count. With bit 7 clear it is
// the accepted proposal in state want with the most approvals, the newest
// on a tie, so random approvals gather on one proposal; the last proposal
// when none is in that state.
func (h *fuzzHarness) pick(v byte, want authorityv1.ProposalState) string {
	if len(h.proposals) == 0 {
		return "p-none"
	}
	if v&0x80 != 0 {
		return h.proposals[int(v&0x7f)%len(h.proposals)]
	}
	best, most := h.last(), -1
	for i := len(h.proposals) - 1; i >= 0; i-- {
		id := h.proposals[i]
		raw := h.base.Get(proposalKey(id))
		if raw == nil {
			h.t.Fatalf("op %d: proposal %s has no record", h.c.Ops-1, id)
		}
		if rec := h.summary(id, raw); rec.state == want && rec.approvers > most {
			best, most = id, rec.approvers
		}
	}
	return best
}

func (h *fuzzHarness) last() string {
	if len(h.proposals) == 0 {
		return "p-none"
	}
	return h.proposals[len(h.proposals)-1]
}

// validatorFuzzMsg is a validator change with a height or unix activation,
// near or far.
func validatorFuzzMsg(st *State, node string, key []byte, power int64, x, v byte) *authorityv1.MsgProposeChange {
	far := v&0x80 != 0
	if x&flagTime != 0 {
		ahead := fuzzNearTimeAhead
		if far {
			ahead = fuzzFarTimeAhead
		}
		return timePower(node, key, power, st.Time+ahead)
	}
	ahead := uint64(fuzzNearHeightAhead)
	if far {
		ahead = fuzzFarHeightAhead
	}
	return powerMsg(node, key, power, uint64(st.Height)+ahead)
}

// otherFuzzMsg is a governed limit (payload bits 0-5), an admit payload (6)
// or a force-withdraw payload (7). The last two cannot execute in goal 06:
// no provider row and no content registry exist.
func otherFuzzMsg(v byte) *authorityv1.MsgProposeChange {
	switch index := int(v & 7); index {
	case 6:
		return payloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{
			AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: "prov-1"},
		}}, "admit-prov-1")
	case 7:
		return payloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ForceWithdraw{
			ForceWithdraw: &authorityv1.ForceWithdrawChange{
				ContentId: "content-1", ContentVersion: 1,
				ReasonDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(7)},
			},
		}}, "withdraw-content-1")
	default:
		key := fuzzLimits[index]
		value := 1 + uint64(v>>3)&15
		if v&0x80 != 0 {
			ceiling, _ := governedCeiling(key)
			value = ceiling + 1
		}
		return governedMsg(key, value)
	}
}

func (h *fuzzHarness) propose(st *State, op byte, seat int, x, v byte) fuzzResult {
	vals := st.Validators()
	var msg *authorityv1.MsgProposeChange
	switch op {
	case opProposeValidator:
		power := int64(v>>5) & 3
		if power == 0 && x&flagExtra != 0 {
			power = fuzzBigPower
		}
		node, _ := fuzzNode(v)
		msg = validatorFuzzMsg(st, node, fuzzKey(vals, v, v>>3), power, x, v)
	case opProposeRemoval:
		pool := byte(0)
		if v&8 != 0 {
			pool = v >> 4
		}
		node, _ := fuzzNode(v)
		msg = validatorFuzzMsg(st, node, fuzzKey(vals, v, pool), 0, x, v)
	default:
		msg = otherFuzzMsg(v)
	}
	id := "p" + strconv.Itoa(len(h.proposals))
	reuse := op != opProposeValidator && x&flagExtra != 0 && len(h.proposals) > 0
	if reuse {
		id = h.last()
	}
	msg.Signer = h.seats[seat].Signer
	msg.ProposalId = id
	msg.BusinessId = "pb-" + id + "-" + fuzzLetter(seat)
	msg.BasePolicyVersion = st.PolicyVersion()
	msg.ExpiryUnixSeconds = st.Time + fuzzNearExpiry
	if x&flagFar != 0 {
		msg.ExpiryUnixSeconds = st.Time + fuzzFarExpiry
	}
	r := fuzzResult{out: st.ProposeChange(msg), target: id}
	if !reuse {
		r.onCommit = func() {
			h.proposals = append(h.proposals, id)
			if got := h.stateOf(id); got != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
				h.t.Fatalf("op %d: accepted proposal %s is %v", h.c.Ops-1, id, got)
			}
		}
	}
	return r
}

func (h *fuzzHarness) approve(st *State, seat int, x, v byte) fuzzResult {
	target := h.pick(v, authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
	if x == approveMissing {
		target = "p-missing"
	}
	business := "ap-" + target + "-" + fuzzLetter(seat)
	if x&flagTime != 0 {
		business += "-" + strconv.Itoa(h.c.Ops)
	}
	out := st.ApproveChange(approve(h.seats[seat].Signer, business, target))
	return fuzzResult{out: out, target: target, onCommit: func() {
		if out.Success() && h.stateOf(target) == authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			h.c.Queued++
		}
	}}
}

func (h *fuzzHarness) execute(st *State, seat int, x, v byte) fuzzResult {
	target := h.pick(v, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	signer, want := h.authority, ""
	switch x {
	case executeByService:
		signer, want = h.service, CodeForbidden
	case executeBySeat:
		// Unit-genesis seats are not submitters (app genesis makes them
		// BROADCASTER, DR-06-06, which gives FORBIDDEN instead).
		signer, want = h.seats[seat].Signer, CodeUnauthorized
	}
	out := st.ExecuteChange(execute(signer, "ex-"+target+"-"+fuzzLetter(seat), target))
	return fuzzResult{out: out, target: target, want: want, onCommit: func() {
		if out.Success() {
			h.executes++
			h.executed[target] = true
			h.c.Executed++
			if got := h.stateOf(target); got != authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED {
				h.t.Fatalf("op %d: executed proposal %s is %v", h.c.Ops-1, target, got)
			}
		}
	}}
}

// staleSigner is the newest retired seat account when one exists, else an
// account that was never admitted.
func (h *fuzzHarness) staleSigner() []byte {
	if len(h.retired) > 0 {
		return h.retired[len(h.retired)-1]
	}
	return bytes20(99)
}

// wrongRole is an action by the wrong account, which must get its exact
// refusal code, or an engine-only rotation.
func (h *fuzzHarness) wrongRole(st *State, seat int, v byte) fuzzResult {
	letter := fuzzLetter(seat)
	target := h.last()
	governed := func(signer []byte, id string) *authorityv1.MsgProposeChange {
		msg := governedMsg("max_page_limit", 9)
		msg.Signer = signer
		msg.ProposalId = id
		msg.BusinessId = "pb-" + id + "-" + letter
		msg.BasePolicyVersion = st.PolicyVersion()
		msg.ExpiryUnixSeconds = st.Time + fuzzFarExpiry
		return msg
	}
	switch v & 7 {
	case 0: // the authority account proposes
		return fuzzResult{out: st.ProposeChange(governed(h.authority, "p-auth")), want: CodeUnauthorized}
	case 1: // the service account approves
		return fuzzResult{out: st.ApproveChange(approve(h.service, "ap-svc-"+letter, target)), want: CodeUnauthorized}
	case 2: // a seat executes
		return fuzzResult{out: st.ExecuteChange(execute(h.seats[seat].Signer, "ex-seat-"+letter, target)), want: CodeUnauthorized}
	case 3: // a seat approves with a business id outside the pattern
		return fuzzResult{out: st.ApproveChange(approve(h.seats[seat].Signer, "Bad!"+letter, target)), want: CodeBounds}
	case 4: // a retired seat key (or an outsider) approves
		return fuzzResult{out: st.ApproveChange(approve(h.staleSigner(), "ap-old-"+letter, target)), want: CodeUnauthorized}
	case 5: // engine-only council seat rotation
		newPub := pub(70 + (v>>3)&7)
		old := h.seats[seat]
		return fuzzResult{out: st.RotateCouncilSeat(old.ID, newPub), onCommit: func() {
			h.retired = append(h.retired, old.Signer)
			h.seats[seat] = testSeat{ID: old.ID, Pub: newPub, Signer: SeatAddress(newPub)}
		}}
	case 6: // engine-only service submitter rotation
		newAddr := bytes20(80 + (v>>3)&3)
		return fuzzResult{out: st.RotateServiceSubmitter(h.service, newAddr), onCommit: func() {
			h.service = newAddr
		}}
	default: // a retired seat key (or an outsider) proposes
		return fuzzResult{out: st.ProposeChange(governed(h.staleSigner(), "p-old")), want: CodeUnauthorized}
	}
}
