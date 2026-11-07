// Package verify checks a provider's package against its media manifest:
// strict manifest parsing, the manifest digest, the interim package layout,
// segment slicing, and sample selection (interim rules of decision request
// DR-09-21; goal 15 owns the real package contract).
//
// Interim rules for goal-09 test endpoints:
//
//   - the manifest is an operator-supplied file of at most 65536 bytes; its
//     digest is SHA-256 over its SVN-JCS-1 canonical text, the precedent of
//     the capability manifest;
//   - the package is the concatenation of the segment bytes in manifest
//     order (renditions in order, segments in order), and package_bytes is
//     the sum of byte_count;
//   - a sampled segment is fetched with one Range request at its prefix-sum
//     offset. Range answers are a goal-09 test-endpoint extension: v0
//     defines only 200 for GET /packages.
//
// Audit returns an Observation of what was checked. It does not decide
// PASS, FAIL, or INCOMPLETE (owner question Q3).
package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
)

// Bounds from docs/protocol/bounds.json and media-manifest.schema.json.
const (
	// MaxManifestBytes is safety_max_manifest_canonical_bytes; the raw file
	// is held to the same bound.
	MaxManifestBytes    = 65536
	MaxRenditions       = 8
	MaxSegments         = 4096
	maxIDChars          = 64
	maxCodecChars       = 32
	maxByteCountChars   = 20
	manifestVersion     = 1
	manifestProfile     = "SVN-JCS-1"
	maxSafeContentValue = uint64(genesis.SafeIntegerMax)
)

// ErrManifestInvalid wraps every manifest validation failure.
var ErrManifestInvalid = errors.New("media manifest invalid")

// Segment is one manifest segment.
type Segment struct {
	ID        string
	SHA256    [32]byte
	ByteCount uint64
}

// Rendition is one manifest rendition.
type Rendition struct {
	ID       string
	Codec    string
	Segments []Segment
}

// Manifest is a validated svn-media-manifest-v1 document.
type Manifest struct {
	ManifestID     string
	ContentID      string
	ContentVersion uint64
	PackageBytes   uint64
	Renditions     []Rendition
	// Canonical is the SVN-JCS-1 text of the manifest.
	Canonical string
	// SHA256 is SHA-256 over Canonical.
	SHA256 [32]byte
}

// ReadManifestFile reads an operator manifest file: a regular file of at
// most MaxManifestBytes bytes.
func ReadManifestFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("manifest file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrManifestInvalid)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("manifest file: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("manifest file: %w", err)
	}
	if len(raw) > MaxManifestBytes {
		return nil, fmt.Errorf("%w: file above %d bytes", ErrManifestInvalid, MaxManifestBytes)
	}
	return raw, nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrManifestInvalid, fmt.Sprintf(format, args...))
}

