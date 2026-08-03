package openapi

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestSiteAt_DeclarationHasNoReferent(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, description: d}
`)
	js := l.ctx.Doc.Components.GetSchemas().GetOrZero("S")
	require.NotNil(t, js)

	s := annotation.At(js)
	assert.Equal(t, annotation.Declaration, s.Kind)
	require.NotNil(t, s.Node)
	assert.Equal(t, "d", s.Node.GetDescription())
	assert.Nil(t, s.Referent, "a declaration site has no referent")
}

func TestSiteAt_ReferenceCarriesBothNodes(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string, description: target-desc}
    S:
      $ref: '#/components/schemas/Target'
      description: site-desc
`)
	js := l.ctx.Doc.Components.GetSchemas().GetOrZero("S")
	require.NotNil(t, js)

	s := annotation.At(js)
	assert.Equal(t, annotation.Reference, s.Kind)
	require.NotNil(t, s.Node)
	assert.Equal(t, "site-desc", s.Node.GetDescription(), "Node is the schema written here")
	require.NotNil(t, s.Referent)
	assert.Equal(t, "target-desc", s.Referent.GetDescription(), "Referent is one hop away")
}

// TestSiteAt_EmptyRefIsDeclaration pins the narrower classification: a $ref
// pointer present but empty never resolves (IsReference is false for it), so
// annotation.At reports a declaration, not a reference with a nil Referent.
func TestSiteAt_EmptyRefIsDeclaration(t *testing.T) {
	t.Parallel()
	emptyRef := references.Reference("")
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: &emptyRef})

	s := annotation.At(js)
	assert.Equal(t, annotation.Declaration, s.Kind, "an empty $ref resolves nowhere, so it is not a reference site")
	assert.Nil(t, s.Referent)
}

// TestLowerComponentSchema_RefSiblingConstraintBindsTheSite locks in the
// $ref-sibling-constraint behaviour at a named component: lowerComponentSchema
// resolves the component's own site via annotation.At, so a bound written
// beside the $ref (S: {$ref: Target, minimum: 5}) binds S, not Target.
func TestLowerComponentSchema_RefSiblingConstraintBindsTheSite(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: integer}
    S:
      $ref: '#/components/schemas/Target'
      minimum: 5
