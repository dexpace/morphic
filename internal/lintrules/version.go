// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lintrules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// versionReadings refuses, outside compilers/openapi/internal/load and
// compilers/openapi/internal/oas, every reading of the OpenAPI version a
// document declares: the parser library's OpenAPI field and GetOpenAPI method
// of its document type, oas.ParseMinor and load.SupportedMinor. How a version
// reads is a row of oas's table, which a lowering asks through
// lowering.Ctx.SchemaRules, so a lowering that asks the version states a fact
// a second time (docs/compilers/openapi.md#later-version-construct).
//
// A use is whatever an identifier resolves to, as fragmentReadings reads one.
// A field or method of another type is none, whatever its name.
func (l layout) versionReadings() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "versionreadings",
		Doc:  "refuse reading the OpenAPI version a document declares outside load and oas",
		Run:  l.runVersionReadings,
	}
}

func (l layout) runVersionReadings(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !l.inModule(path) || path == l.loadPath() || path == l.oasPath() {
		return nil, nil
	}
	field := versionField(pass.Pkg)
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			id, ok := n.(*ast.Ident)
			if !ok {
				continue
			}
			if what := l.versionReading(pass.TypesInfo.Uses[id], field); what != "" {
				pass.Reportf(id.Pos(), "%s reads the OpenAPI version a document declares outside load and oas; "+
					"ask the rule table, through lowering.Ctx.SchemaRules", what)
			}
		}
	}
	return nil, nil
}

// loadPath and oasPath are where the two packages that may read the version
// sit.
func (l layout) loadPath() string { return l.root + "/compilers/openapi/internal/load" }
func (l layout) oasPath() string  { return l.root + "/compilers/openapi/internal/oas" }

// versionField returns the parser library's document field holding the version,
// as pkg sees it, or nil when pkg reaches no such type. The library names other
// fields OpenAPI too, a walk's among them, so the field is the one its document
// type declares, not one of the name.
func versionField(pkg *types.Package) *types.Var {
	tn := typeNamed(pkg, soaPath, "OpenAPI")
	if tn == nil {
		return nil
	}
	s, ok := tn.Type().Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for f := range s.Fields() {
		if f.Name() == "OpenAPI" {
			return f
		}
	}
	return nil
}

// versionReading names obj when it is one of the readings, field being the
// library's version field, or returns "" for anything else. Each function or
// method is the one of its name its package declares, so the package and the
// name tell it.
func (l layout) versionReading(obj types.Object, field *types.Var) string {
	if v, ok := obj.(*types.Var); ok && field != nil && v == field {
		return "soa.OpenAPI.OpenAPI"
	}
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return "" // not a function, or a method of a universe type such as error
	}
	switch path, name := fn.Pkg().Path(), fn.Name(); {
	case path == soaPath && name == "GetOpenAPI":
		return "soa.OpenAPI.GetOpenAPI"
	case path == l.oasPath() && name == "ParseMinor":
		return "oas.ParseMinor"
	case path == l.loadPath() && name == "SupportedMinor":
		return "load.SupportedMinor"
	default:
		return ""
	}
}
