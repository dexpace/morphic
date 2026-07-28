package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestSiteAt_DeclarationHasNoReferent(t *testing.T) {
	t.Parallel()
	l := loweredFor(t, `openapi: 3.1.0
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
	l := loweredFor(t, `openapi: 3.1.0
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

// TestLowerComponentSchema_RefSiblingConstraintBindsTheSite is a characterization
// test: it locks in the existing $ref-sibling-constraint behaviour before
// lowerComponentSchema is rewired through site, so a behaviour change during
// that rewiring shows up as a failure here rather than passing unnoticed.
func TestLowerComponentSchema_RefSiblingConstraintBindsTheSite(t *testing.T) {
	t.Parallel()
	l := loweredFor(t, `openapi: 3.1.0
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
	if tsc, ok := target.(*ir.Scalar); ok {
		assert.Nil(t, tsc.Constraints, "the referent must not acquire the site's bound")
	}
}
