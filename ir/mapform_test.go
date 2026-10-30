// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// mapModel returns a model that is a map and nothing else: a catch-all, plus
// the fields AsMap carries over.
func mapModel() *ir.Model {
	minProps := int64(1)
	return &ir.Model{
		ID:   "t/openapi/components/schemas/Labels",
		Docs: ir.Docs{Description: "labels"},
		AdditionalProps: &ir.AdditionalProps{
			Key:   &ir.TypeRef{Target: "t/prim/int64"},
			Value: ir.TypeRef{Target: "t/prim/string", Nullable: true},
		},
		Constraints: &ir.Constraints{MinProps: &minProps},
	}
}

// TestAsMap_CarriesWhatAMapHolds pins the conversion: the MapT keeps the
// model's TypeCommon and Constraints and takes the catch-all's key and value,
// each unchanged.
func TestAsMap_CarriesWhatAMapHolds(t *testing.T) {
	t.Parallel()
	m := mapModel()

	mt, ok := ir.AsMap(m)

	require.True(t, ok)
	want := &ir.MapT{
		TypeCommon:  m.TypeCommon,
		Key:         m.AdditionalProps.Key,
		Value:       m.AdditionalProps.Value,
		Constraints: m.Constraints,
	}
	assert.Empty(t, cmp.Diff(want, mt))
}

// TestAsMap_RefusesWhatIsNoMap pins the shapes with no catch-all to convert: a
// nil model, a model with no catch-all, and one whose catch-all scopes a value
// to a key pattern, which a MapT has no field for.
func TestAsMap_RefusesWhatIsNoMap(t *testing.T) {
	t.Parallel()
	patterned := mapModel()
	patterned.AdditionalProps.Patterns = []ir.PatternProps{{Pattern: "^x-", Value: ir.TypeRef{Target: "t/prim/string"}}}
	noCatchAll := mapModel()
	noCatchAll.AdditionalProps = nil
	for name, m := range map[string]*ir.Model{"nil": nil, "no catch-all": noCatchAll, "patterned": patterned} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mt, ok := ir.AsMap(m)
			assert.False(t, ok)
			assert.Nil(t, mt)
		})
	}
}

// TestAsMap_EveryUncarriedFieldIsMoreThanAMap sets each field of a Model and
// of its catch-all that AsMap does not carry, one at a time, on a map, and
// requires the result to stay a Model. It reads the fields off the types, so a
// field added to either joins the sweep, and fails it until AsMap carries it or
// refuses it rather than dropping it.
func TestAsMap_EveryUncarriedFieldIsMoreThanAMap(t *testing.T) {
	t.Parallel()
	for _, s := range []struct {
		typ     reflect.Type
		carried map[string]bool
		of      func(*ir.Model) reflect.Value
	}{
		{reflect.TypeFor[ir.Model](), map[string]bool{"TypeCommon": true, "AdditionalProps": true, "Constraints": true},
			func(m *ir.Model) reflect.Value { return reflect.ValueOf(m).Elem() }},
		{reflect.TypeFor[ir.AdditionalProps](), map[string]bool{"Key": true, "Value": true},
			func(m *ir.Model) reflect.Value { return reflect.ValueOf(m.AdditionalProps).Elem() }},
	} {
		for field := range s.typ.Fields() {
			name := s.typ.Name() + "." + field.Name
			if s.carried[field.Name] {
				continue
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				m := mapModel()
				setNonZero(t, s.of(m).FieldByName(field.Name))

				_, ok := ir.AsMap(m)

				assert.False(t, ok, "a model setting %s states more than a map does", name)
			})
		}
	}
}

// setNonZero gives v a value other than its zero value, failing on a kind it
// has no value for so a new field cannot pass the sweep unset.
func setNonZero(t *testing.T, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("set")
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	case reflect.Map:
		v.Set(reflect.MakeMapWithSize(v.Type(), 1))
		v.SetMapIndex(reflect.Zero(v.Type().Key()), reflect.Zero(v.Type().Elem()))
	default:
		require.Failf(t, "no non-zero value", "field of kind %s; extend setNonZero", v.Kind())
	}
	require.False(t, v.IsZero(), "the field was set")
}
