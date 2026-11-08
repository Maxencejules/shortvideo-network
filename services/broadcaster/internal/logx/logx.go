// Package logx is the broadcaster's structured logger: log/slog JSON
// output whose ReplaceAttr hook drops every attribute that could carry a
// secret or a caller's data (plan 7.7).
//
// The hook keeps only plain scalar values under attribute names that are
// not on the deny list. It drops:
//
//   - any attribute whose name (or the name of an enclosing group) names
//     key material, a signature, transaction bytes, a connection string, a
//     password, a certificate, an authorization context, a token, or a
//     request or response body;
//   - every []byte value, whatever its name, and every value that is not a
//     string, number, bool, duration, time or error (structs, maps and
//     slices are marshalled by the JSON handler and could carry anything);
//   - every string longer than MaxStringBytes (an encoded transaction, a
//     body or a key in hex or base64 is long), and every string that starts
//     like a JSON document ('{' or '[' after leading space): a body, an
//     authorization context or a decoded request is never a log value.
//
// A dropped attribute keeps its name and gets the value "[redacted]", so a
// reader sees that something was there. Values are meant to be logged as
// hashes or ids; the hook is a second guard, not a licence to log data.
package logx

import (
	"io"
	"log/slog"
	"strings"
)

// Redacted replaces a dropped value.
const Redacted = "[redacted]"

// MaxStringBytes bounds a logged string value, the message included. Ids
// (at most 64 bytes), hex hashes (64 bytes), type URLs and short refusal
// details fit; encoded transactions, bodies and keys do not.
const MaxStringBytes = 256

// MaxErrorBytes bounds the text of a logged error value. Longer error
// text is cut, not dropped, so the start of the cause stays visible.
const MaxErrorBytes = 512

// deniedParts are the normalized name fragments (lowercase, without '_',
// '-', '.' or ' ') that mark an attribute as sensitive. A name matches when
// it contains any fragment. "key" also matches public keys and idempotency
// keys: a key's name is logged under "signer", never under a key name.
var deniedParts = []string{
	"key",           // key material, key bytes, key files
	"privat",        // private anything
	"seed",          // key seeds
	"mnemonic",      // key phrases
	"secret",        // any secret
	"passw",         // password, passwd
	"passfile",      // database password files
	"dsn",           // connection strings
	"conninfo",      // connection strings
	"credential",    // credentials
	"token",         // bearer and session tokens
	"cookie",        // cookies
	"authorizat",    // authorization headers
	"authcontext",   // the authorized request context (plan 5.5)
	"cert",          // certificates
	"signature",     // inner and outer signatures
	"acceptance",    // provider acceptance (an inner signature)
	"hostingintent", // hosting intents (an inner signature)
	"txbytes",       // encoded transactions
	"rawtx",         // encoded transactions
	"body",          // request and response bodies
	"payload",       // request payloads
	"preimage",      // sign bytes
	"signdoc",       // sign bytes
}

// deniedNames are normalized names denied when they match exactly: short
// names that are too common as fragments ("sig" is in "signer").
var deniedNames = []string{"sig", "sigs", "tx", "raw", "data", "bytes", "request", "req", "response", "resp"}

// normalize lowercases name and removes separators.
func normalize(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		switch r {
		case '_', '-', '.', ' ':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Denied reports whether an attribute or group name is on the deny list.
func Denied(name string) bool {
	n := normalize(name)
	for _, part := range deniedParts {
		if strings.Contains(n, part) {
			return true
		}
	}
	for _, exact := range deniedNames {
		if n == exact {
			return true
		}
	}
	return false
}

// Replace is the ReplaceAttr hook (see the package comment). groups are the
// names of the groups that enclose a; a denied group name redacts every
// attribute inside it.
func Replace(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 {
		switch a.Key {
		case slog.TimeKey, slog.LevelKey:
			return a
		case slog.MessageKey:
			if a.Value.Kind() == slog.KindString && len(a.Value.String()) <= MaxStringBytes && !jsonLike(a.Value.String()) {
				return a
			}
			return slog.String(a.Key, Redacted)
		}
	}
	for _, g := range groups {
		if Denied(g) {
			return slog.String(a.Key, Redacted)
		}
	}
	if Denied(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	v := a.Value
	switch v.Kind() {
	case slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindBool, slog.KindDuration, slog.KindTime:
		return a
	case slog.KindString:
		if len(v.String()) > MaxStringBytes || jsonLike(v.String()) {
			return slog.String(a.Key, Redacted)
		}
		return a
	case slog.KindAny:
		if err, ok := v.Any().(error); ok && err != nil {
			text := err.Error()
			if len(text) > MaxErrorBytes {
				text = text[:MaxErrorBytes]
			}
			return slog.String(a.Key, text)
		}
	}
	return slog.String(a.Key, Redacted)
}

// jsonLike reports whether s starts like a JSON object or array.
func jsonLike(s string) bool {
	t := strings.TrimLeft(s, " \t\r\n")
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// NewHandler returns a JSON handler that writes to w at level and above,
// with Replace as its ReplaceAttr hook and no source locations.
func NewHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: Replace})
}

// New returns a logger over NewHandler(w, level).
func New(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(NewHandler(w, level))
}

// Discard returns a logger that writes nothing.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
