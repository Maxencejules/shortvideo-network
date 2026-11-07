// Package store is the producer's off-chain report store (interim under
// decision request DR-09-23). Layout under the root:
//
//	reports/<full sha256 hex>.json   content-addressed reports
//	envelopes/<name>.json            named objects
//	batches/<name>.pb, <name>.json   named objects
//	pending/<name>.json              named objects
//	index.jsonl                      append-only, one JSON object per line
//
// Writes go to a temporary file in the same directory, are synced, and are
// then hard-linked to the final name, which fails if the name exists: a
// reader never sees a partial file, and a name is written once. Writing the
// same bytes again is a no-op; different bytes under an existing name are
// ErrConflict. Directories are 0700 and files 0600; Open refuses a root or
// subdirectory that is a symlink or is accessible by group or others.
//
// Retention: none set (approved DEC-004 default); nothing is deleted. The
// store has no delete operation. Temporary files left by a crash keep their
// ".tmp-" prefix and are never read as objects.
//
// Which names envelopes, batches, and pending runs get is decided by their
// writers, not here; names must match the protocol id pattern.
package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"syscall"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// Bounds and modes.
const (
	// MaxObjectBytes bounds every stored object.
	MaxObjectBytes = 4 << 20
	// MaxIndexLineBytes bounds one index line, without its newline.
	MaxIndexLineBytes = 4096
	dirMode           = 0o700
	fileMode          = 0o600
	tempPrefix        = ".tmp-"
	reportsDir        = "reports"
	indexFile         = "index.jsonl"
)

// Collection is a directory of named objects.
type Collection string

// Collections of named objects.
const (
	Envelopes Collection = "envelopes"
	Batches   Collection = "batches"
	Pending   Collection = "pending"
)

var extensions = map[Collection][]string{
	Envelopes: {".json"},
	Batches:   {".pb", ".json"},
	Pending:   {".json"},
}

// Errors.
var (
	ErrConflict = errors.New("store: different bytes are already stored under this name")
	ErrNotFound = errors.New("store: not found")
	ErrCorrupt  = errors.New("store: stored bytes do not match")
	ErrUnsafe   = errors.New("store: unsafe path or permissions")
	ErrInvalid  = errors.New("store: invalid name or data")
)

var namePattern = regexp.MustCompile(genesis.IDPattern)

// Store is an opened store root.
type Store struct {
	root string
}

// Open creates the root and its subdirectories when missing (mode 0700)
// and checks that each is a real directory with no group or other access.
func Open(root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: empty root", ErrInvalid)
	}
	if err := os.MkdirAll(root, dirMode); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := checkDir(root); err != nil {
		return nil, err
	}
	for _, sub := range []string{reportsDir, string(Envelopes), string(Batches), string(Pending)} {
		path := filepath.Join(root, sub)
		if err := os.Mkdir(path, dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("store: %w", err)
		}
		if err := checkDir(path); err != nil {
			return nil, err
		}
	}
	return &Store{root: root}, nil
}

func checkDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s must be a directory with mode 0700", ErrUnsafe, filepath.Base(path))
	}
	return nil
}

// PutReport stores a report under the full SHA-256 of its bytes and returns
// the digest.
func (s *Store) PutReport(data []byte) ([32]byte, error) {
	sum := sha256.Sum256(data)
	return sum, s.put(filepath.Join(s.root, reportsDir), hex.EncodeToString(sum[:])+".json", data)
}

// Report reads the report with this digest and checks that its bytes still
// hash to it.
func (s *Store) Report(sum [32]byte) ([]byte, error) {
	data, err := readObject(filepath.Join(s.root, reportsDir, hex.EncodeToString(sum[:])+".json"))
	if err != nil {
		return nil, err
	}
	if sha256.Sum256(data) != sum {
		return nil, ErrCorrupt
	}
	return data, nil
}

func objectPath(root string, c Collection, name, ext string) (string, error) {
	allowed, ok := extensions[c]
	if !ok || !slices.Contains(allowed, ext) {
		return "", fmt.Errorf("%w: collection or extension", ErrInvalid)
	}
	if !namePattern.MatchString(name) {
		return "", fmt.Errorf("%w: name must match %s", ErrInvalid, genesis.IDPattern)
	}
	return filepath.Join(root, string(c), name+ext), nil
}

// Put stores data under name in a collection.
func (s *Store) Put(c Collection, name, ext string, data []byte) error {
	path, err := objectPath(s.root, c, name, ext)
	if err != nil {
		return err
	}
	return s.put(filepath.Dir(path), filepath.Base(path), data)
}

