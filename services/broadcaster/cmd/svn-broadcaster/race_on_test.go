//go:build race

package main

// raceEnabled is true when the test binary is built with -race; the smoke
// test then builds the svn-broadcaster binary with -race too.
const raceEnabled = true
