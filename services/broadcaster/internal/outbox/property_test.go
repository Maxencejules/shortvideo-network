package outbox_test

// Property and fuzz tests of the state machine against a model of one
// signer, one node and a small set of intents (plan 13.5). The model is a
// unit-test fake: it stands in for the chain, the node's mempool and cache,
// the indexer and the outbox rows, and it is never end-to-end evidence.
//
// The model follows the chain rules the machine relies on (plan 1.1): a
// transaction commits only from the node's mempool, at a height no greater
// than its timeout_height and with the signer's current sequence; a refused
// send never enters the mempool; the cache answers CACHE for held or
// committed bytes until a node restart, after which committed bytes get sdk
// 32; a transaction that failed in a block leaves the cache, and if its ante
// failed (no sequence consumed) its bytes pass CheckTx again and can commit
// again until their timeout_height, the new result replacing the failure in
// the index; /tx and tx_search see a transaction at most IndexingMargin
// blocks after it committed. It feeds the machine only truthful
// observations, so every integrity alert is a failure.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
)

const (
	maxModelOps     = 400
	maxModelIntents = 4
	maxModelHeight  = 100000
)

// reach counts how often each transition ran; index 0 is TNone.
type reach [outbox.NumTransitions + 1]int

type objKind uint8

const (
	objNone objKind = iota
	objCommitted
	objTaken
	objChanged
	objConflict
)

// object is the chain state of an intent's object. Twin intents share one.
type object struct {
	kind  objKind
	relay bool
	// hash is the committing transaction of objCommitted and objConflict.
	hash [32]byte
	// byUs: an attempt the outbox stored committed it.
	byUs bool
	// bidIntent and bidN name the business id that committed it; bidIntent
	// is -1 for another account's own business id.
	bidIntent int
	bidN      int
	visibleAt uint64
}

type mAttempt struct {
	a      outbox.Attempt
	intent int
	bidN   int
	inPool bool
	cached bool
}

type mTx struct {
	height    uint64
	code      outbox.Code
	consumed  bool
	typed     bool
	idem      bool
	visibleAt uint64
	// prev is the indexed result these bytes had before they committed
	// again (an ante failure); /tx shows it until this one is visible.
	prev *mTx
}

type mIntent struct {
	in      outbox.Intent
	obj     *object
	expired bool
	live    int
	final   bool
	snap    outbox.Intent
}

type world struct {
	lim      outbox.Limits
	height   uint64
	accSeq   uint64
	attempts []*mAttempt
	txs      map[[32]byte]*mTx
	bySeq    map[uint64][32]byte
	// foreign maps foreign transactions under the signer's key to whether
	// an operator cleared them.
	foreign map[[32]byte]bool
	intents []*mIntent
	halted  bool
	stall   bool
	fresh   bool
	nonce   uint32
	hits    *reach
}

const (
	opCreate = iota
	opValidate
	opCancel
	opExpire
	opReconcile
	opSend
	opBlock
	opForeign
	opOther
	opStall
	opRestart
	opClear
	opPoke
)

// opTable weights the operations: reconciliation, sends and blocks are the
// common ones. A foreign transaction halts the signer until a clear, so it
// is the rarest (foreignTx also skips three of four args).
var opTable = []int{
	opCreate, opCreate, opValidate, opCancel, opExpire,
	opReconcile, opReconcile, opReconcile, opReconcile, opReconcile, opReconcile,
	opSend, opSend, opSend, opSend, opSend,
	opBlock, opBlock, opBlock, opBlock, opBlock,
	opForeign, opOther, opStall, opRestart, opClear, opClear, opPoke,
}

type stream struct {
	b []byte
	i int
}

func (s *stream) more() bool { return s.i < len(s.b) }

func (s *stream) next() byte {
	if s.i >= len(s.b) {
		return 0
	}
	v := s.b[s.i]
	s.i++
	return v
}

var modelLimits = outbox.Limits{
	IndexingMargin:        2,
	AttemptsPerBusinessID: 3,
	BusinessAttempts:      2,
	AttemptsPerIntent:     5,
	Sdk4Retries:           1,
}

