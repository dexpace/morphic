package openapi

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

func TestSchemaRef_NullableNormalization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, version, schema string // YAML fragment under components/schemas/S/properties/p
		wantNullable          bool
		wantTarget            ir.TypeID
	}{
		{"3.0 nullable", "3.0.3", "{type: string, nullable: true}", true, "t/prim/string"},
		{"3.1 type array", "3.1.0", `{type: [string, "null"]}`, true, "t/prim/string"},
		{"plain", "3.1.0", "{type: string}", false, "t/prim/string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: " + tc.version + "\n" +
				"info: {title: T, version: \"1\"}\n" +
				"paths: {}\n" +
				"components:\n" +
				"  schemas:\n" +
				"    S:\n" +
				"      type: object\n" +
				"      properties:\n" +
				"        p: " + tc.schema + "\n"
			doc, diags := lowerSpec(t, spec)
			requireNoErrorDiags(t, diags)
			model, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok)
			require.Len(t, model.Properties, 1)
			assert.Equal(t, tc.wantTarget, model.Properties[0].Type.Target)
			assert.Equal(t, tc.wantNullable, model.Properties[0].Type.Nullable)
		})
	}
}

func TestLower_NamedScalarComponentResolves(t *testing.T) {
	t.Parallel()
	// A named component whose body is a plain scalar must register a resolvable
	// node at its own component pointer, so a $ref to it never dangles.
	spec := componentSpec(`    MyId: {type: string, format: uuid}
    Holder:
      type: object
      properties:
        id: {$ref: "#/components/schemas/MyId"}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

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

func TestLower_OneOfWithStructuralSiblingsPreserved(t *testing.T) {
	t.Parallel()
	// The "exactly one of" idiom co-declares object structure with oneOf. The
	// structural body must survive AND the union must be preserved verbatim.
	spec := componentSpec(`    Thing:
      type: object
      additionalProperties: false
      required: [common]
      properties:
        common: {type: string}
      oneOf:
        - {required: [a]}
        - {required: [b]}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "structural body lowers to a Model, not a bare Union")
	require.Len(t, m.Properties, 1)
	assert.Equal(t, "common", m.Properties[0].Name.Source)
	assert.True(t, m.Properties[0].Required)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
	raw, ok := m.Extensions["openapi:oneOf"]
	require.True(t, ok, "the dropped union is preserved verbatim under extensions")
	assert.Contains(t, string(raw), "required")

	found := false
	for _, d := range diags {
		if d.Severity == ir.SeverityInfo && strings.Contains(d.Message, "oneOf/anyOf co-declared") {
			found = true
		}
	}
	assert.True(t, found, "coexistence emits one info diagnostic")
}

func TestLower_AllOfWithOneOfKeepsBoth(t *testing.T) {
	t.Parallel()
	// allOf co-declared with oneOf must not drop the allOf composition.
	spec := componentSpec(`    Base:
      type: object
      properties:
        id: {type: string}
    Combo:
      allOf:
        - {$ref: "#/components/schemas/Base"}
      oneOf:
        - {type: string}
        - {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Combo")].(*ir.Model)
	require.True(t, ok, "allOf composition survives (Model), oneOf preserved raw")
	require.NotNil(t, m.Base, "the allOf $ref becomes Base")
	assert.Equal(t, componentID("Base"), m.Base.Target)
	_, ok = m.Extensions["openapi:oneOf"]
	assert.True(t, ok, "the oneOf is preserved verbatim under extensions")
}

func TestLower_RecursiveSchemaTerminates(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Node:
      type: object
      properties:
        next: {$ref: "#/components/schemas/Node"}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	node, ok := doc.Types[componentID("Node")].(*ir.Model)
	require.True(t, ok)
	require.Equal(t, ir.TypeRef{Target: "t/openapi/components/schemas/Node"}, node.Properties[0].Type)
}

func TestLower_InlineSchemaHoistedOnce(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        tags:
          type: array
          items:
            type: object
            properties:
              name: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	itemsID := ir.TypeID("t/anon/components/schemas/S/properties/tags/items")
	item, ok := doc.Types[itemsID].(*ir.Model)
	require.True(t, ok, "items object should be hoisted as a model")
	assert.True(t, item.Anonymous)
	assert.Equal(t, "tags_item", item.Name.Hint)
	assert.Empty(t, item.Name.Source, "hoisted inline types carry a hint, not a source name")
}

func TestCanonicalWords(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"userID": "user_id", "HTTPServer": "http_server", "list-users": "list_users",
		"User": "user", "APIKey2": "api_key_2",
	} {
		assert.Equal(t, want, canonicalWords(in), "input %q", in)
	}
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
	byWire := propsByWire(m.Properties)
	assert.Equal(t, ir.TypeID("t/prim/any"), byWire["anything"].Type.Target)
	// `false` schema lowered to a closed empty model.
	nothing := doc.Types[byWire["nothing"].Type.Target]
	require.NotNil(t, nothing)
	assert.Equal(t, ir.KindModel, nothing.Kind())
	assert.Equal(t, ir.AdditionalClosed, nothing.(*ir.Model).Additional)
	assert.Equal(t, ir.TypeID("t/prim/any"), byWire["untyped"].Type.Target)
	assert.Equal(t, ir.KindModel, doc.Types[byWire["withprops"].Type.Target].Kind())

	assert.True(t, hasDiagAt(diags, codeFalseSchema, ir.SeverityInfo), "false schema info diagnostic")
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
	for range maxSchemaDepth + 4 {
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
	assert.GreaterOrEqual(t, countDiagsAt(diags, codeValidationOnlyKeyword, ir.SeverityInfo), 4)
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
	byWire := propsByWire(m.Properties)
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
	assert.True(t, hasDiag(diags, codeUnresolvedRef), "unresolved ref diagnostic emitted")
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

func TestModel_FourOptionalityStates(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      required: [reqPlain, reqNull]
      properties:
        reqPlain: {type: string}
        reqNull: {type: [string, "null"]}
        optPlain: {type: string}
        optNull: {type: [string, "null"]}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 4)
	byName := propsByWire(m.Properties)
	assert.True(t, byName["reqPlain"].Required)
	assert.False(t, byName["reqPlain"].Type.Nullable)
	assert.True(t, byName["reqNull"].Required)
	assert.True(t, byName["reqNull"].Type.Nullable)
	assert.False(t, byName["optPlain"].Required)
	assert.False(t, byName["optPlain"].Type.Nullable)
	assert.False(t, byName["optNull"].Required)
	assert.True(t, byName["optNull"].Type.Nullable)
}

func TestModel_ValidationOnlyKeywordPreserved(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      not: {required: [b]}
`)
	doc, diags := lowerSpec(t, spec)
	m := doc.Types[componentID("S")].(*ir.Model)
	raw, ok := m.Extensions["openapi:not"]
	require.True(t, ok, "not-keyword must be preserved verbatim")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw))
	found := false
	for _, d := range diags {
		if d.Code == codeValidationOnlyKeyword {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}

func TestModel_DefaultBigLiteral(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        n: {type: integer, default: 9007199254740993}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.Properties[0].Default)
	assert.Equal(t, ir.ValueNumber, m.Properties[0].Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), m.Properties[0].Default.Num)
}

