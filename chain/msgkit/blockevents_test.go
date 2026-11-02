package msgkit

import (
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/cosmos/cosmos-sdk/baseapp"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
)

// withMode returns ev with a trailing mode attribute, the way baseapp
// commits a BeginBlock or EndBlock event.
func withMode(ev abci.Event, mode string) abci.Event {
	out := abci.Event{Type: ev.Type, Attributes: append([]abci.EventAttribute(nil), ev.Attributes...)}
	out.Attributes = append(out.Attributes, abci.EventAttribute{Key: ModeKey, Value: mode, Index: true})
	return out
}

// TestNoEventFieldIsNamedLikeAnSDKSuffix: no svn.events.v1 field is named
// mode or msg_index. ParseBlockEvent and ParseCommitted strip the last
// attribute by key, which is unambiguous only while no schema field has one
// of those names.
func TestNoEventFieldIsNamedLikeAnSDKSuffix(t *testing.T) {
	for _, name := range EventTypeNames() {
		for _, fd := range eventTypes[protoreflect.FullName(name)].fields {
			if string(fd.Name()) == ModeKey || string(fd.Name()) == MsgIndexKey {
				t.Fatalf("%s has a field named %s", name, fd.Name())
			}
		}
	}
}

// TestParseBlockEvent: a block event is the EmitTyped attributes and then
// the SDK's mode attribute. ParseBlockEvent returns the typed message and
// the mode for every allowlisted type in both modes, and rejects an event
// without a trailing mode of BeginBlock or EndBlock.
func TestParseBlockEvent(t *testing.T) {
	for _, name := range EventTypeNames() {
		spec := eventTypes[protoreflect.FullName(name)]
		for _, msg := range []proto.Message{filled(t, name), spec.messageType.New().Interface()} {
			event, err := typedToEvent(msg)
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"BeginBlock", "EndBlock"} {
				parsed, gotMode, err := ParseBlockEvent(withMode(event, mode))
				if err != nil {
					t.Fatalf("%s %s: %v", name, mode, err)
				}
				if gotMode != mode || !proto.Equal(parsed, msg) || parsed.ProtoReflect().Descriptor() != msg.ProtoReflect().Descriptor() {
					t.Fatalf("%s %s: parsed %v mode %q", name, mode, parsed, gotMode)
				}
			}
		}
	}

	const ended = "svn.events.v1.LeaseEnded"
	typed := attrs("lease_id", "l1", "state", "EXPIRED", "released_bytes", "1024")
	block := func(extra ...string) abci.Event {
		return abci.Event{Type: ended, Attributes: append(append([]abci.EventAttribute(nil), typed...), attrs(extra...)...)}
	}
	msg, mode, err := ParseBlockEvent(block(ModeKey, "BeginBlock"))
	if err != nil {
		t.Fatal(err)
	}
	if mode != "BeginBlock" || !proto.Equal(msg, &eventsv1.LeaseEnded{LeaseId: "l1", State: "EXPIRED", ReleasedBytes: "1024"}) {
		t.Fatalf("parsed %v mode %q", msg, mode)
	}
	// The Index flag is not part of the schema; a node may clear it.
	unindexed := block(ModeKey, "EndBlock")
	for i := range unindexed.Attributes {
		unindexed.Attributes[i].Index = false
	}
	if _, mode, err := ParseBlockEvent(unindexed); err != nil || mode != "EndBlock" {
		t.Fatalf("unindexed: %q %v", mode, err)
	}
	// The other parsers do not accept a block event.
	if _, err := ParseTyped(block(ModeKey, "BeginBlock")); err == nil {
		t.Fatal("ParseTyped accepted a block event; mode is not schema")
	}
	if _, _, err := ParseCommitted(block(ModeKey, "BeginBlock")); err == nil {
		t.Fatal("ParseCommitted accepted a block event")
	}

	cases := map[string]abci.Event{
		"no mode":                 block(),
		"mode first":              {Type: ended, Attributes: append(attrs(ModeKey, "BeginBlock"), typed...)},
		"mode in the middle":      {Type: ended, Attributes: append(append(attrs("lease_id", "l1"), attrs(ModeKey, "BeginBlock")...), attrs("state", "EXPIRED", "released_bytes", "1024")...)},
		"unknown mode value":      block(ModeKey, "FinalizeBlock"),
		"lower-case mode value":   block(ModeKey, "beginblock"),
		"mode value with space":   block(ModeKey, "BeginBlock "),
		"empty mode value":        block(ModeKey, ""),
		"msg_index instead":       block(MsgIndexKey, "0"),
		"msg_index after mode":    block(ModeKey, "BeginBlock", MsgIndexKey, "0"),
		"mode after msg_index":    block(MsgIndexKey, "0", ModeKey, "BeginBlock"),
		"attribute after mode":    block(ModeKey, "EndBlock", "extra", "x"),
		"attribute before mode":   block("extra", "x", ModeKey, "EndBlock"),
		"two modes":               block(ModeKey, "BeginBlock", ModeKey, "BeginBlock"),
		"typed part invalid":      {Type: ended, Attributes: attrs("lease_id", "l1", ModeKey, "BeginBlock")},
		"typed value invalid":     {Type: "svn.events.v1.LeaseRenewed", Attributes: attrs("lease_id", "l1", "term_version", "02", "deadline_unix_seconds", "5", ModeKey, "EndBlock")},
		"not an svn.events type":  {Type: "message", Attributes: attrs("action", "x", ModeKey, "BeginBlock")},
		"leases_offer_closed":     {Type: "leases_offer_closed", Attributes: attrs("offer_id", "o1", "reason", "EXPIRED", ModeKey, "BeginBlock")},
		"no attributes":           {Type: ended},
		"mode only, no type":      {Type: "", Attributes: attrs(ModeKey, "BeginBlock")},
		"mode only, a typed type": {Type: "svn.events.v1.ContentWithdrawn", Attributes: attrs(ModeKey, "BeginBlock")},
	}
	for name, ev := range cases {
		if msg, mode, err := ParseBlockEvent(ev); err == nil || msg != nil || mode != "" {
			t.Errorf("%s: parsed as %v mode %q (err %v)", name, msg, mode, err)
		}
	}
}

