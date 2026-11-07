package authority

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// payloadVectorFile is testdata/payload-vectors.json. The Python test
// tests/unit/test_payload_vectors.py reads the same file and checks it with
// goal 02's jcs.py and spec_machine.py.
type payloadVectorFile struct {
	Description string          `json:"description"`
	Vectors     []payloadVector `json:"vectors"`
}

type payloadVector struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	ParameterKey     string `json:"parameter_key"`
	ChainProposeCode string `json:"chain_propose_code"`
	PayloadJSON      string `json:"payload_json"`
	Canonical        string `json:"canonical"`
	SHA256           string `json:"sha256"`
}

func strictJSON(t *testing.T, raw []byte, out any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		t.Fatal(err)
	}
	if dec.More() {
		t.Fatal("trailing data")
	}
}

func readPayloadVectors(t *testing.T) []payloadVector {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "payload-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file payloadVectorFile
	strictJSON(t, raw, &file)
	if len(file.Vectors) == 0 {
		t.Fatal("no payload vectors")
	}
	return file.Vectors
}

// TestPayloadVectors checks every vector of testdata/payload-vectors.json
// against the chain code: the proto3 JSON decodes strictly into a
// ProposalPayload; SVN-JCS-1 of the raw text (chain/jcs) and the chain's
// canonical rebuild (CanonicalFromProto) both give the stored canonical
// text; its SHA-256 is the stored digest; the kind and parameter key match.
// A full ProposeChange of the vector on a fresh store succeeds when
// chain_propose_code is empty and returns that code otherwise (D-1), with
// the store unchanged. The file covers every v0 payload kind and every
// governed limit key at 1 and at its ceiling.
func TestPayloadVectors(t *testing.T) {
	vectors := readPayloadVectors(t)
	kinds := map[string]bool{}
	governed := map[string]bool{}
	ids := map[string]bool{}
	for _, v := range vectors {
		if ids[v.ID] {
			t.Fatalf("vector id %s twice", v.ID)
		}
		ids[v.ID] = true
		kinds[v.Kind] = true
		var payload authorityv1.ProposalPayload
		if err := protojson.Unmarshal([]byte(v.PayloadJSON), &payload); err != nil {
			t.Fatalf("%s: %v", v.ID, err)
		}
		canon, err := jcs.ParseAndCanonicalize(v.PayloadJSON)
		if err != nil || canon != v.Canonical {
			t.Fatalf("%s: SVN-JCS-1 of the raw text %q (%v), want %q", v.ID, canon, err, v.Canonical)
		}
		digest, out := PayloadDigest(&payload)
		if !out.Success() || hex.EncodeToString(digest) != v.SHA256 {
			t.Fatalf("%s: digest %x (%+v), want %s", v.ID, digest, out, v.SHA256)
		}
		change, kind, rebuilt, out := CanonicalFromProto(&payload, digest)
		if !out.Success() || kind != v.Kind || rebuilt != v.Canonical {
			t.Fatalf("%s: rebuilt %q kind %s (%+v)", v.ID, rebuilt, kind, out)
		}
		msg := &authorityv1.MsgProposeChange{
			ParameterKey:  v.ParameterKey,
			Payload:       &payload,
			PayloadDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digest},
		}
		if got := parameterKey(kind, change.NodeID, limitKey(msg), providerID(msg), contentID(msg)); got != v.ParameterKey {
			t.Fatalf("%s: parameter key %q, want %q", v.ID, got, v.ParameterKey)
		}
		if body := payload.GetGovernedLimit(); body != nil && v.ChainProposeCode == "" {
			ceiling, _ := msgkit.Ceiling(body.GetLimitKey())
			switch body.GetValue() {
			case 1:
				governed[body.GetLimitKey()+"/1"] = true
			case ceiling:
				governed[body.GetLimitKey()+"/ceiling"] = true
			}
		}
		kv, seats, _ := readyStore(t)
		before := dumpKV(kv)
		got := loadAt(t, kv, testHeight, testTime).ProposeChange(propose(seats[0].Signer, "pv-"+v.ID, 1, msg))
		if got.Code != v.ChainProposeCode {
			t.Fatalf("%s: propose %+v, want %q", v.ID, got, v.ChainProposeCode)
		}
		if !got.Success() {
			wantUnchanged(t, kv, before, v.ID)
		}
	}
	for _, kind := range []string{kindValidatorPower, kindGovernedLimit, kindAdmitProvider, kindDisableProvider, kindForceWithdraw} {
		if !kinds[kind] {
			t.Errorf("no vector of kind %s", kind)
		}
	}
	for _, key := range msgkit.LimitKeys() {
		for _, at := range []string{"1", "ceiling"} {
			if !governed[key+"/"+at] {
				t.Errorf("no accepted vector of %s at %s", key, at)
			}
		}
	}
}

// TestV0PayloadVectorsUnchanged decodes the two protocol payload vectors of
// docs/protocol/fixtures/vectors.json (goal 02) into proto and requires the
// chain's canonical rebuild to equal their canonical text byte for byte.
func TestV0PayloadVectorsUnchanged(t *testing.T) {
	path := filepath.Join(filepath.Dir(msgkittest.BoundsFile(t)), "fixtures", "vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Valid []struct {
			ID        string `json:"id"`
			InputRaw  string `json:"input_raw"`
			Canonical string `json:"canonical"`
		} `json:"valid"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	want := []string{"proposal-payload-validator-power", "proposal-payload-validator-power-zero"}
	var seen []string
	for _, v := range file.Valid {
		if !slices.Contains(want, v.ID) {
			continue
		}
		seen = append(seen, v.ID)
		var payload authorityv1.ProposalPayload
		if err := protojson.Unmarshal([]byte(v.InputRaw), &payload); err != nil {
			t.Fatalf("%s: %v", v.ID, err)
		}
		digest, out := PayloadDigest(&payload)
		if !out.Success() {
			t.Fatalf("%s: %+v", v.ID, out)
		}
		_, _, canon, out := CanonicalFromProto(&payload, digest)
		if !out.Success() || canon != v.Canonical {
			t.Fatalf("%s: canonical %q (%+v), want %q", v.ID, canon, out, v.Canonical)
		}
	}
	slices.Sort(seen)
	if !slices.Equal(seen, want) {
		t.Fatalf("protocol payload vectors %v, want %v", seen, want)
	}
}
