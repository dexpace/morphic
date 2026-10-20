package lintrules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// fragmentReadings refuses, outside compilers/openapi/internal/fragment, the
// two readings of a $ref fragment fragment.Pointer replaces (GitHub #911):
// url.QueryUnescape, and the library's references.Reference.GetJSONPointer,
// which is built on it and reads '+' as a space where RFC 3986 reads a plus. A
// reader of its own parts company with every other on the first '+'.
//
// A use is whatever an identifier resolves to: a call, a method value, a method
// expression, or a method promoted through any depth of embedding. A function
// or method of another package is not one, whatever its name or signature.
func (l layout) fragmentReadings() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "fragmentreadings",
		Doc:  "refuse reading a $ref fragment otherwise than fragment.Pointer does",
		Run:  l.runFragmentReadings,
	}
}

func (l layout) runFragmentReadings(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !l.inModule(path) || path == l.root+"/compilers/openapi/internal/fragment" {
		return nil, nil
	}
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			id, ok := n.(*ast.Ident)
			if !ok {
				continue
			}
			if what := fragmentReading(pass.TypesInfo.Uses[id]); what != "" {
				pass.Reportf(id.Pos(), "%s reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer", what)
			}
		}
	}
	return nil, nil
}

// fragmentReading names obj when it is one of the two readings, or returns ""
// for anything else. Each is the one function or method of its name its
// package declares, so the package and the name tell it. A call through an
// interface resolves to the interface's method, which is out of sight.
func fragmentReading(obj types.Object) string {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return "" // not a function, or a method of a universe type such as error
	}
	switch {
	case fn.Pkg().Path() == "net/url" && fn.Name() == "QueryUnescape":
		return "url.QueryUnescape"
	case fn.Pkg().Path() == referencesPath && fn.Name() == "GetJSONPointer":
		return "references.Reference.GetJSONPointer"
	default:
		return ""
	}
}
