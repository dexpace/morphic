package openapi

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	js := l.doc.Components.GetSchemas().GetOrZero("S")
	require.NotNil(t, js)

	st := l.siteAt(js, ptr("components", "schemas", "S"))
	assert.Equal(t, siteDeclaration, st.Kind)
	require.NotNil(t, st.Node)
	assert.Equal(t, "d", st.Node.GetDescription())
	assert.Nil(t, st.Referent, "a declaration site has no referent")
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
	js := l.doc.Components.GetSchemas().GetOrZero("S")
	require.NotNil(t, js)

	st := l.siteAt(js, ptr("components", "schemas", "S"))
	assert.Equal(t, siteReference, st.Kind)
	require.NotNil(t, st.Node)
	assert.Equal(t, "site-desc", st.Node.GetDescription(), "Node is the schema written here")
	require.NotNil(t, st.Referent)
	assert.Equal(t, "target-desc", st.Referent.GetDescription(), "Referent is one hop away")
}

// TestSiteAt_EmptyRefIsDeclaration pins the narrower classification: a $ref
// pointer present but empty never resolves (IsReference is false for it), so
// siteAt reports a declaration, not a reference with a nil Referent.
func TestSiteAt_EmptyRefIsDeclaration(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	emptyRef := references.Reference("")
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: &emptyRef})

	s := l.siteAt(js, "/p")
	assert.Equal(t, siteDeclaration, s.Kind, "an empty $ref resolves nowhere, so it is not a reference site")
	assert.Nil(t, s.Referent)
}

// TestSiteAt_EmptyPointerPanics covers siteAt's own precondition: pointer is
// always caller-derived and never empty in practice (every call site already
// guarantees it), so an empty pointer here is a programmer error, not a spec
// problem, and siteAt panics rather than build a site for nowhere.
func TestSiteAt_EmptyPointerPanics(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Panics(t, func() { l.siteAt(oas3.NewJSONSchemaFromBool(true), "") })
}

// TestSiteAt_NonPointerPanics covers siteAt's second precondition: pointer
// must be RFC 6901-shaped (starting with "/"), the assumption every pointer-
// keyed lookup in this file relies on.
func TestSiteAt_NonPointerPanics(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Panics(t, func() { l.siteAt(oas3.NewJSONSchemaFromBool(true), "components/schemas/S") })
}

// TestSiteKind_String covers both named values and the default case, so an
// assertion failure or test diff over a siteKind prints a name instead of a
// bare int.
func TestSiteKind_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "siteDeclaration", siteDeclaration.String())
	assert.Equal(t, "siteReference", siteReference.String())
	assert.Equal(t, "siteKind(99)", siteKind(99).String())
}

// TestLowerComponentSchema_RefSiblingConstraintBindsTheSite locks in the
// $ref-sibling-constraint behaviour at a named component: lowerComponentSchema
// resolves the component's own site via siteAt, so a bound written beside the
// $ref (S: {$ref: Target, minimum: 5}) binds S, not Target.
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
	l.lowerComponentSchemas()

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

// TestFillPropertyExamples_RefSiblingExampleBindsTheSite is a characterization
// test: it locks in the existing $ref-sibling-example behaviour at a property
// position before fillPropertyExamples is rewired through site, so a
// behaviour change during that rewiring shows up as a failure here rather
// than passing unnoticed. Property f's $ref targets a named component
// (Target), so resolveComponentRef resolves it directly without ever calling
// hoistSubSchema; fillPropertyExamples is what keeps the example off the
// referent here. TestSchemaSiblings_RefdSubSchemaKeepsThem covers the
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
	l.lowerComponentSchemas()

	target := typeByName(l.out, "Target")
	require.NotNil(t, target)
	assert.Empty(t, target.Common().Examples,
		"an example beside a $ref must not attach to the referent")
}