// run interprets data as a bounded sequence of operations and checks the
// invariants after each one. hits, if not nil, counts the transitions.
func run(data []byte, hits *reach) error {
	if hits == nil {
		hits = new(reach)
	}
	w := &world{
		lim:     modelLimits,
		height:  1,
		txs:     map[[32]byte]*mTx{},
		bySeq:   map[uint64][32]byte{},
		foreign: map[[32]byte]bool{},
		fresh:   true,
		hits:    hits,
	}
	s := &stream{b: data}
	for n := 0; n < maxModelOps && s.more(); n++ {
		op := opTable[int(s.next())%len(opTable)]
		arg := s.next()
		if err := w.do(op, arg); err != nil {
			return fmt.Errorf("op %d (kind %d, arg %d): %w", n, op, arg, err)
		}
		if err := w.check(); err != nil {
			return fmt.Errorf("after op %d (kind %d, arg %d): %w", n, op, arg, err)
		}
	}
	return nil
}

func (w *world) newHash() [32]byte {
	w.nonce++
	var h [32]byte
	binary.BigEndian.PutUint32(h[:4], w.nonce)
	h[31] = 0xa5
	return h
}

func (w *world) pick(arg byte) int { return int(arg>>4) % len(w.intents) }

func (w *world) find(h [32]byte) int {
	for i, ma := range w.attempts {
		if ma.a.Hash == h {
			return i
		}
	}
	return -1
}

func (w *world) anyLive() int {
	for i, ma := range w.attempts {
		if ma.a.Phase.Live() {
			return i
		}
	}
	return -1
}

func (w *world) guard(i int) outbox.Guard {
	return outbox.Guard{
		Expired:      w.intents[i].expired,
		Due:          true,
		Stalled:      w.stall,
		SignerHalted: w.halted,
		SignerIdle:   w.anyLive() < 0,
		LockHeld:     true,
	}
}

func (w *world) lag(arg byte) uint64 { return uint64(arg>>5) % (w.lim.IndexingMargin + 1) }

// advance produces a block: the height rises and the mempool's recheck
// evicts every held transaction whose sequence or deadline no longer
// passes.
func (w *world) advance() error {
	if w.height >= maxModelHeight {
		return errors.New("model height bound reached")
	}
	w.height++
	w.evict()
	return nil
}

func (w *world) evict() {
	for _, ma := range w.attempts {
		if ma.inPool && (ma.a.Sequence != w.accSeq || w.height > ma.a.TimeoutHeight) {
			ma.inPool, ma.cached = false, false
		}
	}
}

// integrityAlerts are alerts a truthful model never causes.
var integrityAlerts = map[outbox.Alert]bool{
	outbox.AlertHashMismatch:         true,
	outbox.AlertAccSeqForeign:        true,
	outbox.AlertResultInconsistent:   true,
	outbox.AlertDeadAttemptCommitted: true,
	outbox.AlertSearchInconsistent:   true,
	outbox.AlertEffectInconsistent:   true,
}

func (w *world) apply(i int, ev outbox.Event) (outbox.Step, error) {
	st, err := outbox.Apply(w.intents[i].in, ev, w.lim)
	if err != nil {
		return st, err
	}
	return st, w.store(i, st)
}

// store writes a step to the model's rows, as the store would, and checks
// what each step may do.
func (w *world) store(i int, st outbox.Step) error {
	m := w.intents[i]
	if st.Before != m.in {
		return errors.New("step Before differs from the stored intent")
	}
	w.hits[st.ID]++
	if integrityAlerts[st.Alert] || st.Halt == outbox.HaltIntegrity {
		return fmt.Errorf("%v: integrity alert %v halt %v from truthful observations", st.ID, st.Alert, st.Halt)
	}
	if st.After.State == outbox.StateCommitted && st.Before.State != outbox.StateCommitted {
		if st.ID != outbox.T5 && st.ID != outbox.T11 && st.ID != outbox.T12 {
			return fmt.Errorf("COMMITTED through %v", st.ID)
		}
		if err := w.checkCommitment(i, st.After); err != nil {
			return fmt.Errorf("%v: %w", st.ID, err)
		}
	}
	if st.After.State == outbox.StateFailed && st.Before.State != outbox.StateFailed {
		if err := w.checkFailure(i, st.After); err != nil {
			return fmt.Errorf("%v: %w", st.ID, err)
		}
	}
	if st.Halt != 0 {
		w.halted = true
	}
	if st.HasAttempt {
		if st.ID == outbox.T8 {
			w.attempts = append(w.attempts, &mAttempt{a: st.Attempt, intent: i, bidN: st.Before.Business})
			m.live = len(w.attempts) - 1
		} else {
			idx := w.find(st.Attempt.Hash)
			if idx < 0 || idx != m.live {
				return fmt.Errorf("%v writes an attempt that is not the intent's live attempt", st.ID)
			}
			w.attempts[idx].a = st.Attempt
			if !st.Attempt.Phase.Live() {
				m.live = -1
			}
		}
	}
	m.in = st.After
	if (m.in.State == outbox.StateInFlight) != (m.live >= 0) {
		return fmt.Errorf("%v: IN_FLIGHT %v but live attempt %d", st.ID, m.in.State == outbox.StateInFlight, m.live)
	}
	if m.in.State.Terminal() && !m.final {
		m.final, m.snap = true, m.in
	}
	return nil
}

