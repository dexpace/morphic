package schema_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
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
			openapitest.RequireNoErrorDiags(t, diags)
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

func TestLower_ConstraintOnlyUnionIsValidationOnly(t *testing.T) {
	t.Parallel()
	// The "exactly one of" idiom co-declares object structure with a oneOf whose
	// branches only narrow which properties are required. That is validation
	// logic, not shape — dependentRequired's sibling — so the structural body
	// survives and the union is preserved under ReasonValidationOnly, the reason
	// a validation emitter selects on (ir-design §4.7).
	spec := openapitest.ComponentSpec(`    Thing:
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
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "structural body lowers to a Model, not a bare Union")
	require.Len(t, m.Properties, 1)
	assert.Equal(t, "common", m.Properties[0].Name.Source)
	assert.True(t, m.Properties[0].Required)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
	raw, ok := m.Unmodeled["openapi:oneOf"]
	require.True(t, ok, "the union is kept verbatim under Unmodeled")
	assert.Contains(t, string(raw.Value), "required")
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason,
		"constraint-only branches narrow the body without reshaping it (ir-design §4.7)")
	assert.Equal(t, "/components/schemas/Thing/oneOf", raw.Provenance.Pointer)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.ValidationOnlyKeyword, ir.SeverityInfo),
		"the union is reported with §4.7's keyword family; got %+v", diags)
}

func TestLower_BooleanUnionBranchDeclaresNoShape(t *testing.T) {
	t.Parallel()
	// A JSON Schema boolean branch is a whole schema: `true` accepts anything,
	// `false` accepts nothing. Neither states a data shape, so a union built
	// only from them narrows the co-declared body without reshaping it, exactly
	// as a constraint-only branch does, and takes the same validation-only
	// lowering. The branch carries no schema object at all, which is what
	// separates it from a branch that declares nothing structural.
	spec := openapitest.ComponentSpec(`    Flag:
      type: object
      required: [common]
      properties:
        common: {type: string}
      oneOf:
        - true
        - false
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := doc.Types[componentID("Flag")].(*ir.Model)
	require.True(t, ok, "the structural body survives as a Model")
	require.Len(t, m.Properties, 1)
	assert.Equal(t, "common", m.Properties[0].Name.Source)

	raw, ok := m.Unmodeled["openapi:oneOf"]
	require.True(t, ok, "the union is kept verbatim under Unmodeled")
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason,
		"boolean branches declare no shape, so the union is validation-only (ir-design §4.7)")
	assert.Equal(t, "/components/schemas/Flag/oneOf", raw.Provenance.Pointer)
}

func TestLower_AllOfWithOneOfKeepsBoth(t *testing.T) {
	t.Parallel()
	// allOf co-declared with oneOf must not drop the allOf composition.
	spec := openapitest.ComponentSpec(`    Base:
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
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Combo")].(*ir.Model)
	require.True(t, ok, "allOf composition survives (Model), oneOf preserved raw")
	require.NotNil(t, m.Base, "the allOf $ref becomes Base")
	assert.Equal(t, componentID("Base"), m.Base.Target)
	_, ok = m.Unmodeled["openapi:oneOf"]
	assert.True(t, ok, "the oneOf is kept verbatim under Unmodeled")
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

func TestLower_InlineSchemaHoistedOnce(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
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
	openapitest.RequireNoErrorDiags(t, diags)
	itemsID := ir.TypeID("t/anon/components/schemas/S/properties/tags/items")
	item, ok := doc.Types[itemsID].(*ir.Model)
	require.True(t, ok, "items object should be hoisted as a model")
	assert.True(t, item.Anonymous)
	assert.Equal(t, "tags_item", item.Name.Hint)
	assert.Empty(t, item.Name.Source, "hoisted inline types carry a hint, not a source name")
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

func TestLower_MultiTypeUnion(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    MT:
      type: [object, array, string]
      properties: {x: {type: string}}
      items: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "MT").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 3)
	assert.True(t, u.Exclusive)
}

func TestScalar_UnknownFormatPerBaseType(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        i: {type: integer, format: weird}
        n: {type: number, format: weird}
        b: {type: boolean, format: weird}
        s: {type: string, format: weird}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
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

func TestLower_TupleWithTrailingItems(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Tup:
      type: array
      prefixItems: [{type: string}, {type: integer}]
      items: {type: boolean}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	tup, ok := typeByName(doc, "Tup").(*ir.Tuple)
	require.True(t, ok)
	require.Len(t, tup.Elems, 2)
	residue, hasResidue := tup.Unmodeled["openapi:items-after-prefix"]
	require.True(t, hasResidue, "trailing items preserved raw")
	assert.JSONEq(t, `{"type":"boolean"}`, string(residue.Value))
	assert.Equal(t, ir.ReasonDegradedLowering, residue.Reason,
		"an open tuple is lowered to a weaker fixed-arity shape, not left homeless (ir-design §4.8)")
	assert.Equal(t, "/components/schemas/Tup/items", residue.Provenance.Pointer)
	assert.True(t, hasDegradedDiag(diags, "open tuple"),
		"the degradation is reported, not silent; got %+v", diags)
}

// hasDegradedDiag reports whether diags carries a degraded-construct info
// diagnostic whose message contains want.
func hasDegradedDiag(diags []ir.Diagnostic, want string) bool {
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && strings.Contains(d.Message, want) {
			return true
		}
	}
	return false
}

func TestLower_ListConstraints(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    L:
      type: array
      items: {type: string}
      minItems: 1
      maxItems: 9
      uniqueItems: true
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	l, ok := typeByName(doc, "L").(*ir.List)
	require.True(t, ok)
	require.NotNil(t, l.Constraints)
	assert.True(t, l.Constraints.UniqueItems)
	require.NotNil(t, l.Constraints.MinItems)
	assert.Equal(t, int64(1), *l.Constraints.MinItems)
}

func TestLower_ListWithoutItems(t *testing.T) {
	t.Parallel()
	// No `items` → schema.Ref(nil) → element is `any`.
	doc, diags := lowerSpec(t, openapitest.ComponentSpec("    L: {type: array}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	l, ok := typeByName(doc, "L").(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/any"), l.Elem.Target)
}

func TestLower_ValidationOnlyKeywords(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    V:
      type: object
      properties: {a: {type: string}}
      if: {required: [a]}
      then: {required: [b]}
      else: {required: [c]}
      dependentSchemas: {a: {required: [d]}}
      propertyNames: {pattern: "^[a-z]+$"}
      contains: {type: string}
      minContains: 1
      unevaluatedProperties: {type: string}
      unevaluatedItems: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "V").(*ir.Model)
	// An entry the source writes as one keyword is located at that keyword; the
	// three that synthesize several into one object fall back to the schema,
	// since no single node addresses the synthesized value.
	wantPointer := map[string]string{
		"openapi:dependentSchemas": "/components/schemas/V/dependentSchemas",
		"openapi:propertyNames":    "/components/schemas/V/propertyNames",
		"openapi:if-then-else":     "/components/schemas/V",
		"openapi:contains":         "/components/schemas/V",
		"openapi:unevaluated":      "/components/schemas/V",
	}
	for key, want := range wantPointer {
		entry, ok := m.Unmodeled[key]
		require.True(t, ok, "keyword %s preserved", key)
		assert.Equal(t, want, entry.Provenance.Pointer, "entry provenance for %s", key)
	}
	assert.GreaterOrEqual(t, openapitest.CountDiagsAt(diags, diag.ValidationOnlyKeyword, ir.SeverityInfo), 5)
}

// TestLower_PropertyNamesPreserved pins the whole §4.7 contract for
// propertyNames end to end: the verbatim payload, the reason a validation
// emitter selects on, and the one info diagnostic naming the keyword (#117).
func TestLower_PropertyNamesPreserved(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Codes:
      type: object
      propertyNames: {type: string, pattern: "^[a-z]+$"}
      additionalProperties: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := typeByName(doc, "Codes").(*ir.Model)
	require.True(t, ok)
	entry, ok := m.Unmodeled["openapi:propertyNames"]
	require.True(t, ok, "propertyNames kept verbatim; got %v", m.Unmodeled)
	assert.JSONEq(t, `{"type":"string","pattern":"^[a-z]+$"}`, string(entry.Value))
	assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.ValidationOnlyKeyword, ir.SeverityInfo))

	// The keyword constrains keys only: the map's value lowering is untouched.
	require.NotNil(t, m.AdditionalProps)
	assert.Equal(t, ir.TypeID("t/prim/integer"), m.AdditionalProps.Value.Target)
	assert.Nil(t, m.AdditionalProps.Key, "a key *constraint* is not a key type")
}

func TestLower_PropertyDetailRichSchema(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    D:
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
	byWire := openapitest.PropsByWire(m.Properties)
	require.NotNil(t, byWire["withXml"].XML)
	assert.Equal(t, "attribute", byWire["withXml"].XML.NodeType)
	assert.Equal(t, "urn:x", byWire["withXml"].XML.Namespace)
	assert.True(t, byWire["withXml"].XML.Wrapped)
	assert.Len(t, byWire["withExample"].Examples, 3)
	assert.NotEmpty(t, byWire["withExt"].Unmodeled)
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
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        ref: {$ref: '#/components/schemas/Target'}
    Target:
      type: string
      description: target-desc
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.Equal(t, "target-desc", m.Properties[0].Docs.Description)
}

func TestLower_UnresolvedRefDiagnostics(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        ghost: {$ref: '#/components/schemas/Ghost'}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "unresolved ref diagnostic emitted")
}

func TestLower_UnionWithStructuralSiblingVariants(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
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
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"A", "B", "C", "D"} {
		td := typeByName(doc, name)
		require.NotNil(t, td, "type %s present", name)
		c := td.Common()
		_, hasOneOf := c.Unmodeled["openapi:oneOf"]
		_, hasAnyOf := c.Unmodeled["openapi:anyOf"]
		assert.True(t, hasOneOf || hasAnyOf, "union preserved for %s", name)
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
	doc, diags := lowerSpec(t, spec)
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

func TestModel_ValidationOnlyKeywordPreserved(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      not: {required: [b]}
`)
	doc, diags := lowerSpec(t, spec)
	m := doc.Types[componentID("S")].(*ir.Model)
	raw, ok := m.Unmodeled["openapi:not"]
	require.True(t, ok, "not-keyword must be preserved verbatim")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw.Value))
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)
	assert.Equal(t, "/components/schemas/S/not", raw.Provenance.Pointer,
		"the entry locates the keyword, not the schema that carried it")
	found := false
	for _, d := range diags {
		if d.Code == diag.ValidationOnlyKeyword {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			assert.Equal(t, "/components/schemas/S", d.Provenance.Pointer,
				"the diagnostic still reports against the declaring schema")
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}

func TestModel_DefaultBigLiteral(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        n: {type: integer, default: 9007199254740993}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec("    S:\n      type: object\n      properties:\n        n:\n          type: string\n          example: !foo bar\n")
	doc, diags := lowerSpec(t, spec)
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, m.Properties[0].Examples, "the unconvertible example is skipped, not appended")
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/components/schemas/S/properties/n/example", d.Provenance.Pointer)
	assert.Contains(t, d.Message, "example:")
}

func TestModel_ReadOnlyWriteOnlyVisibility(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        r: {type: string, readOnly: true}
        w: {type: string, writeOnly: true}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	byName := openapitest.PropsByWire(m.Properties)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, byName["r"].Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, byName["w"].Visibility)
}

// TestModel_ReadOnlyAndWriteOnlyTogetherAreVisibleNowhere pins the answer for a
// property whose schema writes both flags: they admit disjoint lifecycle sets,
// so the property is admitted by none. Declaring both used to lower exactly as
// declaring readOnly alone did, in silence, while the same pairing spread over
// two allOf branches already intersected to None and warned (GitHub #276).
func TestModel_ReadOnlyAndWriteOnlyTogetherAreVisibleNowhere(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        both: {type: string, readOnly: true, writeOnly: true}
        r: {type: string, readOnly: true}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	byName := openapitest.PropsByWire(m.Properties)

	assert.Equal(t, ir.Visibility{None: true}, byName["both"].Visibility)
	assert.NotEqual(t, byName["r"].Visibility, byName["both"].Visibility,
		"declaring both flags must not lower exactly as declaring readOnly alone does")
	msg := openapitest.DiagMessageAt(t, diags, diag.DisjointVisibility, ir.SeverityWarning,
		"/components/schemas/S/properties/both")
	assert.Contains(t, msg, "writeOnly", "the report names the keyword that was being dropped")
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DisjointVisibility, ir.SeverityWarning),
		"the property declaring one flag is not reported")
}

func TestModel_PasswordFormatSecret(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        pw: {type: string, format: password}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.True(t, m.Properties[0].Secret)
}

func TestModel_AdditionalPropertiesFalseClosed(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      additionalProperties: false
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
}

