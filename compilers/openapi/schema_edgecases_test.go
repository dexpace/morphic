package openapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// componentSpec wraps a components/schemas block in a minimal 3.1 document.
func componentSpec(schemas string) string {
	return componentSpecVer("3.1.0", schemas)
}

// componentSpecVer wraps a components/schemas block in a minimal document of the
// given OpenAPI version.
func componentSpecVer(version, schemas string) string {
	return "openapi: " + version + "\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n  schemas:\n" + schemas
}

func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[ir.TypeID("t/openapi/components/schemas/"+name)]
}

func TestSchemaRef_BooleanAndUntypedShapes(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        anything: true
        nothing: false
        untyped: {}
        withprops: {properties: {x: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := typeByName(doc, "S").(*ir.Model)
	byWire := map[string]ir.Property{}
	for _, p := range m.Properties {
		byWire[p.WireName] = p
	}
	assert.Equal(t, ir.TypeID("t/prim/any"), byWire["anything"].Type.Target)
	// `false` schema lowered to a closed empty model.
	nothing := doc.Types[byWire["nothing"].Type.Target]
	require.NotNil(t, nothing)
	assert.Equal(t, ir.KindModel, nothing.Kind())
	assert.Equal(t, ir.AdditionalClosed, nothing.(*ir.Model).Additional)
	assert.Equal(t, ir.TypeID("t/prim/any"), byWire["untyped"].Type.Target)
	assert.Equal(t, ir.KindModel, doc.Types[byWire["withprops"].Type.Target].Kind())

	var sawFalse bool
	for _, d := range diags {
		if d.Code == codeFalseSchema {
			sawFalse = true
		}
	}
	assert.True(t, sawFalse, "false schema info diagnostic")
}

func TestLower_MultiTypeUnion(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    MT:
      type: [object, array, string]
      properties: {x: {type: string}}
      items: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "MT").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 3)
	assert.True(t, u.Exclusive)
}

func TestScalar_UnknownFormatPerBaseType(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        i: {type: integer, format: weird}
        n: {type: number, format: weird}
        b: {type: boolean, format: weird}
        s: {type: string, format: weird}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := typeByName(doc, "S").(*ir.Model)
	bases := map[string]ir.PrimKind{}
	for _, p := range m.Properties {
		sc, ok := doc.Types[p.Type.Target].(*ir.Scalar)
		require.True(t, ok, "prop %s hoisted as scalar", p.WireName)
		require.NotNil(t, sc.Base)
		bases[p.WireName] = doc.Types[sc.Base.Target].(*ir.Primitive).Prim
		assert.Equal(t, "weird", sc.Encoding.Name)
	}
	assert.Equal(t, ir.PrimInteger, bases["i"])
	assert.Equal(t, ir.PrimNumber, bases["n"])
	assert.Equal(t, ir.PrimBool, bases["b"])
	assert.Equal(t, ir.PrimString, bases["s"])
}

func TestLower_DepthCapExceeded(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("    Deep:\n")
	indent := "      "
	for i := 0; i < maxSchemaDepth+4; i++ {
		b.WriteString(indent + "type: array\n")
		b.WriteString(indent + "items:\n")
		indent += "  "
	}
	b.WriteString(indent + "type: string\n")
	doc, diags := lowerSpec(t, componentSpec(b.String()))
	require.NotNil(t, doc)
	var sawCap bool
	for _, d := range diags {
		if d.Code == codeDegradedConstruct && strings.Contains(d.Message, "nesting exceeds") {
			sawCap = true
		}
	}
	assert.True(t, sawCap, "schema depth-cap diagnostic emitted")
}

func TestLower_TupleWithTrailingItems(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Tup:
      type: array
      prefixItems: [{type: string}, {type: integer}]
      items: {type: boolean}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	tup, ok := typeByName(doc, "Tup").(*ir.Tuple)
	require.True(t, ok)
	require.Len(t, tup.Elems, 2)
	_, hasResidue := tup.Extensions["openapi:items-after-prefix"]
	assert.True(t, hasResidue, "trailing items preserved raw")
}

func TestLower_ListConstraints(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    L:
      type: array
      items: {type: string}
      minItems: 1
      maxItems: 9
      uniqueItems: true
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	l, ok := typeByName(doc, "L").(*ir.List)
	require.True(t, ok)
	require.NotNil(t, l.Constraints)
	assert.True(t, l.Constraints.UniqueItems)
	require.NotNil(t, l.Constraints.MinItems)
	assert.Equal(t, int64(1), *l.Constraints.MinItems)
}

