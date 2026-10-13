package irverify

import (
	"fmt"
	"go/types"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// irPath is the import path of the package whose declarations these tests read.
const irPath = "github.com/dexpace/morphic/ir"

// maxTypeChain bounds the walk through a field's type (the bounded-recursion
// rule). A type may recur through a pointer, slice or map, so a walk that has not
// ended by then is following a cycle, not a deep IR.
const maxTypeChain = 16

// loadedPackages are the ir package and this package's own production sources,
// type-checked together, once, so that what this package refers to in ir is the
// object ir declares. Index 0 is ir: LoadAll sorts by import path.
var loadedPackages = sync.OnceValues(func() ([]*decls.Package, error) {
	pkgs, err := decls.LoadAll(irPath, irPath+"/irverify")
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 2 || pkgs[0].Types.Path() != irPath {
		return nil, fmt.Errorf("loaded %d packages, want ir then irverify", len(pkgs))
	}
	return pkgs, nil
})

// irDecls returns the ir package, type-checked.
func irDecls(t *testing.T) *decls.Package {
	t.Helper()
	pkgs, err := loadedPackages()
	require.NoError(t, err)
	return pkgs[0]
}

// verifyDecls returns this package's production sources, type-checked.
func verifyDecls(t *testing.T) *decls.Package {
	t.Helper()
	pkgs, err := loadedPackages()
	require.NoError(t, err)
	return pkgs[1]
}

// declaredField is one field of a struct type the ir package declares, with the
// type it is declared on.
type declaredField struct {
	owner string
	field *types.Var
}

// key names the field "Type.Field", the spelling the classification tables use.
func (f declaredField) key() string {
	return f.owner + "." + f.field.Name()
}

// declaredFields returns every field of every struct type the ir package
// declares at package level, unexported ones and embedded ones included, in
// declaration-name order.
//
// An alias of another declaration adds no fields of its own. An alias of a
// struct literal and a defined type over a struct do, so each is listed under
// its own name. A function-local type is not a package-level declaration and so
// is not listed. Nor, yet, are the fields of an anonymous struct a field is
// typed with (#851); ir declares none.
func declaredFields(t *testing.T) []declaredField {
	t.Helper()
	var out []declaredField
	for decl := range irDecls(t).Objects[*types.TypeName]() {
		if aliasesAnother(decl) {
			continue
		}
		st, isStruct := decl.Type().Underlying().(*types.Struct)
		if !isStruct {
			continue
		}
		for field := range st.Fields() {
			out = append(out, declaredField{owner: decl.Name(), field: field})
		}
	}
	require.NotEmpty(t, out, "the ir package must declare struct fields")
	return out
}

// aliasesAnother reports whether decl is an alias of a defined type declared
// elsewhere, which adds no type of its own. An alias of a type literal, such as
// struct{ Index int }, is not one: no other declaration lists what it holds.
func aliasesAnother(decl *types.TypeName) bool {
	named, isNamed := types.Unalias(decl.Type()).(*types.Named)
	return isNamed && named.Obj() != decl
}
