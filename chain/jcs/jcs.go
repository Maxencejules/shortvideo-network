package jcs

// SVN-JCS-1, the restricted RFC 8785 profile frozen in protocol v0.
// The parser is the same one Goal 02 checks against the vectors.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	safeMax  int64 = 9007199254740991
	maxDepth       = 8
)

type jcsError struct{ reason string }

func (e *jcsError) Error() string { return e.reason }

func fail(reason string) error { return &jcsError{reason: reason} }

func ParseAndCanonicalize(raw string) (string, error) {
	return parseBytes([]byte(raw))
}

func parseBytes(raw []byte) (string, error) {
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return "", fail("bom")
	}
	if !utf8.Valid(raw) {
		return "", fail("invalid_utf8")
	}
	value, index, err := parseValue(string(raw), 0, 0)
	if err != nil {
		return "", err
	}
	index = skip(string(raw), index)
	if index != len(raw) {
		return "", fail("trailing")
	}
	return Canonicalize(value)
}

func Canonicalize(value any) (string, error) {
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
