package msgkit

import (
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	sdk "github.com/cosmos/cosmos-sdk/types"

	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
)

// typedEvent is one allowlisted svn.events.v1 message: its registered type
// and its fields in field-number order.
type typedEvent struct {
	messageType protoreflect.MessageType
	fields      []protoreflect.FieldDescriptor
}

// eventTypes is the allowlist: every message of svn/events/v1/events.proto.
// It is built once from the compiled descriptor, which is the same on every
// node running this binary.
var eventTypes = buildEventTypes()

func buildEventTypes() map[protoreflect.FullName]typedEvent {
	out := map[protoreflect.FullName]typedEvent{}
	messages := eventsv1.File_svn_events_v1_events_proto.Messages()
	for i := 0; i < messages.Len(); i++ {
		md := messages.Get(i)
		mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
		if err != nil {
			panic(fmt.Sprintf("msgkit: event %s is not registered: %v", md.FullName(), err))
		}
		fields := make([]protoreflect.FieldDescriptor, 0, md.Fields().Len())
		for j := 0; j < md.Fields().Len(); j++ {
			fields = append(fields, md.Fields().Get(j))
		}
		sort.Slice(fields, func(a, b int) bool { return fields[a].Number() < fields[b].Number() })
		out[md.FullName()] = typedEvent{messageType: mt, fields: fields}
	}
	return out
}

// EventTypeNames returns the allowlisted event type names in byte order.
func EventTypeNames() []string {
	names := make([]string, 0, len(eventTypes))
	for name := range eventTypes {
		names = append(names, string(name))
	}
	sort.Strings(names)
	return names
}

// EmitTyped emits one svn.events.v1 message as an indexer event.
//
// The event type is the full proto name. Attributes follow field-number
// order; the key is the proto field name; strings are copied as they are,
// integers are decimal, and booleans are "true" or "false". Every field is
// emitted, including zero values, and every attribute is indexed. A message
// outside svn.events.v1, a message with unknown fields, a field kind other
// than string, integer, or bool, or a string that is not UTF-8 is an error
// and nothing is emitted.
func EmitTyped(ctx sdk.Context, ev proto.Message) error {
	event, err := typedToEvent(ev)
	if err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.Event(event))
	return nil
}

func typedToEvent(ev proto.Message) (abci.Event, error) {
	if ev == nil {
		return abci.Event{}, fmt.Errorf("typed event: nil message")
	}
	m := ev.ProtoReflect()
	if !m.IsValid() {
		return abci.Event{}, fmt.Errorf("typed event: nil message")
	}
	name := m.Descriptor().FullName()
	spec, ok := eventTypes[name]
	if !ok {
		return abci.Event{}, fmt.Errorf("typed event: %s is not an svn.events.v1 message", name)
	}
	if m.Descriptor() != spec.messageType.Descriptor() {
		return abci.Event{}, fmt.Errorf("typed event: %s descriptor does not match the registered type", name)
	}
	if len(m.GetUnknown()) != 0 {
		return abci.Event{}, fmt.Errorf("typed event: %s carries unknown fields", name)
	}
	attributes := make([]abci.EventAttribute, 0, len(spec.fields))
	for _, fd := range spec.fields {
		value, err := formatEventValue(fd, m.Get(fd))
		if err != nil {
			return abci.Event{}, fmt.Errorf("typed event: %s.%s: %w", name, fd.Name(), err)
		}
		attributes = append(attributes, abci.EventAttribute{Key: string(fd.Name()), Value: value, Index: true})
	}
	return abci.Event{Type: string(name), Attributes: attributes}, nil
}

func formatEventValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, error) {
	if fd.IsList() || fd.IsMap() {
		return "", fmt.Errorf("repeated and map fields are not event attributes")
	}
	switch fd.Kind() {
	case protoreflect.StringKind:
		text := v.String()
		if !utf8.ValidString(text) {
			return "", fmt.Errorf("string is not UTF-8")
		}
		return text, nil
	case protoreflect.BoolKind:
		if v.Bool() {
			return "true", nil
		}
		return "false", nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind, protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return strconv.FormatUint(v.Uint(), 10), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return strconv.FormatInt(v.Int(), 10), nil
	default:
		return "", fmt.Errorf("field kind %s is not an event attribute", fd.Kind())
	}
}

