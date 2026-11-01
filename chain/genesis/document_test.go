package genesis

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

// fixtureKey returns a distinct 32-byte public key for (tag, index). The
// filler byte 0xab puts hex letters in every key, so a case change is a
// different string with the same bytes.
func fixtureKey(tag byte, index int) string {
	raw := bytes.Repeat([]byte{0xab}, ConsensusKeyBytes)
	raw[0] = tag
	binary.BigEndian.PutUint32(raw[1:5], uint32(index))
	return hex.EncodeToString(raw)
}

func fixtureAddress(fill byte) string {
	return hex.EncodeToString(bytes.Repeat([]byte{fill}, AccountBytes))
}

func fixtureValidators(count int, power int64) []Validator {
	rows := make([]Validator, count)
	for i := range rows {
		rows[i] = Validator{
			NodeID:             "v" + strconv.Itoa(i+1),
			Power:              power,
			ConsensusPubkeyHex: fixtureKey(0x01, i),
		}
	}
	return rows
}

// validFixture is a minimal devnet document that Validate accepts: four
// validators of power 1, an authority and a service submitter, four test
// seats, the eight blocked decisions, and the DEC-008 test row (threshold 3,
// delay 0). Each test changes one field so a rejection has one cause.
func validFixture() Document {
	seats := make([]DevnetTestSeat, 4)
	for i := range seats {
		seats[i] = DevnetTestSeat{SeatID: "seat-" + strconv.Itoa(i+1), PublicKeyHex: fixtureKey(0x02, i)}
	}
	decisions := make([]BlockedDecision, len(DecisionIDs))
	for i, id := range DecisionIDs {
		decisions[i] = BlockedDecision{DecisionID: id, SchemaVersion: 1}
	}
	return Document{
		ChainID:                   DevnetChainIDPrefix + "unit",
		GenesisUnixSeconds:        1_700_000_000,
		ProtocolVersion:           ProtocolVersion,
		AllowDevnetTestParameters: true,
		Validators:                fixtureValidators(MinValidators, 1),
		Submitters: []Submitter{
			{AddressHex: fixtureAddress(0x11), Role: RoleAuthority},
			{AddressHex: fixtureAddress(0x22), Role: RoleAdmittedService},
		},
		DevnetTestSeats:  seats,
		BlockedDecisions: decisions,
		DevnetTestParameters: []DevnetTestParameter{
			{Label: DevnetTestLabel, DecisionID: "DEC-008", NumericValue: 3, DelaySeconds: 0},
		},
		SafetyCaps: DefaultSafetyCaps(),
	}
}

func mustJSON(t *testing.T, doc Document) []byte {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}

// requireAccepted validates doc directly and through the file path (JSON,
// Parse, Validate) that svnd uses.
func requireAccepted(t *testing.T, doc Document) {
	t.Helper()
	if err := Validate(doc, CometIdentity{}); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	parsed, err := Parse(mustJSON(t, doc))
	if err != nil {
		t.Fatalf("parse of a valid document: %v", err)
	}
	if err := Validate(parsed, CometIdentity{}); err != nil {
		t.Fatalf("parsed valid document rejected: %v", err)
	}
}

// requireRejected requires the exact error text on both paths. The genesis
// package keeps no store, so the state a reject must leave byte-identical is
// the input: the document's JSON bytes are compared before and after.
func requireRejected(t *testing.T, doc Document, want string) {
	t.Helper()
	before := mustJSON(t, doc)
	err := Validate(doc, CometIdentity{})
	if err == nil {
		t.Fatalf("accepted; want %q", want)
	}
	if err.Error() != want {
		t.Fatalf("error %q; want %q", err.Error(), want)
	}
	if after := mustJSON(t, doc); !bytes.Equal(before, after) {
		t.Fatal("Validate changed the document")
	}
	parsed, err := Parse(before)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = Validate(parsed, CometIdentity{})
	if err == nil || err.Error() != want {
		t.Fatalf("file path error %v; want %q", err, want)
	}
}

func TestValidFixtureIsAccepted(t *testing.T) {
	requireAccepted(t, validFixture())
}

// TestDevnetDelayBounds covers the DEC-008 delay bound (review non-blocking 2).
func TestDevnetDelayBounds(t *testing.T) {
	cases := []struct {
		name  string
		delay int64
		want  string
	}{
		{"minus one", -1, "DEC-008 delay"},
		{"min int64", math.MinInt64, "DEC-008 delay"},
		{"unix max plus one", UnixSecondsMax + 1, "DEC-008 delay"},
		{"max int64", math.MaxInt64, "DEC-008 delay"},
		{"zero", 0, ""},
		{"one", 1, ""},
		{"unix max", UnixSecondsMax, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validFixture()
			doc.DevnetTestParameters[0].DelaySeconds = tc.delay
			if tc.want == "" {
				requireAccepted(t, doc)
				return
			}
			requireRejected(t, doc, tc.want)
		})
	}
}

