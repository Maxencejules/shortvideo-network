package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store")
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestContentAddressedReports(t *testing.T) {
	s, root := open(t)
	data := []byte(`{"format":"x"}`)
	sum, err := s.PutReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if sum != sha256.Sum256(data) {
		t.Fatal("digest")
	}
	path := filepath.Join(root, "reports", hex.EncodeToString(sum[:])+".json")
	if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
		t.Fatalf("%q %v", got, err)
	}
	if got, err := s.Report(sum); err != nil || string(got) != string(data) {
		t.Fatalf("%q %v", got, err)
	}
	// Idempotent: the same bytes again.
	if again, err := s.PutReport(data); err != nil || again != sum {
		t.Fatalf("second put: %v", err)
	}
	if _, err := s.Report(sha256.Sum256([]byte("absent"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent: %v", err)
	}
	// A report whose bytes no longer hash to its name is corrupt.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"format":"y"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Report(sum); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tampered: %v", err)
	}
}

func TestNamedObjects(t *testing.T) {
	s, _ := open(t)
	if err := s.Put(Batches, "b-1", ".pb", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Batches, "b-1", ".json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Batches, "b-1", ".pb", []byte{1, 2, 3}); err != nil {
		t.Fatalf("same bytes again: %v", err)
	}
	if err := s.Put(Batches, "b-1", ".pb", []byte{1, 2, 4}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting bytes: %v", err)
	}
	if got, err := s.Get(Batches, "b-1", ".pb"); err != nil || string(got) != "\x01\x02\x03" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := s.Get(Envelopes, "e-1", ".json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent: %v", err)
	}
	invalid := []struct {
		c         Collection
		name, ext string
		data      []byte
	}{
		{Envelopes, "e1", ".pb", []byte("x")},
		{Pending, "r1", ".txt", []byte("x")},
		{Collection("reports"), "r1", ".json", []byte("x")},
		{Collection("../x"), "r1", ".json", []byte("x")},
		{Envelopes, "../e1", ".json", []byte("x")},
		{Envelopes, "E1", ".json", []byte("x")},
		{Envelopes, "", ".json", []byte("x")},
		{Envelopes, "e1", ".json", nil},
		{Envelopes, "e1", ".json", make([]byte, MaxObjectBytes+1)},
	}
	for _, tc := range invalid {
		if err := s.Put(tc.c, tc.name, tc.ext, tc.data); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s/%s%s: %v", tc.c, tc.name, tc.ext, err)
		}
	}
}

