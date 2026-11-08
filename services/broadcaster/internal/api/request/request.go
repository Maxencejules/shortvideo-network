// Package request decodes the broadcaster's typed intent requests (goal 11
// plan 5.1-5.5) and computes their request digest. The HTTP API decodes
// POST /v1/intents bodies with it; the worker decodes the request JSON and
// authorization context of a SQL-path intent with the same functions, so
// both paths accept the same requests and compute the same digest.
//
// A request is decoded strictly (internal/config/strictjson: no duplicate
// key, no null, no unknown field, no trailing data), every id is checked
// against the protocol id pattern, every digest, key and signature against
// its exact size, and every integer against the chain's bounds, before the
// typed request is converted to its chain message. The caller never names
// the signer or the business id: the kind fixes the signer class, and the
// outbox draws the business id.
package request

import (
	"errors"
	"fmt"
	"regexp"
)

// Class says how a decoding failure is answered: Malformed is a body that
// is not the request shape (HTTP 400), Invalid is a well-formed request
// whose values break a rule (HTTP 422), UnknownKind is a kind that is not
// routed (HTTP 422).
type Class int

// Failure classes.
const (
	Malformed Class = iota + 1
	Invalid
	UnknownKind
)

// Error is a decoding failure. Field names the request field; Rule says
// what the field breaks. Neither ever carries a value from the request.
type Error struct {
	Class Class
	Field string
	Rule  string
}

func (e *Error) Error() string {
	if e.Field == "" {
		return "request: " + e.Rule
	}
	return fmt.Sprintf("request: %s: %s", e.Field, e.Rule)
}

func malformed(field, rule string) error { return &Error{Class: Malformed, Field: field, Rule: rule} }
func invalid(field, rule string) error   { return &Error{Class: Invalid, Field: field, Rule: rule} }

// ClassOf returns the failure class of err, or 0 when err is not an Error.
func ClassOf(err error) Class {
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	return 0
}

// kindRE bounds a kind name before it is looked up.
var kindRE = regexp.MustCompile(`^[A-Z][A-Z_]{0,39}$`)
