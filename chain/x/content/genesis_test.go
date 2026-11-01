package content

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

type recorder struct{ names []string }

func (r *recorder) Record(name string) { r.names = append(r.names, name) }

// sampleGenesis is a valid catalogue: c-a PENDING, c-b WITHDRAWN with its
// tombstone, in key order.
func sampleGenesis() *contentv1.GenesisState {
	return &contentv1.GenesisState{
		Contents: []*contentv1.Content{
			{ContentId: "c-a", ContentVersion: 1, ManifestDigest: digestOf(manifestDigest("c-a")), PackageBytes: 10, SubmittingHost: serviceAddr, State: contentv1.ContentState_CONTENT_STATE_PENDING},
			{ContentId: "c-b", ContentVersion: 1, ManifestDigest: digestOf(manifestDigest("c-b")), PackageBytes: genesis.MaxPackageBytes, SubmittingHost: service2Addr, State: contentv1.ContentState_CONTENT_STATE_WITHDRAWN},
		},
		Tombstones: []*contentv1.Tombstone{
			{ContentId: "c-b", ContentVersion: 1, CommittedUnixSeconds: 1_767_225_600, ReasonDigest: digestOf(reasonDigest("c-b")), RetentionDecisionId: "DEC-004"},
		},
	}
}

func TestValidateGenesisAcceptsEmptyAndSample(t *testing.T) {
	if err := ValidateGenesis(&contentv1.GenesisState{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGenesis(sampleGenesis()); err != nil {
		t.Fatal(err)
	}
	edge := sampleGenesis()
	edge.Contents[0].ContentId = strings.Repeat("z", 64)
	edge.Contents[0].PackageBytes = 1
	edge.Tombstones[0].CommittedUnixSeconds = genesis.UnixSecondsMax
	if err := ValidateGenesis(edge); err != nil {
		t.Fatal(err)
	}
	edge.Tombstones[0].CommittedUnixSeconds = genesis.UnixSecondsMin
	if err := ValidateGenesis(edge); err != nil {
		t.Fatal(err)
	}
}

// TestValidateGenesisRejects covers each rule of ValidateGenesis.
func TestValidateGenesisRejects(t *testing.T) {
	cases := []struct {
		name string
		edit func(g *contentv1.GenesisState)
		want string
	}{
		{"nil-row", func(g *contentv1.GenesisState) { g.Contents = append(g.Contents, nil) }, "nil row"},
		{"empty-id", func(g *contentv1.GenesisState) { g.Contents[0].ContentId = "" }, "content_id"},
		{"id-pattern", func(g *contentv1.GenesisState) { g.Contents[0].ContentId = "C-A" }, "content_id"},
		{"id-65", func(g *contentv1.GenesisState) { g.Contents[0].ContentId = strings.Repeat("a", 65) }, "content_id"},
		{"repeated-id", func(g *contentv1.GenesisState) {
			g.Contents = append(g.Contents, proto.Clone(g.Contents[0]).(*contentv1.Content))
		}, "repeated"},
		{"version-0", func(g *contentv1.GenesisState) { g.Contents[0].ContentVersion = 0 }, "content_version"},
		{"version-2", func(g *contentv1.GenesisState) { g.Contents[0].ContentVersion = 2 }, "content_version"},
		{"digest-nil", func(g *contentv1.GenesisState) { g.Contents[0].ManifestDigest = nil }, "manifest_digest"},
		{"digest-algorithm", func(g *contentv1.GenesisState) { g.Contents[0].ManifestDigest.Algorithm = 0 }, "manifest_digest"},
		{"digest-31", func(g *contentv1.GenesisState) { g.Contents[0].ManifestDigest.Value = make([]byte, 31) }, "manifest_digest"},
		{"package-0", func(g *contentv1.GenesisState) { g.Contents[0].PackageBytes = 0 }, "package_bytes"},
		{"package-cap-plus-1", func(g *contentv1.GenesisState) { g.Contents[0].PackageBytes = genesis.MaxPackageBytes + 1 }, "package_bytes"},
		{"host-19", func(g *contentv1.GenesisState) { g.Contents[0].SubmittingHost = make([]byte, 19) }, "submitting_host"},
		{"host-empty", func(g *contentv1.GenesisState) { g.Contents[0].SubmittingHost = nil }, "submitting_host"},
		{"state-unspecified", func(g *contentv1.GenesisState) { g.Contents[0].State = 0 }, "state"},
		{"state-reserved-3", func(g *contentv1.GenesisState) { g.Contents[0].State = 3 }, "state"},
		{"withdrawn-without-tombstone", func(g *contentv1.GenesisState) { g.Tombstones = nil }, "no tombstone"},
		{"tombstone-for-pending", func(g *contentv1.GenesisState) {
			extra := proto.Clone(g.Tombstones[0]).(*contentv1.Tombstone)
			extra.ContentId = "c-a"
			g.Tombstones = append(g.Tombstones, extra)
		}, "no WITHDRAWN content"},
		{"tombstone-for-absent", func(g *contentv1.GenesisState) { g.Tombstones[0].ContentId = "c-z" }, "no WITHDRAWN content"},
		{"tombstone-version-mismatch", func(g *contentv1.GenesisState) { g.Tombstones[0].ContentVersion = 2 }, "no WITHDRAWN content"},
		{"second-tombstone", func(g *contentv1.GenesisState) {
			g.Tombstones = append(g.Tombstones, proto.Clone(g.Tombstones[0]).(*contentv1.Tombstone))
		}, "second tombstone"},
		{"nil-tombstone", func(g *contentv1.GenesisState) { g.Tombstones = append(g.Tombstones, nil) }, "nil tombstone"},
		{"tombstone-id-pattern", func(g *contentv1.GenesisState) { g.Tombstones[0].ContentId = "C-B" }, "content_id"},
		{"tombstone-time-0", func(g *contentv1.GenesisState) { g.Tombstones[0].CommittedUnixSeconds = 0 }, "committed_unix_seconds"},
		{"tombstone-time-max-plus-1", func(g *contentv1.GenesisState) {
			g.Tombstones[0].CommittedUnixSeconds = genesis.UnixSecondsMax + 1
		}, "committed_unix_seconds"},
		{"tombstone-reason-nil", func(g *contentv1.GenesisState) { g.Tombstones[0].ReasonDigest = nil }, "reason_digest"},
		{"tombstone-reason-33", func(g *contentv1.GenesisState) { g.Tombstones[0].ReasonDigest.Value = make([]byte, 33) }, "reason_digest"},
		{"history-erased", func(g *contentv1.GenesisState) { g.Tombstones[0].HistoryErased = true }, "history_erased"},
		{"retention-decision", func(g *contentv1.GenesisState) { g.Tombstones[0].RetentionDecisionId = "DEC-005" }, "retention_decision_id"},
		{"retention-version", func(g *contentv1.GenesisState) { g.Tombstones[0].RetentionValueVersion = 1 }, "retention_value_version"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := sampleGenesis()
			c.edit(g)
			err := ValidateGenesis(g)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not name %q", err, c.want)
			}
			f := newFixture(t)
			before := f.snapshot()
			if f.keeper.InitGenesis(f.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), g) == nil {
				t.Fatal("InitGenesis accepted an invalid catalogue")
			}
			requireSame(t, before, f.snapshot(), "invalid genesis")
		})
	}
	if ValidateGenesis(nil) == nil {
		t.Fatal("nil state accepted")
	}
}

