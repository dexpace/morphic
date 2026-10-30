// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package decls

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// loadMode asks for what a reader of declarations needs: the package's files,
// their syntax and their type information. Dependencies come from export data,
// not source.
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
	packages.NeedTypes | packages.NeedTypesInfo

// goList configures a load for mode to run the go command. go/packages would
// otherwise hand it to the driver GOPACKAGESDRIVER names, or to one found on
// PATH, and a driver need not report what the refusals here read, such as the
// files a build leaves out.
func goList(mode packages.LoadMode) *packages.Config {
	return &packages.Config{Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
}

// Package is one package's production sources, type-checked in a load of its
// own: what it imports is its own copy, which Imported reaches.
type Package struct {
	// Types holds what the package declares.
	Types *types.Package
	// Info records what each identifier and expression in Syntax resolves to.
	Info *types.Info
	// Syntax holds the package's files, in the order of their names.
	Syntax []*ast.File
	// Fset positions Syntax and every object Types holds.
	Fset *token.FileSet
}

// Const is one string constant: its identifier and the value the compiler
// gives it.
type Const struct {
	Name  string
	Value string
}

// Load type-checks the one package pattern names, resolved from the working
// directory, without its test files. It refuses what LoadEach refuses, and a
// pattern matching several packages.
func Load(pattern string) (*Package, error) {
	pkgs, err := LoadEach(pattern)
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("load %s: matched %d packages, want 1", pattern, len(pkgs))
	}
	return pkgs[0], nil
}

// LoadEach type-checks every package the patterns name, without their test
// files, and returns each once, sorted by import path.
//
// Each is loaded alone, in a type universe of its own; Imported reaches the
// copy of a package another one imports. go/types can race when one
// go/packages load checks several packages sharing an import (golang/go#81122).
//
// It refuses to read less than asked: a pattern matching no package, a package
// that does not type-check or has no production Go file, and a production Go
// file the build leaves out, since what it declares would go unread.
func LoadEach(patterns ...string) ([]*Package, error) {
	if len(patterns) == 0 {
		return nil, errors.New("load: no pattern")
	}
	what := strings.Join(patterns, " ")
	paths, err := list(patterns)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", what, err)
	}

	var errs []error
	out := make([]*Package, 0, len(paths))
	for _, path := range paths {
		p, err := loadAlone(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("load %s: %w", what, errors.Join(errs...))
	}
	return out, nil
}

// list returns, sorted and once each, the import paths of the packages the
// patterns name. It asks the go command about each pattern on its own, since
// the go command only warns about one that names nothing, and what that
// pattern was meant to name would go unread.
func list(patterns []string) ([]string, error) {
	paths := map[string]bool{}
	var errs []error
	for _, pattern := range patterns {
		listed, err := packages.Load(goList(packages.NeedName), pattern)
		if err != nil {
			return nil, err
		}
		if len(listed) == 0 {
			errs = append(errs, fmt.Errorf("%s matched no packages", pattern))
		}
		for _, p := range listed {
			for _, e := range p.Errors {
				errs = append(errs, e)
			}
			paths[p.PkgPath] = true
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return slices.Sorted(maps.Keys(paths)), nil
}

// loadAlone type-checks the one package at path, as the only package of its
// go/packages load.
func loadAlone(path string) (*Package, error) {
	loaded, err := packages.Load(goList(loadMode), path)
	if err != nil {
		return nil, err
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("%s loaded as %d packages, want 1", path, len(loaded))
	}
	p := loaded[0]
	if errs := unread(p); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &Package{Types: p.Types, Info: p.TypesInfo, Syntax: p.Syntax, Fset: p.Fset}, nil
}

// unread returns what keeps p from being read whole: the errors the load
// recorded on it, a production Go file the build leaves out, or no production
// Go file at all.
func unread(p *packages.Package) []error {
	errs := make([]error, 0, len(p.Errors)+len(p.IgnoredFiles))
	for _, e := range p.Errors {
		errs = append(errs, e)
	}
	for _, file := range p.IgnoredFiles {
		// No build reads a test file, and a file in another language, such as
		// assembly, declares nothing the type checker sees.
		if filepath.Ext(file) != ".go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: the current build leaves out %s", p.PkgPath, filepath.Base(file)))
	}
	if len(p.Syntax) == 0 {
		errs = append(errs, fmt.Errorf("%s has no production Go file", p.PkgPath))
	}
	return errs
}

// Objects returns the package-level objects of kind T the package declares, in
// name order: Objects[*types.Const] its constants, Objects[types.Object] all.
func (p *Package) Objects[T types.Object]() iter.Seq[T] {
	scope := p.Types.Scope()
	return func(yield func(T) bool) {
		for _, name := range scope.Names() {
			obj, isT := scope.Lookup(name).(T)
			if isT && !yield(obj) {
				return
			}
		}
	}
}

