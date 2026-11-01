package capability

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func testKey(name string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-capability-test-key/" + name))
	return ed25519.NewKeyFromSeed(seed[:])
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
	if code := Verify(result, sig, pub); code != msgkit.OK {
		t.Fatalf("Verify: %s", code)
	}
	// The digest covers the body only; signing does not change it.
	if result.SHA256 != sha256.Sum256([]byte(validCanonical)) {
		t.Fatal("digest is not SHA-256 of the body")
	}

	other, err := Encode(Manifest{
		ProviderID: "p1", SigningKeyID: "key-p1", DeclaredCapacityBytes: 2_000_000,
		Endpoints: []Endpoint{{Name: "primary", URL: "https://p1.example.invalid/svn"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	otherKeyID, err := Encode(Manifest{
		ProviderID: "p1", SigningKeyID: "key-p1b", DeclaredCapacityBytes: 1_000_000,
		Endpoints: []Endpoint{{Name: "primary", URL: "https://p1.example.invalid/svn"}},
	})
	if err != nil {
		t.Fatal(err)
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
	cases := []struct {
		name   string
		result Result
		sig    *typesv1.InnerSignature
		pub    []byte
		want   msgkit.Code
	}{
		{"nil signature", result, nil, pub, msgkit.Malformed},
		{"wrong domain", result, wrongDomain, pub, msgkit.WrongDomain},
		{"unknown domain", result, mutate(func(s *typesv1.InnerSignature) { s.Domain = 99 }), pub, msgkit.Malformed},
		{"profile 2", result, mutate(func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 2 }), pub, msgkit.OutOfRange},
		{"short signature", result, mutate(func(s *typesv1.InnerSignature) { s.Signature = s.Signature[:63] }), pub, msgkit.Bounds},
		{"key id other than the manifest's", otherKeyID, sig, pub, msgkit.Unauthorized},
		{"key other than the provider's", result, sig, []byte(stranger.Public().(ed25519.PublicKey)), msgkit.Unauthorized},
		{"signed by another key", result, mutate(func(s *typesv1.InnerSignature) {
			s.PublicKey = []byte(stranger.Public().(ed25519.PublicKey))
			s.Signature = ed25519.Sign(stranger, result.SignBytes())
		}), pub, msgkit.Unauthorized},
		{"another body", other, sig, pub, msgkit.Unauthorized},
		{"flipped signature byte", result, mutate(func(s *typesv1.InnerSignature) { s.Signature[0] ^= 1 }), pub, msgkit.Unauthorized},
	}
	for _, c := range cases {
		if got := Verify(c.result, c.sig, c.pub); got != c.want {
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
// decoded into the keywords this package implements.
type schemaFile struct {
	AdditionalProperties *bool               `json:"additionalProperties"`
	Required             []string            `json:"required"`
	Properties           map[string]property `json:"properties"`
}

type property struct {
	Const     json.RawMessage `json:"const"`
	Type      string          `json:"type"`
	MinLength *int            `json:"minLength"`
	MaxLength *int            `json:"maxLength"`
	Pattern   string          `json:"pattern"`
	MinItems  *int            `json:"minItems"`
	MaxItems  *int            `json:"maxItems"`
	Items     *schemaFile     `json:"items"`
}

func readSchema(t *testing.T) schemaFile {
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
	var schema schemaFile
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func intIs(p *int, want int) bool { return p != nil && *p == want }

// TestValidatorMatchesSchemaFile compares the hand-written rules with the
// frozen JSON schema, keyword by keyword, so the two cannot drift.
func TestValidatorMatchesSchemaFile(t *testing.T) {
	schema := readSchema(t)
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
	if item.AdditionalProperties == nil || *item.AdditionalProperties {
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
