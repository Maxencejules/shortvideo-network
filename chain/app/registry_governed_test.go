package app_test

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func governedPayload(key string, value uint64) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
		GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value},
	}}
}

// providersPage runs one Providers query with limit and returns the result
// code and the page.
func providersPage(t *testing.T, c *cluster, limit uint32) (uint32, *providersv1.QueryProvidersResponse) {
	t.Helper()
	raw, res := query(t, c.app(), "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{
		Page: &typesv1.PageRequest{Limit: limit},
	}, 0)
	out := &providersv1.QueryProvidersResponse{}
	if res.Code == 0 {
		if err := proto.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
	}
	return res.Code, out
}

// TestGovernedLimitsReachTheRegistries runs the real wiring end to end: a
// council GovernedLimitChange executed through ExecuteChange sets
// max_package_bytes and max_page_limit in x/authority, and the registries
// read them through the same keeper interface the app built. Each limit is
// checked at the value (accepted) and one above (BOUNDS), and the value
// above was accepted before the change, so the change is what decides.
func TestGovernedLimitsReachTheRegistries(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	for i, who := range []*actor{n.provider1, n.provider2, n.provider1} {
		id := fmt.Sprintf("p%d", i+1)
		wantCode(t, "register "+id, n.send(t, c, who, n.registerProvider(t, who, "reg-"+id, id, groupDevnet, capacity1)), msgkit.OK)
	}
	const packageLimit = uint64(1000)
	const pageLimit = uint32(2)

	// Before any governed value: the protocol caps apply.
	wantCode(t, "package bytes limit+1 under the cap", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-g0", "g0", 1, packageManifest("g0"), packageLimit+1)), msgkit.OK)
	if code, page := providersPage(t, c, pageLimit+1); code != 0 || len(page.GetProviders()) != 3 {
		t.Fatalf("page limit %d before the change: code %d, %d providers", pageLimit+1, code, len(page.GetProviders()))
	}

	n.council(t, c, "prop-lim-pkg", msgkit.LimitMaxPackageBytes, governedPayload(msgkit.LimitMaxPackageBytes, packageLimit))
	n.council(t, c, "prop-lim-page", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, uint64(pageLimit)))

	wantCode(t, "package bytes at the governed limit", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-g1", "g1", 1, packageManifest("g1"), packageLimit)), msgkit.OK)
	before := c.app().FullSnapshot()
	res := n.send(t, c, n.service, n.registerContent(t, n.service, "reg-g2", "g2", 1, packageManifest("g2"), packageLimit+1))
	wantCode(t, "package bytes above the governed limit", res, msgkit.Bounds)
	unchanged(t, "package bytes above the governed limit", c.app(), before, res)

	code, page := providersPage(t, c, pageLimit)
	if code != 0 || len(page.GetProviders()) != int(pageLimit) || string(page.GetPage().GetNextPageKey()) != "p3" {
		t.Fatalf("page limit %d: code %d, %d providers, next %q", pageLimit, code, len(page.GetProviders()), page.GetPage().GetNextPageKey())
	}
	if code, page := providersPage(t, c, 0); code != 0 || len(page.GetProviders()) != int(pageLimit) {
		t.Fatalf("page limit 0 uses the governed cap: code %d, %d providers", code, len(page.GetProviders()))
	}
	assertQueryCode(t, c.app(), "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{Limit: pageLimit + 1}}, msgkit.Bounds)
}
