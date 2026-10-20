// Package irfacts_test holds the tests of the irfacts package. The builders
// several files share live here.
package irfacts_test

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// compDoc builds a registry from definitions, keyed by each one's own ID.
func compDoc(defs ...ir.TypeDef) *ir.Document {
	types := ir.TypeRegistry{}
	for _, d := range defs {
		types[d.Common().ID] = d
	}
	return &ir.Document{Types: types}
}

// compModel builds the model id, shaped by mutate when it is not nil.
func compModel(id ir.TypeID, mutate func(*ir.Model)) *ir.Model {
	m := &ir.Model{ID: id}
	if mutate != nil {
		mutate(m)
	}
	return m
}

// compScalar builds the scalar id over base, or an opaque one when base is empty.
func compScalar(id, base ir.TypeID) *ir.Scalar {
	s := &ir.Scalar{ID: id}
	if base != "" {
		s.Base = &ir.TypeRef{Target: base}
	}
	return s
}

// refs builds a reference to each of ids.
func refs(ids ...ir.TypeID) []ir.TypeRef {
	out := make([]ir.TypeRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, ir.TypeRef{Target: id})
	}
	return out
}

// typeIDReflect is the reflect.Type of ir.TypeID.
var typeIDReflect = reflect.TypeFor[ir.TypeID]()

// irPackage is the ir package type-checked, loaded once for the tests here that
// read its declarations.
var irPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load("github.com/dexpace/morphic/ir")
})

// declaredKinds returns every TypeKind the ir package declares, sorted: read
// from the source, so a kind added there reaches every test ranging over it.
func declaredKinds(t *testing.T) []ir.TypeKind {
	t.Helper()
	p, err := irPackage()
	require.NoError(t, err)
	consts, err := p.StringConsts("TypeKind")
	require.NoError(t, err)
	kinds := make([]ir.TypeKind, 0, len(consts))
	for _, c := range consts {
		kinds = append(kinds, ir.TypeKind(c.Value))
	}
	slices.SortFunc(kinds, func(a, b ir.TypeKind) int { return strings.Compare(string(a), string(b)) })
	require.NotEmpty(t, kinds, "the ir package must declare its type kinds")
	return kinds
}
