package labels_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/labels"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
)

// The same rules run in apps/web/test/evidence-labels.test.ts against the
// TypeScript copy of the catalog. A deliberate wording change edits
// evidence-labels.json, apps/web/src/evidence-labels.ts, the pinned list
// here and in the Node test, and, for a sentence with a denied phrase, the
// negation allowlist of testdata/label-rules.json, in one change.

const (
	catalogFile = "evidence-labels.json"
	rulesFile   = "testdata/label-rules.json"
)

// repoRoot is the repository root, three levels above this package.
var repoRoot = filepath.Join("..", "..", "..")

// rules is the shared fixture testdata/label-rules.json: the identifiers,
// the denied phrases, and the allowed negation sentences.
type rules struct {
	Fixture     string `json:"fixture"`
	Identifiers struct {
		Catalog    string `json:"catalog"`
		ClaimLimit string `json:"claim_limit"`
	} `json:"identifiers"`
	Denylist          []string `json:"denylist"`
	NegationAllowlist []string `json:"negation_allowlist"`
}

// pinned is every catalog string, sentence by sentence, in file order. It is
// written here, independently of the Node test's own list, so that a
// catalog change fails both tests until both lists are edited.
var pinned = []struct {
	path      string
	sentences []string
}{
	{"modes.AUDITOR_ATTESTATION.label", []string{"Auditor attestation"}},
	{"modes.AUDITOR_ATTESTATION.text", []string{
		"An admitted auditor signed this statement about one lease.",
		"It is an attestation.",
		"It is not proof of storage.",
	}},
	{"modes.PROVIDER_RECEIPT.label", []string{"Provider receipt"}},
	{"modes.PROVIDER_RECEIPT.text", []string{
		"A transfer receipt attributed to a provider.",
		"This beta does not check its signature and does not record it on chain.",
		"It is an attestation.",
		"It is not proof of storage or of future delivery.",
	}},
	{"states.RECORDED.label", []string{"Recorded"}},
	{"states.RECORDED.text", []string{
		"The auditor reported the lease's manifest digest.",
		"The chain compared two digests and fetched nothing.",
		"The manifest digest is public.",
		"Reporting it does not show that any byte was fetched.",
		"A recorded attestation describes one past observation.",
		"It does not show that the lease is held or the content is available now.",
	}},
	{"states.REJECTED.label", []string{"Rejected"}},
	{"states.REJECTED.text", []string{
		"The auditor reported a digest that differs from the lease's manifest digest.",
		"This can mean a failed or mismatched check.",
		"The chain did not check why.",
		"A rejected record does not expire.",
		"The chain cannot tell whether the provider or the auditor caused the difference.",
	}},
	{"states.EXPIRED.label", []string{"Expired"}},
	{"states.EXPIRED.text", []string{"The attestation's expiry time has passed."}},
	{"event_accepted", []string{
		"EvidenceAccepted means the record was written to the ledger.",
		"It does not mean the check passed.",
	}},
	{"acceptance.label", []string{"Provider acceptance"}},
	{"acceptance.text", []string{
		"The provider signed the package digest, byte count and terms.",
		"Acceptance is a promise, not possession.",
	}},
	{"limitations.0", []string{"Auditors can lie."}},
	{"limitations.1", []string{"Providers may fetch from another host."}},
	{"limitations.2", []string{"Node IDs do not prove independent replicas."}},
	{"limitations.3", []string{"A valid signature does not make an observation true."}},
	{"limitations.4", []string{"Accounting success is not physical capacity or operator independence."}},
	{"limitations.5", []string{"A sampled check covers only the sampled segments."}},
	{"limitations.6", []string{"This auditor is not shown to be independent of the provider."}},
	{"limitations.7", []string{"No evidence record renews, extends or revives a lease, in any state."}},
	{"limitations.8", []string{
		"No cryptographic storage proof exists in this beta.",
		"It is a separate decision gate.",
	}},
}