func TestFillPropertyDetail_UnconvertibleExampleDiagnosed(t *testing.T) {
	t.Parallel()
	// A custom tag is structurally unconvertible; the example must be skipped
	// (an example is an annotation, not a structural hole) but never silently —
	// the conversion error was previously discarded on the floor.
	spec := componentSpec("    S:\n      type: object\n      properties:\n        n:\n          type: string\n          example: !foo bar\n")
	doc, diags := lowerSpec(t, spec)
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, m.Properties[0].Examples, "the unconvertible example is skipped, not appended")
	require.Equal(t, 1, countDiagsAt(diags, codeDegradedConstruct, ir.SeverityWarning))
	d, ok := firstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/components/schemas/S/properties/n/example", d.Provenance.Pointer)
	assert.Contains(t, d.Message, "example:")
}

func TestModel_ReadOnlyWriteOnlyVisibility(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        r: {type: string, readOnly: true}
        w: {type: string, writeOnly: true}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	byName := propsByWire(m.Properties)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, byName["r"].Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, byName["w"].Visibility)
}

func TestModel_PasswordFormatSecret(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        pw: {type: string, format: password}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.True(t, m.Properties[0].Secret)
}

func TestModel_AdditionalPropertiesFalseClosed(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      additionalProperties: false
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
}

func TestModel_AdditionalPropertiesSchema(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      additionalProperties: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.AdditionalProps)
	assert.Equal(t, ir.TypeID("t/prim/integer"), m.AdditionalProps.Value.Target)
}

func TestModel_PatternPropertiesOrder(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      patternProperties:
        "^x-": {type: string}
        "^y-": {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.AdditionalProps)
	require.Len(t, m.AdditionalProps.Patterns, 2)
	assert.Equal(t, "^x-", m.AdditionalProps.Patterns[0].Pattern)
	assert.Equal(t, "^y-", m.AdditionalProps.Patterns[1].Pattern)
}

func TestModel_UnevaluatedPropertiesClosedAfterComposition(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      unevaluatedProperties: false
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosedAfterComposition, m.Additional)
}

func TestModel_SchemaExtensionPreserved(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      x-rate-limit: 100
      properties: {a: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	raw, ok := m.Extensions["openapi:x-rate-limit"]
	require.True(t, ok)
	assert.JSONEq(t, "100", string(raw))
}

func TestModel_TitleDescriptionDocs(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      title: "My Title"
      description: "My Desc"
      properties: {a: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, "My Title", m.Docs.Summary)
	assert.Equal(t, "My Desc", m.Docs.Description)
}

func TestModel_PropertyDeprecation(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        old: {type: string, deprecated: true}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.NotNil(t, m.Properties[0].Deprecation)
}

func TestModel_PropertyXML(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        p: {type: string, xml: {name: n, attribute: true}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.Properties[0].XML)
	assert.Equal(t, "n", m.Properties[0].XML.Name)
	assert.Equal(t, "attribute", m.Properties[0].XML.NodeType)
}

func TestModel_RefSiblingDescriptionWins(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Target: {type: string, description: "target desc"}
    S:
      type: object
      properties:
        p:
          $ref: '#/components/schemas/Target'
          description: "sibling desc"
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, "sibling desc", m.Properties[0].Docs.Description)
}

func TestSchemaRef_EmptyEitherIsAny(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	ref := l.schemaRef(emptyEitherSchema(), "/p", "h")
	assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
}

func TestIsNullSchema_EmptyEitherFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, isNullSchema(emptyEitherSchema()), "empty either is not a null schema")
}

func TestPreserveUnionSiblings_MissingNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// No node registered under the id → the guard returns without panicking.
	l.preserveUnionSiblings("t/anon/missing", &oas3.Schema{}, "/p")
	assert.Empty(t, l.diags)
}

// The redeclaration-conflict helpers carry defensive guards for states a
// well-formed lowered document never reaches: a reference into no interned type,
// a base-less opaque scalar, a cyclic base chain, and an unparseable numeric
// literal. These exercise those guards directly.

func TestResolvePrimKind_DanglingTargetIsNotResolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	_, ok := l.resolvePrimKind(ir.TypeRef{Target: "t/missing"})
	assert.False(t, ok, "a target absent from the registry resolves to no primitive")
}

func TestResolvePrimKind_BaselessScalarIsNotResolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.out.Types["t/opaque"] = &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}}
	_, ok := l.resolvePrimKind(ir.TypeRef{Target: "t/opaque"})
	assert.False(t, ok, "a base-less opaque scalar has no underlying primitive")
}

