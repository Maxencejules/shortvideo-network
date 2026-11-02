package leases_test

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// queryCode is the protocol code of a rejected query, read from the gRPC
// status detail (msgkit.QueryCode); it must agree with the ABCI code.
func queryCode(t testing.TB, err error) msgkit.Code {
	t.Helper()
	if err == nil {
		return msgkit.OK
	}
	code, ok := msgkit.QueryCode(err)
	if !ok {
		t.Fatalf("query error without a protocol code: %v", err)
	}
	if abci := codeOf(t, err); abci != code {
		t.Fatalf("query error detail %s, ABCI code %s", code, abci)
	}
	return code
}

func page(limit uint32, key []byte) *typesv1.PageRequest {
	return &typesv1.PageRequest{Limit: limit, PageKey: key}
}

func leaseIDs(ls []*leasesv1.Lease) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.GetLeaseId())
	}
	return out
}

func offerIDs(os []*leasesv1.Offer) []string {
	var out []string
	for _, o := range os {
		out = append(out, o.GetOfferId())
	}
	return out
}

// activeFixture: providers p1..p5 hold c1 (leases l-c1-p1..p5); p1 also
// holds d00..d11; an ended lease of c1 and of p1 exists (not listed).
func activeFixture(e *env) {
	for i := 3; i <= 5; i++ {
		e.putProvider(fmt.Sprintf("p%d", i), bytes.Repeat([]byte{byte(0xa0 + i)}, 20), providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1<<30, 0)
	}
	for i := 1; i <= 5; i++ {
		e.seedLease(e.leaseRecord(fmt.Sprintf("l-c1-p%d", i), "c1", fmt.Sprintf("p%d", i)))
	}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("d%02d", i)
		e.putContent(id, contentv1.ContentState_CONTENT_STATE_PENDING, packageSize)
		e.seedLease(e.leaseRecord("l-"+id, id, "p1"))
	}
	ended := e.leaseRecord("l-ended", "c1", "p1")
	ended.State, ended.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_ENDED, false
	e.seedLease(ended)
}

func (e *env) active(req *leasesv1.QueryActiveLeasesRequest) (*leasesv1.QueryActiveLeasesResponse, msgkit.Code) {
	e.t.Helper()
	res, err := e.k.ActiveLeases(e.fresh(), req)
	return res, queryCode(e.t, err)
}

// walkActive pages through ActiveLeases and returns the ids and page sizes.
func (e *env) walkActive(req *leasesv1.QueryActiveLeasesRequest, limit uint32) ([]string, []int) {
	e.t.Helper()
	var ids []string
	var sizes []int
	var key []byte
	for {
		r := proto.Clone(req).(*leasesv1.QueryActiveLeasesRequest)
		r.Page = page(limit, key)
		res, code := e.active(r)
		if code != msgkit.OK {
			e.t.Fatalf("page after %q: %s", key, code)
		}
		ids = append(ids, leaseIDs(res.GetLeases())...)
		sizes = append(sizes, len(res.GetLeases()))
		key = res.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return ids, sizes
		}
		if len(sizes) > 100 {
			e.t.Fatal("pagination does not end")
		}
	}
}

