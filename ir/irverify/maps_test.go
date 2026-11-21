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
	stringType := ir.TypeRef{Target: "type/primitive/string"}
	for _, tc := range []struct {
		name string
		td   ir.TypeDef
		want []string
	}{
		{"a model that is a map", &ir.Model{AdditionalProperties: &ir.AdditionalProperties{Value: &stringType}},
			[]string{"types[type/x]"}},
		{"the map it should be", &ir.MapT{Value: stringType}, nil},
		{"a model with a property beside its catch-all", &ir.Model{
			Properties:           []ir.Property{{ID: "property/x/a", Name: ir.Naming{Source: "a"}, WireName: "a", Type: stringType}},
			AdditionalProperties: &ir.AdditionalProperties{Value: &stringType},
		}, nil},
		{"a model with no catch-all", &ir.Model{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.td.Common().ID = "type/x"
			doc := &ir.Document{Types: ir.TypeRegistry{"type/x": tc.td}}

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
	stringType := ir.TypeRef{Target: "type/primitive/string"}
	for _, tc := range []struct {
		name string
		td   ir.TypeDef
		want []string
	}{
		{"a map spelling its string key", &ir.MapT{Key: &stringType, Value: stringType}, []string{"types[type/x].Key"}},
		{"a catch-all spelling its string key", &ir.Model{
			Properties:           []ir.Property{{ID: "property/x/a", Name: ir.Naming{Source: "a"}, WireName: "a", Type: stringType}},
			AdditionalProperties: &ir.AdditionalProperties{Key: &stringType, Value: &stringType},
		}, []string{"types[type/x].AdditionalProperties.Key"}},
		{"a map with a nil key", &ir.MapT{Value: stringType}, nil},
		{"a map keyed by another type", &ir.MapT{Key: &ir.TypeRef{Target: "type/primitive/int64"}, Value: stringType}, nil},
		{"a map keyed by a nullable string", &ir.MapT{Key: &ir.TypeRef{Target: stringType.Target, Nullable: true}, Value: stringType}, nil},
		{"a model with no catch-all", &ir.Model{}, nil},
		{"a nil map", (*ir.MapT)(nil), nil},
		{"a nil model", (*ir.Model)(nil), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !ir.IsNilTypeDef(tc.td) {
				tc.td.Common().ID = "type/x"
			}
			doc := &ir.Document{Types: ir.TypeRegistry{"type/x": tc.td}}

			assert.Equal(t, tc.want, violationPaths(irverify.Verify(doc), "ir/map-key-string"))
		})
	}
}

// TestCheckCatchAlls_ReportsACatchAllSayingTwoThings pins ir/catch-all-empty and
// ir/catch-all-closed: a catch-all typing no key, and a closed model's catch-all
// typing keys its closure forbids, are each reported at the field that says it,
// while every shape docs/ir/models.md#catch-alls allows is not.
func TestCheckCatchAlls_ReportsACatchAllSayingTwoThings(t *testing.T) {
	t.Parallel()
	stringType := ir.TypeRef{Target: "type/primitive/string"}
	patterns := []ir.PatternProperties{{Pattern: "^x-", Value: stringType}}
	var typedNil *ir.Model
	for _, tc := range []struct {
		name         string
		td           ir.TypeDef
		empty, close []string
	}{
		{"neither a value nor a pattern", &ir.Model{AdditionalProperties: &ir.AdditionalProperties{}},
			[]string{"types[type/x].AdditionalProperties"}, nil},
		{"a closed model's value", &ir.Model{Additional: ir.AdditionalClosed, AdditionalProperties: &ir.AdditionalProperties{
			Value: &stringType, Patterns: patterns,
		}}, nil, []string{"types[type/x].AdditionalProperties.Value"}},
		{"patterns and no value beside a closure", &ir.Model{Additional: ir.AdditionalClosed,
			AdditionalProperties: &ir.AdditionalProperties{Patterns: patterns}}, nil, nil},
		{"patterns and a value", &ir.Model{AdditionalProperties: &ir.AdditionalProperties{Value: &stringType, Patterns: patterns}}, nil, nil},
		{"a value closed only after composition", &ir.Model{Additional: ir.AdditionalClosedAfterComposition,
			AdditionalProperties: &ir.AdditionalProperties{Value: &stringType, Patterns: patterns}}, nil, nil},
		{"no catch-all", &ir.Model{Additional: ir.AdditionalClosed}, nil, nil},
		{"a nil model", typedNil, nil, nil},
		{"a map", &ir.MapT{Value: stringType}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !ir.IsNilTypeDef(tc.td) {
				tc.td.Common().ID = "type/x"
			}
			doc := &ir.Document{Types: ir.TypeRegistry{"type/x": tc.td}}
			vs := irverify.Verify(doc)
			assert.Equal(t, tc.empty, violationPaths(vs, "ir/catch-all-empty"))
			assert.Equal(t, tc.close, violationPaths(vs, "ir/catch-all-closed"))
		})
	}
}
