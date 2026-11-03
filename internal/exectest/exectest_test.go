// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package exectest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/exectest"
)

// fatal is what a fatalTB's Fatal panics with, standing in for the goroutine
// exit a real Fatal makes.
type fatal string

// fatalTB is a test whose Fatal panics with a fatal, so a test can catch the
// refusals of Command. It embeds a real testing.TB for the rest.
type fatalTB struct {
	testing.TB
}

func (s *fatalTB) Fatal(args ...any) { panic(fatal(fmt.Sprint(args...))) }

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
	t.Setenv("GOCOVERDIR", "/parent")
	t.Setenv("EXECTEST_KEPT", "1")

	env := exectest.Command(t.Context(), t, []string{"A=1"}, "x").Env()

	assert.Empty(t, values(env, "PWD"))
	assert.NotContains(t, values(env, "GOCOVERDIR"), "/parent")
	assert.Equal(t, []string{"1"}, values(env, "EXECTEST_KEPT"))
	require.GreaterOrEqual(t, len(env), 2)
	assert.Equal(t, "A=1", env[len(env)-2])
}

// TestCommand_DiscardsTheChildsDirectory pins that the child's coverage
// directory goes when the test that built the command ends.
func TestCommand_DiscardsTheChildsDirectory(t *testing.T) {
	t.Parallel()
	var own string
	t.Run("build", func(t *testing.T) {
		got := values(exectest.Command(t.Context(), t, nil, "x").Env(), "GOCOVERDIR")
		require.NotEmpty(t, got)
		own = got[len(got)-1]
		require.DirExists(t, own)
	})
	assert.NoDirExists(t, own)
}

// TestCmd_EnvReturnsACopy pins that writing to what Env returns leaves the
// child's environment alone, so Env cannot point the child at another
// coverage directory.
func TestCmd_EnvReturnsACopy(t *testing.T) {
	t.Parallel()
	cmd := exectest.Command(t.Context(), t, nil, "x")

	env := cmd.Env()
	env[len(env)-1] = "GOCOVERDIR=/parent"

	assert.NotContains(t, values(cmd.Env(), "GOCOVERDIR"), "/parent")
}

// TestCommand_BindsTheCommandLineAndTheContext pins that the command runs name
// with args under ctx: Path and Args are what the caller passed, and with ctx
// already cancelled, Run starts no child and returns its error.
func TestCommand_BindsTheCommandLineAndTheContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cmd := exectest.Command(ctx, t, nil, "/bin/sh", "-c", "exit 0")

	assert.Equal(t, "/bin/sh", cmd.Path)
	assert.Equal(t, []string{"/bin/sh", "-c", "exit 0"}, cmd.Args)
	assert.ErrorIs(t, cmd.Run(), context.Canceled)
}

// TestCommand_RefusesANilContext pins the refusal, through Fatal.
func TestCommand_RefusesANilContext(t *testing.T) {
	t.Parallel()
	var none context.Context
	assert.PanicsWithValue(t, fatal("exectest.Command: nil context"), func() {
		exectest.Command(none, &fatalTB{TB: t}, nil, "x")
	})
}

// TestCommand_RefusesAnEmptyName pins the refusal, through Fatal.
func TestCommand_RefusesAnEmptyName(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t, fatal("exectest.Command: empty command name"), func() {
		exectest.Command(t.Context(), &fatalTB{TB: t}, nil, "")
	})
}
