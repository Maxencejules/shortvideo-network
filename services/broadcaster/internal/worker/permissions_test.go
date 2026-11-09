package worker

// Tests of the object permissions the worker enforces itself: at the full
// validation of a SQL-path intent, where they are the only check, and
// again immediately before signing (plan 6.3, 5.7, 7.3, 7.4); of the
// effect check before a retry attempt (plan 8.6 step 3); and of the host
// rule of the owner promotion (plan 5.7). Real store and PostgreSQL; the
// node and the signer are the unit-test fakes of harness_test.go.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// innerJSON is the request JSON of an inner signature.
func innerJSON(sig *typesv1.InnerSignature) string {
	return fmt.Sprintf(`{"domain":%q,"public_key_hex":%q,"signature_hex":%q,"key_id":%q,"canonical_profile_version":%d}`,
		sig.GetDomain().String(), hex.EncodeToString(sig.GetPublicKey()), hex.EncodeToString(sig.GetSignature()), sig.GetKeyId(),
		sig.GetCanonicalProfileVersion())
}

// authJSON is the SQL-path authorization context of fullAuth for
// principal.
func authJSON(t *testing.T, principal string) string {
	t.Helper()
	auth := fullAuth()
	auth.PrincipalID = principal
	b, err := json.Marshal(auth)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// registerSignedBy is registerMsg with the hosting intent signed by key
// (test code).
func (e *env) registerSignedBy(contentID string, key ed25519.PrivateKey) *contentv1.MsgRegisterContent {
	e.t.Helper()
	m := e.registerMsg(contentID)
	sb, err := content.RegisterIntentSignBytes(e.service.info.Address, contentID, 1, m.GetManifestDigest().GetValue(), m.GetPackageBytes(),
		content.DevnetRoleName)
	if err != nil {
		e.t.Fatal(err)
	}
	m.HostingIntent.PublicKey, m.HostingIntent.Signature = key.Public().(ed25519.PublicKey), ed25519.Sign(key, sb)
	return m
}

// withdrawJSON is the SQL-path request of a withdrawal of contentID
// version 1 by the service key, with the withdraw intent signed by key
// (test code).
func (e *env) withdrawJSON(contentID string, key ed25519.PrivateKey) string {
	e.t.Helper()
	reason := sha([]byte("reason " + contentID))
	sb, err := content.WithdrawIntentSignBytes(e.service.info.Address, contentID, 1, reason, content.DevnetRoleName)
	if err != nil {
		e.t.Fatal(err)
	}
	sig := &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT,
		PublicKey: key.Public().(ed25519.PublicKey), Signature: ed25519.Sign(key, sb), KeyId: "intent-key-1", CanonicalProfileVersion: 1}
	return fmt.Sprintf(`{"content_id":%q,"content_version":1,"reason_digest_hex":%q,"hosting_intent":%s}`, contentID,
		hex.EncodeToString(reason), innerJSON(sig))
}