// checkCommitment: a COMMITTED intent names the first commitment of its
// object's effect, which the chain holds with code 0 and the indexer shows.
func (w *world) checkCommitment(i int, after outbox.Intent) error {
	obj := w.intents[i].obj
	if obj.kind != objCommitted || obj.hash != after.Canonical {
		return errors.New("COMMITTED without the object's committed effect")
	}
	tx := w.txs[after.Canonical]
	if tx == nil || !tx.code.OK() || tx.visibleAt > w.height {
		return errors.New("COMMITTED without a visible committed transaction with code 0")
	}
	idx := w.find(after.Canonical)
	if own := idx >= 0 && w.attempts[idx].intent == i; after.Own != own {
		return fmt.Errorf("own_transaction %v, want %v", after.Own, own)
	}
	return nil
}

func (w *world) checkFailure(i int, after outbox.Intent) error {
	obj := w.intents[i].obj
	switch after.Reason {
	case outbox.ReasonObjectTaken:
		if obj.kind != objTaken && (obj.kind != objCommitted || obj.byUs || obj.relay) {
			return errors.New("OBJECT_TAKEN while the object is not taken")
		}
	case outbox.ReasonObjectStateChanged:
		if obj.kind != objChanged {
			return errors.New("OBJECT_STATE_CHANGED while the object did not change")
		}
	}
	return nil
}

// check runs the invariants that hold between operations.
func (w *world) check() error {
	live := 0
	for _, ma := range w.attempts {
		if ma.a.Phase.Live() {
			live++
		}
		if ma.a.Phase == outbox.AttemptDead && ma.a.Dead == outbox.DeadRefused && !ma.a.Sends.AllRefused() {
			return errors.New("dead by (d2) with a send that is not a recorded refusal")
		}
	}
	if live > 1 {
		return fmt.Errorf("%d live attempts for one signer", live)
	}
	for i, m := range w.intents {
		if m.final && m.in != m.snap {
			return fmt.Errorf("terminal intent %d changed", i)
		}
		if (m.in.State == outbox.StateInFlight) != (m.live >= 0) {
			return fmt.Errorf("intent %d: IN_FLIGHT and live attempt disagree", i)
		}
		if m.live >= 0 && (w.attempts[m.live].intent != i || !w.attempts[m.live].a.Phase.Live()) {
			return fmt.Errorf("intent %d: live attempt index is wrong", i)
		}
		if _, err := outbox.Present(m.in, outbox.Overlay{StallOpen: w.stall, ObserverFresh: w.fresh}); err != nil {
			return fmt.Errorf("intent %d: %w", i, err)
		}
	}
	return nil
}

func (w *world) do(op int, arg byte) error {
	if len(w.intents) == 0 && op != opCreate && op != opBlock && op != opStall {
		op = opCreate
	}
	switch op {
	case opCreate:
		return w.create(arg)
	case opValidate:
		return w.validate(arg)
	case opCancel:
		_, err := w.apply(w.pick(arg), outbox.Cancel{})
		if errors.Is(err, outbox.ErrGuard) {
			return nil
		}
		return err
	case opExpire:
		i := w.pick(arg)
		w.intents[i].expired = true
		if arg&1 == 0 {
			return nil
		}
		_, err := w.apply(i, outbox.Expire{})
		return err
	case opReconcile:
		return w.reconcile(w.pick(arg), arg)
	case opSend:
		return w.send(w.pick(arg), arg)
	case opBlock:
		return w.block(arg)
	case opForeign:
		return w.foreignTx(arg)
	case opOther:
		return w.otherAccount(w.pick(arg), arg)
	case opStall:
		if arg&1 != 0 {
			w.stall = !w.stall
		}
		if arg&2 != 0 {
			w.fresh = !w.fresh
		}
		return nil
	case opRestart:
		for _, ma := range w.attempts {
			ma.inPool, ma.cached = false, false
		}
		if arg&1 != 0 {
			w.relayAnteFailed()
		}
		return nil
	case opClear:
		w.halted = false
		for h := range w.foreign {
			w.foreign[h] = true
		}
		return nil
	case opPoke:
		return w.poke()
	}
	return fmt.Errorf("unknown op %d", op)
}

