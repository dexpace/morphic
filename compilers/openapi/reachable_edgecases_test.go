package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestAuth_OAuthNoFlowsUnknownTypeAndGhostRef(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get: {operationId: x, responses: {"200": {description: ok}}}
components:
  securitySchemes:
    oauthNoFlows: {type: oauth2}
    weird: {type: bananas}
    ghost: {$ref: '#/components/securitySchemes/Missing'}
`
	doc, _, _ := lowerServiceSpec(t, spec)
	var oauth, custom ir.AuthScheme
	for _, s := range doc.Auth {
		if s.Kind == ir.AuthKindOAuth2 {
			oauth = s
		}
		if s.Kind == ir.AuthKindCustom {
			custom = s
		}
	}
	assert.Equal(t, ir.AuthKindOAuth2, oauth.Kind)
	assert.Nil(t, oauth.Flows, "oauth2 with no flows lowers to nil flows")
	assert.Equal(t, "bananas", custom.Scheme, "unknown scheme type degrades to custom")
}

func TestAllOf_ModelWithOwnDiscriminator(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Base:
      allOf:
        - {$ref: '#/components/schemas/Common'}
      discriminator: {propertyName: kind}
      properties: {kind: {type: string}}
    Common: {type: object, properties: {id: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	base := typeByName(doc, "Base").(*ir.Model)
	require.NotNil(t, base.Discriminator, "allOf model may declare its own discriminator")
	assert.NotEmpty(t, base.Discriminator.Property)
}

func TestAllOf_BoolRefBranchHasNoDiscriminator(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    BoolComp: false
    Sub:
      allOf:
        - {$ref: '#/components/schemas/BoolComp'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	assert.Empty(t, sub.DiscriminatorValue, "a bool-schema ref target anchors no hierarchy")
	_ = diags
}

func TestEnum_NonScalarAndMidListMismatch(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    ObjEnum:
      enum:
        - {a: 1}
        - {b: 2}
    MidMismatch:
      enum: [alpha, beta, 3]
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	_, objIsUnion := typeByName(doc, "ObjEnum").(*ir.Union)
	assert.True(t, objIsUnion, "object-valued enum degrades to a union")
	_, midIsUnion := typeByName(doc, "MidMismatch").(*ir.Union)
	assert.True(t, midIsUnion, "kind change mid-list degrades to a union")
	_ = diags
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
	var sawWarn bool
	for _, d := range diags {
		if d.Code == codeDegradedConstruct && d.Severity == ir.SeverityWarning {
			sawWarn = true
		}
	}
	assert.True(t, sawWarn, "unserializable extension warns")
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
	var sawUnresolved bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawUnresolved = true
		}
	}
	assert.True(t, sawUnresolved, "the '#' ref form is unresolved")
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
	var count int
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			count++
		}
	}
	assert.GreaterOrEqual(t, count, 2, "both empty refs are unresolved")
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.Contains(t, []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}, "variant_0")
}

func TestParams_QueryStringLocation(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /q:
    get:
      operationId: q
      parameters:
        - {name: qs, in: querystring, schema: {type: string}}
      responses:
        "200": {description: ok}
`
	doc, _ := parseFull(t, spec)
	op := findOp(t, doc, "q")
	assert.Equal(t, ir.HTTPLocationQuerystring, op.Bindings.HTTP[0].ParamBindings[0].Location)
}
