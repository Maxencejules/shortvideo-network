package policy

import (
	"context"
	"crypto/sha256"
	"testing"

	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
)

// txSpec describes a test transaction; zero fields take the defaults of
// a valid forwarded provider transaction.
type txSpec struct {
	msgs          []sdk.Msg
	key           cryptotypes.PrivKey
	chainID       string
	accountNumber uint64
	sequence      uint64
	timeoutHeight uint64
	noTimeout     bool
	memo          string
	fee           bool
}

// buildTx signs a transaction with the application's own TxConfig, so a
// test can produce envelopes the broadcaster never builds (memo, fee,
// several messages, no deadline, another chain id).
func buildTx(t testing.TB, spec txSpec) []byte {
	t.Helper()
	enc, err := encoding()
	if err != nil {
		t.Fatal(err)
	}
	b := enc.TxConfig.NewTxBuilder()
	if err := b.SetMsgs(spec.msgs...); err != nil {
		t.Fatal(err)
	}
	b.SetGasLimit(2_000_000)
	if !spec.noTimeout {
		b.SetTimeoutHeight(spec.timeoutHeight)
	}
	b.SetMemo(spec.memo)
	if spec.fee {
		b.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("stake", 1)))
	}
	pub := spec.key.PubKey()
	info := signingtypes.SignatureV2{PubKey: pub, Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT}, Sequence: spec.sequence}
	if err := b.SetSignatures(info); err != nil {
		t.Fatal(err)
	}
	codec, err := chain.NewCodec(spec.chainID)
	if err != nil {
		t.Fatal(err)
	}
	bech, err := codec.Bech32(pub.Address())
	if err != nil {
		t.Fatal(err)
	}
	sb, err := authsigning.GetSignBytesAdapter(context.Background(), enc.TxConfig.SignModeHandler(), signingtypes.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{Address: bech, ChainID: spec.chainID, AccountNumber: spec.accountNumber, Sequence: spec.sequence, PubKey: pub}, b.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	sig, err := spec.key.Sign(sb)
	if err != nil {
		t.Fatal(err)
	}
	info.Data = &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT, Signature: sig}
	if err := b.SetSignatures(info); err != nil {
		t.Fatal(err)
	}
	raw, err := enc.TxConfig.TxEncoder()(b.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// forwardSpec is a valid forwarded acceptance by provider-1's own account.
func (w *world) forwardSpec(t testing.TB) txSpec {
	t.Helper()
	msg := w.acceptMsg(t, w.p1, worldLeaseID)
	msg.Signer = w.p1Account.PubKey().Address()
	return txSpec{msgs: []sdk.Msg{msg}, key: w.p1Account, chainID: testChainID, accountNumber: p1AccountNum,
		sequence: p1AccountSeq, timeoutHeight: uint64(w.chain.height) + 50}
}

func (w *world) forwardInput(raw []byte) ForwardInput {
	return ForwardInput{Raw: raw, ProviderID: "provider-1", ObservedHeight: w.chain.height, ChainTime: w.chain.time,
		Window: 100, DeadlinePages: 10, Roles: w.view()}
}

// view is the governance view of the world's submitters.
func (w *world) view() GovernanceView {
	v := GovernanceView{roles: map[string]typesv1.SubmitterRole{}, seats: map[string]string{}}
	for _, s := range w.chain.submitters {
		v.roles[string(s.GetAddress())] = s.GetRole()
	}
	return v
}

// TestForwardedTxChecks covers plan 13.1: a provider-signed transaction
// forwarded verbatim passes only with one allowed message, a deadline in
// the window, the configured chain id, and the provider's own account as
// signer; each other shape is refused.
func TestForwardedTxChecks(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	c := w.chain.client(t)
	codec := c.Codec

	t.Run("valid forward passes and is not edited", func(t *testing.T) {
		raw := buildTx(t, w.forwardSpec(t))
		got, err := CheckForwardedTx(ctx, codec, c, w.forwardInput(raw))
		if err != nil {
			t.Fatal(err)
		}
		if got.Hash != sha256.Sum256(raw) || got.TypeURL != TypeAcceptLease || got.Sequence != p1AccountSeq ||
			got.AccountNumber != p1AccountNum || got.Relay.ProviderID != "provider-1" {
			t.Fatalf("forwarded = %+v", got)
		}
	})
	t.Run("valid renew and end forward", func(t *testing.T) {
		for _, msg := range []sdk.Msg{w.renewMsg(t, w.p1), w.endMsg(t, w.p1, 1)} {
			switch m := msg.(type) {
			case *leasesv1.MsgRenewLease:
				m.Signer = w.p1Account.PubKey().Address()
			case *leasesv1.MsgEndLease:
				m.Signer = w.p1Account.PubKey().Address()
			}
			spec := w.forwardSpec(t)
			spec.msgs = []sdk.Msg{msg}
			in := w.forwardInput(buildTx(t, spec))
			in.ContentID = worldContent2
			if _, err := CheckForwardedTx(ctx, codec, c, in); err != nil {
				t.Fatalf("%T: %v", msg, err)
			}
		}
	})

	refusals := map[string]struct {
		edit func(*txSpec, *ForwardInput)
		want Code
	}{
		"multi-message": {func(s *txSpec, _ *ForwardInput) {
			second := w.acceptMsg(t, w.p1, "lease-two")
			second.Signer = w.p1Account.PubKey().Address()
			s.msgs = append(s.msgs, second)
		}, CodeForwardShape},
		"wrong type": {func(s *txSpec, _ *ForwardInput) {
			s.msgs = []sdk.Msg{&leasesv1.MsgOfferLease{Signer: w.p1Account.PubKey().Address(), BusinessId: "b1", OfferId: "o9",
				ContentId: worldContent, ContentVersion: 1, ProviderId: "provider-1", ManifestDigest: digest("m"), PackageBytes: 1,
				TermsDigest: digest("t"), StartUnixSeconds: 1, DeadlineUnixSeconds: 2, ExpiryUnixSeconds: 3, TermVersion: 1}}
		}, CodeTypeNotAllowed},
		"authority end": {func(s *txSpec, in *ForwardInput) {
			end := w.endMsg(t, w.p1, 1)
			end.Signer, end.AuthorityEnd = w.p1Account.PubKey().Address(), true
			s.msgs, in.ContentID = []sdk.Msg{end}, worldContent2
		}, CodeAuthorityEnd},
		"no deadline":           {func(s *txSpec, _ *ForwardInput) { s.noTimeout = true }, CodeForwardDeadline},
		"deadline too far":      {func(s *txSpec, _ *ForwardInput) { s.timeoutHeight = uint64(w.chain.height) + 101 }, CodeForwardDeadline},
		"deadline already past": {func(s *txSpec, _ *ForwardInput) { s.timeoutHeight = uint64(w.chain.height) }, CodeForwardDeadline},
		"wrong chain":           {func(s *txSpec, _ *ForwardInput) { s.chainID = "shortvideo-dev-ffffffffffffffff" }, CodeForwardSignature},
		"wrong account number":  {func(s *txSpec, _ *ForwardInput) { s.accountNumber = p1AccountNum + 1 }, CodeForwardSignature},
		"foreign signer": {func(s *txSpec, _ *ForwardInput) {
			s.key = w.p2Account
			s.msgs[0].(*leasesv1.MsgAcceptLease).Signer = w.p2Account.PubKey().Address()
		}, CodeForwardSigner},
		"signer without a provider role": {func(s *txSpec, _ *ForwardInput) {
			k := sdksecp.GenPrivKey()
			s.key = k
			s.msgs[0].(*leasesv1.MsgAcceptLease).Signer = k.PubKey().Address()
		}, CodeForwardSigner},
		"provider account without the ADMITTED_PROVIDER role": {func(_ *txSpec, in *ForwardInput) {
			v := w.view()
			v.roles[string(w.p1Account.PubKey().Address())] = typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE
			in.Roles = v
		}, CodeForwardSigner},
		"message signer is the provider, signature and public key of another key": {func(s *txSpec, _ *ForwardInput) {
			s.key = w.p2Account
		}, CodeForwardSignature},
		"forged inner signature": {func(s *txSpec, _ *ForwardInput) {
			msg := w.acceptMsg(t, w.p1Old, worldLeaseID)
			msg.Signer = w.p1Account.PubKey().Address()
			s.msgs = []sdk.Msg{msg}
		}, CodeProviderSignature},
		"sequence already used":     {func(s *txSpec, _ *ForwardInput) { s.sequence = p1AccountSeq - 1 }, CodeForwardSequence},
		"memo":                      {func(s *txSpec, _ *ForwardInput) { s.memo = "x" }, CodeForwardShape},
		"fee":                       {func(s *txSpec, _ *ForwardInput) { s.fee = true }, CodeForwardShape},
		"named provider is another": {func(_ *txSpec, in *ForwardInput) { in.ProviderID = "provider-2" }, CodeForwardSigner},
		"named provider id invalid": {func(_ *txSpec, in *ForwardInput) { in.ProviderID = "Provider 1" }, CodeInvalid},
		"named provider unknown":    {func(_ *txSpec, in *ForwardInput) { in.ProviderID = "provider-9" }, CodeForwardSigner},
	}
	for name, tc := range refusals {
		t.Run(name, func(t *testing.T) {
			spec := w.forwardSpec(t)
			in := w.forwardInput(nil)
			tc.edit(&spec, &in)
			in.Raw = buildTx(t, spec)
			_, err := CheckForwardedTx(ctx, codec, c, in)
			wantCode(t, err, tc.want)
		})
	}

	t.Run("garbage and truncated bytes", func(t *testing.T) {
		raw := buildTx(t, w.forwardSpec(t))
		for _, bad := range [][]byte{nil, {0x0a}, raw[:len(raw)/2], append(append([]byte(nil), raw...), 0xff)} {
			_, err := CheckForwardedTx(ctx, codec, c, w.forwardInput(bad))
			wantCode(t, err, CodeForwardShape)
		}
	})
	t.Run("window bounds", func(t *testing.T) {
		in := w.forwardInput(buildTx(t, w.forwardSpec(t)))
		for _, win := range []uint64{0, MinForwardWindow - 1, MaxForwardWindow + 1} {
			in.Window = win
			if _, err := CheckForwardedTx(ctx, codec, c, in); err == nil || CodeOf(err) != "" {
				t.Fatalf("window %d: %v", win, err)
			}
		}
	})
}
