// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestCheckMaps_ReportsAModelThatIsAMap pins ir/map-as-model: a model whose
// one member fact is a catch-all is reported, once, at its registry entry,
// while the MapT it should be and a model with anything beside its catch-all
// are not.
func TestCheckMaps_ReportsAModelThatIsAMap(t *testing.T) {
	t.Parallel()
	str := ir.TypeRef{Target: "t/prim/string"}
	for _, tc := range []struct {
		name string
		td   ir.TypeDef
		want []string
	}{
		{"a model that is a map", &ir.Model{AdditionalProps: &ir.AdditionalProps{Value: str}},
			[]string{"types[t/x]"}},
		{"the map it should be", &ir.MapT{Value: str}, nil},
		{"a model with a property beside its catch-all", &ir.Model{
			Properties:      []ir.Property{{ID: "p/x/a", Name: ir.Naming{Source: "a"}, WireName: "a", Type: str}},
			AdditionalProps: &ir.AdditionalProps{Value: str},
		}, nil},
		{"a model with no catch-all", &ir.Model{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.td.Common().ID = "t/x"
			doc := &ir.Document{Types: ir.TypeRegistry{"t/x": tc.td}}

			assert.Equal(t, tc.want, violationPaths(irverify.Verify(doc), "ir/map-as-model"))
		})
	}
}

// TestCheckMapKeys_ReportsAStringKeySpelledOut pins ir/map-key-string: a map
// or catch-all naming the string primitive as its key is reported at that
// key, since a nil Key already states string keys. A nil key, another key type
// and a nullable string key are not, and neither is a nil registry entry.
func TestCheckMapKeys_ReportsAStringKeySpelledOut(t *testing.T) {
	t.Parallel()
	str := ir.TypeRef{Target: "t/prim/string"}
	for _, tc := range []struct {
		name string
		td   ir.TypeDef
		want []string
	}{
		{"a map spelling its string key", &ir.MapT{Key: &str, Value: str}, []string{"types[t/x].Key"}},
		{"a catch-all spelling its string key", &ir.Model{
			Properties:      []ir.Property{{ID: "p/x/a", Name: ir.Naming{Source: "a"}, WireName: "a", Type: str}},
			AdditionalProps: &ir.AdditionalProps{Key: &str, Value: str},
		}, []string{"types[t/x].AdditionalProps.Key"}},
		{"a map with a nil key", &ir.MapT{Value: str}, nil},
		{"a map keyed by another type", &ir.MapT{Key: &ir.TypeRef{Target: "t/prim/int64"}, Value: str}, nil},
		{"a map keyed by a nullable string", &ir.MapT{Key: &ir.TypeRef{Target: str.Target, Nullable: true}, Value: str}, nil},
		{"a model with no catch-all", &ir.Model{}, nil},
		{"a nil map", (*ir.MapT)(nil), nil},
		{"a nil model", (*ir.Model)(nil), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !ir.IsNilTypeDef(tc.td) {
				tc.td.Common().ID = "t/x"
			}
			doc := &ir.Document{Types: ir.TypeRegistry{"t/x": tc.td}}

			assert.Equal(t, tc.want, violationPaths(irverify.Verify(doc), "ir/map-key-string"))
		})
	}
}
