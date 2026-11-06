// Package report encodes and parses the off-chain audit report,
// format "svn-audit-report-proposed-v0" (interim under decision request
// DR-09-22). The encoding is SVN-JCS-1 text of at most
// 65536 bytes; uint64 values are decimal strings and other integers are in
// the safe range.
//
// This package does not classify. Result, Reasons, the per-segment reasons,
// and Limitations are taken as the caller gives them and only checked for
// syntax and for consistency (a PASS carries no reason and no failed
// segment; FAIL and INCOMPLETE carry at least one reason). Which outcome is
// FAIL and which is INCOMPLETE depends on owner question Q3; the limitation
// strings come from the label catalog. Neither is in this package.
//
// A report holds no evidence id, batch id, URL, IP address, key material,
// media byte, or personal data. The endpoint is named by its index and the
// SHA-256 of its name; the provider's text is never copied. Start and finish
// times come from the auditor host clock and are not verified by anyone.
//
// endpoint_source records where the audit took its endpoint from:
// CAPABILITY_MANIFEST (a capability manifest file, checked against the
// provider record before any request) or OPERATOR_CONFIG (an endpoint the
// operator typed into the configuration). The field is required. It was
// added on 2026-11-06 under the interim choice N2 without a
// new format string: the format is interim and unreleased, and a report
// written before the change does not parse. This package checks only that
// the value is one of the two; which results each source allows is the
// classify package's rule (classify), and signing and review enforce it
// again (batch.NewItem, review).
package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
)

// Fixed values and bounds.
const (
	Format     = "svn-audit-report-proposed-v0"
	ClaimLimit = "ATTESTATION_NOT_STORAGE_PROOF"

	// MaxBytes is the canonical size bound (the SVN-JCS-1 bound).
	MaxBytes = 65536
	// MaxListedFailures bounds failed_segments; failed_segment_count holds
	// the full count.
	MaxListedFailures = 64
	// MaxSegments is 8 renditions of 4096 segments.
	MaxSegments    = 8 * 4096
	MaxEndpoint    = 7
	MaxReasons     = 16
	MaxLimitations = 8
	// MaxLimitationBytes bounds one limitation string.
	MaxLimitationBytes = 512
	maxSegmentIDChars  = 64
)

// Results.
const (
	ResultPass       = "PASS"
	ResultFail       = "FAIL"
	ResultIncomplete = "INCOMPLETE"
)

// Modes.
const (
	ModeWholePackage    = "WHOLE_PACKAGE"
	ModeSampledSegments = "SAMPLED_SEGMENTS"
)

// Endpoint sources.
const (
	// EndpointSourceCapabilityManifest: the endpoint is listed in a
	// capability manifest file that names the provider and its current
	// signing key id and hashes to the provider record's committed
	// endpoint_manifest_digest.
	EndpointSourceCapabilityManifest = "CAPABILITY_MANIFEST"
	// EndpointSourceOperatorConfig: the operator typed the endpoint into
	// the configuration; nothing binds it to the provider.
	EndpointSourceOperatorConfig = "OPERATOR_CONFIG"
)

// FailedSegment is one segment that did not check, with its reason.
type FailedSegment struct {
	SegmentID string
	Reason    string
}

// Report is one audit report.
type Report struct {
	AuditorKeyID       string
	AuditorAddress     string
	LeaseID            string
	ContentID          string
	ContentVersion     uint64
	ProviderID         string
	EndpointIndex      int
	EndpointNameSHA256 [32]byte
	// EndpointSource is EndpointSourceCapabilityManifest or
	// EndpointSourceOperatorConfig.
	EndpointSource         string
	ExpectedManifestSHA256 [32]byte
	Mode                   string
	// SampleSeed is set for SAMPLED_SEGMENTS only.
	SampleSeed      *[32]byte
	CheckedSegments int
	// FailedSegments lists the first MaxListedFailures failed segments.
	FailedSegments     []FailedSegment
	FailedSegmentCount int
	BytesRead          uint64
	// StartedUnixSeconds and FinishedUnixSeconds are auditor host clock
	// readings (unverified).
	StartedUnixSeconds  int64
	FinishedUnixSeconds int64
	Result              string
	// Reasons are sorted and unique.
	Reasons     []string
	Limitations []string
}

