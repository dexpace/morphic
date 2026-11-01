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
	envMessage    = "assigns Env other than by append(cmd.Env, …) without os.Environ, which hands the child the parent's coverage directory or drops its own"
)

// TestChildCommand_EveryFileStartsItsChildThroughExectest holds every Go file
// of the module, production files included, to exectest.Command for the
// commands it starts: a command built any other way starts a child that
// inherits the parent's coverage directory, where two children finishing
// together fail to write the meta-data file. It holds a site to extending
// cmd.Env with append(cmd.Env, …) alone, since resetting it from os.Environ
// hands the child that directory again. It also requires the walk to meet a
// file importing os/exec, so a walk that reaches none fails rather than
// passing on nothing.
func TestChildCommand_EveryFileStartsItsChildThroughExectest(t *testing.T) {
	t.Parallel()
	violations, importers, err := childCommandViolations(irtest.RepoRoot(t))
	require.NoError(t, err)
	assert.Positive(t, importers, "the walk reaches files importing os/exec")
	for _, v := range violations {
		t.Error(v)
	}
}

// TestChildCommandViolations_PlantedRowsAreReportedExactly proves the detector both
// ways on one planted file per row. A line ending in "// want" must be
// reported as building a command, one ending in "// want dot" as a dot
// import and one ending in "// want env" as resetting Env, exactly those lines
// and no other. wantSeen is the number of files
// importing os/exec the walk met.
func TestChildCommandViolations_PlantedRowsAreReportedExactly(t *testing.T) {
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
		{"parenthesized pointer target", "p/a_test.go", execFile(`func f(c *(exec.Cmd)) {}`), 1},
		{"slice of pointers", "p/a_test.go", execFile(`var _ []*exec.Cmd`), 1},
		{"elided literal in a slice of pointers", "p/a_test.go",
			execFile(`var _ = []*exec.Cmd{{Path: "x"}} // want`), 1},
		{"elided literal as a map value", "p/a_test.go",
			execFile(`var _ = map[string]*exec.Cmd{"a": {Path: "x"}} // want`), 1},
		{"elided literal as a map key", "p/a_test.go",
			execFile(`var _ = map[*exec.Cmd]int{{Path: "x"}: 1} // want`), 1},
		{"elided literal in nested slices", "p/a_test.go",
			execFile(`var _ = [][]*exec.Cmd{{{Path: "x"}}} // want`), 1},
		{"elided literal in an array of pointers", "p/a_test.go",
			execFile(`var _ = [...]*exec.Cmd{{Path: "x"}} // want`), 1},
		{"elided literal at a slice index", "p/a_test.go",
			execFile(`var _ = []*exec.Cmd{0: {Path: "x"}} // want`), 1},
		{"elided literal beneath a pointer to a slice", "p/a_test.go",
			execFile(`var _ = []*[]*exec.Cmd{{{Path: "x"}}} // want`), 1},
		{"elided literal in a slice of values", "p/a_test.go",
			execFile(`var _ = []exec.Cmd{{Path: "x"}} // want`), 1},
		{"elided literal of a parenthesized element type", "p/a_test.go",
			execFile(`var _ = [](*exec.Cmd){{Path: "x"}} // want`), 1},
		{"elided literal beneath a pointer to a parenthesized slice", "p/a_test.go",
			execFile(`var _ = []*([]*exec.Cmd){{{Path: "x"}}} // want`), 1},
		{"elided literal of another type", "p/a_test.go",
			execFile("type t struct{ Path string }\nvar _ = []*t{{Path: \"x\"}}\nvar _ exec.ExitError"), 1},
		{"elided literals of another type in nested containers", "p/a_test.go",
			execFile("type t struct{ Path string }\nvar _ = map[t][]*t{{Path: \"x\"}: {{Path: \"y\"}}}\nvar _ exec.ExitError"), 1},
		{"elided literal of another package's Cmd", "p/a_test.go",
			"package p\n\nimport (\n\t\"os/exec\"\n\tother \"example.com/other\"\n)\n\n" +
				"var _ = []*other.Cmd{{}}\nvar _ exec.ExitError\n", 1},
		{"elided literal under a typed inner literal", "p/a_test.go",
			execFile(`var _ = [][]*exec.Cmd{[]*exec.Cmd{{}}} // want`), 1},
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
		{"exec imported from another path", "p/a_test.go",
			"package p\n\nimport exec \"example.com/exec\"\n\nvar _ = exec.Command(\"x\")\n", 0},
		{"elided literal of another type of the package", "p/a_test.go",
			execFile(`var _ = []*exec.ExitError{{}}`), 1},
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
		{"Env passed through another function", "p/a_test.go",
			envFile(`cmd.Env = clip(cmd.Env) // want env`), 1},
		{"Env assigned beside another target", "p/a_test.go",
			envFile("var n int\n\tcmd.Env, n = nil, 1 // want env"), 1},
		{"Env extended by the parent's environment", "p/a_test.go",
			envFile(`cmd.Env = append(cmd.Env, os.Environ()...) // want env`), 1},
		{"Env extended by part of the parent's environment", "p/a_test.go",
			envFile(`cmd.Env = append(cmd.Env, os.Environ()[1:]...) // want env`), 1},
		{"Env assigned from a call returning two values", "p/a_test.go",
			envFile("var n int\n\tn, cmd.Env = g() // want env"), 1},
		{"Env target in parentheses", "p/a_test.go", envFile(`(cmd.Env) = nil // want env`), 1},
		{"Env extended by append in parentheses", "p/a_test.go",
			envFile(`cmd.Env = (append((cmd.Env), "X=1"))`), 1},
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
// os/exec it met; exectest's own file is exempt. It reads syntax, not types,
// so it sees a command only where a statement names exec's Command,
// CommandContext or Cmd, and an Env only where one assigns a selection of it.
// A copy (c := *cmd) selects no Cmd, and a write through a pointer
// (p := &cmd.Env; *p = …) no Env, so both pass, as do os.StartProcess and
// the syscall starters.
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
// that does not extend an Env with append, or that calls Environ, each naming
// rel and its line. A composite literal's Env key is not an assignment and is
// left alone.
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
			if len(assign.Lhs) == len(assign.Rhs) && extendsEnv(assign.Rhs[i]) && !readsEnviron(assign.Rhs[i]) {
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
// innermost call, which the syntax tree bounds. It does not follow x to the
// command assigned, so append(other.Env, …) passes, nor a variable holding
// os.Environ(), nor an appended value that names GOCOVERDIR itself.
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

// readsEnviron reports whether e calls Environ anywhere, as os.Environ() does.
// os/exec keeps the last of a key given twice, so an append that adds the
// parent's environment after the child's puts the parent's coverage directory
// back.
func readsEnviron(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Environ" {
			found = true
		}
		return !found
	})
	return found
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
			if sel, ok := ast.Unparen(star.X).(*ast.SelectorExpr); ok {
				pointed[sel] = true
			}
		}
		return true
	})

	var out []string
	report := func(pos token.Pos) {
		out = append(out, fmt.Sprintf("%s:%d: %s", rel, fset.Position(pos).Line, buildsMessage))
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if buildsCommand(n, name, pointed[n]) {
				report(n.Pos())
			}
		case *ast.CompositeLit:
			for _, pos := range elidedCommands(n, name) {
				report(pos)
			}
		default:
			// Nothing else names a command.
		}
		return true
	})
	return out
}

