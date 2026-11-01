package providers

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Stated response bounds (README "Limits and residual notes"). A gRPC client
// with default options rejects a response above 4 MiB.
const (
	providerResponseBound = 64 << 10
	providersPageBound    = 128 << 10
	exportRecordBound     = 192 << 10
	grpcDefaultReceive    = 4 << 20
)

// TestRetiredKeyBoundsFitAPage: one provider's whole history fits in the
// Providers page budget, so the first provider of a page, which a page
// always returns, never takes the page above it.
func TestRetiredKeyBoundsFitAPage(t *testing.T) {
	if MaxRetiredKeysPerProvider > MaxRetiredKeysPerPage {
		t.Fatalf("MaxRetiredKeysPerProvider %d above MaxRetiredKeysPerPage %d", MaxRetiredKeysPerProvider, MaxRetiredKeysPerPage)
	}
	if MaxRetiredKeysPerProvider < 1 {
		t.Fatal("a provider must be able to rotate at least once")
	}
}

// TestRotationCap: a provider may retire MaxRetiredKeysPerProvider keys.
// The rotation that would retire one more is ERROR_CODE_BOUNDS and leaves
// every store byte-identical and emits nothing. The bound runs after every
// spec check: a stale generation or a conflicting key at the bound still
// gets its spec code.
func TestRotationCap(t *testing.T) {
	e := newEnv(t)
	e.seed(withRetired("p1", MaxRetiredKeysPerProvider-1))
	if e.record("p1").GetKeyGeneration() != MaxRetiredKeysPerProvider {
		t.Fatalf("seeded generation %d", e.record("p1").GetKeyGeneration())
	}
	// The last rotation the bound allows.
	e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-last", MaxRetiredKeysPerProvider)), "rotation to the bound")
	at := e.record("p1")
	if at.GetKeyGeneration() != MaxRetiredKeysPerProvider+1 || at.GetSigningKeyId() != "key-last" {
		t.Fatalf("after the last allowed rotation: generation %d key %s", at.GetKeyGeneration(), at.GetSigningKeyId())
	}
	res, err := e.k.QueryProvider(e.unmetered(), &providersv1.QueryProviderRequest{ProviderId: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.GetProvider().GetPreviousSigningKeys()); got != MaxRetiredKeysPerProvider {
		t.Fatalf("%d retired keys at the bound", got)
	}

	for _, c := range []struct {
		name string
		msg  *providersv1.MsgRotateProviderKey
		want msgkit.Code
	}{
		{"one rotation past the bound", rotMsg(t, "p1", "key-last", "key-over", MaxRetiredKeysPerProvider+1), msgkit.Bounds},
		{"stale generation at the bound", rotMsg(t, "p1", "key-last", "key-over", 7), msgkit.StaleVersion},
		{"same key at the bound", func() *providersv1.MsgRotateProviderKey {
			m := &providersv1.MsgRotateProviderKey{
				Signer: providerAcct, BusinessId: "rot-same", ProviderId: "p1",
				NewSigningPubkey: testPub("key-last"), NewSigningKeyId: "key-other", ExpectedGeneration: MaxRetiredKeysPerProvider + 1,
			}
			signRotate(t, m, "key-last")
			return m
		}(), msgkit.Conflict},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := e.snapshot()
			r := e.rotate(e.fresh(), c.msg)
			if r.code != c.want {
				t.Fatalf("code %s, want %s", r.code, c.want)
			}
			if len(r.events) != 0 {
				t.Fatalf("rejected rotation emitted %d events", len(r.events))
			}
			e.requireSameSnapshot(before, c.name)
		})
	}
	// Other messages of the provider still work at the bound.
	e.mustOK(e.update(e.fresh(), updMsg("p1", "upd-at-bound", 5_000_000)), "update at the bound")
	e.mustOK(e.drain(e.fresh(), drainMsg(t, "p1", "key-last", MaxRetiredKeysPerProvider+1)), "drain at the bound")
}

