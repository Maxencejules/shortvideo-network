package policy

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// publication is the fixture of the publication tests: the pinned service
// address, two principals with registered intent keys, and a caller.
type publication struct {
	service      []byte
	alice, bob   innerKey
	unregistered innerKey
	registry     *Registry
	caller       Caller
}

func newPublication(t *testing.T) publication {
	t.Helper()
	p := publication{
		service: addr("service"), alice: newInnerKey(t, "alice-intent"), bob: newInnerKey(t, "bob-intent"),
		unregistered: newInnerKey(t, "stray-intent"),
		caller:       Caller{Principal: "api", Kinds: []Kind{KindRegisterContent, KindWithdrawContent, KindOfferLease}},
	}
	reg, err := ParseRegistry([]byte(registryJSON(
		[3]string{"alice", hex.EncodeToString(p.alice.pub), "fixture"},
		[3]string{"bob", hex.EncodeToString(p.bob.pub), "fixture"})))
	if err != nil {
		t.Fatal(err)
	}
	p.registry = reg
	return p
}

func authFor(principal string, perms ...Permission) AuthContext {
	return AuthContext{PrincipalKind: PrincipalKindDevFixture, PrincipalID: principal, Permissions: perms, PolicyRef: "policy-1"}
}

// register returns a MsgRegisterContent from the service address whose
// intent k signed for host.
func (p publication) register(t *testing.T, k innerKey, host []byte, contentID string) *contentv1.MsgRegisterContent {
	t.Helper()
	m := &contentv1.MsgRegisterContent{Signer: p.service, BusinessId: "bcast-r", ContentId: contentID, ContentVersion: 1,
		ManifestDigest: digest("manifest:" + contentID), PackageBytes: 4096}
	sb, err := content.RegisterIntentSignBytes(host, contentID, 1, m.GetManifestDigest().GetValue(), 4096, content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	m.HostingIntent = k.innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, sb)
	return m
}

func (p publication) withdraw(t *testing.T, k innerKey, host []byte, contentID string) *contentv1.MsgWithdrawContent {
	t.Helper()
	m := &contentv1.MsgWithdrawContent{Signer: p.service, BusinessId: "bcast-w", ContentId: contentID, ContentVersion: 1,
		ReasonDigest: digest("reason")}
	sb, err := content.WithdrawIntentSignBytes(host, contentID, 1, m.GetReasonDigest().GetValue(), content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	m.HostingIntent = k.innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, sb)
	return m
}

func (p publication) owner(principal string, status OwnerStatus) *ContentOwner {
	return &ContentOwner{ContentID: "c1", ContentVersion: 1, CallerPrincipal: "api", PrincipalID: principal,
		SignerAddress: p.service, Status: status}
}

func (p publication) hosted(host []byte) *contentv1.QueryContentResponse {
	return &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "c1", ContentVersion: 1, SubmittingHost: host}}
}

