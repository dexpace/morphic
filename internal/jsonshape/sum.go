package jsonshape

import (
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"slices"
)

// variants describes the sealed sum n: the type of the tag its Kind method
// returns, then each variant with that tag. The encoder writes whichever
// variant a value holds, so the variants are the interface's shape. Any
// package can implement an interface that is not sealed, so only a sealed one
// has variants to list.
func (w *walker) variants(n *types.Named, iface *types.Interface) ([]string, error) {
	if !sealed(iface) {
		return nil, errors.New("an interface with no unexported method is open to any implementation, so it has no one shape")
	}
	kind, err := kindMethod(iface)
	if err != nil {
		return nil, err
	}
	tag, err := w.spell(kind.Signature().Results().At(0).Type(), 0)
	if err != nil {
		return nil, err
	}

	var out []string
	scope := n.Obj().Pkg().Scope()
	for _, name := range scope.Names() {
		variant, byPointer, implements := implementer(scope.Lookup(name), iface)
		if !implements {
			continue
		}
		held, spelled := types.Type(variant), w.name(variant)
		if byPointer {
			held, spelled = types.NewPointer(variant), "*"+spelled
		}
		value, err := w.tagOf(held, n.Obj().Pkg())
		if err != nil {
			return nil, fmt.Errorf("variant %s: %w", name, err)
		}
		w.reach(variant)
		out = append(out, "variant "+value+" "+spelled)
	}
	if len(out) == 0 {
		return nil, errors.New("no type implements the sealed interface")
	}
	slices.Sort(out)
	return append([]string{"tag " + tag}, out...), nil
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
func (w *walker) tagOf(held types.Type, pkg *types.Package) (string, error) {
	var decl *ast.FuncDecl
	if sel := types.NewMethodSet(held).Lookup(pkg, "Kind"); sel != nil {
		if fn, isFunc := sel.Obj().(*types.Func); isFunc {
			decl = w.methods[fn]
		}
	}
	if decl == nil {
		return "", errors.New("its Kind method is not declared in the package read")
	}
	if len(decl.Body.List) != 1 {
		return "", errors.New("its Kind method is not a single return of a constant")
	}
	ret, isReturn := decl.Body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return "", errors.New("its Kind method is not a single return of a constant")
	}
	tv := w.pkg.Info.Types[ret.Results[0]]
	if tv.Value == nil {
		return "", errors.New("its Kind method is not a single return of a constant")
	}
	return tv.Value.ExactString(), nil
}
