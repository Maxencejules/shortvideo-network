package evidence_test

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// queryCode is the protocol code of a rejected query, read from the gRPC
// status detail (msgkit.QueryCode); success is OK.
func queryCode(t testing.TB, err error) msgkit.Code {
	t.Helper()
	if err == nil {
		return msgkit.OK
	}
	code, ok := msgkit.QueryCode(err)
	if !ok {
		t.Fatalf("query error %v carries no protocol code", err)
	}
	if codeOf(t, err) != code {
		t.Fatalf("query error %v: ABCI code %s, detail %s", err, codeOf(t, err), code)
	}
	return code
}

func ids(records []*evidencev1.EvidenceRecord) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.GetEvidenceId()
	}
	return out
}

// TestReadsWithoutBlockTimeFailClosed: a context without a committed block
// time (a header time below 1: the zero header of today's App.Export
// context, or of the check state right after a node restart) cannot report
// a record's state. A query by id or by lease and an export that would
// return a record are ErrNoBlockTime, never the stored RECORDED state of a
// long-expired record; a read that returns no record (an absent id, an
// empty store's export) still succeeds. The AppModule export, which can only
// panic, panics with ErrNoBlockTime: the SDK module manager runs it in a
// goroutine, so App.Export must refuse before it (README Handoff, INT).
func TestReadsWithoutBlockTimeFailClosed(t *testing.T) {
	e := newEnv(t)
	zeroCtx := func(ctx sdk.Context) sdk.Context { return ctx.WithBlockTime(time.Time{}) }
	empty, err := e.k.ExportGenesis(zeroCtx(e.unmetered()))
	if err != nil || len(empty.GetRecords()) != 0 {
		t.Fatalf("export of an empty store at zero time: %v %v", empty, err)
	}
	am := evidence.NewAppModule(e.k, &recorder{})
	if raw := am.ExportGenesis(zeroCtx(e.unmetered()), nil); len(raw) == 0 {
		t.Fatal("module export of an empty store at zero time")
	}
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
	id := prefix(auditor1) + "e1"
	resp, err := e.query(now+10*86400, &evidencev1.QueryEvidenceRequest{EvidenceId: id})
	if err != nil || len(resp.GetRecords()) != 1 || resp.GetRecords()[0].GetState() != evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED {
		t.Fatalf("query ten days after expiry: %v %v", resp, err)
	}
	for name, ctx := range map[string]sdk.Context{
		"zero header time": zeroCtx(e.unmetered()),
		"unix 0":           e.at(0),
	} {
		for what, req := range map[string]*evidencev1.QueryEvidenceRequest{
			"by id":    {EvidenceId: id},
			"by lease": {LeaseId: "l1"},
			"both":     {EvidenceId: id, LeaseId: "l1"},
		} {
			if resp, err := e.k.Evidence(ctx, req); !errors.Is(err, evidence.ErrNoBlockTime) {
				t.Fatalf("%s, query %s: %v %v", name, what, resp, err)
			}
		}
		if state, err := e.k.ExportGenesis(ctx); !errors.Is(err, evidence.ErrNoBlockTime) {
			t.Fatalf("%s, export: %v %v", name, state, err)
		}
		func() {
			defer func() {
				if err, ok := recover().(error); !ok || !errors.Is(err, evidence.ErrNoBlockTime) {
					t.Fatalf("%s, module export: recovered %v", name, err)
				}
			}()
			am.ExportGenesis(ctx, nil)
		}()
		resp, err := e.k.Evidence(ctx, &evidencev1.QueryEvidenceRequest{EvidenceId: prefix(auditor1) + "absent"})
		if err != nil || len(resp.GetRecords()) != 0 {
			t.Fatalf("%s, absent id: %v %v", name, resp, err)
		}
		resp, err = e.k.Evidence(ctx, &evidencev1.QueryEvidenceRequest{LeaseId: "l3"})
		if err != nil || len(resp.GetRecords()) != 0 {
			t.Fatalf("%s, lease without records: %v %v", name, resp, err)
		}
	}
	if resp, err := e.query(genesisUnixMin, &evidencev1.QueryEvidenceRequest{EvidenceId: id}); err != nil ||
		resp.GetRecords()[0].GetState() != evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED {
		t.Fatalf("query at unix 1: %v %v", resp, err)
	}
}

// genesisUnixMin is the first protocol time; a context at it has a block
// time.
const genesisUnixMin = 1