func TestModel_AdditionalPropertiesSchema(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      additionalProperties: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.AdditionalProps)
	assert.Equal(t, ir.TypeID("t/prim/integer"), m.AdditionalProps.Value.Target)
}

func TestModel_PatternPropertiesOrder(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      patternProperties:
        "^x-": {type: string}
        "^y-": {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
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
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosedAfterComposition, m.Additional)
}

func TestModel_SchemaExtensionPreserved(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      x-rate-limit: 100
      properties: {a: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	raw, ok := m.Unmodeled["openapi:x-rate-limit"]
	require.True(t, ok)
	assert.JSONEq(t, "100", string(raw.Value))
	assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)
	assert.Equal(t, "/components/schemas/S/x-rate-limit", raw.Provenance.Pointer,
		"an entry locates the construct itself, not the node that carries it")
}

func TestModel_TitleDescriptionDocs(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      title: "My Title"
      description: "My Desc"
      properties: {a: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, "My Title", m.Docs.Summary)
	assert.Equal(t, "My Desc", m.Docs.Description)
}

func TestModel_PropertyDeprecation(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        old: {type: string, deprecated: true}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.NotNil(t, m.Properties[0].Deprecation)
}

func TestModel_PropertyXML(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        p: {type: string, xml: {name: n, attribute: true}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.Properties[0].XML)
	assert.Equal(t, "n", m.Properties[0].XML.Name)
	assert.Equal(t, "attribute", m.Properties[0].XML.NodeType)
}

func TestModel_RefSiblingDescriptionWins(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Target: {type: string, description: "target desc"}
    S:
      type: object
      properties:
        p:
          $ref: '#/components/schemas/Target'
          description: "sibling desc"
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, "sibling desc", m.Properties[0].Docs.Description)
}

func TestSchemaRef_EmptyEitherIsAny(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	ref, diags := schema.Ref(l.ctx, l.types, &l.anchors, schema.TopLevelDepth, openapitest.EmptyEitherSchema(), "/p", "h")
	assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
	assert.Empty(t, diags, "an empty either lowers to any without complaint")
}

// TestSchemaExamples_RefdSubSchemaKeepsThem pins the examples of a $ref'd
// internal sub-schema whose body reduces to a shared primitive: the alias
// hoisted for the reference is the first node its pointer owns, and the
// examples belong there rather than on the shared primitive every other string
// in the document also uses.
func TestSchemaExamples_RefdSubSchemaKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/parameters/P/schema'}}
      responses: {'200': {description: ok}}
components:
  parameters:
    P:
      name: p
      in: query
      schema: {type: string, example: sub-example}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	td, ok := doc.Types["t/anon/components/parameters/P/schema"]
	require.True(t, ok, "the referenced sub-schema is hoisted at its own pointer")
	require.Len(t, td.Common().Examples, 1)
	require.NotNil(t, td.Common().Examples[0].Value)
	assert.Equal(t, "sub-example", td.Common().Examples[0].Value.Str)
	assert.Empty(t, doc.Types["t/prim/string"].Common().Examples,
		"the shared primitive carries no per-declaration annotation")
}

// TestSchemaExamples_ComponentRefSiblingKeepsThem pins the examples a component
// writes beside a $ref. They annotate the position they are written at, not the
// referent, so they belong on the alias this component interns — the rule a
// property ($ref plus a sibling example) and a hoisted sub-schema already
// follow. Reading them off the target instead would attribute one component's
// example to every other reference to the same schema.
func TestSchemaExamples_ComponentRefSiblingKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    Base: {type: string}
    Alias:
      $ref: '#/components/schemas/Base'
      example: alias-level
`)
	openapitest.RequireNoErrorDiags(t, diags)
	alias, ok := doc.Types["t/openapi/components/schemas/Alias"]
	require.True(t, ok)
	require.Len(t, alias.Common().Examples, 1)
	require.NotNil(t, alias.Common().Examples[0].Value)
	assert.Equal(t, "alias-level", alias.Common().Examples[0].Value.Str)

	base, ok := doc.Types["t/openapi/components/schemas/Base"]
	require.True(t, ok)
	assert.Empty(t, base.Common().Examples,
		"the referent carries no example the alias wrote beside its own $ref")
}

// TestSiteSchema_BodylessPositions covers the positions that declare no schema
// body to read annotations from: a nil either and a boolean schema.
func TestSiteSchema_BodylessPositions(t *testing.T) {
	t.Parallel()
	assert.Nil(t, annotation.SchemaOf(nil))
	assert.Nil(t, annotation.SchemaOf(oas3.NewJSONSchemaFromBool(true)))
}

func TestSchema_Ref30NullableSiblings(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.0.3", `    Owner:
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
		{
			name:    "3.1 ref to a nullable enum component",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      type: [string, "null"]
      enum: [red, green, null]
`,
			wantNullable: true,
			wantTarget:   targetID,
			// The bit comes from the type array, so this row covers the spelling
			// rather than the enum lowering; that the same declaration is also an
			// Enum of its non-null members is TestEnum_NullMemberNormalizesToNullable
			// and the nullable-enum-31 conformance case.
			msg: "a nullable enum target reads as nullable at a $ref like every other null spelling",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpecVer(tc.version, tc.schemas)
			doc, diags := lowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			m := typeByName(doc, "Owner").(*ir.Model)
			require.Len(t, m.Properties, 1)
			assert.Equal(t, tc.wantNullable, m.Properties[0].Type.Nullable, tc.msg)
			assert.Equal(t, tc.wantTarget, m.Properties[0].Type.Target,
				"ref resolves to the expected target")
		})
	}
}

// TestSchema_NullabilityAgreesAcrossEnumSpellings pins that a value set and a
// type keyword conjoin, and that both ways of writing the same conjunction get
// the same answer.
//
// `{type: [string, "null"], enum: [red, green]}` and
// `{enum: [red, green], oneOf: [{type: string}, {type: "null"}]}` say one thing:
// null is in the type space and out of the value space, so the position does not
// admit it. The type-array spelling used to read the type keyword alone and call
// the position nullable while the oneOf spelling read the enum and called it
// not — two answers for one constraint in a single document (GitHub #288).
//
// Both members of a pair are written into one document on purpose: "the same
// schema, two spellings" is then a property of one compile rather than of two
// runs that could differ for unrelated reasons. The admitting pair is here for
// the same reason the excluding one is — a predicate hardcoded either way fails
// exactly one of them.
func TestSchema_NullabilityAgreesAcrossEnumSpellings(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        excludedByType: {type: [string, "null"], enum: [red, green]}
        excludedByUnion: {enum: [red, green], oneOf: [{type: string}, {type: "null"}]}
        admittedByType: {type: [string, "null"], enum: [red, green, null]}
        admittedByUnion: {enum: [red, green, null], oneOf: [{type: string}, {type: "null"}]}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok, "S is a model")
	props := openapitest.PropsByWire(m.Properties)
	require.Len(t, props, 4)

	pairs := []struct {
		name, typeSpelling, unionSpelling string
		want                              bool
	}{
		{"an enum listing no null member", "excludedByType", "excludedByUnion", false},
		{"an enum listing one", "admittedByType", "admittedByUnion", true},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			got := props[p.typeSpelling].Type.Nullable
			assert.Equal(t, p.want, got, "the enum decides whether the position admits null")
			assert.Equal(t, got, props[p.unionSpelling].Type.Nullable,
				"the type-array and oneOf spellings of one constraint must agree")
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
	spec := openapitest.ComponentSpec("    Target:\n" + indent(6) +
		`    Owner:
      type: object
      properties:
        viaRef: {$ref: '#/components/schemas/Target'}
        inline:
` + indent(10))
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "Owner").(*ir.Model)
	props := openapitest.PropsByWire(m.Properties)
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
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        p:
          type: array
          items: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: integer}, {type: "null"}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	list, ok := doc.Types["t/anon/components/schemas/Owner/properties/p"].(*ir.List)
	require.True(t, ok)
	assert.True(t, list.Elem.Nullable,
		"a nullable union target lifts to the ref wherever it is used, including a list element")
	assert.Equal(t, componentID("Target"), list.Elem.Target)
}

func TestSchema_UnionSiblingsAdditionalAndRequired(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
      additionalProperties: {type: string}
      oneOf: [{type: string}, {type: integer}]
    B:
      required: [x]
      oneOf: [{type: string}, {type: integer}]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"A", "B"} {
		_, ok := typeByName(doc, name).Common().Unmodeled["openapi:oneOf"]
		assert.True(t, ok, "%s preserves its union", name)
	}
}

func TestSchema_RefTargetReadOnlyVisibility(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/RO'}
    RO: {type: string, readOnly: true}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.NotEmpty(t, m.Properties[0].Visibility.Only, "readOnly from the ref target applies")
}

func TestSchema_UnserializableExtension(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      x-bad: {1: intkey}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityWarning), "unserializable extension warns")
	m := typeByName(doc, "S").(*ir.Model)
	_, hasBad := m.Unmodeled["openapi:x-bad"]
	assert.False(t, hasBad, "unserializable extension is dropped, not stored")
}

func TestSchema_EmptyFragmentRef(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: '#'}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "the '#' ref form is unresolved")
}

func TestSchema_EmptyStringRefMirrorBranches(t *testing.T) {
	t.Parallel()
	// An empty-string $ref has IsReference()==false (the ref value is "") yet a
	// non-nil oas3 Ref pointer, exercising that mirror path in schema.Ref and the
	// branchHint fallback.
	spec := openapitest.ComponentSpec(`    Owner:
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
	assert.GreaterOrEqual(t, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError), 2, "both empty refs are unresolved")
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.Contains(t, []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}, "variant_0")
}