// TestActiveLeasesPagination (REQ-IDX-001): ActiveLeases lists only held
// leases by content, by provider, or by pair, with inclusive page keys,
// the governed page cap, and BOUNDS for every out-of-range request.
func TestActiveLeasesPagination(t *testing.T) {
	e := newEnv(t)
	activeFixture(e)
	byContent := []string{"l-c1-p1", "l-c1-p2", "l-c1-p3", "l-c1-p4", "l-c1-p5"}
	var byProvider []string
	byProvider = append(byProvider, "l-c1-p1")
	for i := 0; i < 12; i++ {
		byProvider = append(byProvider, fmt.Sprintf("l-d%02d", i))
	}
	slices.Sort(byProvider)

	ids, sizes := e.walkActive(&leasesv1.QueryActiveLeasesRequest{ContentId: "c1"}, 2)
	if !slices.Equal(ids, byContent) || !slices.Equal(sizes, []int{2, 2, 1}) {
		t.Fatalf("content walk %v %v", ids, sizes)
	}
	ids, sizes = e.walkActive(&leasesv1.QueryActiveLeasesRequest{ProviderId: "p1"}, 5)
	if !slices.Equal(ids, byProvider) || !slices.Equal(sizes, []int{5, 5, 3}) {
		t.Fatalf("provider walk %v %v", ids, sizes)
	}
	// limit 0 and a nil page use the cap; the cap holds everything here.
	for _, p := range []*typesv1.PageRequest{nil, page(0, nil), page(100, nil)} {
		res, code := e.active(&leasesv1.QueryActiveLeasesRequest{ProviderId: "p1", Page: p})
		if code != msgkit.OK || !slices.Equal(leaseIDs(res.GetLeases()), byProvider) || len(res.GetPage().GetNextPageKey()) != 0 {
			t.Fatalf("page %v: %s %v", p, code, res)
		}
	}
	// limit 1 and the inclusive key.
	res, _ := e.active(&leasesv1.QueryActiveLeasesRequest{ContentId: "c1", Page: page(1, []byte("l-c1-p3"))})
	if !slices.Equal(leaseIDs(res.GetLeases()), []string{"l-c1-p3"}) || string(res.GetPage().GetNextPageKey()) != "l-c1-p4" {
		t.Fatalf("inclusive key %v", res)
	}
	// A key between ids starts at the next id; a key past the end is an
	// empty last page.
	res, _ = e.active(&leasesv1.QueryActiveLeasesRequest{ContentId: "c1", Page: page(0, []byte("l-c1-p20"))})
	if !slices.Equal(leaseIDs(res.GetLeases()), []string{"l-c1-p3", "l-c1-p4", "l-c1-p5"}) {
		t.Fatalf("key between ids %v", res)
	}
	res, _ = e.active(&leasesv1.QueryActiveLeasesRequest{ContentId: "c1", Page: page(0, []byte("zzz"))})
	if len(res.GetLeases()) != 0 || len(res.GetPage().GetNextPageKey()) != 0 {
		t.Fatalf("key past the end %v", res)
	}
	// Pair mode.
	res, code := e.active(&leasesv1.QueryActiveLeasesRequest{ContentId: "c1", ProviderId: "p2"})
	if code != msgkit.OK || !slices.Equal(leaseIDs(res.GetLeases()), []string{"l-c1-p2"}) {
		t.Fatalf("pair %v %s", res, code)
	}
	res, code = e.active(&leasesv1.QueryActiveLeasesRequest{ContentId: "c2", ProviderId: "p2"})
	if code != msgkit.OK || len(res.GetLeases()) != 0 {
		t.Fatalf("empty pair %v %s", res, code)
	}
	// Governed page cap.
	e.auth.limits[msgkit.LimitMaxPageLimit] = 3
	res, _ = e.active(&leasesv1.QueryActiveLeasesRequest{ProviderId: "p1"})
	if len(res.GetLeases()) != 3 || string(res.GetPage().GetNextPageKey()) != byProvider[3] {
		t.Fatalf("governed cap page %v", res)
	}
	rejects := map[string]struct {
		req  *leasesv1.QueryActiveLeasesRequest
		want msgkit.Code
	}{
		"no-filter":             {&leasesv1.QueryActiveLeasesRequest{}, msgkit.Malformed},
		"no-filter-with-page":   {&leasesv1.QueryActiveLeasesRequest{Page: page(1, nil)}, msgkit.Malformed},
		"content-id-pattern":    {&leasesv1.QueryActiveLeasesRequest{ContentId: "C1"}, msgkit.Bounds},
		"provider-id-pattern":   {&leasesv1.QueryActiveLeasesRequest{ProviderId: "p1 "}, msgkit.Bounds},
		"above-governed-cap":    {&leasesv1.QueryActiveLeasesRequest{ProviderId: "p1", Page: page(4, nil)}, msgkit.Bounds},
		"page-key-65-bytes":     {&leasesv1.QueryActiveLeasesRequest{ContentId: "c1", Page: page(1, bytes.Repeat([]byte{'a'}, 65))}, msgkit.Bounds},
		"page-key-pattern":      {&leasesv1.QueryActiveLeasesRequest{ContentId: "c1", Page: page(1, []byte("L-C1"))}, msgkit.Bounds},
		"pair-mode-with-a-key":  {&leasesv1.QueryActiveLeasesRequest{ContentId: "c1", ProviderId: "p1", Page: page(1, []byte("l"))}, msgkit.Bounds},
		"content-pattern-first": {&leasesv1.QueryActiveLeasesRequest{ContentId: "C1", ProviderId: "p1", Page: page(1000, nil)}, msgkit.Bounds},
	}
	for name, tc := range rejects {
		if _, code := e.active(tc.req); code != tc.want {
			t.Fatalf("%s: %s, want %s", name, code, tc.want)
		}
	}
	delete(e.auth.limits, msgkit.LimitMaxPageLimit)
	if _, code := e.active(&leasesv1.QueryActiveLeasesRequest{ProviderId: "p1", Page: page(101, nil)}); code != msgkit.Bounds {
		t.Fatalf("limit 101: %s", code)
	}
}