// ErrInvalid wraps every validation failure.
var ErrInvalid = errors.New("audit report invalid")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

var (
	idPattern      = regexp.MustCompile(genesis.IDPattern)
	addressPattern = regexp.MustCompile(`^[a-z0-9]{1,90}$`)
	reasonPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	hexPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (r Report) validate() error {
	ids := []struct{ name, value string }{
		{"auditor_key_id", r.AuditorKeyID}, {"lease_id", r.LeaseID},
		{"content_id", r.ContentID}, {"provider_id", r.ProviderID},
	}
	for _, id := range ids {
		if !idPattern.MatchString(id.value) {
			return invalid("%s must match %s", id.name, genesis.IDPattern)
		}
	}
	if !addressPattern.MatchString(r.AuditorAddress) {
		return invalid("auditor_address must be 1 to 90 lower-case letters and digits")
	}
	if r.ContentVersion < 1 || r.ContentVersion > uint64(genesis.SafeIntegerMax) {
		return invalid("content_version must be 1 to %d", genesis.SafeIntegerMax)
	}
	if r.EndpointIndex < 0 || r.EndpointIndex > MaxEndpoint {
		return invalid("endpoint_index must be 0 to %d", MaxEndpoint)
	}
	if r.EndpointSource != EndpointSourceCapabilityManifest && r.EndpointSource != EndpointSourceOperatorConfig {
		return invalid("endpoint_source must be %s or %s", EndpointSourceCapabilityManifest, EndpointSourceOperatorConfig)
	}
	switch r.Mode {
	case ModeWholePackage:
		if r.SampleSeed != nil {
			return invalid("a whole-package report has no sample_seed")
		}
	case ModeSampledSegments:
		if r.SampleSeed == nil {
			return invalid("a sampled report needs sample_seed")
		}
	default:
		return invalid("mode must be %s or %s", ModeWholePackage, ModeSampledSegments)
	}
	if r.CheckedSegments < 0 || r.CheckedSegments > MaxSegments {
		return invalid("checked_segments must be 0 to %d", MaxSegments)
	}
	if r.FailedSegmentCount < 0 || r.FailedSegmentCount > MaxSegments {
		return invalid("failed_segment_count must be 0 to %d", MaxSegments)
	}
	if len(r.FailedSegments) != min(r.FailedSegmentCount, MaxListedFailures) {
		return invalid("failed_segments must list the first %d of failed_segment_count", MaxListedFailures)
	}
	for _, f := range r.FailedSegments {
		if n := utf8.RuneCountInString(f.SegmentID); n < 1 || n > maxSegmentIDChars || !utf8.ValidString(f.SegmentID) {
			return invalid("failed segment id must be 1 to %d characters", maxSegmentIDChars)
		}
		if !reasonPattern.MatchString(f.Reason) {
			return invalid("failed segment reason must match %s", reasonPattern)
		}
	}
	if r.BytesRead > genesis.MaxPackageBytes+1 {
		return invalid("bytes_read above %d", genesis.MaxPackageBytes+1)
	}
	if r.StartedUnixSeconds < genesis.UnixSecondsMin || r.FinishedUnixSeconds > genesis.UnixSecondsMax || r.FinishedUnixSeconds < r.StartedUnixSeconds {
		return invalid("times must be in [%d, %d] and finished must not precede started", genesis.UnixSecondsMin, genesis.UnixSecondsMax)
	}
	if len(r.Reasons) > MaxReasons || !sort.StringsAreSorted(r.Reasons) {
		return invalid("reasons must be at most %d, sorted", MaxReasons)
	}
	for i, reason := range r.Reasons {
		if !reasonPattern.MatchString(reason) || (i > 0 && r.Reasons[i-1] == reason) {
			return invalid("reasons must be unique and match %s", reasonPattern)
		}
	}
	switch r.Result {
	case ResultPass:
		if len(r.Reasons) != 0 || r.FailedSegmentCount != 0 {
			return invalid("a PASS report has no reason and no failed segment")
		}
	case ResultFail, ResultIncomplete:
		if len(r.Reasons) == 0 {
			return invalid("a %s report needs a reason", r.Result)
		}
	default:
		return invalid("result must be PASS, FAIL, or INCOMPLETE")
	}
	if len(r.Limitations) > MaxLimitations {
		return invalid("at most %d limitations", MaxLimitations)
	}
	for _, text := range r.Limitations {
		if len(text) == 0 || len(text) > MaxLimitationBytes || !printableASCII(text) {
			return invalid("a limitation must be 1 to %d printable ASCII bytes", MaxLimitationBytes)
		}
	}
	return nil
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func anyList(list []string) []any {
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, s)
	}
	return out
}