// TestPublicationPermission pins plan 7.4 and 5.7 (owner answer Q3): a
// register or withdraw passes only with the kind, the permission, the
// principal's registered intent key, an intent signed for the pinned
// service address, and, for a withdraw, the EFFECTIVE ownership of this
// caller and principal with the chain showing our service as host.
func TestPublicationPermission(t *testing.T) {
	p := newPublication(t)
	reg := func(m *contentv1.MsgRegisterContent, auth AuthContext) PublicationInput {
		return PublicationInput{Caller: p.caller, Auth: auth, Msg: m, Service: p.service, Registry: p.registry}
	}
	if err := CheckPublication(reg(p.register(t, p.alice, p.service, "c1"), authFor("alice", PermPublishContent))); err != nil {
		t.Fatalf("valid register: %v", err)
	}
	in := PublicationInput{Caller: p.caller, Auth: authFor("alice", PermWithdrawContent), Msg: p.withdraw(t, p.alice, p.service, "c1"),
		Service: p.service, Registry: p.registry, Owner: p.owner("alice", OwnerEffective), OnChain: p.hosted(p.service)}
	if err := CheckPublication(in); err != nil {
		t.Fatalf("valid withdraw: %v", err)
	}

	cases := map[string]struct {
		in   PublicationInput
		want Code
	}{
		"missing permission":                   {reg(p.register(t, p.alice, p.service, "c1"), authFor("alice", PermScheduleHosting)), CodePermission},
		"withdraw permission is not publish":   {reg(p.register(t, p.alice, p.service, "c1"), authFor("alice", PermWithdrawContent)), CodePermission},
		"unregistered intent key":              {reg(p.register(t, p.unregistered, p.service, "c1"), authFor("alice", PermPublishContent)), CodeIntentKeyUnregistered},
		"another principal's key":              {reg(p.register(t, p.bob, p.service, "c1"), authFor("alice", PermPublishContent)), CodeIntentKeyUnregistered},
		"principal without a registered key":   {reg(p.register(t, p.alice, p.service, "c1"), authFor("carol", PermPublishContent)), CodeIntentKeyUnregistered},
		"submittingHost not the pinned signer": {reg(p.register(t, p.alice, addr("service-2"), "c1"), authFor("alice", PermPublishContent)), CodeIntentSignature},
		"kind not allowed for the caller": {PublicationInput{Caller: Caller{Principal: "api", Kinds: []Kind{KindOfferLease}},
			Auth: authFor("alice", PermPublishContent), Msg: p.register(t, p.alice, p.service, "c1"), Service: p.service, Registry: p.registry}, CodeKindNotAllowed},
		"invalid auth context": {reg(p.register(t, p.alice, p.service, "c1"), AuthContext{PrincipalKind: "EMAIL", PrincipalID: "alice",
			Permissions: []Permission{PermPublishContent}, PolicyRef: "p"}), CodeInvalid},
		"message signer not the service": {PublicationInput{Caller: p.caller, Auth: authFor("alice", PermPublishContent),
			Msg: p.register(t, p.alice, p.service, "c1"), Service: addr("service-2"), Registry: p.registry}, CodeSignerMismatch},
		"no registry": {PublicationInput{Caller: p.caller, Auth: authFor("alice", PermPublishContent),
			Msg: p.register(t, p.alice, p.service, "c1"), Service: p.service}, CodeIntentKeyUnregistered},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { wantCode(t, CheckPublication(tc.in), tc.want) })
	}

	t.Run("register of an unavailable content id", func(t *testing.T) {
		for name, edit := range map[string]func(*PublicationInput){
			"pending owner row":   func(in *PublicationInput) { in.Owner = p.owner("bob", OwnerPending) },
			"effective owner row": func(in *PublicationInput) { in.Owner = p.owner("alice", OwnerEffective) },
			"on-chain record":     func(in *PublicationInput) { in.OnChain = p.hosted(addr("service-2")) },
			"on-chain tombstone": func(in *PublicationInput) {
				in.OnChain = &contentv1.QueryContentResponse{Tombstone: &contentv1.Tombstone{ContentId: "c1", ContentVersion: 1}}
			},
		} {
			in := reg(p.register(t, p.alice, p.service, "c1"), authFor("alice", PermPublishContent))
			edit(&in)
			if CodeOf(CheckPublication(in)) != CodeContentIDTaken {
				t.Errorf("%s: %v", name, CheckPublication(in))
			}
		}
		in := reg(p.register(t, p.alice, p.service, "c1"), authFor("alice", PermPublishContent))
		in.Owner = p.owner("bob", OwnerVoid)
		if err := CheckPublication(in); err != nil {
			t.Fatalf("a VOID row does not hold the id: %v", err)
		}
	})

	t.Run("withdraw of content the caller does not control", func(t *testing.T) {
		base := func() PublicationInput {
			return PublicationInput{Caller: p.caller, Auth: authFor("alice", PermWithdrawContent), Msg: p.withdraw(t, p.alice, p.service, "c1"),
				Service: p.service, Registry: p.registry, Owner: p.owner("alice", OwnerEffective), OnChain: p.hosted(p.service)}
		}
		for name, edit := range map[string]func(*PublicationInput){
			"another principal's content": func(in *PublicationInput) { in.Owner = p.owner("bob", OwnerEffective) },
			"another caller's content":    func(in *PublicationInput) { in.Owner.CallerPrincipal = "other-api" },
			"pending owner":               func(in *PublicationInput) { in.Owner = p.owner("alice", OwnerPending) },
			"void owner":                  func(in *PublicationInput) { in.Owner = p.owner("alice", OwnerVoid) },
			"no owner row":                func(in *PublicationInput) { in.Owner = nil },
			"owner row of another signer": func(in *PublicationInput) { in.Owner.SignerAddress = addr("service-2") },
			"chain host is another":       func(in *PublicationInput) { in.OnChain = p.hosted(addr("service-2")) },
			"chain shows no record":       func(in *PublicationInput) { in.OnChain = nil },
			"content withdrawn": func(in *PublicationInput) {
				in.OnChain = &contentv1.QueryContentResponse{Tombstone: &contentv1.Tombstone{ContentId: "c1", ContentVersion: 1}}
			},
		} {
			in := base()
			edit(&in)
			if CodeOf(CheckPublication(in)) != CodeNotVisible {
				t.Errorf("%s: %v", name, CheckPublication(in))
			}
		}
		in := base()
		in.Msg = p.withdraw(t, p.bob, p.service, "c1")
		wantCode(t, CheckPublication(in), CodeIntentKeyUnregistered)
	})
	t.Run("not a content message", func(t *testing.T) {
		wantCode(t, CheckPublication(PublicationInput{Msg: &leasesv1.MsgOfferLease{}}), CodeInvalid)
	})
}

