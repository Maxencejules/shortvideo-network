package difftest

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// d1Keys are governed limit keys outside the msgkit table (authority D-1):
// two safety caps, the per-seat quota and pending-cap names, and a plain id.
var d1Keys = []string{"max_tx_bytes", "max_gas_per_tx", "max_open_proposals", "max_pending_changes", "foo.bar"}

// gen is the seeded step generator. It reads committed state through the
// harness (never through the oracle) and keeps only the bookkeeping it
// needs to replay ids. Its input domain excludes, by construction: an
// absent message integer (G-2; a Go message always carries one); a zero
// payload integer other than a validator removal's power; an empty payload
// string or consensus key; a reason digest whose value is not 32 bytes; a
// negative validator power; a force-withdraw content id outside the id
// pattern with an unparseable reason digest. Apart from G-2 these are
// proto3 presence and parse-order cases where the chain and spec_machine.py
// answer different reject codes and no G or D rule names the difference;
// they are reported as open divergences (TestUnnamedParseDivergences).
type gen struct {
	rng *rand.Rand
	h   *harness
	n   int
	// steps are the transaction steps sent so far, for replays.
	steps []*step
	// proposed are the proposal ids sent in proposes, committed or not.
	proposed []string
	// providerIDs and contentIDs are the seeded and registered ids that
	// registry payloads target.
	providerIDs []string
	contentIDs  []string
}

func newGen(seed uint64, h *harness) *gen {
	g := &gen{rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), h: h}
	for _, seed := range h.cfg.seeds {
		g.providerIDs = append(g.providerIDs, seed.GetProviderId())
	}
	return g
}

