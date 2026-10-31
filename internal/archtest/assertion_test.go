// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestInterfaceAssertions_CoverEveryExportedPair holds each exported type that
// implements an exported interface its package declares or imports from the
// module to a package-level var of that interface's type or of one whose
// methods include its, by convention var _, as departures.md's
// interface-assertions-stay-narrow asks. A grep lists the
// assertions present; only the type checker shows one missing.
func TestInterfaceAssertions_CoverEveryExportedPair(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	root := irtest.RepoRoot(t)

	pairs := 0
	for _, p := range pkgs {
		violations, n := missingAssertions(p, root)
		pairs += n
		for _, v := range violations {
			t.Error(v)
		}
	}
	require.Positive(t, pairs, "no exported type implements an exported interface, so nothing was checked")
}

// TestMissingAssertions_PlantedShapesAreCaught plants each pair the rule holds
// and each it leaves alone into a package of the module, so the test above
// cannot pass by recognizing nothing. The package's own pairs are asserted, so
// what is reported is the plant's.
func TestMissingAssertions_PlantedShapesAreCaught(t *testing.T) {
	t.Parallel()
	const head = "package pass\n\nimport (\n\t\"reflect\"\n\n\t\"github.com/dexpace/morphic/ir\"\n)\n\n"
	const tail = "\nvar (\n\t_ reflect.Value\n\t_ ir.WalkPath\n)\n" // so every plant uses both imports
	const visit = "Visit(reflect.Value, ir.WalkPath) bool { return true }\n"
	const file = "zz_planted_assertion.go"
	const line = 9 // of the planted type, after head
	tests := []struct {
		name string
		src  string
		want []string // the interfaces reported for the type on line
	}{
		{"an imported interface, unasserted", "type P struct{}\n\nfunc (P) " + visit, []string{"ir.Visitor"}},
		{"through a pointer receiver, unasserted", "type P struct{}\n\nfunc (*P) " + visit, []string{"ir.Visitor"}},
		{"its own package's interface, unasserted",
			"type P struct{}\n\ntype Planter interface{ Plant() }\n\nfunc (P) Plant() {}\n",
			[]string{"pass.Planter"}},
		{"a var of a type that is no interface",
			"type P struct{}\n\nfunc (P) " + visit + "\nvar _ P = P{}\n", []string{"ir.Visitor"}},
		{"a var with no type is no assertion",
			"type P struct{}\n\nfunc (P) " + visit + "\nvar _ = P{}\n", []string{"ir.Visitor"}},
		{"another interface asserted",
			"type P struct{}\n\ntype Planter interface{ Plant() }\n\nfunc (P) Plant() {}\n\nfunc (P) " + visit +
				"\nvar _ Planter = P{}\n", []string{"ir.Visitor"}},
		{"asserted", "type P struct{}\n\nfunc (P) " + visit + "\nvar _ ir.Visitor = P{}\n", nil},
		{"asserted by a conversion", "type P struct{}\n\nfunc (P) " + visit + "\nvar _ = ir.Visitor(P{})\n", nil},
		{"asserted by a parenthesized conversion of a pointer", "type P struct{}\n\nfunc (*P) " + visit +
			"\nvar _ = ((ir.Visitor)((*P)(nil)))\n", nil},
		{"asserted by a conversion beside a type", "type P struct{}\n\nfunc (P) " + visit +
			"\nfunc (P) Cycle(_, _ ir.WalkPath, _ reflect.Type) {}\n\nvar _ ir.Visitor = ir.CycleVisitor(P{})\n", nil},
		{"a conversion to a type that is no interface", "type P struct{}\n\ntype q P\n\nfunc (P) " + visit +
			"\nvar _ = q(P{})\n", []string{"ir.Visitor"}},
		{"a call that converts nothing", "type P struct{}\n\nfunc (P) " + visit +
			"\nfunc wrap(P) ir.Visitor { return nil }\n\nvar _ = wrap(P{})\n", []string{"ir.Visitor"}},
		{"a builtin's call", "type P struct{}\n\nfunc (P) " + visit + "\nvar _ = new(P)\n", []string{"ir.Visitor"}},
		{"asserted through a pointer", "type P struct{}\n\nfunc (*P) " + visit + "\nvar _ ir.Visitor = (*P)(nil)\n", nil},
		{"asserted in a group",
			"type P struct{}\n\nfunc (P) " + visit + "\nvar (\n\t_ ir.Visitor = P{}\n)\n", nil},
		{"an unexported type", "type planted struct{}\n\nfunc (planted) " + visit, nil},
		{"a standard library interface alone", "type P struct{}\n\nfunc (P) String() string { return \"\" }\n", nil},
		{"an alias of another package's type", "type P = ir.VisitFunc\n", nil},
		{"asserted through an embedding interface", "type P struct{}\n\nfunc (P) " + visit +
			"\nfunc (P) Cycle(_, _ ir.WalkPath, _ reflect.Type) {}\n\nvar _ ir.CycleVisitor = P{}\n", nil},
		{"asserted through an alias of the interface", "type P struct{}\n\ntype v = ir.Visitor\n\nfunc (P) " + visit +
			"\nvar _ v = P{}\n", nil},
		{"asserted through an alias of the type", "type P struct{}\n\ntype q = P\n\nfunc (P) " + visit +
			"\nvar _ ir.Visitor = q{}\n", nil},
		{"asserted through an alias of a pointer", "type P struct{}\n\ntype q = *P\n\nfunc (*P) " + visit +
			"\nvar _ ir.Visitor = q(nil)\n", nil},
		{"asserted second in one spec", "type P struct{}\n\ntype q struct{}\n\nfunc (P) " + visit + "\nfunc (q) " + visit +
			"\nvar _, _ ir.Visitor = q{}, P{}\n", nil},
		{"a generic type", "type P[T any] struct{}\n\nfunc (P[T]) " + visit, nil},
		{"a generic interface", "type P struct{}\n\ntype Planter[T any] interface{ Plant() }\n\nfunc (P) Plant() {}\n", nil},
		{"a constraint interface",
			"type P struct{}\n\ntype Planter interface {\n\tcomparable\n\tPlant()\n}\n\nfunc (P) Plant() {}\n", nil},
		{"an empty interface", "type P struct{}\n\ntype Planter interface{}\n", nil},
		{"an unexported interface", "type P struct{}\n\ntype planter interface{ Plant() }\n\nfunc (P) Plant() {}\n", nil},
		{"an alias of an interface", "type P struct{}\n\ntype V = ir.Visitor\n\nfunc (P) " + visit, []string{"ir.Visitor"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := loadOverlaid(t, "pass", file, head+tc.src+tail)
			got, _ := missingAssertions(p, irtest.RepoRoot(t))
			var want []string
			for _, iface := range tc.want {
				want = append(want, fmt.Sprintf("pass/%s:%d: pass.P implements %s with no compile-time assertion in its package",
					file, line, iface))
			}
			assert.Empty(t, cmp.Diff(want, got))
		})
	}
}

