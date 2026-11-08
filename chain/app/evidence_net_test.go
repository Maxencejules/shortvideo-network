package app_test

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The evidence tests run the real application with x/evidence wired to the
// real x/authority, x/leases, and x/content keepers, and deliver signed
// transactions block by block. Each auditor account has an ed25519 account
// key that signs both its SDK transactions and its
// SVN-EVIDENCE-ATTESTATION-1 batches (owner ruling on Q1). Every key is
// generated at test time; nothing here is a mock, and nothing here is a
// storage proof: a RECORDED record means the auditor reported the lease's
// manifest digest, not that any byte was fetched.

// evNet is a registry devnet (newRegNet: DEC-001/002/003/005/008 devnet
// parameters) with three AUDITOR accounts: auditor-1 and auditor-2 have
// ed25519 account keys, so their attestations can bind to their accounts;
// auditor-secp has a secp256k1 account key, whose address no ed25519
// attestation key derives.
type evNet struct {
	*regNet
	auditor1    *actor
	auditor2    *actor
	auditorSecp *actor
}

// newEdAccount is an account with a fresh ed25519 key (the key type of the
// council seats and of auditor accounts).
func newEdAccount(name string) *actor {
	key := sdked.GenPrivKey()
	return &actor{name: name, key: key, addr: key.PubKey().Address()}
}

func newEvNet(drop ...string) *evNet {
	e := &evNet{
		regNet:      newRegNet(drop...),
		auditor1:    newEdAccount("auditor-1"),
		auditor2:    newEdAccount("auditor-2"),
		auditorSecp: newSecp("auditor-secp"),
	}
	e.doc.Submitters = append(e.doc.Submitters,
		genesis.Submitter{AddressHex: e.auditor1.hexAddr(), Role: genesis.RoleAuditor},
		genesis.Submitter{AddressHex: e.auditor2.hexAddr(), Role: genesis.RoleAuditor},
		genesis.Submitter{AddressHex: e.auditorSecp.hexAddr(), Role: genesis.RoleAuditor},
	)
	e.numbers = accountNumbers(e.doc)
	return e
}

// prefix is the evidence id and batch id prefix of an account (D-17).
func prefix(who *actor) string { return evidence.IDPrefix(who.addr) }

// edPriv is the ed25519 private key of an ed25519 account.
func edPriv(t testing.TB, who *actor) ed25519.PrivateKey {
	t.Helper()
	key, ok := who.key.(*sdked.PrivKey)
	if !ok {
		t.Fatalf("%s has no ed25519 account key", who.name)
	}
	return ed25519.PrivateKey(key.Key)
}

// auditItem is one RECORDED-or-REJECTED item of a held lease: kind
// AUDITOR_ATTESTATION, attestation true.
func auditItem(evidenceID, leaseID, contentID string, observation []byte, expiry int64) *evidencev1.EvidenceItem {
	return &evidencev1.EvidenceItem{
		EvidenceId:        evidenceID,
		Kind:              evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		LeaseId:           leaseID,
		ContentId:         contentID,
		ContentVersion:    1,
		ObservationDigest: digestOf(observation),
		ExpiryUnixSeconds: expiry,
		Attestation:       true,
	}
}

// unsignedBatch is a batch from who at epoch with a signature structure for
// signer's public key and no signature bytes yet.
func unsignedBatch(who *actor, pub []byte, keyID, bid, batchID string, epoch uint64, items ...*evidencev1.EvidenceItem) *evidencev1.MsgSubmitAuditBatch {
	return &evidencev1.MsgSubmitAuditBatch{
		Signer:        who.addr,
		BusinessId:    bid,
		BatchId:       batchID,
		Epoch:         epoch,
		SchemaVersion: evidence.SchemaVersion,
		Items:         items,
		AuditorSignature: &typesv1.InnerSignature{
			Domain:                  typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION,
			PublicKey:               append([]byte(nil), pub...),
			KeyId:                   keyID,
			CanonicalProfileVersion: 1,
		},
	}
}

// signAudit sets msg's signature to an ed25519 signature by priv over
// evidence.AuditSignBytes(msg). Tests that edit a field after signing call
// it only for the fields the signature must cover.
func signAudit(t testing.TB, msg *evidencev1.MsgSubmitAuditBatch, priv ed25519.PrivateKey) *evidencev1.MsgSubmitAuditBatch {
	t.Helper()
	signBytes, err := evidence.AuditSignBytes(msg)
	if err != nil {
		t.Fatal(err)
	}
	msg.AuditorSignature.Signature = ed25519.Sign(priv, signBytes)
	return msg
}

