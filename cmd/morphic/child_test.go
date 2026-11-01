// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverDirEnv names the directory a coverage build writes its data to.
const coverDirEnv = "GOCOVERDIR"

// Coverage files are named by the runtime (internal/coverage, which no module
// may import): covmeta.<hash> once per binary, covcounters.<hash>.<pid>.<time>
// once per process. Each is written to a "tmp." name and renamed, so a file
// with either prefix is whole.
const (
	metaPrefix     = "covmeta."
	countersPrefix = "covcounters."
)

// command returns a command running name with args in a child that runs as
// the CLI, which childContext kills when the test ends. It is the one way
// these tests start a child, so none shares a coverage directory with another.
func command(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(childContext(t), name, args...)
	cmd.Env = append(childEnv(t), asMorphic+"=1")
	return cmd
}

// childEnv returns the environment of a child of this process. When this
// binary is built for coverage, the child gets a directory of its own, whose
// files the test's cleanup moves into this process's. Children sharing one
// directory race: the runtime names the temporary file it writes the
// meta-data to from the clock, and two children that read the same value, as
// a clock ticking in microseconds makes likely, create one file, so the
// second fails to rename it.
func childEnv(t *testing.T) []string {
	t.Helper()
	env := os.Environ()
	parent := os.Getenv(coverDirEnv)
	if parent == "" {
		return env
	}
	own := t.TempDir()
	t.Cleanup(func() { adoptCoverage(t, own, parent) })
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, coverDirEnv+"=") })
	return append(env, coverDirEnv+"="+own)
}

// adoptCoverage copies the coverage files a child left in from into to, so
// the profile counts what the child ran. A counter file is the child's alone.
// The meta-data file is the same for every child of one binary, so one that
// to holds is left alone: the copies are renamed whole, and a duplicate
// written by a child that raced this one replaces its twin unseen.
func adoptCoverage(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	require.NoError(t, err)
	for _, e := range entries {
		name := e.Name()
		isMeta := strings.HasPrefix(name, metaPrefix)
		if !isMeta && !strings.HasPrefix(name, countersPrefix) {
			continue
		}
		if isMeta && exists(t, filepath.Join(to, name)) {
			continue
		}
		copyWhole(t, filepath.Join(from, name), filepath.Join(to, name))
	}
}

// exists reports whether path names a file.
func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil {
		require.ErrorIs(t, err, fs.ErrNotExist)
	}
	return err == nil
}