func TestLower_ListWithoutItems(t *testing.T) {
	t.Parallel()
	// No `items` → schemaRef(nil) → element is `any`.
	doc, diags := lowerSpec(t, componentSpec("    L: {type: array}\n"))
	requireNoErrorDiags(t, diags)
	l, ok := typeByName(doc, "L").(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/any"), l.Elem.Target)
}

func TestLower_ValidationOnlyKeywords(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    V:
      type: object
      properties: {a: {type: string}}
      if: {required: [a]}
      then: {required: [b]}
      else: {required: [c]}
      dependentSchemas: {a: {required: [d]}}
      contains: {type: string}
      minContains: 1
      unevaluatedProperties: {type: string}
      unevaluatedItems: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := typeByName(doc, "V").(*ir.Model)
	for _, key := range []string{"openapi:if-then-else", "openapi:dependentSchemas", "openapi:contains", "openapi:unevaluated"} {
		_, ok := m.Extensions[key]
		assert.True(t, ok, "keyword %s preserved", key)
	}
	var count int
	for _, d := range diags {
		if d.Code == codeValidationOnlyKeyword {
			count++
		}
	}
	assert.GreaterOrEqual(t, count, 4)
}

func TestLower_PropertyDetailRichSchema(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    D:
      type: object
      externalDocs: {url: 'https://x', description: more}
      properties:
        withXml:
          type: string
          xml: {name: n, namespace: 'urn:x', prefix: p, wrapped: true, attribute: true}
        withExample: {type: string, example: hi, examples: [a, b]}
        withExt: {type: string, x-foo: bar}
        badDefault: {type: number, default: .inf}
`)
	doc, diags := lowerSpec(t, spec)
	m := typeByName(doc, "D").(*ir.Model)
	assert.NotEmpty(t, m.Docs.ExternalDocs)
	byWire := map[string]ir.Property{}
	for _, p := range m.Properties {
		byWire[p.WireName] = p
	}
	require.NotNil(t, byWire["withXml"].XML)
	assert.Equal(t, "attribute", byWire["withXml"].XML.NodeType)
	assert.Equal(t, "urn:x", byWire["withXml"].XML.Namespace)
	assert.True(t, byWire["withXml"].XML.Wrapped)
	assert.Len(t, byWire["withExample"].Examples, 3)
	assert.NotEmpty(t, byWire["withExt"].Extensions)
	var sawDefaultWarn bool
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning && strings.Contains(d.Message, "default:") {
			sawDefaultWarn = true
		}
	}
	assert.True(t, sawDefaultWarn, "malformed default warns")
}

func TestLower_RefTargetDescriptionFallback(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Owner:
      type: object
      properties:
        ref: {$ref: '#/components/schemas/Target'}
    Target:
      type: string
      description: target-desc
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.Equal(t, "target-desc", m.Properties[0].Docs.Description)
}

func TestLower_UnresolvedRefDiagnostics(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Owner:
      type: object
      properties:
        ghost: {$ref: '#/components/schemas/Ghost'}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	var sawUnresolved bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawUnresolved = true
		}
	}
	assert.True(t, sawUnresolved, "unresolved ref diagnostic emitted")
}

func TestLower_UnionWithStructuralSiblingVariants(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A:
      type: object
      properties: {x: {type: string}}
      required: [x]
      additionalProperties: {type: integer}
      oneOf: [{type: string}, {type: integer}]
    B:
      patternProperties: {'^x-': {type: string}}
      anyOf: [{type: string}, {type: integer}]
    C:
      const: fixed
      oneOf: [{type: string}, {type: integer}]
    D:
      type: string
      oneOf: [{$ref: '#/components/schemas/A'}, {type: integer}]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	for _, name := range []string{"A", "B", "C", "D"} {
		td := typeByName(doc, name)
		require.NotNil(t, td, "type %s present", name)
		c := td.Common()
		_, hasOneOf := c.Extensions["openapi:oneOf"]
		_, hasAnyOf := c.Extensions["openapi:anyOf"]
		assert.True(t, hasOneOf || hasAnyOf, "union preserved for %s", name)
	}
}
