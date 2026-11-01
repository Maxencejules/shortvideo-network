// Package capability validates the off-chain provider capability manifest
// and computes its digest, the value a provider puts in
// endpoint_manifest_digest.
//
// This package is not chain code. The chain stores only the 32-byte digest
// and never fetches or parses a manifest; the x/providers keeper does not
// import this package (a test enforces that). Provider agents, the devnet
// demo, and tests use it.
//
// The rules are docs/protocol/schemas/provider-capability.schema.json
// (svn-provider-capability-v1) on top of the SVN-JCS-1 profile:
//
//   - exactly the keys schema_version, profile, provider_id, signing_key_id,
//     declared_capacity_bytes, endpoints
//   - schema_version is the number 1; profile is "SVN-JCS-1"
//   - provider_id and signing_key_id match ^[a-z0-9][a-z0-9._-]{0,63}$
//   - declared_capacity_bytes is a string of at most 20 characters matching
//     ^[1-9][0-9]*$; this package also requires it to fit in uint64, the
//     type of the chain field (stricter than the schema)
//   - endpoints holds 1 to 8 objects with exactly name (1 to 64 characters)
//     and url (8 to 2048 characters, starting "https://")
//
// Lengths count Unicode code points, as JSON Schema does. The SVN-JCS-1
// parser rejects duplicate keys, null, fractions, exponents, numbers outside
// the safe integer range, a byte order mark, invalid UTF-8, and trailing
// data. The digest is SHA-256 over the UTF-8 SVN-JCS-1 canonical text, so
// whitespace and key order of the input do not change it.
//
// # Signature and digest
//
// The signed manifest is the manifest body plus a detached inner signature
// (svn.types.v1.InnerSignature) by the provider's current signing key:
//
//	signed bytes = UTF-8("SVN-CAPABILITY-1") || 0x00 || UTF-8(SVN-JCS-1 body)
//
// with key_id equal to the manifest's signing_key_id. Sign and Verify
// implement it. endpoint_manifest_digest is SHA-256 over the SVN-JCS-1 body
// only. The signature is not inside the digested bytes: the body is the
// same before and after signing, and a re-signature does not change the
// digest. The chain never checks this signature; it stores the digest that
// the provider account's transaction carried. A peer that fetched a
// manifest passes it, its signature, and the provider record from
// Query/Provider to Verify, which binds all three: the manifest names the
// record's provider_id and current signing_key_id, the signature is by the
// record's current key, and the body's digest is the record's
// endpoint_manifest_digest. A provider that rotates its key therefore
// publishes a new manifest naming the new key and sends UpdateProvider
// with its digest; until then peers reject the old manifest. The wire
// envelope that GET /capability returns (body and signature together) is
// not defined by protocol v0; decision request DR-07-09.
package capability

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Schema limits from provider-capability.schema.json.
const (
	SchemaVersion        = 1
	Profile              = "SVN-JCS-1"
	MinEndpoints         = 1
	MaxEndpoints         = 8
	MaxNameChars         = 64
	MinURLChars          = 8
	MaxURLChars          = 2048
	MaxCapacityChars     = 20
	URLPrefix            = "https://"
	maxManifestInputSize = 1 << 20
)

// ErrInvalid wraps every validation failure.
var ErrInvalid = errors.New("capability manifest invalid")

// Endpoint is one named https endpoint.
type Endpoint struct {
	Name string
	URL  string
}

// Manifest is a validated capability manifest.
type Manifest struct {
	ProviderID            string
	SigningKeyID          string
	DeclaredCapacityBytes uint64
	Endpoints             []Endpoint
}

// Result is a validated manifest with its canonical form and digest.
type Result struct {
	Manifest Manifest
	// Canonical is the SVN-JCS-1 text of the manifest.
	Canonical string
	// SHA256 is SHA-256 over Canonical.
	SHA256 [32]byte
}

// Digest returns the protocol digest for endpoint_manifest_digest.
func (r Result) Digest() *typesv1.Digest {
	return &typesv1.Digest{
		Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
		Value:     append([]byte(nil), r.SHA256[:]...),
	}
}

// SignatureDomain is the inner signature domain of the capability
// manifest, SVN-CAPABILITY-1.
const SignatureDomain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_CAPABILITY

// SignBytes is the preimage of the manifest signature:
// "SVN-CAPABILITY-1" || 0x00 || the SVN-JCS-1 body.
func (r Result) SignBytes() []byte {
	return msgkit.SignBytes(SignatureDomain, r.Canonical)
}