func TestAllOf_UntypedRedeclarationDoesNotConflict(t *testing.T) {
	t.Parallel()
	// One branch leaves the field schemaless (the top type), the other types it.
	// `any` intersects with everything under allOf, so this is a narrowing, not a
	// contradiction — it must not be reported.
	spec := openapitest.ComponentSpec(`    Anyish:
      allOf:
        - type: object
          properties:
            id: {description: the identifier}
        - type: object
          properties:
            id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: number, minimum: 10}
        - type: object
          properties:
            n: {type: number, minimum: 10.0}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"10 and 10.0 are the same numeric bound, not a conflict")
}

func TestAllOf_DifferingNumericBoundsConflict(t *testing.T) {
	t.Parallel()
	// Two branches pin the same lower bound to different magnitudes; the kept
	// winner is arbitrary source order, so the dropped bound is surfaced.
	spec := openapitest.ComponentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: integer, minimum: 5}
        - type: object
          properties:
            n: {type: integer, minimum: 10}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "differing numeric bounds are diagnosed once")
	assert.Contains(t, conflicts[0].Message, `"n"`)
}

func TestAllOf_ScalarVersusObjectRedeclarationConflicts(t *testing.T) {
	t.Parallel()
	// A scalar in one branch and a structural type in the other cannot both hold.
	spec := openapitest.ComponentSpec(`    Mixed:
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
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec(`    Objish:
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
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"two distinct inline objects for one field are not a provable conflict")
}

// allOfConflictSpec builds a component whose two inline allOf branches each
// declare field v with the given flow-style schemas, for exercising per-keyword
// redeclaration conflict detection.
func allOfConflictSpec(schemaA, schemaB string) string {
	return openapitest.ComponentSpec(
		"    T:\n" +
			"      allOf:\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaA + "}\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaB + "}\n")
}

func TestAllOf_ConstraintAndFormatConflictsDiagnosed(t *testing.T) {
	t.Parallel()
	// Each way a redeclaration can contradict across branches: an inclusive vs
	// exclusive bound of equal magnitude, a differing pattern, a differing
	// multipleOf, a bound and a multipleOf that differ past the magnitude a
	// rational will build, and two format-derived primitives (string vs uuid).
	// Each keeps the first declaration but surfaces exactly one conflict naming
	// the field, both branch sites, and — for the constraint cases — the
	// offending keyword with both of its conflicting values, so the author
	// never has to diff both branches by hand.
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
			// The other half of the past-a-rational rows in
			// TestAllOf_CompatibleConstraintRedeclarationsStaySilent: a
			// magnitude no rational holds still has to be ordered, not waved
			// through, or the fix for the equal pair would just silence every
			// pair that large.
			name:       "minimum too large for a rational",
			a:          "{type: number, minimum: 1e1000001}",
			b:          "{type: number, minimum: 2e1000001}",
			wantDetail: "conflicting minimum (1e1000001 and 2e1000001)",
		},
		{
			name:       "multipleOf too large for a rational",
			a:          "{type: number, multipleOf: 1e1000001}",
			b:          "{type: number, multipleOf: 1e1000002}",
			wantDetail: "conflicting multipleOf (1e1000001 and 1e1000002)",
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
			openapitest.RequireNoErrorDiags(t, diags)
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
	// used to drop; one magnitude spelled two ways is one value, whether or not
	// it is one a rational will build; and an unknown-format scalar resolves
	// through its Base to the same primitive as the plain type, so it is not a
	// type conflict.
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
		// Both BigVal keywords at a magnitude math/big will not build as a
		// rational. The value is the same on either branch, so there is nothing
		// to report; a comparison that gave up on the magnitude instead would
		// call one value two and fail a --fail-on warning build.
		{
			name: "equal minimum too large for a rational",
			a:    "{type: number, minimum: 1e1000001}",
			b:    "{type: number, minimum: 10e1000000}",
		},
		{
			name: "equal multipleOf too large for a rational",
			a:    "{type: number, multipleOf: 1e1000001}",
			b:    "{type: number, multipleOf: 10e1000000}",
		},
		{name: "custom format over base", a: "{type: string, format: weird}", b: "{type: string}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			openapitest.RequireNoErrorDiags(t, diags)
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

func TestAllOf_OpaqueScalarVsPrimitiveNoConflict(t *testing.T) {
	t.Parallel()
	// An opaque scalar (format without a base type) is unknown, not structural,
	// so it's not provably incompatible with a primitive. The "never guess"
	// principle means we don't flag this as a conflict.
	spec := openapitest.ComponentSpec(`    OpaqueTest:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {format: custom}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"opaque scalar vs primitive is not flagged as a conflict")
}

func TestAllOf_ThreeWayRedeclarationProducesTwoDiagnostics(t *testing.T) {
	t.Parallel()
	// When three allOf branches declare the same field with different types,
	// reconciliation runs twice: branch[1] vs branch[0], then branch[2] vs branch[0].
	// Each incompatible pair produces one diagnostic, so we expect two total.
	spec := openapitest.ComponentSpec(`    ThreeWay:
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
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 2, "three-way redeclaration produces two diagnostics")
}

func TestAllOf_ThreeWayCompatibleRedeclarationStaysSilent(t *testing.T) {
	t.Parallel()
	// When three allOf branches declare the same field with compatible types
	// (all the same), no conflict is reported.
	spec := openapitest.ComponentSpec(`    ThreeWayCompat:
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
	openapitest.RequireNoErrorDiags(t, diags)
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
			openapitest.RequireNoErrorDiags(t, diags)
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
	openapitest.RequireNoErrorDiags(t, diags)
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
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec(`    Along:
      type: object
      properties:
        id: {type: string}
      allOf:
        - type: object
          properties:
            id: {type: integer}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "the co-declared property conflict is diagnosed once")
	d := conflicts[0]
	assert.Contains(t, d.Message, `"id"`, "the diagnostic names the conflicting field")
	assert.NotContains(t, d.Message, "allOf branches",
		"the redeclaration site is a co-declared property, not an allOf branch")
	assert.Equal(t, "/components/schemas/Along/properties/id", d.Provenance.Pointer,
		"the diagnostic's own site is the co-declared property, not an allOf branch")
}

// TestRawFromNode_SeparatesAbsentFromUnconvertible pins the distinction the
// signature exists for (GitHub #144). A caller that cannot tell "there was
// nothing here" from "there was something and it did not survive" announces a
// preservation that never happened, so each row states which of the two it is.
func TestRawFromNode_SeparatesAbsentFromUnconvertible(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		node    *yaml.Node
		want    string
		wantErr bool
	}{
		{name: "absent node is neither raw nor error", node: nil},
		{name: "undecodable node errors", node: &yaml.Node{Kind: yaml.Kind(99)}, wantErr: true},
		{name: "non-string key does not decode", node: openapitest.YAMLNode(t, "? [a, b]\n: v"), wantErr: true},
		{name: "int key does not marshal", node: openapitest.YAMLNode(t, "1: a\n2: b"), wantErr: true},
		{name: "nan decodes but does not marshal", node: openapitest.YAMLNode(t, "{a: .nan}"), wantErr: true},
		{name: "convertible node yields json", node: openapitest.YAMLNode(t, "{a: 1}"), want: `{"a":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := annotation.RawFromNode(tc.node)
			if tc.wantErr {
				require.Error(t, err, "an unconvertible node must be distinguishable from an absent one")
				assert.Nil(t, raw, "nothing is recorded for a node that did not convert")
				return
			}
			require.NoError(t, err)
			if tc.want == "" {
				assert.Nil(t, raw, "an absent node records nothing and reports nothing")
				return
			}
			assert.JSONEq(t, tc.want, string(raw))
		})
	}
}

func TestRawPropertyNode_NilSchema(t *testing.T) {
	t.Parallel()
	assert.Nil(t, annotation.RawPropertyNode(nil, "x"))
}

func TestSchema_OneOfWithBoolBranch(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    U:
      anyOf:
        - {type: string}
        - true
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.Len(t, u.Variants, 2, "the boolean branch is a variant, not a null strip")
}

// TestComponentConstraints_RefSiblingKeepsThem pins the constraint counterpart
// of TestSchemaExamples_ComponentRefSiblingKeepsThem. A bound written beside a
// $ref constrains the position it is written at, so it belongs on the alias the
// component interns — as a property in the same shape already keeps one. The
// referent must not gain it: every other reference to that schema is unbounded.
func TestComponentConstraints_RefSiblingKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    Base: {type: integer}
    Bounded:
      $ref: '#/components/schemas/Base'
      minimum: 5
`)
	openapitest.RequireNoErrorDiags(t, diags)
	bounded, ok := doc.Types["t/openapi/components/schemas/Bounded"].(*ir.Scalar)
	require.True(t, ok, "a component aliasing another schema interns as a Scalar")
	require.NotNil(t, bounded.Constraints)
	require.NotNil(t, bounded.Constraints.Min)
	assert.Equal(t, ir.BigVal("5"), *bounded.Constraints.Min)

	base, ok := doc.Types["t/openapi/components/schemas/Base"]
	require.True(t, ok)
	if s, isScalar := base.(*ir.Scalar); isScalar {
		assert.Nil(t, s.Constraints, "the referent stays unbounded")
	}
}

// TestSchemaSiblings_RefdSubSchemaKeepsThem is the sub-schema arm of the rule
// TestSchemaExamples_ComponentRefSiblingKeepsThem pins for components: a $ref'd
// internal sub-schema that is itself a $ref carrying siblings keeps them on the
// alias hoisted at its own pointer. Resolving the chain to its end before
// hoisting would read the sub-schema as its target and lose both annotations
// before anything could record them — and would attribute nothing to the
// referent, which every other reference to it shares.
func TestSchemaSiblings_RefdSubSchemaKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/Holder/properties/inner'}}
      responses: {'200': {description: ok}}
components:
  schemas:
    Base: {type: integer}
    Holder:
      type: object
      properties:
        inner:
          $ref: '#/components/schemas/Base'
          minimum: 7
          example: 9
`)
	openapitest.RequireNoErrorDiags(t, diags)
	inner, ok := doc.Types["t/anon/components/schemas/Holder/properties/inner"].(*ir.Scalar)
	require.True(t, ok, "the referenced sub-schema hoists an alias at its own pointer")

	require.NotNil(t, inner.Base)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Base"), inner.Base.Target, "the alias points at what it referenced")
	require.NotNil(t, inner.Constraints)
	require.NotNil(t, inner.Constraints.Min)
	assert.Equal(t, ir.BigVal("7"), *inner.Constraints.Min)
	require.Len(t, inner.Examples, 1)
	require.NotNil(t, inner.Examples[0].Value)
	assert.Equal(t, ir.BigVal("9"), inner.Examples[0].Value.Num)

	base, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Base")].(*ir.Scalar)
	require.True(t, ok)
	assert.Nil(t, base.Constraints, "the referent stays unbounded")
	assert.Empty(t, base.Common().Examples, "and unannotated")
}

// TestSchemaSiblings_RefdSubSchemaBareRefAliases is the no-siblings control: a
// sub-schema that is a bare $ref still interns as an alias to its target rather
// than as a second structural copy of it under the sub-schema's own ID.
func TestSchemaSiblings_RefdSubSchemaBareRefAliases(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/Holder/properties/inner'}}
      responses: {'200': {description: ok}}
components:
  schemas:
    Base: {type: object, properties: {n: {type: string}}}
    Holder:
      type: object
      properties:
        inner: {$ref: '#/components/schemas/Base'}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	inner, ok := doc.Types["t/anon/components/schemas/Holder/properties/inner"].(*ir.Scalar)
	require.True(t, ok, "a bare $ref sub-schema aliases rather than copying its target")
	require.NotNil(t, inner.Base)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Base"), inner.Base.Target)

	_, isModel := doc.Types[ir.TypeID("t/openapi/components/schemas/Base")].(*ir.Model)
	assert.True(t, isModel, "the structure lives at the component it was declared at")
}

// inlinePosition is one schema position with no ir.Property or ir.Parameter to
// carry a declaration's annotations, so the declaration must own a node.
type inlinePosition struct {
	name string
	// spec writes body at the position under test.
	spec func(body string) string
	// id is the node the position's declaration owns once it declares anything.
	id ir.TypeID
	// target is what a bare {type: string} at the position resolves to instead.
	target ir.TypeID
}

func inlinePositions() []inlinePosition {
	const prim = ir.TypeID("t/prim/string")
	return []inlinePosition{
		{"items", func(b string) string { return openapitest.ComponentSpec("    A: {type: array, items: " + b + "}\n") },
			"t/anon/components/schemas/A/items", prim},
		{"nested-items", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: array, items: {type: array, items: " + b + "}}\n")
		}, "t/anon/components/schemas/A/items/items", prim},
		{"additionalProperties", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: object, additionalProperties: " + b + "}\n")
		}, "t/anon/components/schemas/A/additionalProperties", prim},
		{"prefixItems", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: array, prefixItems: [" + b + "]}\n")
		}, "t/anon/components/schemas/A/prefixItems/0", prim},
		{"patternProperties", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: object, patternProperties: {\"^x\": " + b + "}}\n")
		}, "t/anon/components/schemas/A/patternProperties/^x", prim},
		{"oneOf-branch", func(b string) string {
			return openapitest.ComponentSpec("    A: {oneOf: [" + b + ", {type: integer}]}\n")
		}, "t/anon/components/schemas/A/oneOf/0", prim},
		{"anyOf-branch", func(b string) string {
			return openapitest.ComponentSpec("    A: {anyOf: [" + b + ", {type: integer}]}\n")
		}, "t/anon/components/schemas/A/anyOf/0", prim},
		{"request-media-type", func(b string) string {
			return openapitest.PathsSpec("  /x:\n    post:\n      operationId: p\n      requestBody:\n" +
				"        content: {application/json: {schema: " + b + "}}\n" +
				"      responses: {\"204\": {description: ok}}\n")
		}, "t/anon/paths/~1x/post/requestBody/content/application~1json/schema", prim},
		{"response-media-type", func(b string) string {
			return openapitest.PathsSpec("  /x:\n    get:\n      operationId: g\n      responses:\n" +
				"        \"200\":\n          description: ok\n" +
				"          content: {application/json: {schema: " + b + "}}\n")
		}, "t/anon/paths/~1x/get/responses/200/content/application~1json/schema", prim},
	}
}

// TestInlinePosition_DeclarationOwnsANode asserts that a schema declaring
// position-scoped annotations at an inline position keeps every one of them, at
// each position that has no carrier to hold them (GitHub #116). Before the fix
// each of these lowered straight to the shared string primitive and dropped all
// five plus the constraint, with no diagnostic.
func TestInlinePosition_DeclarationOwnsANode(t *testing.T) {
	t.Parallel()
	for _, pos := range inlinePositions() {
		t.Run(pos.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, pos.spec(openapitest.InlineProbeBody))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[pos.id].(*ir.Scalar)
			require.True(t, ok, "the declaration owns a Scalar at its own pointer; got %v", doc.Types[pos.id])
			require.NotNil(t, sc.Base)
			assert.Equal(t, pos.target, sc.Base.Target, "the alias still names the shared primitive it reduced to")
			assertProbeAnnotationsKept(t, sc)
		})
	}
}

// assertProbeAnnotationsKept checks every keyword openapitest.InlineProbeBody writes
// survived onto the node the declaration owns.
func assertProbeAnnotationsKept(t *testing.T, sc *ir.Scalar) {
	t.Helper()
	openapitest.AssertProbeDocsKept(t, sc.Docs)
	assert.NotNil(t, sc.Deprecation, "deprecation")
	openapitest.AssertProbeExample(t, sc.Examples)
	if assert.NotNil(t, sc.XML, "xml hints") {
		assert.Equal(t, "X", sc.XML.Name)
	}
	vendor, ok := sc.Unmodeled["openapi:x-vendor"]
	if assert.True(t, ok, "vendor extension: got %v", sc.Unmodeled) {
		assert.JSONEq(t, `"V"`, string(vendor.Value))
		assert.Equal(t, ir.ReasonVendorExtension, vendor.Reason)
	}
	not, ok := sc.Unmodeled["openapi:not"]
	if assert.True(t, ok, "validation-only keyword: got %v", sc.Unmodeled) {
		assert.Equal(t, ir.ReasonValidationOnly, not.Reason)
	}
	if assert.NotNil(t, sc.Constraints, "value constraints") {
		require.NotNil(t, sc.Constraints.MaxLength)
		assert.Equal(t, int64(3), *sc.Constraints.MaxLength)
	}
}

