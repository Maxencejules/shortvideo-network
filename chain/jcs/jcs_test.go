package jcs

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// reason is the SVN-JCS-1 reason of err: "" for nil, and a marker that
// never equals a reason for an error of another type.
func reason(err error) string {
	if err == nil {
		return ""
	}
	var jerr *jcsError
	if errors.As(err, &jerr) {
		return jerr.reason
	}
	return "not a jcs error: " + err.Error()
}

// esc turns each %u into the JSON escape prefix (a backslash and u), so
// the JSON texts in this file keep their escapes exactly as written.
func esc(s string) string { return strings.ReplaceAll(s, "%u", `\`+"u") }

// vectorsPath is the frozen goal-02 vector file. The goal-02 Go, Python and
// TypeScript vector programs check the same file.
const vectorsPath = "../../docs/protocol/fixtures/vectors.json"

type protocolVectors struct {
	Profile string `json:"profile"`
	Valid   []struct {
		ID        string `json:"id"`
		InputRaw  string `json:"input_raw"`
		Canonical string `json:"canonical"`
	} `json:"valid"`
	Invalid []struct {
		ID     string `json:"id"`
		Raw    string `json:"raw"`
		RawHex string `json:"raw_hex"`
		Reason string `json:"reason"`
	} `json:"invalid"`
}

func loadVectors(tb testing.TB) protocolVectors {
	tb.Helper()
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		tb.Fatalf("read %s: %v", vectorsPath, err)
	}
	var doc protocolVectors
	if err := json.Unmarshal(raw, &doc); err != nil {
		tb.Fatalf("decode %s: %v", vectorsPath, err)
	}
	if doc.Profile != "SVN-JCS-1" || len(doc.Valid) == 0 || len(doc.Invalid) == 0 {
		tb.Fatalf("%s: profile %q, %d valid, %d invalid", vectorsPath, doc.Profile, len(doc.Valid), len(doc.Invalid))
	}
	return doc
}

// TestProtocolVectors runs this package against every valid and invalid
// SVN-JCS-1 vector of protocol v0: the canonical text and the refusal
// reason must match the frozen file, and every canonical text is a fixed
// point.
func TestProtocolVectors(t *testing.T) {
	doc := loadVectors(t)
	for _, v := range doc.Valid {
		got, err := ParseAndCanonicalize(v.InputRaw)
		if err != nil || got != v.Canonical {
			t.Errorf("valid %s: got %q %v, want %q", v.ID, got, err, v.Canonical)
			continue
		}
		again, err := ParseAndCanonicalize(got)
		if err != nil || again != got {
			t.Errorf("valid %s: canonical text is not a fixed point: %q %v", v.ID, again, err)
		}
	}
	for _, v := range doc.Invalid {
		raw := []byte(v.Raw)
		if v.RawHex != "" {
			decoded, err := hex.DecodeString(v.RawHex)
			if err != nil {
				t.Fatalf("invalid %s: raw_hex: %v", v.ID, err)
			}
			raw = decoded
		}
		got, err := parseBytes(raw)
		if reason(err) != v.Reason {
			t.Errorf("invalid %s: got %q, reason %q, want %q", v.ID, got, reason(err), v.Reason)
		}
	}
}

// rfcString is the "string" member of the RFC 8785 section 3.2.2 example,
// as JSON text, and rfcStringValue is the string it spells.
var (
	rfcString      = esc(`"%u20ac$%u000F%u000aA'%u0042%u0022%u005c\\\"\/"`)
	rfcStringValue = "\U000020AC$\x0f\nA'B\"\\\\\"/"
)

// TestRFC8785StringExample is the string member of the RFC 8785 section
// 3.2.2 example. Its null and non-integer numbers are outside the profile
// and refused; the literals that remain are kept.
func TestRFC8785StringExample(t *testing.T) {
	input := "{\n  \"string\": " + rfcString + ",\n  \"literals\": [true, false]\n}"
	want := "{\"literals\":[true,false],\"string\":\"\U000020AC$" + esc(`%u000f\nA'B\"\\\\\"/"}`)
	got, err := ParseAndCanonicalize(input)
	if err != nil || got != want {
		t.Fatalf("parse: got %q %v\nwant %q", got, err, want)
	}
	direct, err := Canonicalize(map[string]any{"string": rfcStringValue, "literals": []any{true, false}})
	if err != nil || direct != want {
		t.Fatalf("canonicalize: got %q %v\nwant %q", direct, err, want)
	}

	// The full RFC 8785 example: the parser reads in text order and stops
	// at the first number with a fraction.
	full := "{\n  \"numbers\": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],\n" +
		"  \"string\": " + rfcString + ",\n  \"literals\": [null, true, false]\n}"
	if _, err := ParseAndCanonicalize(full); reason(err) != "fraction" {
		t.Fatalf("full RFC 8785 example: reason %q, want fraction", reason(err))
	}
	refused := map[string]string{
		"333333333.33333329":            "fraction",
		"1E30":                          "exponent",
		"4.50":                          "fraction",
		"2e-3":                          "exponent",
		"0.000000000000000000000000001": "fraction",
		"null":                          "null",
		"[null,true,false]":             "null",
		"-0":                            "negative_zero",
	}
	for raw, want := range refused {
		if got, err := ParseAndCanonicalize(raw); reason(err) != want {
			t.Errorf("%s: got %q, reason %q, want %q", raw, got, reason(err), want)
		}
	}
}

// TestRFC8785SortingExample sorts the property names of the RFC 8785
// section 3.2.3 example by UTF-16 code units. Byte (code point) order
// differs: it puts U+FB33 before U+1F600, whose UTF-16 form starts with the
// high surrogate 0xD83D. The profile refuses the example as an object,
// because keys are ASCII [A-Za-z0-9._-] only.
func TestRFC8785SortingExample(t *testing.T) {
	keys := []string{"\U000020AC", "\r", "\U0000FB33", "1", "\U0001F600", "\U00000080", "\U000000F6"}
	want := []string{"\r", "1", "\U00000080", "\U000000F6", "\U000020AC", "\U0001F600", "\U0000FB33"}
	got := append([]string(nil), keys...)
	sort.Slice(got, func(i, j int) bool { return compareUTF16(got[i], got[j]) < 0 })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UTF-16 order\n got %q\nwant %q", got, want)
	}
	byBytes := append([]string(nil), keys...)
	sort.Strings(byBytes)
	if reflect.DeepEqual(byBytes, want) {
		t.Fatalf("byte order equals UTF-16 order for the RFC keys; the test does not separate them")
	}
	if compareUTF16("\U0001F600", "\U0000E000") >= 0 || compareUTF16("\U0000E000", "\U0001F600") <= 0 {
		t.Fatalf("U+1F600 (D83D DE00) must sort before U+E000")
	}
	if compareUTF16("a", "ab") >= 0 || compareUTF16("ab", "a") <= 0 || compareUTF16("ab", "ab") != 0 {
		t.Fatalf("a proper prefix must sort first")
	}

	object := esc(`{"%u20ac":"Euro Sign","\r":"Carriage Return","%ufb33":"Hebrew Letter Dalet With Dagesh",` +
		`"1":"One","%ud83d%ude00":"Emoji: Grinning Face","%u0080":"Control","%u00f6":"Latin Small Letter O With Diaeresis"}`)
	if _, err := ParseAndCanonicalize(object); reason(err) != "bad_key" {
		t.Fatalf("RFC 8785 sorting object: reason %q, want bad_key", reason(err))
	}
}

