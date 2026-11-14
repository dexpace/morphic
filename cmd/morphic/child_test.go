// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"testing"

	"github.com/dexpace/morphic/internal/exectest"
)

// command returns a command running name with args in a child that runs as
// the CLI, in this process's environment with env appended, which
// childContext kills when the test ends.
func command(t *testing.T, env []string, name string, args ...string) *exectest.Cmd {
	t.Helper()
	return exectest.Command(childContext(t), t, append([]string{asMorphic + "=1"}, env...), name, args...)
}