// TestInlinePosition_BareScalarStaysShared is the control that bounds the fix:
// a position declaring nothing of its own gains nothing by owning a node, so it
// must keep resolving straight to the shared primitive rather than growing an
// anonymous alias per element position.
func TestInlinePosition_BareScalarStaysShared(t *testing.T) {
	t.Parallel()
	for _, pos := range inlinePositions() {
		t.Run(pos.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, pos.spec("{type: string}"))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.NotContains(t, doc.Types, pos.id, "a bare declaration must not hoist a node")
			assert.Contains(t, doc.Types, pos.target, "it resolves to the shared primitive instead")
		})
	}
}

// TestInlinePosition_NullStrippedScalarKeepsAnnotations covers the shape that
// reaches the shared primitive by a second route: effectiveTypes strips the
// "null" member before dispatch, so `type: [string, "null"]` lands on the same
// shared node a bare string does. Nullability still lifts onto the refs.
func TestInlinePosition_NullStrippedScalarKeepsAnnotations(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {type: array, items: {type: [string, \"null\"], description: DOC}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	sc, ok := doc.Types["t/anon/components/schemas/A/items"].(*ir.Scalar)
	require.True(t, ok, "a null-stripped scalar declaration owns a node like any other")
	assert.Equal(t, "DOC", sc.Docs.Description)
	require.NotNil(t, sc.Base)
	assert.True(t, sc.Base.Nullable, "the alias records that its base admits null")

	list, ok := doc.Types[componentID("A")].(*ir.List)
	require.True(t, ok)
	assert.True(t, list.Elem.Nullable, "and the element position still reads as nullable")
}

// TestInlinePosition_RefSiblingsKeepTheirPosition covers a $ref written at an
// inline position with keywords beside it: those bind the position, not the
// referent, so they cannot go on the shared target and the position must hold
// them itself.
func TestInlinePosition_RefSiblingsKeepTheirPosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    S: {type: string}\n"+
			"    A: {type: array, items: {$ref: '#/components/schemas/S', maxLength: 25, description: D}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	sc, ok := doc.Types["t/anon/components/schemas/A/items"].(*ir.Scalar)
	require.True(t, ok, "siblings beside a $ref give the position a node of its own")
	require.NotNil(t, sc.Base)
	assert.Equal(t, componentID("S"), sc.Base.Target, "the alias points at what it referenced")
	assert.Equal(t, "D", sc.Docs.Description)
	require.NotNil(t, sc.Constraints)
	require.NotNil(t, sc.Constraints.MaxLength)
	assert.Equal(t, int64(25), *sc.Constraints.MaxLength)

	target, ok := doc.Types[componentID("S")].(*ir.Scalar)
	require.True(t, ok)
	assert.Nil(t, target.Constraints, "the referent stays unbounded")
	assert.Empty(t, target.Docs.Description, "and undocumented")
}

// TestInlinePosition_BareRefStaysDirect is the control for the case above: a
// $ref with nothing beside it still points straight at its target.
func TestInlinePosition_BareRefStaysDirect(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    S: {type: string}\n"+
			"    A: {type: array, items: {$ref: '#/components/schemas/S'}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	list, ok := doc.Types[componentID("A")].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, componentID("S"), list.Elem.Target, "a bare $ref needs no alias")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/items"))
}

// stolenPosition is an inline position an outside $ref can name by pointer:
// the component block declaring it, and the node its declaration owns. The
// outside component is spelled around the owner so both declaration orders of
// the same document are available.
type stolenPosition struct {
	name  string
	owner string
	id    ir.TypeID
}

// spec writes the position's owner and an outside $ref naming it, the reference
// declared first or last. Components lower in source order, so the two are the
// two orders in which the pointer's node can be reached.
func (p stolenPosition) spec(refFirst bool) string {
	outsider := "    Outsider: {$ref: '#" + strings.TrimPrefix(string(p.id), "t/anon") + "'}\n"
	if refFirst {
		return openapitest.ComponentSpec(outsider + p.owner)
	}
	return openapitest.ComponentSpec(p.owner + outsider)
}

func stolenPositions() []stolenPosition {
	return []stolenPosition{
		{"items", "    A: {type: array, items: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/items"},
		{"additionalProperties", "    A: {type: object, additionalProperties: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/additionalProperties"},
		{"prefixItems", "    A: {type: array, prefixItems: [" + openapitest.InlineProbeBody + "]}\n",
			"t/anon/components/schemas/A/prefixItems/0"},
		{"patternProperties", "    A: {type: object, patternProperties: {\"^x\": " + openapitest.InlineProbeBody + "}}\n",
			"t/anon/components/schemas/A/patternProperties/^x"},
		{"oneOf-branch", "    A: {oneOf: [" + openapitest.InlineProbeBody + ", {type: integer}]}\n",
			"t/anon/components/schemas/A/oneOf/0"},
	}
}

// TestInlinePosition_OutsideRefDoesNotMoveTheHome is the regression for the
// second half of the pointer collision. A $ref naming an inline position hoists
// that position's home before the position itself is reached, and the position
// then took the shared node its body reduced to instead — so the annotations
// stayed on a node only the outside reference could see, and `items` resolved to
// the bare primitive. Which of the two lowered first decided it, the
// declaration-order dependence ir-design §4.3 rules out.
//
// The strongest statement of the rule is the first assertion: what a component
// lowers to cannot depend on whether some unrelated schema elsewhere points at
// one of its inner pointers.
func TestInlinePosition_OutsideRefDoesNotMoveTheHome(t *testing.T) {
	t.Parallel()
	for _, pos := range stolenPositions() {
		t.Run(pos.name, func(t *testing.T) {
			t.Parallel()
			alone, diags := parseFull(t, openapitest.ComponentSpec(pos.owner))
			openapitest.RequireNoErrorDiags(t, diags)
			refFirst, diags := parseFull(t, pos.spec(true))
			openapitest.RequireNoErrorDiags(t, diags)
			refLast, diags := parseFull(t, pos.spec(false))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, with := range []*ir.Document{refFirst, refLast} {
				assert.Empty(t, cmp.Diff(alone.Types[componentID("A")], with.Types[componentID("A")]),
					"an outside reference to an inner pointer must not change what A lowers to")
			}
			assert.Empty(t, cmp.Diff(refFirst, refLast, orderInvariantIR()...),
				"declaring that reference before or after A must not change the IR")

			sc, ok := refFirst.Types[pos.id].(*ir.Scalar)
			require.True(t, ok, "the position still owns its home; got %v", refFirst.Types[pos.id])
			assertProbeAnnotationsKept(t, sc)
		})
	}
}

// orderInvariantIR compares two whole IR documents, minus the two fields that
// differ by construction when the same components are declared in two orders.
//
// SourceInfo.Hash digests the source bytes, which are the thing being permuted.
// Naming.Hint is a live gap: the hint a node hoisted at a pointer carries is
// minted by whichever of the two namers reaches the pointer first — the
// declaration's context ("A_item") or the reference's last pointer segment
// ("items") — because intern keeps the first name it is given. That divergence
// is naming only, and predates the annotation work: a $ref to an object-bodied
// `items` shows it with no annotations involved at all. Everything else is
// compared.
func orderInvariantIR() []cmp.Option {
	return []cmp.Option{
		cmpopts.IgnoreFields(ir.Naming{}, "Hint"),
		cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"),
	}
}

// TestPropertyAnnotations_KeptWhenAnOutsideRefNamesTheProperty covers the same
// collision at a position that has a carrier. A $ref naming a property's schema
// pointer hoists a node there, and the property then read that node as its own
// home and left its annotations to it — on top of the copy it had already
// written when it lowered first, and instead of any copy at all when it lowered
// second.
func TestPropertyAnnotations_KeptWhenAnOutsideRefNamesTheProperty(t *testing.T) {
	t.Parallel()
	owner := "    A: {type: object, properties: {p: " + openapitest.InlineProbeBody + "}}\n"
	outsider := "    Outsider: {$ref: '#/components/schemas/A/properties/p'}\n"
	for _, tc := range []struct{ name, spec string }{
		{"reference declared first", openapitest.ComponentSpec(outsider + owner)},
		{"reference declared last", openapitest.ComponentSpec(owner + outsider)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)
			openapitest.RequireNoErrorDiags(t, diags)

			m, ok := doc.Types[componentID("A")].(*ir.Model)
			require.True(t, ok)
			p, ok := openapitest.PropsByWire(m.Properties)["p"]
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target,
				"the property's own type is unchanged by the outside reference")
			openapitest.AssertProbeDocsKept(t, p.Docs)
			assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
			assert.Contains(t, p.Unmodeled, "openapi:not")

			sc, ok := doc.Types["t/anon/components/schemas/A/properties/p"].(*ir.Scalar)
			require.True(t, ok, "and the referenced pointer still names the schema written there")
			assertProbeAnnotationsKept(t, sc)
		})
	}
}

// TestPropertyAnnotations_OneHomeWhenSchemaOwnsANode asserts the settlement of
// the double storage: a property whose schema hoists a node keeps that
// declaration's annotations there and not also on the ir.Property, so the two
// copies can never drift apart.
func TestPropertyAnnotations_OneHomeWhenSchemaOwnsANode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A:\n      type: object\n      properties:\n"+
			"        p: {type: object, description: DOC, deprecated: true, xml: {name: X}, x-vendor: V}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	owner, ok := doc.Types[componentID("A")].(*ir.Model)
	require.True(t, ok)
	p, ok := openapitest.PropsByWire(owner.Properties)["p"]
	require.True(t, ok)
	assert.Empty(t, p.Docs.Description, "the node the schema owns is the one home")
	assert.Nil(t, p.Deprecation)
	assert.Nil(t, p.XML)
	assert.Empty(t, p.Unmodeled)

	inner := doc.Types["t/anon/components/schemas/A/properties/p"]
	require.NotNil(t, inner)
	c := inner.Common()
	assert.Equal(t, "DOC", c.Docs.Description)
	assert.NotNil(t, c.Deprecation)
	require.NotNil(t, c.XML)
	assert.Equal(t, "X", c.XML.Name)
	assert.Contains(t, c.Unmodeled, "openapi:x-vendor")
}

// TestPropertyAnnotations_CarriedWhenSchemaOwnsNoNode is the other half of that
// rule, and the reason a property never hoists an alias of its own: a scalar
// property's declaration has ir.Property to land on already.
func TestPropertyAnnotations_CarriedWhenSchemaOwnsNoNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A:\n      type: object\n      properties:\n        p: "+openapitest.InlineProbeBody+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	owner, ok := doc.Types[componentID("A")].(*ir.Model)
	require.True(t, ok)
	p, ok := openapitest.PropsByWire(owner.Properties)["p"]
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target, "a property keeps resolving to the shared primitive")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/properties/p"))
	openapitest.AssertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	openapitest.AssertProbeExample(t, p.Examples)
	require.NotNil(t, p.XML)
	assert.Equal(t, "X", p.XML.Name)
	assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, p.Unmodeled, "openapi:not")
	require.NotNil(t, p.Constraints)
	require.NotNil(t, p.Constraints.MaxLength)
	assert.Equal(t, int64(3), *p.Constraints.MaxLength)
}

// carrierDocKeyword is one of the three documentation keywords a $ref position
// merges onto its carrier: how to write it with a given value, and which
// ir.Docs field reading it back means the merge kept it.
type carrierDocKeyword struct {
	name  string
	write func(value string) string
	read  func(ir.Docs) string
}

func carrierDocKeywords() []carrierDocKeyword {
	return []carrierDocKeyword{
		{"title",
			func(v string) string { return "title: " + v },
			func(d ir.Docs) string { return d.Summary }},
		{"description",
			func(v string) string { return "description: " + v },
			func(d ir.Docs) string { return d.Description }},
		{"externalDocs",
			func(v string) string { return "externalDocs: {url: 'https://e.example', description: " + v + "}" },
			func(d ir.Docs) string {
				if len(d.ExternalDocs) != 1 {
					return ""
				}
				return d.ExternalDocs[0].Description
			}},
	}
}

// docTarget is a component writing all three keywords, each valued after itself
// so a carrier reading one of them cannot be confused with a carrier reading
// another.
func docTarget() string {
	parts := make([]string, 0, len(carrierDocKeywords()))
	for _, kw := range carrierDocKeywords() {
		parts = append(parts, kw.write("REF-"+kw.name))
	}
	return "    Target: {type: string, " + strings.Join(parts, ", ") + "}\n"
}

// propertyOf returns the named component model's property with the given wire
// name.
func propertyOf(t *testing.T, doc *ir.Document, model, wire string) ir.Property {
	t.Helper()
	m, ok := doc.Types[componentID(model)].(*ir.Model)
	require.True(t, ok, "%s lowers to a model", model)
	p, ok := openapitest.PropsByWire(m.Properties)[wire]
	require.True(t, ok, "%s declares a property %q", model, wire)
	return p
}

