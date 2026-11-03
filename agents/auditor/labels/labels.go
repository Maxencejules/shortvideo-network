// Package labels holds the evidence label catalog: the proposed strings
// (decision request DR-09-24) that describe beta evidence to a reader of
// the producer's reports, the reviewer's output, the API, and the UI.
//
// The catalog is evidence-labels.json, embedded in the binary. It names the
// evidence mode explicitly ("Auditor attestation" or "Provider receipt"),
// gives a label and a text for each reported state, the provider acceptance,
// and the EvidenceAccepted event, and lists the limitations of beta
// evidence. Every text describes an attestation. None claims a storage
// proof.
//
// apps/web/src/evidence-labels.ts holds the same catalog:
// JSON.stringify(EVIDENCE_LABELS, null, 2) plus a newline equals
// evidence-labels.json byte for byte. The Go tests in this directory and
// apps/web/test/evidence-labels.test.ts each pin every sentence in their
// own list and check the catalog against the shared rules in
// testdata/label-rules.json: the denied phrases and the allowed negation
// sentences.
//
// Mode keys are the kind names of the chain's evidence.KindName
// (AUDITOR_ATTESTATION, PROVIDER_RECEIPT). State keys are the short state
// names of evidence.StateName (RECORDED, REJECTED, EXPIRED). This package
// imports only the standard library.
package labels

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

//go:embed evidence-labels.json
var embedded []byte

// Fixed catalog values and bounds.
const (
	// CatalogID names this catalog version. The strings are proposed, not
	// approved.
	CatalogID = "svn-evidence-labels-proposed-v0"
	// ClaimLimit is the evidence envelope's fixed claim_limit.
	ClaimLimit = "ATTESTATION_NOT_STORAGE_PROOF"
	// MaxTextBytes bounds every label, text, and limitation string.
	MaxTextBytes = 512
)

// Errors. Every catalog fault wraps ErrInvalidCatalog.
var (
	ErrInvalidCatalog = errors.New("labels: invalid catalog")
	ErrUnknownMode    = errors.New("labels: unknown evidence mode")
	ErrUnknownState   = errors.New("labels: unknown evidence state")
)

// Entry is a short label and its explanatory text.
type Entry struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Modes holds the two evidence modes, keyed by kind name.
type Modes struct {
	AuditorAttestation Entry `json:"AUDITOR_ATTESTATION"`
	ProviderReceipt    Entry `json:"PROVIDER_RECEIPT"`
}

// States holds the three reported evidence states, keyed by short name.
type States struct {
	Recorded Entry `json:"RECORDED"`
	Rejected Entry `json:"REJECTED"`
	Expired  Entry `json:"EXPIRED"`
}

// Catalog is the parsed label catalog. Its field order is the file's key
// order.
type Catalog struct {
	Catalog       string   `json:"catalog"`
	ClaimLimit    string   `json:"claim_limit"`
	Modes         Modes    `json:"modes"`
	States        States   `json:"states"`
	EventAccepted string   `json:"event_accepted"`
	Acceptance    Entry    `json:"acceptance"`
	Limitations   []string `json:"limitations"`
}

// Embedded parses and validates the embedded catalog. Each call returns a
// new value; callers may change it without affecting later calls.
func Embedded() (Catalog, error) {
	return parse(embedded)
}

// Mode returns the entry of an evidence kind name, as evidence.KindName
// spells it. Any other name, including UNSPECIFIED and the reserved numbers,
// is ErrUnknownMode.
func (c Catalog) Mode(kind string) (Entry, error) {
	switch kind {
	case "AUDITOR_ATTESTATION":
		return c.Modes.AuditorAttestation, nil
	case "PROVIDER_RECEIPT":
		return c.Modes.ProviderReceipt, nil
	}
	return Entry{}, fmt.Errorf("%w: %.64q", ErrUnknownMode, kind)
}

// State returns the entry of a short evidence state name, as
// evidence.StateName spells it. Any other name, including UNSPECIFIED and
// the full enum names, is ErrUnknownState.
func (c Catalog) State(state string) (Entry, error) {
	switch state {
	case "RECORDED":
		return c.States.Recorded, nil
	case "REJECTED":
		return c.States.Rejected, nil
	case "EXPIRED":
		return c.States.Expired, nil
	}
	return Entry{}, fmt.Errorf("%w: %.64q", ErrUnknownState, state)
}

