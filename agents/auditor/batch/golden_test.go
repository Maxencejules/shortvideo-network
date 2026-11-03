package batch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
)

var update = flag.Bool("update", false, "rewrite tests/fixtures/evidence/golden")

const (
	chainVectors = "../../../chain/x/evidence/testdata/sign-vectors.json"
	goldenDir    = "../../../tests/fixtures/evidence/golden"
)

type chainVector struct {
	Name      string `json:"name"`
	KeyLabel  string `json:"key_label"`
	PublicKey string `json:"public_key"`
	Signer    string `json:"signer"`
	Canonical string `json:"canonical"`
	SignBytes string `json:"sign_bytes_sha256"`
	Signature string `json:"signature"`
}

type vectorEnvelope struct {
	Attestation    bool   `json:"attestation"`
	ContentID      string `json:"content_id"`
	ContentVersion uint64 `json:"content_version"`
	Epoch          uint64 `json:"epoch"`
	EvidenceID     string `json:"evidence_id"`
	Expiry         int64  `json:"expiry_unix_seconds"`
	Kind           string `json:"kind"`
	LeaseID        string `json:"lease_id"`
	Observation    string `json:"observation_sha256"`
	SchemaVersion  uint32 `json:"schema_version"`
	KeyID          string `json:"signer_key_id"`
}

// TestProducerMatchesChainSignVectors rebuilds every chain golden vector
// (chain/x/evidence/testdata/sign-vectors.json) through the producer's own
// path: items through assemble, the signature through Sign with the
// label-derived TEST-ONLY key. The canonical payload, its sign-bytes hash,
// the public key, the signer, and the signature must all equal the
// vector's. The batch id and epoch are the vector's (vectors use synthetic
// batch ids, not BatchID, and maximal-ids uses epoch 2^53-1). The one vector with an unnamed kind number is a chain
// rejection vector; the producer only ever builds AUDITOR_ATTESTATION items.
func TestProducerMatchesChainSignVectors(t *testing.T) {
	raw, err := os.ReadFile(chainVectors)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Vectors []chainVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	var excluded, checked []string
	for _, v := range file.Vectors {
		var payload struct {
			BatchID   string           `json:"batch_id"`
			Envelopes []vectorEnvelope `json:"envelopes"`
			Signer    string           `json:"signer"`
		}
		if err := json.Unmarshal([]byte(v.Canonical), &payload); err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		producible := true
		for _, e := range payload.Envelopes {
			producible = producible && e.Kind == "AUDITOR_ATTESTATION" && e.Attestation && e.SchemaVersion == evidence.SchemaVersion
		}
		if !producible {
			excluded = append(excluded, v.Name)
			continue
		}
		key := testKey(t, v.KeyLabel)
		first := payload.Envelopes[0]
		p := Plan{Signer: key.Address(), KeyID: first.KeyID, HeaderTime: int64(first.Epoch) * evidence.EpochSeconds}
		items := make([]Item, len(payload.Envelopes))
		for i, e := range payload.Envelopes {
			obs, err := hex.DecodeString(e.Observation)
			if err != nil || len(obs) != 32 || e.Epoch != first.Epoch || e.KeyID != first.KeyID {
				t.Fatalf("%s: envelope %d", v.Name, i)
			}
			items[i] = Item{
				EvidenceID: e.EvidenceID, ProviderID: "p", LeaseID: e.LeaseID, ContentID: e.ContentID,
				ContentVersion: e.ContentVersion, Observation: [32]byte(obs), Expiry: e.Expiry,
			}
		}
		b, err := assemble(p, "p", items, "")
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		// Vectors use synthetic batch ids and, for maximal-ids, the largest
		// epoch, which no header time maps to; both are set as given.
		b.Msg.BatchId, b.Msg.Epoch = payload.BatchID, first.Epoch
		if err := Sign(b, key); err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		canonical, err := evidence.AuditCanonical(b.Msg)
		if err != nil {
			t.Fatal(err)
		}
		signBytes, _ := evidence.AuditSignBytes(b.Msg)
		sum := sha256.Sum256(signBytes)
		sig := b.Msg.GetAuditorSignature()
		switch {
		case canonical != v.Canonical:
			t.Fatalf("%s: canonical payload differs", v.Name)
		case hex.EncodeToString(sum[:]) != v.SignBytes:
			t.Fatalf("%s: sign bytes differ", v.Name)
		case hex.EncodeToString(sig.GetPublicKey()) != v.PublicKey || hex.EncodeToString(b.Msg.GetSigner()) != v.Signer:
			t.Fatalf("%s: key or signer differs", v.Name)
		case hex.EncodeToString(sig.GetSignature()) != v.Signature:
			t.Fatalf("%s: signature differs", v.Name)
		}
		// The producer's envelope files are the vector's envelopes.
		texts, err := Envelopes(b.Msg)
		if err != nil {
			t.Fatal(err)
		}
		var objects struct {
			Envelopes []json.RawMessage `json:"envelopes"`
		}
		if err := json.Unmarshal([]byte(v.Canonical), &objects); err != nil {
			t.Fatal(err)
		}
		for i, text := range texts {
			if !bytes.Equal(text, objects.Envelopes[i]) {
				t.Fatalf("%s: envelope %d differs from the vector", v.Name, i)
			}
		}
		checked = append(checked, v.Name)
	}
	if !reflect.DeepEqual(excluded, []string{"unknown-kind-number"}) || len(checked) != len(file.Vectors)-1 {
		t.Fatalf("checked %v, excluded %v", checked, excluded)
	}
	t.Logf("producer matches chain vectors %v", checked)
}

