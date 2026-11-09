//go:build svn_faultpoint

package faultpoint

import (
	"os"
	"path/filepath"
	"regexp"
)

// Enabled is true when the binary is built with svn_faultpoint.
const Enabled = true

var nameRE = regexp.MustCompile(`^[a-z][a-z-]{0,63}$`)

// Hit renames "<dir>/<name>.arm" to "<dir>/<name>.hit" and then blocks the
// calling goroutine. The rename fires once. An unset directory, a bad name,
// or a missing arm file returns immediately.
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