// deadlineFixture: held leases la, lb (deadline D1) and lc (D2 > D1), an
// ended lease (not listed); open offers oa (E1), ob (E2), ol (E3 > notAfter).
const (
	d1       = t0 + 4000
	d2       = t0 + 4500
	e1       = t0 + 300
	e2       = t0 + 400
	e3       = t0 + 9000
	notAfter = t0 + 5000
)

func deadlineFixture(e *env) {
	lease := func(id, content string, deadline int64) {
		l := e.leaseRecord(id, content, "p1")
		l.StartUnixSeconds, l.DeadlineUnixSeconds = deadline-term, deadline
		e.seedLease(l)
	}
	for _, id := range []string{"x1", "x2", "x3", "x4"} {
		e.putContent(id, contentv1.ContentState_CONTENT_STATE_PENDING, packageSize)
	}
	lease("lb", "x1", d1)
	lease("la", "x2", d1)
	lease("lc", "x3", d2)
	ended := e.leaseRecord("l-ended", "x4", "p1")
	ended.State, ended.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_EXPIRED, false
	e.seedLease(ended)
	offer := func(id string, expiry int64) {
		o := e.offerRecord(id, "c1", "p2")
		o.ExpiryUnixSeconds = expiry
		e.seedOffer(o)
	}
	offer("ob", e2)
	offer("oa", e1)
	offer("ol", e3)
}

func (e *env) deadlines(notAfter int64, p *typesv1.PageRequest) (*leasesv1.QueryDeadlinesResponse, msgkit.Code) {
	e.t.Helper()
	res, err := e.k.Deadlines(e.fresh(), &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: notAfter, Page: p})
	return res, queryCode(e.t, err)
}

