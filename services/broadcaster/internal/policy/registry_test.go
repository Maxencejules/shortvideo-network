package policy

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// registryJSON returns registry file contents for entries of
// (principal, public key hex, label).
func registryJSON(entries ...[3]string) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf(`{"principal_id":%q,"public_key_hex":%q,"label":%q}`, e[0], e[1], e[2])
	}
	return `{"entries":[` + strings.Join(parts, ",") + `]}`
}

func hexKey(b byte) string { return strings.Repeat(hex.EncodeToString([]byte{b}), 32) }

// writeRegistry writes contents into a fresh directory with the given
// file and directory modes and returns the file path.
func writeRegistry(t *testing.T, contents string, fileMode, dirMode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "registry")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "intent-keys.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, fileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeInfo is a synthetic fs.FileInfo for WritableBy.
type fakeInfo struct {
	mode     fs.FileMode
	uid, gid uint32
}

func (f fakeInfo) Name() string       { return "f" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid, Gid: f.gid} }

// TestIntentKeyRegistryRefusesWritableFile pins plan 7.4: serve and the
// signer refuse a registry that their own uid could change. Under
// DistinctUIDs that is decided by owner, group and mode (WritableBy, here
// over synthetic file metadata for a non-root uid, and for real in the
// svn_roottest uid tests); the process must not be root. Under SameUID the
// file must have no write bit and the directory no group or other write
// bit.
func TestIntentKeyRegistryRefusesWritableFile(t *testing.T) {
	const self, selfGid, other, otherGid = 2001, 2001, 0, 0
	cases := []struct {
		name   string
		info   fakeInfo
		groups []int
		want   bool
	}{
		{"operator file 0644", fakeInfo{0o644, other, otherGid}, nil, false},
		{"operator dir 0755", fakeInfo{fs.ModeDir | 0o755, other, otherGid}, nil, false},
		{"owned by self 0444", fakeInfo{0o444, self, otherGid}, nil, true},
		{"world writable", fakeInfo{0o646, other, otherGid}, nil, true},
		{"sticky world-writable dir", fakeInfo{fs.ModeDir | fs.ModeSticky | 0o777, other, otherGid}, nil, true},
		{"group writable, primary group", fakeInfo{0o664, other, selfGid}, nil, true},
		{"group writable, supplementary group", fakeInfo{0o664, other, 3000}, []int{3000}, true},
		{"group writable, other group", fakeInfo{0o664, other, 3000}, []int{4000}, false},
		{"group read-only, own group", fakeInfo{0o644, other, selfGid}, nil, false},
	}
	for _, tc := range cases {
		if got := WritableBy(tc.info, self, selfGid, tc.groups); got != tc.want {
			t.Errorf("%s: WritableBy = %v, want %v", tc.name, got, tc.want)
		}
	}
	if !WritableBy(fakeInfo{0o444, other, otherGid}, 0, 0, nil) {
		t.Error("uid 0 can write every file")
	}

	contents := registryJSON([3]string{"alice", hexKey(1), "devnet fixture"})
	t.Run("same-uid fallback accepts a read-only file", func(t *testing.T) {
		reg, err := LoadRegistry(writeRegistry(t, contents, 0o444, 0o755), SameUID)
		if err != nil {
			t.Fatal(err)
		}
		if k, ok := reg.KeyFor("alice"); !ok || hex.EncodeToString(k) != hexKey(1) || reg.Len() != 1 {
			t.Fatalf("KeyFor = %x %v", k, ok)
		}
	})
	for _, mode := range []os.FileMode{0o644, 0o600, 0o464, 0o446, 0o666} {
		t.Run(fmt.Sprintf("same-uid file mode %04o", mode), func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, contents, mode, 0o755), SameUID)
			if !errors.Is(err, ErrRegistryWritable) {
				t.Fatalf("LoadRegistry = %v, want ErrRegistryWritable", err)
			}
		})
	}
	for _, mode := range []os.FileMode{0o775, 0o757, 0o777} {
		t.Run(fmt.Sprintf("same-uid directory mode %04o", mode), func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, contents, 0o444, mode), SameUID)
			if !errors.Is(err, ErrRegistryWritable) {
				t.Fatalf("LoadRegistry = %v, want ErrRegistryWritable", err)
			}
		})
	}
	t.Run("distinct uids refuse a file this uid owns", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, contents, 0o444, 0o755), DistinctUIDs)
		if !errors.Is(err, ErrRegistryWritable) {
			t.Fatalf("LoadRegistry = %v, want ErrRegistryWritable (root, or the owner)", err)
		}
	})
	t.Run("symlink and missing file", func(t *testing.T) {
		path := writeRegistry(t, contents, 0o444, 0o755)
		link := filepath.Join(filepath.Dir(path), "link.json")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRegistry(link, SameUID); err == nil {
			t.Fatal("a symlinked registry was accepted")
		}
		if _, err := LoadRegistry(filepath.Join(filepath.Dir(path), "absent.json"), SameUID); err == nil {
			t.Fatal("a missing registry was accepted")
		}
		if _, err := LoadRegistry(path, Boundary(0)); err == nil {
			t.Fatal("an unknown boundary was accepted")
		}
	})
}

