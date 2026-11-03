package keys

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"

	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
)

// testSeed is a TEST-ONLY seed derived from a label, the rule of the chain's
// golden vectors; no private key is stored in the repository.
func testSeed(label string) []byte {
	sum := sha256.Sum256([]byte("svn-evidence-test-seed:" + label))
	return sum[:]
}

// writeDevnetKey writes a key file the way `svnd devnet init` writes seat
// keys: hex of the 64-byte SDK private key and a newline, mode 0600.
func writeDevnetKey(t *testing.T, path string, key *sdked.PrivKey, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key.Bytes())+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateLoadAndAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auditor.hex")
	key, err := Generate(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != FileMode || info.Size() != fileBytes {
		t.Fatalf("mode %v size %d", info.Mode(), info.Size())
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.PublicKey(), key.PublicKey()) {
		t.Fatal("loaded key differs")
	}
	sdkKey := loaded.SDK()
	if !bytes.Equal(sdkKey.PubKey().Address(), loaded.Address()) || !bytes.Equal(loaded.Address(), evidence.AuditorAddress(loaded.PublicKey())) {
		t.Fatalf("address %x, sdk %x", loaded.Address(), sdkKey.PubKey().Address())
	}
	msg := []byte("SVN-EVIDENCE-ATTESTATION-1\x00{}")
	if !ed25519.Verify(loaded.PublicKey(), msg, loaded.Sign(msg)) {
		t.Fatal("signature does not verify")
	}
	sdkSig, err := sdkKey.Sign(msg)
	if err != nil || !bytes.Equal(sdkSig, loaded.Sign(msg)) {
		t.Fatalf("sdk signature differs: %v", err)
	}
	if _, err := Generate(path); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	again, err := Load(path)
	if err != nil || !bytes.Equal(again.PublicKey(), key.PublicKey()) {
		t.Fatalf("file changed after a refused overwrite: %v", err)
	}
}

// The key file of `svnd devnet init` loads, and the address is the SDK's.
func TestLoadsDevnetKeyFormat(t *testing.T) {
	sdkKey := sdked.GenPrivKeyFromSecret([]byte("chaintest devnet-format key"))
	path := filepath.Join(t.TempDir(), "auditor-1.hex")
	writeDevnetKey(t, path, sdkKey, 0o600)
	key, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key.Address(), sdkKey.PubKey().Address()) || !bytes.Equal(key.PublicKey(), sdkKey.PubKey().Bytes()) {
		t.Fatal("address or public key differs from the SDK key")
	}
}

// The address is the chain's AuditorAddress for every key: the key binding
// x/evidence checks (step 7, D-2).
func TestAddressIsTheChainAuditorAddress(t *testing.T) {
	for _, label := range []string{"auditor-1", "auditor-2", "seat-1"} {
		key, err := FromSeed(testSeed(label))
		if err != nil {
			t.Fatal(err)
		}
		pub := ed25519.NewKeyFromSeed(testSeed(label)).Public().(ed25519.PublicKey)
		sum := sha256.Sum256(pub)
		if !bytes.Equal(key.Address(), sum[:20]) || !bytes.Equal(key.Address(), evidence.AuditorAddress(pub)) {
			t.Fatalf("%s: address %x", label, key.Address())
		}
	}
	if _, err := FromSeed(make([]byte, 31)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short seed: %v", err)
	}
}

func TestLoadRefuses(t *testing.T) {
	dir := t.TempDir()
	good := sdked.GenPrivKeyFromSecret([]byte("refusal cases"))
	text := hex.EncodeToString(good.Bytes())
	write := func(name, content string, mode os.FileMode) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	other := sdked.GenPrivKeyFromSecret([]byte("another key"))
	mismatch := text[:64] + hex.EncodeToString(other.PubKey().Bytes())
	link := filepath.Join(dir, "link.hex")
	if err := os.Symlink(write("target.hex", text+"\n", 0o600), link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo.hex")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		want       error
	}{
		{"group readable", write("g.hex", text+"\n", 0o640), ErrUnsafe},
		{"other readable", write("o.hex", text+"\n", 0o604), ErrUnsafe},
		{"group writable", write("gw.hex", text+"\n", 0o620), ErrUnsafe},
		{"symlink", link, ErrUnsafe},
		{"fifo", fifo, ErrUnsafe},
		{"directory", t.TempDir(), ErrUnsafe},
		{"no newline", write("nl.hex", text, 0o600), ErrInvalid},
		{"two newlines", write("nl2.hex", text+"\n\n", 0o600), ErrInvalid},
		{"upper case", write("uc.hex", strings.ToUpper(text)+"\n", 0o600), ErrInvalid},
		{"seed only", write("seed.hex", text[:64]+"\n", 0o600), ErrInvalid},
		{"not hex", write("nh.hex", strings.Repeat("zz", 64)+"\n", 0o600), ErrInvalid},
		{"public half of another key", write("mm.hex", mismatch+"\n", 0o600), ErrInvalid},
		{"empty", write("e.hex", "", 0o600), ErrInvalid},
	}
	for _, c := range cases {
		_, err := Load(c.path)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
		// No error text carries file contents.
		if strings.Contains(err.Error(), text[:16]) || strings.Contains(err.Error(), text[64:80]) {
			t.Fatalf("%s: error text carries key bytes: %v", c.name, err)
		}
	}
	if _, err := Load(filepath.Join(dir, "missing.hex")); err == nil {
		t.Fatal("missing file loaded")
	}
}

// Generate refuses a symlink at the path and leaves its target alone.
func TestGenerateDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "key.hex")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(link); err == nil {
		t.Fatal("generated through a symlink")
	}
	if raw, _ := os.ReadFile(target); string(raw) != "keep" {
		t.Fatal("symlink target changed")
	}
}

// Formatting a Key prints its address only, never the seed or the private
// key.
func TestNoKeyBytesInOutput(t *testing.T) {
	seed := testSeed("auditor-1")
	key, err := FromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	private := hex.EncodeToString(ed25519.NewKeyFromSeed(seed))
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%q", "%d"} {
		out := fmt.Sprintf(verb, key)
		if strings.Contains(out, private[:16]) || strings.Contains(strings.ToLower(out), hex.EncodeToString(seed)[:16]) {
			t.Fatalf("%s prints key bytes: %s", verb, out)
		}
		if !strings.Contains(out, hex.EncodeToString(key.Address())) {
			t.Fatalf("%s does not name the address: %s", verb, out)
		}
	}
	wrapped := fmt.Sprintf("%v", struct{ K Key }{key})
	if strings.Contains(wrapped, private[:16]) {
		t.Fatalf("nested formatting prints key bytes: %s", wrapped)
	}
}