// TestKeyOrderASCII: the profile's keys are ASCII, where UTF-16 code-unit
// order is byte order. Every string of one or two key characters sorts the
// same both ways, and Canonicalize writes members in that order.
func TestKeyOrderASCII(t *testing.T) {
	const alphabet = "-.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz"
	var all []string
	for i := 0; i < len(alphabet); i++ {
		all = append(all, alphabet[i:i+1])
		for j := 0; j < len(alphabet); j++ {
			all = append(all, string([]byte{alphabet[i], alphabet[j]}))
		}
	}
	utf16Order := append([]string(nil), all...)
	sort.Slice(utf16Order, func(i, j int) bool { return compareUTF16(utf16Order[i], utf16Order[j]) < 0 })
	byteOrder := append([]string(nil), all...)
	sort.Strings(byteOrder)
	if !reflect.DeepEqual(utf16Order, byteOrder) {
		t.Fatalf("UTF-16 and byte order differ on ASCII keys")
	}

	object := map[string]any{}
	for i, key := range []string{"b", "a0", "aa", "a", "_", "A", "0", ".", "-", "Z", "9"} {
		object[key] = int64(i)
	}
	got, err := Canonicalize(object)
	want := `{"-":8,".":7,"0":6,"9":10,"A":5,"Z":9,"_":4,"a":3,"a0":1,"aa":2,"b":0}`
	if err != nil || got != want {
		t.Fatalf("key order: got %q %v\nwant %q", got, err, want)
	}

	for _, key := range []string{"", "a b", "\U000000E9", "a\x00", strings.Repeat("k", 65), "\r"} {
		if _, err := Canonicalize(map[string]any{key: "x"}); reason(err) != "bad_key" {
			t.Errorf("key %q: reason %q, want bad_key", key, reason(err))
		}
	}
	if _, err := Canonicalize(map[string]any{strings.Repeat("k", 64): "x"}); err != nil {
		t.Errorf("key of 64 bytes: %v", err)
	}
}

