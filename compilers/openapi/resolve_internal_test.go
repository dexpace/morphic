package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
