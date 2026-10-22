package schema_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// nullShape asserts what a position lowers to, read off the component A.
type nullShape func(t *testing.T, td ir.TypeDef)

// asKind returns td as a T, failing the test where it is another kind.
func asKind[T ir.TypeDef](t *testing.T, td ir.TypeDef) T {
	t.Helper()
	got, ok := td.(T)
	require.True(t, ok, "lowered to %T", td)
	return got
}

// TestNullSchema_AnEntryIsTheEmptySchemaAndAKeywordIsLeftOut pins the reading
// of a `null` written where a schema belongs (GitHub #1045). The parser decodes
// one as the boolean `false` schema, which would close a model, forbid a
// property and make a conjunct admit nothing; the compiler reads it as no
// schema written. Each row is compared with its twin: an entry lowers as `{}`
// does, a keyword as if left out, and a branch as `true` does. The lowering
// reports what the twin does, and no false-schema: the load phase reports the
// null itself.
func TestNullSchema_AnEntryIsTheEmptySchemaAndAKeywordIsLeftOut(t *testing.T) {
	t.Parallel()
	top := ir.TypeRef{Target: "t/prim/any"}
	type nullRow struct {
		name    string
		version string // the OpenAPI version, 3.1.0 where empty
		null    string // the schemas, with a null written
		twin    string // the same, as the reading above states it
		shape   nullShape
		kept    map[string]string // what A keeps under Unmodeled, by key, as JSON
	}
	tests := []nullRow{
		{
			name: "a property",
			null: "    A:\n      type: object\n      properties:\n        p: null\n",
			twin: "    A:\n      type: object\n      properties:\n        p: {}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				m := asKind[*ir.Model](t, td)
				require.Len(t, m.Properties, 1)
				assert.Equal(t, top, m.Properties[0].Type, "the top type, and no node of its own")
			},
		},
		{
			name: "an allOf branch",
			null: "    A:\n      allOf:\n        - null\n        - {type: object, properties: {q: {type: string}}}\n",
			twin: "    A:\n      allOf:\n        - true\n        - {type: object, properties: {q: {type: string}}}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				m := asKind[*ir.Model](t, td)
				assert.Empty(t, m.Additional, "the composed model is not closed")
				require.Len(t, m.Properties, 1)
				assert.Equal(t, "q", m.Properties[0].WireName, "the other branch still composes")
			},
		},
		{
			name: "a oneOf branch",
			null: "    A:\n      oneOf:\n        - null\n        - {type: string}\n",
			twin: "    A:\n      oneOf:\n        - {}\n        - {type: string}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				u := asKind[*ir.Union](t, td)
				require.Len(t, u.Variants, 2)
				assert.Equal(t, top, u.Variants[0].Type)
			},
		},
		{
			name: "a component",
			null: "    A: null\n",
			twin: "    A: {}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				s := asKind[*ir.Scalar](t, td)
				assert.Equal(t, &top, s.Base, "a scalar alias over the top type")
			},
		},
		{
			name: "items",
			null: "    A:\n      type: array\n      items: null\n",
			twin: "    A:\n      type: array\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Equal(t, top, asKind[*ir.List](t, td).Elem, "as an absent items")
			},
		},
		{
			name: "a prefixItems entry",
			null: "    A:\n      type: array\n      prefixItems: [{type: string}, null]\n",
			twin: "    A:\n      type: array\n      prefixItems: [{type: string}, {}]\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				tup := asKind[*ir.Tuple](t, td)
				require.Len(t, tup.Elems, 2)
				assert.Equal(t, top, tup.Elems[1])
			},
		},
		{
			name: "the rest of a tuple, written as items",
			null: "    A:\n      type: array\n      prefixItems: [{type: string}]\n      items: null\n",
			twin: "    A:\n      type: array\n      prefixItems: [{type: string}]\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				tup := asKind[*ir.Tuple](t, td)
				assert.Equal(t, &top, tup.Rest, "an absent items admits anything after; false would admit nothing")
			},
		},
		{
			name: "additionalProperties",
			null: "    A:\n      type: object\n      properties: {a: {type: string}}\n" +
				"      additionalProperties: null\n",
			twin: "    A:\n      type: object\n      properties: {a: {type: string}}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				m := asKind[*ir.Model](t, td)
				assert.Empty(t, m.Additional, "the model stays open")
				assert.Nil(t, m.AdditionalProps, "no catch-all is written")
			},
		},
		{
			name: "a patternProperties value",
			null: "    A:\n      type: object\n      patternProperties:\n        '^x': null\n",
			twin: "    A:\n      type: object\n      patternProperties:\n        '^x': {}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				m := asKind[*ir.Model](t, td)
				require.NotNil(t, m.AdditionalProps)
				require.Len(t, m.AdditionalProps.Patterns, 1)
				assert.Equal(t, top, m.AdditionalProps.Patterns[0].Value)
			},
		},
		{
			name: "contentSchema",
			null: "    A:\n      type: string\n      contentMediaType: application/json\n      contentSchema: null\n",
			twin: "    A:\n      type: string\n      contentMediaType: application/json\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				s := asKind[*ir.Scalar](t, td)
				require.NotNil(t, s.Encoding, "the media type is still written")
				assert.Equal(t, "application/json", s.Encoding.MediaType)
				assert.Nil(t, s.Encoding.Schema, "no Encoding.Schema")
			},
		},
		{
			name: "unevaluatedProperties",
			null: "    A:\n      type: object\n      unevaluatedProperties: null\n",
			twin: "    A:\n      type: object\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Empty(t, asKind[*ir.Model](t, td).Additional, "the model stays open")
			},
		},
		{
			name: "unevaluatedItems",
			null: "    A:\n      type: array\n      unevaluatedItems: null\n",
			twin: "    A:\n      type: array\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Equal(t, top, asKind[*ir.List](t, td).Elem)
			},
		},
		{
			name: "not",
			null: "    A:\n      type: string\n      not: null\n",
			twin: "    A:\n      type: string\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Equal(t, &ir.TypeRef{Target: "t/prim/string"}, asKind[*ir.Scalar](t, td).Base)
			},
		},
		{
			name: "propertyNames",
			null: "    A:\n      type: object\n      propertyNames: null\n",
			twin: "    A:\n      type: object\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Empty(t, asKind[*ir.Model](t, td).Additional)
			},
		},
		{
			name:  "an if beside a then",
			null:  "    A:\n      type: string\n      if: null\n      then: {minLength: 2}\n",
			twin:  "    A:\n      type: string\n      then: {minLength: 2}\n",
			shape: func(*testing.T, ir.TypeDef) {},
			kept:  map[string]string{"openapi:if-then-else": `{"then":{"minLength":2}}`},
		},
		{
			name:  "a then beside a real if and else, which alone are kept",
			null:  "    A:\n      type: string\n      if: {minLength: 2}\n      then: null\n      else: {maxLength: 9}\n",
			twin:  "    A:\n      type: string\n      if: {minLength: 2}\n      else: {maxLength: 9}\n",
			shape: func(*testing.T, ir.TypeDef) {},
			kept:  map[string]string{"openapi:if-then-else": `{"if":{"minLength":2},"else":{"maxLength":9}}`},
		},
		{
			name:  "an else beside an if",
			null:  "    A:\n      type: string\n      if: {minLength: 2}\n      else: null\n",
			twin:  "    A:\n      type: string\n      if: {minLength: 2}\n",
			shape: func(*testing.T, ir.TypeDef) {},
			kept:  map[string]string{"openapi:if-then-else": `{"if":{"minLength":2}}`},
		},
		{
			name: "contains beside minContains, which alone is kept",
			null: "    A:\n      type: array\n      contains: null\n      minContains: 2\n",
			twin: "    A:\n      type: array\n      minContains: 2\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Equal(t, top, asKind[*ir.List](t, td).Elem)
			},
			kept: map[string]string{"openapi:contains": `{"minContains":2}`},
		},
		{
			name: "an allOf branch beside one admitting null",
			null: "    A:\n      type: object\n      properties:\n        p:\n          allOf:\n" +
				"            - null\n            - {type: [string, 'null']}\n",
			twin: "    A:\n      type: object\n      properties:\n        p:\n          allOf:\n" +
				"            - true\n            - {type: [string, 'null']}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				m := asKind[*ir.Model](t, td)
				require.Len(t, m.Properties, 1)
				assert.True(t, m.Properties[0].Type.Nullable, "a null branch no longer forbids null")
			},
		},
		{
			name: "a conjunct whose $ref lands on a null component, beside one admitting null",
			null: "    N: null\n    A:\n      type: object\n      properties:\n        p:\n          allOf:\n" +
				"            - {$ref: '#/components/schemas/N'}\n            - {type: [string, 'null']}\n",
			twin: "    N: {}\n    A:\n      type: object\n      properties:\n        p:\n          allOf:\n" +
				"            - {$ref: '#/components/schemas/N'}\n            - {type: [string, 'null']}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				m := asKind[*ir.Model](t, td)
				require.Len(t, m.Properties, 1)
				assert.True(t, m.Properties[0].Type.Nullable, "what the $ref lands on no longer forbids null")
			},
		},
		{
			name:    "items on an array of 3.0, which still warns that it has none",
			version: "3.0.3",
			null:    "    A:\n      type: array\n      items: null\n",
			twin:    "    A:\n      type: array\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Equal(t, top, asKind[*ir.List](t, td).Elem)
			},
		},
	}
	// A keyword beside a $ref is read at the property that writes it, where a
	// null is as if left out: the reference lowers to its target alone.
	keywords := []string{"not", "if", "then", "else", "contains", "propertyNames",
		"unevaluatedItems", "unevaluatedProperties"}
	besideRef := make([]nullRow, 0, len(keywords))
	for _, keyword := range keywords {
		const target = "    B: {type: string}\n    A:\n      type: object\n      properties:\n        p: "
		besideRef = append(besideRef, nullRow{
			name: "a " + keyword + " beside a $ref",
			null: target + "{$ref: '#/components/schemas/B', " + keyword + ": null}\n",
			twin: target + "{$ref: '#/components/schemas/B'}\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				m := asKind[*ir.Model](t, td)
				require.Len(t, m.Properties, 1)
				assert.Equal(t, ir.TypeRef{Target: "t/openapi/components/schemas/B"}, m.Properties[0].Type,
					"the target itself, with no alias over it")
			},
		})
	}
	tests = slices.Concat(tests, besideRef)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			version := tc.version
			if version == "" {
				version = "3.1.0"
			}
			doc, diags := lowerSpecOnly(t, openapitest.ComponentSpecVer(version, tc.null))
			twin, twinDiags := lowerSpecOnly(t, openapitest.ComponentSpecVer(version, tc.twin))
			assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "a null is no false schema: %+v", diags)

			a := typeByName(doc, "A")
			tc.shape(t, a)
			kept := a.Common().Unmodeled
			assert.Equal(t, slices.Sorted(maps.Keys(tc.kept)), unmodeledKeys(kept), "nothing else is kept")
			for key, raw := range tc.kept {
				assert.JSONEq(t, raw, string(kept[key].Value), key)
			}

			assert.Empty(t, cmp.Diff(twin.Types, doc.Types), "lowers as its twin does")
			assert.Empty(t, cmp.Diff(twinDiags, diags, cmpopts.EquateEmpty()), "and reports what its twin does")
		})
	}

	// Where nothing reads a null keyword, or only a predicate asking whether
	// the position wrote one, it is as if left out: the position lowers as its
	// twin does.
	type twinRow struct{ name, null, twin string }
	const union = "    A:\n      oneOf: [{type: string}, {type: integer}]\n"
	const list = "    B: {type: string}\n    A:\n      type: array\n      items: "
	alone := []twinRow{
		{"a null items beside a oneOf, which narrows nothing", union + "      items: null\n", union},
		{"a null additionalProperties beside a oneOf, which declares no shape",
			union + "      additionalProperties: null\n", union},
		{"a null contentSchema alone on a string, which writes no content vocabulary",
			"    A:\n      type: string\n      contentSchema: null\n", "    A:\n      type: string\n"},
		{"a null items on a string, which keeps no items for want of a home",
			"    A:\n      type: string\n      items: null\n", "    A:\n      type: string\n"},
		{"a null additionalProperties on an array, which keeps none either",
			"    A:\n      type: array\n      additionalProperties: null\n", "    A:\n      type: array\n"},
	}
	inItems := make([]twinRow, 0, len(keywords))
	for _, keyword := range keywords {
		inItems = append(inItems, twinRow{"a null " + keyword + " beside a $ref in items, which needs no alias",
			list + "{$ref: '#/components/schemas/B', " + keyword + ": null}\n", list + "{$ref: '#/components/schemas/B'}\n"})
	}
	alone = slices.Concat(alone, inItems)
	for _, tc := range alone {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpecOnly(t, openapitest.ComponentSpec(tc.null))
			twin, twinDiags := lowerSpecOnly(t, openapitest.ComponentSpec(tc.twin))

			assert.Empty(t, cmp.Diff(twin.Types, doc.Types), "lowers as it does with the keyword left out")
			assert.Empty(t, cmp.Diff(twinDiags, diags, cmpopts.EquateEmpty()), "and reports what that does")
		})
	}

	t.Run("a property redeclared as a null keeps the losing declaration as a null", func(t *testing.T) {
		t.Parallel()
		doc, diags := lowerSpecOnly(t, openapitest.ComponentSpec("    A:\n      allOf:\n"+
			"        - {type: object, properties: {p: {type: string}}}\n"+
			"        - {type: object, properties: {p: null}}\n"))

		m := asKind[*ir.Model](t, typeByName(doc, "A"))
		require.Len(t, m.Properties, 1)
		assert.Equal(t, ir.TypeRef{Target: "t/prim/string"}, m.Properties[0].Type, "the first declaration stands")
		const loser = "openapi:conflicting-redeclaration/components/schemas/A/allOf/1/properties/p"
		assert.Equal(t, []string{loser}, unmodeledKeys(m.Properties[0].Unmodeled))
		kept := m.Properties[0].Unmodeled[loser]
		assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
		assert.Equal(t, ir.RawValue("null"), kept.Value, "as written: a null, not the false the parser decodes it as")
		assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "%+v", diags)
	})
}
