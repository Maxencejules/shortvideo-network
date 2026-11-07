package difftest

import (
	"flag"
	"fmt"
	"os"
	"testing"
)

// fuzzMinimizeDefault bounds the minimization of each new interesting fuzz
// input. One attempt replays a whole generated sequence on fresh keepers,
// and a byte removed anywhere changes every later choice, so the testing
// package's default (60 seconds per input) spends a short -fuzztime run on
// minimizing the first input and executes almost nothing else. The default
// applies only when the command line does not set -test.fuzzminimizetime.
const fuzzMinimizeDefault = "50x"

func TestMain(m *testing.M) {
	flag.Parse()
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "test.fuzzminimizetime" {
			set = true
		}
	})
	if !set {
		if err := flag.Set("test.fuzzminimizetime", fuzzMinimizeDefault); err != nil {
			fmt.Fprintf(os.Stderr, "difftest: set -test.fuzzminimizetime: %v\n", err)
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}
