// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestModel_PlainRequiredUndeclaredPropertyLandsInRequires pins the plain,
// non-allOf path to the composition's rule: a required name no declared
// property matches is legal JSON Schema (required does not imply declaration),
// so it lands in Model.Requires, silently, with no property invented for it,
// while one a property declares marks that property.
func TestModel_PlainRequiredUndeclaredPropertyLandsInRequires(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Plain:
      type: object
      required: [ghost, id, ghost]
      properties:
        id: {type: integer}
`)
	doc, diags := schema.LowerSpec(t, spec)
	model, ok := doc.Types[componentID("Plain")].(*ir.Model)
	require.True(t, ok, "Plain should be a model")
	require.Len(t, model.Properties, 1)
	assert.True(t, model.Properties[0].Required, "id is declared, so it is required on its property")
	assert.Equal(t, []string{"ghost"}, model.Requires, "ghost is listed once, however often it is written")
	for _, diagnostic := range diags {
		assert.True(t, strings.HasPrefix(diagnostic.Code, diag.Validation),
			"the repeat draws the validation's finding, and the lowering adds none: %+v", diagnostic)
	}
}

func TestModel_FourOptionalityStates(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      required: [reqPlain, reqNull]
      properties:
        reqPlain: {type: string}
        reqNull: {type: [string, "null"]}
        optPlain: {type: string}
        optNull: {type: [string, "null"]}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 4)
	byName := openapitest.PropsByWire(model.Properties)
	assert.True(t, byName["reqPlain"].Required)
	assert.False(t, byName["reqPlain"].Type.Nullable)
	assert.True(t, byName["reqNull"].Required)
	assert.True(t, byName["reqNull"].Type.Nullable)
	assert.False(t, byName["optPlain"].Required)
	assert.False(t, byName["optPlain"].Type.Nullable)
	assert.False(t, byName["optNull"].Required)
	assert.True(t, byName["optNull"].Type.Nullable)
}

func TestModel_AdditionalPropertiesFalseClosed(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      additionalProperties: false
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosed, model.Additional)
}

// TestObject_MapAndNothingElseLowersToMapT pins docs/ir/containers.md at the
// lowering: an object whose one member fact is its catch-all is a MapT with
// string keys, however it is spelled, and reports nothing. `true` and `{}` are
// one schema, so they lower alike; the entry bounds land on the map; an empty
// property, pattern or required list declares no member; and an allOf that
// composes nothing structural leaves a map as a plain object does.
func TestObject_MapAndNothingElseLowersToMapT(t *testing.T) {
	t.Parallel()
	i64 := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		name, schema string
		value        ir.TypeID
		bounds       *ir.Constraints
	}{
		{"typed", `{type: object, additionalProperties: {type: integer}}`, "type/primitive/integer", nil},
		{"true", `{type: object, additionalProperties: true}`, "type/primitive/any", nil},
		{"empty schema", `{type: object, additionalProperties: {}}`, "type/primitive/any", nil},
		{"no members", `{type: object, properties: {}, patternProperties: {}, required: [], additionalProperties: {type: string}}`,
			"type/primitive/string", nil},
		{"nullable", `{type: [object, "null"], additionalProperties: {type: string}}`, "type/primitive/string", nil},
		{"bounded", `{type: object, additionalProperties: {type: string}, minProperties: 1, maxProperties: 5}`,
			"type/primitive/string", &ir.Constraints{MinProperties: i64(1), MaxProperties: i64(5)}},
		{"composed", `{allOf: [{type: object}], additionalProperties: {type: string}}`, "type/primitive/string", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    S: "+tc.schema+"\n"))
			assert.Empty(t, diags, "a map holds everything the position declared")
			mt, ok := doc.Types[componentID("S")].(*ir.MapT)
			require.True(t, ok, "a map and nothing else is a MapT; got %T", doc.Types[componentID("S")])
			assert.Nil(t, mt.Key, "JSON object keys are strings, which a nil Key states")
			assert.Equal(t, ir.TypeRef{Target: tc.value}, mt.Value)
			assert.Empty(t, cmp.Diff(tc.bounds, mt.Constraints))
		})
	}
}

// TestObject_MapKeepsItsDeclaration pins that converting to a MapT keeps what
// the position declared about the type: its docs, deprecation and extensions
// ride on TypeCommon unchanged.
func TestObject_MapKeepsItsDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    S:
      type: object
      description: Labels by key.
      deprecated: true
      x-owner: platform
      additionalProperties: {type: string}
`))
	assert.Empty(t, diags)
	mt, ok := doc.Types[componentID("S")].(*ir.MapT)
	require.True(t, ok, "got %T", doc.Types[componentID("S")])
	assert.Equal(t, "Labels by key.", mt.Docs.Description)
	assert.NotNil(t, mt.Deprecation)
	assert.Contains(t, mt.Unmodeled, "openapi:x-owner")
}

