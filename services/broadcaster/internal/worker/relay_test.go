package worker

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// providerFixture is one admitted provider of the fake node: its account
// key (development-only secp256k1, account number 10) and its ed25519
// signing key, registered as its current key.
type providerFixture struct {
	id      string
	account *secp256k1.PrivKey
	signing ed25519.PrivateKey
}

// addProvider registers provider p1 as ADMITTED with its account and
// signing key, gives its account the ADMITTED_PROVIDER role, and opens
// offer o1 of content c1 to it.
func (e *env) addProvider() providerFixture {
	e.t.Helper()
	p := providerFixture{id: "p1", account: secp256k1.GenPrivKeyFromSecret([]byte("svn broadcaster worker test provider p1")),
		signing: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))}
	addr := p.account.PubKey().Address()
	e.node.addAccount(addr, 10)
	e.node.mu.Lock()
	defer e.node.mu.Unlock()
	e.node.providers[p.id] = &providersv1.Provider{ProviderId: p.id, Account: addr, SigningPubkey: p.signing.Public().(ed25519.PublicKey),
		SigningKeyId: "pk-p1", KeyGeneration: 1, State: providersv1.ProviderState_PROVIDER_STATE_ADMITTED}
	e.node.governance.Submitters = append(e.node.governance.Submitters, &typesv1.Submitter{Address: addr,
		Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER})
	e.node.offers["o1"] = &leasesv1.Offer{OfferId: "o1", ContentId: "c1", ContentVersion: 1, ProviderId: p.id,
		ManifestDigest: digestOf("manifest c1"), PackageBytes: 4096, TermsDigest: digestOf("terms o1"),
		StartUnixSeconds: 1_900_000_000, DeadlineUnixSeconds: 1_900_003_600, ExpiryUnixSeconds: 1_900_000_600, TermVersion: 1,
		Scheduler: e.service.info.Address}
	return p
}

// acceptance is the provider's consent to take offer o1 as leaseID, signed
// with its current key over the chain's exported payload (test code).
func (e *env) acceptance(p providerFixture, leaseID string) *typesv1.InnerSignature {
	e.t.Helper()
	e.node.mu.Lock()
	offer := e.node.offers["o1"]
	e.node.mu.Unlock()
	signBytes, err := leases.AcceptSignBytes(offer, leaseID)
	if err != nil {
		e.t.Fatal(err)
	}
	return &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,
		PublicKey: p.signing.Public().(ed25519.PublicKey), Signature: ed25519.Sign(p.signing, signBytes), KeyId: "pk-p1",
		CanonicalProfileVersion: 1}
}

// providerTx is msg signed by the provider's own account at sequence seq
// (test code), with a timeout height ahead of the node.
func (e *env) providerTx(p providerFixture, msg sdk.Msg, seq, timeout uint64) []byte {
	e.t.Helper()
	u, err := e.codec.Prepare(msg, p.account.PubKey(), chain.TxInput{AccountNumber: 10, Sequence: seq, Gas: 2_000_000, TimeoutHeight: timeout})
	if err != nil {
		e.t.Fatal(err)
	}
	sig, err := p.account.Sign(u.SignBytes())
	if err != nil {
		e.t.Fatal(err)
	}
	raw, _, err := u.Assemble(sig)
	if err != nil {
		e.t.Fatal(err)
	}
	return raw
}

// holdFirst keeps the first broadcast out of the node's mempool (CheckTx
// code 0 without admission) and returns a function that admits the held
// bytes to the mempool.
func (e *env) holdFirst() func() {
	var held []byte
	e.node.onBroadcast = func(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
		if held == nil {
			held = append([]byte(nil), raw...)
			return ackWithoutMempool(raw)
		}
		return nil, nil, false
	}
	return func() {
		e.node.mu.Lock()
		defer e.node.mu.Unlock()
		e.node.onBroadcast = nil
		e.node.mempool = append(e.node.mempool, held)
	}
}

// commitmentRow is the stored commitment of an intent.
type commitmentRow struct {
	txHash, first, signer []byte
	replay, own           bool
}

func (e *env) commitment(id string) commitmentRow {
	e.t.Helper()
	var c commitmentRow
	if err := e.super.QueryRowContext(e.ctx, `SELECT c.tx_hash, c.first_commit_tx_hash, c.committing_signer, c.idempotent_replay,
			i.own_transaction FROM broadcaster.commitments c JOIN broadcaster.intents i ON i.intent_id = c.intent_id
		WHERE c.intent_id = $1`, id).Scan(&c.txHash, &c.first, &c.signer, &c.replay, &c.own); err != nil {
		e.t.Fatal(err)
	}
	return c
}

