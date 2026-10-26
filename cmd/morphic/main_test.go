package main

import (
	"os"
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// asMorphic, set in its environment, makes this test binary run main in place
// of the tests, so a test can start the CLI as a process and signal it.
const asMorphic = "MORPHIC_TEST_AS_CLI"

// TestMain runs main for a child the signal tests start. Otherwise it runs the
// tests through leakcheck.Main, since watchSignals starts a goroutine.
func TestMain(m *testing.M) {
	if os.Getenv(asMorphic) != "" {
		main()
		os.Exit(exitFailure) // main exits on every path; a child must not run the tests
	}
	leakcheck.Main(m)
}
