package policy

import (
	"context"
	"crypto/ed25519"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// routedMessages returns one message of each routed type with signer.
func routedMessages(signer []byte) map[string]sdk.Msg {
	return map[string]sdk.Msg{
		TypeRegisterContent: &contentv1.MsgRegisterContent{Signer: signer},
		TypeWithdrawContent: &contentv1.MsgWithdrawContent{Signer: signer},
		TypeOfferLease:      &leasesv1.MsgOfferLease{Signer: signer},
		TypeAcceptLease:     &leasesv1.MsgAcceptLease{Signer: signer},
		TypeRenewLease:      &leasesv1.MsgRenewLease{Signer: signer},
		TypeEndLease:        &leasesv1.MsgEndLease{Signer: signer},
	}
}

// TestKindSignerAllowlist pins plan 7.1: the service key signs register,
// withdraw and offer; the broadcaster key signs accept, renew and the
// provider end; the forward kind uses no broadcaster key; nothing else is
// signable, and each class refuses the other class's messages.
func TestKindSignerAllowlist(t *testing.T) {
	want := map[Kind]struct {
		class   Class
		typeURL string
		perm    Permission
	}{
		KindRegisterContent:   {ClassService, "/svn.content.v1.MsgRegisterContent", PermPublishContent},
		KindWithdrawContent:   {ClassService, "/svn.content.v1.MsgWithdrawContent", PermWithdrawContent},
		KindOfferLease:        {ClassService, "/svn.leases.v1.MsgOfferLease", PermScheduleHosting},
		KindRelayAcceptLease:  {ClassBroadcaster, "/svn.leases.v1.MsgAcceptLease", PermRelayProviderSigned},
		KindRelayRenewLease:   {ClassBroadcaster, "/svn.leases.v1.MsgRenewLease", PermRelayProviderSigned},
		KindRelayEndLease:     {ClassBroadcaster, "/svn.leases.v1.MsgEndLease", PermRelayProviderSigned},
		KindForwardProviderTx: {"", "", PermRelayProviderSigned},
	}
	if len(Kinds()) != len(want) {
		t.Fatalf("kinds = %v", Kinds())
	}
	for _, k := range Kinds() {
		spec, err := SpecOf(k)
		if err != nil {
			t.Fatal(err)
		}
		w := want[k]
		if spec.Class != w.class || spec.TypeURL != w.typeURL || spec.Permission != w.perm {
			t.Errorf("%s: spec %+v, want %+v", k, spec, w)
		}
	}
	for _, unknown := range []Kind{"", "FORWARD_AUDIT_BATCH", "SUBMIT_AUDIT_BATCH", "AUTHORITY_END_LEASE", "register_content"} {
		_, err := SpecOf(unknown)
		wantCode(t, err, CodeUnknownKind)
	}

	service := map[string]bool{TypeRegisterContent: true, TypeWithdrawContent: true, TypeOfferLease: true}
	relay := map[string]bool{TypeAcceptLease: true, TypeRenewLease: true, TypeEndLease: true}
	for class, allowed := range map[Class]map[string]bool{ClassService: service, ClassBroadcaster: relay, "": {}, "AUDITOR": {}} {
		got := AllowedTypeURLs(class)
		if len(got) != len(allowed) {
			t.Fatalf("class %q allowlist = %v", class, got)
		}
		signer := addr("key")
		for typeURL, msg := range routedMessages(signer) {
			err := CheckSignable(class, signer, msg)
			if allowed[typeURL] != (err == nil) || Allowed(class, typeURL) != allowed[typeURL] {
				t.Errorf("class %q, %s: CheckSignable = %v", class, typeURL, err)
			}
			if !allowed[typeURL] {
				wantCode(t, err, CodeTypeNotAllowed)
			}
		}
	}
	if got := RelayTypeURLs(); len(got) != 3 || !relay[got[0]] || !relay[got[1]] || !relay[got[2]] {
		t.Fatalf("relay type URLs = %v", got)
	}

	t.Run("signer must be the key address", func(t *testing.T) {
		key := addr("key")
		for _, class := range []Class{ClassService, ClassBroadcaster} {
			for _, typeURL := range AllowedTypeURLs(class) {
				for name, signer := range map[string][]byte{"other": addr("other"), "empty": nil, "short": key[:19]} {
					err := CheckSignable(class, key, routedMessages(signer)[typeURL])
					if CodeOf(err) != CodeSignerMismatch {
						t.Errorf("%s with %s signer: %v", typeURL, name, err)
					}
				}
			}
		}
		_, err := SpecOf(KindOfferLease)
		if err != nil {
			t.Fatal(err)
		}
		wantCode(t, CheckSignable(ClassService, nil, &leasesv1.MsgOfferLease{}), CodeSignerMismatch)
		wantCode(t, CheckSignable(ClassService, key, nil), CodeInvalid)
	})
	t.Run("authority end is never signable", func(t *testing.T) {
		key := addr("key")
		wantCode(t, CheckSignable(ClassBroadcaster, key, &leasesv1.MsgEndLease{Signer: key, AuthorityEnd: true}), CodeAuthorityEnd)
	})
}

// TestStartupRefusesRole pins the start-up refusals of plan 7.1: a key
// whose address is AUTHORITY, AUDITOR, ADMITTED_PROVIDER, a council seat,
// has no role, or has the other class's role; two keys with one address,
// name or class; an unknown class.
func TestStartupRefusesRole(t *testing.T) {
	ctx := context.Background()
	f := newFakeChain(t)
	seatPub := make([]byte, ed25519.PublicKeySize)
	seatPub[0] = 9
	seatAddr := genesis.SeatAddress(seatPub)
	roles := map[string]typesv1.SubmitterRole{
		"authority":   typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY,
		"service":     typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE,
		"provider-1":  typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
		"broadcaster": typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER,
		"auditor-1":   typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
	}
	for name, role := range roles {
		f.submitters = append(f.submitters, &typesv1.Submitter{Address: addr(name), Role: role})
	}
	f.seats = []*authorityv1.DevnetTestSeat{{SeatId: "seat-1", PublicKey: seatPub}}
	// Genesis forbids a seat address with a submitter row (DR-06-06); the
	// fake lists one anyway, so only the seat check can refuse that key.
	f.submitters = append(f.submitters, &typesv1.Submitter{Address: seatAddr, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE})
	f.pageSize = 2 // four pages: the walk must follow page keys
	view, err := ReadGovernance(ctx, f.client(t), f.height)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls["/svn.authority.v1.Query/Governance"] != 4 {
		t.Fatalf("governance pages read = %d, want 4", f.calls["/svn.authority.v1.Query/Governance"])
	}
	if id, ok := view.SeatOf(seatAddr); !ok || id != "seat-1" {
		t.Fatalf("seat not seen: %q %v", id, ok)
	}

	good := []KeyIdentity{{"service", ClassService, addr("service")}, {"broadcaster", ClassBroadcaster, addr("broadcaster")}}
	if err := CheckKeyRoles(good, view); err != nil {
		t.Fatalf("service and broadcaster keys: %v", err)
	}
	if err := CheckKeyRoles(good[:1], view); err != nil {
		t.Fatalf("service key alone: %v", err)
	}
	refused := map[string][]KeyIdentity{
		"authority key":          {{"service", ClassService, addr("authority")}},
		"auditor key":            {{"broadcaster", ClassBroadcaster, addr("auditor-1")}},
		"provider key":           {{"broadcaster", ClassBroadcaster, addr("provider-1")}},
		"seat key":               {{"service", ClassService, seatAddr}},
		"no role":                {{"service", ClassService, addr("nobody")}},
		"broadcaster as service": {{"service", ClassService, addr("broadcaster")}},
		"service as broadcaster": {{"broadcaster", ClassBroadcaster, addr("service")}},
		"duplicate address":      {good[0], {"service-again", ClassBroadcaster, addr("service")}},
		"duplicate class":        {good[0], {"service-2", ClassService, addr("service")}},
		"duplicate name":         {good[0], {"service", ClassBroadcaster, addr("broadcaster")}},
		"unknown class":          {{"intent", "INTENT", addr("service")}},
		"empty name":             {{"", ClassService, addr("service")}},
		"short address":          {{"service", ClassService, addr("service")[:19]}},
		"no keys":                nil,
	}
	for name, ks := range refused {
		t.Run(name, func(t *testing.T) {
			wantCode(t, CheckKeyRoles(ks, view), CodeRole)
		})
	}

	t.Run("governance walk bounds", func(t *testing.T) {
		f.pageSize = 100
		f.failPath = "/svn.authority.v1.Query/Governance"
		if _, err := ReadGovernance(ctx, f.client(t), f.height); err == nil {
			t.Fatal("a failed governance read was accepted")
		}
		f.failPath = ""
		if _, err := ReadGovernance(ctx, f.client(t), f.height+1); err == nil {
			t.Fatal("a stale governance read was accepted")
		}
		f.seats = append(f.seats, &authorityv1.DevnetTestSeat{SeatId: "seat-bad", PublicKey: []byte{1}})
		if _, err := ReadGovernance(ctx, f.client(t), f.height); err == nil {
			t.Fatal("a seat without a 32-byte key was accepted")
		}
		f.seats = f.seats[:1]
		f.submitters = append(f.submitters, &typesv1.Submitter{Address: addr("service"), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY})
		if _, err := ReadGovernance(ctx, f.client(t), f.height); err == nil {
			t.Fatal("a submitter listed twice was accepted")
		}
	})
}
