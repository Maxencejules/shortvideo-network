package app_test

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestRejectedAuditMessagesLeaveTheFullState: through FinalizeBlock, one or
// more cases for every row of the SubmitAuditBatch check order
// (chain/x/evidence/README.md, "Check order") that a committed transaction
// can reach in this app, in check order, except the rows listed below. The
// base message is valid: a Simulate dry run on the committed state
// succeeds. Each case changes one thing, so it passes every earlier check,
// and asserts its exact svn code, that the ante handler committed (the
// handler ran), that the full non-account state is byte-identical, and
// that no typed event was emitted. Fields the attestation signature covers
// are changed before signing, so a case reaches its own step and not the
// signature step; the signature structure and bytes are changed after
// signing. Ante rejections leave the account store unchanged too.
//
// Rows no case here reaches, and where they are tested:
//   - step 1 (msgkit.Enforce): gas limit and transaction size are refused by
//     the ante handler first (the ante cases below); a signer that is not
//     20 bytes cannot sign a transaction (the ante resolves the signer to
//     an account address); keeper ladder enforce rows.
//   - step 2, a nil item: an item on the wire always decodes as a message
//     (an empty item fails the evidence_id presence case below); keeper
//     ladder presence-item-nil.
//   - step 3, no role (UNAUTHORIZED): the ante admits only submitter rows
//     and council seats (outsider case below); a seat is refused by the
//     ante (TestSeatEvidenceRefusedForTheSeatRule); keeper ladder role-none.
//   - step 15, a business payload with no canonical form (MALFORMED): every
//     integer of the business payload is also signed, so such a payload is
//     UNAUTHORIZED at step 8 first (the content_version 2^53 case below).
//   - step 26, content absent (NOT_FOUND): needs a lease that names
//     unregistered content, an invariant violation (keeper ladder).
//   - step 27 (T9, the per-auditor per-epoch cap): 1024 items are reached in
//     TestDishonestAdmittedAuditorInApp, which asserts BOUNDS with the full
//     state unchanged and the next epoch's batch committing.
//
// Step 24 (T6, a held lease past its deadline that expiry has not closed)
// needs an expiry backlog: a council sets max_expiry_steps to 1 and the
// last case runs in the block whose header time reaches the deadline of
// five held leases, where BeginBlock closes only the first.
func TestRejectedAuditMessagesLeaveTheFullState(t *testing.T) {
	e := newEvNet()
	c := e.start(t, 1)
	fx := evidenceSetup(t, e, c)
	chainApp := c.app()
	p, p2 := prefix(e.auditor1), prefix(e.auditor2)
	priv1, priv2 := edPriv(t, e.auditor1), edPriv(t, e.auditor2)
	pub1, pub2 := priv1.Public().(ed25519.PublicKey), priv2.Public().(ed25519.PublicKey)
	now := c.now()
	epoch := c.epoch()

	// Committed starting state: one recorded batch (its batch id and
	// evidence id are used), le ended, c5 (lw's content) withdrawn.
	used := e.audit(t, e.auditor1, "rj-used", p+"rj-used", epoch, auditItem(p+"rj-used-e", "la", "c1", manifestOf("c1"), now+600))
	wantCode(t, "used batch", e.send(t, c, e.auditor1, used), msgkit.OK)
	wantCode(t, "end le", e.send(t, c, e.broadcaster, e.endMsg(t, e.broadcaster, "rj-end-le", "le", "p1", 1)), msgkit.OK)
	wantCode(t, "withdraw c5", e.send(t, c, e.service, e.withdrawContent(t, e.service, "rj-wd-c5", "c5", 1, reasonDigest("c5"))), msgkit.OK)

	// build returns a batch from auditor-1 with one item of la. pre edits
	// the message before it is signed by auditor-1's key (a payload with no
	// SVN-JCS-1 form gets 64 placeholder signature bytes, which step 8
	// refuses), and post edits it after signing.
	build := func(id string, pre, post func(m *evidencev1.MsgSubmitAuditBatch)) *evidencev1.MsgSubmitAuditBatch {
		t.Helper()
		m := unsignedBatch(e.auditor1, pub1, "key-auditor-1", "rj-"+id, p+"rj-"+id, epoch,
			auditItem(p+"rj-"+id+"-e", "la", "c1", manifestOf("c1"), now+600))
		if pre != nil {
			pre(m)
		}
		if m.AuditorSignature != nil {
			if signBytes, err := evidence.AuditSignBytes(m); err == nil {
				m.AuditorSignature.Signature = ed25519.Sign(priv1, signBytes)
			} else {
				m.AuditorSignature.Signature = make([]byte, ed25519.SignatureSize)
			}
		}
		if post != nil {
			post(m)
		}
		return m
	}
	item := func(m *evidencev1.MsgSubmitAuditBatch) *evidencev1.EvidenceItem { return m.Items[0] }
	if _, _, err := chainApp.Simulate(e.signed(t, c, e.auditor1, registryGas, nil, build("dry", nil, nil))); err != nil {
		t.Fatalf("base batch does not succeed: %v", err)
	}

	type rejectCase struct {
		name string
		who  *actor
		msg  func() sdk.Msg
		want msgkit.Code
	}
	n := 0
	next := func() string { n++; return fmt.Sprintf("x%03d", n) }
	before := func(name string, edit func(m *evidencev1.MsgSubmitAuditBatch), want msgkit.Code) rejectCase {
		id := next()
		return rejectCase{name, e.auditor1, func() sdk.Msg { return build(id, edit, nil) }, want}
	}
	after := func(name string, edit func(m *evidencev1.MsgSubmitAuditBatch), want msgkit.Code) rejectCase {
		id := next()
		return rejectCase{name, e.auditor1, func() sdk.Msg { return build(id, nil, edit) }, want}
	}
	// many is n items of la with short ids.
	many := func(m *evidencev1.MsgSubmitAuditBatch, count int) {
		m.Items = nil
		for i := 0; i < count; i++ {
			m.Items = append(m.Items, auditItem(fmt.Sprintf("%s-%03d", m.BatchId, i), "la", "c1", manifestOf("c1"), now+600))
		}
	}
	// The smallest batch of maximal ids whose canonical payload is above
	// the 65536-byte SVN-JCS-1 cap; its transaction still fits in 65536
	// bytes (asserted below), so the handler refuses it at step 8.
	long := strings.Repeat("a", 64-len(p)-5)
	payloadItems := 0
	oversize := func(m *evidencev1.MsgSubmitAuditBatch) {
		m.Items = nil
		for i := 0; ; i++ {
			m.Items = append(m.Items, auditItem(fmt.Sprintf("%s%s%05d", p, long, i), strings.Repeat("l", 64), strings.Repeat("c", 64), manifestOf("c1"), now+600))
			if _, err := evidence.AuditCanonical(m); errors.Is(err, evidence.ErrPayloadTooLarge) {
				payloadItems = len(m.Items)
				return
			}
			if len(m.Items) > 1000 {
				t.Fatal("no payload above the cap")
			}
		}
	}

	cases := []rejectCase{
		// 2: presence.
		before("empty business_id", func(m *evidencev1.MsgSubmitAuditBatch) { m.BusinessId = "" }, msgkit.Malformed),
		before("empty batch_id", func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId = "" }, msgkit.Malformed),
		after("nil auditor_signature", func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature = nil }, msgkit.Malformed),
		before("empty evidence_id", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).EvidenceId = "" }, msgkit.Malformed),
		before("empty lease_id", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).LeaseId = "" }, msgkit.Malformed),
		before("empty content_id", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ContentId = "" }, msgkit.Malformed),
		after("nil observation_digest", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ObservationDigest = nil }, msgkit.Malformed),
		// 3: role (the ante admits these accounts; the handler refuses).
		{"service account as auditor", e.service, func() sdk.Msg {
			m := build(next(), nil, nil)
			m.Signer = e.service.addr
			return m
		}, msgkit.Forbidden},
		{"provider account as auditor", e.provider1, func() sdk.Msg {
			m := build(next(), nil, nil)
			m.Signer = e.provider1.addr
			return m
		}, msgkit.Forbidden},
		{"broadcaster relays an auditor-1 batch", e.broadcaster, func() sdk.Msg {
			m := build(next(), nil, nil)
			m.Signer = e.broadcaster.addr
			return m
		}, msgkit.Forbidden},
		// 4: schema version (D-5).
		before("schema_version 0", func(m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 0 }, msgkit.Malformed),
		before("schema_version 2", func(m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 2 }, msgkit.OutOfRange),
		// 5: observation digest.
		after("digest algorithm 0", func(m *evidencev1.MsgSubmitAuditBatch) {
			item(m).ObservationDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
		}, msgkit.Malformed),
		after("unknown digest algorithm 99", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ObservationDigest.Algorithm = 99 }, msgkit.Malformed),
		after("31-byte digest", func(m *evidencev1.MsgSubmitAuditBatch) {
			item(m).ObservationDigest.Value = item(m).ObservationDigest.Value[:31]
		}, msgkit.Bounds),
		// 6: signature structure.
		after("signature domain 0", func(m *evidencev1.MsgSubmitAuditBatch) {
			m.AuditorSignature.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_UNSPECIFIED
		}, msgkit.Malformed),
		after("unknown signature domain 99", func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.Domain = 99 }, msgkit.Malformed),
		after("signature in the provider renew domain", func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.Domain = domainRenew }, msgkit.WrongDomain),
		after("canonical profile 0", func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.CanonicalProfileVersion = 0 }, msgkit.Malformed),
		after("canonical profile 2", func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.CanonicalProfileVersion = 2 }, msgkit.OutOfRange),
		after("empty key_id", func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.KeyId = "" }, msgkit.Malformed),
		after("key_id outside the id pattern", func(m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.KeyId = "Key-1" }, msgkit.Bounds),
		after("31-byte public key", func(m *evidencev1.MsgSubmitAuditBatch) {
			m.AuditorSignature.PublicKey = m.AuditorSignature.PublicKey[:31]
		}, msgkit.Bounds),
		after("63-byte signature", func(m *evidencev1.MsgSubmitAuditBatch) {
			m.AuditorSignature.Signature = m.AuditorSignature.Signature[:63]
		}, msgkit.Bounds),
		// 7: the attestation key derives another address (D-2).
		{"auditor-2's key signs auditor-1's batch", e.auditor1, func() sdk.Msg {
			id := next()
			m := unsignedBatch(e.auditor1, pub2, "key-auditor-2", "rj-"+id, p+"rj-"+id, epoch, auditItem(p+"rj-"+id+"-e", "la", "c1", manifestOf("c1"), now+600))
			return signAudit(t, m, priv2)
		}, msgkit.Unauthorized},
		// 8: payload and signature (D-1, D-6).
		after("expiry changed after signing", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ExpiryUnixSeconds++ }, msgkit.Unauthorized),
		after("item order changed after signing", func(m *evidencev1.MsgSubmitAuditBatch) {
			m.Items = append(m.Items, auditItem(m.BatchId+"-2", "lb", "c2", manifestOf("c2"), now+600))
			m.Items[0], m.Items[1] = m.Items[1], m.Items[0]
		}, msgkit.Unauthorized),
		before("content_version 2^53 (no SVN-JCS-1 form)", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ContentVersion = 1 << 53 }, msgkit.Unauthorized),
		before("epoch 2^53 (no SVN-JCS-1 form)", func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = 1 << 53 }, msgkit.Unauthorized),
		before("canonical payload above 65536 bytes", oversize, msgkit.Bounds),
		// 9: id patterns.
		before("batch_id outside the id pattern", func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId = strings.ToUpper(m.BatchId) }, msgkit.Bounds),
		before("65-byte evidence_id", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).EvidenceId = p + strings.Repeat("e", 65-len(p)) }, msgkit.Bounds),
		before("lease_id outside the id pattern", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).LeaseId = "LA" }, msgkit.Bounds),
		before("content_id outside the id pattern", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ContentId = "C1" }, msgkit.Bounds),
		// 10: kind.
		before("kind 0", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).Kind = 0 }, msgkit.Malformed),
		before("reserved kind 3", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).Kind = 3 }, msgkit.Malformed),
		before("reserved kind 4", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).Kind = 4 }, msgkit.Malformed),
		before("reserved kind 5", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).Kind = 5 }, msgkit.Malformed),
		before("unknown kind 99", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).Kind = 99 }, msgkit.Malformed),
		// 11: item count.
		before("no item", func(m *evidencev1.MsgSubmitAuditBatch) { m.Items = nil }, msgkit.Bounds),
		before("33 items (cap 32)", func(m *evidencev1.MsgSubmitAuditBatch) { many(m, int(genesis.MaxEvidenceBatch)+1) }, msgkit.Bounds),
		// 12: attestation.
		before("attestation false", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).Attestation = false }, msgkit.Malformed),
		// 13: round (D-3).
		before("previous epoch", func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = epoch - 1 }, msgkit.StaleVersion),
		before("next epoch", func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = epoch + 1 }, msgkit.StaleVersion),
		before("epoch 0", func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = 0 }, msgkit.StaleVersion),
		// 14: expiry range.
		before("expiry 0", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ExpiryUnixSeconds = 0 }, msgkit.Bounds),
		before("expiry after 9999-12-31", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ExpiryUnixSeconds = maxUnix + 1 }, msgkit.Bounds),
		// 15: business gate.
		before("business_id outside the id pattern", func(m *evidencev1.MsgSubmitAuditBatch) { m.BusinessId = "RJ-BID" }, msgkit.Bounds),
		before("business id of a lease acceptance", func(m *evidencev1.MsgSubmitAuditBatch) { m.BusinessId = "ac-la" }, msgkit.Replay),
		before("committed audit business id, another payload", func(m *evidencev1.MsgSubmitAuditBatch) { m.BusinessId = "rj-used" }, msgkit.Replay),
		// 16: duplicate ids.
		before("batch_id already used", func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId = p + "rj-used" }, msgkit.DuplicateID),
		before("evidence_id already stored", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).EvidenceId = p + "rj-used-e" }, msgkit.DuplicateID),
		before("evidence_id repeated in the batch (D-6)", func(m *evidencev1.MsgSubmitAuditBatch) {
			m.Items = append(m.Items, auditItem(item(m).EvidenceId, "lb", "c2", manifestOf("c2"), now+600))
		}, msgkit.DuplicateID),
		// 17: item expiry.
		before("expiry at the committed time", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ExpiryUnixSeconds = now }, msgkit.Expired),
		// 18: lease.
		before("unknown lease", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).LeaseId = "l404" }, msgkit.NotFound),
		// 19: id prefixes (D-17).
		before("batch_id without the signer's prefix", func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId = "rj-no-prefix" }, msgkit.Forbidden),
		before("evidence_id with auditor-2's prefix", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).EvidenceId = p2 + "rj-foreign" }, msgkit.Forbidden),
		// 20: provider receipts are not recorded on chain (D-8).
		before("kind PROVIDER_RECEIPT", func(m *evidencev1.MsgSubmitAuditBatch) {
			item(m).Kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
		}, msgkit.Forbidden),
		// 21: lifetime (D-9).
		before("expiry 86401 s ahead", func(m *evidencev1.MsgSubmitAuditBatch) {
			item(m).ExpiryUnixSeconds = now + evidence.MaxEvidenceLifetimeSeconds + 1
		}, msgkit.Bounds),
		// 22: lease/content binding (D-10).
		before("content_id of another lease", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ContentId = "c2" }, msgkit.Conflict),
		before("content_version 2", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).ContentVersion = 2 }, msgkit.Conflict),
		// 23: lease not held (D-11).
		before("ended lease le", func(m *evidencev1.MsgSubmitAuditBatch) { item(m).LeaseId, item(m).ContentId = "le", "c4" }, msgkit.Conflict),
		// 25: expiry after the lease deadline (D-18).
		before("expiry after lx's deadline", func(m *evidencev1.MsgSubmitAuditBatch) {
			item(m).LeaseId, item(m).ContentId = "lx", "c6"
			item(m).ExpiryUnixSeconds = fx.setupNow + 901
		}, msgkit.Conflict),
		// 26: withdrawn content (D-13).
		before("lease lw of withdrawn content", func(m *evidencev1.MsgSubmitAuditBatch) {
			item(m).LeaseId, item(m).ContentId = "lw", "c5"
			item(m).ObservationDigest = digestOf(manifestOf("c5"))
		}, msgkit.Withdrawn),
	}
	run := func(tc rejectCase) {
		t.Helper()
		msg := tc.msg()
		before := chainApp.FullSnapshot()
		res := e.send(t, c, tc.who, msg)
		wantCode(t, tc.name, res, tc.want)
		if !anteCommitted(res) {
			t.Fatalf("%s: rejected in the ante handler, not by x/evidence", tc.name)
		}
		unchanged(t, tc.name, chainApp, before, res)
		if _, states, idem := evidenceEvents(t, res); len(states) != 0 || idem {
			t.Fatalf("%s: evidence events", tc.name)
		}
	}
	for _, tc := range cases {
		run(tc)
	}
	// The oversize case's transaction fit in 65536 bytes: the handler, not
	// the ante's size bound, refused it.
	big := build("size", oversize, nil)
	raw := e.signed(t, c, e.auditor1, registryGas, nil, big)
	if payloadItems == 0 || len(raw) > 65536 {
		t.Fatalf("payload cap case: %d items, transaction %d bytes", payloadItems, len(raw))
	}
	t.Logf("canonical payload above 65536 bytes at %d maximal-id items; that transaction is %d bytes", payloadItems, len(raw))

	// Ante rejections: nothing reaches a handler, and the account store does
	// not change either.
	ante := []struct {
		name   string
		raw    func() []byte
		svn    msgkit.Code
		sdkErr *errorsmod.Error
	}{
		{"outsider", func() []byte {
			outsider := newEdAccount("outsider")
			id := next()
			msg := signAudit(t, unsignedBatch(outsider, outsider.key.PubKey().Bytes(), "key-outsider", "rj-"+id, prefix(outsider)+id, epoch,
				auditItem(prefix(outsider)+id+"-e", "la", "c1", manifestOf("c1"), now+600)), edPriv(t, outsider))
			return signFull(t, chainApp, e.doc.ChainID, outsider.key, 0, 0, registryGas, nil, msg)
		}, "", sdkerrors.ErrUnknownAddress},
		{"fee", func() []byte {
			return e.signed(t, c, e.auditor1, registryGas, sdk.NewCoins(sdk.NewInt64Coin("reject", 1)), build(next(), nil, nil))
		}, msgkit.FeeRejected, nil},
		{"gas limit 0", func() []byte { return e.signed(t, c, e.auditor1, 0, nil, build(next(), nil, nil)) }, msgkit.Bounds, nil},
		{"memo", func() []byte {
			return e.signEnvelope(t, e.auditor1, build(next(), nil, nil), func(body *txtypes.TxBody, _ *txtypes.AuthInfo) { body.Memo = "x" }, nil)
		}, msgkit.Bounds, nil},
		{"70 KiB transaction", func() []byte {
			return e.signed(t, c, e.auditor1, registryGas, nil, build(next(), func(m *evidencev1.MsgSubmitAuditBatch) {
				m.BusinessId = strings.Repeat("b", 70*1024)
			}, nil))
		}, msgkit.Bounds, nil},
	}
	for _, tc := range ante {
		before := chainApp.FullSnapshot()
		accBefore := chainApp.ModuleSnapshot("acc")
		res := e.deliverRaw(t, c, e.auditor1, tc.raw())
		if res.Code == 0 || anteCommitted(res) {
			t.Fatalf("%s: code %d ante committed %t", tc.name, res.Code, anteCommitted(res))
		}
		if tc.sdkErr != nil {
			if res.Codespace != sdkerrors.RootCodespace || res.Code != tc.sdkErr.ABCICode() {
				t.Fatalf("%s: got %s/%d want sdk/%d (%s)", tc.name, res.Codespace, res.Code, tc.sdkErr.ABCICode(), tc.sdkErr)
			}
		} else {
			wantCode(t, tc.name, res, tc.svn)
		}
		unchanged(t, tc.name, chainApp, before, res)
		if !bytes.Equal(accBefore, chainApp.ModuleSnapshot("acc")) {
			t.Fatalf("%s: account store changed", tc.name)
		}
	}

	// 24 (T6, D-12): max_expiry_steps 1. lx (deadline fx.setupNow+900) is
	// closed first; then in the block whose header time is fx.deadline (the
	// deadline of la, lb, lc, and lw), BeginBlock closes only la, so lb is
	// still held past its deadline when the audit in that block runs.
	e.council(t, c, "rj-expiry-1", msgkit.LimitMaxExpirySteps, governedPayload(msgkit.LimitMaxExpirySteps, 1))
	c.setUnix(fx.setupNow + 900)
	c.block(t)
	c.setUnix(fx.deadline)
	id := next()
	late := e.audit(t, e.auditor1, "rj-"+id, p+"rj-"+id, c.epoch(), auditItem(p+"rj-"+id+"-e", "lb", "c2", manifestOf("c2"), fx.deadline+60))
	evidenceBefore := chainApp.ModuleSnapshot(app.ModuleEvidence)
	res := e.send(t, c, e.auditor1, late)
	wantCode(t, "held lease past its deadline", res, msgkit.Expired)
	if !anteCommitted(res) || len(typedEvents(res.Events)) != 0 {
		t.Fatal("held lease past its deadline: not a handler rejection")
	}
	if lb := heldLeaseState(t, chainApp, "lb"); !lb.GetReservationHeld() || lb.GetDeadlineUnixSeconds() > fx.deadline {
		t.Fatalf("lb is not held past its deadline in the backlog block: %v", lb)
	}
	if la := heldLeaseState(t, chainApp, "la"); la.GetReservationHeld() {
		t.Fatal("la was not closed in the backlog block")
	}
	// The block closed la (BeginBlock writes the lease store); the audit
	// wrote nothing.
	if !bytes.Equal(evidenceBefore, chainApp.ModuleSnapshot(app.ModuleEvidence)) {
		t.Fatal("held lease past its deadline: the evidence store changed")
	}
	c.checkEvidenceInvariants(t, "after every rejection")
	t.Logf("%d handler reject cases, %d ante cases, 1 expiry-backlog case", len(cases), len(ante))
}