// denylist is the full list of denied phrases; the shared rules file must
// hold exactly these, so removing one there fails this test.
var denylist = []string{
	"proof of storage", "storage proof", "proof-of-storage", "verified storage", "verified copy",
	"verified replica", "unique replica", "independent copies", "independently operated", "trustless",
	"guarantee", "lawful", "legal", "certified", "audited network", "independent", "verified", "proven",
	"future delivery", "quality", "independence", "legality", "illegal", "unlawful", "future quality", "merkle",
	"inclusion proof", "unique", "uniquely stored", "proof", "prove", "proof of possession",
	"proof of replication", "proof of retrievability", "filecoin", "reward", "slash", "verif", "certif",
	"confirm", "audited", "honest", "truthful", "accurate", "authentic", "trusted", "trustworthy", "true",
	"replica", "replicat", "redundan", "durab", "permanent", "immutable", "available", "availability",
	"retrievab", "possess", "stored", "stores", "renew", "extend", "reviv", "production", "secure",
}

func loadRules(t *testing.T) rules {
	t.Helper()
	raw, err := os.ReadFile(rulesFile)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var r rules
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("%s: %v", rulesFile, err)
	}
	if r.Fixture != "svn-evidence-label-rules-v0" {
		t.Fatalf("%s: fixture %q", rulesFile, r.Fixture)
	}
	return r
}

func catalog(t *testing.T) labels.Catalog {
	t.Helper()
	c, err := labels.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// sentences splits a text after each ". ". Joining the parts with one
// space gives the text back.
func sentences(text string) []string {
	parts := strings.Split(text, ". ")
	for i := 0; i < len(parts)-1; i++ {
		parts[i] += "."
	}
	return parts
}

// leaves walks the decoded catalog file and returns every string leaf by
// dotted path. A leaf that is not a string fails the test.
func leaves(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		join := func(key string) string {
			if path == "" {
				return key
			}
			return path + "." + key
		}
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				walk(join(key), child)
			}
		case []any:
			for i, child := range x {
				walk(join(strconv.Itoa(i)), child)
			}
		case string:
			out[path] = x
		default:
			t.Errorf("%s: %T is not a string", path, v)
		}
	}
	walk("", doc)
	return out
}

// humanTexts returns every label, text, and limitation of the catalog by
// path: every string leaf except the two identifiers.
func humanTexts(t *testing.T) map[string]string {
	t.Helper()
	out := leaves(t)
	delete(out, "catalog")
	delete(out, "claim_limit")
	return out
}

// sortedKeys returns the keys of a map in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// camelBoundary is a lower-case letter followed by an upper-case one.
var camelBoundary = regexp.MustCompile(`([a-z])([A-Z])`)

// deniedIn returns the denied phrases found in one sentence. The sentence is
// normalized first: an underscore is a space and a camel-case boundary
// gets a space, so STORAGE_PROOF and storageProof read as "storage proof".
// A phrase is matched case-insensitively where it starts at an ASCII word
// boundary.
func deniedIn(denylist []string, sentence string) []string {
	sentence = camelBoundary.ReplaceAllString(strings.ReplaceAll(sentence, "_", " "), "$1 $2")
	var found []string
	for _, phrase := range denylist {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(phrase)).MatchString(sentence) {
			found = append(found, phrase)
		}
	}
	return found
}

// violations returns the denied phrases in a text, after the sentences on
// the negation allowlist are removed.
func violations(r rules, text string) []string {
	var out []string
	for _, sentence := range sentences(text) {
		if slices.Contains(r.NegationAllowlist, sentence) {
			continue
		}
		for _, phrase := range deniedIn(r.Denylist, sentence) {
			out = append(out, fmt.Sprintf("%q contains %q", sentence, phrase))
		}
	}
	return out
}

var negation = regexp.MustCompile(`(?i)\b(no|not|cannot|never)\b`)

// The catalog file is the canonical two-space encoding of the typed
// catalog: the struct covers every key, in file order, with no repeat, and
// the bytes are what JSON.stringify(value, null, 2) writes in the Node test.
func TestCatalogFileIsCanonical(t *testing.T) {
	raw, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(catalog(t)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), raw) {
		t.Fatalf("%s is not the canonical encoding of the parsed catalog:\n%s", catalogFile, buf.String())
	}
}