// audit is a batch sent and signed by auditor who (its account key signs
// the attestation, owner ruling on Q1).
func (e *evNet) audit(t testing.TB, who *actor, bid, batchID string, epoch uint64, items ...*evidencev1.EvidenceItem) *evidencev1.MsgSubmitAuditBatch {
	t.Helper()
	priv := edPriv(t, who)
	msg := unsignedBatch(who, priv.Public().(ed25519.PublicKey), "key-"+who.name, bid, batchID, epoch, items...)
	return signAudit(t, msg, priv)
}

// epoch is the current audit round of the next block.
func (c *cluster) epoch() uint64 { return evidence.EpochAt(c.now()) }

// renewNaming builds a provider-signed renewal of leaseID to termVersion
// and deadline that names evidenceID (x/leases renew check 13).
func (n *regNet) renewNaming(t testing.TB, who *actor, bid, leaseID, providerID, contentID string, size, termVersion uint64, deadline int64, evidenceID string) *leasesv1.MsgRenewLease {
	t.Helper()
	msg := n.renewMsg(t, who, bid, leaseID, providerID, contentID, size, termVersion, deadline)
	signBytes, err := leases.RenewSignBytes(leaseID, termVersion, deadline, msg.GetManifestDigest(), size, evidenceID)
	if err != nil {
		t.Fatal(err)
	}
	msg.EvidenceId = evidenceID
	msg.ProviderSignature = innerSig(domainRenew, n.signing[providerID], signBytes)
	return msg
}

// queryEvidence runs svn.evidence.v1.Query/Evidence at height (0 = latest)
// and requires code 0.
func queryEvidence(t *testing.T, chainApp *app.App, req *evidencev1.QueryEvidenceRequest, height int64) (*evidencev1.QueryEvidenceResponse, []byte) {
	t.Helper()
	raw, res := query(t, chainApp, "/svn.evidence.v1.Query/Evidence", req, height)
	if res.Code != 0 {
		t.Fatalf("Evidence %v at %d: %s/%d %s", req, height, res.Codespace, res.Code, res.Log)
	}
	var out evidencev1.QueryEvidenceResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return &out, raw
}

// recordState is the reported state of one evidence id at height, or
// UNSPECIFIED when no record exists.
func recordState(t *testing.T, chainApp *app.App, evidenceID string, height int64) evidencev1.EvidenceState {
	t.Helper()
	out, _ := queryEvidence(t, chainApp, &evidencev1.QueryEvidenceRequest{EvidenceId: evidenceID}, height)
	switch len(out.GetRecords()) {
	case 0:
		return evidencev1.EvidenceState_EVIDENCE_STATE_UNSPECIFIED
	case 1:
		return out.GetRecords()[0].GetState()
	default:
		t.Fatalf("evidence %s: %d records", evidenceID, len(out.GetRecords()))
		return 0
	}
}

func wantState(t *testing.T, label string, chainApp *app.App, evidenceID string, height int64, want evidencev1.EvidenceState) {
	t.Helper()
	if got := recordState(t, chainApp, evidenceID, height); got != want {
		t.Fatalf("%s: %s at height %d is %s, want %s", label, evidenceID, height, got, want)
	}
}

const (
	stateRecorded = evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED
	stateRejected = evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED
	stateExpired  = evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED
	stateNone     = evidencev1.EvidenceState_EVIDENCE_STATE_UNSPECIFIED
)

// otherStores is every snapshot store except evidence: an evidence message
// never writes any of them.
func otherStores(chainApp *app.App) []byte {
	var out []byte
	for _, name := range app.SnapshotStores() {
		if name == app.ModuleEvidence {
			continue
		}
		out = append(out, chainApp.ModuleSnapshot(name)...)
	}
	return out
}