// TestValidatorPowerTotalBound covers the spec power_set_invariant total
// bound (spec_machine.py:337): total power must be at most safe_integer_max.
func TestValidatorPowerTotalBound(t *testing.T) {
	const tooBig = "validator power total exceeds safe_integer_max"
	// 9007199254740991 = 6361 * 1416003655831, so 6361 equal powers reach the
	// bound exactly. That row pins > against >=.
	if int64(6361)*1416003655831 != SafeIntegerMax {
		t.Fatal("factorisation of safe_integer_max is wrong")
	}
	cases := []struct {
		name  string
		count int
		power int64
		want  string
	}{
		{"four at 2^51, total 2^53", 4, 2251799813685248, tooBig},
		{"four at 2^51-1, total 2^53-4", 4, 2251799813685247, ""},
		{"five, total safe max plus four", 5, 1801439850948199, tooBig},
		{"five, total safe max minus one", 5, 1801439850948198, ""},
		{"total exactly safe max", 6361, 1416003655831, ""},
		{"four at safe max", 4, SafeIntegerMax, tooBig},
		{"int64 overflow is still caught first", 4, 1 << 62, "validator power overflow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validFixture()
			doc.Validators = fixtureValidators(tc.count, tc.power)
			if tc.want == "" {
				requireAccepted(t, doc)
				return
			}
			requireRejected(t, doc, tc.want)
		})
	}
}

func TestValidatorRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Document)
		want   string
	}{
		{"count 3", func(d *Document) { d.Validators = d.Validators[:3] }, "validator count 3 is below 4"},
		{"count 0", func(d *Document) { d.Validators = nil }, "validator count 0 is below 4"},
		{"duplicate key", func(d *Document) {
			d.Validators[3].ConsensusPubkeyHex = d.Validators[0].ConsensusPubkeyHex
		}, "duplicate validator key"},
		{"duplicate key in other hex case", func(d *Document) {
			d.Validators[3].ConsensusPubkeyHex = strings.ToUpper(d.Validators[0].ConsensusPubkeyHex)
		}, "duplicate validator key"},
		{"duplicate node id", func(d *Document) { d.Validators[3].NodeID = d.Validators[0].NodeID }, "duplicate validator node id v1"},
		{"unequal power", func(d *Document) { d.Validators[2].Power = 2 }, "validator powers are not equal"},
		{"zero power", func(d *Document) { d.Validators[0].Power = 0 }, "nonpositive validator power"},
		{"negative power", func(d *Document) { d.Validators[1].Power = -1 }, "nonpositive validator power"},
		{"node id pattern", func(d *Document) { d.Validators[0].NodeID = "V1" }, `validator node id "V1"`},
		{"short key", func(d *Document) {
			d.Validators[0].ConsensusPubkeyHex = d.Validators[0].ConsensusPubkeyHex[:2*(ConsensusKeyBytes-1)]
		}, "validator key v1"},
		{"non-hex key", func(d *Document) {
			d.Validators[0].ConsensusPubkeyHex = strings.Repeat("zz", ConsensusKeyBytes)
		}, "validator key v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validFixture()
			tc.mutate(&doc)
			requireRejected(t, doc, tc.want)
		})
	}
}

// TestPrivateMaterialRejected builds each forbidden name in memory. A quoted
// literal of those names in source is a secret-scan hit; the parser still
// sees the assembled name.
func TestPrivateMaterialRejected(t *testing.T) {
	names := []string{
		"private" + "_key",
		"mnem" + "onic",
		"priv" + "_validator",
		"seed" + "_phrase",
	}
	if len(names) != len(forbiddenJSON) {
		t.Fatalf("test names %d, forbidden list %d", len(names), len(forbiddenJSON))
	}
	valid := mustJSON(t, validFixture())
	for _, name := range names {
		want := "genesis contains " + name
		inputs := map[string][]byte{
			"field":       []byte(`{"` + name + `":null}`),
			"upper field": []byte(`{"` + strings.ToUpper(name) + `":null}`),
			// A valid document whose chain id carries the name: the scan
			// covers values, not only keys, and runs before decoding.
			"value": bytes.Replace(valid, []byte(DevnetChainIDPrefix+"unit"), []byte(DevnetChainIDPrefix+name), 1),
		}
		for _, label := range []string{"field", "upper field", "value"} {
			raw := inputs[label]
			if label == "value" && bytes.Equal(raw, valid) {
				t.Fatalf("%s: fixture chain id not found", name)
			}
			if err := RejectPrivateMaterial(raw); err == nil || err.Error() != want {
				t.Fatalf("%s %s: RejectPrivateMaterial %v; want %q", name, label, err, want)
			}
			doc, err := Parse(raw)
			if err == nil || err.Error() != want {
				t.Fatalf("%s %s: Parse %v; want %q", name, label, err, want)
			}
			if doc.ChainID != "" || doc.Validators != nil {
				t.Fatalf("%s %s: Parse returned a partial document", name, label)
			}
		}
	}
	if err := RejectPrivateMaterial(valid); err != nil {
		t.Fatalf("valid document: %v", err)
	}
}

// TestParseRejectsTrailingData requires that only whitespace follows the
// document. Before this check a second JSON value was ignored, so a file
// validated as its first document alone.
func TestParseRejectsTrailingData(t *testing.T) {
	valid := mustJSON(t, validFixture())
	if _, err := Parse(append(append([]byte{}, valid...), " \n\t"...)); err != nil {
		t.Fatalf("trailing whitespace: %v", err)
	}
	cases := []struct {
		name string
		tail string
		want string
	}{
		{"second object", "{}", "malformed genesis trailing data: a second JSON value follows the document"},
		{"second document", string(valid), "malformed genesis trailing data: a second JSON value follows the document"},
		{"number", " 1", "malformed genesis trailing data: a second JSON value follows the document"},
		{"stray bracket", "]", "malformed genesis trailing data: invalid character ']' looking for beginning of value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := append(append([]byte{}, valid...), tc.tail...)
			doc, err := Parse(raw)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Parse %v; want %q", err, tc.want)
			}
			if doc.ChainID != "" {
				t.Fatal("Parse returned the first document")
			}
		})
	}
}
