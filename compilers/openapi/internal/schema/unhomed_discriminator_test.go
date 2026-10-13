package schema_test

import (
	"encoding/json/v2"
	"maps"
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

const nullableCat = "oneOf: [{$ref: '#/components/schemas/Cat'}, {type: 'null'}]"

// collapseSpec declares a nullable Cat at every position a schema can sit at
// with a discriminator beside it, plus a parameter and a response body.
const collapseSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /p:
    get:
      parameters:
        - name: q
          in: query
          schema:
            ` + nullableCat + `
            discriminator: {propertyName: kind}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                ` + nullableCat + `
                discriminator: {propertyName: kind}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    W:
      ` + nullableCat + `
      discriminator: {propertyName: kind, defaultMapping: '#/components/schemas/Cat', mapping: {cat: '#/components/schemas/Cat'}}
    Holder:
      type: object
      properties:
        w:
          anyOf: [{$ref: '#/components/schemas/Cat'}, {type: 'null'}]
          discriminator: {propertyName: kind}
    L:
      type: array
      items:
        ` + nullableCat + `
        discriminator: {propertyName: kind}
    M:
      type: object
      additionalProperties:
        ` + nullableCat + `
        discriminator: {propertyName: kind}
`

// TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition pins that a
// discriminator beside a oneOf/anyOf that collapses to a nullable reference is
// kept verbatim, whole, by whatever owns the position, with an info diagnostic,
// instead of vanishing (GitHub #595). The reference itself is unchanged. The
// component, the property, an array's items, additionalProperties, a parameter
// and a response body each reach the collapse by their own path.
func TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, collapseSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))
	cat := componentID("Cat")

	w, ok := typeByName(doc, "W").(*ir.Scalar)
	require.True(t, ok, "the component is an alias over the nullable reference")
	assert.Equal(t, ir.TypeRef{Target: cat, Nullable: true}, *w.Base, "the reference is unchanged")
	entry, ok := w.Unmodeled["openapi:discriminator"]
	require.True(t, ok, "the component keeps its discriminator")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.Contains(t, string(entry.Value), "defaultMapping", "the whole object is kept, not just the property name")
	assert.Contains(t, string(entry.Value), `"mapping"`)

	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	require.Len(t, holder.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: cat, Nullable: true}, holder.Properties[0].Type, "the property's reference is unchanged")
	assert.Contains(t, holder.Properties[0].Unmodeled, "openapi:discriminator", "the property carrier keeps it")

	for _, id := range []ir.TypeID{"t/anon/components/schemas/L/items", "t/anon/components/schemas/M/additionalProperties"} {
		node, ok := doc.Types[id]
		require.True(t, ok, id)
		assert.Contains(t, node.Common().Unmodeled, "openapi:discriminator", "%s keeps it on the node it hoists", id)
	}

	for _, pointer := range []string{
		"/components/schemas/W", "/components/schemas/Holder/properties/w",
		"/components/schemas/L/items", "/components/schemas/M/additionalProperties",
		"/paths/~1p/get/parameters/0/schema", "/paths/~1p/get/responses/200/content/application~1json/schema",
	} {
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, pointer),
			"lowered to a nullable reference to its one non-null branch, which has no home for discriminator", pointer)
	}
	assert.Equal(t, 6, strings.Count(marshalForTest(t, doc), `"openapi:discriminator"`),
		"one entry per position, including the parameter and the response, none duplicated")
}

// TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold pins that the collapse
// takes the whole census a $ref site takes, not a discriminator rule: a format
// or a collection bound is no more at home on a nullable reference. A null
// type, which the reference's Nullable bit carries, is, so the component's
// alias keeps nothing for it.
func TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat: {type: object, properties: {kind: {type: string}}}
    Bounded: {` + nullableCat + `, format: uuid, maxItems: 2}
    NullTyped: {` + nullableCat + `, type: 'null'}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	bounded, ok := typeByName(doc, "Bounded").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, []string{"openapi:format", "openapi:maxItems"}, slices.Sorted(maps.Keys(bounded.Unmodeled)))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/components/schemas/Bounded"), "no home for format, maxItems")

	assert.Empty(t, typeByName(doc, "NullTyped").Common().Unmodeled)
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/NullTyped"))
}

// TestCollapsedUnion_WithoutACensusKeywordHoistsNothing pins that the same
// positions without one stay as they were: no entry, no diagnostic, and no
// node hoisted for a position that has none.
func TestCollapsedUnion_WithoutACensusKeywordHoistsNothing(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat: {type: object, properties: {kind: {type: string}}}
    L: {type: array, items: {` + nullableCat + `}}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo), "%+v", diags)
	assert.NotContains(t, marshalForTest(t, doc), "openapi:discriminator")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/L/items"), "a bare nullable reference needs no node")
}

// marshalForTest returns doc's canonical JSON, to count entries wherever they
// sit in it.
func marshalForTest(t *testing.T, doc *ir.Document) string {
	t.Helper()
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(out)
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

// TestNullOnlyUnion_ReportsItsCensusAsABareNullSchemaDoes pins that a null-only
// branch set, which lowers as a bare `{type: null}` schema does, names what a
// keyword it cannot hold sits beside in the same words: the node the position
// lowered to, not the alias holding the keyword.
func TestNullOnlyUnion_ReportsItsCensusAsABareNullSchemaDoes(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpec(
		"    NullOnly: {oneOf: [{type: 'null'}], format: uuid}\n    NullTyped: {type: 'null', format: uuid}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	census := func(name string) string {
		i := slices.IndexFunc(diags, func(d ir.Diagnostic) bool {
			return string(d.Provenance.Pointer) == "/components/schemas/"+name && strings.Contains(d.Message, "which has no home for")
		})
		require.GreaterOrEqual(t, i, 0, "%s reports what it kept", name)
		return diags[i].Message
	}
	assert.Equal(t, census("NullTyped"), census("NullOnly"))
}

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

// TestHomelessDiscriminator_IsKeptBesideEveryOtherNode pins that a
// discriminator is a census keyword: wherever the position lowered to a node
// that does not route by it, the census keeps it verbatim and says so, as it
// keeps any keyword the node has no field for. Each was dropped in silence.
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