var (
	idPattern  = regexp.MustCompile(genesis.IDPattern)
	hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ParseManifest validates raw manifest JSON against
// media-manifest.schema.json on top of SVN-JCS-1, and requires segment ids
// to be unique across the manifest (the goal-02 duplicate check). Lengths
// count code points, as JSON Schema does. Keys are compared exactly.
func ParseManifest(raw []byte) (*Manifest, error) {
	if len(raw) > MaxManifestBytes {
		return nil, invalid("input above %d bytes", MaxManifestBytes)
	}
	canonical, err := jcs.ParseAndCanonicalize(string(raw))
	if err != nil {
		return nil, invalid("not SVN-JCS-1: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(canonical)))
	decoder.UseNumber()
	var top map[string]any
	if err := decoder.Decode(&top); err != nil || top == nil {
		return nil, invalid("top level is not an object")
	}
	m, err := fromObject(top)
	if err != nil {
		return nil, err
	}
	m.Canonical = canonical
	m.SHA256 = sha256.Sum256([]byte(canonical))
	return m, nil
}

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

func chars(s string, lo, hi int) bool {
	n := utf8.RuneCountInString(s)
	return n >= lo && n <= hi
}

func number(v any) (uint64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseUint(n.String(), 10, 64)
	return value, err == nil
}

// decimal parses a schema decimal string: at most 20 characters matching
// ^(0|[1-9][0-9]*)$, and within uint64.
func decimal(v any) (uint64, bool) {
	s, ok := v.(string)
	if !ok || s == "" || len(s) > maxByteCountChars || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(s, 10, 64)
	return value, err == nil
}

var topKeys = []string{"content_id", "content_version", "manifest_id", "package_bytes", "profile", "renditions", "schema_version"}

func fromObject(top map[string]any) (*Manifest, error) {
	if err := exactKeys(top, topKeys, "manifest"); err != nil {
		return nil, err
	}
	if v, ok := number(top["schema_version"]); !ok || v != manifestVersion {
		return nil, invalid("schema_version must be the number %d", manifestVersion)
	}
	if p, ok := top["profile"].(string); !ok || p != manifestProfile {
		return nil, invalid("profile must be %q", manifestProfile)
	}
	m := &Manifest{}
	var ok bool
	if m.ManifestID, ok = top["manifest_id"].(string); !ok || !idPattern.MatchString(m.ManifestID) {
		return nil, invalid("manifest_id must match %s", genesis.IDPattern)
	}
	if m.ContentID, ok = top["content_id"].(string); !ok || !idPattern.MatchString(m.ContentID) {
		return nil, invalid("content_id must match %s", genesis.IDPattern)
	}
	if m.ContentVersion, ok = number(top["content_version"]); !ok || m.ContentVersion < 1 || m.ContentVersion > maxSafeContentValue {
		return nil, invalid("content_version must be 1 to %d", maxSafeContentValue)
	}
	if m.PackageBytes, ok = decimal(top["package_bytes"]); !ok {
		return nil, invalid("package_bytes must be a decimal string of at most %d characters", maxByteCountChars)
	}
	list, ok := top["renditions"].([]any)
	if !ok || len(list) < 1 || len(list) > MaxRenditions {
		return nil, invalid("renditions must hold 1 to %d items", MaxRenditions)
	}
	seen := map[string]bool{}
	for i, item := range list {
		r, err := renditionFrom(item, i, seen)
		if err != nil {
			return nil, err
		}
		m.Renditions = append(m.Renditions, r)
	}
	return m, nil
}

func renditionFrom(item any, index int, seen map[string]bool) (Rendition, error) {
	where := fmt.Sprintf("renditions[%d]", index)
	object, ok := item.(map[string]any)
	if !ok {
		return Rendition{}, invalid("%s is not an object", where)
	}
	if err := exactKeys(object, []string{"codec", "rendition_id", "segments"}, where); err != nil {
		return Rendition{}, err
	}
	var r Rendition
	if r.ID, ok = object["rendition_id"].(string); !ok || !chars(r.ID, 1, maxIDChars) {
		return Rendition{}, invalid("%s.rendition_id must be 1 to %d characters", where, maxIDChars)
	}
	if r.Codec, ok = object["codec"].(string); !ok || !chars(r.Codec, 1, maxCodecChars) {
		return Rendition{}, invalid("%s.codec must be 1 to %d characters", where, maxCodecChars)
	}
	list, ok := object["segments"].([]any)
	if !ok || len(list) < 1 || len(list) > MaxSegments {
		return Rendition{}, invalid("%s.segments must hold 1 to %d items", where, MaxSegments)
	}
	for j, item := range list {
		at := fmt.Sprintf("%s.segments[%d]", where, j)
		object, ok := item.(map[string]any)
		if !ok {
			return Rendition{}, invalid("%s is not an object", at)
		}
		if err := exactKeys(object, []string{"byte_count", "segment_id", "sha256"}, at); err != nil {
			return Rendition{}, err
		}
		var s Segment
		if s.ID, ok = object["segment_id"].(string); !ok || !chars(s.ID, 1, maxIDChars) {
			return Rendition{}, invalid("%s.segment_id must be 1 to %d characters", at, maxIDChars)
		}
		if seen[s.ID] {
			return Rendition{}, invalid("%s.segment_id is a duplicate", at)
		}
		seen[s.ID] = true
		digest, ok := object["sha256"].(string)
		if !ok || !hexPattern.MatchString(digest) {
			return Rendition{}, invalid("%s.sha256 must be 64 lower-case hex characters", at)
		}
		raw, _ := hex.DecodeString(digest)
		copy(s.SHA256[:], raw)
		if s.ByteCount, ok = decimal(object["byte_count"]); !ok {
			return Rendition{}, invalid("%s.byte_count must be a decimal string of at most %d characters", at, maxByteCountChars)
		}
		r.Segments = append(r.Segments, s)
	}
	return r, nil
}