// TestSignerSideIntentCheck pins the signer's half of plan 7.2: the intent
// must verify for the signer's own address and its key must be in the
// registry (any principal's).
func TestSignerSideIntentCheck(t *testing.T) {
	p := newPublication(t)
	if err := CheckIntentRegistered(p.register(t, p.bob, p.service, "c1"), p.service, p.registry); err != nil {
		t.Fatalf("registered intent: %v", err)
	}
	if err := CheckIntentRegistered(p.withdraw(t, p.alice, p.service, "c1"), p.service, p.registry); err != nil {
		t.Fatalf("registered withdraw intent: %v", err)
	}
	wantCode(t, CheckIntentRegistered(p.register(t, p.unregistered, p.service, "c1"), p.service, p.registry), CodeIntentKeyUnregistered)
	wantCode(t, CheckIntentRegistered(p.register(t, p.alice, addr("other-host"), "c1"), p.service, p.registry), CodeIntentSignature)
	wantCode(t, CheckIntentRegistered(p.register(t, p.alice, p.service, "c1"), p.service, nil), CodeIntentKeyUnregistered)
	m := p.register(t, p.alice, p.service, "c1")
	m.PackageBytes++
	wantCode(t, CheckIntentRegistered(m, p.service, p.registry), CodeIntentSignature)
	m = p.register(t, p.alice, p.service, "c1")
	m.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT
	wantCode(t, CheckIntentRegistered(m, p.service, p.registry), CodeIntentSignature)
	m = p.register(t, p.alice, p.service, "c1")
	m.HostingIntent = nil
	wantCode(t, CheckIntentRegistered(m, p.service, p.registry), CodeIntentSignature)
	m = p.register(t, p.alice, p.service, "c1")
	m.ManifestDigest = nil
	wantCode(t, CheckIntentRegistered(m, p.service, p.registry), CodeInvalid)
	wantCode(t, CheckIntentRegistered(&leasesv1.MsgOfferLease{}, p.service, p.registry), CodeInvalid)
	wantCode(t, CheckIntentRegistered(p.register(t, p.alice, p.service, "c1"), p.service[:19], p.registry), CodeInvalid)
}

func TestOfferAndOwnership(t *testing.T) {
	p := newPublication(t)
	offer := &leasesv1.MsgOfferLease{Signer: p.service, ContentId: "c1", ContentVersion: 1}
	base := func() OfferInput {
		return OfferInput{Caller: p.caller, Auth: authFor("alice", PermScheduleHosting), Msg: offer, Service: p.service,
			Owner: p.owner("alice", OwnerEffective), OnChain: p.hosted(p.service)}
	}
	if err := CheckOffer(base()); err != nil {
		t.Fatalf("valid offer: %v", err)
	}
	for name, tc := range map[string]struct {
		edit func(*OfferInput)
		want Code
	}{
		"pending owner row allows no offer": {func(in *OfferInput) { in.Owner = p.owner("alice", OwnerPending) }, CodeNotVisible},
		"another caller's content":          {func(in *OfferInput) { in.Owner.CallerPrincipal = "other-api" }, CodeNotVisible},
		"withdrawn content": {func(in *OfferInput) {
			in.OnChain = &contentv1.QueryContentResponse{Tombstone: &contentv1.Tombstone{ContentId: "c1", ContentVersion: 1}}
		}, CodeNotVisible},
		"chain host is another": {func(in *OfferInput) { in.OnChain = p.hosted(addr("service-2")) }, CodeNotVisible},
		"missing permission":    {func(in *OfferInput) { in.Auth = authFor("alice", PermPublishContent) }, CodePermission},
		"kind not allowed":      {func(in *OfferInput) { in.Caller.Kinds = []Kind{KindRegisterContent} }, CodeKindNotAllowed},
		"offer by another signer": {func(in *OfferInput) {
			in.Msg = &leasesv1.MsgOfferLease{Signer: addr("x"), ContentId: "c1", ContentVersion: 1}
		}, CodeSignerMismatch},
		"not an offer": {func(in *OfferInput) { in.Msg = &contentv1.MsgWithdrawContent{Signer: p.service} }, CodeInvalid},
	} {
		in := base()
		tc.edit(&in)
		if CodeOf(CheckOffer(in)) != tc.want {
			t.Errorf("%s: %v, want %s", name, CheckOffer(in), tc.want)
		}
	}

	pending := *p.owner("alice", OwnerPending)
	if !OwnerBecomesEffective(pending, p.service, p.hosted(p.service)) {
		t.Fatal("a committed registration by our service did not make the row effective")
	}
	for name, ok := range map[string]bool{
		"committed by another signer": OwnerBecomesEffective(pending, addr("service-2"), p.hosted(p.service)),
		"chain host is another":       OwnerBecomesEffective(pending, p.service, p.hosted(addr("service-2"))),
		"no record on chain":          OwnerBecomesEffective(pending, p.service, nil),
		"row already void":            OwnerBecomesEffective(*p.owner("alice", OwnerVoid), p.service, p.hosted(p.service)),
		"record of another version": OwnerBecomesEffective(pending, p.service,
			&contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "c1", ContentVersion: 2, SubmittingHost: p.service}}),
	} {
		if ok {
			t.Errorf("%s: became effective", name)
		}
	}
}

