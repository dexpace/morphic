package schema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestUnhomedApplicator_KeptOnAListAndAnnounced pins the arm of the home
// question that a list answers: items and prefixItems have a home on it, and
// anything else written beside them does not, so `properties` on an array is
// kept verbatim and reported rather than silently dropped.
func TestUnhomedApplicator_KeptOnAListAndAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    Odd:
      type: array
      items: {type: string}
      properties: {p: {type: string}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	td := typeByName(doc, "Odd")
	require.NotNil(t, td, "the array still lowers")
	assert.Contains(t, td.Common().Unmodeled, "openapi:properties",
		"a list has no home for properties, so it is kept")
	assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"and the position says which keywords it could not carry")
}

// TestUnhomedApplicator_ObjectCarriesNoItemKeyword pins the other arm of the
// home question. A model has a home for the property applicators and for
// nothing else, so `items` written on an object is kept verbatim rather than
// read as though the object were an array.
func TestUnhomedApplicator_ObjectCarriesNoItemKeyword(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    Odd:
      type: object
      properties: {p: {type: string}}
      items: {type: string}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := typeByName(doc, "Odd").(*ir.Model)
	require.True(t, ok, "it is still a model")
	assert.Contains(t, m.Unmodeled, "openapi:items", "a model has no home for items")
	assert.Len(t, m.Properties, 1, "and the applicators it does have a home for still land")
}

// TestUnhomedApplicator_UnpreservableKeywordIsNotAnnounced pins the pairing
// GitHub #144 exists for, at this site: when the only unhomed keyword fails to
// convert, the position must say nothing about keeping it. A diagnostic naming a
// keyword that was never written is worse than silence — it sends a reader
// looking in Unmodeled for something that is not there.
func TestUnhomedApplicator_UnpreservableKeywordIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    Odd:
      type: object
      properties: {p: {type: string}}
      items: {type: string, x-t: `+unpreservableValue+`}