// TestEvidenceQuery (DR-09-17): by id, by lease (paginated by evidence id,
// inclusive page key, next key, last page), both, neither (MALFORMED),
// off-pattern ids and page keys (BOUNDS), limits 0/1/cap/cap+1, the
// governed page limit, a page key with an evidence-id filter (BOUNDS), and
// the reported state at the query time.
func TestEvidenceQuery(t *testing.T) {
	e := newEnv(t)
	e.mustSubmit(signed(auditor1, "1", e.items(auditor1, "a", "l1", 5)...))
	e.mustSubmit(signed(auditor2, "2", e.item(auditor2, "b", "l1"), e.item(auditor2, "c", "l2")))
	l1 := []string{prefix(auditor2) + "b"}
	for i := 0; i < 5; i++ {
		l1 = append(l1, fmt.Sprintf("%sa-%d", prefix(auditor1), i))
	}
	sort.Strings(l1)

	t.Run("by-id", func(t *testing.T) {
		resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{EvidenceId: prefix(auditor2) + "c"})
		if err != nil || len(resp.GetRecords()) != 1 || resp.GetRecords()[0].GetLeaseId() != "l2" || len(resp.GetPage().GetNextPageKey()) != 0 {
			t.Fatalf("%v %v", resp, err)
		}
		resp, err = e.query(now, &evidencev1.QueryEvidenceRequest{EvidenceId: prefix(auditor2) + "absent"})
		if err != nil || len(resp.GetRecords()) != 0 {
			t.Fatalf("absent id: %v %v", resp, err)
		}
		_, err = e.query(now, &evidencev1.QueryEvidenceRequest{EvidenceId: prefix(auditor2) + "c", Page: &typesv1.PageRequest{PageKey: []byte("x")}})
		if queryCode(t, err) != msgkit.Bounds {
			t.Fatalf("page key with an evidence id: %v", err)
		}
	})
	t.Run("by-lease-pages", func(t *testing.T) {
		var got []string
		var key []byte
		for page := 0; ; page++ {
			resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{Limit: 2, PageKey: key}})
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.GetRecords()) > 2 {
				t.Fatalf("page of %d", len(resp.GetRecords()))
			}
			got = append(got, ids(resp.GetRecords())...)
			key = resp.GetPage().GetNextPageKey()
			if len(key) == 0 {
				break
			}
			if string(key) != l1[len(got)] {
				t.Fatalf("next key %q, want %q (inclusive start of the next page)", key, l1[len(got)])
			}
			if page > 5 {
				t.Fatal("pagination does not end")
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(l1) {
			t.Fatalf("pages %v, want %v", got, l1)
		}
		resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{PageKey: []byte(l1[3])}})
		if err != nil || fmt.Sprint(ids(resp.GetRecords())) != fmt.Sprint(l1[3:]) || len(resp.GetPage().GetNextPageKey()) != 0 {
			t.Fatalf("inclusive page key: %v %v", resp, err)
		}
		resp, err = e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l3"})
		if err != nil || len(resp.GetRecords()) != 0 {
			t.Fatalf("lease without records: %v %v", resp, err)
		}
		// "l1" must not match records of a lease "l10".
		e.putLease(e.leaseRecord("l10", "c1", "p1"))
		e.mustSubmit(signed(auditor1, "3", e.item(auditor1, "l10", "l10")))
		resp, err = e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1"})
		if err != nil || len(resp.GetRecords()) != len(l1) {
			t.Fatalf("lease scope: %v %v", ids(resp.GetRecords()), err)
		}
	})
	t.Run("both-filters", func(t *testing.T) {
		resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{EvidenceId: prefix(auditor2) + "c", LeaseId: "l2"})
		if err != nil || len(resp.GetRecords()) != 1 {
			t.Fatalf("%v %v", resp, err)
		}
		resp, err = e.query(now, &evidencev1.QueryEvidenceRequest{EvidenceId: prefix(auditor2) + "c", LeaseId: "l1"})
		if err != nil || len(resp.GetRecords()) != 0 {
			t.Fatalf("record of another lease: %v %v", resp, err)
		}
	})
	t.Run("rejections", func(t *testing.T) {
		cases := map[string]struct {
			req  *evidencev1.QueryEvidenceRequest
			want msgkit.Code
		}{
			"neither":             {&evidencev1.QueryEvidenceRequest{}, msgkit.Malformed},
			"page-only":           {&evidencev1.QueryEvidenceRequest{Page: &typesv1.PageRequest{Limit: 1}}, msgkit.Malformed},
			"evidence-id-pattern": {&evidencev1.QueryEvidenceRequest{EvidenceId: "E1"}, msgkit.Bounds},
			"lease-id-pattern":    {&evidencev1.QueryEvidenceRequest{LeaseId: "L1"}, msgkit.Bounds},
			"limit-101":           {&evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{Limit: 101}}, msgkit.Bounds},
			"page-key-65-bytes":   {&evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{PageKey: bytes.Repeat([]byte("a"), 65)}}, msgkit.Bounds},
			"page-key-pattern":    {&evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{PageKey: []byte("A")}}, msgkit.Bounds},
		}
		for name, tc := range cases {
			if _, err := e.query(now, tc.req); queryCode(t, err) != tc.want {
				t.Fatalf("%s: %v, want %s", name, err, tc.want)
			}
		}
	})
	t.Run("limits", func(t *testing.T) {
		for limit, want := range map[uint32]int{0: len(l1), 1: 1, 100: len(l1)} {
			resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{Limit: limit}})
			if err != nil || len(resp.GetRecords()) != want {
				t.Fatalf("limit %d: %v %v", limit, len(resp.GetRecords()), err)
			}
		}
		e.auth.limits[msgkit.LimitMaxPageLimit] = 3
		defer delete(e.auth.limits, msgkit.LimitMaxPageLimit)
		resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1"})
		if err != nil || len(resp.GetRecords()) != 3 || string(resp.GetPage().GetNextPageKey()) != l1[3] {
			t.Fatalf("governed cap 3: %v %v", resp, err)
		}
		if _, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{Limit: 4}}); queryCode(t, err) != msgkit.Bounds {
			t.Fatalf("limit above the governed cap: %v", err)
		}
	})
	t.Run("reported-state", func(t *testing.T) {
		for at, want := range map[int64]evidencev1.EvidenceState{
			now + itemLife - 1: evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED,
			now + itemLife:     evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED,
		} {
			resp, err := e.query(at, &evidencev1.QueryEvidenceRequest{LeaseId: "l1"})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range resp.GetRecords() {
				if r.GetState() != want {
					t.Fatalf("at %d: %s %s", at, r.GetEvidenceId(), r.GetState())
				}
			}
		}
		if e.stored(l1[0]).GetState() != evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED {
			t.Fatal("a query changed the stored state")
		}
	})
	t.Run("broken-index-row", func(t *testing.T) {
		e := newEnv(t)
		e.store(evidence.ModuleName).Set(evidence.LeaseIndexKey("l1", prefix(auditor1)+"ghost"), evidence.Marker)
		if _, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1"}); err == nil {
			t.Fatal("an index row without its record was served")
		} else if _, ok := msgkit.QueryCode(err); ok {
			t.Fatalf("an invariant violation carries a protocol code: %v", err)
		}
	})
}

