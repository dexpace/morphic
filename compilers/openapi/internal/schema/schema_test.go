// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestLower_NamedScalarComponentResolves(t *testing.T) {
	t.Parallel()
	// A named component whose body is a plain scalar must register a resolvable
	// node at its own component pointer, so a $ref to it never dangles.
	spec := openapitest.ComponentSpec(`    MyId: {type: string, format: uuid}
    Holder:
      type: object
      properties:
        id: {$ref: "#/components/schemas/MyId"}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	scalar, ok := doc.Types[componentID("MyId")].(*ir.Scalar)
	require.True(t, ok, "named scalar component registers a Scalar at its own ID")
	require.NotNil(t, scalar.Base)
	assert.Equal(t, ir.TypeID("t/prim/uuid"), scalar.Base.Target)
	assert.Equal(t, "MyId", scalar.Name.Source, "the component name is preserved")

	holder := doc.Types[componentID("Holder")].(*ir.Model)
	assert.Equal(t, componentID("MyId"), holder.Properties[0].Type.Target)

	// The reference must resolve: the validate pass finds no dangling type ref.
	for _, d := range validated(t, doc) {
		assert.NotEqual(t, "ir/dangling-type-ref", d.Code, "ref to named scalar dangles: %+v", d)
	}
}

func TestLower_RecursiveSchemaTerminates(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Node:
      type: object
      properties:
        next: {$ref: "#/components/schemas/Node"}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	node, ok := doc.Types[componentID("Node")].(*ir.Model)
	require.True(t, ok)
	require.Equal(t, ir.TypeRef{Target: "t/openapi/components/schemas/Node"}, node.Properties[0].Type)
}

func TestSchemaRef_BooleanAndUntypedShapes(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        anything: true
        nothing: false
        untyped: {}
        withprops: {properties: {x: {type: string}}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "S").(*ir.Model)
	byWire := openapitest.PropsByWire(m.Properties)
	assert.Equal(t, ir.TypeID("t/prim/any"), byWire["anything"].Type.Target)
	// `false` schema lowered to a closed empty model.
	nothing := doc.Types[byWire["nothing"].Type.Target]
	require.NotNil(t, nothing)
	assert.Equal(t, ir.KindModel, nothing.Kind())
	assert.Equal(t, ir.AdditionalClosed, nothing.(*ir.Model).Additional)
	assert.Equal(t, ir.TypeID("t/prim/any"), byWire["untyped"].Type.Target)
	assert.Equal(t, ir.KindModel, doc.Types[byWire["withprops"].Type.Target].Kind())

	assert.True(t, openapitest.HasDiagAt(diags, diag.FalseSchema, ir.SeverityInfo), "false schema info diagnostic")
}

func TestSchemaRef_EmptyEitherIsAny(t *testing.T) {
	t.Parallel()
	l := schema.NewRawLowerer(&soa.OpenAPI{})
	ref, diags := schema.Ref(l.Ctx, l.Types, schema.TopLevelDepth, schema.Standalone(openapitest.EmptyEitherSchema()), "/p")
	assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
	assert.Empty(t, diags, "an empty either lowers to any without complaint")
}

// vocabCase is one JSON Schema 2020-12 keyword and the minimal document that
// writes it. schemas holds the literal "KW, " where the keyword goes, which a
// test replaces by write.
type vocabCase struct {
	keyword string
	write   string
	schemas string
}

// objectSchema, arraySchema and stringSchema are the three keyword domains the
// 2020-12 vocabularies split along, so each keyword is written where the
// specification gives it meaning.
const (
	objectSchema = "    A: {KW, type: object, properties: {p: {type: string}}}\n"
	arraySchema  = "    A: {KW, type: array, items: {type: string}}\n"
	stringSchema = "    A: {KW, type: string}\n"
)

// vocabularyCases enumerates every keyword of the JSON Schema 2020-12
// vocabularies — core (§8), applicator (§10) and unevaluated (§11) of the core
// specification, and validation (§6), format-annotation (§7), content (§8) and
// meta-data (§9) of the validation specification — each written where the
// specification gives it meaning, for a test that judges every keyword alike.
func vocabularyCases() []vocabCase {
	return append(append(append(
		vocabularyCore(), vocabularyApplicator()...), vocabularyValidation()...),
		vocabularyAnnotation()...)
}

func vocabularyCore() []vocabCase {
	return []vocabCase{
		{
			keyword: "$schema", schemas: stringSchema,
			write: "$schema: 'https://json-schema.org/draft/2020-12/schema'",
		},
		{
			keyword: "$vocabulary", schemas: stringSchema,
			write: "$vocabulary: {'https://json-schema.org/draft/2020-12/vocab/core': true}",
		},
		{keyword: "$id", write: "$id: 'urn:example:a'", schemas: stringSchema},
		{
			keyword: "$ref", schemas: objectSchema + "    B: {type: string}\n    C: {type: integer}\n",
			write: "$ref: '#/components/schemas/B'",
		},
		{
			keyword: "$dynamicRef", schemas: stringSchema + "    B: {$dynamicAnchor: m, type: string}\n" +
				"    C: {$dynamicAnchor: n, type: integer}\n",
			write: "$dynamicRef: '#m'",
		},
		{
			keyword: "$anchor", write: "$anchor: a", schemas: stringSchema,
		},
		{
			keyword: "$dynamicAnchor", write: "$dynamicAnchor: m", schemas: stringSchema,
		},
		{
			keyword: "$defs", write: "$defs: {D: {type: string}}", schemas: stringSchema,
		},
		{
			keyword: "$comment", write: "$comment: note", schemas: stringSchema,
		},
	}
}

func vocabularyApplicator() []vocabCase {
	return []vocabCase{
		{
			keyword: "allOf", schemas: objectSchema,
			write: "allOf: [{type: object, properties: {q: {type: integer}}}]",
		},
		{
			keyword: "anyOf", schemas: stringSchema,
			write: "anyOf: [{type: string}, {type: integer}]",
		},
		{
			keyword: "oneOf", schemas: stringSchema,
			write: "oneOf: [{type: string}, {type: integer}]",
		},
		{keyword: "not", write: "not: {const: n}", schemas: stringSchema},
		{keyword: "if", write: "if: {const: a}", schemas: stringSchema},
		{keyword: "then", write: "then: {const: a}", schemas: stringSchema},
		{keyword: "else", write: "else: {const: a}", schemas: stringSchema},
		{
			keyword: "dependentSchemas", schemas: objectSchema,
			write: "dependentSchemas: {p: {type: object}}",
		},
		{
			keyword: "prefixItems", schemas: arraySchema,
			write: "prefixItems: [{type: integer}]",
		},
		{
			keyword: "items", schemas: "    A: {KW, type: array}\n",
			write: "items: {type: integer}",
		},
		{
			keyword: "contains", schemas: arraySchema,
			write: "contains: {type: string}",
		},
		{
			keyword: "properties", schemas: "    A: {KW, type: object}\n",
			write: "properties: {p: {type: string}}",
		},
		{
			keyword: "patternProperties", schemas: objectSchema,
			write: "patternProperties: {'^a': {type: string}}",
		},
		{
			keyword: "additionalProperties", schemas: objectSchema,
			write: "additionalProperties: {type: integer}",
		},
		{
			keyword: "propertyNames", schemas: objectSchema,
			write: "propertyNames: {maxLength: 4}",
		},
		{
			keyword: "unevaluatedItems", schemas: arraySchema,
			write: "unevaluatedItems: {type: string}",
		},
		{
			keyword: "unevaluatedProperties", schemas: objectSchema,
			write: "unevaluatedProperties: false",
		},
		// Its false form again, where no model holds it as a closure: kept
		// verbatim, as every other form is (docs/compilers/openapi.md#validation-only).
		{
			keyword: "unevaluatedProperties", schemas: stringSchema,
			write: "unevaluatedProperties: false",
		},
	}
}

func vocabularyValidation() []vocabCase {
	const intSchema = "    A: {KW, type: integer}\n"
	return []vocabCase{
		{keyword: "type", write: "type: string", schemas: "    A: {KW, title: T}\n"},
		{keyword: "enum", write: "enum: [a, b]", schemas: stringSchema},
		{keyword: "const", write: "const: a", schemas: stringSchema},
		{keyword: "multipleOf", write: "multipleOf: 2", schemas: intSchema},
		{keyword: "maximum", write: "maximum: 9", schemas: intSchema},
		{keyword: "exclusiveMaximum", write: "exclusiveMaximum: 9", schemas: intSchema},
		{keyword: "minimum", write: "minimum: 1", schemas: intSchema},
		{keyword: "exclusiveMinimum", write: "exclusiveMinimum: 1", schemas: intSchema},
		{keyword: "maxLength", write: "maxLength: 9", schemas: stringSchema},
		{keyword: "minLength", write: "minLength: 1", schemas: stringSchema},
		{keyword: "pattern", write: "pattern: '^a'", schemas: stringSchema},
		{keyword: "maxItems", write: "maxItems: 9", schemas: arraySchema},
		{keyword: "minItems", write: "minItems: 1", schemas: arraySchema},
		{keyword: "uniqueItems", write: "uniqueItems: true", schemas: arraySchema},
		{keyword: "maxContains", write: "maxContains: 9", schemas: arraySchema},
		{keyword: "minContains", write: "minContains: 1", schemas: arraySchema},
		{keyword: "maxProperties", write: "maxProperties: 9", schemas: objectSchema},
		{keyword: "minProperties", write: "minProperties: 1", schemas: objectSchema},
		{keyword: "required", write: "required: [p]", schemas: objectSchema},
		{
			keyword: "dependentRequired", schemas: objectSchema,
			write: "dependentRequired: {p: [q]}",
		},
	}
}

func vocabularyAnnotation() []vocabCase {
	return []vocabCase{
		{keyword: "title", write: "title: T", schemas: stringSchema},
		{keyword: "description", write: "description: D", schemas: stringSchema},
		{keyword: "default", write: "default: a", schemas: stringSchema},
		{keyword: "deprecated", write: "deprecated: true", schemas: stringSchema},
		{keyword: "readOnly", write: "readOnly: true", schemas: stringSchema},
		{keyword: "writeOnly", write: "writeOnly: true", schemas: stringSchema},
		{keyword: "examples", write: "examples: [a]", schemas: stringSchema},
		{keyword: "format", write: "format: uuid", schemas: stringSchema},
		{
			keyword: "contentEncoding", schemas: stringSchema,
			write: "contentEncoding: base64",
		},
		{
			keyword: "contentMediaType", schemas: stringSchema,
			write: "contentMediaType: image/png",
		},
		{
			keyword: "contentSchema", schemas: stringSchema,
			write: "contentSchema: {type: object}",
		},
	}
}

// unpreservableValue is a value that is legal YAML and that JSON cannot spell:
// annotation.RawFromNode refuses NaN, so any preserved payload containing one
// fails whole. It is the shortest reachable trigger for the conversion failure
// the announcement tests need.
const unpreservableValue = ".nan"

// TestCoDeclaredFamily_PassedOverKeywordIsKept covers the families lower()'s
// first-match dispatch used to drop (GitHub #35).
//
// const, enum and allOf compete for one position and only the first declared was
// read; the rest left no Unmodeled entry and no diagnostic. `allOf: [{$ref:
// Base}]` beside an enum is the narrowing idiom rather than a malformed
// document, so what vanished was the entire relationship to Base.
//
// Each case names the family the dispatch elects and the one it passes over, so
// a reordered dispatch fails here rather than quietly changing which half of a
// schema the IR describes.
func TestCoDeclaredFamily_PassedOverKeywordIsKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, raw           string
		key, at, reportAt, report string
		lowered                   ir.TypeKind
	}{
		{
			name: "allOf beside enum", body: "      allOf: [{$ref: '#/components/schemas/Base'}]\n      enum: [a, b]\n",
			key: "openapi:$ref", at: "/components/schemas/S/allOf/0", reportAt: "/components/schemas/S/allOf/0",
			report: "the $ref is kept verbatim under Unmodeled", raw: `{"$ref":"#/components/schemas/Base"}`, lowered: ir.KindEnum,
		},
		{
			name: "allOf beside const", body: "      allOf: [{$ref: '#/components/schemas/Base'}]\n      const: a\n",
			key: "openapi:$ref", at: "/components/schemas/S/allOf/0", reportAt: "/components/schemas/S/allOf/0",
			report: "the $ref is kept verbatim under Unmodeled", raw: `{"$ref":"#/components/schemas/Base"}`, lowered: ir.KindLiteral,
		},
		{
			name: "enum beside const", body: "      const: a\n      enum: [a, b]\n",
			key: "openapi:enum", at: "/components/schemas/S/enum", reportAt: "/components/schemas/S",
			report: "enum kept verbatim under Unmodeled",
			raw:    `["a","b"]`, lowered: ir.KindLiteral,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(
				"    Base: {type: object, properties: {id: {type: string}}}\n    S:\n"+tc.body))
			openapitest.RequireNoErrorDiags(t, diags)

			td := typeByName(doc, "S")
			require.NotNil(t, td)
			assert.Equal(t, tc.lowered, td.Kind(), "the elected family still lowers")
			entry, ok := td.Common().Unmodeled[tc.key]
			require.True(t, ok, "%s is kept verbatim; got %v", tc.key, td.Common().Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.JSONEq(t, tc.raw, string(entry.Value))
			assert.Equal(t, jsontext.Pointer(tc.at), entry.Provenance.Pointer, "routable to where it was written")
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, tc.reportAt), tc.report)
		})
	}
}