// ParseTyped decodes an event written by EmitTyped back into its typed
// message. The type must be allowlisted. Every proto field must appear
// exactly once, in field-number order, and no other key may appear.
// Integers must be canonical decimals (no sign on unsigned values, no
// leading zeros, no "-0") within the field's range; booleans must be "true"
// or "false"; strings must be UTF-8. The Index flag is not checked, because
// a node's index-events setting may clear it.
func ParseTyped(ev abci.Event) (proto.Message, error) {
	spec, ok := eventTypes[protoreflect.FullName(ev.Type)]
	if !ok {
		return nil, fmt.Errorf("typed event: %q is not an svn.events.v1 message", ev.Type)
	}
	byName := make(map[string]protoreflect.FieldDescriptor, len(spec.fields))
	for _, fd := range spec.fields {
		byName[string(fd.Name())] = fd
	}
	seen := make(map[string]bool, len(ev.Attributes))
	for _, attr := range ev.Attributes {
		if _, known := byName[attr.Key]; !known {
			return nil, fmt.Errorf("typed event: %s has unknown key %q", ev.Type, attr.Key)
		}
		if seen[attr.Key] {
			return nil, fmt.Errorf("typed event: %s repeats key %q", ev.Type, attr.Key)
		}
		seen[attr.Key] = true
	}
	for _, fd := range spec.fields {
		if !seen[string(fd.Name())] {
			return nil, fmt.Errorf("typed event: %s is missing key %q", ev.Type, fd.Name())
		}
	}
	m := spec.messageType.New()
	for i, fd := range spec.fields {
		attr := ev.Attributes[i]
		if attr.Key != string(fd.Name()) {
			return nil, fmt.Errorf("typed event: %s key %q at position %d, want %q", ev.Type, attr.Key, i, fd.Name())
		}
		value, err := parseEventValue(fd, attr.Value)
		if err != nil {
			return nil, fmt.Errorf("typed event: %s.%s: %w", ev.Type, fd.Name(), err)
		}
		m.Set(fd, value)
	}
	return m.Interface(), nil
}

func parseEventValue(fd protoreflect.FieldDescriptor, text string) (protoreflect.Value, error) {
	if fd.IsList() || fd.IsMap() {
		return protoreflect.Value{}, fmt.Errorf("repeated and map fields are not event attributes")
	}
	switch fd.Kind() {
	case protoreflect.StringKind:
		if !utf8.ValidString(text) {
			return protoreflect.Value{}, fmt.Errorf("string is not UTF-8")
		}
		return protoreflect.ValueOfString(text), nil
	case protoreflect.BoolKind:
		switch text {
		case "true":
			return protoreflect.ValueOfBool(true), nil
		case "false":
			return protoreflect.ValueOfBool(false), nil
		}
		return protoreflect.Value{}, fmt.Errorf("bool %q is not true or false", text)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := parseCanonicalUint(text, 64)
		return protoreflect.ValueOfUint64(n), err
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := parseCanonicalUint(text, 32)
		return protoreflect.ValueOfUint32(uint32(n)), err
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := parseCanonicalInt(text, 64)
		return protoreflect.ValueOfInt64(n), err
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := parseCanonicalInt(text, 32)
		return protoreflect.ValueOfInt32(int32(n)), err
	default:
		return protoreflect.Value{}, fmt.Errorf("field kind %s is not an event attribute", fd.Kind())
	}
}

// canonicalDigits accepts "0" or a non-empty run of ASCII digits without a
// leading zero.
func canonicalDigits(text string) bool {
	if text == "" || len(text) > 20 {
		return false
	}
	if text[0] == '0' && len(text) > 1 {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

func parseCanonicalUint(text string, bits int) (uint64, error) {
	if !canonicalDigits(text) {
		return 0, fmt.Errorf("%q is not a canonical unsigned decimal", text)
	}
	n, err := strconv.ParseUint(text, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", text)
	}
	return n, nil
}

func parseCanonicalInt(text string, bits int) (int64, error) {
	digits := text
	if len(text) > 0 && text[0] == '-' {
		digits = text[1:]
		if digits == "0" {
			return 0, fmt.Errorf("%q is not a canonical signed decimal", text)
		}
	}
	if !canonicalDigits(digits) {
		return 0, fmt.Errorf("%q is not a canonical signed decimal", text)
	}
	n, err := strconv.ParseInt(text, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", text)
	}
	return n, nil
}

// EmitIdempotent emits "<module>_result" with idempotent=true for a
// business-id replay that matched the committed payload. It is the only
// event of such a message; no typed event follows.
func EmitIdempotent(ctx sdk.Context, module string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(module+"_result", sdk.NewAttribute("idempotent", "true")))
}
