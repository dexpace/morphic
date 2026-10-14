package operation

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestFirstDeclared_KeepsEachKeysFirstDeclaration pins the index precedence
// reads a farther layer through: one entry per key, the first declaration of
// it, which is the one a collision finding names.
func TestFirstDeclared_KeepsEachKeysFirstDeclaration(t *testing.T) {
	t.Parallel()
	got := firstDeclared([]itemConstruct{
		{key: "k", rel: "/first"},
		{key: "other", rel: "/other"},
		{key: "k", rel: "/second"},
	})

	assert.Len(t, got, 2)
	assert.Equal(t, jsontext.Pointer("/first"), got["k"].rel, "a repeated key keeps its first declaration")
	assert.Equal(t, jsontext.Pointer("/other"), got["other"].rel)
	assert.Empty(t, firstDeclared(nil))
}

// TestPathItemLayers_NamesWhereTheUseSitesOwnUnreadFieldsAre pins the use
// site's own place in the report of fields no model holds: read in another
// document's scope they are that document's, and read in the source's they are
// fields its walk did not reach. A compile rarely gets here, since a $ref
// inside another document's content is never resolved (GitHub #751) and a
// source use site goes unmodelled only when a walk panic stops the search, so
// the context is given an empty load result in place of the load phase's
// models.
func TestPathItemLayers_NamesWhereTheUseSitesOwnUnreadFieldsAre(t *testing.T) {
	t.Parallel()
	l, diags := loweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put: {responses: {"200": {description: OK}}}
  /b:
    get: {responses: {"200": {description: OK}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	rp, ok := l.ctx.Doc.GetPaths().Get("/a")
	require.True(t, ok)
	usePtr := jsontext.Pointer("/paths/~1a")
	pi, declPtr := resolve.ObjectAt[soa.PathItem](l.ctx.RefScope(), rp, usePtr)
	require.NotNil(t, pi)

	for foreign, want := range map[bool]string{false: "does not reach", true: "in another document"} {
		use := l.ctx.WithLoaded(&load.Document{})
		if foreign {
			scope := use.RefScope()
			scope.Foreign, scope.Holder = true, "other.yaml"
			use = use.InScope(scope)
		}

		_, found := pathItemLayers(use, rp, pi, usePtr, declPtr)

		require.Len(t, found, 1, "foreign %t: the use site's fields, and no hop's", foreign)
		assert.Equal(t, diag.DegradedConstruct, found[0].Code)
		assert.Contains(t, found[0].Message, want, "foreign %t", foreign)
	}
}

// TestPathItemLayers_AOneLayerMountCostsItsLayerAlone pins what a mount with
// nothing beside a $ref costs, which is nearly every mount: the context
// lowering.Within gives its item, and the one-entry slice. A scope that Hops or
// a sibling layer keeps takes the context's closures with it, so building one
// for every mount cost each that much more. The item is inline, since Within's
// walk through a $ref allocates a varying count under the race detector.
func TestPathItemLayers_AOneLayerMountCostsItsLayerAlone(t *testing.T) {
	l, diags := loweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get: {responses: {"200": {description: OK}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	rp, ok := l.ctx.Doc.GetPaths().Get("/b")
	require.True(t, ok)
	const usePtr = jsontext.Pointer("/paths/~1b")
	pi, declPtr := resolve.ObjectAt[soa.PathItem](l.ctx.RefScope(), rp, usePtr)
	require.NotNil(t, pi)

	within := testing.AllocsPerRun(20, func() { lowering.Within[soa.PathItem](l.ctx, rp).Siblings() })
	layers := testing.AllocsPerRun(20, func() { pathItemLayers(l.ctx, rp, pi, usePtr, declPtr) })
	assert.Equal(t, within+1, layers, "Within's work and the one layer")
}
