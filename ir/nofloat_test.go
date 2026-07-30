package ir_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestIR_NoFloatFields walks the ir.Document type graph and fails on any float
// field: numeric IR data must use ir.BigVal decimal strings, never float32 or
// float64 (the TypeSpec Numeric lesson). The seen set bounds the walk: each
// distinct reflect.Type is visited exactly once, so recursive shapes terminate.
//
// It sits with the ir package's other source-derived completeness checks rather
// than in irverify, which checks compiled documents: nothing here calls Verify,
// and the kinds it walks come from the declaredTypeKinds those checks already
// share.
func TestIR_NoFloatFields(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(rt reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		if rt == nil || seen[rt] {
			return
		}
		seen[rt] = true
		switch rt.Kind() {
		case reflect.Float32, reflect.Float64:
			t.Errorf("float field reachable in IR at %s (%s): use ir.BigVal", path, rt)
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(rt.Elem(), path+"[]")
		case reflect.Map:
			walk(rt.Key(), path+".key")
			walk(rt.Elem(), path+".val")
		case reflect.Struct:
			for i := range rt.NumField() {
				f := rt.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		}
	}

	walk(reflect.TypeOf(ir.Document{}), "Document")
	// Sealed TypeDef kinds are reached only via the interface, which reflection on
	// the static type graph cannot enumerate. Walk each concrete kind explicitly,
	// seeded from the kinds the ir sources declare so a new variant is float-checked
	// the day it is added, not the day someone remembers to extend a list here.
	for _, kc := range declaredTypeKinds(t) {
		td, ok := ir.NewTypeDef(kc.kind)
		require.True(t, ok, "no concrete type registered for kind %q", kc.kind)
		rt := reflect.TypeOf(td)
		require.Equal(t, reflect.Pointer, rt.Kind(), "NewTypeDef must return a pointer for %q", kc.kind)
		walk(rt.Elem(), rt.Elem().Name())
	}
}
