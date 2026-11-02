package providers

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestProviderRecordFields is REQ-MODU-002: the record carries the
// provider id, the account, the signing key reference (public key, key id,
// generation, retired keys), the endpoint manifest digest, the declared
// capacity and reserved bytes (capabilities), the state, and the operator
// group, and Query/Provider returns exactly that record.
func TestProviderRecordFields(t *testing.T) {
	e := newEnv(t)
	register := regMsg("p1")
	register.DeclaredCapacityBytes = 5_000_000
	e.mustOK(e.register(e.fresh(), register), "register")
	if code := e.k.AdmitByCouncil(e.fresh(), "p1", true); code != msgkit.OK {
		t.Fatal(code)
	}
	e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate")
	update := updMsg("p1", "upd-p1", 6_000_000)
	e.mustOK(e.update(e.fresh(), update), "update")

	res, err := e.k.QueryProvider(e.fresh(), &providersv1.QueryProviderRequest{ProviderId: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	want := &providersv1.Provider{
		ProviderId:             "p1",
		Account:                providerAcct,
		SigningPubkey:          testPub("key-p1b"),
		SigningKeyId:           "key-p1b",
		KeyGeneration:          2,
		EndpointManifestDigest: update.GetEndpointManifestDigest(),
		State:                  providersv1.ProviderState_PROVIDER_STATE_ADMITTED,
		AdmittedCapacityBytes:  6_000_000,
		ReservedBytes:          0,
		OperatorGroupId:        DevnetOperatorGroup,
		PreviousSigningKeys: []*providersv1.PreviousSigningKey{
			{PublicKey: testPub("key-p1"), Generation: 1, RetiredUnixSeconds: e.ctx.BlockTime().Unix()},
		},
	}
	if !proto.Equal(res.GetProvider(), want) {
		t.Fatalf("Query/Provider %v, want %v", res.GetProvider(), want)
	}
	// Every field of the state message is asserted above; a new proto
	// field would need a decision about what it records.
	if n := want.ProtoReflect().Descriptor().Fields().Len(); n != 11 {
		t.Fatalf("Provider has %d fields; update REQ-MODU-002 coverage", n)
	}
}

func TestQueryProviderErrors(t *testing.T) {
	e := newEnv(t)
	e.registered("p1")
	cases := []struct {
		name string
		req  *providersv1.QueryProviderRequest
		want msgkit.Code
	}{
		{name: "nil-request", req: nil, want: msgkit.Malformed},
		{name: "empty-id", req: &providersv1.QueryProviderRequest{}, want: msgkit.Malformed},
		{name: "id-65", req: &providersv1.QueryProviderRequest{ProviderId: "a" + strings.Repeat("b", 64)}, want: msgkit.Bounds},
		{name: "id-pattern", req: &providersv1.QueryProviderRequest{ProviderId: "P1"}, want: msgkit.Bounds},
		{name: "absent", req: &providersv1.QueryProviderRequest{ProviderId: "p2"}, want: msgkit.NotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.k.QueryProvider(e.fresh(), c.req)
			if got := codeOf(t, err); got != c.want {
				t.Fatalf("code %s, want %s", got, c.want)
			}
		})
	}
}

// providerIDs returns n ids in byte order: q000, q001, ...
func providerIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("q%03d", i)
	}
	return ids
}

// stateFor cycles through the four provider states.
func stateFor(i int) providersv1.ProviderState { return providersv1.ProviderState(1 + i%4) }

func seedN(e *env, n int) []string {
	ids := providerIDs(n)
	records := make([]*providersv1.Provider, 0, n)
	// Reverse order: InitGenesis must sort.
	for i := n - 1; i >= 0; i-- {
		records = append(records, seededProvider(ids[i], stateFor(i), 10, 0))
	}
	e.seed(records...)
	return ids
}

func (e *env) page(limit uint32, key []byte) ([]string, []byte, msgkit.Code) {
	e.t.Helper()
	res, err := e.k.QueryProviders(e.fresh(), &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{Limit: limit, PageKey: key}})
	if code := codeOf(e.t, err); code != msgkit.OK {
		return nil, nil, code
	}
	ids := make([]string, 0, len(res.GetProviders()))
	for _, p := range res.GetProviders() {
		ids = append(ids, p.GetProviderId())
	}
	if res.GetPage() == nil {
		e.t.Fatal("Providers response without a page")
	}
	return ids, res.GetPage().GetNextPageKey(), msgkit.OK
}

