package verify

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// MaxPackageBytes is bounds.json safety_max_package_bytes.
const MaxPackageBytes = genesis.MaxPackageBytes

// Span is one segment's place in the package.
type Span struct {
	SegmentID string
	Offset    uint64
	Length    uint64
	SHA256    [32]byte
}

// Layout is the interim package layout: segments concatenated in manifest
// order.
type Layout struct {
	Spans        []Span
	PackageBytes uint64
}

// emptySHA256 is the SHA-256 of empty input.
var emptySHA256 = sha256.Sum256(nil)

// NewLayout computes the layout of m. The sum of byte_count must equal
// package_bytes, and package_bytes must be 1 to MaxPackageBytes. A segment
// with byte_count 0 must carry the SHA-256 of empty input: it is judged
// without provider bytes, so any other digest would be a mismatch that no
// provider could avoid.
func NewLayout(m *Manifest) (*Layout, error) {
	l := &Layout{}
	var offset uint64
	for _, r := range m.Renditions {
		for _, s := range r.Segments {
			if s.ByteCount > MaxPackageBytes || offset+s.ByteCount > MaxPackageBytes {
				return nil, invalid("segments add up to more than %d bytes", MaxPackageBytes)
			}
			if s.ByteCount == 0 && s.SHA256 != emptySHA256 {
				return nil, invalid("a segment with byte_count 0 must carry the SHA-256 of empty input")
			}
			l.Spans = append(l.Spans, Span{SegmentID: s.ID, Offset: offset, Length: s.ByteCount, SHA256: s.SHA256})
			offset += s.ByteCount
		}
	}
	if offset != m.PackageBytes {
		return nil, invalid("segments add up to %d bytes, package_bytes is %d", offset, m.PackageBytes)
	}
	if offset == 0 {
		return nil, invalid("package_bytes is 0")
	}
	l.PackageBytes = offset
	return l, nil
}

// SegmentState is what was observed for one segment.
type SegmentState string

// Segment states.
const (
	// Match: the segment's bytes were received in full and their SHA-256 is
	// the manifest's.
	Match SegmentState = "MATCH"
	// DigestMismatch: received in full, but the SHA-256 differs.
	DigestMismatch SegmentState = "DIGEST_MISMATCH"
	// NotReceived: the segment's bytes were not received in full.
	NotReceived SegmentState = "NOT_RECEIVED"
)

// SegmentResult is the observation for one checked segment.
type SegmentResult struct {
	SegmentID string
	State     SegmentState
}

// splitter is the whole-package Sink: it hashes each segment slice of the
// stream in layout order.
type splitter struct {
	spans   []Span
	index   int
	written uint64
	hash    hash.Hash
	results []SegmentResult
}

func newSplitter(spans []Span) *splitter {
	s := &splitter{spans: spans, hash: sha256.New()}
	s.finishEmpty()
	return s
}

// finishEmpty completes zero-length segments at the current position.
func (s *splitter) finishEmpty() {
	for s.index < len(s.spans) && s.written == s.spans[s.index].Length {
		s.finish()
	}
}

func (s *splitter) finish() {
	span := s.spans[s.index]
	state := DigestMismatch
	var sum [32]byte
	copy(sum[:], s.hash.Sum(nil))
	if sum == span.SHA256 {
		state = Match
	}
	s.results = append(s.results, SegmentResult{SegmentID: span.SegmentID, State: state})
	s.index++
	s.written = 0
	s.hash.Reset()
}

var errPastPackage = errors.New("verify: bytes past the end of the package")

func (s *splitter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if s.index >= len(s.spans) {
			return n - len(p), errPastPackage
		}
		take := min(uint64(len(p)), s.spans[s.index].Length-s.written)
		s.hash.Write(p[:take])
		s.written += take
		p = p[take:]
		s.finishEmpty()
	}
	return n, nil
}

// close marks every segment that was not completed as NotReceived.
func (s *splitter) close() []SegmentResult {
	for i := s.index; i < len(s.spans); i++ {
		s.results = append(s.results, SegmentResult{SegmentID: s.spans[i].SegmentID, State: NotReceived})
	}
	s.index = len(s.spans)
	return s.results
}

// SeedBytes is the sample seed size.
const SeedBytes = 32

// NewSeed reads a fresh sample seed from r (crypto/rand.Reader when nil).
func NewSeed(r io.Reader) ([SeedBytes]byte, error) {
	if r == nil {
		r = rand.Reader
	}
	var seed [SeedBytes]byte
	if _, err := io.ReadFull(r, seed[:]); err != nil {
		return seed, fmt.Errorf("verify: sample seed: %w", err)
	}
	return seed, nil
}

// Select returns the first k spans ordered by SHA-256(seed || segment_id),
// ties broken by segment id. k must be 1 to the number of spans. The same
// seed always selects the same spans.
func Select(l *Layout, seed [SeedBytes]byte, k int) ([]Span, error) {
	if k < 1 || k > len(l.Spans) {
		return nil, fmt.Errorf("verify: sample count %d outside 1 to %d", k, len(l.Spans))
	}
	type ranked struct {
		key  [32]byte
		span Span
	}
	all := make([]ranked, len(l.Spans))
	for i, span := range l.Spans {
		all[i] = ranked{key: sha256.Sum256(append(seed[:len(seed):len(seed)], span.SegmentID...)), span: span}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].key != all[j].key {
			return string(all[i].key[:]) < string(all[j].key[:])
		}
		return all[i].span.SegmentID < all[j].span.SegmentID
	})
	out := make([]Span, k)
	for i := range out {
		out[i] = all[i].span
	}
	return out, nil
}
