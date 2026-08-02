package archtest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maxMethodsPerType caps how many methods one type may carry inside compilers/.
//
// It is calibrated against the finished shape, which is why it lands after the
// restructuring rather than before it: the widest type in the tree is
// compile.Types at 14, so this leaves the largest legitimate type room to grow
// by half again. The type it exists for reached 159.
//
// A failure is a prompt to ask what the type has started doing, not to raise the
// number. Nothing arrives at 21 methods by adding one concern.
//
// It counts methods *declared* on a type, not the type's method set: a type that
// embeds another does not inherit its count. That is the dimension the failure
// actually grew in — 159 methods written across 11 files, one at a time — and it
// is what an AST rule can measure without a type checker. It also means the cap
// can be satisfied by embedding, which is the refactoring it exists to prompt
// rather than a way around it: two types each under the cap, each with a
// coherent set, is the outcome. A type reaching for that to stay under the
// number rather than to say something is visible in review in a way that one
// more method on one more struct never was.
const maxMethodsPerType = 20

// TestMethodsPerType_StayUnderTheCap measures the dimension the god object grew
// in, which nothing in force measured while it was growing.
//
// Function-size and complexity caps (#83) are worth having and were satisfied
// throughout — no function in that package ever came near the 70-line limit. The
// failure was type surface: one struct accumulated every lowering concern, one
// short method at a time, and every rule the repo enforced stayed green.
func TestMethodsPerType_StayUnderTheCap(t *testing.T) {
	t.Parallel()
	counts := methodCounts(t, filepath.Join(repoRoot(t), "compilers"))
	require.NotEmpty(t, counts, "the sweep found no methods at all, so an empty result proves nothing")

	var over []string
	for _, name := range sortedKeys(counts) {
		if counts[name] > maxMethodsPerType {
			over = append(over, fmt.Sprintf("%s has %d methods", name, counts[name]))
		}
	}
	assert.Empty(t, over, "no type under compilers/ may carry more than %d methods", maxMethodsPerType)
}

// TestMethodCounts_CountsOneTypePerPackage plants the shapes the counter has to
// tell apart, rather than trusting the live tree to contain them: a type past
// the cap is reported, two same-named types in different packages are counted
// separately, and pointer, value and generic receivers all count as the same
// type's methods.
//
// Planting is what makes the cap a check rather than a claim — the live tree has
// nothing near the limit, so a counter that always returned zero would pass the
// test above forever.
func TestMethodCounts_CountsOneTypePerPackage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMethods(t, filepath.Join(root, "wide", "wide.go"), "package wide", "Big", maxMethodsPerType+1)
	writeMethods(t, filepath.Join(root, "a", "a.go"), "package a", "Same", 3)
	writeMethods(t, filepath.Join(root, "b", "b.go"), "package b", "Same", 4)
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "gen.go"),
		[]byte("package a\n\ntype Same[T any] struct{}\n\n"+
			"func (s Same[T]) byValue() {}\n\nfunc (s *Same[T]) byPointer() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "a_test.go"),
		[]byte("package a\n\nfunc (s Same) inATestFile() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "embed.go"),
		[]byte("package a\n\ntype Host struct{ Same }\n\n"+
			"func (h Host) own1() {}\n\nfunc (h Host) own2() {}\n"), 0o600))

	counts := methodCounts(t, root)

	assert.Equal(t, maxMethodsPerType+1, counts["wide.Big"], "a type past the cap is counted whole")
	assert.Equal(t, 5, counts["a.Same"],
		"value, pointer and generic receivers are one type; a test file's method is not counted")
	assert.Equal(t, 4, counts["b.Same"], "a same-named type in another package is counted apart")
	assert.Equal(t, 2, counts["a.Host"],
		"an embedded type's methods belong to the type that declares them, not to the one embedding it")
}

// methodCounts maps "<dir>.<Type>" to the number of methods production files
// under root declare on it.
//
// The key carries the directory because a type name alone is not an identity: a
// walker named Scope in two packages is two types, and merging them would report
// a limit neither reached.
func methodCounts(t *testing.T, root string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipUninterestingDir(root, p, d)
		}
		if !isProductionGoFile(d.Name()) {
			return nil
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		dir := filepath.Base(filepath.Dir(p))
		for _, decl := range f.Decls {
			if name, ok := receiverType(decl); ok {
				counts[dir+"."+name]++
			}
		}
		return nil
	})
	require.NoError(t, err)
	return counts
}

// receiverType returns the name of the type decl is a method on.
//
// It unwraps a pointer receiver and a generic one, in that order, because both
// spellings declare a method on the same type and counting them apart would let
// a type split its surface across receiver forms and stay under any cap.
func receiverType(decl ast.Decl) (string, bool) {
	fn, isFunc := decl.(*ast.FuncDecl)
	if !isFunc || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return "", false
	}
	expr := fn.Recv.List[0].Type
	if star, isPtr := expr.(*ast.StarExpr); isPtr {
		expr = star.X
	}
	switch v := expr.(type) {
	case *ast.IndexExpr: // Same[T]
		expr = v.X
	case *ast.IndexListExpr: // Same[K, V]
		expr = v.X
	}
	id, isIdent := expr.(*ast.Ident)
	if !isIdent {
		return "", false
	}
	return id.Name, true
}

// sortedKeys returns m's keys in order, so a failure reads the same on every
// run.
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeMethods plants a package declaring typ with n methods on it.
func writeMethods(t *testing.T, file, pkg, typ string, n int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	src := pkg + "\n\ntype " + typ + " struct{}\n"
	for i := range n {
		src += fmt.Sprintf("\nfunc (x %s) m%d() {}\n", typ, i)
	}
	require.NoError(t, os.WriteFile(file, []byte(src), 0o600))
}