// TestIntegerFormatting covers the integers the repository writes: int64
// values in the safe range are plain decimal (never an exponent or a
// fraction), and uint64 quantities are decimal strings.
func TestIntegerFormatting(t *testing.T) {
	cases := []struct {
		value int64
		text  string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{10, "10"},
		{100, "100"},
		{1000000, "1000000"},
		{1000000000000000, "1000000000000000"},
		{253402300799, "253402300799"},
		{9007199254740991, "9007199254740991"},
		{-9007199254740991, "-9007199254740991"},
	}
	for _, c := range cases {
		got, err := Canonicalize(c.value)
		if err != nil || got != c.text {
			t.Errorf("Canonicalize(%d) = %q %v, want %q", c.value, got, err, c.text)
		}
		parsed, err := ParseAndCanonicalize(c.text)
		if err != nil || parsed != c.text {
			t.Errorf("ParseAndCanonicalize(%q) = %q %v", c.text, parsed, err)
		}
	}

	// An int64 outside the safe range has no SVN-JCS-1 form: the parser
	// refuses to read it, so the writer refuses to write it.
	for _, value := range []int64{9007199254740992, -9007199254740992, math.MaxInt64, math.MinInt64} {
		if got, err := Canonicalize(value); reason(err) != "unsafe_integer" {
			t.Errorf("Canonicalize(%d) = %q, reason %q, want unsafe_integer", value, got, reason(err))
		}
		if got, err := Canonicalize(map[string]any{"n": []any{value}}); reason(err) != "unsafe_integer" {
			t.Errorf("nested %d = %q, reason %q, want unsafe_integer", value, got, reason(err))
		}
	}

	// Only int64 is a number; every other numeric Go type is refused.
	for _, value := range []any{1, int32(1), uint64(1), uint32(1), 1.0, float32(1), json.Number("1")} {
		if _, err := Canonicalize(map[string]any{"n": value}); reason(err) != "malformed" {
			t.Errorf("%T: reason %q, want malformed", value, reason(err))
		}
	}

	text := map[string]string{
		"1.0":                  "fraction",
		"1E2":                  "exponent",
		"1e+2":                 "exponent",
		"01":                   "leading_zero",
		"-01":                  "leading_zero",
		"00":                   "leading_zero",
		"-":                    "malformed",
		"+1":                   "malformed",
		"-0":                   "negative_zero",
		"9007199254740992":     "unsafe_integer",
		"-9007199254740992":    "unsafe_integer",
		"99999999999999999999": "unsafe_integer",
	}
	for raw, want := range text {
		if got, err := ParseAndCanonicalize(raw); reason(err) != want {
			t.Errorf("%q: got %q, reason %q, want %q", raw, got, reason(err), want)
		}
	}

	got, err := Canonicalize(map[string]any{"bytes": strconv.FormatUint(math.MaxUint64, 10), "zero": "0"})
	if err != nil || got != `{"bytes":"18446744073709551615","zero":"0"}` {
		t.Fatalf("uint64 decimal strings: %q %v", got, err)
	}
}

