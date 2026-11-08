package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
)

const leaseEventsGolden = "testdata/lease-events.golden.json"

// leaseEvent is one golden row: where the event was emitted (a committed
// transaction, or BeginBlock), its type, and its attributes without the
// SDK's trailing msg_index or mode.
type leaseEvent struct {
	Source     string       `json:"source"`
	Type       string       `json:"type"`
	Attributes []goldenAttr `json:"attributes"`
}

// isLeaseEvent reports whether ev is emitted by x/leases.
func isLeaseEvent(ev abci.Event) bool {
	return strings.HasPrefix(ev.Type, "svn.events.v1.Lease") || ev.Type == leases.EventOfferClosed || ev.Type == "leases_result"
}

// leaseEventRows collects the x/leases events of every committed block of
// c in execution order (BeginBlock events, then each transaction's events)
// and checks each one's schema:
//
//   - typed events parse with msgkit.ParseCommitted (transaction, trailing
//     msg_index 0) or msgkit.ParseBlockEvent (BeginBlock, trailing
//     mode=BeginBlock), attribute keys equal the proto field names in
//     order, uint64 values are canonical decimals, and LeaseEnded.state is
//     ENDED or EXPIRED;
//   - leases_offer_closed is offer_id, reason (EXPIRED in BeginBlock,
//     WITHDRAWN in a transaction), then msg_index or mode;
//   - leases_result is idempotent=true, then msg_index.
func leaseEventRows(t *testing.T, c *cluster) []leaseEvent {
	t.Helper()
	var rows []leaseEvent
	add := func(source string, ev abci.Event) {
		t.Helper()
		n := len(ev.Attributes)
		if n == 0 {
			t.Fatalf("%s %s has no attributes", source, ev.Type)
		}
		trailer := msgkit.MsgIndexKey
		if source == "BeginBlock" {
			trailer = msgkit.ModeKey
		}
		if ev.Attributes[n-1].Key != trailer {
			t.Fatalf("%s %s does not end with %s: %v", source, ev.Type, trailer, ev.Attributes)
		}
		row := leaseEvent{Source: source, Type: ev.Type}
		for _, attr := range ev.Attributes[:n-1] {
			row.Attributes = append(row.Attributes, goldenAttr{Key: attr.Key, Value: attr.Value})
		}
		switch ev.Type {
		case leases.EventOfferClosed:
			wantReason := leases.ReasonWithdrawn
			if source == "BeginBlock" {
				wantReason = leases.ReasonExpired
			}
			if n != 3 || ev.Attributes[0].Key != leases.AttrOfferID || !msgkit.ValidID(ev.Attributes[0].Value) ||
				ev.Attributes[1].Key != leases.AttrReason || ev.Attributes[1].Value != wantReason {
				t.Fatalf("%s %s attributes %v", source, ev.Type, ev.Attributes)
			}
		case "leases_result":
			if source != "tx" || n != 2 || ev.Attributes[0].Key != "idempotent" || ev.Attributes[0].Value != "true" {
				t.Fatalf("%s %s attributes %v", source, ev.Type, ev.Attributes)
			}
		default:
			var msg proto.Message
			var err error
			if source == "BeginBlock" {
				var mode string
				msg, mode, err = msgkit.ParseBlockEvent(ev)
				if err == nil && mode != "BeginBlock" {
					t.Fatalf("%s mode %s", ev.Type, mode)
				}
			} else {
				var index uint32
				msg, index, err = msgkit.ParseCommitted(ev)
				if err == nil && index != 0 {
					t.Fatalf("%s msg_index %d in a one-message transaction", ev.Type, index)
				}
			}
			if err != nil {
				t.Fatalf("%s %s: %v", source, ev.Type, err)
			}
			desc := msg.ProtoReflect().Descriptor()
			if string(desc.FullName()) != ev.Type || desc.Fields().Len() != n-1 {
				t.Fatalf("%s parsed as %s with %d fields for %d attributes", ev.Type, desc.FullName(), desc.Fields().Len(), n-1)
			}
			for i, attr := range ev.Attributes[:n-1] {
				fd := desc.Fields().Get(i)
				if attr.Key != string(fd.Name()) {
					t.Fatalf("%s attribute %d is %q, proto field %q", ev.Type, i, attr.Key, fd.Name())
				}
				switch {
				case fd.Kind() == protoreflect.Uint64Kind || fd.Kind() == protoreflect.Int64Kind:
					if !decimalText.MatchString(attr.Value) {
						t.Fatalf("%s.%s = %q is not a decimal", ev.Type, attr.Key, attr.Value)
					}
				case attr.Key == "package_bytes" || attr.Key == "released_bytes":
					if !decimalText.MatchString(attr.Value) {
						t.Fatalf("%s.%s = %q is not a decimal string", ev.Type, attr.Key, attr.Value)
					}
				case attr.Key == "state":
					if attr.Value != "ENDED" && attr.Value != "EXPIRED" {
						t.Fatalf("%s.state = %q", ev.Type, attr.Value)
					}
					if source == "BeginBlock" && attr.Value != "EXPIRED" {
						t.Fatalf("BeginBlock %s.state = %q", ev.Type, attr.Value)
					}
				case strings.HasSuffix(attr.Key, "_id"):
					if !msgkit.ValidID(attr.Value) {
						t.Fatalf("%s.%s = %q is not an id", ev.Type, attr.Key, attr.Value)
					}
				default:
					t.Fatalf("%s.%s has no schema rule", ev.Type, attr.Key)
				}
			}
		}
		rows = append(rows, row)
	}
	for _, block := range c.blocks {
		for _, ev := range block.Events {
			n := len(ev.Attributes)
			if isLeaseEvent(ev) {
				if n == 0 || ev.Attributes[n-1].Key != msgkit.ModeKey || ev.Attributes[n-1].Value != "BeginBlock" {
					t.Fatalf("block event %s outside BeginBlock: %v", ev.Type, ev.Attributes)
				}
				add("BeginBlock", ev)
			}
		}
		for _, res := range block.TxResults {
			for _, ev := range res.Events {
				if isLeaseEvent(ev) {
					add("tx", ev)
				}
			}
		}
	}
	return rows
}