// elidedCommands returns the position of each literal elided beneath lit that
// builds a pointer to Cmd from pkg, as {Path: "x"} does in []*exec.Cmd{{...}}.
// The type is carried through slice and array elements and through map keys and
// values, by a work list the syntax tree bounds. Only lit's own type names it,
// so a type name is not followed: not a named container, an alias
// (type alias = *exec.Cmd) or a defined pointer (type P *exec.Cmd). An elided
// literal of a value Cmd is not returned: its selection is reported where it
// stands.
func elidedCommands(lit *ast.CompositeLit, pkg string) []token.Pos {
	type carried struct {
		lit *ast.CompositeLit
		typ ast.Expr
	}
	var out []token.Pos
	var work []carried
	if lit.Type != nil {
		work = append(work, carried{lit, lit.Type})
	}
	// visit takes an element e that the container's type gives typ.
	visit := func(e, typ ast.Expr) {
		inner, ok := e.(*ast.CompositeLit)
		if !ok || inner.Type != nil || typ == nil {
			return
		}
		typ = ast.Unparen(typ)
		if star, ok := typ.(*ast.StarExpr); ok {
			if isCommandType(star.X, pkg) {
				out = append(out, inner.Lbrace)
				return
			}
			typ = star.X
		}
		work = append(work, carried{inner, typ})
	}
	for len(work) > 0 {
		next := work[len(work)-1]
		work = work[:len(work)-1]
		key, elem := containerTypes(next.typ)
		for _, elt := range next.lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				visit(kv.Key, key)
				elt = kv.Value
			}
			visit(elt, elem)
		}
	}
	return out
}

// containerTypes returns the type of the keys and of the elements of a slice,
// array or map type, and nil for what is not one. A slice or array has no key
// type, since its keys are indexes.
func containerTypes(typ ast.Expr) (key, elem ast.Expr) {
	switch t := ast.Unparen(typ).(type) {
	case *ast.ArrayType:
		return nil, t.Elt
	case *ast.MapType:
		return t.Key, t.Value
	default:
		return nil, nil
	}
}

// isCommandType reports whether e is the type Cmd of the os/exec package named
// pkg.
func isCommandType(e ast.Expr, pkg string) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Cmd" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
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
