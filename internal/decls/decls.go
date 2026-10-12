package decls

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"
)

// loadMode asks for what a reader of declarations needs: the package's syntax
// and its type information. Dependencies come from export data, not source.
const loadMode = packages.NeedName | packages.NeedSyntax | packages.NeedTypes |
	packages.NeedTypesInfo | packages.NeedImports

// Package is one package's production sources, type-checked.
type Package struct {
	Types  *types.Package
	Info   *types.Info
	Syntax []*ast.File
	Fset   *token.FileSet
}

// Const is one string constant: its identifier and the value the compiler
// gives it.
type Const struct {
	Name  string
	Value string
}

// Load type-checks the one package pattern names, resolved from the working
// directory, without its test files. A pattern matching no package or several,
// or a package that does not type-check, is an error.
func Load(pattern string) (*Package, error) {
	loaded, err := packages.Load(&packages.Config{Mode: loadMode}, pattern)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", pattern, err)
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("load %s: matched %d packages, want 1", pattern, len(loaded))
	}
	p := loaded[0]
	if len(p.Errors) > 0 {
		errs := make([]error, len(p.Errors))
		for i, e := range p.Errors {
			errs[i] = e
		}
		return nil, fmt.Errorf("load %s: %w", pattern, errors.Join(errs...))
	}
	return &Package{Types: p.Types, Info: p.TypesInfo, Syntax: p.Syntax, Fset: p.Fset}, nil
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
// in declaration order. It fails if the package declares no such type, or if
// one of its constants is not a string.
func (p *Package) StringConsts(typeName string) ([]Const, error) {
	named, err := p.Named(typeName)
	if err != nil {
		return nil, err
	}
	scope := p.Types.Scope()
	var found []*types.Const
	for _, name := range scope.Names() {
		c, isConst := scope.Lookup(name).(*types.Const)
		if !isConst || !types.Identical(types.Unalias(c.Type()), named) {
			continue
		}
		if c.Val().Kind() != constant.String {
			return nil, fmt.Errorf("%s.%s is a %s constant, not a string", p.Types.Path(), name, c.Val().Kind())
		}
		found = append(found, c)
	}
	slices.SortFunc(found, func(a, b *types.Const) int { return cmp.Compare(a.Pos(), b.Pos()) })

	out := make([]Const, len(found))
	for i, c := range found {
		out[i] = Const{Name: c.Name(), Value: constant.StringVal(c.Val())}
	}
	return out, nil
}

// FuncDecl returns the declaration of the package-level function name.
func (p *Package) FuncDecl(name string) (*ast.FuncDecl, error) {
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			fd, isDecl := d.(*ast.FuncDecl)
			if isDecl && fd.Recv == nil && fd.Name.Name == name {
				return fd, nil
			}
		}
	}
	return nil, fmt.Errorf("%s declares no function %s", p.Types.Path(), name)
}