// invalidUTF8 are byte strings that are not UTF-8: a stray continuation
// byte, 0xFF, overlong forms, the CESU-8/WTF-8 spelling of a lone high and a
// lone low surrogate and of a surrogate pair, a code point above U+10FFFF, a
// five-byte form, and truncated sequences.
var invalidUTF8 = []string{
	"\xff",
	"\xfe",
	"a\xffb",
	"\x80",
	"\xc0\xaf",
	"\xc1\xbf",
	"\xe0\x80\xaf",
	"\xed\xa0\x80",
	"\xed\xbf\xbf",
	"\xed\xa0\xbd\xed\xb8\x80",
	"\xf4\x90\x80\x80",
	"\xf8\x88\x80\x80\x80",
	"\xe2\x82",
	"ok\xe2\x82",
	"\xf0\x9f\x98",
	"\U0000FFFD\xff",
}

// TestInvalidUTF8Refused: a string that is not valid UTF-8 has no SVN-JCS-1
// form, as a value at any depth or as a key. Before this rule the writer
// spelled every invalid byte as U+FFFD, so "a\xff", "a\xfe" and "a" + U+FFFD
// had one canonical text and one signature.
func TestInvalidUTF8Refused(t *testing.T) {
	for _, bad := range invalidUTF8 {
		if utf8.ValidString(bad) {
			t.Fatalf("%q is valid UTF-8; the table is wrong", bad)
		}
		values := map[string]any{
			"top level":     bad,
			"object value":  map[string]any{"s": bad},
			"array item":    map[string]any{"a": []any{"x", bad}},
			"nested object": map[string]any{"o": map[string]any{"p": []any{map[string]any{"q": bad}}}},
			"after valid":   map[string]any{"a": "ok", "b": int64(1), "z": bad},
		}
		for name, value := range values {
			if got, err := Canonicalize(value); reason(err) != "invalid_utf8" || got != "" {
				t.Errorf("%q as %s: got %q, reason %q, want invalid_utf8", bad, name, got, reason(err))
			}
		}
		// Keys fail the ASCII key rule before they are written.
		for _, key := range []string{bad, "k" + bad, bad + "k"} {
			if got, err := Canonicalize(map[string]any{key: "x"}); reason(err) != "bad_key" || got != "" {
				t.Errorf("key %q: got %q, reason %q, want bad_key", key, got, reason(err))
			}
		}
		// The string writer refuses on its own, independent of the key rule.
		if got, err := emitString(bad); reason(err) != "invalid_utf8" || got != "" {
			t.Errorf("emitString(%q) = %q, reason %q", bad, got, reason(err))
		}
		// The parser refuses the same bytes inside JSON text.
		if got, err := ParseAndCanonicalize(`{"s":"` + bad + `"}`); reason(err) != "invalid_utf8" {
			t.Errorf("parse %q: got %q, reason %q, want invalid_utf8", bad, got, reason(err))
		}
	}

	// The U+FFFD spelling is valid UTF-8 and has its own text; the invalid
	// spellings that used to collide with it have none.
	ok, err := Canonicalize(map[string]any{"s": "a\U0000FFFD"})
	if err != nil || ok != "{\"s\":\"a\U0000FFFD\"}" {
		t.Fatalf("U+FFFD: %q %v", ok, err)
	}
	for _, bad := range []string{"a\xff", "a\xfe", "a\xed\xa0\x80"} {
		if got, err := Canonicalize(map[string]any{"s": bad}); err == nil {
			t.Fatalf("%q canonicalized as %q", bad, got)
		}
	}
}