// TestPropertyDocs_RefTargetReachesTheCarrier pins the referent half of the
// documentation merge, keyword by keyword: a property that declares nothing but
// a $ref still carries what its referent documents, and the referent's own node
// keeps its copy. ir-design §14 asks for that merge uniformly, so the three
// keywords stand or fall together — a fix that inherited only the description
// would leave the other two behind at every carrier.
func TestPropertyDocs_RefTargetReachesTheCarrier(t *testing.T) {
	t.Parallel()
	for _, kw := range carrierDocKeywords() {
		t.Run(kw.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(docTarget()+
				"    Owner: {type: object, properties: {p: {$ref: '#/components/schemas/Target'}}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			p := propertyOf(t, doc, "Owner", "p")
			assert.Equal(t, componentID("Target"), p.Type.Target, "a bare $ref needs no alias")
			assert.Equal(t, "REF-"+kw.name, kw.read(p.Docs), "the carrier reads the referent's %s", kw.name)
			assert.Equal(t, "REF-"+kw.name, kw.read(doc.Types[componentID("Target")].Common().Docs),
				"and the referent keeps its own %s", kw.name)
		})
	}
}

// TestPropertyDocs_UseSiteWinsKeywordByKeyword pins the other half: a keyword
// written beside the $ref replaces that field and only that field, the rest
// still arriving from the referent. Taking either schema whole passes one of
// these subtests and fails the other two.
func TestPropertyDocs_UseSiteWinsKeywordByKeyword(t *testing.T) {
	t.Parallel()
	for _, written := range carrierDocKeywords() {
		t.Run(written.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(docTarget()+
				"    Owner: {type: object, properties: {p: {$ref: '#/components/schemas/Target', "+
				written.write("SITE")+"}}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			p := propertyOf(t, doc, "Owner", "p")
			assert.Equal(t, componentID("Target"), p.Type.Target, "a carrier hoists no alias for its siblings")
			for _, kw := range carrierDocKeywords() {
				want := "REF-" + kw.name
				if kw.name == written.name {
					want = "SITE"
				}
				assert.Equal(t, want, kw.read(p.Docs), "%s, with %s written at the site", kw.name, written.name)
			}
		})
	}
}

// TestInlinePosition_HoistGateFollowsWhatIsKept walks every keyword that only a
// node of its own can hold, one at a time, and requires each to give the
// position that node. The gate and the two things that fill it —
// attachDeclaredAnnotations and internAlias — have to agree keyword for
// keyword: one listed here but stored nowhere hoists an empty node, and one
// stored but missing here is still dropped.
func TestInlinePosition_HoistGateFollowsWhatIsKept(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, keyword string }{
		{"title", "title: T"},
		{"externalDocs", "externalDocs: {url: 'https://e.example'}"},
		{"deprecated", "deprecated: true"},
		{"xml", "xml: {name: X}"},
		{"extension", "x-vendor: V"},
		{"example", "example: a"},
		{"examples", "examples: [a]"},
		{"minimum", "minimum: 1"},
		{"maximum", "maximum: 9"},
		{"multipleOf", "multipleOf: 2"},
		{"exclusiveMinimum", "exclusiveMinimum: 1"},
		{"exclusiveMaximum", "exclusiveMaximum: 9"},
		{"minLength", "minLength: 1"},
		{"maxLength", "maxLength: 9"},
		{"pattern", "pattern: '^a'"},
		{"minProperties", "minProperties: 1"},
		{"maxProperties", "maxProperties: 9"},
		{"not", "not: {const: N}"},
		{"contains", "contains: {type: string}"},
		{"minContains", "minContains: 1"},
		{"maxContains", "maxContains: 9"},
		{"if", "if: {const: a}"},
		{"then", "then: {const: a}"},
		{"else", "else: {const: a}"},
		{"dependentSchemas", "dependentSchemas: {a: {type: string}}"},
		{"propertyNames", "propertyNames: {maxLength: 4}"},
		{"unevaluatedItems", "unevaluatedItems: {type: string}"},
		{"unevaluatedProperties", "unevaluatedProperties: {type: string}"},
		{"default", "default: a"},
		{"readOnly", "readOnly: true"},
		{"writeOnly", "writeOnly: true"},
		{"dependentRequired", "dependentRequired: {a: [b]}"},
		{"contentEncoding", "contentEncoding: base64"},
		{"contentMediaType", "contentMediaType: image/png"},
		{"contentSchema", "contentSchema: {type: object}"},
		{"$dynamicRef", "$dynamicRef: '#nosuch'"},
		{"$id", "$id: 'urn:example:i'"},
		{"$schema", "$schema: 'https://json-schema.org/draft/2020-12/schema'"},
		{"$vocabulary", "$vocabulary: {'https://json-schema.org/draft/2020-12/vocab/core': true}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    A: {type: array, items: {type: string, "+tc.keyword+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Contains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/items"),
				"%s binds the position it is written at, so the position must own a node", tc.keyword)
		})
	}
}

// TestInlinePosition_NothingToHoldHoistsNoNode is the negative half of that
// gate: a schema that declares nothing position-scoped, and one whose only
// extra keyword is a `false` unevaluatedProperties — model openness rather than
// a preserved keyword. Hoisting for either would produce a node holding nothing.
func TestInlinePosition_NothingToHoldHoistsNoNode(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, keyword string }{
		{"none", "type: string"},
		{"unevaluatedProperties-false", "type: string, unevaluatedProperties: false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    A: {type: array, items: {"+tc.keyword+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/items"))
			list, ok := doc.Types[componentID("A")].(*ir.List)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/string"), list.Elem.Target)
		})
	}
}

// TestInlinePosition_ResidueIsKeptAtEveryHomeOwnNodePosition pins the other half
// of the residue rule: default/readOnly/writeOnly bind a *use* of a type, and
// the position that writes one is usually not a property — so every position
// that owns its node keeps them, not just a named component declaration
// (GitHub #138).
func TestInlinePosition_ResidueIsKeptAtEveryHomeOwnNodePosition(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, at string }{
		{"items", "    A: {type: array, items: {type: string, RESIDUE}}\n",
			"t/anon/components/schemas/A/items"},
		{"additionalProperties", "    A: {type: object, additionalProperties: {type: string, RESIDUE}}\n",
			"t/anon/components/schemas/A/additionalProperties"},
		{"patternProperties", "    A: {type: object, patternProperties: {'^a': {type: string, RESIDUE}}}\n",
			"t/anon/components/schemas/A/patternProperties/^a"},
		{"prefixItems", "    A: {type: array, prefixItems: [{type: string, RESIDUE}]}\n",
			"t/anon/components/schemas/A/prefixItems/0"},
		{"oneOf branch", "    A: {oneOf: [{type: string, RESIDUE}, {type: integer}]}\n",
			"t/anon/components/schemas/A/oneOf/0"},
		{"ref sibling", "    B: {type: string}\n" +
			"    A: {type: array, items: {$ref: '#/components/schemas/B', RESIDUE}}\n",
			"t/anon/components/schemas/A/items"},
	}
	const residue = "default: a, readOnly: true"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(strings.ReplaceAll(tc.body, "RESIDUE", residue)))
			openapitest.RequireNoErrorDiags(t, diags)

			td, ok := doc.Types[ir.TypeID(tc.at)]
			require.True(t, ok, "the position owns a node to hold what it wrote")
			assertResidueKeptAndAnnounced(t, td.Common().Unmodeled, diags, "default", `"a"`)
			assertResidueKeptAndAnnounced(t, td.Common().Unmodeled, diags, "readOnly", `true`)
		})
	}
}

// assertResidueKeptAndAnnounced checks one residue keyword's entry and its
// diagnostic together. The external test package has assertResidueKept and
// assertResidueDiag for the same job in two calls; this package cannot reach
// them, so the pairing lives here under a name that is not a transposition of
// either.
func assertResidueKeptAndAnnounced(t *testing.T, p ir.Unmodeled, diags []ir.Diagnostic, keyword, wantJSON string) {
	t.Helper()
	entry, ok := p["openapi:"+keyword]
	require.True(t, ok, "%s must be kept verbatim under Unmodeled", keyword)
	assert.JSONEq(t, wantJSON, string(entry.Value))
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)

	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityInfo &&
			strings.HasSuffix(d.Provenance.Pointer, "/"+keyword) {
			return
		}
	}
	assert.Fail(t, "missing residue diagnostic", "nothing announced %s; got %+v", keyword, diags)
}

// TestPropertyPosition_ResidueStaysOutOfTheTypeNode is the deliberate non-goal.
// A property lands all three in their real fields, so recording them again as
// residue would restate what the carrier already holds.
func TestPropertyPosition_ResidueStaysOutOfTheTypeNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {type: object, properties: {p: {type: array, items: {type: string}, "+
			"default: [], readOnly: true}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := propertyOf(t, doc, "A", "p")
	require.NotNil(t, p.Default, "default lands in the property's own field")
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{
		ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery,
	}}, p.Visibility, "readOnly lands in the property's own field")

	td, ok := doc.Types[p.Type.Target]
	require.True(t, ok)
	assert.NotContains(t, td.Common().Unmodeled, "openapi:default",
		"the carrier holds it, so the node must not restate it")
	assert.NotContains(t, td.Common().Unmodeled, "openapi:readOnly",
		"the carrier holds it, so the node must not restate it")
	assert.Empty(t, p.Unmodeled, "nor does the property preserve what it modelled")
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
			excluded: "neither carried nor honoured: Milestone 1 resolves same-document JSON " +
				"pointers only, so $ref: '#a' is reported unresolved rather than resolving " +
				"through the anchor (GitHub #141). Excluded because the compiler changes " +
				"nothing for it today, not because dropping it is settled",
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

// TestVocabulary2020_12_EveryKeywordIsLoweredOrKept compiles each 2020-12 keyword
// twice with different values, and once more with the keyword omitted, then
// requires all three IR documents to differ. A keyword the compiler neither
// lowers nor keeps verbatim produces an identical document, which is exactly the
// silent drop GitHub #125 catalogued by hand — so this fails on the next one
// instead of waiting for a reader to notice.
//
// What the two values buy is the difference between noticing a keyword and
// carrying it. A keyword whose mere presence reshapes the IR — hoisting a node,
// switching a lowering — moves the document without its value going anywhere,
// and a control-versus-written comparison alone cannot tell the two apart. Two
// values that produce one document say the value was read and discarded.
//
// What it does not claim: that every spelling of a keyword survives. It compares
// one pair of values per keyword, so a lowering that carried one value and
// dropped another would still pass. It is a floor on what reaches the IR, not an
// inventory of it.
//
// The excluded rows are the inverse assertion: each states why nothing carries
// the keyword, and the test holds them to producing no difference at all, so a
// keyword that starts being carried has to move its justification rather than
// keep it.
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

// TestContentVocabulary_LowersToEncoding pins where the 2020-12 content
// vocabulary lands: contentEncoding on Encoding.Name, contentMediaType on
// Encoding.MediaType, on the Scalar node the position hoists rather than on the
// shared primitive every other declaration of that type also resolves to.
func TestContentVocabulary_LowersToEncoding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, schemas string
		at            ir.TypeID
		want          ir.Encoding
	}{
		{
			name:    "component",
			schemas: "    A: {type: string, contentEncoding: base64, contentMediaType: image/png}\n",
			at:      componentID("A"),
			want:    ir.Encoding{Name: "base64", MediaType: "image/png"},
		},
		{
			name:    "property",
			schemas: "    A: {type: object, properties: {p: {type: string, contentMediaType: text/csv}}}\n",
			at:      "t/anon/components/schemas/A/properties/p",
			want:    ir.Encoding{MediaType: "text/csv"},
		},
		{
			name:    "items",
			schemas: "    A: {type: array, items: {type: string, contentEncoding: base32}}\n",
			at:      "t/anon/components/schemas/A/items",
			want:    ir.Encoding{Name: "base32"},
		},
		{
			name:    "beside a known format",
			schemas: "    A: {type: string, format: date-time, contentMediaType: text/plain}\n",
			at:      componentID("A"),
			want:    ir.Encoding{MediaType: "text/plain"},
		},
		{
			name:    "format byte agrees with contentEncoding",
			schemas: "    A: {type: string, format: byte, contentEncoding: base64}\n",
			at:      componentID("A"),
			want:    ir.Encoding{Name: "base64", WireType: &ir.TypeRef{Target: "t/prim/string"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[tc.at].(*ir.Scalar)
			require.True(t, ok, "the content vocabulary needs a Scalar of its own at %s", tc.at)
			require.NotNil(t, sc.Encoding)
			assert.Empty(t, cmp.Diff(tc.want, *sc.Encoding))
			assert.Empty(t, sc.Unmodeled, "a keyword with a field is lowered, never also kept raw")
		})
	}
}

