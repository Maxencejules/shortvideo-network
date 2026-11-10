package keys

import (
	"os"
	"path/filepath"
	"testing"

	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
)

func TestLoadSecpReadsADevnetKey(t *testing.T) {
	dir := t.TempDir()
	key := sdksecp.GenPrivKey()
	path := filepath.Join(dir, OuterName)
	if err := os.WriteFile(path, []byte(encodeHex(key.Bytes())+"\n"), FileMode); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSecp(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Equals(key) {
		t.Fatal("loaded key differs")
	}
}

func TestLoadSecpRejectsAGroupReadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OuterName)
	body := make([]byte, 65)
	for i := 0; i < 64; i++ {
		body[i] = 'a'
	}
	body[64] = '\n'
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSecp(path); err != ErrUnsafe {
		t.Fatalf("err %v", err)
	}
}

func encodeHex(raw []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(raw)*2)
	for i, b := range raw {
		out[i*2] = digits[b>>4]
		out[i*2+1] = digits[b&0x0f]
	}
	return string(out)
}
