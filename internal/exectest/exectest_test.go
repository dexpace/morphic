// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package exectest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/exectest"
)

// coverDirEnv names the directory a coverage build writes its data to.
const coverDirEnv = "GOCOVERDIR"

// fatal is what a spyTB's Fatal panics with, standing in for the goroutine
// exit the real one ends the test with.
type fatal string

// spyTB records what Command asks of a test, so a test can run the cleanups
// itself and read what they reported. It embeds a real testing.TB for the rest.
type spyTB struct {
	testing.TB
	dir      string
	cleanups []func()
	errors   []string
}

func (f *spyTB) Helper()         {}
func (f *spyTB) TempDir() string { return f.dir }

func (f *spyTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

func (f *spyTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

func (f *spyTB) Fatal(args ...any) { panic(fatal(fmt.Sprint(args...))) }

// runCleanups runs the registered cleanups last in, first out, as a test does.
func (f *spyTB) runCleanups() {
	for _, fn := range slices.Backward(f.cleanups) {
		fn()
	}
}

// envValues returns the values env gives key, in order.
func envValues(env []string, key string) []string {
	var values []string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			values = append(values, v)
		}
	}
	return values
}

// namesIn returns the names of the entries of dir.
func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// readFile returns the content of the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

// TestCommand_CoverageBuildGivesEachChildItsOwnDir pins the mechanism behind
// the failure two covered children cause when they share a directory: each
// child of a coverage build writes to a directory of its own, not the parent's.
func TestCommand_CoverageBuildGivesEachChildItsOwnDir(t *testing.T) {
	parent := t.TempDir()
	t.Setenv(coverDirEnv, parent)

	first := envValues(exectest.Command(t.Context(), t, os.Args[0]).Env, coverDirEnv)
	second := envValues(exectest.Command(t.Context(), t, os.Args[0]).Env, coverDirEnv)

	require.Len(t, first, 1, "one directory is named")
	require.Len(t, second, 1, "one directory is named")
	assert.NotEqual(t, parent, first[0], "the child does not write to the parent's directory")
	assert.NotEqual(t, first[0], second[0], "two children do not share a directory")
	assert.DirExists(t, first[0])
}

func TestCommand_WithoutCoverageTheEnvironmentIsAsItWas(t *testing.T) {
	t.Setenv(coverDirEnv, "")
	require.NoError(t, os.Unsetenv(coverDirEnv))

	cmd := exectest.Command(t.Context(), t, os.Args[0])

	assert.Equal(t, os.Environ(), cmd.Env)
}

func TestCommand_RunsTheNamedCommandUnderTheContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cmd := exectest.Command(ctx, t, os.Args[0], "-test.run=^$", "-test.v")

	assert.Equal(t, []string{os.Args[0], "-test.run=^$", "-test.v"}, cmd.Args)
	assert.ErrorIs(t, cmd.Start(), context.Canceled, "the context governs the child")
}

func TestCommand_RefusesANilContext(t *testing.T) {
	t.Parallel()

	spy := &spyTB{TB: t, dir: t.TempDir()}
	var none context.Context

	assert.PanicsWithValue(t, fatal("exectest.Command: nil context"), func() {
		exectest.Command(none, spy, os.Args[0])
	})
}

func TestCommand_RefusesAnEmptyName(t *testing.T) {
	t.Parallel()

	spy := &spyTB{TB: t, dir: t.TempDir()}

	assert.PanicsWithValue(t, fatal("exectest.Command: empty command name"), func() {
		exectest.Command(t.Context(), spy, "")
	})
}

// TestCommand_AdoptsOnlyWhatACoverageRunWrote pins what moves from a child's
// directory into the parent's once the child's test ends: whole counter and
// meta-data files, and not a file still under its temporary name. A
// meta-data file the parent holds is not replaced.
func TestCommand_AdoptsOnlyWhatACoverageRunWrote(t *testing.T) {
	parent := t.TempDir()
	t.Setenv(coverDirEnv, parent)
	child := func(files map[string]string) {
		t.Run("child", func(t *testing.T) {
			dirs := envValues(exectest.Command(t.Context(), t, os.Args[0]).Env, coverDirEnv)
			require.Len(t, dirs, 1, "one directory is named")
			for name, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dirs[0], name), []byte(content), 0o644))
			}
		})
	}

	child(map[string]string{
		"covmeta.abc":             "meta",
		"covcounters.abc.1.1":     "first",
		"tmp.covcounters.abc.1.2": "partial",
		"other":                   "not coverage",
	})
	child(map[string]string{
		"covmeta.abc":         "another copy",
		"covcounters.abc.2.3": "second",
	})

	got := map[string]string{}
	for _, name := range namesIn(t, parent) {
		got[name] = readFile(t, filepath.Join(parent, name))
	}
	assert.Equal(t, map[string]string{
		"covmeta.abc":         "meta",
		"covcounters.abc.1.1": "first",
		"covcounters.abc.2.3": "second",
	}, got)
}

// TestCommand_ReportsWhatItCannotAdopt pins, through a test double, that the
// cleanup reports through Errorf each way the copy can fail, and that the
// fault is the one named.
func TestCommand_ReportsWhatItCannotAdopt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))

	tests := []struct {
		name   string
		parent func(t *testing.T) string
		plant  func(t *testing.T, own string)
		want   string
	}{
		{
			name:   "the child's directory is gone",
			parent: func(t *testing.T) string { return t.TempDir() },
			plant:  func(t *testing.T, own string) { require.NoError(t, os.RemoveAll(own)) },
			want:   "read the child's directory",
		},
		{
			name:   "the parent's copy cannot be looked for",
			parent: func(*testing.T) string { return filepath.Join(file, "dir") },
			plant:  plantFile("covmeta.abc"),
			want:   "look for the parent's copy",
		},
		{
			name:   "a counter file cannot be read",
			parent: func(t *testing.T) string { return t.TempDir() },
			plant: func(t *testing.T, own string) {
				require.NoError(t, os.Mkdir(filepath.Join(own, "covcounters.x"), 0o755))
			},
			want: "read the child's file",
		},
		{
			name:   "the parent's directory is missing",
			parent: func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") },
			plant:  plantFile("covcounters.abc.1.1"),
			want:   "copy the child's file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(coverDirEnv, tt.parent(t))
			spy := &spyTB{TB: t, dir: t.TempDir()}
			exectest.Command(t.Context(), spy, os.Args[0])
			tt.plant(t, spy.dir)

			spy.runCleanups()

			require.Len(t, spy.errors, 1)
			assert.Contains(t, spy.errors[0], tt.want)
		})
	}
}

// plantFile returns a plant that writes one empty file named name.
func plantFile(name string) func(t *testing.T, own string) {
	return func(t *testing.T, own string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(own, name), nil, 0o644))
	}
}
