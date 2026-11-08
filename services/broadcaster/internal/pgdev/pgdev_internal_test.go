package pgdev

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestValidateDirRefuses: only a clean direct child of os.TempDir() named
// svn-pg-* that is a real directory with a short enough socket path passes.
func TestValidateDirRefuses(t *testing.T) {
	tmp := filepath.Clean(os.TempDir())
	good, err := os.MkdirTemp("", DirPrefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(good) })
	if err := ValidateDir(good); err != nil {
		t.Fatalf("good dir refused: %v", err)
	}

	other, err := os.MkdirTemp("", "svn-other-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	nested := filepath.Join(good, DirPrefix+"nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, DirPrefix+"link-"+filepath.Base(good))
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
	file := filepath.Join(tmp, DirPrefix+"file-"+filepath.Base(good))
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(file) })

	cases := map[string]string{
		"wrong prefix":   other,
		"nested":         nested,
		"relative":       filepath.Base(good),
		"not clean":      good + "/",
		"dot dot":        filepath.Join(good, "..", filepath.Base(good)) + "/.",
		"symlink":        link,
		"regular file":   file,
		"missing":        filepath.Join(tmp, DirPrefix+"missing-"+filepath.Base(good)),
		"outside tmpdir": "/var/" + DirPrefix + "x",
	}
	for name, dir := range cases {
		if err := ValidateDir(dir); !errors.Is(err, ErrDirectory) {
			t.Errorf("%s: %q: want ErrDirectory, got %v", name, dir, err)
		}
	}

	t.Setenv("TMPDIR", "/"+strings.Repeat("d", 100))
	long := filepath.Join(os.TempDir(), DirPrefix+"x")
	if err := ValidateDir(long); !errors.Is(err, ErrDirectory) || !strings.Contains(err.Error(), "socket path") {
		t.Errorf("socket path too long: want the socket path ErrDirectory, got %v", err)
	}
}

// TestCanSearchRule: the owner bits decide for the owner, the group bits for
// a group member, the other bits for everyone else, as the kernel does.
func TestCanSearchRule(t *testing.T) {
	cases := []struct {
		perm            os.FileMode
		owner, ownerGrp int
		uid, gid        int
		groups          []int
		want            bool
		name            string
	}{
		{0o700, 0, 0, 102, 104, nil, false, "root 0700 other user"},
		{0o711, 0, 0, 102, 104, nil, true, "root 0711 other user"},
		{0o601, 102, 104, 102, 104, nil, false, "owner without x, other with x"},
		{0o710, 0, 104, 102, 104, nil, true, "group x by primary gid"},
		{0o710, 0, 101, 102, 104, []int{101}, true, "group x by supplementary gid"},
		{0o701, 0, 104, 102, 104, nil, false, "group member without group x"},
		{0o1777, 0, 0, 102, 104, nil, true, "/tmp"},
	}
	for _, c := range cases {
		if got := canSearch(c.perm, c.owner, c.ownerGrp, c.uid, c.gid, c.groups); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestStartRefusesUntraversableTempDir: with TMPDIR under a mode-0700
// directory (the situation of /tmp/claude-0 here), Start refuses with
// ErrNotTraversable before initdb runs and leaves no cluster directory.
// As root the server user is "postgres", which cannot traverse it; as any
// other user the same check runs for uid 65534 (nobody).
func TestStartRefusesUntraversableTempDir(t *testing.T) {
	closed := filepath.Join(t.TempDir(), "closed")
	if err := os.Mkdir(closed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0o700); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Setenv("TMPDIR", closed)
		_, err := Start(context.Background(), Options{BinDir: "/usr/lib/postgresql/18/bin", StartTimeout: time.Minute})
		if !errors.Is(err, ErrNotTraversable) {
			t.Fatalf("want ErrNotTraversable, got %v", err)
		}
		entries, err := os.ReadDir(closed)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("Start left %d entries in TMPDIR", len(entries))
		}
		return
	}
	target := filepath.Join(closed, DirPrefix+"x")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkTraversable(target, 65534, 65534, nil); !errors.Is(err, ErrNotTraversable) {
		t.Fatalf("want ErrNotTraversable for uid 65534, got %v", err)
	}
}

// TestOptionsRefused: a relative or unclean BinDir, a start timeout outside
// its bounds, and a directory without postgres are refused before any
// directory is created.
func TestOptionsRefused(t *testing.T) {
	ctx := context.Background()
	for name, o := range map[string]Options{
		"relative bin":  {BinDir: "usr/lib/postgresql/18/bin", StartTimeout: time.Minute},
		"unclean bin":   {BinDir: "/usr/lib/postgresql/18/bin/", StartTimeout: time.Minute},
		"timeout short": {BinDir: "/usr/lib/postgresql/18/bin", StartTimeout: MinStartTimeout - time.Millisecond},
		"timeout long":  {BinDir: "/usr/lib/postgresql/18/bin", StartTimeout: MaxStartTimeout + time.Second},
	} {
		if _, err := Start(ctx, o); !errors.Is(err, ErrOptions) {
			t.Errorf("%s: want ErrOptions, got %v", name, err)
		}
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if _, err := Start(ctx, Options{BinDir: tmp, StartTimeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "postgres --version") {
		t.Fatalf("missing binary: want a postgres --version error, got %v", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("missing binary left %d entries", len(entries))
	}
}

// TestNamesRefused: role and database names outside the pattern never reach
// the server.
func TestNamesRefused(t *testing.T) {
	c := &Cluster{}
	ctx := context.Background()
	for _, bad := range []string{"", "A", "1abc", "a-b", "a;drop role x", `a"b`, strings.Repeat("a", 64)} {
		if _, err := c.CreateLoginRole(ctx, bad); !errors.Is(err, ErrName) {
			t.Errorf("CreateLoginRole(%q): want ErrName, got %v", bad, err)
		}
		if err := c.CreateDatabase(ctx, bad, "owner"); !errors.Is(err, ErrName) {
			t.Errorf("CreateDatabase(%q, owner): want ErrName, got %v", bad, err)
		}
		if err := c.CreateDatabase(ctx, "db", bad); !errors.Is(err, ErrName) {
			t.Errorf("CreateDatabase(db, %q): want ErrName, got %v", bad, err)
		}
		if err := c.GrantConnect(ctx, bad, "role"); !errors.Is(err, ErrName) {
			t.Errorf("GrantConnect(%q, role): want ErrName, got %v", bad, err)
		}
		if _, err := c.SuperuserDB(bad); !errors.Is(err, ErrName) {
			t.Errorf("SuperuserDB(%q): want ErrName, got %v", bad, err)
		}
	}
}