// Get reads a named object.
func (s *Store) Get(c Collection, name, ext string) ([]byte, error) {
	path, err := objectPath(s.root, c, name, ext)
	if err != nil {
		return nil, err
	}
	return readObject(path)
}

// readObject reads a regular file of at most MaxObjectBytes. A symlink or
// any other file type is ErrUnsafe.
func readObject(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafe, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrUnsafe)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxObjectBytes+1))
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if len(data) > MaxObjectBytes {
		return nil, fmt.Errorf("%w: object above %d bytes", ErrCorrupt, MaxObjectBytes)
	}
	return data, nil
}

func (s *Store) put(dir, name string, data []byte) error {
	if len(data) == 0 || len(data) > MaxObjectBytes {
		return fmt.Errorf("%w: object must be 1 to %d bytes", ErrInvalid, MaxObjectBytes)
	}
	final := filepath.Join(dir, name)
	if exists, err := checkExisting(final, data); exists || err != nil {
		return err
	}
	tmp, err := writeTemp(dir, data)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return publish(dir, tmp, final, data)
}

// publish hard-links the synced temporary file tmp, holding data, to final.
// The link fails when final exists, so a name is never replaced: when
// another writer linked it after put's existence check, its bytes decide
// between a no-op (the same bytes) and ErrConflict.
func publish(dir, tmp, final string, data []byte) error {
	if err := os.Link(tmp, final); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("store: %w", err)
		}
		// Another writer linked the name first.
		exists, err := checkExisting(final, data)
		if !exists && err == nil {
			return fmt.Errorf("%w: name exists but cannot be read", ErrUnsafe)
		}
		return err
	}
	return syncDir(dir)
}

// checkExisting reports whether final exists. When it exists, err is nil
// for the same bytes and ErrConflict for other bytes.
func checkExisting(final string, data []byte) (exists bool, err error) {
	existing, err := readObject(final)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if !bytes.Equal(existing, data) {
		return true, ErrConflict
	}
	return true, nil
}

// writeTemp writes data to a new 0600 temporary file in dir and syncs it.
func writeTemp(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("store: %w", err)
	}
	name := f.Name()
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("store: %w", err)
	}
	return name, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// AppendIndex appends one line to index.jsonl and syncs it. The line must
// be a single JSON object of at most MaxIndexLineBytes bytes. An index whose
// last byte is not a newline ends in a torn append: AppendIndex returns
// ErrCorrupt and writes nothing, so the torn bytes are never joined to a
// new line.
func (s *Store) AppendIndex(line []byte) error {
	if !validIndexLine(line) {
		return fmt.Errorf("%w: index line must be one JSON object of at most %d bytes", ErrInvalid, MaxIndexLineBytes)
	}
	f, err := os.OpenFile(filepath.Join(s.root, indexFile), os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, fileMode)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafe, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: index must be a regular file with mode 0600", ErrUnsafe)
	}
	if size := info.Size(); size > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], size-1); err != nil {
			return fmt.Errorf("store: %w", err)
		}
		if last[0] != '\n' {
			return fmt.Errorf("%w: index ends in a torn line", ErrCorrupt)
		}
	}
	if _, err := f.Write(append(append([]byte(nil), line...), '\n')); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// validIndexLine reports whether line is one JSON object of at most
// MaxIndexLineBytes bytes without a line break.
func validIndexLine(line []byte) bool {
	return len(line) != 0 && len(line) <= MaxIndexLineBytes && line[0] == '{' && !bytes.ContainsAny(line, "\r\n") && json.Valid(line)
}

// ScanIndex calls fn for each index line in order. A last line without its
// newline (a torn append), an over-long line, or a line that is not one JSON
// object is ErrCorrupt; fn is not called for it.
func (s *Store) ScanIndex(fn func(line []byte) error) error {
	f, err := os.OpenFile(filepath.Join(s.root, indexFile), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafe, err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, MaxIndexLineBytes+1)
	for {
		line, err := r.ReadSlice('\n')
		if errors.Is(err, io.EOF) {
			if len(line) != 0 {
				return fmt.Errorf("%w: torn last index line", ErrCorrupt)
			}
			return nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			return fmt.Errorf("%w: index line above %d bytes", ErrCorrupt, MaxIndexLineBytes)
		}
		if err != nil {
			return fmt.Errorf("store: %w", err)
		}
		line = line[:len(line)-1]
		if !validIndexLine(line) {
			return fmt.Errorf("%w: index line is not one JSON object", ErrCorrupt)
		}
		if err := fn(bytes.Clone(line)); err != nil {
			return err
		}
	}
}