// Sign signs a validated manifest with the provider's signing key. The key
// id in the signature is the manifest's signing_key_id. A key that is not
// an ed25519 private key is an error.
func Sign(r Result, key ed25519.PrivateKey) (*typesv1.InnerSignature, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, invalid("signing key is %d bytes, want %d", len(key), ed25519.PrivateKeySize)
	}
	if r.Canonical == "" || !msgkit.ValidID(r.Manifest.SigningKeyID) {
		return nil, invalid("manifest is not a parsed result")
	}
	return &typesv1.InnerSignature{
		Domain:                  SignatureDomain,
		PublicKey:               append([]byte(nil), key.Public().(ed25519.PublicKey)...),
		Signature:               ed25519.Sign(key, r.SignBytes()),
		KeyId:                   r.Manifest.SigningKeyID,
		CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}, nil
}

// Verify checks a signed manifest against the provider record a peer read
// from Query/Provider. In order:
//
//  1. r is not a parsed result: MALFORMED; record nil: NOT_FOUND
//  2. signature structure (MALFORMED, OUT_OF_RANGE, BOUNDS) and a domain
//     other than SVN-CAPABILITY-1 (WRONG_DOMAIN), as msgkit.VerifyInner
//  3. manifest provider_id is not the record's provider_id: UNAUTHORIZED
//  4. manifest signing_key_id is not the record's current signing_key_id
//     (a retired or unknown key id): UNAUTHORIZED
//  5. signature key_id, public key, or ed25519 signature is not by the
//     record's current key (msgkit.VerifyInner): UNAUTHORIZED
//  6. the body's SHA-256 is not the record's endpoint_manifest_digest:
//     INTEGRITY
//
// OK means the provider's current key signed exactly this body, the body
// names that provider and that key, and the chain committed its digest.
func Verify(r Result, sig *typesv1.InnerSignature, record *providersv1.Provider) msgkit.Code {
	if r.Canonical == "" {
		return msgkit.Malformed
	}
	if record == nil {
		return msgkit.NotFound
	}
	if code := msgkit.CheckInnerSignature(sig, SignatureDomain); code != msgkit.OK {
		return code
	}
	if r.Manifest.ProviderID != record.GetProviderId() {
		return msgkit.Unauthorized
	}
	if r.Manifest.SigningKeyID != record.GetSigningKeyId() {
		return msgkit.Unauthorized
	}
	if code := msgkit.VerifyInner(sig, SignatureDomain, record.GetSigningKeyId(), record.GetSigningPubkey(), r.Canonical); code != msgkit.OK {
		return code
	}
	committed := record.GetEndpointManifestDigest()
	if committed.GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 || !bytes.Equal(committed.GetValue(), r.SHA256[:]) {
		return msgkit.Integrity
	}
	return msgkit.OK
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Parse validates raw manifest JSON and returns the manifest, its canonical
// text, and its digest. Input above 1 MiB is rejected before parsing.
func Parse(raw []byte) (Result, error) {
	if len(raw) > maxManifestInputSize {
		return Result{}, invalid("input is %d bytes, above %d", len(raw), maxManifestInputSize)
	}
	canonical, err := jcs.ParseAndCanonicalize(string(raw))
	if err != nil {
		return Result{}, invalid("not SVN-JCS-1: %v", err)
	}
	// The canonical text has no duplicate keys and only safe integers, so a
	// generic decode with UseNumber loses nothing. Keys are compared
	// exactly below; encoding/json's case-insensitive struct matching is
	// never used.
	decoder := json.NewDecoder(bytes.NewReader([]byte(canonical)))
	decoder.UseNumber()
	var top map[string]any
	if err := decoder.Decode(&top); err != nil {
		return Result{}, invalid("top level is not an object: %v", err)
	}
	manifest, err := fromObject(top)
	if err != nil {
		return Result{}, err
	}
	return Result{Manifest: manifest, Canonical: canonical, SHA256: sha256.Sum256([]byte(canonical))}, nil
}

// Encode validates m and returns its canonical form and digest. It builds
// the SVN-JCS-1 object and runs Parse on it, so Encode and Parse apply the
// same rules.
func Encode(m Manifest) (Result, error) {
	endpoints := make([]any, 0, len(m.Endpoints))
	for _, endpoint := range m.Endpoints {
		endpoints = append(endpoints, map[string]any{"name": endpoint.Name, "url": endpoint.URL})
	}
	object := map[string]any{
		"schema_version":          int64(SchemaVersion),
		"profile":                 Profile,
		"provider_id":             m.ProviderID,
		"signing_key_id":          m.SigningKeyID,
		"declared_capacity_bytes": strconv.FormatUint(m.DeclaredCapacityBytes, 10),
		"endpoints":               endpoints,
	}
	text, err := jcs.Canonicalize(object)
	if err != nil {
		return Result{}, invalid("not SVN-JCS-1: %v", err)
	}
	return Parse([]byte(text))
}

var topKeys = []string{"declared_capacity_bytes", "endpoints", "profile", "provider_id", "schema_version", "signing_key_id"}

func exactKeys(object map[string]any, want []string, where string) error {
	for _, key := range want {
		if _, ok := object[key]; !ok {
			return invalid("%s: missing %q", where, key)
		}
	}
	if len(object) == len(want) {
		return nil
	}
	var unknown []string
	for key := range object {
		if !slices.Contains(want, key) {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return invalid("%s: unknown keys %q", where, unknown)
}

func fromObject(top map[string]any) (Manifest, error) {
	if top == nil {
		return Manifest{}, invalid("top level is not an object")
	}
	if err := exactKeys(top, topKeys, "manifest"); err != nil {
		return Manifest{}, err
	}
	version, ok := top["schema_version"].(json.Number)
	if !ok || version.String() != strconv.Itoa(SchemaVersion) {
		return Manifest{}, invalid("schema_version must be the number %d", SchemaVersion)
	}
	profile, ok := top["profile"].(string)
	if !ok || profile != Profile {
		return Manifest{}, invalid("profile must be %q", Profile)
	}
	var m Manifest
	if m.ProviderID, ok = top["provider_id"].(string); !ok || !msgkit.ValidID(m.ProviderID) {
		return Manifest{}, invalid("provider_id must be a string matching %s", msgkit.IDPattern)
	}
	if m.SigningKeyID, ok = top["signing_key_id"].(string); !ok || !msgkit.ValidID(m.SigningKeyID) {
		return Manifest{}, invalid("signing_key_id must be a string matching %s", msgkit.IDPattern)
	}
	capacity, ok := top["declared_capacity_bytes"].(string)
	if !ok {
		return Manifest{}, invalid("declared_capacity_bytes must be a string")
	}
	value, err := parseCapacity(capacity)
	if err != nil {
		return Manifest{}, err
	}
	m.DeclaredCapacityBytes = value
	list, ok := top["endpoints"].([]any)
	if !ok {
		return Manifest{}, invalid("endpoints must be an array")
	}
	if len(list) < MinEndpoints || len(list) > MaxEndpoints {
		return Manifest{}, invalid("endpoints has %d items, want %d to %d", len(list), MinEndpoints, MaxEndpoints)
	}
	for i, item := range list {
		endpoint, err := endpointFrom(item, i)
		if err != nil {
			return Manifest{}, err
		}
		m.Endpoints = append(m.Endpoints, endpoint)
	}
	return m, nil
}

// parseCapacity is the schema rule (at most 20 characters, ^[1-9][0-9]*$)
// plus the uint64 range of the chain field.
func parseCapacity(text string) (uint64, error) {
	if text == "" || len(text) > MaxCapacityChars {
		return 0, invalid("declared_capacity_bytes must be 1 to %d characters", MaxCapacityChars)
	}
	if text[0] < '1' || text[0] > '9' {
		return 0, invalid("declared_capacity_bytes must start with 1-9")
	}
	for i := 1; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, invalid("declared_capacity_bytes must be decimal digits")
		}
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, invalid("declared_capacity_bytes %s does not fit in uint64", text)
	}
	return value, nil
}

func endpointFrom(item any, index int) (Endpoint, error) {
	where := fmt.Sprintf("endpoints[%d]", index)
	object, ok := item.(map[string]any)
	if !ok {
		return Endpoint{}, invalid("%s is not an object", where)
	}
	if err := exactKeys(object, []string{"name", "url"}, where); err != nil {
		return Endpoint{}, err
	}
	name, ok := object["name"].(string)
	if !ok {
		return Endpoint{}, invalid("%s.name must be a string", where)
	}
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxNameChars {
		return Endpoint{}, invalid("%s.name has %d characters, want 1 to %d", where, n, MaxNameChars)
	}
	url, ok := object["url"].(string)
	if !ok {
		return Endpoint{}, invalid("%s.url must be a string", where)
	}
	if n := utf8.RuneCountInString(url); n < MinURLChars || n > MaxURLChars {
		return Endpoint{}, invalid("%s.url has %d characters, want %d to %d", where, n, MinURLChars, MaxURLChars)
	}
	if !strings.HasPrefix(url, URLPrefix) {
		return Endpoint{}, invalid("%s.url must start with %q", where, URLPrefix)
	}
	return Endpoint{Name: name, URL: url}, nil
}