// Every catalog string is pinned in this file, sentence by sentence;
// removing, adding, or changing one fails this test and the Node test.
func TestPinnedSentences(t *testing.T) {
	r := loadRules(t)
	all := leaves(t)
	if all["catalog"] != r.Identifiers.Catalog || all["catalog"] != labels.CatalogID {
		t.Errorf("catalog = %q, pinned %q, constant %q", all["catalog"], r.Identifiers.Catalog, labels.CatalogID)
	}
	if all["claim_limit"] != r.Identifiers.ClaimLimit {
		t.Errorf("claim_limit = %q, pinned %q", all["claim_limit"], r.Identifiers.ClaimLimit)
	}
	texts := humanTexts(t)
	seen := map[string]bool{}
	for _, p := range pinned {
		if seen[p.path] {
			t.Errorf("%s is pinned twice", p.path)
		}
		seen[p.path] = true
		got, ok := texts[p.path]
		if !ok {
			t.Errorf("%s is pinned but not in the catalog", p.path)
			continue
		}
		if !slices.Equal(sentences(got), p.sentences) {
			t.Errorf("%s:\n got  %q\n want %q", p.path, sentences(got), p.sentences)
		}
		if strings.Join(p.sentences, " ") != got {
			t.Errorf("%s: the pinned sentences do not join to the text", p.path)
		}
	}
	for _, path := range sortedKeys(texts) {
		if !seen[path] {
			t.Errorf("%s is in the catalog but not pinned", path)
		}
	}
	if len(pinned) != len(texts) {
		t.Errorf("%d pinned strings, %d catalog strings", len(pinned), len(texts))
	}
}

// Every label is one phrase without a full stop. Every text sentence
// starts with an upper-case letter and ends with one full stop.
func TestSentenceShape(t *testing.T) {
	sentenceShape := regexp.MustCompile(`^[A-Z][^.]*[^. ]\.$`)
	for path, text := range humanTexts(t) {
		if strings.HasSuffix(path, ".label") {
			if strings.Contains(text, ".") || len(sentences(text)) != 1 {
				t.Errorf("%s: label %q is not one phrase", path, text)
			}
			continue
		}
		for _, s := range sentences(text) {
			if !sentenceShape.MatchString(s) {
				t.Errorf("%s: %q is not one sentence", path, s)
			}
		}
	}
}

// Requirement 4 of the goal: the mode is shown explicitly as auditor
// attestation or provider receipt. Exactly two modes exist, keyed by the
// chain's KindName, and each mode text says the record is an attestation.
func TestModesAreExplicit(t *testing.T) {
	c := catalog(t)
	modes := map[string]string{}
	for path := range humanTexts(t) {
		if parts := strings.Split(path, "."); parts[0] == "modes" {
			modes[parts[1]] = path
		}
	}
	if keys := sortedKeys(modes); !slices.Equal(keys, []string{"AUDITOR_ATTESTATION", "PROVIDER_RECEIPT"}) {
		t.Fatalf("mode keys %v", keys)
	}
	// Untyped constants convert to evidencev1.EvidenceKind: 1 is
	// PROVIDER_RECEIPT and 2 is AUDITOR_ATTESTATION (state.proto).
	for name, want := range map[string]string{
		evidence.KindName(2): "Auditor attestation",
		evidence.KindName(1): "Provider receipt",
	} {
		entry, err := c.Mode(name)
		if err != nil {
			t.Fatalf("Mode(%q): %v", name, err)
		}
		if entry.Label != want {
			t.Errorf("Mode(%q).Label = %q, want %q", name, entry.Label, want)
		}
		if !slices.Contains(sentences(entry.Text), "It is an attestation.") {
			t.Errorf("Mode(%q).Text does not say it is an attestation", name)
		}
	}
	for _, name := range []string{
		evidence.KindName(0), evidence.KindName(3), evidence.KindName(4), evidence.KindName(5),
		"", "STORAGE_PROOF", "UNIQUE_REPLICA_PROOF", "FUTURE_DELIVERY", "EVIDENCE_KIND_AUDITOR_ATTESTATION",
		"auditor_attestation",
	} {
		if _, err := c.Mode(name); !errors.Is(err, labels.ErrUnknownMode) {
			t.Errorf("Mode(%q) = %v, want ErrUnknownMode", name, err)
		}
	}
	if got := c.Modes.ProviderReceipt.Text; !slices.Contains(sentences(got), "This beta does not check its signature and does not record it on chain.") {
		t.Errorf("the provider receipt text does not say the beta neither checks nor records it: %q", got)
	}
}