`))

	m, ok := typeByName(doc, "Odd").(*ir.Model)
	require.True(t, ok)
	assert.NotContains(t, m.Unmodeled, "openapi:items", "the conversion failed, so nothing was kept")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct), "the failure itself is reported")
	assert.Empty(t, preservationClaims(diags),
		"nothing was written under Unmodeled, so nothing may announce that it was")
}

// TestUnhomedApplicator_FormatWithNoTypeIsKept pins the one keyword the home
// question answers from the declaration rather than from the node kind.
//
// `format` is homed by a declared type: scalarTypeID pairs the two to select a
// primitive or hoist a Scalar carrying the encoding. Written with no type beside
// it there is no pairing to make, so nothing reads it — and it is kept verbatim
// rather than dropped, which is the whole of the rule
// docs/compilers/openapi.md#shape-applicator for this keyword.
func TestUnhomedApplicator_FormatWithNoTypeIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec("    Odd: {format: date-time}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	td := typeByName(doc, "Odd")
	require.NotNil(t, td, "the position still lowers to something")
	assert.Contains(t, td.Common().Unmodeled, "openapi:format",
		"a format with no type to pair with reaches no field, so it is kept")
	assert.True(t, openapitest.HasDiag(diags, diag.DegradedConstruct), "and the position says so")
}

// TestUnhomedKeywords_ElectedLoweringKeepsWhatItCannotRead covers the keywords
// the winning lowering never reads (GitHub #268). lower() elects one keyword
// family per position, and what the elected form has no field for must be kept:
// a Model has no type token, a Literal no encoding or Constraints, none of
// which a keyword list shows, only the node that was built, which is what the
// census asks.
//
// The kept set is asserted whole, so a census that keeps too much fails as
// loudly as one that keeps too little; the `type: object` row matters because a
// Model restates it and nothing may be recorded.
func TestUnhomedKeywords_ElectedLoweringKeepsWhatItCannotRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		kind       ir.TypeKind
		kept       []string
	}{
		{"non-object type beside allOf", "{allOf: [{$ref: '#/components/schemas/BaseObj'}], type: string}",
			ir.KindModel, []string{"openapi:type"}},
		{"object type beside allOf", "{allOf: [{$ref: '#/components/schemas/BaseObj'}], type: object}",
			ir.KindModel, nil},
		{"format beside const", "{const: 5, type: integer, format: int32}",
			ir.KindLiteral, []string{"openapi:format", "openapi:type"}},
		{"contradictory type beside const", "{const: 5, type: string}",
			ir.KindLiteral, []string{"openapi:type"}},
		{"value constraint beside const", "{const: ab, type: string, maxLength: 1}",
			ir.KindLiteral, []string{"openapi:maxLength", "openapi:type"}},
		// A collection bound is homed by the Constraints of a List or a Tuple and
		// by nothing else, so an object keeps it here and the tuple, its control,
		// keeps nothing.
		{"collection bound on an object", "{type: object, properties: {f: {type: string}}, minItems: 3}",
			ir.KindModel, []string{"openapi:minItems"}},
		{"collection bound beside prefixItems", "{type: array, prefixItems: [{type: string}], maxItems: 2}",
			ir.KindTuple, nil},
		// A Scalar rather than the shared Primitive the type alone would reach:
		// the entry needs a node this pointer owns, so the census hoists the alias
		// that carries it.
		{"unique items on a string", "{type: string, uniqueItems: true}",
			ir.KindScalar, []string{"openapi:uniqueItems"}},
		// The control: a node that does carry them keeps nothing.
		{"collection bounds on an array", "{type: array, items: {type: string}, minItems: 3, maxItems: 9, uniqueItems: true}",
			ir.KindList, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, keywordCensusSpec("    S: "+tc.body+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			td := typeByName(doc, "S")
			require.NotNil(t, td, "the position still lowers")
			assert.Equal(t, tc.kind, td.Kind(), "and lowers to the form the election picked")
			assert.Equal(t, tc.kept, unmodeledKeys(td.Common().Unmodeled))

			want := 1
			if len(tc.kept) == 0 {
				want = 0
			}
			assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/S"), want,
				"a position keeps nothing quietly and reports everything it keeps: %+v", diags)
		})
	}
}

// TestUnhomedKeywords_BoundsThatLandedAreNotAlsoKept is the other half of the
// value-constraint census: a bound is kept only where the node has no field it
// reached, never where it did. The two rows are the two node kinds that read
// annotation.Constraints, so a census asking the kind rather than the field
// would double-record both.
func TestUnhomedKeywords_BoundsThatLandedAreNotAlsoKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, unhomed string }{
		{"scalar", "{type: string, format: email, minLength: 3, required: [a]}", "openapi:required"},
		{"model", "{type: object, properties: {a: {type: string}}, minProperties: 1, items: {type: string}}", "openapi:items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, keywordCensusSpec("    S: "+tc.body+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			td := typeByName(doc, "S")
			require.NotNil(t, td)
			assert.Equal(t, []string{tc.unhomed}, unmodeledKeys(td.Common().Unmodeled),
				"the bound reached the node's Constraints, so only the homeless keyword is kept")
		})
	}
}

// TestUnhomedKeywords_ArrayBoundsKeepWhatCollectionConstraintsDoesNotRead pins
// the reason the census asks what filled a Constraints field rather than which
// kinds have one. An ir.List has the field, but lowerArray fills it from
// collectionConstraints — collection bounds only — so a string bound written on
// an array reaches nothing however full the field looks.
func TestUnhomedKeywords_ArrayBoundsKeepWhatCollectionConstraintsDoesNotRead(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, keywordCensusSpec(
		"    S: {type: array, items: {type: string}, minItems: 1, minLength: 3}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	l, ok := typeByName(doc, "S").(*ir.List)
	require.True(t, ok)
	require.NotNil(t, l.Constraints)
	assert.Equal(t, int64(1), *l.Constraints.MinItems, "the collection bound lowers as it always did")
	assert.Equal(t, []string{"openapi:minLength"}, unmodeledKeys(l.Unmodeled),
		"and only the bound collectionConstraints does not read is kept")
}

// TestUnplacedDiscriminator_IsKeptAtEveryUnionSite pins that a discriminator
// beside a oneOf/anyOf whose lowering has no model or union to hold it is kept
// verbatim on the node or carrier that owns the position, with an info
// diagnostic, instead of vanishing.
func TestUnplacedDiscriminator_IsKeptAtEveryUnionSite(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, unplacedSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))

	for _, name := range []string{"NullOnly", "NullBoth", "EnumU", "ConstU", "ScalarU", "RefSite"} {
		node := typeByName(doc, name)
		require.NotNil(t, node, name)
		assertKeptDiscriminator(t, node.Common().Unmodeled, name)
		assert.True(t, namesDiscriminatorAt(diags, "/components/schemas/"+name), name)
	}
	for _, pointer := range []string{"/components/schemas/Holder/properties/n", "/components/schemas/Holder/properties/r"} {
		assert.True(t, namesDiscriminatorAt(diags, pointer), pointer)
	}

	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	require.Len(t, holder.Properties, 2)
	// The null-only property hoists a node of its own; the $ref one is on the carrier.
	assertKeptDiscriminator(t, doc.Types["t/anon/components/schemas/Holder/properties/n"].Common().Unmodeled, "n")
	r := holder.Properties[slices.IndexFunc(holder.Properties, func(p ir.Property) bool { return p.Name.Source == "r" })]
	assertKeptDiscriminator(t, r.Unmodeled, "r")
	assert.Equal(t, 8, strings.Count(marshalForTest(t, doc), `"openapi:discriminator"`),
		"one entry per position, none duplicated")
}

// TestHomelessDiscriminator_IsKeptBesideEveryOtherNode pins that a
// discriminator is a census keyword: wherever the position lowered to a node
// that does not route by it, the census keeps it verbatim and says so, as it
// keeps any keyword the node has no field for. Each was dropped in silence. A
// multi-typed schema runs no census yet (GitHub #852).
func TestHomelessDiscriminator_IsKeptBesideEveryOtherNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, homelessSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))

	owners := map[string]ir.Unmodeled{}
	for _, name := range []string{"Str", "En", "Mixed", "Arr", "Bare", "RefD"} {
		owners["/components/schemas/"+name] = typeByName(doc, name).Common().Unmodeled
	}
	for _, at := range []string{"Items/items", "Sub/allOf/0", "Holder/properties/q"} {
		owners["/components/schemas/"+at] = doc.Types[ir.TypeID("t/anon/components/schemas/"+at)].Common().Unmodeled
	}
	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	owners["/components/schemas/Holder/properties/p"] = holder.Properties[0].Unmodeled
	op := doc.Services[0].Groups[0].Operations[0]
	require.Len(t, op.Params, 1)
	owners["/paths/~1p/get/parameters/0/schema"] = op.Params[0].Unmodeled

	for pointer, unmodeled := range owners {
		assertKeptDiscriminator(t, unmodeled, pointer)
		assert.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
			return d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityInfo &&
				string(d.Provenance.Pointer) == pointer && strings.Contains(d.Message, "which has no home for discriminator")
		}), "%s reports what it kept", pointer)
	}
	assert.Equal(t, len(owners), strings.Count(marshalForTest(t, doc), `"openapi:discriminator"`),
		"one entry per position, none duplicated")
}

// TestHeldDiscriminator_IsNotKeptTwice pins the census's control: a union or a
// model that routes by its discriminator holds it, so nothing is kept verbatim
// and nothing is reported. A type array lowers each type as a variant of the
// same schema, so its object variant holds one.
func TestHeldDiscriminator_IsNotKeptTwice(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(cat + dog +
		"    U: {oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}], discriminator: {propertyName: kind}}\n" +
		"    Base: {type: object, properties: {kind: {type: string}}, discriminator: {propertyName: kind}}\n" +
		"    Multi: {type: [object, string], properties: {kind: {type: string}}, discriminator: {propertyName: kind}}\n")
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))

	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.NotNil(t, u.Discriminator)
	base, ok := typeByName(doc, "Base").(*ir.Model)
	require.True(t, ok)
	assert.NotNil(t, base.Discriminator)
	variant, ok := doc.Types["t/anon/components/schemas/Multi/type/0"].(*ir.Model)
	require.True(t, ok)
	assert.NotNil(t, variant.Discriminator)
	assert.NotContains(t, marshalForTest(t, doc), "openapi:discriminator")
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo), "%+v", diags)
}

// unplacedSpec declares a discriminator beside a oneOf/anyOf at each position
// whose lowering has no model to hold it: a null-only union as a component, a
// property and beside a second combinator, a union beside a const, an enum and
// a scalar, and a union beside a $ref as a component and as a property.
const unplacedSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    NullOnly:
      oneOf: [{type: 'null'}]
      discriminator: {propertyName: kind}
    NullBoth:
      oneOf: [{type: 'null'}, {type: 'null'}]
      anyOf: [{type: 'null'}]
      discriminator: {propertyName: kind}
    EnumU:
      enum: [a, b]
      oneOf: [{$ref: '#/components/schemas/Cat'}, {type: string}]
      discriminator: {propertyName: kind}
    ConstU:
      const: a
      oneOf: [{$ref: '#/components/schemas/Cat'}, {type: string}]
      discriminator: {propertyName: kind}
    ScalarU:
      type: string
      oneOf: [{$ref: '#/components/schemas/Cat'}, {type: string}]
      discriminator: {propertyName: kind}
    RefSite:
      $ref: '#/components/schemas/Cat'
      oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]
      discriminator: {propertyName: kind}
    Holder:
      type: object
      properties:
        n:
          anyOf: [{type: 'null'}, {type: 'null'}]
          discriminator: {propertyName: kind}
        r:
          $ref: '#/components/schemas/Cat'
          oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]
          discriminator: {propertyName: kind}
`