func (r Report) object() map[string]any {
	failed := make([]any, 0, len(r.FailedSegments))
	for _, f := range r.FailedSegments {
		failed = append(failed, map[string]any{"segment_id": f.SegmentID, "reason": f.Reason})
	}
	o := map[string]any{
		"format":                   Format,
		"claim_limit":              ClaimLimit,
		"auditor_key_id":           r.AuditorKeyID,
		"auditor_address":          r.AuditorAddress,
		"lease_id":                 r.LeaseID,
		"content_id":               r.ContentID,
		"content_version":          int64(r.ContentVersion),
		"provider_id":              r.ProviderID,
		"endpoint_index":           int64(r.EndpointIndex),
		"endpoint_name_sha256":     hex.EncodeToString(r.EndpointNameSHA256[:]),
		"endpoint_source":          r.EndpointSource,
		"expected_manifest_sha256": hex.EncodeToString(r.ExpectedManifestSHA256[:]),
		"mode":                     r.Mode,
		"checked_segments":         int64(r.CheckedSegments),
		"failed_segments":          failed,
		"failed_segment_count":     int64(r.FailedSegmentCount),
		"bytes_read":               strconv.FormatUint(r.BytesRead, 10),
		"started_unix_seconds":     r.StartedUnixSeconds,
		"finished_unix_seconds":    r.FinishedUnixSeconds,
		"result":                   r.Result,
		"reasons":                  anyList(r.Reasons),
		"limitations":              anyList(r.Limitations),
	}
	if r.SampleSeed != nil {
		o[SampledKey] = hex.EncodeToString(r.SampleSeed[:])
	}
	return o
}

// Encode validates r and returns its SVN-JCS-1 text.
func Encode(r Report) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	text, err := jcs.Canonicalize(r.object())
	if err != nil {
		return nil, invalid("not SVN-JCS-1: %v", err)
	}
	return []byte(text), nil
}

// SHA256 is the digest of the encoded report bytes.
func SHA256(encoded []byte) [32]byte { return sha256.Sum256(encoded) }

// Keys are the keys of every report, sorted.
var Keys = []string{
	"auditor_address", "auditor_key_id", "bytes_read", "checked_segments", "claim_limit",
	"content_id", "content_version", "endpoint_index", "endpoint_name_sha256",
	"endpoint_source", "expected_manifest_sha256", "failed_segment_count", "failed_segments",
	"finished_unix_seconds", "format", "lease_id", "limitations", "mode", "provider_id",
	"reasons", "result", "started_unix_seconds",
}

// SampledKey is the one key only a sampled report has.
const SampledKey = "sample_seed"

