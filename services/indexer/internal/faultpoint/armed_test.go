//go:build svn_faultpoint

package faultpoint

import "testing"

func TestArmedBuild(t *testing.T) {
	if !Enabled {
		t.Fatal("svn_faultpoint build must arm fault points")
	}
}