func TestResolvePrimKind_CyclicBaseChainTerminates(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// A scalar whose Base points back at itself: the bounded walk must terminate
	// and report no primitive rather than spin.
	self := ir.TypeRef{Target: "t/cycle"}
	l.out.Types["t/cycle"] = &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/cycle"}, Base: &self}
	_, ok := l.resolvePrimKind(self)
	assert.False(t, ok, "a cyclic base chain terminates without resolving")
}

func TestIsAnyType_DanglingTargetIsNotAny(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.False(t, l.isAnyType(ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not classified as the top type")
}

func TestDifferentTypeKind_UnresolvableTargetIsNotAConflict(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.out.Types["t/model"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}}
	assert.False(t,
		l.differentTypeKind(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not treated as a differing kind")
}

func TestBigValEqual_UnparseableFallsBackToStringEquality(t *testing.T) {
	t.Parallel()
	assert.True(t, bigValEqual(ir.BigVal("not-a-number"), ir.BigVal("not-a-number")),
		"unparseable operands compare by exact string")
	assert.False(t, bigValEqual(ir.BigVal("not-a-number"), ir.BigVal("other")))
}

func TestIsStructuralType_DistinguishesCompositeFromOpaque(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.out.Types["t/model"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}}
	l.out.Types["t/list"] = &ir.List{TypeCommon: ir.TypeCommon{ID: "t/list"}}
	l.out.Types["t/opaque"] = &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}}
	l.out.Types["t/string"] = &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/string"}, Prim: ir.PrimString}

	assert.True(t, l.isStructuralType(ir.TypeRef{Target: "t/model"}),
		"model is a structural type")
	assert.True(t, l.isStructuralType(ir.TypeRef{Target: "t/list"}),
		"list is a structural type")
	assert.False(t, l.isStructuralType(ir.TypeRef{Target: "t/opaque"}),
		"base-less opaque scalar is not structural")
	assert.False(t, l.isStructuralType(ir.TypeRef{Target: "t/string"}),
		"primitive is not structural")
	assert.False(t, l.isStructuralType(ir.TypeRef{Target: "t/missing"}),
		"unresolvable target is not structural")
}

func TestTypesConflict_OpaqueScalarVsPrimitiveIsNotConflict(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.out.Types["t/opaque"] = &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}}
	l.out.Types["t/string"] = &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/string"}, Prim: ir.PrimString}

	assert.False(t, l.typesConflict(ir.TypeRef{Target: "t/opaque"}, ir.TypeRef{Target: "t/string"}),
		"opaque scalar vs primitive is not flagged (never guess)")
	assert.False(t, l.typesConflict(ir.TypeRef{Target: "t/string"}, ir.TypeRef{Target: "t/opaque"}),
		"primitive vs opaque scalar is not flagged (never guess)")
}

func TestTypesConflict_StructuralVsPrimitiveIsConflict(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.out.Types["t/model"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}}
	l.out.Types["t/string"] = &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/string"}, Prim: ir.PrimString}

	assert.True(t, l.typesConflict(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/string"}),
		"model vs primitive is a provable conflict")
	assert.True(t, l.typesConflict(ir.TypeRef{Target: "t/string"}, ir.TypeRef{Target: "t/model"}),
		"primitive vs model is a provable conflict")
}

func TestSchema_Ref30NullableSiblings(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.0.3", `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', nullable: true}
    Target: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	_ = diags
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.True(t, m.Properties[0].Type.Nullable, "3.0 nullable at a $ref site lifts to the ref")
}

