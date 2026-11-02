package app_test

import (
	"fmt"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// seatOnlyLog is the start of the ante's refusal of a seat-signed
// transaction that carries a message other than propose or approve.
const seatOnlyLog = "a council seat key may sign only MsgProposeChange and MsgApproveChange"

// TestSeatKeysSignOnlyCouncilMessages (owner ruling DR-06-06): a council
// seat account has no submitter row and is admitted from the seat list. It
// signs propose and approve, which commit. Every other message it signs is
// ERROR_CODE_FORBIDDEN in the ante: the six registry messages, the four
// lease messages (the three BROADCASTER relays and OfferLease) plus an
// authority_end EndLease, ExecuteChange, and the evidence message
// (SubmitAuditBatch). Each of the four seats sends each of these thirteen
// (52 rows). For every row, CheckTx refuses
// the transaction with the same code, so it never enters the mempool; in
// FinalizeBlock the ante does not commit, the seat's sequence does not move,
// the full non-account state is byte-identical, and no typed event is
// emitted. A seat-signed transaction that mixes a council message with a
// lease message is refused as a whole; the council message alone, and two
// council messages in one transaction, commit. Afterwards the same lease
// messages, relayed by the BROADCASTER account that is not a seat (or sent
// by the service and authority accounts), and the authority execute, all
// commit: the seat rows were refused for the seat rule, not for their
// content.
func TestSeatKeysSignOnlyCouncilMessages(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	for _, seat := range n.seats {
		if row, role, admission := chainApp.SeatViewForTest(seat.addr); row || role != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED || admission != authority.AdmittedSeat {
			t.Fatalf("%s: admission row %v, role %v, admission %v", seat.name, row, role, admission)
		}
	}
	if _, _, admission := chainApp.SeatViewForTest(n.broadcaster.addr); admission != authority.AdmittedSubmitter {
		t.Fatalf("broadcaster admission %v", admission)
	}

	// Seat-signed propose and approve commit (councilReady requires code 0
	// for the seat-1 propose and the three approvals).
	n.registerAdmit(t, c, n.provider1, "p1", capacity1)
	wantCode(t, "register p2", n.send(t, c, n.provider2, n.registerProvider(t, n.provider2, "reg-p2", "p2", groupDevnet, capacity1)), msgkit.OK)
	n.councilReady(t, c, "prop-admit-p2", "admit-p2", admitPayload("p2"))
	for _, id := range []string{"c1", "c2", "c3"} {
		wantCode(t, "register "+id, n.send(t, c, n.service, n.registerContent(t, n.service, "reg-"+id, id, 1, packageManifest(id), sizeC1)), msgkit.OK)
	}
	now := c.now()
	deadline, expiry := now+1800, now+600
	o1 := n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeC1, deadline, expiry)
	wantCode(t, "offer o1", n.send(t, c, n.service, o1), msgkit.OK)
	wantCode(t, "accept l1", n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l1", o1, "l1")), msgkit.OK)
	o2 := n.offerMsg(n.service, "of-o2", "o2", "c2", "p1", sizeC1, deadline, expiry)
	wantCode(t, "offer o2", n.send(t, c, n.service, o2), msgkit.OK)

	// refused delivers a seat-signed transaction and requires the ante seat
	// refusal in CheckTx and in FinalizeBlock with nothing changed.
	refused := func(label string, who *actor, raw []byte) {
		t.Helper()
		checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatalf("%s: CheckTx: %v", label, err)
		}
		forbidden := uint32(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN)
		if checked.Codespace != "svn" || checked.Code != forbidden || !strings.Contains(checked.Log, seatOnlyLog) {
			t.Fatalf("%s: CheckTx %s/%d log %s", label, checked.Codespace, checked.Code, checked.Log)
		}
		before := chainApp.FullSnapshot()
		seqBefore, known := chainApp.AccountSequenceForTest(who.addr)
		if !known {
			t.Fatalf("%s: %s has no auth account", label, who.name)
		}
		res := n.deliverRaw(t, c, who, raw)
		wantCode(t, label, res, msgkit.Forbidden)
		if anteCommitted(res) || !strings.Contains(res.Log, seatOnlyLog) {
			t.Fatalf("%s: not refused by the ante seat rule: log %s", label, res.Log)
		}
		if seqAfter, _ := chainApp.AccountSequenceForTest(who.addr); seqAfter != seqBefore {
			t.Fatalf("%s: sequence %d -> %d", label, seqBefore, seqAfter)
		}
		unchanged(t, label, chainApp, before, res)
		if len(res.Events) != 0 {
			t.Fatalf("%s: %d events", label, len(res.Events))
		}
	}

	names := []string{
		"RegisterProvider", "UpdateProvider", "RotateProviderKey", "DrainProvider", "RegisterContent", "WithdrawContent",
		"OfferLease", "AcceptLease", "RenewLease", "EndLease (provider)", "EndLease (authority_end)",
		"ExecuteChange", "SubmitAuditBatch",
	}
	rows := 0
	for i, seat := range n.seats {
		bid := func(s string) string { return fmt.Sprintf("seat-%d-%s", i, s) }
		build := map[string]func() sdk.Msg{
			"RegisterProvider": func() sdk.Msg {
				return n.registerProvider(t, seat, bid("reg"), fmt.Sprintf("ps%d", i), groupDevnet, capacity1)
			},
			"UpdateProvider": func() sdk.Msg { return n.updateProvider(t, seat, bid("upd"), "p1", capacity2, "r") },
			"RotateProviderKey": func() sdk.Msg {
				return n.rotate(t, seat, bid("rot"), "p1", 1, newSigningKey(fmt.Sprintf("p1-key-s%d", i)))
			},
			"DrainProvider": func() sdk.Msg {
				return n.drain(t, seat, bid("drn"), "p1", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)
			},
			"RegisterContent": func() sdk.Msg {
				return n.registerContent(t, seat, bid("rc"), fmt.Sprintf("cs%d", i), 1, packageManifest("cs"), sizeC1)
			},
			"WithdrawContent": func() sdk.Msg { return n.withdrawContent(t, seat, bid("wc"), "c1", 1, reasonDigest("r")) },
			"OfferLease": func() sdk.Msg {
				return n.offerMsg(seat, bid("of"), "o3", "c3", "p1", sizeC1, deadline, expiry)
			},
			"AcceptLease": func() sdk.Msg { return n.acceptMsg(t, seat, bid("ac"), o2, "l2") },
			"RenewLease": func() sdk.Msg {
				return n.renewMsg(t, seat, bid("rn"), "l1", "p1", "c1", sizeC1, 2, deadline+leaseTerm)
			},
			"EndLease (provider)": func() sdk.Msg { return n.endMsg(t, seat, bid("en"), "l1", "p1", 1) },
			"EndLease (authority_end)": func() sdk.Msg {
				return &leasesv1.MsgEndLease{Signer: seat.addr, BusinessId: bid("ena"), LeaseId: "l1", AuthorityEnd: true}
			},
			"ExecuteChange": func() sdk.Msg {
				return &authorityv1.MsgExecuteChange{Signer: seat.addr, BusinessId: bid("ex"), ProposalId: "prop-admit-p2"}
			},
			"SubmitAuditBatch": func() sdk.Msg {
				return &evidencev1.MsgSubmitAuditBatch{Signer: seat.addr, BusinessId: bid("au"), BatchId: bid("batch"), Epoch: 1, SchemaVersion: 1}
			},
		}
		for _, name := range names {
			rows++
			refused(seat.name+": "+name, seat, n.signed(t, c, seat, registryGas, nil, build[name]()))
		}
	}
	if rows != 52 {
		t.Fatalf("%d rows, want 52", rows)
	}

	// Mixed transactions. seat-1 signs a valid propose with an accept of o2:
	// refused as a whole. The propose alone commits.
	mixed := n.proposal(t, "prop-mixed", "admit-p3", admitPayload("p3"))
	refused("seat-1: propose + AcceptLease", n.seats[0],
		n.signedMulti(t, c, n.seats[0], registryGas, []sdk.Msg{mixed, n.acceptMsg(t, n.seats[0], "seat-mix-ac", o2, "l2")}))
	wantCode(t, "seat-1: propose alone", n.send(t, c, n.seats[0], mixed), msgkit.OK)
	// seat-2 signs two council messages in one transaction: both commit.
	pair := n.proposal(t, "prop-pair", "admit-p4", admitPayload("p4"))
	pair.Signer = n.seats[1].addr
	approve := &authorityv1.MsgApproveChange{Signer: n.seats[1].addr, BusinessId: "prop-mixed-ap-2", ProposalId: "prop-mixed"}
	n.sendMulti(t, c, n.seats[1], "seat-2: propose + approve", []sdk.Msg{pair, approve})
	for _, id := range []string{"prop-mixed", "prop-pair"} {
		if state, found, err := chainApp.ProposalStateForTest(id); err != nil || !found || state != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
			t.Fatalf("%s: state %v found %v err %v", id, state, found, err)
		}
	}

	// The same lease messages from the accounts that hold the roles commit.
	wantCode(t, "service offer o3", n.send(t, c, n.service, n.offerMsg(n.service, "of-o3", "o3", "c3", "p1", sizeC1, deadline, expiry)), msgkit.OK)
	wantCode(t, "broadcaster accept l2", n.send(t, c, n.broadcaster, n.acceptMsg(t, n.broadcaster, "ac-l2", o2, "l2")), msgkit.OK)
	wantCode(t, "broadcaster renew l1", n.send(t, c, n.broadcaster, n.renewMsg(t, n.broadcaster, "rn-l1", "l1", "p1", "c1", sizeC1, 2, deadline+leaseTerm)), msgkit.OK)
	wantCode(t, "broadcaster end l1", n.send(t, c, n.broadcaster, n.endMsg(t, n.broadcaster, "en-l1", "l1", "p1", 2)), msgkit.OK)
	wantCode(t, "authority end l2", n.send(t, c, n.authority, n.authorityEndMsg("en-l2", "l2")), msgkit.OK)
	res := n.execute(t, c, "prop-admit-p2")
	wantCode(t, "authority execute admit-p2", res, msgkit.OK)
	if code := authorityResultCode(res); code != "" {
		t.Fatalf("authority execute admit-p2 kept with %s", code)
	}
	n.policy++
	c.checkInvariants(t, "after the seat rows")
}

