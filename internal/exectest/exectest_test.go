// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package exectest_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/exectest"
)

// fatal is what a spyTB's Fatal panics with, standing in for the goroutine
// exit a real Fatal makes.
type fatal string

// spyTB records what Command asks of a test. It embeds a real testing.TB for
// the rest.
type spyTB struct {
	testing.TB
}

func (s *spyTB) Helper()           {}
func (s *spyTB) Fatal(args ...any) { panic(fatal(fmt.Sprint(args...))) }

// values returns the value of every entry env gives key, in order.
func values(env []string, key string) []string {
	var vs []string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			vs = append(vs, v)
		}
	}
	return vs
}

// TestCommand_ACoveredChildWritesToADirectoryOfItsOwn pins the fix: the
// child's last GOCOVERDIR is a directory of the test's own, whatever this
// process's says and whatever env says.
func TestCommand_ACoveredChildWritesToADirectoryOfItsOwn(t *testing.T) {
	tests := []struct {
		name   string
		parent string
	}{
		{"this process has one", t.TempDir()},
		{"this process has none", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOCOVERDIR", tt.parent)

			cmd := exectest.Command(t.Context(), t, []string{"GOCOVERDIR=" + tt.parent}, "x")

			got := values(cmd.Env(), "GOCOVERDIR")
			require.NotEmpty(t, got)
			own := got[len(got)-1]
			assert.NotEqual(t, tt.parent, own)
			assert.DirExists(t, own)
		})
	}
}

// TestCommand_AppendsEnvToThisEnvironmentWithoutPWD pins that env follows
// this process's environment, which keeps every entry but PWD and GOCOVERDIR.
func TestCommand_AppendsEnvToThisEnvironmentWithoutPWD(t *testing.T) {
	t.Setenv("PWD", "/nowhere")
	t.Setenv("EXECTEST_KEPT", "1")

	env := exectest.Command(t.Context(), t, []string{"A=1"}, "x").Env()

	assert.Empty(t, values(env, "PWD"))
	assert.Equal(t, []string{"1"}, values(env, "EXECTEST_KEPT"))
	require.GreaterOrEqual(t, len(env), 2)
	assert.Equal(t, "A=1", env[len(env)-2])
}

// TestCommand_BindsTheCommandLineAndTheContext pins that the command runs name
// with args under ctx: Path and Args are what the caller passed, and with ctx
// already cancelled, Run starts no child and returns its error.
func TestCommand_BindsTheCommandLineAndTheContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cmd := exectest.Command(ctx, t, nil, "/bin/sh", "-c", "exit 0")

	assert.Equal(t, filepath.Clean("/bin/sh"), cmd.Path)
	assert.Equal(t, []string{"/bin/sh", "-c", "exit 0"}, cmd.Args)
	assert.ErrorIs(t, cmd.Run(), context.Canceled)
}

// TestCommand_RefusesANilContext pins the refusal, through Fatal.
func TestCommand_RefusesANilContext(t *testing.T) {
	t.Parallel()
	var none context.Context
	assert.PanicsWithValue(t, fatal("exectest.Command: nil context"), func() {
		exectest.Command(none, &spyTB{TB: t}, nil, "x")
	})
}

// TestCommand_RefusesAnEmptyName pins the refusal, through Fatal.
func TestCommand_RefusesAnEmptyName(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t, fatal("exectest.Command: empty command name"), func() {
		exectest.Command(t.Context(), &spyTB{TB: t}, nil, "")
	})
}