// copyWhole copies the file src to dst through a temporary name beside dst,
// which no other copy picks, so that dst never holds part of it.
func copyWhole(t *testing.T, src, dst string) {
	t.Helper()
	raw, err := os.ReadFile(src)
	require.NoError(t, err)
	tmp, err := os.CreateTemp(filepath.Dir(dst), "tmp."+filepath.Base(dst)+".*")
	require.NoError(t, err)
	_, err = tmp.Write(raw)
	require.NoError(t, err)
	require.NoError(t, tmp.Close())
	require.NoError(t, os.Rename(tmp.Name(), dst))
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

// TestChild_CoverageBuildGivesEachChildItsOwnDir pins the mechanism behind the
// failure two covered children cause when they share a directory: each child
// of a coverage build writes to a directory of its own, not the parent's.
func TestChild_CoverageBuildGivesEachChildItsOwnDir(t *testing.T) {
	parent := t.TempDir()
	t.Setenv(coverDirEnv, parent)

	first := envValues(morphic(t, "--version").Env, coverDirEnv)
	second := envValues(morphic(t, "--version").Env, coverDirEnv)

	require.Len(t, first, 1, "one directory is named")
	require.Len(t, second, 1, "one directory is named")
	assert.NotEqual(t, parent, first[0], "the child does not write to the parent's directory")
	assert.NotEqual(t, first[0], second[0], "two children do not share a directory")
	assert.DirExists(t, first[0])
}

func TestChild_WithoutCoverageTheEnvironmentIsAsItWas(t *testing.T) {
	t.Setenv(coverDirEnv, "")
	require.NoError(t, os.Unsetenv(coverDirEnv))

	cmd := morphic(t, "--version")

	assert.Equal(t, append(os.Environ(), asMorphic+"=1"), cmd.Env)
}

// TestChild_AdoptsOnlyWhatACoverageRunWrote pins what moves from a child's
// directory into the parent's once the child's test ends: whole counter and
// meta-data files, and not a file still under its temporary name. A
// meta-data file the parent holds is not replaced.
func TestChild_AdoptsOnlyWhatACoverageRunWrote(t *testing.T) {
	parent := t.TempDir()
	t.Setenv(coverDirEnv, parent)
	child := func(files map[string]string) {
		t.Run("child", func(t *testing.T) {
			dirs := envValues(morphic(t, "--version").Env, coverDirEnv)
			require.Len(t, dirs, 1, "one directory is named")
			own := dirs[0]
			for name, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(own, name), []byte(content), 0o644))
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

// childStartsOutside returns, for each file of the package that builds an
// exec.Cmd anywhere but in command, the position of it, and whether command
// was among the declarations it read. It reports exec.Command and
// exec.CommandContext, an exec.Cmd literal, new(exec.Cmd) and a var of type
// exec.Cmd, but not a *exec.Cmd type. It finds the package through the file's
// import of os/exec, so an alias does not hide one, and reads package-level
// declarations as well as functions.
func childStartsOutside(t *testing.T, fset *token.FileSet, files []*ast.File) (starts []string, sawCommand bool) {
	t.Helper()
	for _, f := range files {
		name, imported := execName(f)
		if !imported {
			continue
		}
		require.NotEqual(t, ".", name, "%s dot-imports os/exec", fset.Position(f.Pos()).Filename)
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "command" {
				sawCommand = true
				continue
			}
			in := "package level"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				in = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if buildsCmd(n, name) {
					starts = append(starts, fmt.Sprintf("%s: %s starts a child outside command", fset.Position(n.Pos()), in))
				}
				return true
			})
		}
	}
	return starts, sawCommand
}

// buildsCmd reports whether n builds an exec.Cmd through the os/exec package
// named pkg: a call of Command or CommandContext, a literal of Cmd, a call of
// new(Cmd), or a var spec whose type is the bare Cmd.
func buildsCmd(n ast.Node, pkg string) bool {
	switch n := n.(type) {
	case *ast.SelectorExpr:
		return selects(n, pkg, "Command", "CommandContext")
	case *ast.CompositeLit:
		return selects(n.Type, pkg, "Cmd")
	case *ast.CallExpr:
		fn, ok := n.Fun.(*ast.Ident)
		return ok && fn.Name == "new" && len(n.Args) == 1 && selects(n.Args[0], pkg, "Cmd")
	case *ast.ValueSpec:
		return selects(n.Type, pkg, "Cmd")
	default:
		return false
	}
}

// selects reports whether e is pkg.Name for one of names.
func selects(e ast.Expr, pkg string, names ...string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || !slices.Contains(names, sel.Sel.Name) {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// execName returns the name f gives the os/exec package, and whether f
// imports it.
func execName(f *ast.File) (string, bool) {
	for _, spec := range f.Imports {
		if spec.Path.Value != `"os/exec"` {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name, true
		}
		return "exec", true
	}
	return "", false
}

// TestChild_EveryExecCmdIsBuiltInCommand holds every os/exec command these
// tests build to command, in any file of the package: a command built
// elsewhere starts a child that inherits the parent's coverage directory
// again.
func TestChild_EveryExecCmdIsBuiltInCommand(t *testing.T) {
	names, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, names, "the package's files are read")
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		files = append(files, f)
	}

	starts, sawCommand := childStartsOutside(t, fset, files)

	assert.True(t, sawCommand, "command is declared, so the check reaches what it holds")
	assert.Empty(t, starts)
}
