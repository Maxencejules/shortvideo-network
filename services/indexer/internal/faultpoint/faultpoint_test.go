//go:build !svn_faultpoint

package faultpoint

import "testing"

func TestBuildTag(t *testing.T) {
	if Enabled {
		t.Fatal("default build must not arm fault points")
	}
	Hit(AfterEffectsBeforeCheckpoint)
}
