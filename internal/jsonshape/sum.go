// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonshape

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"slices"
	"strings"
)

// variants reads the sealed sum t declares: the type of the tag its Kind
// method returns, and each variant with that tag. The encoder writes whichever
// variant a value holds, so the variants are the interface's shape. Any
// package can implement an interface that is not sealed, so only a sealed one
// has variants to list.
func (w *walker) variants(t *Type, iface *types.Interface) error {
	if !sealed(iface) {
		return errors.New("an interface with no unexported method is open to any implementation, so it has no one shape")
	}
	kind, err := kindMethod(iface)
	if err != nil {
		return err
	}
	tagType := kind.Signature().Results().At(0).Type()
	tag, err := w.spell(tagType, 0)
	if err != nil {
		return err
	}

	var out []Variant
	pkg := t.Named.Obj().Pkg()
	for _, name := range pkg.Scope().Names() {
		variant, byPointer, implements := implementer(pkg.Scope().Lookup(name), iface)
		if !implements {
			continue
		}
		v := Variant{Named: variant, ByPointer: byPointer, spelled: w.name(variant)}
		held := types.Type(variant)
		if byPointer {
			held, v.spelled = types.NewPointer(variant), "*"+v.spelled
		}
		if v.Tag, err = w.tagOf(held, pkg); err != nil {
			return fmt.Errorf("variant %s: %w", name, err)
		}
		w.reach(variant)
		out = append(out, v)
	}
	if len(out) == 0 {
		return errors.New("no type implements the sealed interface")
	}
	slices.SortFunc(out, func(a, b Variant) int { return strings.Compare(a.line(), b.line()) })
	t.Tag, t.tag, t.Variants = tagType, tag, out
	return nil
}

// sealed reports whether iface has an unexported method, which only its own
// package can implement.
func sealed(iface *types.Interface) bool {
	for m := range iface.Methods() {
		if !m.Exported() {
			return true
		}
	}
	return false
}

// kindMethod returns the method a sealed sum dispatches on: Kind, taking
// nothing and returning the variant's tag.
func kindMethod(iface *types.Interface) (*types.Func, error) {
	for m := range iface.Methods() {
		sig := m.Signature()
		if m.Name() == "Kind" && sig.Params().Len() == 0 && sig.Results().Len() == 1 {
			return m, nil
		}
	}
	return nil, errors.New("a sealed interface with no Kind() method returning its tag")
}

// implementer reports whether obj declares a concrete type that implements
// iface, returning it, and whether a value of it is held by pointer because
// only the pointer has every method. An alias declares no type of its own:
// its type is an Alias, not the Named it names, so it is no variant.
func implementer(obj types.Object, iface *types.Interface) (variant *types.Named, byPointer, implements bool) {
	tn, isType := obj.(*types.TypeName)
	if !isType {
		return nil, false, false
	}
	named, isNamed := tn.Type().(*types.Named)
	if !isNamed || types.IsInterface(named) {
		return nil, false, false
	}
	if types.Implements(named, iface) {
		return named, false, true
	}
	if types.Implements(types.NewPointer(named), iface) {
		return named, true, true
	}
	return nil, false, false
}

// tagOf returns the constant the Kind method of held returns. It reads the
// method's body, which must be that one return, since a tag computed any
// other way is not one a description can state. decls reads only a package
// that compiles, and a method there always has a body.
func (w *walker) tagOf(held types.Type, pkg *types.Package) (constant.Value, error) {
	var decl *ast.FuncDecl
	if sel := types.NewMethodSet(held).Lookup(pkg, "Kind"); sel != nil {
		if fn, isFunc := sel.Obj().(*types.Func); isFunc {
			decl = w.methods[fn]
		}
	}
	if decl == nil {
		return nil, errors.New("its Kind method is not declared in the package read")
	}
	if len(decl.Body.List) != 1 {
		return nil, errors.New("its Kind method is not a single return of a constant")
	}
	ret, isReturn := decl.Body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return nil, errors.New("its Kind method is not a single return of a constant")
	}
	tv := w.pkg.Info.Types[ret.Results[0]]
	if tv.Value == nil {
		return nil, errors.New("its Kind method is not a single return of a constant")
	}
	return tv.Value, nil
}