// TestSchema_RefNullableAcrossSpellings pins that every spelling of "admits
// null" — 3.0 nullable: true, a 3.1 type array, and a oneOf/anyOf null branch —
// normalizes to the same Nullable bit at a $ref site, across direct refs,
// chained refs, sub-schema refs, array/scalar/union targets, and ref-site
// siblings, with negative controls for each shape (issue #28).
func TestSchema_RefNullableAcrossSpellings(t *testing.T) {
	t.Parallel()
	targetID := componentID("Target")
	midID := componentID("Mid")
	// A hoisted sub-schema lands in the anonymous namespace, not the component
	// one — the ref must resolve to that node, never to a synthesized name.
	subSchemaID := ir.TypeID("t/anon/components/schemas/Holder/properties/inner")
	cases := []struct {
		name         string
		version      string
		schemas      string
		wantNullable bool
		wantTarget   ir.TypeID
		msg          string
	}{
		{
			name:    "3.0 direct ref to nullable object component",
			version: "3.0.3",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: object, nullable: true}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.0 nullable on the ref target lifts to the ref",
		},
		{
			name:    "3.1 direct ref to nullable object component (type array)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: [object, "null"]}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.1 type-array null on the ref target lifts to the ref",
		},
		{
			name:    "3.0 direct ref to plain component (negative control)",
			version: "3.0.3",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: object}
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a non-nullable 3.0 ref target stays non-nullable at the ref",
		},
		{
			name:    "3.1 direct ref to plain component (negative control)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: object}
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a non-nullable 3.1 ref target stays non-nullable at the ref",
		},
		{
			name:    "3.0 chained ref lifts nullability from the base",
			version: "3.0.3",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Mid'}
    Mid: {$ref: '#/components/schemas/Base'}
    Base: {type: object, nullable: true}
`,
			wantNullable: true,
			wantTarget:   midID,
			msg:          "3.0 nullable on a chain's base lifts through Mid to the ref",
		},
		{
			name:    "3.1 chained ref lifts nullability from the base (type array)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Mid'}
    Mid: {$ref: '#/components/schemas/Base'}
    Base: {type: [object, "null"]}
`,
			wantNullable: true,
			wantTarget:   midID,
			msg:          "3.1 type-array null on a chain's base lifts through Mid to the ref",
		},
		{
			name:    "3.0 ref to a nullable non-component sub-schema",
			version: "3.0.3",
			schemas: `    Holder:
      type: object
      properties:
        inner: {type: object, nullable: true}
    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Holder/properties/inner'}
`,
			wantNullable: true,
			wantTarget:   subSchemaID,
			msg:          "3.0 nullable on a non-component sub-schema lifts to a ref at its pointer",
		},
		{
			name:    "3.1 ref to a nullable non-component sub-schema (type array)",
			version: "3.1.0",
			schemas: `    Holder:
      type: object
      properties:
        inner: {type: [object, "null"]}
    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Holder/properties/inner'}
`,
			wantNullable: true,
			wantTarget:   subSchemaID,
			msg:          "3.1 type-array null on a non-component sub-schema lifts to a ref at its pointer",
		},
		{
			name:    "3.1 nullable array component via ref",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: [array, "null"], items: {type: string}}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.1 type-array null on a nullable array component lifts to the ref",
		},
		{
			name:    "3.1 nullable scalar component via ref",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: [string, "null"]}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.1 type-array null on a nullable scalar component lifts to the ref",
		},
		{
			name:    "3.1 ref-site sibling type array lifts nullability",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', type: ["null"]}
    Target: {type: string}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.1 type-array null as a $ref-site sibling lifts to the ref",
		},
		{
			name:    "3.1 non-null ref-site sibling type array (negative control)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', type: [string]}
    Target: {type: string}
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a $ref-site type array without null leaves the ref non-nullable",
		},
		{
			name:    "3.1 ref to a collapsed oneOf null component",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: "null"}]
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "a oneOf null branch that collapses to nullable X lifts to the ref",
		},
		{
			name:    "3.1 ref to a multi-branch union with a null branch",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: integer}, {type: "null"}]
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "a null branch stripped from a Union lifts to the ref, not into the variants",
		},
		{
			name:    "3.1 ref to an anyOf null component",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      anyOf: [{type: object}, {type: "null"}]
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "an anyOf null branch lifts to the ref just as a oneOf one does",
		},
		{
			name:    "3.1 ref to a union without a null branch (negative control)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: integer}]
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a union with no null branch stays non-nullable at the ref",
		},
		{
			name:    "3.1 ref to a null branch intersected by structural siblings",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      type: object
      properties: {a: {type: string}}
      oneOf: [{type: string}, {type: "null"}]
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a union co-declared with a structural body intersects with it, so its null branch admits nothing",
		},
		{
			name:    "3.1 ref to a closed enum with a null branch sibling",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      enum: ["open", "closed"]
      oneOf: [{type: string}, {type: "null"}]
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "an enum with no null member must not read as nullable through a $ref",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := componentSpecVer(tc.version, tc.schemas)
			doc, diags := lowerSpec(t, spec)
			requireNoErrorDiags(t, diags)
			m := typeByName(doc, "Owner").(*ir.Model)
			require.Len(t, m.Properties, 1)
			assert.Equal(t, tc.wantNullable, m.Properties[0].Type.Nullable, tc.msg)
			assert.Equal(t, tc.wantTarget, m.Properties[0].Type.Target,
				"ref resolves to the expected target")
		})
	}
}

// TestSchema_RefNullableMatchesInlineForUnionSiblings pins that one schema body
// lowers to the same Nullable bit whether it is written inline or reached
// through a $ref. The $ref site recomputes nullability, so it is the one place
// that can drift from the inline rule for a union carrying structural siblings.
func TestSchema_RefNullableMatchesInlineForUnionSiblings(t *testing.T) {
	t.Parallel()
	// Both positions are built from one body string, so "the same schema" is
	// structural rather than two hand-copied blocks that could drift apart.
	const body = `type: object
properties: {a: {type: string}}
oneOf: [{type: string}, {type: "null"}]`
	indent := func(n int) string {
		pad := strings.Repeat(" ", n)
		return pad + strings.ReplaceAll(body, "\n", "\n"+pad) + "\n"
	}
	spec := componentSpec("    Target:\n" + indent(6) +
		`    Owner:
      type: object
      properties:
        viaRef: {$ref: '#/components/schemas/Target'}
        inline:
` + indent(10))
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := typeByName(doc, "Owner").(*ir.Model)
	props := propsByWire(m.Properties)
	require.Len(t, props, 2)

	assert.Equal(t, props["inline"].Type.Nullable, props["viaRef"].Type.Nullable,
		"the same body must not change nullability by being reached through a $ref")
	assert.False(t, props["viaRef"].Type.Nullable,
		"an intersected union's null branch admits nothing, so neither spelling is nullable")
}

// TestSchema_RefNullableAtNonPropertyPosition pins that the lifted bit reaches a
// $ref used as a list element, not just a model property.
func TestSchema_RefNullableAtNonPropertyPosition(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Owner:
      type: object
      properties:
        p:
          type: array
          items: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: integer}, {type: "null"}]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	list, ok := doc.Types["t/anon/components/schemas/Owner/properties/p"].(*ir.List)
	require.True(t, ok)
	assert.True(t, list.Elem.Nullable,
		"a nullable union target lifts to the ref wherever it is used, including a list element")
	assert.Equal(t, componentID("Target"), list.Elem.Target)
}

func TestSchema_UnionSiblingsAdditionalAndRequired(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A:
      additionalProperties: {type: string}
      oneOf: [{type: string}, {type: integer}]
    B:
      required: [x]
      oneOf: [{type: string}, {type: integer}]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	for _, name := range []string{"A", "B"} {
		_, ok := typeByName(doc, name).Common().Extensions["openapi:oneOf"]
		assert.True(t, ok, "%s preserves its union", name)
	}
}

func TestSchema_RefTargetReadOnlyVisibility(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/RO'}
    RO: {type: string, readOnly: true}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.NotEmpty(t, m.Properties[0].Visibility.Only, "readOnly from the ref target applies")
}

func TestSchema_UnserializableExtension(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      x-bad: {1: intkey}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, hasDiagAt(diags, codeDegradedConstruct, ir.SeverityWarning), "unserializable extension warns")
	m := typeByName(doc, "S").(*ir.Model)
	_, hasBad := m.Extensions["openapi:x-bad"]
	assert.False(t, hasBad, "unserializable extension is dropped, not stored")
}

