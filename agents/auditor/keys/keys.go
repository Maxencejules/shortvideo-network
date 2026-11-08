// Package keys loads and creates the auditor's dev-only ed25519 key file
// (decision request DR-09-25).
//
// Under the owner's ruling on Q1 ("Account key signs") one key does two
// jobs: it signs the auditor's SDK transactions (the account key) and its
// SVN-EVIDENCE-ATTESTATION-1 batches (the attestation key). The chain
// requires the attestation public key to derive the outer signer's address
// (x/evidence step 7, deviation D-2), so Address here is
// evidence.AuditorAddress of the public key, which is also the SDK ed25519
// address. There is no rotation: the address is the key.
//
// File format: the `svnd devnet init` key format, the lowercase hex of the
// 64-byte SDK ed25519 private key (seed, then public key) and one newline.
// Load refuses a symlink, a file that is not regular, a file readable or
// writable by group or others, a file that is not exactly that format, and a
// file whose public half is not the public key of its seed. Key bytes are
// never logged, printed, or put in an error, report, envelope, or index:
// Key formats as its address only.
//
// These are development keys. No live validator, governance, or production
// key is requested or handled here.
package keys

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"

	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
)

// FileMode is the only mode a key file may have.
const FileMode = 0o600

// fileBytes is the size of a key file: 128 hex characters and a newline.
const fileBytes = 2*ed25519.PrivateKeySize + 1

// Errors. Their text never includes file contents.
var (
	ErrUnsafe  = errors.New("keys: key file is not a private regular file")
	ErrInvalid = errors.New("keys: key file is not a 64-byte ed25519 key in hex")
	ErrExists  = errors.New("keys: key file already exists")
)

// Key is a loaded auditor key.
type Key struct {
	private ed25519.PrivateKey
}

// PublicKey returns the 32-byte ed25519 public key.
func (k Key) PublicKey() []byte {
	return append([]byte(nil), k.private.Public().(ed25519.PublicKey)...)
}

// Address returns the 20-byte account address of the key:
// evidence.AuditorAddress(PublicKey()).
func (k Key) Address() []byte { return evidence.AuditorAddress(k.PublicKey()) }

// Sign signs msg with ed25519.
func (k Key) Sign(msg []byte) []byte { return ed25519.Sign(k.private, msg) }

// SDK returns the key as an SDK private key, for the outer transaction
// signature.
func (k Key) SDK() cryptotypes.PrivKey {
	return &sdked.PrivKey{Key: append([]byte(nil), k.private...)}
}

// String names the key by its address only.
func (k Key) String() string { return fmt.Sprintf("auditor key %x", k.Address()) }

// GoString names the key by its address only, so %#v prints no key bytes.
func (k Key) GoString() string { return k.String() }

// Format names the key by its address only, for every fmt verb.
func (k Key) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, k.String()) }

// FromSeed builds a key from a 32-byte seed. Tests use it with seeds
// derived from labels; it reads no file.
func FromSeed(seed []byte) (Key, error) {
	if len(seed) != ed25519.SeedSize {
		return Key{}, ErrInvalid
	}
	return Key{private: ed25519.NewKeyFromSeed(seed)}, nil
}

// Load reads a key file.
func Load(path string) (Key, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Key{}, fmt.Errorf("keys: %w", err)
	}
	if err := checkMode(info); err != nil {
		return Key{}, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Key{}, fmt.Errorf("%w: open failed", ErrUnsafe)
	}
	defer f.Close()
	// The file opened may not be the one Lstat saw; check it again.
	opened, err := f.Stat()
	if err != nil {
		return Key{}, fmt.Errorf("keys: %w", err)
	}
	if err := checkMode(opened); err != nil {
		return Key{}, err
	}
	if !os.SameFile(info, opened) {
		return Key{}, fmt.Errorf("%w: file changed while opening", ErrUnsafe)
	}
	raw, err := io.ReadAll(io.LimitReader(f, fileBytes+1))
	if err != nil {
		return Key{}, fmt.Errorf("keys: read failed")
	}
	defer clear(raw)
	return parse(raw)
}

func checkMode(info fs.FileInfo) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: symlink", ErrUnsafe)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", ErrUnsafe)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: mode %04o, want %04o", ErrUnsafe, info.Mode().Perm(), FileMode)
	}
	return nil
}

func parse(raw []byte) (Key, error) {
	if len(raw) != fileBytes || raw[fileBytes-1] != '\n' {
		return Key{}, ErrInvalid
	}
	text := raw[:fileBytes-1]
	for _, c := range text {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return Key{}, ErrInvalid
		}
	}
	decoded := make([]byte, ed25519.PrivateKeySize)
	defer clear(decoded)
	if _, err := hex.Decode(decoded, text); err != nil {
		return Key{}, ErrInvalid
	}
	key := ed25519.NewKeyFromSeed(decoded[:ed25519.SeedSize])
	if !bytes.Equal(key[ed25519.SeedSize:], decoded[ed25519.SeedSize:]) {
		clear(key)
		return Key{}, fmt.Errorf("%w: public half is not the seed's public key", ErrInvalid)
	}
	return Key{private: key}, nil
}

// Generate writes a fresh key from the OS random source to path with mode
// 0600 and returns it. It never replaces an existing file (O_EXCL), and it
// does not follow a symlink at path.
func Generate(path string) (Key, error) {
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		return Key{}, fmt.Errorf("keys: random source: %w", err)
	}
	key := Key{private: private}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, FileMode)
	if errors.Is(err, fs.ErrExist) {
		return Key{}, ErrExists
	}
	if err != nil {
		return Key{}, fmt.Errorf("keys: create failed: %w", err)
	}
	text := make([]byte, fileBytes)
	defer clear(text)
	hex.Encode(text, private)
	text[fileBytes-1] = '\n'
	_, werr := f.Write(text)
	cerr := f.Chmod(FileMode)
	serr := f.Sync()
	xerr := f.Close()
	if err := errors.Join(werr, cerr, serr, xerr); err != nil {
		_ = os.Remove(path)
		return Key{}, fmt.Errorf("keys: write failed")
	}
	return key, nil
}
