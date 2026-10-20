package lintrules

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// Module is the import path of the module the rules are written against.
const Module = "github.com/dexpace/morphic"

// The packages whose declarations the rules are about, by their real paths
// wherever the code under check sits.
const (
	irPath         = Module + "/ir"
	compilePath    = Module + "/compilers/compile"
	referencesPath = "github.com/speakeasy-api/openapi/references"
)

// Analyzers returns the rules, built afresh, with their scopes read against
// the module.
func Analyzers() []*analysis.Analyzer {
	return layout{root: Module}.analyzers()
}

// layout reads where a package sits from its import path relative to root:
// Module, or in the tests a tree laid out like it, which is how a plant reaches
// each scope's edges.
type layout struct{ root string }

func (l layout) analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{l.registryWrites(), l.fragmentReadings(), l.nameChannels(), l.idDerivations()}
}

// inModule reports whether the package at path is the root or below it.
func (l layout) inModule(path string) bool {
	return path == l.root || strings.HasPrefix(path, l.root+"/")
}

// within reports whether the package at path is dir, a path relative to the
// root, or below it. It cuts on segment boundaries, so irverify is not ir.
func (l layout) within(path, dir string) bool {
	rel, ok := strings.CutPrefix(path, l.root+"/")
	return ok && (rel == dir || strings.HasPrefix(rel, dir+"/"))
}

// framework reports whether the package at path is ir or compilers/compile, or
// below either: the packages that own the type registry and the grammars of
// names and IDs.
func (l layout) framework(path string) bool {
	return l.within(path, "ir") || l.within(path, "compilers/compile")
}

// productionFiles returns the files of the package under check that are not
// tests. A test builds the fixtures these rules refuse elsewhere, as it should.
func productionFiles(pass *analysis.Pass) []*ast.File {
	return slices.DeleteFunc(slices.Clone(pass.Files), func(f *ast.File) bool {
		return strings.HasSuffix(pass.Fset.File(f.Pos()).Name(), "_test.go")
	})
}

// typeNamed returns the type the package at path declares as name, as pkg sees
// it through any chain of its imports, or nil when pkg reaches no such type.
func typeNamed(pkg *types.Package, path, name string) *types.TypeName {
	p := imported(pkg, path)
	if p == nil {
		return nil
	}
	tn, _ := p.Scope().Lookup(name).(*types.TypeName)
	return tn
}

// imported returns the package at path as pkg sees it, or nil when pkg imports
// it through no chain at all. Each package is visited once.
func imported(pkg *types.Package, path string) *types.Package {
	seen := map[*types.Package]bool{pkg: true}
	queue := []*types.Package{pkg}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p.Path() == path {
			return p
		}
		for _, q := range p.Imports() {
			if !seen[q] {
				seen[q] = true
				queue = append(queue, q)
			}
		}
	}
	return nil
}

// satisfies reports whether t satisfies the constraint of the type parameter
// p, so that an instantiation can make p stand for t.
func satisfies(t types.Type, p *types.TypeParam) bool {
	iface, _ := p.Underlying().(*types.Interface) // a type parameter's always is
	return types.Satisfies(t, iface)
}

// frameworkCall reports whether e is a call of a function, not a method, that
// ir or compilers/compile declares.
func frameworkCall(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Signature().Recv() != nil {
		return false
	}
	return fn.Pkg().Path() == irPath || fn.Pkg().Path() == compilePath
}