// TestProviderCommitsAcceptanceFirst (plan 13.4 FR1b at unit level, M6):
// while our relayed acceptance is held, the provider commits the same
// acceptance from its own account under its own business id. Our relay
// then commits NOT_FOUND (the offer closed); the effect check finds the
// provider's transaction with equal business fields, so the intent is
// COMMITTED with own_transaction false and the provider's transaction as
// canonical evidence. A resend's code is never taken as the result.
func TestProviderCommitsAcceptanceFirst(t *testing.T) {
	e := newEnv(t)
	p := e.addProvider()
	release := e.holdFirst()
	sig := e.acceptance(p, "l1")
	id := e.enqueue(principalApp, "accept-1", policy.KindRelayAcceptLease, &leasesv1.MsgAcceptLease{OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: sig}, time.Hour)
	s := e.session(policy.ClassBroadcaster)
	e.pass(s)
	e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
	own := &leasesv1.MsgAcceptLease{Signer: p.account.PubKey().Address(), BusinessId: "provider-accept-1", OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: sig}
	providerHash := e.node.commitDirect(e.providerTx(p, own, 0, uint64(e.node.heightNow())+50))
	release()
	e.node.block()
	ours := e.attempts(id)[0]
	if code, ok := e.node.committedCode(sumOf32(ours.TxHash)); !ok || code != uint32(typesv1.ErrorCode_ERROR_CODE_NOT_FOUND) {
		t.Fatalf("our relay committed %d (%t), want NOT_FOUND", code, ok)
	}
	e.pass(s)
	e.wantState(id, "COMMITTED", "", "")
	c := e.commitment(id)
	if c.own || c.replay || !bytes.Equal(c.txHash, providerHash[:]) || !bytes.Equal(c.first, providerHash[:]) ||
		!bytes.Equal(c.signer, p.account.PubKey().Address()) {
		t.Fatalf("commitment %+v, want the provider's transaction, not ours", c)
	}
	if got := e.attempts(id)[0]; got.Phase != store.AttemptExecutionFailed {
		t.Fatalf("our attempt %s, want EXECUTION_FAILED", got.Phase)
	}
}

func sumOf32(b []byte) [32]byte {
	var h [32]byte
	copy(h[:], b)
	return h
}