// TestContentVocabulary_KeepsTheBoundsWrittenBesideIt pins that hoisting a node
// for the content vocabulary does not cost the position its value constraints.
// Owning a node is what stops hoistDeclarationHome hoisting the alias that
// carries them, so the node the content vocabulary lands on has to carry them
// itself — otherwise adding contentEncoding to a bounded string deletes its
// bounds, which is a lossy lowering nothing reports (invariant 2).
func TestContentVocabulary_KeepsTheBoundsWrittenBesideIt(t *testing.T) {
	t.Parallel()
	five, nine := int64(5), int64(9)
	cases := []struct {
		name, schemas string
		at            ir.TypeID
		want          ir.Constraints
	}{
		{
			name:    "a component",
			schemas: "    A: {type: string, contentEncoding: base64, minLength: 5, maxLength: 9, pattern: '^[a-z]+$'}\n",
			at:      componentID("A"),
			want:    ir.Constraints{MinLength: &five, MaxLength: &nine, Pattern: "^[a-z]+$"},
		},
		{
			name:    "an inline position",
			schemas: "    A: {type: array, items: {type: string, contentMediaType: text/csv, minLength: 5}}\n",
			at:      "t/anon/components/schemas/A/items",
			want:    ir.Constraints{MinLength: &five},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[tc.at].(*ir.Scalar)
			require.True(t, ok, "the content vocabulary hoists a Scalar at %s", tc.at)
			require.NotNil(t, sc.Encoding, "and lowers the keyword onto it")
			require.NotNil(t, sc.Constraints, "while keeping the bounds written beside it")
			assert.Empty(t, cmp.Diff(tc.want, *sc.Constraints))
		})
	}
}

// TestContentVocabulary_KeptWhereNoEncodingHolds covers the other half: a schema
// with no Encoding field to fill, and contentSchema, which has no IR field at any
// position.
func TestContentVocabulary_KeptWhereNoEncodingHolds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, schemas, key, wantJSON string
		at                           ir.TypeID
	}{
		{
			name:    "array position has no Encoding",
			schemas: "    A: {type: array, items: {type: string}, contentMediaType: application/zip}\n",
			key:     "openapi:contentMediaType", wantJSON: `"application/zip"`, at: componentID("A"),
		},
		{
			name:    "contentSchema has no field anywhere",
			schemas: "    A: {type: string, contentSchema: {type: object}}\n",
			key:     "openapi:contentSchema", wantJSON: `{"type":"object"}`, at: componentID("A"),
		},
		{
			name:    "format loses Encoding.Name to contentEncoding",
			schemas: "    A: {type: string, format: iban, contentEncoding: base64}\n",
			key:     "openapi:format", wantJSON: `"iban"`, at: componentID("A"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			td, ok := doc.Types[tc.at]
			require.True(t, ok)
			entry, ok := td.Common().Unmodeled[tc.key]
			require.True(t, ok, "%s must be kept verbatim under Unmodeled", tc.key)
			assert.JSONEq(t, tc.wantJSON, string(entry.Value))
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			openapitest.AssertInfoDiagAt(t, diags, entry.Provenance.Pointer)
		})
	}
}

// TestContentVocabulary_KeptOnACarrierWithNoNode covers the position whose
// schema owns no node at all: a property whose declared shape reduced to a shared
// primitive, where the ir.Property is the only home the keyword has.
func TestContentVocabulary_KeptOnACarrierWithNoNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {type: object, properties: {p: {contentMediaType: application/json}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := propertyOf(t, doc, "A", "p")
	assert.Equal(t, ir.TypeID("t/prim/any"), p.Type.Target, "an untyped schema stays schemaless")
	entry, ok := p.Unmodeled["openapi:contentMediaType"]
	require.True(t, ok, "the carrier is the only home when the schema hoisted no node")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	openapitest.AssertInfoDiagAt(t, diags, entry.Provenance.Pointer)
}

// TestDynamicRef_ExpandsAgainstTheOneMatchingAnchor pins the resolvable half of
// ir-design §4.7's $dynamicRef promise, including the recursive shape that makes
// the keyword worth having.
func TestDynamicRef_ExpandsAgainstTheOneMatchingAnchor(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Meta: {$dynamicAnchor: meta, type: object, properties: {n: {type: string}}}\n"+
			"    Uses: {type: object, properties: {m: {$dynamicRef: '#meta'}}}\n"+
			"    Tree: {$dynamicAnchor: node, type: object, properties: {kids: {type: array, items: {$dynamicRef: '#node'}}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, componentID("Meta"), propertyOf(t, doc, "Uses", "m").Type.Target,
		"the reference resolves to the anchor's own component, not to the top type")

	kids, ok := doc.Types[ir.TypeID("t/anon/components/schemas/Tree/properties/kids")].(*ir.List)
	require.True(t, ok)
	elem, ok := doc.Types[kids.Elem.Target].(*ir.Scalar)
	require.True(t, ok, "the items position keeps a node of its own")
	require.NotNil(t, elem.Base)
	assert.Equal(t, componentID("Tree"), elem.Base.Target,
		"a self-recursive dynamic reference terminates on the interned component ID")

	for _, td := range doc.Types {
		assert.NotContains(t, td.Common().Unmodeled, "openapi:$dynamicRef",
			"an expanded reference is the position's type; keeping it too would read as ignored")
	}

	// Expansion collapses an indirection the source left to evaluation, so it is
	// announced under its own code rather than sharing the composition one.
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DynamicRefExpanded, ir.SeverityInfo),
		"each expanded reference is announced once")
}

// TestDynamicRef_FragmentIsPercentDecoded pins that a $dynamicRef's fragment is
// read as the URI text it is. RFC 3986 §2.3 makes an unreserved character and
// its percent-encoded octet the same character, so `#my%2Danchor` names the
// anchor `my-anchor`; §2.1 makes the escape's hex digits case-insignificant. All
// three spellings below therefore address one declaration (GitHub #233).
func TestDynamicRef_FragmentIsPercentDecoded(t *testing.T) {
	t.Parallel()
	const anchor = "    B: {$dynamicAnchor: my-anchor, type: string}\n"
	cases := map[string]string{
		"an escaped hyphen is the hyphen":           "#my%2Danchor",
		"the escape's hex case is not significant":  "#my%2danchor",
		"the unescaped spelling names the same one": "#my-anchor",
	}
	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(anchor+"    A: {$dynamicRef: '"+ref+"'}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
			require.True(t, ok, "the reference position owns a node")
			require.NotNil(t, sc.Base)
			assert.Equal(t, componentID("B"), sc.Base.Target, "every spelling names the one anchor")
			assert.NotContains(t, sc.Unmodeled, "openapi:$dynamicRef",
				"an expanded reference is the position's type, so it is not also kept")
		})
	}
}

// TestDynamicRef_ReachesAnAnchorSpelledWithAPercent pins that the decode runs on
// the reference alone: an anchor carrying a literal `%` is addressed by escaping
// it as `%25`, and the anchor's own text is matched exactly as declared. The two
// sides mean different things — 2020-12 §8.2.2 makes `$dynamicAnchor` a plain
// name, §8.2.3.2 makes `$dynamicRef` a URI-reference — so only one is decoded.
//
// §8.2.2's production admits no `%` in an anchor name, so the document validator
// reports the declaration and this case arrives with an error diagnostic beside
// it, the same shape TestDynamicRef_NonScalarValueIsKeptNotExpanded documents.
// The lowering still has to agree with itself about what each side spells.
func TestDynamicRef_ReachesAnAnchorSpelledWithAPercent(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpec(
		"    B: {$dynamicAnchor: 'pct%name', type: string}\n"+
			"    A: {$dynamicRef: '#pct%25name'}\n"))

	sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
	require.True(t, ok, "the reference position owns a node")
	require.NotNil(t, sc.Base)
	assert.Equal(t, componentID("B"), sc.Base.Target,
		"an escaped percent addresses the percent itself, so the reference reaches the anchor as declared")
}