// TestGenesisRetiredKeyCap: a genesis record may carry
// MaxRetiredKeysPerProvider retired keys and no more.
func TestGenesisRetiredKeyCap(t *testing.T) {
	if err := ValidateGenesis(&providersv1.GenesisState{Providers: []*providersv1.Provider{withRetired("p1", MaxRetiredKeysPerProvider)}}); err != nil {
		t.Fatalf("a record at the bound: %v", err)
	}
	over := withRetired("p1", MaxRetiredKeysPerProvider+1)
	err := ValidateGenesis(&providersv1.GenesisState{Providers: []*providersv1.Provider{over}})
	if err == nil || !strings.Contains(err.Error(), "retires more than") {
		t.Fatalf("a record above the bound: %v", err)
	}
	e := newEnv(t)
	before := e.snapshot()
	if err := e.k.InitGenesis(e.unmetered(), &providersv1.GenesisState{Providers: []*providersv1.Provider{over}}); err == nil {
		t.Fatal("InitGenesis imported a record above the bound")
	}
	e.requireSameSnapshot(before, "InitGenesis above the bound")
}

// TestStoredRecordAboveTheCap: a stored record whose key_generation claims
// more retired keys than the bound (only a corrupt store holds one) makes
// Query/Provider, Query/Providers, and export fail before they read its
// retired-key rows. Rows past the claimed count fail the read at the first
// extra row.
func TestStoredRecordAboveTheCap(t *testing.T) {
	for _, generation := range []uint64{0, MaxRetiredKeysPerProvider + 2, math.MaxUint64} {
		t.Run(fmt.Sprint(generation), func(t *testing.T) {
			e := newEnv(t)
			e.seed(withRetired("p0", 3))
			record := seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 0)
			record.KeyGeneration = generation
			e.k.save(e.unmetered(), record)
			ctx := e.unmetered()
			if _, err := e.k.QueryProvider(ctx, &providersv1.QueryProviderRequest{ProviderId: "p1"}); err == nil || !strings.Contains(err.Error(), "outside 1..") {
				t.Fatalf("Query/Provider: %v", err)
			}
			// p0 starts the page; p1 is the second record and fails the
			// count check before any of its rows is read.
			if _, err := e.k.QueryProviders(ctx, &providersv1.QueryProvidersRequest{}); err == nil || !strings.Contains(err.Error(), "outside 1..") {
				t.Fatalf("Query/Providers: %v", err)
			}
			if _, err := e.k.ExportGenesis(ctx); err == nil || !strings.Contains(err.Error(), "outside 1..") {
				t.Fatalf("export: %v", err)
			}
		})
	}
	t.Run("rows past the claimed count", func(t *testing.T) {
		e := newEnv(t)
		e.seed(withRetired("p1", 2))
		row := e.store(ModuleName).Get(RetiredKeyKey("p1", 1))
		for g := uint64(3); g <= 50; g++ {
			e.store(ModuleName).Set(RetiredKeyKey("p1", g), row)
		}
		ctx := e.unmetered()
		_, err := e.k.QueryProvider(ctx, &providersv1.QueryProviderRequest{ProviderId: "p1"})
		if err == nil || !strings.Contains(err.Error(), "past key_generation") {
			t.Fatalf("Query/Provider: %v", err)
		}
	})
}

// maxID is a 64-character id (the id pattern's maximum) that starts with
// prefix.
func maxID(prefix string) string {
	return prefix + strings.Repeat("z", msgkit.IDMaxBytes-len(prefix))
}

