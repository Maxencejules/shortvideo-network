//go:build !svn_faultpoint

package faultpoint

// Enabled is false in the default build: there are no fault points.
const Enabled = false

// Hit does nothing in the default build.
func Hit(string) {}