// validated reports whether the worker validated intent id.
func (e *env) validated(id string) bool {
	e.t.Helper()
	work, err := e.st.GetWork(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return work.Validated
}

// TestSQLPathRelayPermission (plan 5.7, 7.3; owner answer Q2): on the SQL
// path the worker's validation is the only permission check. A caller not
// bound to the provider and owning no committed offer may neither relay a
// provider's valid acceptance nor forward the provider's own signed
// transaction: both fail UNAUTHORIZED (RELAY_NOT_ALLOWED), nothing is
// signed, stored as an attempt or sent. The same requests from a caller
// bound to the provider are validated.
func TestSQLPathRelayPermission(t *testing.T) {
	e := newEnv(t)
	p := e.addProvider()
	e.w.callers["sql-bound"] = policy.Caller{Principal: "sql-bound", Kinds: policy.Kinds(), BoundProviderID: p.id}
	unbound, bound := e.sqlCaller(principalSQL), e.sqlCaller("sql-bound")
	auth := authJSON(t, creator)
	sig := e.acceptance(p, "l1")
	acceptJSON := fmt.Sprintf(`{"offer_id":"o1","lease_id":"l1","provider_acceptance":%s,"provider_id":"p1","content_id":"c1"}`, innerJSON(sig))
	own := &leasesv1.MsgAcceptLease{Signer: p.account.PubKey().Address(), BusinessId: "provider-accept-1", OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: sig}
	raw := e.providerTx(p, own, 0, uint64(e.node.heightNow())+50)
	forwardJSON := fmt.Sprintf(`{"tx_base64":%q,"provider_id":"p1","content_id":"c1"}`, base64.StdEncoding.EncodeToString(raw))

	relay := sqlEnqueue(t, unbound, "RELAY_ACCEPT_LEASE", acceptJSON, auth, "relay-unbound")
	forward := sqlEnqueue(t, unbound, "FORWARD_PROVIDER_TX", forwardJSON, auth, "forward-unbound")
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.wantState(relay, "FAILED", "", "UNAUTHORIZED")
	e.wantState(forward, "FAILED", "", "UNAUTHORIZED")
	e.pass(e.session(policy.ClassBroadcaster))
	e.pass(e.forwarder())
	if len(e.attempts(relay)) != 0 || len(e.attempts(forward)) != 0 || e.signer.callCount() != 0 || e.node.broadcastCount() != 0 {
		t.Fatal("a relay or forward by a caller without relay authority was signed, stored or sent")
	}

	boundRelay := sqlEnqueue(t, bound, "RELAY_ACCEPT_LEASE", acceptJSON, auth, "relay-bound")
	boundForward := sqlEnqueue(t, bound, "FORWARD_PROVIDER_TX", forwardJSON, auth, "forward-bound")
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !e.validated(boundRelay) || !e.validated(boundForward) {
		t.Fatalf("the bound caller's relay (%+v) or forward (%+v) was not validated\nworker log:\n%s", e.intent(boundRelay),
			e.intent(boundForward), e.logs.String())
	}
}

// TestSQLPathPublicationPermission (plan 7.4; owner answer Q3): the signer
// checks only that a hosting intent's key is registered, so the worker's
// publication check is what binds a SQL-path content request to the
// principal. A register whose intent key is not in the registry fails
// UNAUTHORIZED; a withdrawal of the caller's EFFECTIVE content whose
// context names another principal, with a withdraw intent signed by that
// principal's own registered key, fails UNAUTHORIZED; neither is signed.
// The owner principal's withdrawal is validated.
func TestSQLPathPublicationPermission(t *testing.T) {
	e := newEnv(t)
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize))
	registry, err := policy.ParseRegistry([]byte(fmt.Sprintf(`{"entries":[{"principal_id":%q,"public_key_hex":%q,"label":"test creator"},`+
		`{"principal_id":"creator-2","public_key_hex":%q,"label":"second creator"}]}`, creator,
		hex.EncodeToString(e.intentKey.Public().(ed25519.PublicKey)), hex.EncodeToString(other.Public().(ed25519.PublicKey)))))
	if err != nil {
		t.Fatal(err)
	}
	e.w.cfg.Registry = registry
	db := e.sqlCaller(principalSQL)
	s := e.session(policy.ClassService)

	stray := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize))
	unregistered := sqlEnqueue(t, db, "REGISTER_CONTENT", registerJSON(e.registerSignedBy("c9", stray)), authJSON(t, creator), "reg-stray")
	reg := sqlEnqueue(t, db, "REGISTER_CONTENT", registerJSON(e.registerMsg("c1")), authJSON(t, creator), "reg-1")
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.wantState(unregistered, "FAILED", "", "UNAUTHORIZED")
	e.pass(s)
	e.node.block()
	e.pass(s)
	e.wantState(reg, "COMMITTED", "", "")
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.ownerStatus(reg) != "EFFECTIVE" {
		t.Fatalf("owner row %s, want EFFECTIVE", e.ownerStatus(reg))
	}
	calls, sends := e.signer.callCount(), e.node.broadcastCount()
	if calls != 1 || len(e.attempts(unregistered)) != 0 {
		t.Fatalf("%d signatures (want the register's one), unregistered intent attempts %d", calls, len(e.attempts(unregistered)))
	}

	foreign := sqlEnqueue(t, db, "WITHDRAW_CONTENT", e.withdrawJSON("c1", other), authJSON(t, "creator-2"), "wd-other")
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.wantState(foreign, "FAILED", "", "UNAUTHORIZED")
	e.pass(s)
	if len(e.attempts(foreign)) != 0 || e.signer.callCount() != calls || e.node.broadcastCount() != sends {
		t.Fatal("a withdrawal for another principal's content was signed, stored or sent")
	}
	owner := sqlEnqueue(t, db, "WITHDRAW_CONTENT", e.withdrawJSON("c1", e.intentKey), authJSON(t, creator), "wd-owner")
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !e.validated(owner) {
		t.Fatalf("the owner's withdrawal was not validated: %+v\nworker log:\n%s", e.intent(owner), e.logs.String())
	}
}

