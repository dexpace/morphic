package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain pins the environment the rendered text depends on, so the goldens
// read the same on a developer's terminal as in CI.
//
// fang and the diagnostics writer pick a colour profile from the environment,
// and fang sizes its help from the terminal's width. TTY_FORCE and
// CLICOLOR_FORCE would make a buffer look like a terminal. __FANG_TEST_WIDTH is
// fang's own test hook, so if a later release drops it the help goldens stop
// matching rather than quietly following the width of whoever ran them.
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
	if err := os.Setenv("__FANG_TEST_WIDTH", "100"); err != nil {
		fmt.Fprintf(os.Stderr, "set fang width: %v\n", err)
		return 1
	}
	return m.Run()
}
