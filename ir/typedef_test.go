// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// allKinds is the closed kind set from ir-design §4. Adding a TypeKind without
// updating every consumer must break this file (the assertNever lesson).
var allKinds = []ir.TypeKind{
	ir.KindPrimitive, ir.KindScalar, ir.KindModel, ir.KindUnion, ir.KindEnum,
	ir.KindList, ir.KindMap, ir.KindTuple, ir.KindLiteral, ir.KindExternal, ir.KindAny,
}

func TestTypeDef_KindDispatchIsComplete(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td, ok := ir.NewTypeDef(k)
			require.True(t, ok, "no concrete type registered for kind %q", k)
			assert.Equal(t, k, td.Kind())
			require.NotNil(t, td.Common())
		})
	}
}

func TestNewTypeDef_UnknownKind(t *testing.T) {
	t.Parallel()
	_, ok := ir.NewTypeDef(ir.TypeKind("bogus"))
	assert.False(t, ok)
}

// allConcreteTypeDefs names the sum's members as concrete types rather than as
// kinds, so a type that stops satisfying ir.TypeDef fails to compile here.
// TestTypeDef_HandWrittenConcreteListIsComplete holds the list to the sealed
// marker set derived from the ir sources: nothing that compares hand-written
// lists against each other can see a variant absent from all of them.
var allConcreteTypeDefs = []ir.TypeDef{
	&ir.Primitive{}, &ir.Scalar{}, &ir.Model{}, &ir.Union{}, &ir.Enum{},
	&ir.List{}, &ir.MapT{}, &ir.Tuple{}, &ir.Literal{}, &ir.External{}, &ir.Any{},
}

func TestTypeDef_ConcreteTypesImplementInterface(t *testing.T) {
	t.Parallel()
	for _, td := range allConcreteTypeDefs {
		assert.Contains(t, allKinds, td.Kind())
	}
}

// TestIsNilTypeDef_ScreensBothSpellingsOfNil holds the screen every walk over a
// type registry runs entries through. The typed nil is the one that matters: it
// satisfies a type switch case and a comma-ok assertion alike, so a check that
// matched a kind and read the value would panic on it rather than report it.
func TestIsNilTypeDef_ScreensBothSpellingsOfNil(t *testing.T) {
	t.Parallel()
	assert.True(t, ir.IsNilTypeDef(nil), "an untyped nil interface holds no definition")
	assert.True(t, ir.IsNilTypeDef((*ir.Model)(nil)), "a typed nil pointer holds none either")
	for _, td := range allConcreteTypeDefs {
		assert.False(t, ir.IsNilTypeDef(td), "%T is a live definition", td)
	}
}

// TestTypeCommon_JSONContract pins TypeCommon's omitempty contract. ID, Name,
// Anonymous, Docs, Sensitive, and Provenance carry no omitempty because every
// type-graph node has an identity, a naming, an anonymity flag, a docs
// object, a sensitivity flag, and provenance; every other field is optional.
// TypeCommon is embedded and inlined into all 11 concrete kinds, so this test
// pins the shared portion of every one of their zero-value encodings. It also
// pins that a fully populated TypeCommon round-trips.
func TestTypeCommon_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.TypeCommon{},
		`{"id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0}}`,
		ir.TypeCommon{
			ID:               "t/openapi/components/schemas/User",
			Name:             populatedNaming(),
			Namespace:        []string{"com", "example"},
			Anonymous:        true,
			Docs:             populatedDocs(),
			Tags:             []string{"users", "public"},
			Sensitive:        true,
			Access:           "internal",
			Deprecation:      populatedDeprecation(),
			Availability:     populatedAvailability(),
			WireNameByFormat: map[string]string{"xml": "User", "json": "user"},
			MediaTypeHint:    "application/json",
			XML:              populatedXMLHints(),
			Examples: []ir.Example{
				{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "v1"}},
			},
			Instantiation: &ir.TemplateInstantiation{
				Template: "Page",
				Args: []ir.TemplateArg{
					{Type: &ir.TypeRef{Target: "t/item"}},
					{Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")}},
				},
			},
			Unmodeled:  populatedUnmodeled(),
			Provenance: populatedProvenance(),
		})
}

// TestTypeCommon_WireNameByFormatDeterministic pins Class C for TypeCommon's
// map field.
func TestTypeCommon_WireNameByFormatDeterministic(t *testing.T) {
	t.Parallel()
	tc := ir.TypeCommon{
		WireNameByFormat: map[string]string{
			"z-format": "Z", "m-format": "M", "a-format": "A", "q-format": "Q", "b-format": "B",
		},
	}
	got := assertDeterministicMarshal(t, tc)
	assert.Contains(t, got,
		`"wireNameByFormat":{"a-format":"A","b-format":"B","m-format":"M","q-format":"Q","z-format":"Z"}`)
}

// TestTypeDef_ZeroValueShapeWithKindTag pins the zero-value marshal shape of
// every concrete TypeDef kind, including the adjacent "kind" discriminator
// (invariant #7 / the sealed-sum JSON encoding). This is the Class A test
// json_internal_test.go does not cover: that file drives the codec's helpers
// directly, but never asserts the exact zero-value payload for each real kind.
func TestTypeDef_ZeroValueShapeWithKindTag(t *testing.T) {
	t.Parallel()
	for _, tt := range typeDefZeroShapeTests() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.td)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(raw))
		})
	}
}

// TestTypeDef_CommonIsAnAliasNotACopy checks that Common() returns a pointer
// into the concrete struct's own TypeCommon, not a copy or a value shared
// across kinds. A copying or singleton implementation would pass every other
// test in this package (including types_test.go's JSON round-trip tests, which
// never call Common()) while silently breaking any pass that mutates shared
// metadata (Availability, Deprecation, …) via the TypeDef interface.
//
// The read-back goes through json.Marshal of the whole td rather than
// td.Common().ID again, since re-reading via Common() would trivially pass a
// shared-singleton mutant.
func TestTypeDef_CommonIsAnAliasNotACopy(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td, ok := ir.NewTypeDef(k)
			require.True(t, ok, "no concrete type registered for kind %q", k)
			td.Common().ID = "t/y"
			raw, err := json.Marshal(td)
			require.NoError(t, err)
			assert.Contains(t, string(raw), `"id":"t/y"`)
		})
	}
}

// TestTemplateInstantiation_JSONContract pins TemplateInstantiation's
// omitempty contract (both fields are optional) and that a fully populated
// TemplateInstantiation round-trips.
func TestTemplateInstantiation_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.TemplateInstantiation{}, `{}`, ir.TemplateInstantiation{
		Template: "Page",
		Args: []ir.TemplateArg{
			{Type: &ir.TypeRef{Target: "t/item"}},
			{Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")}},
		},
	})
}

// TestTemplateArg_ZeroValueShape pins TemplateArg's omitempty contract: both
// fields are optional, since exactly one is legally set at a time.
func TestTemplateArg_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.TemplateArg{}, `{}`)
}

// TestTemplateArg_PopulatedRoundTrip pins that both arms of TemplateArg — the
// type-parameter arm and the valueof arm — round-trip.
func TestTemplateArg_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]ir.TemplateArg{
		"type":  {Type: &ir.TypeRef{Target: "t/item"}},
		"value": {Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")}},
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRoundTrip(t, want)
		})
	}
}