func TestSchema_EmptyFragmentRef(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: '#'}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, hasDiag(diags, codeUnresolvedRef), "the '#' ref form is unresolved")
}

func TestSchema_EmptyStringRefMirrorBranches(t *testing.T) {
	t.Parallel()
	// An empty-string $ref has IsReference()==false (the ref value is "") yet a
	// non-nil Ref pointer, exercising the schema.Ref mirror path in schemaRef and
	// the variantHint fallback.
	spec := componentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: ''}
    U:
      oneOf:
        - {$ref: ''}
        - {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.GreaterOrEqual(t, countDiagsAt(diags, codeUnresolvedRef, ir.SeverityError), 2, "both empty refs are unresolved")
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.Contains(t, []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}, "variant_0")
}

func TestAllOf_UntypedRedeclarationDoesNotConflict(t *testing.T) {
	t.Parallel()
	// One branch leaves the field schemaless (the top type), the other types it.
	// `any` intersects with everything under allOf, so this is a narrowing, not a
	// contradiction — it must not be reported.
	spec := componentSpec(`    Anyish:
      allOf:
        - type: object
          properties:
            id: {description: the identifier}
        - type: object
          properties:
            id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "Anyish").(*ir.Model)
	require.True(t, ok, "Anyish should be a model")
	require.Len(t, m.Properties, 1, "id reconciles to one property")
	assert.Empty(t, conflictDiags(diags),
		"the schemaless top type never conflicts with a sibling redeclaration")
}

func TestAllOf_EquivalentNumericBoundsDoNotConflict(t *testing.T) {
	t.Parallel()
	// The same bound spelled two ways (10 and 10.0) denotes one value, so it must
	// compare equal by magnitude and stay silent.
	spec := componentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: number, minimum: 10}
        - type: object
          properties:
            n: {type: number, minimum: 10.0}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"10 and 10.0 are the same numeric bound, not a conflict")
}

func TestAllOf_DifferingNumericBoundsConflict(t *testing.T) {
	t.Parallel()
	// Two branches pin the same lower bound to different magnitudes; the kept
	// winner is arbitrary source order, so the dropped bound is surfaced.
	spec := componentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: integer, minimum: 5}
        - type: object
          properties:
            n: {type: integer, minimum: 10}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "differing numeric bounds are diagnosed once")
	assert.Contains(t, conflicts[0].Message, `"n"`)
}

func TestAllOf_ScalarVersusObjectRedeclarationConflicts(t *testing.T) {
	t.Parallel()
	// A scalar in one branch and a structural type in the other cannot both hold.
	spec := componentSpec(`    Mixed:
      allOf:
        - type: object
          properties:
            f: {type: string}
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "a scalar against an object is diagnosed once")
	assert.Contains(t, conflicts[0].Message, `"f"`)
}

