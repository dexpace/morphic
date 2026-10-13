package openapi

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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

// TestComponentMediaTypes_KeptWholeAndNamedFor32 pins the components/mediaTypes
// section while this compiler resolves nothing into it (GitHub #856): on 3.2,
// which defines it, the map is kept whole with an info rather than called
// undefined; below 3.2 the census keeps it and warns, as for any undefined key.
func TestComponentMediaTypes_KeptWholeAndNamedFor32(t *testing.T) {
	t.Parallel()
	const body = `paths: {}
components:
  mediaTypes:
    Event: {schema: {type: string}}
`
	for _, tc := range []struct {
		version string
		reason  ir.UnmodeledReason
		code    string
		sev     ir.Severity
	}{
		{"3.2.0", ir.ReasonNoIRHome, diag.DegradedConstruct, ir.SeverityInfo},
		{"3.1.0", ir.ReasonOutOfScope, diag.UnknownObjectKey, ir.SeverityWarning},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, "openapi: "+tc.version+"\ninfo: {title: T, version: \"1\"}\n"+body)
			kept, ok := doc.Unmodeled["openapi:components/mediaTypes"]
			require.True(t, ok, "the section is kept whole")
			assert.Equal(t, tc.reason, kept.Reason)
			assert.NotEmpty(t, openapitest.DiagMessageAt(t, diags, tc.code, tc.sev, "/components/mediaTypes"))
		})
	}
}