// TestCoDeclaredFamily_EveryPassedOverKeywordIsKept pins a schema writing all
// three: it keeps the value set it did not elect, named in one report, and the
// allOf's $ref, a conjunct of the one it did, kept as a conjunct is.
func TestCoDeclaredFamily_EveryPassedOverKeywordIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Base: {type: object, properties: {id: {type: string}}}
    S:
      const: a
      enum: [a, b]
      allOf: [{$ref: '#/components/schemas/Base'}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	p := typeByName(doc, "S").Common().Unmodeled
	assert.Equal(t, []string{"openapi:$ref", "openapi:enum"}, unmodeledKeys(p))
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
		"declares enum beside its const")
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0"),
		"the $ref is kept verbatim under Unmodeled")
}

// TestCoDeclaredFamily_UnpreservableIsNotAnnounced pins the pairing GitHub #144
// exists for, at this site: the enum is elected over the allOf, whose entry
// keeps its type under its own key and fails to convert its extension, the
// failure is reported at the entry, and nothing claims the extension survived.
func TestCoDeclaredFamily_UnpreservableIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    S:
      allOf: [{type: object, x-t: `+unpreservableValue+`}]
      enum: [a, b]
`))

	td := typeByName(doc, "S")
	require.NotNil(t, td)
	assert.Equal(t, []string{"openapi:type"}, unmodeledKeys(td.Common().Unmodeled), "the conversion of x-t failed")
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning,
		"/components/schemas/S/allOf/0"), `extension "x-t" could not be serialized`, "the failure itself is reported")
	for _, claim := range openapitest.PreservationClaims(diags) {
		assert.NotContains(t, claim, "x-t", "nothing claims what was not written under Unmodeled")
	}
}

// unmodeledKeys returns p's keys in sorted order, which is what makes an
// expected key set assertable as a whole: a census that keeps too much fails the
// same test as one that keeps too little.
func unmodeledKeys(p ir.Unmodeled) []string {
	return slices.Sorted(maps.Keys(p))
}

// TestNullSchema_AnEntryIsTheEmptySchemaAndAKeywordIsLeftOut pins the reading
// of a `null` written where a schema belongs (GitHub #1045). The parser decodes
// one as the boolean `false` schema, which would close a model, forbid a
// property and make a conjunct admit nothing; the compiler reads it as no
// schema written. Each row is compared with its twin: an entry lowers as `{}`
// does, a keyword as if left out, and a branch as `true` or `{}` does. The
// lowering reports what the twin does, and no false-schema: the load phase
// reports the null itself.
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
			name: "items beside an unevaluatedItems, which it leaves live",
			null: "    A:\n      type: array\n      items: null\n      unevaluatedItems: false\n",
			twin: "    A:\n      type: array\n      unevaluatedItems: false\n",
			shape: func(t *testing.T, td ir.TypeDef) {
				assert.Equal(t, top, asKind[*ir.List](t, td).Elem)
			},
			kept: map[string]string{"openapi:unevaluated": `{"unevaluatedItems":false}`},
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

	// A null redeclaration is the top type, which loses to the string with
	// nothing to keep, as `{}` would. One is kept only beside a node the merge
	// cannot judge, such as the top type an enum past its member budget lowers
	// to, and then as the null it was written as.
	t.Run("a property redeclared as a null over a string is the string, as with {}", func(t *testing.T) {
		t.Parallel()
		const spec = "    A:\n      allOf:\n" +
			"        - {type: object, properties: {p: {type: string}}}\n" +
			"        - {type: object, properties: {p: %s}}\n"
		doc, diags := lowerSpecOnly(t, openapitest.ComponentSpec(fmt.Sprintf(spec, "null")))
		twin, twinDiags := lowerSpecOnly(t, openapitest.ComponentSpec(fmt.Sprintf(spec, "{}")))

		m := asKind[*ir.Model](t, typeByName(doc, "A"))
		require.Len(t, m.Properties, 1)
		assert.Equal(t, ir.TypeRef{Target: "t/prim/string"}, m.Properties[0].Type)
		assert.Empty(t, m.Properties[0].Unmodeled, "the top type says nothing the string loses")
		assert.Empty(t, cmp.Diff(twin.Types, doc.Types))
		assert.Empty(t, cmp.Diff(twinDiags, diags, cmpopts.EquateEmpty()))
	})

	t.Run("a property redeclared as a null keeps the losing declaration as a null", func(t *testing.T) {
		t.Parallel()
		l, _ := schema.LoweredFor(t, openapitest.ComponentSpec("    A:\n      allOf:\n"+
			"        - {type: object, properties: {p: null}}\n"+
			"        - {type: object, properties: {p: {type: string, enum: [a, b]}}}\n"))
		l.Ctx.Limits = lowering.Limits{MaxEnumMembers: budget.AtMost(1)}
		diags := schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas())

		m := asKind[*ir.Model](t, typeByName(l.Document(t), "A"))
		require.Len(t, m.Properties, 1)
		assert.Equal(t, ir.TypeID("t/anon/components/schemas/A/allOf/1/properties/p"), m.Properties[0].Type.Target,
			"the later declaration stands")
		const loser = "openapi:conflicting-redeclaration/components/schemas/A/allOf/0/properties/p"
		assert.Equal(t, []string{loser}, unmodeledKeys(m.Properties[0].Unmodeled))
		kept := m.Properties[0].Unmodeled[loser]
		assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
		assert.Equal(t, ir.RawValue("null"), kept.Value, "as written: a null, not the false the parser decodes it as")
		assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "%+v", diags)
	})
}

// nullShape asserts what a position lowers to, read off the component A.
type nullShape func(t *testing.T, td ir.TypeDef)

// asKind returns td as a T, failing the test where it is another kind.
func asKind[T ir.TypeDef](t *testing.T, td ir.TypeDef) T {
	t.Helper()
	got, ok := td.(T)
	require.True(t, ok, "lowered to %T", td)
	return got
}

// TestLowerComponentSchema_RefSiblingConstraintBindsTheSite locks in the
// $ref-sibling-constraint behaviour at a named component: lowerComponentSchema
// resolves the component's own site via annotation.At, so a bound written
// beside the $ref (S: {$ref: Target, minimum: 5}) binds S, not Target.
func TestLowerComponentSchema_RefSiblingConstraintBindsTheSite(t *testing.T) {
	t.Parallel()
	l, _ := schema.LoweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: integer}
    S:
      $ref: '#/components/schemas/Target'
      minimum: 5
`)
	l.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))

	sc, ok := typeByName(l.Document(t), "S").(*ir.Scalar)
	require.True(t, ok, "S aliases Target and must own a Scalar node")
	require.NotNil(t, sc.Constraints, "a bound beside a $ref binds S, not Target")
	require.NotNil(t, sc.Constraints.Min)
	assert.Equal(t, "5", sc.Constraints.Min.String())

	target := typeByName(l.Document(t), "Target")
	require.NotNil(t, target)
	tsc, ok := target.(*ir.Scalar)
	require.True(t, ok, "Target aliases a primitive and must own a Scalar node")
	assert.Nil(t, tsc.Constraints, "the referent must not acquire the site's bound")
}

