// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
