package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FuzzForwardedTx (plan 13.5) feeds arbitrary bytes to CheckForwardedTx
// against the fake chain of the forward tests. It must never panic, never
// fail with anything but a Refusal (the fake chain never fails a read),
// and accept only one provider lease message from provider-1's own
// account with a deadline in the window, reported under the hash of the
// unedited bytes.
func FuzzForwardedTx(f *testing.F) {
	w := newWorld(f)
	c := w.chain.client(f)
	valid := buildTx(f, w.forwardSpec(f))
	f.Add(valid)
	for _, edit := range []func(*txSpec){
		func(s *txSpec) { s.noTimeout = true },
		func(s *txSpec) { s.memo = "m" },
		func(s *txSpec) { s.chainID = "shortvideo-dev-ffffffffffffffff" },
		func(s *txSpec) { s.timeoutHeight += 1000 },
		func(s *txSpec) {
			second := w.acceptMsg(f, w.p1, "lease-two")
			second.Signer = w.p1Account.PubKey().Address()
			s.msgs = append(s.msgs, second)
		},
		func(s *txSpec) { s.msgs = []sdk.Msg{w.renewMsg(f, w.p1)} },
	} {
		spec := w.forwardSpec(f)
		edit(&spec)
		f.Add(buildTx(f, spec))
	}
	f.Add([]byte{})
	f.Add(valid[:len(valid)/3])

	f.Fuzz(func(t *testing.T, raw []byte) {
		got, err := CheckForwardedTx(context.Background(), c.Codec, c, w.forwardInput(raw))
		if err != nil {
			if CodeOf(err) == "" {
				t.Fatalf("non-refusal error from the fake chain: %v", err)
			}
			return
		}
		decoded, derr := c.Codec.Decode(raw)
		if derr != nil {
			t.Fatalf("accepted bytes that do not decode: %v", derr)
		}
		own, oerr := decoded.Own()
		if oerr != nil || len(decoded.Msgs) != 1 || !contains(RelayTypeURLs(), own.Msg.TypeURL) {
			t.Fatalf("accepted a transaction that is not one provider lease message: %v", oerr)
		}
		if !bytes.Equal(own.Signer.Address, w.p1Account.PubKey().Address()) || !bytes.Equal(got.Signer, own.Signer.Address) {
			t.Fatal("accepted a transaction not signed by provider-1's account")
		}
		if decoded.TimeoutHeight <= uint64(w.chain.height) || decoded.TimeoutHeight > uint64(w.chain.height)+100 {
			t.Fatalf("accepted timeout height %d outside the window", decoded.TimeoutHeight)
		}
		if got.Hash != sha256.Sum256(raw) || got.TimeoutHeight != decoded.TimeoutHeight {
			t.Fatal("the result does not describe the unedited bytes")
		}
	})
}
