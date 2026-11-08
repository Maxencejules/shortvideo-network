package batch

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/classify"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// minimalRun is a run of n PASS audits with short ids: about 307 bytes of
// text per audit.
func minimalRun(n int) Run {
	r := Run{AuditorAddress: strings.Repeat("a", 40), AuditorKeyID: "key-auditor-1", ChainID: "svn-unit-1",
		HeaderHeight: "12", HeaderTime: 1_800_000_000, StartedUnixSeconds: 1_800_000_000}
	for i := 0; i < n; i++ {
		r.Audits = append(r.Audits, RunAudit{LeaseID: fmt.Sprintf("l%d", i), ProviderID: "p1", ContentID: fmt.Sprintf("c%d", i),
			ContentVersion: 1, ManifestSHA256: strings.Repeat("0", 64), Deadline: 1_800_007_200,
			Result: report.ResultPass, ReportSHA256: strings.Repeat("1", 64)})
	}
	return r
}

// longestResult is the longest result string a run audit may carry.
func longestResult() string {
	longest := ""
	for _, r := range []string{report.ResultPass, report.ResultFail, report.ResultIncomplete} {
		if len(r) > len(longest) {
			longest = r
		}
	}
	return longest
}

// worstRun is the longest run of n audits the format admits: every id at
// msgkit.IDMaxBytes, the largest header height, times, content version and
// deadline, both digests, the longest result, and every reason of the
// classify catalogue (the producer only writes catalogue reasons, and an
// audit's reasons are sorted and unique, so no audit carries more).
func worstRun(n int) Run {
	reasons := make([]string, 0, len(classify.Results))
	for reason := range classify.Results {
		reasons = append(reasons, string(reason))
	}
	sort.Strings(reasons)
	id := func(prefix string, i int) string {
		s := fmt.Sprintf("%s%0*d", prefix, msgkit.IDMaxBytes-len(prefix), i)
		return s[:msgkit.IDMaxBytes]
	}
	r := Run{AuditorAddress: strings.Repeat("a", 40), AuditorKeyID: id("k", 0), ChainID: strings.Repeat("c", maxChainID),
		HeaderHeight: "9223372036854775807", HeaderTime: genesis.UnixSecondsMax, StartedUnixSeconds: genesis.UnixSecondsMax}
	for i := 0; i < n; i++ {
		r.Audits = append(r.Audits, RunAudit{LeaseID: id("l", i), ProviderID: id("p", i), ContentID: id("c", i),
			ContentVersion: genesis.SafeIntegerMax, ManifestSHA256: strings.Repeat("f", 64), Deadline: genesis.UnixSecondsMax,
			Result: longestResult(), Reasons: reasons, ReportSHA256: strings.Repeat("e", 64)})
	}
	r.Format = RunFormat
	return r
}

// canonicalLen is the length of a run's SVN-JCS-1 text without the audit
// count check, or -1 when jcs refuses it.
func canonicalLen(r Run) int {
	text, err := jcs.Canonicalize(r.object())
	if err != nil {
		return -1
	}
	return len(text)
}

// TestWorstCaseRunFitsTheEncodingLimit: the longest run the format admits
// for MaxRunAudits audits is a valid run and encodes within MaxRunBytes, so
// a run within the audit bound can always be stored. It also measures the
// worst-case capacity and checks that one audit more than that capacity is
// refused by jcs ("bounds"), so the limit the bound guards is real.
func TestWorstCaseRunFitsTheEncodingLimit(t *testing.T) {
	r := worstRun(MaxRunAudits)
	if err := r.validate(); err != nil {
		t.Fatalf("the worst-case run is not a valid run: %v", err)
	}
	raw, _, err := EncodeRun(r)
	if err != nil {
		t.Fatalf("%d worst-case audits: %v", MaxRunAudits, err)
	}
	if len(raw) > MaxRunBytes {
		t.Fatalf("%d worst-case audits encode to %d bytes", MaxRunAudits, len(raw))
	}
	capacity := MaxRunAudits
	for canonicalLen(worstRun(capacity+1)) >= 0 {
		capacity++
	}
	t.Logf("worst case: %d audits encode to %d of %d bytes; capacity %d audits", MaxRunAudits, len(raw), MaxRunBytes, capacity)
	if _, err := jcs.Canonicalize(worstRun(capacity + 1).object()); err == nil || !strings.Contains(err.Error(), "bounds") {
		t.Fatalf("%d worst-case audits: %v, want the jcs bounds refusal", capacity+1, err)
	}
	if got := canonicalLen(worstRun(capacity)); got < 0 || got > MaxRunBytes {
		t.Fatalf("capacity %d encodes to %d bytes", capacity, got)
	}
}

// TestMaxRunBytesIsTheJCSLimit pins MaxRunBytes to jcs: a text of exactly
// MaxRunBytes is returned and one byte more is refused.
func TestMaxRunBytesIsTheJCSLimit(t *testing.T) {
	at := strings.Repeat("x", MaxRunBytes-len(`{"a":""}`))
	if got, err := jcs.Canonicalize(map[string]any{"a": at}); err != nil || len(got) != MaxRunBytes {
		t.Fatalf("%d bytes: %d %v", MaxRunBytes, len(got), err)
	}
	if _, err := jcs.Canonicalize(map[string]any{"a": at + "x"}); err == nil {
		t.Fatalf("%d bytes accepted", MaxRunBytes+1)
	}
}

// TestRunAuditCountBoundary: MaxRunAudits audits encode and MaxRunAudits+1
// are refused by the audit count, before encoding. The reported case, 256
// short PASS audits (permitted before this bound; about 79 KB of text),
// is now refused by the count instead of failing in jcs.
func TestRunAuditCountBoundary(t *testing.T) {
	if _, _, err := EncodeRun(minimalRun(MaxRunAudits)); err != nil {
		t.Fatalf("%d audits: %v", MaxRunAudits, err)
	}
	for _, n := range []int{0, MaxRunAudits + 1, 256} {
		_, _, err := EncodeRun(minimalRun(n))
		if !errors.Is(err, ErrFormat) || !strings.Contains(err.Error(), fmt.Sprintf("1 to %d audits", MaxRunAudits)) {
			t.Fatalf("%d audits: %v, want the audit count refusal", n, err)
		}
	}
	if got := canonicalLen(minimalRun(256)); got >= 0 {
		t.Fatalf("256 short audits encode to %d bytes; the reported case no longer exceeds the limit", got)
	}
}
