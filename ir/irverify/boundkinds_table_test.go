// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// oneBound returns a Constraints stating the field named name and no other,
// at a value that states something: a count or a number of 1, a pattern or its
// message "a", and a true flag.
func oneBound(t *testing.T, name string) *ir.Constraints {
	t.Helper()
	constraints := &ir.Constraints{}
	f := reflect.ValueOf(constraints).Elem().FieldByName(name)
	require.True(t, f.IsValid(), "ir.Constraints declares no field %s", name)
	switch f.Interface().(type) {
	case *int64:
		f.Set(reflect.ValueOf(new(int64(1))))
	case *ir.BigVal:
		f.Set(reflect.ValueOf(new(ir.BigVal("1"))))
	case string:
		f.SetString("a")
	case bool:
		f.SetBool(true)
	default:
		require.Failf(t, "a field of a type this test sets no value of", "%s is a %s", name, f.Type())
	}
	return constraints
}

// TestVerify_EachBoundIsHeldToTheKindsThatTakeIt crosses every field of
// ir.Constraints with every kind of node holding one. A Model and a MapT take
// the property counts alone, a List and a Tuple the item counts and uniqueness
// alone, each as JSON Schema applies them to an object and an array, and a
// Scalar and a Union are not judged, whichever field they hold. The fields are
// read off the struct, so one added later is refused on a structural node until
// a row here says a kind takes it.
func TestVerify_EachBoundIsHeldToTheKindsThatTakeIt(t *testing.T) {
	t.Parallel()
	leafRef := ir.TypeRef{Target: "type/x/Leaf"}
	props := []string{"MinProperties", "MaxProperties"}
	items := []string{"MinItems", "MaxItems", "UniqueItems"}
	kinds := []struct {
		name    string
		node    func(*ir.Constraints) ir.TypeDef
		takes   []string
		unasked bool
	}{
		{name: "model", node: func(constraints *ir.Constraints) ir.TypeDef { return &ir.Model{Constraints: constraints} }, takes: props},
		{name: "map", node: func(constraints *ir.Constraints) ir.TypeDef {
			return &ir.MapT{Value: leafRef, Constraints: constraints}
		}, takes: props},
		{name: "list", node: func(constraints *ir.Constraints) ir.TypeDef {
			return &ir.List{ElementType: leafRef, Constraints: constraints}
		}, takes: items},
		{name: "tuple", node: func(constraints *ir.Constraints) ir.TypeDef {
			return &ir.Tuple{ElementTypes: []ir.TypeRef{leafRef}, Constraints: constraints}
		}, takes: items},
		{name: "scalar", node: func(constraints *ir.Constraints) ir.TypeDef {
			return &ir.Scalar{Base: &leafRef, Constraints: constraints}
		}, unasked: true},
		{name: "union", node: func(constraints *ir.Constraints) ir.TypeDef {
			return &ir.Union{Variants: []ir.Variant{{Type: leafRef}}, Constraints: constraints}
		}, unasked: true},
	}
	fields := reflect.TypeFor[ir.Constraints]()
	for _, kind := range kinds {
		for _, name := range kind.takes {
			_, declared := fields.FieldByName(name)
			require.True(t, declared, "%s takes %s, which ir.Constraints does not declare", kind.name, name)
		}
		for declared := range fields.Fields() {
			field := declared.Name
			t.Run(kind.name+"/"+field, func(t *testing.T) {
				t.Parallel()
				td := kind.node(oneBound(t, field))
				td.Common().ID = "type/x/N"
				got := boundViolations(stamped(closedDoc(td)))
				if kind.unasked || slices.Contains(kind.takes, field) {
					assert.Empty(t, got)
					return
				}
				require.Len(t, got, 1)
				assert.Equal(t, "types[type/x/N].Constraints", got[0].Path)
				assert.Contains(t, got[0].Message, kind.name)
			})
		}
	}
}
