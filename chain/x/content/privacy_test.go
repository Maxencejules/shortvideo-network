package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// Synthetic off-chain inputs. Only their digests may reach the chain.
var (
	canaryEndpoint  = []byte("https://canary-endpoint.invalid/packages/c-canary/manifest")
	canaryEmail     = []byte("creator-canary@example.invalid")
	canaryUsername  = []byte("canary_user_7f3a")
	canaryComplaint = []byte("CANARY complaint narrative: synthetic moderation text")
	canaryWatch     = []byte("CANARY-WATCH-EVENT viewer watched c-canary for 31s")
	canaryMedia     = bytes.Repeat([]byte("CANARY-MEDIA-PAYLOAD-0123456789abcdef"), 4096/37+1)
)

// TestContentStoresOnlyDigests registers, withdraws, and force-withdraws
// content whose off-chain manifest and complaint carry canaries, then
// scans every stored key and value, every event, and the exported genesis
// JSON for each canary in raw, hex, and base64 form and for URL schemes.
// It also checks every event key and genesis JSON key against the
// bounds.json forbidden name patterns.
func TestContentStoresOnlyDigests(t *testing.T) {
	manifest, err := json.Marshal(map[string]any{
		"content_id": "c-canary",
		"creator":    string(canaryEmail),
		"handle":     string(canaryUsername),
		"endpoint":   string(canaryEndpoint),
		"payload":    string(canaryMedia),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifest)
	complaintSum := sha256.Sum256(append(append([]byte(nil), canaryComplaint...), canaryWatch...))

	f := newFixture(t)
	var events []abci.Event
	for _, id := range []string{"c-canary", "c-canary-2"} {
		msg := f.registerMsg("reg-"+id, id)
		msg.ManifestDigest = digestOf(manifestSum[:])
		f.signRegister(msg, DevnetRoleName, f.intent)
		ctx, err := f.register(msg)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, ctx.EventManager().ABCIEvents()...)
	}
	wd := f.withdrawMsg("wd-c-canary", "c-canary")
	wd.ReasonDigest = digestOf(complaintSum[:])
	f.signWithdraw(wd, DevnetRoleName, f.intent)
	ctx, err := f.withdraw(wd)
	if err != nil {
		t.Fatal(err)
	}
	events = append(events, ctx.EventManager().ABCIEvents()...)
	ctx, code := f.force("c-canary-2", 1, complaintSum[:], true)
	if code != msgkit.OK {
		t.Fatal(code)
	}
	events = append(events, ctx.EventManager().ABCIEvents()...)

	canaries := [][]byte{canaryEndpoint, canaryEmail, canaryUsername, canaryComplaint, canaryWatch, canaryMedia[:64], manifest[:48]}
	scan := func(what string, blob []byte) {
		t.Helper()
		if form, found := msgkittest.FindCanary(blob, canaries...); found {
			t.Fatalf("%s holds %s", what, form)
		}
		if bytes.Contains(blob, []byte("https://")) || bytes.Contains(blob, []byte("http://")) {
			t.Fatalf("%s holds a URL scheme", what)
		}
	}
	scan("content store", f.snapshot())

	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	checkName := func(where, name string) {
		t.Helper()
		if hits := msgkittest.NameHits(name, patterns, exemptions); len(hits) != 0 {
			t.Fatalf("%s name %q matches forbidden patterns %v", where, name, hits)
		}
	}
	if len(events) == 0 {
		t.Fatal("no events to scan")
	}
	for _, ev := range events {
		scan("event "+ev.Type, []byte(ev.Type))
		checkName("event type", strings.TrimPrefix(ev.Type, "svn.events.v1."))
		for _, attr := range ev.Attributes {
			scan("event "+ev.Type+" key", []byte(attr.Key))
			scan("event "+ev.Type+" value", []byte(attr.Value))
			checkName("event attribute", attr.Key)
		}
	}

	raw, err := MarshalGenesis(f.keeper.ExportGenesis(f.ctx))
	if err != nil {
		t.Fatal(err)
	}
	scan("exported genesis", raw)
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	collectKeys(doc, keys)
	if len(keys) == 0 {
		t.Fatal("no genesis keys to scan")
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		checkName("genesis key", k)
	}
}

func collectKeys(v any, into map[string]bool) {
	switch typed := v.(type) {
	case map[string]any:
		for k, child := range typed {
			into[k] = true
			collectKeys(child, into)
		}
	case []any:
		for _, child := range typed {
			collectKeys(child, into)
		}
	}
}

// TestStateSchemaBytesFieldsAreBounded: every bytes field of the stored
// messages is a fixed-size value the handlers and genesis check (a digest,
// an account), so no stored field can carry a payload.
func TestStateSchemaBytesFieldsAreBounded(t *testing.T) {
	g := sampleGenesis()
	g.Contents[0].SubmittingHost = make([]byte, 21)
	if ValidateGenesis(g) == nil {
		t.Fatal("21-byte submitting host accepted")
	}
	g = sampleGenesis()
	g.Contents[0].ManifestDigest.Value = make([]byte, 1024)
	if ValidateGenesis(g) == nil {
		t.Fatal("1 KiB manifest digest accepted")
	}
	g = sampleGenesis()
	g.Tombstones[0].ReasonDigest.Value = make([]byte, 1024)
	if ValidateGenesis(g) == nil {
		t.Fatal("1 KiB reason digest accepted")
	}
	f := newFixture(t)
	msg := f.registerMsg("reg-big", "c-big")
	msg.ManifestDigest.Value = make([]byte, 1024)
	before := f.snapshot()
	_, err := f.register(msg)
	requireCode(t, err, msgkit.Bounds)
	requireSame(t, before, f.snapshot(), "1 KiB digest")
	// Content and Tombstone: the only bytes field is submitting_host (20
	// bytes); every message field is an svn.types.v1.Digest (32 bytes).
	assertBoundedFields(t, (&contentv1.Content{}).ProtoReflect().Descriptor(), "submitting_host")
	assertBoundedFields(t, (&contentv1.Tombstone{}).ProtoReflect().Descriptor())
}

func assertBoundedFields(t *testing.T, md protoreflect.MessageDescriptor, bytesFields ...string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, name := range bytesFields {
		allowed[name] = true
	}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.IsList() || fd.IsMap() {
			t.Fatalf("%s.%s is repeated", md.FullName(), fd.Name())
		}
		switch fd.Kind() {
		case protoreflect.BytesKind:
			if !allowed[string(fd.Name())] {
				t.Fatalf("%s.%s is an unchecked bytes field", md.FullName(), fd.Name())
			}
		case protoreflect.MessageKind:
			if fd.Message().FullName() != "svn.types.v1.Digest" {
				t.Fatalf("%s.%s is a %s", md.FullName(), fd.Name(), fd.Message().FullName())
			}
		}
	}
}