func (w *world) create(arg byte) error {
	if len(w.intents) >= maxModelIntents {
		return nil
	}
	obj := &object{relay: arg&1 != 0, bidIntent: -1}
	if arg&2 != 0 && len(w.intents) > 0 {
		obj = w.intents[w.pick(arg)].obj
	}
	w.intents = append(w.intents, &mIntent{obj: obj, live: -1})
	_, err := w.apply(len(w.intents)-1, outbox.Create{Validated: arg&4 == 0})
	return err
}

var validationReasons = []outbox.Reason{outbox.ReasonUnauthorized, outbox.ReasonInvalid, outbox.ReasonProviderSignatureInvalid}

func (w *world) validate(arg byte) error {
	i := w.pick(arg)
	m := w.intents[i]
	if arg&8 != 0 {
		_, err := w.apply(i, outbox.FailValidation{Reason: validationReasons[int(arg)%3]})
		if errors.Is(err, outbox.ErrGuard) && m.in.State == outbox.StateInFlight {
			return nil
		}
		return err
	}
	if m.in.State == outbox.StateRequested {
		m.in.Validated = true
	}
	return nil
}

// fill adds the observations named by need to a pass.
func (w *world) fill(i int, p *outbox.Pass, need outbox.Need) {
	if need&outbox.NeedEffect != 0 {
		p.Effect = w.effect(i)
	}
	if p.Live == nil {
		return
	}
	if need&outbox.NeedAccount != 0 {
		p.Chain.Account = &outbox.AccountRead{Sequence: w.accSeq, Height: w.height}
	}
	if need&outbox.NeedSearch != 0 {
		p.Chain.Searched, p.Chain.Search = true, w.search(p.Live.Sequence)
	}
}

func (w *world) pass(i int) outbox.Pass {
	m := w.intents[i]
	p := outbox.Pass{Intent: m.in, Guard: w.guard(i)}
	if m.live >= 0 {
		a := w.attempts[m.live].a
		p.Live = &a
		p.Chain = outbox.ChainView{Tx: w.txView(a.Hash), Height: w.height}
	}
	return p
}

// reconcile is one worker pass: it fetches what the decision needs, stores
// the steps, rebroadcasts or starts a new attempt when the decision allows.
func (w *world) reconcile(i int, arg byte) error {
	var have outbox.Need
	if arg&1 != 0 {
		have = outbox.NeedAccount | outbox.NeedSearch | outbox.NeedEffect
	}
	for round := 0; ; round++ {
		p := w.pass(i)
		w.fill(i, &p, have)
		d, err := outbox.Reconcile(p, w.lim)
		if err != nil {
			return err
		}
		for _, st := range d.Steps {
			if err := w.store(i, st); err != nil {
				return err
			}
		}
		if fetch := d.Need &^ outbox.NeedValidation &^ have; fetch != 0 && round < 3 {
			have |= fetch
			continue
		}
		if d.Start && d.Rebroadcast {
			return errors.New("a decision both starts and rebroadcasts")
		}
		if d.Start && p.Effect.Verdict != outbox.EffectNone {
			return errors.New("a new attempt starts without an effect check that found no effect")
		}
		if d.Rebroadcast && arg&2 != 0 {
			return w.send(i, arg>>2)
		}
		if d.Start {
			return w.start(i, p.Effect, arg)
		}
		return nil
	}
}

// start signs a new attempt the way a worker that pipelines would: after
// the live attempt's sequence if there is one. The machine must refuse that
// case, so every stored attempt takes the chain's next sequence.
func (w *world) start(i int, eff outbox.Effect, arg byte) error {
	seq := w.accSeq
	if live := w.anyLive(); live >= 0 {
		seq = w.attempts[live].a.Sequence + 1
	}
	a := outbox.Attempt{
		Hash:          w.newHash(),
		Sequence:      seq,
		TimeoutHeight: w.height + 1 + uint64(arg>>4)%6,
		Phase:         outbox.AttemptSigned,
	}
	st, err := outbox.Apply(w.intents[i].in, outbox.Persist{Attempt: a, Effect: eff, Guard: w.guard(i)}, w.lim)
	if err != nil {
		return fmt.Errorf("Reconcile allowed a start that Persist refuses: %w", err)
	}
	if seq != w.accSeq {
		return fmt.Errorf("attempt sequence %d while the chain's next is %d", seq, w.accSeq)
	}
	return w.store(i, st)
}

