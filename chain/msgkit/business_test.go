package msgkit

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
)

func digestOf(b byte) []byte { return bytes.Repeat([]byte{b}, DigestBytes) }

// recordingSource is a peer that records lookups. Unit-test fake.
type recordingSource struct {
	name    string
	digests map[string][]byte
	calls   *[]string
}

func (s recordingSource) BusinessDigest(_ sdk.Context, id string) ([]byte, bool) {
	*s.calls = append(*s.calls, s.name)
	d, ok := s.digests[id]
	return d, ok
}

func newLedgers(t *testing.T) (sdk.Context, Ledger, Ledger, *Peers) {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, "providers", "content")
	peers := NewPeers()
	providers := Ledger{Key: keys["providers"], Prefix: 0x02, Self: "providers", Peers: peers}
	content := Ledger{Key: keys["content"], Prefix: 0x03, Self: "content", Peers: peers}
	peers.Add("providers", providers)
	peers.Add("content", content)
	return ctx, providers, content, peers
}

func TestLedgerGateAndCommit(t *testing.T) {
	ctx, providers, content, _ := newLedgers(t)
	providersStore := ctx.KVStore(providers.Key)

	// Gate checks the id and digest first and never writes.
	before := StoreSnapshot(providersStore)
	for _, tc := range []struct {
		id     string
		digest []byte
		want   Code
	}{
		{"", digestOf(1), Malformed},
		{"Bad", digestOf(1), Bounds},
		{repeat('b', 65), digestOf(1), Bounds},
		{"b-1", nil, Malformed},
		{"b-1", digestOf(1)[:31], Malformed},
		{"b-1", digestOf(1), OK},
	} {
		out := providers.Gate(ctx, tc.id, tc.digest)
		if out.Code != tc.want || out.Idempotent {
			t.Errorf("Gate(%q, %d bytes) = %+v, want %q", tc.id, len(tc.digest), out, tc.want)
		}
	}
	if !bytes.Equal(before, StoreSnapshot(providersStore)) {
		t.Fatal("Gate wrote to the store")
	}

	providers.Commit(ctx, "b-1", digestOf(1))
	stored := providersStore.Get(append([]byte{0x02}, "b-1"...))
	if !bytes.Equal(stored, digestOf(1)) {
		t.Fatalf("stored %x", stored)
	}

	// Same digest: idempotent. Other digest: replay.
	if out := providers.Gate(ctx, "b-1", digestOf(1)); out.Code != OK || !out.Idempotent {
		t.Fatalf("idempotent: %+v", out)
	}
	if out := providers.Gate(ctx, "b-1", digestOf(2)); out.Code != Replay || out.Idempotent {
		t.Fatalf("replay: %+v", out)
	}

	// The id is global: another module sees it through Peers.
	if out := content.Gate(ctx, "b-1", digestOf(2)); out.Code != Replay {
		t.Fatalf("peer replay: %+v", out)
	}
	if out := content.Gate(ctx, "b-1", digestOf(1)); out.Code != OK || !out.Idempotent {
		t.Fatalf("peer idempotent: %+v", out)
	}

	// BusinessDigest reads only its own ledger.
	if _, ok := content.BusinessDigest(ctx, "b-1"); ok {
		t.Fatal("content ledger reports a providers id")
	}
	if got, ok := providers.BusinessDigest(ctx, "b-1"); !ok || !bytes.Equal(got, digestOf(1)) {
		t.Fatalf("providers BusinessDigest %x %v", got, ok)
	}
	got, _ := providers.BusinessDigest(ctx, "b-1")
	got[0] ^= 0xff
	if again, _ := providers.BusinessDigest(ctx, "b-1"); !bytes.Equal(again, digestOf(1)) {
		t.Fatal("BusinessDigest returned an aliased slice")
	}
	if _, ok := providers.BusinessDigest(ctx, "NOT-AN-ID"); ok {
		t.Fatal("invalid id found")
	}
	// The content store is still empty.
	if snapshot := StoreSnapshot(ctx.KVStore(content.Key)); len(snapshot) != 0 {
		t.Fatalf("content store written: %x", snapshot)
	}
}