// TestObject_ACatchAllBesideARequiredNameStaysAModel pins an object whose one
// member fact beside its catch-all is a required name, plain or composed: the
// name lands in Model.Requires, which keeps it a Model, since a map requires no
// key, and nothing is kept verbatim or reported.
func TestObject_ACatchAllBesideARequiredNameStaysAModel(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]string{
		"plain":    "    S: {type: object, required: [a], additionalProperties: {type: string}}\n",
		"composed": "    S: {allOf: [{type: object}], additionalProperties: {type: string}, required: [a]}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(spec))
			model, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "got %T", doc.Types[componentID("S")])
			assert.Equal(t, []string{"a"}, model.Requires)
			require.NotNil(t, model.AdditionalProperties)
			assert.Equal(t, &ir.TypeRef{Target: "type/primitive/string"}, model.AdditionalProperties.Value)
			assert.NotContains(t, model.Unmodeled, "openapi:required")
			assert.Empty(t, diags)
		})
	}
}

// TestObject_CatchAllBesideAnotherFactStaysModel pins the other side of the map
// rule: a catch-all beside any other member fact is a Model's
// AdditionalProperties, and an object that writes no additionalProperties
// states no catch-all at all. An explicit `true` beside properties types the
// catch-all as the top type, as `{}` would, rather than reading as unwritten.
func TestObject_CatchAllBesideAnotherFactStaysModel(t *testing.T) {
	t.Parallel()
	const base = "    B: {type: object, properties: {b: {type: string}}}\n"
	anyRef := &ir.AdditionalProperties{Value: &ir.TypeRef{Target: "type/primitive/any"}}
	strRef := ir.TypeRef{Target: "type/primitive/string"}
	for _, tc := range []struct {
		name, schema string
		catchAll     *ir.AdditionalProperties
	}{
		{"properties", `{type: object, properties: {a: {type: string}}, additionalProperties: true}`, anyRef},
		{"patterns", `{type: object, patternProperties: {"^x-": {type: string}}, additionalProperties: {type: string}}`,
			&ir.AdditionalProperties{Value: &strRef, Patterns: []ir.PatternProperties{{Pattern: "^x-", Value: strRef}}}},
		{"patterns beside no catch-all", `{type: object, patternProperties: {"^x-": {type: string}}}`,
			&ir.AdditionalProperties{Value: anyRef.Value, Patterns: []ir.PatternProperties{{Pattern: "^x-", Value: strRef}}}},
		// A closed object admits no key the patterns miss, so nothing types one (#987).
		{"patterns beside a closure", `{type: object, patternProperties: {"^x-": {type: string}}, additionalProperties: false}`,
			&ir.AdditionalProperties{Patterns: []ir.PatternProperties{{Pattern: "^x-", Value: strRef}}}},
		{"discriminator", `{type: object, additionalProperties: {type: string}, discriminator: {propertyName: kind}}`,
			&ir.AdditionalProperties{Value: &strRef}},
		{"base", `{allOf: [{$ref: '#/components/schemas/B'}], additionalProperties: {type: string}}`,
			&ir.AdditionalProperties{Value: &strRef}},
		{"unwritten", `{type: object}`, nil},
		{"closed", `{type: object, additionalProperties: false}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(base+"    S: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "got %T", doc.Types[componentID("S")])
			assert.Empty(t, cmp.Diff(tc.catchAll, model.AdditionalProperties))
		})
	}
}

// TestModel_PatternPropertiesOrder pins that patterns land in the order the
// source writes them, written in their sorted order and against it, so
// neither a sort nor a reversal of them passes.
func TestModel_PatternPropertiesOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		patterns []string
	}{
		{name: "written in sorted order", patterns: []string{"^x-", "^y-"}},
		{name: "written against it", patterns: []string{"^y-", "^x-"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpec(fmt.Sprintf(`    S:
      type: object
      patternProperties:
        %q: {type: string}
        %q: {type: integer}
`, tc.patterns[0], tc.patterns[1]))
			doc, diags := schema.LowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "got %T", doc.Types[componentID("S")])
			require.NotNil(t, model.AdditionalProperties)
			var got []string
			for _, patternProperties := range model.AdditionalProperties.Patterns {
				got = append(got, patternProperties.Pattern)
			}
			assert.Equal(t, tc.patterns, got)
		})
	}
}

// TestModel_UnevaluatedPropertiesClosedAfterComposition pins where
// `unevaluatedProperties: false` is a model's openness: only where no
// additionalProperties stands beside it, which would evaluate every property it
// could apply to (docs/compilers/openapi.md#additional-properties). Beside one,
// the openness is additionalProperties' alone and the keyword is kept with it;
// beside patterns alone, nothing types a key no pattern matches.
func TestModel_UnevaluatedPropertiesClosedAfterComposition(t *testing.T) {
	t.Parallel()
	str, top := ir.TypeRef{Target: "type/primitive/string"}, ir.TypeRef{Target: "type/primitive/any"}
	patterns := []ir.PatternProperties{{Pattern: "^x-", Value: str}}
	const props, pattern = "properties: {a: {type: string}}", `patternProperties: {"^x-": {type: string}}`
	for _, tc := range []struct {
		name, schema string
		mode         ir.AdditionalMode
		catchAll     *ir.AdditionalProperties
		kept         string
	}{
		{"live", "{type: object, " + props + ", unevaluatedProperties: false}",
			ir.AdditionalClosedAfterComposition, nil, ""},
		{"live, untyped", "{" + props + ", unevaluatedProperties: false}",
			ir.AdditionalClosedAfterComposition, nil, ""},
		{"a null additionalProperties is none", "{type: object, " + props + ", additionalProperties: null, unevaluatedProperties: false}",
			ir.AdditionalClosedAfterComposition, nil, ""},
		{"beside true", "{type: object, " + props + ", additionalProperties: true, unevaluatedProperties: false}",
			"", &ir.AdditionalProperties{Value: &top}, `{"additionalProperties":true,"unevaluatedProperties":false}`},
		{"beside the empty schema", "{type: object, " + props + ", additionalProperties: {}, unevaluatedProperties: false}",
			"", &ir.AdditionalProperties{Value: &top}, `{"additionalProperties":{},"unevaluatedProperties":false}`},
		{"beside false", "{type: object, " + props + ", additionalProperties: false, unevaluatedProperties: false}",
			ir.AdditionalClosed, nil, `{"additionalProperties":false,"unevaluatedProperties":false}`},
		{"patterns beside a live closure", "{type: object, " + pattern + ", unevaluatedProperties: false}",
			ir.AdditionalClosedAfterComposition, &ir.AdditionalProperties{Patterns: patterns}, ""},
		{"patterns beside both closures", "{type: object, " + pattern + ", additionalProperties: false, unevaluatedProperties: false}",
			ir.AdditionalClosed, &ir.AdditionalProperties{Patterns: patterns},
			`{"additionalProperties":false,"unevaluatedProperties":false}`},
		{"patterns beside a catch-all", "{type: object, " + pattern + ", additionalProperties: {type: string}, unevaluatedProperties: false}",
			"", &ir.AdditionalProperties{Value: &str, Patterns: patterns},
			`{"additionalProperties":{"type":"string"},"unevaluatedProperties":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec("    S: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Empty(t, irverify.Verify(doc), "the IR holds its invariants")
			model, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "got %T", doc.Types[componentID("S")])
			assert.Equal(t, tc.mode, model.Additional)
			assert.Empty(t, cmp.Diff(tc.catchAll, model.AdditionalProperties))
			if tc.kept == "" {
				assert.NotContains(t, model.Unmodeled, "openapi:unevaluated", "a closure the model holds is kept nowhere else")
				return
			}
			require.Contains(t, model.Unmodeled, "openapi:unevaluated")
			assert.Equal(t, tc.kept, string(model.Unmodeled["openapi:unevaluated"].Value))
		})
	}
}

// TestObject_AnInertClosureLeavesAMap pins that an inert closure leaves a map a
// map: an object whose one member fact is its catch-all is a MapT, which holds
// the keyword beside the catch-all that leaves it inert.
func TestObject_AnInertClosureLeavesAMap(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    S: {type: object, additionalProperties: {type: integer}, unevaluatedProperties: false}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	mt, ok := doc.Types[componentID("S")].(*ir.MapT)
	require.True(t, ok, "got %T", doc.Types[componentID("S")])
	assert.Equal(t, ir.TypeRef{Target: "type/primitive/integer"}, mt.Value)
	require.Contains(t, mt.Unmodeled, "openapi:unevaluated")
	assert.Equal(t, `{"additionalProperties":{"type":"integer"},"unevaluatedProperties":false}`,
		string(mt.Unmodeled["openapi:unevaluated"].Value))
}