// TestLoneSurrogateEscapes: in JSON text an escape for U+D800..U+DFFF is
// accepted only as a high surrogate followed directly by a low-surrogate
// escape; the pair becomes one code point written as four UTF-8 bytes.
func TestLoneSurrogateEscapes(t *testing.T) {
	refused := []string{
		`"%uD800"`,
		`"%uDBFF"`,
		`"%uDC00"`,
		`"%uDFFF"`,
		`"%uD800%u0041"`,
		`"%uD800x"`,
		`"%uD800\n"`,
		`"%uDE00%uD83D"`,
		`"%uD83D%uD83D"`,
		`{"s":"a%ud800b"}`,
	}
	for _, raw := range refused {
		if got, err := ParseAndCanonicalize(esc(raw)); reason(err) != "lone_surrogate" {
			t.Errorf("%s: got %q, reason %q, want lone_surrogate", raw, got, reason(err))
		}
	}
	pairs := []struct{ raw, want string }{
		{`"%uD83D%uDE00"`, "\"\U0001F600\""},
		{`"%ud83d%ude00"`, "\"\U0001F600\""},
		{`"%uD800%uDC00"`, "\"\U00010000\""},
		{`"%uDBFF%uDFFF"`, "\"\U0010FFFF\""},
	}
	for _, p := range pairs {
		got, err := ParseAndCanonicalize(esc(p.raw))
		if err != nil || got != p.want {
			t.Errorf("%s: got %q %v, want %q", p.raw, got, err, p.want)
		}
	}
}

// TestStringEscapes is the RFC 8785 string form: two-character escapes for
// quote, backslash, and the five named controls, a six-character escape in
// lowercase hex for the other controls below U+0020, and every other code
// point as its UTF-8 bytes (including DEL, U+2028, U+2029, U+FFFD and
// U+10FFFF). encoding/json reads each text back to the same string.
func TestStringEscapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `""`},
		{`"`, `"\""`},
		{`\`, `"\\"`},
		{"/", `"/"`},
		{"\b\t\n\f\r", `"\b\t\n\f\r"`},
		{"\x00", esc(`"%u0000"`)},
		{"\x01", esc(`"%u0001"`)},
		{"\x0b", esc(`"%u000b"`)},
		{"\x1f", esc(`"%u001f"`)},
		{"\x7f", "\"\x7f\""},
		{"\U000000E9", "\"\U000000E9\""},
		{"\U00002028\U00002029", "\"\U00002028\U00002029\""},
		{"\U0000FFFD", "\"\U0000FFFD\""},
		{"\U0001F600", "\"\U0001F600\""},
		{"\U0010FFFF", "\"\U0010FFFF\""},
		{"a\"b\\c/d\x1fe\U000020AC", esc(`"a\"b\\c/d%u001fe`) + "\U000020AC\""},
	}
	for _, c := range cases {
		got, err := emitString(c.in)
		if err != nil || got != c.want {
			t.Errorf("emitString(%q) = %q %v, want %q", c.in, got, err, c.want)
			continue
		}
		var back string
		if err := json.Unmarshal([]byte(got), &back); err != nil || back != c.in {
			t.Errorf("encoding/json reads %q as %q %v, want %q", got, back, err, c.in)
		}
	}
}