type goldenItem struct {
	EvidenceID        string `json:"evidence_id"`
	Result            string `json:"result"`
	Report            string `json:"report"`
	Envelope          string `json:"envelope"`
	ObservationSHA256 string `json:"observation_sha256"`
}

type goldenIndex struct {
	Comment   string       `json:"comment"`
	KeyLabel  string       `json:"key_label"`
	PublicKey string       `json:"public_key"`
	Signer    string       `json:"signer"`
	BatchID   string       `json:"batch_id"`
	Epoch     uint64       `json:"epoch"`
	Canonical string       `json:"canonical"`
	Signature string       `json:"signature"`
	Items     []goldenItem `json:"items"`
}

// goldenFiles builds the golden producer output: one batch of a PASS and a
// FAIL audit by the TEST-ONLY auditor-1 key, through NewItem, Build, Sign,
// and Envelopes, keyed by path under goldenDir.
func goldenFiles(t *testing.T) map[string][]byte {
	t.Helper()
	p := plan(t, keyLabel)
	key := testKey(t, keyLabel)
	audits := []struct{ lease, content, result string }{
		{"lease-golden-pass", "content-golden-pass", report.ResultPass},
		{"lease-golden-fail", "content-golden-fail", report.ResultFail},
	}
	files := map[string][]byte{}
	var items []Item
	reports := map[string]string{}
	for _, a := range audits {
		raw := reportBytes(t, keyLabel, a.lease, "provider-golden", a.content, a.result, headerTime-60)
		sum := sha256.Sum256(raw)
		name := "reports/" + hex.EncodeToString(sum[:]) + ".json"
		files[name] = raw
		it, err := NewItem(p, Audit{Report: raw, Deadline: headerTime + 7200, ManifestSHA256: manifestOf(a.content)})
		if err != nil {
			t.Fatal(err)
		}
		reports[it.EvidenceID] = name
		items = append(items, it)
	}
	batches, err := Build(p, items)
	if err != nil || len(batches) != 1 {
		t.Fatalf("%d batches: %v", len(batches), err)
	}
	b := batches[0]
	if err := Sign(b, key); err != nil {
		t.Fatal(err)
	}
	canonical, err := evidence.AuditCanonical(b.Msg)
	if err != nil {
		t.Fatal(err)
	}
	texts, err := Envelopes(b.Msg)
	if err != nil {
		t.Fatal(err)
	}
	index := goldenIndex{
		Comment: "Golden producer output (agents/auditor/batch, TestGoldenEnvelopeFixtures). TEST-ONLY key: ed25519 seed = " +
			"sha256(\"svn-evidence-test-seed:\" + key_label); no private key is stored. Synthetic reports and leases; " +
			"nothing was fetched. Envelope files are byte-identical to the producer's envelopes/<evidence_id>.json and to " +
			"the envelopes inside canonical, which the signature covers under SVN-EVIDENCE-ATTESTATION-1.",
		KeyLabel: keyLabel, PublicKey: hex.EncodeToString(key.PublicKey()), Signer: hex.EncodeToString(b.Msg.GetSigner()),
		BatchID: b.ID(), Epoch: b.Epoch(), Canonical: canonical, Signature: hex.EncodeToString(b.Msg.GetAuditorSignature().GetSignature()),
	}
	for i, it := range b.Items {
		name := "envelopes/" + it.EvidenceID + ".json"
		files[name] = texts[i]
		index.Items = append(index.Items, goldenItem{
			EvidenceID: it.EvidenceID, Result: it.Result, Report: reports[it.EvidenceID], Envelope: name,
			ObservationSHA256: hex.EncodeToString(it.Observation[:]),
		})
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files["index.json"] = append(raw, '\n')
	return files
}

// TestGoldenEnvelopeFixtures keeps tests/fixtures/evidence/golden equal to
// the producer's output (run with -update to rewrite it), and the directory
// holds nothing else. tests/unit/test_evidence_envelopes.py validates these
// files in Python against the frozen envelope schema, re-canonicalizes them,
// and verifies the signature: the cross-language check.
func TestGoldenEnvelopeFixtures(t *testing.T) {
	files := goldenFiles(t)
	if *update {
		for _, generated := range []string{"envelopes", "reports", "index.json"} {
			if err := os.RemoveAll(filepath.Join(goldenDir, generated)); err != nil {
				t.Fatal(err)
			}
		}
		for name, data := range files {
			path := filepath.Join(goldenDir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	var onDisk []string
	err := filepath.WalkDir(goldenDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) == "README.md" {
			return nil
		}
		rel, _ := filepath.Rel(goldenDir, path)
		onDisk = append(onDisk, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("golden directory: %v (run go test ./batch -run TestGoldenEnvelopeFixtures -update)", err)
	}
	var want []string
	for name := range files {
		want = append(want, name)
	}
	sort.Strings(onDisk)
	sort.Strings(want)
	if !reflect.DeepEqual(onDisk, want) {
		t.Fatalf("golden files %v, want %v", onDisk, want)
	}
	for name, data := range files {
		got, err := os.ReadFile(filepath.Join(goldenDir, name))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s differs from the producer's output (%v)", name, err)
		}
	}
	// Every envelope verifies through the chain's own check of its batch.
	var index goldenIndex
	if err := json.Unmarshal(files["index.json"], &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Items) != 2 || index.Items[0].Result == index.Items[1].Result {
		t.Fatalf("golden items %+v", index.Items)
	}
	t.Logf("golden batch %s epoch %s: %d envelopes", index.BatchID, strconv.FormatUint(index.Epoch, 10), len(index.Items))
	if !msgkit.ValidID(index.BatchID) {
		t.Fatal("golden batch id outside the pattern")
	}
}