// TestDeadlinesPagination (REQ-IDX-001): Deadlines lists held leases by
// (deadline, id), then open offers by (expiry, id), at most limit items
// per page across both lists, with a cursor that marks phase 2 by the high
// bit of its first byte.
func TestDeadlinesPagination(t *testing.T) {
	e := newEnv(t)
	deadlineFixture(e)
	type pageWant struct {
		leases, offers []string
		next           []byte
	}
	walk := func(limit uint32, want []pageWant) {
		t.Helper()
		var key []byte
		for i, w := range want {
			res, code := e.deadlines(notAfter, page(limit, key))
			if code != msgkit.OK {
				t.Fatalf("limit %d page %d: %s", limit, i, code)
			}
			if !slices.Equal(leaseIDs(res.GetLeases()), w.leases) || !slices.Equal(offerIDs(res.GetOffers()), w.offers) ||
				!bytes.Equal(res.GetPage().GetNextPageKey(), w.next) {
				t.Fatalf("limit %d page %d: leases %v offers %v next %q", limit, i,
					leaseIDs(res.GetLeases()), offerIDs(res.GetOffers()), res.GetPage().GetNextPageKey())
			}
			key = res.GetPage().GetNextPageKey()
		}
	}
	cursor2 := func(id string) []byte { return leases.EncodeCursorForTest(leases.PhaseOffersForTest, id) }
	// The phase boundary falls inside the second page.
	walk(2, []pageWant{
		{leases: []string{"la", "lb"}, next: []byte("lc")},
		{leases: []string{"lc"}, offers: []string{"oa"}, next: cursor2("ob")},
		{offers: []string{"ob"}},
	})
	// A page that ends exactly at the last lease points to the first offer.
	walk(3, []pageWant{
		{leases: []string{"la", "lb", "lc"}, next: cursor2("oa")},
		{offers: []string{"oa", "ob"}},
	})
	walk(1, []pageWant{
		{leases: []string{"la"}, next: []byte("lb")},
		{leases: []string{"lb"}, next: []byte("lc")},
		{leases: []string{"lc"}, next: cursor2("oa")},
		{offers: []string{"oa"}, next: cursor2("ob")},
		{offers: []string{"ob"}},
	})
	walk(0, []pageWant{{leases: []string{"la", "lb", "lc"}, offers: []string{"oa", "ob"}}})
	// not_after filters both lists, inclusively.
	res, _ := e.deadlines(d1, nil)
	if !slices.Equal(leaseIDs(res.GetLeases()), []string{"la", "lb"}) || !slices.Equal(offerIDs(res.GetOffers()), []string{"oa", "ob"}) {
		t.Fatalf("not_after D1 %v", res)
	}
	res, _ = e.deadlines(e1, nil)
	if len(res.GetLeases()) != 0 || !slices.Equal(offerIDs(res.GetOffers()), []string{"oa"}) {
		t.Fatalf("not_after E1 %v", res)
	}
	res, _ = e.deadlines(genesis.UnixSecondsMax, nil)
	if len(res.GetLeases()) != 3 || len(res.GetOffers()) != 3 {
		t.Fatalf("not_after max %v", res)
	}
	// The high-bit cursor.
	if c := cursor2("ob"); c[0] != 'o'|0x80 || string(c[1:]) != "b" {
		t.Fatalf("phase-2 cursor %x", c)
	}
	// Governed page cap.
	e.auth.limits[msgkit.LimitMaxPageLimit] = 2
	res, code := e.deadlines(notAfter, nil)
	if code != msgkit.OK || len(res.GetLeases()) != 2 || string(res.GetPage().GetNextPageKey()) != "lc" {
		t.Fatalf("governed cap %v %s", res, code)
	}
	rejects := map[string]struct {
		notAfter int64
		page     *typesv1.PageRequest
		want     msgkit.Code
	}{
		"not-after-0":           {0, nil, msgkit.Malformed},
		"not-after-negative":    {-1, nil, msgkit.Bounds},
		"not-after-above-9999":  {genesis.UnixSecondsMax + 1, nil, msgkit.Bounds},
		"limit-above-cap":       {notAfter, page(3, nil), msgkit.Bounds},
		"cursor-65-bytes":       {notAfter, page(1, bytes.Repeat([]byte{'a'}, 65)), msgkit.Bounds},
		"cursor-pattern":        {notAfter, page(1, []byte("LA")), msgkit.Bounds},
		"cursor-high-bit-bad":   {notAfter, page(1, []byte{'A' | 0x80}), msgkit.Bounds},
		"cursor-absent-lease":   {notAfter, page(1, []byte("l-none")), msgkit.Bounds},
		"cursor-ended-lease":    {notAfter, page(1, []byte("l-ended")), msgkit.Bounds},
		"cursor-lease-too-late": {d1, page(1, []byte("lc")), msgkit.Bounds},
		"cursor-absent-offer":   {notAfter, page(1, cursor2("o-none")), msgkit.Bounds},
		"cursor-offer-too-late": {notAfter, page(1, cursor2("ol")), msgkit.Bounds},
		"cursor-lease-as-offer": {notAfter, page(1, cursor2("la")), msgkit.Bounds},
		"cursor-offer-as-lease": {notAfter, page(1, []byte("oa")), msgkit.Bounds},
	}
	for name, tc := range rejects {
		if _, code := e.deadlines(tc.notAfter, tc.page); code != tc.want {
			t.Fatalf("%s: %s, want %s", name, code, tc.want)
		}
	}
	delete(e.auth.limits, msgkit.LimitMaxPageLimit)
	if _, code := e.deadlines(notAfter, page(101, nil)); code != msgkit.Bounds {
		t.Fatalf("limit 101: %s", code)
	}
}