// TestEmitStringShortExhaustive writes every one- and two-byte string and
// every three-byte string with a lead byte in E0..EF (the range that holds
// the surrogate spellings ED A0..BF xx). A string is refused exactly when
// it is not valid UTF-8; otherwise encoding/json reads the text back to the
// same bytes, so distinct accepted strings have distinct texts.
func TestEmitStringShortExhaustive(t *testing.T) {
	check := func(s string) {
		got, err := emitString(s)
		if !utf8.ValidString(s) {
			if reason(err) != "invalid_utf8" {
				t.Fatalf("%q: got %q, reason %q, want invalid_utf8", s, got, reason(err))
			}
			return
		}
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		var back string
		if err := json.Unmarshal([]byte(got), &back); err != nil || back != s {
			t.Fatalf("%q: text %q reads back as %q %v", s, got, back, err)
		}
		if strings.ContainsRune(got, utf8.RuneError) && !strings.ContainsRune(s, utf8.RuneError) {
			t.Fatalf("%q: text %q has U+FFFD", s, got)
		}
	}
	buf := make([]byte, 3)
	for a := 0; a < 256; a++ {
		buf[0] = byte(a)
		check(string(buf[:1]))
		for b := 0; b < 256; b++ {
			buf[1] = byte(b)
			check(string(buf[:2]))
			if a < 0xE0 || a > 0xEF {
				continue
			}
			for c := 0; c < 256; c++ {
				buf[2] = byte(c)
				check(string(buf[:3]))
			}
		}
	}
}

// TestShapeRules covers nil, unsupported container types, the depth limit
// (8, the same for the writer and the parser), and the 65536-byte cap.
func TestShapeRules(t *testing.T) {
	shapes := map[string]struct {
		value  any
		reason string
	}{
		"nil":            {nil, "null"},
		"nil value":      {map[string]any{"a": nil}, "null"},
		"nil item":       {[]any{true, nil}, "null"},
		"[]string":       {[]string{"x"}, "malformed"},
		"map[string]int": {map[string]int{"a": 1}, "malformed"},
		"[]byte":         {[]byte("x"), "malformed"},
		"struct":         {struct{}{}, "malformed"},
	}
	for name, c := range shapes {
		if got, err := Canonicalize(c.value); reason(err) != c.reason {
			t.Errorf("%s: got %q, reason %q, want %q", name, got, reason(err), c.reason)
		}
	}

	nest := func(n int) any {
		var v any = "x"
		for i := 0; i < n; i++ {
			v = []any{v}
		}
		return v
	}
	for n := 0; n <= 12; n++ {
		text := strings.Repeat("[", n) + `"x"` + strings.Repeat("]", n)
		got, err := Canonicalize(nest(n))
		parsed, perr := ParseAndCanonicalize(text)
		if n <= maxDepth {
			if err != nil || got != text || perr != nil || parsed != text {
				t.Errorf("depth %d: %q %v / %q %v", n, got, err, parsed, perr)
			}
			continue
		}
		if reason(err) != "depth" || reason(perr) != "depth" {
			t.Errorf("depth %d: reasons %q / %q, want depth", n, reason(err), reason(perr))
		}
	}

	// {"a":"<n x>"} is 8+n bytes.
	at := strings.Repeat("x", 65536-8)
	if got, err := Canonicalize(map[string]any{"a": at}); err != nil || len(got) != 65536 {
		t.Fatalf("65536 bytes: %d %v", len(got), err)
	}
	if _, err := ParseAndCanonicalize(`{"a":"` + at + `"}`); err != nil {
		t.Fatalf("parse 65536 bytes: %v", err)
	}
	if _, err := Canonicalize(map[string]any{"a": at + "x"}); reason(err) != "bounds" {
		t.Fatalf("65537 bytes: reason %q, want bounds", reason(err))
	}
	if _, err := ParseAndCanonicalize(`{"a":"` + at + `x"}`); reason(err) != "bounds" {
		t.Fatalf("parse 65537 bytes: reason %q, want bounds", reason(err))
	}
}

