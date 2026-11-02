package capability

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func testKey(name string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-capability-test-key/" + name))
	return ed25519.NewKeyFromSeed(seed[:])
}

// recordFor is the provider record a peer reads from Query/Provider: id,
// current key id and key, and the committed digest of r.
func recordFor(id, keyID string, key ed25519.PrivateKey, r Result) *providersv1.Provider {
	return &providersv1.Provider{
		ProviderId:    id,
		SigningKeyId:  keyID,
		SigningPubkey: append([]byte(nil), key.Public().(ed25519.PublicKey)...),
		KeyGeneration: 2,
		EndpointManifestDigest: &typesv1.Digest{
			Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
			Value:     append([]byte(nil), r.SHA256[:]...),
		},
	}
}

func encode(t *testing.T, providerID, keyID string, capacity uint64) Result {
	t.Helper()
	r, err := Encode(Manifest{
		ProviderID: providerID, SigningKeyID: keyID, DeclaredCapacityBytes: capacity,
		Endpoints: []Endpoint{{Name: "primary", URL: "https://" + providerID + ".example.invalid/svn"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSignAndVerify(t *testing.T) {
	result, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	key := testKey("key-p1")
	pub := []byte(key.Public().(ed25519.PublicKey))
	sig, err := Sign(result, key)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte("SVN-CAPABILITY-1\x00"), validCanonical...)
	if string(result.SignBytes()) != string(want) {
		t.Fatalf("sign bytes %q", result.SignBytes())
	}
	if !ed25519.Verify(pub, want, sig.GetSignature()) {
		t.Fatal("the signature is not over SVN-CAPABILITY-1 || 0x00 || body")
	}
	if sig.GetKeyId() != "key-p1" || sig.GetDomain() != typesv1.SignatureDomain_SIGNATURE_DOMAIN_CAPABILITY ||
		sig.GetCanonicalProfileVersion() != msgkit.CanonicalProfileVersion || string(sig.GetPublicKey()) != string(pub) {
		t.Fatalf("signature fields %v", sig)
	}
	record := recordFor("p1", "key-p1", key, result)
	if code := Verify(result, sig, record); code != msgkit.OK {
		t.Fatalf("Verify: %s", code)
	}
	// The digest covers the body only; signing does not change it.
	if result.SHA256 != sha256.Sum256([]byte(validCanonical)) {
		t.Fatal("digest is not SHA-256 of the body")
	}

	mutate := func(edit func(s *typesv1.InnerSignature)) *typesv1.InnerSignature {
		c := proto.Clone(sig).(*typesv1.InnerSignature)
		edit(c)
		return c
	}
	// The same body and key under another domain string.
	rotateDomain := msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE, result.Canonical)
	wrongDomain := mutate(func(s *typesv1.InnerSignature) {
		s.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE
		s.Signature = ed25519.Sign(key, rotateDomain)
	})
	stranger := testKey("key-stranger")
	other := encode(t, "p1", "key-p1", 2_000_000)

	// After a rotation the record's current key is key-p1b. A manifest
	// that still names the retired key id, signed by the current key with
	// the current key id in the signature: only the manifest-to-record key
	// id rule rejects it.
	current := testKey("key-p1b")
	staleBody := encode(t, "p1", "key-p1", 1_000_000)
	staleSig := &typesv1.InnerSignature{
		Domain: SignatureDomain, PublicKey: []byte(current.Public().(ed25519.PublicKey)),
		Signature: ed25519.Sign(current, staleBody.SignBytes()), KeyId: "key-p1b",
		CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}
	// A manifest of another provider id, signed correctly by p1's key and
	// naming p1's key id: only the provider id rule rejects it.
	foreign := encode(t, "p2", "key-p1", 1_000_000)
	foreignSig, err := Sign(foreign, key)
	if err != nil {
		t.Fatal(err)
	}
	foreignRecord := recordFor("p1", "key-p1", key, foreign)
	if code := Verify(foreign, foreignSig, recordFor("p2", "key-p1", key, foreign)); code != msgkit.OK {
		t.Fatalf("the foreign fixture does not verify for its own id: %s", code)
	}

	cases := []struct {
		name   string
		result Result
		sig    *typesv1.InnerSignature
		record *providersv1.Provider
		want   msgkit.Code
	}{
		{"unparsed manifest", Result{}, sig, record, msgkit.Malformed},
		{"no record", result, sig, nil, msgkit.NotFound},
		{"nil signature", result, nil, record, msgkit.Malformed},
		{"wrong domain", result, wrongDomain, record, msgkit.WrongDomain},
		{"unknown domain", result, mutate(func(s *typesv1.InnerSignature) { s.Domain = 99 }), record, msgkit.Malformed},
		{"profile 2", result, mutate(func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 2 }), record, msgkit.OutOfRange},
		{"short signature", result, mutate(func(s *typesv1.InnerSignature) { s.Signature = s.Signature[:63] }), record, msgkit.Bounds},
		// Same body, same valid ed25519 signature; only the signature's
		// key id differs from the record's current key id.
		{"signature key id other than the record's", result, mutate(func(s *typesv1.InnerSignature) { s.KeyId = "key-other" }), record, msgkit.Unauthorized},
		{"manifest names a retired key id", staleBody, staleSig, recordFor("p1", "key-p1b", current, staleBody), msgkit.Unauthorized},
		{"manifest names another provider", foreign, foreignSig, foreignRecord, msgkit.Unauthorized},
		{"record key other than the signer's", result, sig, recordFor("p1", "key-p1", stranger, result), msgkit.Unauthorized},
		{"signed by another key", result, mutate(func(s *typesv1.InnerSignature) {
			s.PublicKey = []byte(stranger.Public().(ed25519.PublicKey))
			s.Signature = ed25519.Sign(stranger, result.SignBytes())
		}), record, msgkit.Unauthorized},
		{"another body", other, sig, recordFor("p1", "key-p1", key, other), msgkit.Unauthorized},
		{"flipped signature byte", result, mutate(func(s *typesv1.InnerSignature) { s.Signature[0] ^= 1 }), record, msgkit.Unauthorized},
		{"digest not committed", result, sig, recordFor("p1", "key-p1", key, other), msgkit.Integrity},
		{"committed digest of another algorithm", result, sig, func() *providersv1.Provider {
			r := recordFor("p1", "key-p1", key, result)
			r.EndpointManifestDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
			return r
		}(), msgkit.Integrity},
	}
	for _, c := range cases {
		if got := Verify(c.result, c.sig, c.record); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if _, err := Sign(result, key[:31]); err == nil {
		t.Fatal("Sign accepted a short key")
	}
	if _, err := Sign(Result{}, key); err == nil {
		t.Fatal("Sign accepted an unparsed manifest")
	}
}

// schemaFile is docs/protocol/schemas/provider-capability.schema.json,
// decoded into the keywords this package implements. The top level also
// carries the annotation keywords $schema, $id, and title, which do not
// validate anything.
type schemaFile struct {
	Schema               string              `json:"$schema"`
	ID                   string              `json:"$id"`
	Title                string              `json:"title"`
	Type                 string              `json:"type"`
	AdditionalProperties *bool               `json:"additionalProperties"`
	Required             []string            `json:"required"`
	Properties           map[string]property `json:"properties"`
}

type property struct {
	Const                json.RawMessage     `json:"const"`
	Type                 string              `json:"type"`
	MinLength            *int                `json:"minLength"`
	MaxLength            *int                `json:"maxLength"`
	Pattern              string              `json:"pattern"`
	MinItems             *int                `json:"minItems"`
	MaxItems             *int                `json:"maxItems"`
	Items                *property           `json:"items"`
	AdditionalProperties *bool               `json:"additionalProperties"`
	Required             []string            `json:"required"`
	Properties           map[string]property `json:"properties"`
}

// schemaKeywords is the exact keyword set at every schema position of
// provider-capability.schema.json: the root ($), each entry of a
// properties keyword, and items. TestValidatorMatchesSchemaFile compares
// the value of every validation keyword listed here with the package
// constants. $schema, $id, and title are annotations at the root; they
// validate nothing. schemaDrift fails on any position or keyword that is
// not exactly this table, so a keyword the test does not compare cannot
// appear anywhere in the schema, including a keyword the validator
// implements at a position where it does not (a pattern on an endpoint
// name, a minLength on declared_capacity_bytes, a type on schema_version).
var schemaKeywords = map[string][]string{
	"$":                                            {"$id", "$schema", "additionalProperties", "properties", "required", "title", "type"},
	"$.properties.schema_version":                  {"const"},
	"$.properties.profile":                         {"const"},
	"$.properties.provider_id":                     {"maxLength", "minLength", "pattern", "type"},
	"$.properties.signing_key_id":                  {"maxLength", "minLength", "pattern", "type"},
	"$.properties.declared_capacity_bytes":         {"maxLength", "pattern", "type"},
	"$.properties.endpoints":                       {"items", "maxItems", "minItems", "type"},
	"$.properties.endpoints.items":                 {"additionalProperties", "properties", "required", "type"},
	"$.properties.endpoints.items.properties.name": {"maxLength", "minLength", "type"},
	"$.properties.endpoints.items.properties.url":  {"maxLength", "minLength", "pattern", "type"},
}

// schemaPositions walks a schema document and returns the keywords
// (case-sensitive) at every schema position it reaches: the root, every
// entry of a properties keyword, and an items keyword, at any depth.
func schemaPositions(raw []byte) (map[string][]string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	positions := map[string][]string{}
	var walk func(where string, node map[string]json.RawMessage) error
	walk = func(where string, node map[string]json.RawMessage) error {
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		positions[where] = keys
		if value, ok := node["properties"]; ok {
			var props map[string]map[string]json.RawMessage
			if err := json.Unmarshal(value, &props); err != nil {
				return fmt.Errorf("%s.properties: %w", where, err)
			}
			for name, sub := range props {
				if err := walk(where+".properties."+name, sub); err != nil {
					return err
				}
			}
		}
		if value, ok := node["items"]; ok {
			var sub map[string]json.RawMessage
			if err := json.Unmarshal(value, &sub); err != nil {
				return fmt.Errorf("%s.items: %w", where, err)
			}
			if err := walk(where+".items", sub); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("$", root); err != nil {
		return nil, err
	}
	return positions, nil
}

// schemaDrift compares a schema document with schemaKeywords. It returns
// one sorted line per difference: "+<position>" for a position the table
// does not have, "-<position>" for a table position the document lacks,
// "+<position>.<keyword>" for a keyword the table does not list there, and
// "-<position>.<keyword>" for a listed keyword the document lacks.
func schemaDrift(raw []byte) ([]string, error) {
	got, err := schemaPositions(raw)
	if err != nil {
		return nil, err
	}
	var drift []string
	diff := func(sign, where string, have, other []string) {
		set := map[string]bool{}
		for _, k := range other {
			set[k] = true
		}
		for _, k := range have {
			if !set[k] {
				drift = append(drift, sign+where+"."+k)
			}
		}
	}
	for where, keys := range got {
		want, ok := schemaKeywords[where]
		if !ok {
			drift = append(drift, "+"+where)
		}
		diff("+", where, keys, want)
	}
	for where, want := range schemaKeywords {
		keys, ok := got[where]
		if !ok {
			drift = append(drift, "-"+where)
		}
		diff("-", where, want, keys)
	}
	sort.Strings(drift)
	return drift, nil
}

func schemaBytes(t *testing.T) []byte {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	path := filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "docs", "protocol", "schemas", "provider-capability.schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// readSchema checks that the schema file has exactly the positions and
// keywords of schemaKeywords (schemaDrift), then decodes it strictly into
// the typed structs: a keyword the structs do not model also fails the
// decode (which alone would match keys case-insensitively).
func readSchema(t *testing.T) schemaFile {
	t.Helper()
	raw := schemaBytes(t)
	drift, err := schemaDrift(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("schema keywords differ from the ones the validator implements and this test compares: %v", drift)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var schema schemaFile
	if err := decoder.Decode(&schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

// TestSchemaKeywordCheck: the frozen schema has no drift, and each edited
// copy reports exactly its edit: a keyword the validator does not
// implement (at the root, in a property, in items), an annotation below
// the root, a keyword in another case, a keyword the validator implements
// at a position where it does not, a removed keyword, a new property, and
// a removed property.
func TestSchemaKeywordCheck(t *testing.T) {
	raw := schemaBytes(t)
	if drift, err := schemaDrift(raw); err != nil || len(drift) != 0 {
		t.Fatalf("frozen schema: %v %v", drift, err)
	}
	for _, c := range []struct {
		name, from, to string
		want           []string
	}{
		{"uniqueItems on endpoints", `"minItems": 1,`, `"minItems": 1, "uniqueItems": true,`, []string{"+$.properties.endpoints.uniqueItems"}},
		{"format on url", `"pattern": "^https://"`, `"pattern": "^https://", "format": "uri"`, []string{"+$.properties.endpoints.items.properties.url.format"}},
		{"enum on profile", `{"const": "SVN-JCS-1"}`, `{"const": "SVN-JCS-1", "enum": ["SVN-JCS-1"]}`, []string{"+$.properties.profile.enum"}},
		{"minProperties at the root", `"type": "object",`, `"type": "object", "minProperties": 6,`, []string{"+$.minProperties"}},
		{"title inside a property", `"declared_capacity_bytes": {"type": "string",`, `"declared_capacity_bytes": {"title": "x", "type": "string",`, []string{"+$.properties.declared_capacity_bytes.title"}},
		{"keyword in another case", `"maxItems": 8,`, `"maxItems": 8, "MaxItems": 8,`, []string{"+$.properties.endpoints.MaxItems"}},
		{"pattern on endpoint name", `"name": {"type": "string", "minLength": 1, "maxLength": 64}`, `"name": {"type": "string", "minLength": 1, "maxLength": 64, "pattern": "^[a-z]+$"}`, []string{"+$.properties.endpoints.items.properties.name.pattern"}},
		{"minLength on declared_capacity_bytes", `"declared_capacity_bytes": {"type": "string",`, `"declared_capacity_bytes": {"type": "string", "minLength": 5,`, []string{"+$.properties.declared_capacity_bytes.minLength"}},
		{"type on schema_version", `"schema_version": {"const": 1}`, `"schema_version": {"const": 1, "type": "string"}`, []string{"+$.properties.schema_version.type"}},
		{"maxLength removed from url", `"minLength": 8, "maxLength": 2048,`, `"minLength": 8,`, []string{"-$.properties.endpoints.items.properties.url.maxLength"}},
		{"new property", `"profile": {"const": "SVN-JCS-1"},`, `"profile": {"const": "SVN-JCS-1"}, "region": {},`, []string{"+$.properties.region"}},
		{"property removed", `"schema_version": {"const": 1},`, ``, []string{"-$.properties.schema_version", "-$.properties.schema_version.const"}},
	} {
		edited := strings.Replace(string(raw), c.from, c.to, 1)
		if edited == string(raw) {
			t.Fatalf("%s: the edit did not apply", c.name)
		}
		drift, err := schemaDrift([]byte(edited))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(drift, c.want) {
			t.Errorf("%s: drift %v, want %v", c.name, drift, c.want)
		}
	}
	// A tuple-form items (an array) is not a schema position this table
	// models; the walk reports it as an error.
	tuple := strings.Replace(string(raw), `"items": {`, `"items": [{`, 1)
	tuple = strings.Replace(tuple, `"pattern": "^https://"}
        }
      }`, `"pattern": "^https://"}
        }
      }]`, 1)
	if tuple == string(raw) || !json.Valid([]byte(tuple)) {
		t.Fatal("tuple-form items: the edit did not apply")
	}
	if _, err := schemaDrift([]byte(tuple)); err == nil {
		t.Error("tuple-form items: no error")
	}
}

func intIs(p *int, want int) bool { return p != nil && *p == want }

// TestValidatorMatchesSchemaFile compares the hand-written rules with the
// frozen JSON schema, keyword by keyword. readSchema first requires the
// exact keyword set of schemaKeywords at every schema position; this test
// then compares the value of every validation keyword in that table, so no
// keyword at any position goes uncompared.
func TestValidatorMatchesSchemaFile(t *testing.T) {
	schema := readSchema(t)
	if schema.Type != "object" || schema.Schema != "https://json-schema.org/draft/2020-12/schema" || schema.ID != "svn-provider-capability-v1" {
		t.Fatalf("schema header: type %q, $schema %q, $id %q", schema.Type, schema.Schema, schema.ID)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatal("schema allows additional top-level properties; exactKeys does not")
	}
	required := append([]string(nil), schema.Required...)
	sort.Strings(required)
	if !reflect.DeepEqual(required, topKeys) {
		t.Fatalf("schema required %v, validator keys %v", required, topKeys)
	}
	var props []string
	for name := range schema.Properties {
		props = append(props, name)
	}
	sort.Strings(props)
	if !reflect.DeepEqual(props, topKeys) {
		t.Fatalf("schema properties %v, validator keys %v", props, topKeys)
	}
	var profile string
	if err := json.Unmarshal(schema.Properties["profile"].Const, &profile); err != nil || profile != Profile {
		t.Fatalf("schema profile const %q (%v), validator %q", profile, err, Profile)
	}
	var version int
	if err := json.Unmarshal(schema.Properties["schema_version"].Const, &version); err != nil || version != SchemaVersion {
		t.Fatalf("schema_version const %d (%v), validator %d", version, err, SchemaVersion)
	}
	for _, id := range []string{"provider_id", "signing_key_id"} {
		p := schema.Properties[id]
		if p.Type != "string" || !intIs(p.MinLength, 1) || !intIs(p.MaxLength, msgkit.IDMaxBytes) || p.Pattern != msgkit.IDPattern {
			t.Fatalf("%s: schema %+v, validator id pattern %s max %d", id, p, msgkit.IDPattern, msgkit.IDMaxBytes)
		}
	}
	capacity := schema.Properties["declared_capacity_bytes"]
	if capacity.Type != "string" || !intIs(capacity.MaxLength, MaxCapacityChars) || capacity.Pattern != "^[1-9][0-9]*$" {
		t.Fatalf("declared_capacity_bytes: schema %+v", capacity)
	}
	list := schema.Properties["endpoints"]
	if list.Type != "array" || !intIs(list.MinItems, MinEndpoints) || !intIs(list.MaxItems, MaxEndpoints) || list.Items == nil {
		t.Fatalf("endpoints: schema %+v", list)
	}
	item := list.Items
	if item.Type != "object" || item.AdditionalProperties == nil || *item.AdditionalProperties {
		t.Fatal("schema allows additional endpoint properties")
	}
	itemRequired := append([]string(nil), item.Required...)
	sort.Strings(itemRequired)
	if !reflect.DeepEqual(itemRequired, []string{"name", "url"}) || len(item.Properties) != 2 {
		t.Fatalf("endpoint keys: schema %v", item.Required)
	}
	name, url := item.Properties["name"], item.Properties["url"]
	if name.Type != "string" || !intIs(name.MinLength, 1) || !intIs(name.MaxLength, MaxNameChars) {
		t.Fatalf("endpoint name: schema %+v", name)
	}
	if url.Type != "string" || !intIs(url.MinLength, MinURLChars) || !intIs(url.MaxLength, MaxURLChars) || url.Pattern != "^"+URLPrefix {
		t.Fatalf("endpoint url: schema %+v", url)
	}
}
