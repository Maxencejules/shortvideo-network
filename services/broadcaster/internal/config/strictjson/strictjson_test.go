package strictjson

import (
	"errors"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	ok := []string{
		`{}`, `{"a":1,"b":{"c":[1,2,{"d":"x"}]}}`, ` {"a" : "b"} `, `[1,2]`, `"s"`, `1`, `true`,
		`{"a":{"a":1}}`, // the same key at two depths is not a duplicate
	}
	for _, s := range ok {
		if err := Check([]byte(s)); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	deep := strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth)
	if err := Check([]byte(deep)); err != nil {
		t.Fatalf("depth %d refused: %v", MaxDepth, err)
	}
	bad := map[string]error{
		``:                             ErrEmpty,
		`   `:                          ErrEmpty,
		`{"a":1,"a":2}`:                ErrDuplicate,
		`{"a":1,"\u0061":2}`:           ErrDuplicate,
		`{"x":{"b":1,"b":1}}`:          ErrDuplicate,
		`[{"b":1,"b":1}]`:              ErrDuplicate,
		`null`:                         ErrNull,
		`{"a":null}`:                   ErrNull,
		`[1,null]`:                     ErrNull,
		`{} {}`:                        ErrTrailing,
		`{}x`:                          ErrTrailing,
		`{"a":}`:                       ErrSyntax,
		`{"a" 1}`:                      ErrSyntax,
		`{`:                            ErrSyntax,
		"{\"a\":\"\xff\"}":             ErrUTF8,
		"[" + deep + "]":               ErrDepth,
		`{"a":[1,2}`:                   ErrSyntax,
		`{"a":1,}`:                     ErrSyntax,
		`{"b":{"c":{"d":1,"d":true}}}`: ErrDuplicate,
	}
	for s, want := range bad {
		if err := Check([]byte(s)); !errors.Is(err, want) {
			t.Fatalf("%q: got %v, want %v", s, err, want)
		}
	}
}

type target struct {
	A string `json:"a"`
	N uint64 `json:"n"`
	I int64  `json:"i"`
}

func TestDecode(t *testing.T) {
	var v target
	if err := Decode([]byte(`{"a":"x","n":5,"i":-2}`), &v); err != nil || v != (target{"x", 5, -2}) {
		t.Fatalf("%+v %v", v, err)
	}
	for _, s := range []string{
		`{"a":"x","extra":1}`, `{"n":"5"}`, `{"n":1.5}`, `{"n":-1}`, `{"n":1e2}`, `{"a":5}`, `[1]`,
	} {
		var v target
		err := Decode([]byte(s), &v)
		if !errors.Is(err, ErrShape) {
			t.Fatalf("%s: %v", s, err)
		}
		if strings.Contains(err.Error(), "extra") {
			t.Fatalf("error quotes a document key: %v", err)
		}
	}
	if err := Decode([]byte(`{"a":null}`), &v); !errors.Is(err, ErrNull) {
		t.Fatalf("null: %v", err)
	}
	if err := Decode([]byte(`{"a":"x","a":"y"}`), &v); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
}