// evidenceEvents returns, for one committed result, the evidence ids of the
// typed EvidenceAccepted events, the states of the evidence_state events,
// and whether an evidence_result idempotent event is present. Every typed
// event must parse (msgkit.ParseCommitted).
func evidenceEvents(t *testing.T, res *abci.ExecTxResult) (accepted []string, states []string, idempotent bool) {
	t.Helper()
	for _, ev := range res.Events {
		switch {
		case ev.Type == "svn.events.v1.EvidenceAccepted":
			msg, _, err := msgkit.ParseCommitted(ev)
			if err != nil {
				t.Fatalf("EvidenceAccepted: %v", err)
			}
			typed, ok := msg.(interface{ GetEvidenceId() string })
			if !ok {
				t.Fatalf("EvidenceAccepted parsed as %T", msg)
			}
			accepted = append(accepted, typed.GetEvidenceId())
		case ev.Type == evidence.EventState:
			for _, attr := range ev.Attributes {
				if attr.Key == evidence.AttrState {
					states = append(states, attr.Value)
				}
			}
		case ev.Type == "evidence_result":
			idempotent = true
		}
	}
	return accepted, states, idempotent
}

// evidenceFixture is the held-lease setup of the evidence tests: providers
// p1 and p2 admitted, content c1-c6 registered by the service account, and
// leases lA (c1, p1), lB (c2, p1), lC (c3, p2), lE (c4, p1), lW (c5, p1),
// lX (c6, p2), each with deadline setupNow+1800 except lX (setupNow+900).
// Every lease term is the DEC-005 devnet number (3600 s); the start time is
// in the past. Package manifests are synthetic and carry canaries; only
// their digests are on chain.
type evidenceFixture struct {
	setupNow int64
	deadline int64
}

var fixtureLeases = []struct {
	lease, content, provider string
}{
	{"la", "c1", "p1"}, {"lb", "c2", "p1"}, {"lc", "c3", "p2"},
	{"le", "c4", "p1"}, {"lw", "c5", "p1"}, {"lx", "c6", "p2"},
}

func evidenceSetup(t *testing.T, e *evNet, c *cluster) evidenceFixture {
	t.Helper()
	e.registerAdmit(t, c, e.provider1, "p1", capacity1)
	e.registerAdmit(t, c, e.provider2, "p2", capacity1)
	for _, row := range fixtureLeases {
		wantCode(t, "register "+row.content, e.send(t, c, e.service, e.registerContent(t, e.service, "reg-"+row.content, row.content, 1, packageManifest(row.content), sizeSmall)), msgkit.OK)
	}
	now := c.now()
	fx := evidenceFixture{setupNow: now, deadline: now + 1800}
	for _, row := range fixtureLeases {
		deadline := fx.deadline
		if row.lease == "lx" {
			deadline = now + 900
		}
		offer := e.offerMsg(e.service, "of-"+row.lease, "o-"+row.lease, row.content, row.provider, sizeSmall, deadline, now+600)
		wantCode(t, "offer "+row.lease, e.send(t, c, e.service, offer), msgkit.OK)
		wantCode(t, "accept "+row.lease, e.send(t, c, e.broadcaster, e.acceptMsg(t, e.broadcaster, "ac-"+row.lease, offer, row.lease)), msgkit.OK)
	}
	c.checkInvariants(t, "evidence setup")
	return fx
}

// manifestOf is the lease manifest digest value of a fixture content id: a
// RECORDED observation.
func manifestOf(contentID string) []byte { return contentManifest(contentID).GetValue() }

// wrongDigest is a digest that is not any lease's manifest digest: a
// REJECTED observation (here, the SHA-256 of a synthetic failing report).
func wrongDigest(label string) []byte { return sha([]byte("svn-audit-report-synthetic-fail:" + label)) }

// checkEvidenceInvariants runs evidencetest.CheckInvariants over the
// committed state of every node of c.
func (c *cluster) checkEvidenceInvariants(t *testing.T, label string) {
	t.Helper()
	for i, node := range c.nodes {
		if err := node.app.CheckEvidenceInvariantsForTest(); err != nil {
			t.Fatalf("%s: node %d: %v", label, i, err)
		}
	}
}

// signedAudit returns the signed transaction bytes of an audit message.
func (e *evNet) signedAudit(t *testing.T, c *cluster, who *actor, msg sdk.Msg) []byte {
	t.Helper()
	return e.signed(t, c, who, registryGas, nil, msg)
}

// sameBytes fails when two byte strings differ.
func sameBytes(t *testing.T, label string, a, b []byte) {
	t.Helper()
	if !bytes.Equal(a, b) {
		t.Fatalf("%s: bytes differ (%d vs %d)", label, len(a), len(b))
	}
}
