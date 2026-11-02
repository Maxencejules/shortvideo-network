package msgkit

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func freshEventContext(t testing.TB) sdk.Context {
	t.Helper()
	ctx, _ := msgkittest.NewContext(t, "events")
	return ctx.WithEventManager(sdk.NewEventManager())
}

// filled returns an instance of an allowlisted event with every field set
// to an extreme non-zero value.
func filled(t testing.TB, name string) proto.Message {
	t.Helper()
	spec, ok := eventTypes[protoreflect.FullName(name)]
	if !ok {
		t.Fatalf("%s not allowlisted", name)
	}
	m := spec.messageType.New()
	for _, fd := range spec.fields {
		switch fd.Kind() {
		case protoreflect.StringKind:
			m.Set(fd, protoreflect.ValueOfString("v-"+string(fd.Name())))
		case protoreflect.BoolKind:
			m.Set(fd, protoreflect.ValueOfBool(true))
		case protoreflect.Uint64Kind:
			m.Set(fd, protoreflect.ValueOfUint64(math.MaxUint64))
		case protoreflect.Int64Kind:
			m.Set(fd, protoreflect.ValueOfInt64(math.MinInt64))
		default:
			t.Fatalf("%s.%s has kind %s; extend the test", name, fd.Name(), fd.Kind())
		}
	}
	return m.Interface()
}

func TestEventAllowlistIsEventsProto(t *testing.T) {
	messages := eventsv1.File_svn_events_v1_events_proto.Messages()
	names := EventTypeNames()
	if len(names) != messages.Len() || len(names) != 11 {
		t.Fatalf("allowlist %v, events.proto has %d messages", names, messages.Len())
	}
	for i := 0; i < messages.Len(); i++ {
		md := messages.Get(i)
		spec, ok := eventTypes[md.FullName()]
		if !ok {
			t.Fatalf("%s is not allowlisted", md.FullName())
		}
		if len(spec.fields) != md.Fields().Len() {
			t.Fatalf("%s: %d fields, descriptor %d", md.FullName(), len(spec.fields), md.Fields().Len())
		}
		for j := 1; j < len(spec.fields); j++ {
			if spec.fields[j-1].Number() >= spec.fields[j].Number() {
				t.Fatalf("%s fields not in number order", md.FullName())
			}
		}
	}
}

func TestEmitParseRoundTripEveryEventType(t *testing.T) {
	for _, name := range EventTypeNames() {
		spec := eventTypes[protoreflect.FullName(name)]
		for _, msg := range []proto.Message{filled(t, name), spec.messageType.New().Interface()} {
			ctx := freshEventContext(t)
			if err := EmitTyped(ctx, msg); err != nil {
				t.Fatalf("%s: emit: %v", name, err)
			}
			events := ctx.EventManager().Events()
			if len(events) != 1 {
				t.Fatalf("%s: %d events", name, len(events))
			}
			event := abci.Event(events[0])
			if event.Type != name {
				t.Fatalf("event type %q, want %q", event.Type, name)
			}
			// The attribute key set is exactly the proto field set, in
			// field-number order, all indexed.
			if len(event.Attributes) != len(spec.fields) {
				t.Fatalf("%s: %d attributes for %d fields", name, len(event.Attributes), len(spec.fields))
			}
			for i, fd := range spec.fields {
				if event.Attributes[i].Key != string(fd.Name()) || !event.Attributes[i].Index {
					t.Fatalf("%s attribute %d = %+v, want key %s indexed", name, i, event.Attributes[i], fd.Name())
				}
			}
			parsed, err := ParseTyped(event)
			if err != nil {
				t.Fatalf("%s: parse: %v", name, err)
			}
			if !proto.Equal(parsed, msg) {
				t.Fatalf("%s: round trip %v != %v", name, parsed, msg)
			}
			if parsed.ProtoReflect().Descriptor() != msg.ProtoReflect().Descriptor() {
				t.Fatalf("%s: parsed into another type", name)
			}
		}
	}
}