// TestRelayRevalidatedBeforeSigning (plan 6.3, 7.3 P1): a relay accepted
// at enqueue is checked again before it is signed. The provider's key is
// rotated on chain after enqueue, so its acceptance no longer verifies
// against the provider's current key: the intent fails
// PROVIDER_SIGNATURE_INVALID and the signer is never asked.
func TestRelayRevalidatedBeforeSigning(t *testing.T) {
	e := newEnv(t)
	p := e.addProvider()
	id := e.enqueue(principalApp, "accept-1", policy.KindRelayAcceptLease, &leasesv1.MsgAcceptLease{OfferId: "o1", LeaseId: "l1",
		ProviderAcceptance: e.acceptance(p, "l1")}, time.Hour)
	rotated := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{10}, ed25519.SeedSize))
	e.node.mu.Lock()
	e.node.providers[p.id].SigningPubkey = rotated.Public().(ed25519.PublicKey)
	e.node.providers[p.id].SigningKeyId = "pk-p1-2"
	e.node.providers[p.id].KeyGeneration = 2
	e.node.mu.Unlock()
	e.pass(e.session(policy.ClassBroadcaster))
	e.wantState(id, "FAILED", "", "PROVIDER_SIGNATURE_INVALID")
	if len(e.attempts(id)) != 0 || e.signer.callCount() != 0 || e.node.broadcastCount() != 0 {
		t.Fatal("a relay whose provider signature no longer verifies was signed, stored or sent")
	}
}