// State keys are the chain's StateName values, and the three reported
// states are the only ones.
func TestStatesMatchChainNames(t *testing.T) {
	c := catalog(t)
	states := map[string]bool{}
	for path := range humanTexts(t) {
		if parts := strings.Split(path, "."); parts[0] == "states" {
			states[parts[1]] = true
		}
	}
	// Untyped constants convert to evidencev1.EvidenceState: 1 RECORDED,
	// 2 EXPIRED, 3 REJECTED (state.proto).
	chain := []string{evidence.StateName(1), evidence.StateName(2), evidence.StateName(3)}
	sort.Strings(chain)
	if keys := sortedKeys(states); !slices.Equal(keys, chain) {
		t.Fatalf("state keys %v, chain names %v", keys, chain)
	}
	for _, name := range chain {
		if _, err := c.State(name); err != nil {
			t.Errorf("State(%q): %v", name, err)
		}
	}
	for _, name := range []string{evidence.StateName(0), evidence.StateName(4), "", "EVIDENCE_STATE_RECORDED", "recorded"} {
		if _, err := c.State(name); !errors.Is(err, labels.ErrUnknownState) {
			t.Errorf("State(%q) = %v, want ErrUnknownState", name, err)
		}
	}
}

// The goal's three limitations (requirement 4), REQ-INV-007, and the
// decision-gate sentence (DR-09-G1) appear verbatim.
func TestRequiredLimitations(t *testing.T) {
	c := catalog(t)
	for _, want := range []string{
		"Auditors can lie.",
		"Providers may fetch from another host.",
		"Node IDs do not prove independent replicas.",
		"Accounting success is not physical capacity or operator independence.",
		"A valid signature does not make an observation true.",
		"No cryptographic storage proof exists in this beta. It is a separate decision gate.",
	} {
		if !slices.Contains(c.Limitations, want) {
			t.Errorf("limitations lack %q", want)
		}
	}
}

// REQ-EVID-002: the acceptance text says what the provider signed and calls
// it a promise, not possession.
func TestAcceptanceIsAPromise(t *testing.T) {
	c := catalog(t)
	if c.Acceptance.Label != "Provider acceptance" {
		t.Errorf("acceptance label %q", c.Acceptance.Label)
	}
	got := sentences(c.Acceptance.Text)
	for _, want := range []string{
		"The provider signed the package digest, byte count and terms.",
		"Acceptance is a promise, not possession.",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("acceptance text lacks %q", want)
		}
	}
}

// A recorded state is one past observation of a public digest; a rejected
// state blames nobody; an accepted event is not a passed check.
func TestStateTextsLimitTheClaim(t *testing.T) {
	c := catalog(t)
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"RECORDED", c.States.Recorded.Text, []string{
			"The chain compared two digests and fetched nothing.",
			"The manifest digest is public.",
			"Reporting it does not show that any byte was fetched.",
			"A recorded attestation describes one past observation.",
			"It does not show that the lease is held or the content is available now.",
		}},
		{"REJECTED", c.States.Rejected.Text, []string{
			"The chain did not check why.",
			"A rejected record does not expire.",
			"The chain cannot tell whether the provider or the auditor caused the difference.",
		}},
		{"event_accepted", c.EventAccepted, []string{
			"It does not mean the check passed.",
		}},
	} {
		got := sentences(tc.text)
		for _, want := range tc.want {
			if !slices.Contains(got, want) {
				t.Errorf("%s lacks %q", tc.name, want)
			}
		}
	}
}

// The no-renewal sentence is catalog-wide: it is a limitation, and no mode,
// state, acceptance, or event text mentions renewing, extending, reviving,
// or prolonging a lease, because every renewal that names evidence is
// rejected whatever the evidence state.
func TestRenewalSentenceIsCatalogWide(t *testing.T) {
	const renewal = "No evidence record renews, extends or revives a lease, in any state."
	c := catalog(t)
	if !slices.Contains(c.Limitations, renewal) {
		t.Fatalf("limitations lack %q", renewal)
	}
	renew := regexp.MustCompile(`(?i)\b(renew|extend|reviv|prolong)`)
	for path, text := range humanTexts(t) {
		if strings.HasPrefix(path, "limitations.") {
			continue
		}
		if strings.Contains(text, renewal) || renew.MatchString(text) {
			t.Errorf("%s mentions renewal: %q", path, text)
		}
	}
}