func TestAllOf_DistinctInlineObjectsDoNotConflict(t *testing.T) {
	t.Parallel()
	// Two branches redeclare the field as distinct inline objects. Each hoists its
	// own model at its own pointer, so the targets differ — but two objects of the
	// same kind are not provably contradictory, and conflict detection never
	// guesses.
	spec := componentSpec(`    Objish:
      allOf:
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"two distinct inline objects for one field are not a provable conflict")
}

// allOfConflictSpec builds a component whose two inline allOf branches each
// declare field v with the given flow-style schemas, for exercising per-keyword
// redeclaration conflict detection.
func allOfConflictSpec(schemaA, schemaB string) string {
	return componentSpec(
		"    T:\n" +
			"      allOf:\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaA + "}\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaB + "}\n")
}

func TestAllOf_ConstraintAndFormatConflictsDiagnosed(t *testing.T) {
	t.Parallel()
	// Each keyword class that can contradict across branches: an inclusive vs
	// exclusive bound of equal magnitude, a differing pattern, a differing
	// multipleOf, and two format-derived primitives (string vs uuid). Each keeps
	// the first declaration but surfaces exactly one conflict naming the field,
	// both branch sites, and — for the constraint cases — the offending keyword
	// with both of its conflicting values, so the author never has to diff both
	// branches by hand.
	cases := []struct {
		name, a, b, wantDetail string
	}{
		{
			name:       "exclusive sense",
			a:          "{type: number, minimum: 10}",
			b:          "{type: number, exclusiveMinimum: 10}",
			wantDetail: "conflicting minimum (10 and exclusive 10)",
		},
		{
			name:       "pattern",
			a:          "{type: string, pattern: '^a$'}",
			b:          "{type: string, pattern: '^b$'}",
			wantDetail: `conflicting pattern ("^a$" and "^b$")`,
		},
		{
			name:       "multipleOf",
			a:          "{type: integer, multipleOf: 2}",
			b:          "{type: integer, multipleOf: 3}",
			wantDetail: "conflicting multipleOf (2 and 3)",
		},
		{
			name: "string vs uuid",
			a:    "{type: string}",
			b:    "{type: string, format: uuid}",
			// A type conflict, not a constraint one — checked via the
			// type-conflict message shape in TestAllOf_TypeConflictMessageNamesBothTypes.
			wantDetail: "incompatible types",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			requireNoErrorDiags(t, diags)
			conflicts := conflictDiags(diags)
			require.Len(t, conflicts, 1, "%s is diagnosed exactly once", tc.name)
			assert.Contains(t, conflicts[0].Message, `"v"`, "the diagnostic names the field")
			assert.Contains(t, conflicts[0].Message, "allOf/0", "names the first branch site")
			assert.Contains(t, conflicts[0].Message, "allOf/1", "names the second branch site")
			assert.Contains(t, conflicts[0].Message, tc.wantDetail,
				"%s: the diagnostic names what actually differs", tc.name)
		})
	}
}

func TestAllOf_CompatibleConstraintRedeclarationsStaySilent(t *testing.T) {
	t.Parallel()
	// The false-positive guards for the constraint path: a keyword present on
	// only one branch (both branches still carry constraints) is intersected,
	// not a conflict — and the merge must genuinely carry both keywords
	// forward on the reconciled property, not just stay quiet about the one it
	// used to drop; equal multipleOf spelled two ways is one value; and an
	// unknown-format scalar resolves through its Base to the same primitive as
	// the plain type, so it is not a type conflict.
	cases := []struct {
		name         string
		a, b         string
		assertMerged func(t *testing.T, c *ir.Constraints)
	}{
		{
			name: "one-sided keywords",
			a:    "{type: string, maxLength: 10}",
			b:    "{type: string, minLength: 2}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.MaxLength, "the first branch's maxLength is kept")
				require.NotNil(t, c.MinLength, "the second branch's minLength is adopted, not dropped")
				assert.Equal(t, int64(10), *c.MaxLength)
				assert.Equal(t, int64(2), *c.MinLength)
			},
		},
		{
			name: "one-sided pattern",
			a:    "{type: string, maxLength: 10}",
			b:    "{type: string, pattern: '^a'}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.MaxLength, "the first branch's maxLength is kept")
				assert.Equal(t, int64(10), *c.MaxLength)
				assert.Equal(t, "^a", c.Pattern, "the second branch's pattern is adopted, not dropped")
			},
		},
		{
			name: "min and exclusiveMin adopted together",
			a:    "{type: number, multipleOf: 2}",
			b:    "{type: number, exclusiveMinimum: 5}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.Min, "the second branch's exclusiveMinimum is adopted as Min")
				assert.Equal(t, "5", c.Min.String())
				assert.True(t, c.ExclusiveMin,
					"ExclusiveMin travels with the adopted Min, not left at its false zero value")
			},
		},
		{name: "equivalent multipleOf", a: "{type: number, multipleOf: 2}", b: "{type: number, multipleOf: 2.0}"},
		{name: "custom format over base", a: "{type: string, format: weird}", b: "{type: string}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			requireNoErrorDiags(t, diags)
			assert.Empty(t, conflictDiags(diags), "%s must not be reported as a conflict", tc.name)
			if tc.assertMerged == nil {
				return
			}
			m, ok := typeByName(doc, "T").(*ir.Model)
			require.True(t, ok, "T should be a model")
			require.Len(t, m.Properties, 1, "v reconciles to one property")
			tc.assertMerged(t, m.Properties[0].Constraints)
		})
	}
}

// The tests below call constraintsConflict and mergeConstraints directly
// rather than through an OpenAPI spec fixture. ir.Constraints is one struct
// shared by every user of the type (properties and lists alike), and the
// OpenAPI compiler does not yet populate Precision, Scale, PatternMessage, or
// the list-owned MinItems/MaxItems/UniqueItems trio from any wire keyword —
// so no YAML spec in this package can drive a redeclaration through those
// fields. The merge and conflict logic must still be correct for them.

func TestConstraintsConflict_PatternMessageConflictReported(t *testing.T) {
	t.Parallel()
	a := &ir.Constraints{PatternMessage: "must be alphanumeric"}
	b := &ir.Constraints{PatternMessage: "must match the legacy format"}
	detail, ok := constraintsConflict(a, b)
	require.True(t, ok, "differing patternMessage is a conflict")
	assert.Contains(t, detail, "patternMessage")
	assert.Contains(t, detail, "must be alphanumeric")
	assert.Contains(t, detail, "must match the legacy format")
}

func TestConstraintsConflict_UniqueItemsNeverConflicts(t *testing.T) {
	t.Parallel()
	// UniqueItems is a bare bool: false cannot be distinguished from "not set",
	// so a differing UniqueItems must never be reported as a conflict in
	// either direction.
	cases := []struct {
		name string
		a, b bool
	}{
		{"true vs false", true, false},
		{"false vs true", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := constraintsConflict(&ir.Constraints{UniqueItems: tc.a}, &ir.Constraints{UniqueItems: tc.b})
			assert.False(t, ok, "differing uniqueItems must not be reported as a conflict")
		})
	}
}

func TestMergeConstraints_UniqueItemsIntersectsToTrue(t *testing.T) {
	t.Parallel()
	// allOf is an intersection: a collection satisfying both branches must be
	// unique whenever either branch requires it, so the merge always adopts
	// true and never downgrades true back to false.
	dst := mergeConstraints(&ir.Constraints{UniqueItems: false}, &ir.Constraints{UniqueItems: true})
	require.NotNil(t, dst)
	assert.True(t, dst.UniqueItems, "dst adopts true from src")

	dst = mergeConstraints(&ir.Constraints{UniqueItems: true}, &ir.Constraints{UniqueItems: false})
	require.NotNil(t, dst)
	assert.True(t, dst.UniqueItems, "dst is never downgraded from true to false")
}

func TestMergeConstraints_AdoptsEveryUnsetKeyword(t *testing.T) {
	t.Parallel()
	// Every keyword ir.Constraints carries: dst leaves each one unset, so the
	// merge must adopt all of them from src, not just the ones exercised by
	// the OpenAPI-spec-driven table tests above.
	five, ten := int64(5), int64(10)
	minVal, maxVal, multipleOf := ir.BigVal("1"), ir.BigVal("9"), ir.BigVal("2")
	src := &ir.Constraints{
		Min: &minVal, ExclusiveMin: true,
		Max: &maxVal, ExclusiveMax: true,
		MultipleOf:     &multipleOf,
		Precision:      &ten,
		Scale:          &five,
		MinLength:      &five,
		MaxLength:      &ten,
		Pattern:        "^a$",
		PatternMessage: "must start with a",
		MinItems:       &five,
		MaxItems:       &ten,
		UniqueItems:    true,
		MinProps:       &five,
		MaxProps:       &ten,
	}
	merged := mergeConstraints(&ir.Constraints{}, src)
	require.NotNil(t, merged)
	assert.Same(t, src.Min, merged.Min)
	assert.Equal(t, src.ExclusiveMin, merged.ExclusiveMin)
	assert.Same(t, src.Max, merged.Max)
	assert.Equal(t, src.ExclusiveMax, merged.ExclusiveMax)
	assert.Same(t, src.MultipleOf, merged.MultipleOf)
	assert.Same(t, src.Precision, merged.Precision)
	assert.Same(t, src.Scale, merged.Scale)
	assert.Same(t, src.MinLength, merged.MinLength)
	assert.Same(t, src.MaxLength, merged.MaxLength)
	assert.Equal(t, src.Pattern, merged.Pattern)
	assert.Equal(t, src.PatternMessage, merged.PatternMessage)
	assert.Same(t, src.MinItems, merged.MinItems)
	assert.Same(t, src.MaxItems, merged.MaxItems)
	assert.Equal(t, src.UniqueItems, merged.UniqueItems)
	assert.Same(t, src.MinProps, merged.MinProps)
	assert.Same(t, src.MaxProps, merged.MaxProps)
}

func TestMergeConstraints_LeavesSetKeywordsUntouched(t *testing.T) {
	t.Parallel()
	// dst already sets every keyword exercised here; src's differing values
	// must never overwrite dst's — first declaration wins, same as every
	// other reconcileProperty field.
	dstMin, srcMin := ir.BigVal("1"), ir.BigVal("99")
	dst := &ir.Constraints{Min: &dstMin, Pattern: "^dst$", PatternMessage: "dst message", UniqueItems: true}
	src := &ir.Constraints{Min: &srcMin, Pattern: "^src$", PatternMessage: "src message", UniqueItems: false}
	merged := mergeConstraints(dst, src)
	require.NotNil(t, merged)
	assert.Same(t, &dstMin, merged.Min, "dst's Min is kept, src's is not adopted")
	assert.Equal(t, "^dst$", merged.Pattern)
	assert.Equal(t, "dst message", merged.PatternMessage)
	assert.True(t, merged.UniqueItems, "true is never downgraded by a false src")
}

func TestMergeConstraints_NilOperands(t *testing.T) {
	t.Parallel()
	// A nil dst adopts src wholesale; a nil src leaves dst untouched; both nil
	// stays nil.
	full := &ir.Constraints{Pattern: "^a$"}
	assert.Same(t, full, mergeConstraints(nil, full), "nil dst adopts src wholesale")
	assert.Same(t, full, mergeConstraints(full, nil), "nil src leaves dst untouched")
	assert.Nil(t, mergeConstraints(nil, nil))
}

func TestConstraintsConflict_DeterministicKeywordOrder(t *testing.T) {
	t.Parallel()
	// minLength is checked before maxLength in constraintsConflict's fixed
	// order, so when both conflict at once the reported keyword is always
	// minLength, never maxLength.
	two, three := int64(2), int64(3)
	twenty, thirty := int64(20), int64(30)
	a := &ir.Constraints{MinLength: &two, MaxLength: &twenty}
	b := &ir.Constraints{MinLength: &three, MaxLength: &thirty}
	detail, ok := constraintsConflict(a, b)
	require.True(t, ok)
	assert.Contains(t, detail, "minLength", "the earlier-checked keyword is reported")
	assert.NotContains(t, detail, "maxLength", "the later-checked keyword is not also reported")
}

func TestAllOf_OpaqueScalarVsPrimitiveNoConflict(t *testing.T) {
	t.Parallel()
	// An opaque scalar (format without a base type) is unknown, not structural,
	// so it's not provably incompatible with a primitive. The "never guess"
	// principle means we don't flag this as a conflict.
	spec := componentSpec(`    OpaqueTest:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {format: custom}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"opaque scalar vs primitive is not flagged as a conflict")
}