// TestDynamicRef_IrreducibleIsKeptAndSaysWhy pins the other half of that promise.
// Each case is a reference no static lowering can resolve, and each must survive
// verbatim with the reason naming which case it was.
func TestDynamicRef_IrreducibleIsKeptAndSaysWhy(t *testing.T) {
	t.Parallel()
	const anchors = "    M1: {$dynamicAnchor: dup, type: string}\n" +
		"    M2: {$dynamicAnchor: dup, type: integer}\n" +
		"    Deep: {type: object, properties: {i: {$dynamicAnchor: deep, type: string}}}\n"
	const anchor = "    M: {$dynamicAnchor: m, type: string}\n"
	// at is the node keeping the reference, defaulting to the component A itself.
	cases := []struct {
		name, schemas, wantWhy string
		at                     ir.TypeID
	}{
		{name: "no such anchor", schemas: "    A: {$dynamicRef: '#nope'}\n",
			wantWhy: "no $dynamicAnchor \"nope\" is declared"},
		{name: "another document", schemas: "    A: {$dynamicRef: 'other.json#m'}\n",
			wantWhy: "is not a plain same-document fragment"},
		{name: "a pointer, not an anchor", schemas: "    A: {$dynamicRef: '#/components/schemas/M1'}\n",
			wantWhy: "not a plain same-document fragment"},
		{name: "an invalid percent escape", schemas: "    A: {$dynamicRef: '#my%zzanchor'}\n",
			wantWhy: `"#my%zzanchor" is not valid percent-encoded text: invalid URL escape "%zz"`},
		{name: "an escaped percent decodes to the character", schemas: "    A: {$dynamicRef: '#pct%25name'}\n",
			wantWhy: `no $dynamicAnchor "pct%name" is declared`},
		// The anchor here is what a second decode would land on: it would read
		// this fragment as "#my-anchor" and expand. Being kept is the proof.
		{name: "the decode is not applied twice",
			schemas: "    H: {$dynamicAnchor: my-anchor, type: string}\n    A: {$dynamicRef: '#my%252Danchor'}\n",
			wantWhy: `no $dynamicAnchor "my%2Danchor" is declared`},
		{name: "declared twice", schemas: anchors + "    A: {$dynamicRef: '#dup'}\n",
			wantWhy: "declared 2 times"},
		{name: "not a component", schemas: anchors + "    A: {$dynamicRef: '#deep'}\n",
			wantWhy: "rather than on a component schema"},
		{name: "co-declared with a shape", schemas: anchors + "    A: {$dynamicRef: '#deep', type: string}\n",
			wantWhy: "co-declared with another applicator"},
		{name: "co-declared with items", schemas: anchor + "    A: {$dynamicRef: '#m', items: {type: integer}}\n",
			wantWhy: "co-declared with another applicator"},
		{name: "co-declared with prefixItems", schemas: anchor + "    A: {$dynamicRef: '#m', prefixItems: [{type: integer}]}\n",
			wantWhy: "co-declared with another applicator"},
		{name: "co-declared with required", schemas: anchor + "    A: {$dynamicRef: '#m', required: [p]}\n",
			wantWhy: "co-declared with another applicator"},
		{
			name:    "co-declared with a $ref",
			schemas: anchor + "    A: {$dynamicRef: '#m', $ref: '#/components/schemas/M'}\n",
			wantWhy: "co-declared with another applicator",
		},
		{
			name:    "co-declared with a union",
			schemas: anchor + "    A: {$dynamicRef: '#m', oneOf: [{type: string}, {type: integer}]}\n",
			wantWhy: "co-declared with another applicator",
		},
		{
			name: "the anchor is in a resource of its own",
			schemas: "    M: {$id: 'https://example.com/m', $dynamicAnchor: m, type: string}\n" +
				"    A: {$dynamicRef: '#m'}\n",
			wantWhy: `an $id at or above "/components/schemas/M"`,
		},
		{
			name:    "the reference is in a resource of its own",
			schemas: anchor + "    A: {$id: 'https://example.com/a', $dynamicRef: '#m'}\n",
			wantWhy: `an $id at or above "/components/schemas/A"`,
		},
		{
			name: "an enclosing schema starts the resource",
			schemas: anchor +
				"    A: {$id: 'https://example.com/a', type: array, items: {$dynamicRef: '#m'}}\n",
			wantWhy: `an $id at or above "/components/schemas/A/items"`,
			at:      "t/anon/components/schemas/A/items",
		},
		{
			name: "expanding it would close a cycle",
			schemas: "    A: {$dynamicAnchor: a, $dynamicRef: '#b'}\n" +
				"    B: {$dynamicAnchor: b, $dynamicRef: '#a'}\n",
			wantWhy: `closes a cycle of $dynamicRef expansions back onto "/components/schemas/A"`,
		},
		{
			name:    "a self-reference is that cycle at length one",
			schemas: "    A: {$dynamicAnchor: a, $dynamicRef: '#a'}\n",
			wantWhy: `closes a cycle of $dynamicRef expansions back onto "/components/schemas/A"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			at := tc.at
			if at == "" {
				at = componentID("A")
			}
			td, ok := doc.Types[at]
			require.True(t, ok, "the position owns a node to keep the reference on")
			entry, ok := td.Common().Unmodeled["openapi:$dynamicRef"]
			require.True(t, ok, "an irreducible $dynamicRef is kept verbatim, not dropped")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assertDiagContains(t, diags, entry.Provenance.Pointer, tc.wantWhy)
		})
	}
}

// TestDynamicRef_CycleIsRefusedAtEveryEdge pins that a cycle of $dynamicRef
// expansions is preserved whole rather than broken at whichever edge lowered
// first. Each member reaches the verdict from its own position, so no Scalar
// ends up with a Base chain that never terminates — the shape refCycles already
// refuses for every pure-$ref cycle, and which neither pass.Validate nor
// irverify checks for aliases.
func TestDynamicRef_CycleIsRefusedAtEveryEdge(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {$dynamicAnchor: a, $dynamicRef: '#b'}\n"+
			"    B: {$dynamicAnchor: b, $dynamicRef: '#a'}\n"+
			"    Self: {$dynamicAnchor: s, $dynamicRef: '#s'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	for _, name := range []string{"A", "B", "Self"} {
		sc, ok := doc.Types[componentID(name)].(*ir.Scalar)
		require.True(t, ok, "%s still owns a node", name)
		require.NotNil(t, sc.Base)
		assert.Equal(t, ir.TypeID("t/prim/any"), sc.Base.Target,
			"%s aliases the top type, never a link of the cycle", name)
		assert.Contains(t, sc.Unmodeled, "openapi:$dynamicRef",
			"%s keeps the reference it could not take", name)
	}
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, diag.DynamicRefExpanded, ir.SeverityInfo),
		"no edge of the cycle is reported as expanded")
}

// TestDynamicRef_ExpandsIntoACycleItIsNotPartOf pins the other side of that
// verdict: a position outside a cycle still expands onto it. Every member of the
// cycle refuses its own expansion, so the target is an ordinary alias over the
// top type rather than a link that loops.
func TestDynamicRef_ExpandsIntoACycleItIsNotPartOf(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {$dynamicAnchor: a, $dynamicRef: '#b'}\n"+
			"    B: {$dynamicAnchor: b, $dynamicRef: '#a'}\n"+
			"    Outside: {$dynamicRef: '#a'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	sc, ok := doc.Types[componentID("Outside")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, sc.Base)
	assert.Equal(t, componentID("A"), sc.Base.Target, "the expansion is taken")
	assert.NotContains(t, sc.Unmodeled, "openapi:$dynamicRef",
		"an expanded reference is the position's type, so it is not also kept")
}

// TestDynamicRef_ChainEndsAtAnAnchorItCannotFollow pins where the chain walk
// stops. A hop landing on an anchor that is not a component schema, or on a name
// declared more than once, cannot be followed any further — and a chain it
// cannot follow is not a chain that returns, so the expansion still stands.
func TestDynamicRef_ChainEndsAtAnAnchorItCannotFollow(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"the hop lands off the components section": "    Mid: {$dynamicAnchor: mid, $dynamicRef: '#deep'}\n" +
			"    Holder: {type: object, properties: {i: {$dynamicAnchor: deep, type: string}}}\n",
		"the hop lands on a name declared twice": "    Mid: {$dynamicAnchor: mid, $dynamicRef: '#dup'}\n" +
			"    D1: {$dynamicAnchor: dup, type: string}\n    D2: {$dynamicAnchor: dup, type: integer}\n",
	}
	for name, schemas := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(schemas+"    A: {$dynamicRef: '#mid'}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
			require.True(t, ok)
			require.NotNil(t, sc.Base)
			assert.Equal(t, componentID("Mid"), sc.Base.Target)
		})
	}
}

// TestDialectKeywords_KeptOutOfScope pins the exclusion §4.7 records: the IR has
// no resource or dialect axis, so these are kept verbatim under the reason that
// says no IR node is coming for them — never dropped in silence, and never read
// as a base URI for reference resolution.
//
// The keyword list is written out here rather than read from
// annotation.DialectKeywords: ranging over the production slice would let a
// keyword dropped from it narrow this test instead of failing it, so the two
// are compared as sets and each is then exercised.
func TestDialectKeywords_KeptOutOfScope(t *testing.T) {
	t.Parallel()
	want := []string{"$id", "$schema", "$vocabulary"}
	require.ElementsMatch(t, want, annotation.DialectKeywords,
		"a keyword joining or leaving the exclusion must be decided here too")

	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A:\n"+
			"      $id: 'urn:example:a'\n"+
			"      $schema: 'https://json-schema.org/draft/2020-12/schema'\n"+
			"      $vocabulary: {'https://json-schema.org/draft/2020-12/vocab/core': true}\n"+
			"      $comment: not for end users\n"+
			"      type: string\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	td, ok := doc.Types[componentID("A")]
	require.True(t, ok)
	for _, keyword := range want {
		entry, ok := td.Common().Unmodeled["openapi:"+keyword]
		require.True(t, ok, "%s must be kept verbatim", keyword)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
		openapitest.AssertInfoDiagAt(t, diags, entry.Provenance.Pointer)
	}
	assert.NotContains(t, td.Common().Unmodeled, "openapi:$comment",
		"2020-12 §8.3 forbids presenting $comment, so it is dropped rather than kept")
}

// assertDiagContains requires one diagnostic at pointer whose message carries
// substr, so a case asserts the reason it was given and not merely that it was
// reported.
func assertDiagContains(t *testing.T, diags []ir.Diagnostic, pointer, substr string) {
	t.Helper()
	for _, d := range diags {
		if d.Provenance.Pointer == pointer && strings.Contains(d.Message, substr) {
			return
		}
	}
	assert.Fail(t, "no diagnostic said why", "nothing at %q mentions %q; got %+v", pointer, substr, diags)
}

// TestDynamicRef_NonScalarValueIsKeptNotExpanded covers the one irreducible case
// the sibling table cannot: a $dynamicRef whose value is not a string is a type
// error the document validator also reports, so the case arrives with an error
// diagnostic beside it. The lowering still has to keep the construct rather than
// treat a malformed reference as absent.
func TestDynamicRef_NonScalarValueIsKeptNotExpanded(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a sequence": "    A: {$dynamicRef: [not, a, string]}\n",
		"a mapping":  "    A: {$dynamicRef: {also: not}}\n",
	}
	for name, schemas := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    M1: {$dynamicAnchor: ok, type: string}\n"+schemas))

			td, ok := doc.Types[componentID("A")]
			require.True(t, ok, "the position owns a node to keep the reference on")
			entry, ok := td.Common().Unmodeled["openapi:$dynamicRef"]
			require.True(t, ok, "a malformed $dynamicRef is kept verbatim, not dropped")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assertDiagContains(t, diags, entry.Provenance.Pointer, "not a reference string")
		})
	}
}

// TestAppendExample_ConvertsAndAppends pins the accumulator every example site
// shares. The example's value is the converted node and nothing else about the
// prototype changes, so a site that fills in a name or a description keeps it.
func TestAppendExample_ConvertsAndAppends(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, overlay.Origin{})
	proto := ir.Example{Name: "n", Summary: "s", Description: "d"}

	out, diags := schema.AppendExample(c, nil, proto, openapitest.StrNode("hello"), "/p", "examples", "n")

	assert.Empty(t, diags, "a convertible node is announced by nothing")
	require.Len(t, out, 1)
	assert.Equal(t, "n", out[0].Name, "the prototype's own fields survive")
	require.NotNil(t, out[0].Value)
	assert.Equal(t, "hello", out[0].Value.Str)
}

// TestAppendExample_UnconvertibleValueIsReported pins the other half: a node
// that cannot become a value adds nothing to the list, and the pointer it is
// reported at is built from the base and the segments — which is the only path
// that joins them, so a wrong join shows up nowhere else.
func TestAppendExample_UnconvertibleValueIsReported(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, overlay.Origin{})
	nan := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".nan"}

	out, diags := schema.AppendExample(c, nil, ir.Example{}, nan, "/p", "examples", "n")

	assert.Empty(t, out, "nothing is appended when the value does not convert")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
	assert.Equal(t, "/p/examples/n", diags[0].Provenance.Pointer)
}

// TestStampConstraintDiags_RelocatesEveryDiagnosticToTheReadingPointer pins what
// makes two reads of one sub-schema dedup rather than report twice: the
// diagnostics a constraint read produces are located at the position that read
// it, not at the position that declared it.
func TestStampConstraintDiags_RelocatesEveryDiagnosticToTheReadingPointer(t *testing.T) {
	t.Parallel()
	c := lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, overlay.Origin{})
	in := []ir.Diagnostic{
		{Code: diag.DegradedConstruct, Provenance: ir.Provenance{Pointer: "/elsewhere"}},
		{Code: diag.NumericPrecision, Provenance: ir.Provenance{Source: 9, Pointer: "/other"}},
	}

	got := schema.StampConstraintDiags(c, in, "/components/schemas/S")

	require.Len(t, got, 2)
	for _, d := range got {
		assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/components/schemas/S"}, d.Provenance,
			"every diagnostic is relocated, not just the first")
	}
	assert.Equal(t, diag.NumericPrecision, got[1].Code, "only the provenance changes")
}

// TestAllOf_FalseBranchClosesTheComposition pins what a `false` allOf branch
// does to the composed node. `false` admits nothing, so the composition admits
// nothing: the model closes and the branch is kept verbatim beside it, which is
// the same answer a bare `false` schema gets in its own right.
func TestAllOf_FalseBranchClosesTheComposition(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    Never:
      allOf:
        - false
        - type: object
          properties: {id: {type: string}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := typeByName(doc, "Never").(*ir.Model)
	require.True(t, ok, "the composition still lowers to a model")
	assert.Equal(t, ir.AdditionalClosed, m.Additional, "a branch matching nothing closes the composition")
	require.Len(t, m.Unmodeled, 1, "the branch is kept verbatim")
	assert.Equal(t, ir.RawValue("false"), m.Unmodeled["openapi:allOf/0"].Value,
		"keyed by the branch index, so sibling branches cannot overwrite one another")
	assert.True(t, openapitest.HasDiagAt(diags, diag.FalseSchema, ir.SeverityInfo), "and it is announced")
}

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

// unpreservableValue is a value that is legal YAML, decodes, and then cannot be
// marshalled as JSON: json.Marshal rejects NaN, so any preserved payload
// containing one fails whole. It is the shortest reachable trigger for the
// conversion failure the announcement tests need.
const unpreservableValue = ".nan"

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

// TestAllOf_UnpreservableBranchIsNotAnnounced is the same pairing on the
// composition path: a branch the merge does not consume whole is normally kept
// beside the model and reported, and when the keeping fails the report must not
// happen either.
func TestAllOf_UnpreservableBranchIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    M:
      allOf:
        - {type: string, maxLength: 3, x-t: `+unpreservableValue+`}
`))

	td := typeByName(doc, "M")
	require.NotNil(t, td)
	assert.NotContains(t, td.Common().Unmodeled, "openapi:allOf/0", "the branch did not convert")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct))
	assert.Empty(t, preservationClaims(diags),
		"no degradation is announced for a branch that was not kept")
}

// TestUnionSiblings_UnpreservableIsReportedNotClaimed pins the announcement
// pairing at the union-sibling site. A oneOf the lowering keeps beside a
// structural body is kept verbatim when it converts; when it does not, the
// failure is reported at the keyword and nothing claims it was kept. The
// structural sibling here is unconvertible too, so the case has no legitimately
// preserved construct that a claim could be about.
func TestUnionSiblings_UnpreservableIsReportedNotClaimed(t *testing.T) {
	t.Parallel()
	_, diags := lowerSpec(t, openapitest.ComponentSpec(`    S:
      items: {type: string, x-t: `+unpreservableValue+`}
      oneOf: [{type: string, x-t: `+unpreservableValue+`}, {type: integer}]
`))

	var at []string
	for _, d := range diags {
		if d.Code == diag.UnpreservableConstruct {
			at = append(at, d.Provenance.Pointer)
		}
	}
	assert.Contains(t, at, "/components/schemas/S/oneOf",
		"the union siblings are reported where they are written: %+v", diags)
	assert.Empty(t, preservationClaims(diags),
		"nothing converted, so nothing may say it was kept")
}

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

// TestResidueKeywords_HandsBackACopy pins the reason this is a function and not
// the slice. One list is the whole point — a keyword added to it has to reach
// every position that preserves one — so the list itself must not be reachable
// from outside. An exported slice would let any importer rewrite what every
// schema position in the process preserves, for the life of the process.
func TestResidueKeywords_HandsBackACopy(t *testing.T) {
	t.Parallel()
	first := schema.ResidueKeywords()
	require.NotEmpty(t, first, "an empty list would make this pass vacuously")

	clobbered := append([]string(nil), first...)
	for i := range first {
		first[i] = "clobbered"
	}

	assert.Equal(t, clobbered, schema.ResidueKeywords(),
		"writing through one caller's slice must not change what the next one reads")
}