// worstCaseProvider is a valid record with every field at its largest
// encoding: 64-character ids, uint64 capacities, the last provider state,
// and retired keys retired at the latest allowed time.
func worstCaseProvider(index, retired int) *providersv1.Provider {
	id := maxID(fmt.Sprintf("p%03d-", index))
	p := &providersv1.Provider{
		ProviderId:             id,
		Account:                bytes.Repeat([]byte{0xff}, msgkit.AccountBytes),
		SigningPubkey:          testPub("key-" + id),
		SigningKeyId:           maxID(fmt.Sprintf("k%03d-", index)),
		KeyGeneration:          uint64(retired) + 1,
		EndpointManifestDigest: digestOf("endpoint/" + id),
		State:                  providersv1.ProviderState_PROVIDER_STATE_DISABLED,
		AdmittedCapacityBytes:  math.MaxUint64,
		ReservedBytes:          math.MaxUint64,
		OperatorGroupId:        maxID("g"),
	}
	for i := 1; i <= retired; i++ {
		p.PreviousSigningKeys = append(p.PreviousSigningKeys, &providersv1.PreviousSigningKey{
			PublicKey: testPub(fmt.Sprintf("%s-old-%d", id, i)), Generation: uint64(i), RetiredUnixSeconds: genesis.UnixSecondsMax,
		})
	}
	return p
}

// TestQueryResponseSizeBound: with every provider at the retired-key bound
// and every field at its largest encoding, Query/Provider stays within
// providerResponseBound, every Providers page at the protocol page cap
// (100) within providersPageBound, and every exported record within
// exportRecordBound; all three are far below the 4 MiB a default gRPC
// client accepts. Before the bound, one provider's response grew by about
// 45 bytes per rotation without limit.
func TestQueryResponseSizeBound(t *testing.T) {
	e := newEnv(t)
	var records []*providersv1.Provider
	const providers = 130 // more than one page at the cap of 100
	for i := 0; i < providers; i++ {
		// p000 and p110 are at the bound. The first page is the worst
		// case: 100 records, one of them with the whole retired-key budget.
		retired := 0
		if i == 0 || i == 110 {
			retired = MaxRetiredKeysPerProvider
		}
		records = append(records, worstCaseProvider(i, retired))
	}
	e.seed(records...)

	largest := 0
	for _, record := range records {
		res, err := e.k.QueryProvider(e.unmetered(), &providersv1.QueryProviderRequest{ProviderId: record.GetProviderId()})
		if err != nil {
			t.Fatal(err)
		}
		size := proto.Size(res)
		if size > providerResponseBound {
			t.Fatalf("Query/Provider %s is %d bytes, bound %d", record.GetProviderId(), size, providerResponseBound)
		}
		largest = max(largest, size)
	}
	t.Logf("largest Query/Provider response: %d bytes (bound %d)", largest, providerResponseBound)

	var key []byte
	pages, seen, largestPage := 0, 0, 0
	for {
		res, err := e.k.QueryProviders(e.unmetered(), &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{PageKey: key}})
		if err != nil {
			t.Fatal(err)
		}
		size := proto.Size(res)
		retired := 0
		for _, p := range res.GetProviders() {
			retired += len(p.GetPreviousSigningKeys())
		}
		if size > providersPageBound || retired > MaxRetiredKeysPerPage {
			t.Fatalf("page %d: %d bytes, %d retired keys (bounds %d, %d)", pages, size, retired, providersPageBound, MaxRetiredKeysPerPage)
		}
		largestPage = max(largestPage, size)
		seen += len(res.GetProviders())
		pages++
		key = res.GetPage().GetNextPageKey()
		if len(key) == 0 {
			break
		}
	}
	if seen != providers {
		t.Fatalf("the walk returned %d of %d providers", seen, providers)
	}
	t.Logf("largest Providers page: %d bytes over %d pages (bound %d)", largestPage, pages, providersPageBound)

	state, err := e.k.ExportGenesis(e.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	largestExport := 0
	for _, record := range state.GetProviders() {
		raw, err := exportJSON.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > exportRecordBound {
			t.Fatalf("exported %s is %d bytes, bound %d", record.GetProviderId(), len(raw), exportRecordBound)
		}
		largestExport = max(largestExport, len(raw))
	}
	t.Logf("largest exported record: %d bytes (bound %d)", largestExport, exportRecordBound)
	for _, bound := range []int{providerResponseBound, providersPageBound, exportRecordBound} {
		if bound >= grpcDefaultReceive {
			t.Fatalf("bound %d is not below the 4 MiB gRPC default", bound)
		}
	}
}