// parse decodes one JSON object with no unknown field, no key that is not
// spelled exactly as in the Catalog types, no repeated key, and no trailing
// value, then validates it.
func parse(data []byte) (Catalog, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Catalog
	if err := dec.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Catalog{}, fmt.Errorf("%w: data after the catalog object", ErrInvalidCatalog)
	}
	if err := exactKeys(json.NewDecoder(bytes.NewReader(data)), reflect.TypeOf(c)); err != nil {
		return Catalog{}, fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	if err := c.validate(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

// exactKeys reads one JSON value of Go type t from dec and refuses an
// object key that is not the exact json tag of a field of t, and a key
// that repeats. encoding/json matches keys without regard to case and keeps
// the last of repeated keys; DisallowUnknownFields refuses neither. A value
// whose JSON type does not fit t has no allowed keys; Decode has already
// refused it.
func exactKeys(dec *json.Decoder, t reflect.Type) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('['):
		var elem reflect.Type
		if t != nil && t.Kind() == reflect.Slice {
			elem = t.Elem()
		}
		for dec.More() {
			if err := exactKeys(dec, elem); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	case json.Delim('{'):
		fields := map[string]reflect.Type{}
		if t != nil && t.Kind() == reflect.Struct {
			for i := 0; i < t.NumField(); i++ {
				name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
				fields[name] = t.Field(i).Type
			}
		}
		seen := map[string]bool{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := tok.(string)
			field, ok := fields[key]
			if !ok {
				return fmt.Errorf("key %.64q is not a catalog key", key)
			}
			if seen[key] {
				return fmt.Errorf("key %.64q repeats", key)
			}
			seen[key] = true
			if err := exactKeys(dec, field); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	}
	return nil
}

// field is one human-readable string of the catalog and its path.
type field struct {
	path  string
	value string
}

// texts lists every label, text, and limitation in file order.
func (c Catalog) texts() []field {
	out := []field{
		{"modes.AUDITOR_ATTESTATION.label", c.Modes.AuditorAttestation.Label},
		{"modes.AUDITOR_ATTESTATION.text", c.Modes.AuditorAttestation.Text},
		{"modes.PROVIDER_RECEIPT.label", c.Modes.ProviderReceipt.Label},
		{"modes.PROVIDER_RECEIPT.text", c.Modes.ProviderReceipt.Text},
		{"states.RECORDED.label", c.States.Recorded.Label},
		{"states.RECORDED.text", c.States.Recorded.Text},
		{"states.REJECTED.label", c.States.Rejected.Label},
		{"states.REJECTED.text", c.States.Rejected.Text},
		{"states.EXPIRED.label", c.States.Expired.Label},
		{"states.EXPIRED.text", c.States.Expired.Text},
		{"event_accepted", c.EventAccepted},
		{"acceptance.label", c.Acceptance.Label},
		{"acceptance.text", c.Acceptance.Text},
	}
	for i, text := range c.Limitations {
		out = append(out, field{"limitations." + strconv.Itoa(i), text})
	}
	return out
}

// validate checks the fixed values, that every string is present and
// bounded printable ASCII, and that the limitations are a non-empty list
// without repeats. A missing key decodes to an empty string and fails here.
func (c Catalog) validate() error {
	if c.Catalog != CatalogID {
		return fmt.Errorf("%w: catalog %.64q, want %q", ErrInvalidCatalog, c.Catalog, CatalogID)
	}
	if c.ClaimLimit != ClaimLimit {
		return fmt.Errorf("%w: claim_limit %.64q, want %q", ErrInvalidCatalog, c.ClaimLimit, ClaimLimit)
	}
	if len(c.Limitations) == 0 {
		return fmt.Errorf("%w: no limitations", ErrInvalidCatalog)
	}
	seen := make(map[string]bool, len(c.Limitations))
	for _, text := range c.Limitations {
		if seen[text] {
			return fmt.Errorf("%w: repeated limitation %.64q", ErrInvalidCatalog, text)
		}
		seen[text] = true
	}
	for _, f := range c.texts() {
		if err := checkText(f.value); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidCatalog, f.path, err)
		}
	}
	return nil
}

// checkText accepts 1 to MaxTextBytes bytes of printable ASCII with no
// leading, trailing, or doubled space.
func checkText(text string) error {
	if len(text) == 0 || len(text) > MaxTextBytes {
		return fmt.Errorf("length %d outside 1..%d", len(text), MaxTextBytes)
	}
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] > 0x7e {
			return fmt.Errorf("byte %d is not printable ASCII", i)
		}
	}
	if text[0] == ' ' || text[len(text)-1] == ' ' || strings.Contains(text, "  ") {
		return errors.New("leading, trailing, or doubled space")
	}
	return nil
}
