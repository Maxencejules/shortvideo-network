package secretfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path string, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	write(t, good, "s3cr3t-canary\n", 0o600)
	got, err := Read(good, 1024)
	if err != nil || string(got) != "s3cr3t-canary\n" {
		t.Fatalf("%q %v", got, err)
	}
	ro := filepath.Join(dir, "readonly")
	write(t, ro, "x", 0o400)
	if _, err := Read(ro, 1024); err != nil {
		t.Fatalf("owner read-only refused: %v", err)
	}

	for _, mode := range []os.FileMode{0o640, 0o604, 0o620, 0o660, 0o644, 0o601} {
		p := filepath.Join(dir, "mode")
		write(t, p, "x", mode)
		if _, err := Read(p, 1024); !errors.Is(err, ErrMode) {
			t.Fatalf("mode %04o: %v", mode, err)
		}
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(link, 1024); !errors.Is(err, ErrNotFile) {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := Read(dir, 1024); !errors.Is(err, ErrNotFile) {
		t.Fatalf("directory: %v", err)
	}

	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(realDir, "f"), "x", 0o600)
	linkDir := filepath.Join(dir, "linkdir")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(filepath.Join(linkDir, "f"), 1024); !errors.Is(err, ErrParent) {
		t.Fatalf("symlinked parent: %v", err)
	}

	empty := filepath.Join(dir, "empty")
	write(t, empty, "", 0o600)
	if _, err := Read(empty, 1024); !errors.Is(err, ErrSize) {
		t.Fatalf("empty: %v", err)
	}
	big := filepath.Join(dir, "big")
	write(t, big, strings.Repeat("x", 11), 0o600)
	if _, err := Read(big, 10); !errors.Is(err, ErrSize) {
		t.Fatalf("oversize: %v", err)
	}
	if _, err := Read(big, 11); err != nil {
		t.Fatalf("at the bound: %v", err)
	}

	for _, p := range []string{"", "relative/file", dir + "/../" + filepath.Base(dir) + "/good"} {
		if _, err := Read(p, 1024); !errors.Is(err, ErrPath) {
			t.Fatalf("%q: %v", p, err)
		}
	}
	for _, n := range []int{0, MaxBytes + 1} {
		if _, err := Read(good, n); !errors.Is(err, ErrMaxBytes) {
			t.Fatalf("max %d: %v", n, err)
		}
	}
	if _, err := Read(filepath.Join(dir, "missing"), 1024); err == nil {
		t.Fatal("missing file read")
	}
}

// TestOwnerRule checks the ownership rule against the effective uid the
// check is given: a file the process owns is refused for any other uid.
// (Creating a file owned by another uid needs root, so the rule is driven
// through the uid argument instead.)
func TestOwnerRule(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mine")
	write(t, p, "x", 0o600)
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	self := uint32(os.Geteuid())
	if err := check(info, self); err != nil {
		t.Fatalf("own file: %v", err)
	}
	if err := check(info, self+1); !errors.Is(err, ErrOwner) {
		t.Fatalf("another uid: %v", err)
	}
}