func TestParseRegistryRefusals(t *testing.T) {
	if reg, err := ParseRegistry([]byte(registryJSON([3]string{"alice", hexKey(1), "a"}, [3]string{"bob", hexKey(2), ""}))); err != nil || reg.Len() != 2 {
		t.Fatalf("valid registry: %v", err)
	}
	reg, err := ParseRegistry([]byte(registryJSON([3]string{"alice", hexKey(1), "a"})))
	if err != nil {
		t.Fatal(err)
	}
	key1, _ := hex.DecodeString(hexKey(1))
	key2, _ := hex.DecodeString(hexKey(2))
	if !reg.Contains(key1) || reg.Contains(key2) || reg.Contains(key1[:31]) {
		t.Fatal("Contains is wrong")
	}
	if _, ok := reg.KeyFor("bob"); ok {
		t.Fatal("KeyFor found an unregistered principal")
	}
	many := make([][3]string, MaxRegistryEntries+1)
	for i := range many {
		many[i] = [3]string{fmt.Sprintf("p%d", i), fmt.Sprintf("%064x", i+1), ""}
	}
	bad := map[string]string{
		"unknown field":       `{"entries":[{"principal_id":"alice","public_key_hex":"` + hexKey(1) + `","label":"","email":"x@y"}]}`,
		"top-level field":     `{"entries":[],"extra":"x"}`,
		"trailing data":       registryJSON([3]string{"alice", hexKey(1), ""}) + `{}`,
		"bad principal id":    registryJSON([3]string{"Alice!", hexKey(1), ""}),
		"empty principal":     registryJSON([3]string{"", hexKey(1), ""}),
		"uppercase hex":       registryJSON([3]string{"alice", strings.ToUpper(hexKey(10)), ""}),
		"short key":           registryJSON([3]string{"alice", hexKey(1)[:62], ""}),
		"not hex":             registryJSON([3]string{"alice", strings.Repeat("zz", 32), ""}),
		"duplicate principal": registryJSON([3]string{"alice", hexKey(1), ""}, [3]string{"alice", hexKey(2), ""}),
		"duplicate key":       registryJSON([3]string{"alice", hexKey(1), ""}, [3]string{"bob", hexKey(1), ""}),
		"long label":          registryJSON([3]string{"alice", hexKey(1), strings.Repeat("l", MaxLabelBytes+1)}),
		"control in label":    registryJSON([3]string{"alice", hexKey(1), "a\nb"}),
		"too many entries":    registryJSON(many...),
		"not json":            `entries`,
	}
	for name, raw := range bad {
		if _, err := ParseRegistry([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestRegistryForServe pins the signer's distinct-uid check of the serve
// uid (CheckRegistryForServe): a registry file or directory owned by the
// serve uid, or with a group or other write bit, is refused; an operator
// file 0644 in a 0755 directory owned by another uid is accepted. The
// files here belong to the test's own uid, which plays the serve uid when
// ownership is checked and the operator otherwise; a directory owned by
// the serve uid needs a chown and is checked by the svn_roottest tests.
func TestRegistryForServe(t *testing.T) {
	self := uint32(os.Geteuid())
	other := self + 1
	contents := registryJSON([3]string{"alice", hexKey(1), "devnet fixture"})
	if err := CheckRegistryForServe(writeRegistry(t, contents, 0o644, 0o755), other); err != nil {
		t.Fatalf("operator layout refused: %v", err)
	}
	if err := CheckRegistryForServe(writeRegistry(t, contents, 0o444, 0o755), self); !errors.Is(err, ErrRegistryServeWritable) ||
		!strings.Contains(err.Error(), "intent-keys.json is owned by the serve uid") {
		t.Fatalf("file owned by the serve uid: %v", err)
	}
	for _, mode := range []os.FileMode{0o664, 0o646, 0o666} {
		if err := CheckRegistryForServe(writeRegistry(t, contents, mode, 0o755), other); !errors.Is(err, ErrRegistryServeWritable) {
			t.Errorf("file mode %04o: %v", mode, err)
		}
	}
	for _, mode := range []os.FileMode{0o775, 0o757, 0o777} {
		if err := CheckRegistryForServe(writeRegistry(t, contents, 0o644, mode), other); !errors.Is(err, ErrRegistryServeWritable) {
			t.Errorf("directory mode %04o: %v", mode, err)
		}
	}
	path := writeRegistry(t, contents, 0o644, 0o755)
	link := filepath.Join(filepath.Dir(path), "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckRegistryForServe(link, other); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlinked registry: %v", err)
	}
	if err := CheckRegistryForServe(filepath.Join(filepath.Dir(path), "absent.json"), other); err == nil {
		t.Fatal("a missing registry was accepted")
	}
}