func TestAllOf_ThreeWayRedeclarationProducesTwoDiagnostics(t *testing.T) {
	t.Parallel()
	// When three allOf branches declare the same field with different types,
	// reconciliation runs twice: branch[1] vs branch[0], then branch[2] vs branch[0].
	// Each incompatible pair produces one diagnostic, so we expect two total.
	spec := componentSpec(`    ThreeWay:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties:
            id: {type: boolean}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 2, "three-way redeclaration produces two diagnostics")
}

func TestAllOf_ThreeWayCompatibleRedeclarationStaysSilent(t *testing.T) {
	t.Parallel()
	// When three allOf branches declare the same field with compatible types
	// (all the same), no conflict is reported.
	spec := componentSpec(`    ThreeWayCompat:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"three-way compatible redeclaration stays silent")
}

func TestAllOf_SatisfiableNarrowingsStaySilent(t *testing.T) {
	t.Parallel()
	// Each case narrows a base type to a stricter but satisfiable shape under
	// allOf intersection (enum of the base's own primitive, const pinning a
	// legal value, a union narrowed to one of its own members) — none is a
	// provable contradiction. isStructuralType treats Enum, Union, and Literal
	// as scalar-compatible, so a bare scalar sibling against any of these must
	// not be flagged.
	cases := []struct {
		name, a, b string
	}{
		{"enum narrows base type", "{type: string}", "{type: string, enum: [active, inactive]}"},
		{"const narrows base type", "{type: string}", "{type: string, const: dog}"},
		{"union narrowed to one member", "{type: [string, integer]}", "{type: string}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			requireNoErrorDiags(t, diags)
			assert.Empty(t, conflictDiags(diags), "%s must not be reported as a conflict", tc.name)
		})
	}
}

func TestAllOf_EnumVersusIncompatibleTypeStillConflicts(t *testing.T) {
	t.Parallel()
	// Guard against over-correcting the enum false-positive fix: an enum whose
	// declared ValueType is string still provably conflicts with an
	// incompatible primitive (integer). Enum now resolves via resolvePrimKind
	// instead of being treated as opaque structural data, so this comparison
	// goes through the same aok&&bok path as two plain scalars.
	_, diags := lowerSpec(t, allOfConflictSpec(
		"{type: string, enum: [active, inactive]}", "{type: integer}"))
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "enum of strings vs integer is still a provable conflict")
	assert.Contains(t, conflicts[0].Message, `"v"`, "the diagnostic names the conflicting field")
}

