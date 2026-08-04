package irverify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// deepDoc returns a document whose one example value nests past ir.MaxWalkDepth,
// so every walk over it is cut short. The model carries a well-formed name, so
// the naming check has something to reach before the value tree runs the walk
// out of budget.
func deepDoc() *ir.Document {
	v := ir.Value{Kind: ir.ValueNull}
	for range ir.MaxWalkDepth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	m := &ir.Model{TypeCommon: ir.TypeCommon{
		ID:       "t/x/M",
		Name:     ir.Naming{Source: "M", Canonical: "m"},
		Examples: []ir.Example{{Value: &v}},
	}}
	return &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
}

// TestWalkChecks_EachReportsTruncation drives the flag every walking check owes
// its caller. checkNaming used to discard it (GitHub #55), which was benign only
// because the unpruned reference walk reaches at least as far as every pruning
// one and so trips the cap first — a coincidence, not a guarantee, and one that
// hid every name past the cap the day a pruning walk went deeper.
func TestWalkChecks_EachReportsTruncation(t *testing.T) {
	t.Parallel()
	doc := deepDoc()
	for _, check := range walkChecks() {
		_, truncated := check(doc)
		assert.True(t, truncated,
			"%s walked a document nested past the cap without reporting it", checkName(check))
	}
}

// TestVerify_ReportsTruncationOnce holds the other half: five walks over one
// too-deep document are one fact about the document, so Verify states it once
// rather than once per walk that noticed it.
func TestVerify_ReportsTruncationOnce(t *testing.T) {
	t.Parallel()
	var truncations int
	for _, v := range Verify(deepDoc()) {
		if v.Code != "ir/walk-truncated" {
			continue
		}
		truncations++
		assert.Equal(t, "doc", v.Path, "truncation is a fact about the document, not about a node")
	}
	assert.Equal(t, 1, truncations)
}

// TestWalkChecks_NoWalkDropsItsTruncationFlag is the drift guard on the list
// itself, and it makes two claims that together leave a walk nowhere to drop the
// flag. A function that calls ir.WalkValues must hand a bool back to its caller,
// so the flag cannot die at the walk site; and a function shaped like a walking
// check must be in walkChecks, so it cannot be wired into Verify by a path that
// never folds the flag into a report.
//
// The two populations overlap without coinciding: checkReferentialIntegrity
// reaches the walk through collectRefs, so it is a check that does not itself
// call ir.WalkValues, and collectRefs is a walker that is no check. Each claim is
// therefore asked of the functions it applies to rather than of one list.
func TestWalkChecks_NoWalkDropsItsTruncationFlag(t *testing.T) {
	t.Parallel()
	listed := map[string]bool{}
	for _, check := range walkChecks() {
		listed[checkName(check)] = true
	}

	fns := packageFuncs(t)
	require.NotEmpty(t, fns, "the package must declare functions")
	var walkers int
	for _, fn := range fns {
		if fn.callsWalkValues {
			walkers++
			assert.Equal(t, "bool", last(fn.results),
				"%s calls ir.WalkValues but returns no truncation flag", fn.name)
		}
		if slices.Equal(fn.results, []string{"[]Violation", "bool"}) {
			assert.True(t, listed[fn.name],
				"%s is shaped like a walking check but is not in walkChecks, "+
					"so nothing folds its truncation flag into a report", fn.name)
		}
	}
	assert.Positive(t, walkers,
		"nothing reaches the document through ir.WalkValues, so the claim above proves nothing")
}

// checkName is a walkChecks entry's function name, without its package path.
func checkName(check func(*ir.Document) ([]Violation, bool)) string {
	full := runtime.FuncForPC(reflect.ValueOf(check).Pointer()).Name()
	return full[strings.LastIndex(full, ".")+1:]
}

// last returns the final element of ss, or "" when ss is empty.
func last(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[len(ss)-1]
}

// packageFunc is one function this package's production sources declare: its
// name, the types it returns, and whether its body reaches ir.WalkValues.
type packageFunc struct {
	name            string
	results         []string
	callsWalkValues bool
}

// packageFuncs returns every function declared in this package's production
// sources, methods included.
func packageFuncs(t *testing.T) []packageFunc {
	t.Helper()
	var out []packageFunc
	for _, path := range goSourceFiles(t, packageDir(t, ".")) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc {
				continue
			}
			out = append(out, packageFunc{
				name:            fn.Name.Name,
				results:         resultTypes(fn),
				callsWalkValues: callsWalkValues(fn),
			})
		}
	}
	return out
}

// resultTypes renders fn's result types, one entry per result, so a named and an
// unnamed result list read the same.
func resultTypes(fn *ast.FuncDecl) []string {
	if fn.Type.Results == nil {
		return nil
	}
	var out []string
	for _, field := range fn.Type.Results.List {
		out = append(out, slices.Repeat([]string{types.ExprString(field.Type)}, max(1, len(field.Names)))...)
	}
	return out
}

// callsWalkValues reports whether fn's body calls ir.WalkValues.
func callsWalkValues(fn *ast.FuncDecl) bool {
	var found bool
	ast.Inspect(fn, func(node ast.Node) bool {
		sel, isSelector := node.(*ast.SelectorExpr)
		if !isSelector || sel.Sel.Name != "WalkValues" {
			return true
		}
		pkg, isIdent := sel.X.(*ast.Ident)
		found = found || (isIdent && pkg.Name == "ir")
		return true
	})
	return found
}