// TestLeaseEventSchemas runs the lease flow and checks every x/leases
// event against its schema (leaseEventRows), the forbidden-name patterns
// of bounds.json, and the golden file chain/app/testdata/
// lease-events.golden.json (go test ./app -run TestLeaseEventSchemas
// -update rewrites it).
func TestLeaseEventSchemas(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runLeaseFlow(t, n, c)
	rows := leaseEventRows(t, c)

	counts := map[string]int{}
	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	for _, row := range rows {
		key := row.Source + " " + row.Type
		for _, attr := range row.Attributes {
			if attr.Key == "state" || attr.Key == "reason" {
				key += " " + attr.Value
			}
			if hits := msgkittest.NameHits(attr.Key, patterns, exemptions); len(hits) != 0 {
				t.Fatalf("%s key %s matches forbidden names %v", row.Type, attr.Key, hits)
			}
		}
		if hits := msgkittest.NameHits(row.Type, patterns, exemptions); len(hits) != 0 {
			t.Fatalf("event type %s matches forbidden names %v", row.Type, hits)
		}
		counts[key]++
	}
	want := map[string]int{
		"tx svn.events.v1.LeaseOffered":               11,
		"tx svn.events.v1.LeaseAccepted":              7,
		"tx svn.events.v1.LeaseRenewed":               2,
		"tx svn.events.v1.LeaseEnded ENDED":           3,
		"BeginBlock svn.events.v1.LeaseEnded EXPIRED": 4,
		"BeginBlock leases_offer_closed EXPIRED":      1,
		"tx leases_offer_closed WITHDRAWN":            2,
		"tx leases_result":                            1,
	}
	if len(counts) != len(want) {
		t.Fatalf("event counts %v, want %v", counts, want)
	}
	for key, count := range want {
		if counts[key] != count {
			t.Fatalf("event counts %v, want %v", counts, want)
		}
	}

	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(leaseEventsGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(leaseEventsGolden, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(leaseEventsGolden)
	if err != nil {
		t.Fatalf("%v (run go test ./app -run TestLeaseEventSchemas -update to create it)", err)
	}
	var stored []leaseEvent
	if err := json.Unmarshal(golden, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(rows) {
		t.Fatalf("golden has %d events, the flow emitted %d", len(stored), len(rows))
	}
	for i := range rows {
		if stored[i].Source != rows[i].Source || stored[i].Type != rows[i].Type || len(stored[i].Attributes) != len(rows[i].Attributes) {
			t.Fatalf("event %d: golden %v got %v", i, stored[i], rows[i])
		}
		for j := range rows[i].Attributes {
			if stored[i].Attributes[j] != rows[i].Attributes[j] {
				t.Fatalf("event %d attribute %d: golden %v got %v", i, j, stored[i].Attributes[j], rows[i].Attributes[j])
			}
		}
	}
}

// TestNoPrivateDataInLeaseStateOrEvents runs the lease flow and scans every
// store, every committed transaction, the genesis export of every module
// except authority and policy (App.Export refuses after council messages,
// DR-06-01, and the policy export refuses governance state, DR-10-04; their
// state is scanned through the dump of every store), and every event for the
// synthetic canaries (an endpoint URL, an email, a username, a watch
// event, a complaint narrative, media bytes) and URL schemes, and the
// leases export keys and every event key for the forbidden-name patterns.
// The flow hashes its canaries before it builds a transaction, so the
// canary scans check that this client submits only digests; the
// chain-side checks are the forbidden-name scans. Opaque ids and digests
// stay linkable by anyone holding their preimage.
func TestNoPrivateDataInLeaseStateOrEvents(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	runLeaseFlow(t, n, c)
	chainApp := c.app()
	list := canaries()
	web := [][]byte{[]byte("https://"), []byte("http://")}
	clean := func(label string, blob []byte) {
		t.Helper()
		if hit, found := msgkittest.FindCanary(blob, list...); found {
			t.Fatalf("%s holds %s", label, hit)
		}
		for _, w := range web {
			if bytes.Contains(blob, w) {
				t.Fatalf("%s holds %q", label, w)
			}
		}
	}
	if len(chainApp.ModuleSnapshot(app.ModuleLeases)) == 0 {
		t.Fatal("the leases store is empty")
	}
	for _, name := range append(app.SnapshotStores(), "acc") {
		clean("store "+name, chainApp.ModuleSnapshot(name))
	}
	for i, tx := range c.txs {
		clean(fmt.Sprintf("committed transaction %d", i), tx)
	}
	var names []string
	for _, name := range chainApp.ModuleNames() {
		if name != authority.ModuleName && name != app.ModulePolicy {
			names = append(names, name)
		}
	}
	if len(names) != 6 {
		t.Fatalf("modules %v", names)
	}
	exported, err := chainApp.ExportModulesForTest(names...)
	if err != nil {
		t.Fatal(err)
	}
	if len(exported) != len(names) {
		t.Fatalf("exported %d of %d modules", len(exported), len(names))
	}
	for _, name := range sortedNames(exported) {
		clean("export of "+name, exported[name])
	}
	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	var doc any
	if err := json.Unmarshal(exported[app.ModuleLeases], &doc); err != nil {
		t.Fatal(err)
	}
	keys := jsonKeys(doc)
	if len(keys) == 0 {
		t.Fatal("the leases export has no keys")
	}
	for _, key := range keys {
		if hits := msgkittest.NameHits(key, patterns, exemptions); len(hits) != 0 {
			t.Fatalf("leases export key %s matches %v", key, hits)
		}
	}
	scanned := 0
	for _, ev := range allEvents(c) {
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
		clean("event "+ev.Type, blob)
		if isLeaseEvent(ev) {
			scanned++
		}
	}
	if scanned == 0 {
		t.Fatal("the flow emitted no lease event")
	}
	golden, err := os.ReadFile(leaseEventsGolden)
	if err != nil {
		t.Fatal(err)
	}
	clean("lease event fixture", golden)
}