// TestEvidenceQueryResponseSizeBound: a full page of 100 maximal records
// (64-byte ids, maximal integers) stays below a stated bound; the encoded
// size is computed and logged.
func TestEvidenceQueryResponseSizeBound(t *testing.T) {
	e := newEnv(t)
	maxLease := "l" + string(bytes.Repeat([]byte("z"), 63))
	maxContent := "c" + string(bytes.Repeat([]byte("z"), 63))
	l := e.leaseRecord(maxLease, maxContent, "p1")
	e.putLease(l)
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("%s%03d", prefix(auditor1), i)
		id += string(bytes.Repeat([]byte("z"), 64-len(id)))
		e.k.SeedRecordForTest(e.unmetered(), &evidencev1.EvidenceRecord{
			EvidenceId: id, BatchId: prefix(auditor1) + string(bytes.Repeat([]byte("b"), 64-17)),
			Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION, State: evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED,
			SignerPubkey: pub(auditor1), LeaseId: maxLease, ContentId: maxContent, ContentVersion: 1<<53 - 1,
			ObservationDigest: digestOf(id), Epoch: 1<<53 - 1, ExpiryUnixSeconds: 253402300799, SchemaVersion: 1, Attestation: true,
		})
	}
	resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: maxLease, Page: &typesv1.PageRequest{Limit: 100}})
	if err != nil || len(resp.GetRecords()) != 100 {
		t.Fatalf("%v %v", len(resp.GetRecords()), err)
	}
	size := proto.Size(resp)
	const bound = 64 * 1024
	if size >= bound {
		t.Fatalf("a page of 100 maximal records is %d bytes, bound %d", size, bound)
	}
	t.Logf("page of 100 maximal records: %d encoded bytes (bound %d)", size, bound)
}

// FuzzEvidencePageKey: any page key and limit either is rejected with a
// protocol code (BOUNDS) or returns at most limit records of the lease in
// id order, never an invariant error, and never panics.
func FuzzEvidencePageKey(f *testing.F) {
	f.Add([]byte(""), uint32(0))
	f.Add([]byte("a"), uint32(2))
	f.Add(bytes.Repeat([]byte("z"), 64), uint32(100))
	f.Add([]byte{0xff, 0x00}, uint32(101))
	f.Fuzz(func(t *testing.T, key []byte, limit uint32) {
		e := newEnv(t)
		e.mustSubmit(signed(auditor1, "1", e.items(auditor1, "a", "l1", 3)...))
		resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1", Page: &typesv1.PageRequest{Limit: limit, PageKey: key}})
		if err != nil {
			if code, ok := msgkit.QueryCode(err); !ok || code != msgkit.Bounds {
				t.Fatalf("key %x limit %d: %v", key, limit, err)
			}
			return
		}
		max := int(limit)
		if limit == 0 {
			max = 100
		}
		if len(resp.GetRecords()) > max {
			t.Fatalf("%d records for limit %d", len(resp.GetRecords()), limit)
		}
		for i, r := range resp.GetRecords() {
			if r.GetLeaseId() != "l1" || (i > 0 && r.GetEvidenceId() <= resp.GetRecords()[i-1].GetEvidenceId()) ||
				(len(key) > 0 && r.GetEvidenceId() < string(key)) {
				t.Fatalf("record %d %q for key %q", i, r.GetEvidenceId(), key)
			}
		}
	})
}
