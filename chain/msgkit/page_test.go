package msgkit

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func TestCheckPage(t *testing.T) {
	key64 := []byte(repeat('k', 64))
	key65 := []byte(repeat('k', 65))
	cases := []struct {
		name      string
		page      *typesv1.PageRequest
		max       uint32
		wantLimit uint32
		wantKey   []byte
		want      Code
	}{
		{"nil page", nil, 100, 100, nil, OK},
		{"limit 0", &typesv1.PageRequest{}, 100, 100, nil, OK},
		{"limit 1", &typesv1.PageRequest{Limit: 1}, 100, 1, nil, OK},
		{"limit 100", &typesv1.PageRequest{Limit: 100}, 100, 100, nil, OK},
		{"limit 101", &typesv1.PageRequest{Limit: 101}, 100, 0, nil, Bounds},
		{"governed max 10, limit 10", &typesv1.PageRequest{Limit: 10}, 10, 10, nil, OK},
		{"governed max 10, limit 11", &typesv1.PageRequest{Limit: 11}, 10, 0, nil, Bounds},
		{"governed max 10, limit 0", &typesv1.PageRequest{}, 10, 10, nil, OK},
		{"max 0 is the cap", &typesv1.PageRequest{Limit: 100}, 0, 100, nil, OK},
		{"max above the cap is the cap", &typesv1.PageRequest{Limit: 101}, 1000, 0, nil, Bounds},
		{"key 64 bytes", &typesv1.PageRequest{PageKey: key64}, 100, 100, key64, OK},
		{"key 65 bytes", &typesv1.PageRequest{PageKey: key65}, 100, 0, nil, Bounds},
		{"key not an id", &typesv1.PageRequest{PageKey: []byte("Provider")}, 100, 0, nil, Bounds},
		{"key with 0x00", &typesv1.PageRequest{PageKey: []byte("p\x00")}, 100, 0, nil, Bounds},
		{"key 'provider-2'", &typesv1.PageRequest{Limit: 5, PageKey: []byte("provider-2")}, 100, 5, []byte("provider-2"), OK},
		{"limit checked before key", &typesv1.PageRequest{Limit: 101, PageKey: key65}, 100, 0, nil, Bounds},
	}
	for _, tc := range cases {
		limit, key, code := CheckPage(tc.page, tc.max)
		if code != tc.want || limit != tc.wantLimit || !bytes.Equal(key, tc.wantKey) {
			t.Errorf("%s: (%d, %q, %q), want (%d, %q, %q)", tc.name, limit, key, code, tc.wantLimit, tc.wantKey, tc.want)
		}
		if code == OK && len(key) > 0 {
			key[0] = 'X'
			if tc.page.PageKey[0] == 'X' {
				t.Errorf("%s: key aliases the request", tc.name)
			}
		}
	}
}

// pagingStore holds ids under prefix 0x01, plus neighbours under 0x00 and
// 0x02 that a page must never return.
func pagingStore(t *testing.T, ids []string) storetypes.KVStore {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, "paging")
	store := ctx.MultiStore().GetKVStore(keys["paging"])
	store.Set([]byte{0x00, 'z'}, []byte("before"))
	store.Set([]byte{0x02, 'a'}, []byte("after"))
	for _, id := range ids {
		store.Set(append([]byte{0x01}, id...), []byte("v:"+id))
	}
	return store
}

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("p%03d", i)
	}
	return out
}

// walk reads every page with the given limit and returns the ids in order.
func walk(t *testing.T, store storetypes.KVStore, limit uint32) ([]string, int) {
	t.Helper()
	var seen []string
	var key []byte
	pages := 0
	for {
		pages++
		if pages > 1000 {
			t.Fatal("pagination does not terminate")
		}
		count := 0
		next, err := Paginate(store, []byte{0x01}, key, limit, func(k, v []byte) error {
			if k[0] != 0x01 || !bytes.Equal(v, append([]byte("v:"), k[1:]...)) {
				t.Fatalf("visited %x=%q", k, v)
			}
			seen = append(seen, string(k[1:]))
			count++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if uint32(count) > limit {
			t.Fatalf("page of %d with limit %d", count, limit)
		}
		if next == nil {
			return seen, pages
		}
		if uint32(count) != limit {
			t.Fatalf("short page %d with a next key", count)
		}
		if !ValidID(string(next)) {
			t.Fatalf("next key %q is not a valid page key", next)
		}
		key = next
	}
}

func TestPaginateBoundaries(t *testing.T) {
	for _, tc := range []struct {
		items     int
		limit     uint32
		wantPages int
	}{
		{0, 1, 1},
		{0, 100, 1},
		{1, 1, 1},
		{1, 100, 1},
		{2, 1, 2},
		{100, 100, 1},
		{100, 10, 10},
		{101, 100, 2},
		{7, 3, 3},
		{9, 3, 3},
	} {
		all := ids(tc.items)
		store := pagingStore(t, all)
		seen, pages := walk(t, store, tc.limit)
		if pages != tc.wantPages {
			t.Errorf("%d items, limit %d: %d pages, want %d", tc.items, tc.limit, pages, tc.wantPages)
		}
		if fmt.Sprint(seen) != fmt.Sprint(all) {
			t.Errorf("%d items, limit %d: walk %v", tc.items, tc.limit, seen)
		}
	}
}

func TestPaginateStartKeyIsInclusive(t *testing.T) {
	store := pagingStore(t, []string{"a", "b", "c", "c0", "d"})
	collect := func(key string, limit uint32) ([]string, string) {
		var got []string
		next, err := Paginate(store, []byte{0x01}, []byte(key), limit, func(k, _ []byte) error {
			got = append(got, string(k[1:]))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return got, string(next)
	}
	if got, next := collect("b", 2); fmt.Sprint(got) != "[b c]" || next != "c0" {
		t.Fatalf("from b: %v next %q", got, next)
	}
	// A key between two ids starts at the next id in byte order.
	if got, next := collect("bz", 2); fmt.Sprint(got) != "[c c0]" || next != "d" {
		t.Fatalf("from bz: %v next %q", got, next)
	}
	if got, next := collect("d", 5); fmt.Sprint(got) != "[d]" || next != "" {
		t.Fatalf("from d: %v next %q", got, next)
	}
	if got, next := collect("e", 5); len(got) != 0 || next != "" {
		t.Fatalf("past the end: %v next %q", got, next)
	}
	// Byte order: "c" < "c0" < "d".
	if got, _ := collect("", 5); fmt.Sprint(got) != "[a b c c0 d]" {
		t.Fatalf("order %v", got)
	}
}

func TestPaginateErrors(t *testing.T) {
	store := pagingStore(t, []string{"a", "b"})
	if _, err := Paginate(store, []byte{0x01}, nil, 0, func(_, _ []byte) error { return nil }); err == nil {
		t.Fatal("limit 0 accepted")
	}
	if _, err := Paginate(store, []byte{0x01}, nil, 1, nil); err == nil {
		t.Fatal("nil visitor accepted")
	}
	stop := errors.New("stop")
	calls := 0
	next, err := Paginate(store, []byte{0x01}, nil, 5, func(_, _ []byte) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || next != nil || calls != 1 {
		t.Fatalf("visitor error: next %q err %v calls %d", next, err, calls)
	}
}
