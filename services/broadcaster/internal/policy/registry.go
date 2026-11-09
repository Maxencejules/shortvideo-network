package policy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unicode"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// Intent-key registry bounds.
const (
	// MaxRegistryBytes bounds the registry file.
	MaxRegistryBytes = 1 << 20
	// MaxRegistryEntries bounds the registered principals.
	MaxRegistryEntries = 4096
	// MaxLabelBytes bounds an entry's label.
	MaxLabelBytes = 64
)

// Boundary is how the signer and serve processes are separated (plan 4.1,
// DR-11-35).
type Boundary int

// Boundaries.
const (
	// DistinctUIDs: signer and serve run under two distinct non-root uids
	// (the launcher was root). The only mode that gives a boundary.
	DistinctUIDs Boundary = iota + 1
	// SameUID: both run under one uid (the launcher was not root). The
	// process split then separates key bytes in memory only; a run in
	// this mode does not verify the boundary.
	SameUID
)

// String names the boundary as the devnet test prints it.
func (b Boundary) String() string {
	switch b {
	case DistinctUIDs:
		return "distinct"
	case SameUID:
		return "same"
	}
	return "unknown"
}

// registryFile is the on-disk shape of intent-keys.json.
type registryFile struct {
	Entries []registryEntry `json:"entries"`
}

type registryEntry struct {
	PrincipalID  string `json:"principal_id"`
	PublicKeyHex string `json:"public_key_hex"`
	Label        string `json:"label"`
}

// Registry is the operator's intent-key registry (plan 7.4, owner answer
// Q3 "Operator-registered intent keys (Recommended)"): each principal id
// has one ed25519 public key that signs that principal's hosting intents.
// It holds public keys only. The operator writes it; serve and the signer
// only read it, at start, and a change takes a restart.
type Registry struct {
	byPrincipal map[string][]byte
	byKey       map[string]string
}

// ErrRegistryWritable is a registry file or directory that the reading
// process could change.
var ErrRegistryWritable = errors.New("policy: intent-key registry is writable by this process")

// ErrRegistryServeWritable is a registry file or directory that the serve
// uid owns or that a group or other uid could write (CheckRegistryForServe).
var ErrRegistryServeWritable = errors.New("policy: intent-key registry could be changed by the serve uid")

// LoadRegistry reads the registry at path. Under DistinctUIDs the process
// must not be root, and neither the file nor its directory may be writable
// by this process: not owned by its uid (an owner can change the mode),
// not group-writable for one of its groups, not writable by others. Under
// SameUID the operator, serve and the signer are one uid, so ownership
// cannot be refused; the file must then have no write bit at all and the
// directory no group or other write bit (a labelled fallback that keeps
// accidental edits out, not a boundary).
//
// The file is JSON: {"entries":[{"principal_id","public_key_hex","label"}]}
// with unknown fields refused, principal ids in the protocol id pattern,
// 32-byte lowercase-hex keys, labels of printable text up to MaxLabelBytes,
// and no principal or key listed twice.
func LoadRegistry(path string, b Boundary) (*Registry, error) {
	if b != DistinctUIDs && b != SameUID {
		return nil, errors.New("policy: unknown process boundary")
	}
	euid, egid := os.Geteuid(), os.Getegid()
	groups, err := os.Getgroups()
	if err != nil {
		return nil, fmt.Errorf("policy: read process groups: %w", err)
	}
	if b == DistinctUIDs && euid == 0 {
		return nil, fmt.Errorf("%w: root can write any file", ErrRegistryWritable)
	}
	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("policy: registry directory: %w", err)
	}
	if dirInfo.Mode()&fs.ModeSymlink != 0 || !dirInfo.IsDir() {
		return nil, errors.New("policy: registry directory is not a real directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("policy: registry file: %w", err)
	}
	if err := registryModeOK(info, dirInfo, b, uint32(euid), uint32(egid), groups); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("policy: open registry: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("policy: stat registry: %w", err)
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("policy: registry file changed while opening")
	}
	if err := registryModeOK(opened, dirInfo, b, uint32(euid), uint32(egid), groups); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxRegistryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("policy: read registry: %w", err)
	}
	if len(raw) > MaxRegistryBytes {
		return nil, fmt.Errorf("policy: registry above %d bytes", MaxRegistryBytes)
	}
	return ParseRegistry(raw)
}

