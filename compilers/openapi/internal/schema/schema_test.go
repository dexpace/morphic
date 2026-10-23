package schema_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
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
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	scalar, ok := doc.Types[componentID("MyId")].(*ir.Scalar)
	require.True(t, ok, "named scalar component registers a Scalar at its own ID")
	require.NotNil(t, scalar.Base)
	assert.Equal(t, ir.TypeID("t/prim/uuid"), scalar.Base.Target)
	assert.Equal(t, "MyId", scalar.Name.Source, "the component name is preserved")

	holder := doc.Types[componentID("Holder")].(*ir.Model)
	assert.Equal(t, componentID("MyId"), holder.Properties[0].Type.Target)

	// The reference must resolve: the validate pass finds no dangling type ref.
	for _, d := range pass.Validate(doc) {
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
	doc, diags := lowerSpec(t, spec)
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
	doc, diags := lowerSpec(t, spec)
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
	l := newRawLowerer(&soa.OpenAPI{})
	ref, diags := schema.Ref(l.ctx, l.types, &l.anchors, schema.TopLevelDepth, openapitest.EmptyEitherSchema(), "/p", "h")
	assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
	assert.Empty(t, diags, "an empty either lowers to any without complaint")
}

// vocabCase is one JSON Schema 2020-12 keyword and the minimal document that
// writes it. schemas must contain the literal "KW, " where the keyword goes: the
// control replaces that marker with nothing, so the two documents differ in
// exactly this one keyword.
//
// write and alt write the same keyword with *different* values. A keyword whose
// presence reshapes the IR while its value is discarded passes a presence check
// and fails this one, which is the whole difference between "the keyword was
// noticed" and "the keyword was carried". excluded, when set, is why nothing
// carries it, and leaves alt unused.
type vocabCase struct {
	keyword  string
	write    string
	alt      string
	schemas  string
	excluded string
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
// vocabularies — core (§8), applicator (§10), unevaluated (§11), validation
// (§6), meta-data (§9), format-annotation (§7) and content (§8.3) — so a keyword
// the compiler neither lowers nor keeps fails loudly instead of joining the ones
// GitHub #125 found by hand.
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
			alt:   "$schema: 'https://json-schema.org/draft/2019-09/schema'",
		},
		{
			keyword: "$vocabulary", schemas: stringSchema,
			write: "$vocabulary: {'https://json-schema.org/draft/2020-12/vocab/core': true}",
			alt:   "$vocabulary: {'https://json-schema.org/draft/2020-12/vocab/validation': true}",
		},
		{keyword: "$id", write: "$id: 'urn:example:a'", alt: "$id: 'urn:example:z'", schemas: stringSchema},
		{
			keyword: "$ref", schemas: objectSchema + "    B: {type: string}\n    C: {type: integer}\n",
			write: "$ref: '#/components/schemas/B'", alt: "$ref: '#/components/schemas/C'",
		},
		{
			keyword: "$dynamicRef", schemas: stringSchema + "    B: {$dynamicAnchor: m, type: string}\n",
			write: "$dynamicRef: '#m'", alt: "$dynamicRef: '#n'",
		},
		{
			keyword: "$anchor", write: "$anchor: a", schemas: stringSchema,
			excluded: "read as a reference target: the binder resolves $ref: '#a' to the schema " +
				"declaring it (docs/compilers/openapi.md#references), so the reference carries it; " +
				"declaring one says nothing about the shape",
		},
		{
			keyword: "$dynamicAnchor", write: "$dynamicAnchor: m", schemas: stringSchema,
			excluded: "read as a reference target by dynamicAnchors, which is what lets a " +
				"$dynamicRef expand; declaring one says nothing about the shape",
		},
		{
			keyword: "$defs", write: "$defs: {D: {type: string}}", schemas: stringSchema,
			excluded: "declares nothing about the enclosing shape; a member is lowered on demand " +
				"when a $ref names it (resolveSchemaRef -> hoistSubSchema)",
		},
		{
			keyword: "$comment", write: "$comment: note", schemas: stringSchema,
			excluded: "2020-12 §8.3 forbids presenting it to end users, so an SDK emitter must " +
				"never see it",
		},
	}
}