func (g *gen) fresh(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

func (g *gen) pct(p int) bool { return g.rng.IntN(100) < p }

func (g *gen) pick(items []string) string { return items[g.rng.IntN(len(items))] }

// genView is the committed state the generator chooses from.
type genView struct {
	version    uint64
	records    []storedRecord
	validators []*authorityv1.ValidatorPower
	pending    []*authorityv1.PendingValidatorPower
}

// view is the generator's view of the latest committed read.
func (g *gen) view() genView {
	c := g.h.last
	return genView{
		version: c.version, records: c.records,
		validators: c.gov.GetValidators(), pending: c.gov.GetPendingValidatorPower(),
	}
}

// current lists the proposals in state on the current base version: the
// only ones that can still execute.
func (v genView) current(state authorityv1.ProposalState) []storedRecord {
	var out []storedRecord
	for _, rec := range v.records {
		if authorityv1.ProposalState(rec.State) == state && rec.BasePolicyVersion == v.version {
			out = append(out, rec)
		}
	}
	return out
}

// next returns the next step. The weights favour finishing proposals that
// can still execute, so sequences reach EXECUTED, REJECTED, the stale
// branch, and applied validator changes, not only proposes.
func (g *gen) next() *step {
	v := g.view()
	propose := 30
	if len(v.current(authorityv1.ProposalState_PROPOSAL_STATE_OPEN)) >= 3 {
		propose = 10
	}
	execute, block := 15, 12
	if queued := v.current(authorityv1.ProposalState_PROPOSAL_STATE_QUEUED); len(queued) > 0 {
		execute = 35
		ready := false
		for _, rec := range queued {
			ready = ready || rec.ExecuteAfter <= g.h.now
		}
		if !ready {
			// Every queued proposal still waits for its review delay.
			execute, block = 10, 30
		}
	}
	weights := []int{propose, 30, execute, block, 3, 3}
	total := 0
	for _, w := range weights {
		total += w
	}
	r := g.rng.IntN(total)
	choice := 0
	for ; r >= weights[choice]; choice++ {
		r -= weights[choice]
	}
	var s *step
	switch choice {
	case 0:
		s = g.propose(v)
	case 1:
		s = g.approve(v)
	case 2:
		s = g.execute(v)
	case 3:
		s = g.blockStep(v)
	case 4:
		s = g.registerProvider()
	default:
		s = g.registerContent()
	}
	if s.op != fxBlock {
		g.steps = append(g.steps, s)
	}
	return s
}

// replay returns a previous step of op sent again unchanged, or nil.
func (g *gen) replay(op string) *step {
	var same []*step
	for _, s := range g.steps {
		if s.op == op {
			same = append(same, s)
		}
	}
	if len(same) == 0 {
		return nil
	}
	old := same[g.rng.IntN(len(same))]
	return &step{op: old.op, tag: "replay of " + old.tag, msg: old.msg, spec: old.spec}
}

// oldBusiness is the business id of a previous transaction of any module,
// or "".
func (g *gen) oldBusiness() string {
	if len(g.steps) == 0 {
		return ""
	}
	id, _ := g.steps[g.rng.IntN(len(g.steps))].spec["business_id"].(string)
	return id
}

func (g *gen) seatSigner() []byte {
	if g.pct(92) {
		return seatAcct(1 + g.rng.IntN(seatCount))
	}
	return [][]byte{authorityAcct, outsiderAcct, serviceAcct}[g.rng.IntN(3)]
}

func (g *gen) propose(v genView) *step {
	if g.pct(5) {
		if s := g.replay(opPropose); s != nil {
			return s
		}
	}
	p := proposeParams{signer: g.seatSigner(), business: g.fresh("bp"), base: v.version}
	var tags []string
	p.proposalID = g.fresh("p")
	if len(g.proposed) > 0 && g.pct(8) {
		p.proposalID, tags = g.pick(g.proposed), append(tags, "duplicate id")
	}
	if old := g.oldBusiness(); old != "" && g.pct(5) {
		p.business, tags = old, append(tags, "reused business id")
	}
	switch r := g.rng.IntN(100); {
	case r < 6:
		p.base, tags = v.version-1, append(tags, "stale base")
	case r < 9:
		p.base, tags = v.version+1, append(tags, "future base")
	case r < 11:
		p.base, tags = 0, append(tags, "base 0")
	}
	now := g.h.now
	switch r := g.rng.IntN(100); {
	case r < 80:
		p.expiry = now + 1 + g.rng.Int64N(3000)
	case r < 85:
		p.expiry, tags = now-g.rng.Int64N(100), append(tags, "expired")
	case r < 88:
		p.expiry, tags = unixMax+1, append(tags, "expiry above unix max")
	case r < 90:
		p.expiry, tags = 0, append(tags, "expiry 0")
	default:
		p.expiry = now + 3000 + g.rng.Int64N(20000)
	}
	var kindTag string
	p.payload, kindTag = g.payload(v)
	if kindTag != "" {
		tags = append(tags, kindTag)
	}
	switch r := g.rng.IntN(100); {
	case r < 3:
		p.digest, tags = sha(g.fresh("wrong-digest")), append(tags, "wrong digest")
	case r < 6:
		p.wrongAlgorithm, tags = true, append(tags, "G-1 algorithm")
	case r < 8:
		p.parameterKey, tags = "validator-zz", append(tags, "wrong parameter key")
	case r < 10:
		p.parameterKey, tags = "dec-"+p.payload.key, append(tags, "dec- parameter key")
	}
	g.proposed = append(g.proposed, p.proposalID)
	tag := strings.Join(tags, "+")
	if tag == "" {
		tag = "fresh"
	}
	return buildPropose(p, tag)
}

// payload draws a payload and, for the rule variants, a coverage tag.
func (g *gen) payload(v genView) (payload, string) {
	switch r := g.rng.IntN(100); {
	case r < 38:
		return g.governed(v)
	case r < 70:
		return g.validator(v)
	case r < 80:
		return admitPayload(g.providerTarget()), ""
	case r < 90:
		return disablePayload(g.providerTarget()), ""
	default:
		target := g.fresh("nc")
		if len(g.contentIDs) > 0 && g.pct(80) {
			target = g.pick(g.contentIDs)
		}
		version := uint64(1)
		if g.pct(15) {
			version = 2
		}
		return withdrawPayload(target, version), ""
	}
}

func (g *gen) providerTarget() string {
	if len(g.providerIDs) > 0 && g.pct(80) {
		return g.pick(g.providerIDs)
	}
	return g.fresh("np")
}

func (g *gen) governed(v genView) (payload, string) {
	if g.pct(18) {
		return governedPayload(g.pick(d1Keys), 1+g.rng.Uint64N(50)), "D-1 key"
	}
	keys := msgkit.LimitKeys()
	key := keys[g.rng.IntN(len(keys))]
	for _, rec := range v.records {
		// Same-key concurrency: a second proposal on the key of an open or
		// queued one, so an execute can meet the stale branch's CONFLICT.
		state := authorityv1.ProposalState(rec.State)
		if (state == authorityv1.ProposalState_PROPOSAL_STATE_OPEN || state == authorityv1.ProposalState_PROPOSAL_STATE_QUEUED) &&
			slices.Contains(keys, rec.ParameterKey) && g.pct(30) {
			key = rec.ParameterKey
			break
		}
	}
	ceiling, _ := msgkit.Ceiling(key)
	switch r := g.rng.IntN(100); {
	case r < 72:
		return governedPayload(key, 1+g.rng.Uint64N(min(ceiling, 2000))), ""
	case r < 84:
		return governedPayload(key, ceiling), ""
	case r < 95:
		return governedPayload(key, ceiling+1), "D-1 value"
	default:
		return governedPayload(key, 1<<62), "D-1 value"
	}
}

// activation draws an activation point: of the pending changes' kind most
// of the time (DEC-012 makes a mix CONFLICT), in the future most of the
// time.
func (g *gen) activation(v genView) activation {
	byHeight := g.pct(50)
	if len(v.pending) > 0 && g.pct(85) {
		_, byHeight = v.pending[0].GetActivation().GetPoint().(*authorityv1.ActivationPoint_Height)
	}
	if byHeight {
		if g.pct(8) {
			return atHeight(uint64(g.h.height))
		}
		return atHeight(uint64(g.h.height) + 1 + g.rng.Uint64N(40))
	}
	if g.pct(8) {
		return atTime(g.h.now)
	}
	return atTime(g.h.now + 1 + g.rng.Int64N(3000))
}

func (g *gen) validator(v genView) (payload, string) {
	reason := digest(sha(g.fresh("reason")))
	at := g.activation(v)
	pick := func() *authorityv1.ValidatorPower { return v.validators[g.rng.IntN(len(v.validators))] }
	switch r := g.rng.IntN(100); {
	case r < 27:
		node := g.fresh("n")
		return validatorPayload(node, consensusKey(node), 1+g.rng.Int64N(15), reason, at), ""
	case r < 52:
		row := pick()
		key := row.GetConsensusPubkey()
		if g.pct(20) {
			key = consensusKey(g.fresh("rotated"))
		}
		// Powers above a third of the total break DEC-011: FORBIDDEN at
		// execute, so the proposal is REJECTED.
		return validatorPayload(row.GetNodeId(), key, 1+g.rng.Int64N(45), reason, at), ""
	case r < 70:
		row := pick()
		key := row.GetConsensusPubkey()
		if g.pct(15) {
			key = consensusKey(g.fresh("other"))
		}
		return validatorPayload(row.GetNodeId(), key, 0, reason, at), ""
	case r < 85:
		return validatorPayload(g.fresh("n"), seatPub(1+g.rng.IntN(seatCount)), 10, reason, at), "D-7 seat key"
	case r < 90:
		return validatorPayload(g.fresh("n"), pick().GetConsensusPubkey(), 10, reason, at), "taken key"
	case r < 93:
		unix := []int64{0, -5, unixMax + 1}[g.rng.IntN(3)]
		return validatorPayload(g.fresh("n"), consensusKey("g9"), 10, reason, atTime(unix)), "G-9"
	case r < 96:
		bad := []*typesv1.Digest{nil, {Value: sha("no-algorithm")}, reason}[g.rng.IntN(3)]
		points := []activation{atTime(0), at}
		return validatorPayload("-bad", consensusKey("g10"), 10, bad, points[g.rng.IntN(2)]), "G-10"
	default:
		bad := []*typesv1.Digest{nil, {Value: sha("no-algorithm")}}[g.rng.IntN(2)]
		return validatorPayload(g.fresh("n"), consensusKey("malformed"), 10, bad, at), "malformed reason"
	}
}

func (g *gen) approve(v genView) *step {
	if g.pct(5) {
		if s := g.replay(opApprove); s != nil {
			return s
		}
	}
	target := g.fresh("p")
	signer := g.seatSigner()
	open := v.current(authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
	if g.pct(25) {
		// Any open proposal, also one on an older base: it can reach
		// QUEUED and then the execute's stale branch.
		open = nil
		for _, rec := range v.records {
			if authorityv1.ProposalState(rec.State) == authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
				open = append(open, rec)
			}
		}
	}
	switch {
	case len(open) > 0 && g.pct(65):
		// The open proposal closest to the threshold, by a seat that has
		// not approved it yet.
		best := open[0]
		for _, rec := range open[1:] {
			if len(rec.Approvers) > len(best.Approvers) {
				best = rec
			}
		}
		target = best.ProposalID
		for i := 1; i <= seatCount && g.pct(85); i++ {
			seat := 1 + (i+g.rng.IntN(seatCount))%seatCount
			if !slices.Contains(best.Approvers, seatID(seat)) {
				signer = seatAcct(seat)
				break
			}
		}
	case len(v.records) > 0 && g.pct(80):
		target = v.records[g.rng.IntN(len(v.records))].ProposalID
	}
	business := g.fresh("ba")
	if old := g.oldBusiness(); old != "" && g.pct(4) {
		business = old
	}
	return buildApprove(signer, business, target)
}

func (g *gen) execute(v genView) *step {
	if g.pct(8) {
		if s := g.replay(opExecute); s != nil {
			return s
		}
	}
	target := g.fresh("p")
	queued := v.current(authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	switch {
	case len(queued) > 0 && g.pct(70):
		target = queued[g.rng.IntN(len(queued))].ProposalID
	case len(v.records) > 0 && g.pct(85):
		target = v.records[g.rng.IntN(len(v.records))].ProposalID
	}
	signer := authorityAcct
	if g.pct(10) {
		signer = [][]byte{serviceAcct, broadcasterAcct, seatAcct(1), outsiderAcct, providerAccount(0)}[g.rng.IntN(5)]
	}
	business := g.fresh("bx")
	if old := g.oldBusiness(); old != "" && g.pct(3) {
		business = old
	}
	return buildExecute(signer, business, target)
}

// blockStep draws the next block time. A quarter of the blocks land on a
// boundary of an open or queued proposal (one second before or exactly at
// its expiry or its execute-after time), so the >= and < comparisons of
// approve and execute meet both sides of each boundary.
func (g *gen) blockStep(v genView) *step {
	if g.pct(25) {
		var points []int64
		for _, rec := range v.records {
			state := authorityv1.ProposalState(rec.State)
			if state != authorityv1.ProposalState_PROPOSAL_STATE_OPEN && state != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
				continue
			}
			for _, t := range []int64{rec.Expiry - 1, rec.Expiry, rec.ExecuteAfter - 1, rec.ExecuteAfter} {
				if t > g.h.now && t <= unixMax {
					points = append(points, t)
				}
			}
		}
		if len(points) > 0 {
			return blockStep(points[g.rng.IntN(len(points))])
		}
	}
	var jump int64
	switch r := g.rng.IntN(100); {
	case r < 40:
		jump = 1 + g.rng.Int64N(5)
	case r < 75:
		jump = g.h.cfg.delay + g.rng.Int64N(4)
	case r < 93:
		jump = 100 + g.rng.Int64N(1900)
	default:
		jump = 3000 + g.rng.Int64N(9000)
	}
	return blockStep(g.h.now + max(jump, 1))
}

func (g *gen) registerProvider() *step {
	id := g.fresh("pr")
	if len(g.providerIDs) > 0 && g.pct(10) {
		id = g.pick(g.providerIDs)
	} else {
		g.providerIDs = append(g.providerIDs, id)
	}
	return buildRegisterProvider(providerAccount(g.rng.IntN(2)), g.fresh("bpr"), id)
}

func (g *gen) registerContent() *step {
	id := g.fresh("c")
	if len(g.contentIDs) > 0 && g.pct(10) {
		id = g.pick(g.contentIDs)
	} else {
		g.contentIDs = append(g.contentIDs, id)
	}
	return buildRegisterContent(serviceAcct, g.fresh("bc"), id, packageBytes)
}

// transition names a proposal state change a step made, for the
// reachability counts.
func transition(before, after map[string]authorityv1.ProposalState, s *step) string {
	id, _ := s.spec["proposal_id"].(string)
	from, had := before[id]
	to, has := after[id]
	switch {
	case !has || (had && from == to):
		return ""
	case !had:
		return "new " + stateName(to)
	default:
		return stateName(from) + " -> " + stateName(to)
	}
}

func proposalStates(records []storedRecord) map[string]authorityv1.ProposalState {
	out := make(map[string]authorityv1.ProposalState, len(records))
	for _, rec := range records {
		out[rec.ProposalID] = authorityv1.ProposalState(rec.State)
	}
	return out
}

// validatorsChanged reports whether two validator sets differ.
func validatorsChanged(a, b []*authorityv1.ValidatorPower) bool {
	key := func(rows []*authorityv1.ValidatorPower) []string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s/%d/%x", r.GetNodeId(), r.GetPower(), r.GetConsensusPubkey()))
		}
		sort.Strings(out)
		return out
	}
	ka, kb := key(a), key(b)
	if len(ka) != len(kb) {
		return true
	}
	for i := range ka {
		if ka[i] != kb[i] {
			return true
		}
	}
	return false
}
