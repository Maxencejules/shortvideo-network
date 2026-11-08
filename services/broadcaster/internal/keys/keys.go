// Package keys loads the broadcaster's dev-only account key files for the
// signer process (plan 7.1, 7.2; owner answer Q1 "Both keys, separate
// signer (Recommended)").
//
// File format: the `svnd devnet init` account key format
// (chain/devnet/init.go writeKey): the lowercase hex of the SDK private key
// bytes and one newline. A secp256k1 key is 32 bytes (64 hex characters);
// an ed25519 key is 64 bytes, seed then public key (128 hex characters).
// The service and broadcaster accounts of the devnet are secp256k1; the
// ed25519 form is read so that a council seat or auditor key file given by
// mistake is refused by its on-chain role, with a message naming the
// reason, rather than by its format.
//
// Load refuses a symlink, a file that is not regular, a file with any
// group or other permission bit, a file not owned by the expected uid, a
// file that is not exactly one of the two formats, an all-zero secp256k1
// key, and an ed25519 file whose public half is not the public key of its
// seed. The file's directory must be a real directory owned by the same
// uid with no group or other permission bit (mode 0700), so another uid
// can neither list nor replace the key (plan 4.1).
//
// Key bytes are never logged, printed, or put in an error: Key formats as
// its address only. This package never signs; only internal/signer, the
// one importer allowed by TestNoSigningOutsideSigner, uses the private key.
//
// These are development keys generated at run time. No live validator,
// governance or production key is requested or handled here.
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
	"path/filepath"
	"syscall"

	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

// Algorithms of a loaded key.
const (
	Secp256k1 = "secp256k1"
	Ed25519   = "ed25519"
)

// FileMode is the mode Load expects for a key file; any group or other bit
// is refused.
const FileMode = 0o600

// DirMode is the mode Load expects for a key file's directory; any group
// or other bit is refused.
const DirMode = 0o700

// File sizes: hex characters and the newline.
const (
	secpFileBytes = 2*32 + 1
	edFileBytes   = 2*ed25519.PrivateKeySize + 1
)

// Errors. Their text never includes file contents.
var (
	ErrUnsafe  = errors.New("keys: key file or directory is not private to the signer uid")
	ErrInvalid = errors.New("keys: key file is not a devnet secp256k1 or ed25519 key in lowercase hex")
)

// Key is a loaded account key.
type Key struct {
	algorithm string
	sdk       cryptotypes.PrivKey
}

// Algorithm returns Secp256k1 or Ed25519.
func (k Key) Algorithm() string { return k.algorithm }

// PubKey returns the SDK public key.
func (k Key) PubKey() cryptotypes.PubKey { return k.sdk.PubKey() }

// Address returns the 20-byte account address the SDK derives from the
// public key.
func (k Key) Address() []byte { return append([]byte(nil), k.sdk.PubKey().Address()...) }

// SDK returns the SDK private key. Only internal/signer may call it
// (TestNoSigningOutsideSigner checks that no other package imports keys).
func (k Key) SDK() cryptotypes.PrivKey { return k.sdk }

// String names the key by its algorithm and address only.
func (k Key) String() string {
	if k.sdk == nil {
		return "account key (none)"
	}
	return fmt.Sprintf("%s account key %x", k.algorithm, k.Address())
}

// GoString names the key by its address only, so %#v prints no key bytes.
func (k Key) GoString() string { return k.String() }

// Format names the key by its address only, for every fmt verb.
func (k Key) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, k.String()) }

// Load reads the key file at path. The file and its directory must be
// owned by owner (the signer's uid).
func Load(path string, owner uint32) (Key, error) {
	if err := checkDir(filepath.Dir(path), owner); err != nil {
		return Key{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Key{}, fmt.Errorf("%w: %v", ErrUnsafe, errors.Unwrap(err))
	}
	if err := checkFile(info, owner); err != nil {
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
		return Key{}, fmt.Errorf("%w: stat failed", ErrUnsafe)
	}
	if err := checkFile(opened, owner); err != nil {
		return Key{}, err
	}
	if !os.SameFile(info, opened) {
		return Key{}, fmt.Errorf("%w: file changed while opening", ErrUnsafe)
	}
	raw, err := io.ReadAll(io.LimitReader(f, edFileBytes+1))
	if err != nil {
		return Key{}, fmt.Errorf("%w: read failed", ErrUnsafe)
	}
	defer clear(raw)
	return parse(raw)
}

// ownerOf returns the owning uid of info.
func ownerOf(info fs.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

func checkFile(info fs.FileInfo, owner uint32) error {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: key file is a symlink", ErrUnsafe)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%w: key file is not a regular file", ErrUnsafe)
	case info.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%w: key file mode %04o, want %04o", ErrUnsafe, info.Mode().Perm(), FileMode)
	}
	uid, ok := ownerOf(info)
	if !ok || uid != owner {
		return fmt.Errorf("%w: key file owner uid %d, want %d", ErrUnsafe, uid, owner)
	}
	return nil
}

func checkDir(dir string, owner uint32) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("%w: key directory: %v", ErrUnsafe, errors.Unwrap(err))
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: key directory is a symlink", ErrUnsafe)
	case !info.IsDir():
		return fmt.Errorf("%w: key directory is not a directory", ErrUnsafe)
	case info.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%w: key directory mode %04o, want %04o", ErrUnsafe, info.Mode().Perm(), DirMode)
	}
	uid, ok := ownerOf(info)
	if !ok || uid != owner {
		return fmt.Errorf("%w: key directory owner uid %d, want %d", ErrUnsafe, uid, owner)
	}
	return nil
}

// parse decodes a key file's contents (see the package comment).
func parse(raw []byte) (Key, error) {
	if (len(raw) != secpFileBytes && len(raw) != edFileBytes) || raw[len(raw)-1] != '\n' {
		return Key{}, ErrInvalid
	}
	text := raw[:len(raw)-1]
	for _, c := range text {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return Key{}, ErrInvalid
		}
	}
	decoded := make([]byte, len(text)/2)
	defer clear(decoded)
	if _, err := hex.Decode(decoded, text); err != nil {
		return Key{}, ErrInvalid
	}
	if len(decoded) == 32 {
		if bytes.Equal(decoded, make([]byte, 32)) {
			return Key{}, fmt.Errorf("%w: zero secp256k1 key", ErrInvalid)
		}
		return Key{algorithm: Secp256k1, sdk: &sdksecp.PrivKey{Key: bytes.Clone(decoded)}}, nil
	}
	full := ed25519.NewKeyFromSeed(decoded[:ed25519.SeedSize])
	if !bytes.Equal(full[ed25519.SeedSize:], decoded[ed25519.SeedSize:]) {
		clear(full)
		return Key{}, fmt.Errorf("%w: public half is not the seed's public key", ErrInvalid)
	}
	return Key{algorithm: Ed25519, sdk: &sdked.PrivKey{Key: full}}, nil
}
