package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain pins the environment the rendered text depends on, so the goldens
// read the same on a developer's terminal as in CI: the diagnostics writer picks
// a colour profile from the environment, and TTY_FORCE and CLICOLOR_FORCE would
// make a buffer look like a terminal.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	for _, name := range []string{"TTY_FORCE", "CLICOLOR_FORCE", "NO_COLOR"} {
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintf(os.Stderr, "unset %s: %v\n", name, err)
			return 1
		}
	}
	return m.Run()
}
