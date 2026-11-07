package labels

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// mutated returns the embedded catalog with one change applied to its
// decoded JSON object, re-encoded.
func mutated(t *testing.T, change func(doc map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(embedded, &doc); err != nil {
		t.Fatal(err)
	}
	change(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func object(t *testing.T, doc map[string]any, keys ...string) map[string]any {
	t.Helper()
	cur := doc
	for _, key := range keys {
		next, ok := cur[key].(map[string]any)
		if !ok {
			t.Fatalf("no object at %v", keys)
		}
		cur = next
	}
	return cur
}

func TestEmbeddedCatalogParses(t *testing.T) {
	c, err := parse(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.texts()); got != 13+len(c.Limitations) {
		t.Fatalf("texts lists %d strings, want %d", got, 13+len(c.Limitations))
	}
}

// Every catalog fault is refused with ErrInvalidCatalog: a strict parse,
// the fixed values, presence, bounds, and the limitation list.
func TestParseRejects(t *testing.T) {
	long := strings.Repeat("a", MaxTextBytes+1)
	cases := []struct {
		name string
		data []byte
	}{
		{"not JSON", []byte("labels")},
		{"empty input", nil},
		{"JSON array", []byte("[]")},
		{"data after the object", append(append([]byte(nil), embedded...), []byte("{}")...)},
		{"unknown top-level field", mutated(t, func(d map[string]any) { d["storage_proof"] = "x" })},
		{"third mode", mutated(t, func(d map[string]any) {
			object(t, d, "modes")["STORAGE_PROOF"] = map[string]any{"label": "x", "text": "x"}
		})},
		{"unknown state", mutated(t, func(d map[string]any) {
			object(t, d, "states")["EVIDENCE_STATE_RECORDED"] = map[string]any{"label": "x", "text": "x"}
		})},
		{"unknown entry field", mutated(t, func(d map[string]any) { object(t, d, "acceptance")["independent"] = true })},
		{"wrong catalog id", mutated(t, func(d map[string]any) { d["catalog"] = "svn-evidence-labels-v1" })},
		{"wrong claim limit", mutated(t, func(d map[string]any) { d["claim_limit"] = "STORAGE_PROOF" })},
		{"missing claim limit", mutated(t, func(d map[string]any) { delete(d, "claim_limit") })},
		{"missing state", mutated(t, func(d map[string]any) { delete(object(t, d, "states"), "EXPIRED") })},
		{"missing mode", mutated(t, func(d map[string]any) { delete(object(t, d, "modes"), "PROVIDER_RECEIPT") })},
		{"empty label", mutated(t, func(d map[string]any) { object(t, d, "modes", "AUDITOR_ATTESTATION")["label"] = "" })},
		{"empty event text", mutated(t, func(d map[string]any) { d["event_accepted"] = "" })},
		{"text not a string", mutated(t, func(d map[string]any) { object(t, d, "states", "RECORDED")["text"] = 1 })},
		{"non-ASCII text", mutated(t, func(d map[string]any) {
			object(t, d, "states", "EXPIRED")["text"] = "The attestation’s expiry time has passed."
		})},
		{"control byte", mutated(t, func(d map[string]any) { d["event_accepted"] = "Written.\nPassed." })},
		{"text over the bound", mutated(t, func(d map[string]any) { object(t, d, "acceptance")["text"] = long })},
		{"leading space", mutated(t, func(d map[string]any) { object(t, d, "acceptance")["label"] = " Provider acceptance" })},
		{"trailing space", mutated(t, func(d map[string]any) { d["limitations"] = []any{"Auditors can lie. "} })},
		{"doubled space", mutated(t, func(d map[string]any) { d["limitations"] = []any{"Auditors  can lie."} })},
		{"no limitations", mutated(t, func(d map[string]any) { d["limitations"] = []any{} })},
		{"null limitations", mutated(t, func(d map[string]any) { d["limitations"] = nil })},
		{"repeated limitation", mutated(t, func(d map[string]any) {
			d["limitations"] = []any{"Auditors can lie.", "Auditors can lie."}
		})},
		{"empty limitation", mutated(t, func(d map[string]any) { d["limitations"] = []any{"Auditors can lie.", ""} })},
		// encoding/json alone accepts these three: it matches keys without
		// regard to case and keeps the last of repeated keys.
		{"lower-case state key", mutated(t, func(d map[string]any) {
			states := object(t, d, "states")
			states["recorded"] = states["RECORDED"]
			delete(states, "RECORDED")
		})},
		{"upper-case claim_limit key", mutated(t, func(d map[string]any) {
			d["CLAIM_LIMIT"] = d["claim_limit"]
			delete(d, "claim_limit")
		})},
		{"repeated label key", repeatedLabel(t)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parse(tc.data)
			if !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("parse = %v, want ErrInvalidCatalog", err)
			}
		})
	}
}

// repeatedLabel returns the embedded catalog with a second "label" key
// placed before the first mode's label; the last value is the real one.
func repeatedLabel(t *testing.T) []byte {
	t.Helper()
	const label = `"label": "Auditor attestation"`
	if bytes.Count(embedded, []byte(label)) != 1 {
		t.Fatalf("the catalog does not hold %s once", label)
	}
	return bytes.Replace(embedded, []byte(label), []byte(`"label": "Proof of storage", `+label), 1)
}

// The text bound is inclusive at MaxTextBytes.
func TestParseTextBound(t *testing.T) {
	exact := strings.Repeat("a", MaxTextBytes)
	data := mutated(t, func(d map[string]any) { object(t, d, "acceptance")["text"] = exact })
	c, err := parse(data)
	if err != nil {
		t.Fatalf("a %d-byte text: %v", MaxTextBytes, err)
	}
	if c.Acceptance.Text != exact {
		t.Fatal("the bounded text did not round-trip")
	}
}