// TestParseBlockEventOnFinalizeBlock: the suffix ParseBlockEvent expects is
// the one SDK v0.55 baseapp actually commits. A real BaseApp over an
// in-memory database runs a BeginBlocker and an EndBlocker that each emit
// one typed event through EmitTyped on a fresh event manager, as the SDK
// module manager does (types/module Manager.BeginBlock and EndBlock); the
// FinalizeBlock response carries both events, and ParseBlockEvent returns
// each message with its mode. ParseCommitted rejects both (they carry no
// msg_index).
func TestParseBlockEventOnFinalizeBlock(t *testing.T) {
	const chainID = "shortvideo-dev-blockevents"
	begin := &eventsv1.LeaseEnded{LeaseId: "l-begin", State: "EXPIRED", ReleasedBytes: "4096"}
	end := &eventsv1.ContentWithdrawn{ContentId: "c-end", ContentVersion: 1}
	app := baseapp.NewBaseApp("blockevents", log.NewNopLogger(), dbm.NewMemDB(), nil, baseapp.SetChainID(chainID))
	app.MountStores(storetypes.NewKVStoreKey("blockevents"))
	app.SetBeginBlocker(func(ctx sdk.Context) (sdk.BeginBlock, error) {
		ctx = ctx.WithEventManager(sdk.NewEventManager())
		if err := EmitTyped(ctx, begin); err != nil {
			return sdk.BeginBlock{}, err
		}
		return sdk.BeginBlock{Events: ctx.EventManager().ABCIEvents()}, nil
	})
	app.SetEndBlocker(func(ctx sdk.Context) (sdk.EndBlock, error) {
		ctx = ctx.WithEventManager(sdk.NewEventManager())
		if err := EmitTyped(ctx, end); err != nil {
			return sdk.EndBlock{}, err
		}
		return sdk.EndBlock{Events: ctx.EventManager().ABCIEvents()}, nil
	})
	if err := app.LoadLatestVersion(); err != nil {
		t.Fatal(err)
	}
	genesisTime := time.Unix(1_767_225_600, 0).UTC()
	if _, err := app.InitChain(&abci.RequestInitChain{ChainId: chainID, Time: genesisTime, InitialHeight: 1}); err != nil {
		t.Fatal(err)
	}
	res, err := app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: genesisTime.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]proto.Message{"BeginBlock": begin, "EndBlock": end}
	seen := map[string]bool{}
	for _, ev := range res.GetEvents() {
		if _, typed := eventTypes[protoreflect.FullName(ev.Type)]; !typed {
			continue
		}
		parsed, mode, err := ParseBlockEvent(ev)
		if err != nil {
			t.Fatalf("committed block event %v: %v", ev, err)
		}
		if seen[mode] || !proto.Equal(parsed, want[mode]) {
			t.Fatalf("mode %s: parsed %v, want %v once", mode, parsed, want[mode])
		}
		seen[mode] = true
		if _, _, err := ParseCommitted(ev); err == nil {
			t.Fatalf("ParseCommitted accepted the block event %v", ev)
		}
	}
	if !seen["BeginBlock"] || !seen["EndBlock"] {
		t.Fatalf("FinalizeBlock events %v", res.GetEvents())
	}
}

