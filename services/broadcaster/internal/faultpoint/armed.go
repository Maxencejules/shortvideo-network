//go:build svn_faultpoint

package faultpoint

import (
	"os"
	"path/filepath"
	"regexp"
)

// Enabled is true in a binary built with the svn_faultpoint tag.
const Enabled = true

// nameRE bounds a point name, so it is always one path element.
var nameRE = regexp.MustCompile(`^[a-z][a-z-]{0,63}$`)

// Hit fires the fault point name when it is armed: it renames
// "<dir>/<name>.arm" to "<dir>/<name>.hit", where dir is the value of
// SVN_FAULTPOINT_DIR, and then blocks the calling goroutine for the rest of
// the process's life. When the variable is unset, the name is malformed, or
// the arm file is absent (the rename fails), Hit returns at once and the
// caller continues.
func Hit(name string) {
	dir := os.Getenv(EnvDir)
	if dir == "" || !filepath.IsAbs(dir) || !nameRE.MatchString(name) {
		return
	}
	if err := os.Rename(filepath.Join(dir, name+".arm"), filepath.Join(dir, name+".hit")); err != nil {
		return
	}
	select {}
}