// send counts, sends and records one send of the intent's live attempt.
// Bit 7 of arg drops the record, as a crash between sends_started and the
// record does.
func (w *world) send(i int, arg byte) error {
	m := w.intents[i]
	if m.live < 0 {
		return nil
	}
	ma := w.attempts[m.live]
	st, err := outbox.Apply(m.in, outbox.StartSend{Attempt: ma.a, Expired: m.expired}, w.lim)
	if errors.Is(err, outbox.ErrGuard) {
		if !m.expired && m.in.Phase != outbox.PhaseAwaitingDeadline {
			return fmt.Errorf("send refused before expiry: %w", err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if m.expired || !st.SendStarted() {
		return errors.New("a send started after expiry or without being counted")
	}
	if err := w.store(i, st); err != nil {
		return err
	}
	answer := w.answer(ma, arg)
	if arg&0x80 != 0 {
		return nil
	}
	_, err = w.apply(i, outbox.RecordSend{Attempt: ma.a, Send: answer})
	return err
}

var refusalCodes = []outbox.Code{
	{Codespace: outbox.CodespaceSVN, Number: 2},
	outbox.CodeSVNUnauthorized,
	{Codespace: outbox.CodespaceSVN, Number: 19},
}

// relayAnteFailed is a third party that sends a node the bytes of every
// transaction whose ante failed in a block (the bytes are public in that
// block). CheckTx admits them again while their sequence is the signer's
// next and their timeout_height has not passed.
func (w *world) relayAnteFailed() {
	for _, ma := range w.attempts {
		tx := w.txs[ma.a.Hash]
		if tx != nil && !tx.consumed && ma.a.Sequence == w.accSeq && w.height+1 <= ma.a.TimeoutHeight {
			ma.inPool, ma.cached = true, true
		}
	}
}

// answer is the node's answer to broadcast_tx_sync of the attempt's bytes.
func (w *world) answer(ma *mAttempt, arg byte) outbox.Send {
	rejected := func(c outbox.Code) outbox.Send { return outbox.Send{Outcome: outbox.SendMempoolRejected, Code: c} }
	prior := w.txs[ma.a.Hash]
	switch {
	case prior != nil && ma.cached, prior == nil && (ma.inPool || ma.cached):
		return outbox.Send{Outcome: outbox.SendCache}
	case prior != nil && prior.consumed, ma.a.Sequence != w.accSeq:
		return rejected(outbox.CodeSDKWrongSequence)
	case w.height+1 > ma.a.TimeoutHeight:
		return rejected(outbox.CodeSDKTimeoutHeight)
	}
	switch arg % 7 {
	case 0, 1:
		ma.inPool, ma.cached = true, true
		return outbox.Send{Outcome: outbox.SendAck}
	case 2:
		ma.inPool, ma.cached = true, true
		return outbox.Send{Outcome: outbox.SendUnknown}
	case 3:
		return outbox.Send{Outcome: outbox.SendUnknown}
	case 4:
		return outbox.Send{Outcome: outbox.SendNodeRefused}
	case 5:
		return rejected(outbox.CodeSDKUnauthorized)
	}
	return rejected(refusalCodes[int(arg>>3)%len(refusalCodes)])
}

// block produces a block that includes the signer's held transaction when
// bit 0 of arg is set.
func (w *world) block(arg byte) error {
	if err := w.advance(); err != nil {
		return err
	}
	if arg&1 == 0 {
		return nil
	}
	for _, ma := range w.attempts {
		if !ma.inPool {
			continue
		}
		if !ma.a.Phase.Live() {
			return fmt.Errorf("attempt %v commits after the outbox marked it %v", ma.a.Hash[:4], ma.a.Phase)
		}
		if w.intents[ma.intent].in.State.Terminal() {
			return errors.New("an attempt of a terminal intent commits")
		}
		w.commit(ma, arg, w.lag(arg))
		return nil
	}
	return nil
}

var anteFailures = []outbox.Code{
	outbox.CodeSDKTimeoutHeight,
	outbox.CodeSDKWrongSequence,
	outbox.CodeSDKOutOfGas,
	{Codespace: outbox.CodespaceSVN, Number: 21},
}

func (w *world) commit(ma *mAttempt, arg byte, lag uint64) {
	obj := w.intents[ma.intent].obj
	tx := &mTx{height: w.height, visibleAt: w.height + lag, consumed: true, prev: w.txs[ma.a.Hash]}
	sameBID := obj.kind == objCommitted && obj.bidIntent == ma.intent && obj.bidN == ma.bidN
	switch {
	case obj.kind == objNone:
		switch (arg >> 1) % 5 {
		case 0, 1:
			tx.typed = true
			*obj = object{kind: objCommitted, relay: obj.relay, hash: ma.a.Hash, byUs: true,
				bidIntent: ma.intent, bidN: ma.bidN, visibleAt: tx.visibleAt}
		case 2:
			tx.code = outbox.CodeSVNReplay
		case 3:
			tx.code = outbox.Code{Codespace: outbox.CodespaceSVN, Number: 2}
		case 4:
			tx.code, tx.consumed = anteFailures[int(arg>>4)%len(anteFailures)], false
		}
	case sameBID && (obj.relay || obj.byUs):
		tx.idem = true
	case sameBID:
		tx.code = outbox.Code{Codespace: outbox.CodespaceSVN, Number: 2}
	default:
		tx.code = outbox.Code{Codespace: outbox.CodespaceSVN, Number: 4}
	}
	w.txs[ma.a.Hash] = tx
	ma.inPool = false
	if !tx.code.OK() {
		// CometBFT removes a failed transaction from the cache so that it
		// can be resubmitted (KeepInvalidTxsInCache is off).
		ma.cached = false
	}
	if tx.consumed {
		w.bySeq[ma.a.Sequence] = ma.a.Hash
		w.accSeq++
		w.evict()
	}
}

// foreignTx commits a transaction the outbox never stored under the
// signer's key, using its next sequence. Bit 7 of arg also reports the
// conflict to a non-terminal intent, as the sequence allocation of plan 8.1
// would.
func (w *world) foreignTx(arg byte) error {
	if arg&0x30 != 0 {
		return nil
	}
	if err := w.advance(); err != nil {
		return err
	}
	h := w.newHash()
	w.txs[h] = &mTx{height: w.height, consumed: true, visibleAt: w.height + w.lag(arg)}
	w.foreign[h] = false
	w.bySeq[w.accSeq] = h
	w.accSeq++
	w.evict()
	if arg&0x80 == 0 || len(w.intents) == 0 {
		return nil
	}
	i := w.pick(arg)
	if w.intents[i].in.State.Terminal() {
		return nil
	}
	_, err := w.apply(i, outbox.SignerConflict{})
	return err
}

// otherAccount changes the intent's object without an attempt of ours:
// another account commits it under its own business id or under the
// intent's, commits other fields, the object changes state, or a foreign
// transaction under the signer's key commits it.
func (w *world) otherAccount(i int, arg byte) error {
	m := w.intents[i]
	if m.obj.kind != objNone {
		return nil
	}
	if err := w.advance(); err != nil {
		return err
	}
	vis := w.height + w.lag(arg)
	h := w.newHash()
	obj := m.obj
	switch {
	case arg&0xc0 == 0xc0:
		w.txs[h] = &mTx{height: w.height, consumed: true, typed: true, visibleAt: vis}
		w.foreign[h] = false
		w.bySeq[w.accSeq] = h
		w.accSeq++
		w.evict()
		obj.kind, obj.hash, obj.visibleAt = objConflict, h, vis
	case arg%4 == 0, arg%4 == 1:
		w.txs[h] = &mTx{height: w.height, typed: true, visibleAt: vis}
		obj.kind, obj.hash, obj.byUs, obj.visibleAt = objCommitted, h, false, vis
		obj.bidIntent, obj.bidN = -1, 0
		if arg%4 == 1 {
			obj.bidIntent, obj.bidN = i, m.in.Business
		}
	case arg%4 == 2:
		obj.kind, obj.visibleAt = objTaken, vis
	default:
		obj.kind, obj.visibleAt = objChanged, vis
	}
	return nil
}

func (w *world) txView(h [32]byte) outbox.TxResult {
	tx := w.txs[h]
	for tx != nil && tx.visibleAt > w.height {
		tx = tx.prev
	}
	if tx == nil {
		return outbox.TxResult{}
	}
	r := outbox.TxResult{Found: true, HashVerified: true, Height: tx.height, Code: tx.code,
		AccSeq: outbox.AccSeqAbsent, TypedEvent: tx.typed, Idempotent: tx.idem}
	if tx.consumed {
		r.AccSeq = outbox.AccSeqThis
	}
	return r
}

func (w *world) search(seq uint64) []outbox.Hit {
	h, ok := w.bySeq[seq]
	if !ok || w.txs[h].visibleAt > w.height {
		return nil
	}
	owner := outbox.OwnerForeign
	if idx := w.find(h); idx >= 0 {
		owner = outbox.OwnerOurs
		if w.attempts[idx].a.Phase == outbox.AttemptDead {
			owner = outbox.OwnerOursDead
		}
	} else if w.foreign[h] {
		owner = outbox.OwnerForeignCleared
	}
	return []outbox.Hit{{Hash: h, Owner: owner}}
}

// effect is the effect check of plan 8.6 step 3 as internal/chain reports
// it, from what the indexer shows.
func (w *world) effect(i int) outbox.Effect {
	obj := w.intents[i].obj
	if obj.kind == objNone || obj.visibleAt > w.height {
		return outbox.Effect{Verdict: outbox.EffectNone}
	}
	switch obj.kind {
	case objCommitted:
		if obj.byUs {
			idx := w.find(obj.hash)
			return outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: obj.hash,
				Own: w.attempts[idx].intent == i, DeadAttempt: w.attempts[idx].a.Phase == outbox.AttemptDead}
		}
		if obj.relay {
			return outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: obj.hash}
		}
		return outbox.Effect{Verdict: outbox.EffectTaken}
	case objTaken:
		return outbox.Effect{Verdict: outbox.EffectTaken}
	case objChanged:
		return outbox.Effect{Verdict: outbox.EffectStateChanged}
	}
	return outbox.Effect{Verdict: outbox.EffectSignerConflict}
}

// poke applies every kind of event to every terminal intent: each is T17,
// with the intent unchanged, no attempt written and no send counted.
func (w *world) poke() error {
	a := outbox.Attempt{Hash: w.newHash(), Sequence: w.accSeq, TimeoutHeight: w.height + 1, Phase: outbox.AttemptSigned}
	ok := outbox.Guard{Due: true, SignerIdle: true, LockHeld: true}
	events := []outbox.Event{
		outbox.FailValidation{Reason: outbox.ReasonInvalid},
		outbox.Cancel{},
		outbox.Expire{},
		outbox.CheckEffect{Effect: outbox.Effect{Verdict: outbox.EffectNone}},
		outbox.Persist{Attempt: a, Effect: outbox.Effect{Verdict: outbox.EffectNone}, Guard: ok},
		outbox.StartSend{Attempt: a},
		outbox.RecordSend{Attempt: a, Send: outbox.Send{Outcome: outbox.SendAck}},
		outbox.Settle{},
		outbox.SignerConflict{},
	}
	for i, m := range w.intents {
		if !m.final {
			continue
		}
		for _, ev := range events {
			st, err := outbox.Apply(m.in, ev, w.lim)
			if err != nil || st.ID != outbox.T17 || st.Changed() || st.HasAttempt || st.SendStarted() {
				return fmt.Errorf("terminal intent %d: %T gave %v (err %v)", i, ev, st.ID, err)
			}
			w.hits[outbox.T17]++
		}
	}
	return nil
}

func checkReach(t *testing.T, label string, hits *reach) {
	t.Helper()
	var line strings.Builder
	for id := outbox.T1; id <= outbox.T17; id++ {
		fmt.Fprintf(&line, " %v=%d", id, hits[id])
		if hits[id] == 0 {
			t.Errorf("%s: transition %v never ran", label, id)
		}
	}
	t.Logf("%s reachability:%s", label, line.String())
}

func randomOps(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// TestStateMachineProperty runs seeded random operation sequences against
// the model and checks the invariants of plan 13.5 after every operation:
// COMMITTED only through T5, T11 or T12 with a committed, visible first
// commitment; at most one live attempt per signer and IN_FLIGHT exactly with
// one; terminal states absorb; no new attempt while one is live, before the
// effect check, or with a sequence other than the chain's next; no send after
// expiry; (d2) never with an unrecorded or non-refused send; no attempt
// commits after the outbox marked it DEAD or failed its intent; no integrity
// alert from truthful observations. Every transition must run.
func TestStateMachineProperty(t *testing.T) {
	var hits reach
	r := rand.New(rand.NewPCG(11, 2026))
	const sequences = 10000
	for n := 0; n < sequences; n++ {
		data := randomOps(r, 2*(1+r.IntN(maxModelOps)))
		if err := run(data, &hits); err != nil {
			t.Fatalf("sequence %d: %v", n, err)
		}
	}
	checkReach(t, fmt.Sprintf("%d random sequences", sequences), &hits)
}

// fuzzSeeds are the seed corpus of FuzzTransitions: hand-written paths plus
// seeded random sequences.
func fuzzSeeds() [][]byte {
	// Operation bytes index opTable: 0 create, 2 validate, 3 cancel,
	// 4 expire, 5 reconcile, 11 send, 16 block, 21 foreign, 22 other,
	// 27 poke.
	seeds := [][]byte{
		// create, reconcile (start), send ACK, block (commit), blocks,
		// reconcile (T11), poke.
		{0, 0, 5, 1, 11, 0, 16, 1, 16, 0, 16, 0, 5, 1, 27, 0},
		// create unvalidated, validate failure (T2).
		{0, 4, 2, 8},
		// create, cancel (T3), expire another (T4).
		{0, 0, 3, 0, 0, 0, 4, 17, 5, 0},
		// create relay, reconcile (start, timeout height + 6), send ACK,
		// another account commits under the intent's business id, block
		// (idempotent replay), reconcile (T12).
		{0, 1, 5, 0x51, 11, 0, 22, 1, 16, 1, 5, 1},
		// create, reconcile (start), send ACK, block (FORBIDDEN, sequence
		// consumed), reconcile with the effect check (T13).
		{0, 0, 5, 0x51, 11, 0, 16, 7, 5, 1},
	}
	r := rand.New(rand.NewPCG(13, 2026))
	for range 24 {
		seeds = append(seeds, randomOps(r, 2*maxModelOps))
	}
	return seeds
}

// FuzzTransitions runs fuzzer-chosen operation sequences against the model
// with the invariants of TestStateMachineProperty. TestTransitionReachability
// replays its seeds, and a fuzz cache corpus when one is named, and shows
// which transitions they reach.
func FuzzTransitions(f *testing.F) {
	for _, s := range fuzzSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2*maxModelOps {
			data = data[:2*maxModelOps]
		}
		if err := run(data, nil); err != nil {
			t.Fatal(err)
		}
	})
}