// TestSeatRotationMovesAnteAdmission (DR-06-06): after the engine-only
// RotateCouncilSeat, the retired key's account is refused by the ante
// (ERROR_CODE_UNAUTHORIZED, sequence unchanged, authority store unchanged)
// and the new key's account is admitted as a seat: its approve and its
// propose commit, and it is still refused a non-council message
// (ERROR_CODE_FORBIDDEN). Neither address gets an admission row or a role.
// The rotation hook gives the new address an auth account, as a rotation
// message would have to; no message rotates a seat today.
func TestSeatRotationMovesAnteAdmission(t *testing.T) {
	c := newCouncil(t, nil)
	s1, s2 := c.seat(0), c.seat(1)
	oldAddr := s1.PubKey().Address().Bytes()
	c.ok(t, s2, change{id: "rot-1", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, s2))

	newKey := sdked.GenPrivKey()
	newAddr := newKey.PubKey().Address().Bytes()
	number, err := c.run.app.RotateCouncilSeatForTest("seat-1", newKey.PubKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	c.run.deliver(t, nil)
	c.numbers[hexAddr(newKey)] = number

	if row, role, admission := c.run.app.SeatViewForTest(oldAddr); row || role != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED || admission != authority.NotAdmitted {
		t.Fatalf("retired key: admission row %v, role %v, admission %v", row, role, admission)
	}
	if row, role, admission := c.run.app.SeatViewForTest(newAddr); row || role != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED || admission != authority.AdmittedSeat {
		t.Fatalf("new key: admission row %v, role %v, admission %v", row, role, admission)
	}
	c.reject(t, rejectRow{name: "approve from the retired seat key", key: s1, msg: approveMsg(s1, "ap-old", "rot-1"),
		space: "svn", code: 1, proto: "signer is not an admitted application account"})
	c.reject(t, rejectRow{name: "propose from the retired seat key", key: s1,
		msg:   change{id: "rot-old", base: 1, node: "v6", pub: bytes32(10), power: 1, height: 1000}.msg(t, s1),
		space: "svn", code: 1, proto: "signer is not an admitted application account"})
	c.reject(t, rejectRow{name: "execute from the new seat key", key: newKey, msg: executeMsg(newKey, "ex-new", "rot-1"),
		space: "svn", code: 2, proto: seatOnlyLog})
	c.reject(t, rejectRow{name: "authority_end from the new seat key", key: newKey,
		msg:   &leasesv1.MsgEndLease{Signer: newAddr, BusinessId: "en-new", LeaseId: "l1", AuthorityEnd: true},
		space: "svn", code: 2, proto: seatOnlyLog})
	c.ok(t, newKey, approveMsg(newKey, "ap-new", "rot-1"))
	c.ok(t, newKey, change{id: "rot-2", base: 1, node: "v6", pub: bytes32(10), power: 1, height: 1000}.msg(t, newKey))
	if state, found, err := c.run.app.ProposalStateForTest("rot-2"); err != nil || !found || state != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
		t.Fatalf("rot-2: state %v found %v err %v", state, found, err)
	}
}
