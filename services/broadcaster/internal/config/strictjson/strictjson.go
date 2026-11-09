// Package strictjson decodes JSON documents the broadcaster reads from
// operators (configuration) and from callers (API requests) under one rule
// set: one document per input, no duplicate object key at any depth, no
// null value, a bounded nesting depth, valid UTF-8, and no field the target
// struct does not declare.
//
// encoding/json alone keeps the last of two duplicate keys, accepts null for
// any field and accepts unknown fields unless told otherwise, so two
// documents that mean different things could decode to the same value. The
// checks here make the accepted document the only spelling of what was
// decoded, apart from whitespace, key order and string escapes.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// MaxDepth bounds the nesting of objects and arrays in a checked document.
const MaxDepth = 16

// Errors. Their text names the rule, never a value from the document.
var (
	ErrEmpty     = errors.New("strictjson: empty document")
	ErrUTF8      = errors.New("strictjson: document is not valid UTF-8")
	ErrSyntax    = errors.New("strictjson: document is not valid JSON")
	ErrTrailing  = errors.New("strictjson: data after the JSON document")
	ErrDuplicate = errors.New("strictjson: duplicate object key")
	ErrNull      = errors.New("strictjson: null value")
	ErrDepth     = errors.New("strictjson: nesting deeper than the bound")
	ErrShape     = errors.New("strictjson: document does not match the expected fields")
)

// Check reports whether raw is exactly one JSON value with no duplicate
// object key, no null and a nesting depth of at most MaxDepth.
func Check(raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ErrEmpty
	}
	if !utf8.Valid(raw) {
		return ErrUTF8
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := checkValue(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrTrailing
	}
	return nil
}

// checkValue reads one value from dec at nesting depth depth.
func checkValue(dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return ErrSyntax
	}
	switch t := tok.(type) {
	case nil:
		return ErrNull
	case json.Delim:
		if depth >= MaxDepth {
			return ErrDepth
		}
		switch t {
		case '{':
			return checkObject(dec, depth+1)
		case '[':
			return checkArray(dec, depth+1)
		}
		return ErrSyntax
	}
	return nil
}

func checkObject(dec *json.Decoder, depth int) error {
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return ErrSyntax
		}
		key, ok := tok.(string)
		if !ok {
			return ErrSyntax
		}
		if seen[key] {
			return ErrDuplicate
		}
		seen[key] = true
		if err := checkValue(dec, depth); err != nil {
			return err
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return ErrSyntax
	}
	return nil
}

func checkArray(dec *json.Decoder, depth int) error {
	for dec.More() {
		if err := checkValue(dec, depth); err != nil {
			return err
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim(']') {
		return ErrSyntax
	}
	return nil
}

// Decode runs Check on raw, then decodes it into v (a pointer to a struct)
// refusing every field v does not declare. A value of the wrong JSON type
// for its field (a string for a number, a fraction or an exponent for an
// integer, a negative number for an unsigned field) is ErrShape.
func Decode(raw []byte, v any) error {
	if err := Check(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %s", ErrShape, describe(err))
	}
	return nil
}

// describe names what went wrong in a decode error without quoting the
// document: the field for a type mismatch, "unknown field" otherwise.
func describe(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		if te.Field != "" {
			return "field " + te.Field + " has the wrong type"
		}
		return "value has the wrong type"
	}
	return "unknown field or malformed value"
}