// corpusEnv names a directory of Go fuzz corpus files (for example the
// FuzzTransitions directory under $(go env GOCACHE)/fuzz after a -fuzz run)
// that TestTransitionReachability replays in addition to the seeds.
const corpusEnv = "SVN_OUTBOX_FUZZ_CORPUS"

const (
	maxCorpusFiles    = 100000
	maxCorpusFileSize = 1 << 20
)

// TestTransitionReachability replays the FuzzTransitions seed corpus with
// the invariants checked and requires every transition to run. When
// SVN_OUTBOX_FUZZ_CORPUS names a fuzz cache corpus, it also replays those
// inputs and requires every transition to run over the seeds and that
// corpus together, which is the corpus the fuzzer works from: the cache
// keeps only inputs that add coverage beyond the seeds. It logs the counts
// of the cache corpus on its own as well.
func TestTransitionReachability(t *testing.T) {
	var seeds reach
	for i, s := range fuzzSeeds() {
		if err := run(s, &seeds); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	checkReach(t, "fuzz seed corpus", &seeds)
	dir := os.Getenv(corpusEnv)
	if dir == "" {
		return
	}
	inputs, err := readCorpus(dir)
	if err != nil {
		t.Fatalf("%s: %v", corpusEnv, err)
	}
	var corpus reach
	for name, data := range inputs {
		if len(data) > 2*maxModelOps {
			data = data[:2*maxModelOps]
		}
		if err := run(data, &corpus); err != nil {
			t.Fatalf("corpus %s: %v", name, err)
		}
	}
	var line strings.Builder
	both := seeds
	for id := outbox.T1; id <= outbox.T17; id++ {
		fmt.Fprintf(&line, " %v=%d", id, corpus[id])
		both[id] += corpus[id]
	}
	t.Logf("cache corpus alone (%d inputs):%s", len(inputs), line.String())
	checkReach(t, "seeds and cache corpus", &both)
}

// readCorpus reads Go fuzz corpus files ("go test fuzz v1" and one []byte
// value) from dir.
func readCorpus(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 || len(entries) > maxCorpusFiles {
		return nil, fmt.Errorf("%d corpus files, want 1 to %d", len(entries), maxCorpusFiles)
	}
	out := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", e.Name())
		}
		data, err := readCorpusFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out[e.Name()] = data
	}
	return out, nil
}

func readCorpusFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), maxCorpusFileSize)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > 2 {
			return nil, errors.New("more than one value")
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(lines) != 2 || lines[0] != "go test fuzz v1" {
		return nil, errors.New("not a go test fuzz v1 file with one value")
	}
	lit, hasPrefix := strings.CutPrefix(lines[1], "[]byte(")
	lit, hasSuffix := strings.CutSuffix(lit, ")")
	if !hasPrefix || !hasSuffix {
		return nil, errors.New("the value is not []byte")
	}
	s, err := strconv.Unquote(lit)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}
