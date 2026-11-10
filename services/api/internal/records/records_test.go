package records

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyHistoryAndReplay(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Domain string `json:"domain"`
		Keys   []struct {
			KeyID  string `json:"key_id"`
			Public string `json:"public"`
		} `json:"keys"`
		Records []struct {
			Canonical string `json:"canonical"`
			Signature string `json:"signature"`
			KeyID     string `json:"key_id"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Domain != Domain || len(doc.Records) != 3 {
		t.Fatalf("%s %d", doc.Domain, len(doc.Records))
	}
	keys := map[string]Key{}
	for _, item := range doc.Keys {
		pub, err := hex.DecodeString(item.Public)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			t.Fatal(item.KeyID)
		}
		keys[item.KeyID] = Key{Public: pub, Active: true}
	}
	script := `import json, sys
sys.path.insert(0, sys.argv[1])
import jcs
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
doc = json.load(open(sys.argv[2]))
pubs = {item["key_id"]: bytes.fromhex(item["public"]) for item in doc["keys"]}
prev = None
for rec in doc["records"]:
    parsed = json.loads(rec["canonical"])
    if jcs.canonicalize(parsed) != rec["canonical"]:
        raise SystemExit("canonical")
    Ed25519PublicKey.from_public_bytes(pubs[rec["key_id"]]).verify(bytes.fromhex(rec["signature"]), b"SVN-PUBLIC-RECORD-1\x00" + rec["canonical"].encode())
    if prev is not None and parsed["previous_digest"] != prev:
        raise SystemExit("previous")
    import hashlib
    prev = hashlib.sha256(rec["canonical"].encode()).hexdigest()
print("ok")
`
	cmd := exec.Command("python3", "-", filepath.Join(root, "docs/evidence/goal-02"), "testdata/history.json")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python %v %s", err, out)
	}
	if string(out) != "ok\n" {
		t.Fatalf("python %s", out)
	}

	var history History
	first, err := Accept(&history, doc.Records[0].Canonical, mustSig(t, doc.Records[0].Signature), keys)
	if err != nil || first.Sequence != 0 || first.KeyID != "k-old" {
		t.Fatal(err, first)
	}
	retired := keys["k-old"]
	retired.Active = false
	keys["k-old"] = retired
	if _, err := Verify(doc.Records[0].Canonical, mustSig(t, doc.Records[0].Signature), keys); err != nil {
		t.Fatal("retired key no longer verifies the old record", err)
	}
	second, err := Accept(&history, doc.Records[1].Canonical, mustSig(t, doc.Records[1].Signature), keys)
	if err != nil || second.KeyID != "k-new" || second.Sequence != 1 {
		t.Fatal(err, second)
	}
	if _, err := Accept(&history, doc.Records[0].Canonical, mustSig(t, doc.Records[0].Signature), keys); !errors.Is(err, ErrReplay) {
		t.Fatal("replay", err)
	}
	if _, err := Accept(&history, doc.Records[2].Canonical, mustSig(t, doc.Records[2].Signature), keys); !errors.Is(err, ErrKey) {
		t.Fatal("retired key signed a new record", err)
	}
	bad := []byte(doc.Records[1].Canonical)
	bad[len(bad)-2] ^= 1
	if _, err := Verify(string(bad), mustSig(t, doc.Records[1].Signature), keys); !errors.Is(err, ErrSignature) && !errors.Is(err, ErrMalformed) {
		t.Fatal("tamper", err)
	}
}

func mustSig(t *testing.T, hexSig string) []byte {
	t.Helper()
	sig, err := hex.DecodeString(hexSig)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs/evidence/goal-02/jcs.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root")
		}
		dir = parent
	}
}