func vocabularyApplicator() []vocabCase {
	return []vocabCase{
		{
			keyword: "allOf", schemas: objectSchema,
			write: "allOf: [{type: object, properties: {q: {type: integer}}}]",
			alt:   "allOf: [{type: object, properties: {q: {type: boolean}}}]",
		},
		{
			keyword: "anyOf", schemas: stringSchema,
			write: "anyOf: [{type: string}, {type: integer}]",
			alt:   "anyOf: [{type: string}, {type: boolean}]",
		},
		{
			keyword: "oneOf", schemas: stringSchema,
			write: "oneOf: [{type: string}, {type: integer}]",
			alt:   "oneOf: [{type: string}, {type: boolean}]",
		},
		{keyword: "not", write: "not: {const: n}", alt: "not: {const: z}", schemas: stringSchema},
		{keyword: "if", write: "if: {const: a}", alt: "if: {const: z}", schemas: stringSchema},
		{keyword: "then", write: "then: {const: a}", alt: "then: {const: z}", schemas: stringSchema},
		{keyword: "else", write: "else: {const: a}", alt: "else: {const: z}", schemas: stringSchema},
		{
			keyword: "dependentSchemas", schemas: objectSchema,
			write: "dependentSchemas: {p: {type: object}}", alt: "dependentSchemas: {p: {type: array}}",
		},
		{
			keyword: "prefixItems", schemas: arraySchema,
			write: "prefixItems: [{type: integer}]", alt: "prefixItems: [{type: boolean}]",
		},
		{
			keyword: "items", schemas: "    A: {KW, type: array}\n",
			write: "items: {type: integer}", alt: "items: {type: boolean}",
		},
		{
			keyword: "contains", schemas: arraySchema,
			write: "contains: {type: string}", alt: "contains: {type: boolean}",
		},
		{
			keyword: "properties", schemas: "    A: {KW, type: object}\n",
			write: "properties: {p: {type: string}}", alt: "properties: {p: {type: boolean}}",
		},
		{
			keyword: "patternProperties", schemas: objectSchema,
			write: "patternProperties: {'^a': {type: string}}", alt: "patternProperties: {'^a': {type: boolean}}",
		},
		{
			keyword: "additionalProperties", schemas: objectSchema,
			write: "additionalProperties: {type: integer}", alt: "additionalProperties: {type: boolean}",
		},
		{
			keyword: "propertyNames", schemas: objectSchema,
			write: "propertyNames: {maxLength: 4}", alt: "propertyNames: {maxLength: 5}",
		},
		{
			keyword: "unevaluatedItems", schemas: arraySchema,
			write: "unevaluatedItems: {type: string}", alt: "unevaluatedItems: {type: boolean}",
		},
		{
			keyword: "unevaluatedProperties", schemas: objectSchema,
			write: "unevaluatedProperties: false", alt: "unevaluatedProperties: {type: string}",
		},
	}
}

func vocabularyValidation() []vocabCase {
	const intSchema = "    A: {KW, type: integer}\n"
	return []vocabCase{
		{keyword: "type", write: "type: string", alt: "type: boolean", schemas: "    A: {KW, title: T}\n"},
		{keyword: "enum", write: "enum: [a, b]", alt: "enum: [y, z]", schemas: stringSchema},
		{keyword: "const", write: "const: a", alt: "const: z", schemas: stringSchema},
		{keyword: "multipleOf", write: "multipleOf: 2", alt: "multipleOf: 3", schemas: intSchema},
		{keyword: "maximum", write: "maximum: 9", alt: "maximum: 8", schemas: intSchema},
		{keyword: "exclusiveMaximum", write: "exclusiveMaximum: 9", alt: "exclusiveMaximum: 8", schemas: intSchema},
		{keyword: "minimum", write: "minimum: 1", alt: "minimum: 2", schemas: intSchema},
		{keyword: "exclusiveMinimum", write: "exclusiveMinimum: 1", alt: "exclusiveMinimum: 2", schemas: intSchema},
		{keyword: "maxLength", write: "maxLength: 9", alt: "maxLength: 8", schemas: stringSchema},
		{keyword: "minLength", write: "minLength: 1", alt: "minLength: 2", schemas: stringSchema},
		{keyword: "pattern", write: "pattern: '^a'", alt: "pattern: '^z'", schemas: stringSchema},
		{keyword: "maxItems", write: "maxItems: 9", alt: "maxItems: 8", schemas: arraySchema},
		{keyword: "minItems", write: "minItems: 1", alt: "minItems: 2", schemas: arraySchema},
		{keyword: "uniqueItems", write: "uniqueItems: true", alt: "uniqueItems: false", schemas: arraySchema},
		{keyword: "maxContains", write: "maxContains: 9", alt: "maxContains: 8", schemas: arraySchema},
		{keyword: "minContains", write: "minContains: 1", alt: "minContains: 2", schemas: arraySchema},
		{keyword: "maxProperties", write: "maxProperties: 9", alt: "maxProperties: 8", schemas: objectSchema},
		{keyword: "minProperties", write: "minProperties: 1", alt: "minProperties: 2", schemas: objectSchema},
		{keyword: "required", write: "required: [p]", alt: "required: []", schemas: objectSchema},
		{
			keyword: "dependentRequired", schemas: objectSchema,
			write: "dependentRequired: {p: [q]}", alt: "dependentRequired: {p: [z]}",
		},
	}
}