// homelessSpec declares a discriminator beside no union at all, at each kind
// of position whose node has no field for it: a scalar, an enum, an array, a
// bare schema, a $ref as a component, a property, a parameter and an allOf
// branch, an array's items, and a mixed-type enum, whose Union has the field
// but routes nothing by it.
const homelessSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    get:
      parameters:
        - name: q
          in: query
          schema: {$ref: '#/components/schemas/Cat', discriminator: {propertyName: kind}}
      responses:
        "204": {description: none}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Str: {type: string, discriminator: {propertyName: kind}}
    En: {enum: [a, b], discriminator: {propertyName: kind}}
    Mixed: {enum: [1, a], discriminator: {propertyName: kind}}
    Arr: {type: array, items: {type: string}, discriminator: {propertyName: kind}}
    Bare: {discriminator: {propertyName: kind}}
    RefD: {$ref: '#/components/schemas/Cat', discriminator: {propertyName: kind}}
    Items: {type: array, items: {type: string, discriminator: {propertyName: kind}}}
    Sub: {allOf: [{$ref: '#/components/schemas/Cat', discriminator: {propertyName: kind}}]}
    Holder:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Cat', discriminator: {propertyName: kind}}
        q: {type: string, discriminator: {propertyName: kind}}
`

// assertKeptDiscriminator requires unmodeled to keep a discriminator verbatim
// as a degraded lowering, whole.
func assertKeptDiscriminator(t *testing.T, unmodeled ir.Unmodeled, where string) {
	t.Helper()
	entry, ok := unmodeled["openapi:discriminator"]
	require.True(t, ok, "%s keeps its discriminator", where)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, where)
	assert.JSONEq(t, `{"propertyName":"kind"}`, string(entry.Value), where)
}

// namesDiscriminatorAt reports whether an info degraded-construct diagnostic at
// pointer mentions a discriminator. A site also reports its union, so the count
// at a pointer is not one.
func namesDiscriminatorAt(diags []ir.Diagnostic, pointer string) bool {
	return slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityInfo &&
			string(d.Provenance.Pointer) == pointer && strings.Contains(d.Message, "discriminator")
	})
}
