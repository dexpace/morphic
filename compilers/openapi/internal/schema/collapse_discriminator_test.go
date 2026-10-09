package schema_test

import (
	"encoding/json/v2"
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
// kept verbatim, whole, on the node that position owns, with an info
// diagnostic, instead of vanishing. The reference itself is unchanged. The
// component, the model property, an array's items, additionalProperties, a
// parameter and a response body each reach lowerOneOfAnyOf by their own path.
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
			"collapses to a nullable reference", pointer)
	}
	assert.Equal(t, 6, strings.Count(marshalForTest(t, doc), `"openapi:discriminator"`),
		"one entry per position, including the parameter and the response, none duplicated")
}

// TestCollapsedUnion_WithoutADiscriminatorHoistsNothing pins that the same
// positions without a discriminator stay as they were: no entry, no
// diagnostic, and no node hoisted for the position that has none today.
func TestCollapsedUnion_WithoutADiscriminatorHoistsNothing(t *testing.T) {
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
// a scalar, and a union beside a $ref as a component and as a property. Each
// reaches its keeper by its own path.
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

// TestUnplacedDiscriminator_IsKeptAtEverySite pins that a discriminator beside
// a oneOf/anyOf whose lowering has no model to hold it is kept verbatim on the
// node or carrier that owns the position, with an info diagnostic, instead of
// vanishing. A model-bodied union still carries it in its own field.
func TestUnplacedDiscriminator_IsKeptAtEverySite(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, unplacedSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))

	for _, name := range []string{"NullOnly", "NullBoth", "EnumU", "ConstU", "ScalarU", "RefSite"} {
		node := typeByName(doc, name)
		require.NotNil(t, node, name)
		entry, ok := node.Common().Unmodeled["openapi:discriminator"]
		require.True(t, ok, "%s keeps its discriminator", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, name)
		assert.Contains(t, string(entry.Value), "propertyName", name)
		assert.True(t, namesDiscriminatorAt(diags, "/components/schemas/"+name), name)
	}
	for _, pointer := range []string{"/components/schemas/Holder/properties/n", "/components/schemas/Holder/properties/r"} {
		assert.True(t, namesDiscriminatorAt(diags, pointer), pointer)
	}

	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	require.Len(t, holder.Properties, 2)
	owned := map[string]ir.Unmodeled{"n": nil, "r": nil}
	for _, prop := range holder.Properties {
		owned[prop.Name.Source] = prop.Unmodeled
	}
	// The null-only property hoists a node of its own; the $ref one is on the carrier.
	assert.Contains(t, doc.Types["t/anon/components/schemas/Holder/properties/n"].Common().Unmodeled, "openapi:discriminator")
	assert.Contains(t, owned["r"], "openapi:discriminator")
	assert.Equal(t, 8, strings.Count(marshalForTest(t, doc), `"openapi:discriminator"`),
		"one entry per position, none duplicated")
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
