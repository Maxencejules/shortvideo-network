package verify

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
)

// Mode is the audit mode.
type Mode string

// Modes.
const (
	WholePackage    Mode = "WHOLE_PACKAGE"
	SampledSegments Mode = "SAMPLED_SEGMENTS"
)

// Lease holds the chain values an audit checks against. The caller reads
// them from the chain; this package never takes them from a provider.
type Lease struct {
	ContentID      string
	ContentVersion uint64
	ManifestSHA256 [32]byte
	PackageBytes   uint64
}

// Request is one audit of one endpoint.
type Request struct {
	Lease Lease
	// Manifest is the operator-supplied manifest file content.
	Manifest []byte
	Mode     Mode
	// SampleCount is k for SampledSegments; it must be 0 for WholePackage.
	SampleCount int
	// seedSource supplies the sample seed; nil is crypto/rand.Reader. It is
	// unexported so that no caller outside this package can fix the seed;
	// only this package's tests set it, to make selection reproducible.
	seedSource io.Reader
}

// Getter is the fetch.Session method Audit uses.
type Getter interface {
	Get(ctx context.Context, req fetch.Request) (fetch.Outcome, error)
}

// InputState says whether the audit inputs allowed a fetch.
type InputState string

// Input states. Only InputOK leads to a request.
const (
	InputOK InputState = "OK"
	// InputManifestInvalid: the manifest file is not a valid
	// svn-media-manifest-v1, or its layout is inconsistent.
	InputManifestInvalid InputState = "MANIFEST_INVALID"
	// InputManifestMismatch: the manifest's digest is not the lease's
	// manifest digest, or its content id, version, or package bytes differ
	// from the lease's.
	InputManifestMismatch InputState = "MANIFEST_MISMATCH"
)

// Observation is what one audit observed. It carries no PASS, FAIL, or
// INCOMPLETE verdict.
type Observation struct {
	Input InputState
	// InputDetail is this package's error text when Input is not OK.
	InputDetail string
	// ManifestSHA256 is the manifest's digest when it parsed.
	ManifestSHA256 [32]byte
	Mode           Mode
	// Seed is the sample seed (SampledSegments only), drawn after the
	// inputs were checked and before the first request.
	Seed *[SeedBytes]byte
	// Fetches is one outcome per request, in request order. A sampled audit
	// stops requesting after the first outcome that is not OK.
	Fetches []fetch.Outcome
	// Segments is one result per checked segment: every segment for
	// WholePackage, the selected ones for SampledSegments.
	Segments []SegmentResult
	// BytesRead is the sum of body bytes read over all requests.
	BytesRead uint64
}

// Audit checks the inputs, then fetches and checks the package. Nothing is
// requested when the manifest is invalid or does not match the lease. The
// error is non-nil only for an invalid Request, a seed read failure, or a
// fetch request error; observations are in the Observation.
func Audit(ctx context.Context, g Getter, req Request) (Observation, error) {
	obs := Observation{Mode: req.Mode}
	switch req.Mode {
	case WholePackage:
		if req.SampleCount != 0 {
			return obs, errors.New("verify: a whole-package audit takes no sample count")
		}
	case SampledSegments:
		if req.SampleCount < 1 {
			return obs, errors.New("verify: a sampled audit needs a sample count of at least 1")
		}
	default:
		return obs, fmt.Errorf("verify: unknown mode %q", req.Mode)
	}
	m, err := ParseManifest(req.Manifest)
	if err != nil {
		obs.Input, obs.InputDetail = InputManifestInvalid, err.Error()
		return obs, nil
	}
	obs.ManifestSHA256 = m.SHA256
	layout, err := NewLayout(m)
	if err != nil {
		obs.Input, obs.InputDetail = InputManifestInvalid, err.Error()
		return obs, nil
	}
	if detail := mismatch(m, req.Lease); detail != "" {
		obs.Input, obs.InputDetail = InputManifestMismatch, detail
		return obs, nil
	}
	obs.Input = InputOK
	if req.Mode == WholePackage {
		return whole(ctx, g, req.Lease, layout, obs)
	}
	if req.SampleCount > len(layout.Spans) {
		return obs, fmt.Errorf("verify: sample count %d above the %d segments", req.SampleCount, len(layout.Spans))
	}
	seed, err := NewSeed(req.seedSource)
	if err != nil {
		return obs, err
	}
	obs.Seed = &seed
	return sampled(ctx, g, req.Lease, layout, seed, req.SampleCount, obs)
}

func mismatch(m *Manifest, l Lease) string {
	switch {
	case m.SHA256 != l.ManifestSHA256:
		return "manifest digest is not the lease's manifest digest"
	case m.ContentID != l.ContentID:
		return "manifest content_id is not the lease's"
	case m.ContentVersion != l.ContentVersion:
		return "manifest content_version is not the lease's"
	case m.PackageBytes != l.PackageBytes:
		return "manifest package_bytes is not the lease's"
	}
	return ""
}

func whole(ctx context.Context, g Getter, l Lease, layout *Layout, obs Observation) (Observation, error) {
	sink := newSplitter(layout.Spans)
	out, err := g.Get(ctx, fetch.Request{
		ContentID: l.ContentID, ContentVersion: l.ContentVersion, Expected: layout.PackageBytes, Sink: sink,
	})
	if err != nil {
		return obs, err
	}
	obs.Fetches = append(obs.Fetches, out)
	obs.BytesRead = out.BytesRead
	obs.Segments = sink.close()
	return obs, nil
}

func sampled(ctx context.Context, g Getter, l Lease, layout *Layout, seed [SeedBytes]byte, k int, obs Observation) (Observation, error) {
	spans, err := Select(layout, seed, k)
	if err != nil {
		return obs, err
	}
	stopped := false
	for _, span := range spans {
		if stopped {
			obs.Segments = append(obs.Segments, SegmentResult{SegmentID: span.SegmentID, State: NotReceived})
			continue
		}
		if span.Length == 0 {
			obs.Segments = append(obs.Segments, SegmentResult{SegmentID: span.SegmentID, State: stateOf(emptySHA256, span)})
			continue
		}
		h := sha256.New()
		out, err := g.Get(ctx, fetch.Request{
			ContentID: l.ContentID, ContentVersion: l.ContentVersion,
			Range:    &fetch.Range{First: span.Offset, Last: span.Offset + span.Length - 1, Total: layout.PackageBytes},
			Expected: span.Length, Sink: h,
		})
		if err != nil {
			return obs, err
		}
		obs.Fetches = append(obs.Fetches, out)
		obs.BytesRead += out.BytesRead
		if out.Kind != fetch.OK {
			stopped = true
			obs.Segments = append(obs.Segments, SegmentResult{SegmentID: span.SegmentID, State: NotReceived})
			continue
		}
		var sum [32]byte
		copy(sum[:], h.Sum(nil))
		obs.Segments = append(obs.Segments, SegmentResult{SegmentID: span.SegmentID, State: stateOf(sum, span)})
	}
	return obs, nil
}

func stateOf(sum [32]byte, span Span) SegmentState {
	if sum == span.SHA256 {
		return Match
	}
	return DigestMismatch
}
