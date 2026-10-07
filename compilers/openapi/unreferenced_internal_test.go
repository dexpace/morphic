package openapi

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestUnreferencedComponents_RetentionIsOrderIndependent is the hand-rolled
// two-order diff for GitHub #616. Document.Unmodeled is neither the type registry
// nor a diagnostic, so the corpus's order-invariance oracle does not see it: the
// same entries declared before and after the paths that reference one of them
// must be kept in the same set, located at the same pointers.
func TestUnreferencedComponents_RetentionIsOrderIndependent(t *testing.T) {
	t.Parallel()
	const paths = `paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/Used'}
`
	const components = `components:
  responses:
    Used: {description: reached}
    Unused: {description: not reached}
`
	first, _ := parseFull(t, "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\n"+components+paths)
	second, _ := parseFull(t, "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\n"+paths+components)
	assert.Empty(t, cmp.Diff(first.Unmodeled, second.Unmodeled),
		"what is kept must not depend on which of the two the document writes first")
	require.Contains(t, first.Unmodeled, "openapi:components/responses/Unused")
}

// TestUnreferencedComponents_ChainKeepsBothEnds pins the transitive rule's other
// half at the IR: an unreferenced entry's own `$ref` is not a root, so the entry
// it names is kept too rather than being lowered on behalf of a component that
// vanishes.
func TestUnreferencedComponents_ChainKeepsBothEnds(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  responses:
    R:
      description: nothing reaches this
      headers: {X: {$ref: '#/components/parameters/P', schema: {type: string}}}
  parameters:
    P: {name: p, in: query, schema: {type: string}}
`)
	require.Contains(t, doc.Unmodeled, "openapi:components/responses/R")
	require.Contains(t, doc.Unmodeled, "openapi:components/parameters/P",
		"the target of an unreachable entry's own reference is kept too")
	assert.Equal(t, ir.ReasonNoIRHome, doc.Unmodeled["openapi:components/parameters/P"].Reason)
}

// TestUnreferencedComponents_ReachedEntryExpandsItsOwnReferences is the same
// shape with a root: the paths name the response, so its own reference reaches
// the parameter and neither is kept.
func TestUnreferencedComponents_ReachedEntryExpandsItsOwnReferences(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/R'}
components:
  responses:
    R:
      description: reached
      headers: {X: {$ref: '#/components/parameters/P', schema: {type: string}}}
  parameters:
    P: {name: p, in: query, schema: {type: string}}
`)
	assert.NotContains(t, doc.Unmodeled, "openapi:components/responses/R")
	assert.NotContains(t, doc.Unmodeled, "openapi:components/parameters/P",
		"a reached entry's own reference is a root in turn")
}

// TestUnreferencedComponents_MediaTypesIsPerEntryOnlyFrom32 pins the one version
// gate in the retained set: components/mediaTypes is a section the dialect
// defines from 3.2, so below it the key is undefined and the components census
// keeps the whole map under its own key instead of one entry per name.
func TestUnreferencedComponents_MediaTypesIsPerEntryOnlyFrom32(t *testing.T) {
	t.Parallel()
	const body = `paths: {}
components:
  mediaTypes:
    Event: {schema: {type: string}}
`
	doc32, _ := parseFull(t, "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\n"+body)
	require.Contains(t, doc32.Unmodeled, "openapi:components/mediaTypes/Event")
	assert.NotContains(t, doc32.Unmodeled, "openapi:components/mediaTypes",
		"3.2 handles the section per entry, so the whole map is not also kept")

	doc31, _ := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"+body)
	assert.Contains(t, doc31.Unmodeled, "openapi:components/mediaTypes",
		"below 3.2 the section is an undefined key, kept whole and reported")
	assert.NotContains(t, doc31.Unmodeled, "openapi:components/mediaTypes/Event")
}
