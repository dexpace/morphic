// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"os"
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs a child in place of the tests when probe re-runs this binary
// as one, and otherwise runs the tests through leakcheck.Main: a child watches
// its own memory from a goroutine of its own.
func TestMain(m *testing.M) {
	if name := os.Getenv(childEnv); name != "" {
		os.Exit(runChild(name, os.Getenv(inputEnv)))
	}
	leakcheck.Main(m)
}
