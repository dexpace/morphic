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

// The two things childCommandViolations reports, which the planted files'
// "// want" and "// want dot" markers stand for.
const (
	buildsMessage = "builds a command outside exectest.Command"
	dotMessage    = "dot-imports os/exec, which hides the commands it builds"
)

// TestChildCommand_EveryTestBuildsItsCommandThroughExectest holds every test
// file of the module to exectest.Command for the commands it starts: a command
// built any other way starts a child that inherits the parent's coverage
// directory, where two children finishing together fail to write the meta-data
// file. It also requires the walk to meet a test file importing os/exec, so a
// walk that reaches none fails rather than passing on nothing.
func TestChildCommand_EveryTestBuildsItsCommandThroughExectest(t *testing.T) {
	t.Parallel()
	violations, importers, err := childCommandViolations(irtest.RepoRoot(t))
	require.NoError(t, err)
	assert.Positive(t, importers, "the walk reaches test files importing os/exec")
	for _, v := range violations {
		t.Error(v)
	}
}

// TestChildCommandViolations_PlantedBuildsAreCaught proves the detector both
// ways on one planted file per row. A line ending in "// want" must be
// reported as building a command, and one ending in "// want dot" as a dot
// import, exactly those lines and no other. wantSeen is the number of test
// files importing os/exec the walk met.
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
		{"file that is not a test", "p/a.go", execFile(`var _ = exec.Command("x")`), 0},
		{"file under testdata", "p/testdata/a_test.go", execFile(`var _ = exec.Command("x")`), 0},
		{"exec that is not os/exec", "p/a_test.go",
			"package p\n\nfunc f(exec fake) { exec.Command(\"x\") }\n", 0},
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
			assert.Equal(t, tt.wantSeen, seen, "test files importing os/exec")
			assert.Equal(t, wantedLines(tt.file, tt.src), violations)
		})
	}
}

// execFile returns a source file importing os/exec as exec, declaring body.
func execFile(body string) string {
	return "package p\n\nimport \"os/exec\"\n\n" + body + "\n"
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
		}
	}
	return want
}

// childCommandViolations walks root for the test files that import os/exec and
// returns one message per command they build any way but exectest.Command, with
// the number of such files it met. It reports a call of Command or
// CommandContext, and every selection of Cmd that is not the target of a
// pointer type: a literal, new, a var, a slice, an array, a field or an alias.
// A dot import is reported too, since it hides what the file builds. It does
// not report os.StartProcess or a syscall, which no test uses.
func childCommandViolations(root string) (violations []string, importers int, err error) {
	err = walkGoFiles(root, func(rel string, fset *token.FileSet, f *ast.File) {
		if !strings.HasSuffix(rel, "_test.go") {
			return
		}
		spec := osExecSpec(f)
		if spec == nil {
			return
		}
		importers++
		violations = append(violations, fileChildCommands(rel, fset, f, spec)...)
	})
	return violations, importers, err
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
