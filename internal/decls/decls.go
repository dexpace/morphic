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

// Package is one package's production sources, type-checked.
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
// directory, without its test files. It refuses what LoadAll refuses, and a
// pattern matching several packages.
func Load(pattern string) (*Package, error) {
	pkgs, err := LoadAll(pattern)
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("load %s: matched %d packages, want 1", pattern, len(pkgs))
	}
	return pkgs[0], nil
}

// LoadAll type-checks every package the patterns name, together, without their
// test files, and returns them sorted by import path.
//
// Together is the point: a type one of them declares is identical to the type
// another sees through its import, which two Load calls cannot give, since each
// builds a universe of its own.
//
// It refuses to read less than asked: a pattern matching no package, a package
// that does not type-check or has no production Go file, and a production Go
// file the current build configuration leaves out, since what that file
// declares would go unread.
func LoadAll(patterns ...string) ([]*Package, error) {
	if len(patterns) == 0 {
		return nil, errors.New("load: no pattern")
	}
	what := strings.Join(patterns, " ")
	if err := eachMatches(patterns); err != nil {
		return nil, fmt.Errorf("load %s: %w", what, err)
	}

	loaded, err := packages.Load(goList(loadMode), patterns...)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", what, err)
	}
	if len(loaded) == 0 {
		return nil, fmt.Errorf("load %s: matched no packages", what)
	}

	var errs []error
	out := make([]*Package, 0, len(loaded))
	for _, p := range loaded {
		errs = append(errs, unread(p)...)
		out = append(out, &Package{Types: p.Types, Info: p.TypesInfo, Syntax: p.Syntax, Fset: p.Fset})
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("load %s: %w", what, errors.Join(errs...))
	}
	slices.SortFunc(out, func(a, b *Package) int { return cmp.Compare(a.Types.Path(), b.Types.Path()) })
	return out, nil
}

// eachMatches fails for a pattern of several that names no package. The go
// command only warns about one, so the load would quietly read less than its
// caller asked for. A lone pattern is not asked twice: an empty load says it.
func eachMatches(patterns []string) error {
	if len(patterns) == 1 {
		return nil
	}
	for _, pattern := range patterns {
		pkgs, err := packages.Load(goList(packages.NeedName), pattern)
		if err != nil {
			return err
		}
		if len(pkgs) == 0 {
			return fmt.Errorf("%s matched no packages", pattern)
		}
	}
	return nil
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
	if len(errs) == 0 && len(p.Syntax) == 0 {
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

// declares reports whether named declares the method itself.
func declares(named *types.Named, method string) bool {
	for m := range named.Methods() {
		if m.Name() == method {
			return true
		}
	}
	return false
}