// walk pages through every provider with limit and returns the ids.
func (e *env) walk(limit uint32) []string {
	e.t.Helper()
	var all []string
	var key []byte
	for steps := 0; ; steps++ {
		if steps > 1000 {
			e.t.Fatal("pagination does not terminate")
		}
		ids, next, code := e.page(limit, key)
		if code != msgkit.OK {
			e.t.Fatalf("page: %s", code)
		}
		if uint32(len(ids)) > limit {
			e.t.Fatalf("page of %d above limit %d", len(ids), limit)
		}
		all = append(all, ids...)
		if len(next) == 0 {
			return all
		}
		if len(ids) != int(limit) {
			e.t.Fatalf("short page of %d with a next key", len(ids))
		}
		key = next
	}
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestProvidersPaginationBoundaries: provider counts 0, 1, 2, 100, 101
// against limits 0 (cap), 1, 2, 100; limit 101 and keys of 65 bytes or
// outside the pattern are BOUNDS; a key between ids starts at the next id;
// the last page has an empty next key; a full walk at any limit equals the
// export order with no skip or duplicate.
func TestProvidersPaginationBoundaries(t *testing.T) {
	for _, n := range []int{0, 1, 2, 100, 101} {
		t.Run(fmt.Sprintf("providers-%d", n), func(t *testing.T) {
			e := newEnv(t)
			ids := seedN(e, n)
			state, err := e.k.ExportGenesis(e.unmetered())
			if err != nil {
				t.Fatal(err)
			}
			exportOrder := make([]string, 0, n)
			for _, p := range state.GetProviders() {
				exportOrder = append(exportOrder, p.GetProviderId())
			}
			if !equalIDs(exportOrder, ids) {
				t.Fatal("export is not in id order")
			}
			for _, limit := range []uint32{1, 2, 100} {
				if got := e.walk(limit); !equalIDs(got, ids) {
					t.Fatalf("walk at limit %d: %d ids, want %d", limit, len(got), len(ids))
				}
			}
			// Limit 0 is the cap (100).
			first, next, code := e.page(0, nil)
			if code != msgkit.OK {
				t.Fatal(code)
			}
			wantFirst := n
			if wantFirst > 100 {
				wantFirst = 100
			}
			if len(first) != wantFirst {
				t.Fatalf("limit 0 returned %d, want %d", len(first), wantFirst)
			}
			if (n > 100) != (len(next) != 0) {
				t.Fatalf("next key %q with %d providers", next, n)
			}
			if n > 100 && string(next) != ids[100] {
				t.Fatalf("next key %q, want %q", next, ids[100])
			}
			if _, _, code := e.page(101, nil); code != msgkit.Bounds {
				t.Fatalf("limit 101: %s, want BOUNDS", code)
			}
		})
	}
	t.Run("page-keys", func(t *testing.T) {
		e := newEnv(t)
		ids := seedN(e, 3) // q000 q001 q002
		if _, _, code := e.page(1, []byte("a"+strings.Repeat("b", 64))); code != msgkit.Bounds {
			t.Fatalf("65-byte key: %s", code)
		}
		if _, _, code := e.page(1, []byte("Q000")); code != msgkit.Bounds {
			t.Fatalf("key outside the pattern: %s", code)
		}
		long := "q" + strings.Repeat("z", 63)
		got, next, code := e.page(1, []byte(long))
		if code != msgkit.OK || len(got) != 0 || len(next) != 0 {
			t.Fatalf("64-byte key after every id: %v %q %s", got, next, code)
		}
		got, next, code = e.page(1, []byte(ids[1]))
		if code != msgkit.OK || !equalIDs(got, ids[1:2]) || string(next) != ids[2] {
			t.Fatalf("inclusive key: %v %q %s", got, next, code)
		}
		got, next, code = e.page(5, []byte("q0005"))
		if code != msgkit.OK || !equalIDs(got, ids[1:]) || len(next) != 0 {
			t.Fatalf("key between q000 and q001: %v %q %s", got, next, code)
		}
		got, next, code = e.page(2, []byte(ids[1]))
		if code != msgkit.OK || !equalIDs(got, ids[1:]) || len(next) != 0 {
			t.Fatalf("exact last page: %v %q %s", got, next, code)
		}
	})
	t.Run("governed-page-limit", func(t *testing.T) {
		e := newEnv(t)
		seedN(e, 8)
		e.limits.values[msgkit.LimitMaxPageLimit] = 5
		got, next, code := e.page(0, nil)
		if code != msgkit.OK || len(got) != 5 || string(next) != "q005" {
			t.Fatalf("limit 0 under a governed cap of 5: %v %q %s", got, next, code)
		}
		if _, _, code := e.page(6, nil); code != msgkit.Bounds {
			t.Fatalf("limit 6 above the governed cap: %s", code)
		}
		// A governed value above the protocol ceiling is ignored.
		e.limits.values[msgkit.LimitMaxPageLimit] = 1000
		if _, _, code := e.page(101, nil); code != msgkit.Bounds {
			t.Fatalf("limit 101 with a governed 1000: %s", code)
		}
	})
	t.Run("nil-page", func(t *testing.T) {
		e := newEnv(t)
		seedN(e, 3)
		res, err := e.k.QueryProviders(e.fresh(), &providersv1.QueryProvidersRequest{})
		if err != nil || len(res.GetProviders()) != 3 || len(res.GetPage().GetNextPageKey()) != 0 {
			t.Fatalf("nil page: %v %v", res, err)
		}
	})
	t.Run("lists-every-state", func(t *testing.T) {
		e := newEnv(t)
		ids := seedN(e, 8)
		res, err := e.k.QueryProviders(e.fresh(), &providersv1.QueryProvidersRequest{})
		if err != nil {
			t.Fatal(err)
		}
		states := map[providersv1.ProviderState]bool{}
		for _, p := range res.GetProviders() {
			states[p.GetState()] = true
		}
		if len(states) != 4 || len(res.GetProviders()) != len(ids) {
			t.Fatalf("states %v", states)
		}
	})
}
