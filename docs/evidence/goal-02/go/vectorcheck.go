// Vector checker for SVN-JCS-1 and uint64 arithmetic.
// The Ed25519 seed is the public RFC 8032 test vector.
// RFC8032_TEST_VECTOR_NOT_A_SECRET. Do not use it outside these fixtures.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	safeMax int64 = 9007199254740991
	maxDepth      = 8
)

type vectors struct {
	Valid    []validVec `json:"valid"`
	Invalid  []invalid  `json:"invalid"`
	Integers []intVec   `json:"integers"`
	Arith    []arithVec `json:"arithmetic"`
	RFCSig   string     `json:"rfc8032_empty_signature"`
	Seed     string     `json:"rfc8032_seed"`
	Sign     []signVec  `json:"sign"`
}

type validVec struct {
	ID        string `json:"id"`
	InputRaw  string `json:"input_raw"`
	Canonical string `json:"canonical"`
}
type invalid struct {
	ID     string `json:"id"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}
type intVec struct {
	ID      string `json:"id"`
	Decimal string `json:"decimal"`
	Safe    bool   `json:"safe"`
	Uint64  bool   `json:"uint64"`
}
type arithVec struct {
	ID     string `json:"id"`
	Op     string `json:"op"`
	A      string `json:"a"`
	B      string `json:"b"`
	Result string `json:"result"`
}
type signVec struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	VectorID string `json:"vector_id"`
}

type jcsError struct{ reason string }

func (e *jcsError) Error() string { return e.reason }

func fail(reason string) error { return &jcsError{reason: reason} }

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: vectorcheck vectors.json")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var doc vectors
	if err := json.Unmarshal(raw, &doc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	canonicalByID := map[string]string{}
	for _, item := range doc.Valid {
		got, err := parseAndCanonicalize(item.InputRaw)
		if err != nil {
			fmt.Printf("VALID %s ERROR %s\n", item.ID, err.Error())
			os.Exit(1)
		}
		if got != item.Canonical {
			fmt.Printf("VALID %s MISMATCH\n", item.ID)
			os.Exit(1)
		}
		sum := sha256.Sum256([]byte(got))
		fmt.Printf("VALID %s %x\n", item.ID, sum)
		canonicalByID[item.ID] = got
	}
	for _, item := range doc.Invalid {
		_, err := parseAndCanonicalize(item.Raw)
		if err == nil {
			fmt.Printf("INVALID %s MISSED\n", item.ID)
			os.Exit(1)
		}
		jerr, ok := err.(*jcsError)
		if !ok || jerr.reason != item.Reason {
			fmt.Printf("INVALID %s %s\n", item.ID, err.Error())
			os.Exit(1)
		}
		fmt.Printf("INVALID %s %s\n", item.ID, item.Reason)
	}
	for _, item := range doc.Integers {
		safe, fits := classifyDecimal(item.Decimal)
		if safe != item.Safe || fits != item.Uint64 {
			fmt.Printf("INTEGER %s MISMATCH\n", item.ID)
			os.Exit(1)
		}
		label := "unsafe"
		if safe {
			label = "safe"
		}
		width := "over"
		if fits {
			width = "uint64"
		}
		fmt.Printf("INTEGER %s %s %s\n", item.ID, label, width)
	}
	for _, item := range doc.Arith {
		got := applyArith(item.Op, item.A, item.B)
		if got != item.Result {
			fmt.Printf("ARITH %s MISMATCH %s\n", item.ID, got)
			os.Exit(1)
		}
		fmt.Printf("ARITH %s %s\n", item.ID, got)
	}
	seed, err := hex.DecodeString(doc.Seed)
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "bad rfc seed")
		os.Exit(1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	emptySig := ed25519.Sign(priv, nil)
	if hex.EncodeToString(emptySig) != doc.RFCSig {
		fmt.Println("RFC8032 MISMATCH")
		os.Exit(1)
	}
	fmt.Printf("RFC8032 %s\n", hex.EncodeToString(emptySig))
	for _, item := range doc.Sign {
		canon, ok := canonicalByID[item.VectorID]
		if !ok {
			fmt.Printf("SIGN %s MISSING\n", item.ID)
			os.Exit(1)
		}
		input := append([]byte(item.Domain), 0)
		input = append(input, []byte(canon)...)
		sum := sha256.Sum256(input)
		sig := ed25519.Sign(priv, input)
		fmt.Printf("SIGN %s %x %s\n", item.ID, sum, hex.EncodeToString(sig))
	}
}

func classifyDecimal(decimal string) (bool, bool) {
	n := new(big.Int)
	if _, ok := n.SetString(decimal, 10); !ok {
		return false, false
	}
	abs := new(big.Int).Abs(n)
	limit := big.NewInt(safeMax)
	safe := abs.Cmp(limit) <= 0
	if n.Sign() < 0 {
		return safe, false
	}
	maxU := new(big.Int).SetUint64(^uint64(0))
	return safe, n.Cmp(maxU) <= 0
}

func applyArith(op, a, b string) string {
	ua, errA := strconv.ParseUint(a, 10, 64)
	ub, errB := strconv.ParseUint(b, 10, 64)
	if errA != nil || errB != nil {
		return "malformed"
	}
	if op == "add" {
		if ua > ^uint64(0)-ub {
			return "overflow"
		}
		return strconv.FormatUint(ua+ub, 10)
	}
	if op == "sub" {
		if ua < ub {
			return "underflow"
		}
		return strconv.FormatUint(ua-ub, 10)
	}
	return "malformed"
}

func parseAndCanonicalize(raw string) (string, error) {
	value, index, err := parseValue(raw, 0, 0)
	if err != nil {
		return "", err
	}
	index = skip(raw, index)
	if index != len(raw) {
		return "", fail("trailing")
	}
	return canonicalize(value)
}

func canonicalize(value any) (string, error) {
	text, err := emit(value, 0)
	if err != nil {
		return "", err
	}
	if len(text) > 65536 {
		return "", fail("bounds")
	}
	return text, nil
}

func emit(value any, depth int) (string, error) {
	if depth > maxDepth {
		return "", fail("depth")
	}
	switch typed := value.(type) {
	case nil:
		return "", fail("null")
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	case int64:
		if typed > safeMax || typed < -safeMax {
			return "", fail("unsafe_integer")
		}
		return strconv.FormatInt(typed, 10), nil
	case string:
		return emitString(typed), nil
	case []any:
		parts := make([]string, len(typed))
		for i, item := range typed {
			part, err := emit(item, depth+1)
			if err != nil {
				return "", err
			}
			parts[i] = part
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if err := checkKey(key); err != nil {
				return "", err
			}
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return compareUTF16(keys[i], keys[j]) < 0 })
		parts := make([]string, len(keys))
		for i, key := range keys {
			part, err := emit(typed[key], depth+1)
			if err != nil {
				return "", err
			}
			parts[i] = emitString(key) + ":" + part
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", fail("malformed")
	}
}

func emitString(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func compareUTF16(a, b string) int {
	aa := utf16.Encode([]rune(a))
	bb := utf16.Encode([]rune(b))
	n := len(aa)
	if len(bb) < n {
		n = len(bb)
	}
	for i := 0; i < n; i++ {
		if aa[i] < bb[i] {
			return -1
		}
		if aa[i] > bb[i] {
			return 1
		}
	}
	return len(aa) - len(bb)
}

func checkKey(key string) error {
	if key == "" || len(key) > 64 {
		return fail("bad_key")
	}
	for _, r := range key {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-') {
			return fail("bad_key")
		}
	}
	return nil
}

func skip(raw string, index int) int {
	for index < len(raw) {
		switch raw[index] {
		case ' ', '\t', '\r', '\n':
			index++
		default:
			return index
		}
	}
	return index
}

func parseValue(raw string, index, depth int) (any, int, error) {
	if depth > maxDepth {
		return nil, index, fail("depth")
	}
	index = skip(raw, index)
	if index >= len(raw) {
		return nil, index, fail("malformed")
	}
	switch raw[index] {
	case '{':
		return parseObject(raw, index, depth)
	case '[':
		return parseArray(raw, index, depth)
	case '"':
		return parseString(raw, index)
	case 't':
		return literal(raw, index, "true", true)
	case 'f':
		return literal(raw, index, "false", false)
	case 'n':
		return nil, index, fail("null")
	default:
		if raw[index] == '-' || (raw[index] >= '0' && raw[index] <= '9') {
			return parseNumber(raw, index)
		}
		return nil, index, fail("malformed")
	}
}

func literal(raw string, index int, text string, value bool) (any, int, error) {
	if !strings.HasPrefix(raw[index:], text) {
		return nil, index, fail("unknown_literal")
	}
	return value, index + len(text), nil
}

func parseNumber(raw string, index int) (any, int, error) {
	start := index
	if raw[index] == '-' {
		index++
		if index >= len(raw) || raw[index] < '0' || raw[index] > '9' {
			return nil, index, fail("malformed")
		}
	}
	if index < len(raw) && raw[index] == '0' {
		index++
		if index < len(raw) && raw[index] >= '0' && raw[index] <= '9' {
			return nil, index, fail("leading_zero")
		}
	} else {
		if index >= len(raw) || raw[index] < '0' || raw[index] > '9' {
			return nil, index, fail("malformed")
		}
		for index < len(raw) && raw[index] >= '0' && raw[index] <= '9' {
			index++
		}
	}
	if index < len(raw) && (raw[index] == '.' || raw[index] == 'e' || raw[index] == 'E') {
		if raw[index] == '.' {
			return nil, index, fail("fraction")
		}
		return nil, index, fail("exponent")
	}
	token := raw[start:index]
	if token == "-0" || token == "-" {
		return nil, index, fail("negative_zero")
	}
	value, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return nil, index, fail("unsafe_integer")
	}
	if value > safeMax || value < -safeMax {
		return nil, index, fail("unsafe_integer")
	}
	return value, index, nil
}

func parseString(raw string, index int) (any, int, error) {
	if raw[index] != '"' {
		return nil, index, fail("malformed")
	}
	index++
	var b strings.Builder
	for index < len(raw) {
		if raw[index] == '"' {
			return b.String(), index + 1, nil
		}
		if raw[index] == '\\' {
			index++
			if index >= len(raw) {
				return nil, index, fail("malformed")
			}
			esc := raw[index]
			switch esc {
			case 'b':
				b.WriteByte('\b')
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'f':
				b.WriteByte('\f')
			case 'r':
				b.WriteByte('\r')
			case '"', '\\', '/':
				b.WriteByte(esc)
			case 'u':
				if index+5 > len(raw) {
					return nil, index, fail("malformed")
				}
				hexDigits := raw[index+1 : index+5]
				code, err := strconv.ParseUint(hexDigits, 16, 16)
				if err != nil {
					return nil, index, fail("malformed")
				}
				index += 5
				if code >= 0xD800 && code <= 0xDBFF {
					if index+6 > len(raw) || raw[index:index+2] != `\u` {
						return nil, index, fail("lone_surrogate")
					}
					low, err := strconv.ParseUint(raw[index+2:index+6], 16, 16)
					if err != nil || low < 0xDC00 || low > 0xDFFF {
						return nil, index, fail("lone_surrogate")
					}
					code = 0x10000 + ((code - 0xD800) << 10) + (low - 0xDC00)
					index += 6
					b.WriteRune(rune(code))
					continue
				}
				if code >= 0xDC00 && code <= 0xDFFF {
					return nil, index, fail("lone_surrogate")
				}
				b.WriteRune(rune(code))
				continue
			default:
				return nil, index, fail("malformed")
			}
			index++
			continue
		}
		if raw[index] < 0x20 {
			return nil, index, fail("malformed")
		}
		r, size := utf8.DecodeRuneInString(raw[index:])
		if r == utf8.RuneError && size == 1 {
			return nil, index, fail("malformed")
		}
		b.WriteRune(r)
		index += size
	}
	return nil, index, fail("malformed")
}

func parseObject(raw string, index, depth int) (any, int, error) {
	index++
	obj := map[string]any{}
	index = skip(raw, index)
	if index < len(raw) && raw[index] == '}' {
		return obj, index + 1, nil
	}
	for index < len(raw) {
		index = skip(raw, index)
		keyVal, next, err := parseString(raw, index)
		if err != nil {
			return nil, index, err
		}
		key := keyVal.(string)
		if _, exists := obj[key]; exists {
			return nil, next, fail("duplicate_key")
		}
		if err := checkKey(key); err != nil {
			return nil, next, err
		}
		index = skip(raw, next)
		if index >= len(raw) || raw[index] != ':' {
			return nil, index, fail("malformed")
		}
		value, next, err := parseValue(raw, index+1, depth+1)
		if err != nil {
			return nil, next, err
		}
		obj[key] = value
		index = skip(raw, next)
		if index >= len(raw) {
			return nil, index, fail("malformed")
		}
		if raw[index] == '}' {
			return obj, index + 1, nil
		}
		if raw[index] != ',' {
			return nil, index, fail("malformed")
		}
		index++
		index = skip(raw, index)
		if index < len(raw) && raw[index] == '}' {
			return nil, index, fail("trailing_comma")
		}
	}
	return nil, index, fail("malformed")
}

func parseArray(raw string, index, depth int) (any, int, error) {
	index++
	items := []any{}
	index = skip(raw, index)
	if index < len(raw) && raw[index] == ']' {
		return items, index + 1, nil
	}
	for index < len(raw) {
		value, next, err := parseValue(raw, index, depth+1)
		if err != nil {
			return nil, next, err
		}
		items = append(items, value)
		index = skip(raw, next)
		if index >= len(raw) {
			return nil, index, fail("malformed")
		}
		if raw[index] == ']' {
			return items, index + 1, nil
		}
		if raw[index] != ',' {
			return nil, index, fail("malformed")
		}
		index++
		index = skip(raw, index)
		if index < len(raw) && raw[index] == ']' {
			return nil, index, fail("trailing_comma")
		}
	}
	return nil, index, fail("malformed")
}