func vocabularyAnnotation() []vocabCase {
	return []vocabCase{
		{keyword: "title", write: "title: T", alt: "title: Z", schemas: stringSchema},
		{keyword: "description", write: "description: D", alt: "description: Z", schemas: stringSchema},
		{keyword: "default", write: "default: a", alt: "default: z", schemas: stringSchema},
		{keyword: "deprecated", write: "deprecated: true", alt: "deprecated: false", schemas: stringSchema},
		{keyword: "readOnly", write: "readOnly: true", alt: "readOnly: false", schemas: stringSchema},
		{keyword: "writeOnly", write: "writeOnly: true", alt: "writeOnly: false", schemas: stringSchema},
		{keyword: "examples", write: "examples: [a]", alt: "examples: [z]", schemas: stringSchema},
		{keyword: "format", write: "format: uuid", alt: "format: date", schemas: stringSchema},
		{
			keyword: "contentEncoding", schemas: stringSchema,
			write: "contentEncoding: base64", alt: "contentEncoding: base32",
		},
		{
			keyword: "contentMediaType", schemas: stringSchema,
			write: "contentMediaType: image/png", alt: "contentMediaType: text/csv",
		},
		{
			keyword: "contentSchema", schemas: stringSchema,
			write: "contentSchema: {type: object}", alt: "contentSchema: {type: array}",
		},
	}
}

// TestVocabulary2020_12_EveryKeywordIsLoweredOrKept compiles each 2020-12
// keyword with two values and once omitted, and requires all three IR documents
// to differ. A keyword the compiler neither lowers nor keeps verbatim yields an
// identical document, the silent drop of GitHub #125.
//
// Two values are needed because a keyword whose mere presence reshapes the IR
// moves the document even if its value is discarded. One pair per keyword is a
// floor on what reaches the IR, not an inventory.
//
// Each excluded row states why nothing carries its keyword and is held to
// producing no difference, so a newly carried keyword must move its
// justification.
func TestVocabulary2020_12_EveryKeywordIsLoweredOrKept(t *testing.T) {
	t.Parallel()
	for _, tc := range vocabularyCases() {
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			require.Contains(t, tc.schemas, "KW, ", "the template must mark where the keyword goes")

			with := compileVocabIR(t, strings.Replace(tc.schemas, "KW, ", tc.write+", ", 1))
			without := compileVocabIR(t, strings.Replace(tc.schemas, "KW, ", "", 1))

			if tc.excluded != "" {
				assert.Equal(t, without, with, "%s is deliberately not carried: %s", tc.keyword, tc.excluded)
				return
			}
			require.NotEmpty(t, tc.alt, "a carried keyword needs a second value to be checked against")
			assert.NotEqual(t, without, with,
				"%s changed nothing in the IR, so it is neither lowered nor kept verbatim", tc.keyword)

			other := compileVocabIR(t, strings.Replace(tc.schemas, "KW, ", tc.alt+", ", 1))
			assert.NotEqual(t, with, other,
				"%s reshaped the IR by being present but two values of it produced one document, "+
					"so the value itself is read and discarded", tc.keyword)
		})
	}
}

// compileVocabIR compiles a components/schemas block and renders the IR it
// produced. Two fields are cleared first: diagnostics, because a keyword that is
// only announced and not lowered or kept is still lost, and Sources, whose
// content hash differs for any two distinct source texts and would make every
// case "differ" for free.
func compileVocabIR(t *testing.T, schemas string) string {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpec(schemas))
	openapitest.RequireNoErrorDiags(t, diags)
	doc.Diagnostics = nil
	doc.Sources = nil
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(out)
}

// unpreservableValue is a value that is legal YAML, decodes, and then cannot be
// marshalled as JSON: json.Marshal rejects NaN, so any preserved payload
// containing one fails whole. It is the shortest reachable trigger for the
// conversion failure the announcement tests need.
const unpreservableValue = ".nan"

