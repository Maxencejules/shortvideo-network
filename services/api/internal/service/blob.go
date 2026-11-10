package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

var objectRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Blob is the local quarantine directory. Names are generated hex. The directory
// is not mounted on an HTTP route.
type Blob struct {
	root string
}

// OpenBlob creates the quarantine directory at mode 0700.
func OpenBlob(root string) (*Blob, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("quarantine directory must be absolute")
	}
	dir := filepath.Join(root, "quarantine")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &Blob{root: root}, nil
}

// Path returns the file path for a generated object name.
func (b *Blob) Path(name string) (string, error) {
	if !objectRE.MatchString(name) {
		return "", errMalformed
	}
	dir := filepath.Join(b.root, "quarantine")
	full := filepath.Join(dir, name)
	if filepath.Base(full) != name || filepath.Dir(full) != dir {
		return "", errMalformed
	}
	return full, nil
}

// Remove deletes a quarantine object. A missing file is not an error.
func (b *Blob) Remove(name string) error {
	path, err := b.Path(name)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// WriteBody writes at most declared bytes. oversized is set when the reader
// still has a byte after that count. The caller deletes the file in that case.
func WriteBody(path string, r io.Reader, declared int64) (n int64, sum string, oversized bool, err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return 0, "", false, err
	}
	defer file.Close()
	hash := sha256.New()
	buf := make([]byte, 32*1024)
	for n < declared {
		want := declared - n
		if want > int64(len(buf)) {
			want = int64(len(buf))
		}
		read, rerr := r.Read(buf[:want])
		if read > 0 {
			if _, werr := file.Write(buf[:read]); werr != nil {
				return n, "", false, werr
			}
			_, _ = hash.Write(buf[:read])
			n += int64(read)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, "", false, rerr
		}
	}
	if n == declared {
		extra := make([]byte, 1)
		read, rerr := r.Read(extra)
		if read > 0 {
			oversized = true
		} else if rerr != nil && rerr != io.EOF {
			return n, "", false, rerr
		}
	}
	if err := file.Sync(); err != nil {
		return n, "", oversized, err
	}
	return n, hex.EncodeToString(hash.Sum(nil)), oversized, nil
}