func TestRelayAuthority(t *testing.T) {
	ctx := context.Background()
	found := RelayResult{ProviderID: "provider-1", OfferID: "offer-1"}
	auth := authFor("agent", PermRelayProviderSigned)
	bound := RelayAuthority{Caller: Caller{Principal: "agent", Kinds: []Kind{KindRelayAcceptLease}, BoundProviderID: "provider-1"}}
	if err := bound.CheckRelay(ctx, KindRelayAcceptLease, auth, found); err != nil {
		t.Fatalf("bound provider: %v", err)
	}
	other := bound
	other.Caller.BoundProviderID = "provider-2"
	wantCode(t, other.CheckRelay(ctx, KindRelayAcceptLease, auth, found), CodeRelayNotAllowed)
	owner := RelayAuthority{Caller: Caller{Principal: "api", Kinds: []Kind{KindRelayAcceptLease}},
		OwnsCommittedOffer: func(_ context.Context, offerID string) (bool, error) { return offerID == "offer-1", nil }}
	if err := owner.CheckRelay(ctx, KindRelayAcceptLease, auth, found); err != nil {
		t.Fatalf("offer owner: %v", err)
	}
	wantCode(t, owner.CheckRelay(ctx, KindRelayAcceptLease, auth, RelayResult{ProviderID: "provider-1", OfferID: "offer-9"}), CodeRelayNotAllowed)
	wantCode(t, owner.CheckRelay(ctx, KindRelayRenewLease, auth, found), CodeKindNotAllowed)
	wantCode(t, owner.CheckRelay(ctx, KindRelayAcceptLease, authFor("api", PermScheduleHosting), found), CodePermission)
	broken := owner
	broken.OwnsCommittedOffer = func(context.Context, string) (bool, error) { return false, errors.New("store down") }
	if err := broken.CheckRelay(ctx, KindRelayAcceptLease, auth, found); err == nil || CodeOf(err) != "" {
		t.Fatalf("store failure = %v, want a plain error", err)
	}
	none := RelayAuthority{Caller: Caller{Principal: "api", Kinds: []Kind{KindRelayAcceptLease}}}
	wantCode(t, none.CheckRelay(ctx, KindRelayAcceptLease, auth, found), CodeRelayNotAllowed)
}

func TestAuthContextValidate(t *testing.T) {
	good := authFor("alice", PermPublishContent, PermWithdrawContent)
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	canon := AuthContext{PrincipalKind: PrincipalKindDevFixture, PrincipalID: "a", PolicyRef: "p",
		Permissions: []Permission{PermWithdrawContent, PermPublishContent}}.Canonical()
	if canon.Permissions[0] != PermPublishContent || canon.Permissions[1] != PermWithdrawContent {
		t.Fatalf("canonical order = %v", canon.Permissions)
	}
	for name, a := range map[string]AuthContext{
		"other principal kind": {PrincipalKind: "USER_EMAIL", PrincipalID: "a", PolicyRef: "p", Permissions: []Permission{PermPublishContent}},
		"email as principal":   {PrincipalKind: PrincipalKindDevFixture, PrincipalID: "a@b.example", PolicyRef: "p", Permissions: []Permission{PermPublishContent}},
		"bad policy ref":       {PrincipalKind: PrincipalKindDevFixture, PrincipalID: "a", PolicyRef: "", Permissions: []Permission{PermPublishContent}},
		"no permission":        {PrincipalKind: PrincipalKindDevFixture, PrincipalID: "a", PolicyRef: "p"},
		"unknown permission":   {PrincipalKind: PrincipalKindDevFixture, PrincipalID: "a", PolicyRef: "p", Permissions: []Permission{"ADMIN"}},
		"repeated permission":  {PrincipalKind: PrincipalKindDevFixture, PrincipalID: "a", PolicyRef: "p", Permissions: []Permission{PermPublishContent, PermPublishContent}},
		"too many": {PrincipalKind: PrincipalKindDevFixture, PrincipalID: "a", PolicyRef: "p",
			Permissions: []Permission{PermPublishContent, PermWithdrawContent, PermScheduleHosting, PermRelayProviderSigned, PermPublishContent}},
	} {
		if CodeOf(a.Validate()) != CodeInvalid {
			t.Errorf("%s: %v", name, a.Validate())
		}
	}
}
