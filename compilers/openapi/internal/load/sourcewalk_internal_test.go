package load

import (
	"go/ast"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	typed "github.com/dexpace/morphic/internal/decls"
)

// soaPath is the import path of the library's OpenAPI model.
const soaPath = "github.com/speakeasy-api/openapi/openapi"

// TestSourceWalks_GoThroughWalkSource holds every pass of this package that
// walks the source document to walkSource, which walks the fields beside each
// path item $ref with it. A pass calling soa.Walk on the document itself sees
// none of them, and nothing else says so: the parameter-repeat check landed that
// way and let a repeat written beside a $ref compile clean. modelSiblings is
// the one other caller, since it finds the fields walkSource walks.
//
// It reads the package type-checked, so a call is found however it is spelled.
func TestSourceWalks_GoThroughWalkSource(t *testing.T) {
	t.Parallel()
	pkg, err := typed.Load(".")
	require.NoError(t, err)

	require.Equal(t, []string{"modelSiblings", "walkSource"}, documentWalkers(t, pkg))
}

// documentWalkers returns the functions of pkg that call soa.Walk with the
// OpenAPI document as its start, sorted.
func documentWalkers(t *testing.T, pkg *typed.Package) []string {
	t.Helper()
	soa, ok := pkg.Imported(soaPath)
	require.True(t, ok, "the package reads the library's model")
	walk := soa.Scope().Lookup("Walk")
	document := types.NewPointer(soa.Scope().Lookup("OpenAPI").Type())
	require.NotNil(t, walk)

	var walkers []string
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || len(call.Args) != 2 || !callsObject(pkg, call.Fun, walk) {
					return true
				}
				if types.Identical(pkg.Info.TypeOf(call.Args[1]), document) {
					walkers = append(walkers, fn.Name.Name)
				}
				return true
			})
		}
	}
	slices.Sort(walkers)
	return slices.Compact(walkers)
}

// callsObject reports whether fun, a call's function expression, names obj.
func callsObject(pkg *typed.Package, fun ast.Expr, obj types.Object) bool {
	if index, generic := fun.(*ast.IndexExpr); generic {
		fun = index.X
	}
	var ident *ast.Ident
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		ident = f.Sel
	case *ast.Ident:
		ident = f
	default:
		return false
	}
	return pkg.Info.Uses[ident] == obj
}