// TestGenesisRoundTrip: committed state from real transactions exports a
// valid catalogue; importing it into a fresh store gives byte-identical
// record prefixes (the business prefix is not in the v0 genesis schema,
// DR-07-03), and a second export is proto-equal to the first.
func TestGenesisRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c2", "c2")
	f.mustRegister("reg-c10", "c10")
	f.mustRegister("reg-c1", "c1")
	f.mustWithdraw("wd-c10", "c10")
	if code := f.keeper.ForceWithdrawByCouncil(f.tx(), "c2", 1, reasonDigest("council"), true); code != msgkit.OK {
		t.Fatal(code)
	}
	exported := f.keeper.ExportGenesis(f.ctx)
	if err := ValidateGenesis(exported); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(exported.GetContents()))
	for _, c := range exported.GetContents() {
		ids = append(ids, c.GetContentId())
	}
	if strings.Join(ids, ",") != "c1,c10,c2" {
		t.Fatalf("export order %v, want key order c1,c10,c2", ids)
	}
	if len(exported.GetTombstones()) != 2 {
		t.Fatalf("%d tombstones", len(exported.GetTombstones()))
	}

	raw, err := MarshalGenesis(exported)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseGenesis(raw)
	if err != nil {
		t.Fatal(err)
	}
	g := newFixture(t)
	am := NewAppModule(g.keeper, &recorder{})
	if err := am.ValidateGenesis(nil, nil, raw); err != nil {
		t.Fatal(err)
	}
	am.InitGenesis(g.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), nil, raw)
	if !proto.Equal(g.keeper.ExportGenesis(g.ctx), exported) || !proto.Equal(parsed, exported) {
		t.Fatal("re-export differs from export")
	}
	if !bytes.Equal(recordPrefixes(f), recordPrefixes(g)) {
		t.Fatal("imported record prefixes differ from the committed store")
	}
	again, err := ParseGenesis(am.ExportGenesis(g.ctx, nil))
	if err != nil || !proto.Equal(again, exported) {
		t.Fatalf("module JSON export differs: %v", err)
	}
}

