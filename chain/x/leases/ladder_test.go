package leases_test

import (
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// A fault ladder proves a handler's check order and that every reject row
// reaches its own branch. faults lists one fault per check, in check order.
//
//   - cumulative step i applies faults i..n together (the later ones first,
//     so an earlier fault on the same field wins) and expects the code of
//     fault i: every later fault is present, so a handler that ran a later
//     check first would return that check's code;
//   - isolated step i applies fault i alone and expects the same code: a
//     handler without check i would accept the message, so the row reaches
//     the branch that rejects it even when a later check shares its code;
//   - the last step applies no fault and expects success.
//
// Every step runs the handler on a cached branch of the context (the trial
// state). A rejected step must leave the trial branch's leases, providers,
// and content stores byte-identical to the state before the call and emit
// no event.

type call[M any] struct {
	ctx sdk.Context
	msg M
	// sig edits the inner signature after the message is signed, in order.
	sig []func(*typesv1.InnerSignature)
	// unsigned skips signing (the fault removed the signature).
	unsigned bool
}

type fault[M any] struct {
	name  string
	want  msgkit.Code
	apply func(e *env, c *call[M])
}

type ladder[M any] struct {
	setup func(e *env)
	base  func(e *env) M
	// sign signs the message after the faults are applied, unless the call
	// is unsigned; it then applies the call's signature edits.
	sign   func(e *env, c *call[M])
	faults []fault[M]
	exec   func(e *env, ctx sdk.Context, msg M) result
}

func (l ladder[M]) step(t *testing.T, apply []int, want msgkit.Code) {
	t.Helper()
	e := newEnv(t)
	if l.setup != nil {
		l.setup(e)
	}
	c := &call[M]{ctx: e.fresh(), msg: l.base(e)}
	for j := len(apply) - 1; j >= 0; j-- {
		l.faults[apply[j]].apply(e, c)
	}
	if l.sign != nil && !c.unsigned {
		l.sign(e, c)
	}
	r := l.exec(e, c.ctx, c.msg)
	if want == msgkit.OK {
		e.requireOK(r, "no fault")
		return
	}
	e.requireReject(r, want, t.Name())
}

func (l ladder[M]) run(t *testing.T) {
	t.Helper()
	for i := range l.faults {
		f := l.faults[i]
		var cumulative []int
		for j := i; j < len(l.faults); j++ {
			cumulative = append(cumulative, j)
		}
		t.Run("cumulative/"+f.name, func(t *testing.T) { l.step(t, cumulative, f.want) })
		t.Run("isolated/"+f.name, func(t *testing.T) { l.step(t, []int{i}, f.want) })
	}
	t.Run("no-fault", func(t *testing.T) { l.step(t, nil, msgkit.OK) })
}

// Faults shared by every handler: msgkit.Enforce runs first.

func gasFault[M any]() fault[M] {
	return fault[M]{name: "enforce-gas-limit-0", want: msgkit.Bounds, apply: func(_ *env, c *call[M]) {
		c.ctx = c.ctx.WithGasMeter(storetypes.NewGasMeter(0))
	}}
}

func txBytesFault[M any]() fault[M] {
	return fault[M]{name: "enforce-tx-65537-bytes", want: msgkit.Bounds, apply: func(_ *env, c *call[M]) {
		c.ctx = c.ctx.WithTxBytes(make([]byte, 65537))
	}}
}

func signerLenFault[M any](set func(M, []byte)) fault[M] {
	return fault[M]{name: "enforce-signer-19-bytes", want: msgkit.Malformed, apply: func(_ *env, c *call[M]) {
		set(c.msg, serviceAcct[:19])
	}}
}

// sigFault edits the inner signature after signing.
func sigFault[M any](name string, want msgkit.Code, edit func(*typesv1.InnerSignature)) fault[M] {
	return fault[M]{name: name, want: want, apply: func(_ *env, c *call[M]) { c.sig = append(c.sig, edit) }}
}

func (c *call[M]) editSignature(sig *typesv1.InnerSignature) {
	for _, edit := range c.sig {
		edit(sig)
	}
}

// shapeFaults are the structural inner-signature faults in
// msgkit.CheckInnerSignature order, for a signature expected in domain
// want; other is another known domain.
func shapeFaults[M any](other typesv1.SignatureDomain) []fault[M] {
	return []fault[M]{
		sigFault[M]("sig-domain-unspecified", msgkit.Malformed, func(s *typesv1.InnerSignature) {
			s.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_UNSPECIFIED
		}),
		sigFault[M]("sig-domain-unknown-99", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.Domain = 99 }),
		sigFault[M]("sig-wrong-domain", msgkit.WrongDomain, func(s *typesv1.InnerSignature) { s.Domain = other }),
		sigFault[M]("sig-profile-0", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 0 }),
		sigFault[M]("sig-profile-2", msgkit.OutOfRange, func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 2 }),
		sigFault[M]("sig-key-id-empty", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.KeyId = "" }),
		sigFault[M]("sig-key-id-pattern", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.KeyId = "KEY-P1" }),
		sigFault[M]("sig-public-key-31", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.PublicKey = s.PublicKey[:31] }),
		sigFault[M]("sig-signature-63", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.Signature = s.Signature[:63] }),
	}
}

// replayFault commits the message's business id in another module's
// ledger with another digest: REPLAY at the gate.
func replayFault[M any](businessID func(M) string) fault[M] {
	return fault[M]{name: "gate-replay-other-module", want: msgkit.Replay, apply: func(e *env, c *call[M]) {
		e.peer.digests[businessID(c.msg)] = make([]byte, 32)
	}}
}

// TestTrialSnapshotDetectsEarlyWrite: the reject check of the ladders sees
// a handler that writes to any of the three stores, or emits an event,
// before it fails (mutants of a handler), and accepts one that fails
// cleanly.
func TestTrialSnapshotDetectsEarlyWrite(t *testing.T) {
	e := newEnv(t)
	for name, key := range e.keys {
		r := e.exec(e.fresh(), func(ctx sdk.Context) error {
			ctx.KVStore(key).Set([]byte{0x0e, 'x'}, []byte{1})
			return msgkit.Bounds.Err()
		})
		if r.code != msgkit.Bounds || r.trialSame {
			t.Fatalf("write to %s before a failure was not seen", name)
		}
	}
	r := e.exec(e.fresh(), func(ctx sdk.Context) error {
		ctx.EventManager().EmitEvent(sdk.NewEvent("leases_offer_closed"))
		return msgkit.Bounds.Err()
	})
	if len(r.events) != 1 {
		t.Fatal("event before a failure was not seen")
	}
	r = e.exec(e.fresh(), func(sdk.Context) error { return msgkit.Bounds.Err() })
	if !r.trialSame || len(r.events) != 0 || r.code != msgkit.Bounds {
		t.Fatal("a clean failure was reported as a write")
	}
}
