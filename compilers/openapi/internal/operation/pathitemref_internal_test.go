// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
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

// TestPathItemLayers_AOneLayerMountCostsItsLayerAlone pins what a mount with
// nothing beside a $ref costs, which is nearly every mount: resolving its item
// and the one-entry slice. A scope that a sibling layer keeps takes its closure
// over a copy of the context with it, so building one for every mount cost
// each that much more.
func TestPathItemLayers_AOneLayerMountCostsItsLayerAlone(t *testing.T) {
	lowerer, diags := loweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get: {responses: {"200": {description: OK}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	rp, ok := lowerer.ctx.Doc.GetPaths().Get("/b")
	require.True(t, ok)
	const usePtr = jsontext.Pointer("/paths/~1b")

	within := testing.AllocsPerRun(20, func() {
		componentlookup.ObjectAt[soa.PathItem](lowerer.ctx.RefScope(), rp, reftable.DocumentPointer{Pointer: usePtr})
		_ = lowerer.ctx.Siblings()
	})
	layers := testing.AllocsPerRun(20, func() { pathItemLayers(lowerer.ctx, rp, usePtr) })
	assert.Equal(t, within+1, layers, "the item's resolution and the one layer")
}