// TestLowerComponentSchemas_PercentEncodedRefResolves pins that a $ref whose
// fragment is percent-encoded reaches the component it names. A $ref is a URI, so
// `#/components/schemas/Foo%2DBar` addresses "Foo-Bar", and the resolver reads it
// that way; comparing the raw fragment against declared names instead called a
// resolved reference unresolved and left the property as `any`, losing the type
// from a spec-correct document (GitHub #40). Each name here is legal under
// OpenAPI's own component-name rule (^[a-zA-Z0-9.\-_]+$), so the escape is the
// only thing under test.
func TestLowerComponentSchemas_PercentEncodedRefResolves(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, decl, encoded string }{
		{"hyphen", "Foo-Bar", "Foo%2DBar"},
		{"underscore", "Foo_Bar", "Foo%5FBar"},
		{"dot", "Foo.Bar", "Foo%2EBar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(
				"    "+tc.decl+": {type: string}\n"+
					"    User: {type: object, properties: {x: {$ref: '#/components/schemas/"+tc.encoded+"'}}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			user, ok := typeByName(doc, "User").(*ir.Model)
			require.True(t, ok, "User must own a Model node")
			require.Len(t, user.Properties, 1)
			assert.Equal(t, componentID(tc.decl), user.Properties[0].Type.Target,
				"the encoded fragment names the declared component")
		})
	}
}