// FuzzBlockEventParse feeds arbitrary event types and attribute lists to
// ParseBlockEvent. It must never panic. An accepted event ends with mode
// BeginBlock or EndBlock, its typed part re-emits to the same type, keys,
// and values (the encoding is canonical) and parses to the same message
// with ParseTyped, and neither ParseTyped nor ParseCommitted accepts the
// whole event.
func FuzzBlockEventParse(f *testing.F) {
	for _, name := range EventTypeNames() {
		spec := eventTypes[protoreflect.FullName(name)]
		for _, msg := range []proto.Message{filled(f, name), spec.messageType.New().Interface()} {
			event, err := typedToEvent(msg)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(event.Type, encodeAttributes(withMode(event, "BeginBlock").Attributes))
			f.Add(event.Type, encodeAttributes(withMode(event, "EndBlock").Attributes))
		}
	}
	f.Add("svn.events.v1.LeaseEnded", []byte("lease_id\x1fl\x1estate\x1fEXPIRED\x1ereleased_bytes\x1f1\x1emsg_index\x1f0"))
	f.Add("svn.events.v1.LeaseEnded", []byte("lease_id\x1fl\x1estate\x1fEXPIRED\x1ereleased_bytes\x1f1\x1emode\x1fFinalizeBlock"))
	f.Add("svn.events.v1.LeaseEnded", []byte("mode\x1fBeginBlock\x1elease_id\x1fl\x1estate\x1fEXPIRED\x1ereleased_bytes\x1f1"))
	f.Add("leases_offer_closed", []byte("offer_id\x1fo\x1ereason\x1fEXPIRED\x1emode\x1fBeginBlock"))
	f.Add("", []byte("mode\x1fEndBlock"))
	f.Add("", []byte{})
	f.Fuzz(func(t *testing.T, typ string, raw []byte) {
		event := abci.Event{Type: typ, Attributes: decodeAttributes(raw)}
		parsed, mode, err := ParseBlockEvent(event)
		if err != nil {
			if parsed != nil || mode != "" {
				t.Fatal("message or mode returned with an error")
			}
			return
		}
		n := len(event.Attributes)
		if (mode != "BeginBlock" && mode != "EndBlock") || event.Attributes[n-1].Key != ModeKey || event.Attributes[n-1].Value != mode {
			t.Fatalf("accepted mode %q from %v", mode, event.Attributes)
		}
		again, err := typedToEvent(parsed)
		if err != nil {
			t.Fatalf("accepted event does not re-emit: %v", err)
		}
		if again.Type != event.Type || len(again.Attributes) != n-1 {
			t.Fatalf("round trip changed the event: %v -> %v", event, again)
		}
		for i := range again.Attributes {
			if again.Attributes[i].Key != event.Attributes[i].Key || again.Attributes[i].Value != event.Attributes[i].Value {
				t.Fatalf("attribute %d: %+v -> %+v", i, event.Attributes[i], again.Attributes[i])
			}
		}
		reparsed, err := ParseTyped(again)
		if err != nil || !proto.Equal(reparsed, parsed) {
			t.Fatalf("typed parse: %v %v", reparsed, err)
		}
		if _, err := ParseTyped(event); err == nil {
			t.Fatal("ParseTyped accepted a block event")
		}
		if _, _, err := ParseCommitted(event); err == nil {
			t.Fatal("ParseCommitted accepted a block event")
		}
	})
}