// TestUnhomedApplicator_FormatWithNoTypeIsKept pins the one keyword the home
// question answers from the declaration rather than from the node kind.
//
// `format` is homed by a declared type: scalarTypeID pairs the two to select a
// primitive or hoist a Scalar carrying the encoding. Written with no type beside
// it there is no pairing to make, so nothing reads it — and it is kept verbatim
// rather than dropped, which is the whole of §4.8 for this keyword.
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
			assert.Equal(t, "/components/schemas/S/"+tc.skipped, entry.Provenance.Pointer,
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

// keywordCensusSpec wraps the two $ref targets every ref-site row aliases in a
// component block, so a row states only what it declares beside its $ref.
func keywordCensusSpec(schemas string) string {
	return openapitest.ComponentSpec("    BaseStr: {type: string}\n" +
		"    BaseObj: {type: object, properties: {a: {type: string}}}\n" + schemas)
}

// assertKeptVerbatim asserts p holds key as a degraded-lowering entry whose raw
// payload is raw.
func assertKeptVerbatim(t *testing.T, p ir.Unmodeled, key, raw string) {
	t.Helper()
	entry, ok := p[key]
	require.True(t, ok, "%q is kept verbatim; kept instead: %v", key, unmodeledKeys(p))
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, raw, string(entry.Value))
}

// unmodeledKeys returns p's keys in sorted order, which is what makes an
// expected key set assertable as a whole: a census that keeps too much fails the
// same test as one that keeps too little.
func unmodeledKeys(p ir.Unmodeled) []string {
	if len(p) == 0 {
		return nil
	}
	out := make([]string, 0, len(p))
	for key := range p {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// TestRefSiteKeywords_KeptAtEveryPosition covers the census the $ref path never
// ran (GitHub #283).
//
// In JSON Schema 2020-12 — and so in OpenAPI 3.1 — `$ref` is an ordinary keyword
// and what stands beside it is conjoined with it. The position lowers to an alias
// over the target, which has no property set, no member set, no value and no
// encoding of its own, so each of these keywords reached no IR field at all: no
// field, no Unmodeled entry and no diagnostic either.
//
// Every row is checked at both positions, because they take different paths: a
// component is an annotation.HomeOwnNode position and keeps the keyword on the
// alias it hoists, a property is an annotation.HomeCarrier one and keeps it on
// itself, and only the first of the two ran any census at all.
func TestRefSiteKeywords_KeptAtEveryPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ keyword, sibling, target, raw string }{
		{"format", "format: email", "BaseStr", `"email"`},
		{"enum", "enum: [a, b]", "BaseStr", `["a","b"]`},
		{"const", "const: a", "BaseStr", `"a"`},
		{"required", "required: [a]", "BaseObj", `["a"]`},
		{"additionalProperties", "additionalProperties: false", "BaseObj", `false`},
	} {
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			site := "{$ref: '#/components/schemas/" + tc.target + "', " + tc.sibling + "}"
			doc, diags := lowerSpec(t, keywordCensusSpec(
				"    Alias: "+site+"\n"+
					"    Holder: {type: object, properties: {p: "+site+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			key := "openapi:" + tc.keyword

			alias, ok := typeByName(doc, "Alias").(*ir.Scalar)
			require.True(t, ok, "the component position hoists an alias to hold what it wrote")
			require.NotNil(t, alias.Base)
			assert.Equal(t, componentID(tc.target), alias.Base.Target, "and still aliases the target")
			assertKeptVerbatim(t, alias.Unmodeled, key, tc.raw)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/Alias"),
				"no home for "+tc.keyword)

			holder, ok := typeByName(doc, "Holder").(*ir.Model)
			require.True(t, ok)
			p, ok := openapitest.PropsByWire(holder.Properties)["p"]
			require.True(t, ok)
			assert.Equal(t, componentID(tc.target), p.Type.Target,
				"the carrier still resolves straight to the target; only the loss is now recorded")
			assertKeptVerbatim(t, p.Unmodeled, key, tc.raw)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
					"/components/schemas/Holder/properties/p"),
				"no home for "+tc.keyword)
		})
	}
}

// TestRefSiteKeywords_SiblingsWithATypedHomeAreUntouched is the control the
// census has to leave alone. A bound and a description written at the identical
// position always did reach a field — the alias's Constraints and the property's
// own — so neither may acquire an Unmodeled entry or a diagnostic now.
func TestRefSiteKeywords_SiblingsWithATypedHomeAreUntouched(t *testing.T) {
	t.Parallel()
	site := "{$ref: '#/components/schemas/BaseStr', minLength: 3, description: kept}"
	doc, diags := lowerSpec(t, keywordCensusSpec(
		"    Alias: "+site+"\n"+
			"    Holder: {type: object, properties: {p: "+site+"}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"nothing was degraded: %+v", diags)

	alias, ok := typeByName(doc, "Alias").(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, alias.Constraints)
	assert.Equal(t, int64(3), *alias.Constraints.MinLength)
	assert.Equal(t, "kept", alias.Docs.Description)
	assert.Empty(t, unmodeledKeys(alias.Unmodeled))

	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	p, ok := openapitest.PropsByWire(holder.Properties)["p"]
	require.True(t, ok)
	require.NotNil(t, p.Constraints)
	assert.Equal(t, int64(3), *p.Constraints.MinLength)
	assert.Equal(t, "kept", p.Docs.Description)
	assert.Empty(t, unmodeledKeys(p.Unmodeled))
}

// TestUnhomedKeywords_ElectedLoweringKeepsWhatItCannotRead covers the keywords
// the winning lowering never reads (GitHub #268).
//
// lower() elects one keyword family per position; what the elected form has no
// field for was dropped, because the census that ran was a fixed list of shape
// applicators. A Model has no type token, a Literal has no encoding and no
// Constraints, and none of that is visible from a keyword list — only from the
// node that was built, which is what the census asks now.
//
// The kept set is asserted whole, so a census that keeps too much fails here as
// loudly as one that keeps too little; the `type: object` row is the case that
// makes that matter, since a Model does restate it and nothing may be recorded.
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
		// A collection bound is homed by ir.List.Constraints and by nothing else.
		// listConstraints is its only reader and only lowerArray calls it, so an
		// object keeps it here; so does a Tuple, which has no Constraints field at
		// all. Both reached the IR in no form before they joined the census.
		{"collection bound on an object", "{type: object, properties: {f: {type: string}}, minItems: 3}",
			ir.KindModel, []string{"openapi:minItems"}},
		{"collection bound beside prefixItems", "{type: array, prefixItems: [{type: string}], maxItems: 2}",
			ir.KindTuple, []string{"openapi:maxItems"}},
		// A Scalar rather than the shared Primitive the type alone would reach:
		// the entry needs a node this pointer owns, so the census hoists the alias
		// that carries it.
		{"unique items on a string", "{type: string, uniqueItems: true}",
			ir.KindScalar, []string{"openapi:uniqueItems"}},
		// The control: the one node that does carry them keeps nothing.
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
			assert.Equal(t, want, len(diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/S")),
				"a position keeps nothing quietly and reports everything it keeps: %+v", diags)
		})
	}
}

// TestCoDeclaredBound_KeptOnTheCarrierThatReadIt pins the two carriers this
// package owns for a 2020-12 side that declares both of its bound keywords
// (GitHub #286). ir.Constraints holds one bound per side, so one keyword reaches
// no field of it, and without an entry beside those constraints
// {minimum: 10, exclusiveMinimum: 0} lowers to exactly what {minimum: 10} does.
//
// Both directions run at both carriers. A case where the exclusive keyword is
// the one kept verbatim passes just as well on a reader that always kept that
// one, so on its own it would say nothing about which keyword the carrier holds.
func TestCoDeclaredBound_KeptOnTheCarrierThatReadIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Alias: {type: integer, minimum: 10, exclusiveMinimum: 0}\n"+
			"    Tight: {type: integer, maximum: 100, exclusiveMaximum: 5}\n"+
			"    Holder:\n      type: object\n      properties:\n"+
			"        low: {type: integer, minimum: 10, exclusiveMinimum: 0}\n"+
			"        high: {type: integer, maximum: 100, exclusiveMaximum: 5}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	tests := []struct {
		name     string
		unmod    ir.Unmodeled
		bound    *ir.Constraints
		wantKept string
		wantRaw  string
		at       string
	}{
		{
			name:     "alias node keeps the exclusive bound the minimum implies",
			unmod:    typeByName(doc, "Alias").Common().Unmodeled,
			bound:    typeByName(doc, "Alias").(*ir.Scalar).Constraints,
			wantKept: "openapi:exclusiveMinimum", wantRaw: "0",
			at: "/components/schemas/Alias/exclusiveMinimum",
		},
		{
			name:     "alias node keeps the inclusive bound the exclusive one implies",
			unmod:    typeByName(doc, "Tight").Common().Unmodeled,
			bound:    typeByName(doc, "Tight").(*ir.Scalar).Constraints,
			wantKept: "openapi:maximum", wantRaw: "100",
			at: "/components/schemas/Tight/maximum",
		},
		{
			name:     "property keeps the exclusive bound the minimum implies",
			unmod:    propertyOf(t, doc, "Holder", "low").Unmodeled,
			bound:    propertyOf(t, doc, "Holder", "low").Constraints,
			wantKept: "openapi:exclusiveMinimum", wantRaw: "0",
			at: "/components/schemas/Holder/properties/low/exclusiveMinimum",
		},
		{
			name:     "property keeps the inclusive bound the exclusive one implies",
			unmod:    propertyOf(t, doc, "Holder", "high").Unmodeled,
			bound:    propertyOf(t, doc, "Holder", "high").Constraints,
			wantKept: "openapi:maximum", wantRaw: "100",
			at: "/components/schemas/Holder/properties/high/maximum",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.NotNil(t, tc.bound, "the tighter bound still reaches ir.Constraints")
			entry, ok := tc.unmod[tc.wantKept]
			require.True(t, ok, "%s is kept beside the constraints it did not reach; got %v",
				tc.wantKept, tc.unmod)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.JSONEq(t, tc.wantRaw, string(entry.Value))
			assert.Equal(t, tc.at, entry.Provenance.Pointer)
		})
	}
}

// diagsAtPointer returns the diagnostics in diags with the exact code whose
// provenance is pointer.
func diagsAtPointer(diags []ir.Diagnostic, code, pointer string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == code && d.Provenance.Pointer == pointer {
			out = append(out, d)
		}
	}
	return out
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

// TestUnhomedKeywords_ArrayBoundsKeepWhatListConstraintsDoesNotRead pins the
// reason the census asks what filled a Constraints field rather than which kinds
// have one. An ir.List has the field, but lowerArray fills it from
// listConstraints — collection bounds only — so a string bound written on an
// array reaches nothing however full the field looks.
func TestUnhomedKeywords_ArrayBoundsKeepWhatListConstraintsDoesNotRead(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, keywordCensusSpec(
		"    S: {type: array, items: {type: string}, minItems: 1, minLength: 3}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	l, ok := typeByName(doc, "S").(*ir.List)
	require.True(t, ok)
	require.NotNil(t, l.Constraints)
	assert.Equal(t, int64(1), *l.Constraints.MinItems, "the collection bound lowers as it always did")
	assert.Equal(t, []string{"openapi:minLength"}, unmodeledKeys(l.Unmodeled),
		"and only the bound listConstraints does not read is kept")
}

// TestRefSiteKeywords_AllOfBranchKeepsWhatTheAliasCannotHold runs the same
// census at the third $ref site: an allOf branch spelled as a reference, which
// homes its siblings on an alias exactly as a component does.
//
// `required` is the branch keyword this composition does read —
// applyCompositionRequired ORs every branch's list onto the composed model — so
// it must not be kept, or one keyword would be reported twice.
func TestRefSiteKeywords_AllOfBranchKeepsWhatTheAliasCannotHold(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, keywordCensusSpec(
		"    S: {allOf: [{$ref: '#/components/schemas/BaseObj', format: email, required: [a]}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, m.Base, "the branch still composes")
	branch, ok := doc.Types[m.Base.Target].(*ir.Scalar)
	require.True(t, ok, "through an alias hoisted at the branch position")
	assert.Equal(t, []string{"openapi:format"}, unmodeledKeys(branch.Unmodeled),
		"required is read by the composition, so only the format is kept")
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0"),
		"no home for format")
}

// TestCoDeclaredBound_ASingleKeywordKeepsNothing is the other half of the case
// above: a side writing one keyword has it in a field, so an entry restating it
// would give one bound two homes and make the two source shapes indistinguishable
// in the opposite direction.
func TestCoDeclaredBound_ASingleKeywordKeepsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Alias: {type: integer, minimum: 10}\n"+
			"    Holder: {type: object, properties: {low: {type: integer, exclusiveMinimum: 0}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Empty(t, typeByName(doc, "Alias").Common().Unmodeled)
	assert.Empty(t, propertyOf(t, doc, "Holder", "low").Unmodeled)
}