// TestRoundTrip: the text of a Go value is a fixed point of the parser, and
// encoding/json reads it as the same value json.Marshal gives.
func TestRoundTrip(t *testing.T) {
	values := []any{
		map[string]any{},
		[]any{},
		map[string]any{
			"a": []any{true, false, int64(-7), "", "\x00\U00002028\U0001F600"},
			"b": map[string]any{"c": "\U0000FFFD"},
		},
		map[string]any{"z": int64(9007199254740991), "y": int64(-9007199254740991), "x": "18446744073709551615"},
		"top-level string",
		int64(42),
		true,
	}
	for _, value := range values {
		text, err := Canonicalize(value)
		if err != nil {
			t.Fatalf("%v: %v", value, err)
		}
		again, err := ParseAndCanonicalize(text)
		if err != nil || again != text {
			t.Fatalf("%q: parse gives %q %v", text, again, err)
		}
		var fromText, fromValue any
		if err := json.Unmarshal([]byte(text), &fromText); err != nil {
			t.Fatalf("%q: encoding/json: %v", text, err)
		}
		reference, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%v: json.Marshal: %v", value, err)
		}
		if err := json.Unmarshal(reference, &fromValue); err != nil || !reflect.DeepEqual(fromText, fromValue) {
			t.Fatalf("%q: encoding/json reads %v, want %v (%v)", text, fromText, fromValue, err)
		}
	}
}

// containsReplacement reports whether raw JSON text can decode to U+FFFD:
// the character itself, or its six-character escape in either case.
func containsReplacement(raw []byte) bool {
	return bytes.Contains(raw, []byte("\U0000FFFD")) || bytes.Contains(bytes.ToLower(raw), []byte(esc("%ufffd")))
}

// Fuzz input bounds; see FuzzCanonical.
const (
	fuzzMaxRaw = 256
	fuzzMaxKey = 72
)

