package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"testing"

	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"

	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// forwardTx builds a provider-signed MsgEndLease transaction: the
// provider account's secp256k1 key signs the outer envelope for chainID
// (account 7, sequence 3), and the provider's ed25519 key signs the end
// payload.
func forwardTx(t *testing.T, acct *sdksecp.PrivKey, chainID string, timeoutHeight uint64) []byte {
	t.Helper()
	sb, err := leases.EndSignBytes("lease-1", "provider-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	pk := providerKey()
	msg := &leasesv1.MsgEndLease{
		Signer: acct.PubKey().Address(), BusinessId: "biz-fwd-1", LeaseId: "lease-1",
		ProviderSignature: &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END,
			PublicKey: pk.Public().(ed25519.PublicKey), Signature: ed25519.Sign(pk, sb), KeyId: "pkey-1",
			CanonicalProfileVersion: 1},
	}
	codec, err := chain.NewCodec(chainID)
	if err != nil {
		t.Fatal(err)
	}
	u, err := codec.Prepare(msg, acct.PubKey(), chain.TxInput{AccountNumber: 7, Sequence: 3, Gas: 300000, TimeoutHeight: timeoutHeight})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := acct.Sign(u.SignBytes())
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := u.Assemble(sig)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestForwardProviderTx checks plan 7.3 P3 (owner answer Q2) through the
// API: a provider-signed transaction with a deadline inside the window is
// enqueued verbatim, under the provider account and no broadcaster key,
// with its envelope fields; one without a deadline inside the window or
// signed for another chain id is 422 and writes nothing; one from a caller
// neither bound to the provider nor owning the committed offer is 403
// RELAY_NOT_ALLOWED and writes nothing.
func TestForwardProviderTx(t *testing.T) {
	e := newEnv(t)
	acct := sdksecp.GenPrivKeyFromSecret([]byte("api test provider account"))
	addr := acct.PubKey().Address()
	e.ch.providers["provider-1"] = &providersv1.Provider{ProviderId: "provider-1", Account: addr,
		SigningPubkey: providerKey().Public().(ed25519.PublicKey), SigningKeyId: "pkey-1",
		State: providersv1.ProviderState_PROVIDER_STATE_ADMITTED}
	e.ch.leases["provider-1/video-1"] = &leasesv1.Lease{LeaseId: "lease-1", OfferId: "offer-1", ContentId: "video-1",
		ContentVersion: 1, ProviderId: "provider-1", TermVersion: 1}
	e.ch.submitters = []*typesv1.Submitter{{Address: addr, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER}}
	e.ch.account = chain.Account{Number: 7, Sequence: 3, Height: 100}

	post := func(key string, raw []byte) answer {
		return e.do(e.as("provider-agent"), http.MethodPost, "/v1/intents", envelope(t, "FORWARD_PROVIDER_TX", key, 600,
			authContext("provider-1", "RELAY_PROVIDER_SIGNED"), map[string]any{
				"tx_base64": base64.StdEncoding.EncodeToString(raw), "provider_id": "provider-1", "content_id": "video-1",
			}))
	}
	far := post("fwd-far", forwardTx(t, acct, testChainID, 201))
	if far.status != http.StatusUnprocessableEntity || far.code(t) != string(policy.CodeForwardDeadline) {
		t.Fatalf("deadline beyond the window: %d %s", far.status, far.body)
	}
	foreign := post("fwd-chain", forwardTx(t, acct, "shortvideo-dev-other", 150))
	if foreign.status != http.StatusUnprocessableEntity || foreign.code(t) != string(policy.CodeForwardSignature) {
		t.Fatalf("signed for another chain: %d %s", foreign.status, foreign.body)
	}
	if e.st.enqueueCount() != 0 {
		t.Fatal("a refused forward was enqueued")
	}
	raw := forwardTx(t, acct, testChainID, 150)
	ok := post("fwd-1", raw)
	if ok.status != http.StatusCreated {
		t.Fatalf("valid forward: %d %s", ok.status, ok.body)
	}
	r := e.st.enqueued[0]
	want := store.ForwardFields{MessageType: policy.TypeEndLease, ProviderID: "provider-1", BusinessID: "biz-fwd-1",
		AccountNumber: 7, Sequence: 3, TimeoutHeight: 150, Gas: 300000}
	if !bytes.Equal(r.Request, raw) || !bytes.Equal(r.SignerAddress, addr) || r.Forward == nil || *r.Forward != want ||
		r.Object != (store.ObjectRef{Type: store.ObjectLease, ID: "lease-1"}) {
		t.Fatalf("forward enqueue %+v %+v", r, r.Forward)
	}
	if bytes.Equal(r.SignerAddress, serviceAddr) || bytes.Equal(r.SignerAddress, broadcasterAddr) {
		t.Fatal("a forward is pinned to a broadcaster key")
	}

	// Relay authority (plan 5.7): a caller with the kind and the
	// permission but bound to no provider, and owning no committed offer,
	// may not forward the provider's valid transaction; once it owns the
	// committed offer of the lease, it may.
	unbound := func(key string, raw []byte) answer {
		return e.do(e.as("relay-agent"), http.MethodPost, "/v1/intents", envelope(t, "FORWARD_PROVIDER_TX", key, 600,
			authContext("provider-1", "RELAY_PROVIDER_SIGNED"), map[string]any{
				"tx_base64": base64.StdEncoding.EncodeToString(raw), "provider_id": "provider-1", "content_id": "video-1",
			}))
	}
	other := forwardTx(t, acct, testChainID, 151)
	if a := unbound("fwd-unbound", other); a.status != http.StatusForbidden || a.code(t) != string(policy.CodeRelayNotAllowed) {
		t.Fatalf("forward by a caller without relay authority: %d %s", a.status, a.body)
	}
	if e.st.enqueueCount() != 1 {
		t.Fatal("a forward by a caller without relay authority was enqueued")
	}
	e.st.mu.Lock()
	e.st.scheduled["relay-agent/offer-1"] = true
	e.st.mu.Unlock()
	if a := unbound("fwd-offer-owner", other); a.status != http.StatusCreated || e.st.enqueueCount() != 2 {
		t.Fatalf("forward by the owner of the committed offer: %d %s", a.status, a.body)
	}
}
