//go:build !race

package main

// raceEnabled is false when the test binary is built without -race.
const raceEnabled = false