// TestIdempotentReplayNeedsFirstCommitment (plan 9.4, T12, FR1 at unit
// level): the provider commits our exact acceptance under our business id
// first; our relay then commits code 0 with only the idempotent marker.
// The intent is COMMITTED only with the first commitment, found by the
// typed event search, as canonical evidence: idempotent_replay true, the
// provider's transaction as first commitment, ours as the transaction read.
func TestIdempotentReplayNeedsFirstCommitment(t *testing.T) {
	e := newEnv(t)
	p := e.addProvider()
	release := e.holdFirst()
	sig := e.acceptance(p, "l1")
	id := e.enqueue(principalApp, "accept-1", policy.KindRelayAcceptLease, &leasesv1.MsgAcceptLease{OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: sig}, time.Hour)
	s := e.session(policy.ClassBroadcaster)
	e.pass(s)
	work, err := e.st.GetWork(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	same := &leasesv1.MsgAcceptLease{Signer: p.account.PubKey().Address(), BusinessId: work.BusinessID, OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: sig}
	providerHash := e.node.commitDirect(e.providerTx(p, same, 0, uint64(e.node.heightNow())+50))
	release()
	e.node.block()
	e.pass(s)
	e.wantState(id, "COMMITTED", "", "")
	ours := e.attempts(id)[0]
	c := e.commitment(id)
	if ours.Phase != store.AttemptCommittedIdempotent || !c.replay || c.own || !bytes.Equal(c.txHash, ours.TxHash) ||
		!bytes.Equal(c.first, providerHash[:]) {
		t.Fatalf("attempt %s, commitment %+v", ours.Phase, c)
	}
}

// TestObjectStateChangedWaitsForIndex (plan 8.6 step 3, T7): the offer of
// a queued acceptance disappears with no transaction (an expiry at
// begin-block). The effect check waits L blocks from the height at which it
// first saw the offer closed, so a transaction that closed it would be
// indexed, and then fails the intent with OBJECT_STATE_CHANGED; nothing is
// signed or sent.
func TestObjectStateChangedWaitsForIndex(t *testing.T) {
	e := newEnv(t)
	p := e.addProvider()
	id := e.enqueue(principalApp, "accept-1", policy.KindRelayAcceptLease, &leasesv1.MsgAcceptLease{OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: e.acceptance(p, "l1")}, time.Hour)
	e.node.mu.Lock()
	delete(e.node.offers, "o1")
	e.node.mu.Unlock()
	s := e.session(policy.ClassBroadcaster)
	e.pass(s)
	e.node.block()
	e.pass(s)
	e.wantState(id, "REQUESTED", "QUEUED", "")
	e.node.block()
	e.pass(s)
	e.wantState(id, "FAILED", "", "OBJECT_STATE_CHANGED")
	if e.signer.callCount() != 0 || e.node.broadcastCount() != 0 {
		t.Fatal("an acceptance of a closed offer was signed or sent")
	}
}

// enqueueForward stores a FORWARD_PROVIDER_TX intent for raw as the API
// would after its forward checks.
func (e *env) enqueueForward(p providerFixture, key string, raw []byte, msg *leasesv1.MsgAcceptLease, seq, timeout uint64) string {
	e.t.Helper()
	auth := fullAuth()
	digest, err := request.Digest(policy.KindForwardProviderTx, raw, auth)
	if err != nil {
		e.t.Fatal(err)
	}
	authJSON, err := request.CanonicalAuthJSON(auth)
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := e.st.EnqueueHTTP(e.ctx, store.EnqueueRequest{CallerPrincipal: principalApp, IdempotencyKey: key,
		Kind: store.KindForwardProvider, Request: raw, RequestDigest: digest, AuthContext: authJSON,
		SignerAddress: p.account.PubKey().Address(), Object: store.ObjectRef{Type: store.ObjectOffer, ID: msg.GetOfferId()},
		Forward: &store.ForwardFields{MessageType: store.MsgAcceptLease, ProviderID: p.id, BusinessID: msg.GetBusinessId(),
			AccountNumber: 10, Sequence: seq, TimeoutHeight: timeout, Gas: 2_000_000}, ExpiresIn: time.Hour})
	if err != nil {
		e.t.Fatal(err)
	}
	return res.IntentID
}

// TestForwardCommitsVerbatim (owner answer Q2, plan 7.3 P3): a
// provider-signed acceptance is checked again (P3 rules), persisted as its
// own attempt and sent byte for byte; no key of the broadcaster signs
// anything; the intent is COMMITTED from /tx of the forwarded bytes' hash.
func TestForwardCommitsVerbatim(t *testing.T) {
	e := newEnv(t)
	p := e.addProvider()
	msg := &leasesv1.MsgAcceptLease{Signer: p.account.PubKey().Address(), BusinessId: "provider-accept-1", OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: e.acceptance(p, "l1")}
	timeout := uint64(e.node.heightNow()) + 50
	raw := e.providerTx(p, msg, 0, timeout)
	id := e.enqueueForward(p, "forward-1", raw, msg, 0, timeout)
	f := e.forwarder()
	e.pass(f)
	e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
	hashes := e.node.broadcastHashes()
	if len(hashes) != 1 || hashes[0] != sumOf(raw) || e.signer.callCount() != 0 {
		t.Fatalf("forwarded %d broadcasts (first %x), %d signatures; want the provider's bytes only", len(hashes), hashes, e.signer.callCount())
	}
	e.node.block()
	e.pass(f)
	e.wantState(id, "COMMITTED", "", "")
	c := e.commitment(id)
	if !c.own || !bytes.Equal(c.txHash, hashes[0][:]) || !bytes.Equal(c.signer, p.account.PubKey().Address()) {
		t.Fatalf("commitment %+v", c)
	}
}

// TestForwardThatCannotCommitFailsMustResign (plan 7.3 P3): every send of
// a forwarded transaction is lost and its deadline passes: the attempt is
// dead by (d3), and because only the provider can sign new bytes the
// intent fails with PROVIDER_MUST_RESIGN instead of retrying.
func TestForwardThatCannotCommitFailsMustResign(t *testing.T) {
	e := newEnv(t)
	p := e.addProvider()
	e.node.onBroadcast = dropAll
	msg := &leasesv1.MsgAcceptLease{Signer: p.account.PubKey().Address(), BusinessId: "provider-accept-1", OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: e.acceptance(p, "l1")}
	timeout := uint64(e.node.heightNow()) + 6
	id := e.enqueueForward(p, "forward-1", e.providerTx(p, msg, 0, timeout), msg, 0, timeout)
	f := e.forwarder()
	e.pass(f)
	e.wantState(id, "IN_FLIGHT", "UNCERTAIN", "")
	e.node.blocks(int(timeout) + int(outboxLimits().IndexingMargin) - int(e.node.heightNow()))
	e.pass(f)
	e.wantState(id, "FAILED", "", "PROVIDER_MUST_RESIGN")
	if got := e.attempts(id); len(got) != 1 || got[0].DeadReason != string(store.DeadD3DeadlinePassed) {
		t.Fatalf("forwarded attempts %+v", got)
	}
}