func TestEmitTypedEncoding(t *testing.T) {
	ctx := freshEventContext(t)
	manifest := Hex(bytes.Repeat([]byte{0xab}, 32))
	events := []proto.Message{
		&eventsv1.ContentRegistered{ContentId: "c1", ContentVersion: 1, ManifestSha256: manifest, PackageBytes: "1024"},
		&eventsv1.ContentWithdrawn{ContentId: "c1", ContentVersion: 1},
		&eventsv1.ProviderStateChanged{ProviderId: "p1", State: "ADMITTED", KeyGeneration: 2},
		&eventsv1.LeaseAccepted{LeaseId: "l1", DeadlineUnixSeconds: -5},
	}
	for _, ev := range events {
		if err := EmitTyped(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	got := ctx.EventManager().Events().ToABCIEvents()
	want := []abci.Event{
		{Type: "svn.events.v1.ContentRegistered", Attributes: []abci.EventAttribute{
			{Key: "content_id", Value: "c1", Index: true},
			{Key: "content_version", Value: "1", Index: true},
			{Key: "manifest_sha256", Value: manifest, Index: true},
			{Key: "package_bytes", Value: "1024", Index: true},
		}},
		{Type: "svn.events.v1.ContentWithdrawn", Attributes: []abci.EventAttribute{
			{Key: "content_id", Value: "c1", Index: true},
			{Key: "content_version", Value: "1", Index: true},
			{Key: "history_erased", Value: "false", Index: true},
		}},
		{Type: "svn.events.v1.ProviderStateChanged", Attributes: []abci.EventAttribute{
			{Key: "provider_id", Value: "p1", Index: true},
			{Key: "state", Value: "ADMITTED", Index: true},
			{Key: "key_generation", Value: "2", Index: true},
		}},
		{Type: "svn.events.v1.LeaseAccepted", Attributes: []abci.EventAttribute{
			{Key: "lease_id", Value: "l1", Index: true},
			{Key: "provider_id", Value: "", Index: true},
			{Key: "content_id", Value: "", Index: true},
			{Key: "content_version", Value: "0", Index: true},
			{Key: "package_bytes", Value: "", Index: true},
			{Key: "deadline_unix_seconds", Value: "-5", Index: true},
		}},
	}
	if len(got) != len(want) {
		t.Fatalf("%d events", len(got))
	}
	for i := range want {
		if got[i].Type != want[i].Type || fmt.Sprint(got[i].Attributes) != fmt.Sprint(want[i].Attributes) {
			t.Fatalf("event %d\n got %v\nwant %v", i, got[i], want[i])
		}
	}
}

func TestEmitTypedRejectsOtherMessages(t *testing.T) {
	ctx := freshEventContext(t)
	withUnknown := &eventsv1.ContentWithdrawn{ContentId: "c1"}
	withUnknown.ProtoReflect().SetUnknown(protoreflect.RawFields{0xf8, 0x06, 0x01}) // field 99 varint 1
	badUTF8 := &eventsv1.ProviderRegistered{ProviderId: "p\xff"}
	for name, msg := range map[string]proto.Message{
		"nil":            nil,
		"typed nil":      (*eventsv1.ContentRegistered)(nil),
		"not an event":   &typesv1.Digest{},
		"another proto":  &typesv1.InnerSignature{},
		"unknown fields": withUnknown,
		"invalid utf-8":  badUTF8,
	} {
		if err := EmitTyped(ctx, msg); err == nil {
			t.Errorf("%s: emitted", name)
		}
	}
	if n := len(ctx.EventManager().Events()); n != 0 {
		t.Fatalf("%d events emitted by rejected calls", n)
	}
}

func attrs(pairs ...string) []abci.EventAttribute {
	out := make([]abci.EventAttribute, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, abci.EventAttribute{Key: pairs[i], Value: pairs[i+1], Index: true})
	}
	return out
}

func TestParseTypedRejects(t *testing.T) {
	const changed = "svn.events.v1.ProviderStateChanged"
	const lease = "svn.events.v1.LeaseAccepted"
	leaseAttrs := func(deadline string) []abci.EventAttribute {
		return attrs("lease_id", "l", "provider_id", "p", "content_id", "c", "content_version", "1", "package_bytes", "1", "deadline_unix_seconds", deadline)
	}
	cases := map[string]abci.Event{
		"unknown type":       {Type: "svn.events.v1.Nope", Attributes: attrs()},
		"non-event type":     {Type: "svn.types.v1.Digest", Attributes: attrs("algorithm", "1")},
		"sdk event":          {Type: "message", Attributes: attrs("action", "x")},
		"unknown key":        {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "1", "extra", "x")},
		"missing key":        {Type: changed, Attributes: attrs("provider_id", "p", "state", "S")},
		"duplicate key":      {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "state", "S")},
		"out of order":       {Type: changed, Attributes: attrs("state", "S", "provider_id", "p", "key_generation", "1")},
		"leading zero":       {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "01")},
		"plus sign":          {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "+1")},
		"negative unsigned":  {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "-1")},
		"empty number":       {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "")},
		"fraction":           {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "1.0")},
		"exponent":           {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "1e3")},
		"uint64 overflow":    {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "18446744073709551616")},
		"spaces":             {Type: changed, Attributes: attrs("provider_id", "p", "state", "S", "key_generation", " 1")},
		"invalid utf-8":      {Type: changed, Attributes: attrs("provider_id", "p\xff", "state", "S", "key_generation", "1")},
		"minus zero":         {Type: lease, Attributes: leaseAttrs("-0")},
		"int64 overflow":     {Type: lease, Attributes: leaseAttrs("9223372036854775808")},
		"int64 underflow":    {Type: lease, Attributes: leaseAttrs("-9223372036854775809")},
		"signed leading 0":   {Type: lease, Attributes: leaseAttrs("-01")},
		"lone minus":         {Type: lease, Attributes: leaseAttrs("-")},
		"bool capitalised":   {Type: "svn.events.v1.ContentWithdrawn", Attributes: attrs("content_id", "c", "content_version", "1", "history_erased", "True")},
		"bool numeric":       {Type: "svn.events.v1.ContentWithdrawn", Attributes: attrs("content_id", "c", "content_version", "1", "history_erased", "0")},
		"no attributes":      {Type: changed},
		"empty type":         {Type: "", Attributes: attrs("provider_id", "p")},
		"proto name, no pkg": {Type: "ProviderStateChanged", Attributes: attrs("provider_id", "p", "state", "S", "key_generation", "1")},
	}
	for name, ev := range cases {
		if msg, err := ParseTyped(ev); err == nil {
			t.Errorf("%s: parsed as %v", name, msg)
		}
	}
	// Extremes that are valid.
	if _, err := ParseTyped(abci.Event{Type: lease, Attributes: leaseAttrs("-9223372036854775808")}); err != nil {
		t.Fatalf("min int64: %v", err)
	}
	if _, err := ParseTyped(abci.Event{Type: changed, Attributes: attrs("provider_id", "", "state", "", "key_generation", "18446744073709551615")}); err != nil {
		t.Fatalf("max uint64: %v", err)
	}
	// The Index flag is not part of the schema.
	unindexed := abci.Event{Type: changed, Attributes: []abci.EventAttribute{
		{Key: "provider_id", Value: "p"}, {Key: "state", Value: "S"}, {Key: "key_generation", Value: "3"},
	}}
	parsed, err := ParseTyped(unindexed)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.(*eventsv1.ProviderStateChanged); got.GetKeyGeneration() != 3 || got.GetProviderId() != "p" {
		t.Fatalf("parsed %v", got)
	}
}

