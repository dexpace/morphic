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

// decoratorBase is the reference every decorator below decorates.
var decoratorBase = ir.TypeRef{Target: "t/openapi/components/schemas/Status", Nullable: true}

// decoratorCommon returns a TypeCommon setting every field a decorator may
// set, so a sweep that sets one more field changes only that.
func decoratorCommon() ir.TypeCommon {
	return ir.TypeCommon{
		ID:           "t/anon/components/schemas/W/properties/state",
		Name:         ir.Naming{Hint: "state"},
		Namespace:    []string{"ns"},
		Anonymous:    true,
		Docs:         ir.Docs{Description: "the widget state"},
		Deprecation:  &ir.Deprecation{Message: "gone"},
		Availability: &ir.Availability{},
		Examples:     []ir.Example{{Name: "e"}},
		Unmodeled:    ir.Unmodeled{"x-note": {Value: ir.RawValue(`"n"`), Reason: ir.ReasonNoIRHome}},
		Provenance:   ir.Provenance{Pointer: "/components/schemas/W/properties/state"},
	}
}

// decoratorModel returns a Model decorator: an anonymous model with a Base
// and every annotation a decorator may carry.
func decoratorModel() *ir.Model {
	base := decoratorBase
	return &ir.Model{TypeCommon: decoratorCommon(), Base: &base}
}

// decoratorScalar returns a Scalar decorator, the alias 3.1's $ref with
// siblings hoists.
func decoratorScalar() *ir.Scalar {
	base := decoratorBase
	return &ir.Scalar{TypeCommon: decoratorCommon(), Base: &base}
}

// TestDecorated_ReturnsTheReferenceADecoratorDecorates pins both decorator
// kinds: each returns its Base, Nullable included, unchanged.
func TestDecorated_ReturnsTheReferenceADecoratorDecorates(t *testing.T) {
	t.Parallel()
	for name, td := range map[string]ir.TypeDef{"model": decoratorModel(), "scalar": decoratorScalar()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := ir.Decorated(td)
			require.True(t, ok)
			assert.Empty(t, cmp.Diff(decoratorBase, got))
		})
	}
}

// TestDecorated_RefusesWhatIsNoDecorator pins the shapes that decorate
// nothing: no node, a typed nil of either kind, another kind, no Base, and a
// named node, whose name is public surface.
func TestDecorated_RefusesWhatIsNoDecorator(t *testing.T) {
	t.Parallel()
	baseless := decoratorModel()
	baseless.Base = nil
	baselessScalar := decoratorScalar()
	baselessScalar.Base = nil
	named := decoratorModel()
	named.Anonymous = false
	namedScalar := decoratorScalar()
	namedScalar.Anonymous = false
	base := decoratorBase
	for name, td := range map[string]ir.TypeDef{
		"nil":                nil,
		"typed nil model":    (*ir.Model)(nil),
		"typed nil scalar":   (*ir.Scalar)(nil),
		"list":               &ir.List{TypeCommon: decoratorCommon(), Elem: base},
		"model with no base": baseless,
		"scalar no base":     baselessScalar,
		"named model":        named,
		"named scalar":       namedScalar,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := ir.Decorated(td)
			assert.False(t, ok)
			assert.Empty(t, cmp.Diff(ir.TypeRef{}, got))
		})
	}
}

// TestDecorated_EveryOtherFieldMakesATypeOfItsOwn sets each field a decorator
// may not set, one at a time, on each kind, and requires the node to stop
// being one. It reads the fields off the types, so a field added to Model,
// Scalar or TypeCommon joins the sweep and fails it until Decorated refuses
// it, or the decorator rule (ir-design §4.10) is changed to allow it.
func TestDecorated_EveryOtherFieldMakesATypeOfItsOwn(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"TypeCommon": true, "Base": true,
		"ID": true, "Name": true, "Namespace": true, "Anonymous": true, "Provenance": true,
		"Docs": true, "Deprecation": true, "Examples": true, "Availability": true, "Unmodeled": true,
	}
	for _, s := range []struct {
		typ reflect.Type
		of  func(ir.TypeDef) reflect.Value
	}{
		{reflect.TypeFor[ir.Model](), func(td ir.TypeDef) reflect.Value { return reflect.ValueOf(td).Elem() }},
		{reflect.TypeFor[ir.Scalar](), func(td ir.TypeDef) reflect.Value { return reflect.ValueOf(td).Elem() }},
		{reflect.TypeFor[ir.TypeCommon](), func(td ir.TypeDef) reflect.Value {
			return reflect.ValueOf(td).Elem().FieldByName("TypeCommon")
		}},
	} {
		for field := range s.typ.Fields() {
			if allowed[field.Name] {
				continue
			}
			for kind, td := range map[string]func() ir.TypeDef{
				"model":  func() ir.TypeDef { return decoratorModel() },
				"scalar": func() ir.TypeDef { return decoratorScalar() },
			} {
				if s.typ == reflect.TypeFor[ir.Model]() && kind != "model" ||
					s.typ == reflect.TypeFor[ir.Scalar]() && kind != "scalar" {
					continue
				}
				name := kind + "/" + s.typ.Name() + "." + field.Name
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					node := td()
					setNonZero(t, s.of(node).FieldByName(field.Name))

					_, ok := ir.Decorated(node)

					assert.False(t, ok, "a node setting %s says something about values its Base does not", name)
				})
			}
		}
	}
}