// recordPrefixes is the content and tombstone prefixes of the store.
func recordPrefixes(f *fixture) []byte {
	store := f.ctx.MultiStore().GetKVStore(f.keys[ModuleName])
	var out []byte
	for _, prefix := range []byte{ContentPrefix, TombstonePrefix} {
		it := store.Iterator([]byte{prefix}, []byte{prefix + 1})
		for ; it.Valid(); it.Next() {
			out = append(out, it.Key()...)
			out = append(out, 0xff)
			out = append(out, it.Value()...)
			out = append(out, 0xfe)
		}
		it.Close()
	}
	return out
}

// TestInitGenesisSortsAndRecords: input order does not matter, the export
// is in key order, and the init order recorder sees the module.
func TestInitGenesisSortsAndRecords(t *testing.T) {
	g := sampleGenesis()
	g.Contents[0], g.Contents[1] = g.Contents[1], g.Contents[0]
	raw, err := MarshalGenesis(g)
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t)
	rec := &recorder{}
	NewAppModule(f.keeper, rec).InitGenesis(f.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), nil, raw)
	if len(rec.names) != 1 || rec.names[0] != ModuleName {
		t.Fatalf("recorded %v", rec.names)
	}
	want := sampleGenesis()
	sort.Slice(want.Contents, func(i, j int) bool { return want.Contents[i].GetContentId() < want.Contents[j].GetContentId() })
	if !proto.Equal(f.keeper.ExportGenesis(f.ctx), want) {
		t.Fatal("export is not in key order")
	}
	// A nil recorder is allowed.
	h := newFixture(t)
	NewAppModule(h.keeper, nil).InitGenesis(h.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), nil, raw)
}

// TestGenesisJSONIsStrict: unknown fields, wrong types, and invalid
// catalogues fail module validation; InitGenesis panics on them.
func TestGenesisJSONIsStrict(t *testing.T) {
	am := NewAppModule(newFixture(t).keeper, nil)
	def := am.DefaultGenesis(nil)
	if err := am.ValidateGenesis(nil, nil, def); err != nil {
		t.Fatalf("default genesis: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(def, &keys); err != nil || len(keys) != 2 {
		t.Fatalf("default genesis %s", def)
	}
	for _, bad := range []string{
		`{"contents":[],"tombstones":[],"extra":1}`,
		`{"contents":[{"content_id":"c1","video_bytes":"AAAA"}]}`,
		`{"contents":"x"}`,
		`not json`,
		`{"contents":[{"content_id":"c1","content_version":"1","package_bytes":"1","state":"CONTENT_STATE_PENDING"}]}`,
		`{"contents":[{"content_id":"c1","content_version":"1","manifest_digest":{"algorithm":"DIGEST_ALGORITHM_SHA256","value":""},"package_bytes":"1","submitting_host":"","state":"CONTENT_STATE_PUBLISHED"}]}`,
	} {
		if err := am.ValidateGenesis(nil, nil, json.RawMessage(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("InitGenesis accepted %s", bad)
				}
			}()
			f := newFixture(t)
			NewAppModule(f.keeper, nil).InitGenesis(f.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), nil, json.RawMessage(bad))
		}()
	}
}

// TestGenesisDigestAlgorithmOnly: genesis stores the digest value under the
// SHA-256 algorithm only; an unknown-field-free import keeps the values.
func TestGenesisDigestAlgorithmOnly(t *testing.T) {
	g := sampleGenesis()
	g.Contents[0].ManifestDigest.Algorithm = typesv1.DigestAlgorithm(99)
	if ValidateGenesis(g) == nil {
		t.Fatal("unknown digest algorithm accepted")
	}
}