// TestEffectCheckBeforeRetry (plan 8.6 step 3: before any new attempt,
// not only the first): attempt 1 of a register dies by (d3) and the intent
// waits in RETRY_WAIT; the step that ends attempt 1 reconciles the live
// attempt only (settleLive), so the retry cannot start before the chain
// changes. Before the retry the chain changes:
//   - another service account registers the same content id: the intent
//     fails OBJECT_TAKEN from the effect check, with no second attempt;
//   - a transaction signed by our service key that the broadcaster never
//     stored registers the content: the signer halts with SIGNER_CONFLICT.
//
// In both cases nothing more is signed or sent.
func TestEffectCheckBeforeRetry(t *testing.T) {
	retryWait := func(t *testing.T) (*env, *session, string) {
		e := newEnv(t)
		e.node.onBroadcast = dropAll
		id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
		s := e.session(policy.ClassService)
		e.pass(s)
		first := e.attempts(id)[0]
		e.node.blocks(int(first.TimeoutHeight) + int(outboxLimits().IndexingMargin) - int(e.node.heightNow()))
		e.settleLive(s)
		e.wantState(id, "REQUESTED", "RETRY_WAIT", "D3_DEADLINE_PASSED")
		e.node.onBroadcast = nil
		return e, s, id
	}
	t.Run("content registered by another service", func(t *testing.T) {
		e, s, id := retryWait(t)
		other := newKeyFixture("service-2", policy.ClassService)
		e.node.addAccount(other.info.Address, 9)
		e.signer.keys[other.info.Name] = other
		taken := withSigner(e.registerMsg("c1"), other.info.Address)
		setBusinessID(taken, "reg-other")
		raw, _ := e.signWith(other, taken, 0)
		e.node.commitDirect(raw)
		e.node.blocks(int(outboxLimits().IndexingMargin))
		calls, sends := e.signer.callCount(), e.node.broadcastCount()
		e.until("retry decided", 100, func() { e.pass(s) }, func() bool { return e.intent(id).state != "REQUESTED" })
		e.wantState(id, "FAILED", "", "OBJECT_TAKEN")
		if len(e.attempts(id)) != 1 || e.signer.callCount() != calls || e.node.broadcastCount() != sends {
			t.Fatal("a retry was signed or sent after another service took the content id")
		}
	})
	t.Run("content registered by our key under a foreign hash", func(t *testing.T) {
		e, s, id := retryWait(t)
		ours := withSigner(e.registerMsg("c1"), e.service.info.Address)
		setBusinessID(ours, "reg-foreign")
		raw, _ := e.signWith(e.service, ours, 0)
		e.node.commitDirect(raw)
		calls, sends := e.signer.callCount(), e.node.broadcastCount()
		e.until("signer halted", 100, func() { e.pass(s) }, func() bool {
			sg, err := e.st.GetSigner(e.ctx, e.service.info.Address)
			return err == nil && sg.HaltedReason != ""
		})
		sg, err := e.st.GetSigner(e.ctx, e.service.info.Address)
		if err != nil || sg.HaltedReason != "SIGNER_CONFLICT" {
			t.Fatalf("signer %+v %v, want SIGNER_CONFLICT", sg, err)
		}
		if len(e.attempts(id)) != 1 || e.signer.callCount() != calls || e.node.broadcastCount() != sends {
			t.Fatal("a retry was signed or sent after our key committed a transaction the broadcaster did not store")
		}
	})
}

// TestOwnerHostMismatchStaysPending (plan 5.7, owner answer Q3): an owner
// row becomes EFFECTIVE only when Query/Content shows the row's signer as
// the record's submitting host. The node hides the record while the
// register commits (so the confirmation at commit cannot run), then shows
// it with another host: the row stays PENDING and the worker raises
// CONTENT_HOST_MISMATCH; once the node shows our host, the row becomes
// EFFECTIVE.
func TestOwnerHostMismatchStaysPending(t *testing.T) {
	e := newEnv(t)
	s := e.session(policy.ClassService)
	reg := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	e.node.mu.Lock()
	e.node.hidden[contentKey("c1", 1)] = true
	e.node.mu.Unlock()
	e.pass(s)
	e.node.block()
	e.pass(s)
	e.wantState(reg, "COMMITTED", "", "")
	if e.ownerStatus(reg) != "PENDING" {
		t.Fatal("owner row confirmed while the node hid the record")
	}
	other := newKeyFixture("service-2", policy.ClassService)
	e.node.mu.Lock()
	delete(e.node.hidden, contentKey("c1", 1))
	e.node.contents[contentKey("c1", 1)].Content.SubmittingHost = other.info.Address
	e.node.mu.Unlock()
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.ownerStatus(reg) != "PENDING" || !e.logged("CONTENT_HOST_MISMATCH") {
		t.Fatalf("owner row %s with another host on chain (alert logged %t), want PENDING and an alert", e.ownerStatus(reg),
			e.logged("CONTENT_HOST_MISMATCH"))
	}
	e.node.mu.Lock()
	e.node.contents[contentKey("c1", 1)].Content.SubmittingHost = e.service.info.Address
	e.node.mu.Unlock()
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.ownerStatus(reg) != "EFFECTIVE" {
		t.Fatalf("owner row %s once the node shows our host, want EFFECTIVE", e.ownerStatus(reg))
	}
}
