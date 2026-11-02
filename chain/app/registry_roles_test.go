package app_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestRegistryMessagesRejectOtherRoles sends each of the six goal 07
// registry messages from every account that neither holds the handler's role
// nor owns the object: two council seats, a BROADCASTER that is not a seat,
// the AUTHORITY account, an AUDITOR, the service account (provider messages)
// and provider-2 (p1's messages and the content messages). Rotate and drain
// carry a valid inner signature by p1's current key, relayed by the other
// account; the hosting intent is bound to the sending account. Every row is
// ERROR_CODE_FORBIDDEN with the full non-account state byte-identical and no
// typed event. A seat account has no submitter row (DR-06-06): the ante
// refuses the seat rows before the handler, so the seat's sequence does not
// move either. Every other row passes the ante and is refused by the
// registry handler. A council key therefore reaches no registry handler.
func TestRegistryMessagesRejectOtherRoles(t *testing.T) {
	n := newRegNet()
	auditor := newSecp("auditor")
	broadcaster := newSecp("broadcaster")
	n.doc.Submitters = append(n.doc.Submitters,
		genesis.Submitter{AddressHex: auditor.hexAddr(), Role: genesis.RoleAuditor},
		genesis.Submitter{AddressHex: broadcaster.hexAddr(), Role: genesis.RoleBroadcaster},
	)
	n.numbers = accountNumbers(n.doc)
	c := n.start(t, 1)
	registrySetup(t, n, c)
	chainApp := c.app()

	const (
		registerProvider = "register provider"
		updateP1         = "update p1"
		rotateP1         = "rotate p1 (valid p1 signature)"
		drainP1          = "drain p1 (valid p1 signature)"
		registerContent  = "register content"
		withdrawC1       = "withdraw c1"
	)
	all := []string{registerProvider, updateP1, rotateP1, drainP1, registerContent, withdrawC1}
	actors := []struct {
		who  *actor
		msgs []string
	}{
		{n.seats[0], all},
		{n.seats[3], all},
		{broadcaster, all},
		{n.authority, all},
		{auditor, all},
		// The service account owns the content paths.
		{n.service, []string{registerProvider, updateP1, rotateP1, drainP1}},
		// provider-2 holds the provider role but does not own p1.
		{n.provider2, []string{updateP1, rotateP1, drainP1, registerContent, withdrawC1}},
	}
	rows := 0
	for i, a := range actors {
		who := a.who
		bid := func(s string) string { return fmt.Sprintf("role-%d-%s", i, s) }
		build := map[string]func() sdk.Msg{
			registerProvider: func() sdk.Msg {
				return n.registerProvider(t, who, bid("reg"), fmt.Sprintf("pr%d", i), groupDevnet, capacity1)
			},
			updateP1: func() sdk.Msg { return n.updateProvider(t, who, bid("upd"), "p1", capacity2, "r") },
			rotateP1: func() sdk.Msg {
				return n.rotate(t, who, bid("rot"), "p1", 1, newSigningKey(fmt.Sprintf("p1-key-r%d", i)))
			},
			drainP1: func() sdk.Msg {
				return n.drain(t, who, bid("drn"), "p1", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)
			},
			registerContent: func() sdk.Msg {
				return n.registerContent(t, who, bid("rc"), fmt.Sprintf("cr%d", i), 1, packageManifest("cr"), sizeC1)
			},
			withdrawC1: func() sdk.Msg { return n.withdrawContent(t, who, bid("wc"), "c1", 1, reasonDigest("r")) },
		}
		seat := false
		for _, s := range n.seats {
			seat = seat || s == who
		}
		for _, name := range a.msgs {
			rows++
			label := who.name + ": " + name
			before := chainApp.FullSnapshot()
			seqBefore, _ := chainApp.AccountSequenceForTest(who.addr)
			res := n.send(t, c, who, build[name]())
			wantCode(t, label, res, msgkit.Forbidden)
			seqAfter, _ := chainApp.AccountSequenceForTest(who.addr)
			switch {
			case seat && (anteCommitted(res) || seqAfter != seqBefore || !strings.Contains(res.Log, seatOnlyLog)):
				t.Fatalf("%s: not refused by the ante seat rule (sequence %d -> %d, log %s)", label, seqBefore, seqAfter, res.Log)
			case !seat && !anteCommitted(res):
				t.Fatalf("%s: rejected in the ante handler, not by the registry", label)
			}
			unchanged(t, label, chainApp, before, res)
		}
	}
	if rows != 39 {
		t.Fatalf("%d rows, want 39", rows)
	}
}