// FuzzCanonical checks both entry points.
//
// Text: when raw is accepted, the canonical text is valid UTF-8, is a fixed
// point, is JSON that encoding/json reads to the same value as raw, and has
// no U+FFFD unless raw spells one.
//
// Value: an object holding s (as a value, an array item and a nested value),
// n, and s under key k is accepted exactly when s is valid UTF-8, k obeys the
// key rule, and n is a safe integer. Accepted text is a fixed point,
// encoding/json reads every member back unchanged (so distinct strings never
// share a text), and it has no U+FFFD unless s has.
//
// Inputs longer than fuzzMaxRaw (raw, s) or fuzzMaxKey (k) are ignored. The
// fuzzing engine minimizes every new interesting input with a pass whose
// cost is quadratic in its length, and a few mutated inputs of several KiB
// kept all workers minimizing for the whole run. Long input is covered by
// TestShapeRules (the 65536-byte cap), and short strings exhaustively by
// TestEmitStringShortExhaustive.
func FuzzCanonical(f *testing.F) {
	doc := loadVectors(f)
	for _, v := range doc.Valid {
		f.Add([]byte(v.InputRaw), "s", "k", int64(1))
	}
	for _, v := range doc.Invalid {
		raw := []byte(v.Raw)
		if v.RawHex != "" {
			decoded, err := hex.DecodeString(v.RawHex)
			if err != nil {
				f.Fatalf("invalid %s: raw_hex: %v", v.ID, err)
			}
			raw = decoded
		}
		f.Add(raw, "", "", int64(0))
	}
	for _, bad := range invalidUTF8 {
		f.Add([]byte(`{"s":"`+bad+`"}`), bad, "k", int64(-1))
		f.Add([]byte(`["x"]`), "ok", bad, int64(2))
	}
	f.Add([]byte(`{"string":`+rfcString+`,"literals":[true,false]}`), rfcStringValue, "literals", int64(9007199254740991))
	f.Add([]byte(esc(`"%uD83D%uDE00%uFFFD"`)), "\U0000FFFD", "-", int64(-9007199254740991))
	f.Add([]byte(esc(`"%uD800"`)), "\U0001F600\U00002028", "a.b_c-D9", int64(9007199254740992))
	f.Add([]byte("{\"a\":\"\U0000FFFD\"}"), "\x00\x1f\x7f", "", int64(math.MinInt64))

	f.Fuzz(func(t *testing.T, raw []byte, s, k string, n int64) {
		if len(raw) > fuzzMaxRaw || len(s) > fuzzMaxRaw || len(k) > fuzzMaxKey {
			return
		}
		canon, err := ParseAndCanonicalize(string(raw))
		if err == nil {
			if !utf8.ValidString(canon) {
				t.Fatalf("canonical text of %q is not UTF-8", raw)
			}
			again, err := ParseAndCanonicalize(canon)
			if err != nil || again != canon {
				t.Fatalf("not a fixed point: %q -> %q -> %q %v", raw, canon, again, err)
			}
			var fromRaw, fromCanon any
			if err := json.Unmarshal(raw, &fromRaw); err != nil {
				t.Fatalf("accepted %q but encoding/json refuses it: %v", raw, err)
			}
			if err := json.Unmarshal([]byte(canon), &fromCanon); err != nil {
				t.Fatalf("canonical %q is not JSON: %v", canon, err)
			}
			if !reflect.DeepEqual(fromRaw, fromCanon) {
				t.Fatalf("%q and its canonical %q differ: %v vs %v", raw, canon, fromRaw, fromCanon)
			}
			if !containsReplacement(raw) && strings.ContainsRune(canon, utf8.RuneError) {
				t.Fatalf("canonical %q of %q has U+FFFD", canon, raw)
			}
		} else if r := reason(err); r == "" || strings.HasPrefix(r, "not a jcs error") {
			t.Fatalf("%q: error without a reason: %v", raw, err)
		}

		object := map[string]any{
			"s": s,
			"n": n,
			"a": []any{s, map[string]any{"t": s}},
		}
		if _, taken := object[k]; !taken {
			object[k] = s
		}
		text, err := Canonicalize(object)
		allowed := map[string]bool{}
		if !utf8.ValidString(s) {
			allowed["invalid_utf8"] = true
		}
		if checkKey(k) != nil {
			allowed["bad_key"] = true
		}
		if n > safeMax || n < -safeMax {
			allowed["unsafe_integer"] = true
		}
		// No "bounds": each byte of s is at most 6 bytes of text, s appears
		// at most 4 times, and the rest of the object is below 200 bytes, so
		// the text stays far below 65536 bytes.
		if err != nil {
			if !allowed[reason(err)] {
				t.Fatalf("s=%q k=%q n=%d: refused with %q", s, k, n, reason(err))
			}
			if text != "" {
				t.Fatalf("refused with text %q", text)
			}
			return
		}
		for _, r := range []string{"invalid_utf8", "bad_key", "unsafe_integer"} {
			if allowed[r] {
				t.Fatalf("s=%q k=%q n=%d: accepted as %q, want %s", s, k, n, text, r)
			}
		}
		again, err := ParseAndCanonicalize(text)
		if err != nil || again != text {
			t.Fatalf("not a fixed point: %q -> %q %v", text, again, err)
		}
		var back map[string]any
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if err := decoder.Decode(&back); err != nil {
			t.Fatalf("%q: encoding/json: %v", text, err)
		}
		items, _ := back["a"].([]any)
		if len(back) != len(object) || back["s"] != s || back["n"] != json.Number(strconv.FormatInt(n, 10)) ||
			len(items) != 2 || items[0] != s {
			t.Fatalf("%q reads back as %v, want s=%q n=%d", text, back, s, n)
		}
		if inner, _ := items[1].(map[string]any); len(inner) != 1 || inner["t"] != s {
			t.Fatalf("%q reads back nested %v, want %q", text, items[1], s)
		}
		if _, taken := map[string]bool{"s": true, "n": true, "a": true}[k]; !taken && back[k] != s {
			t.Fatalf("%q reads back %q as %v, want %q", text, k, back[k], s)
		}
		if !strings.ContainsRune(s, utf8.RuneError) && strings.ContainsRune(text, utf8.RuneError) {
			t.Fatalf("text %q of s=%q has U+FFFD", text, s)
		}
	})
}