`)
	l.diags.AppendAll(schema.LowerComponentSchemas(l.ctx, l.types, &l.anchors))

	sc, ok := typeByName(l.out, "S").(*ir.Scalar)
	require.True(t, ok, "S aliases Target and must own a Scalar node")
	require.NotNil(t, sc.Constraints, "a bound beside a $ref binds S, not Target")
	require.NotNil(t, sc.Constraints.Min)
	assert.Equal(t, "5", sc.Constraints.Min.String())

	target := typeByName(l.out, "Target")
	require.NotNil(t, target)
	tsc, ok := target.(*ir.Scalar)
	require.True(t, ok, "Target aliases a primitive and must own a Scalar node")
	assert.Nil(t, tsc.Constraints, "the referent must not acquire the site's bound")
}

// TestFillPropertyExamples_RefSiblingExampleBindsTheSite locks in the
// $ref-sibling-example behaviour at a property position: an example written
// beside a $ref binds the property, not the referent, because
// fillPropertyExamples resolves it directly rather than through site.
// Property f's $ref targets a named component (Target), so
// resolve.Scope.ComponentRef resolves it directly without ever calling
// hoistSubSchema. TestSchemaSiblings_RefdSubSchemaKeepsThem covers the
// analogous hoistSubSchema path, where the $ref target is an internal
// sub-schema pointer rather than a named component.
func TestFillPropertyExamples_RefSiblingExampleBindsTheSite(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    Holder:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          example: at-reference
`)
	l.diags.AppendAll(schema.LowerComponentSchemas(l.ctx, l.types, &l.anchors))

	target := typeByName(l.out, "Target")
	require.NotNil(t, target)
	assert.Empty(t, target.Common().Examples,
		"an example beside a $ref must not attach to the referent")

	holder, ok := typeByName(l.out, "Holder").(*ir.Model)
	require.True(t, ok, "Holder must own a Model node")
	f, ok := propsByWire(holder.Properties)["f"]
	require.True(t, ok, "property f must be present")
	require.Len(t, f.Examples, 1, "the example beside the $ref must bind the property")
	require.NotNil(t, f.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "at-reference"}, *f.Examples[0].Value)
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
			doc, diags := lowerSpec(t, componentSpec(
				"    "+tc.decl+": {type: string}\n"+
					"    User: {type: object, properties: {x: {$ref: '#/components/schemas/"+tc.encoded+"'}}}\n"))
			requireNoErrorDiags(t, diags)

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
// *through* an encoded component name addresses a sub-schema an unencoded pointer
// also addresses, so the two must intern one node. Reading the fragment raw
// hoisted a second one at `.../Foo%2DBar/properties/inner` — a path no source
// coordinate spells, the derivation GitHub #141 refused for anchors — so one
// position became two types, silently: both references resolved, no diagnostic
// was emitted, and the duplicate is a node irverify has no reason to call
// dangling.
//
// Both spellings appear here because that is what makes the duplicate observable
// at all; the encoded ref alone lands on one node either way, and only its name
// is wrong.
func TestLowerComponentSchemas_PercentEncodedRefHoistsAtTheDeclaredCoordinate(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, componentSpec(
		"    Foo-Bar: {type: object, properties: {inner: {type: object, properties: {n: {type: integer}}}}}\n"+
			"    User:\n      type: object\n      properties:\n"+
			"        a: {$ref: '#/components/schemas/Foo-Bar/properties/inner'}\n"+
			"        b: {$ref: '#/components/schemas/Foo%2DBar/properties/inner'}\n"))
	requireNoErrorDiags(t, diags)

	user, ok := typeByName(doc, "User").(*ir.Model)
	require.True(t, ok, "User must own a Model node")
	props := propsByWire(user.Properties)
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

// TestLowerService_PercentEncodedEntryRefKeepsTheDeclaredCoordinate covers the
// same identity defect on the components that are not schemas, where it was
// wholly silent. Their entries resolve through the library, so the value arrived
// intact and no unresolved-ref was ever emitted; only the pointer the entry
// carried stayed encoded, and every ID hoisted beneath it inherited the
// encoding — here the response's own content schema.
func TestLowerService_PercentEncodedEntryRefKeepsTheDeclaredCoordinate(t *testing.T) {
	t.Parallel()
	doc, _, diags := lowerServiceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        '200': {$ref: '#/components/responses/My%2DResp'}
components:
  responses:
    My-Resp:
      description: ok
      content:
        application/json:
          schema: {type: object, properties: {q: {type: string}}}
`)
	requireNoErrorDiags(t, diags)

	const want = ir.TypeID("t/anon/components/responses/My-Resp/content/application~1json/schema")
	assert.Contains(t, doc.Types, want,
		"the response body is hoisted at the coordinate the component declares")
	for id := range doc.Types {
		assert.NotContains(t, string(id), "%",
			"no node is interned at an encoded path")
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
	doc, diags := lowerSpec(t, componentSpec(
		"    Cat-A: {type: object, properties: {kind: {type: string}}}\n"+
			"    Pet:\n      oneOf: [{$ref: '#/components/schemas/Cat-A'}]\n"+
			"      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat%2DA'}}\n"))
	requireNoErrorDiags(t, diags)

	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok, "Pet must own a Union node")
	require.NotNil(t, pet.Discriminator, "the discriminator survives lowering")
	assert.Equal(t, map[string]ir.TypeID{"cat": componentID("Cat-A")}, pet.Discriminator.Mapping,
		"the encoded mapping target names the declared component, and the entry is kept")
}
