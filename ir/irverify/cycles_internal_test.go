// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// concreteTypeDefs returns the type of every concrete TypeDef the ir package
// seals into the sum.
//
// The kinds are read from the type-checked sources, and the count is held to the
// types that declare the sealed marker, so a variant that has no kind, or a kind
// that has no variant, ends the test here rather than leaving the graph a
// variant short. ir.TypeDef is an interface, which reflection alone cannot
// expand.
func concreteTypeDefs(t *testing.T) []reflect.Type {
	t.Helper()
	kinds, err := irDecls(t).StringConsts("TypeKind")
	require.NoError(t, err)
	require.Len(t, kinds, len(irDecls(t).TypesDeclaring("typeDef")),
		"the sum holds one concrete type per kind")

	out := make([]reflect.Type, 0, len(kinds))
	for _, k := range kinds {
		td, known := ir.NewTypeDef(ir.TypeKind(k.Value))
		require.True(t, known, "kind %q has no constructor", k.Value)
		out = append(out, reflect.TypeOf(td))
	}
	return out
}

// typeChildren returns the types a value of typ holds directly: the element of a
// pointer, slice or array, both halves of a map, a struct's fields, and every
// concrete TypeDef for the sum. Any other interface ends the test, because a
// value of it can hold a type this graph cannot see.
func typeChildren(t *testing.T, typ reflect.Type, defs []reflect.Type) []reflect.Type {
	t.Helper()
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return []reflect.Type{typ.Elem()}
	case reflect.Map:
		return []reflect.Type{typ.Key(), typ.Elem()}
	case reflect.Struct:
		out := make([]reflect.Type, 0, typ.NumField())
		for field := range typ.Fields() {
			out = append(out, field.Type)
		}
		return out
	case reflect.Interface:
		require.Equal(t, reflect.TypeFor[ir.TypeDef](), typ,
			"%s is an interface the type graph cannot expand", typ)
		return defs
	default:
		return nil
	}
}

// reachableFrom returns every type reachable from the roots, the roots included.
func reachableFrom(t *testing.T, defs []reflect.Type, roots ...reflect.Type) map[reflect.Type]bool {
	t.Helper()
	seen := map[reflect.Type]bool{}
	queue := slices.Clone(roots)
	for len(queue) > 0 {
		typ := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[typ] {
			continue
		}
		seen[typ] = true
		queue = append(queue, typeChildren(t, typ, defs)...)
	}
	return seen
}

// selfReachableReferences returns, sorted, every pointer, slice and map type in
// the document's type graph that can reach itself: those whose element, or key,
// can lead back to a value of the same type. A reference of one of them can hold
// the reference that holds it, which a type that cannot reach itself never can.
func selfReachableReferences(t *testing.T) []string {
	t.Helper()
	defs := concreteTypeDefs(t)
	var out []string
	for typ := range reachableFrom(t, defs, reflect.TypeFor[*ir.Document]()) {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map:
			if reachableFrom(t, defs, typeChildren(t, typ, defs)...)[typ] {
				out = append(out, typ.String())
			}
		default:
		}
	}
	slices.Sort(out)
	require.NotEmpty(t, out, "the ir sources hold values that contain values; none found means the graph is wrong")
	return out
}
