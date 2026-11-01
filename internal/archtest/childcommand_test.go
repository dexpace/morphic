// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// osExecImport is the import path of the package whose commands start a child.
const osExecImport = `"os/exec"`

// exectestFile is the one file allowed to build a command: exectest.Command
// itself, as walkGoFiles names it.
const exectestFile = "internal/exectest/exectest.go"

// The three things childCommandViolations reports, which the planted files'
// "// want", "// want dot" and "// want env" markers stand for.
const (
	buildsMessage = "builds a command outside exectest.Command"
	dotMessage    = "dot-imports os/exec, which hides the commands it builds"
	envMessage    = "assigns Env other than by append(cmd.Env, …), which hands the child the parent's coverage directory or drops its own"
)

// TestChildCommand_EveryTestBuildsItsCommandThroughExectest holds every Go file
// of the module, production files included, to exectest.Command for the
// commands it starts: a command built any other way starts a child that
// inherits the parent's coverage directory, where two children finishing
// together fail to write the meta-data file. It holds a site to extending
// cmd.Env with append(cmd.Env, …) alone, since resetting it from os.Environ
// hands the child that directory again. It also requires the walk to meet a
// file importing os/exec, so a walk that reaches none fails rather than
// passing on nothing.
func TestChildCommand_EveryTestBuildsItsCommandThroughExectest(t *testing.T) {
	t.Parallel()
	violations, importers, err := childCommandViolations(irtest.RepoRoot(t))
	require.NoError(t, err)
	assert.Positive(t, importers, "the walk reaches files importing os/exec")
	for _, v := range violations {
		t.Error(v)
	}
}