// TestLowerComponentSchemas_PercentEncodedRefHoistsAtTheDeclaredCoordinate pins
// the identity half of the same fix, which the resolution half hides: a pointer
// *through* an encoded component name addresses a sub-schema an unencoded
// pointer also addresses, so the two must intern one node, at the declared
// coordinate and not at `.../Foo%2DBar/properties/inner`, a path no source
// coordinate spells (the derivation GitHub #141 refused for anchors). A second
// node raises no diagnostic. Both spellings appear because the encoded ref
// alone lands on one node either way, and only its name would be wrong.
func TestLowerComponentSchemas_PercentEncodedRefHoistsAtTheDeclaredCoordinate(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(
		"    Foo-Bar: {type: object, properties: {inner: {type: object, properties: {n: {type: integer}}}}}\n"+
			"    User:\n      type: object\n      properties:\n"+
			"        a: {$ref: '#/components/schemas/Foo-Bar/properties/inner'}\n"+
			"        b: {$ref: '#/components/schemas/Foo%2DBar/properties/inner'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	user, ok := typeByName(doc, "User").(*ir.Model)
	require.True(t, ok, "User must own a Model node")
	props := openapitest.PropsByWire(user.Properties)
	require.Len(t, props, 2)

	const want = ir.TypeID("t/anon/components/schemas/Foo-Bar/properties/inner")
	assert.Equal(t, want, props["a"].Type.Target)
	assert.Equal(t, want, props["b"].Type.Target,
		"the encoded spelling addresses the position the declaration spells, not one of its own")
	assert.Contains(t, doc.Types, want, "and that ID is backed by a node")
	for id := range doc.Types {
		assert.NotContains(t, string(id), "%",
			"no node is interned at an encoded path: nothing in this document is named with one")
	}
}

// TestLowerComponentSchemas_PercentEncodedDiscriminatorMapping covers the third
// consumer of the pointer, and the one whose failure is loudest: a mapping entry
// whose target does not resolve is dropped, so an encoded target silently cost
// the union a branch of its polymorphic dispatch rather than merely degrading a
// position's type. InternalPointer's contract has always named discriminator
// mappings alongside $ref; nothing exercised that half.
func TestLowerComponentSchemas_PercentEncodedDiscriminatorMapping(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(
		"    Cat-A: {type: object, properties: {kind: {type: string}}}\n"+
			"    Pet:\n      oneOf: [{$ref: '#/components/schemas/Cat-A'}]\n"+
			"      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat%2DA'}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok, "Pet must own a Union node")
	require.NotNil(t, pet.Discriminator, "the discriminator survives lowering")
	assert.Equal(t, map[string]ir.TypeID{"cat": componentID("Cat-A")}, pet.Discriminator.Mapping,
		"the encoded mapping target names the declared component, and the entry is kept")
}

// TestUntypedKinds_ACutReadLeavesTheKeywordsToThePolicy pins a schema writing
// no type whose statement lies on a cycle of conjunct edges, a union branch
// naming it, so the run's admission cannot read what it names: its shape
// keywords decide, under the kind-inference policy and marked by it, and with
// the policy off it is the top type keeping them.
func TestUntypedKinds_ACutReadLeavesTheKeywordsToThePolicy(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.1.0",
		"    A: {oneOf: [{$ref: '#/components/schemas/A'}], anyOf: [{type: object}], properties: {a: {type: string}}}\n")
	for _, off := range []bool{false, true} {
		doc, diags := schema.LowerSpecWith(t, spec, lowering.Policy{KindInference: lowering.KindInference{Disabled: off}})
		openapitest.RequireNoErrorDiags(t, diags)
		td := typeByName(doc, "A")
		marked := td.Common().Provenance.Inferred == lowering.KindInferenceHeuristic
		if off {
			assert.Equal(t, ir.KindScalar, td.Kind(), "the top type with the policy off")
			assert.Contains(t, td.Common().Unmodeled, "openapi:properties")
			assert.False(t, marked)
			continue
		}
		assert.Equal(t, ir.KindModel, td.Kind(), "the policy reads its properties as an object")
		assert.True(t, marked)
	}
}
