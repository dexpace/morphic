package main

import (
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// asMorphic, set in its environment, makes this test binary run main in place
// of the tests, so a test can start the CLI as a process and signal it.
const asMorphic = "MORPHIC_TEST_AS_CLI"

// signalAtExit, set beside asMorphic to a signal's number, makes main's exit
// send its own process that signal first, at a moment no test could time
// from outside.
const signalAtExit = "MORPHIC_TEST_SIGNAL_AT_EXIT"

// TestMain runs main for a child the signal tests start. Otherwise it runs the
// tests through leakcheck.Main, since they and the signal watch start
// goroutines.
func TestMain(m *testing.M) {
	if os.Getenv(asMorphic) != "" {
		if sig := os.Getenv(signalAtExit); sig != "" {
			osExit = signalledExit(sig)
		}
		main()
		os.Exit(exitFailure) // main exits on every path; a child must not run the tests
	}
	leakcheck.Main(m)
}

// signalledExit returns an osExit that sends this process the signal numbered
// sig, gives it settle to take effect, and then exits with the code it is
// handed. A sig that is no number, or a signal that cannot be sent, exits
// with exitFailure instead.
func signalledExit(sig string) func(int) {
	n, err := strconv.Atoi(sig)
	return func(code int) {
		if err != nil || syscall.Kill(os.Getpid(), syscall.Signal(n)) != nil {
			os.Exit(exitFailure)
		}
		time.Sleep(settle)
		os.Exit(code)
	}
}
