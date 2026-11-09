//go:build !svn_faultpoint

package faultpoint

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHitIsNoOpInDefaultBuild: without the svn_faultpoint tag a point never
// fires, even when its arm file exists and the directory variable names it.
func TestHitIsNoOpInDefaultBuild(t *testing.T) {
	if Enabled {
		t.Fatal("Enabled is true in the default build")
	}
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	for _, name := range []string{AfterSignBeforePersist, AfterPersistCommit, AfterSendsStarted,
		AfterBroadcastBeforeRecord, AfterDecodeBeforeReply} {
		arm := filepath.Join(dir, name+".arm")
		if err := os.WriteFile(arm, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		Hit(name)
		if _, err := os.Stat(arm); err != nil {
			t.Fatalf("%s: arm file changed in the default build: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name+".hit")); !os.IsNotExist(err) {
			t.Fatalf("%s: a marker file exists in the default build", name)
		}
	}
}