// preservationClaims returns the messages announcing that something was kept
// under Unmodeled. The unpreservable report is excluded by code rather than by
// wording: it says a construct "could not be kept verbatim under Unmodeled",
// which contains the phrase and is the opposite of a claim.
func preservationClaims(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if strings.Contains(d.Message, "under Unmodeled") && d.Code != diag.UnpreservableConstruct {
			out = append(out, d.Message)
		}
	}
	return out
}

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
		name, body, skipped, raw string
		lowered                  ir.TypeKind
	}{
		{
			name:    "allOf beside enum",
			body:    "      allOf: [{$ref: '#/components/schemas/Base'}]\n      enum: [a, b]\n",
			skipped: "allOf",
			raw:     `[{"$ref":"#/components/schemas/Base"}]`,
			lowered: ir.KindEnum,
		},
		{
			name:    "allOf beside const",
			body:    "      allOf: [{$ref: '#/components/schemas/Base'}]\n      const: a\n",
			skipped: "allOf",
			raw:     `[{"$ref":"#/components/schemas/Base"}]`,
			lowered: ir.KindLiteral,
		},
		{
			name:    "enum beside const",
			body:    "      const: a\n      enum: [a, b]\n",
			skipped: "enum",
			raw:     `["a","b"]`,
			lowered: ir.KindLiteral,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, openapitest.ComponentSpec(
				"    Base: {type: object, properties: {id: {type: string}}}\n    S:\n"+tc.body))
			openapitest.RequireNoErrorDiags(t, diags)

			td := typeByName(doc, "S")
			require.NotNil(t, td)
			assert.Equal(t, tc.lowered, td.Kind(), "the elected family still lowers")
			entry, ok := td.Common().Unmodeled["openapi:"+tc.skipped]
			require.True(t, ok, "%s is kept verbatim; got %v", tc.skipped, td.Common().Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.JSONEq(t, tc.raw, string(entry.Value))
			assert.Equal(t, jsontext.Pointer("/components/schemas/S/"+tc.skipped), entry.Provenance.Pointer,
				"routable to where it was written")
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
				tc.skipped+" kept verbatim under Unmodeled")
		})
	}
}

// TestCoDeclaredFamily_EveryPassedOverKeywordIsKept pins the plural arm: a
// schema writing all three keeps both of the two it did not elect, named
// together in one report rather than one of them standing in for the rest.
func TestCoDeclaredFamily_EveryPassedOverKeywordIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    Base: {type: object, properties: {id: {type: string}}}
    S:
      const: a
      enum: [a, b]
      allOf: [{$ref: '#/components/schemas/Base'}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	p := typeByName(doc, "S").Common().Unmodeled
	assert.Contains(t, p, "openapi:enum")
	assert.Contains(t, p, "openapi:allOf")
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
		"declares enum and allOf beside its const",
		"both are named in the one report")
}

// TestCoDeclaredFamily_UnpreservableIsNotAnnounced pins the pairing GitHub #144
// exists for, at this site: the allOf fails to convert, so the enum lowers, the
// failure is reported at the keyword, and nothing claims the composition
// survived.
func TestCoDeclaredFamily_UnpreservableIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    S:
      allOf: [{type: object, x-t: `+unpreservableValue+`}]
      enum: [a, b]
`))

	td := typeByName(doc, "S")
	require.NotNil(t, td)
	assert.NotContains(t, td.Common().Unmodeled, "openapi:allOf", "the conversion failed")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct), "the failure itself is reported")
	assert.Empty(t, preservationClaims(diags),
		"nothing was written under Unmodeled, so nothing may announce that it was")
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
		l, _ := loweredFor(t, openapitest.ComponentSpec("    A:\n      allOf:\n"+
			"        - {type: object, properties: {p: {type: string, enum: [a, b]}}}\n"+
			"        - {type: object, properties: {p: null}}\n"))
		l.ctx.Limits = lowering.Limits{MaxEnumMembers: 1}
		diags := schema.LowerComponentSchemas(t.Context(), l.ctx, l.types, &l.anchors)

		m := asKind[*ir.Model](t, typeByName(l.document(t), "A"))
		require.Len(t, m.Properties, 1)
		assert.Equal(t, ir.TypeID("t/anon/components/schemas/A/allOf/0/properties/p"), m.Properties[0].Type.Target,
			"the first declaration stands")
		const loser = "openapi:conflicting-redeclaration/components/schemas/A/allOf/1/properties/p"
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