// Concurrent writers of one name: exactly the same bytes all succeed;
// different bytes produce exactly one winner and conflicts for the rest.
func TestConcurrentPutsCreateOnce(t *testing.T) {
	s, root := open(t)
	var wg sync.WaitGroup
	errs := make([]error, 16)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.Put(Pending, "run-1", ".json", []byte{byte('a' + i%2)})
		}(i)
	}
	wg.Wait()
	stored, err := s.Get(Pending, "run-1", ".json")
	if err != nil {
		t.Fatal(err)
	}
	for i, err := range errs {
		wrote := []byte{byte('a' + i%2)}
		if string(wrote) == string(stored) && err != nil {
			t.Fatalf("writer %d with the stored bytes failed: %v", i, err)
		}
		if string(wrote) != string(stored) && !errors.Is(err, ErrConflict) {
			t.Fatalf("writer %d with other bytes: %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "pending"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("pending holds %d entries", len(entries))
	}
}

// A writer that loses the race between its existence check and its link
// (another writer linked the name in between) never replaces the stored
// object: other bytes are ErrConflict, the same bytes are a no-op. This is
// the path TestConcurrentPutsCreateOnce reaches only when the scheduler
// interleaves the writers; here it is reached every time.
func TestLinkAfterAnotherWriterNeverReplaces(t *testing.T) {
	s, root := open(t)
	if err := s.Put(Pending, "run-1", ".json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "pending")
	final := filepath.Join(dir, "run-1.json")
	for _, tc := range []struct {
		data     string
		conflict bool
	}{{"other", true}, {"first", false}} {
		tmp, err := writeTemp(dir, []byte(tc.data))
		if err != nil {
			t.Fatal(err)
		}
		err = publish(dir, tmp, final, []byte(tc.data))
		os.Remove(tmp)
		if tc.conflict != errors.Is(err, ErrConflict) || (!tc.conflict && err != nil) {
			t.Fatalf("%s: %v", tc.data, err)
		}
		if got, err := s.Get(Pending, "run-1", ".json"); err != nil || string(got) != "first" {
			t.Fatalf("%s: stored %q %v", tc.data, got, err)
		}
	}
}

// A crash between the temporary write and the link leaves no object under
// the final name; a later put of the same name succeeds.
func TestNoPartialFileAfterCrash(t *testing.T) {
	s, root := open(t)
	dir := filepath.Join(root, "envelopes")
	tmp, err := writeTemp(dir, []byte("partial"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(tmp), tempPrefix) || mode(t, tmp) != fileMode {
		t.Fatalf("temp %s mode %v", tmp, mode(t, tmp))
	}
	if _, err := s.Get(Envelopes, "e-1", ".json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("visible after crash: %v", err)
	}
	if err := s.Put(Envelopes, "e-1", ".json", []byte("complete")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(Envelopes, "e-1", ".json"); err != nil || string(got) != "complete" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("the crashed temp file is gone: %v", err)
	}
}

func TestModes(t *testing.T) {
	s, root := open(t)
	if _, err := s.PutReport([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Pending, "r1", ".json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendIndex([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", "reports", "envelopes", "batches", "pending"} {
		if m := mode(t, filepath.Join(root, dir)); m != dirMode {
			t.Fatalf("%s/ is %v", dir, m)
		}
	}
	sum := sha256.Sum256([]byte("{}"))
	for _, file := range []string{"reports/" + hex.EncodeToString(sum[:]) + ".json", "pending/r1.json", "index.jsonl"} {
		if m := mode(t, filepath.Join(root, file)); m != fileMode {
			t.Fatalf("%s is %v", file, m)
		}
	}
}

func TestOpenRefusesUnsafeRoots(t *testing.T) {
	base := t.TempDir()
	wide := filepath.Join(base, "wide")
	if err := os.Mkdir(wide, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wide, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(wide); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("0755 root: %v", err)
	}
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("symlink root: %v", err)
	}
	subWide := filepath.Join(base, "subwide")
	if err := os.MkdirAll(filepath.Join(subWide, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(subWide, "reports"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(subWide); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("0750 subdirectory: %v", err)
	}
	if _, err := Open(""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty root: %v", err)
	}
}

func TestSymlinkedObjectRefused(t *testing.T) {
	s, root := open(t)
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "envelopes", "e-1.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(Envelopes, "e-1", ".json"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("get through symlink: %v", err)
	}
	if err := s.Put(Envelopes, "e-1", ".json", []byte("x")); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("put over symlink: %v", err)
	}
}

func TestIndexAppendOnly(t *testing.T) {
	s, root := open(t)
	lines := []string{`{"evidence":"e1","report_sha256":"aa"}`, `{"evidence":"e2","report_sha256":"bb"}`}
	for _, line := range lines {
		if err := s.AppendIndex([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	if err := s.ScanIndex(func(line []byte) error { got = append(got, string(line)); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, lines) {
		t.Fatalf("%v", got)
	}
	for _, bad := range []string{"", "[]", `"x"`, "{", "{}\n{}", `{"a":1}` + "\r", "{\"a\":\"" + strings.Repeat("x", MaxIndexLineBytes) + "\"}"} {
		if err := s.AppendIndex([]byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%.20q: %v", bad, err)
		}
	}
	// A torn last line (an append cut short) is reported, not skipped.
	f, err := os.OpenFile(filepath.Join(root, "index.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"evidence":"e3"`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := s.ScanIndex(func([]byte) error { return nil }); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("torn line: %v", err)
	}
	// An append after a torn line is refused and leaves the file unchanged,
	// so the torn bytes are never glued to a new line.
	index := filepath.Join(root, "index.jsonl")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendIndex([]byte(`{"evidence":"e4"}`)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("append after a torn line: %v", err)
	}
	if after, err := os.ReadFile(index); err != nil || string(after) != string(before) {
		t.Fatalf("index changed by a refused append: %v", err)
	}
}

// A complete line that is not a JSON object (written by something other
// than AppendIndex, or two torn appends glued together) is ErrCorrupt, not
// handed to the caller.
func TestScanIndexRefusesInvalidLines(t *testing.T) {
	for _, content := range []string{
		"{\"evidence\":\"e2\",\"rep{\"evidence\":\"e3\"}\n",
		"[]\n",
		"\n",
		"{\"a\":1}\n\"x\"\n",
	} {
		s, root := open(t)
		if err := os.WriteFile(filepath.Join(root, "index.jsonl"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		calls := 0
		err := s.ScanIndex(func([]byte) error { calls++; return nil })
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%q: %v", content, err)
		}
		if strings.Count(content, "\n") == 2 && calls != 1 {
			t.Fatalf("%q: %d valid lines handed over before the invalid one", content, calls)
		}
	}
}

func TestScanIndexWithoutIndex(t *testing.T) {
	s, _ := open(t)
	calls := 0
	if err := s.ScanIndex(func([]byte) error { calls++; return nil }); err != nil || calls != 0 {
		t.Fatalf("%d %v", calls, err)
	}
}

// The store has no way to delete: retention is the approved DEC-004 default
// (no duration set; nothing deleted).
func TestNoDeleteOperation(t *testing.T) {
	typ := reflect.TypeOf(&Store{})
	for i := 0; i < typ.NumMethod(); i++ {
		name := strings.ToLower(typ.Method(i).Name)
		for _, verb := range []string{"delete", "remove", "prune", "purge", "expire", "truncate"} {
			if strings.Contains(name, verb) {
				t.Fatalf("method %s", typ.Method(i).Name)
			}
		}
	}
}
