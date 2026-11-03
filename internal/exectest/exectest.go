// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package exectest

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// coverDirEnv names the directory a coverage build writes its data to.
const coverDirEnv = "GOCOVERDIR"

// execCmd names exec.Cmd under a name no other package can select, so Cmd
// embeds it without exporting the command it wraps.
type execCmd = exec.Cmd

// Cmd is a command Command built. It has exec.Cmd's fields and methods bar
// Env, which Command alone sets: the Env method reads it, and since a method
// shadows the field at every depth, neither a caller nor a type embedding Cmd
// can assign it.
type Cmd struct{ *execCmd }

// Env returns a copy of the environment the child runs with.
func (c *Cmd) Env() []string { return slices.Clone(c.execCmd.Env) }

// Command returns a command running name with args under ctx, in this
// process's environment with env appended. PWD is left out, so it cannot
// contradict a Dir the caller sets. When the test binary is built for
// coverage, the child writes to a coverage directory of its own, which is
// discarded: children sharing one race to rename the meta-data file the
// runtime names from the clock. A statement only a child runs therefore needs
// an in-process test as well.
func Command(ctx context.Context, t testing.TB, env []string, name string, args ...string) *Cmd {
	t.Helper()
	if ctx == nil {
		t.Fatal("exectest.Command: nil context")
	}
	if name == "" {
		t.Fatal("exectest.Command: empty command name")
	}
	full := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, coverDirEnv+"=") || strings.HasPrefix(kv, "PWD=")
	})
	full = append(full, env...)
	if os.Getenv(coverDirEnv) != "" {
		// Last, so that no entry of env can name another.
		full = append(full, coverDirEnv+"="+t.TempDir())
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = full
	return &Cmd{cmd}
}
