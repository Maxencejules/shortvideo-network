// Package secretfile reads small files that hold a secret: the database
// password file and the TLS server key. A file is read only when no other
// uid could have read or replaced it: it is a regular file reached without
// a symlink, owned by the reading process's effective uid, with no group or
// other permission bit, in a directory that is not a symlink. The checks
// run on the path and again on the opened descriptor, so a file swapped
// between the two is refused.
package secretfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// MaxBytes is the largest file Read accepts.
const MaxBytes = 64 << 10

// Errors. Their text names the rule, never the file's contents.
var (
	ErrPath     = errors.New("secretfile: path must be a clean absolute path")
	ErrNotFile  = errors.New("secretfile: not a regular file reached without a symlink")
	ErrOwner    = errors.New("secretfile: file is not owned by this process's effective uid")
	ErrMode     = errors.New("secretfile: file has a group or other permission bit")
	ErrSize     = errors.New("secretfile: file is empty or larger than the bound")
	ErrSwapped  = errors.New("secretfile: file changed while it was opened")
	ErrParent   = errors.New("secretfile: parent directory is a symlink or not a directory")
	ErrMaxBytes = errors.New("secretfile: maximum size outside [1, MaxBytes]")
)

// Read returns the contents of the secret file at path, at most maxBytes
// (1 to MaxBytes) bytes.
func Read(path string, maxBytes int) ([]byte, error) {
	if maxBytes < 1 || maxBytes > MaxBytes {
		return nil, ErrMaxBytes
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrPath
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("secretfile: parent directory: %w", err)
	}
	if parent.Mode()&fs.ModeSymlink != 0 || !parent.IsDir() {
		return nil, ErrParent
	}
	euid := uint32(os.Geteuid())
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("secretfile: stat: %w", err)
	}
	if err := check(before, euid); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("secretfile: open: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("secretfile: stat opened file: %w", err)
	}
	if !os.SameFile(before, opened) {
		return nil, ErrSwapped
	}
	if err := check(opened, euid); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("secretfile: read: %w", err)
	}
	if len(data) == 0 || len(data) > maxBytes {
		return nil, ErrSize
	}
	return data, nil
}

// check applies the ownership and mode rules to info.
func check(info fs.FileInfo, euid uint32) error {
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrNotFile
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != euid {
		return ErrOwner
	}
	if info.Mode().Perm()&0o077 != 0 {
		return ErrMode
	}
	return nil
}
