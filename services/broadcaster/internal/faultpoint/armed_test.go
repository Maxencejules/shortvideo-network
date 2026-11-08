//go:build svn_faultpoint

package faultpoint

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// returned runs Hit(name) in a goroutine and reports whether it returned
// within wait.
func returned(name string, wait time.Duration) bool {
	done := make(chan struct{})
	go func() {
		Hit(name)
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(wait):
		return false
	}
}

// TestArmedPointFiresOnceAndBlocks: an armed point renames its arm file to
// the marker and blocks its goroutine; the same point reached again (the
// restarted process) is no longer armed and returns.
func TestArmedPointFiresOnceAndBlocks(t *testing.T) {
	if !Enabled {
		t.Fatal("Enabled is false in the svn_faultpoint build")
	}
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	for _, name := range []string{AfterSignBeforePersist, AfterPersistCommit, AfterSendsStarted,
		AfterBroadcastBeforeRecord, AfterDecodeBeforeReply} {
		if err := os.WriteFile(filepath.Join(dir, name+".arm"), []byte("armed by test"), 0o600); err != nil {
			t.Fatal(err)
		}
		if returned(name, 300*time.Millisecond) {
			t.Fatalf("%s: an armed point returned instead of blocking", name)
		}
		got, err := os.ReadFile(filepath.Join(dir, name+".hit"))
		if err != nil || string(got) != "armed by test" {
			t.Fatalf("%s: marker %q, %v", name, got, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name+".arm")); !os.IsNotExist(err) {
			t.Fatalf("%s: the arm file is still there after firing", name)
		}
		if !returned(name, 5*time.Second) {
			t.Fatalf("%s: a point that already fired blocked again", name)
		}
	}
}

// TestUnarmedPointReturns: no directory variable, a relative directory, a
// missing arm file, or a malformed name never blocks.
func TestUnarmedPointReturns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Bad_Name.arm"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		label, env, name string
	}{
		{"no directory", "", AfterPersistCommit},
		{"relative directory", "relative/dir", AfterPersistCommit},
		{"not armed", dir, AfterPersistCommit},
		{"malformed name", dir, "Bad_Name"},
		{"path in the name", dir, "../" + AfterPersistCommit},
	}
	for _, c := range cases {
		t.Setenv(EnvDir, c.env)
		if !returned(c.name, 5*time.Second) {
			t.Fatalf("%s: Hit blocked", c.label)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Bad_Name.arm")); err != nil {
		t.Fatalf("a malformed name touched its file: %v", err)
	}
}