// Imported returns the package p's types use for the import path path, whether
// p imports it itself or reaches it through another import. Since every
// Package is loaded on its own, a type in p is identical only to the copy of
// that package's types reached this way.
func (p *Package) Imported(path string) (*types.Package, bool) {
	seen := map[*types.Package]bool{p.Types: true}
	queue := []*types.Package{p.Types}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if pkg.Path() == path {
			return pkg, true
		}
		for _, imp := range pkg.Imports() {
			if !seen[imp] {
				seen[imp] = true
				queue = append(queue, imp)
			}
		}
	}
	return nil, false
}

// Named returns the named type the package declares as name, through an alias
// if name is one.
func (p *Package) Named(name string) (*types.Named, error) {
	tn, isType := p.Types.Scope().Lookup(name).(*types.TypeName)
	if !isType {
		return nil, fmt.Errorf("%s declares no type %s", p.Types.Path(), name)
	}
	named, isNamed := types.Unalias(tn.Type()).(*types.Named)
	if !isNamed {
		return nil, fmt.Errorf("%s.%s is not a named type", p.Types.Path(), name)
	}
	return named, nil
}

// StringConsts returns the package-level constants of the named type typeName,
// in declaration order: file by file in name order, each in source order. It
// fails if the package declares no such type, or if one of its constants is not
// a string.
func (p *Package) StringConsts(typeName string) ([]Const, error) {
	named, err := p.Named(typeName)
	if err != nil {
		return nil, err
	}
	var found []*types.Const
	for c := range p.Objects[*types.Const]() {
		if !types.Identical(c.Type(), named) {
			continue
		}
		if c.Val().Kind() != constant.String {
			return nil, fmt.Errorf("%s.%s has kind %s, not String", p.Types.Path(), c.Name(), c.Val().Kind())
		}
		found = append(found, c)
	}
	slices.SortFunc(found, p.declarationOrder)

	out := make([]Const, len(found))
	for i, c := range found {
		out[i] = Const{Name: c.Name(), Value: constant.StringVal(c.Val())}
	}
	return out, nil
}

// IntConst returns the value of the package-level constant name. It fails if
// the package declares no such constant, or if its value is not an integer an
// int64 holds.
func (p *Package) IntConst(name string) (int64, error) {
	c, isConst := p.Types.Scope().Lookup(name).(*types.Const)
	if !isConst {
		return 0, fmt.Errorf("%s declares no constant %s", p.Types.Path(), name)
	}
	if c.Val().Kind() != constant.Int {
		return 0, fmt.Errorf("%s.%s has kind %s, not Int", p.Types.Path(), name, c.Val().Kind())
	}
	v, exact := constant.Int64Val(c.Val())
	if !exact {
		return 0, fmt.Errorf("%s.%s is %s, past what an int64 holds", p.Types.Path(), name, c.Val())
	}
	return v, nil
}

// declarationOrder compares two constants by where the package declares them: by
// file name, then by offset in the file. A token.Pos would not do, because the
// loader parses files concurrently and a position orders files by whichever
// finished parsing first.
func (p *Package) declarationOrder(a, b *types.Const) int {
	pa, pb := p.Fset.Position(a.Pos()), p.Fset.Position(b.Pos())
	return cmp.Or(cmp.Compare(pa.Filename, pb.Filename), cmp.Compare(pa.Offset, pb.Offset))
}

// TypesDeclaring returns, sorted, the names of the package's types that
// declare the method name themselves. A type reaching it only by embedding is
// not one: a sealed sum's members are the types that declare its marker.
func (p *Package) TypesDeclaring(method string) []string {
	var out []string
	for tn := range p.Objects[*types.TypeName]() {
		named, isNamed := tn.Type().(*types.Named)
		if isNamed && declares(named, method) {
			out = append(out, tn.Name())
		}
	}
	return out
}

// FuncsUsing returns, sorted, the names of the package's functions and methods
// whose declarations name obj, in the receiver, the signature or the body. obj
// is compared as an object, so it must be the copy the package sees, as
// Imported reaches it.
func (p *Package) FuncsUsing(obj types.Object) []string {
	var out []string
	for _, f := range p.Syntax {
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if isFunc && p.names(fn, obj) {
				out = append(out, fn.Name.Name)
			}
		}
	}
	slices.Sort(out)
	return out
}

// names reports whether fn's declaration names obj anywhere in it.
func (p *Package) names(fn *ast.FuncDecl, obj types.Object) bool {
	for node := range ast.Preorder(fn) {
		ident, isIdent := node.(*ast.Ident)
		if isIdent && p.Info.Uses[ident] == obj {
			return true
		}
	}
	return false
}

// declares reports whether named declares the method itself.
func declares(named *types.Named, method string) bool {
	for m := range named.Methods() {
		if m.Name() == method {
			return true
		}
	}
	return false
}