// Parse reads an encoded report. raw must be canonical: Parse rebuilds the
// report, encodes it again, and requires the same bytes.
func Parse(raw []byte) (Report, error) {
	if len(raw) > MaxBytes {
		return Report{}, invalid("above %d bytes", MaxBytes)
	}
	canonical, err := jcs.ParseAndCanonicalize(string(raw))
	if err != nil {
		return Report{}, invalid("not SVN-JCS-1: %v", err)
	}
	if canonical != string(raw) {
		return Report{}, invalid("not canonical")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var o map[string]any
	if err := decoder.Decode(&o); err != nil || o == nil {
		return Report{}, invalid("top level is not an object")
	}
	r, err := fromObject(o)
	if err != nil {
		return Report{}, err
	}
	again, err := Encode(r)
	if err != nil {
		return Report{}, err
	}
	if !bytes.Equal(again, raw) {
		return Report{}, invalid("fields outside the format")
	}
	return r, nil
}

type reader struct {
	o   map[string]any
	err error
}

func (rd *reader) str(key string) string {
	s, ok := rd.o[key].(string)
	if !ok && rd.err == nil {
		rd.err = invalid("%s must be a string", key)
	}
	return s
}

func (rd *reader) num(key string) int64 {
	n, ok := rd.o[key].(json.Number)
	v, err := n.Int64()
	if (!ok || err != nil) && rd.err == nil {
		rd.err = invalid("%s must be an integer", key)
	}
	return v
}

func (rd *reader) digest(key string) [32]byte {
	var out [32]byte
	s := rd.str(key)
	if !hexPattern.MatchString(s) {
		if rd.err == nil {
			rd.err = invalid("%s must be 64 lower-case hex characters", key)
		}
		return out
	}
	raw, _ := hex.DecodeString(s)
	copy(out[:], raw)
	return out
}

func (rd *reader) list(key string) []string {
	items, ok := rd.o[key].([]any)
	if !ok {
		if rd.err == nil {
			rd.err = invalid("%s must be an array", key)
		}
		return nil
	}
	out := []string{}
	for _, item := range items {
		s, ok := item.(string)
		if !ok && rd.err == nil {
			rd.err = invalid("%s must hold strings", key)
		}
		out = append(out, s)
	}
	return out
}

func fromObject(o map[string]any) (Report, error) {
	want := slices.Clone(Keys)
	if _, ok := o[SampledKey]; ok {
		want = append(want, SampledKey)
	}
	if len(o) != len(want) {
		return Report{}, invalid("unexpected keys")
	}
	for _, key := range want {
		if _, ok := o[key]; !ok {
			return Report{}, invalid("missing %q", key)
		}
	}
	rd := &reader{o: o}
	if rd.str("format") != Format || rd.str("claim_limit") != ClaimLimit {
		return Report{}, invalid("format or claim_limit")
	}
	r := Report{
		AuditorKeyID:           rd.str("auditor_key_id"),
		AuditorAddress:         rd.str("auditor_address"),
		LeaseID:                rd.str("lease_id"),
		ContentID:              rd.str("content_id"),
		ContentVersion:         uint64(rd.num("content_version")),
		ProviderID:             rd.str("provider_id"),
		EndpointIndex:          int(rd.num("endpoint_index")),
		EndpointNameSHA256:     rd.digest("endpoint_name_sha256"),
		EndpointSource:         rd.str("endpoint_source"),
		ExpectedManifestSHA256: rd.digest("expected_manifest_sha256"),
		Mode:                   rd.str("mode"),
		CheckedSegments:        int(rd.num("checked_segments")),
		FailedSegmentCount:     int(rd.num("failed_segment_count")),
		StartedUnixSeconds:     rd.num("started_unix_seconds"),
		FinishedUnixSeconds:    rd.num("finished_unix_seconds"),
		Result:                 rd.str("result"),
		Reasons:                rd.list("reasons"),
		Limitations:            rd.list("limitations"),
	}
	if _, ok := o[SampledKey]; ok {
		seed := rd.digest(SampledKey)
		r.SampleSeed = &seed
	}
	bytesRead, err := strconv.ParseUint(rd.str("bytes_read"), 10, 64)
	if err != nil && rd.err == nil {
		rd.err = invalid("bytes_read must be a decimal string")
	}
	r.BytesRead = bytesRead
	failed, ok := o["failed_segments"].([]any)
	if !ok {
		return Report{}, invalid("failed_segments must be an array")
	}
	for _, item := range failed {
		f, ok := item.(map[string]any)
		if !ok || len(f) != 2 {
			return Report{}, invalid("failed segment must be an object of segment_id and reason")
		}
		inner := &reader{o: f}
		r.FailedSegments = append(r.FailedSegments, FailedSegment{SegmentID: inner.str("segment_id"), Reason: inner.str("reason")})
		if inner.err != nil {
			return Report{}, inner.err
		}
	}
	if rd.err != nil {
		return Report{}, rd.err
	}
	return r, nil
}
