//go:build race

package devnet_test

// broadcasterRace reports whether this test binary was built with -race.
// The broadcaster devnet tests build the svn-broadcaster binary under test
// with the same setting.
const broadcasterRace = true