// FuzzDeadlinesPageKey: decoding a page key never panics; a key that
// decodes re-encodes to the same bytes; Deadlines answers any key with a
// page or a protocol code, never a panic or an untyped error.
func FuzzDeadlinesPageKey(f *testing.F) {
	for _, seed := range [][]byte{
		nil, []byte("la"), []byte("lc"), {'o' | 0x80, 'a'}, {'o' | 0x80, 'b'}, {0x80}, {0xff, 'a'}, []byte("L"),
		bytes.Repeat([]byte{'a'}, 64), bytes.Repeat([]byte{'a'}, 65), {'a' | 0x80, 0x80},
	} {
		f.Add(seed, int64(notAfter), uint32(2))
	}
	e := newEnv(f)
	deadlineFixture(e)
	f.Fuzz(func(t *testing.T, key []byte, notAfter int64, limit uint32) {
		phase, id, ok := leases.DecodeCursorForTest(key)
		if ok {
			if !msgkit.ValidID(id) || (phase != leases.PhaseLeasesForTest && phase != leases.PhaseOffersForTest) {
				t.Fatalf("decoded %d %q from %x", phase, id, key)
			}
			if !bytes.Equal(leases.EncodeCursorForTest(phase, id), key) {
				t.Fatalf("cursor %x does not round-trip", key)
			}
		}
		res, err := e.k.Deadlines(e.fresh(), &leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: notAfter, Page: page(limit, key)})
		if err != nil {
			if _, typed := msgkit.QueryCode(err); !typed {
				t.Fatalf("untyped query error %v", err)
			}
			return
		}
		if len(res.GetLeases())+len(res.GetOffers()) > 100 {
			t.Fatalf("page of %d items", len(res.GetLeases())+len(res.GetOffers()))
		}
		if next := res.GetPage().GetNextPageKey(); len(next) != 0 {
			if _, _, ok := leases.DecodeCursorForTest(next); !ok {
				t.Fatalf("next key %x does not decode", next)
			}
		}
	})
}

// maxID is a 64-byte id in the id pattern.
func maxID(prefix string, i int) string {
	s := fmt.Sprintf("%s%d-", prefix, i)
	return s + strings.Repeat("z", 64-len(s))
}

// TestLeaseQueryResponseSizeBound: with records at their largest encoding
// (64-byte ids, 32-byte digests, every integer at its widest varint), an
// ActiveLeases page at the cap of 100 and every Deadlines page stay below
// 64 KiB. Records have a constant size, so the bound is 100 records.
func TestLeaseQueryResponseSizeBound(t *testing.T) {
	const bound = 64 << 10
	e := newEnv(t)
	content := maxID("c", 0)
	e.putContent(content, contentv1.ContentState_CONTENT_STATE_PENDING, genesis.MaxPackageBytes)
	widest := func(l *leasesv1.Lease) *leasesv1.Lease {
		l.ContentVersion, l.TermVersion, l.PolicyVersion = math.MaxUint64, math.MaxUint64, math.MaxUint64
		l.PackageBytes = math.MaxUint64
		l.StartUnixSeconds, l.DeadlineUnixSeconds = math.MinInt64, genesis.UnixSecondsMax
		l.State = leasesv1.LeaseState_LEASE_STATE_RENEWED
		return l
	}
	for i := 0; i < 130; i++ {
		provider := maxID("p", i)
		l := e.leaseRecord(maxID("l", i), content, provider)
		l.OfferId = maxID("o", i)
		e.k.SeedLeaseForTest(e.unmetered(), widest(l))
		o := e.offerRecord(maxID("q", i), content, provider)
		o.ContentVersion, o.TermVersion, o.PackageBytes = math.MaxUint64, math.MaxUint64, math.MaxUint64
		o.StartUnixSeconds, o.DeadlineUnixSeconds, o.ExpiryUnixSeconds = math.MinInt64, math.MinInt64, genesis.UnixSecondsMax
		if err := e.k.SeedOfferForTest(e.unmetered(), o); err != nil {
			t.Fatal(err)
		}
	}
	res, code := e.active(&leasesv1.QueryActiveLeasesRequest{ContentId: content, Page: page(100, nil)})
	if code != msgkit.OK || len(res.GetLeases()) != 100 {
		t.Fatalf("ActiveLeases page: %s, %d leases", code, len(res.GetLeases()))
	}
	size := proto.Size(res)
	t.Logf("ActiveLeases page of 100 widest leases: %d bytes (bound %d)", size, bound)
	if size > bound {
		t.Fatalf("ActiveLeases page %d bytes", size)
	}
	var key []byte
	largest, pages := 0, 0
	for {
		res, code := e.deadlines(genesis.UnixSecondsMax, page(100, key))
		if code != msgkit.OK {
			t.Fatal(code)
		}
		if n := len(res.GetLeases()) + len(res.GetOffers()); n > 100 {
			t.Fatalf("Deadlines page of %d items", n)
		}
		largest = max(largest, proto.Size(res))
		pages++
		key = res.GetPage().GetNextPageKey()
		if len(key) == 0 {
			break
		}
	}
	t.Logf("largest Deadlines page: %d bytes over %d pages (bound %d)", largest, pages, bound)
	if largest > bound || pages != 3 {
		t.Fatalf("Deadlines pages: largest %d bytes, %d pages", largest, pages)
	}
}
