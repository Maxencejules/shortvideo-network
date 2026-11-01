package providers

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// A fault ladder proves a handler's check order. faults lists one fault per
// check, in spec order. Step i applies faults i..n together (the later ones
// first, so an earlier fault on the same field wins) and expects the code
// of fault i: every later fault is present, so a handler that ran a later
// check first would return the wrong code. The last step applies no fault
// and expects success. Every rejected step must leave every mounted store
// byte-identical and emit no event.

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
	setup    func(e *env)
	base     func(e *env) M
	faults   []fault[M]
	finalize func(t testing.TB, e *env, c *call[M])
	exec     func(e *env, ctx sdk.Context, msg M) result
}

func (l ladder[M]) run(t *testing.T) {
	t.Helper()
	for i := 0; i <= len(l.faults); i++ {
		name := "no-fault"
		want := msgkit.OK
		if i < len(l.faults) {
			name, want = l.faults[i].name, l.faults[i].want
		}
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			if l.setup != nil {
				l.setup(e)
			}
			c := &call[M]{ctx: e.fresh(), msg: l.base(e)}
			for j := len(l.faults) - 1; j >= i; j-- {
				l.faults[j].apply(e, c)
			}
			if l.finalize != nil {
				l.finalize(t, e, c)
			}
			before := e.snapshot()
			r := l.exec(e, c.ctx, c.msg)
			if r.code != want {
				t.Fatalf("code %s, want %s", r.code, want)
			}
			if want == msgkit.OK {
				return
			}
			e.requireSameSnapshot(before, name)
			if len(r.events) != 0 {
				t.Fatalf("rejected message emitted %d events", len(r.events))
			}
		})
	}
}

// Faults shared by every handler: msgkit.Enforce runs first.

func gasFault[M any]() fault[M] {
	return fault[M]{name: "enforce-gas-limit-0", want: msgkit.Bounds, apply: func(_ *env, c *call[M]) {
		c.ctx = c.ctx.WithGasMeter(newGasMeter(0))
	}}
}

func txBytesFault[M any]() fault[M] {
	return fault[M]{name: "enforce-tx-65537-bytes", want: msgkit.Bounds, apply: func(_ *env, c *call[M]) {
		c.ctx = c.ctx.WithTxBytes(make([]byte, 65537))
	}}
}

func signerLenFault[M any](set func(M, []byte)) fault[M] {
	return fault[M]{name: "enforce-signer-19-bytes", want: msgkit.Malformed, apply: func(_ *env, c *call[M]) {
		set(c.msg, providerAcct[:19])
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
