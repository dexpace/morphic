package ir_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestJSONTags_OmitEmptyOnlyOnCollections holds every struct a Document can
// reach to the serialization rule: omitempty only on strings, slices, maps and
// arrays, whose empty and absent forms mean the same. Under encoding/json/v2,
// omitempty keeps a zero number or bool and drops a pointer to an empty struct,
// so a non-nil &ir.XMLHints{} — what an `xml: {}` lowers to — would vanish from
// the document; every other optional field takes omitzero.
func TestJSONTags_OmitEmptyOnlyOnCollections(t *testing.T) {
	t.Parallel()
	structs := reachableStructs(t)
	require.Greater(t, len(structs), len(allKinds), "the walk must reach past the TypeDef kinds themselves")

	var bad []string
	for _, st := range structs {
		for f := range st.Fields() {
			_, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !strings.Contains(opts, "omitempty") {
				continue
			}
			switch f.Type.Kind() {
			case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
			default:
				bad = append(bad, st.String()+"."+f.Name+" ("+f.Type.String()+")")
			}
		}
	}
	assert.Empty(t, bad, "omitempty on a field that is not a string, slice, map or array; use omitzero")
}

// reachableStructs returns every struct type reachable from Document and from
// each TypeDef kind through fields, pointers, slices, arrays and maps. The
// worklist is bounded by the finite set of types the ir package declares.
func reachableStructs(t *testing.T) []reflect.Type {
	t.Helper()
	queue := []reflect.Type{reflect.TypeFor[ir.Document]()}
	for _, k := range allKinds {
		td, ok := ir.NewTypeDef(k)
		require.True(t, ok, "NewTypeDef(%s)", k)
		queue = append(queue, reflect.TypeOf(td))
	}
	seen := map[reflect.Type]bool{}
	var out []reflect.Type
	for len(queue) > 0 {
		typ := queue[0]
		queue = queue[1:]
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Map {
			queue = append(queue, typ.Key(), typ.Elem())
			continue
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			continue
		}
		seen[typ] = true
		out = append(out, typ)
		for f := range typ.Fields() {
			queue = append(queue, f.Type)
		}
	}
	return out
}