// missingAssertions returns a line for each exported type p declares that
// implements an exported interface p or a package p imports declares in the
// module, with no var in p asserting it, and how many such pairs it found. An
// interface elsewhere p could not assert without a new import. A generic type
// or interface is left out, as only an instantiation of one implements anything.
func missingAssertions(p *decls.Package, root string) ([]string, int) {
	ifaces := moduleInterfaces(p)
	asserted := assertedInterfaces(p)
	var violations []string
	pairs := 0
	for tn := range p.Objects[*types.TypeName]() {
		if !tn.Exported() || tn.IsAlias() || isGeneric(tn) {
			continue
		}
		for _, iface := range ifaces {
			// *T's method set holds T's, so this covers both receivers, and a
			// pointer to an interface has no methods, so no interface is held.
			if !types.Implements(types.NewPointer(tn.Type()), iface.it) {
				continue
			}
			pairs++
			// An interface implements itself and each one it embeds, so
			// asserting an embedding interface asserts this one too.
			if !slices.ContainsFunc(asserted[tn], func(a *types.Interface) bool { return types.Implements(a, iface.it) }) {
				violations = append(violations, fmt.Sprintf("%s: %s implements %s with no compile-time assertion in its package",
					positions([]token.Position{p.Fset.Position(tn.Pos())}, root), qualified(tn), qualified(iface.tn)))
			}
		}
	}
	slices.Sort(violations)
	return violations, pairs
}

