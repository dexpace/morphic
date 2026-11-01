// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/exectest"
)

// coverDirEnv names the directory a coverage build writes its data to, and
// countersPrefix starts the name of a counter file the runtime writes once per
// process (internal/coverage, which no module may import).
const (
	coverDirEnv    = "GOCOVERDIR"
	countersPrefix = "covcounters."
)

// command returns a command running name with args in a child that runs as
// the CLI, which childContext kills when the test ends. It is the one way
// these tests start a child, so none shares a coverage directory with another.
func command(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exectest.Command(childContext(t), t, name, args...)
	cmd.Env = append(cmd.Env, asMorphic+"=1")
	return cmd
}

// TestChild_ACoveredChildsCountersReachTheParentDir pins, in a coverage build,
// that what a real child writes is in the parent's directory once its test
// ends, which is how the profile counts the statements only a child ran.
func TestChild_ACoveredChildsCountersReachTheParentDir(t *testing.T) {
	parent := os.Getenv(coverDirEnv)
	if parent == "" {
		t.Skip("this binary is not built for coverage")
	}
	var pid int
	t.Run("child", func(t *testing.T) {
		cmd := morphic(t, "--version")
		require.NoError(t, cmd.Run())
		pid = cmd.Process.Pid
	})

	var mine []string
	for _, name := range namesIn(t, parent) {
		if strings.HasPrefix(name, countersPrefix) && strings.Contains(name, "."+strconv.Itoa(pid)+".") {
			mine = append(mine, name)
		}
	}
	assert.Len(t, mine, 1, "the child's counter file")
}
