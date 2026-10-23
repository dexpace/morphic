package schema_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
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
	m, ok := doc.Types[componentID("Plain")].(*ir.Model)
	require.True(t, ok, "Plain should be a model")
	require.Len(t, m.Properties, 1)
	assert.True(t, m.Properties[0].Required, "id is declared, so it is required on its property")
	assert.Equal(t, []string{"ghost"}, m.Requires, "ghost is listed once, however often it is written")
	for _, d := range diags {
		assert.True(t, strings.HasPrefix(d.Code, diag.Validation),
			"the repeat draws the validation's finding, and the lowering adds none: %+v", d)
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
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 4)
	byName := openapitest.PropsByWire(m.Properties)
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
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
}

// TestObject_MapAndNothingElseLowersToMapT pins ir-design §4.6 at the
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
		{"typed", `{type: object, additionalProperties: {type: integer}}`, "t/prim/integer", nil},
		{"true", `{type: object, additionalProperties: true}`, "t/prim/any", nil},
		{"empty schema", `{type: object, additionalProperties: {}}`, "t/prim/any", nil},
		{"no members", `{type: object, properties: {}, patternProperties: {}, required: [], additionalProperties: {type: string}}`,
			"t/prim/string", nil},
		{"nullable", `{type: [object, "null"], additionalProperties: {type: string}}`, "t/prim/string", nil},
		{"bounded", `{type: object, additionalProperties: {type: string}, minProperties: 1, maxProperties: 5}`,
			"t/prim/string", &ir.Constraints{MinProps: i64(1), MaxProps: i64(5)}},
		{"composed", `{allOf: [{type: object}], additionalProperties: {type: string}}`, "t/prim/string", nil},
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
			m, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "got %T", doc.Types[componentID("S")])
			assert.Equal(t, []string{"a"}, m.Requires)
			require.NotNil(t, m.AdditionalProps)
			assert.Equal(t, ir.TypeRef{Target: "t/prim/string"}, m.AdditionalProps.Value)
			assert.NotContains(t, m.Unmodeled, "openapi:required")
			assert.Empty(t, diags)
		})
	}
}

// TestObject_CatchAllBesideAnotherFactStaysModel pins the other side of the map
// rule: a catch-all beside any other member fact is a Model's AdditionalProps,
// and an object that writes no additionalProperties states no catch-all at all.
// An explicit `true` beside properties types the catch-all as the top type, as
// `{}` would, rather than reading as unwritten.
func TestObject_CatchAllBesideAnotherFactStaysModel(t *testing.T) {
	t.Parallel()
	const base = "    B: {type: object, properties: {b: {type: string}}}\n"
	anyRef := &ir.AdditionalProps{Value: ir.TypeRef{Target: "t/prim/any"}}
	strRef := ir.TypeRef{Target: "t/prim/string"}
	for _, tc := range []struct {
		name, schema string
		catchAll     *ir.AdditionalProps
	}{
		{"properties", `{type: object, properties: {a: {type: string}}, additionalProperties: true}`, anyRef},
		{"patterns", `{type: object, patternProperties: {"^x-": {type: string}}, additionalProperties: {type: string}}`,
			&ir.AdditionalProps{Value: strRef, Patterns: []ir.PatternProps{{Pattern: "^x-", Value: strRef}}}},
		// Inert beside a catch-all in JSON Schema, but still read as a closure (#862).
		{"closed after composition", `{type: object, additionalProperties: {type: string}, unevaluatedProperties: false}`,
			&ir.AdditionalProps{Value: strRef}},
		{"discriminator", `{type: object, additionalProperties: {type: string}, discriminator: {propertyName: kind}}`,
			&ir.AdditionalProps{Value: strRef}},
		{"base", `{allOf: [{$ref: '#/components/schemas/B'}], additionalProperties: {type: string}}`,
			&ir.AdditionalProps{Value: strRef}},
		{"unwritten", `{type: object}`, nil},
		{"closed", `{type: object, additionalProperties: false}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(base+"    S: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "got %T", doc.Types[componentID("S")])
			assert.Empty(t, cmp.Diff(tc.catchAll, m.AdditionalProps))
		})
	}
}

func TestModel_PatternPropertiesOrder(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      patternProperties:
        "^x-": {type: string}
        "^y-": {type: integer}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.AdditionalProps)
	require.Len(t, m.AdditionalProps.Patterns, 2)
	assert.Equal(t, "^x-", m.AdditionalProps.Patterns[0].Pattern)
	assert.Equal(t, "^y-", m.AdditionalProps.Patterns[1].Pattern)
}

func TestModel_UnevaluatedPropertiesClosedAfterComposition(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      unevaluatedProperties: false
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosedAfterComposition, m.Additional)
}