func TestLedgerOwnRecordWinsOverPeers(t *testing.T) {
	ctx, providers, content, _ := newLedgers(t)
	// Construct an inconsistent state directly (it cannot arise through
	// Gate/Commit) to show the own ledger is consulted first.
	content.Commit(ctx, "b-2", digestOf(9))
	providers.Commit(ctx, "b-2", digestOf(3))
	if out := providers.Gate(ctx, "b-2", digestOf(3)); out.Code != OK || !out.Idempotent {
		t.Fatalf("own record: %+v", out)
	}
	if out := content.Gate(ctx, "b-2", digestOf(9)); out.Code != OK || !out.Idempotent {
		t.Fatalf("own record: %+v", out)
	}
}

func TestLedgerWithoutPeers(t *testing.T) {
	ctx, keys := msgkittest.NewContext(t, "solo")
	solo := Ledger{Key: keys["solo"], Prefix: 0x09, Self: "solo"}
	if out := solo.Gate(ctx, "b-1", digestOf(1)); out.Code != OK || out.Idempotent {
		t.Fatalf("empty: %+v", out)
	}
	solo.Commit(ctx, "b-1", digestOf(1))
	if out := solo.Gate(ctx, "b-1", digestOf(4)); out.Code != Replay {
		t.Fatalf("replay: %+v", out)
	}
}

func TestLedgerCommitRequiresAPassingGate(t *testing.T) {
	ctx, providers, _, _ := newLedgers(t)
	for _, tc := range []struct {
		id     string
		digest []byte
	}{{"", digestOf(1)}, {"Bad", digestOf(1)}, {"b-1", digestOf(1)[:31]}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Commit(%q, %d bytes) did not panic", tc.id, len(tc.digest))
				}
			}()
			providers.Commit(ctx, tc.id, tc.digest)
		}()
	}
	if snapshot := StoreSnapshot(ctx.KVStore(providers.Key)); len(snapshot) != 0 {
		t.Fatalf("a refused commit wrote: %x", snapshot)
	}
}

// Lookups visit peers in name order whatever the Add order, and skip self.
func TestPeersLookupOrderIsDeterministic(t *testing.T) {
	ctx, _ := msgkittest.NewContext(t, "unused")
	var calls []string
	peers := NewPeers()
	peers.Add("leases", recordingSource{name: "leases", digests: map[string][]byte{"x": digestOf(5)}, calls: &calls})
	peers.Add("authority", recordingSource{name: "authority", digests: map[string][]byte{"x": digestOf(6)}, calls: &calls})
	peers.Add("content", recordingSource{name: "content", digests: map[string][]byte{}, calls: &calls})
	if names := peers.Names(); len(names) != 3 || names[0] != "authority" || names[1] != "content" || names[2] != "leases" {
		t.Fatalf("names %v", names)
	}

	got, ok := peers.Lookup(ctx, "x", "content")
	if !ok || !bytes.Equal(got, digestOf(6)) {
		t.Fatalf("lookup %x %v", got, ok)
	}
	if len(calls) != 1 || calls[0] != "authority" {
		t.Fatalf("calls %v", calls)
	}
	calls = calls[:0]
	got, ok = peers.Lookup(ctx, "x", "authority")
	if !ok || !bytes.Equal(got, digestOf(5)) {
		t.Fatalf("lookup skipping authority %x %v", got, ok)
	}
	if len(calls) != 2 || calls[0] != "content" || calls[1] != "leases" {
		t.Fatalf("calls %v", calls)
	}
	calls = calls[:0]
	if _, ok := peers.Lookup(ctx, "missing", ""); ok {
		t.Fatal("missing id found")
	}
	if len(calls) != 3 {
		t.Fatalf("calls %v", calls)
	}
	var nilPeers *Peers
	if _, ok := nilPeers.Lookup(ctx, "x", ""); ok {
		t.Fatal("nil peers found an id")
	}
}

func TestPeersAddRejectsWiringErrors(t *testing.T) {
	var calls []string
	source := recordingSource{name: "a", calls: &calls}
	cases := map[string]func(p *Peers){
		"empty name": func(p *Peers) { p.Add("", source) },
		"nil source": func(p *Peers) { p.Add("a", nil) },
		"duplicate":  func(p *Peers) { p.Add("a", source); p.Add("a", source) },
	}
	for name, add := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			add(NewPeers())
		}()
	}
}