// claim_limit is the envelope constant of the chain, the report, and the
// frozen envelope schema, and the schema's kinds are the two modes.
func TestClaimLimitMatchesEnvelope(t *testing.T) {
	c := catalog(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "protocol", "schemas", "evidence-envelope.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
			ClaimLimit struct {
				Const string `json:"const"`
			} `json:"claim_limit"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"catalog":           c.ClaimLimit,
		"labels.ClaimLimit": labels.ClaimLimit,
		"evidence":          evidence.ClaimLimit,
		"report":            report.ClaimLimit,
		"schema":            schema.Properties.ClaimLimit.Const,
	} {
		if got != "ATTESTATION_NOT_STORAGE_PROOF" {
			t.Errorf("%s claim_limit = %q", name, got)
		}
	}
	kinds := slices.Clone(schema.Properties.Kind.Enum)
	sort.Strings(kinds)
	if !slices.Equal(kinds, []string{"AUDITOR_ATTESTATION", "PROVIDER_RECEIPT"}) {
		t.Errorf("schema kinds %v", kinds)
	}
	for _, kind := range kinds {
		if _, err := c.Mode(kind); err != nil {
			t.Errorf("schema kind %q has no mode: %v", kind, err)
		}
	}
}

// Each limitation fits the audit report's per-string bound, so the
// producer can copy it unchanged.
func TestLimitationsFitReportBound(t *testing.T) {
	if labels.MaxTextBytes > report.MaxLimitationBytes {
		t.Fatalf("MaxTextBytes %d above report.MaxLimitationBytes %d", labels.MaxTextBytes, report.MaxLimitationBytes)
	}
	for i, text := range catalog(t).Limitations {
		if len(text) > report.MaxLimitationBytes {
			t.Errorf("limitation %d is %d bytes", i, len(text))
		}
	}
}

// The shared rules are well formed, and their denylist is exactly the list
// in this file: the denied phrases of REQ-EVID-001 (storage proof,
// independence, legality, future quality), REQ-EVID-007 (Merkle inclusion
// as a unique replica), the goal (Filecoin, rewards, slashing), claims of
// verification, truth, replication, availability, possession, renewal, or
// production readiness, and the plan's list. Each allowed negation
// sentence is in the catalog, holds a denied phrase, and negates.
func TestDenylistRulesAreWellFormed(t *testing.T) {
	r := loadRules(t)
	phrase := regexp.MustCompile(`^[a-z][a-z -]*[a-z]$`)
	seen := map[string]bool{}
	for _, p := range r.Denylist {
		if !phrase.MatchString(p) || seen[p] {
			t.Errorf("denylist entry %q is malformed or repeated", p)
		}
		seen[p] = true
	}
	if got, want := slices.Sorted(slices.Values(r.Denylist)), slices.Sorted(slices.Values(denylist)); !slices.Equal(got, want) {
		t.Errorf("denylist of %s:\n got  %q\n want %q", rulesFile, got, want)
	}
	var catalogSentences []string
	for _, text := range humanTexts(t) {
		catalogSentences = append(catalogSentences, sentences(text)...)
	}
	allowed := map[string]bool{}
	for _, s := range r.NegationAllowlist {
		if allowed[s] {
			t.Errorf("allowlist repeats %q", s)
		}
		allowed[s] = true
		if !slices.Contains(catalogSentences, s) {
			t.Errorf("allowlist sentence %q is not a catalog sentence", s)
		}
		if len(deniedIn(r.Denylist, s)) == 0 {
			t.Errorf("allowlist sentence %q holds no denied phrase; remove it", s)
		}
		if !negation.MatchString(s) {
			t.Errorf("allowlist sentence %q does not negate", s)
		}
	}
}

// No catalog string claims proof of storage, a unique replica,
// independence, legality, future quality, or a Merkle replica proof, except
// in the listed negation sentences (REQ-EVID-001, REQ-EVID-007).
func TestNoDeniedClaims(t *testing.T) {
	r := loadRules(t)
	texts := humanTexts(t)
	for _, path := range sortedKeys(texts) {
		for _, v := range violations(r, texts[path]) {
			t.Errorf("%s: %s", path, v)
		}
	}
}

// The scanner itself finds what it should: each denied phrase alone and in
// upper case, a denied claim next to an allowed sentence, and an allowed
// sentence extended inside one sentence; it passes the allowed sentences.
func TestDenylistScannerDetects(t *testing.T) {
	r := loadRules(t)
	for _, p := range r.Denylist {
		for _, s := range []string{p, strings.ToUpper(p), "This is " + p + " here."} {
			if len(violations(r, s)) == 0 {
				t.Errorf("%q is not detected", s)
			}
		}
	}
	for _, s := range []string{
		"A Merkle inclusion shows a unique replica.",
		"It is not proof of storage. It is proof of storage.",
		"It is not proof of storage, but it is lawful.",
		"Independently operated nodes hold verified copies.",
		"Future quality is guaranteed.",
		"This is a STORAGE_PROOF.",
		"A storageProof was recorded.",
		"The chain verifies storage.",
		"The auditor is honest.",
		"The record extends the lease.",
		"This beta is production ready.",
	} {
		if len(violations(r, s)) == 0 {
			t.Errorf("%q is not detected", s)
		}
	}
	for _, s := range r.NegationAllowlist {
		if v := violations(r, s); len(v) != 0 {
			t.Errorf("allowed sentence flagged: %v", v)
		}
	}
	for _, s := range []string{"An approved improvement.", "Unverified host clock."} {
		if v := violations(r, s); len(v) != 0 {
			t.Errorf("%q flagged: %v (a phrase must start at a word boundary)", s, v)
		}
	}
}

// Embedded returns a fresh value each call.
func TestEmbeddedReturnsIndependentValues(t *testing.T) {
	a := catalog(t)
	want := a.Limitations[0]
	a.Limitations[0] = "changed"
	if b := catalog(t); b.Limitations[0] != want {
		t.Fatalf("a caller's change reached the next call: %q", b.Limitations[0])
	}
}

// missingFrom returns the limitations that a document does not contain
// verbatim. Runs of white space count as one space, as Markdown renders a
// wrapped line.
func missingFrom(doc string, limitations []string) []string {
	flat := strings.Join(strings.Fields(doc), " ")
	var missing []string
	for _, text := range limitations {
		if !strings.Contains(flat, text) {
			missing = append(missing, text)
		}
	}
	return missing
}

func TestMissingFromChecker(t *testing.T) {
	limits := []string{"Auditors can lie.", "No cryptographic storage proof exists in this beta. It is a separate decision gate."}
	wrapped := "Limits:\n\n- Auditors can\n  lie.\n- No cryptographic storage proof exists in this beta.\n  It is a separate decision gate.\n"
	if m := missingFrom(wrapped, limits); len(m) != 0 {
		t.Errorf("wrapped document: missing %q", m)
	}
	partial := "- Auditors can lie.\n- No cryptographic storage proof exists in this beta. It is a separate decision gate (DR-09-G1).\n"
	if m := missingFrom(partial, limits); !slices.Equal(m, limits[1:]) {
		t.Errorf("partial document: missing %q", m)
	}
	if m := missingFrom("", limits); !slices.Equal(m, limits) {
		t.Errorf("empty document: missing %q", m)
	}
}

// The module README and the architecture page contain every limitation
// verbatim. This test fails, and is not skipped, until both documents
// carry them (chain/x/evidence/README.md is P1's; docs/architecture/
// evidence.md is the integration package's).
func TestDocsContainLimitations(t *testing.T) {
	c := catalog(t)
	for _, rel := range []string{
		filepath.Join("chain", "x", "evidence", "README.md"),
		filepath.Join("docs", "architecture", "evidence.md"),
	} {
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Errorf("%s: %v", filepath.ToSlash(rel), err)
			continue
		}
		for _, text := range missingFrom(string(raw), c.Limitations) {
			t.Errorf("%s lacks %q", filepath.ToSlash(rel), text)
		}
	}
}