func TestAllOf_TypeConflictMessageNamesBothTypes(t *testing.T) {
	t.Parallel()
	// The type-conflict diagnostic must say what actually differs, not just
	// that something did: the two conflicting type identities, so the author
	// can see at a glance what disagreed without cross-referencing the spec.
	_, diags := lowerSpec(t, allOfConflictSpec("{type: string}", "{type: integer}"))
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1)
	assert.Contains(t, conflicts[0].Message, "t/prim/string", "names the first branch's type")
	assert.Contains(t, conflicts[0].Message, "t/prim/integer", "names the second branch's type")
}

func TestAllOf_PropertyAlongsideAllOfConflictMessageIsAccurate(t *testing.T) {
	t.Parallel()
	// A property declared directly on the schema alongside allOf redeclares a
	// field the inline branch also declares. The redeclaration's provenance
	// pointer here is a plain "properties/id" path, never an "allOf/N" branch,
	// so the message must not claim "allOf branches redeclare" — it must read
	// correctly for a co-declared sibling property too (mergeProperty folds
	// both cases the same way; see its doc comment).
	spec := componentSpec(`    Along:
      type: object
      properties:
        id: {type: string}
      allOf:
        - type: object
          properties:
            id: {type: integer}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "the co-declared property conflict is diagnosed once")
	d := conflicts[0]
	assert.Contains(t, d.Message, `"id"`, "the diagnostic names the conflicting field")
	assert.NotContains(t, d.Message, "allOf branches",
		"the redeclaration site is a co-declared property, not an allOf branch")
	assert.Equal(t, "/components/schemas/Along/properties/id", d.Provenance.Pointer,
		"the diagnostic's own site is the co-declared property, not an allOf branch")
}

func TestPreserveKeyword_NilRaw(t *testing.T) {
	t.Parallel()
	l := &lowerer{}
	m := &ir.Model{}
	l.preserveKeyword(m, "openapi:not", nil, "/p", "not")
	assert.Nil(t, m.Extensions, "nil raw is a no-op")
	assert.Empty(t, l.diags)
}

func TestNodeToRaw(t *testing.T) {
	t.Parallel()
	assert.Nil(t, nodeToRaw(nil), "nil node")
	assert.Nil(t, nodeToRaw(&yaml.Node{Kind: yaml.Kind(99)}), "decode error")
	assert.Nil(t, nodeToRaw(yamlNode(t, "1: a\n2: b")), "int-key map: json marshal error")
	raw := nodeToRaw(yamlNode(t, "{a: 1}"))
	assert.JSONEq(t, `{"a":1}`, string(raw))
}

func TestRawPropertyNode_NilSchema(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawPropertyNode(nil, "x"))
}

// TestComponentConstraints_NonSchemaInputs covers the js-level early return: a nil,
// boolean, or reference component has no scalar body to carry constraints.
func TestComponentConstraints_NonSchemaInputs(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Nil(t, l.componentConstraints(nil, "/p"))
	assert.Nil(t, l.componentConstraints(oas3.NewJSONSchemaFromBool(true), "/p"))
	assert.Nil(t, l.componentConstraints(oas3.NewJSONSchemaFromReference("#/components/schemas/Other"), "/p"))
}

// TestSchemaConstraints_EmptyRefSchema covers the schemaConstraints early return: a
// schema whose $ref pointer is present but empty is not a reference (IsReference is
// false) yet its Ref field is set, so it holds no readable constraint body. The
// parser never emits this shape, so it is built by hand.
func TestSchemaConstraints_EmptyRefSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	emptyRef := references.Reference("")
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: &emptyRef})
	assert.Nil(t, l.componentConstraints(js, "/p"))
}

func TestResolveSchemaRef_ReusesInternedSubSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.byPointer[deepPointer] = "t/anon/prev"

	id, ok := l.resolveSchemaRef(emptyEitherSchema(), "#"+deepPointer)
	require.True(t, ok, "a $ref to an already-hoisted sub-schema reuses its ID")
	assert.Equal(t, ir.TypeID("t/anon/prev"), id)
}

func TestResolveSchemaRef_UnresolvedDeepRefDropped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// A same-file $ref to a deep pointer the library never resolved: no interned
	// node, GetResolvedSchema is nil, so the reference is dropped (ok=false).
	js := oas3.NewJSONSchemaFromReference("#" + deepPointer)

	_, ok := l.resolveSchemaRef(js, "#"+deepPointer)
	assert.False(t, ok, "an unresolved deep sub-schema $ref is dropped, not synthesized")
}

func TestHoistSubSchema_NilSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	_, ok := l.hoistSubSchema(nil, deepPointer)
	assert.False(t, ok, "a nil resolved sub-schema cannot be hoisted")
}

func TestHoistSubSchema_BodyInternsAtPointer(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// An object body interns a node at the sub-schema's own pointer, so the
	// pointer-owns-a-node branch returns that node rather than aliasing it.
	object := &oas3.Schema{Type: oas3.NewTypeFromString(oas3.SchemaTypeObject)}

	id, ok := l.hoistSubSchema(object, deepPointer)
	require.True(t, ok)
	assert.Equal(t, anonTypeID(deepPointer), id)
	assert.Equal(t, anonTypeID(deepPointer), l.byPointer[deepPointer])
}

func TestIsRefBranch_Nil(t *testing.T) {
	t.Parallel()
	assert.False(t, isRefBranch(nil))
}

func TestSchema_OneOfWithBoolBranch(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    U:
      anyOf:
        - {type: string}
        - true
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.Len(t, u.Variants, 2, "the boolean branch is a variant, not a null strip")
}
