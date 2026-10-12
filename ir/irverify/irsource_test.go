package irverify

import (
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

// irPackage is the ir package type-checked, loaded once for every test here
// that reads its declarations.
var irPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load(irPath)
})

// irDecls returns the ir package, type-checked.
func irDecls(t *testing.T) *decls.Package {
	t.Helper()
	p, err := irPackage()
	require.NoError(t, err)
	return p
}

// verifyPackage is this package's own production sources, type-checked, loaded
// once for the tests that read what the checks declare.
var verifyPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load(irPath + "/irverify")
})

// verifyDecls returns this package's production sources, type-checked.
func verifyDecls(t *testing.T) *decls.Package {
	t.Helper()
	p, err := verifyPackage()
	require.NoError(t, err)
	return p
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
// A type declared as an alias of another declaration adds no fields of its own,
// but a defined type over a struct does, so it is listed under its own name. A
// function-local type is not a package-level declaration and so is not listed.
func declaredFields(t *testing.T) []declaredField {
	t.Helper()
	scope := irDecls(t).Types.Scope()
	var out []declaredField
	for _, name := range scope.Names() {
		decl, isType := scope.Lookup(name).(*types.TypeName)
		if !isType || decl.IsAlias() {
			continue
		}
		st, isStruct := decl.Type().Underlying().(*types.Struct)
		if !isStruct {
			continue
		}
		for field := range st.Fields() {
			out = append(out, declaredField{owner: name, field: field})
		}
	}
	require.NotEmpty(t, out, "the ir package must declare struct fields")
	return out
}