// CheckRegistryForServe applies the distinct-uid rule of plan 7.4 for the
// signer to the serve uid: neither the registry file at path nor its
// directory may be owned by serveUID or have a group or other write bit.
// LoadRegistry checks only the reading process's own uid; serve and the
// signer have separate configuration files, so the registry the signer
// reads could otherwise be one that serve can write, and a compromised
// serve could add its own intent key before the signer's next start. The
// signer does not know serve's groups, so any group write bit is refused.
func CheckRegistryForServe(path string, serveUID uint32) error {
	for _, p := range []string{path, filepath.Dir(path)} {
		info, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("policy: registry: %w", err)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			return errors.New("policy: registry path is a symbolic link")
		case !ok:
			return errors.New("policy: registry owner is unknown")
		case st.Uid == serveUID:
			return fmt.Errorf("%w: %s is owned by the serve uid", ErrRegistryServeWritable, info.Name())
		case info.Mode().Perm()&0o022 != 0:
			return fmt.Errorf("%w: %s mode %04o has a group or other write bit", ErrRegistryServeWritable, info.Name(), info.Mode().Perm())
		}
	}
	return nil
}

// registryModeOK applies the writability rule of LoadRegistry to the file
// and its directory.
func registryModeOK(file, dir fs.FileInfo, b Boundary, uid, gid uint32, groups []int) error {
	if file.Mode()&fs.ModeSymlink != 0 || !file.Mode().IsRegular() {
		return errors.New("policy: registry is not a regular file")
	}
	if b == SameUID {
		if file.Mode().Perm()&0o222 != 0 {
			return fmt.Errorf("%w: same-uid fallback needs a file without write bits, mode %04o", ErrRegistryWritable, file.Mode().Perm())
		}
		if dir.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%w: directory mode %04o has a group or other write bit", ErrRegistryWritable, dir.Mode().Perm())
		}
		return nil
	}
	for _, info := range []fs.FileInfo{file, dir} {
		if WritableBy(info, uid, gid, groups) {
			return fmt.Errorf("%w: %s mode %04o", ErrRegistryWritable, info.Name(), info.Mode().Perm())
		}
	}
	return nil
}

// WritableBy reports whether a process with uid, primary gid and
// supplementary groups could write to the file or directory info, or
// change its mode: uid 0, the owner, a group write bit for one of its
// groups, or the other write bit.
func WritableBy(info fs.FileInfo, uid, gid uint32, groups []int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	perm := info.Mode().Perm()
	if uid == 0 || st.Uid == uid || perm&0o002 != 0 {
		return true
	}
	if perm&0o020 != 0 {
		if st.Gid == gid {
			return true
		}
		for _, g := range groups {
			if g >= 0 && uint32(g) == st.Gid {
				return true
			}
		}
	}
	return false
}

// ParseRegistry decodes and validates registry file contents (see
// LoadRegistry).
func ParseRegistry(raw []byte) (*Registry, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var file registryFile
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("policy: registry is not the registry JSON shape: %w", err)
	}
	if dec.More() {
		return nil, errors.New("policy: registry has data after its JSON document")
	}
	if len(file.Entries) > MaxRegistryEntries {
		return nil, fmt.Errorf("policy: registry has more than %d entries", MaxRegistryEntries)
	}
	reg := &Registry{byPrincipal: map[string][]byte{}, byKey: map[string]string{}}
	for i, e := range file.Entries {
		if !msgkit.ValidID(e.PrincipalID) {
			return nil, fmt.Errorf("policy: registry entry %d: principal id outside the id pattern", i)
		}
		if len(e.PublicKeyHex) != 2*msgkit.PublicKeyBytes || hex.EncodeToString(mustHex(e.PublicKeyHex)) != e.PublicKeyHex {
			return nil, fmt.Errorf("policy: registry entry %d: public key is not 32 bytes of lowercase hex", i)
		}
		if len(e.Label) > MaxLabelBytes || !printable(e.Label) {
			return nil, fmt.Errorf("policy: registry entry %d: label is not printable text of at most %d bytes", i, MaxLabelBytes)
		}
		if _, dup := reg.byPrincipal[e.PrincipalID]; dup {
			return nil, fmt.Errorf("policy: registry entry %d: principal listed twice", i)
		}
		if _, dup := reg.byKey[e.PublicKeyHex]; dup {
			return nil, fmt.Errorf("policy: registry entry %d: public key registered for two principals", i)
		}
		reg.byPrincipal[e.PrincipalID] = mustHex(e.PublicKeyHex)
		reg.byKey[e.PublicKeyHex] = e.PrincipalID
	}
	return reg, nil
}

// mustHex decodes s, or returns nil when s is not hex.
func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

func printable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// KeyFor returns the public key registered for principal.
func (r *Registry) KeyFor(principal string) ([]byte, bool) {
	k, ok := r.byPrincipal[principal]
	return bytes.Clone(k), ok
}

// Contains reports whether pub is registered for any principal.
func (r *Registry) Contains(pub []byte) bool {
	_, ok := r.byKey[hex.EncodeToString(pub)]
	return ok && len(pub) == msgkit.PublicKeyBytes
}

// Len returns the number of registered principals.
func (r *Registry) Len() int { return len(r.byPrincipal) }