// moduleInterface is an interface a module package exports: its declaration
// and its method set.
type moduleInterface struct {
	tn *types.TypeName
	it *types.Interface
}

// moduleInterfaces returns the exported, non-generic interfaces with methods
// that p and each module package p imports declare, as p's load sees them.
func moduleInterfaces(p *decls.Package) []moduleInterface {
	var out []moduleInterface
	for _, pkg := range append([]*types.Package{p.Types}, p.Types.Imports()...) {
		if pkg.Path() != module && !strings.HasPrefix(pkg.Path(), module+"/") {
			continue
		}
		for _, name := range pkg.Scope().Names() {
			tn, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || !tn.Exported() || tn.IsAlias() || isGeneric(tn) {
				continue
			}
			if it, ok := tn.Type().Underlying().(*types.Interface); ok && it.IsMethodSet() && it.NumMethods() > 0 {
				out = append(out, moduleInterface{tn, it})
			}
		}
	}
	return out
}

// assertedInterfaces returns, for each named type a package-level var in p
// holds as an interface, the interfaces such vars give it: a conversion's, as
// in var _ = I(T{}), or else the var's declared type's. The value's type is
// read with any pointer and alias taken off. A named var asserts as var _
// does.
func assertedInterfaces(p *decls.Package) map[*types.TypeName][]*types.Interface {
	out := map[*types.TypeName][]*types.Interface{}
	for _, f := range p.Syntax {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, value := range vs.Values {
					if tn, iface, ok := assertion(p, vs.Type, value); ok {
						out[tn] = append(out[tn], iface)
					}
				}
			}
		}
	}
	return out
}

// assertion returns the named type value holds and the interface it is held
// as, by a conversion to an interface or else by typ, the var's declared type,
// which is nil when the var declares none. A call whose callee has an
// interface type is a conversion, as no interface value can be called.
func assertion(p *decls.Package, typ, value ast.Expr) (*types.TypeName, *types.Interface, bool) {
	if call, ok := ast.Unparen(value).(*ast.CallExpr); ok && len(call.Args) == 1 {
		if iface, ok := p.Info.TypeOf(call.Fun).Underlying().(*types.Interface); ok {
			tn, ok := typeName(p.Info.TypeOf(call.Args[0]))
			if !ok {
				return nil, nil, false
			}
			return tn, iface, true
		}
	}
	if typ == nil {
		return nil, nil, false
	}
	iface, ok := p.Info.TypeOf(typ).Underlying().(*types.Interface)
	if !ok {
		return nil, nil, false
	}
	tn, ok := typeName(p.Info.TypeOf(value))
	if !ok {
		return nil, nil, false
	}
	return tn, iface, true
}

// typeName returns the declaration of t's named type, through an alias and
// one pointer.
func typeName(t types.Type) (*types.TypeName, bool) {
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil, false
	}
	return named.Obj(), true
}

// isGeneric reports whether tn declares type parameters.
func isGeneric(tn *types.TypeName) bool {
	named, ok := tn.Type().(*types.Named)
	return ok && named.TypeParams().Len() > 0
}

// qualified names tn by its package's name, as a reader writes it.
func qualified(tn *types.TypeName) string {
	return tn.Pkg().Name() + "." + tn.Name()
}