// TestParseCommitted: a committed event is the EmitTyped attributes and
// then the SDK's msg_index. ParseCommitted returns the typed message and
// the index, and rejects an event without a trailing canonical msg_index.
func TestParseCommitted(t *testing.T) {
	const changed = "svn.events.v1.ProviderStateChanged"
	typed := attrs("provider_id", "p", "state", "ADMITTED", "key_generation", "2")
	committed := func(extra ...string) abci.Event {
		return abci.Event{Type: changed, Attributes: append(append([]abci.EventAttribute(nil), typed...), attrs(extra...)...)}
	}
	msg, index, err := ParseCommitted(committed(MsgIndexKey, "3"))
	if err != nil {
		t.Fatal(err)
	}
	if index != 3 || !proto.Equal(msg, &eventsv1.ProviderStateChanged{ProviderId: "p", State: "ADMITTED", KeyGeneration: 2}) {
		t.Fatalf("parsed %v index %d", msg, index)
	}
	if _, err := ParseTyped(committed(MsgIndexKey, "0")); err == nil {
		t.Fatal("ParseTyped accepted a committed event; msg_index is not schema")
	}
	cases := map[string]abci.Event{
		"no msg_index":            committed(),
		"msg_index first":         {Type: changed, Attributes: append(attrs(MsgIndexKey, "0"), typed...)},
		"msg_index leading zero":  committed(MsgIndexKey, "01"),
		"msg_index negative":      committed(MsgIndexKey, "-1"),
		"msg_index above uint32":  committed(MsgIndexKey, "4294967296"),
		"msg_index empty":         committed(MsgIndexKey, ""),
		"two msg_index":           committed(MsgIndexKey, "0", MsgIndexKey, "0"),
		"attribute after":         committed(MsgIndexKey, "0", "extra", "x"),
		"typed part invalid":      {Type: changed, Attributes: attrs("provider_id", "p", MsgIndexKey, "0")},
		"not an svn.events type":  {Type: "message", Attributes: attrs("action", "x", MsgIndexKey, "0")},
		"no attributes":           {Type: changed},
		"msg_index only, no type": {Type: "", Attributes: attrs(MsgIndexKey, "0")},
	}
	for name, ev := range cases {
		if msg, _, err := ParseCommitted(ev); err == nil {
			t.Errorf("%s: parsed as %v", name, msg)
		}
	}
}

