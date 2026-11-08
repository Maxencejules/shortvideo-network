package keys

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

// self is the test process's effective uid, the owner every Load call
// below expects unless a case says otherwise.
var self = uint32(os.Geteuid())

// keyDir returns a fresh 0700 directory.
func keyDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "keys")
	if err := os.Mkdir(dir, DirMode); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeKeyFile writes key in the devnet format (chain/devnet/init.go
// writeKey) with mode.
func writeKeyFile(t *testing.T, dir, name string, key cryptotypes.PrivKey, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key.Bytes())+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDevnetFormats(t *testing.T) {
	dir := keyDir(t)
	secp := sdksecp.GenPrivKey()
	ed := sdked.GenPrivKey()
	for _, tc := range []struct {
		name, algorithm string
		key             cryptotypes.PrivKey
	}{{"service.hex", Secp256k1, secp}, {"seat.hex", Ed25519, ed}} {
		key, err := Load(writeKeyFile(t, dir, tc.name, tc.key, FileMode), self)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if key.Algorithm() != tc.algorithm || !bytes.Equal(key.Address(), tc.key.PubKey().Address()) ||
			!key.PubKey().Equals(tc.key.PubKey()) || !key.SDK().Equals(tc.key) {
			t.Fatalf("%s: loaded key differs from the written one", tc.name)
		}
		secretHex := hex.EncodeToString(tc.key.Bytes())
		for _, out := range []string{key.String(), fmt.Sprintf("%v %+v %#v %s %q %x", key, key, key, key, key, key)} {
			if strings.Contains(out, secretHex) || strings.Contains(out, secretHex[:16]) {
				t.Fatalf("%s: formatting printed key bytes: %s", tc.name, out)
			}
			if !strings.Contains(out, hex.EncodeToString(tc.key.PubKey().Address())) {
				t.Fatalf("%s: formatting does not name the address: %s", tc.name, out)
			}
		}
	}
}

// TestKeyFileRules checks every refusal of Load (plan 7.1: the auditor
// loader rules plus the owner and directory checks).
func TestKeyFileRules(t *testing.T) {
	good := sdksecp.GenPrivKey()
	goodHex := hex.EncodeToString(good.Bytes())

	content := map[string]string{
		"uppercase hex":     strings.ToUpper(goodHex) + "\n",
		"no newline":        goodHex,
		"two newlines":      goodHex + "\n\n",
		"leading space":     " " + goodHex[1:] + "\n",
		"short":             goodHex[:62] + "\n",
		"between lengths":   goodHex + goodHex[:32] + "\n",
		"too long":          goodHex + goodHex + "00\n",
		"zero secp256k1":    strings.Repeat("0", 64) + "\n",
		"empty":             "",
		"ed25519 bad half":  hex.EncodeToString(append(bytes.Repeat([]byte{7}, 32), bytes.Repeat([]byte{9}, 32)...)) + "\n",
		"crlf":              goodHex + "\r\n",
		"0x prefix":         "0x" + goodHex[2:] + "\n",
		"non hex character": goodHex[:63] + "g\n",
	}
	for name, text := range content {
		t.Run("format/"+name, func(t *testing.T) {
			dir := keyDir(t)
			path := filepath.Join(dir, "k.hex")
			if err := os.WriteFile(path, []byte(text), FileMode); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path, self); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Load = %v, want ErrInvalid", err)
			}
		})
	}

	for _, mode := range []os.FileMode{0o640, 0o604, 0o620, 0o602, 0o601, 0o610, 0o644, 0o666} {
		t.Run(fmt.Sprintf("file mode %04o", mode), func(t *testing.T) {
			path := writeKeyFile(t, keyDir(t), "k.hex", good, mode)
			if _, err := Load(path, self); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("Load = %v, want ErrUnsafe", err)
			}
		})
	}
	for _, mode := range []os.FileMode{0o710, 0o701, 0o750, 0o755, 0o777} {
		t.Run(fmt.Sprintf("directory mode %04o", mode), func(t *testing.T) {
			dir := keyDir(t)
			path := writeKeyFile(t, dir, "k.hex", good, FileMode)
			if err := os.Chmod(dir, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path, self); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("Load = %v, want ErrUnsafe", err)
			}
		})
	}

	t.Run("symlink file", func(t *testing.T) {
		dir := keyDir(t)
		target := writeKeyFile(t, dir, "real.hex", good, FileMode)
		link := filepath.Join(dir, "link.hex")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(link, self); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
	t.Run("symlink directory", func(t *testing.T) {
		dir := keyDir(t)
		writeKeyFile(t, dir, "k.hex", good, FileMode)
		link := filepath.Join(filepath.Dir(dir), "linkdir")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(filepath.Join(link, "k.hex"), self); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
	t.Run("directory not a file", func(t *testing.T) {
		dir := keyDir(t)
		sub := filepath.Join(dir, "k.hex")
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(sub, self); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
	t.Run("fifo", func(t *testing.T) {
		dir := keyDir(t)
		fifo := filepath.Join(dir, "k.hex")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(fifo, self); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if _, err := Load(filepath.Join(keyDir(t), "absent.hex"), self); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
	t.Run("wrong expected owner", func(t *testing.T) {
		path := writeKeyFile(t, keyDir(t), "k.hex", good, FileMode)
		if _, err := Load(path, self+1); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
}

func TestErrorsCarryNoKeyBytes(t *testing.T) {
	good := sdksecp.GenPrivKey()
	goodHex := hex.EncodeToString(good.Bytes())
	dir := keyDir(t)
	path := filepath.Join(dir, "k.hex")
	if err := os.WriteFile(path, []byte(goodHex+"\n\n"), FileMode); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, self)
	if err == nil || strings.Contains(err.Error(), goodHex[:16]) {
		t.Fatalf("Load error %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, self); err == nil || strings.Contains(err.Error(), goodHex[:16]) {
		t.Fatalf("Load error %v", err)
	}
}