// TestChildCommandViolations_PlantedBuildsAreCaught proves the detector both
// ways on one planted file per row. A line ending in "// want" must be
// reported as building a command, one ending in "// want dot" as a dot
// import and one ending in "// want env" as resetting Env, exactly those lines
// and no other. wantSeen is the number of files
// importing os/exec the walk met.
func TestChildCommandViolations_PlantedBuildsAreCaught(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		file     string
		src      string
		wantSeen int
	}{
		{"call of Command", "p/a_test.go", execFile(`var _ = exec.Command("x") // want`), 1},
		{"call of CommandContext", "p/a_test.go", execFile(`var _ = exec.CommandContext(nil, "x") // want`), 1},
		{"literal", "p/a_test.go", execFile(`var _ = exec.Cmd{} // want`), 1},
		{"address of a literal", "p/a_test.go", execFile(`var _ = &exec.Cmd{} // want`), 1},
		{"new", "p/a_test.go", execFile(`var _ = new(exec.Cmd) // want`), 1},
		{"var of the type", "p/a_test.go", execFile(`var c exec.Cmd // want`), 1},
		{"slice of the type", "p/a_test.go", execFile(`var _ []exec.Cmd // want`), 1},
		{"make of a slice", "p/a_test.go", execFile(`var _ = make([]exec.Cmd, 1) // want`), 1},
		{"array of the type", "p/a_test.go", execFile(`var _ [2]exec.Cmd // want`), 1},
		{"field of the type", "p/a_test.go", execFile(`var _ struct{ c exec.Cmd } // want`), 1},
		{"alias of the type", "p/a_test.go", execFile(`type alias = exec.Cmd // want`), 1},
		{"import under another name", "p/a_test.go",
			"package p\n\nimport x \"os/exec\"\n\nvar _ = x.Command(\"x\") // want\n", 1},
		{"dot import", "p/a_test.go", "package p\n\nimport . \"os/exec\" // want dot\n", 1},
		{"pointer to the type", "p/a_test.go", execFile(`func f(c *exec.Cmd) *exec.Cmd { return c }`), 1},
		{"slice of pointers", "p/a_test.go", execFile(`var _ []*exec.Cmd`), 1},
		{"another name of the package", "p/a_test.go", execFile(`var _ exec.ExitError`), 1},
		{"another identifier selecting Command", "p/a_test.go",
			execFile("var o struct{ Command func() }\nvar _ = o.Command"), 1},
		{"file that is not a test", "p/a.go", execFile(`var _ = exec.Command("x") // want`), 1},
		{"the helper itself", exectestFile, execFile(`var _ = exec.Command("x")`), 0},
		{"another file of the helper's package", "internal/exectest/other.go",
			execFile(`var _ = exec.Command("x") // want`), 1},
		{"a file named like the helper elsewhere", "p/internal/exectest/exectest.go",
			execFile(`var _ = exec.Command("x") // want`), 1},
		{"file under testdata", "p/testdata/a_test.go", execFile(`var _ = exec.Command("x")`), 0},
		{"exec that is not os/exec", "p/a_test.go",
			"package p\n\nfunc f(exec fake) { exec.Command(\"x\") }\n", 0},
		{"Env reset from os.Environ", "p/a_test.go",
			envFile(`cmd.Env = append(os.Environ(), "X=1") // want env`), 1},
		{"Env replaced by a fresh list", "p/a_test.go",
			envFile(`cmd.Env = []string{"X=1"} // want env`), 1},
		{"Env cleared", "p/a_test.go", envFile(`cmd.Env = nil // want env`), 1},
		{"Env extended by append", "p/a_test.go", envFile(`cmd.Env = append(cmd.Env, "X=1")`), 1},
		{"Env extended by nested appends", "p/a_test.go",
			envFile(`cmd.Env = append(append(cmd.Env, "X=1"), "Y=2")`), 1},
		{"Env appended to another list", "p/a_test.go",
			envFile(`cmd.Env = append(other, "X=1") // want env`), 1},
		{"Env assigned beside another target", "p/a_test.go",
			envFile("var n int\n\tcmd.Env, n = nil, 1 // want env"), 1},
		{"Env of a file not importing os/exec", "p/a_test.go",
			"package p\n\nfunc f(cmd *struct{ Env []string }) {\n\tcmd.Env = []string{\"X=1\"} // want env\n}\n", 0},
		{"Env given in a literal", "p/a_test.go",
			"package p\n\nvar _ = struct{ Env []string }{Env: []string{\"X=1\"}}\n", 0},
		{"Env of the helper itself", exectestFile, envFile(`cmd.Env = os.Environ()`), 0},
		{"Env of another file of the helper's package", "internal/exectest/other.go",
			envFile(`cmd.Env = os.Environ() // want env`), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			plant := filepath.Join(root, filepath.FromSlash(tt.file))
			require.NoError(t, os.MkdirAll(filepath.Dir(plant), 0o755))
			require.NoError(t, os.WriteFile(plant, []byte(tt.src), 0o644))

			violations, seen, err := childCommandViolations(root)

			require.NoError(t, err)
			assert.Equal(t, tt.wantSeen, seen, "files importing os/exec")
			assert.Equal(t, wantedLines(tt.file, tt.src), violations)
		})
	}
}

// execFile returns a source file importing os/exec as exec, declaring body.
func execFile(body string) string {
	return "package p\n\nimport \"os/exec\"\n\n" + body + "\n"
}

// envFile returns a source file importing os/exec as exec, declaring a
// function whose parameter cmd is a command and whose body is body.
func envFile(body string) string {
	return execFile("func f(cmd *exec.Cmd, other []string) {\n\t" + body + "\n}")
}

// wantedLines returns the violation reported in file for each line of src
// that ends in a want marker, in line order.
func wantedLines(file, src string) []string {
	var want []string
	for i, line := range strings.Split(src, "\n") {
		switch {
		case strings.HasSuffix(line, "// want"):
			want = append(want, fmt.Sprintf("%s:%d: %s", file, i+1, buildsMessage))
		case strings.HasSuffix(line, "// want dot"):
			want = append(want, fmt.Sprintf("%s:%d: %s", file, i+1, dotMessage))
		case strings.HasSuffix(line, "// want env"):
			want = append(want, fmt.Sprintf("%s:%d: %s", file, i+1, envMessage))
		}
	}
	return want
}