func TestEmitIdempotent(t *testing.T) {
	ctx := freshEventContext(t)
	EmitIdempotent(ctx, "providers")
	events := ctx.EventManager().Events()
	if len(events) != 1 || events[0].Type != "providers_result" || len(events[0].Attributes) != 1 ||
		events[0].Attributes[0].Key != "idempotent" || events[0].Attributes[0].Value != "true" {
		t.Fatalf("events %v", events)
	}
	if _, err := ParseTyped(abci.Event(events[0])); err == nil {
		t.Fatal("the idempotent marker parsed as a typed event")
	}
}

// encodeAttributes and decodeAttributes give the fuzzer one byte string
// per attribute list: records split by 0x1e, key and value by 0x1f.
func encodeAttributes(list []abci.EventAttribute) []byte {
	var out []byte
	for i, a := range list {
		if i > 0 {
			out = append(out, 0x1e)
		}
		out = append(out, a.Key...)
		out = append(out, 0x1f)
		out = append(out, a.Value...)
	}
	return out
}

func decodeAttributes(raw []byte) []abci.EventAttribute {
	if len(raw) == 0 {
		return nil
	}
	var out []abci.EventAttribute
	for _, record := range bytes.Split(raw, []byte{0x1e}) {
		key, value, _ := bytes.Cut(record, []byte{0x1f})
		out = append(out, abci.EventAttribute{Key: string(key), Value: string(value), Index: true})
	}
	return out
}

// FuzzTypedEventParse feeds arbitrary event types and attribute lists to
// ParseTyped. It must never panic, and any event it accepts must re-emit
// to the same type, keys, and values (the encoding is canonical).
func FuzzTypedEventParse(f *testing.F) {
	for _, name := range EventTypeNames() {
		spec := eventTypes[protoreflect.FullName(name)]
		for _, msg := range []proto.Message{filled(f, name), spec.messageType.New().Interface()} {
			event, err := typedToEvent(msg)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(event.Type, encodeAttributes(event.Attributes))
		}
	}
	f.Add("svn.events.v1.ProviderStateChanged", []byte("provider_id\x1fp\x1estate\x1fS\x1ekey_generation\x1f01"))
	f.Add("svn.events.v1.LeaseAccepted", []byte("lease_id\x1f\x1e\x1e\x1f-0"))
	f.Add("", []byte{})
	f.Fuzz(func(t *testing.T, typ string, raw []byte) {
		event := abci.Event{Type: typ, Attributes: decodeAttributes(raw)}
		parsed, err := ParseTyped(event)
		if err != nil {
			if parsed != nil {
				t.Fatal("message returned with an error")
			}
			return
		}
		again, err := typedToEvent(parsed)
		if err != nil {
			t.Fatalf("accepted event does not re-emit: %v", err)
		}
		if again.Type != event.Type || len(again.Attributes) != len(event.Attributes) {
			t.Fatalf("round trip changed the event: %v -> %v", event, again)
		}
		for i := range again.Attributes {
			if again.Attributes[i].Key != event.Attributes[i].Key || again.Attributes[i].Value != event.Attributes[i].Value {
				t.Fatalf("attribute %d: %+v -> %+v", i, event.Attributes[i], again.Attributes[i])
			}
		}
		reparsed, err := ParseTyped(again)
		if err != nil || !proto.Equal(reparsed, parsed) {
			t.Fatalf("second parse: %v %v", reparsed, err)
		}
	})
}
