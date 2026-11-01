package app_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
)

var updateGolden = flag.Bool("update", false, "rewrite chain/app/testdata golden files")

const eventsGolden = "testdata/registry-events.golden.json"

type goldenAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type goldenEvent struct {
	Type       string       `json:"type"`
	Attributes []goldenAttr `json:"attributes"`
}

var (
	decimalText = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	sha256Text  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// allEvents returns every event of every committed block in order: the tx
// results first, then the block events.
func allEvents(c *cluster) []abci.Event {
	var out []abci.Event
	for _, block := range c.blocks {
		for _, res := range block.TxResults {
			out = append(out, res.Events...)
		}
		out = append(out, block.Events...)
	}
	return out
}

func TestRegistryEventSchemas(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runRegistryFlow(t, n, c)

	var got []goldenEvent
	counts := map[string]int{}
	for _, committed := range typedEvents(allEvents(c)) {
		ev := withoutMsgIndex(t, committed)
		msg, err := msgkit.ParseTyped(ev)
		if err != nil {
			t.Fatalf("%s: %v", ev.Type, err)
		}
		desc := msg.ProtoReflect().Descriptor()
		if string(desc.FullName()) != ev.Type {
			t.Fatalf("event type %s parsed as %s", ev.Type, desc.FullName())
		}
		fields := desc.Fields()
		if fields.Len() != len(ev.Attributes) {
			t.Fatalf("%s: %d attributes for %d proto fields", ev.Type, len(ev.Attributes), fields.Len())
		}
		row := goldenEvent{Type: ev.Type}
		for i, attr := range ev.Attributes {
			fd := fields.Get(i)
			if attr.Key != string(fd.Name()) {
				t.Fatalf("%s attribute %d is %q, proto field %q", ev.Type, i, attr.Key, fd.Name())
			}
			switch fd.Kind() {
			case protoreflect.Uint64Kind, protoreflect.Uint32Kind:
				if !decimalText.MatchString(attr.Value) {
					t.Fatalf("%s.%s = %q is not a decimal", ev.Type, attr.Key, attr.Value)
				}
			case protoreflect.BoolKind:
				if attr.Value != "true" && attr.Value != "false" {
					t.Fatalf("%s.%s = %q is not a bool", ev.Type, attr.Key, attr.Value)
				}
			case protoreflect.StringKind:
				if attr.Key == "manifest_sha256" && !sha256Text.MatchString(attr.Value) {
					t.Fatalf("%s.%s = %q is not 64 lowercase hex", ev.Type, attr.Key, attr.Value)
				}
				if attr.Key == "state" && attr.Value != "APPLIED" && attr.Value != "ADMITTED" && attr.Value != "DRAINING" && attr.Value != "DISABLED" {
					t.Fatalf("%s.state = %q", ev.Type, attr.Value)
				}
			default:
				t.Fatalf("%s.%s has kind %s", ev.Type, attr.Key, fd.Kind())
			}
			row.Attributes = append(row.Attributes, goldenAttr{Key: attr.Key, Value: attr.Value})
		}
		counts[ev.Type]++
		got = append(got, row)
	}
	want := map[string]int{
		string(proto.MessageName(&eventsv1.ProviderRegistered{})):   2,
		string(proto.MessageName(&eventsv1.ProviderStateChanged{})): 5,
		string(proto.MessageName(&eventsv1.ContentRegistered{})):    2,
		string(proto.MessageName(&eventsv1.ContentWithdrawn{})):     2,
	}
	if len(counts) != len(want) {
		t.Fatalf("event counts %v", counts)
	}
	for name, count := range want {
		if counts[name] != count {
			t.Fatalf("event counts %v want %v", counts, want)
		}
	}

	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(eventsGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(eventsGolden, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(eventsGolden)
	if err != nil {
		t.Fatalf("%v (run go test ./app -run TestRegistryEventSchemas -update to create it)", err)
	}
	var stored []goldenEvent
	if err := json.Unmarshal(golden, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(got) {
		t.Fatalf("golden has %d events, the flow emitted %d", len(stored), len(got))
	}
	for i := range got {
		if stored[i].Type != got[i].Type || len(stored[i].Attributes) != len(got[i].Attributes) {
			t.Fatalf("event %d: golden %v got %v", i, stored[i], got[i])
		}
		for j := range got[i].Attributes {
			if stored[i].Attributes[j] != got[i].Attributes[j] {
				t.Fatalf("event %d attribute %d: golden %v got %v", i, j, stored[i].Attributes[j], got[i].Attributes[j])
			}
		}
	}
}

// withoutMsgIndex removes the msg_index attribute baseapp appends to every
// event a message emits. Every registry transaction here has one message,
// so the value must be "0" and it must be the last attribute. The typed
// schema itself has no msg_index field.
func withoutMsgIndex(t *testing.T, ev abci.Event) abci.Event {
	t.Helper()
	n := len(ev.Attributes)
	if n == 0 || ev.Attributes[n-1].Key != "msg_index" || ev.Attributes[n-1].Value != "0" {
		t.Fatalf("%s: committed event has no trailing msg_index=0: %v", ev.Type, ev.Attributes)
	}
	out := ev
	out.Attributes = append([]abci.EventAttribute(nil), ev.Attributes[:n-1]...)
	return out
}

// sdkOwnedNames are SDK ante and message event keys that contain a
// forbidden name pattern. They are framework attributes, not registry
// schema; the test pins their values (no fee is ever charged).
var sdkOwnedNames = map[string]bool{"fee": true, "fee_payer": true}

func TestNoPrivateDataInStateOrEvents(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runRegistryFlow(t, n, c)
	chainApp := c.app()
	list := canaries()
	web := [][]byte{[]byte("https://"), []byte("http://")}

	stores := append(app.SnapshotStores(), "acc")
	for _, name := range stores {
		blob := chainApp.ModuleSnapshot(name)
		if blob == nil && name != app.ModuleLeases && name != app.ModuleEvidence {
			t.Fatalf("store %s has no snapshot", name)
		}
		if hit, found := msgkittest.FindCanary(blob, list...); found {
			t.Fatalf("store %s holds %s", name, hit)
		}
		for _, w := range web {
			if bytes.Contains(blob, w) {
				t.Fatalf("store %s holds %q", name, w)
			}
		}
	}

	exported, err := chainApp.Export()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedNames(exported) {
		if hit, found := msgkittest.FindCanary(exported[name], list...); found {
			t.Fatalf("export of %s holds %s", name, hit)
		}
	}

	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	events := allEvents(c)
	if len(typedEvents(events)) == 0 {
		t.Fatal("the flow emitted no typed events")
	}
	for _, ev := range events {
		var blob []byte
		blob = append(blob, ev.Type...)
		for _, attr := range ev.Attributes {
			blob = append(blob, attr.Key...)
			blob = append(blob, 0)
			blob = append(blob, attr.Value...)
			blob = append(blob, 0)
			hits := msgkittest.NameHits(attr.Key, patterns, exemptions)
			if len(hits) == 0 {
				continue
			}
			if ev.Type == sdk.EventTypeTx && sdkOwnedNames[attr.Key] {
				if attr.Key == "fee" && attr.Value != "" {
					t.Fatalf("tx event fee = %q", attr.Value)
				}
				continue
			}
			t.Fatalf("event %s key %s matches forbidden names %v", ev.Type, attr.Key, hits)
		}
		if hits := msgkittest.NameHits(ev.Type, patterns, exemptions); len(hits) != 0 {
			t.Fatalf("event type %s matches forbidden names %v", ev.Type, hits)
		}
		if hit, found := msgkittest.FindCanary(blob, list...); found {
			t.Fatalf("event %s holds %s", ev.Type, hit)
		}
		for _, w := range web {
			if bytes.Contains(blob, w) {
				t.Fatalf("event %s holds %q", ev.Type, w)
			}
		}
	}

	golden, err := os.ReadFile(eventsGolden)
	if err != nil {
		t.Fatal(err)
	}
	if hit, found := msgkittest.FindCanary(golden, list...); found {
		t.Fatalf("event fixture holds %s", hit)
	}
	var fixture []goldenEvent
	if err := json.Unmarshal(golden, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, ev := range fixture {
		for _, attr := range ev.Attributes {
			if hits := msgkittest.NameHits(attr.Key, patterns, exemptions); len(hits) != 0 {
				t.Fatalf("fixture key %s matches %v", attr.Key, hits)
			}
		}
	}
	for _, name := range []string{app.ModuleProviders, app.ModuleContent} {
		var doc any
		if err := json.Unmarshal(exported[name], &doc); err != nil {
			t.Fatal(err)
		}
		for _, key := range jsonKeys(doc) {
			if hits := msgkittest.NameHits(key, patterns, exemptions); len(hits) != 0 {
				t.Fatalf("%s export key %s matches %v", name, key, hits)
			}
		}
	}
}

// jsonKeys returns every object key in a decoded JSON value.
func jsonKeys(v any) []string {
	var out []string
	switch typed := v.(type) {
	case map[string]any:
		for _, key := range sortedNames(typed) {
			out = append(out, key)
			out = append(out, jsonKeys(typed[key])...)
		}
	case []any:
		for _, item := range typed {
			out = append(out, jsonKeys(item)...)
		}
	}
	return out
}
