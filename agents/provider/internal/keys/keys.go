// Package keys creates and loads the provider's development ed25519 key.
// The file is the svnd devnet layout: lowercase hex of the 64-byte key and
// a newline, mode 0600, not a symlink. These are per-workspace test keys.
// No production, validator, or governance key is read or written.
package keys

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

const FileMode os.FileMode = 0o600

var (
	ErrUnsafe  = errors.New("keys: key file is not a private regular file")
	ErrInvalid = errors.New("keys: key file is not a 64-byte ed25519 key")
)

// Key is a loaded development key. Its formatting methods never print bytes.
type Key struct {
	private ed25519.PrivateKey
}

// Generate creates a new key. It does not write a file.
func Generate() (Key, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Key{}, err
	}
	return Key{private: priv}, nil
}

// Public returns the 32-byte public key.
func (k Key) Public() []byte {
	return append([]byte(nil), k.private.Public().(ed25519.PublicKey)...)
}

// Sign signs msg with ed25519.
func (k Key) Sign(msg []byte) []byte { return ed25519.Sign(k.private, msg) }

// Private returns a copy of the 64-byte key for the SDK signer.
func (k Key) Private() ed25519.PrivateKey {
	return append(ed25519.PrivateKey(nil), k.private...)
}

// SDK returns the key as a Cosmos SDK private key. The address is the
// SDK ed25519 address. The caller configures the svn bech32 prefix first.
func (k Key) SDK() cryptotypes.PrivKey {
	return &sdked.PrivKey{Key: k.Private()}
}

// Address is the 20-byte SDK account address.
func (k Key) Address() []byte { return k.SDK().PubKey().Address() }

func (k Key) String() string             { return fmt.Sprintf("provider key %x", k.Address()) }
func (k Key) GoString() string           { return k.String() }
func (k Key) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, k.String()) }

// Write creates path. It refuses an existing file.
func Write(path string, k Key) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("keys: %s exists", path)
	}
	body := hex.EncodeToString(k.private) + "\n"
	return writeNew(path, []byte(body))
}

// Load reads a key file.
func Load(path string) (Key, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Key{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Key{}, ErrUnsafe
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return Key{}, ErrUnsafe
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Key{}, err
	}
	if len(raw) != 2*ed25519.PrivateKeySize+1 || raw[len(raw)-1] != '\n' {
		return Key{}, ErrInvalid
	}
	buf := make([]byte, ed25519.PrivateKeySize)
	if _, err := hex.Decode(buf, raw[:len(raw)-1]); err != nil {
		return Key{}, ErrInvalid
	}
	priv := ed25519.NewKeyFromSeed(buf[:ed25519.SeedSize])
	if !bytes.Equal(priv, buf) {
		return Key{}, ErrInvalid
	}
	return Key{private: priv}, nil
}

// OuterName is the optional secp256k1 account key file under keys/.
// A development run may place a devnet account key there so the outer
// transaction signer is that account. The ed25519 file remains the
// inner signing key. The file is not a production key.
const OuterName = "outer.hex"

// LoadSecp reads a 32-byte secp256k1 key in the devnet hex layout.
func LoadSecp(path string) (cryptotypes.PrivKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, ErrUnsafe
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) != 64+1 || raw[len(raw)-1] != '\n' {
		return nil, ErrInvalid
	}
	buf := make([]byte, 32)
	if _, err := hex.Decode(buf, raw[:len(raw)-1]); err != nil {
		return nil, ErrInvalid
	}
	key := &sdksecp.PrivKey{Key: buf}
	if len(key.PubKey().Bytes()) == 0 {
		return nil, ErrInvalid
	}
	return key, nil
}

func writeNew(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode)
	if err != nil {
		return err
	}
	_, werr := f.Write(body)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