// TestSeatAddressIsNotASubmitter: a genesis that lists a council seat
// address as a submitter with any role, BROADCASTER included, is refused
// (owner ruling DR-06-06, DR-07-11 option 2). It replaces
// TestSeatAddressIsBroadcasterOnly, which accepted a BROADCASTER row. Both
// rules are checked: genesis.Validate on the document (AppStateFromDocument,
// and InitChain through DocumentFromModules, which runs first), and the
// authority rule ValidateAuthorityGenesis (InitChain after the document
// check, and svnd's genesis validation) on module genesis whose authority
// state alone carries the row. The genesis without seat rows passes both
// rules and boots; on that chain no seat address has an admission row or a
// role, each seat address has an auth account, and the ante admits it as a
// seat.
func TestSeatAddressIsNotASubmitter(t *testing.T) {
	roles := []struct {
		doc   string
		proto typesv1.SubmitterRole
	}{
		{genesis.RoleAuthority, typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY},
		{genesis.RoleAdmittedService, typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
		{genesis.RoleAdmittedProvider, typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER},
		{genesis.RoleBroadcaster, typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER},
		{genesis.RoleAuditor, typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR},
	}
	for _, role := range roles {
		t.Run("document "+role.doc, func(t *testing.T) {
			n := newRegNet()
			n.doc.Submitters = append(n.doc.Submitters, genesis.Submitter{AddressHex: n.seats[0].hexAddr(), Role: role.doc})
			_, err := app.AppStateFromDocument(codecFromApp(t, n.doc.ChainID), n.doc)
			want := "seat seat-1 address is a " + role.doc + " submitter; a seat address may not be a submitter"
			if err == nil || err.Error() != want {
				t.Fatalf("AppStateFromDocument: %v; want %q", err, want)
			}
		})
		t.Run("InitChain "+role.doc, func(t *testing.T) {
			doc, seats, _ := councilDocument()
			seatAddr := seats[0].key.PubKey().Address().Bytes()
			modules := councilModules(t, doc, func(s *authorityv1.GenesisState) {
				s.Authority.Submitters = append(s.Authority.Submitters, &typesv1.Submitter{Address: seatAddr, Role: role.proto})
			})
			_, err := initChain(t, doc, modules, 1)
			want := "seat seat-1 address is a " + role.doc + " submitter; a seat address may not be a submitter"
			if err == nil || err.Error() != want {
				t.Fatalf("InitChain: %v; want %q", err, want)
			}
			err = app.ValidateAuthorityGenesis(modules, 1, doc.GenesisUnixSeconds)
			wantAuth := "authority genesis: " + authority.CodeForbidden + `: seat "seat-1" address is a ` + role.proto.String() + " submitter; a seat address may not be a submitter"
			if err == nil || err.Error() != wantAuth {
				t.Fatalf("ValidateAuthorityGenesis: %v; want %q", err, wantAuth)
			}
		})
	}
	// The same genesis without seat rows passes both rules and boots.
	doc, seats, _ := councilDocument()
	modules := councilModules(t, doc, nil)
	if err := app.ValidateAuthorityGenesis(modules, 1, doc.GenesisUnixSeconds); err != nil {
		t.Fatalf("ValidateAuthorityGenesis without seat rows: %v", err)
	}
	running := bootModules(t, doc, modules)
	running.deliver(t, nil)
	chainApp := running.app
	for _, seat := range seats {
		addr := seat.key.PubKey().Address().Bytes()
		if row, role, admission := chainApp.SeatViewForTest(addr); row || role != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED || admission != authority.AdmittedSeat {
			t.Fatalf("seat %s: admission row %v, role %v, admission %v", seat.addr, row, role, admission)
		}
		if _, known := chainApp.AccountSequenceForTest(addr); !known {
			t.Fatalf("seat %s has no auth account", seat.addr)
		}
	}
}

// TestExportRefusesANonEmptyCatalogue isolates the catalogue clause of
// App.Export. With one provider record, or one content record, and no
// council message, the authority store is unchanged since genesis and its
// own export succeeds, so the refusal comes from DocumentFromModules'
// launch rule (an empty catalogue at genesis, DR-07-02) and names it.
func TestExportRefusesANonEmptyCatalogue(t *testing.T) {
	rows := []struct {
		name string
		send func(t *testing.T, n *regNet, c *cluster)
		want string
	}{
		{"provider only", func(t *testing.T, n *regNet, c *cluster) {
			wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
		}, "export refused: exported genesis does not validate: providers are not empty"},
		{"content only", func(t *testing.T, n *regNet, c *cluster) {
			wantCode(t, "register c1", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)), msgkit.OK)
		}, "export refused: exported genesis does not validate: content is not empty"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			n := newRegNet()
			c := n.start(t, 1)
			if _, err := c.app().Export(); err != nil {
				t.Fatalf("App.Export of the fresh chain: %v", err)
			}
			authBefore := c.app().AuthoritySnapshotForTest()
			row.send(t, n, c)
			if !bytes.Equal(authBefore, c.app().AuthoritySnapshotForTest()) {
				t.Fatal("a registry message changed the authority store")
			}
			if _, err := c.app().ExportModulesForTest(authority.ModuleName); err != nil {
				t.Fatalf("authority export alone: %v", err)
			}
			_, err := c.app().Export()
			if err == nil || err.Error() != row.want {
				t.Fatalf("App.Export: %v; want %q", err, row.want)
			}
		})
	}
}
