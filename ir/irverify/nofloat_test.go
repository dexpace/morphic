package irverify_test

import (
	"reflect"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestIR_NoFloatFields walks the ir.Document type graph and fails on any float
// field: numeric IR data must use ir.BigVal decimal strings, never float32 or
// float64 (the TypeSpec Numeric lesson). The seen set bounds the walk: each
// distinct reflect.Type is visited exactly once, so recursive shapes terminate.
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
	// the static type graph cannot enumerate; walk each concrete kind explicitly.
	for _, td := range []any{
		ir.Primitive{}, ir.Scalar{}, ir.Model{}, ir.Union{}, ir.Enum{},
		ir.List{}, ir.MapT{}, ir.Tuple{}, ir.Literal{}, ir.External{}, ir.Any{},
	} {
		rt := reflect.TypeOf(td)
		walk(rt, rt.Name())
	}
}