// childCommandViolations walks root and returns one message per command a Go
// file builds any way but exectest.Command, with the number of files importing
// os/exec it met. Of those files, bar exectest's own, it reports a call of
// Command or CommandContext, and every selection of Cmd that is not the target
// of a pointer type: a literal, new, a var, a slice, an array, a field or an
// alias. A dot import is reported too. Of every file, it reports an assignment
// to an Env field unless it is append(cmd.Env, …), since a caller of a helper
// imports no os/exec.
func childCommandViolations(root string) (violations []string, importers int, err error) {
	err = walkGoFiles(root, func(rel string, fset *token.FileSet, f *ast.File) {
		if rel == exectestFile {
			return
		}
		violations = append(violations, envViolations(rel, fset, f)...)
		spec := osExecSpec(f)
		if spec == nil {
			return
		}
		importers++
		violations = append(violations, fileChildCommands(rel, fset, f, spec)...)
	})
	return violations, importers, err
}

// envViolations returns one message per assignment in f to a selection of Env
// that does not extend an Env with append, each naming rel and its line. A
// composite literal's Env key is not an assignment and is left alone.
func envViolations(rel string, fset *token.FileSet, f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			if !isEnvSelection(lhs) {
				continue
			}
			if len(assign.Lhs) == len(assign.Rhs) && extendsEnv(assign.Rhs[i]) {
				continue
			}
			out = append(out, fmt.Sprintf("%s:%d: %s", rel, fset.Position(lhs.Pos()).Line, envMessage))
		}
		return true
	})
	return out
}

// isEnvSelection reports whether e selects a field named Env.
func isEnvSelection(e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Env"
}

// extendsEnv reports whether e is append(x.Env, …), where the first argument
// may itself be such an append. The walk down the first arguments ends at the
// innermost call, which the syntax tree bounds.
func extendsEnv(e ast.Expr) bool {
	for {
		call, ok := ast.Unparen(e).(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return false
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok || fn.Name != "append" {
			return false
		}
		if isEnvSelection(call.Args[0]) {
			return true
		}
		e = call.Args[0]
	}
}

// osExecSpec returns f's import of os/exec, or nil when f has none.
func osExecSpec(f *ast.File) *ast.ImportSpec {
	for _, spec := range f.Imports {
		if spec.Path.Value == osExecImport {
			return spec
		}
	}
	return nil
}

// fileChildCommands returns the violations of f, which imports os/exec through
// spec, each naming rel and its line.
func fileChildCommands(rel string, fset *token.FileSet, f *ast.File, spec *ast.ImportSpec) []string {
	name := "exec"
	if spec.Name != nil {
		name = spec.Name.Name
	}
	switch name {
	case ".":
		return []string{fmt.Sprintf("%s:%d: %s", rel, fset.Position(spec.Pos()).Line, dotMessage)}
	case "_":
		return nil
	}

	pointed := map[*ast.SelectorExpr]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if star, ok := n.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok {
				pointed[sel] = true
			}
		}
		return true
	})

	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !buildsCommand(sel, name, pointed[sel]) {
			return true
		}
		out = append(out, fmt.Sprintf("%s:%d: %s", rel, fset.Position(sel.Pos()).Line, buildsMessage))
		return true
	})
	return out
}

// buildsCommand reports whether sel, taken from the os/exec package named pkg,
// builds a command: Command, CommandContext, or Cmd unless a pointer type
// names it.
func buildsCommand(sel *ast.SelectorExpr, pkg string, pointed bool) bool {
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != pkg {
		return false
	}
	switch sel.Sel.Name {
	case "Command", "CommandContext":
		return true
	case "Cmd":
		return !pointed
	default:
		return false
	}
}
